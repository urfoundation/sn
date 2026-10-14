//go:build linux || darwin

// Public proposals and archive adoption retain the original combined policy.
// Grammar controls use signed input files; the separate producer control runs
// the actual capture and replay images before and after a public restart.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// No VM observation is fabricated for these ordinary authority controls. The
// empty original checkpoint holds at its admitted boundary throughout them.
func newEconomicConservationNativeRenewalFixture(t *testing.T, configure ...func(*economicConservationFixture)) (*economicConservationArchiveFixture, []planFileReference, nativeProducerRenewal) {
	t.Helper()
	_, native, _, review, _ := nativeRenewalTestInputs(t)
	references := slices.Clone(native.Execution.Producer.Renewals)
	native.Execution.Producer.Renewals = nil
	source := newEconomicConservationFixture(t, false)
	source.policy.Native.Observation, source.policy.Vault.Network = native, native.Network
	for _, change := range configure {
		change(source)
	}
	source.writePolicy(t)
	ctx := monitorTestStorageContext(t, t.Context(), source.args(t))
	metadata := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, metadata)
	ctx = economicConservationTestArchiveReserves(t, ctx, metadata)
	f := &economicConservationArchiveFixture{source: source, ctx: ctx, metadata: metadata}
	state := newEconomicConservationState(source.policy)
	observeEconomicConservationTestClaims(t, ctx, source, state)
	state.ContentHash = state.hash()
	if err := state.validate(ctx, source.policy); err != nil {
		t.Fatal("original unobserved combined checkpoint", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openMonitorHistorySnapshot(ctx, source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(append(raw, '\n'), nil), owner.close()); err != nil {
		t.Fatal(err)
	}
	f.reset(t)
	return f, references, review
}

func economicConservationNativeRenewalTestPropose(t *testing.T, f *economicConservationArchiveFixture, references []planFileReference, review string) (economicConservationNativeRenewalProposal, int, string) {
	t.Helper()
	request := economicConservationNativeRenewalRequest{Schema: "urnetwork-economic-native-approval-request-v1", Policy: f.source.policy, Original: f.request.Original, Renewals: references, ReviewSha256: review}
	path, pin := f.document(t, "native-proposal.json", request)
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, []string{"economic-conservation-archive", "native-proposal", "--request", path, "--request-sha256", pin}, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("read-only native approval proposal changed original custody")
	}
	var proposal economicConservationNativeRenewalProposal
	if code == 0 {
		if err := decodePlanJson(output.Bytes(), &proposal); err != nil {
			t.Fatal(err)
		}
		message, err := proposal.Adoption.signingBytes()
		if err != nil || proposal.RestartAuthorized || proposal.Adoption.Signature != "" || proposal.SigningBytes != hex.EncodeToString(message) || proposal.ApplyCommand == "" {
			t.Fatal("native proposal omitted its exact unsigned authority", err, proposal)
		}
	} else if output.Len() != 0 {
		t.Fatal("refused native proposal published partial authority")
	}
	return proposal, code, diagnostic.String()
}

func economicConservationNativeRenewalTestSign(t *testing.T, adoption *economicConservationNativeRenewal) {
	t.Helper()
	message, err := adoption.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	adoption.Signature = hex.EncodeToString(ed25519.Sign(key, message))
}

func economicConservationNativeRenewalTestPlan(t *testing.T, f *economicConservationArchiveFixture, references []planFileReference, review string) (economicConservationNativeRenewal, economicConservationArchivePlan, []string) {
	t.Helper()
	proposal, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, review)
	if code != 0 {
		t.Fatal("public native proposal failed", code, issue)
	}
	economicConservationNativeRenewalTestSign(t, &proposal.Adoption)
	f.request.NativeRenewal = &proposal.Adoption
	plan, args := f.plan(t)
	return proposal.Adoption, plan, args
}

