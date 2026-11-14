// Exact original passive activation drives deterministic process repair. The
// private manager transport cannot install or start any real host service.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Original activation and monitor state are fully retained before one incident.
type repairRootPassiveFixture struct {
	t        *testing.T
	root     *rootPassiveHostFixture
	envelope *repairRootPassiveEnvelope
	private  ed25519.PrivateKey
	key      string
	path     string
}

// All signatures and generations are synthetic; the real custody owners run.
func newRepairRootPassiveFixture(t *testing.T) *repairRootPassiveFixture {
	t.Helper()
	root := newRootPassiveHostFixture(t)
	root.host.files.host.rootGid = uint32(os.Getegid())
	root.require("claim", "claimed")
	root.require("install", "installed")
	root.require("admit", "admitted-read-only")
	initial := root.require("start", "acknowledged-running")
	f := &repairRootPassiveFixture{t: t, root: root, private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize)), path: filepath.Join(filepath.Dir(root.path), "root-repair-approval.json")}
	f.key = "0x" + hex.EncodeToString(f.private.Public().(ed25519.PublicKey))
	f.checkpoint(root.now)
	checkpointPath := root.chain.root.plan.PassiveService.CheckpointPath
	checkpointRaw, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint monitorCheckpointRecord
	if err := json.Unmarshal(checkpointRaw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	ref := func(path string) planFileReference {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
	}
	root.now = root.now.Add(time.Minute)
	p := repairRootPassivePlan{OriginalApproval: ref(root.path), OriginalPublicKey: root.key, OriginalJournal: ref(root.approval.Plan.StatePath), OriginalCheckpoint: ref(checkpointPath), Checkpoint: checkpoint, Previous: *initial.Generation, IncidentAt: root.now, StatePath: filepath.Join(filepath.Dir(root.path), "root-repair-state.json"), ValidFrom: root.now, ExpiresAt: root.now.Add(time.Hour), MaximumStarts: 1, MaximumObservations: 8, MaximumSampleAgeSeconds: 120}
	prepareMainnetSnapshotTest(t, p.StatePath, "mainnet-host-action", 64*1024)
	f.envelope = &repairRootPassiveEnvelope{approval: repairRootPassiveApproval{Schema: repairRootPassiveSchema, Plan: p}, original: root.approval, uid: uint32(os.Geteuid()), gid: uint32(os.Getegid())}
	f.sign()
	for key, value := range map[string]string{"ActiveState": "inactive", "SubState": "dead", "MainPID": "0"} {
		root.manager[key] = value
	}
	root.host.files.host.monotonic = func() (uint64, error) { return 200, nil }
	execute := root.host.files.host.execute
	root.host.files.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		raw, err := execute(ctx, path, args)
		if strings.Contains(strings.Join(args, " "), " start -- ") {
			for key, value := range map[string]string{"MainPID": "4322", "ExecMainPID": "4322", "ExecMainStartTimestampMonotonic": "201", "InvocationID": strings.Repeat("6", 32)} {
				root.manager[key] = value
			}
		}
		return raw, err
	}
	return f
}

// Checkpoint publication uses the same existing lifetime marker and durable head.
func (self *repairRootPassiveFixture) checkpoint(stamp time.Time) {
	self.t.Helper()
	policy := self.root.chain.root.plan.PassiveService.Policy
	path := self.root.chain.root.plan.PassiveService.CheckpointPath
	store, err := openMonitorCheckpoint(path, identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}, self.ctx())
	if err != nil {
		self.t.Fatal(err)
	}
	err = errors.Join(store.save(&monitorState{lastHash: "0x" + strings.Repeat("7", 64), lastNumber: 100, lastProgressAt: stamp, lastSuccessAt: stamp}), store.close())
	if err != nil {
		self.t.Fatal(err)
	}
}

// The fixture uses the original approved physical volume declaration.
func (self *repairRootPassiveFixture) ctx() context.Context {
	return self.root.chain.storageContext(self.t.Context())
}

// Changing an incident in a test requires a fresh explicit independent signature.
func (self *repairRootPassiveFixture) sign() {
	self.t.Helper()
	self.envelope.approval.Plan.IncidentId = self.envelope.approval.Plan.incidentId()
	raw, err := self.envelope.approval.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.envelope.approval.Signature = hex.EncodeToString(ed25519.Sign(self.private, raw))
	if err := self.envelope.validate(self.key); err != nil {
		self.t.Fatal(err)
	}
	bootstrapRootTestWrite(self.t, self.path, self.envelope.approval)
}

// A claim is a separate durable operation and performs no start itself.
func (self *repairRootPassiveFixture) claim() {
	self.t.Helper()
	if err := self.envelope.claim(self.ctx(), self.key, self.root.host.files.host, func() time.Time { return self.root.now }); err != nil {
		self.t.Fatal(err)
	}
	if self.root.starts != 1 {
		self.t.Fatal("claim started a process", self.root.starts)
	}
}

