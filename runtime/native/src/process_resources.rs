use super::{hashing, ownership, provider::FileIdentity, supervisor_wire::{Decoder, Descriptor, Encoder}, Processes};
use crate::{context::{host, limit, Context, Fault}, locks::{Locks, RemoteFreeze}};
use std::{ffi::CString, fs::File, mem::{size_of, ManuallyDrop}, os::{fd::{AsRawFd, FromRawFd, IntoRawFd, RawFd}, unix::fs::{FileTypeExt, MetadataExt}}, path::{Component, Path, PathBuf}, sync::Arc};

const MAX_RESOURCES: usize = 65536;
const MAX_PATH: usize = 1024;
const LOCK_GROUP: u32 = u32::MAX;
#[repr(u32)]
#[derive(Clone, Copy, PartialEq, Eq)]
enum Kind { Image = 1, Launcher = 2, Read = 3, Write = 4, Prune = 5, Endpoint = 6, Ancestor = 7, Lock = 8, ProgramEntry = 9 }
impl Kind {
    fn decode(value: u32) -> Result<Self, &'static str> {
        Ok(match value { 1=>Self::Image, 2=>Self::Launcher, 3=>Self::Read, 4=>Self::Write,
            5=>Self::Prune, 6=>Self::Endpoint, 7=>Self::Ancestor, 8=>Self::Lock, 9=>Self::ProgramEntry,
            _=>return Err("unknown Process resource kind") })
    }
    fn image(self) -> bool { matches!(self, Self::Image | Self::Launcher) }
    fn regular(self) -> bool { self.image() || self == Self::ProgramEntry }
}
struct Resource {
    kind: Kind,
    group: u32,
    path: String,
    file: FileIdentity,
    endpoint: FileIdentity,
    digest: String,
    descriptor: Descriptor,
}
pub(super) struct Inventory {
    resources: Vec<Resource>,
    expected: usize,
    storage: u64,
    remote: Option<(Arc<Locks>, RemoteFreeze)>,
    closed: bool,
    close_error: Option<String>,
    pending: Option<Descriptor>,
}
// Construct this proof only after the final receipt, EOF, and unreaped child
// identity have all been checked by the supervisor transport owner.
pub(super) struct RemoteReleaseProof(());
impl RemoteReleaseProof {
    pub fn complete(receipt: bool, channel_eof: bool, supervisor_reaped: bool) -> Result<Self, &'static str> {
        if receipt && channel_eof && supervisor_reaped { Ok(Self(())) }
        else { Err("Process remote ownership lacks complete release proof") }
    }
}
fn storage_bound(count: usize) -> Result<u64, &'static str> {
    if count == 0 || count > MAX_RESOURCES { return Err("Process resource inventory exceeds its bound"); }
    // Includes the retained records, image copies, one encoded frame, and
    // one validation walk. Paths and digests have independent fixed bounds.
    let item = size_of::<Resource>() + size_of::<ownership::Image>() + MAX_PATH * 3 + 128;
    (count as u64).checked_mul(item as u64).and_then(|n| n.checked_add(16384 + size_of::<Inventory>() as u64))
        .ok_or("Process resource storage overflow")
}
fn borrowed(raw: RawFd) -> ManuallyDrop<File> { ManuallyDrop::new(unsafe { File::from_raw_fd(raw) }) }
fn identity(raw: RawFd) -> Result<FileIdentity, &'static str> {
    let info = borrowed(raw).metadata().map_err(|_| "Process resource identity is unavailable")?;
    Ok(FileIdentity { device: info.dev(), inode: info.ino() })
}
fn duplicate(raw: RawFd) -> Result<Descriptor, &'static str> {
    let copy = unsafe { libc::fcntl(raw, libc::F_DUPFD_CLOEXEC, 16) };
    if copy < 0 { Err("Process resource duplication failed") } else { Ok(unsafe { Descriptor::take(copy) }) }
}
fn clean(path: &str) -> bool {
    let source = Path::new(path);
    if !source.is_absolute() || path.len() > MAX_PATH || path.as_bytes().contains(&0) { return false; }
    let mut normalized = PathBuf::new();
    for part in source.components() {
        match part { Component::RootDir => normalized.push("/"), Component::Normal(name) => normalized.push(name), _ => return false }
    }
    normalized.as_os_str() == source.as_os_str()
}
fn open_at(parent: Option<RawFd>, name: &str, directory: bool) -> Result<Descriptor, &'static str> {
    let name = CString::new(name).map_err(|_| "Process resource name contains NUL")?;
    let flags = libc::O_RDONLY | libc::O_CLOEXEC | libc::O_NOFOLLOW | libc::O_NONBLOCK
        | if directory { libc::O_DIRECTORY } else { 0 };
    let raw = unsafe { match parent { Some(parent) => libc::openat(parent, name.as_ptr(), flags), None => libc::open(name.as_ptr(), flags) } };
    if raw < 0 { Err("Process resource cannot be opened without following links") }
    else { Ok(unsafe { Descriptor::take(raw) }) }
}
fn named_identity(parent: RawFd, name: &str) -> Result<FileIdentity, &'static str> {
    let name = CString::new(name).map_err(|_| "Process resource name contains NUL")?;
    let mut information: libc::stat = unsafe { std::mem::zeroed() };
    if unsafe { libc::fstatat(parent, name.as_ptr(), &mut information, libc::AT_SYMLINK_NOFOLLOW) } != 0 {
        return Err("Process resource namespace is unavailable");
    }
    Ok(FileIdentity { device: information.st_dev as u64, inode: information.st_ino as u64 })
}
fn digest(raw: RawFd, check: &mut impl FnMut() -> Result<(), Fault>) -> Result<String, Fault> {
    Ok(hashing::hex(&hashing::digest_with_cancel(&borrowed(raw), check)?))
}
impl Inventory {
    pub fn capture(context: &mut Context, processes: &Processes, initial_executable: &str, generation: u64) -> Result<Self, Fault> {
        let scope = processes.scopes.get(initial_executable).ok_or_else(|| host("Process initial executable has no admitted scope"))?;
        let entry = context.session.entry.clone();
        let entry_path = entry.path.as_ref().map_err(|reason| host(format!("retain Process supervisor entry: {reason}")))?;
        let retain_entry = entry_path != &processes.launcher;
        if retain_entry && !clean(entry_path) { return Err(host("Process supervisor entry path exceeds the transfer contract")); }
        let mut paths = processes.registrations.keys().chain(processes.roots.iter())
            .chain(scope.writes.iter()).chain(scope.prunes.iter()).chain(scope.endpoints.iter());
        let entry_count = if retain_entry { Path::new(entry_path).components().count() } else { 0 };
        let count = paths.try_fold(crate::locks::MAX_LOCKS + entry_count, |count, path| {
            count.checked_add(Path::new(path).components().count()).ok_or_else(|| limit("Process resource inventory"))
        })?;
        let storage = storage_bound(count).map_err(host)?;
        context.values.preflight(storage)?;
        context.values.reserve_temporary(storage)?;
        let mut resources = Vec::new();
        if resources.try_reserve_exact(count).is_err() {
            context.values.release_temporary(storage);
            return Err(host("Process resource storage is unavailable"));
        }
        let mut inventory = Self { resources, expected: 0, storage, remote: None, closed: false, close_error: None, pending: None };
        let result = (|| {
            let mut group = 0;
            for (path, registration) in &processes.registrations {
                context.step()?;
                processes.validate_image(path, || context.check_cancel())?;
                let start = inventory.resources.len();
                inventory.capture_path(path, if *path == processes.launcher { Kind::Launcher } else { Kind::Image }, group, || context.step())?;
                let end = inventory.resources.len() - 1;
                for resource in &inventory.resources[start..end] {
                    let mut expected = None;
                    for (path, identity) in &registration.ancestors {
                        context.step()?;
                        if path.as_os_str() == Path::new(&resource.path).as_os_str() { expected = Some(*identity); break; }
                    }
                    let expected = expected.ok_or_else(|| host("Process executable ancestor was not registered"))?;
                    if resource.file != (FileIdentity { device: expected.0, inode: expected.1 }) {
                        return Err(host("Process executable ancestor identity changed before transfer"));
                    }
                }
                let leaf = &mut inventory.resources[end];
                let expected = FileIdentity { device: registration.identity.0, inode: registration.identity.1 };
                if leaf.file != expected { return Err(host("Process executable identity changed before transfer")); }
                let retained = registration.file.lock().map_err(|_| host("Process registry is poisoned"))?;
                let retained = retained.as_ref().ok_or_else(|| host("Process executable registry is closed"))?;
                inventory.pending = Some(duplicate(retained.as_raw_fd()).map_err(host)?);
                leaf.descriptor.close().map_err(host)?;
                leaf.descriptor = inventory.pending.take().unwrap();
                leaf.digest = registration.digest.clone();
                group += 1;
            }
            if retain_entry {
                let start = inventory.resources.len();
                entry.duplicate_witnesses(|| context.step(), |path, id, file| {
                    inventory.resources.push(Resource {
                        kind: if path == Path::new(entry_path) { Kind::ProgramEntry } else { Kind::Ancestor },
                        group, path: path.to_str().unwrap().into(),
                        file: FileIdentity { device: id.0, inode: id.1 },
                        endpoint: FileIdentity::default(), digest: String::new(),
                        descriptor: unsafe { Descriptor::take(file.into_raw_fd()) },
                    });
                })?;
                inventory.validate_group(start, inventory.resources.len() - 1, &mut || context.step(), false)?;
                group += 1;
            }
            for (kind, paths) in [(Kind::Read, &processes.roots), (Kind::Write, &scope.writes), (Kind::Prune, &scope.prunes), (Kind::Endpoint, &scope.endpoints)] {
                for path in paths { inventory.capture_path(path, kind, group, || context.step())?; group += 1; }
            }
            let locks = context.session.locks.clone();
            let (freeze, leases) = locks.freeze_remote(generation).map_err(host)?;
            inventory.remote = Some((locks, freeze));
            for lease in leases {
                inventory.resources.push(Resource { kind: Kind::Lock, group: LOCK_GROUP, path: String::new(),
                    file: FileIdentity { device: lease.identity.0, inode: lease.identity.1 }, endpoint: FileIdentity::default(), digest: String::new(),
                    descriptor: unsafe { Descriptor::take(lease.file.into_raw_fd()) } });
            }
            inventory.expected = inventory.resources.len();
            inventory.validate(|| context.step())?;
            Ok(())
        })();
        if let Err(primary) = result {
            let close = inventory.close();
            // No descriptor was transferred and no command can exist yet.
            let release = if close.is_ok() { inventory.release_remote(RemoteReleaseProof(())) } else { Ok(()) };
            if close.is_ok() && release.is_ok() { context.values.release_temporary(storage); }
            let cleanup = close.err().or_else(|| release.err()).map(host);
            return Err(crate::context::combine_cleanup(Some(primary), cleanup).unwrap());
        }
        Ok(inventory)
    }
    fn capture_path(&mut self, path: &str, kind: Kind, group: u32, mut check: impl FnMut() -> Result<(), Fault>) -> Result<(), Fault> {
        if !clean(path) { return Err(host("Process resource path is not canonical")); }
        let selected = Path::new(path);
        let mut ancestors: Vec<_> = selected.ancestors().skip(1).collect();
        ancestors.reverse();
        let start = self.resources.len();
        for ancestor in ancestors {
            check()?;
            let parent = self.resources.last().filter(|r| r.group == group).map(|r| r.descriptor.raw());
            let name = ancestor.file_name().map_or("/", |name| name.to_str().unwrap());
            let descriptor = open_at(parent, name, true).map_err(host)?;
            self.resources.push(Resource { kind: Kind::Ancestor, group, path: ancestor.to_str().unwrap().into(), file: FileIdentity::default(),
                endpoint: FileIdentity::default(), digest: String::new(), descriptor });
            let record = self.resources.last_mut().unwrap();
            record.file = identity(record.descriptor.raw()).map_err(host)?;
        }
        check()?;
        let parent = self.resources.last().filter(|r| r.group == group).map(|r| r.descriptor.raw());
        let name = selected.file_name().map_or("/", |name| name.to_str().unwrap());
        let (descriptor, endpoint) = if kind == Kind::Endpoint {
            let parent = parent.ok_or_else(|| host("Process endpoint requires a containing directory"))?;
            let observed = std::fs::symlink_metadata(path).map_err(|_| host("Process endpoint is unavailable"))?;
            if !observed.file_type().is_socket() { return Err(host("Process endpoint requires a socket")); }
            let endpoint = named_identity(parent, name).map_err(host)?;
            if endpoint != (FileIdentity { device: observed.dev(), inode: observed.ino() }) { return Err(host("Process endpoint identity changed")); }
            (duplicate(parent).map_err(host)?, endpoint)
        } else { (open_at(parent, name, !kind.image()).map_err(host)?, FileIdentity::default()) };
        self.resources.push(Resource { kind, group, path: path.into(), file: FileIdentity::default(), endpoint, digest: String::new(), descriptor });
        let record = self.resources.last_mut().unwrap();
        record.file = identity(record.descriptor.raw()).map_err(host)?;
        self.validate_group(start, self.resources.len() - 1, &mut check, false)
    }
    pub fn len(&self) -> usize { self.resources.len() }
    pub fn storage(&self) -> u64 { self.storage }
    pub fn frame(&self, index: usize) -> Result<(Vec<u8>, RawFd), &'static str> {
        if self.closed { return Err("Process resource inventory is closed"); }
        let resource = self.resources.get(index).ok_or("Process resource index is absent")?;
        let mut output = Encoder::new();
        output.u32(resource.kind as u32)?; output.u32(resource.group)?;
        output.text(&resource.path)?; output.u64(resource.file.device)?; output.u64(resource.file.inode)?;
        output.u64(resource.endpoint.device)?; output.u64(resource.endpoint.inode)?; output.text(&resource.digest)?;
        Ok((output.finish(), resource.descriptor.raw()))
    }
    pub fn receiving(expected_count: usize, advertised_storage: u64) -> Result<Self, &'static str> {
        if advertised_storage < storage_bound(expected_count)? || advertised_storage > storage_bound(MAX_RESOURCES)? {
            return Err("Process resource storage differs from the admitted inventory");
        }
        let mut resources = Vec::new();
        resources.try_reserve_exact(expected_count).map_err(|_| "Process resource storage is unavailable")?;
        Ok(Self { resources, expected: expected_count, storage: advertised_storage,
            remote: None, closed: false, close_error: None, pending: None })
    }
    pub fn receive(&mut self, body: &[u8], descriptor: Descriptor) -> Result<(), &'static str> {
        if self.closed {
            let mut descriptor = descriptor;
            if let Err(error) = descriptor.close() { self.record_close_error(error); }
            return Err("Process resource inventory is closed");
        }
        // Own a received descriptor before parsing any untrusted payload. The
        // bounded pending slot retains rejected input for explicit cleanup.
        let rejected = self.pending.is_some();
        if let Some(mut previous) = self.pending.replace(descriptor) {
            if let Err(error) = previous.close() { self.record_close_error(error); }
        }
        if rejected || self.resources.len() == self.expected { return Err("Process resource inventory has an unexpected descriptor"); }
        let mut input = Decoder::new(body);
        let kind = Kind::decode(input.u32()?)?;
        let group = input.u32()?;
        let path = input.text()?.to_owned();
        let file = FileIdentity { device: input.u64()?, inode: input.u64()? };
        let endpoint = FileIdentity { device: input.u64()?, inode: input.u64()? };
        let digest = input.text()?.to_owned();
        input.finish()?;
        if file == FileIdentity::default() || (kind != Kind::Lock && !clean(&path))
            || (kind == Kind::Lock && (!path.is_empty() || group != LOCK_GROUP))
            || (kind != Kind::Lock && group == LOCK_GROUP)
            || (kind.image() && (digest.len() != 64 || !digest.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))))
            || (!kind.image() && !digest.is_empty())
            || (kind == Kind::Endpoint) != (endpoint != FileIdentity::default()) {
            return Err("Process resource record is invalid");
        }
        if identity(self.pending.as_ref().unwrap().raw())? != file { return Err("Process transferred descriptor identity differs"); }
        let descriptor = self.pending.take().unwrap();
        self.resources.push(Resource { kind, group, path, file, endpoint, digest, descriptor });
        Ok(())
    }
    fn validate_chain(&self, start: usize, end: usize, check: &mut impl FnMut() -> Result<(), Fault>) -> Result<(), Fault> {
        let leaf = &self.resources[end];
        if matches!(leaf.kind, Kind::Ancestor | Kind::Lock) { return Err(host("Process resource group has no subject")); }
        let mut expected = PathBuf::from("/");
        for (offset, record) in self.resources[start..=end].iter().enumerate() {
            check()?;
            if record.group != leaf.group || (start + offset != end && record.kind != Kind::Ancestor)
                || record.path != expected.to_str().unwrap() || identity(record.descriptor.raw()).map_err(host)? != record.file {
                return Err(host("Process retained resource chain differs"));
            }
            let physical = borrowed(record.descriptor.raw()).metadata().map_err(|_| host("Process resource metadata is unavailable"))?;
            if if record.kind.regular() { !physical.is_file() } else { !physical.is_dir() } {
                return Err(host("Process resource descriptor has the wrong kind"));
            }
            if start + offset == end { break; }
            let next = &self.resources[start + offset + 1];
            if Path::new(&next.path).parent() != Some(Path::new(&record.path)) { return Err(host("Process resource ancestor is missing")); }
            let name = Path::new(&next.path).file_name().and_then(|s| s.to_str()).ok_or_else(|| host("Process resource basename is absent"))?;
            let actual = named_identity(record.descriptor.raw(), name).map_err(host)?;
            let selected = if next.kind == Kind::Endpoint { next.endpoint } else { next.file };
            if actual != selected { return Err(host("Process resource namespace identity changed")); }
            expected.push(name);
        }
        // Anchor the retained root to the current namespace on every check.
        let mut root = open_at(None, "/", true).map_err(host)?;
        let observed = identity(root.raw()).map_err(host);
        let closed = root.close().map_err(host);
        let observed = match observed { Ok(value) => { closed?; value }, Err(primary) => return Err(crate::context::combine_cleanup(Some(primary), closed.err()).unwrap()) };
        if observed != self.resources[start].file { return Err(host("Process resource namespace root changed")); }
        Ok(())
    }
    fn validate_group(&self, start: usize, end: usize, check: &mut impl FnMut() -> Result<(), Fault>, hash: bool) -> Result<(), Fault> {
        self.validate_chain(start, end, check)?;
        let leaf = &self.resources[end];
        if leaf.kind == Kind::Endpoint {
            let endpoint = std::fs::symlink_metadata(&leaf.path).map_err(|_| host("Process endpoint is unavailable"))?;
            if !endpoint.file_type().is_socket() || (FileIdentity { device: endpoint.dev(), inode: endpoint.ino() }) != leaf.endpoint {
                return Err(host("Process endpoint identity changed"));
            }
        }
        if hash && leaf.kind.image() && digest(leaf.descriptor.raw(), check)? != leaf.digest { return Err(host("Process executable digest changed")); }
        if leaf.kind == Kind::Endpoint || (hash && leaf.kind.image()) {
            // Recheck namespace witnesses after endpoint observation or hashing.
            self.validate_chain(start, end, check)?;
        }
        Ok(())
    }
    pub fn validate(&self, mut check: impl FnMut() -> Result<(), Fault>) -> Result<(), Fault> {
        if self.closed || self.pending.is_some() || self.close_error.is_some() || self.resources.len() != self.expected { return Err(host("Process resource inventory is incomplete")); }
        let mut start = 0; let mut group = 0; let mut locks = false; let mut images = 0; let mut launchers = 0;
        let mut lock_count = 0; let mut reads = 0; let mut mutations = 0; let mut endpoints = 0; let mut entries = 0;
        for (index, resource) in self.resources.iter().enumerate() {
            check()?;
            if resource.kind == Kind::Lock {
                locks = true;
                lock_count += 1;
                if lock_count > crate::locks::MAX_LOCKS { return Err(host("Process lock inventory exceeds its bound")); }
                if start != index || identity(resource.descriptor.raw()).map_err(host)? != resource.file
                    || !borrowed(resource.descriptor.raw()).metadata().map_err(|_| host("Process lock metadata is unavailable"))?.is_file() {
                    return Err(host("Process lock lease transfer differs"));
                }
                start = index + 1; continue;
            }
            if locks || resource.group != group { return Err(host("Process resource group order differs")); }
            if resource.kind != Kind::Ancestor {
                self.validate_group(start, index, &mut check, true)?;
                if resource.kind == Kind::Launcher { launchers += 1; }
                if resource.kind == Kind::Image { images += 1; }
                match resource.kind { Kind::Read => reads += 1, Kind::Write | Kind::Prune => mutations += 1, Kind::Endpoint => endpoints += 1, Kind::ProgramEntry => entries += 1, _ => () }
                if images > super::MAX_POLICY_ENTRIES || launchers > 1 || entries > 1 || reads > super::MAX_POLICY_ENTRIES
                    || mutations > super::MAX_POLICY_ENTRIES || endpoints > super::MAX_POLICY_ENTRIES {
                    return Err(host("Process resource subjects exceed the admitted policy bounds"));
                }
                if resource.kind.image() {
                    for other in &self.resources[..start] {
                        check()?;
                        if other.kind.image() && other.path == resource.path {
                            return Err(host("Process executable registry contains a duplicate"));
                        }
                    }
                }
                start = index + 1; group += 1;
            }
        }
        if start != self.resources.len() || images == 0 || launchers != 1 { return Err(host("Process executable registry is incomplete")); }
        Ok(())
    }
    pub fn images(&self) -> Vec<ownership::Image> {
        self.resources.iter().filter(|resource| resource.kind.image()).map(|resource| ownership::Image {
            path: resource.path.clone(), file: resource.file, launch_only: resource.kind == Kind::Launcher }).collect()
    }
    pub fn validate_image(&self, path: &str, file: FileIdentity, mut check: impl FnMut() -> Result<(), Fault>) -> Result<(), Fault> {
        if self.closed { return Err(host("Process executable inventory is closed")); }
        let mut start = 0;
        for (end, resource) in self.resources.iter().enumerate() {
            check()?;
            if resource.kind.image() && resource.path == path && resource.file == file {
                return self.validate_group(start, end, &mut check, true);
            }
            if resource.kind != Kind::Ancestor { start = end + 1; }
        }
        Err(host("Process executable is not registered"))
    }
    fn record_close_error(&mut self, error: &str) {
        match &mut self.close_error {
            Some(previous) => { previous.push_str("; "); previous.push_str(error); }
            None => self.close_error = Some(error.into()),
        }
    }
    pub fn close(&mut self) -> Result<(), String> {
        if !self.closed {
            let mut errors = Vec::new();
            for resource in &mut self.resources { if let Err(error) = resource.descriptor.close() { errors.push(error); } }
            if let Some(mut pending) = self.pending.take() { if let Err(error) = pending.close() { errors.push(error); } }
            self.closed = true;
            for error in errors { self.record_close_error(error); }
        }
        self.close_error.clone().map_or(Ok(()), Err)
    }
    pub fn release_remote(&mut self, _proof: RemoteReleaseProof) -> Result<(), String> {
        if !self.closed || self.close_error.is_some() { return Err("Process resource closure is not proved".into()); }
        if let Some((locks, freeze)) = self.remote.take() { locks.release_remote(freeze)?; }
        Ok(())
    }
}
