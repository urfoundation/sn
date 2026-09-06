//go:build linux || darwin

package validator

// Cross-window artifacts reuse genuine signed M8 streams and the real terminal
// sealer. Empty successor streams start at the same durable ledger head; this
// suite proves public wire/lineage, not the runtime's separate atomic promotion.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Each invocation creates fresh public-replay namespaces for all three roles:
// previous native measurement, terminal settlement and successor measurement.
type releaseMeasurementV2SettlementTestFixture struct {
	previous   *releaseMeasurementV2TestFixture
	current    *releaseMeasurementV2TestFixture
	terminalTs []*attemptCutV2StatsTestFixture
}

// The previous measurement's legacy oracle establishes its real raw scores.
// The next head fold uses the real persisted EMA store; pool priors come only
// from the complete signed terminal fold, including valid zero/idle quality.
func newReleaseMeasurementV2SettlementTestFixture(t *testing.T, completed int, sameNativeEpoch bool, generations uint64) *releaseMeasurementV2SettlementTestFixture {
	t.Helper()
	previous := newReleaseMeasurementV2TestFixture(t, completed)
	var terminalTs []*attemptCutV2StatsTestFixture
	for _, input := range previous.artifact.Inputs {
		operator := previous.operators[input.NoID]
		terminalTs = append(terminalTs, &attemptCutV2StatsTestFixture{
			seal: operator.seal, cut: *input.AttemptCutV2,
			measurement: cloneAttemptCutV2StatsTestMeasurement(t, input.Stats),
			metadata:    operator.metadata, data: operator.data,
		})
	}
	return newReleaseMeasurementV2SettlementTestFromTerminal(t, previous, terminalTs, sameNativeEpoch, generations)
}

