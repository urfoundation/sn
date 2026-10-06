// Synthetic independent bootstrap approvals drive real files, journals and
// finalized RPC reads. The manager/device authority remains an explicit fixture.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type validatorActivationFixture struct {
	t              *testing.T
	chain          *bootstrapChainFixture
	approval       validatorActivationApproval
	path           string
	key            string
	private        ed25519.PrivateKey
	host           *validatorActivationHost
	now            time.Time
	managers       [2]map[string]string
	starts         [2]int
	reloads        int
	progress       [2]bool
	startError     error
	authorityCalls int
}

// This port is deliberately test-only: it checks the request it was supplied
// but does not pretend synthetic fixtures prove production operator/custody facts.
func (self *validatorActivationFixture) authorizeActivation(ctx context.Context, approval validatorActivationApproval, preparation bootstrapChainPreparation, readiness bootstrapChainReadiness, index int) error {
	if ctx.Err() != nil || approval.Plan.PlanHash != self.approval.Plan.PlanHash || preparation.Plan.ContentHash != approval.Plan.PlanHash || readiness.PlanHash != approval.Plan.PlanHash || len(readiness.UrValidators[index].ActivationBlockers) == 0 {
		return errors.New("synthetic current authority received substituted scope")
	}
	self.authorityCalls++
	return nil
}

func newValidatorActivationFixture(t *testing.T) *validatorActivationFixture {
	return newValidatorActivationFixtureWithNativeMetadata(t, nil)
}

// Metadata changes precede independent approval and custody, so codec denial
// tests exercise an approved artifact without mutating a retained plan.
func newValidatorActivationFixtureWithNativeMetadata(t *testing.T, mutate func(*types.Metadata)) *validatorActivationFixture {
	return newValidatorActivationFixtureWithNativeScope(t, mutate, nil)
}

// All signed native restrictions are fixed before original bootstrap claim.
func newValidatorActivationFixtureWithNativeScope(t *testing.T, mutate func(*types.Metadata), configureApproval func(*bootstrapChainValidatorFixture)) *validatorActivationFixture {
	return newValidatorActivationFixtureWithNativeCensus(t, mutate, configureApproval, nil)
}

// A further native profile is selected before approval and permanent custody.
func newValidatorActivationFixtureWithNativeCensus(t *testing.T, mutate func(*types.Metadata), configureApproval func(*bootstrapChainValidatorFixture), configureCensus func(*rootRpcFixture, *subnetCensusPolicy)) *validatorActivationFixture {
	t.Helper()
	chain := newBootstrapChainReadinessFixtureWithCensus(t, func(census *rootRpcFixture, policy *subnetCensusPolicy) {
		metadata, encoded, hash := economicEmissionTestMetadata(t, mutate)
		census.metadata, census.metadataHex, census.policy.RuntimeMetadataHash = metadata, encoded, hash
		policy.RuntimeMetadataHash = hash
		arg := []byte{25, 0}
		census.set(t, "MechanismCountCurrent", []byte{1}, arg)
		census.set(t, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 20), arg)
		census.set(t, "PendingServerEmission", make([]byte, 8), arg)
		census.set(t, "LastUpdate", subnetTestVector(make([]byte, 6*8), 8), arg)
		census.set(t, "RecycleOrBurn", []byte{1}, arg)
		if configureCensus != nil {
			configureCensus(census, policy)
		}
	}, configureApproval)
	return newValidatorActivationFixtureForChain(t, chain)
}

