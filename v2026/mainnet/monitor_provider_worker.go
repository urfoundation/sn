package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorProviderCheckpointSchema = "urnetwork-mainnet-provider-checkpoint-v1"
const maxMonitorProviderCheckpointBytes = 128 * 1024

type monitorProviderCheckpointRecord struct {
	Schema        string                        `json:"schema"`
	PolicyHash    string                        `json:"policy_hash"`
	State         monitorProviderState          `json:"state"`
	PolicyHistory *monitorProgressPolicyHistory `json:"policy_history,omitempty"`
	ContentHash   string                        `json:"content_hash"`
}

type monitorProviderWorker struct {
	policy               monitorProviderPolicy
	checkpoint           *monitorCheckpointStore
	metrics              *monitorMetricsStore
	state                *monitorProviderState
	client               *http.Client
	storage              monitorStorageRecovery
	policyHistory        *monitorProgressPolicyHistory
	acknowledgedPolicies int
}

// Diagnostic events have constant size regardless of the member census. The
// complete retained roster belongs to the bounded checkpoint, never a queue
// record that can overflow the shared diagnostic sink's independent limit.
type monitorProviderEventState struct {
	SampleAt        time.Time `json:"sample_at"`
	LastAcceptedAt  time.Time `json:"last_accepted_at"`
	OutageSince     time.Time `json:"outage_since"`
	Incidents       uint64    `json:"incidents"`
	Restarts        uint64    `json:"restarts"`
	InstanceId      string    `json:"instance_id,omitempty"`
	Sequence        uint64    `json:"sequence"`
	ExpectedMembers int       `json:"expected_members"`
	ReadyMembers    int       `json:"ready_members"`
}

func (self *monitorProviderWorker) eventState(current bool) monitorProviderEventState {
	value := monitorProviderEventState{SampleAt: self.state.SampleAt, LastAcceptedAt: self.state.LastAcceptedAt, OutageSince: self.state.OutageSince, Incidents: self.state.Incidents, Restarts: self.state.Restarts, ExpectedMembers: len(self.policy.Members)}
	if current {
		value.ReadyMembers = self.state.ready
	}
	if self.state.Record != nil {
		value.InstanceId = self.state.Record.InstanceId
		value.Sequence = self.state.Record.Sequence
	}
	return value
}

func openMonitorProviderWorker(ctx context.Context, policy monitorProviderPolicy, expected identityExpectation, checkpoint, metrics string, hooks monitorServiceHooks) (*monitorProviderWorker, error) {
	if err := durablepath.Require(ctx); err != nil {
		return nil, err
	}
	if err := policy.validate(); err != nil {
		return nil, err
	}
	policy.Members = append([]monitorExpectedProviderMember(nil), policy.Members...)
	checkpointPath, metricsPath := monitorProviderPaths(checkpoint, metrics, policy.Role)
	owner, err := openMonitorCheckpoint(checkpointPath, expected, ctx)
	if err != nil {
		return nil, err
	}
	worker := &monitorProviderWorker{policy: policy, checkpoint: owner}
	metricOwner, err := openMonitorMetrics(metricsPath, ctx)
	if err != nil {
		return nil, monitorAdmissionFailure(err, closeMonitorServiceOwners(policy.Role, nil, owner, hooks))
	}
	worker.metrics = metricOwner
	if hooks.afterCheckpointOpen != nil {
		hooks.afterCheckpointOpen(ctx, policy.Role, owner.lock)
	}
	worker.state, err = worker.load(ctx)
	if err != nil {
		return nil, monitorAdmissionFailure(err, closeMonitorServiceOwners(policy.Role, metricOwner, owner, hooks))
	}
	if hooks.syncDirectory != nil {
		owner.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "checkpoint", file) }
		metricOwner.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "metrics", file) }
	}
	worker.client = newMonitorProviderClient()
	return worker, nil
}

