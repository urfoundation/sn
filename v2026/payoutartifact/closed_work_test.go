// These fixtures contain synthetic original database rows, independently
// chosen byte partitions, and actual signed canonical payout artifacts.
package payoutartifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// The original rows deliberately have different contract identities even when
// their complete provider vectors coincide.
func closedWorkTestArtifact(t *testing.T) *Artifact {
	t.Helper()
	artifact := testArtifact(t)
	census := &ClosedWorkCensus{Schema: ClosedWorkSchema, DeploymentId: artifact.DeploymentID, ChainId: artifact.ChainID, GenesisHash: artifact.GenesisHash, Netuid: artifact.Netuid, Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, Epoch: artifact.Epoch, NoId: artifact.NoID, PolicyHash: artifact.PolicyHash, Start: artifact.Start, End: artifact.End, WindowStart: "2026-10-06T00:00:00Z", WindowEnd: "2026-10-06T01:00:00Z", Count: 2}
	for index := 0; index < 2; index++ {
		census.Records = append(census.Records, ClosedWorkRecord{ContractId: [16]byte{byte(index + 1)}, ClosedAt: "2026-10-06T00:30:00Z", Original: []byte(`{"version":1,"byte_count":100,"providers":[{"client_id":"01000000-0000-0000-0000-000000000000","network_id":"0a000000-0000-0000-0000-000000000000","byte_count":50},{"client_id":"02000000-0000-0000-0000-000000000000","network_id":"14000000-0000-0000-0000-000000000000","byte_count":50}]}`)})
	}
	artifact.ClosedWork = census
	closedWorkTestSign(t, artifact)
	return artifact
}

// Re-signing deliberately distinguishes a semantically false original from a
// simple broken-signature negative.
func closedWorkTestSign(t *testing.T, artifact *Artifact) {
	t.Helper()
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
}

// Legacy omission must preserve reconstruction and never manufacture a census.
func TestClosedWorkLegacySignedGrammarRemainsAbsent(t *testing.T) {
	artifact := testArtifact(t)
	raw, err := Bytes(artifact)
	if err != nil || bytes.Contains(raw, []byte("original_closed_work")) {
		t.Fatal("legacy signed grammar gained a component", err)
	}
	decoded, err := Decode(raw)
	if err != nil || decoded.ContentHash != artifact.ContentHash || decoded.ClosedWork != nil {
		t.Fatal("legacy original did not reconstruct", err)
	}
	if result, err := VerifyClosedWork(t.Context(), decoded); result != nil || !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("legacy payout invented complete closed-work evidence", result, err)
	}
}

// Exact original rows reproduce both provider identities and the whole total.
func TestClosedWorkSignedOriginalCensusRoundTrip(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	raw, err := Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := VerifyClosedWork(t.Context(), decoded)
	if err != nil || value.Contracts != 2 || value.Providers != 2 || value.UsageBytes != 200 || value.OrdinarySnapshots != 2 || value.CensusHash != artifact.ClosedWork.Hash() {
		t.Fatal("complete original census did not reproduce usage", value, err)
	}
}

// A valid signed foreign or partial component supplies no authority here.
func TestClosedWorkForeignAndPartialRemainUnknown(t *testing.T) {
	for _, change := range []func(*ClosedWorkCensus){
		func(c *ClosedWorkCensus) { c.NoId++ },
		func(c *ClosedWorkCensus) { c.Schema = "synthetic-future-closed-work-v2" },
		func(c *ClosedWorkCensus) { c.PolicyHash = "0x" + strings.Repeat("ef", 32) },
		func(c *ClosedWorkCensus) { c.Start.Number++ },
		func(c *ClosedWorkCensus) { c.Count++ },
		func(c *ClosedWorkCensus) { c.Records = nil },
	} {
		artifact := closedWorkTestArtifact(t)
		change(artifact.ClosedWork)
		closedWorkTestSign(t, artifact)
		if value, err := VerifyClosedWork(t.Context(), artifact); value != nil || !errors.Is(err, ErrClosedWorkUnavailable) || errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("foreign or partial component was promoted or quarantined", value, err)
		}
	}
}

// A newly valid signature cannot hide a different original recipient identity.
func TestClosedWorkResignedEqualTotalForeignRecipientRefuses(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	for index := range artifact.ClosedWork.Records {
		artifact.ClosedWork.Records[index].Original = bytes.ReplaceAll(artifact.ClosedWork.Records[index].Original, []byte("0a000000-0000"), []byte("0b000000-0000"))
	}
	closedWorkTestSign(t, artifact)
	if err := Verify(artifact); err != nil {
		t.Fatal("negative did not retain a valid original artifact", err)
	}
	if value, err := VerifyClosedWork(t.Context(), artifact); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) || !strings.Contains(err.Error(), "identity or usage") {
		t.Fatal("re-signed equal total hid a different original recipient", value, err)
	}
}

// Count agreement cannot hide duplicate contract identities or closed-time drift.
func TestClosedWorkOriginalIdentityWindowAndPartitionRefuse(t *testing.T) {
	for _, change := range []func(*ClosedWorkCensus){
		func(c *ClosedWorkCensus) { c.Records[1].ContractId = c.Records[0].ContractId },
		func(c *ClosedWorkCensus) { c.Records[0].ClosedAt = c.WindowEnd },
		func(c *ClosedWorkCensus) {
			c.Records[0].Original = bytes.Replace(c.Records[0].Original, []byte(`"byte_count":50`), []byte(`"byte_count":51`), 1)
		},
		func(c *ClosedWorkCensus) { c.Records[0].Original = append(c.Records[0].Original, []byte(`{}`)...) },
	} {
		artifact := closedWorkTestArtifact(t)
		change(artifact.ClosedWork)
		closedWorkTestSign(t, artifact)
		if value, err := VerifyClosedWork(t.Context(), artifact); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("changed original row reached provider authority", value, err)
		}
	}
}

