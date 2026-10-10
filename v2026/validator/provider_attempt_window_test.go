//go:build linux || darwin

// Real M8 failed/complete trails, disk ledgers, two public replicas and original
// dual consents exercise the independent cross-validator census boundary.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Source identities are chosen before sealing any cut or public metadata.
type providerAttemptWindowTestOwner struct {
	fixture  *evidenceCensusV2TestFixture
	read     ValidatorEvidencePublicationV2ReadOptions
	manifest *ValidatorEvidencePublicationV2Manifest
}

// Every operator, including the idle lane, publishes the actual complete batch.
func newProviderAttemptWindowTestOwner(t *testing.T, seed byte, completed, failed int) *providerAttemptWindowTestOwner {
	return newProviderAttemptWindowTestOwnerWithRequests(t, seed, completed, failed, nil)
}

// The hook prepares the real optional request owner before the new fixture's
// first send; existing terminal-only fixtures keep their exact old path.
func newProviderAttemptWindowTestOwnerWithRequests(t *testing.T, seed byte, completed, failed int, before func(*attemptCutV2SealTestFixture)) *providerAttemptWindowTestOwner {
	t.Helper()
	return newProviderAttemptWindowTestOwnerForWindow(t, seed, completed, failed, before, nil)
}

// A separately selected window precedes every dual-signed publication. Existing
// callers retain their exact original clock; a new fixture never relabels a cut.
func newProviderAttemptWindowTestOwnerForWindow(t *testing.T, seed byte, completed, failed int, before func(*attemptCutV2SealTestFixture), window *protocol.ValidatorEvidenceWindow) *providerAttemptWindowTestOwner {
	t.Helper()
	return newProviderAttemptWindowTestOwnerForCensus(t, seed, completed, failed, before, window, []uint64{9, 11})
}

// A validator measuring one operator publishes that lane at its only origin.
func newProviderAttemptWindowTestOwnerForCensus(t *testing.T, seed byte, completed, failed int, before func(*attemptCutV2SealTestFixture), window *protocol.ValidatorEvidenceWindow, noIds []uint64) *providerAttemptWindowTestOwner {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	replicas, stores := newAttemptCutV2ReplicaTestStoreCensus(t, releaseEvidenceV2ReplicaCensus(len(noIds)))
	fixture := &evidenceCensusV2TestFixture{hotkey: hotkey, replicas: replicas, stores: stores}
	self := &providerAttemptWindowTestOwner{fixture: fixture}
	for position, noId := range noIds {
		complete, failure := 0, 0
		if position == 0 {
			complete, failure = completed, failed
		}
		seal := newAttemptCutV2SealTestFixtureForOperator(t, 8, complete, failure, noId)
		if before != nil {
			before(seal)
		}
		domain := seal.expected.Activation.Domain
		activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: domain.ChainID, GenesisHash: domain.GenesisHash, Netuid: domain.Netuid, Coordinator: domain.Coordinator, SettlementVault: domain.SettlementVault, DeploymentIDHash: domain.DeploymentIDHash, PolicyHash: domain.PolicyHash, Epoch: domain.ActivationEpoch}, Hotkey: hotkey.PublicKey(), NoID: noId, VPK: [32]byte(seal.key.Public().(ed25519.PublicKey)), FirstSequence: 1, NativeBlock: 1, NativeHash: [32]byte{19}, EVMBlock: 1, EVMHash: [32]byte{20}}
		seal.expected.Activation.Domain, err = activation.EvidenceDomain()
		if err != nil {
			t.Fatal(err)
		}
		seal.expected.Activation.Hotkey = hotkey.PublicKey()
		seal.bounds.Records.MaxPageBytes = 256 * 1024
		publication, err := SealReplicatedAttemptCutV2(t.Context(), seal.ledger, seal.expected, seal.policy, seal.key, seal.bounds, AttemptCutV2ReplicaOptions{ReplayBounds: seal.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: seal.server.serverPublicKeys(), Replicas: replicas})
		if err != nil || publication == nil {
			t.Fatalf("real owner cut: %v", err)
		}
		measurement := func() ReleaseStatsMeasurement {
			seal.engine.stats.mu.Lock()
			defer seal.engine.stats.mu.Unlock()
			return seal.engine.stats.releaseStatsMeasurementWithLock()
		}()
		reader, err := NewHTTPAttemptStreamV2Reader(replicas[0].Origin, seal.bounds)
		if err != nil {
			t.Fatal(err)
		}
		fixture.operators = append(fixture.operators, &attemptCutV2StatsTestFixture{seal: seal, cut: *publication.Cut, measurement: measurement, metadata: reader.ReadMetadata, data: reader.OpenData})
		self.read.Activations = append(self.read.Activations, activation)
	}
	fixture.closure = sealAttemptSettlementV2Test(t, fixture.operators...)
	options := fixture.options(t)
	if window != nil {
		options.Window = *window
	}
	self.read.Window = options.Window
	for _, replica := range replicas {
		self.read.Origins = append(self.read.Origins, replica.Origin)
	}
	self.read.Bounds = ReleaseEvidenceV2Bounds{Cut: fixture.operators[0].seal.bounds, MaxParticipants: options.Settlement.MaxParticipants, MaxTransitionBytes: options.Settlement.MaxTransitionBytes, MaxClosureBytes: options.Settlement.MaxClosureBytes}
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
	if err != nil || publication == nil {
		t.Fatalf("real owner closed publication: %v", err)
	}
	self.manifest, err = WriteValidatorEvidencePublicationV2Manifest(t.Context(), newAttemptSettlementRuntimeV2TestStateDir(t), publication, noIds, options.Settlement.MaxClosureBytes, options.Settlement.MaxParticipants)
	if err != nil {
		t.Fatal(err)
	}
	return self
}

