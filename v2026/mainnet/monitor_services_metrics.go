// Fixed gauges share a bounded independently configured role label. Producer
// identities and raw errors never create additional metric time series.
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Numeric values remain useful when retained, but current/known flags govern
// their interpretation. A timestamp never becomes protocol acceptance.
func renderMonitorValidatorMetrics(policy monitorValidatorPolicy, state *monitorValidatorState, publication string, checkpointCurrent bool) ([]byte, error) {
	if state == nil || !monitorRolePattern.MatchString(policy.Role) {
		return nil, errors.New("validator metrics require a bounded role and state")
	}
	status, severity := state.condition(state.SampleAt, policy)
	statusCode, known := monitorServiceStatusCodes[status]
	if !known {
		return nil, errors.New("validator metrics status is unknown")
	}
	publicationCode := 0
	switch publication {
	case "starting":
	case "published":
		publicationCode = 1
	case "retrying":
		publicationCode = 2
	default:
		return nil, errors.New("validator metrics publication is unknown")
	}
	severityCode := 0
	if severity == "warning" {
		severityCode = 1
	} else if severity == "critical" {
		severityCode = 2
	}
	flag := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	stamp := func(value time.Time) int64 {
		if value.IsZero() {
			return 0
		}
		return value.Unix()
	}
	timestamp := func(value string) int64 { return stamp(monitorProgressTime(value)) }
	values := []struct {
		name  string
		value any
	}{
		{name: "sample_timestamp_seconds", value: stamp(state.SampleAt)},
		{name: "read_last_success_timestamp_seconds", value: stamp(state.LastReadSuccessAt)},
		{name: "read_current", value: flag(state.readCurrent)},
		{name: "read_outage_started_timestamp_seconds", value: stamp(state.OutageSince)},
		{name: "status", value: statusCode}, {name: "severity", value: severityCode},
		{name: "clock_fault_timestamp_seconds", value: stamp(state.ClockFaultAt)},
		{name: "candidate_heartbeat_timestamp_seconds", value: stamp(state.CandidateHeartbeatAt)},
		{name: "export_status", value: publicationCode},
		{name: "export_last_success_timestamp_seconds", value: stamp(state.PublicationLastSuccessAt)},
		{name: "checkpoint_current", value: flag(checkpointCurrent)},
		{name: "has_record", value: flag(state.Record != nil)},
		{name: "source_current", value: flag(state.sourceCurrent(state.SampleAt))},
	}
	// A zero value is always paired with a current/known flag; it cannot turn a
	// missing intent into known empty or an unknown cursor into epoch zero.
	var heartbeat, published, intentObserved, intentSuccess, intentCreated, intentProgress, nativeSuccess, settlementSuccess, settlementProgress, steeringSuccess int64
	var publisherStatus, intentStatus, steeringStatus int
	var sourceMatches, intentCurrent, intentKnownEmpty, intentPresent, intentOriginalConfig, nativeCurrent, settlementCurrent, cursorKnown, steeringCurrent bool
	var nativeEpoch, nativeBlock, revealBlock, preparedBlock, finalizedBlock, applicationBlock, settlementEpoch, targetEpoch, pending, firstPending uint64
	if value := state.Record; value != nil {
		heartbeat, published = timestamp(value.HeartbeatAt), timestamp(value.Publisher.LastSuccessAt)
		sourceMatches = value.Source == policy.ExpectedSource
		switch value.Publisher.Outcome {
		case "published":
			publisherStatus = 1
		case "retrying":
			publisherStatus = 2
		}
		current := state.sourceCurrent(state.SampleAt) && sourceMatches
		fresh := func(observed, success string, reported bool) bool {
			return current && reported && monitorServiceFresh(state.SampleAt, monitorProgressTime(observed)) && monitorServiceFresh(state.SampleAt, monitorProgressTime(success))
		}
		if observation := value.Intent; observation != nil {
			intentObserved, intentSuccess = timestamp(observation.ObservedAt), timestamp(observation.LastSuccessAt)
			intentCurrent = fresh(observation.ObservedAt, observation.LastSuccessAt, observation.Current)
			intentPresent = observation.Value != nil
			intentKnownEmpty = intentCurrent && !intentPresent
			if intent := observation.Value; intent != nil {
				intentOriginalConfig = intent.ConfigHash != policy.ExpectedSource.ConfigHash
				intentCreated, intentProgress = timestamp(intent.CreatedAt), timestamp(intent.ProgressAt)
				revealBlock, preparedBlock, finalizedBlock, applicationBlock = intent.RevealBlock, intent.PreparedAtBlock, intent.FinalizedBlock, intent.ApplicationBlock
				switch intent.Status {
				case "pending":
					intentStatus = 1
				case "finalized":
					intentStatus = 2
				case "applied":
					intentStatus = 3
				case "failed":
					intentStatus = 4
				}
			}
		}
		if native := value.Native; native != nil {
			nativeCurrent = fresh(native.ObservedAt, native.LastSuccessAt, native.Current)
			nativeSuccess, nativeEpoch, nativeBlock = timestamp(native.LastSuccessAt), native.Epoch, native.Block
		}
		if settlement := value.Settlement; settlement != nil {
			settlementCurrent = fresh(settlement.ObservedAt, settlement.LastSuccessAt, settlement.Current) && settlement.CursorKnown
			cursorKnown = settlement.CursorKnown
			settlementSuccess, settlementProgress = timestamp(settlement.LastSuccessAt), timestamp(settlement.ProgressAt)
			settlementEpoch, targetEpoch, pending, firstPending = settlement.Epoch, settlement.TargetEpoch, settlement.PendingPublications, settlement.FirstPendingEpoch
		}
		if steering := value.Steering; steering != nil {
			steeringCurrent = fresh(steering.ObservedAt, steering.LastSuccessAt, steering.Current)
			steeringSuccess = timestamp(steering.LastSuccessAt)
			steeringStatus = map[string]int{"starting": 0, "working": 1, "epoch_wait": 2, "reveal_wait": 3, "receipt_pending": 4, "receipt_transport_wait": 5, "read_wait": 6, "complete": 7, "hard_error": 8}[steering.Outcome]
		}
	}
	values = append(values, []struct {
		name  string
		value any
	}{
		{name: "source_config_current", value: flag(sourceMatches)}, {name: "heartbeat_timestamp_seconds", value: heartbeat},
		{name: "producer_publish_status", value: publisherStatus}, {name: "producer_publish_last_success_timestamp_seconds", value: published},
		{name: "intent_current", value: flag(intentCurrent)}, {name: "intent_known_empty", value: flag(intentKnownEmpty)},
		{name: "intent_present", value: flag(intentPresent)}, {name: "intent_original_config", value: flag(intentOriginalConfig)},
		{name: "intent_status", value: intentStatus}, {name: "intent_observed_timestamp_seconds", value: intentObserved},
		{name: "intent_last_success_timestamp_seconds", value: intentSuccess}, {name: "intent_created_timestamp_seconds", value: intentCreated},
		{name: "intent_progress_timestamp_seconds", value: intentProgress}, {name: "intent_prepared_block", value: preparedBlock},
		{name: "intent_reveal_block", value: revealBlock}, {name: "intent_finalized_block", value: finalizedBlock}, {name: "intent_application_block", value: applicationBlock},
		{name: "native_current", value: flag(nativeCurrent)}, {name: "native_last_success_timestamp_seconds", value: nativeSuccess},
		{name: "native_epoch", value: nativeEpoch}, {name: "native_block", value: nativeBlock},
		{name: "settlement_current", value: flag(settlementCurrent)}, {name: "settlement_cursor_known", value: flag(cursorKnown)},
		{name: "settlement_last_success_timestamp_seconds", value: settlementSuccess}, {name: "settlement_progress_timestamp_seconds", value: settlementProgress},
		{name: "settlement_epoch", value: settlementEpoch}, {name: "settlement_target_epoch", value: targetEpoch},
		{name: "settlement_pending_publications", value: pending}, {name: "settlement_first_pending_epoch", value: firstPending},
		{name: "steering_current", value: flag(steeringCurrent)}, {name: "steering_last_success_timestamp_seconds", value: steeringSuccess},
		{name: "steering_status", value: steeringStatus},
	}...)
	deadline := state.nativeDeadline(policy, state.SampleAt)
	var misses, firstEpoch, lastEpoch, firstBlock, lastBlock uint64
	var firstAt, lastAt int64
	if history := state.NativeDeadline; history != nil {
		misses = history.MissedWindows
		firstEpoch, lastEpoch = history.FirstMiss.Intent.Value.NativeEpoch, history.LastMiss.Intent.Value.NativeEpoch
		firstBlock, lastBlock = history.FirstMiss.Native.Block, history.LastMiss.Native.Block
		firstAt, lastAt = stamp(history.FirstMiss.DetectedAt), stamp(history.LastMiss.DetectedAt)
	}
	values = append(values, []struct {
		name  string
		value any
	}{
		{name: "protocol_deadline_known", value: flag(deadline.Known)},
		{name: "native_deadline_enabled", value: flag(policy.NativeDeadline != nil)},
		{name: "native_deadline_current", value: flag(deadline.Current)},
		{name: "native_deadline_status", value: monitorNativeDeadlineStatusCodes[deadline.Status]},
		{name: "native_deadline_intent_epoch", value: deadline.IntentEpoch},
		{name: "native_deadline_observed_epoch", value: deadline.ObservedEpoch},
		{name: "native_deadline_observed_block", value: deadline.ObservedBlock},
		{name: "native_deadline_projected_boundary_block", value: deadline.ProjectedBoundaryBlock},
		{name: "native_deadline_blocks_remaining", value: deadline.BlocksRemaining},
		{name: "native_deadline_warning_blocks", value: deadline.WarningBlocks},
		{name: "native_deadline_critical_blocks", value: deadline.CriticalBlocks},
		{name: "native_deadline_unresolved", value: flag(state.NativeDeadline != nil)},
		{name: "native_deadline_missed_windows", value: misses},
		{name: "native_deadline_first_miss_timestamp_seconds", value: firstAt},
		{name: "native_deadline_last_miss_timestamp_seconds", value: lastAt},
		{name: "native_deadline_first_miss_intent_epoch", value: firstEpoch},
		{name: "native_deadline_last_miss_intent_epoch", value: lastEpoch},
		{name: "native_deadline_first_miss_observed_block", value: firstBlock},
		{name: "native_deadline_last_miss_observed_block", value: lastBlock},
	}...)
	var output strings.Builder
	for _, metric := range values {
		fmt.Fprintf(&output, "# TYPE sn_mainnet_validator_%s gauge\nsn_mainnet_validator_%s{role=%q} %v\n", metric.name, metric.name, policy.Role, metric.value)
	}
	raw := appendMonitorReadIncidentMetrics([]byte(output.String()), policy.Role, state.ReadIncidents)
	raw = appendMonitorProducerDiagnostics(raw, policy.Role, state)
	raw = appendMonitorSteeringLivenessMetrics(raw, policy, state)
	if len(raw) > 32*1024 {
		return nil, errors.New("validator metrics exceed their bound")
	}
	return raw, nil
}
