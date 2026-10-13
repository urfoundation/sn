// Synthetic current readers drive real two-unit custody and manager boundaries.
// Production readback is separately exercised; no fixture holds a live signer.
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
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// The fixture injects observations, never an on-disk report into production.
// Native/stake control observations use the real bounded synthetic RPC reader.
type validatorActivationCurrentFixture struct {
	f             *validatorActivationFixture
	authority     *validatorActivationCurrentAuthority
	private       ed25519.PrivateKey
	readiness     validatorActivationReadiness
	observations  int
	installations int
	observeFault  func(int, *validatorActivationReadiness) error
	installFault  func(int, *validatorActivationInstallationObservation) error
}

// Two fully imported empty tails are the finite initial-launch scope. Later
// unfinished work requires a distinct recovery policy and remains refused here.
func newValidatorActivationCurrentFixture(t *testing.T) *validatorActivationCurrentFixture {
	t.Helper()
	stake, readiness := validatorActivationHealthTestReadiness(t)
	f := stake.activation
	self := &validatorActivationCurrentFixture{f: f, readiness: readiness, private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0xa7}, ed25519.SeedSize))}
	health := readiness.Health
	health.OpenGates = validatorActivationCommittedOpenGates()
	health.Committed = &[2]validatorActivationCommittedCheckpoint{}
	for i, approved := range health.Proofs {
		checkpoint := validatorActivationCommittedTestCheckpoint(f, i, approved)
		inventory := &validator.ProductionBootstrapUnsealedObservation{Schema: validator.ProductionBootstrapUnsealedSchema, CensusHash: rootObjectHash("synthetic empty state census"), SourceCount: 5, SourceBytes: 32}
		for _, prefix := range checkpoint.Proof.Prefixes {
			inventory.Ledgers = append(inventory.Ledgers, validator.ProductionBootstrapUnsealedLedger{NoId: prefix.NoId, Head: validator.AttemptLedgerHead{Root: prefix.Root}, PendingHash: validator.ReleaseMeasurementContentHash(nil),
				TailBoundaryProof: &validator.ProductionBootstrapUnsealedBoundaryProof{Schema: validator.ProductionBootstrapUnsealedBoundarySchema, HistoricalSources: true, Boundaries: []validator.ProductionBootstrapUnsealedBoundary{}}})
		}
		checkpoint.Proof.Unsealed = inventory
		checkpoint.Proof.ContentHash = ""
		checkpoint.Proof.ContentHash = rootObjectHash(checkpoint.Proof)
		health.Committed[i] = checkpoint
	}
	health.ContentHash = ""
	health.ContentHash = rootObjectHash(*health)
	if err := readiness.validate(f.approval.Plan); err != nil {
		t.Fatal("synthetic complete domain control", err)
	}
	input := func(name string) planFileReference {
		path := filepath.Join(filepath.Dir(f.path), "synthetic-current-"+name)
		raw := []byte("synthetic independent " + name + "\n")
		repairValidatorTestWrite(t, path, raw, 0600)
		return planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
	}
	p := validatorActivationCurrentAuthorization{Schema: validatorActivationCurrentSchema, ActivationHash: rootObjectHash(f.approval), ActivationPublicKey: f.key, PreparationHash: f.approval.Plan.PlanHash,
		Installation: validatorActivationInstallationInputs{Request: input("request"), SafeRequest: input("safe-request"), ExecutionRequest: input("execution-request"), ExecutionApproval: input("execution-approval"), CanonicalApproval: input("canonical-approval"),
			ExecutionPlanHash: rootObjectHash("synthetic original execution"), InstallationHash: rootObjectHash("synthetic complete installation"), AcceptedSafePolicy: rootObjectHash("synthetic distinct accepted safe policy")},
		NativeHotkeys: [2]string{f.chain.config.Validators[0].Hotkey, f.chain.config.Validators[1].Hotkey}, CustodyEvidence: input("custody-evidence"), ValidFrom: f.approval.Plan.ValidFrom, ExpiresAt: f.approval.Plan.ExpiresAt,
		Policy: validatorActivationCurrentPolicy, CustodyPolicy: validatorActivationCurrentCustodyPolicy}
	self.authority = &validatorActivationCurrentAuthority{retained: validatorActivationCurrentRetained{Approval: validatorActivationCurrentApproval{Authorization: p}, PublicKey: "0x" + hex.EncodeToString(self.private.Public().(ed25519.PublicKey)), Reference: planFileReference{Path: filepath.Join(filepath.Dir(f.path), "synthetic-current-approval.json")}}}
	self.sign()
	self.authority.observe = self.observe
	self.authority.installation = self.installation
	self.authority.close = func() error { return nil }
	original := f.host.host.execute
	f.host.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		raw, err := original(ctx, path, args)
		if err == nil && len(args) >= 2 && args[len(args)-2] == "--" && strings.Contains(strings.Join(args, " "), " start ") {
			for i, unit := range f.approval.Plan.Units {
				if args[len(args)-1] == unit.Unit.Name && f.progress[i] {
					data, readErr := os.ReadFile(unit.Unit.ProgressFile)
					if readErr != nil {
						t.Fatal(readErr)
					}
					value, readErr := protocol.DecodeValidatorProgress(data)
					if readErr != nil {
						t.Fatal(readErr)
					}
					value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: f.now.Format(time.RFC3339Nano), Outcome: "read_wait"}
					validatorActivationHealthTestWrite(t, f, i, *value)
				}
			}
		}
		return raw, err
	}
	return self
}

