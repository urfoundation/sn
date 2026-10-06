//go:build linux

// Actual public adoption must inspect both retained outer images without
// repairing them, then let only the independently approved writer reconcile.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The barriers follow real writes. Re-publishing the identical logical census
// deliberately gives both images the same hash: inode lineage must select next.
func bootstrapSuccessorOuterTestInterrupt(t *testing.T, f *bootstrapSuccessorCanonicalFixture, registry bool, boundary string) {
	t.Helper()
	path := f.original.config.RunDirectory
	if registry {
		path = f.approval.Plan.Request.RegistryDirectory
	}
	root, err := bootstrapSuccessorPhysicalRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	active := openStorageMemberRestoreTestOwner(t, f.original.storageContext(t.Context()), path, root, rootObjectHash(f.approval), registry, nil)
	raw, err := json.Marshal(active.members.census)
	if err != nil {
		t.Fatal(err)
	}
	lost := errors.New("synthetic paired public checkpoint acknowledgement lost")
	reached := false
	stop := func() error { reached = true; return lost }
	hooks := durablehead.PublicationHooks{}
	switch boundary {
	case "file-sync":
		hooks.AfterFileSync = stop
	case "exchange":
		hooks.AfterRename = stop
	case "directory-sync":
		hooks.AfterDirectorySync = func(*os.File) error { return stop() }
	default:
		t.Fatal("unknown paired public barrier", boundary)
	}
	if err := active.members.head.PublishWithHooks(raw, hooks); !reached || !errors.Is(err, lost) || !errors.Is(err, durablehead.ErrUncertain) {
		t.Fatal("actual paired checkpoint did not stop at its durable boundary", boundary, reached, err)
	}
	if err := active.close(); err != nil {
		t.Fatal(err)
	}
}