// The independent registry is created from fixture deployment identities,
// before the candidate may omit, duplicate or replace any published owner.
type providerAttemptWindowTestFixture struct {
	owners      []*providerAttemptWindowTestOwner
	candidate   ProviderAttemptWindow
	expectation protocol.ProviderAttemptRegistryExpectation
}

// Two distinct original validator signing keys own four genuine operator lanes.
func newProviderAttemptWindowTestFixture(t *testing.T, trails bool) *providerAttemptWindowTestFixture {
	t.Helper()
	return newProviderAttemptWindowTestFixtureForCensus(t, trails, []byte{51, 52}, []uint64{9, 11})
}

// Every registered validator measures the same configured operator census.
// The first validator's first lane may carry real complete/failed trails.
func newProviderAttemptWindowTestFixtureForCensus(t *testing.T, trails bool, seeds []byte, noIds []uint64) *providerAttemptWindowTestFixture {
	t.Helper()
	self := &providerAttemptWindowTestFixture{}
	for index, seed := range seeds {
		complete, failed := 0, 0
		if trails && index == 0 {
			complete, failed = 1, 1
		}
		self.owners = append(self.owners, newProviderAttemptWindowTestOwnerForCensus(t, seed, complete, failed, nil, nil, noIds))
	}
	sort.Slice(self.owners, func(i, j int) bool {
		a, b := self.owners[i].fixture.hotkey.PublicKey(), self.owners[j].fixture.hotkey.PublicKey()
		return bytes.Compare(a[:], b[:]) < 0
	})
	domain := self.owners[0].read.Activations[0].Domain
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{53}, ed25519.SeedSize))
	self.expectation = protocol.ProviderAttemptRegistryExpectation{Domain: protocol.ProviderAttemptDomain{ChainId: domain.ChainID, GenesisHash: domain.GenesisHash, Netuid: domain.Netuid, Coordinator: domain.Coordinator, SettlementVault: domain.SettlementVault, DeploymentIdHash: domain.DeploymentIDHash, PolicyHash: domain.PolicyHash}, Signer: [32]byte(key.Public().(ed25519.PublicKey)), MaxRevisions: 4, MaxOwners: 8, MaxOperatorLanes: 16, MaxBytes: 64 * 1024}
	registry := protocol.ProviderAttemptRegistry{Schema: protocol.ProviderAttemptRegistrySchema, Domain: self.expectation.Domain, EffectiveEpoch: self.owners[0].read.Window.Epoch}
	self.candidate.Schema = ProviderAttemptWindowSchema
	for _, owner := range self.owners {
		hotkey := owner.fixture.hotkey.PublicKey()
		registry.Owners = append(registry.Owners, protocol.ProviderAttemptOwner{Hotkey: hotkey, NoIds: slices.Clone(noIds)})
		self.candidate.Members = append(self.candidate.Members, ProviderAttemptWindowMember{Hotkey: hotkey, Manifest: *owner.manifest})
	}
	signed, err := protocol.SealProviderAttemptRegistry(t.Context(), registry, self.expectation, key)
	if err != nil {
		t.Fatal(err)
	}
	self.expectation.RootHash, err = signed.Hash()
	if err != nil {
		t.Fatal(err)
	}
	self.expectation.RequiredHeadHash = self.expectation.RootHash
	self.candidate.RegistryHistory = []protocol.ProviderAttemptRegistry{*signed}
	return self
}

