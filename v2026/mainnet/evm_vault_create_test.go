// Vault regressions cross the real command, private journals, owned local HTTP,
// and genuine release constructors. No fixture signature authorizes live work.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Both envelopes exist before the one fixture approval. Historical state is
// captured independently by geth and later served at exact inclusion hashes.
func newEvmVaultFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	f := newEvmCreateFixture(t)
	var vault contractReleaseArtifact
	for _, artifact := range evmTestRelease(t).Artifacts {
		if artifact.Name == "SettlementVault" {
			vault = artifact
		}
	}
	action := f.config.Plan.Actions[0]
	data, _, _, err := contractVaultPayload(vault, 25, [32]byte{32}, 7200, 1_000_000, action.Sender, action.Nonce+1)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "vault-create", Sender: action.Sender, Nonce: action.Nonce + 1, Data: "0x" + hex.EncodeToString(data), ValueWei: "0", Gas: 7_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "90000000"
	f.config.Plan.MaximumAttempts = 4
	f.publishConfig()
	f.history = &evmCreateHistory{receipts: map[string]map[string]any{}, transactions: map[string]*types.Transaction{}, states: map[string]*state.StateDB{}, storage: map[string]map[string]string{}}
	for hash := range f.evmHeaders {
		f.history.states[hash] = f.state.Copy()
	}
	return f
}

// This signer is fixture-only and signs exactly the selected approved action.
// Exported inputs remain with the original configuration when a composed
// fixture assigns a separate runtime root. The runtime owns its journal copy.
func (self *evmCreateFixture) signSelectedAction() {
	self.t.Helper()
	private, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		self.t.Fatal(err)
	}
	action := self.config.Plan.Actions[self.plan.ActionIndex]
	unsigned, err := action.unsigned()
	if err != nil {
		self.t.Fatal(err)
	}
	self.tx, err = types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(964)), private)
	if err != nil {
		self.t.Fatal(err)
	}
	self.raw, err = self.tx.MarshalBinary()
	if err != nil {
		self.t.Fatal(err)
	}
	self.signedPath = filepath.Join(filepath.Dir(self.configPath), action.Id+".signed.bin")
	if err := os.WriteFile(self.signedPath, self.raw, 0600); err != nil {
		self.t.Fatal(err)
	}
	digest := sha256.Sum256(self.raw)
	self.signedHash = "sha256:" + hex.EncodeToString(digest[:])
	self.vm.GasLimit = action.Gas
}

