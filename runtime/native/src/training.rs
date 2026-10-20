use crate::{
    context::*,
    resources::Class,
    tensor::{self, Shape},
    value::*,
};
use num_bigint::BigInt;
use num_traits::One;
use std::collections::BTreeMap;

pub const SUPPORTED: &[&str] = &[
    "Parameters.new",
    "Parameters.values",
    "Gradients.new",
    "Gradients.values",
    "Gradients.zero",
    "Gradients.add",
    "Gradients.scale",
    "Optimizer.SGD.Config.new",
    "Optimizer.AdamW.Config.new",
    "Optimizer.SGD.State.new",
    "Optimizer.AdamW.State.new",
    "Optimizer.SGD.step",
    "Optimizer.AdamW.step",
];

pub fn contents(value: &Handle) -> Handle {
    if let Data::Wrapped(inner) = &value.data {
        inner.clone()
    } else {
        std::process::abort()
    }
}
fn wrapped(typ: Type, fields: BTreeMap<String, Handle>) -> Handle {
    new(typ, Data::Wrapped(record(fields)))
}
fn field(value: &Handle, name: &str) -> Handle {
    value.fields()[name].clone()
}
fn schema(typ: &Type, name: &str) -> Option<Type> {
    if let Type::Nominal(kind, schema) = typ {
        if kind == name {
            return Some(*schema.clone());
        }
    }
    None
}
fn arguments(site: &Site) -> Result<Vec<Type>, String> {
    site.arguments
        .iter()
        .map(|argument| {
            if let Argument::Value(typ) = argument {
                Ok(typ.clone())
            } else {
                Err("training operation requires value arguments".into())
            }
        })
        .collect()
}
fn tensor_record(typ: &Type) -> bool {
    matches!(typ, Type::Record(fields) if !fields.is_empty() && fields.iter().all(|(_,typ)| *typ == atom("Tensor<FP32>")))
}
pub fn admit(site: &Site) -> Result<(), String> {
    let args = arguments(site)?;
    let base = site.typ.base();
    let ok = match (site.operation.as_str(), args.as_slice()) {
        ("Parameters.new", [values]) => {
            tensor_record(values) && schema(&base, "Parameters").as_ref() == Some(values)
        }
        ("Parameters.values", [value]) => schema(value, "Parameters").as_ref() == Some(&site.typ),
        ("Gradients.values", [value]) => schema(value, "Gradients").as_ref() == Some(&site.typ),
        ("Gradients.new", [parameters, values]) => {
            schema(parameters, "Parameters").as_ref() == Some(values)
                && schema(&base, "Gradients").as_ref() == Some(values)
        }
        ("Gradients.zero", [parameters]) => {
            schema(parameters, "Parameters").is_some()
                && schema(parameters, "Parameters") == schema(&base, "Gradients")
        }
        ("Gradients.add", [left, right]) => {
            left == right && schema(left, "Gradients").is_some() && *left == base
        }
        ("Gradients.scale", [value, factor]) => {
            *factor == atom("Float") && schema(value, "Gradients").is_some() && *value == base
        }
        ("Optimizer.SGD.Config.new", [rate]) => {
            *rate == atom("Float") && base == atom("Optimizer.SGD.Config")
        }
        ("Optimizer.AdamW.Config.new", values) => {
            values.len() == 5
                && values.iter().all(|typ| *typ == atom("Float"))
                && base == atom("Optimizer.AdamW.Config")
        }
        ("Optimizer.SGD.State.new", [parameters, config]) => {
            *config == atom("Optimizer.SGD.Config")
                && schema(parameters, "Parameters").is_some()
                && schema(parameters, "Parameters") == schema(&base, "Optimizer.SGD.State")
        }
        ("Optimizer.AdamW.State.new", [parameters, config]) => {
            *config == atom("Optimizer.AdamW.Config")
                && schema(parameters, "Parameters").is_some()
                && schema(parameters, "Parameters") == schema(&base, "Optimizer.AdamW.State")
        }
        ("Optimizer.SGD.step", [state, gradients]) => {
            *state == base
                && schema(state, "Optimizer.SGD.State").is_some()
                && schema(state, "Optimizer.SGD.State") == schema(gradients, "Gradients")
        }
        ("Optimizer.AdamW.step", [state, gradients]) => {
            *state == base
                && schema(state, "Optimizer.AdamW.State").is_some()
                && schema(state, "Optimizer.AdamW.State") == schema(gradients, "Gradients")
        }
        _ => false,
    };
    if !ok {
        return Err("training operation metadata type mismatch".into());
    }
    let plain = matches!(
        site.operation.as_str(),
        "Parameters.values" | "Gradients.values"
    );
    if plain == matches!(site.typ, Type::Fallible(_)) {
        return Err("training fallibility metadata mismatch".into());
    }
    Ok(())
}

