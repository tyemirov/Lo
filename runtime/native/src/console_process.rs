use super::WriteFailure;
use crate::context::{host, Fault};
use std::{
    fs::File,
    io::{Read, Write},
    os::{
        fd::{AsRawFd, FromRawFd},
        unix::process::CommandExt,
    },
    process::{Command, Stdio},
    time::Duration,
};
pub struct Image {
    pub paths: Vec<String>,
    pub registry: crate::process::Processes,
    path: String,
}
impl Image {
    pub fn bind() -> Result<Self, String> {
        let path = std::env::current_exe().map_err(|e| e.to_string())?;
        let resolved = path.canonicalize().map_err(|e| e.to_string())?;
        let path = path
            .to_str()
            .ok_or("Console image path requires UTF-8")?
            .to_owned();
        let resolved = resolved
            .to_str()
            .ok_or("Console image path requires UTF-8")?
            .to_owned();
        let mut paths = vec![path.clone()];
        if resolved != path {
            paths.push(resolved);
        }
        let registry = crate::process::protection(&paths)?;
        Ok(Self {
            paths,
            registry,
            path,
        })
    }
    pub fn write(
        &self,
        destination: u32,
        bytes: &[u8],
        check: &mut impl FnMut() -> Result<(), Fault>,
    ) -> Result<(), WriteFailure> {
        if bytes.len() > 2097152 {
            return Err(WriteFailure::Terminal(crate::context::limit(
                "Console encoded record bytes",
            )));
        }
        for path in &self.paths {
            self.registry
                .validate_image(path, &mut *check)
                .map_err(WriteFailure::Terminal)?;
        }
        let result = (|| -> Result<Result<(), WriteFailure>, Fault> {
            let spawn_ownership = crate::spawn_ownership::acquire(&mut *check)?;
            let (request_read, mut request_write) = pipe()?;
            let (mut ack_read, ack_write) = pipe()?;
            let (live_read, _live_write) = pipe()?;
            let ack_child = duplicate(&ack_write)?;
            let live_child = duplicate(&live_read)?;
            let ack_fd = ack_child.as_raw_fd();
            let live_fd = live_child.as_raw_fd();
            let mut command = Command::new(&self.path);
            command
                .arg("--lo-console-writer")
                .stdin(Stdio::from(request_read))
                .stdout(Stdio::inherit())
                .stderr(Stdio::inherit());
            unsafe {
                command.pre_exec(move || {
                    if libc::dup2(ack_fd, 3) < 0 || libc::dup2(live_fd, 4) < 0 {
                        return Err(std::io::Error::last_os_error());
                    }
                    Ok(())
                });
            }
            let mut child = command
                .spawn()
                .map_err(|_| host("Console writer spawn failed"))?;
            drop(ack_child);
            drop(live_child);
            drop(ack_write);
            drop(live_read);
            drop(spawn_ownership);
            let operation = (|| -> Result<Result<(), WriteFailure>, Fault> {
                nonblocking(&request_write)?;
                nonblocking(&ack_read)?;
                let mut request = Vec::with_capacity(bytes.len() + 9);
                request.extend_from_slice(b"LCW1");
                request.push(destination as u8);
                request.extend_from_slice(&(bytes.len() as u32).to_be_bytes());
                request.extend_from_slice(bytes);
                let mut sent = 0;
                while sent < request.len() {
                    check()?;
                    match request_write.write(&request[sent..]) {
                        Ok(0) => return Err(host("Console request closed")),
                        Ok(n) => sent += n,
                        Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                            std::thread::sleep(Duration::from_millis(1))
                        }
                        Err(e) if e.kind() == std::io::ErrorKind::Interrupted => (),
                        Err(_) => return Err(host("Console request failed")),
                    }
                }
                drop(request_write);
                let mut confirmed = 0u64;
                let mut ack = [0u8; 10];
                let mut received = 0;
                loop {
                    check()?;
                    match ack_read.read(&mut ack[received..]) {
                        Ok(0) => return Err(host("Console acknowledgment ended early")),
                        Ok(n) => received += n,
                        Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                            std::thread::sleep(Duration::from_millis(1));
                            continue;
                        }
                        Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
                        Err(_) => return Err(host("Console acknowledgment failed")),
                    }
                    if received < 10 {
                        continue;
                    }
                    received = 0;
                    let count = u64::from_be_bytes(ack[1..9].try_into().unwrap());
                    if count < confirmed || count > bytes.len() as u64 {
                        return Err(host("Console acknowledgment count invalid"));
                    }
                    match (ack[0], ack[9]) {
                        (1, 0) if count > confirmed => confirmed = count,
                        (2, 0) if count == bytes.len() as u64 => break Ok(Ok(())),
                        (3, 1 | 2) => {
                            break Ok(Err(WriteFailure::Recoverable(count as usize, "write")))
                        }
                        _ => return Err(host("Console acknowledgment invalid")),
                    }
                }
            })();
            let operation = match operation {
                Ok(result) => loop {
                    if let Err(fault) = check() {
                        break Err(fault);
                    }
                    match child.try_wait() {
                        Ok(Some(status)) => {
                            break if status.success() {
                                Ok(result)
                            } else {
                                Err(host("Console writer exited unsuccessfully"))
                            }
                        }
                        Ok(None) => std::thread::sleep(Duration::from_millis(1)),
                        Err(_) => break Err(host("Console writer join failed")),
                    }
                },
                Err(fault) => Err(fault),
            };
            if operation.is_err() {
                let _ = child.kill();
                child
                    .wait()
                    .map_err(|_| host("Console writer cleanup failed"))?;
            }
            operation
        })();
        result.map_err(WriteFailure::Terminal)?
    }
}
fn pipe() -> Result<(File, File), Fault> {
    let mut descriptors = [0; 2];
    if unsafe { libc::pipe(descriptors.as_mut_ptr()) } != 0 {
        return Err(host("Console pipe creation failed"));
    }
    for fd in descriptors {
        if unsafe { libc::fcntl(fd, libc::F_SETFD, libc::FD_CLOEXEC) } < 0 {
            unsafe {
                libc::close(descriptors[0]);
                libc::close(descriptors[1]);
            }
            return Err(host("Console pipe ownership failed"));
        }
    }
    Ok(unsafe {
        (
            File::from_raw_fd(descriptors[0]),
            File::from_raw_fd(descriptors[1]),
        )
    })
}
fn duplicate(file: &File) -> Result<File, Fault> {
    let fd = unsafe { libc::fcntl(file.as_raw_fd(), libc::F_DUPFD_CLOEXEC, 5) };
    if fd < 0 {
        return Err(host("Console child descriptor binding failed"));
    }
    Ok(unsafe { File::from_raw_fd(fd) })
}
fn nonblocking(file: &File) -> Result<(), Fault> {
    let fd = file.as_raw_fd();
    let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
    if flags < 0 || unsafe { libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) } < 0 {
        return Err(host("Console owned pipe configuration failed"));
    }
    Ok(())
}
pub fn worker() -> i32 {
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_IGN);
    }
    if std::thread::Builder::new()
        .spawn(|| {
            let mut byte = [0u8; 1];
            loop {
                let n = unsafe { libc::read(4, byte.as_mut_ptr().cast(), 1) };
                if n < 0
                    && std::io::Error::last_os_error().kind() == std::io::ErrorKind::Interrupted
                {
                    continue;
                }
                unsafe { libc::_exit(125) }
            }
        })
        .is_err()
    {
        return 125;
    }
    let mut input = std::io::stdin().lock();
    let mut header = [0u8; 9];
    if input.read_exact(&mut header).is_err()
        || &header[..4] != b"LCW1"
        || ![1, 2].contains(&header[4])
    {
        return 125;
    }
    let length = u32::from_be_bytes(header[5..9].try_into().unwrap()) as usize;
    if length > 2097152 {
        return 125;
    }
    let mut body = vec![0; length];
    if input.read_exact(&mut body).is_err() {
        return 125;
    }
    let mut end = [0u8; 1];
    if !matches!(input.read(&mut end), Ok(0)) {
        return 125;
    }
    let mut confirmed = 0;
    while confirmed < length {
        let count = unsafe {
            libc::write(
                header[4] as i32,
                body[confirmed..].as_ptr().cast(),
                length - confirmed,
            )
        };
        if count < 0 {
            let error = std::io::Error::last_os_error();
            if error.kind() == std::io::ErrorKind::Interrupted {
                continue;
            }
            return if ack(
                3,
                confirmed,
                if error.raw_os_error() == Some(libc::EPIPE) {
                    1
                } else {
                    2
                },
            ) {
                0
            } else {
                125
            };
        }
        if count == 0 {
            return if ack(3, confirmed, 2) { 0 } else { 125 };
        }
        confirmed += count as usize;
        if !ack(1, confirmed, 0) {
            return 125;
        }
    }
    if ack(2, confirmed, 0) {
        0
    } else {
        125
    }
}
fn ack(kind: u8, count: usize, class: u8) -> bool {
    let mut frame = [0u8; 10];
    frame[0] = kind;
    frame[1..9].copy_from_slice(&(count as u64).to_be_bytes());
    frame[9] = class;
    let mut offset = 0;
    while offset < frame.len() {
        let n = unsafe { libc::write(3, frame[offset..].as_ptr().cast(), frame.len() - offset) };
        if n < 0 && std::io::Error::last_os_error().kind() == std::io::ErrorKind::Interrupted {
            continue;
        }
        if n <= 0 {
            return false;
        }
        offset += n as usize;
    }
    true
}
