//go:build linux || darwin

// Real protected files and actual signed two-origin tapes exercise the new
// read-only scope. Hooks schedule filesystem events, never a proof verdict.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// All fixture directories explicitly exclude group/other access under either
// qualification umask. This helper never changes an ancestor it did not own.
func productionBootstrapCommittedTestDirectory(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return physical
}

// The expected owner is mandatory on both namespace and leaf paths. Using
// the invoker's uid in either check defeats this independently chosen domain.
func TestProductionBootstrapCommittedUidScopeAndBounds(t *testing.T) {
	root, _ := newReleaseEvidenceV2HistoryReadTestFixture(t)
	uid := uint32(os.Geteuid())
	bounds := releaseEvidenceV2HistoryReadTestBounds()
	for _, fault := range []string{"", "uid", "objects", "links", "mode"} {
		expected, maximum := uid, uint64(8192)
		path := releaseMeasurementInputV2Path(root, 7, 9)
		switch fault {
		case "uid":
			expected ^= 1
		case "objects":
			maximum = 1
		case "links":
			if err := os.Link(path, filepath.Join(root, "alias")); err != nil {
				t.Fatal(err)
			}
		case "mode":
			if err := os.Chmod(path, 0640); err != nil {
				t.Fatal(err)
			}
		}
		owner, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), root, bounds, expected, maximum, true, releaseMeasurementInputV2ReadHooks{})
		if fault == "" {
			if err != nil || owner == nil || len(owner.inputs) != 2 {
				t.Fatal("positive census", err)
			}
			if err := owner.close(); err != nil {
				t.Fatal(err)
			}
		} else if err == nil || owner != nil {
			t.Fatal("source substitution admitted", fault, err)
		}
		if fault == "links" {
			if err := os.Remove(filepath.Join(root, "alias")); err != nil {
				t.Fatal(err)
			}
		}
		if fault == "mode" {
			if err := os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// An actual different-owner source requires root only to provision/chown the
// synthetic fixture. Sol executes this dedicated root separately with sudo;
// ordinary runs retain the full mismatch/alias/bounds tests above.
func TestProductionBootstrapCommittedForeignUidReadOnly(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root-owned fixture provisioning")
	}
	root, encoded := newReleaseEvidenceV2HistoryReadTestFixture(t)
	const uid = uint32(65534)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, int(uid), -1)
	}); err != nil {
		t.Fatal(err)
	}
	bounds := releaseEvidenceV2HistoryReadTestBounds()
	if old, err := readReleaseEvidenceV2HistoryFiles(t.Context(), root, bounds, releaseMeasurementInputV2ReadHooks{}); err == nil || old != nil {
		t.Fatal("ordinary producer owner policy changed")
	}
	owner, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), root, bounds, uid, 8192, true, releaseMeasurementInputV2ReadHooks{})
	if err != nil || owner == nil || len(owner.inputs) != 2 || !bytes.Equal(owner.inputs[0].encoded, encoded) {
		t.Fatal("original foreign service owner refused", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	leaf := releaseMeasurementInputV2Path(root, 7, 9)
	if err := os.Chown(leaf, 0, -1); err != nil {
		t.Fatal(err)
	}
	if replaced, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), root, bounds, uid, 8192, true, releaseMeasurementInputV2ReadHooks{}); err == nil || replaced != nil {
		t.Fatal("foreign service namespace accepted another leaf owner")
	}
	if err := os.Chown(leaf, int(uid), -1); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, filepath.Join(root, "measurements"), filepath.Join(root, "measurements", "inputs")} {
		directory, err := openAttemptPrivateDirectory(path)
		if err != nil {
			t.Fatal(err)
		}
		if directory.anchor.uid != uid || directory.anchor.mode&0077 != 0 {
			t.Fatal("read-only admission repaired foreign ownership or mode")
		}
		if err := directory.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A held census must refuse append, alias and actual-close replacement. The
// close hook mutates only after the real descriptor has already been closed.
func TestProductionBootstrapCommittedUidClosingCensus(t *testing.T) {
	for _, fault := range []string{"append", "symlink", "close"} {
		root, encoded := newReleaseEvidenceV2HistoryReadTestFixture(t)
		armed, changed := false, false
		cause := errors.New("synthetic late close")
		hooks := releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			if armed && !changed && fault == "close" {
				changed = true
				if _, err := file.Stat(); err == nil {
					t.Fatal("close observer ran before real close")
				}
				return errors.Join(cause, atomicStateWrite(releaseMeasurementInputV2Path(root, 7, 9), encoded, 0600))
			}
			return nil
		}}
		owner, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), root, releaseEvidenceV2HistoryReadTestBounds(), uint32(os.Geteuid()), 8192, true, hooks)
		if err != nil {
			t.Fatal("control", err)
		}
		armed = true
		switch fault {
		case "append":
			err = writeReleaseMeasurementInputV2(releaseMeasurementInputV2Path(root, 9, 9), encoded, 1024)
		case "symlink":
			path := releaseMeasurementInputV2Path(root, 7, 9)
			if err = os.Rename(path, filepath.Join(root, "original")); err == nil {
				err = os.Symlink(filepath.Join(root, "original"), path)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		err = owner.close()
		if err == nil || fault == "close" && (!changed || !errors.Is(err, cause)) {
			t.Fatal("late namespace/close change escaped", fault, err)
		}
	}
}