func TestEconomicConservationNativeRenewalPublicPreservesOriginalPolicyAndArchive(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	before := f.source.state(t)
	original, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	adoption, _, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic combined native adoption")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public native adoption failed", code, issue)
	}
	retained, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(retained, original) {
		t.Fatal("native adoption changed exact original checkpoint", err)
	}
	after := f.source.state(t)
	operating, err := after.operatingPolicy(f.source.policy)
	if err != nil || after.NativeRenewal == nil || after.NativeRenewal.Ordinal != 1 || after.PolicyHash != before.PolicyHash || operating.identityHash() != before.PolicyHash || !reflect.DeepEqual(before.Native, after.Native) || !reflect.DeepEqual(before.Vault, after.Vault) || !reflect.DeepEqual(before.ClaimStates, after.ClaimStates) || !slices.Equal(operating.Native.Observation.Execution.Producer.Renewals, references) || len(f.source.policy.Native.Observation.Execution.Producer.Renewals) != 0 {
		t.Fatal("native adoption reset source state or changed original policy", err, after)
	}
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &after, monitorServiceHooks{})
	if err != nil {
		t.Fatal("restart could not authenticate original native approval archive", err)
	}
	after.archiveView = view
	if err := errors.Join(after.requireNativeApprovalHistory(), view.close()); err != nil {
		t.Fatal(err)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("next archive could not retain original native adoption", code, issue)
	}
	archived := f.source.state(t)
	if archived.NativeRenewal != nil || archived.Archive.NativeApprovalHead == nil || archived.Archive.NativeApprovalHead.Hash != rootObjectHash(adoption) || !slices.Equal(archived.Archive.NativeApprovalHead.Renewals, references) {
		t.Fatal("archive discarded original native approval lineage", archived.Archive)
	}
	view, err = openEconomicConservationArchive(f.ctx, f.source.policy, &archived, monitorServiceHooks{})
	if err != nil {
		t.Fatal("archived native adoption did not reopen", err)
	}
	if !view.nativeReviews[adoption.ReviewSha256] {
		t.Fatal("original signed review missing from admitted index")
	}
	if err := view.close(); err != nil {
		t.Fatal(err)
	}
}

func TestEconomicConservationNativeRenewalPublicLostAckAndShortOutputReconcile(t *testing.T) {
	for _, mode := range []string{"archive", "checkpoint", "output"} {
		f, references, _ := newEconomicConservationNativeRenewalFixture(t)
		_, plan, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic native lost acknowledgement "+mode)))
		var fired atomic.Bool
		hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			if role == economicConservationRole && kind == mode && fired.CompareAndSwap(false, true) {
				return syscall.EIO
			}
			return nil
		}}
		if mode == "output" {
			if code, _ := f.apply(t, args, economicConservationWindowShortWriter{}, hooks); code == 0 {
				t.Fatal("native adoption hid short output after publication")
			}
		} else if code, issue := f.apply(t, args, &bytes.Buffer{}, hooks); code == 0 || !fired.Load() || !strings.Contains(issue, syscall.EIO.Error()) {
			t.Fatal("native adoption did not reach its actual synced failure", mode, code, issue)
		}
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("native adoption did not reconcile original/next custody", mode, code, issue)
		}
		raw, err := os.ReadFile(f.source.checkpoint)
		if err != nil || monitorReadDigest(raw) != plan.Next.Sha256 || f.source.state(t).NativeRenewal.Ordinal != 1 {
			t.Fatal("native adoption retry repeated or changed original lineage", mode, err)
		}
	}
}

func TestEconomicConservationNativeRenewalPublicRejectsForeignSignerAndLineage(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	adoption, _, _ := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic original native signer")))
	for _, fault := range []string{"foreign-key", "policy", "previous", "ordinal", "original"} {
		changed := adoption
		wanted := "original independent signature"
		switch fault {
		case "foreign-key":
			message, err := changed.signingBytes()
			if err != nil {
				t.Fatal(err)
			}
			key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x76}, ed25519.SeedSize))
			changed.Signature = hex.EncodeToString(ed25519.Sign(key, message))
		case "policy":
			changed.PolicyHash = monitorReadDigest([]byte("different original economic identity"))
			economicConservationNativeRenewalTestSign(t, &changed)
		case "previous":
			changed.Previous = monitorReadDigest([]byte("different predecessor approval"))
			economicConservationNativeRenewalTestSign(t, &changed)
			wanted = "exact predecessor"
		case "ordinal":
			changed.Ordinal++
			economicConservationNativeRenewalTestSign(t, &changed)
			wanted = "exact predecessor"
		case "original":
			changed.Original.Sha256 = monitorReadDigest([]byte("different original checkpoint"))
			economicConservationNativeRenewalTestSign(t, &changed)
			wanted = "another original checkpoint"
		}
		request := f.request
		request.NativeRenewal = &changed
		economicConservationRevisionTestRefuse(t, f, request, wanted)
	}
}

