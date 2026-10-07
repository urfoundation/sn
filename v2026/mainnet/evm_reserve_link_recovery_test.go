// Binding lifecycle tests preserve cumulative authority and exact
// historical custody across deterministic interruption boundaries.
package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Missing, signed or merely included proxy custody cannot unlock binding;
// its exact retained initialized completion is required.
func TestEvmReserveLinkRequiresCompletedProxy(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	refused := func(stage string) {
		t.Helper()
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("apply", "--action", "reserve-link"); code != 3 {
			t.Fatalf("%s proxy admitted reserve binding: %d %s", stage, code, diagnostic)
		}
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmReserveLinkStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) || !maps.Equal(counts, f.counts) {
			t.Fatalf("%s proxy opened binding custody: %v", stage, err)
		}
	}
	refused("absent")
	f.prepareProxySigned()
	refused("signed")
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	refused("included")
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("apply", "--action", "reserve-link"); code != 0 || result.Status != "signature-awaiting-import" {
		t.Fatalf("completed proxy did not unlock reserve binding: %+v %d %s", result, code, diagnostic)
	}
}

// All five original receipts are reauthenticated before a binding attempt
// without rewriting the selected or ancestor journals.
func TestEvmReserveLinkReauditsFivePredecessors(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	path := filepath.Join(f.config.Plan.RunDirectory, evmReserveLinkStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for ancestor := 0; ancestor < 5; ancestor++ {
		observed := false
		f.override = func(method string, params []any, result any) any {
			if method == "eth_getTransactionReceipt" && result != nil {
				if receipt, ok := result.(map[string]any); ok && receipt != nil {
					for hash, tx := range f.history.transactions {
						if tx.Nonce() == uint64(ancestor) && receipt["blockHash"] == hash {
							observed = true
							changed := maps.Clone(receipt)
							changed["gasUsed"] = "0x0"
							return changed
						}
					}
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "contradicts") || len(f.writes) != 5 {
			t.Fatalf("binding skipped ancestor %d: %d %s", ancestor, code, diagnostic)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("binding audit fault changed custody: %v", err)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("binding ancestor recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Each historical checkpoint fences initial and refreshed admission even
// after all five predecessor receipt audits finished.
func TestEvmReserveLinkFencesFivePredecessorCheckpoints(t *testing.T) {
	for _, number := range []uint64{101, 102, 103, 104, 105} {
		for _, refresh := range []bool{false, true} {
			f := newEvmReserveLinkFixture(t)
			f.prepareReserveLinkSigned()
			originalHash := f.hashes[number]
			original := f.headers[originalHash]
			fork, forkHash := evmTestNativeHeader(t, original.ParentHash, number, append(append([]string(nil), original.Digest.Logs...), "0x0000"))
			f.headers[forkHash] = fork
			proxyObserved, targetObserved, changed := false, false, false
			f.override = func(method string, params []any, result any) any {
				if method == "eth_getStorageAt" && params[0] == f.plan.Proxy.Address.Hex() && params[1] == f.plan.Proxy.Storage[4].Slot {
					proxyObserved = true
				}
				if method == "eth_getCode" && params[0] == f.plan.Address.Hex() && params[1] == "pending" {
					targetObserved = true
				}
				if method == "chain_getFinalizedHead" && proxyObserved && (!refresh || targetObserved) && !changed {
					changed = true
					f.hashes[number] = forkHash
					return f.hashes[f.head]
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !changed || !strings.Contains(diagnostic, "ancestry") || len(f.writes) != 5 {
				t.Fatalf("binding checkpoint %d refresh=%v accepted: %d %s", number, refresh, code, diagnostic)
			}
			f.override = nil
			f.hashes[number] = originalHash
			if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Attempts != 1 {
				t.Fatalf("binding checkpoint recovery: %+v %d %s", result, code, diagnostic)
			}
		}
	}
}

// A lost acknowledgement preserves the executed original one-shot call, its
// consumed nonce and its bounded attempt.
func TestEvmReserveLinkLostReplyRecoversOriginalBinding(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	f.loseReply = true
	if _, code, _ := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || len(f.writes) != 6 {
		t.Fatalf("uncertain binding reply retried or acknowledged: %d", code)
	}
	retained, code, diagnostic := f.command("resume", "--action", "reserve-link")
	if code != 0 || retained.Attempts != 1 || retained.Receipt != nil || retained.TransactionHash != f.tx.Hash().Hex() {
		t.Fatalf("uncertain binding liability lost: %+v %d %s", retained, code, diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit")
	if code != 0 || result.Status != "reserve-recorder-bound" || result.Attempts != 1 || result.TransactionHash != retained.TransactionHash || len(f.writes) != 6 || !bytes.Equal(f.writes[5], f.raw) {
		t.Fatalf("uncertain binding acquired replacement authority: %+v %d %s", result, code, diagnostic)
	}
}

// Five predecessor submissions consume five of the original six attempts;
// reopening cannot renew that graph allowance.
func TestEvmReserveLinkKeepsCumulativeGraphAttempts(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.config.Plan.MaximumAttempts = 6
	f.publishConfig()
	f.prepareReserveLinkSigned()
	f.mine = false
	for attempt := 0; attempt < 3; attempt++ {
		result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit")
		if code != 0 || result.Attempts != 1 || attempt > 0 && result.Status != "attempt-allowance-exhausted" {
			t.Fatalf("binding renewed graph allowance: %+v %d %s", result, code, diagnostic)
		}
	}
	if len(f.writes) != 6 {
		t.Fatal("binding exceeded cumulative graph attempts")
	}
}

// An interruption after attempt publication poisons the owner; reopening
// counts the ambiguous attempt before another transport effect.
func TestEvmReserveLinkAmbiguousPublicationRetainsAttempt(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	f.mine = false
	stores, records := f.openReserveLinkAncestors()
	store, err := openEvmReserveLinkActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmReserveLinkOwner(f.plan, store, stores[0], stores[1], stores[2], stores[3], stores[4], chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 2 {
			return errors.New("synthetic binding attempt publication interruption")
		}
		return nil
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("ambiguous binding publication acknowledged")
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != 5 {
		t.Fatal("poisoned binding owner reached transport")
	}
	store.close()
	for _, prior := range stores {
		prior.close()
	}
	result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit")
	if code != 0 || result.Attempts != 2 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 6 {
		t.Fatalf("binding reopen renewed ambiguous attempt: %+v %d %s", result, code, diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Status != "attempt-allowance-exhausted" || result.Attempts != 2 || len(f.writes) != 6 {
		t.Fatalf("binding exceeded original ambiguous allowance: %+v %d %s", result, code, diagnostic)
	}
}

// Both initial-claim boundaries recover only the untouched record under six
// locks; a missing claimed child cannot regain fresh authority.
func TestEvmReserveLinkClaimRecoveryKeepsSixLocks(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "record-synced"} {
		f := newEvmReserveLinkFixture(t)
		f.prepareReserveLinkPrerequisites()
		stores, records := f.openReserveLinkAncestors()
		store, err := openEvmReserveLinkActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], true, func(stage string) error {
			if stage == boundary {
				return errors.New("synthetic binding initial claim interruption")
			}
			return nil
		}, f.storage.Context)
		if err == nil || store != nil {
			t.Fatalf("binding %s interruption acknowledged", boundary)
		}
		store, err = openEvmReserveLinkActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		if err != nil || record.Signed != "" || record.Attempts != 0 || record.PredecessorHash != rootObjectHash(records[4]) {
			t.Fatalf("binding claim recovery changed custody: %+v %v", record, err)
		}
		for index := 0; index < 6; index++ {
			predecessor := ""
			if index > 0 {
				predecessor = rootObjectHash(records[index-1])
			}
			if other, err := openEvmSelectedActionStore(f.config, index, predecessor, false, nil, f.storage.Context); err == nil {
				other.close()
				t.Fatalf("binding lost held lock %d", index)
			}
		}
		store.close()
		if err := os.Remove(filepath.Join(f.config.Plan.RunDirectory, evmReserveLinkStateFile)); err != nil {
			t.Fatal(err)
		}
		for _, create := range []bool{false, true} {
			if other, err := openEvmReserveLinkActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], create, nil, f.storage.Context); err == nil {
				other.close()
				t.Fatal("lost binding child renewed allowance")
			}
		}
		for _, prior := range stores {
			prior.close()
		}
	}
}

// Even a self-consistent changed ancestor differs from the original hash
// sealed into its descendant marker, before any route access.
func TestEvmReserveLinkRejectsChangedFiveAncestorLineage(t *testing.T) {
	for ancestor := 0; ancestor < 5; ancestor++ {
		f := newEvmReserveLinkFixture(t)
		f.prepareReserveLinkSigned()
		stores, records := f.openReserveLinkAncestors()
		record := records[ancestor]
		record.Attempts++
		record.ContentHash = ""
		record.ContentHash = rootObjectHash(record)
		if err := stores[ancestor].save(record); err != nil {
			t.Fatal(err)
		}
		for _, store := range stores {
			store.close()
		}
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 3 || !strings.Contains(diagnostic, "marker differs") || !maps.Equal(counts, f.counts) || len(f.writes) != 5 {
			t.Fatalf("binding learned changed ancestor %d: %d %s", ancestor, code, diagnostic)
		}
	}
}

// Convenience output may fail only after the authoritative binding receipt
// has been retained for offline recovery.
func TestEvmReserveLinkOutputFailureRetainsBinding(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	args := []string{"bootstrap-contracts", "resume", "--action", "reserve-link", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--online", "--submit"}
	var stderr bytes.Buffer
	if code := runMain(f.storageContext(context.Background()), args, bootstrapRootFailedWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output failed") {
		t.Fatalf("binding output failed before authoritative publication: %d %s", code, stderr.String())
	}
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link"); code != 0 || result.Status != "reserve-recorder-bound" || result.Receipt == nil || result.Receipt.StorageHash == "" || len(f.writes) != 6 {
		t.Fatalf("output loss erased recorder binding: %+v %d %s", result, code, diagnostic)
	}
}

// The proxy signature and all six journal or marker paths cannot populate
// the binding action original signature custody.
func TestEvmReserveLinkRejectsOtherSignatureAndSixCustodyAliases(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkPrerequisites()
	if _, code, diagnostic := f.command("apply", "--action", "reserve-link"); code != 0 {
		t.Fatal(diagnostic)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, "proxy-create.signed.bin")
	_, hash, err := readBootstrapRootFile(context.Background(), path, 128*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "signed-byte envelope") {
		t.Fatalf("proxy signature filled binding custody: %d %s", code, diagnostic)
	}
	for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile} {
		for _, suffix := range []string{"", ".lock"} {
			path = filepath.Join(f.config.Plan.RunDirectory, name+suffix)
			_, hash, err = readBootstrapRootFile(context.Background(), path, 128*1024)
			if err != nil {
				t.Fatal(err)
			}
			if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "aliases custody") {
				t.Fatalf("binding input aliased %s: %d %s", name+suffix, code, diagnostic)
			}
		}
	}
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link"); code != 0 || result.Status != "signature-awaiting-import" || result.Attempts != 0 {
		t.Fatalf("binding invalid import changed custody: %+v %d %s", result, code, diagnostic)
	}
}

// Expiry, runtime changes, nonce movement and missing target code retain
// the original signed binding without a new spend or nonce.
func TestEvmReserveLinkCurrentAdmissionRetainsOriginalLiability(t *testing.T) {
	for _, fault := range []string{"expiry", "confirmed", "pending", "runtime", "target-code"} {
		f := newEvmReserveLinkFixture(t)
		if fault == "expiry" {
			f.config.Plan.ValidThroughNative = 105
			f.publishConfig()
		}
		f.prepareReserveLinkSigned()
		status := "approval-expired-signed-liability-retained"
		if fault == "expiry" || fault == "runtime" {
			f.advanceEmpty()
		}
		if fault != "expiry" {
			status = "nonce-consumed-receipt-unresolved"
			if fault == "pending" {
				status = "pending-nonce-unresolved"
			}
			f.override = func(method string, params []any, result any) any {
				if (fault == "confirmed" || fault == "pending") && method == "eth_getTransactionCount" && (fault == "confirmed" || params[1] == "pending") {
					return "0x6"
				}
				if fault == "runtime" && method == "state_getRuntimeVersion" && params[0] == f.hashes[f.head] {
					version := f.config.Plan.Runtime.RuntimeVersion
					version.SpecVersion++
					return version
				}
				if fault == "target-code" && method == "eth_getCode" && params[0] == f.plan.Address.Hex() && params[1] == "pending" {
					return "0x00"
				}
				return result
			}
		}
		result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit")
		if fault == "runtime" || fault == "target-code" {
			if code != 1 {
				t.Fatalf("binding %s admitted: %d %s", fault, code, diagnostic)
			}
			result, code, diagnostic = f.command("resume", "--action", "reserve-link")
		} else if result.Status != status {
			t.Fatalf("binding %s status: %+v %d %s", fault, result, code, diagnostic)
		}
		if code != 0 || result.Attempts != 0 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 5 {
			t.Fatalf("binding %s replaced original liability: %+v %d %s", fault, result, code, diagnostic)
		}
	}
}

// A later runtime change and expiry cannot erase the binding authenticated
// at its historical runtime and canonical inclusion mapping.
func TestEvmReserveLinkHistoricalBindingAfterRuntimeChange(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.config.Plan.ValidThroughNative = 106
	f.publishConfig()
	f.prepareReserveLinkSigned()
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	f.advanceEmpty()
	f.override = func(method string, params []any, result any) any {
		if method == "state_getRuntimeVersion" && params[0] == f.hashes[f.head] {
			version := f.config.Plan.Runtime.RuntimeVersion
			version.SpecVersion++
			return version
		}
		return result
	}
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Status != "reserve-recorder-bound" || result.Receipt == nil || result.Receipt.NativeNumber != 106 || len(f.writes) != 6 {
		t.Fatalf("later runtime erased binding initialization: %+v %d %s", result, code, diagnostic)
	}
}
