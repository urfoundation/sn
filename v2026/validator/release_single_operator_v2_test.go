//go:build linux || darwin

// SN25 launches with one network operator whose own server holds the only
// public evidence replica. These controls run the real startup, upload,
// census, publication and archive owners against that census, and pin every
// two-operator wire encoding to the fixed-pair bytes.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// One configured operator holds the only public evidence replica. The policy
// admits one healthy operator and one live validator, with the full-vector
// weight cap a pool-only testnet row then requires, before any activation is
// signed over its hash.
func releaseSingleOperatorV2TestConfig(t *testing.T) func(*ReleaseConfig) {
	t.Helper()
	return func(cfg *ReleaseConfig) {
		cfg.Operators = slices.Clone(cfg.Operators[:1])
		cfg.Policy.Safety.MinimumHealthyNOCount, cfg.Policy.Safety.MinimumLiveValidatorCount = 1, 1
		cfg.Policy.Steering.MaxWeightLimitU16 = ^uint16(0)
		hash, err := cfg.Policy.HashHex()
		if err != nil {
			t.Fatal(err)
		}
		cfg.PolicyHash = hash
	}
}

// The steady mainnet cadence used by the protocol package's own policy tests,
// with the launch census of one operator and one validator.
func releaseSingleOperatorMainnetTestPolicy(t *testing.T) protocol.Policy {
	t.Helper()
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	policy.NetworkProfile, policy.EffectiveEpoch = "mainnet", 0
	policy.ProductionCadence = protocol.CadenceSnapshot{AfterAcceleratedEpochs: 0, EpochBlocks: 50_400, RootCommitWindowBlocks: 8_400, FinalizeOffsetBlocks: 25_200, CloseGraceBlocks: 840}
	policy.Settlement.EpochBlocks, policy.Settlement.RootCommitWindowBlocks = 50_400, 8_400
	policy.Settlement.FinalizeOffsetBlocks, policy.Settlement.CloseGraceBlocks = 25_200, 840
	policy.Steering.MaxWeightLimitU16 = 32768
	policy.Safety.MinimumHealthyNOCount, policy.Safety.MinimumLiveValidatorCount = 1, 1
	return *policy
}

// The launch configuration loads with its only operator and that operator's
// API origin is the complete replica census. It cannot mask that operator.
func TestReleaseConfigLoadsSingleOperatorMainnetPolicy(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.Policy = releaseSingleOperatorMainnetTestPolicy(t)
	hash, err := cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	cfg.PolicyHash, cfg.TrailDepth = hash, cfg.Policy.Verify.TrailDepth
	cfg.Operators = cfg.Operators[:1]
	cfg.EvidenceV2 = releaseEvidenceV2TestConfig(filepath.Dir(cfg.StateDir), cfg.Operators)
	loaded, err := LoadReleaseConfig(writeReleaseConfig(t, cfg))
	if err != nil {
		t.Fatalf("one-operator mainnet configuration was refused: %v", err)
	}
	if len(loaded.Operators) != 1 || len(loaded.EvidenceV2.Operators) != 1 || len(loaded.ControlledNOIDs) != 0 || loaded.Policy.NetworkProfile != "mainnet" {
		t.Fatalf("one-operator mainnet configuration changed its census: %+v", loaded.Operators)
	}
	origins, err := releaseEvidenceV2ConfiguredOrigins(loaded)
	if err != nil || !slices.Equal(origins, []string{loaded.Operators[0].APIURL}) {
		t.Fatalf("single operator is not the only public replica: %v %v", origins, err)
	}
	readers, err := newReleaseEvidenceV2StartupReaders(origins, loaded.EvidenceV2.Bounds.Cut)
	if err != nil || len(readers) != 1 {
		t.Fatalf("single public origin has no startup reader: %v", err)
	}
	cfg.ControlledNOIDs = []uint64{cfg.Operators[0].NoID}
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil || !strings.Contains(err.Error(), "covers every configured operator") {
		t.Fatalf("configuration masked its only operator: %v", err)
	}
}

// Two configured operators keep their prior pair; a third still replicates
// to the first two. No configured census admits zero origins.
func TestReleaseEvidenceV2ConfiguredOriginsKeepTwoOperatorPair(t *testing.T) {
	cfg := validReleaseConfig(t)
	origins, err := releaseEvidenceV2ConfiguredOrigins(&cfg)
	if err != nil || !slices.Equal(origins, []string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}) {
		t.Fatalf("two-operator origins changed: %v %v", origins, err)
	}
	third := append(slices.Clone(cfg.Operators), OperatorConfig{NoID: 3, APIURL: "https://three.example"})
	origins, err = releaseEvidenceV2ConfiguredOrigins(&ReleaseConfig{Operators: third})
	if err != nil || !slices.Equal(origins, []string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}) {
		t.Fatalf("larger census changed its replica pair: %v %v", origins, err)
	}
	if origins, err := releaseEvidenceV2ConfiguredOrigins(&ReleaseConfig{}); err == nil || origins != nil {
		t.Fatal("empty operator census produced a public origin")
	}
	if origins, err := releaseEvidenceV2ConfiguredOrigins(nil); err == nil || origins != nil {
		t.Fatal("absent configuration produced a public origin")
	}
}

