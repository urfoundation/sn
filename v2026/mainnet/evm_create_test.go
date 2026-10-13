// Deterministic contract-owner regressions cover real local execution, original
// signature custody, uncertain sends and interrupted durable publication.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Public-key and signature encodings are deliberately distinct. A successful
// independent verification precedes exact signed-payload rejection downstream.
func TestEvmCreateApprovalSignatureCanonicalWireReachesSignedAdmission(t *testing.T) {
	f := newEvmCreateFixture(t)
	if len(f.config.Signature) != 128 || strings.HasPrefix(f.config.Signature, "0x") || f.config.Signature != strings.ToLower(f.config.Signature) {
		t.Fatal("fixture approval did not emit canonical signature wire")
	}
	signature, err := hex.DecodeString(f.config.Signature)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(strings.TrimPrefix(f.config.ApprovalPublicKey, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	message, err := f.config.Plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(key, message, signature) {
		t.Fatal("fixture approval bytes fail independent Ed25519 verification")
	}
	if err := f.config.validate(); err != nil {
		t.Fatalf("canonical EVM approval was rejected: %v", err)
	}
	if _, code, diagnostic := f.command("apply"); code != 0 {
		t.Fatalf("canonical wire did not reach local custody: %d %s", code, diagnostic)
	}
	changed := f.config.Plan.Actions[0]
	changed.Nonce++
	if _, err := changed.signed(f.raw); err == nil || !strings.Contains(err.Error(), "differs from exact approved envelope") {
		t.Fatalf("canonical approval did not reach the intended signed-nonce boundary: %v", err)
	}
}

// No alternate wire spelling or valid-width invalid signature can acquire
// custody. This does not broaden the existing root offline signature parser.
func TestEvmCreateApprovalSignatureRejectsAlternateWire(t *testing.T) {
	f := newEvmCreateFixture(t)
	for _, value := range []string{"0x" + f.config.Signature, strings.ToUpper(f.config.Signature), f.config.Signature[:126], f.config.Signature + "00", " " + f.config.Signature, strings.Repeat("gg", 64), strings.Repeat("00", 64)} {
		candidate := copyEvmPhaseConfig(f.config)
		candidate.Signature = value
		if err := candidate.validate(); err == nil {
			t.Fatalf("accepted alternate or invalid approval signature %q", value)
		}
	}
	if len(f.counts) != 0 || len(f.writes) != 0 {
		t.Fatal("signature wire validation reached RPC")
	}
}

// Offline custody preparation must produce no RPC observation or write.
func (self *evmCreateFixture) prepareSigned() {
	self.t.Helper()
	result, code, diagnostic := self.command("apply")
	if code != 0 || result.Status != "signature-awaiting-import" {
		self.t.Fatalf("prepare custody: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic = self.command("resume", "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash)
	if code != 0 || result.Status != "signed-custody-complete" || result.TransactionHash != self.tx.Hash().Hex() {
		self.t.Fatalf("import original bytes: %+v %d %s", result, code, diagnostic)
	}
	if len(self.counts) != 0 || len(self.writes) != 0 {
		self.t.Fatal("offline custody accessed RPC")
	}
}

// Full command dispatch reaches actual artifact decoding, durable custody,
// owned HTTP and genuine reserve execution before canonical getter admission.
func TestEvmCreateCommandExecutesReviewedReserveAndResumes(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "submitted-awaiting-canonical-receipt" || result.Attempts != 1 {
		t.Fatalf("owned send: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic = f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || result.Receipt == nil || result.Receipt.NativeNumber != 101 || result.Receipt.BlockNumber != 38 || result.Receipt.RuntimeHash == "" || result.Receipt.GetterHash == "" {
		t.Fatalf("canonical genuine-EVM receipt: %+v %d %s", result, code, diagnostic)
	}
	if result.InstallationComplete || result.ActivationReady || len(result.RemainingActions) != 8 || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) {
		t.Fatalf("first CREATE altered its finite scope: %+v", result)
	}
	if f.counts["eth_call"] != len(f.plan.Getters) || f.counts["eth_getTransactionByBlockHashAndIndex"] != 1 {
		t.Fatalf("receipt omitted live constructor postconditions: %v", f.counts)
	}
	result, code, diagnostic = f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || len(f.writes) != 1 {
		t.Fatalf("completed resume repeated CREATE: %+v %d %s", result, code, diagnostic)
	}
}

// Losing the HTTP acknowledgement after execution retains exactly one attempt;
// the next invocation discovers the original inclusion without broadcasting.
func TestEvmCreateLostReplyReconcilesOriginalInclusion(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	f.loseReply = true
	_, code, diagnostic := f.command("resume", "--online", "--submit")
	f.stateLock.Lock()
	oneOriginalWrite := len(f.writes) == 1 && bytes.Equal(f.writes[0], f.raw)
	f.stateLock.Unlock()
	if code != 1 || !oneOriginalWrite {
		t.Fatalf("uncertain send was retried or forgotten: %d %s", code, diagnostic)
	}
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := store.load()
	if closeErr := store.close(); err != nil || closeErr != nil {
		t.Fatalf("uncertain original custody could not reopen: %v %v", err, closeErr)
	}
	if retained.Attempts != 1 || retained.Signed != "0x"+hex.EncodeToString(f.raw) || retained.TransactionHash != f.tx.Hash().Hex() || retained.Receipt != nil {
		t.Fatalf("lost acknowledgement changed durable original liability: %+v", retained)
	}
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	f.stateLock.Lock()
	oneOriginalWrite = len(f.writes) == 1 && bytes.Equal(f.writes[0], f.raw)
	f.stateLock.Unlock()
	if code != 0 || result.Status != "reserve-created" || result.Attempts != retained.Attempts || result.TransactionHash != retained.TransactionHash || result.Receipt == nil || result.Receipt.TransactionHash != retained.TransactionHash || !oneOriginalWrite {
		t.Fatalf("lost acknowledgement recovery: %+v %d %s", result, code, diagnostic)
	}
}

// A never-included transaction can replay only its original bytes and only the
// originally approved number of attempts; restarts do not refresh that budget.
func TestEvmCreateReplayRetainsFiniteOriginalAllowance(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	f.mine = false
	for attempt := 0; attempt < 3; attempt++ {
		result, code, diagnostic := f.command("resume", "--online", "--submit")
		if code != 0 {
			t.Fatalf("attempt %d: %s", attempt, diagnostic)
		}
		if attempt == 2 && (result.Status != "attempt-allowance-exhausted" || result.Attempts != 2) {
			t.Fatalf("restart renewed send allowance: %+v", result)
		}
	}
	if len(f.writes) != 2 || !bytes.Equal(f.writes[0], f.raw) || !bytes.Equal(f.writes[1], f.raw) {
		t.Fatalf("replay changed original signed bytes or write count: %d", len(f.writes))
	}
}

// Every signed field and the recovered sender belong to independent approval.
// Semantic similarity or a valid signature alone cannot substitute an envelope.
func TestEvmCreateRejectsChangedSignedAuthority(t *testing.T) {
	f := newEvmCreateFixture(t)
	key, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nonce", "fee", "tip", "gas", "value", "target", "data", "chain", "access-list", "signer"} {
		original := f.tx
		transaction := &types.DynamicFeeTx{ChainID: big.NewInt(964), Nonce: original.Nonce(), GasFeeCap: original.GasFeeCap(), GasTipCap: original.GasTipCap(), Gas: original.Gas(), Value: original.Value(), Data: original.Data()}
		signerKey := key
		switch name {
		case "nonce":
			transaction.Nonce++
		case "fee":
			transaction.GasFeeCap = big.NewInt(11)
		case "tip":
			transaction.GasTipCap = big.NewInt(2)
		case "gas":
			transaction.Gas--
		case "value":
			transaction.Value = big.NewInt(1)
		case "target":
			target := common.Address{42}
			transaction.To = &target
		case "data":
			transaction.Data = append(transaction.Data, 0)
		case "chain":
			transaction.ChainID = big.NewInt(945)
		case "access-list":
			transaction.AccessList = types.AccessList{{Address: common.Address{42}}}
		case "signer":
			signerKey, err = crypto.HexToECDSA(strings.Repeat("18", 32))
			if err != nil {
				t.Fatal(err)
			}
		}
		signed, err := types.SignTx(types.NewTx(transaction), types.LatestSignerForChainID(transaction.ChainID), signerKey)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := signed.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.config.Plan.Actions[0].signed(raw); err == nil {
			t.Fatalf("accepted changed signed %s", name)
		}
	}
	if _, err := f.config.Plan.Actions[0].signed(append(f.raw, 0)); err == nil {
		t.Fatal("accepted signed trailing bytes")
	}
}

// Wrong independent approval, release or acceptance fails before a journal or
// HTTP connection exists; a content pin is not learned from mutated input.
func TestEvmCreateRejectsUnapprovedPlanBeforeCustody(t *testing.T) {
	for _, name := range []string{"signature", "approval-key", "artifact", "source", "scope", "fee-budget", "route", "constructor"} {
		f := newEvmCreateFixture(t)
		candidate := copyEvmPhaseConfig(f.config)
		switch name {
		case "signature":
			candidate.Signature = "0x" + strings.Repeat("00", 64)
		case "approval-key":
			candidate.ApprovalPublicKey = "0x" + strings.Repeat("15", 32)
		case "artifact":
			if err := os.WriteFile(candidate.Plan.Artifacts.Path, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
		case "source":
			candidate.Plan.Runtime.RuntimeSourceCommit = strings.Repeat("1", 40)
		case "scope":
			candidate.Plan.Network.EvmChainId = 945
		case "fee-budget":
			candidate.Plan.MaximumTotalWei = "1"
		case "route":
			candidate.Plan.Route.RpcUrl = "http://localhost:80"
		case "constructor":
			candidate.Plan.Actions[0].Data = "0x00"
		}
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		_, code, _ := f.command("apply")
		if code == 0 {
			t.Fatalf("accepted unapproved %s", name)
		}
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid %s acquired custody", name)
		}
		if len(f.counts) != 0 {
			t.Fatalf("invalid %s accessed RPC", name)
		}
	}
}

// Exact current-chain checks happen before counting an attempt or calling HTTP
// submission. Nonce movement is unresolved custody, never a replacement budget.
func TestEvmCreateAdmissionRejectsChangedRuntimeNetworkAndNonce(t *testing.T) {
	for _, name := range []string{"genesis", "runtime", "metadata", "nonce-consumed", "pending", "predecessor", "balance", "occupied", "moving-head"} {
		f := newEvmCreateFixture(t)
		if name == "predecessor" {
			f.config.Plan.Actions[0].Nonce = 1
			// Rebuild the independently approved address-derived constructor.
			var artifact contractReleaseArtifact
			for _, a := range evmTestRelease(t).Artifacts {
				if a.Name == "ReserveSink" {
					artifact = a
				}
			}
			hotkey := [32]byte{31}
			data, _, _, err := contractReservePayload(artifact, 25, hotkey, f.config.Plan.Actions[0].Sender, 1)
			if err != nil {
				t.Fatal(err)
			}
			f.config.Plan.Actions[0].Data = "0x" + hex.EncodeToString(data)
			f.publishConfig()
			key, err := crypto.HexToECDSA(strings.Repeat("17", 32))
			if err != nil {
				t.Fatal(err)
			}
			tx, err := f.config.Plan.Actions[0].unsigned()
			if err != nil {
				t.Fatal(err)
			}
			f.tx, err = types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(964)), key)
			if err != nil {
				t.Fatal(err)
			}
			f.raw, err = f.tx.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			// This case imports through the owner below, leaving the unrelated
			// original signed-byte file unused.
		}
		store, err := openEvmActionStore(f.config, true, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		chain, err := newEvmOwnedChain(f.config)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := newEvmCreateOwner(f.plan, store, chain)
		if err != nil {
			t.Fatal(err)
		}
		f.override = func(method string, args []any, result any) any {
			switch name {
			case "genesis":
				if method == "chain_getBlockHash" && args[0] == float64(0) {
					return "0x" + strings.Repeat("99", 32)
				}
			case "runtime":
				if method == "state_getStorage" && args[0] == runtimeCodeStorageKey {
					return "0x0061736d0100000000"
				}
			case "metadata":
				if method == "state_getMetadata" {
					return "0x6d657461ff00"
				}
			case "nonce-consumed":
				if method == "eth_getTransactionCount" {
					return "0x1"
				}
			case "pending":
				if method == "eth_getTransactionCount" && args[1] == "pending" {
					return "0x1"
				}
			case "balance":
				if method == "eth_getBalance" {
					return "0x0"
				}
			case "occupied":
				if method == "eth_getCode" {
					return "0x00"
				}
			case "moving-head":
				if method == "eth_getBalance" {
					f.head = 99
				}
			}
			return result
		}
		result, advanceErr := owner.advance(context.Background(), f.raw, true, true)
		if advanceErr == nil && result.Status != "nonce-consumed-receipt-unresolved" && result.Status != "pending-nonce-unresolved" && result.Status != "predecessor-nonce-unresolved" {
			t.Fatalf("admitted changed %s: %+v", name, result)
		}
		record, err := store.load()
		if err != nil {
			t.Fatal(err)
		}
		if record.Attempts != 0 || len(f.writes) != 0 {
			t.Fatalf("changed %s consumed authority or sent", name)
		}
		store.close()
	}
}

// Status1 is insufficient: canonical original bytes, first native insertion,
// release runtime and every constructor getter must all agree.
func TestEvmCreateRejectsCorruptCanonicalPostconditions(t *testing.T) {
	for _, name := range []string{"missing-status", "missing-gas", "missing-fee", "fee", "gas", "status", "address", "runtime", "getter", "first-insertion", "native-fork", "transaction"} {
		f := newEvmCreateFixture(t)
		f.prepareSigned()
		if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
			t.Fatal(diagnostic)
		}
		f.override = func(method string, args []any, result any) any {
			if method == "eth_getTransactionReceipt" {
				copy := map[string]any{}
				for k, v := range f.receipt {
					copy[k] = v
				}
				switch name {
				case "missing-status":
					delete(copy, "status")
				case "missing-gas":
					delete(copy, "gasUsed")
				case "missing-fee":
					delete(copy, "effectiveGasPrice")
				case "fee":
					copy["effectiveGasPrice"] = "0xb"
				case "gas":
					copy["gasUsed"] = "0xffffff"
				case "status":
					copy["status"] = "0x2"
				case "address":
					copy["contractAddress"] = (common.Address{9}).Hex()
				}
				return copy
			}
			if name == "runtime" && method == "eth_getCode" {
				return "0x00"
			}
			if name == "getter" && method == "eth_call" {
				return "0x" + strings.Repeat("00", 32)
			}
			if name == "first-insertion" && method == "state_getStorage" && args[0] == f.storageKey && args[1] == f.hashes[100] {
				return f.receipt["blockHash"]
			}
			if name == "native-fork" && method == "chain_getBlockHash" && args[0] == float64(100) {
				return "0x" + strings.Repeat("77", 32)
			}
			if name == "transaction" && method == "eth_getTransactionByBlockHashAndIndex" {
				return types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(1)})
			}
			return result
		}
		_, code, diagnostic := f.command("resume", "--online", "--submit")
		if code == 0 || len(f.writes) != 1 {
			t.Fatalf("accepted corrupt %s postcondition or resent: %d %s", name, code, diagnostic)
		}
	}
}