pub fn config(typ: Type, values: &[f64]) -> Result<Handle, String> {
    let names = if typ.kind() == "Optimizer.SGD.Config" {
        vec!["learning_rate"]
    } else {
        vec!["learning_rate", "beta1", "beta2", "epsilon", "weight_decay"]
    };
    if names.len() != values.len() {
        return Err("optimizer configuration field set differs".into());
    }
    let mut fields = BTreeMap::new();
    let mut numbers = vec![];
    for (name, value) in names.iter().zip(values) {
        let value = *value as f32;
        if !value.is_finite() {
            return Err(format!("optimizer {name} must convert to finite FP32"));
        }
        numbers.push(value);
        fields.insert(
            (*name).into(),
            new(atom("Float"), Data::Float(value as f64)),
        );
    }
    if names.len() == 1 {
        if numbers[0] <= 0.0 {
            return Err("optimizer learning_rate must be positive after FP32 conversion".into());
        }
    } else {
        if numbers[0] <= 0.0
            || numbers[3] <= 0.0
            || numbers[1] < 0.0
            || numbers[1] >= 1.0
            || numbers[2] < 0.0
            || numbers[2] >= 1.0
            || numbers[4] < 0.0
        {
            return Err("AdamW requires positive learning_rate and epsilon, betas in [0,1), and nonnegative weight_decay after FP32 conversion".into());
        }
        if !(numbers[0] * numbers[4]).is_finite() {
            return Err("AdamW learning_rate times weight_decay must be finite FP32".into());
        }
    }
    Ok(wrapped(typ, fields))
}
pub fn match_schema(expected: &Handle, actual: &Handle) -> Result<(), String> {
    let expected = if matches!(expected.data, Data::Wrapped(_)) {
        contents(expected)
    } else {
        expected.clone()
    };
    let actual = if matches!(actual.data, Data::Wrapped(_)) {
        contents(actual)
    } else {
        actual.clone()
    };
    if expected.typ != actual.typ {
        return Err(format!(
            "gradient fields require {}, got {}",
            expected.typ.name(),
            actual.typ.name()
        ));
    }
    for (name, left) in expected.fields() {
        let right = &actual.fields()[name];
        let a = tensor::data(left);
        let b = tensor::data(right);
        if a.storage.location != b.storage.location {
            return Err(format!("gradient field {name} has a different device"));
        }
        if a.shape.dims != b.shape.dims {
            return Err(format!(
                "gradient field {name} requires shape {}, got {}",
                shape_text(&a.shape.dims),
                shape_text(&b.shape.dims)
            ));
        }
    }
    Ok(())
}
fn shape_text(shape: &[i64]) -> String {
    format!(
        "[{}]",
        shape
            .iter()
            .map(ToString::to_string)
            .collect::<Vec<_>>()
            .join(" ")
    )
}

