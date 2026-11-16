package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/component/dialer"
	C "github.com/metacubex/mihomo/constant"
	oc "github.com/sagernet/sing-openconnect"
)

type OpenConnectOption struct {
	BasicOption
	Name             string                 `proxy:"name"`
	Server           string                 `proxy:"server"`
	Port             int                    `proxy:"port,omitempty"`
	Username         string                 `proxy:"username,omitempty"`
	Password         string                 `proxy:"password,omitempty"`
	AuthGroup        string                 `proxy:"auth-group,omitempty"`
	Cookie           string                 `proxy:"cookie,omitempty"`
	TOTPSecret       string                 `proxy:"totp-secret,omitempty"`
	ServerName       string                 `proxy:"servername,omitempty"`
	CA               string                 `proxy:"ca,omitempty"`
	Certificate      string                 `proxy:"certificate,omitempty"`
	PrivateKey       string                 `proxy:"private-key,omitempty"`
	SkipCertVerify   bool                   `proxy:"skip-cert-verify,omitempty"`
	UDP              *bool                  `proxy:"udp,omitempty"`
	DTLS             *bool                  `proxy:"dtls,omitempty"`
	IPv6             bool                   `proxy:"ipv6,omitempty"`
	MTU              int                    `proxy:"mtu,omitempty"`
	DPDInterval      int                    `proxy:"dpd-interval,omitempty"`
	ReconnectTimeout int                    `proxy:"reconnect-timeout,omitempty"`
	ConnectTimeout   int                    `proxy:"connect-timeout,omitempty"`
	UserAgent        string                 `proxy:"user-agent,omitempty"`
	ReportedOS       string                 `proxy:"reported-os,omitempty"`
	LocalHostname    string                 `proxy:"local-hostname,omitempty"`
	IPStack          IPStackOption          `proxy:"ip-stack,omitempty"`
	AuthHandler      OpenConnectAuthHandler `proxy:"-"`
}

type OpenConnect struct {
	*Base
	option  OpenConnectOption
	client  *oc.Client
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	started bool
	closed  bool
	err     error
	stack   *openConnectStack
	workers sync.WaitGroup
}

var _ ProxyAdapter = (*OpenConnect)(nil)

func NewOpenConnect(option OpenConnectOption) (*OpenConnect, error) {
	server, err := openConnectServerURL(option.Server, option.Port)
	if err != nil {
		return nil, err
	}
	if option.DialerProxy != "" && option.DialerProxy == option.Name {
		return nil, errors.New("openconnect dialer-proxy cannot reference itself")
	}
	if option.MTU != 0 && (option.MTU < 576 || option.MTU > 65535) {
		return nil, errors.New("openconnect mtu must be between 576 and 65535")
	}
	for _, seconds := range []int{option.ConnectTimeout, option.ReconnectTimeout, option.DPDInterval} {
		if seconds < 0 || seconds > 86400 {
			return nil, errors.New("openconnect timeouts must be between 0 and 86400 seconds")
		}
	}
	if option.ConnectTimeout == 0 {
		option.ConnectTimeout = 30
	}
	if option.IPStack.Mode == "" {
		option.IPStack.Mode = ipStackGVisor
	}
	option.IPStack.normalize()
	if err := option.IPStack.validate(); err != nil {
		return nil, err
	}
	// Other stacks need their own interoperability and lifecycle validation.
	if option.IPStack.Mode != ipStackGVisor {
		return nil, errors.New("openconnect currently requires ip-stack mode gvisor")
	}
	o := &OpenConnect{
		Base: NewBase(BaseOption{Name: option.Name, Addr: server.Host, Type: C.OpenConnect,
			ProviderName: option.ProviderName, UDP: option.UDP == nil || *option.UDP,
			TFO: option.TFO, MPTCP: option.MPTCP, Interface: option.Interface,
			RoutingMark: option.RoutingMark, Prefer: option.IPVersion}),
		option: option,
	}
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.dialer = option.NewDialer(o.DialOptions())
	clientOption := oc.ClientOptions{
		Context: o.ctx, Server: server.String(), Flavor: oc.FlavorAnyConnect,
		QueueLength: openConnectPacketQueueLength,
		Username:    option.Username, Password: option.Password, AuthGroup: option.AuthGroup, Cookie: option.Cookie,
		NoUDP: option.DTLS != nil && !*option.DTLS, IPv6Disabled: !option.IPv6,
		MTU: uint32(option.MTU), DPDInterval: time.Duration(option.DPDInterval) * time.Second,
		ReconnectTimeout: time.Duration(option.ReconnectTimeout) * time.Second,
		UserAgent:        option.UserAgent, ReportedOS: option.ReportedOS, LocalHostname: option.LocalHostname,
		Dialer: openConnectDialer{dialer: o.dialer},
		TLSConfig: oc.ClientTLSOptions{
			Config:               &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: option.SkipCertVerify},
			ServerName:           option.ServerName,
			CertificateAuthority: oc.Material{Content: []byte(option.CA)},
			Certificate:          oc.Material{Content: []byte(option.Certificate)},
			Key:                  oc.Material{Content: []byte(option.PrivateKey)},
		},
		OnTunnelConfiguration: o.configurationChanged,
	}
	if option.TOTPSecret != "" {
		clientOption.Token = &oc.TokenOptions{Mode: oc.TokenModeTOTP, Secret: option.TOTPSecret}
	}
	o.client, err = oc.NewClient(clientOption)
	if err != nil {
		o.cancel()
		return nil, fmt.Errorf("openconnect: %w", err)
	}
	return o, nil
}

