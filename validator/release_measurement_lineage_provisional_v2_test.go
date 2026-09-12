//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Empty streams are actually sealed from real private disk ledgers. This
// fixture tests signed lineage/custody across the observed epoch numbers; it
// does not claim positive provider usage, native scores or live-chain receipts.
func newProvisionalMeasurementLineageV2Initial(t *testing.T) *releaseMeasurementV2TestFixture {
	t.Helper()
	f := &releaseMeasurementV2TestFixture{operators: map[uint64]*releaseMeasurementV2TestOperator{}}
	a := &ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, SubnetEpoch: 1401,
		NativeSnapshotBlock: 200, NativeSnapshotHash: releaseHex32([32]byte{0x51}),
		ControlledNOIDs: []uint64{}, Inputs: []ReleaseMeasurementInput{}, Bindings: []ReleaseBindingMeasurement{},
		HeadEMA: []HeadEMAMeasurement{}, Pools: []ReleasePoolMeasurement{}, DepositAudits: []DepositAudit{}}
	for _, noID := range []uint64{9, 10} {
		seal := newAttemptCutV2SealTestFixtureForOperator(t, 8, 0, 0, noID)
		// The independent context is chosen before signing; no retained signed
		// record is relabeled to manufacture the numeric regression fixture.
		seal.expected.Boundary.SettlementEpoch = 302
		seal.expected.Activation.Domain.ActivationHash = [32]byte{0x14, byte(noID)}
		seal.engine.stats.mu.Lock()
		stats := seal.engine.stats.releaseStatsMeasurementWithLock()
		seal.engine.stats.mu.Unlock()
		options, _ := newAttemptCutV2SealTestOptions(t, seal)
		cut, _, err := SealAttemptCutV2(t.Context(), seal.ledger, seal.expected, seal.policy, seal.key, seal.bounds, options)
		if err != nil || cut == nil || cut.RecordCount != 0 {
			t.Fatalf("real initial302 seal: %v", err)
		}
		identity, domain := seal.expected.Identity, seal.expected.Activation.Domain
		a.DeploymentID, a.ChainID, a.GenesisHash = identity.DeploymentID, identity.ChainID, identity.GenesisHash
		a.Coordinator = strings.ToLower(common.Address(domain.Coordinator).Hex())
		a.SettlementVault = strings.ToLower(common.Address(domain.SettlementVault).Hex())
		a.ValidatorID, a.Netuid, a.SelfUID = identity.ValidatorID, identity.Netuid, identity.ValidatorUID
		a.Policy, a.PolicyHash = seal.policy, releaseHex32(domain.PolicyHash)
		a.SettlementEpoch, a.EVMSnapshotBlock, a.EVMSnapshotHash = 302, cut.Context.Boundary.EVMBlock, cut.Context.Boundary.EVMBlockHash
		a.Inputs = append(a.Inputs, ReleaseMeasurementInput{NoID: noID, SettlementEpoch: 302,
			CutNativeBlock: a.NativeSnapshotBlock, CutNativeBlockHash: a.NativeSnapshotHash,
			CutEVMSnapshotBlock: a.EVMSnapshotBlock, CutEVMSnapshotHash: a.EVMSnapshotHash,
			EgressGeneration: cut.Context.EgressGeneration, Stats: stats, AttemptCutV2: cut})
		a.Pools = append(a.Pools, ReleasePoolMeasurement{NoID: noID, UID: uint16(100 + noID), PoolHotkey: releaseHex32([32]byte{0x61, byte(noID)})})
		audit := releaseMeasurementDepositAudit(t, seal.policy, noID)
		audit.Epoch, audit.SourceEpoch, audit.ObservedAtBlock = 302, 302-seal.policy.Deposit.UsageLagEpochs, a.EVMSnapshotBlock
		a.DepositAudits = append(a.DepositAudits, audit)
		f.operators[noID] = &releaseMeasurementV2TestOperator{seal: seal, metadata: options.ReadMetadata, data: options.OpenData}
	}
	f.artifact = a
	return f
}

