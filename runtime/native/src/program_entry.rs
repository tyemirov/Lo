use crate::locks::{close_file, identity, Grant, Identity};
use crate::{context::*, value::*};
use std::{
    fs::File,
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
        let address = libc::_dyld_get_image_header(0) as u64;
        if address == 0 {
            return Err("main executable header is unavailable".into());
        }
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
        Ok((
            PathBuf::from(path),
            Identity(
                (info.vnode.vip_vi.vi_stat.vst_dev as libc::dev_t) as u64,
                info.vnode.vip_vi.vi_stat.vst_ino,
            ),
        ))
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
