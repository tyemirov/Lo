use super::*;
use std::{ffi::c_void, sync::{atomic::AtomicU32, mpsc::SyncSender}};

pub const CHUNK_BYTES: usize = 4096;
#[repr(C)]
#[derive(Clone, Copy)]
pub struct Limits {
    pub timeout_ms: u64,
    pub cleanup_ms: u64,
    pub output_bytes: usize,
    pub forward_streams: u32,
}
impl Default for Limits {
    fn default() -> Self {
        Self { timeout_ms: 5000, cleanup_ms: 1000, output_bytes: 1 << 20, forward_streams: 0 }
    }
}
impl Limits {
    pub fn validate(self) -> Result<Self, String> {
        if self.timeout_ms == 0 || self.cleanup_ms == 0 || self.output_bytes == 0
            || self.output_bytes > isize::MAX as usize || self.forward_streams & !3 != 0
            || self.timeout_ms.checked_add(self.cleanup_ms).is_none_or(|total|
                total > i64::MAX as u64 || Instant::now().checked_add(Duration::from_millis(total)).is_none()) {
            return Err("Process limits require positive representable duration, cleanup, and output bounds".into());
        }
        Ok(self)
    }
}
#[repr(C)]
#[derive(Default, Clone, Copy)]
pub struct NativeResult {
    pub stage: i32,
    pub class_id: i32,
    pub started: i32,
    pub cleanup_complete: i32,
    pub termination_kind: i32,
    pub termination_value: i32,
    pub stdin_written: usize,
    pub stdout_size: usize,
    pub stderr_size: usize,
    pub cleanup_failures: u32,
    pub ownership: usize,
}
#[repr(C)]
pub struct Request {
    pub frame: *const u8,
    pub frame_size: usize,
    pub input: *const u8,
    pub input_size: usize,
    pub timeout_ms: u64,
    pub cleanup_ms: u64,
    pub output_bytes: usize,
    pub output_mode: u32,
    pub stop: extern "C" fn(*const AtomicU32) -> i32,
    pub stop_token: *const AtomicU32,
    pub output: unsafe extern "C" fn(*mut c_void, u32, *const u8, usize, *mut usize) -> i32,
    pub output_token: *mut c_void,
}
pub type DriverCallback = unsafe extern "C" fn(usize, *const Request, *mut NativeResult);
pub enum Event {
    Output { stream: u32, bytes: Vec<u8>, reply: SyncSender<(usize, i32)> },
    Complete(NativeResult),
    Released(u64),
    Unavailable(NativeResult),
}
pub struct Output {
    pub events: SyncSender<Event>,
    pub stop: Arc<AtomicU32>,
    pub state: Mutex<OutputState>,
    pub maximum: usize,
}
#[derive(Default)]
pub struct OutputState {
    pub counts: [usize; 2],
    pub failure: Option<(i32, i32)>,
}
impl Output {
    pub fn deliver(&self, stream: u32, bytes: &[u8]) -> (usize, i32) {
        let mut confirmed = 0;
        let class = unsafe { publish((self as *const Self).cast_mut().cast(), stream,
            bytes.as_ptr(), bytes.len(), &mut confirmed) };
        (confirmed, class)
    }
    pub fn complete(&self, result: &mut NativeResult, input_length: usize) {
        let observed = self.state.lock().unwrap();
        if let Some((stage, class)) = observed.failure { result.stage = stage; result.class_id = class; }
        if !(0..=7).contains(&result.stage) || !(0..=5).contains(&result.class_id)
            || !(0..=1).contains(&result.started) || !(0..=1).contains(&result.cleanup_complete)
            || !(0..=2).contains(&result.termination_kind) || result.cleanup_failures & !7 != 0
            || (result.cleanup_complete == 1 && result.ownership != 0)
            || (result.stage == 0 && result.cleanup_failures != 0)
            || (result.termination_kind == 1 && result.termination_value < 0)
            || (result.termination_kind == 2 && result.termination_value <= 0)
            || (result.stage == 0 && result.class_id != 0)
            || (result.termination_kind != 0 && result.started != 1)
            || (result.started == 1 && result.termination_kind == 0 && result.cleanup_complete == 1)
            || result.stdin_written > input_length
            || [result.stdout_size, result.stderr_size] != observed.counts {
            result.stage = 5; result.class_id = 2; result.cleanup_complete = 0;
            result.stdout_size = observed.counts[0]; result.stderr_size = observed.counts[1];
        }
        if result.stage == 0 && result.termination_kind == 0 {
            result.stage = 1; result.class_id = 2; result.cleanup_complete = 0;
        }
        if result.stage == 0 && result.cleanup_complete == 0 { result.stage = 6; result.class_id = 2; }
    }
    fn reject(&self, state: &mut OutputState, stream: u32, class: i32) -> i32 {
        let (_, first) = *state.failure.get_or_insert((if stream == 2 { 4 } else { 3 }, class));
        let _ = self.stop.compare_exchange(0, first as u32, Ordering::SeqCst, Ordering::SeqCst);
        first
    }
}
pub extern "C" fn stopped(token: *const AtomicU32) -> i32 {
    unsafe { (*token).load(Ordering::SeqCst) as i32 }
}
unsafe extern "C" fn publish(token: *mut c_void, stream: u32, bytes: *const u8, length: usize, confirmed: *mut usize) -> i32 {
    let collector = &*token.cast::<Output>();
    let mut state = collector.state.lock().unwrap();
    if confirmed.is_null() { return collector.reject(&mut state, stream, 2); }
    *confirmed = 0;
    if let Some((_, class)) = state.failure { return class; }
    if !(1..=2).contains(&stream) || bytes.is_null() || length == 0 || length > CHUNK_BYTES {
        return collector.reject(&mut state, stream, 2);
    }
    let index = (stream - 1) as usize;
    if length > collector.maximum - state.counts[index] { return collector.reject(&mut state, stream, 3); }
    let reason = collector.stop.load(Ordering::SeqCst);
    if reason != 0 { return reason as i32; }
    let (reply, response) = std::sync::mpsc::sync_channel(1);
    let bytes = std::slice::from_raw_parts(bytes, length).to_vec();
    if collector.events.send(Event::Output { stream, bytes, reply }).is_err() { return collector.reject(&mut state, stream, 2); }
    let (count, class) = response.recv().unwrap_or((0, 2));
    if count > length || !(0..=5).contains(&class) || (class == 0 && count != length) {
        return collector.reject(&mut state, stream, 2);
    }
    state.counts[index] += count;
    *confirmed = count;
    if class != 0 { collector.reject(&mut state, stream, class) } else { 0 }
}
impl Request {
    pub fn new(frame: &[u8], input: &[u8], limits: Limits, timeout_ms: u64, forward: bool, output: &Output) -> Self {
        Self { frame: frame.as_ptr(), frame_size: frame.len(), input: input.as_ptr(), input_size: input.len(),
            timeout_ms, cleanup_ms: limits.cleanup_ms, output_bytes: limits.output_bytes,
            output_mode: u32::from(forward), stop: stopped, stop_token: Arc::as_ptr(&output.stop),
            output: publish, output_token: (output as *const Output).cast_mut().cast() }
    }
}
