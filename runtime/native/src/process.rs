use crate::{context::*, value::*};
use serde_json::{json, Value as Json};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs::{self, File, OpenOptions},
    os::unix::fs::{MetadataExt, OpenOptionsExt},
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Condvar, Mutex,
    },
    time::{Duration, Instant},
};
#[path = "process_hash.rs"]
pub(crate) mod hashing;
pub const UNAVAILABLE: &str =
    "Process activation requires a configured, qualified host execution profile";
pub const SUPPORTED: &[&str] = &[
    "Process.Executable.new",
    "Process.Command.new",
    "Process.Termination.exit_code",
    "Process.Termination.signal",
];
#[derive(Clone)]
pub struct Value {
    pub kind: String,
    pub fields: BTreeMap<String, Handle>,
}
impl Value {
    pub fn weight(&self) -> u64 {
        self.fields.values().map(|v| v.weight).sum()
    }
    pub fn json(&self) -> Json {
        Json::Object(
            self.fields
                .iter()
                .map(|(n, v)| (n.clone(), v.json()))
                .collect(),
        )
    }
    pub fn canonical_json(&self) -> String {
        let names: &[&str] = match self.kind.as_str() {
            "Process.Executable" => &["path"],
            "Process.Command" => &[
                "executable",
                "arguments",
                "environment",
                "directory",
                "stdin",
            ],
            "Process.Result" => &["termination", "stdout", "stderr"],
            "Process.Termination" if self.fields.contains_key("exit_code") => {
                &["kind", "exit_code"]
            }
            "Process.Termination" => &["kind", "signal"],
            _ => std::process::abort(),
        };
        format!(
            "{{{}}}",
            names
                .iter()
                .map(|name| format!(
                    "{}:{}",
                    serde_json::to_string(name).unwrap(),
                    self.fields[*name].canonical_json()
                ))
                .collect::<Vec<_>>()
                .join(",")
        )
    }
    pub fn output(&self) -> String {
        self.canonical_json()
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        Some(&self.fields)
    }
    pub fn snapshot(&self) -> Self {
        if self.kind == "Process.Result" {
            let mut result = self.clone();
            for name in ["stdout", "stderr"] {
                result
                    .fields
                    .insert(name.into(), new(atom("Bytes"), Data::Bytes(vec![])));
            }
            return result;
        }
        if self.kind != "Process.Command" {
            return self.clone();
        }
        let mut v = self.clone();
        v.fields.insert(
            "arguments".into(),
            new(Type::List(Box::new(atom("String"))), Data::List(vec![])),
        );
        v.fields.insert(
            "environment".into(),
            new(
                Type::Map(Box::new(atom("String")), Box::new(atom("String"))),
                Data::Map(vec![]),
            ),
        );
        v.fields
            .insert("stdin".into(), new(atom("Bytes"), Data::Bytes(vec![])));
        v
    }
}
fn value(kind: &str, fields: impl IntoIterator<Item = (String, Handle)>) -> Handle {
    new(
        atom(kind),
        Data::Adapter(crate::adapters::AdapterValue::Process(Value {
            kind: kind.into(),
            fields: fields.into_iter().collect(),
        })),
    )
}
fn payload(v: &Handle) -> &Value {
    let Data::Adapter(crate::adapters::AdapterValue::Process(v)) = &v.data else {
        std::process::abort()
    };
    v
}
pub fn admit(site: &Site) -> Result<(), String> {
    let args: Vec<_> = site
        .arguments
        .iter()
        .filter_map(|a| {
            if let Argument::Value(t) = a {
                Some(t.name())
            } else {
                None
            }
        })
        .collect();
    let (expected, out) = match site.operation.as_str() {
        "Process.Executable.new" => (vec!["File.Path"], "Process.Executable!"),
        "Process.Command.new" => (
            vec![
                "Process.Executable",
                "List<String>",
                "Map<String, String>",
                "File.Path",
                "Bytes",
            ],
            "Process.Command!",
        ),
        "Process.Termination.exit_code" | "Process.Termination.signal" => {
            (vec!["Process.Termination"], "Int!")
        }
        _ => return Err("unsupported Process operation".into()),
    };
    if args.iter().map(String::as_str).collect::<Vec<_>>() == expected
        && args.len() == site.arguments.len()
        && site.typ.name() == out
    {
        Ok(())
    } else {
        Err("Process operation metadata type mismatch".into())
    }
}
fn invalid(site: &Site, kind: &str, message: &str) -> Outcome {
    Err(Fault::Recoverable(error(site, kind, message, record([]))))
}
pub fn execute(_context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    match site.operation.as_str() {
        "Process.Executable.new" => {
            if !Path::new(args[0].string()).is_absolute() || args[0].string().contains('\0') {
                return invalid(
                    site,
                    "Process.InvalidExecutable",
                    "Process executable path must be absolute and contain no NUL",
                );
            };
            Ok(value(
                "Process.Executable",
                [("path".into(), args[0].clone())],
            ))
        }
        "Process.Command.new" => {
            let directory = args[3].string();
            if !Path::new(directory).is_absolute() || directory.contains('\0') {
                return invalid(
                    site,
                    "Process.InvalidCommand",
                    "Process directory must be absolute and contain no NUL",
                );
            };
            for (index, arg) in args[1].items().iter().enumerate() {
                if arg.string().contains('\0') {
                    return invalid(
                        site,
                        "Process.InvalidCommand",
                        &format!("Process argument {index} contains NUL"),
                    );
                }
            }
            let Data::Map(environment) = &args[2].data else {
                return Err(host("invalid Process environment"));
            };
            for (index, (name, value)) in environment.iter().enumerate() {
                if name.string().is_empty() || name.string().contains(['\0', '=']) {
                    return invalid(
                        site,
                        "Process.InvalidCommand",
                        &format!("Process environment entry {index} has an invalid name"),
                    );
                }
                if value.string().contains('\0') {
                    return invalid(
                        site,
                        "Process.InvalidCommand",
                        &format!("Process environment entry {index} value contains NUL"),
                    );
                }
            }
            Ok(value(
                "Process.Command",
                [
                    "executable",
                    "arguments",
                    "environment",
                    "directory",
                    "stdin",
                ]
                .into_iter()
                .zip(args.iter().cloned())
                .map(|(n, v)| (n.into(), v)),
            ))
        }
        "Process.Termination.exit_code" | "Process.Termination.signal" => {
            let v = payload(&args[0]);
            let name = if site.operation.ends_with("exit_code") {
                "exit_code"
            } else {
                "signal"
            };
            let expected = if name == "exit_code" {
                "exited"
            } else {
                "signaled"
            };
            if v.fields["kind"].string() != expected {
                return invalid(
                    site,
                    "Process.TerminationKind",
                    &format!("process termination is not {expected}"),
                );
            }
            Ok(v.fields[name].clone())
        }
        _ => Err(host("unsupported Process operation")),
    }
}
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
struct Identity(u64, u64);
fn close_file(file: File) -> std::io::Result<()> {
    use std::os::fd::IntoRawFd;
    extern "C" {
        fn close(fd: i32) -> i32;
    }
    if unsafe { close(file.into_raw_fd()) } == 0 {
        Ok(())
    } else {
        Err(std::io::Error::last_os_error())
    }
}
fn open_read(path: impl AsRef<Path>) -> std::io::Result<File> {
    #[cfg(target_os = "macos")]
    const NONBLOCK: i32 = 4;
    #[cfg(target_os = "linux")]
    const NONBLOCK: i32 = 0x800;
    OpenOptions::new()
        .read(true)
        .custom_flags(NONBLOCK)
        .open(path)
}
fn identity(file: &File) -> Result<Identity, String> {
    let m = file.metadata().map_err(|e| e.to_string())?;
    Ok(Identity(m.dev(), m.ino()))
}
fn canonical(path: &str) -> bool {
    let p = Path::new(path);
    policy_path(path)
        && !path.contains('\0')
        && path.len() <= 1024
        && p.components().all(|c| {
            !matches!(
                c,
                std::path::Component::CurDir | std::path::Component::ParentDir
            )
        })
        && fs::canonicalize(p).map_or(false, |r| r == p)
}
struct Registration {
    file: Mutex<Option<File>>,
    digest: String,
    identity: Identity,
    ancestors: Vec<(PathBuf, Identity)>,
}
struct State {
    active: bool,
    unresolved: bool,
    closing: bool,
    quarantined: bool,
    releasing: bool,
    closed: bool,
    close_error: Option<String>,
}
pub type DriverCallback = unsafe extern "C" fn(
    usize,
    *const u8,
    usize,
    *const u8,
    usize,
    *mut u8,
    *mut u8,
    usize,
    extern "C" fn(*const AtomicBool) -> i32,
    *const AtomicBool,
    u64,
    *mut NativeResult,
);
#[derive(Clone, Copy)]
struct Driver {
    callback: DriverCallback,
    userdata: usize,
}
pub struct Processes {
    registrations: BTreeMap<String, Registration>,
    identities: BTreeSet<Identity>,
    namespace: BTreeSet<(Identity, String)>,
    directories: Mutex<Vec<File>>,
    launcher: String,
    embedded: bool,
    driver: Option<Driver>,
    configuration: Vec<u8>,
    roots: Vec<String>,
    timeout_ms: u64,
    output_bytes: usize,
    cleanup_ms: u64,
    state: Mutex<State>,
    changed: Condvar,
}
pub fn empty() -> Processes {
    Processes {
        registrations: BTreeMap::new(),
        identities: BTreeSet::new(),
        namespace: BTreeSet::new(),
        directories: Mutex::new(vec![]),
        launcher: String::new(),
        embedded: false,
        driver: None,
        configuration: vec![],
        roots: vec![],
        timeout_ms: 5000,
        output_bytes: 1 << 20,
        cleanup_ms: 1000,
        state: Mutex::new(State {
            active: false,
            unresolved: false,
            closing: false,
            quarantined: false,
            releasing: false,
            closed: false,
            close_error: None,
        }),
        changed: Condvar::new(),
    }
}
fn registry_error(subject: &str, message: impl AsRef<str>) -> String {
    format!("Process registry \"{subject}\": {}", message.as_ref())
}
fn startup_error(message: &str) -> String {
    format!("Process startup \"ownership\": {message}")
}
fn field(body: &mut Vec<u8>, entry: &str) {
    body.extend_from_slice(&(entry.len() as u32).to_be_bytes());
    body.extend_from_slice(entry.as_bytes())
}
fn frame(
    magic: &[u8; 4],
    entries: &[String],
    count: usize,
    other: usize,
    maximum: usize,
) -> Result<Vec<u8>, String> {
    let mut body = vec![];
    for entry in entries {
        if entry.len() > maximum || entry.contains('\0') {
            return Err("Process startup failed during input".into());
        }
        field(&mut body, entry)
    }
    let mut out = magic.to_vec();
    out.extend_from_slice(&(body.len() as u32).to_be_bytes());
    out.extend_from_slice(&(count as u32).to_be_bytes());
    out.extend_from_slice(&(other as u32).to_be_bytes());
    out.extend(body);
    Ok(out)
}
#[cfg(target_os = "macos")]
fn supported() -> bool {
    if !cfg!(target_arch = "aarch64") {
        return false;
    }
    use std::ffi::CStr;
    extern "C" {
        fn sysctlbyname(
            name: *const i8,
            old: *mut u8,
            size: *mut usize,
            new: *mut u8,
            len: usize,
        ) -> i32;
    }
    let mut bytes = [0u8; 256];
    let mut size = bytes.len();
    unsafe {
        sysctlbyname(
            b"kern.osrelease\0".as_ptr().cast(),
            bytes.as_mut_ptr(),
            &mut size,
            std::ptr::null_mut(),
            0,
        ) == 0
            && CStr::from_ptr(bytes.as_ptr().cast())
                .to_bytes()
                .starts_with(b"27.")
    }
}
#[cfg(not(target_os = "macos"))]
fn supported() -> bool {
    false
}
fn policy_path(path: &str) -> bool {
    let input = Path::new(path);
    let rebuilt: PathBuf = input.components().collect();
    input.is_absolute()
        && !path.contains('\0')
        && rebuilt.as_os_str() == std::ffi::OsStr::new(path)
        && input
            .components()
            .all(|c| !matches!(c, std::path::Component::ParentDir))
}
pub fn validate_policy(grants: &[String], launcher: &str, roots: &[String]) -> Result<(), String> {
    if grants.is_empty() && launcher.is_empty() && roots.is_empty() {
        return Ok(());
    }
    if grants.is_empty() || launcher.is_empty() || roots.is_empty() {
        return Err("Process policy requires a launcher, at least one executable grant, and at least one read root".into());
    }
    if !policy_path(launcher) {
        return Err("Process launcher requires a clean absolute NUL-free path".into());
    }
    let mut paths = BTreeSet::new();
    for (index, description) in grants.iter().enumerate() {
        let Some((digest, path)) = description.split_once(',') else {
            return Err(format!(
                "Process grant {index} requires SHA256,ABSOLUTE_PATH"
            ));
        };
        if digest.len() != 64 {
            return Err(format!(
                "Process grant {index} requires SHA256,ABSOLUTE_PATH"
            ));
        }
        if !digest.bytes().all(|b| b.is_ascii_hexdigit()) {
            return Err(format!(
                "Process grant {index} requires a 64-digit hexadecimal SHA256"
            ));
        }
        if !policy_path(path) {
            return Err(format!("Process grant {index}: Process executable grant requires a clean absolute NUL-free path"));
        }
        if !paths.insert(path) {
            return Err(format!(
                "duplicate Process executable path at grant {index}"
            ));
        }
        if path == launcher {
            return Err("Process launcher and command paths must differ".into());
        }
    }
    let mut seen = BTreeSet::new();
    for (index, path) in roots.iter().enumerate() {
        if !policy_path(path) {
            return Err(format!(
                "Process read root {index} requires a clean absolute NUL-free path"
            ));
        }
        if !seen.insert(path) {
            return Err(format!("duplicate Process read root at index {index}"));
        }
    }
    Ok(())
}
pub fn configure(
    grants: &[String],
    launcher: &str,
    roots: &[String],
    timeout_ms: u64,
    output_bytes: usize,
    cleanup_ms: u64,
) -> Result<Processes, String> {
    validate_policy(grants, launcher, roots)?;
    let mut p = empty();
    if grants.is_empty() && launcher.is_empty() && roots.is_empty() {
        return Ok(p);
    }
    if grants.is_empty() || launcher.is_empty() || roots.is_empty() {
        return Err("Process policy requires a launcher, at least one executable grant, and at least one read root".into());
    }
    if !supported() {
        return Err(UNAVAILABLE.into());
    }
    if timeout_ms == 0
        || timeout_ms > 5000
        || cleanup_ms == 0
        || cleanup_ms > 1000
        || output_bytes == 0
        || output_bytes > 1 << 20
    {
        return Err(startup_error(
            "native lifecycle limits exceed the selected profile",
        ));
    }
    if grants.len() > 16 || roots.len() > 16 {
        return Err("Process startup failed during input".into());
    }
    if !canonical(launcher) {
        return Err(startup_error("invalid native helper path"));
    }
    p.timeout_ms = timeout_ms;
    p.output_bytes = output_bytes;
    p.cleanup_ms = cleanup_ms;
    p.launcher = launcher.into();
    let helper = open_read(launcher).map_err(|_| startup_error("native helper unavailable"))?;
    if !helper.metadata().map_or(false, |info| info.is_file()) {
        let _ = close_file(helper);
        return Err(startup_error("native helper requires a regular file"));
    }
    let helper_id =
        identity(&helper).map_err(|_| startup_error("native helper identity mismatch"))?;
    let digest = hashing::digest(&helper);
    let close = close_file(helper);
    if digest.is_err() || close.is_err() {
        return Err(startup_error("native helper identity mismatch"));
    }
    let helper_hash = hashing::hex(&digest.unwrap());
    let own = std::env::current_exe().map_err(|e| e.to_string())?;
    let embedded = own == Path::new(launcher)
        && identity(&open_read(&own).map_err(|e| e.to_string())?)? == helper_id;
    p.embedded = embedded;
    if !embedded
        && helper_hash != "ed153dd84e20a3d97212bbe06b02f6c5f236c43eed00a37543f1f0d6988e6caa"
    {
        return Err(startup_error("native helper identity mismatch"));
    }
    let mut commands = vec![];
    for (index, description) in grants.iter().enumerate() {
        let bytes = description.as_bytes();
        if bytes.len() < 66 || bytes[64] != b',' {
            return Err(format!(
                "Process grant {index} requires SHA256,ABSOLUTE_PATH"
            ));
        }
        let digest = &description[..64];
        if !digest.bytes().all(|b| b.is_ascii_hexdigit()) {
            return Err(format!(
                "Process grant {index} requires a 64-digit hexadecimal SHA256"
            ));
        }
        let path = &description[65..];
        if !canonical(path) {
            return Err(startup_error("invalid native command path"));
        }
        if path == launcher {
            return Err("Process launcher and command paths must differ".into());
        }
        if p.registrations.contains_key(path) {
            return Err("duplicate Process executable path".into());
        }
        p.register(path, &digest.to_ascii_lowercase())?;
        if p.registrations[path].identity == helper_id {
            return Err(startup_error("native helper cannot be a command"));
        }
        commands.push(path.to_owned())
    }
    p.register(launcher, &helper_hash)?;
    for root in roots {
        if !canonical(root) || p.roots.contains(root) {
            return Err(startup_error("invalid native read root"));
        }
        let file = open_read(root).map_err(|_| startup_error("native read root unavailable"))?;
        if !file.metadata().map_err(|e| e.to_string())?.is_dir() {
            return Err(startup_error("native read root unavailable"));
        }
        p.roots.push(root.clone())
    }
    let mut lines = vec![
        "(version 1)".into(),
        "(deny default (with no-log))".into(),
        "(allow file-read* (subpath \"/usr/lib\") (subpath \"/System/Library\"))".into(),
        "(allow file-read* file-test-existence (literal \"/\"))".into(),
        "(deny network* (with no-log))".into(),
        "(deny process-fork (with no-log))".into(),
        "(allow signal (target self))".into(),
    ];
    for path in &commands {
        let q = serde_json::to_string(path).unwrap();
        lines.push(format!("(allow process-exec (literal {q}))"));
        lines.push(format!("(allow file-read* (literal {q}))"));
    }
    for path in roots {
        lines.push(format!(
            "(allow file-read* (subpath {}))",
            serde_json::to_string(path).unwrap()
        ))
    }
    let profile = lines.join("\n") + "\n";
    if profile.len() > 32768 {
        return Err("Process startup failed during input".into());
    }
    let directory = Path::new(launcher)
        .parent()
        .unwrap()
        .to_str()
        .unwrap()
        .to_owned();
    let mut entries = vec![profile, directory];
    entries.extend(commands.iter().cloned());
    entries.extend(roots.iter().cloned());
    p.configuration = frame(b"LOC1", &entries, commands.len(), roots.len(), 32768)?;
    if p.configuration.len() > 65552 {
        return Err("Process startup failed during input".into());
    }
    Ok(p)
}
impl Processes {
    fn register(&mut self, path: &str, digest: &str) -> Result<(), String> {
        let file = open_read(path).map_err(|e| registry_error("open executable", e.to_string()))?;
        if !file.metadata().map_err(|e| e.to_string())?.is_file() {
            return Err(registry_error(
                "registration",
                "executable must be a regular file",
            ));
        }
        if hashing::hex(&hashing::digest(&file)?) != digest {
            return Err(registry_error("registration", "executable digest differs"));
        }
        let id = identity(&file)?;
        self.identities.insert(id);
        let mut ancestors = vec![];
        let mut current = Path::new(path);
        loop {
            let parent = current.parent().unwrap_or(current);
            let file =
                open_read(parent).map_err(|e| registry_error("retain namespace", e.to_string()))?;
            let id = identity(&file)?;
            self.identities.insert(id);
            self.namespace.insert((
                id,
                current
                    .file_name()
                    .map_or("", |s| s.to_str().unwrap())
                    .to_owned(),
            ));
            self.directories.get_mut().unwrap().push(file);
            ancestors.push((parent.to_owned(), id));
            if current == parent {
                break;
            }
            current = parent;
        }
        self.registrations.insert(
            path.into(),
            Registration {
                file: Mutex::new(Some(file)),
                digest: digest.into(),
                identity: id,
                ancestors,
            },
        );
        Ok(())
    }
    fn validate(
        &self,
        path: &str,
        mut check: impl FnMut() -> Result<(), Fault>,
    ) -> Result<(), Fault> {
        check()?;
        let registration = self
            .registrations
            .get(path)
            .ok_or_else(|| host(registry_error("admission", "unregistered executable")))?;
        for (path, id) in &registration.ancestors {
            check()?;
            let observed = open_read(path)
                .map_err(|error| host(registry_error("admission", error.to_string())))?;
            if identity(&observed).map_err(host)? != *id {
                return Err(host(registry_error(
                    "admission",
                    "registered namespace identity differs",
                )));
            }
        }
        check()?;
        let observed = open_read(path)
            .map_err(|error| host(registry_error("admission", error.to_string())))?;
        if identity(&observed).map_err(host)? != registration.identity {
            return Err(host(registry_error(
                "admission",
                "executable identity differs",
            )));
        }
        let retained = registration.file.lock().unwrap();
        let file = retained
            .as_ref()
            .ok_or_else(|| host(registry_error("admission", "registry is closing or closed")))?;
        if identity(file).map_err(host)? != registration.identity {
            return Err(host(registry_error(
                "admission",
                "retained executable identity differs",
            )));
        }
        if hashing::hex(&hashing::digest_with_cancel(file, &mut check)?) != registration.digest {
            return Err(host(registry_error(
                "admission",
                "executable digest differs",
            )));
        }
        Ok(())
    }
    pub fn protect_name(&self, parent: &File, name: &str) -> Result<(), Fault> {
        if self.state.lock().unwrap().closing {
            return Err(host(registry_error(
                "admission",
                "registry is closing or closed",
            )));
        }
        if self
            .namespace
            .contains(&(identity(parent).map_err(host)?, name.into()))
        {
            Err(host(registry_error(
                "File mutation",
                "registered namespace entry is protected",
            )))
        } else {
            Ok(())
        }
    }
    pub fn protect_file(&self, file: &File) -> Result<(), Fault> {
        if self.state.lock().unwrap().closing {
            return Err(host(registry_error(
                "admission",
                "registry is closing or closed",
            )));
        }
        if self.identities.contains(&identity(file).map_err(host)?) {
            Err(host(registry_error(
                "File mutation",
                "registered executable or namespace is protected",
            )))
        } else {
            Ok(())
        }
    }
    pub fn protect_removal(&self, parent: &File, name: &str) -> Result<(), Fault> {
        self.protect_name(parent, name)?;
        let name = std::ffi::CString::new(name)
            .map_err(|_| host(registry_error("File mutation identity", "invalid argument")))?;
        let mut observed = std::mem::MaybeUninit::<libc::stat>::uninit();
        let status = unsafe {
            libc::fstatat(
                std::os::fd::AsRawFd::as_raw_fd(parent),
                name.as_ptr(),
                observed.as_mut_ptr(),
                libc::AT_SYMLINK_NOFOLLOW,
            )
        };
        if status != 0 {
            return Err(host(registry_error(
                "File mutation identity",
                std::io::Error::last_os_error().to_string(),
            )));
        }
        let observed = unsafe { observed.assume_init() };
        if self
            .identities
            .contains(&Identity(observed.st_dev as u64, observed.st_ino as u64))
        {
            Err(host(registry_error(
                "File mutation",
                "registered executable or namespace is protected",
            )))
        } else {
            Ok(())
        }
    }

