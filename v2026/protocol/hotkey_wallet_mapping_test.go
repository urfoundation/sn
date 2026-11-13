// The global hotkey consent signed by both its coldkey and its hotkey, and its
// lineage. Synthetic sr25519 keys from fixed seeds sign the same substrate
// transcripts as wallets, and every identity is visibly synthetic.
package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
)

// The synthetic sr25519 key for a seed.
func hotkeyWalletTestKey(t testing.TB, seed byte) *schnorrkel.MiniSecretKey {
	t.Helper()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	return mini
}

// The seed's signature over the exact message, raw or in the Bytes wrapper.
func hotkeyWalletTestSign(t testing.TB, seed byte, message string, wrapped bool) [64]byte {
	t.Helper()
	payload := message
	if wrapped {
		payload = "<Bytes>" + message + "</Bytes>"
	}
	signature, err := hotkeyWalletTestKey(t, seed).ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	return signature.Encode()
}

// Every field is visibly synthetic; the subnet is the test operator domain's.
func hotkeyWalletTestStatement() HotkeyWalletMappingStatement {
	return HotkeyWalletMappingStatement{Schema: HotkeyWalletMappingConsentSchema, Scope: HotkeyWalletMappingScope, Subnet: walletMappingTestStatement().Domain.HotkeySubnet(), Nonce: [32]byte{13}, Generation: 1, IssuedAt: 1000, FromEpoch: 0, ThroughEpoch: 1000}
}

// Sets the coldkey and the hotkey from their seeds, has both sign the message,
// raw or in the Bytes wrapper, and returns the original and its hash.
func hotkeyWalletTestOriginal(t testing.TB, statement *HotkeyWalletMappingStatement, coldkeySeed byte, hotkeySeed byte, wrapped bool) (HotkeyWalletMappingConsent, [32]byte) {
	t.Helper()
	statement.Coldkey = hotkeyWalletTestKey(t, coldkeySeed).Public().Encode()
	statement.Hotkey = hotkeyWalletTestKey(t, hotkeySeed).Public().Encode()
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	original := HotkeyWalletMappingConsent{Message: message, ColdkeySignature: hotkeyWalletTestSign(t, coldkeySeed, message, wrapped), HotkeySignature: hotkeyWalletTestSign(t, hotkeySeed, message, wrapped)}
	_, hash, err := VerifyHotkeyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

// The next generation of a chain: the predecessor's hash, a fresh nonce and a
// later first epoch.
func hotkeyWalletTestSuccessor(previous HotkeyWalletMappingStatement, previousHash [32]byte, fromEpoch uint64, throughEpoch uint64) HotkeyWalletMappingStatement {
	next := previous
	next.Generation, next.PreviousHash, next.FromEpoch, next.ThroughEpoch = previous.Generation+1, previousHash, fromEpoch, throughEpoch
	next.Nonce[1]++
	return next
}

// The displayed bytes are the first line, then every field in declaration
// order, with byte arrays as JSON number arrays. Decoding returns the same
// statement and re-encodes to the same bytes.
func TestHotkeyWalletMappingMessageExactBytes(t *testing.T) {
	statement := hotkeyWalletTestStatement()
	statement.Hotkey, statement.Coldkey = [32]byte{21}, [32]byte{11}
	array := func(first byte) string {
		return fmt.Sprintf("[%d%s]", first, strings.Repeat(",0", 31))
	}
	want := "Approve URnetwork hotkey wallet mapping\n" + `{"schema":"urnetwork-hotkey-wallet-mapping-consent-v1","scope":"hotkey","subnet":{"chain_id":964,"genesis_hash":` + array(1) + `,"netuid":25},"hotkey":` + array(21) + `,"coldkey":` + array(11) + `,"generation":1,"previous_hash":` + array(0) + `,"nonce":` + array(13) + `,"issued_at":1000,"from_epoch":0,"through_epoch":1000}`
	message, err := statement.Message()
	if err != nil || message != want {
		t.Fatalf("hotkey message differs:\n%q\n%q\n%v", message, want, err)
	}
	decoded, err := DecodeHotkeyWalletMappingStatement(message)
	if err != nil || decoded == nil || *decoded != statement {
		t.Fatal("hotkey statement did not round trip", decoded, err)
	}
	again, err := decoded.Message()
	if err != nil || again != message {
		t.Fatal("decoded hotkey statement re-encoded differently", err)
	}
}

// Signed originals round trip through the verifier, raw and wrapped.
func TestHotkeyWalletMappingOriginalRoundTrip(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		statement := hotkeyWalletTestStatement()
		original, hash := hotkeyWalletTestOriginal(t, &statement, 11, 21, wrapped)
		decoded, decodedHash, err := VerifyHotkeyWalletMappingConsent(t.Context(), original)
		if err != nil || decoded == nil || *decoded != statement || decodedHash != hash {
			t.Fatal("hotkey original was not reconstructed", wrapped, decoded, err)
		}
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var copied HotkeyWalletMappingConsent
		if err := json.Unmarshal(raw, &copied); err != nil || copied != original {
			t.Fatal("hotkey original did not survive its JSON encoding", err)
		}
	}
}

