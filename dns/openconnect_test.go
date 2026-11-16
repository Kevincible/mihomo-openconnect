package dns_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/component/openconnect"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/dns"
	RC "github.com/metacubex/mihomo/rules/common"
	"github.com/metacubex/mihomo/rules/wrapper"

	D "github.com/miekg/dns"
)

type openConnectDNSProxy struct {
	*outbound.Base
	mu      sync.Mutex
	policy  *openconnect.Policy
	err     error
	queries atomic.Int32
}

func (p *openConnectDNSProxy) OpenConnectPolicy(context.Context) (*openconnect.Policy, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.policy, p.err
}

func (p *openConnectDNSProxy) DialContext(_ context.Context, metadata *C.Metadata) (C.Conn, error) {
	if metadata.DstPort != 53 || !metadata.DstIP.IsValid() {
		return nil, errors.New("DNS query did not target the supplied server")
	}
	local, remote := net.Pipe()
	go p.answer(remote, metadata.DstIP)
	return outbound.NewConn(local, p), nil
}

func (p *openConnectDNSProxy) answer(connection net.Conn, server netip.Addr) {
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	client := &D.Conn{Conn: connection}
	query, err := client.ReadMsg()
	if err != nil {
		return
	}
	p.queries.Add(1)
	answer := new(D.Msg).SetReply(query)
	answer.Answer = []D.RR{&D.A{Hdr: D.RR_Header{Name: query.Question[0].Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 300}, A: server.AsSlice()}}
	client.WriteMsg(answer)
}

func (p *openConnectDNSProxy) configure(t *testing.T, server string) {
	t.Helper()
	var servers []netip.Addr
	if server != "" {
		servers = append(servers, netip.MustParseAddr(server))
	}
	policy, err := openconnect.NewPolicy(nil, nil, []string{"corp.example"}, servers)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.policy = policy
	p.mu.Unlock()
}

func TestOpenConnectDNS(t *testing.T) {
	proxy := &openConnectDNSProxy{Base: outbound.NewBase(outbound.BaseOption{Name: "VPN", Type: C.OpenConnect})}
	proxy.configure(t, "192.0.2.53")
	rule, _ := RC.NewOpenConnect("COMPANY")
	if err := rule.Bind(adapter.NewProxy(proxy)); err != nil {
		t.Fatal(err)
	}
	wrapped := wrapper.NewRuleWrapper(rule)
	config := dns.Config{
		Main:             []dns.NameServer{{Net: "rcode", Addr: "name_error"}},
		Default:          []dns.NameServer{{Net: "rcode", Addr: "success"}},
		OpenConnectRules: []C.Rule{wrapped},
	}
	resolvers := dns.NewResolver(config)
	query := new(D.Msg).SetQuestion("internal.corp.example.", D.TypeA)
	for _, server := range []string{"192.0.2.53", "192.0.2.54"} {
		proxy.configure(t, server)
		for i := 0; i < 2; i++ {
			answer, err := resolvers.ExchangeContext(context.Background(), query.Copy())
			if err != nil || len(answer.Answer) != 1 || answer.Answer[0].(*D.A).A.String() != server {
				t.Fatalf("split DNS server %s: answer=%v error=%v", server, answer, err)
			}
		}
	}
	if proxy.queries.Load() != 2 {
		t.Fatalf("DNS cache was not isolated by policy: %d requests", proxy.queries.Load())
	}
	answer, err := resolvers.ExchangeContext(context.Background(), new(D.Msg).SetQuestion("public.example.", D.TypeA))
	if err != nil || answer.Rcode != D.RcodeNameError {
		t.Fatal("public domain did not use ordinary DNS")
	}
	answer, err = resolvers.ProxyResolver.ExchangeContext(context.Background(), query.Copy())
	if err != nil || len(answer.Answer) != 0 || answer.Rcode != D.RcodeSuccess {
		t.Fatal("gateway resolver inherited split DNS policy")
	}
	proxy.configure(t, "")
	if _, err = resolvers.ExchangeContext(context.Background(), query.Copy()); err == nil {
		t.Fatal("split DNS without a server fell back to public DNS")
	}
	wrapped.SetDisabled(true)
	answer, err = resolvers.ExchangeContext(context.Background(), query.Copy())
	if err != nil || answer.Rcode != D.RcodeNameError {
		t.Fatal("disabled rule still applied split DNS")
	}
	wrapped.SetDisabled(false)
	proxy.mu.Lock()
	proxy.err = errors.New("no initial configuration")
	proxy.mu.Unlock()
	if _, err = resolvers.ExchangeContext(context.Background(), query.Copy()); err == nil {
		t.Fatal("unavailable initial policy fell back to public DNS")
	}
	config.Policy = []dns.Policy{{Domain: "+.corp.example", NameServers: []dns.NameServer{{Net: "rcode", Addr: "refused"}}}}
	answer, err = dns.NewResolver(config).ExchangeContext(context.Background(), query.Copy())
	if err != nil || answer.Rcode != D.RcodeRefused {
		t.Fatal("explicit nameserver-policy did not override automatic split DNS")
	}
}
