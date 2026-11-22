// A synthetic service approver authorizes only bounded original-owner reopen.
// Real journals and the existing local chain fixture supply the causal effects.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The key is the independently provisioned synthetic service key, never the
// native signing key. The original journal exists before recovery approval.
func newRootServiceRecoveryFixture(t *testing.T) (*rootServiceRuntimeFixture, rootServiceRuntimePreparation, rootServiceRecoveryApproval, time.Time) {
	t.Helper()
	fixture := newRootServiceRuntimeFixture(t)
	fixture.prepare(t)
	ctx := fixture.root.storage.Context
	preparation, err := loadRootServiceRuntime(ctx, fixture.input.Path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := openRootServiceStore(preparation.Root.Service, false, ctx)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err := errors.Join(err, store.close()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	approval := rootServiceRecoveryApproval{Schema: rootServiceRecoverySchema, RuntimeSha256: preparation.Input.Sha256,
		ServiceConfigHash: rootObjectHash(preparation.Root.Service), InitialServiceStateHash: record.ContentHash,
		DurableVolumes: fixture.root.storage.Reference, MaximumOpens: 2, MaximumStepsPerOpen: 1, IntervalSeconds: 60,
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	rootServiceRecoveryTestSign(t, &approval)
	return fixture, preparation, approval, now
}

// Signing one explicit test policy never changes original service/action bytes.
func rootServiceRecoveryTestSign(t *testing.T, approval *rootServiceRecoveryApproval) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5c}, ed25519.SeedSize))
	raw, err := approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	approval.Signature = hex.EncodeToString(ed25519.Sign(key, raw))
}

// A post-rename loss closes every child before the next independently budgeted
// open. The prior pending observation and original native request survive.
func TestRootServiceRecoveryJoinsAndReopensOriginalOwner(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	fixture.weights(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	var first *rootServiceRuntime
	var retained rootServiceRecord
	opens, waits, writes := 0, 0, 0
	result, err := runRootServiceRecovery(ctx, preparation, approval, nil, rootServiceRecoveryPorts{
		now: func() time.Time { return now },
		wait: func(context.Context, time.Duration) bool {
			waits++
			if first == nil || first.serviceStore.lock != nil || first.custodyStore.lock != nil || first.submissionStore.lock != nil {
				t.Fatal("recovery waited before the original runtime joined and closed")
			}
			raw, err := os.ReadFile(preparation.Root.Service.Packet.Action.Scope.StatePath)
			if err := errors.Join(err, decodePlanJson(raw, &retained)); err != nil || retained.Recovery.OpenAttempts != 1 || retained.Observations != 1 || !retained.ObservationPending {
				t.Fatal("uncertain original observation or consumed allowance was lost", err)
			}
			now = now.Add(time.Minute)
			return true
		},
		open: func(ctx context.Context, p rootServiceRuntimePreparation, create bool) (*rootServiceRuntime, error) {
			opens++
			if create {
				t.Fatal("recovery requested fresh child creation")
			}
			runtime, err := openRootServiceRuntime(ctx, p, false)
			if err == nil && opens == 1 {
				first = runtime
				runtime.serviceStore.syncDirectoryForTest = func(*os.File) error {
					writes++
					return errors.New("synthetic lost original observation sync")
				}
			}
			if err == nil && (runtime.service.ports.Authority != nil || runtime.service.ports.Submitter != nil) {
				t.Fatal("recovery instantiated fresh native authority")
			}
			return runtime, err
		},
	})
	if err != nil || result.Failed || opens != 2 || waits != 1 || writes != 1 || result.RecoveryOpenAttempts != 2 || result.NativeSigning || result.NetworkEffects || len(fixture.submission.sent()) != 0 {
		t.Fatalf("bounded original continuation differs: %+v %v opens=%d waits=%d writes=%d", result, err, opens, waits, writes)
	}
	store, err := openRootServiceStore(preparation.Root.Service, false, ctx)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err := errors.Join(err, store.close()); err != nil || record.Recovery.OpenAttempts != 2 || record.Observations != 2 || record.Action.Action.RequestHash != retained.Action.Action.RequestHash || record.Action.Action.Nonce != retained.Action.Action.Nonce || record.Action.Signature != retained.Action.Signature {
		t.Fatal("recovery rewrote original intent or replenished observations", err)
	}
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal("idempotent original preparation", err)
	}
	if _, _, err := reserveRootServiceRecovery(ctx, preparation, approval, now.Add(time.Minute)); err == nil {
		t.Fatal("process restart or repeated prepare replenished the lifetime allowance")
	}
}

