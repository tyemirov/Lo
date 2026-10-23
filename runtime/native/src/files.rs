use crate::{context::*, value::*};
use std::{
    collections::{BTreeMap, BTreeSet, VecDeque},
    ffi::{CStr, CString, OsString},
    fs::File,
    io::{self, Read, Write},
    os::{
        fd::{AsRawFd, FromRawFd, IntoRawFd},
        unix::{
            ffi::{OsStrExt, OsStringExt},
            fs::MetadataExt,
        },
    },
    path::{Component, Path, PathBuf},
};

#[path = "file_entries.rs"]
mod entries;
#[path = "file_probe.rs"]
mod probe;
pub use entries::Value;
pub const SUPPORTED: &[&str] = &[
    "File.read",
    "File.Write.new",
    "File.Append.new",
    "File.list",
    "File.open_paths",
    "File.Remove.new",
];
const MAX_FILE_ENTRIES: usize = 10_000;
const MAX_FILE_DIRECTORIES: usize = 128;
static SESSION: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1);
pub fn admit(site: &Site) -> Result<(), String> {
    let args: Vec<_> = site
        .arguments
        .iter()
        .filter_map(|argument| {
            if let Argument::Value(typ) = argument {
                Some(typ.clone())
            } else {
                None
            }
        })
        .collect();
    let valid = match site.operation.as_str() {
        "File.read" => {
            args == [atom("File.Path")] && site.typ == Type::Fallible(Box::new(atom("Bytes")))
        }
        "File.Write.new" | "File.Append.new" => {
            args == [atom("File.Path"), atom("Bytes")]
                && site.typ == atom(site.operation.trim_end_matches(".new"))
        }
        "File.list" => {
            args == [atom("File.Path")]
                && site.typ == Type::Fallible(Box::new(Type::List(Box::new(atom("File.Entry")))))
        }
        "File.open_paths" => {
            args == [atom("File.Path")]
                && site.typ == Type::Fallible(Box::new(Type::List(Box::new(atom("File.Path")))))
        }
        "File.Remove.new" => {
            args == [Type::List(Box::new(atom("File.Entry")))]
                && site.typ == Type::Fallible(Box::new(atom("File.Remove")))
        }
        _ => false,
    };
    if valid && args.len() == site.arguments.len() {
        Ok(())
    } else {
        Err("file operation metadata type mismatch".into())
    }
}
#[cfg(target_os = "macos")]
const FLAGS: [i32; 6] = [0x100, 0x100000, 0x1000000, 0x4, 0x200, 0x400];
#[cfg(target_os = "linux")]
const FLAGS: [i32; 6] = [0x8000, 0x4000, 0x80000, 0x800, 0x40, 0x200];
#[cfg(target_os = "macos")]
type PollCount = u32;
#[cfg(target_os = "linux")]
type PollCount = usize;
extern "C" {
    fn openat(fd: i32, path: *const std::os::raw::c_char, flags: i32, ...) -> i32;
    fn readlinkat(
        fd: i32,
        path: *const std::os::raw::c_char,
        output: *mut u8,
        size: usize,
    ) -> isize;
    fn close(fd: i32) -> i32;
    fn strerror(error: i32) -> *const std::os::raw::c_char;
    fn fcntl(fd: i32, command: i32, ...) -> i32;
    fn poll(descriptors: *mut PollFD, count: PollCount, timeout: i32) -> i32;
}

#[repr(C)]
struct PollFD {
    fd: i32,
    events: i16,
    returned: i16,
}
fn wait_file(
    context: &Context,
    file: &File,
    events: i16,
    failure: impl Fn(io::Error) -> Fault,
) -> Result<(), Fault> {
    loop {
        context.check_cancel()?;
        let mut descriptor = PollFD {
            fd: file.as_raw_fd(),
            events,
            returned: 0,
        };
        let result = unsafe { poll(&mut descriptor, 1, 50) };
        context.check_cancel()?;
        if result < 0 {
            let error = io::Error::last_os_error();
            if error.kind() == io::ErrorKind::Interrupted {
                continue;
            }
            return Err(failure(error));
        }
        if descriptor.returned != 0 {
            return Ok(());
        }
    }
}
fn nonblocking(file: &File) -> io::Result<()> {
    let flags = unsafe { fcntl(file.as_raw_fd(), 3) };
    if flags < 0 || unsafe { fcntl(file.as_raw_fd(), 4, flags | FLAGS[3]) } < 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}