// A later real terminal may observe additional providers. Their independently
// pinned binding observations use the same resolver as the actual trail engine.
// The head fold consumes its real eligibility census, with no successor work.
func newReleaseMeasurementV2SettlementTestFromTerminal(t *testing.T, previous *releaseMeasurementV2TestFixture, terminalTs []*attemptCutV2StatsTestFixture, sameNativeEpoch bool, generations uint64) *releaseMeasurementV2SettlementTestFixture {
	t.Helper()
	fixture := &releaseMeasurementV2SettlementTestFixture{previous: previous, terminalTs: terminalTs}
	closure := sealAttemptSettlementV2Test(t, fixture.terminalTs...)
	current := cloneReleaseMeasurementArtifact(t, previous.artifact)
	current.SettlementEpoch++
	if !sameNativeEpoch {
		current.SubnetEpoch++
	}
	current.NativeSnapshotBlock++
	current.NativeSnapshotHash = releaseHex32([32]byte{0x52})
	current.SettlementClosureV2 = closure
	previousBytes, err := canonicalReleaseMeasurementBytes(previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	current.PreviousArtifactHash = ReleaseMeasurementContentHash(previousBytes)
	fixture.current = &releaseMeasurementV2TestFixture{artifact: current, operators: map[uint64]*releaseMeasurementV2TestOperator{}}
	for index, transition := range closure.Transitions {
		terminal := fixture.terminalTs[index]
		next := attemptSettlementV2TestEmptySuccessor(t, terminal, transition, generations)
		priors := map[string]uint32{}
		for _, quality := range transition.PostFold {
			priors[quality.ClientID] = quality.QualityPPM
		}
		// Keep all provider observations, including providers with no quality.
		// Empty assignments do not authorize dropping their binding census.
		next.measurement.Providers = nil
		for _, provider := range terminal.measurement.Providers {
			prior, exists := priors[provider.ClientID]
			next.measurement.Providers = append(next.measurement.Providers, ReleaseProviderMeasurement{
				ClientID: provider.ClientID, HasPriorQuality: exists, PriorQualityPPM: prior,
				LatencyBuckets: make([]uint64, statsLatencyBuckets),
			})
		}
		input := &current.Inputs[index]
		input.Stats, input.AttemptCutV2 = next.measurement, &next.cut
		input.SettlementEpoch, input.EgressGeneration = current.SettlementEpoch, next.cut.Context.EgressGeneration
		input.CutNativeBlock, input.CutNativeBlockHash = current.NativeSnapshotBlock, current.NativeSnapshotHash
		input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash = next.cut.Context.Boundary.EVMBlock, next.cut.Context.Boundary.EVMBlockHash
		current.EVMSnapshotBlock, current.EVMSnapshotHash = input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash
		seal := *terminal.seal
		seal.expected = next.cut.Context
		fixture.current.operators[input.NoID] = &releaseMeasurementV2TestOperator{seal: &seal, metadata: next.metadata, data: next.data}
	}
	for index := range current.DepositAudits {
		audit := &current.DepositAudits[index]
		audit.Epoch, audit.SourceEpoch = current.SettlementEpoch, current.SettlementEpoch-current.Policy.Deposit.UsageLagEpochs
		audit.ObservedAtBlock = current.EVMSnapshotBlock
	}
	fixture.rebuildHead(t, sameNativeEpoch)
	return fixture
}

// Reconstruct raw head scores from genuine signed records through the existing
// legacy claim oracle, not the v2 verifier's result. New providers use the same
// binding resolver as the real fixture engines and retain the complete census.
func (self *releaseMeasurementV2SettlementTestFixture) rebuildHead(t *testing.T, sameNativeEpoch bool) {
	t.Helper()
	current, previous := self.current.artifact, self.previous.artifact
	providerKVs := map[uint64]map[connect.Id]bool{}
	for _, input := range current.Inputs {
		observedKVs := map[string]bool{}
		for _, binding := range current.Bindings {
			if binding.NoID == input.NoID {
				observedKVs[binding.ClientID] = true
			}
		}
		providerKVs[input.NoID] = map[connect.Id]bool{}
		for _, provider := range input.Stats.Providers {
			clientID, err := connect.ParseId(provider.ClientID)
			if err != nil {
				t.Fatal(err)
			}
			providerKVs[input.NoID][clientID] = true
			if observedKVs[provider.ClientID] {
				continue
			}
			binding := attemptLedgerTestBinding(clientID, 1)
			current.Bindings = append(current.Bindings, ReleaseBindingMeasurement{
				NoID: input.NoID, ClientID: provider.ClientID, Active: true,
				FleetID: binding.FleetID, Hotkey: binding.Hotkey, Generation: binding.Generation,
				ClientKey: releaseHex32([32]byte{0x31}), LocalClientKey: releaseHex32([32]byte{0x31}),
				CommitmentHash: releaseHex32([32]byte{0x32}), ValidFromEpoch: 42, ValidToEpoch: 43,
				RecordUID: binding.UID, LiveUIDFound: true, LiveUID: binding.UID,
			})
		}
	}
	sort.Slice(current.Bindings, func(i, j int) bool {
		if current.Bindings[i].NoID != current.Bindings[j].NoID {
			return current.Bindings[i].NoID < current.Bindings[j].NoID
		}
		return current.Bindings[i].ClientID < current.Bindings[j].ClientID
	})
	fleets, bound, _, _, activeKVs, err := releaseMeasurementBindingObservations(current, providerKVs)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range current.Inputs {
		currentKVs := map[connect.Id]FleetScoreKey{}
		for clientID := range bound[input.NoID] {
			currentKVs[clientID] = activeKVs[fmt.Sprintf("%020d:%s", input.NoID, clientID.String())]
		}
		operator := self.current.operators[input.NoID]
		prefixKVs := attemptCutV2HeadTestExpected(t, &attemptCutV2StatsTestFixture{seal: operator.seal}, currentKVs, input.AttemptCutV2.Context.EgressFirstSequence)
		for key, hashes := range prefixKVs {
			for hash := range hashes {
				fleets[key][hash] = true
			}
		}
	}
	head, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sameNativeEpoch {
		for _, record := range previous.HeadEMA {
			if record.HasPrior {
				t.Fatal("same-native fixture requires the actual empty pre-epoch EMA base")
			}
		}
	} else {
		if err := head.CommitForEpoch(previous.SubnetEpoch, previous.HeadEMA, current.Policy.Steering.HeadScoreEMA); err != nil {
			t.Fatal(err)
		}
	}
	raw := releaseRawHeadScores(fleets)
	_, current.HeadEMA, err = head.PreviewForEpoch(current.SubnetEpoch, raw, current.Policy.Steering.HeadScoreEMA)
	if err != nil {
		t.Fatal(err)
	}
}

// Append a third genuine complete M8 trail after the already sealed 18-record
// measurement. The alternate branch re-signs two complete trails in reverse
// order in a separate real ledger; neither its proofs nor its raw stats change.
func newReleaseMeasurementV2SettlementExtensionTestFixture(t *testing.T, rewritten bool) *releaseMeasurementV2SettlementTestFixture {
	t.Helper()
	previous := newReleaseMeasurementV2TestFixture(t, 2)
	var terminalTs []*attemptCutV2StatsTestFixture
	for _, input := range previous.artifact.Inputs {
		original := previous.operators[input.NoID].seal
		proof, err := original.engine.RunTrail(t.Context())
		if err != nil || proof == nil || proof.M != 8 || len(proof.Hops) != 8 {
			t.Fatalf("actual terminal extension did not complete M8: %v", err)
		}
		head, err := original.ledger.Head()
		if err != nil || head.LastSequence != 26 || input.AttemptCutV2.LastSequence != 18 {
			t.Fatalf("strict terminal extension changed the real 18/26 census: %+v error=%v", head, err)
		}
		var recordTs []AttemptRecord
		if err := original.ledger.Walk(t.Context(), 1, head.LastSequence, func(record AttemptRecord) error {
			if err := VerifyAttemptRecord(&record, original.expected.Identity, original.key.Public().(ed25519.PublicKey), original.server.serverPublicKeys()); err != nil {
				return err
			}
			recordTs = append(recordTs, record)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(recordTs) != 26 || recordTs[7].Disposition != AttemptDispositionComplete || recordTs[15].Disposition != AttemptDispositionComplete || recordTs[17].Disposition == AttemptDispositionPending || recordTs[17].Disposition == AttemptDispositionComplete || recordTs[25].Disposition != AttemptDispositionComplete || recordTs[17].RecordHash != input.AttemptCutV2.Root {
			t.Fatal("genuine strict extension lost the interior prior terminal checkpoint")
		}
		measurement := func() ReleaseStatsMeasurement {
			original.engine.stats.mu.Lock()
			defer original.engine.stats.mu.Unlock()
			return original.engine.stats.releaseStatsMeasurementWithLock()
		}()
		seal := *original
		seal.recordTs = recordTs
		if rewritten && input.NoID == 9 {
			ledger, err := NewDiskAttemptLedger(t.Context(), newAttemptLedgerDiskTestStateDir(t), original.expected.Identity, attemptLedgerDiskTestCoordinator, original.key, attemptLedgerDiskTestLimits())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ledger.Close() })
			reordered := append(append(append([]AttemptRecord{}, recordTs[8:16]...), recordTs[:8]...), recordTs[16:]...)
			seal.ledger, seal.recordTs = ledger, nil
			for _, record := range reordered {
				appended, err := ledger.AppendContext(t.Context(), record)
				if err != nil {
					t.Fatal(err)
				}
				if err := VerifyAttemptRecord(appended, original.expected.Identity, original.key.Public().(ed25519.PublicKey), original.server.serverPublicKeys()); err != nil {
					t.Fatalf("alternate terminal ordering is not genuinely signed: %v", err)
				}
				seal.recordTs = append(seal.recordTs, *appended)
			}
			if seal.recordTs[17].RecordHash == input.AttemptCutV2.Root {
				t.Fatal("genuine reordered terminal did not change the interior checkpoint")
			}
		}
		options, _ := newAttemptCutV2SealTestOptions(t, &seal)
		cut, _, err := SealAttemptCutV2(t.Context(), seal.ledger, seal.expected, seal.policy, seal.key, seal.bounds, options)
		if err != nil || cut == nil || cut.RecordCount != 26 || cut.Root != seal.recordTs[25].RecordHash || cut.Root == input.AttemptCutV2.Root {
			t.Fatalf("actual strict terminal seal: %v", err)
		}
		terminalTs = append(terminalTs, &attemptCutV2StatsTestFixture{seal: &seal, cut: *cut, measurement: measurement, metadata: options.ReadMetadata, data: options.OpenData})
	}
	return newReleaseMeasurementV2SettlementTestFromTerminal(t, previous, terminalTs, false, 1)
}

// Bootstrap the fixture's restart cursors from its already signed empty
// successor, then attach a fresh real Stats engine and append genuine M8 work
// at that boundary. This is not the runtime's separate atomic promotion/WAL.
func newReleaseMeasurementV2SettlementNonemptyTestFixture(t *testing.T) *releaseMeasurementV2SettlementTestFixture {
	t.Helper()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	for index := range fixture.current.artifact.Inputs {
		input := &fixture.current.artifact.Inputs[index]
		terminal := fixture.terminalTs[index]
		expected := input.AttemptCutV2.Context
		if input.AttemptCutV2.RecordCount != 0 || expected.FirstSequence != terminal.cut.LastSequence+1 || expected.PriorRoot != terminal.cut.Root || len(fixture.current.artifact.SettlementClosureV2.Transitions[index].PostFold) != 0 {
			t.Fatal("real successor bootstrap requires the signed empty cursor and genuinely absent sparse priors")
		}
		state := newAttemptLedgerDiskTestStateDir(t)
		stats := NewStatsEngine(StatsConfig{AMin: terminal.seal.policy.Verify.ReliabilityAMin})
		if err := stats.AdvanceSettlementEpoch(expected.Boundary.SettlementEpoch, state); err != nil {
			t.Fatal(err)
		}
		// Only authenticated restart cursors are initialized here. All new
		// provider counters and egress hashes come from the actual trail engine.
		stats.attemptSettlementFirstSequence = expected.FirstSequence
		stats.attemptEgressFirstSequence = expected.EgressFirstSequence
		stats.attemptLastAppliedSequence = expected.FirstSequence - 1
		stats.egressGeneration = expected.EgressGeneration
		if err := stats.AttachAttemptLedgerContext(t.Context(), terminal.seal.ledger, state); err != nil {
			t.Fatal(err)
		}
		store, err := NewProofStore(state)
		if err != nil {
			t.Fatal(err)
		}
		cfg := terminal.seal.engine.cfg
		cfg.AttemptLedger = terminal.seal.ledger
		cfg.AttemptBoundaryResolver = func(_ context.Context, pinned *AttemptBoundary, clients []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if pinned != nil && *pinned != expected.Boundary {
				return AttemptBoundary{}, nil, errors.New("nonempty successor boundary pin changed")
			}
			bindings := make([]AttemptBinding, len(clients))
			for index, clientID := range clients {
				bindings[index] = attemptLedgerTestBinding(clientID, 1)
			}
			return expected.Boundary, bindings, nil
		}
		engine := NewTrailEngine(terminal.seal.engine.clientId, terminal.seal.key, terminal.seal.server, terminal.seal.engine.keys, terminal.seal.engine.pickSeed, stats, store, func() uint64 { return expected.Boundary.SettlementEpoch }, cfg)
		proof, err := engine.RunTrail(t.Context())
		if err != nil || proof == nil || proof.M != 8 || len(proof.Hops) != 8 {
			t.Fatalf("real nonempty successor M8 failed: %v", err)
		}
		measurement := func() ReleaseStatsMeasurement {
			stats.mu.Lock()
			defer stats.mu.Unlock()
			return stats.releaseStatsMeasurementWithLock()
		}()
		providerKVs := map[string]bool{}
		for _, provider := range measurement.Providers {
			providerKVs[provider.ClientID] = true
		}
		for _, provider := range input.Stats.Providers {
			if provider.HasPriorQuality || provider.PriorQualityPPM != 0 {
				t.Fatal("sparse successor bootstrap invented a quality prior")
			}
			if !providerKVs[provider.ClientID] {
				measurement.Providers = append(measurement.Providers, ReleaseProviderMeasurement{ClientID: provider.ClientID, LatencyBuckets: make([]uint64, statsLatencyBuckets)})
			}
		}
		sort.Slice(measurement.Providers, func(i, j int) bool { return measurement.Providers[i].ClientID < measurement.Providers[j].ClientID })
		head, err := terminal.seal.ledger.Head()
		if err != nil || head.LastSequence != 26 {
			t.Fatalf("real successor durable census changed: %+v error=%v", head, err)
		}
		seal := *terminal.seal
		seal.engine, seal.expected, seal.recordTs = engine, expected, nil
		if err := seal.ledger.Walk(t.Context(), expected.FirstSequence, head.LastSequence, func(record AttemptRecord) error {
			if err := VerifyAttemptRecord(&record, expected.Identity, seal.key.Public().(ed25519.PublicKey), seal.server.serverPublicKeys()); err != nil {
				return err
			}
			seal.recordTs = append(seal.recordTs, record)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		options, _ := newAttemptCutV2SealTestOptions(t, &seal)
		cut, _, err := SealAttemptCutV2(t.Context(), seal.ledger, expected, seal.policy, seal.key, seal.bounds, options)
		if err != nil || cut == nil || cut.RecordCount != 8 || cut.CompleteCount != 1 || cut.FailedCount != 0 || len(seal.recordTs) != 8 || cut.Root != seal.recordTs[7].RecordHash {
			t.Fatalf("real nonempty successor cut changed: %v", err)
		}
		input.Stats, input.AttemptCutV2 = measurement, cut
		fixture.current.operators[input.NoID] = &releaseMeasurementV2TestOperator{seal: &seal, metadata: options.ReadMetadata, data: options.OpenData}
	}
	fixture.rebuildHead(t, false)
	return fixture
}

// Independent terminal expected contexts are supplied separately from the
// successor options, even though the fixture owns both real source histories.
func (self *releaseMeasurementV2SettlementTestFixture) options(t *testing.T) ReleaseMeasurementV2Options {
	t.Helper()
	options := self.current.options(t)
	terminal := attemptSettlementV2TestOptions(t, self.terminalTs...)
	options.Settlement = &terminal
	return options
}

// Count actual callbacks while delegating all real bytes, signatures and EOF.
func observeReleaseMeasurementV2SettlementTest(options *ReleaseMeasurementV2Options) *int {
	reads := 0
	observe := func(replay *AttemptCutV2ReplayOptions) {
		metadata, data := replay.ReadMetadata, replay.OpenData
		replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			reads++
			return metadata(ctx, hash, size)
		}
		replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reads++
			return data(ctx, kind, hash, size)
		}
	}
	for noID, operator := range options.Operators {
		observe(&operator.Measurement.Replay)
		options.Operators[noID] = operator
	}
	if options.Settlement != nil {
		for noID, operator := range options.Settlement.Operators {
			observe(&operator.Measurement.Replay)
			options.Settlement.Operators[noID] = operator
		}
	}
	return &reads
}

// All122 genuine records,15 complete/1 failed trails and positive qualities
// survive as terminal evidence. The successor contributes an actually empty
// stream and retains exactly the independently recomputed post-fold priors.
func TestReleaseMeasurementV2SettlementCarriesRealQualityAndRestartEvidence(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 15, false, 1)
	encoded, _, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		decoded, result, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, fixture.options(t))
		if err != nil || !reflect.DeepEqual(decoded, fixture.current.artifact) || len(result.ReplayByNO) != 2 || len(result.SettlementReplayByNO) != 2 {
			t.Fatalf("complete terminal restart evidence: %v", err)
		}
		positive := 0
		for noID, terminal := range result.SettlementReplayByNO {
			if terminal.Records.ItemCount != 122 || terminal.CompleteCount != 15 || terminal.FailedCount != 1 || result.ReplayByNO[noID].Records.ItemCount != 0 {
				t.Fatalf("terminal/current real census differs: terminal=%+v current=%+v", terminal, result.ReplayByNO[noID])
			}
			for _, provider := range result.Decision.StatsByNO[noID].Providers {
				if provider.HasQuality && provider.QualityPPM > 0 {
					positive++
				}
			}
		}
		if positive < 2 {
			t.Fatal("actual above-minimum terminal qualities disappeared on restart")
		}
	}
}

