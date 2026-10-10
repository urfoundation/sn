// Independently signed client reports are reconstructed under explicit root authority.
package payoutartifact

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Original root and artifact publishers deliberately use different synthetic keys.
func closedReportsTestArtifact(t *testing.T) (*Artifact, *ecdsa.PrivateKey) {
	t.Helper()
	artifact := closedWorkTestArtifact(t)
	root, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	domain, err := ClosedWorkReportDomain(artifact)
	if err != nil {
		t.Fatal(err)
	}
	domainHash, _ := domain.Digest()
	for index := range artifact.ClosedWork.Records {
		row := &artifact.ClosedWork.Records[index]
		census := ClosedWorkReports{Schema: ClosedWorkReportsSchema, Count: 2}
		for party := 0; party < 2; party++ {
			clientId, reportId := [16]byte{byte(party + 1)}, [16]byte{byte(index + 10), byte(party + 1)}
			key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(40 + party)}, 32))
			original, err := coreprotocol.SignOriginalCloseReport(coreprotocol.OriginalCloseReport{DomainHash: domainHash, ClientId: clientId, ContractId: row.ContractId, ReportId: reportId, AckedByteCount: 100, UnackedByteCount: 7}, key)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := original.Bytes()
			registration := protocol.ClientKeyRegistration{Domain: domain, ClientID: clientId, NetworkID: [16]byte{byte((party + 1) * 10)}, Generation: 1, Present: true, PublicKey: [32]byte(key[32:]), EffectiveBoundary: protocol.ClientKeyEffectiveBoundary{Epoch: artifact.Epoch, Block: artifact.Start.Number + 1, Hash: [32]byte{22}}}
			if err := protocol.SignClientKeyRegistration(&registration, root); err != nil {
				t.Fatal(err)
			}
			registered, _ := registration.Bytes()
			amount, unacked, checkpoint := uint64(100), uint64(7), false
			partyName := "source"
			if party == 1 {
				partyName = "destination"
			}
			idText := func(id [16]byte) string {
				return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
			}
			census.Reports = append(census.Reports, ClosedWorkReport{ClientId: idText(clientId), ReportId: idText(reportId), Party: partyName, AckedBytes: &amount, UnackedBytes: &unacked, Checkpoint: &checkpoint, Original: raw, KeyRegistration: registered})
		}
		row.OriginalReports, err = json.Marshal(census)
		if err != nil {
			t.Fatal(err)
		}
	}
	closedWorkTestSign(t, artifact)
	return artifact, root
}

// Mutation happens before the containing artifact is signed again, ensuring
// each negative reaches original evidence semantics rather than outer custody.
func changeClosedReports(t *testing.T, artifact *Artifact, change func(*ClosedWorkReports)) {
	t.Helper()
	row := &artifact.ClosedWork.Records[0]
	var census ClosedWorkReports
	if err := json.Unmarshal(row.OriginalReports, &census); err != nil {
		t.Fatal(err)
	}
	change(&census)
	var err error
	row.OriginalReports, err = json.Marshal(census)
	if err != nil {
		t.Fatal(err)
	}
	closedWorkTestSign(t, artifact)
}

