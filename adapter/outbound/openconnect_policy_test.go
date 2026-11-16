package outbound

import (
	"net/netip"
	"testing"

	oc "github.com/sagernet/sing-openconnect"
)

func TestOpenConnectPolicyUpdates(t *testing.T) {
	o := &OpenConnect{Base: NewBase(BaseOption{Name: "VPN"})}
	config := oc.TunnelConfiguration{
		Routes:        []oc.TunnelRoute{{Prefix: netip.MustParsePrefix("10.0.0.0/8")}},
		SplitDNS:      []string{"corp.example"},
		SearchDomains: []string{"search.example"},
		DNS:           []netip.Addr{netip.MustParseAddr("10.0.0.53")},
	}
	o.updatePolicyLocked(config)
	if o.policy.current != nil {
		t.Fatal("ordinary outbound unexpectedly enabled dynamic policy")
	}
	o.policy.enabled = true
	o.updatePolicyLocked(config)
	first := o.policy.current
	if first == nil || !first.MatchDomain("corp.example") || first.MatchDomain("search.example") {
		t.Fatal("split DNS and search domains were not kept separate")
	}
	o.updatePolicyLocked(config)
	if o.policy.current != first {
		t.Fatal("unchanged configuration replaced policy")
	}
	config.SplitDNS = []string{"*."}
	o.updatePolicyLocked(config)
	if o.policy.err == nil || o.policy.current != first {
		t.Fatal("invalid update replaced last valid policy")
	}
	config.SplitDNS = []string{"new.example"}
	config.Routes = []oc.TunnelRoute{{Prefix: netip.MustParsePrefix("192.168.0.0/16")}}
	o.updatePolicyLocked(config)
	if o.policy.err != nil || o.policy.current == first || !o.policy.current.MatchDomain("new.example") ||
		o.policy.current.MatchDomain("corp.example") || o.policy.current.MatchIP(netip.MustParseAddr("10.1.2.3")) ||
		!o.policy.current.MatchIP(netip.MustParseAddr("192.168.1.1")) {
		t.Fatal("updated policy did not replace routes and domains together")
	}
}
