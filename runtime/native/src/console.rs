use crate::{context::*, value::*};
use base64::{engine::general_purpose::STANDARD, Engine};
use serde_json::{json, Value as Json};
use std::{
    collections::BTreeMap,
    ffi::c_void,
    fs::File,
    sync::{Mutex, TryLockError},
    time::{Duration, Instant},
};
#[path = "console_process.rs"]
mod transport;
pub const OPERATION: &str = "Console.Line.new";
pub const MAX_RECORD: usize = 1048576;
#[derive(Clone)]
pub struct Value {
    stream: String,
    bytes: Vec<u8>,
    length: usize,
    flush: bool,
}
impl Value {
    pub fn weight(&self) -> u64 {
        (self.stream.len() + self.bytes.len().max(1) + 9) as u64
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        None
    }
    pub fn json(&self) -> Json {
        json!({"stream":self.stream,"bytes":self.length,"flush":self.flush,"redacted":true})
    }
    pub fn canonical_json(&self) -> String {
        format!(
            "{{\"stream\":{},\"bytes\":{},\"flush\":{},\"redacted\":true}}",
            serde_json::to_string(&self.stream).unwrap(),
            self.length,
            self.flush
        )
    }
    pub fn snapshot(&self) -> Self {
        Self {
            stream: self.stream.clone(),
            bytes: vec![],
            length: self.length,
            flush: self.flush,
        }
    }
}
pub type WriteCallback =
    unsafe extern "C" fn(*mut c_void, u32, *const u8, usize, *mut usize) -> i32;
