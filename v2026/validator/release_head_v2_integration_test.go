//go:build linux || darwin

package validator

// Composition controls retain real bounded startup, signed head replay and
// durable EMA writes as distinct authorities. None activates a live runtime.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// TempDir is a scratch parent, not private state authority. Provision one
// physical owner-only child; reloads must reuse that child without repairing it.
func newReleaseHeadV2TestStateDir(t *testing.T) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(parent, "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

// Only fixture provisioning resolves platform aliases. Loading an existing
// namespace never creates a child, repairs permissions or changes its limits.
func newReleaseHeadV2EMAStore(t *testing.T, stateDir string) (*HeadEMAStore, error) {
	t.Helper()
	physical, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return nil, err
	}
	return NewHeadEMAStoreV2(t.Context(), physical, HeadEMAStoreV2Limits{
		MaxFileBytes:    maxHeadEMAStoreV2Bytes,
		MaxEntries:      65536,
		MaxControlBytes: maxHeadEMAStoreV2Bytes,
	})
}

// Independent logical owner accounting keeps the original head controls at
// their arithmetic boundaries after the genuine runtime owner is composed.
func releaseHeadV2TestOwnerControlBytes(store *HeadEMAStore) uint64 {
	fixed := uint64(reflect.TypeFor[headEMAFile]().Size()) +
		2*uint64(reflect.TypeFor[HeadEMAStore]().Size()) +
		uint64(reflect.TypeFor[headEMAStoreV2Owner]().Size()) +
		uint64(reflect.TypeFor[releaseHeadEMAOwnerV2]().Size()) +
		uint64(reflect.TypeFor[releaseHeadV2Budget]().Size()) +
		2*(uint64(reflect.TypeFor[attemptPrivateDirectory]().Size())+uint64(reflect.TypeFor[os.File]().Size())+uint64(reflect.TypeFor[attemptPrivateFileState]().Size()))
	if store.lastSubnetEpoch != nil {
		fixed += uint64(reflect.TypeFor[uint64]().Size())
	}
	if store.lastAlpha != nil {
		fixed += uint64(reflect.TypeFor[protocol.Rational]().Size())
	}
	return fixed + uint64(len(store.path))*(12+3*uint64(reflect.TypeFor[string]().Size()))
}

// Constructor selection is authority, not a filename convention. The same
// genuine M8 fixture succeeds only after restoring its actually loaded owner.
func TestReleaseHeadV2IntegrationRequiresBoundedOwnerBeforeCallbacks(t *testing.T) {
	t.Parallel()
	stateDir := newReleaseHeadV2TestStateDir(t)
	parent := filepath.Dir(stateDir)
	for _, mode := range []os.FileMode{0o750, 0o755} {
		if err := os.Chmod(parent, mode); err != nil {
			t.Fatal(err)
		}
		refused, err := newReleaseHeadV2EMAStore(t, parent)
		if err == nil || refused != nil {
			t.Fatalf("non-private fixture parent %o acquired an EMA owner: %v", mode, err)
		}
		info, err := os.Stat(parent)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("refused fixture parent %o was repaired: %v", mode, err)
		}
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 1 || entries[0].Name() != "state" {
			t.Fatalf("refused fixture parent gained state: %v", err)
		}
		loaded, err := newReleaseHeadV2EMAStore(t, stateDir)
		if err != nil || loaded == nil || loaded.path != filepath.Join(stateDir, "head-ema.json") || loaded.v2 == nil {
			t.Fatalf("private child lost exact bounded reload: %v", err)
		}
		info, err = os.Stat(stateDir)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("fixture did not provision a private child: %v", err)
		}
		entries, err = os.ReadDir(stateDir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("empty child admission provisioned durable state: %v", err)
		}
	}
	fixture := newReleaseHeadV2TestFixture(t, 2)
	owned := fixture.steerer.headEMA
	legacy, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture.steerer.headEMA = legacy
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, _ := fixture.rpc.counts()
	if err == nil || *reads != 0 || requests != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("legacy store acquired compact work: requests=%d reads=%d error=%v", requests, *reads, err)
	}
	fixture.steerer.headEMA = owned
	options = fixture.options(t)
	reads = observeReleaseMeasurementV2SettlementTest(&options)
	result, err = fixture.gather(t.Context(), options)
	if err != nil || *reads == 0 || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) {
		t.Fatalf("actual bounded owner lost real replay: reads=%d error=%v", *reads, err)
	}
	fixture.assertNoEMACommit(t)
}