struct Grant {
    path: PathBuf,
    directory: File,
    inert: bool,
}
pub struct Files {
    base: PathBuf,
    reads: Vec<Grant>,
    writes: Vec<Grant>,
    lists: Vec<Grant>,
    removes: Vec<Grant>,
    open_files: Vec<Grant>,
    observed: BTreeMap<entries::Directory, File>,
    session: u64,
    probe: Option<probe::Probe>,
    last_removal: Option<(Handle, Handle)>,
    write_progress: std::cell::RefCell<Option<Handle>>,
}
#[derive(Default)]
pub struct Options {
    pub reads: Vec<String>,
    pub writes: Vec<String>,
    pub lists: Vec<String>,
    pub removes: Vec<String>,
    pub open_files: Vec<String>,
}
pub fn empty() -> Files {
    Files {
        base: PathBuf::new(),
        reads: vec![],
        writes: vec![],
        lists: vec![],
        removes: vec![],
        open_files: vec![],
        observed: BTreeMap::new(),
        session: SESSION.fetch_add(1, std::sync::atomic::Ordering::Relaxed),
        probe: None,
        last_removal: None,
        write_progress: std::cell::RefCell::new(None),
    }
}

fn clean(path: &Path) -> PathBuf {
    let mut result = PathBuf::new();
    for part in path.components() {
        match part {
            Component::CurDir => (),
            Component::ParentDir => {
                result.pop();
            }
            _ => result.push(part.as_os_str()),
        }
    }
    result
}
fn quote(path: &Path) -> String {
    crate::checkpoint_print::quote(&path.to_string_lossy())
}
fn system_error(error: &io::Error) -> String {
    match error.raw_os_error() {
        Some(code) => unsafe {
            CStr::from_ptr(strerror(code))
                .to_string_lossy()
                .to_lowercase()
        },
        None => error.to_string(),
    }
}
fn close_file(file: File) -> io::Result<()> {
    if unsafe { close(file.into_raw_fd()) } == 0 {
        Ok(())
    } else {
        Err(io::Error::last_os_error())
    }
}
fn cleanup_failure(primary: Fault, cleanup: io::Error) -> Fault {
    let message = system_error(&cleanup);
    match primary {
        Fault::RawCancellation => Fault::Host(
            "runtime.cancelled",
            format!("execution cancelled: context canceled\n{message}"),
        ),
        Fault::Budget {
            primary,
            message: original,
        } => Fault::Budget {
            primary,
            message: format!("{original}\n{message}"),
        },
        Fault::Host(code, primary) => Fault::Host(code, format!("{primary}\n{message}")),
        Fault::Recoverable(value) => {
            let Data::Error(cause) = &value.data else {
                return host("invalid file failure");
            };
            let mut cause = cause.clone();
            let mut fields = cause.input.fields().clone();
            fields.insert(
                "cleanup_errors".into(),
                new(
                    Type::List(Box::new(atom("String"))),
                    Data::List(vec![text(&message)]),
                ),
            );
            cause.input = view(&record(fields));
            Fault::Recoverable(new(atom("Error"), Data::Error(cause)))
        }
        Fault::Numeric(kind, primary) => Fault::Numeric(kind, format!("{primary}\n{message}")),
        Fault::Boundary {
            primary,
            mut details,
        } => {
            details.cleanup_errors.push(message);
            Fault::Boundary { primary, details }
        }
    }
}
fn open_roots(base: &Path, roots: &[String]) -> Result<Vec<Grant>, String> {
    let mut grants: Vec<Grant> = vec![];
    for input in roots {
        let path = clean(&base.join(input));
        if grants.iter().any(|grant| grant.path == path) {
            return Err(format!("duplicate adapter root {}", quote(&path)));
        }
        let directory = File::open(&path)
            .map_err(|e| format!("open adapter root {}: {}", quote(&path), system_error(&e)))?;
        if !directory.metadata().map_err(|e| e.to_string())?.is_dir() {
            return Err(format!(
                "open adapter root {}: not a directory",
                quote(&path)
            ));
        }
        grants.push(Grant {
            path,
            directory,
            inert: false,
        });
    }
    grants.sort_by_key(|grant| std::cmp::Reverse(grant.path.as_os_str().len()));
    Ok(grants)
}
pub fn configure_options(options: &Options) -> Result<Files, String> {
    let base = std::env::current_dir().map_err(|e| format!("resolve execution directory: {e}"))?;
    let mut files = empty();
    files.base = base;
    let configured = (|| {
        files.reads = open_roots(&files.base, &options.reads)?;
        files.writes = open_roots(&files.base, &options.writes)?;
        files.lists = entries::open_exact_roots(&files.base, &options.lists)?;
        files.removes = entries::open_exact_roots(&files.base, &options.removes)?;
        files.open_files = entries::open_exact_roots(&files.base, &options.open_files)?;
        if !files.open_files.is_empty() {
            files.probe = Some(probe::Probe::bind());
        }
        Ok::<_, String>(())
    })();
    if let Err(mut error) = configured {
        if let Err(cleanup) = files.close() {
            error.push('\n');
            error.push_str(&cleanup)
        }
        return Err(error);
    }
    Ok(files)
}
impl Files {
    pub fn last_removal(&self) -> Option<(Handle, Handle)> {
        self.last_removal.clone()
    }

