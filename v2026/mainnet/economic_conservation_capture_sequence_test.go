// Complete original execution and receipt sequences exercise the per-pool
// ordinal cursor, including a later receipt after native evidence retires.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// Header digests bind the real fixture receipts. State roots, query results,
// original code and proof nodes retain the Rust exporter's exact identities.
func newEconomicCaptureSequenceFixture(t *testing.T, sameBlock bool) (*economicConservationArchiveFixture, *nativeProducerPublicFixture, []*economicCaptureContractFixture) {
	t.Helper()
	name := "capture-next-block"
	steps := []economicCaptureContractStep{{amount: 20, block: 11}, {amount: 6, block: 12}}
	if sameBlock {
		name, steps[1] = "capture-same-block", economicCaptureContractStep{amount: 5, block: 11}
	}
	var contracts []*economicCaptureContractFixture
	change := func(job *historicalReplayJob) {
		count := 1
		if sameBlock {
			count = 2
		}
		if len(contracts) != 2 || len(job.ExtrinsicsHex) != count {
			t.Fatal("complete original capture body census differs")
		}
		for index := range job.ExtrinsicsHex {
			job.ExtrinsicsHex[index] = nativeExecutionTestHex(append(rootCompact(32), contracts[index].transaction.Hash().Bytes()...))
		}
	}
	next := func(first historicalReplayJob, vault *monitorEvmFixture) []historicalReplayJob {
		if sameBlock {
			return nil
		}
		path := filepath.Join(os.Getenv("URNETWORK_NATIVE_VAULT_CAPTURE_SEQUENCE_FIXTURE_DIR"), "principal-capture-next-block-102.json")
		raw, _, err := readPlanFile(t.Context(), path, historicalNativeJobLimit)
		if err != nil {
			t.Fatal(err)
		}
		var job historicalReplayJob
		if err := decodePlanJson(raw, &job); err != nil || len(job.ExtrinsicsHex) != 1 {
			t.Fatal("original next capture job absent", err)
		}
		originalParent, number := nativeProducerTestHeader(t, job.ParentHeaderHex, job.ParentHash)
		firstChild, _ := nativeProducerTestHeader(t, first.ChildHeaderHex, first.ChildHash)
		if number != 101 || originalParent.StateRoot != firstChild.StateRoot {
			t.Fatal("next original proof did not begin at the first original post-state")
		}
		job.ParentHeaderHex, job.ParentHash = first.ChildHeaderHex, first.ChildHash
		body := append(rootCompact(32), contracts[1].transaction.Hash().Bytes()...)
		job.ExtrinsicsHex = []string{nativeExecutionTestHex(body)}
		// Admitted system versions zero and one use extrinsics layout zero.
		if job.ExecutionStateVersion > 1 {
			t.Fatal("fixture system version is unsupported")
		}
		root, err := rootExtrinsicsRoot([][]byte{body}, 0)
		if err != nil {
			t.Fatal(err)
		}
		headerRaw, err := historicalReplayHex(job.ChildHeaderHex, 64*1024)
		if err != nil {
			t.Fatal(err)
		}
		var header types.Header
		if err := codec.Decode(headerRaw, &header); err != nil {
			t.Fatal(err)
		}
		header.ParentHash = types.Hash(first.ChildHash)
		header.ExtrinsicsRoot, err = types.NewHashFromHexString(root)
		if err != nil {
			t.Fatal(err)
		}
		mapping, err := hex.DecodeString(strings.TrimPrefix(mappingTestDigest(t, 3, vault.blocks[12].header.Hash().Hex(), nil), "0x"))
		if err != nil {
			t.Fatal(err)
		}
		var digest types.DigestItem
		if err := codec.Decode(mapping, &digest); err != nil {
			t.Fatal(err)
		}
		header.Digest = append(header.Digest, digest)
		headerRaw, err = codec.Encode(header)
		if err != nil {
			t.Fatal(err)
		}
		job.ChildHeaderHex, job.ChildHash = nativeExecutionTestHex(headerRaw), historicalReplayDigest(blake2b.Sum256(headerRaw))
		return []historicalReplayJob{job}
	}
	f, producer := newEconomicConservationPrincipalFixtureWithSequence(t, name, true, change, next, func(vault *monitorEvmFixture) {
		contracts = economicCaptureExecuteContractSequence(t, vault, steps)
		vault.policy.BatchBlocks = 1
	})
	return f, producer, contracts
}