// Cancellation happens at an exact actual-file phase. No scheduling delay or
// timeout is used as proof that the canceled owner has joined its descriptors.
func TestProductionBootstrapCommittedUidCancellationJoins(t *testing.T) {
	root, _ := newReleaseEvidenceV2HistoryReadTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var reached, closed bool
	hooks := releaseMeasurementInputV2ReadHooks{step: func(stage string, file *os.File, _ int) error {
		if stage == "leaf-read" {
			reached = true
			cancel()
		}
		return nil
	}, afterClose: func(file *os.File) error {
		if reached {
			if _, err := file.Stat(); err == nil {
				t.Fatal("cancellation returned before real descriptor close")
			}
			closed = true
		}
		return nil
	}}
	owner, err := readReleaseEvidenceV2HistoryFilesForUid(ctx, root, releaseEvidenceV2HistoryReadTestBounds(), uint32(os.Geteuid()), 8192, true, hooks)
	if owner != nil || !errors.Is(err, context.Canceled) || !reached || !closed {
		t.Fatal("canceled source retained partial bytes or failed to join", err)
	}
}

// These cases prove the physical route, not just the leaf uid. Qualification
// places TMPDIR under a protected /mnt/data parent, as required by the task.
func TestProductionBootstrapCommittedProtectedStatePath(t *testing.T) {
	root := productionBootstrapCommittedTestDirectory(t)
	uid := uint32(os.Geteuid())
	if err := checkProductionBootstrapStatePath(t.Context(), root, uid); err != nil {
		t.Fatal("protected physical control", err)
	}
	if err := checkProductionBootstrapStatePath(t.Context(), root, uid^1); err == nil {
		t.Fatal("wrong root owner admitted")
	}
	if err := os.Chmod(root, 0750); err != nil {
		t.Fatal(err)
	}
	if err := checkProductionBootstrapStatePath(t.Context(), root, uid); err == nil {
		t.Fatal("nonprivate protocol root admitted")
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	if err := checkProductionBootstrapStatePath(t.Context(), alias, uid); err == nil {
		t.Fatal("aliased protocol state admitted")
	}
	if err := os.Chmod(root, 0770); err != nil {
		t.Fatal(err)
	}
	if err := checkProductionBootstrapStatePath(t.Context(), child, uid); err == nil {
		t.Fatal("writable physical ancestor admitted")
	}
}

// Local origins return the actual producer's detached, signed streams and
// actual server keys. No source reader or replay result is injected.
func productionBootstrapCommittedTestFixture(t *testing.T) (releaseArchiveV2TestFixture, ProductionBootstrapObservation, ProductionBootstrapPrefixObservation, *[2]atomic.Uint64) {
	t.Helper()
	f := newReleaseArchiveV2TestFixtureWithTrails(t, 1)
	reads := &[2]atomic.Uint64{}
	for i, original := range f.options.Origins {
		index, origin := i, original
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			reads[index].Add(1)
			var source ReleaseEvidenceV2CaptureSource
			switch r.URL.Path {
			case "/verify/keys":
				source = ReleaseEvidenceV2CaptureSource{Kind: "server-keys", Name: fmt.Sprintf("no-%d", f.options.Config.Operators[index].NoID), Origin: origin}
				w.Header().Set("Content-Type", "application/json")
			case "/sn/attempt-artifact":
				source = ReleaseEvidenceV2CaptureSource{Kind: r.URL.Query().Get("kind"), Name: r.URL.Query().Get("hash"), Origin: origin}
				if source.Kind == "metadata" {
					w.Header().Set("Content-Type", "application/json")
				} else {
					w.Header().Set("Content-Type", "application/x-ndjson")
				}
			default:
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			raw, exists := f.files[source]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(raw)
		}))
		t.Cleanup(server.Close)
		f.options.Config.Operators[i].APIURL, f.options.Origins[i] = server.URL, server.URL
	}
	observed := ProductionBootstrapObservation{ConfigHash: "sha256:" + strings.Repeat("a", 64), DeploymentId: f.options.Config.DeploymentID, ValidatorId: f.options.Config.ValidatorID,
		Native:   ProductionBootstrapNativePoint{Block: 100000, Hash: [32]byte{0xa1}, Epoch: 1000, Hotkey: f.options.Hotkey},
		EvmBlock: f.startup.finalized, EvmHash: f.startup.boundary.EVMBlockHash, SettlementEpoch: f.startup.boundary.SettlementEpoch}
	for _, input := range f.startup.inputs {
		digest, err := input.Candidate.Digest()
		if err != nil {
			t.Fatal(err)
		}
		observed.Operators = append(observed.Operators, ProductionBootstrapOperatorObservation{NoId: input.Config.NoID, ActivationHash: attemptHex32(digest), ClientId: "synthetic-client", ClientKey: attemptHex32(input.Candidate.VPK), ClientKeyGeneration: 1,
			ClientKeyRegistrationHash: attemptHex32([32]byte{0xb1}), ClientKeyResponseHash: attemptHex32([32]byte{0xb2}), ObservationNonce: attemptHex32([32]byte{0xb3})})
	}
	approved, err := replayProductionBootstrapPrefix(t.Context(), f.options.Config, f.startup.inputs, f.startup.keys, observed)
	if err != nil {
		t.Fatal(err)
	}
	approved.HistoricalSources = true
	approved.ContentHash = productionBootstrapPrefixHash(*approved)
	return f, observed, *approved, reads
}

