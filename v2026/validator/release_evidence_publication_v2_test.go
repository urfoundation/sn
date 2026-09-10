//go:build linux || darwin

// Actual activation readers, disk ledgers, M8 trails, complete terminal folds
// and two public origins exercise publication recovery without verdict hooks.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// The fixture selects terminal context before calling the real terminal owner.
// Its transport refusal produces an actual failed trail after a signed seed.
type releasePublicationV2TestFixture struct {
	startup     *releaseStartupV2TestFixture
	closure     *AttemptSettlementClosureV2
	expected    map[uint64]AttemptCutV2Context
	readOptions ValidatorEvidencePublicationV2ReadOptions
	publication *ValidatorEvidenceCensusV2Publication
	manifest    *ValidatorEvidencePublicationV2Manifest
}

func newReleasePublicationV2TestFixture(t *testing.T, trails bool) *releasePublicationV2TestFixture {
	t.Helper()
	startup := newReleaseStartupV2TestFixture(t, true)
	startup.cfg.EvidenceV2.Bounds.Cut.Records.MaxPageBytes = 256 * 1024
	if trails {
		startup.trail(t, 0)
		server := startup.servers[0]
		startup.engines[0].transport = attemptCutV2SealTestTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
			var envelope struct {
				TrailId *connect.Id `json:"trail_id"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				return nil, err
			}
			if envelope.TrailId != nil {
				return nil, errors.New("publication fixture extension refusal")
			}
			return server.PostVerify(ctx, hop, raw)
		})
		if proof, err := startup.engines[0].RunTrail(t.Context()); err == nil || proof != nil {
			t.Fatalf("actual failed trail: %v", err)
		}
		startup.engines[0].transport = server
	}
	epoch := startup.boundary.SettlementEpoch
	boundary := AttemptBoundary{SettlementEpoch: epoch, EVMBlock: 1500 + 500*(epoch-7), EVMBlockHash: attemptHex32([32]byte{byte(epoch), 0x71})}
	self := &releasePublicationV2TestFixture{startup: startup, expected: make(map[uint64]AttemptCutV2Context), readOptions: ValidatorEvidencePublicationV2ReadOptions{
		Window:  protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: 1001 + 500*(epoch-7), EndBlock: boundary.EVMBlock + 1, FinalizedBlock: boundary.EVMBlock + 1},
		Origins: [2]string{startup.replicas[0].Origin, startup.replicas[1].Origin}, Bounds: startup.cfg.EvidenceV2.Bounds}}
	for _, input := range startup.inputs {
		expected := input.Context.InitialCut
		expected.Boundary = boundary
		self.expected[input.Config.NoID] = expected
		self.readOptions.Activations = append(self.readOptions.Activations, input.Context.Activation)
	}
	self.closure = startup.terminal(t, false)
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), self.closure, self.options(t))
	if err != nil || publication == nil {
		t.Fatalf("actual closed census publication: %v", err)
	}
	self.publication = publication
	noIds := []uint64{startup.inputs[0].Config.NoID, startup.inputs[1].Config.NoID}
	self.manifest, err = WriteValidatorEvidencePublicationV2Manifest(t.Context(), startup.cfg.StateDir, publication, noIds, self.readOptions.Bounds.MaxClosureBytes, self.readOptions.Bounds.MaxParticipants)
	if err != nil {
		t.Fatal(err)
	}
	return self
}

// Fresh scratch is explicit for each real replay; signer and expected contexts
// come from independently constructed activation and terminal geometry.
func (self *releasePublicationV2TestFixture) options(t *testing.T) ValidatorEvidenceCensusV2Options {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	bounds := self.readOptions.Bounds
	options := ValidatorEvidenceCensusV2Options{Settlement: AttemptSettlementV2Options{Operators: make(map[uint64]AttemptSettlementV2OperatorOptions),
		MaxParticipants: bounds.MaxParticipants, MaxTransitionBytes: bounds.MaxTransitionBytes, MaxClosureBytes: bounds.MaxClosureBytes},
		Window: self.readOptions.Window, Hotkey: hotkey, Replicas: self.startup.replicas, SecondReplicaScratchDirectories: make(map[uint64]string)}
	options.PrivateKeys = make(map[uint64]ed25519.PrivateKey)
	for index, input := range self.startup.inputs {
		setup := self.startup.sealOptions(t, index)
		options.PrivateKeys[input.Config.NoID] = input.PrivateKey
		options.SecondReplicaScratchDirectories[input.Config.NoID] = filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "second")
		options.Settlement.Operators[input.Config.NoID] = AttemptSettlementV2OperatorOptions{Expected: self.expected[input.Config.NoID], Policy: self.startup.cfg.Policy,
			Bounds: bounds.Cut, Measurement: AttemptCutV2MeasurementOptions{ExpectedConfig: setup.Stats.ExpectedConfig, MaxProviders: bounds.MaxProviders,
				MaxEgressHashes: bounds.MaxEgressHashes, MaxFleetPrefixes: bounds.MaxFleetPrefixes, Replay: setup.Stats.Replay}}
	}
	return options
}

func TestValidatorEvidencePublicationV2ReadsRealMixedM8AndIdleCensus(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, true)
	observed, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions)
	if err != nil || observed == nil {
		t.Fatalf("actual public metadata reader: %v", err)
	}
	var census ValidatorEvidenceCensusV2
	if err := json.Unmarshal(observed.Census, &census); err != nil {
		t.Fatal(err)
	}
	if len(census.Members) != 2 || census.Members[0].RecordCount != 10 || census.Members[0].CompleteCount != 1 || census.Members[0].FailedCount != 1 || census.Members[1].RecordCount != 0 {
		t.Fatal("public reader collapsed pending checkpoints, failed trails or idle membership")
	}
	for index, member := range observed.Members {
		if !bytes.Equal(member.SignedArtifact, fixture.publication.Members[index].SignedArtifact) || !bytes.Equal(member.Payload, fixture.publication.Members[index].Payload) || !bytes.Equal(member.Calldata, fixture.publication.Members[index].Calldata) {
			t.Fatal("public reader did not return exact published bytes")
		}
	}
}

func TestValidatorEvidencePublicationV2RestartReusesExactConsentsWithoutUploads(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, fixture.manifest.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, fixture.readOptions.Bounds.MaxClosureBytes, fixture.readOptions.Bounds.MaxParticipants)
	if err != nil {
		t.Fatal(err)
	}
	var writes [2]int
	for index, store := range fixture.startup.stores {
		_, writes[index], _ = store.snapshot()
	}
	publication, err := publishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t), retained, fixture.readOptions)
	if err != nil || publication == nil {
		t.Fatalf("restart replay and exact publication reuse: %v", err)
	}
	for index, member := range publication.Members {
		if !bytes.Equal(member.SignedArtifact, fixture.publication.Members[index].SignedArtifact) {
			t.Fatal("restart re-signed randomized hotkey consent")
		}
	}
	for index, store := range fixture.startup.stores {
		_, after, _ := store.snapshot()
		if after != writes[index] {
			t.Fatal("restart spent upload quota on already published metadata")
		}
	}
	if _, err := WriteValidatorEvidencePublicationV2Manifest(t.Context(), fixture.startup.cfg.StateDir, publication, []uint64{fixture.startup.inputs[0].Config.NoID, fixture.startup.inputs[1].Config.NoID}, fixture.readOptions.Bounds.MaxClosureBytes, fixture.readOptions.Bounds.MaxParticipants); err != nil {
		t.Fatalf("exact immutable locator retry: %v", err)
	}
}

func TestValidatorEvidencePublicationV2RefusesMissingLastConsentAtSecondOrigin(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	store := fixture.startup.stores[1]
	func() {
		store.stateLock.Lock()
		defer store.stateLock.Unlock()
		delete(store.objects, "metadata/"+attemptHex32(fixture.manifest.Members[1].SignedArtifactHash))
	}()
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions); err == nil || publication != nil {
		t.Fatal("second-origin missing last consent escaped")
	}
}

func TestValidatorEvidencePublicationV2RefusesMissingTerminalPayloadAtSecondOrigin(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	store := fixture.startup.stores[1]
	func() {
		store.stateLock.Lock()
		defer store.stateLock.Unlock()
		delete(store.objects, "metadata/"+attemptHex32(fixture.publication.Members[1].Evidence.Header.PayloadHash))
	}()
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions); err == nil || publication != nil {
		t.Fatal("second-origin missing terminal bytes escaped")
	}
}

func TestValidatorEvidencePublicationV2RefusesLocatorCensusDriftBeforeHttp(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	var reads [2]int
	for index, store := range fixture.startup.stores {
		_, _, reads[index] = store.snapshot()
	}
	for _, mutate := range []func(*ValidatorEvidencePublicationV2Manifest){
		func(value *ValidatorEvidencePublicationV2Manifest) { value.Members = value.Members[:1] },
		func(value *ValidatorEvidencePublicationV2Manifest) { value.Origins[1] = value.Origins[0] },
		func(value *ValidatorEvidencePublicationV2Manifest) { value.Epoch++ },
	} {
		candidate := *fixture.manifest
		mutate(&candidate)
		if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), &candidate, fixture.readOptions); err == nil || publication != nil {
			t.Fatal("locator changed independently expected census")
		}
	}
	for index, store := range fixture.startup.stores {
		_, _, after := store.snapshot()
		if after != reads[index] {
			t.Fatal("invalid locator performed public reads")
		}
	}
}

func TestValidatorEvidencePublicationV2RefusesChangedIndependentActivation(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	options := fixture.readOptions
	options.Activations = append([]protocol.ValidatorEvidenceActivation(nil), options.Activations...)
	options.Activations[1].NativeBlock++
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, options); err == nil || publication != nil {
		t.Fatal("public object supplied a different activation authority")
	}
}

func TestValidatorEvidencePublicationV2OverlapsAndJoinsBothPublicOrigins(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	entered := make(chan int, 2)
	release := make(chan struct{})
	for index, store := range fixture.startup.stores {
		var once sync.Once
		store.readBytes = func(kind string, raw []byte) []byte { once.Do(func() { entered <- index; <-release }); return raw }
	}
	type outcome struct {
		value *ValidatorEvidenceCensusV2Publication
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		value, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions)
		done <- outcome{value: value, err: err}
	}()
	first, second := <-entered, <-entered
	close(release)
	result := <-done
	if first == second || result.err != nil || result.value == nil {
		t.Fatalf("both origin readers were not joined: %v", result.err)
	}
}

func TestValidatorEvidencePublicationV2LateCancellationDiscardsAllCalldata(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.startup.stores[1].readBytes = func(kind string, raw []byte) []byte {
		if bytes.Equal(raw, fixture.publication.Members[1].Payload) {
			cancel()
		}
		return raw
	}
	if publication, err := ReadValidatorEvidencePublicationV2(ctx, fixture.manifest, fixture.readOptions); !errors.Is(err, context.Canceled) || publication != nil {
		t.Fatalf("canceled last payload leaked calldata: %v", err)
	}
}

func TestValidatorEvidencePublicationV2ManifestPreservesEpochZeroAndRejectsSymlink(t *testing.T) {
	root := newAttemptSettlementRuntimeV2TestStateDir(t)
	if _, err := ValidatorEvidencePublicationV2ManifestPath(root, 0); err != nil {
		t.Fatalf("protocol epoch zero was rejected: %v", err)
	}
	fixture := newReleasePublicationV2TestFixture(t, false)
	path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, fixture.manifest.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(root, "evidence-publications")
	if err := os.Mkdir(aliasRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasRoot, filepath.Base(path))
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), alias, fixture.readOptions.Bounds.MaxClosureBytes, fixture.readOptions.Bounds.MaxParticipants); err == nil || value != nil {
		t.Fatal("private locator accepted a symlink")
	}
}
