//go:build linux || darwin

package validator

// Generates only synthetic public evidence for the operator's cross-repository
// replay regression. The real engine, disk ledger and sealer create every
// record/proof/signature. No production keys, addresses or captures are used.

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol/headscore"
	"github.com/urfoundation/sn/v2026/protocol/headsummary"
	"github.com/urnetwork/connect/v2026"
)

// Explicit regeneration keeps routine tests read-only. The checked-in fixture
// is consumed and authenticated by server/controller's complete blob replay.
func TestHeadSummaryReplayFixture(t *testing.T) {
	if os.Getenv("SN_HEAD_REPLICA_FIXTURE_UPDATE") != "1" {
		t.Skip("set SN_HEAD_REPLICA_FIXTURE_UPDATE=1 to regenerate the synthetic operator fixture")
	}
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	providers := slices.Clone(fixture.server.providers)
	fixture.server.providers = providers[:8]
	first, err := fixture.engine.RunTrail(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.providers = providers[8:]
	second, err := fixture.engine.RunTrail(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.expected.EgressFirstSequence = 9
	key := headSummaryTestKeypair(t)
	fixture.expected.Activation.Hotkey = key.PublicKey()
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	currentBindingKVs := map[connect.Id]FleetScoreKey{}
	if err := fixture.ledger.Walk(t.Context(), 1, 16, func(record AttemptRecord) error {
		for _, assignment := range record.Assignments {
			currentBindingKVs[assignment.NextHop] = attemptCutV2HeadTestFleetKey(t, assignment.Binding)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reboundId := second.Hops[1].ClientId
	reboundKey := currentBindingKVs[reboundId]
	reboundKey.Generation++
	currentBindingKVs[reboundId] = reboundKey
	prefixKVs, _, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), *cut, fixture.expected, fixture.policy, fixture.bounds, AttemptCutV2HeadOptions{
		CurrentBindingKVs: currentBindingKVs, MaxProviders: 16, MaxFleetPrefixes: 32,
		Replay: AttemptCutV2ReplayOptions{Bounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "fixture-replay"), ServerKeys: fixture.server.serverPublicKeys(), ReadMetadata: options.ReadMetadata, OpenData: options.OpenData},
	})
	if err != nil || len(prefixKVs) != 2 {
		t.Fatalf("genuine fixture replay: %v", err)
	}
	var liveKey FleetScoreKey
	for fleetKey, prefixes := range prefixKVs {
		if fleetKey.Generation == 1 {
			if len(prefixes) != 6 || prefixes[first.Hops[1].EgressIpHash] || prefixes[second.Hops[1].EgressIpHash] {
				t.Fatal("fixture lost pre-cut or rebound prefix exclusion")
			}
			liveKey = fleetKey
		} else if len(prefixes) != 0 {
			t.Fatal("new generation inherited a historical prefix")
		}
	}
	raw, err := cut.CanonicalJSON(fixture.bounds)
	if err != nil {
		t.Fatal(err)
	}
	cutHash := headsummary.Hex32(sha256.Sum256(raw))
	objects.metadataKVs[cutHash] = raw
	bindings := make([]headsummary.EvidenceBinding, 0, len(currentBindingKVs))
	for clientId, fleetKey := range currentBindingKVs {
		bindings = append(bindings, headsummary.EvidenceBinding{ClientId: clientId.String(), FleetId: headsummary.Hex32(fleetKey.FleetID), Hotkey: headsummary.Hex32(fleetKey.Hotkey), Generation: fleetKey.Generation, Uid: fleetKey.UID})
	}
	slices.SortFunc(bindings, func(a, b headsummary.EvidenceBinding) int { return strings.Compare(a.ClientId, b.ClientId) })
	summary := &headsummary.Summary{
		Schema: headsummary.SchemaV2, Netuid: fixture.expected.Identity.Netuid, NativeEpoch: 7, NativeSnapshotBlock: 200, NativeSnapshotHash: headsummary.Hex32([32]byte{0x71}),
		EVMSnapshotBlock: fixture.expected.Boundary.EVMBlock, EVMSnapshotHash: fixture.expected.Boundary.EVMBlockHash, SettlementEpoch: fixture.expected.Boundary.SettlementEpoch,
		PolicyID: fixture.policy.PolicyID, PolicyHash: headsummary.Hex32(fixture.expected.Activation.Domain.PolicyHash), MeasurementArtifactHash: "sha256:" + headsummary.Hex32([32]byte{0x72})[2:],
		EvidenceEmptyPrior: true, EvidenceRecipient: fixture.expected.Identity.NoID, EvidenceOperators: []uint64{fixture.expected.Identity.NoID}, EvidencePolicy: &fixture.policy,
		EvidenceCuts:    []headsummary.EvidenceCut{{NoId: fixture.expected.Identity.NoID, NativeBlock: 198, NativeBlockHash: headsummary.Hex32([32]byte{0x73}), ContentHash: cutHash, Bindings: bindings}},
		ValidatorHotkey: headsummary.Hex32(key.PublicKey()), ValidatorUID: fixture.expected.Identity.ValidatorUID, MaximumHeadFleets: fixture.policy.Steering.MaximumHeadFleets,
		Entries:      []headsummary.Entry{{Rank: 1, UID: liveKey.UID, Hotkey: headsummary.Hex32(liveKey.Hotkey), Score: headscore.Rational{Numerator: "6", Denominator: "1"}, Selected: true, Fleets: []headsummary.Fleet{{FleetID: headsummary.Hex32(liveKey.FleetID), Generation: liveKey.Generation}}}},
		SelectedUIDs: []uint16{liveKey.UID}, RejectedUIDs: []uint16{},
	}
	body, err := headsummary.Sign(summary, key.PublicKey(), time.Unix(1_800_000_000, 0), key.Sign)
	if err != nil {
		t.Fatal(err)
	}
	type object struct {
		Kind string `json:"kind"`
		Hash string `json:"hash"`
		Data []byte `json:"data"`
	}
	type prefixSet struct {
		Key    FleetScoreKey `json:"key"`
		Hashes []string      `json:"hashes"`
	}
	output := struct {
		Summary       json.RawMessage `json:"summary"`
		ServerKeys    map[byte][]byte `json:"server_keys"`
		Objects       []object        `json:"objects"`
		Prefixes      []prefixSet     `json:"prefixes"`
		PreCutPrefix  string          `json:"pre_cut_prefix"`
		ReboundPrefix string          `json:"rebound_prefix"`
	}{Summary: body, ServerKeys: map[byte][]byte{}, PreCutPrefix: headsummary.Hex32(first.Hops[1].EgressIpHash), ReboundPrefix: headsummary.Hex32(second.Hops[1].EgressIpHash)}
	for id, key := range fixture.server.serverPublicKeys() {
		output.ServerKeys[id] = key
	}
	for hash, data := range objects.metadataKVs {
		output.Objects = append(output.Objects, object{Kind: "metadata", Hash: hash, Data: data})
	}
	for kind, values := range objects.dataKVs {
		for hash, data := range values {
			output.Objects = append(output.Objects, object{Kind: kind, Hash: hash, Data: data})
		}
	}
	slices.SortFunc(output.Objects, func(a, b object) int { return strings.Compare(a.Kind+"/"+a.Hash, b.Kind+"/"+b.Hash) })
	for key, prefixes := range prefixKVs {
		hashes := make([]string, 0, len(prefixes))
		for hash := range prefixes {
			hashes = append(hashes, headsummary.Hex32(hash))
		}
		slices.Sort(hashes)
		output.Prefixes = append(output.Prefixes, prefixSet{Key: key, Hashes: hashes})
	}
	slices.SortFunc(output.Prefixes, func(a, b prefixSet) int { return strings.Compare(a.Key.String(), b.Key.String()) })
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	path := "../../server/controller/testdata/sn-top-replica-v2.json.gz"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	if _, err := compressed.Write(encoded); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
