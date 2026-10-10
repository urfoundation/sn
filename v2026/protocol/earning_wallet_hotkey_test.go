// Hotkey earning-wallet resolution through a network's delegation and the
// global consent it pins, and the provider > network > hotkey precedence.
package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// The gate inputs of every resolution here: a start block after the operator
// boundary block and a start time after the delegation's acceptance window.
const hotkeyEarningTestStartBlock = 510
const hotkeyEarningTestStartUnix = 1400

// The global consent of hotkey seed 21 to coldkey seed 11 from epoch 0, and the
// test network's delegation to that hotkey pinning it, read at epoch 51.
type hotkeyEarningTestFixture struct {
	consent        HotkeyWalletMappingStatement
	consentHash    [32]byte
	delegation     HotkeyNetworkDelegationStatement
	delegationHash [32]byte
	evidence       HotkeyEarningWalletEvidence
	signer         common.Address
}

func newHotkeyEarningTestFixture(t testing.TB) hotkeyEarningTestFixture {
	t.Helper()
	consent := hotkeyWalletTestStatement()
	consentOriginal, consentHash := hotkeyWalletTestOriginal(t, &consent, 11, 21, false)
	delegation := hotkeyDelegationTestStatement(consentHash, 1)
	delegationOriginal, delegationHash := hotkeyDelegationTestOriginal(t, &delegation, 21, true)
	_, signer := networkWalletTestOperator(t)
	return hotkeyEarningTestFixture{
		consent:        consent,
		consentHash:    consentHash,
		delegation:     delegation,
		delegationHash: delegationHash,
		evidence: HotkeyEarningWalletEvidence{
			Delegations:        []WalletMappingConsent{delegationOriginal},
			DelegationExpected: HotkeyNetworkDelegationHistoryExpectation{Domain: delegation.Domain, NetworkId: delegation.NetworkId, HeadHash: delegationHash, Generation: 1, Epoch: 51},
			Consents:           []HotkeyWalletMappingConsent{consentOriginal},
		},
		signer: signer,
	}
}

// The fixture's evidence with its delegation re-signed by hotkey seed 21 to pin
// another global head, and that head's originals.
func (self hotkeyEarningTestFixture) pinning(t testing.TB, consents []HotkeyWalletMappingConsent, headHash [32]byte, generation uint64) HotkeyEarningWalletEvidence {
	t.Helper()
	delegation := hotkeyDelegationTestStatement(headHash, generation)
	original, hash := hotkeyDelegationTestOriginal(t, &delegation, 21, false)
	evidence := self.evidence
	evidence.Delegations = []WalletMappingConsent{original}
	evidence.DelegationExpected.HeadHash = hash
	evidence.Consents = consents
	return evidence
}

// The delegation chain is what the roster pins; the global consent selected at
// the epoch supplies the coldkey.
func TestResolveHotkeyEarningWallet(t *testing.T) {
	f := newHotkeyEarningTestFixture(t)
	wallet, err := ResolveHotkeyEarningWallet(t.Context(), [16]byte{8}, f.evidence, f.signer, hotkeyEarningTestStartBlock, hotkeyEarningTestStartUnix)
	want := EarningWallet{
		Mode:                  EarningWalletModeHotkey,
		ClientId:              [16]byte{8},
		NetworkId:             [16]byte{9},
		Coldkey:               f.consent.Coldkey,
		OriginalHash:          f.delegationHash,
		Generation:            1,
		HeadHash:              f.delegationHash,
		HeadGeneration:        1,
		Hotkey:                f.delegation.Hotkey,
		ConsentOriginalHash:   f.consentHash,
		ConsentGeneration:     1,
		ConsentHeadHash:       f.consentHash,
		ConsentHeadGeneration: 1,
	}
	if err != nil || wallet == nil || *wallet != want {
		t.Fatalf("hotkey earning wallet differs:\n%+v\n%+v\n%v", wallet, want, err)
	}
	if HotkeyEarningWallet([16]byte{8}, nil, nil) != nil {
		t.Fatal("a wallet was built without its verified chains")
	}
}

