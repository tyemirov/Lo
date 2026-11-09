use super::{ownership::{Controller, Image, Limits as DescendantLimits, Purpose},
    provider::{Admission, Configuration, FileIdentity, Provider, Token},
    supervisor_wire::{Channel, Decoder, Descriptor, Encoder, Kind, Packet, Received}};
use std::{mem::{size_of, zeroed}, os::fd::RawFd, sync::Arc};

const CODE_ID_MAX: usize = 64;
#[path = "process_supervisor_transport.rs"]
mod transport;
#[path = "process_supervisor_protocol.rs"]
mod protocol;
#[path = "process_supervisor_parent.rs"]
mod parent;
#[path = "process_supervisor_child.rs"]
mod child;
pub(super) use parent::{Inputs, Completion, execute};
pub(super) use protocol::{Setup, reservation};
pub(super) fn main() -> i32 { child::main() }
#[repr(C)]
#[derive(Clone, Copy)]
pub(super) struct HostIdentity {
    token: Token,
    code_size: u32,
    code: [u8; CODE_ID_MAX],
}
impl HostIdentity {
    pub fn token(self) -> Token { self.token }
    pub fn current() -> Result<Self, &'static str> {
        #[cfg(target_os = "macos")]
        unsafe {
            let mut identity: Self = zeroed();
            if lo_process_self_identity(&mut identity) == 0 && identity.valid() { return Ok(identity); }
        }
        Err("Process supervisor code identity is unavailable")
    }
    fn valid(&self) -> bool {
        self.token.valid() && self.code_size != 0 && self.code_size as usize <= self.code.len()
            && self.code[self.code_size as usize..].iter().all(|byte| *byte == 0)
    }
    pub fn verify_peer(self, channel: &Channel, claimed: Self, expected_pid: i32) -> Result<(), &'static str> {
        if !self.valid() || !claimed.valid() || expected_pid <= 0 || claimed.token.pid() != expected_pid as u32
            || self.code_size != claimed.code_size || self.code != claimed.code {
            return Err("Process supervisor bootstrap identity differs");
        }
        #[cfg(target_os = "macos")]
        unsafe {
            if lo_process_verify_peer_channel(channel.raw(), &claimed, expected_pid) == 0 { return Ok(()); }
        }
        Err("Process supervisor peer identity was not verified")
    }
    pub fn hello(self, generation: u64) -> Result<Vec<u8>, &'static str> {
        let mut output = Encoder::new();
        for word in self.token.word { output.u32(word)?; }
        output.u32(self.code_size)?;
        output.bytes(&self.code)?;
        output.u64(generation)?;
        Ok(output.finish())
    }
    pub fn from_hello(packet: Packet) -> Result<(Self, u64), &'static str> {
        if packet.kind != Kind::Hello || packet.descriptor.is_some() {
            return Err("Process supervisor did not send an initial identity");
        }
        let mut input = Decoder::new(&packet.body);
        let mut token = Token::default();
        for word in &mut token.word { *word = input.u32()?; }
        let code_size = input.u32()?;
        let code = input.bytes(CODE_ID_MAX)?.try_into().unwrap();
        let generation = input.u64()?;
        input.finish()?;
        let identity = Self { token, code_size, code };
        if !identity.valid() { return Err("Process supervisor identity fields are invalid"); }
        Ok((identity, generation))
    }
}