// Each signature is the statement's own key's, over the exact message, raw or
// wrapped independently of the other. One signature alone, the two exchanged
// or another key's signature never verify.
func TestHotkeyWalletMappingConsentNeedsBothSignatures(t *testing.T) {
	statement := hotkeyWalletTestStatement()
	original, _ := hotkeyWalletTestOriginal(t, &statement, 11, 21, false)
	for _, c := range []struct {
		coldkeyWrapped bool
		hotkeyWrapped  bool
	}{{coldkeyWrapped: false, hotkeyWrapped: false}, {coldkeyWrapped: false, hotkeyWrapped: true}, {coldkeyWrapped: true, hotkeyWrapped: false}, {coldkeyWrapped: true, hotkeyWrapped: true}} {
		value := HotkeyWalletMappingConsent{Message: original.Message, ColdkeySignature: hotkeyWalletTestSign(t, 11, original.Message, c.coldkeyWrapped), HotkeySignature: hotkeyWalletTestSign(t, 21, original.Message, c.hotkeyWrapped)}
		if decoded, _, err := VerifyHotkeyWalletMappingConsent(t.Context(), value); err != nil || decoded == nil || *decoded != statement {
			t.Fatal("hotkey consent signature encoding was refused", c, err)
		}
	}
	for index, value := range []HotkeyWalletMappingConsent{
		// coldkey only
		{Message: original.Message, ColdkeySignature: original.ColdkeySignature},
		// hotkey only
		{Message: original.Message, HotkeySignature: original.HotkeySignature},
		// swapped
		{Message: original.Message, ColdkeySignature: original.HotkeySignature, HotkeySignature: original.ColdkeySignature},
		// one key in both places
		{Message: original.Message, ColdkeySignature: original.ColdkeySignature, HotkeySignature: original.ColdkeySignature},
		{Message: original.Message, ColdkeySignature: original.HotkeySignature, HotkeySignature: original.HotkeySignature},
		// another coldkey or hotkey over the same message
		{Message: original.Message, ColdkeySignature: hotkeyWalletTestSign(t, 12, original.Message, false), HotkeySignature: original.HotkeySignature},
		{Message: original.Message, ColdkeySignature: original.ColdkeySignature, HotkeySignature: hotkeyWalletTestSign(t, 22, original.Message, false)},
		// a wrapper the wallets do not apply
		{Message: original.Message, ColdkeySignature: hotkeyWalletTestSign(t, 11, "<Bytes>"+original.Message, false), HotkeySignature: original.HotkeySignature},
	} {
		if decoded, _, err := VerifyHotkeyWalletMappingConsent(t.Context(), value); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("hotkey consent verified without both of its own signatures", index, err)
		}
	}
}