// Each operand allowance fits separately; their simultaneous owners do not.
// Neither a large caller cap nor a large loader cap may reset the other cap.
func TestReleaseHeadV2IntegrationCombinesBothExplicitControlCaps(t *testing.T) {
	t.Parallel()
	for _, constrained := range []string{"loader", "collector"} {
		fixture := newReleaseHeadV2TestFixture(t, 2)
		store := fixture.steerer.headEMA
		fixed := releaseHeadV2TestOwnerControlBytes(store)
		options := fixture.options(t)
		combined := releaseHeadV2AccountingCollectionBudget(t, fixture, options)
		limit := combined - 1
		if fixed >= limit || combined-fixed >= limit {
			t.Fatal("combined control witness does not fit each separate operand")
		}
		limits := store.v2.limits
		if constrained == "loader" {
			limits.MaxControlBytes = limit
		} else {
			options.MaxControlBytes = limit
		}
		reloaded, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), limits)
		if err != nil {
			t.Fatal(err)
		}
		fixture.steerer.headEMA = reloaded
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		result, err := fixture.gather(t.Context(), options)
		requests, _ := fixture.rpc.counts()
		if err == nil || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) || reloaded.v2.active.Load() {
			t.Fatalf("%s control limit acquired callbacks or leaked owner: requests=%d reads=%d error=%v", constrained, requests, *reads, err)
		}
		fixture.assertNoEMACommit(t)
	}
}

// A real prior fold fits the loader census; the independently observed live
// fleet would exceed its union even though the collector's census is larger.
func TestReleaseHeadV2IntegrationRetainsLoaderUnionCapBeforeProofs(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	before := releaseHeadV2AccountingCommit(t, fixture, fixture.measurement.artifact.SubnetEpoch-1, releaseHeadV2AccountingRaw(1), fixture.steerer.cfg.Policy.Steering.HeadScoreEMA)
	store := fixture.steerer.headEMA
	limits := store.v2.limits
	limits.MaxEntries = 1
	reloaded, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), limits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.steerer.headEMA = reloaded
	options := fixture.options(t)
	if options.MaxHeadEntries <= limits.MaxEntries {
		t.Fatal("collector census does not distinguish the retained cap")
	}
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, batches := fixture.rpc.bindingCounts()
	if err == nil || requests == 0 || batches != 2 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("retained union cap reached proof work: requests=%d batches=%d reads=%d error=%v", requests, batches, *reads, err)
	}
	after, readErr := os.ReadFile(store.path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("union refusal changed real persisted state: %v", readErr)
	}
}

