pub use crate::adapters::Session;
use crate::metadata::{Document, Fields, SourceSpan};
use crate::{action::Action, value::*};
use serde_json::Value as Json;
use std::{
    cell::Cell,
    sync::{
        atomic::{AtomicBool, AtomicU64, Ordering},
        Arc,
    },
    thread::{self, ThreadId},
    time::Instant,
};
pub static LIVE_PROGRAMS: AtomicU64 = AtomicU64::new(0);
pub static LIVE_CONTEXTS: AtomicU64 = AtomicU64::new(0);
pub const MAX_BYTES: usize = 16 << 20;
pub const MAX_ITEMS: usize = 1_000_000;
pub const MAX_BITS: u64 = 1_048_576;
pub type Outcome = Result<Handle, Fault>;
#[derive(Clone)]
pub enum Fault {
    Boundary {
        primary: Box<Fault>,
        details: Box<BoundaryDetails>,
    },
    RawCancellation,
    Budget {
        primary: String,
        message: String,
    },
    Numeric(&'static str, String),
    Recoverable(Handle),
    Host(&'static str, String),
}
#[derive(Clone, Default)]
pub struct BoundaryDetails {
    pub context_captured: bool,
    pub process_progress: Option<Json>,
    pub http_progress: Option<Json>,
    pub write_progress: Option<Json>,
    pub removal_progress: Option<Json>,
    pub removal_entries: Option<Json>,
    pub last_completed_activation: Option<Json>,
    pub flow_progress: Vec<Handle>,
    pub cleanup_errors: Vec<String>,
    pub cleanup_messages: Vec<String>,
}
impl BoundaryDetails {
    pub fn merge(&mut self, other: Self) {
        if other.context_captured {
            self.context_captured = true;
            self.last_completed_activation = other.last_completed_activation;
            self.flow_progress = other.flow_progress;
        } else {
            if other.last_completed_activation.is_some() {
                self.last_completed_activation = other.last_completed_activation;
            }
            if !other.flow_progress.is_empty() {
                self.flow_progress = other.flow_progress;
            }
        }
        if other.process_progress.is_some() {
            self.process_progress = other.process_progress;
        }
        if other.http_progress.is_some() {
            self.http_progress = other.http_progress;
        }
        if other.write_progress.is_some() {
            self.write_progress = other.write_progress;
        }
        if other.removal_entries.is_some() {
            self.removal_entries = other.removal_entries;
        }
        if other.removal_progress.is_some() {
            self.removal_progress = other.removal_progress;
        }
        self.cleanup_errors.extend(other.cleanup_errors);
        self.cleanup_messages.extend(other.cleanup_messages);
    }
}
impl Fault {
    pub fn primary(&self) -> &Self {
        match self {
            Self::Boundary { primary, .. } => primary.primary(),
            other => other,
        }
    }
    pub fn with_details(self, extra: BoundaryDetails) -> Self {
        match self {
            Self::Boundary {
                primary,
                mut details,
            } => {
                details.merge(extra);
                Self::Boundary { primary, details }
            }
            primary => Self::Boundary {
                primary: Box::new(primary),
                details: Box::new(extra),
            },
        }
    }
    pub fn message(&self) -> String {
        match self {
            Self::RawCancellation => "execution cancelled: context canceled".into(),
            Self::Budget { message, .. } | Self::Numeric(_, message) | Self::Host(_, message) => {
                message.clone()
            }
            Self::Recoverable(e) => match &e.data {
                Data::Error(e) => format!("{}: {}", e.kind, e.message),
                _ => std::process::abort(),
            },
            Self::Boundary { primary, details } => {
                let mut message = primary.message();
                for cleanup in &details.cleanup_messages {
                    message.push_str("; cleanup: ");
                    message.push_str(cleanup);
                }
                message
            }
        }
    }
}
pub fn combine_cleanup(primary: Option<Fault>, cleanup: Option<Fault>) -> Option<Fault> {
    match (primary, cleanup) {
        (None, other) | (other, None) => other,
        (Some(primary), Some(cleanup)) => {
            let message = cleanup.message();
            let (primary, original) = match primary {
                Fault::Boundary { primary, details } => (*primary, *details),
                other => (other, BoundaryDetails::default()),
            };
            let mut details = match cleanup {
                Fault::Boundary { details, .. } => *details,
                _ => BoundaryDetails::default(),
            };
            details.cleanup_messages.clear();
            details.merge(original);
            details.cleanup_errors.push(message.clone());
            details.cleanup_messages.push(message);
            Some(primary.with_details(details))
        }
    }
}
pub fn boundary_host_fault(fault: Fault) -> Fault {
    match fault {
        Fault::Boundary { primary, details } => {
            boundary_host_fault(*primary).with_details(*details)
        }
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
pub struct Site(Arc<SiteData>);
impl std::ops::Deref for Site {
    type Target = SiteData;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}
impl std::ops::DerefMut for Site {
    fn deref_mut(&mut self) -> &mut Self::Target {
        Arc::make_mut(&mut self.0)
    }
}
#[derive(Clone)]
pub enum Literal {
    Value(Handle),
    Limit(&'static str),
}
#[derive(Clone)]
pub struct SiteData {
    pub literal: Option<Literal>,
    pub action: Action,
    pub span: Arc<SourceSpan>,
    pub owner: String,
    pub payload: SitePayload,
    pub typ: Type,
    pub id: u64,
    pub operation: String,
    pub labels: Vec<String>,
    pub flow: usize,
    pub arguments: Vec<Argument>,
}
#[derive(Clone)]
pub struct DerivativeOrigin {
    pub plan: usize,
    pub node: usize,
    pub frame: usize,
}
#[derive(Clone)]
pub enum SitePayload {
    Empty,
    Body(DerivativeOrigin),
    Literal {
        text: String,
        float_bits: Option<String>,
    },
    Project(String),
    Operation,
    Derivative(DerivativeOrigin),
}
impl SiteData {
    pub fn source_record(&self) -> Json {
        self.span.source_record()
    }
    pub fn derivative_origin(&self) -> Option<&DerivativeOrigin> {
        match &self.payload {
            SitePayload::Body(origin) | SitePayload::Derivative(origin) => Some(origin),
            _ => None,
        }
    }
    pub fn derivative(&self) -> &DerivativeOrigin {
        match self.derivative_origin() {
            Some(origin) => origin,
            None => std::process::abort(),
        }
    }
    pub fn project_field(&self) -> &str {
        match &self.payload {
            SitePayload::Project(field) => field,
            _ => std::process::abort(),
        }
    }
    fn literal_source(&self) -> (&str, Option<&str>) {
        match &self.payload {
            SitePayload::Literal { text, float_bits } => (text, float_bits.as_deref()),
            _ => std::process::abort(),
        }
    }
}
#[derive(Clone)]
pub struct Flow(Arc<FlowData>);
impl std::ops::Deref for Flow {
    type Target = FlowData;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}
pub struct FlowData {
    pub identity: String,
    pub handler: Option<usize>,
    pub name: String,
    pub parameters: Vec<(String, Type)>,
    pub output: Type,
}
struct ProgramLifetime;
pub struct Program {
    _lifetime: ProgramLifetime,
    control_null: Handle,
    pub artifact: Option<Json>,
    pub derivatives: Vec<crate::autodiff::Plan>,
    pub sites: Vec<Site>,
    pub flows: Vec<Flow>,
    pub has_result: bool,
}
pub struct Context {
    pub control_null: Handle,
    pub session: Arc<Session>,
    pub cancellation: Arc<AtomicBool>,
    pub deadline: Option<Instant>,
    pub observe_cli_cancel: bool,
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
fn uint(v: &impl Fields, key: &str) -> Result<usize, String> {
    v.field(key)
        .as_u64()
        .and_then(|n| usize::try_from(n).ok())
        .ok_or_else(|| format!("invalid metadata {key}"))
}
fn borrowed_string<'a>(v: &'a impl Fields, key: &str) -> Result<&'a str, String> {
    v.field(key)
        .as_str()
        .ok_or_else(|| format!("invalid metadata {key}"))
}
fn string(v: &impl Fields, key: &str) -> Result<String, String> {
    borrowed_string(v, key).map(str::to_owned)
}
fn closed(value: &Json, keys: &[&str], subject: &str) -> Result<(), String> {
    let fields = value
        .as_object()
        .ok_or_else(|| format!("invalid metadata {subject}"))?;
    for key in fields.keys() {
        if !keys.contains(&key.as_str()) {
            return Err(format!("unknown {subject} metadata field {key}"));
        }
    }
    Ok(())
}
fn derivative_origin(value: &Json) -> Result<DerivativeOrigin, String> {
    Ok(DerivativeOrigin {
        plan: uint(value, "derivative_plan")?,
        node: uint(value, "derivative_node")?,
        frame: uint(value, "derivative_frame")?,
    })
}
fn operation_metadata(
    value: &Json,
    types: &[Type],
    flow_count: usize,
) -> Result<(String, Vec<String>, Vec<Argument>), String> {
    let operation = string(value, "operation")?;
    let labels = value["labels"]
        .as_array()
        .ok_or("invalid site labels")?
        .iter()
        .map(|label| {
            label
                .as_str()
                .map(str::to_owned)
                .ok_or("invalid label".to_owned())
        })
        .collect::<Result<Vec<_>, _>>()?;
    let mut arguments = Vec::new();
    for argument in value["arguments"]
        .as_array()
        .ok_or("absent argument metadata")?
    {
        let typ = uint(argument, "type")?;
        let flow = uint(argument, "flow")?;
        arguments.push(match borrowed_string(argument, "kind")? {
            "value" if flow == 0 => {
                Argument::Value(types.get(typ).ok_or("unknown argument type")?.clone())
            }
            "flow" if typ == 0 && flow < flow_count => Argument::Flow(flow),
            _ => return Err("invalid argument metadata".into()),
        });
    }
    Ok((operation, labels, arguments))
}
impl Program {
    pub fn parse(data: &[u8], flow_count: usize) -> Result<Self, String> {
        let document: Document =
            serde_json::from_slice(data).map_err(|e| format!("program metadata: {e}"))?;
        let v = document.root;
        closed(
            &v,
            &[
                "abi",
                "has_result",
                "source",
                "types",
                "files",
                "flows",
                "derivatives",
                "artifact",
            ],
            "program",
        )?;
        if v["abi"] != "lo-native-2" {
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
            let typ = Type::parse(borrowed_string(t, "name")?)?;
            if typ.kind() != borrowed_string(t, "kind")? {
                return Err("type kind mismatch".into());
            }
            types.push(typ)
        }
        let files = v["files"]
            .as_array()
            .ok_or("absent metadata files")?
            .iter()
            .map(|file| {
                file.as_str()
                    .map(Arc::<str>::from)
                    .ok_or("invalid metadata file".to_owned())
            })
            .collect::<Result<Vec<_>, _>>()?;
        let mut spans = Vec::new();
        for span in document.spans.ok_or("absent metadata spans")? {
            if let Some(key) = span.unknown_fields.iter().next() {
                return Err(format!("unknown span metadata field {key}"));
            }
            let file_index = span
                .file_index
                .as_u64()
                .and_then(|index| usize::try_from(index).ok())
                .ok_or("invalid metadata file_index")?;
            let file = files.get(file_index).ok_or("unknown span file")?.clone();
            for coordinate in [&span.start, &span.end] {
                for key in ["offset", "line", "column"] {
                    let value = uint(coordinate, key)?;
                    if key != "offset" && value == 0 {
                        return Err("invalid location coordinate".into());
                    }
                }
            }
            if uint(&span.end, "offset")? < uint(&span.start, "offset")?
                || uint(&span.end, "line")? < uint(&span.start, "line")?
            {
                return Err("invalid source span".into());
            }
            spans.push(Arc::new(SourceSpan {
                file,
                start: span.start,
                end: span.end,
            }));
        }
        let mut sites = Vec::new();
        for (index, row) in document
            .locations
            .as_ref()
            .ok_or("absent metadata locations")?
            .iter()
            .enumerate()
        {
            if !row.object {
                return Err("invalid metadata site".into());
            }
            if let Some(key) = row.unknown_fields.iter().next() {
                return Err(format!("unknown site metadata field {key}"));
            }
            let span = spans
                .get(uint(row, "span")?)
                .ok_or("unknown site span")?
                .clone();
            let action = Action::parse(borrowed_string(row, "action")?)?;
            let typ = types
                .get(uint(row, "type")?)
                .ok_or("unknown site type")?
                .clone();
            let owner = string(row, "owner")?;
            let flow = uint(row, "flow")?;
            let value = row.field("payload");
            let mut operation = String::new();
            let mut labels = Vec::new();
            let mut arguments = Vec::new();
            let payload = match action {
                Action::Body => {
                    closed(
                        value,
                        &["derivative_plan", "derivative_node", "derivative_frame"],
                        "payload",
                    )?;
                    SitePayload::Body(derivative_origin(value)?)
                }
                Action::Literal => {
                    if typ.kind() == "Float" {
                        closed(value, &["text", "float_bits"], "payload")?;
                    } else {
                        closed(value, &["text"], "payload")?;
                    }
                    let text = string(value, "text")?;
                    let float_bits = if typ.kind() == "Float" {
                        Some(string(value, "float_bits")?)
                    } else {
                        None
                    };
                    SitePayload::Literal { text, float_bits }
                }
                Action::Project => {
                    closed(value, &["field"], "payload")?;
                    SitePayload::Project(string(value, "field")?)
                }
                Action::Operation => {
                    closed(value, &["operation", "labels", "arguments"], "payload")?;
                    (operation, labels, arguments) = operation_metadata(value, &types, flow_count)?;
                    SitePayload::Operation
                }
                action if action.is_derivative() => {
                    closed(
                        value,
                        &[
                            "derivative_plan",
                            "derivative_node",
                            "derivative_frame",
                            "operation",
                            "labels",
                            "arguments",
                        ],
                        "payload",
                    )?;
                    let origin = derivative_origin(value)?;
                    (operation, labels, arguments) = operation_metadata(value, &types, flow_count)?;
                    SitePayload::Derivative(origin)
                }
                _ => {
                    closed(value, &[], "payload")?;
                    SitePayload::Empty
                }
            };
            sites.push(Site(Arc::new(SiteData {
                literal: None,
                action,
                span,
                owner,
                payload,
                typ,
                id: index as u64,
                operation,
                labels,
                arguments,
                flow,
            })));
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
            flows.push(Flow(Arc::new(FlowData {
                identity: string(f, "identity")?,
                handler: f["handler"].as_u64().map(|h| h as usize),
                name: string(f, "name")?,
                parameters,
                output,
            })));
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
        for site in &mut sites {
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
                let (t, float_bits) = site.literal_source();
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
                        let bits = float_bits.unwrap();
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
                site.literal = Some(match site.typ.kind() {
                    "Int" => {
                        let number = num_bigint::BigInt::parse_bytes(t.as_bytes(), 10)
                            .ok_or("invalid native integer metadata")?;
                        if number.bits() > MAX_BITS {
                            Literal::Limit("integer bits")
                        } else {
                            Literal::Value(int(number))
                        }
                    }
                    "Float" => {
                        let bits = u64::from_str_radix(float_bits.unwrap(), 16)
                            .map_err(|_| "invalid native Float metadata")?;
                        Literal::Value(new(atom("Float"), Data::Float(f64::from_bits(bits))))
                    }
                    "String" if t.len() > MAX_BYTES => Literal::Limit("string bytes"),
                    "String" => Literal::Value(text(t)),
                    "Bool" => Literal::Value(boolean(t == "true")),
                    "Null" => Literal::Value(new(atom("Null"), Data::Null)),
                    _ => unreachable!(),
                });
            }
            if site.action == Action::Project && site.project_field().is_empty() {
                return Err("invalid projection metadata".into());
            }
            if site.action.is_derivative() {
                crate::autodiff::admit_site(site, &derivatives)?;
            }
            if site.action == Action::Operation {
                crate::operations::admit(site)?;
                if site.operation == "HTTP.Server.new" {
                    crate::http::admit_callbacks(site, &flows)?;
                }
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
        let has_result = v["has_result"]
            .as_bool()
            .ok_or("invalid program result presence")?;
        LIVE_PROGRAMS.fetch_add(1, Ordering::Relaxed);
        Ok(Self {
            _lifetime: ProgramLifetime,
            control_null: new(atom("Null"), Data::Null),
            artifact: v.get("artifact").cloned(),
            derivatives,
            sites,
            flows,
            has_result,
        })
    }
}
impl Drop for ProgramLifetime {
    fn drop(&mut self) {
        LIVE_PROGRAMS.fetch_sub(1, Ordering::Relaxed);
    }
}
impl Context {
    pub fn new(program: Arc<Program>, steps: u64) -> Self {
        let session = Session::empty(program, steps);
        Self::with_session(session, crate::files::empty(), true)
    }
    fn with_session(
        session: Arc<Session>,
        files: crate::files::Files,
        observe_cli_cancel: bool,
    ) -> Self {
        LIVE_CONTEXTS.fetch_add(1, Ordering::Relaxed);
        Self {
            control_null: session.program.control_null.clone(),
            cancellation: Arc::new(AtomicBool::new(false)),
            deadline: None,
            observe_cli_cancel,
            files,
            last_activation: None,
            resources: crate::resources::Ledger::with_config(session.devices.clone()),
            device_grant: session.devices.grant.clone(),
            tapes: vec![],
            program: session.program.clone(),
            owner: thread::current().id(),
            remaining: session.steps,
            session,
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
    pub fn request(session: Arc<Session>) -> Result<Self, Fault> {
        let files = session.files.lock().unwrap().fork().map_err(host)?;
        Ok(Self::with_session(session, files, false))
    }
    pub fn invoke(&mut self, flow: usize, args: &[Handle]) -> Outcome {
        unsafe { crate::exports::invoke(self, flow, args, 0) }
    }
    pub fn close(&mut self) -> Result<(), Fault> {
        let files = self.files.close().err().map(host);
        let devices = self.resources.close().err();
        match combine_cleanup(files, devices) {
            None => Ok(()),
            Some(fault) => {
                let message = format!("close runtime resources \"program\": {}", fault.message());
                let details = match fault {
                    Fault::Boundary { mut details, .. } => {
                        details.cleanup_messages.clear();
                        *details
                    }
                    _ => BoundaryDetails::default(),
                };
                Err(host(message).with_details(details))
            }
        }
    }
    pub fn device_parts(&mut self) -> (&mut crate::resources::Ledger, crate::device::Control<'_>) {
        let control = crate::device::Control::new(
            &self.cancelled,
            &self.cancellation,
            self.deadline,
            self.observe_cli_cancel,
            &self.cancel_polls,
            self.cancel_after_polls,
        );
        (&mut self.resources, control)
    }
    pub fn capture_fault(&self, fault: Fault) -> Fault {
        if matches!(fault.primary(), Fault::Recoverable(_)) {
            return fault;
        }
        let removal = self.files.last_removal();
        let mut details = BoundaryDetails {
            context_captured: true,
            last_completed_activation: self.last_activation.clone(),
            flow_progress: self.flow_progress.clone(),
            removal_entries: removal.as_ref().map(|(entries, _)| entries.json()),
            removal_progress: removal.as_ref().map(|(_, progress)| progress.json()),
            ..Default::default()
        };
        match fault {
            Fault::Boundary {
                primary,
                details: precise,
            } => {
                details.merge(*precise);
                primary.with_details(details)
            }
            fault => fault.with_details(details),
        }
    }
    pub fn reserve_file(&mut self, steps: u64, weight: u64) -> Result<(), Fault> {
        self.check_cancel()?;
        if weight > MAX_BYTES as u64 {
            return Err(limit("expanded file value bytes"));
        }
        if weight > (64u64 << 20).saturating_sub(self.weight) {
            return Err(limit("evaluation file value bytes"));
        }
        if steps > self.remaining {
            return Err(limit("file work steps"));
        }
        self.remaining -= steps;
        Ok(())
    }
    pub fn add_file_weight(&mut self, weight: u64) {
        self.weight += weight;
    }
    fn check_owner(&self) -> Result<(), Fault> {
        if self.owner != thread::current().id() {
            Err(host("context belongs to another thread"))
        } else {
            Ok(())
        }
    }
    pub fn check_fault(&self) -> Result<(), Fault> {
        self.fault
            .as_ref()
            .map_or(Ok(()), |fault| Err(fault.clone()))
    }
    pub fn entry(&self) -> Result<(), Fault> {
        self.check_owner()?;
        self.check_fault()
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
        self.check_fault()?;
        let polls = self.cancel_polls.get().saturating_add(1);
        self.cancel_polls.set(polls);
        if self.cancelled
            || self.cancellation.load(Ordering::Relaxed)
            || self
                .deadline
                .is_some_and(|deadline| Instant::now() >= deadline)
            || (self.observe_cli_cancel && crate::cli::cancelled())
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
