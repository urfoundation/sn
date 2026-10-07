// Synthetic storage proofs exercise admission independently of signatures or
// a live chain. Optional retained originals test the same mathematical guard.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/validator"
	"golang.org/x/crypto/blake2b"
)

// The independent fixture encoder uses the literal source-defined storage key.
func nativeMigrationTestWitness(t *testing.T, marker []byte) (safeCurrentStorageWitness, [32]byte) {
	t.Helper()
	key, err := hex.DecodeString("658faa385070e074c85bf6b568cf05556830ac129ea89510a686ad696de131c4906d6967726174655f616c7068615f76325f616e645f756e7374616b655f647573745f7631")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, nativeTreasuryMigrationKey()) {
		t.Fatal("migration storage key changed its reviewed Identity SCALE encoding")
	}
	code := []byte("synthetic native runtime with a separately authenticated hashed value")
	entries := map[string][]byte{":code": code}
	if marker != nil {
		entries[string(key)] = marker
	}
	witness := safeCurrentTestWitness(t, entries)
	external := "0x" + hex.EncodeToString(code)
	witness.Nodes = slices.DeleteFunc(witness.Nodes, func(node string) bool { return node == external })
	return witness, blake2b.Sum256(code)
}

// A fresh synthetic approval signs the completed opening, never old authority.
func nativeMigrationTestAuthority(t *testing.T) (*nativeTreasuryAuthority, economicEmissionPolicy) {
	t.Helper()
	authority, policy := nativeTreasuryTestAuthority(t)
	witness, code := nativeMigrationTestWitness(t, []byte{1})
	authority.MigrationComplete = &witness
	policy.Runtime.RuntimeSourceCommit = crv4.NativeOwnerSource473
	policy.Runtime.RuntimeVersion.SpecVersion = 473
	policy.Runtime.RuntimeCodeHash = fmt.Sprintf("0x%x", code)
	policy.From = economicEmissionBoundary{Number: 703, Hash: witness.At}
	policy.Through = economicEmissionBoundary{Number: 704, Hash: "0x" + strings.Repeat("43", 32)}
	nativeTreasuryTestBindPolicy(t, authority, policy)
	return authority, policy
}

// The complete original opening is part of both independently signed approval
// and the producer's original principal baseline; later frames cannot move it.
func TestNativeTreasuryMigrationCompletedOpeningRequiresFreshAuthority(t *testing.T) {
	authority, policy := nativeMigrationTestAuthority(t)
	if err := authority.validateScope(policy, policy.Through, nil); err != nil {
		t.Fatal("completed opening with fresh approval refused", err)
	}
	if nativeExecutionRuntimeSource(authority) != crv4.NativeOwnerSource473 {
		t.Fatal("producer lost the independently approved successor source")
	}
	missing := *authority
	missing.MigrationComplete = nil
	if err := missing.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("source473 acquired economic authority without completion proof")
	}
	changed := *authority
	changed.Approval = bytes.Clone(authority.Approval)
	changed.Approval[len(changed.Approval)-1] ^= 1
	if changed.validate() == nil || nativeExecutionRuntimeSource(&changed) != "" {
		t.Fatal("altered original approval selected economic source authority")
	}
}

// Neither a null marker, a false marker nor a malformed bool proves completion.
func TestNativeTreasuryMigrationRefusesIncompleteOriginalState(t *testing.T) {
	for index, marker := range [][]byte{nil, {}, {0}, {1, 0}, {2}} {
		witness, code := nativeMigrationTestWitness(t, marker)
		authority := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
		runtime := validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource473, CodeHash: code}
		if err := authority.validateMigration(runtime); err == nil {
			t.Fatal("incomplete original state accepted as completed", index)
		}
	}
}

