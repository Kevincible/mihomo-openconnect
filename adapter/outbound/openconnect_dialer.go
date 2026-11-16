package outbound

import (
	"context"
	"net"
	"net/netip"

	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	M "github.com/sagernet/sing/common/metadata"
)

type openConnectDialer struct{ dialer C.Dialer }

func (d openConnectDialer) DialContext(ctx context.Context, network string, address M.Socksaddr) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, address.String())
}

func (d openConnectDialer) ListenPacket(ctx context.Context, address M.Socksaddr) (net.PacketConn, error) {
	ip := address.Addr
	if !ip.IsValid() {
		var err error
		ip, err = resolver.ResolveIPWithResolver(ctx, address.Fqdn, resolver.ProxyServerHostResolver)
		if err != nil {
			return nil, err
		}
	}
	return d.dialer.ListenPacket(ctx, "udp", "", netip.AddrPortFrom(ip, address.Port))
}
