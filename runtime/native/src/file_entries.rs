use super::*;
use num_bigint::BigInt;
use serde_json::{json, Value as Json};

#[path = "file_mutation_scan.rs"]
mod mutation_scan;
pub(crate) use mutation_scan::{validate_namespace, MutationAccess};

#[derive(Clone, PartialEq, Eq, PartialOrd, Ord)]
pub struct Directory {
    pub path: PathBuf,
    pub identity: (u64, u64),
}
#[derive(Clone)]
pub struct Observation {
    pub session: u64,
    pub lease: u64,
    pub directory: Directory,
    pub identity: (u64, u64),
}
#[derive(Clone)]
pub struct Guard {
    pub flow: usize,
    pub predicate: String,
    pub contexts: Vec<Handle>,
}
#[derive(Clone)]
pub enum Value {
    Entry {
        fields: Handle,
        observation: Option<Observation>,
    },
    Directory {
        fields: Handle,
        observation: Option<Observation>,
    },
    Scan {
        fields: Handle,
    },
    Content {
        append: bool,
        path: Handle,
        content: Handle,
    },
    Remove {
        entries: Handle,
        session: u64,
        payload_weight: u64,
        guard: Option<Guard>,
    },
}
impl Value {
    pub fn weight(&self) -> u64 {
        match self {
            Self::Entry {
                fields,
                observation,
            } => {
                fields.weight
                    + observation.as_ref().map_or(0, |observation| {
                        128 + observation.directory.path.as_os_str().len() as u64
                    })
            }
            Self::Directory {
                fields,
                observation,
            } => {
                fields.weight
                    + observation.as_ref().map_or(0, |observation| {
                        128 + observation.directory.path.as_os_str().len() as u64
                    })
            }
            Self::Scan { fields } => fields.weight,
            Self::Content {
                append,
                path,
                content,
            } => {
                if *append {
                    "File.Append.new"
                } else {
                    "File.Write.new"
                }
                .len() as u64
                    + path.weight
                    + content.weight
            }
            Self::Remove { payload_weight, .. } => *payload_weight,
        }
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        match self {
            Self::Entry { fields, .. } | Self::Directory { fields, .. } | Self::Scan { fields } => {
                Some(fields.fields())
            }
            _ => None,
        }
    }
    pub fn json(&self) -> Json {
        match self {
            Self::Entry { fields, .. } | Self::Directory { fields, .. } | Self::Scan { fields } => {
                Json::Object(
                    fields
                        .fields()
                        .iter()
                        .map(|(name, value)| (name.clone(), value.json()))
                        .collect(),
                )
            }
            Self::Content {
                append,
                path,
                content,
            } => {
                json!({"operation":if *append {"File.Append.new"}else{"File.Write.new"},"file_path":path.json(),"file_content":content.json()})
            }
            Self::Remove { entries, guard, .. } => {
                let mut result = json!({"operation":"File.Remove.new","entries":entries.json()});
                if let Some(guard) = guard {
                    result["guard"] = json!({"predicate":guard.predicate,"context":guard.contexts.iter().map(|v|crate::value::snapshot(v).json()).collect::<Vec<_>>()});
                }
                result
            }
        }
    }
    pub fn canonical_json(&self) -> String {
        match self {
            Self::Entry { fields, .. } | Self::Directory { fields, .. } | Self::Scan { fields } => {
                format!(
                    "{{{}}}",
                    fields
                        .fields()
                        .iter()
                        .map(|(name, value)| format!(
                            "{}:{}",
                            serde_json::to_string(name).unwrap(),
                            value.canonical_json()
                        ))
                        .collect::<Vec<_>>()
                        .join(",")
                )
            }
            Self::Content {
                append,
                path,
                content,
            } => format!(
                "{{\"operation\":\"{}\",\"file_path\":{},\"file_content\":{}}}",
                if *append {
                    "File.Append.new"
                } else {
                    "File.Write.new"
                },
                path.canonical_json(),
                content.canonical_json()
            ),
            Self::Remove { entries, guard, .. } => {
                let suffix = guard
                    .as_ref()
                    .map(|guard| {
                        format!(
                            ",\"guard\":{{\"predicate\":{},\"context\":[{}]}}",
                            serde_json::to_string(&guard.predicate).unwrap(),
                            guard
                                .contexts
                                .iter()
                                .map(|v| crate::value::snapshot(v).canonical_json())
                                .collect::<Vec<_>>()
                                .join(",")
                        )
                    })
                    .unwrap_or_default();
                format!(
                    "{{\"operation\":\"File.Remove.new\",\"entries\":{}{}}}",
                    entries.canonical_json(),
                    suffix
                )
            }
        }
    }
    pub fn output(&self) -> String {
        self.canonical_json()
    }
    pub fn snapshot(&self) -> Self {
        match self {
            Self::Entry { fields, .. } => Self::Entry {
                fields: fields.clone(),
                observation: None,
            },
            Self::Directory { fields, .. } => Self::Directory {
                fields: crate::value::snapshot(fields),
                observation: None,
            },
            Self::Scan { fields } => Self::Scan {
                fields: crate::value::snapshot(fields),
            },
            Self::Remove { entries, guard, .. } => Self::Remove {
                entries: crate::value::snapshot(entries),
                session: 0,
                payload_weight: self.weight() + 16,
                guard: guard.as_ref().map(|g| Guard {
                    flow: g.flow,
                    predicate: g.predicate.clone(),
                    contexts: g.contexts.iter().map(crate::value::snapshot).collect(),
                }),
            },
            _ => self.clone(),
        }
    }
}
pub fn adapter(typ: &str, value: Value) -> Handle {
    new(
        atom(typ),
        Data::Adapter(crate::adapters::AdapterValue::File(value)),
    )
}
fn identity(info: &std::fs::Metadata) -> (u64, u64) {
    (info.dev(), info.ino())
}
#[derive(Clone)]
pub(super) struct FileInfo {
    pub identity: (u64, u64),
    size: u64,
    modified: i64,
    nanoseconds: i64,
    links: u64,
    owner: u32,
    accessed: i64,
    accessed_nsec: i64,
    changed: i64,
    changed_nsec: i64,
    kind: &'static str,
}
impl FileInfo {
    fn len(&self) -> u64 {
        self.size
    }
    fn mtime(&self) -> i64 {
        self.modified
    }
    fn mtime_nsec(&self) -> i64 {
        self.nanoseconds
    }
    fn nlink(&self) -> u64 {
        self.links
    }
    fn file_type(&self) -> FileType {
        FileType(self.kind)
    }
    fn is_dir(&self) -> bool {
        self.kind == "directory"
    }
}
struct FileType(&'static str);
impl FileType {
    fn is_file(&self) -> bool {
        self.0 == "regular"
    }
    fn is_symlink(&self) -> bool {
        self.0 == "symlink"
    }
}
pub(super) fn lstat_at(parent: &File, name: &str) -> io::Result<FileInfo> {
    let name = CString::new(name).map_err(|_| io::Error::other("invalid argument"))?;
    let mut information = std::mem::MaybeUninit::<libc::stat>::uninit();
    if unsafe {
        libc::fstatat(
            parent.as_raw_fd(),
            name.as_ptr(),
            information.as_mut_ptr(),
            libc::AT_SYMLINK_NOFOLLOW,
        )
    } < 0
    {
        return Err(io::Error::last_os_error());
    }
    let information = unsafe { information.assume_init() };
    // Only the file-kind bits classify entries; permission bits are not read.
    let kind = match information.st_mode & libc::S_IFMT {
        libc::S_IFREG => "regular",
        libc::S_IFDIR => "directory",
        libc::S_IFLNK => "symlink",
        _ => "other",
    };
    Ok(FileInfo {
        identity: (information.st_dev as u64, information.st_ino as u64),
        size: information.st_size as u64,
        modified: information.st_mtime as i64,
        nanoseconds: information.st_mtime_nsec as i64,
        links: information.st_nlink as u64,
        owner: information.st_uid as u32,
        accessed: information.st_atime as i64,
        accessed_nsec: information.st_atime_nsec as i64,
        changed: information.st_ctime as i64,
        changed_nsec: information.st_ctime_nsec as i64,
        kind,
    })
}
fn observed_at(
    context: &Context,
    parent: &File,
    root: &Path,
    name: &str,
    phase: u32,
) -> Result<FileInfo, super::observer::Failure> {
    use super::observer::{Failure, Metadata};
    let mut info = lstat_at(parent, name).map_err(Failure::IO)?;
    let mut metadata = Metadata {
        owner_uid: info.owner as u64,
        accessed_sec: info.accessed,
        accessed_nsec: info.accessed_nsec,
        modified_sec: info.modified,
        modified_nsec: info.nanoseconds,
        changed_sec: info.changed,
        changed_nsec: info.changed_nsec,
        link_count: info.links,
        size_bytes: info.size,
    };
    super::observer::apply(context, root, name, phase, &mut metadata)?;
    info.owner = metadata.owner_uid as u32;
    info.accessed = metadata.accessed_sec;
    info.accessed_nsec = metadata.accessed_nsec;
    info.modified = metadata.modified_sec;
    info.nanoseconds = metadata.modified_nsec;
    info.changed = metadata.changed_sec;
    info.changed_nsec = metadata.changed_nsec;
    info.links = metadata.link_count;
    info.size = metadata.size_bytes;
    Ok(info)
}
fn observation_failure(
    error: super::observer::Failure,
    site: &Site,
    root: &Handle,
    kind: &str,
    path: &Path,
) -> Fault {
    match error {
        super::observer::Failure::IO(error) => {
            list_failure(site, root, kind, path, &error.to_string())
        }
        super::observer::Failure::Host(fault) => fault,
    }
}
fn exact_path(path: &Path) -> Result<(), String> {
    let resolved = std::fs::canonicalize(path).map_err(|error| {
        format!(
            "resolve file directory {}: lstat {}: {}",
            quote(path),
            path.display(),
            system_error(&error)
        )
    })?;
    if resolved != path {
        return Err(format!(
            "file directory {} traverses a symlink",
            quote(path)
        ));
    }
    Ok(())
}
pub(super) fn open_exact_roots(base: &Path, paths: &[String]) -> Result<Vec<Grant>, String> {
    let mut roots = Vec::new();
    let mut seen = BTreeSet::new();
    for path in paths {
        let absolute = clean(&base.join(path));
        if !seen.insert(absolute.clone()) {
            return Err(format!("duplicate adapter root {}", quote(&absolute)));
        }
        let mut prefix = absolute.clone();
        loop {
            match std::fs::symlink_metadata(&prefix) {
                Ok(_) => {
                    exact_path(&prefix)?;
                    break;
                }
                Err(error) if error.kind() == io::ErrorKind::NotFound => {
                    if !prefix.pop() {
                        return Err(format!(
                            "observe adapter root {}: {}",
                            quote(&absolute),
                            system_error(&error)
                        ));
                    }
                }
                Err(error) => {
                    return Err(format!(
                        "observe adapter root {}: {}",
                        quote(&absolute),
                        system_error(&error)
                    ))
                }
            }
        }
        let inert = prefix != absolute;
        let directory = File::open(&prefix).map_err(|error| {
            format!(
                "open adapter root {}: {}",
                quote(&absolute),
                system_error(&error)
            )
        })?;
        if !directory
            .metadata()
            .map_err(|error| error.to_string())?
            .is_dir()
        {
            return Err(format!(
                "open adapter root {}: not a directory",
                quote(&absolute)
            ));
        }
        roots.push(Grant {
            path: absolute,
            directory,
            inert,
        });
    }
    Ok(roots)
}
fn denied(operation: &str, path: &Path) -> Fault {
    Fault::Host(
        "runtime.capability-denied",
        format!("{operation} {} has no host capability", quote(path)),
    )
}
fn file_fault(site: &Site, kind: &str, message: &str, input: Handle) -> Fault {
    Fault::Recoverable(error(site, kind, message, input))
}
fn operation_input(root: &Handle) -> Handle {
    record([("root".into(), root.clone())])
}
fn list_failure(site: &Site, root: &Handle, kind: &str, path: &Path, message: &str) -> Fault {
    file_fault(
        site,
        kind,
        &format!("{kind}: {} {}: {message}", site.operation, quote(path)),
        operation_input(root),
    )
}
fn exact_grant(
    roots: &[Grant],
    base: &Path,
    operation: &str,
    path: &str,
    site: &Site,
    root: &Handle,
) -> Result<(File, Directory), Fault> {
    let path = clean(&base.join(path));
    let grant = roots
        .iter()
        .find(|grant| grant.path == path)
        .ok_or_else(|| denied(operation, &path))?;
    if grant.inert {
        if matches!(operation, "File.list" | "File.directory" | "File.scan")
            && matches!(std::fs::symlink_metadata(&path),Err(error) if error.kind()==io::ErrorKind::NotFound)
        {
            return Err(file_fault(
                site,
                "File.DirectoryAbsent",
                &format!("File.DirectoryAbsent: directory {} is absent", quote(&path)),
                operation_input(root),
            ));
        }
        return Err(denied(operation, &path));
    }
    if let Err(message) = exact_path(&path) {
        if matches!(operation, "File.list" | "File.directory" | "File.scan")
            && matches!(std::fs::symlink_metadata(&path),Err(error) if error.kind()==io::ErrorKind::NotFound)
        {
            return Err(file_fault(
                site,
                "File.DirectoryAbsent",
                &format!("File.DirectoryAbsent: {message}"),
                operation_input(root),
            ));
        }
        return Err(denied(operation, &path));
    }
    let current = std::fs::symlink_metadata(&path).map_err(|_| denied(operation, &path))?;
    let retained = grant.directory.metadata().map_err(|error| {
        host(format!(
            "{operation} {}: {}",
            quote(&path),
            system_error(&error)
        ))
    })?;
    if !current.is_dir() || identity(&current) != identity(&retained) {
        return Err(denied(operation, &path));
    }
    Ok((
        grant
            .directory
            .try_clone()
            .map_err(|error| host(error.to_string()))?,
        Directory {
            path,
            identity: identity(&retained),
        },
    ))
}
struct DirectoryStream {
    pointer: *mut libc::DIR,
}
impl DirectoryStream {
    fn open(directory: &File) -> io::Result<Self> {
        let name = CString::new(".").unwrap();
        let fd = unsafe { openat(directory.as_raw_fd(), name.as_ptr(), FLAGS[1] | FLAGS[2]) };
        if fd < 0 {
            return Err(io::Error::last_os_error());
        }
        let pointer = unsafe { libc::fdopendir(fd) };
        if pointer.is_null() {
            let error = io::Error::last_os_error();
            unsafe { close(fd) };
            return Err(error);
        }
        Ok(Self { pointer })
    }
    fn next(&mut self) -> io::Result<Option<OsString>> {
        loop {
            #[cfg(target_os = "macos")]
            unsafe {
                *libc::__error() = 0;
            }
            #[cfg(target_os = "linux")]
            unsafe {
                *libc::__errno_location() = 0;
            }
            let entry = unsafe { libc::readdir(self.pointer) };
            if entry.is_null() {
                let error = io::Error::last_os_error();
                return if error.raw_os_error() == Some(0) {
                    Ok(None)
                } else {
                    Err(error)
                };
            }
            let name = unsafe { CStr::from_ptr((*entry).d_name.as_ptr()) }.to_bytes();
            if name == b"." || name == b".." {
                continue;
            }
            return Ok(Some(OsString::from_vec(name.to_vec())));
        }
    }
    fn close(mut self) -> io::Result<()> {
        let pointer = std::mem::replace(&mut self.pointer, std::ptr::null_mut());
        if unsafe { libc::closedir(pointer) } < 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(())
        }
    }
}
impl Drop for DirectoryStream {
    fn drop(&mut self) {
        if !self.pointer.is_null() {
            unsafe {
                libc::closedir(self.pointer);
            }
        }
    }
}
fn timestamp(seconds: i64, nanoseconds: i64) -> Result<i64, String> {
    if !(0..1_000_000_000).contains(&nanoseconds) {
        return Err("invalid timestamp nanoseconds".into());
    }
    i64::try_from((seconds as i128) * 1_000_000_000 + nanoseconds as i128)
        .map_err(|_| "timestamp exceeds Unix nanosecond range".into())
}
fn metadata_times(info: &FileInfo) -> Result<(i64, i64, i64), String> {
    Ok((
        timestamp(info.accessed, info.accessed_nsec)?,
        timestamp(info.modified, info.nanoseconds)?,
        timestamp(info.changed, info.changed_nsec)?,
    ))
}
fn entry_value(
    context: &mut Context,
    site: &Site,
    observation: &Directory,
    lease: u64,
    name: &str,
    info: &FileInfo,
    prior_weight: u64,
    kind: &str,
) -> Outcome {
    let (accessed, modified, changed) = metadata_times(info).map_err(|message| {
        list_failure(
            site,
            &text(name),
            kind,
            &observation.path.join(name),
            &message,
        )
    })?;
    let path_value = new(
        atom("File.Path"),
        Data::Text(observation.path.join(name).to_string_lossy().into_owned()),
    );
    let fields = [
        ("path", path_value),
        ("name", text(name)),
        ("kind", text(info.kind)),
        ("size_bytes", int(info.len())),
        (
            "modified_unix_ns",
            new(atom("Int"), Data::Int(BigInt::from(modified))),
        ),
        ("link_count", int(info.nlink())),
        ("owner_uid", int(info.owner)),
        (
            "accessed_unix_ns",
            new(atom("Int"), Data::Int(BigInt::from(accessed))),
        ),
        (
            "changed_unix_ns",
            new(atom("Int"), Data::Int(BigInt::from(changed))),
        ),
    ];
    let weight = 16
        + 128
        + observation.path.as_os_str().len() as u64
        + 16
        + fields
            .iter()
            .map(|(name, value)| name.len() as u64 + value.weight)
            .sum::<u64>();
    context.reserve_file(0, prior_weight + weight)?;
    Ok(adapter(
        "File.Entry",
        Value::Entry {
            fields: record(fields.into_iter().map(|(k, v)| (k.into(), v))),
            observation: Some(Observation {
                session: context.files.session,
                lease,
                directory: observation.clone(),
                identity: info.identity,
            }),
        },
    ))
}
fn directory_value(
    context: &mut Context,
    site: &Site,
    root: &Handle,
    directory: &File,
    observation: &Directory,
    lease: u64,
    prior_weight: u64,
) -> Outcome {
    context.reserve_file(1, prior_weight)?;
    let failure_kind = if site.operation == "File.directory" {
        "File.DirectoryFailed"
    } else {
        "File.ListFailed"
    };
    let info = observed_at(context, directory, &observation.path, ".", 1)
        .map_err(|error| observation_failure(error, site, root, failure_kind, &observation.path))?;
    if !info.is_dir() || info.identity != observation.identity {
        return Err(denied(&site.operation, &observation.path));
    }
    let (accessed, modified, changed) = metadata_times(&info)
        .map_err(|message| list_failure(site, root, failure_kind, &observation.path, &message))?;
    let fields = [
        (
            "path",
            new(
                atom("File.Path"),
                Data::Text(observation.path.to_string_lossy().into_owned()),
            ),
        ),
        ("owner_uid", int(info.owner)),
        (
            "accessed_unix_ns",
            new(atom("Int"), Data::Int(BigInt::from(accessed))),
        ),
        (
            "modified_unix_ns",
            new(atom("Int"), Data::Int(BigInt::from(modified))),
        ),
        (
            "changed_unix_ns",
            new(atom("Int"), Data::Int(BigInt::from(changed))),
        ),
    ];
    let weight = 16
        + 128
        + observation.path.as_os_str().len() as u64
        + 16
        + fields
            .iter()
            .map(|(name, value)| name.len() as u64 + value.weight)
            .sum::<u64>();
    context.reserve_file(0, prior_weight + weight)?;
    Ok(adapter(
        "File.Directory",
        Value::Directory {
            fields: record(fields.into_iter().map(|(k, v)| (k.into(), v))),
            observation: Some(Observation {
                session: context.files.session,
                lease,
                directory: observation.clone(),
                identity: observation.identity,
            }),
        },
    ))
}
fn finish_observation(
    context: &mut Context,
    observation: Directory,
    directory: File,
    retained: File,
    fresh: bool,
    lease: u64,
    result: Outcome,
) -> Outcome {
    let mut result = match (result, close_file(directory)) {
        (Err(primary), Err(cleanup)) => Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => Err(primary),
        (Ok(_), Err(cleanup)) => Err(host(system_error(&cleanup))),
        (Ok(value), Ok(())) => Ok(value),
    };
    if result.is_ok() && fresh {
        context
            .files
            .retain_observation(observation, lease, retained);
    } else if let Err(cleanup) = close_file(retained) {
        result = Err(match result {
            Err(primary) => cleanup_failure(primary, cleanup),
            Ok(_) => host(system_error(&cleanup)),
        });
    }
    result
}
pub(super) fn directory(context: &mut Context, site: &Site, root: &Handle) -> Outcome {
    context.check_cancel()?;
    let (directory, observation) = exact_grant(
        &context.files.lists,
        &context.files.base,
        "File.directory",
        root.string(),
        site,
        root,
    )?;
    let fresh = !context.files.observed.contains_key(&observation);
    if fresh
        && context.values.limits.file_directories != 0
        && context.files.observed.len() as u64 >= context.values.limits.file_directories
    {
        return Err(limit("retained file directories"));
    }
    let lease = if fresh {
        let lease = context.files.next_observation_lease;
        context.files.next_observation_lease += 1;
        lease
    } else {
        context.files.observed_leases[&observation]
    };
    let retained = directory
        .try_clone()
        .map_err(|error| host(error.to_string()))?;
    let result = (|| {
        let value = directory_value(context, site, root, &directory, &observation, lease, 16)?;
        let (check, current) = exact_grant(
            &context.files.lists,
            &context.files.base,
            "File.directory",
            root.string(),
            site,
            root,
        )?;
        close_file(check).map_err(|error| host(system_error(&error)))?;
        if current != observation {
            return Err(denied("File.directory", &observation.path));
        }
        Ok(value)
    })();
    finish_observation(
        context,
        observation,
        directory,
        retained,
        fresh,
        lease,
        result,
    )
}
#[derive(Clone, Copy, PartialEq)]
enum Discovery {
    Strict,
    ReportVanished,
}
pub(super) fn list(context: &mut Context, site: &Site, root: &Handle) -> Outcome {
    discover(context, site, root, Discovery::Strict)
}
pub(super) fn scan(context: &mut Context, site: &Site, root: &Handle) -> Outcome {
    discover(context, site, root, Discovery::ReportVanished)
}
fn discover(context: &mut Context, site: &Site, root: &Handle, mode: Discovery) -> Outcome {
    context.check_cancel()?;
    let operation = if mode == Discovery::Strict {
        "File.list"
    } else {
        "File.scan"
    };
    let (directory, observation) = exact_grant(
        &context.files.lists,
        &context.files.base,
        operation,
        root.string(),
        site,
        root,
    )?;
    let fresh = !context.files.observed.contains_key(&observation);
    if fresh
        && context.values.limits.file_directories != 0
        && context.files.observed.len() as u64 >= context.values.limits.file_directories
    {
        return Err(limit("retained file directories"));
    }
    let lease = if fresh {
        let lease = context.files.next_observation_lease;
        context.files.next_observation_lease += 1;
        lease
    } else {
        context.files.observed_leases[&observation]
    };
    let retained = directory
        .try_clone()
        .map_err(|error| host(error.to_string()))?;
    let mut stream = DirectoryStream::open(&directory).map_err(|error| {
        list_failure(
            site,
            root,
            "File.ListFailed",
            &observation.path,
            &error.to_string(),
        )
    })?;
    let result = (|| {
        let directory_observation = if mode == Discovery::ReportVanished {
            Some(directory_value(
                context,
                site,
                root,
                &directory,
                &observation,
                lease,
                128,
            )?)
        } else {
            None
        };
        let mut weight = directory_observation.as_ref().map_or(32, |v| {
            96 + "directoryentriesvanished_names".len() as u64 + v.weight
        });
        let mut items = Vec::new();
        let mut vanished = Vec::new();
        let mut names = BTreeSet::new();
        loop {
            context.check_cancel()?;
            let Some(filename) = stream.next().map_err(|error| {
                list_failure(
                    site,
                    root,
                    "File.ListFailed",
                    &observation.path,
                    &error.to_string(),
                )
            })?
            else {
                break;
            };
            if context.values.limits.file_entries != 0
                && names.len() as u64 >= context.values.limits.file_entries
            {
                return Err(limit("directory entries"));
            }
            context.reserve_file(1, weight)?;
            let name = filename.to_str().ok_or_else(|| {
                list_failure(
                    site,
                    root,
                    "File.InvalidName",
                    &observation.path,
                    "invalid UTF-8 filename",
                )
            })?;
            if !names.insert(name.to_owned()) {
                return Err(list_failure(
                    site,
                    root,
                    "File.ListFailed",
                    &observation.path,
                    "duplicate directory entry",
                ));
            }
            let info = match observed_at(context, &directory, &observation.path, name, 2) {
                Ok(info) => info,
                Err(super::observer::Failure::IO(error))
                    if mode == Discovery::ReportVanished
                        && error.kind() == io::ErrorKind::NotFound =>
                {
                    context.reserve_file(0, weight + 16 + name.len() as u64)?;
                    let value = text(name);
                    weight += value.weight;
                    vanished.push(value);
                    continue;
                }
                Err(error) => {
                    return Err(observation_failure(
                        error,
                        site,
                        root,
                        "File.ListFailed",
                        &observation.path.join(name),
                    ))
                }
            };
            let entry = entry_value(
                context,
                site,
                &observation,
                lease,
                name,
                &info,
                weight,
                "File.ListFailed",
            )?;
            weight += entry.weight;
            items.push(entry);
        }
        let count = names.len() as u64;
        let sort_steps = if count > 1 {
            count * (64 - (count - 1).leading_zeros() as u64)
        } else {
            0
        };
        context.reserve_file(sort_steps, weight)?;
        items.sort_by(|a, b| a.fields()["name"].string().cmp(b.fields()["name"].string()));
        let entries = new(Type::List(Box::new(atom("File.Entry"))), Data::List(items));
        if let Some(directory_observation) = directory_observation {
            vanished.sort_by(|a, b| a.string().cmp(b.string()));
            let (check, current) = exact_grant(
                &context.files.lists,
                &context.files.base,
                operation,
                root.string(),
                site,
                root,
            )?;
            close_file(check).map_err(|error| host(system_error(&error)))?;
            if current != observation {
                return Err(denied(operation, &observation.path));
            }
            Ok(adapter(
                "File.Scan",
                Value::Scan {
                    fields: record([
                        ("directory".into(), directory_observation),
                        ("entries".into(), entries),
                        (
                            "vanished_names".into(),
                            new(Type::List(Box::new(atom("String"))), Data::List(vanished)),
                        ),
                    ]),
                },
            ))
        } else {
            Ok(entries)
        }
    })();
    let result = match (result, stream.close()) {
        (Err(primary), Err(cleanup)) => Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => Err(primary),
        (Ok(_), Err(cleanup)) => Err(host(format!(
            "boundary operation failed: {}",
            system_error(&cleanup)
        ))),
        (Ok(value), Ok(())) => Ok(value),
    };
    finish_observation(
        context,
        observation,
        directory,
        retained,
        fresh,
        lease,
        result,
    )
}

