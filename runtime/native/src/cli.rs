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
fn value_text(value: &Json) -> String {
    if value["status"] == "ok" {
        return value_text(&value["value"]);
    }
    match value["type"].as_str() {
        Some("Int" | "String" | "Scalar" | "File.Path") => value["value"].as_str().unwrap().into(),
        Some("Bool") => value["value"].as_bool().unwrap().to_string(),
        Some("Float") => format_float(value["value"].as_f64().unwrap()),
        _ => canonical_json_value(value),
    }
}
fn host_progress_text(diagnostic: &Json) -> String {
    let mut message = diagnostic["message"].as_str().unwrap().to_owned();
    if let Some(progress) = diagnostic.get("removal_progress") {
        let fields = &progress["value"];
        message.push_str(&format!(
            " (completed {} removals, stage {})",
            value_text(&fields["completed_count"]),
            value_text(&fields["stage"])
        ));
    }
    if let Some(progress) = diagnostic.get("write_progress") {
        let fields = &progress["value"];
        message.push_str(&format!(
            " (wrote {} bytes, stage {})",
            value_text(&fields["bytes_written"]),
            value_text(&fields["stage"])
        ));
    }
    if let Some(progress) = diagnostic.get("http_progress") {
        let fields = &progress["value"];
        message.push_str(&format!(
            " (HTTP stage {}, request written {}, remote outcome {})",
            value_text(&fields["stage"]),
            value_text(&fields["request_written"]),
            value_text(&fields["remote_outcome"])
        ));
    }
    if let Some(progress) = diagnostic.get("process_progress") {
        message.push_str(&format!(" (Process progress {})", value_text(progress)));
    }
    if let Some(completed) = diagnostic.get("last_completed_activation") {
        let summary = if let Some(receipt) = completed.get("receipt") {
            value_text(receipt)
        } else if let Some(process) = completed.get("process") {
            format!(
                "{} {}",
                completed["result_type"].as_str().unwrap(),
                value_text(process)
            )
        } else {
            format!(
                "{} status {}",
                completed["result_type"].as_str().unwrap(),
                value_text(&completed["status"])
            )
        };
        message.push_str(&format!(
            " (last completed {} at {}:{}: {summary})",
            completed["operation"].as_str().unwrap(),
            completed["span"]["file"].as_str().unwrap(),
            completed["span"]["start"]["line"]
        ));
    }
    if let Some(flows) = diagnostic["flow_progress"].as_array() {
        for flow in flows {
            let fields = &flow["value"];
            message.push_str(&format!(
                " (Flow.fold completed {}/{}, state {})",
                value_text(&fields["completed_count"]),
                value_text(&fields["total_count"]),
                value_text(&fields["state"])
            ));
        }
    }
    message
}
fn report(f: Fault, context: &Context, json_format: bool, cleanup: Option<&str>) -> i32 {
    let program = &context.program;
    let (f, details) = match f {
        Fault::Boundary { primary, details } => (*primary, *details),
        other => (other, BoundaryDetails::default()),
    };
    let (mut d, status) = match f {
        Fault::Boundary { .. } => unreachable!(),
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
                d["span"] = json!({"file":site.raw["file"],"start":site.raw.coordinate("start"),"end":site.raw.coordinate("end")});
            }
            d["error"] = e.json();
            (d, 1)
        }
    };
    for cleanup in &details.cleanup_messages {
        d["message"] = json!(format!(
            "{}; cleanup: {cleanup}",
            d["message"].as_str().unwrap()
        ));
    }
    if status == 2 && !details.context_captured && !context.flow_progress.is_empty() {
        d["flow_progress"] = json!(context
            .flow_progress
            .iter()
            .map(|x| x.json())
            .collect::<Vec<_>>())
    }
    if d["error"]["flow_progress"].is_array() {
        d["flow_progress"] = d["error"]["flow_progress"].clone();
    }
    if !details.context_captured {
        if let Some(activation) = &context.last_activation {
            d["last_completed_activation"] = activation.clone();
        }
    }
    for (key, value) in [
        ("process_progress", details.process_progress),
        ("http_progress", details.http_progress),
        ("write_progress", details.write_progress),
        ("removal_entries", details.removal_entries),
        ("removal_progress", details.removal_progress),
        (
            "last_completed_activation",
            details.last_completed_activation,
        ),
    ] {
        if let Some(value) = value {
            d[key] = value;
        }
    }
    if !details.flow_progress.is_empty() {
        d["flow_progress"] = json!(details
            .flow_progress
            .iter()
            .map(|v| v.json())
            .collect::<Vec<_>>());
    }
    if !details.cleanup_errors.is_empty() {
        d["cleanup_errors"] = json!(details.cleanup_errors);
    }
    if let Some(cleanup) = cleanup {
        d["message"] = json!(format!(
            "{}; cleanup: {cleanup}",
            d["message"].as_str().unwrap()
        ));
        if !d["cleanup_errors"].is_array() {
            d["cleanup_errors"] = json!([]);
        }
        d["cleanup_errors"]
            .as_array_mut()
            .unwrap()
            .push(json!(cleanup));
    }
    if json_format {
        checked_stderr!("{}", canonical_json_value(&json!({"diagnostics":[d]})));
    } else {
        if status == 2 {
            d["message"] = json!(host_progress_text(&d));
        }
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
pub unsafe extern "C" fn lo_cli_main(
    argc: c_int,
    argv: *const *const c_char,
    _entry_environment: *const *const c_char,
) -> c_int {
    #[cfg(target_os = "macos")]
    if argc == 2 && CStr::from_ptr(*argv.add(1)).to_bytes() == b"--lo-process-launcher" {
        extern "C" {
            fn lo_process_launcher_main(
                argc: c_int,
                argv: *const *const c_char,
                env: *const *const c_char,
            ) -> c_int;
        }
        let arguments = [*argv, ptr::null()];
        return lo_process_launcher_main(1, arguments.as_ptr(), _entry_environment);
    }
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
    if args == ["metadata"] {
        if lo_program_metadata_size > 64 << 20 || lo_program_flow_count > 1_000_000 {
            checked_stderr!("runtime.host-failure: program metadata size limit");
            return 2;
        }
        let data = std::slice::from_raw_parts(
            ptr::addr_of!(lo_program_metadata),
            lo_program_metadata_size as usize,
        );
        let artifact = match Program::parse(data, lo_program_flow_count as usize) {
            Ok(program) => program.artifact,
            Err(message) => {
                checked_stderr!("runtime.host-failure: {message}");
                return 2;
            }
        };
        let Some(artifact) = artifact else {
            checked_stderr!("runtime.host-failure: absent artifact metadata");
            return 2;
        };
        if let Err(error) = writeln!(
            std::io::stdout().lock(),
            "{}",
            canonical_json_value(&artifact)
        ) {
            checked_stderr!("runtime.host-failure: write artifact metadata: {error}");
            return 2;
        }
        return 0;
    }
    let mut format = "text";
    let mut steps = 1_000_000u64;
    let mut error = None;
    let mut format_set = false;
    let mut steps_set = false;
    let mut roots = crate::files::Options::default();
    let mut process_grants = vec![];
    let mut process_roots = vec![];
    let mut process_launcher = String::new();
    let mut launcher_set = false;
    let mut http_grants = vec![];
    let mut ports = vec![];
    let mut device_grant = String::new();
    let mut device_set = false;
    let serving = args.first() == Some(&"serve");
    if args.first() != Some(&"run") && !serving {
        error = Some("artifact requires run or serve without a source path".to_string())
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
        } else if arg == "--allow-device" {
            if device_set || i + 1 == args.len() {
                error = Some("--allow-device requires one selector with run or serve".into());
                break;
            } else {
                i += 1;
                device_set = true;
                device_grant = args[i].to_owned();
                if device_grant.is_empty() || device_grant == "cpu" {
                    error = Some("--allow-device requires a non-CPU selector".into());
                }
            }
        } else if matches!(
            arg,
            "--read-root"
                | "--write-root"
                | "--list-root"
                | "--remove-root"
                | "--open-files-root"
                | "--process-allow"
                | "--process-launcher"
                | "--process-read-root"
                | "--http-allow"
                | "--listen"
        ) && i + 1 < args.len()
        {
            i += 1;
            let value = args[i].to_owned();
            if value.is_empty() {
                error = Some(format!("{arg} requires a nonempty value"));
            }
            match arg {
                "--read-root" => roots.reads.push(value),
                "--write-root" => roots.writes.push(value),
                "--list-root" => roots.lists.push(value),
                "--remove-root" => roots.removes.push(value),
                "--open-files-root" => roots.open_files.push(value),
                "--process-allow" => process_grants.push(value),
                "--process-read-root" => process_roots.push(value),
                "--process-launcher" => {
                    if launcher_set {
                        error = Some("duplicate --process-launcher".into());
                    }
                    launcher_set = true;
                    process_launcher = value;
                }
                "--http-allow" => http_grants.push(value),
                "--listen" => {
                    if !serving {
                        error = Some("--listen requires a port with serve".into());
                    }
                    match value.parse::<u16>() {
                        Ok(port) => ports.push(port),
                        Err(_) => error = Some("invalid listen port".into()),
                    }
                }
                _ => unreachable!(),
            }
        } else {
            error = Some(format!("unknown option {arg:?}"))
        }
        i += 1;
    }
    let http = crate::http::Http::new(&http_grants, &ports);
    let devices = crate::device::Config::new(&device_grant);
    if let Err(message) = &devices {
        if error.is_none() {
            error = Some(message.clone());
        }
    }
    if let Err(message) = &http {
        error = Some(message.clone());
    }
    if let Err(message) =
        crate::process::validate_policy(&process_grants, &process_launcher, &process_roots)
    {
        error = Some(message);
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
    let processes = match crate::process::configure(
        &process_grants,
        &process_launcher,
        &process_roots,
        5000,
        1 << 20,
        1000,
    ) {
        Ok(processes) => Arc::new(processes),
        Err(message) => {
            let fault = if message == crate::process::UNAVAILABLE {
                Fault::Host("runtime.process-unavailable", message)
            } else {
                host(message)
            };
            return report(
                fault,
                &Context::new(program.clone(), steps),
                format == "json",
                None,
            );
        }
    };
    let files = match crate::files::configure_options(&roots) {
        Ok(files) => files,
        Err(message) => {
            let cleanup = processes.close().err();
            return report(
                host(message),
                &Context::new(program.clone(), steps),
                format == "json",
                cleanup.as_deref(),
            );
        }
    };
    let session = Arc::new(Session {
        program: program.clone(),
        steps,
        processes,
        http: Arc::new(http.unwrap()),
        files: std::sync::Mutex::new(files),
        devices: devices.unwrap(),
        serving,
    });
    let mut context = match Context::request(session.clone()) {
        Ok(context) => context,
        Err(fault) => {
            let cleanup = session.close().err().map(|f| f.message());
            return report(
                fault,
                &Context::new(program.clone(), steps),
                format == "json",
                cleanup.as_deref(),
            );
        }
    };
    let status = (|| {
        context.observe_cli_cancel = true;
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
                } else if !crate::tensor::host_publishable(&v) {
                    Err(Fault::Host(
                        "runtime.tensor-publication",
                        "result contains a tensor without an explicit CPU transfer".into(),
                    ))
                } else {
                    Ok(v)
                }
            }
            other => other,
        };
        let result = result.map_err(|fault| context.capture_fault(fault));
        let request_cleanup = context.close().err();
        if serving && result.is_ok() && request_cleanup.is_none() {
            let value = result.as_ref().unwrap_or_else(|_| unreachable!());
            let servers = session.http.snapshot();
            if servers.is_empty() {
                let cleanup = session.close().err().map(|f| f.message());
                return report(
                    Fault::Host(
                        "runtime.no-server",
                        "serve requires an active HTTP server".into(),
                    ),
                    &context,
                    format == "json",
                    cleanup.as_deref(),
                );
            }
            let ready = if format == "json" {
                let result = if program.has_result {
                    format!(",\"result\":{}", value.canonical_json())
                } else {
                    String::new()
                };
                format!(
                    "{{\"command\":\"serve\",\"status\":\"ready\",\"servers\":{}{result}}}",
                    canonical_json_value(&json!(servers))
                )
            } else {
                servers
                    .iter()
                    .map(|s| format!("listening {}", s["address"].as_str().unwrap()))
                    .collect::<Vec<_>>()
                    .join("\n")
            };
            let serving_result = match writeln!(std::io::stdout().lock(), "{ready}") {
                Ok(()) => session.http.serve(session.clone()),
                Err(e) => Err(host(format!("write readiness \"\": {e}"))),
            };
            let cleanup = session.close().err();
            if let Err(fault) = serving_result {
                return report(
                    combine_cleanup(Some(fault), cleanup).unwrap(),
                    &context,
                    format == "json",
                    None,
                );
            }
            if let Some(fault) = cleanup {
                return report(fault, &context, format == "json", None);
            }
            let stopped = if format == "json" {
                "{\"command\":\"serve\",\"status\":\"stopped\"}"
            } else {
                "stopped"
            };
            if let Err(e) = writeln!(std::io::stdout().lock(), "{stopped}") {
                return report(
                    host(format!("write shutdown result \"\": {e}")),
                    &context,
                    format == "json",
                    None,
                );
            }
            return 0;
        }
        let session_cleanup = session.close().err();
        let cleanup = combine_cleanup(request_cleanup, session_cleanup);
        match result {
            Ok(v) => {
                if let Some(fault) = cleanup {
                    return report(fault, &context, format == "json", None);
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
            Err(f) => report(
                combine_cleanup(Some(f), cleanup).unwrap(),
                &context,
                format == "json",
                None,
            ),
        }
    })();
    // The bounded failure is printed before this mandatory ownership join.
    let retained = session.wait_retained();
    if status == 0 {
        if let Err(fault) = retained {
            return report(fault, &context, format == "json", None);
        }
    }
    status
}
