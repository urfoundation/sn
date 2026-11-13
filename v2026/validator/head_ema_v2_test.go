//go:build linux || darwin

package validator

// These are genuine persisted EMA arithmetic and native-file controls. They
// do not stand in for signed M8 collection, live activation or on-chain tests.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
)

// The old public preview/commit path creates the actual restart input.
type headEMAStoreV2TestFixture struct {
	stateDir string
	path     string
	encoded  []byte
	store    *HeadEMAStore
	keys     []FleetScoreKey
	alpha    protocol.Rational
	raw      map[FleetScoreKey]*big.Rat
	records  []HeadEMAMeasurement
}

// Only fixture provisioning resolves a platform alias. Candidate admission
// itself must never normalize an authority path, including Darwin /var aliases.
func newHeadEMAStoreV2TestFixture(t *testing.T) *headEMAStoreV2TestFixture {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(parent, "state")
	store, err := NewHeadEMAStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	keys := []FleetScoreKey{
		{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7},
		{FleetID: [32]byte{3}, Hotkey: [32]byte{4}, Generation: 2, UID: 9},
	}
	raw := map[FleetScoreKey]*big.Rat{keys[0]: big.NewRat(4, 1), keys[1]: big.NewRat(6, 1)}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	_, records, err := store.PreviewForEpoch(10, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitForEpoch(10, records, alpha); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "head-ema.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return &headEMAStoreV2TestFixture{stateDir: stateDir, path: path, encoded: encoded, store: store, keys: keys, alpha: alpha, raw: raw, records: records}
}

// Bounds are test-owner inputs, independent of anything in the file.
func headEMAStoreV2TestLimits() HeadEMAStoreV2Limits {
	return HeadEMAStoreV2Limits{MaxFileBytes: 1024 * 1024, MaxEntries: 64, MaxControlBytes: 1024 * 1024}
}

// Same-epoch restart is exact and the following epoch decays once, without a
// loader write or a second fold of the already committed epoch.
func TestHeadEMAStoreV2ReloadsRealDurableFoldWithoutWriting(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	before, err := os.Stat(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits())
	if err != nil {
		t.Fatal(err)
	}
	output, records, err := store.PreviewForEpoch(10, fixture.raw, fixture.alpha)
	if err != nil || !equalHeadEMAFolds(records, fixture.records) || len(output) != 2 || output[7].Cmp(big.NewRat(4, 1)) != 0 || output[9].Cmp(big.NewRat(6, 1)) != 0 {
		t.Fatalf("same-epoch loaded preview = %v, %v: %v", output, records, err)
	}
	next, _, err := store.PreviewForEpoch(11, map[FleetScoreKey]*big.Rat{fixture.keys[0]: big.NewRat(0, 1)}, fixture.alpha)
	if err != nil || len(next) != 1 || next[7].Cmp(big.NewRat(2, 1)) != 0 {
		t.Fatalf("successor preview = %v: %v", next, err)
	}
	after, err := os.Stat(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(encoded, fixture.encoded) || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatalf("read-only restart changed persisted input: %v", err)
	}
}

// Historical schema-one entries retain their original canonical values. This
// is a schema fixture derived from real entries, not a claimed live migration.
func TestHeadEMAStoreV2PreservesLegacySchemaEntries(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	var file headEMAFile
	if err := json.Unmarshal(fixture.encoded, &file); err != nil {
		t.Fatal(err)
	}
	file.Schema, file.LastSubnetEpoch, file.LastAlpha, file.LastFold = headEMASchemaV1, nil, nil, nil
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := NewHeadEMAStore(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	bounded, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits())
	if err != nil || !reflect.DeepEqual(bounded.values, legacy.values) || bounded.lastSubnetEpoch != nil {
		t.Fatalf("legacy state differs: %v", err)
	}
}

// Missing state is not permission to mkdir, chmod, repair or write a journal.
func TestHeadEMAStoreV2MissingFileNeverProvisionsState(t *testing.T) {
	t.Parallel()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(parent, "provisioned")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewHeadEMAStoreV2(context.Background(), stateDir, headEMAStoreV2TestLimits())
	if err != nil || len(store.values) != 0 || store.lastSubnetEpoch != nil {
		t.Fatalf("empty store = %v: %v", store, err)
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("loader created state: %v, %v", entries, err)
	}
	missingDir := filepath.Join(parent, "not-provisioned", "nested")
	if store, err := NewHeadEMAStoreV2(context.Background(), missingDir, headEMAStoreV2TestLimits()); err == nil || store != nil {
		t.Fatalf("missing directory accepted: %v, %v", store, err)
	}
	if _, err := os.Lstat(filepath.Join(parent, "not-provisioned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing directory was provisioned: %v", err)
	}
}

// Invalid trusted bounds are refused before the first actual filesystem hook.
func TestHeadEMAStoreV2InvalidLimitsPrecedeAcquisition(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	for _, mutate := range []func(*HeadEMAStoreV2Limits){
		func(limits *HeadEMAStoreV2Limits) { limits.MaxFileBytes = 0 },
		func(limits *HeadEMAStoreV2Limits) { limits.MaxFileBytes = maxHeadEMAStoreV2Bytes + 1 },
		func(limits *HeadEMAStoreV2Limits) { limits.MaxEntries = 0 },
		func(limits *HeadEMAStoreV2Limits) { limits.MaxControlBytes = 0 },
		func(limits *HeadEMAStoreV2Limits) { limits.MaxControlBytes = 1 },
		func(limits *HeadEMAStoreV2Limits) { limits.MaxControlBytes = maxHeadEMAStoreV2Bytes + 1 },
	} {
		limits, visits := headEMAStoreV2TestLimits(), 0
		mutate(&limits)
		store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, limits, headEMAStoreV2LoadHooks{step: func(string, *os.File) error { visits++; return nil }})
		if err == nil || store != nil || visits != 0 {
			t.Fatalf("limits %+v reached IO: %v visits=%d: %v", limits, store, visits, err)
		}
	}
}

