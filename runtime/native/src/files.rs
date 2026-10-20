use crate::{context::*, value::*};
use std::{
    collections::VecDeque,
    ffi::{CStr, CString, OsString},
    fs::File,
    io::{self, Read, Write},
    os::{
        fd::{AsRawFd, FromRawFd, IntoRawFd},
        unix::ffi::{OsStrExt, OsStringExt},
    },
    path::{Component, Path, PathBuf},
};

pub const SUPPORTED: &[&str] = &["File.read", "File.Write.new"];
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
        "File.Write.new" => {
            args == [atom("File.Path"), atom("Bytes")] && site.typ == atom("File.Write")
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
}
pub struct Files {
    base: PathBuf,
    reads: Vec<Grant>,
    writes: Vec<Grant>,
}
pub fn empty() -> Files {
    Files {
        base: PathBuf::new(),
        reads: vec![],
        writes: vec![],
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
    serde_json::to_string(&path.to_string_lossy()).unwrap()
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
        grants.push(Grant { path, directory });
    }
    grants.sort_by_key(|grant| std::cmp::Reverse(grant.path.as_os_str().len()));
    Ok(grants)
}
pub fn configure(reads: &[String], writes: &[String]) -> Result<Files, String> {
    let base = std::env::current_dir().map_err(|e| format!("resolve execution directory: {e}"))?;
    Ok(Files {
        reads: open_roots(&base, reads)?,
        writes: open_roots(&base, writes)?,
        base,
    })
}
impl Files {
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
        for grant in self.reads.drain(..).chain(self.writes.drain(..)) {
            if let Err(error) = close_file(grant.directory) {
                errors.push(format!(
                    "close adapter root {}: {}",
                    quote(&grant.path),
                    system_error(&error)
                ));
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
    let [nofollow, directory, cloexec, nonblock, create, truncate] = FLAGS;
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
        let flags = nofollow
            | cloexec
            | if last {
                if write {
                    1 | create | truncate
                } else {
                    nonblock
                }
            } else {
                directory
            };
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
    if site.operation == "File.Write.new" {
        return Ok(new(
            atom("File.Write"),
            Data::FileWrite {
                path: args[0].clone(),
                content: args[1].clone(),
            },
        ));
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
    activation.operation = "File.write".into();
    let progress = record([
        ("bytes_written".into(), int(confirmed)),
        ("stage".into(), text(stage)),
    ]);
    Fault::Recoverable(error(
        &activation,
        "File.WriteFailed",
        &format!("File.write {}: {message}", quote(absolute)),
        record([
            ("file_path".into(), path.clone()),
            ("file_content".into(), content.clone()),
            ("progress".into(), progress),
        ]),
    ))
}
pub fn commit(context: &mut Context, site: &Site, target: &Handle) -> Outcome {
    let Data::FileWrite { path, content } = &target.data else {
        return Err(host("unsupported native commit target"));
    };
    context.check_cancel()?;
    let (grant, relative, absolute) = context.files.resolve(true, "File.write", path.string())?;
    let opened_name = format!("{}/{}", grant.path.display(), relative.display());
    let mut file = open_confined(grant, &relative, true).map_err(|e| {
        write_failure(
            site,
            path,
            content,
            &absolute,
            0,
            "open",
            &format!("openat {}: {}", relative.display(), system_error(&e)),
        )
    })?;
    let Data::Bytes(data) = &content.data else {
        return Err(host("invalid File.Write content"));
    };
    let mut confirmed = 0;
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
        }
        Ok(())
    })();
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
        atom("File.WriteReceipt"),
        Data::Wrapped(record([
            ("path".into(), path.clone()),
            ("bytes_written".into(), int(confirmed)),
        ])),
    );
    context.last_activation = Some(serde_json::json!({
        "operation": "File.write",
        "span": {"file":site.raw["file"],"start":site.raw["start"],"end":site.raw["end"]},
        "receipt":receipt.json()
    }));
    context.charge(&receipt)?;
    Ok(receipt)
}
