package headsummary_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	subkey "github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/urfoundation/sn/v2026/protocol/headscore"
	"github.com/urfoundation/sn/v2026/protocol/headsummary"
)

// The golden vector the spec names. Set SN_HEAD_SUMMARY_VECTOR_UPDATE=1 to
// sign a new one; sr25519 signatures are randomized, so its bytes change.
const vectorPath = "../testdata/head-summary-v1.json"

type vector struct {
	Description string `json:"description"`
	Seed        string `json:"seed"`
	PublicKey   string `json:"public_key"`
	SigningHash string `json:"signing_hash"`
	Body        string `json:"body"`
}

func testKey(t *testing.T) subkey.KeyPair {
	t.Helper()
	seed := strings.Repeat("48", 32)
	keypair, err := subkey.DeriveKeyPair(sr25519.Scheme{}, "0x"+seed)
	if err != nil {
		t.Fatal(err)
	}
	return keypair
}

func publicKey(t *testing.T, keypair subkey.KeyPair) [32]byte {
	t.Helper()
	var public [32]byte
	copy(public[:], keypair.Public())
	return public
}

func hash32(seed byte) string {
	var value [32]byte
	for index := range value {
		value[index] = seed ^ byte(index)
	}
	return headsummary.Hex32(value)
}

func rational(numerator, denominator string) headscore.Rational {
	return headscore.Rational{Numerator: numerator, Denominator: denominator}
}

// An unsigned summary: UID 7 has two generations; UIDs 3 and 12 tie and the
// lower UID ranks first; two seats leave UID 12 unselected.
func unsigned(public [32]byte) *headsummary.Summary {
	return &headsummary.Summary{
		Schema: headsummary.Schema, Netuid: 25, NativeEpoch: 51234, NativeSnapshotBlock: 6123456, NativeSnapshotHash: hash32(0x6d),
		EVMSnapshotBlock: 6123450, EVMSnapshotHash: hash32(0x2a), SettlementEpoch: 7, PolicyID: 1, PolicyHash: hash32(0x9c),
		MeasurementArtifactHash: "sha256:" + hash32(0x41)[2:], ValidatorHotkey: headsummary.Hex32(public), ValidatorUID: 1, MaximumHeadFleets: 2,
		Entries: []headsummary.Entry{
			{Rank: 1, UID: 7, Hotkey: hash32(0x07), Score: rational("33", "16"), Selected: true, Fleets: []headsummary.Fleet{{FleetID: hash32(0x11), Generation: 1}, {FleetID: hash32(0x11), Generation: 2}}},
			{Rank: 2, UID: 3, Hotkey: hash32(0x03), Score: rational("3", "2"), Selected: true, Fleets: []headsummary.Fleet{{FleetID: hash32(0x13), Generation: 4}}},
			{Rank: 3, UID: 12, Hotkey: hash32(0x0c), Score: rational("3", "2"), Selected: false, Fleets: []headsummary.Fleet{{FleetID: hash32(0x1c), Generation: 1}}},
		},
		SelectedUIDs: []uint16{7, 3}, RejectedUIDs: []uint16{12},
	}
}

func signed(t *testing.T) (*headsummary.Summary, []byte) {
	t.Helper()
	keypair := testKey(t)
	summary := unsigned(publicKey(t, keypair))
	body, err := headsummary.Sign(summary, publicKey(t, keypair), time.Date(2026, 10, 29, 18, 4, 5, 123456789, time.UTC), keypair.Sign)
	if err != nil {
		t.Fatal(err)
	}
	return summary, body
}

