// Actual signed root envelopes traverse the public bounded controller. Private
// host transports expose effect barriers without installing a real service.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The original root fixture provides every independently signed retained owner.
func repairControllerRootCommand(t *testing.T, f *repairRootPassiveFixture, entries ...repairControllerEntry) ([]string, repairControllerManifest) {
	t.Helper()
	if len(entries) == 0 {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		entries = []repairControllerEntry{{Id: "synthetic-root-incident", Kind: "root-passive", Approval: planFileReference{Path: f.path, Sha256: monitorReadDigest(raw)}, PublicKey: f.key}}
	}
	manifest := repairControllerManifest{Schema: repairControllerSchema, MaximumParallel: 4, Entries: entries}
	directory := filepath.Dir(f.path)
	path := filepath.Join(directory, "root-controller-manifest.json")
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, path, raw, 0600)
	checkpoint := filepath.Join(directory, "root-controller.json")
	prepareMainnetSnapshotTest(t, checkpoint, "mainnet-host-action", 64*1024)
	return []string{"--manifest", path, "--manifest-sha256", monitorReadDigest(raw), "--checkpoint", checkpoint, "--metrics-file", filepath.Join(directory, "root-controller.prom")}, manifest
}

// One complete invocation joins the actual role adapter before reading its census.
func repairControllerRootRun(t *testing.T, f *repairRootPassiveFixture, args []string, manifest repairControllerManifest) repairControllerRecord {
	t.Helper()
	var out, diagnostic bytes.Buffer
	if code := runRepairControllerCommandWithHost(f.ctx(), args, &out, &diagnostic, func() time.Time { return f.root.now }, f.root.host.files.host); code != 0 {
		t.Fatal("root controller command refused", code, diagnostic.String())
	}
	var record repairControllerRecord
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.validate(manifest, args[3]) != nil {
		t.Fatal("root controller census differs", err, record)
	}
	return record
}

// A real start sees both durable intents and still waits for the original
// checkpoint's fresh continuation before any completed metric is published.
func TestRepairControllerRootPassiveCompletesOriginalCheckpoint(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	args, manifest := repairControllerRootCommand(t, f)
	before := f.root.chain.journals(t)
	original, err := os.ReadFile(f.envelope.approval.Plan.OriginalJournal.Path)
	if err != nil {
		t.Fatal(err)
	}
	execute := f.root.host.files.host.execute
	intents := false
	f.root.host.files.host.execute = func(ctx context.Context, path string, arguments []string) ([]byte, error) {
		if strings.Contains(strings.Join(arguments, " "), " start -- ") {
			controllerRaw, controllerErr := os.ReadFile(args[5])
			processRaw, processErr := os.ReadFile(f.envelope.approval.Plan.StatePath)
			var controller repairControllerRecord
			var process repairProcessRecord
			if err := errors.Join(controllerErr, processErr, json.Unmarshal(controllerRaw, &controller), json.Unmarshal(processRaw, &process)); err != nil {
				return nil, err
			}
			if len(controller.Entries) != 1 || !controller.Entries[0].Attempted || controller.Entries[0].Disposition != "claim-intent-durable" || process.StartAt.IsZero() || process.Status != "start-consumed" || process.Generation != nil {
				return nil, errors.New("root start preceded original durable intents")
			}
			intents = true
		}
		return execute(ctx, path, arguments)
	}
	record := repairControllerRootRun(t, f, args, manifest)
	state := record.Entries[0]
	if !intents || !state.Attempted || state.Status != "pending" || state.Cause != "pending" || state.Disposition != "waiting-progress" || f.root.starts != 2 {
		t.Fatal("root dispatch lost original pending custody", intents, state, f.root.starts)
	}
	f.checkpoint(f.root.now)
	record = repairControllerRootRun(t, f, args, manifest)
	if record.Entries[0].Status != "completed" || record.Entries[0].Disposition != "resumed-generation-observed" || f.root.starts != 2 {
		t.Fatal("root dispatch did not observe original continuing state", record, f.root.starts)
	}
	record = repairControllerRootRun(t, f, args, manifest)
	if record.Entries[0].Status != "completed" || f.root.starts != 2 {
		t.Fatal("completed controller replenished root allowance", record, f.root.starts)
	}
	after, err := os.ReadFile(f.envelope.approval.Plan.OriginalJournal.Path)
	if err != nil || !bytes.Equal(original, after) || !reflect.DeepEqual(before, f.root.chain.journals(t)) {
		t.Fatal("root controller changed original activation or bootstrap custody", err)
	}
	raw, err := os.ReadFile(args[7])
	if err != nil || !bytes.Contains(raw, []byte("sn_mainnet_repair_controller_completed 1\n")) || !bytes.Contains(raw, []byte("sn_mainnet_repair_controller_pending 0\n")) || bytes.Contains(raw, []byte(f.envelope.approval.Plan.IncidentId)) || bytes.Contains(raw, []byte(f.key)) {
		t.Fatal("root outcome metrics lost completion or exposed authority", err, string(raw))
	}
}

