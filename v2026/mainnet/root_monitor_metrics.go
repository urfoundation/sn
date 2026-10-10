// Root-monitor metrics reuse the atomic textfile owner. Optional publication
// cannot alter finalized continuity, sample classification or action authority.
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// A compact daemon observation references the complete finite preview by hash.
// It deliberately contains no storage census, signed bytes or unbounded details.
type rootMonitorObservation struct {
	ContentHash     string `json:"content_hash"`
	PolicyHash      string `json:"policy_hash"`
	FinalizedNumber uint64 `json:"finalized_number"`
	FinalizedHash   string `json:"finalized_hash"`
	ReadOnlyReady   bool   `json:"read_only_ready"`
	ActivationReady bool   `json:"activation_ready"`
}

// Only a complete sealed read contributes a current observation. Retained
// finality remains available independently in metrics during an unavailable read.
func projectRootMonitorObservation(value *rootPreviewEnvelope) *rootMonitorObservation {
	if value == nil {
		return nil
	}
	return &rootMonitorObservation{ContentHash: value.ContentHash, PolicyHash: value.Observation.PolicyHash,
		FinalizedNumber: value.Observation.Identity.FinalizedNumber, FinalizedHash: value.Observation.Identity.FinalizedHash,
		ReadOnlyReady: value.Observation.ReadOnlyReady}
}

// Publication acknowledgment is local to this process and always lags the file
// being written. Ambiguous writes cannot acknowledge themselves in their bytes.
type rootMonitorPublication struct {
	store         *monitorMetricsStore
	admit         func() (*monitorMetricsStore, error)
	outcome       string
	lastSuccessAt time.Time
	disabled      bool
}

// A changed path disables this optional publisher. Ordinary I/O failures retry
// at the next sample; old output ages independently in the existing collector.
func (self *rootMonitorPublication) publish(event rootMonitorEvent, state *monitorState, role string, sampledAt time.Time) {
	if self.disabled {
		return
	}
	if self.store == nil && self.admit != nil {
		var err error
		self.store, err = self.admit()
		if err != nil {
			self.outcome = "retrying"
			if !rootMonitorStartupPending(err) {
				self.outcome, self.disabled = "ownership-error", true
			}
			return
		}
	}
	if self.store == nil {
		return
	}
	raw := renderRootMonitorMetrics(event, state, role, self.outcome, self.lastSuccessAt)
	if err := self.store.saveRaw(raw); err != nil {
		self.outcome = "retrying"
		var ownership *monitorOutputOwnershipError
		if errors.As(err, &ownership) {
			self.outcome, self.disabled = "ownership-error", true
		}
		return
	}
	self.outcome, self.lastSuccessAt = "published", sampledAt
}

// Fixed numeric states and supplied bounded role/stream labels have no RPC text.
// Delivery, read success and finalized progress are deliberately separate series.
func renderRootMonitorMetrics(event rootMonitorEvent, state *monitorState, role, publication string, lastPublication time.Time) []byte {
	status := map[string]int{"starting": 0, "ready": 1, "blocked": 2, "rpc-error": 3, "finality-stalled": 4, "finality-conflict": 5, "rpc-integrity": 6, "checkpoint-error": 7, "storage-unavailable": 8}[event.Status]
	publicationCode := map[string]int{"unconfigured": 0, "starting": 1, "published": 2, "retrying": 3, "ownership-error": 4, "unavailable": 5}[publication]
	observedAt, _ := time.Parse(time.RFC3339Nano, event.ObservedAt)
	unix := func(value time.Time) int64 {
		if value.IsZero() {
			return 0
		}
		return value.Unix()
	}
	current, ready, finality := 0, 0, 0
	if event.Observation != nil {
		current = 1
		if event.Observation.ReadOnlyReady {
			ready = 1
		}
	}
	if state.lastHash != "" {
		finality = 1
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		value any
	}{
		{name: "sample_timestamp_seconds", value: unix(observedAt)},
		{name: "status", value: status},
		{name: "current_observation", value: current},
		{name: "read_only_ready", value: ready},
		{name: "last_read_success_timestamp_seconds", value: unix(state.lastSuccessAt)},
		{name: "has_finalized_evidence", value: finality},
		{name: "finalized_block", value: state.lastNumber},
		{name: "finalized_progress_timestamp_seconds", value: unix(state.lastProgressAt)},
		{name: "publication_status", value: publicationCode},
		{name: "publication_last_success_timestamp_seconds", value: unix(lastPublication)},
	} {
		name := "sn_mainnet_root_monitor_" + metric.name
		fmt.Fprintf(&output, "# TYPE %s gauge\n%s{role=%q} %v\n", name, name, role, metric.value)
	}
	return appendMonitorOutputMetrics([]byte(output.String()), "sn_mainnet_root_monitor", role, event.Diagnostics)
}