// Both signatures cover the subnet, both keys, the lineage, the nonce, the
// issuance time and the interval.
func TestHotkeyWalletMappingSignaturesBindEveryField(t *testing.T) {
	statement := hotkeyWalletTestStatement()
	original, _ := hotkeyWalletTestOriginal(t, &statement, 11, 21, false)
	for index, mutate := range []func(*HotkeyWalletMappingStatement){
		func(s *HotkeyWalletMappingStatement) { s.Subnet.ChainID++ },
		func(s *HotkeyWalletMappingStatement) { s.Subnet.GenesisHash[0]++ },
		func(s *HotkeyWalletMappingStatement) { s.Subnet.Netuid++ },
		func(s *HotkeyWalletMappingStatement) { s.Hotkey = hotkeyWalletTestKey(t, 22).Public().Encode() },
		func(s *HotkeyWalletMappingStatement) { s.Coldkey = hotkeyWalletTestKey(t, 12).Public().Encode() },
		func(s *HotkeyWalletMappingStatement) { s.Generation, s.PreviousHash = 2, [32]byte{14} },
		func(s *HotkeyWalletMappingStatement) { s.Nonce[0]++ },
		func(s *HotkeyWalletMappingStatement) { s.IssuedAt++ },
		func(s *HotkeyWalletMappingStatement) { s.FromEpoch++ },
		func(s *HotkeyWalletMappingStatement) { s.ThroughEpoch++ },
	} {
		changed := statement
		mutate(&changed)
		message, err := changed.Message()
		if err != nil {
			t.Fatal(index, err)
		}
		value := original
		value.Message = message
		if decoded, _, err := VerifyHotkeyWalletMappingConsent(t.Context(), value); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("changed hotkey statement borrowed the original signatures", index, err)
		}
	}
}

// Field validity: the exact schema and scope, a complete subnet, nonzero keys
// and nonce, a generation within the history with a predecessor exactly after
// generation 1, a positive issuance time and the interval bounds.
func TestHotkeyWalletMappingRefusesInvalidFields(t *testing.T) {
	valid := hotkeyWalletTestStatement()
	valid.Hotkey, valid.Coldkey = [32]byte{21}, [32]byte{11}
	for index, mutate := range []func(*HotkeyWalletMappingStatement){
		func(s *HotkeyWalletMappingStatement) {},
		func(s *HotkeyWalletMappingStatement) { s.FromEpoch, s.ThroughEpoch = 5, 5 },
		func(s *HotkeyWalletMappingStatement) { s.FromEpoch, s.ThroughEpoch = 0, 65535 },
		func(s *HotkeyWalletMappingStatement) {
			s.FromEpoch, s.ThroughEpoch = math.MaxUint64-65535, math.MaxUint64
		},
		func(s *HotkeyWalletMappingStatement) {
			s.Generation, s.PreviousHash = MaxWalletMappingHistory, [32]byte{14}
		},
	} {
		statement := valid
		mutate(&statement)
		if _, err := statement.Message(); err != nil {
			t.Fatal("valid hotkey statement was refused", index, err)
		}
	}
	for index, mutate := range []func(*HotkeyWalletMappingStatement){
		func(s *HotkeyWalletMappingStatement) { s.Schema = "" },
		func(s *HotkeyWalletMappingStatement) { s.Schema = NetworkWalletMappingConsentSchema },
		func(s *HotkeyWalletMappingStatement) { s.Schema = HotkeyNetworkDelegationSchema },
		func(s *HotkeyWalletMappingStatement) { s.Scope = "" },
		func(s *HotkeyWalletMappingStatement) { s.Scope = NetworkWalletMappingScope },
		func(s *HotkeyWalletMappingStatement) { s.Scope = HotkeyNetworkDelegationScope },
		func(s *HotkeyWalletMappingStatement) { s.Subnet.ChainID = 0 },
		func(s *HotkeyWalletMappingStatement) { s.Subnet.GenesisHash = [32]byte{} },
		func(s *HotkeyWalletMappingStatement) { s.Subnet.Netuid = 0 },
		func(s *HotkeyWalletMappingStatement) { s.Hotkey = [32]byte{} },
		func(s *HotkeyWalletMappingStatement) { s.Coldkey = [32]byte{} },
		func(s *HotkeyWalletMappingStatement) { s.Nonce = [32]byte{} },
		func(s *HotkeyWalletMappingStatement) { s.Generation = 0 },
		func(s *HotkeyWalletMappingStatement) {
			s.Generation, s.PreviousHash = MaxWalletMappingHistory+1, [32]byte{14}
		},
		// generation 1 with a predecessor, and a later generation without one
		func(s *HotkeyWalletMappingStatement) { s.PreviousHash = [32]byte{14} },
		func(s *HotkeyWalletMappingStatement) { s.Generation = 2 },
		func(s *HotkeyWalletMappingStatement) { s.IssuedAt = 0 },
		func(s *HotkeyWalletMappingStatement) { s.IssuedAt = -1 },
		func(s *HotkeyWalletMappingStatement) { s.FromEpoch, s.ThroughEpoch = 6, 5 },
		func(s *HotkeyWalletMappingStatement) { s.FromEpoch, s.ThroughEpoch = 0, 65536 },
		func(s *HotkeyWalletMappingStatement) {
			s.FromEpoch, s.ThroughEpoch = math.MaxUint64-65536, math.MaxUint64
		},
	} {
		statement := valid
		mutate(&statement)
		if message, err := statement.Message(); message != "" || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("invalid hotkey statement was encoded", index, err)
		}
	}
}

