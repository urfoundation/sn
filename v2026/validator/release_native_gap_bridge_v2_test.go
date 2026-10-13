//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The production shape: the retained applied intent at native 25648 and a
// first resumable native epoch after the missed window.
const (
	nativeGapBridgeTestLast  = uint64(25648)
	nativeGapBridgeTestFirst = uint64(25665)
)

func nativeGapBridgeTest() ReleaseNativeGapBridge {
	return ReleaseNativeGapBridge{Schema: ReleaseNativeGapBridgeSchema, ChainID: 964, DeploymentID: "sn25-mainnet-20261007-01", ValidatorID: 1,
		LastNativeEpoch: nativeGapBridgeTestLast, LastArtifactHash: nativeGapBridgeTestArtifactHash(nativeGapBridgeTestLast), FirstNativeEpoch: nativeGapBridgeTestFirst}
}

func nativeGapBridgeTestArtifactHash(epoch uint64) string {
	return ReleaseMeasurementContentHash([]byte(fmt.Sprintf("artifact-%d", epoch)))
}

// These inert intents exercise only the edge rules; real startup and the V2
// owner additionally authenticate every lifecycle, vector, artifact and source.
func nativeGapBridgeTestIntent(epoch uint64, status string) *SteeringIntent {
	return &SteeringIntent{SubnetEpoch: epoch, SettlementEpoch: 1, Status: status, MeasurementArtifactHash: nativeGapBridgeTestArtifactHash(epoch)}
}

