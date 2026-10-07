// Real contract/Safe execution and independently encoded native tries exercise
// installation, hidden storage, counted recovery and immutable original custody.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// All variants retain valid Safe success; only the one-shot coordinator event
// changes. The pre-fix owner accepted missing/wrong events as installation.
func TestBootstrapContractAnchorRequiresExactBindingEvent(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	plan := f.approval.Plan
	for _, fault := range []string{"missing", "foreign emitter", "wrong evidence", "padding", "data", "duplicate", "after success"} {
		r := f.receipt()
		switch fault {
		case "missing":
			r.Receipt.Logs = r.Receipt.Logs[1:]
		case "foreign emitter":
			r.Receipt.Logs[0].Address[0] ^= 1
		case "wrong evidence":
			r.Receipt.Logs[0].Topics[1][31] ^= 1
		case "padding":
			r.Receipt.Logs[0].Topics[1][0] = 1
		case "data":
			r.Receipt.Logs[0].Data = []byte{0}
		case "duplicate":
			duplicate := *r.Receipt.Logs[0]
			duplicate.Index = 1
			r.Receipt.Logs[1].Index = 2
			r.Receipt.Logs = []*types.Log{r.Receipt.Logs[0], &duplicate, r.Receipt.Logs[1]}
		case "after success":
			r.Receipt.Logs[0], r.Receipt.Logs[1] = r.Receipt.Logs[1], r.Receipt.Logs[0]
			r.Receipt.Logs[0].Index, r.Receipt.Logs[1].Index = 0, 1
		}
		if phase, err := r.phase(plan, f.profile); err == nil || phase != "" {
			t.Fatalf("%s binding event became installation: %s %v", fault, phase, err)
		}
	}
	r := f.receipt()
	other := *r.Receipt.Logs[1]
	other.Topics = []common.Hash{crypto.Keccak256Hash([]byte("SyntheticOther(bytes32)")), plan.Review.Transaction.Digest}
	other.Index, r.Receipt.Logs[0].Index, r.Receipt.Logs[1].Index = 0, 1, 2
	r.Receipt.Logs = append([]*types.Log{&other}, r.Receipt.Logs...)
	if phase, err := r.phase(plan, f.profile); err != nil || phase != "installed" {
		t.Fatal("unrelated Safe event was mistaken for execution success", phase, err)
	}
}

// The fixture reads the real EVM independently of the production projection:
// dense source slots, the policy array and ERC-7201/ERC-1967 namespaces.
func bootstrapContractInstallationTestEntries(t *testing.T, f *bootstrapSuccessorCanonicalFixture) map[string][]byte {
	t.Helper()
	chain, plan := f.original.contracts, f.approval.Plan
	oracle := &safeExecutionFixture{state: chain.state, transaction: plan.transaction(), owners: slices.Clone(plan.Request.Owners)}
	entries := safeCurrentProofFixtureFromOracle(t, oracle, plan.Review.Request.Version, plan.Review.Request.Variant).entries
	entries[":code"] = slices.Clone(chain.code)
	deployer := f.original.contracts.config.Plan.Actions[0].Sender
	for _, index := range []int{0, 1, 2, 4, 7} {
		address := crypto.CreateAddress(deployer, f.original.contracts.config.Plan.Actions[index].Nonce)
		code := chain.state.GetCode(address)
		encoded, err := codec.Encode(code)
		if err != nil {
			t.Fatal(err)
		}
		entries[string(safeCurrentTestNativeKey("AccountCodes", address, nil))] = encoded
		metadata := make([]byte, 40)
		binary.LittleEndian.PutUint64(metadata, uint64(len(code)))
		copy(metadata[8:], crypto.Keccak256(code))
		entries[string(safeCurrentTestNativeKey("AccountCodesMetadata", address, nil))] = metadata
		var slots []common.Hash
		for slot := int64(0); slot < 64; slot++ {
			slots = append(slots, common.BigToHash(big.NewInt(slot)))
		}
		base := new(big.Int).SetBytes(crypto.Keccak256(common.LeftPadBytes([]byte{6}, 32)))
		for offset := int64(0); offset < 6; offset++ {
			slots = append(slots, common.BigToHash(new(big.Int).Add(base, big.NewInt(offset))))
		}
		for _, slot := range []string{"0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", "0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00", "0x9016d09d72d40fdae2fd8ceac6b6234c7706214fd39c1cd1e609a0528c199300"} {
			slots = append(slots, common.HexToHash(slot))
		}
		for _, slot := range slots {
			word := chain.state.GetState(address, slot)
			if word != (common.Hash{}) {
				entries[string(safeCurrentTestNativeKey("AccountStorages", address, &slot))] = slices.Clone(word[:])
			}
		}
	}
	return entries
}

