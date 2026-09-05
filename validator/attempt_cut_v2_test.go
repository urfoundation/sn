package validator

// Compact-header tests exercise authentication and bounded metadata only;
// stream data signatures and actual producer activation remain separate gates.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// Uses real deterministic signing keys and metadata-only stream fixtures.
func attemptCutV2TestFixture(t *testing.T) (AttemptCutV2, ed25519.PrivateKey, AttemptCutV2Bounds) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x46}, ed25519.SeedSize))
	vpk := key.Public().(ed25519.PublicKey)
	genesis := [32]byte{0x31}
	identity := AttemptLedgerIdentity{DeploymentID: "compact-cut-fixture", ChainID: 945, GenesisHash: attemptHex32(genesis), Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 2, ValidatorVPK: attemptHex32(*(*[32]byte)(vpk))}
	domain := protocol.ValidatorEvidenceDomain{ChainID: 945, GenesisHash: genesis, Netuid: 521, Coordinator: [20]byte{0x11}, SettlementVault: [20]byte{0x12}, DeploymentIDHash: sha256.Sum256([]byte(identity.DeploymentID)), PolicyHash: [32]byte{0x13}, ActivationEpoch: 42, ActivationHash: [32]byte{0x14}}
	records, _ := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, nil)
	proofs, _ := attemptStreamV2TestArchive(t, AttemptStreamV2Proofs, attemptStreamV2TestPages(AttemptStreamV2Proofs), nil, nil)
	cut := AttemptCutV2{Schema: AttemptCutV2Schema, Context: AttemptCutV2Context{Identity: identity, Activation: AttemptCutV2Activation{Domain: domain, Hotkey: [32]byte{0x15}, FirstSequence: 1, PriorRoot: zeroAttemptHash()}, Boundary: AttemptBoundary{SettlementEpoch: 42, EVMBlock: 1000, EVMBlockHash: attemptHex32([32]byte{0x16})}, FirstSequence: 1, EgressFirstSequence: 5, EgressGeneration: 3, PriorRoot: zeroAttemptHash()}, LastSequence: 8, RecordCount: 8, CompleteCount: 4, FailedCount: 1, Root: attemptHex32([32]byte{0x17}), Records: records, Proofs: proofs}
	bounds := AttemptCutV2Bounds{MaxHeaderBytes: 16 * 1024, Records: attemptStreamV2TestBounds(), Proofs: attemptStreamV2TestBounds()}
	var err error
	cut.Signature, err = cut.Sign(key, bounds)
	if err != nil {
		t.Fatal(err)
	}
	return cut, key, bounds
}

// The cut carries fixed-size references, not a copied history or hash array.
func TestAttemptCutV2SignedHeaderRoundTrip(t *testing.T) {
	cut, key, bounds := attemptCutV2TestFixture(t)
	raw, err := cut.CanonicalJSON(bounds)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("record_hashes")) || bytes.Contains(raw, []byte("assignments")) {
		t.Fatal("compact header materialized historical rows")
	}
	decoded, err := DecodeAttemptCutV2(raw, cut.Context, bounds)
	if err != nil || !reflect.DeepEqual(*decoded, cut) {
		t.Fatalf("header roundtrip: %v", err)
	}
	again, err := decoded.Sign(key, bounds)
	if err != nil || !bytes.Equal(again, cut.Signature) {
		t.Fatalf("signature is not deterministic: %v", err)
	}
	unsigned := cut
	unsigned.Signature = nil
	encoded, err := json.Marshal(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(append([]byte("urnetwork-validator-attempt-cut-signature-v2\x00"), encoded...))
	got, err := cut.SigningDigest(bounds)
	if err != nil || got != want || !ed25519.Verify(key.Public().(ed25519.PublicKey), want[:], cut.Signature) {
		t.Fatalf("ordinary cut digest/signature domain differs: %v", err)
	}
	legacyDigest := sha256.Sum256(append([]byte(attemptCutSignDomain), encoded...))
	if ed25519.Verify(key.Public().(ed25519.PublicKey), legacyDigest[:], cut.Signature) {
		t.Fatal("v2 signature authenticated a v1 cut domain")
	}
}