// Host approval binds the already prepared chain regardless of its independently
// selected legacy or passive root policy.
func newValidatorActivationFixtureForChain(t *testing.T, chain *bootstrapChainFixture) *validatorActivationFixture {
	t.Helper()
	directory := filepath.Dir(chain.path)
	hostRoot := filepath.Dir(directory)
	if err := os.Chmod(hostRoot, 0755); err != nil {
		t.Fatal(err)
	}
	unitDirectory := filepath.Join(hostRoot, "systemd")
	if err := os.Mkdir(unitDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	server := bootstrapReadinessTestServer(t, chain.census)
	now := time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x38}, ed25519.SeedSize))
	f := &validatorActivationFixture{t: t, chain: chain, path: filepath.Join(directory, "activation-approval.json"), key: "0x" + hex.EncodeToString(private.Public().(ed25519.PublicKey)), private: private, now: now, progress: [2]bool{true, true}}
	binary := planFileReference{Path: filepath.Join(hostRoot, "validator"), Sha256: monitorReadDigest([]byte("synthetic pinned standard validator\n"))}
	systemctl := planFileReference{Path: filepath.Join(hostRoot, "systemctl"), Sha256: monitorReadDigest([]byte("synthetic pinned systemctl\n"))}
	repairValidatorTestWrite(t, binary.Path, []byte("synthetic pinned standard validator\n"), 0755)
	repairValidatorTestWrite(t, systemctl.Path, []byte("synthetic pinned systemctl\n"), 0755)
	p := validatorActivationPlan{Preparation: planFileReference{Path: chain.path, Sha256: chain.preparation.Plan.ConfigSha256}, PlanHash: chain.preparation.Plan.ContentHash,
		MachineId: strings.Repeat("3", 32), BootId: "44444444-4444-4444-4444-444444444444", Systemctl: systemctl, RequiredMounts: []string{"-.mount"},
		Route: ownedSubmissionRoute{RpcUrl: server.URL, ReadRetrySeconds: 60}, StatePath: filepath.Join(directory, "activation-state.json"),
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaximumOperations: 32, CommandTimeoutSeconds: 5, MaximumSampleAgeSeconds: 120, InstallStaticUnits: true, AuthorizeProductionValidator: true}
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if uid == 0 {
		uid, gid = 65534, 65534
	}
	for i, role := range chain.config.Validators {
		inspection := chain.preparation.Plan.ValidatorInspections[i]
		state := filepath.Join(hostRoot, "operations-"+role.Role)
		if err := os.MkdirAll(state, 0700); err != nil {
			t.Fatal(err)
		}
		if os.Getuid() == 0 {
			if err := os.Chown(state, int(uid), int(gid)); err != nil {
				t.Fatal(err)
			}
		}
		configDirectory := filepath.Join(hostRoot, "runtime-"+role.Role)
		if err := os.Mkdir(configDirectory, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(configDirectory, os.Geteuid(), int(gid)); err != nil {
			t.Fatal(err)
		}
		unit := repairValidatorUnit{Name: "sn-mainnet-validator-" + role.Role + ".service", Binary: binary, Config: planFileReference{Path: filepath.Join(configDirectory, "validator.yml"), Sha256: role.Config.Sha256}, StateDirectory: state, ProgressFile: filepath.Join(state, "progress.json"), Uid: uid, Gid: gid}
		unit.File = planFileReference{Path: filepath.Join(unitDirectory, unit.Name), Sha256: monitorReadDigest(unit.render())}
		source := protocol.ValidatorProgressSource{ConfigHash: "sha256:" + hex.EncodeToString(inspection.Approval.ConfigHash[:]), DeploymentId: chain.config.DeploymentId, ValidatorId: role.ValidatorId, ChainId: mainnetEvmChainId, GenesisHash: chain.config.Network.GenesisHash, Netuid: 25}
		p.Units = append(p.Units, validatorActivationUnit{Role: role.Role, Unit: unit, Source: source})
	}
	// The protocol roots and operational output roots are separate explicit
	// directories. This fixture provisions both before signing unit authority.
	roots := []string{directory, chain.config.RunDirectory}
	for i := range p.Units {
		paths := []string{chain.validators[i].config.StateDir}
		for _, operator := range chain.validators[i].config.Operators {
			paths = append(paths, operator.StateDir)
		}
		for _, operator := range chain.validators[i].config.EvidenceV2.Operators {
			paths = append(paths, operator.ReplayScratchRoot, operator.SealScratchRoot, filepath.Dir(operator.Activation.Path))
		}
		for _, path := range paths {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		roots = append(roots, p.Units[i].Unit.StateDirectory)
	}
	chain.root.storage = durablefixture.New(t, t.Context(), roots...)
	chain.contracts.storage = chain.root.storage
	for i := range p.Units {
		p.Units[i].Unit.DurableVolumes = &chain.root.storage.Reference
		p.Units[i].Unit.File.Sha256 = monitorReadDigest(p.Units[i].Unit.render())
	}
	prepareMainnetSnapshotTest(t, p.StatePath, "mainnet-validator-activation", 128*1024)
	f.approval = validatorActivationApproval{Schema: validatorActivationSchema, Plan: p}
	h := &repairValidatorHost{rootUid: uint32(os.Geteuid()), trustRoot: hostRoot, machinePath: filepath.Join(directory, "machine-id"), bootPath: filepath.Join(directory, "boot-id"), cgroupRoot: filepath.Join(directory, "cgroup"), execute: f.execute, monotonic: func() (uint64, error) { return 150, nil }, cgroupType: func(string) (int64, error) { return unix.CGROUP2_SUPER_MAGIC, nil }}
	f.host = &validatorActivationHost{host: h, unitDirectory: unitDirectory}
	h.storageCommand = serviceStorageTestTransport(t)
	repairValidatorTestWrite(t, h.machinePath, []byte(p.MachineId+"\n"), 0644)
	repairValidatorTestWrite(t, h.bootPath, []byte(p.BootId+"\n"), 0644)
	if err := os.Mkdir(h.cgroupRoot, 0700); err != nil {
		t.Fatal(err)
	}
	for i, item := range p.Units {
		u := item.Unit
		f.managers[i] = map[string]string{}
		for _, key := range repairValidatorProperties {
			f.managers[i][key] = ""
		}
		for key, value := range map[string]string{"Id": u.Name, "LoadState": "loaded", "FragmentPath": u.File.Path, "NeedDaemonReload": "no", "Transient": "no", "Type": "exec", "User": strconv.FormatUint(uint64(uid), 10), "Group": strconv.FormatUint(uint64(gid), 10), "WorkingDirectory": u.StateDirectory, "Restart": "no", "KillMode": "control-group", "Delegate": "no", "Job": "0", "ControlPID": "0", "MainPID": "0", "ExecMainPID": "0", "ExecMainStartTimestampMonotonic": "0", "ActiveState": "inactive", "SubState": "dead", "ControlGroup": "", "Requires": "system.slice -.mount", "Slice": "system.slice", "ExecStart": "{ path=" + u.Binary.Path + " ; argv[]=" + u.Binary.Path + " run --config=" + u.Config.Path + " --progress-file=" + u.ProgressFile + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0 }"} {
			f.managers[i][key] = value
		}
	}
	for i, item := range p.Units {
		f.managers[i]["ExecStart"] = strings.Replace(f.managers[i]["ExecStart"], " ; ignore_errors=", unitDurableArguments(item.Unit.DurableVolumes)+" ; ignore_errors=", 1)
	}
	f.sign()
	return f
}

