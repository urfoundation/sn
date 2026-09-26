//go:build linux || darwin

// The rate bridge retains real signed attempt history while independently
// admitting the exact governed policy at each decision's chain boundary.
package validator

import (
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Construct only the approved rate revision from an independently owned old
// document. All identity, cadence, proof and custody fields remain unchanged.
func releaseRateAmendmentTestNext(t *testing.T, previous protocol.Policy) protocol.Policy {
	t.Helper()
	next := *cloneReleasePolicy(&previous)
	next.PolicyID++
	for index := range next.Deposit.Tiers {
		next.Deposit.Tiers[index].RateNumeratorRaoPerGiB *= 40_000
	}
	if err := protocol.ValidateTestnetRateAmendment(&previous, &next); err != nil {
		t.Fatal(err)
	}
	return next
}

// No mutation of one selected policy can change another reader's authority.
func TestReleaseRateAmendmentSelectsExactOwnedDocuments(t *testing.T) {
	previous := exactPolicy(t)
	next := releaseRateAmendmentTestNext(t, previous)
	oldHash, _ := previous.HashHex()
	newHash, _ := next.HashHex()
	cfg := ReleaseConfig{Policy: next, PreviousPolicy: &previous, PolicyHash: newHash}
	for _, expected := range []protocol.Policy{previous, next} {
		hash, _ := expected.HashHex()
		selected, err := ReleasePolicyForHash(&cfg, hash)
		if err != nil || !reflect.DeepEqual(selected, expected) {
			t.Fatalf("exact document refused: %v", err)
		}
		selected.Deposit.Tiers[0].RateNumeratorRaoPerGiB++
		again, err := ReleasePolicyForHash(&cfg, hash)
		if err != nil || !reflect.DeepEqual(again, expected) {
			t.Fatalf("selection shared mutable authority: %v", err)
		}
	}
	historical, err := releaseConfigForPolicyHash(&cfg, oldHash)
	if err != nil || historical.PolicyHash != oldHash || historical.PreviousPolicy != nil || !reflect.DeepEqual(historical.Policy, previous) {
		t.Fatalf("historical projection changed authority: %v", err)
	}
	if _, err := ReleasePolicyForHash(&cfg, releaseHex32([32]byte{0x77})); err == nil {
		t.Fatal("foreign hash was accepted")
	}
	for _, mutate := range []func(*protocol.Policy){
		func(policy *protocol.Policy) { policy.Deposit.EpochCapRaoPerOperator++ },
		func(policy *protocol.Policy) { policy.Deposit.Tiers[1].MinConvictionRao++ },
		func(policy *protocol.Policy) { policy.Verify.ReliabilityAMin++ },
	} {
		altered := *cloneReleasePolicy(&next)
		mutate(&altered)
		cfg.Policy, cfg.PolicyHash = altered, newHash
		if _, err := ReleasePolicyForHash(&cfg, oldHash); err == nil {
			t.Fatal("changed current document retained predecessor authority")
		}
		cfg.PolicyHash, _ = altered.HashHex()
		if _, err := ReleasePolicyForHash(&cfg, oldHash); err == nil {
			t.Fatal("rehashed adjacent change gained predecessor authority")
		}
	}
}

// A new artifact can cross one consecutive settlement boundary with exactly
// the old signed terminal and cut. The generic cut verifier remains strict.
func TestReleaseRateAmendmentReplaysOriginalCutsAcrossLineage(t *testing.T) {
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 1, false, 1)
	previous, current := fixture.previous.artifact, fixture.current.artifact
	current.Policy = releaseRateAmendmentTestNext(t, previous.Policy)
	current.PolicyHash, _ = current.Policy.HashHex()
	for index := range current.DepositAudits {
		old := current.DepositAudits[index]
		audit := releaseMeasurementDepositAudit(t, current.Policy, old.NoID)
		audit.Epoch, audit.SourceEpoch, audit.ObservedAtBlock = old.Epoch, old.SourceEpoch, old.ObservedAtBlock
		current.DepositAudits[index] = audit
	}
	for _, input := range current.Inputs {
		if _, err := attemptCutV2PolicyDepth(input.AttemptCutV2.Context, current.Policy); err == nil {
			t.Fatal("generic replay accepted a successor policy under the original activation")
		}
	}
	encoded, err := canonicalReleaseMeasurementBytes(previous)
	if err != nil {
		t.Fatal(err)
	}
	options := fixture.options(t)
	options.ReplayPolicy = cloneReleasePolicy(&previous.Policy)
	verified, err := VerifyReleaseMeasurementLineageV2(t.Context(), encoded, fixture.previous.options(t), current, options)
	if err != nil || verified.Decision == nil || len(verified.ReplayByNO) != 2 || len(verified.SettlementReplayByNO) != 2 {
		t.Fatalf("actual retained lineage failed: %v", err)
	}
	if !reflect.DeepEqual(current.Inputs[0].AttemptCutV2.Context.Activation, previous.Inputs[0].AttemptCutV2.Context.Activation) {
		t.Fatal("amendment replaced immutable activation")
	}
}

