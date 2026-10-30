use crate::{
    context::{limit, Fault},
    value::{Handle, Value},
    value_entry_map::EntryMap,
    value_graph::{is_leaf, GraphCache, Reachability},
    value_id_set::IdentitySet,
    value_receipts::{ReceiptArena, SmallSlots},
};
use std::sync::{Arc, Weak};

#[repr(C)]
#[derive(Clone, Copy)]
pub struct Limits {
    pub value_bytes: u64,
    pub live_bytes: u64,
    pub data_bytes: u64,
    pub list_items: u64,
    pub file_entries: u64,
    pub file_directories: u64,
}
impl Default for Limits {
    fn default() -> Self {
        Self {
            value_bytes: 16 << 20,
            live_bytes: 64 << 20,
            data_bytes: 0,
            list_items: 0,
            file_entries: 0,
            file_directories: 128,
        }
    }
}
pub fn exceeds(used: u64, additional: u64, maximum: u64) -> bool {
    additional > u64::MAX - used
        || (maximum != 0 && (used > maximum || additional > maximum - used))
}

struct Entry {
    _value: Weak<Value>,
    weight: u64,
    scope: usize,
    pins: usize,
    position: usize,
}
impl Entry {
    fn id(&self) -> usize {
        self._value.as_ptr() as usize
    }
}
const NO_POSITION: usize = usize::MAX;

pub enum PinnedValues {
    Empty,
    One(usize),
    Many(Vec<usize>),
}
impl PinnedValues {
    fn push(&mut self, value: usize) {
        match self {
            Self::Empty => *self = Self::One(value),
            Self::One(first) => *self = Self::Many(vec![*first, value]),
            Self::Many(values) => values.push(value),
        }
    }
    fn as_slice(&self) -> &[usize] {
        match self {
            Self::Empty => &[],
            Self::One(value) => std::slice::from_ref(value),
            Self::Many(values) => values,
        }
    }
}

struct Scope {
    parent: Option<usize>,
    ephemeral: SmallSlots,
    pinned: IdentitySet,
    // Every slot before this boundary is covered by the immutable anchors.
    // New admissions append after it and require a fresh membership check.
    protected: usize,
    anchors: Vec<Weak<Value>>,
}
impl Scope {
    fn new(parent: Option<usize>) -> Self {
        Self {
            parent,
            ephemeral: SmallSlots::default(),
            pinned: IdentitySet::default(),
            protected: 0,
            anchors: Vec::new(),
        }
    }
}

