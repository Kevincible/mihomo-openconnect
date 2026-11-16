package config

import (
	"fmt"

	C "github.com/metacubex/mihomo/constant"
	RC "github.com/metacubex/mihomo/rules/common"
)

func bindOpenConnectRule(rule C.Rule, proxies map[string]C.Proxy, format string) error {
	openConnect, ok := rule.(*RC.OpenConnect)
	if !ok {
		return nil
	}
	if format != "rules" {
		return fmt.Errorf("OPENCONNECT is only supported in top-level rules")
	}
	return openConnect.Bind(proxies[rule.Adapter()])
}

func openConnectRules(rules []C.Rule) []C.Rule {
	var result []C.Rule
	for _, rule := range rules {
		if rule.RuleType() == C.OpenConnectRule {
			result = append(result, rule)
		}
	}
	return result
}