// Both original endpoints and the retained actual root signer join the source
// amount, while reliability, eligibility and earning-window truth remain separate.
func TestClosedReportsOriginalClientsAndIndependentRootJoinAmounts(t *testing.T) {
	artifact, root := closedReportsTestArtifact(t)
	raw, err := BytesWithContext(t.Context(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWithContext(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := VerifyClosedWorkReports(t.Context(), decoded, crypto.PubkeyToAddress(root.PublicKey))
	if err != nil || value.Contracts != 2 || value.SignedReports != 4 || value.RegisteredReports != 4 || value.AmountJoins != 2 || value.ClosedWork.UsageBytes != 200 {
		t.Fatal("original client reports failed independent source join", value, err)
	}
}

// The artifact's own valid publisher signature does not authorize key registration.
func TestClosedReportsPublisherCannotSubstituteForOriginalRootSigner(t *testing.T) {
	artifact, _ := closedReportsTestArtifact(t)
	for _, authority := range []common.Address{{}, artifact.Signer, {99}} {
		value, err := VerifyClosedWorkReports(t.Context(), artifact, authority)
		if err != nil || value.SignedReports != 4 || value.RegisteredReports != 0 || value.AmountJoins != 0 {
			t.Fatal("artifact publisher or missing root authenticated original keys", value, err)
		}
	}
}

// Rolling absence, future schemas and incomplete envelopes retain the ordinary
// payout component. Count agreement alone is never a coverage certificate.
func TestClosedReportsMissingPartialAndFutureRemainUnknown(t *testing.T) {
	for _, change := range []func(*ClosedWorkReports){
		func(c *ClosedWorkReports) { c.Count++ },
		func(c *ClosedWorkReports) { c.Reports = nil },
		func(c *ClosedWorkReports) { c.Schema = "synthetic-future-original-closes-v2" },
		func(c *ClosedWorkReports) { c.Reports[0].Original, c.Reports[0].KeyRegistration = nil, nil },
		func(c *ClosedWorkReports) { c.Reports[0].KeyRegistration = nil },
	} {
		artifact, root := closedReportsTestArtifact(t)
		changeClosedReports(t, artifact, change)
		value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey))
		if err != nil || value.RegisteredReports >= 4 || value.AmountJoins >= 2 || value.ClosedWork.UsageBytes != 200 {
			t.Fatal("missing original component acquired complete report authority", value, err)
		}
	}
	artifact, root := closedReportsTestArtifact(t)
	artifact.ClosedWork.Records[0].OriginalReports = []byte(`{"schema":"synthetic-future-original-closes-v2","future_field":1}`)
	closedWorkTestSign(t, artifact)
	if value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); err != nil || value.RegisteredReports != 2 {
		t.Fatal("unsupported optional schema poisoned a healthy original component", value, err)
	}
}

// Valid re-signing by the operator cannot alter what the original client signed.
func TestClosedReportsResignedOuterTupleAndSignatureConflictsRefuse(t *testing.T) {
	for _, change := range []func(*ClosedWorkReports){
		func(c *ClosedWorkReports) { *c.Reports[0].AckedBytes++ },
		func(c *ClosedWorkReports) { c.Reports[0].Original[len(c.Reports[0].Original)-1] ^= 1 },
		func(c *ClosedWorkReports) { c.Reports[0].ClientId = "03000000-0000-0000-0000-000000000000" },
		func(c *ClosedWorkReports) {
			c.Reports[1].ReportId, c.Reports[1].ClientId = c.Reports[0].ReportId, c.Reports[0].ClientId
		},
	} {
		artifact, root := closedReportsTestArtifact(t)
		changeClosedReports(t, artifact, change)
		if err := VerifyWithContext(t.Context(), artifact); err != nil {
			t.Fatal("negative lost its otherwise valid original artifact", err)
		}
		if value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("re-signed conflicting client original was accepted", value, err)
		}
	}
}

// A correctly signed old-policy report stays in its original namespace.
func TestClosedReportsForeignPolicyCannotMigrateOriginalAuthority(t *testing.T) {
	artifact, root := closedReportsTestArtifact(t)
	changeClosedReports(t, artifact, func(c *ClosedWorkReports) {
		original, err := coreprotocol.DecodeOriginalCloseReport(c.Reports[0].Original)
		if err != nil {
			t.Fatal(err)
		}
		original.DomainHash[0]++
		original, err = coreprotocol.SignOriginalCloseReport(original, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{40}, 32)))
		if err != nil {
			t.Fatal(err)
		}
		c.Reports[0].Original, _ = original.Bytes()
	})
	value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey))
	if err != nil || value.SignedReports != 3 || value.RegisteredReports != 3 || value.AmountJoins != 1 {
		t.Fatal("foreign original policy acquired current amount authority", value, err)
	}
}

