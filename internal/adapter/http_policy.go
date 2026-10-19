package adapter

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// HTTPPolicy contains explicit method, origin, and connection-address grants.
type HTTPPolicy interface {
	httpPolicy()
	addresses(string, string) (map[netip.Addr]struct{}, bool)
}
type httpGrantKey struct{ method, origin string }
type httpPolicy struct {
	grants map[httpGrantKey]map[netip.Addr]struct{}
}

func (httpPolicy) httpPolicy() {}
func (p httpPolicy) addresses(method, origin string) (map[netip.Addr]struct{}, bool) {
	addresses, ok := p.grants[httpGrantKey{method, origin}]
	return addresses, ok
}

// NewHTTPPolicy validates repeatable METHOD,ORIGIN,ADDRESS grants.
func NewHTTPPolicy(grants []string) (HTTPPolicy, error) {
	policy := httpPolicy{grants: map[httpGrantKey]map[netip.Addr]struct{}{}}
	for index, grant := range grants {
		fields := strings.Split(grant, ",")
		invalid := func() (HTTPPolicy, error) {
			return nil, fmt.Errorf("HTTP grant %d requires METHOD,ORIGIN,ADDRESS without whitespace", index)
		}
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" || fields[2] == "" || strings.IndexFunc(grant, unicode.IsSpace) >= 0 {
			return invalid()
		}
		if _, err := value.CanonicalHeaderName(fields[0]); err != nil {
			return invalid()
		}
		origin, err := url.Parse(fields[1])
		if err != nil || origin.User != nil || origin.Opaque != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.RawFragment != "" || origin.Path != "" && origin.Path != "/" || origin.RawPath != "" {
			return invalid()
		}
		canonical, err := value.NewURL(fields[1])
		if err != nil {
			return invalid()
		}
		address, err := netip.ParseAddr(fields[2])
		if err != nil || address.Zone() != "" {
			return invalid()
		}
		key := httpGrantKey{fields[0], canonical.Origin()}
		if policy.grants[key] == nil {
			policy.grants[key] = map[netip.Addr]struct{}{}
		}
		policy.grants[key][address.Unmap()] = struct{}{}
	}
	return policy, nil
}
