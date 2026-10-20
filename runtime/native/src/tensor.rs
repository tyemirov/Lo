use crate::{context::*, value::*};
use num_traits::ToPrimitive;
use std::sync::Arc;
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Shape {
    pub dims: Vec<i64>,
    pub count: u64,
}
impl Shape {
    pub fn new(dims: Vec<i64>) -> Result<Self, Fault> {
        let text = fmt_dims(&dims);
        if dims.iter().any(|d| *d < 0) {
            return Err(invalid("nonnegative signed 64-bit dimensions", text));
        }
        let count = if dims.contains(&0) {
            0
        } else {
            let mut n = 1u64;
            for d in &dims {
                n = n
                    .checked_mul(*d as u64)
                    .filter(|n| *n <= i64::MAX as u64)
                    .ok_or_else(|| {
                        invalid("element count within signed 64-bit range", text.clone())
                    })?;
            }
            n
        };
        Ok(Self { dims, count })
    }
    pub fn weight(&self) -> u64 {
        16 + 4 * self.count + 8 * self.dims.len() as u64 + 7
    }
    pub fn limits(&self) -> Result<(), Fault> {
        if self.dims.len() > 8 {
            return Err(limit("tensor rank exceeds 8"));
        }
        if self.dims.iter().any(|d| *d > 1_000_000) {
            return Err(limit("tensor dimension exceeds 1000000"));
        }
        if self.count > 1_000_000 {
            return Err(limit("tensor elements exceed 1000000"));
        }
        Ok(())
    }
}
pub struct Storage {
    pub session: u64,
    pub id: u64,
    pub location: String,
    pub values: Vec<f32>,
}
impl Drop for Storage {
    fn drop(&mut self) {
        use std::sync::atomic::Ordering;
        crate::resources::LIVE_STORAGES.fetch_sub(1, Ordering::Relaxed);
        crate::resources::LIVE_STORAGE_BYTES
            .fetch_sub(4 * self.values.len() as u64, Ordering::Relaxed);
    }
}
#[derive(Clone)]
pub struct Tensor {
    pub shape: Shape,
    pub storage: Arc<Storage>,
}
impl Tensor {
    pub fn weight(&self) -> u64 {
        self.shape.weight() - 3 + self.storage.location.len() as u64
    }
}
pub fn data(v: &Handle) -> &Tensor {
    if let Data::Tensor(t) = &v.data {
        t
    } else {
        std::process::abort()
    }
}
pub fn value(t: Tensor) -> Handle {
    new(atom("Tensor<FP32>"), Data::Tensor(t))
}
pub fn fmt_dims(d: &[i64]) -> String {
    format!(
        "[{}]",
        d.iter()
            .map(|d| d.to_string())
            .collect::<Vec<_>>()
            .join(" ")
    )
}
fn invalid(expected: impl Into<String>, actual: impl Into<String>) -> Fault {
    Fault::Numeric(
        "Tensor.InvalidShape",
        format!("expected {}, got {}", expected.into(), actual.into()),
    )
}
fn mismatch(expected: impl Into<String>, actual: impl Into<String>) -> Fault {
    Fault::Numeric(
        "Tensor.ShapeMismatch",
        format!("expected {}, got {}", expected.into(), actual.into()),
    )
}
fn numeric(kind: &'static str, index: usize, expected: impl Into<String>) -> Fault {
    Fault::Numeric(
        kind,
        format!(
            "expected {}, got invalid value at row-major element {index}",
            expected.into()
        ),
    )
}
fn visit_storages(
    values: &[Handle],
    mut visit: impl FnMut(&Storage) -> Result<(), Fault>,
) -> Result<(), Fault> {
    let mut stack = values.to_vec();
    while let Some(v) = stack.pop() {
        match &v.data {
            Data::Tensor(t) => visit(&t.storage)?,
            Data::Wrapped(v) | Data::Success(v) | Data::Failure(v) => stack.push(v.clone()),
            Data::Record(fs) => stack.extend(fs.values().cloned()),
            Data::List(v) => stack.extend_from_slice(v),
            Data::Map(v) => {
                for (k, v) in v {
                    stack.push(k.clone());
                    stack.push(v.clone())
                }
            }
            Data::Range(a, b, _, _) => {
                stack.push(a.clone());
                stack.push(b.clone())
            }
            Data::Error(e) => {
                stack.push(e.input.clone());
                stack.extend_from_slice(&e.trace);
                stack.extend_from_slice(&e.flow_progress)
            }
            Data::Adapter(value) => {
                if let Some(fields) = value.fields() {
                    stack.extend(fields.values().cloned());
                }
            }
            _ => (),
        }
    }
    Ok(())
}
pub fn validate_inputs(c: &Context, values: &[Handle]) -> Result<(), Fault> {
    visit_storages(values, |storage| c.resources.validate(storage))
}
pub fn allocations(values: &[Handle]) -> Vec<u64> {
    let mut out = vec![];
    if visit_storages(values, |storage| {
        out.push(storage.id);
        Ok(())
    })
    .is_err()
    {
        std::process::abort()
    }
    out.sort_unstable();
    out.dedup();
    out
}
pub fn construct(c: &mut Context, shape: &Shape, values: Vec<f32>) -> Result<Tensor, Fault> {
    raw_check_cancel(c)?;
    if values.len() as u64 != shape.count {
        return Err(invalid(
            format!(
                "{} elements for shape {}",
                shape.count,
                fmt_dims(&shape.dims)
            ),
            values.len().to_string(),
        ));
    }
    for (i, v) in values.iter().enumerate() {
        if !v.is_finite() {
            return Err(numeric("Numeric.NonFinite", i, "finite FP32 conversion"));
        }
    }
    let storage = c.resources.allocate(values, "cpu")?;
    Ok(Tensor {
        shape: shape.clone(),
        storage,
    })
}
pub fn transfer(c: &mut Context, a: &Tensor, destination: &str) -> Result<Tensor, Fault> {
    raw_check_cancel(c)?;
    c.resources.validate(&a.storage)?;
    for selector in [&a.storage.location, destination] {
        if selector != "cpu" && selector != &c.device_grant {
            return Err(Fault::Host(
                "runtime.capability-denied",
                format!("Tensor.to {selector:?} has no host capability"),
            ));
        }
    }
    if a.storage.location == destination {
        return Ok(a.clone());
    }
    if a.storage.location != "cpu" && destination != "cpu" {
        return Err(Fault::Numeric(
            "Tensor.DeviceMismatch",
            "transfers require CPU and one compute location".into(),
        ));
    }
    Err(Fault::Numeric(
        "Device.Unavailable",
        format!("Device.Unavailable: backend {destination} is not present in this build"),
    ))
}
#[derive(Clone)]
struct Plan {
    op: String,
    shape: Shape,
    axes: Vec<usize>,
    steps: u64,
    region: u64,
}
fn broadcast(a: &Shape, b: &Shape) -> Result<Shape, Fault> {
    let mut dims = vec![1; a.dims.len().max(b.dims.len())];
    let rank = dims.len();
    for i in 1..=rank {
        let x = if i <= a.dims.len() {
            a.dims[a.dims.len() - i]
        } else {
            1
        };
        let y = if i <= b.dims.len() {
            b.dims[b.dims.len() - i]
        } else {
            1
        };
        dims[rank - i] = if x == y || y == 1 {
            x
        } else if x == 1 {
            y
        } else {
            return Err(mismatch(
                format!("broadcast dimensions compatible with {}", fmt_dims(&a.dims)),
                fmt_dims(&b.dims),
            ));
        }
    }
    Shape::new(dims)
}
fn axes(op: &str, indices: &[i64], rank: usize) -> Result<Vec<usize>, Fault> {
    let mut out = vec![];
    for (i, n) in indices.iter().enumerate() {
        if *n < 0 {
            return Err(invalid("nonnegative signed 64-bit axis", fmt_dims(indices)));
        }
        if *n as usize >= rank {
            return Err(invalid(format!("axis in [0,{rank})"), fmt_dims(indices)));
        }
        if out.contains(&(*n as usize)) || op != "Tensor.transpose" && i > 0 && indices[i - 1] >= *n
        {
            return Err(invalid(
                "unique axes; reduction axes in increasing order",
                fmt_dims(indices),
            ));
        }
        out.push(*n as usize)
    }
    if op == "Tensor.transpose" && out.len() != rank {
        return Err(invalid(
            format!("permutation of {rank} axes"),
            fmt_dims(indices),
        ));
    }
    Ok(out)
}
fn prepare(op: &str, a: &Shape, b: Option<&Shape>, indices: &[i64]) -> Result<Plan, Fault> {
    let mut p = Plan {
        op: op.into(),
        shape: a.clone(),
        axes: vec![],
        steps: 0,
        region: 1,
    };
    match op {
        "Tensor.add" | "Tensor.sub" | "Tensor.mul" | "Tensor.div" => {
            p.shape = broadcast(a, b.unwrap())?
        }
        "Tensor.sqrt" | "Tensor.relu" => (),
        "Tensor.matmul" => {
            let b = b.unwrap();
            if a.dims.len() != 2 || b.dims.len() != 2 {
                return Err(mismatch(
                    "rank-two [m,k] and [k,n]",
                    format!("{} and {}", fmt_dims(&a.dims), fmt_dims(&b.dims)),
                ));
            }
            if a.dims[1] != b.dims[0] {
                return Err(mismatch(
                    format!(
                        "right shape [{},n] for left {}",
                        a.dims[1],
                        fmt_dims(&a.dims)
                    ),
                    fmt_dims(&b.dims),
                ));
            }
            p.shape = Shape::new(vec![a.dims[0], b.dims[1]])?
        }
        "Tensor.reshape" | "Tensor.broadcast" => {
            p.shape = Shape::new(indices.to_vec())?;
            if op == "Tensor.reshape" {
                if a.count != p.shape.count {
                    return Err(mismatch(
                        format!("{} elements for reshape of {}", a.count, fmt_dims(&a.dims)),
                        fmt_dims(indices),
                    ));
                }
            } else {
                let s = broadcast(a, &p.shape)?;
                if s != p.shape {
                    return Err(mismatch(
                        format!("broadcast target compatible with {}", fmt_dims(&a.dims)),
                        fmt_dims(indices),
                    ));
                }
            }
        }
        "Tensor.transpose" | "Tensor.sum" | "Tensor.mean" => {
            p.axes = axes(op, indices, a.dims.len())?;
            let dims = if op == "Tensor.transpose" {
                p.axes.iter().map(|i| a.dims[*i]).collect()
            } else {
                a.dims
                    .iter()
                    .enumerate()
                    .filter_map(|(i, d)| {
                        if p.axes.contains(&i) {
                            if *d == 0 {
                                p.region = 0
                            }
                            None
                        } else {
                            Some(*d)
                        }
                    })
                    .collect()
            };
            p.shape = Shape::new(dims)?
        }
        _ => return Err(host(format!("unknown tensor primitive {op}"))),
    }
    p.shape.limits()?;
    p.steps = match op {
        "Tensor.reshape" => 0,
        "Tensor.matmul" => 2 * p.shape.count * a.dims[1] as u64,
        "Tensor.sum" | "Tensor.mean" => {
            if p.shape.count != 0 {
                p.region = a.count / p.shape.count
            }
            a.count
                + if op == "Tensor.mean" {
                    p.shape.count
                } else {
                    0
                }
        }
        _ => p.shape.count,
    };
    Ok(p)
}
fn broadcast_index(mut index: usize, out: &[i64], input: &[i64]) -> usize {
    let mut offset = 0;
    let mut stride = 1;
    for j in (0..input.len()).rev() {
        let i = j + out.len() - input.len();
        let coordinate = index % out[i] as usize;
        index /= out[i] as usize;
        if input[j] != 1 {
            offset += coordinate * stride
        }
        stride *= input[j] as usize
    }
    offset
}
fn raw_check_cancel(c: &Context) -> Result<(), Fault> {
    c.check_cancel().map_err(|fault| match fault {
        Fault::Host("runtime.cancelled", _) => Fault::RawCancellation,
        other => other,
    })
}
fn poll(c: &Context, i: usize) -> Result<(), Fault> {
    if i % 1024 == 0 {
        raw_check_cancel(c)
    } else {
        Ok(())
    }
}
fn execute_plan(
    c: &mut Context,
    p: &Plan,
    a: &Tensor,
    b: Option<&Tensor>,
) -> Result<Tensor, Fault> {
    raw_check_cancel(c)?;
    c.resources.validate(&a.storage)?;
    if let Some(b) = b {
        c.resources.validate(&b.storage)?
    }
    if a.storage.location != "cpu" {
        return Err(Fault::Numeric(
            "Device.UnsupportedOperation",
            format!(
                "Device.UnsupportedOperation: tensor arithmetic is unavailable on {}",
                a.storage.location
            ),
        ));
    }
    if p.op == "Tensor.reshape"
        || matches!(p.op.as_str(), "Tensor.sum" | "Tensor.mean") && p.axes.is_empty()
    {
        return Ok(Tensor {
            shape: p.shape.clone(),
            storage: a.storage.clone(),
        });
    }
    if p.op == "Tensor.mean" && p.region == 0 {
        return Err(Fault::Numeric(
            "Tensor.EmptyReduction",
            format!(
                "expected nonempty mean reduction region, got {}",
                fmt_dims(&a.shape.dims)
            ),
        ));
    }
    c.resources.check_allocation(4 * p.shape.count)?;
    let mut out = vec![0f32; p.shape.count as usize];
    if out.is_empty() {
        return construct(c, &p.shape, out);
    }
    let ad = &a.shape.dims;
    let od = &p.shape.dims;
    let av = &a.storage.values;
    let bv = b.map(|b| b.storage.values.as_slice()).unwrap_or(&[]);
    match p.op.as_str() {
        "Tensor.matmul" => {
            let bd = &b.unwrap().shape.dims;
            let k = ad[1] as usize;
            let n = bd[1] as usize;
            for i in 0..out.len() {
                poll(c, i)?;
                for j in 0..k {
                    poll(c, j)?;
                    let product = av[(i / n) * k + j] * bv[j * n + i % n];
                    if !product.is_finite() {
                        return Err(numeric(
                            "Numeric.NonFinite",
                            i,
                            format!("finite product at contracted index {j}"),
                        ));
                    }
                    let sum = out[i] + product;
                    if !sum.is_finite() {
                        return Err(numeric(
                            "Numeric.NonFinite",
                            i,
                            format!("finite sum at contracted index {j}"),
                        ));
                    }
                    out[i] = sum;
                }
            }
        }
        "Tensor.sum" | "Tensor.mean" => {
            for i in 0..av.len() {
                poll(c, i)?;
                let (mut index, mut stride, mut rest) = (0, 1, i);
                for axis in (0..ad.len()).rev() {
                    let coordinate = rest % ad[axis] as usize;
                    rest /= ad[axis] as usize;
                    if !p.axes.contains(&axis) {
                        index += coordinate * stride;
                        stride *= ad[axis] as usize
                    }
                }
                let sum = out[index] + av[i];
                if !sum.is_finite() {
                    return Err(numeric(
                        "Numeric.NonFinite",
                        i,
                        "finite reduction intermediate",
                    ));
                }
                out[index] = sum;
            }
            if p.op == "Tensor.mean" {
                for (i, v) in out.iter_mut().enumerate() {
                    poll(c, i)?;
                    *v /= p.region as f32;
                    if !v.is_finite() {
                        return Err(numeric("Numeric.NonFinite", i, "finite mean"));
                    }
                }
            }
        }
        "Tensor.transpose" => {
            let mut strides = vec![0; ad.len()];
            let mut stride = 1;
            for i in (0..ad.len()).rev() {
                strides[i] = stride;
                stride *= ad[i] as usize
            }
            for i in 0..out.len() {
                poll(c, i)?;
                let (mut index, mut rest) = (0, i);
                for axis in (0..od.len()).rev() {
                    index += (rest % od[axis] as usize) * strides[p.axes[axis]];
                    rest /= od[axis] as usize
                }
                out[i] = av[index]
            }
        }
        _ => {
            for i in 0..out.len() {
                poll(c, i)?;
                let x = av[broadcast_index(i, od, ad)];
                let y = b
                    .map(|b| bv[broadcast_index(i, od, &b.shape.dims)])
                    .unwrap_or(0.0);
                let z = match p.op.as_str() {
                    "Tensor.add" => x + y,
                    "Tensor.sub" => x - y,
                    "Tensor.mul" => x * y,
                    "Tensor.div" => {
                        if y == 0.0 {
                            return Err(numeric("Numeric.DivideByZero", i, "nonzero divisor"));
                        }
                        x / y
                    }
                    "Tensor.sqrt" => {
                        if x < 0.0 {
                            return Err(numeric(
                                "Numeric.Domain",
                                i,
                                "nonnegative square-root input",
                            ));
                        }
                        (x as f64).sqrt() as f32
                    }
                    "Tensor.relu" => {
                        if x > 0.0 {
                            x
                        } else {
                            0.0
                        }
                    }
                    "Tensor.broadcast" => x,
                    _ => return Err(host("unknown CPU tensor primitive")),
                };
                if !z.is_finite() {
                    return Err(numeric("Numeric.NonFinite", i, "finite FP32 result"));
                }
                out[i] = z;
            }
        }
    }
    construct(c, &p.shape, out)
}
pub fn apply(
    c: &mut Context,
    op: &str,
    a: &Tensor,
    b: Option<&Tensor>,
    indices: &[i64],
) -> Result<Tensor, Fault> {
    if b.is_some_and(|b| b.storage.location != a.storage.location) {
        return Err(Fault::Numeric(
            "Tensor.DeviceMismatch",
            format!(
                "Tensor.DeviceMismatch: numeric operation {op} requires matching input locations"
            ),
        ));
    }
    let p = prepare(op, &a.shape, b.map(|b| &b.shape), indices)?;
    let weight = p.shape.weight() - 3 + a.storage.location.len() as u64;
    c.tensor_budget(p.steps, weight)?;
    let out = execute_plan(c, &p, a, b)?;
    c.weight += out.weight();
    Ok(out)
}
pub fn constant(c: &mut Context, shape: &Shape, location: &str, n: f32) -> Result<Tensor, Fault> {
    c.tensor_budget(shape.count, shape.weight() - 3 + location.len() as u64)?;
    if location != "cpu" {
        return Err(Fault::Numeric(
            "Device.UnsupportedOperation",
            format!("tensor arithmetic is unavailable on {location}"),
        ));
    }
    c.resources.check_allocation(4 * shape.count)?;
    let out = construct(c, shape, vec![n; shape.count as usize])?;
    c.weight += out.weight();
    Ok(out)
}
pub fn relu_derivative(c: &mut Context, g: &Tensor, a: &Tensor) -> Result<Tensor, Fault> {
    if a.storage.location != g.storage.location {
        return Err(Fault::Numeric(
            "Tensor.DeviceMismatch",
            "Tensor.DeviceMismatch: derivative inputs require matching locations".into(),
        ));
    }
    c.tensor_budget(a.shape.count, a.weight())?;
    c.resources.validate(&a.storage)?;
    c.resources.validate(&g.storage)?;
    c.resources.check_allocation(4 * a.shape.count)?;
    let mut out = vec![0.0; a.shape.count as usize];
    for (i, v) in out.iter_mut().enumerate() {
        poll(c, i)?;
        if a.storage.values[i] > 0.0 {
            *v = g.storage.values[i]
        }
    }
    let out = construct(c, &a.shape, out)?;
    c.weight += out.weight();
    Ok(out)
}
pub fn check_sqrt(c: &mut Context, a: &Tensor) -> Result<(), Fault> {
    c.resources.validate(&a.storage)?;
    for (i, x) in a.storage.values.iter().enumerate() {
        poll(c, i)?;
        if *x == 0.0 {
            return Err(numeric(
                "Autodiff.UndefinedDerivative",
                i,
                "nonzero active square-root input",
            ));
        }
    }
    Ok(())
}
pub fn contextualize(s: &Site, a: &[Handle], fault: Fault) -> Fault {
    if matches!(fault, Fault::Budget { .. } | Fault::RawCancellation) {
        return boundary_host_fault(fault);
    }

    if let Fault::Host("runtime.limit", msg) = &fault {
        if [
            "execution limit: tensor rank",
            "execution limit: tensor dimension",
            "execution limit: tensor elements",
        ]
        .iter()
        .any(|p| msg.starts_with(p))
        {
            return limit(&format!(
                "{}: {}",
                s.operation,
                msg.strip_prefix("execution limit: ").unwrap()
            ));
        }
    }
    if let Fault::Numeric(kind, msg) = fault {
        let v = operation_failure(s, a, kind, &format!("{}: {msg}", s.operation));
        let Data::Failure(e) = &v.data else {
            std::process::abort()
        };
        Fault::Recoverable(e.clone())
    } else {
        fault
    }
}
pub const SUPPORTED: &[&str] = &[
    "Autodiff.stop",
    "Device.new",
    "Tensor.new",
    "Tensor.to",
    "Tensor.shape",
    "Tensor.rank",
    "Tensor.dtype",
    "Tensor.device",
    "Tensor.to_float",
    "Tensor.values",
    "Tensor.add",
    "Tensor.sub",
    "Tensor.mul",
    "Tensor.div",
    "Tensor.sqrt",
    "Tensor.relu",
    "Tensor.matmul",
    "Tensor.broadcast",
    "Tensor.reshape",
    "Tensor.transpose",
    "Tensor.sum",
    "Tensor.mean",
];
fn indices(v: &Handle, expected: &str) -> Result<Vec<i64>, Fault> {
    v.items()
        .iter()
        .map(|x| {
            x.integer().to_i64().filter(|n| *n >= 0).ok_or_else(|| {
                invalid(
                    expected,
                    format!(
                        "[{}]",
                        v.items()
                            .iter()
                            .map(|x| x.integer().to_string())
                            .collect::<Vec<_>>()
                            .join(" ")
                    ),
                )
            })
        })
        .collect()
}
pub fn execute(c: &mut Context, s: &Site, a: &[Handle]) -> Outcome {
    let op = s.operation.as_str();
    let result = (|| -> Outcome {
        if op == "Autodiff.stop" {
            c.resources.validate(&data(&a[0]).storage)?;
            return Ok(a[0].clone());
        }
        if op == "Device.new" {
            let d = a[0].string();
            if !matches!(d, "cpu" | "cuda:0" | "cuda-sim:0" | "metal:0") {
                let out = operation_failure(
                    s,
                    a,
                    "Device.InvalidSelector",
                    &format!("invalid device selector {d:?}"),
                );
                let Data::Failure(e) = &out.data else {
                    std::process::abort()
                };
                return Err(Fault::Recoverable(e.clone()));
            }
            return Ok(new(atom("Device"), Data::Device(d.into())));
        }
        if op == "Tensor.new" {
            let shape = Shape::new(indices(&a[1], "nonnegative signed 64-bit dimensions")?)?;
            shape.limits().map_err(|f| match f {
                Fault::Host(_, m) => limit(&format!(
                    "{op}: {}",
                    m.strip_prefix("execution limit: ").unwrap_or(&m)
                )),
                f => f,
            })?;
            let items = a[0].items();
            if items.len() as u64 != shape.count {
                return Err(invalid(
                    format!(
                        "{} elements for shape {}",
                        shape.count,
                        fmt_dims(&shape.dims)
                    ),
                    items.len().to_string(),
                ));
            }
            c.tensor_budget(shape.count, shape.weight() + 16)?;
            c.resources.check_allocation(4 * shape.count)?;
            let mut values = Vec::with_capacity(items.len());
            for (i, v) in items.iter().enumerate() {
                poll(c, i)?;
                let n = v.number() as f32;
                if !n.is_finite() {
                    return Err(numeric("Numeric.NonFinite", i, "finite FP32 conversion"));
                }
                values.push(n)
            }
            return Ok(value(construct(c, &shape, values)?));
        }
        let t = data(&a[0]);
        c.resources.validate(&t.storage)?;
        match op {
            "Tensor.shape" => Ok(new(
                Type::List(Box::new(atom("Int"))),
                Data::List(t.shape.dims.iter().map(|d| int(*d)).collect()),
            )),
            "Tensor.rank" => Ok(int(t.shape.dims.len())),
            "Tensor.dtype" => Ok(text("fp32")),
            "Tensor.device" => Ok(new(
                atom("Device"),
                Data::Device(t.storage.location.clone()),
            )),
            "Tensor.to" => {
                let Data::Device(destination) = &a[1].data else {
                    return Err(host("invalid Device value"));
                };
                for selector in [&t.storage.location, destination] {
                    if selector != "cpu" && selector != &c.device_grant {
                        return Err(Fault::Host(
                            "runtime.capability-denied",
                            format!("Tensor.to {selector:?} has no host capability"),
                        ));
                    }
                }
                if t.storage.location != "cpu"
                    && destination != "cpu"
                    && t.storage.location != *destination
                {
                    return Err(Fault::Numeric(
                        "Tensor.DeviceMismatch",
                        "transfers require CPU and one compute location".into(),
                    ));
                }
                let steps = if &t.storage.location == destination {
                    0
                } else {
                    t.shape.count
                };
                c.tensor_budget(steps, t.shape.weight() - 3 + destination.len() as u64 + 16)?;
                Ok(value(transfer(c, t, destination)?))
            }
            "Tensor.to_float" => {
                if t.storage.location != "cpu" {
                    return Err(Fault::Numeric(
                        "Tensor.HostObservationRequired",
                        "transfer tensor to CPU before numeric observation".into(),
                    ));
                }
                if !t.shape.dims.is_empty() {
                    return Err(mismatch("rank-zero shape []", fmt_dims(&t.shape.dims)));
                }
                c.tensor_budget(1, 40)?;
                Ok(new(atom("Float"), Data::Float(t.storage.values[0] as f64)))
            }
            "Tensor.values" => {
                if t.storage.location != "cpu" {
                    return Err(Fault::Numeric(
                        "Tensor.HostObservationRequired",
                        "transfer tensor to CPU before numeric observation".into(),
                    ));
                }
                c.tensor_budget(t.shape.count, 32 + 24 * t.shape.count)?;
                let mut values = vec![];
                for (i, n) in t.storage.values.iter().enumerate() {
                    poll(c, i)?;
                    values.push(new(atom("Float"), Data::Float(*n as f64)))
                }
                Ok(new(Type::List(Box::new(atom("Float"))), Data::List(values)))
            }
            _ => {
                let mut b = None;
                let mut axes = vec![];
                if a.len() == 2 {
                    if let Data::Tensor(t) = &a[1].data {
                        b = Some(t)
                    } else {
                        axes = indices(
                            &a[1],
                            if matches!(op, "Tensor.reshape" | "Tensor.broadcast") {
                                "nonnegative signed 64-bit dimensions"
                            } else {
                                "nonnegative signed 64-bit axis"
                            },
                        )?
                    }
                }
                if b.is_some_and(|b| b.storage.location != t.storage.location) {
                    return Err(Fault::Numeric(
                        "Tensor.DeviceMismatch",
                        "tensor input locations differ".into(),
                    ));
                }
                let p = prepare(op, &t.shape, b.map(|b| &b.shape), &axes)?;
                c.tensor_budget(
                    p.steps,
                    p.shape.weight() - 3 + t.storage.location.len() as u64 + 16,
                )?;
                Ok(value(execute_plan(c, &p, t, b)?))
            }
        }
    })();
    result.map_err(|f| contextualize(s, a, f))
}
pub fn admit(s: &Site) -> Result<(), String> {
    let op = s.operation.as_str();
    let tensor = atom("Tensor<FP32>");
    let ints = Type::List(Box::new(atom("Int")));
    let floats = Type::List(Box::new(atom("Float")));
    let (inputs, out) = match op {
        "Autodiff.stop" => (vec![tensor.clone()], tensor.clone()),
        "Device.new" => (
            vec![atom("String")],
            Type::Fallible(Box::new(atom("Device"))),
        ),
        "Tensor.new" => (
            vec![floats.clone(), ints.clone()],
            Type::Fallible(Box::new(tensor.clone())),
        ),
        "Tensor.to" => (
            vec![tensor.clone(), atom("Device")],
            Type::Fallible(Box::new(tensor.clone())),
        ),
        "Tensor.shape" => (vec![tensor.clone()], ints.clone()),
        "Tensor.rank" => (vec![tensor.clone()], atom("Int")),
        "Tensor.dtype" => (vec![tensor.clone()], atom("String")),
        "Tensor.device" => (vec![tensor.clone()], atom("Device")),
        "Tensor.to_float" => (
            vec![tensor.clone()],
            Type::Fallible(Box::new(atom("Float"))),
        ),
        "Tensor.values" => (vec![tensor.clone()], Type::Fallible(Box::new(floats))),
        "Tensor.add" | "Tensor.sub" | "Tensor.mul" | "Tensor.div" | "Tensor.matmul" => (
            vec![tensor.clone(), tensor.clone()],
            Type::Fallible(Box::new(tensor.clone())),
        ),
        "Tensor.sqrt" | "Tensor.relu" => (
            vec![tensor.clone()],
            Type::Fallible(Box::new(tensor.clone())),
        ),
        "Tensor.reshape" | "Tensor.broadcast" | "Tensor.transpose" | "Tensor.sum"
        | "Tensor.mean" => (vec![tensor.clone(), ints], Type::Fallible(Box::new(tensor))),
        _ => return Err("unknown tensor operation".into()),
    };
    if out != s.typ
        || inputs.len() != s.arguments.len()
        || s.arguments
            .iter()
            .zip(inputs)
            .any(|(a, t)| !matches!(a,Argument::Value(v)if *v==t))
    {
        Err(format!("invalid native tensor contract {op}"))
    } else {
        Ok(())
    }
}
