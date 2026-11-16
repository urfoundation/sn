// Physical HTTP failures run through real fleet commands, signed custody and
// restart recovery without manufacturing finality or nonce contradictions.
package miner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
)

// Every joined branch must have the same typed physical origin; finding a
// timeout/status anywhere in an error tree must not conceal an integrity leaf.
func fleetRecoveryOnlyHttpStatus(err error, status int) bool {
	if err == nil {
		return false
	}
	if value, ok := err.(rpc.HTTPError); ok {
		return value.StatusCode == status
	}
	if value, ok := err.(*rpc.HTTPError); ok {
		return value.StatusCode == status
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !fleetRecoveryOnlyHttpStatus(cause, status) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return fleetRecoveryOnlyHttpStatus(wrapped.Unwrap(), status)
	}
	return false
}

// The first invocation really signs, persists and sends. Each next invocation
// fails one physical read, then reopens the same signed journal and completes
// its already included transaction with no new consent or signing inputs.
func TestFleetRecoveryEvmReadFailuresPreserveOriginalWork(t *testing.T) {
	for _, selected := range []string{"chain-id", "finalized", "canonical", "nonce"} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.opts["--dry-run"] = false
		fixture.stateLock.Lock()
		fixture.rpcFailure = func(method string) error {
			if method == "eth_sendRawTransaction" {
				return errors.New("synthetic accepted transaction lost acknowledgment")
			}
			return nil
		}
		fixture.stateLock.Unlock()
		if err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil {
			t.Fatal("uncertain original send reported success")
		}
		original := fleetRecoveryTestRecord(t, fixture)
		if original.Stage != "may_have_sent" || fixture.count("eth_sendRawTransaction") != 1 {
			t.Fatal("original pending signature was not durably retained")
		}
		for _, option := range []string{"--client_seed_file", "--hotkey_seed_file"} {
			if err := os.Remove(fleetOpt(fixture.opts, option)); err != nil {
				t.Fatal(err)
			}
		}
		fixture.stateLock.Lock()
		receipt := fixture.evmReceipt
		fixture.rpcFailure = nil
		if selected == "nonce" {
			fixture.evmReceipt = nil
		}
		failures := 0
		fixture.responseStatus = func(method string, parameters []json.RawMessage) int {
			matches := selected == "chain-id" && method == "eth_chainId" || selected == "nonce" && method == "eth_getTransactionCount"
			if method == "eth_getBlockByNumber" && len(parameters) == 2 {
				var number string
				if err := json.Unmarshal(parameters[0], &number); err != nil {
					t.Error(err)
				}
				matches = selected == "finalized" && number == "finalized" || selected == "canonical" && number == fmt.Sprintf("0x%x", receipt.BlockNumber.Uint64())
			}
			if matches {
				failures++
				return http.StatusServiceUnavailable
			}
			return 0
		}
		fixture.stateLock.Unlock()
		err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest)
		fixture.stateLock.Lock()
		failureCount := failures
		fixture.responseStatus = nil
		fixture.evmReceipt = receipt
		fixture.stateLock.Unlock()
		if failureCount == 0 || !fleetRecoveryOnlyHttpStatus(err, http.StatusServiceUnavailable) {
			t.Fatalf("%s fleet read failure became an independent identity/finality/nonce contradiction: %v", selected, err)
		}
		retained := fleetRecoveryTestRecord(t, fixture)
		if retained.Stage != original.Stage || retained.TxHash != original.TxHash || retained.Nonce != original.Nonce || !bytes.Equal(retained.Raw, original.Raw) || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_gasPrice") != 1 {
			t.Fatalf("%s read failure advanced or replaced the durable original", selected)
		}
		if err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
			t.Fatalf("%s read recovery did not finish the original transaction: %v", selected, err)
		}
		retained = fleetRecoveryTestRecord(t, fixture)
		if retained.Stage != "finalized" || retained.Mapping == nil || retained.TxHash != original.TxHash || !bytes.Equal(retained.Raw, original.Raw) || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_gasPrice") != 1 {
			t.Fatalf("%s recovered command replaced original signed work", selected)
		}
	}
}