// Reserve completion must pass actual canonical runtime/getter authentication
// before the fixture switches its active constructor to the vault reservation.
func (self *evmCreateFixture) prepareVaultPrerequisite() {
	self.t.Helper()
	self.prepareSigned()
	if _, code, diagnostic := self.command("resume", "--online", "--submit"); code != 0 {
		self.t.Fatal(diagnostic)
	}
	result, code, diagnostic := self.command("resume", "--online")
	if code != 0 || result.Status != "reserve-created" {
		self.t.Fatalf("reserve prerequisite: %+v %d %s", result, code, diagnostic)
	}
	plan, err := selectEvmCreatePlan(context.Background(), self.plan, "vault-create", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan = plan
	self.receipt = nil
	self.signSelectedAction()
}

// Vault preparation/import is offline even though its reserve was observed
// online previously. Command flags select the child without changing approval.
func (self *evmCreateFixture) prepareVaultSigned() {
	self.t.Helper()
	self.prepareVaultPrerequisite()
	self.stateLock.Lock()
	counts := maps.Clone(self.counts)
	self.stateLock.Unlock()
	result, code, diagnostic := self.command("apply", "--action", "vault-create")
	if code != 0 || result.Status != "signature-awaiting-import" || result.ExecutableAction != "vault-create" || result.VaultAddress != self.plan.Address.Hex() {
		self.t.Fatalf("vault preparation: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic = self.command("resume", "--action", "vault-create", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() {
		self.t.Fatalf("vault signed custody: %+v %d %s", result, code, diagnostic)
	}
	self.stateLock.Lock()
	unchanged := maps.Equal(counts, self.counts)
	self.stateLock.Unlock()
	if !unchanged {
		self.t.Fatal("offline vault custody reached the RPC route")
	}
}

// Independent full execution checks all thirteen getters and keeps the reserve
// file unchanged. Completed offline/online resumes never resend either CREATE.
func TestEvmVaultCreateCommandExecutesAndRetainsBothContracts(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultSigned()
	reservePath := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	reserveBefore, err := os.ReadFile(reservePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeCalls := f.counts["eth_call"]
	result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Status != "submitted-awaiting-canonical-receipt" || result.Attempts != 1 {
		t.Fatalf("vault send: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic = f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Status != "vault-created" || result.Receipt == nil || result.Receipt.RuntimeHash != crypto.Keccak256Hash(f.plan.Runtime).Hex() || result.Receipt.GetterHash != rootObjectHash(f.plan.Getters) || result.ReceiptObservation != "revalidated-online" {
		t.Fatalf("vault canonical completion: %+v %d %s", result, code, diagnostic)
	}
	if result.Address != f.plan.Reserve.Address.Hex() || result.VaultAddress != f.plan.Address.Hex() || result.PlanHash != f.config.Plan.hash() || result.InstallationComplete || result.ActivationReady || len(result.RemainingActions) != 7 || len(f.writes) != 2 || !bytes.Equal(f.writes[1], f.raw) {
		t.Fatalf("vault changed graph authority or completion scope: %+v", result)
	}
	if len(f.plan.Getters) != 13 || f.counts["eth_call"]-beforeCalls != 2*len(f.plan.Reserve.Getters)+13 {
		t.Fatalf("vault omitted prerequisite or constructor getters: %v", f.counts)
	}
	vaultPath := filepath.Join(f.config.Plan.RunDirectory, evmVaultCreateStateFile)
	vaultBefore, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "vault-create")
	if code != 0 || retained.Status != "vault-created" || retained.ReceiptObservation != "retained" || retained.Receipt == nil || *retained.Receipt != *result.Receipt || !maps.Equal(counts, f.counts) {
		t.Fatalf("vault offline completion lost historical evidence: %+v %d %s", retained, code, diagnostic)
	}
	vaultAfter, err := os.ReadFile(vaultPath)
	if err != nil || !bytes.Equal(vaultBefore, vaultAfter) {
		t.Fatalf("offline vault completion rewrote custody: %v", err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 || len(f.writes) != 2 {
		t.Fatalf("completed vault was resent: %d %s", code, diagnostic)
	}
	reserveAfter, err := os.ReadFile(reservePath)
	if err != nil || !bytes.Equal(reserveBefore, reserveAfter) {
		t.Fatalf("vault progress rewrote the original reserve journal: %v", err)
	}
}

// A lost acknowledgement can execute the vault. Reopen authenticates that
// original inclusion instead of allocating another nonce, signature or attempt.
func TestEvmVaultCreateLostReplyRecoversOriginalInclusion(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultSigned()
	f.loseReply = true
	if _, code, _ := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 1 || len(f.writes) != 2 {
		t.Fatalf("lost vault reply was retried or acknowledged: %d", code)
	}
	retained, code, diagnostic := f.command("resume", "--action", "vault-create")
	if code != 0 || retained.Attempts != 1 || retained.TransactionHash != f.tx.Hash().Hex() || retained.Receipt != nil {
		t.Fatalf("uncertain vault liability disappeared: %+v %d %s", retained, code, diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Status != "vault-created" || result.Attempts != retained.Attempts || result.TransactionHash != retained.TransactionHash || len(f.writes) != 2 || !bytes.Equal(f.writes[1], f.raw) {
		t.Fatalf("uncertain vault recovery changed original intent: %+v %d %s", result, code, diagnostic)
	}
}

// Chain inclusion alone cannot authorize the next local journal. The completed
// reserve record must contain the canonical code/getter postconditions first.
func TestEvmVaultCreateRequiresRetainedReserveCompletion(t *testing.T) {
	f := newEvmVaultFixture(t)
	assertRefused := func(stage string) {
		t.Helper()
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("apply", "--action", "vault-create"); code != 3 {
			t.Fatalf("%s reserve admitted vault custody: %d %s", stage, code, diagnostic)
		}
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmVaultCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) || !maps.Equal(counts, f.counts) {
			t.Fatalf("%s refusal acquired vault custody or RPC: %v", stage, err)
		}
	}
	assertRefused("absent")
	if _, code, diagnostic := f.command("apply"); code != 0 {
		t.Fatal(diagnostic)
	}
	assertRefused("unsigned")
	if _, code, diagnostic := f.command("resume", "--signed-transaction", f.signedPath, "--signed-transaction-hash", f.signedHash); code != 0 {
		t.Fatal(diagnostic)
	}
	assertRefused("signed")
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	assertRefused("included but unauthenticated")
	if _, code, diagnostic := f.command("resume", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("apply", "--action", "vault-create"); code != 0 || result.Status != "signature-awaiting-import" {
		t.Fatalf("successful prerequisite did not unlock exact vault: %+v %d %s", result, code, diagnostic)
	}
}

// Each changed constructor field is independently approved by the fixture so
// failure exercises semantic construction, not an unrelated signature check.
func TestEvmVaultCreateRejectsChangedConstructorAndNonceDomain(t *testing.T) {
	for _, fault := range []string{"netuid", "zero-hotkey", "reserve-hotkey", "coldkey", "zero-ttl", "wide-ttl", "zero-transfer", "wide-transfer", "bootstrap", "creation", "trailing", "target", "value", "nonce", "sender"} {
		f := newEvmVaultFixture(t)
		action := &f.config.Plan.Actions[1]
		data, err := hex.DecodeString(action.Data[2:])
		if err != nil {
			t.Fatal(err)
		}
		start := len(data) - 6*32
		switch fault {
		case "netuid":
			data[start+31]++
		case "zero-hotkey":
			clear(data[start+32 : start+64])
		case "reserve-hotkey":
			copy(data[start+32:start+64], common.FromHex(f.config.Plan.ReserveHotkey))
		case "coldkey":
			data[start+64] ^= 1
		case "zero-ttl":
			clear(data[start+96 : start+128])
		case "wide-ttl":
			data[start+96] = 1
		case "zero-transfer":
			clear(data[start+128 : start+160])
		case "wide-transfer":
			data[start+128] = 1
		case "bootstrap":
			data[start+191] ^= 1
		case "creation":
			data[0] ^= 1
		case "trailing":
			data = append(data, 0)
		case "target":
			target := common.Address{42}
			action.To = &target
		case "value":
			action.ValueWei = "1"
			f.config.Plan.MaximumTotalWei = "90000001"
		case "nonce":
			action.Nonce++
		case "sender":
			action.Sender = common.Address{42}
		}
		action.Data = "0x" + hex.EncodeToString(data)
		f.publishConfig()
		if _, code, diagnostic := f.command("plan", "--action", "vault-create"); code != 2 || !strings.Contains(diagnostic, "selected action") {
			t.Fatalf("approved %s escaped vault semantic admission: %d %s", fault, code, diagnostic)
		}
		if len(f.counts) != 0 {
			t.Fatalf("constructor %s reached RPC", fault)
		}
	}
}

// The approval payload is still the unchanged whole graph. Preview exposes
// decoded vault values and both addresses without inspecting the future run dir.
func TestEvmVaultCreatePreviewExportsSameGraphAndConstructor(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.config.Signature = ""
	f.config.Plan.RunDirectory = filepath.Join(f.config.Plan.RunDirectory, "absent-run")
	raw, err := json.Marshal(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "preview", "--config", f.configPath, "--action", "vault-create"}, &stdout, &stderr)
	var preview evmPhasePreview
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil || code != 0 {
		t.Fatalf("vault preview: %d %v %s", code, err, stderr.String())
	}
	message, err := f.config.Plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	if preview.ApprovalSigningMessageHex != hex.EncodeToString(message) || preview.PlanHash != f.config.Plan.hash() || preview.ExecutableAction != "vault-create" || preview.ApprovalVerified || preview.InstallationComplete || preview.VaultConstructor == nil || preview.VaultConstructor.MinimumClaimTtlBlocks != 7200 || preview.VaultConstructor.MinimumTransferTaoRao != 1_000_000 || preview.ReserveAddress == preview.VaultAddress || !rootCanonicalHash(preview.ExpectedVaultRuntimeHash) {
		t.Fatalf("vault preview changed authority or omitted semantic values: %+v", preview)
	}
	if _, err := os.Lstat(f.config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("vault preview acquired custody or RPC: %v", err)
	}
}

// Every successful postcondition comes from the exact vault inclusion. Each
// getter fault is isolated after genuine constructor execution and before save.
func TestEvmVaultCreateRejectsEachCanonicalPostcondition(t *testing.T) {
	for fault := -2; fault < 13; fault++ {
		f := newEvmVaultFixture(t)
		f.prepareVaultSigned()
		if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 {
			t.Fatal(diagnostic)
		}
		f.override = func(method string, params []any, result any) any {
			if fault == -2 && method == "eth_getTransactionReceipt" && params[0] == f.tx.Hash().Hex() {
				changed := maps.Clone(result.(map[string]any))
				changed["contractAddress"] = common.Address{42}.Hex()
				return changed
			}
			if fault == -1 && method == "eth_getCode" && params[0] == f.plan.Address.Hex() {
				return "0x00"
			}
			if fault >= 0 && method == "eth_call" {
				input := params[0].(map[string]any)
				if input["to"] == f.plan.Address.Hex() && input["data"] == f.plan.Getters[fault].Data {
					return "0x00"
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "differs") && !strings.Contains(diagnostic, "contradicts") {
			t.Fatalf("postcondition %d accepted: %d %s", fault, code, diagnostic)
		}
		retained, code, diagnostic := f.command("resume", "--action", "vault-create")
		if code != 0 || retained.Receipt != nil || retained.Attempts != 1 || len(f.writes) != 2 {
			t.Fatalf("postcondition %d persisted false completion: %+v %d %s", fault, retained, code, diagnostic)
		}
		f.override = nil
		if recovered, code, diagnostic := f.command("resume", "--action", "vault-create", "--online"); code != 0 || recovered.Status != "vault-created" {
			t.Fatalf("postcondition %d recovery: %+v %d %s", fault, recovered, code, diagnostic)
		}
	}
}

// A changed reserve observation fails before vault custody mutation or a send.
// Removing the fault proves the refusal is caused by the prerequisite audit.
func TestEvmVaultCreateReauditsReserveBeforeSubmission(t *testing.T) {
	for _, fault := range []string{"receipt", "runtime", "getter"} {
		f := newEvmVaultFixture(t)
		f.prepareVaultSigned()
		vaultPath := filepath.Join(f.config.Plan.RunDirectory, evmVaultCreateStateFile)
		before, err := os.ReadFile(vaultPath)
		if err != nil {
			t.Fatal(err)
		}
		f.override = func(method string, params []any, result any) any {
			if fault == "receipt" && method == "eth_getTransactionReceipt" && params[0] != f.tx.Hash().Hex() {
				return nil
			}
			if fault == "runtime" && method == "eth_getCode" && params[0] == f.plan.Reserve.Address.Hex() {
				return "0x00"
			}
			if fault == "getter" && method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Reserve.Address.Hex() {
				return "0x00"
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 1 {
			t.Fatalf("changed reserve %s allowed vault send: %d %s", fault, code, diagnostic)
		}
		after, err := os.ReadFile(vaultPath)
		if err != nil || !bytes.Equal(before, after) || len(f.writes) != 1 {
			t.Fatalf("changed reserve %s mutated vault liability: %v", fault, err)
		}
		f.override = nil
		if result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 || result.Attempts != 1 || len(f.writes) != 2 {
			t.Fatalf("reserve %s fault recovery: %+v %d %s", fault, result, code, diagnostic)
		}
	}
}

// The conservative phase allowance covers reserve and vault attempts together;
// reopening the child cannot mint another allowance after an uncertain send.
func TestEvmVaultCreateRetainsOriginalGraphAttemptAllowance(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.config.Plan.MaximumAttempts = 2
	f.publishConfig()
	f.prepareVaultSigned()
	f.mine = false
	for attempt := 0; attempt < 3; attempt++ {
		result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
		if code != 0 || result.Attempts != 1 {
			t.Fatalf("vault attempt %d: %+v %d %s", attempt, result, code, diagnostic)
		}
		if attempt > 0 && result.Status != "attempt-allowance-exhausted" {
			t.Fatalf("vault restart refreshed graph allowance: %+v", result)
		}
	}
	if len(f.writes) != 2 || !bytes.Equal(f.writes[1], f.raw) {
		t.Fatal("vault exceeded original graph attempts")
	}
}

// Future reservations still own their maximum value/fee funding even though
// this command cannot execute their semantics or sign their envelopes.
func TestEvmVaultCreatePreservesLaterFundingReservation(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "coordinator-create", Sender: f.config.Plan.Actions[0].Sender, Nonce: 2, Data: "0x00", ValueWei: "0", Gas: 2_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "110000000"
	f.publishConfig()
	f.prepareVaultSigned()
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 70_000_000)
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "maximum liability") || len(f.writes) != 1 {
		t.Fatalf("vault spent later funding reservation: %d %s", code, diagnostic)
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("vault restored funding: %+v %d %s", result, code, diagnostic)
	}
}

// A directory-sync interruption after publishing the consumed attempt poisons
// that owner; a fresh one keeps both the original bytes and spent graph budget.
func TestEvmVaultCreateAmbiguousPublicationPoisonsOwner(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultSigned()
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
	store, err := openEvmVaultActionStore(f.plan, reserve, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmVaultCreateOwner(f.plan, store, reserveStore, chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 2 {
			return errors.New("synthetic vault attempt directory-sync interruption")
		}
		return nil
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("vault attempt sync failure was acknowledged")
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != 1 {
		t.Fatal("poisoned vault owner sent or reused custody")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if err := reserveStore.close(); err != nil {
		t.Fatal(err)
	}
	result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Attempts != 2 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 2 {
		t.Fatalf("vault reopen discarded ambiguous attempt: %+v %d %s", result, code, diagnostic)
	}
}

// Both pre-signature interruption boundaries recover only their untouched state.
// Neither a copied reserve marker nor a lost completed child renews authority.
func TestEvmVaultCreateClaimRecoveryAndLostChild(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "record-synced"} {
		f := newEvmVaultFixture(t)
		f.prepareVaultPrerequisite()
		reserveStore, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		reserve, err := reserveStore.load()
		if err != nil {
			t.Fatal(err)
		}
		store, err := openEvmVaultActionStore(f.plan, reserve, true, func(stage string) error {
			if stage == boundary {
				return errors.New("synthetic vault claim interruption")
			}
			return nil
		}, f.storage.Context)
		if err == nil || store != nil {
			t.Fatalf("vault %s interruption acknowledged", boundary)
		}
		store, err = openEvmVaultActionStore(f.plan, reserve, false, nil, f.storage.Context)
		if err != nil {
			t.Fatalf("vault %s claim recovery: %v", boundary, err)
		}
		record, err := store.load()
		if err != nil || record.Signed != "" || record.Attempts != 0 || record.PredecessorHash != rootObjectHash(reserve) {
			t.Fatalf("vault initial recovery changed custody: %+v %v", record, err)
		}
		if other, err := openEvmActionStore(f.config, false, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("vault prerequisite lock was not held")
		}
		if other, err := openEvmVaultActionStore(f.plan, reserve, false, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("second vault owner acquired custody")
		}
		store.close()
		if err := os.Remove(filepath.Join(f.config.Plan.RunDirectory, evmVaultCreateStateFile)); err != nil {
			t.Fatal(err)
		}
		if other, err := openEvmVaultActionStore(f.plan, reserve, false, nil, f.storage.Context); err == nil {
			other.close()
			t.Fatal("missing completed vault journal became fresh allowance")
		}
		reserveStore.close()
	}
}

// Preserve the original reserve wire order, absent-field rules, marker bytes,
// content hash and offline reopen behavior, including existing multi-action plans.
func TestEvmVaultCreatePreservesLegacyReserveJournalEncoding(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareSigned()
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		Schema          string            `json:"schema"`
		ConfigHash      string            `json:"config_hash"`
		Signed          string            `json:"signed_transaction,omitempty"`
		TransactionHash string            `json:"transaction_hash,omitempty"`
		Attempts        uint8             `json:"attempts"`
		ScanNumber      uint64            `json:"scan_number"`
		ScanHash        string            `json:"scan_hash"`
		Receipt         *evmCreateReceipt `json:"receipt,omitempty"`
		ContentHash     string            `json:"content_hash"`
	}
	if err := decodePlanJson(raw, &old); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(old)
	if err != nil || !bytes.Equal(raw, append(encoded, '\n')) {
		t.Fatalf("legacy reserve JSON changed: %v", err)
	}
	claimed := old.ContentHash
	old.ContentHash = ""
	if claimed != rootObjectHash(old) {
		t.Fatal("legacy reserve hash changed")
	}
	marker, err := os.ReadFile(path + ".lock")
	if err != nil || string(marker) != rootObjectHash(f.config)+"\n"+bootstrapRootClaimComplete {
		t.Fatalf("legacy reserve marker changed: %v", err)
	}
	if result, code, diagnostic := f.command("resume"); code != 0 || result.VaultAddress != "" || result.ExecutableAction != "" || len(result.RemainingActions) != 8 {
		t.Fatalf("legacy reserve result changed: %+v %d %s", result, code, diagnostic)
	}
}

// A genuinely reverted reserve consumes its nonce but cannot become a vault
// prerequisite, even with an already approved correctly addressed successor.
func TestEvmVaultCreateRejectsRevertedReservePrerequisite(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.config.Plan.Actions[0].Gas = 200_000
	f.publishConfig()
	f.signSelectedAction()
	f.gasFailure = true
	f.prepareSigned()
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--online")
	if code != 0 || result.Status != "create-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 {
		t.Fatalf("reserve did not genuinely revert: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("apply", "--action", "vault-create"); code != 3 || !strings.Contains(diagnostic, "successful reserve") {
		t.Fatalf("reverted reserve admitted vault: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmVaultCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) || len(f.writes) != 1 {
		t.Fatalf("reverted reserve acquired vault custody: %v", err)
	}
}

// Vault gas failure is a canonical consumed nonce, with no runtime/getter claim.
// Offline reopen preserves that terminal receipt without another send.
func TestEvmVaultCreateRevertedReceiptRetainsConsumedNonce(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.config.Plan.Actions[1].Gas = 200_000
	f.publishConfig()
	f.prepareVaultSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Status != "create-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 || result.Receipt.RuntimeHash != "" || result.Receipt.GetterHash != "" || len(f.writes) != 2 {
		t.Fatalf("vault revert lost its nonce outcome: %+v %d %s", result, code, diagnostic)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "vault-create")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || retained.ReceiptObservation != "retained" || !maps.Equal(counts, f.counts) {
		t.Fatalf("vault offline revert was not retained: %+v %d %s", retained, code, diagnostic)
	}
}

// Approval cannot be extended around an existing reserve marker. The exact
// completed original graph remains the only authority for its nonce custody.
func TestEvmVaultCreateCannotAmendExistingReserveApproval(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultPrerequisite()
	reservePath := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before, err := os.ReadFile(reservePath)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Plan.MaximumTotalWei = "90000001"
	f.publishConfig()
	if _, code, diagnostic := f.command("apply", "--action", "vault-create"); code != 3 || !strings.Contains(diagnostic, "independent phase approval") {
		t.Fatalf("fresh graph approval amended old reserve custody: %d %s", code, diagnostic)
	}
	after, err := os.ReadFile(reservePath)
	if err != nil || !bytes.Equal(before, after) || len(f.writes) != 1 {
		t.Fatalf("graph amendment mutated old reserve: %v", err)
	}
}

// Original transaction signatures are routed by action. A valid reserve
// envelope cannot populate vault custody merely because its sender is shared.
func TestEvmVaultCreateRejectsReserveSignatureImport(t *testing.T) {
	f := newEvmVaultFixture(t)
	reservePath, reserveHash := f.signedPath, f.signedHash
	f.prepareVaultPrerequisite()
	if _, code, diagnostic := f.command("apply", "--action", "vault-create"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--signed-transaction", reservePath, "--signed-transaction-hash", reserveHash); code != 2 || !strings.Contains(diagnostic, "signed-byte envelope") {
		t.Fatalf("reserve signature populated vault custody: %d %s", code, diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "vault-create")
	if code != 0 || result.Status != "signature-awaiting-import" || result.TransactionHash != "" || result.Attempts != 0 {
		t.Fatalf("wrong action import altered vault intent: %+v %d %s", result, code, diagnostic)
	}
}

// Output loss after durable receipt retention reopens the same completed vault.
// The local output channel owns no transaction retry or completion authority.
func TestEvmVaultCreateOutputFailureRecoversCompletedChild(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultSigned()
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	args := []string{"bootstrap-contracts", "resume", "--action", "vault-create", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--online", "--submit"}
	var stderr bytes.Buffer
	if code := runMain(f.storageContext(context.Background()), args, bootstrapRootFailedWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output failed") {
		t.Fatalf("vault output loss was acknowledged or occurred before retention: %d %s", code, stderr.String())
	}
	result, code, diagnostic := f.command("resume", "--action", "vault-create")
	if code != 0 || result.Status != "vault-created" || result.Receipt == nil || len(f.writes) != 2 {
		t.Fatalf("output loss discarded completed vault: %+v %d %s", result, code, diagnostic)
	}
}

// Existing signed intent remains unresolved when current confirmed or pending
// nonce moves. Neither observation may authorize a replacement or fresh budget.
func TestEvmVaultCreateNonceMovementRetainsOriginalIntent(t *testing.T) {
	for _, pending := range []bool{false, true} {
		f := newEvmVaultFixture(t)
		f.prepareVaultSigned()
		f.override = func(method string, params []any, result any) any {
			if method == "eth_getTransactionCount" && (!pending || params[1] == "pending") {
				return "0x2"
			}
			return result
		}
		result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
		status := "nonce-consumed-receipt-unresolved"
		if pending {
			status = "pending-nonce-unresolved"
		}
		if code != 0 || result.Status != status || result.Attempts != 0 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 1 {
			t.Fatalf("nonce movement renewed vault intent: %+v %d %s", result, code, diagnostic)
		}
	}
}

// A later unrelated runtime and expired send window cannot erase either
// canonical historical inclusion under the originally approved runtime.
func TestEvmVaultCreateHistoricalReceiptsSurviveLaterRuntime(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.config.Plan.ValidThroughNative = 102
	f.publishConfig()
	f.prepareVaultSigned()
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 0 {
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
	result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Status != "vault-created" || result.Receipt == nil || result.Receipt.NativeNumber != 102 || len(f.writes) != 2 {
		t.Fatalf("later runtime erased historical vault intent: %+v %d %s", result, code, diagnostic)
	}
}

// The window stops local sends while keeping an unexecuted signed vault and
// its original graph reservation available for later canonical reconciliation.
func TestEvmVaultCreateExpiryRetainsSignedLiability(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.config.Plan.ValidThroughNative = 101
	f.publishConfig()
	f.prepareVaultSigned()
	f.advanceEmpty()
	result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Status != "approval-expired-signed-liability-retained" || result.Attempts != 0 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 1 {
		t.Fatalf("expiry discarded or submitted vault liability: %+v %d %s", result, code, diagnostic)
	}
	retained, code, diagnostic := f.command("resume", "--action", "vault-create")
	if code != 0 || retained.TransactionHash != result.TransactionHash || retained.Attempts != 0 || retained.Receipt != nil {
		t.Fatalf("offline expiry lost signed vault: %+v %d %s", retained, code, diagnostic)
	}
}

// A reserve marker cannot claim another action, even with the identical graph
// hash. It must fail without synthesizing an unsigned vault allowance.
func TestEvmVaultCreateRejectsCopiedReserveMarker(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultPrerequisite()
	marker, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile) + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, evmVaultCreateStateFile)
	if err := os.WriteFile(path+".lock", marker, 0600); err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault-create"); code != 3 || !strings.Contains(diagnostic, "marker differs") {
		t.Fatalf("reserve marker became vault authority: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) || len(f.writes) != 1 {
		t.Fatalf("copied reserve marker created vault state: %v", err)
	}
}

// A self-consistent changed predecessor record still differs from the exact
// reserve intent sealed by vault custody. Its hash cannot be learned on resume.
func TestEvmVaultCreateRejectsChangedRetainedPredecessor(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultSigned()
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	reserve, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	reserve.Attempts++
	reserve.ContentHash = ""
	reserve.ContentHash = rootObjectHash(reserve)
	if err := store.save(reserve); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	counts := maps.Clone(f.counts)
	if _, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit"); code != 3 || !strings.Contains(diagnostic, "marker differs") {
		t.Fatalf("vault learned changed predecessor custody: %d %s", code, diagnostic)
	}
	if !maps.Equal(counts, f.counts) || len(f.writes) != 1 {
		t.Fatal("changed predecessor reached vault RPC")
	}
}

// The node changes its canonical native hash after the reserve receipt audit
// but before vault admission. The original phase start is still shared, so the
// vault must additionally retain the completed reserve checkpoint as a fence.
func TestEvmVaultCreateKeepsReserveCheckpointDuringFreshAdmission(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultSigned()
	originalHash := f.hashes[101]
	original := f.headers[originalHash]
	logs := append(append([]string(nil), original.Digest.Logs...), "0x0000")
	fork, forkHash := evmTestNativeHeader(t, f.hashes[100], 101, logs)
	f.headers[forkHash] = fork
	gettersObserved, changed := false, false
	f.override = func(method string, params []any, result any) any {
		if method == "eth_call" {
			input := params[0].(map[string]any)
			gettersObserved = gettersObserved || input["to"] == f.plan.Reserve.Address.Hex() && input["data"] == f.plan.Reserve.Getters[len(f.plan.Reserve.Getters)-1].Data
		}
		if method == "chain_getFinalizedHead" && gettersObserved && !changed {
			changed = true
			f.hashes[101] = forkHash
			return forkHash
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 1 || !changed || !strings.Contains(diagnostic, "canonical native ancestry changed") || len(f.writes) != 1 {
		t.Fatalf("vault forgot reserve checkpoint after prior audit: %+v %d %s", result, code, diagnostic)
	}
	f.override = nil
	f.hashes[101] = originalHash
	result, code, diagnostic = f.command("resume", "--action", "vault-create", "--online", "--submit")
	if code != 0 || result.Attempts != 1 || len(f.writes) != 2 {
		t.Fatalf("restored reserve checkpoint did not admit vault: %+v %d %s", result, code, diagnostic)
	}
}
