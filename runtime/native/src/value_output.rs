use crate::value::{Data, Handle, Value};
use serde::Serialize;
use std::{
    collections::btree_map,
    io::{self, Write},
};

pub fn write(value: &Value, output: &mut impl Write) -> io::Result<()> {
    enum Part<'a> {
        Value(&'a Value, bool),
        Text(&'static str),
        Fields(btree_map::Iter<'a, String, Handle>, bool, bool),
        Items(&'a [Handle], usize, bool),
        Map(&'a [(Handle, Handle)], usize, bool),
        RangeFlags(bool, bool),
        Quote(&'a str),
    }
    fn guard<'a>(pending: &mut Vec<Part<'a>>, predicate: &'a str, contexts: &'a [Handle]) {
        pending.push(Part::Text("}"));
        pending.push(Part::Items(contexts, 0, true));
        pending.push(Part::Text(",\"context\":["));
        pending.push(Part::Quote(predicate));
        pending.push(Part::Text(",\"guard\":{\"predicate\":"));
    }
    let mut pending = vec![Part::Value(value, false)];
    while let Some(part) = pending.pop() {
        match part {
            Part::Text(text) => output.write_all(text.as_bytes())?,
            Part::Quote(text) => quote(output, text)?,
            Part::RangeFlags(start, end) => {
                write!(output, ",\"include_start\":{start},\"include_end\":{end}}}")?
            }
            Part::Fields(mut fields, first, snapshot) => {
                if let Some((name, value)) = fields.next() {
                    if !first {
                        output.write_all(b",")?;
                    }
                    quote(output, name)?;
                    output.write_all(b":")?;
                    pending.push(Part::Fields(fields, false, snapshot));
                    pending.push(Part::Value(value, snapshot));
                } else {
                    output.write_all(b"}")?;
                }
            }
            Part::Items(items, index, snapshot) => {
                if let Some(value) = items.get(index) {
                    if index > 0 {
                        output.write_all(b",")?;
                    }
                    pending.push(Part::Items(items, index + 1, snapshot));
                    pending.push(Part::Value(value, snapshot));
                } else {
                    output.write_all(b"]")?;
                }
            }
            Part::Map(items, index, snapshot) => {
                if let Some((key, value)) = items.get(index) {
                    if index > 0 {
                        output.write_all(b",")?;
                    }
                    output.write_all(b"{\"key\":")?;
                    pending.push(Part::Map(items, index + 1, snapshot));
                    pending.push(Part::Text("}"));
                    pending.push(Part::Value(value, snapshot));
                    pending.push(Part::Text(",\"value\":"));
                    pending.push(Part::Value(key, false));
                } else {
                    output.write_all(b"]")?;
                }
            }
            Part::Value(value, snapshot) => {
                if snapshot && matches!(value.data, Data::Tensor(_)) {
                    // Tensor snapshots contain only a bounded shape and scalar metadata.
                    let tensor = crate::value::tensor_snapshot(value);
                    write(&tensor, output)?;
                    continue;
                }
                output.write_all(b"{\"type\":")?;
                let typ = if snapshot {
                    crate::value::snapshot_type(&value.typ)
                } else {
                    value.typ.clone()
                };
                quote(output, &typ.name())?;
                pending.push(Part::Text("}"));
                match &value.data {
                    Data::Success(value) => {
                        output.write_all(b",\"status\":\"ok\",\"value\":")?;
                        pending.push(Part::Value(value, snapshot));
                    }
                    Data::Failure(value) => {
                        output.write_all(b",\"status\":\"error\",\"error\":")?;
                        pending.push(Part::Value(value, false));
                    }
                    Data::Error(error) => {
                        output.write_all(b",\"kind\":")?;
                        quote(output, &error.kind)?;
                        output.write_all(b",\"message\":")?;
                        quote(output, &error.message)?;
                        output.write_all(b",\"input\":")?;
                        if !error.flow_progress.is_empty() {
                            pending.push(Part::Items(&error.flow_progress, 0, false));
                            pending.push(Part::Text(",\"flow_progress\":["));
                        }
                        pending.push(Part::Items(&error.trace, 0, false));
                        pending.push(Part::Text(",\"trace\":["));
                        pending.push(Part::Value(&error.input, false));
                    }
                    data => {
                        output.write_all(b",\"value\":")?;
                        match data {
                            Data::JSON(document) => {
                                document.write_node(document.nodes.len() - 1, output)?
                            }
                            Data::Record(fields) => {
                                output.write_all(b"{")?;
                                pending.push(Part::Fields(fields.iter(), true, snapshot));
                            }
                            Data::Wrapped(value) => {
                                output.write_all(b"{")?;
                                pending.push(Part::Fields(value.fields().iter(), true, snapshot));
                            }
                            Data::List(items) => {
                                output.write_all(b"[")?;
                                pending.push(Part::Items(items, 0, snapshot));
                            }
                            Data::Map(items) => {
                                output.write_all(b"[")?;
                                pending.push(Part::Map(items, 0, snapshot));
                            }
                            Data::Range(start, end, include_start, include_end) => {
                                output.write_all(b"{\"start\":")?;
                                pending.push(Part::RangeFlags(*include_start, *include_end));
                                pending.push(Part::Value(end, false));
                                pending.push(Part::Text(",\"end\":"));
                                pending.push(Part::Value(start, false));
                            }
                            Data::Text(text) | Data::Device(text) => quote(output, text)?,
                            Data::Int(number) => quote(output, &number.to_string())?,
                            Data::Float(number) => {
                                quote(output, &crate::value::format_float(*number))?
                            }
                            Data::Bool(value) => {
                                output.write_all(if *value { b"true" } else { b"false" })?
                            }
                            Data::Null => output.write_all(b"null")?,
                            Data::Glob(pattern) => quote(output, &pattern.pattern)?,
                            Data::Adapter(crate::adapters::AdapterValue::File(
                                crate::files::Value::Remove {
                                    entries,
                                    guard: check,
                                    ..
                                },
                            )) => {
                                output.write_all(
                                    b"{\"operation\":\"File.Remove.new\",\"entries\":",
                                )?;
                                pending.push(Part::Text("}"));
                                if let Some(check) = check {
                                    guard(&mut pending, &check.predicate, &check.contexts);
                                }
                                pending.push(Part::Value(entries, false));
                            }
                            Data::Adapter(crate::adapters::AdapterValue::File(
                                crate::files::Value::TreeRemove {
                                    expected,
                                    guard: check,
                                },
                            )) => {
                                output.write_all(
                                    b"{\"operation\":\"File.Tree.Remove.checked\",\"expected\":",
                                )?;
                                pending.push(Part::Text("}"));
                                guard(&mut pending, &check.predicate, &check.contexts);
                                pending.push(Part::Value(expected, false));
                            }
                            Data::Bytes(bytes) => {
                                output.write_all(b"\"")?;
                                let mut encoder = base64::write::EncoderWriter::new(
                                    &mut *output,
                                    &base64::engine::general_purpose::STANDARD,
                                );
                                encoder.write_all(bytes)?;
                                encoder.finish()?;
                                drop(encoder);
                                output.write_all(b"\"")?;
                            }
                            Data::Adapter(adapter) => {
                                let redacted;
                                let rendered = if snapshot {
                                    redacted = adapter.snapshot();
                                    &redacted
                                } else {
                                    adapter
                                };
                                output.write_all(rendered.canonical_json().as_bytes())?
                            }
                            Data::Tensor(_) => {
                                let json = value.json();
                                output.write_all(
                                    crate::value::canonical_json_value(&json["value"]).as_bytes(),
                                )?;
                            }
                            Data::Flow(_) => {
                                return Err(io::Error::other("flow has no value representation"))
                            }
                            Data::Success(_) | Data::Failure(_) | Data::Error(_) => unreachable!(),
                        }
                    }
                }
            }
        }
    }
    Ok(())
}

struct Canonical;
impl serde_json::ser::Formatter for Canonical {
    fn write_string_fragment<W: Write + ?Sized>(
        &mut self,
        writer: &mut W,
        fragment: &str,
    ) -> io::Result<()> {
        let mut start = 0;
        for (index, ch) in fragment.char_indices() {
            let escaped = match ch {
                '&' => Some("\\u0026"),
                '<' => Some("\\u003c"),
                '>' => Some("\\u003e"),
                '\u{2028}' => Some("\\u2028"),
                '\u{2029}' => Some("\\u2029"),
                _ => None,
            };
            if let Some(escaped) = escaped {
                writer.write_all(fragment[start..index].as_bytes())?;
                writer.write_all(escaped.as_bytes())?;
                start = index + ch.len_utf8();
            }
        }
        writer.write_all(fragment[start..].as_bytes())
    }
}
pub fn quote(output: &mut impl Write, text: &str) -> io::Result<()> {
    text.serialize(&mut serde_json::Serializer::with_formatter(
        output, Canonical,
    ))
    .map_err(io::Error::other)
}
