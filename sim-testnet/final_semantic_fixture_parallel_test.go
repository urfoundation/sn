// Parallel fixture controls use the independently captured serial wire census.
// Reference origin: Jx7hRa index 2d90b5afd279b55a664c16c99dacc129059229b047a0e5c423fb835bfed34a06.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/ss58"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// The first true preparation batch must overlap inside each measured stage.
func TestFinalSemanticFixtureRealPreparationStagesOverlap(t *testing.T) {
	t.Parallel()
	_, _ = finalSemanticFixture(t)
	finalSemanticFixtureCache.stateLock.Lock()
	stages := make(map[string]finalSemanticFixtureStageObservation, len(finalSemanticFixtureCache.workStages))
	for stage, value := range finalSemanticFixtureCache.workStages {
		stages[stage] = value
	}
	finalSemanticFixtureCache.stateLock.Unlock()
	if len(stages) != 4 {
		t.Fatalf("real preparation stages=%d, want 4", len(stages))
	}
	for _, want := range []struct {
		stage        string
		count, width int
	}{
		{stage: finalSemanticFixtureValidatorLanes, count: 2, width: 2},
		{stage: finalSemanticFixtureTerminalClosures, count: 10, width: 4},
		{stage: finalSemanticFixtureGenerationCalldata, count: 20, width: 4},
		{stage: finalSemanticFixtureFleetAssembly, count: 202, width: 4},
	} {
		got := stages[want.stage]
		if got.entered != want.count || got.completed != want.count || got.active != 0 || got.maximum != want.width {
			t.Errorf("real preparation %s=%+v, want entered/completed=%d and overlap=%d", want.stage, got, want.count, want.width)
		}
		t.Logf("real preparation %s entered=%d joined=%d maximum=%d", want.stage, got.entered, got.completed, got.maximum)
	}
	// Counts are detached; a reader never receives the cache's map.
	stages[finalSemanticFixtureValidatorLanes] = finalSemanticFixtureStageObservation{}
	finalSemanticFixtureCache.stateLock.Lock()
	unchanged := finalSemanticFixtureCache.workStages[finalSemanticFixtureValidatorLanes]
	finalSemanticFixtureCache.stateLock.Unlock()
	if unchanged.entered != 2 {
		t.Fatal("stage reader changed the cold cache")
	}
}