// A settlement may turn within the same native retry epoch. Both retry and
// next-native-epoch cases retain exact head EMA semantics and the real prefix.
func TestReleaseMeasurementV2SettlementLineageAcceptsRealPrefixAndNativeRetry(t *testing.T) {
	t.Parallel()
	for _, variation := range []struct {
		sameNative bool
		completed  int
		records    uint64
	}{
		{sameNative: false, completed: 2, records: 18},
		{sameNative: true, completed: 15, records: 122},
	} {
		fixture := newReleaseMeasurementV2SettlementTestFixture(t, variation.completed, variation.sameNative, 3)
		previous, err := canonicalReleaseMeasurementBytes(fixture.previous.artifact)
		if err != nil {
			t.Fatal(err)
		}
		result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, fixture.previous.options(t), fixture.current.artifact, fixture.options(t))
		if err != nil || len(result.SettlementReplayByNO) != 2 || len(result.ReplayByNO) != 2 {
			t.Fatalf("real cross-settlement lineage, same-native=%t: %v", variation.sameNative, err)
		}
		positive := 0
		for noID, terminal := range result.SettlementReplayByNO {
			if terminal.Records.ItemCount != variation.records || terminal.CompleteCount != uint64(variation.completed) || terminal.FailedCount != 1 || result.ReplayByNO[noID].Records.ItemCount != 0 {
				t.Fatalf("real retry census, same-native=%t: terminal=%+v current=%+v", variation.sameNative, terminal, result.ReplayByNO[noID])
			}
			for _, provider := range result.Decision.StatsByNO[noID].Providers {
				if provider.HasQuality && provider.QualityPPM > 0 {
					positive++
				}
			}
		}
		if variation.sameNative && positive < 2 {
			t.Fatal("same-native empty head requires the genuine retained positive pools")
		}
		if !variation.sameNative {
			for _, terminal := range fixture.terminalTs {
				for _, provider := range terminal.measurement.Providers {
					if provider.Assignments > 3 || provider.Assignments >= terminal.measurement.Config.AMin {
						t.Fatal("sparse next-native fixture changed its below-minimum exposure")
					}
				}
			}
			if positive != 0 {
				t.Fatal("sparse next-native fixture invented terminal quality")
			}
		}
	}
}