// Real synchronous key/proof callbacks cannot commit or preview reentrantly.
// TryLock witnesses that refusal is the atomic operation owner, not EMA.mu.
func TestReleaseHeadV2IntegrationRejectsReentrantRuntimeOperations(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"client key", "proof"} {
		fixture := newReleaseHeadV2TestFixture(t, 2)
		store := fixture.steerer.headEMA
		options := fixture.options(t)
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		observed := false
		check := func() error {
			if observed {
				return nil
			}
			observed = true
			if !store.mu.TryLock() {
				return errors.New("head callback retained EMA state mutex")
			}
			store.mu.Unlock()
			epoch, alpha := fixture.measurement.artifact.SubnetEpoch, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA
			if err := store.CommitForEpochV2(t.Context(), epoch, fixture.measurement.artifact.HeadEMA, alpha); !errors.Is(err, errHeadEMAStoreV2Busy) {
				return errors.Join(errors.New("reentrant commit did not refuse its held owner"), err)
			}
			out, records, err := store.PreviewForEpochV2(t.Context(), epoch, releaseHeadV2AccountingRaw(1), alpha)
			if !errors.Is(err, errHeadEMAStoreV2Busy) || out != nil || records != nil {
				return errors.Join(errors.New("reentrant preview returned work"), err)
			}
			return nil
		}
		if phase == "client key" {
			lookup := fixture.steerer.contexts[9].ClientKey
			fixture.steerer.contexts[9].ClientKey = func(id connect.Id) ([32]byte, bool, error) {
				if err := check(); err != nil {
					return [32]byte{}, false, err
				}
				return lookup(id)
			}
		} else {
			operator := options.Operators[9]
			read := operator.Measurement.Replay.ReadMetadata
			operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
				if err := check(); err != nil {
					return nil, err
				}
				return read(ctx, hash, size)
			}
			options.Operators[9] = operator
		}
		result, err := fixture.gather(t.Context(), options)
		if err != nil || !observed || *reads == 0 || store.v2.active.Load() || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) {
			t.Fatalf("%s owner composition changed full replay: observed=%t reads=%d error=%v", phase, observed, *reads, err)
		}
		fixture.assertNoEMACommit(t)
	}
}

// A channel barrier is entered by the real first key callback. The competing
// public commit must return while collection is still deliberately suspended.
func TestReleaseHeadV2IntegrationConcurrentCommitRefusesHeldCollector(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	store := fixture.steerer.headEMA
	options := fixture.options(t)
	entered, release := make(chan struct{}), make(chan struct{})
	type outcome struct {
		result releaseHeadResult
		err    error
	}
	done := make(chan outcome, 1)
	lookup := fixture.steerer.contexts[9].ClientKey
	first := true
	fixture.steerer.contexts[9].ClientKey = func(id connect.Id) ([32]byte, bool, error) {
		if first {
			first = false
			close(entered)
			select {
			case <-release:
			case <-t.Context().Done():
				return [32]byte{}, false, t.Context().Err()
			}
		}
		return lookup(id)
	}
	go func() { result, err := fixture.gather(t.Context(), options); done <- outcome{result: result, err: err} }()
	select {
	case <-entered:
	case result := <-done:
		close(release)
		t.Fatalf("real callback barrier was not reached: %v", result.err)
	}
	unlocked := store.mu.TryLock()
	if unlocked {
		store.mu.Unlock()
	}
	commitErr := store.CommitForEpochV2(t.Context(), fixture.measurement.artifact.SubnetEpoch, fixture.measurement.artifact.HeadEMA, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA)
	close(release)
	completed := <-done
	if !unlocked || !errors.Is(commitErr, errHeadEMAStoreV2Busy) || completed.err != nil || !reflect.DeepEqual(completed.result.Weights, fixture.measurement.want.SelectedHead) {
		t.Fatalf("concurrent owner boundary differs: stateUnlocked=%t commit=%v gather=%v", unlocked, commitErr, completed.err)
	}
	fixture.assertNoEMACommit(t)
}

// The reader's genuine Close happens first. The callback may observe that
// boundary but cannot replace signed bytes, crypto, a syscall or a verdict.
type releaseHeadV2IntegrationCloseReader struct {
	io.ReadCloser
	afterClose func() error
}

// Keep the real resource-close cause alongside the observer's later cause.
func (self *releaseHeadV2IntegrationCloseReader) Close() error {
	return errors.Join(self.ReadCloser.Close(), self.afterClose())
}

