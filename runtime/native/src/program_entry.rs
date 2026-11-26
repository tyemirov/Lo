use crate::locks::{close_file, identity, Grant, Identity};
use crate::{context::*, value::*};
use std::{
    fs::File,
    os::fd::{AsRawFd, FromRawFd},
    path::{Path, PathBuf},
    sync::Mutex,
};
pub const OPERATION: &str = "Program.path";
pub struct Entry {
    pub path: Result<String, String>,
    pinned: Mutex<Option<Grant>>,
}
impl Entry {
    pub fn unavailable() -> Self {
        Self {
            path: Err("program entry identity is unavailable".into()),
            pinned: Mutex::new(None),
        }
    }
    pub fn inject(path: &str) -> Result<Self, String> {
        if !Path::new(path).is_absolute() {
            return Err("entry identity requires an absolute path".into());
        }
        use std::os::unix::fs::OpenOptionsExt;
        if path.len() > 4096
            || Path::new(path)
                .components()
                .filter(|part| matches!(part, std::path::Component::Normal(_)))
                .count()
                > 256
        {
            return Err("entry path limit".into());
        }
        let file = std::fs::OpenOptions::new()
            .read(true)
            .custom_flags(libc::O_CLOEXEC | libc::O_NONBLOCK)
            .open(path)
            .map_err(|e| e.to_string())?;
        let captured = (|| {
            let metadata = file.metadata().map_err(|e| e.to_string())?;
            if !metadata.is_file() {
                return Err("entry identity requires a regular file".into());
            }
            let id = identity(&file)?;
            let resolved = std::fs::canonicalize(path).map_err(|e| e.to_string())?;
            Self::bind(&resolved, Some(id))
        })();
        match (captured, close_file(file)) {
            (Ok(entry), Ok(())) => Ok(entry),
            (Err(error), Ok(())) => Err(error),
            (Ok(entry), Err(cleanup)) => {
                let retained = entry
                    .close()
                    .err()
                    .map_or(String::new(), |error| format!("; cleanup: {error}"));
                Err(format!("close entry witness: {cleanup}{retained}"))
            }
            (Err(error), Err(cleanup)) => Err(format!("{error}; cleanup: {cleanup}")),
        }
    }
    fn bind(path: &Path, expected: Option<Identity>) -> Result<Self, String> {
        let mut grant = Grant::bind(path)?;
        if expected
            .is_some_and(|expected| identity(grant.file.as_ref().unwrap()).ok() != Some(expected))
        {
            return Err(match grant.close() {
                Ok(()) => "running executable identity differs from pathname".into(),
                Err(cleanup) => {
                    format!("running executable identity differs from pathname; cleanup: {cleanup}")
                }
            });
        }
        let path = grant
            .path
            .to_str()
            .ok_or("program entry path requires UTF-8")?
            .to_owned();
        Ok(Self {
            path: Ok(path),
            pinned: Mutex::new(Some(grant)),
        })
    }
    pub fn native() -> Result<Self, String> {
        #[cfg(target_os = "linux")]
        let (path, witness) = {
            use std::os::unix::fs::OpenOptionsExt;
            let file = std::fs::OpenOptions::new()
                .read(true)
                .custom_flags(libc::O_CLOEXEC)
                .open("/proc/self/exe")
                .map_err(|e| e.to_string())?;
            let id = identity(&file)?;
            let path = std::fs::read_link("/proc/self/exe").map_err(|e| e.to_string())?;
            close_file(file)?;
            (path, id)
        };
        #[cfg(target_os = "macos")]
        let (path, witness) = darwin_image()?;
        Self::bind(&path, Some(witness))
    }
    pub fn close(&self) -> Result<(), String> {
        self.pinned
            .lock()
            .unwrap()
            .take()
            .map_or(Ok(()), |mut grant| grant.close())
    }
    pub(crate) fn duplicate_witnesses(
        &self,
        mut check: impl FnMut() -> Result<(), Fault>,
        mut retain: impl FnMut(&Path, Identity, File),
    ) -> Result<(), Fault> {
        let pinned = self.pinned.lock().map_err(|_| host("program entry ownership is poisoned"))?;
        let grant = pinned.as_ref().ok_or_else(|| host("program entry identity is unavailable"))?;
        grant.visit_retained_witnesses(|path, identity, original| {
            check()?;
            let duplicate = unsafe { libc::fcntl(original.as_raw_fd(), libc::F_DUPFD_CLOEXEC, 16) };
            if duplicate < 0 {
                return Err(host(format!("duplicate retained program entry witness: {}", std::io::Error::last_os_error())));
            }
            // The caller owns each duplicate immediately, including partial
            // snapshots. It must close them explicitly on every failure path.
            retain(path, identity, unsafe { File::from_raw_fd(duplicate) });
            Ok(())
        })
    }
    pub fn expected_identity(&self) -> Result<Identity, String> {
        let pinned = self.pinned.lock().unwrap();
        let grant = pinned.as_ref().ok_or("entry identity is unavailable")?;
        identity(grant.file.as_ref().ok_or("entry identity is closed")?)
    }
    pub fn value(&self) -> Result<Handle, String> {
        self.path
            .as_ref()
            .map(|path| new(atom("File.Path"), Data::Text(path.clone())))
            .map_err(Clone::clone)
    }
}
impl Drop for Entry {
    fn drop(&mut self) {
        let _ = self.close();
    }
}