// Every proof is committed before returning it. Original ancestor headers and
// receipt identities remain unchanged when a later empty head advances.
func bootstrapContractInstallationTestProofs(t *testing.T, f *bootstrapSuccessorCanonicalFixture) func(bool) safeCurrentStorageWitness {
	t.Helper()
	chain := f.original.contracts
	witnesses := map[string]safeCurrentStorageWitness{}
	chain.stateLock.Lock()
	chain.nativeProof = func(params []any) any {
		witness, found := witnesses[params[1].(string)]
		if !found {
			return mappingFixtureRpcError{code: -32000}
		}
		return map[string]any{"at": witness.At, "proof": witness.Nodes}
	}
	chain.stateLock.Unlock()
	return func(advance bool) safeCurrentStorageWitness {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if advance {
			chain.advanceEmpty()
		}
		oldHash := chain.hashes[chain.head]
		witness := bootstrapSuccessorSafeCurrentTestHeadWithLock(t, chain, bootstrapContractInstallationTestEntries(t, f))
		// The synthetic native state root is installed before any observer sees
		// this block; retain its original Ethereum.BlockHash storage witness too.
		for _, values := range chain.history.storage {
			if value, found := values[oldHash]; found {
				values[witness.At] = value
				delete(values, oldHash)
			}
		}
		witnesses[witness.At] = witness
		return witness
	}
}