func TestEconomicConservationNativeRenewalPublicMissingReviewRemainsUnavailable(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	path := references[0].Path
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if _, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, monitorReadDigest([]byte("synthetic unavailable review"))); code == 0 || strings.Contains(issue, "signature") || !strings.Contains(issue, "no such file") {
		t.Fatal("unread signed review acquired authority or became a fabricated signature contradiction", code, issue)
	}
	if err := os.Rename(path+".retained", path); err != nil {
		t.Fatal(err)
	}
	if _, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, monitorReadDigest([]byte("synthetic unavailable review"))); code != 0 {
		t.Fatal("same original input did not recover after readable review", code, issue)
	}
}

func TestEconomicConservationNativeRenewalPublicRejectsReviewReuseAndChangedPrefix(t *testing.T) {
	f, references, review := newEconomicConservationNativeRenewalFixture(t)
	adoption, _, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic retained native review")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	review.Previous, review.Ordinal = references[0], 2
	review.After.Number++
	review.After.Hash = "0x" + strings.Repeat("5", 64)
	review.Completed++
	review.CompletionChain = monitorReadDigest([]byte("synthetic later original completion"))
	review.Next.MaximumJobs += 4096
	nativeRenewalTestSign(t, &review)
	references = append(slices.Clone(references), nativeRenewalTestWrite(t, filepath.Join(f.metadata, "second-native-review.json"), review))
	if _, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, adoption.ReviewSha256); code == 0 || !strings.Contains(issue, "reused") {
		t.Fatal("archived native review was silently reused", code, issue)
	}
	if _, code, issue := economicConservationNativeRenewalTestPropose(t, f, references[1:], monitorReadDigest([]byte("synthetic replacement prefix"))); code == 0 || !strings.Contains(issue, "append-only") {
		t.Fatal("native proposal discarded an original review", code, issue)
	}
	proposal, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, monitorReadDigest([]byte("synthetic second native approval")))
	if code != 0 || proposal.Adoption.Ordinal != 2 || proposal.Adoption.Previous != rootObjectHash(adoption) || !slices.Equal(proposal.Adoption.From, adoption.To) {
		t.Fatal("next native adoption lost cumulative original lineage", code, issue, proposal)
	}
}

func TestEconomicConservationNativeRenewalExternalPolicyChangeDoesNotResetOwner(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	_, _, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic immutable external policy")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	changed := f.source.policy
	execution := *changed.Native.Observation.Execution
	producer := *execution.Producer
	producer.Renewals, execution.Producer = slices.Clone(references), &producer
	changed.Native.Observation.Execution = &execution
	if err := changed.validate(); err != nil || changed.identityHash() == f.source.policy.identityHash() {
		t.Fatal("external policy fixture did not change valid original authority", err)
	}
	path, pin := f.document(t, "unreviewed-outer-policy.json", changed)
	command := f.source.args(t)
	command[2], command[4] = path, pin
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, command, &output, &diagnostic)
	// Either original-policy comparison can refuse first. A retained archive
	// checks its capacity/review basis before the active checkpoint hash.
	refusedOriginalBasis := strings.Contains(diagnostic.String(), "policy basis") || strings.Contains(diagnostic.String(), "economic archive changed its original capacity or review basis")
	if code != 3 || output.Len() != 0 || !refusedOriginalBasis || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("changed external config bypassed original combined owner identity", code, diagnostic.String())
	}
	if f.source.state(t).PolicyHash != f.source.policy.identityHash() {
		t.Fatal("refused external policy replaced original checkpoint authority")
	}
}

func TestEconomicConservationNativeRenewalPublicRequiresIndependentInnerApproval(t *testing.T) {
	f, _, review := newEconomicConservationNativeRenewalFixture(t)
	for _, fault := range []string{"foreign-signature", "reduced-capacity", "changed-provider"} {
		changed := review
		switch fault {
		case "foreign-signature":
			changed.Signature = strings.Repeat("0", 128)
		case "reduced-capacity":
			changed.Next.MaximumJobs = 1
			nativeRenewalTestSign(t, &changed)
		case "changed-provider":
			changed.Next.Providers = []nativeProducerProvider{{Hotkey: "0x" + strings.Repeat("6", 64), Coldkey: "0x" + strings.Repeat("7", 64)}}
			nativeRenewalTestSign(t, &changed)
		}
		reference := nativeRenewalTestWrite(t, filepath.Join(f.metadata, "refused-inner-approval-"+fault+".json"), changed)
		request := f.request
		adoption := &economicConservationNativeRenewal{Schema: economicConservationNativeRenewalSchema, PolicyHash: f.source.policy.identityHash(), Original: request.Original, Ordinal: 1, Previous: rootObjectHash(f.source.policy.Native.Observation.Execution.Producer), ReviewSha256: monitorReadDigest([]byte("synthetic outer signature cannot grant inner authority " + fault)), To: []planFileReference{reference}}
		economicConservationNativeRenewalTestSign(t, adoption)
		request.NativeRenewal = adoption
		economicConservationRevisionTestRefuse(t, f, request, "native producer")
	}
}