// Before the signed window, the root readiness path must not create a journal,
// generation marker or controller intent even though the envelope is valid.
func TestRepairControllerRootPendingWindowDoesNotClaim(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	args, manifest := repairControllerRootCommand(t, f)
	f.root.now = f.envelope.approval.Plan.ValidFrom.Add(-time.Second)
	record := repairControllerRootRun(t, f, args, manifest)
	state := record.Entries[0]
	if state.Attempted || state.Status != "pending" || state.Cause != "pending" || f.root.starts != 1 {
		t.Fatal("pending root approval consumed an allowance", state, f.root.starts)
	}
	for _, path := range []string{f.envelope.approval.Plan.StatePath, repairProcessClaimPath(f.envelope.profile())} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("pending root created original custody", path, err)
		}
	}
}

// A held root signature cannot cancel a separately valid original envelope.
func TestRepairControllerRootHeldEnvelopeKeepsIndependentOutcome(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	_, manifest := repairControllerRootCommand(t, f)
	valid := manifest.Entries[0]
	held := valid
	held.Id, held.PublicKey = "synthetic-held-root", f.root.key
	args, manifest := repairControllerRootCommand(t, f, held, valid)
	record := repairControllerRootRun(t, f, args, manifest)
	if record.Entries[0].Status != "held" || record.Entries[0].Cause != "integrity" || record.Entries[0].Attempted || record.Entries[1].Status != "pending" || !record.Entries[1].Attempted || record.Entries[1].Disposition != "waiting-progress" || f.root.starts != 2 {
		t.Fatal("held root signature blocked independent original dispatch", record, f.root.starts)
	}
	raw, err := os.ReadFile(args[7])
	if err != nil || !bytes.Contains(raw, []byte("sn_mainnet_repair_controller_held 1\n")) || !bytes.Contains(raw, []byte("sn_mainnet_repair_controller_pending 1\n")) {
		t.Fatal("root outcomes collapsed into one disposition", err, string(raw))
	}
}

// The manifest selects a verifier, not a new interpretation of signed bytes.
func TestRepairControllerRootCannotUseValidatorSignatureDomains(t *testing.T) {
	for _, kind := range []string{"validator", "active-validator"} {
		f := newRepairRootPassiveFixture(t)
		_, manifest := repairControllerRootCommand(t, f)
		entry := manifest.Entries[0]
		entry.Kind = kind
		args, manifest := repairControllerRootCommand(t, f, entry)
		record := repairControllerRootRun(t, f, args, manifest)
		if record.Entries[0].Status != "held" || record.Entries[0].Cause != "integrity" || record.Entries[0].Attempted || f.root.starts != 1 {
			t.Fatal("root signature acquired validator authority", kind, record, f.root.starts)
		}
	}
}

// An uncertain manager result remains consumed after command restart, even if
// a healthy checkpoint appears and the manager could accept another start.
func TestRepairControllerRootUncertainStartNeverRetries(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	args, manifest := repairControllerRootCommand(t, f)
	f.root.startError = context.DeadlineExceeded
	record := repairControllerRootRun(t, f, args, manifest)
	if record.Entries[0].Disposition != "uncertain-consumed-start" || !record.Entries[0].Attempted || f.root.starts != 2 {
		t.Fatal("controller lost uncertain root consumption", record, f.root.starts)
	}
	f.root.startError = nil
	f.checkpoint(f.root.now)
	record = repairControllerRootRun(t, f, args, manifest)
	if record.Entries[0].Status != "held" || record.Entries[0].Disposition != "uncertain-consumed-start" || f.root.starts != 2 {
		t.Fatal("controller adopted or retried uncertain root start", record, f.root.starts)
	}
}

