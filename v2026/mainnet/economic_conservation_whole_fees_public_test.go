//go:build linux

// Original mixed native/fee Wasm runs in distinct owned capture and historical
// proof executables. The public combined owner consumes every body entry and
// preserves unresolved fees through durable publication and cold admission.
package main

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Every original entry is covered: one participant fee, one unrelated native
// fee and one explicit Pays::No call. Unrelated payment cannot inflate costs.
func TestEconomicWholeFeePublicOriginalBodyIncludesAllPayersAndExemptions(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "whole-fee-complete", true, nil)
	value := f.sample(t, monitorServiceHooks{})
	fees := value.OriginalFees
	if fees == nil || !fees.Complete || fees.Head.Blocks != 1 || fees.Head.Extrinsics != 3 || fees.Head.ParticipantCharges != 1 || *fees.WithdrawalRao != "1000" || *fees.RefundRao != "250" || *fees.DebitRao != "750" || fees.Head.Through != producer.source.policy.Through {
		t.Fatal("actual all-body original fee census differs", fees)
	}
	state := f.source.state(t)
	if len(state.OriginalFees) != 1 || len(state.OriginalFees[0].Extrinsics) != 3 || state.OriginalFees[0].Extrinsics[1].Participant || state.OriginalFees[0].Extrinsics[1].Status != "original-pair" || state.OriginalFees[0].Extrinsics[2].Status != "original-exempt" {
		t.Fatal("actual unrelated/native/exempt body entries were omitted", state.OriginalFees)
	}
	if value.Execution == nil || value.Execution.ProviderEntitlement != "9" || value.Execution.OwnerRecycled != "89" || value.OpeningPrincipalAlpha == nil || *value.OpeningPrincipalAlpha != "14" || value.TargetMet != nil || value.ActivationReady {
		t.Fatal("whole native costs changed alpha income, stock or missing authority", value)
	}
}

// A missing refund remains unknown even when withdrawal and transaction are
// original and every healthy native/vault/Claim component makes progress.
func TestEconomicWholeFeePublicMissingRefundKeepsHealthyProgressAndUnknownCost(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "whole-fee-missing-refund", true, nil)
	value := f.sample(t, monitorServiceHooks{})
	if value.OriginalFees == nil || value.OriginalFees.Complete || value.OriginalFees.Head.Missing != 1 || value.OriginalFees.WithdrawalRao != nil || value.OriginalFees.RefundRao != nil || value.NativeFeeWithdrawalRao != nil || value.NativeFeeRefundRao != nil || !value.NativeCurrent || !value.VaultCurrent || f.source.claimReads.Load() == 0 {
		t.Fatal("missing original refund was zero or stopped healthy siblings", value)
	}
	if !strings.Contains(strings.Join(value.MissingEvidence, ","), "whole-original-provider-native-fee-withdrawal-refund-census") || value.TargetMet != nil {
		t.Fatal("incomplete original cost acquired conformance", value)
	}
}

// Zero is established by an actual reviewed original u64 branch capture, not
// by the absence of a deposit event or the selected Ethereum receipt status.
func TestEconomicWholeFeePublicOriginalZeroRefundRequiresExecutedBranch(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "whole-fee-zero-refund", true, nil)
	value := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if value.OriginalFees == nil || !value.OriginalFees.Complete || *value.OriginalFees.WithdrawalRao != "1000" || *value.OriginalFees.RefundRao != "0" || *value.OriginalFees.DebitRao != "1000" || len(state.OriginalFees) != 1 || state.OriginalFees[0].Extrinsics[0].RefundZero == nil {
		t.Fatal("original zero-refund branch did not reach public cost evidence", value.OriginalFees, state.OriginalFees)
	}
	if state.OriginalFees[0].Extrinsics[0].TransactionHash == "" || value.TargetMet != nil {
		t.Fatal("zero refund lost original reverted transaction or granted authority", value)
	}
}

// An actual empty body proves no transaction fees for that original block.
// It says nothing about alpha principal, allocation or provider measurements.
func TestEconomicWholeFeePublicQuietBlockHasExactZeroBodyCensus(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "whole-fee-quiet", true, nil)
	value := f.sample(t, monitorServiceHooks{})
	if value.OriginalFees == nil || !value.OriginalFees.Complete || value.OriginalFees.Head.Blocks != 1 || value.OriginalFees.Head.Extrinsics != 0 || *value.OriginalFees.WithdrawalRao != "0" || *value.OriginalFees.RefundRao != "0" || *value.OriginalFees.DebitRao != "0" || value.TargetMet != nil {
		t.Fatal("actual empty body lost exact original interval", value)
	}
}

// The exact precompaction snapshot owns complete retired fees. A lost durable
// acknowledgment and two retries cannot repeat costs or erase unknown refunds.
func TestEconomicWholeFeePublicArchiveLostAckAndColdRestartKeepOriginalCosts(t *testing.T) {
	for _, mode := range []string{"complete", "missing-refund"} {
		f, _ := newEconomicConservationPrincipalFixture(t, "whole-fee-"+mode, true, nil)
		first := f.sample(t, monitorServiceHooks{})
		original := f.source.state(t).OriginalFees
		f.reset(t)
		_, args := f.plan(t)
		reached := false
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{syncDirectory: func(_, step string, file *os.File) error {
			if step == "checkpoint" {
				reached = true
				return errors.Join(file.Sync(), syscall.EIO)
			}
			return file.Sync()
		}}); code != 2 || !reached || !strings.Contains(issue, syscall.EIO.Error()) {
			t.Fatal("actual whole-fee checkpoint lost acknowledgement did not occur", code, issue)
		}
		for range 2 {
			if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
				t.Fatal("identical original fee archive did not reconcile", mode, code, issue)
			}
		}
		state := f.source.state(t)
		if state.Archive == nil || state.Archive.OriginalFees == nil || mode == "complete" && len(state.OriginalFees) != 0 || mode == "missing-refund" && !reflect.DeepEqual(state.OriginalFees, original) {
			t.Fatal("retirement dropped unresolved fee or retained complete hot history", mode, state.OriginalFees)
		}
		view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
		if err != nil {
			t.Fatal("cold original fee admission failed", err)
		}
		state.archiveView = view
		value, err := state.wholeFeeSummary(f.ctx, f.source.policy)
		closeErr := view.close()
		if err != nil || closeErr != nil || !reflect.DeepEqual(first.OriginalFees, value) {
			t.Fatal("cold original fee totals changed after lost acknowledgment", mode, err, closeErr, first.OriginalFees, value)
		}
	}
}
