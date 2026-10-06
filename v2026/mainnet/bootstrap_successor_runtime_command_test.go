// A complete local native/EVM/Safe graph exercises runtime changes after
// reservation and across historical inclusion while every custody file survives.
package main

import (
	"bytes"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/blake2b"
)

// Full history exceeds the CRv4 per-call cap. Only the independently approved
// inclusion/parent pair is authenticated; a later unapproved upgrade cannot
// erase the historical result or free the failed reservation before that send.
func TestBootstrapSuccessorRuntimeRevisionCommandPreservesUpgradeCustody(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	chain, plan := f.original.contracts, f.approval.Plan
	root := f.original.config.RunDirectory
	original := bootstrapSuccessorPreparationTestFiles(t, root)
	nonces := bootstrapSuccessorPreparationTestFiles(t, plan.Request.RegistryDirectory)
	baseOverride := chain.override
	var revisions []bootstrapSuccessorRuntimeApproval
	var codes [][]byte
	for i := range 12 {
		profile := f.canonical.Authorization.CurrentRuntime
		profile.RuntimeVersion.SpecVersion += uint32(i + 1)
		code := append(bytes.Clone(chain.code), byte(i+1))
		hash := blake2b.Sum256(code)
		profile.RuntimeCodeHash = "0x" + hex.EncodeToString(hash[:])
		revisions = append(revisions, bootstrapSuccessorRuntimeTestApproval(t, plan, f.canonical, f.key, revisions, profile))
		codes = append(codes, code)
	}
	firstRef := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-first-runtime-revision.json"), revisions[0])
	lastRef := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-last-runtime-revision.json"), revisions[11])
	// Before first upgrade, no future observation has imported its authority.
	provenanceCalls := 0
	var firstUpgrade, inclusionUpgrade, laterUpgrade uint64
	var artifactFault bool
	f.afterProvenance = func() {
		provenanceCalls++
		if provenanceCalls != 2 {
			return
		}
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		chain.advanceEmpty()
		firstUpgrade = chain.head
		chain.override = func(method string, params []any, result any) any {
			result = baseOverride(method, params, result)
			var hash string
			switch method {
			case "state_getRuntimeVersion", "state_getMetadata":
				hash, _ = params[0].(string)
			case "state_getStorageHash", "state_getStorage":
				if params[0] != runtimeCodeStorageKey {
					return result
				}
				hash, _ = params[1].(string)
			default:
				return result
			}
			for number, nativeHash := range chain.hashes {
				if nativeHash != hash || number < firstUpgrade {
					continue
				}
				index := 0
				if inclusionUpgrade != 0 && number >= inclusionUpgrade {
					index = 11
				}
				profile := revisions[index].Authorization.Runtime
				if laterUpgrade != 0 && number >= laterUpgrade {
					profile.RuntimeVersion.SpecVersion++
				}
				switch method {
				case "state_getRuntimeVersion":
					return profile.RuntimeVersion
				case "state_getStorageHash":
					if artifactFault && number == inclusionUpgrade {
						return (common.Hash{7}).Hex()
					}
					return profile.RuntimeCodeHash
				case "state_getStorage":
					return "0x" + hex.EncodeToString(codes[index])
				}
			}
			return result
		}
	}
	var stdout bytes.Buffer
	if code, diagnostic := f.online(&stdout, true); code != 1 || !strings.Contains(diagnostic, "current runtime differs from its independent successor approval") || provenanceCalls != 2 {
		t.Fatal("unapproved runtime after reservation was not refused", code, provenanceCalls, diagnostic)
	}
	f.afterProvenance = nil
	var counted bootstrapSuccessorExecutionEvent
	countedPath := filepath.Join(root, bootstrapSuccessorExecutionEventName(1)+".json")
	countedRaw, err := os.ReadFile(countedPath)
	if err != nil || decodePlanJson(countedRaw, &counted) != nil || counted.CumulativeAttempts != 9 || counted.RuntimeRevisionHash != "" || counted.CanonicalAuthorityHash != rootObjectHash(f.canonical) {
		t.Fatal("runtime change reset or rebound its already counted attempt", counted, err)
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 8 || chain.state.GetNonce(plan.Review.Relayer.Sender) != 42 {
			t.Fatal("unapproved post-reservation runtime sent a transaction")
		}
	}()
	args := append(append([]string{}, f.approvalArgs...), "--online", "--canonical-approval", f.canonicalRef.Path, "--canonical-approval-sha256", f.canonicalRef.Sha256,
		"--runtime-revision", firstRef.Path, "--runtime-revision-sha256", firstRef.Sha256)
	publicArgs := []string{"contract-successor-execution-resume", "--config", f.original.path, "--run-dir", root, "--accept-plan-hash", f.original.preparation.Plan.ContentHash}
	publicArgs = append(append(append(publicArgs, f.paths...), args...), "--submit")
	var publicError bytes.Buffer
	stdout.Reset()
	if code := runBootstrapSuccessorExecutionCommand(f.original.storageContext(t.Context()), publicArgs, &stdout, &publicError); code != 2 || !strings.Contains(publicError.String(), errBootstrapSuccessorSafeProvenanceUnavailable.Error()) || stdout.Len() != 0 {
		t.Fatal("signed runtime revision unlocked the public provenance gate", code, publicError.String())
	}
	if _, err := os.Stat(filepath.Join(root, bootstrapSuccessorRuntimeName(1))); !os.IsNotExist(err) {
		t.Fatal("public runtime submission refusal mutated revision custody", err)
	}
	for _, fault := range []string{"missing digest", "wrong digest", "wrong signature"} {
		changedArgs := append([]string{}, args...)
		switch fault {
		case "missing digest":
			changedArgs = changedArgs[:len(changedArgs)-2]
		case "wrong digest":
			changedArgs[len(changedArgs)-1] = rootObjectHash("synthetic wrong runtime file digest")
		case "wrong signature":
			changed := revisions[0]
			changed.Signature = f.canonical.Signature
			reference := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-invalid-runtime-revision.json"), changed)
			changedArgs[len(changedArgs)-3], changedArgs[len(changedArgs)-1] = reference.Path, reference.Sha256
		}
		stdout.Reset()
		if code, diagnostic := f.invoke("contract-successor-execution-resume", &stdout, changedArgs...); code != 2 || stdout.Len() != 0 {
			t.Fatal("runtime command accepted an unpinned or unsigned revision", fault, code, diagnostic)
		}
	}
	stdout.Reset()
	if code, diagnostic := f.invoke("contract-successor-execution-resume", &stdout, args...); code != 0 {
		t.Fatal("exact independently signed runtime revision did not import", code, diagnostic)
	}
	var imported bootstrapSuccessorExecutionResult
	if err := decodePlanJson(stdout.Bytes(), &imported); err != nil || imported.RuntimeRevisionCount != 1 || imported.RuntimeRevisionHash != rootObjectHash(revisions[0]) || imported.CumulativeAttempts != 9 || imported.SubmissionAttempted {
		t.Fatal("runtime import lost reviewed tip or consumed another attempt", imported, err)
	}
	owner, adapter, closeAll := f.openRuntimeRevisions(revisions[1:]...)
	if len(adapter.runtimeProfiles) != 13 || owner.last.CumulativeAttempts != 9 || owner.last.RuntimeRevisionHash != "" {
		t.Fatal("full retained artifact history was capped or rebound old custody")
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		inclusionUpgrade = chain.head + 1
		chain.loseReply = true
	}()
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, adapter, true)
	if err == nil || !result.SubmissionAttempted || owner.last.CumulativeAttempts != 10 || owner.last.RuntimeRevisionHash != rootObjectHash(revisions[11]) {
		t.Fatal("approved runtime continuation renewed old capacity or lost exact counted authority", result, err)
	}
	closeAll()
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 || chain.receipt == nil || chain.receipt["status"] != "0x1" || !bytes.Equal(chain.writes[8], common.FromHex(plan.SignedRelayer)) || chain.head != inclusionUpgrade {
			t.Fatal("runtime migration changed the exact signed send or historical inclusion", len(chain.writes), chain.receipt)
		}
		chain.loseReply = false
		chain.advanceEmpty()
		laterUpgrade = chain.head
		artifactFault = true
	}()
	args[len(args)-3], args[len(args)-1] = lastRef.Path, lastRef.Sha256
	stdout.Reset()
	if code, diagnostic := f.invoke("contract-successor-execution-resume", &stdout, args...); code != 1 || stdout.Len() != 0 {
		t.Fatal("historical pair admitted a changed inclusion artifact", code, diagnostic)
	}
	func() { chain.stateLock.Lock(); defer chain.stateLock.Unlock(); artifactFault = false }()
	stdout.Reset()
	if code, diagnostic := f.invoke("contract-successor-execution-resume", &stdout, args...); code != 0 {
		t.Fatal("more than ten runtime profiles lost exact historical inclusion across an upgrade", code, diagnostic)
	}
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil || !result.InstallationComplete || result.RuntimeRevisionCount != 12 || result.RuntimeRevisionHash != rootObjectHash(revisions[11]) || result.CumulativeAttempts != 10 || result.SubmissionAttempted {
		t.Fatal("later unapproved runtime erased original inclusion or retained liability", result, err)
	}
	for name, raw := range original {
		retained, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(retained) != raw {
			t.Fatal("runtime migration rewrote original receipts, signatures or adoption", name, err)
		}
	}
	retained, err := os.ReadFile(countedPath)
	if err != nil || !bytes.Equal(countedRaw, retained) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, plan.Request.RegistryDirectory)) {
		t.Fatal("runtime migration rewrote a prior counted attempt or nonce claim", err)
	}
	terminalRaw, err := os.ReadFile(filepath.Join(root, bootstrapSuccessorExecutionEventName(3)+".json"))
	var terminal bootstrapSuccessorExecutionEvent
	if err != nil || decodePlanJson(terminalRaw, &terminal) != nil || terminal.RuntimeRevisionHash != rootObjectHash(revisions[11]) || terminal.ReservedLifetimeWei != counted.ReservedLifetimeWei {
		t.Fatal("runtime terminal custody lost its artifact authority or original liability", terminal, err)
	}
	if terminal.Receipt == nil || terminal.Receipt.NativeNumber != inclusionUpgrade || terminal.Receipt.NativeNumber == terminal.Receipt.Receipt.BlockNumber {
		t.Fatal("runtime inclusion confused native and EVM positions")
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 || chain.state.GetNonce(plan.Review.Relayer.Sender) != 43 || chain.counts["txpool_content"] != 0 {
			t.Fatal("historical runtime recovery resent or changed scoped nonce authority")
		}
	}()
}