// A correctly re-signed candidate still cannot choose its expected namespace,
// validator identity, activation or native-window cursor.
func TestAttemptCutV2RejectsCandidateSelectedContext(t *testing.T) {
	cut, key, bounds := attemptCutV2TestFixture(t)
	for _, mutation := range []struct {
		name string
		edit func(*AttemptCutV2Context)
	}{
		{name: "operator", edit: func(ctx *AttemptCutV2Context) { ctx.Identity.NoID++ }},
		{name: "validator", edit: func(ctx *AttemptCutV2Context) { ctx.Identity.ValidatorID++ }},
		{name: "uid", edit: func(ctx *AttemptCutV2Context) { ctx.Identity.ValidatorUID++ }},
		{name: "hotkey", edit: func(ctx *AttemptCutV2Context) { ctx.Activation.Hotkey[0]++ }},
		{name: "coordinator", edit: func(ctx *AttemptCutV2Context) { ctx.Activation.Domain.Coordinator[1]++ }},
		{name: "vault", edit: func(ctx *AttemptCutV2Context) { ctx.Activation.Domain.SettlementVault[0]++ }},
		{name: "policy", edit: func(ctx *AttemptCutV2Context) { ctx.Activation.Domain.PolicyHash[0]++ }},
		{name: "activation hash", edit: func(ctx *AttemptCutV2Context) { ctx.Activation.Domain.ActivationHash[0]++ }},
		{name: "activation epoch", edit: func(ctx *AttemptCutV2Context) { ctx.Activation.Domain.ActivationEpoch-- }},
		{name: "boundary epoch", edit: func(ctx *AttemptCutV2Context) { ctx.Boundary.SettlementEpoch++ }},
		{name: "boundary block", edit: func(ctx *AttemptCutV2Context) { ctx.Boundary.EVMBlock++ }},
		{name: "boundary hash", edit: func(ctx *AttemptCutV2Context) { ctx.Boundary.EVMBlockHash = attemptHex32([32]byte{0x55}) }},
		{name: "egress cursor", edit: func(ctx *AttemptCutV2Context) { ctx.EgressFirstSequence++ }},
		{name: "egress generation", edit: func(ctx *AttemptCutV2Context) { ctx.EgressGeneration++ }},
		{name: "deployment", edit: func(ctx *AttemptCutV2Context) {
			ctx.Identity.DeploymentID += "-other"
			ctx.Activation.Domain.DeploymentIDHash = sha256.Sum256([]byte(ctx.Identity.DeploymentID))
		}},
		{name: "chain", edit: func(ctx *AttemptCutV2Context) { ctx.Identity.ChainID++; ctx.Activation.Domain.ChainID++ }},
		{name: "netuid", edit: func(ctx *AttemptCutV2Context) { ctx.Identity.Netuid++; ctx.Activation.Domain.Netuid++ }},
	} {
		changed := cut
		mutation.edit(&changed.Context)
		var err error
		changed.Signature, err = changed.Sign(key, bounds)
		if err != nil {
			t.Fatalf("%s did not form a valid separate context: %v", mutation.name, err)
		}
		if err := changed.VerifyHeader(changed.Context, bounds); err != nil {
			t.Fatalf("%s self-context control: %v", mutation.name, err)
		}
		if err := changed.VerifyHeader(cut.Context, bounds); err == nil || !strings.Contains(err.Error(), "expected context") {
			t.Errorf("%s candidate selected the trusted context: %v", mutation.name, err)
		}
	}
}

