package rules_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/component/openconnect"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/rules"
	RC "github.com/metacubex/mihomo/rules/common"
)

type openConnectSource struct {
	*outbound.Base
	policy *openconnect.Policy
	err    error
}

func (s *openConnectSource) OpenConnectPolicy(context.Context) (*openconnect.Policy, error) {
	return s.policy, s.err
}

func TestOpenConnectRule(t *testing.T) {
	policy, err := openconnect.NewPolicy(
		[]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8"), netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
		[]netip.Prefix{netip.MustParsePrefix("10.20.0.0/16"), netip.MustParsePrefix("fd00:1::/32")},
		[]string{"Corp.Example."}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	source := &openConnectSource{Base: outbound.NewBase(outbound.BaseOption{Name: "VPN", Type: C.OpenConnect}), policy: policy}
	tp, payload, target, params := RC.ParseRulePayload("OPENCONNECT,COMPANY", true)
	rule, err := rules.ParseRule(tp, payload, target, params, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = rule.(*RC.OpenConnect).Bind(adapter.NewProxy(source)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		host, address string
		match         bool
	}{
		{"", "10.1.2.3", true},
		{"", "10.20.1.1", false},
		{"", "fd00:2::1", true},
		{"", "fd00:1::1", false},
		{"", "203.0.113.1", false},
		{"", "2001:db8::1", false},
		{"CORP.EXAMPLE.", "", true},
		{"a.b.corp.example", "203.0.113.1", true},
		{"notcorp.example", "", false},
		{"corp.example.evil.test", "", false},
		{"excluded.corp.example", "10.20.1.1", false},
	} {
		metadata := &C.Metadata{Host: test.host}
		if test.address != "" {
			metadata.DstIP = netip.MustParseAddr(test.address)
		}
		matched, target := rule.Match(metadata, C.RuleMatchHelper{})
		if matched != test.match || target != "COMPANY" {
			t.Errorf("host=%q ip=%s: got %v,%s", test.host, test.address, matched, target)
		}
	}
	metadata := &C.Metadata{Host: "resolved.example"}
	matched, _ := rule.Match(metadata, C.RuleMatchHelper{ResolveIP: func() { metadata.DstIP = netip.MustParseAddr("10.1.2.3") }})
	if !matched {
		t.Fatal("resolved IP did not match split route")
	}
	source.err = errors.New("initial authentication failed")
	if matched, target := rule.Match(&C.Metadata{Host: "unknown.example"}, C.RuleMatchHelper{}); !matched || target != "REJECT" {
		t.Fatal("unavailable initial policy fell through")
	}
	for _, line := range []string{"OPENCONNECT", "OPENCONNECT,", "OPENCONNECT,COMPANY,no-resolve", "OPENCONNECT,VPN,COMPANY"} {
		tp, payload, target, params := RC.ParseRulePayload(line, true)
		if _, err = rules.ParseRule(tp, payload, target, params, nil); err == nil {
			t.Errorf("accepted invalid rule %q", line)
		}
	}
}
