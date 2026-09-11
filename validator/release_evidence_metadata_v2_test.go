//go:build linux || darwin

package validator

// Real M8 histories produce terminal metadata beyond stream-page capacity.
// Publication keeps source signatures, live HTTP owners and both public copies.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

func TestReleaseRuntimeV2EvidenceMetadataLargerThanStreamPages(t *testing.T) {
	bounds := releaseEvidenceV2TestConfig("", []OperatorConfig{{NoID: 1}, {NoID: 2}}).Bounds
	bounds.Cut.MaxHeaderBytes = 64 * 1024
	bounds.Cut.Records.MaxPageBytes, bounds.Cut.Proofs.MaxPageBytes = 64*1024, 64*1024
	bounds.MaxTransitionBytes, bounds.MaxClosureBytes = 4*1024*1024, 12*1024*1024
	bounds.MaxProviders, bounds.MaxEgressHashes, bounds.MaxFleetPrefixes = 1000, 1000, 1000
	bounds.Disk.MaxRecordCount, bounds.Disk.MaxTrailCount = 1024, 128
	bounds.Cut.Records.MaxItems, bounds.Cut.Proofs.MaxItems, bounds.Replay.MaxTrails = 1024, 128, 128
	fixture := newReleaseRuntimeV2TestFixtureWithBounds(t, &bounds)
	for range 35 {
		// Each M8 trail visits all eight newly configured synthetic providers.
		// Random hop order cannot change the complete 245-provider measurement:
		// every trail excludes its validator-chosen seed from statistics.
		server := fixture.startup.servers[0]
		server.providers = make([]connect.Id, 8)
		for index := range server.providers {
			server.providers[index] = connect.NewId()
		}
		fixture.startup.trail(t, 0)
	}
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatalf("real large terminal census publication: %v", err)
	}
	path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, 7)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, bounds.MaxClosureBytes, bounds.MaxParticipants)
	if err != nil {
		t.Fatal(err)
	}
	options := ValidatorEvidencePublicationV2ReadOptions{Origins: fixture.origins, Bounds: bounds,
		Window: protocol.ValidatorEvidenceWindow{Epoch: 7, StartBlock: 1001, EndBlock: 1501, FinalizedBlock: 2001}}
	for _, input := range fixture.startup.inputs {
		options.Activations = append(options.Activations, input.Context.Activation)
	}
	publication, err := ReadValidatorEvidencePublicationV2(t.Context(), manifest, options)
	if err != nil || publication == nil || len(publication.Members) != 2 {
		t.Fatalf("both large public copies: %v", err)
	}
	member := publication.Members[0]
	if len(member.Payload) <= 64*1024 || uint64(len(member.Payload)) >= bounds.MaxTransitionBytes {
		t.Fatalf("real transition did not reproduce the page boundary: %d bytes", len(member.Payload))
	}
	closure := fixture.runtime.history.terminals[7]
	if len(closure.Transitions[0].PreFold.Providers) != 245 || closure.Transitions[0].Cut.CompleteCount != 35 || closure.Transitions[0].Cut.FailedCount != 0 {
		t.Fatalf("real census providers=%d complete=%d failed=%d", len(closure.Transitions[0].PreFold.Providers), closure.Transitions[0].Cut.CompleteCount, closure.Transitions[0].Cut.FailedCount)
	}
	for _, store := range fixture.stores {
		store.stateLock.Lock()
		stored := bytes.Clone(store.objects["metadata/"+attemptHex32(sha256.Sum256(member.Payload))])
		posts := store.postsBySource[member.Evidence.Header.NoID]
		store.stateLock.Unlock()
		if !bytes.Equal(stored, member.Payload) || posts == 0 {
			t.Fatal("one real HTTP owner omitted or changed the large signed source payload")
		}
	}
	if fixture.startup.cfg.EvidenceV2.Bounds != bounds {
		t.Fatal("publication changed the independently supplied page or payload bounds")
	}
	// The publisher must reject the same valid transition under a smaller
	// independent payload allowance before any new public read or write.
	publicationFixture := &releasePublicationV2TestFixture{startup: fixture.startup, closure: closure,
		expected: fixture.runtime.history.terminalContexts[7], readOptions: options}
	publishOptions := publicationFixture.options(t)
	publishOptions.Settlement.MaxTransitionBytes = uint64(len(member.Payload) - 1)
	for index := range publishOptions.Replicas {
		publishOptions.Replicas[index].WriteMetadata = func(_ context.Context, _ string, _ []byte) error {
			t.Error("over-budget terminal reached publication")
			return errors.New("unexpected over-budget publication")
		}
	}
	before := [2]int{}
	for index, store := range fixture.startup.stores {
		_, _, before[index] = store.snapshot()
	}
	if value, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), closure, publishOptions); err == nil || value != nil {
		t.Fatal("publisher admitted an over-budget valid terminal")
	}
	for index, store := range fixture.startup.stores {
		_, _, reads := store.snapshot()
		if reads != before[index] {
			t.Fatal("over-budget terminal reached public replay")
		}
	}
	t.Logf("real terminal payload=%d bytes, page limit=%d, transition limit=%d, complete M8 trails=%d", len(member.Payload), bounds.Cut.Records.MaxPageBytes, bounds.MaxTransitionBytes, closure.Transitions[0].Cut.CompleteCount)
}

