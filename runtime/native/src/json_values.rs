use crate::{
    context::*,
    json::{Document, Kind},
    value::*,
};
use num_traits::ToPrimitive;
use std::sync::Arc;
pub const SUPPORTED: &[&str] = &[
    "JSON.decode",
    "JSON.get",
    "JSON.at",
    "JSON.string",
    "JSON.bool",
    "JSON.int",
    "JSON.float",
    "JSON.kind",
    "JSON.length",
];
pub fn parameter_names(op: &str) -> Vec<&'static str> {
    match op {
        "JSON.decode" => vec!["data"],
        "JSON.get" => vec!["value", "key"],
        "JSON.at" => vec!["value", "index"],
        _ => vec!["value"],
    }
}
pub fn admit(s: &Site) -> Result<(), String> {
    let (inputs, output) = match s.operation.as_str() {
        "JSON.decode" => (vec![atom("Bytes")], atom("JSON.Value")),
        "JSON.get" => (vec![atom("JSON.Value"), atom("String")], atom("JSON.Value")),
        "JSON.at" => (vec![atom("JSON.Value"), atom("Int")], atom("JSON.Value")),
        "JSON.string" | "JSON.kind" => (vec![atom("JSON.Value")], atom("String")),
        "JSON.bool" => (vec![atom("JSON.Value")], atom("Bool")),
        "JSON.int" | "JSON.length" => (vec![atom("JSON.Value")], atom("Int")),
        "JSON.float" => (vec![atom("JSON.Value")], atom("Float")),
        _ => return Err("unknown JSON operation".into()),
    };
    let output = if s.operation == "JSON.kind" {
        output
    } else {
        Type::Fallible(Box::new(output))
    };
    if s.typ != output
        || inputs.len() != s.arguments.len()
        || inputs
            .iter()
            .zip(&s.arguments)
            .any(|(t, a)| !matches!(a,Argument::Value(v) if t==v))
    {
        return Err("JSON operation metadata mismatch".into());
    }
    Ok(())
}
fn value(document: Document) -> Handle {
    new(atom("JSON.Value"), Data::JSON(Arc::new(document)))
}
fn invalid(kind: &'static str, message: &str) -> Fault {
    Fault::Numeric(kind, message.into())
}
fn execute_inner(c: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    if site.operation == "JSON.decode" {
        let Data::Bytes(bytes) = &args[0].data else {
            return Err(host("JSON.decode requires Bytes"));
        };
        return Ok(value(crate::json::parse(c, bytes)?));
    }
    let Data::JSON(document) = &args[0].data else {
        return Err(host("JSON operation requires JSON.Value"));
    };
    let root = document.root();
    let wrong = || invalid("JSON.TypeMismatch", "JSON value has the wrong kind");
    match site.operation.as_str() {
        "JSON.kind" => Ok(text(root.kind())),
        "JSON.get" => {
            let items = root.object().ok_or_else(wrong)?;
            let key = args[1].string();
            let (mut low, mut high) = (0, items.len());
            let mut found = None;
            while low < high {
                let middle = low + (high - low) / 2;
                match crate::json::compare_text(c, &items[middle].0, key)? {
                    std::cmp::Ordering::Equal => {
                        found = Some(items[middle].1);
                        break;
                    }
                    std::cmp::Ordering::Less => low = middle + 1,
                    std::cmp::Ordering::Greater => high = middle,
                }
            }
            let child =
                found.ok_or_else(|| invalid("JSON.FieldAbsent", "JSON object field is absent"))?;
            Ok(value(document.extract(c, child)?))
        }
        "JSON.at" => {
            let items = root.array().ok_or_else(wrong)?;
            let index = args[1]
                .integer()
                .to_usize()
                .and_then(|i| items.get(i))
                .ok_or_else(|| invalid("JSON.InvalidIndex", "JSON array index is out of range"))?;
            Ok(value(document.extract(c, *index)?))
        }
        "JSON.length" => Ok(int(root
            .array()
            .map(|v| v.len())
            .or_else(|| root.object().map(|v| v.len()))
            .ok_or_else(wrong)?)),
        "JSON.string" => {
            let s = root.as_str().ok_or_else(wrong)?;
            c.values.preflight(s.len() as u64 + 16)?;
            crate::json::work(c, s.len() as u64)?;
            Ok(text(s))
        }
        "JSON.bool" => {
            if let Kind::Bool(b) = document.nodes[root.index].kind {
                Ok(boolean(b))
            } else {
                Err(wrong())
            }
        }
        "JSON.int" | "JSON.float" => {
            let Kind::Number(token) = &document.nodes[root.index].kind else {
                return Err(wrong());
            };
            if site.operation == "JSON.int" {
                if token.bytes().any(|b| matches!(b, b'.' | b'e' | b'E')) {
                    return Err(invalid(
                        "JSON.InvalidNumber",
                        "JSON integer requires an integer token",
                    ));
                }
                Ok(int(crate::text_format::integer(
                    c,
                    token,
                    "JSON.InvalidNumber",
                )?))
            } else {
                Ok(new(
                    atom("Float"),
                    Data::Float(crate::text_format::float(c, token)?),
                ))
            }
        }
        _ => Err(host("unknown JSON operation")),
    }
}
pub fn execute(c: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    execute_inner(c, site, args).map_err(|fault| match fault {
        Fault::Numeric(kind, message) => crate::training::recoverable(site, args, kind, &message),
        other => other,
    })
}
