use super::client::{framing, Framing, Policy, Reader};
use super::*;
use std::{
    io::{Read, Write},
    net::{Shutdown, TcpListener, TcpStream},
    sync::{
        atomic::{AtomicBool, Ordering},
        Mutex,
    },
    thread,
    time::{Duration, Instant},
};
struct Listener {
    listener: TcpListener,
    target: Handle,
    address: String,
}
struct State {
    listeners: Vec<Listener>,
    connections: Vec<Connection>,
    workers: Vec<thread::JoinHandle<()>>,
    failure: Option<Fault>,
    stopping: bool,
}
struct Wire<'a> {
    stream: &'a mut TcpStream,
    reader: std::io::BufReader<TcpStream>,
}
impl Read for Wire<'_> {
    fn read(&mut self, buffer: &mut [u8]) -> std::io::Result<usize> {
        if buffer.is_empty() {
            return Ok(0);
        }
        self.reader.read(&mut buffer[..1])
    }
}
impl Write for Wire<'_> {
    fn write(&mut self, buffer: &[u8]) -> std::io::Result<usize> {
        self.stream.write(buffer)
    }
    fn flush(&mut self) -> std::io::Result<()> {
        self.stream.flush()
    }
}
struct Connection {
    stream: TcpStream,
    active: Arc<AtomicBool>,
    cancel: Arc<AtomicBool>,
    done: Arc<AtomicBool>,
}
pub struct Http {
    policy: Policy,
    resolver: super::resolver::Resolver,
    ports: Vec<u16>,
    state: Arc<Mutex<State>>,
}
impl Http {
    pub fn new(grants: &[String], ports: &[u16]) -> Result<Self, String> {
        let mut seen = std::collections::BTreeSet::new();
        for port in ports {
            if !seen.insert(*port) {
                return Err(format!("duplicate listen port {port}"));
            }
        }
        Ok(Self {
            policy: Policy::new(grants)?,
            resolver: super::resolver::Resolver::new(),
            ports: ports.to_vec(),
            state: Arc::new(Mutex::new(State {
                listeners: vec![],
                connections: vec![],
                workers: vec![],
                failure: None,
                stopping: false,
            })),
        })
    }
    pub fn empty() -> Self {
        Self::new(&[], &[]).unwrap()
    }
    pub fn commit(&self, context: &mut Context, site: &Site, target: &Handle) -> Outcome {
        match as_http(target) {
            Value::Client { method, url, .. } => {
                match client::exchange(context, &self.policy, &self.resolver, as_http(target)) {
                    Ok(v) => {
                        context.last_activation = Some(
                            json!({"operation":"HTTP.request","span":{"file":site.raw["file"],"start":site.raw.coordinate("start"),"end":site.raw.coordinate("end")},"result_type":"HTTP.Response","status":v.fields()["status"].json()}),
                        );
                        context.charge(&v)?;
                        Ok(v)
                    }
                    Err(fault) => {
                        if let Fault::Host("runtime.host-failure", message) = fault.primary() {
                            let details = match &fault {
                                Fault::Boundary { details, .. } => details.as_ref().clone(),
                                _ => Default::default(),
                            };
                            let Value::Url(url) = as_http(url) else {
                                std::process::abort()
                            };
                            let progress = details.http_progress.as_ref().map(progress_handle);
                            let mut input = vec![
                                ("method".into(), method.clone()),
                                ("origin".into(), text(&url.origin)),
                            ];
                            if let Some(progress) = progress {
                                input.push(("progress".into(), progress))
                            }
                            if !details.cleanup_errors.is_empty() {
                                input.push((
                                    "cleanup_errors".into(),
                                    new(
                                        Type::List(Box::new(atom("String"))),
                                        Data::List(
                                            details.cleanup_errors.iter().map(text).collect(),
                                        ),
                                    ),
                                ))
                            }
                            let mut activation = site.clone();
                            activation.operation = "HTTP.request".into();
                            return Err(Fault::Recoverable(error(
                                &activation,
                                "HTTP.ExchangeFailed",
                                message,
                                record(input),
                            )));
                        }
                        Err(boundary_host_fault(fault))
                    }
                }
            }
            Value::Server { port, .. } => {
                if !context.session.serving {
                    return Err(Fault::Host(
                        "runtime.server-requires-serve",
                        "HTTP server activation requires lo serve".into(),
                    ));
                }
                let mut activation = site.clone();
                activation.operation = "HTTP.listen".into();
                let site = &activation;
                let failed = |message: &str| {
                    Fault::Recoverable(error(
                        site,
                        "HTTP.ServerStartFailed",
                        message,
                        record([("port".into(), port.clone())]),
                    ))
                };

                let Some(port) = port.integer().to_u16() else {
                    return Err(failed(&format!(
                        "HTTP.listen: invalid port {}",
                        port.integer()
                    )));
                };
                let address = format!("127.0.0.1:{port}");
                if !self.ports.contains(&port) {
                    return Err(Fault::Host(
                        "runtime.capability-denied",
                        format!("boundary operation failed: HTTP.listen {address:?} has no host capability"),
                    ));
                }
                let mut state = self.state.lock().unwrap();
                if state.stopping {
                    return Err(Fault::RawCancellation);
                }
                if state.listeners.len() == 64 {
                    return Err(limit("active HTTP servers"));
                }
                let listener = match TcpListener::bind(&address) {
                    Ok(l) => l,
                    Err(e) => return Err(failed(&format!("HTTP.listen {address}: {e}"))),
                };
                listener
                    .set_nonblocking(true)
                    .map_err(|e| host(format!("HTTP.listen {address}: {e}")))?;
                let actual = listener.local_addr().map_err(|e| host(e.to_string()))?;
                let fields = record([
                    ("address".into(), text(actual.to_string())),
                    ("port".into(), int(actual.port())),
                ]);
                let acceptor = listener
                    .try_clone()
                    .map_err(|error| host(error.to_string()))?;
                let state_arc = self.state.clone();
                let session = context.session.clone();
                let target_for_worker = target.clone();
                let worker = thread::Builder::new()
                    .name("lo-http-listener".into())
                    .spawn(move || accept_loop(acceptor, target_for_worker, session, state_arc))
                    .map_err(|error| host(error.to_string()))?;
                state.workers.push(worker);
                state.listeners.push(Listener {
                    listener,
                    target: target.clone(),
                    address: actual.to_string(),
                });
                let receipt = wrap("HTTP.ServerReceipt", Value::Fields(fields));
                context.last_activation = Some(
                    json!({"operation":"HTTP.listen","span":{"file":site.raw["file"],"start":site.raw.coordinate("start"),"end":site.raw.coordinate("end")},"receipt":receipt.json()}),
                );
                context.charge(&receipt)?;
                Ok(receipt)
            }
            _ => Err(host("invalid HTTP commit target")),
        }
    }
    pub fn snapshot(&self) -> Vec<Json> {
        self.state
            .lock()
            .unwrap()
            .listeners
            .iter()
            .map(|l| json!({"address":l.address}))
            .collect()
    }
    pub fn serve(self: &Arc<Self>, _session: Arc<Session>) -> Result<(), Fault> {
        if self.state.lock().unwrap().listeners.is_empty() {
            return Err(Fault::Host(
                "runtime.no-server",
                "serve requires an activated HTTP server".into(),
            ));
        }
        loop {
            if crate::cli::cancelled() {
                return Ok(());
            }
            let mut state = self.state.lock().unwrap();
            if let Some(failure) = state.failure.take() {
                state.stopping = true;
                return Err(failure);
            }
            if state.stopping {
                return Ok(());
            }
            drop(state);
            thread::sleep(Duration::from_millis(10));
        }
    }
    pub fn wait_retained(&self) -> Result<(), Fault> {
        self.resolver.wait_retained()
    }
    pub fn shutdown(&self) -> Result<(), Fault> {
        let (connections, workers) = {
            let mut state = self.state.lock().unwrap();
            state.stopping = true;
            state.listeners.clear();
            (
                std::mem::take(&mut state.connections),
                std::mem::take(&mut state.workers),
            )
        };
        for connection in &connections {
            if !connection.active.load(Ordering::Acquire) {
                let _ = connection.stream.shutdown(Shutdown::Both);
            }
        }
        let deadline = Instant::now() + Duration::from_secs(5);
        while connections
            .iter()
            .any(|c| c.active.load(Ordering::Acquire) && !c.done.load(Ordering::Acquire))
            && Instant::now() < deadline
        {
            thread::sleep(Duration::from_millis(10));
        }
        for connection in connections {
            if !connection.done.load(Ordering::Acquire) {
                connection.cancel.store(true, Ordering::Release);
                let _ = connection.stream.shutdown(Shutdown::Both);
            }
        }
        for worker in workers {
            if worker.join().is_err() {
                return Err(host("HTTP request worker failed"));
            }
        }
        let resolver_closed = self.resolver.close();
        let mut state = self.state.lock().unwrap();
        if let Some(fault) = state.failure.take() {
            match resolver_closed {
                Ok(()) => Err(fault),
                Err(cleanup) => Err(fault.with_details(BoundaryDetails {
                    cleanup_errors: vec![cleanup.message()],
                    ..Default::default()
                })),
            }
        } else {
            resolver_closed
        }
    }
}
fn handle_request(
    stream: &mut Wire<'_>,
    session: &Arc<Session>,
    target: &Handle,
    active: Arc<AtomicBool>,
    cancel: Arc<AtomicBool>,
) -> Result<bool, Fault> {
    let Value::Server {
        port,
        handler,
        on_error,
        ..
    } = as_http(target)
    else {
        return Err(host("invalid HTTP server"));
    };
    let mut context = Context::request(session.clone())?;
    context.cancellation = cancel;

    let run = (|| {
        let request = read_request(stream, &context, &active);
        context.deadline = Some(
            request
                .as_ref()
                .ok()
                .map(|request| request.3)
                .unwrap_or_else(|| Instant::now() + Duration::from_secs(5)),
        );
        let http10 = request.as_ref().ok().is_some_and(|request| request.2);
        let drain = request.is_err();
        let keep = request.as_ref().ok().is_some_and(|request| request.1);
        let request = request.map(|request| request.0);
        let head = request
            .as_ref()
            .ok()
            .is_some_and(|r| r.fields()["method"].string() == "HEAD");
        let mut from_error = false;
        let output = match request {
            Ok(request) => match context.invoke(*handler, &[request]) {
                Ok(v) => {
                    if let Some(cause) = cause(&v) {
                        from_error = true;
                        context.invoke(
                            *on_error,
                            &[frame(
                                &cause,
                                "",
                                "HTTP.request",
                                record([("port".into(), port.clone())]),
                            )],
                        )?
                    } else {
                        v
                    }
                }
                Err(Fault::Recoverable(cause)) => {
                    from_error = true;
                    context.invoke(
                        *on_error,
                        &[frame(
                            &cause,
                            "",
                            "HTTP.request",
                            record([("port".into(), port.clone())]),
                        )],
                    )?
                }
                Err(fault) => return Err(fault),
            },
            Err(RequestError::Cause(kind, message, input)) => {
                from_error = true;
                let cause = request_error(&kind, &message, input);
                context.invoke(
                    *on_error,
                    &[frame(
                        &cause,
                        "",
                        "HTTP.request",
                        record([("port".into(), port.clone())]),
                    )],
                )?
            }
            Err(RequestError::Host(_)) => return Ok(false),
        };
        let output = match response_data(&output) {
            Ok(r) => r,
            Err((message, input)) => {
                if from_error {
                    return Err(host(format!(
                        "HTTP.respond \"onError\": HTTP.InvalidResponse: {message}"
                    )));
                }
                let cause = request_error("HTTP.InvalidResponse", &message, input);
                let result = context.invoke(
                    *on_error,
                    &[frame(
                        &cause,
                        "",
                        "HTTP.respond",
                        record([("port".into(), port.clone())]),
                    )],
                )?;
                response_data(&result).map_err(|(m, _)| {
                    host(format!("HTTP.respond onError: HTTP.InvalidResponse: {m}"))
                })?
            }
        };
        context.close()?;
        let (status, headers, body) = output;
        let (content, keep) = emit_response(status, headers, &body, head, keep, http10)?;
        let mut offset = 0;
        while offset < content.len() {
            context.check_cancel()?;
            match stream.write(&content[offset..]) {
                Ok(0) => return Err(host("HTTP.respond: write zero")),
                Ok(n) => offset += n,
                Err(e)
                    if matches!(
                        e.kind(),
                        std::io::ErrorKind::WouldBlock
                            | std::io::ErrorKind::TimedOut
                            | std::io::ErrorKind::Interrupted
                    ) =>
                {
                    continue
                }
                Err(e)
                    if matches!(
                        e.kind(),
                        std::io::ErrorKind::BrokenPipe
                            | std::io::ErrorKind::ConnectionReset
                            | std::io::ErrorKind::ConnectionAborted
                    ) =>
                {
                    return Err(Fault::Host(
                        "runtime.cancelled",
                        format!("HTTP.respond: {e}"),
                    ))
                }
                Err(e) => return Err(host(format!("HTTP.respond: {e}"))),
            }
        }
        if drain {
            let _ = stream.stream.shutdown(Shutdown::Write);
            let deadline = Instant::now() + Duration::from_millis(500);
            let mut buffer = [0u8; 8192];
            let mut drained = 0;
            while Instant::now() < deadline && drained < (256 << 10) {
                match stream.reader.read(&mut buffer) {
                    Ok(0) => break,
                    Ok(count) => drained += count,
                    Err(error)
                        if matches!(
                            error.kind(),
                            std::io::ErrorKind::WouldBlock
                                | std::io::ErrorKind::TimedOut
                                | std::io::ErrorKind::Interrupted
                        ) =>
                    {
                        continue
                    }
                    Err(_) => break,
                }
            }
        }
        Ok(keep)
    })();
    let run = run.map_err(|fault| context.capture_fault(fault));
    let run = match run {
        Err(fault)
            if matches!(
                fault.primary(),
                Fault::Host("runtime.cancelled", _) | Fault::RawCancellation
            ) && !has_progress(&fault) =>
        {
            Ok(false)
        }
        other => other,
    };
    let close = context.close();
    match (run, close) {
        (Ok(keep), Ok(())) => Ok(keep),
        (Err(f), Err(cleanup)) => Err(f.with_details(BoundaryDetails {
            cleanup_errors: vec![cleanup.message()],
            ..Default::default()
        })),
        (Err(f), Ok(())) => Err(f),
        (_, Err(f)) => Err(f),
    }
}
fn request_error(kind: &str, message: &str, input: Handle) -> Handle {
    new(
        atom("Error"),
        Data::Error(value::Error {
            kind: kind.into(),
            message: message.into(),
            input: view(&input),
            trace: vec![],
            flow_progress: vec![],
            site: 0,
            operation: "".into(),
        }),
    )
}
enum RequestError {
    Cause(String, String, Handle),
    Host(Fault),
}
fn read_request(
    stream: &mut Wire<'_>,
    context: &Context,
    active: &AtomicBool,
) -> Result<(Handle, bool, bool, Instant), RequestError> {
    let deadline = Instant::now() + Duration::from_secs(30);
    let mut reader = Reader {
        connection: stream,
        context,
        deadline,
        pending: vec![],
        offset: 0,
        idle_header: true,
        strict_crlf: false,
    };
    let (first, mut pairs, _) = reader.head().map_err(RequestError::Host)?;
    reader.deadline = Instant::now() + Duration::from_secs(5);
    let invocation_deadline = reader.deadline;
    let parts = first.split(' ').collect::<Vec<_>>();
    if parts.len() != 3 || !token(parts[0]) || !(parts[2] == "HTTP/1.1" || parts[2] == "HTTP/1.0") {
        return Err(RequestError::Host(host("invalid HTTP request")));
    }
    active.store(true, Ordering::Release);
    let method = parts[0];
    let raw_path = request_path(method, parts[1]).map_err(RequestError::Host)?;
    let path = percent_decode(raw_path).map_err(RequestError::Host)?;
    let input = record([
        (
            "method".into(),
            new(atom("Bytes"), Data::Bytes(method.as_bytes().to_vec())),
        ),
        ("path".into(), new(atom("Bytes"), Data::Bytes(path.clone()))),
    ]);
    let path = String::from_utf8(path).map_err(|_| {
        RequestError::Cause(
            "HTTP.InvalidRequest".into(),
            "request text is not valid UTF-8".into(),
            input.clone(),
        )
    })?;
    let declared = framing(&pairs).map_err(RequestError::Host)?;
    if matches!(declared,Framing::Length(n)if n>MAX_BODY) {
        return Err(RequestError::Cause(
            "HTTP.RequestTooLarge".into(),
            "request body exceeds its byte limit".into(),
            input,
        ));
    }
    if pairs.iter().filter(|(n, _)| n == "host").count() != 1 && parts[2] == "HTTP/1.1" {
        return Err(RequestError::Host(host("invalid Host header")));
    }
    let connection_token = |expected: &[u8]| {
        pairs.iter().any(|(name, value)| {
            name == "connection"
                && value
                    .split(|byte| *byte == b',')
                    .any(|part| part.trim_ascii().eq_ignore_ascii_case(expected))
        })
    };
    let keep =
        !connection_token(b"close") && (parts[2] != "HTTP/1.0" || connection_token(b"keep-alive"));
    let host_header = pairs
        .iter()
        .find(|(n, _)| n == "host")
        .map(|(_, v)| v.clone())
        .unwrap_or_default();
    pairs.retain(|(n, _)| !["host", "transfer-encoding", "trailer"].contains(&n.as_str()));
    pairs.insert(0, ("host".into(), host_header));
    if pairs
        .iter()
        .any(|(n, v)| n == "expect" && v.eq_ignore_ascii_case(b"100-continue"))
    {
        reader
            .connection
            .write_all(b"HTTP/1.1 100 Continue\r\n\r\n")
            .map_err(|e| RequestError::Host(host(e.to_string())))?;
    }
    let headers = headers(pairs)
        .map_err(|e| RequestError::Cause("HTTP.InvalidRequest".into(), e, input.clone()))?;
    let body = reader
        .body(match declared {
            Framing::Eof => Framing::Length(0),
            other => other,
        })
        .map_err(|f| match f.primary() {
            Fault::Host("runtime.limit", _) => RequestError::Cause(
                "HTTP.RequestTooLarge".into(),
                "request body exceeds its byte limit".into(),
                input.clone(),
            ),
            _ => RequestError::Cause(
                "HTTP.RequestReadFailed".into(),
                "read request body: unexpected EOF".into(),
                input.clone(),
            ),
        })?;
    Ok((
        wrap(
            "HTTP.Request",
            Value::Fields(record([
                ("method".into(), text(method)),
                ("path".into(), text(path)),
                ("headers".into(), headers),
                ("body".into(), new(atom("Bytes"), Data::Bytes(body))),
            ])),
        ),
        keep,
        parts[2] == "HTTP/1.0",
        invocation_deadline,
    ))
}
fn request_path<'a>(method: &str, target: &'a str) -> Result<&'a str, Fault> {
    let target = target.split('?').next().unwrap();
    if target.starts_with('/') || target == "*" {
        return Ok(target);
    }
    if method == "CONNECT" {
        return Ok("");
    }
    let (scheme, rest) = target
        .split_once(':')
        .ok_or_else(|| host("invalid HTTP request target"))?;
    if !scheme.starts_with(|c: char| c.is_ascii_alphabetic())
        || !scheme
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"+-.".contains(&b))
    {
        return Err(host("invalid HTTP request target"));
    }
    let Some(authority) = rest.strip_prefix("//") else {
        return Ok("");
    };
    Ok(authority
        .find('/')
        .map(|start| &authority[start..])
        .unwrap_or(""))
}
fn percent_decode(input: &str) -> Result<Vec<u8>, Fault> {
    let b = input.as_bytes();
    let mut out = vec![];
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'%' {
            if i + 2 >= b.len() {
                return Err(host("invalid URL escape"));
            }
            let s =
                std::str::from_utf8(&b[i + 1..i + 3]).map_err(|_| host("invalid URL escape"))?;
            out.push(u8::from_str_radix(s, 16).map_err(|_| host("invalid URL escape"))?);
            i += 3
        } else {
            out.push(b[i]);
            i += 1
        }
    }
    Ok(out)
}
fn response_data(
    handle: &Handle,
) -> Result<(u16, Vec<(String, Vec<u8>)>, Vec<u8>), (String, Handle)> {
    let handle = if let Data::Success(v) = &handle.data {
        v
    } else {
        handle
    };
    let fields = handle.fields();
    let input = record(fields.iter().map(|(n, v)| (n.clone(), v.clone())));
    let status = fields["status"]
        .integer()
        .to_u16()
        .filter(|s| (200..=599).contains(s))
        .ok_or_else(|| {
            (
                "response status must be between 200 and 599".into(),
                input.clone(),
            )
        })?;
    let body = bytes(&fields["body"]).to_vec();
    if [204, 205, 304].contains(&status) && !body.is_empty() {
        return Err(("response status prohibits a body".into(), input));
    }
    Ok((status, header_pairs(&fields["headers"]), body))
}
fn reason(status: u16) -> String {
    match status {
        200 => "OK".into(),
        201 => "Created".into(),
        202 => "Accepted".into(),
        203 => "Non-Authoritative Information".into(),
        204 => "No Content".into(),
        205 => "Reset Content".into(),
        206 => "Partial Content".into(),
        207 => "Multi-Status".into(),
        208 => "Already Reported".into(),
        226 => "IM Used".into(),
        300 => "Multiple Choices".into(),
        301 => "Moved Permanently".into(),
        302 => "Found".into(),
        303 => "See Other".into(),
        304 => "Not Modified".into(),
        305 => "Use Proxy".into(),
        307 => "Temporary Redirect".into(),
        308 => "Permanent Redirect".into(),
        400 => "Bad Request".into(),
        401 => "Unauthorized".into(),
        402 => "Payment Required".into(),
        403 => "Forbidden".into(),
        404 => "Not Found".into(),
        405 => "Method Not Allowed".into(),
        406 => "Not Acceptable".into(),
        407 => "Proxy Authentication Required".into(),
        408 => "Request Timeout".into(),
        409 => "Conflict".into(),
        410 => "Gone".into(),
        411 => "Length Required".into(),
        412 => "Precondition Failed".into(),
        413 => "Request Entity Too Large".into(),
        414 => "Request URI Too Long".into(),
        415 => "Unsupported Media Type".into(),
        416 => "Requested Range Not Satisfiable".into(),
        417 => "Expectation Failed".into(),
        418 => "I'm a teapot".into(),
        421 => "Misdirected Request".into(),
        422 => "Unprocessable Entity".into(),
        423 => "Locked".into(),
        424 => "Failed Dependency".into(),
        425 => "Too Early".into(),
        426 => "Upgrade Required".into(),
        428 => "Precondition Required".into(),
        429 => "Too Many Requests".into(),
        431 => "Request Header Fields Too Large".into(),
        451 => "Unavailable For Legal Reasons".into(),
        500 => "Internal Server Error".into(),
        501 => "Not Implemented".into(),
        502 => "Bad Gateway".into(),
        503 => "Service Unavailable".into(),
        504 => "Gateway Timeout".into(),
        505 => "HTTP Version Not Supported".into(),
        506 => "Variant Also Negotiates".into(),
        507 => "Insufficient Storage".into(),
        508 => "Loop Detected".into(),
        510 => "Not Extended".into(),
        511 => "Network Authentication Required".into(),
        _ => format!("status code {status}"),
    }
}

