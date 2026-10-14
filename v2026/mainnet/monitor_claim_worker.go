package main

import (
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

const monitorClaimCheckpointSchema = "urnetwork-mainnet-claim-checkpoint-v1"
const maxMonitorClaimCheckpointBytes = 1024 * 1024

type monitorClaimCheckpointRecord struct {
	Schema        string                        `json:"schema"`
	PolicyHash    string                        `json:"policy_hash"`
	State         monitorClaimState             `json:"state"`
	PolicyHistory *monitorProgressPolicyHistory `json:"policy_history,omitempty"`
	Archive       *monitorClaimArchive          `json:"archive,omitempty"`
	Catalog       *monitorHistoryCatalogState   `json:"catalog,omitempty"`
	Window        *monitorClaimWindowState      `json:"window,omitempty"`
	ContentHash   string                        `json:"content_hash"`
}

type monitorClaimWorker struct {
	policy               monitorClaimPolicy
	checkpoint           *monitorCheckpointStore
	metrics              *monitorMetricsStore
	state                *monitorClaimState
	client               *http.Client
	storage              monitorStorageRecovery
	policyHistory        *monitorProgressPolicyHistory
	acknowledgedPolicies int
	archive              *monitorClaimArchive
	catalog              *monitorHistoryCatalogState
	archiveAdmission     *monitorClaimArchiveAdmission
	window               *monitorClaimWindowState
}

func openMonitorClaimWorker(ctx context.Context, policy monitorClaimPolicy, expected identityExpectation, checkpoint, metrics string, hooks monitorServiceHooks) (*monitorClaimWorker, error) {
	if err := durablepath.Require(ctx); err != nil {
		return nil, err
	}
	if err := policy.validate(expected); err != nil {
		return nil, err
	}
	policy.Epochs = append([]monitorClaimEpochPolicy(nil), policy.Epochs...)
	if policy.HistoryCatalog != nil {
		catalog := *policy.HistoryCatalog
		policy.HistoryCatalog = &catalog
	}
	checkpointPath, metricsPath := monitorClaimPaths(checkpoint, metrics, policy.Role)
	owner, err := openMonitorCheckpoint(checkpointPath, expected, ctx)
	if err != nil {
		return nil, err
	}
	worker := &monitorClaimWorker{policy: policy, checkpoint: owner}
	worker.metrics, err = openMonitorMetrics(metricsPath, ctx)
	if err != nil {
		return nil, monitorAdmissionFailure(err, closeMonitorServiceOwners(policy.Role, nil, owner, hooks))
	}
	if hooks.afterCheckpointOpen != nil {
		hooks.afterCheckpointOpen(ctx, policy.Role, owner.lock)
	}
	worker.state, err = worker.load(ctx)
	if err != nil {
		return nil, monitorAdmissionFailure(err, worker.close(hooks))
	}
	if hooks.syncDirectory != nil {
		owner.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "checkpoint", file) }
		worker.metrics.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "metrics", file) }
	}
	// Each role owns the same bounded, redirect-free transport policy.
	worker.client = newMonitorProviderClient()
	return worker, nil
}

