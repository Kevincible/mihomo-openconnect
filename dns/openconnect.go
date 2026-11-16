package dns

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/metacubex/mihomo/component/openconnect"
	C "github.com/metacubex/mihomo/constant"

	D "github.com/miekg/dns"
)

type openConnectDNSPolicy struct {
	rule   C.Rule
	target *openconnect.Target
	mu     sync.Mutex
	policy *openconnect.Policy
	client *Resolver
}

func newOpenConnectDNSPolicies(rules []C.Rule) []*openConnectDNSPolicy {
	var policies []*openConnectDNSPolicy
	for _, rule := range rules {
		inner := rule
		if wrapper, ok := rule.(C.RuleWrapper); ok {
			inner = wrapper.Unwrap()
		}
		if source, ok := inner.(interface{ OpenConnectTarget() *openconnect.Target }); ok {
			policies = append(policies, &openConnectDNSPolicy{rule: rule, target: source.OpenConnectTarget()})
		}
	}
	return policies
}

func (r *Resolver) exchangeOpenConnect(ctx context.Context, message *D.Msg) (*D.Msg, bool, error) {
	if len(r.openConnect) == 0 || len(r.matchPolicy(message)) != 0 {
		return nil, false, nil
	}
	domain := msgToDomain(message)
	for _, entry := range r.openConnect {
		if wrapper, ok := entry.rule.(C.RuleWrapper); ok && wrapper.IsDisabled() {
			continue
		}
		policy, source, err := entry.target.Policy(ctx)
		if err != nil {
			return nil, true, fmt.Errorf("OPENCONNECT DNS policy %s: %w", entry.rule.Adapter(), err)
		}
		if !policy.MatchDomain(domain) {
			continue
		}
		client, err := entry.resolver(policy, source, r)
		if err != nil {
			return nil, true, err
		}
		answer, err := client.ExchangeContext(ctx, message)
		return answer, true, err
	}
	return nil, false, nil
}

func (p *openConnectDNSPolicy) resolver(policy *openconnect.Policy, source openconnect.Source, parent *Resolver) (*Resolver, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if policy == p.policy {
		return p.client, nil
	}
	servers := policy.DNSServers()
	if len(servers) == 0 {
		return nil, fmt.Errorf("OpenConnect proxy %q supplied split DNS domains without a DNS server", source.Name())
	}
	clients := make([]dnsClient, 0, len(servers))
	network := "udp"
	if !source.SupportUDP() {
		network = "tcp"
	}
	for _, server := range servers {
		clients = append(clients, newClient(net.JoinHostPort(server.String(), "53"), nil, network, nil, source, ""))
	}
	// Keep these answers out of the ordinary resolver's cache. A new policy
	// creates a new cache, so replies from a previous VPN configuration cannot win.
	p.client = &Resolver{
		ipv6: parent.ipv6, ipv6Timeout: parent.ipv6Timeout,
		main: clients, cache: parent.openConnectCache.newCache(),
	}
	p.policy = policy
	return p.client, nil
}

func (p *openConnectDNSPolicy) clearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.client.ClearCache()
}
