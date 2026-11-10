use super::*;

#[derive(Clone, PartialEq, Eq, PartialOrd, Ord)]
pub(super) struct Witness {
    pub name: OsString,
    pub identity: (u64, u64),
}

pub(super) enum Failure {
    Changed,
    Missing(usize),
    IO(io::Error),
    Close(io::Error),
}

fn metadata(file: &File) -> io::Result<(u64, u64)> {
    let info = file.metadata()?;
    if !info.is_dir() {
        return Err(io::Error::from_raw_os_error(libc::ENOTDIR));
    }
    Ok((info.dev(), info.ino()))
}

pub(super) fn open_at(parent: &File, name: &std::ffi::OsStr) -> io::Result<File> {
    let name =
        CString::new(name.as_bytes()).map_err(|_| io::Error::from_raw_os_error(libc::EINVAL))?;
    let descriptor = unsafe {
        libc::openat(
            parent.as_raw_fd(),
            name.as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if descriptor < 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(unsafe { File::from_raw_fd(descriptor) })
    }
}

fn root() -> io::Result<File> {
    let descriptor = unsafe {
        libc::open(
            c"/".as_ptr(),
            libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC,
        )
    };
    if descriptor < 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(unsafe { File::from_raw_fd(descriptor) })
    }
}

fn close_on_error(file: File, error: io::Error) -> io::Error {
    match close_file(file) {
        Ok(()) => error,
        Err(cleanup) => io::Error::other(format!("{error}; close directory: {cleanup}")),
    }
}

pub(super) fn capture(path: &Path) -> io::Result<(File, Vec<Witness>, bool)> {
    let mut current = root()?;
    let initial = match metadata(&current) {
        Ok(identity) => identity,
        Err(error) => return Err(close_on_error(current, error)),
    };
    let mut chain = vec![Witness {
        name: "/".into(),
        identity: initial,
    }];
    for component in path.components() {
        let Component::Normal(name) = component else {
            continue;
        };
        let next = match open_at(&current, name) {
            Ok(next) => next,
            Err(error) if error.kind() == io::ErrorKind::NotFound => {
                return Ok((current, chain, true))
            }
            Err(error) => return Err(close_on_error(current, error)),
        };
        if let Err(error) = close_file(current) {
            return Err(close_on_error(next, error));
        }
        current = next;
        let identity = match metadata(&current) {
            Ok(identity) => identity,
            Err(error) => return Err(close_on_error(current, error)),
        };
        chain.push(Witness {
            name: name.to_owned(),
            identity,
        });
    }
    Ok((current, chain, false))
}

fn failure(error: io::Error) -> Failure {
    match error.raw_os_error() {
        Some(libc::ENOENT | libc::ENOTDIR | libc::ELOOP) => Failure::Changed,
        _ => Failure::IO(error),
    }
}

pub(super) fn reopen(chain: &[Witness]) -> Result<File, Failure> {
    let mut current = root().map_err(failure)?;
    for (index, witness) in chain.iter().enumerate() {
        if index > 0 {
            let next = match open_at(&current, &witness.name) {
                Ok(next) => next,
                Err(error) => {
                    close_file(current).map_err(Failure::Close)?;
                    if error.kind() == io::ErrorKind::NotFound {
                        return Err(Failure::Missing(index));
                    }
                    return Err(failure(error));
                }
            };
            if let Err(error) = close_file(current) {
                return Err(Failure::Close(close_on_error(next, error)));
            }
            current = next;
        }
        let observed = metadata(&current);
        if !matches!(&observed, Ok(identity) if *identity == witness.identity) {
            close_file(current).map_err(Failure::Close)?;
            return Err(match observed {
                Ok(_) => Failure::Changed,
                Err(error) => failure(error),
            });
        }
    }
    Ok(current)
}

pub(super) fn suffix_absent(mut current: File, names: &[OsString]) -> Result<bool, Failure> {
    for name in names {
        let next = match open_at(&current, name) {
            Ok(next) => next,
            Err(error) => {
                close_file(current).map_err(Failure::Close)?;
                return if error.kind() == io::ErrorKind::NotFound {
                    Ok(true)
                } else {
                    Err(failure(error))
                };
            }
        };
        if let Err(error) = close_file(current) {
            return Err(Failure::Close(close_on_error(next, error)));
        }
        current = next;
    }
    close_file(current).map_err(Failure::Close)?;
    Ok(false)
}
