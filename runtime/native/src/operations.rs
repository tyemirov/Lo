use crate::{context::*, value::*};
use num_bigint::BigInt;
use num_traits::{FromPrimitive, One, ToPrimitive, Zero};
use std::cmp::Ordering;
pub const SUPPORTED: &[&str] = &[
    "Int.new",
    "String.new",
    "Bool.new",
    "Float.new",
    "Scalar.new",
    "File.Path.new",
    "Bytes.new",
    "UTF8.Bytes.new",
    "UTF8.String.new",
    "String.FromScalars.new",
    "List.new",
    "Record.new",
    "Map.new",
    "Range.new",
    "String.concat",
    "String.upper",
    "String.lower",
    "String.length",
    "String.eq",
    "Scalar.eq",
    "File.Path.eq",
    "Int.to_string",
    "Int.to_float",
    "Float.to_int",
    "Float.to_string",
    "Float.add",
    "Float.sub",
    "Float.mul",
    "Float.div",
    "Float.sqrt",
    "Float.eq",
    "Float.lt",
    "Float.lte",
    "Float.gt",
    "Float.gte",
    "Math.add",
    "Math.sub",
    "Math.mul",
    "Math.div",
    "Math.mod",
    "Math.lt",
    "Math.lte",
    "Math.gt",
    "Math.gte",
    "Math.eq",
    "Math.sum",
    "Math.product",
    "Math.min",
    "Math.max",
    "List.length",
    "Range.contains",
    "Range.is_empty",
    "Record.merge",
    "Map.has_key",
    "Map.get",
    "Error.new",
    "List.map",
    "List.filter",
    "List.fold",
    "List.flat_map",
    "List.unfold",
    "Flow.fold",
    "Flow.worklist",
];
pub fn arity(op: &str) -> (usize, Option<usize>) {
    match op {
        "List.new" | "Map.new" | "Record.new" | "String.concat" => (0, None),
        "Range.new" => (4, Some(4)),
        "Error.new" => (3, Some(3)),
        "List.map" | "List.filter" | "List.flat_map" => (2, None),
        "List.fold" | "List.unfold" | "Flow.fold" | "Flow.worklist" => (3, None),
        "Math.add" | "Math.sub" | "Math.mul" | "Math.div" | "Math.mod" | "Math.lt" | "Math.lte"
        | "Math.gt" | "Math.gte" | "Math.eq" | "Float.add" | "Float.sub" | "Float.mul"
        | "Float.div" | "Float.eq" | "Float.lt" | "Float.lte" | "Float.gt" | "Float.gte"
        | "String.eq" | "Scalar.eq" | "File.Path.eq" | "Range.contains" | "Record.merge"
        | "Map.has_key" | "Map.get" => (2, Some(2)),
        _ => (1, Some(1)),
    }
}
pub fn admit(s: &Site) -> Result<(), String> {
    if s.operation == crate::host_invocation::OPERATION {
        return crate::host_invocation::admit(s);
    }
    if crate::process::SUPPORTED.contains(&s.operation.as_str()) {
        return crate::process::admit(s);
    }
    if crate::http::SUPPORTED.contains(&s.operation.as_str()) {
        return crate::http::admit(s);
    }
    if crate::tensor::SUPPORTED.contains(&s.operation.as_str()) {
        return crate::tensor::admit(s);
    }
    if crate::training::SUPPORTED.contains(&s.operation.as_str()) {
        return crate::training::admit(s);
    }
    if crate::checkpoint::SUPPORTED.contains(&s.operation.as_str()) {
        return crate::checkpoint::admit(s);
    }
    if crate::files::SUPPORTED.contains(&s.operation.as_str()) {
        return crate::files::admit(s);
    }
    let op = s.operation.as_str();
    if !SUPPORTED.contains(&op) {
        return Err(format!("unsupported native operation {op}"));
    }
    let (min, max) = arity(op);
    if s.arguments.len() < min || max.is_some_and(|n| n != s.arguments.len()) {
        return Err(format!("{op}: metadata argument count mismatch"));
    }
    if matches!(
        op,
        "List.map"
            | "List.filter"
            | "List.flat_map"
            | "List.fold"
            | "List.unfold"
            | "Flow.fold"
            | "Flow.worklist"
    ) {
        return Ok(());
    }
    let mut args = vec![];
    for a in &s.arguments {
        match a {
            Argument::Value(t) => args.push(t),
            Argument::Flow(_) => return Err("unexpected callback argument".into()),
        }
    }
    let fixed = match op {
        "Int.new" | "Int.to_string" | "Int.to_float" => Some(vec![atom("Int")]),
        "String.new" | "String.upper" | "String.lower" | "String.length" | "Bytes.new"
        | "UTF8.Bytes.new" | "File.Path.new" => Some(vec![atom("String")]),
        "Scalar.new" => {
            if args[0].kind() != "Scalar" && args[0].kind() != "String" {
                return Err("invalid Scalar constructor metadata".into());
            }
            None
        }
        "Bool.new" => Some(vec![atom("Bool")]),
        "Float.new" | "Float.sqrt" | "Float.to_int" | "Float.to_string" => {
            Some(vec![atom("Float")])
        }
        "Float.add" | "Float.sub" | "Float.mul" | "Float.div" | "Float.eq" | "Float.lt"
        | "Float.lte" | "Float.gt" | "Float.gte" => Some(vec![atom("Float"), atom("Float")]),
        "Math.add" | "Math.sub" | "Math.mul" | "Math.div" | "Math.mod" | "Math.eq" | "Math.lt"
        | "Math.lte" | "Math.gt" | "Math.gte" => Some(vec![atom("Int"), atom("Int")]),
        "Math.sum" | "Math.product" | "Math.min" | "Math.max" => {
            Some(vec![Type::List(Box::new(atom("Int")))])
        }
        "String.eq" => Some(vec![atom("String"), atom("String")]),
        "Scalar.eq" => Some(vec![atom("Scalar"), atom("Scalar")]),
        "File.Path.eq" => Some(vec![atom("File.Path"), atom("File.Path")]),
        "UTF8.String.new" => Some(vec![atom("Bytes")]),
        "String.FromScalars.new" => Some(vec![Type::List(Box::new(atom("Scalar")))]),
        "Error.new" => Some(vec![atom("String"), atom("String"), atom("Record")]),
        _ => None,
    };
    if let Some(expected) = fixed {
        if expected.iter().zip(&args).any(|(a, b)| a != *b) {
            return Err(format!("{op}: argument type metadata mismatch"));
        }
    }
    match op {
        "String.concat" => {
            if args.iter().any(|t| t.kind() != "String") {
                return Err("invalid concat input".into());
            }
        }
        "List.new" => {
            if let Type::List(t) = &s.typ {
                if args.iter().any(|a| *a != t.as_ref()) {
                    return Err("invalid List input".into());
                }
            } else {
                return Err("invalid List output".into());
            }
        }
        "List.length" => {
            if !matches!(args[0], Type::List(_)) {
                return Err("invalid list length input".into());
            }
        }
        "Record.new" => {
            if s.labels.len() != args.len() {
                return Err("record labels mismatch".into());
            }
            let mut fs = s
                .labels
                .iter()
                .cloned()
                .zip(args.iter().map(|t| (*t).clone()))
                .collect::<Vec<_>>();
            fs.sort_by(|a, b| a.0.cmp(&b.0));
            if fs.windows(2).any(|p| p[0].0 == p[1].0) || s.typ != Type::Record(fs) {
                return Err("record output metadata mismatch".into());
            }
        }
        "Record.merge" => {
            if args.iter().any(|t| !matches!(t, Type::Record(_))) {
                return Err("invalid record merge input".into());
            }
        }
        "Range.new" => {
            if args[0] != args[1]
                || args[2] != &atom("Bool")
                || args[3] != &atom("Bool")
                || s.typ != Type::Range(Box::new(args[0].clone()))
            {
                return Err("invalid range metadata".into());
            }
        }
        "Range.contains" => {
            if let Type::Range(t) = args[0] {
                if t.as_ref() != args[1] {
                    return Err("range element mismatch".into());
                }
            } else {
                return Err("invalid range input".into());
            }
        }
        "Range.is_empty" => {
            if !matches!(args[0], Type::Range(_)) {
                return Err("invalid range input".into());
            }
        }
        "Map.new" => {
            let Type::Map(k, v) = &s.typ else {
                return Err("invalid map output".into());
            };
            let expected = Type::Record(vec![
                ("key".into(), *k.clone()),
                ("value".into(), *v.clone()),
            ]);
            if args.iter().any(|a| **a != expected) {
                return Err("invalid map entry".into());
            }
        }
        "Map.has_key" | "Map.get" => {
            if let Type::Map(k, _) = args[0] {
                if k.as_ref() != args[1] {
                    return Err("map key mismatch".into());
                }
            } else {
                return Err("invalid map input".into());
            }
        }
        _ => (),
    }
    let expected = match op {
        "Int.new" | "Float.to_int" | "Math.add" | "Math.sub" | "Math.mul" | "Math.sum"
        | "Math.product" | "List.length" | "String.length" => Some(atom("Int")),
        "String.new"
        | "String.upper"
        | "String.lower"
        | "String.concat"
        | "String.FromScalars.new"
        | "Int.to_string"
        | "Float.to_string" => Some(atom("String")),
        "Bool.new" | "String.eq" | "Scalar.eq" | "File.Path.eq" | "Math.lt" | "Math.lte"
        | "Math.gt" | "Math.gte" | "Math.eq" | "Float.eq" | "Float.lt" | "Float.lte"
        | "Float.gt" | "Float.gte" | "Range.contains" | "Range.is_empty" | "Map.has_key" => {
            Some(atom("Bool"))
        }
        "Scalar.new" => Some(atom("Scalar")),
        "File.Path.new" => Some(atom("File.Path")),
        "Bytes.new" | "UTF8.Bytes.new" => Some(atom("Bytes")),
        "Float.new" => Some(atom("Float")),
        "Float.add" | "Float.sub" | "Float.mul" | "Float.div" | "Float.sqrt" | "Int.to_float" => {
            Some(Type::Fallible(Box::new(atom("Float"))))
        }
        "Math.div" | "Math.mod" | "Math.min" | "Math.max" => {
            Some(Type::Fallible(Box::new(atom("Int"))))
        }
        "UTF8.String.new" => Some(Type::Fallible(Box::new(atom("String")))),
        "Error.new" => Some(atom("Error")),
        "Map.get" => {
            let Type::Map(_, v) = args[0] else {
                unreachable!()
            };
            Some(if matches!(v.as_ref(), Type::Fallible(_)) {
                *v.clone()
            } else {
                Type::Fallible(v.clone())
            })
        }
        "Record.merge" => {
            let mut fields = std::collections::BTreeMap::new();
            for arg in &args {
                let Type::Record(fs) = arg else {
                    unreachable!()
                };
                fields.extend(fs.iter().cloned());
            }
            Some(Type::Record(fields.into_iter().collect()))
        }
        _ => None,
    };
    if expected.is_some_and(|t| s.typ != t) {
        return Err(format!("{op}: result type metadata mismatch"));
    }
    Ok(())
}
pub fn parameter_names(op: &str) -> Vec<&'static str> {
    match op {
        "Parameters.new" => vec!["values"],
        "Parameters.values" | "Gradients.zero" => vec!["parameters"],
        "Gradients.new" => vec!["parameters", "values"],
        "Gradients.values" => vec!["gradients"],
        "Gradients.add" => vec!["left", "right"],
        "Gradients.scale" => vec!["gradients", "factor"],
        "Optimizer.SGD.Config.new" => vec!["learning_rate"],
        "Optimizer.AdamW.Config.new" => {
            vec!["learning_rate", "beta1", "beta2", "epsilon", "weight_decay"]
        }
        "Optimizer.SGD.State.new" | "Optimizer.AdamW.State.new" => vec!["parameters", "config"],
        "Optimizer.SGD.step" | "Optimizer.AdamW.step" => vec!["state", "gradients"],
        "Checkpoint.decode" => vec!["data", "template"],
        "Parameters.to" => vec!["parameters", "device"],
        "Optimizer.SGD.State.to" | "Optimizer.AdamW.State.to" => vec!["state", "device"],
        "File.read" => vec!["path"],
        "File.Write.new" => vec!["path", "content"],
        "Tensor.new" => return vec!["values", "shape"],
        "Tensor.to" => return vec!["value", "device"],
        "Tensor.add" | "Tensor.sub" | "Tensor.mul" | "Tensor.div" | "Tensor.matmul" => {
            return vec!["left", "right"]
        }
        "Tensor.reshape" | "Tensor.broadcast" => return vec!["value", "shape"],
        "Tensor.transpose" | "Tensor.sum" | "Tensor.mean" => return vec!["value", "axes"],
        "Device.new" => return vec!["selector"],
        "Float.add" | "Float.sub" | "Float.mul" | "Float.div" | "Float.eq" | "Float.lt"
        | "Float.lte" | "Float.gt" | "Float.gte" | "File.Path.eq" => vec!["left", "right"],
        "Math.add" | "Math.sub" | "Math.mul" | "Math.div" | "Math.mod" | "Math.lt" | "Math.lte"
        | "Math.gt" | "Math.gte" | "Math.eq" | "String.eq" | "Scalar.eq" => {
            vec!["left_value", "right_value"]
        }
        "Math.sum" | "Math.product" | "Math.min" | "Math.max" => vec!["number_list"],
        "UTF8.String.new" => vec!["bytes"],
        "Map.get" | "Map.has_key" => vec!["target_map", "target_key"],
        _ => vec!["value"],
    }
}
fn checked_integer(n: BigInt) -> Outcome {
    if n.bits() > MAX_BITS {
        Err(limit("integer bits"))
    } else {
        Ok(int(n))
    }
}
fn bounded_text(c: &Context, s: String) -> Outcome {
    c.values.preflight(s.len() as u64 + 16)?;
    Ok(text(s))
}
fn fail(site: &Site, args: &[Handle], kind: &str, msg: &str) -> Outcome {
    let v = operation_failure(site, args, kind, msg);
    let Data::Failure(e) = &v.data else {
        std::process::abort()
    };
    Err(Fault::Recoverable(e.clone()))
}
fn float_result(s: &Site, a: &[Handle], n: f64) -> Outcome {
    if !n.is_finite() {
        fail(s, a, "Numeric.NonFinite", "numeric result is not finite")
    } else {
        Ok(new(atom("Float"), Data::Float(n)))
    }
}
pub fn execute(c: &mut Context, s: &Site, a: &[Handle]) -> Outcome {
    let op = s.operation.as_str();
    let (min, max) = arity(op);
    if a.len() < min || max.is_some_and(|n| n != a.len()) {
        return Err(host(format!("{op}: invalid argument count")));
    }
    match op {
        "Int.new" | "String.new" | "Bool.new" | "Float.new" => Ok(a[0].clone()),
        "Scalar.new" | "File.Path.new" => {
            if op == "Scalar.new" && a[0].string().chars().count() != 1 {
                return Err(host("Scalar requires one Unicode scalar"));
            }
            Ok(new(s.typ.clone(), Data::Text(a[0].string().into())))
        }
        "Bytes.new" | "UTF8.Bytes.new" => Ok(new(
            atom("Bytes"),
            Data::Bytes(a[0].string().as_bytes().to_vec()),
        )),
        "UTF8.String.new" => {
            let Data::Bytes(b) = &a[0].data else {
                return Err(host("UTF8 input type"));
            };
            match String::from_utf8(b.clone()) {
                Ok(s) => Ok(text(s)),
                Err(_) => fail(s, a, "UTF8.InvalidBytes", "bytes are not valid UTF-8"),
            }
        }
        "String.FromScalars.new" => {
            bounded_text(c, a[0].items().iter().map(|x| x.string()).collect())
        }
        "List.new" => {
            if crate::value_resources::exceeds(0, a.len() as u64, c.values.limits.list_items) {
                return Err(limit("list elements"));
            }
            Ok(new(s.typ.clone(), Data::List(a.to_vec())))
        }
        "Record.new" => {
            if s.labels.len() != a.len() {
                return Err(host("record label count mismatch"));
            }
            Ok(record(s.labels.iter().cloned().zip(a.iter().cloned())))
        }
        "Map.new" => {
            let mut entries: Vec<(Handle, Handle)> = vec![];
            for v in a {
                let fs = v.fields();
                let k = fs["key"].clone();
                let val = fs["value"].clone();
                if let Some(i) = entries.iter().position(|(x, _)| equal(x, &k)) {
                    entries[i] = (k, val)
                } else {
                    entries.push((k, val))
                }
            }
            Ok(new(s.typ.clone(), Data::Map(entries)))
        }
        "Range.new" => Ok(new(
            s.typ.clone(),
            Data::Range(a[0].clone(), a[1].clone(), a[2].bool(), a[3].bool()),
        )),
        "String.concat" => {
            let n: usize = a.iter().map(|x| x.string().len()).sum();
            c.values.preflight(n as u64 + 16)?;
            bounded_text(c, a.iter().map(|x| x.string()).collect())
        }
        "String.upper" => bounded_text(
            c,
            a[0].string().chars().map(crate::unicode::upper).collect(),
        ),
        "String.lower" => bounded_text(
            c,
            a[0].string().chars().map(crate::unicode::lower).collect(),
        ),
        "String.length" => Ok(int(a[0].string().chars().count())),
        "String.eq" | "Scalar.eq" | "File.Path.eq" => Ok(boolean(equal(&a[0], &a[1]))),
        "Int.to_string" => Ok(text(a[0].integer().to_string())),
        "Int.to_float" => float_result(s, a, a[0].integer().to_f64().unwrap_or(f64::INFINITY)),
        "Float.to_int" => checked_integer(
            BigInt::from_f64(a[0].number().trunc()).ok_or_else(|| host("invalid finite Float"))?,
        ),
        "Float.to_string" => Ok(text(format_float(a[0].number()))),
        "Float.add" | "Float.sub" | "Float.mul" | "Float.div" | "Float.sqrt" | "Float.eq"
        | "Float.lt" | "Float.lte" | "Float.gt" | "Float.gte" => {
            let x = a[0].number();
            if op == "Float.sqrt" {
                if x < 0.0 {
                    return fail(
                        s,
                        a,
                        "Numeric.Domain",
                        "square root requires a nonnegative input",
                    );
                }
                return float_result(s, a, x.sqrt());
            }
            let y = a[1].number();
            let result = match op {
                "Float.add" => x + y,
                "Float.sub" => x - y,
                "Float.mul" => x * y,
                "Float.div" => {
                    if y == 0.0 {
                        return fail(s, a, "Numeric.DivideByZero", "division by zero");
                    }
                    x / y
                }
                "Float.eq" => return Ok(boolean(x == y)),
                "Float.lt" => return Ok(boolean(x < y)),
                "Float.lte" => return Ok(boolean(x <= y)),
                "Float.gt" => return Ok(boolean(x > y)),
                "Float.gte" => return Ok(boolean(x >= y)),
                _ => unreachable!(),
            };
            float_result(s, a, result)
        }
        "Math.add" | "Math.sub" | "Math.mul" | "Math.div" | "Math.mod" | "Math.lt" | "Math.lte"
        | "Math.gt" | "Math.gte" | "Math.eq" => {
            let x = a[0].integer();
            let y = a[1].integer();
            let n = match op {
                "Math.add" => x + y,
                "Math.sub" => x - y,
                "Math.mul" => {
                    if !x.is_zero() && !y.is_zero() && x.bits() + y.bits() - 1 > MAX_BITS {
                        return Err(limit("integer bits"));
                    }
                    x * y
                }
                "Math.div" | "Math.mod" => {
                    if y.is_zero() {
                        return if op == "Math.div" {
                            fail(s, a, "Math.DivideByZero", "division by zero")
                        } else {
                            fail(s, a, "Math.ModuloByZero", "remainder by zero")
                        };
                    }
                    if op == "Math.div" {
                        x / y
                    } else {
                        x % y
                    }
                }
                "Math.lt" => return Ok(boolean(x < y)),
                "Math.lte" => return Ok(boolean(x <= y)),
                "Math.gt" => return Ok(boolean(x > y)),
                "Math.gte" => return Ok(boolean(x >= y)),
                "Math.eq" => return Ok(boolean(x == y)),
                _ => unreachable!(),
            };
            checked_integer(n)
        }
        "Math.sum" | "Math.product" | "Math.min" | "Math.max" => {
            let items = a[0].items();
            if items.is_empty() && (op == "Math.min" || op == "Math.max") {
                return fail(s, a, "Math.EmptyList", "empty number list");
            }
            let mut n = if op == "Math.product" {
                BigInt::one()
            } else {
                BigInt::zero()
            };
            for (i, v) in items.iter().enumerate() {
                c.step()?;
                let x = v.integer();
                match op {
                    "Math.sum" => n += x,
                    "Math.product" => {
                        if !n.is_zero() && !x.is_zero() && n.bits() + x.bits() - 1 > MAX_BITS {
                            return Err(limit("integer bits"));
                        }
                        n *= x
                    }
                    "Math.min" => {
                        if i == 0 || x < &n {
                            n = x.clone()
                        }
                    }
                    "Math.max" => {
                        if i == 0 || x > &n {
                            n = x.clone()
                        }
                    }
                    _ => unreachable!(),
                }
                if n.bits() > MAX_BITS {
                    return Err(limit("integer bits"));
                }
            }
            Ok(int(n))
        }
        "List.length" => Ok(int(a[0].items().len())),
        "Range.contains" => Ok(boolean(contains(&a[0], &a[1]))),
        "Range.is_empty" => {
            let Data::Range(x, y, sa, sb) = &a[0].data else {
                return Err(host("range input type"));
            };
            let cmp = compare(x, y);
            let empty = if cmp == Ordering::Greater {
                true
            } else if cmp == Ordering::Equal {
                !*sa || !*sb
            } else if *sa || *sb {
                false
            } else {
                match (&x.data, &y.data) {
                    (Data::Int(x), Data::Int(y)) => y - x == BigInt::one(),
                    (Data::Text(x), Data::Text(y))
                        if x.chars().count() == 1
                            && a[0].typ == Type::Range(Box::new(atom("Scalar"))) =>
                    {
                        let mut n = x.chars().next().unwrap() as u32 + 1;
                        if n == 0xd800 {
                            n = 0xe000
                        }
                        y.chars().next().unwrap() as u32 == n
                    }
                    (Data::Text(x), Data::Text(y)) => y == &(x.clone() + "\0"),
                    _ => return Err(host("invalid range element")),
                }
            };
            Ok(boolean(empty))
        }
        "Record.merge" => {
            Ok(record(a.iter().flat_map(|x| {
                x.fields().iter().map(|(n, v)| (n.clone(), v.clone()))
            })))
        }
        "Map.has_key" | "Map.get" => {
            let Data::Map(entries) = &a[0].data else {
                return Err(host("map input type"));
            };
            let found = entries.iter().find(|(k, _)| equal(k, &a[1]));
            if op == "Map.has_key" {
                return Ok(boolean(found.is_some()));
            }
            match found {
                None => fail(s, a, "Map.MissingKey", "map key is absent"),
                Some((_, v)) => match &v.data {
                    Data::Success(x) => Ok(x.clone()),
                    Data::Failure(e) => Err(Fault::Recoverable(e.clone())),
                    _ => Ok(v.clone()),
                },
            }
        }
        "Error.new" => Ok(error(s, a[0].string(), a[1].string(), a[2].clone())),

        _ => Err(host(format!("unsupported native operation {op}"))),
    }
}
