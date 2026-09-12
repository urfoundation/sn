//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urnetwork/sdk"
)

type releaseArchiveV2TestFixture struct {
	startup *releaseStartupV2TestFixture
	options ReleaseEvidenceV2ArchiveOptions
	files   map[ReleaseEvidenceV2CaptureSource][]byte
	last    *AttemptSettlementClosureV2
}

// The public archive includes original proof/record objects as well as local
// journals. MaxHistoryBytes bounds the latter and each lifetime replay; it is
// not the allowance for their combined detached public source inventory.
const releaseArchiveV2TestMaximumBytes = uint64(32 * 1024 * 1024)

// The actual producer creates M8 signed trails, ordinary cuts and two complete
// consecutive terminals. Public replay reads detached bytes after production;
// it has no private key or access to the original ledger/snapshot paths.
func newReleaseArchiveV2TestFixture(t *testing.T) releaseArchiveV2TestFixture {
	return newReleaseArchiveV2TestFixtureWithTrails(t, 15)
}

func newReleaseArchiveV2TestFixtureWithTrails(t *testing.T, trails int) releaseArchiveV2TestFixture {
	t.Helper()
	startup := newReleaseStartupV2TestFixture(t, true)
	for index := range startup.disk.participants {
		for range trails {
			startup.trail(t, index)
		}
		startup.ordinary(t, index, 7, false)
	}
	startup.terminal(t, false)
	for index := range startup.disk.participants {
		startup.trail(t, index)
	}
	last := startup.terminal(t, false)
	cfg := startup.cfg
	cfg.Operators = slices.Clone(cfg.Operators)
	for index := range cfg.Operators {
		cfg.Operators[index].APIURL = startup.replicas[index].Origin
	}
	fixture := releaseArchiveV2TestFixture{startup: startup, files: map[ReleaseEvidenceV2CaptureSource][]byte{}, last: last}
	for _, operator := range cfg.EvidenceV2.Operators {
		fields := []struct {
			name string
			ref  ReleaseEvidenceV2File
		}{{"activation", operator.Activation}, {"vpk-signature", operator.VPKSignature}, {"hotkey-signature", operator.HotkeySignature}, {"context", operator.Context}, {"history", operator.History}}
		for _, field := range fields {
			raw, err := os.ReadFile(field.ref.Path)
			if err != nil {
				t.Fatal(err)
			}
			fixture.files[ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: fmt.Sprintf("activation/no-%d/%s", operator.NoID, field.name)}] = raw
		}
		value := sdk.VerifyKeysResult{}
		for version, key := range startup.keys[operator.NoID] {
			value.Keys = append(value.Keys, &sdk.VerifyServerKey{ServerKeyId: int32(version), PublicKey: key})
		}
		slices.SortFunc(value.Keys, func(a, b *sdk.VerifyServerKey) int { return int(a.ServerKeyId - b.ServerKeyId) })
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var origin string
		for _, configured := range cfg.Operators {
			if configured.NoID == operator.NoID {
				origin = configured.APIURL
			}
		}
		fixture.files[ReleaseEvidenceV2CaptureSource{Kind: "server-keys", Name: fmt.Sprintf("no-%d", operator.NoID), Origin: origin}] = raw
	}
	history, err := readReleaseEvidenceV2HistoryFiles(t.Context(), cfg.StateDir, cfg.EvidenceV2.Bounds, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range history.inputs {
		fixture.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "measurements/inputs/" + input.name}] = bytes.Clone(input.encoded)
	}
	for _, closure := range history.closures {
		fixture.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "settlement-closures-v2/" + closure.name}] = bytes.Clone(closure.encoded)
	}
	if err := errors.Join(history.check(), history.close()); err != nil {
		t.Fatal(err)
	}
	for index, store := range startup.stores {
		objects, _, _ := store.snapshot()
		for name, raw := range objects {
			parts := strings.SplitN(name, "/", 2)
			if len(parts) != 2 {
				t.Fatal("invalid actual replica namespace")
			}
			fixture.files[ReleaseEvidenceV2CaptureSource{Kind: parts[0], Name: parts[1], Origin: startup.replicas[index].Origin}] = raw
		}
	}
	fixture.options = ReleaseEvidenceV2ArchiveOptions{Config: &cfg, Hotkey: startup.inputs[0].Context.Activation.Hotkey, Origins: [2]string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}, ScratchRoot: newAttemptSettlementRuntimeV2TestStateDir(t), MaximumBytes: releaseArchiveV2TestMaximumBytes, MaximumObjects: 4096}
	fixture.options.ReadSource = func(ctx context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, found := fixture.files[source]
		if !found {
			return nil, errors.New("test closed source is missing")
		}
		return bytes.Clone(raw), nil
	}
	fixture.repin()
	return fixture
}