// A complete standalone candidate can fold a positive head score correctly
// yet use the wrong base for a same-native retry. The lineage join rejects the
// extra fold before replaying its otherwise valid terminal/current evidence.
func TestReleaseMeasurementV2SettlementNativeRetryRejectsDoubleFoldedHead(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 15, false, 1)
	fixture.current.artifact.SubnetEpoch = fixture.previous.artifact.SubnetEpoch
	priorCount := 0
	for _, record := range fixture.current.artifact.HeadEMA {
		if record.HasPrior {
			priorCount++
		}
	}
	if priorCount == 0 {
		t.Fatal("double-fold candidate lacks genuine positive prior head scores")
	}
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, fixture.options(t)); err != nil {
		t.Fatalf("double-fold candidate must remain independently valid: %v", err)
	}
	previous, err := canonicalReleaseMeasurementBytes(fixture.previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	previousOptions, currentOptions := fixture.previous.options(t), fixture.options(t)
	previousReads, currentReads := observeReleaseMeasurementV2SettlementTest(&previousOptions), observeReleaseMeasurementV2SettlementTest(&currentOptions)
	result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, previousOptions, fixture.current.artifact, currentOptions)
	if err == nil || !strings.Contains(err.Error(), "head EMA prior state changed") || *previousReads == 0 || *currentReads != 0 {
		t.Fatalf("same-native double fold bypassed the real head lineage join: previous=%d current=%d error=%v", *previousReads, *currentReads, err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// The authenticated previous root occurs at record18, strictly inside each
// complete 26-record terminal. Comparing only terminal end roots cannot pass.
func TestReleaseMeasurementV2SettlementLineageAcceptsStrictRealExtension(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementExtensionTestFixture(t, false)
	previous, err := canonicalReleaseMeasurementBytes(fixture.previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.previous.artifact, fixture.previous.options(t)); err != nil {
		t.Fatalf("strict extension's previous artifact is not independently valid: %v", err)
	}
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, fixture.options(t)); err != nil {
		t.Fatalf("strict terminal extension is not independently valid: %v", err)
	}
	result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, fixture.previous.options(t), fixture.current.artifact, fixture.options(t))
	if err != nil || len(result.SettlementReplayByNO) != 2 || len(result.ReplayByNO) != 2 {
		t.Fatalf("actual interior terminal checkpoint was refused: %v", err)
	}
	for noID, terminal := range result.SettlementReplayByNO {
		if terminal.Records.ItemCount != 26 || terminal.CompleteCount != 3 || terminal.FailedCount != 1 || result.ReplayByNO[noID].Records.ItemCount != 0 {
			t.Fatalf("strict terminal extension lost genuine complete census: %+v", terminal)
		}
	}
}

