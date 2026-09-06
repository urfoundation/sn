//go:build linux || darwin

package validator

// These artifacts contain two independent real M8 engines and disk ledgers.
// The legacy oracle appends the very same signed records into real v1 stores;
// neither path invents cuts, signatures, scoring totals or verifier verdicts.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

type releaseMeasurementV2TestOperator struct {
	seal     *attemptCutV2SealTestFixture
	legacy   *AttemptLedgerCut
	metadata AttemptStreamV2MetadataReader
	data     AttemptStreamV2DataOpener
}

type releaseMeasurementV2TestFixture struct {
	artifact  *ReleaseMeasurementArtifact
	legacy    *ReleaseMeasurementArtifact
	want      *VerifiedReleaseMeasurement
	operators map[uint64]*releaseMeasurementV2TestOperator
}

// No fixture raises the original 128-record/16-trail durable bounds. The
// positive-quality case uses exactly 15 complete + 1 failed M8 trail per NO.
func newReleaseMeasurementV2TestFixture(t *testing.T, completed int) *releaseMeasurementV2TestFixture {
	t.Helper()
	fixture := &releaseMeasurementV2TestFixture{operators: map[uint64]*releaseMeasurementV2TestOperator{}}
	artifact := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchemaV2, SubnetEpoch: 7,
		NativeSnapshotBlock: 200, NativeSnapshotHash: releaseHex32([32]byte{0x51}),
		ControlledNOIDs: []uint64{}, Inputs: []ReleaseMeasurementInput{}, Bindings: []ReleaseBindingMeasurement{},
		HeadEMA: []HeadEMAMeasurement{}, Pools: []ReleasePoolMeasurement{}, DepositAudits: []DepositAudit{},
	}
	for _, noID := range []uint64{9, 10} {
		seal := newAttemptCutV2SealTestFixtureForOperator(t, 8, completed, 1, noID)
		// These caller-pinned, distinct earlier activation anchors are fixed
		// before actual sealing. This fixture proves full cut authentication,
		// not historical chain publication of the earlier activation records.
		seal.expected.Activation.Domain.ActivationHash = [32]byte{0x14, byte(noID)}
		if seal.policy.Verify.TrailDepth != 8 || seal.policy.Safety.MinimumHealthyNOCount != 2 {
			t.Fatal("real release policy M8/two-operator safety precondition changed")
		}
		identity, activation := seal.expected.Identity, seal.expected.Activation
		artifact.DeploymentID, artifact.ChainID, artifact.GenesisHash = identity.DeploymentID, identity.ChainID, identity.GenesisHash
		artifact.Coordinator, artifact.SettlementVault = strings.ToLower(common.Address(activation.Domain.Coordinator).Hex()), strings.ToLower(common.Address(activation.Domain.SettlementVault).Hex())
		artifact.ValidatorID, artifact.Netuid, artifact.SelfUID = identity.ValidatorID, identity.Netuid, identity.ValidatorUID
		artifact.Policy, artifact.PolicyHash = seal.policy, releaseHex32(activation.Domain.PolicyHash)
		artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash, artifact.SettlementEpoch = seal.expected.Boundary.EVMBlock, seal.expected.Boundary.EVMBlockHash, seal.expected.Boundary.SettlementEpoch
		seal.engine.stats.mu.Lock()
		measurement := seal.engine.stats.releaseStatsMeasurementWithLock()
		seal.engine.stats.mu.Unlock()
		ledger, err := NewAttemptLedger(newAttemptLedgerDiskTestStateDir(t), identity, seal.key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ledger.Close() })
		for _, record := range seal.recordTs {
			appended, err := ledger.AppendContext(t.Context(), record)
			if err != nil || !reflect.DeepEqual(appended, &record) {
				t.Fatalf("v1 oracle changed actual signed record: %v", err)
			}
		}
		legacyCut, err := ledger.BuildCut(seal.expected.Boundary, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		options, _ := newAttemptCutV2SealTestOptions(t, seal)
		cut, _, err := SealAttemptCutV2(t.Context(), seal.ledger, seal.expected, seal.policy, seal.key, seal.bounds, options)
		if err != nil || cut == nil {
			t.Fatalf("actual M8 compact seal: %v", err)
		}
		fixture.operators[noID] = &releaseMeasurementV2TestOperator{seal: seal, legacy: legacyCut, metadata: options.ReadMetadata, data: options.OpenData}
		artifact.Inputs = append(artifact.Inputs, ReleaseMeasurementInput{
			NoID: noID, SettlementEpoch: artifact.SettlementEpoch,
			CutNativeBlock: artifact.NativeSnapshotBlock, CutNativeBlockHash: artifact.NativeSnapshotHash,
			CutEVMSnapshotBlock: artifact.EVMSnapshotBlock, CutEVMSnapshotHash: artifact.EVMSnapshotHash,
			EgressGeneration: cut.Context.EgressGeneration, Stats: measurement, AttemptCutV2: cut,
		})
		unbound := measurement.Providers[0]
		for _, provider := range measurement.Providers {
			if provider.Assignments > unbound.Assignments {
				unbound = provider
			}
		}
		if completed == 15 && unbound.Assignments < seal.policy.Verify.ReliabilityAMin {
			t.Fatal("106 genuine M8 assignments among at most 15 providers lost the positive-quality pigeonhole bound")
		}
		for _, provider := range measurement.Providers {
			clientID, err := connect.ParseId(provider.ClientID)
			if err != nil {
				t.Fatal(err)
			}
			binding := attemptLedgerTestBinding(clientID, 1)
			artifact.Bindings = append(artifact.Bindings, ReleaseBindingMeasurement{
				NoID: noID, ClientID: provider.ClientID, Active: true,
				FleetID: binding.FleetID, Hotkey: binding.Hotkey, Generation: binding.Generation,
				ClientKey: releaseHex32([32]byte{0x31}), LocalClientKey: releaseHex32([32]byte{0x31}),
				CommitmentHash: releaseHex32([32]byte{0x32}), ValidFromEpoch: 42, ValidToEpoch: 43,
				RecordUID: binding.UID, LiveUIDFound: true, LiveUID: binding.UID,
			})
			// The independently observed current coordinator state may unbind a
			// provider whose historical signed work remains in the operator pool.
			if provider.ClientID == unbound.ClientID {
				observed := &artifact.Bindings[len(artifact.Bindings)-1]
				observed.Active, observed.LiveUIDFound, observed.LiveUID = false, false, 0
				observed.LocalClientKey = releaseHex32([32]byte{})
			}
		}
		artifact.Pools = append(artifact.Pools, ReleasePoolMeasurement{NoID: noID, UID: uint16(100 + noID), PoolHotkey: releaseHex32([32]byte{0x61, byte(noID)})})
		audit := releaseMeasurementDepositAudit(t, seal.policy, noID)
		audit.Epoch, audit.SourceEpoch, audit.ObservedAtBlock = 42, 42-seal.policy.Deposit.UsageLagEpochs, artifact.EVMSnapshotBlock
		artifact.DepositAudits = append(artifact.DepositAudits, audit)
	}
	fixture.artifact = artifact
	fixture.rebuildLegacy(t)
	return fixture
}

