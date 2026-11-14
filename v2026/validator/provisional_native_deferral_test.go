package validator

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProvisionalClosedNativeInputConfigRequiresExplicitTestnet(t *testing.T) {
	cfg := validReleaseConfig(t)
	if provisionalClosedNativeInputEnabled(&cfg) {
		t.Fatal("strict configuration enabled provisional deferral")
	}
	cfg.ProvisionalDeferClosedNativeInput = true
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = 1
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil || !strings.Contains(err.Error(), "chain 945") {
		t.Fatalf("non-testnet deferral accepted: %v", err)
	}
}

func TestProvisionalClosedNativeInputLoopWaitsWithoutSubmittingAndResumesNextEpoch(t *testing.T) {
	reads, attempts := 0, 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(context.Background(), func() (uint64, error) {
		reads++
		if reads <= releaseSteeringFailureLimit+2 {
			return 7, nil
		}
		return 8, nil
	}, func() error {
		attempts++
		if attempts == 1 {
			return &provisionalClosedNativeInput{nativeEpoch: 7, activeSettlement: 11}
		}
		if reads != releaseSteeringFailureLimit+3 {
			t.Fatal("deferred native epoch attempted a submission")
		}
		return nil
	}, func() bool { return reads < releaseSteeringFailureLimit+5 }, true)
	if err != nil || attempts != 2 {
		t.Fatalf("provisional loop attempts=%d err=%v", attempts, err)
	}
}

func TestProvisionalClosedNativeInputLoopRejectsStrictWrongEpochAndJoinedFailure(t *testing.T) {
	for _, test := range []struct {
		name  string
		allow bool
		err   error
	}{
		{"strict", false, &provisionalClosedNativeInput{nativeEpoch: 7, activeSettlement: 11}},
		{"different native epoch", true, &provisionalClosedNativeInput{nativeEpoch: 8, activeSettlement: 11}},
		{"independent error", true, errors.Join(&provisionalClosedNativeInput{nativeEpoch: 7, activeSettlement: 11}, errors.New("signed input changed"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			err := runReleaseSteeringLoopWithWaitAndDeferral(context.Background(), func() (uint64, error) { return 7, nil }, func() error { attempts++; return test.err }, func() bool { return true }, test.allow)
			if err == nil || attempts != releaseSteeringFailureLimit {
				t.Fatalf("unsafe deferral escaped: attempts=%d err=%v", attempts, err)
			}
		})
	}
}
