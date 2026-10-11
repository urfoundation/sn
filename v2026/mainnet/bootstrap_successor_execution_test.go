// Deterministic causal controls cover independent authority, irreversible local
// accounting and both nonce domains. Canonical responses here are synthetic.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Both signature inputs must reach the real private-file reader independently
// of ambient umask. A private file in a shared directory still fails admission.
func TestBootstrapSuccessorExecutionFixturesUsePrivateInputDirectories(t *testing.T) {
	for _, name := range []string{"synthetic-safe-signatures.bin", "synthetic-relayer-transaction.bin"} {
		raw := []byte("synthetic pinned binary input " + name)
		reference := bootstrapSuccessorExecutionTestRaw(t, name, raw)
		retained, digest, err := readBootstrapRootFile(t.Context(), reference.Path, 1024)
		if err != nil || !bytes.Equal(retained, raw) || digest != reference.Sha256 {
			t.Fatal("binary fixture did not reach exact private input admission", name, err)
		}
		if err := os.Chmod(filepath.Dir(reference.Path), 0755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readBootstrapRootFile(t.Context(), reference.Path, 1024); err == nil || !strings.Contains(err.Error(), "directory is not owner-private") {
			t.Fatal("private binary file bypassed its shared parent directory", name, err)
		}
	}
}

// Neither original nor preparation signatures can substitute for this domain;
// the expected original key and reconstructed plan remain independent inputs.
func TestBootstrapSuccessorExecutionRequiresIndependentExactApproval(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	message, err := f.approval.Plan.signingBytes(f.profile)
	if err != nil || !bytes.HasPrefix(message, []byte(bootstrapSuccessorExecutionApprovalSchema+"\x00")) {
		t.Fatal("execution approval domain absent", err)
	}
	for _, change := range []func(*bootstrapSuccessorExecutionApproval){
		func(a *bootstrapSuccessorExecutionApproval) { a.Signature = "" },
		func(a *bootstrapSuccessorExecutionApproval) {
			a.Signature = hex.EncodeToString(ed25519.Sign(f.key, append([]byte(bootstrapSuccessorPreparationApprovalSchema+"\x00"), message[len(bootstrapSuccessorExecutionApprovalSchema)+1:]...)))
		},
		func(a *bootstrapSuccessorExecutionApproval) { a.Plan.Registry.Inode++ },
		func(a *bootstrapSuccessorExecutionApproval) {
			a.Plan.Review.Preparation.Approval.Plan.Proposal.AdoptedActions[0].Receipt.GasUsed++
		},
		func(a *bootstrapSuccessorExecutionApproval) {
			a.Plan.Review.Preparation.Approval.Plan.Proposal.Budget.RetainedAttempts--
		},
		func(a *bootstrapSuccessorExecutionApproval) { a.Plan.Request.Owners[0] = common.Address{} },
	} {
		changed := bootstrapSuccessorExecutionTestCopy(t, f.approval)
		change(&changed)
		if err := changed.validate(f.approval.Plan, f.profile); err == nil {
			t.Fatal("execution admitted changed domain, key, scope or original custody")
		}
	}
	if err := f.approval.validate(f.approval.Plan, f.profile); err != nil {
		t.Fatal(err)
	}
}

// Signature parsing and envelope matching are exercised with independently
// computed proxy digests; report hashes alone cannot authorize changed bytes.
func TestBootstrapSuccessorExecutionBindsSafeSignaturesAndOuterEnvelope(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	for _, change := range []func(*bootstrapSuccessorExecutionPlan){
		func(p *bootstrapSuccessorExecutionPlan) { p.SafeSignatures = "0x" + strings.Repeat("00", 130) },
		func(p *bootstrapSuccessorExecutionPlan) {
			raw := f.oracle.signatures(p.transaction(), "eth-sign", "raw")
			p.SafeSignatures = "0x" + hex.EncodeToString(raw)
			p.Request.SafeSignatures.Sha256 = safeReleaseHash(raw)
		},
		func(p *bootstrapSuccessorExecutionPlan) { p.SafeSignatures += "00" },
		func(p *bootstrapSuccessorExecutionPlan) { p.SignedRelayer += "00" },
		func(p *bootstrapSuccessorExecutionPlan) { p.Review.Relayer.Nonce++ },
		func(p *bootstrapSuccessorExecutionPlan) { p.Review.Relayer.Gas++ },
		func(p *bootstrapSuccessorExecutionPlan) { p.Review.Relayer.ValueWei = "1" },
		func(p *bootstrapSuccessorExecutionPlan) { p.Review.Transaction.Nonce = "42" },
		func(p *bootstrapSuccessorExecutionPlan) { p.Review.Transaction.Operation = 1 },
		func(p *bootstrapSuccessorExecutionPlan) { p.Review.Transaction.GasPrice = "1" },
	} {
		changed := bootstrapSuccessorExecutionTestCopy(t, f.approval.Plan)
		change(&changed)
		changed.Review.ContentHash = ""
		changed.Review.ContentHash = rootObjectHash(changed.Review)
		changed.Request.SafeReviewHash = changed.Review.ContentHash
		if err := changed.validate(f.profile); err == nil {
			t.Fatal("execution changed exact inner signature or outer envelope")
		}
	}
	if f.approval.Plan.Review.Transaction.Digest != f.oracle.oracleDigest(f.approval.Plan.transaction()) {
		t.Fatal("execution signature digest differs from published Safe")
	}
}

// Original attempts and both financial floors survive claim, repeated resume
// and mutable caller input. Offline status never infers live installation.
func TestBootstrapSuccessorExecutionAdoptsExactFloorsWithoutSending(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	directory := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	original := bootstrapSuccessorPreparationTestFiles(t, directory)
	owner := f.open(true, nil)
	if owner.last.CumulativeAttempts != 8 || owner.last.ReservedLifetimeWei != "1200000" || owner.last.Phase != "adopted" || owner.result().InstallationComplete || owner.result().CanonicalAdoptionVerified {
		t.Fatal("execution claim reset original floors or claimed live authority")
	}
	for name, raw := range original {
		actual, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(actual) != raw {
			t.Fatal("execution claim changed original prepared bytes", err)
		}
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, directory)
	registry := bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)
	for range 2 {
		owner = f.open(false, nil)
		if owner.last.CumulativeAttempts != 8 || owner.last.ReservedLifetimeWei != "1200000" {
			t.Fatal("resume reset cumulative custody")
		}
		owner.close()
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) || !maps.Equal(registry, bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)) {
		t.Fatal("complete idempotent resume changed retained custody")
	}
}