// The native size check accepts exact EOF and refuses one byte less without
// opening the leaf or entering the rational parser.
func TestHeadEMAStoreV2ExactFileByteBoundary(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits := headEMAStoreV2TestLimits()
	limits.MaxFileBytes = uint64(len(fixture.encoded))
	if _, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, limits); err != nil {
		t.Fatalf("exact byte boundary: %v", err)
	}
	limits.MaxFileBytes--
	opened, parsed := 0, 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, limits, headEMAStoreV2LoadHooks{
		step: func(operation string, _ *os.File) error {
			if operation == "leaf-opened" {
				opened++
			}
			return nil
		},
		beforeRational: func() error { parsed++; return nil },
	})
	if err == nil || store != nil || opened != 0 || parsed != 0 {
		t.Fatalf("short byte bound: store=%v opened=%d parsed=%d: %v", store, opened, parsed, err)
	}
}

// A real two-entry file cannot consume a one-entry arithmetic allowance.
func TestHeadEMAStoreV2EntryCensusPrecedesRationalParsing(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits, parsed := headEMAStoreV2TestLimits(), 0
	limits.MaxEntries = 1
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, limits, headEMAStoreV2LoadHooks{beforeRational: func() error { parsed++; return nil }})
	if err == nil || store != nil || parsed != 0 || !strings.Contains(err.Error(), "census") {
		t.Fatalf("entry census: %v parsed=%d: %v", store, parsed, err)
	}
}

// A real alpha-one zero fold has no retained entries but still has a complete
// last-fold transcript. Its independent count must be admitted before parsing.
func TestHeadEMAStoreV2LastFoldCensusPrecedesRationalParsing(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	raw := map[FleetScoreKey]*big.Rat{fixture.keys[0]: big.NewRat(0, 1), fixture.keys[1]: big.NewRat(0, 1)}
	if _, _, err := fixture.store.FoldForEpoch(11, raw, protocol.Rational{Numerator: 1, Denominator: 1}); err != nil {
		t.Fatal(err)
	}
	if len(fixture.store.values) != 0 || len(fixture.store.lastFold) != 2 {
		t.Fatal("fixture did not persist the intended empty-entry/full-transcript state")
	}
	limits, parsed := headEMAStoreV2TestLimits(), 0
	limits.MaxEntries = 1
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, limits, headEMAStoreV2LoadHooks{beforeRational: func() error { parsed++; return nil }})
	if err == nil || store != nil || parsed != 0 || !strings.Contains(err.Error(), "census") {
		t.Fatalf("last-fold census: %v parsed=%d: %v", store, parsed, err)
	}
	loaded, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits())
	if err != nil || len(loaded.values) != 0 || len(loaded.lastFold) != 2 {
		t.Fatalf("same genuine zero-fold file at sufficient bounds: %v", err)
	}
}

