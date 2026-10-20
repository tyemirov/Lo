use crate::{
    context::{host, Fault},
    tensor::Storage,
};
use std::{
    collections::{BTreeMap, BTreeSet},
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    },
};
pub static LIVE_STORAGES: AtomicU64 = AtomicU64::new(0);
pub static LIVE_STORAGE_BYTES: AtomicU64 = AtomicU64::new(0);
static NEXT_SESSION: AtomicU64 = AtomicU64::new(1);
#[derive(Clone, Copy)]
pub enum Class {
    Tensor = 0,
    Tape = 1,
    Optimizer = 2,
    Workspace = 3,
}
struct Entry {
    storage: Arc<Storage>,
    scope: usize,
    classes: [bool; 4],
}
struct Scope {
    parent: Option<usize>,
    owned: BTreeSet<u64>,
}
pub struct Ledger {
    pub session: u64,
    next: u64,
    pub live: u64,
    pub class_bytes: [u64; 4],
    pub peak_bytes: [u64; 4],
    entries: BTreeMap<u64, Entry>,
    scopes: Vec<Option<Scope>>,
    current: usize,
}
fn budget(subject: &str) -> Fault {
    let message = format!("device resource limit: {subject}");
    Fault::Budget {
        primary: message.clone(),
        message,
    }
}
impl Ledger {
    pub fn new() -> Self {
        Self {
            session: NEXT_SESSION.fetch_add(1, Ordering::Relaxed),
            next: 0,
            live: 0,
            class_bytes: [0; 4],
            peak_bytes: [0; 4],
            entries: BTreeMap::new(),
            scopes: vec![Some(Scope {
                parent: None,
                owned: BTreeSet::new(),
            })],
            current: 0,
        }
    }
    pub fn check_budget(&self, class: Class, bytes: u64) -> Result<(), Fault> {
        let i = class as usize;
        let limits = [64 << 20, 32 << 20, 32 << 20, 16 << 20];
        let names = [
            "tensor storage",
            "tape storage",
            "optimizer storage",
            "workspace storage",
        ];
        let used = if i == 0 {
            self.live
        } else {
            self.class_bytes[i]
        };
        if bytes > limits[i] - used {
            Err(budget(names[i]))
        } else {
            Ok(())
        }
    }
    pub fn check_allocation(&self, bytes: u64) -> Result<(), Fault> {
        if bytes > (64 << 20) - self.live {
            Err(budget("live tensor storage"))
        } else {
            Ok(())
        }
    }
    pub fn allocate(&mut self, values: Vec<f32>, location: &str) -> Result<Arc<Storage>, Fault> {
        let bytes = 4 * values.len() as u64;
        self.check_allocation(bytes)?;
        self.next += 1;
        let storage = Arc::new(Storage {
            session: self.session,
            id: self.next,
            location: location.into(),
            values,
        });
        LIVE_STORAGES.fetch_add(1, Ordering::Relaxed);
        LIVE_STORAGE_BYTES.fetch_add(bytes, Ordering::Relaxed);
        self.live += bytes;
        self.peak_bytes[0] = self.peak_bytes[0].max(self.live);
        self.entries.insert(
            storage.id,
            Entry {
                storage: storage.clone(),
                scope: self.current,
                classes: [false; 4],
            },
        );
        self.scopes[self.current]
            .as_mut()
            .unwrap()
            .owned
            .insert(storage.id);
        Ok(storage)
    }
    pub fn validate(&self, storage: &Storage) -> Result<(), Fault> {
        if storage.session != self.session || !self.entries.contains_key(&storage.id) {
            Err(host(
                "tensor allocation belongs to another invocation or a released scope",
            ))
        } else {
            Ok(())
        }
    }
    pub fn classify(&mut self, ids: &[u64], class: Class) -> Result<(), Fault> {
        let i = class as usize;
        self.check_budget(class, 0)?;
        let mut extra = 0;
        let unique: BTreeSet<u64> = ids.iter().copied().collect();
        for id in &unique {
            let e = self
                .entries
                .get(id)
                .ok_or_else(|| host("classify absent tensor allocation"))?;
            if !e.classes[i] {
                extra += 4 * e.storage.values.len() as u64
            }
        }
        self.check_budget(class, extra)?;
        for id in unique {
            self.entries.get_mut(&id).unwrap().classes[i] = true
        }
        self.class_bytes[i] += extra;
        self.peak_bytes[i] = self.peak_bytes[i].max(self.class_bytes[i]);
        Ok(())
    }
    pub fn unclassify(&mut self, ids: &[u64], class: Class) {
        let i = class as usize;
        for id in ids {
            if let Some(e) = self.entries.get_mut(id) {
                if e.classes[i] {
                    e.classes[i] = false;
                    self.class_bytes[i] -= 4 * e.storage.values.len() as u64
                }
            }
        }
    }
    pub fn release(&mut self, id: u64) {
        if let Some(e) = self.entries.remove(&id) {
            let bytes = 4 * e.storage.values.len() as u64;
            self.live -= bytes;
            for (i, active) in e.classes.iter().enumerate() {
                if *active {
                    self.class_bytes[i] -= bytes
                }
            }
            if let Some(s) = &mut self.scopes[e.scope] {
                s.owned.remove(&id);
            }
        }
    }
    pub fn scope_enter(&mut self) -> usize {
        let id = self.scopes.len();
        self.scopes.push(Some(Scope {
            parent: Some(self.current),
            owned: BTreeSet::new(),
        }));
        self.current = id;
        id
    }
    pub fn scope_keep(&mut self, id: usize, roots: &[u64]) {
        let keep: BTreeSet<u64> = roots.iter().copied().collect();
        let owned = self.scopes[id]
            .as_ref()
            .unwrap()
            .owned
            .iter()
            .copied()
            .collect::<Vec<_>>();
        for allocation in owned {
            if !keep.contains(&allocation) {
                self.release(allocation)
            }
        }
    }
    pub fn scope_retain(&mut self, id: usize, roots: &[u64]) {
        let parent = self.scopes[id].as_ref().unwrap().parent.unwrap();
        for root in roots {
            if let Some(e) = self.entries.get_mut(root) {
                if e.scope == id {
                    self.scopes[id].as_mut().unwrap().owned.remove(root);
                    self.scopes[parent].as_mut().unwrap().owned.insert(*root);
                    e.scope = parent;
                }
            }
        }
    }
    pub fn scope_close(&mut self, id: usize, roots: &[u64]) {
        if id != self.current || id == 0 {
            std::process::abort()
        }
        self.scope_keep(id, roots);
        self.scope_retain(id, roots);
        self.current = self.scopes[id].as_ref().unwrap().parent.unwrap();
        self.scopes[id] = None;
        while self.scopes.last().is_some_and(Option::is_none) {
            self.scopes.pop();
        }
    }
    pub fn reclaim_orphans(&mut self) {
        if self.current != 0 || self.scopes.len() != 1 {
            std::process::abort()
        }
        let orphans: Vec<u64> = self
            .entries
            .iter()
            .filter(|(_, entry)| entry.scope == 0 && Arc::strong_count(&entry.storage) == 1)
            .map(|(id, _)| *id)
            .collect();
        for id in orphans {
            self.release(id)
        }
    }
    pub fn current_scope(&self) -> usize {
        self.current
    }
}
