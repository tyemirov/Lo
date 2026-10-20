use crate::{action::Action, value::*};
use serde_json::Value as Json;
use std::{
    cell::Cell,
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    },
    thread::{self, ThreadId},
};
pub static LIVE_CONTEXTS: AtomicU64 = AtomicU64::new(0);
pub const MAX_BYTES: usize = 16 << 20;
pub const MAX_ITEMS: usize = 1_000_000;
pub const MAX_BITS: u64 = 1_048_576;
pub type Outcome = Result<Handle, Fault>;
#[derive(Clone)]
pub enum Fault {
    RawCancellation,
    Budget { primary: String, message: String },
    Numeric(&'static str, String),
    Recoverable(Handle),
    Host(&'static str, String),
}
pub fn boundary_host_fault(fault: Fault) -> Fault {
    match fault {
        Fault::RawCancellation => Fault::Host(
            "runtime.cancelled",
            "execution cancelled: context canceled".into(),
        ),
        Fault::Budget { primary, message } => Fault::Budget {
            message: format!("{primary}: {message}"),
            primary,
        },
        Fault::Host(code, message) => Fault::Host(
            code,
            format!(
                "{}: {message}",
                if code == "runtime.cancelled" {
                    "execution cancelled"
                } else {
                    "boundary operation failed"
                }
            ),
        ),
        other => other,
    }
}
pub fn host(s: impl Into<String>) -> Fault {
    Fault::Host("runtime.host-failure", s.into())
}
pub fn limit(s: &str) -> Fault {
    Fault::Host("runtime.limit", format!("execution limit: {s}"))
}
#[derive(Clone)]
pub enum Argument {
    Value(Type),
    Flow(usize),
}
#[derive(Clone)]
pub struct Site {
    pub action: Action,
    pub raw: Json,
    pub typ: Type,
    pub id: u64,
    pub operation: String,
    pub labels: Vec<String>,
    pub flow: usize,
    pub arguments: Vec<Argument>,
}
#[derive(Clone)]
pub struct Flow {
    pub handler: Option<usize>,
    pub name: String,
    pub parameters: Vec<(String, Type)>,
    pub output: Type,
}
pub struct Program {
    pub derivatives: Vec<crate::autodiff::Plan>,
    pub sites: Vec<Site>,
    pub flows: Vec<Flow>,
    pub has_result: bool,
}
pub struct Context {
    pub files: crate::files::Files,
    pub last_activation: Option<Json>,
    pub resources: crate::resources::Ledger,
    pub device_grant: String,
    pub tapes: Vec<crate::autodiff::Tape>,
    pub program: Arc<Program>,
    pub owner: ThreadId,
    pub remaining: u64,
    pub weight: u64,
    pub depth: u32,
    pub fault: Option<Fault>,
    pub scopes: u64,
    pub cancelled: bool,
    pub cancel_polls: Cell<u64>,
    pub cancel_after_polls: Option<u64>,
    pub flow_progress: Vec<Handle>,
    pub flow_fault_progress: bool,
}
fn uint(v: &Json, key: &str) -> Result<usize, String> {
    v[key]
        .as_u64()
        .and_then(|n| usize::try_from(n).ok())
        .ok_or_else(|| format!("invalid metadata {key}"))
}
fn string(v: &Json, key: &str) -> Result<String, String> {
    v[key]
        .as_str()
        .map(str::to_owned)
        .ok_or_else(|| format!("invalid metadata {key}"))
}
impl Program {
    pub fn parse(data: &[u8], flow_count: usize) -> Result<Self, String> {
        let v: Json = serde_json::from_slice(data).map_err(|e| format!("program metadata: {e}"))?;
        if v["abi"] != "lo-native-1" {
            return Err("program ABI identity mismatch".into());
        }
        if !v["source"]["path"].is_string()
            || v["source"]["sha256"].as_str().map_or(true, |s| {
                s.len() != 64 || !s.bytes().all(|c| c.is_ascii_hexdigit())
            })
        {
            return Err("invalid program source metadata".into());
        }
        let mut types = vec![];
        for (i, t) in v["types"]
            .as_array()
            .ok_or("absent metadata types")?
            .iter()
            .enumerate()
        {
            if uint(t, "id")? != i {
                return Err("noncanonical type identifier".into());
            }
            let typ = Type::parse(&string(t, "name")?)?;
            if typ.kind() != string(t, "kind")? {
                return Err("type kind mismatch".into());
            }
            types.push(typ)
        }
        let mut sites = vec![];
        for (i, s) in v["locations"]
            .as_array()
            .ok_or("absent metadata locations")?
            .iter()
            .enumerate()
        {
            if uint(s, "id")? != i {
                return Err("noncanonical location identifier".into());
            }
            let t = types
                .get(uint(s, "type")?)
                .ok_or("unknown site type")?
                .clone();
            let labels = s["labels"]
                .as_array()
                .ok_or("invalid site labels")?
                .iter()
                .map(|x| {
                    x.as_str()
                        .map(str::to_owned)
                        .ok_or("invalid label".to_string())
                })
                .collect::<Result<Vec<_>, _>>()?;
            let mut arguments = vec![];
            for a in s["arguments"]
                .as_array()
                .ok_or("absent argument metadata")?
            {
                let t = uint(a, "type")?;
                let f = uint(a, "flow")?;
                arguments.push(match string(a, "kind")?.as_str() {
                    "value" if f == 0 => {
                        Argument::Value(types.get(t).ok_or("unknown argument type")?.clone())
                    }
                    "flow" if t == 0 && f < flow_count => Argument::Flow(f),
                    _ => return Err("invalid argument metadata".into()),
                });
            }
            let action = Action::parse(&string(s, "action")?)?;
            let _ = string(s, "file")?;
            let _ = string(s, "owner")?;
            for key in ["text", "float_bits", "field"] {
                string(s, key)?;
            }
            for key in ["start", "end"] {
                for coordinate in ["offset", "line", "column"] {
                    let n = uint(&s[key], coordinate)?;
                    if coordinate != "offset" && n == 0 {
                        return Err("invalid location coordinate".into());
                    }
                }
            }
            if uint(&s["end"], "offset")? < uint(&s["start"], "offset")?
                || uint(&s["end"], "line")? < uint(&s["start"], "line")?
            {
                return Err("invalid source span".into());
            }
            sites.push(Site {
                action,
                raw: s.clone(),
                typ: t,
                id: i as u64,
                operation: string(s, "operation")?,
                labels,
                arguments,
                flow: uint(s, "flow")?,
            });
        }
        let flows_json = v["flows"].as_array().ok_or("absent metadata flows")?;
        if flows_json.len() != flow_count {
            return Err("flow table count mismatch".into());
        }
        let mut flows = vec![];
        for (i, f) in flows_json.iter().enumerate() {
            if uint(f, "id")? != i || uint(f, "location")? >= sites.len() {
                return Err("invalid flow identity".into());
            }
            let output = types
                .get(uint(f, "output")?)
                .ok_or("unknown flow output")?
                .clone();
            let mut parameters = vec![];
            for p in f["parameters"]
                .as_array()
                .ok_or("invalid flow parameters")?
            {
                let name = string(p, "name")?;
                if parameters.iter().any(|(n, _)| n == &name) {
                    return Err("duplicate flow parameter".into());
                }
                parameters.push((
                    name,
                    types
                        .get(uint(p, "type")?)
                        .ok_or("unknown parameter type")?
                        .clone(),
                ));
            }
            if !f["handler"].is_null()
                && f["handler"]
                    .as_u64()
                    .map_or(true, |h| h >= flow_count as u64)
            {
                return Err("invalid flow handler".into());
            }
            flows.push(Flow {
                handler: f["handler"].as_u64().map(|h| h as usize),
                name: string(f, "name")?,
                parameters,
                output,
            });
        }
        for f in flows_json {
            if let Some(handler) = f["handler"].as_u64() {
                let h = &flows[handler as usize];
                let original = &flows[uint(f, "id")?];
                if h.parameters.len() != 1
                    || h.parameters[0].1 != atom("Error")
                    || h.output != original.output
                {
                    return Err("invalid handler signature".into());
                }
            }
        }
        let derivatives = crate::autodiff::parse(&v["derivatives"], &types, &sites)?;
        for site in &sites {
            if site.action == Action::FlowRef && site.flow >= flow_count {
                return Err("invalid flow reference".into());
            }
            if !site.action.is_derivative()
                && (site.action == Action::Operation) != !site.operation.is_empty()
            {
                return Err("native operation action identity mismatch".into());
            }
            if site.action != Action::Operation
                && !site.action.is_derivative()
                && (!site.arguments.is_empty() || !site.labels.is_empty())
            {
                return Err("nonoperation contains operation metadata".into());
            }
            let valid_type = match site.action {
                Action::Body
                | Action::StatementStep
                | Action::ScopeEnter
                | Action::FlowEnter
                | Action::FlowRef => site.typ == atom("Null"),
                Action::Match | Action::Predicate => site.typ == atom("Bool"),
                Action::ConvertSuccess | Action::ConvertFailure => {
                    matches!(site.typ, Type::Fallible(_))
                }
                Action::ConvertRecordView => site.typ == atom("Record"),
                Action::ConvertScalarListView => site.typ == Type::List(Box::new(atom("Scalar"))),
                Action::ConvertFallibleRecordView => {
                    site.typ == Type::Fallible(Box::new(atom("Record")))
                }
                Action::Literal => matches!(
                    site.typ.kind(),
                    "Int" | "Float" | "String" | "Bool" | "Null"
                ),
                _ => true,
            };
            if !valid_type {
                return Err("invalid native action result type".into());
            }
            if site.action == Action::Literal {
                let t = site.raw["text"].as_str().unwrap();
                let valid = match site.typ.kind() {
                    "Int" => {
                        let digits = t
                            .strip_prefix('-')
                            .or_else(|| t.strip_prefix('+'))
                            .unwrap_or(t);
                        !digits.is_empty() && digits.bytes().all(|c| c.is_ascii_digit())
                    }
                    "Bool" => t == "true" || t == "false",
                    "Null" => t == "null",
                    "Float" => {
                        let bits = site.raw["float_bits"].as_str().unwrap();
                        bits.len() == 16
                            && u64::from_str_radix(bits, 16)
                                .is_ok_and(|b| f64::from_bits(b).is_finite())
                    }
                    "String" => true,
                    _ => false,
                };
                if !valid {
                    return Err("invalid native literal metadata".into());
                }
            }
            if site.action == Action::Project && site.raw["field"].as_str().unwrap().is_empty() {
                return Err("invalid projection metadata".into());
            }
            if site.action.is_derivative() {
                crate::autodiff::admit_site(site, &derivatives)?;
            }
            if site.action == Action::Operation {
                crate::operations::admit(site)?;
                if matches!(
                    site.operation.as_str(),
                    "List.map"
                        | "List.filter"
                        | "List.fold"
                        | "List.flat_map"
                        | "List.unfold"
                        | "Flow.fold"
                ) {
                    crate::collections::admit(site, &flows)?;
                }
            }
        }
        Ok(Self {
            derivatives,
            sites,
            flows,
            has_result: v["has_result"]
                .as_bool()
                .ok_or("invalid program result presence")?,
        })
    }
}
impl Context {
    pub fn new(program: Arc<Program>, steps: u64) -> Self {
        LIVE_CONTEXTS.fetch_add(1, Ordering::Relaxed);
        Self {
            files: crate::files::empty(),
            last_activation: None,
            resources: crate::resources::Ledger::new(),
            device_grant: String::new(),
            tapes: vec![],
            program,
            owner: thread::current().id(),
            remaining: steps,
            weight: 0,
            depth: 0,
            fault: None,
            scopes: 0,
            cancelled: false,
            cancel_polls: Cell::new(0),
            cancel_after_polls: None,
            flow_progress: vec![],
            flow_fault_progress: false,
        }
    }
    pub fn entry(&self) -> Result<(), Fault> {
        if self.owner != thread::current().id() {
            Err(host("context belongs to another thread"))
        } else if let Some(fault) = &self.fault {
            Err(fault.clone())
        } else {
            Ok(())
        }
    }
    pub fn step(&mut self) -> Result<(), Fault> {
        self.check_cancel()?;
        if self.remaining == 0 {
            return Err(limit("evaluation steps"));
        }
        self.remaining -= 1;
        Ok(())
    }
    pub fn check_cancel(&self) -> Result<(), Fault> {
        self.entry()?;
        let polls = self.cancel_polls.get().saturating_add(1);
        self.cancel_polls.set(polls);
        if self.cancelled
            || crate::cli::cancelled()
            || self.cancel_after_polls.is_some_and(|limit| polls >= limit)
        {
            Err(Fault::Host(
                "runtime.cancelled",
                "execution cancelled: context canceled".into(),
            ))
        } else {
            Ok(())
        }
    }
    pub fn tensor_budget(&mut self, steps: u64, weight: u64) -> Result<(), Fault> {
        self.check_cancel()?;
        if weight > MAX_BYTES as u64 {
            return Err(limit("expanded tensor result bytes"));
        }
        if weight > (64 << 20) - self.weight {
            return Err(limit("evaluation tensor result bytes"));
        }
        if steps > self.remaining {
            return Err(limit("tensor work steps"));
        }
        self.remaining -= steps;
        Ok(())
    }
    pub fn charge(&mut self, v: &Handle) -> Result<(), Fault> {
        if v.weight > MAX_BYTES as u64 || v.weight > (64 << 20) - self.weight {
            let e = match &v.data {
                Data::Failure(e) => Some(e),
                Data::Error(_) => Some(v),
                _ => None,
            };
            if let Some(e) = e {
                if let Data::Error(e) = &e.data {
                    if !e.flow_progress.is_empty() {
                        self.flow_progress = e.flow_progress.clone();
                        self.flow_fault_progress = true;
                    }
                }
            }
            return Err(limit(if v.weight > MAX_BYTES as u64 {
                "expanded value bytes"
            } else {
                "evaluation value bytes"
            }));
        }
        self.weight += v.weight;
        Ok(())
    }
    pub fn record_flow_progress(
        &mut self,
        total: usize,
        completed: usize,
        state: &Handle,
    ) -> Result<Handle, Fault> {
        self.check_cancel()?;
        let progress = record([
            ("operation".into(), text("Flow.fold")),
            ("total_count".into(), int(total)),
            ("completed_count".into(), int(completed)),
            ("next_index".into(), int(completed)),
            ("state".into(), crate::value::snapshot(state)),
        ]);
        if progress.weight > MAX_BYTES as u64 {
            return Err(limit("expanded flow progress bytes"));
        }
        if progress.weight > (64 << 20) - self.weight {
            return Err(limit("evaluation flow progress bytes"));
        }
        self.weight += progress.weight;
        self.flow_progress = vec![progress.clone()];
        self.flow_fault_progress = false;
        Ok(progress)
    }
    pub fn site(&self, id: u64) -> Result<Site, Fault> {
        self.program
            .sites
            .get(id as usize)
            .cloned()
            .ok_or_else(|| host("invalid source location"))
    }
}
pub fn error(site: &Site, kind: &str, message: &str, input: Handle) -> Handle {
    new(
        atom("Error"),
        Data::Error(Error {
            kind: kind.into(),
            message: message.into(),
            input: view(&crate::value::snapshot(&input)),
            trace: vec![],
            flow_progress: vec![],
            site: site.id,
            operation: site.operation.clone(),
        }),
    )
}
pub fn operation_failure(site: &Site, args: &[Handle], kind: &str, message: &str) -> Handle {
    let names = crate::operations::parameter_names(&site.operation);
    let input = record(args.iter().enumerate().map(|(i, v)| {
        (
            names
                .get(i)
                .map(|s| (*s).to_owned())
                .unwrap_or_else(|| format!("argument_{i}")),
            v.clone(),
        )
    }));
    failure(&site.typ, error(site, kind, message, input))
}
pub fn cause(v: &Handle) -> Option<Handle> {
    match &v.data {
        Data::Failure(e) => Some(e.clone()),
        _ => None,
    }
}
pub fn frame(e: &Handle, flow: &str, operation: &str, input: Handle) -> Handle {
    let Data::Error(original) = &e.data else {
        std::process::abort()
    };
    let mut out = original.clone();
    let fields = record(
        input
            .fields()
            .iter()
            .map(|(n, v)| (n.clone(), record([("type".into(), text(v.typ.name()))]))),
    );
    out.trace.push(record([
        ("flow".into(), text(flow)),
        ("operation".into(), text(operation)),
        ("input".into(), view(&fields)),
    ]));
    new(atom("Error"), Data::Error(out))
}

impl Drop for Context {
    fn drop(&mut self) {
        LIVE_CONTEXTS.fetch_sub(1, Ordering::Relaxed);
    }
}