func hashMonitorClaimCheckpoint(record monitorClaimCheckpointRecord) (string, error) {
	record.ContentHash = ""
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (self *monitorClaimWorker) load(ctx context.Context) (result *monitorClaimState, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := self.checkpoint.requireOwner(); err != nil {
		return nil, err
	}
	raw, err := self.checkpoint.directory.read(filepath.Base(self.checkpoint.path), maxMonitorClaimCheckpointBytes, true)
	if monitorCheckpointAbsent(err) {
		if self.policy.Renewal != nil || self.policy.Window != nil {
			return nil, errors.New("claim policy renewal requires its retained checkpoint")
		}
		self.policyHistory = newMonitorProgressPolicyHistory(self.policy.resources(), self.policy.hash(), "")
		return newMonitorClaimState(self.policy), nil
	}
	if err != nil {
		return nil, err
	}
	record, err := decodeMonitorClaimCheckpoint(raw, self.policy)
	if err != nil {
		return nil, err
	}
	if err := record.Catalog.checkPath(self.checkpoint.path); err != nil {
		return nil, err
	}
	if record.Archive != nil {
		for _, reference := range record.Archive.Segments {
			if monitorHistoryPathsAlias(reference.Path, self.checkpoint.path) {
				return nil, errors.New("claim checkpoint aliases retained archive custody")
			}
		}
	}
	admission, err := openMonitorClaimArchive(ctx, self.policy, record, self.checkpoint.path)
	if err != nil {
		return nil, err
	}
	admitted := false
	defer func() {
		if !admitted {
			resultErr = monitorAdmissionFailure(resultErr, admission.close())
		}
	}()
	record, err = hydrateMonitorClaimRecord(record, admission.epochStateKVs, self.policy)
	if err != nil {
		return nil, err
	}
	history, prior, acknowledged, err := renewMonitorProgressPolicy(true, self.policy.resources(), self.policy.Renewal, record.PolicyHash, record.ContentHash, record.PolicyHistory, self.policy.policyHashAt)
	if err != nil {
		return nil, err
	}
	if err := admission.windows.checkReviews(history); err != nil {
		return nil, err
	}
	if err := validateMonitorClaimState(self.policy.atResources(prior), record.State); err != nil {
		return nil, err
	}
	for _, expected := range self.policy.Epochs[len(record.State.Epochs):] {
		record.State.Epochs = append(record.State.Epochs, monitorClaimEpochState{Epoch: expected.Epoch})
	}
	self.policyHistory, self.acknowledgedPolicies = history, acknowledged
	self.archive, self.catalog, self.archiveAdmission, self.window = record.Archive, record.Catalog, admission, record.Window
	admitted = true
	record.State.current = false
	return &record.State, nil
}

func (self *monitorClaimWorker) save() error {
	if err := self.archiveAdmission.check(); err != nil {
		return err
	}
	if err := self.checkpoint.requireOwner(); err != nil {
		return err
	}
	if err := errors.Join(validateMonitorClaimState(self.policy, *self.state), monitorClaimActiveBudget(*self.state, self.policy)); err != nil {
		return err
	}
	if self.policyHistory == nil || len(self.policyHistory.Entries) == 0 || self.policyHistory.validate(true, self.policyHistory.Entries[0].PolicyHash, self.policy.policyHashAt) != nil {
		return errors.New("claim policy history is not admitted")
	}
	record := monitorClaimCheckpointRecord{Schema: monitorClaimCheckpointSchema, PolicyHash: self.policyHistory.Entries[0].PolicyHash, State: *self.state, PolicyHistory: self.policyHistory, Archive: self.archive, Catalog: self.catalog, Window: self.window}
	record = self.archiveAdmission.externalize(record)
	var err error
	record.ContentHash, err = hashMonitorClaimCheckpoint(record)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > maxMonitorClaimCheckpointBytes {
		return errors.New("claim checkpoint exceeds its byte bound")
	}
	self.policy.observeWork("checkpoint-encoded-bytes", uint64(len(raw)))
	if record.Window != nil {
		// The current bounded transition is serialized for the checksum and
		// the publication. Report this work separately from hot state scans.
		self.policy.observeWork("encoded-window-epoch", 2*uint64(len(record.Window.PreviousPolicy.Epochs)+len(record.Window.Transition.Retired)))
	}
	err = errors.Join(self.checkpoint.directory.publish(filepath.Base(self.checkpoint.path), append(raw, '\n'), 0600, self.checkpoint.syncDirectory), self.checkpoint.requireOwner(), self.archiveAdmission.check())
	if err == nil {
		self.acknowledgedPolicies = len(self.policyHistory.Entries)
	}
	return err
}

type monitorClaimEventState struct {
	monitorClaimSummary
	SampleAt            time.Time `json:"sample_at"`
	LastAcceptedAt      time.Time `json:"last_accepted_at"`
	OutageSince         time.Time `json:"outage_since"`
	Incidents           uint64    `json:"incidents"`
	Restarts            uint64    `json:"restarts"`
	Sequence            uint64    `json:"sequence"`
	TotalEntries        uint64    `json:"total_entries"`
	OmittedEntries      uint64    `json:"omitted_entries"`
	UnresolvedEntries   uint64    `json:"unresolved_entries"`
	OmittedUnresolved   uint64    `json:"omitted_unresolved"`
	OldestUnresolved    *int64    `json:"oldest_unresolved_epoch,omitempty"`
	OmittedObservations uint64    `json:"omitted_observations"`
}

func (self *monitorClaimState) eventState(policy monitorClaimPolicy) monitorClaimEventState {
	value := monitorClaimEventState{monitorClaimSummary: self.summary(policy, self.SampleAt), SampleAt: self.SampleAt, LastAcceptedAt: self.LastAcceptedAt, OutageSince: self.OutageSince, Incidents: self.Incidents, Restarts: self.Restarts}
	if self.Record != nil {
		value.Sequence, value.TotalEntries = self.Record.Sequence, self.Record.TotalEntries
		value.OmittedEntries, value.UnresolvedEntries = self.Record.OmittedEntries, self.Record.UnresolvedEntries
		value.OmittedUnresolved, value.OmittedObservations = self.Record.OmittedUnresolvedEntries, self.Record.OmittedObservations
		value.OldestUnresolved = self.Record.OldestUnresolvedEpoch
	}
	return value
}

func renderMonitorClaimMetrics(policy monitorClaimPolicy, state *monitorClaimState, checkpointCurrent bool) []byte {
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
	summary := state.eventState(policy)
	oldest := int64(0)
	if summary.OldestUnresolved != nil {
		oldest = *summary.OldestUnresolved
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		value any
	}{
		{name: "status", value: monitorClaimCodes[state.Status]}, {name: "sample_timestamp_seconds", value: stamp(state.SampleAt)},
		{name: "read_current", value: flag(state.current && checkpointCurrent)}, {name: "read_last_success_timestamp_seconds", value: stamp(state.LastAcceptedAt)},
		{name: "checkpoint_current", value: flag(checkpointCurrent)}, {name: "outage_started_timestamp_seconds", value: stamp(state.OutageSince)},
		{name: "expected_epochs", value: summary.Expected}, {name: "unknown_epochs", value: summary.Unknown}, {name: "overdue_epochs", value: summary.Overdue},
		{name: "retained_accepted_receipts", value: summary.AcceptedReceipts}, {name: "retained_claimed_leaves", value: summary.ClaimedLeaves},
		{name: "retained_merkle_proofs", value: summary.MerkleProofs}, {name: "retained_api_no_claim", value: summary.ApiNoClaim},
		{name: "retained_deferred_receipts", value: summary.Deferred}, {name: "retained_aggregate_paid_receipts", value: summary.AggregatePaid},
		{name: "invalid_payment_observations", value: summary.InvalidPayment}, {name: "semantic_progress_timestamp_seconds", value: stamp(summary.ProgressAt)},
		{name: "source_total_entries", value: summary.TotalEntries}, {name: "source_unresolved_entries", value: summary.UnresolvedEntries},
		{name: "source_omitted_entries", value: summary.OmittedEntries}, {name: "source_omitted_unresolved_entries", value: summary.OmittedUnresolved},
		{name: "source_oldest_unresolved_epoch", value: oldest}, {name: "source_oldest_unresolved_known", value: flag(summary.OldestUnresolved != nil)},
		{name: "source_omitted_observations", value: summary.OmittedObservations}, {name: "incidents_total", value: state.Incidents}, {name: "producer_restarts_total", value: state.Restarts},
		{name: "independent_finality_verified", value: 0}, {name: "genesis_verified", value: 0}, {name: "per_epoch_payment_known", value: 0},
	} {
		fmt.Fprintf(&output, "# TYPE sn_mainnet_claim_%s gauge\nsn_mainnet_claim_%s{role=%q} %v\n", metric.name, metric.name, policy.Role, metric.value)
	}
	return []byte(output.String())
}

func (self *monitorClaimWorker) close(hooks monitorServiceHooks) error {
	if self.client != nil {
		self.client.CloseIdleConnections()
	}
	return errors.Join(self.archiveAdmission.close(), closeMonitorServiceOwners(self.policy.Role, self.metrics, self.checkpoint, hooks))
}

func (self *monitorClaimWorker) run(ctx context.Context, interval time.Duration, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) int {
	for ctx.Err() == nil {
		archiveReadErr := self.archiveAdmission.check()
		clock := monitorProgressReadClock{}
		if hooks.rpcWait != nil {
			clock.wait = func(waitCtx context.Context, delay time.Duration) error {
				return hooks.rpcWait(waitCtx, self.policy.Role, delay)
			}
		}
		var value *protocol.ClaimProgress
		code := "unavailable"
		if archiveReadErr == nil {
			value, code = readMonitorClaimWithBudget(ctx, self.client, self.policy, self.policy.resources().readBudget(), clock)
		} else if errors.Is(archiveReadErr, durablevolume.ErrIdentity) {
			code = "identity"
		}
		if value != nil && (code == "ok" || code == "unknown") && self.archiveAdmission != nil {
			if retainedCode := self.archiveAdmission.windows.publicationCode(value); retainedCode != "ok" {
				code = retainedCode
			}
		}
		if ctx.Err() != nil && !monitorClaimTerminal(code) {
			return 0
		}
		self.state.observe(self.policy, value, code, now().UTC())
		checkpointErr := self.save()
		raw := renderMonitorClaimMetrics(self.policy, self.state, checkpointErr == nil)
		raw = self.appendArchiveMetrics(raw)
		raw = self.appendWindowMetrics(raw)
		raw = appendMonitorProgressPolicyMetrics(raw, "sn_mainnet_claim", self.policy.Role, self.policyHistory, self.acknowledgedPolicies)
		raw = appendMonitorOutputMetrics(raw, "sn_mainnet_claim", self.policy.Role, monitorDiagnosticSnapshot(stdout, stderr))
		metricsErr := self.metrics.saveRaw(raw)
		combined := errors.Join(archiveReadErr, checkpointErr, metricsErr)
		var ownership *monitorOutputOwnershipError
		terminal := monitorClaimTerminal(self.state.Status) || errors.As(combined, &ownership) || errors.Is(combined, durablevolume.ErrIdentity)
		event := struct {
			Schema            string                      `json:"schema"`
			Role              string                      `json:"role"`
			Status            string                      `json:"status"`
			Current           bool                        `json:"current"`
			CheckpointCurrent bool                        `json:"checkpoint_current"`
			State             monitorClaimEventState      `json:"state"`
			Policy            monitorProgressPolicyStatus `json:"policy"`
			Archive           monitorClaimArchiveStatus   `json:"archive"`
			Window            monitorClaimWindowStatus    `json:"window"`
		}{Schema: "urnetwork-mainnet-claim-event-v1", Role: self.policy.Role, Status: self.state.Status, Current: self.state.current && checkpointErr == nil, CheckpointCurrent: checkpointErr == nil, State: self.state.eventState(self.policy), Policy: progressPolicyStatus(self.policyHistory, self.acknowledgedPolicies), Archive: self.archiveStatus(), Window: self.windowStatus()}
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
				archiveErr := self.archiveAdmission.close()
				self.archiveAdmission = nil
				file := prior.lock
				err := errors.Join(prior.close(), archiveErr)
				if hooks.afterClose != nil {
					err = errors.Join(err, hooks.afterClose(self.policy.Role, "checkpoint", file))
				}
				return err
			}, func() error {
				owner, err := openMonitorCheckpoint(prior.path, prior.expected, ctx)
				if err != nil {
					return err
				}
				candidate := &monitorClaimWorker{policy: self.policy, checkpoint: owner}
				state, err := candidate.load(ctx)
				if err != nil {
					return monitorAdmissionFailure(err, owner.close())
				}
				owner.syncDirectory = prior.syncDirectory
				self.checkpoint, self.state = owner, state
				self.policyHistory, self.acknowledgedPolicies = candidate.policyHistory, candidate.acknowledgedPolicies
				self.archive, self.catalog, self.archiveAdmission, self.window = candidate.archive, candidate.catalog, candidate.archiveAdmission, candidate.window
				return nil
			}, hooks)
			if err != nil {
				if monitorCanceledCheckpointLoad(ctx, err) {
					return 0
				}
				fmt.Fprintln(stderr, "claim checkpoint continuation:", err)
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
