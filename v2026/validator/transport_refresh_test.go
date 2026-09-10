package validator

import (
	"context"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

func TestTunnelTransportReadsCurrentJwtForEveryTunnel(t *testing.T) {
	currentJwt := "first"
	strategy := connect.NewClientStrategyWithDefaults(context.Background())
	defer strategy.Close()
	transport := NewTunnelTransport(
		context.Background(),
		strategy,
		TunnelTransportConfig{ByClientJwt: func() string { return currentJwt }},
	)

	got, err := transport.currentByClientJwt()
	if err != nil || got != "first" {
		t.Fatalf("initial JWT = %q, err=%v", got, err)
	}
	currentJwt = "refreshed"
	got, err = transport.currentByClientJwt()
	if err != nil || got != "refreshed" {
		t.Fatalf("refreshed JWT = %q, err=%v", got, err)
	}
}

func TestTunnelTransportRejectsMissingJwt(t *testing.T) {
	transport := &TunnelTransport{}
	if _, err := transport.currentByClientJwt(); err == nil {
		t.Fatal("missing JWT source was accepted")
	}
	transport.cfg.ByClientJwt = func() string { return "" }
	if _, err := transport.currentByClientJwt(); err == nil {
		t.Fatal("empty JWT was accepted")
	}
}