    pub fn fork(&self) -> Result<Self, String> {
        fn roots(source: &[Grant]) -> Result<Vec<Grant>, String> {
            let mut result = Vec::new();
            for grant in source {
                match grant.directory.try_clone() {
                    Ok(directory) => result.push(Grant {
                        path: grant.path.clone(),
                        directory,
                        inert: grant.inert,
                    }),
                    Err(error) => {
                        let mut message = error.to_string();
                        for grant in result {
                            if let Err(cleanup) = close_file(grant.directory) {
                                message.push('\n');
                                message.push_str(&system_error(&cleanup));
                            }
                        }
                        return Err(message);
                    }
                }
            }
            Ok(result)
        }
        let mut fork = empty();
        fork.base = self.base.clone();
        let result = (|| {
            fork.reads = roots(&self.reads)?;
            fork.writes = roots(&self.writes)?;
            fork.lists = roots(&self.lists)?;
            fork.removes = roots(&self.removes)?;
            fork.open_files = roots(&self.open_files)?;
            fork.probe = self.probe.as_ref().map(probe::Probe::fork).transpose()?;
            Ok::<_, String>(())
        })();
        if let Err(mut message) = result {
            if let Err(cleanup) = fork.close() {
                message.push('\n');
                message.push_str(&cleanup)
            }
            return Err(message);
        }
        Ok(fork)
    }

    fn resolve(
        &self,
        write: bool,
        operation: &str,
        input: &str,
    ) -> Result<(&Grant, PathBuf, PathBuf), Fault> {
        let absolute = clean(&self.base.join(input));
        for grant in if write { &self.writes } else { &self.reads } {
            if let Ok(relative) = absolute.strip_prefix(&grant.path) {
                let relative = if relative.as_os_str().is_empty() {
                    PathBuf::from(".")
                } else {
                    relative.to_owned()
                };
                return Ok((grant, relative, absolute));
            }
        }
        Err(Fault::Host(
            "runtime.capability-denied",
            format!("{operation} {} has no host capability", quote(&absolute)),
        ))
    }
    pub fn close(&mut self) -> Result<(), String> {
        let mut errors = vec![];
        for grant in self
            .reads
            .drain(..)
            .chain(self.writes.drain(..))
            .chain(self.lists.drain(..))
            .chain(self.removes.drain(..))
            .chain(self.open_files.drain(..))
        {
            if let Err(error) = close_file(grant.directory) {
                errors.push(format!(
                    "close adapter root {}: {}",
                    quote(&grant.path),
                    system_error(&error)
                ));
            }
        }
        for (directory, file) in std::mem::take(&mut self.observed) {
            if let Err(error) = close_file(file) {
                errors.push(format!(
                    "close observed directory {}: {}",
                    quote(&directory.path),
                    system_error(&error)
                ));
            }
        }
        if let Some(probe) = self.probe.take() {
            if let Err(error) = probe.close() {
                errors.push(error)
            }
        }
        if errors.is_empty() {
            Ok(())
        } else {
            Err(errors.join("\n"))
        }
    }
}