// The independent existing v1 claim path, not the v2 result, determines every
// raw head score and exact EMA transcript used as the compact test input.
func (self *releaseMeasurementV2TestFixture) rebuildLegacy(t *testing.T) {
	t.Helper()
	legacy := cloneReleaseMeasurementArtifact(t, self.artifact)
	legacy.Schema = ReleaseMeasurementSchema
	statsKVs := map[uint64]VerifiedReleaseStats{}
	for index := range legacy.Inputs {
		input := &legacy.Inputs[index]
		input.AttemptCutV2 = nil
		input.Stats.AttemptCut = self.operators[input.NoID].legacy
		stats, err := VerifyReleaseStatsMeasurement(input.Stats)
		if err != nil {
			t.Fatalf("genuine v1 statistics oracle: %v", err)
		}
		statsKVs[input.NoID] = stats
	}
	fleets, _, _, _, err := releaseMeasurementBindings(legacy, statsKVs)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, head, err := store.PreviewForEpoch(legacy.SubnetEpoch, releaseRawHeadScores(fleets), legacy.Policy.Steering.HeadScoreEMA)
	if err != nil {
		t.Fatal(err)
	}
	legacy.HeadEMA, self.artifact.HeadEMA = head, slices.Clone(head)
	want, err := VerifyReleaseMeasurementArtifact(legacy)
	if err != nil {
		t.Fatalf("complete genuine v1 measurement oracle: %v", err)
	}
	self.legacy, self.want = legacy, want
}

// Every invocation owns new real replay scratch and an independent authority
// snapshot. The tests change those observations explicitly when exercising an
// actual different authenticated coordinator/native decision.
func (self *releaseMeasurementV2TestFixture) options(t *testing.T) ReleaseMeasurementV2Options {
	t.Helper()
	artifact := self.artifact
	options := ReleaseMeasurementV2Options{
		Expected: releaseMeasurementV2Decision(artifact), Policy: artifact.Policy,
		ControlledNOIDs: slices.Clone(artifact.ControlledNOIDs), Bindings: slices.Clone(artifact.Bindings),
		Pools: slices.Clone(artifact.Pools), DepositAudits: slices.Clone(artifact.DepositAudits),
		Operators: map[uint64]ReleaseMeasurementV2OperatorOptions{}, MaxOperators: 2,
		MaxHeadEntries: 32, MaxArtifactBytes: 1024 * 1024, MaxControlBytes: 1024 * 1024,
	}
	for _, input := range artifact.Inputs {
		operator := self.operators[input.NoID]
		currentKVs := map[connect.Id]FleetScoreKey{}
		for _, binding := range artifact.Bindings {
			if binding.NoID != input.NoID || !binding.Active || !binding.LiveUIDFound || binding.RecordUID != binding.LiveUID {
				continue
			}
			clientID, _ := connect.ParseId(binding.ClientID)
			currentKVs[clientID] = attemptCutV2HeadTestFleetKey(t, AttemptBinding{FleetID: binding.FleetID, Hotkey: binding.Hotkey, Generation: binding.Generation, UID: binding.LiveUID})
		}
		var hashes uint64
		for _, provider := range input.Stats.Providers {
			hashes += uint64(len(provider.EgressIPHashHexes))
		}
		options.Operators[input.NoID] = ReleaseMeasurementV2OperatorOptions{
			Expected: operator.seal.expected, CutNativeBlock: input.CutNativeBlock, CutNativeBlockHash: input.CutNativeBlockHash,
			Bounds: operator.seal.bounds,
			Measurement: AttemptCutV2MeasurementOptions{
				ExpectedConfig: input.Stats.Config, CurrentBindingKVs: currentKVs,
				MaxProviders: max(1, uint64(len(input.Stats.Providers))), MaxEgressHashes: max(1, hashes), MaxFleetPrefixes: 64,
				Replay: AttemptCutV2ReplayOptions{Bounds: operator.seal.replay, ScratchDirectory: filepath.Join(t.TempDir(), "measurement-replay"), ServerKeys: operator.seal.server.serverPublicKeys(), ReadMetadata: operator.metadata, OpenData: operator.data},
			},
		}
	}
	return options
}

func assertReleaseMeasurementV2Empty(t *testing.T, result VerifiedReleaseMeasurementV2) {
	t.Helper()
	if result.Decision != nil || result.ReplayByNO != nil || result.SettlementReplayByNO != nil {
		t.Fatal("failed compact artifact published partial decision or replay census")
	}
}

