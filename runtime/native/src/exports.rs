use crate::{action::Action, context::*, value::*};
use std::{ptr, sync::Arc};
#[repr(C)]
pub struct ResultSlot {
    pub status: u32,
    pub code: u32,
    pub value: *const Value,
}
pub type Callback =
    unsafe extern "C" fn(*mut Context, *const *const Value, u64, u64, *mut ResultSlot);
#[allow(improper_ctypes)]
extern "C" {
    pub static lo_program_metadata: u8;
    pub static lo_program_metadata_size: u64;
    pub static lo_program_flow_count: u64;
    pub static lo_program_flows: Callback;
    pub fn lo_entry(c: *mut Context, a: *const *const Value, n: u64, s: u64, out: *mut ResultSlot);
}
#[no_mangle]
pub unsafe extern "C" fn lo_value_retain(v: *const Value) {
    if !v.is_null() {
        Arc::increment_strong_count(v)
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_value_release(v: *const Value) {
    if !v.is_null() {
        drop(Arc::from_raw(v))
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_boolean_value(v: *const Value) -> i32 {
    (*v).bool() as i32
}
#[no_mangle]
pub extern "C" fn lo_runtime_live_values() -> u64 {
    LIVE_VALUES.load(std::sync::atomic::Ordering::Relaxed)
}
pub unsafe fn borrow(v: *const Value) -> Handle {
    Arc::increment_strong_count(v);
    Arc::from_raw(v)
}
pub unsafe fn write(c: &mut Context, out: *mut ResultSlot, r: Outcome) {
    let slot = match r {
        Ok(v) => ResultSlot {
            status: 0,
            code: 0,
            value: Arc::into_raw(v),
        },
        Err(Fault::Recoverable(v)) => ResultSlot {
            status: 1,
            code: 1,
            value: Arc::into_raw(v),
        },
        Err(f) => {
            c.fault = Some(f);
            ResultSlot {
                status: 2,
                code: 2,
                value: ptr::null(),
            }
        }
    };
    ptr::write(out, slot)
}
unsafe fn reject_thread(c: *const Context, out: *mut ResultSlot) -> bool {
    if ptr::addr_of!((*c).owner).read() != std::thread::current().id() {
        ptr::write(
            out,
            ResultSlot {
                status: 2,
                code: 3,
                value: ptr::null(),
            },
        );
        true
    } else {
        false
    }
}
enum ArgumentValues {
    Empty,
    One([Handle; 1]),
    Two([Handle; 2]),
    Many(Vec<Handle>),
}
impl std::ops::Deref for ArgumentValues {
    type Target = [Handle];
    fn deref(&self) -> &Self::Target {
        match self {
            Self::Empty => &[],
            Self::One(values) => values,
            Self::Two(values) => values,
            Self::Many(values) => values,
        }
    }
}
impl ArgumentValues {
    unsafe fn read(args: *const *const Value, count: usize) -> Result<Self, Fault> {
        let input = |index| {
            let value = *args.add(index);
            if value.is_null() {
                Err(host("null input handle"))
            } else {
                Ok(borrow(value))
            }
        };
        Ok(match count {
            0 => Self::Empty,
            1 => Self::One([input(0)?]),
            2 => Self::Two([input(0)?, input(1)?]),
            _ => {
                let mut values = Vec::with_capacity(count);
                for index in 0..count {
                    values.push(input(index)?);
                }
                Self::Many(values)
            }
        })
    }
}
pub unsafe fn call(
    c: *mut Context,
    args: *const *const Value,
    n: u64,
    site: u64,
    out: *mut ResultSlot,
    operation: Option<&str>,
) {
    if c.is_null() || out.is_null() {
        std::process::abort()
    }
    if reject_thread(c, out) {
        return;
    }
    let result = (|| -> Outcome {
        if operation != Some("lo_derivative_host_failure") {
            (&*c).check_fault()?;
        }
        let s = (&*c).site(site)?;
        let count = usize::try_from(n).map_err(|_| host("invalid argument count"))?;
        if count > MAX_ITEMS || count > 0 && args.is_null() {
            return Err(host("invalid argument array"));
        }
        let a = ArgumentValues::read(args, count)?;
        crate::tensor::validate_inputs(&*c, &a)?;
        if let Some(op) = operation {
            if op.starts_with("lo_derivative_") {
                if op.strip_prefix("lo_") != Some(s.action.as_str()) {
                    return Err(host("derivative helper metadata mismatch"));
                }
                return crate::autodiff::execute(&mut *c, &s, &a);
            }
            if op.starts_with("lo_") {
                let (expected, argc) = match op {
                    "lo_flow_enter" => (true, 0),
                    "lo_step" => (
                        matches!(s.action, Action::StatementStep | Action::ExpressionStep),
                        0,
                    ),
                    "lo_convert_propagate" => (
                        matches!(s.action, Action::ConvertPropagate | Action::Cause),
                        1,
                    ),
                    "lo_literal" => (s.action == Action::Literal, 0),
                    "lo_scope_enter" => (s.action == Action::ScopeEnter, 0),
                    "lo_flow_ref" => (s.action == Action::FlowRef, 0),
                    "lo_charge_value" => (s.action == Action::ChargeValue, 1),
                    "lo_cause" => (s.action == Action::Cause, 1),
                    "lo_commit" => (s.action == Action::Commit, 1),
                    "lo_project" => (s.action == Action::Project, 1),
                    "lo_match" => (s.action == Action::Match, 2),
                    "lo_convert_success" => (s.action == Action::ConvertSuccess, 1),
                    "lo_convert_failure" => (s.action == Action::ConvertFailure, 1),
                    "lo_convert_record_view" => (s.action == Action::ConvertRecordView, 1),
                    "lo_convert_scalar_list_view" => (s.action == Action::ConvertScalarListView, 1),
                    "lo_convert_fallible_record_view" => {
                        (s.action == Action::ConvertFallibleRecordView, 1)
                    }
                    _ => (false, 1),
                };
                if !expected || count != argc {
                    return Err(host("runtime helper metadata mismatch"));
                }
                validate_helper(op, &s, &a)?;
                return helper(&mut *c, &s, &a, op);
            }
            if s.action != Action::Operation || s.operation != op {
                return Err(host("operation metadata identity mismatch"));
            }
            if count != s.arguments.len()
                || a.iter().zip(&s.arguments).any(|(v, p)| match p {
                    Argument::Value(t) => v.typ != *t,
                    Argument::Flow(f) => !matches!(v.data,Data::Flow(id) if id==*f),
                })
            {
                return Err(host("operation arguments disagree with admitted metadata"));
            }
            if crate::tensor::SUPPORTED.contains(&op) {
                return crate::tensor::execute(&mut *c, &s, &a);
            }
            if crate::training::SUPPORTED.contains(&op) {
                return crate::training::execute(&mut *c, &s, &a);
            }
            if crate::checkpoint::SUPPORTED.contains(&op) {
                return crate::checkpoint::execute(&mut *c, &s, &a);
            }
            if crate::process::SUPPORTED.contains(&op) {
                return crate::process::execute(&mut *c, &s, &a);
            }
            if crate::http::SUPPORTED.contains(&op) {
                return crate::http::execute(&mut *c, &s, &a);
            }
            if crate::files::SUPPORTED.contains(&op) {
                return crate::files::execute(&mut *c, &s, &a);
            }
            if matches!(
                op,
                "List.map"
                    | "List.filter"
                    | "List.fold"
                    | "List.flat_map"
                    | "List.unfold"
                    | "Flow.fold"
            ) {
                crate::collections::execute(c, &s, &a)
            } else {
                crate::operations::execute(&mut *c, &s, &a)
            }
        } else {
            Err(host("absent ABI symbol identity"))
        }
    })();
    write(&mut *c, out, result)
}
fn validate_helper(symbol: &str, s: &Site, a: &[Handle]) -> Result<(), Fault> {
    let invalid = || host(format!("{symbol}: invalid typed helper contract"));
    let valid = match symbol {
        "lo_literal" | "lo_scope_enter" | "lo_flow_enter" | "lo_flow_ref" | "lo_step" => true,
        "lo_charge_value" | "lo_cause" => a[0].typ == s.typ,
        "lo_convert_success" => matches!(&s.typ,Type::Fallible(t) if t.as_ref()==&a[0].typ),
        "lo_convert_failure" => {
            matches!(s.typ, Type::Fallible(_)) && matches!(a[0].data, Data::Error(_))
        }
        "lo_convert_propagate" => {
            matches!(&a[0].typ,Type::Fallible(t) if s.typ==a[0].typ||s.typ==**t)
                && matches!(a[0].data, Data::Success(_) | Data::Failure(_))
        }
        "lo_convert_record_view" => {
            s.typ == atom("Record")
                && matches!(a[0].typ, Type::Record(_))
                && matches!(a[0].data, Data::Record(_))
        }
        "lo_convert_scalar_list_view" => {
            a[0].typ == atom("String") && s.typ == Type::List(Box::new(atom("Scalar")))
        }
        "lo_convert_fallible_record_view" => {
            matches!(&a[0].typ,Type::Fallible(t) if matches!(t.as_ref(),Type::Record(_)))
                && s.typ == Type::Fallible(Box::new(atom("Record")))
        }
        "lo_project" => {
            let field = s.project_field();
            let typ = match &a[0].data {
                Data::Record(fields) => fields.get(field).map(|v| v.typ.clone()),
                Data::Wrapped(v) => v.fields().get(field).map(|v| v.typ.clone()),
                Data::Adapter(v) => v
                    .fields()
                    .and_then(|fields| fields.get(field))
                    .map(|v| v.typ.clone()),
                Data::Error(_) => match field {
                    "kind" | "message" => Some(atom("String")),
                    "input" => Some(atom("Record")),
                    "trace" => Some(Type::List(Box::new(Type::Record(vec![
                        ("flow".into(), atom("String")),
                        ("input".into(), atom("Record")),
                        ("operation".into(), atom("String")),
                    ])))),
                    "flow_progress" => Some(Type::List(Box::new(atom("Record")))),
                    _ => None,
                },
                _ => None,
            };
            typ.as_ref() == Some(&s.typ)
        }
        "lo_match" => {
            s.typ == atom("Bool")
                && match &a[1].typ {
                    Type::Range(t) => t.as_ref() == &a[0].typ,
                    _ => {
                        a[0].typ == a[1].typ
                            && matches!(
                                a[0].typ.kind(),
                                "Int" | "String" | "Scalar" | "Bool" | "Null"
                            )
                    }
                }
        }
        "lo_commit" => true,
        _ => false,
    };
    if valid {
        Ok(())
    } else {
        Err(invalid())
    }
}
fn helper(c: &mut Context, s: &Site, a: &[Handle], symbol: &str) -> Outcome {
    let action = if symbol == "lo_convert_propagate" {
        Action::ConvertPropagate
    } else if symbol == "lo_flow_enter" {
        Action::FlowEnter
    } else {
        s.action
    };
    match action {
        Action::StatementStep | Action::ExpressionStep => {
            if !a.is_empty() {
                return Err(host("step arity"));
            }
            c.step()?;
            Ok(c.control_null.clone())
        }
        Action::ScopeEnter => {
            c.resources.scope_enter();
            c.scopes += 1;
            Ok(c.control_null.clone())
        }
        Action::FlowEnter => {
            if c.depth == 256 {
                return Err(limit("flow depth"));
            }
            c.depth += 1;
            Ok(c.control_null.clone())
        }
        Action::ChargeValue => {
            if a.len() != 1 {
                return Err(host("charge arity"));
            }
            c.charge(&a[0])?;
            Ok(c.control_null.clone())
        }
        Action::Literal => {
            if !a.is_empty() {
                return Err(host("literal arity"));
            }
            match s.literal.as_ref() {
                Some(Literal::Value(value)) => Ok(value.clone()),
                Some(Literal::Limit(message)) => Err(limit(message)),
                None => Err(host("literal has no admitted value")),
            }
        }
        Action::FlowRef => Ok(new(atom("Null"), Data::Flow(s.flow))),
        Action::Cause => {
            if a.len() != 1 {
                return Err(host("cause arity"));
            }
            if let Some(e) = cause(&a[0]) {
                Err(Fault::Recoverable(e))
            } else {
                Ok(a[0].clone())
            }
        }
        Action::ConvertSuccess => {
            if a.len() != 1 {
                return Err(host("conversion arity"));
            }
            Ok(success(&s.typ, a[0].clone()))
        }
        Action::ConvertFailure => {
            if a.len() != 1 || a[0].typ != atom("Error") {
                return Err(host("failure conversion input"));
            }
            Ok(failure(&s.typ, a[0].clone()))
        }
        Action::ConvertPropagate => {
            if a.len() != 1 {
                return Err(host("conversion arity"));
            }
            match &a[0].data {
                Data::Success(v) => Ok(v.clone()),
                Data::Failure(e) => Err(Fault::Recoverable(e.clone())),
                _ => Err(host("propagation input type")),
            }
        }
        Action::ConvertRecordView => Ok(view(&a[0])),
        Action::ConvertFallibleRecordView => match &a[0].data {
            Data::Success(v) => Ok(success(&s.typ, view(v))),
            Data::Failure(e) => Ok(failure(&s.typ, e.clone())),
            _ => Err(host("fallible record view input")),
        },
        Action::ConvertScalarListView => {
            let mut output = vec![];
            for ch in a[0].string().chars() {
                if output.len() == MAX_ITEMS {
                    return Err(limit("list elements"));
                }
                output.push(new(atom("Scalar"), Data::Text(ch.to_string())))
            }
            Ok(new(s.typ.clone(), Data::List(output)))
        }
        Action::Project => {
            let field = s.project_field();
            if let Data::Error(e) = &a[0].data {
                return match field {
                    "kind" => Ok(text(&e.kind)),
                    "message" => Ok(text(&e.message)),
                    "input" => Ok(e.input.clone()),
                    "trace" => Ok(new(s.typ.clone(), Data::List(e.trace.clone()))),
                    "flow_progress" => Ok(new(
                        s.typ.clone(),
                        Data::List(e.flow_progress.iter().map(view).collect()),
                    )),
                    _ => Err(host("unknown Error field")),
                };
            }
            a[0].fields()
                .get(field)
                .cloned()
                .ok_or_else(|| host("unknown record field"))
        }
        Action::Match => Ok(boolean(if matches!(a[1].data, Data::Range(..)) {
            contains(&a[1], &a[0])
        } else {
            equal(&a[0], &a[1])
        })),
        Action::Commit => crate::adapters::commit(c, s, &a[0]),
        _ => Err(host(format!(
            "invalid runtime action {}",
            s.action.as_str()
        ))),
    }
}
macro_rules! helper {($($name:ident),*)=>{$(#[no_mangle] pub unsafe extern "C" fn $name(c:*mut Context,a:*const *const Value,n:u64,s:u64,o:*mut ResultSlot){call(c,a,n,s,o,Some(stringify!($name)))})*};}
helper!(
    lo_literal,
    lo_step,
    lo_charge_value,
    lo_scope_enter,
    lo_flow_enter,
    lo_cause,
    lo_commit,
    lo_project,
    lo_match,
    lo_flow_ref,
    lo_convert_success,
    lo_convert_failure,
    lo_convert_propagate,
    lo_convert_record_view,
    lo_convert_scalar_list_view,
    lo_convert_fallible_record_view
);
#[no_mangle]
pub unsafe extern "C" fn lo_invalid_status(
    c: *mut Context,
    _: *const *const Value,
    _: u64,
    _: u64,
    o: *mut ResultSlot,
) {
    write(&mut *c, o, Err(host("invalid native ABI status or arity")))
}
#[no_mangle]
pub unsafe extern "C" fn lo_scope_leave(c: *mut Context, result: *const Value, _: u64) {
    let c = &mut *c;
    if c.owner != std::thread::current().id() || c.scopes == 0 {
        std::process::abort()
    }
    let roots = if result.is_null() {
        vec![]
    } else {
        let roots = [borrow(result)];
        if crate::tensor::validate_inputs(c, &roots).is_err() {
            std::process::abort()
        }
        crate::tensor::allocations(&roots)
    };
    let scope = c.resources.current_scope();
    c.resources.scope_close(scope, &roots);
    c.scopes -= 1;
}
#[no_mangle]
pub unsafe extern "C" fn lo_flow_leave(c: *mut Context) {
    let c = &mut *c;
    if c.owner != std::thread::current().id() || c.depth == 0 {
        std::process::abort()
    }
    c.depth -= 1;
}
#[no_mangle]
pub unsafe extern "C" fn lo_flow_error(
    c: *mut Context,
    flow: u64,
    args: *const *const Value,
    n: u64,
    site: u64,
    out: *mut ResultSlot,
) {
    if reject_thread(c, out) {
        return;
    }
    let c = &mut *c;
    let result = (|| {
        c.entry()?;
        let f = c
            .program
            .flows
            .get(flow as usize)
            .cloned()
            .ok_or_else(|| host("unknown flow"))?;
        if n as usize != f.parameters.len() + 1 || args.is_null() {
            return Err(host("flow error arity"));
        }
        let e = borrow(*args);
        let mut fields = vec![];
        for (i, (name, _)) in f.parameters.iter().enumerate() {
            fields.push((name.clone(), borrow(*args.add(i + 1))))
        }
        crate::tensor::validate_inputs(c, &[e.clone(), record(fields.clone())])?;
        let _ = c.site(site)?;
        let Data::Error(err) = &e.data else {
            return Err(host("flow error input type"));
        };
        let framed = frame(&e, &f.name, &err.operation, record(fields));
        if f.handler.is_some() {
            return Err(Fault::Recoverable(framed));
        }
        if !matches!(f.output, Type::Fallible(_)) {
            return Err(host("unhandled failure in total flow"));
        }
        let Data::Error(mut error) = framed.data.clone() else {
            std::process::abort()
        };
        error.operation = f.name.clone();
        error.site = site;
        Err(Fault::Recoverable(new(atom("Error"), Data::Error(error))))
    })();
    write(c, out, result)
}
pub unsafe fn invoke(c: *mut Context, flow: usize, a: &[Handle], site: u64) -> Outcome {
    let f = (&*c)
        .program
        .flows
        .get(flow)
        .cloned()
        .ok_or_else(|| host("unknown callback flow"))?;
    if a.len() != f.parameters.len() || a.iter().zip(&f.parameters).any(|(a, (_, t))| a.typ != *t) {
        return Err(host("callback parameter metadata mismatch"));
    }
    let mut inline = [ptr::null(); 2];
    let heap;
    let raw: &[*const Value] = if a.len() <= inline.len() {
        for (slot, value) in inline.iter_mut().zip(a) {
            *slot = Arc::as_ptr(value);
        }
        &inline[..a.len()]
    } else {
        heap = a.iter().map(Arc::as_ptr).collect::<Vec<_>>();
        &heap
    };
    let mut out = ResultSlot {
        status: 0,
        code: 0,
        value: ptr::null(),
    };
    let table = ptr::addr_of!(lo_program_flows);
    let callback = *table.add(flow);
    callback(c, raw.as_ptr(), raw.len() as u64, site, &mut out);
    match read(&*c, out) {
        Ok(v) => {
            if &v.typ != f.output.base_ref() {
                return Err(host("callback result type metadata mismatch"));
            }
            Ok(success(&f.output, v))
        }
        Err(Fault::Recoverable(e)) if matches!(f.output, Type::Fallible(_)) => {
            Ok(failure(&f.output, e))
        }
        r => r,
    }
}
pub unsafe fn read(c: &Context, out: ResultSlot) -> Outcome {
    match out.status {
        0 | 1 => {
            if out.value.is_null() {
                return Err(host("null native result"));
            }
            let v = Arc::from_raw(out.value);
            if out.status == 0 {
                Ok(v)
            } else {
                Err(Fault::Recoverable(v))
            }
        }
        2 => Err(c
            .fault
            .clone()
            .unwrap_or_else(|| host("missing terminal diagnostic"))),
        _ => Err(host("invalid native result status")),
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_program_new() -> *const Program {
    if lo_program_metadata_size > 64 << 20 || lo_program_flow_count > 1_000_000 {
        return ptr::null();
    }
    let data = std::slice::from_raw_parts(
        ptr::addr_of!(lo_program_metadata),
        lo_program_metadata_size as usize,
    );
    match Program::parse(data, lo_program_flow_count as usize) {
        Ok(program) => Arc::into_raw(Arc::new(program)),
        Err(_) => ptr::null(),
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_program_free(program: *const Program) {
    if !program.is_null() {
        drop(Arc::from_raw(program));
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_from_program(
    program: *const Program,
    steps: u64,
) -> *mut Context {
    if program.is_null() || steps == 0 {
        return ptr::null_mut();
    }
    Arc::increment_strong_count(program);
    Box::into_raw(Box::new(Context::new(Arc::from_raw(program), steps)))
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_new(steps: u64) -> *mut Context {
    if steps == 0 {
        return ptr::null_mut();
    }
    let program = lo_program_new();
    let context = lo_context_from_program(program, steps);
    lo_program_free(program);
    context
}
#[no_mangle]
pub extern "C" fn lo_runtime_live_programs() -> u64 {
    LIVE_PROGRAMS.load(std::sync::atomic::Ordering::Relaxed)
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_free(c: *mut Context) {
    if !c.is_null() {
        if (*c).owner != std::thread::current().id() {
            std::process::abort()
        }
        drop(Box::from_raw(c))
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_set_process_context(
    c: *mut Context,
    bytes: *const u8,
    length: usize,
) -> std::os::raw::c_int {
    if c.is_null() || bytes.is_null() || length == 0 || length > MAX_BYTES {
        return 0;
    }
    let context = &mut *c;
    if context.entry().is_err()
        || context.remaining != context.session.steps
        || context.weight != 0
        || context.scopes != 0
        || context.depth != 0
        || Arc::strong_count(&context.session) != 1
    {
        return 0;
    }
    let Ok(initial) =
        crate::process::parse_initial_context(std::slice::from_raw_parts(bytes, length))
    else {
        return 0;
    };
    Arc::get_mut(&mut context.session)
        .unwrap()
        .initial_process_context = Ok(initial);
    1
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_set_device_grant(
    c: *mut Context,
    grant: *const std::os::raw::c_char,
) -> std::os::raw::c_int {
    if c.is_null() || grant.is_null() {
        return 0;
    }
    let context = &mut *c;
    if context.entry().is_err()
        || context.remaining != context.session.steps
        || context.weight != 0
        || context.scopes != 0
        || context.depth != 0
        || !context.resources.can_configure()
        || Arc::strong_count(&context.session) != 1
    {
        return 0;
    }
    let Ok(grant) = std::ffi::CStr::from_ptr(grant).to_str() else {
        return 0;
    };
    let Ok(config) = crate::device::Config::new(grant) else {
        return 0;
    };
    if !context.resources.configure(config.clone()) {
        return 0;
    }
    Arc::get_mut(&mut context.session).unwrap().devices = config;
    context.device_grant = grant.to_owned();
    1
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_run(c: *mut Context, out: *mut ResultSlot) {
    if c.is_null() || out.is_null() {
        std::process::abort()
    }
    if reject_thread(c, out) {
        return;
    }
    if (*c).scopes != 0 || (*c).depth != 0 || !(*c).tapes.is_empty() {
        write(
            &mut *c,
            out,
            Err(host("context invocation is not quiescent")),
        );
        return;
    }
    (*c).resources.reclaim_orphans();
    lo_entry(c, ptr::null(), 0, 0, out);
    if (*out).status == 0 && !(*out).value.is_null() {
        let v = borrow((*out).value);
        if let Some(e) = cause(&v) {
            lo_value_release((*out).value);
            ptr::write(
                out,
                ResultSlot {
                    status: 1,
                    code: 1,
                    value: Arc::into_raw(e),
                },
            );
        }
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_cancel(c: *mut Context) {
    if (&*c).entry().is_err() {
        std::process::abort()
    }
    (*c).cancelled = true;
}
#[no_mangle]
pub extern "C" fn lo_runtime_live_contexts() -> u64 {
    LIVE_CONTEXTS.load(std::sync::atomic::Ordering::Relaxed)
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_scope_count(c: *const Context) -> u64 {
    (*c).scopes
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_depth(c: *const Context) -> u64 {
    (*c).depth as u64
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_remaining_steps(c: *const Context) -> u64 {
    (*c).remaining
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_leave(c: *mut Context) {
    crate::autodiff::leave(&mut *c)
}

#[no_mangle]
pub unsafe extern "C" fn lo_context_cancel_after_polls(c: *mut Context, polls: u64) {
    if (&*c).entry().is_err() || polls == 0 {
        std::process::abort()
    }
    (*c).cancel_polls.set(0);
    (*c).cancel_after_polls = Some(polls);
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_resource_bytes(c: *const Context, class: u32) -> u64 {
    match class {
        0 => (*c).resources.live,
        1..=3 => (*c).resources.class_bytes[class as usize],
        _ => std::process::abort(),
    }
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_peak_resource_bytes(c: *const Context, class: u32) -> u64 {
    if class > 3 {
        std::process::abort()
    }
    (*c).resources.peak_bytes[class as usize]
}
#[no_mangle]
pub unsafe extern "C" fn lo_context_tape_count(c: *const Context) -> u64 {
    (*c).tapes.len() as u64
}
#[no_mangle]
pub extern "C" fn lo_runtime_live_storages() -> u64 {
    crate::resources::LIVE_STORAGES.load(std::sync::atomic::Ordering::Relaxed)
}
#[no_mangle]
pub extern "C" fn lo_runtime_live_storage_bytes() -> u64 {
    crate::resources::LIVE_STORAGE_BYTES.load(std::sync::atomic::Ordering::Relaxed)
}