// Unknown, duplicate, case-variant and escaped keys, reordered fields, other
// spellings, trailing data and login text never decode.
func TestHotkeyWalletMappingDecodeRefusesNoncanonicalMessages(t *testing.T) {
	statement := hotkeyWalletTestStatement()
	original, _ := hotkeyWalletTestOriginal(t, &statement, 11, 21, false)
	body := strings.TrimPrefix(original.Message, HotkeyWalletMappingConsentPrefix)
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
		"Approve URnetwork hotkey wallet mapping\r\n" + body,
		HotkeyWalletMappingConsentPrefix + " " + body,
		HotkeyWalletMappingConsentPrefix + string(reordered),
		original.Message + " ",
		original.Message + "\n",
		original.Message + "{}",
		strings.Replace(original.Message, `"schema":`, `"extra":0,"schema":`, 1),
		strings.Replace(original.Message, `"netuid":25`, `"netuid":25,"extra":0`, 1),
		strings.Replace(original.Message, `"scope":"hotkey"`, `"scope":"hotkey","scope":"hotkey"`, 1),
		strings.Replace(original.Message, `"scope":"hotkey"`, `"scope":"hotkey","Scope":"hotkey"`, 1),
		strings.Replace(original.Message, `"netuid":25`, `"netuid":25,"netuid":25`, 1),
		strings.Replace(original.Message, `"schema":`, `"Schema":`, 1),
		// the key spelled with a JSON escape
		strings.Replace(original.Message, `"schema":`, `"`+string(rune(92))+`u0073chema":`, 1),
		strings.Replace(original.Message, `"issued_at":1000`, `"issued_at":1000.0`, 1),
		strings.Replace(original.Message, `"issued_at":1000`, `"issued_at":1e3`, 1),
		strings.Replace(original.Message, `"generation":1`, `"generation":01`, 1),
		strings.Replace(original.Message, `":`, `": `, 1),
		strings.Replace(original.Message, `"from_epoch":0,`, ``, 1),
		HotkeyWalletMappingConsentPrefix + strings.Repeat(" ", MaxWalletMappingMessageBytes) + body,
	} {
		if value, err := DecodeHotkeyWalletMappingStatement(message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("noncanonical hotkey statement decoded", index, err)
		}
		value := original
		value.Message = message
		if decoded, _, err := VerifyHotkeyWalletMappingConsent(t.Context(), value); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("noncanonical hotkey original verified", index, err)
		}
	}
}