fn progress_handle(value: &Json) -> Handle {
    let fields = &value["value"];
    record([
        (
            "stage".into(),
            text(fields["stage"]["value"].as_str().unwrap()),
        ),
        (
            "request_written".into(),
            boolean(fields["request_written"]["value"].as_bool().unwrap()),
        ),
        (
            "remote_outcome".into(),
            text(fields["remote_outcome"]["value"].as_str().unwrap()),
        ),
    ])
}

fn has_progress(fault: &Fault) -> bool {
    if let Fault::Boundary { details, .. } = fault {
        details.process_progress.is_some()
            || details.http_progress.is_some()
            || details.write_progress.is_some()
            || details.removal_progress.is_some()
            || details.last_completed_activation.is_some()
            || !details.flow_progress.is_empty()
            || !details.cleanup_errors.is_empty()
    } else {
        false
    }
}

fn accept_loop(
    listener: TcpListener,
    target: Handle,
    session: Arc<Session>,
    state: Arc<Mutex<State>>,
) {
    loop {
        {
            let mut state = state.lock().unwrap();
            if state.stopping {
                return;
            }
            state
                .connections
                .retain(|connection| !connection.done.load(Ordering::Acquire));
            let mut index = 0;
            while index < state.workers.len() {
                if state.workers[index].is_finished() {
                    let worker = state.workers.swap_remove(index);
                    if worker.join().is_err() && state.failure.is_none() {
                        state.failure = Some(host("HTTP request worker failed"));
                    }
                } else {
                    index += 1
                }
            }
        }
        match listener.accept() {
            Ok((mut stream, _)) => {
                let mut current = state.lock().unwrap();
                if current.stopping {
                    let _ = stream.shutdown(Shutdown::Both);
                    return;
                }
                let prepared = (|| {
                    stream.set_read_timeout(Some(Duration::from_millis(50)))?;
                    stream.set_write_timeout(Some(Duration::from_millis(50)))?;
                    stream.try_clone()
                })();
                let pending = match prepared {
                    Ok(stream) => stream,
                    Err(error) => {
                        current.failure = Some(host(error.to_string()));
                        return;
                    }
                };
                let active = Arc::new(AtomicBool::new(false));
                let cancel = Arc::new(AtomicBool::new(false));
                let done = Arc::new(AtomicBool::new(false));
                let tracked = Connection {
                    stream: pending,
                    active: active.clone(),
                    cancel: cancel.clone(),
                    done: done.clone(),
                };
                let worker_state = state.clone();
                let session = session.clone();
                let target = target.clone();
                let worker =
                    thread::Builder::new()
                        .name("lo-http-request".into())
                        .spawn(move || {
                            let result: Result<(), Fault> = (|| {
                                let read_stream =
                                    stream.try_clone().map_err(|e| host(e.to_string()))?;
                                let mut wire = Wire {
                                    stream: &mut stream,
                                    reader: std::io::BufReader::new(read_stream),
                                };
                                loop {
                                    active.store(false, Ordering::Release);
                                    if cancel.load(Ordering::Acquire) {
                                        return Ok(());
                                    }
                                    let keep = handle_request(
                                        &mut wire,
                                        &session,
                                        &target,
                                        active.clone(),
                                        cancel.clone(),
                                    )?;
                                    if !keep {
                                        return Ok(());
                                    }
                                }
                            })();
                            let _ = stream.shutdown(Shutdown::Both);
                            done.store(true, Ordering::Release);
                            if let Err(fault) = result {
                                let mut current = worker_state.lock().unwrap();
                                if current.failure.is_none()
                                    && !(current.stopping
                                        && matches!(
                                            fault.primary(),
                                            Fault::RawCancellation
                                                | Fault::Host("runtime.cancelled", _)
                                        )
                                        && !has_progress(&fault))
                                {
                                    current.failure = Some(fault)
                                }
                            }
                        });
                match worker {
                    Ok(worker) => {
                        current.connections.push(tracked);
                        current.workers.push(worker)
                    }
                    Err(error) => {
                        current.failure = Some(host(error.to_string()));
                        return;
                    }
                }
            }
            Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                thread::sleep(Duration::from_millis(10))
            }
            Err(error) => {
                let mut current = state.lock().unwrap();
                if !current.stopping && current.failure.is_none() {
                    current.failure = Some(host(format!("HTTP.listen: {error}")))
                }
                return;
            }
        }
    }
}