// A malformed later owner's path cannot let the earlier complete M8 stream
// read or create scratch before the complete ordinary census is admitted.
func TestReleaseMeasurementV2AdmissionRejectsRootScratchBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	options := fixture.options(t)
	reads := 0
	var scratchTs []string
	for noID, operator := range options.Operators {
		scratchTs = append(scratchTs, operator.Measurement.Replay.ScratchDirectory)
		metadata, data := operator.Measurement.Replay.ReadMetadata, operator.Measurement.Replay.OpenData
		operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			reads++
			return metadata(ctx, hash, size)
		}
		operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reads++
			return data(ctx, kind, hash, size)
		}
		if noID == 10 {
			operator.Measurement.Replay.ScratchDirectory = "/"
		}
		options.Operators[noID] = operator
	}
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, options)
	if err == nil || !strings.Contains(err.Error(), "replay namespaces") || reads != 0 {
		t.Fatalf("root namespace passed complete ordinary admission: reads=%d error=%v", reads, err)
	}
	assertReleaseMeasurementV2Empty(t, result)
	for _, scratch := range scratchTs {
		if _, err := os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("root namespace created another ordinary scratch: %v", err)
		}
	}
}

// Shared math retains real positive quality, complete failed exposure, exact
// head/pool/masking decisions and the existing v1 rational vector.
func TestReleaseMeasurementV2MatchesRealM8WholeArtifact(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 15)
	before, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("real v1/v2 artifact parity differs: %v", err)
	}
	positive := 0
	for noID, replayed := range result.ReplayByNO {
		if replayed.Records.ItemCount != 122 || replayed.CompleteCount != 15 || replayed.FailedCount != 1 {
			t.Fatalf("operator %d real census differs: %+v", noID, replayed)
		}
		for _, provider := range result.Decision.StatsByNO[noID].Providers {
			if provider.HasQuality && provider.QualityPPM > 0 {
				positive++
			}
		}
	}
	if len(result.ReplayByNO) != 2 || positive < 2 {
		t.Fatal("complete M8/two-operator positive-quality precondition was lost")
	}
	after, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	if !bytes.Equal(before, after) {
		t.Fatal("compact verifier mutated its caller's artifact")
	}
}

// Exact callback census is independently measured through one standalone
// joint replay, then compared with the real public artifact workflow.
func TestReleaseMeasurementV2UsesOneJointReplayPerOperator(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	wantKVs, gotKVs := map[string]int{}, map[string]int{}
	observe := func(options ReleaseMeasurementV2Options, counts map[string]int) ReleaseMeasurementV2Options {
		for noID, operator := range options.Operators {
			transport := fixture.operators[noID]
			operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
				counts[fmtReleaseMeasurementV2Read(noID, "metadata", hash)]++
				return transport.metadata(ctx, hash, size)
			}
			operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
				counts[fmtReleaseMeasurementV2Read(noID, kind, hash)]++
				return transport.data(ctx, kind, hash, size)
			}
			options.Operators[noID] = operator
		}
		return options
	}
	standalone := observe(fixture.options(t), wantKVs)
	for _, input := range fixture.artifact.Inputs {
		operator := standalone.Operators[input.NoID]
		if _, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), input.Stats, *input.AttemptCutV2, operator.Expected, fixture.artifact.Policy, operator.Bounds, operator.Measurement); err != nil {
			t.Fatal(err)
		}
	}
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, observe(fixture.options(t), gotKVs))
	if err != nil || result.Decision == nil || len(wantKVs) == 0 || !maps.Equal(gotKVs, wantKVs) {
		t.Fatalf("artifact duplicated complete stream work: want=%v got=%v error=%v", wantKVs, gotKVs, err)
	}
}

func fmtReleaseMeasurementV2Read(noID uint64, kind, hash string) string {
	return new(big.Int).SetUint64(noID).String() + "/" + kind + "/" + hash
}

// Canonical decode and seal both execute the full verifier, but legacy paths
// reject compact fields even when the outer schema is forged back to v1.
func TestReleaseMeasurementV2CanonicalWireAndLegacyRefusal(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	encoded, hash, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil || hash != ReleaseMeasurementContentHash(encoded) {
		t.Fatalf("compact seal: %v", err)
	}
	decoded, result, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, fixture.options(t))
	if err != nil || !reflect.DeepEqual(decoded, fixture.artifact) || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("compact canonical round trip: %v", err)
	}
	if _, _, err := DecodeReleaseMeasurementArtifact(encoded); err == nil {
		t.Fatal("legacy decoder accepted compact artifact")
	}
	forged := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	forged.Schema = ReleaseMeasurementSchema
	if _, err := VerifyReleaseMeasurementArtifact(forged); err == nil {
		t.Fatal("v1 schema spoof accepted compact authority")
	}
	journal := &releaseMeasurementInputJournal{
		Schema: releaseMeasurementInputSchema, DeploymentID: forged.DeploymentID, ChainID: forged.ChainID,
		GenesisHash: forged.GenesisHash, Coordinator: forged.Coordinator, ValidatorID: forged.ValidatorID,
		Netuid: forged.Netuid, SubnetEpoch: forged.SubnetEpoch, PolicyHash: forged.PolicyHash, MeasurementInput: fixture.legacy.Inputs[0],
	}
	legacyJournalBytes, _ := canonicalReleaseMeasurementInputBytes(journal)
	if _, err := decodeReleaseMeasurementInput(legacyJournalBytes); err != nil {
		t.Fatalf("complete genuine legacy journal precondition failed: %v", err)
	}
	journal.MeasurementInput = forged.Inputs[0]
	journalBytes, _ := canonicalReleaseMeasurementInputBytes(journal)
	if _, err := decodeReleaseMeasurementInput(journalBytes); err == nil || !strings.Contains(err.Error(), "legacy measurement input journal rejects compact evidence") {
		t.Fatalf("legacy private journal did not refuse compact authority at its actual boundary: %v", err)
	}
	for _, malformed := range [][]byte{bytes.TrimSpace(encoded), append(slices.Clone(encoded), []byte("{}\n")...), bytes.Replace(encoded, []byte("\"stats\":{"), []byte("\"stats\":{\"attempt_cut\":{},"), 1)} {
		if _, _, err := DecodeReleaseMeasurementArtifactV2(t.Context(), malformed, fixture.options(t)); err == nil {
			t.Fatal("compact strict wire admitted malformed or competing legacy fields")
		}
	}
}