// Both public artifacts fully verify in isolation, including the alternate
// terminal's signatures, proofs, statistics and real empty successor. Only
// observing its interior record18 against the prior root exposes the fork.
func TestReleaseMeasurementV2SettlementLineageRejectsGenuineInteriorFork(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementExtensionTestFixture(t, true)
	previous, err := canonicalReleaseMeasurementBytes(fixture.previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.previous.artifact, fixture.previous.options(t)); err != nil {
		t.Fatalf("fork's previous artifact is not independently valid: %v", err)
	}
	if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, fixture.options(t)); err != nil {
		t.Fatalf("genuine alternate terminal is not independently valid: %v", err)
	}
	previousOptions, currentOptions := fixture.previous.options(t), fixture.options(t)
	previousReads := observeReleaseMeasurementV2SettlementTest(&previousOptions)
	terminalReads := attemptSettlementV2TestObserveReads(currentOptions.Settlement, fixture.terminalTs[0])
	ordinaryOptions := currentOptions
	ordinaryOptions.Settlement = nil
	currentReads := observeReleaseMeasurementV2SettlementTest(&ordinaryOptions)
	result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, previousOptions, fixture.current.artifact, currentOptions)
	if err == nil || !strings.Contains(err.Error(), "compact terminal closure:") || !strings.Contains(err.Error(), "rewrites its authenticated prior prefix") || *previousReads == 0 || len(terminalReads) == 0 || *currentReads != 0 {
		t.Fatalf("genuine fork bypassed actual terminal checkpoint: previous=%d terminal=%v current=%d error=%v", *previousReads, terminalReads, *currentReads, err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// The empty current verification authenticates its header and raw state with
// zero fetches. Its nonempty companion below pins actual work for both roles;
// neither path invents an empty metadata/data object just to count a callback.
func TestReleaseMeasurementV2SettlementUsesOneReplayPerContainingCut(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	terminalOptions := fixture.options(t)
	terminalReads := observeReleaseMeasurementV2SettlementTest(&terminalOptions)
	if _, err := VerifyAttemptSettlementClosureV2(t.Context(), fixture.current.artifact.SettlementClosureV2, *terminalOptions.Settlement); err != nil {
		t.Fatal(err)
	}
	current := cloneReleaseMeasurementArtifact(t, fixture.current.artifact)
	current.SettlementClosureV2 = nil
	currentOptions := fixture.current.options(t)
	currentReads := observeReleaseMeasurementV2SettlementTest(&currentOptions)
	want, err := VerifyReleaseMeasurementArtifactV2(t.Context(), current, currentOptions)
	if err != nil {
		t.Fatal(err)
	}
	combinedOptions := fixture.options(t)
	combinedReads := observeReleaseMeasurementV2SettlementTest(&combinedOptions)
	got, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, combinedOptions)
	if err != nil || *terminalReads == 0 || *currentReads != 0 || *combinedReads != *terminalReads+*currentReads || !reflect.DeepEqual(got.Decision, want.Decision) {
		t.Fatalf("terminal/current replay count or exact math differs: terminal=%d current=%d combined=%d error=%v", *terminalReads, *currentReads, *combinedReads, err)
	}
}

// Nonempty successor records retain the terminal root while both actual
// streams replay once. Canonical restart reconstructs the same complete result.
func TestReleaseMeasurementV2SettlementCarriesRealNonemptySuccessors(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementNonemptyTestFixture(t)
	terminalOptions := fixture.options(t)
	terminalReads := observeReleaseMeasurementV2SettlementTest(&terminalOptions)
	if _, err := VerifyAttemptSettlementClosureV2(t.Context(), fixture.current.artifact.SettlementClosureV2, *terminalOptions.Settlement); err != nil {
		t.Fatal(err)
	}
	ordinary := cloneReleaseMeasurementArtifact(t, fixture.current.artifact)
	ordinary.SettlementClosureV2 = nil
	ordinaryOptions := fixture.current.options(t)
	currentReads := observeReleaseMeasurementV2SettlementTest(&ordinaryOptions)
	want, err := VerifyReleaseMeasurementArtifactV2(t.Context(), ordinary, ordinaryOptions)
	if err != nil {
		t.Fatal(err)
	}
	options := fixture.options(t)
	combinedReads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, options)
	if err != nil || *terminalReads == 0 || *currentReads == 0 || *combinedReads != *terminalReads+*currentReads || len(result.SettlementReplayByNO) != 2 || len(result.ReplayByNO) != 2 || !reflect.DeepEqual(result.Decision, want.Decision) {
		t.Fatalf("real nonempty containing streams changed replay work or exact math: terminal=%d current=%d combined=%d error=%v", *terminalReads, *currentReads, *combinedReads, err)
	}
	for noID, terminal := range result.SettlementReplayByNO {
		current := result.ReplayByNO[noID]
		if terminal.Records.ItemCount != 18 || terminal.CompleteCount != 2 || terminal.FailedCount != 1 || current.Records.ItemCount != 8 || current.CompleteCount != 1 || current.FailedCount != 0 {
			t.Fatalf("real nonempty terminal/current census differs: terminal=%+v current=%+v", terminal, current)
		}
	}
	encoded, contentHash, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, fixture.options(t))
	if err != nil || contentHash != ReleaseMeasurementContentHash(encoded) {
		t.Fatalf("real nonempty successor seal content address differs: hash=%s error=%v", contentHash, err)
	}
	decoded, restarted, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, fixture.options(t))
	if err != nil || !reflect.DeepEqual(decoded, fixture.current.artifact) || !reflect.DeepEqual(restarted, result) {
		t.Fatalf("real nonempty successor restart differs: %v", err)
	}
}