// A second genuine durable owner changes the originally absent leaf after
// real proof Close. Even completed proof replay cannot authorize that new EMA.
func TestReleaseHeadV2IntegrationFinalProofCloseRechecksDurableOwner(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	store := fixture.steerer.headEMA
	writer, err := newReleaseHeadV2EMAStore(t, filepath.Dir(store.path))
	if err != nil {
		t.Fatal(err)
	}
	epoch, alpha := fixture.measurement.artifact.SubnetEpoch-1, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA
	_, records, err := writer.PreviewForEpochV2(t.Context(), epoch, releaseHeadV2AccountingRaw(1), alpha)
	if err != nil {
		t.Fatal(err)
	}
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	operator := options.Operators[10]
	open := operator.Measurement.Replay.OpenData
	closes := 0
	operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &releaseHeadV2IntegrationCloseReader{ReadCloser: reader, afterClose: func() error {
			closes++
			if closes == 1 {
				return writer.CommitForEpochV2(ctx, epoch, records, alpha)
			}
			return nil
		}}, nil
	}
	options.Operators[10] = operator
	result, err := fixture.gather(t.Context(), options)
	if err == nil || closes == 0 || *reads == 0 || !reflect.DeepEqual(result, releaseHeadResult{}) || len(store.values) != 0 || store.lastSubnetEpoch != nil || store.v2.active.Load() {
		t.Fatalf("final proof close bypassed original EMA authority: closes=%d reads=%d error=%v", closes, *reads, err)
	}
	reloaded, err := newReleaseHeadV2EMAStore(t, filepath.Dir(store.path))
	if err != nil || reloaded.lastSubnetEpoch == nil || *reloaded.lastSubnetEpoch != epoch || !equalHeadEMAFolds(reloaded.lastFold, records) {
		t.Fatalf("independent writer's genuine persisted witness is absent: %v", err)
	}
}