// Even a valid original root signature cannot join a provider's work to another
// registered network. A valid foreign-policy registration remains unknown instead.
func TestClosedReportsOriginalRegistrationBindsProviderIdentity(t *testing.T) {
	artifact, root := closedReportsTestArtifact(t)
	changeClosedReports(t, artifact, func(c *ClosedWorkReports) {
		registration, err := protocol.DecodeClientKeyRegistration(c.Reports[0].KeyRegistration)
		if err != nil {
			t.Fatal(err)
		}
		registration.NetworkID[0]++
		if err := protocol.SignClientKeyRegistration(&registration, root); err != nil {
			t.Fatal(err)
		}
		c.Reports[0].KeyRegistration, _ = registration.Bytes()
	})
	if value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) || !strings.Contains(err.Error(), "retained provider identity") {
		t.Fatal("authorized original registration changed immutable provider identity", value, err)
	}
	artifact, root = closedReportsTestArtifact(t)
	changeClosedReports(t, artifact, func(c *ClosedWorkReports) {
		registration, _ := protocol.DecodeClientKeyRegistration(c.Reports[0].KeyRegistration)
		registration.Domain.PolicyHash[0]++
		if err := protocol.SignClientKeyRegistration(&registration, root); err != nil {
			t.Fatal(err)
		}
		c.Reports[0].KeyRegistration, _ = registration.Bytes()
	})
	if value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); err != nil || value.RegisteredReports != 3 || value.AmountJoins != 1 {
		t.Fatal("foreign original registration acquired authority or poisoned the current component", value, err)
	}
}

// Legacy canonical bytes remain exact and an owned cancellation keeps its cause.
func TestClosedReportsLegacyBytesAndCancellationStayDistinct(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	before, err := Bytes(artifact)
	if err != nil || bytes.Contains(before, []byte("original_close_reports")) {
		t.Fatal("legacy artifact gained optional original report bytes", err)
	}
	value, err := VerifyClosedWorkReports(t.Context(), artifact, common.Address{1})
	if err != nil || value.Contracts != 0 || value.SignedReports != 0 || value.AmountJoins != 0 {
		t.Fatal("unsigned original gained client evidence", value, err)
	}
	after, _ := Bytes(artifact)
	if !bytes.Equal(before, after) {
		t.Fatal("reading old component resigned original bytes")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := VerifyClosedWorkReports(ctx, artifact, common.Address{1}); !errors.Is(err, context.Canceled) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("owned original cancellation became conflicting evidence", err)
	}
}

// Distinct contracts cannot reuse the client's operation namespace, even when
// both changed envelopes and the containing artifact have valid signatures.
func TestClosedReportsCrossContractOperationReuseRefuses(t *testing.T) {
	artifact, root := closedReportsTestArtifact(t)
	var first, second ClosedWorkReports
	if err := json.Unmarshal(artifact.ClosedWork.Records[0].OriginalReports, &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(artifact.ClosedWork.Records[1].OriginalReports, &second); err != nil {
		t.Fatal(err)
	}
	original, _ := coreprotocol.DecodeOriginalCloseReport(second.Reports[0].Original)
	prior, _ := coreprotocol.DecodeOriginalCloseReport(first.Reports[0].Original)
	original.ReportId = prior.ReportId
	original, err := coreprotocol.SignOriginalCloseReport(original, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{40}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	second.Reports[0].ReportId = first.Reports[0].ReportId
	second.Reports[0].Original, _ = original.Bytes()
	artifact.ClosedWork.Records[1].OriginalReports, err = json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	closedWorkTestSign(t, artifact)
	if value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) || !strings.Contains(err.Error(), "reused across contracts") {
		t.Fatal("one original operation was counted in two contracts", value, err)
	}
}

// A retained availability diagnostic cannot replace registration authority or
// invalidate the remaining independently admitted source amounts.
func TestClosedReportsOptionalHistoryIssuesCannotAuthorizeKeys(t *testing.T) {
	for _, issue := range []string{"history_not_found", "history_capacity", "history_read_unavailable", "synthetic_future_diagnostic"} {
		artifact, root := closedReportsTestArtifact(t)
		changeClosedReports(t, artifact, func(c *ClosedWorkReports) {
			c.Reports[0].KeyRegistration = nil
			c.Reports[0].KeyIssue = issue
		})
		value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey))
		if err != nil || value.SignedReports != 4 || value.RegisteredReports != 3 || value.AmountJoins != 1 || value.ClosedWork.UsageBytes != 200 {
			t.Fatal("optional diagnostic changed original authority or healthy component", issue, value, err)
		}
	}
	artifact, root := closedReportsTestArtifact(t)
	changeClosedReports(t, artifact, func(c *ClosedWorkReports) { c.Reports[0].KeyIssue = "history_capacity" })
	if value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("contradictory present registration and absent-history diagnostic admitted", value, err)
	}
}