// Every verification receives fresh explicit scratch owners and the same
// independently constructed historical activation and terminal boundary.
func (self *providerAttemptWindowTestFixture) options(t *testing.T) ProviderAttemptWindowOptions {
	t.Helper()
	result := ProviderAttemptWindowOptions{Registry: self.expectation, Window: self.owners[0].read.Window, Validators: map[[32]byte]ProviderAttemptValidatorOptions{}, MaxProviders: 128, MaxMetadataBytes: 16 * 1024 * 1024, MaxRecordBytes: 64 * 1024 * 1024, MaxRecords: 100000}
	for _, owner := range self.owners {
		result.Validators[owner.fixture.hotkey.PublicKey()] = ProviderAttemptValidatorOptions{Publication: owner.read, Settlement: owner.fixture.options(t).Settlement}
	}
	return result
}

// Failed exposure and idle/no-payout validators survive the complete replay.
// Legacy cuts do not magically prove missing pre-assignment requests or wallets.
func TestProviderAttemptWindowReplaysAllOwnersFailedAndIdle(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, true)
	result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, fixture.options(t))
	if err != nil || result == nil || !result.CutCensusComplete || result.OwnedRequestsComplete || result.Validators != 2 || result.OperatorLanes != 4 || result.CompleteTrails != 1 || result.FailedTrails != 1 || result.RegistryHash == ([32]byte{}) || result.WindowHash == ([32]byte{}) {
		t.Fatalf("complete original owner replay differs: %+v %v", result, err)
	}
	var assignments, confirmations uint64
	for _, row := range result.Providers {
		assignments += row.Assignments
		confirmations += row.Confirmations
	}
	if assignments != 8 || confirmations != 7 {
		t.Fatalf("failed exposure was omitted or pending checkpoints were double-counted: %d/%d", assignments, confirmations)
	}
}

// A real empty cut for every independently expected validator proves zero
// recorded terminal trials; omitting the final owner remains unknown.
func TestProviderAttemptWindowIdleIsDistinctFromMissingOwner(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, false)
	result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, fixture.options(t))
	if err != nil || result == nil || !result.CutCensusComplete || len(result.Providers) != 0 {
		t.Fatalf("real empty owner windows did not replay: %+v %v", result, err)
	}
	candidate := fixture.candidate
	candidate.Members = candidate.Members[:1]
	if result, err := VerifyProviderAttemptWindow(t.Context(), candidate, fixture.options(t)); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsUnavailable) {
		t.Fatalf("missing idle validator became zero evidence: %+v %v", result, err)
	}
}

// A changed clock, repeated owner or foreign key is refused before public I/O.
func TestProviderAttemptWindowRefusesDuplicateClockAndForeignOwner(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, false)
	before := [2][2][2]int{fixture.owners[0].fixture.counts(), fixture.owners[1].fixture.counts()}
	for _, mutate := range []func(*ProviderAttemptWindow){
		func(value *ProviderAttemptWindow) { value.Members[1] = value.Members[0] },
		func(value *ProviderAttemptWindow) { value.Members[0].Manifest.Epoch++ },
		func(value *ProviderAttemptWindow) { value.Members[0].Hotkey[0] ^= 1 },
	} {
		value := fixture.candidate
		value.Members = append([]ProviderAttemptWindowMember(nil), value.Members...)
		mutate(&value)
		if result, err := VerifyProviderAttemptWindow(t.Context(), value, fixture.options(t)); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
			t.Fatalf("duplicate/foreign/cross-window original was accepted: %+v %v", result, err)
		}
	}
	if after := [2][2][2]int{fixture.owners[0].fixture.counts(), fixture.owners[1].fixture.counts()}; after != before {
		t.Fatal("invalid complete owner census performed public I/O")
	}
}

// The historical activation fixes the exact vpk; a new key in a locator does
// not silently rotate an old window's original registered signing authority.
func TestProviderAttemptWindowRefusesChangedActivationKey(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, false)
	options := fixture.options(t)
	hotkey := fixture.owners[0].fixture.hotkey.PublicKey()
	owner := options.Validators[hotkey]
	owner.Publication.Activations = append([]protocol.ValidatorEvidenceActivation(nil), owner.Publication.Activations...)
	owner.Publication.Activations[0].VPK[0] ^= 1
	options.Validators[hotkey] = owner
	if result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, options); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("candidate vpk rotation rewrote original window: %+v %v", result, err)
	}
}

