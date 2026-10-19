package value

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// URL is a validated absolute HTTP destination without credentials or fragments.
type URL interface {
	Value
	Text() string
	Origin() string
	Parsed() url.URL
	httpURL()
}
type httpURL struct {
	base
	parsed url.URL
}

func (httpURL) httpURL()                       {}
func (u httpURL) Text() string                 { return u.parsed.String() }
func (u httpURL) Origin() string               { return u.parsed.Scheme + "://" + u.parsed.Host }
func (u httpURL) Parsed() url.URL              { return u.parsed }
func (u httpURL) MarshalJSON() ([]byte, error) { return encode(u, u.Text()) }

// NewURL validates supplied text without resolving or connecting to its host.
func NewURL(text string) (URL, error) {
	u, err := url.Parse(text)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || strings.Contains(text, "#") || strings.ContainsAny(text, "\r\n\t ") {
		return nil, fmt.Errorf("HTTP URL requires an absolute destination without credentials, fragments, or whitespace")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("HTTP URL requires http or https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Contains(host, "%") || strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("HTTP URL has an invalid host")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.Unmap().String()
	} else {
		if len(host) > 253 {
			return nil, fmt.Errorf("HTTP URL hostname exceeds its limit")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return nil, fmt.Errorf("HTTP URL has an invalid hostname")
			}
			for _, ch := range []byte(label) {
				if ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
					return nil, fmt.Errorf("HTTP URL has an invalid hostname")
				}
			}
		}
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return nil, fmt.Errorf("HTTP URL has an invalid port")
	}
	u.Host = net.JoinHostPort(host, strconv.FormatUint(n, 10))
	u.Fragment, u.RawFragment = "", ""
	return httpURL{newBase(types.URL(), uint64(len(u.String()))), *u}, nil
}

// NewMethod validates an HTTP method without changing its case.
func NewMethod(method string) (Text, error) { return NewText(types.Method(), method) }

// ClientRequest is an inert description of one outbound HTTP exchange.
type ClientRequest interface {
	Target
	Method() Text
	URL() URL
	Headers() Headers
	Body() Bytes
	clientRequest()
}
type clientRequest struct {
	base
	method  Text
	url     URL
	headers Headers
	body    Bytes
}

func (clientRequest) target()            {}
func (clientRequest) clientRequest()     {}
func (r clientRequest) Method() Text     { return r.method }
func (r clientRequest) URL() URL         { return r.url }
func (r clientRequest) Headers() Headers { return r.headers }
func (r clientRequest) Body() Bytes      { return r.body }
func (r clientRequest) MarshalJSON() ([]byte, error) {
	return encode(r, struct {
		Method  Text    `json:"method"`
		URL     URL     `json:"url"`
		Headers Headers `json:"headers"`
		Body    Bytes   `json:"body"`
	}{r.method, r.url, r.headers, r.body})
}

// NewClientRequest rejects exchanges that require authority overrides or an upgraded connection.
func NewClientRequest(method Text, url URL, headers Headers, body Bytes) (ClientRequest, error) {
	if method == nil || !types.Equal(method.Type(), types.Method()) || url == nil || headers == nil || body == nil {
		return nil, fmt.Errorf("incomplete HTTP client request")
	}
	if method.Text() == "CONNECT" {
		return nil, fmt.Errorf("HTTP CONNECT is not supported")
	}
	for _, item := range headers.Entries().Items() {
		switch item.(Record).Field("name").(Text).Text() {
		case "host", "connection", "upgrade", "content-length", "transfer-encoding", "trailer", "te", "keep-alive", "proxy-connection", "proxy-authorization", "expect":
			return nil, fmt.Errorf("HTTP client header controls authority or protocol framing")
		}
	}
	weight := addWeight(addWeight(method.Weight(), url.Weight()), addWeight(headers.Weight(), body.Weight()))
	return clientRequest{newBase(types.ClientRequest(), weight), method, url, headers, body}, nil
}

func redactedURL(url URL) URL {
	parsed := url.Parsed()
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.ForceQuery = "/", "", "", false
	return httpURL{newBase(types.URL(), uint64(len(parsed.String()))), parsed}
}

func redactedClientRequest(request ClientRequest) ClientRequest {
	url := redactedURL(request.URL())
	headers, body := redactedHeaders(request.Headers()), NewBytes(nil)
	weight := addWeight(addWeight(request.Method().Weight(), url.Weight()), addWeight(headers.Weight(), body.Weight()))
	return clientRequest{newBase(types.ClientRequest(), weight), request.Method(), url, headers, body}
}