// Controller intent cannot turn loss of a root journal into fresh authority.
func TestRepairControllerRootLostAttemptedJournalStaysHeld(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	args, manifest := repairControllerRootCommand(t, f)
	repairControllerRootRun(t, f, args, manifest)
	if err := os.Remove(f.envelope.approval.Plan.StatePath); err != nil {
		t.Fatal(err)
	}
	record := repairControllerRootRun(t, f, args, manifest)
	if record.Entries[0].Status != "held" || record.Entries[0].Cause != "integrity" || record.Entries[0].Disposition != "original-journal-unavailable" || f.root.starts != 2 {
		t.Fatal("lost root journal was re-created or softened", record, f.root.starts)
	}
	if _, err := os.Lstat(f.envelope.approval.Plan.StatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lost root journal was recreated", err)
	}
}

// Both the nested authority graph and its adjacent lifetime locks must be
// rejected before any controller checkpoint, metrics or root effect is opened.
func TestRepairControllerRootRejectsNestedOutputAliasesBeforeWrites(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	args, _ := repairControllerRootCommand(t, f)
	p := f.root.chain.preparation
	paths := []string{p.Root.ConfigPath, p.Root.ServiceInput.Path, p.Plan.Config.RootValidator.Approval.Path, p.Plan.Config.OwnerTrimPolicy.Path, p.Plan.Config.OwnerTrimPlan.Path, p.Plan.Config.Contracts.Path, p.Contracts.Config.Plan.Artifacts.Path, repairProcessClaimPath(f.envelope.profile())}
	paths = append(paths, p.childPaths()...)
	for _, role := range p.Plan.Config.Validators {
		paths = append(paths, role.Config.Path)
	}
	for _, inspection := range p.Plan.ValidatorInspections {
		paths = append(paths, inspection.ApprovalReference.Path)
		paths = append(paths, inspection.DeclaredPaths...)
	}
	before := mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)
	for _, path := range paths {
		for _, output := range []string{path, path + ".lock"} {
			changed := append([]string(nil), args...)
			changed[5] = output
			var out, diagnostic bytes.Buffer
			code := runRepairControllerCommandWithHost(f.ctx(), changed, &out, &diagnostic, func() time.Time { return f.root.now }, f.root.host.files.host)
			if code != 2 || !strings.Contains(diagnostic.String(), "write overlaps an original input") || out.Len() != 0 || f.root.starts != 1 {
				t.Fatal("nested output reached a controller owner", output, code, diagnostic.String(), f.root.starts)
			}
		}
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)) {
		t.Fatal("nested output refusal changed original physical custody")
	}
}

// A sibling .prom inside the sole root checkpoint directory would change its
// fixed file census even if it did not replace the checkpoint's exact pathname.
func TestRepairControllerRootRejectsCheckpointDirectoryOutputBeforeWrites(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	args, _ := repairControllerRootCommand(t, f)
	args[7] = filepath.Join(f.envelope.original.Plan.CheckpointDirectory, "controller.prom")
	before := mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)
	var out, diagnostic bytes.Buffer
	code := runRepairControllerCommandWithHost(f.ctx(), args, &out, &diagnostic, func() time.Time { return f.root.now }, f.root.host.files.host)
	if code != 2 || !strings.Contains(diagnostic.String(), "write overlaps an original input") || f.root.starts != 1 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)) {
		t.Fatal("controller output entered original checkpoint custody", code, diagnostic.String(), f.root.starts)
	}
}

// The public adapter also refuses an independently signed repair journal that
// aliases nested bootstrap custody; a fresh signature cannot grant that write.
func TestRepairControllerRootRejectsNestedRepairJournal(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.envelope.approval.Plan.StatePath = f.root.chain.preparation.childPaths()[0]
	f.sign()
	args, manifest := repairControllerRootCommand(t, f)
	before := mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)
	_, err := loadRepairControllerEnvelope(f.ctx(), manifest.Entries[0], f.root.host.files.host)
	var out, diagnostic bytes.Buffer
	code := runRepairControllerCommandWithHost(f.ctx(), args, &out, &diagnostic, func() time.Time { return f.root.now }, f.root.host.files.host)
	if !errors.Is(err, errRpcIntegrity) || code != 2 || !strings.Contains(diagnostic.String(), "write overlaps an original input") || out.Len() != 0 || f.root.starts != 1 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)) {
		t.Fatal("public signed nested repair journal acquired original custody", err, code, diagnostic.String(), f.root.starts)
	}
}