// Readers exist for one or two independent origins only. A duplicate pair
// still refuses, and no reader census can be empty or padded.
func TestReleaseEvidenceV2StartupReadersAdmitOneOrTwoOrigins(t *testing.T) {
	bounds := releaseEvidenceV2TestConfig("", []OperatorConfig{{NoID: 1}}).Bounds.Cut
	for _, origins := range [][]string{{"https://one.example"}, {"https://one.example", "https://two.example"}} {
		readers, err := newReleaseEvidenceV2StartupReaders(origins, bounds)
		if err != nil || len(readers) != len(origins) {
			t.Fatalf("%d-origin reader census was refused: %v", len(origins), err)
		}
		for index, reader := range readers {
			if reader == nil || reader.endpoint.Host != strings.TrimPrefix(origins[index], "https://") {
				t.Fatal("reader census lost its configured origin order")
			}
		}
	}
	for _, origins := range [][]string{nil, {}, {"https://one.example", "https://ONE.example:443"}, {"https://one.example", "https://two.example", "https://three.example"}} {
		if readers, err := newReleaseEvidenceV2StartupReaders(origins, bounds); err == nil || readers != nil {
			t.Fatalf("invalid reader census admitted: %v", origins)
		}
	}
}

// A single-member census names its only origin. A larger census cannot be
// truncated to one origin, and no census can be padded past two.
func TestValidatorEvidencePublicationV2ManifestBindsOriginCensus(t *testing.T) {
	member := func(noId uint64) ValidatorEvidencePublicationV2MemberReference {
		return ValidatorEvidencePublicationV2MemberReference{NoId: noId, SignedArtifactHash: [32]byte{byte(noId)}, SignedArtifactBytes: 1}
	}
	manifest := func(origins []string, members ...ValidatorEvidencePublicationV2MemberReference) *ValidatorEvidencePublicationV2Manifest {
		return &ValidatorEvidencePublicationV2Manifest{Schema: ValidatorEvidencePublicationV2Schema, Kind: protocol.ValidatorEvidenceClosedCensus, Epoch: 7, Origins: origins, CensusHash: [32]byte{1}, CensusBytes: 1, Members: members}
	}
	one, two, three := "https://one.example", "https://two.example", "https://three.example"
	for _, valid := range []*ValidatorEvidencePublicationV2Manifest{
		manifest([]string{one}, member(1)),
		manifest([]string{one, two}, member(1)),
		manifest([]string{one, two}, member(1), member(2)),
		manifest([]string{one, two}, member(1), member(2), member(3)),
	} {
		if err := valid.validate(4096, 4); err != nil {
			t.Fatalf("%d-origin %d-member locator refused: %v", len(valid.Origins), len(valid.Members), err)
		}
	}
	for _, invalid := range []*ValidatorEvidencePublicationV2Manifest{
		manifest(nil, member(1)),
		manifest([]string{}, member(1)),
		manifest([]string{one}, member(1), member(2)),
		manifest([]string{one}, member(1), member(2), member(3)),
		manifest([]string{one, two, three}, member(1)),
		manifest([]string{one, two, three}, member(1), member(2), member(3)),
		manifest([]string{one, one}, member(1), member(2)),
		manifest([]string{one + "/"}, member(1)),
	} {
		if err := invalid.validate(4096, 4); err == nil {
			t.Fatalf("%d-origin %d-member locator admitted: %v", len(invalid.Origins), len(invalid.Members), invalid.Origins)
		}
		audit := ValidatorEvidenceDepositAuditV2Manifest{Schema: ValidatorEvidenceDepositAuditV2ManifestSchema, Kind: protocol.ValidatorEvidenceDepositAudit, Epoch: 7, Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: 9, NativeEpoch: 3},
			Decision: ReleaseMeasurementV2Decision{SettlementEpoch: 9, SubnetEpoch: 3}, Origins: invalid.Origins, CensusHash: [32]byte{1}, CensusBytes: 1, Members: invalid.Members}
		if err := audit.validate(4096, 4); err == nil {
			t.Fatalf("%d-origin %d-member audit locator admitted", len(invalid.Origins), len(invalid.Members))
		}
	}
	// A canonical decoder refuses a truncated pair rather than zero-filling it.
	pair, err := json.Marshal(manifest([]string{one, two}, member(1), member(2)))
	if err != nil {
		t.Fatal(err)
	}
	truncated := bytes.Replace(pair, []byte(`["`+one+`","`+two+`"]`), []byte(`["`+one+`"]`), 1)
	var decoded ValidatorEvidencePublicationV2Manifest
	if bytes.Equal(pair, truncated) || decodeAttemptStreamV2JSON(truncated, 4096, &decoded) != nil || decoded.validate(4096, 4) == nil {
		t.Fatal("a truncated two-member origin pair was decoded as a complete locator")
	}
}

