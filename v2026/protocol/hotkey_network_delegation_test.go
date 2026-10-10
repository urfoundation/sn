// The per-operator hotkey network delegation: the network consent signed by a
// hotkey and pinning a global consent head. The synthetic operator key and
// sr25519 keys from fixed seeds sign the same transcripts as the operator and
// wallets, and every identity is visibly synthetic.
package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Every field is visibly synthetic; the interval starts after the operator
// boundary, and the delegation pins the given global consent head.
func hotkeyDelegationTestStatement(consentHeadHash [32]byte, consentGeneration uint64) HotkeyNetworkDelegationStatement {
	return HotkeyNetworkDelegationStatement{Domain: walletMappingTestStatement().Domain, UserId: [16]byte{7}, NetworkId: [16]byte{9}, ConsentHeadHash: consentHeadHash, ConsentGeneration: consentGeneration, Nonce: [32]byte{10}, Generation: 1, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 51, ThroughEpoch: 151}
}

// Sets the hotkey from its seed, signs the statement as the operator at the
// test boundary and then as the hotkey, raw or in the Bytes wrapper, and
// returns the original and its hash.
func hotkeyDelegationTestOriginal(t testing.TB, statement *HotkeyNetworkDelegationStatement, hotkeySeed byte, wrapped bool) (WalletMappingConsent, [32]byte) {
	t.Helper()
	statement.Hotkey = hotkeyWalletTestKey(t, hotkeySeed).Public().Encode()
	operator, _ := networkWalletTestOperator(t)
	if err := SignProspectiveHotkeyNetworkDelegation(statement, networkWalletTestBoundary, operator); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	original := WalletMappingConsent{Message: message, Signature: hotkeyWalletTestSign(t, hotkeySeed, message, wrapped)}
	_, hash, err := VerifyHotkeyNetworkDelegation(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

func TestHotkeyNetworkDelegationRawAndWrappedSignatures(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		statement := hotkeyDelegationTestStatement([32]byte{15}, 1)
		original, hash := hotkeyDelegationTestOriginal(t, &statement, 21, wrapped)
		if !strings.HasPrefix(original.Message, "Delegate URnetwork network wallet to hotkey\n{") || !strings.Contains(original.Message, `"scope":"hotkey-network"`) {
			t.Fatalf("delegation message does not state its scope: %q", original.Message)
		}
		for _, field := range []string{`"coldkey"`, `"client_id"`, `"subnet"`} {
			if strings.Contains(original.Message, field) {
				t.Fatalf("delegation message carries another kind's %s: %q", field, original.Message)
			}
		}
		delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), []WalletMappingConsent{original}, HotkeyNetworkDelegationHistoryExpectation{Domain: statement.Domain, NetworkId: statement.NetworkId, HeadHash: hash, Generation: 1, Epoch: 51})
		if err != nil || delegation == nil || delegation.Statement != statement || delegation.OriginalHash != hash || delegation.HeadHash != hash || delegation.Generation != 1 {
			t.Fatal("delegation was not reconstructed", wrapped, delegation, err)
		}
	}
}

// The displayed bytes are the first line, then every field in declaration
// order. Decoding returns the same statement and re-encodes to the same bytes.
func TestHotkeyNetworkDelegationMessageRoundTrip(t *testing.T) {
	statement := hotkeyDelegationTestStatement([32]byte{15}, 1)
	original, _ := hotkeyDelegationTestOriginal(t, &statement, 21, false)
	decoded, err := DecodeHotkeyNetworkDelegationStatement(original.Message)
	if err != nil || decoded == nil || *decoded != statement {
		t.Fatal("delegation did not round trip", decoded, err)
	}
	again, err := decoded.Message()
	if err != nil || again != original.Message {
		t.Fatal("decoded delegation re-encoded differently", err)
	}
	body := strings.TrimPrefix(original.Message, HotkeyNetworkDelegationPrefix)
	if !strings.HasPrefix(body, `{"schema":"urnetwork-hotkey-network-delegation-v1","scope":"hotkey-network","domain":{"chain_id":964,`) || !strings.HasSuffix(body, "]}}") {
		t.Fatalf("delegation fields are not in declaration order: %s", body)
	}
	position := 0
	for _, field := range []string{`,"user_id":[7`, `,"network_id":[9`, `,"hotkey":[`, `,"consent_head_hash":[15`, `,"consent_generation":1`, `,"generation":1`, `,"previous_hash":[0`, `,"nonce":[10`, `,"issued_at":1000`, `,"expires_at":1300`, `,"from_epoch":51`, `,"through_epoch":151`, `,"prospective":{"boundary":{"epoch":50,"block":500,"hash":[12`, `,"signer":"0x`, `,"signature":[`} {
		next := strings.Index(body[position:], field)
		if next < 0 {
			t.Fatalf("delegation lacks %s after byte %d: %s", field, position, body)
		}
		position += next + len(field)
	}
}

