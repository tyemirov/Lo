use super::*;
use std::{
    collections::{BTreeMap, BTreeSet},
    io::{self, Read, Write},
    net::TcpStream,
    time::{Duration, Instant},
};
#[derive(Clone, Default)]
pub struct Policy {
    grants: BTreeMap<(String, String), BTreeSet<IpAddr>>,
}
impl Policy {
    pub fn new(grants: &[String]) -> Result<Self, String> {
        let mut result = Self::default();
        for (index, grant) in grants.iter().enumerate() {
            let invalid =
                || format!("HTTP grant {index} requires METHOD,ORIGIN,ADDRESS without whitespace");
            let parts = grant.split(',').collect::<Vec<_>>();
            if parts.len() != 3 || grant.chars().any(char::is_whitespace) || !token(parts[0]) {
                return Err(invalid());
            }
            let url = Url::parse(parts[1]).map_err(|_| invalid())?;
            if url.path != "/" {
                return Err(invalid());
            }
            let ip = parts[2].parse::<IpAddr>().map_err(|_| invalid())?;
            result
                .grants
                .entry((parts[0].into(), url.origin))
                .or_default()
                .insert(unmap(ip));
        }
        Ok(result)
    }
}
struct Progress {
    stage: &'static str,
    written: bool,
    possible: bool,
}
impl Progress {
    fn failure(&self, cause: Fault) -> Fault {
        let details = record([
            ("stage".into(), text(self.stage)),
            ("request_written".into(), boolean(self.written)),
            (
                "remote_outcome".into(),
                text(if self.possible {
                    "unknown"
                } else {
                    "not_started"
                }),
            ),
        ])
        .json();
        let cause = match cause {
            Fault::Host("runtime.host-failure", _) => {
                host(format!("HTTP.request {} failed", self.stage))
            }
            other => other,
        };
        cause.with_details(BoundaryDetails {
            http_progress: Some(details),
            ..Default::default()
        })
    }
}
enum Connection {
    Plain(TcpStream),
    Secure(rustls::StreamOwned<rustls::ClientConnection, TcpStream>),
}
impl Read for Connection {
    fn read(&mut self, b: &mut [u8]) -> io::Result<usize> {
        match self {
            Self::Plain(c) => c.read(b),
            Self::Secure(c) => c.read(b),
        }
    }
}
impl Write for Connection {
    fn write(&mut self, b: &[u8]) -> io::Result<usize> {
        match self {
            Self::Plain(c) => c.write(b),
            Self::Secure(c) => c.write(b),
        }
    }
    fn flush(&mut self) -> io::Result<()> {
        match self {
            Self::Plain(c) => c.flush(),
            Self::Secure(c) => c.flush(),
        }
    }
}
impl Connection {
    fn close(self) -> io::Result<()> {
        let (stream, primary) = match self {
            Self::Plain(stream) => (stream, None),
            Self::Secure(mut secure) => {
                secure.conn.send_close_notify();
                let failed = secure.flush().err();
                (secure.sock, failed)
            }
        };
        let cleanup = close_stream(stream).err();
        match (primary, cleanup) {
            (Some(error), _) | (_, Some(error)) => Err(error),
            _ => Ok(()),
        }
    }
}
fn cancelled(context: &Context, deadline: Instant) -> Result<(), Fault> {
    context.check_cancel()?;
    if Instant::now() >= deadline {
        Err(Fault::Host(
            "runtime.cancelled",
            "context deadline exceeded".into(),
        ))
    } else {
        Ok(())
    }
}
fn io_fault(e: io::Error) -> Fault {
    host(e.to_string())
}
fn retry(e: &io::Error) -> bool {
    matches!(
        e.kind(),
        io::ErrorKind::WouldBlock | io::ErrorKind::TimedOut | io::ErrorKind::Interrupted
    )
}
pub fn exchange(
    context: &mut Context,
    policy: &Policy,
    resolver: &super::resolver::Resolver,
    target: &Value,
) -> Outcome {
    let Value::Client {
        method,
        url,
        headers: request_headers,
        body,
    } = target
    else {
        return Err(host("invalid HTTP target"));
    };
    let Value::Url(url) = as_http(url) else {
        return Err(host("invalid HTTP URL"));
    };
    let mut progress = Progress {
        stage: "authorize",
        written: false,
        possible: false,
    };
    let deadline = Instant::now() + Duration::from_secs(5);

    if bytes(body).len() > MAX_BODY {
        return Err(progress.failure(limit("HTTP request body bytes")));
    }
    let Some(allowed) = policy
        .grants
        .get(&(method.string().into(), url.origin.clone()))
    else {
        return Err(progress.failure(Fault::Host(
            "runtime.capability-denied",
            format!("HTTP.request {:?} has no host capability", url.origin),
        )));
    };
    progress.stage = "resolve";
    let resolved = if let Ok(ip) = url.host.parse::<IpAddr>() {
        vec![ip]
    } else {
        resolver
            .resolve(context, &url.host, url.port, deadline)
            .map_err(|e| progress.failure(e))?
    };
    let mut selected = resolved
        .into_iter()
        .map(unmap)
        .filter(|ip| allowed.contains(ip))
        .map(|ip| ip.to_string())
        .collect::<Vec<_>>();
    selected.sort();
    selected.dedup();
    let Some(address) = selected.first() else {
        return Err(progress.failure(Fault::Host(
            "runtime.capability-denied",
            format!("HTTP.request {:?} has no host capability", url.origin),
        )));
    };
    cancelled(context, deadline).map_err(|f| progress.failure(f))?;
    progress.stage = "connect";
    let address = SocketAddr::new(address.parse().unwrap(), url.port);
    let mut stream =
        connect(context, address, deadline).map_err(|fault| progress.failure(fault))?;
    let prepared = (|| -> Result<Option<rustls::ClientConnection>, Fault> {
        stream
            .set_read_timeout(Some(Duration::from_millis(50)))
            .map_err(io_fault)?;
        stream
            .set_write_timeout(Some(Duration::from_millis(50)))
            .map_err(io_fault)?;
        if url.scheme != "https" {
            return Ok(None);
        }
        progress.stage = "tls";
        let provider = Arc::new(rustls::crypto::ring::default_provider());
        let builder = rustls::ClientConfig::builder_with_provider(provider.clone())
            .with_safe_default_protocol_versions()
            .map_err(|error| host(error.to_string()))?;
        #[cfg(target_os = "macos")]
        let mut config = builder
            .dangerous()
            .with_custom_certificate_verifier(Arc::new(super::tls::PlatformVerifier {
                provider,
                hostname: url.host.clone(),
            }))
            .with_no_client_auth();
        #[cfg(not(target_os = "macos"))]
        let mut config = {
            let mut roots = rustls::RootCertStore::empty();
            roots.add_parsable_certificates(system_roots()?);
            if roots.is_empty() {
                return Err(host("system certificate store is empty"));
            }
            builder.with_root_certificates(roots).with_no_client_auth()
        };
        config.alpn_protocols = vec![b"http/1.1".to_vec()];
        let name = rustls::pki_types::ServerName::try_from(url.host.clone())
            .map_err(|error| host(error.to_string()))?;
        let mut tls = rustls::ClientConnection::new(Arc::new(config), name)
            .map_err(|error| host(error.to_string()))?;
        while tls.is_handshaking() {
            cancelled(context, deadline)?;
            match tls.complete_io(&mut stream) {
                Ok(_) => (),
                Err(error) if retry(&error) => continue,
                Err(error) => return Err(io_fault(error)),
            }
        }
        Ok(Some(tls))
    })();
    let mut connection = match prepared {
        Ok(Some(tls)) => Connection::Secure(rustls::StreamOwned::new(tls, stream)),
        Ok(None) => Connection::Plain(stream),
        Err(fault) => return Err(progress.failure(preserve_close(fault, close_stream(stream)))),
    };
    let result = (|| {
        let mut request = format!(
            "{} {} HTTP/1.1\r\nHost: {}\r\nUser-Agent: Go-http-client/1.1\r\nConnection: close\r\n",
            method.string(),
            url.path,
            url.origin.split_once("://").unwrap().1
        )
        .into_bytes();
        let pairs = header_pairs(request_headers);
        let has_agent = pairs.iter().any(|(n, _)| n == "user-agent");
        if has_agent {
            let marker = b"User-Agent: Go-http-client/1.1\r\n";
            let start = request
                .windows(marker.len())
                .position(|w| w == marker)
                .unwrap();
            request.drain(start..start + marker.len());
        }
        for (name, content) in pairs {
            request.extend_from_slice(name.as_bytes());
            request.extend_from_slice(b": ");
            request.extend_from_slice(&content);
            request.extend_from_slice(b"\r\n");
        }
        if !bytes(body).is_empty() || ["POST", "PUT", "PATCH"].contains(&method.string()) {
            request
                .extend_from_slice(format!("Content-Length: {}\r\n", bytes(body).len()).as_bytes())
        }
        request.extend_from_slice(b"\r\n");
        request.extend_from_slice(bytes(body));
        progress.stage = "write-request";
        progress.possible = true;
        let mut offset = 0;
        while offset < request.len() {
            cancelled(context, deadline)?;
            match connection.write(&request[offset..]) {
                Ok(0) => return Err(host("write zero")),
                Ok(n) => offset += n,
                Err(e) if retry(&e) => continue,
                Err(e) => return Err(io_fault(e)),
            }
        }
        loop {
            cancelled(context, deadline)?;
            match connection.flush() {
                Ok(()) => break,
                Err(error) if retry(&error) => continue,
                Err(error) => return Err(io_fault(error)),
            }
        }
        progress.written = true;
        progress.stage = "read-response";
        let mut reader = Reader {
            connection: &mut connection,
            context,
            deadline,
            valid: 0,
            scratch: [0; 8192],
            offset: 0,
            idle_header: false,
            strict_crlf: true,
        };
        let mut interim = 0;
        let mut interim_bytes = 0;
        let (status, mut pairs) = loop {
            let (start, pairs, size) = reader.head()?;
            let fields = start.split_whitespace().collect::<Vec<_>>();
            if fields.len() < 2 || fields[0] != "HTTP/1.1" {
                return Err(host("unsupported HTTP response version"));
            }
            if fields[1].len() != 3 || !fields[1].bytes().all(|byte| byte.is_ascii_digit()) {
                return Err(host("malformed HTTP status code"));
            }
            let status = fields[1]
                .parse::<u16>()
                .map_err(|_| host("malformed HTTP status code"))?;
            if (100..200).contains(&status) && status != 101 {
                interim += 1;
                interim_bytes += size;
                if interim > 8 || interim_bytes > MAX_HEADERS {
                    return Err(limit("HTTP interim response headers"));
                }
                continue;
            }
            if !(200..=599).contains(&status) {
                return Err(host("unsupported HTTP response status"));
            }
            break (status, pairs);
        };
        let declared = framing(&pairs)?;
        let mut seen_length = false;
        pairs.retain(|(n, _)| {
            if n == "transfer-encoding" || n == "trailer" {
                return false;
            }
            if n == "content-length" {
                if matches!(declared, Framing::Chunked) || seen_length {
                    return false;
                }
                seen_length = true
            }
            true
        });
        let h = headers(pairs).map_err(|_| host("invalid HTTP response headers"))?;
        progress.stage = "read-body";
        let content = if method.string() == "HEAD" || status == 204 || status == 304 {
            vec![]
        } else {
            reader.body(declared)?
        };
        if status == 205 && !content.is_empty() {
            return Err(host("HTTP response status prohibits a body"));
        }
        progress.stage = "finalize";
        cancelled(context, deadline)?;
        Ok(response(
            int(status),
            h,
            new(atom("Bytes"), Data::Bytes(content)),
        ))
    })();
    let closed = connection.close();
    match (result, closed) {
        (Ok(v), Ok(())) => Ok(v),
        (Err(primary), cleanup) => Err(progress.failure(preserve_close(primary, cleanup))),
        (Ok(_), Err(_error)) => Err(progress.failure(
            host("HTTP resource release failed").with_details(BoundaryDetails {
                cleanup_errors: vec!["HTTP resource release failed".into()],
                ..Default::default()
            }),
        )),
    }
}
#[derive(Clone, Copy)]
pub enum Framing {
    Length(usize),
    Chunked,
    Eof,
}
pub fn framing(headers: &[(String, Vec<u8>)]) -> Result<Framing, Fault> {
    let mut length = None;
    let mut transfer = vec![];
    for (n, b) in headers {
        if n == "content-length" {
            let s = std::str::from_utf8(b)
                .map_err(|_| host("bad Content-Length"))?
                .trim();
            if s.is_empty() || !s.bytes().all(|b| b.is_ascii_digit()) {
                return Err(host("bad Content-Length"));
            }
            let n = s.parse::<usize>().map_err(|_| host("bad Content-Length"))?;
            if length.is_some_and(|old| old != n) {
                return Err(host("conflicting Content-Length headers"));
            }
            length = Some(n)
        } else if n == "transfer-encoding" {
            transfer.push(String::from_utf8_lossy(b).to_ascii_lowercase());
        }
    }
    if !transfer.is_empty() {
        if transfer != ["chunked"] {
            return Err(host("unsupported transfer encoding"));
        }
        return Ok(Framing::Chunked);
    }
    Ok(length.map(Framing::Length).unwrap_or(Framing::Eof))
}
pub trait ReadWrite: Read + Write {}
impl<T: Read + Write> ReadWrite for T {}
pub struct Reader<'a, T: ReadWrite> {
    pub connection: &'a mut T,
    pub context: &'a Context,
    pub deadline: Instant,
    pub valid: usize,
    pub scratch: [u8; 8192],
    pub offset: usize,
    pub idle_header: bool,
    pub strict_crlf: bool,
}
impl<T: ReadWrite> Reader<'_, T> {
    fn byte(&mut self) -> Result<Option<u8>, Fault> {
        loop {
            cancelled(self.context, self.deadline)?;
            if self.offset < self.valid {
                let b = self.scratch[self.offset];
                self.offset += 1;
                return Ok(Some(b));
            }
            match self.connection.read(&mut self.scratch) {
                Ok(0) => return Ok(None),
                Ok(n) => {
                    if self.idle_header {
                        self.deadline = Instant::now() + Duration::from_secs(5);
                        self.idle_header = false;
                    }
                    self.valid = n;
                    self.offset = 0
                }
                Err(e) if retry(&e) => continue,
                Err(e) => return Err(io_fault(e)),
            }
        }
    }
    pub fn line(&mut self, max: usize) -> Result<Vec<u8>, Fault> {
        let mut line = vec![];
        loop {
            let b = self.byte()?.ok_or_else(|| host("unexpected EOF"))?;
            line.push(b);
            if line.len() > max {
                return Err(limit("HTTP response header bytes"));
            }
            if b == b'\n' {
                if self.strict_crlf && (line.len() < 2 || line[line.len() - 2] != b'\r') {
                    return Err(host("HTTP response requires CRLF framing"));
                }
                line.pop();
                if line.last() == Some(&b'\r') {
                    line.pop();
                }
                return Ok(line);
            }
            if self.strict_crlf && line.len() > 1 && line[line.len() - 2] == b'\r' {
                return Err(host("HTTP response requires CRLF framing"));
            }
        }
    }
    pub fn head(&mut self) -> Result<(String, Vec<(String, Vec<u8>)>, usize), Fault> {
        let line = self.line(MAX_HEADERS)?;
        let size = line.len() + 2;
        let first = String::from_utf8(line).map_err(|_| host("invalid HTTP status line"))?;
        let (headers, size) = self.fields(size)?;
        self.strict_crlf = true;
        Ok((first, headers, size))
    }
    fn fields(&mut self, mut size: usize) -> Result<(Vec<(String, Vec<u8>)>, usize), Fault> {
        let mut headers: Vec<(String, Vec<u8>)> = vec![];
        loop {
            let line = self.line(MAX_HEADERS.saturating_sub(size))?;
            size += line.len() + 2;
            if size > MAX_HEADERS {
                return Err(limit("HTTP response header bytes"));
            }
            if line.is_empty() {
                break;
            }
            if line.iter().any(|b| *b != 9 && (*b < 32 || *b == 127)) {
                return Err(host("malformed MIME header"));
            }
            if line.starts_with(b" ") || line.starts_with(b"\t") {
                let Some((_, previous)) = headers.last_mut() else {
                    return Err(host("malformed MIME header"));
                };
                previous.push(b' ');
                previous.extend_from_slice(trim(&line));
                continue;
            }
            let Some(colon) = line.iter().position(|b| *b == b':') else {
                return Err(host("malformed MIME header"));
            };
            let name =
                std::str::from_utf8(&line[..colon]).map_err(|_| host("malformed MIME header"))?;
            let name = header_name(name).map_err(host)?;
            let value = trim(&line[colon + 1..]).to_vec();
            headers.push((name, value));
        }
        Ok((headers, size))
    }
    fn exact(&mut self, length: usize) -> Result<Vec<u8>, Fault> {
        if length > MAX_BODY {
            return Err(limit("HTTP response body bytes"));
        }
        let mut out = Vec::with_capacity(length);
        for _ in 0..length {
            out.push(self.byte()?.ok_or_else(|| host("unexpected EOF"))?)
        }
        Ok(out)
    }
    pub fn body(&mut self, framing: Framing) -> Result<Vec<u8>, Fault> {
        match framing {
            Framing::Length(n) => self.exact(n),
            Framing::Eof => {
                let mut out = vec![];
                while let Some(b) = self.byte()? {
                    out.push(b);
                    if out.len() > MAX_BODY {
                        return Err(limit("HTTP response body bytes"));
                    }
                }
                Ok(out)
            }
            Framing::Chunked => {
                let mut out = vec![];
                loop {
                    let line = self.line(4096)?;
                    let mut line = line.as_slice();
                    while line
                        .last()
                        .is_some_and(|byte| *byte == b' ' || *byte == b'\t')
                    {
                        line = &line[..line.len() - 1];
                    }
                    let digits = line.split(|byte| *byte == b';').next().unwrap();
                    if digits.is_empty() || !digits.iter().all(u8::is_ascii_hexdigit) {
                        return Err(host("invalid byte in chunk length"));
                    }
                    let n = usize::from_str_radix(
                        std::str::from_utf8(digits)
                            .map_err(|_| host("invalid byte in chunk length"))?,
                        16,
                    )
                    .map_err(|_| host("invalid byte in chunk length"))?;
                    if n == 0 {
                        self.fields(0)?;
                        break;
                    }
                    if out.len().saturating_add(n) > MAX_BODY {
                        return Err(limit("HTTP response body bytes"));
                    }
                    out.extend(self.exact(n)?);
                    if self.line(2)? != b"" {
                        return Err(host("malformed chunked encoding"));
                    }
                }
                Ok(out)
            }
        }
    }
}
fn trim(mut b: &[u8]) -> &[u8] {
    while b.first().is_some_and(|b| *b == b' ' || *b == b'\t') {
        b = &b[1..]
    }
    while b.last().is_some_and(|b| *b == b' ' || *b == b'\t') {
        b = &b[..b.len() - 1]
    }
    b
}
fn connect(context: &Context, address: SocketAddr, deadline: Instant) -> Result<TcpStream, Fault> {
    use std::os::fd::{AsRawFd, FromRawFd};
    let family = if address.is_ipv4() {
        libc::AF_INET
    } else {
        libc::AF_INET6
    };
    #[cfg(target_os = "linux")]
    let socket_type = libc::SOCK_STREAM | libc::SOCK_CLOEXEC;
    #[cfg(target_os = "macos")]
    let socket_type = libc::SOCK_STREAM;
    let fd = unsafe { libc::socket(family, socket_type, 0) };
    if fd < 0 {
        return Err(io_fault(io::Error::last_os_error()));
    }
    let stream = unsafe { TcpStream::from_raw_fd(fd) };
    let setup = (|| -> Result<(), Fault> {
        #[cfg(target_os = "macos")]
        {
            let flags = unsafe { libc::fcntl(fd, libc::F_GETFD) };
            if flags < 0 || unsafe { libc::fcntl(fd, libc::F_SETFD, flags | libc::FD_CLOEXEC) } < 0
            {
                return Err(io_fault(io::Error::last_os_error()));
            }
            let enabled: libc::c_int = 1;
            if unsafe {
                libc::setsockopt(
                    fd,
                    libc::SOL_SOCKET,
                    libc::SO_NOSIGPIPE,
                    &enabled as *const _ as *const libc::c_void,
                    std::mem::size_of_val(&enabled) as _,
                )
            } < 0
            {
                return Err(io_fault(io::Error::last_os_error()));
            }
        }
        stream.set_nonblocking(true).map_err(io_fault)?;
        let mut storage: libc::sockaddr_storage = unsafe { std::mem::zeroed() };
        let length = match address {
            SocketAddr::V4(a) => {
                let mut raw: libc::sockaddr_in = unsafe { std::mem::zeroed() };
                raw.sin_family = libc::AF_INET as _;
                raw.sin_port = a.port().to_be();
                raw.sin_addr = libc::in_addr {
                    s_addr: u32::from_ne_bytes(a.ip().octets()),
                };
                #[cfg(target_os = "macos")]
                {
                    raw.sin_len = std::mem::size_of::<libc::sockaddr_in>() as _;
                }
                unsafe { std::ptr::write(&mut storage as *mut _ as *mut libc::sockaddr_in, raw) };
                std::mem::size_of::<libc::sockaddr_in>()
            }
            SocketAddr::V6(a) => {
                let mut raw: libc::sockaddr_in6 = unsafe { std::mem::zeroed() };
                raw.sin6_family = libc::AF_INET6 as _;
                raw.sin6_port = a.port().to_be();
                raw.sin6_addr = libc::in6_addr {
                    s6_addr: a.ip().octets(),
                };
                #[cfg(target_os = "macos")]
                {
                    raw.sin6_len = std::mem::size_of::<libc::sockaddr_in6>() as _;
                }
                unsafe { std::ptr::write(&mut storage as *mut _ as *mut libc::sockaddr_in6, raw) };
                std::mem::size_of::<libc::sockaddr_in6>()
            }
        };
        let result = unsafe {
            libc::connect(
                fd,
                &storage as *const _ as *const libc::sockaddr,
                length as _,
            )
        };
        if result < 0 {
            let err = io::Error::last_os_error();
            if err.raw_os_error() != Some(libc::EINPROGRESS) {
                return Err(io_fault(err));
            }
            loop {
                cancelled(context, deadline)?;
                let mut descriptor = libc::pollfd {
                    fd: stream.as_raw_fd(),
                    events: libc::POLLOUT,
                    revents: 0,
                };
                let result = unsafe { libc::poll(&mut descriptor, 1, 50) };
                if result < 0 {
                    let e = io::Error::last_os_error();
                    if e.kind() == io::ErrorKind::Interrupted {
                        continue;
                    }
                    return Err(io_fault(e));
                }
                if result == 0 {
                    continue;
                }
                if let Some(e) = stream.take_error().map_err(io_fault)? {
                    return Err(io_fault(e));
                }
                break;
            }
        }
        stream.set_nonblocking(false).map_err(io_fault)?;
        Ok(())
    })();
    match setup {
        Ok(()) => Ok(stream),
        Err(fault) => Err(preserve_close(fault, close_stream(stream))),
    }
}

#[cfg(not(target_os = "macos"))]
fn system_roots() -> Result<Vec<rustls::pki_types::CertificateDer<'static>>, Fault> {
    let result = rustls_native_certs::load_native_certs();
    if result.certs.is_empty() && !result.errors.is_empty() {
        return Err(host("failed to load system certificate roots"));
    }
    Ok(result.certs)
}

fn close_stream(stream: TcpStream) -> io::Result<()> {
    use std::os::fd::IntoRawFd;
    if unsafe { libc::close(stream.into_raw_fd()) } == 0 {
        Ok(())
    } else {
        Err(io::Error::last_os_error())
    }
}
fn preserve_close(primary: Fault, cleanup: io::Result<()>) -> Fault {
    if cleanup.is_err() {
        primary.with_details(BoundaryDetails {
            cleanup_errors: vec!["HTTP resource release failed".into()],
            ..Default::default()
        })
    } else {
        primary
    }
}