func openConnectServerURL(server string, port int) (*url.URL, error) {
	if server == "" {
		return nil, errors.New("openconnect server is required")
	}
	if !strings.Contains(server, "://") {
		if ip, err := netip.ParseAddr(server); err == nil && ip.Is6() {
			server = "[" + server + "]"
		}
		server = "https://" + server
	}
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("openconnect server must be an HTTPS host or URL without userinfo, query or fragment")
	}
	if port == 0 {
		port = 443
		if u.Port() != "" {
			port, err = strconv.Atoi(u.Port())
		}
	} else if u.Port() != "" && u.Port() != strconv.Itoa(port) {
		return nil, errors.New("openconnect server URL and port disagree")
	}
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("openconnect port must be between 1 and 65535")
	}
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	return u, nil
}

func (o *OpenConnect) DialContext(ctx context.Context, metadata *C.Metadata) (C.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(o.option.ConnectTimeout)*time.Second)
	defer cancel()
	s, err := o.readyStack(ctx)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	if metadata.Resolved() {
		conn, err = s.device.DialTCP(ctx, "tcp", netip.AddrPort{}, metadata.AddrPort())
	} else {
		conn, err = dialer.NewDialer(dialer.WithNetDialer(ipStackNetDialer{stack: s.device})).DialContext(ctx, "tcp", metadata.RemoteAddress())
	}
	if err != nil {
		return nil, err
	}
	return NewConn(conn, o), nil
}

func (o *OpenConnect) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (C.PacketConn, error) {
	if !o.udp {
		return nil, C.ErrNotSupport
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(o.option.ConnectTimeout)*time.Second)
	defer cancel()
	s, err := o.readyStack(ctx)
	if err != nil {
		return nil, err
	}
	if err = o.Base.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}
	pc, err := s.device.ListenUDP(ctx, "udp", netip.AddrPort{})
	if err != nil {
		return nil, err
	}
	return NewPacketConn(pc, o), nil
}

func (o *OpenConnect) ProxyInfo() C.ProxyInfo {
	info := o.Base.ProxyInfo()
	info.DialerProxy = o.option.DialerProxy
	return info
}

func (o *OpenConnect) IsL3Protocol(*C.Metadata) bool { return true }

func (o *OpenConnect) Close() error {
	o.stop(net.ErrClosed)
	o.workers.Wait()
	return nil
}
