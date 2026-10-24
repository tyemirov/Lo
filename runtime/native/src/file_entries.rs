use super::*;
use num_bigint::BigInt;
use serde_json::{json, Value as Json};

#[derive(Clone, PartialEq, Eq, PartialOrd, Ord)]
pub struct Directory {
    pub path: PathBuf,
    pub identity: (u64, u64),
}
#[derive(Clone)]
pub struct Observation {
    pub session: u64,
    pub directory: Directory,
    pub identity: (u64, u64),
}
#[derive(Clone)]
pub enum Value {
    Entry {
        fields: Handle,
        observation: Option<Observation>,
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
            Self::Entry { fields, .. } => Some(fields.fields()),
            _ => None,
        }
    }
    pub fn json(&self) -> Json {
        match self {
            Self::Entry { fields, .. } => Json::Object(
                fields
                    .fields()
                    .iter()
                    .map(|(name, value)| (name.clone(), value.json()))
                    .collect(),
            ),
            Self::Content {
                append,
                path,
                content,
            } => {
                json!({"operation":if *append {"File.Append.new"}else{"File.Write.new"},"file_path":path.json(),"file_content":content.json()})
            }
            Self::Remove { entries, .. } => {
                json!({"operation":"File.Remove.new","entries":entries.json()})
            }
        }
    }
    pub fn canonical_json(&self) -> String {
        match self {
            Self::Entry { fields, .. } => format!(
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
            ),
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
            Self::Remove { entries, .. } => format!(
                "{{\"operation\":\"File.Remove.new\",\"entries\":{}}}",
                entries.canonical_json()
            ),
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
            Self::Remove { entries, .. } => Self::Remove {
                entries: crate::value::snapshot(entries),
                session: 0,
                payload_weight: self.weight() + 16,
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
pub(super) struct FileInfo {
    pub identity: (u64, u64),
    size: u64,
    modified: i64,
    nanoseconds: i64,
    links: u64,
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
        kind,
    })
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
        &format!("{kind}: File.list {}: {message}", quote(path)),
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
        if operation == "File.list"
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
        if operation == "File.list"
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
pub(super) fn list(context: &mut Context, site: &Site, root: &Handle) -> Outcome {
    context.check_cancel()?;
    let (directory, observation) = exact_grant(
        &context.files.lists,
        &context.files.base,
        "File.list",
        root.string(),
        site,
        root,
    )?;
    let fresh = !context.files.observed.contains_key(&observation);
    if fresh && context.files.observed.len() == MAX_FILE_DIRECTORIES {
        return Err(limit("retained file directories"));
    }
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
        let mut items = Vec::new();
        let mut weight = 32;
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
            if items.len() == MAX_FILE_ENTRIES {
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
            let info = lstat_at(&directory, name).map_err(|error| {
                list_failure(
                    site,
                    root,
                    "File.ListFailed",
                    &observation.path.join(name),
                    &error.to_string(),
                )
            })?;
            let modified = (info.mtime() as i128) * 1_000_000_000 + info.mtime_nsec() as i128;
            if modified < i64::MIN as i128 || modified > i64::MAX as i128 {
                return Err(list_failure(
                    site,
                    root,
                    "File.ListFailed",
                    &observation.path.join(name),
                    "modification time exceeds Unix nanosecond range",
                ));
            }
            let kind = if info.file_type().is_file() {
                "regular"
            } else if info.is_dir() {
                "directory"
            } else if info.file_type().is_symlink() {
                "symlink"
            } else {
                "other"
            };
            let path_value = new(
                atom("File.Path"),
                Data::Text(observation.path.join(name).to_string_lossy().into_owned()),
            );
            let name_value = text(name);
            let size_value = int(info.len());
            let modified_value = new(atom("Int"), Data::Int(BigInt::from(modified)));
            let links_value = int(info.nlink());
            let entry_weight = 16
                + 128
                + observation.path.as_os_str().len() as u64
                + 16
                + ("path".len()
                    + "name".len()
                    + "kind".len()
                    + "size_bytes".len()
                    + "modified_unix_ns".len()
                    + "link_count".len()) as u64
                + path_value.weight
                + name_value.weight
                + 16
                + kind.len() as u64
                + size_value.weight
                + modified_value.weight
                + links_value.weight;
            context.reserve_file(0, weight + entry_weight)?;
            let fields = record([
                ("path".into(), path_value),
                ("name".into(), name_value),
                ("kind".into(), text(kind)),
                ("size_bytes".into(), size_value),
                ("modified_unix_ns".into(), modified_value),
                ("link_count".into(), links_value),
            ]);
            let entry = adapter(
                "File.Entry",
                Value::Entry {
                    fields,
                    observation: Some(Observation {
                        session: context.files.session,
                        directory: observation.clone(),
                        identity: info.identity,
                    }),
                },
            );
            weight += entry.weight;
            items.push(entry);
        }
        let count = items.len() as u64;
        let sort_steps = if count > 1 {
            count * (64 - (count - 1).leading_zeros() as u64)
        } else {
            0
        };
        context.reserve_file(sort_steps, weight)?;
        items.sort_by(|left, right| {
            left.fields()["name"]
                .string()
                .cmp(right.fields()["name"].string())
        });
        Ok(new(
            Type::List(Box::new(atom("File.Entry"))),
            Data::List(items),
        ))
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
    let mut result = match (result, close_file(directory)) {
        (Err(primary), Err(cleanup)) => Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => Err(primary),
        (Ok(_), Err(cleanup)) => Err(host(system_error(&cleanup))),
        (Ok(value), Ok(())) => Ok(value),
    };
    if result.is_ok() && fresh {
        context.files.observed.insert(observation, retained);
    } else if let Err(cleanup) = close_file(retained) {
        result = Err(match result {
            Err(primary) => cleanup_failure(primary, cleanup),
            Ok(_) => host(system_error(&cleanup)),
        });
    }
    result
}

pub(super) fn selection(context: &mut Context, site: &Site, entries: &Handle) -> Outcome {
    let items = entries.items();
    if items.len() > MAX_FILE_ENTRIES {
        return Err(limit("file removal entries"));
    }
    context.reserve_file(
        items.len() as u64,
        entries.weight + 32 + "File.Remove.new".len() as u64,
    )?;
    let mut names = BTreeSet::new();
    let mut directory = None;
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
    }
    Ok(adapter(
        "File.Remove",
        Value::Remove {
            entries: entries.clone(),
            session: context.files.session,
            payload_weight: "File.Remove.new".len() as u64 + entries.weight,
        },
    ))
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
fn revalidate(directory: &File, entry: &Handle) -> Result<(), (&'static str, String)> {
    let Data::Adapter(crate::adapters::AdapterValue::File(Value::Entry {
        fields,
        observation: Some(observation),
    })) = &entry.data
    else {
        return Err((
            "File.InvalidSelection",
            "removal requires observed file entries".into(),
        ));
    };
    let name = fields.fields()["name"].string();
    let info = lstat_at(directory, name).map_err(|error| {
        (
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
    })?;
    let modified = (info.mtime() as i128) * 1_000_000_000 + info.mtime_nsec() as i128;
    if !info.file_type().is_file()
        || info.identity != observation.identity
        || fields.fields()["size_bytes"].integer() != &BigInt::from(info.len())
        || fields.fields()["modified_unix_ns"].integer() != &BigInt::from(modified)
    {
        return Err((
            "File.EntryChanged",
            format!(
                "selected file {} changed since discovery",
                crate::checkpoint_print::quote(name)
            ),
        ));
    }
    Ok(())
}
extern "C" {
    fn unlinkat(directory: i32, path: *const std::os::raw::c_char, flags: i32) -> i32;
}
pub(super) fn remove(context: &mut Context, site: &Site, target: &Value) -> Outcome {
    let result = remove_inner(context, site, target);
    result.map_err(|fault| {
        if matches!(fault.primary(), Fault::Recoverable(_))
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
    entries: &Handle,
    total: usize,
    completed: usize,
    bytes: &BigInt,
    index: i64,
    stage: &str,
) {
    context.files.last_removal = Some((
        crate::value::snapshot(entries),
        progress(total, completed, bytes, index, stage),
    ));
}
fn remove_inner(context: &mut Context, site: &Site, target: &Value) -> Outcome {
    let Value::Remove {
        entries, session, ..
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
        record_removal(context, entries, total, 0, &bytes, -1, "preflight");
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
            context.add_file_weight(diagnostic_weight);
            for (index, entry) in entries.items().iter().enumerate() {
                record_removal(
                    context,
                    entries,
                    total,
                    completed,
                    &bytes,
                    index as i64,
                    "preflight",
                );
                context.check_cancel()?;
                if let Err((kind, message)) = revalidate(&retained, entry) {
                    return Err(fail(0, &bytes, index as i64, "preflight", kind, &message));
                }
            }
            for (index, entry) in entries.items().iter().enumerate() {
                record_removal(
                    context,
                    entries,
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
                if let Err((kind, message)) = revalidate(&retained, entry) {
                    return Err(fail(
                        completed,
                        &bytes,
                        index as i64,
                        "remove",
                        kind,
                        &message,
                    ));
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
                record_removal(context, entries, total, completed, &bytes, -1, "finalize");
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
