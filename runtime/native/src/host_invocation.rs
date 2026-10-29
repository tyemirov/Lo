use crate::{context::*, value::*};
use serde_json::{json, Value as Json};
use std::{
    collections::BTreeMap,
    ffi::OsString,
    path::Path,
    time::{SystemTime, UNIX_EPOCH},
};

pub const OPERATION: &str = "Host.Invocation.current";
const INVOCATION: &str = "Host.Invocation";
const FAILED: &str = "Host.InvocationFailed";
const MAX_ARGUMENTS: usize = 4096;
const MAX_ARGUMENT_BYTES: usize = 65536;
const MAX_TRANSPORT: usize = 1048576;

#[derive(Clone)]
pub struct Value {
    fields: Handle,
    argument_count: usize,
    transport_weight: u64,
}
impl Value {
    pub fn weight(&self) -> u64 {
        self.fields.weight + self.transport_weight
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        Some(self.fields.fields())
    }
    pub fn json(&self) -> Json {
        let fields = self.fields.fields();
        json!({"argument_count":self.argument_count,"real_uid":fields["real_uid"].json(),"effective_uid":fields["effective_uid"].json(),"now_unix_ns":fields["now_unix_ns"].json(),"redacted":true})
    }
    pub fn canonical_json(&self) -> String {
        let fields = self.fields.fields();
        format!("{{\"argument_count\":{},\"real_uid\":{},\"effective_uid\":{},\"now_unix_ns\":{},\"redacted\":true}}",self.argument_count,fields["real_uid"].canonical_json(),fields["effective_uid"].canonical_json(),fields["now_unix_ns"].canonical_json())
    }
    pub fn snapshot(&self) -> Self {
        let fields = self.fields.fields();
        Self {
            fields: record([
                (
                    "arguments".into(),
                    new(Type::List(Box::new(atom("String"))), Data::List(vec![])),
                ),
                (
                    "home".into(),
                    new(atom("File.Path"), Data::Text("/".into())),
                ),
                ("real_uid".into(), fields["real_uid"].clone()),
                ("effective_uid".into(), fields["effective_uid"].clone()),
                ("now_unix_ns".into(), fields["now_unix_ns"].clone()),
            ]),
            argument_count: self.argument_count,
            transport_weight: 0,
        }
    }
}

pub struct CapturedInputs {
    pub process: Result<Handle, String>,
    pub host: Result<Handle, String>,
}
pub fn capture_inputs(arguments: &[String]) -> CapturedInputs {
    let environment: Vec<_> = std::env::vars_os().collect();
    let process = crate::process::capture_context_with_environment(&environment);
    let host = capture(arguments, &environment);
    CapturedInputs { process, host }
}
fn capture(arguments: &[String], environment: &[(OsString, OsString)]) -> Result<Handle, String> {
    let home = environment
        .iter()
        .find(|(name, _)| name == "HOME")
        .ok_or("capture host home: missing HOME")?
        .1
        .to_str()
        .ok_or("capture host home: invalid text")?;
    let now = match SystemTime::now().duration_since(UNIX_EPOCH) {
        Ok(duration) => i128::try_from(duration.as_nanos())
            .map_err(|_| "capture host clock: outside the signed 64-bit range")?,
        Err(error) => -i128::try_from(error.duration().as_nanos())
            .map_err(|_| "capture host clock: outside the signed 64-bit range")?,
    };
    let now =
        i64::try_from(now).map_err(|_| "capture host clock: outside the signed 64-bit range")?;
    construct(
        arguments,
        home,
        unsafe { libc::getuid() },
        unsafe { libc::geteuid() },
        now,
    )
}