// Reserving an open and crashing before it starts consumes that attempt. A new
// process honors the retained cadence and cannot reset the original policy.
func TestRootServiceRecoveryCrashRetainsAllowanceAndCadence(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	if attempt, wait, err := reserveRootServiceRecovery(ctx, preparation, approval, now); err != nil || attempt != 1 || wait != 0 {
		t.Fatal("first reservation", attempt, wait, err)
	}
	if attempt, wait, err := reserveRootServiceRecovery(ctx, preparation, approval, now); err != nil || attempt != 1 || wait != time.Minute {
		t.Fatal("restart skipped retained cadence", attempt, wait, err)
	}
	if _, _, err := reserveRootServiceRecovery(ctx, preparation, approval, now.Add(-time.Second)); err == nil {
		t.Fatal("clock rollback replenished recovery cadence")
	}
	if attempt, wait, err := reserveRootServiceRecovery(ctx, preparation, approval, now.Add(time.Minute)); err != nil || attempt != 2 || wait != 0 {
		t.Fatal("same original allowance could not resume", attempt, wait, err)
	}
	if _, _, err := reserveRootServiceRecovery(ctx, preparation, approval, now.Add(2*time.Minute)); err == nil {
		t.Fatal("third attempt exceeded signed lifetime allowance")
	}
	changed := approval
	changed.MaximumOpens = 4
	rootServiceRecoveryTestSign(t, &changed)
	if err := prepareRootServiceRecovery(ctx, preparation, changed, now); err == nil {
		t.Fatal("new approval replaced consumed original recovery scope")
	}
}

// A real mount-generation loss is terminal for this controller even though
// the old root inode still exists. No wait or second owner is admitted.
func TestRootServiceRecoveryRefusesIdentityLoss(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	opens, waits := 0, 0
	_, err := runRootServiceRecovery(ctx, preparation, approval, nil, rootServiceRecoveryPorts{
		now:  func() time.Time { return now },
		wait: func(context.Context, time.Duration) bool { waits++; return true },
		open: func(ctx context.Context, p rootServiceRuntimePreparation, create bool) (*rootServiceRuntime, error) {
			opens++
			runtime, err := openRootServiceRuntime(ctx, p, create)
			if err == nil {
				fixture.root.storage.Host.ReplaceMount()
			}
			return runtime, err
		},
	})
	if !errors.Is(err, durablevolume.ErrIdentity) || opens != 1 || waits != 0 || len(fixture.submission.sent()) != 0 {
		t.Fatal("identity loss became an automatic recovery opportunity", opens, waits, err)
	}
}

