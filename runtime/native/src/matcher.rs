use crate::{context::*, value::*};
use serde_json::{json, Value as Json};
use std::io::{self, Write};

pub const CASE_NEW: &str = "Match.Case.new";
pub const NEW: &str = "Match.new";
pub const APPLY: &str = "Match.apply";
pub const SUPPORTED: &[&str] = &[CASE_NEW, NEW, APPLY];

pub fn read_view_allowed(input: &Type, output: &Type) -> bool {
    match (input, output) {
        (Type::Matcher(input_kind, input), Type::Matcher(output_kind, output)) => {
            input_kind == output_kind
                && !input.reads
                && output.reads
                && input.input == output.input
                && input.output == output.output
        }
        _ => false,
    }
}

#[derive(Clone)]
struct Callback {
    index: usize,
    name: String,
}

impl Callback {
    fn from_flow(index: usize, flows: &[Flow]) -> Self {
        Self {
            index,
            name: flows[index].name.clone(),
        }
    }
}

#[derive(Clone)]
pub struct Case {
    predicate: Callback,
    handler: Callback,
}

impl Case {
    pub fn weight(&self) -> u64 {
        16 + self.predicate.name.len() as u64 + self.handler.name.len() as u64
    }

    pub fn json(&self) -> Json {
        json!({"predicate": self.predicate.name, "handler": self.handler.name})
    }

    pub fn write_json(&self, output: &mut impl Write) -> io::Result<()> {
        output.write_all(b"{\"handler\":")?;
        crate::value_output::quote(output, &self.handler.name)?;
        output.write_all(b",\"predicate\":")?;
        crate::value_output::quote(output, &self.predicate.name)?;
        output.write_all(b"}")
    }
}

#[derive(Clone)]
pub struct Matcher {
    cases: Vec<Case>,
    default: Callback,
}

impl Matcher {
    pub fn weight(&self) -> u64 {
        self.cases
            .iter()
            .fold(8 + self.default.name.len() as u64, |weight, case| {
                weight.saturating_add(16 + case.weight())
            })
    }

    pub fn json(&self) -> Json {
        json!({
            "cases": self.cases.iter().map(Case::json).collect::<Vec<_>>(),
            "default": self.default.name,
        })
    }

    pub fn write_json(&self, output: &mut impl Write) -> io::Result<()> {
        output.write_all(b"{\"cases\":[")?;
        for (index, case) in self.cases.iter().enumerate() {
            if index != 0 {
                output.write_all(b",")?;
            }
            case.write_json(output)?;
        }
        output.write_all(b"],\"default\":")?;
        crate::value_output::quote(output, &self.default.name)?;
        output.write_all(b"}")
    }
}

pub fn admit(site: &Site, flows: &[Flow]) -> Result<(), String> {
    let invalid = || format!("invalid native matcher contract {}", site.operation);
    match (site.operation.as_str(), site.arguments.as_slice()) {
        (CASE_NEW, [Argument::Flow(predicate), Argument::Flow(handler)]) => {
            let Type::Matcher("Match.Case", signature) = &site.typ else {
                return Err(invalid());
            };
            let input = &signature.input;
            let output = &signature.output;
            let predicate = flows.get(*predicate).ok_or_else(invalid)?;
            let handler = flows.get(*handler).ok_or_else(invalid)?;
            if predicate.parameters.len() != 1
                || predicate.parameters[0].1 != *input
                || predicate.output != atom("Bool")
                || handler.parameters.len() != 1
                || handler.parameters[0].1 != *input
                || handler.output != *output
            {
                return Err(invalid());
            }
        }
        (NEW, [Argument::Value(cases), Argument::Flow(default)]) => {
            let Type::Matcher("Match", signature) = &site.typ else {
                return Err(invalid());
            };
            let input = &signature.input;
            let output = &signature.output;
            let Type::List(cases) = cases else {
                return Err(invalid());
            };
            let Type::Matcher("Match.Case", case_signature) = cases.as_ref() else {
                return Err(invalid());
            };
            let default = flows.get(*default).ok_or_else(invalid)?;
            if case_signature.input != *input
                || case_signature.output != *output
                || (case_signature.reads && !signature.reads)
                || default.parameters.len() != 1
                || default.parameters[0].1 != *input
                || default.output != *output
            {
                return Err(invalid());
            }
        }
        (APPLY, [Argument::Value(request), Argument::Value(matcher)]) => {
            let Type::Matcher("Match", signature) = matcher else {
                return Err(invalid());
            };
            let input = &signature.input;
            let output = &signature.output;
            if request != input || site.typ != *output {
                return Err(invalid());
            }
        }
        _ => return Err(invalid()),
    }
    Ok(())
}

