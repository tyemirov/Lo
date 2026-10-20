use crate::{context::*, tensor, training, value::*};
use num_bigint::BigInt;

use std::collections::BTreeMap;

// Checkpoint JSON is a typed wire contract. Parse its grammar directly so lexical
// errors and zero values have the same meaning as the evaluator's decoder.
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
// Unicode simple-fold classes that intersect ASCII consist of ASCII case
// pairs plus long s/S and Kelvin sign/K. Record/map keys stay exact.
fn folded_field(name: &str) -> String {
    name.chars()
        .map(|c| match c {
            'ſ' => 'S',
            'K' => 'K',
            _ => c.to_ascii_uppercase(),
        })
        .collect()
}
impl Map {
    fn get(&self, key: &str) -> Option<&Json> {
        self.0.iter().rev().find(|(k, _)| k == key).map(|(_, v)| v)
    }
    fn field(&self, key: &str, skip_null: bool) -> Option<&Json> {
        let folded = folded_field(key);
        self.0
            .iter()
            .rev()
            .find(|(k, v)| folded_field(k) == folded && (!skip_null || !matches!(v, Json::Null)))
            .map(|(_, v)| v)
    }
    fn len(&self) -> usize {
        self.0
            .iter()
            .map(|(k, _)| k)
            .collect::<std::collections::BTreeSet<_>>()
            .len()
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
struct JsonParser<'a> {
    bytes: &'a [u8],
    offset: usize,
}
enum Frame {
    Array(Vec<Json>, u8),
    Object(Map, Option<String>, u8),
}
impl JsonParser<'_> {
    fn whitespace(&mut self) {
        while self
            .bytes
            .get(self.offset)
            .is_some_and(|b| matches!(b, b' ' | b'\n' | b'\r' | b'\t'))
        {
            self.offset += 1;
        }
    }
    fn character(&self, context: &str) -> Fault {
        let Some(&b) = self.bytes.get(self.offset) else {
            return invalid("unexpected EOF");
        };
        let quoted = match b {
            b'\n' => "'\\n'".into(),
            b'\r' => "'\\r'".into(),
            b'\t' => "'\\t'".into(),
            b'\'' => "'\\''".into(),
            b'\\' => "'\\\\'".into(),
            0..=31 | 127..=255 => format!("'\\x{b:02x}'"),
            _ => format!("'{}'", b as char),
        };
        invalid(format!("invalid character {quoted} {context}"))
    }
    fn hex(&mut self, escape_start: usize) -> Result<u16, Fault> {
        let mut n = 0;
        for _ in 0..4 {
            let Some(&b) = self.bytes.get(self.offset) else {
                return Err(invalid("unexpected EOF"));
            };
            let Some(v) = (b as char).to_digit(16) else {
                let end = (escape_start + 6).min(self.bytes.len());
                return Err(invalid(format!(
                    "invalid escape sequence `{}` in string",
                    String::from_utf8_lossy(&self.bytes[escape_start..end])
                )));
            };
            n = n * 16 + v as u16;
            self.offset += 1;
        }
        Ok(n)
    }
    fn string(&mut self) -> Result<String, Fault> {
        self.offset += 1;
        let mut out = String::new();
        loop {
            let Some(&b) = self.bytes.get(self.offset) else {
                return Err(invalid("unexpected EOF"));
            };
            if b == b'"' {
                self.offset += 1;
                return Ok(out);
            }
            if b < 32 {
                return Err(self.character("in string"));
            }
            if b == b'\\' {
                self.offset += 1;
                let Some(&e) = self.bytes.get(self.offset) else {
                    return Err(invalid("unexpected EOF"));
                };
                self.offset += 1;
                match e {
                    b'"' | b'\\' | b'/' => out.push(e as char),
                    b'b' => out.push('\u{8}'),
                    b'f' => out.push('\u{c}'),
                    b'n' => out.push('\n'),
                    b'r' => out.push('\r'),
                    b't' => out.push('\t'),
                    b'u' => {
                        let high = self.hex(self.offset - 2)?;
                        let mut n = high as u32;
                        if (0xd800..=0xdbff).contains(&high)
                            && self.bytes.get(self.offset..self.offset + 2) == Some(b"\\u")
                        {
                            let saved = self.offset;
                            self.offset += 2;
                            let low = self.hex(self.offset - 2)?;
                            if (0xdc00..=0xdfff).contains(&low) {
                                n = 0x10000 + ((high as u32 - 0xd800) << 10) + (low as u32 - 0xdc00)
                            } else {
                                self.offset = saved;
                            }
                        }
                        out.push(char::from_u32(n).unwrap_or('\u{fffd}'));
                    }
                    _ => {
                        return Err(invalid(format!(
                            "invalid escape sequence `{}` in string",
                            String::from_utf8_lossy(&self.bytes[self.offset - 2..self.offset])
                        )));
                    }
                }
            } else if b < 128 {
                out.push(b as char);
                self.offset += 1;
            } else {
                let width = match b {
                    0xc2..=0xdf => 2,
                    0xe0..=0xef => 3,
                    0xf0..=0xf4 => 4,
                    _ => 1,
                };
                let end = (self.offset + width).min(self.bytes.len());
                match std::str::from_utf8(&self.bytes[self.offset..end]) {
                    Ok(text) => {
                        out.push_str(text);
                        self.offset = end;
                    }
                    Err(_) => {
                        out.push('\u{fffd}');
                        self.offset += 1;
                    }
                }
            }
        }
    }
    fn scalar(&mut self) -> Result<Json, Fault> {
        let Some(&b) = self.bytes.get(self.offset) else {
            return Err(invalid("unexpected EOF"));
        };
        if b == b'"' {
            return Ok(Json::String(self.string()?));
        }
        for (first, literal) in [(b'n', "null"), (b't', "true"), (b'f', "false")] {
            if b == first {
                for wanted in literal.bytes() {
                    if self.bytes.get(self.offset) != Some(&wanted) {
                        return Err(self.character(&format!(
                            "in literal {literal} (expecting '{}')",
                            wanted as char
                        )));
                    }
                    self.offset += 1;
                }
                return Ok(if first == b'n' {
                    Json::Null
                } else {
                    Json::Bool
                });
            }
        }
        if b == b'-' || b.is_ascii_digit() {
            if b == b'-' {
                self.offset += 1;
                if !self.bytes.get(self.offset).is_some_and(u8::is_ascii_digit) {
                    return Err(self.character("in numeric literal"));
                }
            }
            if self.bytes[self.offset] == b'0' {
                self.offset += 1;
            } else {
                while self.bytes.get(self.offset).is_some_and(u8::is_ascii_digit) {
                    self.offset += 1;
                }
            }
            for (mark, context) in [(b'.', "in numeric literal"), (b'e', "in numeric literal")] {
                let next = self.bytes.get(self.offset).copied();
                if next == Some(mark) || (mark == b'e' && next == Some(b'E')) {
                    self.offset += 1;
                    if mark == b'e'
                        && self
                            .bytes
                            .get(self.offset)
                            .is_some_and(|b| matches!(b, b'+' | b'-'))
                    {
                        self.offset += 1;
                    }
                    if !self.bytes.get(self.offset).is_some_and(u8::is_ascii_digit) {
                        return Err(self.character(context));
                    }
                    while self.bytes.get(self.offset).is_some_and(u8::is_ascii_digit) {
                        self.offset += 1;
                    }
                }
            }
            return Ok(Json::Number);
        }
        Err(self.character("looking for beginning of value"))
    }
    fn document(&mut self) -> Result<(Json, bool), Fault> {
        self.whitespace();
        if self.offset == self.bytes.len() {
            return Err(invalid("EOF"));
        }
        let mut stack: Vec<Frame> = vec![];
        let mut value = None;
        loop {
            if let Some(v) = value.take() {
                match stack.last_mut() {
                    Some(Frame::Array(values, state)) => {
                        values.push(v);
                        *state = 1;
                    }
                    Some(Frame::Object(map, key, state)) => {
                        map.0.push((key.take().unwrap(), v));
                        *state = 3;
                    }
                    None => {
                        self.whitespace();
                        return Ok((v, self.offset != self.bytes.len()));
                    }
                }
            }
            self.whitespace();
            match stack.last_mut() {
                Some(Frame::Array(_, state)) if *state == 1 => match self.bytes.get(self.offset) {
                    Some(b']') => {
                        self.offset += 1;
                        let Some(Frame::Array(values, _)) = stack.pop() else {
                            unreachable!()
                        };
                        value = Some(Json::Array(values));
                        continue;
                    }
                    Some(b',') => {
                        self.offset += 1;
                        *state = 2;
                        self.whitespace();
                    }
                    _ => return Err(self.character("after array element")),
                },
                Some(Frame::Array(_, state))
                    if *state == 0 && self.bytes.get(self.offset) == Some(&b']') =>
                {
                    self.offset += 1;
                    let Some(Frame::Array(values, _)) = stack.pop() else {
                        unreachable!()
                    };
                    value = Some(Json::Array(values));
                    continue;
                }
                Some(Frame::Object(_, _, state)) if *state == 3 => {
                    match self.bytes.get(self.offset) {
                        Some(b'}') => {
                            self.offset += 1;
                            let Some(Frame::Object(map, _, _)) = stack.pop() else {
                                unreachable!()
                            };
                            value = Some(Json::Object(map));
                            continue;
                        }
                        Some(b',') => {
                            self.offset += 1;
                            *state = 4;
                            self.whitespace();
                        }
                        _ => return Err(self.character("after object key:value pair")),
                    }
                }
                _ => (),
            }
            if matches!(stack.last(),Some(Frame::Object(_,_,state)) if matches!(state,0|4)) {
                if matches!(stack.last(), Some(Frame::Object(_, _, 0)))
                    && self.bytes.get(self.offset) == Some(&b'}')
                {
                    self.offset += 1;
                    let Some(Frame::Object(map, _, _)) = stack.pop() else {
                        unreachable!()
                    };
                    value = Some(Json::Object(map));
                    continue;
                }
                if self.bytes.get(self.offset) != Some(&b'"') {
                    return Err(self.character("looking for beginning of object key string"));
                }
                let name = self.string()?;
                self.whitespace();
                if self.bytes.get(self.offset) != Some(&b':') {
                    return Err(self.character("after object key"));
                }
                self.offset += 1;
                self.whitespace();
                let Some(Frame::Object(_, key, state)) = stack.last_mut() else {
                    unreachable!()
                };
                *key = Some(name);
                *state = 2;
            }
            match self.bytes.get(self.offset) {
                Some(b'[') | Some(b'{') => {
                    if stack.len() >= 10000 {
                        return Err(self.character("exceeded max depth"));
                    }
                    let array = self.bytes[self.offset] == b'[';
                    self.offset += 1;
                    stack.push(if array {
                        Frame::Array(vec![], 0)
                    } else {
                        Frame::Object(Map::default(), None, 0)
                    });
                }
                _ => value = Some(self.scalar()?),
            }
        }
    }
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
        let Some((_, array)) = fields
            .iter()
            .find(|(key, _)| folded_field(key) == folded_field(name))
        else {
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
// strconv.ParseFloat accepts Go numeric literal separators and hexadecimal
// exponents before the codec's independent canonical-byte comparison.
fn float_digits(bytes: &[u8], index: &mut usize, hex: bool, prefix: bool) -> Option<usize> {
    let start = *index;
    let digit = |b: u8| {
        if hex {
            b.is_ascii_hexdigit()
        } else {
            b.is_ascii_digit()
        }
    };
    let mut count = 0;
    let mut previous_digit = false;
    while let Some(&b) = bytes.get(*index) {
        if digit(b) {
            count += 1;
            previous_digit = true;
            *index += 1;
        } else if b == b'_' {
            if !(previous_digit || prefix && *index == start)
                || !bytes.get(*index + 1).is_some_and(|b| digit(*b))
            {
                return None;
            }
            previous_digit = false;
            *index += 1;
        } else {
            break;
        }
    }
    Some(count)
}
fn float_literal(text: &str) -> Option<(String, bool)> {
    let bytes = text.as_bytes();
    let mut index = usize::from(bytes.first().is_some_and(|b| matches!(b, b'+' | b'-')));
    let hex = bytes
        .get(index..index + 2)
        .is_some_and(|p| p == b"0x" || p == b"0X");
    if hex {
        index += 2;
    }
    let mut digits = float_digits(bytes, &mut index, hex, hex)?;
    if bytes.get(index) == Some(&b'.') {
        index += 1;
        digits += float_digits(bytes, &mut index, hex, false)?;
    }
    if digits == 0 {
        return None;
    }
    let exponent = bytes.get(index).is_some_and(|b| {
        if hex {
            matches!(b, b'p' | b'P')
        } else {
            matches!(b, b'e' | b'E')
        }
    });
    if exponent {
        index += 1;
        if bytes.get(index).is_some_and(|b| matches!(b, b'+' | b'-')) {
            index += 1;
        }
        if float_digits(bytes, &mut index, false, false)? == 0 {
            return None;
        }
    } else if hex {
        return None;
    }
    if index != bytes.len() {
        return None;
    }
    Some((text.chars().filter(|c| *c != '_').collect(), hex))
}
fn hexadecimal_float(text: &str, bits: u32) -> Option<f64> {
    let bytes = text.as_bytes();
    let negative = bytes.first() == Some(&b'-');
    let sign_len = usize::from(bytes.first().is_some_and(|b| matches!(b, b'+' | b'-')));
    let exponent_at = bytes.iter().position(|b| matches!(b, b'p' | b'P'))?;
    let mantissa = &bytes[sign_len + 2..exponent_at];
    let exponent_text = &bytes[exponent_at + 1..];
    let exponent_negative = exponent_text.first() == Some(&b'-');
    let mut exponent = 0i64;
    // This bound exceeds every possible mantissa bit displacement admitted by
    // the runtime input limit. Continue validating/reading all exponent digits.
    for &b in exponent_text {
        if b.is_ascii_digit() {
            exponent = (exponent * 10 + (b - b'0') as i64).min(1i64 << 55);
        }
    }
    if exponent_negative {
        exponent = -exponent;
    }
    let integer_digits = mantissa
        .iter()
        .position(|b| *b == b'.')
        .unwrap_or(mantissa.len());
    let nibble = |b: u8| (b as char).to_digit(16).unwrap() as u64;
    let digits = || mantissa.iter().copied().filter(|b| *b != b'.');
    let first = digits().enumerate().find(|(_, b)| nibble(*b) != 0);
    let sign = if negative { 1u64 << (bits - 1) } else { 0 };
    let from_bits = |value: u64| {
        if bits == 32 {
            f32::from_bits(value as u32) as f64
        } else {
            f64::from_bits(value)
        }
    };
    let Some((first_index, first_digit)) = first else {
        return Some(from_bits(sign));
    };
    let precision = if bits == 32 { 24i64 } else { 53 };
    let minimum = if bits == 32 { -126i64 } else { -1022 };
    let maximum = if bits == 32 { 127i64 } else { 1023 };
    let high_bit = 63 - nibble(first_digit).leading_zeros() as i64;
    exponent += 4 * (integer_digits as i64 - first_index as i64 - 1) + high_bit;
    if exponent > maximum {
        return None;
    }
    if exponent < minimum - precision {
        return Some(from_bits(sign));
    }
    // Subnormals have fewer retained significant bits. Rounding directly at
    // the requested width prevents f64-to-f32 double rounding at halfway.
    let retained = precision - (minimum - exponent).max(0);
    let mut significand = 0u64;
    let mut captured = 0i64;
    let mut guard = None;
    let mut sticky = false;
    let mut begun = false;
    for b in digits() {
        for shift in (0..4).rev() {
            let bit = (nibble(b) >> shift) & 1;
            if !begun && bit == 0 {
                continue;
            }
            begun = true;
            if captured < retained {
                significand = (significand << 1) | bit;
                captured += 1;
            } else if guard.is_none() {
                guard = Some(bit != 0);
            } else {
                sticky |= bit != 0;
            }
        }
    }
    significand <<= retained - captured;
    if guard == Some(true) && (sticky || significand & 1 != 0) {
        significand += 1;
    }
    let value = if exponent >= minimum {
        if significand == 1u64 << precision {
            significand >>= 1;
            exponent += 1;
        }
        if exponent > maximum {
            return None;
        }
        let fraction_bits = precision - 1;
        let bias = if bits == 32 { 127 } else { 1023 };
        (((exponent + bias) as u64) << fraction_bits)
            | (significand & ((1u64 << fraction_bits) - 1))
    } else {
        significand
    };
    Some(from_bits(sign | value))
}
fn checkpoint_float(text: &str, bits: u32) -> Option<f64> {
    let (clean, hex) = float_literal(text)?;
    let number = if hex {
        hexadecimal_float(&clean, bits)?
    } else if bits == 32 {
        clean.parse::<f32>().ok()? as f64
    } else {
        clean.parse::<f64>().ok()?
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
    let scope = c.resources.scope_enter();
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
        Ok(value) => tensor::allocations(&[value.clone()]),
        Err(_) => vec![],
    };
    c.resources.scope_close(scope, &roots);
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
            let (json, trailing) = JsonParser {
                bytes: data,
                offset: 0,
            }
            .document()?;
            let output = decode_document(c, &json, &args[1], trailing)?;
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
    decode_document(c, encoded, template, false)
}
fn decode_document(c: &mut Context, encoded: &Json, template: &Handle, trailing: bool) -> Outcome {
    let empty = Map::default();
    let envelope = if matches!(encoded, Json::Null) {
        &empty
    } else {
        object(encoded, "envelope")?
    };
    for (name, value) in &envelope.0 {
        match folded_field(name).as_str() {
            "TYPE" if !matches!(value, Json::String(_) | Json::Null) => {
                return Err(typed_error(
                    value,
                    &format!("Go struct field .{name} of type string"),
                ))
            }
            "TYPE" | "VALUE" => (),
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
    if trailing {
        return Err(invalid("checkpoint contains trailing data"));
    }
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
                    for (index, number) in tensor::data(value).storage.values.iter().enumerate() {
                        if index % 1024 == 0 {
                            c.check_cancel()?;
                        }
                        if *number < 0.0 {
                            return Err(invalid("AdamW second moments must be nonnegative"));
                        }
                    }
                }
                for name in ["beta1_power", "beta2_power"] {
                    let number = tensor::data(&result.fields()[name]).storage.values[0];
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
                data.weight(),
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
