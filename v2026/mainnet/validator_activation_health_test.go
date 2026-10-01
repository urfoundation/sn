// Synthetic manager state and real protected progress/journal files exercise
// health ownership. Constructed checkpoints test custody, never live authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// Record ownership and all parent permissions use the actual host reader;
// explicit directory modes keep both ordinary umasks equivalent.
func validatorActivationHealthTestProgress(t *testing.T, f *validatorActivationFixture, index int) (validatorActivationUnitState, protocol.ValidatorProgress) {
	t.Helper()
	unit := f.approval.Plan.Units[index].Unit
	if err := os.Chmod(unit.StateDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	value := monitorServicesTestRecord(f.now, 71)
	value.Source = f.approval.Plan.Units[index].Source
	value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: f.now.Format(time.RFC3339Nano), LastSuccessAt: f.now.Format(time.RFC3339Nano), Current: true, EpochKnown: true, NativeEpoch: 8, Outcome: "read_wait"}
	state := validatorActivationUnitState{StartAt: f.now.Add(-2 * time.Hour), Generation: &repairValidatorGeneration{InvocationId: strings.Repeat("9", 32), Pid: 71, StartedUsec: 200}, Completed: &repairValidatorPostcondition{InstanceId: value.InstanceId, ObservedAt: f.now, RecordHash: rootObjectHash(value)}}
	validatorActivationHealthTestWrite(t, f, index, value)
	return state, value
}

// The fixture publishes valid standard wire bytes with the approved service
// owner, including when the test runner itself happens to run as root.
func validatorActivationHealthTestWrite(t *testing.T, f *validatorActivationFixture, index int, value protocol.ValidatorProgress) {
	t.Helper()
	raw, err := value.Encode()
	if err != nil {
		t.Fatal(err)
	}
	unit := f.approval.Plan.Units[index].Unit
	if err := os.WriteFile(unit.ProgressFile, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unit.ProgressFile, 0644); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(unit.ProgressFile, int(unit.Uid), int(unit.Gid)); err != nil {
			t.Fatal(err)
		}
	}
}

// Fresh process progress can report a transient transport wait. It is retained
// as an operational warning without erasing completed protocol observations.
func TestValidatorActivationHealthWorkerSeparatesTransientWarnings(t *testing.T) {
	f := newValidatorActivationFixture(t)
	state, value := validatorActivationHealthTestProgress(t, f, 0)
	got, err := f.host.observeValidatorActivationWorker(t.Context(), f.approval.Plan, state, 0, nil, f.now)
	if err != nil || !got.Current || !reflect.DeepEqual(got.Record, &value) || !slices.Contains(got.Warnings, "STEERING_TRANSPORT_WAIT") || !slices.Contains(got.Warnings, "PER_OPERATOR_LIVE_WORKER_UNVERIFIED") {
		t.Fatal("actual standard progress refused or overstated", got, err)
	}
	if err := os.Remove(f.approval.Plan.Units[0].Unit.ProgressFile); err != nil {
		t.Fatal(err)
	}
	next, err := f.host.observeValidatorActivationWorker(t.Context(), f.approval.Plan, state, 0, &got, f.now.Add(time.Second))
	if err != nil || next.Current || !reflect.DeepEqual(next.Record, &value) || !slices.Contains(next.Warnings, "PROGRESS_UNAVAILABLE") {
		t.Fatal("read outage lost prior protocol clocks or became current", next, err)
	}
}