// Every traversed directory remains open. Relative symlinks can only walk within
// that descriptor stack; replacement of a pathname cannot redirect a held root.
fn open_confined(grant: &Grant, relative: &Path, write: bool) -> io::Result<File> {
    open_confined_flags(
        grant,
        relative,
        if write {
            1 | FLAGS[4] | FLAGS[5] | FLAGS[3]
        } else {
            FLAGS[3]
        },
    )
}
fn open_confined_flags(grant: &Grant, relative: &Path, final_flags: i32) -> io::Result<File> {
    let [nofollow, directory, cloexec, _, _, _] = FLAGS;
    let mut stack = vec![grant.directory.try_clone()?];
    let mut pending: VecDeque<OsString> = relative
        .components()
        .map(|part| part.as_os_str().to_owned())
        .collect();
    if pending.is_empty() {
        pending.push_back(".".into());
    }
    let mut links = 0;
    while let Some(part) = pending.pop_front() {
        if part == ".." {
            if stack.len() == 1 {
                return Err(io::Error::other("path escapes from parent"));
            }
            stack.pop();
            if pending.is_empty() {
                pending.push_back(".".into());
            }
            continue;
        }
        let name =
            CString::new(part.as_bytes()).map_err(|_| io::Error::other("invalid argument"))?;
        let last = pending.is_empty();
        let flags = nofollow | cloexec | if last { final_flags } else { directory };
        let parent = stack.last().unwrap().as_raw_fd();
        let fd = unsafe { openat(parent, name.as_ptr(), flags, 0o666u32) };
        if fd >= 0 {
            let opened = unsafe { File::from_raw_fd(fd) };
            if last {
                return Ok(opened);
            }
            stack.push(opened);
            continue;
        }
        let original = io::Error::last_os_error();
        let mut target = vec![0u8; 4096];
        let length =
            unsafe { readlinkat(parent, name.as_ptr(), target.as_mut_ptr(), target.len()) };
        if length < 0 {
            return Err(original);
        }
        if length as usize == target.len() {
            return Err(io::Error::other("symlink target too long"));
        }
        links += 1;
        if links > 40 {
            return Err(io::Error::other("too many levels of symbolic links"));
        }
        target.truncate(length as usize);
        let target = PathBuf::from(OsString::from_vec(target));
        if target.is_absolute() {
            return Err(io::Error::other("path escapes from parent"));
        }
        let components: Vec<_> = target
            .components()
            .map(|part| part.as_os_str().to_owned())
            .collect();
        for component in components.into_iter().rev() {
            pending.push_front(component);
        }
    }
    stack
        .pop()
        .ok_or_else(|| io::Error::other("invalid file path"))
}

