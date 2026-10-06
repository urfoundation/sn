//go:build linux

// Completion framing preserves the original fee authority and generation
// census. These controls exercise physical publication, not synthetic execution.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The actual admitted original document supplies all 4096 provider pairs and
// 8192 participant accounts; no amount or VM authority is inferred from size.
func nativeCompletionFullFeeTestRecord(t *testing.T) nativeProducerCompletion {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	scope := nativeRestoreFullFeeTestScope(t, root)
	authority, err := loadNativeProducerAuthority(t.Context(), scope.Policy)
	if err != nil {
		t.Fatal(err)
	}
	admission := nativeExecutionAdmission{Schema: nativeExecutionAdmissionSchema, Network: scope.Policy.Network, Netuid: scope.Policy.Netuid, Registration: authority.Registration, Generation: authority.Generation, Parent: authority.From, Child: scope.Policy.Through, Runtime: authority.Runtime, ReviewSha256: authority.ReviewSha256, ProfileSha256: scope.Policy.Execution.ProfileSha256, EngineSha256: authority.ReplayEngine.Sha256, Job: planFileReference{Path: filepath.Join(scope.Policy.Execution.Directory, "job.json"), Sha256: monitorReadDigest([]byte("original job"))}, FeeCensus: authority.FeeCensus, FinalityAuthority: "approved-anchor-and-verified-grandpa-original-execution"}
	for index, provider := range authority.Providers {
		admission.Providers = append(admission.Providers, nativeExecutionRecipient{Uid: uint16(index), Hotkey: provider.Hotkey, Coldkey: provider.Coldkey, Registered: 0})
	}
	return nativeProducerCompletion{Schema: nativeProducerCompletionSchema, AuthorityHash: scope.Policy.Execution.Producer.Authority.Sha256, Previous: scope.Policy.Execution.Producer.Authority.Sha256, Sequence: 1, Input: planFileReference{Path: filepath.Join(scope.Policy.Execution.Directory, "input.json"), Sha256: monitorReadDigest([]byte("original input"))}, Admission: admission, Anchor: authority.Checkpoint, Window: planFileReference{Path: filepath.Join(scope.Policy.Execution.Directory, "native-proof.json"), Sha256: monitorReadDigest([]byte("original finality"))}, Certified: scope.Policy.Through, OutcomeHash: monitorReadDigest([]byte("original outcome")), ResourceForecast: &nativeProducerResourceForecast{BytesUpperBound: nativeProducerBoundaryReserve, EntriesUpperBound: nativeProducerBoundaryEntries}}
}

// A full truthful census must retain twice its measured serialized size. Both
// the active owner and restored pending/completed members use that same frame.
func TestNativeProducerCompletionFullFeeCensusKeepsTwoCopyMargin(t *testing.T) {
	completion := nativeCompletionFullFeeTestRecord(t)
	raw, err := json.Marshal(completion)
	if err != nil {
		t.Fatal(err)
	}
	maximum := nativeProducerCompletionMaximum(completion.Admission.FeeCensus)
	if len(completion.Admission.Providers) != 4096 || len(completion.Admission.FeeCensus.Participants) != 8192 || 2*(len(raw)+1) <= nativeProducerCompletionLimit || 2*(len(raw)+1) > maximum {
		t.Fatal("full original completion census lost its explicit two-copy frame", len(raw), maximum)
	}
	boundary := "b0000000101-" + strings.Repeat("a", 64)
	for _, suffix := range []string{"complete.json", "complete.json.pending"} {
		admitted, pending, ok := storageNativeProducerMemberFor(boundary+"/"+suffix, completion.Admission.FeeCensus)
		legacy, legacyPending, legacyOk := storageNativeProducerMember(boundary + "/" + suffix)
		if !ok || !legacyOk || admitted != maximum || legacy != nativeProducerCompletionLimit || pending != strings.HasSuffix(suffix, ".pending") || pending != legacyPending {
			t.Fatal("restored completion frame differs from original live authority", suffix, admitted, legacy)
		}
	}
	if nativeProducerCompletionMaximum(nil) != nativeProducerCompletionLimit {
		t.Fatal("legacy completion frame changed without original fee authority")
	}
}

// Legal JSON whitespace fills the declared physical frame without inventing
// extra providers. Exact bound publication/read/retry and one-byte excess are
// checked under real original private custody; this is not an execution proof.
func TestNativeProducerCompletionFramePublishesExactBoundAndRefusesExcess(t *testing.T) {
	completion := nativeCompletionFullFeeTestRecord(t)
	raw, err := json.Marshal(completion)
	if err != nil {
		t.Fatal(err)
	}
	maximum := nativeProducerCompletionMaximum(completion.Admission.FeeCensus)
	if len(raw) >= maximum {
		t.Fatal("full original completion exceeds its admitted frame")
	}
	raw = append(raw, bytes.Repeat([]byte(" "), maximum-len(raw))...)
	_, _, files := nativeProducerTestFiles(t, nil)
	defer files.close()
	path := fmt.Sprintf("b%010d-%s/complete.json", 101, strings.Repeat("b", 64))
	reference, err := files.publish(path, raw, maximum)
	if err != nil {
		t.Fatal("fee completion exact frame could not publish", err)
	}
	observed, err := files.readReference(reference, maximum)
	if err != nil || !bytes.Equal(observed, raw) {
		t.Fatal("fee completion exact frame could not be reopened", err)
	}
	var decoded nativeProducerCompletion
	if err := decodePlanJson(observed, &decoded); err != nil || rootObjectHash(decoded) != rootObjectHash(completion) {
		t.Fatal("original completion changed through physical framing", err)
	}
	if _, err := files.readReference(reference, nativeProducerCompletionMaximum(nil)); err == nil {
		t.Fatal("legacy completion reader acquired the fee frame")
	}
	if retry, err := files.publish(path, raw, maximum); err != nil || retry != reference {
		t.Fatal("identical completion frame retry changed original reference", err)
	}
	oversized := path + ".other"
	if _, err := files.publish(oversized, append(raw, ' '), maximum); err == nil {
		t.Fatal("completion frame accepted one byte beyond its original authority")
	}
	if _, err := os.Lstat(filepath.Join(files.path, oversized)); !os.IsNotExist(err) {
		t.Fatal("overbound completion publication changed the original namespace", err)
	}
}