// Current capture plus the real archive replay reaches both complete M8 tape
// origins, both ordinary cuts and two terminals, without changing live files.
func TestProductionBootstrapCommittedCapturesAndReplaysBothOrigins(t *testing.T) {
	f, observed, approved, reads := productionBootstrapCommittedTestFixture(t)
	before := releaseArchiveV2TestPrivateFiles(t, f.options.Config.StateDir)
	history, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), f.options.Config.StateDir, f.options.Config.EvidenceV2.Bounds, uint32(os.Geteuid()), 8192, true, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := history.close(); err != nil {
			t.Error(err)
		}
	}()
	options, err := captureProductionBootstrapCommitted(t.Context(), f.options.Config, observed, history, f.options.ScratchRoot)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	got, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, approved, nil, uint32(os.Geteuid()))
	if err != nil || got == nil || len(got.Prefixes) != 2 || got.HistoricalSources || got.ContentHash != "" || got.ApprovedPrefixHash != approved.ContentHash || reads[0].Load() < 3 || reads[1].Load() < 3 {
		t.Fatal("current committed replay scope differs", got, err)
	}
	for i, prefix := range got.Prefixes {
		cursor := archive.history.current[prefix.NoId]
		if prefix.LastSequence <= approved.Prefixes[i].LastSequence || prefix.Root != cursor.lastRoot || prefix.Generation != cursor.generation || prefix.PriorEmaHash != productionBootstrapPrefixHash(cursor.prior) {
			t.Fatal("header projection bypassed actual current replay", prefix)
		}
	}
	if len(archive.history.terminals) != 2 || len(archive.history.inputByEpoch) != 1 || len(archive.intents) != 0 || !reflect.DeepEqual(before, releaseArchiveV2TestPrivateFiles(t, f.options.Config.StateDir)) {
		t.Fatal("current replay changed live state or widened its scope")
	}
}