// A signed summary is exactly its canonical JSON, decodes to itself, and
// carries the digest of the spec's signing input.
func TestSignAndDecodeRoundTrip(t *testing.T) {
	summary, body := signed(t)
	decoded, err := headsummary.Decode(body)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil || !bytes.Equal(reencoded, body) || decoded.Signature != summary.Signature || len(decoded.SelectedEntries()) != 2 {
		t.Fatalf("round trip changed the summary: %v", err)
	}
	if !strings.HasPrefix(string(body), `{"schema":"urnetwork-validator-head-summary-v1","netuid":25,"native_epoch":51234,`) ||
		!strings.Contains(string(body), `"entries":[{"rank":1,"uid":7,"hotkey":"`) || !strings.Contains(string(body), `"score":{"numerator":"33","denominator":"16"},"selected":true,"fleets":[{"fleet_id":"`) ||
		!strings.Contains(string(body), `"selected_uids":[7,3],"rejected_uids":[12],"signed_at":"2026-10-29T18:04:05.123456789Z","signature_scheme":"sr25519","signing_hash":"sha256:`) {
		t.Fatalf("canonical member order differs from the spec: %s", body)
	}
	// The digest is SHA-256 of the domain, a zero byte and the envelope with
	// both signature members emptied.
	unsignedCopy := *decoded
	unsignedCopy.SigningHash, unsignedCopy.Signature = "", ""
	inner, err := json.Marshal(&unsignedCopy)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := headsummary.SigningDigest(decoded)
	if err != nil || !bytes.Contains(inner, []byte(`"signing_hash":"","signature":""}`)) || decoded.SigningHash != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("signing hash differs: %v", err)
	}
}

