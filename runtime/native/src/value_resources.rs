use crate::{
    context::{limit, Fault},
    value::{Handle, Value},
    value_entry_map::EntryMap,
    value_graph::{is_leaf, GraphCache, Reachability},
    value_id_set::IdentitySet,
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
}

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
    ephemeral: IdentitySet,
    pinned: IdentitySet,
}
impl Scope {
    fn new(parent: Option<usize>) -> Self {
        Self {
            parent,
            ephemeral: IdentitySet::default(),
            pinned: IdentitySet::default(),
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
    entries: EntryMap<Entry>,
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
        self.entries.insert_absent(
            id,
            Entry {
                _value: Arc::downgrade(value),
                weight: value.weight,
                scope: self.current,
                pins: 0,
            },
        );
        self.track_candidate(self.current, id);
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
        self.entries.insert_absent(
            id,
            Entry {
                _value: Arc::downgrade(value),
                weight: value.weight,
                scope: self.current,
                pins: 0,
            },
        );
        self.track_candidate(self.current, id);
        Ok(())
    }
    fn track_candidate(&mut self, scope: usize, value: usize) {
        if scope != 0 {
            self.scopes[scope].as_mut().unwrap().ephemeral.insert(value);
        }
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
    fn release(&mut self, id: usize) {
        if let Some(entry) = self.entries.remove(&id) {
            self.graphs.remove(id);
            self.live -= entry.weight;
            let scope = self.scopes[entry.scope].as_mut().unwrap();
            scope.ephemeral.remove(&id);
            scope.pinned.remove(&id);
        }
    }
    pub fn scope_keep(&mut self, id: usize, roots: &[Handle]) {
        if self.scopes[id].as_ref().unwrap().ephemeral.is_empty() {
            return;
        }
        if roots.is_empty() {
            let candidates = self.scopes[id].as_mut().unwrap().ephemeral.drain();
            for value in candidates {
                self.release(value);
            }
            return;
        }
        if let Some(value) = leaf_identity(roots) {
            self.keep_leaf(id, value);
            return;
        }
        let keep = self.reachable(roots);
        self.keep_identities(id, &keep);
    }
    fn keep_leaf(&mut self, id: usize, retained: usize) {
        let scope = self.scopes[id].as_ref().unwrap();
        if scope.ephemeral.len() == 1 && scope.ephemeral.contains(&retained) {
            return;
        }
        let candidates = self.scopes[id].as_mut().unwrap().ephemeral.drain();
        let mut keep = false;
        for value in candidates {
            if value == retained {
                keep = true;
            } else {
                self.release(value);
            }
        }
        if keep {
            self.track_candidate(id, retained);
        }
    }
    fn keep_identities(&mut self, id: usize, keep: &Reachability) {
        let cached = self.cached_candidates(id, keep, false);
        let mut released = vec![];
        self.scopes[id].as_mut().unwrap().ephemeral.retain(|value| {
            if keep.direct.contains(value) || cached.contains(value) {
                true
            } else {
                released.push(*value);
                false
            }
        });
        for value in released {
            self.release(value);
        }
    }
    pub fn scope_retain(&mut self, id: usize, roots: &[Handle]) {
        let scope = self.scopes[id].as_ref().unwrap();
        if scope.ephemeral.is_empty() && scope.pinned.is_empty() {
            return;
        }
        let parent = self.scopes[id].as_ref().unwrap().parent.unwrap();
        if let Some(value) = leaf_identity(roots) {
            self.retain_identity(id, parent, value);
            return;
        }
        let reachable = self.reachable(roots);
        for value in reachable.direct.iter().copied() {
            self.retain_identity(id, parent, value);
        }
        if reachable.has_cached() {
            for value in self.cached_candidates(id, &reachable, true) {
                self.retain_identity(id, parent, value);
            }
        }
    }
    fn retain_identity(&mut self, id: usize, parent: usize, value: usize) {
        if let Some(entry) = self.entries.get_mut(&value) {
            if entry.scope == id {
                self.scopes[id].as_mut().unwrap().ephemeral.remove(&value);
                self.scopes[id].as_mut().unwrap().pinned.remove(&value);
                entry.scope = parent;
                entry.pins = 0;
                self.track_candidate(parent, value);
            }
        }
    }
    pub fn scope_pin(&mut self, id: usize, roots: &[Handle]) -> PinnedValues {
        let mut pinned = PinnedValues::Empty;
        let scope = self.scopes[id].as_ref().unwrap();
        if scope.ephemeral.is_empty() && scope.pinned.is_empty() {
            return pinned;
        }
        if let Some(value) = leaf_identity(roots) {
            let Some(entry) = self.entries.get_mut(&value) else {
                return pinned;
            };
            if entry.scope != id {
                return pinned;
            }
            entry.pins += 1;
            let scope = self.scopes[id].as_mut().unwrap();
            scope.ephemeral.remove(&value);
            scope.pinned.insert(value);
            pinned.push(value);
            return pinned;
        }
        let reachable = self.reachable(roots);
        let cached = self.cached_candidates(id, &reachable, true);
        let mut candidates = reachable.direct;
        candidates.extend(cached);
        for value in candidates {
            if let Some(entry) = self.entries.get_mut(&value) {
                if entry.scope == id {
                    entry.pins += 1;
                    let scope = self.scopes[id].as_mut().unwrap();
                    scope.ephemeral.remove(&value);
                    scope.pinned.insert(value);
                    pinned.push(value);
                }
            }
        }
        pinned
    }
    pub fn scope_unpin(&mut self, id: usize, values: &PinnedValues) {
        for value in values.as_slice() {
            if let Some(entry) = self.entries.get_mut(value) {
                if entry.scope == id {
                    entry.pins -= 1;
                    if entry.pins == 0 {
                        let scope = self.scopes[id].as_mut().unwrap();
                        scope.pinned.remove(value);
                        self.track_candidate(id, *value);
                    }
                }
            }
        }
    }
    pub fn scope_close(&mut self, id: usize, roots: &[Handle]) {
        assert_eq!(id, self.current);
        let scope = self.scopes[id].as_mut().unwrap();
        scope.ephemeral.extend(scope.pinned.drain());
        if !scope.ephemeral.is_empty() {
            let parent = self.scopes[id].as_ref().unwrap().parent.unwrap();
            if let Some(value) = leaf_identity(roots) {
                self.close_candidates(id, parent, |candidate| candidate == value);
            } else {
                let retained = self.reachable(roots);
                let cached = self.cached_candidates(id, &retained, false);
                self.close_candidates(id, parent, |candidate| {
                    retained.direct.contains(&candidate) || cached.contains(&candidate)
                });
            }
        }
        self.current = self.scopes[id].as_ref().unwrap().parent.unwrap();
        self.scopes.pop();
    }
    fn close_candidates(&mut self, id: usize, parent: usize, keep: impl Fn(usize) -> bool) {
        let candidates = self.scopes[id].as_mut().unwrap().ephemeral.drain();
        for value in candidates {
            if keep(value) {
                self.retain_identity(id, parent, value);
            } else {
                self.release(value);
            }
        }
    }
    pub fn reclaim_orphans(&mut self) {
        assert_eq!(self.current, 0);
        let expired = self
            .entries
            .iter()
            .filter_map(|(id, entry)| {
                (entry.scope == 0 && entry.pins == 0 && entry._value.strong_count() == 0)
                    .then_some(*id)
            })
            .collect::<Vec<_>>();
        for id in expired {
            self.release(id);
        }
    }
    fn reachable(&mut self, roots: &[Handle]) -> Reachability {
        let entries = &self.entries;
        let current = self.current;
        self.graphs.reachable(roots, |value| {
            entries
                .get(&value)
                .is_some_and(|entry| entry.scope != current)
        })
    }
    fn cached_candidates(
        &self,
        id: usize,
        reachable: &Reachability,
        include_pinned: bool,
    ) -> IdentitySet {
        let mut candidates = IdentitySet::default();
        if !reachable.has_cached() {
            return candidates;
        }
        let scope = self.scopes[id].as_ref().unwrap();
        let count = scope.ephemeral.len()
            + if include_pinned {
                scope.pinned.len()
            } else {
                0
            };
        if count.saturating_mul(reachable.cached_parts()) <= reachable.cached_len() {
            let pinned = include_pinned.then_some(&scope.pinned);
            candidates.extend(
                scope
                    .ephemeral
                    .iter()
                    .chain(pinned.into_iter().flat_map(|pinned| pinned.iter()))
                    .filter(|value| reachable.contains_cached(value))
                    .copied(),
            );
        } else {
            candidates.extend(reachable.cached_values().copied().filter(|value| {
                self.entries
                    .get(value)
                    .is_some_and(|entry| entry.scope == id)
                    && (include_pinned || scope.ephemeral.contains(value))
            }));
        }
        candidates
    }
}

fn leaf_identity(roots: &[Handle]) -> Option<usize> {
    let [value] = roots else {
        return None;
    };
    is_leaf(value).then_some(Arc::as_ptr(value) as usize)
}