// The hotkey's signature covers the operator domain, the user, the network,
// the pinned consent head and generation, the lineage, the nonce, the
// interval and the acceptance window. Another hotkey cannot sign for it.
func TestHotkeyNetworkDelegationSignatureBindsEveryField(t *testing.T) {
	statement := hotkeyDelegationTestStatement([32]byte{15}, 1)
	original, _ := hotkeyDelegationTestOriginal(t, &statement, 21, false)
	operator, _ := networkWalletTestOperator(t)
	for index, mutate := range []func(*HotkeyNetworkDelegationStatement){
		func(s *HotkeyNetworkDelegationStatement) { s.Domain.NoID++ },
		func(s *HotkeyNetworkDelegationStatement) { s.Domain.Netuid++ },
		func(s *HotkeyNetworkDelegationStatement) { s.UserId[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.NetworkId[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentHeadHash[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentGeneration++ },
		func(s *HotkeyNetworkDelegationStatement) { s.Generation, s.PreviousHash = 2, [32]byte{16} },
		func(s *HotkeyNetworkDelegationStatement) { s.Nonce[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.IssuedAt++ },
		func(s *HotkeyNetworkDelegationStatement) { s.ExpiresAt-- },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch++ },
		func(s *HotkeyNetworkDelegationStatement) { s.ThroughEpoch++ },
	} {
		changed := statement
		mutate(&changed)
		if err := SignProspectiveHotkeyNetworkDelegation(&changed, networkWalletTestBoundary, operator); err != nil {
			t.Fatal(index, err)
		}
		message, err := changed.Message()
		if err != nil {
			t.Fatal(index, err)
		}
		value := original
		value.Message = message
		if result, _, err := VerifyHotkeyNetworkDelegation(t.Context(), value); result != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("changed delegation borrowed the original hotkey signature", index, err)
		}
	}
	for index, value := range []WalletMappingConsent{
		{Message: original.Message},
		{Message: original.Message, Signature: hotkeyWalletTestSign(t, 22, original.Message, false)},
		{Message: original.Message, Signature: hotkeyWalletTestSign(t, 11, original.Message, true)},
		{Message: original.Message, Signature: hotkeyWalletTestSign(t, 21, "<Bytes>"+original.Message, false)},
	} {
		if result, _, err := VerifyHotkeyNetworkDelegation(t.Context(), value); result != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("delegation verified without its own hotkey's signature", index, err)
		}
	}
	// the operator signature covers the displayed bytes too
	message := strings.Replace(original.Message, `"consent_generation":1`, `"consent_generation":2`, 1)
	if value, err := DecodeHotkeyNetworkDelegationStatement(message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("operator signature did not cover the consent generation", err)
	}
}

// Validity: a complete domain, nonzero identities, hotkey, consent head and
// nonce, a consent generation of at least 1, the lineage and generation
// bounds, the 300s acceptance window, the interval bounds and an interval
// after the operator boundary. A refused statement is left unchanged.
func TestHotkeyNetworkDelegationRefusesInvalidFields(t *testing.T) {
	operator, _ := networkWalletTestOperator(t)
	valid := hotkeyDelegationTestStatement([32]byte{15}, 1)
	valid.Hotkey = [32]byte{21}
	for index, mutate := range []func(*HotkeyNetworkDelegationStatement){
		func(s *HotkeyNetworkDelegationStatement) {},
		func(s *HotkeyNetworkDelegationStatement) { s.ExpiresAt = s.IssuedAt + 300 },
		func(s *HotkeyNetworkDelegationStatement) { s.ExpiresAt = s.IssuedAt + 1 },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch, s.ThroughEpoch = 51, 51 },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch, s.ThroughEpoch = 51, 51+65535 },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentGeneration = MaxWalletMappingHistory },
		func(s *HotkeyNetworkDelegationStatement) {
			s.Generation, s.PreviousHash = MaxWalletMappingHistory, [32]byte{16}
		},
	} {
		statement := valid
		mutate(&statement)
		if err := SignProspectiveHotkeyNetworkDelegation(&statement, networkWalletTestBoundary, operator); err != nil {
			t.Fatal("valid delegation was refused", index, err)
		}
		if _, err := statement.Message(); err != nil {
			t.Fatal("signed delegation does not encode", index, err)
		}
	}
	for index, mutate := range []func(*HotkeyNetworkDelegationStatement){
		func(s *HotkeyNetworkDelegationStatement) { s.Domain.NoID = 0 },
		func(s *HotkeyNetworkDelegationStatement) { s.Domain.Netuid = 0 },
		func(s *HotkeyNetworkDelegationStatement) { s.UserId = [16]byte{} },
		func(s *HotkeyNetworkDelegationStatement) { s.NetworkId = [16]byte{} },
		func(s *HotkeyNetworkDelegationStatement) { s.Hotkey = [32]byte{} },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentHeadHash = [32]byte{} },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentGeneration = 0 },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentGeneration = MaxWalletMappingHistory + 1 },
		func(s *HotkeyNetworkDelegationStatement) { s.Nonce = [32]byte{} },
		func(s *HotkeyNetworkDelegationStatement) { s.Generation = 0 },
		func(s *HotkeyNetworkDelegationStatement) {
			s.Generation, s.PreviousHash = MaxWalletMappingHistory+1, [32]byte{16}
		},
		// generation 1 with a predecessor, and a later generation without one
		func(s *HotkeyNetworkDelegationStatement) { s.PreviousHash = [32]byte{16} },
		func(s *HotkeyNetworkDelegationStatement) { s.Generation = 2 },
		func(s *HotkeyNetworkDelegationStatement) { s.IssuedAt = 0 },
		func(s *HotkeyNetworkDelegationStatement) { s.ExpiresAt = s.IssuedAt },
		func(s *HotkeyNetworkDelegationStatement) { s.ExpiresAt = s.IssuedAt + 301 },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch, s.ThroughEpoch = 60, 59 },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch, s.ThroughEpoch = 51, 51+65536 },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch = networkWalletTestBoundary.Epoch },
	} {
		statement := valid
		mutate(&statement)
		before := statement
		if err := SignProspectiveHotkeyNetworkDelegation(&statement, networkWalletTestBoundary, operator); !errors.Is(err, ErrWalletMappingIntegrity) || statement != before {
			t.Fatal("invalid delegation acquired operator approval or was changed", index, err)
		}
	}
	signed := valid
	if err := SignProspectiveHotkeyNetworkDelegation(&signed, networkWalletTestBoundary, operator); err != nil {
		t.Fatal(err)
	}
	for index, mutate := range []func(*HotkeyNetworkDelegationStatement){
		func(s *HotkeyNetworkDelegationStatement) { s.Schema = NetworkWalletMappingConsentSchema },
		func(s *HotkeyNetworkDelegationStatement) { s.Schema = HotkeyWalletMappingConsentSchema },
		func(s *HotkeyNetworkDelegationStatement) { s.Scope = NetworkWalletMappingScope },
		func(s *HotkeyNetworkDelegationStatement) { s.Scope = HotkeyWalletMappingScope },
	} {
		statement := signed
		mutate(&statement)
		if message, err := statement.Message(); message != "" || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("a delegation with another kind's schema or scope encoded", index, err)
		}
	}
	statement := valid
	if err := SignProspectiveHotkeyNetworkDelegation(&statement, ClientKeyEffectiveBoundary{Epoch: 50}, operator); !errors.Is(err, ErrWalletMappingIntegrity) || statement != valid {
		t.Fatal("an incomplete operator boundary was signed", err)
	}
	if err := SignProspectiveHotkeyNetworkDelegation(&statement, networkWalletTestBoundary, nil); !errors.Is(err, ErrWalletMappingUnavailable) || statement != valid {
		t.Fatal("a missing operator key signed", err)
	}
	if err := SignProspectiveHotkeyNetworkDelegation(nil, networkWalletTestBoundary, operator); !errors.Is(err, ErrWalletMappingUnavailable) {
		t.Fatal("a missing statement was signed", err)
	}
}