// The delegation selected at the epoch decides which global head is read, and
// the global generation selected at the same epoch decides the coldkey.
func TestResolveHotkeyEarningWalletFollowsTheDelegatedHead(t *testing.T) {
	f := newHotkeyEarningTestFixture(t)
	firstOriginal := f.evidence.Consents[0]
	second := hotkeyWalletTestSuccessor(f.consent, f.consentHash, 60, 1000)
	secondOriginal, secondHash := hotkeyWalletTestOriginal(t, &second, 12, 21, false)
	both := []HotkeyWalletMappingConsent{firstOriginal, secondOriginal}

	// one delegation pinning the second head reads either global generation
	evidence := f.pinning(t, both, secondHash, 2)
	for _, c := range []struct {
		epoch       uint64
		coldkey     [32]byte
		consentHash [32]byte
		generation  uint64
	}{
		{epoch: 55, coldkey: f.consent.Coldkey, consentHash: f.consentHash, generation: 1},
		{epoch: 60, coldkey: second.Coldkey, consentHash: secondHash, generation: 2},
	} {
		evidence.DelegationExpected.Epoch = c.epoch
		wallet, err := ResolveHotkeyEarningWallet(t.Context(), [16]byte{8}, evidence, f.signer, hotkeyEarningTestStartBlock, hotkeyEarningTestStartUnix)
		if err != nil || wallet == nil || wallet.Coldkey != c.coldkey || wallet.ConsentOriginalHash != c.consentHash || wallet.ConsentGeneration != c.generation || wallet.ConsentHeadHash != secondHash || wallet.ConsentHeadGeneration != 2 {
			t.Fatal("the pinned global head selected another consent", c.epoch, wallet, err)
		}
	}

	// a second delegation generation adopts the second head from epoch 80
	next := f.delegation
	next.ConsentHeadHash, next.ConsentGeneration = secondHash, 2
	next.Generation, next.PreviousHash, next.FromEpoch, next.ThroughEpoch = 2, f.delegationHash, 80, 180
	next.Nonce[0]++
	nextOriginal, nextHash := hotkeyDelegationTestOriginal(t, &next, 21, false)
	evidence = f.evidence
	evidence.Delegations = append([]WalletMappingConsent{f.evidence.Delegations[0]}, nextOriginal)
	evidence.DelegationExpected.HeadHash, evidence.DelegationExpected.Generation = nextHash, 2
	for _, c := range []struct {
		epoch          uint64
		consents       []HotkeyWalletMappingConsent
		coldkey        [32]byte
		delegationHash [32]byte
		generation     uint64
	}{
		{epoch: 60, consents: []HotkeyWalletMappingConsent{firstOriginal}, coldkey: f.consent.Coldkey, delegationHash: f.delegationHash, generation: 1},
		{epoch: 80, consents: both, coldkey: second.Coldkey, delegationHash: nextHash, generation: 2},
	} {
		evidence.DelegationExpected.Epoch, evidence.Consents = c.epoch, c.consents
		wallet, err := ResolveHotkeyEarningWallet(t.Context(), [16]byte{8}, evidence, f.signer, hotkeyEarningTestStartBlock, hotkeyEarningTestStartUnix)
		if err != nil || wallet == nil || wallet.Coldkey != c.coldkey || wallet.OriginalHash != c.delegationHash || wallet.Generation != c.generation || wallet.HeadHash != nextHash || wallet.HeadGeneration != 2 || wallet.ConsentHeadGeneration != c.generation {
			t.Fatal("the delegation selected at the epoch did not decide the global head", c.epoch, wallet, err)
		}
	}
	// the global chain must end exactly at the head the selected delegation pins
	evidence.DelegationExpected.Epoch, evidence.Consents = 60, both
	if wallet, err := ResolveHotkeyEarningWallet(t.Context(), [16]byte{8}, evidence, f.signer, hotkeyEarningTestStartBlock, hotkeyEarningTestStartUnix); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a global chain past the pinned head was admitted", err)
	}
	evidence.DelegationExpected.Epoch, evidence.Consents = 80, []HotkeyWalletMappingConsent{firstOriginal}
	if wallet, err := ResolveHotkeyEarningWallet(t.Context(), [16]byte{8}, evidence, f.signer, hotkeyEarningTestStartBlock, hotkeyEarningTestStartUnix); wallet != nil || !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingNotEffective) || errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("missing global originals were not plainly unavailable", err)
	}
}