// A signed incident whose original closure cannot be observed suspends every
// shared write. Clearing the read fault resumes without a permanent held census.
func TestRepairControllerRootUnreadableClosureDefersAllSharedOutputs(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	args, manifest := repairControllerRootCommand(t, f)
	before := mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)
	faults := 0
	ctx := context.WithValue(f.ctx(), repairValidatorObservationKey{}, func(operation string) error {
		if operation == "host-read:"+f.envelope.approval.Plan.OriginalApproval.Path {
			faults++
			return syscall.EIO
		}
		return nil
	})
	var out, diagnostic bytes.Buffer
	code := runRepairControllerCommandWithHost(ctx, args, &out, &diagnostic, func() time.Time { return f.root.now }, f.root.host.files.host)
	if code != 1 || faults != 1 || out.Len() != 0 || f.root.starts != 1 || !strings.Contains(diagnostic.String(), "original passive root custody closure is unavailable") || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.root.host.files.host.trustRoot)) {
		t.Fatal("unreadable authenticated root closure wrote shared custody", code, faults, diagnostic.String(), f.root.starts)
	}
	record := repairControllerRootRun(t, f, args, manifest)
	if record.Entries[0].Status != "pending" || record.Entries[0].Disposition != "waiting-progress" || !record.Entries[0].Attempted || f.root.starts != 2 {
		t.Fatal("recoverable closure observation left a permanent hold", record, f.root.starts)
	}
}

// A real root readiness owner is held at the manager boundary. Another entry
// for that unit must return pending before taking either custody or intent.
func TestRepairControllerRootBusyHostReturnsPendingBeforeReadiness(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	_, manifest := repairControllerRootCommand(t, f)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseOwner := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseOwner()
	var once sync.Once
	execute := f.root.host.files.host.execute
	f.root.host.files.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), " show ") && args[len(args)-1] == rootPassiveHostUnitName {
			once.Do(func() {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
			})
		}
		return execute(ctx, path, args)
	}
	ctx, cancel := context.WithTimeout(f.ctx(), 30*time.Second)
	defer cancel()
	step := repairControllerHostStep(f.root.host.files.host, func() time.Time { return f.root.now })
	done := make(chan error, 1)
	joined := false
	go func() {
		_, _, err := step(ctx, manifest.Entries[0], false, func() error { return nil })
		done <- err
	}()
	defer func() {
		releaseOwner()
		cancel()
		if !joined {
			<-done
		}
	}()
	select {
	case <-entered:
	case err := <-done:
		joined = true
		t.Fatal("root never entered actual readiness", err)
	case <-ctx.Done():
		t.Fatal("root readiness barrier was not reached", ctx.Err())
	}
	reserved := false
	status, complete, err := step(ctx, manifest.Entries[0], false, func() error { reserved = true; return nil })
	releaseOwner()
	firstErr := <-done
	joined = true
	if status != "original-unit-busy" || complete || !errors.Is(err, errRepairControllerPending) || reserved || !errors.Is(firstErr, errRepairProcessPending) || f.root.starts != 2 {
		t.Fatal("busy root admitted another readiness or claim owner", status, complete, err, reserved, firstErr, f.root.starts)
	}
}

// Failure to publish the controller's intent prevents even permanent root
// generation admission, independently of the role's own start reservation.
func TestRepairControllerRootIntentFailurePreventsOriginalClaim(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	_, manifest := repairControllerRootCommand(t, f)
	status, complete, err := repairControllerHostStep(f.root.host.files.host, func() time.Time { return f.root.now })(f.ctx(), manifest.Entries[0], false, func() error { return errMainnetDurablePublicationUncertain })
	if status != "claim-intent-refused" || complete || !errors.Is(err, errMainnetDurablePublicationUncertain) || f.root.starts != 1 {
		t.Fatal("root dispatch passed an uncertain controller intent", status, complete, err, f.root.starts)
	}
	for _, path := range []string{f.envelope.approval.Plan.StatePath, repairProcessClaimPath(f.envelope.profile()), repairProcessClaimPath(f.envelope.profile()) + ".lock"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed controller intent created original root custody", path, err)
		}
	}
	status, cause := repairControllerCause(errors.Join(errRepairProcessPending, durablevolume.ErrIdentity))
	if status != "held" || cause != "integrity" {
		t.Fatal("root pending softened mixed integrity", status, cause)
	}
}
