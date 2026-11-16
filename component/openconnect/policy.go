package openconnect

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/metacubex/mihomo/component/trie"

	"go4.org/netipx"
	"golang.org/x/net/idna"
)

// Policy is an immutable snapshot shared by routing and DNS.
type Policy struct {
	routes   *netipx.IPSet
	excluded *netipx.IPSet
	domains  *trie.DomainSet
	servers  []netip.Addr
}

func NewPolicy(routes, excluded []netip.Prefix, domains []string, servers []netip.Addr) (*Policy, error) {
	var includeSet, excludeSet netipx.IPSetBuilder
	for _, prefix := range routes {
		if !prefix.IsValid() {
			return nil, fmt.Errorf("invalid OpenConnect route: %s", prefix)
		}
		// The library also synthesizes /0 routes; its public API cannot identify
		// whether the server actually sent them. OPENCONNECT only imports split routes.
		if prefix.Bits() != 0 {
			includeSet.AddPrefix(prefix.Masked())
		}
	}
	for _, prefix := range excluded {
		excludeSet.AddPrefix(prefix.Masked())
	}
	include, err := includeSet.IPSet()
	if err != nil {
		return nil, err
	}
	exclude, err := excludeSet.IPSet()
	if err != nil {
		return nil, err
	}

	var domainSet trie.DomainSetBuilder
	for _, value := range domains {
		names := strings.Fields(strings.ReplaceAll(value, ",", " "))
		if len(names) == 0 {
			return nil, fmt.Errorf("empty OpenConnect split DNS domain")
		}
		for _, domain := range names {
			domain = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(domain, "*."), "."), ".")
			domain, err = idna.Lookup.ToASCII(domain)
			if err != nil || domain == "" || strings.ContainsAny(domain, "*+:/\\") {
				return nil, fmt.Errorf("invalid OpenConnect split DNS domain: %q", value)
			}
			if err = domainSet.Insert("+." + strings.ToLower(domain)); err != nil {
				return nil, err
			}
		}
	}
	for _, server := range servers {
		if !server.IsValid() || server.IsUnspecified() || server.IsMulticast() || server.Zone() != "" {
			return nil, fmt.Errorf("invalid OpenConnect DNS server: %s", server)
		}
	}
	return &Policy{routes: include, excluded: exclude, domains: domainSet.Build(), servers: slices.Clone(servers)}, nil
}

func (p *Policy) MatchDomain(domain string) bool {
	return p.domains.Has(strings.ToLower(strings.TrimSuffix(domain, ".")))
}

func (p *Policy) MatchIP(address netip.Addr) bool {
	address = address.Unmap().WithZone("")
	return p.routes.Contains(address) && !p.excluded.Contains(address)
}

func (p *Policy) Excludes(address netip.Addr) bool {
	return p.excluded.Contains(address.Unmap().WithZone(""))
}

func (p *Policy) HasExclusions() bool {
	return len(p.excluded.Ranges()) != 0
}

func (p *Policy) DNSServers() []netip.Addr {
	return slices.Clone(p.servers)
}