// Large canonical values are produced and persisted by the real arithmetic
// path. The loader's smaller independent control cap refuses before SetString.
func TestHeadEMAStoreV2DecimalBudgetPrecedesRationalParsing(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	numerator, ok := new(big.Int).SetString(strings.Repeat("9", 2048), 10)
	if !ok {
		t.Fatal("decimal fixture is invalid")
	}
	if _, _, err := fixture.store.FoldForEpoch(11, map[FleetScoreKey]*big.Rat{fixture.keys[0]: new(big.Rat).SetInt(numerator)}, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	limits, parsed := headEMAStoreV2TestLimits(), 0
	limits.MaxControlBytes = 8192
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, limits, headEMAStoreV2LoadHooks{beforeRational: func() error { parsed++; return nil }})
	if err == nil || store != nil || parsed != 0 || !strings.Contains(err.Error(), "control") {
		t.Fatalf("decimal bound: %v parsed=%d: %v", store, parsed, err)
	}
	if _, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits()); err != nil {
		t.Fatalf("same genuine file at sufficient bounds: %v", err)
	}
}

// Framing controls are malformed-wire tests, not claimed persisted valid
// state. None may enter arithmetic, even when duplicate values agree.
func TestHeadEMAStoreV2RejectsCompetingWireBeforeArithmetic(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	for _, prefix := range []string{
		"{\"schema\":\"" + headEMASchemaV2 + "\",",
		"{\"Schema\":\"" + headEMASchemaV2 + "\",",
		"{\"unexpected\":0,",
	} {
		encoded := append([]byte(prefix), fixture.encoded[1:]...)
		parsed := 0
		store, err := decodeHeadEMAStoreV2(context.Background(), fixture.path, encoded, headEMAStoreV2TestLimits(), func() error { parsed++; return nil })
		if err == nil || store != nil || parsed != 0 {
			t.Fatalf("competing field reached arithmetic: parsed=%d: %v", parsed, err)
		}
	}
	for _, encoded := range [][]byte{
		append(append([]byte(nil), fixture.encoded...), []byte(" {}")...),
		bytes.Replace(fixture.encoded, []byte("\"numerator\": \"4\""), []byte("\"numerator\": \"4\", \"numerator\": \"4\""), 1),
	} {
		if bytes.Equal(encoded, fixture.encoded) {
			t.Fatal("malformed-wire fixture did not change bytes")
		}
		parsed := 0
		store, err := decodeHeadEMAStoreV2(context.Background(), fixture.path, encoded, headEMAStoreV2TestLimits(), func() error { parsed++; return nil })
		if err == nil || store != nil || parsed != 0 {
			t.Fatalf("trailing/duplicate wire reached arithmetic: parsed=%d: %v", parsed, err)
		}
	}
}

// Go's ordinary array decoder can pad/truncate arrays; the bounded schema
// requires the complete fixed-width fleet and hotkey bytes before decoding.
func TestHeadEMAStoreV2RejectsWrongIdentityArrayWidth(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	for _, width := range []int{0, 31, 33} {
		var wire map[string]any
		if err := json.Unmarshal(fixture.encoded, &wire); err != nil {
			t.Fatal(err)
		}
		entry := wire["entries"].([]any)[0].(map[string]any)
		entry["key"].(map[string]any)["FleetID"] = make([]uint16, width)
		encoded, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		parsed := 0
		store, err := decodeHeadEMAStoreV2(context.Background(), fixture.path, encoded, headEMAStoreV2TestLimits(), func() error { parsed++; return nil })
		if err == nil || store != nil || parsed != 0 {
			t.Fatalf("width %d reached arithmetic: %d: %v", width, parsed, err)
		}
	}
}

