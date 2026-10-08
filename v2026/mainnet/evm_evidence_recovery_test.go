// Evidence custody tests retain seven original ancestors and the unchanged
// eight-attempt graph bound across uncertain publication and process restart.
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

// Missing, signed or merely included vault binding cannot unlock evidence.
func TestEvmEvidenceCreateRequiresCompletedVaultBinding(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	refused := func(stage string) {
		t.Helper()
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("apply", "--action", "evidence-create"); code != 3 {
			t.Fatalf("%s binding admitted evidence: %d %s", stage, code, diagnostic)
		}
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmEvidenceCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) || !maps.Equal(counts, f.counts) {
			t.Fatalf("%s binding opened evidence custody: %v", stage, err)
		}
	}
	refused("absent")
	f.prepareVaultLinkSigned()
	refused("signed")
	if _, code, diagnostic := f.command("resume", "--action", "vault-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	refused("included")
	if _, code, diagnostic := f.command("resume", "--action", "vault-link", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("apply", "--action", "evidence-create"); code != 0 || result.Status != "signature-awaiting-import" {
		t.Fatalf("completed vault binding did not unlock evidence: %+v %d %s", result, code, diagnostic)
	}
}

// Every predecessor receipt is reauthenticated before a new attempt.
func TestEvmEvidenceCreateReauditsSevenPredecessors(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	path := filepath.Join(f.config.Plan.RunDirectory, evmEvidenceCreateStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for ancestor := uint64(0); ancestor < 7; ancestor++ {
		observed := false
		f.override = func(method string, _ []any, result any) any {
			if method == "eth_getTransactionReceipt" {
				if receipt, ok := result.(map[string]any); ok && receipt != nil {
					for hash, transaction := range f.history.transactions {
						if transaction.Nonce() == ancestor && receipt["blockHash"] == hash {
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
		if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !observed || len(f.writes) != 7 {
			t.Fatalf("evidence skipped ancestor %d: %d %s", ancestor, code, diagnostic)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("ancestor fault changed evidence custody: %v", err)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Attempts != 1 || len(f.writes) != 8 {
		t.Fatalf("ancestor recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Explicit barriers fork each original checkpoint after the seventh audit,
// then separately after current admission before its refreshed continuity fence.
func TestEvmEvidenceCreateFencesSevenPredecessorCheckpoints(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		for number := uint64(101); number <= 107; number++ {
			f := newEvmEvidenceFixture(t)
			f.prepareEvidenceSigned()
			var bindingHash string
			for hash, transaction := range f.history.transactions {
				if transaction.Nonce() == 6 {
					bindingHash = hash
				}
			}
			if bindingHash == "" {
				t.Fatal("vault binding lacks its actual inclusion")
			}
			originalHash := f.hashes[number]
			original := f.headers[originalHash]
			fork, forkHash := evmTestNativeHeader(t, original.ParentHash, number, append(append([]string(nil), original.Digest.Logs...), "0x0000"))
			f.headers[forkHash] = fork
			proxyObserved, targetObserved, pendingObserved, changed := false, false, false, false
			f.override = func(method string, params []any, result any) any {
				if method == "eth_getStorageAt" && params[0] == f.plan.Proxy.Address.Hex() && params[1] == f.plan.Proxy.Storage[4].Slot {
					switch block := params[2].(type) {
					case map[string]any:
						if block["blockHash"] == bindingHash {
							proxyObserved = true
						}
					case string:
						if block == "pending" {
							pendingObserved = true
						}
					}
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
			// Keep mixed selectors in the direct regression path instead of
			// allowing an HTTP-handler panic to become a retry timeout.
			if result := f.override("eth_getStorageAt", []any{f.plan.Proxy.Address.Hex(), f.plan.Proxy.Storage[4].Slot, "pending"}, "0x"); result != "0x" || !pendingObserved || proxyObserved || targetObserved || changed {
				t.Fatal("pending storage selector released the evidence checkpoint barrier")
			}
			pendingObserved = false
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !changed || refresh && !pendingObserved || !strings.Contains(diagnostic, "ancestry") || len(f.writes) != 7 {
				t.Fatalf("evidence checkpoint %d refresh=%v accepted: %d %s", number, refresh, code, diagnostic)
			}
			f.override = nil
			f.hashes[number] = originalHash
			if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
				t.Fatalf("evidence checkpoint recovery: %+v %d %s", result, code, diagnostic)
			}
		}
	}
}

// The lost reply occurs after genuine creation; receipt recovery cannot send
// again even though all eight original graph attempts are already consumed.
func TestEvmEvidenceCreateLostReplyRecoversOriginalCreation(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	f.loseReply = true
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || len(f.writes) != 8 {
		t.Fatalf("evidence lost reply was retried: %d %s", code, diagnostic)
	}
	f.loseReply = false
	result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit")
	if code != 0 || result.Status != "evidence-created-unanchored" || result.Attempts != 1 || result.TransactionHash != f.tx.Hash().Hex() || result.Receipt == nil || result.Receipt.TransactionHash != result.TransactionHash || len(f.writes) != 8 || !bytes.Equal(f.writes[7], f.raw) {
		t.Fatalf("uncertain evidence acquired replacement authority: %+v %d %s", result, code, diagnostic)
	}
}

// Seven prior submissions leave one attempt under the original maximum eight.
func TestEvmEvidenceCreateKeepsCumulativeGraphAttempts(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	f.mine = false
	for attempt := 0; attempt < 3; attempt++ {
		result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit")
		if code != 0 || result.Attempts != 1 || attempt > 0 && result.Status != "attempt-allowance-exhausted" {
			t.Fatalf("evidence renewed graph allowance: %+v %d %s", result, code, diagnostic)
		}
	}
	if len(f.writes) != 8 {
		t.Fatal("evidence exceeded cumulative graph attempts")
	}
}

// Ambiguous durable publication consumes the last allowance even when transport
// was never reached. Reopening retains the original liability without a send.
func TestEvmEvidenceCreateAmbiguousPublicationExhaustsLastAttempt(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	stores, records := f.openEvidenceAncestors()
	store, err := openEvmEvidenceActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], records[5], records[6], false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmEvidenceCreateOwner(f.plan, store, stores[0], stores[1], stores[2], stores[3], stores[4], stores[5], stores[6], chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 2 {
			return errors.New("synthetic evidence attempt publication interruption")
		}
		return nil
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("ambiguous evidence publication acknowledged")
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != 7 {
		t.Fatal("poisoned evidence owner reached transport")
	}
	store.close()
	for _, prior := range stores {
		prior.close()
	}
	for restart := 0; restart < 2; restart++ {
		result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit")
		if code != 0 || result.Status != "attempt-allowance-exhausted" || result.Attempts != 1 || result.TransactionHash != f.tx.Hash().Hex() || result.Receipt != nil || len(f.writes) != 7 {
			t.Fatalf("evidence reopen renewed ambiguous allowance: %+v %d %s", result, code, diagnostic)
		}
	}
}

// Both initial claim boundaries retain the original marker and eight locks;
// removing a claimed child must never manufacture a fresh signing reservation.
func TestEvmEvidenceCreateClaimRecoveryKeepsEightLocks(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "record-synced"} {
		f := newEvmEvidenceFixture(t)
		f.prepareEvidencePrerequisites()
		stores, records := f.openEvidenceAncestors()
		store, err := openEvmEvidenceActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], records[5], records[6], true, func(stage string) error {
			if stage == boundary {
				return errors.New("synthetic evidence initial claim interruption")
			}
			return nil
		}, f.storage.Context)
		if err == nil || store != nil {
			t.Fatalf("evidence %s interruption acknowledged", boundary)
		}
		store, err = openEvmEvidenceActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], records[5], records[6], false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		if err != nil || record.Signed != "" || record.Attempts != 0 || record.PredecessorHash != rootObjectHash(records[6]) {
			t.Fatalf("evidence claim recovery changed custody: %+v %v", record, err)
		}
		for index := 0; index < 8; index++ {
			predecessor := ""
			if index > 0 {
				predecessor = rootObjectHash(records[index-1])
			}
			competing, err := openEvmSelectedActionStore(f.config, index, predecessor, false, nil, f.storage.Context)
			if err == nil || competing != nil {
				t.Fatalf("evidence operation released lock %d", index)
			}
		}
		store.close()
		for _, prior := range stores {
			prior.close()
		}
		if err := os.Remove(filepath.Join(f.config.Plan.RunDirectory, evmEvidenceCreateStateFile)); err != nil {
			t.Fatal(err)
		}
		if _, code, diagnostic := f.command("apply", "--action", "evidence-create"); code != 3 {
			t.Fatalf("missing claimed evidence journal recreated: %d %s", code, diagnostic)
		}
	}
}

// Rehashed local changes cannot replace any ancestor consumed by the approval.
func TestEvmEvidenceCreateRejectsChangedSevenAncestorLineage(t *testing.T) {
	for ancestor := 0; ancestor < 7; ancestor++ {
		f := newEvmEvidenceFixture(t)
		f.prepareEvidenceSigned()
		stores, records := f.openEvidenceAncestors()
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
		if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 3 || !maps.Equal(counts, f.counts) || len(f.writes) != 7 {
			t.Fatalf("evidence learned changed ancestor %d: %d %s", ancestor, code, diagnostic)
		}
	}
}

// The durable canonical receipt remains authoritative after convenience output
// fails; the next process must publish it without another original send.
func TestEvmEvidenceCreateOutputFailureRetainsCreation(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	var stderr bytes.Buffer
	code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "resume", "--action", "evidence-create", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--online"}, bootstrapRootFailedWriter{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "output failed") {
		t.Fatalf("evidence output failure not surfaced: %d %s", code, stderr.String())
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create"); code != 0 || result.Status != "evidence-created-unanchored" || result.Receipt == nil || len(f.writes) != 8 {
		t.Fatalf("evidence output loss lost creation: %+v %d %s", result, code, diagnostic)
	}
}

// A different original reservation or any one of sixteen custody paths cannot
// substitute for the imported evidence transaction. Input rejection precedes
// custody, preserves every journal byte and leaves the genuine import available.
func TestEvmEvidenceCreateRejectsOtherSignatureAndEightCustodyAliases(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidencePrerequisites()
	if _, code, diagnostic := f.command("apply", "--action", "evidence-create"); code != 0 {
		t.Fatal(diagnostic)
	}
	counts := maps.Clone(f.counts)
	before := map[string][]byte{}
	custodyNames := []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile, evmVaultLinkStateFile, evmEvidenceCreateStateFile}
	for _, name := range custodyNames {
		for _, suffix := range []string{"", ".lock"} {
			raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name+suffix))
			if err != nil {
				t.Fatal(err)
			}
			before[name+suffix] = raw
		}
	}
	for _, name := range custodyNames {
		for _, suffix := range []string{"", ".lock"} {
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--signed-transaction", filepath.Join(f.config.Plan.RunDirectory, name+suffix), "--signed-transaction-hash", f.signedHash); code != 2 || !strings.Contains(diagnostic, "aliases custody") {
				t.Fatalf("evidence accepted alias %s%s: %d %s", name, suffix, code, diagnostic)
			}
		}
	}
	path := filepath.Join(f.config.Plan.RunDirectory, "vault-link.signed.bin")
	raw, hash, err := readBootstrapRootFile(context.Background(), path, 128*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.config.Plan.Actions[7].signed(raw); err == nil {
		t.Fatal("vault binding signature became evidence CREATE authority")
	}
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.HasPrefix(diagnostic, "signed-byte envelope:") || !strings.Contains(diagnostic, "differs from exact approved envelope") {
		t.Fatalf("wrong evidence signature escaped input rejection: %d %s", code, diagnostic)
	}
	for name, raw := range before {
		after, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatalf("rejected evidence input changed %s: %v", name, err)
		}
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create"); code != 0 || result.Status != "signature-awaiting-import" || result.Attempts != 0 || result.TransactionHash != "" || result.Receipt != nil {
		t.Fatalf("rejected evidence input changed prepared custody: %+v %d %s", result, code, diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--signed-transaction", f.signedPath, "--signed-transaction-hash", f.signedHash); code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != f.tx.Hash().Hex() || result.Attempts != 0 || result.Receipt != nil {
		t.Fatalf("genuine evidence import refused after invalid input: %+v %d %s", result, code, diagnostic)
	}
	if !maps.Equal(counts, f.counts) || len(f.writes) != 7 {
		t.Fatal("evidence custody alias check reached RPC")
	}
}

// Current nonce, runtime and finite send window cannot replace original liability.
func TestEvmEvidenceCreateCurrentAdmissionRetainsOriginalLiability(t *testing.T) {
	for _, fault := range []string{"expiry", "confirmed", "pending", "runtime", "target-code"} {
		f := newEvmEvidenceFixture(t)
		if fault == "expiry" {
			f.config.Plan.ValidThroughNative = 107
			f.publishConfig()
		}
		f.prepareEvidenceSigned()
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
					return "0x8"
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
		result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit")
		if fault == "runtime" || fault == "target-code" {
			if code != 1 {
				t.Fatalf("evidence %s admitted: %d %s", fault, code, diagnostic)
			}
			result, code, diagnostic = f.command("resume", "--action", "evidence-create")
		} else if result.Status != status {
			t.Fatalf("evidence %s status: %+v %d %s", fault, result, code, diagnostic)
		}
		if code != 0 || result.Attempts != 0 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 7 {
			t.Fatalf("evidence %s replaced original liability: %+v %d %s", fault, result, code, diagnostic)
		}
	}
}

// Later runtime changes and expiry cannot erase the original authenticated CREATE.
func TestEvmEvidenceCreateHistoricalReceiptAfterRuntimeChange(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.config.Plan.ValidThroughNative = 108
	f.publishConfig()
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
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
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Status != "evidence-created-unanchored" || result.Receipt == nil || result.Receipt.NativeNumber != 108 || len(f.writes) != 8 {
		t.Fatalf("later runtime erased evidence creation: %+v %d %s", result, code, diagnostic)
	}
}
