// Real signed replica publication retains every Put/Get while finite local
// quota scopes roll and close. No copied storage or authentication callback is
// used as the publication result.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/server/v2026"
)

// The existing actual archive fixture retains every observed write/read while
// advertising the same local backing instance for finite quota ownership.
func (self *streamingCampaignEvidenceStore) LocalBlobBatchSource() server.BlobStore {
	return self.BlobStore
}

// Only coordination is delegated. Staging-byte checks and all four real
// readbacks still run through the existing immutable-wire observer.
type campaignEvidenceBatchStoreTest struct {
	*campaignWireOwnerStoreTest
	contexts []context.Context
}

// The advertised source is the same local instance used by the real writes.
func (self *campaignEvidenceBatchStoreTest) LocalBlobBatchSource() server.BlobStore {
	return self.BlobStore
}

// Record actual default publisher contexts without replacing any byte checks.
func (self *campaignEvidenceBatchStoreTest) PutIfAbsent(ctx context.Context, key, source, contentType string) (bool, error) {
	self.contexts = append(self.contexts, ctx)
	return self.campaignWireOwnerStoreTest.PutIfAbsent(ctx, key, source, contentType)
}

// One shared root deliberately covers two independent operator namespaces.
type campaignEvidenceBatchFixtureTest struct {
	root      string
	stores    map[int]server.BlobStore
	observers []*campaignEvidenceBatchStoreTest
	envelope  func(int) *ReleaseEvidenceEnvelope
}

// Synthetic signed envelopes use the real canonical signer and publisher.
func newCampaignEvidenceBatchFixtureTest(t *testing.T) *campaignEvidenceBatchFixtureTest {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := buildEVMRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &campaignEvidenceBatchFixtureTest{root: t.TempDir(), stores: map[int]server.BlobStore{}}
	for index, prefix := range []string{"operator-1", "operator-2"} {
		observer := &campaignEvidenceBatchStoreTest{campaignWireOwnerStoreTest: &campaignWireOwnerStoreTest{BlobStore: server.NewLocalBlobStore(fixture.root, prefix)}}
		fixture.observers = append(fixture.observers, observer)
		fixture.stores[index+1] = observer
	}
	fixture.envelope = func(index int) *ReleaseEvidenceEnvelope {
		t.Helper()
		envelope, err := signEvidence(cfg, "unit", "batch-publication", map[string]int{"synthetic": index}, roles["testnet-owner"])
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		for _, observer := range fixture.observers {
			observer.want = wire
		}
		return envelope
	}
	return fixture
}