#[cfg(target_os = "macos")]
fn darwin_image() -> Result<(PathBuf, Identity), String> {
    const MH_EXECUTE: u32 = 2;
    extern "C" {
        static _mh_execute_header: libc::mach_header;
        fn _NSGetExecutablePath(buffer: *mut libc::c_char, size: *mut u32) -> libc::c_int;
    }
    #[repr(C)]
    struct Region {
        prefix: [u8; 80],
        address: u64,
        size: u64,
    }
    #[repr(C)]
    struct RegionPath {
        region: Region,
        vnode: libc::vnode_info_path,
    }
    unsafe {
        // The linker identifies the main image independently of dyld's mutable list.
        if _mh_execute_header.filetype != MH_EXECUTE {
            return Err("main executable header is invalid".into());
        }
        let address = std::ptr::addr_of!(_mh_execute_header) as u64;
        let mut info: RegionPath = std::mem::zeroed();
        let length = std::mem::size_of::<RegionPath>();
        let received = libc::proc_pidinfo(
            libc::getpid(),
            8,
            address,
            (&mut info as *mut RegionPath).cast(),
            length as i32,
        );
        if received != length as i32
            || info.region.address > address
            || info.region.size == 0
            || address - info.region.address >= info.region.size
            || info.vnode.vip_vi.vi_type != 1
        {
            return Err("running executable vnode is unavailable".into());
        }
        let bytes = std::slice::from_raw_parts(info.vnode.vip_path.as_ptr().cast::<u8>(), 1024);
        let end = bytes
            .iter()
            .position(|byte| *byte == 0)
            .ok_or("running executable path is unterminated")?;
        let path = std::str::from_utf8(&bytes[..end])
            .map_err(|_| "running executable path requires UTF-8")?;
        if path.is_empty() || info.vnode.vip_vi.vi_stat.vst_ino == 0 {
            return Err("running executable vnode is empty".into());
        }
        let witness = Identity(
            (info.vnode.vip_vi.vi_stat.vst_dev as libc::dev_t) as u64,
            info.vnode.vip_vi.vi_stat.vst_ino,
        );
        let mut invoked = [0u8; 4097];
        let mut size = invoked.len() as u32;
        if _NSGetExecutablePath(invoked.as_mut_ptr().cast(), &mut size) != 0 {
            return Err("invoked executable path exceeds the entry path limit".into());
        }
        let end = invoked
            .iter()
            .position(|byte| *byte == 0)
            .ok_or("invoked executable path is unterminated")?;
        let invoked = Path::new(
            std::str::from_utf8(&invoked[..end])
                .map_err(|_| "invoked executable path requires UTF-8")?,
        );
        if !invoked.is_absolute() {
            return Err("invoked executable path requires an absolute name".into());
        }
        use std::os::unix::fs::MetadataExt;
        let location = match std::fs::metadata(invoked) {
            // Hardlink lookups can change the vnode's cached name without moving
            // the executable. Keep the invocation name while its identity agrees.
            Ok(metadata) if Identity(metadata.dev(), metadata.ino()) == witness => {
                std::fs::canonicalize(invoked)
                    .map_err(|error| format!("resolve invoked executable path: {error}"))?
            }
            // A replaced or removed invocation name means the mapped image moved.
            // Entry::bind must validate its current kernel name against the witness.
            Ok(_) => PathBuf::from(path),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => PathBuf::from(path),
            Err(error) => return Err(format!("inspect invoked executable path: {error}")),
        };
        Ok((location, witness))
    }
}
pub fn admit(site: &Site) -> Result<(), String> {
    if !site.arguments.is_empty() || site.typ != Type::Fallible(Box::new(atom("File.Path"))) {
        return Err("Program.path metadata mismatch".into());
    }
    Ok(())
}
pub fn execute(context: &mut Context, site: &Site) -> Outcome {
    match &context.session.initial_program_path {
        Ok(path) => Ok(path.clone()),
        Err(_) => Err(Fault::Recoverable(error(
            site,
            "Program.EntryUnavailable",
            "program entry identity is unavailable",
            record([]),
        ))),
    }
}
