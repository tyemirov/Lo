use super::*;

const CONTEXT: &str = "Process.Context";
const INVALID: &str = "Process.InvalidContext";

fn environment_type() -> Type {
    Type::Map(Box::new(atom("String")), Box::new(atom("String")))
}

fn validate_directory(directory: &str) -> Result<(), String> {
    if !Path::new(directory).is_absolute() || directory.contains('\0') {
        return Err("Process context directory must be absolute and contain no NUL".into());
    }
    Ok(())
}
fn valid_name(name: &str) -> bool {
    !name.is_empty() && !name.contains(['\0', '='])
}
fn validate_environment(environment: &Handle) -> Result<(), String> {
    let Data::Map(entries) = &environment.data else {
        return Err("invalid Process context environment".into());
    };
    for (index, (key, value)) in entries.iter().enumerate() {
        if !valid_name(key.string()) {
            return Err(format!(
                "Process context environment entry {index} has an invalid name"
            ));
        }
        if value.string().contains('\0') {
            return Err(format!(
                "Process context environment entry {index} value contains NUL"
            ));
        }
    }
    Ok(())
}
fn new_context(environment: Handle, directory: Handle, origin: &str) -> Handle {
    value(
        CONTEXT,
        [
            ("environment".into(), environment),
            ("directory".into(), directory),
            ("origin".into(), text(origin)),
        ],
    )
}
fn context_value(environment: Handle, directory: Handle, origin: &str) -> Result<Handle, String> {
    validate_environment(&environment)?;
    validate_directory(directory.string())?;
    Ok(new_context(environment, directory, origin))
}

pub fn capture_context() -> Result<Handle, String> {
    capture_context_with_environment(&std::env::vars_os().collect::<Vec<_>>())
}

pub fn capture_context_with_environment(
    environment: &[(std::ffi::OsString, std::ffi::OsString)],
) -> Result<Handle, String> {
    let directory = std::env::current_dir().map_err(|_| "capture Process directory failed")?;
    let directory = directory
        .to_str()
        .ok_or("capture Process directory: invalid text")?;
    let mut variables = BTreeMap::new();
    for (key, val) in environment {
        let key = key
            .clone()
            .into_string()
            .map_err(|_| "capture Process environment name: invalid text")?;
        let val = val
            .clone()
            .into_string()
            .map_err(|_| "capture Process environment value: invalid text")?;
        variables.insert(key, val);
    }
    let environment = new(
        environment_type(),
        Data::Map(
            variables
                .into_iter()
                .map(|(k, v)| (text(k), text(v)))
                .collect(),
        ),
    );
    context_value(
        environment,
        new(atom("File.Path"), Data::Text(directory.into())),
        "inherited",
    )
}

pub(super) fn redacted(context: &Value) -> Value {
    let mut result = context.clone();
    result.fields.insert(
        "environment".into(),
        new(environment_type(), Data::Map(vec![])),
    );
    result.fields.insert(
        "directory".into(),
        new(atom("File.Path"), Data::Text("/".into())),
    );
    result
}

pub fn parse_initial_context(bytes: &[u8]) -> Result<Handle, String> {
    let input: Json =
        serde_json::from_slice(bytes).map_err(|_| "invalid initial Process context JSON")?;
    let fields = input
        .as_object()
        .ok_or("initial Process context must be an object")?;
    if fields.len() != 2 || !fields.contains_key("environment") || !fields.contains_key("directory")
    {
        return Err("initial Process context has an invalid field set".into());
    }
    let directory = fields["directory"]
        .as_str()
        .ok_or("initial Process directory must be text")?;
    let entries = fields["environment"]
        .as_array()
        .ok_or("initial Process environment must contain pairs")?;
    let mut names = BTreeSet::new();
    let mut environment = vec![];
    for entry in entries {
        let pair = entry
            .as_array()
            .ok_or("initial Process environment entry must be a pair")?;
        if pair.len() != 2 {
            return Err("initial Process environment entry must be a pair".into());
        }
        let name = pair[0]
            .as_str()
            .ok_or("initial Process environment name must be text")?;
        let val = pair[1]
            .as_str()
            .ok_or("initial Process environment value must be text")?;
        if !names.insert(name) {
            return Err("initial Process environment contains a duplicate name".into());
        }
        environment.push((text(name), text(val)));
    }
    context_value(
        new(environment_type(), Data::Map(environment)),
        new(atom("File.Path"), Data::Text(directory.into())),
        "explicit",
    )
}

pub(super) fn admit(site: &Site, args: &[String]) -> Result<(), String> {
    let (expected, output) = match site.operation.as_str() {
        "Process.Context.current" => (vec![], "Process.Context!"),
        "Process.Context.new" => (vec!["Map<String, String>", "File.Path"], "Process.Context!"),
        "Process.Context.override_environment" => {
            (vec![CONTEXT, "Map<String, String>"], "Process.Context!")
        }
        "Process.Context.without_environment" => {
            (vec![CONTEXT, "List<String>"], "Process.Context!")
        }
        "Process.Context.with_directory" => (vec![CONTEXT, "File.Path"], "Process.Context!"),
        "Process.Context.environment_value" => (vec![CONTEXT, "String"], "String!"),
        "Process.Executable.resolve" => (vec![CONTEXT, "String"], "Process.Executable!"),
        _ => return Err("unsupported Process operation".into()),
    };
    if args.iter().map(String::as_str).collect::<Vec<_>>() != expected
        || args.len() != site.arguments.len()
        || site.typ.name() != output
    {
        return Err("Process context metadata type mismatch".into());
    }
    Ok(())
}

