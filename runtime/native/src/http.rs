use crate::{
    context::*,
    value::{self, *},
};
use num_traits::ToPrimitive;
use serde_json::{json, Value as Json};
use std::{
    collections::BTreeMap,
    net::{IpAddr, SocketAddr},
    sync::Arc,
};
mod client;
mod resolver;
mod server;
mod sniff;
mod tls;
pub use server::Http;
pub const MAX_BODY: usize = 1 << 20;
pub const MAX_HEADERS: usize = 64 << 10;
pub const SUPPORTED: &[&str] = &[
    "HTTP.Headers.new",
    "HTTP.Headers.values",
    "HTTP.URL.new",
    "HTTP.Method.new",
    "HTTP.Client.Request.new",
    "HTTP.Response.new",
    "HTTP.Response.Text.new",
    "HTTP.Response.Bytes.new",
    "HTTP.Server.new",
];
#[derive(Clone)]
pub enum Value {
    Headers {
        entries: Handle,
        fields: BTreeMap<String, Handle>,
    },
    Url(Url),
    Fields(Handle),
    Client {
        method: Handle,
        url: Handle,
        headers: Handle,
        body: Handle,
    },
    Server {
        port: Handle,
        handler: usize,
        on_error: usize,
        handler_name: String,
        error_name: String,
    },
}
impl Value {
    pub fn weight(&self) -> u64 {
        match self {
            Self::Headers { entries, .. } => entries.weight,
            Self::Url(url) => url.text.len() as u64,
            Self::Fields(fields) => fields.weight,
            Self::Client {
                method,
                url,
                headers,
                body,
            } => method.weight + url.weight + headers.weight + body.weight,
            Self::Server {
                port,
                handler_name,
                error_name,
                ..
            } => port.weight + (handler_name.len() + error_name.len()) as u64,
        }
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        match self {
            Self::Headers { fields, .. } => Some(fields),
            Self::Fields(fields) => Some(fields.fields()),
            _ => None,
        }
    }
    pub fn json(&self) -> Json {
        match self {
            Self::Headers { entries, .. } => json!({"entries":entries.json()}),
            Self::Url(url) => json!(url.text),
            Self::Fields(fields) => Json::Object(
                fields
                    .fields()
                    .iter()
                    .map(|(n, v)| (n.clone(), v.json()))
                    .collect(),
            ),
            Self::Client {
                method,
                url,
                headers,
                body,
            } => {
                json!({"method":method.json(),"url":url.json(),"headers":headers.json(),"body":body.json()})
            }
            Self::Server {
                port,
                handler_name,
                error_name,
                ..
            } => json!({"port":port.json(),"handler":handler_name,"on_error":error_name}),
        }
    }
    pub fn canonical_json(&self) -> String {
        match self {
            Self::Headers { entries, .. } => {
                format!("{{\"entries\":{}}}", entries.canonical_json())
            }
            Self::Url(url) => quote(&url.text),
            Self::Fields(fields) => fields_payload(fields.fields()),
            Self::Client {
                method,
                url,
                headers,
                body,
            } => format!(
                "{{\"method\":{},\"url\":{},\"headers\":{},\"body\":{}}}",
                method.canonical_json(),
                url.canonical_json(),
                headers.canonical_json(),
                body.canonical_json()
            ),
            Self::Server {
                port,
                handler_name,
                error_name,
                ..
            } => format!(
                "{{\"port\":{},\"handler\":{},\"on_error\":{}}}",
                port.canonical_json(),
                quote(handler_name),
                quote(error_name)
            ),
        }
    }
    pub fn output(&self) -> String {
        self.canonical_json()
    }
    pub fn snapshot(&self) -> Self {
        match self {
            Self::Headers { entries, .. } => {
                let redacted = headers(
                    entries
                        .items()
                        .iter()
                        .map(|e| (e.fields()["name"].string().to_owned(), vec![]))
                        .collect(),
                )
                .unwrap();
                as_http(&redacted).clone()
            }
            Self::Url(url) => {
                let mut url = url.clone();
                url.path = "/".into();
                url.text = format!("{}/", url.origin);
                Self::Url(url)
            }
            Self::Client {
                method,
                url,
                headers,
                body: _,
            } => Self::Client {
                method: method.clone(),
                url: value::snapshot(url),
                headers: value::snapshot(headers),
                body: new(atom("Bytes"), Data::Bytes(vec![])),
            },
            Self::Fields(fields) => Self::Fields(value::snapshot(fields)),
            Self::Server { .. } => self.clone(),
        }
    }
}
fn fields_payload(fields: &BTreeMap<String, Handle>) -> String {
    format!(
        "{{{}}}",
        fields
            .iter()
            .map(|(n, v)| format!("{}:{}", quote(n), v.canonical_json()))
            .collect::<Vec<_>>()
            .join(",")
    )
}
fn quote(s: &str) -> String {
    serde_json::to_string(s)
        .unwrap()
        .replace('&', "\\u0026")
        .replace('<', "\\u003c")
        .replace('>', "\\u003e")
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029")
}
pub fn wrap(typ: &str, value: Value) -> Handle {
    new(
        atom(typ),
        Data::Adapter(crate::adapters::AdapterValue::Http(value)),
    )
}
pub fn as_http(handle: &Handle) -> &Value {
    if let Data::Adapter(crate::adapters::AdapterValue::Http(value)) = &handle.data {
        value
    } else {
        std::process::abort()
    }
}
pub fn bytes(handle: &Handle) -> &[u8] {
    if let Data::Bytes(value) = &handle.data {
        value
    } else {
        std::process::abort()
    }
}
pub fn token(name: &str) -> bool {
    !name.is_empty()
        && name
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"!#$%&'*+-.^_`|~".contains(&b))
}
fn header_name(name: &str) -> Result<String, String> {
    if name.is_empty() {
        Err("HTTP header name is empty".into())
    } else if !token(name) {
        Err("HTTP header name is not an ASCII token".into())
    } else {
        Ok(name.to_ascii_lowercase())
    }
}
pub fn headers(items: Vec<(String, Vec<u8>)>) -> Result<Handle, String> {
    let mut entries = vec![];
    for (index, (name, content)) in items.into_iter().enumerate() {
        let name =
            header_name(&name).map_err(|e| format!("HTTP header entry {index} name: {e}"))?;
        if content.iter().any(|b| *b != 9 && (*b < 32 || *b == 127)) {
            return Err(format!(
                "HTTP header entry {index} value contains a prohibited byte"
            ));
        }
        entries.push((name, content));
    }
    entries.sort_by(|a, b| a.0.cmp(&b.0));
    let typ = Type::Record(vec![
        ("name".into(), atom("String")),
        ("value".into(), atom("Bytes")),
    ]);
    let entries = new(
        Type::List(Box::new(typ)),
        Data::List(
            entries
                .into_iter()
                .map(|(n, b)| {
                    record([
                        ("name".into(), text(n)),
                        ("value".into(), new(atom("Bytes"), Data::Bytes(b))),
                    ])
                })
                .collect(),
        ),
    );
    Ok(wrap(
        "HTTP.Headers",
        Value::Headers {
            fields: BTreeMap::from([("entries".into(), entries.clone())]),
            entries,
        },
    ))
}
pub fn header_pairs(handle: &Handle) -> Vec<(String, Vec<u8>)> {
    let Value::Headers { entries, .. } = as_http(handle) else {
        std::process::abort()
    };
    entries
        .items()
        .iter()
        .map(|e| {
            (
                e.fields()["name"].string().to_owned(),
                bytes(&e.fields()["value"]).to_vec(),
            )
        })
        .collect()
}
#[derive(Clone)]
pub struct Url {
    pub text: String,
    pub scheme: String,
    pub host: String,
    pub port: u16,
    pub path: String,
    pub origin: String,
}
impl Url {
    pub fn parse(input: &str) -> Result<Self, String> {
        let invalid="HTTP URL requires an absolute destination without credentials, fragments, or whitespace";
        if input
            .bytes()
            .any(|b| b"\r\n\t #".contains(&b) || b < 32 || b == 127)
        {
            return Err(invalid.into());
        }
        let (scheme, rest) = input.split_once("://").ok_or(invalid)?;
        let scheme = scheme.to_ascii_lowercase();
        if scheme != "http" && scheme != "https" {
            return Err("HTTP URL requires http or https".into());
        }
        let split = rest.find(['/', '?']).unwrap_or(rest.len());
        let authority = &rest[..split];
        let suffix = &rest[split..];
        if authority.is_empty() || authority.contains('@') {
            return Err(invalid.into());
        }
        let (hostname, port) = if authority.starts_with('[') {
            let end = authority.find(']').ok_or("HTTP URL has an invalid host")?;
            if authority[1..end].parse::<std::net::Ipv6Addr>().is_err() {
                return Err(invalid.into());
            }
            let tail = &authority[end + 1..];
            if !tail.is_empty() && !tail.starts_with(':') {
                return Err("HTTP URL has an invalid host".into());
            }
            (&authority[1..end], tail.strip_prefix(':'))
        } else {
            match authority.rsplit_once(':') {
                Some((h, p)) => (h, Some(p)),
                None => (authority, None),
            }
        };
        if hostname.is_empty() || hostname.contains('%') || port == Some("") {
            return Err("HTTP URL has an invalid host".into());
        }
        let mut host = hostname.to_ascii_lowercase();
        if let Ok(ip) = host.parse::<IpAddr>() {
            host = unmap(ip).to_string()
        } else {
            if host.len() > 253 {
                return Err("HTTP URL hostname exceeds its limit".into());
            }
            if host.split('.').any(|l| {
                l.is_empty()
                    || l.len() > 63
                    || l.starts_with('-')
                    || l.ends_with('-')
                    || !l
                        .bytes()
                        .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
            }) {
                return Err("HTTP URL has an invalid hostname".into());
            }
        }
        let port = match port {
            Some(p) if p.bytes().all(|b| b.is_ascii_digit()) => p
                .parse::<u16>()
                .ok()
                .filter(|p| *p > 0)
                .ok_or("HTTP URL has an invalid port")?,
            None => {
                if scheme == "https" {
                    443
                } else {
                    80
                }
            }
            _ => return Err("HTTP URL has an invalid port".into()),
        };
        let mut encoded = String::new();
        let (path_part, query) = suffix
            .split_once('?')
            .map(|(path, query)| (path, Some(query)))
            .unwrap_or((suffix, None));
        for b in path_part.bytes() {
            if b >= 128 || b"\\\"<>^`{|}".contains(&b) {
                encoded.push_str(&format!("%{b:02X}"))
            } else {
                encoded.push(b as char)
            }
        }
        let data = encoded.as_bytes();
        for i in 0..data.len() {
            if data[i] == b'%'
                && (i + 2 >= data.len()
                    || !data[i + 1].is_ascii_hexdigit()
                    || !data[i + 2].is_ascii_hexdigit())
            {
                return Err(invalid.into());
            }
        }
        if let Some(query) = query {
            encoded.push('?');
            encoded.push_str(query);
        }
        let authority = if host.contains(':') {
            format!("[{host}]:{port}")
        } else {
            format!("{host}:{port}")
        };
        let origin = format!("{scheme}://{authority}");
        let text = format!("{origin}{encoded}");
        let path = if encoded.is_empty() {
            "/".into()
        } else if encoded.starts_with('?') {
            format!("/{encoded}")
        } else {
            encoded
        };
        Ok(Self {
            text,
            scheme,
            host,
            port,
            path,
            origin,
        })
    }
}
pub fn unmap(ip: IpAddr) -> IpAddr {
    match ip {
        IpAddr::V6(ip) => ip
            .to_ipv4_mapped()
            .map(IpAddr::V4)
            .unwrap_or(IpAddr::V6(ip)),
        ip => ip,
    }
}
pub fn admit(site: &Site) -> Result<(), String> {
    let args = site
        .arguments
        .iter()
        .filter_map(|a| match a {
            Argument::Value(t) => Some(t.name()),
            _ => None,
        })
        .collect::<Vec<_>>();
    let (expected, out) = match site.operation.as_str() {
        "HTTP.URL.new" => (vec!["String"], "HTTP.URL!"),
        "HTTP.Method.new" => (vec!["String"], "HTTP.Method!"),
        "HTTP.Headers.new" => (
            vec!["List<Record{name: String, value: Bytes}>"],
            "HTTP.Headers!",
        ),
        "HTTP.Headers.values" => (vec!["HTTP.Headers", "String"], "List<Bytes>!"),
        "HTTP.Client.Request.new" => (
            vec!["HTTP.Method", "HTTP.URL", "HTTP.Headers", "Bytes"],
            "HTTP.Client.Request!",
        ),
        "HTTP.Response.new" => (vec!["Int", "HTTP.Headers", "Bytes"], "HTTP.Response"),
        "HTTP.Response.Text.new" => (vec!["Int", "String"], "HTTP.Response"),
        "HTTP.Response.Bytes.new" => (vec!["Int", "Bytes"], "HTTP.Response"),
        "HTTP.Server.new" => {
            if site.arguments.len() != 3
                || !matches!(site.arguments[0],Argument::Value(ref t)if *t==atom("Int"))
                || !matches!(site.arguments[1], Argument::Flow(_))
                || !matches!(site.arguments[2], Argument::Flow(_))
                || site.typ != atom("HTTP.Server")
            {
                return Err("HTTP server operation metadata type mismatch".into());
            }
            return Ok(());
        }
        _ => return Err("unsupported HTTP operation".into()),
    };
    if args.iter().map(String::as_str).collect::<Vec<_>>() == expected
        && args.len() == site.arguments.len()
        && site.typ.name() == out
    {
        Ok(())
    } else {
        Err("HTTP operation metadata type mismatch".into())
    }
}
pub fn execute(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let failure =
        |kind: &str, message: &str| Err(Fault::Recoverable(error(site, kind, message, record([]))));
    match site.operation.as_str() {
        "HTTP.URL.new" => match Url::parse(args[0].string()) {
            Ok(url) => Ok(wrap("HTTP.URL", Value::Url(url))),
            Err(e) => failure("HTTP.InvalidURL", &e),
        },
        "HTTP.Method.new" => {
            if !token(args[0].string()) {
                failure(
                    "HTTP.InvalidMethod",
                    "HTTP method must be a nonempty ASCII token",
                )
            } else {
                Ok(new(
                    atom("HTTP.Method"),
                    Data::Text(args[0].string().into()),
                ))
            }
        }
        "HTTP.Headers.new" => {
            let entries = args[0]
                .items()
                .iter()
                .map(|e| {
                    (
                        e.fields()["name"].string().into(),
                        bytes(&e.fields()["value"]).to_vec(),
                    )
                })
                .collect();
            match headers(entries) {
                Ok(v) => Ok(v),
                Err(e) => failure("HTTP.InvalidHeaders", &e),
            }
        }
        "HTTP.Headers.values" => {
            let name = match header_name(args[1].string()) {
                Ok(n) => n,
                Err(e) => return failure("HTTP.InvalidHeaders", &e),
            };
            let matches = header_pairs(&args[0])
                .into_iter()
                .filter(|(n, _)| n == &name)
                .map(|(_, b)| new(atom("Bytes"), Data::Bytes(b)))
                .collect();
            Ok(new(
                Type::List(Box::new(atom("Bytes"))),
                Data::List(matches),
            ))
        }
        "HTTP.Response.new" | "HTTP.Response.Text.new" | "HTTP.Response.Bytes.new" => {
            let (headers, body) = if site.operation == "HTTP.Response.new" {
                (args[1].clone(), args[2].clone())
            } else {
                (
                    headers(vec![]).unwrap(),
                    if site.operation == "HTTP.Response.Text.new" {
                        new(
                            atom("Bytes"),
                            Data::Bytes(args[1].string().as_bytes().to_vec()),
                        )
                    } else {
                        args[1].clone()
                    },
                )
            };
            Ok(wrap(
                "HTTP.Response",
                Value::Fields(record([
                    ("status".into(), args[0].clone()),
                    ("headers".into(), headers),
                    ("body".into(), body),
                ])),
            ))
        }
        "HTTP.Client.Request.new" => {
            if args[0].string() == "CONNECT" {
                return failure("HTTP.InvalidRequest", "HTTP CONNECT is not supported");
            }
            if header_pairs(&args[2]).iter().any(|(n, _)| {
                [
                    "host",
                    "connection",
                    "upgrade",
                    "content-length",
                    "transfer-encoding",
                    "trailer",
                    "te",
                    "keep-alive",
                    "proxy-connection",
                    "proxy-authorization",
                    "expect",
                ]
                .contains(&n.as_str())
            }) {
                return failure(
                    "HTTP.InvalidRequest",
                    "HTTP client header controls authority or protocol framing",
                );
            }
            Ok(wrap(
                "HTTP.Client.Request",
                Value::Client {
                    method: args[0].clone(),
                    url: args[1].clone(),
                    headers: args[2].clone(),
                    body: args[3].clone(),
                },
            ))
        }
        "HTTP.Server.new" => {
            let flow = |index: usize| match &args[index].data {
                Data::Flow(id) => *id,
                _ => std::process::abort(),
            };
            let handler = flow(1);
            let on_error = flow(2);
            Ok(wrap(
                "HTTP.Server",
                Value::Server {
                    port: args[0].clone(),
                    handler,
                    on_error,
                    handler_name: context.program.flows[handler].identity.clone(),
                    error_name: context.program.flows[on_error].identity.clone(),
                },
            ))
        }
        _ => Err(host("invalid HTTP operation")),
    }
}
pub fn response(status: Handle, headers: Handle, body: Handle) -> Handle {
    wrap(
        "HTTP.Response",
        Value::Fields(record([
            ("status".into(), status),
            ("headers".into(), headers),
            ("body".into(), body),
        ])),
    )
}

pub fn admit_callbacks(site: &Site, flows: &[Flow]) -> Result<(), String> {
    if site.operation != "HTTP.Server.new" {
        return Ok(());
    }
    for (position, input, fallible) in [(1, "HTTP.Request", true), (2, "Error", false)] {
        let Some(Argument::Flow(index)) = site.arguments.get(position) else {
            return Err("HTTP server callback metadata mismatch".into());
        };
        let flow = flows.get(*index).ok_or("unknown HTTP server callback")?;
        if flow.parameters.len() != 1
            || flow.parameters[0].1 != atom(input)
            || flow.output.base() != atom("HTTP.Response")
            || (!fallible && flow.output != atom("HTTP.Response"))
        {
            return Err("HTTP server callback signature mismatch".into());
        }
    }
    Ok(())
}