func (self *validatorActivationFixture) sign() {
	self.t.Helper()
	message, err := self.approval.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.approval.Signature = hex.EncodeToString(ed25519.Sign(self.private, message))
	if err := self.approval.validate(self.key); err != nil {
		self.t.Fatal("original independently signed activation fixture refused", err)
	}
	bootstrapRootTestWrite(self.t, self.path, self.approval)
}

func (self *validatorActivationFixture) publish(index int) {
	self.t.Helper()
	record := monitorServicesTestRecord(self.now, 71)
	record.Source = self.approval.Plan.Units[index].Source
	record.InstanceId = strings.Repeat(fmt.Sprintf("%x", 8+index), 32)
	record.StartedAt = self.now.Format(time.RFC3339Nano)
	raw, err := record.Encode()
	if err != nil {
		self.t.Fatal(err)
	}
	unit := self.approval.Plan.Units[index].Unit
	repairValidatorTestWrite(self.t, unit.ProgressFile, raw, 0644)
	if os.Getuid() == 0 {
		if err := os.Chown(unit.ProgressFile, int(unit.Uid), int(unit.Gid)); err != nil {
			self.t.Fatal(err)
		}
	}
}

func (self *validatorActivationFixture) execute(ctx context.Context, path string, args []string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if path != self.approval.Plan.Systemctl.Path {
		return nil, errors.New("synthetic manager path differs")
	}
	joined := strings.Join(args, " ")
	if joined == "--system --no-pager --no-ask-password daemon-reload" {
		self.reloads++
		return nil, nil
	}
	name := args[len(args)-1]
	if strings.Contains(joined, " show ") {
		for i, item := range self.approval.Plan.Units {
			if name == item.Unit.Name {
				var output strings.Builder
				for _, key := range repairValidatorProperties {
					fmt.Fprintf(&output, "%s=%s\n", key, self.managers[i][key])
				}
				return []byte(output.String()), nil
			}
		}
		return []byte("Id=" + name + "\nLoadState=loaded\nActiveState=active\nJob=0\n"), nil
	}
	for i, item := range self.approval.Plan.Units {
		if joined == "--system --no-pager --no-ask-password --job-mode=fail start -- "+item.Unit.Name {
			self.starts[i]++
			m := self.managers[i]
			m["ActiveState"], m["SubState"], m["MainPID"], m["ExecMainPID"], m["ExecMainStartTimestampMonotonic"], m["InvocationID"], m["ControlGroup"] = "active", "running", strconv.Itoa(72+i), strconv.Itoa(72+i), "200", strings.Repeat(fmt.Sprintf("%x", 6+i), 32), "/system.slice/"+item.Unit.Name
			if self.progress[i] {
				self.publish(i)
			}
			return nil, self.startError
		}
	}
	return nil, errors.New("synthetic manager refused unknown effect")
}

func (self *validatorActivationFixture) command(ctx context.Context, operation string, authority validatorActivationAuthority) (validatorActivationResult, int, string) {
	self.t.Helper()
	ctx = self.chain.storageContext(ctx)
	raw, err := os.ReadFile(self.path)
	if err != nil {
		self.t.Fatal(err)
	}
	args := []string{operation, "--approval", self.path, "--accept-approval-hash", monitorReadDigest(raw), "--independent-public-key", self.key}
	if operation == "start" {
		args = append(args, "--execute-approved-starts")
	}
	var stdout, stderr bytes.Buffer
	code := runValidatorActivationCommandWithHost(ctx, args, &stdout, &stderr, func() time.Time { return self.now }, self.host, authority)
	var result validatorActivationResult
	if stdout.Len() != 0 {
		if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
			self.t.Fatal("activation output invalid", err, stdout.String())
		}
	}
	return result, code, stderr.String()
}