// Header-selected authority, incomplete census and false chain observations
// are all refused before any real reader or scratch directory is touched.
func TestReleaseMeasurementV2RejectsUntrustedAuthorityBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	for _, edit := range []func(*ReleaseMeasurementArtifact, *ReleaseMeasurementV2Options){
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { a.Inputs = a.Inputs[:1] },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { a.Bindings[0].Generation++ },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { a.Pools[0].UID++ },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			a.DepositAudits[0].ObservedDepositRao = "1"
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { a.Policy.Verify.TrailDepth = 4 },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[9]
			op.Expected.Identity.ValidatorVPK = releaseHex32([32]byte{9})
			o.Operators[9] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[9]
			op.Expected.Activation.Domain.ActivationHash[0]++
			o.Operators[9] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[9]
			op.CutNativeBlockHash = releaseHex32([32]byte{0x77})
			o.Operators[9] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[9]
			op.Measurement.Replay.VisitRecord = func(AttemptRecord) error { return nil }
			o.Operators[9] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[10]
			op.Measurement.Replay.ScratchDirectory = o.Operators[9].Measurement.Replay.ScratchDirectory
			o.Operators[10] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[10]
			op.Measurement.Replay.ScratchDirectory = "relative-scratch"
			o.Operators[10] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[10]
			op.Measurement.Replay.Bounds.MaxProofBytes = 0
			o.Operators[10] = op
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[10]
			op.Measurement.Replay.OpenData = nil
			o.Operators[10] = op
		},
	} {
		artifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
		options := fixture.options(t)
		reads := 0
		var scratches []string
		for noID, op := range options.Operators {
			scratches = append(scratches, op.Measurement.Replay.ScratchDirectory)
			op.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
				reads++
				return nil, errors.New("unexpected external read")
			}
			options.Operators[noID] = op
		}
		edit(artifact, &options)
		result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, options)
		if err == nil || reads != 0 {
			t.Fatalf("untrusted authority reached replay: reads=%d error=%v", reads, err)
		}
		assertReleaseMeasurementV2Empty(t, result)
		for _, scratch := range scratches {
			if _, err := os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("authority refusal mutated original owned scratch")
			}
		}
	}
}

// Raw counter drift fails through real replay. A later operator's actual proof
// close refusal also discards the earlier operator's otherwise complete result.
func TestReleaseMeasurementV2LateRecordAndProofErrorsAreAtomic(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	artifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	artifact.Inputs[1].Stats.Providers[0].Assignments++
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, fixture.options(t))
	if err == nil {
		t.Fatal("compact artifact accepted invented exposure")
	}
	assertReleaseMeasurementV2Empty(t, result)
	artifact = cloneReleaseMeasurementArtifact(t, fixture.artifact)
	provider := &artifact.Inputs[1].Stats.Providers[0]
	provider.Assignments, provider.Confirmations = ^uint64(0), ^uint64(0)
	provider.LatencyBuckets[0], provider.LatencyBuckets[1] = ^uint64(0), 1
	result, err = VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, fixture.options(t))
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("compact artifact failed to retain exact histogram overflow refusal: %v", err)
	}
	assertReleaseMeasurementV2Empty(t, result)
	failure := errors.New("second operator actual proof close refusal")
	options := fixture.options(t)
	op := options.Operators[10]
	opens := 0
	op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.operators[10].data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		opens++
		return &attemptCutV2StatsCloseFailure{ReadCloser: reader, failure: failure}, nil
	}
	options.Operators[10] = op
	result, err = VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, options)
	if !errors.Is(err, failure) || opens == 0 {
		t.Fatalf("late proof error was not reached: opens=%d error=%v", opens, err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// One actual opened resource is observed through the real replay boundary.
// Changing/truncating bytes never replaces cryptographic or EOF verification.
type releaseMeasurementV2ObservedReader struct {
	reader  io.Reader
	owner   io.Closer
	corrupt bool
	changed bool
	closes  *int
}

// Corruption is confined to this reader’s first nonempty returned bytes.
func (self *releaseMeasurementV2ObservedReader) Read(buffer []byte) (int, error) {
	count, err := self.reader.Read(buffer)
	if self.corrupt && !self.changed && count > 0 {
		buffer[0] ^= 1
		self.changed = true
	}
	return count, err
}

// Exactly one delegated close records actual resource ownership.
func (self *releaseMeasurementV2ObservedReader) Close() error {
	(*self.closes)++
	return self.owner.Close()
}

// The public whole-artifact path still consumes exact signed record/proof
// bytes and closes every owner, including an opener returning reader+error.
func TestReleaseMeasurementV2RejectsChangedStreamsAndJoinsOwners(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	for _, test := range []struct {
		name, kind          string
		truncate, withError bool
	}{
		{name: "changed-record", kind: AttemptStreamV2Records, truncate: false, withError: false},
		{name: "changed-proof", kind: AttemptStreamV2Proofs, truncate: false, withError: false},
		{name: "short-proof", kind: AttemptStreamV2Proofs, truncate: true, withError: false},
		{name: "reader-and-error", kind: AttemptStreamV2Proofs, truncate: false, withError: true},
	} {
		{
			options := fixture.options(t)
			operator := options.Operators[10]
			opens, closes := 0, 0
			failure := errors.New("actual compact stream opener returned reader and failure")
			operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
				reader, err := fixture.operators[10].data(ctx, kind, hash, size)
				if err != nil || kind != test.kind {
					return reader, err
				}
				opens++
				observed := &releaseMeasurementV2ObservedReader{reader: reader, owner: reader, corrupt: !test.truncate && !test.withError, closes: &closes}
				if test.truncate {
					if size == 0 {
						return observed, errors.New("actual proof chunk is unexpectedly empty")
					}
					observed.reader = io.LimitReader(reader, int64(size)-1)
				}
				if test.withError {
					return observed, failure
				}
				return observed, nil
			}
			options.Operators[10] = operator
			result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, options)
			if err == nil || opens == 0 || opens != closes || test.withError && !errors.Is(err, failure) {
				t.Fatalf("%s escaped full verification/ownership: opens=%d closes=%d error=%v", test.name, opens, closes, err)
			}
			assertReleaseMeasurementV2Empty(t, result)
		}
	}
}