// Full authority/census/namespace admission happens before the first reader.
// A candidate's valid own signatures never supply missing independent pins.
func TestReleaseMeasurementV2SettlementAdmissionPrecedesAllIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	for _, edit := range []func(*ReleaseMeasurementArtifact, *ReleaseMeasurementV2Options){
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { o.Settlement = nil },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { a.SettlementClosureV2 = nil },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			delete(o.Settlement.Operators, 10)
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			a.SettlementClosureV2.Transitions = a.SettlementClosureV2.Transitions[:1]
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { a.SettlementClosureV2.Epoch-- },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			o.Settlement.MaxClosureBytes = o.MaxArtifactBytes + 1
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { o.MaxControlBytes = 1 },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Settlement.Operators[9]
			op.Measurement.Replay.ScratchDirectory = o.Operators[10].Measurement.Replay.ScratchDirectory
			o.Settlement.Operators[9] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[10]
			op.Measurement.Replay.ScratchDirectory = "/"
			o.Operators[10] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Settlement.Operators[10]
			op.Measurement.Replay.ScratchDirectory = "/"
			o.Settlement.Operators[10] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Settlement.Operators[10]
			op.Expected.Activation.Domain.ActivationHash[0] ^= 1
			o.Settlement.Operators[10] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			a.SettlementClosureV2.Transitions[0].PreFold.AttemptCut = &AttemptLedgerCut{}
		},
	} {
		artifact := cloneReleaseMeasurementArtifact(t, fixture.current.artifact)
		options := fixture.options(t)
		var scratchTs []string
		for _, operator := range options.Operators {
			scratchTs = append(scratchTs, operator.Measurement.Replay.ScratchDirectory)
		}
		for _, operator := range options.Settlement.Operators {
			scratchTs = append(scratchTs, operator.Measurement.Replay.ScratchDirectory)
		}
		edit(artifact, &options)
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, options)
		if err == nil || *reads != 0 {
			t.Fatalf("terminal admission read or accepted: reads=%d error=%v", *reads, err)
		}
		assertReleaseMeasurementV2Empty(t, result)
		for _, scratch := range scratchTs {
			if _, err := os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("complete terminal/current admission created replay scratch: %v", err)
			}
		}
	}
}