// A hash change within the same governed epoch has no amendment authority.
func TestReleaseRateAmendmentRejectsSameEpochAndReverseLineage(t *testing.T) {
	previous := &ReleaseMeasurementArtifact{Policy: exactPolicy(t), SettlementEpoch: 7}
	previous.PolicyHash, _ = previous.Policy.HashHex()
	current := &ReleaseMeasurementArtifact{Policy: releaseRateAmendmentTestNext(t, previous.Policy), SettlementEpoch: 7}
	current.PolicyHash, _ = current.Policy.HashHex()
	if err := verifyReleasePolicyLineage(previous, current); err == nil {
		t.Fatal("same-epoch policy mutation accepted")
	}
	current.SettlementEpoch++
	if err := verifyReleasePolicyLineage(previous, current); err != nil {
		t.Fatal(err)
	}
	previous.SettlementEpoch = current.SettlementEpoch + 1
	if err := verifyReleasePolicyLineage(current, previous); err == nil {
		t.Fatal("policy rollback accepted")
	}
}

// A served policy snapshot is encoded through the real checked-in ABI, and
// every successful assertion still makes the actual pinned Evm calls.
func releaseRateAmendmentTestSnapshot(t *testing.T, policy protocol.Policy, effective, block uint64) stabi.STCoordinatorPolicySnapshot {
	t.Helper()
	hash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return stabi.STCoordinatorPolicySnapshot{PolicyHash: hash, EffectiveEpoch: effective, EffectiveBlock: block,
		EpochBlocks: policy.Settlement.EpochBlocks, RootCommitWindowBlocks: policy.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: policy.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs: policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: policy.Binding.MaximumValidityEpochs,
		EpochDepositCapRao:           new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator),
		CampaignDepositCapRao:        new(big.Int).SetUint64(policy.Deposit.TotalTestCampaignCapRao)}
}