func TestProvisionalMeasurementLineageV2AuthenticatesActualTerminalGap(t *testing.T) {
	t.Parallel()
	previous := newProvisionalMeasurementLineageV2Initial(t)
	previousBytes, err := canonicalReleaseMeasurementBytes(previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{retainedStartup: true,
		terminals: map[uint64]*AttemptSettlementClosureV2{}, terminalContexts: map[uint64]map[uint64]AttemptCutV2Context{},
		keys: map[uint64]map[byte]ed25519.PublicKey{}, readers: [2]*HTTPAttemptStreamV2Reader{{}, {}}}
	current := previous
	var last *releaseMeasurementV2SettlementTestFixture
	for step := 0; step < 4; step++ {
		var terminalInputs []*attemptCutV2StatsTestFixture
		contexts := map[uint64]AttemptCutV2Context{}
		for _, input := range current.artifact.Inputs {
			operator := current.operators[input.NoID]
			terminalInputs = append(terminalInputs, &attemptCutV2StatsTestFixture{seal: operator.seal,
				cut: *input.AttemptCutV2, measurement: input.Stats, metadata: operator.metadata, data: operator.data})
			contexts[input.NoID] = operator.seal.expected
		}
		// Four real settlement transitions span three native epoch increments.
		last = newReleaseMeasurementV2SettlementTestFromTerminal(t, current, terminalInputs, step == 3, 1)
		history.terminals[current.artifact.SettlementEpoch] = last.current.artifact.SettlementClosureV2
		history.terminalContexts[current.artifact.SettlementEpoch] = contexts
		current = last.current
	}
	current.artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(previousBytes)
	if previous.artifact.SubnetEpoch != 1401 || previous.artifact.SettlementEpoch != 302 || current.artifact.SubnetEpoch != 1404 || current.artifact.SettlementEpoch != 306 || len(history.terminals) != 4 {
		t.Fatal("actual gap fixture changed its native/settlement scope")
	}
	first := previous.operators[9].seal
	cfg := ReleaseConfig{ChainID: 945, Policy: previous.artifact.Policy, ProvisionalDeferClosedNativeInput: true,
		EvidenceV2: ReleaseEvidenceV2Config{Bounds: ReleaseEvidenceV2Bounds{
			Cut: first.bounds, Replay: first.replay, MaxParticipants: 2, MaxTransitionBytes: 256 * 1024,
			MaxClosureBytes: 1024 * 1024, MaxProviders: 16, MaxEgressHashes: 16, MaxFleetPrefixes: 64}}}
	for _, input := range previous.artifact.Inputs {
		operator := previous.operators[input.NoID]
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: input.NoID, Ledger: operator.seal.ledger})
		history.keys[input.NoID] = operator.seal.server.serverPublicKeys()
		cfg.EvidenceV2.Operators = append(cfg.EvidenceV2.Operators, ReleaseEvidenceV2OperatorConfig{NoID: input.NoID,
			ReplayScratchRoot: newReleaseHeadV2TestStateDir(t), SealScratchRoot: newReleaseHeadV2TestStateDir(t)})
	}
	runtime := &releaseRuntimeV2{cfg: cfg, history: history, gate: make(chan struct{}, 1)}
	store := &IntentStore{v2: &releaseIntentV2Owner{runtime: runtime}}
	previousOptions, currentOptions := previous.options(t), last.options(t)
	beforeCurrent, err := canonicalReleaseMeasurementBytes(current.artifact)
	if err != nil {
		t.Fatal(err)
	}
	beforeTerminals := map[uint64][]byte{}
	for epoch, closure := range history.terminals {
		raw, err := marshalAttemptSettlementV2JSON(t.Context(), closure, cfg.EvidenceV2.Bounds.MaxClosureBytes, false, true)
		if err != nil {
			t.Fatal(err)
		}
		beforeTerminals[epoch] = raw
	}
	if err := store.verifyMeasurementLineageV2(t.Context(), previousBytes, previousOptions, current.artifact, currentOptions); err == nil {
		t.Fatal("strict/default intent owner accepted a nonconsecutive lineage")
	}
	store.v2.provisionalEpochGaps = true
	if err := store.verifyMeasurementLineageV2(t.Context(), previousBytes, previousOptions, current.artifact, currentOptions); err != nil {
		t.Fatalf("actual authenticated302..305 terminal chain rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func() func()
	}{
		{"missing interior terminal", func() func() {
			original := history.terminals[303]
			delete(history.terminals, 303)
			return func() { history.terminals[303] = original }
		}},
		{"changed terminal signature", func() func() {
			history.terminals[303].Transitions[0].Signature[0] ^= 1
			return func() { history.terminals[303].Transitions[0].Signature[0] ^= 1 }
		}},
		{"missing independent context", func() func() {
			original := history.terminalContexts[303]
			delete(history.terminalContexts, 303)
			return func() { history.terminalContexts[303] = original }
		}},
		{"unvalidated runtime", func() func() {
			history.retainedStartup = false
			return func() { history.retainedStartup = true }
		}},
		{"missing explicit provisional flag", func() func() {
			runtime.cfg.ProvisionalDeferClosedNativeInput = false
			return func() { runtime.cfg.ProvisionalDeferClosedNativeInput = true }
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			undo := test.mutate()
			defer undo()
			if err := store.verifyMeasurementLineageV2(t.Context(), previousBytes, previousOptions, current.artifact, currentOptions); err == nil {
				t.Fatal("invalid retained history accepted")
			}
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(*ReleaseMeasurementArtifact)
	}{
		{"wrong predecessor hash", func(a *ReleaseMeasurementArtifact) { a.PreviousArtifactHash = "sha256:" + strings.Repeat("a", 64) }},
		{"native epoch regression", func(a *ReleaseMeasurementArtifact) { a.SubnetEpoch = 1400 }},
		{"native boundary regression", func(a *ReleaseMeasurementArtifact) { a.NativeSnapshotBlock = 199 }},
		{"different deployment", func(a *ReleaseMeasurementArtifact) { a.DeploymentID = "another-deployment" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := cloneReleaseMeasurementArtifact(t, current.artifact)
			test.mutate(changed)
			options := currentOptions
			options.Expected = releaseMeasurementV2Decision(changed)
			if err := store.verifyMeasurementLineageV2(t.Context(), previousBytes, previousOptions, changed, options); err == nil {
				t.Fatal("mismatched or regressing lineage accepted")
			}
		})
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.verifyMeasurementLineageV2(cancelled, previousBytes, previousOptions, current.artifact, currentOptions); !errors.Is(err, context.Canceled) {
		t.Fatalf("lineage ignored cancellation: %v", err)
	}
	afterCurrent, err := canonicalReleaseMeasurementBytes(current.artifact)
	if err != nil || !bytes.Equal(beforeCurrent, afterCurrent) {
		t.Fatalf("lineage changed current signed cuts: %v", err)
	}
	afterPrevious, err := canonicalReleaseMeasurementBytes(previous.artifact)
	if err != nil || !bytes.Equal(previousBytes, afterPrevious) {
		t.Fatalf("lineage changed predecessor: %v", err)
	}
	for epoch, closure := range history.terminals {
		raw, err := marshalAttemptSettlementV2JSON(t.Context(), closure, cfg.EvidenceV2.Bounds.MaxClosureBytes, false, true)
		if err != nil || !bytes.Equal(beforeTerminals[epoch], raw) {
			t.Fatalf("lineage rewrote terminal%d: %v", epoch, err)
		}
	}
}

func TestIntentV2ProvisionalGapRequiresTerminalPriorAndMonotonicEpoch(t *testing.T) {
	previous := &SteeringIntent{SubnetEpoch: 1401, SettlementEpoch: 302, Status: "applied"}
	current := &SteeringIntent{SubnetEpoch: 1404, SettlementEpoch: 306}
	if err := validateSteeringIntentSuccessorWithGapsV2(previous, current, false); err == nil {
		t.Fatal("strict intent successor accepted the observed gap")
	}
	if err := validateSteeringIntentSuccessorWithGapsV2(previous, current, true); err != nil {
		t.Fatalf("provisional actual forward gap rejected: %v", err)
	}
	for _, status := range []string{"pending", "finalized"} {
		prior := *previous
		prior.Status = status
		if err := validateSteeringIntentSuccessorWithGapsV2(&prior, current, true); err == nil {
			t.Fatalf("unfinished prior status%s accepted", status)
		}
	}
	for _, next := range []SteeringIntent{{SubnetEpoch: 1400, SettlementEpoch: 306}, {SubnetEpoch: 1404, SettlementEpoch: 301}} {
		if err := validateSteeringIntentSuccessorWithGapsV2(previous, &next, true); err == nil {
			t.Fatal("provisional successor accepted epoch regression")
		}
	}
}