    pub fn close(&self) -> Result<(), String> {
        self.close_wait(Some(Duration::from_millis(self.cleanup_ms)))
    }
    pub fn wait_retained(&self) -> Result<(), String> {
        self.close_wait(None)
    }
    fn close_wait(&self, bound: Option<Duration>) -> Result<(), String> {
        let deadline = bound.map(|duration| Instant::now() + duration);
        let mut state = self.state.lock().unwrap();
        state.closing = true;
        while state.active || state.unresolved || state.releasing {
            if let Some(deadline) = deadline {
                let remaining = deadline.saturating_duration_since(Instant::now());
                if remaining.is_zero() {
                    let message =
                        registry_error("close", "active or unresolved registry ownership");
                    state.close_error.get_or_insert(message.clone());
                    return Err(message);
                }
                state = self.changed.wait_timeout(state, remaining).unwrap().0;
            } else {
                state = self.changed.wait(state).unwrap();
            }
        }
        if state.closed {
            return state.close_error.clone().map_or(Ok(()), Err);
        }
        state.releasing = true;
        drop(state);
        let mut failures = vec![];
        for registration in self.registrations.values() {
            if let Some(file) = registration.file.lock().unwrap().take() {
                if close_file(file).is_err() {
                    failures.push(registry_error(
                        "close",
                        "registered executable release failed",
                    ));
                }
            }
        }
        for file in self.directories.lock().unwrap().drain(..) {
            if close_file(file).is_err() {
                failures.push(registry_error("close", "namespace handle release failed"));
            }
        }
        let mut state = self.state.lock().unwrap();
        if state.quarantined {
            failures.push(registry_error("close", "registry is quarantined"))
        }
        if let Some(history) = state.close_error.take() {
            failures.insert(0, history)
        }
        state.close_error = if failures.is_empty() {
            None
        } else {
            Some(failures.join("\n"))
        };
        state.closed = true;
        state.releasing = false;
        self.changed.notify_all();
        state.close_error.clone().map_or(Ok(()), Err)
    }
}
#[repr(C)]
#[derive(Default, Clone, Copy)]
pub struct NativeResult {
    stage: i32,
    class_id: i32,
    started: i32,
    cleanup_complete: i32,
    termination_kind: i32,
    termination_value: i32,
    stdin_written: usize,
    stdout_size: usize,
    stderr_size: usize,
    cleanup_failures: u32,
    ownership: usize,
}
struct Completed {
    receipt: NativeResult,
    stdout: Vec<u8>,
    stderr: Vec<u8>,
}
#[cfg(target_os = "macos")]
extern "C" {
    fn lo_native_process_execute(
        launcher: *const i8,
        embedded: i32,
        directory: *const i8,
        frame: *const u8,
        frame_size: usize,
        configuration: *const u8,
        configuration_size: usize,
        input: *const u8,
        input_size: usize,
        output: *mut u8,
        diagnostic: *mut u8,
        capacity: usize,
        cancelled: extern "C" fn(*const AtomicBool) -> i32,
        token: *const AtomicBool,
        timeout_ms: u64,
        result: *mut NativeResult,
    ) -> i32;
}
extern "C" fn cancelled(token: *const AtomicBool) -> i32 {
    unsafe { (*token).load(Ordering::SeqCst) as i32 }
}
fn termination(r: &NativeResult) -> Option<Handle> {
    match r.termination_kind {
        1 => Some(value(
            "Process.Termination",
            [
                ("kind".into(), text("exited")),
                ("exit_code".into(), int(r.termination_value)),
            ],
        )),
        2 => Some(value(
            "Process.Termination",
            [
                ("kind".into(), text("signaled")),
                ("signal".into(), int(r.termination_value)),
            ],
        )),
        _ => None,
    }
}
fn progress(r: &NativeResult) -> Handle {
    let stage = match r.stage {
        1 => "start",
        2 => "stdin",
        3 => "stdout",
        4 => "stderr",
        5 => "wait",
        6 => "release",
        7 => "cancel",
        _ => "start",
    };
    let class = match r.class_id {
        1 => "cancelled",
        2 => "host",
        3 => "limit",
        _ => "execution",
    };
    let mut fields = vec![
        ("stage".into(), text(stage)),
        ("start_confirmed".into(), boolean(r.started != 0)),
        ("stdin_written".into(), int(r.stdin_written)),
        ("stdout_bytes".into(), int(r.stdout_size)),
        ("stderr_bytes".into(), int(r.stderr_size)),
        ("cleanup_complete".into(), boolean(r.cleanup_complete != 0)),
        ("primary_class".into(), text(class)),
    ];
    if let Some(t) = termination(r) {
        fields.push(("termination".into(), t))
    }
    record(fields)
}
fn process_failure(site: &Site, r: &NativeResult) -> Fault {
    let progress = progress(r);
    let stage = progress.fields()["stage"].string().to_owned();
    let message = format!("Process execution failed during {stage}");
    let primary = if r.cleanup_complete == 0 {
        host("Process cleanup termination or containment: Process cleanup was not verified")
    } else {
        match r.class_id {
            1 => Fault::Host("runtime.cancelled", message.clone()),
            2 => host(message.clone()),
            3 => Fault::Host("runtime.limit", message.clone()),
            4 => Fault::Host("runtime.capability-denied", message.clone()),
            _ => {
                let mut activation = site.clone();
                activation.operation = "Process.run".into();
                return Fault::Recoverable(error(
                    &activation,
                    "Process.ExecutionFailed",
                    &message,
                    record([("progress".into(), progress)]),
                ));
            }
        }
    };
    let mut cleanup_errors = vec![];
    if r.cleanup_failures & 1 != 0 {
        cleanup_errors.push("Process pipe release failed".into())
    }
    if r.cleanup_failures & 2 != 0 {
        cleanup_errors.push("Process termination observation failed".into())
    }
    if r.cleanup_failures & 4 != 0 {
        cleanup_errors.push("Process resource release failed".into())
    }
    boundary_host_fault(primary.with_details(BoundaryDetails {
        process_progress: Some(progress.json()),
        cleanup_errors,
        ..Default::default()
    }))
}

