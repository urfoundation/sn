//go:build linux

// Public conservation obtains these proofs from the actual capture/replay
// producer. The original receipt reader, durable archive and restart own the
// data; no caller-authored finality boolean or replacement amount is admitted.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Two real original capture jobs certify different receipt blocks. Their
// complete proof windows survive checkpoint acknowledgement loss and cold read.
func TestEconomicFinalityPublicCaptureArchiveRestartKeepsOriginalCertificates(t *testing.T) {
	f, producer, _ := newEconomicCaptureSequenceFixture(t, false)
	first := f.sample(t, monitorServiceHooks{})
	if first.OriginalFinality == nil || !first.OriginalFinality.Complete || first.OriginalFinality.Head.Windows != 1 || first.OriginalFinality.Head.VaultBlocks != 1 || first.OriginalFinality.Head.Certified != producer.source.policy.Through || first.TargetMet != nil || first.ActivationReady {
		t.Fatal("actual original capture did not derive independent finality only", first)
	}
	state := f.source.state(t)
	if len(state.FinalityWindows) != 1 || len(state.FinalityApproval) == 0 || state.FinalityWindows[0].Anchor.Hash() != producer.authority.Checkpoint.Hash() {
		t.Fatal("completed producer dropped its original consensus witness", state.FinalityWindows)
	}
	f.reset(t)
	_, args := f.plan(t)
	reached := false
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{syncDirectory: func(_, step string, file *os.File) error {
		if step == "checkpoint" {
			reached = true
			return errors.Join(file.Sync(), syscall.EIO)
		}
		return file.Sync()
	}}); code != 2 || !reached || !strings.Contains(issue, syscall.EIO.Error()) {
		t.Fatal("original consensus lost acknowledgement control did not occur", code, issue)
	}
	for range 2 {
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("exact original consensus plan failed acknowledgement recovery", code, issue)
		}
	}
	economicCaptureSequenceAdvance(producer)
	second := f.sample(t, monitorServiceHooks{})
	if second.OriginalFinality == nil || !second.OriginalFinality.Complete || second.OriginalFinality.Head.Windows != 2 || second.OriginalFinality.Head.VaultBlocks != 2 || second.OriginalFinality.Head.VaultThrough != second.VaultCursor || second.CausallyJoinedCaptures != 2 || second.TargetMet != nil {
		t.Fatal("cold original certificate handoff skipped or repeated a capture", second)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state = f.source.state(t)
	if state.Archive.Finality == nil || len(state.FinalityWindows) != 0 || len(state.FinalityVaultBlocks) != 0 {
		t.Fatal("complete consensus proof did not retire into original held snapshots")
	}
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	}()
	state.archiveView = view
	cold, err := state.finalitySummary(f.ctx, f.source.policy)
	if err != nil || !reflect.DeepEqual(cold, second.OriginalFinality) {
		t.Fatal("restart lost original certified interval", err, cold, second.OriginalFinality)
	}
}

// The same public producer cannot promote its own changed certificate. Native
// refusal retains the previous economic cursor while receipt/Claim roles run.
func TestEconomicFinalityPublicForgedCertificateKeepsHealthySiblings(t *testing.T) {
	f, producer, _ := newEconomicCaptureSequenceFixture(t, false)
	producer.corruptCertificate.Store(true)
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var value economicConservationSummary
	if code != 3 || json.Unmarshal(output.Bytes(), &value) != nil {
		t.Fatal("public forged certificate did not produce a held native sample", code, diagnostic.String(), output.String())
	}
	state := f.source.state(t)
	if value.NativeCurrent || !value.VaultCurrent || state.Native.Cursor != f.source.policy.Native.Observation.From || len(state.FinalityWindows) != 0 || value.OriginalFinality == nil || value.OriginalFinality.Complete || value.TargetMet != nil || f.source.claimReads.Load() == 0 {
		t.Fatal("forged native certificate became finality or stopped healthy siblings", value)
	}
}

// Late original-owner replacement is checked after actual certificate work.
// The public command must publish no partial summary even with equal bytes.
func TestEconomicFinalityPublicLateCustodyLossPublishesNoCertificateIndex(t *testing.T) {
	f, _, _ := newEconomicCaptureSequenceFixture(t, false)
	first := f.sample(t, monitorServiceHooks{})
	if first.OriginalFinality == nil || !first.OriginalFinality.Complete {
		t.Fatal("healthy original finality absent", first)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	path := f.source.state(t).Archive.Segments[0].Path
	calls := 0
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{economicFinalityWork: func(context.Context) { calls++; economicConservationReplaceArchiveForTest(t, path) }})
	if code != 3 || output.Len() != 0 || calls != 1 || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
		t.Fatal("late original certificate owner loss published summary", code, calls, diagnostic.String())
	}
}

// A held original proof is the unit of reuse; repeated hot projections retain
// its exact cache, while a canceled owner cannot expose that cache as evidence.
func TestEconomicFinalityPublicCanceledCacheKeepsOriginalState(t *testing.T) {
	f, _, _ := newEconomicCaptureSequenceFixture(t, false)
	first := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	}()
	state.archiveView = view
	entries, charged := view.entries, view.bytes
	directory := f.source.policy.Native.Observation.Execution.Directory
	before := mainnetNamespaceTest(t, directory)
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if value, err := state.finalitySummary(ctx, f.source.policy); value != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled certificate cache exposed finality", value, err)
	}
	value, err := state.finalitySummary(f.ctx, f.source.policy)
	if err != nil || !reflect.DeepEqual(value, first.OriginalFinality) || view.entries != entries || view.bytes != charged || !reflect.DeepEqual(before, mainnetNamespaceTest(t, directory)) {
		t.Fatal("canceled owner changed original certificate or healthy continuation", value, err)
	}
}

// Cancellation after verification of a new original window must not replace
// the writer's checkpoint. The already completed native job resumes through the
// same producer and is accounted exactly once by a later healthy invocation.
func TestEconomicFinalityPublicCanceledAppendKeepsPublishedCheckpoint(t *testing.T) {
	f, producer, _ := newEconomicCaptureSequenceFixture(t, false)
	first := f.sample(t, monitorServiceHooks{})
	before, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	economicCaptureSequenceAdvance(producer)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	calls := 0
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{economicFinalityWork: func(context.Context) {
		calls++
		if calls == 2 {
			cancel()
		}
	}})
	after, err := os.ReadFile(f.source.checkpoint)
	if err != nil || code != 0 || calls != 2 || output.Len() != 0 || !bytes.Equal(before, after) {
		t.Fatal("canceled original consensus append changed published checkpoint", code, calls, err, diagnostic.String())
	}
	resumed := f.sample(t, monitorServiceHooks{})
	if resumed.OriginalFinality == nil || !resumed.OriginalFinality.Complete || resumed.OriginalFinality.Head.Windows != 2 || resumed.CausallyJoinedCaptures != first.CausallyJoinedCaptures+1 {
		t.Fatal("healthy original job could not resume after canceled consensus append", resumed)
	}
}