// An exact file pin and independent signature are updated together only before
// the first admission; tests intentionally keep old bytes for replay refusals.
func (self *validatorActivationCurrentFixture) sign() {
	self.f.t.Helper()
	message, err := self.authority.retained.Approval.Authorization.signingBytes()
	if err != nil {
		self.f.t.Fatal(err)
	}
	self.authority.retained.Approval.Signature = hex.EncodeToString(ed25519.Sign(self.private, message))
	raw, err := json.Marshal(self.authority.retained.Approval)
	if err != nil {
		self.f.t.Fatal(err)
	}
	repairValidatorTestWrite(self.f.t, self.authority.retained.Reference.Path, raw, 0600)
	self.authority.retained.Reference.Sha256 = monitorReadDigest(raw)
}

// A typed synthetic observer preserves the same partial-checkpoint ownership
// as the production producer. It re-reads actual protected process progress.
func (self *validatorActivationCurrentFixture) observe(ctx context.Context, store *validatorActivationStore, host *validatorActivationHost, record *validatorActivationRecord, now func() time.Time, pending int) (*validatorActivationReadiness, error) {
	self.observations++
	raw, _ := json.Marshal(self.readiness)
	var result validatorActivationReadiness
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	result.ObservedAt = now()
	for i := range result.Health.Proofs {
		checkpoint := &result.Health.Proofs[i]
		checkpoint.ObservedAt = now()
		if err := retainValidatorActivationProofCheckpoint(ctx, store, record, i, *checkpoint); err != nil {
			return nil, err
		}
		current := &result.Health.Committed[i]
		current.ObservedAt = now()
		if len(record.CommittedCheckpoints) == 2 && record.CommittedCheckpoints[i] != nil {
			current.Proof.PreviousHash = record.CommittedCheckpoints[i].Proof.ContentHash
		}
		current.Proof.ContentHash = ""
		current.Proof.ContentHash = rootObjectHash(current.Proof)
		if err := retainValidatorActivationCommittedCheckpoint(ctx, store, record, i, *current); err != nil {
			return nil, err
		}
		worker, err := host.observeValidatorActivationWorker(ctx, store.approval.Plan, validatorActivationPreStartRecord(*record, pending).Units[i], i, nil, now())
		if err != nil {
			return nil, err
		}
		result.Health.Workers[i] = worker
	}
	result.Health.ContentHash = ""
	result.Health.ContentHash = rootObjectHash(*result.Health)
	if self.observeFault != nil {
		if err := self.observeFault(self.observations, &result); err != nil {
			return nil, err
		}
	}
	return &result, ctx.Err()
}

