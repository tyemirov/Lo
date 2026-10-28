use super::*;
use serde::de::{Error as _, MapAccess, Visitor};
use serde::Deserialize as _;
use serde::Deserializer as _;
use std::fmt;

#[derive(Clone, Default)]
pub(super) struct Scope {
    pub writes: Vec<String>,
    pub prunes: Vec<String>,
    pub endpoints: Vec<String>,
}

pub struct Policy {
    pub(super) grants: Vec<String>,
    pub(super) launcher: String,
    pub(super) roots: Vec<String>,
    pub(super) scopes: BTreeMap<String, Scope>,
}

impl Policy {
    pub fn new(
        grants: Vec<String>,
        launcher: String,
        roots: Vec<String>,
        writes: &[String],
        prunes: &[String],
        endpoints: &[String],
    ) -> Result<Self, String> {
        let mut resources = vec![];
        for (kind, values) in [(0, writes), (1, prunes), (2, endpoints)] {
            for raw in values {
                let (executable, path) =
                    parse_scope(raw, if kind == 2 { "endpoint" } else { "root" }).map_err(
                        |error| {
                            format!(
                                "{}: {error}",
                                [
                                    "--process-write-root",
                                    "--process-prune-root",
                                    "--process-unix-endpoint"
                                ][kind]
                            )
                        },
                    )?;
                resources.push((kind, executable, path));
            }
        }
        if grants.is_empty() && launcher.is_empty() && roots.is_empty() && !resources.is_empty() {
            return Err(
                "Process scopes require executable grants, a launcher, and read roots".into(),
            );
        }
        validate_base_policy(&grants, &launcher, &roots)?;
        let mut scopes: BTreeMap<String, Scope> = grants
            .iter()
            .map(|grant| (grant.split_once(',').unwrap().1.into(), Scope::default()))
            .collect();
        for (kind, executable, path) in resources {
            let label = ["write root", "prune root", "Unix endpoint"][kind];
            let scope = scopes
                .get_mut(&executable)
                .ok_or_else(|| format!("Process {label} requires an exact granted executable"))?;
            if !policy_path(&path) {
                return Err(format!(
                    "Process {label} requires a clean absolute NUL-free path"
                ));
            }
            let values = match kind {
                0 => &mut scope.writes,
                1 => &mut scope.prunes,
                2 => &mut scope.endpoints,
                _ => unreachable!(),
            };
            if values.contains(&path) {
                return Err(format!("duplicate Process {label}"));
            }
            values.push(path);
        }
        for (executable, scope) in &scopes {
            let mutations: Vec<_> = scope.writes.iter().chain(&scope.prunes).collect();
            for (index, root) in mutations.iter().enumerate() {
                for other in &mutations[..index] {
                    if contains(root, other) || contains(other, root) {
                        return Err(format!("Process mutation roots overlap for {executable:?}"));
                    }
                }
                if contains(root, &launcher) {
                    return Err("Process mutation root contains the launcher".into());
                }
                for image in scopes.keys() {
                    if contains(root, image) {
                        return Err("Process mutation root contains a registered executable".into());
                    }
                }
                if !roots.iter().any(|read| contains(read, root)) {
                    return Err(
                        "Process mutation root requires a containing child read root".into(),
                    );
                }
                if scope
                    .endpoints
                    .iter()
                    .any(|endpoint| contains(root, endpoint))
                {
                    return Err("Process Unix endpoint overlaps a mutation root".into());
                }
            }
        }
        Ok(Self {
            grants,
            launcher,
            roots,
            scopes,
        })
    }
}

fn contains(root: &str, path: &str) -> bool {
    Path::new(path).starts_with(root)
}