// Cancellation from a later operator's real proof Close joins that resource
// and returns neither canonical bytes nor a partial whole-artifact decision.
func TestReleaseMeasurementV2CancellationJoinsAndClears(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options := fixture.options(t)
	op := options.Operators[10]
	closes := 0
	op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.operators[10].data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &attemptCutV2MeasurementCancelClose{ReadCloser: reader, cancel: cancel, closes: &closes}, nil
	}
	options.Operators[10] = op
	encoded, hash, err := SealReleaseMeasurementArtifactV2(ctx, fixture.artifact, options)
	if !errors.Is(err, context.Canceled) || closes == 0 || encoded != nil || hash != "" {
		t.Fatalf("post-close cancellation published artifact: closes=%d error=%v", closes, err)
	}
	result, err := VerifyReleaseMeasurementArtifactV2(ctx, fixture.artifact, fixture.options(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatal("pre-canceled artifact was admitted")
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// Admission detaches both operators before the first callback, not merely the
// currently replaying operator. Returned wire must retain that exact snapshot.
func TestReleaseMeasurementV2OwnsEveryOperatorBeforeFirstIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	artifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	options := fixture.options(t)
	expectedBytes, _ := canonicalReleaseMeasurementBytes(artifact)
	changed := false
	op := options.Operators[9]
	op.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !changed {
			artifact.Inputs[1].Stats.Providers[0].Assignments++
			artifact.Inputs[1].AttemptCutV2.Signature[0] ^= 1
			options.Bindings[0].Generation++
			other := options.Operators[10]
			for _, key := range other.Measurement.Replay.ServerKeys {
				key[0] ^= 1
			}
			clear(other.Measurement.CurrentBindingKVs)
			changed = true
		}
		return fixture.operators[9].metadata(ctx, hash, size)
	}
	options.Operators[9] = op
	encoded, _, err := SealReleaseMeasurementArtifactV2(t.Context(), artifact, options)
	if err != nil || !changed || !bytes.Equal(encoded, expectedBytes) {
		t.Fatalf("first callback changed later operator/wire: changed=%v error=%v", changed, err)
	}
}

// Finite census and byte budgets precede copying; fixed hash widths prevent
// generic normalization from expanding an oversized single candidate string.
func TestReleaseMeasurementV2BoundsAndFixedWidthAdmission(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	for _, edit := range []func(*ReleaseMeasurementArtifact, *ReleaseMeasurementV2Options){
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { o.MaxControlBytes = 1 },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { o.MaxControlBytes = ^uint64(0) },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { o.MaxArtifactBytes = 1 },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { o.MaxOperators = 1 },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) { o.MaxHeadEntries = 0 },
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			a.Inputs[0].Stats.Providers[0].ClientID = strings.Repeat("a", 100000)
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			a.Inputs[0].Stats.Providers[0].EgressIPHashHexes = []string{strings.Repeat("aA", 100000)}
		},
		func(a *ReleaseMeasurementArtifact, o *ReleaseMeasurementV2Options) {
			op := o.Operators[9]
			op.Measurement.MaxProviders = 1
			o.Operators[9] = op
		},
	} {
		a := cloneReleaseMeasurementArtifact(t, fixture.artifact)
		o := fixture.options(t)
		edit(a, &o)
		result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), a, o)
		if err == nil {
			t.Fatal("compact artifact exceeded an explicit bound")
		}
		assertReleaseMeasurementV2Empty(t, result)
	}
}