// The widest valid original, every number at its widest spelling, stays within
// the message bound and the per-original bound that transport readers apply,
// and a signed one at those widths verifies. Past either bound is refused.
func TestHotkeyWalletMappingOriginalFitsTransportBound(t *testing.T) {
	var wide [32]byte
	var wideSignature [64]byte
	for index := range wide {
		wide[index] = 255
	}
	for index := range wideSignature {
		wideSignature[index] = 255
	}
	widest := HotkeyWalletMappingStatement{Schema: HotkeyWalletMappingConsentSchema, Scope: HotkeyWalletMappingScope, Subnet: HotkeyWalletMappingSubnet{ChainID: math.MaxUint64, GenesisHash: wide, Netuid: math.MaxUint16}, Hotkey: wide, Coldkey: wide, Generation: MaxWalletMappingHistory, PreviousHash: wide, Nonce: wide, IssuedAt: math.MaxInt64, FromEpoch: math.MaxUint64 - 65535, ThroughEpoch: math.MaxUint64}
	message, err := widest.Message()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(HotkeyWalletMappingConsent{Message: message, ColdkeySignature: wideSignature, HotkeySignature: wideSignature})
	if err != nil || len(message) > MaxWalletMappingMessageBytes || len(raw) > MaxWalletMappingConsentBytes {
		t.Fatal("the widest hotkey original exceeds a bound", len(message), len(raw), err)
	}

	signed := widest
	original, _ := hotkeyWalletTestOriginal(t, &signed, 11, 21, true)
	if decoded, _, err := VerifyHotkeyWalletMappingConsent(t.Context(), original); err != nil || decoded == nil || *decoded != signed {
		t.Fatal("a signed hotkey original at the widest field spellings was refused", err)
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
	for _, value := range []HotkeyWalletMappingConsent{pastMessage, pastOriginal} {
		if decoded, _, err := VerifyHotkeyWalletMappingConsent(t.Context(), value); decoded != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("a hotkey original past its bound verified", len(value.Message), err)
		}
	}
}

// A two-generation chain whose second generation moves the hotkey's earnings
// to another coldkey from epoch 60.
type hotkeyWalletTestChain struct {
	first          HotkeyWalletMappingStatement
	firstOriginal  HotkeyWalletMappingConsent
	firstHash      [32]byte
	second         HotkeyWalletMappingStatement
	secondOriginal HotkeyWalletMappingConsent
	secondHash     [32]byte
}

func newHotkeyWalletTestChain(t testing.TB) hotkeyWalletTestChain {
	t.Helper()
	first := hotkeyWalletTestStatement()
	first.FromEpoch, first.ThroughEpoch = 10, 100
	firstOriginal, firstHash := hotkeyWalletTestOriginal(t, &first, 11, 21, false)
	second := hotkeyWalletTestSuccessor(first, firstHash, 60, 70)
	secondOriginal, secondHash := hotkeyWalletTestOriginal(t, &second, 12, 21, true)
	return hotkeyWalletTestChain{first: first, firstOriginal: firstOriginal, firstHash: firstHash, second: second, secondOriginal: secondOriginal, secondHash: secondHash}
}

// Both originals in order.
func (self hotkeyWalletTestChain) originals() []HotkeyWalletMappingConsent {
	return []HotkeyWalletMappingConsent{self.firstOriginal, self.secondOriginal}
}

// The complete chain from generation 1 verifies to its head; the coldkey may
// change between generations.
func TestHotkeyWalletMappingLineageAllowsColdkeyReplacement(t *testing.T) {
	chain := newHotkeyWalletTestChain(t)
	if chain.first.Coldkey == chain.second.Coldkey {
		t.Fatal("the chain fixture does not replace its coldkey")
	}
	head, headHash, err := VerifyHotkeyWalletMappingLineage(t.Context(), chain.originals())
	if err != nil || head == nil || *head != chain.second || headHash != chain.secondHash {
		t.Fatal("coldkey replacement was refused", head, err)
	}
	head, headHash, err = VerifyHotkeyWalletMappingLineage(t.Context(), []HotkeyWalletMappingConsent{chain.firstOriginal})
	if err != nil || head == nil || *head != chain.first || headHash != chain.firstHash {
		t.Fatal("the first generation alone is not its own lineage", head, err)
	}
}