// A re-signed complete batch can lie consistently about post-fold and current
// prior state. Header/member admission succeeds; actual statistics replay must
// still refuse the invented quality, without publishing a half-result.
func TestReleaseMeasurementV2SettlementRejectsResignedInventedFold(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	artifact := cloneReleaseMeasurementArtifact(t, fixture.current.artifact)
	transition := artifact.SettlementClosureV2.Transitions[0]
	if len(transition.PostFold) != 0 {
		t.Fatal("small real fixture unexpectedly qualified a provider")
	}
	provider := &artifact.Inputs[0].Stats.Providers[0]
	transition.PostFold = []AttemptSettlementQuality{{ClientID: provider.ClientID, HasQuality: true, QualityPPM: 123456}}
	provider.HasPriorQuality, provider.PriorQualityPPM = true, 123456
	resignAttemptSettlementV2Test(t, artifact.SettlementClosureV2, fixture.terminalTs...)
	options := fixture.options(t)
	if _, err := admitAttemptSettlementV2(t.Context(), artifact.SettlementClosureV2, *options.Settlement, true); err != nil {
		t.Fatalf("valid member-signature prerequisite: %v", err)
	}
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, options)
	if err == nil || !strings.Contains(err.Error(), "post-fold EMA") || *reads == 0 {
		t.Fatalf("invented signed fold escaped real replay: reads=%d error=%v", *reads, err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// Even separately valid options cannot reuse one physical scratch name across
// previous/terminal/current roles in the complete lineage invocation.
func TestReleaseMeasurementV2SettlementLineageRejectsCrossRoleScratchBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	for _, carriedClosure := range []bool{false, true} {
		previousArtifact := fixture.previous.artifact
		if carriedClosure {
			previousArtifact = fixture.current.artifact
		}
		previousBytes, err := canonicalReleaseMeasurementBytes(previousArtifact)
		if err != nil {
			t.Fatal(err)
		}
		currentArtifact := cloneReleaseMeasurementArtifact(t, fixture.current.artifact)
		currentArtifact.PreviousArtifactHash = ReleaseMeasurementContentHash(previousBytes)
		for _, priorTerminal := range []bool{false, true} {
			if priorTerminal && !carriedClosure {
				continue
			}
			for _, nextTerminal := range []bool{false, true} {
				previous, current := fixture.previous.options(t), fixture.options(t)
				if carriedClosure {
					previous = fixture.options(t)
				}
				var scratchTs []string
				for _, options := range []ReleaseMeasurementV2Options{previous, current} {
					for _, operator := range options.Operators {
						scratchTs = append(scratchTs, operator.Measurement.Replay.ScratchDirectory)
					}
					if options.Settlement != nil {
						for _, operator := range options.Settlement.Operators {
							scratchTs = append(scratchTs, operator.Measurement.Replay.ScratchDirectory)
						}
					}
				}
				priorPath := previous.Operators[9].Measurement.Replay.ScratchDirectory
				if priorTerminal {
					priorPath = previous.Settlement.Operators[9].Measurement.Replay.ScratchDirectory
				}
				if nextTerminal {
					operator := current.Settlement.Operators[10]
					operator.Measurement.Replay.ScratchDirectory = priorPath
					current.Settlement.Operators[10] = operator
				} else {
					operator := current.Operators[10]
					operator.Measurement.Replay.ScratchDirectory = priorPath
					current.Operators[10] = operator
				}
				previousReads, currentReads := observeReleaseMeasurementV2SettlementTest(&previous), observeReleaseMeasurementV2SettlementTest(&current)
				result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previousBytes, previous, currentArtifact, current)
				if err == nil || !strings.Contains(err.Error(), "replay namespace") || *previousReads != 0 || *currentReads != 0 {
					t.Fatalf("cross-role alias reached replay: carried=%t prior-terminal=%t next-terminal=%t error=%v", carriedClosure, priorTerminal, nextTerminal, err)
				}
				assertReleaseMeasurementV2Empty(t, result)
				for _, scratch := range scratchTs {
					if _, err := os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("cross-role alias created replay scratch: %v", err)
					}
				}
			}
		}
	}
}

// Nested legacy authority and duplicate/noncanonical terminal fields have no
// decoding destination that could materialize a recursive legacy history.
func TestReleaseMeasurementV2SettlementDecodeRejectsCompetingTerminalWire(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	raw, err := canonicalReleaseMeasurementBytes(fixture.current.artifact)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range [][]byte{
		bytes.Replace(raw, []byte(`"pre_fold":{`), []byte(`"pre_fold":{"attempt_cut":{},`), 1),
		bytes.Replace(raw, []byte(`"pre_fold":{`), []byte(`"pre_fold":{"settlement_transition":{},`), 1),
		bytes.Replace(raw, []byte(`"epoch":42`), []byte(`"epoch":42,"epoch":42`), 1),
		append(bytes.Clone(raw), []byte("{}\n")...), raw[:len(raw)-1],
	} {
		if bytes.Equal(candidate, raw) {
			t.Fatal("terminal wire mutation did not change actual bytes")
		}
		options := fixture.options(t)
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		artifact, result, err := DecodeReleaseMeasurementArtifactV2(t.Context(), candidate, options)
		if err == nil || artifact != nil || *reads != 0 {
			t.Fatalf("competing terminal decoded or read: %v", err)
		}
		assertReleaseMeasurementV2Empty(t, result)
	}
}

