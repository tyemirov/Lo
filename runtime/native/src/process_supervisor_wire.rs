use std::{io, mem::{size_of, size_of_val, zeroed}, os::fd::RawFd};

// One complete 4096-byte transport output chunk plus its closed metadata.
pub(super) const PAYLOAD: usize = 4112;
const HEADER: usize = 32;
const FRAME: usize = HEADER + PAYLOAD;
const RECEIVED_FDS: usize = 16;
#[repr(u16)]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(super) enum Kind {
    Hello = 1, Initialize = 2, Resource = 3, Configuration = 4, Command = 5,
    SetupDone = 6, Accepted = 7, Input = 9, Output = 10,
    OutputAck = 11, Cancel = 12, Receipt = 13, Launch = 14, ResourceAdopted = 15,
}
impl Kind {
    fn decode(value: u16) -> Result<Self, &'static str> {
        Ok(match value {
            1 => Self::Hello, 2 => Self::Initialize, 3 => Self::Resource,
            4 => Self::Configuration, 5 => Self::Command, 6 => Self::SetupDone,
            7 => Self::Accepted, 9 => Self::Input,
            10 => Self::Output, 11 => Self::OutputAck, 12 => Self::Cancel,
            13 => Self::Receipt, 14 => Self::Launch, 15 => Self::ResourceAdopted,
            _ => return Err("unknown Process supervisor message kind"),
        })
    }
}
// Runtime cleanup explicitly closes each descriptor and records errors before
// it publishes proof. Drop only handles an abandoned error path; it is not proof.
pub(super) struct Descriptor { raw: Option<RawFd> }
impl Descriptor {
    pub unsafe fn take(raw: RawFd) -> Self { Self { raw: Some(raw) } }
    pub fn raw(&self) -> RawFd { self.raw.unwrap() }
    pub fn duplicate(&self) -> Result<Self, &'static str> {
        let descriptor = unsafe { libc::fcntl(self.raw(), libc::F_DUPFD_CLOEXEC, 16) };
        if descriptor < 0 { Err("Process descriptor duplication failed") }
        else { Ok(unsafe { Self::take(descriptor) }) }
    }
    pub fn close(&mut self) -> Result<(), &'static str> {
        let Some(raw) = self.raw.take() else { return Ok(()); };
        if unsafe { libc::close(raw) } == 0 { Ok(()) }
        else { Err("Process descriptor release failed") }
    }
    pub fn nonblocking(&self) -> Result<(), &'static str> {
        let flags = unsafe { libc::fcntl(self.raw(), libc::F_GETFL) };
        if flags < 0 || unsafe { libc::fcntl(self.raw(), libc::F_SETFL, flags | libc::O_NONBLOCK) } < 0 {
            Err("Process descriptor cannot use bounded I/O")
        } else { Ok(()) }
    }
}
impl Drop for Descriptor {
    fn drop(&mut self) { let _ = self.close(); }
}
pub(super) struct Packet {
    pub kind: Kind,
    pub body: Vec<u8>,
    pub descriptor: Option<Descriptor>,
}
pub(super) enum Received { Pending, Packet(Packet), Eof }
pub(super) struct Channel {
    descriptor: Descriptor,
    generation: u64,
    sent_sequence: u64,
    received_sequence: u64,
    incoming: [u8; FRAME],
    incoming_size: usize,
    incoming_expected: usize,
    incoming_kind: Option<Kind>,
    incoming_descriptors: [Option<Descriptor>; RECEIVED_FDS],
    descriptor_count: usize,
    expected_descriptors: usize,
    outgoing: [u8; FRAME],
    outgoing_size: usize,
    outgoing_offset: usize,
    outgoing_descriptor: Option<RawFd>,
    failed: Option<&'static str>,
    eof: bool,
}
impl Channel {
    pub fn storage() -> u64 { (size_of::<Self>() + PAYLOAD) as u64 }
    pub fn new(descriptor: Descriptor) -> Result<Self, (Self, &'static str)> {
        let channel = Self { descriptor, generation: 0, sent_sequence: 0, received_sequence: 0,
            incoming: [0; FRAME], incoming_size: 0, incoming_expected: HEADER, incoming_kind: None,
            incoming_descriptors: std::array::from_fn(|_| None), descriptor_count: 0,
            expected_descriptors: 0, outgoing: [0; FRAME], outgoing_size: 0,
            outgoing_offset: 0, outgoing_descriptor: None, failed: None, eof: false };
        if let Err(reason) = channel.descriptor.nonblocking() { return Err((channel, reason)); }
        #[cfg(target_os = "macos")]
        unsafe {
            let enabled: libc::c_int = 1;
            if libc::setsockopt(channel.raw(), libc::SOL_SOCKET, libc::SO_NOSIGPIPE,
                (&enabled as *const libc::c_int).cast(), size_of::<libc::c_int>() as _) != 0 {
                return Err((channel, "Process supervisor channel cannot suppress SIGPIPE"));
            }
        }
        Ok(channel)
    }
    pub fn raw(&self) -> RawFd { self.descriptor.raw() }
    pub fn bind_generation(&mut self, generation: u64) -> Result<(), &'static str> {
        if generation == 0 || self.generation != 0 || self.sent_sequence != 1
            || self.received_sequence != 1 || self.pending_write() || self.incoming_size != 0 {
            return self.fail("Process supervisor generation cannot be rebound");
        }
        self.generation = generation;
        Ok(())
    }
    fn fail<T>(&mut self, reason: &'static str) -> Result<T, &'static str> {
        let first = *self.failed.get_or_insert(reason);
        Err(first)
    }
    pub fn pending_write(&self) -> bool { self.outgoing_offset != self.outgoing_size }
    pub fn queue(&mut self, kind: Kind, body: &[u8], descriptor: Option<RawFd>) -> Result<(), &'static str> {
        if let Some(reason) = self.failed { return Err(reason); }
        if self.pending_write() || body.len() > PAYLOAD || descriptor.is_some() != (kind == Kind::Resource)
            || (self.generation == 0) != (kind == Kind::Hello) {
            return self.fail("invalid Process supervisor outgoing frame");
        }
        let sequence = self.sent_sequence.checked_add(1).ok_or("Process supervisor sequence overflow")?;
        self.outgoing[..HEADER].fill(0);
        self.outgoing[..4].copy_from_slice(b"LOS1");
        self.outgoing[4..6].copy_from_slice(&(kind as u16).to_be_bytes());
        self.outgoing[8..12].copy_from_slice(&(body.len() as u32).to_be_bytes());
        self.outgoing[12..16].copy_from_slice(&u32::from(descriptor.is_some()).to_be_bytes());
        self.outgoing[16..24].copy_from_slice(&self.generation.to_be_bytes());
        self.outgoing[24..32].copy_from_slice(&sequence.to_be_bytes());
        self.outgoing[HEADER..HEADER + body.len()].copy_from_slice(body);
        self.outgoing_size = HEADER + body.len();
        self.outgoing_offset = 0;
        self.outgoing_descriptor = descriptor;
        self.sent_sequence = sequence;
        Ok(())
    }
    pub fn flush(&mut self) -> Result<bool, &'static str> {
        if let Some(reason) = self.failed { return Err(reason); }
        if !self.pending_write() { return Ok(true); }
        let result = unsafe {
            let mut vector = libc::iovec { iov_base: self.outgoing.as_mut_ptr().add(self.outgoing_offset).cast(),
                iov_len: self.outgoing_size - self.outgoing_offset };
            let mut message: libc::msghdr = zeroed();
            message.msg_iov = &mut vector;
            message.msg_iovlen = 1;
            let mut control = [0usize; 8];
            if let Some(descriptor) = self.outgoing_descriptor {
                message.msg_control = control.as_mut_ptr().cast();
                message.msg_controllen = libc::CMSG_SPACE(size_of::<RawFd>() as _) as _;
                let header = libc::CMSG_FIRSTHDR(&message);
                (*header).cmsg_level = libc::SOL_SOCKET;
                (*header).cmsg_type = libc::SCM_RIGHTS;
                (*header).cmsg_len = libc::CMSG_LEN(size_of::<RawFd>() as _) as _;
                std::ptr::write_unaligned(libc::CMSG_DATA(header).cast::<RawFd>(), descriptor);
            }
            #[cfg(target_os = "macos")]
            let flags = 0;
            #[cfg(not(target_os = "macos"))]
            let flags = libc::MSG_NOSIGNAL;
            libc::sendmsg(self.descriptor.raw(), &message, flags)
        };
        if result < 0 {
            return match io::Error::last_os_error().kind() {
                io::ErrorKind::Interrupted | io::ErrorKind::WouldBlock => Ok(false),
                _ => self.fail("Process supervisor send failed"),
            };
        }
        if result == 0 { return self.fail("Process supervisor send ended early"); }
        self.outgoing_offset += result as usize;
        self.outgoing_descriptor = None; // Rights were sent with the first byte.
        Ok(!self.pending_write())
    }
    pub fn receive(&mut self) -> Result<Received, &'static str> {
        if let Some(reason) = self.failed { return Err(reason); }
        if self.eof { return Ok(Received::Eof); }
        let offset = self.incoming_size;
        let mut vector = libc::iovec { iov_base: unsafe { self.incoming.as_mut_ptr().add(offset).cast() },
            iov_len: self.incoming_expected - offset };
        let mut control = [0usize; 8];
        let mut message: libc::msghdr = unsafe { zeroed() };
        message.msg_iov = &mut vector;
        message.msg_iovlen = 1;
        message.msg_control = control.as_mut_ptr().cast();
        message.msg_controllen = size_of_val(&control) as _;
        let result = unsafe { libc::recvmsg(self.descriptor.raw(), &mut message, 0) };
        if result < 0 {
            return match io::Error::last_os_error().kind() {
                io::ErrorKind::Interrupted | io::ErrorKind::WouldBlock => Ok(Received::Pending),
                _ => self.fail("Process supervisor receive failed"),
            };
        }
        let mut invalid = false;
        unsafe {
            let mut header = libc::CMSG_FIRSTHDR(&message);
            while !header.is_null() {
                let length = (*header).cmsg_len as usize;
                let minimum = libc::CMSG_LEN(0) as usize;
                if length < minimum || length > message.msg_controllen as usize {
                    invalid = true; break;
                }
                if (*header).cmsg_level != libc::SOL_SOCKET || (*header).cmsg_type != libc::SCM_RIGHTS {
                    invalid = true;
                } else {
                    let bytes = length - minimum;
                    if bytes % size_of::<RawFd>() != 0 { invalid = true; }
                    for index in 0..bytes / size_of::<RawFd>() {
                        let raw = std::ptr::read_unaligned(libc::CMSG_DATA(header).add(index * size_of::<RawFd>()).cast());
                        if self.descriptor_count == RECEIVED_FDS {
                            // The fixed control buffer cannot contain this many
                            // rights. Retain this as a protocol failure anyway.
                            libc::close(raw);
                            invalid = true;
                        } else {
                            self.incoming_descriptors[self.descriptor_count] = Some(Descriptor::take(raw));
                            self.descriptor_count += 1;
                            if libc::fcntl(raw, libc::F_SETFD, libc::FD_CLOEXEC) < 0 { invalid = true; }
                        }
                    }
                }
                header = libc::CMSG_NXTHDR(&message, header);
            }
        }
        if message.msg_flags & (libc::MSG_CTRUNC | libc::MSG_TRUNC) != 0
            || invalid || self.descriptor_count > 1
            || (offset != 0 && message.msg_controllen != 0) {
            return self.fail("Process supervisor ancillary data is invalid");
        }
        if result == 0 {
            if self.incoming_size != 0 || self.descriptor_count != 0 {
                return self.fail("Process supervisor channel ended in a partial frame");
            }
            self.eof = true;
            return Ok(Received::Eof);
        }
        self.incoming_size += result as usize;
        if self.incoming_size < self.incoming_expected { return Ok(Received::Pending); }
        if self.incoming_kind.is_none() {
            let header = &self.incoming[..HEADER];
            if &header[..4] != b"LOS1" || header[6..8] != [0, 0] {
                return self.fail("Process supervisor frame header is invalid");
            }
            let kind = match Kind::decode(u16::from_be_bytes(header[4..6].try_into().unwrap())) {
                Ok(kind) => kind,
                Err(reason) => return self.fail(reason),
            };
            let length = u32::from_be_bytes(header[8..12].try_into().unwrap()) as usize;
            let descriptors = u32::from_be_bytes(header[12..16].try_into().unwrap()) as usize;
            let generation = u64::from_be_bytes(header[16..24].try_into().unwrap());
            let sequence = u64::from_be_bytes(header[24..32].try_into().unwrap());
            if length > PAYLOAD || descriptors != usize::from(kind == Kind::Resource)
                || self.descriptor_count != descriptors || generation != self.generation
                || (self.generation == 0) != (kind == Kind::Hello)
                || self.received_sequence.checked_add(1) != Some(sequence) {
                return self.fail("Process supervisor frame has invalid bounds, sequence, or generation");
            }
            self.received_sequence = sequence;
            self.incoming_kind = Some(kind);
            self.incoming_expected = HEADER + length;
            self.expected_descriptors = descriptors;
            if length != 0 { return Ok(Received::Pending); }
        }
        let kind = self.incoming_kind.take().unwrap();
        let mut body = Vec::new();
        body.try_reserve_exact(self.incoming_expected - HEADER).map_err(|_| "Process supervisor frame storage is unavailable")?;
        body.extend_from_slice(&self.incoming[HEADER..self.incoming_expected]);
        let descriptor = if self.expected_descriptors == 1 { self.incoming_descriptors[0].take() } else { None };
        self.incoming_size = 0;
        self.incoming_expected = HEADER;
        self.expected_descriptors = 0;
        self.descriptor_count = 0;
        Ok(Received::Packet(Packet { kind, body, descriptor }))
    }
    pub fn close(&mut self) -> Result<(), &'static str> {
        let mut result = Ok(());
        for descriptor in self.incoming_descriptors.iter_mut().flatten() {
            if let Err(reason) = descriptor.close() { result = Err(reason); }
        }
        if let Err(reason) = self.descriptor.close() { result = Err(reason); }
        result
    }
}

