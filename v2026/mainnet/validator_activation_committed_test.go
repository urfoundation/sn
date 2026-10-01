// Committed checkpoints exercise actual journal custody and public closed
// starts. Synthetic projections test shape only, never deployed authority.
package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/validator"
)

// Checkpoint fields use the original service uid and both immutable operator
// origins. A containing checksum never substitutes for replay authentication.
func validatorActivationCommittedTestCheckpoint(f *validatorActivationFixture, index int, approved validatorActivationProofCheckpoint) validatorActivationCommittedCheckpoint {
	p := approved.Proof
	proof := validator.ProductionBootstrapCommittedObservation{Schema: validator.ProductionBootstrapCommittedSchema, ConfigHash: p.ConfigHash, PolicyHash: p.PolicyHash, ClientDomainHash: p.ClientDomainHash,
		ServiceUid: f.approval.Plan.Units[index].Unit.Uid, Native: p.Native, EvmBlock: p.EvmBlock, EvmHash: p.EvmHash, ApprovedPrefixHash: p.ContentHash,
		CensusHash: "sha256:" + strings.Repeat("e", 64), SourceCount: 12, Prefixes: slices.Clone(p.Prefixes), HistoricalSources: true}
	proof.ContentHash = rootObjectHash(proof)
	return validatorActivationCommittedCheckpoint{ObservedAt: f.now, Proof: proof}
}

// Actual later admission refusal cannot delete either kind of completed
// checkpoint or consume fresh starts. Reopening preserves exact prior clocks.
func TestValidatorActivationCommittedRetainsCheckpointAndClosesStart(t *testing.T) {
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
	approved := validatorActivationHealthTestCheckpoint(f, 0)
	if err := retainValidatorActivationProofCheckpoint(t.Context(), store, &record, 0, approved); err != nil {
		t.Fatal(err)
	}
	checkpoint := validatorActivationCommittedTestCheckpoint(f, 0, approved)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	before := f.chain.journals(t)
	result, code, detail := f.command(t.Context(), "admit-committed", nil)
	if code != 3 || result.Status != "source-refused" || !strings.Contains(detail, "complete approved contract profile") || len(result.CommittedCheckpoints) != 2 || !reflect.DeepEqual(result.CommittedCheckpoints[0], &checkpoint) || result.CommittedCheckpoints[1] != nil || !reflect.DeepEqual(result.ProofCheckpoints[0], &approved) {
		t.Fatal("later refusal erased exact completed custody", code, result, detail)
	}
	status, code, detail := f.command(t.Context(), "status", nil)
	if code != 0 || !reflect.DeepEqual(status.CommittedCheckpoints, result.CommittedCheckpoints) {
		t.Fatal("reopen", code, detail)
	}
	closed, code, detail := f.command(t.Context(), "start", nil)
	if code != 3 || closed.Status != "activation-authority-unavailable" || closed.Operations != status.Operations || f.starts != [2]int{} || !reflect.DeepEqual(before, f.chain.journals(t)) {
		t.Fatal("committed checkpoint opened public starts or changed liability", code, detail)
	}
}

// A valid later observation chains the exact prior checkpoint; every changed
// uid/source/operator or regressed clock remains a refusal after rehashing.
func TestValidatorActivationCommittedRejectsCheckpointRegression(t *testing.T) {
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
	original := validatorActivationCommittedTestCheckpoint(f, 0, validatorActivationHealthTestCheckpoint(f, 0))
	original.Proof.Native.Block, original.Proof.Native.Epoch = 10, 2
	original.Proof.Prefixes[0].LastSequence, original.Proof.Prefixes[0].Root = 5, "0x"+strings.Repeat("6", 64)
	original.Proof.Prefixes[0].Epoch, original.Proof.Prefixes[0].Generation = 2, 2
	original.Proof.ContentHash = ""
	original.Proof.ContentHash = rootObjectHash(original.Proof)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, original); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"uid", "config", "previous", "clock", "native", "native-hash", "evm-hash", "sequence", "root", "generation", "epoch", "ema", "history", "source"} {
		next := original
		next.Proof.Prefixes = slices.Clone(next.Proof.Prefixes)
		next.Proof.PreviousHash = original.Proof.ContentHash
		switch fault {
		case "uid":
			next.Proof.ServiceUid++
		case "config":
			next.Proof.ConfigHash = f.approval.Plan.Units[0].Source.ConfigHash
		case "previous":
			next.Proof.PreviousHash = ""
		case "clock":
			next.ObservedAt = next.ObservedAt.Add(-time.Second)
		case "native":
			next.Proof.Native.Block--
		case "native-hash":
			next.Proof.Native.Hash[0] ^= 1
		case "evm-hash":
			next.Proof.EvmHash = "0x" + strings.Repeat("a", 64)
		case "sequence":
			next.Proof.Prefixes[0].LastSequence--
		case "root":
			next.Proof.Prefixes[0].Root = "0x" + strings.Repeat("7", 64)
		case "generation":
			next.Proof.Prefixes[0].Generation--
		case "epoch":
			next.Proof.Prefixes[0].Epoch--
		case "ema":
			next.Proof.Prefixes[0].PriorEmaHash = "sha256:" + strings.Repeat("8", 64)
		case "history":
			next.Proof.Prefixes[0].HistoryHash = "sha256:" + strings.Repeat("9", 64)
		case "source":
			next.Proof.HistoricalSources = false
		}
		next.Proof.ContentHash = ""
		next.Proof.ContentHash = rootObjectHash(next.Proof)
		if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, next); err == nil {
			t.Fatal("rehashed regression admitted", fault)
		}
	}
	retained, err := store.load(t.Context())
	if err != nil || !reflect.DeepEqual(retained.CommittedCheckpoints[0], &original) {
		t.Fatal("failed observation replaced custody", err)
	}
	next := original
	next.Proof.PreviousHash, next.Proof.ContentHash = original.Proof.ContentHash, ""
	next.Proof.ContentHash = rootObjectHash(next.Proof)
	if err := retainValidatorActivationCommittedCheckpoint(t.Context(), store, &record, 0, next); err != nil {
		t.Fatal("valid continuation", err)
	}
}

