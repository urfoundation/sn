// The concrete production capability uses real native proofs in the full local
// Safe graph. Public v2 opt-in and internal legacy qualification stay distinct.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/blake2b"
)

// Replace only the new empty native head's state commitment. Original receipt
// headers and EVM snapshots remain immutable and independently authenticated.
func bootstrapSuccessorSafeCurrentTestHeadWithLock(t *testing.T, chain *evmCreateFixture, entries map[string][]byte) safeCurrentStorageWitness {
	t.Helper()
	root, nodes := safeCurrentTestTrie(t, entries)
	oldHash := chain.hashes[chain.head]
	header := chain.headers[oldHash]
	encoded := native.Header{ParentHash: native.Hash(common.HexToHash(header.ParentHash)), Number: native.BlockNumber(chain.head),
		StateRoot: native.Hash(root), ExtrinsicsRoot: native.Hash(common.HexToHash(header.ExtrinsicsRoot))}
	raw, err := codec.Encode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	count, err := codec.Encode(native.NewUCompactFromUInt(uint64(len(header.Digest.Logs))))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], count...)
	for _, log := range header.Digest.Logs {
		value, err := hex.DecodeString(log[2:])
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, value...)
	}
	header.StateRoot = common.Hash(root).Hex()
	hash := common.Hash(blake2b.Sum256(raw)).Hex()
	delete(chain.headers, oldHash)
	chain.headers[hash], chain.hashes[chain.head] = header, hash
	return safeCurrentStorageWitness{At: hash, Header: header, Nodes: nodes}
}

// A signed acceptance cannot select a route on its own. Unknown routes, mixed
// history capabilities and a later runtime tip refuse before network admission.
func TestBootstrapSuccessorSafeCurrentCapabilityRequiresInstalledRoute(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	revision := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), revision); err != nil {
		t.Fatal(err)
	}
	adapter := &bootstrapSuccessorCanonicalChain{owner: owner, approval: base, currentRevisionHash: rootObjectHash(revision)}
	if err := adapter.selectCurrentPolicy(t.Context(), 0); err != nil || adapter.currentPolicy != nil {
		t.Fatal("signed acceptance installed a public capability route", err)
	}
	if err := adapter.selectCurrentPolicy(t.Context(), 99); !errors.Is(err, errBootstrapSuccessorSafeCurrentCapabilityUnavailable) {
		t.Fatal("unknown current-policy capability route was admitted", err)
	}
	adapter.provenance = &bootstrapSuccessorCanonicalFixture{}
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentNativeRoute); !errors.Is(err, errBootstrapSuccessorSafeCurrentCapabilityUnavailable) {
		t.Fatal("current proof was mixed with a complete-history claim", err)
	}
	adapter.provenance = nil
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentNativeRoute); err != nil || adapter.currentPolicy == nil || adapter.currentPolicy.scope.Safe != f.approval.Plan.Review.Transaction.Safe {
		t.Fatal("separate signed capability scope did not select", err)
	}
	if err := adapter.currentPolicyReady(t.Context(), f.approval.Plan, rootObjectHash(revision)); err == nil {
		t.Fatal("current-policy selection inferred missing original adapter custody")
	}
	if err := adapter.selectCurrentPolicy(t.Context(), 0); err != nil || adapter.currentPolicy != nil {
		t.Fatal("uninstalled route retained a previously selected capability", err)
	}
	runtime := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
	if err := owner.retainRuntimeRevision(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	adapter.runtimeRevisionHash = owner.runtimeHistory.hash()
	adapter.currentPolicy = nil
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentNativeRoute); err == nil || adapter.currentPolicy != nil {
		t.Fatal("old current policy silently widened to a new runtime tip", err)
	}
}

// Genuine prefix proofs drive the concrete adapter through public read-only
// import, hidden-authority refusal, proof-time mutation, one lost-reply send and
// later receipt recovery. The old synthetic history capability is never supplied.
func TestBootstrapSuccessorSafeCurrentCapabilityCommandPreservesExactSend(t *testing.T) {
	bootstrapSuccessorSafeCurrentTestCommandPreservesExactSend(t, false)
}

// The public entrypoint uses the same native proofs and exact custody, with a
// separately signed v2 acceptance and explicit hash opt-in on each attempted send.
func TestBootstrapSuccessorSafeCurrentPublicCommandPreservesExactSend(t *testing.T) {
	bootstrapSuccessorSafeCurrentTestCommandPreservesExactSend(t, true)
}

