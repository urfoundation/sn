//go:build linux || darwin

// The actual public command reconstructs eight retained actions, original
// signatures and restored nonce custody. All keys and routes are synthetic.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A wire mirror lets the unchanged old dispatcher compile the causal control.
// Its signing-byte equality is checked against the actual public preview.
type registryRebindTestPlan struct {
	Schema                   string                         `json:"schema"`
	ExecutionApprovalHash    string                         `json:"execution_approval_hash"`
	RegistryDirectory        string                         `json:"registry_directory"`
	OriginalRegistry         bootstrapSuccessorRootIdentity `json:"original_registry"`
	RestoredRegistry         bootstrapSuccessorRootIdentity `json:"restored_registry"`
	RestoredGeneration       string                         `json:"restored_generation_sha256"`
	RuntimeDeclaration       durablevolume.Reference        `json:"runtime_declaration"`
	RestorePlan              durablevolume.Reference        `json:"restore_plan"`
	OriginalInventory        durablevolume.Reference        `json:"original_inventory"`
	OriginalFormerWriter     durablevolume.Reference        `json:"original_former_writer_fence"`
	OriginalMemberCensusHash string                         `json:"original_member_census_sha256"`
	DerivedMemberCensusHash  string                         `json:"derived_member_census_sha256"`
}