func encodeNativeGapBridgeTest(t *testing.T, bridge ReleaseNativeGapBridge) []byte {
	t.Helper()
	encoded, err := json.MarshalIndent(bridge, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func TestNativeGapBridgeDecodePinsCanonicalScope(t *testing.T) {
	t.Parallel()
	bridge := nativeGapBridgeTest()
	encoded := encodeNativeGapBridgeTest(t, bridge)
	pin := ReleaseMeasurementContentHash(encoded)
	decoded, err := DecodeReleaseNativeGapBridge(encoded, pin)
	if err != nil || !reflect.DeepEqual(*decoded, bridge) {
		t.Fatalf("canonical pinned bridge was refused: %v", err)
	}
	for _, wrong := range []string{"", ReleaseMeasurementContentHash([]byte("other")), strings.ToUpper(pin), strings.TrimPrefix(pin, "sha256:"), pin + "0"} {
		if _, err := DecodeReleaseNativeGapBridge(encoded, wrong); err == nil {
			t.Fatalf("bridge accepted pin %q", wrong)
		}
	}
	compact, err := json.Marshal(bridge)
	if err != nil {
		t.Fatal(err)
	}
	// Each malformed body is refused even under its own exact pin.
	for name, body := range map[string][]byte{
		"compact":         compact,
		"no newline":      bytes.TrimSuffix(encoded, []byte("\n")),
		"trailing":        append(bytes.Clone(encoded), []byte("{}\n")...),
		"unknown field":   bytes.Replace(encoded, []byte("{\n"), []byte("{\n  \"extra\": 1,\n"), 1),
		"duplicate field": bytes.Replace(encoded, []byte("{\n"), []byte("{\n  \"chain_id\": 945,\n"), 1),
		"empty":           nil,
		"oversized":       append(bytes.Repeat([]byte(" "), ReleaseNativeGapBridgeMaximumBytes), encoded...),
	} {
		if _, err := DecodeReleaseNativeGapBridge(body, ReleaseMeasurementContentHash(body)); err == nil {
			t.Fatalf("%s bridge body was admitted", name)
		}
	}
	for name, mutate := range map[string]func(*ReleaseNativeGapBridge){
		"schema":         func(b *ReleaseNativeGapBridge) { b.Schema = ReleaseHistoryAdoptionV2Schema },
		"chain":          func(b *ReleaseNativeGapBridge) { b.ChainID = 0 },
		"deployment":     func(b *ReleaseNativeGapBridge) { b.DeploymentID = " " },
		"validator":      func(b *ReleaseNativeGapBridge) { b.ValidatorID = 0 },
		"last epoch":     func(b *ReleaseNativeGapBridge) { b.LastNativeEpoch = 0 },
		"hash spelling":  func(b *ReleaseNativeGapBridge) { b.LastArtifactHash = "0x" + strings.Repeat("ab", 32) },
		"uppercase hash": func(b *ReleaseNativeGapBridge) { b.LastArtifactHash = strings.ToUpper(b.LastArtifactHash) },
		"no gap":         func(b *ReleaseNativeGapBridge) { b.FirstNativeEpoch = b.LastNativeEpoch + 1 },
		"regressed":      func(b *ReleaseNativeGapBridge) { b.FirstNativeEpoch = b.LastNativeEpoch },
		"maximum":        func(b *ReleaseNativeGapBridge) { b.FirstNativeEpoch = ^uint64(0) },
	} {
		changed := bridge
		mutate(&changed)
		body := encodeNativeGapBridgeTest(t, changed)
		if _, err := DecodeReleaseNativeGapBridge(body, ReleaseMeasurementContentHash(body)); err == nil {
			t.Fatalf("%s bridge scope was admitted", name)
		}
	}
}

func TestNativeGapBridgeReadsOnlyThePinnedRegularFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	encoded := encodeNativeGapBridgeTest(t, nativeGapBridgeTest())
	pin := ReleaseMeasurementContentHash(encoded)
	path := filepath.Join(root, "native-gap-bridge.json")
	if err := os.WriteFile(path, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
	bridge, err := ReadReleaseNativeGapBridge(path, pin)
	if err != nil || !reflect.DeepEqual(*bridge, nativeGapBridgeTest()) {
		t.Fatalf("pinned regular bridge file was refused: %v", err)
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(root, "large.json")
	if err := os.WriteFile(large, bytes.Repeat([]byte(" "), ReleaseNativeGapBridgeMaximumBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]string{
		"relative":  "native-gap-bridge.json",
		"unclean":   root + "/./native-gap-bridge.json",
		"symlink":   link,
		"directory": root,
		"missing":   filepath.Join(root, "absent.json"),
		"oversized": large,
	} {
		if _, err := ReadReleaseNativeGapBridge(candidate, pin); err == nil {
			t.Fatalf("%s bridge path was read", name)
		}
	}
	if _, err := ReadReleaseNativeGapBridge(path, ReleaseMeasurementContentHash([]byte("changed"))); err == nil {
		t.Fatal("bridge file was admitted under a different pin")
	}
}

func TestNativeGapBridgeConfigureRequiresItsExactOwner(t *testing.T) {
	t.Parallel()
	bridge := nativeGapBridgeTest()
	owner := func() *ReleaseConfig {
		return &ReleaseConfig{ChainID: 964, DeploymentID: bridge.DeploymentID, ValidatorID: bridge.ValidatorID, StateDir: "/srv/sn25/sn-validator/durable/validator/state"}
	}
	cfg := owner()
	source := bridge
	if err := source.configure(cfg); err != nil || cfg.nativeGapBridge == nil || !reflect.DeepEqual(*cfg.nativeGapBridge, bridge) {
		t.Fatalf("exact owner refused the bridge: %v", err)
	}
	source.FirstNativeEpoch++
	if cfg.nativeGapBridge.FirstNativeEpoch != bridge.FirstNativeEpoch {
		t.Fatal("borrowed bridge mutation changed the configured owner")
	}
	// The bridge is not part of the signed config's schema or hash.
	before, err := OwnerRecycleConfigHash(owner())
	if err != nil {
		t.Fatal(err)
	}
	after, err := OwnerRecycleConfigHash(cfg)
	if err != nil || before != after {
		t.Fatalf("bridge changed the config hash: %v", err)
	}
	plain, err := json.Marshal(owner())
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(cfg)
	if err != nil || !bytes.Equal(plain, wire) {
		t.Fatalf("bridge leaked into the config wire form: %v", err)
	}
	for name, mutate := range map[string]func(*ReleaseConfig){
		"testnet chain":         func(c *ReleaseConfig) { c.ChainID = 945 },
		"deployment":            func(c *ReleaseConfig) { c.DeploymentID = "sn25-mainnet-20261007-02" },
		"validator":             func(c *ReleaseConfig) { c.ValidatorID = 2 },
		"provisional deferral":  func(c *ReleaseConfig) { c.ProvisionalDeferClosedNativeInput = true },
		"runtime compatibility": func(c *ReleaseConfig) { c.ProvisionalRuntimeCompatibility = "reviewed-predecessor" },
		"history adoption":      func(c *ReleaseConfig) { c.historyAdoptionV2 = &ReleaseHistoryAdoptionV2{} },
		"second bridge":         func(c *ReleaseConfig) { c.nativeGapBridge = &ReleaseNativeGapBridge{} },
	} {
		changed := owner()
		mutate(changed)
		if err := bridge.configure(changed); err == nil {
			t.Fatalf("%s owner accepted the bridge", name)
		}
	}
}

func TestNativeGapBridgeCLIRequiresPathAndPinTogether(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	encoded := encodeNativeGapBridgeTest(t, nativeGapBridgeTest())
	path := filepath.Join(root, "native-gap-bridge.json")
	if err := os.WriteFile(path, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
	pin := ReleaseMeasurementContentHash(encoded)
	usage := mainUsage()
	// The deploy playbook probes the binary for each unit flag's spelling.
	if !strings.Contains(usage, "--native-gap-bridge=<path>") || !strings.Contains(usage, "--native-gap-bridge-sha256=<hash>") {
		t.Fatal("run usage lacks the bridge flags")
	}
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=/etc/sn-validator/validator.yml", "--progress-file=/var/lib/sn-validator-progress/progress.json",
		"--durable-volumes=/etc/sn-validator/durable-volumes.json", "--durable-volumes-sha256=" + pin,
		"--native-gap-bridge=" + path, "--native-gap-bridge-sha256=" + pin, "--operators-refresh=1h"})
	if err != nil {
		t.Fatalf("production run with a bridge did not parse: %v", err)
	}
	bridge, err := nativeGapBridgeFromOpts(opts)
	if err != nil || bridge == nil || !reflect.DeepEqual(*bridge, nativeGapBridgeTest()) {
		t.Fatalf("pinned bridge flag was refused: %v", err)
	}
	plain, err := parseValidatorArgsForTest(t, []string{"run", "--config=/etc/sn-validator/validator.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if bridge, err := nativeGapBridgeFromOpts(plain); bridge != nil || err != nil {
		t.Fatalf("release without the flag gained a bridge: %v", err)
	}
	for _, args := range [][]string{
		{"run", "--config=/etc/sn-validator/validator.yml", "--native-gap-bridge=" + path},
		{"run", "--config=/etc/sn-validator/validator.yml", "--native-gap-bridge-sha256=" + pin},
		{"run", "--all-operators", "--native-gap-bridge=" + path, "--native-gap-bridge-sha256=" + pin},
	} {
		if _, err := parseValidatorArgsForTest(t, args); err == nil {
			t.Fatalf("usage accepted %v", args)
		}
	}
	for _, opts := range []docopt.Opts{
		{"--native-gap-bridge": path},
		{"--native-gap-bridge-sha256": pin},
		{"--native-gap-bridge": path, "--native-gap-bridge-sha256": ReleaseMeasurementContentHash(nil)},
	} {
		if _, err := nativeGapBridgeFromOpts(opts); err == nil {
			t.Fatalf("incomplete or mispinned bridge options were admitted: %v", opts)
		}
	}
}

func TestNativeGapBridgeMatchesRetainedHistory(t *testing.T) {
	t.Parallel()
	bridge := nativeGapBridgeTest()
	const L, F = nativeGapBridgeTestLast, nativeGapBridgeTestFirst
	pending, err := bridge.matchHistory(t.Context(), []*SteeringIntent{nativeGapBridgeTestIntent(L-2, "applied"), nativeGapBridgeTestIntent(L-1, "applied"), nativeGapBridgeTestIntent(L, "applied")})
	if err != nil || pending == nil || pending.crossed || !reflect.DeepEqual(pending.bridge, bridge) {
		t.Fatalf("current bridged predecessor was refused: %v", err)
	}
	for _, next := range []uint64{F, F + 9} {
		crossed, err := bridge.matchHistory(t.Context(), []*SteeringIntent{nativeGapBridgeTestIntent(L-1, "applied"), nativeGapBridgeTestIntent(L, "applied"), nativeGapBridgeTestIntent(next, "applied"), nativeGapBridgeTestIntent(next+1, "pending")})
		if err != nil || crossed == nil || !crossed.crossed {
			t.Fatalf("crossed bridge history at %d was refused: %v", next, err)
		}
	}
	if owner, err := (*ReleaseNativeGapBridge)(nil).matchHistory(t.Context(), nil); owner != nil || err != nil {
		t.Fatal("absent bridge produced an owner")
	}
	foreign := nativeGapBridgeTestIntent(L, "applied")
	foreign.MeasurementArtifactHash = nativeGapBridgeTestArtifactHash(L - 1)
	failed := nativeGapBridgeTestIntent(L, "failed")
	failed.Error = "expired"
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for name, test := range map[string]struct {
		ctx context.Context
		all []*SteeringIntent
	}{
		"empty history":         {t.Context(), nil},
		"absent predecessor":    {t.Context(), []*SteeringIntent{nativeGapBridgeTestIntent(L-1, "applied")}},
		"different hash":        {t.Context(), []*SteeringIntent{foreign}},
		"unapplied predecessor": {t.Context(), []*SteeringIntent{failed}},
		"consecutive successor": {t.Context(), []*SteeringIntent{nativeGapBridgeTestIntent(L, "applied"), nativeGapBridgeTestIntent(L+1, "applied")}},
		"successor before F":    {t.Context(), []*SteeringIntent{nativeGapBridgeTestIntent(L, "applied"), nativeGapBridgeTestIntent(F-1, "applied")}},
		"cancelled":             {cancelled, []*SteeringIntent{nativeGapBridgeTestIntent(L, "applied")}},
	} {
		if owner, err := bridge.matchHistory(test.ctx, test.all); owner != nil || err == nil {
			t.Fatalf("%s matched the bridge", name)
		}
	}
}

// Accept once; refuse a second gap, a different L or hash, an edge into an
// epoch before F, and keep the ordinary refusal when no bridge is supplied.
func TestNativeGapBridgeAdmitsOneIntentEdge(t *testing.T) {
	t.Parallel()
	const L, F = nativeGapBridgeTestLast, nativeGapBridgeTestFirst
	bridge := nativeGapBridgeTest()
	predecessor := nativeGapBridgeTestIntent(L, "applied")
	owner, err := bridge.matchHistory(t.Context(), []*SteeringIntent{nativeGapBridgeTestIntent(L-1, "applied"), predecessor})
	if err != nil {
		t.Fatal(err)
	}
	admit := func(bridged *releaseNativeGapBridgeV2, previous, current *SteeringIntent) error {
		return validateSteeringIntentSuccessorWithGapsV2(previous, current, (&releaseIntentV2Owner{nativeGapBridge: bridged}).allowsIntentGap(previous, current))
	}
	first := nativeGapBridgeTestIntent(F, "applied")
	for _, current := range []*SteeringIntent{first, nativeGapBridgeTestIntent(F+40, "pending")} {
		if err := admit(owner, predecessor, current); err != nil {
			t.Fatalf("bridged edge into %d was refused: %v", current.SubnetEpoch, err)
		}
	}
	if err := admit(nil, predecessor, first); err == nil || !strings.Contains(err.Error(), "not consecutive") {
		t.Fatalf("missing bridge changed the ordinary gap refusal: %v", err)
	}
	for _, epoch := range []uint64{L + 2, L + 13, F - 1} {
		if err := admit(owner, predecessor, nativeGapBridgeTestIntent(epoch, "pending")); err == nil {
			t.Fatalf("bridge admitted an edge into %d before F", epoch)
		}
	}
	foreign := *predecessor
	foreign.MeasurementArtifactHash = nativeGapBridgeTestArtifactHash(1)
	failed := *predecessor
	failed.Status, failed.Error = "failed", "expired"
	for name, previous := range map[string]*SteeringIntent{"different hash": &foreign, "different L": nativeGapBridgeTestIntent(L-1, "applied"), "unapplied predecessor": &failed} {
		if err := admit(owner, previous, first); err == nil {
			t.Fatalf("bridge admitted a %s", name)
		}
	}
	// After crossing, every edge is ordinary: no second gap.
	if err := admit(owner, first, nativeGapBridgeTestIntent(F+2, "pending")); err == nil {
		t.Fatal("bridge admitted a second gap")
	}
	if err := admit(owner, first, nativeGapBridgeTestIntent(F+1, "pending")); err != nil {
		t.Fatalf("consecutive successor after the bridge was refused: %v", err)
	}
	failedFirst := *first
	failedFirst.Status, failedFirst.Error = "failed", "expired"
	if err := admit(owner, &failedFirst, nativeGapBridgeTestIntent(F, "pending")); err != nil {
		t.Fatalf("same-epoch retry after a failed bridged intent was refused: %v", err)
	}
	regressed := nativeGapBridgeTestIntent(F, "pending")
	regressed.SettlementEpoch = 0
	if err := admit(owner, predecessor, regressed); err == nil {
		t.Fatal("bridge admitted a settlement regression")
	}
	// The artifact edge names the same predecessor content and epochs.
	previous := &ReleaseMeasurementArtifact{SubnetEpoch: L}
	current := &ReleaseMeasurementArtifact{SubnetEpoch: F, PreviousArtifactHash: bridge.LastArtifactHash}
	if !owner.allowsArtifactEdge(bridge.LastArtifactHash, previous, current) {
		t.Fatal("bridged artifact edge was refused")
	}
	early := *current
	early.SubnetEpoch = F - 1
	unlinked := *current
	unlinked.PreviousArtifactHash = nativeGapBridgeTestArtifactHash(2)
	for name, test := range map[string]struct {
		hash              string
		previous, current *ReleaseMeasurementArtifact
	}{
		"different hash": {nativeGapBridgeTestArtifactHash(2), previous, current},
		"different L":    {bridge.LastArtifactHash, &ReleaseMeasurementArtifact{SubnetEpoch: L - 1}, current},
		"before F":       {bridge.LastArtifactHash, previous, &early},
		"unlinked":       {bridge.LastArtifactHash, previous, &unlinked},
	} {
		if owner.allowsArtifactEdge(test.hash, test.previous, test.current) {
			t.Fatalf("bridge admitted an artifact edge with a %s", name)
		}
	}
	if (*releaseNativeGapBridgeV2)(nil).allowsArtifactEdge(bridge.LastArtifactHash, previous, current) {
		t.Fatal("absent bridge admitted an artifact edge")
	}
}

// Before F the release path returns the ordinary already-final result, which
// the production steering loop treats as complete instead of a hard failure.
func TestNativeGapBridgeIdlesBeforeFirstEpoch(t *testing.T) {
	t.Parallel()
	const L, F = nativeGapBridgeTestLast, nativeGapBridgeTestFirst
	bridge := nativeGapBridgeTest()
	current := nativeGapBridgeTestIntent(L, "applied")
	owner, err := bridge.matchHistory(t.Context(), []*SteeringIntent{current})
	if err != nil {
		t.Fatal(err)
	}
	for _, epoch := range []uint64{L, L + 1, 25661, F - 1} {
		if err := owner.requireFirstEpoch(current, epoch); err != ErrSteeringAlreadyFinal {
			t.Fatalf("native epoch %d did not idle before F: %v", epoch, err)
		}
	}
	for _, epoch := range []uint64{F, F + 3} {
		if err := owner.requireFirstEpoch(current, epoch); err != nil {
			t.Fatalf("native epoch %d stayed idle at or after F: %v", epoch, err)
		}
	}
	other := *current
	other.MeasurementArtifactHash = nativeGapBridgeTestArtifactHash(1)
	for name, err := range map[string]error{
		"crossed":           owner.requireFirstEpoch(nativeGapBridgeTestIntent(F, "applied"), F+1),
		"no intent":         owner.requireFirstEpoch(nil, F-1),
		"no bridge":         (*releaseNativeGapBridgeV2)(nil).requireFirstEpoch(current, F-1),
		"other predecessor": owner.requireFirstEpoch(&other, F-1),
	} {
		if err != nil {
			t.Fatalf("%s deferred release: %v", name, err)
		}
	}
	limits := headEMAStoreV2TestLimits()
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	if _, _, err := store.FoldForEpochV2(t.Context(), L, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha); err != nil {
		t.Fatal(err)
	}
	// The same order as submitOnceV2: idle check, then compact head admission.
	submitAt := func(bridged *releaseNativeGapBridgeV2, epoch uint64) func() error {
		return func() error {
			if err := bridged.requireFirstEpoch(current, epoch); err != nil {
				return err
			}
			store.v2.nativeGapBridge = bridged
			return store.admitReleaseHeadV2Known(t.Context(), epoch, alpha, limits.MaxEntries, releaseHeadV2Budget{limit: limits.MaxControlBytes})
		}
	}
	loop := func(submit func() error) (int, error) {
		polls := 0
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			polls++
			return submit()
		}, func() bool { return polls < releaseSteeringFailureLimit+3 }, nil)
		return polls, err
	}
	polls, err := loop(submitAt(nil, 25661))
	if err == nil || !strings.Contains(err.Error(), "compact head EMA epoch jumped") || polls != releaseSteeringFailureLimit {
		t.Fatalf("unbridged production failure changed: %d polls, %v", polls, err)
	}
	polls, err = loop(submitAt(owner, F-1))
	if err != nil || polls != releaseSteeringFailureLimit+3 {
		t.Fatalf("bridged idle counted hard failures: %d polls, %v", polls, err)
	}
	polls, err = loop(submitAt(owner, F))
	if err != nil || polls != releaseSteeringFailureLimit+3 {
		t.Fatalf("bridged first epoch did not reach head admission: %d polls, %v", polls, err)
	}
}

// The bridged edge folds the next actual input once, exactly like the existing
// forward fold; missed epochs add no observation or decay. Restart keeps it
// one-shot, and a crash before the EMA commit still completes once.
func TestNativeGapBridgeHeadEMAFoldsNextInputOnce(t *testing.T) {
	t.Parallel()
	const L, F = nativeGapBridgeTestLast, nativeGapBridgeTestFirst
	const E = F + 2
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	kept := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	dropped := FleetScoreKey{FleetID: [32]byte{3}, Hotkey: [32]byte{4}, Generation: 1, UID: 8}
	prior := map[FleetScoreKey]*big.Rat{kept: big.NewRat(4, 1), dropped: big.NewRat(6, 1)}
	raw := map[FleetScoreKey]*big.Rat{kept: big.NewRat(2, 1)}
	limits := headEMAStoreV2TestLimits()
	budget := releaseHeadV2Budget{limit: limits.MaxControlBytes}
	newPrior := func(stateDir string) *HeadEMAStore {
		t.Helper()
		store, err := NewHeadEMAStoreV2(t.Context(), stateDir, limits)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.FoldForEpochV2(t.Context(), L, prior, alpha); err != nil {
			t.Fatal(err)
		}
		return store
	}
	reference := newPrior(newReleaseHeadV2TestStateDir(t))
	want, wantRecords, err := reference.FoldForEpochV2(t.Context(), E, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 1 || want[7].Cmp(big.NewRat(3, 1)) != 0 || len(wantRecords) != 2 || wantRecords[1].HasRaw || wantRecords[1].Next != (RationalJSON{Numerator: "3", Denominator: "1"}) {
		t.Fatalf("reference gap fold is not one decay of the retained state: %v %v", want, wantRecords)
	}
	store := newPrior(newReleaseHeadV2TestStateDir(t))
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.admitReleaseHeadV2Known(t.Context(), E, alpha, limits.MaxEntries, budget); err == nil || !strings.Contains(err.Error(), "compact head EMA epoch jumped") {
		t.Fatalf("unbridged compact admission changed: %v", err)
	}
	if _, _, err := store.PreviewForEpochV2(t.Context(), E, raw, alpha); err == nil {
		t.Fatal("unbridged preview accepted the gap")
	}
	if err := store.CommitForEpochV2(t.Context(), E, wantRecords, alpha); err == nil {
		t.Fatal("unbridged commit accepted the gap")
	}
	owner := &releaseNativeGapBridgeV2{bridge: nativeGapBridgeTest()}
	store.v2.nativeGapBridge = owner
	for _, epoch := range []uint64{L + 2, F - 1} {
		if err := store.admitReleaseHeadV2Known(t.Context(), epoch, alpha, limits.MaxEntries, budget); err == nil {
			t.Fatalf("bridge admitted head epoch %d before F", epoch)
		}
		if _, _, err := store.PreviewForEpochV2(t.Context(), epoch, raw, alpha); err == nil {
			t.Fatalf("bridge previewed head epoch %d before F", epoch)
		}
	}
	if err := store.admitReleaseHeadV2Known(t.Context(), E, alpha, limits.MaxEntries, budget); err != nil {
		t.Fatalf("bridged compact admission failed: %v", err)
	}
	got, records, err := store.PreviewForEpochV2(t.Context(), E, raw, alpha)
	if err != nil || !reflect.DeepEqual(got, want) || !equalHeadEMAFolds(records, wantRecords) {
		t.Fatalf("bridged preview differs from one forward fold: %v", err)
	}
	compact, compactRecords, err := store.previewForEpochV2(t.Context(), E, raw, alpha, limits.MaxEntries, limits.MaxControlBytes)
	if err != nil || !reflect.DeepEqual(compact, want) || !equalHeadEMAFolds(compactRecords, wantRecords) {
		t.Fatalf("bridged compact preview differs: %v", err)
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("bridged preview changed durable EMA: %v", err)
	}
	bad := slices.Clone(wantRecords)
	bad[0].Next.Numerator = "19"
	if err := store.CommitForEpochV2(t.Context(), E, bad, alpha); err == nil {
		t.Fatal("bridge admitted a transcript that changes exact scores")
	}
	if err := store.CommitForEpochV2(t.Context(), E, wantRecords, alpha); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.values, reference.values) || *store.lastSubnetEpoch != E {
		t.Fatal("bridged commit differs from the forward fold")
	}
	check := func(store *HeadEMAStore) {
		t.Helper()
		if store.allowsHeadEMAEpochGapTo(E+2) || store.admitReleaseHeadV2Known(t.Context(), E+2, alpha, limits.MaxEntries, budget) == nil {
			t.Fatal("bridge admitted a second head gap")
		}
		if _, _, err := store.PreviewForEpochV2(t.Context(), E+2, raw, alpha); err == nil {
			t.Fatal("bridge previewed a second head gap")
		}
		again, retry, err := store.PreviewForEpochV2(t.Context(), E, raw, alpha)
		if err != nil || !reflect.DeepEqual(again, want) || !equalHeadEMAFolds(retry, wantRecords) {
			t.Fatalf("same-epoch retry folded twice: %v", err)
		}
		if _, _, err := store.PreviewForEpochV2(t.Context(), E+1, raw, alpha); err != nil {
			t.Fatalf("consecutive head epoch after the bridge failed: %v", err)
		}
	}
	check(store)
	restarted, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), limits)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.allowsHeadEMAEpochGapTo(E + 2) {
		t.Fatal("head EMA file data enabled a bridge")
	}
	restarted.v2.nativeGapBridge = owner
	check(restarted)
	// A crash between the intent write and the EMA commit restarts at L; the
	// retained intent's restore commit completes the bridged fold once.
	crashedDir := newReleaseHeadV2TestStateDir(t)
	newPrior(crashedDir)
	crashed, err := NewHeadEMAStoreV2(t.Context(), crashedDir, limits)
	if err != nil {
		t.Fatal(err)
	}
	crashed.v2.nativeGapBridge = owner
	if err := crashed.CommitForEpochV2(t.Context(), E, wantRecords, alpha); err != nil || !reflect.DeepEqual(crashed.values, reference.values) {
		t.Fatalf("restored bridged commit failed: %v", err)
	}
}

