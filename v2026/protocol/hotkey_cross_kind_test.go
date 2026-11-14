// The four signed wallet mapping kinds: provider (v1 and v2), network, global
// hotkey and hotkey delegation. Each decoder requires its own first line and
// refuses the other kinds' fields, so a message or signature of one kind
// never decodes or verifies as another, in either direction. The same
// synthetic coldkey and hotkey sign every kind, so only the signed bytes tell
// the kinds apart.
package protocol

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// One signed original of a kind, with every signature over it.
type walletMappingKindSource struct {
	name       string
	kind       string
	prefix     string
	message    string
	signatures [][64]byte
}

// Every kind signed by coldkey seed 11 and hotkey seed 21.
func walletMappingKindSources(t testing.TB) []walletMappingKindSource {
	t.Helper()
	providerStatement := walletMappingTestStatement()
	provider, _ := walletMappingTestOriginal(t, &providerStatement, 11, false)
	prospective, _, _ := walletProspectiveFixture(t)
	networkStatement := networkWalletTestStatement()
	network, _ := networkWalletTestOriginal(t, &networkStatement, 11, false)
	hotkeyStatement := hotkeyWalletTestStatement()
	hotkey, hotkeyHash := hotkeyWalletTestOriginal(t, &hotkeyStatement, 11, 21, false)
	delegationStatement := hotkeyDelegationTestStatement(hotkeyHash, 1)
	delegation, _ := hotkeyDelegationTestOriginal(t, &delegationStatement, 21, false)
	return []walletMappingKindSource{
		{name: "provider v1", kind: "provider", prefix: WalletMappingConsentPrefix, message: provider.Message, signatures: [][64]byte{provider.Signature}},
		{name: "provider v2", kind: "provider", prefix: WalletMappingConsentPrefix, message: prospective.Message, signatures: [][64]byte{prospective.Signature}},
		{name: "network", kind: "network", prefix: NetworkWalletMappingConsentPrefix, message: network.Message, signatures: [][64]byte{network.Signature}},
		{name: "global hotkey", kind: "hotkey", prefix: HotkeyWalletMappingConsentPrefix, message: hotkey.Message, signatures: [][64]byte{hotkey.ColdkeySignature, hotkey.HotkeySignature}},
		{name: "hotkey delegation", kind: "delegation", prefix: HotkeyNetworkDelegationPrefix, message: delegation.Message, signatures: [][64]byte{delegation.Signature}},
	}
}

// The decoder and the original verifier of a kind, reporting only the error.
// The global hotkey verifier is given the one signature in both places.
type walletMappingKindReader struct {
	kind   string
	prefix string
	decode func(string) error
	verify func(context.Context, string, [64]byte) error
}

func walletMappingKindReaders() []walletMappingKindReader {
	return []walletMappingKindReader{
		{
			kind:   "provider",
			prefix: WalletMappingConsentPrefix,
			decode: func(message string) error {
				_, err := DecodeWalletMappingStatement(message)
				return err
			},
			verify: func(ctx context.Context, message string, signature [64]byte) error {
				_, _, err := VerifyWalletMappingConsent(ctx, WalletMappingConsent{Message: message, Signature: signature})
				return err
			},
		},
		{
			kind:   "network",
			prefix: NetworkWalletMappingConsentPrefix,
			decode: func(message string) error {
				_, err := DecodeNetworkWalletMappingStatement(message)
				return err
			},
			verify: func(ctx context.Context, message string, signature [64]byte) error {
				_, _, err := VerifyNetworkWalletMappingConsent(ctx, WalletMappingConsent{Message: message, Signature: signature})
				return err
			},
		},
		{
			kind:   "hotkey",
			prefix: HotkeyWalletMappingConsentPrefix,
			decode: func(message string) error {
				_, err := DecodeHotkeyWalletMappingStatement(message)
				return err
			},
			verify: func(ctx context.Context, message string, signature [64]byte) error {
				_, _, err := VerifyHotkeyWalletMappingConsent(ctx, HotkeyWalletMappingConsent{Message: message, ColdkeySignature: signature, HotkeySignature: signature})
				return err
			},
		},
		{
			kind:   "delegation",
			prefix: HotkeyNetworkDelegationPrefix,
			decode: func(message string) error {
				_, err := DecodeHotkeyNetworkDelegationStatement(message)
				return err
			},
			verify: func(ctx context.Context, message string, signature [64]byte) error {
				_, _, err := VerifyHotkeyNetworkDelegation(ctx, WalletMappingConsent{Message: message, Signature: signature})
				return err
			},
		},
	}
}