// After the operator signs, no displayed field can change under that
// signature, the operator's own approval included.
func TestHotkeyNetworkDelegationFieldsCannotBorrowOperatorSignature(t *testing.T) {
	statement := hotkeyDelegationTestStatement([32]byte{15}, 1)
	hotkeyDelegationTestOriginal(t, &statement, 21, false)
	for index, mutate := range []func(*HotkeyNetworkDelegationStatement){
		func(s *HotkeyNetworkDelegationStatement) { s.Hotkey[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentHeadHash[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.ConsentGeneration++ },
		func(s *HotkeyNetworkDelegationStatement) { s.NetworkId[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.Nonce[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.IssuedAt-- },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch++ },
		func(s *HotkeyNetworkDelegationStatement) { s.Prospective.Boundary.Hash[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.Prospective.Boundary.Block++ },
		func(s *HotkeyNetworkDelegationStatement) { s.Prospective.Signer = common.Address{99} },
	} {
		changed := statement
		mutate(&changed)
		if _, err := changed.Message(); !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("modified delegation borrowed the operator signature", index, err)
		}
	}
	// the operator digest names this kind, so a network consent's operator
	// signature never approves a delegation
	network := networkWalletTestStatement()
	networkWalletTestOriginal(t, &network, 11, false)
	borrowed := statement
	borrowed.Prospective = network.Prospective
	if _, err := borrowed.Message(); !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a network consent's operator approval signed a delegation", err)
	}
}

// Unknown, duplicate and case-variant keys, reordered fields, other spellings,
// trailing data and login text never decode.
func TestHotkeyNetworkDelegationDecodeRefusesNoncanonicalMessages(t *testing.T) {
	statement := hotkeyDelegationTestStatement([32]byte{15}, 1)
	original, _ := hotkeyDelegationTestOriginal(t, &statement, 21, false)
	body := strings.TrimPrefix(original.Message, HotkeyNetworkDelegationPrefix)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	// a map encodes its keys sorted, so this reorders the same values
	reordered, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	for index, message := range []string{
		"",
		"Sign in to URnetwork\nChallenge: synthetic\nTimestamp: 1000",
		body,
		HotkeyNetworkDelegationPrefix + " " + body,
		HotkeyNetworkDelegationPrefix + string(reordered),
		original.Message + " ",
		original.Message + "{}",
		strings.Replace(original.Message, `"schema":`, `"extra":0,"schema":`, 1),
		strings.Replace(original.Message, `"block":500`, `"block":500,"extra":0`, 1),
		strings.Replace(original.Message, `"scope":"hotkey-network"`, `"scope":"hotkey-network","scope":"hotkey-network"`, 1),
		strings.Replace(original.Message, `"scope":"hotkey-network"`, `"scope":"hotkey-network","SCOPE":"hotkey-network"`, 1),
		strings.Replace(original.Message, `"hotkey":`, `"Hotkey":`, 1),
		// the key spelled with a JSON escape
		strings.Replace(original.Message, `"hotkey":`, `"`+string(rune(92))+`u0068otkey":`, 1),
		strings.Replace(original.Message, `"consent_generation":1`, `"consent_generation":1.0`, 1),
		strings.Replace(original.Message, `":`, `": `, 1),
		HotkeyNetworkDelegationPrefix + strings.Repeat(" ", MaxWalletMappingMessageBytes) + body,
	} {
		if value, err := DecodeHotkeyNetworkDelegationStatement(message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("noncanonical delegation decoded", index, err)
		}
		value := original
		value.Message = message
		if result, _, err := VerifyHotkeyNetworkDelegation(t.Context(), value); result != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("noncanonical delegation original verified", index, err)
		}
	}
}

// The widest valid original, every number at its widest spelling, stays within
// the message bound and the per-original bound that transport readers apply,
// and a signed one at those widths verifies. Past either bound is refused.
func TestHotkeyNetworkDelegationOriginalFitsTransportBound(t *testing.T) {
	var wide [32]byte
	var wideIdentity [16]byte
	var wideSignature [64]byte
	var wideOperatorSignature [65]byte
	for index := range wide {
		wide[index] = 255
	}
	for index := range wideIdentity {
		wideIdentity[index] = 255
	}
	for index := range wideSignature {
		wideSignature[index] = 255
	}
	for index := range wideOperatorSignature {
		wideOperatorSignature[index] = 255
	}
	statement := HotkeyNetworkDelegationStatement{
		Domain:            ClientKeyHistoryDomain{ChainID: math.MaxUint64, GenesisHash: wide, Netuid: math.MaxUint16, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: wide, PolicyHash: wide, NoID: math.MaxUint64},
		UserId:            wideIdentity,
		NetworkId:         wideIdentity,
		Hotkey:            hotkeyWalletTestKey(t, 21).Public().Encode(),
		ConsentHeadHash:   wide,
		ConsentGeneration: MaxWalletMappingHistory,
		Generation:        MaxWalletMappingHistory,
		PreviousHash:      wide,
		Nonce:             wide,
		IssuedAt:          math.MaxInt64 - 300,
		ExpiresAt:         math.MaxInt64,
		FromEpoch:         math.MaxUint64 - 65535,
		ThroughEpoch:      math.MaxUint64,
	}
	operator, _ := networkWalletTestOperator(t)
	if err := SignProspectiveHotkeyNetworkDelegation(&statement, ClientKeyEffectiveBoundary{Epoch: math.MaxUint64 - 65536, Block: math.MaxUint64, Hash: wide}, operator); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	original := WalletMappingConsent{Message: message, Signature: hotkeyWalletTestSign(t, 21, message, true)}
	if decoded, _, err := VerifyHotkeyNetworkDelegation(t.Context(), original); err != nil || decoded == nil || *decoded != statement {
		t.Fatal("a signed delegation at the widest field spellings was refused", err)
	}

	// every byte at its widest spelling, signatures included
	widest := statement
	widest.Hotkey, widest.Prospective.Signature = wide, wideOperatorSignature
	widestRaw, err := json.Marshal(widest)
	if err != nil {
		t.Fatal(err)
	}
	widestMessage := HotkeyNetworkDelegationPrefix + string(widestRaw)
	raw, err := json.Marshal(WalletMappingConsent{Message: widestMessage, Signature: wideSignature})
	if err != nil || len(widestMessage) > MaxWalletMappingMessageBytes || len(raw) > MaxWalletMappingConsentBytes {
		t.Fatal("the widest delegation original exceeds a bound", len(widestMessage), len(raw), err)
	}

	pastMessage := original
	pastMessage.Message += strings.Repeat(" ", MaxWalletMappingMessageBytes+1-len(original.Message))
	raw, err = json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	pastOriginal := original
	pastOriginal.Message += strings.Repeat(" ", MaxWalletMappingConsentBytes+1-len(raw))
	pastRaw, err := json.Marshal(pastOriginal)
	if err != nil || len(pastMessage.Message) != MaxWalletMappingMessageBytes+1 || len(pastRaw) != MaxWalletMappingConsentBytes+1 {
		t.Fatal("bound fixtures are not one byte past their bounds", len(pastMessage.Message), len(pastRaw), err)
	}
	for _, value := range []WalletMappingConsent{pastMessage, pastOriginal} {
		if decoded, _, err := VerifyHotkeyNetworkDelegation(t.Context(), value); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("a delegation original past its bound verified", len(value.Message), err)
		}
	}
}

