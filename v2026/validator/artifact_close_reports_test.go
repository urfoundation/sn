// Actual HTTP readers verify original client signatures without trusting a publisher key.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// The HTTP fixture signs the final artifact only after these actual client
// reports are installed. No registration or whole-window authority is supplied.
func validatorOriginalCloseReader(t *testing.T, corrupt bool) (*HTTPArtifactReader, *payoutartifact.Artifact) {
	t.Helper()
	return validatorClosedWorkReader(t, func(c *payoutartifact.ClosedWorkCensus) {
		domain, err := payoutartifact.ClosedWorkReportDomain(&payoutartifact.Artifact{DeploymentID: c.DeploymentId, ChainID: c.ChainId, GenesisHash: c.GenesisHash, Netuid: c.Netuid, Coordinator: c.Coordinator, SettlementVault: c.SettlementVault, NoID: c.NoId, PolicyHash: c.PolicyHash})
		if err != nil {
			t.Fatal(err)
		}
		hash, _ := domain.Digest()
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{65}, 32))
		original, err := coreprotocol.SignOriginalCloseReport(coreprotocol.OriginalCloseReport{DomainHash: hash, ClientId: [16]byte{1}, ContractId: c.Records[0].ContractId, ReportId: [16]byte{9}, AckedByteCount: 3 * 1024 * 1024 * 1024}, key)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := original.Bytes()
		if corrupt {
			raw[len(raw)-1] ^= 1
		}
		amount, unacked, checkpoint := original.AckedByteCount, uint64(0), false
		reports := payoutartifact.ClosedWorkReports{Schema: payoutartifact.ClosedWorkReportsSchema, Count: 1, Reports: []payoutartifact.ClosedWorkReport{{ClientId: "01000000-0000-0000-0000-000000000000", ReportId: "09000000-0000-0000-0000-000000000000", Party: "source", AckedBytes: &amount, UnackedBytes: &unacked, Checkpoint: &checkpoint, Original: raw}}}
		c.Records[0].OriginalReports, err = json.Marshal(reports)
		if err != nil {
			t.Fatal(err)
		}
	})
}

// Public source reads retain verified signatures but do not supply chain authority.
func TestArtifactActualHttpOriginalClosesKeepRegistrationUnknown(t *testing.T) {
	reader, expected := validatorOriginalCloseReader(t, false)
	artifact, err := reader.Read(t.Context(), expected.Epoch, expected.NoID)
	if err != nil || artifact == nil {
		t.Fatal("actual HTTP original close read failed", err)
	}
	value, err := payoutartifact.VerifyClosedWorkReports(t.Context(), artifact, common.Address{})
	if err != nil || value.SignedReports != 1 || value.RegisteredReports != 0 || value.AmountJoins != 0 {
		t.Fatal("public artifact publisher supplied absent registered or complete authority", value, err)
	}
}

// The containing artifact is validly signed; only the client's original is wrong.
func TestArtifactActualHttpResignedFalseCloseSignatureRefuses(t *testing.T) {
	reader, expected := validatorOriginalCloseReader(t, true)
	if err := payoutartifact.Verify(expected); err != nil {
		t.Fatal("negative lost its valid outer publisher signature", err)
	}
	artifact, err := reader.Read(t.Context(), expected.Epoch, expected.NoID)
	if artifact != nil || !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) {
		t.Fatal("actual HTTP trusted an operator-resigned false client signature", artifact, err)
	}
}
