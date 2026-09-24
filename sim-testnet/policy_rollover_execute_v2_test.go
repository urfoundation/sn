//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

func policyRolloverPublicationFixtureV2(t *testing.T) (*policyRolloverPlanV2, *Journal, uint64) {
	t.Helper()
	f := newRuntimeEvidenceProvisionV2TestFixture(t)
	journal, err := OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: "source.checkpoint", IntentHash: common.Hash{1}.Hex(), Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	p := &policyRolloverPlanV2{Schema: policyRolloverPlanV2Schema, PlanHash: common.Hash{0x88}.Hex(), SourcePlanHash: f.plan.PlanHash, SourceJournalHash: journal.Entries()[0].EntryHash, StateDir: f.stateDir, DeploymentID: f.plan.DeploymentID,
		Journal: f.plan.ValidatorEvidence.Address, Keeper: common.HexToAddress(f.roles.EVM["keeper"].Address), MaximumGasUnits: 1_000_000, MaximumFeePerGasWei: 100, MaximumAttempts: 1, AttemptTimeoutSeconds: 1,
		Members: f.prepared.Members, Epoch: f.prepared.Epoch, EVM: f.prepared.Evm, Native: f.prepared.Native}
	p.Actions, err = policyRolloverActionsV2(p)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := runtimeEvidenceProvisionLimit(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p, journal, limit
}

func TestPolicyRolloverReconcilesLostReceiptAndResumesPartialPublication(t *testing.T) {
	p, journal, limit := policyRolloverPublicationFixtureV2(t)
	defer func() { _ = journal.Close() }()
	present := map[uint64]bool{0: true}
	sends := map[uint64]int{}
	memberIndex := func(member runtimeEvidenceActivationMemberV2) uint64 {
		return (member.ValidatorId-1)*2 + member.NoId - 1
	}
	io := policyRolloverPublicationIOV2{
		Observe: func(ctx context.Context, member runtimeEvidenceActivationMemberV2) (uint64, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded finalized observation")
			}
			if present[memberIndex(member)] {
				return 220 + memberIndex(member), nil
			}
			return 0, validatorcomponent.ErrValidatorEvidenceAbsent
		},
		Send: func(ctx context.Context, _ Action, member runtimeEvidenceActivationMemberV2) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded keeper send")
			}
			i := memberIndex(member)
			sends[i]++
			if i != 2 {
				present[i] = true
			}
			return errors.New("RPC lost the receipt after send")
		},
	}
	if _, err := publishPolicyRolloverV2(t.Context(), p, journal, limit, io); err == nil {
		t.Fatal("partial publication was accepted")
	}
	if sends[0] != 0 || sends[1] != 1 || sends[2] != 1 || sends[3] != 0 {
		t.Fatalf("unexpected partial sends: %v", sends)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	journal, err = OpenJournal(p.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	// The original transaction finalizes after restart. Exhausting a send
	// budget cannot hide its exact public success or create another nonce.
	present[2] = true
	publications, err := publishPolicyRolloverV2(t.Context(), p, journal, limit, io)
	if err != nil || len(publications) != 4 {
		t.Fatalf("resume complete exact census: %v", err)
	}
	if sends[0] != 0 || sends[1] != 1 || sends[2] != 1 || sends[3] != 1 {
		t.Fatalf("resume sent an already published member: %v", sends)
	}
	before := len(journal.Entries())
	if _, err := publishPolicyRolloverV2(t.Context(), p, journal, limit, io); err != nil {
		t.Fatal(err)
	}
	if len(journal.Entries()) != before {
		t.Fatal("completed replay rewrote durable history")
	}
}

func TestPolicyRolloverRetryBudgetSurvivesRestartAndTransportNeverAuthorizesSend(t *testing.T) {
	p, journal, limit := policyRolloverPublicationFixtureV2(t)
	defer func() { _ = journal.Close() }()
	p.MaximumAttempts = 3
	reads, sends := 0, 0
	io := policyRolloverPublicationIOV2{Observe: func(context.Context, runtimeEvidenceActivationMemberV2) (uint64, error) {
		reads++
		return 0, errors.New("RPC unavailable")
	}, Send: func(context.Context, Action, runtimeEvidenceActivationMemberV2) error { sends++; return nil }}
	if _, err := publishPolicyRolloverV2(t.Context(), p, journal, limit, io); err == nil {
		t.Fatal("transport failure accepted")
	}
	if reads != 4 || sends != 0 {
		t.Fatalf("reads/sends = %d/%d", reads, sends)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	journal, err = OpenJournal(p.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publishPolicyRolloverV2(t.Context(), p, journal, limit, io); err == nil {
		t.Fatal("exhausted retry budget accepted")
	}
	if reads != 8 || sends != 0 {
		t.Fatalf("restart reset durable retry budget: %d/%d", reads, sends)
	}
	attempts, _ := policyRolloverPriorV2(p, p.Actions[0], journal.Entries())
	if attempts != 0 {
		t.Fatalf("durable attempts = %d", attempts)
	}
}

func TestPolicyRolloverChangedCheckpointAndCalldataFailClosed(t *testing.T) {
	p, journal, limit := policyRolloverPublicationFixtureV2(t)
	defer journal.Close()
	if err := journal.Append(JournalEntry{DeploymentID: p.DeploymentID, PlanHash: p.SourcePlanHash, ActionID: "unrelated", IntentHash: common.Hash{9}.Hex(), Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	called := false
	io := policyRolloverPublicationIOV2{Observe: func(context.Context, runtimeEvidenceActivationMemberV2) (uint64, error) { called = true; return 0, nil }, Send: func(context.Context, Action, runtimeEvidenceActivationMemberV2) error { called = true; return nil }}
	if _, err := publishPolicyRolloverV2(t.Context(), p, journal, limit, io); err == nil || called {
		t.Fatal("changed checkpoint reached external side effects")
	}
	if policyRolloverPlanPathV2(p.StateDir, 1, 10) == policyRolloverPlanPathV2(p.StateDir, 2, 11) || filepath.Dir(policyRolloverPlanPathV2(p.StateDir, 1, 10)) == policyRolloverRoot(p.StateDir) {
		t.Fatal("future retry overwrites original immutable plan")
	}
}

func TestPolicyRolloverTimeoutsBeforeAuthenticatedAbsenceDoNotSpendSendBudget(t *testing.T) {
	p, journal, limit := policyRolloverPublicationFixtureV2(t)
	defer journal.Close()
	reads, sends := 0, 0
	present := false
	io := policyRolloverPublicationIOV2{
		Observe: func(_ context.Context, member runtimeEvidenceActivationMemberV2) (uint64, error) {
			if member.ValidatorId != 1 || member.NoId != 1 {
				return 230, nil
			}
			reads++
			if reads <= 3 {
				return 0, context.DeadlineExceeded
			}
			if present {
				return 230, nil
			}
			return 0, validatorcomponent.ErrValidatorEvidenceAbsent
		},
		Send: func(context.Context, Action, runtimeEvidenceActivationMemberV2) error {
			sends++
			present = true
			return nil
		},
	}
	if _, err := publishPolicyRolloverV2(t.Context(), p, journal, limit, io); err != nil {
		t.Fatal(err)
	}
	attempts, _ := policyRolloverPriorV2(p, p.Actions[0], journal.Entries())
	if sends != 1 || attempts != 1 || reads != 5 {
		t.Fatalf("pre-send read retries consumed durable budget: sends=%d attempts=%d reads=%d", sends, attempts, reads)
	}
}