func nativeGapBridgeTestOwner(previousBytes []byte, last, first uint64) *releaseNativeGapBridgeV2 {
	bridge := nativeGapBridgeTest()
	bridge.LastNativeEpoch, bridge.LastArtifactHash, bridge.FirstNativeEpoch = last, ReleaseMeasurementContentHash(previousBytes), first
	return &releaseNativeGapBridgeV2{bridge: bridge}
}

// The likely production shape: the settlement epoch is unchanged across the
// missed window. Strict gap replay matches both real cuts to the retained
// ledgers and requires the bridged head fold to carry the prior state.
func TestNativeGapBridgeLineageReplaysSameSettlementGap(t *testing.T) {
	t.Parallel()
	previous := newReleaseMeasurementV2TestFixture(t, 2)
	previousBytes, err := canonicalReleaseMeasurementBytes(previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	L := previous.artifact.SubnetEpoch
	alpha := previous.artifact.Policy.Steering.HeadScoreEMA
	owner := nativeGapBridgeTestOwner(previousBytes, L, L+3)
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
	carried := false
	for _, record := range current.HeadEMA {
		carried = carried || record.HasPrior
	}
	if !carried {
		t.Fatal("bridged fold carried no prior head state")
	}
	history := &releaseEvidenceV2StartupHistory{nativeGapBridge: owner}
	for _, input := range previous.artifact.Inputs {
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: input.NoID, Ledger: previous.operators[input.NoID].seal.ledger})
	}
	runtime := &releaseRuntimeV2{history: history, gate: make(chan struct{}, 1)}
	store := &IntentStore{v2: &releaseIntentV2Owner{runtime: runtime, nativeGapBridge: owner}}
	verify := func(current *ReleaseMeasurementArtifact) error {
		options := previous.options(t)
		options.Expected = releaseMeasurementV2Decision(current)
		return store.verifyMeasurementLineageV2(t.Context(), previousBytes, previous.options(t), current, options)
	}
	if err := verify(current); err != nil {
		t.Fatalf("strict same-settlement bridged lineage failed: %v", err)
	}
	reset := cloneReleaseMeasurementArtifact(t, current)
	for index := range reset.HeadEMA {
		reset.HeadEMA[index].HasPrior, reset.HeadEMA[index].Prior = false, RationalJSON{Numerator: "0", Denominator: "1"}
	}
	rewritten := cloneReleaseMeasurementArtifact(t, current)
	cut := *rewritten.Inputs[0].AttemptCutV2
	if strings.HasSuffix(cut.Root, "0") {
		cut.Root = cut.Root[:len(cut.Root)-1] + "1"
	} else {
		cut.Root = cut.Root[:len(cut.Root)-1] + "0"
	}
	rewritten.Inputs[0].AttemptCutV2 = &cut
	regressed := cloneReleaseMeasurementArtifact(t, current)
	regressed.NativeSnapshotBlock = previous.artifact.NativeSnapshotBlock - 1
	for name, changed := range map[string]*ReleaseMeasurementArtifact{"head EMA reset": reset, "rewritten cut": rewritten, "native boundary regression": regressed} {
		if err := verify(changed); err == nil {
			t.Fatalf("bridge admitted a lineage with a %s", name)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func() func()
	}{
		{"no bridge", func() func() {
			store.v2.nativeGapBridge = nil
			return func() { store.v2.nativeGapBridge = owner }
		}},
		{"foreign startup owner", func() func() {
			foreign := *owner
			history.nativeGapBridge = &foreign
			return func() { history.nativeGapBridge = owner }
		}},
		{"provisional startup", func() func() {
			history.retainedStartup = true
			return func() { history.retainedStartup = false }
		}},
		{"edge before F", func() func() {
			late := nativeGapBridgeTestOwner(previousBytes, L, current.SubnetEpoch+1)
			store.v2.nativeGapBridge, history.nativeGapBridge = late, late
			return func() { store.v2.nativeGapBridge, history.nativeGapBridge = owner, owner }
		}},
		{"different L", func() func() {
			other := nativeGapBridgeTestOwner(previousBytes, L-1, L+3)
			store.v2.nativeGapBridge, history.nativeGapBridge = other, other
			return func() { store.v2.nativeGapBridge, history.nativeGapBridge = owner, owner }
		}},
		{"different hash", func() func() {
			other := nativeGapBridgeTestOwner([]byte("another predecessor"), L, L+3)
			store.v2.nativeGapBridge, history.nativeGapBridge = other, other
			return func() { store.v2.nativeGapBridge, history.nativeGapBridge = owner, owner }
		}},
	} {
		undo := test.mutate()
		if err := verify(current); err == nil {
			t.Fatalf("%s admitted the bridged lineage", test.name)
		}
		undo()
	}
	if err := verify(current); err != nil {
		t.Fatalf("restored owner refused the bridged lineage: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	options := previous.options(t)
	options.Expected = releaseMeasurementV2Decision(current)
	if err := store.verifyMeasurementLineageV2(cancelled, previousBytes, previous.options(t), current, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("bridged lineage ignored cancellation: %v", err)
	}
}

// A bridged edge that also spans settlement windows replays every interior
// terminal with the ordinary strict decoder, as history adoption does.
func TestNativeGapBridgeLineageReplaysInteriorTerminals(t *testing.T) {
	t.Parallel()
	previous := newProvisionalMeasurementLineageV2Initial(t)
	previousBytes, err := canonicalReleaseMeasurementBytes(previous.artifact)
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{terminals: map[uint64]*AttemptSettlementClosureV2{}, terminalContexts: map[uint64]map[uint64]AttemptCutV2Context{}, keys: map[uint64]map[byte]ed25519.PublicKey{}}
	current := previous
	var last *releaseMeasurementV2SettlementTestFixture
	for step := 0; step < 4; step++ {
		var terminalInputs []*attemptCutV2StatsTestFixture
		contexts := map[uint64]AttemptCutV2Context{}
		for _, input := range current.artifact.Inputs {
			operator := current.operators[input.NoID]
			terminalInputs = append(terminalInputs, &attemptCutV2StatsTestFixture{seal: operator.seal, cut: *input.AttemptCutV2, measurement: input.Stats, metadata: operator.metadata, data: operator.data})
			contexts[input.NoID] = operator.seal.expected
		}
		last = newReleaseMeasurementV2SettlementTestFromTerminal(t, current, terminalInputs, step == 3, 1)
		history.terminals[current.artifact.SettlementEpoch] = last.current.artifact.SettlementClosureV2
		history.terminalContexts[current.artifact.SettlementEpoch] = contexts
		current = last.current
	}
	current.artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(previousBytes)
	if previous.artifact.SubnetEpoch != 1401 || current.artifact.SubnetEpoch != 1404 || current.artifact.SettlementEpoch != 306 {
		t.Fatal("interior terminal fixture changed its native/settlement scope")
	}
	first := previous.operators[9].seal
	cfg := ReleaseConfig{ChainID: 964, Policy: previous.artifact.Policy, PolicyHash: previous.artifact.PolicyHash, EvidenceV2: ReleaseEvidenceV2Config{Bounds: ReleaseEvidenceV2Bounds{
		Cut: first.bounds, Replay: first.replay, MaxParticipants: 2, MaxTransitionBytes: 256 * 1024, MaxClosureBytes: 1024 * 1024, MaxProviders: 16, MaxEgressHashes: 16, MaxFleetPrefixes: 64}}}
	for _, input := range previous.artifact.Inputs {
		operator := previous.operators[input.NoID]
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: input.NoID, Ledger: operator.seal.ledger})
		history.keys[input.NoID] = operator.seal.server.serverPublicKeys()
		cfg.EvidenceV2.Operators = append(cfg.EvidenceV2.Operators, ReleaseEvidenceV2OperatorConfig{NoID: input.NoID, ReplayScratchRoot: newReleaseHeadV2TestStateDir(t), SealScratchRoot: newReleaseHeadV2TestStateDir(t)})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	reader, err := NewHTTPAttemptStreamV2Reader(server.URL, first.bounds)
	if err != nil {
		t.Fatal(err)
	}
	history.readers = []*HTTPAttemptStreamV2Reader{reader, reader}
	owner := nativeGapBridgeTestOwner(previousBytes, 1401, 1403)
	history.nativeGapBridge = owner
	runtime := &releaseRuntimeV2{cfg: cfg, history: history, gate: make(chan struct{}, 1)}
	store := &IntentStore{v2: &releaseIntentV2Owner{runtime: runtime, nativeGapBridge: owner}}
	verify := func() error {
		return store.verifyMeasurementLineageV2(t.Context(), previousBytes, previous.options(t), current.artifact, last.options(t))
	}
	if err := verify(); err != nil {
		t.Fatalf("strict bridged terminal replay failed: %v", err)
	}
	history.terminals[303].Transitions[0].Signature[0] ^= 1
	if err := verify(); err == nil {
		t.Fatal("bridge accepted a changed interior terminal signature")
	}
	history.terminals[303].Transitions[0].Signature[0] ^= 1
	original := history.terminals[304]
	delete(history.terminals, 304)
	if err := verify(); err == nil {
		t.Fatal("bridge accepted a missing interior terminal")
	}
	history.terminals[304] = original
	late := nativeGapBridgeTestOwner(previousBytes, 1401, 1405)
	store.v2.nativeGapBridge, history.nativeGapBridge = late, late
	if err := verify(); err == nil {
		t.Fatal("bridge admitted an edge into an epoch before F")
	}
	store.v2.nativeGapBridge, history.nativeGapBridge = nil, nil
	if err := verify(); err == nil {
		t.Fatal("ordinary strict owner admitted the missed native epochs")
	}
	store.v2.nativeGapBridge, history.nativeGapBridge = owner, owner
	if err := verify(); err != nil {
		t.Fatalf("restored bridge refused the terminal replay: %v", err)
	}
}

// Restart after crossing: startup reference replay runs these exact edge
// checks over the persisted history, and the V2 owner repeats the successor
// check on every read. The edge stays acceptable while the bridge is supplied.
func TestNativeGapBridgeStartupReplayAcceptsCrossedHistory(t *testing.T) {
	t.Parallel()
	const L, F = nativeGapBridgeTestLast, nativeGapBridgeTestFirst
	bridge := nativeGapBridgeTest()
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	value := func(n int) RationalJSON { return RationalJSON{Numerator: fmt.Sprint(n), Denominator: "1"} }
	build := func(epochs []uint64) ([]*SteeringIntent, []*ReleaseMeasurementArtifact) {
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
		return intents, artifacts
	}
	replay := func(intents []*SteeringIntent, artifacts []*ReleaseMeasurementArtifact) error {
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
	intents, artifacts := build([]uint64{L - 1, L, F + 2, F + 3})
	if err := replay(intents, artifacts); err != nil {
		t.Fatalf("restart refused the crossed bridged history: %v", err)
	}
	// The same persisted history without the bridge is refused, as before.
	if err := validateSteeringIntentSuccessorWithGapsV2(intents[1], intents[2], false); err == nil {
		t.Fatal("unbridged startup admitted the missed native epochs")
	}
	gapped, gappedArtifacts := build([]uint64{L - 1, L, F + 2, F + 5})
	if err := replay(gapped, gappedArtifacts); err == nil {
		t.Fatal("restart admitted a second gap after the bridged edge")
	}
	early, earlyArtifacts := build([]uint64{L - 1, L, F - 1})
	if err := replay(early, earlyArtifacts); err == nil {
		t.Fatal("restart admitted a bridged edge before F")
	}
	// The gap never waives the head EMA prior state or boundary order.
	reset := *artifacts[2]
	reset.HeadEMA = []HeadEMAMeasurement{{Key: key, HasRaw: true, Raw: value(2), Prior: value(0), Next: value(1)}}
	regressed := *artifacts[2]
	regressed.EVMSnapshotBlock = artifacts[1].EVMSnapshotBlock - 1
	for name, artifact := range map[string]*ReleaseMeasurementArtifact{"head EMA reset": &reset, "EVM boundary regression": &regressed} {
		if err := verifyStartupArtifactSuccessorV2(intents[1], artifacts[1], artifact, true); err == nil {
			t.Fatalf("startup admitted a bridged artifact with a %s", name)
		}
	}
	// Across the bridged edge a skipped settlement window is left to the
	// runtime's interior terminal replay; without the edge it is refused here.
	skipped := *artifacts[2]
	skipped.SettlementEpoch = 3
	if err := verifyStartupArtifactSuccessorV2(intents[1], artifacts[1], &skipped, true); err != nil {
		t.Fatalf("bridged startup refused a settlement window skip: %v", err)
	}
	if err := verifyStartupArtifactSuccessorV2(intents[1], artifacts[1], &skipped, false); err == nil {
		t.Fatal("ordinary startup admitted a settlement window skip")
	}
}
