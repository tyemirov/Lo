use crate::{context::*, value::*};
use num_bigint::BigInt;
use num_traits::{Signed, ToPrimitive};

pub const SUPPORTED: &[&str] = &[
    "String.starts_with",
    "String.ends_with",
    "String.slice",
    "String.compare",
    "String.split",
    "String.split_first",
    "String.replace_all",
    "String.trim",
    "String.is_whitespace",
    "String.split_whitespace",
    "String.lines",
    "Bytes.length",
    "Bytes.starts_with",
    "Bytes.slice",
    "Bytes.split",
    "Bytes.trim_start",
    "List.at",
    "List.slice",
    "List.sort",
    "File.Path.to_string",
    "File.Path.join",
    "File.Path.parent",
    "File.Path.name",
    "Glob.new",
    "Glob.matches",
    "Process.Arguments.format_shell",
];

pub fn parameter_names(op: &str) -> Vec<&'static str> {
    match op {
        "String.starts_with" => vec!["text", "prefix"],
        "String.ends_with" => vec!["text", "suffix"],
        "String.slice" => vec!["text", "start", "end"],
        "String.compare" => vec!["left", "right"],
        "String.split" | "String.split_first" => vec!["text", "separator"],
        "String.replace_all" => vec!["text", "old", "replacement"],
        "String.trim" | "String.is_whitespace" | "String.split_whitespace" | "String.lines" => {
            vec!["text"]
        }
        "Bytes.length" => vec!["bytes"],
        "Bytes.starts_with" | "Bytes.trim_start" => vec!["bytes", "prefix"],
        "Bytes.slice" => vec!["bytes", "start", "end"],
        "Bytes.split" => vec!["bytes", "separator"],
        "List.at" => vec!["values", "index"],
        "List.slice" => vec!["values", "start", "end"],
        "File.Path.join" => vec!["path", "relative"],
        "File.Path.to_string" | "File.Path.parent" | "File.Path.name" => vec!["path"],
        "Glob.new" => vec!["pattern"],
        "Glob.matches" => vec!["pattern", "text"],
        "Process.Arguments.format_shell" => vec!["arguments"],
        _ => vec![],
    }
}

pub fn admit(site: &Site) -> Result<(), String> {
    let invalid = || format!("invalid native processor contract {}", site.operation);
    let op = site.operation.as_str();
    if op == "List.sort" {
        return if site.arguments.len() >= 2 {
            Ok(())
        } else {
            Err(invalid())
        };
    }
    let string = atom("String");
    let bytes = atom("Bytes");
    let integer = atom("Int");
    let path = atom("File.Path");
    let (inputs, output, fallible) = match op {
        "String.starts_with" | "String.ends_with" => {
            (vec![string.clone(), string.clone()], atom("Bool"), false)
        }
        "String.slice" => (
            vec![string.clone(), integer.clone(), integer.clone()],
            string.clone(),
            true,
        ),
        "String.compare" => (vec![string.clone(), string.clone()], integer.clone(), false),
        "String.split" => (
            vec![string.clone(), string.clone()],
            Type::List(Box::new(string.clone())),
            true,
        ),
        "String.split_first" => (
            vec![string.clone(), string.clone()],
            Type::Record(vec![
                ("after".into(), string.clone()),
                ("before".into(), string.clone()),
            ]),
            true,
        ),
        "String.replace_all" => (
            vec![string.clone(), string.clone(), string.clone()],
            string.clone(),
            true,
        ),
        "String.trim" => (vec![string.clone()], string.clone(), false),
        "String.is_whitespace" => (vec![string.clone()], atom("Bool"), false),
        "String.split_whitespace" | "String.lines" => (
            vec![string.clone()],
            Type::List(Box::new(string.clone())),
            false,
        ),
        "Bytes.length" => (vec![bytes.clone()], integer.clone(), false),
        "Bytes.starts_with" => (vec![bytes.clone(), bytes.clone()], atom("Bool"), false),
        "Bytes.slice" => (
            vec![bytes.clone(), integer.clone(), integer.clone()],
            bytes.clone(),
            true,
        ),
        "Bytes.split" => (
            vec![bytes.clone(), bytes.clone()],
            Type::List(Box::new(bytes.clone())),
            true,
        ),
        "Bytes.trim_start" => (vec![bytes.clone(), bytes.clone()], bytes.clone(), true),
        "List.at" | "List.slice" => {
            let Some(Argument::Value(Type::List(element))) = site.arguments.first() else {
                return Err(invalid());
            };
            let list = Type::List(element.clone());
            if op == "List.at" {
                (vec![list, integer.clone()], *element.clone(), true)
            } else {
                (
                    vec![list.clone(), integer.clone(), integer.clone()],
                    list,
                    true,
                )
            }
        }
        "File.Path.to_string" | "File.Path.name" => (vec![path.clone()], string.clone(), false),
        "File.Path.parent" => (vec![path.clone()], path.clone(), false),
        "File.Path.join" => (vec![path.clone(), string.clone()], path.clone(), true),
        "Glob.new" => (vec![string.clone()], atom("Glob"), true),
        "Glob.matches" => (vec![atom("Glob"), string.clone()], atom("Bool"), false),
        "Process.Arguments.format_shell" => {
            (vec![Type::List(Box::new(string))], atom("String"), true)
        }
        _ => return Err(invalid()),
    };
    let expected = if fallible && !matches!(output, Type::Fallible(_)) {
        Type::Fallible(Box::new(output))
    } else {
        output
    };
    if site.typ != expected
        || inputs.len() != site.arguments.len()
        || inputs.iter().zip(&site.arguments).any(
            |(expected, actual)| !matches!(actual, Argument::Value(actual) if expected == actual),
        )
    {
        return Err(invalid());
    }
    Ok(())
}

