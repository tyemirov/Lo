use crate::{context::*, value::*};
use num_bigint::BigInt;
use std::{fs::File, io, os::fd::AsRawFd, sync::MutexGuard};

pub const CLOCK: &str = "Clock.now_unix_ns";
pub const USAGE: &str = "File.System.usage";
const CLOCK_FAILED: &str = "Clock.Failed";
const USAGE_FAILED: &str = "File.System.UsageFailed";

#[repr(C)]
pub struct ClockMetadata {
    seconds: i64,
    nanoseconds: i64,
}
#[repr(C)]
pub struct VolumeMetadata {
    fragment_bytes: u64,
    total_blocks: u64,
    free_blocks: u64,
    available_blocks: u64,
}
type ClockCallback = unsafe extern "C" fn(usize, *mut ClockMetadata) -> i32;
type VolumeCallback = unsafe extern "C" fn(usize, *const u8, u64, *mut VolumeMetadata) -> i32;
#[derive(Default)]
pub struct Observers {
    clock: Option<(ClockCallback, usize)>,
    volume: Option<(VolumeCallback, usize)>,
    started: bool,
}

impl Observers {
    pub fn seal(&mut self) {
        self.started = true;
    }
}

pub fn admit(site: &Site) -> Result<(), String> {
    if site.operation != CLOCK
        || !site.arguments.is_empty()
        || site.typ != Type::Fallible(Box::new(atom("Int")))
    {
        return Err("clock observation metadata type mismatch".into());
    }
    Ok(())
}
fn failure(site: &Site, kind: &str, message: &str, input: Handle) -> Fault {
    Fault::Recoverable(error(
        site,
        kind,
        &format!("{kind}: {}: {message}", site.operation),
        input,
    ))
}
fn callback_status(
    context: &Context,
    site: &Site,
    status: i32,
    kind: &str,
    input: Handle,
) -> Result<(), Fault> {
    context.check_cancel()?;
    if status < 0 {
        return Err(host(format!(
            "{}: trusted observation provider failed",
            site.operation
        )));
    }
    if status > 0 {
        return Err(failure(
            site,
            kind,
            &io::Error::from_raw_os_error(status).to_string(),
            input,
        ));
    }
    Ok(())
}
pub fn clock(context: &mut Context, site: &Site) -> Outcome {
    context.reserve_file(1, 64)?;
    let mut native = std::mem::MaybeUninit::<libc::timespec>::uninit();
    if unsafe { libc::clock_gettime(libc::CLOCK_REALTIME, native.as_mut_ptr()) } != 0 {
        return Err(failure(
            site,
            CLOCK_FAILED,
            &io::Error::last_os_error().to_string(),
            record([]),
        ));
    }
    let native = unsafe { native.assume_init() };
    let mut metadata = ClockMetadata {
        seconds: native.tv_sec as i64,
        nanoseconds: native.tv_nsec as i64,
    };
    let observer = {
        let observers = context
            .session
            .observations
            .lock()
            .map_err(|_| host("clock observer lock failed"))?;
        observers.clock
    };
    if let Some((callback, user)) = observer {
        let status = unsafe { callback(user, &mut metadata) };
        callback_status(context, site, status, CLOCK_FAILED, record([]))?;
    }
    if !(0..1_000_000_000).contains(&metadata.nanoseconds) {
        return Err(host("invalid trusted clock observation"));
    }
    let nanos = i128::from(metadata.seconds) * 1_000_000_000 + i128::from(metadata.nanoseconds);
    let nanos = i64::try_from(nanos).map_err(|_| {
        failure(
            site,
            CLOCK_FAILED,
            "timestamp exceeds the signed 64-bit range",
            record([]),
        )
    })?;
    context.check_cancel()?;
    Ok(int(nanos))
}
pub fn usage(context: &mut Context, site: &Site, root: &Handle, directory: &File) -> Outcome {
    let input = || record([("root".into(), root.clone())]);
    context.reserve_file(1, 512)?;
    let mut native = std::mem::MaybeUninit::<libc::statvfs>::uninit();
    if unsafe { libc::fstatvfs(directory.as_raw_fd(), native.as_mut_ptr()) } != 0 {
        return Err(failure(
            site,
            USAGE_FAILED,
            &io::Error::last_os_error().to_string(),
            input(),
        ));
    }
    let native = unsafe { native.assume_init() };
    let mut metadata = VolumeMetadata {
        fragment_bytes: native.f_frsize as u64,
        total_blocks: native.f_blocks as u64,
        free_blocks: native.f_bfree as u64,
        available_blocks: native.f_bavail as u64,
    };
    let observer = {
        let observers = context
            .session
            .observations
            .lock()
            .map_err(|_| host("volume observer lock failed"))?;
        observers.volume
    };
    if let Some((callback, user)) = observer {
        let path = root.string().as_bytes();
        let status = unsafe { callback(user, path.as_ptr(), path.len() as u64, &mut metadata) };
        callback_status(context, site, status, USAGE_FAILED, input())?;
    }
    if metadata.fragment_bytes == 0
        || metadata.free_blocks > metadata.total_blocks
        || metadata.available_blocks > metadata.free_blocks
    {
        return Err(if observer.is_some() {
            host("invalid trusted volume observation")
        } else {
            failure(
                site,
                USAGE_FAILED,
                "invalid native volume block counts",
                input(),
            )
        });
    }
    let bytes = |blocks| {
        new(
            atom("Int"),
            Data::Int(BigInt::from(blocks) * BigInt::from(metadata.fragment_bytes)),
        )
    };
    let fields = [
        ("total_bytes".into(), bytes(metadata.total_blocks)),
        (
            "used_bytes".into(),
            bytes(metadata.total_blocks - metadata.free_blocks),
        ),
        ("free_bytes".into(), bytes(metadata.available_blocks)),
    ];
    context.check_cancel()?;
    Ok(new(
        atom("File.System.Usage"),
        Data::Record(fields.into_iter().collect()),
    ))
}

fn install(context: &Context) -> Option<MutexGuard<'_, Observers>> {
    if context.owner != std::thread::current().id()
        || context.scopes != 0
        || context.depth != 0
        || context.remaining != context.session.steps
        || context.weight != 0
        || context.fault.is_some()
    {
        return None;
    }
    let observers = context.session.observations.lock().ok()?;
    if observers.started {
        return None;
    }
    Some(observers)
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_clock_observer(
    context: *mut Context,
    callback: Option<ClockCallback>,
    userdata: usize,
) -> i32 {
    let (Some(context), Some(callback)) = (context.as_ref(), callback) else {
        return 0;
    };
    let Some(mut observers) = install(context) else {
        return 0;
    };
    if observers.clock.is_some() {
        return 0;
    }
    observers.clock = Some((callback, userdata));
    1
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_volume_observer(
    context: *mut Context,
    callback: Option<VolumeCallback>,
    userdata: usize,
) -> i32 {
    let (Some(context), Some(callback)) = (context.as_ref(), callback) else {
        return 0;
    };
    let Some(mut observers) = install(context) else {
        return 0;
    };
    if observers.volume.is_some() {
        return 0;
    }
    observers.volume = Some((callback, userdata));
    1
}