// Known documents alone cannot authorize a current decision: its exact hash
// must be returned by policyAt at that decision's historical block.
func TestReleaseRateAmendmentDecisionRequiresActualChainTransition(t *testing.T) {
	fixture := newReleaseDecisionV2TestFixture(t)
	previous := fixture.query.policy
	next := releaseRateAmendmentTestNext(t, previous)
	newHash, _ := next.HashHex()
	oldHash, _ := previous.HashHex()
	cfg := ReleaseConfig{Policy: next, PreviousPolicy: &previous, PolicyHash: newHash}
	originalDomain := fixture.query.domain
	boundary := fixture.query.boundary
	if err := fixture.chain.authenticateReleaseStartupBoundaryV2WithPolicy(t.Context(), originalDomain, 1, boundary, false, false, &cfg, oldHash); err != nil {
		t.Fatalf("historical original boundary refused: %v", err)
	}
	query := fixture.query
	query.policy = next
	query.domain.PolicyHash, _ = next.Hash()
	if _, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), query); err == nil {
		t.Fatal("new decision accepted before actual policyAt transition")
	}
	actual := releaseRateAmendmentTestSnapshot(t, next, boundary.SettlementEpoch, boundary.EVMBlock-5)
	fixture.set(t, "policyAt", fixture.chain.coordinator.PackPolicyAt(new(big.Int).SetUint64(boundary.SettlementEpoch)), actual)
	if _, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), query); err != nil {
		t.Fatalf("actual new decision boundary refused: %v", err)
	}
	if err := fixture.chain.authenticateReleaseStartupBoundaryV2WithPolicy(t.Context(), originalDomain, 1, boundary, false, false, &cfg, newHash); err != nil {
		t.Fatalf("old activation/new decision boundary refused: %v", err)
	}
	if err := fixture.chain.authenticateReleaseStartupBoundaryV2Context(t.Context(), originalDomain, 1, boundary, false); err == nil {
		t.Fatal("strict caller gained unconfigured amendment authority")
	}
	if err := fixture.chain.authenticateReleaseStartupBoundaryV2WithPolicy(t.Context(), originalDomain, 1, boundary, false, false, &cfg, oldHash); err == nil {
		t.Fatal("old journal relabeled at a new policy boundary")
	}
	actual.EpochDepositCapRao.Add(actual.EpochDepositCapRao, big.NewInt(1))
	fixture.set(t, "policyAt", fixture.chain.coordinator.PackPolicyAt(new(big.Int).SetUint64(boundary.SettlementEpoch)), actual)
	if err := fixture.chain.authenticateReleaseStartupBoundaryV2WithPolicy(t.Context(), originalDomain, 1, boundary, false, false, &cfg, newHash); err == nil {
		t.Fatal("on-chain changed cap escaped exact document verification")
	}
}

// Restart resolves old signed decision fields against old policyAt, even when
// its configured live policy is the reviewed successor.
func TestReleaseRateAmendmentHistoricalDecisionKeepsOriginalDomain(t *testing.T) {
	fixture := newReleaseDecisionV2TestFixture(t)
	for key, view := range fixture.views {
		if view.method == "bindingAt" {
			view.data = slices.Clone(view.data)
			clear(view.data[:32])
			fixture.views[key] = view
		}
	}
	for _, operator := range fixture.query.operators {
		fixture.set(t, "rootCommitments", fixture.chain.coordinator.PackRootCommitments(big.NewInt(0), new(big.Int).SetUint64(operator.noID)), [32]byte{}, [32]byte{}, common.Address{}, uint64(0))
	}
	native := newReleaseDecisionV2NativeTestFixture(t, fixture)
	history, intent, artifact := releaseDecisionV2HistoricalTestReference(t, fixture, native)
	previous := *cloneReleasePolicy(&history.cfg.Policy)
	history.cfg.Policy = releaseRateAmendmentTestNext(t, previous)
	history.cfg.PolicyHash, _ = history.cfg.Policy.HashHex()
	history.cfg.PreviousPolicy = &previous
	if err := history.authenticateIntentChainReference(t.Context(), fixture.chain, native.chain, native.expected, intent, artifact); err != nil {
		t.Fatalf("retained old decision refused after amendment: %v", err)
	}
	before := artifact.PolicyHash
	artifact.PolicyHash = history.cfg.PolicyHash
	if err := history.authenticateIntentChainReference(t.Context(), fixture.chain, native.chain, native.expected, intent, artifact); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("relabeling old chain history accepted: %v", err)
	}
	artifact.PolicyHash = before
	for _, policyHash := range []string{before, history.cfg.PolicyHash} {
		artifact.PolicyHash = policyHash
		domain, _, err := releaseClientKeyDecisionV2(&history.cfg, 1, native.hotkey, artifact, releaseMeasurementTestID(1))
		if err != nil || releaseHex32(domain.PolicyHash) != policyHash {
			t.Fatalf("client-key domain did not select exact decision policy: %v", err)
		}
	}
}