pub(crate) fn fail(site: &Site, args: &[Handle], kind: &str, message: &str) -> Outcome {
    let value = operation_failure(site, args, kind, message);
    let Data::Failure(cause) = &value.data else {
        std::process::abort()
    };
    Err(Fault::Recoverable(cause.clone()))
}

pub(crate) struct Scratch {
    context: *mut Context,
    bytes: u64,
}
impl Scratch {
    pub(crate) fn new(context: *mut Context, bytes: u64) -> Result<Self, Fault> {
        unsafe {
            (*context).values.reserve_temporary(bytes)?;
        }
        Ok(Self { context, bytes })
    }
}
impl Scratch {
    pub(crate) fn grow(&mut self, bytes: u64) -> Result<(), Fault> {
        let total = self.bytes.checked_add(bytes).ok_or_else(|| limit("temporary bytes"))?;
        unsafe { (*self.context).values.reserve_temporary(bytes)?; }
        self.bytes = total;
        Ok(())
    }
}
impl Drop for Scratch {
    fn drop(&mut self) {
        unsafe {
            (*self.context).values.release_temporary(self.bytes);
        }
    }
}

fn data_bytes(value: &Handle) -> &[u8] {
    let Data::Bytes(bytes) = &value.data else {
        std::process::abort()
    };
    bytes
}
fn bounded_text(context: &mut Context, value: &str, typ: Type) -> Outcome {
    context.values.preflight(value.len() as u64 + 16)?;
    for _ in value.bytes() {
        context.step()?;
    }
    Ok(new(typ, Data::Text(value.to_owned())))
}
fn bounded_bytes(context: &mut Context, value: &[u8]) -> Outcome {
    context.values.preflight(value.len() as u64 + 16)?;
    for _ in value {
        context.step()?;
    }
    Ok(new(atom("Bytes"), Data::Bytes(value.to_vec())))
}
fn white(c: char) -> bool {
    matches!(c, '\u{0009}'..='\u{000d}' | '\u{0020}' | '\u{0085}' | '\u{00a0}' | '\u{1680}' | '\u{2000}'..='\u{200a}' | '\u{2028}' | '\u{2029}' | '\u{202f}' | '\u{205f}' | '\u{3000}')
}
fn starts(context: &mut Context, text: &[u8], prefix: &[u8]) -> Result<bool, Fault> {
    if prefix.len() > text.len() {
        return Ok(false);
    }
    for (a, b) in text.iter().zip(prefix) {
        context.step()?;
        if a != b {
            return Ok(false);
        }
    }
    Ok(true)
}
fn find(
    context: &mut Context,
    text: &[u8],
    separator: &[u8],
    start: usize,
) -> Result<Option<usize>, Fault> {
    if separator.len() > text.len().saturating_sub(start) {
        return Ok(None);
    }
    for index in start..=text.len() - separator.len() {
        context.step()?;
        if starts(context, &text[index..], separator)? {
            return Ok(Some(index));
        }
    }
    Ok(None)
}
fn index(value: &BigInt, length: usize) -> Option<usize> {
    if value.is_negative() {
        (value + BigInt::from(length)).to_usize()
    } else {
        value.to_usize()
    }
}
fn bounds(args: &[Handle], length: usize) -> Option<(usize, usize)> {
    let start = index(args[1].integer(), length)?;
    let end = index(args[2].integer(), length)?;
    (start <= end && end <= length).then_some((start, end))
}
struct Pieces {
    values: Vec<Handle>,
    weight: u64,
    bytes: bool,
}
impl Pieces {
    fn new(bytes: bool) -> Self {
        Self {
            values: vec![],
            weight: 16,
            bytes,
        }
    }
    fn push(&mut self, context: &mut Context, bytes: &[u8]) -> Result<(), Fault> {
        context.step()?;
        if crate::value_resources::exceeds(
            self.values.len() as u64,
            1,
            context.values.limits.list_items,
        ) {
            return Err(limit("list elements"));
        }
        self.weight = self
            .weight
            .checked_add(bytes.len() as u64 + 16)
            .ok_or_else(|| limit("expanded value bytes"))?;
        context.values.preflight(self.weight)?;
        for _ in bytes {
            context.step()?;
        }
        self.values.push(if self.bytes {
            new(atom("Bytes"), Data::Bytes(bytes.to_vec()))
        } else {
            text(std::str::from_utf8(bytes).unwrap())
        });
        Ok(())
    }
    fn finish(self) -> Handle {
        new(
            Type::List(Box::new(atom(if self.bytes { "Bytes" } else { "String" }))),
            Data::List(self.values),
        )
    }
}