// Complete structural composition admits both roles, while its exact ordered
// gate list and operator/current domains survive every rehashed substitution.
func TestValidatorActivationCommittedCompositionPinsBothRolesAndGates(t *testing.T) {
	f, readiness := validatorActivationHealthTestReadiness(t)
	health := readiness.Health
	health.Committed = &[2]validatorActivationCommittedCheckpoint{}
	health.OpenGates = validatorActivationCommittedOpenGates()
	for i, approved := range health.Proofs {
		health.Committed[i] = validatorActivationCommittedTestCheckpoint(f.activation, i, approved)
	}
	health.ContentHash = ""
	health.ContentHash = rootObjectHash(*health)
	if err := readiness.validate(f.activation.approval.Plan); err != nil {
		t.Fatal("complete structural control", err)
	}
	want := []string{"UNSEALED_LEDGER_AND_INTENT_STATE_UNVERIFIED", "PER_OPERATOR_LIVE_WORKER_UNVERIFIED", "GLOBAL_SIGNER_CUSTODY_UNVERIFIED", "APPLIED_WEIGHTS_INFLUENCE_UNVERIFIED", "SIGNED_LAUNCH_AUTHORITY_UNAVAILABLE"}
	if !slices.Equal(health.OpenGates, want) || !slices.Equal(validatorActivationHealthOpenGates(), []string{"CURRENT_MUTABLE_PROOF_PREFIX_UNVERIFIED", "PER_OPERATOR_LIVE_WORKER_UNVERIFIED", "GLOBAL_SIGNER_CUSTODY_UNVERIFIED", "APPLIED_WEIGHTS_INFLUENCE_UNVERIFIED", "SIGNED_LAUNCH_AUTHORITY_UNAVAILABLE"}) {
		t.Fatal("gate scope or ordering changed")
	}
	for _, fault := range []string{"uid", "config", "approved", "client", "native", "evm", "operator", "activation", "future", "source", "clock", "gate-order", "gate-drop", "gate-old"} {
		raw, err := json.Marshal(readiness)
		if err != nil {
			t.Fatal(err)
		}
		var changed validatorActivationReadiness
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		checkpoint := &changed.Health.Committed[1]
		p := &checkpoint.Proof
		switch fault {
		case "uid":
			p.ServiceUid++
		case "config":
			p.ConfigHash = f.activation.approval.Plan.Units[1].Source.ConfigHash
		case "approved":
			p.ApprovedPrefixHash = "sha256:" + strings.Repeat("2", 64)
		case "client":
			p.ClientDomainHash = "sha256:" + strings.Repeat("2", 64)
		case "native":
			p.Native.Hash[0] ^= 1
		case "evm":
			p.EvmBlock++
		case "operator":
			p.Prefixes[1].NoId++
		case "activation":
			p.Prefixes[1].ActivationHash = "0x" + strings.Repeat("2", 64)
		case "future":
			p.Prefixes[1].Epoch++
		case "source":
			p.HistoricalSources = false
		case "clock":
			checkpoint.ObservedAt = checkpoint.ObservedAt.Add(-time.Second)
		case "gate-order":
			slices.Reverse(changed.Health.OpenGates)
		case "gate-drop":
			changed.Health.OpenGates = changed.Health.OpenGates[1:]
		case "gate-old":
			changed.Health.OpenGates = validatorActivationHealthOpenGates()
		}
		p.ContentHash = ""
		p.ContentHash = rootObjectHash(*p)
		changed.Health.ContentHash = ""
		changed.Health.ContentHash = rootObjectHash(*changed.Health)
		if err := changed.validate(f.activation.approval.Plan); err == nil {
			t.Fatal("rehashed committed substitution admitted", fault)
		}
	}
}

// A committed disposition must have both exact journal checkpoints; deleting
// the second role or its observation is not a valid partial completion status.
func TestValidatorActivationCommittedJournalRequiresCompleteDisposition(t *testing.T) {
	f, readiness := validatorActivationHealthTestReadiness(t)
	record := validatorActivationRecord{Schema: validatorActivationSchema, Approval: f.activation.approval, PublicKey: f.activation.key, HighWaterAt: f.activation.now, Status: "observed-committed-prefix-and-worker-health", Readiness: &readiness}
	health := readiness.Health
	health.Committed, health.OpenGates = &[2]validatorActivationCommittedCheckpoint{}, validatorActivationCommittedOpenGates()
	for i, approved := range health.Proofs {
		record.ProofCheckpoints = append(record.ProofCheckpoints, &approved)
		value := validatorActivationCommittedTestCheckpoint(f.activation, i, approved)
		health.Committed[i] = value
		record.CommittedCheckpoints = append(record.CommittedCheckpoints, &value)
	}
	health.ContentHash = ""
	health.ContentHash = rootObjectHash(*health)
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(f.activation.approval, f.activation.key); err != nil {
		t.Fatal("complete journal control", err)
	}
	record.CommittedCheckpoints[1] = nil
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(f.activation.approval, f.activation.key); err == nil {
		t.Fatal("partial committed disposition admitted")
	}
}