// Every failing step returns its own error: the delegation history, the
// delegation's earning gate, then the global chain through the pinned head
// for the delegation's hotkey and the operator domain's subnet.
func TestResolveHotkeyEarningWalletRefusals(t *testing.T) {
	f := newHotkeyEarningTestFixture(t)
	firstOriginal := f.evidence.Consents[0]
	second := hotkeyWalletTestSuccessor(f.consent, f.consentHash, 60, 1000)
	secondOriginal, secondHash := hotkeyWalletTestOriginal(t, &second, 12, 21, false)
	otherHotkey := hotkeyWalletTestStatement()
	otherHotkeyOriginal, otherHotkeyHash := hotkeyWalletTestOriginal(t, &otherHotkey, 11, 22, false)
	otherSubnet := hotkeyWalletTestStatement()
	otherSubnet.Subnet.Netuid++
	otherSubnetOriginal, otherSubnetHash := hotkeyWalletTestOriginal(t, &otherSubnet, 11, 21, false)
	late := hotkeyWalletTestStatement()
	late.FromEpoch = 52
	lateOriginal, lateHash := hotkeyWalletTestOriginal(t, &late, 11, 21, false)
	ended := hotkeyWalletTestStatement()
	ended.ThroughEpoch = 50
	endedOriginal, endedHash := hotkeyWalletTestOriginal(t, &ended, 11, 21, false)

	type inputs struct {
		clientId   [16]byte
		evidence   HotkeyEarningWalletEvidence
		signer     common.Address
		startBlock uint64
		startUnix  int64
	}
	base := inputs{clientId: [16]byte{8}, evidence: f.evidence, signer: f.signer, startBlock: hotkeyEarningTestStartBlock, startUnix: hotkeyEarningTestStartUnix}
	pin := func(consents []HotkeyWalletMappingConsent, headHash [32]byte, generation uint64) func(*inputs) {
		evidence := f.pinning(t, consents, headHash, generation)
		return func(i *inputs) { i.evidence = evidence }
	}
	notEffective := []error{ErrWalletMappingNotEffective, ErrWalletMappingUnavailable}
	unavailable := []error{ErrWalletMappingUnavailable}
	integrity := []error{ErrWalletMappingIntegrity}
	for _, c := range []struct {
		name   string
		change func(*inputs)
		want   []error
		// unavailable only, distinct from not effective, absent and integrity
		plain  bool
		absent bool
	}{
		{name: "delegation not yet effective", change: func(i *inputs) { i.evidence.DelegationExpected.Epoch = 50 }, want: notEffective},
		{name: "delegation no longer effective", change: func(i *inputs) { i.evidence.DelegationExpected.Epoch = 152 }, want: notEffective},
		{name: "missing delegation originals", change: func(i *inputs) { i.evidence.Delegations = nil }, want: unavailable, plain: true},
		{name: "foreign delegation head", change: func(i *inputs) { i.evidence.DelegationExpected.HeadHash = [32]byte{99} }, want: integrity},
		{name: "another network's delegation", change: func(i *inputs) { i.evidence.DelegationExpected.NetworkId = [16]byte{10} }, want: integrity},
		{name: "another operator domain's delegation", change: func(i *inputs) { i.evidence.DelegationExpected.Domain.NoID++ }, want: integrity},
		{name: "gate with a foreign operator signer", change: func(i *inputs) { i.signer = common.Address{99} }, want: integrity},
		{name: "gate with a boundary inside the window", change: func(i *inputs) { i.startBlock = 500 }, want: integrity},
		{name: "gate with an acceptance that may fall inside the window", change: func(i *inputs) { i.startUnix = 1299 }, want: unavailable, plain: true},
		{name: "gate without a signer", change: func(i *inputs) { i.signer = common.Address{} }, want: unavailable, plain: true},
		{name: "global head differs from the pinned head", change: pin([]HotkeyWalletMappingConsent{firstOriginal}, [32]byte{77}, 1), want: integrity},
		{name: "global chain continues past the pinned generation", change: pin([]HotkeyWalletMappingConsent{firstOriginal, secondOriginal}, f.consentHash, 1), want: integrity},
		{name: "pinned global head of another generation", change: pin([]HotkeyWalletMappingConsent{firstOriginal, secondOriginal}, f.consentHash, 2), want: integrity},
		{name: "missing global originals", change: func(i *inputs) { i.evidence.Consents = nil }, want: unavailable, plain: true},
		{name: "global originals short of the pinned generation", change: pin([]HotkeyWalletMappingConsent{firstOriginal}, secondHash, 2), want: unavailable, plain: true},
		{name: "global consent not yet effective", change: pin([]HotkeyWalletMappingConsent{lateOriginal}, lateHash, 1), want: notEffective},
		{name: "global consent no longer effective", change: pin([]HotkeyWalletMappingConsent{endedOriginal}, endedHash, 1), want: notEffective},
		{name: "global consent of another hotkey", change: pin([]HotkeyWalletMappingConsent{otherHotkeyOriginal}, otherHotkeyHash, 1), want: integrity},
		{name: "global consent of another subnet", change: pin([]HotkeyWalletMappingConsent{otherSubnetOriginal}, otherSubnetHash, 1), want: integrity},
		{name: "no pinned delegation is absent", change: func(i *inputs) { i.evidence = HotkeyEarningWalletEvidence{} }, want: unavailable, absent: true},
		{name: "delegation originals without a pinned head", change: func(i *inputs) { i.evidence = HotkeyEarningWalletEvidence{Delegations: f.evidence.Delegations} }, want: integrity},
		{name: "global originals without a pinned head", change: func(i *inputs) { i.evidence = HotkeyEarningWalletEvidence{Consents: f.evidence.Consents} }, want: integrity},
		{name: "a zero client", change: func(i *inputs) { i.clientId = [16]byte{} }, want: integrity},
	} {
		value := base
		c.change(&value)
		wallet, err := ResolveHotkeyEarningWallet(t.Context(), value.clientId, value.evidence, value.signer, value.startBlock, value.startUnix)
		if wallet != nil || err == nil {
			t.Errorf("%s: resolved %v", c.name, wallet)
			continue
		}
		for _, want := range c.want {
			if !errors.Is(err, want) {
				t.Errorf("%s: %v is not %v", c.name, err, want)
			}
		}
		if c.plain && (errors.Is(err, ErrWalletMappingNotEffective) || errors.Is(err, ErrWalletMappingIntegrity)) {
			t.Errorf("%s: %v is more than unavailable", c.name, err)
		}
		if errors.Is(err, ErrWalletMappingAbsent) != c.absent {
			t.Errorf("%s: %v absent is not %t", c.name, err, c.absent)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if wallet, err := ResolveHotkeyEarningWallet(ctx, [16]byte{8}, f.evidence, f.signer, hotkeyEarningTestStartBlock, hotkeyEarningTestStartUnix); wallet != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("a canceled resolution published a wallet", err)
	}
}

// The precedence over each mode's outcome: a mode falls back only when absent
// or not effective, every other outcome is final, and a mode is evaluated
// only when every mode before it fell back.
func TestSelectEarningWalletWithHotkeyPrecedence(t *testing.T) {
	provider := &EarningWallet{Mode: EarningWalletModeProvider, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: [32]byte{1}}
	network := &EarningWallet{Mode: EarningWalletModeNetwork, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: [32]byte{2}}
	hotkey := &EarningWallet{Mode: EarningWalletModeHotkey, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: [32]byte{3}, Hotkey: [32]byte{4}}
	absent := WalletMappingAbsentError()
	notEffective := errors.Join(ErrWalletMappingUnavailable, ErrWalletMappingNotEffective)
	cases := []struct {
		name          string
		providerErr   error
		networkErr    error
		hotkeyErr     error
		want          *EarningWallet
		wantErrs      []error
		networkCalled bool
		hotkeyCalled  bool
	}{
		{name: "provider consent wins", want: provider},
		{name: "absent provider chain uses the network consent", providerErr: absent, want: network, networkCalled: true},
		{name: "ineffective provider consent uses the network consent", providerErr: notEffective, want: network, networkCalled: true},
		{name: "absent network chain uses the hotkey delegation", providerErr: absent, networkErr: absent, want: hotkey, networkCalled: true, hotkeyCalled: true},
		{name: "ineffective network consent uses the hotkey delegation", providerErr: notEffective, networkErr: notEffective, want: hotkey, networkCalled: true, hotkeyCalled: true},
		{name: "absent provider and ineffective network use the hotkey", providerErr: absent, networkErr: notEffective, want: hotkey, networkCalled: true, hotkeyCalled: true},
		{name: "ineffective provider and absent network use the hotkey", providerErr: notEffective, networkErr: absent, want: hotkey, networkCalled: true, hotkeyCalled: true},
		{name: "network integrity failure never falls back to the hotkey", providerErr: absent, networkErr: ErrWalletMappingIntegrity, wantErrs: []error{ErrWalletMappingIntegrity}, networkCalled: true},
		{name: "missing network originals never fall back to the hotkey", providerErr: absent, networkErr: ErrWalletMappingUnavailable, wantErrs: []error{ErrWalletMappingUnavailable}, networkCalled: true},
		{name: "network capacity failure never falls back to the hotkey", providerErr: notEffective, networkErr: ErrWalletMappingCapacity, wantErrs: []error{ErrWalletMappingCapacity}, networkCalled: true},
		{name: "provider integrity failure never falls back", providerErr: ErrWalletMappingIntegrity, wantErrs: []error{ErrWalletMappingIntegrity}},
		{name: "missing provider originals never fall back", providerErr: ErrWalletMappingUnavailable, wantErrs: []error{ErrWalletMappingUnavailable}},
		{name: "everything absent is unavailable", providerErr: absent, networkErr: absent, hotkeyErr: absent, wantErrs: []error{ErrWalletMappingUnavailable, ErrWalletMappingAbsent}, networkCalled: true, hotkeyCalled: true},
		{name: "nothing effective is unavailable", providerErr: notEffective, networkErr: notEffective, hotkeyErr: notEffective, wantErrs: []error{ErrWalletMappingUnavailable, ErrWalletMappingNotEffective}, networkCalled: true, hotkeyCalled: true},
		{name: "hotkey integrity failure is returned", providerErr: absent, networkErr: absent, hotkeyErr: ErrWalletMappingIntegrity, wantErrs: []error{ErrWalletMappingIntegrity}, networkCalled: true, hotkeyCalled: true},
		{name: "missing hotkey originals are returned", providerErr: absent, networkErr: notEffective, hotkeyErr: ErrWalletMappingUnavailable, wantErrs: []error{ErrWalletMappingUnavailable}, networkCalled: true, hotkeyCalled: true},
	}
	for _, c := range cases {
		networkCalled, hotkeyCalled := false, false
		var providerWallet *EarningWallet
		if c.providerErr == nil {
			providerWallet = provider
		}
		wallet, err := SelectEarningWalletWithHotkey(providerWallet, c.providerErr, func() (*EarningWallet, error) {
			networkCalled = true
			if c.networkErr != nil {
				return nil, c.networkErr
			}
			return network, nil
		}, func() (*EarningWallet, error) {
			hotkeyCalled = true
			if c.hotkeyErr != nil {
				return nil, c.hotkeyErr
			}
			return hotkey, nil
		})
		if networkCalled != c.networkCalled || hotkeyCalled != c.hotkeyCalled {
			t.Errorf("%s: network evaluated = %t, hotkey evaluated = %t", c.name, networkCalled, hotkeyCalled)
		}
		if c.wantErrs != nil {
			if wallet != nil || err == nil {
				t.Errorf("%s: got %v, %v", c.name, wallet, err)
				continue
			}
			for _, want := range c.wantErrs {
				if !errors.Is(err, want) {
					t.Errorf("%s: %v is not %v", c.name, err, want)
				}
			}
			continue
		}
		if err != nil || wallet != c.want {
			t.Errorf("%s: got %v, %v", c.name, wallet, err)
		}
	}
}

// A final outcome is the failing mode's own error value, unchanged.
func TestSelectEarningWalletWithHotkeyReturnsErrorsUnchanged(t *testing.T) {
	providerErr := errors.Join(ErrWalletMappingIntegrity, errors.New("synthetic provider evidence"))
	networkErr := errors.Join(ErrWalletMappingUnavailable, errors.New("synthetic network evidence"))
	hotkeyErr := errors.Join(ErrWalletMappingUnavailable, ErrWalletMappingNotEffective)
	fail := func(err error) func() (*EarningWallet, error) {
		return func() (*EarningWallet, error) { return nil, err }
	}
	if _, err := SelectEarningWalletWithHotkey(nil, providerErr, fail(WalletMappingAbsentError()), fail(WalletMappingAbsentError())); err != providerErr {
		t.Fatal("the provider failure was replaced", err)
	}
	if _, err := SelectEarningWalletWithHotkey(nil, WalletMappingAbsentError(), fail(networkErr), fail(WalletMappingAbsentError())); err != networkErr {
		t.Fatal("the network failure was replaced", err)
	}
	if _, err := SelectEarningWalletWithHotkey(nil, WalletMappingAbsentError(), fail(WalletMappingAbsentError()), fail(hotkeyErr)); err != hotkeyErr {
		t.Fatal("the hotkey outcome was replaced", err)
	}
}

// A wallet of the wrong mode in any position is refused rather than paid.
func TestSelectEarningWalletWithHotkeyRefusesWrongMode(t *testing.T) {
	absent := func() (*EarningWallet, error) { return nil, WalletMappingAbsentError() }
	for index, wrong := range []*EarningWallet{nil, {Mode: EarningWalletModeProvider}, {Mode: EarningWalletModeNetwork}, {Mode: ""}} {
		if wallet, err := SelectEarningWalletWithHotkey(nil, WalletMappingAbsentError(), absent, func() (*EarningWallet, error) { return wrong, nil }); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("a wallet of another mode was accepted as the hotkey delegation", index, err)
		}
	}
	hotkeyCalled := false
	hotkey := &EarningWallet{Mode: EarningWalletModeHotkey}
	if wallet, err := SelectEarningWalletWithHotkey(nil, WalletMappingAbsentError(), func() (*EarningWallet, error) { return hotkey, nil }, func() (*EarningWallet, error) {
		hotkeyCalled = true
		return hotkey, nil
	}); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) || hotkeyCalled {
		t.Fatal("a hotkey wallet was accepted as the network consent", err)
	}
	if wallet, err := SelectEarningWalletWithHotkey(hotkey, nil, absent, absent); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a hotkey wallet was accepted as the provider consent", err)
	}
}