pub type FlushCallback = unsafe extern "C" fn(*mut c_void, u32) -> i32;
struct Callbacks {
    enabled: u32,
    userdata: usize,
    write: WriteCallback,
    flush: FlushCallback,
}
enum Sink {
    Missing,
    Cli(transport::Image),
    Callback(Callbacks),
}
struct State {
    charged: u64,
    maximum: u64,
    json: bool,
    serving: bool,
    closed: bool,
}
pub struct Console {
    sink: Sink,
    state: Mutex<State>,
    reporting_deadline: Mutex<Option<Instant>>,
}
impl Console {
    pub fn empty() -> Self {
        Self {
            sink: Sink::Missing,
            state: Mutex::new(State {
                charged: 0,
                maximum: MAX_RECORD as u64,
                json: false,
                serving: false,
                closed: false,
            }),
            reporting_deadline: Mutex::new(None),
        }
    }
    pub fn unconfigured(&self) -> bool {
        matches!(self.sink, Sink::Missing)
    }
    pub fn cli() -> Result<Self, String> {
        Ok(Self {
            sink: Sink::Cli(transport::Image::bind()?),
            ..Self::empty()
        })
    }
    pub fn callbacks(
        enabled: u32,
        userdata: *mut c_void,
        write: WriteCallback,
        flush: FlushCallback,
        maximum: u64,
    ) -> Self {
        Self {
            sink: Sink::Callback(Callbacks {
                enabled,
                userdata: userdata as usize,
                write,
                flush,
            }),
            state: Mutex::new(State {
                maximum,
                ..Self::empty().state.into_inner().unwrap()
            }),
            reporting_deadline: Mutex::new(None),
        }
    }
    pub fn configure(&self, maximum: u64, json: bool, serving: bool) {
        let mut state = self.state.lock().unwrap();
        state.maximum = maximum;
        state.json = json;
        state.serving = serving;
    }
    pub fn image_paths(&self) -> Vec<String> {
        match &self.sink {
            Sink::Cli(image) => image.paths.clone(),
            _ => vec![],
        }
    }
    pub fn protect_name(&self, parent: &File, name: &str) -> Result<(), Fault> {
        match &self.sink {
            Sink::Cli(image) => image.registry.protect_name(parent, name),
            _ => Ok(()),
        }
    }
    pub fn protect_file(&self, file: &File) -> Result<(), Fault> {
        match &self.sink {
            Sink::Cli(image) => image.registry.protect_file(file),
            _ => Ok(()),
        }
    }
    pub fn protect_removal(&self, parent: &File, name: &str) -> Result<(), Fault> {
        match &self.sink {
            Sink::Cli(image) => image.registry.protect_removal(parent, name),
            _ => Ok(()),
        }
    }
    fn lock(
        &self,
        check: &mut impl FnMut() -> Result<(), Fault>,
    ) -> Result<std::sync::MutexGuard<'_, State>, Fault> {
        loop {
            check()?;
            match self.state.try_lock() {
                Ok(v) => return Ok(v),
                Err(TryLockError::Poisoned(_)) => return Err(host("Console ownership poisoned")),
                Err(TryLockError::WouldBlock) => std::thread::sleep(Duration::from_millis(1)),
            }
        }
    }
    fn reserve(state: &mut State, bytes: usize) -> Result<(), Fault> {
        let total = state
            .charged
            .checked_add(bytes as u64)
            .ok_or_else(|| limit("Console logical bytes"))?;
        if state.maximum != 0 && total > state.maximum {
            return Err(limit("Console logical bytes"));
        }
        state.charged = total;
        Ok(())
    }
    fn encoded(state: &State, stream: &str, bytes: &[u8], flush: bool) -> Vec<u8> {
        if !state.json {
            return bytes.to_vec();
        }
        format!("{{\"command\":\"{}\",\"event\":\"console\",\"stream\":{},\"encoding\":\"base64\",\"data\":\"{}\",\"bytes\":{},\"flush\":{}}}\n",if state.serving{"serve"}else{"run"},serde_json::to_string(stream).unwrap(),STANDARD.encode(bytes),bytes.len(),flush).into_bytes()
    }
    fn authorize(&self, stream: &str) -> Result<(), Fault> {
        let destination = if stream == "stdout" { 1 } else { 2 };
        match &self.sink {
            Sink::Missing => Err(Fault::Host(
                "runtime.capability-denied",
                format!("Console.commit {stream:?} has no host capability"),
            )),
            Sink::Callback(callbacks) if callbacks.enabled & destination == 0 => Err(Fault::Host(
                "runtime.capability-denied",
                format!("Console.commit {stream:?} has no host capability"),
            )),
            _ => Ok(()),
        }
    }
    fn write(
        &self,
        stream: &str,
        bytes: &[u8],
        flush: bool,
        check: &mut impl FnMut() -> Result<(), Fault>,
    ) -> Result<(), WriteFailure> {
        let destination = if stream == "stdout" { 1 } else { 2 };
        match &self.sink {
            Sink::Missing => Err(WriteFailure::Terminal(Fault::Host(
                "runtime.capability-denied",
                "Console destination is not enabled".into(),
            ))),
            Sink::Cli(image) => image.write(destination, bytes, check),
            Sink::Callback(callbacks) => {
                if callbacks.enabled & destination == 0 {
                    return Err(WriteFailure::Terminal(Fault::Host(
                        "runtime.capability-denied",
                        "Console destination is not enabled".into(),
                    )));
                }
                let mut offset = 0;
                while offset < bytes.len() {
                    check().map_err(WriteFailure::Terminal)?;
                    let mut confirmed = 0;
                    let code = unsafe {
                        (callbacks.write)(
                            callbacks.userdata as *mut c_void,
                            destination,
                            bytes[offset..].as_ptr(),
                            bytes.len() - offset,
                            &mut confirmed,
                        )
                    };
                    if confirmed > bytes.len() - offset
                        || !(0..=3).contains(&code)
                        || (code == 0 && confirmed == 0)
                    {
                        return Err(WriteFailure::Terminal(host(
                            "Console callback protocol failure",
                        )));
                    }
                    offset += confirmed;
                    match code {
                        2 | 3 => return Err(WriteFailure::Recoverable(offset, "write")),
                        1 => std::thread::sleep(Duration::from_millis(1)),
                        _ => (),
                    }
                }
                if flush {
                    loop {
                        check().map_err(WriteFailure::Terminal)?;
                        match unsafe {
                            (callbacks.flush)(callbacks.userdata as *mut c_void, destination)
                        } {
                            0 => break,
                            1 => std::thread::sleep(Duration::from_millis(1)),
                            2 | 3 => return Err(WriteFailure::Recoverable(offset, "flush")),
                            _ => {
                                return Err(WriteFailure::Terminal(host(
                                    "Console flush callback protocol failure",
                                )))
                            }
                        }
                    }
                }
                check().map_err(WriteFailure::Terminal)
            }
        }
    }
    pub fn line(&self, context: &Context, site: &Site, line: &Value) -> Outcome {
        let mut check = || context.check_cancel();
        let mut state = self.lock(&mut check)?;
        if state.closed {
            return Err(host("Console is closed"));
        }
        self.authorize(&line.stream)?;
        Self::reserve(&mut state, line.length)?;
        let encoded = Self::encoded(&state, &line.stream, &line.bytes, line.flush);
        self.write(&line.stream, &encoded, line.flush, &mut check)
            .map_err(|failure| failure.fault(site, &line.stream))?;
        Ok(new(
            atom("Console.Receipt"),
            Data::Wrapped(record([
                ("stream".into(), text(&line.stream)),
                ("bytes_written".into(), int(line.length)),
                ("flushed".into(), boolean(line.flush)),
            ])),
        ))
    }
    pub fn terminal(
        &self,
        context: &Context,
        value: &crate::program_exit::Value,
    ) -> Result<(), Fault> {
        let mut check = || context.check_cancel();
        let mut state = self.lock(&mut check)?;
        if state.closed {
            return Err(host("Console is closed"));
        }
        if !value.stdout().is_empty() {
            self.authorize("stdout")?;
        }
        if !value.stderr().is_empty() {
            self.authorize("stderr")?;
        }
        Self::reserve(&mut state, value.stdout().len() + value.stderr().len())?;
        for (stream, bytes) in [("stdout", value.stdout()), ("stderr", value.stderr())] {
            if !bytes.is_empty() {
                let encoded = Self::encoded(&state, stream, bytes, true);
                self.write(stream, &encoded, true, &mut check)
                    .map_err(|failure| failure.terminal())?;
            }
        }
        Ok(())
    }
    pub fn raw(&self, stream: &str, bytes: &[u8]) -> Result<(), Fault> {
        let deadline = {
            let mut deadline = self.reporting_deadline.lock().unwrap();
            *deadline.get_or_insert_with(|| Instant::now() + Duration::from_secs(1))
        };
        let mut check = || {
            if Instant::now() >= deadline {
                Err(Fault::RawCancellation)
            } else {
                Ok(())
            }
        };
        let _state = self.lock(&mut check)?;
        self.raw_write(stream, bytes, &mut check)
    }
    pub fn raw_completion(&self, stream: &str, bytes: &[u8]) -> Result<(), Fault> {
        let deadline = Instant::now() + Duration::from_secs(1);
        let mut check = || {
            if Instant::now() >= deadline {
                Err(Fault::RawCancellation)
            } else {
                Ok(())
            }
        };
        let _state = self.lock(&mut check)?;
        self.raw_write(stream, bytes, &mut check)
    }
    pub fn raw_context(&self, context: &Context, stream: &str, bytes: &[u8]) -> Result<(), Fault> {
        let mut check = || context.check_cancel();
        let _state = self.lock(&mut check)?;
        self.raw_write(stream, bytes, &mut check)
    }

    fn raw_write(
        &self,
        stream: &str,
        bytes: &[u8],
        check: &mut impl FnMut() -> Result<(), Fault>,
    ) -> Result<(), Fault> {
        if bytes.is_empty() {
            return Ok(());
        }
        if let Sink::Cli(image) = &self.sink {
            let destination = if stream == "stdout" { 1 } else { 2 };
            return image
                .stream(destination, bytes, check)
                .map_err(|failure| failure.terminal());
        }
        for chunk in bytes.chunks(transport::FRAME_BYTES) {
            self.write(stream, chunk, true, check)
                .map_err(|failure| failure.terminal())?;
        }
        Ok(())
    }

    pub fn close(&self) -> Result<(), Fault> {
        self.state.lock().unwrap().closed = true;
        match &self.sink {
            Sink::Cli(image) => image.registry.close().map_err(host),
            _ => Ok(()),
        }
    }
}
pub(super) enum WriteFailure {
    Terminal(Fault),
    Recoverable(usize, &'static str),
}
impl WriteFailure {
    fn terminal(self) -> Fault {
        match self {
            Self::Terminal(f) => f,
            Self::Recoverable(n, stage) => {
                host(format!("Console output failed after {n} bytes at {stage}"))
            }
        }
    }
    fn fault(self, site: &Site, stream: &str) -> Fault {
        match self {
            Self::Terminal(f) => f,
            Self::Recoverable(n, stage) => Fault::Recoverable(error(
                site,
                "Console.WriteFailed",
                "console output failed",
                record([
                    ("stream".into(), text(stream)),
                    ("bytes_written".into(), int(n)),
                    ("stage".into(), text(stage)),
                ]),
            )),
        }
    }
}
pub fn admit(site: &Site) -> Result<(), String> {
    let types: Vec<_> = site
        .arguments
        .iter()
        .map(|a| match a {
            Argument::Value(t) => t.name(),
            _ => String::new(),
        })
        .collect();
    if types != ["String", "String", "Bool"]
        || site.typ != Type::Fallible(Box::new(atom("Console.Line")))
    {
        return Err("Console.Line metadata type mismatch".into());
    }
    Ok(())
}
pub fn execute(_context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let stream = args[0].string();
    let message = args[1].string();
    if !["stdout", "stderr"].contains(&stream) {
        return Err(Fault::Recoverable(error(
            site,
            "Console.InvalidLine",
            "Console destination must be stdout or stderr",
            record([]),
        )));
    }
    if message.len() >= MAX_RECORD {
        return Err(Fault::Recoverable(error(
            site,
            "Console.InvalidLine",
            "Console line must be valid UTF-8 and fit the record limit",
            record([]),
        )));
    }
    let mut bytes = message.as_bytes().to_vec();
    bytes.push(b'\n');
    let length = bytes.len();
    Ok(new(
        atom("Console.Line"),
        Data::Adapter(crate::adapters::AdapterValue::Console(Value {
            stream: stream.into(),
            bytes,
            length,
            flush: args[2].bool(),
        })),
    ))
}
pub fn commit(context: &mut Context, site: &Site, target: &Handle) -> Outcome {
    let Data::Adapter(crate::adapters::AdapterValue::Console(value)) = &target.data else {
        return Err(host("invalid Console target"));
    };
    let mut activation = site.clone();
    activation.operation = "Console.write".into();
    let receipt = context.session.console.line(context, &activation, value)?;
    context.last_activation = Some(
        json!({"operation":"Console.write","span":site.source_record(),"receipt":receipt.json()}),
    );
    context.charge(&receipt)?;
    Ok(receipt)
}
pub fn worker() -> i32 {
    transport::worker()
}