/// Raw context access permits native callbacks to reenter the runtime.
pub unsafe fn execute(context: *mut Context, site: &Site, args: &[Handle]) -> Outcome {
    match site.operation.as_str() {
        CASE_NEW => {
            let (Data::Flow(predicate), Data::Flow(handler)) = (&args[0].data, &args[1].data)
            else {
                return Err(host("matcher case callback identity mismatch"));
            };
            let flows = &(*context).program.flows;
            Ok(new(
                site.typ.clone(),
                Data::MatchCase(Case {
                    predicate: Callback::from_flow(*predicate, flows),
                    handler: Callback::from_flow(*handler, flows),
                }),
            ))
        }
        NEW => {
            let Data::Flow(default) = &args[1].data else {
                return Err(host("matcher default callback identity mismatch"));
            };
            let cases = args[0]
                .items()
                .iter()
                .map(|case| match &case.data {
                    Data::MatchCase(case) => Ok(case.clone()),
                    _ => Err(host("matcher case value mismatch")),
                })
                .collect::<Result<Vec<_>, _>>()?;
            Ok(new(
                site.typ.clone(),
                Data::Matcher(Matcher {
                    cases,
                    default: Callback::from_flow(*default, &(*context).program.flows),
                }),
            ))
        }
        APPLY => apply(context, site, &args[0], &args[1]),
        _ => Err(host("unknown matcher operation")),
    }
}