// Without a network chain to fall back from, as in SelectEarningWallet, the
// selection stops at the provider outcome and never reads the hotkey.
func TestSelectEarningWalletWithHotkeyNeedsTheNetworkOutcome(t *testing.T) {
	hotkeyCalled := false
	providerErr := WalletMappingAbsentError()
	wallet, err := SelectEarningWalletWithHotkey(nil, providerErr, nil, func() (*EarningWallet, error) {
		hotkeyCalled = true
		return &EarningWallet{Mode: EarningWalletModeHotkey}, nil
	})
	if wallet != nil || err != providerErr || hotkeyCalled {
		t.Fatal("the hotkey was read without a network outcome", wallet, err, hotkeyCalled)
	}
}

// A nil hotkey gives exactly SelectEarningWallet's outcome for every provider
// and network outcome, including the network's evaluation.
func TestSelectEarningWalletWithNilHotkeyMatchesSelectEarningWallet(t *testing.T) {
	provider := &EarningWallet{Mode: EarningWalletModeProvider}
	network := &EarningWallet{Mode: EarningWalletModeNetwork}
	hotkey := &EarningWallet{Mode: EarningWalletModeHotkey}
	providerErrs := []error{nil, WalletMappingAbsentError(), errors.Join(ErrWalletMappingUnavailable, ErrWalletMappingNotEffective), ErrWalletMappingUnavailable, ErrWalletMappingIntegrity}
	providerWallets := []*EarningWallet{provider, nil, network, hotkey}
	type networkOutcome struct {
		wallet *EarningWallet
		err    error
		absent bool
	}
	networkOutcomes := []networkOutcome{
		{absent: true},
		{wallet: network},
		{wallet: provider},
		{wallet: hotkey},
		{},
		{err: WalletMappingAbsentError()},
		{err: errors.Join(ErrWalletMappingUnavailable, ErrWalletMappingNotEffective)},
		{err: ErrWalletMappingIntegrity},
		{err: ErrWalletMappingUnavailable},
	}
	for _, providerErr := range providerErrs {
		for _, providerWallet := range providerWallets {
			for index, outcome := range networkOutcomes {
				run := func(selectWallet func(*EarningWallet, error, func() (*EarningWallet, error)) (*EarningWallet, error)) (*EarningWallet, error, bool) {
					called := false
					var networkFunction func() (*EarningWallet, error)
					if !outcome.absent {
						networkFunction = func() (*EarningWallet, error) {
							called = true
							return outcome.wallet, outcome.err
						}
					}
					wallet, err := selectWallet(providerWallet, providerErr, networkFunction)
					return wallet, err, called
				}
				wantWallet, wantErr, wantCalled := run(SelectEarningWallet)
				gotWallet, gotErr, gotCalled := run(func(provider *EarningWallet, providerErr error, network func() (*EarningWallet, error)) (*EarningWallet, error) {
					return SelectEarningWalletWithHotkey(provider, providerErr, network, nil)
				})
				if gotWallet != wantWallet || gotErr != wantErr || gotCalled != wantCalled {
					t.Errorf("provider %v %v, network outcome %d: got %v %v %t, want %v %v %t", providerWallet, providerErr, index, gotWallet, gotErr, gotCalled, wantWallet, wantErr, wantCalled)
				}
			}
		}
	}
}