pub(super) fn execute(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let result = match site.operation.as_str() {
        "Process.Context.current" => {
            return context
                .session
                .initial_process_context
                .clone()
                .map_err(|message| {
                    Fault::Recoverable(error(
                        site,
                        "Process.ContextUnavailable",
                        &message,
                        record([]),
                    ))
                })
        }
        "Process.Context.new" => context_value(args[0].clone(), args[1].clone(), "explicit"),
        "Process.Context.with_directory" => validate_directory(args[1].string()).map(|()| {
            new_context(
                payload(&args[0]).fields["environment"].clone(),
                args[1].clone(),
                "derived",
            )
        }),
        "Process.Context.override_environment" | "Process.Context.without_environment" => {
            let initial = payload(&args[0]);
            let Data::Map(environment) = &initial.fields["environment"].data else {
                return Err(host("invalid Process context environment"));
            };
            let mut entries = environment.clone();
            if site.operation == "Process.Context.override_environment" {
                if let Err(message) = validate_environment(&args[1]) {
                    return invalid(site, INVALID, &message);
                }
                let Data::Map(updates) = &args[1].data else {
                    return Err(host("invalid Process context updates"));
                };
                for (key, val) in updates {
                    if let Some(entry) =
                        entries.iter_mut().find(|(k, _)| k.string() == key.string())
                    {
                        *entry = (key.clone(), val.clone());
                    } else {
                        entries.push((key.clone(), val.clone()));
                    }
                }
            } else {
                let removed: BTreeSet<_> =
                    args[1].items().iter().map(|name| name.string()).collect();
                if removed.iter().any(|name| !valid_name(name)) {
                    return invalid(site, INVALID, "Process context removal has an invalid name");
                }
                entries.retain(|(key, _)| !removed.contains(key.string()));
            }
            Ok(new_context(
                new(environment_type(), Data::Map(entries)),
                initial.fields["directory"].clone(),
                "derived",
            ))
        }
        "Process.Context.environment_value" => {
            let initial = payload(&args[0]);
            let Data::Map(environment) = &initial.fields["environment"].data else {
                return Err(host("invalid Process context environment"));
            };
            return environment
                .iter()
                .find(|(k, _)| k.string() == args[1].string())
                .map(|(_, v)| v.clone())
                .ok_or_else(|| {
                    Fault::Recoverable(error(
                        site,
                        "Process.EnvironmentAbsent",
                        "Process context variable is absent",
                        record([]),
                    ))
                });
        }
        "Process.Executable.resolve" => return resolve(context, site, &args[0], args[1].string()),
        _ => return Err(host("unsupported Process context operation")),
    };
    result.map_err(|message| Fault::Recoverable(error(site, INVALID, &message, record([]))))
}

fn resolve(context: &mut Context, site: &Site, initial: &Handle, name: &str) -> Outcome {
    use std::ffi::CString;
    let failed = |message: &str| {
        Fault::Recoverable(error(site, "Process.DiscoveryFailed", message, record([])))
    };
    let absent = || {
        Fault::Recoverable(error(
            site,
            "Process.ExecutableAbsent",
            "Process executable is absent",
            record([]),
        ))
    };
    if name.is_empty() || name.contains('\0') {
        return Err(failed("discover Process executable: invalid name"));
    }
    let explicit = name.contains('/');
    let mut candidates = vec![];
    if explicit {
        if !Path::new(name).is_absolute()
            || name != "/"
                && name
                    .split('/')
                    .skip(1)
                    .any(|part| part.is_empty() || part == "." || part == "..")
        {
            return Err(failed(
                "discover Process executable: explicit path must be absolute and canonical",
            ));
        }
        candidates.push(PathBuf::from(name));
    } else {
        let initial = payload(initial);
        let Data::Map(environment) = &initial.fields["environment"].data else {
            return Err(host("invalid Process context environment"));
        };
        let Some((_, path)) = environment.iter().find(|(k, _)| k.string() == "PATH") else {
            return Err(absent());
        };
        for directory in path.string().split(':') {
            if !Path::new(directory).is_absolute()
                || directory != "/"
                    && directory
                        .split('/')
                        .skip(1)
                        .any(|part| part.is_empty() || part == "." || part == "..")
            {
                return Err(failed(
                    "discover Process executable: PATH entries must be absolute and canonical",
                ));
            }
            candidates.push(Path::new(directory).join(name));
        }
    }
    for candidate in candidates {
        context.check_cancel()?;
        let resolved = match fs::canonicalize(&candidate) {
            Ok(resolved) => resolved,
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => continue,
            Err(_) => {
                return Err(failed(
                    "discover Process executable: candidate resolution failed",
                ))
            }
        };
        let entry = match fs::metadata(&resolved) {
            Ok(entry) => entry,
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => continue,
            Err(_) => {
                return Err(failed(
                    "discover Process executable: candidate observation failed",
                ))
            }
        };
        if entry.is_dir() {
            continue;
        }
        let candidate_text = resolved
            .to_str()
            .ok_or_else(|| failed("discover Process executable: candidate is not valid text"))?;
        let path = CString::new(candidate_text)
            .map_err(|_| failed("discover Process executable: invalid candidate"))?;
        extern "C" {
            fn access(path: *const std::os::raw::c_char, mode: i32) -> i32;
        }
        if unsafe { access(path.as_ptr(), 1) } != 0 {
            let err = std::io::Error::last_os_error();
            if !explicit && err.kind() == std::io::ErrorKind::PermissionDenied {
                continue;
            }
            return Err(failed(
                "discover Process executable: candidate cannot execute",
            ));
        }
        return Ok(value(
            "Process.Executable",
            [(
                "path".into(),
                new(atom("File.Path"), Data::Text(candidate_text.into())),
            )],
        ));
    }
    Err(absent())
}
