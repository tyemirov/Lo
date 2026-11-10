use super::{super::{io, resources::Inventory}, protocol, transport, Channel, Deadlines, Decoder, Descriptor,
    Encoder, HostIdentity, Kind, OpenOwner, Owner, Purpose, Received};
use std::{sync::mpsc::{SyncSender, TryRecvError}, time::Duration};

struct Child {
    channel: Channel,
    inventory: Inventory,
    owner: Option<Owner>,
    transport: Option<transport::Transport>,
    connected: bool,
    deadlines: Deadlines,
    cleanup_deadline: Option<u64>,
    stop: u32,
    output_sequence: u64,
    pending_output: Option<(u64, usize, SyncSender<(usize, i32)>)>,
    pending_ready: Option<transport::Ready>,
    result: Option<io::NativeResult>,
    ready_resolved: bool,
    launch_token: Option<super::Token>,
}
impl Child {
    fn stop(&mut self, class: u32) {
        if self.stop == 0 { self.stop = class; }
        self.cleanup_deadline.get_or_insert_with(|| self.deadlines.cleanup().unwrap_or(0));
        if let Some(owner) = &self.owner { owner.controller.freeze(); }
        if let Some(transport) = &self.transport { transport.stop(self.stop); }
        if !self.connected {
            if let Some((_, _, reply)) = self.pending_output.take() { let _ = reply.send((0, self.stop as i32)); }
        }
        if let Some(ready) = self.pending_ready.take() { let _ = ready.response.send(self.stop as i32); }
    }
    fn deadline(&self) -> u64 { self.cleanup_deadline.unwrap_or(self.deadlines.command) }
    fn check(&self) -> Result<(), crate::context::Fault> {
        if super::now().unwrap_or(u64::MAX) >= self.deadline() {
            Err(crate::context::host("Process ownership deadline expired"))
        } else { Ok(()) }
    }
    fn service(&self) -> Result<(), &'static str> {
        let owner = self.owner.as_ref().unwrap();
        owner.service(self.deadline(), |image| self.inventory.validate_image(&image.path, image.file, || self.check()).is_ok())
    }
    fn controls(&mut self) -> Result<(), &'static str> {
        if !self.connected { return Ok(()); }
        if super::now()? >= self.deadline() { return Err("Process control deadline expired"); }
        if self.channel.pending_write() { self.channel.flush()?; }
        loop {
            match self.channel.receive()? {
                Received::Pending => return Ok(()),
                Received::Eof => { self.connected = false; self.stop(1); return Ok(()); }
                Received::Packet(mut packet) => {
                    if let Some(descriptor) = packet.descriptor.take() {
                        let _ = self.inventory.receive(&packet.body, descriptor);
                        return Err("Process control unexpectedly transferred a descriptor");
                    }
                    let mut body = Decoder::new(&packet.body);
                    match packet.kind {
                        Kind::Cancel => {
                            let class = body.u32()?; body.finish()?;
                            if !(1..=5).contains(&class) { return Err("Process cancellation class is invalid"); }
                            self.stop(class);
                        }
                        Kind::OutputAck => {
                            let sequence = body.u64()?; let count = body.u64()?; let class = body.u32()?; body.finish()?;
                            let Some((expected, length, reply)) = self.pending_output.take() else {
                                return Err("Process output acknowledgment has no pending chunk");
                            };
                            if sequence != expected || count > length as u64 || class > 5 || (class == 0 && count != length as u64) {
                                let _ = reply.send((0, 2)); return Err("Process output acknowledgment fields differ");
                            }
                            let _ = reply.send((count as usize, class as i32));
                            if class != 0 { self.stop(class); }
                        }
                        _ => return Err("Process control message is not valid during execution"),
                    }
                    // One bounded message per service turn keeps lifecycle and
                    // deadline work active even when the parent sends a flood.
                    return Ok(());
                }
            }
        }
    }
    fn marker(&mut self, purpose: Purpose) -> Result<(), &'static str> {
        let request = self.owner.as_mut().unwrap().begin_marker(purpose)?;
        loop {
            self.service()?;
            self.controls()?;
            if self.owner.as_mut().unwrap().finish_marker(request)? { return Ok(()); }
            if super::now()? >= self.deadline() { return Err("Process marker deadline expired"); }
            self.owner.as_ref().unwrap().controller.wait(Duration::from_millis(1));
        }
    }
    fn startup(&mut self, setup: &protocol::Setup) -> Result<transport::Inputs, &'static str> {
        let mut configuration = protocol::buffer(setup.configuration)?;
        let mut frame = protocol::buffer(setup.command)?;
        let mut input = protocol::buffer(setup.input)?;
        for index in 0..setup.resources {
            let mut packet = super::receive(&mut self.channel, self.deadlines.command, || Ok(()))?;
            if packet.kind != Kind::Resource { return Err("Process resource transfer ended early"); }
            let descriptor = packet.descriptor.take().ok_or("Process resource transfer omitted a descriptor")?;
            self.inventory.receive(&packet.body, descriptor)?;
            // One rights frame is outstanding at a time. Adoption owns this
            // descriptor; full inventory validation still precedes admission.
            let mut adopted = Encoder::new(); adopted.u64(index as u64)?;
            super::send(&mut self.channel, Kind::ResourceAdopted, &adopted.finish(), None,
                self.deadlines.command, || Ok(()))?;
        }
        for (kind, target, size) in [(Kind::Configuration, &mut configuration, setup.configuration),
            (Kind::Command, &mut frame, setup.command), (Kind::Input, &mut input, setup.input)] {
            while target.len() != size {
                let mut packet = super::receive(&mut self.channel, self.deadlines.command, || Ok(()))?;
                if let Some(descriptor) = packet.descriptor.take() {
                    let _ = self.inventory.receive(&packet.body, descriptor);
                    return Err("Process input unexpectedly transferred a descriptor");
                }
                if packet.kind != kind { return Err("Process input transfer kind differs"); }
                protocol::chunk(&packet.body, target, size)?;
            }
        }
        let mut packet = super::receive(&mut self.channel, self.deadlines.command, || Ok(()))?;
        if let Some(descriptor) = packet.descriptor.take() {
            let _ = self.inventory.receive(&packet.body, descriptor);
            return Err("Process setup boundary unexpectedly transferred a descriptor");
        }
        protocol::empty(packet, Kind::SetupDone)?;
        self.inventory.validate(|| self.check()).map_err(|_| "Process retained resources failed admission")?;
        Ok(transport::Inputs { launcher: setup.launcher.clone(), embedded: setup.embedded,
            directory: setup.directory.clone(), configuration, frame, input, limits: setup.limits,
            forward: setup.forward, deadlines: self.deadlines })
    }
    fn run(mut self, generation: u64, setup: protocol::Setup) -> i32 {
        let inputs = match self.startup(&setup) {
            Ok(inputs) => inputs,
            Err(_) => return self.finish(io::NativeResult { stage: 1, class_id: 2, cleanup_complete: 1, ..Default::default() }, false),
        };
        match Owner::open(generation, setup.descendants, self.inventory.images()) {
            Ok(OpenOwner::Ready(owner)) => self.owner = Some(owner),
            Ok(OpenOwner::Unavailable) => return self.finish(io::NativeResult { stage: 1, class_id: 2, cleanup_complete: 1, ..Default::default() }, true),
            Ok(OpenOwner::Failed(owner)) => { self.owner = Some(owner); self.quarantine(); }
            Err(_) => self.quarantine(),
        }
        if self.marker(Purpose::Admission).is_err() { self.quarantine(); }
        if self.stop != 0 { return self.finish_without_launch(); }
        if super::send(&mut self.channel, Kind::Accepted, &[], None, self.deadlines.command, || Ok(())).is_err() { self.stop(1); return self.finish_without_launch(); }
        match super::receive(&mut self.channel, self.deadlines.command, || Ok(())) {
            Ok(packet) if packet.kind == Kind::Launch => {
                if protocol::empty(packet, Kind::Launch).is_err() { self.stop(2); return self.finish_without_launch(); }
            }
            Ok(packet) if packet.kind == Kind::Cancel => {
                let mut body = Decoder::new(&packet.body);
                let class = body.u32().ok().filter(|class| (1..=5).contains(class)).unwrap_or(2);
                self.stop(class); return self.finish_without_launch();
            }
            Ok(mut packet) => {
                if let Some(descriptor) = packet.descriptor.take() { let _ = self.inventory.receive(&packet.body, descriptor); }
                self.stop(2); return self.finish_without_launch();
            }
            Err(_) => { self.connected = false; self.stop(1); return self.finish_without_launch(); }
        }
        if self.inventory.validate(|| self.check()).is_err() { self.stop(4); return self.finish_without_launch(); }
        match transport::Transport::spawn(self.owner.as_ref().unwrap().controller.clone(), inputs) {
            Ok(transport) => self.transport = Some(transport),
            Err(_) => { self.stop(2); return self.finish_without_launch(); }
        }
        loop {
            if self.owner.as_ref().unwrap().controller.failure().is_some() { self.stop(2); }
            if self.owner.as_ref().unwrap().controller.denied_execution() { self.stop(4); }
            if super::now().unwrap_or(u64::MAX) >= self.deadlines.command && self.stop == 0 { self.stop(5); }
            if self.service().is_err() { self.stop(2); }
            if self.controls().is_err() { self.connected = false; self.stop(2); }
            if self.pending_ready.is_none() {
                match self.transport.as_ref().unwrap().ready.try_recv() {
                    Ok(ready) => { self.launch_token = Some(ready.token); self.pending_ready = Some(ready); },
                    Err(TryRecvError::Empty | TryRecvError::Disconnected) => (),
                }
            }
            if let Some(ready) = &self.pending_ready {
                let controller = &self.owner.as_ref().unwrap().controller;
                match controller.launch_evidence(ready.token) {
                    Ok(true) => {
                        let permitted = self.stop == 0 && self.inventory.validate(|| self.check()).is_ok();
                        if controller.resolve_launch(ready.token).is_err() { self.stop(2); }
                        else {
                            self.ready_resolved = true;
                            let ready = self.pending_ready.take().unwrap();
                            let _ = ready.response.send(if permitted { 0 } else { self.stop.max(4) as i32 });
                            if !permitted { self.stop(4); }
                        }
                    }
                    Ok(false) => (),
                    Err(_) => self.stop(2),
                }
            }
            if self.pending_output.is_none() && (!self.connected || !self.channel.pending_write()) {
                match self.transport.as_ref().unwrap().output.try_recv() {
                    Ok(io::Event::Output { stream, bytes, reply }) => {
                        if self.stop != 0 || !self.connected { let _ = reply.send((0, self.stop.max(1) as i32)); }
                        else {
                            let queued = (|| {
                                self.output_sequence = self.output_sequence.checked_add(1).ok_or("Process output sequence overflow")?;
                                let mut body = Encoder::new(); body.u64(self.output_sequence)?; body.u32(stream)?;
                                body.u32(bytes.len() as u32)?; body.bytes(&bytes)?;
                                self.channel.queue(Kind::Output, &body.finish(), None)
                            })();
                            if queued.is_err() { let _ = reply.send((0, 2)); self.stop(2); }
                            else { self.pending_output = Some((self.output_sequence, bytes.len(), reply)); }
                        }
                    }
                    Ok(io::Event::Complete(result)) => {
                        self.result = Some(result);
                        if self.stop == 0 { self.cleanup_deadline.get_or_insert_with(|| self.deadlines.cleanup().unwrap_or(0)); }
                        self.owner.as_ref().unwrap().controller.freeze();
                    }
                    Ok(_) => { self.owner.as_ref().unwrap().controller.damage("Process transport sent an unexpected event"); self.stop(2); }
                    Err(TryRecvError::Empty | TryRecvError::Disconnected) => (),
                }
            }
            if let Some(result) = self.result {
                if result.cleanup_complete == 0 { self.quarantine(); }
                if self.transport.as_ref().unwrap().finished() && self.owner.as_ref().unwrap().controller.empty() {
                    if self.transport.as_mut().unwrap().join().is_err() { self.quarantine(); }
                    if !self.ready_resolved {
                        let controller = &self.owner.as_ref().unwrap().controller;
                        if let Some(token) = self.launch_token {
                            match controller.launch_evidence(token) {
                                Ok(true) => { if controller.resolve_launch(token).is_err() { self.quarantine(); } }
                                Ok(false) => (),
                                Err(_) => self.quarantine(),
                            }
                        } else if controller.launch_was_started() {
                            match controller.failed_launch_evidence(self.transport.as_ref().unwrap().child_pid()) {
                                Ok(true) => { if self.marker(Purpose::FailedLaunch).is_err() { self.quarantine(); } }
                                Ok(false) => { /* Lifecycle delivery can follow the failed spawn receipt. */ }
                                Err(_) => self.quarantine(),
                            }
                        } else if controller.resolve_without_launch().is_err() { self.quarantine(); }
                        if !self.owner.as_ref().unwrap().controller.launch_resolved() {
                            if super::now().unwrap_or(u64::MAX) >= self.deadline() { self.quarantine(); }
                            self.owner.as_ref().unwrap().controller.wait(Duration::from_millis(1)); continue;
                        }
                    }
                    if self.marker(Purpose::Completion).is_err() { self.quarantine(); }
                    if self.owner.as_mut().unwrap().close(self.transport.as_ref().unwrap().child_reaped(), true, true).is_err() { self.quarantine(); }
                    let mut result = result;
                    if self.stop != 0 && (result.stage == 0 || result.class_id == 0) { result.stage = 7; result.class_id = self.stop as i32; }
                    return self.finish(result, false);
                }
            }
            if super::now().unwrap_or(u64::MAX) >= self.deadline() { self.quarantine(); }
            self.owner.as_ref().unwrap().controller.wait(Duration::from_millis(1));
        }
    }
    fn finish_without_launch(mut self) -> i32 {
        self.owner.as_ref().unwrap().controller.freeze();
        if self.owner.as_ref().unwrap().controller.resolve_without_launch().is_err()
            || self.marker(Purpose::Completion).is_err()
            || self.owner.as_mut().unwrap().close(true, true, true).is_err() { self.quarantine(); }
        let class_id = if self.stop == 0 { 2 } else { self.stop as i32 };
        self.finish(io::NativeResult { stage: 1, class_id, cleanup_complete: 1, ..Default::default() }, false)
    }
    fn finish(mut self, result: io::NativeResult, unavailable: bool) -> i32 {
        if self.inventory.close().is_err() { self.result = Some(result); self.quarantine(); }
        let deadline = self.deadlines.cleanup().unwrap_or(0);
        let sent = !self.connected || protocol::result_body(&result, unavailable).and_then(|body|
            super::send(&mut self.channel, Kind::Receipt, &body, None, deadline, || Ok(()))).is_ok();
        let closed = self.channel.close().is_ok();
        if sent && closed { 0 } else { 1 }
    }
    fn quarantine(mut self) -> ! {
        let connected = self.connected;
        self.connected = false;
        self.stop(2);
        let mut result = self.result.unwrap_or(io::NativeResult { stage: 6, class_id: 2, ..Default::default() });
        result.stage = 6; result.class_id = 2; result.cleanup_complete = 0;
        if connected && !self.channel.pending_write() {
            if let Ok(body) = protocol::result_body(&result, false) {
                let deadline = super::now().unwrap_or(0).saturating_add(5_000_000);
                let _ = super::send(&mut self.channel, Kind::Receipt, &body, None, deadline, || Ok(()));
            }
        }
        let _ = self.channel.close(); self.connected = false;
        loop {
            if let Some(owner) = &self.owner {
                let deadline = super::now().unwrap_or(0).saturating_add(5_000_000);
                let _ = owner.service(deadline, |_| false);
            }
            if let Some(transport) = &self.transport {
                while let Ok(ready) = transport.ready.try_recv() { let _ = ready.response.send(2); }
                while let Ok(event) = transport.output.try_recv() {
                    if let io::Event::Output { reply, .. } = event { let _ = reply.send((0, 2)); }
                }
            }
            // Inventory, provider, marker, and transport remain owned here.
            std::thread::sleep(Duration::from_millis(5));
        }
    }
}
pub(super) fn main() -> i32 {
    #[cfg(not(target_os = "macos"))]
    { return 78; }
    #[cfg(target_os = "macos")]
    {
        unsafe { libc::signal(libc::SIGPIPE, libc::SIG_IGN); }
        let mut channel = match Channel::new(unsafe { Descriptor::take(3) }) {
            Ok(channel) => channel,
            Err((mut channel, _)) => { let _ = channel.close(); return 78; }
        };
        // Bootstrap has no command authority. Its finite deadline precedes the
        // activation's original absolute deadline carried by Initialize.
        let bootstrap = super::now().unwrap_or(0).saturating_add(5_000_000_000);
        let started = (|| {
            let identity = HostIdentity::current()?;
            super::send(&mut channel, Kind::Hello, &identity.hello(0)?, None, bootstrap, || Ok(()))?;
            let (parent, generation) = HostIdentity::from_hello(super::receive(&mut channel, bootstrap, || Ok(()))?)?;
            let pid = unsafe { libc::getppid() };
            if generation == 0 { return Err("Process parent generation is zero"); }
            identity.verify_peer(&channel, parent, pid)?;
            channel.bind_generation(generation)?;
            let setup = protocol::Setup::decode(super::receive(&mut channel, bootstrap, || Ok(()))?)?;
            let inventory = protocol::inventory(&setup)?;
            Ok((generation, setup, inventory))
        })();
        let Ok((generation, setup, inventory)) = started else { let _ = channel.close(); return 78; };
        let child = Child { channel, inventory, owner: None, transport: None, connected: true,
            deadlines: setup.deadlines, cleanup_deadline: None, stop: 0, output_sequence: 0,
            pending_output: None, pending_ready: None, result: None, ready_resolved: false, launch_token: None };
        child.run(generation, setup)
    }
}