// SelectEarningWallet keeps its two modes: a hotkey wallet in either position
// is refused, and it never evaluates beyond the network chain.
func TestSelectEarningWalletRefusesHotkeyMode(t *testing.T) {
	hotkey := &EarningWallet{Mode: EarningWalletModeHotkey}
	if wallet, err := SelectEarningWallet(hotkey, nil, nil); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a hotkey wallet was accepted as the provider consent", err)
	}
	if wallet, err := SelectEarningWallet(nil, WalletMappingAbsentError(), func() (*EarningWallet, error) { return hotkey, nil }); wallet != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a hotkey wallet was accepted as the network consent", err)
	}
	if wallet, err := SelectEarningWallet(nil, WalletMappingAbsentError(), func() (*EarningWallet, error) { return nil, WalletMappingAbsentError() }); wallet != nil || !errors.Is(err, ErrWalletMappingAbsent) {
		t.Fatal("neither chain did not stay unavailable", err)
	}
}

// The resolution and the precedence together, the way settlement and the
// economic verifier call them: absent provider and network chains reach the
// delegation, and a network without a pinned delegation stays unavailable.
func TestSelectEarningWalletWithHotkeyResolvesThroughDelegation(t *testing.T) {
	f := newHotkeyEarningTestFixture(t)
	resolve := func(evidence HotkeyEarningWalletEvidence) func() (*EarningWallet, error) {
		return func() (*EarningWallet, error) {
			return ResolveHotkeyEarningWallet(t.Context(), [16]byte{8}, evidence, f.signer, hotkeyEarningTestStartBlock, hotkeyEarningTestStartUnix)
		}
	}
	networkAbsent := func() (*EarningWallet, error) { return nil, WalletMappingAbsentError() }
	wallet, err := SelectEarningWalletWithHotkey(nil, WalletMappingAbsentError(), networkAbsent, resolve(f.evidence))
	if err != nil || wallet == nil || wallet.Mode != EarningWalletModeHotkey || wallet.Coldkey != f.consent.Coldkey || wallet.HeadHash != f.delegationHash {
		t.Fatal("the delegation did not earn after absent provider and network chains", wallet, err)
	}
	wallet, err = SelectEarningWalletWithHotkey(nil, WalletMappingAbsentError(), networkAbsent, resolve(HotkeyEarningWalletEvidence{}))
	if wallet != nil || !errors.Is(err, ErrWalletMappingAbsent) || !errors.Is(err, ErrWalletMappingUnavailable) {
		t.Fatal("a network without a pinned delegation did not stay unavailable", wallet, err)
	}
}