// The existing public fixture supplies real completed execution custody. Its
// already joined registry is exported/restored through the actual dispatcher.
func registryRebindTestRestore(t *testing.T, f *bootstrapSuccessorCanonicalFixture) (string, string) {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	original := f.original.root.storage
	declaration, err := durablevolume.Load(original.Reference)
	if err != nil {
		t.Fatal(err)
	}
	registry := f.approval.Plan.Request.RegistryDirectory
	source.root, source.storage, source.ctx = registry, original, f.original.storageContext(t.Context())
	owner := storagePreparationDirectoryOwner(t, "mainnet-successor-nonce-members")
	var request durablevolume.PreparationRequest
	raw, err := os.ReadFile(source.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	request.RootPath = registry
	request.Owners = []durablevolume.PreparationOwner{owner}
	volume := declaration.Volumes[0]
	request.MountPath, request.FilesystemUuid, request.FilesystemType = volume.MountPath, volume.FilesystemUuid, volume.FilesystemType
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, source, source.ctx, owner, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, storage.target, "storage-prepare")
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("actual registry storage restore failed", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("storage copy acquired execution authority", err)
	}
	restored, err := durablevolume.Load(result.Declaration)
	if err != nil || len(restored.Volumes) != 1 || len(restored.Volumes[0].StateRoots) != 1 {
		t.Fatal(err)
	}
	found := false
	for index := range declaration.Volumes {
		for root := range declaration.Volumes[index].StateRoots {
			if declaration.Volumes[index].StateRoots[root].Path == registry {
				declaration.Volumes[index].StateRoots[root] = restored.Volumes[0].StateRoots[0]
				found = true
			}
		}
	}
	if !found {
		t.Fatal("original runtime declaration did not retain the registry root")
	}
	raw, err = json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	declarationPath := filepath.Join(storage.target.metadata, "reviewed-combined-runtime.json")
	if err := os.WriteFile(declarationPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reference := durablevolume.Reference{Path: declarationPath, Sha256: durablefixture.Digest(raw)}
	// This test-only declaration update does not enroll any root or head. It
	// preserves all original local roots and the exact publicly restored tuple.
	f.original.root.storage = &durablefixture.Fixture{Reference: reference, Host: original.Host, Roots: append([]string(nil), original.Roots...),
		Context: durablepath.WithHost(durablevolume.WithReference(t.Context(), reference), original.Host)}
	f.original.contracts.storage = f.original.root.storage
	return path, hash
}

func registryRebindTestSign(t *testing.T, path string, plan registryRebindTestPlan, key ed25519.PrivateKey, domain string) planFileReference {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	approval := struct {
		Schema    string                 `json:"schema"`
		Plan      registryRebindTestPlan `json:"plan"`
		Signature string                 `json:"signature_ed25519"`
	}{Schema: "urnetwork-mainnet-successor-registry-rebind-envelope-v1", Plan: plan,
		Signature: hex.EncodeToString(ed25519.Sign(key, append([]byte(domain+"\x00"), raw...)))}
	return bootstrapRootTestWrite(t, path, approval)
}

// Invalid domains, approvers, plan pins, physical generations and declarations
// refuse before local history changes. Valid resume retains every original
// signed byte and adds exactly one bounded physical-adoption receipt.
func TestBootstrapSuccessorRegistryRebindPublicResumeKeepsOriginalAuthority(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	invoke := func(command string, output *bytes.Buffer, extra ...string) (int, string) {
		var diagnostic bytes.Buffer
		args := append(append(append([]string{}, f.paths...), f.approvalArgs...), extra...)
		code := f.original.command(t.Context(), command, output, &diagnostic, args...)
		return code, diagnostic.String()
	}
	originalApproval, err := os.ReadFile(f.approvalArgs[1])
	if err != nil {
		t.Fatal(err)
	}
	local := f.original.config.RunDirectory
	registry := f.approval.Plan.Request.RegistryDirectory
	beforeLocal := bootstrapSuccessorPreparationTestFiles(t, local)
	originalNonces := bootstrapSuccessorPreparationTestFiles(t, registry)
	f.original.contracts.stateLock.Lock()
	counts, writes := maps.Clone(f.original.contracts.counts), len(f.original.contracts.writes)
	f.original.contracts.stateLock.Unlock()
	f.original.census.stateLock.Lock()
	censusCounts := maps.Clone(f.original.census.methodCounts)
	f.original.census.stateLock.Unlock()
	path, hash := registryRebindTestRestore(t, f)
	var output bytes.Buffer
	if code, diagnostic := invoke("contract-successor-execution-resume", &output); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic, "accepted hash differs") {
		t.Fatal("restored registry silently changed old signed root authority", code, diagnostic)
	}
	if code, diagnostic := invoke("contract-successor-execution-rebind-preview", &output, "--registry-restore-plan", path, "--registry-restore-plan-sha256", hash); code != 0 {
		t.Fatal("public restored-registry rebind review is unavailable", code, diagnostic)
	}
	var preview struct {
		Schema       string                 `json:"schema"`
		Plan         registryRebindTestPlan `json:"plan"`
		SigningBytes string                 `json:"signing_bytes"`
	}
	if err := json.Unmarshal(output.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	domain := "urnetwork-mainnet-successor-registry-rebind-v1"
	planRaw, err := json.Marshal(preview.Plan)
	if err != nil || preview.SigningBytes != "0x"+hex.EncodeToString(append([]byte(domain+"\x00"), planRaw...)) || preview.Plan.ExecutionApprovalHash != rootObjectHash(f.approval) || preview.Plan.OriginalRegistry != f.approval.Plan.Registry {
		t.Fatal("public rebind domain changed original approval authority", err)
	}
	if !maps.Equal(beforeLocal, bootstrapSuccessorPreparationTestFiles(t, local)) {
		t.Fatal("unsigned rebind preview changed execution custody")
	}
	baselineNonce := bootstrapSuccessorPreparationTestFiles(t, registry)
	for name, raw := range originalNonces {
		if name != ".successor-nonce-members.json" && baselineNonce[name] != raw {
			t.Fatal("physical restore changed original nonce claim bytes", name)
		}
	}
	approvalPath := filepath.Join(filepath.Dir(path), "synthetic-registry-rebind-approval.json")
	for _, mode := range []string{"approver", "domain", "generation", "plan", "declaration", "plan-lineage", "declaration-lineage"} {
		plan, key, signingDomain := preview.Plan, f.key, domain
		expectedRefusal := "successor registry approval differs from exact restored lineage"
		switch mode {
		case "approver":
			key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{87}, ed25519.SeedSize))
			expectedRefusal = "successor registry independent rebind approval is invalid"
		case "domain":
			signingDomain = "urnetwork-mainnet-successor-execution-approval-v1"
			expectedRefusal = "successor registry independent rebind approval is invalid"
		case "generation":
			plan.RestoredRegistry.Inode++
		case "plan":
			// A zero digest is malformed before any lineage file is read.
			plan.RestorePlan.Sha256 = "sha256:" + strings.Repeat("0", 64)
			expectedRefusal = "successor registry independent rebind approval is invalid"
		case "declaration":
			plan.RuntimeDeclaration.Sha256 = "sha256:" + strings.Repeat("0", 64)
			expectedRefusal = "successor registry independent rebind approval is invalid"
		case "plan-lineage":
			// A canonical but wrong digest passes envelope admission and must
			// still refuse the exact original reviewed restore lineage.
			plan.RestorePlan.Sha256 = "sha256:" + strings.Repeat("a", 64)
		case "declaration-lineage":
			plan.RuntimeDeclaration.Sha256 = "sha256:" + strings.Repeat("b", 64)
		}
		ref := registryRebindTestSign(t, approvalPath, plan, key, signingDomain)
		output.Reset()
		code, diagnostic := invoke("contract-successor-execution-resume", &output, "--registry-rebind-approval", ref.Path, "--registry-rebind-approval-sha256", ref.Sha256)
		if code != 1 || output.Len() != 0 {
			t.Fatal("invalid registry approval admitted execution", mode, code, diagnostic)
		}
		if !maps.Equal(beforeLocal, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(baselineNonce, bootstrapSuccessorPreparationTestFiles(t, registry)) {
			t.Fatal("refused registry approval changed original custody", mode)
		}
		// Collect all diagnostic mismatches only after the semantic refusal
		// and exact original-byte checks. A message mismatch must not hide
		// the remaining fault cases or the valid retained resume control.
		if !strings.HasPrefix(diagnostic, "successor execution custody unresolved; retain original and nonce registry files:") || !strings.Contains(diagnostic, expectedRefusal) {
			t.Errorf("registry approval refusal cause differs: mode=%s code=%d diagnostic=%s", mode, code, diagnostic)
		}
	}
	ref := registryRebindTestSign(t, approvalPath, preview.Plan, f.key, domain)
	extra := []string{"--registry-rebind-approval", ref.Path, "--registry-rebind-approval-sha256", ref.Sha256}
	generation := storagePreparationOwnerAttribute(t, registry, durablevolume.RootGenerationAttribute)
	changedGeneration := bytes.Clone(generation)
	changedGeneration[0] ^= 1
	if err := unix.Setxattr(registry, durablevolume.RootGenerationAttribute, changedGeneration, unix.XATTR_REPLACE); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code, diagnostic := invoke("contract-successor-execution-resume", &output, extra...); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic, "identity") {
		t.Fatal("approved rebind admitted a different actual physical generation", code, diagnostic)
	}
	if !maps.Equal(beforeLocal, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(baselineNonce, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("refused actual generation change mutated original custody")
	}
	if err := unix.Setxattr(registry, durablevolume.RootGenerationAttribute, generation, unix.XATTR_REPLACE); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code, _ := invoke("contract-successor-execution-claim", &output, extra...); code != 2 || output.Len() != 0 {
		t.Fatal("registry physical approval enabled a fresh claim", code)
	}
	var completed map[string]string
	for attempt := 0; attempt < 2; attempt++ {
		output.Reset()
		if code, diagnostic := invoke("contract-successor-execution-resume", &output, extra...); code != 0 {
			t.Fatal("public exact restored-registry resume failed", attempt, code, diagnostic)
		}
		var result bootstrapSuccessorExecutionResult
		if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.CumulativeAttempts != 8 || result.SubmissionAttempted || result.CanonicalAdoptionVerified || result.InstallationComplete || result.ActivationReady {
			t.Fatal("registry rebind gained network or allowance authority", err)
		}
		current := bootstrapSuccessorPreparationTestFiles(t, local)
		for name, raw := range beforeLocal {
			if name != ".successor-local-members.json" && current[name] != raw {
				t.Fatal("registry rebind rewrote original signed execution history", name)
			}
		}
		if len(current) != len(beforeLocal)+1 || current[bootstrapSuccessorExecutionPrefix+"-registry-rebind.json"] == "" || !maps.Equal(baselineNonce, bootstrapSuccessorPreparationTestFiles(t, registry)) {
			t.Fatal("registry rebind changed nonce custody or created unrelated history")
		}
		if attempt == 0 {
			completed = current
		} else if !maps.Equal(completed, current) {
			t.Fatal("repeated physical adoption changed completed custody")
		}
	}
	// The completed member census distinguishes retained authority from a new
	// absent name. Even byte-identical replacement cannot recreate this receipt.
	receiptPath := filepath.Join(local, "contract-successor-execution-registry-rebind.json")
	heldReceipt := filepath.Join(filepath.Dir(path), "held-original-rebind-receipt.json")
	for _, replace := range []bool{false, true} {
		if err := os.Rename(receiptPath, heldReceipt); err != nil {
			t.Fatal(err)
		}
		if replace {
			if err := os.WriteFile(receiptPath, []byte(completed[filepath.Base(receiptPath)]), 0600); err != nil {
				t.Fatal(err)
			}
		}
		interrupted := bootstrapSuccessorPreparationTestFiles(t, local)
		output.Reset()
		if code, diagnostic := invoke("contract-successor-execution-resume", &output, extra...); code == 0 || output.Len() != 0 {
			t.Fatal("lost/replaced retained registry approval was recreated", replace, code, diagnostic)
		}
		if !maps.Equal(interrupted, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(baselineNonce, bootstrapSuccessorPreparationTestFiles(t, registry)) {
			t.Fatal("refused retained approval loss changed another original member", replace)
		}
		if replace {
			if err := os.Remove(receiptPath); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Rename(heldReceipt, receiptPath); err != nil {
			t.Fatal(err)
		}
	}
	afterApproval, err := os.ReadFile(f.approvalArgs[1])
	if err != nil || !bytes.Equal(afterApproval, originalApproval) {
		t.Fatal("registry rebind rewrote the original independent execution approval", err)
	}
	f.original.contracts.stateLock.Lock()
	unchanged := maps.Equal(counts, f.original.contracts.counts) && writes == len(f.original.contracts.writes)
	f.original.contracts.stateLock.Unlock()
	f.original.census.stateLock.Lock()
	unchanged = unchanged && maps.Equal(censusCounts, f.original.census.methodCounts)
	f.original.census.stateLock.Unlock()
	if !unchanged {
		t.Fatal("offline registry adoption performed a network read or send")
	}
}