// Public recovery entrypoints reject an unsigned/changed policy and a missing
// physical declaration before adding any counter or opening a chain route.
func TestRootServiceRecoveryPublicPolicyIsSeparate(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	path := preparation.Root.Service.Packet.Action.Scope.StatePath
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*rootServiceRecoveryApproval){
		func(p *rootServiceRecoveryApproval) { p.Signature = "" },
		func(p *rootServiceRecoveryApproval) { p.MaximumOpens = 3 },
		func(p *rootServiceRecoveryApproval) { p.IntervalSeconds = 10; rootServiceRecoveryTestSign(t, p) },
		func(p *rootServiceRecoveryApproval) {
			p.InitialServiceStateHash = rootObjectHash("another original journal")
			rootServiceRecoveryTestSign(t, p)
		},
		func(p *rootServiceRecoveryApproval) {
			p.DurableVolumes.Sha256 = rootObjectHash("another generation")
			rootServiceRecoveryTestSign(t, p)
		},
	} {
		changed := approval
		change(&changed)
		ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(fixture.input.Path), "recovery-approval.json"), changed)
		if code, _, _ := fixture.command(t, t.Context(), "prepare-recovery", "--recovery-approval", ref.Path, "--recovery-approval-sha256", ref.Sha256); code == 0 {
			t.Fatal("public recovery accepted a different/unsigned policy")
		}
	}
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(fixture.input.Path), "recovery-approval.json"), approval)
	args := []string{"root-service", "prepare-recovery", "--config", fixture.input.Path, "--accept-runtime-sha256", fixture.input.Sha256, "--recovery-approval", ref.Path, "--recovery-approval-sha256", ref.Sha256}
	var output bytes.Buffer
	if code := runMain(t.Context(), args, &output, &output); code == 0 {
		t.Fatal("public recovery accepted missing physical declaration")
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) || len(fixture.submission.sent()) != 0 {
		t.Fatal("refused recovery changed original state or submitted", err)
	}
	if err := prepareRootServiceRecovery(fixture.root.storage.Context, preparation, approval, now); err != nil {
		t.Fatal("separately approved positive control", err)
	}
}

// Loss of the existing journal is not an empty counter. Recovery cannot use
// its approval to create a replacement or silently reset completed progress.
func TestRootServiceRecoveryMissingJournalDoesNotRecreate(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	path := preparation.Root.Service.Packet.Action.Scope.StatePath
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err == nil {
		t.Fatal("repeated preparation recreated a missing journal")
	}
	if _, _, err := reserveRootServiceRecovery(ctx, preparation, approval, now); err == nil {
		t.Fatal("recovery reserved from missing original custody")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing journal was recreated", err)
	}
}

// Lost acknowledgement of the recovery reservation cannot open the runtime.
// The next invocation reconciles the committed counter before spending again.
func TestRootServiceRecoveryUncertainReservationStopsBeforeOpen(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	opens, waits, writes := 0, 0, 0
	_, err := runRootServiceRecovery(ctx, preparation, approval, nil, rootServiceRecoveryPorts{
		now:  func() time.Time { return now },
		wait: func(context.Context, time.Duration) bool { waits++; return true },
		open: func(context.Context, rootServiceRuntimePreparation, bool) (*rootServiceRuntime, error) {
			opens++
			return nil, errors.New("unadmitted runtime open")
		},
		reserve: func(ctx context.Context, p rootServiceRuntimePreparation, approval rootServiceRecoveryApproval, now time.Time) (uint32, time.Duration, error) {
			return reserveRootServiceRecoveryWithSync(ctx, p, approval, now, func(*os.File) error {
				writes++
				return errors.New("synthetic lost recovery-counter sync")
			})
		},
	})
	if !errors.Is(err, errMainnetDurablePublicationUncertain) || errors.Is(err, durablevolume.ErrIdentity) || opens != 0 || waits != 0 || writes != 1 {
		t.Fatal("uncertain reservation reached runtime or retried itself", opens, waits, writes, err)
	}
	if attempt, wait, err := reserveRootServiceRecovery(ctx, preparation, approval, now); err != nil || attempt != 1 || wait != time.Minute {
		t.Fatal("original committed reservation was not recovered", attempt, wait, err)
	}
}