// Every genuine publication boundary is forced; another approval cannot claim
// the interrupted root, and the same approval finishes both persistent fences.
func TestBootstrapSuccessorExecutionRecoversInterruptedCreation(t *testing.T) {
	for _, target := range []string{"claim", "safe", "relayer", "adoption-intent", "adoption", "ready"} {
		for _, boundary := range []string{"name-synced", "stage-written", "stage-synced", "published", "published-synced"} {
			f := newBootstrapSuccessorExecutionFixture(t)
			name := bootstrapSuccessorExecutionPrefix + ".claim"
			switch target {
			case "safe":
				name = f.approval.Plan.nonceNames()[0]
			case "relayer":
				name = f.approval.Plan.nonceNames()[1]
			case "adoption-intent":
				name = bootstrapSuccessorExecutionEventName(0) + ".intent"
			case "adoption":
				name = bootstrapSuccessorExecutionEventName(0) + ".json"
			case "ready":
				name = bootstrapSuccessorExecutionPrefix + ".ready"
			}
			interrupted := errors.New("synthetic interrupted claim")
			reached := false
			owner, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, true, func(stage string) error {
				if stage == name+":"+boundary {
					reached = true
					return interrupted
				}
				return nil
			})
			if owner != nil {
				owner.close()
			}
			if !reached || !errors.Is(err, interrupted) {
				t.Fatalf("creation interruption absent at %s/%s: %v", target, boundary, err)
			}
			owner, err = openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, true, nil)
			if owner != nil {
				owner.close()
			}
			if err == nil {
				t.Fatal("fresh owner renewed an interrupted claim")
			}
			owner = f.open(false, nil)
			if owner.last.CumulativeAttempts != 8 || owner.last.ReservedLifetimeWei != "1200000" {
				t.Fatal("interrupted claim changed floors")
			}
			owner.close()
		}
	}
}