// All content remains bounded and committed to the exact signed parent and code.
func TestNativeTreasuryMigrationRejectsProofRebindingAndOmission(t *testing.T) {
	witness, code := nativeMigrationTestWitness(t, []byte{1})
	base := nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
	runtime := validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource473, CodeHash: code}
	for _, fault := range []string{"at", "activation-number", "header", "code", "missing-node", "duplicate-node", "node-count", "byte-bound", "historical"} {
		candidate := ownerTrimTestCopy(t, base)
		pin := runtime
		switch fault {
		case "at":
			candidate.MigrationComplete.At = "0x" + strings.Repeat("44", 32)
		case "activation-number":
			candidate.Deployment.Activation.Number++
		case "header":
			candidate.MigrationComplete.Header.StateRoot = "0x" + strings.Repeat("44", 32)
		case "code":
			pin.CodeHash[0] ^= 1
		case "missing-node":
			candidate.MigrationComplete.Nodes = []string{"0x00"}
		case "duplicate-node":
			candidate.MigrationComplete.Nodes = append(candidate.MigrationComplete.Nodes, candidate.MigrationComplete.Nodes[0])
		case "node-count":
			candidate.MigrationComplete.Nodes = make([]string, nativeTreasuryMigrationNodes+1)
		case "byte-bound":
			candidate.MigrationComplete.Nodes = []string{"0x" + strings.Repeat("aa", nativeTreasuryMigrationBytes+1)}
		case "historical":
			pin.SourceCommit = crv4.NativeOwnerSource470
		}
		if err := candidate.validateMigration(pin); err == nil {
			t.Fatal("migration proof escaped original custody", fault)
		}
	}
	if err := base.validateMigration(runtime); err != nil {
		t.Fatal("unchanged bounded proof no longer admitted", err)
	}
}

// Height alone is not ancestry. The fresh principal baseline is the proved root.
func TestNativeTreasuryMigrationKeepsExactOriginalPrincipalParent(t *testing.T) {
	authority, policy := nativeMigrationTestAuthority(t)
	principal := nativeTreasuryTestPrincipal(authority, policy.From)
	if err := validateNativeTreasuryPrincipal(authority, principal); err != nil {
		t.Fatal("exact opening principal refused", err)
	}
	for _, parent := range []economicEmissionBoundary{
		{Number: policy.From.Number - 1, Hash: policy.From.Hash},
		{Number: policy.From.Number + 1, Hash: policy.From.Hash},
		{Number: policy.From.Number, Hash: "0x" + strings.Repeat("45", 32)},
	} {
		changed := *principal
		changed.Parent = parent
		if err := validateNativeTreasuryPrincipal(authority, &changed); err == nil {
			t.Fatal("another opening acquired post-migration principal authority", parent)
		}
	}
}

