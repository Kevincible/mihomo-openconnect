package openconnect

import (
	"context"
	"fmt"

	C "github.com/metacubex/mihomo/constant"
)

type Source interface {
	C.ProxyAdapter
	OpenConnectPolicy(context.Context) (*Policy, error)
}

type Target struct {
	proxy C.ProxyAdapter
}

func NewTarget(proxy C.ProxyAdapter) (*Target, error) {
	target := &Target{proxy: proxy}
	_, err := target.source()
	return target, err
}

func (t *Target) Policy(ctx context.Context) (*Policy, Source, error) {
	source, err := t.source()
	if err != nil {
		return nil, nil, err
	}
	policy, err := source.OpenConnectPolicy(ctx)
	return policy, source, err
}

func (t *Target) source() (Source, error) {
	if t == nil || t.proxy == nil {
		return nil, fmt.Errorf("OPENCONNECT rule has no bound proxy")
	}
	sources := make(map[string]Source)
	if err := collectSources(t.proxy, make(map[string]bool), sources); err != nil {
		return nil, err
	}
	if len(sources) != 1 {
		return nil, fmt.Errorf("OPENCONNECT target %q must contain exactly one OpenConnect proxy", t.proxy.Name())
	}
	var result Source
	for _, source := range sources {
		result = source
	}
	return result, nil
}

func collectSources(adapter C.ProxyAdapter, visiting map[string]bool, sources map[string]Source) error {
	if proxy, ok := adapter.(C.Proxy); ok {
		adapter = proxy.Adapter()
	}
	if source, ok := adapter.(Source); ok && adapter.Type() == C.OpenConnect {
		sources[adapter.Name()] = source
		return nil
	}
	group, ok := adapter.(interface{ GetProxies(bool) []C.Proxy })
	if !ok {
		return fmt.Errorf("OPENCONNECT target contains non-OpenConnect proxy %q", adapter.Name())
	}
	if visiting[adapter.Name()] {
		return fmt.Errorf("OPENCONNECT target contains a proxy group cycle at %q", adapter.Name())
	}
	visiting[adapter.Name()] = true
	defer delete(visiting, adapter.Name())
	proxies := group.GetProxies(false)
	if len(proxies) == 0 {
		return fmt.Errorf("OPENCONNECT target contains empty proxy group %q", adapter.Name())
	}
	for _, proxy := range proxies {
		if err := collectSources(proxy, visiting, sources); err != nil {
			return err
		}
	}
	return nil
}
