package common

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/metacubex/mihomo/component/openconnect"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

type OpenConnect struct {
	Base
	adapter string
	target  *openconnect.Target
	failed  atomic.Bool
}

func NewOpenConnect(adapter string) (*OpenConnect, error) {
	if adapter == "" {
		return nil, fmt.Errorf("OPENCONNECT requires a target: OPENCONNECT,proxy-or-group")
	}
	return &OpenConnect{adapter: adapter}, nil
}

func (r *OpenConnect) Bind(proxy C.ProxyAdapter) error {
	target, err := openconnect.NewTarget(proxy)
	if err == nil {
		r.target = target
	}
	return err
}

func (r *OpenConnect) OpenConnectTarget() *openconnect.Target { return r.target }
func (r *OpenConnect) RuleType() C.RuleType                   { return C.OpenConnectRule }
func (r *OpenConnect) Adapter() string                        { return r.adapter }
func (r *OpenConnect) Payload() string                        { return "" }

func (r *OpenConnect) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	policy, _, err := r.target.Policy(context.Background())
	if err != nil {
		if !r.failed.Swap(true) {
			log.Warnln("[OpenConnect] rule %s cannot load split route/DNS policy: %s", r.adapter, err)
		}
		// Until the first configuration arrives we cannot distinguish company
		// traffic from public traffic, so never silently fall through to DIRECT.
		return true, "REJECT"
	}
	r.failed.Store(false)
	domain := policy.MatchDomain(metadata.RuleHost())
	if (!domain || policy.HasExclusions()) && helper.ResolveIP != nil {
		helper.ResolveIP()
	}
	if policy.Excludes(metadata.DstIP) {
		return false, r.adapter
	}
	return domain || policy.MatchIP(metadata.DstIP), r.adapter
}

var _ C.Rule = (*OpenConnect)(nil)
