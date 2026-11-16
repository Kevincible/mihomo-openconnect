//go:build with_gvisor

package adapter

import (
	C "github.com/metacubex/mihomo/constant"
	"strings"
	"testing"
)

func TestOpenConnectConfig(t *testing.T) {
	p, err := ParseProxy(map[string]any{"type": "openconnect", "name": "OPENCONNECT", "server": "vpn.invalid", "username": "test", "password": "secret-not-for-json", "dtls": false})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Type() != C.OpenConnect || !p.SupportUDP() {
		t.Fatal("OpenConnect registration or UDP capability is incorrect")
	}
	data, err := p.MarshalJSON()
	if err != nil || strings.Contains(string(data), "secret-not-for-json") {
		t.Fatal("credentials exposed")
	}
}