fn read_failure(site: &Site, path: &Handle, message: &str) -> Fault {
    Fault::Recoverable(error(
        site,
        "File.ReadFailed",
        message,
        record([("file_path".into(), path.clone())]),
    ))
}
pub fn execute(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    match site.operation.as_str() {
        "File.Write.new" | "File.Append.new" => {
            return Ok(new(
                atom(site.operation.trim_end_matches(".new")),
                Data::Adapter(crate::adapters::AdapterValue::File(Value::Content {
                    append: site.operation == "File.Append.new",
                    path: args[0].clone(),
                    content: args[1].clone(),
                })),
            ))
        }
        "File.list" => return entries::list(context, site, &args[0]),
        "File.open_paths" => return probe::open_paths(context, site, &args[0]),
        "File.Remove.new" => return entries::selection(context, site, &args[0]),
        _ => (),
    }
    context.check_cancel()?;
    let (grant, relative, absolute) =
        context
            .files
            .resolve(false, "File.read", args[0].string())?;
    let opened_name = format!("{}/{}", grant.path.display(), relative.display());
    let mut file = match open_confined(grant, &relative, false) {
        Ok(file) => file,
        Err(e) => {
            return Err(read_failure(
                site,
                &args[0],
                &format!(
                    "File.read {}: openat {}: {}",
                    quote(&absolute),
                    relative.display(),
                    system_error(&e)
                ),
            ))
        }
    };
    let mut data = vec![];
    let mut buffer = [0u8; 65536];
    let result: Result<(), Fault> = (|| {
        loop {
            context.check_cancel()?;
            let remaining = (MAX_BYTES + 1 - data.len()).min(buffer.len());
            let count = match file.read(&mut buffer[..remaining]) {
                Ok(count) => count,
                Err(e) if e.kind() == io::ErrorKind::WouldBlock => {
                    wait_file(context, &file, 1, |error| {
                        read_failure(
                            site,
                            &args[0],
                            &format!("File.read {}: {}", quote(&absolute), system_error(&error)),
                        )
                    })?;
                    continue;
                }
                Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
                Err(e) => {
                    return Err(read_failure(
                        site,
                        &args[0],
                        &format!(
                            "File.read {}: read {}: {}",
                            quote(&absolute),
                            opened_name,
                            system_error(&e)
                        ),
                    ))
                }
            };
            if count == 0 {
                break;
            }
            data.extend_from_slice(&buffer[..count]);
            if data.len() > MAX_BYTES {
                return Err(limit("file read bytes"));
            }
        }
        Ok(())
    })();
    let closed = close_file(file);
    match (result, closed) {
        (Err(primary), Err(cleanup)) => return Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => return Err(primary),
        (Ok(()), Err(e)) => {
            return Err(read_failure(
                site,
                &args[0],
                &format!(
                    "File.read {}: close {}: {}",
                    quote(&absolute),
                    opened_name,
                    system_error(&e)
                ),
            ))
        }
        (Ok(()), Ok(())) => (),
    }
    context.check_cancel()?;
    Ok(new(atom("Bytes"), Data::Bytes(data)))
}

