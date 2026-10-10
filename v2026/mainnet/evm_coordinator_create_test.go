// Coordinator implementation regressions use genuine geth constructors and
// historical state through the public command. All identities are fixture-only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// All three CREATE envelopes are fixed before the single independent fixture
// approval, including their full graph liability and sequential sender nonces.
func newEvmCoordinatorFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmVaultFixture(t)
	var creation string
	for _, artifact := range evmTestRelease(t).Artifacts {
		if artifact.Name == "Coordinator" {
			creation = artifact.Creation
		}
	}
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "coordinator-create", Sender: f.config.Plan.Actions[0].Sender, Nonce: 2, Data: "0x" + creation, ValueWei: "0", Gas: 7_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "160000000"
	f.publishConfig()
	return f
}

// Both ancestors must complete their real canonical postconditions before the
// fixture selects implementation creation and signs that exact reserved nonce.
func (self *evmCreateFixture) prepareCoordinatorPrerequisites() {
	self.t.Helper()
	self.prepareVaultSigned()
	if _, code, diagnostic := self.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 {
		self.t.Fatal(diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--action", "vault-create", "--online")
	if code != 0 || result.Status != "vault-created" {
		self.t.Fatalf("vault prerequisite: %+v %d %s", result, code, diagnostic)
	}
	plan, err := selectEvmCreatePlan(context.Background(), *self.plan.Reserve, "coordinator-create", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan, self.receipt = plan, nil
	self.signSelectedAction()
}

// Offline child preparation/import retains intent without any new route read.
func (self *evmCreateFixture) prepareCoordinatorSigned() {
	self.t.Helper()
	self.prepareCoordinatorPrerequisites()
	counts := maps.Clone(self.counts)
	result, code, diagnostic := self.command("apply", "--action", "coordinator-create")
	if code != 0 || result.Status != "signature-awaiting-import" || result.CoordinatorAddress != self.plan.Address.Hex() {
		self.t.Fatalf("coordinator preparation: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic = self.command("resume", "--action", "coordinator-create", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() || !maps.Equal(counts, self.counts) {
		self.t.Fatalf("coordinator import: %+v %d %s", result, code, diagnostic)
	}
}

// Exact creation/runtime, eighteen getters and both storage words come from
// independent genuine execution. Ancestor files and completed default outputs
// remain unchanged across offline and online implementation recovery.
func TestEvmCoordinatorCreateExecutesDisabledImplementation(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.prepareCoordinatorSigned()
	before := map[string][]byte{}
	for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile} {
		raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = raw
	}
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	storageReads := 0
	f.override = func(method string, params []any, result any) any {
		if method == "eth_getStorageAt" {
			block := params[2].(map[string]any)
			if params[0] != f.plan.Address.Hex() || block["blockHash"] != f.receipt["blockHash"] || block["requireCanonical"] != true {
				t.Error("implementation storage escaped exact canonical inclusion")
			}
			storageReads++
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit")
	if code != 0 || result.Status != "coordinator-created" || result.Receipt == nil || result.Receipt.RuntimeHash != crypto.Keccak256Hash(f.plan.Runtime).Hex() || result.Receipt.GetterHash != rootObjectHash(f.plan.Getters) || result.Receipt.StorageHash != rootObjectHash(f.plan.Storage) || storageReads != 2 || len(f.plan.Getters) != 18 {
		t.Fatalf("implementation postconditions: %+v %d %s", result, code, diagnostic)
	}
	if result.Address != f.plan.Reserve.Address.Hex() || result.VaultAddress != f.plan.Vault.Address.Hex() || result.CoordinatorAddress != f.plan.Address.Hex() || result.ExecutableAction != "coordinator-create" || result.PlanHash != f.config.Plan.hash() || result.InstallationComplete || result.ActivationReady || len(result.RemainingActions) != 6 || len(f.writes) != 3 || !bytes.Equal(f.writes[2], f.raw) {
		t.Fatalf("implementation escaped approved scope: %+v", result)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "coordinator-create")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || retained.ReceiptObservation != "retained" || !maps.Equal(counts, f.counts) {
		t.Fatalf("offline implementation completion: %+v %d %s", retained, code, diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 || len(f.writes) != 3 {
		t.Fatalf("implementation completion resent: %d %s", code, diagnostic)
	}
	for name, raw := range before {
		after, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatalf("implementation changed historical %s: %v", name, err)
		}
	}
	for _, action := range []string{"reserve-create", "vault-create"} {
		legacy, code, diagnostic := f.command("resume", "--action", action)
		if code != 0 || legacy.CoordinatorAddress != "" || legacy.Receipt == nil || legacy.Receipt.StorageHash != "" {
			t.Fatalf("implementation altered legacy %s result: %+v %d %s", action, legacy, code, diagnostic)
		}
	}
}

// A fresh signature over altered values still cannot change the fixed release
// constructor or the previously ordered deployer/nonce domain.
func TestEvmCoordinatorCreateRejectsConstructorAndNonceChanges(t *testing.T) {
	for _, fault := range []string{"creation", "argument", "nonce", "sender", "target", "value"} {
		f := newEvmCoordinatorFixture(t)
		action := &f.config.Plan.Actions[2]
		switch fault {
		case "creation":
			action.Data = "0x00"
		case "argument":
			action.Data += strings.Repeat("00", 32)
		case "nonce":
			action.Nonce++
		case "sender":
			action.Sender = common.Address{42}
		case "target":
			target := common.Address{42}
			action.To = &target
		case "value":
			action.ValueWei = "1"
			f.config.Plan.MaximumTotalWei = "160000001"
		}
		f.publishConfig()
		if _, code, diagnostic := f.command("plan", "--action", "coordinator-create"); code != 2 || !strings.Contains(diagnostic, "selected action") {
			t.Fatalf("approved %s escaped implementation construction: %d %s", fault, code, diagnostic)
		}
		if len(f.counts) != 0 {
			t.Fatalf("constructor %s reached RPC", fault)
		}
	}
}

// Preview reads no run directory and exports the implementation address/runtime
// plus exact reviewed storage expectations under the unchanged full graph hash.
func TestEvmCoordinatorCreatePreviewExportsFullGraphWithoutCustody(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.config.Signature = ""
	f.config.Plan.RunDirectory = filepath.Join(f.config.Plan.RunDirectory, "future-run")
	raw, err := json.Marshal(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "preview", "--action", "coordinator-create", "--config", f.configPath}, &stdout, &stderr)
	var preview evmPhasePreview
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil || code != 0 {
		t.Fatalf("coordinator preview: %d %v %s", code, err, stderr.String())
	}
	if preview.PlanHash != f.config.Plan.hash() || preview.ExecutableAction != "coordinator-create" || preview.CoordinatorAddress != crypto.CreateAddress(f.config.Plan.Actions[2].Sender, 2).Hex() || !rootCanonicalHash(preview.ExpectedCoordinatorRuntimeHash) || len(preview.CoordinatorStorage) != 2 || preview.CoordinatorStorage[0].Expected != "0x"+strings.Repeat("00", 24)+strings.Repeat("ff", 8) || preview.ApprovalVerified || preview.InstallationComplete {
		t.Fatalf("implementation preview changed approval or disabled-state expectation: %+v", preview)
	}
	if _, err := os.Lstat(f.config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("implementation preview acquired custody: %v", err)
	}
}

// Each getter and storage assertion is isolated after successful real creation.
// Clearing the one fault restores canonical completion without another send.
func TestEvmCoordinatorCreateRejectsEachCanonicalPostcondition(t *testing.T) {
	for fault := -4; fault < 18; fault++ {
		f := newEvmCoordinatorFixture(t)
		f.prepareCoordinatorSigned()
		if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
			t.Fatal(diagnostic)
		}
		f.override = func(method string, params []any, result any) any {
			if fault == -4 && method == "eth_getTransactionReceipt" && params[0] == f.tx.Hash().Hex() {
				changed := maps.Clone(result.(map[string]any))
				changed["contractAddress"] = common.Address{42}.Hex()
				return changed
			}
			if fault == -3 && method == "eth_getCode" && params[0] == f.plan.Address.Hex() {
				return "0x00"
			}
			if (fault == -2 || fault == -1) && method == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() && params[1] == f.plan.Storage[fault+2].Slot {
				return "0x" + strings.Repeat("01", 32)
			}
			if fault >= 0 && method == "eth_call" {
				input := params[0].(map[string]any)
				if input["to"] == f.plan.Address.Hex() && input["data"] == f.plan.Getters[fault].Data {
					return "0x00"
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "differs") && !strings.Contains(diagnostic, "contradicts") {
			t.Fatalf("implementation postcondition %d accepted: %d %s", fault, code, diagnostic)
		}
		retained, code, diagnostic := f.command("resume", "--action", "coordinator-create")
		if code != 0 || retained.Receipt != nil || retained.Attempts != 1 || len(f.writes) != 3 {
			t.Fatalf("implementation false completion %d: %+v %d %s", fault, retained, code, diagnostic)
		}
		f.override = nil
		if result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online"); code != 0 || result.Status != "coordinator-created" {
			t.Fatalf("implementation postcondition recovery %d: %+v %d %s", fault, result, code, diagnostic)
		}
	}
}

// An initializer word of zero permits takeover; it is never interpreted as the
// constructor's disabled uint64-max version. An unavailable archive is separate.
func TestEvmCoordinatorCreateRequiresDisabledInitializerStorage(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		f := newEvmCoordinatorFixture(t)
		f.prepareCoordinatorSigned()
		if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
			t.Fatal(diagnostic)
		}
		f.override = func(method string, params []any, result any) any {
			if method == "eth_getStorageAt" && params[1] == f.plan.Storage[0].Slot {
				if unavailable {
					return mappingFixtureRpcError{code: -32602}
				}
				return "0x" + strings.Repeat("00", 32)
			}
			return result
		}
		_, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online")
		if code != 1 || unavailable == strings.Contains(diagnostic, "constructor storage differs") {
			t.Fatalf("disabled-state mismatch and archive error collapsed: %t %d %s", unavailable, code, diagnostic)
		}
		f.override = nil
		if result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online"); code != 0 || result.Status != "coordinator-created" || len(f.writes) != 3 {
			t.Fatalf("initializer observation did not recover: %+v %d %s", result, code, diagnostic)
		}
	}
}

// Included but unauthenticated vault work cannot open implementation custody.
func TestEvmCoordinatorCreateRequiresCompletedVaultJournal(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	refused := func(stage string) {
		t.Helper()
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("apply", "--action", "coordinator-create"); code != 3 {
			t.Fatalf("%s vault admitted coordinator: %d %s", stage, code, diagnostic)
		}
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmCoordinatorCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) || !maps.Equal(counts, f.counts) {
			t.Fatalf("%s vault acquired implementation custody: %v", stage, err)
		}
	}
	refused("absent")
	f.prepareVaultSigned()
	refused("signed")
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	refused("included")
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("apply", "--action", "coordinator-create"); code != 0 || result.Status != "signature-awaiting-import" {
		t.Fatalf("completed vault did not unlock exact implementation: %+v %d %s", result, code, diagnostic)
	}
}

// Every ancestor is re-audited, not merely the immediate vault predecessor.
func TestEvmCoordinatorCreateReauditsBothPredecessors(t *testing.T) {
	for _, ancestor := range []int{0, 1} {
		f := newEvmCoordinatorFixture(t)
		f.prepareCoordinatorSigned()
		address := f.plan.priorPlan(ancestor).Address.Hex()
		path := filepath.Join(f.config.Plan.RunDirectory, evmCoordinatorCreateStateFile)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f.override = func(method string, params []any, result any) any {
			if method == "eth_call" && params[0].(map[string]any)["to"] == address {
				return "0x00"
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "constructor getter differs") {
			t.Fatalf("ancestor %d bypassed re-audit: %d %s", ancestor, code, diagnostic)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) || len(f.writes) != 2 {
			t.Fatalf("ancestor %d changed implementation custody: %v", ancestor, err)
		}
		f.override = nil
		if result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
			t.Fatalf("ancestor %d recovery: %+v %d %s", ancestor, result, code, diagnostic)
		}
	}
}

// After both receipt audits, a changed predecessor checkpoint must still stop
// fresh implementation admission even though the approved start hash is shared.
func TestEvmCoordinatorCreateFencesBothPredecessorCheckpoints(t *testing.T) {
	for _, number := range []uint64{101, 102} {
		f := newEvmCoordinatorFixture(t)
		f.prepareCoordinatorSigned()
		originalHash := f.hashes[number]
		original := f.headers[originalHash]
		fork, forkHash := evmTestNativeHeader(t, original.ParentHash, number, append(append([]string(nil), original.Digest.Logs...), "0x0000"))
		f.headers[forkHash] = fork
		gettersObserved, changed := false, false
		f.override = func(method string, params []any, result any) any {
			if method == "eth_call" {
				input := params[0].(map[string]any)
				gettersObserved = gettersObserved || input["to"] == f.plan.Vault.Address.Hex() && input["data"] == f.plan.Vault.Getters[len(f.plan.Vault.Getters)-1].Data
			}
			if method == "chain_getFinalizedHead" && gettersObserved && !changed {
				changed = true
				f.hashes[number] = forkHash
				return f.hashes[f.head]
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 1 || !changed || !strings.Contains(diagnostic, "canonical native ancestry changed") || len(f.writes) != 2 {
			t.Fatalf("checkpoint %d escaped implementation admission: %d %s", number, code, diagnostic)
		}
		f.override = nil
		f.hashes[number] = originalHash
		if result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
			t.Fatalf("checkpoint %d recovery: %+v %d %s", number, result, code, diagnostic)
		}
	}
}

// A lost HTTP reply retains the original third nonce and attempt; canonical
// recovery finds the same executed implementation without broadcasting again.
func TestEvmCoordinatorCreateLostReplyRecoversOriginalIntent(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.prepareCoordinatorSigned()
	f.loseReply = true
	if _, code, _ := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 1 || len(f.writes) != 3 {
		t.Fatalf("uncertain implementation was acknowledged or retried: %d", code)
	}
	retained, code, diagnostic := f.command("resume", "--action", "coordinator-create")
	if code != 0 || retained.Attempts != 1 || retained.Receipt != nil || retained.TransactionHash != f.tx.Hash().Hex() {
		t.Fatalf("uncertain implementation liability lost: %+v %d %s", retained, code, diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit")
	if code != 0 || result.Status != "coordinator-created" || result.Attempts != retained.Attempts || result.TransactionHash != retained.TransactionHash || len(f.writes) != 3 || !bytes.Equal(f.writes[2], f.raw) {
		t.Fatalf("uncertain implementation acquired replacement authority: %+v %d %s", result, code, diagnostic)
	}
}

// Two predecessor sends have already spent two of the original three attempts.
func TestEvmCoordinatorCreateKeepsCumulativeGraphAttempts(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.config.Plan.MaximumAttempts = 3
	f.publishConfig()
	f.prepareCoordinatorSigned()
	f.mine = false
	for attempt := 0; attempt < 3; attempt++ {
		result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit")
		if code != 0 || result.Attempts != 1 || attempt > 0 && result.Status != "attempt-allowance-exhausted" {
			t.Fatalf("implementation restarted original allowance: %+v %d %s", result, code, diagnostic)
		}
	}
	if len(f.writes) != 3 {
		t.Fatal("implementation exceeded cumulative graph attempts")
	}
}

// A later same-sender value reservation remains funded without implementing
// that action. Gas-only checks would silently spend the escrow reservation.
func TestEvmCoordinatorCreatePreservesFutureValueAndGasFunding(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	target := crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 1)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "escrow-register", Sender: f.config.Plan.Actions[0].Sender, Nonce: 3, To: &target, Data: "0x00", ValueWei: "5000000", Gas: 1_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "175000000"
	f.publishConfig()
	f.prepareCoordinatorSigned()
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 80_000_000)
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "maximum liability") || len(f.writes) != 2 {
		t.Fatalf("implementation spent later value reservation: %d %s", code, diagnostic)
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("restored implementation funding: %+v %d %s", result, code, diagnostic)
	}
}

// Failed durable attempt acknowledgement poisons the owner before transport.
// Reopen counts that attempt alongside both already-spent predecessor attempts.
func TestEvmCoordinatorCreateAmbiguousAttemptPublication(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.prepareCoordinatorSigned()
	f.mine = false
	reserveStore, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer reserveStore.close()
	reserve, err := reserveStore.load()
	if err != nil {
		t.Fatal(err)
	}
	vaultStore, err := openEvmVaultActionStore(*f.plan.Vault, reserve, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer vaultStore.close()
	vault, err := vaultStore.load()
	if err != nil {
		t.Fatal(err)
	}
	store, err := openEvmCoordinatorActionStore(f.plan, reserve, vault, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmCoordinatorCreateOwner(f.plan, store, reserveStore, vaultStore, chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 2 {
			return errors.New("synthetic implementation attempt sync interruption")
		}
		return nil
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("implementation acknowledged ambiguous attempt publication")
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != 2 {
		t.Fatal("poisoned implementation owner reached transport")
	}
	store.close()
	vaultStore.close()
	reserveStore.close()
	result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit")
	if code != 0 || result.Attempts != 2 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 3 {
		t.Fatalf("implementation reopen refreshed cumulative attempts: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic = f.command("resume", "--action", "coordinator-create", "--online", "--submit")
	if code != 0 || result.Status != "attempt-allowance-exhausted" || result.Attempts != 2 || len(f.writes) != 3 {
		t.Fatalf("ambiguous publication exceeded graph budget: %+v %d %s", result, code, diagnostic)
	}
}

// Both exact initial-claim boundaries recover. A missing completed child never
// becomes a new allowance, and all predecessor locks remain independently held.
func TestEvmCoordinatorCreateClaimRecoveryKeepsAllCustody(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "record-synced"} {
		f := newEvmCoordinatorFixture(t)
		f.prepareCoordinatorPrerequisites()
		reserveStore, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		reserve, err := reserveStore.load()
		if err != nil {
			t.Fatal(err)
		}
		vaultStore, err := openEvmVaultActionStore(*f.plan.Vault, reserve, false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		vault, err := vaultStore.load()
		if err != nil {
			t.Fatal(err)
		}
		store, err := openEvmCoordinatorActionStore(f.plan, reserve, vault, true, func(stage string) error {
			if stage == boundary {
				return errors.New("synthetic implementation claim interruption")
			}
			return nil
		}, f.storage.Context)
		if err == nil || store != nil {
			t.Fatalf("implementation %s interruption was acknowledged", boundary)
		}
		store, err = openEvmCoordinatorActionStore(f.plan, reserve, vault, false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		if err != nil || record.Signed != "" || record.Attempts != 0 || record.PredecessorHash != rootObjectHash(vault) {
			t.Fatalf("implementation claim recovery altered intent: %+v %v", record, err)
		}
		if other, err := openEvmActionStore(f.config, false, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("implementation lost reserve lock")
		}
		if other, err := openEvmVaultActionStore(*f.plan.Vault, reserve, false, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("implementation lost vault lock")
		}
		if other, err := openEvmCoordinatorActionStore(f.plan, reserve, vault, false, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("second implementation owner acquired custody")
		}
		store.close()
		if err := os.Remove(filepath.Join(f.config.Plan.RunDirectory, evmCoordinatorCreateStateFile)); err != nil {
			t.Fatal(err)
		}
		if other, err := openEvmCoordinatorActionStore(f.plan, reserve, vault, false, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("missing implementation child became fresh authority")
		}
		if other, err := openEvmCoordinatorActionStore(f.plan, reserve, vault, true, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("apply bypassed completed implementation marker")
		}
		vaultStore.close()
		reserveStore.close()
	}
}

// Both a changed reserve and a changed vault differ from the transitively
// sealed lineage even when each modified record has a self-consistent hash.
func TestEvmCoordinatorCreateRejectsChangedPredecessorLineage(t *testing.T) {
	for _, ancestor := range []int{0, 1} {
		f := newEvmCoordinatorFixture(t)
		f.prepareCoordinatorSigned()
		reserveStore, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		reserve, err := reserveStore.load()
		if err != nil {
			t.Fatal(err)
		}
		store := reserveStore
		if ancestor == 1 {
			store, err = openEvmVaultActionStore(*f.plan.Vault, reserve, false, nil, f.storage.Context)
			if err != nil {
				t.Fatal(err)
			}
		}
		record, err := store.load()
		if err != nil {
			t.Fatal(err)
		}
		record.Attempts++
		record.ContentHash = ""
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			t.Fatal(err)
		}
		store.close()
		reserveStore.close()
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 3 || !strings.Contains(diagnostic, "marker differs") {
			t.Fatalf("implementation learned changed ancestor %d: %d %s", ancestor, code, diagnostic)
		}
		if !maps.Equal(counts, f.counts) || len(f.writes) != 2 {
			t.Fatal("changed lineage reached implementation RPC")
		}
	}
}

// A genuine implementation gas failure retains a consumed nonce without any
// runtime, getter or disabled-storage completion claim, including offline reopen.
func TestEvmCoordinatorCreateRevertRetainsConsumedNonce(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.config.Plan.Actions[2].Gas = 200_000
	f.publishConfig()
	f.prepareCoordinatorSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit")
	if code != 0 || result.Status != "create-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 || result.Receipt.RuntimeHash != "" || result.Receipt.GetterHash != "" || result.Receipt.StorageHash != "" || len(f.writes) != 3 {
		t.Fatalf("implementation revert lost canonical nonce outcome: %+v %d %s", result, code, diagnostic)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "coordinator-create")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || !maps.Equal(counts, f.counts) {
		t.Fatalf("offline implementation revert changed state: %+v %d %s", retained, code, diagnostic)
	}
}

// A consumed but reverted vault nonce is not a successful prerequisite for the
// separately reserved implementation address, regardless of the next nonce.
func TestEvmCoordinatorCreateRefusesRevertedVault(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.config.Plan.Actions[1].Gas = 200_000
	f.publishConfig()
	f.prepareVaultSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online"); code != 0 || result.Status != "create-reverted-nonce-consumed" {
		t.Fatalf("vault did not genuinely revert: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("apply", "--action", "coordinator-create"); code != 3 || !strings.Contains(diagnostic, "successful vault") {
		t.Fatalf("reverted vault admitted implementation: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmCoordinatorCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reverted vault opened child: %v", err)
	}
}

// Output failure happens after authoritative child receipt publication. Offline
// recovery preserves completion even though the convenience output was absent.
func TestEvmCoordinatorCreateOutputFailureRetainsCompletion(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.prepareCoordinatorSigned()
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	args := []string{"bootstrap-contracts", "resume", "--action", "coordinator-create", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--online", "--submit"}
	var stderr bytes.Buffer
	if code := runMain(f.storageContext(context.Background()), args, bootstrapRootFailedWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output failed") {
		t.Fatalf("implementation output failure occurred before retention: %d %s", code, stderr.String())
	}
	if result, code, diagnostic := f.command("resume", "--action", "coordinator-create"); code != 0 || result.Status != "coordinator-created" || result.Receipt == nil || result.Receipt.StorageHash == "" || len(f.writes) != 3 {
		t.Fatalf("output failure lost implementation completion: %+v %d %s", result, code, diagnostic)
	}
}

// Existing signatures and journal paths belong to their selected actions.
func TestEvmCoordinatorCreateRejectsVaultSignatureAndCustodyAlias(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.prepareCoordinatorPrerequisites()
	if _, code, diagnostic := f.command("apply", "--action", "coordinator-create"); code != 0 {
		t.Fatal(diagnostic)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, "vault-create.signed.bin")
	_, hash, err := readBootstrapRootFile(context.Background(), path, 128*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "signed-byte envelope") {
		t.Fatalf("vault signature populated implementation: %d %s", code, diagnostic)
	}
	path = filepath.Join(f.config.Plan.RunDirectory, evmCoordinatorCreateStateFile)
	_, hash, err = readBootstrapRootFile(context.Background(), path, 128*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "aliases custody") {
		t.Fatalf("implementation signed input aliased journal: %d %s", code, diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "coordinator-create"); code != 0 || result.Status != "signature-awaiting-import" || result.Attempts != 0 {
		t.Fatalf("wrong implementation input changed intent: %+v %d %s", result, code, diagnostic)
	}
}

// A later runtime cannot erase canonical construction under the original
// historical profile, even after the finite local send window has closed.
func TestEvmCoordinatorCreateHistoricalCompletionAfterRuntimeChange(t *testing.T) {
	f := newEvmCoordinatorFixture(t)
	f.config.Plan.ValidThroughNative = 103
	f.publishConfig()
	f.prepareCoordinatorSigned()
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
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
	if result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 || result.Status != "coordinator-created" || result.Receipt == nil || result.Receipt.NativeNumber != 103 || len(f.writes) != 3 {
		t.Fatalf("later runtime erased implementation receipt: %+v %d %s", result, code, diagnostic)
	}
}

// Expiry and nonce movement preserve the signed implementation liability while
// refusing both replacements and another action's remaining send allowance.
func TestEvmCoordinatorCreateExpiryAndNonceMovementRetainLiability(t *testing.T) {
	for _, fault := range []string{"expiry", "confirmed", "pending"} {
		f := newEvmCoordinatorFixture(t)
		if fault == "expiry" {
			f.config.Plan.ValidThroughNative = 102
			f.publishConfig()
		}
		f.prepareCoordinatorSigned()
		status := "approval-expired-signed-liability-retained"
		if fault == "expiry" {
			f.advanceEmpty()
		} else {
			status = "nonce-consumed-receipt-unresolved"
			if fault == "pending" {
				status = "pending-nonce-unresolved"
			}
			f.override = func(method string, params []any, result any) any {
				if method == "eth_getTransactionCount" && (fault == "confirmed" || params[1] == "pending") {
					return "0x3"
				}
				return result
			}
		}
		result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit")
		if code != 0 || result.Status != status || result.Attempts != 0 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 2 {
			t.Fatalf("implementation %s replaced original liability: %+v %d %s", fault, result, code, diagnostic)
		}
	}
}