func TestReleaseArchiveV2LifetimeRejectsValidlySignedTrailReuse(t *testing.T) {
	fixture := newReleaseArchiveV2TestFixtureWithTrails(t, 1)
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	participant := fixture.startup.disk.participants[0]
	var original AttemptRecord
	if err := participant.Ledger.Walk(t.Context(), 1, 1, func(record AttemptRecord) error { original = record; return nil }); err != nil {
		t.Fatal(err)
	}
	ledger := archive.owner.ledgers[participant.NoID]
	cursor := archive.history.current[participant.NoID]
	original.Sequence, original.PreviousHash = ledger.last+1, ledger.root
	original.Boundary = AttemptBoundary{SettlementEpoch: cursor.epoch, EVMBlock: cursor.lastBoundary.EVMBlock + 1, EVMBlockHash: attemptHex32([32]byte{0xf1})}
	reused := resignAttemptRecordStoreTest(t, original, fixture.startup.inputs[0].PrivateKey)
	if err := VerifyAttemptRecord(&reused, ledger.identity, ledger.vpk, fixture.startup.keys[participant.NoID]); err != nil {
		t.Fatalf("reused record did not reach lifetime checks with real valid signatures: %v", err)
	}
	expected := fixture.startup.inputs[0].Context.InitialCut
	expected.Boundary, expected.FirstSequence, expected.EgressFirstSequence, expected.EgressGeneration, expected.PriorRoot = reused.Boundary, cursor.first, cursor.egressFirst, cursor.generation, cursor.priorRoot
	visitor, err := archive.owner.beginCut(t.Context(), participant.NoID, AttemptCutV2{Context: expected, LastSequence: reused.Sequence, Root: reused.RecordHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := visitor.visit(reused); err == nil {
		t.Fatal("validly signed new-epoch record reused an already terminal trail")
	}
	if ledger.last != reused.Sequence-1 {
		t.Fatal("failed lifetime append advanced the authenticated prefix")
	}
}

func (self *releaseArchiveV2TestFixture) repin() {
	self.options.Sources = nil
	for source, raw := range self.files {
		self.options.Sources = append(self.options.Sources, ReleaseEvidenceV2ArchiveSource{Source: source, SizeBytes: uint64(len(raw)), ContentHash: ReleaseMeasurementContentHash(raw)})
	}
	slices.SortFunc(self.options.Sources, func(a, b ReleaseEvidenceV2ArchiveSource) int {
		return strings.Compare(a.Source.Kind+"/"+a.Source.Origin+"/"+a.Source.Name, b.Source.Kind+"/"+b.Source.Origin+"/"+b.Source.Name)
	})
}

func TestReleaseArchiveV2ReplaysCompleteSignedHistoryWithoutLiveState(t *testing.T) {
	fixture := newReleaseArchiveV2TestFixture(t)
	before := releaseArchiveV2TestPrivateFiles(t, fixture.startup.cfg.StateDir)
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if archive.history.retainedStartup || len(archive.history.terminals) != 2 || len(archive.history.inputContextsByEpoch[7]) != 2 {
		t.Fatal("archive skipped complete ordinary/terminal replay")
	}
	for _, transition := range fixture.last.Transitions {
		cursor := archive.history.current[transition.Identity.NoID]
		if cursor.epoch != transition.ToEpoch || cursor.lastRoot != transition.Cut.Root || cursor.lastSequence != transition.Cut.LastSequence || !slices.Equal(cursor.prior, transition.PostFold) {
			t.Fatal("archive cursor or exact post-fold EMA differs")
		}
	}
	if _, _, err := archive.Measurement("sha256:" + strings.Repeat("1", 64)); err == nil {
		t.Fatal("history-only replay invented a verified decision")
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	after := releaseArchiveV2TestPrivateFiles(t, fixture.startup.cfg.StateDir)
	if !releaseArchiveV2TestSameFiles(before, after) {
		t.Fatal("public archive replay changed the original live state")
	}
}

func releaseArchiveV2TestPrivateFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("unexpected fixture alias")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = ReleaseMeasurementContentHash(raw)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func releaseArchiveV2TestSameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, hash := range a {
		if b[name] != hash {
			return false
		}
	}
	return true
}

func TestReleaseArchiveV2RejectsMissingReplicaAndOriginalPinMutation(t *testing.T) {
	for _, kind := range []string{"second-origin-proof", "initial-context", "interior-terminal"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newReleaseArchiveV2TestFixture(t)
			switch kind {
			case "second-origin-proof":
				for source := range fixture.files {
					if source.Kind == AttemptStreamV2Proofs && source.Origin == fixture.options.Origins[1] {
						delete(fixture.files, source)
						break
					}
				}
			case "initial-context":
				for source, raw := range fixture.files {
					if source.Kind == "setup" && strings.HasSuffix(source.Name, "/context") {
						changed := bytes.Clone(raw)
						changed[len(changed)-1] = ' '
						fixture.files[source] = changed
						break
					}
				}
			case "interior-terminal":
				delete(fixture.files, ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "settlement-closures-v2/7.json"})
			}
			fixture.repin()
			archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
			if archive != nil {
				_ = archive.Close()
			}
			if err == nil || archive != nil {
				t.Fatalf("accepted changed complete source %s: %v", kind, err)
			}
			if strings.Contains(err.Error(), "source bytes exceed their independent allowance") {
				t.Fatalf("source mutation did not reach semantic replay: %v", err)
			}
		})
	}
}