// Every step reopens the existing journal to exercise restart continuity.
func (self *repairRootPassiveFixture) resume() (string, bool, error) {
	return self.envelope.resume(self.ctx(), self.key, self.root.host.files.host, func() time.Time { return self.root.now })
}

// A new generation must publish fresh continuing state before repair completes.
func TestRepairRootPassiveContinuesExactOriginalCheckpoint(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	before := f.root.chain.journals(t)
	original, err := os.ReadFile(f.envelope.approval.Plan.OriginalJournal.Path)
	if err != nil {
		t.Fatal(err)
	}
	f.claim()
	status, complete, err := f.resume()
	if status != "waiting-progress" || complete || !errors.Is(err, errRepairProcessPending) || f.root.starts != 2 {
		t.Fatal("restart did not retain pending original progress", status, complete, err, f.root.starts)
	}
	f.checkpoint(f.root.now)
	status, complete, err = f.resume()
	if err != nil || !complete || status != "resumed-generation-observed" || f.root.starts != 2 {
		t.Fatal("continued original checkpoint did not complete one repair", status, complete, err, f.root.starts)
	}
	status, complete, err = f.resume()
	if err != nil || !complete || f.root.starts != 2 {
		t.Fatal("completed repair replayed its start", status, complete, err, f.root.starts)
	}
	after, err := os.ReadFile(f.envelope.approval.Plan.OriginalJournal.Path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("repair changed original consumed host allowance", err)
	}
	if !reflect.DeepEqual(before, f.root.chain.journals(t)) {
		t.Fatal("repair changed original bootstrap journals")
	}
}

// An acknowledged process is never inferred after an uncertain start command.
func TestRepairRootPassiveUncertainStartNeverRetries(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	f.root.startError = errors.Join(context.DeadlineExceeded, syscall.EIO)
	status, complete, err := f.resume()
	if status != "uncertain-consumed-start" || complete || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, syscall.EIO) || f.root.starts != 2 {
		t.Fatal("uncertain start lost consumption or original cause", status, complete, err, f.root.starts)
	}
	f.root.startError = nil
	f.checkpoint(f.root.now)
	status, complete, err = f.resume()
	if status != "uncertain-consumed-start" || complete || !errors.Is(err, errRepairProcessHeld) || f.root.starts != 2 {
		t.Fatal("uncertain generation was adopted or restarted", status, complete, err, f.root.starts)
	}
}

// Another signature and journal cannot renew custody for the same generation.
func TestRepairRootPassiveAlternateJournalCannotRenewGeneration(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	f.envelope.approval.Plan.StatePath = filepath.Join(filepath.Dir(f.path), "another-repair.json")
	prepareMainnetSnapshotTest(t, f.envelope.approval.Plan.StatePath, "mainnet-host-action", 64*1024)
	f.sign()
	err := f.envelope.claim(f.ctx(), f.key, f.root.host.files.host, func() time.Time { return f.root.now })
	if err == nil || f.root.starts != 1 {
		t.Fatal("alternate journal renewed original generation custody", err, f.root.starts)
	}
}

// The initial activation signature cannot cross the new incident domain.
func TestRepairRootPassiveRequiresDistinctRoleSignature(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.envelope.approval.Signature = f.root.approval.Signature
	if err := f.envelope.validate(f.root.key); err == nil {
		t.Fatal("initial activation signature authorized incident repair")
	}
	f.envelope.approval.Schema = repairValidatorSchema
	f.signInvalid(t)
	if err := f.envelope.validate(f.key); err == nil {
		t.Fatal("validator role alias authorized passive root repair")
	}
}

// Negative fixtures sign malformed authority without bypassing its validator.
func (self *repairRootPassiveFixture) signInvalid(t *testing.T) {
	t.Helper()
	raw, err := self.envelope.approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	self.envelope.approval.Signature = hex.EncodeToString(ed25519.Sign(self.private, raw))
}

// Losing continuing state must never create an empty replacement checkpoint.
func TestRepairRootPassiveMissingCheckpointCannotStart(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	path := f.envelope.approval.Plan.OriginalCheckpoint.Path
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, complete, err := f.resume()
	if err == nil || complete || f.root.starts != 1 {
		t.Fatal("missing checkpoint replenished process continuity", complete, err, f.root.starts)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("repair recreated lost checkpoint", err)
	}
}

// Original host activation must have acknowledged the signed prior generation.
func TestRepairRootPassiveRejectsUnattributedPreviousGeneration(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.envelope.approval.Plan.Previous.InvocationId = strings.Repeat("8", 32)
	f.root.manager["InvocationID"] = f.envelope.approval.Plan.Previous.InvocationId
	f.sign()
	err := f.envelope.ready(f.ctx(), f.root.host.files.host, f.root.now)
	if err == nil || f.root.starts != 1 {
		t.Fatal("manager-only generation replaced original acknowledgment", err)
	}
}

