// Execute the maintained vault artifact directly before introducing native
// proof fixtures. Both its successful move and exact revert retain real state.
package main

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
)

// The public artifact catalog name must select the actual creation bytecode;
// a matching ABI alone neither deploys the contract nor proves rollback.
func TestEconomicCaptureMaintainedVaultDeploysAndRetainsRollback(t *testing.T) {
	for _, reverted := range []bool{false, true} {
		fixture := newMonitorEvmFixture(t, "settlement-vault", false)
		result := economicCaptureExecuteContract(t, fixture, 20, reverted)
		if result == nil || result.receipt == nil || result.transaction == nil || result.receipt.TxHash != result.transaction.Hash() || !fixture.policy.CaptureIdentity || len(fixture.code) == 0 {
			t.Fatal("maintained original vault did not execute its signed transaction", reverted, result)
		}
		if result.registrationCalls != 1 || result.uidCalls != 1 || result.moveCalls != 1 {
			t.Fatal("maintained vault omitted original registration, UID or movement", reverted, result)
		}
		if reverted {
			if result.receipt.Status != types.ReceiptStatusFailed || len(result.receipt.Logs) != 0 || result.poolAfter != "20" || result.escrowAfter != "0" || fixture.blocks[11].snapshot.Counters["totalCaptured"] != "0" {
				t.Fatal("original vault revert retained a transfer or receipt effect", result)
			}
		} else if result.receipt.Status != types.ReceiptStatusSuccessful || len(result.receipt.Logs) != 1 || result.poolAfter != "0" || result.escrowAfter != "20" || fixture.blocks[11].snapshot.Counters["totalCaptured"] != "20" {
			t.Fatal("original vault successful capture omitted its exact transfer", result)
		}
	}
}
