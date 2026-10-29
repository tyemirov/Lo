use super::*;

#[repr(C)]
pub struct Metadata {
    pub owner_uid: u64,
    pub accessed_sec: i64,
    pub accessed_nsec: i64,
    pub modified_sec: i64,
    pub modified_nsec: i64,
    pub changed_sec: i64,
    pub changed_nsec: i64,
    pub link_count: u64,
    pub size_bytes: u64,
}
type Callback =
    unsafe extern "C" fn(usize, u32, *const u8, u64, *const u8, u64, *mut Metadata) -> i32;
#[derive(Clone, Copy)]
pub(super) struct Observer {
    callback: Callback,
    userdata: usize,
}
pub(super) enum Failure {
    IO(io::Error),
    Host(Fault),
}
pub(super) fn apply(
    context: &Context,
    root: &Path,
    name: &str,
    phase: u32,
    metadata: &mut Metadata,
) -> Result<(), Failure> {
    let Some(observer) = context.files.observer else {
        return Ok(());
    };
    let root = root.as_os_str().as_bytes();
    let result = unsafe {
        (observer.callback)(
            observer.userdata,
            phase,
            root.as_ptr(),
            root.len() as u64,
            name.as_ptr(),
            name.len() as u64,
            metadata,
        )
    };
    if result > 0 {
        return Err(Failure::IO(io::Error::from_raw_os_error(result)));
    }
    if result < 0
        || metadata.owner_uid > u32::MAX as u64
        || metadata.size_bytes > i64::MAX as u64
        || [
            metadata.accessed_nsec,
            metadata.modified_nsec,
            metadata.changed_nsec,
        ]
        .iter()
        .any(|n| !(0..1_000_000_000).contains(n))
    {
        return Err(Failure::Host(host(
            "invalid trusted file metadata observation",
        )));
    }
    Ok(())
}

// Trusted embedding hook. Install before any context in this session evaluates.
// Callback code and userdata must outlive the context and all session requests.
#[no_mangle]
pub unsafe extern "C" fn lo_context_file_metadata_observer(
    context: *mut Context,
    callback: Option<Callback>,
    userdata: usize,
) -> i32 {
    let Some(context) = context.as_mut() else {
        return 0;
    };
    if context.owner != std::thread::current().id()
        || context.scopes != 0
        || context.depth != 0
        || context.remaining != context.session.steps
        || context.weight != 0
        || context.fault.is_some()
        || context.files.observer.is_some()
    {
        return 0;
    }
    let Some(callback) = callback else {
        return 0;
    };
    let Ok(mut files) = context.session.files.lock() else {
        return 0;
    };
    if files.observer.is_some() {
        return 0;
    }
    let observer = Observer { callback, userdata };
    files.observer = Some(observer);
    context.files.observer = Some(observer);
    1
}