// Publication delay can consume a start while closing its permission window.
func TestRepairRootPassiveExpiryAfterReservationDoesNotStart(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	store, err := openRepairProcessStore(f.ctx(), f.envelope, f.key, false, f.root.now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	syncDirectory := store.syncDirectory
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(store.path)
		if err != nil {
			return err
		}
		var record repairProcessRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		if record.Status == "start-consumed" {
			f.root.now = f.envelope.approval.Plan.ExpiresAt
		}
		if syncDirectory != nil {
			return syncDirectory(directory)
		}
		return directory.Sync()
	}
	status, complete, err := resumeRepairProcess(f.ctx(), store, f.root.host.files.host, func() time.Time { return f.root.now })
	if status != "uncertain-consumed-start" || complete || !errors.Is(err, errRepairProcessHeld) || f.root.starts != 1 {
		t.Fatal("expired reserved authority started a process", status, complete, err, f.root.starts)
	}
	record, err := store.load(f.ctx())
	if err != nil || record.StartAt.IsZero() {
		t.Fatal("expiry refunded the reserved start", record, err)
	}
}

// A finite observation budget survives reopens and cannot retry a prior start.
func TestRepairRootPassiveObservationBudgetSurvivesReopen(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.envelope.approval.Plan.MaximumObservations = 1
	f.sign()
	f.claim()
	_, _, err := f.resume()
	if !errors.Is(err, errRepairProcessPending) {
		t.Fatal(err)
	}
	f.checkpoint(f.root.now)
	status, complete, err := f.resume()
	if status != "observation-limit" || complete || !errors.Is(err, errRepairProcessHeld) || f.root.starts != 2 {
		t.Fatal("reopen replenished bounded observations", status, complete, err, f.root.starts)
	}
}

// An actual changed unit and an unavailable observation have different meaning.
func TestRepairRootPassiveReadCauseDoesNotInventIntegrity(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	ctx := context.WithValue(f.ctx(), repairValidatorObservationKey{}, func(operation string) error {
		if operation == "host-read:"+f.envelope.approval.Plan.OriginalApproval.Path {
			return errors.Join(context.DeadlineExceeded, syscall.EIO)
		}
		return nil
	})
	_, complete, err := f.envelope.resume(ctx, f.key, f.root.host.files.host, func() time.Time { return f.root.now })
	if complete || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, syscall.EIO) || errors.Is(err, errRpcIntegrity) || f.root.starts != 1 {
		t.Fatal("unavailable original read changed authority meaning", complete, err, f.root.starts)
	}
	if _, _, err := f.resume(); !errors.Is(err, errRepairProcessPending) || f.root.starts != 2 {
		t.Fatal("unfinished original read did not continue", err, f.root.starts)
	}
}

// Loaded manager overrides cannot hide behind matching original unit bytes.
func TestRepairRootPassiveLoadedOverrideCannotStart(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	f.root.manager["Environment"] = "SYNTHETIC_OVERRIDE=1"
	_, complete, err := f.resume()
	if err == nil || complete || f.root.starts != 1 {
		t.Fatal("loaded override passed exact original profile", complete, err, f.root.starts)
	}
}

// Repair defaults give repeatable reads one five-minute owner with a one-minute
// minimum, independent of the old activation's finite command timeout.
func TestRepairRootPassiveReadOwnerBudgetIsBounded(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	if f.envelope.profile().CommandTimeoutSeconds != 300 {
		t.Fatal("repair did not retain the default read owner")
	}
	observed := false
	execute := f.root.host.files.host.execute
	f.root.host.files.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < time.Minute || time.Until(deadline) > 300*time.Second {
			t.Fatal("actual manager read inherited a short or unbounded owner", deadline, ok)
		}
		observed = true
		return execute(ctx, path, args)
	}
	if err := f.envelope.ready(f.ctx(), f.root.host.files.host, f.root.now); err != nil || !observed {
		t.Fatal("actual bounded read owner was not observed", observed, err)
	}
	for _, seconds := range []uint32{1, 59, 901} {
		f.envelope.approval.Plan.ReadTimeoutSeconds = seconds
		f.signInvalid(t)
		if err := f.envelope.validate(f.key); err == nil {
			t.Fatal("invalid read owner was admitted", seconds)
		}
	}
}

