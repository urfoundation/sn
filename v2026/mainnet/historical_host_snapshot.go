// Explicit allocator observations retain original memory without asserting a
// storage read or write. The Rust engine checks the exact original call ABI.
package main

import (
	"encoding/hex"
	"errors"
)

func validateHistoricalHostSnapshotRule(profile *historicalReplayObservationProfile, rule historicalReplayHookRule) error {
	if rule.HostSnapshot == nil {
		return nil
	}
	if profile.Schema != historicalNativeProfileSchema || rule.Purpose != "native-epoch" || len(rule.Memory) == 0 || (*rule.HostSnapshot != "ext_allocator_malloc_version_1" && *rule.HostSnapshot != "ext_allocator_free_version_1") {
		return errors.New("original host snapshot requires native epoch memory and an exact allocator name")
	}
	return nil
}

func validateHistoricalHostSnapshotRecord(profile *historicalReplayObservationProfile, rule historicalReplayHookRule, record historicalReplayObservation, frameIndex int) error {
	if rule.HostSnapshot == nil {
		if record.Operation == "host" {
			return errors.New("original host snapshot lacks an explicit selected host")
		}
		return nil
	}
	if err := validateHistoricalHostSnapshotRule(profile, rule); err != nil {
		return err
	}
	if record.Operation != "host" || record.KeyHex != "0x"+hex.EncodeToString([]byte(*rule.HostSnapshot)) || record.ValueHex != nil || record.StorageReturn != nil || frameIndex != 0 {
		return errors.New("original host snapshot differs from its direct nonstorage call")
	}
	return nil
}