// Independent fixture bindings and decision pins locate retained source
// bytes, never a candidate-provided hash. Actual historical authority and
// signed request verification precede the expected observation-byte digest.
func (self *releaseHeadV2TestFixture) retainedOptions(t *testing.T) (options ReleaseMeasurementV2Options, resultErr error) {
	t.Helper()
	options = self.measurement.options(t)
	custody := &releaseEvidenceV2StartupReferences{remaining: self.steerer.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	defer func() {
		resultErr = errors.Join(resultErr, custody.check(), custody.close(), t.Context().Err())
		if resultErr != nil {
			options = ReleaseMeasurementV2Options{}
		}
	}()
	for index := range options.Bindings {
		binding := &options.Bindings[index]
		if !binding.Active {
			continue
		}
		clientId, err := connect.ParseId(binding.ClientID)
		if err != nil {
			return options, err
		}
		domain, request, err := releaseClientKeyDecisionV2(self.steerer.cfg, binding.NoID, self.steerer.hotkey.PublicKey(), self.measurement.artifact, clientId)
		if err != nil {
			return options, err
		}
		path, err := releaseClientKeyCaptureV2Path(self.steerer.cfg.StateDir, domain, request)
		if err != nil {
			return options, err
		}
		encoded, err := custody.read(t.Context(), path, releaseClientKeyHistoryTestResponseBytes, false)
		if err != nil {
			return options, err
		}
		registration, err := verifyReleaseClientKeyCaptureV2(t.Context(), self.steerer.chain, encoded, releaseClientKeyHistoryTestResponseBytes, domain, request, true)
		if err != nil || !registration.Present || releaseHex32(registration.PublicKey) != binding.LocalClientKey {
			return options, errors.Join(errors.New("retained signed observation differs from independent fixture key and decision"), err)
		}
		binding.ClientKeyObservationHash = ReleaseMeasurementContentHash(encoded)
	}
	return options, nil
}

// A complete positive-quality M8 collection is sealed and decoded through
// fresh full replay, then explicitly committed and collected again after load.
// Preview never acquires intent/on-chain publication authority on its own.
func TestReleaseHeadV2IntegrationPersistsRealPreviewAndReplaysCleanRestart(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 15)
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	if err != nil || *reads == 0 {
		t.Fatalf("genuine collection failed: reads=%d error=%v", *reads, err)
	}
	fixture.assertNoEMACommit(t)
	artifact := cloneReleaseMeasurementArtifact(t, fixture.measurement.artifact)
	artifact.Inputs, artifact.Bindings, artifact.HeadEMA = result.Inputs, result.Bindings, result.HeadEMA
	for _, owner := range fixture.steerer.contexts {
		owner.ClientKeyHistory.byJwt = func() string { return "" }
	}
	sealOptions, err := fixture.retainedOptions(t)
	if err != nil || !reflect.DeepEqual(sealOptions.Bindings, result.Bindings) {
		t.Fatalf("independently retained signed head bindings differ: %v", err)
	}
	encoded, hash, err := SealReleaseMeasurementArtifactV2(t.Context(), artifact, sealOptions)
	if err != nil || hash != ReleaseMeasurementContentHash(encoded) {
		t.Fatalf("full real wire seal failed: %v", err)
	}
	decodeOptions, err := fixture.retainedOptions(t)
	if err != nil {
		t.Fatal(err)
	}
	decoded, verified, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, decodeOptions)
	if err != nil || decoded == nil || verified.Decision == nil || !reflect.DeepEqual(verified.Decision.SelectedHead, result.Weights) || len(verified.ReplayByNO) != 2 {
		t.Fatalf("fresh complete wire replay differs: %v", err)
	}
	store := fixture.steerer.headEMA
	epoch, alpha := artifact.SubnetEpoch, artifact.Policy.Steering.HeadScoreEMA
	if err := store.CommitForEpochV2(t.Context(), epoch, result.HeadEMA, alpha); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), store.v2.limits)
	if err != nil || reloaded.v2 == nil {
		t.Fatalf("bounded clean restart failed: %v", err)
	}
	fixture.steerer.headEMA = reloaded
	options = fixture.options(t)
	reads = observeReleaseMeasurementV2SettlementTest(&options)
	again, err := fixture.gather(t.Context(), options)
	if err != nil || *reads == 0 || !reflect.DeepEqual(again.HeadEMA, result.HeadEMA) || !reflect.DeepEqual(again.Weights, result.Weights) {
		t.Fatalf("same-epoch restarted collection changed genuine head: reads=%d error=%v", *reads, err)
	}
	if err := reloaded.CommitForEpochV2(t.Context(), epoch, again.HeadEMA, alpha); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("idempotent commit rewrote persisted fold: %v", err)
	}
}

// The actual admission equality remains mandatory: neither omitting a live
// source hash nor substituting a valid different digest can start M8 replay.
func TestReleaseHeadV2IntegrationRejectsChangedObservedBindingHash(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, contentHash := range []string{"", ReleaseMeasurementContentHash([]byte("not the observed signed response"))} {
		artifact := cloneReleaseMeasurementArtifact(t, fixture.measurement.artifact)
		artifact.Inputs, artifact.Bindings, artifact.HeadEMA = result.Inputs, append([]ReleaseBindingMeasurement(nil), result.Bindings...), result.HeadEMA
		options, err := fixture.retainedOptions(t)
		if err != nil || len(artifact.Bindings) == 0 || !reflect.DeepEqual(options.Bindings, artifact.Bindings) {
			t.Fatalf("retained signed negative-control prerequisite differs: %v", err)
		}
		bindingIndex := -1
		for index, binding := range options.Bindings {
			if binding.Active && binding.ClientKeyObservationHash != "" {
				bindingIndex = index
				break
			}
		}
		if bindingIndex < 0 || artifact.Bindings[bindingIndex].ClientKeyObservationHash == contentHash {
			t.Fatal("negative control has no actual authenticated observation to change")
		}
		artifact.Bindings[bindingIndex].ClientKeyObservationHash = contentHash
		if reflect.DeepEqual(options.Bindings, artifact.Bindings) {
			t.Fatal("negative control did not change its independently authenticated slot")
		}
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		encoded, hash, err := SealReleaseMeasurementArtifactV2(t.Context(), artifact, options)
		if err == nil || !strings.Contains(err.Error(), "independently authenticated observations") || encoded != nil || hash != "" || *reads != 0 {
			t.Fatalf("changed live observation hash acquired measurement authority: reads=%d error=%v", *reads, err)
		}
	}
	fixture.assertNoEMACommit(t)
}