func TestReleaseArchiveV2RejectsSelfDeclaredEMAAndChangedSourceBytes(t *testing.T) {
	for _, kind := range []string{"post-fold-ema", "after-pin"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newReleaseArchiveV2TestFixture(t)
			if kind == "post-fold-ema" {
				source := ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "settlement-closures-v2/8.json"}
				var closure AttemptSettlementClosureV2
				if err := json.Unmarshal(fixture.files[source], &closure); err != nil {
					t.Fatal(err)
				}
				if len(closure.Transitions[0].PostFold) == 0 {
					t.Fatal("actual trail produced no EMA census")
				}
				closure.Transitions[0].PostFold[0].QualityPPM++
				raw, err := json.Marshal(&closure)
				if err != nil {
					t.Fatal(err)
				}
				fixture.files[source] = append(raw, '\n')
				fixture.repin()
			} else {
				for source, raw := range fixture.files {
					if source.Kind == AttemptStreamV2Records {
						changed := bytes.Clone(raw)
						changed[len(changed)/2] ^= 1
						fixture.files[source] = changed
						break
					}
				}
			}
			archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
			if archive != nil {
				_ = archive.Close()
			}
			if err == nil || archive != nil {
				t.Fatalf("accepted fabricated archive result %s: %v", kind, err)
			}
			if strings.Contains(err.Error(), "source bytes exceed their independent allowance") {
				t.Fatalf("source mutation did not reach semantic replay: %v", err)
			}
		})
	}
}

func TestReleaseArchiveV2RejectsArchiveByteBudgetBeforeReads(t *testing.T) {
	fixture := newReleaseArchiveV2TestFixtureWithTrails(t, 1)
	fixture.options.MaximumBytes = 1
	read := false
	fixture.options.ReadSource = func(context.Context, ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		read = true
		return nil, errors.New("unexpected source read")
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
	if archive != nil {
		_ = archive.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source bytes exceed their independent allowance") || read || archive != nil {
		t.Fatalf("bounded archive census escaped admission: read=%t, err=%v", read, err)
	}
}

func TestReleaseArchiveV2CancellationCannotPublishHistory(t *testing.T) {
	fixture := newReleaseArchiveV2TestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	read := fixture.options.ReadSource
	fixture.options.ReadSource = func(ctx context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		raw, err := read(ctx, source)
		if source.Kind == AttemptStreamV2Proofs {
			cancel()
		}
		return raw, err
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(ctx, fixture.options)
	if archive != nil {
		_ = archive.Close()
	}
	if !errors.Is(err, context.Canceled) || archive != nil {
		t.Fatalf("archive escaped canceled complete replay: %v", err)
	}
}