// Coherently changed counts/references pass structural validation but still
// require a fresh signature; no unsigned summary can change the committed set.
func TestAttemptCutV2AuthenticatesStreamAndOutcomeFields(t *testing.T) {
	cut, _, bounds := attemptCutV2TestFixture(t)
	for _, mutation := range []struct {
		name string
		edit func(*AttemptCutV2)
	}{
		{name: "root", edit: func(cut *AttemptCutV2) { cut.Root = attemptHex32([32]byte{0x44}) }},
		{name: "failed outcomes", edit: func(cut *AttemptCutV2) { cut.FailedCount++ }},
		{name: "completed outcomes", edit: func(cut *AttemptCutV2) { cut.CompleteCount++; cut.Proofs.ItemCount++ }},
		{name: "records manifest hash", edit: func(cut *AttemptCutV2) { cut.Records.ManifestHash = attemptHex32([32]byte{0x45}) }},
		{name: "records manifest size", edit: func(cut *AttemptCutV2) { cut.Records.ManifestBytes++ }},
		{name: "records bytes", edit: func(cut *AttemptCutV2) { cut.Records.DataBytes++ }},
		{name: "records chunks", edit: func(cut *AttemptCutV2) { cut.Records.ChunkCount++ }},
		{name: "records pages", edit: func(cut *AttemptCutV2) { cut.Records.PageCount = 1 }},
		{name: "proof manifest hash", edit: func(cut *AttemptCutV2) { cut.Proofs.ManifestHash = attemptHex32([32]byte{0x46}) }},
		{name: "proof manifest size", edit: func(cut *AttemptCutV2) { cut.Proofs.ManifestBytes++ }},
		{name: "proof bytes", edit: func(cut *AttemptCutV2) { cut.Proofs.DataBytes++ }},
		{name: "proof chunks", edit: func(cut *AttemptCutV2) { cut.Proofs.ChunkCount++ }},
		{name: "proof pages", edit: func(cut *AttemptCutV2) { cut.Proofs.PageCount = 1 }},
	} {
		changed := cut
		mutation.edit(&changed)
		if err := changed.Validate(bounds); err != nil {
			t.Fatalf("%s fixture is not a coherent mutation: %v", mutation.name, err)
		}
		if err := changed.VerifyHeader(cut.Context, bounds); err == nil || !strings.Contains(err.Error(), "signature") {
			t.Errorf("%s changed without a fresh signature: %v", mutation.name, err)
		}
	}
}

// Empty windows carry the unchanged root and no fake stream object. Activation
// still authenticates the earlier migrated prefix even when this cut is empty.
func TestAttemptCutV2EmptyWindowAndMigrationPrefix(t *testing.T) {
	cut, key, bounds := attemptCutV2TestFixture(t)
	cut.Context.FirstSequence, cut.Context.EgressFirstSequence = 9, 9
	cut.Context.Activation.FirstSequence = 9
	cut.Context.PriorRoot = cut.Root
	cut.Context.Activation.PriorRoot = cut.Root
	cut.RecordCount, cut.CompleteCount, cut.FailedCount = 0, 0, 0
	cut.Records = AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}
	cut.Proofs = AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}
	var err error
	cut.Signature, err = cut.Sign(key, bounds)
	if err != nil {
		t.Fatal(err)
	}
	if err := cut.VerifyHeader(cut.Context, bounds); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*AttemptCutV2){
		func(cut *AttemptCutV2) { cut.Root = attemptHex32([32]byte{0x71}) },
		func(cut *AttemptCutV2) { cut.Context.Activation.PriorRoot = attemptHex32([32]byte{0x72}) },
		func(cut *AttemptCutV2) { cut.Context.FirstSequence = 8 },
		func(cut *AttemptCutV2) { cut.Context.EgressFirstSequence = 8 },
		func(cut *AttemptCutV2) { cut.Records.DataBytes = 1 },
		func(cut *AttemptCutV2) { cut.Proofs.ManifestHash = attemptHex32([32]byte{0x73}) },
	} {
		changed := cut
		edit(&changed)
		if err := changed.Validate(bounds); err == nil {
			t.Fatal("invalid empty window or migration prefix was accepted")
		}
	}
}