// Generations select by epoch through the pinned head, and the network may
// move to another hotkey, which signs its own generation. A verified chain
// with no effective delegation is distinct from missing originals.
func TestHotkeyNetworkDelegationHistorySelectsEffectiveDelegation(t *testing.T) {
	first := hotkeyDelegationTestStatement([32]byte{15}, 1)
	old, oldHash := hotkeyDelegationTestOriginal(t, &first, 21, false)
	second := hotkeyDelegationTestStatement([32]byte{16}, 2)
	second.Generation, second.PreviousHash, second.FromEpoch, second.ThroughEpoch = 2, oldHash, 60, 70
	second.Nonce[0]++
	newer, newHash := hotkeyDelegationTestOriginal(t, &second, 22, true)
	if first.Hotkey == second.Hotkey {
		t.Fatal("the delegation fixture does not move to another hotkey")
	}
	history := []WalletMappingConsent{old, newer}
	expected := HotkeyNetworkDelegationHistoryExpectation{Domain: first.Domain, NetworkId: first.NetworkId, HeadHash: newHash, Generation: 2}
	for _, c := range []struct {
		epoch     uint64
		statement HotkeyNetworkDelegationStatement
		hash      [32]byte
	}{
		{epoch: 51, statement: first, hash: oldHash},
		{epoch: 59, statement: first, hash: oldHash},
		{epoch: 60, statement: second, hash: newHash},
		{epoch: 70, statement: second, hash: newHash},
	} {
		expected.Epoch = c.epoch
		delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), history, expected)
		if err != nil || delegation == nil || delegation.Statement != c.statement || delegation.OriginalHash != c.hash || delegation.HeadHash != newHash || delegation.Generation != 2 {
			t.Fatal("delegation selection differs", c.epoch, delegation, err)
		}
	}
	// before the first delegation and after the newest one ends
	for _, epoch := range []uint64{50, 71} {
		expected.Epoch = epoch
		if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), history, expected); delegation != nil || !errors.Is(err, ErrWalletMappingNotEffective) || !errors.Is(err, ErrWalletMappingUnavailable) {
			t.Fatal("an ineffective delegation was selected", epoch, err)
		}
	}
	// missing originals are unavailable, never not effective
	expected.Epoch = 60
	for _, partial := range [][]WalletMappingConsent{{old}, {newer}, nil} {
		if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), partial, expected); delegation != nil || !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingNotEffective) || errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("missing delegation originals read as not effective", err)
		}
	}
	for _, partial := range [][]WalletMappingConsent{{newer, old}, {old, old}} {
		if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), partial, expected); delegation != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("broken delegation lineage was admitted", err)
		}
	}
	for index, mutate := range []func(*HotkeyNetworkDelegationHistoryExpectation){
		func(e *HotkeyNetworkDelegationHistoryExpectation) { e.HeadHash = oldHash },
		func(e *HotkeyNetworkDelegationHistoryExpectation) { e.Generation, e.HeadHash = 1, oldHash },
		func(e *HotkeyNetworkDelegationHistoryExpectation) { e.NetworkId = [16]byte{10} },
		func(e *HotkeyNetworkDelegationHistoryExpectation) { e.NetworkId = [16]byte{} },
		func(e *HotkeyNetworkDelegationHistoryExpectation) { e.Domain.NoID++ },
		func(e *HotkeyNetworkDelegationHistoryExpectation) { e.Domain = ClientKeyHistoryDomain{} },
	} {
		changed := expected
		mutate(&changed)
		if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), history, changed); delegation != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("a contradicting delegation expectation was admitted", index, err)
		}
	}
	changed := expected
	changed.Generation = MaxWalletMappingHistory + 1
	if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), history, changed); delegation != nil || !errors.Is(err, ErrWalletMappingCapacity) {
		t.Fatal("a pinned generation past the history bound was not capacity", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if delegation, err := VerifyHotkeyNetworkDelegationHistory(ctx, history, expected); delegation != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("a canceled delegation history published a delegation", err)
	}
}

