use crate::{adapters::AdapterValue, context::*, value::*};
use serde_json::{json, Value as Json};
use std::collections::{BTreeMap, BTreeSet};

pub const OPERATION: &str = "CLI.parse";
const OPTIONS: &str = "CLI.Options";
const INVALID_DEFINITION: &str = "CLI.InvalidDefinition";
const INVALID_ARGUMENTS: &str = "CLI.InvalidArguments";
const HELP_SHORT: &str = "-h";
const HELP_LONG: &str = "--help";
const HELP_ROW: &str = "  -h, --help  Show this help message and exit.\n";

#[derive(Clone)]
pub struct Value {
    fields: Handle,
}

impl Value {
    pub fn weight(&self) -> u64 {
        self.fields.weight
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        Some(self.fields.fields())
    }
    pub fn json(&self) -> Json {
        let fields = self.fields.fields();
        json!({"flags":fields["flags"].json(),"help_requested":fields["help_requested"].json(),"help_text":fields["help_text"].json()})
    }
    pub fn canonical_json(&self) -> String {
        let fields = self.fields.fields();
        format!(
            "{{\"flags\":{},\"help_requested\":{},\"help_text\":{}}}",
            fields["flags"].canonical_json(),
            fields["help_requested"].canonical_json(),
            fields["help_text"].canonical_json()
        )
    }
    pub fn snapshot(&self) -> Self {
        Self {
            fields: snapshot(&self.fields),
        }
    }
}

struct Flag {
    name: String,
    aliases: Vec<String>,
    group: String,
    help: String,
}
struct Spelling {
    text: String,
    owner: usize,
}
struct Definition {
    flags: Vec<Flag>,
    exact: BTreeMap<String, usize>,
    long: Vec<Spelling>,
    help: String,
    abbreviations: bool,
}
struct Parsed {
    selections: Vec<bool>,
    help_requested: bool,
    definition: Definition,
}

fn failure(site: &Site, kind: &str, reason: &str, index: i64, candidates: Vec<String>) -> Fault {
    let input = record([
        ("reason".into(), text(reason)),
        ("index".into(), new(atom("Int"), Data::Int(index.into()))),
        (
            "candidates".into(),
            new(
                Type::List(Box::new(atom("String"))),
                Data::List(candidates.into_iter().map(text).collect()),
            ),
        ),
    ]);
    Fault::Recoverable(error(site, kind, "declared option parsing failed", input))
}

fn valid_text(context: &mut Context, text: &str) -> Result<bool, Fault> {
    for scalar in text.chars() {
        context.step()?;
        if scalar.is_control() {
            return Ok(false);
        }
    }
    Ok(true)
}

fn valid_spelling(context: &mut Context, text: &str) -> Result<bool, Fault> {
    for (index, character) in text.bytes().enumerate() {
        context.step()?;
        let valid = if index == 0 {
            character == b'-'
        } else if text.len() == 2 {
            character.is_ascii_alphanumeric()
        } else if index == 1 {
            character == b'-'
        } else if index == 2 || index == text.len() - 1 {
            character.is_ascii_alphanumeric()
        } else {
            character.is_ascii_alphanumeric() || character == b'-'
        };
        if !valid {
            return Ok(false);
        }
    }
    Ok(text.len() >= 2 && text != "--")
}

