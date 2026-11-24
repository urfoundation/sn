//go:build linux || darwin

// A runtime this build does not admit holds steering instead of ending the
// process. The loop is exercised directly, and the real production runtime
// gate supplies the refusals.
package validator

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
)

func runtimeHoldTestRefusals() map[string]error {
	block := types.Hash{0x49}
	approved := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 475, TransactionVersion: 1, StateVersion: 1}
	successor := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 499, TransactionVersion: 3, StateVersion: 1}
	notSuccessor := &crv4.RuntimeSuccessorRefusedError{BlockHash: block, Version: successor, Approved: approved}
	notAdmitted := &crv4.RuntimeSuccessorRefusedError{BlockHash: block, Version: successor, Approved: approved, CodeHash: types.Hash{0x50}.Hex(),
		Cause: errors.New("consumed SubnetInfoRuntimeApi version3 requires the reviewed node-subtensor/494/2/1 tuple")}
	unreviewed := &crv4.UnreviewedRuntimeError{BlockHash: block, Version: successor}
	return map[string]error{
		"not a successor":        notSuccessor,
		"not admitted":           notAdmitted,
		"unreviewed":             unreviewed,
		"wrapped":                fmt.Errorf("prepare native steering snapshot: %w", notAdmitted),
		"every endpoint":         errors.Join(notSuccessor, fmt.Errorf("second endpoint: %w", notSuccessor)),
		"unreviewed and wrapped": errors.Join(unreviewed, fmt.Errorf("retained intent: %w", notAdmitted)),
	}
}

// On 11-05, 11-16, 11-19, 11-20 and 11-23 a new Finney runtime ended the
// process every ten attempts, and each restart spent half an hour in startup
// to refuse again. The loop now keeps running and keeps reading as a failure.
func TestProductionSteeringLoopHoldsOnAnUnadmittedRuntime(t *testing.T) {
	t.Parallel()
	for name, refusal := range runtimeHoldTestRefusals() {
		polls, waits := 0, 0
		progress := nativeGapHoldTestProgress()
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			polls++
			return refusal
		}, func() bool {
			waits++
			return polls < 3*releaseSteeringFailureLimit
		}, progress)
		if err != nil || polls != 3*releaseSteeringFailureLimit {
			t.Fatalf("%s: a runtime no retry can admit ended the steering loop after %d polls: %v", name, polls, err)
		}
		if want := (polls-1)*productionRuntimeHoldPolls + 1; waits != want {
			t.Fatalf("%s: %d waits over %d polls, want the hold's pace %d", name, waits, polls, want)
		}
		if progress.value.Steering == nil || progress.value.Steering.Outcome != "hard_error" || progress.value.Steering.Current {
			t.Fatalf("%s: a held runtime must keep reading as a failure: %+v", name, progress.value.Steering)
		}
	}
}

// Only a graph that is wholly the refusal holds. A local file error or a
// transport failure beside it keeps the ordinary failure budget.
func TestProductionRuntimeHoldLeavesOtherFailuresHard(t *testing.T) {
	t.Parallel()
	local := &os.PathError{Op: "read", Path: "synthetic-state", Err: os.ErrNotExist}
	for name, refusal := range runtimeHoldTestRefusals() {
		polls := 0
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			polls++
			return errors.Join(refusal, local)
		}, func() bool { return true }, nativeGapHoldTestProgress())
		var limit *releaseSteeringFailureLimitError
		if !errors.As(err, &limit) || polls != releaseSteeringFailureLimit {
			t.Fatalf("%s: a refusal beside a local failure was held: %d polls: %v", name, polls, err)
		}
	}
	if productionSteeringRuntimeHoldOf(nil) != nil || productionSteeringRuntimeHoldOf(errors.New("runtime at 0x01 has identity node-subtensor/499/3/1, which is not a successor")) != nil {
		t.Fatal("an untyped error with the same text was held")
	}
}

// Each refusal of the real production runtime gate is held, as each one ended
// the process before, and keeps the text that operators search for.
func TestProductionRuntimeHoldRecognizesTheGateRefusals(t *testing.T) {
	for _, fault := range []string{"no-opt-in", "producer-storage", "transaction-version"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newProductionSuccessorTestFixture(t, fault != "no-opt-in")
			switch fault {
			case "producer-storage":
				fixture.successorWire = productionSuccessorChangedMetadata(t, func(metadata *types.Metadata) {
					for index := range metadata.AsMetadataV14.Pallets {
						pallet := &metadata.AsMetadataV14.Pallets[index]
						for itemIndex := range pallet.Storage.Items {
							if item := &pallet.Storage.Items[itemIndex]; pallet.Name == crv4.PalletName && string(item.Name) == "SubnetEpochIndex" {
								item.Type.AsMap.Hashers = []types.StorageHasherV10{{IsTwox64Concat: true}}
							}
						}
					}
				})
			case "transaction-version":
				fixture.successorVersion["transactionVersion"] = 3
			}
			native, cfg := fixture.rpc.native, fixture.cfg
			if err := enableReleaseRuntimeSuccession(native, cfg); err != nil {
				t.Fatal(err)
			}
			err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(170))
			if err == nil {
				t.Fatal("the gate admitted the changed runtime")
			}
			held := productionSteeringRuntimeHoldOf(observeReleaseError(fmt.Errorf("prepare: %w", err)))
			if held == nil {
				t.Fatalf("the gate's refusal is not held: %v", err)
			}
			if !strings.Contains(held.Error(), "node-subtensor/473/1/1") && !strings.Contains(held.Error(), "unreviewed identity") {
				t.Fatalf("the held refusal lost the text operators search for: %v", held)
			}
		})
	}
}
