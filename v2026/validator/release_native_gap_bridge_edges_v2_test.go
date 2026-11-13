//go:build linux || darwin

package validator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// The bridge file installed on the production validator, byte for byte, and
// the pin its deploy settings carry.
const (
	nativeGapBridgeLiveV1 = "{\n" +
		"  \"schema\": \"urnetwork-validator-native-gap-bridge-v1\",\n" +
		"  \"chain_id\": 964,\n" +
		"  \"deployment_id\": \"sn25-mainnet-20261007-01\",\n" +
		"  \"validator_id\": 1,\n" +
		"  \"last_native_epoch\": 25648,\n" +
		"  \"last_artifact_hash\": \"sha256:510390579f2734da814406138580ad4d941885b5821d0ed3ad8899b132dd0a6d\",\n" +
		"  \"first_native_epoch\": 25682\n" +
		"}\n"
	nativeGapBridgeLiveV1Pin  = "sha256:66472fc8be3a1e46f6c445fd05e38ae6f72a30d13e7e72fcac39568ceefd25d0"
	nativeGapBridgeLiveV1Hash = "sha256:510390579f2734da814406138580ad4d941885b5821d0ed3ad8899b132dd0a6d"
)

// The crossed production edge and a hypothetical later one.
func nativeGapBridgeEdgesTestList() []ReleaseNativeGapBridgeEdge {
	return []ReleaseNativeGapBridgeEdge{
		{LastNativeEpoch: 25648, LastArtifactHash: nativeGapBridgeLiveV1Hash, FirstNativeEpoch: 25682},
		{LastNativeEpoch: 25700, LastArtifactHash: nativeGapBridgeTestArtifactHash(25700), FirstNativeEpoch: 25705},
	}
}

func nativeGapBridgeEdgesTestWire(edges []ReleaseNativeGapBridgeEdge) releaseNativeGapBridgeEdgesWire {
	return releaseNativeGapBridgeEdgesWire{Schema: ReleaseNativeGapBridgeEdgesSchema, ChainID: 964, DeploymentID: "sn25-mainnet-20261007-01", ValidatorID: 1, Edges: edges}
}