// Both intended fresh starts can lack progress. Neither absence nor a stale
// heartbeat grants current worker health or consumes an initial start.
func TestValidatorActivationHealthFreshAndStaleWorkersRemainWarnings(t *testing.T) {
	f := newValidatorActivationFixture(t)
	for i := range f.approval.Plan.Units {
		got, err := f.host.observeValidatorActivationWorker(t.Context(), f.approval.Plan, validatorActivationUnitState{}, i, nil, f.now)
		if err != nil || got.Current || got.Record != nil || len(got.Warnings) < 2 {
			t.Fatal("fresh stopped role not represented", i, got, err)
		}
	}
	state, value := validatorActivationHealthTestProgress(t, f, 0)
	got, err := f.host.observeValidatorActivationWorker(t.Context(), f.approval.Plan, state, 0, nil, f.now.Add(3*time.Minute))
	if err != nil || got.Current || !reflect.DeepEqual(got.Record, &value) || !slices.Contains(got.Warnings, "PROGRESS_STALE") {
		t.Fatal("stale heartbeat treated as integrity failure or healthy", got, err)
	}
	if f.starts != [2]int{} {
		t.Fatal("health observation started a worker")
	}
}

// Current-looking bytes from another source, instance or semantic epoch must
// refuse. A current heartbeat cannot conceal reset ages or a hard worker error.
func TestValidatorActivationHealthRejectsProgressContradictions(t *testing.T) {
	for _, fault := range []string{"source", "generation", "fresh-start", "started-clock", "heartbeat-rollback", "future-clock", "intent-age", "hard-error", "symlink", "owner"} {
		f := newValidatorActivationFixture(t)
		state, value := validatorActivationHealthTestProgress(t, f, 0)
		value.Intent.Value = &protocol.ValidatorIntentProgress{ConfigHash: value.Source.ConfigHash, VectorHash: "0x" + strings.Repeat("5", 64), NativeEpoch: 8, SettlementEpoch: 7, Status: "pending", CreatedAt: f.now.Add(-time.Hour).Format(time.RFC3339Nano), ProgressAt: f.now.Add(-time.Hour).Format(time.RFC3339Nano)}
		validatorActivationHealthTestWrite(t, f, 0, value)
		prior, err := f.host.observeValidatorActivationWorker(t.Context(), f.approval.Plan, state, 0, nil, f.now)
		if err != nil {
			t.Fatal("positive control", fault, err)
		}
		raw, _ := json.Marshal(value)
		var next protocol.ValidatorProgress
		if err := json.Unmarshal(raw, &next); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "source":
			next.Source.ValidatorId++
		case "generation":
			next.InstanceId = strings.Repeat("a", 32)
		case "fresh-start":
			state = validatorActivationUnitState{}
		case "started-clock":
			next.StartedAt = f.now.Add(-30 * time.Minute).Format(time.RFC3339Nano)
		case "heartbeat-rollback":
			next.HeartbeatAt = f.now.Add(-time.Second).Format(time.RFC3339Nano)
		case "future-clock":
			next.Native.ObservedAt = f.now.Add(time.Minute).Format(time.RFC3339Nano)
		case "intent-age":
			next.Intent.Value.CreatedAt = f.now.Format(time.RFC3339Nano)
		case "hard-error":
			next.Steering.Outcome = "hard_error"
		case "owner":
			f.approval.Plan.Units[0].Unit.Uid++
		case "symlink":
			path := f.approval.Plan.Units[0].Unit.ProgressFile
			if err := os.Rename(path, path+".other"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+".other", path); err != nil {
				t.Fatal(err)
			}
		}
		if fault != "symlink" && fault != "owner" {
			validatorActivationHealthTestWrite(t, f, 0, next)
		}
		if _, err := f.host.observeValidatorActivationWorker(t.Context(), f.approval.Plan, state, 0, &prior, f.now); err == nil {
			t.Fatal("contradictory progress accepted", fault)
		}
	}
}

// Shared file status text is not an integrity verdict. A second hard cause
// must keep a missing/timeout result from becoming a permissive health warning.
func TestValidatorActivationHealthUnavailableDoesNotHideOwnership(t *testing.T) {
	soft := &monitorServiceReadError{code: "unavailable", cause: &os.PathError{Op: "read", Path: "/synthetic/progress", Err: syscall.EIO}}
	hard := errors.New("synthetic owner mismatch")
	for _, err := range []error{&monitorServiceReadError{code: "unavailable", cause: hard}, errors.Join(soft, hard), &monitorServiceReadError{code: "changed", cause: syscall.EIO}} {
		if validatorActivationWorkerReadUnavailable(err) {
			t.Fatal("integrity failure was hidden by unavailable classification", err)
		}
	}
	if !validatorActivationWorkerReadUnavailable(soft) {
		t.Fatal("pure read unavailability became an integrity blocker")
	}
}