// Forks, a broken predecessor and a first epoch that does not advance are
// refused, and a chain stays within one (domain, network).
func TestHotkeyNetworkDelegationLineageRefusals(t *testing.T) {
	first := hotkeyDelegationTestStatement([32]byte{15}, 1)
	first.FromEpoch = 55
	old, oldHash := hotkeyDelegationTestOriginal(t, &first, 21, false)
	successor := func(mutate func(*HotkeyNetworkDelegationStatement)) (WalletMappingConsent, [32]byte) {
		statement := first
		statement.Generation, statement.PreviousHash, statement.FromEpoch, statement.ThroughEpoch = 2, oldHash, 60, 70
		statement.Nonce[0]++
		mutate(&statement)
		return hotkeyDelegationTestOriginal(t, &statement, 21, false)
	}
	newer, _ := successor(func(s *HotkeyNetworkDelegationStatement) {})
	fork, forkHash := successor(func(s *HotkeyNetworkDelegationStatement) { s.Nonce[1]++ })
	expected := HotkeyNetworkDelegationHistoryExpectation{Domain: first.Domain, NetworkId: first.NetworkId, HeadHash: forkHash, Generation: 3, Epoch: 60}
	if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), []WalletMappingConsent{old, newer, fork}, expected); delegation != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a forked delegation lineage was admitted", err)
	}
	expected.Generation = 2
	if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), []WalletMappingConsent{old, newer}, expected); delegation != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("the other branch of a fork matched the pinned head", err)
	}
	for index, mutate := range []func(*HotkeyNetworkDelegationStatement){
		func(s *HotkeyNetworkDelegationStatement) { s.PreviousHash = [32]byte{99} },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch = first.FromEpoch },
		func(s *HotkeyNetworkDelegationStatement) { s.FromEpoch = first.FromEpoch - 1 },
		func(s *HotkeyNetworkDelegationStatement) { s.NetworkId[0]++ },
		func(s *HotkeyNetworkDelegationStatement) { s.Domain.NoID++ },
	} {
		original, hash := successor(mutate)
		expected := HotkeyNetworkDelegationHistoryExpectation{Domain: first.Domain, NetworkId: first.NetworkId, HeadHash: hash, Generation: 2, Epoch: 60}
		if delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), []WalletMappingConsent{old, original}, expected); delegation != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("a broken delegation lineage was admitted", index, err)
		}
	}
}

