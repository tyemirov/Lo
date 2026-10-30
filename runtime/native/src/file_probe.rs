use super::*;
use std::os::unix::process::{CommandExt, ExitStatusExt};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};
pub(super) struct Probe {
    bound: Result<Bound, String>,
}
struct Bound {
    path: PathBuf,
    file: File,
    identity: (u64, u64),
    digest: [u8; 32],
    ancestors: Vec<(PathBuf, File, (u64, u64))>,
    namespace: BTreeSet<((u64, u64), String)>,
    identities: BTreeSet<(u64, u64)>,
}
impl Probe {
    pub fn bind() -> Self {
        Self {
            bound: (|| {
                let path = std::env::var_os("PATH")
                    .and_then(|path| {
                        std::env::split_paths(&path)
                            .map(|directory| directory.join("lsof"))
                            .find(|path| path.is_file())
                    })
                    .ok_or_else(|| {
                        "exec: \"lsof\": executable file not found in $PATH".to_owned()
                    })?;
                let path = clean(
                    &std::env::current_dir()
                        .map_err(|error| error.to_string())?
                        .join(path),
                );
                let file = File::open(&path).map_err(|error| error.to_string())?;
                let info = file.metadata().map_err(|error| error.to_string())?;
                let digest = crate::process::hashing::digest(&file)?;
                let mut ancestors = Vec::new();
                let mut namespace = BTreeSet::new();
                let mut identities = BTreeSet::new();
                identities.insert((info.dev(), info.ino()));
                let resolved = std::fs::canonicalize(&path).map_err(|error| error.to_string())?;
                for selected in [&path, &resolved] {
                    let mut current = selected.as_path();
                    loop {
                        let parent = current.parent().unwrap_or(current);
                        let file = File::open(parent).map_err(|error| error.to_string())?;
                        let info = file.metadata().map_err(|error| error.to_string())?;
                        let identity = (info.dev(), info.ino());
                        identities.insert(identity);
                        namespace.insert((
                            identity,
                            current.file_name().map_or_else(String::new, |name| {
                                name.to_string_lossy().into_owned()
                            }),
                        ));
                        if let Ok(info) = std::fs::symlink_metadata(current) {
                            identities.insert((info.dev(), info.ino()));
                        }
                        ancestors.push((parent.to_owned(), file, identity));
                        if current == parent {
                            break;
                        }
                        current = parent;
                    }
                }
                Ok(Bound {
                    path,
                    file,
                    identity: (info.dev(), info.ino()),
                    digest,
                    ancestors,
                    namespace,
                    identities,
                })
            })(),
        }
    }
    pub fn fork(&self) -> Result<Self, String> {
        Ok(Self {
            bound: match &self.bound {
                Ok(bound) => Ok(Bound {
                    path: bound.path.clone(),
                    file: bound.file.try_clone().map_err(|error| error.to_string())?,
                    identity: bound.identity,
                    digest: bound.digest,
                    ancestors: bound
                        .ancestors
                        .iter()
                        .map(|(path, file, identity)| {
                            Ok((
                                path.clone(),
                                file.try_clone().map_err(|error| error.to_string())?,
                                *identity,
                            ))
                        })
                        .collect::<Result<_, String>>()?,
                    namespace: bound.namespace.clone(),
                    identities: bound.identities.clone(),
                }),
                Err(error) => Err(error.clone()),
            },
        })
    }
    pub fn protect_name(&self, parent: &File, name: &str) -> Result<(), Fault> {
        if let Ok(bound) = &self.bound {
            let info = parent.metadata().map_err(|error| host(error.to_string()))?;
            if bound
                .namespace
                .contains(&((info.dev(), info.ino()), name.to_owned()))
            {
                return Err(host(
                    "Process registry File mutation: registered namespace entry is protected",
                ));
            }
        }
        Ok(())
    }
    pub fn protect_file(&self, file: &File) -> Result<(), Fault> {
        if let Ok(bound) = &self.bound {
            let info = file.metadata().map_err(|error| host(error.to_string()))?;
            if bound.identities.contains(&(info.dev(), info.ino())) {
                return Err(host("Process registry File mutation: registered executable or namespace is protected"));
            }
        }
        Ok(())
    }
    pub fn protect_removal(&self, parent: &File, name: &str) -> Result<(), Fault> {
        self.protect_name(parent, name)?;
        if let Ok(bound) = &self.bound {
            let info = entries::lstat_at(parent, name).map_err(|error| host(error.to_string()))?;
            if bound.identities.contains(&info.identity) {
                return Err(host("Process registry File mutation: registered executable or namespace is protected"));
            }
        }
        Ok(())
    }
    pub fn close(self) -> Result<(), String> {
        if let Ok(bound) = self.bound {
            let mut errors = Vec::new();
            if let Err(error) = close_file(bound.file) {
                errors.push(system_error(&error))
            }
            for (_, file, _) in bound.ancestors {
                if let Err(error) = close_file(file) {
                    errors.push(system_error(&error))
                }
            }
            if !errors.is_empty() {
                return Err(errors.join("\n"));
            }
        }
        Ok(())
    }
    fn run(&self, context: &Context, directory: &File) -> Result<Vec<u8>, Fault> {
        let bound = self
            .bound
            .as_ref()
            .map_err(|error| host(format!("locate open-file probe: {error}")))?;
        for (path, _, identity) in &bound.ancestors {
            let info = std::fs::metadata(path).map_err(|error| {
                host(format!("validate open-file probe: {error}"))
                    .with_details(BoundaryDetails::default())
            })?;
            if (info.dev(), info.ino()) != *identity {
                return Err(host(
                    "validate open-file probe: registered namespace identity differs",
                )
                .with_details(BoundaryDetails::default()));
            }
        }
        let current = File::open(&bound.path).map_err(|error| {
            host(format!("validate open-file probe: {error}"))
                .with_details(BoundaryDetails::default())
        })?;
        let validation = (|| {
            let info = current.metadata().map_err(|error| {
                host(format!("validate open-file probe: {error}"))
                    .with_details(BoundaryDetails::default())
            })?;
            let digest =
                crate::process::hashing::digest_with_cancel(&current, || context.check_cancel())
                    .map_err(|fault| {
                        if matches!(fault.primary(), Fault::Host("runtime.host-failure", _)) {
                            host(format!("validate open-file probe: {}", fault.message()))
                                .with_details(BoundaryDetails::default())
                        } else {
                            fault
                        }
                    })?;
            if (info.dev(), info.ino()) != bound.identity || digest != bound.digest {
                return Err(host("validate open-file probe: executable changed")
                    .with_details(BoundaryDetails::default()));
            }
            Ok::<_, Fault>(())
        })();
        match (validation, close_file(current)) {
            (Err(primary), Err(cleanup)) => return Err(cleanup_failure(primary, cleanup)),
            (Err(primary), Ok(())) => return Err(primary),
            (Ok(()), Err(error)) => {
                return Err(host(format!(
                    "validate open-file probe: {}",
                    system_error(&error)
                ))
                .with_details(BoundaryDetails::default()))
            }
            (Ok(()), Ok(())) => (),
        }
        let mut command = Command::new(&bound.path);
        command
            .args(["-Fn", "+D", "."])
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        let fd = directory.as_raw_fd();
        unsafe {
            command.pre_exec(move || {
                if fchdir(fd) < 0 {
                    return Err(io::Error::last_os_error());
                }
                if setpgid(0, 0) < 0 {
                    return Err(io::Error::last_os_error());
                }
                Ok(())
            });
        }
        let mut child = command
            .spawn()
            .map_err(|error| host(format!("probe open files: {error}")))?;
        let mut stdout = child.stdout.take().unwrap();
        let mut stderr = child.stderr.take().unwrap();
        let mut output = Vec::new();
        let mut errors = Vec::new();
        let mut status = None;
        let mut stdout_done = false;
        let mut stderr_done = false;
        let mut exited = None;
        let result = (|| {
            set_nonblocking_fd(stdout.as_raw_fd()).map_err(|error| host(error.to_string()))?;
            set_nonblocking_fd(stderr.as_raw_fd()).map_err(|error| host(error.to_string()))?;

            loop {
                context.check_cancel()?;
                read_stream(context, &mut stdout, &mut output, &mut stdout_done)?;
                read_stream(context, &mut stderr, &mut errors, &mut stderr_done)?;
                if status.is_none() {
                    status = child.try_wait().map_err(|error| host(error.to_string()))?;
                    if status.is_some() {
                        exited = Some(Instant::now());
                    }
                }
                if status.is_some() && stdout_done && stderr_done {
                    break;
                }
                if exited.is_some_and(|when| when.elapsed() > Duration::from_secs(1)) {
                    return Err(host(
                        "probe open files: exec: WaitDelay expired before I/O complete",
                    ));
                }
                std::thread::sleep(Duration::from_millis(10));
            }
            let status = status.unwrap();
            if !status.success() && status.code() != Some(1) {
                return Err(host(format!(
                    "probe open files: {}",
                    status.code().map_or_else(
                        || format!("signal: {}", status.signal().unwrap_or(0)),
                        |code| format!("exit status {code}")
                    )
                )));
            }
            let errors = std::str::from_utf8(&errors)
                .map_err(|_| host("open-file probe returned invalid UTF-8 on standard error"))?;
            if !errors.trim().is_empty() {
                return Err(host(format!(
                    "open-file probe reported standard error: {errors}"
                )));
            }
            Ok(output)
        })();
        unsafe {
            kill(-(child.id() as i32), 9);
        }
        let waited = if status.is_none() {
            child.wait().map(|_| ())
        } else {
            Ok(())
        };
        match (result, waited) {
            (Err(primary), _) => Err(primary),
            (Ok(_), Err(error)) => Err(host(error.to_string())),
            (Ok(output), Ok(())) => Ok(output),
        }
    }
}
fn set_nonblocking_fd(fd: i32) -> io::Result<()> {
    let flags = unsafe { fcntl(fd, 3) };
    if flags < 0 || unsafe { fcntl(fd, 4, flags | FLAGS[3]) } < 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}