// Both routes must reach real proof/readmission boundaries, preserve a consumed
// reservation after refusal, and reconcile a lost reply without replacing bytes.
func bootstrapSuccessorSafeCurrentTestCommandPreservesExactSend(t *testing.T, public bool) {
	t.Helper()
	f := newBootstrapSuccessorCanonicalFixture(t)
	chain, plan := f.original.contracts, f.approval.Plan
	root := f.original.config.RunDirectory
	original := bootstrapSuccessorPreparationTestFiles(t, root)
	nonces := bootstrapSuccessorPreparationTestFiles(t, plan.Request.RegistryDirectory)
	model := &bootstrapSuccessorExecutionFixture{t: t, approval: f.approval, key: f.key}
	revision := bootstrapSuccessorSafeCurrentTestNext(t, model, f.canonical, bootstrapSuccessorRuntimeHistory{}, nil)
	if public {
		revision = bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, revision, f.key)
	}
	reference := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-current-policy-acceptance.json"), revision)
	var witness safeCurrentStorageWitness
	var proof *safeCurrentProofFixture
	var proofCalls int
	var mutateAfterProof bool
	var advanceAfterProof bool
	var reorgAfterProof bool
	var reorgHash string
	var lateFault string
	var lateInjected, pendingSeen, badCurrentCode bool
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		oracle := &safeExecutionFixture{state: chain.state, transaction: plan.transaction(), owners: slices.Clone(plan.Request.Owners)}
		proof = safeCurrentProofFixtureFromOracle(t, oracle, plan.Review.Request.Version, plan.Review.Request.Variant)
		proof.entries[":code"] = slices.Clone(chain.code)
		chain.advanceEmpty()
		witness = bootstrapSuccessorSafeCurrentTestHeadWithLock(t, chain, proof.entries)
		priorOverride := chain.override
		chain.override = func(method string, params []any, result any) any {
			result = priorOverride(method, params, result)
			if method == "chain_getBlockHash" && reorgHash != "" && result == reorgHash {
				return (common.Hash{31: 91}).Hex()
			}
			if lateFault != "" && proofCalls > 0 {
				if method == "eth_getTransactionCount" && params[1] == "pending" {
					pendingSeen = true
				}
				if method == "chain_getFinalizedHead" && pendingSeen && !lateInjected {
					lateInjected = true
					switch lateFault {
					case "Safe nonce":
						chain.state.SetState(plan.Review.Transaction.Safe, common.BigToHash(big.NewInt(5)), common.Hash{31: 99})
					case "relayer nonce":
						chain.state.SetNonce(plan.Review.Relayer.Sender, 43, tracing.NonceChangeUnspecified)
					case "same-version artifact":
						badCurrentCode = true
					}
				}
			}
			if badCurrentCode && method == "state_getStorageHash" && params[0] == runtimeCodeStorageKey {
				return (common.Hash{31: 88}).Hex()
			}
			return result
		}
		chain.nativeProof = func(params []any) any {
			if len(params) != 2 || params[1] != witness.At {
				return mappingFixtureRpcError{code: -32000}
			}
			proofCalls++
			if mutateAfterProof && proofCalls == 2 {
				chain.state.SetState(plan.Review.Transaction.Safe, common.BigToHash(big.NewInt(5)), common.Hash{31: 99})
			}
			response := map[string]any{"at": witness.At, "proof": witness.Nodes}
			if reorgAfterProof {
				reorgHash = witness.At
			}
			if advanceAfterProof {
				chain.advanceEmpty()
				witness = bootstrapSuccessorSafeCurrentTestHeadWithLock(t, chain, proof.entries)
			}
			return response
		}
	}()
	args := []string{"contract-successor-execution-resume", "--config", f.original.path, "--run-dir", root, "--accept-plan-hash", f.original.preparation.Plan.ContentHash}
	args = append(append(args, f.paths...), f.approvalArgs...)
	args = append(args, "--online", "--canonical-approval", f.canonicalRef.Path, "--canonical-approval-sha256", f.canonicalRef.Sha256,
		"--safe-current-revision", reference.Path, "--safe-current-revision-sha256", reference.Sha256)
	invoke := func(route bootstrapSuccessorSafeCurrentRoute, submit bool, selected []string) (int, bootstrapSuccessorExecutionResult, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		input := slices.Clone(selected)
		if submit {
			input = append(input, "--submit")
		}
		var code int
		if public || route == 0 {
			if route == bootstrapSuccessorSafeCurrentNativeRoute && submit {
				input = append(input, "--accept-safe-current-policy", rootObjectHash(revision))
			}
			code = runBootstrapSuccessorExecutionCommand(f.original.storageContext(t.Context()), input, &stdout, &stderr)
		} else {
			code = runBootstrapSuccessorExecutionCommandWithAuthorities(f.original.storageContext(t.Context()), input, &stdout, &stderr, nil, route)
		}
		var result bootstrapSuccessorExecutionResult
		if stdout.Len() != 0 {
			if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
		return code, result, stderr.String()
	}
	if code, _, diagnostic := invoke(0, true, args); code != 2 || !strings.Contains(diagnostic, errBootstrapSuccessorSafeProvenanceUnavailable.Error()) {
		t.Fatal("signed current-policy input enabled public submission", code, diagnostic)
	}
	if _, err := os.Stat(filepath.Join(root, bootstrapSuccessorSafeCurrentName(1))); !os.IsNotExist(err) {
		t.Fatal("public capability refusal changed policy custody", err)
	}
	if public {
		withoutAcceptance := append(slices.Clone(args[:len(args)-4]), "--accept-safe-current-policy", rootObjectHash(revision))
		if code, _, diagnostic := invoke(0, true, withoutAcceptance); code != 1 || !strings.Contains(diagnostic, errBootstrapSuccessorSafeCurrentCapabilityUnavailable.Error()) {
			t.Fatal("public opt-in alone enabled current-policy submission", code, diagnostic)
		}
		for _, fault := range []string{"read-only", "invalid hash"} {
			hash := rootObjectHash(revision)
			if fault == "invalid hash" {
				hash = "synthetic-invalid-acceptance-hash"
			}
			if code, _, diagnostic := invoke(0, fault != "read-only", append(slices.Clone(args), "--accept-safe-current-policy", hash)); code != 2 {
				t.Fatal("public current-policy opt-in accepted an invalid invocation", fault, code, diagnostic)
			}
		}
	}
	for _, fault := range []string{"missing digest", "wrong digest"} {
		changed := slices.Clone(args)
		if fault == "missing digest" {
			changed = changed[:len(changed)-2]
		} else {
			changed[len(changed)-1] = rootObjectHash("synthetic wrong current-policy input pin")
		}
		if code, _, _ := invoke(0, false, changed); code != 2 {
			t.Fatal("current-policy command accepted an unpinned input", fault, code)
		}
	}
	if code, result, diagnostic := invoke(0, false, args); code != 0 || result.SafeCurrentRevisionHash != rootObjectHash(revision) || result.SafeCurrentRevisionCount != 1 || result.CumulativeAttempts != 8 || result.SubmissionAttempted {
		t.Fatal("public read-only current-policy import failed", code, result, diagnostic)
	}
	if !public {
		legacyOptIn := append(slices.Clone(args), "--accept-safe-current-policy", rootObjectHash(revision))
		if code, _, diagnostic := invoke(0, true, legacyOptIn); code != 1 || !strings.Contains(diagnostic, "separately signed v2 acceptance") {
			t.Fatal("public opt-in upgraded legacy current-policy acceptance", code, diagnostic)
		}
	}
	if public {
		wrong := append(slices.Clone(args), "--accept-safe-current-policy", rootObjectHash("synthetic different accepted revision"))
		if code, _, diagnostic := invoke(0, true, wrong); code != 1 || !strings.Contains(diagnostic, "opt-in differs from the exact retained acceptance") {
			t.Fatal("public current-policy submission ignored the accepted revision", code, diagnostic)
		}
		if _, err := os.Stat(filepath.Join(root, bootstrapSuccessorExecutionEventName(1)+".json")); !os.IsNotExist(err) {
			t.Fatal("public acceptance refusal consumed a counted attempt", err)
		}
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			if len(chain.writes) != 8 || proofCalls != 0 {
				t.Fatal("public acceptance refusal reached network admission or a write")
			}
		}()
		// Recovery needs the immutable retained acceptance, not a replaceable
		// import file. Every following send still supplies its exact object hash.
		args = args[:len(args)-4]
	}
	func() {
		ctx := f.original.storageContext(t.Context())
		loaded, profile, retained, err := loadBootstrapSuccessorExecution(ctx, f.original.path, root,
			f.original.preparation.Plan.ContentHash, f.paths[1], f.paths[3], f.paths[5], f.approvalArgs[1], f.canonicalRef.Path)
		if err != nil {
			t.Fatal(err)
		}
		defer retained.close()
		owner, err := openBootstrapSuccessorExecutionStore(ctx, loaded, f.approval, profile, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer owner.close()
		adapter, err := newBootstrapSuccessorCanonicalChainWithAuthorities(ctx, owner, f.canonical, nil, bootstrapSuccessorSafeCurrentNativeRoute, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer adapter.close()
		if _, err := adapter.authenticate(t.Context(), loaded); err != nil {
			t.Fatal(err)
		}
		var snapshot safeCurrentStorageWitness
		var height uint64
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			snapshot, height, advanceAfterProof = witness, chain.head, true
		}()
		observation, err := adapter.observe(t.Context(), loaded)
		if err != nil {
			t.Fatal("current-policy advancing canonical snapshot refused", err)
		}
		if observation.SafeCurrentProof == nil || observation.SafeCurrentPending == nil ||
			observation.SafeCurrentProof.NativeHash != snapshot.At || observation.SafeCurrentProof.NativeNumber != height || observation.SafeCurrentProof.StateRoot != snapshot.Header.StateRoot ||
			observation.NativeNumber <= height || observation.NativeHash.Hex() == snapshot.At ||
			observation.SafeCurrentPending.FinalizedObservationHash != rootObjectHash(observation.SafeCurrentProof) ||
			!observation.SafeCurrentProof.CompleteFinalizedStorage || !observation.SafeCurrentPending.ScopedWordsMatched ||
			observation.SafeCurrentProof.DeploymentHistoryVerified || observation.SafeCurrentProof.CompletePendingVerified || observation.SafeCurrentProof.SendAuthorized ||
			observation.SafeCurrentPending.CompletePendingVerified || observation.SafeCurrentPending.SendAuthorized {
			t.Fatal("current-policy moving head relabeled or overstated its exact proof", observation)
		}
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			advanceAfterProof, reorgAfterProof = false, true
		}()
		if _, err := adapter.observe(t.Context(), loaded); err == nil || !strings.Contains(err.Error(), "snapshot is no longer canonical") || adapter.admitted {
			t.Fatal("current-policy reorg reused a previously admitted snapshot", err)
		}
		func() { chain.stateLock.Lock(); defer chain.stateLock.Unlock(); reorgAfterProof, reorgHash = false, "" }()
	}()
	for _, mapping := range []byte{1, 2} {
		orphan := common.Address{19: 231}
		slot := crypto.Keccak256Hash(common.LeftPadBytes(orphan[:], 32), common.LeftPadBytes([]byte{mapping}, 32))
		key := string(safeCurrentTestNativeKey("AccountStorages", plan.Review.Transaction.Safe, &slot))
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			word := common.Hash{31: 1}
			chain.state.SetState(plan.Review.Transaction.Safe, slot, word)
			proof.entries[key] = slices.Clone(word[:])
			witness = bootstrapSuccessorSafeCurrentTestHeadWithLock(t, chain, proof.entries)
		}()
		if code, _, diagnostic := invoke(bootstrapSuccessorSafeCurrentNativeRoute, true, args); code != 1 || !strings.Contains(diagnostic, "unapproved, orphan or noncanonical word") {
			t.Fatal("current-policy command accepted genuine hidden authority", mapping, code, diagnostic)
		}
		if _, err := os.Stat(filepath.Join(root, bootstrapSuccessorExecutionEventName(1)+".json")); !os.IsNotExist(err) {
			t.Fatal("orphan authority consumed a reservation before proof admission", mapping, err)
		}
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			chain.state.SetState(plan.Review.Transaction.Safe, slot, common.Hash{})
			delete(proof.entries, key)
			witness = bootstrapSuccessorSafeCurrentTestHeadWithLock(t, chain, proof.entries)
		}()
	}
	for _, fault := range []string{"Safe nonce", "relayer nonce", "same-version artifact"} {
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			lateFault, lateInjected, pendingSeen, proofCalls = fault, false, false, 0
		}()
		if code, _, diagnostic := invoke(bootstrapSuccessorSafeCurrentNativeRoute, true, args); code != 1 {
			t.Fatal("late current-policy change escaped final readmission", fault, code, diagnostic)
		}
		if _, err := os.Stat(filepath.Join(root, bootstrapSuccessorExecutionEventName(1)+".json")); !os.IsNotExist(err) {
			t.Fatal("late current-policy refusal consumed a reservation", fault, err)
		}
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			if !lateInjected || len(chain.writes) != 8 {
				t.Fatal("late current-policy barrier did not precede the next send", fault)
			}
			lateFault, lateInjected, pendingSeen, badCurrentCode = "", false, false, false
			chain.state.SetState(plan.Review.Transaction.Safe, common.BigToHash(big.NewInt(5)), common.BigToHash(big.NewInt(17)))
			chain.state.SetNonce(plan.Review.Relayer.Sender, 42, tracing.NonceChangeUnspecified)
		}()
	}
	func() { chain.stateLock.Lock(); defer chain.stateLock.Unlock(); mutateAfterProof, proofCalls = true, 0 }()
	if code, _, diagnostic := invoke(bootstrapSuccessorSafeCurrentNativeRoute, true, args); code != 1 || !strings.Contains(diagnostic, "pending authority or nonce differs") {
		t.Fatal("post-proof Safe mutation reused stale current-policy admission", code, diagnostic)
	}
	var counted bootstrapSuccessorExecutionEvent
	countedPath := filepath.Join(root, bootstrapSuccessorExecutionEventName(1)+".json")
	countedRaw, err := os.ReadFile(countedPath)
	if err != nil || decodePlanJson(countedRaw, &counted) != nil || counted.CumulativeAttempts != 9 || counted.SafeCurrentRevisionHash != rootObjectHash(revision) {
		t.Fatal("proof-time refusal reset its counted current-policy attempt", counted, err)
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if proofCalls != 2 || len(chain.writes) != 8 {
			t.Fatal("current-policy proof did not precede every counted send admission", proofCalls, len(chain.writes))
		}
		mutateAfterProof = false
		advanceAfterProof = true
		chain.state.SetState(plan.Review.Transaction.Safe, common.BigToHash(big.NewInt(5)), common.BigToHash(big.NewInt(17)))
		chain.loseReply = true
	}()
	if code, _, diagnostic := invoke(bootstrapSuccessorSafeCurrentNativeRoute, true, args); code != 1 {
		t.Fatal("current-policy exact send did not preserve its lost reply", code, diagnostic)
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 || !bytes.Equal(chain.writes[8], common.FromHex(plan.SignedRelayer)) || chain.receipt == nil || chain.receipt["status"] != "0x1" {
			t.Fatal("current-policy capability replaced exact bytes or omitted real Safe execution")
		}
		chain.loseReply = false
		chain.advanceEmpty()
	}()
	if code, result, diagnostic := invoke(0, false, args); code != 0 || !result.InstallationComplete || result.CumulativeAttempts != 10 || result.SubmissionAttempted || result.SafeCurrentRevisionHash != rootObjectHash(revision) {
		t.Fatal("public historical reconciliation lost current-policy custody", code, result, diagnostic)
	}
	for name, raw := range original {
		retained, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(retained) != raw {
			t.Fatal("current-policy capability rewrote original receipts or execution", name, err)
		}
	}
	retained, err := os.ReadFile(countedPath)
	if err != nil || !bytes.Equal(retained, countedRaw) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, plan.Request.RegistryDirectory)) {
		t.Fatal("current-policy recovery changed a counted attempt or nonce claim", err)
	}
	terminalRaw, err := os.ReadFile(filepath.Join(root, bootstrapSuccessorExecutionEventName(3)+".json"))
	var terminal bootstrapSuccessorExecutionEvent
	if err != nil || json.Unmarshal(terminalRaw, &terminal) != nil || terminal.SafeCurrentRevisionHash != rootObjectHash(revision) || terminal.ReservedLifetimeWei != counted.ReservedLifetimeWei {
		t.Fatal("current-policy terminal outcome lost counted authority or liability", terminal, err)
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 || chain.counts["txpool_content"] != 0 || chain.counts["state_getReadProof"] != 11 {
			t.Fatal("current-policy recovery resent or bypassed exact native proofs")
		}
	}()
}
