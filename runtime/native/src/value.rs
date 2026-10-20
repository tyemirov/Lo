use base64::{engine::general_purpose::STANDARD, Engine};
use num_bigint::BigInt;
use serde_json::{json, Value as Json};
use std::{
    cmp::Ordering,
    collections::BTreeMap,
    sync::{
        atomic::{AtomicU64, Ordering as AtomicOrder},
        Arc,
    },
};
pub static LIVE_VALUES: AtomicU64 = AtomicU64::new(0);
pub type Handle = Arc<Value>;
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Type {
    Atom(String),
    Nominal(String, Box<Type>),
    List(Box<Type>),
    Map(Box<Type>, Box<Type>),
    Range(Box<Type>),
    Fallible(Box<Type>),
    Record(Vec<(String, Type)>),
}
impl Type {
    pub fn parse(s: &str) -> Result<Self, String> {
        Self::parse_depth(s, 0)
    }
    fn parse_depth(s: &str, depth: u32) -> Result<Self, String> {
        if depth > 256 {
            return Err("type nesting limit".into());
        }
        fn split(s: &str) -> Vec<&str> {
            let mut depth = 0;
            let mut start = 0;
            let mut out = vec![];
            for (i, c) in s.char_indices() {
                match c {
                    '<' | '{' => depth += 1,
                    '>' | '}' => depth -= 1,
                    ',' if depth == 0 => {
                        out.push(s[start..i].trim());
                        start = i + 1
                    }
                    _ => (),
                }
            }
            out.push(s[start..].trim());
            out
        }
        if let Some(s) = s.strip_suffix('!') {
            let base = Self::parse_depth(s, depth + 1)?;
            if matches!(base, Self::Fallible(_)) {
                return Err("invalid fallible base".into());
            }
            return Ok(Self::Fallible(Box::new(base)));
        }
        if s.starts_with("Record{") && s.ends_with('}') {
            let body = &s[7..s.len() - 1];
            let mut fields = vec![];
            if !body.is_empty() {
                for f in split(body) {
                    let (n, t) = f.split_once(": ").ok_or("invalid record field")?;
                    if n.is_empty() || fields.iter().any(|(x, _)| x == n) {
                        return Err("invalid record field".into());
                    }
                    fields.push((n.into(), Self::parse_depth(t, depth + 1)?));
                }
            }
            if fields.windows(2).any(|p| p[0].0 >= p[1].0) {
                return Err("unordered record type".into());
            }
            return Ok(Self::Record(fields));
        }
        for kind in [
            "Parameters",
            "Gradients",
            "Optimizer.SGD.State",
            "Optimizer.AdamW.State",
        ] {
            let prefix = format!("{kind}<");
            if s.starts_with(&prefix) && s.ends_with('>') {
                let schema = Self::parse_depth(&s[prefix.len()..s.len() - 1], depth + 1)?;
                let Self::Record(fields) = &schema else {
                    return Err("training schema requires exact record".into());
                };
                if fields.is_empty() || fields.iter().any(|(_, t)| *t != atom("Tensor<FP32>")) {
                    return Err("training schema requires nonempty tensor fields".into());
                }
                return Ok(Self::Nominal(kind.into(), Box::new(schema)));
            }
        }
        for p in ["List<", "Range<", "Map<"] {
            if s.starts_with(p) && s.ends_with('>') {
                let parts = split(&s[p.len()..s.len() - 1]);
                return match (p, parts.as_slice()) {
                    ("List<", [a]) => Ok(Self::List(Box::new(Self::parse_depth(a, depth + 1)?))),
                    ("Range<", [a]) => {
                        let t = Self::parse_depth(a, depth + 1)?;
                        if !matches!(t.kind(), "Int" | "String" | "Scalar") {
                            return Err("invalid range element".into());
                        }
                        Ok(Self::Range(Box::new(t)))
                    }
                    ("Map<", [a, b]) => {
                        let key = Self::parse_depth(a, depth + 1)?;
                        if !matches!(key.kind(), "Int" | "String" | "Scalar" | "Bool" | "Null") {
                            return Err("invalid map key".into());
                        }
                        Ok(Self::Map(
                            Box::new(key),
                            Box::new(Self::parse_depth(b, depth + 1)?),
                        ))
                    }
                    _ => Err("invalid collection type".into()),
                };
            }
        }
        if matches!(
            s,
            "Int"
                | "String"
                | "Bool"
                | "Null"
                | "Scalar"
                | "Bytes"
                | "Float"
                | "Error"
                | "Record"
                | "File.Path"
                | "Tensor<FP32>"
                | "Device"
                | "Optimizer.SGD.Config"
                | "Optimizer.AdamW.Config"
                | "File.Write"
                | "File.WriteReceipt"
        ) {
            Ok(Self::Atom(s.into()))
        } else {
            Err(format!("unsupported native type {s}"))
        }
    }
    pub fn name(&self) -> String {
        match self {
            Self::Atom(a) => a.clone(),
            Self::Nominal(kind, schema) => format!("{kind}<{}>", schema.name()),
            Self::List(a) => format!("List<{}>", a.name()),
            Self::Range(a) => format!("Range<{}>", a.name()),
            Self::Map(a, b) => format!("Map<{}, {}>", a.name(), b.name()),
            Self::Fallible(a) => format!("{}!", a.name()),
            Self::Record(fs) => format!(
                "Record{{{}}}",
                fs.iter()
                    .map(|(n, t)| format!("{n}: {}", t.name()))
                    .collect::<Vec<_>>()
                    .join(", ")
            ),
        }
    }
    pub fn kind(&self) -> &str {
        match self {
            Self::Atom(a) => a,
            Self::Nominal(kind, _) => kind,
            Self::List(_) => "List",
            Self::Map(_, _) => "Map",
            Self::Range(_) => "Range",
            Self::Fallible(_) => "Fallible",
            Self::Record(_) => "ExactRecord",
        }
    }
    pub fn base(&self) -> Type {
        if let Self::Fallible(t) = self {
            *t.clone()
        } else {
            self.clone()
        }
    }
}
#[derive(Clone)]
pub enum Data {
    Tensor(crate::tensor::Tensor),
    Device(String),
    Wrapped(Handle),
    FileWrite { path: Handle, content: Handle },
    Int(BigInt),
    Float(f64),
    Text(String),
    Bool(bool),
    Null,
    Bytes(Vec<u8>),
    List(Vec<Handle>),
    Record(BTreeMap<String, Handle>),
    Map(Vec<(Handle, Handle)>),
    Range(Handle, Handle, bool, bool),
    Success(Handle),
    Failure(Handle),
    Error(Error),
    Flow(usize),
}
#[derive(Clone)]
pub struct Error {
    pub kind: String,
    pub message: String,
    pub input: Handle,
    pub trace: Vec<Handle>,
    pub flow_progress: Vec<Handle>,
    pub site: u64,
    pub operation: String,
}
pub struct Value {
    pub typ: Type,
    pub data: Data,
    pub weight: u64,
}
impl Drop for Value {
    fn drop(&mut self) {
        LIVE_VALUES.fetch_sub(1, AtomicOrder::Relaxed);
    }
}
pub fn atom(s: &str) -> Type {
    Type::Atom(s.into())
}
fn add_weight(a: u64, b: u64) -> u64 {
    a.saturating_add(b).min(1 << 62)
}
fn sum_weight(it: impl Iterator<Item = u64>) -> u64 {
    it.fold(0, add_weight)
}
pub fn new(typ: Type, data: Data) -> Handle {
    let weight = 16
        + match &data {
            Data::Tensor(t) => t.weight() - 16,
            Data::Device(s) => s.len() as u64,
            Data::Wrapped(contents) => contents.weight,
            Data::FileWrite { path, content } => {
                "File.Write.new".len() as u64 + path.weight + content.weight
            }
            Data::Int(n) => (n.bits() + 7) / 8,
            Data::Float(_) => 8,
            Data::Text(s) => s.len() as u64,
            Data::Bool(_) => 1,
            Data::Null | Data::Flow(_) => 0,
            Data::Bytes(b) => b.len() as u64,
            Data::List(v) => sum_weight(v.iter().map(|x| x.weight)),
            Data::Record(v) => {
                sum_weight(v.iter().map(|(n, x)| add_weight(n.len() as u64, x.weight)))
            }
            Data::Map(v) => sum_weight(v.iter().map(|(a, b)| add_weight(a.weight, b.weight))),
            Data::Range(a, b, _, _) => add_weight(a.weight, b.weight) + 2,
            Data::Success(v) | Data::Failure(v) => v.weight,
            Data::Error(e) => sum_weight(
                [
                    e.kind.len() as u64,
                    e.message.len() as u64,
                    e.input.weight,
                    16,
                ]
                .into_iter()
                .chain(e.trace.iter().chain(&e.flow_progress).map(|x| x.weight)),
            ),
        };
    LIVE_VALUES.fetch_add(1, AtomicOrder::Relaxed);
    Arc::new(Value { typ, data, weight })
}
pub fn int(n: impl Into<BigInt>) -> Handle {
    new(atom("Int"), Data::Int(n.into()))
}
pub fn text(s: impl Into<String>) -> Handle {
    new(atom("String"), Data::Text(s.into()))
}
pub fn boolean(b: bool) -> Handle {
    new(atom("Bool"), Data::Bool(b))
}
pub fn record(fields: impl IntoIterator<Item = (String, Handle)>) -> Handle {
    let fs: BTreeMap<_, _> = fields.into_iter().collect();
    let typ = Type::Record(fs.iter().map(|(n, v)| (n.clone(), v.typ.clone())).collect());
    new(typ, Data::Record(fs))
}
pub fn view(v: &Handle) -> Handle {
    new(atom("Record"), v.data.clone())
}
pub fn success(t: &Type, v: Handle) -> Handle {
    if matches!(t, Type::Fallible(_)) {
        new(t.clone(), Data::Success(v))
    } else {
        v
    }
}
pub fn failure(t: &Type, e: Handle) -> Handle {
    new(t.clone(), Data::Failure(e))
}
impl Value {
    pub fn integer(&self) -> &BigInt {
        if let Data::Int(v) = &self.data {
            v
        } else {
            std::process::abort()
        }
    }
    pub fn string(&self) -> &str {
        if let Data::Text(v) = &self.data {
            v
        } else {
            std::process::abort()
        }
    }
    pub fn number(&self) -> f64 {
        if let Data::Float(v) = &self.data {
            *v
        } else {
            std::process::abort()
        }
    }
    pub fn bool(&self) -> bool {
        if let Data::Bool(v) = &self.data {
            *v
        } else {
            std::process::abort()
        }
    }
    pub fn items(&self) -> &[Handle] {
        if let Data::List(v) = &self.data {
            v
        } else {
            std::process::abort()
        }
    }
    pub fn fields(&self) -> &BTreeMap<String, Handle> {
        if let Data::Record(v) = &self.data {
            v
        } else if let Data::Wrapped(v) = &self.data {
            v.fields()
        } else {
            std::process::abort()
        }
    }
    pub fn json(&self) -> Json {
        let data = match &self.data {
            Data::Tensor(t) => {
                json!({"dtype":"fp32","shape":t.shape.dims.iter().map(|d|d.to_string()).collect::<Vec<_>>(),"device":t.storage.location,"values":t.storage.values.iter().map(|n|format_float32(*n)).collect::<Vec<_>>()})
            }
            Data::Device(d) => json!(d),
            Data::Wrapped(v) => Json::Object(
                v.fields()
                    .iter()
                    .map(|(n, x)| (n.clone(), x.json()))
                    .collect(),
            ),
            Data::FileWrite { path, content } => {
                json!({"operation":"File.Write.new","file_path":path.json(),"file_content":content.json()})
            }
            Data::Int(v) => json!(v.to_string()),
            Data::Float(v) => json!(format_float(*v)),
            Data::Text(v) => json!(v),
            Data::Bool(v) => json!(v),
            Data::Null => Json::Null,
            Data::Bytes(v) => json!(STANDARD.encode(v)),
            Data::List(v) => json!(v.iter().map(|x| x.json()).collect::<Vec<_>>()),
            Data::Record(v) => Json::Object(v.iter().map(|(n, x)| (n.clone(), x.json())).collect()),
            Data::Map(v) => json!(v
                .iter()
                .map(|(k, v)| json!({"key":k.json(),"value":v.json()}))
                .collect::<Vec<_>>()),
            Data::Range(a, b, c, d) => {
                json!({"start":a.json(),"end":b.json(),"include_start":c,"include_end":d})
            }
            Data::Success(v) => {
                return json!({"type":self.typ.name(),"status":"ok","value":v.json()})
            }
            Data::Failure(e) => {
                return json!({"type":self.typ.name(),"status":"error","error":e.json()})
            }
            Data::Error(e) => {
                let mut out = json!({"type":"Error","kind":e.kind,"message":e.message,"input":e.input.json(),"trace":e.trace.iter().map(|x|x.json()).collect::<Vec<_>>()});
                if !e.flow_progress.is_empty() {
                    out["flow_progress"] =
                        json!(e.flow_progress.iter().map(|x| x.json()).collect::<Vec<_>>())
                }
                return out;
            }
            Data::Flow(_) => std::process::abort(),
        };
        json!({"type":self.typ.name(),"value":data})
    }
    pub fn canonical_json(&self) -> String {
        fn quote(s: &str) -> String {
            serde_json::to_string(s)
                .unwrap()
                .replace('&', "\\u0026")
                .replace('<', "\\u003c")
                .replace('>', "\\u003e")
                .replace('\u{2028}', "\\u2028")
                .replace('\u{2029}', "\\u2029")
        }
        fn list(items: &[Handle]) -> String {
            format!(
                "[{}]",
                items
                    .iter()
                    .map(|v| v.canonical_json())
                    .collect::<Vec<_>>()
                    .join(",")
            )
        }
        let data = match &self.data {
            Data::Tensor(t) => format!(
                "{{\"dtype\":\"fp32\",\"shape\":{},\"device\":{},\"values\":{}}}",
                canonical_json_value(&json!(t
                    .shape
                    .dims
                    .iter()
                    .map(|d| d.to_string())
                    .collect::<Vec<_>>())),
                quote(&t.storage.location),
                canonical_json_value(&json!(t
                    .storage
                    .values
                    .iter()
                    .map(|n| format_float32(*n))
                    .collect::<Vec<_>>()))
            ),
            Data::Device(d) => quote(d),
            Data::Wrapped(v) => format!(
                "{{{}}}",
                v.fields()
                    .iter()
                    .map(|(n, v)| format!("{}:{}", quote(n), v.canonical_json()))
                    .collect::<Vec<_>>()
                    .join(",")
            ),
            Data::FileWrite { path, content } => format!(
                "{{\"operation\":\"File.Write.new\",\"file_path\":{},\"file_content\":{}}}",
                path.canonical_json(),
                content.canonical_json()
            ),
            Data::Int(n) => quote(&n.to_string()),
            Data::Float(n) => quote(&format_float(*n)),
            Data::Text(s) => quote(s),
            Data::Bool(v) => v.to_string(),
            Data::Null => "null".into(),
            Data::Bytes(b) => quote(&STANDARD.encode(b)),
            Data::List(v) => list(v),
            Data::Record(v) => format!(
                "{{{}}}",
                v.iter()
                    .map(|(n, v)| format!("{}:{}", quote(n), v.canonical_json()))
                    .collect::<Vec<_>>()
                    .join(",")
            ),
            Data::Map(v) => format!(
                "[{}]",
                v.iter()
                    .map(|(k, v)| format!(
                        "{{\"key\":{},\"value\":{}}}",
                        k.canonical_json(),
                        v.canonical_json()
                    ))
                    .collect::<Vec<_>>()
                    .join(",")
            ),
            Data::Range(a, b, c, d) => format!(
                "{{\"start\":{},\"end\":{},\"include_start\":{},\"include_end\":{}}}",
                a.canonical_json(),
                b.canonical_json(),
                c,
                d
            ),
            Data::Success(v) => {
                return format!(
                    "{{\"type\":{},\"status\":\"ok\",\"value\":{}}}",
                    quote(&self.typ.name()),
                    v.canonical_json()
                )
            }
            Data::Failure(e) => {
                return format!(
                    "{{\"type\":{},\"status\":\"error\",\"error\":{}}}",
                    quote(&self.typ.name()),
                    e.canonical_json()
                )
            }
            Data::Error(e) => {
                return format!(
                    "{{\"type\":\"Error\",\"kind\":{},\"message\":{},\"input\":{},\"trace\":{}{}}}",
                    quote(&e.kind),
                    quote(&e.message),
                    e.input.canonical_json(),
                    list(&e.trace),
                    if e.flow_progress.is_empty() {
                        String::new()
                    } else {
                        format!(",\"flow_progress\":{}", list(&e.flow_progress))
                    }
                )
            }
            Data::Flow(_) => std::process::abort(),
        };
        format!(
            "{{\"type\":{},\"value\":{}}}",
            quote(&self.typ.name()),
            data
        )
    }
    pub fn output(&self) -> String {
        match &self.data {
            Data::Int(v) => v.to_string(),
            Data::Float(v) => format_float(*v),
            Data::Text(v) => v.clone(),
            Data::Bool(v) => v.to_string(),
            Data::Success(v) => v.output(),
            _ => self.canonical_json(),
        }
    }
}
pub fn format_float(n: f64) -> String {
    // Rust and Go both use shortest round-trippable decimal digits. Match Go's exponent policy.
    if n == 0.0 {
        return if n.is_sign_negative() {
            "-0".into()
        } else {
            "0".into()
        };
    }
    let abs = n.abs();
    if abs < 1e-4 || abs >= 1e6 {
        let s = format!("{n:e}");
        let (m, e) = s.split_once('e').unwrap();
        let e: i32 = e.parse().unwrap();
        format!("{m}e{}{e:02}", if e >= 0 { "+" } else { "-" }, e = e.abs())
    } else {
        n.to_string()
    }
}
pub fn compare(a: &Handle, b: &Handle) -> Ordering {
    match (&a.data, &b.data) {
        (Data::Int(a), Data::Int(b)) => a.cmp(b),
        (Data::Text(a), Data::Text(b)) => a.cmp(b),
        (Data::Bool(a), Data::Bool(b)) => a.cmp(b),
        (Data::Null, Data::Null) => Ordering::Equal,
        _ => std::process::abort(),
    }
}
pub fn equal(a: &Handle, b: &Handle) -> bool {
    a.typ == b.typ && compare(a, b) == Ordering::Equal
}
pub fn contains(r: &Handle, v: &Handle) -> bool {
    if let Data::Range(a, b, sa, sb) = &r.data {
        let x = compare(v, a);
        let y = compare(v, b);
        (x.is_gt() || x.is_eq() && *sa) && (y.is_lt() || y.is_eq() && *sb)
    } else {
        std::process::abort()
    }
}
pub fn canonical_json_value(value: &Json) -> String {
    match value {
        Json::Null => "null".into(),
        Json::Bool(b) => b.to_string(),
        Json::Number(n) => n.to_string(),
        Json::String(s) => serde_json::to_string(s)
            .unwrap()
            .replace('&', "\\u0026")
            .replace('<', "\\u003c")
            .replace('>', "\\u003e")
            .replace('\u{2028}', "\\u2028")
            .replace('\u{2029}', "\\u2029"),
        Json::Array(a) => format!(
            "[{}]",
            a.iter()
                .map(canonical_json_value)
                .collect::<Vec<_>>()
                .join(",")
        ),
        Json::Object(object) => {
            let ordered: &[&str] = if object.get("type").is_some_and(Json::is_string) {
                if object["type"] == "Error" {
                    &["type", "kind", "message", "input", "trace", "flow_progress"]
                } else if object.contains_key("status") {
                    &["type", "status", "value", "error"]
                } else {
                    &["type", "value"]
                }
            } else if object.get("dtype").is_some_and(Json::is_string)
                && object.get("shape").is_some_and(Json::is_array)
                && object.get("device").is_some_and(Json::is_string)
                && object.get("values").is_some_and(Json::is_array)
            {
                &["dtype", "shape", "device", "values"]
            } else if object
                .get("operation")
                .is_some_and(|v| v == "File.Write.new")
                && object.contains_key("file_path")
            {
                &["operation", "file_path", "file_content"]
            } else if object.get("code").is_some_and(Json::is_string)
                && object.get("stage").is_some_and(Json::is_string)
            {
                &[
                    "code",
                    "stage",
                    "message",
                    "span",
                    "expected",
                    "actual",
                    "related",
                    "error",
                    "flow_progress",
                    "last_completed_activation",
                    "cleanup_errors",
                ]
            } else if object.get("file").is_some_and(Json::is_string)
                && object.contains_key("start")
            {
                &["file", "start", "end"]
            } else if object.get("offset").is_some_and(Json::is_number)
                && object.contains_key("line")
            {
                &["offset", "line", "column"]
            } else if object.get("include_start").is_some_and(Json::is_boolean) {
                &["start", "end", "include_start", "include_end"]
            } else {
                &[]
            };
            let mut fields = vec![];
            for key in ordered {
                if let Some(v) = object.get(*key) {
                    fields.push(format!(
                        "{}:{}",
                        canonical_json_value(&json!(key)),
                        canonical_json_value(v)
                    ))
                }
            }
            for (key, v) in object {
                if !ordered.contains(&key.as_str()) {
                    fields.push(format!(
                        "{}:{}",
                        canonical_json_value(&json!(key)),
                        canonical_json_value(v)
                    ))
                }
            }
            format!("{{{}}}", fields.join(","))
        }
    }
}

