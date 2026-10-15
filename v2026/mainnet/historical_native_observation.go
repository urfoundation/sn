// Native v2 retains actual returned storage bytes and original execution
// memory. A callsite label remains unapproved until the economic policy admits
// its exact runtime, body/layout review, engine and replay boundary.
package main

import (
	"errors"
	"strings"
)

const historicalNativeProfileSchema = "urnetwork-original-wasm-native-observation-v2"
const historicalNativeCaptureLimit = 256 * 1024
const historicalNativeReportLimit = 64 * 1024 * 1024

// Separate native capacity keeps the receipt profile unchanged. Three nodes
// per four-field UID leaf (branch/leaf/hashed value), doubled, admits the full
// 4096-provider census while all byte, call and time dimensions remain finite.
const historicalNativeProofNodes = 2 * (3*(4*4096+4) + 1)
const historicalNativeProofBytes = 64 * 1024 * 1024
const historicalNativeProofNodeBytes = 16 * 1024 * 1024
const historicalNativeJobLimit = 192 * 1024 * 1024
const historicalNativeCaptureReportLimit = 272 * 1024 * 1024
const historicalNativeBackendReads = 4 * 65536
const historicalNativeBackendBytes = 256 * 1024 * 1024

func historicalNativeProfile(profile *historicalReplayObservationProfile) bool {
	return profile != nil && profile.Schema == historicalNativeProfileSchema
}
func historicalProofNodeLimit(profile *historicalReplayObservationProfile) int {
	if historicalNativeProfile(profile) {
		return historicalNativeProofNodeBytes
	}
	return 8 * 1024 * 1024
}
func historicalJobLimits(profile *historicalReplayObservationProfile) (int, int, int) {
	if historicalNativeProfile(profile) {
		return historicalNativeJobLimit, historicalNativeProofNodes, historicalNativeProofBytes
	}
	return historicalReplayJobLimit, 8192, 24 * 1024 * 1024
}
func historicalCaptureLimits(profile *historicalReplayObservationProfile) (int, uint64, uint64) {
	if historicalNativeProfile(profile) {
		return historicalNativeCaptureReportLimit, historicalNativeBackendReads, historicalNativeBackendBytes
	}
	return historicalCaptureReportLimit, 65536, 64 * 1024 * 1024
}

func historicalNativePointerValid(global *string, offsets []uint32) bool {
	return len(offsets) <= 4 && (global == nil || len(*global) > 0 && len(*global) <= 64)
}

func historicalNativePurpose(value string) bool {
	return value == "native-miner-capture" || value == "native-recipient-owner-hotkey" || value == "native-recipient-auto-stake" || value == "native-recipient-owner" || value == "native-uid-census" || value == "native-epoch-index" || value == "native-fee-exempt" || value == "native-fee-refund-zero" || value == "native-drain" || value == "native-epoch" || value == "native-emission" || value == "native-miner-credit" || value == "native-owner-recycle" || historicalPrincipalEffectPurpose(value) || historicalYumaPurpose(value)
}

func validateHistoricalNativeCaptures(rule historicalReplayHookRule) error {
	if len(rule.Memory) > 16 || !historicalNativePurpose(rule.Purpose) && len(rule.Memory) != 0 {
		return errors.New("native callsite memory profile exceeds its purpose or count")
	}
	seen, total := map[string]bool{}, uint64(0)
	for _, capture := range rule.Memory {
		if capture.Name == "" || len(capture.Name) > 48 || strings.Trim(capture.Name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || seen[capture.Name] || capture.Bytes == 0 || capture.Bytes > historicalNativeCaptureLimit || !historicalNativePointerValid(capture.Global, capture.DereferenceOffsets) {
			return errors.New("native memory capture layout is invalid")
		}
		seen[capture.Name] = true
		maximum := uint64(capture.Bytes)
		if repeat := capture.Repeat; repeat != nil {
			if repeat.Maximum == 0 || repeat.Maximum > rootCensusLimit || repeat.Stride < capture.Bytes || repeat.Stride > historicalNativeCaptureLimit || !historicalNativePointerValid(repeat.Count.Global, repeat.Count.DereferenceOffsets) {
				return errors.New("native repeated capture count or stride exceeds reviewed bound")
			}
			maximum *= uint64(repeat.Maximum)
		}
		if maximum > historicalNativeCaptureLimit {
			return errors.New("native repeated capture bytes exceed bound")
		}
		total += maximum
	}
	if total > 512*1024 {
		return errors.New("native memory capture bytes exceed bound")
	}
	return nil
}

func validateHistoricalNativeObservation(profile *historicalReplayObservationProfile, observation historicalReplayObservation) error {
	v2 := profile.Schema == historicalNativeProfileSchema
	read := observation.Operation == "get" || observation.Operation == "read"
	if v2 && read {
		returned := observation.StorageReturn
		if returned == nil || returned.Present != (returned.ValueHex != nil) || (observation.Operation == "read") != (returned.Offset != nil && returned.OutputLength != nil) || observation.Operation == "get" && (returned.Offset != nil || returned.OutputLength != nil) {
			return errors.New("native actual storage return is absent or has contradictory slice provenance")
		}
		if returned.ValueHex != nil {
			if _, err := historicalReplayHex(*returned.ValueHex, 1024*1024); err != nil {
				return err
			}
		}
	} else if observation.StorageReturn != nil {
		return errors.New("legacy or non-read observation invented returned storage bytes")
	}
	native := historicalNativePurpose(observation.Purpose)
	if !native {
		if observation.Native != nil {
			return errors.New("non-native callsite acquired native memory")
		}
		return nil
	}
	if !v2 || observation.Native == nil {
		return errors.New("native callsite omits original memory provenance")
	}
	var selected *historicalReplayHookRule
	for index := range profile.Rules {
		rule := &profile.Rules[index]
		for frameIndex := range observation.Stack {
			if historicalRuleMatchesAt(*rule, observation.Stack, frameIndex) {
				selected = rule
				break
			}
		}
	}
	if selected == nil || len(selected.Memory) != len(observation.Native.Memory) {
		return errors.New("native memory differs from exact callsite layout")
	}
	if err := validateHistoricalExecutionState(*selected, *observation.Native); err != nil {
		return err
	}
	if observation.Native.ExecutionPhaseHex != nil {
		phase, err := historicalReplayHex(*observation.Native.ExecutionPhaseHex, 5)
		if err != nil || len(phase) == 0 || phase[0] > 2 || phase[0] == 0 && len(phase) != 5 || phase[0] != 0 && len(phase) != 1 {
			return errors.New("native execution phase encoding differs")
		}
	}
	for index, capture := range selected.Memory {
		value := observation.Native.Memory[index]
		width := uint64(capture.Bytes)
		if capture.Repeat == nil {
			if value.ElementCount != nil {
				return errors.New("native scalar capture invented a vector count")
			}
		} else {
			if value.ElementCount == nil || *value.ElementCount > capture.Repeat.Maximum {
				return errors.New("native repeated capture omitted or exceeded original count")
			}
			width *= uint64(*value.ElementCount)
		}
		raw, err := historicalReplayHex(value.BytesHex, int(width))
		if err != nil || value.Name != capture.Name || len(raw) != int(width) || capture.Global == nil && len(capture.DereferenceOffsets) == 0 && value.Address != capture.Address {
			return errors.New("native captured original memory width, name or direct address differs")
		}
	}
	return nil
}