func (self *validatorActivationFixture) installed() {
	self.t.Helper()
	for _, operation := range []string{"claim", "install"} {
		result, code, detail := self.command(self.t.Context(), operation, nil)
		if code != 0 {
			self.t.Fatal("original two-unit installation refused", operation, code, result.Status, detail)
		}
	}
	if self.starts != [2]int{} {
		self.t.Fatal("installation started a validator")
	}
}

func TestValidatorActivationSignedBootstrapBinding(t *testing.T) {
	f := newValidatorActivationFixture(t)
	if _, err := loadValidatorActivationPreparation(t.Context(), f.approval); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"signature", "key", "plan", "config", "source", "role", "window", "write-route", "alias"} {
		raw, _ := json.Marshal(f.approval)
		var a validatorActivationApproval
		json.Unmarshal(raw, &a)
		key := f.key
		switch change {
		case "signature":
			a.Signature = strings.Repeat("0", 128)
		case "key":
			key = "0x" + strings.Repeat("3", 64)
		case "plan":
			a.Plan.PlanHash = monitorReadDigest([]byte("other plan"))
		case "config":
			a.Plan.Units[0].Unit.Config = f.approval.Plan.Units[1].Unit.Config
		case "source":
			a.Plan.Units[0].Source.ConfigHash = monitorReadDigest([]byte("other producer"))
		case "role":
			a.Plan.Units[0].Role = "secondary"
		case "window":
			a.Plan.ExpiresAt = a.Plan.ValidFrom.Add(25 * time.Hour)
		case "write-route":
			a.Plan.Route.SendTimeoutSeconds = 1
		case "alias":
			a.Plan.StatePath = a.Plan.Units[0].Unit.Config.Path
		}
		if err := a.validate(key); err == nil {
			t.Fatal("changed activation authority accepted", change)
		}
	}
	a := f.approval
	a.Plan.Units = append([]validatorActivationUnit(nil), a.Plan.Units...)
	a.Plan.Units[0].Source.ConfigHash = monitorReadDigest([]byte("signed different source"))
	message, _ := a.signingBytes()
	a.Signature = hex.EncodeToString(ed25519.Sign(f.private, message))
	if err := a.validate(f.key); err != nil {
		t.Fatal(err)
	}
	if _, err := loadValidatorActivationPreparation(t.Context(), a); err == nil {
		t.Fatal("separately signed unrelated producer escaped bootstrap binding")
	}
}

func TestValidatorActivationInstallsAndAdmitsBothWithoutStarting(t *testing.T) {
	f := newValidatorActivationFixture(t)
	original := f.chain.journals(t)
	f.installed()
	for _, item := range f.approval.Plan.Units {
		raw, err := os.ReadFile(item.Unit.File.Path)
		if err != nil || !bytes.Equal(raw, item.Unit.render()) {
			t.Fatal("installed unit differs", err)
		}
	}
	result, code, detail := f.command(t.Context(), "admit", nil)
	if code != 0 || result.Status != "admitted-process-only" || result.Readiness == nil || len(result.Readiness.Roles) != 2 || result.ActivationReady || result.ChainSuccessProven || result.RootServiceActive || f.starts != [2]int{} || f.reloads != 1 {
		t.Fatal("two-role admission changed authority", code, result, detail)
	}
	if !reflect.DeepEqual(original, f.chain.journals(t)) {
		t.Fatal("activation admission changed original custody")
	}
	result, code, detail = f.command(t.Context(), "start", nil)
	if code == 0 || result.Status != "activation-authority-unavailable" || f.starts != [2]int{} || !result.Units[0].StartAt.IsZero() || !result.Units[1].StartAt.IsZero() {
		t.Fatal("public start escaped missing production authority", code, result, detail)
	}
}

func TestValidatorActivationPartialStartSurvivesRestart(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	f.progress[0] = false
	result, code, detail := f.command(t.Context(), "start", f)
	if code == 0 || f.starts != [2]int{1, 0} || result.Units[0].Generation == nil || result.Units[0].Completed != nil || !result.Units[1].StartAt.IsZero() {
		t.Fatal("partial start lost independent custody", code, result, detail)
	}
	f.publish(0)
	result, code, detail = f.command(t.Context(), "start", f)
	if code != 0 || result.Status != "processes-observed" || f.starts != [2]int{1, 1} || result.Units[0].Completed == nil || result.Units[1].Completed == nil || result.ChainSuccessProven {
		t.Fatal("partial start did not reconcile exact invocations", code, result, detail)
	}
	for i := 0; i < 2; i++ {
		if _, code, detail = f.command(t.Context(), "resume", nil); code != 0 || f.starts != [2]int{1, 1} {
			t.Fatal("completed pair issued another start", code, detail, f.starts)
		}
	}
	if f.authorityCalls != 2 {
		t.Fatal("each start did not require independent current authority", f.authorityCalls)
	}
}

