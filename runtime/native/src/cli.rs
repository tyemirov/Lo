use crate::{context::*, exports::*, value::*};
use serde_json::{json, Value as Json};
use std::{
    ffi::CStr,
    io::Write,
    os::raw::{c_char, c_int},
    ptr,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
};
macro_rules! checked_stderr {($($argument:tt)*)=>{if writeln!(std::io::stderr().lock(),$($argument)*).is_err(){return 2}};}
static CANCELLED: AtomicBool = AtomicBool::new(false);
const SIGPIPE: c_int = 13;
const SIG_IGN: usize = 1;
pub fn cancelled() -> bool {
    CANCELLED.load(Ordering::Relaxed)
}
extern "C" {
    fn signal(sig: c_int, handler: usize) -> usize;
}
extern "C" fn stop(_: c_int) {
    CANCELLED.store(true, Ordering::Relaxed);
}
fn diagnostic(code: &str, stage: &str, message: &str) -> Json {
    json!({"code":code,"stage":stage,"message":message})
}
fn report(f: Fault, context: &Context, json_format: bool, cleanup: Option<&str>) -> i32 {
    let program = &context.program;
    let (mut d, status) = match f {
        Fault::RawCancellation => (
            diagnostic(
                "runtime.cancelled",
                "runtime",
                "execution cancelled: context canceled",
            ),
            2,
        ),
        Fault::Budget { message, .. } => (diagnostic("runtime.limit", "runtime", &message), 2),
        Fault::Numeric(_, message) => (
            diagnostic(
                "runtime.host-failure",
                "runtime",
                &format!("uncontextualized numerical failure: {message}"),
            ),
            2,
        ),
        Fault::Host(code, msg) => (diagnostic(code, "runtime", &msg), 2),
        Fault::Recoverable(e) => {
            let Data::Error(error) = &e.data else {
                std::process::abort()
            };
            let mut d = diagnostic(
                "runtime.unhandled-error",
                "runtime",
                &format!("{}: {}", error.kind, error.message),
            );
            if let Some(site) = program.sites.get(error.site as usize) {
                d["span"] = json!({"file":site.raw["file"],"start":site.raw["start"],"end":site.raw["end"]});
            }
            d["error"] = e.json();
            (d, 1)
        }
    };
    if status == 2 && !context.flow_progress.is_empty() {
        d["flow_progress"] = json!(context
            .flow_progress
            .iter()
            .map(|x| x.json())
            .collect::<Vec<_>>())
    }
    if d["error"]["flow_progress"].is_array() {
        d["flow_progress"] = d["error"]["flow_progress"].clone();
    }
    if status == 2 {
        if let Some(activation) = &context.last_activation {
            d["last_completed_activation"] = activation.clone();
        }
    }
    if let Some(cleanup) = cleanup {
        d["message"] = json!(format!(
            "{}; cleanup: {cleanup}",
            d["message"].as_str().unwrap()
        ));
        d["cleanup_errors"] = json!([cleanup]);
    }
    if json_format {
        checked_stderr!("{}", canonical_json_value(&json!({"diagnostics":[d]})));
    } else {
        if d["span"].is_object() {
            checked_stderr!(
                "{}:{}:{}: {}: {}",
                d["span"]["file"].as_str().unwrap(),
                d["span"]["start"]["line"],
                d["span"]["start"]["column"],
                d["code"].as_str().unwrap(),
                d["message"].as_str().unwrap()
            );
        } else {
            checked_stderr!(
                "{}: {}",
                d["code"].as_str().unwrap(),
                d["message"].as_str().unwrap()
            );
        }
        if let Some(frames) = d["error"]["trace"].as_array() {
            for frame in frames {
                let f = &frame["value"];
                checked_stderr!(
                    "  flow {}, operation {}, input {}",
                    serde_json::to_string(f["flow"]["value"].as_str().unwrap()).unwrap(),
                    serde_json::to_string(f["operation"]["value"].as_str().unwrap()).unwrap(),
                    canonical_json_value(&f["input"])
                );
            }
        }
    }
    status
}
#[no_mangle]
pub unsafe extern "C" fn lo_cli_main(argc: c_int, argv: *const *const c_char) -> c_int {
    signal(SIGPIPE, SIG_IGN);
    let mut args = vec![];
    for i in 1..argc {
        let s = match CStr::from_ptr(*argv.add(i as usize)).to_str() {
            Ok(v) => v,
            Err(_) => {
                checked_stderr!("cli.invalid-invocation: invalid UTF-8 argument");
                return 2;
            }
        };
        args.push(s)
    }
    let mut format = "text";
    let mut steps = 1_000_000u64;
    let mut error = None;
    let mut format_set = false;
    let mut steps_set = false;
    let mut read_roots = vec![];
    let mut write_roots = vec![];
    if args.first() != Some(&"run") {
        error = Some("artifact requires run without a source path".to_string())
    }
    let mut i = 1;
    while i < args.len() {
        let arg = args[i];
        if arg == "--format" && !format_set && i + 1 < args.len() {
            i += 1;
            format = args[i];
            format_set = true;
            if format != "text" && format != "json" {
                error = Some(format!("unknown output format {format:?}"))
            }
        } else if arg == "--max-steps" && !steps_set && i + 1 < args.len() {
            i += 1;
            steps_set = true;
            match args[i].parse::<u64>() {
                Ok(v) if v > 0 => steps = v,
                _ => error = Some("invalid --max-steps: execution steps must be positive".into()),
            }
        } else if (arg == "--read-root" || arg == "--write-root") && i + 1 < args.len() {
            i += 1;
            if arg == "--read-root" {
                read_roots.push(args[i].to_string());
            } else {
                write_roots.push(args[i].to_string());
            }
        } else {
            error = Some(format!("unknown option {arg:?}"))
        }
        i += 1;
    }
    if let Some(message) = error {
        if format == "json" {
            checked_stderr!(
                "{}",
                canonical_json_value(
                    &json!({"diagnostics":[diagnostic("cli.invalid-invocation","cli",&message)]})
                )
            )
        } else {
            checked_stderr!("cli.invalid-invocation: {message}")
        }
        return 2;
    }
    if lo_program_metadata_size > 64 << 20 || lo_program_flow_count > 1_000_000 {
        checked_stderr!("runtime.host-failure: program metadata size limit");
        return 2;
    }
    let data = std::slice::from_raw_parts(
        ptr::addr_of!(lo_program_metadata),
        lo_program_metadata_size as usize,
    );
    let program = match Program::parse(data, lo_program_flow_count as usize) {
        Ok(p) => Arc::new(p),
        Err(message) => {
            if format == "json" {
                checked_stderr!(
                    "{}",
                    canonical_json_value(
                        &json!({"diagnostics":[diagnostic("runtime.host-failure","runtime",&message)]})
                    )
                )
            } else {
                checked_stderr!("runtime.host-failure: {message}")
            }
            return 2;
        }
    };
    signal(2, stop as *const () as usize);
    signal(15, stop as *const () as usize);
    let mut context = Context::new(program.clone(), steps);
    context.files = match crate::files::configure(&read_roots, &write_roots) {
        Ok(files) => files,
        Err(message) => return report(host(message), &context, format == "json", None),
    };
    let mut out = ResultSlot {
        status: 0,
        code: 0,
        value: ptr::null(),
    };
    lo_entry(&mut context, ptr::null(), 0, 0, &mut out);
    let result = read(&context, out);
    let result = match result {
        Ok(v) => {
            if let Some(e) = cause(&v) {
                Err(Fault::Recoverable(e))
            } else {
                Ok(v)
            }
        }
        other => other,
    };
    let cleanup = context
        .files
        .close()
        .err()
        .map(|message| format!("close runtime resources \"program\": {message}"));
    match result {
        Ok(v) => {
            if let Some(message) = cleanup {
                return report(host(message), &context, format == "json", None);
            }
            let payload = if program.has_result {
                Some(if format == "json" {
                    format!(
                        "{{\"command\":\"run\",\"status\":\"ok\",\"result\":{}}}",
                        v.canonical_json()
                    )
                } else {
                    v.output()
                })
            } else if format == "json" {
                Some("{\"command\":\"run\",\"status\":\"ok\"}".into())
            } else {
                None
            };
            if let Some(payload) = payload {
                if let Err(e) = writeln!(std::io::stdout().lock(), "{payload}") {
                    return report(
                        host(format!("write command result \"\": {e}")),
                        &context,
                        format == "json",
                        None,
                    );
                }
            }
            0
        }
        Err(f) => report(f, &context, format == "json", cleanup.as_deref()),
    }
}
