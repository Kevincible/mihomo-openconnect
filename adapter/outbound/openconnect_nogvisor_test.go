//go:build !with_gvisor

package outbound

import (
	"strings"
	"testing"
)

func TestOpenConnectRequiresGVisor(t *testing.T) {
	_, err := NewOpenConnect(OpenConnectOption{Name: "OPENCONNECT", Server: "vpn.invalid"})
	if err == nil || !strings.Contains(err.Error(), "with_gvisor") {
		t.Fatalf("expected a missing build tag error, got %v", err)
	}
}