// A genuine same-prefix retry fully verifies both artifacts. A changed prior
// quality or missing operator cannot borrow the prior content address.
func TestReleaseMeasurementV2SameEpochLineageReplaysExactPrefix(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	previous, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	current := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	current.PreviousArtifactHash = ReleaseMeasurementContentHash(previous)
	currentOptions := fixture.options(t)
	currentOptions.Expected = releaseMeasurementV2Decision(current)
	result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, fixture.options(t), current, currentOptions)
	if err != nil || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("real same-prefix lineage failed: %v", err)
	}
	current.Inputs[0].Stats.Providers[0].HasPriorQuality = true
	current.Inputs[0].Stats.Providers[0].PriorQualityPPM = 12345
	currentOptions = fixture.options(t)
	currentOptions.Expected = releaseMeasurementV2Decision(current)
	result, err = VerifyReleaseMeasurementLineageV2(t.Context(), previous, fixture.options(t), current, currentOptions)
	if err == nil || !strings.Contains(err.Error(), "prior pool EMA") {
		t.Fatalf("lineage ignored prior state: %v", err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// The intermediate ordinary slice must not activate settlement transitions,
// terminal closures, or legacy persistence through an implicit raw-stat path.
func TestReleaseMeasurementV2TerminalBoundaryFailsClosed(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	a := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	a.SettlementClosureV2 = &AttemptSettlementClosureV2{Schema: AttemptSettlementClosureV2Schema, Epoch: 42}
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), a, fixture.options(t))
	if err == nil || !strings.Contains(err.Error(), "terminal closure") {
		t.Fatalf("unintegrated compact terminal was accepted: %v", err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// Native egress moves independently of cumulative settlement quality. Both
// representations genuinely sign the new cursor over exactly the original M8
// records; clearing only egress is independently verified by the v1 oracle.
func TestReleaseMeasurementV2JointSignedNativeCursorParity(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 15)
	priorStats := fixture.want.StatsByNO
	for index := range fixture.artifact.Inputs {
		input := &fixture.artifact.Inputs[index]
		operator := fixture.operators[input.NoID]
		input.AttemptCutV2.Context.EgressFirstSequence = input.AttemptCutV2.LastSequence + 1
		input.AttemptCutV2.Context.EgressGeneration++
		input.EgressGeneration = input.AttemptCutV2.Context.EgressGeneration
		// The caller independently observes this later native cursor before
		// obtaining its signature; options do not infer it from the candidate.
		operator.seal.expected = input.AttemptCutV2.Context
		signature, err := input.AttemptCutV2.Sign(operator.seal.key, operator.seal.bounds)
		if err != nil {
			t.Fatal(err)
		}
		input.AttemptCutV2.Signature = signature
		legacy := cloneAttemptLedgerCut(t, operator.legacy)
		legacy.EgressFirstSequence = legacy.LastSequence + 1
		message, err := attemptCutSignatureMessage(legacy)
		if err != nil {
			t.Fatal(err)
		}
		legacy.Signature = ed25519.Sign(operator.seal.key, message)
		operator.legacy = legacy
		for provider := range input.Stats.Providers {
			input.Stats.Providers[provider].EgressIPHashHexes = nil
		}
	}
	fixture.rebuildLegacy(t)
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("signed native cursor changed whole artifact parity: %v", err)
	}
	if len(result.Decision.EligibleHead) != 0 || len(result.Decision.UIDs) != 2 {
		t.Fatal("empty native window did not cede head to the two real positive pools")
	}
	for noID, stats := range result.Decision.StatsByNO {
		if result.ReplayByNO[noID].Records.ItemCount != 122 {
			t.Fatal("native cursor skipped cumulative settlement records")
		}
		for clientID, provider := range stats.Providers {
			prior := priorStats[noID].Providers[clientID]
			if provider.Exposure != prior.Exposure || provider.QualityPPM != prior.QualityPPM || provider.HasQuality != prior.HasQuality || len(provider.EgressIPHashes) != 0 {
				t.Fatal("native cursor changed settlement quality or retained old egress")
			}
		}
	}
}

// A different, completely valid signed ordering of the same genuine trails
// has identical raw scores but must not be accepted as the prior cut's prefix.
// This reaches the actual private checkpoint during current public replay.
func TestReleaseMeasurementV2LineageRejectsGenuineRewrittenPrefix(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	previous, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	previousOptions := fixture.options(t)
	original := fixture.operators[9]
	records := original.seal.recordTs
	if len(records) != 18 || records[7].Disposition != AttemptDispositionComplete || records[15].Disposition != AttemptDispositionComplete {
		t.Fatal("two full M8 trails and one failed trail are required")
	}
	ledger, err := NewDiskAttemptLedger(t.Context(), newAttemptLedgerDiskTestStateDir(t), original.seal.expected.Identity, attemptLedgerDiskTestCoordinator, original.seal.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	reordered := append(append(append([]AttemptRecord{}, records[8:16]...), records[:8]...), records[16:]...)
	for _, record := range reordered {
		appended, err := ledger.AppendContext(t.Context(), record)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAttemptRecord(appended, original.seal.expected.Identity, original.seal.key.Public().(ed25519.PublicKey), original.seal.server.serverPublicKeys()); err != nil {
			t.Fatalf("alternative signed ordering is not genuine: %v", err)
		}
	}
	options, _ := newAttemptCutV2SealTestOptions(t, original.seal)
	cut, _, err := SealAttemptCutV2(t.Context(), ledger, original.seal.expected, original.seal.policy, original.seal.key, original.seal.bounds, options)
	if err != nil || cut == nil || cut.Root == fixture.artifact.Inputs[0].AttemptCutV2.Root {
		t.Fatalf("genuine fork did not produce a distinct root: %v", err)
	}
	fixture.artifact.Inputs[0].AttemptCutV2 = cut
	fixture.artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(previous)
	fixture.operators[9] = &releaseMeasurementV2TestOperator{seal: original.seal, legacy: original.legacy, metadata: options.ReadMetadata, data: options.OpenData}
	standalone, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil || !reflect.DeepEqual(standalone.Decision, fixture.want) {
		t.Fatalf("valid independent fork precondition failed: %v", err)
	}
	result, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, previousOptions, fixture.artifact, fixture.options(t))
	if err == nil || !strings.Contains(err.Error(), "rewrites its authenticated prior prefix") {
		t.Fatalf("genuine fork bypassed actual prefix checkpoint: %v", err)
	}
	assertReleaseMeasurementV2Empty(t, result)
}

// Earlier acceptance and mutable returned maps confer no authority on a later
// call. The same content locator with a changed signature must verify again.
func TestReleaseMeasurementV2StandaloneRechecksSignatures(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	first, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	clear(first.Decision.StatsByNO)
	clear(first.ReplayByNO)
	changed := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	changed.Inputs[1].AttemptCutV2.Signature[0] ^= 1
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), changed, fixture.options(t))
	if err == nil {
		t.Fatal("standalone compact artifact reused old signature acceptance")
	}
	assertReleaseMeasurementV2Empty(t, result)
	result, err = VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("caller-mutated prior result contaminated next call: %v", err)
	}
}

