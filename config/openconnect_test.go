//go:build with_gvisor

package config

import (
	"testing"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	C "github.com/metacubex/mihomo/constant"
)

func TestOpenConnectRuleConfig(t *testing.T) {
	raw, err := UnmarshalRawConfig([]byte(`
proxies:
  - {name: VPN, type: openconnect, server: vpn.invalid}
proxy-groups:
  - {name: COMPANY, type: select, proxies: [VPN]}
dns:
  enable: true
  nameserver: [1.1.1.1]
rules:
  - OPENCONNECT,COMPANY
  - DOMAIN,ad.com,REJECT
  - MATCH,DIRECT
`))
	if err != nil {
		t.Fatal(err)
	}
	proxies, _, err := parseProxies(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer proxies["VPN"].Close()
	rules, err := parseRules(raw.Rule, proxies, nil, nil, "rules")
	if err != nil {
		t.Fatal(err)
	}
	dnsRules := openConnectRules(rules)
	if len(dnsRules) != 1 || dnsRules[0] != rules[0] ||
		rules[0].RuleType() != C.OpenConnectRule || rules[0].Adapter() != "COMPANY" {
		t.Fatal("rule and automatic DNS policy were not connected")
	}
	proxies["OTHER"] = adapter.NewProxy(outbound.NewDirect())
	for _, line := range []string{"OPENCONNECT,MISSING", "OPENCONNECT,OTHER", "OPENCONNECT,COMPANY,extra"} {
		if _, err := parseRules([]string{line}, proxies, nil, nil, "rules"); err == nil {
			t.Errorf("accepted invalid configuration: %s", line)
		}
	}
	if _, err := parseRules([]string{"OPENCONNECT,COMPANY"}, proxies, nil, nil, "sub-rules[test]"); err == nil {
		t.Fatal("accepted OPENCONNECT without top-level DNS policy")
	}
}
