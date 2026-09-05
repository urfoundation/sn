//go:build linux || darwin

package validator

// Sealing fixtures use complete production trails, the real disk ledger and
// actual writer/replay codecs. Only immutable object transport is in memory;
// no test replaces cryptographic, lifecycle, persistence or policy verdicts.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// Fixtures retain histories only for independent assertions, never in the
// production ledger or sealer. The release policy's real reliability minimum
// is unchanged, including tests with a separately authenticated legal depth.
type attemptCutV2SealTestFixture struct {
	engine   *TrailEngine
	ledger   *AttemptLedger
	server   *mockVerifyServer
	key      ed25519.PrivateKey
	policy   protocol.Policy
	expected AttemptCutV2Context
	recordTs []AttemptRecord
	bounds   AttemptCutV2Bounds
	replay   AttemptCutV2ReplayBounds
}

// A transport seam forwards genuine signed seed responses and can refuse
// actual extension I/O to produce a real authenticated failed terminal.
type attemptCutV2SealTestTransport func(context.Context, connect.Id, []byte) ([]byte, error)

// The synchronous adapter never substitutes a proof or an acceptance result.
func (self attemptCutV2SealTestTransport) PostVerify(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
	return self(ctx, hop, raw)
}

// Disk startup, stats attachment and every trail use actual production APIs.
// Policy copies change only their explicitly authenticated protocol depth;
// M8 remains the real release-policy control and no minimum is reduced.
func newAttemptCutV2SealTestFixture(t *testing.T, depth, completed, failed int) *attemptCutV2SealTestFixture {
	t.Helper()
	policy := exactPolicy(t)
	if policy.Verify.TrailDepth != 8 {
		t.Fatalf("release policy depth = %d, want the existing M8 policy", policy.Verify.TrailDepth)
	}
	policy.Verify.TrailDepth = depth
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	server, key, clientID := newMockVerifyServer(t, 16)
	state := newAttemptLedgerDiskTestStateDir(t)
	stats := NewStatsEngine(StatsConfig{AMin: policy.Verify.ReliabilityAMin})
	if err := stats.AdvanceSettlementEpoch(42, state); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewDiskAttemptLedger(context.Background(), state, AttemptLedgerIdentity{DeploymentID: "attempt-cut-v2-sealer-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}), Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 9}, attemptLedgerDiskTestCoordinator, key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	if err := stats.AttachAttemptLedger(ledger, state); err != nil {
		t.Fatal(err)
	}
	store, err := NewProofStore(state)
	if err != nil {
		t.Fatal(err)
	}
	boundary := attemptLedgerTestBoundary()
	engine := NewTrailEngine(clientID, key, server, NewStaticServerKeyRing(server.serverPublicKeys()), func(context.Context) (connect.Id, error) {
		return server.providers[0], nil
	}, stats, store, func() uint64 { return 42 }, TrailEngineConfig{M: depth, StepTimeout: 2 * time.Second, ExtendAttempts: 3, Pace: time.Millisecond, AttemptLedger: ledger, AttemptBoundaryResolver: func(_ context.Context, pinned *AttemptBoundary, clients []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
		if pinned != nil && *pinned != boundary {
			return AttemptBoundary{}, nil, errors.New("fixture boundary pin changed")
		}
		bindings := make([]AttemptBinding, len(clients))
		for index, client := range clients {
			bindings[index] = attemptLedgerTestBinding(client, 1)
		}
		return boundary, bindings, nil
	}})
	for index := 0; index < completed; index++ {
		proof, err := engine.RunTrail(context.Background())
		if err != nil || proof == nil || proof.M != depth || len(proof.Hops) != depth {
			t.Fatalf("real M%d complete trail = %+v: %v", depth, proof, err)
		}
	}
	engine.transport = attemptCutV2SealTestTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
		var envelope struct {
			TrailID *connect.Id `json:"trail_id"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, err
		}
		if envelope.TrailID != nil {
			return nil, errors.New("fixture extension transport failure")
		}
		return server.PostVerify(ctx, hop, raw)
	})
	for index := 0; index < failed; index++ {
		proof, err := engine.RunTrail(context.Background())
		if err == nil || proof != nil {
			t.Fatalf("real M%d failed trail returned proof=%v error=%v", depth, proof, err)
		}
	}
	engine.transport = server
	head, err := ledger.Head()
	if err != nil || head.LastSequence != uint64(completed*depth+failed*2) {
		t.Fatalf("real durable census %+v: %v", head, err)
	}
	var recordTs []AttemptRecord
	if head.LastSequence != 0 {
		if err := ledger.Walk(context.Background(), 1, head.LastSequence, func(record AttemptRecord) error {
			if err := VerifyAttemptRecord(&record, ledger.identity, key.Public().(ed25519.PublicKey), server.serverPublicKeys()); err != nil {
				return err
			}
			recordTs = append(recordTs, record)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if ledger.records != nil || ledger.pending != nil || ledger.terminal != nil {
		t.Fatal("production disk ledger retained full-history maps")
	}
	bounds, replay := attemptReplayV2TestBounds()
	bounds.Records.MaxChunkBytes = 32 * 1024
	bounds.Proofs.MaxChunkBytes = 32 * 1024
	domain := protocol.ValidatorEvidenceDomain{ChainID: ledger.identity.ChainID, GenesisHash: [32]byte{4}, Netuid: ledger.identity.Netuid, Coordinator: [20]byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11}, SettlementVault: [20]byte{0x12}, DeploymentIDHash: sha256.Sum256([]byte(ledger.identity.DeploymentID)), PolicyHash: policyHash, ActivationEpoch: 42, ActivationHash: [32]byte{0x14}}
	expected := AttemptCutV2Context{Identity: ledger.identity, Activation: AttemptCutV2Activation{Domain: domain, Hotkey: [32]byte{0x15}, FirstSequence: 1, PriorRoot: zeroAttemptHash()}, Boundary: boundary, FirstSequence: 1, EgressFirstSequence: 1, EgressGeneration: 1, PriorRoot: zeroAttemptHash()}
	if err := expected.Validate(); err != nil {
		t.Fatal(err)
	}
	return &attemptCutV2SealTestFixture{engine: engine, ledger: ledger, server: server, key: key, policy: policy, expected: expected, recordTs: recordTs, bounds: bounds, replay: replay}
}

// Separate typed data namespaces prevent a same-hash object from being
// mistaken for another stream. Callbacks own their returned/staged bytes.
type attemptCutV2SealTestObjects struct {
	metadataKVs map[string][]byte
	dataKVs     map[string]map[string][]byte
	writes      int
	reads       int
	closes      int
}

// Each operation gets a fresh private scratch name and object namespace.
func newAttemptCutV2SealTestOptions(t *testing.T, fixture *attemptCutV2SealTestFixture) (AttemptCutV2SealOptions, *attemptCutV2SealTestObjects) {
	t.Helper()
	objects := &attemptCutV2SealTestObjects{metadataKVs: map[string][]byte{}, dataKVs: map[string]map[string][]byte{AttemptStreamV2Records: {}, AttemptStreamV2Proofs: {}}}
	write := func(kind string) AttemptStreamV2ObjectWriter {
		return func(ctx context.Context, hash string, raw []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if hash != attemptHex32(sha256.Sum256(raw)) {
				return errors.New("staged object hash differs from its owned bytes")
			}
			objects.writes++
			values := objects.metadataKVs
			if kind != "metadata" {
				values = objects.dataKVs[kind]
			}
			if previous, exists := values[hash]; exists && !bytes.Equal(previous, raw) {
				return errors.New("immutable staged object changed")
			}
			values[hash] = bytes.Clone(raw)
			return nil
		}
	}
	return AttemptCutV2SealOptions{ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal-scratch"), ServerKeys: fixture.server.serverPublicKeys(), WriteRecords: write(AttemptStreamV2Records), WriteProofs: write(AttemptStreamV2Proofs), WriteMetadata: write("metadata"), ReadMetadata: func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		objects.reads++
		raw, exists := objects.metadataKVs[hash]
		if !exists || uint64(len(raw)) != size {
			return nil, errors.New("staged metadata is missing or resized")
		}
		return bytes.Clone(raw), nil
	}, OpenData: func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		objects.reads++
		raw, exists := objects.dataKVs[kind][hash]
		if !exists || uint64(len(raw)) != size {
			return nil, errors.New("staged typed data is missing or resized")
		}
		return &attemptCutV2SealTestReader{Reader: bytes.NewReader(bytes.Clone(raw)), close: func() error { objects.closes++; return nil }}, nil
	}}, objects
}

// Close controls observe actual stream ownership without replacing replay.
type attemptCutV2SealTestReader struct {
	*bytes.Reader
	close func() error
}

// Every opened object must be closed, including data returned with an error.
func (self *attemptCutV2SealTestReader) Close() error { return self.close() }

// An independently authenticated header must replay its exact complete input
// census through the public policy-aware API, not a producer-only shortcut.
func assertAttemptCutV2SealTestSuccess(t *testing.T, fixture *attemptCutV2SealTestFixture, options AttemptCutV2SealOptions, cut *AttemptCutV2, verified AttemptCutV2ReplayResult, recordTs []AttemptRecord, complete, failed uint64) {
	t.Helper()
	if cut == nil || len(cut.Signature) != ed25519.SignatureSize || verified.Records.ItemCount != uint64(len(recordTs)) || verified.CompleteCount != complete || verified.FailedCount != failed || verified.Proofs.ItemCount != complete || verified.TrailCount != complete+failed {
		t.Fatalf("sealed census cut=%+v verified=%+v records=%d", cut, verified, len(recordTs))
	}
	raw, err := cut.CanonicalJSON(fixture.bounds)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAttemptCutV2(raw, cut.Context, fixture.bounds)
	if err != nil || decoded == nil || !reflect.DeepEqual(decoded, cut) {
		t.Fatalf("public signed header decode=%+v: %v", decoded, err)
	}
	visited := 0
	replay, err := ReplayAttemptCutV2WithPolicy(context.Background(), *decoded, cut.Context, fixture.policy, fixture.bounds, AttemptCutV2ReplayOptions{Bounds: options.ReplayBounds, ScratchDirectory: filepath.Join(t.TempDir(), "public-replay"), ServerKeys: options.ServerKeys, ReadMetadata: options.ReadMetadata, OpenData: options.OpenData, VisitRecord: func(record AttemptRecord) error {
		if visited >= len(recordTs) || !reflect.DeepEqual(record, recordTs[visited]) {
			return fmt.Errorf("public record %d differs from actual disk-ledger input", visited)
		}
		visited++
		return nil
	}})
	if err != nil || replay != verified || visited != len(recordTs) {
		t.Fatalf("independent policy replay=%+v visits=%d: %v", replay, visited, err)
	}
}

// Two real full-depth trails traverse multiple bounded chunks and pages.
func TestAttemptCutV2SealRealM8DiskWriterPublicReplay(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 2, 0)
	if verified.Records.ChunkCount < 3 || verified.Records.PageCount < 2 || objects.reads == 0 || objects.closes != len(objects.dataKVs[AttemptStreamV2Records])*2+len(objects.dataKVs[AttemptStreamV2Proofs])*2 {
		t.Fatalf("bounded multipage fetch-back census=%+v reads=%d closes=%d", verified, objects.reads, objects.closes)
	}
	for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		name := "records.descriptors"
		count := verified.Records.ChunkCount
		if kind == AttemptStreamV2Proofs {
			name, count = "proofs.descriptors", verified.Proofs.ChunkCount
		}
		info, err := os.Lstat(filepath.Join(options.ScratchDirectory, name))
		if err != nil || !attemptStorePrivateFile(info) || uint64(info.Size()) != count*attemptStreamV2DescriptorBytes {
			t.Fatalf("exact private %s spool: %v", kind, err)
		}
	}
}

// Failed signed attempts remain in the record census but produce no proof.
func TestAttemptCutV2SealRealFailedTerminal(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 0, 1)
	if len(objects.dataKVs[AttemptStreamV2Proofs]) != 0 || cut.Proofs.ManifestHash != zeroAttemptHash() || fixture.recordTs[1].Disposition == AttemptDispositionPending || fixture.recordTs[1].Disposition == AttemptDispositionComplete {
		t.Fatal("failed trail fabricated a complete proof or lost its terminal")
	}
}

// A failed neighbor cannot be omitted from a complete trail's signed cut.
func TestAttemptCutV2SealMixedCompleteAndFailedTerminals(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 1)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 1, 1)
}

// Only an authenticated checked empty prefix may sign without object I/O.
func TestAttemptCutV2SealCheckedEmptyPrefix(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, nil, 0, 0)
	if objects.writes != 0 || objects.reads != 0 || cut.Root != zeroAttemptHash() {
		t.Fatal("empty prefix touched object storage or invented a root")
	}
	if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty prefix created scratch: %v", err)
	}
}

// A second actual trail appended after Head is outside both source passes.
func TestAttemptCutV2SealHeadSnapshotExcludesLaterAppends(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	var captured AttemptLedgerHead
	cut, verified, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{HeadCaptured: func(head AttemptLedgerHead) error {
		captured = head
		_, err := fixture.engine.RunTrail(context.Background())
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	head, err := fixture.ledger.Head()
	if err != nil || captured.LastSequence != 8 || head.LastSequence != 16 || cut.LastSequence != captured.LastSequence || cut.Root != captured.Root || head.Root == cut.Root {
		t.Fatalf("fixed prefix captured=%+v actual=%+v cut=%+v: %v", captured, head, cut, err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 1, 0)
}

// Real callbacks may inspect Head and append without an append/state mutex
// being held across external I/O; their later records remain outside the cut.
func TestAttemptCutV2SealObjectCallbackCanAppend(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	ledger, err := NewDiskAttemptLedger(context.Background(), newAttemptLedgerDiskTestStateDir(t), fixture.expected.Identity, attemptLedgerDiskTestCoordinator, fixture.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	for _, record := range fixture.recordTs[:8] {
		if _, err := ledger.AppendContext(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	write, appended := options.WriteRecords, false
	options.WriteRecords = func(ctx context.Context, hash string, raw []byte) error {
		if _, err := ledger.Head(); err != nil {
			return err
		}
		if !appended {
			appended = true
			for _, record := range fixture.recordTs[8:] {
				if _, err := ledger.AppendContext(ctx, record); err != nil {
					return err
				}
			}
		}
		return write(ctx, hash, raw)
	}
	cut, verified, err := SealAttemptCutV2(context.Background(), ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil || !appended {
		t.Fatalf("callback append=%t: %v", appended, err)
	}
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 16 || cut.LastSequence != 8 {
		t.Fatalf("callback append crossed captured prefix: head=%+v cut=%+v error=%v", head, cut, err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs[:8], 1, 0)
}

// A later cut begins at the exact previously signed root, not sequence one.
func TestAttemptCutV2SealExactNoninitialPrefix(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	fixture.expected.FirstSequence, fixture.expected.EgressFirstSequence, fixture.expected.EgressGeneration = 9, 9, 2
	fixture.expected.PriorRoot = fixture.recordTs[7].RecordHash
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs[8:], 1, 0)
}

// A noninitial empty cut must keep the actual durable predecessor root.
func TestAttemptCutV2SealNoninitialEmptyPrefix(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	fixture.expected.FirstSequence, fixture.expected.EgressFirstSequence, fixture.expected.EgressGeneration = 9, 9, 2
	fixture.expected.PriorRoot = fixture.recordTs[7].RecordHash
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, nil, 0, 0)
	if cut.LastSequence != 8 || cut.Root != fixture.expected.PriorRoot || objects.writes != 0 || objects.reads != 0 {
		t.Fatal("noninitial empty cut changed its durable head")
	}
}

// Same-height hashes must agree; earlier records remain valid at a later
// authenticated boundary in the same semantic epoch and cursor generation.
func TestAttemptCutV2SealBoundaryControls(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, later := range []bool{false, true} {
		expected := fixture.expected
		if later {
			expected.Boundary.EVMBlock++
			expected.Boundary.EVMBlockHash = attemptHex32([32]byte{0x39})
		}
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, expected, fixture.policy, fixture.key, fixture.bounds, options)
		if err != nil {
			t.Fatalf("later=%t: %v", later, err)
		}
		assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 1, 0)
	}
}

// Namespace, cursor and boundary contradictions must never yield a header.
func TestAttemptCutV2SealRejectsExpectedContextContradictions(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	for _, variation := range []string{"identity", "coordinator", "future-first", "future-egress", "prior-root", "empty-prior", "epoch", "backwards-height", "same-height-hash"} {
		expected := fixture.expected
		switch variation {
		case "identity":
			expected.Identity.ValidatorUID++
		case "coordinator":
			expected.Activation.Domain.Coordinator[0]++
		case "future-first":
			expected.FirstSequence, expected.EgressFirstSequence = 18, 18
			expected.PriorRoot = fixture.recordTs[15].RecordHash
		case "future-egress":
			expected.EgressFirstSequence = 18
		case "prior-root":
			expected.FirstSequence, expected.EgressFirstSequence = 9, 9
			expected.PriorRoot = attemptHex32([32]byte{0x77})
		case "empty-prior":
			expected.FirstSequence, expected.EgressFirstSequence = 17, 17
			expected.PriorRoot = attemptHex32([32]byte{0x77})
		case "epoch":
			expected.Boundary.SettlementEpoch++
		case "backwards-height":
			expected.Boundary.EVMBlock--
		case "same-height-hash":
			expected.Boundary.EVMBlockHash = attemptHex32([32]byte{0x77})
		}
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, expected, fixture.policy, fixture.key, fixture.bounds, options)
		if err == nil || cut != nil || verified != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("%s contradiction yielded cut=%+v result=%+v error=%v", variation, cut, verified, err)
		}
	}
}

// Checked Head refuses closed ownership; in-memory legacy history cannot be
// silently substituted for the bounded disk source required by this API.
func TestAttemptCutV2SealRejectsUnavailableOrLegacyLedger(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	if err := fixture.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	legacy, err := NewAttemptLedger(newAttemptLedgerDiskTestStateDir(t), fixture.expected.Identity, fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	for _, ledger := range []*AttemptLedger{nil, fixture.ledger, legacy} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		cut, verified, err := SealAttemptCutV2(context.Background(), ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		if err == nil || cut != nil || verified != (AttemptCutV2ReplayResult{}) || objects.writes != 0 || objects.reads != 0 {
			t.Fatalf("unavailable/legacy source yielded cut=%+v result=%+v error=%v", cut, verified, err)
		}
	}
}

// A malformed signing key is rejected before creating or staging anything.
func TestAttemptCutV2SealRejectsWrongPrivateKeyBeforeIO(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	for _, variation := range []string{"short", "other", "split"} {
		key := bytes.Clone(fixture.key)
		switch variation {
		case "short":
			key = key[:32]
		case "other":
			key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, ed25519.SeedSize))
		case "split":
			key[0] ^= 1
		}
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, key, fixture.bounds, options)
		if err == nil || cut != nil || verified != (AttemptCutV2ReplayResult{}) || objects.writes != 0 || objects.reads != 0 || !strings.Contains(err.Error(), "private key") {
			t.Fatalf("%s signing key yielded cut=%+v result=%+v error=%v", variation, cut, verified, err)
		}
	}
}