// The fixed pair and the census slice share one JSON grammar, so every
// two-operator locator, read authority and publication keeps its exact
// canonical bytes, content hash and window-authority signing input.
func TestReleaseEvidenceV2TwoOriginEncodingsAreUnchanged(t *testing.T) {
	origins := []string{"https://one.example", "https://two.example"}
	pair := [2]string{origins[0], origins[1]}
	members := []ValidatorEvidencePublicationV2MemberReference{{NoId: 2, SignedArtifactHash: [32]byte{2}, SignedArtifactBytes: 2}, {NoId: 3, SignedArtifactHash: [32]byte{3}, SignedArtifactBytes: 3}}
	type pairManifest struct {
		Schema      string                                          `json:"schema"`
		Kind        byte                                            `json:"kind"`
		Epoch       uint64                                          `json:"epoch"`
		Origins     [2]string                                       `json:"origins"`
		CensusHash  [32]byte                                        `json:"census_hash"`
		CensusBytes uint64                                          `json:"census_bytes"`
		Members     []ValidatorEvidencePublicationV2MemberReference `json:"members"`
	}
	type pairAudit struct {
		Schema      string                                          `json:"schema"`
		Kind        byte                                            `json:"kind"`
		Epoch       uint64                                          `json:"epoch"`
		Subject     protocol.ValidatorEvidenceSubject               `json:"subject"`
		Decision    ReleaseMeasurementV2Decision                    `json:"decision"`
		Origins     [2]string                                       `json:"origins"`
		CensusHash  [32]byte                                        `json:"census_hash"`
		CensusBytes uint64                                          `json:"census_bytes"`
		Members     []ValidatorEvidencePublicationV2MemberReference `json:"members"`
	}
	type pairReadOptions struct {
		Policy         *protocol.Policy
		PreviousPolicy *protocol.Policy
		Activations    []protocol.ValidatorEvidenceActivation
		Window         protocol.ValidatorEvidenceWindow
		Origins        [2]string
		Bounds         ReleaseEvidenceV2Bounds
	}
	type pairPublication struct {
		Census     []byte
		CensusHash [32]byte
		Members    []ValidatorEvidenceMemberV2Publication
		Origins    [2]string
	}
	decision := ReleaseMeasurementV2Decision{DeploymentID: "pair", SettlementEpoch: 9, SubnetEpoch: 3}
	bounds := releaseEvidenceV2TestConfig("", []OperatorConfig{{NoID: 2}, {NoID: 3}}).Bounds
	window := protocol.ValidatorEvidenceWindow{Epoch: 7, StartBlock: 10, EndBlock: 20, FinalizedBlock: 20}
	activations := []protocol.ValidatorEvidenceActivation{{NoID: 2, Hotkey: [32]byte{4}}, {NoID: 3, Hotkey: [32]byte{4}}}
	member := ValidatorEvidenceMemberV2Publication{Evidence: ValidatorEvidenceSignedV2{Schema: ValidatorEvidenceSignedV2Schema, VPKSignature: []byte{5}, HotkeySignature: []byte{6}}, Payload: []byte("payload"), SignedArtifact: []byte("signed"), SignedArtifactHash: [32]byte{7}, Calldata: []byte{8}}
	for _, value := range []struct {
		name          string
		census, fixed any
	}{
		{"closed locator", ValidatorEvidencePublicationV2Manifest{Schema: ValidatorEvidencePublicationV2Schema, Kind: protocol.ValidatorEvidenceClosedCensus, Epoch: 7, Origins: origins, CensusHash: [32]byte{1}, CensusBytes: 1, Members: members},
			pairManifest{Schema: ValidatorEvidencePublicationV2Schema, Kind: protocol.ValidatorEvidenceClosedCensus, Epoch: 7, Origins: pair, CensusHash: [32]byte{1}, CensusBytes: 1, Members: members}},
		{"audit locator", ValidatorEvidenceDepositAuditV2Manifest{Schema: ValidatorEvidenceDepositAuditV2ManifestSchema, Kind: protocol.ValidatorEvidenceDepositAudit, Epoch: 7, Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: 9, NativeEpoch: 3}, Decision: decision, Origins: origins, CensusHash: [32]byte{1}, CensusBytes: 1, Members: members},
			pairAudit{Schema: ValidatorEvidenceDepositAuditV2ManifestSchema, Kind: protocol.ValidatorEvidenceDepositAudit, Epoch: 7, Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: 9, NativeEpoch: 3}, Decision: decision, Origins: pair, CensusHash: [32]byte{1}, CensusBytes: 1, Members: members}},
		{"read authority", ValidatorEvidencePublicationV2ReadOptions{Activations: activations, Window: window, Origins: origins, Bounds: bounds},
			pairReadOptions{Activations: activations, Window: window, Origins: pair, Bounds: bounds}},
		{"publication", ValidatorEvidenceCensusV2Publication{Census: []byte("census"), CensusHash: [32]byte{9}, Members: []ValidatorEvidenceMemberV2Publication{member}, Origins: origins},
			pairPublication{Census: []byte("census"), CensusHash: [32]byte{9}, Members: []ValidatorEvidenceMemberV2Publication{member}, Origins: pair}},
	} {
		for _, indent := range []bool{false, true} {
			want, err := marshalAttemptSettlementV2JSON(t.Context(), value.fixed, 1024*1024, indent, true)
			if err != nil {
				t.Fatal(err)
			}
			got, err := marshalAttemptSettlementV2JSON(t.Context(), value.census, 1024*1024, indent, true)
			if err != nil || !bytes.Equal(got, want) || sha256.Sum256(got) != sha256.Sum256(want) {
				t.Fatalf("%s two-origin encoding changed (indent=%t): %v\n%s\n%s", value.name, indent, err, got, want)
			}
		}
	}
	// The provider window authority signs the publication read authority as
	// ordinary JSON; its two-origin signing input is unchanged.
	authority := ProviderAttemptValidatorAuthority{Hotkey: [32]byte{4}, Publication: ValidatorEvidencePublicationV2ReadOptions{Activations: activations, Window: window, Origins: origins, Bounds: bounds}, MaxParticipants: 2, MaxTransitionBytes: 3, MaxClosureBytes: 4}
	fixed := struct {
		Hotkey             [32]byte                           `json:"hotkey"`
		Publication        pairReadOptions                    `json:"publication"`
		Operators          []ProviderAttemptOperatorAuthority `json:"operators"`
		MaxParticipants    uint64                             `json:"max_participants"`
		MaxTransitionBytes uint64                             `json:"max_transition_bytes"`
		MaxClosureBytes    uint64                             `json:"max_closure_bytes"`
	}{Hotkey: [32]byte{4}, Publication: pairReadOptions{Activations: activations, Window: window, Origins: pair, Bounds: bounds}, MaxParticipants: 2, MaxTransitionBytes: 3, MaxClosureBytes: 4}
	got, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(fixed)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("provider window authority signing input changed: %v", err)
	}
	var decoded ValidatorEvidencePublicationV2Manifest
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), ValidatorEvidencePublicationV2Manifest{Schema: ValidatorEvidencePublicationV2Schema, Kind: protocol.ValidatorEvidenceClosedCensus, Epoch: 7, Origins: origins, CensusHash: [32]byte{1}, CensusBytes: 1, Members: members}, 4096, true, true)
	if err != nil || decodeAttemptStreamV2JSON(encoded, 4096, &decoded) != nil || !slices.Equal(decoded.Origins, origins) {
		t.Fatalf("two-origin locator did not decode to its exact pair: %v", err)
	}
	reencoded, err := marshalAttemptSettlementV2JSON(t.Context(), &decoded, 4096, true, true)
	if err != nil || !bytes.Equal(reencoded, encoded) {
		t.Fatal("two-origin locator is not canonical after decoding", err)
	}
}

