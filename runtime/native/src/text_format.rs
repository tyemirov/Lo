use crate::{context::*, processors::Scratch, value::*};
use num_bigint::{BigInt, BigUint};
use num_traits::{One, ToPrimitive, Zero};

pub const SUPPORTED: &[&str] = &["Int.parse", "Float.format_fixed", "String.format"];
pub fn parameter_names(op: &str) -> Vec<&'static str> {
    match op {
        "Int.parse" => vec!["text"],
        "Float.format_fixed" => vec!["value", "places"],
        _ => vec!["template", "fields"],
    }
}
pub fn admit(s: &Site) -> Result<(), String> {
    let expected = match s.operation.as_str() {
        "Int.parse" => vec![atom("String")],
        "Float.format_fixed" => vec![atom("Float"), atom("Int")],
        "String.format" => {
            let Some(Argument::Value(Type::Record(fields))) = s.arguments.get(1) else {
                return Err("String.format requires a concrete record".into());
            };
            if fields
                .iter()
                .any(|(_, t)| !matches!(t, Type::Atom("String" | "Int" | "Float" | "Bool")))
            {
                return Err("String.format requires total scalar fields".into());
            }
            vec![atom("String"), Type::Record(fields.clone())]
        }
        _ => return Err("unknown formatter".into()),
    };
    let output = if s.operation == "Int.parse" {
        atom("Int")
    } else {
        atom("String")
    };
    if s.typ != Type::Fallible(Box::new(output))
        || expected.len() != s.arguments.len()
        || expected
            .iter()
            .zip(&s.arguments)
            .any(|(t, a)| !matches!(a,Argument::Value(v) if t==v))
    {
        return Err("formatter metadata mismatch".into());
    }
    Ok(())
}
fn invalid(kind: &'static str, message: &str) -> Fault {
    Fault::Numeric(kind, message.into())
}
pub fn decimal_token(text: &str) -> bool {
    let bytes = text.as_bytes();
    let mut i = usize::from(bytes.first() == Some(&b'-'));
    match bytes.get(i) {
        Some(b'0') => i += 1,
        Some(b'1'..=b'9') => {
            while bytes.get(i).is_some_and(u8::is_ascii_digit) {
                i += 1;
            }
        }
        _ => return false,
    }
    for mark in [b'.', b'e'] {
        if bytes
            .get(i)
            .is_some_and(|b| *b == mark || mark == b'e' && *b == b'E')
        {
            i += 1;
            if mark == b'e' && bytes.get(i).is_some_and(|b| matches!(b, b'+' | b'-')) {
                i += 1;
            }
            let start = i;
            while bytes.get(i).is_some_and(u8::is_ascii_digit) {
                i += 1;
            }
            if i == start {
                return false;
            }
        }
    }
    i == bytes.len()
}
pub fn integer(c: &mut Context, text: &str, kind: &'static str) -> Result<BigInt, Fault> {
    let digits = text.strip_prefix(['+', '-']).unwrap_or(text);
    if digits.is_empty() {
        return Err(invalid(kind, "integer requires decimal digits"));
    }
    for b in digits.bytes() {
        c.step()?;
        if !b.is_ascii_digit() {
            return Err(invalid(kind, "integer requires decimal digits"));
        }
    }
    let significant = digits.trim_start_matches('0');
    // floor(log10(2) * MAX_BITS) + 1 is an upper decimal digit bound.
    if significant.len() as u64 > MAX_BITS * 30103 / 100000 + 1 {
        return Err(limit("integer bits"));
    }
    let bytes = (significant.len() as u64)
        .checked_mul(4)
        .and_then(|n| n.checked_add(32))
        .ok_or_else(|| limit("integer conversion bytes"))?;
    c.values
        .preflight((significant.len() as u64 * 3322 / 1000 + 7) / 8 + 16)?;
    let _scratch = Scratch::new(c, bytes)?;
    crate::json::work(c, significant.len() as u64)?;
    let mut number = if significant.is_empty() {
        BigInt::zero()
    } else {
        BigInt::parse_bytes(significant.as_bytes(), 10)
            .ok_or_else(|| invalid(kind, "invalid decimal integer"))?
    };
    c.check_cancel()?;
    if number.bits() > MAX_BITS {
        return Err(limit("integer bits"));
    }
    if text.starts_with('-') {
        number = -number;
    }
    Ok(number)
}
pub fn float(c: &mut Context, text: &str) -> Result<f64, Fault> {
    crate::json::work(c, text.len() as u64)?;
    let _scratch = Scratch::new(c, text.len() as u64 + 64)?;
    let number = text.parse::<f64>().map_err(|_| {
        invalid(
            "JSON.InvalidNumber",
            "number cannot be represented as Float",
        )
    })?;
    let nonzero = text
        .bytes()
        .take_while(|b| !matches!(b, b'e' | b'E'))
        .any(|b| matches!(b, b'1'..=b'9'));
    if !number.is_finite() || number == 0.0 && nonzero {
        return Err(invalid(
            "JSON.InvalidNumber",
            "number is outside finite nonzero Float range",
        ));
    }
    c.check_cancel()?;
    Ok(number)
}
fn fixed(c: &mut Context, number: f64, places: usize) -> Result<String, Fault> {
    let maximum = if number.abs() >= 1.0 { 310 } else { 2 } + places + usize::from(places != 0);

    let _scratch = Scratch::new(c, maximum as u64 * 4 + 1024)?;
    crate::json::work(c, places as u64 + 64)?;
    let bits = number.to_bits();
    let exponent = ((bits >> 52) & 2047) as i32;
    let significand = (bits & ((1u64 << 52) - 1)) | if exponent == 0 { 0 } else { 1u64 << 52 };
    let shift = if exponent == 0 {
        -1074
    } else {
        exponent - 1023 - 52
    } + places as i32;
    let mut scaled = BigUint::from(significand) * BigUint::from(5u8).pow(places as u32);
    if shift >= 0 {
        scaled <<= shift as usize;
    } else {
        let power = (-shift) as usize;
        let quotient = &scaled >> power;
        let remainder = &scaled - (&quotient << power);
        let half = BigUint::one() << (power - 1);
        let increment =
            remainder > half || remainder == half && (&quotient & BigUint::one()) == BigUint::one();
        scaled = quotient + BigUint::from(u8::from(increment));
    }
    let digits = scaled.to_string();
    let exact = digits.len().max(places + 1)
        + usize::from(places != 0)
        + usize::from(number.is_sign_negative());
    c.values.preflight(exact as u64 + 16)?;
    let mut output = String::with_capacity(exact);
    if number.is_sign_negative() {
        output.push('-');
    }
    if places == 0 {
        output.push_str(&digits);
    } else if digits.len() <= places {
        output.push_str("0.");
        output.extend(std::iter::repeat('0').take(places - digits.len()));
        output.push_str(&digits);
    } else {
        let split = digits.len() - places;
        output.push_str(&digits[..split]);
        output.push('.');
        output.push_str(&digits[split..]);
    }
    crate::json::work(c, output.len() as u64)?;
    Ok(output)
}
fn scalar(
    c: &mut Context,
    scratch: &mut Scratch,
    value: &Handle,
    grouped: bool,
) -> Result<String, Fault> {
    if grouped && !matches!(value.data, Data::Int(_)) {
        return Err(invalid("String.InvalidFormat", "grouping requires Int"));
    }
    let max = value
        .weight
        .checked_mul(4)
        .and_then(|n| n.checked_add(32))
        .ok_or_else(|| limit("format field bytes"))?;
    scratch.grow(max)?;
    let plain = match &value.data {
        Data::Text(s) => s.clone(),
        Data::Int(n) => n.to_string(),
        Data::Float(n) => format_float(*n),
        Data::Bool(b) => b.to_string(),
        _ => return Err(host("invalid formatter field")),
    };
    crate::json::work(c, plain.len() as u64)?;
    if !grouped {
        return Ok(plain);
    }
    let mut output = String::with_capacity(plain.len() + plain.len() / 3);
    let digits = plain.strip_prefix('-').unwrap_or(&plain);
    if plain.starts_with('-') {
        output.push('-');
    }
    for (index, b) in digits.bytes().enumerate() {
        if index > 0 && (digits.len() - index) % 3 == 0 {
            output.push(',');
        }
        output.push(b as char);
    }
    Ok(output)
}
fn format(c: &mut Context, template: &str, fields: &Handle) -> Result<String, Fault> {
    let mut output = String::new();
    let mut i = 0;
    let bytes = template.as_bytes();
    let mut scratch = Scratch::new(c, 0)?;
    while i < bytes.len() {
        c.step()?;
        let (piece, width) = if bytes[i] == b'{' && bytes.get(i + 1) == Some(&b'{')
            || bytes[i] == b'}' && bytes.get(i + 1) == Some(&b'}')
        {
            let piece = (bytes[i] as char).to_string();
            i += 2;
            (piece, 0)
        } else if bytes[i] == b'{' {
            i += 1;
            let start = i;
            if !bytes
                .get(i)
                .is_some_and(|b| b.is_ascii_alphabetic() || *b == b'_')
            {
                return Err(invalid("String.InvalidFormat", "invalid placeholder name"));
            }
            while bytes
                .get(i)
                .is_some_and(|b| b.is_ascii_alphanumeric() || *b == b'_')
            {
                c.step()?;
                i += 1;
            }
            let name = &template[start..i];
            let mut width = 0u64;
            let mut grouped = false;
            if bytes.get(i) == Some(&b':') {
                i += 1;
                if bytes.get(i) == Some(&b'>') {
                    i += 1;
                    let start = i;
                    while bytes.get(i).is_some_and(u8::is_ascii_digit) {
                        c.step()?;
                        width = width
                            .checked_mul(10)
                            .and_then(|n| n.checked_add((bytes[i] - b'0') as u64))
                            .ok_or_else(|| {
                                invalid("String.InvalidFormat", "width exceeds unsigned-64 range")
                            })?;
                        i += 1;
                    }
                    if i == start || i - start > 1 && bytes[start] == b'0' {
                        return Err(invalid("String.InvalidFormat", "invalid placeholder width"));
                    }
                } else if bytes.get(i) != Some(&b',') {
                    return Err(invalid(
                        "String.InvalidFormat",
                        "invalid placeholder format",
                    ));
                }
                if bytes.get(i) == Some(&b',') {
                    grouped = true;
                    i += 1;
                }
            }
            if bytes.get(i) != Some(&b'}') {
                return Err(invalid("String.InvalidFormat", "unclosed placeholder"));
            }
            i += 1;
            let value = fields
                .fields()
                .get(name)
                .ok_or_else(|| invalid("String.InvalidFormat", "placeholder field is absent"))?;
            (scalar(c, &mut scratch, value, grouped)?, width)
        } else if bytes[i] == b'}' {
            return Err(invalid("String.InvalidFormat", "unmatched closing brace"));
        } else {
            let start = i;
            while i < bytes.len() && !matches!(bytes[i], b'{' | b'}') {
                c.step()?;
                i += 1;
            }
            {
                scratch.grow((i - start) as u64)?;
                (template[start..i].to_owned(), 0)
            }
        };
        let padding = width.saturating_sub(piece.chars().count() as u64);
        let added = padding
            .checked_add(piece.len() as u64)
            .ok_or_else(|| limit("formatted output bytes"))?;
        let total = (output.len() as u64)
            .checked_add(added)
            .and_then(|n| n.checked_add(16))
            .ok_or_else(|| limit("formatted output bytes"))?;
        c.values.preflight(total)?;
        scratch.grow(
            added
                .checked_mul(2)
                .ok_or_else(|| limit("formatted output bytes"))?,
        )?;
        crate::json::work(c, added)?;
        let padding = usize::try_from(padding).map_err(|_| limit("formatted output bytes"))?;
        output.extend(std::iter::repeat(' ').take(padding));
        output.push_str(&piece);
    }
    Ok(output)
}
pub fn execute(c: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let result = (|| match site.operation.as_str() {
        "Int.parse" => Ok(int(integer(c, args[0].string(), "Int.InvalidText")?)),
        "Float.format_fixed" => {
            let places = args[1]
                .integer()
                .to_usize()
                .filter(|n| *n <= 1000)
                .ok_or_else(|| {
                    invalid(
                        "Numeric.InvalidPrecision",
                        "precision must be from 0 through 1000",
                    )
                })?;
            Ok(text(fixed(c, args[0].number(), places)?))
        }
        "String.format" => Ok(text(format(c, args[0].string(), &args[1])?)),
        _ => Err(host("unknown formatter")),
    })();
    result.map_err(|fault| match fault {
        Fault::Numeric(kind, message) => crate::training::recoverable(site, args, kind, &message),
        other => other,
    })
}