// These structurally valid checkpoints exercise the real journal's persistence
// and domain checks. They cannot implement the unavailable start capability.
func validatorActivationHealthTestCheckpoint(f *validatorActivationFixture, index int) validatorActivationProofCheckpoint {
	canonical := "0x" + strings.Repeat("4", 64)
	digest := "sha256:" + strings.Repeat("5", 64)
	proof := validator.ProductionBootstrapPrefixObservation{Schema: validator.ProductionBootstrapPrefixSchema, ConfigHash: f.approval.Plan.Units[index].Unit.Config.Sha256, PolicyHash: canonical, ClientDomainHash: digest,
		Native: validator.ProductionBootstrapNativePoint{Block: 1, Hash: [32]byte{1}, Hotkey: [32]byte{2}}, EvmBlock: 1, EvmHash: canonical, HistoricalSources: true}
	for _, id := range []uint64{2, 3} {
		proof.Prefixes = append(proof.Prefixes, validator.ProductionBootstrapOperatorPrefix{NoId: id, ActivationHash: canonical, HistoryHash: digest, Root: "0x" + strings.Repeat("0", 64), Generation: 1, PriorEmaHash: digest})
	}
	proof.ContentHash = rootObjectHash(proof)
	return validatorActivationProofCheckpoint{ObservedAt: f.now, Proof: proof}
}

// Reopening after a later observation refusal retains the original successful
// role exactly. This uses actual synced custody rather than an in-memory flag.
func TestValidatorActivationHealthCompletedCheckpointSurvivesLaterRefusal(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := validatorActivationHealthTestCheckpoint(f, 0)
	if err := retainValidatorActivationProofCheckpoint(t.Context(), store, &record, 0, checkpoint); err != nil {
		t.Fatal("pristine zero-root checkpoint refused", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	before := f.chain.journals(t)
	result, code, detail := f.command(t.Context(), "admit-health", nil)
	if code != 3 || result.Status != "source-refused" || !strings.Contains(detail, "complete approved contract profile") || len(result.ProofCheckpoints) != 2 || !reflect.DeepEqual(result.ProofCheckpoints[0], &checkpoint) || result.ProofCheckpoints[1] != nil {
		t.Fatal("later refusal erased a completed checkpoint", code, result, detail)
	}
	status, code, detail := f.command(t.Context(), "status", nil)
	if code != 0 || !reflect.DeepEqual(status.ProofCheckpoints, result.ProofCheckpoints) {
		t.Fatal("reopen lost checkpoint", code, detail)
	}
	closed, code, detail := f.command(t.Context(), "start", nil)
	if code != 3 || closed.Status != "activation-authority-unavailable" || closed.Operations != status.Operations || f.starts != [2]int{} || !reflect.DeepEqual(before, f.chain.journals(t)) {
		t.Fatal("checkpoint opened public start or changed bootstrap liability", code, detail)
	}
}

// A completed prefix cannot be overwritten with a new generation/root merely
// because its containing JSON has a freshly recomputed checksum.
func TestValidatorActivationHealthRejectsCheckpointReplacement(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.close(); err != nil {
			t.Error(err)
		}
	})
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := validatorActivationHealthTestCheckpoint(f, 0)
	if err := retainValidatorActivationProofCheckpoint(t.Context(), store, &record, 0, original); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"root", "generation", "history", "config", "clock", "source-flag", "current-flag"} {
		next := validatorActivationHealthTestCheckpoint(f, 0)
		switch fault {
		case "root":
			next.Proof.Prefixes[0].LastSequence = 1
			next.Proof.Prefixes[0].Root = "0x" + strings.Repeat("a", 64)
		case "generation":
			next.Proof.Prefixes[0].Generation++
		case "history":
			next.Proof.Prefixes[0].HistoryHash = "sha256:" + strings.Repeat("a", 64)
		case "config":
			next.Proof.ConfigHash = f.approval.Plan.Units[0].Source.ConfigHash
		case "clock":
			next.ObservedAt = next.ObservedAt.Add(-time.Second)
		case "source-flag":
			next.Proof.HistoricalSources = false
		case "current-flag":
			next.Proof.CurrentPrefixProven = true
		}
		next.Proof.ContentHash = ""
		next.Proof.ContentHash = rootObjectHash(next.Proof)
		if err := retainValidatorActivationProofCheckpoint(t.Context(), store, &record, 0, next); err == nil {
			t.Fatal("completed prefix replacement accepted", fault)
		}
	}
	retained, err := store.load(t.Context())
	if err != nil || !reflect.DeepEqual(retained.ProofCheckpoints[0], &original) {
		t.Fatal("refused checkpoint changed durable custody", err)
	}
}