func TestValidatorActivationLostAcknowledgementCannotAdoptAnotherInvocation(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	f.startError = errors.New("synthetic lost acknowledgement")
	result, code, detail := f.command(t.Context(), "start", f)
	if code == 0 || f.starts != [2]int{1, 0} || result.Units[0].StartAt.IsZero() || result.Units[0].Generation != nil {
		t.Fatal("uncertain start was not consumed", code, result, detail)
	}
	f.startError = nil
	for _, operation := range []string{"resume", "start"} {
		result, code, detail = f.command(t.Context(), operation, f)
		if code == 0 || f.starts != [2]int{1, 0} || result.Units[0].Generation != nil || !strings.Contains(result.Disposition, "Manual host reconciliation") {
			t.Fatal("uncertain start adopted an unrelated invocation or retried", code, result, detail)
		}
	}
}

func TestValidatorActivationRejectsDriftAndDescendantCgroup(t *testing.T) {
	for _, change := range []string{"binary", "config", "loaded-command", "prior-generation", "descendant-cgroup"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		unit := f.approval.Plan.Units[0].Unit
		switch change {
		case "binary":
			repairValidatorTestWrite(t, unit.Binary.Path, []byte("changed binary"), 0755)
		case "config":
			if err := os.Chmod(unit.Config.Path, 0600); err != nil {
				t.Fatal(err)
			}
			repairValidatorTestWrite(t, unit.Config.Path, []byte("changed config"), 0600)
			if err := os.Chmod(unit.Config.Path, 0440); err != nil {
				t.Fatal(err)
			}
		case "loaded-command":
			f.managers[0]["ExecStart"] = "{ path=/bin/false ; argv[]=/bin/false ; ignore_errors=no ; }"
		case "prior-generation":
			f.managers[0]["ExecMainPID"], f.managers[0]["ExecMainStartTimestampMonotonic"] = "72", "1"
		case "descendant-cgroup":
			group := filepath.Join(f.host.host.cgroupRoot, "system.slice", unit.Name)
			if err := os.MkdirAll(group, 0700); err != nil {
				t.Fatal(err)
			}
			repairValidatorTestWrite(t, filepath.Join(group, "cgroup.events"), []byte("populated 1\nfrozen 0\n"), 0644)
			repairValidatorTestWrite(t, filepath.Join(group, "cgroup.procs"), nil, 0644)
		}
		result, code, detail := f.command(t.Context(), "start", f)
		if code == 0 || f.starts != [2]int{} || !result.Units[0].StartAt.IsZero() {
			t.Fatal("changed host or descendant signer escaped start admission", change, code, result, detail)
		}
	}
}

func TestValidatorActivationRejectsCurrentPermitAndGeneration(t *testing.T) {
	for _, change := range []string{"permit", "generation"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		if change == "permit" {
			f.chain.census.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 0, 1, 0, 0}, 1), []byte{25, 0})
		} else {
			f.chain.census.set(t, "BlockAtRegistration", []byte{99, 0, 0, 0, 0, 0, 0, 0}, []byte{25, 0}, []byte{2, 0})
		}
		result, code, detail := f.command(t.Context(), "start", f)
		if code == 0 || f.starts != [2]int{} || !result.Units[0].StartAt.IsZero() || f.authorityCalls != 0 {
			t.Fatal("stale current role reached production authority", change, code, result, detail)
		}
	}
}

func TestValidatorActivationPostSyncWindowConsumesWithoutStarting(t *testing.T) {
	for _, change := range []string{"expiry", "read-age", "cancellation"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		ctx, cancel := context.WithCancel(f.chain.storageContext(t.Context()))
		store, err := openValidatorActivationStore(ctx, f.approval, f.key, false, f.now)
		if err != nil {
			t.Fatal(err)
		}
		fired := false
		store.syncDirectory = func(file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			raw, err := os.ReadFile(store.path)
			if err != nil {
				return err
			}
			var record validatorActivationRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			if !fired && !record.Units[0].StartAt.IsZero() {
				fired = true
				switch change {
				case "expiry":
					f.now = f.approval.Plan.ExpiresAt
				case "read-age":
					f.now = f.now.Add(121 * time.Second)
				case "cancellation":
					cancel()
				}
			}
			return nil
		}
		result, err := advanceValidatorActivation(ctx, store, f.host, f, "start", func() time.Time { return f.now })
		closeErr := store.close()
		cancel()
		if err == nil || closeErr != nil || !fired || f.starts != [2]int{} || result.Units[0].StartAt.IsZero() {
			t.Fatal("post-sync refusal refunded or issued a start", change, result, err, closeErr)
		}
		result, code, detail := f.command(t.Context(), "resume", nil)
		if code == 0 || f.starts != [2]int{} || result.Units[0].StartAt.IsZero() {
			t.Fatal("restart lost post-sync consumption", change, code, result, detail)
		}
	}
}

