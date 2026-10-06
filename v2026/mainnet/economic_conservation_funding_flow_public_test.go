// The mixed source is produced by the actual retained native capture job.
// Two original receipt-backed leaves exhaust the authorized payout artifact;
// neither caller supplied source colors nor a checkpoint digest grants them.
package main

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func newEconomicFundingJointPublicFixture(t *testing.T) (*economicConservationArchiveFixture, *economicEntitlementFixture) {
	t.Helper()
	entitlement := newEconomicEntitlementFixture(t, 50)
	vault := entitlement.source.vault
	first := vault.policy.Coldkeys[0]
	second := common.Hash(entitlement.artifact.Providers[1].Coldkey).Hex()
	if first == second || len(entitlement.artifact.Leaves) != 2 {
		t.Fatal("joint funding fixture lost two distinct original leaves")
	}
	vault.policy.Coldkeys = append(vault.policy.Coldkeys, second)
	vault.policy.CaptureIdentity = true
	vault.fixtureGetters = map[string][]any{
		"pools":        {[32]byte(common.HexToHash("0x" + strings.Repeat("11", 32))), uint16(1), true},
		"selfColdkey":  {[32]byte(common.HexToHash("0x" + strings.Repeat("33", 32)))},
		"escrowHotkey": {[32]byte(common.HexToHash("0x" + strings.Repeat("55", 32)))},
	}
	for _, number := range []uint64{0, 10, 11} {
		vault.blocks[number].snapshot.Credits[second] = "0"
	}
	for _, number := range []uint64{12, 13} {
		vault.blocks[number].snapshot = monitorEvmVaultSnapshot("120", "62", "0", "58", "78", "0", "0", first)
		vault.blocks[number].snapshot.Credits[second] = "0"
	}
	economicConservationTestEvmRehash(t, vault, func(number uint64, receipt *types.Receipt) {
		if number != 12 {
			return
		}
		address, sender := common.HexToAddress(vault.policy.Address), common.HexToAddress(vault.policy.FeePayers[0])
		receipt.Logs[1] = monitorEvmTestLog(t, vault.contract, address, "Claimed", big.NewInt(3), big.NewInt(1), [32]byte(common.HexToHash(first)), big.NewInt(5000), big.NewInt(25), sender)
		receipt.Logs[2] = monitorEvmTestLog(t, vault.contract, address, "ClaimPaid", [32]byte(common.HexToHash(first)), big.NewInt(37), sender)
		receipt.Logs = append(receipt.Logs,
			monitorEvmTestLog(t, vault.contract, address, "Claimed", big.NewInt(3), big.NewInt(1), [32]byte(common.HexToHash(second)), big.NewInt(5000), big.NewInt(25), sender),
			monitorEvmTestLog(t, vault.contract, address, "ClaimPaid", [32]byte(common.HexToHash(second)), big.NewInt(25), sender))
	})
	vault.policy.BatchBlocks = entitlement.source.policy.Vault.BatchBlocks
	entitlement.source.policy.Vault = vault.policy
	// This case reads the actual vault payments directly. The fixture's old
	// epoch-one daemon expectation is not additional payout/funding authority.
	entitlement.source.policy.Claims = nil
	f, _ := economicConservationPrincipalFixtureWithSource(t, entitlement.source, "capture", true, func(job *historicalReplayJob) {
		if len(job.ExtrinsicsHex) != 1 {
			t.Fatal("joint funding original capture body changed")
		}
		job.ExtrinsicsHex[0] = nativeExecutionTestHex(append(rootCompact(32), vault.blocks[11].transactions[0].Hash().Bytes()...))
	}, nil)
	return f, entitlement
}

func economicFundingJointPublicResult(t *testing.T, result economicConservationSummary) {
	t.Helper()
	if result.Funding == nil {
		t.Fatal("public original funding projection is absent")
	}
	economicFundingTestRange(t, result.Funding.Captured, "20", "6", "6", "14", "14", true)
	// The original root's fifty units include thirty unknown opening carry.
	// Both twenty-five-unit leaves jointly consume the fourteen proven capital
	// units. An older seven-unit credit and five-unit opening credit stay unknown.
	economicFundingTestRange(t, result.Funding.Accepted, "57", "6", "43", "14", "51", false)
	economicFundingTestRange(t, result.Funding.Paid, "62", "6", "48", "14", "56", false)
	if result.Funding.OriginalCaptures != 1 || result.Funding.OriginalClaims != 3 || result.Funding.OriginalPayments != 2 || result.Funding.NoNonIncomeProviderCredit == nil || *result.Funding.NoNonIncomeProviderCredit || result.TargetMet == nil || *result.TargetMet || result.Conformance.CompleteEvidence || result.ActualNativeOutcomeVerified || result.ActivationReady {
		t.Fatal("joint original capital exhaustion became unknown or healthy", result)
	}
}

func TestEconomicFundingJointPublicOriginalLeavesExhaustAndReplayAfterRetirement(t *testing.T) {
	f, entitlement := newEconomicFundingJointPublicFixture(t)
	first := f.sample(t, monitorServiceHooks{})
	if first.CausallyJoinedCaptures != 1 || first.VaultCursor.Number != 11 {
		t.Fatal("joint public funding lacks the actual original capture", first)
	}
	result := f.sample(t, monitorServiceHooks{})
	economicFundingJointPublicResult(t, result)
	record := economicEntitlementRecord(t, f.source)
	if record.Claimed != "50" || record.Census == nil || record.Census.LeafObligationsAlpha != "50" || record.Census.FloorResidueAlpha != "0" || entitlement.artifactReads.Load() != 2 {
		t.Fatal("original complete leaf vector did not exhaust its obligation", record)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("joint original funding retirement", code, issue)
	}
	state := f.source.state(t)
	if len(state.Captures) != 0 || len(state.Claims) != 0 || len(state.Payments) != 0 || state.Archive.Counts.Claims != 3 || state.Archive.Counts.Payments != 2 {
		t.Fatal("joint original funding did not cross actual cold retirement", state)
	}
	again := f.sample(t, monitorServiceHooks{})
	economicFundingJointPublicResult(t, again)
	if !reflect.DeepEqual(result.Funding, again.Funding) || entitlement.artifactReads.Load() != 2 {
		t.Fatal("cold original replay lost correlation or readmitted a leaf", result.Funding, again.Funding)
	}
}

func TestEconomicFundingJointPublicLostAckKeepsOriginalSharedConstraint(t *testing.T) {
	f, entitlement := newEconomicFundingJointPublicFixture(t)
	f.sample(t, monitorServiceHooks{})
	var failed atomic.Bool
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == economicConservationRole && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
			return errors.Join(err, syscall.EIO)
		}
		return err
	}})
	if code != 3 || output.Len() != 0 || !failed.Load() || len(f.source.state(t).Payments) != 2 || entitlement.artifactReads.Load() != 2 {
		t.Fatal("joint funding did not reach lost checkpoint acknowledgment", code, diagnostic.String())
	}
	first := f.sample(t, monitorServiceHooks{})
	again := f.sample(t, monitorServiceHooks{})
	economicFundingJointPublicResult(t, first)
	economicFundingJointPublicResult(t, again)
	if !reflect.DeepEqual(first.Funding, again.Funding) || entitlement.artifactReads.Load() != 2 {
		t.Fatal("original retry repeated leaves or erased the shared constraint", first.Funding, again.Funding)
	}
}