// A lost intent or final-record write cannot create uncounted sends. Recovery
// treats even an empty persisted attempt stage as consumed conservative capacity.
func TestBootstrapSuccessorExecutionRecoversCountedAttemptInterruptions(t *testing.T) {
	for _, suffix := range []string{".intent", ".json"} {
		for _, boundary := range []string{"name-synced", "stage-written", "stage-synced", "published", "published-synced"} {
			f := newBootstrapSuccessorExecutionFixture(t)
			owner := f.open(true, nil)
			interrupted := errors.New("synthetic interrupted reservation")
			reached := false
			owner.local.hook = func(stage string) error {
				if stage == bootstrapSuccessorExecutionEventName(1)+suffix+":"+boundary {
					reached = true
					return interrupted
				}
				return nil
			}
			_, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
			if !reached || !errors.Is(err, interrupted) || len(f.writes) != 0 {
				t.Fatalf("reservation did not fence sending at %s/%s: %v", suffix, boundary, err)
			}
			owner = f.open(false, nil)
			if owner.last.CumulativeAttempts != 9 || owner.last.Phase != "attempt-reserved" || owner.last.ReservedLifetimeWei != "1200000" {
				t.Fatalf("partial reservation reset capacity at %s/%s", suffix, boundary)
			}
			owner.close()
		}
	}
}

// Exact canonical readback finishes partial outcome intents. Missing readback
// cannot replace the partial outcome with another send reservation.
func TestBootstrapSuccessorExecutionRecoversInterruptedCanonicalOutcome(t *testing.T) {
	for _, boundary := range []string{"name-synced", "stage-written", "published", "published-synced"} {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
			t.Fatal(err)
		}
		f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
		interrupted := errors.New("synthetic interrupted completion")
		owner.local.hook = func(stage string) error {
			if stage == bootstrapSuccessorExecutionEventName(2)+".intent:"+boundary {
				return interrupted
			}
			return nil
		}
		if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); !errors.Is(err, interrupted) {
			t.Fatal("outcome interruption absent", err)
		}
		owner = f.open(false, nil)
		result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
		if err != nil || !result.InstallationComplete || result.ActivationReady || owner.last.CumulativeAttempts != 9 || len(f.writes) != 1 {
			t.Fatal("canonical outcome recovery resent or lost completion", boundary, err)
		}
		owner.close()
	}
}

// No supplied partial/changed original seal list authorizes an observation or
// send, even when every later synthetic adapter response would be successful.
func TestBootstrapSuccessorExecutionRequiresCanonicalEightReceiptAdoption(t *testing.T) {
	for _, fault := range []string{"unavailable", "missing", "changed", "reordered"} {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		switch fault {
		case "unavailable":
			f.authErr = errors.New("synthetic historical receipt unavailable")
		case "missing":
			f.changeSeals = func(seals []string) []string { return seals[:7] }
		case "changed":
			f.changeSeals = func(seals []string) []string { seals[7] = rootObjectHash("different record"); return seals }
		case "reordered":
			f.changeSeals = func(seals []string) []string { seals[0], seals[1] = seals[1], seals[0]; return seals }
		}
		result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
		if err == nil || result.CanonicalAdoptionVerified || len(f.writes) != 0 || f.observations != 0 {
			t.Fatal("owner skipped exact historical adoption", fault, err)
		}
	}
}