func encodeNativeGapBridgeEdgesTest(t *testing.T, wire releaseNativeGapBridgeEdgesWire) []byte {
	t.Helper()
	encoded, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func nativeGapBridgeEdgesTestBridge(edges ...ReleaseNativeGapBridgeEdge) ReleaseNativeGapBridge {
	return ReleaseNativeGapBridge{Schema: ReleaseNativeGapBridgeEdgesSchema, ChainID: 964, DeploymentID: "sn25-mainnet-20261007-01", ValidatorID: 1, edges: edges}
}

// History intents whose predecessor hash matches the listed edges.
func nativeGapBridgeEdgesTestIntent(epoch uint64, status string) *SteeringIntent {
	intent := nativeGapBridgeTestIntent(epoch, status)
	if epoch == 25648 {
		intent.MeasurementArtifactHash = nativeGapBridgeLiveV1Hash
	}
	return intent
}

func nativeGapBridgeEdgesTestHistory(epochs []uint64, current string) []*SteeringIntent {
	all := make([]*SteeringIntent, len(epochs))
	for index, epoch := range epochs {
		all[index] = nativeGapBridgeEdgesTestIntent(epoch, "applied")
	}
	all[len(all)-1].Status = current
	return all
}

// The live v1 file keeps its bytes, decoded value, edge and history behaviour.
func TestNativeGapBridgeLiveV1FileIsUnchanged(t *testing.T) {
	t.Parallel()
	encoded := []byte(nativeGapBridgeLiveV1)
	if ReleaseMeasurementContentHash(encoded) != nativeGapBridgeLiveV1Pin || len(encoded) != 306 {
		t.Fatal("live bridge fixture differs from the installed file")
	}
	want := ReleaseNativeGapBridge{Schema: ReleaseNativeGapBridgeSchema, ChainID: 964, DeploymentID: "sn25-mainnet-20261007-01", ValidatorID: 1,
		LastNativeEpoch: 25648, LastArtifactHash: nativeGapBridgeLiveV1Hash, FirstNativeEpoch: 25682}
	decoded, err := DecodeReleaseNativeGapBridge(encoded, nativeGapBridgeLiveV1Pin)
	if err != nil || !reflect.DeepEqual(*decoded, want) || decoded.edges != nil {
		t.Fatalf("live v1 bridge decoded differently: %v %+v", err, decoded)
	}
	reencoded, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil || !bytes.Equal(append(reencoded, '\n'), encoded) {
		t.Fatalf("live v1 bridge no longer re-encodes to its pinned bytes: %v", err)
	}
	path := filepath.Join(t.TempDir(), "native-gap-bridge.json")
	if err := os.WriteFile(path, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
	read, err := ReadReleaseNativeGapBridge(path, nativeGapBridgeLiveV1Pin)
	if err != nil || !reflect.DeepEqual(*read, want) {
		t.Fatalf("live v1 bridge file was refused: %v", err)
	}
	if edges := decoded.Edges(); !reflect.DeepEqual(edges, nativeGapBridgeEdgesTestList()[:1]) {
		t.Fatalf("live v1 bridge lists %v", edges)
	}
	// The production history after crossing: 25648 applied, then 25683.
	crossed, err := decoded.matchHistory(t.Context(), nativeGapBridgeEdgesTestHistory([]uint64{25647, 25648, 25683}, "finalized"))
	if err != nil || crossed == nil || !crossed.crossed {
		t.Fatalf("live v1 bridge refused the crossed production history: %v", err)
	}
	if !crossed.allowsIntentEdge(nativeGapBridgeEdgesTestIntent(25648, "applied"), nativeGapBridgeEdgesTestIntent(25683, "finalized")) {
		t.Fatal("live v1 bridge refused its crossed edge")
	}
	if crossed.allowsIntentEdge(nativeGapBridgeEdgesTestIntent(25683, "applied"), nativeGapBridgeEdgesTestIntent(25690, "pending")) {
		t.Fatal("live v1 bridge admitted a second gap")
	}
	pending, err := decoded.matchHistory(t.Context(), nativeGapBridgeEdgesTestHistory([]uint64{25647, 25648}, "applied"))
	if err != nil || pending == nil || pending.crossed {
		t.Fatalf("live v1 bridge refused its pending history: %v", err)
	}
	current := nativeGapBridgeEdgesTestIntent(25648, "applied")
	if err := pending.requireFirstEpoch(current, 25681); err != ErrSteeringAlreadyFinal {
		t.Fatalf("live v1 bridge did not idle before F: %v", err)
	}
	if err := pending.requireFirstEpoch(current, 25682); err != nil {
		t.Fatalf("live v1 bridge stayed idle at F: %v", err)
	}
}

func TestNativeGapBridgeEdgesDecodePinsCanonicalList(t *testing.T) {
	t.Parallel()
	wire := nativeGapBridgeEdgesTestWire(nativeGapBridgeEdgesTestList())
	encoded := encodeNativeGapBridgeEdgesTest(t, wire)
	pin := ReleaseMeasurementContentHash(encoded)
	decoded, err := DecodeReleaseNativeGapBridge(encoded, pin)
	if err != nil || !reflect.DeepEqual(*decoded, nativeGapBridgeEdgesTestBridge(nativeGapBridgeEdgesTestList()...)) {
		t.Fatalf("canonical pinned edge list was refused: %v", err)
	}
	if decoded.LastNativeEpoch != 0 || decoded.LastArtifactHash != "" || decoded.FirstNativeEpoch != 0 {
		t.Fatal("v2 bridge filled the v1 edge fields")
	}
	listed := decoded.Edges()
	if !reflect.DeepEqual(listed, nativeGapBridgeEdgesTestList()) {
		t.Fatalf("v2 bridge lists %v", listed)
	}
	listed[0].FirstNativeEpoch++
	if decoded.Edges()[0].FirstNativeEpoch != 25682 {
		t.Fatal("Edges shared the decoded list")
	}
	for _, wrong := range []string{"", ReleaseMeasurementContentHash([]byte("other")), nativeGapBridgeLiveV1Pin, strings.ToUpper(pin)} {
		if _, err := DecodeReleaseNativeGapBridge(encoded, wrong); err == nil {
			t.Fatalf("edge list accepted pin %q", wrong)
		}
	}
	compact, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	// Each malformed body is refused even under its own exact pin.
	for name, body := range map[string][]byte{
		"compact":            compact,
		"no newline":         bytes.TrimSuffix(encoded, []byte("\n")),
		"trailing":           append(bytes.Clone(encoded), []byte("{}\n")...),
		"unknown field":      bytes.Replace(encoded, []byte("{\n"), []byte("{\n  \"extra\": 1,\n"), 1),
		"unknown edge field": bytes.Replace(encoded, []byte("    {\n"), []byte("    {\n      \"extra\": 1,\n"), 1),
		"v1 edge fields":     bytes.Replace(encoded, []byte("  \"edges\""), []byte("  \"last_native_epoch\": 25648,\n  \"edges\""), 1),
		"duplicate field":    bytes.Replace(encoded, []byte("{\n"), []byte("{\n  \"chain_id\": 945,\n"), 1),
		"upper-case key":     bytes.Replace(encoded, []byte("\"schema\""), []byte("\"SCHEMA\""), 1),
		"oversized":          append(bytes.Repeat([]byte(" "), ReleaseNativeGapBridgeMaximumBytes), encoded...),
	} {
		if _, err := DecodeReleaseNativeGapBridge(body, ReleaseMeasurementContentHash(body)); err == nil {
			t.Fatalf("%s edge list body was admitted", name)
		}
	}
	edge := func(last uint64, hash string, first uint64) ReleaseNativeGapBridgeEdge {
		return ReleaseNativeGapBridgeEdge{LastNativeEpoch: last, LastArtifactHash: hash, FirstNativeEpoch: first}
	}
	crossed := nativeGapBridgeEdgesTestList()[0]
	for name, mutate := range map[string]func(*releaseNativeGapBridgeEdgesWire){
		"other schema": func(w *releaseNativeGapBridgeEdgesWire) { w.Schema = "urnetwork-validator-native-gap-bridge-v3" },
		"v1 schema":    func(w *releaseNativeGapBridgeEdgesWire) { w.Schema = ReleaseNativeGapBridgeSchema },
		"chain":        func(w *releaseNativeGapBridgeEdgesWire) { w.ChainID = 0 },
		"deployment":   func(w *releaseNativeGapBridgeEdgesWire) { w.DeploymentID = " " },
		"validator":    func(w *releaseNativeGapBridgeEdgesWire) { w.ValidatorID = 0 },
		"null edges":   func(w *releaseNativeGapBridgeEdgesWire) { w.Edges = nil },
		"empty edges":  func(w *releaseNativeGapBridgeEdgesWire) { w.Edges = []ReleaseNativeGapBridgeEdge{} },
		"last epoch":   func(w *releaseNativeGapBridgeEdgesWire) { w.Edges[1].LastNativeEpoch = 0 },
		"hash spelling": func(w *releaseNativeGapBridgeEdgesWire) {
			w.Edges[1].LastArtifactHash = "0x" + strings.Repeat("ab", 32)
		},
		"uppercase hash": func(w *releaseNativeGapBridgeEdgesWire) {
			w.Edges[0].LastArtifactHash = strings.ToUpper(w.Edges[0].LastArtifactHash)
		},
		"no gap":         func(w *releaseNativeGapBridgeEdgesWire) { w.Edges[1].FirstNativeEpoch = w.Edges[1].LastNativeEpoch + 1 },
		"regressed":      func(w *releaseNativeGapBridgeEdgesWire) { w.Edges[1].FirstNativeEpoch = w.Edges[1].LastNativeEpoch },
		"maximum":        func(w *releaseNativeGapBridgeEdgesWire) { w.Edges[1].FirstNativeEpoch = ^uint64(0) },
		"out of order":   func(w *releaseNativeGapBridgeEdgesWire) { w.Edges[0], w.Edges[1] = w.Edges[1], w.Edges[0] },
		"duplicate edge": func(w *releaseNativeGapBridgeEdgesWire) { w.Edges = []ReleaseNativeGapBridgeEdge{crossed, crossed} },
		"overlapping": func(w *releaseNativeGapBridgeEdgesWire) {
			w.Edges[1] = edge(25681, nativeGapBridgeTestArtifactHash(25681), 25690)
		},
		"same predecessor": func(w *releaseNativeGapBridgeEdgesWire) {
			w.Edges[1] = edge(crossed.LastNativeEpoch, crossed.LastArtifactHash, 25700)
		},
	} {
		changed := nativeGapBridgeEdgesTestWire(nativeGapBridgeEdgesTestList())
		mutate(&changed)
		body := encodeNativeGapBridgeEdgesTest(t, changed)
		if _, err := DecodeReleaseNativeGapBridge(body, ReleaseMeasurementContentHash(body)); err == nil {
			t.Fatalf("%s edge list scope was admitted", name)
		}
	}
	// A second edge may start exactly at the first bridged epoch.
	adjacent := nativeGapBridgeEdgesTestWire([]ReleaseNativeGapBridgeEdge{crossed, edge(25682, nativeGapBridgeTestArtifactHash(25682), 25690)})
	body := encodeNativeGapBridgeEdgesTest(t, adjacent)
	if _, err := DecodeReleaseNativeGapBridge(body, ReleaseMeasurementContentHash(body)); err != nil {
		t.Fatalf("adjacent edges were refused: %v", err)
	}
	// The existing 4 KiB bound leaves room for many later gaps.
	many := []ReleaseNativeGapBridgeEdge{crossed}
	for index := uint64(1); index < 20; index++ {
		last := 25700 + 10*index
		many = append(many, edge(last, nativeGapBridgeTestArtifactHash(last), last+2))
	}
	body = encodeNativeGapBridgeEdgesTest(t, nativeGapBridgeEdgesTestWire(many))
	if len(body) > ReleaseNativeGapBridgeMaximumBytes {
		t.Fatalf("twenty edges take %d bytes", len(body))
	}
	if decoded, err := DecodeReleaseNativeGapBridge(body, ReleaseMeasurementContentHash(body)); err != nil || len(decoded.Edges()) != 20 {
		t.Fatalf("twenty edges were refused: %v", err)
	}
}

// The same flags carry a v2 file; the config owner gets its own copy.
func TestNativeGapBridgeEdgesUseTheSameFlagsAndOwner(t *testing.T) {
	t.Parallel()
	encoded := encodeNativeGapBridgeEdgesTest(t, nativeGapBridgeEdgesTestWire(nativeGapBridgeEdgesTestList()))
	pin := ReleaseMeasurementContentHash(encoded)
	path := filepath.Join(t.TempDir(), "native-gap-bridge.json")
	if err := os.WriteFile(path, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=/etc/sn-validator/validator.yml", "--native-gap-bridge=" + path, "--native-gap-bridge-sha256=" + pin})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := nativeGapBridgeFromOpts(opts)
	want := nativeGapBridgeEdgesTestBridge(nativeGapBridgeEdgesTestList()...)
	if err != nil || bridge == nil || !reflect.DeepEqual(*bridge, want) {
		t.Fatalf("pinned edge list flag was refused: %v", err)
	}
	owner := func() *ReleaseConfig {
		return &ReleaseConfig{ChainID: 964, DeploymentID: "sn25-mainnet-20261007-01", ValidatorID: 1, StateDir: "/srv/sn25/sn-validator/durable/validator/state"}
	}
	cfg := owner()
	if err := bridge.configure(cfg); err != nil || !reflect.DeepEqual(*cfg.nativeGapBridge, want) {
		t.Fatalf("exact owner refused the edge list: %v", err)
	}
	bridge.edges[0].FirstNativeEpoch++
	if cfg.nativeGapBridge.edges[0].FirstNativeEpoch != 25682 {
		t.Fatal("borrowed edge list mutation changed the configured owner")
	}
	before, err := OwnerRecycleConfigHash(owner())
	if err != nil {
		t.Fatal(err)
	}
	if after, err := OwnerRecycleConfigHash(cfg); err != nil || before != after {
		t.Fatalf("edge list changed the config hash: %v", err)
	}
	if err := want.configure(cfg); err == nil {
		t.Fatal("a configured owner accepted a second bridge file")
	}
	foreign := owner()
	foreign.ChainID = 945
	if err := want.configure(foreign); err == nil {
		t.Fatal("a foreign chain accepted the edge list")
	}
}

// Every edge but the last must match history exactly and in order; the last
// may be pending or crossed. Crossed edges may not be altered.
func TestNativeGapBridgeEdgesMatchRetainedHistory(t *testing.T) {
	t.Parallel()
	edges := nativeGapBridgeEdgesTestList()
	bridge := nativeGapBridgeEdgesTestBridge(edges...)
	pending, err := bridge.matchHistory(t.Context(), nativeGapBridgeEdgesTestHistory([]uint64{25647, 25648, 25683, 25700}, "applied"))
	if err != nil || pending == nil || pending.crossed || !reflect.DeepEqual(pending.bridge, bridge) {
		t.Fatalf("pending second edge was refused: %v", err)
	}
	current := nativeGapBridgeEdgesTestIntent(25700, "applied")
	for _, epoch := range []uint64{25700, 25701, 25704} {
		if err := pending.requireFirstEpoch(current, epoch); err != ErrSteeringAlreadyFinal {
			t.Fatalf("native epoch %d did not idle before the pending F: %v", epoch, err)
		}
	}
	if err := pending.requireFirstEpoch(current, 25705); err != nil {
		t.Fatalf("pending edge stayed idle at its F: %v", err)
	}
	// Only the pending edge can idle the release.
	if err := pending.requireFirstEpoch(nativeGapBridgeEdgesTestIntent(25648, "applied"), 25650); err != nil {
		t.Fatalf("a crossed edge idled the release: %v", err)
	}
	crossed, err := bridge.matchHistory(t.Context(), nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700, 25706}, "pending"))
	if err != nil || crossed == nil || !crossed.crossed {
		t.Fatalf("crossed second edge was refused: %v", err)
	}
	edge := func(last uint64, hash string, first uint64) ReleaseNativeGapBridgeEdge {
		return ReleaseNativeGapBridgeEdge{LastNativeEpoch: last, LastArtifactHash: hash, FirstNativeEpoch: first}
	}
	failed := nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700}, "failed")
	failed[2].Error = "expired"
	for name, test := range map[string]struct {
		bridge ReleaseNativeGapBridge
		all    []*SteeringIntent
	}{
		"first edge not crossed":    {bridge, nativeGapBridgeEdgesTestHistory([]uint64{25647, 25648}, "applied")},
		"second predecessor absent": {bridge, nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25699}, "applied")},
		"unapplied second":          {bridge, failed},
		"crossed edge later F":      {nativeGapBridgeEdgesTestBridge(edge(25648, nativeGapBridgeLiveV1Hash, 25690), edges[1]), nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700}, "applied")},
		"crossed edge other hash":   {nativeGapBridgeEdgesTestBridge(edge(25648, nativeGapBridgeTestArtifactHash(25648), 25682), edges[1]), nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700}, "applied")},
		"out of history order":      {nativeGapBridgeEdgesTestBridge(edges[1], edges[0]), nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700, 25706}, "pending")},
		"duplicate edges":           {nativeGapBridgeEdgesTestBridge(edges[0], edges[0]), nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700}, "applied")},
		"second successor before F": {bridge, nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700, 25704}, "pending")},
		"empty history":             {bridge, nil},
	} {
		if owner, err := test.bridge.matchHistory(t.Context(), test.all); owner != nil || err == nil {
			t.Fatalf("%s matched the edge list", name)
		}
	}
}