// Trusted embedding boundary. Install before evaluation and before a session is shared.
// The callback retains all child ownership until it returns, including after cancellation.
// Callback code and userdata must outlive Session::wait_retained.
#[no_mangle]
pub unsafe extern "C" fn lo_context_process_driver(
    context: *mut Context,
    callback: Option<DriverCallback>,
    userdata: usize,
) -> i32 {
    let Some(context) = context.as_mut() else {
        return 0;
    };
    if context.owner != std::thread::current().id() || context.scopes != 0 || context.depth != 0 {
        return 0;
    }
    let Some(callback) = callback else { return 0 };
    let Some(session) = Arc::get_mut(&mut context.session) else {
        return 0;
    };
    let mut processes = empty();
    processes.driver = Some(Driver { callback, userdata });
    processes.launcher = "[trusted-driver]".into();
    processes.roots = vec!["/".into()];
    session.processes = Arc::new(processes);
    1
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_process_driver_limits(
    context: *mut Context,
    timeout_ms: u64,
    cleanup_ms: u64,
    output_bytes: usize,
) -> i32 {
    let Some(context) = context.as_mut() else {
        return 0;
    };
    if context.owner != std::thread::current().id() || context.scopes != 0 || context.depth != 0 {
        return 0;
    }
    if timeout_ms == 0
        || timeout_ms > 5000
        || cleanup_ms == 0
        || cleanup_ms > 1000
        || output_bytes == 0
        || output_bytes > 1 << 20
    {
        return 0;
    }
    let Some(session) = Arc::get_mut(&mut context.session) else {
        return 0;
    };
    let Some(processes) = Arc::get_mut(&mut session.processes) else {
        return 0;
    };
    if processes.driver.is_none() {
        return 0;
    }
    processes.timeout_ms = timeout_ms;
    processes.cleanup_ms = cleanup_ms;
    processes.output_bytes = output_bytes;
    1
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_process_peer(context: *mut Context) -> *mut Context {
    let Some(context) = context.as_ref() else {
        return std::ptr::null_mut();
    };
    if context.owner != std::thread::current().id() {
        return std::ptr::null_mut();
    }
    match Context::request(context.session.clone()) {
        Ok(peer) => Box::into_raw(Box::new(peer)),
        Err(_) => std::ptr::null_mut(),
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_process_close(context: *mut Context) -> i32 {
    let Some(context) = context.as_ref() else {
        return 0;
    };
    context.session.processes.close().is_ok() as i32
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_process_wait_retained(context: *mut Context) -> i32 {
    let Some(context) = context.as_ref() else {
        return 0;
    };
    context.session.processes.wait_retained().is_ok() as i32
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_process_ownership(context: *mut Context) -> u32 {
    let Some(context) = context.as_ref() else {
        return 0;
    };
    let state = context.session.processes.state.lock().unwrap();
    state.active as u32 | ((state.unresolved as u32) << 1) | ((state.quarantined as u32) << 2)
}
pub fn commit(context: &mut Context, site: &Site, target: &Handle) -> Outcome {
    let processes = context.session.processes.clone();
    if processes.launcher.is_empty() {
        return Err(boundary_host_fault(Fault::Host(
            "runtime.process-unavailable",
            UNAVAILABLE.into(),
        )));
    }
    context.check_cancel()?;
    let command = payload(target);
    let executable = payload(&command.fields["executable"]).fields["path"]
        .string()
        .to_owned();
    let directory = command.fields["directory"].string().to_owned();
    if processes.driver.is_none() && !processes.registrations.contains_key(&executable) {
        return Err(process_failure(
            site,
            &NativeResult {
                stage: 1,
                class_id: 2,
                cleanup_complete: 1,
                ..Default::default()
            },
        ));
    }
    if processes.driver.is_none()
        && (executable == processes.launcher
            || !canonical(&directory)
            || !processes
                .roots
                .iter()
                .any(|r| Path::new(&directory).starts_with(r)))
    {
        return Err(process_failure(
            site,
            &NativeResult {
                stage: 1,
                class_id: 4,
                cleanup_complete: 1,
                ..Default::default()
            },
        ));
    }
    if processes.driver.is_none() {
        let checked = processes
            .validate(&executable, || context.check_cancel())
            .and_then(|()| processes.validate(&processes.launcher, || context.check_cancel()));
        if let Err(fault) = checked {
            if matches!(
                fault.primary(),
                Fault::Host("runtime.cancelled", _) | Fault::RawCancellation
            ) {
                return Err(fault);
            }
            return Err(process_failure(
                site,
                &NativeResult {
                    stage: 1,
                    class_id: 2,
                    cleanup_complete: 1,
                    ..Default::default()
                },
            ));
        }
    }
    let arguments = command.fields["arguments"].items();
    let Data::Map(environment) = &command.fields["environment"].data else {
        return Err(host("invalid Process environment"));
    };
    if arguments.len() + 1 > 16 || environment.len() > 16 {
        return Err(process_failure(
            site,
            &NativeResult {
                stage: 1,
                cleanup_complete: 1,
                ..Default::default()
            },
        ));
    }
    let mut entries = vec![executable.clone(), directory.clone(), executable];
    entries.extend(arguments.iter().map(|a| a.string().to_owned()));
    let mut environment: Vec<_> = environment
        .iter()
        .map(|(k, v)| format!("{}={}", k.string(), v.string()))
        .collect();
    environment.sort();
    let env_count = environment.len();
    entries.extend(environment);
    let frame = frame(b"LOP1", &entries, arguments.len() + 1, env_count, 1024).map_err(|_| {
        process_failure(
            site,
            &NativeResult {
                stage: 1,
                cleanup_complete: 1,
                ..Default::default()
            },
        )
    })?;
    if frame.len() > 8208 {
        return Err(process_failure(
            site,
            &NativeResult {
                stage: 1,
                cleanup_complete: 1,
                ..Default::default()
            },
        ));
    }
    let Data::Bytes(input) = &command.fields["stdin"].data else {
        return Err(host("invalid Process stdin"));
    };
    let input = input.clone();
    {
        let mut state = processes.state.lock().unwrap();
        if state.closing || state.active || state.quarantined {
            return Err(process_failure(
                site,
                &NativeResult {
                    stage: 1,
                    class_id: 2,
                    cleanup_complete: 1,
                    ..Default::default()
                },
            ));
        }
        state.active = true;
    }
    let cancel = Arc::new(AtomicBool::new(false));
    let worker_cancel = cancel.clone();
    let worker_processes = processes.clone();
    let (tx, rx) = std::sync::mpsc::sync_channel(1);
    let spawned = std::thread::Builder::new()
        .name("lo-process-owner".into())
        .spawn(move || {
            let p = &worker_processes;
            let mut result = NativeResult::default();
            let mut stdout = vec![0; p.output_bytes];
            let mut stderr = vec![0; p.output_bytes];
            if let Some(driver) = p.driver {
                unsafe {
                    (driver.callback)(
                        driver.userdata,
                        frame.as_ptr(),
                        frame.len(),
                        input.as_ptr(),
                        input.len(),
                        stdout.as_mut_ptr(),
                        stderr.as_mut_ptr(),
                        p.output_bytes,
                        cancelled,
                        Arc::as_ptr(&worker_cancel),
                        p.timeout_ms,
                        &mut result,
                    );
                }
            } else {
                #[cfg(target_os = "macos")]
                unsafe {
                    use std::ffi::CString;
                    let launcher = CString::new(p.launcher.clone()).unwrap();
                    let directory =
                        CString::new(Path::new(&p.launcher).parent().unwrap().to_str().unwrap())
                            .unwrap();
                    lo_native_process_execute(
                        launcher.as_ptr(),
                        p.embedded as i32,
                        directory.as_ptr(),
                        frame.as_ptr(),
                        frame.len(),
                        p.configuration.as_ptr(),
                        p.configuration.len(),
                        input.as_ptr(),
                        input.len(),
                        stdout.as_mut_ptr(),
                        stderr.as_mut_ptr(),
                        p.output_bytes,
                        cancelled,
                        Arc::as_ptr(&worker_cancel),
                        p.timeout_ms,
                        &mut result,
                    );
                }
                #[cfg(not(target_os = "macos"))]
                {
                    let _ = (&frame, &input, &worker_cancel);
                    result.stage = 1;
                    result.class_id = 2;
                    result.cleanup_complete = 1;
                }
            }
            if !(0..=7).contains(&result.stage)
                || !(0..=4).contains(&result.class_id)
                || !(0..=1).contains(&result.started)
                || !(0..=1).contains(&result.cleanup_complete)
                || !(0..=2).contains(&result.termination_kind)
                || result.cleanup_failures & !7 != 0
                || (result.cleanup_complete == 1 && result.ownership != 0)
                || (result.termination_kind == 1 && result.termination_value < 0)
                || (result.termination_kind == 2 && result.termination_value <= 0)
                || (result.stage == 0 && result.class_id != 0)
                || (result.termination_kind != 0 && result.started != 1)
                || (result.started == 1
                    && result.termination_kind == 0
                    && result.cleanup_complete == 1)
            {
                result.stage = 5;
                result.class_id = 2;
                result.cleanup_complete = 0;
            }
            // A driver returning no termination and no failure has not settled ownership.
            if result.stage == 0 && result.termination_kind == 0 {
                result.stage = 1;
                result.class_id = 2;
                result.cleanup_complete = 0;
            }
            if result.stage == 0 && result.cleanup_complete == 0 {
                result.stage = 6;
                result.class_id = 2;
            }
            if result.stdout_size > p.output_bytes || result.stderr_size > p.output_bytes {
                result.stage = 5;
                result.class_id = 2;
                result.cleanup_complete = 0;
                result.stdout_size = result.stdout_size.min(p.output_bytes);
                result.stderr_size = result.stderr_size.min(p.output_bytes);
            }
            stdout.truncate(result.stdout_size);
            stderr.truncate(result.stderr_size);
            {
                let mut state = p.state.lock().unwrap();
                state.active = false;
                if result.cleanup_complete == 0 {
                    state.quarantined = true;
                    state.unresolved = true;
                    // No proof of release exists. Retain the registry and its frozen
                    // descriptors; a caller cannot discharge unknown ownership.
                    std::mem::forget(Box::new((result, worker_processes.clone())));
                }
                p.changed.notify_all();
            }
            let _ = tx.send(Completed {
                receipt: result,
                stdout,
                stderr,
            });
        });
    if spawned.is_err() {
        let mut state = processes.state.lock().unwrap();
        state.active = false;
        processes.changed.notify_all();
        drop(state);
        return Err(process_failure(
            site,
            &NativeResult {
                stage: 1,
                class_id: 2,
                cleanup_complete: 1,
                ..Default::default()
            },
        ));
    }

    let deadline =
        Instant::now() + Duration::from_millis(processes.timeout_ms + processes.cleanup_ms);
    let mut cancellation_started = None;
    let completed = loop {
        match rx.recv_timeout(Duration::from_millis(10)) {
            Ok(v) => break v,
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                processes.state.lock().unwrap().quarantined = true;
                return Err(host("Process cleanup was not verified"));
            }
            Err(_) => (),
        };
        if context.check_cancel().is_err() {
            cancel.store(true, Ordering::SeqCst);
            cancellation_started.get_or_insert_with(Instant::now);
        }
        if Instant::now() >= deadline
            || cancellation_started.map_or(false, |start| {
                Instant::now() - start >= Duration::from_millis(processes.cleanup_ms)
            })
        {
            cancel.store(true, Ordering::SeqCst);
            processes.state.lock().unwrap().quarantined = true;
            return Err(process_failure(
                site,
                &NativeResult {
                    stage: 7,
                    class_id: 1,
                    ..Default::default()
                },
            ));
        }
    };
    let r = &completed.receipt;
    if r.stage != 0 {
        return Err(process_failure(site, r));
    }
    let Some(termination) = termination(r) else {
        return Err(host("Process termination was not confirmed"));
    };
    let receipt = value(
        "Process.Result",
        [
            ("termination".into(), termination),
            (
                "stdout".into(),
                new(atom("Bytes"), Data::Bytes(completed.stdout)),
            ),
            (
                "stderr".into(),
                new(atom("Bytes"), Data::Bytes(completed.stderr)),
            ),
        ],
    );
    let completed = record([
        (
            "termination".into(),
            payload(&receipt).fields["termination"].clone(),
        ),
        ("stdout_bytes".into(), int(r.stdout_size)),
        ("stderr_bytes".into(), int(r.stderr_size)),
    ]);
    context.last_activation = Some(
        json!({"operation":"Process.run","span":{"file":site.raw["file"],"start":site.raw.coordinate("start"),"end":site.raw.coordinate("end")},"result_type":"Process.Result","process":completed.json()}),
    );
    context.charge(&receipt)?;
    Ok(receipt)
}
