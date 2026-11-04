# HTTP Operations

[Documentation](../README.md) / [Operation index](README.md)

HTTP constructors create inert values. Outgoing requests and server startup require explicit activation.
The examples construct values without network access.

All entries describe implemented operations. Host quotas and cancellation apply in addition to the listed typed failures.

## HTTP.Client.Request.new

```text
HTTP.Client.Request.new(method: HTTP.Method, url: HTTP.URL, headers: HTTP.Headers, body: Bytes) -> HTTP.Client.Request!
```

Construct an inert outgoing request. The constructor rejects CONNECT and authority or framing headers. Every method, including GET, requires commit for network access.

Commit returns a complete `HTTP.Response`. Valid status codes from 200 through 599 remain response data. Redirects and automatic retries are not supported.

The exchange has a five-second deadline. Request and response bodies each have a 1 MiB bound. Response headers have a 64 KiB bound.

**Failures:** `HTTP.InvalidRequest` during construction. Commit can produce `HTTP.ExchangeFailed` with progress and an unknown remote outcome after transmission.

Host denial, cancellation, and resource limits remain terminal diagnostics. A failed exchange cannot establish that the remote operation did not occur.

**Authority:** Pure construction. Commit requires `--http-allow METHOD,ORIGIN,ADDRESS`. The method, canonical origin, and resolved address must match explicit grants.

**Example:**

```lo
flow Main() -> HTTP.Client.Request! {
    method = HTTP.Method.new("GET")
    url = HTTP.URL.new("https://example.test/resource")
    headers = HTTP.Headers.new(List.new())
    return HTTP.Client.Request.new(method, url, headers, Bytes.new(""))
}
return Main()
```

[Contract](../../specification.md#94-http-client)

## HTTP.Headers.new

```text
HTTP.Headers.new(entries: List<Record{name: String, value: Bytes}>) -> HTTP.Headers!
```

Construct nominal HTTP headers from names and Bytes. Names must be ASCII tokens. The constructor converts names to lowercase and sorts distinct names.

Repeated values keep their supplied order. Values permit opaque bytes and horizontal tabs, but exclude other control bytes and DEL.

**Failures:** `HTTP.InvalidHeaders` for invalid names or values.

**Authority:** Pure construction. No network access.

**Example:**

```lo
return HTTP.Headers.new(List.new(Record.new(name: "X-Label", value: Bytes.new("example"))))
```

[Contract](../../specification.md#92-http-adapter)

## HTTP.Headers.values

```text
HTTP.Headers.values(headers: HTTP.Headers, name: String) -> List<Bytes>!
```

Look up a header name without case sensitivity. An absent valid name returns an empty typed list. Repeated values remain separate Bytes values.

**Failures:** `HTTP.InvalidHeaders` for an invalid lookup name.

**Authority:** Pure lookup. No network access.

**Example:**

```lo
flow Main() -> List<Bytes>! {
    headers = HTTP.Headers.new(List.new())
    return HTTP.Headers.values(headers, "x-label")
}
return Main()
```

[Contract](../../specification.md#92-http-adapter)

## HTTP.Method.new

```text
HTTP.Method.new(text: String) -> HTTP.Method!
```

Validate an ASCII HTTP method token and keep its exact case. A valid method value does not authorize a request.

**Failures:** `HTTP.InvalidMethod` for an invalid token.

**Authority:** Pure construction. No network access.

**Example:**

```lo
return HTTP.Method.new("GET")
```

[Contract](../../specification.md#94-http-client)

## HTTP.Response.Bytes.new

```text
HTTP.Response.Bytes.new(status: Int, body: Bytes) -> HTTP.Response
```

Construct an HTTP response with empty nominal headers and the exact supplied body bytes.

**Failures:** Construction is total for correctly typed arguments. The server checks status and body constraints at emission.

**Authority:** Pure construction. No network access during construction.

**Example:**

```lo
return HTTP.Response.Bytes.new(200, Bytes.new("hello"))
```

[Contract](../../specification.md#92-http-adapter)

## HTTP.Response.Text.new

```text
HTTP.Response.Text.new(status: Int, body: String) -> HTTP.Response
```

Construct an HTTP response with empty nominal headers. The constructor explicitly encodes the text body as UTF-8.

**Failures:** Construction is total for correctly typed arguments. The server checks status and body constraints at emission.

**Authority:** Pure construction. No network access during construction.

**Example:**

```lo
return HTTP.Response.Text.new(200, "hello")
```

[Contract](../../specification.md#92-http-adapter)

## HTTP.Response.new

```text
HTTP.Response.new(status: Int, headers: HTTP.Headers, body: Bytes) -> HTTP.Response
```

Construct an HTTP response from status, nominal headers, and Bytes. The value keeps its supplied bytes. The server checks status and body constraints at emission.

**Failures:** Construction is total for correctly typed arguments. A response that violates protocol constraints can fail at server emission.

**Authority:** Pure construction. Returning a response to an active server handler lets that server emit it.

**Example:**

```lo
flow Main() -> HTTP.Response! {
    headers = HTTP.Headers.new(List.new())
    return HTTP.Response.new(200, headers, Bytes.new("ok"))
}
return Main()
```

[Contract](../../specification.md#92-http-adapter)

## HTTP.Server.new

```text
HTTP.Server.new(port: Int, handler: FlowName, onError: FlowName) -> HTTP.Server
```

Construct an inert HTTP server with named request and error handlers. The request handler accepts `HTTP.Request` and returns `HTTP.Response` or `HTTP.Response!`.

The required error handler accepts `Error` and returns a pure total `HTTP.Response`. Commit starts the listener and returns its address and actual port.

**Failures:** Invalid handler types or effects fail compiler checks. Activation can produce `HTTP.ServerStartFailed`, including an invalid port outside `0..65535`.

Host denial and resource failures remain terminal diagnostics. Port `0` requests an available port from the operating system.

**Authority:** Pure construction. Activation requires a matching host listen grant. The host session owns shutdown.

**Example:**

```lo
flow Handle(request: HTTP.Request) -> HTTP.Response {
    return HTTP.Response.Text.new(200, "ok")
}
flow Failed(error: Error) -> HTTP.Response {
    return HTTP.Response.Text.new(500, "failed")
}
return HTTP.Server.new(8080, Handle, Failed)
```

[Contract](../../specification.md#92-http-adapter)

## HTTP.URL.new

```text
HTTP.URL.new(text: String) -> HTTP.URL!
```

Validate an absolute HTTP or HTTPS URL. Construction converts scheme and host names to canonical case and makes the default port explicit.

The constructor rejects credentials, fragments, whitespace, invalid hosts, and invalid ports. Construction does not resolve DNS names.

**Failures:** `HTTP.InvalidURL` for invalid URL text.

**Authority:** Pure construction. No DNS, network access, or host grant.

**Example:**

```lo
return HTTP.URL.new("https://example.test/resource")
```

[Contract](../../specification.md#94-http-client)
