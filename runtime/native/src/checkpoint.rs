use crate::{context::*, tensor, training, value::*};
use num_bigint::BigInt;

use std::collections::BTreeMap;

// Typed checkpoint view over the one strict JSON parser.
#[derive(Debug)]
enum Json {
    Null,
    Bool,
    Number,
    String(String),
    Array(Vec<Json>),
    Object(Map),
}
#[derive(Debug, Default)]
struct Map(Vec<(String, Json)>);
impl Map {
    fn get(&self, key: &str) -> Option<&Json> {
        self.0.iter().rev().find(|(k, _)| k == key).map(|(_, v)| v)
    }
    fn field(&self, key: &str, skip_null: bool) -> Option<&Json> {
        self.0
            .iter()
            .rev()
            .find(|(k, v)| k == key && (!skip_null || !matches!(v, Json::Null)))
            .map(|(_, v)| v)
    }
    fn len(&self) -> usize {
        self.0.len()
    }
}
impl Json {
    fn as_object(&self) -> Option<&Map> {
        if let Self::Object(m) = self {
            Some(m)
        } else {
            None
        }
    }
    fn as_str(&self) -> Option<&str> {
        if let Self::String(s) = self {
            Some(s)
        } else {
            None
        }
    }
    fn kind(&self) -> &str {
        match self {
            Self::Null => "null",
            Self::Bool => "bool",
            Self::Number => "number",
            Self::String(_) => "string",
            Self::Array(_) => "array",
            Self::Object(_) => "object",
        }
    }
}
impl Drop for Json {
    fn drop(&mut self) {
        let mut pending = Vec::new();
        fn drain(value: &mut Json, pending: &mut Vec<Json>) {
            match value {
                Json::Array(values) => pending.append(values),
                Json::Object(map) => pending.extend(map.0.drain(..).map(|(_, v)| v)),
                _ => (),
            }
        }
        drain(self, &mut pending);
        while let Some(mut value) = pending.pop() {
            drain(&mut value, &mut pending);
        }
    }
}
fn checkpoint_view(c: &mut Context, document: &crate::json::Document) -> Result<Json, Fault> {
    use crate::json::Kind;
    let mut values: Vec<Option<Json>> = Vec::with_capacity(document.nodes.len());
    for node in &document.nodes {
        c.step()?;
        let value = match &node.kind {
            Kind::Null => Json::Null,
            Kind::Bool(_) => Json::Bool,
            Kind::Number(_) => Json::Number,
            Kind::String(s) => {
                crate::json::work(c, s.len() as u64)?;
                Json::String(s.clone())
            }
            Kind::Array(items) => {
                let mut output = Vec::with_capacity(items.len());
                for index in items {
                    c.step()?;
                    output.push(values[*index].take().unwrap());
                }
                Json::Array(output)
            }
            Kind::Object(items) => {
                let mut output = Vec::with_capacity(items.len());
                for (key, index) in items {
                    crate::json::work(c, key.len() as u64 + 1)?;
                    output.push((key.clone(), values[*index].take().unwrap()));
                }
                Json::Object(Map(output))
            }
        };
        values.push(Some(value));
    }
    Ok(values.pop().unwrap().unwrap())
}
fn typed_error(value: &Json, destination: &str) -> Fault {
    invalid(format!(
        "json: cannot unmarshal {} into {destination}",
        value.kind()
    ))
}
pub const SUPPORTED: &[&str] = &[
    "Checkpoint.encode",
    "Checkpoint.decode",
    "Parameters.to",
    "Optimizer.SGD.State.to",
    "Optimizer.AdamW.State.to",
];
fn invalid(message: impl Into<String>) -> Fault {
    Fault::Numeric("Checkpoint.InvalidData", message.into())
}
fn mismatch(message: impl Into<String>) -> Fault {
    Fault::Numeric("Checkpoint.SchemaMismatch", message.into())
}
fn object<'a>(value: &'a Json, subject: &str) -> Result<&'a Map, Fault> {
    value.as_object().ok_or_else(|| {
        let typ=match subject {
            "envelope"=>r#"struct { Type string "json:\"type\""; Value jsontext.Value "json:\"value\"" }"#,
            "tensor"=>r#"struct { Dtype string "json:\"dtype\""; Shape []string "json:\"shape\""; Device string "json:\"device\""; Values []string "json:\"values\"" }"#,
            "record"=>"map[string]jsontext.Value",
            _=>unreachable!()
        };
        typed_error(value,&format!("Go value of type {typ}"))
    })
}
fn typed_fields(object: &Map, fields: &[(&str, bool)]) -> Result<(), Fault> {
    // Preserve source order: the evaluator reports the first typed/unknown field.
    for (name, value) in &object.0 {
        let Some((_, array)) = fields.iter().find(|(key, _)| key == &name.as_str()) else {
            return Err(invalid(format!(
                "json: unknown field {}",
                crate::checkpoint_print::quote(name)
            )));
        };
        if matches!(value, Json::Null) {
            continue;
        }
        if *array {
            let Json::Array(values) = value else {
                return Err(typed_error(
                    value,
                    &format!("Go struct field .{name} of type []string"),
                ));
            };
            for (index, value) in values.iter().enumerate() {
                if !matches!(value, Json::String(_) | Json::Null) {
                    return Err(typed_error(
                        value,
                        &format!(".{name}.{index} of type string"),
                    ));
                }
            }
        } else if !matches!(value, Json::String(_)) {
            return Err(typed_error(
                value,
                &format!("Go struct field .{name} of type string"),
            ));
        }
    }
    Ok(())
}
fn checkpoint_float(text: &str, bits: u32) -> Option<f64> {
    if !crate::text_format::decimal_token(text) {
        return None;
    }
    let number = if bits == 32 {
        text.parse::<f32>().ok()? as f64
    } else {
        text.parse::<f64>().ok()?
    };
    number.is_finite().then_some(number)
}
fn checkpoint_type(typ: &Type) -> bool {
    matches!(
        typ.kind(),
        "Parameters" | "Optimizer.SGD.State" | "Optimizer.AdamW.State"
    )
}
pub fn admit(site: &Site) -> Result<(), String> {
    let args = site
        .arguments
        .iter()
        .map(|a| {
            if let Argument::Value(t) = a {
                Ok(t)
            } else {
                Err("checkpoint operation requires value arguments")
            }
        })
        .collect::<Result<Vec<_>, _>>()?;
    let output = site.typ.base();
    let ok = match (site.operation.as_str(), args.as_slice()) {
        ("Checkpoint.encode", [value]) => checkpoint_type(value) && output == atom("Bytes"),
        ("Checkpoint.decode", [bytes, template]) => {
            **bytes == atom("Bytes") && checkpoint_type(template) && output == **template
        }
        (operation, [value, device]) => {
            **device == atom("Device")
                && checkpoint_type(value)
                && output == **value
                && match operation {
                    "Parameters.to" => value.kind() == "Parameters",
                    "Optimizer.SGD.State.to" => value.kind() == "Optimizer.SGD.State",
                    "Optimizer.AdamW.State.to" => value.kind() == "Optimizer.AdamW.State",
                    _ => false,
                }
        }
        _ => false,
    };
    if !ok || !matches!(site.typ, Type::Fallible(_)) {
        return Err("checkpoint operation metadata type mismatch".into());
    }
    Ok(())
}
fn on_cpu(value: &Handle) -> Result<(), Fault> {
    match &value.data {
        Data::Tensor(data) if data.storage.location != "cpu" => {
            return Err(Fault::Numeric(
                "Tensor.HostObservationRequired",
                "transfer all checkpoint tensors to CPU explicitly".into(),
            ))
        }
        Data::Wrapped(inner) => on_cpu(inner)?,
        Data::Record(fields) => {
            for value in fields.values() {
                on_cpu(value)?;
            }
        }
        _ => (),
    }
    Ok(())
}
pub fn execute(c: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let scope = c.scope_enter();
    let result = execute_inner(c, site, args).map_err(boundary_host_fault);
    let result = match result {
        Ok(value) => training::classify(c, &value)
            .and_then(|()| c.check_cancel())
            .map(|()| value),
        Err(Fault::Numeric(kind, message)) => Err(training::recoverable(
            site,
            args,
            kind,
            &format!("{}: {message}", site.operation),
        )),
        Err(error) => Err(error),
    };
    let roots = match &result {
        Ok(value) => vec![value.clone()],
        Err(_) => vec![],
    };
    c.scope_close(scope, &roots);
    result
}
fn execute_inner(c: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    match site.operation.as_str() {
        "Checkpoint.encode" => {
            on_cpu(&args[0])?;
            c.tensor_budget(args[0].weight, args[0].weight)?;
            let encoded = args[0].canonical_json().into_bytes();
            c.tensor_budget(0, encoded.len() as u64)?;
            Ok(new(atom("Bytes"), Data::Bytes(encoded)))
        }
        "Checkpoint.decode" => {
            on_cpu(&args[1])?;
            let Data::Bytes(data) = &args[0].data else {
                return Err(host("checkpoint input is not Bytes"));
            };
            c.tensor_budget(data.len() as u64, data.len() as u64)?;
            c.step()?;
            let document = crate::json::parse(c, data).map_err(|fault| match fault {
                Fault::Numeric(_, message) => invalid(message),
                other => other,
            })?;
            let reserve = document
                .weight()
                .checked_mul(4)
                .and_then(|n| n.checked_add((document.nodes.len() as u64).checked_mul(128)?))
                .ok_or_else(|| limit("checkpoint parser bytes"))?;
            let _scratch = crate::processors::Scratch::new(c, reserve)?;
            let json = checkpoint_view(c, &document)?;
            drop(document);
            let output = decode_document(c, &json, &args[1])?;
            if output.canonical_json().as_bytes() != data {
                return Err(invalid("checkpoint must use the canonical encoding"));
            }
            Ok(output)
        }
        _ => {
            let Data::Device(destination) = &args[1].data else {
                return Err(host("checkpoint transfer requires Device"));
            };
            transfer(c, &args[0], destination, &site.operation)
        }
    }
}
fn decode(c: &mut Context, encoded: &Json, template: &Handle) -> Outcome {
    c.step()?;
    decode_document(c, encoded, template)
}
fn decode_document(c: &mut Context, encoded: &Json, template: &Handle) -> Outcome {
    let empty = Map::default();
    let envelope = if matches!(encoded, Json::Null) {
        &empty
    } else {
        object(encoded, "envelope")?
    };
    for (name, value) in &envelope.0 {
        match name.as_str() {
            "type" if !matches!(value, Json::String(_) | Json::Null) => {
                return Err(typed_error(
                    value,
                    &format!("Go struct field .{name} of type string"),
                ))
            }
            "type" | "value" => (),
            _ => {
                return Err(invalid(format!(
                    "json: unknown field {}",
                    crate::checkpoint_print::quote(name)
                )))
            }
        }
    }
    let name = match envelope.field("type", true) {
        None | Some(Json::Null) => "",
        Some(Json::String(value)) => value,
        Some(value) => return Err(typed_error(value, "Go struct field .type of type string")),
    };
    if name != template.typ.name() {
        return Err(mismatch(format!(
            "expected {}, got {name}",
            template.typ.name()
        )));
    }
    let data = envelope
        .field("value", false)
        .ok_or_else(|| invalid("EOF"))?;
    match &template.data {
        Data::Tensor(_) => decode_tensor(c, data, template),
        Data::Record(_) => decode_fields(c, data, template),
        Data::Wrapped(inner) => {
            let fields = decode_fields(c, data, inner)?;
            let result = if matches!(
                template.typ.kind(),
                "Optimizer.SGD.Config" | "Optimizer.AdamW.Config"
            ) {
                let names = if template.typ.kind() == "Optimizer.SGD.Config" {
                    vec!["learning_rate"]
                } else {
                    vec!["learning_rate", "beta1", "beta2", "epsilon", "weight_decay"]
                };
                training::config(
                    template.typ.clone(),
                    &names
                        .iter()
                        .map(|name| fields.fields()[*name].number())
                        .collect::<Vec<_>>(),
                )
                .map_err(invalid)?
            } else {
                new(template.typ.clone(), Data::Wrapped(fields))
            };
            training::validate(&result).map_err(invalid)?;
            if result.typ.kind() == "Optimizer.AdamW.State" {
                for value in result.fields()["second_moment"].fields().values() {
                    for (index, number) in
                        tensor::data(value).storage.host_values().iter().enumerate()
                    {
                        if index % 1024 == 0 {
                            c.check_cancel()?;
                        }
                        if *number < 0.0 {
                            return Err(invalid("AdamW second moments must be nonnegative"));
                        }
                    }
                }
                for name in ["beta1_power", "beta2_power"] {
                    let number = tensor::data(&result.fields()[name]).storage.host_values()[0];
                    if !(0.0..=1.0).contains(&number) {
                        return Err(invalid("AdamW beta powers must be in [0,1]"));
                    }
                }
            }
            Ok(result)
        }
        Data::Float(_) => {
            let text = data
                .as_str()
                .or_else(|| {
                    if matches!(data, Json::Null) {
                        Some("")
                    } else {
                        None
                    }
                })
                .ok_or_else(|| typed_error(data, "Go value of type string"))?;
            let number = checkpoint_float(text, 64)
                .ok_or_else(|| invalid("checkpoint Float must be finite"))?;
            Ok(new(atom("Float"), Data::Float(number)))
        }
        Data::Int(_) => {
            let text = data
                .as_str()
                .or_else(|| {
                    if matches!(data, Json::Null) {
                        Some("")
                    } else {
                        None
                    }
                })
                .ok_or_else(|| typed_error(data, "Go value of type string"))?;
            if text.len() > MAX_BITS as usize / 3 + 1 {
                return Err(limit("checkpoint integer bits"));
            }
            let digits = text.strip_prefix(['+', '-']).unwrap_or(text);
            if digits.is_empty() || !digits.bytes().all(|b| b.is_ascii_digit()) {
                return Err(invalid("invalid checkpoint integer"));
            }
            let number = text
                .parse::<BigInt>()
                .map_err(|_| invalid("invalid checkpoint integer"))?;
            if number.bits() > MAX_BITS {
                return Err(limit("integer bits"));
            }
            Ok(int(number))
        }
        _ => Err(host(format!(
            "unsupported checked checkpoint component {}",
            template.typ.name()
        ))),
    }
}
fn decode_fields(c: &mut Context, data: &Json, template: &Handle) -> Outcome {
    let empty = Map::default();
    let object = if matches!(data, Json::Null) {
        &empty
    } else {
        object(data, "record")?
    };
    let expected = template.fields();
    if object.len() != expected.len() {
        return Err(mismatch("checkpoint field set differs from template"));
    }
    let mut fields = BTreeMap::new();
    for (name, value) in expected {
        let encoded = object
            .get(name)
            .ok_or_else(|| mismatch(format!("checkpoint is missing field {name}")))?;
        let output = decode(c, encoded, value).map_err(|fault| match fault {
            Fault::Numeric(kind, message) => {
                Fault::Numeric(kind, format!("field {name}: {message}"))
            }
            Fault::Host(code, message) => Fault::Host(code, format!("field {name}: {message}")),
            Fault::Budget { primary, message } => Fault::Budget {
                primary,
                message: format!("field {name}: {message}"),
            },
            Fault::RawCancellation => Fault::Host(
                "runtime.cancelled",
                format!("field {name}: context canceled"),
            ),
            other => other,
        })?;
        fields.insert(name.clone(), output);
    }
    Ok(record(fields))
}
fn decode_tensor(c: &mut Context, data: &Json, template: &Handle) -> Outcome {
    let empty = Map::default();
    let object = if matches!(data, Json::Null) {
        &empty
    } else {
        object(data, "tensor")?
    };
    typed_fields(
        object,
        &[
            ("dtype", false),
            ("shape", true),
            ("device", false),
            ("values", true),
        ],
    )?;
    let tensor = tensor::data(template);
    let shape = &tensor.shape;
    let string_field = |name: &str| -> Result<&str, Fault> {
        match object.field(name, true) {
            None | Some(Json::Null) => Ok(""),
            Some(Json::String(value)) => Ok(value),
            Some(value) => Err(typed_error(
                value,
                &format!("Go struct field .{name} of type string"),
            )),
        }
    };
    let string_array = |name: &str| -> Result<Vec<&str>, Fault> {
        match object.field(name, false) {
            None | Some(Json::Null) => Ok(vec![]),
            Some(Json::Array(values)) => values
                .iter()
                .enumerate()
                .map(|(index, value)| match value {
                    Json::String(text) => Ok(text.as_str()),
                    Json::Null => Ok(""),
                    value => Err(typed_error(
                        value,
                        &format!(".{name}.{index} of type string"),
                    )),
                })
                .collect(),
            Some(value) => Err(typed_error(
                value,
                &format!("Go struct field .{name} of type []string"),
            )),
        }
    };
    let dtype = string_field("dtype")?;
    let dimensions = string_array("shape")?;
    let device = string_field("device")?;
    let values = string_array("values")?;
    if dtype != "fp32" || device != "cpu" || dimensions.len() != shape.dims.len() {
        return Err(mismatch(
            "checkpoint tensor requires CPU FP32 and the template shape",
        ));
    }
    for (encoded, dimension) in dimensions.iter().zip(&shape.dims) {
        if *encoded != dimension.to_string() {
            return Err(mismatch("checkpoint tensor shape differs from template"));
        }
    }
    if values.len() as u64 != shape.count {
        return Err(mismatch(
            "checkpoint tensor element count differs from shape",
        ));
    }
    c.tensor_budget(shape.count, shape.weight())?;
    let mut numbers = Vec::with_capacity(values.len());
    for (index, value) in values.iter().enumerate() {
        if index % 1024 == 0 {
            c.check_cancel()?;
        }
        let number = checkpoint_float(value, 32)
            .ok_or_else(|| invalid("checkpoint tensor elements must be finite FP32"))?;
        numbers.push(number as f32);
    }
    Ok(tensor::value(tensor::construct(c, shape, numbers)?))
}
fn transfer(c: &mut Context, value: &Handle, destination: &str, operation: &str) -> Outcome {
    c.step()?;
    match &value.data {
        Data::Tensor(data) => {
            for location in [data.storage.location.as_str(), destination] {
                if location != "cpu" && location != c.device_grant {
                    return Err(Fault::Host(
                        "runtime.capability-denied",
                        format!("{operation} {location:?} has no host capability"),
                    ));
                }
            }
            if data.storage.location != "cpu"
                && destination != "cpu"
                && data.storage.location != destination
            {
                return Err(Fault::Numeric(
                    "Tensor.DeviceMismatch",
                    "transfers require CPU and one compute location".into(),
                ));
            }
            c.tensor_budget(
                if data.storage.location == destination {
                    0
                } else {
                    data.shape.count
                },
                data.shape.weight() - 3 + destination.len() as u64,
            )?;
            Ok(tensor::value(tensor::transfer(c, data, destination)?))
        }
        Data::Record(fields) => {
            let mut output = BTreeMap::new();
            for (name, value) in fields {
                output.insert(name.clone(), transfer(c, value, destination, operation)?);
            }
            Ok(record(output))
        }
        Data::Wrapped(_inner)
            if matches!(
                value.typ.kind(),
                "Optimizer.SGD.Config" | "Optimizer.AdamW.Config"
            ) =>
        {
            Ok(value.clone())
        }
        Data::Wrapped(inner) => {
            let moved = transfer(c, inner, destination, operation)?;
            let result = new(value.typ.clone(), Data::Wrapped(moved));
            training::validate(&result).map_err(invalid)?;
            Ok(result)
        }
        Data::Int(_) => Ok(value.clone()),
        _ => Err(host(format!(
            "unsupported checked transfer component {}",
            value.typ.name()
        ))),
    }
}