pub(super) fn selection(context: &mut Context, site: &Site, entries: &Handle) -> Outcome {
    let items = entries.items();
    if context.values.limits.file_entries != 0
        && items.len() as u64 > context.values.limits.file_entries
    {
        return Err(limit("file removal entries"));
    }
    context.reserve_file(
        items.len() as u64,
        entries.weight + 32 + "File.Remove.new".len() as u64,
    )?;
    let mut names = BTreeSet::new();
    let mut directory = None;
    let mut lease = None;
    for entry in items {
        let Data::Adapter(crate::adapters::AdapterValue::File(Value::Entry {
            fields,
            observation: Some(observation),
        })) = &entry.data
        else {
            return Err(file_fault(
                site,
                "File.InvalidSelection",
                "removal requires observed file entries",
                record([("entries".into(), entries.clone())]),
            ));
        };
        let name = fields.fields()["name"].string();
        if observation.session != context.files.session
            || lease.is_some_and(|prior| prior != observation.lease)
            || fields.fields()["kind"].string() != "regular"
            || !names.insert(name)
            || directory
                .as_ref()
                .is_some_and(|prior| prior != &observation.directory)
        {
            return Err(file_fault(
                site,
                "File.InvalidSelection",
                "invalid removal selection",
                record([("entries".into(), entries.clone())]),
            ));
        }
        directory = Some(observation.directory.clone());
        lease = Some(observation.lease);
    }
    Ok(adapter(
        "File.Remove",
        Value::Remove {
            entries: entries.clone(),
            session: context.files.session,
            payload_weight: "File.Remove.new".len() as u64 + entries.weight,
            guard: None,
        },
    ))
}
pub(super) fn selection_checked(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let flow = match &args
        .last()
        .ok_or_else(|| host("missing removal predicate"))?
        .data
    {
        Data::Flow(flow) => *flow,
        _ => return Err(host("invalid removal predicate")),
    };
    let predicate = context
        .program
        .flows
        .get(flow)
        .ok_or_else(|| host("unknown removal predicate"))?
        .identity
        .clone();
    let contexts = args[1..args.len() - 1].to_vec();
    let selected = selection(context, site, &args[0])?;
    let Data::Adapter(crate::adapters::AdapterValue::File(Value::Remove {
        entries,
        session,
        payload_weight,
        ..
    })) = &selected.data
    else {
        return Err(host("invalid removal construction"));
    };
    let payload_weight = *payload_weight
        + 32
        + predicate.len() as u64
        + contexts.iter().map(|v| v.weight).sum::<u64>();
    context.reserve_file(0, 16 + payload_weight + 16)?;
    Ok(adapter(
        "File.Remove",
        Value::Remove {
            entries: entries.clone(),
            session: *session,
            payload_weight,
            guard: Some(Guard {
                flow,
                predicate,
                contexts,
            }),
        },
    ))
}
fn guard_rejection(
    site: &Site,
    entries: &Handle,
    total: usize,
    completed: usize,
    bytes: &BigInt,
    index: usize,
    phase: &str,
    guard: &Guard,
) -> Fault {
    let mut activation = site.clone();
    activation.operation = "File.remove".into();
    file_fault(
        &activation,
        "File.EntryChanged",
        "File.EntryChanged: removal predicate returned false",
        record([
            ("entries".into(), crate::value::snapshot(entries)),
            (
                "progress".into(),
                progress(total, completed, bytes, index as i64, phase),
            ),
            (
                "guard".into(),
                record([
                    ("predicate".into(), text(&guard.predicate)),
                    ("phase".into(), text(phase)),
                    ("index".into(), int(index)),
                    ("condition".into(), text("predicate-false")),
                ]),
            ),
        ]),
    )
}
fn evaluate_guard(
    context: &mut Context,
    site: &Site,
    info: &FileInfo,
    entry: &Handle,
    guard: &Guard,
    phase: &str,
) -> Result<bool, Fault> {
    let Data::Adapter(crate::adapters::AdapterValue::File(Value::Entry {
        observation: Some(observation),
        ..
    })) = &entry.data
    else {
        return Err(host("invalid guarded entry"));
    };
    let current = entry_value(
        context,
        site,
        &observation.directory,
        observation.lease,
        entry.fields()["name"].string(),
        info,
        0,
        "File.RemoveFailed",
    )?;
    context.charge(&current)?;
    let mut args = guard.contexts.clone();
    args.push(entry.clone());
    args.push(current);
    let result = unsafe { crate::exports::invoke(context, guard.flow, &args, site.id) }?;
    let Data::Bool(eligible) = &result.data else {
        return Err(host(format!(
            "invalid removal predicate result during {phase}"
        )));
    };
    Ok(*eligible)
}
fn progress(total: usize, completed: usize, bytes: &BigInt, index: i64, stage: &str) -> Handle {
    record([
        ("total_count".into(), int(total)),
        ("completed_count".into(), int(completed)),
        (
            "completed_bytes".into(),
            new(atom("Int"), Data::Int(bytes.clone())),
        ),
        (
            "failed_index".into(),
            new(atom("Int"), Data::Int(BigInt::from(index))),
        ),
        ("stage".into(), text(stage)),
    ])
}
fn removal_failure(
    site: &Site,
    entries: &Handle,
    total: usize,
    completed: usize,
    bytes: &BigInt,
    index: i64,
    stage: &str,
    kind: &str,
    message: &str,
) -> Fault {
    let mut activation = site.clone();
    activation.operation = "File.remove".into();
    file_fault(
        &activation,
        kind,
        &format!("{kind}: {message}"),
        record([
            ("entries".into(), crate::value::snapshot(entries)),
            (
                "progress".into(),
                progress(total, completed, bytes, index, stage),
            ),
        ]),
    )
}
enum RevalidationFailure {
    File(&'static str, String),
    Host(Fault),
}
fn revalidate(
    context: &Context,
    directory: &File,
    entry: &Handle,
    phase: u32,
) -> Result<FileInfo, RevalidationFailure> {
    let Data::Adapter(crate::adapters::AdapterValue::File(Value::Entry {
        fields,
        observation: Some(observation),
    })) = &entry.data
    else {
        return Err(RevalidationFailure::File(
            "File.InvalidSelection",
            "removal requires observed file entries".into(),
        ));
    };
    let name = fields.fields()["name"].string();
    let info = observed_at(context, directory, &observation.directory.path, name, phase).map_err(
        |failure| {
            let error = match failure {
                super::observer::Failure::IO(error) => error,
                super::observer::Failure::Host(fault) => return RevalidationFailure::Host(fault),
            };
            RevalidationFailure::File(
                if error.kind() == io::ErrorKind::NotFound {
                    "File.EntryChanged"
                } else {
                    "File.RemoveFailed"
                },
                format!(
                    "revalidate {}: lstat {name}: {}",
                    crate::checkpoint_print::quote(name),
                    system_error(&error)
                ),
            )
        },
    )?;
    let modified = (info.mtime() as i128) * 1_000_000_000 + info.mtime_nsec() as i128;
    if !info.file_type().is_file()
        || info.identity != observation.identity
        || fields.fields()["size_bytes"].integer() != &BigInt::from(info.len())
        || fields.fields()["modified_unix_ns"].integer() != &BigInt::from(modified)
    {
        return Err(RevalidationFailure::File(
            "File.EntryChanged",
            format!(
                "selected file {} changed since discovery",
                crate::checkpoint_print::quote(name)
            ),
        ));
    }
    Ok(info)
}
extern "C" {
    fn unlinkat(directory: i32, path: *const std::os::raw::c_char, flags: i32) -> i32;
}
pub(super) fn remove(context: &mut Context, site: &Site, target: &Value) -> Outcome {
    let result = remove_inner(context, site, target);
    result.map_err(|fault| {
        if matches!(&fault, Fault::Recoverable(_))
            || matches!(&fault,Fault::Boundary{details,..} if details.removal_progress.is_some())
        {
            fault
        } else if let Some((entries, progress)) = context.files.last_removal() {
            fault.with_details(BoundaryDetails {
                removal_entries: Some(entries.json()),
                removal_progress: Some(progress.json()),
                ..Default::default()
            })
        } else {
            fault
        }
    })
}
fn record_removal(
    context: &mut Context,
    entries_snapshot: &Handle,
    total: usize,
    completed: usize,
    bytes: &BigInt,
    index: i64,
    stage: &str,
) {
    context.files.last_removal = Some((
        entries_snapshot.clone(),
        progress(total, completed, bytes, index, stage),
    ));
}
fn remove_inner(context: &mut Context, site: &Site, target: &Value) -> Outcome {
    let Value::Remove {
        entries,
        session,
        guard,
        ..
    } = target
    else {
        return Err(host("invalid removal target"));
    };
    let total = entries.items().len();
    let mut completed = 0;
    let mut bytes = BigInt::from(0);
    let fail = |completed, bytes: &BigInt, index, stage, kind, message: &str| {
        removal_failure(
            site, entries, total, completed, bytes, index, stage, kind, message,
        )
    };
    if *session != context.files.session {
        return Err(fail(
            0,
            &bytes,
            -1,
            "preflight",
            "File.InvalidSelection",
            "removal effect belongs to another execution session",
        ));
    }
    if total > 0 {
        let diagnostic_entries = crate::value::snapshot(entries);
        record_removal(
            context,
            &diagnostic_entries,
            total,
            0,
            &bytes,
            -1,
            "preflight",
        );
        let Data::Adapter(crate::adapters::AdapterValue::File(Value::Entry {
            observation: Some(first),
            ..
        })) = &entries.items()[0].data
        else {
            return Err(host("invalid observed file selection"));
        };
        let root = new(
            atom("File.Path"),
            Data::Text(first.directory.path.to_string_lossy().into_owned()),
        );
        let (granted, current) = exact_grant(
            &context.files.removes,
            &context.files.base,
            "File.remove",
            root.string(),
            site,
            &root,
        )?;
        close_file(granted).map_err(|error| host(error.to_string()))?;
        if current != first.directory {
            return Err(fail(
                0,
                &bytes,
                -1,
                "preflight",
                "File.EntryChanged",
                "observed directory identity changed",
            ));
        }
        let retained = context
            .files
            .observed
            .get(&first.directory)
            .filter(|_| context.files.observed_leases.get(&first.directory) == Some(&first.lease))
            .ok_or_else(|| {
                fail(
                    0,
                    &bytes,
                    -1,
                    "preflight",
                    "File.InvalidSelection",
                    "observed directory is not retained by this session",
                )
            })?
            .try_clone()
            .map_err(|error| host(error.to_string()))?;
        let mutation = (|| {
            let diagnostic_weight = entries.weight + 2048;
            context.reserve_file((3 * total) as u64, diagnostic_weight)?;
            for (index, entry) in entries.items().iter().enumerate() {
                record_removal(
                    context,
                    &diagnostic_entries,
                    total,
                    completed,
                    &bytes,
                    index as i64,
                    "preflight",
                );
                context.check_cancel()?;
                let info = match revalidate(context, &retained, entry, 3) {
                    Ok(info) => info,
                    Err(RevalidationFailure::File(kind, message)) => {
                        return Err(fail(0, &bytes, index as i64, "preflight", kind, &message))
                    }
                    Err(RevalidationFailure::Host(fault)) => return Err(fault),
                };
                if let Some(guard) = guard {
                    match evaluate_guard(context, site, &info, entry, guard, "preflight") {
                        Ok(true) => (),
                        Ok(false) => {
                            return Err(guard_rejection(
                                site,
                                entries,
                                total,
                                0,
                                &bytes,
                                index,
                                "preflight",
                                guard,
                            ))
                        }
                        Err(fault) if matches!(&fault, Fault::Recoverable(_)) => {
                            return Err(fail(
                                0,
                                &bytes,
                                index as i64,
                                "preflight",
                                "File.RemoveFailed",
                                &fault.message(),
                            ))
                        }
                        Err(fault) => return Err(fault),
                    }
                }
            }
            for (index, entry) in entries.items().iter().enumerate() {
                record_removal(
                    context,
                    &diagnostic_entries,
                    total,
                    completed,
                    &bytes,
                    index as i64,
                    "remove",
                );
                context.check_cancel()?;
                let (granted, current) = exact_grant(
                    &context.files.removes,
                    &context.files.base,
                    "File.remove",
                    root.string(),
                    site,
                    &root,
                )?;
                close_file(granted).map_err(|error| host(error.to_string()))?;
                if current != first.directory {
                    return Err(fail(
                        completed,
                        &bytes,
                        index as i64,
                        "remove",
                        "File.EntryChanged",
                        "observed directory identity changed",
                    ));
                }
                let info = match revalidate(context, &retained, entry, 4) {
                    Ok(info) => info,
                    Err(RevalidationFailure::File(kind, message)) => {
                        return Err(fail(
                            completed,
                            &bytes,
                            index as i64,
                            "remove",
                            kind,
                            &message,
                        ))
                    }
                    Err(RevalidationFailure::Host(fault)) => return Err(fault),
                };
                if let Some(guard) = guard {
                    match evaluate_guard(context, site, &info, entry, guard, "remove") {
                        Ok(true) => (),
                        Ok(false) => {
                            return Err(guard_rejection(
                                site, entries, total, completed, &bytes, index, "remove", guard,
                            ))
                        }
                        Err(fault) if matches!(&fault, Fault::Recoverable(_)) => {
                            return Err(fail(
                                completed,
                                &bytes,
                                index as i64,
                                "remove",
                                "File.RemoveFailed",
                                &fault.message(),
                            ))
                        }
                        Err(fault) => return Err(fault),
                    }
                }
                let name = entry.fields()["name"].string();
                let name_c = CString::new(name).unwrap();
                context.session.processes.protect_removal(&retained, name)?;
                if let Some(probe) = &context.files.probe {
                    probe.protect_removal(&retained, name)?;
                }
                if unsafe { unlinkat(retained.as_raw_fd(), name_c.as_ptr(), 0) } < 0 {
                    let error = io::Error::last_os_error();
                    return Err(fail(
                        completed,
                        &bytes,
                        index as i64,
                        "remove",
                        if error.kind() == io::ErrorKind::NotFound {
                            "File.EntryChanged"
                        } else {
                            "File.RemoveFailed"
                        },
                        &format!(
                            "remove {}: {}",
                            crate::checkpoint_print::quote(name),
                            system_error(&error)
                        ),
                    ));
                }
                completed += 1;
                bytes += entry.fields()["size_bytes"].integer();
                record_removal(
                    context,
                    &diagnostic_entries,
                    total,
                    completed,
                    &bytes,
                    -1,
                    "finalize",
                );
                context.check_cancel().map_err(|fault| {
                    fault.with_details(BoundaryDetails {
                        removal_entries: Some(crate::value::snapshot(entries).json()),
                        removal_progress: Some(
                            progress(total, completed, &bytes, -1, "remove").json(),
                        ),
                        ..Default::default()
                    })
                })?;
            }
            Ok::<_, Fault>(())
        })();
        match (mutation, close_file(retained)) {
            (Err(primary), Err(cleanup)) => return Err(cleanup_failure(primary, cleanup)),
            (Err(primary), Ok(())) => return Err(primary),
            (Ok(()), Err(cleanup)) => return Err(host(system_error(&cleanup))),
            (Ok(()), Ok(())) => (),
        }
    }
    let receipt = new(
        atom("File.RemoveReceipt"),
        Data::Wrapped(record([
            ("total_count".into(), int(total)),
            ("completed_count".into(), int(completed)),
            ("observed_bytes".into(), new(atom("Int"), Data::Int(bytes))),
        ])),
    );
    context.last_activation = Some(
        json!({"operation":"File.remove","span":site.source_record(),"receipt":receipt.json()}),
    );
    context.charge(&receipt)?;
    Ok(receipt)
}
pub(super) fn exact_probe(
    context: &Context,
    site: &Site,
    root: &Handle,
) -> Result<(File, Directory), Fault> {
    exact_grant(
        &context.files.open_files,
        &context.files.base,
        "File.open_paths",
        root.string(),
        site,
        root,
    )
}
