//go:build linux || darwin

// Public restoration must reach actual original execution without rewriting
// signed physical authority. All keys, approval and routes are synthetic.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The wire mirror permits the same public control on the old dispatcher.
type localRebindTestPlan struct {
	Schema                   string                         `json:"schema"`
	ExecutionApprovalHash    string                         `json:"execution_approval_hash"`
	LocalDirectory           string                         `json:"local_directory"`
	OriginalLocal            bootstrapSuccessorRootIdentity `json:"original_local"`
	RestoredLocal            bootstrapSuccessorRootIdentity `json:"restored_local"`
	RestoredGeneration       string                         `json:"restored_generation_sha256"`
	RuntimeDeclaration       durablevolume.Reference        `json:"runtime_declaration"`
	RestorePlan              durablevolume.Reference        `json:"restore_plan"`
	OriginalInventory        durablevolume.Reference        `json:"original_inventory"`
	OriginalFormerWriter     durablevolume.Reference        `json:"original_former_writer_fence"`
	OriginalMemberCensusHash string                         `json:"original_member_census_sha256"`
	DerivedMemberCensusHash  string                         `json:"derived_member_census_sha256"`
}

// Export every real owner, restore the complete fixed union through public
// plan/apply, then update only the test's separately reviewed declaration.
func localRebindTestRestore(t *testing.T, f *bootstrapSuccessorCanonicalFixture) (string, string) {
	t.Helper()
	// External signing inputs are retained separately; every original journal
	// must already own precisely the same transaction and approved envelope.
	inputDirectory := filepath.Dir(f.original.contracts.configPath)
	if inputDirectory == f.original.config.RunDirectory {
		t.Fatal("composed fixture did not separate signed input and runtime custody")
	}
	inputs := map[string][]byte{}
	for index, action := range f.original.contracts.config.Plan.Actions {
		path := filepath.Join(inputDirectory, action.Id+".signed.bin")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("original signing input was lost", action.Id, err)
		}
		journal, err := os.ReadFile(filepath.Join(f.original.config.RunDirectory, bootstrapContractStateFile(index)))
		if err != nil {
			t.Fatal(err)
		}
		var record evmActionRecord
		if err := decodePlanJson(journal, &record); err != nil {
			t.Fatal(err)
		}
		if err := record.validateForAction(f.original.contracts.config, index); err != nil || record.Signed != "0x"+hex.EncodeToString(raw) {
			t.Fatal("original journal lost exact signed bytes or action lineage", action.Id, err)
		}
		inputs[path] = raw
	}
	if len(inputs) != 8 {
		t.Fatal("complete original fixture did not retain all eight signing inputs")
	}
	source := newStoragePreparationCommandFixture(t)
	original := f.original.root.storage
	declaration, err := durablevolume.Load(original.Reference)
	if err != nil {
		t.Fatal(err)
	}
	source.root, source.storage, source.ctx = f.original.config.RunDirectory, original, f.original.storageContext(t.Context())
	var request durablevolume.PreparationRequest
	raw, err := os.ReadFile(source.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	request.RootPath = source.root
	request.MountPath, request.FilesystemUuid, request.FilesystemType = declaration.Volumes[0].MountPath, declaration.Volumes[0].FilesystemUuid, declaration.Volumes[0].FilesystemType
	request.Limits = durablevolume.PreparationLimits{MaxEntries: 512, MaxBytes: 64 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 64, MaxOwnerAttributeBytes: 256 * 1024, MaxPlanBytes: 8 * 1024 * 1024}
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, source, source.ctx, storagePreparationDirectoryOwner(t, "mainnet-successor-local-members"), false)
	raw, err = os.ReadFile(storage.target.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	report, err := durablevolume.LoadPhysicalInventory(t.Context(), request.RestoreSource.Inventory)
	if err != nil {
		t.Fatal(err)
	}
	owners := []durablevolume.PreparationOwner{}
	for _, entry := range report.Entries {
		for _, attribute := range entry.OwnerAttributes {
			if attribute.Name == durablevolume.PreparationAttribute {
				continue
			}
			var head durablehead.Checkpoint
			if err := json.Unmarshal(attribute.Value, &head); err != nil || attribute.Name != durablehead.Attribute(head.Kind, head.Name) {
				t.Fatal("fixture source contains an unclassified physical head", attribute.Name, err)
			}
			var owner durablevolume.PreparationOwner
			if head.Kind == "mainnet-successor-local-members" {
				owner = storagePreparationDirectoryOwner(t, head.Kind)
			} else {
				owner = storagePreparationSnapshotOwner(t, head.Kind, head.Name, int(head.MaximumBytes))
			}
			owner.Purpose, owner.RestoreCoverage = "restore", "complete-union-v1"
			owners = append(owners, owner)
		}
	}
	if len(owners) < 10 {
		t.Fatal("actual bootstrap fixture lost its shared owner scope", len(owners))
	}
	localRebindTestCoverage(t, owners, report)
	request.Owners = owners
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storage.target.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	storage.target.requestHash = durablefixture.Digest(raw)
	path, hash := storagePreparationFreezeOwnerPlan(t, storage.target, "storage-prepare")
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("actual local union storage restore failed", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("local copy acquired execution authority", err)
	}
	restored, err := durablevolume.Load(result.Declaration)
	if err != nil || len(restored.Volumes) != 1 || len(restored.Volumes[0].StateRoots) != 1 {
		t.Fatal(err)
	}
	found := false
	for volumeIndex := range declaration.Volumes {
		for rootIndex := range declaration.Volumes[volumeIndex].StateRoots {
			if declaration.Volumes[volumeIndex].StateRoots[rootIndex].Path == source.root {
				declaration.Volumes[volumeIndex].StateRoots[rootIndex] = restored.Volumes[0].StateRoots[0]
				found = true
			}
		}
	}
	if !found {
		t.Fatal("original declaration omitted local root")
	}
	raw, err = json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	declarationPath := filepath.Join(storage.target.metadata, "reviewed-local-runtime.json")
	if err := os.WriteFile(declarationPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reference := durablevolume.Reference{Path: declarationPath, Sha256: durablefixture.Digest(raw)}
	f.original.root.storage = &durablefixture.Fixture{Reference: reference, Host: original.Host, Roots: append([]string(nil), original.Roots...),
		Context: durablepath.WithHost(durablevolume.WithReference(t.Context(), reference), original.Host)}
	f.original.contracts.storage = f.original.root.storage
	for path, original := range inputs {
		retained, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(retained, original) {
			t.Fatal("restoring runtime custody changed original signing input", filepath.Base(path), err)
		}
	}
	return path, hash
}

// This read-only fixture census names the exact entries left after the same
// fixed production adapters have planned their views. The public command still
// decides coverage and performs the only preparation; diagnostics grant none.
func localRebindTestCoverage(t *testing.T, owners []durablevolume.PreparationOwner, report durablevolume.Inventory) {
	t.Helper()
	files := map[string]durablevolume.PreparationFile{}
	attributes := map[durablevolume.PreparationAttributeSpec]bool{}
	for _, entry := range report.Entries {
		if entry.Path != "" {
			files[entry.Path] = durablevolume.PreparationFile{Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256}
		}
		for _, attribute := range entry.OwnerAttributes {
			if entry.Path == "" && attribute.Name == durablevolume.PreparationAttribute {
				continue
			}
			path := entry.Path
			if path == "" {
				path = "."
			}
			attributes[durablevolume.PreparationAttributeSpec{Path: path, Name: attribute.Name}] = true
		}
	}
	for index, owner := range owners {
		plan, err := planStoragePreparationRestore(t.Context(), fmt.Sprintf("synthetic-coverage-%02d", index), owner, report, false)
		if err != nil {
			t.Fatal("actual owner cannot plan its original view", owner.Kind, err)
		}
		for _, file := range plan.Files {
			if original, found := files[file.Path]; !found || original != file {
				t.Fatal("actual fixed owners overlap or change original member", owner.Kind, file.Path)
			}
			delete(files, file.Path)
		}
		for _, attribute := range plan.Attributes {
			if !attributes[attribute] {
				t.Fatal("actual fixed owners overlap or invent original checkpoint", owner.Kind, attribute)
			}
			delete(attributes, attribute)
		}
	}
	names := []string{}
	for name := range files {
		names = append(names, "file:"+name)
	}
	for attribute := range attributes {
		names = append(names, "attribute:"+attribute.Path+":"+attribute.Name)
	}
	slices.Sort(names)
	if len(names) != 0 {
		t.Fatalf("actual original fixture has unassigned custody: %q", names)
	}
}

func localRebindTestSign(t *testing.T, path string, plan localRebindTestPlan, key ed25519.PrivateKey, domain string) planFileReference {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapRootTestWrite(t, path, struct {
		Schema    string              `json:"schema"`
		Plan      localRebindTestPlan `json:"plan"`
		Signature string              `json:"signature_ed25519"`
	}{Schema: "urnetwork-mainnet-successor-local-rebind-envelope-v1", Plan: plan, Signature: hex.EncodeToString(ed25519.Sign(key, append([]byte(domain+"\x00"), raw...)))})
}

// Independent approval enables retained resume, never a fresh claim. Faults
// are checked semantically before diagnostic reporting so all cases execute.
func TestBootstrapSuccessorLocalRebindPublicResumePreservesOriginalAuthority(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	local, registry := f.original.config.RunDirectory, f.approval.Plan.Request.RegistryDirectory
	originals, nonces := bootstrapSuccessorPreparationTestFiles(t, local), bootstrapSuccessorPreparationTestFiles(t, registry)
	path, hash := localRebindTestRestore(t, f)
	before := bootstrapSuccessorPreparationTestFiles(t, local)
	for name, raw := range originals {
		if name != ".successor-local-members.json" && before[name] != raw {
			t.Fatal("restore rewrote original signed custody", name)
		}
	}
	invoke := func(command string, extra ...string) (int, []byte, string) {
		var output, diagnostic bytes.Buffer
		args := append(append(append([]string{}, f.paths...), f.approvalArgs...), extra...)
		code := f.original.command(t.Context(), command, &output, &diagnostic, args...)
		return code, output.Bytes(), diagnostic.String()
	}
	if code, output, diagnostic := invoke("contract-successor-execution-resume"); code == 0 || len(output) != 0 {
		t.Fatal("restored local root bypassed original physical authority", code, diagnostic)
	}
	code, output, diagnostic := invoke("contract-successor-execution-local-rebind-preview", "--local-restore-plan", path, "--local-restore-plan-sha256", hash)
	if code != 0 {
		t.Fatal("public local rebind preview cannot inspect complete restored original custody", code, diagnostic)
	}
	var preview struct {
		Plan         localRebindTestPlan `json:"plan"`
		SigningBytes string              `json:"signing_bytes"`
	}
	if err := json.Unmarshal(output, &preview); err != nil {
		t.Fatal(err)
	}
	domain := "urnetwork-mainnet-successor-local-rebind-v1"
	raw, err := json.Marshal(preview.Plan)
	if err != nil || preview.SigningBytes != "0x"+hex.EncodeToString(append([]byte(domain+"\x00"), raw...)) || preview.Plan.ExecutionApprovalHash != rootObjectHash(f.approval) || preview.Plan.OriginalLocal != f.approval.Plan.Review.Preparation.Approval.Plan.Root {
		t.Fatal("local review changed original domain or approval", err)
	}
	approvalPath := filepath.Join(filepath.Dir(path), "synthetic-local-rebind.json")
	for _, mode := range []string{"approver", "domain", "generation", "plan", "declaration", "original"} {
		plan, key, signingDomain := preview.Plan, f.key, domain
		switch mode {
		case "approver":
			key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{89}, ed25519.SeedSize))
		case "domain":
			signingDomain = "urnetwork-mainnet-successor-registry-rebind-v1"
		case "generation":
			plan.RestoredLocal.Inode++
		case "plan":
			plan.RestorePlan.Sha256 = "sha256:" + strings.Repeat("a", 64)
		case "declaration":
			plan.RuntimeDeclaration.Sha256 = "sha256:" + strings.Repeat("b", 64)
		case "original":
			plan.OriginalLocal.Inode++
		}
		ref := localRebindTestSign(t, approvalPath, plan, key, signingDomain)
		code, output, diagnostic := invoke("contract-successor-execution-resume", "--local-rebind-approval", ref.Path, "--local-rebind-approval-sha256", ref.Sha256)
		if code == 0 || len(output) != 0 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
			t.Fatal("invalid local adoption acquired authority or changed original custody", mode, code, diagnostic)
		}
		if !strings.Contains(diagnostic, "successor local") {
			t.Errorf("local refusal reached an unrelated boundary mode=%s: %s", mode, diagnostic)
		}
	}
	// Even a separately signed edited restore plan cannot discard another
	// co-owner. The complete original inventory remains an independent bound.
	planBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var originalRestore durablevolume.PreparationPlan
	if err := json.Unmarshal(planBytes, &originalRestore); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"omitted-owner", "repeated-source"} {
		var changedPlan durablevolume.PreparationPlan
		if err := json.Unmarshal(planBytes, &changedPlan); err != nil {
			t.Fatal(err)
		}
		if mode == "repeated-source" {
			changedPlan.Sources = append(changedPlan.Sources, changedPlan.Sources[0])
		} else {
			index := 0
			if changedPlan.Owners[index].Owner.Kind == "mainnet-successor-local-members" {
				index++
			}
			changedPlan.Owners = append(changedPlan.Owners[:index], changedPlan.Owners[index+1:]...)
			if changedPlan.Derivations[0].OwnerIndex > index {
				changedPlan.Derivations[0].OwnerIndex--
			}
			var request durablevolume.PreparationRequest
			if err := json.Unmarshal(changedPlan.RequestBytes, &request); err != nil {
				t.Fatal(err)
			}
			request.Owners = append(request.Owners[:index], request.Owners[index+1:]...)
			changedPlan.RequestBytes, err = json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			changedPlan.RequestSha256 = safeReleaseHash(changedPlan.RequestBytes)
			changedPlan.Request.Sha256 = changedPlan.RequestSha256
		}
		changedRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(path), "synthetic-"+mode+".json"), changedPlan)
		changed := preview.Plan
		changed.RestorePlan = durablevolume.Reference{Path: changedRef.Path, Sha256: changedRef.Sha256}
		ref := localRebindTestSign(t, approvalPath, changed, f.key, domain)
		code, output, diagnostic := invoke("contract-successor-execution-resume", "--local-rebind-approval", ref.Path, "--local-rebind-approval-sha256", ref.Sha256)
		if code == 0 || len(output) != 0 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
			t.Fatal("signed local rebind discarded shared owner custody", mode, code, diagnostic)
		}
		if !strings.Contains(diagnostic, "coverage omits") && !strings.Contains(diagnostic, "reviewed target source") {
			t.Errorf("changed owner union reached an unrelated boundary mode=%s: %s", mode, diagnostic)
		}
	}
	ref := localRebindTestSign(t, approvalPath, preview.Plan, f.key, domain)
	extra := []string{"--local-rebind-approval", ref.Path, "--local-rebind-approval-sha256", ref.Sha256}
	if code, output, diagnostic := invoke("contract-successor-execution-claim", extra...); code != 2 || len(output) != 0 {
		t.Fatal("local adoption enabled a fresh execution claim", code, diagnostic)
	}
	generation := storagePreparationOwnerAttribute(t, local, durablevolume.RootGenerationAttribute)
	changed := bytes.Clone(generation)
	changed[0] ^= 1
	if err := unix.Setxattr(local, durablevolume.RootGenerationAttribute, changed, unix.XATTR_REPLACE); err != nil {
		t.Fatal(err)
	}
	if code, output, diagnostic := invoke("contract-successor-execution-resume", extra...); code == 0 || len(output) != 0 || !strings.Contains(diagnostic, "identity") {
		t.Fatal("local adoption ignored actual changed generation", code, diagnostic)
	}
	if err := unix.Setxattr(local, durablevolume.RootGenerationAttribute, generation, unix.XATTR_REPLACE); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, local)) {
		t.Fatal("unsigned/failed local review changed original custody")
	}
	for attempt := 0; attempt < 2; attempt++ {
		code, output, diagnostic := invoke("contract-successor-execution-resume", extra...)
		if code != 0 {
			t.Fatal("public local rebind could not retain original execution", attempt, code, diagnostic)
		}
		var result bootstrapSuccessorExecutionResult
		if err := json.Unmarshal(output, &result); err != nil || result.CumulativeAttempts != 8 || result.SubmissionAttempted || result.ActivationReady {
			t.Fatal("local adoption changed allowance or acquired execution effects", err)
		}
	}
	after := bootstrapSuccessorPreparationTestFiles(t, local)
	for name, raw := range before {
		if name != ".successor-local-members.json" && after[name] != raw {
			t.Fatal("local adoption rewrote immutable original member", name)
		}
	}
	if len(after) != len(before)+1 || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("local adoption lost original history or added unexpected custody")
	}
	var oldCensus, newCensus bootstrapSuccessorMemberCensus
	if err := json.Unmarshal([]byte(before[".successor-local-members.json"]), &oldCensus); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(after[".successor-local-members.json"]), &newCensus); err != nil {
		t.Fatal(err)
	}
	if len(newCensus.Members) != len(oldCensus.Members)+1 || newCensus.Pending != nil {
		t.Fatal("local adoption did not append exactly one settled receipt")
	}
	actual := map[string]bootstrapSuccessorMember{}
	for _, member := range newCensus.Members {
		actual[member.Name] = member
	}
	for _, member := range oldCensus.Members {
		if actual[member.Name] != member {
			t.Fatal("local adoption changed prior member generation", member.Name)
		}
	}
	receipt := bootstrapSuccessorExecutionPrefix + "-local-rebind.json"
	if actual[receipt].Sha256 != safeReleaseHash([]byte(after[receipt])) {
		t.Fatal("local receipt census differs from retained exact approval")
	}
	// The acknowledged receipt is mandatory on every restart. Removing only
	// that file cannot cause a second physical enrollment or a renewed nonce.
	if err := os.Rename(filepath.Join(local, receipt), filepath.Join(filepath.Dir(path), "held-local-receipt")); err != nil {
		t.Fatal(err)
	}
	if code, output, diagnostic := invoke("contract-successor-execution-resume", extra...); code == 0 || len(output) != 0 {
		t.Fatal("lost local receipt was recreated", code, diagnostic)
	}
	if !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("lost receipt released original nonce authority")
	}
}