// The new fields are omitted for the provider and network modes, whose JSON
// keeps its exact bytes; a hotkey wallet carries and round trips them.
func TestEarningWalletJsonKeepsTheOtherModes(t *testing.T) {
	array := func(size int, first byte) string {
		return fmt.Sprintf("[%d%s]", first, strings.Repeat(",0", size-1))
	}
	network := EarningWallet{Mode: EarningWalletModeNetwork, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: [32]byte{1}, OriginalHash: [32]byte{2}, Generation: 1, HeadHash: [32]byte{3}, HeadGeneration: 4}
	want := `{"Mode":"network","ClientId":` + array(16, 8) + `,"NetworkId":` + array(16, 9) + `,"Coldkey":` + array(32, 1) + `,"OriginalHash":` + array(32, 2) + `,"Generation":1,"HeadHash":` + array(32, 3) + `,"HeadGeneration":4}`
	raw, err := json.Marshal(network)
	if err != nil || string(raw) != want {
		t.Fatalf("network wallet JSON changed:\n%s\n%s\n%v", raw, want, err)
	}
	hotkey := network
	hotkey.Mode, hotkey.Hotkey, hotkey.ConsentOriginalHash, hotkey.ConsentGeneration, hotkey.ConsentHeadHash, hotkey.ConsentHeadGeneration = EarningWalletModeHotkey, [32]byte{5}, [32]byte{6}, 1, [32]byte{7}, 2
	want = strings.Replace(want, `"network"`, `"hotkey"`, 1)
	want = strings.TrimSuffix(want, "}") + `,"Hotkey":` + array(32, 5) + `,"ConsentOriginalHash":` + array(32, 6) + `,"ConsentGeneration":1,"ConsentHeadHash":` + array(32, 7) + `,"ConsentHeadGeneration":2}`
	raw, err = json.Marshal(hotkey)
	if err != nil || string(raw) != want {
		t.Fatalf("hotkey wallet JSON differs:\n%s\n%s\n%v", raw, want, err)
	}
	var decoded EarningWallet
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded != hotkey {
		t.Fatal("hotkey wallet did not round trip", decoded, err)
	}
}
