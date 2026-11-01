use crate::context::{host, Fault};
use std::{
    ffi::c_void,
    sync::{Mutex, MutexGuard, TryLockError},
    time::Duration,
};

static CREATION_AND_SPAWN: Mutex<()> = Mutex::new(());

pub fn acquire(
    mut check: impl FnMut() -> Result<(), Fault>,
) -> Result<MutexGuard<'static, ()>, Fault> {
    loop {
        check()?;
        match CREATION_AND_SPAWN.try_lock() {
            Ok(guard) => return Ok(guard),
            Err(TryLockError::Poisoned(_)) => {
                return Err(host("native descriptor ownership is poisoned"))
            }
            Err(TryLockError::WouldBlock) => std::thread::sleep(Duration::from_millis(1)),
        }
    }
}

#[no_mangle]
pub unsafe extern "C" fn lo_native_spawn_acquire(
    cancelled: unsafe extern "C" fn(*mut c_void) -> i32,
    token: *mut c_void,
) -> *mut c_void {
    match acquire(|| {
        if cancelled(token) != 0 {
            Err(Fault::RawCancellation)
        } else {
            Ok(())
        }
    }) {
        Ok(guard) => Box::into_raw(Box::new(guard)).cast(),
        Err(_) => std::ptr::null_mut(),
    }
}

#[no_mangle]
pub unsafe extern "C" fn lo_native_spawn_release(ownership: *mut c_void) {
    drop(Box::from_raw(ownership.cast::<MutexGuard<'static, ()>>()));
}
