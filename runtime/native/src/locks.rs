use crate::{context::*, value::*};
use serde_json::{json, Value as Json};
use std::{
    collections::BTreeMap,
    ffi::CString,
    fs::File,
    io,
    os::{
        fd::{AsRawFd, FromRawFd, IntoRawFd},
        unix::fs::MetadataExt,
    },
    path::{Component, Path, PathBuf},
    sync::{Mutex, TryLockError},
    time::Duration,
};

pub const OPERATION: &str = "Lock.File.new";
pub const MAX_LOCKS: usize = 128;

#[derive(Clone)]
pub struct Value {
    path: Option<Handle>,
}
impl Value {
    pub fn weight(&self) -> u64 {
        self.path.as_ref().map_or(0, |path| path.weight)
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        None
    }
    pub fn retained_children(&self) -> &[Handle] {
        self.path.as_slice()
    }
    pub fn json(&self) -> Json {
        json!({"redacted":true})
    }
    pub fn canonical_json(&self) -> String {
        "{\"redacted\":true}".into()
    }
    pub fn snapshot(&self) -> Self {
        Self { path: None }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub struct Identity(pub u64, pub u64);
pub fn identity(file: &File) -> Result<Identity, String> {
    let metadata = file.metadata().map_err(|e| e.to_string())?;
    Ok(Identity(metadata.dev(), metadata.ino()))
}
pub fn close_file(file: File) -> Result<(), String> {
    if unsafe { libc::close(file.into_raw_fd()) } == 0 {
        Ok(())
    } else {
        Err(io::Error::last_os_error().to_string())
    }
}
fn opened(fd: i32) -> Result<File, String> {
    if fd < 0 {
        Err(io::Error::last_os_error().to_string())
    } else {
        Ok(unsafe { File::from_raw_fd(fd) })
    }
}
fn name(path: &std::ffi::OsStr) -> Result<CString, String> {
    use std::os::unix::ffi::OsStrExt;
    CString::new(path.as_bytes()).map_err(|_| "lock path contains NUL".into())
}
pub fn absolute(input: &Path) -> Result<PathBuf, String> {
    let path = if input.is_absolute() {
        input.to_owned()
    } else {
        std::env::current_dir()
            .map_err(|e| e.to_string())?
            .join(input)
    };
    let mut result = PathBuf::new();
    for component in path.components() {
        match component {
            Component::RootDir => result.push("/"),
            Component::CurDir => (),
            Component::ParentDir => {
                result.pop();
            }
            Component::Normal(part) => result.push(part),
            _ => return Err("invalid lock path".into()),
        }
    }
    if result.to_str().is_none() {
        return Err("lock path requires UTF-8".into());
    }
    Ok(result)
}

pub struct Grant {
    pub path: PathBuf,
    pub file: Option<File>,
    parent: Option<File>,
    id: Identity,
    parent_id: Identity,
    anchors: Vec<(File, CString)>,
}
enum AttemptFailure {
    Access,
    Cleanup(String),
}
impl Grant {
    pub fn bind(input: &Path) -> Result<Self, String> {
        Self::bind_attempt(input).map_err(|failure| match failure {
            AttemptFailure::Access => "lock file admission failed".into(),
            AttemptFailure::Cleanup(error) => error,
        })
    }
    fn bind_attempt(input: &Path) -> Result<Self, AttemptFailure> {
        let path = absolute(input).map_err(|_| AttemptFailure::Access)?;
        if !input.is_absolute() || input.as_os_str() != path.as_os_str() {
            return Err(AttemptFailure::Access);
        }
        let parts: Vec<_> = path
            .components()
            .filter_map(|part| match part {
                Component::Normal(name) => Some(name),
                _ => None,
            })
            .collect();
        if parts.len() > 256 || path.as_os_str().len() > 4096 {
            return Err(AttemptFailure::Access);
        }
        let Some((last, parents)) = parts.split_last() else {
            return Err(AttemptFailure::Access);
        };
        let mut grant = Self {
            path: path.clone(),
            file: None,
            parent: None,
            id: Identity(0, 0),
            parent_id: Identity(0, 0),
            anchors: vec![],
        };
        let result = (|| -> Result<(), String> {
            let flags = libc::O_RDONLY | libc::O_CLOEXEC | libc::O_NOFOLLOW | libc::O_NONBLOCK;
            grant.parent = Some(opened(unsafe {
                libc::open(c"/".as_ptr(), flags | libc::O_DIRECTORY)
            })?);
            for part in parents {
                let name = name(part)?;
                let next = opened(unsafe {
                    libc::openat(
                        grant.parent.as_ref().unwrap().as_raw_fd(),
                        name.as_ptr(),
                        flags | libc::O_DIRECTORY,
                    )
                })?;
                grant
                    .anchors
                    .push((grant.parent.replace(next).unwrap(), name));
            }
            let file = opened(unsafe {
                libc::openat(
                    grant.parent.as_ref().unwrap().as_raw_fd(),
                    name(last)?.as_ptr(),
                    flags,
                )
            })?;
            grant.file = Some(file);
            let file = grant.file.as_ref().unwrap();
            if !file.metadata().map_err(|e| e.to_string())?.is_file() {
                return Err("lock requires existing regular file".into());
            }
            grant.id = identity(file)?;
            grant.parent_id = identity(grant.parent.as_ref().unwrap())?;
            Ok(())
        })();
        if let Err(primary) = result {
            return Err(match grant.close() {
                Ok(()) => AttemptFailure::Access,
                Err(cleanup) => AttemptFailure::Cleanup(format!("{primary}; cleanup: {cleanup}")),
            });
        }
        Ok(grant)
    }
    fn fresh(&self) -> Result<File, AttemptFailure> {
        let mut current = Self::bind_attempt(&self.path)?;
        let matched = self.binding_matches(&current).unwrap_or(false);
        let file = current.file.take().unwrap();
        let cleanup = current.close();
        if !matched || cleanup.is_err() {
            let close = close_file(file);
            let errors = [cleanup.err(), close.err()]
                .into_iter()
                .flatten()
                .collect::<Vec<_>>();
            return Err(if errors.is_empty() {
                AttemptFailure::Access
            } else {
                AttemptFailure::Cleanup(errors.join("; "))
            });
        }
        Ok(file)
    }
    fn verify(&self, file: &File) -> Result<(), AttemptFailure> {
        if identity(file).map_err(|_| AttemptFailure::Access)? != self.id {
            return Err(AttemptFailure::Access);
        }
        let mut current = Self::bind_attempt(&self.path)?;
        let matched = self.binding_matches(&current).unwrap_or(false);
        current.close().map_err(AttemptFailure::Cleanup)?;
        if matched {
            Ok(())
        } else {
            Err(AttemptFailure::Access)
        }
    }
    fn binding_matches(&self, current: &Self) -> Result<bool, String> {
        if current.id != self.id
            || current.parent_id != self.parent_id
            || current.anchors.len() != self.anchors.len()
        {
            return Ok(false);
        }
        for ((original, original_name), (fresh, fresh_name)) in
            self.anchors.iter().zip(&current.anchors)
        {
            if original_name != fresh_name || identity(original)? != identity(fresh)? {
                return Ok(false);
            }
        }
        Ok(true)
    }
    pub fn close(&mut self) -> Result<(), String> {
        let mut failures = vec![];
        for file in [self.file.take(), self.parent.take()].into_iter().flatten() {
            if let Err(error) = close_file(file) {
                failures.push(error);
            }
        }
        for (file, _) in self.anchors.drain(..) {
            if let Err(error) = close_file(file) {
                failures.push(error);
            }
        }
        if failures.is_empty() {
            Ok(())
        } else {
            Err(failures.join("; "))
        }
    }
}

#[derive(Default)]
struct State {
    grants: Vec<Grant>,
    leases: Vec<(Identity, File)>,
    closing: bool,
    closed: bool,
    error: Option<String>,
}
pub struct Locks {
    state: Mutex<State>,
}
impl Locks {
    pub fn empty() -> Self {
        Self {
            state: Mutex::new(State::default()),
        }
    }
    pub fn configure(paths: &[String]) -> Result<Self, String> {
        let locks = Self::empty();
        if paths.len() > MAX_LOCKS {
            return Err("lock grants exceed 128".into());
        }
        let result = (|| {
            let mut state = locks.state.lock().unwrap();
            for path in paths {
                let mut grant = Grant::bind(Path::new(path))?;
                if state.grants.iter().any(|other| other.path == grant.path) {
                    grant.close()?;
                    return Err("duplicate lock grant".into());
                }
                state.grants.push(grant);
            }
            Ok(())
        })();
        if let Err(error) = result {
            return Err(match locks.close() {
                Ok(()) => error,
                Err(cleanup) => format!("{error}; cleanup: {cleanup}"),
            });
        }
        Ok(locks)
    }
    pub fn configure_with_entry(
        paths: &[String],
        entry: Option<&crate::program_entry::Entry>,
    ) -> Result<Self, String> {
        let locks = Self::configure(paths)?;
        let validation = (|| {
            if let Some(entry) = entry {
                let state = locks.state.lock().unwrap();
                if let Ok(path) = &entry.path {
                    if let Some(grant) = state
                        .grants
                        .iter()
                        .find(|grant| grant.path.to_str() == Some(path.as_str()))
                    {
                        if grant.id != entry.expected_identity()? {
                            return Err("entry lock identity differs from captured entry".into());
                        }
                    }
                }
            }
            Ok(())
        })();
        if let Err(error) = validation {
            return Err(match locks.close() {
                Ok(()) => error,
                Err(cleanup) => format!("{error}; cleanup: {cleanup}"),
            });
        }
        Ok(locks)
    }
    pub fn seal(&self) {
        self.state.lock().unwrap().closing = true;
    }
    pub fn close(&self) -> Result<(), String> {
        let mut state = self.state.lock().unwrap();
        if state.closed {
            return state.error.clone().map_or(Ok(()), Err);
        }
        state.closing = true;
        let mut errors = vec![];
        for (_, file) in state.leases.drain(..) {
            if let Err(error) = close_file(file) {
                errors.push(format!("close lock lease: {error}"));
            }
        }
        for mut grant in state.grants.drain(..) {
            if let Err(error) = grant.close() {
                errors.push(format!("close lock grant: {error}"));
            }
        }
        state.closed = true;
        if !errors.is_empty() {
            state.error = Some(errors.join("; "));
        }
        state.error.clone().map_or(Ok(()), Err)
    }
    fn acquire(&self, context: &Context, site: &Site, path: &str) -> Outcome {
        let mut state = loop {
            context.check_cancel()?;
            match self.state.try_lock() {
                Ok(state) => break state,
                Err(TryLockError::WouldBlock) => std::thread::sleep(Duration::from_millis(1)),
                Err(_) => return Err(host("lock ownership is poisoned")),
            }
        };
        if state.closing {
            return Err(host("lock session is closing"));
        }
        let grant = state
            .grants
            .iter()
            .find(|grant| grant.path.to_str() == Some(path))
            .ok_or_else(|| {
                Fault::Host(
                    "runtime.capability-denied",
                    "Lock.acquire requires exact lock authority".into(),
                )
            })?;
        if state.leases.len() >= MAX_LOCKS {
            return Err(limit("lock leases"));
        }
        let file = grant
            .fresh()
            .map_err(|failure| attempt_fault(site, failure))?;
        let result = unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) };
        let failure = if result != 0 {
            let error_code = io::Error::last_os_error().raw_os_error();
            Some(Fault::Recoverable(error(
                site,
                if error_code == Some(libc::EWOULDBLOCK) {
                    "Lock.Busy"
                } else {
                    "Lock.Failed"
                },
                if error_code == Some(libc::EWOULDBLOCK) {
                    "file lock is busy"
                } else {
                    "file lock acquisition failed"
                },
                record([]),
            )))
        } else {
            context.check_cancel().err().or_else(|| {
                grant
                    .verify(&file)
                    .err()
                    .map(|failure| attempt_fault(site, failure))
            })
        };
        if let Some(failure) = failure {
            return Err(match close_file(file) {
                Ok(()) => failure,
                Err(cleanup) => {
                    let terminal = if matches!(&failure, Fault::Recoverable(_)) {
                        host(failure.message())
                    } else {
                        failure
                    };
                    combine_cleanup(
                        Some(terminal),
                        Some(host(format!("close lock attempt: {cleanup}"))),
                    )
                    .unwrap()
                }
            });
        }
        let id = grant.id;
        state.leases.push((id, file));
        Ok(new(
            atom("Lock.Lease"),
            Data::Adapter(crate::adapters::AdapterValue::Lock(Value { path: None })),
        ))
    }
}
impl Drop for Locks {
    fn drop(&mut self) {
        let _ = self.close();
    }
}

