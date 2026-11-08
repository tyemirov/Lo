use crate::{context::*, value::*};
use serde_json::{json, Value as Json};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs::{self, File, OpenOptions},
    os::unix::fs::{MetadataExt, OpenOptionsExt},
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicU32, Ordering},
        Arc, Condvar, Mutex,
    },
    time::{Duration, Instant},
};
#[path = "process_hash.rs"]
pub(crate) mod hashing;
#[path = "process_helper_identity.rs"]
mod helper_identity;
pub const UNAVAILABLE: &str =
    "Process activation requires a configured, qualified host execution profile";
#[path = "process_cleanup.rs"]
mod cleanup;
#[path = "process_context.rs"]
mod context_values;
#[path = "process_policy.rs"]
mod policy;
#[path = "process_io.rs"]
mod io;
pub use io::{DriverCallback, Limits, NativeResult};
pub use context_values::{
    capture_context, capture_context_with_environment, parse_initial_context,
};
pub use policy::Policy;
const MAX_COMMAND_BODY: usize = 1 << 20;
const MAX_COMMAND_ENTRY: usize = 1 << 16;
const MAX_COMMAND_ARGUMENTS: usize = 256;
const MAX_COMMAND_ENVIRONMENT: usize = 4096;
const MAX_POLICY_ENTRIES: usize = 16;
pub const SUPPORTED: &[&str] = &[
    "Process.Context.current",
    "Process.Context.new",
    "Process.Context.override_environment",
    "Process.Context.without_environment",
    "Process.Context.with_directory",
    "Process.Context.environment_value",
    "Process.Executable.resolve",
    "Process.Executable.new",
    "Process.Command.new",
    "Process.Command.with_timeout",
    "Process.Command.with_output",
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
        if self.kind == "Process.Context" {
            return self.fields["environment"].weight + self.fields["directory"].weight;
        }
        self.fields.values().map(|v| v.weight).sum()
    }
    pub fn json(&self) -> Json {
        if self.kind == "Process.Context" {
            return json!({"origin": self.fields["origin"].string(), "redacted": true});
        }
        Json::Object(
            self.fields
                .iter()
                .map(|(n, v)| (n.clone(), v.json()))
                .collect(),
        )
    }
    pub fn canonical_json(&self) -> String {
        if self.kind == "Process.Context" {
            return format!(
                "{{\"origin\":{},\"redacted\":true}}",
                serde_json::to_string(self.fields["origin"].string()).unwrap()
            );
        }
        let names: &[&str] = match self.kind.as_str() {
            "Process.Executable" => &["path"],
            "Process.Command" => &["context", "executable", "arguments", "stdin", "timeout_ms", "output"],
            "Process.Result" => &["termination", "stdout", "stderr", "output", "stdout_bytes", "stderr_bytes"],
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
        if self.kind == "Process.Context" {
            return context_values::redacted(self);
        }
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
        v.fields
            .insert("context".into(), snapshot(&v.fields["context"]));
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
                "Process.Context",
                "Process.Executable",
                "List<String>",
                "Bytes",
            ],
            "Process.Command!",
        ),
        "Process.Command.with_timeout" => (vec!["Process.Command", "Int"], "Process.Command!"),
        "Process.Command.with_output" => (vec!["Process.Command", "String"], "Process.Command!"),
        "Process.Termination.exit_code" | "Process.Termination.signal" => {
            (vec!["Process.Termination"], "Int!")
        }
        _ => return context_values::admit(site, &args),
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
pub fn execute(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
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
            for (index, arg) in args[2].items().iter().enumerate() {
                if arg.string().contains('\0') {
                    return invalid(
                        site,
                        "Process.InvalidCommand",
                        &format!("Process argument {index} contains NUL"),
                    );
                }
            }
            Ok(value(
                "Process.Command",
                ["context", "executable", "arguments", "stdin"]
                    .into_iter()
                    .zip(args.iter().cloned())
                    .map(|(n, v)| (n.into(), v))
                    .chain([("timeout_ms".into(), int(0)), ("output".into(), text("capture"))]),
            ))
        }
        "Process.Command.with_timeout" | "Process.Command.with_output" => {
            let field = if site.operation.ends_with("with_timeout") {
                let Ok(duration) = args[1].integer().to_string().parse::<u64>() else {
                    return invalid(site, "Process.InvalidCommand", "Process timeout requires positive representable milliseconds");
                };
                if duration == 0 || duration > i64::MAX as u64 {
                    return invalid(site, "Process.InvalidCommand", "Process timeout requires positive representable milliseconds");
                }
                "timeout_ms"
            } else {
                if !matches!(args[1].string(), "capture" | "forward") {
                    return invalid(site, "Process.InvalidCommand", "Process output requires capture or forward");
                }
                "output"
            };
            let mut fields = payload(&args[0]).fields.clone();
            fields.insert(field.into(), args[1].clone());
            Ok(value("Process.Command", fields))
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
        _ => context_values::execute(context, site, args),
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
    registration_sealed: bool,
    active: bool,
    unresolved: bool,
    closing: bool,
    quarantined: bool,
    releasing: bool,
    closed: bool,
    close_error: Option<String>,
}
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
    configurations: BTreeMap<String, Vec<u8>>,
    scopes: BTreeMap<String, policy::Scope>,
    roots: Vec<String>,
    limits: Limits,
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
        configurations: BTreeMap::new(),
        scopes: BTreeMap::new(),
        roots: vec![],
        limits: Limits::default(),
        state: Mutex::new(State {
            registration_sealed: false,
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
        let body_limit = if magic == b"LOP1" {
            MAX_COMMAND_BODY
        } else {
            65536
        };
        if entry.len() > maximum
            || entry.len() + 4 > body_limit - body.len()
            || entry.contains('\0')
        {
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
fn validate_base_policy(grants: &[String], launcher: &str, roots: &[String]) -> Result<(), String> {
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
    policy: &Policy,
    limits: Limits,
) -> Result<Processes, String> {
    let (grants, launcher, roots) = (&policy.grants, policy.launcher.as_str(), &policy.roots);
    let mut p = empty();
    p.limits = limits.validate()?;
    if grants.is_empty() && launcher.is_empty() && roots.is_empty() {
        return Ok(p);
    }
    if grants.is_empty() || launcher.is_empty() || roots.is_empty() {
        return Err("Process policy requires a launcher, at least one executable grant, and at least one read root".into());
    }
    if !supported() {
        return Err(UNAVAILABLE.into());
    }
    if grants.len() > MAX_POLICY_ENTRIES || roots.len() > MAX_POLICY_ENTRIES {
        return Err("Process startup failed during input".into());
    }
    if policy.scopes.values().any(|scope| {
        scope.writes.len() + scope.prunes.len() > MAX_POLICY_ENTRIES
            || scope.endpoints.len() > MAX_POLICY_ENTRIES
    }) {
        return Err("Process startup failed during input".into());
    }
    if !canonical(launcher) {
        return Err(startup_error("invalid native helper path"));
    }
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
    if !embedded && helper_hash != helper_identity::SHA256 {
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
    for executable in &commands {
        let scope = policy.scopes.get(executable).unwrap();
        let configuration = policy::configuration(&p, &commands, executable, scope)?;
        p.configurations.insert(executable.clone(), configuration);
        p.scopes.insert(executable.clone(), scope.clone());
    }
    Ok(p)
}
pub fn protection(paths: &[String]) -> Result<Processes, String> {
    let mut registry = empty();
    for path in paths {
        let file = open_read(path).map_err(|e| e.to_string())?;
        let digest = hashing::hex(&hashing::digest(&file)?);
        registry.register(path, &digest)?;
    }
    Ok(registry)
}
impl Processes {
    pub fn seal_registration(&self) {
        self.state.lock().unwrap().registration_sealed = true;
    }
    pub fn validate_image(
        &self,
        path: &str,
        check: impl FnMut() -> Result<(), Fault>,
    ) -> Result<(), Fault> {
        self.validate(path, check)
    }
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
        self.close_wait(Some(Duration::from_millis(self.limits.cleanup_ms)), false)
    }
    pub fn wait_retained(&self) -> Result<(), String> {
        self.close_wait(None, false)
    }
    pub fn wait_owned(&self) -> Result<(), String> {
        self.close_wait(None, true)
    }
    pub fn unresolved(&self) -> bool {
        self.state.lock().unwrap().unresolved
    }
    fn close_wait(&self, bound: Option<Duration>, reject_unknown: bool) -> Result<(), String> {
        let deadline = bound.map(|duration| Instant::now() + duration);
        let mut state = self.state.lock().unwrap();
        state.closing = true;
        while state.active || state.unresolved || state.releasing {
            if reject_unknown && state.unresolved && !state.active && !state.releasing {
                let message = registry_error(
                    "close",
                    "ownership remains quarantined without a release handle",
                );
                state.close_error.get_or_insert(message.clone());
                return Err(message);
            }
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
#[cfg(target_os = "macos")]
extern "C" {
    fn lo_native_process_execute(
        launcher: *const i8, embedded: i32, directory: *const i8,
        configuration: *const u8, configuration_size: usize,
        request: *const io::Request, result: *mut NativeResult,
    ) -> i32;
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
        2 | 4 => "host",
        3 => "limit",
        5 => "timeout",
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
                    if r.class_id == 5 { "Process.Timeout" } else { "Process.ExecutionFailed" },
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
fn driver_registration_ready(context: &Context) -> bool {
    context.owner == std::thread::current().id() && context.owns_session
        && !context.evaluated && !context.completed && context.final_result.is_none()
        && context.scopes == 0 && context.depth == 0 && context.weight == 0
        && context.remaining == context.session.steps && context.fault.is_none()
        && {
            let state = context.session.processes.state.lock().unwrap();
            !state.registration_sealed && !state.closing
        }
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_process_driver(
    context: *mut Context,
    callback: Option<DriverCallback>,
    userdata: usize,
) -> i32 {
    let Some(context) = context.as_mut() else {
        return 0;
    };
    if !driver_registration_ready(context) { return 0; }
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
    context: *mut Context, limits: *const Limits,
) -> i32 {
    let (Some(context), Some(limits)) = (context.as_mut(), limits.as_ref()) else { return 0; };
    if !driver_registration_ready(context) { return 0; }
    let Ok(limits) = limits.validate() else { return 0; };
    let Some(session) = Arc::get_mut(&mut context.session) else { return 0; };
    let Some(processes) = Arc::get_mut(&mut session.processes) else { return 0; };
    if processes.driver.is_none() { return 0; }
    processes.limits = limits;
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
    let requested = command.fields["timeout_ms"].integer().to_string().parse::<u64>().unwrap();
    let timeout_ms = if requested == 0 { processes.limits.timeout_ms } else { requested };
    let forward = command.fields["output"].string() == "forward";
    if timeout_ms > processes.limits.timeout_ms || (forward && processes.limits.forward_streams != 3) {
        return Err(process_failure(site, &NativeResult { stage: 1, class_id: 4, cleanup_complete: 1, ..Default::default() }));
    }
    if forward { context.session.console.authorize_process()?; }
    let executable = payload(&command.fields["executable"]).fields["path"]
        .string()
        .to_owned();
    let command_context = payload(&command.fields["context"]);
    let directory = command_context.fields["directory"].string().to_owned();
    if processes.driver.is_none() && !processes.registrations.contains_key(&executable) {
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
    if processes.driver.is_none() {
        let scope = processes
            .scopes
            .get(&executable)
            .ok_or_else(|| host("registered command has no scope"))?;
        if let Err(fault) = cleanup::validate(context, scope) {
            let class_id = match fault.primary() {
                Fault::Host("runtime.capability-denied", _) => 4,
                Fault::Host("runtime.limit", _) | Fault::Budget { .. } => 3,
                Fault::Host("runtime.cancelled", _) | Fault::RawCancellation => 1,
                _ => 2,
            };
            return Err(process_failure(
                site,
                &NativeResult {
                    stage: 1,
                    class_id,
                    cleanup_complete: 1,
                    ..Default::default()
                },
            ));
        }
    }
    let configuration = if processes.driver.is_some() {
        vec![]
    } else {
        processes
            .configurations
            .get(&executable)
            .ok_or_else(|| host("registered command has no configuration"))?
            .clone()
    };
    let arguments = command.fields["arguments"].items();
    let Data::Map(environment) = &command_context.fields["environment"].data else {
        return Err(host("invalid Process environment"));
    };
    if arguments.len() + 1 > MAX_COMMAND_ARGUMENTS || environment.len() > MAX_COMMAND_ENVIRONMENT {
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
    let frame = frame(
        b"LOP1",
        &entries,
        arguments.len() + 1,
        env_count,
        MAX_COMMAND_ENTRY,
    )
    .map_err(|_| {
        process_failure(
            site,
            &NativeResult {
                stage: 1,
                cleanup_complete: 1,
                ..Default::default()
            },
        )
    })?;
    if frame.len() > MAX_COMMAND_BODY + 16 {
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
    let cancel = Arc::new(AtomicU32::new(0));
    let worker_cancel = cancel.clone();
    let worker_processes = processes.clone();
    let worker_locks = context.session.locks.clone();
    let worker_entry = context.session.entry.clone();
    let (tx, rx) = std::sync::mpsc::sync_channel(1);
    let command_deadline = Instant::now() + Duration::from_millis(timeout_ms);
    let deadline = command_deadline + Duration::from_millis(processes.limits.cleanup_ms);
    let spawned = std::thread::Builder::new().name("lo-process-owner".into()).spawn(move || {
        let p = &worker_processes;
        let mut result = NativeResult::default();
        let output = io::Output { events: tx.clone(), stop: worker_cancel.clone(), state: Mutex::new(io::OutputState::default()), maximum: p.limits.output_bytes };
        let request = io::Request::new(&frame, &input, p.limits, timeout_ms, forward, &output);
        if let Some(driver) = p.driver {
            unsafe { (driver.callback)(driver.userdata, &request, &mut result); }
        } else {
            #[cfg(target_os = "macos")]
            unsafe {
                use std::ffi::CString;
                let launcher = CString::new(p.launcher.clone()).unwrap();
                let directory = CString::new(Path::new(&p.launcher).parent().unwrap().to_str().unwrap()).unwrap();
                lo_native_process_execute(launcher.as_ptr(), p.embedded as i32, directory.as_ptr(),
                    configuration.as_ptr(), configuration.len(), &request, &mut result);
            }
            #[cfg(not(target_os = "macos"))]
            {
                let _ = (&request, &configuration);
                result.stage = 1; result.class_id = 2; result.cleanup_complete = 1;
            }
        }
        let observed = output.state.lock().unwrap();
        if let Some((stage, class)) = observed.failure { result.stage = stage; result.class_id = class; }
        if !(0..=7).contains(&result.stage) || !(0..=5).contains(&result.class_id)
            || !(0..=1).contains(&result.started) || !(0..=1).contains(&result.cleanup_complete)
            || !(0..=2).contains(&result.termination_kind) || result.cleanup_failures & !7 != 0
            || (result.cleanup_complete == 1 && result.ownership != 0)
            || (result.stage == 0 && result.cleanup_failures != 0)
            || (result.termination_kind == 1 && result.termination_value < 0)
            || (result.termination_kind == 2 && result.termination_value <= 0)
            || (result.stage == 0 && result.class_id != 0)
            || (result.termination_kind != 0 && result.started != 1)
            || (result.started == 1 && result.termination_kind == 0 && result.cleanup_complete == 1)
            || result.stdin_written > input.len()
            || [result.stdout_size, result.stderr_size] != observed.counts {
            result.stage = 5; result.class_id = 2; result.cleanup_complete = 0;
            result.stdout_size = observed.counts[0]; result.stderr_size = observed.counts[1];
        }
        drop(observed);
        if result.stage == 0 && result.termination_kind == 0 {
            result.stage = 1; result.class_id = 2; result.cleanup_complete = 0;
        }
        if result.stage == 0 && result.cleanup_complete == 0 { result.stage = 6; result.class_id = 2; }
        {
            let mut state = p.state.lock().unwrap();
            state.active = false;
            if result.cleanup_complete == 0 {
                state.quarantined = true;
                state.unresolved = true;
                std::mem::forget(Box::new((result, worker_processes.clone(), worker_locks.clone(), worker_entry.clone())));
            }
            p.changed.notify_all();
        }
        let _ = tx.send(io::Event::Complete(result));
    });
    if spawned.is_err() {
        let mut state = processes.state.lock().unwrap();
        state.active = false;
        processes.changed.notify_all();
        drop(state);
        return Err(process_failure(site, &NativeResult { stage: 1, class_id: 2, cleanup_complete: 1, ..Default::default() }));
    }
    let mut stdout = Vec::new();
    let mut stderr = Vec::new();
    let mut primary = None;
    let mut stopping = None;
    let mut confirmed = [0usize; 2];
    let mut receipt = loop {
        if primary.is_none() && matches!(cancel.load(Ordering::SeqCst), 0 | 5) {
            if let Err(fault) = context.check_cancel() {
                if cancel.fetch_update(Ordering::SeqCst, Ordering::SeqCst, |current|
                    matches!(current, 0 | 5).then(|| stop_class(&fault))).is_ok() {
                    primary = Some(fault);
                    stopping.get_or_insert_with(Instant::now);
                }
            }
        }
        if Instant::now() >= command_deadline && cancel.compare_exchange(0, 5, Ordering::SeqCst, Ordering::SeqCst).is_ok() {
            stopping.get_or_insert(command_deadline);
        }
        if cancel.load(Ordering::SeqCst) != 0 { stopping.get_or_insert_with(Instant::now); }
        if Instant::now() >= deadline || stopping.is_some_and(|start| Instant::now() - start >= Duration::from_millis(processes.limits.cleanup_ms)) {
            processes.state.lock().unwrap().quarantined = true;
            return Err(process_failure(site, &NativeResult { stage: 7, class_id: cancel.load(Ordering::SeqCst) as i32,
                stdout_size: confirmed[0], stderr_size: confirmed[1], ..Default::default() }));
        }
        match rx.recv_timeout(Duration::from_millis(5)) {
            Ok(io::Event::Complete(result)) => break result,
            Ok(io::Event::Output { stream, bytes, reply }) => {
                let reason = cancel.load(Ordering::SeqCst);
                let delivered = if reason != 0 {
                    Err((0, if reason == 5 { host_timeout() } else { primary.clone().unwrap_or_else(|| host("Process output stopped")) }))
                } else if forward {
                    let console = context.session.console.clone();
                    match context.reserve_file(1, bytes.len() as u64 + 512) {
                        Ok(()) => {
                            let mut check = || {
                                context.check_cancel()?;
                                if Instant::now() >= command_deadline { return Err(host_timeout()); }
                                Ok(())
                            };
                            console.forward(if stream == 1 { "stdout" } else { "stderr" }, &bytes, &mut check)
                        },
                        Err(fault) => Err((0, fault)),
                    }
                } else {
                    let weight = (stdout.len() as u64).checked_add(stderr.len() as u64).and_then(|n| n.checked_add(bytes.len() as u64)).and_then(|n| n.checked_add(512)).ok_or_else(|| limit("Process output bytes"));
                    match weight.and_then(|weight| context.reserve_file(1, weight)) {
                        Ok(()) => {
                            let target = if stream == 1 { &mut stdout } else { &mut stderr };
                            if target.try_reserve(bytes.len()).is_err() { Err((0, host("Process output allocation failed"))) }
                            else { target.extend_from_slice(&bytes); Ok(bytes.len()) }
                        }
                        Err(fault) => Err((0, fault)),
                    }
                };
                let response = match delivered {
                    Ok(count) => (count, 0),
                    Err((count, fault)) => {
                        let class = stop_class(&fault);
                        if class != 5 && primary.is_none() { primary = Some(fault); }
                        cancel.store(class, Ordering::SeqCst);
                        stopping.get_or_insert_with(Instant::now);
                        (count, class as i32)
                    }
                };
                confirmed[(stream - 1) as usize] += response.0;
                let _ = reply.send(response);
            }
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                processes.state.lock().unwrap().quarantined = true;
                return Err(process_failure(site, &NativeResult { stage: 5, class_id: 2,
                    stdout_size: confirmed[0], stderr_size: confirmed[1], ..Default::default() }));
            }
            Err(_) => (),
        }
    };
    if receipt.cleanup_complete == 0 { return Err(process_failure(site, &receipt)); }
    if primary.is_none() && !(receipt.stage != 0 && (1..=4).contains(&receipt.class_id)) {
        if let Err(fault) = context.check_cancel() { primary = Some(fault); }
    }
    if let Some(primary) = primary {
        return Err(boundary_host_fault(primary.with_details(BoundaryDetails { process_progress: Some(progress(&receipt).json()), ..Default::default() })));
    }
    if receipt.stage == 0 && cancel.load(Ordering::SeqCst) != 0 {
        receipt.stage = 7; receipt.class_id = cancel.load(Ordering::SeqCst) as i32;
    }
    let r = &receipt;
    if r.stage != 0 { return Err(process_failure(site, r)); }
    let Some(termination) = termination(r) else {
        return Err(host("Process termination was not confirmed"));
    };
    let receipt = value(
        "Process.Result",
        [
            ("termination".into(), termination),
            (
                "stdout".into(),
                new(atom("Bytes"), Data::Bytes(stdout)),
            ),
            (
                "stderr".into(),
                new(atom("Bytes"), Data::Bytes(stderr)),
            ),
            ("output".into(), text(if forward { "forward" } else { "capture" })),
            ("stdout_bytes".into(), int(r.stdout_size)),
            ("stderr_bytes".into(), int(r.stderr_size)),
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
        json!({"operation":"Process.run","span":site.source_record(),"result_type":"Process.Result","process":completed.json()}),
    );
    context.charge(&receipt)?;
    Ok(receipt)
}

fn host_timeout() -> Fault { Fault::Host("runtime.process-timeout", "Process command deadline expired".into()) }
fn stop_class(fault: &Fault) -> u32 {
    match fault.primary() {
        Fault::RawCancellation | Fault::Host("runtime.cancelled", _) => 1,
        Fault::Budget { .. } | Fault::Host("runtime.limit", _) => 3,
        Fault::Host("runtime.capability-denied", _) => 4,
        Fault::Host("runtime.process-timeout", _) => 5,
        _ => 2,
    }
}