// Each site admits exactly the listed edges and keeps the ordinary refusals.
func TestNativeGapBridgeEdgesAdmitOnlyListedEdges(t *testing.T) {
	t.Parallel()
	bridge := nativeGapBridgeEdgesTestBridge(nativeGapBridgeEdgesTestList()...)
	owner, err := bridge.matchHistory(t.Context(), nativeGapBridgeEdgesTestHistory([]uint64{25648, 25683, 25700, 25706}, "pending"))
	if err != nil {
		t.Fatal(err)
	}
	intent := nativeGapBridgeEdgesTestIntent
	admit := func(previous, current *SteeringIntent) error {
		return validateSteeringIntentSuccessorWithGapsV2(previous, current, (&releaseIntentV2Owner{nativeGapBridge: owner}).allowsIntentGap(previous, current))
	}
	for _, pair := range [][2]*SteeringIntent{
		{intent(25648, "applied"), intent(25683, "applied")},
		{intent(25700, "applied"), intent(25705, "pending")},
		{intent(25700, "applied"), intent(25706, "pending")},
		{intent(25683, "applied"), intent(25684, "pending")},
	} {
		if err := admit(pair[0], pair[1]); err != nil {
			t.Fatalf("listed or consecutive edge %d -> %d was refused: %v", pair[0].SubnetEpoch, pair[1].SubnetEpoch, err)
		}
	}
	other := intent(25700, "applied")
	other.MeasurementArtifactHash = nativeGapBridgeTestArtifactHash(1)
	failedFirst := intent(25648, "failed")
	failedFirst.Error = "expired"
	for name, pair := range map[string][2]*SteeringIntent{
		"before the second F": {intent(25700, "applied"), intent(25704, "pending")},
		"before the first F":  {intent(25648, "applied"), intent(25681, "pending")},
		"unlisted gap":        {intent(25683, "applied"), intent(25690, "pending")},
		"gap after the last":  {intent(25706, "applied"), intent(25708, "pending")},
		"other hash":          {other, intent(25706, "pending")},
		"unapplied":           {failedFirst, intent(25683, "pending")},
	} {
		if err := admit(pair[0], pair[1]); err == nil {
			t.Fatalf("edge list admitted the %s", name)
		}
	}
	// The artifact edges name the same predecessor content and epochs.
	for _, edge := range nativeGapBridgeEdgesTestList() {
		previous := &ReleaseMeasurementArtifact{SubnetEpoch: edge.LastNativeEpoch}
		current := &ReleaseMeasurementArtifact{SubnetEpoch: edge.FirstNativeEpoch, PreviousArtifactHash: edge.LastArtifactHash}
		if !owner.allowsArtifactEdge(edge.LastArtifactHash, previous, current) {
			t.Fatalf("listed artifact edge from %d was refused", edge.LastNativeEpoch)
		}
		early := *current
		early.SubnetEpoch = edge.FirstNativeEpoch - 1
		unlinked := *current
		unlinked.PreviousArtifactHash = nativeGapBridgeTestArtifactHash(2)
		if owner.allowsArtifactEdge(edge.LastArtifactHash, previous, &early) || owner.allowsArtifactEdge(edge.LastArtifactHash, previous, &unlinked) ||
			owner.allowsArtifactEdge(nativeGapBridgeTestArtifactHash(2), previous, current) {
			t.Fatalf("artifact edge from %d admitted an early, unlinked or foreign successor", edge.LastNativeEpoch)
		}
	}
	unlisted := &ReleaseMeasurementArtifact{SubnetEpoch: 25683}
	if owner.allowsArtifactEdge(nativeGapBridgeTestArtifactHash(25683), unlisted, &ReleaseMeasurementArtifact{SubnetEpoch: 25690, PreviousArtifactHash: nativeGapBridgeTestArtifactHash(25683)}) {
		t.Fatal("edge list admitted an unlisted artifact gap")
	}
	for _, test := range []struct {
		last, epoch uint64
		want        bool
	}{{25648, 25683, true}, {25700, 25705, true}, {25700, 25704, false}, {25648, 25681, false}, {25683, 25690, false}, {25706, 25708, false}} {
		if owner.allowsHeadEMAEdge(test.last, test.epoch) != test.want {
			t.Fatalf("head EMA edge %d -> %d: want %v", test.last, test.epoch, test.want)
		}
	}
	// The compact head admission folds the second edge's next input once.
	limits := headEMAStoreV2TestLimits()
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	if _, _, err := store.FoldForEpochV2(t.Context(), 25700, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha); err != nil {
		t.Fatal(err)
	}
	budget := releaseHeadV2Budget{limit: limits.MaxControlBytes}
	if err := store.admitReleaseHeadV2Known(t.Context(), 25705, alpha, limits.MaxEntries, budget); err == nil || !strings.Contains(err.Error(), "compact head EMA epoch jumped") {
		t.Fatalf("unbridged head admission changed: %v", err)
	}
	store.v2.nativeGapBridge = owner
	if err := store.admitReleaseHeadV2Known(t.Context(), 25704, alpha, limits.MaxEntries, budget); err == nil {
		t.Fatal("edge list admitted a head epoch before the second F")
	}
	if err := store.admitReleaseHeadV2Known(t.Context(), 25705, alpha, limits.MaxEntries, budget); err != nil {
		t.Fatalf("second edge head admission failed: %v", err)
	}
}