// The bounded seal is produced on each invocation. A prior seal is never read
// from disk, and mutations let tests force exactly which admission must refuse.
func (self *validatorActivationCurrentFixture) installation(ctx context.Context, readiness *validatorActivationReadiness) (validatorActivationInstallationObservation, error) {
	self.installations++
	result := validatorActivationInstallationObservation{InstallationHash: self.authority.retained.Approval.Authorization.Installation.InstallationHash,
		PreparationHash: readiness.PlanHash, ContractPlanHash: readiness.Production.ContractPlanHash, ObservationHash: rootObjectHash("synthetic fresh installation observation"),
		NativeNumber: readiness.FinalizedNumber, NativeHash: readiness.FinalizedHash, EvmNumber: readiness.Production.EvmBlock, EvmHash: readiness.Production.EvmHash,
		EarliestOriginalEvmBlock: 1, DeclaredScanFloors: [2]uint64{1, 1}}
	if self.installFault != nil {
		if err := self.installFault(self.installations, &result); err != nil {
			return result, err
		}
	}
	return result, ctx.Err()
}

// Exact current acceptance enables both bounded starts through real journals.
// Responsive read waits prove loop liveness, never economic/protocol success.
func TestValidatorActivationCurrentStartsBothExactUnitsOnce(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	before := f.f.chain.journals(t)
	result, code, detail := f.f.command(t.Context(), "start", f.authority)
	if code != 0 || result.Status != "processes-observed" || f.f.starts != [2]int{1, 1} || f.observations != 4 || f.installations != 4 || result.ActivationReady || result.RootServiceActive || result.ChainSuccessProven || !reflect.DeepEqual(before, f.f.chain.journals(t)) {
		t.Fatal("exact current policy did not produce two bounded attributable starts", code, detail, result.Status, f.f.starts, f.observations, f.installations)
	}
	for _, unit := range result.Units {
		if unit.CurrentAuthorityHash != rootObjectHash(f.authority.retained.Approval) || !planSha256(unit.RecheckedReadinessHash) || unit.Completed == nil {
			t.Fatal("start lost exact current admission", unit)
		}
	}
	// The original approval can reconcile both acknowledged processes without
	// a current acceptance or renewed effect window; no second start is issued.
	f.f.now = f.f.approval.Plan.ExpiresAt.Add(time.Second)
	for i := range f.f.approval.Plan.Units {
		f.f.publish(i)
		data, _ := os.ReadFile(f.f.approval.Plan.Units[i].Unit.ProgressFile)
		value, err := protocol.DecodeValidatorProgress(data)
		if err != nil {
			t.Fatal(err)
		}
		value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: f.f.now.Format(time.RFC3339Nano), Outcome: "epoch_wait"}
		validatorActivationHealthTestWrite(t, f.f, i, *value)
	}
	result, code, detail = f.f.command(t.Context(), "resume", nil)
	if code != 0 || result.Status != "processes-observed" || f.f.starts != [2]int{1, 1} {
		t.Fatal("reconciliation renewed or lost exact starts", code, detail, f.f.starts)
	}
}

// Valid signatures cannot silently replace the exact policy, original scope,
// or independent key. Process signatures have a different signing domain.
func TestValidatorActivationCurrentRequiresIndependentPolicyAndCustody(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	original := f.authority.retained
	if err := original.Approval.validate(f.f.approval, f.f.key, original.PublicKey, f.f.chain.preparation); err != nil {
		t.Fatal("independent positive control", err)
	}
	for _, fault := range []string{"policy", "custody", "activation", "process-key", "hotkey", "prep", "expiry", "key", "domain", "reference"} {
		value := original.Approval
		key := original.PublicKey
		switch fault {
		case "policy":
			value.Authorization.Policy += ";waive-health"
		case "custody":
			value.Authorization.CustodyPolicy = "heartbeat-is-enough"
		case "activation":
			value.Authorization.ActivationHash = rootObjectHash("synthetic replacement")
		case "process-key":
			value.Authorization.ActivationPublicKey = key
		case "hotkey":
			value.Authorization.NativeHotkeys[1] = "0x" + strings.Repeat("a", 64)
		case "prep":
			value.Authorization.PreparationHash = rootObjectHash("synthetic other preparation")
		case "expiry":
			value.Authorization.ExpiresAt = f.f.approval.Plan.ExpiresAt.Add(time.Second)
		case "key":
			key = f.f.key
		case "domain":
			value.Signature = f.f.approval.Signature
		case "reference":
			value.Authorization.CustodyEvidence = value.Authorization.Installation.Request
		}
		if fault != "domain" {
			message, _ := value.Authorization.signingBytes()
			value.Signature = hex.EncodeToString(ed25519.Sign(f.private, message))
		}
		if err := value.validate(f.f.approval, f.f.key, key, f.f.chain.preparation); err == nil {
			t.Fatal("signed substitution acquired current authority", fault)
		}
	}
}

