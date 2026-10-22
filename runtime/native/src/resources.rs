use crate::{
    context::{host, Fault},
    device::{self, Backing, Config, Control, Operation},
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
    config: Config,
    backend: Option<device::Backend>,
    cleanup: Vec<Fault>,
    lost: Option<Fault>,
    closed: bool,
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
    pub fn with_config(config: Config) -> Self {
        Self {
            config,
            backend: None,
            cleanup: vec![],
            lost: None,
            closed: false,
            session: NEXT_SESSION.fetch_add(1, Ordering::Relaxed),
            next: 0,
            live: 0,
            class_bytes: [0; 4],
            peak_bytes: [0; 4],
            entries: BTreeMap::new(),
            scopes: {
                let mut scopes = Vec::with_capacity(2);
                scopes.push(Some(Scope {
                    parent: None,
                    owned: BTreeSet::new(),
                }));
                scopes
            },
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
            bytes,
            backing: Backing::Host(values),
        });
        LIVE_STORAGES.fetch_add(1, Ordering::Relaxed);
        LIVE_STORAGE_BYTES.fetch_add(bytes, Ordering::Relaxed);
        self.live += bytes;
        self.peak_bytes[0] = self.peak_bytes[0].max(self.live);
        self.config
            .emit(2, storage.id, location, bytes, self.live, None);
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
    pub fn can_configure(&self) -> bool {
        self.next == 0 && self.backend.is_none() && !self.closed
    }
    pub fn configure(&mut self, config: Config) -> bool {
        if !self.can_configure() {
            return false;
        }
        self.config = config;
        true
    }
    fn backend_fault<T>(
        &mut self,
        control: &Control<'_>,
        result: Result<T, Fault>,
    ) -> Result<T, Fault> {
        if let Err(fault) = &result {
            if matches!(fault.primary(), Fault::Numeric("Device.Lost", _)) {
                self.lost = Some(fault.clone());
            }
        }
        control.check()?;
        result
    }
    fn open(&mut self, location: &str, control: &Control<'_>) -> Result<(), Fault> {
        control.check()?;
        if let Some(fault) = &self.lost {
            return Err(fault.clone());
        }
        if self.backend.is_some() {
            return Ok(());
        }
        if location != self.config.grant {
            return Err(host(format!(
                "unauthorized internal device request {location}"
            )));
        }
        let result = device::Backend::open(&self.config, location);
        let mut backend = match result {
            Ok(backend) => backend,
            Err(fault) => return self.backend_fault(control, Err(fault)),
        };
        if let Err(fault) = control.check() {
            if let Err(cleanup) = backend.close() {
                self.cleanup.push(cleanup);
            }
            return Err(fault);
        }
        self.config
            .emit(1, 0, location, 0, self.live, Some(&backend.identity));
        self.backend = Some(backend);
        Ok(())
    }
    pub fn allocate_device(
        &mut self,
        location: &str,
        bytes: u64,
        control: &Control<'_>,
    ) -> Result<Arc<Storage>, Fault> {
        if self.closed {
            return Err(host("allocate after device session close"));
        }
        control.check()?;
        if bytes % 4 != 0 {
            return Err(host("FP32 allocation size is not divisible by four"));
        }
        self.check_allocation(bytes)?;
        self.open(location, control)?;
        let result = self.backend.as_ref().unwrap().allocate(bytes);
        let allocation = match result {
            Ok(allocation) => allocation,
            Err(fault) => return self.backend_fault(control, Err(fault)),
        };
        if let Err(fault) = control.check() {
            if let Err(cleanup) = allocation.close() {
                self.cleanup.push(cleanup);
            }
            return Err(fault);
        }
        let reserved = allocation.reserved_bytes();
        let rejection = if reserved < bytes {
            Some(device::fault(
                "Device.AllocationFailed",
                format!("backend reserved {reserved} bytes for {bytes} requested bytes"),
            ))
        } else if reserved > (64 << 20) - self.live {
            Some(budget("reserved device storage"))
        } else {
            None
        };
        if let Some(fault) = rejection {
            if let Err(cleanup) = allocation.close() {
                self.cleanup.push(cleanup);
            }
            return Err(fault);
        }
        self.next += 1;
        let storage = Arc::new(Storage {
            session: self.session,
            id: self.next,
            location: location.into(),
            bytes: reserved,
            backing: Backing::Device(allocation),
        });
        LIVE_STORAGES.fetch_add(1, Ordering::Relaxed);
        LIVE_STORAGE_BYTES.fetch_add(reserved, Ordering::Relaxed);
        self.live += reserved;
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
        self.config
            .emit(2, storage.id, location, reserved, self.live, None);
        Ok(storage)
    }
    pub fn transfer(
        &mut self,
        source: &Arc<Storage>,
        destination: &str,
        control: &Control<'_>,
    ) -> Result<Arc<Storage>, Fault> {
        control.check()?;
        self.validate(source)?;
        for selector in [&source.location[..], destination] {
            if selector != "cpu" && selector != self.config.grant {
                return Err(Fault::Host(
                    "runtime.capability-denied",
                    format!("Tensor.to {selector:?} has no host capability"),
                ));
            }
        }
        if source.location == destination {
            return Ok(source.clone());
        }
        if source.location != "cpu" && destination != "cpu" {
            return Err(Fault::Numeric(
                "Tensor.DeviceMismatch",
                "transfers require CPU and one compute location".into(),
            ));
        }
        if let Some(fault) = &self.lost {
            return Err(fault.clone());
        }
        let target = if destination == "cpu" {
            self.allocate(vec![0.0; (source.logical_bytes() / 4) as usize], "cpu")?
        } else {
            self.allocate_device(destination, source.logical_bytes(), control)?
        };
        let id = target.id;
        let result = if destination == "cpu" {
            let Backing::Device(allocation) = &source.backing else {
                std::process::abort();
            };
            drop(target);
            let target = Arc::get_mut(&mut self.entries.get_mut(&id).unwrap().storage).unwrap();
            let Backing::Host(values) = &mut target.backing else {
                std::process::abort();
            };
            allocation.download(values)
        } else {
            let Backing::Device(allocation) = &target.backing else {
                std::process::abort();
            };
            allocation.upload(source.host_values())
        };
        if let Err(fault) = self.backend_fault(control, result) {
            self.release(id);
            return Err(fault);
        }
        let target = self.entries[&id].storage.clone();
        self.config
            .emit(3, id, destination, target.bytes, self.live, None);
        Ok(target)
    }
    pub fn compute(
        &mut self,
        location: &str,
        operation: &Operation,
        inputs: &[Arc<Storage>],
        control: &Control<'_>,
    ) -> Result<Arc<Storage>, Fault> {
        self.open(location, control)?;
        let result = self.backend.as_ref().unwrap().supports(operation);
        self.backend_fault(control, result)?;
        if location == "metal:0"
            && self.backend.as_ref().unwrap().identity.profile()? != device::METAL_PROFILE
        {
            return Err(device::fault(
                "Device.UnsupportedProfile",
                "Metal computation requires metal-fp32",
            ));
        }
        let mut buffers = vec![];
        for input in inputs {
            self.validate(input)?;
            if input.location != location {
                return Err(device::fault(
                    "Tensor.DeviceMismatch",
                    "compute inputs require the same invocation and location",
                ));
            }
            let Backing::Device(buffer) = &input.backing else {
                std::process::abort();
            };
            buffers.push(buffer.clone());
        }
        let bytes = if operation.kind == device::SQRT_GRAD {
            0
        } else {
            4 * operation.count as u64
        };
        let output = self.allocate_device(location, bytes, control)?;
        if operation.count == 0 {
            return Ok(output);
        }
        let status = match (|| {
            self.check_budget(Class::Workspace, 8)?;
            let status = self.allocate_device(location, 4, control)?;
            if let Err(fault) = self.classify(&[status.id], Class::Workspace) {
                self.release(status.id);
                return Err(fault);
            }
            Ok(status)
        })() {
            Ok(status) => status,
            Err(fault) => {
                self.release(output.id);
                return Err(fault);
            }
        };
        let Backing::Device(output_buffer) = &output.backing else {
            std::process::abort();
        };
        let Backing::Device(status_buffer) = &status.backing else {
            std::process::abort();
        };
        let result = self.backend.as_ref().unwrap().compute(
            operation,
            &buffers,
            output_buffer,
            status_buffer,
        );
        let result = self.backend_fault(control, result);
        if let Err(fault) = result {
            self.release(status.id);
            self.release(output.id);
            return Err(fault);
        }
        self.config
            .emit(6, output.id, location, output.bytes, self.live, None);
        self.release(status.id);
        Ok(output)
    }
    pub fn close(&mut self) -> Result<(), Fault> {
        if !self.closed {
            let ids = self.entries.keys().copied().collect::<Vec<_>>();
            for id in ids {
                self.release(id);
            }
            if let Some(mut backend) = self.backend.take() {
                if let Err(fault) = backend.close() {
                    self.cleanup.push(fault);
                }
            }
            self.closed = true;
            self.config.emit(5, 0, "", 0, self.live, None);
        }
        if self.cleanup.is_empty() {
            Ok(())
        } else {
            Err(host(
                self.cleanup
                    .iter()
                    .map(Fault::message)
                    .collect::<Vec<_>>()
                    .join("\n"),
            ))
        }
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
                extra += e.storage.bytes
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
                    self.class_bytes[i] -= e.storage.bytes
                }
            }
        }
    }
    pub fn release(&mut self, id: u64) {
        if let Some(e) = self.entries.remove(&id) {
            let bytes = e.storage.bytes;
            if let Err(fault) = e.storage.release() {
                self.cleanup.push(fault);
            }
            self.live -= bytes;
            self.config
                .emit(4, id, &e.storage.location, bytes, self.live, None);
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

impl Drop for Ledger {
    fn drop(&mut self) {
        let _ = self.close();
    }
}