// A publisher census is the configured one or two complete replicas; no
// empty, padded, incomplete or aliased pair reaches a storage callback.
func TestAttemptCutV2ReplicasAdmitOneOrTwoOrigins(t *testing.T) {
	write := func(context.Context, string, []byte) error { t.Error("admission invoked a writer"); return nil }
	replica := func(origin string) AttemptCutV2Replica {
		return AttemptCutV2Replica{Origin: origin, WriteRecords: write, WriteProofs: write, WriteMetadata: write}
	}
	bounds := attemptCutV2ReplicaTestBounds()
	for _, origins := range [][]string{{"https://one.test"}, {"https://one.test", "https://two.test"}} {
		replicas := make([]AttemptCutV2Replica, len(origins))
		for index, origin := range origins {
			replicas[index] = replica(origin)
		}
		publisher, err := newAttemptCutV2Replicas(bounds, replicas)
		if err != nil || len(publisher.readers) != len(origins) || !slices.Equal(publisher.origins(), origins) {
			t.Fatalf("%d-replica publisher census was refused: %v", len(origins), err)
		}
	}
	for _, replicas := range [][]AttemptCutV2Replica{nil, {}, {replica("https://one.test"), replica("https://two.test"), replica("https://three.test")}, {replica("https://one.test"), replica("https://one.test:443")}, {{Origin: "https://one.test", WriteRecords: write, WriteProofs: write}}} {
		if publisher, err := newAttemptCutV2Replicas(bounds, replicas); err == nil || publisher != nil {
			t.Fatalf("invalid %d-replica publisher census admitted", len(replicas))
		}
	}
	// Admission owns its census; a later caller mutation cannot redirect it.
	replicas := []AttemptCutV2Replica{replica("https://one.test")}
	publisher, err := newAttemptCutV2Replicas(bounds, replicas)
	if err != nil {
		t.Fatal(err)
	}
	replicas[0].Origin = "https://redirected.test"
	if !slices.Equal(publisher.origins(), []string{"https://one.test"}) {
		t.Fatal("publisher census aliased its caller's replica slice")
	}
}