fn compile(
    context: &mut Context,
    site: &Site,
    flags: Vec<Flag>,
    name: &str,
    description: &str,
    abbreviations: bool,
) -> Result<Definition, Fault> {
    let mut definition = Definition {
        flags: vec![],
        exact: BTreeMap::from([(HELP_SHORT.into(), 0), (HELP_LONG.into(), 0)]),
        long: vec![Spelling {
            text: HELP_LONG.into(),
            owner: 0,
        }],
        help: String::new(),
        abbreviations,
    };
    for (text, reason) in [(name, "invalid-name"), (description, "invalid-description")] {
        if !valid_text(context, text)? || (reason == "invalid-name" && text.is_empty()) {
            return Err(failure(site, INVALID_DEFINITION, reason, -1, vec![]));
        }
    }
    let mut help = format!("Usage: {name} [OPTIONS]\n\n");
    if !description.is_empty() {
        help.push_str(description);
        help.push_str("\n\n");
    }
    help.push_str("Options:\n");
    help.push_str(HELP_ROW);
    for (index, flag) in flags.into_iter().enumerate() {
        context.step()?;
        for (text, reason) in [(&flag.group, "invalid-group"), (&flag.help, "invalid-help")] {
            if !valid_text(context, text)? {
                return Err(failure(
                    site,
                    INVALID_DEFINITION,
                    reason,
                    index as i64,
                    vec![],
                ));
            }
        }
        let spellings: Vec<_> = std::iter::once(&flag.name)
            .chain(flag.aliases.iter())
            .collect();
        for (alias, spelling) in spellings.iter().enumerate() {
            if !valid_spelling(context, spelling)? {
                let reason = if alias == 0 {
                    "invalid-flag-name"
                } else {
                    "invalid-alias"
                };
                return Err(failure(
                    site,
                    INVALID_DEFINITION,
                    reason,
                    index as i64,
                    vec![],
                ));
            }
            if spelling.as_str() == HELP_SHORT || spelling.as_str() == HELP_LONG {
                return Err(failure(
                    site,
                    INVALID_DEFINITION,
                    "reserved-spelling",
                    index as i64,
                    vec![],
                ));
            }
            if definition.exact.contains_key(spelling.as_str()) {
                return Err(failure(
                    site,
                    INVALID_DEFINITION,
                    "duplicate-spelling",
                    index as i64,
                    vec![],
                ));
            }
            definition.exact.insert((*spelling).clone(), index + 1);
            if spelling.starts_with("--") {
                definition.long.push(Spelling {
                    text: (*spelling).clone(),
                    owner: index + 1,
                });
            }
        }
        help.push_str("  ");
        for (index, spelling) in spellings.iter().enumerate() {
            if index != 0 {
                help.push_str(", ");
            }
            help.push_str(spelling);
        }
        help.push_str("  ");
        help.push_str(&flag.help);
        help.push('\n');
        definition.flags.push(flag);
    }
    definition.help = help;
    Ok(definition)
}

impl Definition {
    fn candidates(&self, owners: &[usize]) -> Vec<String> {
        let selected: BTreeSet<_> = owners.iter().copied().collect();
        let mut names = vec![];
        if selected.contains(&0) {
            names.push(HELP_LONG.into());
        }
        for (index, flag) in self.flags.iter().enumerate() {
            if selected.contains(&(index + 1)) {
                names.push(flag.name.clone());
            }
        }
        names
    }
    fn resolve(
        &self,
        context: &mut Context,
        site: &Site,
        argument: &str,
        index: usize,
    ) -> Result<usize, Fault> {
        if let Some(owner) = self.exact.get(argument) {
            return Ok(*owner);
        }
        if self.abbreviations && argument.starts_with("--") {
            let mut owners = vec![];
            let mut seen = BTreeSet::new();
            for candidate in &self.long {
                context.step()?;
                if candidate.text.starts_with(argument) && seen.insert(candidate.owner) {
                    owners.push(candidate.owner);
                }
            }
            if owners.len() == 1 {
                return Ok(owners[0]);
            }
            if owners.len() > 1 {
                return Err(failure(
                    site,
                    INVALID_ARGUMENTS,
                    "ambiguous-option",
                    index as i64,
                    self.candidates(&owners),
                ));
            }
        }
        Err(failure(
            site,
            INVALID_ARGUMENTS,
            "unknown-option",
            index as i64,
            vec![],
        ))
    }
}