// Every selected live account constraint fails before a reservation. Distinct
// inner and outer nonce controls catch accidental conflation of their domains.
func TestBootstrapSuccessorExecutionFencesCurrentAuthorityAndBothNonces(t *testing.T) {
	changes := []func(*bootstrapSuccessorExecutionObservation){
		func(o *bootstrapSuccessorExecutionObservation) { o.SafeNonce = "42" },
		func(o *bootstrapSuccessorExecutionObservation) { o.RelayerNonce = 17 },
		func(o *bootstrapSuccessorExecutionObservation) { o.RelayerPendingNonce++ },
		func(o *bootstrapSuccessorExecutionObservation) { o.Threshold = 1 },
		func(o *bootstrapSuccessorExecutionObservation) { o.Owners[0] = common.Address{} },
		func(o *bootstrapSuccessorExecutionObservation) { o.Modules = []common.Address{o.Owners[0]} },
		func(o *bootstrapSuccessorExecutionObservation) { o.Guard = o.Owners[0] },
		func(o *bootstrapSuccessorExecutionObservation) { o.ModuleGuard = o.Owners[0] },
		func(o *bootstrapSuccessorExecutionObservation) { o.FallbackHandler = o.Owners[0] },
		func(o *bootstrapSuccessorExecutionObservation) { o.Singleton = o.Owners[0] },
		func(o *bootstrapSuccessorExecutionObservation) { o.SingletonRuntimeHash = common.Hash{} },
		func(o *bootstrapSuccessorExecutionObservation) { o.SafeProxyRuntimeHash = common.Hash{} },
		func(o *bootstrapSuccessorExecutionObservation) {
			o.PendingSafeDigests = []common.Hash{crypto.Keccak256Hash([]byte("synthetic pending digest"))}
		},
		func(o *bootstrapSuccessorExecutionObservation) { o.CoordinatorOwner = o.Owners[0] },
		func(o *bootstrapSuccessorExecutionObservation) { o.CoordinatorEvidence = o.Owners[0] },
		func(o *bootstrapSuccessorExecutionObservation) {
			o.EvidenceGetterHash = rootObjectHash("synthetic other domain")
		},
		func(o *bootstrapSuccessorExecutionObservation) {
			o.EvidenceRuntimeHash = crypto.Keccak256Hash([]byte("synthetic other runtime")).Hex()
		},
		func(o *bootstrapSuccessorExecutionObservation) { o.NativeNumber = 209 },
	}
	for i, change := range changes {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		change(&f.observation)
		if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 0 || owner.last.CumulativeAttempts != 8 {
			t.Fatal("current authority/nonce control escaped", i, err)
		}
	}
}

// A second current-state observation follows durable publication. A window
// change there retains the attempt and signatures without permitting a send.
func TestBootstrapSuccessorExecutionRechecksAfterReservation(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	f.observeHook = func(number int, observation *bootstrapSuccessorExecutionObservation) {
		if number == 2 {
			observation.NativeNumber = f.approval.Plan.Review.Request.ValidThroughNative + 1
		}
	}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 0 || owner.last.CumulativeAttempts != 9 {
		t.Fatal("owner sent after postpublication expiry or reset attempt", err)
	}
	owner = f.open(false, nil)
	if owner.last.CumulativeAttempts != 9 {
		t.Fatal("postpublication refusal renewed capacity")
	}
}

// Maximum liability and attempts are independently enforced. Exhaustion does
// not prevent later canonical completion or release either nonce fence.
func TestBootstrapSuccessorExecutionConservesFundingAndAttemptCeilings(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	f.observation.RelayerBalanceWei = "699999"
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 0 || owner.last.CumulativeAttempts != 8 {
		t.Fatal("insufficient funding created an attempt or send", err)
	}
	f.observation.RelayerBalanceWei = "700000"
	owner = f.open(false, nil)
	for range 2 {
		if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 2 || owner.last.CumulativeAttempts != 10 {
		t.Fatal("owner reset original eight attempts or exceeded its increment", err)
	}
	owner = f.open(false, nil)
	f.observation.NativeNumber = 1000
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
	if err != nil || !result.InstallationComplete || len(f.writes) != 2 || owner.last.CumulativeAttempts != 10 || owner.last.ReservedLifetimeWei != "1200000" {
		t.Fatal("expired exhausted owner lost exact canonical success", err)
	}
	plan := bootstrapSuccessorExecutionTestCopy(t, f.approval.Plan)
	target := plan.Review.Transaction.To
	plan.Review.Preparation.Approval.Plan.Proposal.OriginalUnexecutedActions = []evmPhaseAction{{Id: "synthetic-retained-reservation", Sender: plan.Review.Relayer.Sender,
		Nonce: 77, To: &target, Data: "0x00", ValueWei: "0", Gas: 100000, FeeCapWei: "1", TipCapWei: "0"}}
	f.observation.NativeNumber = plan.Review.Request.StartNativeNumber
	if err := plan.admit(f.observation); err == nil {
		t.Fatal("outer funding spent the same relayer's old unexecuted reservation")
	}
	f.observation.RelayerBalanceWei = "800000"
	if err := plan.admit(f.observation); err != nil {
		t.Fatal("exact old plus new funding was rejected", err)
	}
}

// Lost replies and pending visibility retain the exact signature. Canonical
// reconciliation precedes every retry, including process restart after expiry.
func TestBootstrapSuccessorExecutionReconcilesAmbiguousSendBeforeRetry(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	f.submitErr = errors.New("synthetic lost submission reply")
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
	if !errors.Is(err, f.submitErr) || !result.SubmissionAttempted || len(f.writes) != 1 || owner.last.CumulativeAttempts != 9 || !owner.closed {
		t.Fatal("ambiguous send lost durable attempt", err)
	}
	owner = f.open(false, nil)
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "pending"}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil || len(f.writes) != 1 || owner.last.CumulativeAttempts != 9 {
		t.Fatal("pending signature was sent again", err)
	}
	owner.close()
	owner = f.open(false, nil)
	f.observation.NativeNumber = 1000
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	result, err = advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
	if err != nil || !result.InstallationComplete || result.ActivationReady || len(f.writes) != 1 || owner.last.CumulativeAttempts != 9 {
		t.Fatal("restart resent landed exact transaction", err)
	}
}