func economicCaptureSequenceAdvance(producer *nativeProducerPublicFixture) {
	producer.source.chain.stateLock.Lock()
	defer producer.source.chain.stateLock.Unlock()
	producer.source.chain.finalized = producer.source.chain.byHeight[102]
}

// The first original movement consumes the earnings; a later same-block
// deposit and capture cannot earn that first interval a second time.
func TestEconomicConservationPublicSameBlockCapturesKeepOriginalOrdinals(t *testing.T) {
	f, _, contracts := newEconomicCaptureSequenceFixture(t, true)
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.JoinIssue != "" || summary.CausallyJoinedCaptures != 2 || len(state.Captures) != 2 || state.Captures[0].PrincipalEffects == nil || state.Captures[1].PrincipalEffects == nil {
		t.Fatal("same-block original captures did not both join their exact receipts", summary, state.Captures)
	}
	first, second := state.Captures[0], state.Captures[1]
	a, b := first.PrincipalEffects, second.PrincipalEffects
	if *first.Native != *second.Native || a.ExtrinsicIndex != 0 || b.ExtrinsicIndex != 1 || a.Through.Ordinal >= b.Through.Ordinal || b.From != a.Through || b.PreviousCapture != first.Id || a.TransactionHash != contracts[0].transaction.Hash().Hex() || b.TransactionHash != contracts[1].transaction.Hash().Hex() || first.Event.TransactionIndex != 0 || second.Event.TransactionIndex != 1 || first.Event.ReceiptHash == second.Event.ReceiptHash {
		t.Fatal("same native block reused a capture ordinal or original transaction", a, b, first.Event, second.Event)
	}
	if a.OpeningStock != "14" || a.LiquidEarnings != "6" || a.Captured != "20" || b.OpeningStock != "0" || b.Deposits != "5" || b.LiquidEarnings != "0" || b.Captured != "5" || b.After != "0" || *first.KnownLiquidAlpha != "6" || *second.KnownLiquidAlpha != "0" || *second.AmountDifferenceAlpha != "5" || *summary.TailGrossAlpha != "9" || summary.TargetMet != nil || summary.ActualNativeOutcomeVerified || contracts[1].escrowAfter != "25" {
		t.Fatal("same-block principal deposit became repeated native earnings", summary, a, b)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	if state := f.source.state(t); len(state.Captures) != 0 || state.Archive.Counts.CausalCaptures != 2 {
		t.Fatal("same-block cursor failed to retire complete originals", state)
	}
	if reopened := f.sample(t, monitorServiceHooks{}); reopened.CausallyJoinedCaptures != 2 || *reopened.TailGrossAlpha != "9" || reopened.TargetMet != nil {
		t.Fatal("cold same-block capture census was recounted", reopened)
	}
}

func TestEconomicConservationPublicNextCaptureContinuesArchivedOriginalCursor(t *testing.T) {
	f, producer, contracts := newEconomicCaptureSequenceFixture(t, false)
	first := economicCaptureTestWitness(t, f, f.sample(t, monitorServiceHooks{}))
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	if state := f.source.state(t); len(state.Captures) != 0 || len(state.PrincipalExecutions) != 0 || state.Archive.Counts.CausalCaptures != 1 {
		t.Fatal("next-block fixture did not retire its first original capture", state)
	}
	economicCaptureSequenceAdvance(producer)
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.JoinIssue != "" || summary.CausallyJoinedCaptures != 2 || len(state.Captures) != 1 || state.Captures[0].PrincipalEffects == nil {
		t.Fatal("next original capture lost its retired cursor", summary, state.Captures)
	}
	second := state.Captures[0].PrincipalEffects
	if second.From != first.PrincipalEffects.Through || second.PreviousCapture != first.Id || second.Through.Boundary.Number != 102 || second.OpeningStock != "0" || second.LiquidEarnings != "6" || second.Captured != "6" || second.After != "0" || second.TransactionHash != contracts[1].transaction.Hash().Hex() || *summary.TailGrossAlpha != "18" || summary.OpeningPrincipalAlpha == nil || *summary.OpeningPrincipalAlpha != "14" || summary.TargetMet != nil || contracts[1].escrowAfter != "26" {
		t.Fatal("archived cursor reset stock or lost the next actual earning interval", summary, first, second)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	if reopened := f.sample(t, monitorServiceHooks{}); reopened.CausallyJoinedCaptures != 2 || *reopened.TailGrossAlpha != "18" || reopened.TargetMet != nil {
		t.Fatal("cold next-block captures were replayed or recounted", reopened)
	}
}

// Missing an earlier receipt keeps the whole vault page pending while native
// and Claim observation continue. Both native proofs retire before recovery.
func TestEconomicConservationPublicDelayedFirstReceiptJoinsBothArchivedIntervals(t *testing.T) {
	f, producer, _ := newEconomicCaptureSequenceFixture(t, false)
	f.source.vault.unavailable.Store(true)
	var waits atomic.Uint64
	sample := func() economicConservationSummary {
		var output, issue bytes.Buffer
		code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
			if role != f.source.policy.Vault.Role {
				t.Error("first-receipt outage blocked another source", role)
			}
			waits.Add(1)
			return context.DeadlineExceeded
		}})
		var summary economicConservationSummary
		if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
			t.Fatal(err, issue.String())
		}
		if code != 3 || !summary.NativeCurrent || summary.VaultCurrent || summary.VaultHeld || summary.VaultCursor != f.source.policy.Vault.From || summary.Captures != 0 || f.source.claimReads.Load() == 0 {
			t.Fatal("missing first receipt advanced its page or blocked healthy siblings", code, summary, issue.String())
		}
		return summary
	}
	first := sample()
	economicCaptureSequenceAdvance(producer)
	second := sample()
	if first.NativeCursor.Number != 101 || second.NativeCursor.Number != 102 || waits.Load() != 2 || len(f.source.state(t).PrincipalExecutions) != 2 {
		t.Fatal("native sequence failed to advance behind the first missing receipt", first, second, waits.Load())
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	if state := f.source.state(t); len(state.PrincipalExecutions) != 0 || state.Archive.PrincipalEffects == nil {
		t.Fatal("delayed receipt did not use genuinely retired original execution", state)
	}
	proofReads := producer.proofs.Load()
	f.source.vault.unavailable.Store(false)
	recovered := f.sample(t, monitorServiceHooks{})
	if recovered.CausallyJoinedCaptures != 1 || recovered.VaultCursor.Number != 11 {
		t.Fatal("earlier receipt did not recover first from its retained original input", recovered)
	}
	last := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if last.CausallyJoinedCaptures != 2 || last.Captures != 2 || last.VaultCursor.Number != 12 || last.NativeCursor != second.NativeCursor || len(state.PrincipalExecutions) != 0 || len(state.Captures) != 2 || state.Captures[1].PrincipalEffects == nil || state.Captures[1].PrincipalEffects.PreviousCapture != state.Captures[0].Id || state.Captures[1].PrincipalEffects.OpeningStock != "0" || *last.TailGrossAlpha != "18" || producer.proofs.Load() != proofReads || last.TargetMet != nil {
		t.Fatal("later receipt skipped the original prefix or recaptured retired native evidence", last, state.Captures)
	}
}
