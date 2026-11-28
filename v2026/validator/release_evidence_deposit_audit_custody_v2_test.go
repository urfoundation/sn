//go:build linux || darwin

// A paid audit's retained HTTP capture is replayed inside a custody that
// startup shares across every retained intent.
package validator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A paid deposit audit replayed from its retained HTTP capture checks only the
// custody it acquired. Startup shares one custody across every retained
// intent, so a full check after each audit made its disk stage quadratic in
// retained intents. An earlier entry's file replaced by identical bytes under a
// new inode is visible only to that earlier owner's check, and the custody
// owner's final check still refuses it.
func TestValidatorEvidenceDepositAuditV2CaptureChecksOnlyItsOwnCustody(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	runtime := fixture.base.runtime
	decision := releaseMeasurementV2Decision(fixture.artifact)
	decision.PreviousArtifactHash = ""
	claimed := fixture.artifact.DepositAudits
	if len(claimed) == 0 || claimed[0].HttpObservationHash == "" {
		t.Fatal("the paid fixture has no retained HTTP capture")
	}
	// The same pinned observation the source owner makes for this decision.
	decisionCfg, err := releaseConfigForPolicyHash(&runtime.cfg, decision.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := runtime.sources[runtime.history.participants[0].NoID].activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	queryDomain := domain
	if queryDomain.PolicyHash, err = decisionCfg.Policy.Hash(); err != nil {
		t.Fatal(err)
	}
	bounds := runtime.cfg.EvidenceV2.Bounds
	query := releaseDecisionChainV2Query{domain: queryDomain, boundary: AttemptBoundary{SettlementEpoch: decision.SettlementEpoch, EVMBlock: decision.EVMSnapshotBlock, EVMBlockHash: decision.EVMSnapshotHash}, policy: decisionCfg.Policy,
		maxOperators: bounds.MaxOperators, maxProviders: bounds.MaxProviders, maxControlBytes: bounds.MaxControlBytes}
	for _, participant := range runtime.history.participants {
		query.operators = append(query.operators, releaseDecisionChainV2OperatorQuery{noID: participant.NoID})
	}
	nativeHash, err := canonicalAttemptHex32("synthetic deposit audit native boundary", decision.NativeSnapshotHash, false)
	if err != nil {
		t.Fatal(err)
	}
	observationCtx, cancel := context.WithTimeout(t.Context(), releaseNativeEndpointTimeout(&runtime.cfg))
	observed, _, err := readReleaseDecisionWithConfigV2Context(observationCtx, runtime.chain, runtime.native, query, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(domain.GenesisHash), BlockHash: types.Hash(nativeHash), BlockNumber: decision.NativeSnapshotBlock, Netuid: domain.Netuid, Hotkey: runtime.hotkey.PublicKey(), MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, releaseRuntimeIdentityV2(&runtime.cfg), decisionCfg)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	projection := releaseDepositAuditV2Artifact(decision, claimed, decisionCfg.Policy)
	history := *runtime.history
	history.cfg = *decisionCfg
	// An earlier retained intent's file already in the shared custody.
	earlier := filepath.Join(runtime.cfg.StateDir, "synthetic-earlier-intent.json")
	raw := []byte("{\"synthetic\":\"earlier retained intent\"}\n")
	if err := os.WriteFile(earlier, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
	if _, err := custody.read(t.Context(), earlier, uint64(len(raw)), false); err != nil {
		t.Fatal(err)
	}
	replacement := earlier + ".synthetic-replacement"
	if err := os.WriteFile(replacement, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, earlier); err != nil {
		t.Fatal(err)
	}
	audit, err := history.historicalDepositAuditWithCaptureV2(t.Context(), observed, observed.operators[0], claimed[0], projection, custody)
	if err != nil || audit != claimed[0] {
		t.Errorf("the paid audit checked the earlier entry's custody again: %v", err)
	}
	if err := custody.check(); err == nil {
		t.Error("the custody owner's final check accepted a replaced earlier file")
	}
	// Closing reports the same replaced earlier file.
	_ = custody.close()
}