fn read_stream(
    context: &Context,
    stream: &mut impl Read,
    output: &mut Vec<u8>,
    done: &mut bool,
) -> Result<(), Fault> {
    let mut buffer = [0; 65536];
    loop {
        context.check_cancel()?;
        match stream.read(&mut buffer) {
            Ok(0) => {
                *done = true;
                return Ok(());
            }
            Ok(count) => {
                context
                    .values
                    .preflight(output.len() as u64 + count as u64 + 16)?;
                output.extend_from_slice(&buffer[..count])
            }
            Err(error) if error.kind() == io::ErrorKind::WouldBlock => return Ok(()),
            Err(error) if error.kind() == io::ErrorKind::Interrupted => continue,
            Err(error) => return Err(host(error.to_string())),
        }
    }
}
extern "C" {
    fn fchdir(fd: i32) -> i32;
    fn setpgid(pid: i32, group: i32) -> i32;
    fn kill(pid: i32, signal: i32) -> i32;
}
fn parse(context: &mut Context, directory: &Path, output: &[u8]) -> Outcome {
    context.reserve_file(output.len() as u64, output.len() as u64)?;
    let output =
        std::str::from_utf8(output).map_err(|_| host("open-file probe returned invalid text"))?;
    if output.contains('\0') {
        return Err(host("open-file probe returned invalid text"));
    }
    let mut paths = BTreeSet::new();
    let mut weight = 32;
    for line in output.split('\n') {
        if line.is_empty() {
            continue;
        }
        context.reserve_file(1, weight)?;
        let kind = std::str::from_utf8(&line.as_bytes()[..1]).unwrap_or("");
        let value = line.get(1..).unwrap_or("");
        match kind {
            "p" => {
                if value.is_empty() {
                    return Err(host("open-file probe returned an empty process identifier"));
                }
                if !value.bytes().all(|byte| byte.is_ascii_digit()) {
                    return Err(host(
                        "open-file probe returned an invalid process identifier",
                    ));
                }
            }
            "f" => {
                if value.is_empty() {
                    return Err(host("open-file probe returned an empty descriptor"));
                }
                if !valid_descriptor(value) {
                    return Err(host("open-file probe returned an invalid descriptor"));
                }
            }
            "n" => {
                if value
                    .chars()
                    .any(|character| character == '\\' || character.is_control())
                {
                    return Err(host("open-file probe returned an ambiguous filename"));
                }
                if value == "." {
                    continue;
                }
                let relative = value
                    .strip_prefix("./")
                    .ok_or_else(|| host("open-file probe returned a path outside its directory"))?;
                let relative_path = Path::new(relative);
                if relative.is_empty()
                    || relative == "."
                    || relative_path.is_absolute()
                    || relative_path.components().any(|component| {
                        matches!(component, Component::ParentDir | Component::CurDir)
                    })
                    || clean(relative_path) != relative_path
                    || relative.ends_with("/.")
                    || relative.ends_with('/')
                    || relative.contains("//")
                    || relative.contains("/./")
                {
                    return Err(host("open-file probe returned an invalid relative path"));
                }
                let absolute = directory.join(relative);
                if paths.contains(&absolute) {
                    continue;
                }
                if context.values.limits.file_entries != 0
                    && paths.len() as u64 >= context.values.limits.file_entries
                {
                    return Err(limit("open-file probe paths"));
                }
                weight += 16 + absolute.as_os_str().len() as u64;
                context.reserve_file(0, weight)?;
                paths.insert(absolute);
            }
            _ => return Err(host("open-file probe returned an unknown record")),
        }
    }
    let count = paths.len() as u64;
    let steps = count
        + if count > 1 {
            count * (64 - (count - 1).leading_zeros() as u64)
        } else {
            0
        };
    context.reserve_file(steps, weight)?;
    Ok(new(
        Type::List(Box::new(atom("File.Path"))),
        Data::List(
            paths
                .into_iter()
                .map(|path| {
                    new(
                        atom("File.Path"),
                        Data::Text(path.to_string_lossy().into_owned()),
                    )
                })
                .collect(),
        ),
    ))
}

