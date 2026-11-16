//go:build with_gvisor

package outbound

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/proxydialer"
	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/dns"
	RC "github.com/metacubex/mihomo/rules/common"
	D "github.com/miekg/dns"
)

type openConnectFixture struct {
	name         string
	port         int
	ca, password string
}

func ocDocker(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

func newOpenConnectFixture(t *testing.T, publishUDP bool) openConnectFixture {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	f := openConnectFixture{name: fmt.Sprintf("mihomo-oc-%d", port), port: port, password: hex.EncodeToString(secret)}
	args := []string{"run", "-d", "--name", f.name, "--cap-add=NET_ADMIN", "--device=/dev/net/tun", "-e", fmt.Sprintf("VPN_PORT=%d", port), "-e", "TEST_PASSWORD=" + f.password, "-p", fmt.Sprintf("127.0.0.1:%d:%d/tcp", port, port)}
	if publishUDP {
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d/udp", port, port))
	}
	args = append(args, "mihomo-openconnect-ocserv:test")
	ocDocker(t, args...)
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(ocDocker(t, "logs", f.name))
		}
		ocDocker(t, "rm", "-f", f.name)
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		// Docker can accept TCP before ocserv starts. Wait for the actual TLS server.
		c, err := tls.DialWithDialer(&net.Dialer{Timeout: 200 * time.Millisecond}, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &tls.Config{InsecureSkipVerify: true})
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ocserv did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	cert := filepath.Join(t.TempDir(), "ca.pem")
	ocDocker(t, "cp", f.name+":/etc/ocserv/server-cert.pem", cert)
	pem, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	f.ca = string(pem)
	return f
}

func (f openConnectFixture) option() OpenConnectOption {
	return OpenConnectOption{Name: "OPENCONNECT", Server: "127.0.0.1", Port: f.port, Username: "test", Password: f.password, CA: f.ca, DPDInterval: 2, ConnectTimeout: 10, ReconnectTimeout: 60}
}

func ocNew(t *testing.T, option OpenConnectOption) *OpenConnect {
	t.Helper()
	o, err := NewOpenConnect(option)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := o.Close(); err != nil {
			t.Error(err)
		}
	})
	return o
}

func ocTCP(o *OpenConnect, address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	metadata := &C.Metadata{}
	if err := metadata.SetRemoteAddress(net.JoinHostPort(address, "8000")); err != nil {
		return err
	}
	c, err := o.DialContext(ctx, metadata)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(4 * time.Second))
	payload := bytes.Repeat([]byte("openconnect TCP "), 500)
	if _, err = c.Write(payload); err != nil {
		return err
	}
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(c, got); err != nil {
		return err
	}
	if !bytes.Equal(payload, got) {
		return errors.New("TCP payload mismatch")
	}
	return nil
}

func ocUDP(o *OpenConnect, address string, dns bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	port := uint16(8001)
	payload := []byte("openconnect UDP")
	if dns {
		port = 5353
		m := new(D.Msg).SetQuestion("internal.corp.test.", D.TypeA)
		payload, _ = m.Pack()
	}
	meta := &C.Metadata{DstIP: netip.MustParseAddr(address), DstPort: port}
	pc, err := o.ListenPacketContext(ctx, meta)
	if err != nil {
		return err
	}
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err = pc.WriteTo(payload, net.UDPAddrFromAddrPort(meta.AddrPort())); err != nil {
		return err
	}
	buf := make([]byte, 2048)
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		return err
	}
	if !dns {
		if !bytes.Equal(buf[:n], payload) {
			return errors.New("UDP payload mismatch")
		}
	} else {
		var answer D.Msg
		if err = answer.Unpack(buf[:n]); err != nil {
			return err
		}
		if len(answer.Answer) != 1 || !strings.Contains(answer.Answer[0].String(), "192.168.77.1") {
			return errors.New("unexpected DNS answer")
		}
	}
	return nil
}