// A successful outer status without the correct Safe event and immutable
// evidence binding cannot become installation, even with a canonical adapter.
func TestBootstrapSuccessorExecutionRequiresCanonicalInnerSuccessAndBinding(t *testing.T) {
	for _, change := range []func(*bootstrapSuccessorExecutionReceipt){
		func(r *bootstrapSuccessorExecutionReceipt) { r.Receipt.Logs = nil },
		func(r *bootstrapSuccessorExecutionReceipt) {
			r.Receipt.Logs[0].Topics[1] = crypto.Keccak256Hash([]byte("synthetic other digest"))
		},
		func(r *bootstrapSuccessorExecutionReceipt) { r.CoordinatorEvidence = common.Address{} },
		func(r *bootstrapSuccessorExecutionReceipt) {
			r.EvidenceGetterHash = rootObjectHash("synthetic wrong domain")
		},
		func(r *bootstrapSuccessorExecutionReceipt) {
			r.EvidenceRuntimeHash = crypto.Keccak256Hash([]byte("synthetic wrong runtime")).Hex()
		},
		func(r *bootstrapSuccessorExecutionReceipt) {
			r.Receipt.TransactionHash = crypto.Keccak256Hash([]byte("synthetic wrong outer"))
		},
	} {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
			t.Fatal(err)
		}
		receipt := f.receipt()
		change(receipt)
		f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: receipt}
		result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
		if err == nil || result.InstallationComplete || owner.last.Phase != "attempt-reserved" || len(f.writes) != 1 {
			t.Fatal("outer success bypassed canonical inner binding", err)
		}
	}
}

// A failed outer call consumes its account nonce while leaving the inner Safe
// signature live. Neither that nonce nor its financial floor can be released.
func TestBootstrapSuccessorExecutionRetainsRevertedOuterAndLiveInnerClaim(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
		t.Fatal(err)
	}
	receipt := f.receipt()
	receipt.Receipt.Status, receipt.Receipt.Logs = 0, nil
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: receipt}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
	if err != nil || result.Status != "outer-reverted" || result.InstallationComplete || owner.last.CumulativeAttempts != 9 {
		t.Fatal("outer revert changed nonce disposition", err)
	}
	owner.close()
	owner = f.open(false, nil)
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "absent"}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 1 {
		t.Fatal("terminal revert reopened signature custody", err)
	}
	for _, name := range f.approval.Plan.nonceNames() {
		if _, err := os.Stat(filepath.Join(f.approval.Plan.Request.RegistryDirectory, name)); err != nil {
			t.Fatal("outer revert released a nonce claim", err)
		}
	}
}

// Cancellation has no release semantics; a fresh empty resume also cannot
// silently install the original eight attempts as a new allowance.
func TestBootstrapSuccessorExecutionCancellationAndMissingClaimFailClosed(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
	if owner != nil {
		owner.close()
	}
	if err == nil {
		t.Fatal("missing execution claim resumed as new")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	owner, err = openBootstrapSuccessorExecutionStore(f.storageContext(ctx), f.approval.Plan, f.approval, f.profile, true, nil)
	if owner != nil {
		owner.close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled claim gained ownership", err)
	}
	owner = f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(ctx, owner, f, true); !errors.Is(err, context.Canceled) || len(f.writes) != 0 || owner.last.CumulativeAttempts != 8 {
		t.Fatal("canceled execution sent or released allowance", err)
	}
}