// A valid UID zero observation cannot inherit historical work signed for UID1.
// With real positive pool quality it still produces the exact legacy decision.
func TestReleaseMeasurementV2CurrentUIDZeroAndControlledMaskParity(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 15)
	for index := range fixture.artifact.Bindings {
		binding := &fixture.artifact.Bindings[index]
		if binding.Active {
			binding.RecordUID, binding.LiveUID = 0, 0
		}
	}
	fixture.artifact.ControlledNOIDs = []uint64{9}
	fixture.rebuildLegacy(t)
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("UID0/current-control math differs: %v", err)
	}
	if !slices.Contains(result.Decision.MaskedUIDs, uint16(0)) || !slices.Contains(result.Decision.MaskedUIDs, uint16(109)) || len(result.Decision.UIDs) != 1 || result.Decision.UIDs[0] != 110 {
		t.Fatal("shared eligible UID or controlled pool escaped exact masking")
	}
}

// Use the independently reconstructed v1 decision, never the compact result,
// to build the exact intent fields that the common comparison must recover.
func (self *releaseMeasurementV2TestFixture) intent(t *testing.T, encoded []byte) *SteeringIntent {
	t.Helper()
	a, w := self.artifact, self.want
	headScores := make([]*big.Rat, len(w.EligibleHead))
	for index, input := range w.EligibleHead {
		headScores[index] = input.Score
	}
	eligible, err := rationalJSON(headScores)
	if err != nil {
		t.Fatal(err)
	}
	scores, err := rationalJSON(w.Scores)
	if err != nil {
		t.Fatal(err)
	}
	return &SteeringIntent{
		ValidatorID: a.ValidatorID, Netuid: a.Netuid, SubnetEpoch: a.SubnetEpoch, NativeSnapshotBlock: a.NativeSnapshotBlock, NativeSnapshotHash: a.NativeSnapshotHash,
		EVMSnapshotBlock: a.EVMSnapshotBlock, EVMSnapshotHash: a.EVMSnapshotHash, SettlementEpoch: a.SettlementEpoch, PolicyHash: a.PolicyHash, SelfUID: a.SelfUID,
		MeasurementArtifactHash: ReleaseMeasurementContentHash(encoded), MeasurementArtifactSize: uint64(len(encoded)),
		MaskedUIDs: slices.Clone(w.MaskedUIDs), EligibleHeadUIDs: headSelectionUIDs(w.EligibleHead), EligibleHeadScores: eligible,
		SelectedHeadUIDs: headSelectionUIDs(w.SelectedHead), RejectedHeadUIDs: headSelectionUIDs(w.RejectedHead), StaleHeadBindings: slices.Clone(w.StaleBindings), DepositAudits: slices.Clone(a.DepositAudits), UIDs: slices.Clone(w.UIDs), Scores: scores,
	}
}

// Intent comparison reaches full compact decoding/replay and the existing
// exact vector comparison. A different content address cannot borrow it.
func TestReleaseMeasurementV2IntentRequiresFullEvidenceAndContent(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	encoded, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	intent := fixture.intent(t, encoded)
	if err := VerifyReleaseMeasurementIntentV2(t.Context(), encoded, fixture.options(t), intent); err != nil {
		t.Fatalf("real compact intent comparison failed: %v", err)
	}
	intent.MeasurementArtifactHash = ReleaseMeasurementContentHash([]byte("other"))
	if err := VerifyReleaseMeasurementIntentV2(t.Context(), encoded, fixture.options(t), intent); err == nil {
		t.Fatal("intent accepted unrelated content address")
	}
}

// A first transport callback cannot repair a rejected input's decision or
// corrupt an already admitted one. This uses the actual public intent route.
func TestReleaseMeasurementV2IntentOwnsDecisionBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	encoded, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	for _, initiallyValid := range []bool{false, true} {
		intent := fixture.intent(t, encoded)
		validScore := intent.Scores[0]
		if !initiallyValid {
			intent.Scores[0] = RationalJSON{Numerator: "999", Denominator: "1"}
		}
		options := fixture.options(t)
		operator := options.Operators[9]
		changed := false
		operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			if !changed {
				if initiallyValid {
					intent.Scores[0] = RationalJSON{Numerator: "999", Denominator: "1"}
					intent.MeasurementArtifactHash = "changed"
				} else {
					intent.Scores[0] = validScore
				}
				changed = true
			}
			return fixture.operators[9].metadata(ctx, hash, size)
		}
		options.Operators[9] = operator
		err := VerifyReleaseMeasurementIntentV2(t.Context(), encoded, options, intent)
		if !changed || initiallyValid && err != nil || !initiallyValid && (err == nil || !strings.Contains(err.Error(), "decision differs")) {
			t.Fatalf("transport changed admitted intent: initialValid=%v changed=%v error=%v", initiallyValid, changed, err)
		}
	}
}