// The same private cut bytes survive config adoption and still select only
// their original replay policy, never the decision policy from outer labels.
func TestReleaseRateAmendmentStartupInputKeepsOriginalReplay(t *testing.T) {
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	input := fixture.detach(t)
	previous := *cloneReleasePolicy(&fixture.steerer.cfg.Policy)
	fixture.steerer.cfg.Policy = releaseRateAmendmentTestNext(t, previous)
	fixture.steerer.cfg.PolicyHash, _ = fixture.steerer.cfg.Policy.HashHex()
	fixture.steerer.cfg.PreviousPolicy = &previous
	if err := validateReleaseMeasurementInputV2Context(fixture.steerer.cfg, input.NoID, input.AttemptCutV2.Context); err != nil {
		t.Fatalf("original activation refused: %v", err)
	}
	encoded, err := canonicalReleaseMeasurementInputBytes(&releaseMeasurementInputJournal{Schema: releaseMeasurementInputV2Schema,
		DeploymentID: fixture.steerer.cfg.DeploymentID, ChainID: fixture.steerer.cfg.ChainID, GenesisHash: fixture.steerer.cfg.GenesisHash,
		Coordinator: fixture.steerer.cfg.Coordinator, ValidatorID: fixture.steerer.cfg.ValidatorID, Netuid: fixture.steerer.cfg.Netuid,
		SubnetEpoch: 7, PolicyHash: releaseHex32(input.AttemptCutV2.Context.Activation.Domain.PolicyHash), MeasurementInput: input})
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{cfg: *fixture.steerer.cfg}
	history.cfg.EvidenceV2.Bounds = ReleaseEvidenceV2Bounds{MaxInputJournalBytes: fixture.options.MaxJournalBytes, Cut: fixture.options.Stats.Bounds,
		MaxProviders: fixture.options.Stats.Stats.MaxProviders, MaxEgressHashes: fixture.options.Stats.Stats.MaxEgressHashes}
	journal, err := history.decodeInput(t.Context(), releaseEvidenceV2HistoryFile{epoch: 7, noID: input.NoID, encoded: encoded})
	if err != nil || journal.PolicyHash == history.cfg.PolicyHash {
		t.Fatalf("startup lost original input policy: %v", err)
	}
}

// Public deposit-audit payloads can follow the new decision policy only with
// exact independent amendment authority; their signed header stays original.
func TestReleaseRateAmendmentDepositEvidenceKeepsActivation(t *testing.T) {
	previous := exactPolicy(t)
	next := releaseRateAmendmentTestNext(t, previous)
	oldHash, _ := previous.Hash()
	newHash, _ := next.HashHex()
	fixture := newReleaseDecisionV2TestFixture(t)
	domain := fixture.query.domain
	domain.PolicyHash = oldHash
	cfg := validReleaseConfig(t)
	// The fixture's synthetic deployment digest must match the complete claim.
	domain.DeploymentIDHash = sha256.Sum256([]byte(cfg.DeploymentID))
	decision := ReleaseMeasurementV2Decision{DeploymentID: cfg.DeploymentID, ChainID: domain.ChainID, GenesisHash: releaseHex32(domain.GenesisHash),
		Coordinator: strings.ToLower(common.Address(domain.Coordinator).Hex()), SettlementVault: strings.ToLower(common.Address(domain.SettlementVault).Hex()),
		Netuid: domain.Netuid, ValidatorID: 1, PolicyHash: newHash, NativeSnapshotBlock: 200, NativeSnapshotHash: releaseHex32([32]byte{0x31}),
		EVMSnapshotBlock: 150, EVMSnapshotHash: releaseHex32([32]byte{0x32}), SettlementEpoch: 2, SubnetEpoch: 3}
	window := protocol.ValidatorEvidenceWindow{Epoch: 1, StartBlock: 10, EndBlock: 100, FinalizedBlock: 150, Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: 2, NativeEpoch: 3}}
	if err := validateValidatorEvidenceDepositAuditV2Decision(decision, domain, window); err == nil {
		t.Fatal("unconfigured reader accepted new policy")
	}
	if err := validateValidatorEvidenceDepositAuditV2DecisionWithPolicy(decision, domain, window, &next, &previous); err != nil {
		t.Fatal(err)
	}
	if domain.PolicyHash != oldHash {
		t.Fatal("payload bridge mutated signed activation")
	}
	next.Deposit.Tiers[0].RateNumeratorRaoPerGiB++
	if err := validateValidatorEvidenceDepositAuditV2DecisionWithPolicy(decision, domain, window, &next, &previous); err == nil {
		t.Fatal("foreign tier gained public bridge authority")
	}
}