// Complete mathematical and historical correspondence checks still run after
// bounded admission; a structurally small file is not automatically trusted.
func TestHeadEMAStoreV2RetainsCanonicalFoldAndEntryVerification(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	for _, mutate := range []func(*headEMAFile){
		func(file *headEMAFile) { file.Entries[0].Numerator = "04" },
		func(file *headEMAFile) { file.Entries[0].Denominator = "0" },
		func(file *headEMAFile) { file.Entries[0].Numerator = "8"; file.Entries[0].Denominator = "2" },
		func(file *headEMAFile) { file.Entries[0], file.Entries[1] = file.Entries[1], file.Entries[0] },
		func(file *headEMAFile) { file.LastFold[0].Next.Numerator = "5" },
		func(file *headEMAFile) { file.Entries[0].Numerator = "5" },
		func(file *headEMAFile) { file.LastAlpha = nil },
		func(file *headEMAFile) { file.LastAlpha.Denominator = 0 },
		func(file *headEMAFile) { file.LastFold = append(file.LastFold, file.LastFold[0]) },
	} {
		var file headEMAFile
		if err := json.Unmarshal(fixture.encoded, &file); err != nil {
			t.Fatal(err)
		}
		mutate(&file)
		encoded, err := json.Marshal(file)
		if err != nil {
			t.Fatal(err)
		}
		if store, err := decodeHeadEMAStoreV2(context.Background(), fixture.path, encoded, headEMAStoreV2TestLimits(), nil); err == nil || store != nil {
			t.Fatalf("invalid canonical/fold state accepted: %+v, %v", file, err)
		}
	}
}

// Direct parent aliases and same-inode leaf aliases are refused rather than
// resolved into a different durable-state authority.
func TestHeadEMAStoreV2RejectsAliasedAuthorityPaths(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	alias := filepath.Join(filepath.Dir(fixture.stateDir), "state-alias")
	if err := os.Symlink(fixture.stateDir, alias); err != nil {
		t.Fatal(err)
	}
	if store, err := NewHeadEMAStoreV2(context.Background(), alias, headEMAStoreV2TestLimits()); err == nil || store != nil {
		t.Fatalf("parent alias accepted: %v", err)
	}
	retained := fixture.path + ".retained"
	if err := os.Rename(fixture.path, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(retained, fixture.path); err != nil {
		t.Fatal(err)
	}
	if store, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits()); err == nil || store != nil {
		t.Fatalf("same-inode leaf alias accepted: %v", err)
	}
}

// This performs a real peerless FIFO open and observes its real flags; it is
// not a timeout or a substituted reader verdict.
func TestHeadEMAStoreV2ReplacementFIFOOpensNonblocking(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	opened, parsed := 0, 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{
		step: func(operation string, file *os.File) error {
			if operation == "leaf-observed" {
				if err := os.Rename(fixture.path, fixture.path+".retained"); err != nil {
					return err
				}
				return unix.Mkfifo(fixture.path, 0o600)
			}
			if operation == "leaf-opened" {
				opened++
				flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
				if err != nil {
					return err
				}
				if flags&unix.O_NONBLOCK == 0 {
					t.Error("real FIFO descriptor omitted O_NONBLOCK")
				}
			}
			return nil
		},
		beforeRational: func() error { parsed++; return nil },
	})
	if err == nil || store != nil || opened != 1 || parsed != 0 {
		t.Fatalf("FIFO acquisition: store=%v opened=%d parsed=%d: %v", store, opened, parsed, err)
	}
}

// A same-inode/same-size write is forced with an explicit timestamp change,
// not a sleep or an assumption about scheduler/filesystem timestamp precision.
func TestHeadEMAStoreV2RejectsSameInodeWriteStateChanges(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	visits := 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{step: func(operation string, _ *os.File) error {
		if operation != "leaf-read" {
			return nil
		}
		visits++
		if err := os.WriteFile(fixture.path, fixture.encoded, 0o600); err != nil {
			return err
		}
		return os.Chtimes(fixture.path, time.Unix(1, 0), time.Unix(1, 0))
	}})
	if err == nil || store != nil || visits != 1 {
		t.Fatalf("same-inode write accepted: visits=%d: %v", visits, err)
	}
}