// Forks, a broken predecessor, a first epoch that does not advance, and a
// changed subnet or hotkey are refused; so is a chain that does not start at
// generation 1.
func TestHotkeyWalletMappingLineageRefusals(t *testing.T) {
	chain := newHotkeyWalletTestChain(t)
	successor := func(hotkeySeed byte, mutate func(*HotkeyWalletMappingStatement)) HotkeyWalletMappingConsent {
		statement := hotkeyWalletTestSuccessor(chain.first, chain.firstHash, 60, 70)
		mutate(&statement)
		original, _ := hotkeyWalletTestOriginal(t, &statement, 12, hotkeySeed, false)
		return original
	}
	fork := successor(21, func(s *HotkeyWalletMappingStatement) { s.Nonce[2]++ })
	for index, originals := range [][]HotkeyWalletMappingConsent{
		{chain.firstOriginal, chain.secondOriginal, fork},
		{chain.firstOriginal, chain.firstOriginal},
		{chain.secondOriginal},
		{chain.secondOriginal, chain.firstOriginal},
		{chain.firstOriginal, successor(21, func(s *HotkeyWalletMappingStatement) { s.PreviousHash = [32]byte{99} })},
		{chain.firstOriginal, successor(21, func(s *HotkeyWalletMappingStatement) { s.FromEpoch = chain.first.FromEpoch })},
		{chain.firstOriginal, successor(21, func(s *HotkeyWalletMappingStatement) { s.FromEpoch = chain.first.FromEpoch - 1 })},
		{chain.firstOriginal, successor(21, func(s *HotkeyWalletMappingStatement) { s.Subnet.Netuid++ })},
		{chain.firstOriginal, successor(21, func(s *HotkeyWalletMappingStatement) { s.Subnet.ChainID++ })},
		{chain.firstOriginal, successor(22, func(s *HotkeyWalletMappingStatement) {})},
	} {
		if head, _, err := VerifyHotkeyWalletMappingLineage(t.Context(), originals); head != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("broken hotkey lineage was admitted", index, err)
		}
	}
	// the fork is itself a complete lineage, so a pinned head tells the two apart
	forkHead, forkHash, err := VerifyHotkeyWalletMappingLineage(t.Context(), []HotkeyWalletMappingConsent{chain.firstOriginal, fork})
	if err != nil || forkHead == nil {
		t.Fatal(err)
	}
	expected := HotkeyWalletMappingHistoryExpectation{Subnet: chain.first.Subnet, Hotkey: chain.first.Hotkey, HeadHash: forkHash, Generation: 2, Epoch: 60}
	if mapping, err := VerifyHotkeyWalletMappingHistory(t.Context(), chain.originals(), expected); mapping != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("the other branch of a fork matched the pinned head", err)
	}
	if head, _, err := VerifyHotkeyWalletMappingLineage(t.Context(), nil); head != nil || !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("an empty lineage was not unavailable", err)
	}
	if head, _, err := VerifyHotkeyWalletMappingLineage(t.Context(), make([]HotkeyWalletMappingConsent, MaxWalletMappingHistory+1)); head != nil || !errors.Is(err, ErrWalletMappingCapacity) {
		t.Fatal("a lineage past the history bound was not refused for capacity", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if head, _, err := VerifyHotkeyWalletMappingLineage(ctx, []HotkeyWalletMappingConsent{chain.firstOriginal}); head != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("a canceled lineage verification published a head", err)
	}
}

// The last generation whose first epoch has come is selected through the
// pinned head; a verified chain with none effective is not effective, which is
// distinct from missing originals.
func TestHotkeyWalletMappingHistorySelectsEffectiveConsent(t *testing.T) {
	chain := newHotkeyWalletTestChain(t)
	expected := HotkeyWalletMappingHistoryExpectation{Subnet: chain.first.Subnet, Hotkey: chain.first.Hotkey, HeadHash: chain.secondHash, Generation: 2}
	for _, c := range []struct {
		epoch     uint64
		statement HotkeyWalletMappingStatement
		hash      [32]byte
	}{
		{epoch: 10, statement: chain.first, hash: chain.firstHash},
		{epoch: 59, statement: chain.first, hash: chain.firstHash},
		{epoch: 60, statement: chain.second, hash: chain.secondHash},
		{epoch: 70, statement: chain.second, hash: chain.secondHash},
	} {
		expected.Epoch = c.epoch
		mapping, err := VerifyHotkeyWalletMappingHistory(t.Context(), chain.originals(), expected)
		if err != nil || mapping == nil || mapping.Statement != c.statement || mapping.OriginalHash != c.hash || mapping.HeadHash != chain.secondHash || mapping.Generation != 2 {
			t.Fatal("hotkey consent selection differs", c.epoch, mapping, err)
		}
	}
	// before the first generation, and after the selected generation ends even
	// though an earlier one would still run
	for _, epoch := range []uint64{0, 9, 71} {
		expected.Epoch = epoch
		if mapping, err := VerifyHotkeyWalletMappingHistory(t.Context(), chain.originals(), expected); mapping != nil || !errors.Is(err, ErrWalletMappingNotEffective) || !errors.Is(err, ErrWalletMappingUnavailable) {
			t.Fatal("an ineffective hotkey consent was selected", epoch, err)
		}
	}
}

