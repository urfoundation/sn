//go:build runtime_rehearsal

package rehearsal

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
)

// transientProbe is the creation code of this Yul object, compiled by solc
// 0.8.24 with `--strict-assembly --evm-version cancun --optimize --bin`. solc
// warns (2394) on every use of transient storage and the release build denies
// warnings, so the probe is not a forge source.
//
//	object "TransientProbe" {
//	    code {
//	        datacopy(0, dataoffset("runtime"), datasize("runtime"))
//	        return(0, datasize("runtime"))
//	    }
//	    object "runtime" {
//	        code {
//	            // Called with no data: writes 2, then reverts.
//	            if iszero(calldatasize()) {
//	                tstore(0, 2)
//	                revert(0, 0)
//	            }
//	            // Otherwise logs and returns the slot's value on entry, its
//	            // value after a call to itself that wrote 2 and reverted, and
//	            // that call's success.
//	            mstore(0, tload(0))
//	            tstore(0, 1)
//	            mstore(0x40, call(gas(), address(), 0, 0, 0, 0, 0))
//	            mstore(0x20, tload(0))
//	            log0(0, 0x60)
//	            return(0, 0x60)
//	        }
//	    }
//	}
const transientProbe = "0x602d80600a5f395ff3fe36156025575f5c5f5260015f5d5f80808080305af16040525f5c60205260605fa060605ff35b60025f5d5f80fd"

// TestRehearsalTransientStorage checks EIP-1153 transient storage on runtime
// 475's EVM (Frontier, Pectra configuration). TLOAD and TSTORE run, and the
// storage is empty at the start of every transaction, but a write made in a
// call frame that reverts survives the revert, which EIP-1153 forbids:
// Frontier keeps transient storage in one map per transaction and rolls back
// only persistent storage (vendor/frontier/frame/evm/src/runner/stack.rs,
// SubstrateStackState). A transient reentrancy guard would therefore stay
// set after a guarded call reverts inside a caller that catches the revert,
// and refuse every later call in that transaction, so vault2 keeps its guard
// in persistent storage.
func TestRehearsalTransientStorage(t *testing.T) {
	t.Parallel()
	n := startNode(t)
	sender := newEVMKey(t)
	n.fundEVM(t, sender.address, 100*tao)
	probe := n.deploy(t, sender, hexutil.MustDecode(transientProbe))
	for transaction := 1; transaction <= 2; transaction++ {
		receipt := n.transact(t, sender, &probe, nil, []byte{1}, 0)
		if receipt.Status != gethtypes.ReceiptStatusSuccessful || len(receipt.Logs) != 1 || len(receipt.Logs[0].Data) != 96 {
			t.Fatalf("transaction %d: status %d, %d logs", transaction, receipt.Status, len(receipt.Logs))
		}
		data := receipt.Logs[0].Data
		entry, afterRevert, called := new(big.Int).SetBytes(data[:32]), new(big.Int).SetBytes(data[32:64]), new(big.Int).SetBytes(data[64:])
		t.Logf("transaction %d: slot %d on entry, %d after the reverted call (call success %d)", transaction, entry, afterRevert, called)
		// The first transaction left the slot at 2; the second starts empty.
		if entry.Sign() != 0 {
			t.Fatalf("transaction %d started with transient value %d", transaction, entry)
		}
		if called.Sign() != 0 {
			t.Fatal("the writing call did not revert")
		}
		switch afterRevert.Uint64() {
		case 2:
			// Frontier: the reverted frame's write remains.
		case 1:
			t.Fatal("transient storage now rolls back with a reverted call frame (EIP-1153); revisit the persistent reentrancy guards")
		default:
			t.Fatalf("transient value %d after the reverted call", afterRevert)
		}
	}
}
