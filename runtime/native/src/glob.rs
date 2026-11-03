use crate::{context::*, value::*};
use std::{iter::Peekable, str::Chars, sync::Arc};

#[derive(Clone)]
pub struct Glob {
    pub pattern: String,
    tokens: Arc<Vec<Token>>,
    weight: u64,
}
#[derive(Clone)]
enum Token {
    Literal(char),
    Any,
    Star,
    Class {
        negated: bool,
        ranges: Vec<(char, char)>,
    },
}
impl Glob {
    pub fn weight(&self) -> u64 {
        self.weight
    }
}
fn next(context: &mut Context, chars: &mut Peekable<Chars<'_>>) -> Result<Option<char>, Fault> {
    let value = chars.next();
    if value.is_some() {
        context.step()?;
    }
    Ok(value)
}
fn invalid(site: &Site, args: &[Handle]) -> Fault {
    match crate::processors::fail(
        site,
        args,
        "Glob.InvalidPattern",
        "glob pattern is malformed",
    ) {
        Err(fault) => fault,
        Ok(_) => std::process::abort(),
    }
}
fn scalar(
    context: &mut Context,
    chars: &mut Peekable<Chars<'_>>,
    site: &Site,
    args: &[Handle],
) -> Result<char, Fault> {
    match next(context, chars)? {
        Some('\\') => next(context, chars)?.ok_or_else(|| invalid(site, args)),
        Some(c) => Ok(c),
        None => Err(invalid(site, args)),
    }
}
pub fn compile(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let pattern = args[0].string();
    let mut chars = pattern.chars().peekable();
    let mut tokens = Vec::new();
    let mut weight = pattern.len() as u64;
    context.values.preflight(weight.saturating_add(16))?;
    while let Some(c) = next(context, &mut chars)? {
        weight = weight.saturating_add(32);
        context.values.preflight(weight.saturating_add(16))?;
        let token = match c {
            '*' => Token::Star,
            '?' => Token::Any,
            '\\' => Token::Literal(next(context, &mut chars)?.ok_or_else(|| invalid(site, args))?),
            '[' => {
                let negated = chars.peek() == Some(&'!');
                if negated {
                    next(context, &mut chars)?;
                }
                let mut ranges = Vec::new();
                loop {
                    if chars.peek() == Some(&']') {
                        next(context, &mut chars)?;
                        if ranges.is_empty() {
                            return Err(invalid(site, args));
                        }
                        break;
                    }
                    let mut first = scalar(context, &mut chars, site, args)?;
                    let last = if chars.peek() == Some(&'-') {
                        next(context, &mut chars)?;
                        if chars.peek() == Some(&']') {
                            weight = weight.saturating_add(8);
                            context.values.preflight(weight.saturating_add(16))?;
                            ranges.push((first, first));
                            first = '-';
                            '-'
                        } else {
                            scalar(context, &mut chars, site, args)?
                        }
                    } else {
                        first
                    };
                    if first > last {
                        return Err(invalid(site, args));
                    }
                    weight = weight.saturating_add(8);
                    context.values.preflight(weight.saturating_add(16))?;
                    ranges.push((first, last));
                }
                Token::Class { negated, ranges }
            }
            _ => Token::Literal(c),
        };
        tokens.push(token);
    }
    for _ in pattern.bytes() {
        context.step()?;
    }
    Ok(new(
        atom("Glob"),
        Data::Glob(Glob {
            pattern: pattern.to_owned(),
            tokens: Arc::new(tokens),
            weight,
        }),
    ))
}
pub fn matches(context: &mut Context, pattern: &Handle, text: &str) -> Outcome {
    let Data::Glob(pattern) = &pattern.data else {
        return Err(host("invalid glob value"));
    };
    let width = pattern
        .tokens
        .len()
        .checked_add(1)
        .ok_or_else(|| limit("live value bytes"))?;
    let bytes = (width as u64)
        .checked_mul(2)
        .ok_or_else(|| limit("live value bytes"))?;
    let _scratch = crate::processors::Scratch::new(context, bytes)?;
    let mut current = vec![0u8; width];
    let mut following = vec![0u8; width];
    current[0] = 1;
    for (index, token) in pattern.tokens.iter().enumerate() {
        context.step()?;
        if matches!(token, Token::Star) {
            current[index + 1] = current[index];
        }
    }
    for c in text.chars() {
        context.step()?;
        following[0] = 0;
        for (index, token) in pattern.tokens.iter().enumerate() {
            context.step()?;
            following[index + 1] = match token {
                Token::Star => following[index] | current[index + 1],
                Token::Any => current[index],
                Token::Literal(expected) => u8::from(c == *expected) & current[index],
                Token::Class { negated, ranges } => {
                    let mut found = false;
                    for (first, last) in ranges {
                        context.step()?;
                        if *first <= c && c <= *last {
                            found = true;
                            break;
                        }
                    }
                    u8::from(found != *negated) & current[index]
                }
            };
        }
        std::mem::swap(&mut current, &mut following);
    }
    Ok(boolean(current[width - 1] != 0))
}