// Every deterministic byte must retain the independent serial identity, length
// and digest. Randomized sr25519 envelope and intent bytes are not normalized.
func TestFinalSemanticFixtureParallelPreservesSerialWireGoldens(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	if source.DeploymentID != "ur-subnet-testnet-v1-attempt-4" || source.ChainID != 945 || source.GenesisHash != finalTestHex(5) || source.Netuid != 521 || source.PolicyHash != "0x872a3bf9f3ecf0a5eb28cd2ac63d7da07ef0f8d1d58a12d4975afd0b2b019b33" {
		t.Fatal("parallel fixture identity differs from independent serial reference")
	}
	type wireReference struct {
		kind              string
		validatorID       uint64
		uid               uint16
		noID, first, last uint64
		uri               string
		size              uint64
		hash              string
	}
	expected := []wireReference{
		{kind: "validator-path-proofs", validatorID: 1, uid: 12, noID: 1, first: 10, last: 14, uri: "final-derived/path-proofs-1-1.jsonl", size: 256769, hash: "sha256:d2c8351436687520682456599a51dc74da79864d26cadf5303fd1c81b861f4fb"},
		{kind: "validator-path-proofs", validatorID: 1, uid: 12, noID: 2, first: 10, last: 14, uri: "final-derived/path-proofs-1-2.jsonl", size: 256802, hash: "sha256:772f4b655af3426ac15d9889e17d7df7aba11349c908aa9accc9f272ca0df5f9"},
		{kind: "validator-path-proofs", validatorID: 2, uid: 14, noID: 1, first: 10, last: 14, uri: "final-derived/path-proofs-2-1.jsonl", size: 256773, hash: "sha256:5183c0803ae236592aa15d9d5c508a2a4e9a1745d81aee23ca53dcaaccc2eaa9"},
		{kind: "validator-path-proofs", validatorID: 2, uid: 14, noID: 2, first: 10, last: 14, uri: "final-derived/path-proofs-2-2.jsonl", size: 256798, hash: "sha256:4e8d5cf4370f6a10f1fd8815aae91280ec875730623bd53ec611660cdd27c06b"},
		{kind: "validator-settlement-closure", validatorID: 1, uid: 12, noID: 0, first: 10, last: 10, uri: "final-derived/settlement-closure-1-10.json", size: 1218959, hash: "sha256:3f8a4a76d86e7a75b518d0ec70d1018b1d46f68f04a3ea1e1f14e8237e542794"},
		{kind: "validator-settlement-closure", validatorID: 1, uid: 12, noID: 0, first: 11, last: 11, uri: "final-derived/settlement-closure-1-11.json", size: 1219058, hash: "sha256:5cd480c0c365cca2d7af4651774551a4ef7a4aaf93172346e409318b66c58211"},
		{kind: "validator-settlement-closure", validatorID: 1, uid: 12, noID: 0, first: 12, last: 12, uri: "final-derived/settlement-closure-1-12.json", size: 1219196, hash: "sha256:2250d76713dba3d4d9b3559d9e09723cb86779415e6d95eb4d85a22f9a8a7e85"},
		{kind: "validator-settlement-closure", validatorID: 1, uid: 12, noID: 0, first: 13, last: 13, uri: "final-derived/settlement-closure-1-13.json", size: 1219430, hash: "sha256:e04171161e884835f0794c4f9c32f44df8af5cf3d9d922ec992b5a74e6556eac"},
		{kind: "validator-settlement-closure", validatorID: 1, uid: 12, noID: 0, first: 14, last: 14, uri: "final-derived/settlement-closure-1-14.json", size: 1219440, hash: "sha256:e5a6988574ad8d938243211171bc4592ce76700ace94f4062cf1b5f4ad69fa8e"},
		{kind: "validator-settlement-closure", validatorID: 2, uid: 14, noID: 0, first: 10, last: 10, uri: "final-derived/settlement-closure-2-10.json", size: 1218959, hash: "sha256:acebf5aa8e5a725a7e1b77285701555026e6c932d6b8eb8d89074d94e031aef9"},
		{kind: "validator-settlement-closure", validatorID: 2, uid: 14, noID: 0, first: 11, last: 11, uri: "final-derived/settlement-closure-2-11.json", size: 1219058, hash: "sha256:6b93487efd751bcecc11307831a5c0adedb413b194143ed8be1e2afd3d274a91"},
		{kind: "validator-settlement-closure", validatorID: 2, uid: 14, noID: 0, first: 12, last: 12, uri: "final-derived/settlement-closure-2-12.json", size: 1219142, hash: "sha256:4eed1d3fe90b9c6d19e0c160e014729ce774135f14feb5e2f85bdb7af2b6d1f5"},
		{kind: "validator-settlement-closure", validatorID: 2, uid: 14, noID: 0, first: 13, last: 13, uri: "final-derived/settlement-closure-2-13.json", size: 1219430, hash: "sha256:9c403d0d1444bae82bd6e23f127310e17a480d1be57e0d744945bbdb4ecbb780"},
		{kind: "validator-settlement-closure", validatorID: 2, uid: 14, noID: 0, first: 14, last: 14, uri: "final-derived/settlement-closure-2-14.json", size: 1219440, hash: "sha256:01c99bc284565d8255ad56276c9272c4c15fc515aa9d9c665459e829fa937344"},
		{kind: "validator-release-measurement", validatorID: 1, uid: 12, noID: 0, first: 10, last: 10, uri: "final-derived/validator-1-measurement-10.json", size: 1861869, hash: "sha256:bad75545d03cc2e183135e5a47c696eeb6c61fe0b4829bbbdb6199d5bdf94fbd"},
		{kind: "validator-release-measurement", validatorID: 1, uid: 12, noID: 0, first: 11, last: 11, uri: "final-derived/validator-1-measurement-11.json", size: 3080798, hash: "sha256:3d285af3fbb0abc5ad9e0ae6c05123758255bd5ff980019985b149f91bdbb1a0"},
		{kind: "validator-release-measurement", validatorID: 1, uid: 12, noID: 0, first: 12, last: 12, uri: "final-derived/validator-1-measurement-12.json", size: 3081035, hash: "sha256:f995fbd40e9e252aa209aa34dd14f8566cb20bdab656b4745cbd0c29c87774ac"},
		{kind: "validator-release-measurement", validatorID: 1, uid: 12, noID: 0, first: 13, last: 13, uri: "final-derived/validator-1-measurement-13.json", size: 3082318, hash: "sha256:12a2629d7afb754423c73a19a9550a567340af91f659dbed07d36d0ed94c97f8"},
		{kind: "validator-release-measurement", validatorID: 1, uid: 12, noID: 0, first: 14, last: 14, uri: "final-derived/validator-1-measurement-14.json", size: 3082574, hash: "sha256:9f290ac94f4f5bd38763466cc8a3f3628c33e4d2ce99170dd2ab5d40d09577e4"},
		{kind: "validator-release-measurement", validatorID: 2, uid: 14, noID: 0, first: 10, last: 10, uri: "final-derived/validator-2-measurement-10.json", size: 1861869, hash: "sha256:e9d580cef9546674ee7e65dbd2d50d4b929596a7bbde94c45da99fe61334cfba"},
		{kind: "validator-release-measurement", validatorID: 2, uid: 14, noID: 0, first: 11, last: 11, uri: "final-derived/validator-2-measurement-11.json", size: 3080798, hash: "sha256:9598b6c927e3f2d74a1969dd59c19f1616380a1618e0c4d9fffe36bf3c64170a"},
		{kind: "validator-release-measurement", validatorID: 2, uid: 14, noID: 0, first: 12, last: 12, uri: "final-derived/validator-2-measurement-12.json", size: 3080981, hash: "sha256:5476cfb8a515486e5de460f599cd45b3d4e372bc168f8bb0f351b10a226f1abf"},
		{kind: "validator-release-measurement", validatorID: 2, uid: 14, noID: 0, first: 13, last: 13, uri: "final-derived/validator-2-measurement-13.json", size: 3082260, hash: "sha256:d938771da7d924514bdb9a3817a71f6148f58856d2b8732add8fbb9d33cc518f"},
		{kind: "validator-release-measurement", validatorID: 2, uid: 14, noID: 0, first: 14, last: 14, uri: "final-derived/validator-2-measurement-14.json", size: 3082564, hash: "sha256:16c53c38a7e09d474a29e8be47825353dec49a6b734011bb359cae1de8940601"},
	}
	actual := map[string]wireReference{}
	add := func(validatorID, noID, first, last uint64, locator FinalArtifactLocator) {
		value := wireReference{kind: locator.Kind, validatorID: validatorID, uid: finalValidatorUID(&source, validatorID), noID: noID, first: first, last: last, uri: locator.URI, size: locator.SizeBytes, hash: locator.ContentHash}
		if prior, exists := actual[locator.URI]; exists && prior != value {
			t.Fatalf("parallel wire %s identity conflicts", locator.URI)
		}
		actual[locator.URI] = value
	}
	for _, validator := range source.Validators {
		for _, cycle := range validator.Cycles {
			add(validator.ValidatorID, 0, cycle.SettlementEpoch, cycle.SettlementEpoch, cycle.MeasurementArtifact)
		}
	}
	for _, proof := range source.PathProofs {
		add(proof.ValidatorID, proof.NoID, proof.FirstEpoch, proof.LastEpoch, proof.Artifact)
		for _, closure := range proof.SettlementClosures {
			add(proof.ValidatorID, 0, closure.Epoch, closure.Epoch, closure.Artifact)
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("parallel deterministic census=%d, want %d", len(actual), len(expected))
	}
	for _, want := range expected {
		data, exists := artifacts[want.uri]
		if !exists || actual[want.uri] != want || uint64(len(data)) != want.size || bytesSHA256(data) != want.hash {
			t.Errorf("parallel deterministic bytes changed: %s got=%+v bytes=%d hash=%s want=%+v", want.uri, actual[want.uri], len(data), bytesSHA256(data), want)
		}
	}
}

// Fresh signatures are fully verified, then the very same sealed outputs are
// assembled in serial and parallel orders without signature/hash normalization.
func TestFinalSemanticFixtureSealedAssemblyPreservesAuthenticatedEnvelopes(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	type sealedSlot struct {
		validatorID uint64
		uid         uint16
		hotkey      [32]byte
		cycle       FinalCRv4Cycle
	}
	var slots []sealedSlot
	for _, validator := range source.Validators {
		hotkey, _, err := ss58.Decode(validator.Hotkey)
		if err != nil {
			t.Fatal(err)
		}
		for _, cycle := range validator.Cycles {
			slots = append(slots, sealedSlot{validatorID: validator.ValidatorID, uid: validator.UID, hotkey: hotkey, cycle: cycle})
		}
	}
	if len(slots) != 10 {
		t.Fatalf("sealed slots=%d, want 10", len(slots))
	}
	serial := &finalSemanticFixtureArtifacts{}
	for _, slot := range slots {
		for _, locator := range []FinalArtifactLocator{slot.cycle.MeasurementEnvelope, slot.cycle.IntentArtifact} {
			if _, err := serial.derive(locator.Kind, locator.URI, artifacts[locator.URI]); err != nil {
				t.Fatal(err)
			}
		}
	}
	owners := make([]*finalSemanticFixtureArtifacts, len(slots))
	work := finalSemanticFixtureWorkControl{ctx: t.Context()}
	if err := work.run("sealed-assembly", len(slots), 4, func(ctx context.Context, index int) error {
		slot := slots[index]
		envelope, err := validatorpkg.DecodeReleaseMeasurementEnvelope(artifacts[slot.cycle.MeasurementEnvelope.URI])
		if err != nil {
			return err
		}
		measurement, _, err := validatorpkg.VerifyReleaseMeasurementEnvelope(envelope, artifacts[slot.cycle.MeasurementArtifact.URI], slot.hotkey, slot.uid, slot.cycle.Commit.ExtrinsicHash)
		if err != nil {
			return err
		}
		if measurement.ValidatorID != slot.validatorID || measurement.SettlementEpoch != slot.cycle.SettlementEpoch || measurement.SubnetEpoch != slot.cycle.SubnetEpoch {
			return errors.New("sealed envelope cycle identity changed")
		}
		var intent validatorpkg.SteeringIntent
		if err := json.Unmarshal(artifacts[slot.cycle.IntentArtifact.URI], &intent); err != nil {
			return err
		}
		if intent.Prepared == nil || intent.Prepared.ExtrinsicHash != slot.cycle.Commit.ExtrinsicHash || intent.Prepared.AccountNonce != 1 || intent.ValidatorID != slot.validatorID || intent.SettlementEpoch != slot.cycle.SettlementEpoch || intent.MeasurementEnvelopeHash != slot.cycle.MeasurementEnvelope.ContentHash || intent.MeasurementArtifactHash != slot.cycle.MeasurementArtifact.ContentHash {
			return errors.New("sealed intent identity, nonce or exact artifact links changed")
		}
		if _, err := intent.Prepared.Validate(); err != nil {
			return err
		}
		vector, err := intent.ReconstructedVectorHash()
		if err != nil || vector != intent.VectorHash {
			return fmt.Errorf("sealed intent vector changed: %v", err)
		}
		owner := &finalSemanticFixtureArtifacts{}
		for _, locator := range []FinalArtifactLocator{slot.cycle.MeasurementEnvelope, slot.cycle.IntentArtifact} {
			if _, err := owner.derive(locator.Kind, locator.URI, artifacts[locator.URI]); err != nil {
				return err
			}
		}
		owners[index] = owner
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	expected, err := joinFinalSemanticFixtureArtifacts(nil, []*finalSemanticFixtureArtifacts{serial})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := joinFinalSemanticFixtureArtifacts(nil, owners)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("parallel assembly changed the same fully authenticated signed outputs")
	}
}

// Admitted owners are bounded and joined even when a later slot fails first.
func TestFinalSemanticFixtureWorkersBoundAndJoinCanonicalFailures(t *testing.T) {
	t.Parallel()
	for _, width := range []int{1, 2, 4} {
		var active, maximum, completed atomic.Int32
		var observations []finalSemanticFixtureWorkBatch
		entered := make(chan struct{}, width)
		release := make(chan struct{})
		firstErr, laterErr := errors.New("canonical-first"), errors.New("completed-earlier")
		returned := make(chan error, 1)
		work := finalSemanticFixtureWorkControl{ctx: t.Context(), observer: func(batch finalSemanticFixtureWorkBatch) { observations = append(observations, batch) }}
		go func() {
			returned <- work.run("bounded", width+1, width, func(_ context.Context, index int) error {
				now := active.Add(1)
				for prior := maximum.Load(); now > prior && !maximum.CompareAndSwap(prior, now); prior = maximum.Load() {
				}
				defer active.Add(-1)
				defer completed.Add(1)
				entered <- struct{}{}
				<-release
				if index == 0 {
					return firstErr
				}
				return laterErr
			})
		}()
		for range width {
			<-entered
		}
		if active.Load() != int32(width) {
			t.Fatalf("real admitted owners=%d want %d", active.Load(), width)
		}
		close(release)
		err := <-returned
		if !errors.Is(err, firstErr) || active.Load() != 0 || completed.Load() != int32(width) || maximum.Load() != int32(width) || len(observations) != 1 || observations[0].count != width {
			t.Fatalf("bounded owner join width=%d err=%v active=%d completed=%d maximum=%d batches=%v", width, err, active.Load(), completed.Load(), maximum.Load(), observations)
		}
	}
}

// Cancellation before admission cannot launch workers or invoke observations.
func TestFinalSemanticFixtureWorkerCancellationBeforeDispatch(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var calls atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: ctx, observer: func(finalSemanticFixtureWorkBatch) { calls.Add(1) }}
	err := work.run("canceled", 10, 4, func(context.Context, int) error { calls.Add(1); return nil })
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("canceled dispatch calls=%d err=%v", calls.Load(), err)
	}
}

