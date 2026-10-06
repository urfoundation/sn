// The second current-state pass must close finality itself. A healthy first
// advance cannot authorize a later send from stale account or contract reads.
package main

import (
	"bytes"
	"testing"
)

// Exact durable custody survives a refused observation without consuming an
// attempt or asking for another signature.
func readEvmFinalityCustody(t *testing.T, f *evmCreateFixture) evmActionRecord {
	t.Helper()
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	closeErr := store.close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return record
}

// All four contradictions land after the refresh pass's final pending-code
// read. Without its own closing mapping, the public command broadcasts once.
func TestEvmCreateRefreshFinalityRefusesLateContradictions(t *testing.T) {
	for _, fault := range []string{"regressed-head", "replaced-native", "replaced-evm", "missing-head"} {
		f := newEvmCreateFixture(t)
		f.prepareSigned()
		original := readEvmFinalityCustody(t, f)
		balances, codes := 0, 0
		late := false
		f.override = func(method string, args []any, result any) any {
			if method == "eth_getBalance" {
				balances++
				if balances == 1 {
					f.advanceEmpty()
				}
			}
			if method == "eth_getCode" && args[1] == "pending" {
				codes++
				if codes == 2 {
					late = true
					if fault == "regressed-head" {
						f.head = 100
					}
				}
			}
			if late {
				switch {
				case fault == "replaced-native" && method == "chain_getBlockHash" && args[0] == float64(101):
					return testGenesisHash
				case fault == "replaced-evm" && method == "eth_getBlockByNumber" && args[0] == "0x26":
					responseKVs := map[string]any{}
					for key, value := range result.(map[string]any) {
						responseKVs[key] = value
					}
					responseKVs["hash"] = testGenesisHash
					return responseKVs
				case fault == "missing-head" && method == "chain_getFinalizedHead":
					return nil
				}
			}
			return result
		}
		_, code, diagnostic := f.command("resume", "--online", "--submit")
		if !late || balances != 2 || codes != 2 || code == 0 || len(f.writes) != 0 || rootObjectHash(readEvmFinalityCustody(t, f)) != rootObjectHash(original) {
			t.Errorf("%s late contradiction changed custody or sent: late=%t balances=%d codes=%d writes=%d exit=%d %s", fault, late, balances, codes, len(f.writes), code, diagnostic)
		}
	}
}

// Once the route recovers, the original unchanged imported transaction can
// complete its one send and canonical receipt without another approval.
func TestEvmCreateRefreshFinalityRecoversOriginalTransaction(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	original := readEvmFinalityCustody(t, f)
	balances, codes := 0, 0
	f.override = func(method string, args []any, result any) any {
		if method == "eth_getBalance" {
			balances++
			if balances == 1 {
				f.advanceEmpty()
			}
		}
		if method == "eth_getCode" && args[1] == "pending" {
			codes++
			if codes == 2 {
				f.head = 100
			}
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code == 0 || balances != 2 || codes != 2 || len(f.writes) != 0 || rootObjectHash(readEvmFinalityCustody(t, f)) != rootObjectHash(original) {
		t.Fatalf("regressed refresh published an attempt: exit=%d balances=%d codes=%d writes=%d %s", code, balances, codes, len(f.writes), diagnostic)
	}
	f.override, f.head = nil, 101
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Attempts != 1 || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) {
		t.Fatalf("recovered refresh lost original transaction: %+v exit=%d %s", result, code, diagnostic)
	}
	result, code, diagnostic = f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || result.Receipt == nil || result.Receipt.NativeNumber != 102 || result.Receipt.BlockNumber != 39 || result.Attempts != 1 || len(f.writes) != 1 {
		t.Fatalf("recovered refresh duplicated or lost canonical inclusion: %+v exit=%d %s", result, code, diagnostic)
	}
}

// Both passes may see healthy advancement. Closing the selected mapping does
// not demand a stationary chain or change the retained nonce/signature.
func TestEvmCreateRefreshFinalityAllowsFurtherAdvancement(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	balances, codes := 0, 0
	f.override = func(method string, args []any, result any) any {
		if method == "eth_getBalance" {
			balances++
			if balances == 1 {
				f.advanceEmpty()
			}
		}
		if method == "eth_getCode" && args[1] == "pending" {
			codes++
			if codes == 2 {
				f.advanceEmpty()
			}
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || balances != 2 || codes != 2 || result.Attempts != 1 || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) {
		t.Fatalf("healthy second advance stranded or changed transaction: %+v exit=%d balances=%d codes=%d %s", result, code, balances, codes, diagnostic)
	}
	f.override = nil
	result, code, diagnostic = f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || result.Receipt == nil || result.Receipt.NativeNumber != 103 || result.Receipt.BlockNumber != 40 || len(f.writes) != 1 {
		t.Fatalf("healthy second advance lost receipt: %+v exit=%d %s", result, code, diagnostic)
	}
}