// Signature recovery is exercised before observation. Reopening an uncertain
// copy keeps the original signature, nonce and broadcast floor byte-for-byte.
func TestRootServiceRecoveryPreservesIssuedNativeBytes(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	ctx := fixture.root.storage.Context
	receipt := fixture.root.offline.receipt(t)
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(fixture.input.Path), "signature.json"), receipt)
	fixture.root.result(t, "resume", "--signature-file", ref.Path, "--signature-sha256", ref.Sha256)
	signature, err := hex.DecodeString(receipt.Signature)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := preparation.Root.Service.Packet.Action.signed(signature)
	if err != nil {
		t.Fatal(err)
	}
	fixture.submission.finalize(t, raw)
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	opens := 0
	result, err := runRootServiceRecovery(ctx, preparation, approval, nil, rootServiceRecoveryPorts{
		now:  func() time.Time { return now },
		wait: func(context.Context, time.Duration) bool { now = now.Add(time.Minute); return true },
		open: func(ctx context.Context, p rootServiceRuntimePreparation, create bool) (*rootServiceRuntime, error) {
			opens++
			runtime, err := openRootServiceRuntime(ctx, p, create)
			if err == nil && opens == 1 {
				runtime.serviceStore.syncDirectoryForTest = func(*os.File) error { return errors.New("synthetic lost signature-copy sync") }
			}
			return runtime, err
		},
	})
	if err != nil || opens != 2 || result.ActionPhase != "finalized" || result.ExtrinsicHash != rootExtrinsicHash(raw) || !result.RecoveredSignature || result.Observations != 0 || result.Broadcasts != 0 || len(fixture.submission.sent()) != 0 {
		t.Fatalf("original native liability changed across recovery: %+v %v opens=%d", result, err, opens)
	}
	store, err := openRootServiceStore(preparation.Root.Service, false, ctx)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err := errors.Join(err, store.close()); err != nil || record.Action.Signature != receipt.Signature || record.Action.RawExtrinsic != "0x"+hex.EncodeToString(raw) || record.Action.Action.Nonce != preparation.Root.Service.Packet.Action.Nonce {
		t.Fatal("recovery replaced original signed bytes", err)
	}
}

// A new command requires explicit separately signed policy, while omitted old
// fields retain their prior bytes. A successful bounded run creates no signer.
func TestRootServiceRecoveryPublicRunPreservesLegacyEncoding(t *testing.T) {
	fixture, preparation, approval, _ := newRootServiceRecoveryFixture(t)
	fixture.weights(t)
	path := preparation.Root.Service.Packet.Action.Scope.StatePath
	before, err := os.ReadFile(path)
	var record rootServiceRecord
	if err := errors.Join(err, decodePlanJson(before, &record)); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil || record.Recovery != nil || bytes.Contains(encoded, []byte(`"recovery"`)) || !bytes.Equal(append(encoded, '\n'), before) {
		t.Fatal("optional recovery changed historical journal encoding", err)
	}
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(fixture.input.Path), "recovery-approval.json"), approval)
	args := []string{"--recovery-approval", ref.Path, "--recovery-approval-sha256", ref.Sha256}
	if code, result, detail := fixture.command(t, t.Context(), "prepare-recovery", args...); code != 0 || result.RecoveryApprovalHash != rootObjectHash(approval) {
		t.Fatal("public bounded recovery preparation", code, result, detail)
	}
	if code, result, detail := fixture.command(t, t.Context(), "run-recovery", args...); code != 0 || result.RecoveryOpenAttempts != 1 || result.Observations != 1 || result.NativeSigning || result.NetworkEffects || result.ActivationReady {
		t.Fatal("public bounded recovery run", code, result, detail)
	}
	if code, _, _ := fixture.command(t, t.Context(), "run", args...); code == 0 {
		t.Fatal("legacy run silently selected the new recovery policy")
	}
}