// A cancellation observed after all owners entered still waits for every exit
// and cannot admit a second batch or return successful partial results.
func TestFinalSemanticFixtureWorkerCancellationJoinsAdmittedOwners(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var completed atomic.Int32
	var batches int
	work := finalSemanticFixtureWorkControl{ctx: ctx, observer: func(finalSemanticFixtureWorkBatch) { batches++ }}
	returned := make(chan error, 1)
	go func() {
		returned <- work.run("cancel-join", 10, 4, func(context.Context, int) error {
			entered <- struct{}{}
			<-release
			completed.Add(1)
			return nil
		})
	}()
	for range 4 {
		<-entered
	}
	cancel()
	close(release)
	err := <-returned
	if !errors.Is(err, context.Canceled) || completed.Load() != 4 || batches != 1 {
		t.Fatalf("cancel join err=%v completed=%d batches=%d", err, completed.Load(), batches)
	}
}

// No malformed configuration may silently raise the worker bound.
func TestFinalSemanticFixtureWorkerBoundsRejectInvalidInputs(t *testing.T) {
	t.Parallel()
	for _, entry := range []struct {
		ctx          context.Context
		count, width int
	}{
		{ctx: nil, count: 1, width: 1}, {ctx: t.Context(), count: -1, width: 1}, {ctx: t.Context(), count: 1, width: 0}, {ctx: t.Context(), count: 1, width: 5},
	} {
		called := false
		work := finalSemanticFixtureWorkControl{ctx: entry.ctx}
		if err := work.run("invalid", entry.count, entry.width, func(context.Context, int) error { called = true; return nil }); err == nil || called {
			t.Fatalf("invalid dispatch admitted work: %+v called=%v err=%v", entry, called, err)
		}
	}
	work := finalSemanticFixtureWorkControl{ctx: t.Context()}
	if err := work.run("empty", 0, 4, func(context.Context, int) error { return errors.New("unexpected") }); err != nil {
		t.Fatal(err)
	}
	if err := work.run("nil", 1, 4, nil); err == nil {
		t.Fatal("nil preparation accepted")
	}
	work.entered = func(context.Context, string, int) (func(), error) { return nil, nil }
	if leave, err := work.enter("missing-exit", 0); err == nil || leave != nil {
		t.Fatal("stage entry without an owned exit was accepted")
	}
}