// Invalid ranges and terminal sums are checked without overflowing additions.
func TestAttemptCutV2RejectsRangeCountAndBoundViolations(t *testing.T) {
	cut, _, bounds := attemptCutV2TestFixture(t)
	for _, edit := range []func(*AttemptCutV2){
		func(cut *AttemptCutV2) { cut.LastSequence = ^uint64(0) },
		func(cut *AttemptCutV2) { cut.Context.Activation.FirstSequence = 0 },
		func(cut *AttemptCutV2) { cut.Context.Activation.FirstSequence = ^uint64(0) },
		func(cut *AttemptCutV2) { cut.Context.FirstSequence = 0 },
		func(cut *AttemptCutV2) { cut.Context.EgressFirstSequence = 10 },
		func(cut *AttemptCutV2) { cut.RecordCount++ },
		func(cut *AttemptCutV2) { cut.CompleteCount = ^uint64(0) },
		func(cut *AttemptCutV2) { cut.FailedCount = ^uint64(0) },
		func(cut *AttemptCutV2) {
			cut.CompleteCount = 0
			cut.FailedCount = 0
			cut.Proofs = AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}
		},
		func(cut *AttemptCutV2) { cut.Context.Boundary.SettlementEpoch-- },
		func(cut *AttemptCutV2) { cut.Schema = attemptLedgerCutSchema },
		func(cut *AttemptCutV2) { cut.Context.Activation.Domain.DeploymentIDHash[0]++ },
	} {
		changed := cut
		edit(&changed)
		if err := changed.Validate(bounds); err == nil {
			t.Fatal("invalid compact cut was accepted")
		}
	}
	raw, err := cut.CanonicalJSON(bounds)
	if err != nil {
		t.Fatal(err)
	}
	exact := bounds
	exact.MaxHeaderBytes = uint64(len(raw))
	if _, err := DecodeAttemptCutV2(raw, cut.Context, exact); err != nil {
		t.Fatalf("exact byte bound: %v", err)
	}
	exact.MaxHeaderBytes--
	if _, err := DecodeAttemptCutV2(raw, cut.Context, exact); err == nil {
		t.Fatal("header exceeded its exact byte bound")
	}
}

// Byte canonicality cannot be bypassed by a valid signature over parsed fields.
func TestAttemptCutV2RejectsNoncanonicalAndUnsignedData(t *testing.T) {
	cut, _, bounds := attemptCutV2TestFixture(t)
	raw, err := cut.CanonicalJSON(bounds)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{
		append([]byte(" "), raw...),
		bytes.Clone(raw[:len(raw)-1]),
		append(bytes.Clone(raw), '{', '}'),
		bytes.Replace(raw, []byte("{\"schema\":"), []byte("{\"record_count\":8,\"schema\":"), 1),
		bytes.Replace(raw, []byte("{\"schema\":"), []byte("{\"extra\":0,\"schema\":"), 1),
	} {
		if _, err := DecodeAttemptCutV2(changed, cut.Context, bounds); err == nil {
			t.Fatal("noncanonical signed header was accepted")
		}
	}
	cut.Signature = nil
	unsigned, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAttemptCutV2(append(unsigned, '\n'), cut.Context, bounds); err == nil {
		t.Fatal("unsigned cut was accepted")
	}
}

// A private key with a forged public half must fail before any signature is
// emitted; header and private-key inputs remain unchanged on every path.
func TestAttemptCutV2SigningRejectsInconsistentPrivateKey(t *testing.T) {
	cut, key, bounds := attemptCutV2TestFixture(t)
	before, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	broken := bytes.Clone(key)
	broken[0] ^= 1
	if _, err := cut.Sign(broken, bounds); err == nil {
		t.Fatal("inconsistent private key was accepted")
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x47}, ed25519.SeedSize))
	if _, err := cut.Sign(other, bounds); err == nil {
		t.Fatal("different validator private key was accepted")
	}
	after, err := json.Marshal(cut)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("signing failure mutated the header")
	}
	if key[0] != 0x46 {
		t.Fatal("signing failure mutated the private key")
	}
}
