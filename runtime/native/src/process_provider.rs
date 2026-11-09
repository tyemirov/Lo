use super::ownership::Controller;
use std::{ffi::c_void, sync::{Arc, Mutex}};

pub(super) const PATH_MAX: usize = 1024;
pub(super) const FORK: u32 = 1;
pub(super) const AUTH_EXEC: u32 = 2;
pub(super) const EXEC: u32 = 3;
pub(super) const EXIT: u32 = 4;
pub(super) const OPEN: u32 = 5;
pub(super) const SYNC: u32 = 6;
pub(super) const FAILURE: u32 = 7;

#[repr(C)]
#[derive(Clone, Copy, Default, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub(super) struct Token { pub word: [u32; 8] }
impl Token {
    pub fn pid(self) -> u32 { self.word[5] }
    pub fn is_zero(self) -> bool { self.word == [0; 8] }
    pub fn valid(self) -> bool { self.pid() != 0 && self.pid() <= i32::MAX as u32 }
    pub fn credentials_match(self, owner: Self) -> bool {
        [0, 1, 2, 3, 4, 6].into_iter().all(|index| self.word[index] == owner.word[index])
    }
}
#[repr(C)]
#[derive(Clone, Copy, Default, Debug, Eq, PartialEq)]
pub(super) struct FileIdentity { pub device: u64, pub inode: u64 }
impl FileIdentity {
    fn is_zero(self) -> bool { self == Self::default() }
}
#[repr(C)]
#[derive(Clone, Copy)]
pub(super) struct Configuration {
    pub generation: u64,
    pub supervisor: Token,
    pub marker: FileIdentity,
    pub max_events: u64,
    pub max_processes: u32,
    pub max_tokens: u32,
    pub kernel_processes: u32,
}
#[repr(C)]
pub(super) struct RawEvent {
    kind: u32,
    generation: u64,
    sequence: u64,
    request: u64,
    actor: Token,
    target: Token,
    file: FileIdentity,
    path: *const u8,
    path_size: usize,
    flags: u32,
    status: i32,
}
pub(super) struct Event<'a> {
    pub kind: u32,
    pub generation: u64,
    pub sequence: u64,
    pub request: u64,
    pub actor: Token,
    pub target: Token,
    pub file: FileIdentity,
    pub path: &'a str,
    pub flags: u32,
    pub status: i32,
}
impl RawEvent {
    // The provider is trusted code. It must supply valid borrowed storage, but
    // it cannot bypass field, length, transition, or sequence validation.
    unsafe fn checked(&self) -> Result<Event<'_>, &'static str> {
        if !(FORK..=FAILURE).contains(&self.kind) || self.generation == 0 {
            return Err("invalid provider event kind or generation");
        }
        if self.path_size > PATH_MAX || (self.path_size != 0 && self.path.is_null())
            || (self.path_size == 0 && !self.path.is_null()) {
            return Err("provider event path exceeds its bound");
        }
        let path = if self.path_size == 0 { "" } else {
            let bytes = std::slice::from_raw_parts(self.path, self.path_size);
            if bytes.contains(&0) { return Err("provider event path contains NUL"); }
            std::str::from_utf8(bytes).map_err(|_| "provider event path is not UTF-8")?
        };
        let zero_actor = self.actor.is_zero();
        let zero_target = self.target.is_zero();
        let zero_file = self.file.is_zero();
        let plain = self.flags == 0 && self.status == 0;
        let valid = match self.kind {
            FORK => plain && self.actor.valid() && self.target.valid()
                && zero_file && path.is_empty() && self.request == 0,
            AUTH_EXEC => plain && self.actor.valid() && self.target.valid()
                && !zero_file && path.starts_with('/') && self.request != 0,
            EXEC => plain && self.actor.valid() && zero_target && !zero_file
                && path.starts_with('/') && self.request == 0,
            EXIT => plain && self.actor.valid() && zero_target && zero_file
                && path.is_empty() && self.request == 0,
            OPEN => self.actor.valid() && zero_target && !zero_file && self.request == 0
                && self.status == 0 && self.flags != 0 && self.flags & !3 == 0
                && (path.is_empty() || path.starts_with('/')),
            SYNC => plain && zero_actor && zero_target && zero_file && path.is_empty()
                && self.sequence == 0 && self.request != 0,
            FAILURE => self.flags == 0 && (1..=3).contains(&self.status) && zero_actor
                && zero_target && zero_file && path.is_empty() && self.sequence == 0
                && self.request == 0,
            _ => false,
        };
        if !valid { return Err("provider event contains invalid or reserved fields"); }
        Ok(Event { kind: self.kind, generation: self.generation, sequence: self.sequence,
            request: self.request, actor: self.actor, target: self.target, file: self.file,
            path, flags: self.flags, status: self.status })
    }
}
pub(super) type Emit = unsafe extern "C" fn(*mut c_void, *const RawEvent) -> i32;
#[repr(C)]
#[derive(Clone, Copy, Default)]
pub(super) struct RawClient {
    handle: usize,
    open_marker: Option<unsafe extern "C" fn(usize, u64) -> i32>,
    synchronize: Option<unsafe extern "C" fn(usize, u64) -> i32>,
    respond: Option<unsafe extern "C" fn(usize, u64, u32) -> i32>,
    signal: Option<unsafe extern "C" fn(usize, *const Token, i32) -> i32>,
    close: Option<unsafe extern "C" fn(usize) -> i32>,
}
impl RawClient {
    fn complete(self) -> bool {
        self.handle != 0 && self.open_marker.is_some() && self.synchronize.is_some()
            && self.respond.is_some() && self.signal.is_some() && self.close.is_some()
    }
    fn empty(self) -> bool {
        self.handle == 0 && self.open_marker.is_none() && self.synchronize.is_none()
            && self.respond.is_none() && self.signal.is_none() && self.close.is_none()
    }
}
pub(super) type Factory = unsafe extern "C" fn(usize, *const Configuration, Emit,
    *mut c_void, *mut RawClient) -> u32;
