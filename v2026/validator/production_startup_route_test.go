//go:build linux || darwin

// Native producer routes need subscription capability. Qualified HTTP read
// support does not approve a replacement writer route or reinterpret old bytes.
package validator

import (
	"slices"
	"testing"
)

// Both approval construction and the public config validator refuse routes
// that cannot carry native submission subscriptions. Exact supported routes
// remain signed; changing one cannot reuse the old private capsule.
func TestProductionStartupNativeWriterRouteRemainsExplicitWebsocket(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	original := slices.Clone(fixture.cfg.Substrate)
	for _, endpoint := range []string{"http://native.example", "https://native.example", "ftp://native.example", "native.example", "wss:///missing-host", "wss://user:secret@native.example", "wss://native.example#fragment"} {
		cfg := *fixture.cfg
		cfg.Substrate = []string{endpoint}
		if err := validateOwnerRecycleApprovalScope(&cfg); err == nil {
			t.Fatalf("unsupported native writer route gained approval scope: %q", endpoint)
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("unsupported native writer route passed public config admission: %q", endpoint)
		}
	}
	cfg := *fixture.cfg
	cfg.Substrate = []string{"wss://another-native.example"}
	if err := validateOwnerRecycleApprovalScope(&cfg); err != nil {
		t.Fatalf("supported explicit route could not be proposed independently: %v", err)
	}
	if _, err := ownerRecycleProductionApproval(&cfg); err == nil {
		t.Fatal("a different supported route reused original signed approval")
	}
	if !slices.Equal(fixture.cfg.Substrate, original) {
		t.Fatal("native writer admission silently rewrote an original signed endpoint")
	}
}