pub fn validate_arguments(arguments: &[String]) -> Result<(), String> {
    if arguments.len() > MAX_ARGUMENTS {
        return Err("program arguments exceed the count limit".into());
    }
    let mut size = 4usize;
    for argument in arguments {
        if argument.contains('\0') {
            return Err("program argument requires valid UTF-8 without NUL".into());
        }
        if argument.len() + 1 > MAX_ARGUMENT_BYTES {
            return Err("program argument exceeds the byte limit".into());
        }
        if argument.len() + 5 > MAX_TRANSPORT - size {
            return Err("program argument vector exceeds the byte limit".into());
        }
        size += argument.len() + 5;
    }
    Ok(())
}
fn construct(
    arguments: &[String],
    home: &str,
    real_uid: u32,
    effective_uid: u32,
    now: i64,
) -> Result<Handle, String> {
    validate_arguments(arguments)?;
    if !Path::new(home).is_absolute() || home.contains('\0') {
        return Err("capture host home: require an absolute UTF-8 path without NUL".into());
    }
    let size = 4
        + 4
        + home.len()
        + 1
        + 4
        + 4
        + 8
        + arguments
            .iter()
            .map(|argument| 4 + argument.len() + 1)
            .sum::<usize>();
    if size > MAX_TRANSPORT {
        return Err("capture host snapshot: exceeds the transport limit".into());
    }
    let fields = record([
        (
            "arguments".into(),
            new(
                Type::List(Box::new(atom("String"))),
                Data::List(arguments.iter().map(text).collect()),
            ),
        ),
        (
            "home".into(),
            new(atom("File.Path"), Data::Text(home.into())),
        ),
        ("real_uid".into(), int(real_uid)),
        ("effective_uid".into(), int(effective_uid)),
        ("now_unix_ns".into(), int(now)),
    ]);
    Ok(new(
        atom(INVOCATION),
        Data::Adapter(crate::adapters::AdapterValue::Host(Value {
            fields,
            argument_count: arguments.len(),
            transport_weight: size as u64,
        })),
    ))
}

pub fn decode(bytes: &[u8]) -> Result<Handle, String> {
    if bytes.len() > MAX_TRANSPORT {
        return Err("decode host snapshot: transport limit".into());
    }
    struct Decoder<'a> {
        bytes: &'a [u8],
        offset: usize,
    }
    impl<'a> Decoder<'a> {
        fn integer(&mut self) -> Result<u32, String> {
            if self.bytes.len() - self.offset < 4 {
                return Err("decode host snapshot: truncated integer".into());
            }
            let result =
                u32::from_be_bytes(self.bytes[self.offset..self.offset + 4].try_into().unwrap());
            self.offset += 4;
            Ok(result)
        }
        fn string(&mut self) -> Result<String, String> {
            let length = self.integer()? as usize;
            if length == 0 || length > self.bytes.len() - self.offset {
                return Err("decode host snapshot: invalid text length".into());
            }
            let value = &self.bytes[self.offset..self.offset + length];
            self.offset += length;
            if value[length - 1] != 0 || value[..length - 1].contains(&0) {
                return Err("decode host snapshot: invalid text".into());
            }
            std::str::from_utf8(&value[..length - 1])
                .map(str::to_owned)
                .map_err(|_| "decode host snapshot: invalid text".into())
        }
    }
    let mut decoder = Decoder { bytes, offset: 0 };
    let count = decoder.integer()? as usize;
    if count > MAX_ARGUMENTS {
        return Err("decode host snapshot: argument count limit".into());
    }
    let mut arguments = Vec::with_capacity(count);
    for _ in 0..count {
        arguments.push(decoder.string()?)
    }
    let home = decoder.string()?;
    let real_uid = decoder.integer()?;
    let effective_uid = decoder.integer()?;
    if bytes.len() - decoder.offset != 8 {
        return Err("decode host snapshot: clock or trailing bytes".into());
    }
    let now = i64::from_be_bytes(bytes[decoder.offset..].try_into().unwrap());
    construct(&arguments, &home, real_uid, effective_uid, now)
}

pub fn admit(site: &Site) -> Result<(), String> {
    if site.operation != OPERATION
        || !site.arguments.is_empty()
        || site.typ != Type::Fallible(Box::new(atom(INVOCATION)))
    {
        return Err("host invocation metadata type mismatch".into());
    }
    Ok(())
}
pub fn execute(context: &mut Context, site: &Site) -> Outcome {
    context
        .session
        .initial_host_invocation
        .clone()
        .map_err(|message| Fault::Recoverable(error(site, FAILED, &message, record([]))))
}