// The single operator's own authenticated session is the only replica. Real
// attempt evidence is sealed, uploaded and read back from that origin alone.
func TestReleaseEvidenceV2UploadReplicasSealSingleOperatorEvidence(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 1)
	bounds := attemptCutV2ReplicaTestBounds()
	cfg, runtimes, stores := newReleaseAttemptUploadV2HTTPFixture(t, 1, bounds)
	origin := cfg.Operators[0].APIURL
	replicas, err := releaseAttemptUploadReplicasV2(cfg, []string{origin}, runtimes)
	if err != nil || len(replicas) != 1 || replicas[0].Origin != origin {
		t.Fatalf("single operator session was not its only replica: %v", err)
	}
	publication, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, bounds, AttemptCutV2ReplicaOptions{ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas})
	if err != nil || publication == nil || !slices.Equal(publication.Origins, []string{origin}) {
		t.Fatalf("real single-origin attempt seal: %v", err)
	}
	if publication.Cut.RecordCount != 10 || publication.Cut.CompleteCount != 1 || publication.Cut.FailedCount != 1 {
		t.Fatal("real attempt census changed at its only public origin")
	}
	cut, err := DecodeAttemptCutV2(stores[0].snapshot()["metadata/"+publication.ContentHash], fixture.expected, bounds)
	if err != nil || !reflect.DeepEqual(cut, publication.Cut) || stores[0].posts.Load() < 5 || stores[0].gets.Load() < stores[0].posts.Load() {
		t.Fatalf("only replica omitted signed proof bytes or their public readback: %v", err)
	}
}

// A single configured operator admits only its own origin. A two-operator
// census cannot be truncated to one replica, and no census can be padded.
func TestReleaseEvidenceV2UploadReplicasBindConfiguredOriginCensus(t *testing.T) {
	t.Parallel()
	bounds := attemptCutV2ReplicaTestBounds()
	single, singleRuntimes, singleStores := newReleaseAttemptUploadV2HTTPFixture(t, 1, bounds)
	pair, pairRuntimes, pairStores := newReleaseAttemptUploadV2HTTPFixture(t, 2, bounds)
	for _, attempt := range []struct {
		name     string
		cfg      *ReleaseConfig
		origins  []string
		runtimes []*releaseOperatorRuntime
	}{
		{"single-empty", single, nil, singleRuntimes},
		{"single-padded", single, []string{single.Operators[0].APIURL, pair.Operators[0].APIURL}, singleRuntimes},
		{"single-foreign", single, []string{pair.Operators[0].APIURL}, singleRuntimes},
		{"pair-truncated-first", pair, []string{pair.Operators[0].APIURL}, pairRuntimes},
		{"pair-truncated-second", pair, []string{pair.Operators[1].APIURL}, pairRuntimes},
		{"pair-padded", pair, []string{pair.Operators[0].APIURL, pair.Operators[1].APIURL, single.Operators[0].APIURL}, pairRuntimes},
		{"pair-empty", pair, nil, pairRuntimes},
	} {
		if replicas, err := releaseAttemptUploadReplicasV2(attempt.cfg, attempt.origins, attempt.runtimes); err == nil || replicas != nil {
			t.Fatalf("%s origin census was admitted", attempt.name)
		}
	}
	for _, store := range append(slices.Clone(singleStores), pairStores...) {
		if store.posts.Load() != 0 || store.gets.Load() != 0 {
			t.Fatal("refused origin census performed HTTP")
		}
	}
	if replicas, err := releaseAttemptUploadReplicasV2(pair, []string{pair.Operators[0].APIURL, pair.Operators[1].APIURL}, pairRuntimes); err != nil || len(replicas) != 2 {
		t.Fatalf("two-operator pair changed admission: %v", err)
	}
}

// The single operator's complete census is replayed, consented and stored at
// its only public origin; the locator names exactly that origin and member.
func TestValidatorEvidenceCensusV2PublishesSingleOperatorAtItsOnlyOrigin(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixtureForCensus(t, 1, 1, []uint64{9})
	options := fixture.options(t)
	if len(options.Replicas) != 1 || len(fixture.stores) != 1 || options.SecondReplicaScratchDirectories != nil {
		t.Fatal("single-operator census did not configure exactly one replica")
	}
	_, _, readsBefore := fixture.stores[0].snapshot()
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
	if err != nil || publication == nil || len(publication.Members) != 1 || !slices.Equal(publication.Origins, []string{fixture.replicas[0].Origin}) {
		t.Fatalf("single-operator census publication: %v", err)
	}
	var census ValidatorEvidenceCensusV2
	if err := json.Unmarshal(publication.Census, &census); err != nil {
		t.Fatal(err)
	}
	if len(census.Members) != 1 || census.Members[0].NoID != 9 || census.Members[0].RecordCount != 10 || census.Members[0].CompleteCount != 1 || census.Members[0].FailedCount != 1 {
		t.Fatalf("single-operator census lost its complete/failed trails: %+v", census)
	}
	member := publication.Members[0]
	if err := member.Evidence.Header.Verify(options.Settlement.Operators[9].Expected.Activation.Domain, options.Window, member.Evidence.VPKSignature, member.Evidence.HotkeySignature); err != nil {
		t.Fatalf("single-operator dual consent: %v", err)
	}
	objects, _, reads := fixture.stores[0].snapshot()
	if reads <= readsBefore || !bytes.Equal(objects["metadata/"+attemptHex32(member.Evidence.Header.PayloadHash)], member.Payload) || !bytes.Equal(objects["metadata/"+attemptHex32(publication.CensusHash)], publication.Census) || !bytes.Equal(objects["metadata/"+attemptHex32(member.SignedArtifactHash)], member.SignedArtifact) {
		t.Fatal("only replica was not replayed or lacks its payload, census or consent")
	}
	manifest, err := WriteValidatorEvidencePublicationV2Manifest(t.Context(), newAttemptSettlementRuntimeV2TestStateDir(t), publication, []uint64{9}, options.Settlement.MaxClosureBytes, options.Settlement.MaxParticipants)
	if err != nil || !slices.Equal(manifest.Origins, publication.Origins) || len(manifest.Members) != 1 || manifest.Members[0].NoId != 9 {
		t.Fatalf("single-operator locator: %v", err)
	}
}

