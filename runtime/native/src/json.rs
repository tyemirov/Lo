use crate::{
    context::{limit, Context, Fault},
    processors::Scratch,
};
use std::io::{self, Write};

#[derive(Clone, Debug)]
pub enum Kind {
    Null,
    Bool(bool),
    Number(String),
    String(String),
    Array(Vec<usize>),
    Object(Vec<(String, usize)>),
}
#[derive(Clone, Debug)]
pub struct Node {
    pub kind: Kind,
    pub start: usize,
    pub weight: u64,
}
#[derive(Clone, Debug)]
pub struct Document {
    pub nodes: Vec<Node>,
    pub max_items: usize,
}
#[derive(Clone, Copy)]
pub struct Ref<'a> {
    pub document: &'a Document,
    pub index: usize,
}
impl Document {
    pub fn root(&self) -> Ref<'_> {
        Ref {
            document: self,
            index: self.nodes.len() - 1,
        }
    }
    pub fn weight(&self) -> u64 {
        self.nodes.last().unwrap().weight
    }
    pub fn extract(&self, c: &mut Context, index: usize) -> Result<Self, Fault> {
        let first = self.nodes[index].start;
        c.values.preflight(self.nodes[index].weight)?;
        let _scratch = Scratch::new(
            c,
            self.nodes[index]
                .weight
                .checked_mul(4)
                .and_then(|n| n.checked_add(((index - first + 1) as u64).checked_mul(128)?))
                .ok_or_else(|| limit("JSON extraction bytes"))?,
        )?;
        let mut nodes = Vec::with_capacity(index - first + 1);
        let mut max_items = 0;
        for node in &self.nodes[first..=index] {
            work(c, 1)?;
            let kind = match &node.kind {
                Kind::Array(items) => {
                    work(c, items.len() as u64)?;
                    max_items = max_items.max(items.len());
                    Kind::Array(items.iter().map(|i| i - first).collect())
                }
                Kind::Object(items) => {
                    max_items = max_items.max(items.len());
                    let mut output = Vec::with_capacity(items.len());
                    for (key, i) in items {
                        work(c, key.len() as u64 + 1)?;
                        output.push((key.clone(), i - first));
                    }
                    Kind::Object(output)
                }
                Kind::String(s) | Kind::Number(s) => {
                    work(c, s.len() as u64)?;
                    node.kind.clone()
                }
                _ => node.kind.clone(),
            };
            nodes.push(Node {
                kind,
                start: node.start - first,
                weight: node.weight,
            });
        }
        Ok(Self { nodes, max_items })
    }
    pub fn write_node(&self, index: usize, output: &mut impl Write) -> io::Result<()> {
        struct Frame {
            index: usize,
            position: usize,
            opened: bool,
        }
        let mut pending = vec![Frame {
            index,
            position: 0,
            opened: false,
        }];
        while let Some(frame) = pending.last_mut() {
            let node = &self.nodes[frame.index];
            if !frame.opened {
                output.write_all(b"{\"kind\":")?;
                crate::value_output::quote(
                    output,
                    Ref {
                        document: self,
                        index: frame.index,
                    }
                    .kind(),
                )?;
                output.write_all(b",\"value\":")?;
                frame.opened = true;
                match &node.kind {
                    Kind::Null => output.write_all(b"null")?,
                    Kind::Bool(b) => output.write_all(if *b { b"true" } else { b"false" })?,
                    Kind::String(s) | Kind::Number(s) => crate::value_output::quote(output, s)?,
                    Kind::Array(_) => output.write_all(b"[")?,
                    Kind::Object(_) => output.write_all(b"{")?,
                }
            }
            let child = match &node.kind {
                Kind::Array(items) => {
                    if let Some(child) = items.get(frame.position) {
                        if frame.position > 0 {
                            output.write_all(b",")?;
                        }
                        Some(*child)
                    } else {
                        output.write_all(b"]}")?;
                        None
                    }
                }
                Kind::Object(items) => {
                    if let Some((key, child)) = items.get(frame.position) {
                        if frame.position > 0 {
                            output.write_all(b",")?;
                        }
                        crate::value_output::quote(output, key)?;
                        output.write_all(b":")?;
                        Some(*child)
                    } else {
                        output.write_all(b"}}")?;
                        None
                    }
                }
                _ => {
                    output.write_all(b"}")?;
                    None
                }
            };
            if let Some(index) = child {
                frame.position += 1;
                pending.push(Frame {
                    index,
                    position: 0,
                    opened: false,
                });
            } else {
                pending.pop();
            }
        }
        Ok(())
    }
}
impl<'a> Ref<'a> {
    pub fn kind(self) -> &'static str {
        match self.document.nodes[self.index].kind {
            Kind::Null => "null",
            Kind::Bool(_) => "bool",
            Kind::Number(_) => "number",
            Kind::String(_) => "string",
            Kind::Array(_) => "array",
            Kind::Object(_) => "object",
        }
    }
    pub fn as_str(self) -> Option<&'a str> {
        if let Kind::String(s) = &self.document.nodes[self.index].kind {
            Some(s)
        } else {
            None
        }
    }
    pub fn object(self) -> Option<&'a [(String, usize)]> {
        if let Kind::Object(v) = &self.document.nodes[self.index].kind {
            Some(v)
        } else {
            None
        }
    }
    pub fn array(self) -> Option<&'a [usize]> {
        if let Kind::Array(v) = &self.document.nodes[self.index].kind {
            Some(v)
        } else {
            None
        }
    }
    pub fn at(self, index: usize) -> Self {
        Self {
            document: self.document,
            index,
        }
    }
    pub fn get(self, key: &str) -> Option<Self> {
        let items = self.object()?;
        items
            .binary_search_by(|(name, _)| name.as_str().cmp(key))
            .ok()
            .map(|index| self.at(items[index].1))
    }
}
pub fn work(c: &mut Context, count: u64) -> Result<(), Fault> {
    for _ in 0..count {
        c.step()?;
    }
    Ok(())
}
fn invalid(message: impl Into<String>) -> Fault {
    Fault::Numeric("JSON.InvalidData", message.into())
}
struct Frame {
    start: usize,
    array: bool,
    items: Vec<usize>,
    fields: Vec<(String, usize)>,
    key: Option<String>,
    state: u8,
    weight: u64,
}
struct Parser<'a> {
    c: &'a mut Context,
    bytes: &'a [u8],
    offset: usize,
    scratch: Scratch,
    nodes: Vec<Node>,
    weight: u64,
    max_items: usize,
}
impl Parser<'_> {
    fn advance(&mut self) -> Result<u8, Fault> {
        let b = *self
            .bytes
            .get(self.offset)
            .ok_or_else(|| invalid("unexpected end of JSON input"))?;
        self.c.step()?;
        self.offset += 1;
        Ok(b)
    }
    fn whitespace(&mut self) -> Result<(), Fault> {
        while self
            .bytes
            .get(self.offset)
            .is_some_and(|b| matches!(b, b' ' | b'\n' | b'\r' | b'\t'))
        {
            self.advance()?;
        }
        Ok(())
    }
    fn string(&mut self) -> Result<String, Fault> {
        let start = self.offset;
        if self.advance()? != b'"' {
            return Err(invalid("JSON object key must be a string"));
        }
        loop {
            match self.advance()? {
                b'"' => break,
                b'\\' => {
                    self.advance()?;
                }
                0..=31 => return Err(invalid("raw control character in JSON string")),
                _ => (),
            }
        }
        let raw = &self.bytes[start + 1..self.offset - 1];
        let mut length = 0usize;
        decoded_string(self.c, raw, |ch| length += ch.len_utf8())?;
        let weight = self
            .weight
            .checked_add(length as u64)
            .and_then(|n| n.checked_add(16))
            .ok_or_else(|| limit("JSON string bytes"))?;
        self.c.values.preflight(weight)?;
        self.scratch.grow(length as u64)?;
        let mut output = String::with_capacity(length);
        decoded_string(self.c, raw, |ch| output.push(ch))?;
        Ok(output)
    }

    fn add_weight(&mut self, amount: u64) -> Result<(), Fault> {
        self.weight = self
            .weight
            .checked_add(amount)
            .ok_or_else(|| limit("JSON value bytes"))?;
        self.c.values.preflight(self.weight)
    }
    fn push(&mut self, kind: Kind, start: usize, weight: u64) -> Result<usize, Fault> {
        self.c.step()?;
        self.scratch.grow(2 * std::mem::size_of::<Node>() as u64)?;
        let index = self.nodes.len();
        self.nodes.push(Node {
            kind,
            start,
            weight,
        });
        Ok(index)
    }
    fn scalar(&mut self) -> Result<usize, Fault> {
        let start = self.offset;
        let b = *self
            .bytes
            .get(start)
            .ok_or_else(|| invalid("unexpected end of JSON input"))?;
        let kind = match b {
            b'"' => Kind::String(self.string()?),
            b'n' | b't' | b'f' => {
                let literal: &[u8] = match b {
                    b'n' => b"null",
                    b't' => b"true",
                    _ => b"false",
                };
                for expected in literal {
                    if self.advance()? != *expected {
                        return Err(invalid("invalid JSON literal"));
                    }
                }
                if b == b'n' {
                    Kind::Null
                } else {
                    Kind::Bool(b == b't')
                }
            }
            b'-' | b'0'..=b'9' => {
                if b == b'-' {
                    self.advance()?;
                }
                match self.bytes.get(self.offset) {
                    Some(b'0') => {
                        self.advance()?;
                    }
                    Some(b'1'..=b'9') => {
                        while self.bytes.get(self.offset).is_some_and(u8::is_ascii_digit) {
                            self.advance()?;
                        }
                    }
                    _ => return Err(invalid("invalid JSON number")),
                }
                for mark in [b'.', b'e'] {
                    if self
                        .bytes
                        .get(self.offset)
                        .is_some_and(|b| *b == mark || mark == b'e' && *b == b'E')
                    {
                        self.advance()?;
                        if mark == b'e'
                            && self
                                .bytes
                                .get(self.offset)
                                .is_some_and(|b| matches!(b, b'+' | b'-'))
                        {
                            self.advance()?;
                        }
                        if !self.bytes.get(self.offset).is_some_and(u8::is_ascii_digit) {
                            return Err(invalid("invalid JSON number"));
                        }
                        while self.bytes.get(self.offset).is_some_and(u8::is_ascii_digit) {
                            self.advance()?;
                        }
                    }
                }
                self.scratch.grow((self.offset - start) as u64)?;
                Kind::Number(
                    std::str::from_utf8(&self.bytes[start..self.offset])
                        .unwrap()
                        .to_owned(),
                )
            }
            _ => return Err(invalid("invalid JSON value")),
        };
        let weight = 16
            + match &kind {
                Kind::String(s) | Kind::Number(s) => s.len() as u64,
                _ => 0,
            };
        self.add_weight(weight)?;
        self.push(kind, self.nodes.len(), weight)
    }
    fn document(&mut self) -> Result<(), Fault> {
        let mut stack: Vec<Frame> = vec![];
        let mut value: Option<usize> = None;
        loop {
            if let Some(index) = value.take() {
                if let Some(frame) = stack.last_mut() {
                    let count = if frame.array {
                        frame.items.len()
                    } else {
                        frame.fields.len()
                    };
                    let next = count
                        .checked_add(1)
                        .ok_or_else(|| limit("JSON container items"))?;
                    if self.c.values.limits.list_items != 0
                        && next as u64 > self.c.values.limits.list_items
                    {
                        return Err(limit("JSON container items"));
                    }
                    self.max_items = self.max_items.max(next);
                    frame.weight = frame
                        .weight
                        .checked_add(self.nodes[index].weight)
                        .ok_or_else(|| limit("JSON value bytes"))?;
                    self.scratch.grow(if frame.array { 16 } else { 64 })?;
                    if frame.array {
                        frame.items.push(index);
                    } else {
                        frame.fields.push((frame.key.take().unwrap(), index));
                    }
                    frame.state = 1;
                } else {
                    self.whitespace()?;
                    if self.offset != self.bytes.len() {
                        return Err(invalid("trailing JSON content"));
                    }
                    return Ok(());
                }
            }
            self.whitespace()?;
            if let Some(frame) = stack.last_mut() {
                let closing = if frame.array { b']' } else { b'}' };
                if (frame.state == 0 || frame.state == 1)
                    && self.bytes.get(self.offset) == Some(&closing)
                {
                    self.advance()?;
                    let mut frame = stack.pop().unwrap();
                    let kind = if frame.array {
                        Kind::Array(frame.items)
                    } else {
                        self.scratch.grow(
                            (frame.fields.len() as u64)
                                .checked_mul(64)
                                .ok_or_else(|| limit("JSON key sort bytes"))?,
                        )?;
                        sort_fields(self.c, &mut frame.fields)?;
                        Kind::Object(frame.fields)
                    };
                    value = Some(self.push(kind, frame.start, frame.weight)?);
                    continue;
                }
                if frame.state == 1 {
                    if self.advance()? != b',' {
                        return Err(invalid("missing JSON container separator"));
                    }
                    stack.last_mut().unwrap().state = 2;
                    self.whitespace()?;
                }
                if !stack.last().unwrap().array {
                    let key = self.string()?;
                    let frame = stack.last_mut().unwrap();
                    let weight = 16 + key.len() as u64;
                    frame.weight = frame
                        .weight
                        .checked_add(weight)
                        .ok_or_else(|| limit("JSON value bytes"))?;
                    frame.key = Some(key);
                    self.add_weight(weight)?;
                    self.whitespace()?;
                    if self.advance()? != b':' {
                        return Err(invalid("missing JSON field separator"));
                    }
                    self.whitespace()?;
                }
            }
            match self.bytes.get(self.offset) {
                Some(b'[') | Some(b'{') => {
                    if self.c.values.limits.json_depth != 0
                        && stack.len() as u64 >= self.c.values.limits.json_depth
                    {
                        return Err(limit("JSON container depth"));
                    }
                    let array = self.advance()? == b'[';
                    self.add_weight(16)?;
                    self.scratch.grow(2 * std::mem::size_of::<Frame>() as u64)?;
                    stack.push(Frame {
                        start: self.nodes.len(),
                        array,
                        items: vec![],
                        fields: vec![],
                        key: None,
                        state: 0,
                        weight: 16,
                    });
                }
                _ => value = Some(self.scalar()?),
            }
        }
    }
}
fn decoded_string(c: &mut Context, bytes: &[u8], mut emit: impl FnMut(char)) -> Result<(), Fault> {
    let mut i = 0;
    fn hex(bytes: &[u8], i: &mut usize) -> Result<u16, Fault> {
        let mut n = 0u16;
        for _ in 0..4 {
            let b = *bytes
                .get(*i)
                .ok_or_else(|| invalid("incomplete JSON Unicode escape"))?;
            let digit = (b as char)
                .to_digit(16)
                .ok_or_else(|| invalid("invalid JSON Unicode escape"))?;
            n = n * 16 + digit as u16;
            *i += 1;
        }
        Ok(n)
    }
    while i < bytes.len() {
        c.step()?;
        let b = bytes[i];
        i += 1;
        let ch = if b == b'\\' {
            let escape = *bytes
                .get(i)
                .ok_or_else(|| invalid("incomplete JSON escape"))?;
            i += 1;
            match escape {
                b'"' | b'\\' | b'/' => escape as char,
                b'b' => '\u{8}',
                b'f' => '\u{c}',
                b'n' => '\n',
                b'r' => '\r',
                b't' => '\t',
                b'u' => {
                    let high = hex(bytes, &mut i)?;
                    let point = if (0xd800..=0xdbff).contains(&high) {
                        if bytes.get(i..i + 2) != Some(b"\\u") {
                            return Err(invalid("unpaired JSON surrogate"));
                        }
                        i += 2;
                        let low = hex(bytes, &mut i)?;
                        if !(0xdc00..=0xdfff).contains(&low) {
                            return Err(invalid("unpaired JSON surrogate"));
                        }
                        0x10000 + ((high as u32 - 0xd800) << 10) + (low as u32 - 0xdc00)
                    } else {
                        high as u32
                    };
                    char::from_u32(point).ok_or_else(|| invalid("unpaired JSON surrogate"))?
                }
                _ => return Err(invalid("invalid JSON escape")),
            }
        } else if b < 32 {
            return Err(invalid("raw JSON control character"));
        } else if b < 128 {
            b as char
        } else {
            let width = match b {
                0xc2..=0xdf => 2,
                0xe0..=0xef => 3,
                0xf0..=0xf4 => 4,
                _ => return Err(invalid("invalid JSON UTF-8")),
            };
            let end = (i - 1)
                .checked_add(width)
                .ok_or_else(|| invalid("invalid JSON UTF-8"))?;
            let encoded = bytes
                .get(i - 1..end)
                .ok_or_else(|| invalid("incomplete JSON UTF-8"))?;
            i = end;
            std::str::from_utf8(encoded)
                .map_err(|_| invalid("invalid JSON UTF-8"))?
                .chars()
                .next()
                .unwrap()
        };
        emit(ch);
    }
    Ok(())
}
pub fn compare_text(c: &mut Context, a: &str, b: &str) -> Result<std::cmp::Ordering, Fault> {
    for (a, b) in a.bytes().zip(b.bytes()) {
        c.step()?;
        let order = a.cmp(&b);
        if order != std::cmp::Ordering::Equal {
            return Ok(order);
        }
    }
    c.step()?;
    Ok(a.len().cmp(&b.len()))
}
fn sort_fields(c: &mut Context, fields: &mut Vec<(String, usize)>) -> Result<(), Fault> {
    let n = fields.len();
    if n < 2 {
        return Ok(());
    }
    work(c, n as u64)?;
    let mut order: Vec<usize> = (0..n).collect();
    let mut temporary = vec![0; n];
    let mut width = 1;
    while width < n {
        for start in (0..n).step_by(width * 2) {
            let middle = (start + width).min(n);
            let end = (middle + width).min(n);
            let (mut a, mut b) = (start, middle);
            for slot in &mut temporary[start..end] {
                c.step()?;
                if b == end
                    || a < middle
                        && compare_text(c, &fields[order[a]].0, &fields[order[b]].0)?
                            != std::cmp::Ordering::Greater
                {
                    *slot = order[a];
                    a += 1;
                } else {
                    *slot = order[b];
                    b += 1;
                }
            }
        }
        std::mem::swap(&mut order, &mut temporary);
        width = width
            .checked_mul(2)
            .ok_or_else(|| limit("JSON sort items"))?;
    }
    for pair in order.windows(2) {
        if compare_text(c, &fields[pair[0]].0, &fields[pair[1]].0)? == std::cmp::Ordering::Equal {
            return Err(invalid("duplicate decoded JSON object key"));
        }
    }
    let mut original: Vec<_> = std::mem::take(fields).into_iter().map(Some).collect();
    for index in order {
        fields.push(original[index].take().unwrap());
    }
    Ok(())
}
pub fn parse(c: &mut Context, bytes: &[u8]) -> Result<Document, Fault> {
    let scratch = Scratch::new(c, 0)?;
    let mut parser = Parser {
        c,
        bytes,
        offset: 0,
        scratch,
        nodes: vec![],
        weight: 0,
        max_items: 0,
    };
    parser.document()?;
    Ok(Document {
        nodes: std::mem::take(&mut parser.nodes),
        max_items: parser.max_items,
    })
}