func hashMonitorProviderCheckpoint(record monitorProviderCheckpointRecord) (string, error) {
	record.ContentHash = ""
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (self *monitorProviderWorker) load(ctx context.Context) (*monitorProviderState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := self.checkpoint.requireOwner(); err != nil {
		return nil, err
	}
	raw, err := self.checkpoint.directory.read(filepath.Base(self.checkpoint.path), maxMonitorProviderCheckpointBytes, true)
	if monitorCheckpointAbsent(err) {
		if self.policy.Renewal != nil {
			return nil, errors.New("provider policy renewal requires its retained checkpoint")
		}
		self.policyHistory = newMonitorProgressPolicyHistory(self.policy.resources(), self.policy.hash(), "")
		return &monitorProviderState{Status: "starting"}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record monitorProviderCheckpointRecord
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("provider checkpoint has trailing JSON")
	}
	hash, err := hashMonitorProviderCheckpoint(record)
	if err != nil || hash != record.ContentHash || record.Schema != monitorProviderCheckpointSchema {
		return nil, errors.New("provider checkpoint differs from retained policy or checksum")
	}
	history, prior, acknowledged, err := renewMonitorProgressPolicy(false, self.policy.resources(), self.policy.Renewal, record.PolicyHash, record.ContentHash, record.PolicyHistory, self.policy.policyHashAt)
	if err != nil {
		return nil, err
	}
	if err := validateMonitorProviderState(self.policy.atResources(prior), record.State); err != nil {
		return nil, err
	}
	self.policyHistory, self.acknowledgedPolicies = history, acknowledged
	// Only a new complete observation can make this process current again.
	record.State.current, record.State.ready = false, 0
	return &record.State, nil
}

func validateMonitorProviderState(policy monitorProviderPolicy, state monitorProviderState) error {
	if state.SampleAt.IsZero() || state.HighWaterAt.IsZero() || state.HighWaterAt.Before(state.SampleAt) {
		return errors.New("provider checkpoint lacks a completed bounded observation")
	}
	if _, ok := monitorProviderCodes[state.Status]; !ok || state.Status == "starting" {
		return errors.New("provider checkpoint status is invalid")
	}
	if (state.Record == nil) != state.LastAcceptedAt.IsZero() || state.Status != "ok" && state.OutageSince.IsZero() || state.Status == "ok" && !state.OutageSince.IsZero() {
		return errors.New("provider checkpoint lost its observation provenance")
	}
	if state.Record != nil {
		if err := state.Record.Validate(); err != nil {
			return err
		}
		if state.Record.Source != policy.ExpectedSource || len(state.Record.Members) != len(policy.Members) {
			return errors.New("provider checkpoint source or roster differs")
		}
		expected := map[string]string{}
		for _, member := range policy.Members {
			expected[member.Slot] = member.ClientId
		}
		for _, member := range state.Record.Members {
			identity, ok := expected[member.Slot]
			if !ok || member.ClientId != "" && member.ClientId != identity {
				return errors.New("provider checkpoint member identity differs")
			}
		}
	}
	return nil
}

func (self *monitorProviderWorker) save() error {
	if err := self.checkpoint.requireOwner(); err != nil {
		return err
	}
	if err := validateMonitorProviderState(self.policy, *self.state); err != nil {
		return err
	}
	if self.policyHistory == nil || len(self.policyHistory.Entries) == 0 || self.policyHistory.validate(false, self.policyHistory.Entries[0].PolicyHash, self.policy.policyHashAt) != nil {
		return errors.New("provider policy history is not admitted")
	}
	record := monitorProviderCheckpointRecord{Schema: monitorProviderCheckpointSchema, PolicyHash: self.policyHistory.Entries[0].PolicyHash, State: *self.state, PolicyHistory: self.policyHistory}
	var err error
	record.ContentHash, err = hashMonitorProviderCheckpoint(record)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > maxMonitorProviderCheckpointBytes {
		return errors.New("provider checkpoint exceeds its byte bound")
	}
	err = errors.Join(self.checkpoint.directory.publish(filepath.Base(self.checkpoint.path), append(raw, '\n'), 0600, self.checkpoint.syncDirectory), self.checkpoint.requireOwner())
	if err == nil {
		self.acknowledgedPolicies = len(self.policyHistory.Entries)
	}
	return err
}

var monitorProviderCodes = map[string]int{"starting": 0, "ok": 1, "not_ready": 2, "missing": 3, "unavailable": 4, "invalid": 5, "stale": 6, "clock": 7, "identity": 8, "authentication": 9}

func renderMonitorProviderMetrics(policy monitorProviderPolicy, state *monitorProviderState, checkpointCurrent bool) []byte {
	current := state.current && checkpointCurrent
	ready := 0
	if current {
		ready = state.ready
	}
	flag := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	stamp := func(v time.Time) int64 {
		if v.IsZero() {
			return 0
		}
		return v.Unix()
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		value any
	}{
		{"status", monitorProviderCodes[state.Status]}, {"sample_timestamp_seconds", stamp(state.SampleAt)}, {"read_current", flag(current)},
		{"read_last_success_timestamp_seconds", stamp(state.LastAcceptedAt)}, {"outage_started_timestamp_seconds", stamp(state.OutageSince)},
		{"expected_members", len(policy.Members)}, {"ready_members", ready}, {"all_expected_ready", flag(current && ready == len(policy.Members))},
		{"checkpoint_current", flag(checkpointCurrent)}, {"incidents_total", state.Incidents}, {"producer_restarts_total", state.Restarts},
		{"proof_progress_known", 0}, {"settlement_progress_known", 0},
	} {
		fmt.Fprintf(&output, "# TYPE sn_mainnet_provider_%s gauge\nsn_mainnet_provider_%s{role=%q} %v\n", metric.name, metric.name, policy.Role, metric.value)
	}
	return []byte(output.String())
}

func (self *monitorProviderWorker) close(hooks monitorServiceHooks) error {
	if self.client != nil {
		self.client.CloseIdleConnections()
	}
	return closeMonitorServiceOwners(self.policy.Role, self.metrics, self.checkpoint, hooks)
}

func (self *monitorProviderWorker) run(ctx context.Context, interval time.Duration, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) int {
	for ctx.Err() == nil {
		clock := monitorProgressReadClock{}
		if hooks.rpcWait != nil {
			clock.wait = func(waitCtx context.Context, delay time.Duration) error {
				return hooks.rpcWait(waitCtx, self.policy.Role, delay)
			}
		}
		value, code := readMonitorProviderWithBudget(ctx, self.client, self.policy, self.policy.resources().readBudget(), clock)
		if ctx.Err() != nil && !monitorProviderTerminal(code) {
			return 0
		}
		self.state.observe(self.policy, value, code, now().UTC())
		checkpointErr := self.save()
		raw := renderMonitorProviderMetrics(self.policy, self.state, checkpointErr == nil)
		raw = appendMonitorProgressPolicyMetrics(raw, "sn_mainnet_provider", self.policy.Role, self.policyHistory, self.acknowledgedPolicies)
		raw = appendMonitorOutputMetrics(raw, "sn_mainnet_provider", self.policy.Role, monitorDiagnosticSnapshot(stdout, stderr))
		metricsErr := self.metrics.saveRaw(raw)
		combined := errors.Join(checkpointErr, metricsErr)
		var ownership *monitorOutputOwnershipError
		terminal := monitorProviderTerminal(self.state.Status) || errors.As(combined, &ownership) || errors.Is(combined, durablevolume.ErrIdentity)
		event := struct {
			Schema            string                      `json:"schema"`
			Role              string                      `json:"role"`
			Status            string                      `json:"status"`
			Current           bool                        `json:"current"`
			CheckpointCurrent bool                        `json:"checkpoint_current"`
			State             monitorProviderEventState   `json:"state"`
			Policy            monitorProgressPolicyStatus `json:"policy"`
		}{Schema: "urnetwork-mainnet-provider-event-v1", Role: self.policy.Role, Status: self.state.Status, Current: self.state.current && checkpointErr == nil, CheckpointCurrent: checkpointErr == nil, State: self.eventState(self.state.current && checkpointErr == nil), Policy: progressPolicyStatus(self.policyHistory, self.acknowledgedPolicies)}
		if err := json.NewEncoder(stdout).Encode(event); err != nil {
			if ctx.Err() != nil && !terminal {
				return 0
			}
			return 3
		}
		if hooks.afterEvent != nil {
			hooks.afterEvent(ctx, self.policy.Role)
		}
		if terminal {
			return 3
		}
		if errors.Is(checkpointErr, durablehead.ErrUncertain) {
			prior := self.checkpoint
			err := self.storage.resume(ctx, self.policy.Role, func() error {
				file := prior.lock
				err := prior.close()
				if hooks.afterClose != nil {
					err = errors.Join(err, hooks.afterClose(self.policy.Role, "checkpoint", file))
				}
				return err
			}, func() error {
				owner, err := openMonitorCheckpoint(prior.path, prior.expected, ctx)
				if err != nil {
					return err
				}
				candidate := &monitorProviderWorker{policy: self.policy, checkpoint: owner}
				state, err := candidate.load(ctx)
				if err != nil {
					return monitorAdmissionFailure(err, owner.close())
				}
				owner.syncDirectory = prior.syncDirectory
				self.checkpoint, self.state = owner, state
				self.policyHistory, self.acknowledgedPolicies = candidate.policyHistory, candidate.acknowledgedPolicies
				return nil
			}, hooks)
			if err != nil {
				if monitorCanceledCheckpointLoad(ctx, err) {
					return 0
				}
				fmt.Fprintln(stderr, "provider checkpoint continuation:", err)
				return 3
			}
			continue
		}
		if !waitMonitorService(ctx, self.policy.Role, interval, hooks) {
			return 0
		}
	}
	return 0
}