// Even another genuine operator-signed capture cannot fill the independent
// client's slot. Restoring exact original bytes, not a new signature, recovers.
func TestReleaseHeadV2IntegrationRetainedOracleRejectsOtherSignedClient(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, binding := range fixture.measurement.artifact.Bindings {
		if !binding.Active {
			continue
		}
		clientId, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientId)
		if err != nil {
			t.Fatal(err)
		}
		path, err := releaseClientKeyCaptureV2Path(fixture.steerer.cfg.StateDir, domain, request)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		if len(paths) == 2 {
			break
		}
	}
	if len(paths) != 2 || paths[0] == paths[1] {
		t.Fatal("actual signed source substitution needs two independent client slots")
	}
	original, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(paths[1])
	if err != nil || bytes.Equal(original, other) {
		t.Fatalf("actual distinct client response prerequisite differs: %v", err)
	}
	for _, owner := range fixture.steerer.contexts {
		owner.ClientKeyHistory.byJwt = func() string { return "" }
	}
	if err := os.WriteFile(paths[0], other, 0o600); err != nil {
		t.Fatal(err)
	}
	if options, err := fixture.retainedOptions(t); err == nil || !reflect.DeepEqual(options, ReleaseMeasurementV2Options{}) {
		t.Fatalf("another valid signed client response became fixture observation authority: %v", err)
	}
	if err := os.WriteFile(paths[0], original, 0o600); err != nil {
		t.Fatal(err)
	}
	options, err := fixture.retainedOptions(t)
	if err != nil || !reflect.DeepEqual(options.Bindings, result.Bindings) {
		t.Fatalf("restoring original signed bytes lost full binding equality: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

// Every observed native phase sees the old in-memory epoch, including after
// actual Sync and Close. Only the fully witnessed return publishes new state.
func TestReleaseHeadV2IntegrationCommitPublishesAfterActualSyncAndClose(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	store := fixture.steerer.headEMA
	steps := map[string]bool{}
	closes := 0
	check := func() error {
		if !store.mu.TryLock() {
			return errors.New("native callback retained EMA state mutex")
		}
		defer store.mu.Unlock()
		if len(store.values) != 0 || store.lastSubnetEpoch != nil {
			return errors.New("speculative EMA state was published before final persistence")
		}
		return nil
	}
	hooks := headEMAStoreV2RuntimeHooks{
		step: func(step string, _ *os.File) error { steps[step] = true; return check() },
		afterClose: func(file *os.File) error {
			closes++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.Join(errors.New("close hook preceded actual Close"), err)
			}
			return check()
		},
	}
	epoch, alpha := fixture.measurement.artifact.SubnetEpoch, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA
	_, _, err = store.runHeadEMAStoreV2(t.Context(), headEMAStoreV2Commit, epoch, nil, result.HeadEMA, alpha, hooks)
	if err != nil || closes == 0 || !steps["candidate-synced"] || !steps["published"] || !steps["directory-synced"] || store.lastSubnetEpoch == nil || *store.lastSubnetEpoch != epoch || !equalHeadEMAFolds(store.lastFold, result.HeadEMA) {
		t.Fatalf("durable publication order differs: steps=%v closes=%d error=%v", steps, closes, err)
	}
	reloaded, err := newReleaseHeadV2EMAStore(t, filepath.Dir(store.path))
	if err != nil || !equalHeadEMAFolds(reloaded.lastFold, result.HeadEMA) {
		t.Fatalf("published memory lacks real persisted counterpart: %v", err)
	}
}

// Cancellation and an observer-injected failure after actual candidate Close
// preserve both causes and the marker. This is not an OS-close-failure claim.
func TestReleaseHeadV2IntegrationLateWriteCancellationFaultsSharedOwner(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	store := fixture.steerer.headEMA
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failure := errors.New("test-owned error after real candidate Close")
	observed := false
	hooks := headEMAStoreV2RuntimeHooks{afterClose: func(file *os.File) error {
		if filepath.Base(file.Name()) != headEMAStoreV2Candidate {
			return nil
		}
		observed = true
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			return errors.Join(errors.New("candidate was not actually closed"), err)
		}
		cancel()
		return failure
	}}
	out, records, err := store.runHeadEMAStoreV2(ctx, headEMAStoreV2Commit, fixture.measurement.artifact.SubnetEpoch, nil, result.HeadEMA, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA, hooks)
	if !observed || !errors.Is(err, failure) || !errors.Is(err, context.Canceled) || out != nil || records != nil || store.v2.fault == nil || store.lastSubnetEpoch != nil {
		t.Fatalf("late write failure lost cause, fault or atomicity: observed=%t error=%v", observed, err)
	}
	marker := filepath.Join(filepath.Dir(store.path), headEMAStoreV2Marker)
	if info, err := os.Lstat(marker); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("uncertain write evidence missing: %v", err)
	}
	requestsBefore, _ := fixture.rpc.counts()
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	refused, err := fixture.gather(t.Context(), options)
	requestsAfter, _ := fixture.rpc.counts()
	if err == nil || requestsAfter != requestsBefore || *reads != 0 || !reflect.DeepEqual(refused, releaseHeadResult{}) {
		t.Fatalf("faulted durable owner continued real collection: reads=%d error=%v", *reads, err)
	}
	reloaded, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), store.v2.limits)
	if err == nil || reloaded != nil {
		t.Fatal("unresolved marker was treated as clean restart recovery")
	}
}