// Every kind's message, as sent and under every other kind's first line,
// never decodes as another kind, and none of its signatures verifies as one.
func TestWalletMappingKindsRefuseEachOther(t *testing.T) {
	sources := walletMappingKindSources(t)
	for _, reader := range walletMappingKindReaders() {
		for _, source := range sources {
			if source.kind == reader.kind {
				if err := reader.decode(source.message); err != nil {
					t.Fatalf("%s did not decode as its own kind: %v", source.name, err)
				}
				continue
			}
			body := strings.TrimPrefix(source.message, source.prefix)
			for _, message := range []string{source.message, reader.prefix + body} {
				if err := reader.decode(message); !errors.Is(err, ErrWalletMappingIntegrity) {
					t.Errorf("%s decoded as %s: %v", source.name, reader.kind, err)
				}
				for _, signature := range source.signatures {
					if err := reader.verify(t.Context(), message, signature); !errors.Is(err, ErrWalletMappingIntegrity) {
						t.Errorf("a %s signature verified as %s: %v", source.name, reader.kind, err)
					}
				}
			}
		}
	}
}

// Each decoder refuses a field that only other kinds carry, even inside its
// own otherwise valid message.
func TestWalletMappingKindsRefuseForeignFields(t *testing.T) {
	sources := walletMappingKindSources(t)
	readers := walletMappingKindReaders()
	for _, c := range []struct {
		source int
		reader int
		fields []string
	}{
		{source: 0, reader: 0, fields: []string{`"scope":"provider",`, `"subnet":{},`, `"hotkey":[],`, `"consent_head_hash":[],`, `"consent_generation":1,`}},
		{source: 1, reader: 0, fields: []string{`"scope":"provider",`, `"subnet":{},`, `"hotkey":[],`, `"consent_head_hash":[],`, `"consent_generation":1,`}},
		{source: 2, reader: 1, fields: []string{`"client_id":[],`, `"subnet":{},`, `"hotkey":[],`, `"consent_head_hash":[],`, `"consent_generation":1,`}},
		{source: 3, reader: 2, fields: []string{`"domain":{},`, `"user_id":[],`, `"client_id":[],`, `"network_id":[],`, `"expires_at":1300,`, `"prospective":{},`, `"consent_head_hash":[],`, `"consent_generation":1,`}},
		{source: 4, reader: 3, fields: []string{`"client_id":[],`, `"coldkey":[],`, `"subnet":{},`}},
	} {
		source, reader := sources[c.source], readers[c.reader]
		if source.kind != reader.kind {
			t.Fatal("the foreign field case pairs different kinds", source.name, reader.kind)
		}
		for _, field := range c.fields {
			message := strings.Replace(source.message, `"generation":`, field+`"generation":`, 1)
			if message == source.message {
				t.Fatal("no field was added", source.name, field)
			}
			if err := reader.decode(message); !errors.Is(err, ErrWalletMappingIntegrity) {
				t.Errorf("%s decoded with the foreign field %s: %v", source.name, field, err)
			}
		}
	}
}

// One key's signature over one kind never completes an original of another,
// in either direction.
func TestWalletMappingKindSignaturesDoNotCross(t *testing.T) {
	providerStatement := walletMappingTestStatement()
	provider, _ := walletMappingTestOriginal(t, &providerStatement, 11, false)
	networkStatement := networkWalletTestStatement()
	network, _ := networkWalletTestOriginal(t, &networkStatement, 11, false)
	hotkeyStatement := hotkeyWalletTestStatement()
	hotkey, hotkeyHash := hotkeyWalletTestOriginal(t, &hotkeyStatement, 11, 21, false)
	delegationStatement := hotkeyDelegationTestStatement(hotkeyHash, 1)
	delegation, _ := hotkeyDelegationTestOriginal(t, &delegationStatement, 21, false)
	if providerStatement.Coldkey != hotkeyStatement.Coldkey || networkStatement.Coldkey != hotkeyStatement.Coldkey || delegationStatement.Hotkey != hotkeyStatement.Hotkey {
		t.Fatal("the kinds are not signed by the same keys")
	}
	for index, value := range []HotkeyWalletMappingConsent{
		{Message: hotkey.Message, ColdkeySignature: provider.Signature, HotkeySignature: hotkey.HotkeySignature},
		{Message: hotkey.Message, ColdkeySignature: network.Signature, HotkeySignature: hotkey.HotkeySignature},
		{Message: hotkey.Message, ColdkeySignature: hotkey.ColdkeySignature, HotkeySignature: delegation.Signature},
	} {
		if decoded, _, err := VerifyHotkeyWalletMappingConsent(t.Context(), value); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("another kind's signature completed a global hotkey consent", index, err)
		}
	}
	if decoded, _, err := VerifyHotkeyNetworkDelegation(t.Context(), WalletMappingConsent{Message: delegation.Message, Signature: hotkey.HotkeySignature}); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("the hotkey's global consent signature completed a delegation", err)
	}
	if decoded, _, err := VerifyNetworkWalletMappingConsent(t.Context(), WalletMappingConsent{Message: network.Message, Signature: hotkey.ColdkeySignature}); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("the coldkey's global consent signature completed a network consent", err)
	}
	if decoded, _, err := VerifyWalletMappingConsent(t.Context(), WalletMappingConsent{Message: provider.Message, Signature: hotkey.ColdkeySignature}); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("the coldkey's global consent signature completed a provider consent", err)
	}
}