pub fn validate(value: &Handle) -> Result<(), String> {
    match value.typ.kind() {
        "Parameters" | "Gradients" => {
            let mut location = None;
            for tensor in value.fields().values() {
                let current = &tensor::data(tensor).storage.location;
                if location.is_some_and(|previous| previous != current) {
                    return Err("tensor record fields require one device location".into());
                }
                location = Some(current);
            }
        }
        "Optimizer.SGD.Config" | "Optimizer.AdamW.Config" => {
            let names = if value.typ.kind() == "Optimizer.SGD.Config" {
                vec!["learning_rate"]
            } else {
                vec!["learning_rate", "beta1", "beta2", "epsilon", "weight_decay"]
            };
            config(
                value.typ.clone(),
                &names
                    .iter()
                    .map(|name| field(value, name).number())
                    .collect::<Vec<_>>(),
            )?;
        }
        "Optimizer.SGD.State" | "Optimizer.AdamW.State" => {
            let parameters = field(value, "parameters");
            let step = field(value, "step");
            let cfg = field(value, "config");
            if parameters.typ.kind() != "Parameters" || step.integer() < &BigInt::from(0) {
                return Err(
                    "optimizer state requires Parameters, nonnegative step, and validated config"
                        .into(),
                );
            }
            validate(&parameters)?;
            validate(&cfg)?;
            if value.typ.kind() == "Optimizer.AdamW.State" {
                for name in ["first_moment", "second_moment"] {
                    match_schema(&parameters, &field(value, name))
                        .map_err(|message| format!("AdamW moment: {message}"))?;
                }
                let location = &tensor::data(parameters.fields().values().next().unwrap())
                    .storage
                    .location;
                for name in ["beta1_power", "beta2_power"] {
                    let power = field(value, name);
                    let data = tensor::data(&power);
                    if &data.storage.location != location || !data.shape.dims.is_empty() {
                        return Err(
                            "AdamW beta power must be rank zero at the Parameter location".into(),
                        );
                    }
                }
            }
        }
        _ => return Err("unsupported training value".into()),
    }
    Ok(())
}

fn scalar(c: &mut Context, location: &str, number: f32) -> Outcome {
    Ok(tensor::value(
        tensor::constant(
            c,
            &Shape {
                dims: vec![],
                count: 1,
            },
            location,
            number,
        )
        .map_err(boundary_host_fault)?,
    ))
}
fn apply(c: &mut Context, operation: &str, left: &Handle, right: Option<&Handle>) -> Outcome {
    Ok(tensor::value(
        tensor::apply(
            c,
            operation,
            tensor::data(left),
            right.map(tensor::data),
            &[],
        )
        .map_err(boundary_host_fault)?,
    ))
}
fn constant(c: &mut Context, like: &Handle, number: f32) -> Outcome {
    let data = tensor::data(like);
    Ok(tensor::value(
        tensor::constant(c, &data.shape, &data.storage.location, number)
            .map_err(boundary_host_fault)?,
    ))
}
fn state(
    typ: Type,
    parameters: Handle,
    step: Handle,
    config: Handle,
    extra: impl IntoIterator<Item = (String, Handle)>,
) -> Handle {
    let mut fields = BTreeMap::from([
        ("parameters".into(), parameters),
        ("step".into(), step),
        ("config".into(), config),
    ]);
    fields.extend(extra);
    wrapped(typ, fields)
}
fn adam_roots(value: &Handle) -> Vec<u64> {
    if value.typ.kind() != "Optimizer.AdamW.State" {
        return vec![];
    }
    tensor::allocations(&[
        field(value, "first_moment"),
        field(value, "second_moment"),
        field(value, "beta1_power"),
        field(value, "beta2_power"),
    ])
}
pub fn classify(c: &mut Context, value: &Handle) -> Result<(), Fault> {
    c.resources
        .classify(&adam_roots(value), Class::Optimizer)
        .map_err(boundary_host_fault)
}

pub fn recoverable(site: &Site, args: &[Handle], kind: &str, message: &str) -> Fault {
    let failed = operation_failure(site, args, kind, message);
    if let Data::Failure(error) = &failed.data {
        Fault::Recoverable(error.clone())
    } else {
        std::process::abort()
    }
}

