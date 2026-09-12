//go:build linux || darwin

package main

import (
	"reflect"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// The measured predecessor history remains charged alongside both complete
// campaign phases, restart/active tails and the fixed continuation end. The
// arithmetic does not authenticate a live source; stopped-ledger admission
// still obtains and checks its original signed prefix independently.
func TestEvidenceRelayContinuationNativeWarmupFitsRetainedSourceWithoutNewCapacity(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	bounds := cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds
	original := bounds
	observed := validatorcomponent.StoppedAttemptLedgerCapacity{Head: validatorcomponent.AttemptLedgerHead{LastSequence: 134673, TrailCount: 16958, RecordBytes: 698568804}, StorageBytes: 283301268, StorageFiles: 100}
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := work.remaining("release-1.0", false)
	if err != nil || remaining != 7570 {
		t.Fatal("full campaign readiness work changed", remaining, err)
	}
	if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, remaining, observed); err != nil {
		t.Fatal("overlapping real preparation/readiness does not fit retained lifetime", err)
	}
	if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, 8065, observed); err != nil {
		t.Fatal("exact measured source runway changed", err)
	}
	for _, span := range []uint64{8066, 10077} {
		if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, span, observed); err == nil {
			t.Fatal("preparation delay exceeded retained source capacity", span)
		}
	}
	full := observed
	full.Head.TrailCount = bounds.Disk.MaxTrailCount
	if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, remaining, full); err == nil {
		t.Fatal("past source use or pending restart/active tails were refunded")
	}
	if !reflect.DeepEqual(bounds, original) || cfg.Config.ValidatorEvidenceRelay.MaxSlots != 256 || bounds.Disk.MaxRecordCount != 655360 || bounds.Disk.MaxTrailCount != 81920 {
		t.Fatal("launchable forecast enlarged original source or relay limits")
	}
}
