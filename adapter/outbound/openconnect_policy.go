package outbound

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"time"

	"github.com/metacubex/mihomo/component/openconnect"
	"github.com/metacubex/mihomo/log"
	oc "github.com/sagernet/sing-openconnect"
)

// All fields are protected by OpenConnect.mu. A rejected update leaves current intact.
type openConnectPolicyState struct {
	enabled       bool
	configured    bool
	configuration oc.TunnelConfiguration
	current       *openconnect.Policy
	err           error
}

func (o *OpenConnect) OpenConnectPolicy(ctx context.Context) (*openconnect.Policy, error) {
	o.mu.Lock()
	o.policy.enabled = true
	if o.client.Ready() {
		o.updatePolicyLocked(o.client.TunnelConfiguration())
	}
	policy, err := o.policy.current, o.policy.err
	o.mu.Unlock()
	if policy != nil {
		return policy, nil
	}
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(o.option.ConnectTimeout)*time.Second)
	defer cancel()
	if _, err = o.readyStack(ctx); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.policy.current != nil {
		return o.policy.current, nil
	}
	if o.policy.err != nil {
		return nil, o.policy.err
	}
	return nil, errors.New("OpenConnect tunnel policy is unavailable")
}

func (o *OpenConnect) updatePolicyLocked(config oc.TunnelConfiguration) {
	state := &o.policy
	if !state.enabled {
		return
	}
	previous := state.configuration
	if state.configured && slices.Equal(previous.Routes, config.Routes) &&
		slices.Equal(previous.ExcludedRoutes, config.ExcludedRoutes) &&
		slices.Equal(previous.SplitDNS, config.SplitDNS) && slices.Equal(previous.DNS, config.DNS) {
		return
	}
	state.configuration, state.configured = config, true
	routes := make([]netip.Prefix, 0, len(config.Routes))
	excluded := make([]netip.Prefix, 0, len(config.ExcludedRoutes))
	for _, route := range config.Routes {
		routes = append(routes, route.Prefix)
	}
	for _, route := range config.ExcludedRoutes {
		excluded = append(excluded, route.Prefix)
	}
	policy, err := openconnect.NewPolicy(routes, excluded, config.SplitDNS, config.DNS)
	state.err = err
	if err != nil {
		log.Warnln("[OpenConnect](%s) keeping previous split route/DNS policy: %s", o.Name(), err)
		return
	}
	state.current = policy
}

var _ openconnect.Source = (*OpenConnect)(nil)

func (p *autoCloseProxyAdapter) OpenConnectPolicy(ctx context.Context) (*openconnect.Policy, error) {
	source, ok := p.ProxyAdapter.(openconnect.Source)
	if !ok {
		return nil, errors.New("proxy does not expose OpenConnect split route/DNS configuration")
	}
	return source.OpenConnectPolicy(ctx)
}