// Reading a commitment does not manufacture a missing value or weaken old reads.
func TestNativeTreasuryMigrationCodeCommitmentRetainsMissingValueRefusal(t *testing.T) {
	witness, expected := nativeMigrationTestWitness(t, []byte{1})
	trie, err := newSafeCurrentStorageTrie(t.Context(), witness.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rootReceiptHex(witness.Header.StateRoot, 32)
	if err != nil {
		t.Fatal(err)
	}
	var root [32]byte
	copy(root[:], raw)
	if hash, present, err := trie.commitment(t.Context(), root, []byte(":code")); err != nil || !present || hash != expected {
		t.Fatal("authenticated external commitment refused", hash, present, err)
	}
	if value, present, err := trie.read(t.Context(), root, []byte(":code")); err == nil || present || value != nil {
		t.Fatal("commitment manufactured missing external bytes", value, present, err)
	}
	if hash, present, err := trie.commitment(t.Context(), root, []byte("absent")); err != nil || present || hash != ([32]byte{}) {
		t.Fatal("proved absence became a commitment", hash, present, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := trie.commitment(ctx, root, []byte(":code")); err == nil {
		t.Fatal("commitment traversal escaped caller cancellation")
	}
}

// The optional field leaves old signed policy encoding and capability unchanged.
func TestNativeTreasuryMigrationDoesNotRewriteHistoricalAuthority(t *testing.T) {
	authority, policy := nativeTreasuryTestAuthority(t)
	raw, err := json.Marshal(authority)
	if err != nil || bytes.Contains(raw, []byte("migration_complete")) {
		t.Fatal("nil migration field changed historical wire shape", err)
	}
	if nativeExecutionRuntimeSource(authority) != crv4.NativeOwnerSource470 || authority.validateScope(policy, policy.Through, nil) != nil {
		t.Fatal("historical independent authority no longer admitted")
	}
	witness, _ := nativeMigrationTestWitness(t, []byte{1})
	authority.MigrationComplete = &witness
	if err := authority.validate(); err == nil {
		t.Fatal("historical authority silently acquired new migration purpose")
	}
}

// Retained originals remain outside source control. Their independently selected
// file hash and both positive/negative original parents are required together.
func TestNativeTreasuryMigrationRetainedOriginalParents(t *testing.T) {
	path := os.Getenv("SN_NATIVE_TREASURY_MIGRATION_CASES_FILE")
	digest := os.Getenv("SN_NATIVE_TREASURY_MIGRATION_CASES_SHA256")
	if path == "" && digest == "" {
		t.Skip("retained native migration originals were not selected")
	}
	if path == "" || !rootCanonicalHash("0x"+digest) {
		t.Fatal("retained migration originals require a path and independent hash")
	}
	raw, _, err := readPlanFile(t.Context(), path, 2*nativeTreasuryMigrationBytes)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		t.Fatal("retained migration originals changed", err)
	}
	var input struct {
		Schema string `json:"schema"`
		Cases  []struct {
			Name            string                    `json:"name"`
			Complete        bool                      `json:"complete"`
			Number          uint64                    `json:"number"`
			RuntimeCodeHash string                    `json:"runtime_code_hash"`
			Witness         safeCurrentStorageWitness `json:"witness"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || input.Schema != "native-treasury-migration-original-cases-v1" || len(input.Cases) != 2 {
		t.Fatal("retained migration case shape differs", err)
	}
	completed := 0
	for _, value := range input.Cases {
		code, err := rootReceiptHex(value.RuntimeCodeHash, 32)
		if err != nil {
			t.Fatal(err)
		}
		runtime := validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource473}
		copy(runtime.CodeHash[:], code)
		// A decoding or incomplete-path error cannot count as the negative's
		// expected missing marker. First prove the original fixture's cause.
		number, err := value.Witness.Header.authenticate(value.Witness.At)
		if err != nil || number != value.Number {
			t.Fatal("retained original header failed authentication", value.Name, err)
		}
		rootBytes, err := rootReceiptHex(value.Witness.Header.StateRoot, 32)
		if err != nil {
			t.Fatal(err)
		}
		var root [32]byte
		copy(root[:], rootBytes)
		trie, err := newSafeCurrentStorageTrie(t.Context(), value.Witness.Nodes)
		if err != nil {
			t.Fatal("retained original trie is malformed", value.Name, err)
		}
		if hash, present, err := trie.commitment(t.Context(), root, []byte(":code")); err != nil || !present || hash != runtime.CodeHash {
			t.Fatal("retained original code commitment differs", value.Name, err)
		}
		marker, present, err := trie.read(t.Context(), root, nativeTreasuryMigrationKey())
		if err != nil || present != value.Complete || value.Complete && !bytes.Equal(marker, []byte{1}) {
			t.Fatal("retained original marker cause differs", value.Name, present, marker, err)
		}
		authority := &nativeTreasuryAuthority{MigrationComplete: &value.Witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: value.Number, Hash: value.Witness.At}}}
		err = authority.validateMigration(runtime)
		if (err == nil) != value.Complete {
			t.Fatal("original migration state differed from independently retained outcome", value.Name, err)
		}
		if value.Complete {
			completed++
		}
	}
	if completed != 1 {
		t.Fatal("retained cases lost the actual completed and incomplete originals")
	}
}