// Sixty-five real envelopes force a complete first window and a fresh second
// owner. Both operators share each context without bypassing one readback.
func TestCampaignEvidencePublicationBatchesRollWithExactReplicaChecks(t *testing.T) {
	fixture := newCampaignEvidenceBatchFixtureTest(t)
	var escaped func(*ReleaseEvidenceEnvelope) error
	err := withCampaignEvidencePublicationBatches(t.Context(), fixture.stores, func(publish func(*ReleaseEvidenceEnvelope) error) error {
		escaped = publish
		for index := 0; index < campaignEvidenceBatchEnvelopes+1; index++ {
			envelope := fixture.envelope(index)
			before := bytes.Clone(envelope.Payload)
			if err := publish(envelope); err != nil {
				return err
			}
			if !bytes.Equal(envelope.Payload, before) {
				return errors.New("quota scope changed the signed payload")
			}
			for _, observer := range fixture.observers {
				if observer.puts != 2*(index+1) || observer.gets != 4*(index+1) || observer.closes != observer.gets {
					return errors.New("quota scope omitted an immutable route or exact readback")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, observer := range fixture.observers {
		if len(observer.contexts) != 2*(campaignEvidenceBatchEnvelopes+1) {
			t.Fatal("actual immutable attempt census changed")
		}
		first := observer.contexts[0]
		for _, ctx := range observer.contexts[:2*campaignEvidenceBatchEnvelopes] {
			if ctx != first || ctx == t.Context() {
				t.Fatal("publication did not retain one finite quota owner")
			}
		}
		next := observer.contexts[2*campaignEvidenceBatchEnvelopes]
		if next == first || observer.contexts[len(observer.contexts)-1] != next {
			t.Fatal("publication did not roll finite quota ownership")
		}
	}
	if fixture.observers[0].contexts[0] != fixture.observers[1].contexts[0] {
		t.Fatal("operator aliases acquired competing root contexts")
	}
	puts := fixture.observers[0].puts
	if err := escaped(fixture.envelope(99)); err == nil || !strings.Contains(err.Error(), "scope is closed") || fixture.observers[0].puts != puts {
		t.Fatal("escaped publication callback reopened a closed quota owner", err)
	}
}

// The entire archive remains failed if its final fresh accounting detects an
// external write, even after every actual signature and readback has passed.
func TestCampaignEvidencePublicationBatchesRetainFinalCensusFailure(t *testing.T) {
	fixture := newCampaignEvidenceBatchFixtureTest(t)
	fixture.observers[0].afterFirstPut = func() {
		if err := os.WriteFile(filepath.Join(fixture.root, "unaccounted.json"), []byte("synthetic"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bodyFailure := errors.New("synthetic archive body failure")
	err := withCampaignEvidencePublicationBatches(t.Context(), fixture.stores, func(publish func(*ReleaseEvidenceEnvelope) error) error {
		if err := publish(fixture.envelope(1)); err != nil {
			return err
		}
		return bodyFailure
	})
	if !errors.Is(err, bodyFailure) || !strings.Contains(err.Error(), "unaccounted bytes") {
		t.Fatal("publication lost body or final accounting failure", err)
	}
	for _, observer := range fixture.observers {
		if observer.puts != 2 || observer.gets != 4 || observer.closes != 4 {
			t.Fatal("failure control skipped real immutable readback")
		}
	}
	// A failed final check still releases its physical root owner.
	source := filepath.Join(t.TempDir(), "source.json")
	if err := os.WriteFile(source, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if created, err := server.NewLocalBlobStore(fixture.root, "later").PutIfAbsent(t.Context(), "later/object.json", source, "application/json"); err != nil || !created {
		t.Fatal("failed batch retained its physical lock", created, err)
	}
}

// The real archive entrypoint must not return its otherwise genuine manifest
// when final quota reconciliation fails after the last independent readback.
func TestCampaignEvidencePublicationBatchesFinalFailureReturnsNoManifest(t *testing.T) {
	fixture := newStreamingCampaignEvidenceFixture(t)
	changed := false
	manifestReads := 0
	fixture.stores[2].afterRead = func(operator int, name string, history bool) {
		if name != campaignEvidenceManifestKind || !history {
			return
		}
		manifestReads++
		if manifestReads == 2 {
			if err := os.WriteFile(filepath.Join(fixture.stateDir, "object-store", "unaccounted.json"), []byte("synthetic"), 0o600); err != nil {
				t.Fatal(err)
			}
			changed = true
		}
	}
	manifest, err := fixture.publish(t.Context())
	if !changed || manifest != nil || err == nil || !strings.Contains(err.Error(), "unaccounted bytes") {
		t.Fatalf("actual archive returned success after final census failure: changed=%v manifest=%v err=%v", changed, manifest, err)
	}
	for operator := 1; operator <= 2; operator++ {
		reads := 0
		for _, event := range fixture.events {
			if event == fmt.Sprintf("read/%d/%s", operator, campaignEvidenceManifestKind) {
				reads++
			}
		}
		if reads != 4 {
			t.Fatalf("final census control skipped an actual manifest readback: operator=%d reads=%d", operator, reads)
		}
	}
	// The returned error does not poison a later fresh immutable retry. Its
	// new initial census independently charges the previously untracked file.
	fixture.stores[2].afterRead = nil
	if manifest, err := fixture.publish(t.Context()); err != nil || manifest == nil {
		t.Fatal("failed final census retained a lock or stale usage", manifest, err)
	}
}

// Ignoring a publisher error cannot produce a successful body result or permit
// more writes; a closed callback cannot create its first lease after return.
func TestCampaignEvidencePublicationBatchesKeepFailureSticky(t *testing.T) {
	fixture := newCampaignEvidenceBatchFixtureTest(t)
	continued := false
	err := withCampaignEvidencePublicationBatches(t.Context(), fixture.stores, func(publish func(*ReleaseEvidenceEnvelope) error) error {
		first := publish(nil)
		if first == nil {
			return errors.New("missing envelope was accepted")
		}
		continued = true
		if later := publish(fixture.envelope(1)); later == nil {
			return errors.New("failed publication resumed")
		}
		return nil
	})
	if err == nil || !continued {
		t.Fatal("ignored publication failure became a successful scope", err)
	}
	for _, observer := range fixture.observers {
		if observer.puts != 0 || observer.gets != 0 {
			t.Fatal("failed envelope performed a replica operation")
		}
	}
	var escaped func(*ReleaseEvidenceEnvelope) error
	if err := withCampaignEvidencePublicationBatches(t.Context(), fixture.stores, func(publish func(*ReleaseEvidenceEnvelope) error) error { escaped = publish; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := escaped(fixture.envelope(2)); err == nil || !strings.Contains(err.Error(), "scope is closed") {
		t.Fatal("empty closed scope admitted its first publication", err)
	}
}

// Invalid owners and preexisting cancellation are refused before body work.
func TestCampaignEvidencePublicationBatchesRejectUnownedAdmission(t *testing.T) {
	fixture := newCampaignEvidenceBatchFixtureTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, input := range []struct {
		ctx    context.Context
		stores map[int]server.BlobStore
	}{
		{ctx: nil, stores: fixture.stores},
		{ctx: t.Context(), stores: nil},
		{ctx: t.Context(), stores: map[int]server.BlobStore{2: fixture.stores[1]}},
		{ctx: ctx, stores: fixture.stores},
	} {
		called := false
		if err := withCampaignEvidencePublicationBatches(input.ctx, input.stores, func(func(*ReleaseEvidenceEnvelope) error) error { called = true; return nil }); err == nil || called {
			t.Fatal("unowned scope began its body", err, called)
		}
	}
}