func TestEconomicConservationNativeRenewalHeldArchiveCustodyBeforeDispatch(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	_, _, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic retained native custody")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	state.archiveView = view
	if err := state.requireNativeApprovalHistory(); err != nil {
		t.Fatal("positive original archive did not admit", err)
	}
	if err := os.Chmod(f.request.ArchivePath, 0644); err != nil {
		t.Fatal(err)
	}
	if err := state.requireNativeApprovalHistory(); err == nil {
		t.Fatal("changed retained native approval custody allowed dispatch")
	}
	if err := os.Chmod(f.request.ArchivePath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := view.close(); err != nil {
		t.Fatal(err)
	}
}

func TestEconomicConservationNativeRenewalRestoreAuthenticatesCompleteOriginalHistory(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	_, _, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic native restore approval")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	raw, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	original := monitorHistoryReference{Path: f.source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
	var reads int
	read := func(reference monitorHistoryReference) ([]byte, error) { reads++; return os.ReadFile(reference.Path) }
	if err := validateEconomicConservationRestoreHistory(f.ctx, f.source.policy, original, read, nil, monitorServiceHooks{}); err != nil || reads != 3 {
		t.Fatal("copied native adoption did not authenticate exact original history once", err, reads)
	}
	state := f.source.state(t)
	state.Archive.NativeApprovalHead.Renewals = nil
	state.ContentHash = state.hash()
	changed, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	changed = append(changed, '\n')
	original.Bytes, original.Sha256 = uint64(len(changed)), monitorReadDigest(changed)
	if err := validateEconomicConservationRestoreHistory(f.ctx, f.source.policy, original, func(reference monitorHistoryReference) ([]byte, error) {
		if reference.Path == original.Path {
			return changed, nil
		}
		return os.ReadFile(reference.Path)
	}, nil, monitorServiceHooks{}); err == nil {
		t.Fatal("self-sealed copied native head discarded its original approval lineage")
	}
}

func TestEconomicConservationNativeRenewalCanceledProposalAndUnownedIndexHaveNoEffects(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t)
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	request := economicConservationNativeRenewalRequest{Schema: "urnetwork-economic-native-approval-request-v1", Policy: f.source.policy, Original: f.request.Original, Renewals: references, ReviewSha256: monitorReadDigest([]byte("synthetic canceled native proposal"))}
	if value, err := proposeEconomicConservationNativeRenewal(ctx, request, monitorServiceHooks{}); !errors.Is(err, context.Canceled) || value.SigningBytes != "" || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("canceled native proposal published authority or changed history", err, value)
	}
	adoption, _, args := economicConservationNativeRenewalTestPlan(t, f, references, request.ReviewSha256)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if err := state.requireNativeApprovalHistory(); err == nil {
		t.Fatal("unadmitted native approval history allowed dispatch")
	}
	for _, view := range []*economicConservationArchiveView{nil, {}} {
		state.archiveView = view
		if err := applyEconomicConservationNativeRenewal(f.ctx, f.source.policy, &state, &adoption); err == nil {
			t.Fatal("missing original native review index authorized mutation")
		}
	}
}