// A terminal reader can synchronously mutate both later candidate roles and
// their caller-owned keys. The sealed bytes must describe the admitted copy.
func TestReleaseMeasurementV2SettlementOwnsTerminalAndSuccessorBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	artifact := cloneReleaseMeasurementArtifact(t, fixture.current.artifact)
	want, err := canonicalReleaseMeasurementBytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	options := fixture.options(t)
	op := options.Settlement.Operators[9]
	metadata := op.Measurement.Replay.ReadMetadata
	changed := false
	op.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !changed {
			changed = true
			artifact.SettlementClosureV2.Transitions[1].Signature[0] ^= 1
			artifact.Inputs[1].Stats.Providers[0].Assignments++
			for _, key := range options.Operators[10].Measurement.Replay.ServerKeys {
				key[0] ^= 1
			}
			other := options.Settlement.Operators[10]
			other.Expected.Activation.Domain.ActivationHash[0] ^= 1
			options.Settlement.Operators[10] = other
		}
		return metadata(ctx, hash, size)
	}
	options.Settlement.Operators[9] = op
	raw, _, err := SealReleaseMeasurementArtifactV2(t.Context(), artifact, options)
	if err != nil || !changed || !bytes.Equal(raw, want) {
		t.Fatalf("terminal callback altered successor/closure bytes: changed=%t error=%v", changed, err)
	}
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, fixture.options(t))
	if err == nil {
		t.Fatal("a second operation reused the earlier owned verdict")
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// The real proof owner closes before a deterministic cancellation becomes
// visible. Neither a terminal result nor current scores can escape that exit.
func TestReleaseMeasurementV2SettlementCancellationAfterProofCloseClearsAll(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options := fixture.options(t)
	op := options.Settlement.Operators[10]
	data := op.Measurement.Replay.OpenData
	closes := 0
	op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &attemptCutV2MeasurementCancelClose{ReadCloser: reader, cancel: cancel, closes: &closes}, nil
	}
	options.Settlement.Operators[10] = op
	result, err := VerifyReleaseMeasurementArtifactV2(ctx, fixture.current.artifact, options)
	if !errors.Is(err, context.Canceled) || closes == 0 {
		t.Fatalf("terminal close cancellation disappeared: closes=%d error=%v", closes, err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// Terminal success cannot publish a partial result when the successor's last
// operator subsequently fails a real metadata read callback.
func TestReleaseMeasurementV2SettlementLateCurrentFailureDiscardsTerminalCensus(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementNonemptyTestFixture(t)
	options := fixture.options(t)
	terminalReads := 0
	terminalProofCloses := map[uint64]uint64{}
	for noID, op := range options.Settlement.Operators {
		metadata := op.Measurement.Replay.ReadMetadata
		data := op.Measurement.Replay.OpenData
		op.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			terminalReads++
			return metadata(ctx, hash, size)
		}
		op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reader, err := data(ctx, kind, hash, size)
			if err != nil || kind != AttemptStreamV2Proofs {
				return reader, err
			}
			return &attemptCutV2SealTestClosingReader{ReadCloser: reader, afterClose: func() error { terminalProofCloses[noID]++; return nil }}, nil
		}
		options.Settlement.Operators[noID] = op
	}
	failure := errors.New("test-owned current metadata failure after terminal replay")
	op := options.Operators[10]
	metadata := op.Measurement.Replay.ReadMetadata
	currentReads := 0
	terminalCompleteAtFailure := false
	op.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		currentReads++
		terminalCompleteAtFailure = true
		for _, transition := range fixture.current.artifact.SettlementClosureV2.Transitions {
			if transition.Cut.Proofs.ChunkCount == 0 || terminalProofCloses[transition.Identity.NoID] != transition.Cut.Proofs.ChunkCount {
				terminalCompleteAtFailure = false
			}
		}
		raw, err := metadata(ctx, hash, size)
		return raw, errors.Join(err, failure)
	}
	options.Operators[10] = op
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, options)
	if !errors.Is(err, failure) || terminalReads == 0 || currentReads == 0 || !terminalCompleteAtFailure {
		t.Fatalf("late current failure lost complete terminal/current traversal: closes=%v error=%v", terminalProofCloses, err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// The complete retained pool-quality census cannot be reset, omitted or
// invented at the new window even when all native cut signatures are genuine.
func TestReleaseMeasurementV2SettlementRejectsPriorQualityDriftBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 15, false, 1)
	sparse := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	for _, terminal := range sparse.terminalTs {
		for _, provider := range terminal.measurement.Providers {
			if provider.Assignments > 3 || provider.Assignments >= terminal.measurement.Config.AMin {
				t.Fatal("two complete trails and one failed exposure must remain below the unchanged reliability minimum")
			}
		}
	}
	for _, mode := range []string{"changed", "omitted", "invented"} {
		source := fixture
		if mode == "invented" {
			source = sparse
		}
		artifact := cloneReleaseMeasurementArtifact(t, source.current.artifact)
		providerIndex := -1
		for index, provider := range artifact.Inputs[0].Stats.Providers {
			if provider.HasPriorQuality == (mode != "invented") {
				providerIndex = index
				break
			}
		}
		if providerIndex < 0 {
			t.Fatalf("real prior-quality variation prerequisite absent: %s", mode)
		}
		provider := &artifact.Inputs[0].Stats.Providers[providerIndex]
		switch mode {
		case "changed":
			provider.PriorQualityPPM ^= 1
		case "omitted":
			provider.HasPriorQuality, provider.PriorQualityPPM = false, 0
		case "invented":
			provider.HasPriorQuality, provider.PriorQualityPPM = true, 12345
		}
		options := source.options(t)
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, options)
		if err == nil || !strings.Contains(err.Error(), "prior EMA") || *reads != 0 {
			t.Fatalf("%s prior state escaped pre-I/O join: %v", mode, err)
		}
		assertReleaseMeasurementV2Empty(t, result)
	}
}

// Same-settlement lineage still requires the exact prior pool state when a
// previously accepted closure is carried again into a later native window.
func TestReleaseMeasurementV2SettlementSameWindowRetryRetainsClosure(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2SettlementTestFixture(t, 2, false, 1)
	previous, err := canonicalReleaseMeasurementBytes(fixture.current.artifact)
	if err != nil {
		t.Fatal(err)
	}
	current := cloneReleaseMeasurementArtifact(t, fixture.current.artifact)
	current.PreviousArtifactHash = ReleaseMeasurementContentHash(previous)
	options := fixture.options(t)
	options.Expected = releaseMeasurementV2Decision(current)
	result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, fixture.options(t), current, options)
	if err != nil || !slices.Equal(current.HeadEMA, fixture.current.artifact.HeadEMA) || len(result.SettlementReplayByNO) != 2 {
		t.Fatalf("same-window complete-closure retry: %v", err)
	}
}
