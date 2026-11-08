use crate::{context::*, exports::*, value::*};
use serde_json::{json, Value as Json};
use std::{
    ffi::CStr,
    os::raw::{c_char, c_int},
    ptr,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
};
thread_local! {static OUTPUT:std::cell::RefCell<Option<Arc<crate::console::Console>>>=const {std::cell::RefCell::new(None)};}
fn raw(stream: &str, bytes: &[u8]) -> Result<(), Fault> {
    OUTPUT.with(|output| {
        output
            .borrow()
            .as_ref()
            .ok_or_else(|| host("Console unavailable"))?
            .raw(stream, bytes)
    })
}
macro_rules! checked_stderr {($($argument:tt)*)=>{if raw("stderr",format!("{}\n",format_args!($($argument)*)).as_bytes()).is_err(){return 2}};}

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
            if fields["operation"]["value"] == "Flow.worklist" {
                message.push_str(&format!(
                    " (Flow.worklist completed {}, pending {}, state {})",
                    value_text(&fields["completed_count"]),
                    value_text(&fields["pending_count"]),
                    value_text(&fields["state"])
                ));
            } else {
                message.push_str(&format!(
                    " (Flow.fold completed {}/{}, state {})",
                    value_text(&fields["completed_count"]),
                    value_text(&fields["total_count"]),
                    value_text(&fields["state"])
                ));
            }
        }
    }
    message
}
pub(crate) fn fault_diagnostic(f: Fault, context: &Context, cleanup: Option<&str>) -> (Json, i32) {
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
                d["span"] = site.source_record();
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
    (d, status)
}
fn report(f: Fault, context: &Context, json_format: bool, cleanup: Option<&str>) -> i32 {
    let (mut d, status) = fault_diagnostic(f, context, cleanup);
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
    if argc == 2 && CStr::from_ptr(*argv.add(1)).to_bytes() == b"--lo-console-writer" {
        return crate::console::worker();
    }
    CANCELLED.store(false, Ordering::Relaxed);
    let console = match crate::console::Console::cli() {
        Ok(console) => Arc::new(console),
        Err(_) => return 2,
    };
    OUTPUT.with(|output| *output.borrow_mut() = Some(console.clone()));
    let status = cli_main(argc, argv, _entry_environment, console.clone());
    let cleanup = console.close();
    OUTPUT.with(|output| *output.borrow_mut() = None);
    if cleanup.is_err() {
        2
    } else {
        status
    }
}
unsafe fn cli_main(
    argc: c_int,
    argv: *const *const c_char,
    _entry_environment: *const *const c_char,
    console: Arc<crate::console::Console>,
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
    let mut program_utf8_error = false;
    let mut after_separator = false;
    for i in 1..argc {
        let raw = CStr::from_ptr(*argv.add(i as usize));
        if raw.to_bytes() == b"--" {
            after_separator = true;
        }
        let s = match raw.to_str() {
            Ok(v) => v,
            Err(_) if after_separator => {
                program_utf8_error = true;
                ""
            }
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
        if raw(
            "stdout",
            format!("{}\n", canonical_json_value(&artifact)).as_bytes(),
        )
        .is_err()
        {
            return 2;
        }
        return 0;
    }
    let mut format = "text";
    let mut steps = 1_000_000u64;
    let mut error = None;
    let mut format_set = false;
    let mut steps_set = false;
    let mut console_maximum = 1048576u64;
    let mut console_limit_set = false;
    let mut limits = crate::value_resources::Limits::default();
    let mut limit_flags = std::collections::BTreeSet::new();
    let mut roots = crate::files::Options::default();
    let mut lock_paths = vec![];
    let mut lock_entry = false;
    let mut process_grants = vec![];
    let mut process_roots = vec![];
    let mut process_writes = vec![];
    let mut process_prunes = vec![];
    let mut process_endpoints = vec![];
    let mut process_launcher = String::new();
    let mut process_limits = crate::process::Limits::default();
    let mut process_limit_flags = std::collections::BTreeSet::new();
    let mut launcher_set = false;
    let mut http_grants = vec![];
    let mut ports = vec![];
    let mut device_grant = String::new();
    let mut device_set = false;
    let mut program_arguments = vec![];
    let serving = args.first() == Some(&"serve");
    if args.first() != Some(&"run") && !serving {
        error = Some("artifact requires run or serve without a source path".to_string())
    }
    let mut i = 1;
    while i < args.len() {
        let arg = args[i];
        if arg == "--" {
            program_arguments = args[i + 1..]
                .iter()
                .map(|argument| (*argument).to_owned())
                .collect();
            if program_utf8_error {
                error = Some("program argument requires valid UTF-8 without NUL".into());
            } else if let Err(message) =
                crate::host_invocation::validate_arguments(&program_arguments)
            {
                error = Some(message);
            }
            break;
        } else if arg == "--lock-entry" && !lock_entry {
            lock_entry = true;
        } else if arg == "--lock-file" && i + 1 < args.len() {
            i += 1;
            lock_paths.push(args[i].to_owned());
        } else if arg == "--format" && !format_set && i + 1 < args.len() {
            i += 1;
            format = args[i];
            format_set = true;
            if format != "text" && format != "json" {
                error = Some(format!("unknown output format {format:?}"))
            }
        } else if arg == "--process-forward-output" && process_limits.forward_streams == 0 {
            process_limits.forward_streams = 3;
        } else if matches!(
            arg,
            "--max-process-ms" | "--max-process-cleanup-ms" | "--max-process-output-bytes"
        ) {
            if !process_limit_flags.insert(arg) || i + 1 == args.len() {
                error = Some(format!("{arg} requires one positive integer"));
                break;
            }
            i += 1;
            match args[i].parse::<u64>() {
                Ok(value) if value > 0 => match arg {
                    "--max-process-ms" => process_limits.timeout_ms = value,
                    "--max-process-cleanup-ms" => process_limits.cleanup_ms = value,
                    _ => match usize::try_from(value) {
                        Ok(value) => process_limits.output_bytes = value,
                        Err(_) => error = Some(format!("invalid {arg}")),
                    },
                },
                _ => error = Some(format!("invalid {arg}: value must be positive")),
            }
        } else if arg == "--max-console-bytes" && !console_limit_set && i + 1 < args.len() {
            console_limit_set = true;
            i += 1;
            match args[i].parse::<u64>() {
                Ok(value) => console_maximum = value,
                Err(_) => error = Some("invalid --max-console-bytes".into()),
            }
        } else if arg == "--max-steps" && !steps_set && i + 1 < args.len() {
            i += 1;
            steps_set = true;
            match args[i].parse::<u64>() {
                Ok(v) if v > 0 => steps = v,
                _ => error = Some("invalid --max-steps: execution steps must be positive".into()),
            }
        } else if matches!(
            arg,
            "--max-value-bytes"
                | "--max-live-bytes"
                | "--max-data-bytes"
                | "--max-list-items"
                | "--max-file-entries"
                | "--max-file-directories"
        ) {
            if !limit_flags.insert(arg) || i + 1 == args.len() {
                error = Some(format!("{arg} requires one non-negative integer"));
                break;
            }
            i += 1;
            let value = match args[i].parse::<u64>() {
                Ok(value) => value,
                Err(_) => {
                    error = Some(format!("invalid {arg}: expected a non-negative integer"));
                    break;
                }
            };
            match arg {
                "--max-value-bytes" => limits.value_bytes = value,
                "--max-live-bytes" => limits.live_bytes = value,
                "--max-data-bytes" => limits.data_bytes = value,
                "--max-list-items" => limits.list_items = value,
                "--max-file-entries" => limits.file_entries = value,
                "--max-file-directories" => limits.file_directories = value,
                _ => unreachable!(),
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
            "--process-write-root" | "--process-prune-root" | "--process-unix-endpoint"
        ) && i + 1 == args.len()
        {
            error = Some(format!("{arg} requires a value with run or serve"));
        } else if matches!(
            arg,
            "--read-root"
                | "--write-root"
                | "--list-root"
                | "--remove-root"
                | "--observe-tree-root"
                | "--remove-tree-root"
                | "--open-files-root"
                | "--process-allow"
                | "--process-launcher"
                | "--process-read-root"
                | "--process-write-root"
                | "--process-prune-root"
                | "--process-unix-endpoint"
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
                "--observe-tree-root" => roots.observe_trees.push(value),
                "--remove-tree-root" => roots.remove_trees.push(value),
                "--open-files-root" => roots.open_files.push(value),
                "--process-allow" => process_grants.push(value),
                "--process-read-root" => process_roots.push(value),
                "--process-write-root" => process_writes.push(value),
                "--process-prune-root" => process_prunes.push(value),
                "--process-unix-endpoint" => process_endpoints.push(value),
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
    let process_policy = crate::process::Policy::new(
        process_grants,
        process_launcher,
        process_roots,
        &process_writes,
        &process_prunes,
        &process_endpoints,
    );
    console.configure(console_maximum, format == "json", serving);
    if let Ok(policy) = &process_policy {
        if let Err(message) = policy.protect_images(&console.image_paths()) {
            error = Some(message);
        }
    }
    if let Err(message) = &process_policy {
        error = Some(message.clone());
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
    if serving
        && program
            .result_type
            .as_ref()
            .is_some_and(|typ| typ.base_ref() == &atom("Program.Exit"))
    {
        return report(
            host("serve does not accept a Program.Exit entry result"),
            &Context::new(program.clone(), steps),
            format == "json",
            None,
        );
    }
    signal(2, stop as *const () as usize);
    signal(15, stop as *const () as usize);
    let processes = match crate::process::configure(&process_policy.unwrap(), process_limits) {
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
    let entry = console
        .program_entry()
        .unwrap_or_else(|| Arc::new(crate::program_entry::Entry::unavailable()));
    if lock_entry {
        match &entry.path {
            Ok(path) => {
                if !lock_paths.contains(path) {
                    lock_paths.push(path.clone());
                }
            }
            Err(_) => {
                let mut files = files;
                let cleanup = combine_cleanup(
                    files.close().err().map(host),
                    combine_cleanup(
                        processes.close().err().map(host),
                        processes.wait_retained().err().map(host),
                    ),
                );
                return report(
                    combine_cleanup(Some(host("entry lock identity is unavailable")), cleanup)
                        .unwrap(),
                    &Context::new(program.clone(), steps),
                    format == "json",
                    None,
                );
            }
        }
    }
    let locks = match crate::locks::Locks::configure_with_entry(&lock_paths, Some(entry.as_ref())) {
        Ok(locks) => locks,
        Err(message) => {
            let mut files = files;
            let cleanup = combine_cleanup(
                files.close().err().map(host),
                combine_cleanup(
                    processes.close().err().map(host),
                    processes.wait_retained().err().map(host),
                ),
            );
            return report(
                combine_cleanup(
                    Some(if lock_paths.len() > crate::locks::MAX_LOCKS {
                        limit("lock grants")
                    } else {
                        host(message)
                    }),
                    cleanup,
                )
                .unwrap(),
                &Context::new(program.clone(), steps),
                format == "json",
                None,
            );
        }
    };
    let captured = crate::host_invocation::capture_inputs(&program_arguments);
    let initial_program_path = entry.value();
    let session = Arc::new(Session {
        initial_program_path,
        entry,
        locks: Arc::new(locks),
        initial_process_context: captured.process,
        initial_host_invocation: captured.host,
        observations: std::sync::Mutex::new(crate::host_observations::Observers::default()),
        program: program.clone(),
        steps,
        limits,
        processes,
        http: Arc::new(http.unwrap()),
        files: std::sync::Mutex::new(files),
        devices: devices.unwrap(),
        serving,
        console: console.clone(),
    });
    let mut context = match Context::request(session.clone()) {
        Ok(context) => context,
        Err(fault) => {
            let cleanup = combine_cleanup(session.close().err(), session.wait_retained().err());
            return report(
                combine_cleanup(Some(fault), cleanup).unwrap(),
                &Context::new(program.clone(), steps),
                format == "json",
                None,
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
                let cleanup = combine_cleanup(session.close().err(), session.wait_retained().err());
                let fault = Fault::Host(
                    "runtime.no-server",
                    "serve requires an active HTTP server".into(),
                );
                return report(
                    combine_cleanup(Some(fault), cleanup).unwrap(),
                    &context,
                    format == "json",
                    None,
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
            let serving_result =
                match console.raw_completion("stdout", format!("{ready}\n").as_bytes()) {
                    Ok(()) => session.http.serve(session.clone()),
                    Err(e) => Err(e),
                };
            let cleanup = combine_cleanup(session.close().err(), session.wait_retained().err());
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
            if let Err(e) = console.raw_completion("stdout", format!("{stopped}\n").as_bytes()) {
                return report(e, &context, format == "json", None);
            }
            return 0;
        }
        let session_cleanup = combine_cleanup(session.close().err(), session.wait_retained().err());
        let cleanup = combine_cleanup(request_cleanup, session_cleanup);
        match result {
            Ok(v) => {
                if let Some(fault) = cleanup {
                    return report(fault, &context, format == "json", None);
                }
                let terminal = crate::program_exit::payload(&v);
                if let Some(value) = terminal {
                    if let Err(fault) = console.terminal(&context, value) {
                        return report(fault, &context, format == "json", None);
                    }
                }
                let payload = if let Some(value) = terminal {
                    if format == "json" {
                        Some(format!(
                            "{{\"command\":\"run\",\"status\":\"ok\",\"exit_code\":{}}}",
                            value.code
                        ))
                    } else {
                        None
                    }
                } else if program.has_result {
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
                    if let Err(e) =
                        console.raw_context(&context, "stdout", format!("{payload}\n").as_bytes())
                    {
                        return report(e, &context, format == "json", None);
                    }
                }
                terminal.map_or(0, |value| value.code as i32)
            }
            Err(f) => report(
                combine_cleanup(Some(f), cleanup).unwrap(),
                &context,
                format == "json",
                None,
            ),
        }
    })();
    status
}