// An earlier current admission supplies no later start authority. Actual
// installation is re-read and an absent/changed/mixed snapshot refuses both.
func TestValidatorActivationCurrentReobservesAnchorBeforeEveryStart(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	if _, code, detail := f.f.command(t.Context(), "admit-current", f.authority); code != 0 {
		t.Fatal("read-only current control", code, detail)
	}
	for _, fault := range []string{"missing", "partial", "anchor", "native", "evm", "contract", "floor"} {
		before := f.installations
		f.installFault = func(_ int, observation *validatorActivationInstallationObservation) error {
			switch fault {
			case "missing":
				return errors.New("synthetic original anchor absent")
			case "partial":
				observation.ObservationHash = ""
			case "anchor":
				observation.InstallationHash = rootObjectHash("synthetic other anchor")
			case "native":
				observation.NativeNumber--
			case "evm":
				observation.EvmHash = "0x" + strings.Repeat("a", 64)
			case "contract":
				observation.ContractPlanHash = rootObjectHash("synthetic other contract")
			case "floor":
				observation.DeclaredScanFloors[1] = observation.EarliestOriginalEvmBlock + 1
			}
			return nil
		}
		result, code, detail := f.f.command(t.Context(), "start", f.authority)
		if code != 3 || result.Status != "authority-refused" || f.f.starts != [2]int{} || !result.Units[0].StartAt.IsZero() || f.installations != before+1 {
			t.Fatal("prior admission bypassed a fresh complete anchor", fault, code, detail, result.Status)
		}
	}
}