// This exact plan boundary uses genuine persisted folds and an explicitly
// retained input buffer. It is a control-budget contract, not extra M8 work.
func TestReleaseHeadV2IntegrationExactCombinedPreviewBudget(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	bounds := map[FleetScoreKey]releaseHeadV2FractionBound{}
	for key, value := range fixture.raw {
		bounds[key] = releaseHeadV2FractionBound{numerator: uint64(value.Num().BitLen()), denominator: uint64(value.Denom().BitLen())}
	}
	initial := releaseHeadV2Budget{limit: store.v2.limits.MaxControlBytes, used: uint64(len(fixture.encoded))}
	plan, err := func() (releaseHeadV2Budget, error) {
		store.mu.Lock()
		defer store.mu.Unlock()
		plan, err := store.admitReleaseHeadV2KnownWithLock(t.Context(), 10, fixture.alpha, 2, initial)
		if err != nil {
			return plan, err
		}
		plan, err = admitReleaseHeadV2CurrentWithLock(t.Context(), store, 10, fixture.raw, 2, plan)
		if err != nil {
			return plan, err
		}
		return store.planReleaseHeadV2PreviewWithLock(t.Context(), 10, bounds, fixture.alpha, plan)
	}()
	if err != nil || plan.used <= initial.used {
		t.Fatalf("exact combined plan missing: %v", err)
	}
	for _, limit := range []uint64{plan.used - 1, plan.used} {
		out, records, err := store.previewForEpochV2WithBudget(t.Context(), 10, fixture.raw, fixture.alpha, 2, releaseHeadV2Budget{limit: limit, used: initial.used})
		if limit < plan.used {
			if err == nil || out != nil || records != nil {
				t.Fatalf("one-short combined budget produced output: %v", err)
			}
		} else if err != nil || !equalHeadEMAFolds(records, fixture.records) || len(out) != 2 {
			t.Fatalf("exact combined budget changed persisted fold: %v", err)
		}
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(after, fixture.encoded) {
		t.Fatalf("exact preview boundary changed persistence: %v", err)
	}
}

// Context is checked before the real collection callback tree or atomic
// owner acquisition; a subsequent live call uses fresh full replay scratch.
func TestReleaseHeadV2IntegrationCanceledAdmissionLeavesOwnerUsable(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(ctx, options)
	requests, _ := fixture.rpc.counts()
	if !errors.Is(err, context.Canceled) || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) || fixture.steerer.headEMA.v2.active.Load() {
		t.Fatalf("canceled admission acquired work or retained owner: requests=%d reads=%d error=%v", requests, *reads, err)
	}
	result, err = fixture.gather(t.Context(), fixture.options(t))
	if err != nil || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) {
		t.Fatalf("canceled admission poisoned fresh replay: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

// Scratch and key owners are admitted before common wire copies. An exact
// valid key-map allowance reaches binding RPC; one byte less does not. Fresh
// full replay with that same key map supplies the independent positive control.
func TestReleaseHeadV2IntegrationBoundsReplayOptionsBeforeOwnershipCopy(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	for _, field := range []string{"scratch", "key width"} {
		options := fixture.options(t)
		options.MaxControlBytes = 32 * 1024
		operator := options.Operators[10]
		if field == "scratch" {
			operator.Measurement.Replay.ScratchDirectory = "/" + strings.Repeat("bounded-unused-path", 4096)
		} else {
			operator.Measurement.Replay.ServerKeys[255] = make(ed25519.PublicKey, ed25519.PublicKeySize+1)
		}
		options.Operators[10] = operator
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		result, err := fixture.gather(t.Context(), options)
		requests, _ := fixture.rpc.counts()
		if err == nil || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
			t.Fatalf("%s option copy escaped combined admission: requests=%d reads=%d error=%v", field, requests, *reads, err)
		}
	}
	for _, phase := range []string{"one short", "exact input", "full replay"} {
		options := fixture.options(t)
		inputBytes := releaseHeadV2AccountingCollectionBudget(t, fixture, options)
		operator := options.Operators[10]
		keys := operator.Measurement.Replay.ServerKeys
		var publicKey ed25519.PublicKey
		for _, key := range keys {
			publicKey = key
			break
		}
		if len(publicKey) != ed25519.PublicKeySize || len(keys) == 256 {
			t.Fatal("fixture lacks a genuine expandable replay key map")
		}
		added := uint64(256 - len(keys))
		for index := 0; index < 256; index++ {
			keyID := byte(index)
			if _, exists := keys[keyID]; !exists {
				keys[keyID] = publicKey
			}
		}
		options.Operators[10] = operator
		// byte map key, owned slice header and exact ed25519 key payload.
		additional := added * (1 + uint64(reflect.TypeFor[ed25519.PublicKey]().Size()) + ed25519.PublicKeySize)
		if phase != "full replay" {
			options.MaxControlBytes = inputBytes + additional
			if phase == "one short" {
				options.MaxControlBytes--
			}
		}
		requestsBefore, _ := fixture.rpc.counts()
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		result, err := fixture.gather(t.Context(), options)
		requestsAfter, _ := fixture.rpc.counts()
		if phase == "full replay" {
			if err != nil || *reads == 0 || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) {
				t.Fatalf("valid owned key map changed genuine replay: reads=%d error=%v", *reads, err)
			}
		} else if err == nil || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) ||
			phase == "one short" && requestsAfter != requestsBefore || phase == "exact input" && requestsAfter == requestsBefore {
			t.Fatalf("%s option-owner boundary differs: requests=%d reads=%d error=%v", phase, requestsAfter-requestsBefore, *reads, err)
		}
	}
	fixture.assertNoEMACommit(t)
}