// A canceled read joins before publication and preserves the last accepted
// heartbeat; cancellation cannot be downgraded into a soft missing-file warning.
func TestValidatorActivationHealthCancellationPreservesProgress(t *testing.T) {
	f := newValidatorActivationFixture(t)
	state, value := validatorActivationHealthTestProgress(t, f, 0)
	prior := validatorActivationWorkerHealth{ObservedAt: f.now, Record: &value, Current: true}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := f.host.observeValidatorActivationWorker(ctx, f.approval.Plan, state, 0, &prior, f.now)
	if !errors.Is(err, context.Canceled) || got.Current || !reflect.DeepEqual(got.Record, prior.Record) {
		t.Fatal("cancellation changed progress or became a warning", got, err)
	}
}

// Structural composition cannot inherit a good stake result and skip health
// validation. This assertion operates through the retained journal validator.
func TestValidatorActivationHealthCompositionRejectsForgedReadyProjection(t *testing.T) {
	f := newValidatorActivationStakeFixture(t, nil)
	f.activation.installed()
	result, code, detail := f.activation.command(t.Context(), "admit-stake", nil)
	if code != 0 || result.Readiness == nil {
		t.Fatal(code, detail)
	}
	result.Readiness.Health = &validatorActivationHealthReadiness{Schema: validatorActivationHealthSchema, OpenGates: validatorActivationHealthOpenGates()}
	result.Readiness.Health.ContentHash = rootObjectHash(*result.Readiness.Health)
	if err := result.Readiness.validate(f.activation.approval.Plan); err == nil {
		t.Fatal("valid native stake bypassed missing current operator/proof health")
	}
}