// This is an actual restored producer and both original-program executables.
// The completed but unacknowledged third job must keep its old authority; only
// the following fresh job may adopt the independently signed next profile.
func TestEconomicConservationNativeRenewalPublicRestoredPendingJobAndRestart(t *testing.T) {
	var before economicConservationState
	var originalPolicy []byte
	var references []planFileReference
	fixture := newNativeProducerRestoreOptionsFixture(t, 2, false, func(fixture *nativeProducerRestoreFixture) {
		f, producer := fixture.combined.archive, fixture.producer
		before = f.source.state(t)
		var err error
		originalPolicy, err = os.ReadFile(f.source.path)
		if err != nil {
			t.Fatal(err)
		}
		// The fixture shares its execution pointer. New signed inputs must not
		// mutate the already pinned outer economic policy.
		raw, err := json.Marshal(producer.source.policy)
		if err != nil {
			t.Fatal(err)
		}
		var next economicEmissionPolicy
		if err := decodePlanJson(raw, &next); err != nil {
			t.Fatal(err)
		}
		producer.source.policy = next
		nativeRenewalTestPublicReview(t, producer, before.Native.ExecutionProducer)
		references = slices.Clone(producer.source.policy.Execution.Producer.Renewals)
		f.reset(t)
		_, _, args := economicConservationNativeRenewalTestPlan(t, f, references, monitorReadDigest([]byte("synthetic native approval after existing checkpoint")))
		if code, issue := f.apply(t, args, economicConservationWindowShortWriter{}, monitorServiceHooks{}); code == 0 || !strings.Contains(issue, "delivered") {
			t.Fatal("native adoption did not lose public acknowledgement", code, issue)
		}
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("native adoption did not reconcile before original source export", code, issue)
		}
	})
	// The new review is an original external authority input. Restoring an
	// older-engine pending job cannot excuse losing that adopted exact input.
	if err := os.Rename(references[0].Path, references[0].Path+".retained"); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(fixture.combined.archive.ctx, fixture.combined.args(t, fixture.combined.request), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "no such file") {
		t.Fatal("copied combined native adoption lost its exact external review", code, diagnostic.String())
	}
	fixture.combined.unchanged(t)
	if err := os.Rename(references[0].Path+".retained", references[0].Path); err != nil {
		t.Fatal(err)
	}
	fixture.combined.apply(t, fixture.combined.plan(t), true)
	f, producer := fixture.combined.archive, fixture.producer
	namespace := mainnetNamespaceTest(t, producer.source.policy.Execution.Directory)
	summary := f.sample(t, monitorServiceHooks{})
	resumed := f.source.state(t)
	if resumed.NativeHeld || resumed.NativeIssue != "" || resumed.Native.Cursor.Number != 103 || resumed.Native.ExecutionProducer.Completed != 3 || len(resumed.Native.ExecutionProducer.AuthorityRevisions) != 0 || resumed.Native.ExecutionProducer.AuthorityHash != before.Native.ExecutionProducer.AuthorityHash || summary.TargetMet != nil || resumed.PolicyHash != before.PolicyHash || producer.proofs.Load() != fixture.proofs || !reflect.DeepEqual(namespace, mainnetNamespaceTest(t, producer.source.policy.Execution.Directory)) {
		t.Fatal("combined restart replaced old pending authority, recaptured work or reset original progress", summary, resumed.NativeIssue, resumed.Native.ExecutionProducer)
	}
	f.sample(t, monitorServiceHooks{})
	adopted := f.source.state(t)
	if adopted.NativeHeld || adopted.NativeIssue != "" || adopted.Native.Cursor.Number != 104 || adopted.Native.ExecutionProducer.Completed != 4 || len(adopted.Native.ExecutionProducer.AuthorityRevisions) != 1 || adopted.Native.ExecutionProducer.AuthorityHash != references[0].Sha256 || adopted.Native.ExecutionProducer.AuthorityRevisions[0].AdoptedCompleted != 3 {
		t.Fatal("fresh original job did not adopt reviewed profile under retained combined owner", adopted.NativeIssue, adopted.Native.ExecutionProducer)
	}
	afterPolicy, err := os.ReadFile(f.source.path)
	if err != nil || !bytes.Equal(originalPolicy, afterPolicy) {
		t.Fatal("continued native observer rewrote immutable original policy", err)
	}
	if pending, err := os.ReadFile(fixture.pendingPath); err != nil || !bytes.Equal(pending, []byte{1, 2, 3}) {
		t.Fatal("approval adoption discarded incomplete original trie evidence", err)
	}
}

func TestEconomicConservationNativeRenewalReferenceBudgetAndIdentity(t *testing.T) {
	refs := make([]planFileReference, maximumNativeProducerRenewals)
	for index := range refs {
		refs[index] = planFileReference{Path: filepath.Join(t.TempDir(), fmt.Sprintf("approval-%03d.json", index)), Sha256: monitorReadDigest([]byte(fmt.Sprintf("synthetic review %d", index)))}
	}
	if !economicConservationNativeReferencesInclude(refs[:1], refs) {
		t.Fatal("full declared native reference capacity did not retain prefix")
	}
	if economicConservationNativeReferencesInclude(nil, append(slices.Clone(refs), planFileReference{Path: filepath.Join(t.TempDir(), "extra.json"), Sha256: monitorReadDigest([]byte("extra"))})) {
		t.Fatal("unreviewed native reference overflow acquired authority")
	}
	refs[len(refs)-1] = refs[0]
	if economicConservationNativeReferencesInclude(nil, refs) {
		t.Fatal("duplicate original approval identity was admitted")
	}
}