#[derive(Default)]
struct Registration { sealed: bool, selected: Option<(Factory, usize)> }
static REGISTRATION: Mutex<Registration> = Mutex::new(Registration { sealed: false, selected: None });

#[no_mangle]
pub unsafe extern "C" fn lo_process_ownership_provider(factory: Option<Factory>, userdata: usize) -> i32 {
    let Some(factory) = factory else { return 0; };
    let mut registration = REGISTRATION.lock().unwrap();
    if registration.sealed || registration.selected.is_some() { return 0; }
    registration.selected = Some((factory, userdata));
    1
}
pub(super) fn seal_registration() { REGISTRATION.lock().unwrap().sealed = true; }

struct Destination { controller: Arc<Controller> }
unsafe extern "C" fn emit(destination: *mut c_void, event: *const RawEvent) -> i32 {
    let destination = &*destination.cast::<Destination>();
    if event.is_null() {
        destination.controller.damage("provider returned a null event");
        return 0;
    }
    match (&*event).checked() {
        Ok(event) => i32::from(destination.controller.receive(event)),
        Err(reason) => { destination.controller.damage(reason); 0 }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(super) enum Admission { Ready, Unavailable, Failed }
pub(super) struct Provider {
    client: RawClient,
    destination: Option<Box<Destination>>,
    closed: bool,
}
impl Provider {
    pub fn open(configuration: Configuration, controller: Arc<Controller>) -> (Admission, Self) {
        let selected = {
            let mut registration = REGISTRATION.lock().unwrap();
            registration.sealed = true;
            registration.selected
        };
        let mut destination = Box::new(Destination { controller });
        let mut client = RawClient::default();
        #[cfg(target_os = "macos")]
        let result = unsafe {
            let pointer = (&mut *destination as *mut Destination).cast();
            match selected {
                Some((factory, userdata)) => factory(userdata, &configuration, emit, pointer, &mut client),
                None => native_open(&configuration, emit, pointer, &mut client),
            }
        };
        #[cfg(not(target_os = "macos"))]
        let result = { let _ = selected; 1 };
        let admission = match result {
            0 if client.complete() => Admission::Ready,
            1 if client.empty() => Admission::Unavailable,
            _ => Admission::Failed,
        };
        if admission == Admission::Failed {
            destination.controller.damage("provider admission failed or returned an invalid client");
        }
        let closed = client.empty();
        (admission, Self { client, destination: Some(destination), closed })
    }
    pub fn open_marker(&self, request: u64) -> Result<i32, &'static str> {
        if self.closed { return Err("provider is closed"); }
        let Some(operation) = self.client.open_marker else { return Err("provider marker operation is missing"); };
        let descriptor = unsafe { operation(self.client.handle, request) };
        if descriptor < 0 { Err("provider marker open failed") } else { Ok(descriptor) }
    }
    pub fn synchronize(&self, request: u64) -> Result<(), &'static str> {
        if self.closed { return Err("provider is closed"); }
        let Some(operation) = self.client.synchronize else { return Err("provider synchronization operation is missing"); };
        if unsafe { operation(self.client.handle, request) } != 0 {
            Err("provider synchronization failed")
        } else { Ok(()) }
    }
    pub fn respond(&self, request: u64, allow: bool) -> Result<(), &'static str> {
        if self.closed { return Err("provider is closed"); }
        let Some(operation) = self.client.respond else { return Err("provider authorization operation is missing"); };
        if unsafe { operation(self.client.handle, request, u32::from(allow)) } != 0 {
            Err("provider authorization reply failed")
        } else { Ok(()) }
    }
    pub fn signal(&self, token: Token, number: i32) -> Result<(), i32> {
        if self.closed { return Err(libc::EBADF); }
        let Some(operation) = self.client.signal else { return Err(libc::EINVAL); };
        let error = unsafe { operation(self.client.handle, &token, number) };
        if error == 0 { Ok(()) } else { Err(error) }
    }
    pub fn close(&mut self) -> Result<(), &'static str> {
        if self.closed { return Ok(()); }
        let Some(operation) = self.client.close else { return Err("provider close operation is missing"); };
        if unsafe { operation(self.client.handle) } != 0 { return Err("provider close did not join its callbacks"); }
        self.closed = true;
        self.destination.take();
        Ok(())
    }
}
impl Drop for Provider {
    fn drop(&mut self) {
        // Unknown ownership must keep callback storage alive. The supervisor
        // also retains its transferred resource owners and does not exit.
        if !self.closed {
            if let Some(destination) = self.destination.take() { Box::leak(destination); }
        }
    }
}
#[cfg(target_os = "macos")]
unsafe fn native_open(configuration: *const Configuration, emit: Emit, destination: *mut c_void,
    client: *mut RawClient) -> u32 {
    extern "C" { fn lo_process_es_open(configuration: *const Configuration, emit: Emit,
        destination: *mut c_void, client: *mut RawClient) -> u32; }
    lo_process_es_open(configuration, emit, destination, client)
}
#[cfg(not(target_os = "macos"))]
unsafe fn native_open(_: *const Configuration, _: Emit, _: *mut c_void, _: *mut RawClient) -> u32 { 1 }