// Conflicting bytes, kind and pre-existing destinations all fail preflight;
// publication cannot leak the earlier valid artifacts in the same union.
func TestFinalSemanticFixtureArtifactMergeRejectsConflictsWithoutPublication(t *testing.T) {
	t.Parallel()
	for _, variation := range []string{"bytes", "kind", "destination", "tampered"} {
		destination := map[string][]byte{"existing": []byte("original")}
		baseline := cloneFinalSemanticFixtureArtifacts(destination)
		first, second := &finalSemanticFixtureArtifacts{}, &finalSemanticFixtureArtifacts{}
		_, _ = first.derive("wire", "a", []byte("valid"))
		_, _ = first.derive("wire", "z", []byte("one"))
		kind, data := "wire", []byte("one")
		switch variation {
		case "bytes":
			data = []byte("two")
		case "kind":
			kind = "other"
		case "destination":
			destination["z"] = []byte("other")
			baseline = cloneFinalSemanticFixtureArtifacts(destination)
		}
		_, _ = second.derive(kind, "z", data)
		if variation == "tampered" {
			value := second.values["z"]
			value.data[0] ^= 1
			second.values["z"] = value
		}
		joined, err := joinFinalSemanticFixtureArtifacts(destination, []*finalSemanticFixtureArtifacts{first, second})
		if err == nil || joined != nil || !reflect.DeepEqual(destination, baseline) {
			t.Fatalf("%s collision returned partial publication: %v", variation, err)
		}
	}
}