// Public v2 dispatch runs real Safe code once, loses its acknowledgement, then
// recovers the exact counted anchor and proves installation without another send.
func TestBootstrapContractInstallationPublicReadbackRecoversExactAnchor(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	chain, plan := f.original.contracts, f.approval.Plan
	original := bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)
	seal := bootstrapContractInstallationTestProofs(t, f)
	seal(true)
	model := &bootstrapSuccessorExecutionFixture{t: t, approval: f.approval, key: f.key}
	revision := bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, bootstrapSuccessorSafeCurrentTestNext(t, model, f.canonical, bootstrapSuccessorRuntimeHistory{}, nil), f.key)
	reference := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-anchor-current-acceptance.json"), revision)
	args := []string{"contract-successor-execution-resume", "--config", f.original.path, "--run-dir", f.original.config.RunDirectory, "--accept-plan-hash", f.original.preparation.Plan.ContentHash}
	args = append(append(args, f.paths...), f.approvalArgs...)
	args = append(args, "--online", "--canonical-approval", f.canonicalRef.Path, "--canonical-approval-sha256", f.canonicalRef.Sha256,
		"--safe-current-revision", reference.Path, "--safe-current-revision-sha256", reference.Sha256, "--accept-safe-current-policy", rootObjectHash(revision))
	invoke := func(input []string) (int, bootstrapContractInstallation, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := runBootstrapSuccessorExecutionCommand(f.original.storageContext(t.Context()), input, &stdout, &stderr)
		var result bootstrapContractInstallation
		if stdout.Len() != 0 {
			if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
				t.Fatal(err, stdout.String())
			}
		}
		return code, result, stderr.String()
	}
	chain.stateLock.Lock()
	chain.loseReply = true
	chain.stateLock.Unlock()
	if code, _, diagnostic := invoke(append(slices.Clone(args), "--submit")); code != 1 {
		t.Fatal("expected lost reply", code, diagnostic)
	}
	chain.stateLock.Lock()
	chain.loseReply = false
	if len(chain.writes) != 9 || !bytes.Equal(chain.writes[8], common.FromHex(plan.SignedRelayer)) {
		t.Fatal("exact anchor was not sent once")
	}
	chain.stateLock.Unlock()
	anchor := seal(false)
	args[0] = "contract-successor-execution-readback"
	if code, _, diagnostic := invoke(append(slices.Clone(args), "--submit")); code != 2 {
		t.Fatal("readback accepted submit", code, diagnostic)
	}
	if code, _, diagnostic := invoke(args[:len(args)-2]); code != 1 || !strings.Contains(diagnostic, "provenance") {
		t.Fatal("readback inferred policy selection", code, diagnostic)
	}
	code, result, diagnostic := invoke(args)
	if code != 0 {
		t.Fatal("initial installation readback", code, diagnostic)
	}
	if !result.InstallationComplete || !result.InitialContracts.CompleteStorage || !result.InitialSafe.CompleteFinalizedStorage || !result.CurrentSafe.CompleteFinalizedStorage ||
		result.InitialContracts.NativeHash != anchor.At || result.Identity.AnchorReceipt.NativeHash.Hex() != anchor.At || result.Identity.SafeCurrentRevisionHash != rootObjectHash(revision) ||
		result.CompleteSafeHistory || result.CompleteContractHistory || result.CompletePendingState || result.ActivationReady || result.NetworkEffects || result.CurrentSafeNonce != "18" || len(result.CurrentContracts) != 5 {
		t.Fatal("installation readback scope differs", result)
	}
	identityHash := result.InstallationIdentityHash
	seal(true)
	code, later, diagnostic := invoke(args)
	if code != 0 || later.InstallationIdentityHash != identityHash || later.ContentHash == result.ContentHash || later.InitialContracts.NativeHash != anchor.At {
		t.Fatal("restart relabeled original installation", code, diagnostic)
	}
	// Later governance can advance the Safe nonce without rewriting its anchor.
	// This proves present authority only, never approval of that later operation.
	chain.stateLock.Lock()
	chain.state.SetState(plan.Review.Transaction.Safe, common.BigToHash(big.NewInt(5)), common.BigToHash(big.NewInt(19)))
	chain.state.SetNonce(plan.Review.Relayer.Sender, 44, tracing.NonceChangeUnspecified)
	chain.stateLock.Unlock()
	seal(true)
	code, later, diagnostic = invoke(args)
	if code != 0 || later.InstallationIdentityHash != identityHash || later.CurrentSafeNonce != "19" || later.CompleteSafeHistory || later.ActivationReady {
		t.Fatal("later read-only nonce changed original anchor authority", code, diagnostic)
	}
	// Original anchor proofs stay exact while a later implementation could
	// rewrite the clock and restore the reviewed code. A past block alone is
	// insufficient: current readback must retain the original proxy inclusion.
	clockSlot := common.BigToHash(new(big.Int).Add(new(big.Int).SetBytes(crypto.Keccak256(common.LeftPadBytes([]byte{6}, 32))), big.NewInt(1)))
	proxy := plan.Review.Transaction.To
	chain.stateLock.Lock()
	originalClock := chain.state.GetState(proxy, clockSlot)
	changedClock := originalClock
	binary.BigEndian.PutUint64(changedClock[16:24], binary.BigEndian.Uint64(originalClock[16:24])+1)
	chain.state.SetState(proxy, clockSlot, changedClock)
	chain.stateLock.Unlock()
	seal(true)
	if code, changed, diagnostic := invoke(args); code != 1 || changed.InstallationComplete || !strings.Contains(diagnostic, "current policy clock differs from original proxy inclusion") {
		t.Fatal("readback admitted rewritten current policy clock after exact anchor", code, diagnostic)
	}
	chain.stateLock.Lock()
	chain.state.SetState(proxy, clockSlot, originalClock)
	chain.stateLock.Unlock()
	seal(true)
	for name, raw := range original {
		if retained, err := os.ReadFile(filepath.Join(f.original.config.RunDirectory, name)); err != nil || string(retained) != raw {
			t.Fatal("changed original custody", name, err)
		}
	}
	// The final current proof follows historical recovery. Losing original
	// custody at this boundary must prevent a otherwise complete readback.
	chain.stateLock.Lock()
	proofCalls := 0
	priorOverride := chain.override
	chain.override = func(method string, params []any, value any) any {
		value = priorOverride(method, params, value)
		if method == "state_getReadProof" {
			proofCalls++
			if proofCalls == 3 {
				if err := os.WriteFile(filepath.Join(f.original.config.RunDirectory, bootstrapContractStateFile(7)), []byte("synthetic changed original evidence custody"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		return value
	}
	chain.stateLock.Unlock()
	if code, changed, diagnostic := invoke(args); code != 1 || changed.InstallationComplete || !strings.Contains(diagnostic, durablevolume.ErrIdentity.Error()) {
		t.Fatal("readback escaped final original custody fence", code, diagnostic)
	}
	chain.stateLock.Lock()
	if len(chain.writes) != 9 {
		t.Fatal("readback resent anchor")
	}
	chain.stateLock.Unlock()
}

// Projection values are checked against real executed source storage; forged
// roots containing otherwise invisible evidence/operator entries remain invalid.
func TestBootstrapContractInstallationRejectsHiddenAndChangedStorage(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	var stdout bytes.Buffer
	if code, diagnostic := f.online(&stdout, true); code != 0 {
		t.Fatal(code, diagnostic)
	}
	chain := f.original.contracts
	chain.stateLock.Lock()
	entries := bootstrapContractInstallationTestEntries(t, f)
	chain.stateLock.Unlock()
	_, adapter, close := f.openRuntimeRevisions()
	defer close()
	accounts, err := bootstrapContractInstallationAccounts(t.Context(), adapter.plans, adapter.records)
	if err != nil {
		t.Fatal(err)
	}
	check := func(values map[string][]byte) error {
		witness := safeCurrentTestWitness(t, values)
		head := chainIdentity{FinalizedHash: witness.At, FinalizedNumber: 703, runtimeVersion: f.canonical.Authorization.CurrentRuntime.RuntimeVersion}
		_, err := verifyBootstrapContractInstallationStorage(t.Context(), accounts, f.canonical.Authorization.CurrentRuntime, head, witness)
		return err
	}
	if err := check(entries); err != nil {
		t.Fatal("real initialized storage mismatches reviewed profile", err)
	}
	for _, fault := range []string{"hidden evidence", "hidden operator", "binding", "initialization clock", "code metadata", "missing code"} {
		changed := map[string][]byte{}
		for key, value := range entries {
			changed[key] = slices.Clone(value)
		}
		switch fault {
		case "hidden evidence", "hidden operator":
			address := adapter.plans[7].Address
			if fault == "hidden operator" {
				address = adapter.plans[4].Address
			}
			slot := crypto.Keccak256Hash([]byte("synthetic hidden mapping entry"))
			changed[string(safeCurrentTestNativeKey("AccountStorages", address, &slot))] = common.LeftPadBytes([]byte{1}, 32)
		case "binding":
			slot := common.BigToHash(big.NewInt(23))
			changed[string(safeCurrentTestNativeKey("AccountStorages", adapter.plans[4].Address, &slot))][31] ^= 1
		case "initialization clock":
			slot := common.BigToHash(new(big.Int).Add(new(big.Int).SetBytes(crypto.Keccak256(common.LeftPadBytes([]byte{6}, 32))), big.NewInt(1)))
			changed[string(safeCurrentTestNativeKey("AccountStorages", adapter.plans[4].Address, &slot))][23] ^= 1
		case "code metadata":
			changed[string(safeCurrentTestNativeKey("AccountCodesMetadata", adapter.plans[7].Address, nil))][0] ^= 1
		case "missing code":
			delete(changed, string(safeCurrentTestNativeKey("AccountCodes", adapter.plans[7].Address, nil)))
		}
		if err := check(changed); err == nil {
			t.Fatal("accepted changed initial storage", fault)
		}
	}
}

// A valid Safe success cannot complete a durable counted action if its exact
// binding event disappears after an interrupted terminal publication.
func TestBootstrapContractAnchorInterruptedOutcomeRequiresBindingEvent(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
		t.Fatal(err)
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	interrupted := errors.New("synthetic anchor intent publication interruption")
	owner.local.hook = func(stage string) error {
		if stage == bootstrapSuccessorExecutionEventName(2)+".intent:name-synced" {
			return interrupted
		}
		return nil
	}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false); !errors.Is(err, interrupted) {
		t.Fatal("anchor outcome interruption was not reached", err)
	}
	owner = f.open(false, nil)
	receipt := f.receipt()
	receipt.Receipt.Logs = receipt.Receipt.Logs[1:]
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: receipt}
	if result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false); err == nil || result.InstallationComplete {
		t.Fatal("missing event completed original custody", err)
	}
	owner = f.open(false, nil)
	f.resolution.Receipt = f.receipt()
	if result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false); err != nil || !result.InstallationComplete || result.CumulativeAttempts != 9 || len(f.writes) != 1 {
		t.Fatal("exact recovery lost original attempts", result, err)
	}
}