fn parse(
    context: &mut Context,
    site: &Site,
    arguments: &[String],
    definition: Definition,
) -> Result<Parsed, Fault> {
    let mut selections = vec![false; definition.flags.len()];
    let mut groups = BTreeMap::new();
    for (index, argument) in arguments.iter().enumerate() {
        context.step()?;
        if !valid_text(context, argument)? {
            return Err(failure(
                site,
                INVALID_ARGUMENTS,
                "invalid-argument",
                index as i64,
                vec![],
            ));
        }
        if !valid_spelling(context, argument)? {
            return Err(failure(
                site,
                INVALID_ARGUMENTS,
                "unsupported-token",
                index as i64,
                vec![],
            ));
        }
        let owner = definition.resolve(context, site, argument, index)?;
        if owner == 0 {
            return Ok(Parsed {
                selections,
                help_requested: true,
                definition,
            });
        }
        let flag = &definition.flags[owner - 1];
        if !flag.group.is_empty() {
            if let Some(previous) = groups.get(&flag.group) {
                if *previous != owner {
                    return Err(failure(
                        site,
                        INVALID_ARGUMENTS,
                        "exclusive-group",
                        index as i64,
                        definition.candidates(&[*previous, owner]),
                    ));
                }
            }
            groups.insert(flag.group.clone(), owner);
        }
        selections[owner - 1] = true;
    }
    Ok(Parsed {
        selections,
        help_requested: false,
        definition,
    })
}

pub fn admit(site: &Site) -> Result<(), String> {
    let flag = Type::Record(vec![
        ("aliases".into(), Type::List(Box::new(atom("String")))),
        ("group".into(), atom("String")),
        ("help".into(), atom("String")),
        ("name".into(), atom("String")),
    ]);
    let expected = [
        Type::List(Box::new(atom("String"))),
        Type::List(Box::new(flag)),
        atom("String"),
        atom("String"),
        atom("Bool"),
    ];
    if site.operation != OPERATION
        || site.typ != Type::Fallible(Box::new(atom(OPTIONS)))
        || site.arguments.len() != expected.len()
        || site
            .arguments
            .iter()
            .zip(expected)
            .any(|(actual, expected)| !matches!(actual, Argument::Value(typ) if *typ == expected))
    {
        return Err("CLI option metadata type mismatch".into());
    }
    Ok(())
}

pub fn execute(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let mut arguments = Vec::with_capacity(args[0].items().len());
    for argument in args[0].items() {
        context.step()?;
        arguments.push(argument.string().to_owned());
    }
    let mut flags = Vec::with_capacity(args[1].items().len());
    for value in args[1].items() {
        context.step()?;
        let fields = value.fields();
        let mut aliases = Vec::with_capacity(fields["aliases"].items().len());
        for alias in fields["aliases"].items() {
            context.step()?;
            aliases.push(alias.string().to_owned());
        }
        flags.push(Flag {
            name: fields["name"].string().to_owned(),
            aliases,
            group: fields["group"].string().to_owned(),
            help: fields["help"].string().to_owned(),
        });
    }
    let definition = compile(
        context,
        site,
        flags,
        args[2].string(),
        args[3].string(),
        args[4].bool(),
    )?;
    let parsed = parse(context, site, &arguments, definition)?;
    let mut options = Vec::with_capacity(parsed.selections.len());
    for (flag, selected) in parsed.definition.flags.iter().zip(parsed.selections) {
        context.step()?;
        options.push((text(&flag.name), boolean(selected)));
    }
    let fields = record([
        (
            "flags".into(),
            new(
                Type::Map(Box::new(atom("String")), Box::new(atom("Bool"))),
                Data::Map(options),
            ),
        ),
        ("help_requested".into(), boolean(parsed.help_requested)),
        ("help_text".into(), text(parsed.definition.help)),
    ]);
    Ok(new(
        atom(OPTIONS),
        Data::Adapter(AdapterValue::Cli(Value { fields })),
    ))
}