// Removing and re-signing one complete row must still fail the independent sum.
func TestClosedWorkResignedOmittedRowCannotMatchUsage(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	artifact.ClosedWork.Records = artifact.ClosedWork.Records[:1]
	artifact.ClosedWork.Count = 1
	closedWorkTestSign(t, artifact)
	if value, err := VerifyClosedWork(t.Context(), artifact); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) || !strings.Contains(err.Error(), "complete usage census") {
		t.Fatal("omitted original contract escaped the independent usage census", value, err)
	}
}

// Expiry retains original lower bounds, and an explicit legacy exclusion remains
// debt rather than being counted as usage or final acceptance.
func TestClosedWorkExpiryAndLegacyDebtRetainOriginalInputs(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	row := &artifact.ClosedWork.Records[0]
	row.Original = append(bytes.Clone(row.Original[:len(row.Original)-1]), []byte(`,"expiry":{"capacity":120,"reports":{"source":{"byte_count":100,"checkpoint":false},"destination":{"byte_count":110,"checkpoint":true}}}}`)...)
	artifact.ClosedWork.Count++
	artifact.ClosedWork.Records = append(artifact.ClosedWork.Records, ClosedWorkRecord{ContractId: [16]byte{3}, ClosedAt: "2026-10-06T00:30:00Z", Original: []byte(fmt.Sprintf(`{"version":1,"byte_count":0,"providers":[],"excluded_reason":"legacy_usage_unavailable","legacy_exclusion":{"repair_manifest_sha256":"sha256:%s","contract_id":"03000000-0000-0000-0000-000000000000","epoch":4,"closed_at":"2026-10-06T00:30:00Z","retained_report_minimum":80,"final_acceptance":false}}`, strings.Repeat("4d", 32)))})
	closedWorkTestSign(t, artifact)
	value, err := VerifyClosedWork(t.Context(), artifact)
	if err != nil || value.ExpiredSnapshots != 1 || value.UncreditedLegacySnapshots != 1 || value.UsageBytes != 200 || value.Contracts != 3 {
		t.Fatal("original expiry or legacy debt was relabelled", value, err)
	}
	artifact.ClosedWork.Records[0].Original = bytes.Replace(artifact.ClosedWork.Records[0].Original, []byte(`"byte_count":110`), []byte(`"byte_count":90`), 1)
	closedWorkTestSign(t, artifact)
	if _, err := VerifyClosedWork(t.Context(), artifact); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("changed original lower-bound report was accepted", err)
	}
}

// Capacity and owner cancellation are operational refusals, not false integrity.
func TestClosedWorkCapacityAndCancellationPreserveCauses(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := VerifyClosedWork(ctx, artifact); !errors.Is(err, context.Canceled) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("canceled component owner became integrity", err)
	}
	if _, err := VerifyClosedWork(t.Context(), artifact); err != nil {
		t.Fatal("same original did not recover under a healthy owner", err)
	}
	artifact.ClosedWork.Records[0].Original = make([]byte, MaxClosedWorkRecordBytes+1)
	if _, err := VerifyClosedWork(t.Context(), artifact); !errors.Is(err, ErrClosedWorkCapacity) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("finite row limit became conflicting evidence", err)
	}
}

// Build takes an owned copy rather than retaining caller slices until signing.
func TestClosedWorkBuildOwnsOriginalRows(t *testing.T) {
	original := closedWorkTestArtifact(t)
	in := BuildInput{ClosedWork: original.ClosedWork, DeploymentID: original.DeploymentID, GenesisHash: original.GenesisHash, PolicyHash: original.PolicyHash, ChainID: original.ChainID, Netuid: original.Netuid, Coordinator: original.Coordinator, SettlementVault: original.SettlementVault, Epoch: original.Epoch, NoID: original.NoID, Start: original.Start, End: original.End, OperatorSnapshotHash: original.OperatorSnapshotHash, FleetSnapshotHash: original.FleetSnapshotHash, Providers: original.Providers, ReliabilityAMin: original.ReliabilityAMin, CreatedAt: time.Unix(1_700_000_000, 123).UTC()}
	copy, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(copy.ClosedWork)
	if err != nil {
		t.Fatal(err)
	}
	in.ClosedWork.Records[0].Original[0] = 'x'
	in.ClosedWork.Records[1].ContractId[0]++
	after, err := json.Marshal(copy.ClosedWork)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("caller mutation changed owned original bytes", err)
	}
	closedWorkTestSign(t, copy)
	if _, err := VerifyClosedWork(t.Context(), copy); err != nil {
		t.Fatal("owned original did not survive caller mutation", err)
	}
}

// Swapping one byte in both directions preserves every global provider total,
// but cannot change the original equal participant partition of either contract.
func TestClosedWorkEqualAggregateCannotHideNoncanonicalPartition(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	for index := range artifact.ClosedWork.Records {
		row := &artifact.ClosedWork.Records[index]
		first, second := "51", "49"
		if index == 1 {
			first, second = second, first
		}
		row.Original = bytes.Replace(row.Original, []byte(`"byte_count":50`), []byte(`"byte_count":`+first), 1)
		row.Original = bytes.Replace(row.Original, []byte(`"byte_count":50`), []byte(`"byte_count":`+second), 1)
	}
	closedWorkTestSign(t, artifact)
	if value, err := VerifyClosedWork(t.Context(), artifact); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) || !strings.Contains(err.Error(), "participant partition") {
		t.Fatal("equal provider totals hid rewritten original participant partitions", value, err)
	}
}