// Approval expiry stops new writes but cannot invalidate an already signed EVM
// liability or erase its historical canonical execution after a runtime upgrade.
func TestEvmCreateHistoricalReceiptSurvivesApprovalExpiry(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.config.Plan.ValidThroughNative = 101
	f.publishConfig()
	f.prepareSigned()
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	header, hash := evmTestNativeHeader(t, f.hashes[101], 102, []string{mappingTestDigest(t, 1, f.receipt["blockHash"].(string), []string{f.tx.Hash().Hex()})})
	f.headers[hash], f.hashes[102], f.head = header, hash, 102
	f.override = func(method string, args []any, result any) any {
		if method == "state_getRuntimeVersion" && args[0] == hash {
			version := f.config.Plan.Runtime.RuntimeVersion
			version.SpecVersion++
			return version
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || len(f.writes) != 1 {
		t.Fatalf("historical original receipt was stranded: %+v %d %s", result, code, diagnostic)
	}
}

// Post-rename directory-sync failure leaves a complete record on disk. The
// failed owner must not send; its replacement reuses that record's spent attempt.
func TestEvmCreateAmbiguousAttemptPublicationPoisonsOwner(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	f.mine = false
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newEvmCreateOwner(f.plan, store, chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 2 {
			return errors.New("synthetic attempt directory-sync interruption")
		}
		return nil
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("ambiguous attempt sync was acknowledged")
	}
	if len(f.writes) != 0 {
		t.Fatal("failed attempt publication reached network")
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("poisoned owner reused an ambiguous write")
	}
	store.close()
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Attempts != 2 || len(f.writes) != 1 {
		t.Fatalf("reopen renewed ambiguous attempt allowance: %+v %d %s", result, code, diagnostic)
	}
}

// Output failure occurs after child durability; resume observes actual child
// state instead of resending because a convenience output was lost.
func TestEvmCreateOutputFailureRetainsCompletedChild(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	args := []string{"bootstrap-contracts", "resume", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--online", "--submit"}
	var stderr bytes.Buffer
	if code := runMain(f.storageContext(context.Background()), args, bootstrapRootFailedWriter{}, &stderr); code != 1 {
		t.Fatalf("output failure was acknowledged: %d", code)
	}
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || len(f.writes) != 1 {
		t.Fatalf("lost output repeated child: %+v %d %s", result, code, diagnostic)
	}
}

// Only two exact pre-signature claim boundaries can recover; completed custody
// loss and malformed markers never grant a fresh reservation.
func TestEvmCreateInitialClaimRecoveryAndLostCustody(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "record-synced"} {
		f := newEvmCreateFixture(t)
		store, err := openEvmActionStore(f.config, true, func(stage string) error {
			if stage == boundary {
				return errors.New("synthetic claim interruption")
			}
			return nil
		}, f.storage.Context)
		if err == nil || store != nil {
			t.Fatal("claim interruption was acknowledged")
		}
		store, err = openEvmActionStore(f.config, false, nil, f.storage.Context)
		if err != nil {
			t.Fatalf("recover %s: %v", boundary, err)
		}
		if _, err := openEvmActionStore(f.config, false, nil, f.storage.Context); err == nil {
			t.Fatal("second owner acquired flock")
		}
		record, err := store.load()
		if err != nil || record.Signed != "" || record.Attempts != 0 {
			t.Fatalf("claim recovery altered allowance: %+v %v", record, err)
		}
		store.close()
		if err := os.Remove(filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)); err != nil {
			t.Fatal(err)
		}
		if store, err := openEvmActionStore(f.config, false, nil, f.storage.Context); err == nil {
			store.close()
			t.Fatal("completed missing journal became fresh allowance")
		}
		if store, err := openEvmActionStore(f.config, true, nil, f.storage.Context); err == nil {
			store.close()
			t.Fatal("apply bypassed completed ownership marker")
		}
	}
}

// A caller waiting on the owner must return cancellation before it can access
// the retained journal or reserve a send, with no timing assumptions.
func TestEvmCreateCanceledWaiterAndClosedStorage(t *testing.T) {
	f := newEvmCreateFixture(t)
	store, err := openEvmActionStore(f.config, true, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newEvmCreateOwner(f.plan, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := owner.advance(ctx, f.raw, false, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter reached custody: %v", err)
	}
	<-owner.gate
	record, err := store.load()
	if err != nil || record.Signed != "" {
		t.Fatalf("canceled waiter mutated custody: %+v %v", record, err)
	}
	store.close()
	if _, err := store.load(); err == nil {
		t.Fatal("closed journal read remained available")
	}
	if err := store.save(record); err == nil {
		t.Fatal("closed journal write remained available")
	}
	if _, err := owner.advance(context.Background(), f.raw, false, false); err == nil {
		t.Fatal("closed custody owner accepted signature")
	}
}

// Public signature files must be exact independently pinned bytes and cannot
// alias mutable custody; no key file or signing callback exists in this command.
func TestEvmCreateSignaturePinAndPrivatePaths(t *testing.T) {
	f := newEvmCreateFixture(t)
	if _, code, diagnostic := f.command("apply"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, code, _ := f.command("resume", "--signed-transaction", f.signedPath, "--signed-transaction-hash", "sha256:"+strings.Repeat("99", 32)); code == 0 {
		t.Fatal("wrong signed-byte pin imported")
	}
	link := filepath.Join(f.config.Plan.RunDirectory, "signature-link")
	if err := os.Symlink(f.signedPath, link); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := f.command("resume", "--signed-transaction", link, "--signed-transaction-hash", f.signedHash); code == 0 {
		t.Fatal("symlink signature imported")
	}
	if err := os.Chmod(f.signedPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := f.command("resume", "--signed-transaction", f.signedPath, "--signed-transaction-hash", f.signedHash); code == 0 {
		t.Fatal("publicly mutable signature file imported")
	}
	if len(f.counts) != 0 {
		t.Fatal("signature refusal contacted RPC")
	}
}

// Expired approvals cannot send even when nonce/balance permit it; EVM signatures
// have no native mortality, so the result retains the original unresolved hash.
func TestEvmCreateExpiryRetainsUnresolvedSignedLiability(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.config.Plan.ValidThroughNative = 101
	f.publishConfig()
	f.prepareSigned()
	parent, parentHash := evmTestNativeHeader(t, f.hashes[100], 101, []string{})
	head, headHash := evmTestNativeHeader(t, parentHash, 102, []string{})
	f.headers[parentHash], f.hashes[101] = parent, parentHash
	f.headers[headHash], f.hashes[102], f.head = head, headHash, 102
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "approval-expired-signed-liability-retained" || result.TransactionHash != f.tx.Hash().Hex() || result.Attempts != 0 || len(f.writes) != 0 {
		t.Fatalf("expiry released or sent a signed liability: %+v %d %s", result, code, diagnostic)
	}
}

// One-shot read adapters cannot be coerced into writes or acquire a subscription.
func TestEvmCreateReadProfileCannotSubmit(t *testing.T) {
	f := newEvmCreateFixture(t)
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.read(context.Background(), "eth_sendRawTransaction", []any{}, new(string)); err == nil {
		t.Fatal("read profile admitted submission")
	}
	bridge := &evmNativeReadClient{client: chain.client}
	if err := bridge.Call(new(string), "system_chain"); err == nil {
		t.Fatal("native bridge used background context")
	}
	if _, err := bridge.Subscribe(context.Background(), "", "", "", "", nil); err == nil {
		t.Fatal("native bridge acquired subscription")
	}
	if err := bridge.CallContext(context.Background(), new(string), "author_submitExtrinsic"); err == nil {
		t.Fatal("native bridge admitted write")
	}
	if len(f.counts) != 0 {
		t.Fatal("rejected method reached transport")
	}
	chain.client.url = f.server.URL + "/changed-route"
	if _, err := ownedSubmissionPost(context.Background(), chain.client, f.config.Plan.Route, "eth_sendRawTransaction", "0x"+hex.EncodeToString(f.raw), f.tx.Hash().Hex()); err == nil {
		t.Fatal("submission escaped the independently approved route")
	}
	if len(f.counts) != 0 {
		t.Fatal("changed submission route reached HTTP")
	}
}

// Normal head advancement refreshes current authority in the same bounded
// invocation; it neither requires a paused chain nor replaces signed custody.
func TestEvmCreateHealthyHeadAdvancePreservesPreparedTransaction(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	advanced := false
	f.override = func(method string, args []any, result any) any {
		if method == "eth_getBalance" && !advanced {
			advanced = true
			f.advanceEmpty()
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Attempts != 1 || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) {
		t.Fatalf("healthy advancement stranded or changed prepared transaction: %+v %d %s", result, code, diagnostic)
	}
	result, code, diagnostic = f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || result.Receipt.NativeNumber != 102 || result.Receipt.BlockNumber != 39 || len(f.writes) != 1 {
		t.Fatalf("advanced-head transaction lost canonical recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Missing raw and public capabilities are not successful-value mismatches.
// Removing that outage resumes the original receipt and signature.
func TestEvmCreateUnavailableMappingRemainsResumable(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	f.override = func(method string, args []any, result any) any {
		if method == "debug_getRawHeader" || method == "eth_getBlockByHash" {
			return mappingFixtureRpcError{code: -32601}
		}
		return result
	}
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	publicReads := f.counts["eth_getBlockByHash"]
	_, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 1 || !strings.Contains(diagnostic, "mapping is unavailable") || strings.Contains(diagnostic, "does not match exact native commitment") || len(f.writes) != 1 || f.counts["eth_getBlockByHash"] != publicReads+1 {
		t.Fatalf("unavailable read became contradictory evidence: %d %s", code, diagnostic)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("unavailable mapping changed original custody: %v", err)
	}
	f.override = nil
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || result.Attempts != 1 || len(f.writes) != 1 {
		t.Fatalf("outage poisoned original custody: %+v %d %s", result, code, diagnostic)
	}
}

// Completed 128-header chunks survive later receipt-read failure. The current
// incomplete chunk can be repeated, but its candidate can never be skipped.
func TestEvmCreateScanCheckpointSurvivesLaterReadFailure(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	f.mine = false
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for i := 0; i < 128; i++ {
		f.advanceEmpty()
	}
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	result, code, diagnostic := f.command("resume", "--online")
	if code != 0 || result.Status != "receipt-awaiting-finalized-mapping" {
		t.Fatalf("bounded historical scan: %+v %d %s", result, code, diagnostic)
	}
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.load()
	store.close()
	if err != nil || before.ScanNumber != 228 {
		t.Fatalf("complete chunk not retained: %+v %v", before, err)
	}
	f.override = func(method string, args []any, result any) any {
		if method == "debug_getRawHeader" || method == "eth_getBlockByHash" {
			return mappingFixtureRpcError{code: -32601}
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--online"); code != 1 || !strings.Contains(diagnostic, "mapping is unavailable") {
		t.Fatalf("historical outage acknowledged or misclassified: %d %s", code, diagnostic)
	}
	store, err = openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.load()
	store.close()
	if err != nil || before.ContentHash != after.ContentHash {
		t.Fatalf("later failure discarded chunk or skipped candidate: %+v %v", after, err)
	}
	f.override = nil
	result, code, diagnostic = f.command("resume", "--online")
	if code != 0 || result.Status != "reserve-created" || result.Receipt.NativeNumber != 229 || len(f.writes) != 1 {
		t.Fatalf("checkpointed scan failed to recover original receipt: %+v %d %s", result, code, diagnostic)
	}
}