// Restart after crossing two edges: startup reference replay accepts both
// listed gaps and refuses a file that omits or alters either one.
func TestNativeGapBridgeEdgesStartupReplay(t *testing.T) {
	t.Parallel()
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	value := func(n int) RationalJSON { return RationalJSON{Numerator: fmt.Sprint(n), Denominator: "1"} }
	epochs := []uint64{25647, 25648, 25683, 25684, 25685, 25700, 25701}
	intents := make([]*SteeringIntent, len(epochs))
	artifacts := make([]*ReleaseMeasurementArtifact, len(epochs))
	for index, epoch := range epochs {
		intents[index] = nativeGapBridgeTestIntent(epoch, "applied")
		artifacts[index] = &ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, SubnetEpoch: epoch, SettlementEpoch: 1,
			NativeSnapshotBlock: 1000 + epoch, NativeSnapshotHash: releaseHex32([32]byte{0x51, byte(index)}),
			EVMSnapshotBlock: 500 + uint64(index), EVMSnapshotHash: releaseHex32([32]byte{0x41, byte(index)}),
			HeadEMA: []HeadEMAMeasurement{{Key: key, HasRaw: true, Raw: value(2), HasPrior: index > 0, Prior: value(index + 1), Next: value(index + 2)}}}
		if index == 0 {
			artifacts[index].HeadEMA[0].Prior = value(0)
		} else {
			artifacts[index].PreviousArtifactHash = intents[index-1].MeasurementArtifactHash
		}
	}
	intents[len(intents)-1].Status = "pending"
	edge := func(last, first uint64) ReleaseNativeGapBridgeEdge {
		return ReleaseNativeGapBridgeEdge{LastNativeEpoch: last, LastArtifactHash: nativeGapBridgeTestArtifactHash(last), FirstNativeEpoch: first}
	}
	first, second := edge(25648, 25682), edge(25685, 25700)
	replay := func(bridge ReleaseNativeGapBridge) error {
		owner, err := bridge.matchHistory(t.Context(), intents)
		if err != nil {
			return err
		}
		history := &releaseEvidenceV2StartupHistory{nativeGapBridge: owner}
		read := &releaseIntentV2Owner{nativeGapBridge: owner}
		for index := 1; index < len(intents); index++ {
			previous, intent := intents[index-1], intents[index]
			allowGap := history.historyAdoption.allowsIntentEdge(previous, intent) || history.nativeGapBridge.allowsIntentEdge(previous, intent)
			if err := validateSteeringIntentSuccessorWithGapsV2(previous, intent, allowGap); err != nil {
				return err
			}
			if err := verifyStartupArtifactSuccessorV2(previous, artifacts[index-1], artifacts[index], allowGap); err != nil {
				return err
			}
			if err := validateSteeringIntentSuccessorWithGapsV2(previous, intent, read.allowsIntentGap(previous, intent)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := replay(nativeGapBridgeEdgesTestBridge(first, second)); err != nil {
		t.Fatalf("restart refused two crossed edges: %v", err)
	}
	v1 := nativeGapBridgeTest()
	v1.LastNativeEpoch, v1.LastArtifactHash, v1.FirstNativeEpoch = first.LastNativeEpoch, first.LastArtifactHash, first.FirstNativeEpoch
	for name, bridge := range map[string]ReleaseNativeGapBridge{
		"v1 file without the second edge": v1,
		"missing the crossed first edge":  nativeGapBridgeEdgesTestBridge(second),
		"first edge with a later F":       nativeGapBridgeEdgesTestBridge(edge(25648, 25690), second),
		"second edge with a later F":      nativeGapBridgeEdgesTestBridge(first, edge(25685, 25701)),
	} {
		if err := replay(bridge); err == nil {
			t.Fatalf("restart accepted the %s", name)
		}
	}
}

// The strict lineage replay crosses whichever listed edge names its endpoints.
func TestNativeGapBridgeEdgesLineageUsesTheListedEdge(t *testing.T) {
	t.Parallel()
	previous := newReleaseMeasurementV2TestFixture(t, 2)
	previousBytes, err := canonicalReleaseMeasurementBytes(previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	L := previous.artifact.SubnetEpoch
	alpha := previous.artifact.Policy.Steering.HeadScoreEMA
	earlier := ReleaseNativeGapBridgeEdge{LastNativeEpoch: L - 20, LastArtifactHash: nativeGapBridgeTestArtifactHash(L - 20), FirstNativeEpoch: L - 10}
	listed := ReleaseNativeGapBridgeEdge{LastNativeEpoch: L, LastArtifactHash: ReleaseMeasurementContentHash(previousBytes), FirstNativeEpoch: L + 3}
	owner := &releaseNativeGapBridgeV2{bridge: nativeGapBridgeEdgesTestBridge(earlier, listed)}
	limits := headEMAStoreV2TestLimits()
	head, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := head.CommitForEpochV2(t.Context(), L, previous.artifact.HeadEMA, alpha); err != nil {
		t.Fatal(err)
	}
	head.v2.nativeGapBridge = owner
	raw, err := rawHeadEMAInputs(previous.artifact.HeadEMA)
	if err != nil || len(raw) == 0 {
		t.Fatalf("fixture lacks a real head EMA input: %v", err)
	}
	current := cloneReleaseMeasurementArtifact(t, previous.artifact)
	current.SubnetEpoch = L + 5
	current.NativeSnapshotBlock += 5 * 360
	current.NativeSnapshotHash = releaseHex32([32]byte{0x53})
	current.PreviousArtifactHash = ReleaseMeasurementContentHash(previousBytes)
	if _, current.HeadEMA, err = head.PreviewForEpochV2(t.Context(), current.SubnetEpoch, raw, alpha); err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{nativeGapBridge: owner}
	for _, input := range previous.artifact.Inputs {
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: input.NoID, Ledger: previous.operators[input.NoID].seal.ledger})
	}
	runtime := &releaseRuntimeV2{history: history, gate: make(chan struct{}, 1)}
	store := &IntentStore{v2: &releaseIntentV2Owner{runtime: runtime, nativeGapBridge: owner}}
	verify := func() error {
		options := previous.options(t)
		options.Expected = releaseMeasurementV2Decision(current)
		return store.verifyMeasurementLineageV2(t.Context(), previousBytes, previous.options(t), current, options)
	}
	if err := verify(); err != nil {
		t.Fatalf("strict lineage refused the listed second edge: %v", err)
	}
	for name, edges := range map[string][]ReleaseNativeGapBridgeEdge{
		"only the earlier edge": {earlier},
		"listed edge after F":   {earlier, {LastNativeEpoch: L, LastArtifactHash: listed.LastArtifactHash, FirstNativeEpoch: current.SubnetEpoch + 1}},
	} {
		other := &releaseNativeGapBridgeV2{bridge: nativeGapBridgeEdgesTestBridge(edges...)}
		store.v2.nativeGapBridge, history.nativeGapBridge = other, other
		if err := verify(); err == nil {
			t.Fatalf("strict lineage admitted the gap with %s", name)
		}
	}
	store.v2.nativeGapBridge, history.nativeGapBridge = owner, owner
	if err := verify(); err != nil {
		t.Fatalf("restored edge list refused the lineage: %v", err)
	}
}