// Reserve pressure pauses admission without consuming an attempt. A canceled
// request likewise cannot mutate the retained recovery counter.
func TestRootServiceRecoveryReserveAndCancellationDoNotConsume(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	path := preparation.Root.Service.Packet.Action.Scope.StatePath
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.root.storage.Host.SetReserve(0, 0)
	if _, _, err := reserveRootServiceRecovery(ctx, preparation, approval, now); !errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("full-volume reservation did not remain pending", err)
	}
	fixture.root.storage.Host.SetReserve(1024*1024*1024, 1024*1024)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := reserveRootServiceRecovery(canceled, preparation, approval, now); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled reservation admitted", err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatal("preadmission failure consumed recovery allowance", err)
	}
	if attempt, delay, err := reserveRootServiceRecovery(ctx, preparation, approval, now); err != nil || attempt != 1 || delay != 0 {
		t.Fatal("original recovery allowance did not resume", attempt, delay, err)
	}
}

// Counter admission is part of the same bounded controller. Pressure before
// mutation waits at the signed cadence and resumes without spending an open.
func TestRootServiceRecoveryWaitsForCounterAdmission(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	fixture.weights(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(preparation.Root.Service.Packet.Action.Scope.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.root.storage.Host.SetReserve(0, 0)
	opens, waits := 0, 0
	result, err := runRootServiceRecovery(ctx, preparation, approval, nil, rootServiceRecoveryPorts{
		now: func() time.Time { return now },
		wait: func(ctx context.Context, delay time.Duration) bool {
			waits++
			if delay != time.Minute || opens != 0 || ctx.Err() != nil {
				t.Fatal("preadmission pressure used unapproved cadence or opened runtime", delay, opens, ctx.Err())
			}
			after, err := os.ReadFile(preparation.Root.Service.Packet.Action.Scope.StatePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("pressure spent original recovery allowance", err)
			}
			fixture.root.storage.Host.SetReserve(1024*1024*1024, 1024*1024)
			now = now.Add(delay)
			return true
		},
		open: func(ctx context.Context, p rootServiceRuntimePreparation, create bool) (*rootServiceRuntime, error) {
			opens++
			return openRootServiceRuntime(ctx, p, create)
		},
	})
	if err != nil || waits != 1 || opens != 1 || result.RecoveryOpenAttempts != 1 || result.Observations != 1 || len(fixture.submission.sent()) != 0 {
		t.Fatal("counter pressure did not resume bounded original owner", waits, opens, result, err)
	}
}

// The real application flock uses the same pending class as the volume lease.
// A competing owner joins at the signed cadence before any counter is spent.
func TestRootServiceRecoveryWaitsForPhysicalOwner(t *testing.T) {
	fixture, preparation, approval, now := newRootServiceRecoveryFixture(t)
	fixture.weights(t)
	ctx := fixture.root.storage.Context
	if err := prepareRootServiceRecovery(ctx, preparation, approval, now); err != nil {
		t.Fatal(err)
	}
	prior, err := openRootServiceStore(preparation.Root.Service, false, ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prior.close() })
	path := preparation.Root.Service.Packet.Action.Scope.StatePath
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opens, waits := 0, 0
	result, err := runRootServiceRecovery(ctx, preparation, approval, nil, rootServiceRecoveryPorts{
		now: func() time.Time { return now },
		wait: func(ctx context.Context, delay time.Duration) bool {
			waits++
			if delay != time.Minute || opens != 0 || ctx.Err() != nil {
				t.Fatal("contention changed cadence or spent an open", delay, opens, ctx.Err())
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("contention changed the retained counter", err)
			}
			if err := prior.close(); err != nil {
				t.Fatal(err)
			}
			now = now.Add(delay)
			return true
		},
		open: func(ctx context.Context, p rootServiceRuntimePreparation, create bool) (*rootServiceRuntime, error) {
			opens++
			return openRootServiceRuntime(ctx, p, create)
		},
	})
	if err != nil || waits != 1 || opens != 1 || result.RecoveryOpenAttempts != 1 || result.Observations != 1 || len(fixture.submission.sent()) != 0 {
		t.Fatal("physical contention did not preserve bounded original recovery", waits, opens, result, err)
	}
}