pub(super) fn now() -> Result<u64, &'static str> {
    let mut value: libc::timespec = unsafe { zeroed() };
    if unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut value) } != 0
        || value.tv_sec < 0 || value.tv_nsec < 0 || value.tv_nsec >= 1_000_000_000 {
        return Err("Process monotonic clock is unavailable");
    }
    (value.tv_sec as u64).checked_mul(1_000_000_000).and_then(|seconds| seconds.checked_add(value.tv_nsec as u64))
        .ok_or("Process monotonic clock is not representable")
}
#[derive(Clone, Copy)]
pub(super) struct Deadlines { pub command: u64, pub cleanup_ms: u64 }
impl Deadlines {
    pub fn new(timeout_ms: u64, cleanup_ms: u64) -> Result<Self, &'static str> {
        if timeout_ms == 0 || cleanup_ms == 0 { return Err("Process deadlines must be positive"); }
        let command = timeout_ms.checked_mul(1_000_000).and_then(|duration| now().ok()?.checked_add(duration))
            .ok_or("Process command deadline is not representable")?;
        command.checked_add(cleanup_ms.checked_mul(1_000_000).ok_or("Process cleanup duration overflow")?)
            .ok_or("Process cleanup deadline overflow")?;
        Ok(Self { command, cleanup_ms })
    }
    pub fn cleanup(self) -> Result<u64, &'static str> {
        let started = now()?.min(self.command);
        started.checked_add(self.cleanup_ms.checked_mul(1_000_000).ok_or("Process cleanup duration overflow")?)
            .ok_or("Process cleanup deadline overflow")
    }
}
pub(super) fn poll(descriptor: RawFd, events: i16, deadline: u64) -> Result<(), &'static str> {
    let remaining = deadline.saturating_sub(now()?);
    if remaining == 0 { return Err("Process deadline expired"); }
    let milliseconds = (remaining / 1_000_000).clamp(1, 5) as i32;
    let mut pending = libc::pollfd { fd: descriptor, events, revents: 0 };
    let result = unsafe { libc::poll(&mut pending, 1, milliseconds) };
    if result < 0 && std::io::Error::last_os_error().kind() != std::io::ErrorKind::Interrupted {
        Err("Process descriptor polling failed")
    } else { Ok(()) }
}
pub(super) fn send(channel: &mut Channel, kind: Kind, body: &[u8], descriptor: Option<RawFd>,
    deadline: u64, mut progress: impl FnMut() -> Result<(), &'static str>) -> Result<(), &'static str> {
    progress()?;
    channel.queue(kind, body, descriptor)?;
    loop {
        progress()?;
        if now()? >= deadline { return Err("Process deadline expired during control output"); }
        if channel.flush()? { return Ok(()); }
        poll(channel.raw(), libc::POLLOUT, deadline)?;
    }
}
pub(super) fn receive(channel: &mut Channel, deadline: u64,
    mut progress: impl FnMut() -> Result<(), &'static str>) -> Result<Packet, &'static str> {
    loop {
        progress()?;
        if now()? >= deadline { return Err("Process deadline expired during control input"); }
        match channel.receive()? {
            Received::Packet(packet) => return Ok(packet),
            Received::Eof => return Err("Process supervisor control channel closed"),
            Received::Pending => poll(channel.raw(), libc::POLLIN, deadline)?,
        }
    }
}

pub(super) struct Owner {
    pub controller: Arc<Controller>,
    provider: Provider,
    marker: Option<Descriptor>,
    marker_identity: FileIdentity,
    temporary_marker: Option<(u64, Descriptor)>,
    admitted: bool,
    closed: bool,
}
pub(super) enum OpenOwner { Ready(Owner), Unavailable, Failed(Owner) }
impl Owner {
    pub fn provider_storage(tokens: u32) -> Result<u64, &'static str> {
        #[cfg(target_os = "macos")]
        unsafe {
            let bytes = lo_process_es_storage(tokens);
            if bytes != 0 { return Ok(bytes as u64); }
        }
        Err("Process provider storage is unavailable")
    }
    // The enclosing supervisor retains all transferred resources before this
    // call. It keeps Owner and those resources together on every unknown path.
    pub fn open(generation: u64, limits: DescendantLimits, images: Vec<Image>) -> Result<OpenOwner, &'static str> {
        #[cfg(not(target_os = "macos"))]
        { let _ = (generation, limits, images); return Ok(OpenOwner::Unavailable); }
        #[cfg(target_os = "macos")]
        {
            limits.storage()?;
            let identity = HostIdentity::current()?;
            if unsafe { lo_process_kernel_limit(limits.kernel_processes, &identity.token) } != 0 {
                return Ok(OpenOwner::Unavailable);
            }
            let marker = unsafe { libc::open(b"/dev/null\0".as_ptr().cast(), libc::O_RDONLY | libc::O_NOFOLLOW | libc::O_CLOEXEC) };
            if marker < 0 { return Err("Process marker resource is unavailable"); }
            let mut marker = unsafe { Descriptor::take(marker) };
            let marker_identity = match descriptor_identity(&marker) {
                Ok(identity) => identity,
                Err(reason) => { marker.close()?; return Err(reason); }
            };
            let configuration = Configuration { generation, supervisor: identity.token, marker: marker_identity,
                max_events: limits.max_events, max_processes: limits.max_processes,
                max_tokens: limits.max_tokens, kernel_processes: limits.kernel_processes };
            let controller = match Controller::new(configuration, images) {
                Ok(controller) => Arc::new(controller),
                Err(reason) => { marker.close()?; return Err(reason); }
            };
            let (admission, mut provider) = Provider::open(configuration, controller.clone());
            if admission == Admission::Unavailable {
                provider.close()?;
                marker.close()?;
                return Ok(OpenOwner::Unavailable);
            }
            let owner = Self { controller, provider, marker: Some(marker), marker_identity,
                temporary_marker: None, admitted: false, closed: false };
            Ok(if admission == Admission::Ready { OpenOwner::Ready(owner) } else { OpenOwner::Failed(owner) })
        }
    }
    pub fn begin_marker(&mut self, purpose: Purpose) -> Result<u64, &'static str> {
        let request = self.controller.begin_marker(purpose)?;
        let descriptor = self.provider.open_marker(request).map_err(|reason| { self.controller.damage(reason); reason })?;
        let descriptor = unsafe { Descriptor::take(descriptor) };
        self.temporary_marker = Some((request, descriptor));
        let identity = descriptor_identity(&self.temporary_marker.as_ref().unwrap().1)
            .map_err(|reason| { self.controller.damage(reason); reason })?;
        if identity != self.marker_identity { self.controller.damage("Process marker identity changed"); }
        self.controller.marker_opened(request, identity)?;
        // Mark submission before entering a provider that can call back inline.
        self.controller.marker_submitted(request)?;
        self.provider.synchronize(request).map_err(|reason| { self.controller.damage(reason); reason })?;
        Ok(request)
    }
    pub fn finish_marker(&mut self, request: u64) -> Result<bool, &'static str> {
        if let Some(reason) = self.controller.failure() { return Err(reason); }
        if !self.controller.marker_ready(request) { return Ok(false); }
        let Some((pending, mut descriptor)) = self.temporary_marker.take() else {
            self.controller.damage("Process temporary marker descriptor is missing");
            return Err("Process temporary marker descriptor is missing");
        };
        if pending != request {
            self.temporary_marker = Some((pending, descriptor));
            self.controller.damage("Process temporary marker request differs");
            return Err("Process temporary marker request differs");
        }
        descriptor.close().map_err(|reason| { self.controller.damage(reason); reason })?;
        if self.controller.finish_marker(request)? == Purpose::Admission { self.admitted = true; }
        Ok(true)
    }
    pub fn service(&self, deadline: u64, mut validate_registry: impl FnMut(&Image) -> bool) -> Result<(), &'static str> {
        while self.controller.reply_one(&self.provider, &mut validate_registry) {
            if now()? >= deadline { return Err("Process deadline expired during authorization cleanup"); }
        }
        while let Some(token) = self.controller.next_termination() {
            let result = self.provider.signal(token, libc::SIGKILL);
            // A stale token can be superseded by a queued EXEC. ESRCH never
            // removes an owner. A confirmed new token is scheduled again.
            if let Err(error) = result {
                if error != libc::ESRCH { return Err("Process token-bound termination failed"); }
            }
            if now()? >= deadline { return Err("Process deadline expired during descendant termination"); }
        }
        Ok(())
    }
    pub fn close(&mut self, child_reaped: bool, channels_closed: bool, sinks_joined: bool) -> Result<(), &'static str> {
        self.controller.begin_close(child_reaped, channels_closed, sinks_joined)?;
        self.provider.close().map_err(|reason| { self.controller.damage(reason); reason })?;
        if let Some(marker) = &mut self.marker {
            marker.close().map_err(|reason| { self.controller.damage(reason); reason })?;
        }
        self.controller.finish_close()?;
        self.closed = true;
        Ok(())
    }

}
impl Drop for Owner {
    fn drop(&mut self) {
        if !self.closed {
            // The top-level supervisor moves unknown activations into its
            // non-returning quarantine loop. Preserve marker descriptors if an
            // earlier error still abandons this wrapper.
            if let Some((_, descriptor)) = self.temporary_marker.take() { std::mem::forget(descriptor); }
            if let Some(descriptor) = self.marker.take() { std::mem::forget(descriptor); }
        }
    }
}
pub(super) fn descriptor_identity(descriptor: &Descriptor) -> Result<FileIdentity, &'static str> {
    let mut metadata: libc::stat = unsafe { zeroed() };
    if unsafe { libc::fstat(descriptor.raw(), &mut metadata) } != 0 {
        return Err("Process retained descriptor identity is unavailable");
    }
    Ok(FileIdentity { device: metadata.st_dev as u64, inode: metadata.st_ino as u64 })
}

#[cfg(target_os = "macos")]
extern "C" {
    fn lo_process_self_identity(identity: *mut HostIdentity) -> i32;
    fn lo_process_verify_peer_channel(descriptor: i32, claimed: *const HostIdentity, expected_pid: i32) -> i32;
    fn lo_process_kernel_limit(requested: u32, expected: *const Token) -> i32;
    fn lo_process_es_storage(tokens: u32) -> usize;
}
