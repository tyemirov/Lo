use super::{super::{io, resources::{Inventory, RemoteReleaseProof}}, protocol, Channel, Deadlines, Decoder, Descriptor,
    Encoder, HostIdentity, Kind, Received};
use std::{ffi::CString, sync::atomic::Ordering};

pub(in crate::process) struct Inputs {
    pub image: String,
    pub generation: u64,
    pub inventory: Inventory,
    pub setup: protocol::Setup,
    pub configuration: Vec<u8>,
    pub command: Vec<u8>,
    pub input: Vec<u8>,
}
pub(in crate::process) struct Completion { pub result: io::NativeResult, pub unavailable: bool, pub released: u64 }
fn wait_child(pid: i32, deadline: u64) -> Result<bool, &'static str> {
    loop {
        let mut status = 0;
        let result = unsafe { libc::waitpid(pid, &mut status, libc::WNOHANG) };
        if result == pid { return Ok(libc::WIFEXITED(status) && libc::WEXITSTATUS(status) == 0); }
        if result < 0 && std::io::Error::last_os_error().kind() != std::io::ErrorKind::Interrupted {
            return Err("Process supervisor reap failed");
        }
        if super::now()? >= deadline { return Err("Process supervisor reap deadline expired"); }
        std::thread::sleep(std::time::Duration::from_millis(1));
    }
}
fn chunks(channel: &mut Channel, kind: Kind, bytes: &[u8], deadline: u64, output: &io::Output) -> Result<(), &'static str> {
    for (index, chunk) in bytes.chunks(io::CHUNK_BYTES).enumerate() {
        let mut body = Encoder::new(); body.u64((index * io::CHUNK_BYTES) as u64)?; body.bytes(chunk)?;
        super::send(channel, kind, &body.finish(), None, deadline, || startup_progress(output))?;
    }
    Ok(())
}
fn startup_progress(output: &io::Output) -> Result<(), &'static str> {
    if output.stop.load(Ordering::SeqCst) == 0 { Ok(()) }
    else { Err("Process supervisor startup was cancelled") }
}
fn clean_local(inventory: &mut Inventory) -> Result<(), &'static str> {
    inventory.close().map_err(|_| "Process local resource release failed")?;
    inventory.release_remote(RemoteReleaseProof::complete(true, true, true)?)
        .map_err(|_| "Process lock freeze release failed")
}
fn bound_cleanup(deadline: &mut u64, cleanup_ms: u64) -> Result<(), &'static str> {
    *deadline = (*deadline).min(super::now()?.saturating_add(cleanup_ms.saturating_mul(1_000_000)));
    Ok(())
}
pub(in crate::process) fn execute(mut inputs: Inputs, output: &io::Output) -> Completion {
    let mut result = io::NativeResult { stage: 1, class_id: 2, ..Default::default() };
    let reserved = inputs.setup.storage + inputs.inventory.storage();
    let mut pid = 0;
    let mut spawn_cleanup = 1u32;
    let mut local_cleanup = true;
    let mut channel: Option<Channel> = None;
    let mut peer: Option<Descriptor> = None;
    let mut receipt = None;
    let mut eof = false;
    let mut clean_exit = false;
    let deadlines = inputs.setup.deadlines;
    let final_deadline = deadlines.command.saturating_add(deadlines.cleanup_ms.saturating_mul(1_000_000));
    let attempt = (|| -> Result<(), &'static str> {
        let identity = HostIdentity::current()?;
        let mut pair = [-1; 2];
        if unsafe { libc::socketpair(libc::AF_UNIX, libc::SOCK_STREAM, 0, pair.as_mut_ptr()) } != 0 {
            return Err("Process supervisor channel creation failed");
        }
        let local = unsafe { Descriptor::take(pair[0]) };
        peer = Some(unsafe { Descriptor::take(pair[1]) });
        match Channel::new(local) {
            Ok(created) => channel = Some(created),
            Err((failed, reason)) => { channel = Some(failed); return Err(reason); }
        }
        // Both endpoints are adopted before any fallible inheritance setup.
        for descriptor in [channel.as_ref().unwrap().raw(), peer.as_ref().unwrap().raw()] {
            if unsafe { libc::fcntl(descriptor, libc::F_SETFD, libc::FD_CLOEXEC) } != 0 {
                return Err("Process supervisor descriptor inheritance failed");
            }
        }
        let image = CString::new(inputs.image.as_str()).map_err(|_| "Process supervisor image is invalid")?;
        #[cfg(target_os = "macos")]
        let spawned = unsafe { lo_process_supervisor_spawn(image.as_ptr(), peer.as_ref().unwrap().raw(), &mut pid, &mut spawn_cleanup) };
        #[cfg(not(target_os = "macos"))]
        let spawned = { let _ = image; libc::ENOTSUP };
        let closed = peer.as_mut().unwrap().close();
        local_cleanup &= closed.is_ok();
        peer = None;
        closed?;
        if spawned != 0 || pid <= 0 { return Err("Process supervisor creation failed"); }
        let channel = channel.as_mut().unwrap();
        super::send(channel, Kind::Hello, &identity.hello(inputs.generation)?, None, deadlines.command, || startup_progress(output))?;
        let (remote, generation) = HostIdentity::from_hello(super::receive(channel, deadlines.command, || startup_progress(output))?)?;
        if generation != 0 { return Err("Process supervisor initial generation differs"); }
        // Capture LOCAL_PEERTOKEN only after the child's handshake. Verify the
        // current exact token against this still-unreaped spawn receipt.
        identity.verify_peer(channel, remote, pid)?;
        channel.bind_generation(inputs.generation)?;
        super::send(channel, Kind::Initialize, &inputs.setup.encode()?, None, deadlines.command, || startup_progress(output))?;
        let mut stop_deadline = final_deadline;
        for index in 0..inputs.inventory.len() {
            let (body, descriptor) = inputs.inventory.frame(index)?;
            super::send(channel, Kind::Resource, &body, Some(descriptor), deadlines.command, || startup_progress(output))?;
            let mut adopted = super::receive(channel, deadlines.command, || startup_progress(output))?;
            if let Some(descriptor) = adopted.descriptor.take() {
                let _ = inputs.inventory.receive(&adopted.body, descriptor);
                return Err("Process adoption acknowledgment unexpectedly transferred a descriptor");
            }
            if adopted.kind == Kind::Receipt {
                receipt = Some(protocol::startup_result(adopted)?);
                bound_cleanup(&mut stop_deadline, deadlines.cleanup_ms)?;
                break;
            }
            if adopted.kind != Kind::ResourceAdopted { return Err("Process resource adoption acknowledgment is missing"); }
            let mut body = Decoder::new(&adopted.body);
            if body.u64()? != index as u64 { return Err("Process resource adoption index differs"); }
            body.finish()?;
        }
        if receipt.is_none() {
            chunks(channel, Kind::Configuration, &inputs.configuration, deadlines.command, output)?;
            chunks(channel, Kind::Command, &inputs.command, deadlines.command, output)?;
            chunks(channel, Kind::Input, &inputs.input, deadlines.command, output)?;
            super::send(channel, Kind::SetupDone, &[], None, deadlines.command, || startup_progress(output))?;
            let accepted = super::receive(channel, deadlines.command, || startup_progress(output))?;
            if accepted.kind == Kind::Receipt {
                receipt = Some(protocol::startup_result(accepted)?);
                bound_cleanup(&mut stop_deadline, deadlines.cleanup_ms)?;
            } else {
                protocol::empty(accepted, Kind::Accepted)?;
                // Inventory remains held locally and remotely across this final gate.
                inputs.inventory.validate(|| {
                    if output.stop.load(Ordering::SeqCst) != 0 || super::now().unwrap_or(u64::MAX) >= deadlines.command {
                        Err(crate::context::host("Process launch was cancelled"))
                    } else { Ok(()) }
                }).map_err(|_| "Process resource validation failed before launch")?;
                super::send(channel, Kind::Launch, &[], None, deadlines.command, || startup_progress(output))?;
            }
        }
        let mut last_output = 0u64;
        let mut cancelled = false;
        loop {
            let now = super::now()?;
            if now >= deadlines.command { let _ = output.stop.compare_exchange(0, 5, Ordering::SeqCst, Ordering::SeqCst); }
            let stop = output.stop.load(Ordering::SeqCst);
            if stop != 0 && !cancelled && receipt.is_none() {
                let mut body = Encoder::new(); body.u32(stop)?;
                bound_cleanup(&mut stop_deadline, deadlines.cleanup_ms)?;
                super::send(channel, Kind::Cancel, &body.finish(), None, stop_deadline, || Ok(()))?;
                cancelled = true;
            }
            if now >= stop_deadline { return Err("Process supervisor cleanup deadline expired"); }
            match channel.receive()? {
                Received::Pending => super::poll(channel.raw(), libc::POLLIN, stop_deadline)?,
                Received::Eof => { eof = true; break; }
                Received::Packet(mut packet) => {
                    if let Some(descriptor) = packet.descriptor.take() {
                        let _ = inputs.inventory.receive(&packet.body, descriptor);
                        return Err("Process supervisor unexpectedly transferred a descriptor");
                    }
                    if receipt.is_some() { return Err("Process supervisor sent data after its receipt"); }
                    if packet.kind == Kind::Receipt {
                        receipt = Some(protocol::result(packet)?);
                        bound_cleanup(&mut stop_deadline, deadlines.cleanup_ms)?;
                        if receipt.as_ref().unwrap().0.cleanup_complete == 0 { return Err("Process supervisor retained unknown ownership"); }
                    } else if packet.kind == Kind::Output && packet.descriptor.is_none() {
                        let mut body = Decoder::new(&packet.body);
                        let sequence = body.u64()?; let stream = body.u32()?;
                        let length = body.u32()? as usize;
                        if sequence != last_output.checked_add(1).ok_or("Process output sequence overflow")?
                            || !(1..=2).contains(&stream) || length == 0 || length > io::CHUNK_BYTES {
                            return Err("Process supervisor output metadata is invalid");
                        }
                        let bytes = body.bytes(length)?; body.finish()?;
                        let (count, class) = output.deliver(stream, bytes);
                        last_output = sequence;
                        let mut acknowledgement = Encoder::new(); acknowledgement.u64(sequence)?;
                        acknowledgement.u64(count as u64)?; acknowledgement.u32(class as u32)?;
                        super::send(channel, Kind::OutputAck, &acknowledgement.finish(), None, stop_deadline, || Ok(()))?;
                    } else { return Err("Process supervisor response kind is invalid"); }
                }
            }
        }
        let (mut confirmed, unavailable) = receipt.ok_or("Process supervisor closed without a receipt")?;
        // Validate against owner-confirmed counters before any resource release.
        // The outer Process collector repeats no new proof after this boundary.
        output.complete(&mut confirmed, inputs.input.len());
        receipt = Some((confirmed, unavailable));
        if confirmed.cleanup_complete != 1 { return Err("Process supervisor cleanup is incomplete"); }
        let closed = channel.close(); local_cleanup &= closed.is_ok(); closed?;
        clean_exit = wait_child(pid, stop_deadline)?;
        if !clean_exit { return Err("Process supervisor exit was not clean"); }
        Ok(())
    })();
    let mut local_closed = spawn_cleanup == 1 && local_cleanup;
    if let Some(peer) = &mut peer { local_closed &= peer.close().is_ok(); }
    if let Some(channel) = &mut channel { local_closed &= channel.close().is_ok(); }
    if let Some((confirmed, _)) = receipt { result = confirmed; }
    if (attempt.is_ok() && eof && clean_exit && local_closed) || (pid == 0 && local_closed) {
        if clean_local(&mut inputs.inventory).is_ok() {
            result.cleanup_complete = 1;
            return Completion { result, unavailable: receipt.is_some_and(|receipt| receipt.1), released: reserved };
        }
        result.cleanup_failures |= 4;
    }
    result.stage = 6; result.class_id = 2; result.cleanup_complete = 0;
    if !local_closed { result.cleanup_failures |= 1; }
    // A spawned supervisor is never killed by an unqualified PID. Closing its
    // control channel requests cleanup; unknown ownership retains both copies.
    std::mem::forget(inputs.inventory);
    Completion { result, unavailable: false, released: 0 }
}
#[cfg(target_os = "macos")]
extern "C" { fn lo_process_supervisor_spawn(image: *const i8, control: i32, pid: *mut i32, cleanup_complete: *mut u32) -> i32; }