// A complete synthetic retained projection is accepted structurally, then
// individually rehashed substitutions prove that hashes alone grant no scope.
func validatorActivationHealthTestReadiness(t *testing.T) (*validatorActivationStakeFixture, validatorActivationReadiness) {
	t.Helper()
	f := newValidatorActivationStakeFixture(t, nil)
	f.activation.installed()
	result, code, detail := f.activation.command(t.Context(), "admit-stake", nil)
	if code != 0 || result.Readiness == nil {
		t.Fatal(code, detail)
	}
	readiness := *result.Readiness
	digest := "sha256:" + strings.Repeat("7", 64)
	canonical := "0x" + strings.Repeat("8", 64)
	production := &validatorActivationProductionReadiness{Schema: validatorActivationProductionSchema, ContractPlanHash: digest, MappingHash: digest, EvmBlock: 1, EvmHash: canonical}
	for i := byte(1); i <= 5; i++ {
		production.Contracts = append(production.Contracts, validatorActivationContractObservation{Address: common.Address{i}, RuntimeHash: canonical, GettersHash: digest, StorageHash: digest})
	}
	health := &validatorActivationHealthReadiness{Schema: validatorActivationHealthSchema, OpenGates: validatorActivationHealthOpenGates()}
	for i, unit := range f.activation.approval.Plan.Units {
		observed := validator.ProductionBootstrapObservation{ConfigHash: unit.Unit.Config.Sha256, DeploymentId: unit.Source.DeploymentId, ValidatorId: unit.Source.ValidatorId, EvmBlock: 1, EvmHash: canonical,
			Native: validator.ProductionBootstrapNativePoint{Block: readiness.FinalizedNumber, Hash: common.HexToHash(readiness.FinalizedHash), Epoch: readiness.Native.NativeEpoch, Hotkey: common.HexToHash(readiness.Roles[i].Expected.Hotkey)}}
		for _, id := range []uint64{2, 3} {
			observed.Operators = append(observed.Operators, validator.ProductionBootstrapOperatorObservation{NoId: id, ActivationHash: canonical, PublishedBlock: 1, ClientId: "synthetic-client", ClientKey: canonical, ClientKeyGeneration: 1, ClientKeyRegistrationHash: canonical, ClientKeyResponseHash: canonical, ObservationNonce: canonical, RootSigner: common.Address{1}.Hex()})
		}
		production.Validators = append(production.Validators, observed)
		checkpoint := validatorActivationHealthTestCheckpoint(f.activation, i)
		checkpoint.Proof.Native = observed.Native
		checkpoint.Proof.EvmHash = observed.EvmHash
		checkpoint.Proof.ClientDomainHash = rootObjectHash(observed.Operators)
		for j := range checkpoint.Proof.Prefixes {
			checkpoint.Proof.Prefixes[j].ActivationHash = canonical
		}
		checkpoint.Proof.ContentHash = ""
		checkpoint.Proof.ContentHash = rootObjectHash(checkpoint.Proof)
		health.Proofs[i] = checkpoint
		health.Workers[i] = validatorActivationWorkerHealth{ObservedAt: f.activation.now, Warnings: []string{"PER_OPERATOR_LIVE_WORKER_UNVERIFIED", "WORKER_NOT_STARTED"}}
	}
	production.EvidenceHash = rootObjectHash(*production)
	health.ContentHash = rootObjectHash(*health)
	readiness.Production, readiness.Health = production, health
	if err := readiness.validate(f.activation.approval.Plan); err != nil {
		t.Fatal("complete retained structure refused", err)
	}
	return f, readiness
}

// Every rehashed source substitution must still satisfy the exact current
// composition; this fixture confers no real deployment or start authority.
func TestValidatorActivationHealthProjectionPinsBothRolesAndOpenGates(t *testing.T) {
	f, readiness := validatorActivationHealthTestReadiness(t)
	digest := "sha256:" + strings.Repeat("7", 64)
	for _, fault := range []string{"operator", "activation", "native", "evm", "client-domain", "config-file", "current-prefix", "source-check", "open-gate", "worker-current", "worker-domain"} {
		raw, _ := json.Marshal(readiness)
		var changed validatorActivationReadiness
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		p := &changed.Health.Proofs[1].Proof
		switch fault {
		case "operator":
			p.Prefixes[1].NoId++
		case "activation":
			p.Prefixes[1].ActivationHash = "0x" + strings.Repeat("a", 64)
		case "native":
			p.Native.Hash[0] ^= 1
		case "evm":
			p.EvmBlock++
		case "client-domain":
			p.ClientDomainHash = digest
		case "config-file":
			p.ConfigHash = f.activation.approval.Plan.Units[1].Source.ConfigHash
		case "current-prefix":
			p.CurrentPrefixProven = true
		case "source-check":
			p.HistoricalSources = false
		case "open-gate":
			changed.Health.OpenGates = changed.Health.OpenGates[1:]
		case "worker-current":
			changed.Health.Workers[1].Current = true
		case "worker-domain":
			value := monitorServicesTestRecord(f.activation.now, 999)
			changed.Health.Workers[1].Record = &value
		}
		p.ContentHash = ""
		p.ContentHash = rootObjectHash(*p)
		changed.Health.ContentHash = ""
		changed.Health.ContentHash = rootObjectHash(*changed.Health)
		if err := changed.validate(f.activation.approval.Plan); err == nil {
			t.Fatal("rehashed health substitution admitted", fault)
		}
	}
}
