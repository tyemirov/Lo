use super::*;
use std::fs::OpenOptions;
use std::os::unix::fs::OpenOptionsExt;

struct Links {
    observed: u64,
    data_names: u64,
    declared: u64,
}

#[derive(Clone, Copy)]
pub(crate) enum MutationAccess {
    Data,
    Namespace,
}
struct Scan {
    entries: usize,
    maximum: usize,
    depth: usize,
    links: BTreeMap<(u64, u64), Links>,
}

fn denied(subject: &str) -> Fault {
    Fault::Host(
        "runtime.capability-denied",
        format!("Process.run {subject:?} has no host capability"),
    )
}

pub(crate) fn validate_namespace(
    context: &mut Context,
    roots: &[(String, MutationAccess)],
    maximum: usize,
    depth: usize,
) -> Result<(), Fault> {
    let mut scan = Scan {
        entries: 0,
        maximum,
        depth,
        links: BTreeMap::new(),
    };
    for (root, access) in roots {
        context.check_cancel()?;
        let directory = OpenOptions::new()
            .read(true)
            .custom_flags(libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(root)
            .map_err(|error| host(format!("open Process cleanup root: {error}")))?;
        let scanned = scan.directory(context, &directory, 0, *access);
        let closed = close_file(directory)
            .map_err(|error| host(format!("close Process cleanup root: {error}")));
        combine_cleanup(scanned.err(), closed.err()).map_or(Ok(()), Err)?;
    }
    if scan
        .links
        .values()
        .any(|entry| entry.data_names != 0 && entry.observed != entry.declared)
    {
        return Err(denied("cleanup root has an outside hardlink alias"));
    }
    if scan
        .links
        .values()
        .any(|entry| entry.data_names != 0 && entry.data_names != entry.declared)
    {
        return Err(denied("cleanup inode crosses data-write authority"));
    }
    Ok(())
}

impl Scan {
    fn directory(
        &mut self,
        context: &mut Context,
        directory: &File,
        depth: usize,
        access: MutationAccess,
    ) -> Result<(), Fault> {
        if depth > self.depth {
            return Err(Fault::Host(
                "runtime.limit",
                "Process cleanup directory depth".into(),
            ));
        }
        let mut stream = DirectoryStream::open(directory)
            .map_err(|error| host(format!("open Process cleanup directory: {error}")))?;
        let scanned = (|| loop {
            context.check_cancel()?;
            let Some(name) = stream
                .next()
                .map_err(|error| host(format!("list Process cleanup directory: {error}")))?
            else {
                return Ok(());
            };
            self.entries += 1;
            if self.entries > self.maximum {
                return Err(Fault::Host(
                    "runtime.limit",
                    "Process cleanup entries".into(),
                ));
            }
            let name = name
                .to_str()
                .ok_or_else(|| host("Process cleanup name is not valid UTF-8"))?;
            let information = lstat_at(directory, name)
                .map_err(|error| host(format!("observe Process cleanup entry: {error}")))?;
            if !information.is_dir() {
                let entry = self.links.entry(information.identity).or_insert(Links {
                    observed: 0,
                    declared: information.nlink(),
                    data_names: 0,
                });
                if entry.declared != information.nlink() {
                    return Err(denied("cleanup entry changed during admission"));
                }
                entry.observed += 1;
                if matches!(access, MutationAccess::Data) {
                    entry.data_names += 1;
                }
                continue;
            }
            let encoded =
                CString::new(name).map_err(|_| host("Process cleanup name contains NUL"))?;
            let descriptor = unsafe {
                libc::openat(
                    directory.as_raw_fd(),
                    encoded.as_ptr(),
                    libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
                )
            };
            if descriptor < 0 {
                return Err(host(format!(
                    "open Process cleanup child: {}",
                    io::Error::last_os_error()
                )));
            }
            let child = unsafe { File::from_raw_fd(descriptor) };
            let scanned = match child.metadata() {
                Ok(metadata) if identity(&metadata) == information.identity => {
                    self.directory(context, &child, depth + 1, access)
                }
                Ok(_) => Err(denied("cleanup directory changed during admission")),
                Err(error) => Err(host(format!("observe Process cleanup child: {error}"))),
            };
            let closed = close_file(child)
                .map_err(|error| host(format!("close Process cleanup child: {error}")));
            combine_cleanup(scanned.err(), closed.err()).map_or(Ok(()), Err)?;
        })();
        let closed = stream
            .close()
            .map_err(|error| host(format!("close Process cleanup directory: {error}")));
        combine_cleanup(scanned.err(), closed.err()).map_or(Ok(()), Err)
    }
}