// A single origin owns exactly one replay. A second scratch, a padded alias
// replica or an absent replica census is refused before any public I/O.
func TestValidatorEvidenceCensusV2SingleOriginRefusesOtherCensusBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixtureForCensus(t, 0, 0, []uint64{9})
	for _, change := range []func(*ValidatorEvidenceCensusV2Options){
		func(options *ValidatorEvidenceCensusV2Options) {
			options.SecondReplicaScratchDirectories = map[uint64]string{9: filepath.Join(t.TempDir(), "replica-two")}
		},
		func(options *ValidatorEvidenceCensusV2Options) {
			options.Replicas = append(options.Replicas, options.Replicas[0])
		},
		func(options *ValidatorEvidenceCensusV2Options) { options.Replicas = nil },
		func(options *ValidatorEvidenceCensusV2Options) {
			options.ReplicasByOperator = map[uint64][]AttemptCutV2Replica{9: options.Replicas}
		},
		func(options *ValidatorEvidenceCensusV2Options) {
			options.ReplicasByOperator, options.Replicas = map[uint64][]AttemptCutV2Replica{9: nil}, nil
		},
	} {
		options := fixture.options(t)
		change(&options)
		before := fixture.counts()
		publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
		if err == nil || publication != nil || fixture.counts() != before {
			t.Fatalf("other replica census crossed single-origin admission: %v", err)
		}
	}
}

// The only origin must serve every real proof through EOF; no other copy can
// stand in for a corrupt single replica.
func TestValidatorEvidenceCensusV2RejectsOnlyReplicaProofCorruption(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixtureForCensus(t, 1, 0, []uint64{9})
	fixture.stores[0].readBytes = func(kind string, raw []byte) []byte {
		if kind == AttemptStreamV2Proofs {
			return append(raw, '\n')
		}
		return raw
	}
	before := fixture.counts()
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t))
	after := fixture.counts()
	if err == nil || publication != nil || after[0][0] != before[0][0] || after[0][1] <= before[0][1] {
		t.Fatalf("bad single-origin proof produced a publication: %v", err)
	}
}

// Capture retains the single operator's complete signed setup before any
// historical read. Padded, foreign, empty or truncated origin censuses are
// refused before the first retained source.
func TestReleaseCaptureV2SingleOperatorRetainsSetupBeforeHistoricalReads(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t, releaseSingleOperatorV2TestConfig(t))
	capture := func(fixture *releaseBootstrapV2TestFixture, origins []string) ([]ReleaseEvidenceV2CaptureSource, *ReleaseEvidenceV2Capture, error) {
		options := releaseCaptureV2TestOptions(fixture)
		if origins != nil {
			options.Origins = origins
		}
		var sources []ReleaseEvidenceV2CaptureSource
		result, err := CaptureReleaseEvidenceV2(t.Context(), &fixture.cfg, fixture.chain, fixture.native, options, func(_ context.Context, source ReleaseEvidenceV2CaptureSource, raw []byte) error {
			if len(raw) == 0 {
				t.Fatal("empty original setup source")
			}
			sources = append(sources, source)
			return nil
		})
		return sources, result, err
	}
	before := fixture.calls()
	if options := releaseCaptureV2TestOptions(fixture); !slices.Equal(options.Origins, []string{fixture.cfg.Operators[0].APIURL}) {
		t.Fatalf("single-operator capture census differs: %v", options.Origins)
	}
	sources, result, err := capture(fixture, nil)
	if err == nil || result != nil || len(sources) != 5 || fixture.calls() != before {
		t.Fatalf("single-operator capture queried history or lost setup sources: sources=%d calls=%d/%d result=%v err=%v", len(sources), fixture.calls(), before, result, err)
	}
	for _, source := range sources {
		if source.Kind != "setup" || source.Origin != "" || !strings.HasPrefix(source.Name, "activation/no-2/") {
			t.Fatalf("unexpected pre-history source: %+v", source)
		}
	}
	for _, origins := range [][]string{{}, {fixture.cfg.Operators[0].APIURL, "https://padded.example"}, {"https://foreign.example"}} {
		if sources, result, err := capture(fixture, origins); err == nil || result != nil || len(sources) != 0 {
			t.Fatalf("capture admitted origin census %v", origins)
		}
	}
	pair := newReleaseBootstrapV2TestFixture(t)
	if sources, result, err := capture(pair, []string{pair.cfg.Operators[0].APIURL}); err == nil || result != nil || len(sources) != 0 {
		t.Fatal("two-operator capture admitted a truncated origin census")
	}
}

