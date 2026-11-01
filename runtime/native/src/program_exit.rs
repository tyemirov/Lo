use crate::{context::*, value::*};
use num_traits::ToPrimitive;
use serde_json::{json, Value as Json};
use std::collections::BTreeMap;
pub const OPERATION: &str = "Program.Exit.new";
#[derive(Clone)]
pub struct Value {
    pub code: u8,
    output: [Handle; 2],
    stdout_bytes: usize,
    stderr_bytes: usize,
}
impl Value {
    pub fn weight(&self) -> u64 {
        8 + self.output[0].weight + self.output[1].weight
    }
    pub fn retained_children(&self) -> &[Handle] {
        &self.output
    }
    pub fn stdout(&self) -> &[u8] {
        bytes(&self.output[0])
    }
    pub fn stderr(&self) -> &[u8] {
        bytes(&self.output[1])
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        None
    }
    pub fn json(&self) -> Json {
        json!({"code":self.code,"stdout_bytes":self.stdout_bytes,"stderr_bytes":self.stderr_bytes,"redacted":true})
    }
    pub fn canonical_json(&self) -> String {
        format!(
            "{{\"code\":{},\"stdout_bytes\":{},\"stderr_bytes\":{},\"redacted\":true}}",
            self.code, self.stdout_bytes, self.stderr_bytes
        )
    }
    pub fn snapshot(&self) -> Self {
        Self {
            code: self.code,
            output: [
                new(atom("Bytes"), Data::Bytes(vec![])),
                new(atom("Bytes"), Data::Bytes(vec![])),
            ],
            stdout_bytes: self.stdout_bytes,
            stderr_bytes: self.stderr_bytes,
        }
    }
}
fn bytes(value: &Handle) -> &[u8] {
    let Data::Bytes(bytes) = &value.data else {
        std::process::abort()
    };
    bytes
}
pub fn payload(v: &Handle) -> Option<&Value> {
    match &v.data {
        Data::Adapter(crate::adapters::AdapterValue::Program(v)) => Some(v),
        Data::Success(v) => payload(v),
        _ => None,
    }
}
pub fn admit(site: &Site) -> Result<(), String> {
    let types: Vec<_> = site
        .arguments
        .iter()
        .map(|a| match a {
            Argument::Value(t) => t.name(),
            _ => String::new(),
        })
        .collect();
    if types != ["Int", "Bytes", "Bytes"]
        || site.typ != Type::Fallible(Box::new(atom("Program.Exit")))
    {
        return Err("Program.Exit metadata type mismatch".into());
    }
    Ok(())
}
pub fn execute(_context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let Data::Bytes(stdout) = &args[1].data else {
        return Err(host("invalid Program.Exit stdout"));
    };
    let Data::Bytes(stderr) = &args[2].data else {
        return Err(host("invalid Program.Exit stderr"));
    };
    let Some(code) = args[0].integer().to_u8() else {
        return Err(Fault::Recoverable(error(
            site,
            "Program.InvalidExit",
            "Program exit status must be from 0 through 255",
            record([]),
        )));
    };
    if stdout.len() + stderr.len() > 1048576 {
        return Err(Fault::Recoverable(error(
            site,
            "Program.InvalidExit",
            "Program terminal output exceeds the record limit",
            record([]),
        )));
    }
    Ok(new(
        atom("Program.Exit"),
        Data::Adapter(crate::adapters::AdapterValue::Program(Value {
            code,
            output: [args[1].clone(), args[2].clone()],
            stdout_bytes: stdout.len(),
            stderr_bytes: stderr.len(),
        })),
    ))
}