// Missing originals and a missing head are unavailable and never not
// effective; a contradicting head, hotkey or subnet is integrity.
func TestHotkeyWalletMappingHistoryRefusals(t *testing.T) {
	chain := newHotkeyWalletTestChain(t)
	base := HotkeyWalletMappingHistoryExpectation{Subnet: chain.first.Subnet, Hotkey: chain.first.Hotkey, HeadHash: chain.secondHash, Generation: 2, Epoch: 60}
	for index, c := range []struct {
		originals []HotkeyWalletMappingConsent
		mutate    func(*HotkeyWalletMappingHistoryExpectation)
	}{
		{originals: []HotkeyWalletMappingConsent{chain.firstOriginal}, mutate: func(e *HotkeyWalletMappingHistoryExpectation) {}},
		{originals: nil, mutate: func(e *HotkeyWalletMappingHistoryExpectation) {}},
		{originals: chain.originals(), mutate: func(e *HotkeyWalletMappingHistoryExpectation) { e.HeadHash = [32]byte{} }},
		{originals: chain.originals(), mutate: func(e *HotkeyWalletMappingHistoryExpectation) { e.Generation = 0 }},
		{originals: chain.originals(), mutate: func(e *HotkeyWalletMappingHistoryExpectation) { e.Generation = 3 }},
	} {
		expected := base
		c.mutate(&expected)
		if mapping, err := VerifyHotkeyWalletMappingHistory(t.Context(), c.originals, expected); mapping != nil || !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingNotEffective) || errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("missing hotkey originals were not plainly unavailable", index, err)
		}
	}
	otherHotkey := hotkeyWalletTestKey(t, 22).Public().Encode()
	for index, mutate := range []func(*HotkeyWalletMappingHistoryExpectation){
		func(e *HotkeyWalletMappingHistoryExpectation) { e.HeadHash = chain.firstHash },
		func(e *HotkeyWalletMappingHistoryExpectation) { e.Generation, e.HeadHash = 1, chain.firstHash },
		func(e *HotkeyWalletMappingHistoryExpectation) { e.Hotkey = otherHotkey },
		func(e *HotkeyWalletMappingHistoryExpectation) { e.Hotkey = [32]byte{} },
		func(e *HotkeyWalletMappingHistoryExpectation) { e.Subnet.Netuid++ },
		func(e *HotkeyWalletMappingHistoryExpectation) { e.Subnet = HotkeyWalletMappingSubnet{} },
	} {
		expected := base
		mutate(&expected)
		if mapping, err := VerifyHotkeyWalletMappingHistory(t.Context(), chain.originals(), expected); mapping != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("contradicting hotkey history was admitted", index, err)
		}
	}
	expected := base
	expected.Generation = MaxWalletMappingHistory + 1
	if mapping, err := VerifyHotkeyWalletMappingHistory(t.Context(), chain.originals(), expected); mapping != nil || !errors.Is(err, ErrWalletMappingCapacity) {
		t.Fatal("a pinned generation past the history bound was not capacity", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if mapping, err := VerifyHotkeyWalletMappingHistory(ctx, chain.originals(), base); mapping != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("a canceled hotkey history published a consent", err)
	}
}