fn valid_descriptor(value: &str) -> bool {
    if let Some(digits) = value.strip_prefix('*') {
        digits.len() == 3 && digits.bytes().all(|byte| byte.is_ascii_digit())
    } else {
        !value.is_empty() && value.bytes().all(|byte| byte.is_ascii_alphanumeric())
    }
}
pub(super) fn open_paths(context: &mut Context, site: &Site, root: &Handle) -> Outcome {
    context.check_cancel()?;
    let (directory, observation) = entries::exact_probe(context, site, root)?;
    context.reserve_file(1, 32)?;
    let output = context
        .files
        .probe
        .as_ref()
        .ok_or_else(|| host("missing bound open-file probe"))?
        .run(context, &directory);
    let close = close_file(directory);
    let output = match output {
        Err(Fault::Host("runtime.host-failure", message)) => {
            return Err(Fault::Recoverable(error(
                site,
                "File.OpenFilesFailed",
                &format!(
                    "File.OpenFilesFailed: probe {}: {message}",
                    quote(&observation.path)
                ),
                record([("root".into(), root.clone())]),
            )))
        }
        Err(fault) => return Err(fault),
        Ok(output) => output,
    };
    close.map_err(|error| host(error.to_string()))?;
    match parse(context, &observation.path, &output) {
        Err(Fault::Host("runtime.host-failure", message)) => Err(Fault::Recoverable(error(
            site,
            "File.OpenFilesFailed",
            &format!(
                "File.OpenFilesFailed: probe {}: {message}",
                quote(&observation.path)
            ),
            record([("root".into(), root.clone())]),
        ))),
        result => result,
    }
}
