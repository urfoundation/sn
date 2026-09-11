// transport_encryption_test.go pins the per-attempt validator client's
// encryption responder capability used by provider return sequences.
package validator

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// Provider return sequences may initiate TLS toward the derived validator
// client. Pin both the policy and the concrete responder material: the old
// default-off factory had neither and left each handshake pending for 60s.
func TestTunnelClientSettingsEnableProviderReplyEncryption(t *testing.T) {
	clientSettings := newTunnelClientSettings()
	if clientSettings.EncryptionSettings == nil {
		t.Fatal("tunnel client has no encryption settings")
	}
	if clientSettings.EncryptionSettings.Mode != connect.EncryptionModeOpportunistic {
		t.Fatalf("tunnel encryption mode = %v, want opportunistic", clientSettings.EncryptionSettings.Mode)
	}
	if !clientSettings.ClientKeyRegistrationRequired {
		t.Fatal("tunnel client does not require processed identity-key registration")
	}

	ctx, cancel := context.WithCancel(context.Background())
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pack":"","error":null}`))
	}))
	strategy := connect.NewClientStrategyWithDefaults(ctx)
	control := connect.NewApiOutOfBandControl(ctx, strategy, "synthetic-tunnel-token", endpoint.URL)
	client := connect.NewClient(
		ctx,
		connect.NewId(),
		control,
		clientSettings,
	)
	t.Cleanup(func() {
		cancel()
		joinCtx, joinCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer joinCancel()
		if err := client.CloseAndWait(joinCtx); err != nil {
			t.Errorf("close tunnel capability client: %v", err)
		}
		if err := control.CloseAndWait(joinCtx); err != nil {
			t.Errorf("close tunnel capability control: %v", err)
		}
		strategy.Close()
		endpoint.Close()
	})

	encryptionManager := client.EncryptionSessionManager()
	if encryptionManager.Settings().Mode != connect.EncryptionModeOpportunistic {
		t.Fatalf("client encryption mode = %v, want opportunistic", encryptionManager.Settings().Mode)
	}
	if len(encryptionManager.ProvideTlsCertificatePem()) == 0 {
		t.Fatal("opportunistic tunnel client has no TLS responder certificate")
	}
	if client.ClientKeyManager() == nil || len(client.ClientKeyManager().PublicKey()) != ed25519.PublicKeySize {
		t.Fatal("opportunistic tunnel client has no identity key for its TLS proof")
	}
	registrationCtx, registrationCancel := context.WithTimeout(ctx, 10*time.Second)
	defer registrationCancel()
	if err := client.ClientKeyManager().WaitForRegistration(registrationCtx); err != nil {
		t.Fatalf("tunnel key did not receive processed registration: %v", err)
	}
}

// Every PostVerify owns a distinct derived client graph. Its settings must be
// distinct too, so one attempt cannot change another attempt's TLS policy.
func TestTunnelClientSettingsAreIndependent(t *testing.T) {
	first := newTunnelClientSettings()
	second := newTunnelClientSettings()
	if first == second || first.EncryptionSettings == second.EncryptionSettings {
		t.Fatal("tunnel client settings are shared between attempts")
	}
	first.EncryptionSettings.Mode = connect.EncryptionModeOff
	if second.EncryptionSettings.Mode != connect.EncryptionModeOpportunistic {
		t.Fatalf("second tunnel encryption mode = %v after mutating first, want opportunistic", second.EncryptionSettings.Mode)
	}
}