pub fn execute(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let op = site.operation.as_str();
    match op {
        "String.starts_with" | "String.ends_with" | "Bytes.starts_with" => {
            let (value, prefix) = if op.starts_with("String") {
                (args[0].string().as_bytes(), args[1].string().as_bytes())
            } else {
                (data_bytes(&args[0]), data_bytes(&args[1]))
            };
            let value = if op == "String.ends_with" {
                &value[value.len().saturating_sub(prefix.len())..]
            } else {
                value
            };
            Ok(boolean(starts(context, value, prefix)?))
        }
        "String.compare" => {
            let mut left = args[0].string().chars();
            let mut right = args[1].string().chars();
            loop {
                context.step()?;
                let order = left.next().cmp(&right.next());
                if !order.is_eq() {
                    return Ok(int(if order.is_lt() { -1 } else { 1 }));
                }
                if left.as_str().is_empty() && right.as_str().is_empty() {
                    return Ok(int(0));
                }
            }
        }
        "String.slice" => {
            let value = args[0].string();
            let mut length = 0;
            for _ in value.chars() {
                context.step()?;
                length += 1;
            }
            let Some((start, end)) = bounds(args, length) else {
                return fail(
                    site,
                    args,
                    "String.InvalidBounds",
                    "text slice bounds are invalid",
                );
            };
            let mut start_byte = value.len();
            let mut end_byte = value.len();
            for (index, (offset, _)) in value.char_indices().enumerate() {
                context.step()?;
                if index == start {
                    start_byte = offset;
                }
                if index == end {
                    end_byte = offset;
                    break;
                }
            }
            bounded_text(context, &value[start_byte..end_byte], atom("String"))
        }
        "Bytes.length" => Ok(int(data_bytes(&args[0]).len())),
        "Bytes.slice" => {
            let value = data_bytes(&args[0]);
            let Some((start, end)) = bounds(args, value.len()) else {
                return fail(
                    site,
                    args,
                    "Bytes.InvalidBounds",
                    "byte slice bounds are invalid",
                );
            };
            for _ in start..end {
                context.step()?;
            }
            bounded_bytes(context, &value[start..end])
        }
        "List.at" => {
            let Some(index) = index(args[1].integer(), args[0].items().len())
                .filter(|index| *index < args[0].items().len())
            else {
                return fail(site, args, "List.InvalidIndex", "list index is invalid");
            };
            let value = &args[0].items()[index];
            match &value.data {
                Data::Success(value) => Ok(value.clone()),
                Data::Failure(error) => Err(Fault::Recoverable(error.clone())),
                _ => Ok(value.clone()),
            }
        }
        "List.slice" => {
            let values = args[0].items();
            let Some((start, end)) = bounds(args, values.len()) else {
                return fail(
                    site,
                    args,
                    "List.InvalidBounds",
                    "list slice bounds are invalid",
                );
            };
            let mut weight = 16u64;
            for value in &values[start..end] {
                context.step()?;
                weight = weight.saturating_add(value.weight);
            }
            context.values.preflight(weight)?;
            Ok(new(
                site.typ.base(),
                Data::List(values[start..end].to_vec()),
            ))
        }
        "String.split" | "String.split_first" | "Bytes.split" => {
            let bytes = op == "Bytes.split";
            let (value, separator) = if bytes {
                (data_bytes(&args[0]), data_bytes(&args[1]))
            } else {
                (args[0].string().as_bytes(), args[1].string().as_bytes())
            };
            if separator.is_empty() {
                return fail(
                    site,
                    args,
                    if bytes {
                        "Bytes.InvalidSeparator"
                    } else {
                        "String.InvalidSeparator"
                    },
                    "separator must not be empty",
                );
            }
            if op == "String.split_first" {
                let Some(index) = find(context, value, separator, 0)? else {
                    return fail(site, args, "String.SeparatorAbsent", "separator is absent");
                };
                context
                    .values
                    .preflight((value.len() - separator.len()) as u64 + 59)?;
                for _ in 0..value.len() - separator.len() {
                    context.step()?;
                }
                return Ok(record([
                    (
                        "before".into(),
                        text(std::str::from_utf8(&value[..index]).unwrap()),
                    ),
                    (
                        "after".into(),
                        text(std::str::from_utf8(&value[index + separator.len()..]).unwrap()),
                    ),
                ]));
            }
            let mut pieces = Pieces::new(bytes);
            let mut start = 0;
            while let Some(index) = find(context, value, separator, start)? {
                pieces.push(context, &value[start..index])?;
                start = index + separator.len();
            }
            pieces.push(context, &value[start..])?;
            Ok(pieces.finish())
        }
        "Bytes.trim_start" => {
            let value = data_bytes(&args[0]);
            let prefix = data_bytes(&args[1]);
            if prefix.is_empty() {
                return fail(
                    site,
                    args,
                    "Bytes.InvalidSeparator",
                    "prefix must not be empty",
                );
            }
            let mut start = 0;
            while starts(context, &value[start..], prefix)? {
                start += prefix.len();
            }
            bounded_bytes(context, &value[start..])
        }
        "String.replace_all" => {
            let value = args[0].string();
            let old = args[1].string();
            let replacement = args[2].string();
            if old.is_empty() {
                return fail(
                    site,
                    args,
                    "String.InvalidSeparator",
                    "old text must not be empty",
                );
            }
            let mut output = String::new();
            let mut start = 0;
            while let Some(index) = find(context, value.as_bytes(), old.as_bytes(), start)? {
                let extra = (index - start)
                    .checked_add(replacement.len())
                    .ok_or_else(|| limit("expanded value bytes"))?;
                context.values.preflight(
                    (output.len() as u64)
                        .saturating_add(extra as u64)
                        .saturating_add(16),
                )?;
                for _ in value[start..index].bytes().chain(replacement.bytes()) {
                    context.step()?;
                }
                output.push_str(&value[start..index]);
                output.push_str(replacement);
                start = index + old.len();
            }
            context.values.preflight(
                (output.len() as u64)
                    .saturating_add((value.len() - start) as u64)
                    .saturating_add(16),
            )?;
            for _ in value[start..].bytes() {
                context.step()?;
            }
            output.push_str(&value[start..]);
            Ok(text(output))
        }
        "String.trim" | "String.is_whitespace" => {
            let value = args[0].string();
            let mut first = value.len();
            let mut last = 0;
            for (offset, c) in value.char_indices() {
                context.step()?;
                if !white(c) {
                    if op == "String.is_whitespace" {
                        return Ok(boolean(false));
                    }
                    first = first.min(offset);
                    last = offset + c.len_utf8();
                }
            }
            if op == "String.is_whitespace" {
                Ok(boolean(!value.is_empty()))
            } else {
                bounded_text(
                    context,
                    if first <= last {
                        &value[first..last]
                    } else {
                        ""
                    },
                    atom("String"),
                )
            }
        }
        "String.split_whitespace" => {
            let value = args[0].string();
            let mut pieces = Pieces::new(false);
            let mut start = None;
            for (offset, c) in value.char_indices() {
                context.step()?;
                if white(c) {
                    if let Some(index) = start.take() {
                        pieces.push(context, &value.as_bytes()[index..offset])?;
                    }
                } else if start.is_none() {
                    start = Some(offset);
                }
            }
            if let Some(index) = start {
                pieces.push(context, &value.as_bytes()[index..])?;
            }
            Ok(pieces.finish())
        }
        "String.lines" => {
            let value = args[0].string().as_bytes();
            let mut pieces = Pieces::new(false);
            let mut start = 0;
            let mut index = 0;
            while index < value.len() {
                context.step()?;
                if matches!(value[index], b'\n' | b'\r') {
                    pieces.push(context, &value[start..index])?;
                    if value[index] == b'\r' && value.get(index + 1) == Some(&b'\n') {
                        index += 1;
                    }
                    start = index + 1;
                }
                index += 1;
            }
            if start < value.len() {
                pieces.push(context, &value[start..])?;
            }
            Ok(pieces.finish())
        }
        "File.Path.to_string" => bounded_text(context, args[0].string(), atom("String")),
        "File.Path.parent" | "File.Path.name" => {
            let path = args[0].string();
            for _ in path.bytes() {
                context.step()?;
            }
            let trimmed = path.trim_end_matches('/');
            let value = if op == "File.Path.name" {
                trimmed.rsplit('/').next().unwrap_or("")
            } else if trimmed.is_empty() {
                if path.starts_with('/') {
                    "/"
                } else {
                    ""
                }
            } else if let Some(index) = trimmed.rfind('/') {
                let parent = trimmed[..index].trim_end_matches('/');
                if parent.is_empty() {
                    "/"
                } else {
                    parent
                }
            } else {
                ""
            };
            bounded_text(
                context,
                value,
                if op == "File.Path.parent" {
                    atom("File.Path")
                } else {
                    atom("String")
                },
            )
        }
        "File.Path.join" => {
            let path = args[0].string();
            let relative = args[1].string();
            for byte in path.bytes().chain(relative.bytes()) {
                context.step()?;
                if byte == 0 {
                    return fail(site, args, "File.InvalidPath", "path contains NUL");
                }
            }
            if relative.starts_with('/') {
                return fail(
                    site,
                    args,
                    "File.InvalidPath",
                    "joined path must be relative",
                );
            }
            let separator = if path.is_empty() || path.ends_with('/') || relative.is_empty() {
                ""
            } else {
                "/"
            };
            context.values.preflight(
                (path.len() as u64)
                    .saturating_add(relative.len() as u64)
                    .saturating_add(separator.len() as u64)
                    .saturating_add(16),
            )?;
            Ok(new(
                atom("File.Path"),
                Data::Text(format!("{path}{separator}{relative}")),
            ))
        }
        "Glob.new" => crate::glob::compile(context, site, args),
        "Glob.matches" => crate::glob::matches(context, &args[0], args[1].string()),
        "Process.Arguments.format_shell" => {
            let mut output = String::new();
            for argument in args[0].items() {
                context.step()?;
                let mut length = 2u64;
                for byte in argument.string().bytes() {
                    context.step()?;
                    if byte == 0 {
                        return fail(
                            site,
                            args,
                            "Process.InvalidArguments",
                            "argument contains NUL",
                        );
                    }
                    length = length.saturating_add(if byte == b'\'' { 4 } else { 1 });
                }
                context.values.preflight(
                    (output.len() as u64)
                        .saturating_add(length)
                        .saturating_add(if output.is_empty() { 16 } else { 17 }),
                )?;
                if !output.is_empty() {
                    output.push(' ');
                }
                output.push('\'');
                for c in argument.string().chars() {
                    context.step()?;
                    if c == '\'' {
                        output.push_str("'\\''");
                    } else {
                        output.push(c);
                    }
                }
                output.push('\'');
            }
            Ok(text(output))
        }
        _ => Err(host("invalid pure processor operation")),
    }
}