// Public replay of a one-operator history reads its only retained origin and
// refuses a missing proof there. A padded pair cannot address that history.
func TestReleaseArchiveV2SingleOperatorReplaysItsOnlyOrigin(t *testing.T) {
	fixture := newReleaseArchiveV2TestFixtureWithTrails(t, 1, releaseSingleOperatorV2TestConfig(t))
	if len(fixture.options.Origins) != 1 || len(fixture.options.Config.Operators) != 1 || fixture.options.Origins[0] != fixture.options.Config.Operators[0].APIURL {
		t.Fatalf("single-operator archive census differs: %v", fixture.options.Origins)
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if archive.history.replicas() != 1 || len(archive.history.terminals) != 2 || len(archive.history.inputContextsByEpoch[7]) != 1 {
		t.Fatal("single-origin archive skipped complete ordinary/terminal replay")
	}
	for _, transition := range fixture.last.Transitions {
		cursor := archive.history.current[transition.Identity.NoID]
		if cursor.epoch != transition.ToEpoch || cursor.lastRoot != transition.Cut.Root || cursor.lastSequence != transition.Cut.LastSequence || !slices.Equal(cursor.prior, transition.PostFold) {
			t.Fatal("single-origin archive cursor or exact post-fold EMA differs")
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	padded := fixture.options
	padded.Origins = append(slices.Clone(fixture.options.Origins), "https://padded.example")
	if archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), padded); err == nil || archive != nil {
		t.Fatal("single-operator archive admitted a padded origin pair")
	}
	removed := false
	for source := range fixture.files {
		if source.Kind == AttemptStreamV2Proofs && source.Origin == fixture.options.Origins[0] {
			delete(fixture.files, source)
			removed = true
			break
		}
	}
	fixture.repin()
	if archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options); !removed || err == nil || archive != nil {
		t.Fatalf("missing only-origin proof was not refused: removed=%t %v", removed, err)
	}
}

// The single operator's actual terminal census is read from its only origin;
// a padded read authority and the loss of that origin's last consent refuse.
func TestValidatorEvidencePublicationV2SingleOriginReadsExactCensus(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, true, releaseSingleOperatorV2TestConfig(t))
	if len(fixture.startup.inputs) != 1 || len(fixture.readOptions.Origins) != 1 || !slices.Equal(fixture.manifest.Origins, fixture.readOptions.Origins) || len(fixture.manifest.Members) != 1 {
		t.Fatalf("single-origin publication locator differs: %+v", fixture.manifest)
	}
	observed, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions)
	if err != nil || observed == nil || len(observed.Members) != 1 || !slices.Equal(observed.Origins, fixture.readOptions.Origins) {
		t.Fatalf("single-origin public reader: %v", err)
	}
	var census ValidatorEvidenceCensusV2
	if err := json.Unmarshal(observed.Census, &census); err != nil {
		t.Fatal(err)
	}
	if len(census.Members) != 1 || census.Members[0].RecordCount != 10 || census.Members[0].CompleteCount != 1 || census.Members[0].FailedCount != 1 {
		t.Fatal("single-origin reader collapsed pending checkpoints or failed trails")
	}
	if member := observed.Members[0]; !bytes.Equal(member.SignedArtifact, fixture.publication.Members[0].SignedArtifact) || !bytes.Equal(member.Payload, fixture.publication.Members[0].Payload) || !bytes.Equal(member.Calldata, fixture.publication.Members[0].Calldata) {
		t.Fatal("single-origin reader did not return exact published bytes")
	}
	_, _, reads := fixture.startup.stores[0].snapshot()
	padded := fixture.readOptions
	padded.Origins = append(slices.Clone(fixture.readOptions.Origins), "https://padded.example")
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, padded); err == nil || publication != nil {
		t.Fatal("single-origin locator was read under a padded origin pair")
	}
	if _, _, after := fixture.startup.stores[0].snapshot(); after != reads {
		t.Fatal("padded read authority performed public reads")
	}
	store := fixture.startup.stores[0]
	func() {
		store.stateLock.Lock()
		defer store.stateLock.Unlock()
		delete(store.objects, "metadata/"+attemptHex32(fixture.manifest.Members[0].SignedArtifactHash))
	}()
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions); err == nil || publication != nil {
		t.Fatal("missing only-origin consent escaped")
	}
}