pub(super) struct Encoder { bytes: Vec<u8> }
impl Encoder {
    pub fn new() -> Self { Self { bytes: Vec::with_capacity(PAYLOAD) } }
    pub fn bytes(&mut self, value: &[u8]) -> Result<(), &'static str> {
        if value.len() > PAYLOAD - self.bytes.len() { return Err("Process supervisor payload exceeds its bound"); }
        self.bytes.extend_from_slice(value); Ok(())
    }
    pub fn u32(&mut self, value: u32) -> Result<(), &'static str> { self.bytes(&value.to_be_bytes()) }
    pub fn u64(&mut self, value: u64) -> Result<(), &'static str> { self.bytes(&value.to_be_bytes()) }
    pub fn text(&mut self, value: &str) -> Result<(), &'static str> {
        if value.as_bytes().contains(&0) || value.len() > 1024 { return Err("Process supervisor path exceeds its bound"); }
        self.u32(value.len() as u32)?; self.bytes(value.as_bytes())
    }
    pub fn finish(self) -> Vec<u8> { self.bytes }
}
pub(super) struct Decoder<'a> { bytes: &'a [u8], offset: usize }
impl<'a> Decoder<'a> {
    pub fn new(bytes: &'a [u8]) -> Self { Self { bytes, offset: 0 } }
    pub fn bytes(&mut self, size: usize) -> Result<&'a [u8], &'static str> {
        if size > self.bytes.len() - self.offset { return Err("Process supervisor payload is truncated"); }
        let value = &self.bytes[self.offset..self.offset + size]; self.offset += size; Ok(value)
    }
    pub fn u32(&mut self) -> Result<u32, &'static str> { Ok(u32::from_be_bytes(self.bytes(4)?.try_into().unwrap())) }
    pub fn u64(&mut self) -> Result<u64, &'static str> { Ok(u64::from_be_bytes(self.bytes(8)?.try_into().unwrap())) }
    pub fn text(&mut self) -> Result<&'a str, &'static str> {
        let size = self.u32()? as usize;
        if size > 1024 { return Err("Process supervisor path exceeds its bound"); }
        let bytes = self.bytes(size)?;
        if bytes.contains(&0) { return Err("Process supervisor path contains NUL"); }
        std::str::from_utf8(bytes).map_err(|_| "Process supervisor path is not UTF-8")
    }
    pub fn finish(self) -> Result<(), &'static str> {
        if self.offset == self.bytes.len() { Ok(()) } else { Err("Process supervisor payload has trailing fields") }
    }
}