// The production local observer reports retained applied work under the exact
// new config/handoff; no RPC, fabricated proof, or final-acceptance flag is used.
func TestReleaseRateAmendmentProvisionalObserverRetainsOldIntents(t *testing.T) {
	_, storePath := authenticatedIntentHistoryFixture(t)
	cfg := validReleaseConfig(t)
	previous := *cloneReleasePolicy(&cfg.Policy)
	cfg.Policy = releaseRateAmendmentTestNext(t, previous)
	cfg.PreviousPolicy = &previous
	cfg.PolicyHash, _ = cfg.Policy.HashHex()
	cfg.StateDir = filepath.Dir(storePath)
	configPath := writeReleaseConfig(t, cfg)
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	setup := ProvisionalActivationSetupV2{Schema: ProvisionalActivationSetupV2Schema, Provisional: true,
		DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID,
		ApprovedPlanHash: releaseHex32([32]byte{0x41}), SourcePlanHash: releaseHex32([32]byte{0x42}),
		PreparedSHA256:  provisionalActivationSetupSHA256([]byte("synthetic prepared")),
		CompletedSHA256: provisionalActivationSetupSHA256([]byte("synthetic completed")),
		ConfigSHA256:    provisionalActivationSetupSHA256(configBytes)}
	for _, action := range []string{"evidence.activate.1.1", "evidence.activate.1.2", "evidence.activate.2.1", "evidence.activate.2.2", "evidence.activation-boundary"} {
		setup.Receipts = append(setup.Receipts, ProvisionalActivationSetupV2Receipt{ActionID: action,
			PostconditionHash: releaseHex32([32]byte{0x43}), SHA256: provisionalActivationSetupSHA256([]byte(action))})
	}
	for _, operator := range cfg.EvidenceV2.Operators {
		setup.Members = append(setup.Members, ProvisionalActivationSetupV2Member{NoID: operator.NoID, ContextSHA256: operator.Context.SHA256, PublishedBlock: 17})
	}
	encoded, err := json.MarshalIndent(setup, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	options := ProvisionalIntentObservationV2Options{ConfigPath: configPath, Handoff: encoded,
		HandoffSHA256: provisionalActivationSetupSHA256(encoded), PlanHash: setup.ApprovedPlanHash,
		AcceptedPlanHashes: []string{setup.ApprovedPlanHash, setup.SourcePlanHash}, DeploymentID: cfg.DeploymentID,
		ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, Hotkey: testIntentHotkey(t).PublicKey()}
	result, err := ObserveProvisionalIntentsV2(t.Context(), options)
	if err != nil || result.State != "observed" || result.RecordedAppliedIntents == nil || *result.RecordedAppliedIntents != 2 || result.FinalAcceptance {
		t.Fatalf("old applied history became unknown after exact rate adoption: %+v %v", result, err)
	}
	original, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	var state steeringIntentFile
	if err := json.Unmarshal(original, &state); err != nil {
		t.Fatal(err)
	}
	state.Current.PolicyHash = releaseHex32([32]byte{0x44})
	writeIntentHistoryFixture(t, storePath, state)
	result, err = ObserveProvisionalIntentsV2(t.Context(), options)
	if err == nil || result.State != "unknown" || result.RecordedAppliedIntents != nil || result.FinalAcceptance {
		t.Fatalf("observer accepted foreign policy or published partial progress: %+v %v", result, err)
	}
}