unsafe fn apply(context: *mut Context, site: &Site, request: &Handle, matcher: &Handle) -> Outcome {
    let Data::Matcher(matcher) = &matcher.data else {
        return Err(host("matcher value mismatch"));
    };
    let scope = (*context).scope_enter();
    let result = (|| {
        for case in &matcher.cases {
            (*context).step()?;
            let selected = crate::exports::invoke(
                context,
                case.predicate.index,
                std::slice::from_ref(request),
                site.id,
            )?
            .bool();
            (*context).scope_keep(scope, &[]);
            if selected {
                return crate::exports::invoke(
                    context,
                    case.handler.index,
                    std::slice::from_ref(request),
                    site.id,
                );
            }
        }
        crate::exports::invoke(
            context,
            matcher.default.index,
            std::slice::from_ref(request),
            site.id,
        )
    })()
    .and_then(|value| match &value.data {
        Data::Success(value) => Ok(value.clone()),
        Data::Failure(error) => Err(Fault::Recoverable(error.clone())),
        _ => Ok(value),
    });
    let output = result.as_ref().ok().cloned();
    (*context).scope_close(scope, output.as_slice());
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    fn metadata() -> Json {
        let types = [
            ("Int", "Int"),
            ("Bool", "Bool"),
            ("String", "String"),
            ("Match.Case<Int, Int>", "Match.Case"),
            ("Match<Int, Int>", "Match"),
            ("List<Match.Case<Int, Int>>", "List"),
        ];
        let operation = |name: &str, typ: usize, arguments: Json| {
            json!({
                "span": 0, "action": "operation", "type": typ, "owner": "program", "flow": 0,
                "payload": {"operation": name, "labels": [], "arguments": arguments},
            })
        };
        json!({
            "abi": "lo-native-3",
            "has_result": false, "result_type": null,
            "source": {"path": "matcher.lo", "sha256": "0".repeat(64)},
            "types": types.iter().enumerate().map(|(id, (name, kind))| {
                json!({"id": id, "name": name, "kind": kind})
            }).collect::<Vec<_>>(),
            "files": ["matcher.lo"],
            "spans": [{
                "file_index": 0,
                "start": {"offset": 0, "line": 1, "column": 1},
                "end": {"offset": 1, "line": 1, "column": 2},
            }],
            "locations": [
                operation(CASE_NEW, 3, json!([
                    {"kind": "flow", "type": 0, "flow": 0},
                    {"kind": "flow", "type": 0, "flow": 1},
                ])),
                operation(NEW, 4, json!([
                    {"kind": "value", "type": 5, "flow": 0},
                    {"kind": "flow", "type": 0, "flow": 1},
                ])),
                operation(APPLY, 0, json!([
                    {"kind": "value", "type": 0, "flow": 0},
                    {"kind": "value", "type": 4, "flow": 0},
                ])),
            ],
            "flows": [
                {"id": 0, "identity": "predicate", "location": 0, "name": "One",
                    "handler": null, "parameters": [{"name": "value", "type": 0}], "output": 1},
                {"id": 1, "identity": "handler", "location": 1, "name": "Keep",
                    "handler": null, "parameters": [{"name": "value", "type": 0}], "output": 0},
            ],
            "derivatives": [],
        })
    }

    #[test]
    fn matcher_admission_rejects_inconsistent_callback_and_apply_types() {
        let valid = metadata();
        assert!(Program::parse(valid.to_string().as_bytes(), 2).is_ok());
        for (name, category, index, field, value) in [
            ("predicate result", "flows", 0, "output", json!(0)),
            ("handler result", "flows", 1, "output", json!(2)),
            ("apply result", "locations", 2, "type", json!(2)),
        ] {
            let mut invalid = valid.clone();
            invalid[category][index][field] = value;
            assert!(
                Program::parse(invalid.to_string().as_bytes(), 2).is_err(),
                "{name} was admitted"
            );
        }
        let mut invalid = valid;
        invalid["locations"][2]["payload"]["arguments"][0]["type"] = json!(2);
        assert!(Program::parse(invalid.to_string().as_bytes(), 2).is_err());
    }

    #[test]
    fn matcher_values_render_and_snapshot_without_callback_payloads() {
        let program = Program::parse(metadata().to_string().as_bytes(), 2).unwrap();
        let payload = Case {
            predicate: Callback::from_flow(0, &program.flows),
            handler: Callback::from_flow(1, &program.flows),
        };
        let case = new(
            program.sites[0].typ.clone(),
            Data::MatchCase(payload.clone()),
        );
        let matcher = new(
            program.sites[1].typ.clone(),
            Data::Matcher(Matcher {
                cases: vec![payload],
                default: Callback::from_flow(1, &program.flows),
            }),
        );
        let nested = record([
            ("rules".into(), matcher.clone()),
            ("case".into(), case.clone()),
        ]);
        assert!(!nested.contains_storage());
        assert_eq!(case.weight, 16 + 16 + 3 + 4);
        assert_eq!(matcher.weight, 16 + 8 + 4 + 16 + 16 + 3 + 4);
        assert_eq!(
            matcher.json()["value"],
            json!({"cases": [{"handler": "Keep", "predicate": "One"}], "default": "Keep"})
        );
        let rendered: Json = serde_json::from_str(&nested.canonical_json()).unwrap();
        assert_eq!(rendered, nested.json());
        assert_eq!(snapshot(&nested).canonical_json(), nested.canonical_json());
        assert_eq!(
            canonical_json_value(&matcher.json()),
            matcher.canonical_json()
        );
        assert_eq!(
            Type::parse("Match<Record{x: Int}, List<Int!>>")
                .unwrap()
                .name(),
            "Match<Record{x: Int}, List<Int!>>"
        );
        assert!(Type::parse("Match<Int>").is_err());
    }

    #[test]
    fn matcher_read_qualifier_only_widens_its_own_effect() {
        let pure = Type::parse("Match<Int, Int>").unwrap();
        let reading = Type::parse("Match<Int, Int, Read>").unwrap();
        assert_eq!(reading.name(), "Match<Int, Int, Read>");
        assert!(read_view_allowed(&pure, &reading));
        assert!(!read_view_allowed(&reading, &pure));
        assert!(!read_view_allowed(&pure, &pure));
        assert!(!read_view_allowed(
            &pure,
            &Type::parse("Match.Case<Int, Int, Read>").unwrap()
        ));
        assert!(!read_view_allowed(
            &Type::parse("Match<Match<Int, Int>, Int>").unwrap(),
            &Type::parse("Match<Match<Int, Int, Read>, Int, Read>").unwrap()
        ));
        assert!(Type::parse("Match<Int, Int, Write>").is_err());
        assert!(Type::parse("Match<Int, Int, Read, Read>").is_err());
    }

    #[test]
    fn matcher_admission_preserves_read_case_effects() {
        let mut reading = metadata();
        reading["types"][3]["name"] = json!("Match.Case<Int, Int, Read>");
        reading["types"][4]["name"] = json!("Match<Int, Int, Read>");
        reading["types"][5]["name"] = json!("List<Match.Case<Int, Int, Read>>");
        assert!(Program::parse(reading.to_string().as_bytes(), 2).is_ok());

        reading["types"][4]["name"] = json!("Match<Int, Int>");
        assert!(Program::parse(reading.to_string().as_bytes(), 2).is_err());

        let mut pure_cases = metadata();
        pure_cases["types"][4]["name"] = json!("Match<Int, Int, Read>");
        assert!(Program::parse(pure_cases.to_string().as_bytes(), 2).is_ok());
    }
}