// A restart reuses the exact single-origin consent without another upload,
// and a stopped server's retained store serves that exact publication.
func TestValidatorEvidencePublicationV2SingleOriginRestartAndRetainedRead(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false, releaseSingleOperatorV2TestConfig(t))
	path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, fixture.manifest.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, fixture.readOptions.Bounds.MaxClosureBytes, fixture.readOptions.Bounds.MaxParticipants)
	if err != nil || !slices.Equal(retained.Origins, fixture.readOptions.Origins) {
		t.Fatalf("single-origin locator did not reopen: %v", err)
	}
	_, writes, _ := fixture.startup.stores[0].snapshot()
	publication, err := publishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t), retained, fixture.readOptions)
	if err != nil || publication == nil || !bytes.Equal(publication.Members[0].SignedArtifact, fixture.publication.Members[0].SignedArtifact) {
		t.Fatalf("single-origin restart did not reuse its exact consent: %v", err)
	}
	if _, after, _ := fixture.startup.stores[0].snapshot(); after != writes {
		t.Fatal("single-origin restart spent upload quota on published metadata")
	}
	if _, err := WriteValidatorEvidencePublicationV2Manifest(t.Context(), fixture.startup.cfg.StateDir, publication, []uint64{fixture.startup.inputs[0].Config.NoID}, fixture.readOptions.Bounds.MaxClosureBytes, fixture.readOptions.Bounds.MaxParticipants); err != nil {
		t.Fatalf("exact single-origin locator retry: %v", err)
	}
	objects, _, _ := fixture.startup.stores[0].snapshot()
	fixture.startup.stores[0].stopHTTP()
	replicas := retainedPublicationV2TestReplicas(fixture.readOptions.Origins, []map[string][]byte{objects})
	actual, err := ReadRetainedValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions, replicas)
	if err != nil || !sameReleaseArchivePublicationV2(actual, fixture.publication) {
		t.Fatal("stopped single origin lost its exact signed publication", err)
	}
	for _, census := range [][]ValidatorEvidenceRetainedReplicaV2{nil, {}, append(slices.Clone(replicas), replicas[0])} {
		if actual, err := ReadRetainedValidatorEvidencePublicationV2(t.Context(), fixture.manifest, fixture.readOptions, census); err == nil || actual != nil {
			t.Fatalf("retained reader admitted a %d-store census", len(census))
		}
	}
}

// The one-operator root closes missed epochs, uploads each signed source to
// that operator's own server only and publishes one-origin census locators.
// A disk restart reuses every published consent without another upload.
func TestReleaseRuntimeV2SingleOperatorPublishesAndRestartsAtItsOnlyOrigin(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixtureWithBounds(t, nil, releaseSingleOperatorV2TestConfig(t))
	if len(fixture.runtimes) != 1 || len(fixture.origins) != 1 || len(fixture.stores) != 1 || !slices.Equal(fixture.runtime.origins, fixture.origins) || len(fixture.runtime.history.readers) != 1 {
		t.Fatal("single-operator root does not own exactly one public origin")
	}
	fixture.startup.trail(t, 0)
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	noID := fixture.startup.inputs[0].Config.NoID
	if fixture.runtime.history.current[noID].epoch != 9 {
		t.Fatal("single-operator missed epochs were not closed consecutively")
	}
	for _, epoch := range []uint64{7, 8} {
		path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, epoch)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, fixture.startup.cfg.EvidenceV2.Bounds.MaxClosureBytes, 1)
		if err != nil || !slices.Equal(manifest.Origins, fixture.origins) || len(manifest.Members) != 1 || manifest.Members[0].NoId != noID {
			t.Fatalf("single-operator locator for epoch %d: %v", epoch, err)
		}
		options := ValidatorEvidencePublicationV2ReadOptions{Origins: fixture.origins, Bounds: fixture.startup.cfg.EvidenceV2.Bounds, Window: protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: 1001 + 500*(epoch-7), EndBlock: 1501 + 500*(epoch-7), FinalizedBlock: 2001},
			Activations: []protocol.ValidatorEvidenceActivation{fixture.startup.inputs[0].Context.Activation}}
		publication, err := ReadValidatorEvidencePublicationV2(t.Context(), manifest, options)
		if err != nil || len(publication.Members) != 1 {
			t.Fatalf("actual single-origin closed census for epoch %d: %v", epoch, err)
		}
	}
	before := fixture.stores[0].counts()
	if len(before) != 1 || before[noID] == 0 {
		t.Fatalf("operator server did not receive its own signed source: %+v", before)
	}
	fixture.startup.reopen(t)
	fixture.start(t)
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(before, fixture.stores[0].counts()) {
		t.Fatal("single-operator restart changed its published source census")
	}
}

// A provider request window is replicated to, and read back from, the single
// operator's only origin. An empty, padded or aliased census writes nothing.
func TestProviderRequestPublicationReplicatesToSingleOrigin(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStoreCensus(t, 1)
	bounds := attemptCutV2ReplicaTestBounds()
	raw := []byte("{\"single\":true}\n")
	for _, census := range [][]AttemptCutV2Replica{nil, {}, {replicas[0], replicas[0]}, {replicas[0], replicas[0], replicas[0]}} {
		if err := replicateProviderAttemptPublication(t.Context(), raw, census, bounds, 4096); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
			t.Fatalf("%d-replica provider publication census admitted: %v", len(census), err)
		}
	}
	if _, writes, reads := stores[0].snapshot(); writes != 0 || reads != 0 {
		t.Fatal("refused provider publication census reached storage or HTTP")
	}
	if err := replicateProviderAttemptPublication(t.Context(), raw, replicas, bounds, 4096); err != nil {
		t.Fatalf("single-origin provider publication: %v", err)
	}
	objects, writes, reads := stores[0].snapshot()
	if writes != 1 || reads != 1 || !bytes.Equal(objects["metadata/"+attemptHex32(sha256.Sum256(raw))], raw) {
		t.Fatal("single-origin provider publication was not stored and read back exactly")
	}
}