// Original activation bytes and their own signature remain independently bound.
func TestRepairRootPassiveLoadVerifiesOriginalApproval(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadRepairRootPassiveEnvelope(f.ctx(), raw, f.key, f.root.host.files.host)
	if err != nil || loaded.approvalHash() != f.envelope.approvalHash() {
		t.Fatal("original role envelope did not load", err)
	}
	changed := f.root.approval
	changed.Signature = strings.Repeat("0", 128)
	ref := bootstrapRootTestWrite(t, f.root.path, changed)
	f.envelope.approval.Plan.OriginalApproval = ref
	f.signInvalid(t)
	raw, err = json.Marshal(f.envelope.approval)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadRepairRootPassiveEnvelope(f.ctx(), raw, f.key, f.root.host.files.host); err == nil {
		t.Fatal("new incident signature replaced original activation authority")
	}
}

// A missing generation marker cannot be recreated from an intact incident file.
func TestRepairRootPassiveMissingGenerationClaimCannotResume(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	if err := os.Remove(repairProcessClaimPath(f.envelope.profile())); err != nil {
		t.Fatal(err)
	}
	_, complete, err := f.resume()
	if err == nil || complete || f.root.starts != 1 {
		t.Fatal("missing generation claim recreated start permission", complete, err, f.root.starts)
	}
}

// A new explicit incident can follow only the previous durably acknowledged
// repair generation while retaining the same original activation and checkpoint.
func TestRepairRootPassiveSuccessorUsesAcknowledgedRepair(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	if _, _, err := f.resume(); !errors.Is(err, errRepairProcessPending) {
		t.Fatal(err)
	}
	f.checkpoint(f.root.now)
	if _, complete, err := f.resume(); err != nil || !complete {
		t.Fatal("initial repair did not complete", complete, err)
	}
	ref := func(path string) planFileReference {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
	}
	prior := &repairProcessPredecessor{Approval: ref(f.path), PublicKey: f.key, Journal: ref(f.envelope.profile().StatePath)}
	plan := f.envelope.approval.Plan
	plan.Predecessor = prior
	plan.Previous = repairValidatorGeneration{InvocationId: strings.Repeat("6", 32), Pid: 4322, StartedUsec: 201}
	plan.OriginalCheckpoint = ref(plan.OriginalCheckpoint.Path)
	raw, err := os.ReadFile(plan.OriginalCheckpoint.Path)
	if err != nil || json.Unmarshal(raw, &plan.Checkpoint) != nil {
		t.Fatal("continued checkpoint unavailable", err)
	}
	f.root.now = f.root.now.Add(time.Minute)
	plan.IncidentAt, plan.ValidFrom, plan.ExpiresAt = f.root.now, f.root.now, f.root.now.Add(time.Hour)
	plan.StatePath = filepath.Join(filepath.Dir(f.path), "successor-repair-state.json")
	prepareMainnetSnapshotTest(t, plan.StatePath, "mainnet-host-action", 64*1024)
	f.path = filepath.Join(filepath.Dir(f.path), "successor-repair-approval.json")
	f.envelope.approval.Plan = plan
	f.sign()
	f.root.manager["MainPID"], f.root.manager["ActiveState"], f.root.manager["SubState"] = "0", "inactive", "dead"
	f.root.host.files.host.monotonic = func() (uint64, error) { return 300, nil }
	execute := f.root.host.files.host.execute
	f.root.host.files.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		raw, err := execute(ctx, path, args)
		if strings.Contains(strings.Join(args, " "), " start -- ") {
			for key, value := range map[string]string{"MainPID": "4323", "ExecMainPID": "4323", "ExecMainStartTimestampMonotonic": "301", "InvocationID": strings.Repeat("9", 32)} {
				f.root.manager[key] = value
			}
		}
		return raw, err
	}
	if err := f.envelope.claim(f.ctx(), f.key, f.root.host.files.host, func() time.Time { return f.root.now }); err != nil {
		t.Fatal("explicit successor could not retain original generation lineage", err)
	}
	status, complete, err := f.resume()
	if status != "waiting-progress" || complete || !errors.Is(err, errRepairProcessPending) || f.root.starts != 3 {
		t.Fatal("successor did not continue exactly once", status, complete, err, f.root.starts)
	}
	f.checkpoint(f.root.now)
	if _, complete, err := f.resume(); err != nil || !complete || f.root.starts != 3 {
		t.Fatal("successor checkpoint did not complete", complete, err, f.root.starts)
	}
}

// A returned clock contradiction remains hard even during an availability gap.
func TestRepairRootPassiveFutureCheckpointIsIntegrityFailure(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	f.claim()
	if _, _, err := f.resume(); !errors.Is(err, errRepairProcessPending) {
		t.Fatal(err)
	}
	f.checkpoint(f.root.now.Add(time.Second))
	_, complete, err := f.resume()
	if complete || !errors.Is(err, errRpcIntegrity) || f.root.starts != 2 {
		t.Fatal("future checkpoint became successful recovery", complete, err, f.root.starts)
	}
}
