//go:build linux

// A synced but unacknowledged completion retains its original signed economic
// policy even when a fault producer consistently rehashes its export inventory.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise both policy domains through the actual two-root restore command.
// The original pending job then resumes with no new capture or authority.
func TestNativeProducerRestorePublicPendingCompletionKeepsOriginalAllocationAndFeeAuthority(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	if state := f.combined.state.Native.ExecutionProducer; state == nil || state.Completed != 2 || state.Cursor.Number != 102 {
		t.Fatal("pending-policy fixture lost its acknowledged predecessor", state)
	}
	path := filepath.Join("runtime", "native", fmt.Sprintf("b%010d-%s", 103, strings.TrimPrefix(f.producer.source.chain.byHeight[103], "0x")), "complete.json")
	original := []byte(f.combined.files[1][path])
	if len(original) == 0 {
		t.Fatal("pending-policy fixture lacks its synced third completion")
	}
	f.combined.plan(t)
	for _, domain := range []string{"allocation", "fee"} {
		var completion nativeProducerCompletion
		if err := decodePlanJson(original, &completion); err != nil {
			t.Fatal(err)
		}
		if completion.Sequence != 3 || completion.Admission.Child.Number != 103 {
			t.Fatal("policy mutation did not select the unacknowledged original job")
		}
		if domain == "allocation" {
			if completion.Admission.Yuma == nil {
				completion.Admission.Yuma = &nativeYumaPolicy{Schema: nativeYumaSchema, LayoutSha256: monitorReadDigest([]byte(nativeYumaLayout)), MaximumWitnessBytes: 64 * 1024, HotBlockReserve: 1, MaximumEdges: 64, MaximumOperations: 200000}
			}
			completion.Admission.Yuma.ReviewSha256 = monitorReadDigest([]byte("synthetic foreign pending allocation review"))
			if err := completion.Admission.Yuma.validate(); err != nil {
				t.Fatal("policy fault must remain a valid foreign allocation descriptor", err)
			}
		} else {
			if completion.Admission.FeeCensus == nil {
				completion.Admission.FeeCensus = &nativeFeeCensusPolicy{Schema: nativeFeeCensusSchema, Participants: []string{"0x" + strings.Repeat("5", 64)}}
			}
			completion.Admission.FeeCensus.ReviewSha256 = monitorReadDigest([]byte("synthetic foreign pending fee review"))
			if err := completion.Admission.FeeCensus.validate(); err != nil {
				t.Fatal("policy fault must remain a valid foreign fee descriptor", err)
			}
		}
		raw, err := json.Marshal(completion)
		if err != nil {
			t.Fatal(err)
		}
		f.replaceReviewedMember(t, 1, path, append(raw, '\n'))
		var output, diagnostic bytes.Buffer
		code := runMain(f.combined.archive.ctx, f.combined.args(t, f.combined.request), &output, &diagnostic)
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "changed original allocation or fee authority") {
			t.Fatal("self-sealed pending completion replaced original economic authority", domain, code, diagnostic.String())
		}
		f.combined.unchanged(t)
		f.replaceReviewedMember(t, 1, path, original)
	}
	plan := f.combined.plan(t)
	f.combined.apply(t, plan, true)
	p := f.producer
	p.ctx = context.WithValue(f.combined.archive.ctx, nativeProducerStateKey{}, f.combined.state.Native.ExecutionProducer)
	observation, code, issue := p.command(t)
	if code != 0 || !observation.Complete || observation.ExecutionProducer == nil || observation.ExecutionProducer.Completed != 3 || observation.ExecutionProducer.Cursor.Number != 103 || p.proofs.Load() != f.proofs {
		t.Fatal("unchanged original pending policy could not resume retained job", code, issue)
	}
}