func TestValidatorActivationAmbiguousAcknowledgementWriteRecoversExactBytes(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(f.chain.storageContext(t.Context()), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	store.syncDirectory = func(file *os.File) error {
		raw, err := os.ReadFile(store.path)
		if err != nil {
			return err
		}
		var record validatorActivationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		if record.Units[0].Generation != nil {
			return errors.New("synthetic acknowledgement directory sync failed")
		}
		return file.Sync()
	}
	result, err := advanceValidatorActivation(f.chain.storageContext(t.Context()), store, f.host, f, "start", func() time.Time { return f.now })
	if err == nil || f.starts != [2]int{1, 0} || result.Units[0].Generation == nil {
		t.Fatal("ambiguous acknowledged start not retained", result, err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	result, code, detail := f.command(t.Context(), "start", f)
	if code != 0 || f.starts != [2]int{1, 1} || result.Units[0].Completed == nil {
		t.Fatal("reopen replayed acknowledged start", code, result, detail)
	}
}

// Missing host storage authority refuses before opening the original owner.
// Supplying that same prepared declaration later does not rewrite its history.
func TestValidatorActivationRequiresDeclaredStorageBeforeReopen(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	original := mainnetNamespaceTest(t, filepath.Dir(f.approval.Plan.StatePath))
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if store != nil {
		_ = store.close()
		t.Fatal("undeclared activation acquired an original journal owner")
	}
	if err == nil || !strings.Contains(err.Error(), "explicit durable-volume declaration and hash are required") || f.starts != [2]int{} {
		t.Fatal("undeclared activation did not refuse at storage admission", err, f.starts)
	}
	if !reflect.DeepEqual(original, mainnetNamespaceTest(t, filepath.Dir(f.approval.Plan.StatePath))) {
		t.Fatal("missing declaration changed original activation custody")
	}
	ctx := f.chain.storageContext(t.Context())
	store, err = openValidatorActivationStore(ctx, f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal("explicit original storage declaration could not reopen activation", err)
	}
	defer store.close()
	record, readErr := store.load(ctx)
	closeErr := store.close()
	if readErr != nil || closeErr != nil || record.Status != "installed" || record.Operations != 1 ||
		!record.Units[0].StartAt.IsZero() || !record.Units[1].StartAt.IsZero() || f.starts != [2]int{} {
		t.Fatal("declared reopen changed installed authority or consumed a start", record, readErr, closeErr, f.starts)
	}
	if !reflect.DeepEqual(original, mainnetNamespaceTest(t, filepath.Dir(f.approval.Plan.StatePath))) {
		t.Fatal("declared reopen rewrote original activation custody")
	}
}

func TestValidatorActivationInstallationNeverClobbersAnotherUnit(t *testing.T) {
	f := newValidatorActivationFixture(t)
	if _, code, detail := f.command(t.Context(), "claim", nil); code != 0 {
		t.Fatal(detail)
	}
	foreign := []byte("[Service]\nExecStart=/bin/false\n")
	path := f.approval.Plan.Units[1].Unit.File.Path
	repairValidatorTestWrite(t, path, foreign, 0644)
	result, code, detail := f.command(t.Context(), "install", nil)
	raw, err := os.ReadFile(path)
	if code == 0 || err != nil || !bytes.Equal(raw, foreign) || !result.Units[0].Installed || result.Units[1].Installed || f.starts != [2]int{} {
		t.Fatal("partial installation overwrote foreign unit", code, result, detail, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	} // Explicit synthetic operator disposition.
	result, code, detail = f.command(t.Context(), "install", nil)
	if code != 0 || !result.Units[0].Installed || !result.Units[1].Installed || f.starts != [2]int{} {
		t.Fatal("installation could not reconcile exact prior unit", code, result, detail)
	}
}

func TestValidatorActivationCancellationJoinsAndReleasesOwnership(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	entered := make(chan struct{})
	finished := make(chan struct{})
	original := f.host.host.execute
	f.host.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), " show ") {
			close(entered)
			<-ctx.Done()
			close(finished)
			return nil, ctx.Err()
		}
		return original(ctx, path, args)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	go func() { _, code, _ := f.command(ctx, "admit", nil); done <- code }()
	<-entered
	if store, err := openValidatorActivationStore(f.chain.storageContext(t.Context()), f.approval, f.key, false, f.now); !errors.Is(err, durablevolume.ErrBusy) {
		if store != nil {
			store.close()
		}
		t.Fatal("concurrent activation did not reach the original busy owner", err)
	}
	cancel()
	code := <-done
	joined = true
	if code == 0 {
		t.Fatal("canceled activation reported success")
	}
	select {
	case <-finished:
	default:
		t.Fatal("activation returned before joining manager")
	}
	f.host.host.execute = original
	if _, code, detail := f.command(t.Context(), "status", nil); code != 0 {
		t.Fatal("canceled owner leaked lock", code, detail)
	}
	if f.starts != [2]int{} {
		t.Fatal("cancellation caused an unapproved start")
	}
}

func TestValidatorActivationMissingStateAndOperationLimitNeverRenew(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.approval.Plan.MaximumOperations = 1
	f.sign()
	if _, code, detail := f.command(t.Context(), "claim", nil); code != 0 {
		t.Fatal(detail)
	}
	if _, code, detail := f.command(t.Context(), "install", nil); code != 0 {
		t.Fatal(detail)
	}
	result, code, detail := f.command(t.Context(), "start", f)
	if code == 0 || result.Status != "operation-limit" || f.starts != [2]int{} {
		t.Fatal("operation allowance replenished", code, result, detail)
	}
	if err := os.Remove(f.approval.Plan.StatePath); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"claim", "resume"} {
		if _, code, detail := f.command(t.Context(), operation, nil); code == 0 {
			t.Fatal("missing journal became fresh authority", operation, detail)
		}
	}
}

// The public parser cannot accept credentials, an alternate authority or an
// implicit start. These invalid calls never reach even synthetic host effects.
func TestValidatorActivationPublicFlagsDoNotInstallAuthority(t *testing.T) {
	f := newValidatorActivationFixture(t)
	for _, args := range [][]string{{"start"}, {"install", "--execute-approved-starts"}, {"start", "--authority-ready"}, {"start", "--hotkey-seed", "secret"}} {
		var out, diagnostic bytes.Buffer
		if code := runValidatorActivationCommandWithHost(t.Context(), args, &out, &diagnostic, func() time.Time { return f.now }, f.host, nil); code != 2 {
			t.Fatal("unsafe activation CLI flag admitted", args, code)
		}
	}
	if code := runValidatorActivationCommand(nil, []string{"status"}, io.Discard, io.Discard, time.Now); code != 2 {
		t.Fatal("nil activation context admitted")
	}
}

// A deployment with a missing production capability may be inspected many
// times. Those local refusals cannot exhaust its eventual signed operation cap.
func TestValidatorActivationAbsentCapabilityPreservesAllowance(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.approval.Plan.MaximumOperations = 3
	f.sign()
	if _, code, detail := f.command(t.Context(), "claim", nil); code != 0 {
		t.Fatal(detail)
	}
	before, err := os.ReadFile(f.approval.Plan.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i <= f.approval.Plan.MaximumOperations; i++ {
		if _, code, _ := f.command(t.Context(), "admit", nil); code == 0 {
			t.Fatal("missing installation reported admitted")
		}
	}
	after, err := os.ReadFile(f.approval.Plan.StatePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("local missing installation consumed signed allowance", err)
	}
	if _, code, detail := f.command(t.Context(), "install", nil); code != 0 {
		t.Fatal(detail)
	}
	before, err = os.ReadFile(f.approval.Plan.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i <= f.approval.Plan.MaximumOperations; i++ {
		if result, code, _ := f.command(t.Context(), "start", nil); code == 0 || result.Status != "activation-authority-unavailable" {
			t.Fatal("absent current authority admitted", code, result)
		}
	}
	after, err = os.ReadFile(f.approval.Plan.StatePath)
	if err != nil || !bytes.Equal(before, after) || f.starts != [2]int{} {
		t.Fatal("absent activation capability consumed signed allowance", err)
	}
}

// The production publisher requires an operational sibling outside protocol
// state. Re-signing an unsafe layout cannot make that producer path usable.
func TestValidatorActivationProgressRejectsProtocolPathsAndAliases(t *testing.T) {
	f := newValidatorActivationFixture(t)
	for _, path := range []string{
		f.chain.preparation.Plan.ValidatorInspections[0].DeclaredPaths[0],
		filepath.Join(f.chain.preparation.Plan.ValidatorInspections[0].DeclaredPaths[0], "nested"),
		filepath.Dir(f.chain.preparation.Plan.ValidatorInspections[0].DeclaredPaths[0]),
		f.chain.preparation.Plan.ValidatorInspections[1].DeclaredPaths[0],
	} {
		raw, _ := json.Marshal(f.approval)
		var approval validatorActivationApproval
		if err := json.Unmarshal(raw, &approval); err != nil {
			t.Fatal(err)
		}
		unit := &approval.Plan.Units[0].Unit
		unit.StateDirectory, unit.ProgressFile = path, filepath.Join(path, "progress.json")
		unit.File.Sha256 = monitorReadDigest(unit.render())
		message, _ := approval.signingBytes()
		approval.Signature = hex.EncodeToString(ed25519.Sign(f.private, message))
		if err := approval.validate(f.key); err != nil {
			t.Fatal("synthetic signed path refused before semantic check", err)
		}
		if _, err := loadValidatorActivationPreparation(t.Context(), approval); err == nil {
			t.Fatal("protocol or other-role progress path admitted", path)
		}
	}
	f.installed()
	u := f.approval.Plan.Units[0].Unit
	if err := os.Rename(u.StateDirectory, u.StateDirectory+"-real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(u.StateDirectory+"-real", u.StateDirectory); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := f.command(t.Context(), "start", f); code == 0 || f.starts != [2]int{} {
		t.Fatal("operational progress directory alias reached start")
	}
}

// The producer consumes the original semantic config through a distinct
// least-privilege runtime copy. Source path identity is not its byte hash.
func TestValidatorActivationRuntimeCopiesPreserveOriginalScope(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	for i, item := range f.approval.Plan.Units {
		source := f.chain.config.Validators[i].Config
		original, err := os.ReadFile(source.Path)
		if err != nil {
			t.Fatal(err)
		}
		copied, err := os.ReadFile(item.Unit.Config.Path)
		if err != nil || source.Path == item.Unit.Config.Path || source.Sha256 != item.Unit.Config.Sha256 || !bytes.Equal(original, copied) {
			t.Fatal("runtime config copy changed original source bytes or identity", err)
		}
		info, err := os.Lstat(item.Unit.Config.Path)
		if err != nil || info.Mode().Perm() != 0440 {
			t.Fatal("runtime config lost least-privilege publication", err)
		}
		inspection, err := validator.InspectProductionBootstrapConfig(t.Context(), item.Unit.Config.Path, copied)
		if err != nil || inspection == nil || inspection.Approval.ConfigHash != f.chain.preparation.Plan.ValidatorInspections[i].Approval.ConfigHash {
			t.Fatal("runtime copy changed actual production parser interpretation", err)
		}
		seedPath := f.chain.validators[i].config.HotkeySeedFile
		if !bytes.Contains(copied, []byte(seedPath)) {
			t.Fatal("synthetic config has no declared absolute hotkey path")
		}
		relative := bytes.Replace(copied, []byte(seedPath), []byte("hotkey.seed"), 1)
		if _, err := validator.InspectProductionBootstrapConfig(t.Context(), item.Unit.Config.Path, relative); err == nil {
			t.Fatal("runtime copy accepted changed relative-path semantics")
		}
	}
	if f.approval.Plan.Units[0].Unit.Config.Path == f.approval.Plan.Units[1].Unit.Config.Path || f.approval.Plan.Units[0].Unit.Config.Sha256 == f.approval.Plan.Units[1].Unit.Config.Sha256 {
		t.Fatal("two producer copies lost role isolation")
	}
}

// Stage cancellation cannot publish a truncated config. A changed original
// source after a successful copy remains a hard refusal on the next invocation.
func TestValidatorActivationInterruptedCopyAndChangedSourceRefuse(t *testing.T) {
	f := newValidatorActivationFixture(t)
	if _, code, detail := f.command(t.Context(), "claim", nil); code != 0 {
		t.Fatal(detail)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.host.afterStage = func(path string) error {
		if path == f.approval.Plan.Units[0].Unit.Config.Path {
			cancel()
		}
		return nil
	}
	if _, code, _ := f.command(ctx, "install", nil); code == 0 {
		t.Fatal("interrupted runtime config publication succeeded")
	}
	if _, err := os.Lstat(f.approval.Plan.Units[0].Unit.Config.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted runtime copy acquired its final name", err)
	}
	f.host.afterStage = nil
	if _, code, detail := f.command(t.Context(), "install", nil); code != 0 {
		t.Fatal("interrupted installation did not resume", code, detail)
	}
	path := f.approval.Plan.Units[1].Unit.Config.Path
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 3); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0440); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := f.command(t.Context(), "start", f); code == 0 || f.starts != [2]int{} {
		t.Fatal("truncated runtime copy reached a start")
	}
	config := f.chain.config.Validators[0].Config.Path
	original, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, config, append(original, '\n'), 0600)
	if _, code, _ := f.command(t.Context(), "admit", nil); code == 0 || f.starts != [2]int{} {
		t.Fatal("changed original source was replaced by its retained runtime copy")
	}
}
