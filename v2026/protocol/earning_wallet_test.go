// The precedence between a provider client's own consent and its network's
// consent, for every provider chain outcome.
package protocol

import (
	"errors"
	"testing"
)

func TestSelectEarningWalletPrecedence(t *testing.T) {
	provider := &EarningWallet{Mode: EarningWalletModeProvider, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: [32]byte{1}}
	network := &EarningWallet{Mode: EarningWalletModeNetwork, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: [32]byte{2}}
	notEffective := errors.Join(ErrWalletMappingUnavailable, ErrWalletMappingNotEffective)
	cases := []struct {
		name          string
		providerErr   error
		networkErr    error
		network       *EarningWallet
		want          *EarningWallet
		wantErr       error
		networkCalled bool
	}{
		{name: "provider consent wins", providerErr: nil, network: network, want: provider},
		{name: "absent provider chain uses the network consent", providerErr: WalletMappingAbsentError(), network: network, want: network, networkCalled: true},
		{name: "ineffective provider consent uses the network consent", providerErr: notEffective, network: network, want: network, networkCalled: true},
		{name: "missing provider originals never fall back", providerErr: ErrWalletMappingUnavailable, network: network, wantErr: ErrWalletMappingUnavailable},
		{name: "contradictory provider evidence never falls back", providerErr: ErrWalletMappingIntegrity, network: network, wantErr: ErrWalletMappingIntegrity},
		{name: "neither chain is unavailable", providerErr: WalletMappingAbsentError(), networkErr: WalletMappingAbsentError(), wantErr: ErrWalletMappingUnavailable, networkCalled: true},
		{name: "ineffective network consent is unavailable", providerErr: notEffective, networkErr: notEffective, wantErr: ErrWalletMappingNotEffective, networkCalled: true},
		{name: "network integrity failure is returned", providerErr: WalletMappingAbsentError(), networkErr: ErrWalletMappingIntegrity, wantErr: ErrWalletMappingIntegrity, networkCalled: true},
	}
	for _, c := range cases {
		called := false
		var providerWallet *EarningWallet
		if c.providerErr == nil {
			providerWallet = provider
		}
		wallet, err := SelectEarningWallet(providerWallet, c.providerErr, func() (*EarningWallet, error) {
			called = true
			if c.networkErr != nil {
				return nil, c.networkErr
			}
			return c.network, nil
		})
		if called != c.networkCalled {
			t.Errorf("%s: network chain evaluated = %t", c.name, called)
		}
		if c.wantErr != nil {
			if wallet != nil || !errors.Is(err, c.wantErr) {
				t.Errorf("%s: got %v, %v", c.name, wallet, err)
			}
			continue
		}
		if err != nil || wallet != c.want {
			t.Errorf("%s: got %v, %v", c.name, wallet, err)
		}
	}
}

// A wallet of the wrong mode is refused rather than paid.
func TestSelectEarningWalletRefusesWrongMode(t *testing.T) {
	wrong := &EarningWallet{Mode: EarningWalletModeNetwork}
	if wallet, err := SelectEarningWallet(wrong, nil, nil); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a network wallet was accepted as the provider consent", err)
	}
	provider := &EarningWallet{Mode: EarningWalletModeProvider}
	if wallet, err := SelectEarningWallet(nil, WalletMappingAbsentError(), func() (*EarningWallet, error) { return provider, nil }); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a provider wallet was accepted as the network consent", err)
	}
}
