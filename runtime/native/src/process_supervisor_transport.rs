use super::{super::io, Controller, Deadlines, Token};
use std::{ffi::{c_void, CString}, sync::{atomic::{AtomicBool, AtomicI32, AtomicU32, Ordering},
    mpsc::{self, Receiver, SyncSender}, Arc, Mutex}, thread::JoinHandle};

pub(super) struct Inputs {
    pub launcher: String,
    pub embedded: bool,
    pub directory: String,
    pub configuration: Vec<u8>,
    pub frame: Vec<u8>,
    pub input: Vec<u8>,
    pub limits: io::Limits,
    pub forward: bool,
    pub deadlines: Deadlines,
}
pub(super) struct Ready {
    pub pid: i32,
    pub token: Token,
    pub response: SyncSender<i32>,
}
struct Shared {
    controller: Arc<Controller>,
    stop: Arc<AtomicU32>,
    created: AtomicI32,
    reaped: AtomicBool,
    ready: SyncSender<Ready>,
}
pub(super) struct Transport {
    shared: Arc<Shared>,
    pub ready: Receiver<Ready>,
    pub output: Receiver<io::Event>,
    thread: Option<JoinHandle<()>>,
}
#[repr(C)]
struct Hooks {
    context: *mut c_void,
    launch: unsafe extern "C" fn(*mut c_void) -> i32,
    created: unsafe extern "C" fn(*mut c_void, i32),
    ready: unsafe extern "C" fn(*mut c_void, i32, *const Token) -> i32,
    reaped: unsafe extern "C" fn(*mut c_void),
    stopping: unsafe extern "C" fn(*mut c_void, i32, i32),
}
unsafe fn shared<'a>(pointer: *mut c_void) -> &'a Shared { &*pointer.cast() }
unsafe extern "C" fn launch(pointer: *mut c_void) -> i32 {
    let owner = shared(pointer);
    let stop = owner.stop.load(Ordering::SeqCst);
    if stop != 0 { return stop as i32; }
    if owner.controller.start_launch().is_ok() { 0 } else { 2 }
}
unsafe extern "C" fn created(pointer: *mut c_void, pid: i32) {
    let owner = shared(pointer);
    if pid <= 0 || owner.created.compare_exchange(0, pid, Ordering::SeqCst, Ordering::SeqCst).is_err() {
        owner.controller.damage("Process transport returned an invalid child receipt");
    }
}
unsafe extern "C" fn ready_hook(pointer: *mut c_void, pid: i32, token: *const Token) -> i32 {
    let owner = shared(pointer);
    if token.is_null() || pid <= 0 || owner.created.load(Ordering::SeqCst) != pid || (*token).pid() != pid as u32 {
        owner.controller.damage("Process Ready token differs from the retained direct child");
        return 2;
    }
    let (response, received) = mpsc::sync_channel(1);
    if owner.ready.send(Ready { pid, token: *token, response }).is_err() { return 2; }
    // The main supervisor services this receipt alongside ES and cancellation.
    // It never drops a pending Ready without sending a denial.
    received.recv().unwrap_or(2)
}
unsafe extern "C" fn reaped(pointer: *mut c_void) {
    let owner = shared(pointer);
    if owner.created.load(Ordering::SeqCst) <= 0 || owner.reaped.swap(true, Ordering::SeqCst) {
        owner.controller.damage("Process direct-child reap receipt is invalid");
    }
    owner.controller.freeze();
}
unsafe extern "C" fn stopping(pointer: *mut c_void, _stage: i32, _classification: i32) {
    shared(pointer).controller.freeze();
}
impl Transport {
    pub fn spawn(controller: Arc<Controller>, inputs: Inputs) -> Result<Self, &'static str> {
        let (ready_tx, ready) = mpsc::sync_channel(1);
        let (output_tx, output) = mpsc::sync_channel(1);
        let shared = Arc::new(Shared { controller, stop: Arc::new(AtomicU32::new(0)),
            created: AtomicI32::new(0), reaped: AtomicBool::new(false), ready: ready_tx });
        let worker = shared.clone();
        let thread = std::thread::Builder::new().name("lo-process-transport".into()).spawn(move || {
            let mut result = io::NativeResult::default();
            let collector = io::Output { events: output_tx.clone(), stop: worker.stop.clone(),
                state: Mutex::new(io::OutputState::default()), maximum: inputs.limits.output_bytes };
            let remaining = super::now().ok().map(|now| inputs.deadlines.command.saturating_sub(now) / 1_000_000).unwrap_or(0);
            if remaining == 0 {
                result.stage = 1; result.class_id = 5; result.cleanup_complete = 1;
            } else if let (Ok(launcher), Ok(directory)) = (CString::new(inputs.launcher), CString::new(inputs.directory)) {
                let request = io::Request::new(&inputs.frame, &inputs.input, inputs.limits, remaining,
                    inputs.forward, &collector);
                let hooks = Hooks { context: Arc::as_ptr(&worker).cast_mut().cast(),
                    launch, created, ready: ready_hook, reaped, stopping };
                #[cfg(target_os = "macos")]
                unsafe {
                    lo_native_process_execute_descendants(launcher.as_ptr(), i32::from(inputs.embedded), directory.as_ptr(),
                        inputs.configuration.as_ptr(), inputs.configuration.len(), &request, &mut result, &hooks);
                }
                #[cfg(not(target_os = "macos"))]
                { let _ = (launcher, directory, request, hooks); result.stage = 1; result.class_id = 2; result.cleanup_complete = 1; }
            } else { result.stage = 1; result.class_id = 2; result.cleanup_complete = 1; }
            collector.complete(&mut result, inputs.input.len());
            let _ = output_tx.send(io::Event::Complete(result));
        }).map_err(|_| "Process transport thread is unavailable")?;
        Ok(Self { shared, ready, output, thread: Some(thread) })
    }
    pub fn stop(&self, class: u32) {
        let class = if (1..=5).contains(&class) { class } else { 2 };
        let _ = self.shared.stop.compare_exchange(0, class, Ordering::SeqCst, Ordering::SeqCst);
        self.shared.controller.freeze();
    }
    pub fn child_pid(&self) -> i32 { self.shared.created.load(Ordering::SeqCst) }
    pub fn child_reaped(&self) -> bool {
        self.shared.created.load(Ordering::SeqCst) == 0 || self.shared.reaped.load(Ordering::SeqCst)
    }
    pub fn finished(&self) -> bool { self.thread.as_ref().is_none_or(JoinHandle::is_finished) }
    pub fn join(&mut self) -> Result<(), &'static str> {
        if !self.finished() { return Err("Process transport thread still owns command resources"); }
        if self.thread.take().is_some_and(|thread| thread.join().is_err()) {
            Err("Process transport thread failed")
        } else { Ok(()) }
    }
}
#[cfg(target_os = "macos")]
extern "C" {
    fn lo_native_process_execute_descendants(launcher: *const i8, embedded: i32, directory: *const i8,
        configuration: *const u8, configuration_size: usize, request: *const io::Request,
        result: *mut io::NativeResult, hooks: *const Hooks) -> i32;
}