struct ScopeVisitor<'a> {
    field: &'a str,
}
impl<'de> Visitor<'de> for ScopeVisitor<'_> {
    type Value = (String, String);
    fn expecting(&self, output: &mut fmt::Formatter) -> fmt::Result {
        output.write_str("a Process scope object")
    }
    fn visit_map<M: MapAccess<'de>>(self, mut map: M) -> Result<Self::Value, M::Error> {
        let mut fields = BTreeMap::new();
        while let Some(name) = map.next_key::<String>()? {
            if name != "executable" && name != self.field {
                return Err(M::Error::custom("unknown Process scope field"));
            }
            if fields.contains_key(&name) {
                return Err(M::Error::custom("duplicate Process scope field"));
            }
            let raw = map.next_value::<Json>()?;
            let value = raw
                .as_str()
                .ok_or_else(|| M::Error::custom("Process scope field must contain text"))?;
            fields.insert(name, value.to_owned());
        }
        if fields.len() != 2
            || fields.get("executable").is_none_or(String::is_empty)
            || fields.get(self.field).is_none_or(String::is_empty)
        {
            return Err(M::Error::custom(format!(
                "Process scope requires executable and {}",
                self.field
            )));
        }
        Ok((
            fields.remove("executable").unwrap(),
            fields.remove(self.field).unwrap(),
        ))
    }
}

fn parse_scope(raw: &str, field: &str) -> Result<(String, String), String> {
    if !raw.trim_start().starts_with('{') {
        return Err(format!(
            "expected one JSON object with executable and {field}"
        ));
    }
    let mut validation = serde_json::Deserializer::from_str(raw);
    Json::deserialize(&mut validation).map_err(|_| "malformed Process scope JSON")?;
    validation
        .end()
        .map_err(|_| "Process scope must contain one JSON object")?;
    let mut decoder = serde_json::Deserializer::from_str(raw);
    let result = decoder
        .deserialize_map(ScopeVisitor { field })
        .map_err(|error| {
            error
                .to_string()
                .split(" at line ")
                .next()
                .unwrap()
                .to_owned()
        })?;
    decoder
        .end()
        .map_err(|_| "Process scope must contain one JSON object")?;
    Ok(result)
}

pub(super) fn configuration(
    processes: &Processes,
    commands: &[String],
    executable: &str,
    scope: &Scope,
) -> Result<Vec<u8>, String> {
    let quote = |path: &str| format!("\"{}\"", path.replace('\\', "\\\\").replace('"', "\\\""));
    let mut lines = vec![
        "(version 1)".into(),
        "(deny default (with no-log))".into(),
        "(allow file-read* (subpath \"/usr/lib\") (subpath \"/System/Library\"))".into(),
        "(allow file-read* file-test-existence (literal \"/\"))".into(),
        "(deny process-fork (with no-log))".into(),
        "(allow signal (target self))".into(),
    ];
    for image in commands {
        lines.push(format!("(allow process-exec (literal {}))", quote(image)));
        lines.push(format!("(allow file-read* (literal {}))", quote(image)));
    }
    for root in &processes.roots {
        lines.push(format!("(allow file-read* (subpath {}))", quote(root)));
    }
    for root in &scope.writes {
        lines.push(format!("(allow file-write* (subpath {}))", quote(root)));
    }
    for root in &scope.prunes {
        lines.push(format!(
            "(allow file-write-unlink (subpath {}))",
            quote(root)
        ));
    }
    for endpoint in &scope.endpoints {
        lines.push(format!("(allow file-read* (literal {}))", quote(endpoint)));
        lines.push(format!(
            "(allow network-outbound (remote unix-socket (literal {})))",
            quote(endpoint)
        ));
    }
    for image in commands.iter().chain(std::iter::once(&processes.launcher)) {
        lines.push(format!("(deny file-write* (literal {}))", quote(image)));
    }
    let profile = lines.join("\n") + "\n";
    if profile.len() > 32768 {
        return Err("Process startup failed during input".into());
    }
    let directory = Path::new(&processes.launcher)
        .parent()
        .unwrap()
        .to_str()
        .unwrap()
        .to_owned();
    let mut entries = vec![profile, directory, executable.into()];
    entries.extend(commands.iter().cloned());
    entries.extend(processes.roots.iter().cloned());
    let configuration = frame(
        b"LOC2",
        &entries,
        commands.len(),
        processes.roots.len(),
        32768,
    )?;
    if configuration.len() > 65552 {
        return Err("Process startup failed during input".into());
    }
    Ok(configuration)
}