fn emit_response(
    status: u16,
    headers: Vec<(String, Vec<u8>)>,
    body: &[u8],
    head: bool,
    keep: bool,
    http10: bool,
) -> Result<(Vec<u8>, bool), Fault> {
    let mut fields: BTreeMap<String, Vec<Vec<u8>>> = BTreeMap::new();
    for (name, value) in headers {
        fields.entry(name).or_default().push(value);
    }
    let mut trailers: BTreeMap<String, Vec<Vec<u8>>> = BTreeMap::new();
    for declaration in fields.get("trailer").into_iter().flatten() {
        for name in declaration.split(|byte| *byte == b',') {
            let name = String::from_utf8_lossy(name.trim_ascii()).to_ascii_lowercase();
            if name.starts_with("if-")
                || matches!(
                    name.as_str(),
                    "authorization"
                        | "cache-control"
                        | "connection"
                        | "content-encoding"
                        | "content-length"
                        | "content-range"
                        | "content-type"
                        | "expect"
                        | "host"
                        | "keep-alive"
                        | "max-forwards"
                        | "pragma"
                        | "proxy-authenticate"
                        | "proxy-authorization"
                        | "proxy-connection"
                        | "range"
                        | "realm"
                        | "te"
                        | "trailer"
                        | "transfer-encoding"
                        | "www-authenticate"
                )
            {
                continue;
            }
            if let Some(values) = fields.get(&name) {
                trailers
                    .entry(name)
                    .or_default()
                    .extend(values.iter().cloned());
            }
        }
    }
    let first = |name: &str| fields.get(name).and_then(|values| values.first()).cloned();
    let transfer = first("transfer-encoding").unwrap_or_default();
    let has_transfer = !transfer.is_empty();
    let transfer_text = String::from_utf8_lossy(&transfer);
    let declared = first("content-length")
        .and_then(|value| std::str::from_utf8(&value).ok()?.parse::<i64>().ok())
        .filter(|value| *value >= 0)
        .map(|value| value as usize);
    let declared_present = first("content-length").is_some();
    let content_encoded = first("content-encoding").is_some_and(|value| !value.is_empty());
    let mut keep =
        keep && !first("connection").is_some_and(|value| value.eq_ignore_ascii_case(b"close"));
    let body_allowed = ![204, 304].contains(&status);
    if !head && declared.is_some_and(|length| body.len() > length) {
        return Err(host(
            "HTTP.respond: http: wrote more than the declared Content-Length",
        ));
    }
    if declared_present
        && declared.is_none()
        && !first("content-length").is_some_and(|value| value.is_empty())
    {
        fields.remove("content-length");
    }
    let mut length = declared;
    if body_allowed
        && !has_transfer
        && !fields.contains_key("content-length")
        && !fields.contains_key("trailer")
        && !head
        && body.len() <= 2048
    {
        length = Some(body.len());
        fields.insert(
            "content-length".into(),
            vec![body.len().to_string().into_bytes()],
        );
    }
    if body_allowed
        && !head
        && !content_encoded
        && !fields.contains_key("content-type")
        && !has_transfer
        && !body.is_empty()
    {
        fields.insert(
            "content-type".into(),
            vec![super::sniff::detect(body).as_bytes().to_vec()],
        );
    }
    if !body_allowed {
        fields.remove("content-length");
        fields.remove("transfer-encoding");
        length = None;
        if status == 304 {
            fields.remove("content-type");
        }
    }
    if length.is_some() && has_transfer && transfer_text != "identity" {
        fields.remove("content-length");
        length = None;
    }
    let chunked = if head || !body_allowed {
        fields.remove("transfer-encoding");
        false
    } else if length.is_some() {
        fields.remove("transfer-encoding");
        false
    } else if http10 || transfer_text == "identity" {
        fields.remove("transfer-encoding");
        keep = false;
        false
    } else {
        if has_transfer && transfer_text != "chunked" {
            fields
                .entry("transfer-encoding".into())
                .or_default()
                .push(b"chunked".to_vec());
        } else {
            fields.insert("transfer-encoding".into(), vec![b"chunked".to_vec()]);
        }
        fields.remove("content-length");
        true
    };
    let close_header = !keep;
    if !head && body_allowed && declared.is_some_and(|length| length != body.len()) {
        keep = false;
    }
    if http10 && keep && !fields.contains_key("connection") {
        fields.insert("connection".into(), vec![b"keep-alive".to_vec()]);
    }
    if close_header && !http10 {
        fields.insert("connection".into(), vec![b"close".to_vec()]);
    }
    if !fields.contains_key("date") {
        fields.insert("date".into(), vec![http_date()?.into_bytes()]);
    }
    let mut content = format!(
        "HTTP/1.{} {status} {}\r\n",
        if http10 { 0 } else { 1 },
        reason(status)
    )
    .into_bytes();
    for (name, values) in fields {
        for value in values {
            content.extend(name.as_bytes());
            content.extend(b": ");
            content.extend(value);
            content.extend(b"\r\n");
        }
    }
    content.extend(b"\r\n");
    if !head && body_allowed {
        if chunked {
            if !body.is_empty() {
                content.extend(format!("{:x}\r\n", body.len()).as_bytes());
                content.extend(body);
                content.extend(b"\r\n");
            }
            content.extend(b"0\r\n");
            for (name, values) in trailers {
                for value in values {
                    content.extend(name.as_bytes());
                    content.extend(b": ");
                    content.extend(value);
                    content.extend(b"\r\n");
                }
            }
            content.extend(b"\r\n");
        } else {
            content.extend(body);
        }
    }
    Ok((content, keep))
}
fn http_date() -> Result<String, Fault> {
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|_| host("HTTP response date is before its epoch"))?
        .as_secs();
    let days = (now / 86400) as i64;
    let within = now % 86400;
    let z = days + 719468;
    let era = z / 146097;
    let day = z - era * 146097;
    let year = (day - day / 1460 + day / 36524 - day / 146096) / 365;
    let day = day - (365 * year + year / 4 - year / 100);
    let month = (5 * day + 2) / 153;
    let date = day - (153 * month + 2) / 5 + 1;
    let month = month + if month < 10 { 3 } else { -9 };
    let year = year + era * 400 + if month <= 2 { 1 } else { 0 };
    Ok(format!(
        "{}, {:02} {} {:04} {:02}:{:02}:{:02} GMT",
        ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"][((days + 4) % 7) as usize],
        date,
        ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
            [(month - 1) as usize],
        year,
        within / 3600,
        (within % 3600) / 60,
        within % 60
    ))
}