func TestValidatorEvidenceDepositAuditV2MetadataLargerThanStreamPages(t *testing.T) {
	bounds := releaseEvidenceV2TestConfig("", []OperatorConfig{{NoID: 1}, {NoID: 2}}).Bounds
	bounds.Cut.MaxHeaderBytes = 64 * 1024
	bounds.Cut.Records.MaxPageBytes, bounds.Cut.Proofs.MaxPageBytes = 64*1024, 64*1024
	bounds.MaxTransitionBytes, bounds.MaxProviders = 4*1024*1024, 1000
	// Source replay and payload construction each retain the original signed
	// observation. Admit both copies before constructing the actual owners.
	bounds.MaxHistoryBytes = 8 * 1024 * 1024
	fixture := newDepositAuditPublicationV2TestFixtureWithBounds(t, "positive", &bounds, 300)
	runtime := fixture.base.runtime
	reader, err := NewHTTPArtifactReader(runtime.cfg.Operators[0].APIURL, runtime.cfg.DeploymentID, runtime.cfg.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	_, observationPath, err := releaseArtifactHttpRequestV2(&runtime.cfg, fixture.base.hotkey.PublicKey(), fixture.artifact, fixture.sourceNoIds[0], fixture.options.Window.Epoch, reader)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := os.Lstat(observationPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.payout.Providers) != 300 || !observation.Mode().IsRegular() || observation.Mode().Perm() != 0o600 || observation.Size()*2 <= 1024*1024 || uint64(observation.Size()*2) > bounds.MaxHistoryBytes {
		t.Fatalf("actual signed source custody providers=%d bytes=%d mode=%v history limit=%d", len(fixture.payout.Providers), observation.Size(), observation.Mode(), bounds.MaxHistoryBytes)
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil || publication == nil || len(publication.Members) != 2 {
		t.Fatalf("large deposit public census: %v", err)
	}
	member := publication.Members[0]
	if len(member.Payload) <= 64*1024 || uint64(len(member.Payload)) >= bounds.MaxTransitionBytes {
		t.Fatalf("actual signed payout observation missed the metadata boundary: %d bytes", len(member.Payload))
	}
	for _, store := range fixture.base.stores {
		store.stateLock.Lock()
		stored := bytes.Clone(store.objects["metadata/"+attemptHex32(sha256.Sum256(member.Payload))])
		store.stateLock.Unlock()
		if !bytes.Equal(stored, member.Payload) {
			t.Fatal("one HTTP replica changed or omitted the original large deposit observation")
		}
	}
	if fixture.options.Bounds.Cut != bounds.Cut || fixture.options.Bounds.MaxTransitionBytes != bounds.MaxTransitionBytes {
		t.Fatal("deposit publication changed the approved stream or transition limits")
	}
	changed := fixture.options
	changed.Bounds.MaxTransitionBytes = uint64(len(member.Payload) - 1)
	if value, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, changed); err == nil || value != nil {
		t.Fatal("deposit reader admitted a payload above its independent allowance")
	}
	t.Logf("actual deposit payload=%d bytes, original signed observation=%d bytes, page limit=%d, transition limit=%d", len(member.Payload), observation.Size(), bounds.Cut.Records.MaxPageBytes, bounds.MaxTransitionBytes)
}