fn write_failure(
    site: &Site,
    path: &Handle,
    content: &Handle,
    absolute: &Path,
    confirmed: usize,
    stage: &str,
    message: &str,
) -> Fault {
    let mut activation = site.clone();
    let operation = if site.operation == "File.append" {
        "File.append"
    } else {
        "File.write"
    };
    activation.operation = operation.into();
    let progress = record([
        ("bytes_written".into(), int(confirmed)),
        ("stage".into(), text(stage)),
    ]);
    Fault::Recoverable(error(
        &activation,
        if operation == "File.append" {
            "File.AppendFailed"
        } else {
            "File.WriteFailed"
        },
        &format!("{operation} {}: {message}", quote(absolute)),
        record([
            ("file_path".into(), path.clone()),
            ("file_content".into(), content.clone()),
            ("progress".into(), progress),
        ]),
    ))
}
fn mutation_failure(fault: Fault, operation: &str, path: &Path) -> Fault {
    match fault {
        Fault::Boundary { primary, details } => Fault::Boundary {
            primary: Box::new(mutation_failure(*primary, operation, path)),
            details,
        },
        Fault::Host(code, message) => Fault::Host(
            code,
            format!(
                "boundary operation failed: {operation} {}: {message}",
                quote(path)
            ),
        ),
        other => other,
    }
}
pub fn commit(context: &mut Context, site: &Site, target: &Handle) -> Outcome {
    let removal = matches!(
        &target.data,
        Data::Adapter(crate::adapters::AdapterValue::File(Value::Remove { .. }))
    );
    if !removal {
        *context.files.write_progress.borrow_mut() = Some(record([
            ("bytes_written".into(), int(0)),
            ("stage".into(), text("open")),
        ]));
    }
    let result = commit_inner(context, site, target);
    if removal {
        return result;
    }
    let progress = context.files.write_progress.borrow_mut().take();
    result.map_err(|fault| {
        if matches!(fault.primary(), Fault::Recoverable(_)) {
            fault
        } else {
            fault.with_details(BoundaryDetails {
                write_progress: progress.map(|progress| progress.json()),
                ..Default::default()
            })
        }
    })
}
fn commit_inner(context: &mut Context, site: &Site, target: &Handle) -> Outcome {
    let Data::Adapter(crate::adapters::AdapterValue::File(target)) = &target.data else {
        return Err(host("unsupported native commit target"));
    };
    if let Value::Remove { .. } = target {
        return entries::remove(context, site, target);
    }
    let Value::Content {
        path,
        content,
        append,
    } = target
    else {
        return Err(host("unsupported native commit target"));
    };
    let operation = if *append { "File.append" } else { "File.write" };
    let mut activation = site.clone();
    activation.operation = operation.into();
    let site = &activation;
    context.check_cancel()?;
    let (grant, relative, absolute) = context.files.resolve(true, operation, path.string())?;
    let opened_name = format!("{}/{}", grant.path.display(), relative.display());
    let parent_relative = relative
        .parent()
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or(Path::new("."));
    let name = relative
        .file_name()
        .unwrap_or_else(|| std::ffi::OsStr::new("."));
    let parent = open_confined_flags(grant, parent_relative, FLAGS[1]).map_err(|error| {
        write_failure(
            site,
            path,
            content,
            &absolute,
            0,
            "open",
            &format!("openat {}: {}", relative.display(), system_error(&error)),
        )
    })?;
    let opened = (|| {
        context
            .session
            .processes
            .protect_name(&parent, &name.to_string_lossy())
            .map_err(|fault| mutation_failure(fault, operation, &absolute))?;
        if let Some(probe) = &context.files.probe {
            probe
                .protect_name(&parent, &name.to_string_lossy())
                .map_err(|fault| mutation_failure(fault, operation, &absolute))?;
        }
        let flags = 1 | FLAGS[3] | if *append { append_flag() } else { 0 };
        let file = loop {
            context.check_cancel()?;
            match open_confined_flags(grant, &relative, flags) {
                Ok(file) => break file,
                Err(error) if error.kind() == io::ErrorKind::NotFound => {
                    match open_confined_flags(grant, &relative, flags | FLAGS[4] | exclusive_flag())
                    {
                        Ok(file) => break file,
                        Err(error) if error.kind() == io::ErrorKind::AlreadyExists && *append => {
                            continue
                        }
                        Err(error) => {
                            return Err(write_failure(
                                site,
                                path,
                                content,
                                &absolute,
                                0,
                                "open",
                                &format!("openat {}: {}", relative.display(), system_error(&error)),
                            ))
                        }
                    }
                }
                Err(error) if error.raw_os_error() == Some(6) => {
                    std::thread::sleep(std::time::Duration::from_millis(10));
                    continue;
                }
                Err(error) => {
                    return Err(write_failure(
                        site,
                        path,
                        content,
                        &absolute,
                        0,
                        "open",
                        &format!("openat {}: {}", relative.display(), system_error(&error)),
                    ))
                }
            }
        };
        let mutation = (|| {
            context
                .session
                .processes
                .protect_file(&file)
                .map_err(|fault| mutation_failure(fault, operation, &absolute))?;
            if let Some(probe) = &context.files.probe {
                probe
                    .protect_file(&file)
                    .map_err(|fault| mutation_failure(fault, operation, &absolute))?;
            }
            if !*append
                && file
                    .metadata()
                    .map_err(|error| host(error.to_string()))?
                    .file_type()
                    .is_file()
            {
                file.set_len(0).map_err(|error| {
                    write_failure(
                        site,
                        path,
                        content,
                        &absolute,
                        0,
                        "open",
                        &system_error(&error),
                    )
                })?;
            }
            Ok::<_, Fault>(())
        })();
        if let Err(primary) = mutation {
            return match close_file(file) {
                Ok(()) => Err(primary),
                Err(cleanup) => Err(cleanup_failure(primary, cleanup)),
            };
        }
        Ok(file)
    })();
    let mut file = match (opened, close_file(parent)) {
        (Err(primary), Err(cleanup)) => return Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => return Err(primary),
        (Ok(file), Err(cleanup)) => {
            let primary = host(system_error(&cleanup));
            return match close_file(file) {
                Ok(()) => Err(primary),
                Err(cleanup) => Err(cleanup_failure(primary, cleanup)),
            };
        }
        (Ok(file), Ok(())) => file,
    };
    let Data::Bytes(data) = &content.data else {
        return Err(host("invalid File.Write content"));
    };
    let mut confirmed = 0;
    *context.files.write_progress.borrow_mut() = Some(record([
        ("bytes_written".into(), int(0)),
        ("stage".into(), text("write")),
    ]));
    let result: Result<(), Fault> = (|| {
        nonblocking(&file).map_err(|e| {
            write_failure(
                site,
                path,
                content,
                &absolute,
                0,
                "write",
                &system_error(&e),
            )
        })?;
        while confirmed < data.len() {
            context.check_cancel()?;
            let count = match file.write(&data[confirmed..]) {
                Ok(count) => count,
                Err(e) if e.kind() == io::ErrorKind::WouldBlock => {
                    wait_file(context, &file, 4, |error| {
                        write_failure(
                            site,
                            path,
                            content,
                            &absolute,
                            confirmed,
                            "write",
                            &system_error(&error),
                        )
                    })?;
                    continue;
                }
                Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
                Err(e) => {
                    return Err(write_failure(
                        site,
                        path,
                        content,
                        &absolute,
                        confirmed,
                        "write",
                        &format!("write {}: {}", opened_name, system_error(&e)),
                    ))
                }
            };
            if count == 0 {
                return Err(write_failure(
                    site,
                    path,
                    content,
                    &absolute,
                    confirmed,
                    "write",
                    "short write",
                ));
            }
            confirmed += count;
            *context.files.write_progress.borrow_mut() = Some(record([
                ("bytes_written".into(), int(confirmed)),
                ("stage".into(), text("write")),
            ]));
        }
        Ok(())
    })();
    if result.is_ok() {
        *context.files.write_progress.borrow_mut() = Some(record([
            ("bytes_written".into(), int(confirmed)),
            ("stage".into(), text("finalize")),
        ]));
    }
    let closed = close_file(file);
    match (result, closed) {
        (Err(primary), Err(cleanup)) => return Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => return Err(primary),
        (Ok(()), Err(e)) => {
            return Err(write_failure(
                site,
                path,
                content,
                &absolute,
                confirmed,
                "finalize",
                &format!("close {}: {}", opened_name, system_error(&e)),
            ))
        }
        (Ok(()), Ok(())) => (),
    }
    context.check_cancel()?;
    let receipt = new(
        atom(if *append {
            "File.AppendReceipt"
        } else {
            "File.WriteReceipt"
        }),
        Data::Wrapped(record([
            ("path".into(), path.clone()),
            ("bytes_written".into(), int(confirmed)),
        ])),
    );
    context.last_activation = Some(serde_json::json!({
        "operation": operation,
        "span": site.source_record(),
        "receipt":receipt.json()
    }));
    context.charge(&receipt)?;
    Ok(receipt)
}

#[cfg(target_os = "macos")]
fn append_flag() -> i32 {
    8
}
#[cfg(target_os = "linux")]
fn append_flag() -> i32 {
    0x400
}

#[cfg(target_os = "macos")]
fn exclusive_flag() -> i32 {
    0x800
}
#[cfg(target_os = "linux")]
fn exclusive_flag() -> i32 {
    0x80
}