fn attempt_fault(site: &Site, failure: AttemptFailure) -> Fault {
    match failure {
        AttemptFailure::Access => Fault::Recoverable(error(
            site,
            "Lock.Failed",
            "file lock acquisition failed",
            record([]),
        )),
        AttemptFailure::Cleanup(error) => {
            host(format!("close lock acquisition resources: {error}"))
        }
    }
}
pub fn admit(site: &Site) -> Result<(), String> {
    if site.arguments.len() != 1
        || !matches!(&site.arguments[0],Argument::Value(typ) if *typ==atom("File.Path"))
        || site.typ != atom("Lock.File")
    {
        return Err("Lock.File metadata mismatch".into());
    }
    Ok(())
}
pub fn execute(_context: &mut Context, _site: &Site, args: &[Handle]) -> Outcome {
    Ok(new(
        atom("Lock.File"),
        Data::Adapter(crate::adapters::AdapterValue::Lock(Value {
            path: Some(args[0].clone()),
        })),
    ))
}
pub fn commit(context: &mut Context, site: &Site, target: &Handle) -> Outcome {
    let Data::Adapter(crate::adapters::AdapterValue::Lock(value)) = &target.data else {
        return Err(host("invalid lock target"));
    };
    let Some(path) = &value.path else {
        return Err(Fault::Host(
            "runtime.capability-denied",
            "lock snapshot is not admissible".into(),
        ));
    };
    let mut activation = site.clone();
    activation.operation = "Lock.acquire".into();
    let receipt = context
        .session
        .locks
        .acquire(context, &activation, path.string())?;
    context.last_activation = Some(
        json!({"operation":"Lock.acquire","span":site.source_record(),"result_type":"Lock.Lease"}),
    );
    context.charge(&receipt)?;
    Ok(receipt)
}