// Distinct earlier anchors are normal for genuine operators. Both complete
// signed M8 streams, not merely their headers, must reach the common decision.
func TestReleaseMeasurementV2DistinctOperatorActivationHashes(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	options := fixture.options(t)
	left, right := options.Operators[9].Expected, options.Operators[10].Expected
	if left.Identity.NoID == right.Identity.NoID || left.Activation.Domain.ActivationHash == right.Activation.Domain.ActivationHash || !equalAttemptCutV2CommonDomain(left.Activation.Domain, right.Activation.Domain) {
		t.Fatal("two independent activation anchors/common deployment precondition changed")
	}
	before, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, options)
	if err != nil || len(result.ReplayByNO) != 2 || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("complete distinct-activation operators refused: %v", err)
	}
	for _, replayed := range result.ReplayByNO {
		if replayed.Records.ItemCount != 18 || replayed.CompleteCount != 2 || replayed.FailedCount != 1 {
			t.Fatal("distinct anchors lost complete M8/failed census")
		}
	}
	after, _ := canonicalReleaseMeasurementBytes(fixture.artifact)
	if !bytes.Equal(before, after) {
		t.Fatal("common-domain join normalized caller activation hashes")
	}
}

// A valid candidate signature does not choose the caller's activation hash.
// Changing or swapping pinned per-NO anchors fails before any remote read.
func TestReleaseMeasurementV2RejectsChangedOperatorActivationHashes(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	for _, name := range []string{"signed-candidate", "swapped-expected", "altered-expected"} {
		{
			artifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
			options := fixture.options(t)
			switch name {
			case "signed-candidate":
				cut := artifact.Inputs[1].AttemptCutV2
				cut.Context.Activation.Domain.ActivationHash[2] ^= 1
				signature, err := cut.Sign(fixture.operators[10].seal.key, fixture.operators[10].seal.bounds)
				if err != nil {
					t.Fatal(err)
				}
				cut.Signature = signature
				if err := cut.VerifyHeader(cut.Context, fixture.operators[10].seal.bounds); err != nil {
					t.Fatalf("changed-anchor candidate was not genuinely signed: %v", err)
				}
			case "swapped-expected":
				left, right := options.Operators[9], options.Operators[10]
				left.Expected.Activation.Domain.ActivationHash, right.Expected.Activation.Domain.ActivationHash = right.Expected.Activation.Domain.ActivationHash, left.Expected.Activation.Domain.ActivationHash
				options.Operators[9], options.Operators[10] = left, right
			case "altered-expected":
				operator := options.Operators[10]
				operator.Expected.Activation.Domain.ActivationHash[2] ^= 1
				options.Operators[10] = operator
			}
			reads := 0
			for noID, operator := range options.Operators {
				operator.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
					reads++
					return nil, errors.New("unexpected activation read")
				}
				options.Operators[noID] = operator
			}
			result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, options)
			if err == nil || reads != 0 || !strings.Contains(err.Error(), "differs from expected authority") {
				t.Fatalf("%s per-operator activation pin bypassed: reads=%d error=%v", name, reads, err)
			}
			assertReleaseMeasurementV2Empty(t, result)
		}
	}
}

// Each common domain field is independently required. The changed candidate
// header is genuinely signed and internally shape-valid, yet cannot redefine
// the common release deployment through one operator's otherwise pinned hash.
func TestReleaseMeasurementV2RejectsCommonDomainDrift(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	for _, test := range []struct {
		name   string
		change func(*AttemptCutV2Context)
	}{
		{name: "chain", change: func(c *AttemptCutV2Context) { c.Identity.ChainID++; c.Activation.Domain.ChainID = c.Identity.ChainID }},
		{name: "genesis", change: func(c *AttemptCutV2Context) {
			c.Activation.Domain.GenesisHash[0] ^= 1
			c.Identity.GenesisHash = attemptHex32(c.Activation.Domain.GenesisHash)
		}},
		{name: "netuid", change: func(c *AttemptCutV2Context) { c.Identity.Netuid++; c.Activation.Domain.Netuid = c.Identity.Netuid }},
		{name: "coordinator", change: func(c *AttemptCutV2Context) { c.Activation.Domain.Coordinator[0] ^= 1 }},
		{name: "vault", change: func(c *AttemptCutV2Context) { c.Activation.Domain.SettlementVault[0] ^= 1 }},
		{name: "deployment", change: func(c *AttemptCutV2Context) {
			c.Identity.DeploymentID += "-other"
			c.Activation.Domain.DeploymentIDHash = sha256.Sum256([]byte(c.Identity.DeploymentID))
		}},
		{name: "policy", change: func(c *AttemptCutV2Context) { c.Activation.Domain.PolicyHash[0] ^= 1 }},
		{name: "epoch", change: func(c *AttemptCutV2Context) { c.Activation.Domain.ActivationEpoch-- }},
	} {
		{
			artifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
			options := fixture.options(t)
			operator := options.Operators[10]
			test.change(&operator.Expected)
			cut := artifact.Inputs[1].AttemptCutV2
			cut.Context = operator.Expected
			signature, err := cut.Sign(fixture.operators[10].seal.key, operator.Bounds)
			if err != nil {
				t.Fatalf("changed common field lost valid signed-header precondition: %v", err)
			}
			cut.Signature = signature
			if err := cut.VerifyHeader(operator.Expected, operator.Bounds); err != nil {
				t.Fatal(err)
			}
			options.Operators[10] = operator
			reads := 0
			for noID, observed := range options.Operators {
				observed.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
					reads++
					return nil, errors.New("unexpected mixed-domain read")
				}
				options.Operators[noID] = observed
			}
			result, err := VerifyReleaseMeasurementArtifactV2(t.Context(), artifact, options)
			if err == nil || reads != 0 || !strings.Contains(err.Error(), "do not share one authenticated activation") {
				t.Fatalf("common domain %s was ignored: reads=%d error=%v", test.name, reads, err)
			}
			assertReleaseMeasurementV2Empty(t, result)
		}
	}
	// Keep the helper's explicit field census visible if the protocol grows.
	if reflect.TypeFor[protocol.ValidatorEvidenceDomain]().NumField() != 9 {
		t.Fatal("activation common-domain field census requires explicit review")
	}
}
