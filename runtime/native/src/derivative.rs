use crate::{
    context::{Context, Fault},
    tensor::{self, Shape, Tensor},
};
struct Calculation<'a> {
    context: &'a mut Context,
    location: String,
}
impl Calculation<'_> {
    fn apply(
        &mut self,
        op: &str,
        a: &Tensor,
        b: Option<&Tensor>,
        indices: &[i64],
    ) -> Result<Tensor, Fault> {
        tensor::apply(self.context, op, a, b, indices)
    }
    fn scalar(&mut self, n: f32) -> Result<Tensor, Fault> {
        tensor::constant(self.context, &Shape::new(vec![])?, &self.location, n)
    }
    fn negate(&mut self, v: &Tensor) -> Result<Tensor, Fault> {
        let n = self.scalar(-1.0)?;
        self.apply("Tensor.mul", &n, Some(v), &[])
    }
    fn unbroadcast(&mut self, g: &Tensor, shape: &Shape) -> Result<Tensor, Fault> {
        if g.shape.dims == shape.dims {
            return Ok(g.clone());
        }
        let shift = g.shape.dims.len() - shape.dims.len();
        let axes = g
            .shape
            .dims
            .iter()
            .enumerate()
            .filter_map(|(i, d)| {
                if i < shift || shape.dims[i - shift] == 1 && *d != 1 {
                    Some(i as i64)
                } else {
                    None
                }
            })
            .collect::<Vec<_>>();
        let mut out = g.clone();
        if !axes.is_empty() {
            out = self.apply("Tensor.sum", &out, None, &axes)?
        }
        self.apply("Tensor.reshape", &out, None, &shape.dims)
    }
}
pub fn vjp(
    c: &mut Context,
    op: &str,
    operand: usize,
    g: &Tensor,
    a: &Tensor,
    b: Option<&Tensor>,
    result: &Tensor,
    indices: &[i64],
) -> Result<Tensor, Fault> {
    let mut c = Calculation {
        context: c,
        location: g.storage.location.clone(),
    };
    let mut out = match op {
        "Tensor.add" => g.clone(),
        "Tensor.sub" => {
            if operand == 1 {
                c.negate(g)?
            } else {
                g.clone()
            }
        }
        "Tensor.mul" => c.apply(
            "Tensor.mul",
            g,
            Some(if operand == 1 { a } else { b.unwrap() }),
            &[],
        )?,
        "Tensor.div" => {
            let mut out = c.apply("Tensor.div", g, b, &[])?;
            if operand == 1 {
                out = c.negate(&out)?;
                let ratio = c.apply("Tensor.div", a, b, &[])?;
                out = c.apply("Tensor.mul", &out, Some(&ratio), &[])?
            }
            out
        }
        "Tensor.sqrt" => {
            tensor::check_sqrt(c.context, a)?;
            let two = c.scalar(2.0)?;
            let denominator = c.apply("Tensor.mul", &two, Some(result), &[])?;
            c.apply("Tensor.div", g, Some(&denominator), &[])?
        }
        "Tensor.relu" => tensor::relu_derivative(c.context, g, a)?,
        "Tensor.matmul" => {
            if operand == 0 {
                let bt = c.apply("Tensor.transpose", b.unwrap(), None, &[1, 0])?;
                c.apply("Tensor.matmul", g, Some(&bt), &[])?
            } else {
                let at = c.apply("Tensor.transpose", a, None, &[1, 0])?;
                c.apply("Tensor.matmul", &at, Some(g), &[])?
            }
        }
        "Tensor.broadcast" => c.unbroadcast(g, &a.shape)?,
        "Tensor.reshape" => c.apply("Tensor.reshape", g, None, &a.shape.dims)?,
        "Tensor.transpose" => {
            let mut inverse = vec![0; indices.len()];
            for (i, axis) in indices.iter().enumerate() {
                inverse[*axis as usize] = i as i64
            }
            c.apply("Tensor.transpose", g, None, &inverse)?
        }
        "Tensor.sum" | "Tensor.mean" => {
            let mut shape = a.shape.dims.clone();
            let mut count = 1i64;
            for axis in indices {
                count *= shape[*axis as usize];
                shape[*axis as usize] = 1
            }
            let mut out = g.clone();
            if op == "Tensor.mean" && !indices.is_empty() {
                let n = c.scalar(count as f32)?;
                out = c.apply("Tensor.div", g, Some(&n), &[])?
            }
            if !indices.is_empty() {
                out = c.apply("Tensor.reshape", &out, None, &shape)?;
                out = c.apply("Tensor.broadcast", &out, None, &a.shape.dims)?
            }
            out
        }
        _ => {
            return Err(crate::context::host(format!(
                "missing derivative rule {op}"
            )))
        }
    };
    if matches!(
        op,
        "Tensor.add" | "Tensor.sub" | "Tensor.mul" | "Tensor.div"
    ) {
        out = c.unbroadcast(
            &out,
            if operand == 0 {
                &a.shape
            } else {
                &b.unwrap().shape
            },
        )?
    }
    Ok(out)
}