// Identical repeats are deduplicated in URI order with fully detached bytes.
func TestFinalSemanticFixtureArtifactMergeDetachesAndOrdersBytes(t *testing.T) {
	t.Parallel()
	input := []byte("immutable")
	first, second := &finalSemanticFixtureArtifacts{}, &finalSemanticFixtureArtifacts{}
	_, _ = first.derive("wire", "z", input)
	_, _ = second.derive("wire", "a", input)
	_, _ = second.derive("wire", "z", input)
	input[0] ^= 1
	joined, err := joinFinalSemanticFixtureArtifacts(nil, []*finalSemanticFixtureArtifacts{first, second})
	if err != nil || len(joined) != 2 || joined[0].locator.URI != "a" || joined[1].locator.URI != "z" || string(joined[0].data) != "immutable" {
		t.Fatalf("canonical join=%v err=%v", joined, err)
	}
	destination := map[string][]byte{}
	publishFinalSemanticFixtureArtifacts(destination, joined)
	first.values["z"].data[0] ^= 1
	joined[0].data[0] ^= 1
	if string(destination["a"]) != "immutable" || string(destination["z"]) != "immutable" {
		t.Fatal("published bytes alias a worker or joined owner")
	}
}

// Worker-reachable malformed signing contexts return errors, never Goexit.
func TestFinalSemanticFixtureWorkerSigningErrorsReturn(t *testing.T) {
	t.Parallel()
	if err := attachFinalAttemptCutsResult(nil, nil, nil, nil, nil); err == nil {
		t.Fatal("nil attempt owner accepted")
	}
	if _, err := finalSemanticFixtureTerminalTransitionsResult(nil, nil); err == nil {
		t.Fatal("nil terminal owner accepted")
	}
	if _, _, err := finalSemanticFixtureHeadEgressResult(nil, 0, 0); err == nil {
		t.Fatal("invalid egress context accepted")
	}
	if _, err := finalTestPreparedSubmissionResult([]uint16{1}, nil, FinalCRv4Cycle{}, [32]byte{}); err == nil {
		t.Fatal("invalid prepared payload accepted")
	}
}

