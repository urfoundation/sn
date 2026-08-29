package payoutartifact

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func testArtifact(t *testing.T) *Artifact {
	t.Helper()
	artifact, err := Build(BuildInput{
		DeploymentID: "artifact-test", GenesisHash: "0x" + strings.Repeat("ab", 32),
		PolicyHash: "0x" + strings.Repeat("cd", 32), ChainID: 945, Netuid: 521,
		Coordinator: common.HexToAddress("0x100"), SettlementVault: common.HexToAddress("0x200"),
		Epoch: 4, NoID: 1,
		Start:                Boundary{Number: 100, Hash: "0x" + strings.Repeat("01", 32)},
		End:                  Boundary{Number: 200, Hash: "0x" + strings.Repeat("02", 32)},
		OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32),
		FleetSnapshotHash:    "sha256:" + strings.Repeat("20", 32),
		Providers: []ProviderInput{
			{ClientID: [16]byte{2}, NetworkID: [16]byte{20}, Coldkey: [32]byte{2}, UsageBytes: 100, Assignments: 10, Confirmations: 10, Eligible: true},
			{ClientID: [16]byte{1}, NetworkID: [16]byte{10}, Coldkey: [32]byte{1}, UsageBytes: 100, Assignments: 10, Confirmations: 5, Eligible: true},
		},
		ReliabilityAMin: 8, CreatedAt: time.Unix(1_700_000_000, 123).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestDecodeAcceptsOnlyCanonicalReconstructableArtifact(t *testing.T) {
	artifact := testArtifact(t)
	value, err := Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(value)
	if err != nil || decoded.ContentHash != artifact.ContentHash || decoded.TotalUsageBytes != 200 {
		t.Fatalf("canonical decode = %+v, %v", decoded, err)
	}
	unknown := bytes.Replace(value, []byte(`"schema":`), []byte(`"unknown":1,"schema":`), 1)
	if _, err := Decode(unknown); err == nil {
		t.Fatal("unknown field was accepted")
	}
	if _, err := Decode(append(append([]byte(nil), value...), '\n')); err == nil {
		t.Fatal("non-canonical whitespace was accepted")
	}
	if _, err := Decode(append(append([]byte(nil), value...), []byte(`{}`)...)); err == nil {
		t.Fatal("trailing JSON value was accepted")
	}
}

func TestVerifyRejectsResignedFalseUsageSummary(t *testing.T) {
	artifact := testArtifact(t)
	artifact.TotalUsageBytes++
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	if err := Verify(artifact); err == nil {
		t.Fatal("a valid signer hid a false usage summary")
	}
}