// The same actual eight-action fixture covers local and nonce custody. Its
// original signed authority stays byte-exact across preview, approval and reopen.
func bootstrapSuccessorOuterPublicControl(t *testing.T, registry, pending bool, boundary string) {
	t.Helper()
	f := newBootstrapSuccessorCanonicalFixture(t)
	attempts := uint16(8)
	pendingHash, pendingPayload := "", ""
	var pendingOriginal bootstrapSuccessorMemberPending
	if pending {
		owner, adapter, join := f.openRuntimeRevisions()
		if result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, adapter, true); err != nil || !result.SubmissionAttempted || result.CumulativeAttempts != 9 {
			t.Fatal("original ninth attempt was not retained", result, err)
		}
		lost := errors.New("synthetic original terminal reservation acknowledgement lost")
		reached := false
		owner.local.hook = func(stage string) error {
			if stage == bootstrapSuccessorExecutionEventName(2)+".intent:reserved" {
				reached = true
				return lost
			}
			return nil
		}
		if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, adapter, false); !reached || !errors.Is(err, lost) || owner.local.members.census.Pending == nil {
			t.Fatal("original terminal reservation missed its barrier", reached, err)
		}
		pendingOriginal = *owner.local.members.census.Pending
		pendingHash, pendingPayload = pendingOriginal.Sha256, pendingOriginal.Payload
		if pendingOriginal.Name != bootstrapSuccessorExecutionEventName(2)+".intent" || pendingOriginal.StageInode != 0 {
			t.Fatal("original reservation did not precede its exact second-event intent")
		}
		join()
		attempts = 9
	}
	bootstrapSuccessorOuterTestInterrupt(t, f, registry, boundary)
	local, nonce := f.original.config.RunDirectory, f.approval.Plan.Request.RegistryDirectory
	originals, nonces := bootstrapSuccessorPreparationTestFiles(t, local), bootstrapSuccessorPreparationTestFiles(t, nonce)
	f.original.contracts.stateLock.Lock()
	counts, sends := maps.Clone(f.original.contracts.counts), len(f.original.contracts.writes)
	f.original.contracts.stateLock.Unlock()
	path, hash, root := "", "", local
	command, flag, approvalFlag := "contract-successor-execution-local-rebind-preview", "--local-restore-plan", "--local-rebind-approval"
	domain, envelope := "urnetwork-mainnet-successor-local-rebind-v1", "urnetwork-mainnet-successor-local-rebind-envelope-v1"
	if registry {
		path, hash = registryRebindTestRestore(t, f)
		root = nonce
		command, flag, approvalFlag = "contract-successor-execution-rebind-preview", "--registry-restore-plan", "--registry-rebind-approval"
		domain, envelope = "urnetwork-mainnet-successor-registry-rebind-v1", "urnetwork-mainnet-successor-registry-rebind-envelope-v1"
	} else {
		path, hash = localRebindTestRestore(t, f)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var restore durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &restore); err != nil || len(restore.Derivations) != 2 || restore.Derivations[0].Original.File.Sha256 != restore.Derivations[1].Original.File.Sha256 {
		t.Fatal("real outer fixture lost its two identical logical images", err)
	}
	metadataNames := map[string]bool{}
	for _, derivation := range restore.Derivations {
		metadataNames[derivation.Original.File.Path] = true
	}
	spec := bootstrapSuccessorMemberSpec(registry)
	headBytes := make([]byte, 4096)
	n, err := unix.Getxattr(root, durablehead.Attribute(spec.Kind, spec.Name), headBytes)
	if err != nil {
		t.Fatal(err)
	}
	headBytes = headBytes[:n]
	beforeLocal, beforeNonce := bootstrapSuccessorPreparationTestFiles(t, local), bootstrapSuccessorPreparationTestFiles(t, nonce)
	invoke := func(command string, extra ...string) (int, []byte, string) {
		var output, diagnostic bytes.Buffer
		args := append(append(append([]string{}, f.paths...), f.approvalArgs...), extra...)
		code := f.original.command(t.Context(), command, &output, &diagnostic, args...)
		return code, output.Bytes(), diagnostic.String()
	}
	// Omitting either reviewed image never grants a preview or repairs the pair.
	changed := restore
	changed.Derivations = append([]durablevolume.PreparationDerivation(nil), restore.Derivations[:1]...)
	changedRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(path), "synthetic-missing-pair.json"), changed)
	if code, output, diagnostic := invoke(command, flag, changedRef.Path, flag+"-sha256", changedRef.Sha256); code == 0 || len(output) != 0 {
		t.Fatal("incomplete reviewed pair acquired public adoption", code, diagnostic)
	}
	code, output, diagnostic := invoke(command, flag, path, flag+"-sha256", hash)
	if code != 0 {
		t.Fatal("actual public preview refused the exact restored outer pair", code, diagnostic)
	}
	var preview struct {
		Plan         json.RawMessage `json:"plan"`
		SigningBytes string          `json:"signing_bytes"`
	}
	if err := json.Unmarshal(output, &preview); err != nil {
		t.Fatal(err)
	}
	message, err := hex.DecodeString(strings.TrimPrefix(preview.SigningBytes, "0x"))
	var compactPlan bytes.Buffer
	compactErr := json.Compact(&compactPlan, preview.Plan)
	if err != nil || compactErr != nil || !bytes.Equal(message, append([]byte(domain+"\x00"), compactPlan.Bytes()...)) {
		t.Fatal("outer adoption changed the original separate signing domain", err)
	}
	var reviewed struct {
		PendingOutcomeSha256 string `json:"pending_outcome_sha256"`
	}
	if err := json.Unmarshal(preview.Plan, &reviewed); err != nil || reviewed.PendingOutcomeSha256 != pendingHash {
		t.Fatal("outer review lost its original logical outcome", err)
	}
	sign := func(key ed25519.PrivateKey, name string) planFileReference {
		return bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(path), name), struct {
			Schema    string          `json:"schema"`
			Plan      json.RawMessage `json:"plan"`
			Signature string          `json:"signature_ed25519"`
		}{Schema: envelope, Plan: preview.Plan, Signature: hex.EncodeToString(ed25519.Sign(key, message))})
	}
	wrong := sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{71}, ed25519.SeedSize)), "synthetic-wrong-outer-approval.json")
	if code, output, diagnostic := invoke("contract-successor-execution-resume", approvalFlag, wrong.Path, approvalFlag+"-sha256", wrong.Sha256); code == 0 || len(output) != 0 {
		t.Fatal("foreign outer approver acquired original writer authority", code, diagnostic)
	}
	currentHead := make([]byte, 4096)
	n, err = unix.Getxattr(root, durablehead.Attribute(spec.Kind, spec.Name), currentHead)
	if err != nil || !bytes.Equal(headBytes, currentHead[:n]) || !maps.Equal(beforeLocal, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(beforeNonce, bootstrapSuccessorPreparationTestFiles(t, nonce)) {
		t.Fatal("passive or refused public outer adoption changed physical custody", err)
	}
	approval := sign(f.key, "synthetic-approved-outer-pair.json")
	extra := []string{approvalFlag, approval.Path, approvalFlag + "-sha256", approval.Sha256}
	var completed map[string]string
	for attempt := 0; attempt < 2; attempt++ {
		code, output, diagnostic := invoke("contract-successor-execution-resume", extra...)
		if code != 0 {
			t.Fatal("approved public outer resume failed", attempt, code, diagnostic)
		}
		var result struct {
			CumulativeAttempts    uint16 `json:"cumulative_attempts"`
			SubmissionAttempted   bool   `json:"submission_attempted"`
			PhysicalRebindPending bool   `json:"physical_rebind_pending"`
		}
		if err := json.Unmarshal(output, &result); err != nil || result.CumulativeAttempts != attempts || result.SubmissionAttempted || result.PhysicalRebindPending != pending {
			t.Fatal("outer adoption changed original allowance or completion", result, err)
		}
		current := bootstrapSuccessorPreparationTestFiles(t, local)
		if attempt == 0 {
			completed = current
		} else if !maps.Equal(completed, current) {
			t.Fatal("repeated approved outer resume rewrote completed custody")
		}
	}
	currentHead = make([]byte, 4096)
	n, err = unix.Getxattr(root, durablehead.Attribute(spec.Kind, spec.Name), currentHead)
	var reconciled durablehead.Checkpoint
	if err != nil || json.Unmarshal(currentHead[:n], &reconciled) != nil || reconciled.Pending != nil {
		t.Fatal("approved exclusive writer did not reconcile the exact outer pair", err)
	}
	f.original.contracts.stateLock.Lock()
	unchanged := maps.Equal(counts, f.original.contracts.counts) && sends == len(f.original.contracts.writes)
	f.original.contracts.stateLock.Unlock()
	if !unchanged {
		t.Fatal("offline outer adoption read or sent on the network")
	}
	if pending {
		var census bootstrapSuccessorMemberCensus
		if err := json.Unmarshal([]byte(completed[".successor-local-members.json"]), &census); err != nil || census.Pending == nil || census.Pending.Payload != pendingPayload || census.Pending.Sha256 != pendingHash {
			t.Fatal("outer reconciliation replaced original pending outcome", err)
		}
		expected, err := base64.StdEncoding.Strict().DecodeString(pendingOriginal.Payload)
		if err != nil || int64(len(expected)) != pendingOriginal.Size || safeReleaseHash(expected) != pendingOriginal.Sha256 {
			t.Fatal("original terminal reservation lost its exact payload", err)
		}
		stage := filepath.Join(local, census.Pending.Stage)
		staged, err := os.ReadFile(stage)
		var stagedStat unix.Stat_t
		if err != nil || !bytes.Equal(staged, expected) || unix.Stat(stage, &stagedStat) != nil || census.Pending.StageInode == 0 || census.Pending.StageInode != stagedStat.Ino || census.Pending.Name != pendingOriginal.Name || census.Pending.Stage != pendingOriginal.Stage {
			t.Fatal("approved offline recovery did not retain its exact staged original intent", err)
		}
		var output bytes.Buffer
		online := append(append(append([]string{}, f.approvalArgs...), extra...), "--online", "--canonical-approval", f.canonicalRef.Path, "--canonical-approval-sha256", f.canonicalRef.Sha256)
		if code, diagnostic := f.invoke("contract-successor-execution-resume", &output, online...); code != 0 {
			t.Fatal("original canonical outcome could not finish after outer adoption", code, diagnostic)
		}
		var result bootstrapSuccessorExecutionResult
		if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.InstallationComplete || result.CumulativeAttempts != attempts || result.SubmissionAttempted {
			t.Fatal("original outer-pending completion changed economic authority", result, err)
		}
		completed = bootstrapSuccessorPreparationTestFiles(t, local)
		// Only the already reserved event may become this exact intent/final
		// pair. Its staged inode moves to intent; its signed lineage stays exact.
		intentName := pendingOriginal.Name
		finalName := bootstrapSuccessorExecutionEventName(2) + ".json"
		var intentStat unix.Stat_t
		if completed[intentName] != string(expected) || completed[finalName] != string(expected) || unix.Stat(filepath.Join(local, intentName), &intentStat) != nil || intentStat.Dev != stagedStat.Dev || intentStat.Ino != stagedStat.Ino {
			t.Fatal("canonical recovery replaced original staged intent bytes or inode")
		}
		if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("canonical completion retained another staged copy", err)
		}
		var outcome, predecessor bootstrapSuccessorExecutionEvent
		if err := decodePlanJson(expected, &outcome); err != nil {
			t.Fatal(err)
		}
		if err := decodePlanJson([]byte(originals[bootstrapSuccessorExecutionEventName(1)+".json"]), &predecessor); err != nil {
			t.Fatal(err)
		}
		if outcome.Sequence != 2 || outcome.Phase != "installed" || outcome.Receipt == nil || outcome.CumulativeAttempts != attempts || outcome.PreviousHash != predecessor.ContentHash || outcome.ApprovalHash != predecessor.ApprovalHash || outcome.CanonicalAuthorityHash != predecessor.CanonicalAuthorityHash || outcome.RuntimeRevisionHash != predecessor.RuntimeRevisionHash || outcome.SafeCurrentRevisionHash != predecessor.SafeCurrentRevisionHash || outcome.ReservedLifetimeWei != predecessor.ReservedLifetimeWei {
			t.Fatal("original terminal intent changed its action, predecessor or economic authority")
		}
	}
	for name, raw := range originals {
		if name != ".successor-local-members.json" && !(!registry && metadataNames[name]) && completed[name] != raw {
			t.Fatal("public outer recovery rewrote original signed local history", name)
		}
	}
	receiptName := bootstrapSuccessorExecutionPrefix + "-local-rebind.json"
	if registry {
		receiptName = bootstrapSuccessorExecutionPrefix + "-registry-rebind.json"
	}
	for name := range completed {
		if _, found := originals[name]; !found && name != receiptName && !(pending && (name == pendingOriginal.Name || name == bootstrapSuccessorExecutionEventName(2)+".json")) {
			t.Fatal("public outer recovery invented an unrelated local member", name)
		}
	}
	if completed[receiptName] == "" {
		t.Fatal("completed physical adoption omitted its exact independent approval")
	}
	currentNonce := bootstrapSuccessorPreparationTestFiles(t, nonce)
	for name, raw := range nonces {
		if !(registry && metadataNames[name]) && currentNonce[name] != raw {
			t.Fatal("public outer recovery rewrote original nonce authority", name)
		}
	}
	for name := range currentNonce {
		if _, found := nonces[name]; !found {
			t.Fatal("public outer recovery invented a nonce claim", name)
		}
	}
	f.original.contracts.stateLock.Lock()
	actualSends := len(f.original.contracts.writes)
	f.original.contracts.stateLock.Unlock()
	if actualSends != sends {
		t.Fatal("original outcome reconciliation submitted another transaction")
	}
}

func TestBootstrapSuccessorOuterLocalPublicResume(t *testing.T) {
	bootstrapSuccessorOuterPublicControl(t, false, false, "file-sync")
}

func TestBootstrapSuccessorOuterRegistryPublicResume(t *testing.T) {
	bootstrapSuccessorOuterPublicControl(t, true, false, "exchange")
}

func TestBootstrapSuccessorOuterLocalRetainsOriginalOutcome(t *testing.T) {
	bootstrapSuccessorOuterPublicControl(t, false, true, "directory-sync")
}