// Real public metadata is insufficient when any original proof object is gone.
func TestProviderAttemptWindowRequiresActualRecordReplay(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, true)
	removed := false
	for _, owner := range fixture.owners {
		store := owner.fixture.stores[0]
		func() {
			store.stateLock.Lock()
			defer store.stateLock.Unlock()
			for key := range store.objects {
				if strings.HasPrefix(key, "records/") {
					delete(store.objects, key)
					removed = true
					break
				}
			}
		}()
		if removed {
			break
		}
	}
	if !removed {
		t.Fatal("real fixture did not publish original records")
	}
	if result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, fixture.options(t)); err == nil || result != nil {
		t.Fatalf("signed metadata replaced missing original attempt bytes: %+v %v", result, err)
	}
}

// Cancellation after an actual source open clears every staged provider row;
// a fresh owned continuation can replay the same immutable public originals.
func TestProviderAttemptWindowCanceledReplayThenHealthyContinuation(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options := fixture.options(t)
	for hotkey, owner := range options.Validators {
		for noId, operator := range owner.Settlement.Operators {
			original := operator.Measurement.Replay.OpenData
			operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
				value, err := original(ctx, kind, hash, size)
				cancel()
				return value, err
			}
			owner.Settlement.Operators[noId] = operator
		}
		options.Validators[hotkey] = owner
	}
	if result, err := VerifyProviderAttemptWindow(ctx, fixture.candidate, options); result != nil || !errors.Is(err, context.Canceled) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("canceled source retained partial reliability: %+v %v", result, err)
	}
	if result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, fixture.options(t)); err != nil || result == nil || !result.CutCensusComplete {
		t.Fatalf("healthy same-authority continuation failed: %+v %v", result, err)
	}
}

// Aggregate work limits apply across every owner without discarding a suffix.
func TestProviderAttemptWindowCapacityKeepsCompleteCensus(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, true)
	options := fixture.options(t)
	options.MaxRecords = 1
	if result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, options); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsCapacity) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("bounded census was silently trimmed or quarantined: %+v %v", result, err)
	}
}

// Retained registry/census bytes survive JSON without acquiring a verifier
// result flag. Every reopen still traverses the complete actual source streams.
func TestProviderAttemptWindowOriginalJsonRestartReplays(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, false)
	raw, err := json.Marshal(fixture.candidate)
	if err != nil {
		t.Fatal(err)
	}
	var retained ProviderAttemptWindow
	if err := json.Unmarshal(raw, &retained); err != nil {
		t.Fatal(err)
	}
	first, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := VerifyProviderAttemptWindow(t.Context(), retained, fixture.options(t))
	if err != nil || reopened == nil || reopened.WindowHash != first.WindowHash {
		t.Fatalf("retained original complete census changed: %+v %v", reopened, err)
	}
}

// The original window commitment survives a later observation of finality.
func TestProviderAttemptWindowFinalityAdvanceKeepsOriginalCommitment(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, false)
	first, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	options := fixture.options(t)
	options.Window.FinalizedBlock += 10
	for hotkey, owner := range options.Validators {
		owner.Publication.Window = options.Window
		options.Validators[hotkey] = owner
	}
	later, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, options)
	if err != nil || later == nil || later.WindowHash != first.WindowHash {
		t.Fatalf("later finality renamed original window: %+v %v", later, err)
	}
}

// An independently selected owner cannot move one lane onto another fork.
func TestProviderAttemptWindowRefusesMixedTerminalFork(t *testing.T) {
	fixture := newProviderAttemptWindowTestFixture(t, false)
	options := fixture.options(t)
	hotkey := fixture.owners[1].fixture.hotkey.PublicKey()
	owner := options.Validators[hotkey]
	for noId, operator := range owner.Settlement.Operators {
		operator.Expected.Boundary.EVMBlockHash = strings.Repeat("ab", 32)
		owner.Settlement.Operators[noId] = operator
	}
	options.Validators[hotkey] = owner
	if result, err := VerifyProviderAttemptWindow(t.Context(), fixture.candidate, options); result != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("mixed original terminal fork was admitted: %+v %v", result, err)
	}
}