// Inject a failure only after two genuine terminal transactions reached the
// real preparation stage; both owners must return without publishing or mutation.
func TestFinalSemanticFixtureTerminalFailureJoinsWithoutPublication(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	validators := []FinalValidatorIdentityEvidence{source.Validators[0]}
	validators[0].Cycles = append([]FinalCRv4Cycle(nil), validators[0].Cycles[:2]...)
	inputs := map[string][]byte{}
	for _, cycle := range validators[0].Cycles {
		inputs[cycle.MeasurementArtifact.URI] = append([]byte(nil), artifacts[cycle.MeasurementArtifact.URI]...)
	}
	baseline := cloneFinalSemanticFixtureArtifacts(inputs)
	keys := []ed25519.PrivateKey{ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))}
	reached := make(chan struct{})
	var entries, completed atomic.Int32
	var release sync.Once
	failure := errors.New("terminal real-stage failure")
	work := finalSemanticFixtureWorkControl{ctx: t.Context(), entered: func(ctx context.Context, stage string, index int) (func(), error) {
		if stage != finalSemanticFixtureTerminalClosures {
			return nil, fmt.Errorf("unexpected terminal stage %s", stage)
		}
		if entries.Add(1) == 2 {
			release.Do(func() { close(reached) })
		}
		select {
		case <-reached:
		case <-ctx.Done():
			completed.Add(1)
			return nil, ctx.Err()
		}
		if index == 1 {
			completed.Add(1)
			return nil, failure
		}
		return func() { completed.Add(1) }, nil
	}}
	proofs, owner, err := prepareFinalSemanticFixtureClosedProofs(validators, keys, finalSemanticFixtureMaximumAttemptM, inputs, work)
	if !errors.Is(err, failure) || proofs != nil || owner != nil || entries.Load() != 2 || completed.Load() != 2 || !reflect.DeepEqual(inputs, baseline) {
		t.Fatalf("terminal failure leaked work: err=%v entries=%d completed=%d proofs=%d owner=%v", err, entries.Load(), completed.Load(), len(proofs), owner != nil)
	}
}

