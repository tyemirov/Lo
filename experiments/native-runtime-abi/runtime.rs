//! P005 ABI feasibility fixture, not a partial production Lo runtime.
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::thread::{self, ThreadId};

static LIVE: AtomicU64 = AtomicU64::new(0);

pub struct Context {
    owner: ThreadId,
    budget: u64,
}

pub struct Value(i64);

impl Drop for Value {
    fn drop(&mut self) {
        LIVE.fetch_sub(1, Ordering::SeqCst);
    }
}

#[repr(C)]
pub struct ResultSlot {
    status: u32,
    code: u32,
    value: *const Value,
}

type Callback = extern "C" fn(*mut Context, *const Value, *mut ResultSlot);

#[no_mangle]
pub extern "C" fn lo_context_new(budget: u64) -> *mut Context {
    Box::into_raw(Box::new(Context { owner: thread::current().id(), budget }))
}

#[no_mangle]
pub unsafe extern "C" fn lo_context_drop(context: *mut Context) {
    drop(Box::from_raw(context));
}

#[no_mangle]
pub extern "C" fn lo_value_new(number: i64) -> *const Value {
    LIVE.fetch_add(1, Ordering::SeqCst);
    Arc::into_raw(Arc::new(Value(number)))
}

#[no_mangle]
pub unsafe extern "C" fn lo_value_read(value: *const Value) -> i64 {
    (*value).0
}

#[no_mangle]
pub unsafe extern "C" fn lo_value_retain(value: *const Value) {
    Arc::increment_strong_count(value);
}

#[no_mangle]
pub unsafe extern "C" fn lo_value_release(value: *const Value) {
    drop(Arc::from_raw(value));
}

#[no_mangle]
pub extern "C" fn lo_live_values() -> u64 {
    LIVE.load(Ordering::SeqCst)
}

#[no_mangle]
pub unsafe extern "C" fn lo_apply(
    context: *mut Context,
    input: *const Value,
    callback: Callback,
    output: *mut ResultSlot,
) {
    let context_value = &mut *context;
    *output = ResultSlot { status: 2, code: 10, value: std::ptr::null() };
    if context_value.owner != thread::current().id() {
        return;
    }
    if context_value.budget == 0 {
        (*output).code = 9;
        return;
    }
    context_value.budget -= 1;
    if (*input).0 < 0 {
        *output = ResultSlot { status: 1, code: 7, value: lo_value_new(-7) };
        return;
    }
    let scaled = lo_value_new((*input).0.checked_mul(3).expect("fixture input overflow"));
    callback(context, scaled, output);
    lo_value_release(scaled);
}