// Recomputed hashes do not authorize a replaced approved prefix or a shorter
// current history. A prior root must exist in the replayed lifetime anchors.
func TestProductionBootstrapCommittedRejectsCheckpointSubstitution(t *testing.T) {
	f, observed, approved, _ := productionBootstrapCommittedTestFixture(t)
	// Detached original source origins remain authoritative for this replay.
	options := f.options
	for i := range f.options.Config.Operators {
		options.Config.Operators[i].APIURL = f.startup.replicas[i].Origin
		options.Origins[i] = f.startup.replicas[i].Origin
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	current, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, approved, nil, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal("control", err)
	}
	current.HistoricalSources = true
	current.ContentHash = productionBootstrapPrefixHash(*current)
	if _, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, approved, current, uint32(os.Geteuid())); err != nil {
		t.Fatal("retained identical prefix", err)
	}
	for _, fault := range []string{"uid", "root", "epoch", "generation", "ema", "history", "native", "native-hash", "evm-hash", "approved"} {
		prior, original := *current, approved
		prior.Prefixes = slices.Clone(prior.Prefixes)
		original.Prefixes = slices.Clone(original.Prefixes)
		switch fault {
		case "uid":
			prior.ServiceUid ^= 1
		case "root":
			prior.Prefixes[0].Root = attemptHex32([32]byte{0xff})
		case "epoch":
			prior.Prefixes[0].Epoch++
		case "generation":
			prior.Prefixes[0].Generation++
		case "ema":
			prior.Prefixes[0].PriorEmaHash = "sha256:" + strings.Repeat("c", 64)
		case "history":
			prior.Prefixes[0].HistoryHash = "sha256:" + strings.Repeat("d", 64)
		case "native":
			prior.Native.Block++
		case "native-hash":
			prior.Native.Hash[0] ^= 1
		case "evm-hash":
			prior.EvmHash = attemptHex32([32]byte{0xaa})
		case "approved":
			original.Prefixes[0].Generation++
		}
		prior.ContentHash, original.ContentHash = "", ""
		prior.ContentHash, original.ContentHash = productionBootstrapPrefixHash(prior), productionBootstrapPrefixHash(original)
		if got, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, original, &prior, uint32(os.Geteuid())); got != nil || err == nil {
			t.Fatal("rehashed checkpoint substitution admitted", fault)
		}
	}
}

// A current ceiling failure is local and precedes both public origin reads.
// It cannot be softened into an empty current prefix or a health warning.
func TestProductionBootstrapCommittedRejectsFutureCutBeforeFetch(t *testing.T) {
	f, observed, _, reads := productionBootstrapCommittedTestFixture(t)
	history, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), f.options.Config.StateDir, f.options.Config.EvidenceV2.Bounds, uint32(os.Geteuid()), 8192, true, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := history.close(); err != nil {
			t.Error(err)
		}
	}()
	observed.Native.Epoch = 0
	if _, err := captureProductionBootstrapCommitted(t.Context(), f.options.Config, observed, history, f.options.ScratchRoot); err == nil || reads[0].Load() != 0 || reads[1].Load() != 0 {
		t.Fatal("future native cut reached origins", err)
	}
}

// Both origins must authenticate the exact proof and record bodies. Valid
// first-origin bytes cannot stand in for a corrupted second replica.
func TestProductionBootstrapCommittedRejectsReplicaSubstitution(t *testing.T) {
	for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		f, observed, _, reads := productionBootstrapCommittedTestFixture(t)
		changed := false
		for source, raw := range f.files {
			if source.Kind == kind && source.Origin == f.startup.replicas[1].Origin {
				value := bytes.Clone(raw)
				value[0] ^= 1
				f.files[source] = value
				changed = true
			}
		}
		if !changed {
			t.Fatal("real fixture lacks second-origin tape", kind)
		}
		history, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), f.options.Config.StateDir, f.options.Config.EvidenceV2.Bounds, uint32(os.Geteuid()), 8192, true, releaseMeasurementInputV2ReadHooks{})
		if err != nil {
			t.Fatal(err)
		}
		_, captureErr := captureProductionBootstrapCommitted(t.Context(), f.options.Config, observed, history, f.options.ScratchRoot)
		if err := history.close(); err != nil {
			t.Fatal(err)
		}
		if captureErr == nil || reads[0].Load() < 2 || reads[1].Load() < 2 {
			t.Fatal("second-origin substitution admitted", kind, captureErr)
		}
	}
}

// Public production admission must prove original scope before any state,
// signing input or configured endpoint can be accessed.
func TestProductionBootstrapCommittedPublicScopeBeforeState(t *testing.T) {
	f := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	scratch := productionBootstrapCommittedTestDirectory(t)
	for _, uid := range []uint32{0, 65534} {
		if got, err := ObserveProductionBootstrapCommittedPrefix(t.Context(), f.path, raw, ProductionBootstrapObservation{}, ProductionBootstrapPrefixObservation{}, nil, uid, scratch); err == nil || got != nil {
			t.Fatal("unscoped production observation admitted", uid)
		}
	}
	for _, path := range []string{f.cfg.StateDir, f.cfg.HotkeySeedFile, f.cfg.Operators[0].ClientKeySeedFile} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("scope refusal opened producer/key state", path, err)
		}
	}
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatal("scope refusal created replay scratch", err)
	}
}