// Replay every initial/refresh batch from the same genuinely signed archive.
// A separately authenticated serial production source supplies exact calldata;
// private parallel owners cannot mutate parent caches, files or callbacks.
func TestFinalSemanticFixtureCalldataOwnersMatchSerialProduction(t *testing.T) {
	t.Parallel()
	evidence, artifacts := finalSemanticFixture(t)
	files, err := finalFleetGenerationArtifactFiles(&evidence, artifacts[evidence.FleetGeneration.Artifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	var parentCalls atomic.Int32
	archive := &finalSemanticArchive{ctx: t.Context(), files: files, collected: &FinalSemanticCollectedInputs{Policy: evidence.PolicyArtifact}, artifactDeriver: func(string, string, []byte) (FinalArtifactLocator, error) {
		parentCalls.Add(1)
		return FinalArtifactLocator{}, errors.New("parallel owner called parent artifact publisher")
	}}
	var batcher string
	for _, batch := range evidence.FleetGeneration.Batches {
		if batch.BatchWrite != nil && batch.BatchWrite.BatcherAddress != "" {
			batcher = batch.BatchWrite.BatcherAddress
			break
		}
	}
	chain := &FinalCollectedChainSnapshot{FleetBatcher: batcher}
	events := &finalSemanticEventIndex{byName: map[string][]finalSemanticEvent{}, byTx: map[string][]finalCanonicalEVMLog{}}
	parent, err := newFinalFleetGenerationSource(archive, &evidence, chain, events)
	if err != nil {
		t.Fatal(err)
	}
	originalRaw := cloneFinalSemanticFixtureArtifacts(parent.raw)
	fileHashes := map[string]string{}
	for path, data := range files {
		fileHashes[path] = bytesSHA256(data)
	}
	jobs := make([]finalSemanticFixtureCalldataJob, finalFleetGenerationBatchCount)
	work := finalSemanticFixtureWorkControl{ctx: t.Context()}
	if err := work.run(finalSemanticFixtureGenerationCalldata, len(jobs), 4, func(ctx context.Context, index int) error {
		stageWork := work
		stageWork.ctx = ctx
		job, err := prepareFinalSemanticFixtureGenerationCalldata(parent, uint64(index+1), stageWork, index)
		if err == nil {
			jobs[index] = job
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if parentCalls.Load() != 0 || len(parent.versions) != 0 || len(parent.postProofs) != 0 || !reflect.DeepEqual(parent.raw, originalRaw) || len(files) != len(fileHashes) {
		t.Fatal("parallel calldata preparation mutated its parent source")
	}
	for path, hash := range fileHashes {
		if bytesSHA256(files[path]) != hash {
			t.Fatalf("parallel calldata changed source %s", path)
		}
	}
	serialArtifacts := &finalSemanticFixtureArtifacts{}
	serialArchive := &finalSemanticArchive{ctx: t.Context(), files: files, collected: archive.collected, artifactDeriver: serialArtifacts.derive}
	serial, err := newFinalFleetGenerationSource(serialArchive, &evidence, chain, events)
	if err != nil {
		t.Fatal(err)
	}
	owners := make([]*finalSemanticFixtureArtifacts, len(jobs))
	for index, job := range jobs {
		batch := uint64(index + 1)
		first := (batch-1)*finalFleetGenerationBatchSize + 1
		last := first + finalFleetGenerationBatchSize - 1
		installed := make([]uint64, 0, finalFleetGenerationBatchSize)
		for fleet := first; fleet <= last; fleet++ {
			installed = append(installed, fleet)
		}
		initial, err := serial.installCalldata(FinalFleetGenerationBatchEvidence{Batch: batch, Generation: 1}, installed)
		if err != nil {
			t.Fatal(err)
		}
		if len(job.receipts) != 2 {
			t.Fatalf("batch %d receipt count=%d", batch, len(job.receipts))
		}
		var install FleetInstallBatchEvidence
		if err := json.Unmarshal(job.receipts[0].data, &install); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(job.initialCalldata, initial) || install.CalldataHash != common.BytesToHash(crypto.Keccak256(initial)).Hex() {
			t.Fatalf("parallel initial batch %d differs from serial production", batch)
		}
		var refresh FleetRefreshBatchEvidence
		if err := json.Unmarshal(files[job.receipts[1].path], &refresh); err != nil {
			t.Fatal(err)
		}
		renewed, err := serial.refreshCalldata(FinalFleetGenerationBatchEvidence{Batch: batch, Generation: 2, FirstFleet: first, LastFleet: last}, refresh)
		if err != nil {
			t.Fatal(err)
		}
		var updated FleetRefreshBatchEvidence
		if err := json.Unmarshal(job.receipts[1].data, &updated); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(job.refreshCalldata, renewed) || updated.CalldataHash != common.BytesToHash(crypto.Keccak256(renewed)).Hex() {
			t.Fatalf("parallel refresh batch %d differs from serial production", batch)
		}
		owners[index] = &jobs[index].artifacts
	}
	joined, err := joinFinalSemanticFixtureArtifacts(nil, owners)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := joinFinalSemanticFixtureArtifacts(nil, []*finalSemanticFixtureArtifacts{serialArtifacts})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(joined, expected) {
		t.Fatal("parallel calldata owners changed exact serial derived artifacts")
	}
	jobs[0].receipts[0].prior[0] ^= 1
	for _, value := range jobs[0].artifacts.values {
		value.data[0] ^= 1
		break
	}
	for path, hash := range fileHashes {
		if bytesSHA256(files[path]) != hash {
			t.Fatalf("worker output aliases parent source %s", path)
		}
	}
}

// Complete absence or canceled entry is rejected before any private source
// owner can reach an artifact callback.
func TestFinalSemanticFixtureCalldataRejectsInvalidOwnership(t *testing.T) {
	t.Parallel()
	if _, err := prepareFinalSemanticFixtureGenerationCalldata(nil, 1, finalSemanticFixtureWorkControl{ctx: t.Context()}, 0); err == nil {
		t.Fatal("nil source accepted")
	}
	if _, err := prepareFinalSemanticFixtureGenerationCalldata(&finalFleetGenerationSource{}, 1, finalSemanticFixtureWorkControl{ctx: t.Context()}, 0); err == nil {
		t.Fatal("unowned source accepted")
	}
}

// Cancel only after two fully authenticated terminal bodies are simultaneously
// held in their real stage; no result may escape before both owners leave.
func TestFinalSemanticFixtureTerminalCancellationJoinsWithoutPublication(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	validators := []FinalValidatorIdentityEvidence{source.Validators[0]}
	validators[0].Cycles = append([]FinalCRv4Cycle(nil), validators[0].Cycles[:2]...)
	inputs := map[string][]byte{}
	for _, cycle := range validators[0].Cycles {
		inputs[cycle.MeasurementArtifact.URI] = append([]byte(nil), artifacts[cycle.MeasurementArtifact.URI]...)
	}
	baseline := cloneFinalSemanticFixtureArtifacts(inputs)
	keys := []ed25519.PrivateKey{ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var completed atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: ctx, entered: func(context.Context, string, int) (func(), error) {
		entered <- struct{}{}
		<-release
		return func() { completed.Add(1) }, nil
	}}
	type result struct {
		proofs []FinalValidatorPathProofEvidence
		owner  *finalSemanticFixtureArtifacts
		err    error
	}
	returned := make(chan result, 1)
	go func() {
		proofs, owner, err := prepareFinalSemanticFixtureClosedProofs(validators, keys, finalSemanticFixtureMaximumAttemptM, inputs, work)
		returned <- result{proofs: proofs, owner: owner, err: err}
	}()
	for range 2 {
		<-entered
	}
	cancel()
	close(release)
	got := <-returned
	if !errors.Is(got.err, context.Canceled) || got.proofs != nil || got.owner != nil || completed.Load() != 2 || !reflect.DeepEqual(inputs, baseline) {
		t.Fatalf("terminal cancellation escaped its join: err=%v completed=%d proofs=%d owner=%v", got.err, completed.Load(), len(got.proofs), got.owner != nil)
	}
}