// The callback returns nil after the real Close and replaces only the name,
// keeping identical bytes. The final witness must still refuse that replacement.
func TestHeadEMAStoreV2RejectsLeafRetargetAfterRealClose(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	closed := 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{afterClose: func(file *os.File) error {
		if file.Name() != fixture.path {
			return nil
		}
		closed++
		if _, err := file.Stat(); err == nil {
			t.Error("observer ran before actual Close")
		}
		if err := os.Rename(fixture.path, fixture.path+".retained"); err != nil {
			return err
		}
		return os.WriteFile(fixture.path, fixture.encoded, 0o600)
	}})
	if err == nil || store != nil || closed != 1 {
		t.Fatalf("post-Close leaf retarget accepted: closed=%d: %v", closed, err)
	}
}

// The owner directory itself may be retargeted by the final visible callback;
// a fresh unhooked native witness must compare the original physical identity.
func TestHeadEMAStoreV2RejectsParentRetargetAfterRealClose(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	closed := 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{afterClose: func(file *os.File) error {
		if file.Name() != fixture.stateDir {
			return nil
		}
		closed++
		if err := os.Rename(fixture.stateDir, fixture.stateDir+"-retained"); err != nil {
			return err
		}
		if err := os.Mkdir(fixture.stateDir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(fixture.path, fixture.encoded, 0o600)
	}})
	if err == nil || store != nil || closed != 1 {
		t.Fatalf("post-Close parent retarget accepted: closed=%d: %v", closed, err)
	}
}

// Empty-state authority must survive the same final callback as occupied state.
func TestHeadEMAStoreV2MissingFileCannotAppearAfterClose(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	if err := os.Rename(fixture.path, fixture.path+".retained"); err != nil {
		t.Fatal(err)
	}
	closed := 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{afterClose: func(file *os.File) error {
		if file.Name() != fixture.stateDir {
			return nil
		}
		closed++
		return os.WriteFile(fixture.path, fixture.encoded, 0o600)
	}})
	if err == nil || store != nil || closed != 1 {
		t.Fatalf("changed initial absence accepted: closed=%d: %v", closed, err)
	}
}

// A physically preclosed descriptor contributes a real OS failure, not an
// injected stand-in for the file's actual Close operation.
func TestHeadEMAStoreV2RetainsActualClosedDescriptorFailure(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	closed := 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{step: func(operation string, file *os.File) error {
		if operation != "leaf-opened" {
			return nil
		}
		closed++
		return file.Close()
	}})
	if err == nil || store != nil || closed != 1 || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("real closed-descriptor cause lost: closed=%d: %v", closed, err)
	}
}

// The added Close cause is explicitly an error-contract seam after real Close;
// cancellation is a separate event and both causes must survive together.
func TestHeadEMAStoreV2JoinsLateCloseFailureAndCancellation(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause, closed := errors.New("injected after-real-Close cause"), 0
	store, err := newHeadEMAStoreV2(ctx, fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{afterClose: func(file *os.File) error {
		if file.Name() != fixture.path {
			return nil
		}
		closed++
		if _, err := file.Stat(); err == nil {
			t.Error("Close cause injected before real Close")
		}
		cancel()
		return cause
	}})
	if store != nil || closed != 1 || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) {
		t.Fatalf("late causes lost: closed=%d: %v", closed, err)
	}
}