// The earning gate requires the operator signer, a boundary before the window
// and an acceptance that expired before the window started.
func TestHotkeyNetworkDelegationProspectiveGate(t *testing.T) {
	statement := hotkeyDelegationTestStatement([32]byte{15}, 1)
	original, hash := hotkeyDelegationTestOriginal(t, &statement, 21, false)
	delegation, err := VerifyHotkeyNetworkDelegationHistory(t.Context(), []WalletMappingConsent{original}, HotkeyNetworkDelegationHistoryExpectation{Domain: statement.Domain, NetworkId: statement.NetworkId, HeadHash: hash, Generation: 1, Epoch: 51})
	if err != nil {
		t.Fatal(err)
	}
	_, signer := networkWalletTestOperator(t)
	for _, c := range []struct {
		block uint64
		clock int64
	}{{block: 510, clock: 1400}, {block: 501, clock: 1300}} {
		if err := VerifyProspectiveHotkeyNetworkDelegation(t.Context(), delegation, signer, c.block, c.clock); err != nil {
			t.Fatal("approved delegation was refused", c, err)
		}
	}
	if err := VerifyProspectiveHotkeyNetworkDelegation(t.Context(), delegation, common.Address{99}, 510, 1400); !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a foreign operator signer was accepted", err)
	}
	for _, block := range []uint64{500, 499} {
		if err := VerifyProspectiveHotkeyNetworkDelegation(t.Context(), delegation, signer, block, 1400); !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("a boundary inside the window was accepted", block, err)
		}
	}
	for _, clock := range []int64{1002, 1299} {
		if err := VerifyProspectiveHotkeyNetworkDelegation(t.Context(), delegation, signer, 510, clock); !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingNotEffective) || errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("an acceptance that may fall inside the window became known", clock, err)
		}
	}
	if err := VerifyProspectiveHotkeyNetworkDelegation(t.Context(), delegation, common.Address{99}, 510, 1002); !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("unknown acceptance time hid a foreign operator signer", err)
	}
	for index, c := range []struct {
		delegation *VerifiedHotkeyNetworkDelegation
		signer     common.Address
		block      uint64
		clock      int64
	}{
		{delegation: nil, signer: signer, block: 510, clock: 1400},
		{delegation: delegation, signer: common.Address{}, block: 510, clock: 1400},
		{delegation: delegation, signer: signer, block: 0, clock: 1400},
		{delegation: delegation, signer: signer, block: 510, clock: 0},
	} {
		if err := VerifyProspectiveHotkeyNetworkDelegation(t.Context(), c.delegation, c.signer, c.block, c.clock); !errors.Is(err, ErrWalletMappingUnavailable) {
			t.Fatal("a missing gate input was not unavailable", index, err)
		}
	}
	altered := *delegation
	altered.Statement.FromEpoch++
	if err := VerifyProspectiveHotkeyNetworkDelegation(t.Context(), &altered, signer, 510, 1400); !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("an altered delegation passed the gate", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := VerifyProspectiveHotkeyNetworkDelegation(ctx, delegation, signer, 510, 1400); !errors.Is(err, context.Canceled) {
		t.Fatal("a canceled gate passed", err)
	}
}

// The operator approval is the same recoverable signature a network consent
// carries, from the same synthetic operator key.
func TestHotkeyNetworkDelegationOperatorSignerIsRecoverable(t *testing.T) {
	statement := hotkeyDelegationTestStatement([32]byte{15}, 1)
	hotkeyDelegationTestOriginal(t, &statement, 21, false)
	operator, signer := networkWalletTestOperator(t)
	if statement.Prospective.Signer != signer || statement.Prospective.Signer != crypto.PubkeyToAddress(operator.PublicKey) || statement.Prospective.Boundary != networkWalletTestBoundary {
		t.Fatal("the delegation does not carry the operator approval", statement.Prospective)
	}
	if err := statement.VerifyProspectiveSignature(); err != nil {
		t.Fatal(err)
	}
}