pub fn execute(c: &mut Context, s: &Site, a: &[Handle]) -> Outcome {
    let scope = c.resources.scope_enter();
    let result = match execute_inner(c, s, a) {
        Err(Fault::Numeric(kind, message)) => Err(recoverable(s, a, kind, &message)),
        other => other,
    };
    let result = match result {
        Ok(value) => match classify(c, &value) {
            Ok(()) => Ok(value),
            Err(error) => Err(error),
        },
        Err(error) => Err(error),
    };
    let roots = match &result {
        Ok(value) => tensor::allocations(&[value.clone()]),
        Err(_) => vec![],
    };
    c.resources.scope_close(scope, &roots);
    result
}
fn execute_inner(c: &mut Context, s: &Site, a: &[Handle]) -> Outcome {
    let op = s.operation.as_str();
    let typ = s.typ.base();
    let fail = |kind: &str, message: &str| recoverable(s, a, kind, message);
    if op.ends_with("Config.new") {
        return config(
            typ,
            &a.iter().map(|value| value.number()).collect::<Vec<_>>(),
        )
        .map_err(|message| fail("Optimizer.InvalidConfig", &message));
    }
    if op == "Parameters.new" {
        let value = wrapped(typ, a[0].fields().clone());
        return validate(&value)
            .map(|()| value)
            .map_err(|message| fail("Gradient.SchemaMismatch", &message));
    }
    if matches!(op, "Parameters.values" | "Gradients.values") {
        return Ok(contents(&a[0]));
    }
    if op.starts_with("Gradients.") {
        let left = contents(&a[0]);
        if matches!(op, "Gradients.new" | "Gradients.add") {
            let right = if op == "Gradients.new" {
                a[1].clone()
            } else {
                contents(&a[1])
            };
            if let Err(message) = match_schema(&left, &right) {
                return Err(fail("Gradient.SchemaMismatch", &message));
            }
            if op == "Gradients.new" {
                return Ok(wrapped(typ, right.fields().clone()));
            }
        }
        let location = tensor::data(left.fields().values().next().unwrap())
            .storage
            .location
            .clone();
        let factor = if op == "Gradients.scale" {
            let number = a[1].number() as f32;
            if !number.is_finite() {
                return Err(fail(
                    "Numeric.NonFinite",
                    "gradient scale factor exceeds finite FP32",
                ));
            }
            Some(scalar(c, &location, number)?)
        } else {
            None
        };
        let mut fields = BTreeMap::new();
        for (name, value) in left.fields() {
            let next = match op {
                "Gradients.zero" => constant(c, value, 0.0)?,
                "Gradients.add" => apply(c, "Tensor.add", value, Some(&a[1].fields()[name]))?,
                "Gradients.scale" => apply(c, "Tensor.mul", value, factor.as_ref())?,
                _ => return Err(host("unknown gradient operation")),
            };
            fields.insert(name.clone(), next);
        }
        return Ok(wrapped(typ, fields));
    }
    if op.ends_with("State.new") {
        if op == "Optimizer.SGD.State.new" {
            return Ok(state(typ, a[0].clone(), int(0), a[1].clone(), []));
        }
        let parameters = &a[0];
        let location = tensor::data(parameters.fields().values().next().unwrap())
            .storage
            .location
            .clone();
        let bytes = parameters.fields().values().fold(4u64, |sum, value| {
            sum.saturating_add(8 * tensor::data(value).shape.count)
        });
        c.resources
            .check_budget(Class::Optimizer, bytes)
            .map_err(boundary_host_fault)?;
        let mut first = BTreeMap::new();
        let mut second = BTreeMap::new();
        for (name, value) in parameters.fields() {
            first.insert(name.clone(), constant(c, value, 0.0)?);
            second.insert(name.clone(), constant(c, value, 0.0)?);
        }
        let power = scalar(c, &location, 1.0)?;
        return Ok(state(
            typ,
            parameters.clone(),
            int(0),
            a[1].clone(),
            [
                ("first_moment".into(), record(first)),
                ("second_moment".into(), record(second)),
                ("beta1_power".into(), power.clone()),
                ("beta2_power".into(), power),
            ],
        ));
    }
    let previous = &a[0];
    let parameters = field(previous, "parameters");
    let gradients = contents(&a[1]);
    if let Err(message) = match_schema(&parameters, &gradients) {
        return Err(fail("Gradient.SchemaMismatch", &message));
    }
    let location = tensor::data(parameters.fields().values().next().unwrap())
        .storage
        .location
        .clone();
    let cfg = field(previous, "config");
    let rate = scalar(c, &location, field(&cfg, "learning_rate").number() as f32)?;
    let mut fields = parameters.fields().clone();
    if op == "Optimizer.SGD.step" {
        for (name, parameter) in &mut fields {
            let update = apply(c, "Tensor.mul", &rate, Some(&gradients.fields()[name]))?;
            *parameter = apply(c, "Tensor.sub", parameter, Some(&update))?;
        }
        let step = int(field(previous, "step").integer() + BigInt::one());
        if step.integer().bits() > MAX_BITS {
            return Err(limit("integer bits"));
        }
        let next = wrapped(parameters.typ.clone(), fields);
        return Ok(state(typ, next, step, cfg, []));
    }
    let bytes = fields.values().fold(8u64, |sum, value| {
        sum.saturating_add(8 * tensor::data(value).shape.count)
    });
    c.resources
        .check_budget(Class::Optimizer, bytes)
        .map_err(boundary_host_fault)?;
    let mut first = field(previous, "first_moment").fields().clone();
    let mut second = field(previous, "second_moment").fields().clone();
    let beta1 = scalar(c, &location, field(&cfg, "beta1").number() as f32)?;
    let beta2 = scalar(c, &location, field(&cfg, "beta2").number() as f32)?;
    let epsilon = scalar(c, &location, field(&cfg, "epsilon").number() as f32)?;
    let decay = scalar(c, &location, field(&cfg, "weight_decay").number() as f32)?;
    let one = scalar(c, &location, 1.0)?;
    let mut powers = None;
    for (name, parameter) in &mut fields {
        let gradient = &gradients.fields()[name];
        let m_beta = apply(c, "Tensor.mul", &beta1, Some(&first[name]))?;
        let m_factor = apply(c, "Tensor.sub", &one, Some(&beta1))?;
        let m_gradient = apply(c, "Tensor.mul", &m_factor, Some(gradient))?;
        let m_next = apply(c, "Tensor.add", &m_beta, Some(&m_gradient))?;
        let v_beta = apply(c, "Tensor.mul", &beta2, Some(&second[name]))?;
        let v_factor = apply(c, "Tensor.sub", &one, Some(&beta2))?;
        let squared = apply(c, "Tensor.mul", gradient, Some(gradient))?;
        let v_gradient = apply(c, "Tensor.mul", &v_factor, Some(&squared))?;
        let v_next = apply(c, "Tensor.add", &v_beta, Some(&v_gradient))?;
        if powers.is_none() {
            powers = Some((
                apply(
                    c,
                    "Tensor.mul",
                    &field(previous, "beta1_power"),
                    Some(&beta1),
                )?,
                apply(
                    c,
                    "Tensor.mul",
                    &field(previous, "beta2_power"),
                    Some(&beta2),
                )?,
            ));
        }
        let (power1, power2) = powers.as_ref().unwrap();
        let m_denom = apply(c, "Tensor.sub", &one, Some(power1))?;
        let m_hat = apply(c, "Tensor.div", &m_next, Some(&m_denom))?;
        let v_denom = apply(c, "Tensor.sub", &one, Some(power2))?;
        let v_hat = apply(c, "Tensor.div", &v_next, Some(&v_denom))?;
        let decay_factor = apply(c, "Tensor.mul", &rate, Some(&decay))?;
        let decay_update = apply(c, "Tensor.mul", &decay_factor, Some(parameter))?;
        let decayed = apply(c, "Tensor.sub", parameter, Some(&decay_update))?;
        let root = apply(c, "Tensor.sqrt", &v_hat, None)?;
        let denom = apply(c, "Tensor.add", &root, Some(&epsilon))?;
        let quotient = apply(c, "Tensor.div", &m_hat, Some(&denom))?;
        let update = apply(c, "Tensor.mul", &rate, Some(&quotient))?;
        *parameter = apply(c, "Tensor.sub", &decayed, Some(&update))?;
        first.insert(name.clone(), m_next);
        second.insert(name.clone(), v_next);
    }
    let step = int(field(previous, "step").integer() + BigInt::one());
    if step.integer().bits() > MAX_BITS {
        return Err(limit("integer bits"));
    }
    let (power1, power2) = powers.unwrap();
    let next = wrapped(parameters.typ.clone(), fields);
    Ok(state(
        typ,
        next,
        step,
        cfg,
        [
            ("first_moment".into(), record(first)),
            ("second_moment".into(), record(second)),
            ("beta1_power".into(), power1),
            ("beta2_power".into(), power2),
        ],
    ))
}