// Cancellation is checked before acquisition and immediately before arithmetic,
// including a nil-returning observer that independently cancels its caller.
func TestHeadEMAStoreV2CancellationNeverPublishesLoadedState(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	visits := 0
	store, err := newHeadEMAStoreV2(ctx, fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{step: func(string, *os.File) error { visits++; return nil }})
	if store != nil || !errors.Is(err, context.Canceled) || visits != 0 {
		t.Fatalf("pre-canceled admission: visits=%d: %v", visits, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	store, err = newHeadEMAStoreV2(ctx, fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{beforeRational: func() error { visits++; cancel(); return nil }})
	if store != nil || !errors.Is(err, context.Canceled) || visits != 1 {
		t.Fatalf("arithmetic-boundary cancellation: visits=%d: %v", visits, err)
	}
	if store, err := NewHeadEMAStoreV2(nil, fixture.stateDir, headEMAStoreV2TestLimits()); err == nil || store != nil {
		t.Fatalf("nil context accepted: %v", err)
	}
}

// Stable owner-only read modes and hardlinks are distinct from replacement
// attacks and remain accepted; the loader never chmods an existing inode.
func TestHeadEMAStoreV2PreservesPrivateModesAndStableHardlinks(t *testing.T) {
	t.Parallel()
	for _, mode := range []os.FileMode{0o400, 0o600, 0o700} {
		fixture := newHeadEMAStoreV2TestFixture(t)
		if err := os.Chmod(fixture.path, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(fixture.path, fixture.path+".alias"); err != nil {
			t.Fatal(err)
		}
		store, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits())
		if err != nil || len(store.values) != 2 {
			t.Fatalf("stable mode %o: %v", mode, err)
		}
		state, err := os.Stat(fixture.path)
		if err != nil || state.Mode().Perm() != mode {
			t.Fatalf("loader changed mode %o: %v", mode, err)
		}
	}
}

// Refusal leaves an existing non-private directory exactly as the caller
// supplied it; a loader cannot silently turn chmod into authority admission.
func TestHeadEMAStoreV2RejectsNonPrivateDirectoryWithoutChmod(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	if err := os.Chmod(fixture.stateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	store, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits())
	if err == nil || store != nil {
		t.Fatalf("non-private directory accepted: %v", err)
	}
	state, err := os.Stat(fixture.stateDir)
	if err != nil || state.Mode().Perm() != 0o750 {
		t.Fatalf("refused directory was repaired: %v", err)
	}
}

// Initially observed nonregular or shared-readable leaves fail before any
// leaf acquisition. A peerless FIFO is exercised without waiting for a peer.
func TestHeadEMAStoreV2RejectsNonPrivateAndNonRegularLeaves(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"shared-mode", "directory", "fifo"} {
		fixture := newHeadEMAStoreV2TestFixture(t)
		if kind == "shared-mode" {
			if err := os.Chmod(fixture.path, 0o640); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Rename(fixture.path, fixture.path+".retained"); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if err := os.Mkdir(fixture.path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := unix.Mkfifo(fixture.path, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		opened := 0
		store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{step: func(operation string, _ *os.File) error {
			if operation == "leaf-opened" {
				opened++
			}
			return nil
		}})
		if err == nil || store != nil || opened != 0 {
			t.Fatalf("initial %s leaf accepted/opened: opened=%d: %v", kind, opened, err)
		}
	}
}

// A file observed before a rename cannot later authorize a fresh empty EMA,
// even though the real open fails with an ENOENT-compatible error.
func TestHeadEMAStoreV2LateDisappearanceDoesNotReturnEmptyState(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	observed := 0
	store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(), headEMAStoreV2LoadHooks{step: func(operation string, _ *os.File) error {
		if operation != "leaf-observed" {
			return nil
		}
		observed++
		return os.Rename(fixture.path, fixture.path+".retained")
	}})
	if store != nil || !errors.Is(err, os.ErrNotExist) || observed != 1 {
		t.Fatalf("late disappearance became empty state: observed=%d: %v", observed, err)
	}
}

// Mutating the supplied bytes at the real arithmetic boundary cannot change
// decoded values, metadata or transcripts retained by the returned owner.
func TestHeadEMAStoreV2OwnsDecodedDataBeforeArithmeticCallback(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	encoded := append([]byte(nil), fixture.encoded...)
	visits := 0
	store, err := decodeHeadEMAStoreV2(context.Background(), fixture.path, encoded, headEMAStoreV2TestLimits(), func() error {
		visits++
		for index := range encoded {
			encoded[index] = 0
		}
		return nil
	})
	if err != nil || visits != 1 || !reflect.DeepEqual(store.values, fixture.store.values) || !equalHeadEMAFolds(store.lastFold, fixture.records) || store.lastSubnetEpoch == nil || *store.lastSubnetEpoch != 10 || store.lastAlpha == nil || *store.lastAlpha != fixture.alpha {
		t.Fatalf("decoded state borrowed caller bytes: visits=%d: %v", visits, err)
	}
}
