//go:build linux || darwin

package validator

// The shared renderer must produce files the production loader admits byte
// for byte, and its pure helpers must fail closed on every geometry error.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

func testActivationDeployment() ReleaseActivationDeploymentV2 {
	return ReleaseActivationDeploymentV2{
		DeploymentID: "test-deployment", ChainID: 945, GenesisHash: [32]byte{1}, Netuid: 7,
		Coordinator: [20]byte{0x11}, SettlementVault: [20]byte{0x22}, PolicyHash: [32]byte{3},
	}
}

func TestBuildAndSignFreshReleaseActivationV2(t *testing.T) {
	hotkey, err := crv4.KeypairFromSeed([32]byte{4})
	if err != nil {
		t.Fatal(err)
	}
	clientKey := ed25519.NewKeyFromSeed([]byte(strings.Repeat("k", 32)))
	vpk := [32]byte(clientKey[ed25519.SeedSize:])
	snapshot := ReleaseActivationSnapshotV2{Epoch: 9, NativeBlock: 100, NativeHash: [32]byte{5}, EVMBlock: 200, EVMHash: [32]byte{6}}
	activation, err := BuildFreshReleaseActivationV2(testActivationDeployment(), snapshot, hotkey.PublicKey(), 2, vpk)
	if err != nil {
		t.Fatal(err)
	}
	if activation.FirstSequence != 1 || activation.PriorRoot != ([32]byte{}) || activation.Domain.Epoch != 9 || activation.Domain.DeploymentIDHash != sha256.Sum256([]byte("test-deployment")) {
		t.Fatalf("fresh activation = %+v", activation)
	}
	vpkSignature, hotkeySignature, err := SignReleaseActivationV2(activation, hotkey, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := activation.Verify(activation, vpkSignature, hotkeySignature); err != nil {
		t.Fatal(err)
	}
	other, _ := crv4.KeypairFromSeed([32]byte{9})
	if _, _, err := SignReleaseActivationV2(activation, other, clientKey); err == nil {
		t.Fatal("a different hotkey signed another hotkey's activation")
	}
	if _, _, err := SignReleaseActivationV2(activation, hotkey, ed25519.NewKeyFromSeed([]byte(strings.Repeat("x", 32)))); err == nil {
		t.Fatal("a different client key signed another VPK's activation")
	}
	if _, err := BuildFreshReleaseActivationV2(testActivationDeployment(), ReleaseActivationSnapshotV2{}, hotkey.PublicKey(), 2, vpk); err == nil {
		t.Fatal("an empty snapshot built a valid activation")
	}
}

func TestReleaseActivationBoundaryBlockV2Table(t *testing.T) {
	for _, testCase := range []struct {
		name                            string
		start, end, snapshot, finalized uint64
		published                       []uint64
		want                            uint64
		wantErr                         string
	}{
		{name: "epoch start after publications", start: 1000, end: 2000, snapshot: 500, finalized: 1500, published: []uint64{600, 700}, want: 1000},
		{name: "latest publication after start", start: 1000, end: 2000, snapshot: 500, finalized: 1500, published: []uint64{600, 1200}, want: 1200},
		{name: "publication at or past the epoch end", start: 1000, end: 2000, snapshot: 500, finalized: 2500, published: []uint64{2000}, wantErr: "before their common initial epoch boundary"},
		{name: "boundary not finalized", start: 1000, end: 2000, snapshot: 500, finalized: 900, published: []uint64{600}, wantErr: "before their common initial epoch boundary"},
		{name: "boundary not after the snapshot", start: 1000, end: 2000, snapshot: 1000, finalized: 1500, published: []uint64{600}, wantErr: "before their common initial epoch boundary"},
		{name: "unpublished member", start: 1000, end: 2000, snapshot: 500, finalized: 1500, published: []uint64{0}, wantErr: "publication height is zero"},
		{name: "invalid geometry", start: 2000, end: 2000, snapshot: 500, finalized: 1500, wantErr: "geometry"},
	} {
		got, err := ReleaseActivationBoundaryBlockV2(testCase.start, testCase.end, testCase.snapshot, testCase.finalized, testCase.published)
		if testCase.wantErr == "" {
			if err != nil || got != testCase.want {
				t.Fatalf("%s: boundary=%d err=%v", testCase.name, got, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
			t.Fatalf("%s: err=%v, want %q", testCase.name, err, testCase.wantErr)
		}
	}
}

// Rendered files must be exactly what the production activation reader
// decodes: the payload round-trips, both signatures verify, the context is
// canonical and consistent with the activation, and every reference pins the
// bytes that were written.
func TestRenderReleaseActivationInputsV2RoundTripsThroughTheProductionReaders(t *testing.T) {
	hotkey, err := crv4.KeypairFromSeed([32]byte{4})
	if err != nil {
		t.Fatal(err)
	}
	clientKey := ed25519.NewKeyFromSeed([]byte(strings.Repeat("k", 32)))
	deployment := testActivationDeployment()
	snapshot := ReleaseActivationSnapshotV2{Epoch: 9, NativeBlock: 100, NativeHash: [32]byte{5}, EVMBlock: 200, EVMHash: [32]byte{6}}
	activation, err := BuildFreshReleaseActivationV2(deployment, snapshot, hotkey.PublicKey(), 2, [32]byte(clientKey[ed25519.SeedSize:]))
	if err != nil {
		t.Fatal(err)
	}
	vpkSignature, hotkeySignature, err := SignReleaseActivationV2(activation, hotkey, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	member := ReleaseActivationMemberV2{NoID: 2, ValidatorUID: 3, Activation: activation, VPKSignature: vpkSignature, HotkeySignature: hotkeySignature}
	root := t.TempDir()
	bounds := releaseEvidenceV2TestConfig(root, []OperatorConfig{{NoID: 1}, {NoID: 2}}).Bounds
	paths := DefaultReleaseEvidenceV2OperatorPaths(root, 2)
	ledger := ReleaseActivationLedgerV2{DeploymentID: "test-deployment", ChainID: 945, GenesisHash: attemptHex32(deployment.GenesisHash), Netuid: 7, ValidatorID: 1}
	boundary := ReleaseActivationBoundaryV2{Block: 260, Hash: [32]byte{7}}
	entry, inputs, err := RenderReleaseActivationInputsV2(ledger, member, [20]byte{0x33}, [32]byte{8}, boundary, bounds, paths)
	if err != nil {
		t.Fatal(err)
	}
	if entry.NoID != 2 || entry.ReplayScratchRoot != filepath.Join(root, "scratch-v2", "no-2", "replay") || entry.Activation.Path != filepath.Join(root, "evidence-v2", "no-2", "activation.payload") {
		t.Fatalf("rendered entry = %+v", entry)
	}
	ctx := context.Background()
	for index, reference := range entry.Files() {
		data := inputs[reference.Path]
		if uint64(len(data)) != reference.Bytes || reference.SHA256 != attemptHex32(sha256.Sum256(data)) {
			t.Fatalf("reference %d does not pin its bytes: %+v", index, reference)
		}
		written, err := WriteReleaseEvidenceV2File(ctx, reference.Path, data, ReleaseEvidenceV2ReferenceLimit(bounds, index))
		if err != nil {
			t.Fatal(err)
		}
		if written != reference {
			t.Fatalf("written reference %+v differs from rendered %+v", written, reference)
		}
		read, err := ReadReleaseEvidenceV2File(ctx, reference, ReleaseEvidenceV2ReferenceLimit(bounds, index))
		if err != nil {
			t.Fatal(err)
		}
		if string(read) != string(data) {
			t.Fatalf("reference %d bytes changed through the private reader", index)
		}
	}
	candidate, err := protocol.DecodeValidatorEvidenceActivationPayload(inputs[entry.Activation.Path])
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Verify(activation, inputs[entry.VPKSignature.Path], inputs[entry.HotkeySignature.Path]); err != nil {
		t.Fatal(err)
	}
	configured, err := decodeReleaseEvidenceV2ActivationContext(inputs[entry.Context.Path], bounds.Cut.MaxHeaderBytes)
	if err != nil {
		t.Fatal(err)
	}
	if configured.Activation != activation || configured.ValidatorUID != 3 || configured.Journal != [20]byte{0x33} || configured.RuntimeHash != [32]byte{8} || configured.ObservedEVMBlock != 260 || configured.InitialCut.Boundary.SettlementEpoch != 9 || configured.InitialCut.Identity.NoID != 2 || configured.InitialCut.FirstSequence != 1 {
		t.Fatalf("decoded context = %+v", configured)
	}
	if string(inputs[entry.History.Path]) != `{"schema":"urnetwork-validator-activation-history-v2","legacy_closures":[]}`+"\n" {
		t.Fatalf("history = %q", inputs[entry.History.Path])
	}
	// A boundary that does not follow the EVM snapshot is refused by the
	// context's own consistency rule, so the renderer cannot emit it.
	if _, _, err := RenderReleaseActivationInputsV2(ledger, member, [20]byte{0x33}, [32]byte{8}, ReleaseActivationBoundaryV2{Block: 200, Hash: [32]byte{7}}, bounds, paths); err == nil {
		t.Fatal("a boundary at the EVM snapshot rendered")
	}
	broken := member
	broken.HotkeySignature = append([]byte(nil), hotkeySignature...)
	broken.HotkeySignature[0] ^= 1
	if _, _, err := RenderReleaseActivationInputsV2(ledger, broken, [20]byte{0x33}, [32]byte{8}, boundary, bounds, paths); err == nil {
		t.Fatal("an invalid hotkey signature rendered")
	}
}