func ocTraffic(t *testing.T, o *OpenConnect, address string) {
	t.Helper()
	if err := ocTCP(o, address); err != nil {
		t.Fatal(err)
	}
	if err := ocUDP(o, address, false); err != nil {
		t.Fatal(err)
	}
	if err := ocUDP(o, address, true); err != nil {
		t.Fatal(err)
	}
}

func ocEventually(t *testing.T, o *OpenConnect, address string) {
	t.Helper()
	deadline := time.Now().Add(70 * time.Second)
	for {
		err := ocTCP(o, address)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not recover: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func TestOpenConnectIntegration(t *testing.T) {
	if os.Getenv("OPENCONNECT_INTEGRATION") != "1" {
		t.Skip("set OPENCONNECT_INTEGRATION=1 and build tests/openconnect/ocserv")
	}
	t.Run("SplitPolicy", testOpenConnectSplitPolicy)
	t.Run("TLS", func(t *testing.T) {
		f := newOpenConnectFixture(t, true)
		option := f.option()
		disabled := false
		option.DTLS = &disabled
		o := ocNew(t, option)
		ocTraffic(t, o, "192.168.77.1")
		if strings.Contains(strings.ToLower(o.client.ActiveTransport()), "dtls") {
			t.Fatal("DTLS enabled despite dtls: false")
		}
	})
	t.Run("DTLSUnavailable", func(t *testing.T) {
		f := newOpenConnectFixture(t, false)
		o := ocNew(t, f.option())
		ocTraffic(t, o, "192.168.77.1")
		if strings.Contains(strings.ToLower(o.client.ActiveTransport()), "dtls") {
			t.Fatal("unexpected DTLS transport")
		}
	})
	t.Run("Lifecycle", func(t *testing.T) {
		f := newOpenConnectFixture(t, true)
		o := ocNew(t, f.option())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := o.DialContext(ctx, &C.Metadata{DstIP: netip.MustParseAddr("192.168.77.1"), DstPort: 8000}); err == nil {
			t.Fatal("canceled dial succeeded")
		}
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := ocTCP(o, "192.168.77.1"); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		ocTraffic(t, o, "192.168.77.1")
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(strings.ToLower(o.client.ActiveTransport()), "dtls") {
			if time.Now().After(deadline) {
				t.Fatalf("DTLS not established: %s", o.client.ActiveTransport())
			}
			time.Sleep(100 * time.Millisecond)
		}
		o.mu.Lock()
		first := o.stack
		o.mu.Unlock()
		// A protocol reconnect with unchanged addresses and MTU must preserve the stack.
		o.client.RestartSession()
		ocEventually(t, o, "192.168.77.1")
		o.mu.Lock()
		same := first == o.stack
		o.mu.Unlock()
		if !same {
			t.Fatal("unchanged configuration replaced the stack")
		}
		ocDocker(t, "pause", f.name)
		time.Sleep(6 * time.Second)
		ocDocker(t, "unpause", f.name)
		ocEventually(t, o, "192.168.77.1")
		ocDocker(t, "restart", f.name)
		ocEventually(t, o, "192.168.77.1")
		old, err := o.DialContext(context.Background(), &C.Metadata{DstIP: netip.MustParseAddr("192.168.77.1"), DstPort: 8000})
		if err != nil {
			t.Fatal(err)
		}
		defer old.Close()
		ocDocker(t, "exec", f.name, "sed", "-i", "s/192.168.77/192.168.78/g; s/mtu = 1400/mtu = 1300/", "/etc/ocserv/ocserv.conf")
		ocDocker(t, "restart", f.name)
		ocEventually(t, o, "192.168.78.1")
		ocTraffic(t, o, "192.168.78.1")
		o.mu.Lock()
		changed := first != o.stack
		mtu := o.stack.mtu
		o.mu.Unlock()
		if !changed || mtu > 1300 {
			t.Fatalf("configuration not applied: changed=%v mtu=%d", changed, mtu)
		}
		old.SetReadDeadline(time.Now().Add(time.Second))
		_, err = old.Read(make([]byte, 1))
		if err == nil {
			t.Fatal("old connection survived address change")
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			t.Fatal("old connection was left waiting instead of being closed")
		}
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); o.Close() }()
		}
		wg.Wait()
		if err := ocTCP(o, "192.168.78.1"); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("closed outbound: %v", err)
		}
	})
	t.Run("ProxyChain", func(t *testing.T) {
		f := newOpenConnectFixture(t, true)
		handler := &ocConnectProxy{target: net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port))}
		server := httptest.NewServer(handler)
		defer server.Close()
		host, portString, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
		port, _ := strconv.Atoi(portString)
		proxy, err := NewHttp(HttpOption{Name: "GATEWAY", Server: host, Port: port})
		if err != nil {
			t.Fatal(err)
		}
		defer proxy.Close()
		option := f.option()
		option.DialerForAPI = proxydialer.New(proxy, false)
		o := ocNew(t, option)
		ocTraffic(t, o, "192.168.77.1")
		if handler.connections.Load() == 0 {
			t.Fatal("gateway bypassed the proxy chain")
		}
	})
	t.Run("TOTP", func(t *testing.T) {
		f := newOpenConnectFixture(t, true)
		secret := make([]byte, 20)
		if _, err := rand.Read(secret); err != nil {
			t.Fatal(err)
		}
		oath := filepath.Join(t.TempDir(), "users.oath")
		if err := os.WriteFile(oath, []byte("HOTP/T30\ttest\t-\t"+hex.EncodeToString(secret)+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		ocDocker(t, "cp", oath, f.name+":/etc/ocserv/users.oath")
		ocDocker(t, "exec", f.name, "chmod", "600", "/etc/ocserv/users.oath")
		ocDocker(t, "exec", f.name, "sed", "-i", "s@passwd=/etc/ocserv/ocpasswd@passwd=/etc/ocserv/ocpasswd,otp=/etc/ocserv/users.oath@", "/etc/ocserv/ocserv.conf")
		ocDocker(t, "restart", f.name)
		option := f.option()
		option.TOTPSecret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
		ocTraffic(t, ocNew(t, option), "192.168.77.1")
	})
	t.Run("AuthenticationAndTrust", func(t *testing.T) {
		f := newOpenConnectFixture(t, true)
		bad := f.option()
		bad.Password = "incorrect"
		o := ocNew(t, bad)
		if err := ocTCP(o, "192.168.77.1"); err == nil {
			t.Fatal("bad credentials accepted")
		}
		untrusted := f.option()
		untrusted.CA = ""
		if err := ocTCP(ocNew(t, untrusted), "192.168.77.1"); err == nil {
			t.Fatal("untrusted certificate accepted")
		}
		host := f.option()
		host.Password = ""
		host.AuthHandler = ocTestAuthenticator{password: f.password}
		ocTraffic(t, ocNew(t, host), "192.168.77.1")
		// Failure in one outbound must not interfere with a separate valid session.
		ocTraffic(t, ocNew(t, f.option()), "192.168.77.1")
	})
}

func testOpenConnectSplitPolicy(t *testing.T) {
	f := newOpenConnectFixture(t, true)
	o := ocNew(t, f.option())
	proxy := NewAutoCloseProxyAdapter(o)
	defer proxy.Close()
	rule, err := RC.NewOpenConnect(o.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err = rule.Bind(proxy); err != nil {
		t.Fatal(err)
	}
	r := dns.NewResolver(dns.Config{
		Main:             []dns.NameServer{{Net: "rcode", Addr: "name_error"}},
		Default:          []dns.NameServer{{Net: "rcode", Addr: "name_error"}},
		OpenConnectRules: []C.Rule{rule},
	})
	previousDefault, previousProxy := resolver.DefaultResolver, resolver.ProxyServerHostResolver
	resolver.DefaultResolver, resolver.ProxyServerHostResolver = r.Resolver, r.ProxyResolver
	t.Cleanup(func() {
		resolver.DefaultResolver, resolver.ProxyServerHostResolver = previousDefault, previousProxy
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	query := new(D.Msg).SetQuestion("internal.corp.test.", D.TypeA)
	answer, err := r.ExchangeContext(ctx, query.Copy())
	if err != nil || len(answer.Answer) != 1 || !strings.Contains(answer.Answer[0].String(), "192.168.77.1") {
		t.Fatalf("automatic split DNS: answer=%v error=%v", answer, err)
	}
	if err := ocTCP(o, "internal.corp.test"); err != nil {
		t.Fatalf("TCP connection using split DNS: %v", err)
	}
	for address, want := range map[string]bool{"192.168.77.1": true, "192.168.77.200": false, "203.0.113.1": false} {
		if matched, _ := rule.Match(&C.Metadata{DstIP: netip.MustParseAddr(address)}, C.RuleMatchHelper{}); matched != want {
			t.Fatalf("split route %s: got %v, want %v", address, matched, want)
		}
	}
	o.mu.Lock()
	firstStack := o.stack
	o.mu.Unlock()
	ocDocker(t, "exec", f.name, "sed", "-i", "s/split-dns = corp.test/split-dns = new.example/; s@route = 192.168.77.0/255.255.255.0@route = 192.168.77.128/255.255.255.128@", "/etc/ocserv/ocserv.conf")
	ocDocker(t, "restart", f.name)
	o.client.RestartSession()
	ocEventually(t, o, "192.168.77.1")
	policy, err := o.OpenConnectPolicy(ctx)
	if err != nil || policy.MatchDomain("corp.test") || !policy.MatchDomain("new.example") ||
		policy.MatchIP(netip.MustParseAddr("192.168.77.1")) || !policy.MatchIP(netip.MustParseAddr("192.168.77.150")) {
		t.Fatalf("route/DNS-only update was not applied: %v", err)
	}
	o.mu.Lock()
	sameStack := firstStack == o.stack
	o.mu.Unlock()
	if !sameStack {
		t.Fatal("route/DNS-only update replaced the IP stack")
	}
	answer, err = r.ExchangeContext(ctx, query.Copy())
	if err != nil || answer.Rcode != D.RcodeNameError {
		t.Fatal("old split DNS domain retained its cached VPN answer")
	}
	answer, err = r.ExchangeContext(ctx, new(D.Msg).SetQuestion("internal.new.example.", D.TypeA))
	if err != nil || len(answer.Answer) != 1 {
		t.Fatalf("new split DNS domain: answer=%v error=%v", answer, err)
	}
}

type ocTestAuthenticator struct{ password string }

func (a ocTestAuthenticator) AuthenticateOpenConnect(_ context.Context, challenge OpenConnectAuthChallenge) (map[string]string, error) {
	values := make(map[string]string)
	for _, f := range challenge.Fields {
		if f.Kind == "password" {
			values[f.Key] = a.password
		} else {
			values[f.Key] = f.Value
		}
	}
	return values, nil
}

type ocConnectProxy struct {
	target      string
	connections atomic.Int64
}

func (p *ocConnectProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect || r.Host != p.target {
		http.Error(w, "unexpected destination", 403)
		return
	}
	upstream, err := net.DialTimeout("tcp", p.target, 5*time.Second)
	if err != nil {
		http.Error(w, "connect failed", 502)
		return
	}
	defer upstream.Close()
	client, buffer, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	p.connections.Add(1)
	_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	_ = buffer.Flush()
	done := make(chan struct{})
	go func() { defer close(done); io.Copy(upstream, buffer); upstream.Close() }()
	io.Copy(client, upstream)
	client.Close()
	<-done
}