pub fn format_float32(n: f32) -> String {
    if n == 0.0 {
        return if n.is_sign_negative() {
            "-0".into()
        } else {
            "0".into()
        };
    }
    let a = n.abs();
    if a < 1e-4 || a >= 1e6 {
        let s = format!("{n:e}");
        let (m, e) = s.split_once('e').unwrap();
        let e: i32 = e.parse().unwrap();
        format!("{m}e{}{e:02}", if e >= 0 { "+" } else { "-" }, e = e.abs())
    } else {
        n.to_string()
    }
}
pub fn snapshot(v: &Handle) -> Handle {
    match &v.data {
        Data::Tensor(t) => record([
            ("tensor_type".into(), text("Tensor<FP32>")),
            ("dtype".into(), text("fp32")),
            (
                "shape".into(),
                new(
                    Type::List(Box::new(atom("Int"))),
                    Data::List(t.shape.dims.iter().map(|d| int(*d)).collect()),
                ),
            ),
            ("device".into(), text(&t.storage.location)),
        ]),
        Data::Wrapped(contents) => snapshot(contents),
        Data::Record(fields) => {
            let out = record(fields.iter().map(|(n, v)| (n.clone(), snapshot(v))));
            if v.typ == atom("Record") {
                view(&out)
            } else {
                out
            }
        }
        Data::List(items) => {
            let out: Vec<_> = items.iter().map(snapshot).collect();
            let element = out
                .first()
                .map(|v| v.typ.clone())
                .unwrap_or_else(|| snapshot_type(&v.typ).base_list());
            new(Type::List(Box::new(element)), Data::List(out))
        }
        Data::Map(entries) => new(
            snapshot_type(&v.typ),
            Data::Map(
                entries
                    .iter()
                    .map(|(k, v)| (k.clone(), snapshot(v)))
                    .collect(),
            ),
        ),
        Data::Success(x) => {
            let x = snapshot(x);
            success(&Type::Fallible(Box::new(x.typ.clone())), x)
        }
        Data::Failure(e) => failure(&snapshot_type(&v.typ), e.clone()),
        _ => v.clone(),
    }
}
impl Type {
    fn base_list(&self) -> Type {
        if let Self::List(t) = self {
            *t.clone()
        } else {
            std::process::abort()
        }
    }
}
fn snapshot_type(t: &Type) -> Type {
    match t {
        Type::Nominal(_, schema) => snapshot_type(schema),
        Type::List(e) => Type::List(Box::new(snapshot_type(e))),
        Type::Map(k, v) => Type::Map(k.clone(), Box::new(snapshot_type(v))),
        Type::Record(fs) => Type::Record(
            fs.iter()
                .map(|(n, t)| (n.clone(), snapshot_type(t)))
                .collect(),
        ),
        Type::Fallible(t) => Type::Fallible(Box::new(snapshot_type(t))),
        _ if t == &atom("Tensor<FP32>") => Type::Record(vec![
            ("device".into(), atom("String")),
            ("dtype".into(), atom("String")),
            ("shape".into(), Type::List(Box::new(atom("Int")))),
            ("tensor_type".into(), atom("String")),
        ]),
        _ if t == &atom("Optimizer.SGD.Config") || t == &atom("Optimizer.AdamW.Config") => {
            let mut fields: Vec<(String, Type)> = vec![("learning_rate".into(), atom("Float"))];
            if t == &atom("Optimizer.AdamW.Config") {
                for n in ["beta1", "beta2", "epsilon", "weight_decay"] {
                    fields.push((n.into(), atom("Float")))
                }
            }
            fields.sort_by(|a, b| a.0.cmp(&b.0));
            Type::Record(fields)
        }
        _ => t.clone(),
    }
}