// The recorded vector, signed once with a fixed key, still decodes: the
// encoding, digest and signature check have not drifted.
func TestGoldenVectorDecodes(t *testing.T) {
	if os.Getenv("SN_HEAD_SUMMARY_VECTOR_UPDATE") == "1" {
		keypair := testKey(t)
		summary, body := signed(t)
		encoded, err := json.MarshalIndent(vector{
			Description: "head-summary-v1 golden vector: body is the exact request body, signed by the sr25519 key of the mini secret seed (go-subkey, Substrate context).",
			Seed:        "0x" + strings.Repeat("48", 32), PublicKey: headsummary.Hex32(publicKey(t, keypair)), SigningHash: summary.SigningHash, Body: string(body),
		}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorPath, append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var recorded vector
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	decoded, err := headsummary.Decode([]byte(recorded.Body))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ValidatorHotkey != recorded.PublicKey || recorded.PublicKey != headsummary.Hex32(publicKey(t, testKey(t))) || decoded.SigningHash != recorded.SigningHash || len(decoded.Entries) != 3 {
		t.Fatal("golden vector fields differ")
	}
	want := unsigned(publicKey(t, testKey(t)))
	want.SignedAt, want.SignatureScheme, want.SigningHash, want.Signature = decoded.SignedAt, decoded.SignatureScheme, decoded.SigningHash, decoded.Signature
	if wantBody, err := json.Marshal(want); err != nil || string(wantBody) != recorded.Body {
		t.Fatal("golden vector content differs from its construction")
	}
}

// The operator refuses every body that is not exactly one canonical,
// consistent envelope signed by its own validator_hotkey.
func TestDecodeRefusals(t *testing.T) {
	summary, body := signed(t)
	other, err := subkey.DeriveKeyPair(sr25519.Scheme{}, "0x"+strings.Repeat("51", 32))
	if err != nil {
		t.Fatal(err)
	}
	resign := func(change func(*headsummary.Summary)) []byte {
		changed := unsigned(publicKey(t, testKey(t)))
		change(changed)
		encoded, err := headsummary.Sign(changed, publicKey(t, testKey(t)), time.Date(2026, 10, 29, 18, 4, 5, 0, time.UTC), testKey(t).Sign)
		if err == nil {
			return encoded
		}
		// Sign refuses inconsistent content; hand-encode it as an attacker would.
		changed.SignedAt, changed.SignatureScheme = "2026-10-29T18:04:05Z", "sr25519"
		digest, digestErr := headsummary.SigningDigest(changed)
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		signature, signErr := testKey(t).Sign(digest[:])
		if signErr != nil {
			t.Fatal(signErr)
		}
		changed.SigningHash, changed.Signature = "sha256:"+hex.EncodeToString(digest[:]), "0x"+hex.EncodeToString(signature)
		raw, marshalErr := json.Marshal(changed)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return raw
	}
	replace := func(old, new string) []byte {
		if !strings.Contains(string(body), old) {
			t.Fatalf("body has no %q", old)
		}
		return []byte(strings.Replace(string(body), old, new, 1))
	}
	cases := map[string][]byte{
		"trailing newline":   append(append([]byte(nil), body...), '\n'),
		"whitespace":         replace(`"netuid":25`, `"netuid": 25`),
		"unknown member":     replace(`"netuid":25`, `"netuid":25,"extra":1`),
		"duplicate member":   replace(`"netuid":25`, `"netuid":25,"netuid":25`),
		"trailing data":      append(append([]byte(nil), body...), []byte(`{}`)...),
		"escaped string":     replace(`"schema":"urnetwork`, `"schema":"`+string(rune(0x5c))+`u0075rnetwork`),
		"unknown schema":     replace(`head-summary-v1"`, `head-summary-v2"`),
		"tampered score":     replace(`"numerator":"33"`, `"numerator":"34"`),
		"tampered selection": replace(`"selected_uids":[7,3],"rejected_uids":[12]`, `"selected_uids":[7,12],"rejected_uids":[3]`),
		"null list":          replace(`"rejected_uids":[12]`, `"rejected_uids":null`),
		"uppercase hex":      []byte(strings.Replace(string(body), summary.NativeSnapshotHash, strings.ToUpper(summary.NativeSnapshotHash[:2])+summary.NativeSnapshotHash[2:], 1)),
		"tie out of order": resign(func(value *headsummary.Summary) {
			value.Entries[1].UID, value.Entries[2].UID = 12, 3
			value.SelectedUIDs = []uint16{7, 12}
			value.RejectedUIDs = []uint16{3}
		}),
		"selected past maximum": resign(func(value *headsummary.Summary) { value.Entries[2].Selected = true }),
		"rank gap":              resign(func(value *headsummary.Summary) { value.Entries[2].Rank = 4 }),
		"fleets out of order": resign(func(value *headsummary.Summary) {
			value.Entries[0].Fleets[0], value.Entries[0].Fleets[1] = value.Entries[0].Fleets[1], value.Entries[0].Fleets[0]
		}),
		"no fleet":               resign(func(value *headsummary.Summary) { value.Entries[1].Fleets = []headsummary.Fleet{} }),
		"zero generation":        resign(func(value *headsummary.Summary) { value.Entries[1].Fleets[0].Generation = 0 }),
		"unreduced score":        resign(func(value *headsummary.Summary) { value.Entries[0].Score = rational("66", "32") }),
		"zero maximum":           resign(func(value *headsummary.Summary) { value.MaximumHeadFleets = 0 }),
		"zero hotkey":            resign(func(value *headsummary.Summary) { value.Entries[0].Hotkey = headsummary.Hex32([32]byte{}) }),
		"bad artifact hash":      resign(func(value *headsummary.Summary) { value.MeasurementArtifactHash = "sha256:00" }),
		"oversize":               bytes.Repeat([]byte(" "), headsummary.MaxBytes+1),
		"not canonical time":     replace(`"signed_at":"2026-10-29T18:04:05.123456789Z"`, `"signed_at":"2026-10-29T19:04:05.123456789+01:00"`),
		"signature of other key": nil,
	}
	// A valid signature by a key other than validator_hotkey.
	foreign := unsigned(publicKey(t, testKey(t)))
	foreign.SignedAt, foreign.SignatureScheme = "2026-10-29T18:04:05Z", "sr25519"
	digest, err := headsummary.SigningDigest(foreign)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := other.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	foreign.SigningHash, foreign.Signature = "sha256:"+hex.EncodeToString(digest[:]), "0x"+hex.EncodeToString(signature)
	cases["signature of other key"], _ = json.Marshal(foreign)
	// A signer claiming another hotkey than its own.
	claimed := unsigned(publicKey(t, other))
	if _, err := headsummary.Sign(claimed, publicKey(t, testKey(t)), time.Now(), testKey(t).Sign); err == nil {
		t.Fatal("Sign accepted a hotkey other than the summary's")
	}
	for name, raw := range cases {
		if _, err := headsummary.Decode(raw); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := headsummary.Decode(body); err != nil {
		t.Fatalf("the untampered body was refused: %v", err)
	}
}

// An empty head is a valid summary with three empty lists.
func TestEmptyHeadSummary(t *testing.T) {
	keypair := testKey(t)
	summary := unsigned(publicKey(t, keypair))
	summary.Entries, summary.SelectedUIDs, summary.RejectedUIDs = []headsummary.Entry{}, []uint16{}, []uint16{}
	body, err := headsummary.Sign(summary, publicKey(t, keypair), time.Now(), keypair.Sign)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"entries":[],"selected_uids":[],"rejected_uids":[]`) {
		t.Fatalf("empty lists are not encoded as []: %s", body)
	}
	if _, err := headsummary.Decode(body); err != nil {
		t.Fatal(err)
	}
}