/// Logical expanded-value weights owned by active evaluation scopes.
/// Container and child weights can both be charged; this is not allocator RSS.
pub struct Ledger {
    pub limits: Limits,
    pub live: u64,
    pub data: u64,
    progress: u64,
    // Canonical identities resolve to stable slots; scopes transfer slots
    // without probing the identity map again.
    entries: EntryMap<usize>,
    receipts: ReceiptArena<Entry>,
    graphs: GraphCache,
    scopes: Vec<Option<Scope>>,
    current: usize,
}
impl Ledger {
    pub fn new(limits: Limits) -> Self {
        Self {
            limits,
            live: 0,
            data: 0,
            progress: 0,
            entries: EntryMap::new(),
            receipts: ReceiptArena::default(),
            graphs: GraphCache::default(),
            scopes: vec![Some(Scope::new(None))],
            current: 0,
        }
    }
    pub fn preflight(&self, weight: u64) -> Result<(), Fault> {
        if exceeds(0, weight, self.limits.value_bytes) {
            return Err(limit("expanded value bytes"));
        }
        if exceeds(self.live, weight, self.limits.live_bytes) {
            return Err(limit("live value bytes"));
        }
        if exceeds(self.data, weight, self.limits.data_bytes) {
            return Err(limit("cumulative data bytes"));
        }
        Ok(())
    }
    pub fn retain_root(&mut self, value: &Handle) -> Result<(), Fault> {
        let id = Arc::as_ptr(value) as usize;
        if self.entries.contains_key(&id) {
            return Ok(());
        }
        if exceeds(0, value.weight, self.limits.value_bytes) {
            return Err(limit("expanded value bytes"));
        }
        if exceeds(self.live, value.weight, self.limits.live_bytes) {
            return Err(limit("live value bytes"));
        }
        self.live = self.live.saturating_add(value.weight);
        self.admit(id, value);
        Ok(())
    }
    pub fn reserve_worklist(&mut self, count: usize) -> Result<(), Fault> {
        let bytes = (count as u64)
            .checked_mul(16)
            .ok_or_else(|| limit("live value bytes"))?;
        if exceeds(self.live, bytes, self.limits.live_bytes) {
            return Err(limit("live value bytes"));
        }
        self.live = self.live.saturating_add(bytes);
        Ok(())
    }
    pub fn release_worklist(&mut self, count: usize) {
        self.live -= count as u64 * 16;
    }
    pub fn charge(&mut self, value: &Handle) -> Result<(), Fault> {
        let id = Arc::as_ptr(value) as usize;
        if self.entries.contains_key(&id) {
            return Ok(());
        }
        if self.limits.list_items != 0 {
            if exceeds(
                0,
                self.reachable(std::slice::from_ref(value)).max_list_items as u64,
                self.limits.list_items,
            ) {
                return Err(limit("list elements"));
            }
        }
        self.preflight(value.weight)?;
        self.live = self.live.saturating_add(value.weight);
        self.data = self.data.saturating_add(value.weight);
        self.admit(id, value);
        Ok(())
    }
    fn admit(&mut self, id: usize, value: &Handle) {
        let slot = self.receipts.insert(Entry {
            _value: Arc::downgrade(value),
            weight: value.weight,
            scope: self.current,
            pins: 0,
            position: NO_POSITION,
        });
        self.entries.insert_absent(id, slot);
        let position = self.track_candidate(self.current, slot);
        self.receipts.get_mut(slot).position = position;
    }
    fn track_candidate(&mut self, scope: usize, slot: usize) -> usize {
        if scope == 0 {
            return NO_POSITION;
        }
        self.scopes[scope].as_mut().unwrap().ephemeral.push(slot)
    }
    fn swap_candidates(&mut self, owner: usize, first: usize, second: usize) {
        let scope = self.scopes[owner].as_mut().unwrap();
        scope.ephemeral.swap(first, second);
        let (a, b) = (scope.ephemeral.get(first), scope.ephemeral.get(second));
        self.receipts.get_mut(a).position = first;
        self.receipts.get_mut(b).position = second;
    }
    fn detach(&mut self, slot: usize) {
        let entry = self.receipts.get(slot);
        let (owner, mut position) = (entry.scope, entry.position);
        if owner != 0 {
            let protected = self.scopes[owner].as_ref().unwrap().protected;
            if position < protected {
                self.swap_candidates(owner, position, protected - 1);
                position = protected - 1;
                self.scopes[owner].as_mut().unwrap().protected -= 1;
            }
            let scope = self.scopes[owner].as_mut().unwrap();
            if position != NO_POSITION {
                if let Some(moved) = scope.ephemeral.swap_remove(position) {
                    self.receipts.get_mut(moved).position = position;
                }
            } else {
                scope.pinned.remove(&slot);
            }
        }
        self.receipts.get_mut(slot).position = NO_POSITION;
    }
    pub fn replace_progress(&mut self, weight: u64) -> Result<(), Fault> {
        let old = self.progress;
        self.live -= old;
        if let Err(fault) = self.preflight(weight) {
            self.live += old;
            return Err(fault);
        }
        self.progress = weight;
        self.live = self.live.saturating_add(weight);
        self.data = self.data.saturating_add(weight);
        Ok(())
    }
    pub fn scope_enter(&mut self, id: usize) {
        assert_eq!(id, self.scopes.len());
        self.scopes.push(Some(Scope::new(Some(self.current))));
        self.current = id;
    }
    fn release_detached(&mut self, slot: usize) {
        let id = self.receipts.get(slot).id();
        self.entries.remove(&id);
        self.graphs.remove(id);
        let entry = self.receipts.remove(slot);
        self.live -= entry.weight;
    }
    fn release(&mut self, slot: usize) {
        self.detach(slot);
        self.release_detached(slot);
    }
    pub fn scope_keep(&mut self, id: usize, roots: &[Handle]) {
        if self.scopes[id].as_ref().unwrap().ephemeral.is_empty() {
            return;
        }
        if roots.is_empty() {
            let scope = self.scopes[id].as_mut().unwrap();
            scope.protected = 0;
            scope.anchors.clear();
            let candidates = scope.ephemeral.drain();
            for slot in candidates {
                self.release_detached(slot);
            }
            return;
        }
        if let Some(value) = leaf_identity(roots) {
            let scope = self.scopes[id].as_mut().unwrap();
            scope.protected = 0;
            scope.anchors.clear();
            let mut index = 0;
            while index < self.scopes[id].as_ref().unwrap().ephemeral.len() {
                let slot = self.scopes[id].as_ref().unwrap().ephemeral.get(index);
                if self.receipts.get(slot).id() == value {
                    index += 1;
                } else {
                    self.release(slot);
                }
            }
            return;
        }
        let discover = {
            let scope = self.scopes[id].as_ref().unwrap();
            scope.protected == 0 && scope.ephemeral.len() >= 16
        };
        let mut keep = self.reachable(roots);
        if discover || keep.direct.len() >= 16 {
            let entries = &self.entries;
            keep = self
                .graphs
                .reachable_keep(roots, |value| entries.contains_key(&value));
        }
        let scope = self.scopes[id].as_mut().unwrap();
        if !anchors_reachable(&scope.anchors, &keep) {
            scope.protected = 0;
            scope.anchors.clear();
        }
        if scope.protected == 0 {
            scope.anchors.clear();
        }
        let start = scope.protected;
        let fragments = self.cached_candidates_from(id, &keep, false, start);
        let mut promoted = false;
        let mut index = start;
        while index < self.scopes[id].as_ref().unwrap().ephemeral.len() {
            let slot = self.scopes[id].as_ref().unwrap().ephemeral.get(index);
            let value = self.receipts.get(slot).id();
            let fragment = fragments.contains(&slot);
            if fragment || keep.direct.contains(&value) {
                if fragment {
                    let boundary = self.scopes[id].as_ref().unwrap().protected;
                    self.swap_candidates(id, index, boundary);
                    self.scopes[id].as_mut().unwrap().protected += 1;
                    promoted = true;
                }
                index += 1;
            } else {
                self.release(slot);
            }
        }
        let scope = self.scopes[id].as_mut().unwrap();
        if scope.protected < 16 {
            scope.protected = 0;
            scope.anchors.clear();
        } else if promoted {
            let mut known = IdentitySet::default();
            known.extend(scope.anchors.iter().map(|root| root.as_ptr() as usize));
            for root in keep.cached_roots() {
                if known.insert(root.as_ptr() as usize) {
                    scope.anchors.push(root.clone());
                }
            }
        }
    }
    pub fn scope_retain(&mut self, id: usize, roots: &[Handle]) {
        let scope = self.scopes[id].as_ref().unwrap();
        if scope.ephemeral.is_empty() && scope.pinned.is_empty() {
            return;
        }
        let parent = scope.parent.unwrap();
        if let Some(value) = leaf_identity(roots) {
            self.retain_identity(id, parent, value);
            return;
        }
        let reachable = self.reachable(roots);
        let cached = self.cached_candidates(id, &reachable, true);
        for value in reachable.direct.iter().copied() {
            self.retain_identity(id, parent, value);
        }
        for slot in cached {
            if self.receipts.get(slot).scope == id {
                self.transfer(slot, parent);
            }
        }
    }
    fn transfer_detached(&mut self, slot: usize, parent: usize) {
        let position = self.track_candidate(parent, slot);
        let entry = self.receipts.get_mut(slot);
        entry.scope = parent;
        entry.pins = 0;
        entry.position = position;
    }
    fn transfer(&mut self, slot: usize, parent: usize) {
        self.detach(slot);
        self.transfer_detached(slot, parent);
    }
    fn retain_identity(&mut self, id: usize, parent: usize, value: usize) {
        if let Some(&slot) = self.entries.get(&value) {
            if self.receipts.get(slot).scope == id {
                self.transfer(slot, parent);
            }
        }
    }
    fn pin_slot(&mut self, id: usize, slot: usize, pinned: &mut PinnedValues) {
        let entry = self.receipts.get(slot);
        if entry.scope != id {
            return;
        }
        let value = entry.id();
        if entry.pins == 0 {
            self.detach(slot);
            self.scopes[id].as_mut().unwrap().pinned.insert(slot);
        }
        self.receipts.get_mut(slot).pins += 1;
        pinned.push(value);
    }
    pub fn scope_pin(&mut self, id: usize, roots: &[Handle]) -> PinnedValues {
        let mut pinned = PinnedValues::Empty;
        let scope = self.scopes[id].as_ref().unwrap();
        if scope.ephemeral.is_empty() && scope.pinned.is_empty() {
            return pinned;
        }
        if let Some(value) = leaf_identity(roots) {
            if let Some(&slot) = self.entries.get(&value) {
                self.pin_slot(id, slot, &mut pinned);
            }
            return pinned;
        }
        let reachable = self.reachable(roots);
        let mut candidates = self.cached_candidates(id, &reachable, true);
        for value in reachable.direct {
            if let Some(&slot) = self.entries.get(&value) {
                if self.receipts.get(slot).scope == id {
                    candidates.insert(slot);
                }
            }
        }
        for slot in candidates {
            self.pin_slot(id, slot, &mut pinned);
        }
        pinned
    }
    pub fn scope_unpin(&mut self, id: usize, values: &PinnedValues) {
        for value in values.as_slice() {
            if let Some(&slot) = self.entries.get(value) {
                let entry = self.receipts.get_mut(slot);
                if entry.scope == id {
                    entry.pins -= 1;
                    if entry.pins == 0 {
                        self.scopes[id].as_mut().unwrap().pinned.remove(&slot);
                        let position = self.track_candidate(id, slot);
                        self.receipts.get_mut(slot).position = position;
                    }
                }
            }
        }
    }
    pub fn scope_close(&mut self, id: usize, roots: &[Handle]) {
        assert_eq!(id, self.current);
        let scope = self.scopes[id].as_ref().unwrap();
        let empty = scope.ephemeral.is_empty() && scope.pinned.is_empty();
        let leaf = leaf_identity(roots);
        let reachable = if !empty && leaf.is_none() {
            self.reachable(roots)
        } else {
            Reachability::default()
        };
        let cached = self.cached_candidates(id, &reachable, true);
        let scope = self.scopes.pop().unwrap().unwrap();
        let parent = scope.parent.unwrap();
        self.current = parent;
        for slot in scope.ephemeral.into_iter().chain(scope.pinned) {
            let value = self.receipts.get(slot).id();
            if leaf == Some(value) || reachable.direct.contains(&value) || cached.contains(&slot) {
                self.transfer_detached(slot, parent);
            } else {
                self.release_detached(slot);
            }
        }
    }
    pub fn reclaim_orphans(&mut self) {
        assert_eq!(self.current, 0);
        let expired = self
            .entries
            .iter()
            .filter_map(|(_, &slot)| {
                let entry = self.receipts.get(slot);
                (entry.scope == 0 && entry.pins == 0 && entry._value.strong_count() == 0)
                    .then_some(slot)
            })
            .collect::<Vec<_>>();
        for slot in expired {
            self.release(slot);
        }
    }
    fn reachable(&mut self, roots: &[Handle]) -> Reachability {
        let entries = &self.entries;
        let receipts = &self.receipts;
        let current = self.current;
        self.graphs.reachable(roots, |value| {
            entries
                .get(&value)
                .is_some_and(|&slot| receipts.get(slot).scope != current)
        })
    }
    fn cached_candidates(
        &self,
        id: usize,
        reachable: &Reachability,
        include_pinned: bool,
    ) -> IdentitySet {
        self.cached_candidates_from(id, reachable, include_pinned, 0)
    }
    fn cached_candidates_from(
        &self,
        id: usize,
        reachable: &Reachability,
        include_pinned: bool,
        start: usize,
    ) -> IdentitySet {
        let mut candidates = IdentitySet::default();
        if !reachable.has_cached() {
            return candidates;
        }
        let scope = self.scopes[id].as_ref().unwrap();
        let count = scope.ephemeral.len() - start
            + if include_pinned {
                scope.pinned.len()
            } else {
                0
            };
        if count.saturating_mul(reachable.cached_parts()) <= reachable.cached_len() {
            for index in start..scope.ephemeral.len() {
                let slot = scope.ephemeral.get(index);
                if reachable.contains_cached(&self.receipts.get(slot).id()) {
                    candidates.insert(slot);
                }
            }
            if include_pinned {
                for &slot in scope.pinned.iter() {
                    if reachable.contains_cached(&self.receipts.get(slot).id()) {
                        candidates.insert(slot);
                    }
                }
            }
        } else {
            for value in reachable.cached_values() {
                if let Some(&slot) = self.entries.get(value) {
                    let entry = self.receipts.get(slot);
                    if entry.scope == id
                        && ((entry.position != NO_POSITION && entry.position >= start)
                            || (include_pinned && entry.pins != 0))
                    {
                        candidates.insert(slot);
                    }
                }
            }
        }
        candidates
    }
}

fn anchors_reachable(anchors: &[Weak<Value>], keep: &Reachability) -> bool {
    let mut missing = IdentitySet::default();
    missing.extend(
        anchors
            .iter()
            .map(|root| root.as_ptr() as usize)
            .filter(|value| !keep.direct.contains(value)),
    );
    if missing.is_empty() {
        return true;
    }
    if missing.len().saturating_mul(keep.cached_parts()) <= keep.cached_len() {
        missing.iter().all(|value| keep.contains_cached(value))
    } else {
        for value in keep.cached_values() {
            missing.remove(value);
            if missing.is_empty() {
                return true;
            }
        }
        false
    }
}

fn leaf_identity(roots: &[Handle]) -> Option<usize> {
    let [value] = roots else {
        return None;
    };
    is_leaf(value).then_some(Arc::as_ptr(value) as usize)
}