// A failure after synced consumption preserves the claim and cannot issue a
// start on reopen. Barriers target the actual durable publication, not sleeps.
func TestValidatorActivationCurrentPostSyncRefusalNeverStarts(t *testing.T) {
	for _, fault := range []string{"reference", "expiry", "rollback", "claim", "journal", "journal-tamper", "cancel", "anchor", "operator"} {
		f := newValidatorActivationCurrentFixture(t)
		store, err := openValidatorActivationStore(f.f.chain.storageContext(t.Context()), f.f.approval, f.f.key, false, f.f.now)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(f.f.chain.storageContext(t.Context()))
		fired := false
		store.syncDirectory = func(directory *os.File) error {
			if err := directory.Sync(); err != nil {
				return err
			}
			raw, err := os.ReadFile(store.path)
			var record validatorActivationRecord
			if err == nil {
				err = json.Unmarshal(raw, &record)
			}
			if err != nil || fired || record.Units[0].StartAt.IsZero() {
				return err
			}
			fired = true
			switch fault {
			case "reference":
				return os.WriteFile(f.authority.retained.Approval.Authorization.CustodyEvidence.Path, []byte("synthetic changed custody\n"), 0600)
			case "expiry":
				f.f.now = f.authority.retained.Approval.Authorization.ExpiresAt
			case "rollback":
				f.f.now = f.f.now.Add(-time.Second)
			case "claim":
				return os.Remove(validatorActivationFirstStartPath(f.f.approval.Plan.Units[0].Unit))
			case "journal":
				return os.Remove(store.path)
			case "journal-tamper":
				record.Operations++
				record.ContentHash = ""
				record.ContentHash = rootObjectHash(record)
				raw, _ := json.Marshal(record)
				return os.WriteFile(store.path, raw, 0600)
			case "cancel":
				cancel()
			case "anchor":
				f.installFault = func(_ int, value *validatorActivationInstallationObservation) error {
					value.NativeHash = "0x" + strings.Repeat("b", 64)
					return nil
				}
			case "operator":
				f.observeFault = func(_ int, value *validatorActivationReadiness) error {
					value.Health.Proofs[1].Proof.Prefixes[1].ActivationHash = "0x" + strings.Repeat("c", 64)
					return nil
				}
			}
			return nil
		}
		result, err := advanceValidatorActivation(ctx, store, f.f.host, f.authority, "start", func() time.Time { return f.f.now })
		cancel()
		if err == nil || !fired || result.Units[0].StartAt.IsZero() || f.f.starts != [2]int{} {
			t.Fatal("post-sync refusal issued an effect or refunded consumption", fault, err, fired, result.Status, f.f.starts)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		if _, code, detail := f.f.command(t.Context(), "start", f.authority); code != 3 || f.f.starts != [2]int{} {
			t.Fatal("restart renewed a consumed refused start", fault, code, detail)
		}
		if fault == "journal" {
			if _, err := os.Lstat(f.f.approval.Plan.StatePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refusal recreated missing counted journal", err)
			}
		}
	}
}

// Role one may finish before role two loses authority. Reopening observes the
// same invocation and requires an entirely fresh second-role admission.
func TestValidatorActivationCurrentPartialPairRetainsExactCustody(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	f.installFault = func(call int, _ *validatorActivationInstallationObservation) error {
		if call == 3 {
			return errors.New("synthetic second-role anchor unavailable")
		}
		return nil
	}
	result, code, detail := f.f.command(t.Context(), "start", f.authority)
	if code != 3 || f.f.starts != [2]int{1, 0} || result.Units[0].Completed == nil || !result.Units[1].StartAt.IsZero() {
		t.Fatal("partial pair lost its exact first start", code, detail, f.f.starts)
	}
	generation := *result.Units[0].Generation
	f.installFault = nil
	result, code, detail = f.f.command(t.Context(), "start", f.authority)
	if code != 0 || f.f.starts != [2]int{1, 1} || *result.Units[0].Generation != generation || result.Status != "processes-observed" {
		t.Fatal("partial restart changed first role or failed fresh second admission", code, detail, f.f.starts)
	}
}

// A lost manager reply leaves one consumed, unattributable start. Matching
// process bytes after restart cannot manufacture its missing acknowledgement.
func TestValidatorActivationCurrentLostStartAcknowledgementCannotReplay(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	f.f.startError = errors.New("synthetic start reply lost")
	result, code, detail := f.f.command(t.Context(), "start", f.authority)
	if code != 3 || f.f.starts != [2]int{1, 0} || result.Units[0].StartAt.IsZero() || result.Units[0].Generation != nil || result.Units[0].Status != "uncertain-consumed-start" {
		t.Fatal("lost reply dropped lifetime consumption", code, detail, f.f.starts)
	}
	f.f.startError = nil
	for _, operation := range []string{"start", "resume"} {
		if _, code, detail := f.f.command(t.Context(), operation, f.authority); code != 3 || f.f.starts != [2]int{1, 0} {
			t.Fatal("lost acknowledgement became a replay", operation, code, detail)
		}
	}
}

// Even separately valid approvals and new journal paths cannot consume the
// same unit's initial allowance again. Either single missing marker also refuses.
func TestValidatorActivationCurrentFirstClaimExcludesAlternateEnvelopes(t *testing.T) {
	for _, removed := range []string{"", ".claim", ".lock"} {
		f := newValidatorActivationCurrentFixture(t)
		store, err := openValidatorActivationStore(f.f.chain.storageContext(t.Context()), f.f.approval, f.f.key, false, f.f.now)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		record.CurrentAuthority = &f.authority.retained
		control, err := f.f.host.host.control(f.f.chain.storageContext(t.Context()), f.f.approval.Plan.Units[0].Unit)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.f.host.firstStartClaim(f.f.chain.storageContext(t.Context()), record, 0, true); err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(control.close(), store.close()); err != nil {
			t.Fatal(err)
		}
		path := validatorActivationFirstStartPath(f.f.approval.Plan.Units[0].Unit)
		if removed != "" {
			name := path
			if removed == ".lock" {
				name += ".lock"
			}
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
		}
		// A different signed path cannot hide the fixed per-unit lifetime claim.
		f.f.approval.Plan.StatePath += ".other"
		f.f.sign()
		f.authority.retained.Approval.Authorization.ActivationHash = rootObjectHash(f.f.approval)
		f.sign()
		// Only this newly approved journal is freshly prepared; the original
		// per-unit lifetime claims remain unchanged and must still refuse.
		prepareMainnetSnapshotTest(t, f.f.approval.Plan.StatePath, "mainnet-validator-activation", 128*1024)
		f.f.installed()
		result, code, detail := f.f.command(t.Context(), "start", f.authority)
		if code != 3 || result.Status != "source-refused" || f.f.starts != [2]int{} || !result.Units[0].StartAt.IsZero() {
			t.Fatal("alternate independent envelope renewed initial start", removed, code, detail, result.Status)
		}
	}
}

// A valid outer signature does not waive unknown tails, active stake or either
// operator's authenticated history. Rehashing tests the actual semantic gate.
func TestValidatorActivationCurrentRejectsPartialDomains(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	for _, fault := range []string{"stake", "low-capacity", "health", "committed", "inventory", "boundary", "tail", "intent", "second-operator", "worker", "unavailable-worker"} {
		f.observeFault = func(_ int, value *validatorActivationReadiness) error {
			switch fault {
			case "stake":
				value.Stake = nil
			case "low-capacity":
				value.Stake.Roles[0].CapacityShareLowerQ32 = validatorStakeQ32One / 2
				value.Stake.ContentHash = ""
				value.Stake.ContentHash = rootObjectHash(*value.Stake)
			case "health":
				value.Health = nil
			case "committed":
				value.Health.Committed = nil
				value.Health.OpenGates = validatorActivationHealthOpenGates()
			case "inventory":
				value.Health.Committed[1].Proof.Unsealed = nil
			case "boundary":
				for i := range value.Health.Committed[1].Proof.Unsealed.Ledgers {
					value.Health.Committed[1].Proof.Unsealed.Ledgers[i].TailBoundaryProof = nil
				}
			case "tail":
				ledger := &value.Health.Committed[1].Proof.Unsealed.Ledgers[1]
				ledger.Head = validator.AttemptLedgerHead{LastSequence: 1, Root: "0x" + strings.Repeat("e", 64), RecordBytes: 100, TrailCount: 1}
				ledger.UnsealedRecords = 1
				ledger.TailBoundaryProof.Boundaries = []validator.ProductionBootstrapUnsealedBoundary{{Boundary: validator.AttemptBoundary{EVMBlock: value.Production.EvmBlock, EVMBlockHash: value.Production.EvmHash}, Records: 1}}
			case "intent":
				return errors.New("synthetic nonempty intent graph unsupported")
			case "second-operator":
				value.Health.Committed[1].Proof.Prefixes[1].NoId++
			case "worker":
				value.Health.Workers[1].Current = true
			case "unavailable-worker":
				return os.WriteFile(f.f.approval.Plan.Units[1].Unit.ProgressFile, []byte("synthetic unreadable prior progress"), 0600)
			}
			if value.Health != nil {
				if value.Health.Committed != nil {
					for i := range value.Health.Committed {
						proof := &value.Health.Committed[i].Proof
						proof.ContentHash = ""
						proof.ContentHash = rootObjectHash(*proof)
					}
				}
				value.Health.ContentHash = ""
				value.Health.ContentHash = rootObjectHash(*value.Health)
			}
			return nil
		}
		result, code, detail := f.f.command(t.Context(), "start", f.authority)
		if code != 3 || f.f.starts != [2]int{} || !result.Units[0].StartAt.IsZero() || f.installations != 0 {
			t.Fatal("partial current domain reached initial start", fault, code, detail)
		}
	}
}

// Only a same-operation freshly consumed reservation may borrow the stopped
// view. Uncertain state and public readmission cannot erase its lifetime action.
func TestValidatorActivationCurrentPendingViewRejectsUncertainty(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	if _, code, detail := f.f.command(t.Context(), "admit-current", f.authority); code != 0 {
		t.Fatal(code, detail)
	}
	store, err := openValidatorActivationStore(f.f.chain.storageContext(t.Context()), f.f.approval, f.f.key, false, f.f.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	record.Status = "operation-reserved"
	unit := &record.Units[0]
	unit.StartAt, unit.StartMonotonicUsec, unit.Readiness, unit.Status = f.f.now, 150, record.Readiness, "start-consumed"
	unit.CurrentAuthorityHash = rootObjectHash(f.authority.retained.Approval)
	originalBytes, _ := json.Marshal(record)
	if !validatorActivationCurrentPending(record, 0) || !validatorActivationPreStartRecord(record, 0).Units[0].StartAt.IsZero() || record.Units[0].StartAt.IsZero() {
		t.Fatal("same synchronous reservation did not preserve original consumed state")
	}
	unchangedBytes, _ := json.Marshal(record)
	if !bytes.Equal(originalBytes, unchangedBytes) {
		t.Fatal("temporary stopped view mutated original consumed record, authority or readiness")
	}
	if err := validatorActivationCurrentDomains(f.f.approval.Plan, record, *record.Readiness, -1); err == nil {
		t.Fatal("ordinary public observation treated consumed start as fresh")
	}
	for _, fault := range []string{"uncertain", "partial", "rechecked", "other-authority", "no-evidence", "no-monotonic", "other-role"} {
		changed := record
		pending := 0
		switch fault {
		case "uncertain":
			changed.Units[0].Status = "uncertain-consumed-start"
		case "partial":
			changed.Status = "partial"
		case "rechecked":
			changed.Units[0].RecheckedReadinessHash = rootObjectHash("synthetic already rechecked")
		case "other-authority":
			changed.Units[0].CurrentAuthorityHash = rootObjectHash("synthetic other authority")
		case "no-evidence":
			changed.Units[0].Readiness = nil
		case "no-monotonic":
			changed.Units[0].StartMonotonicUsec = 0
		case "other-role":
			pending = 1
		}
		if validatorActivationCurrentPending(changed, pending) || validatorActivationPreStartRecord(changed, pending).Units[0].StartAt.IsZero() {
			t.Fatal("uncertain or foreign reservation borrowed a fresh stopped view", fault)
		}
		if _, err := f.authority.admit(t.Context(), store, f.f.host, &changed, func() time.Time { return f.f.now }, pending); err == nil {
			t.Fatal("uncertain reservation reached readmission", fault)
		}
	}
}

// Retained complete observations cannot select public current authority. The
// exact file/key opt-in is required on each invocation and never grants writes
// when original execution inputs are absent, even with valid policy signatures.
func TestValidatorActivationCurrentPublicOptInDoesNotTrustReports(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	ready, code, detail := f.f.command(t.Context(), "admit-current", f.authority)
	if code != 0 {
		t.Fatal(code, detail)
	}
	refused, code, detail := f.f.command(t.Context(), "start", nil)
	if code != 3 || refused.Status != "activation-authority-unavailable" || refused.Operations != ready.Operations || f.f.starts != [2]int{} {
		t.Fatal("retained current report supplied public authority", code, detail)
	}
	raw, err := os.ReadFile(f.f.path)
	if err != nil {
		t.Fatal(err)
	}
	base := []string{"start", "--approval", f.f.path, "--accept-approval-hash", monitorReadDigest(raw), "--independent-public-key", f.f.key, "--execute-approved-starts"}
	for _, args := range [][]string{
		{"--current-approval", f.authority.retained.Reference.Path},
		{"--current-approval", f.authority.retained.Reference.Path, "--accept-current-approval-hash", f.authority.retained.Reference.Sha256},
		{"--current-approval", f.authority.retained.Reference.Path, "--accept-current-approval-hash", rootObjectHash("synthetic wrong file"), "--current-independent-public-key", f.authority.retained.PublicKey},
		{"--current-approval", f.authority.retained.Reference.Path, "--accept-current-approval-hash", f.authority.retained.Reference.Sha256, "--current-independent-public-key", f.f.key},
		{"--current-approval", f.authority.retained.Reference.Path, "--accept-current-approval-hash", f.authority.retained.Reference.Sha256, "--current-independent-public-key", f.authority.retained.PublicKey},
	} {
		var stdout, stderr bytes.Buffer
		code := runValidatorActivationCommandWithHost(t.Context(), append(append([]string(nil), base...), args...), &stdout, &stderr, func() time.Time { return f.f.now }, f.f.host, nil)
		if code == 0 || f.f.starts != [2]int{} {
			t.Fatal("public policy bypassed absent original installation", code, stderr.String())
		}
	}
	retained, code, detail := f.f.command(t.Context(), "status", nil)
	if code != 0 || retained.Operations != ready.Operations || !retained.Units[0].StartAt.IsZero() {
		t.Fatal("public source refusal spent initial allowance", code, detail)
	}
}

// An unavailable read joins cancellation before returning; a stale previously
// admitted current snapshot cannot authorize either start during the outage.
func TestValidatorActivationCurrentCancellationJoinsObservation(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	if _, code, detail := f.f.command(t.Context(), "admit-current", f.authority); code != 0 {
		t.Fatal(code, detail)
	}
	entered, joined := make(chan struct{}), make(chan struct{})
	f.authority.observe = func(ctx context.Context, _ *validatorActivationStore, _ *validatorActivationHost, _ *validatorActivationRecord, _ func() time.Time, _ int) (*validatorActivationReadiness, error) {
		defer close(joined)
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan int, 1)
	go func() {
		_, code, _ := f.f.command(ctx, "start", f.authority)
		done <- code
	}()
	<-entered
	cancel()
	if code := <-done; code != 3 || f.f.starts != [2]int{} {
		t.Fatal("canceled current read issued a start", code)
	}
	select {
	case <-joined:
	default:
		t.Fatal("cancellation returned before the read joined")
	}
	retained, code, detail := f.f.command(t.Context(), "status", nil)
	if code != 0 || retained.Readiness == nil || retained.Readiness.Current == nil || !retained.Units[0].StartAt.IsZero() {
		t.Fatal("outage erased completed observation or consumed a start", code, detail)
	}
}

// Rehashing cannot detach current readiness from its observation or erase the
// signed current authority. Ordinary read-only journals stay backward compatible.
func TestValidatorActivationCurrentJournalRejectsNarrowedEvidence(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	if _, code, detail := f.f.command(t.Context(), "admit-current", f.authority); code != 0 {
		t.Fatal(code, detail)
	}
	store, err := openValidatorActivationStore(f.f.chain.storageContext(t.Context()), f.f.approval, f.f.key, false, f.f.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	original, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"authority", "domain", "boundary", "installation", "key"} {
		raw, _ := json.Marshal(original)
		var value validatorActivationRecord
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "authority":
			value.CurrentAuthority = nil
		case "domain":
			value.Readiness.Current.DomainHash = rootObjectHash("synthetic another observation")
		case "boundary":
			proof := &value.Readiness.Health.Committed[1].Proof
			for i := range proof.Unsealed.Ledgers {
				proof.Unsealed.Ledgers[i].TailBoundaryProof = nil
			}
			proof.ContentHash = ""
			proof.ContentHash = rootObjectHash(*proof)
			value.Readiness.Health.ContentHash = ""
			value.Readiness.Health.ContentHash = rootObjectHash(*value.Readiness.Health)
			unsigned := *value.Readiness
			unsigned.Current = nil
			value.Readiness.Current.DomainHash = rootObjectHash(unsigned)
		case "installation":
			value.Readiness.Current.Installation.InstallationHash = rootObjectHash("synthetic changed inclusion")
		case "key":
			value.CurrentAuthority.PublicKey = value.PublicKey
		}
		value.Readiness.Current.ContentHash = ""
		value.Readiness.Current.ContentHash = rootObjectHash(*value.Readiness.Current)
		value.ContentHash = ""
		value.ContentHash = rootObjectHash(value)
		if err := value.validate(f.f.approval, f.f.key); err == nil {
			t.Fatal("rehashed current authority narrowing was accepted", fault)
		}
	}
}
