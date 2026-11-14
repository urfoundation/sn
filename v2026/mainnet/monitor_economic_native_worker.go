// Native economic observations use an independently provisioned checkpoint per
// role. A durable publication precedes process-local cursor acknowledgement.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"github.com/urnetwork/server/v2026/strecovery"
)

const monitorEconomicNativeCheckpointSchema = "urnetwork-mainnet-native-economic-checkpoint-v1"

var monitorEconomicNativeStatusCodes = map[string]int{
	"starting": 0, "observed-economic-outcome-unresolved": 1, "caught-up": 2,
	"unavailable": 3, "runtime-unavailable": 4, "capacity-held": 5,
	"identity-conflict": 6, "clock-unavailable": 7,
	"archive-ready": 8,
}

type monitorEconomicNativeCheckpoint struct {
	Schema            string                          `json:"schema"`
	PolicyHash        string                          `json:"policy_hash"`
	RuntimeCatalog    []monitorEconomicRuntimeEntry   `json:"runtime_catalog,omitempty"`
	RuntimeCapacity   *monitorEconomicRuntimeCapacity `json:"runtime_capacity,omitempty"`
	ReadBudgetSeconds *uint64                         `json:"read_budget_seconds,omitempty"`
	State             monitorEconomicNativeState      `json:"state"`
	ContentHash       string                          `json:"content_hash"`
}

func (self monitorEconomicNativeCheckpoint) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

type monitorEconomicNativeWorker struct {
	policy                 monitorEconomicNativePolicy
	checkpoint             *monitorCheckpointStore
	metrics                *monitorMetricsStore
	state                  *monitorEconomicNativeState
	client                 *rpcClient
	storage                monitorStorageRecovery
	runtimeAcknowledgement *monitorEconomicRuntimeAcknowledgement
	archive                []*monitorHistorySnapshot
}

// These settings describe an acknowledged checkpoint, not a config proposal.
type monitorEconomicRuntimeAcknowledgement struct {
	CatalogHash       string                         `json:"catalog_hash"`
	Entries           int                            `json:"entries"`
	Capacity          monitorEconomicRuntimeCapacity `json:"capacity"`
	ReadBudgetSeconds uint64                         `json:"read_budget_seconds"`
}

func (self *monitorEconomicNativeWorker) acknowledgeRuntime(record monitorEconomicNativeCheckpoint) {
	capacity := monitorEconomicRuntimeCapacity{Entries: 8, Bytes: 8 * 1024}
	if record.RuntimeCapacity != nil {
		capacity = *record.RuntimeCapacity
	}
	seconds := self.policy.ReadBudgetSeconds
	if self.policy.ReadBudgetBasisSeconds != nil {
		seconds = *self.policy.ReadBudgetBasisSeconds
	}
	seconds = monitorEconomicReadSeconds(seconds)
	if record.ReadBudgetSeconds != nil {
		seconds = *record.ReadBudgetSeconds
	}
	self.runtimeAcknowledgement = &monitorEconomicRuntimeAcknowledgement{CatalogHash: rootObjectHash(record.RuntimeCatalog), Entries: len(record.RuntimeCatalog), Capacity: capacity, ReadBudgetSeconds: seconds}
}

func openMonitorEconomicNativeWorker(ctx context.Context, client *rpcClient, policy monitorEconomicNativePolicy, expected identityExpectation, checkpoint, metrics string, hooks monitorServiceHooks) (*monitorEconomicNativeWorker, error) {
	if err := durablepath.Require(ctx); err != nil {
		return nil, err
	}
	if err := policy.validate(expected); err != nil {
		return nil, err
	}
	policy.Observation.FeePayers = append([]string(nil), policy.Observation.FeePayers...)
	if policy.Observation.Execution != nil {
		value := *policy.Observation.Execution
		if value.Producer != nil {
			producer := *value.Producer
			producer.Renewals = append([]planFileReference(nil), producer.Renewals...)
			value.Producer = &producer
		}
		policy.Observation.Execution = &value
	}
	registration, generation := *policy.Observation.SubnetRegistrationBlock, *policy.Observation.SubnetGeneration
	policy.Observation.SubnetRegistrationBlock, policy.Observation.SubnetGeneration = &registration, &generation
	policy.RuntimeCatalog = append([]monitorEconomicRuntimeEntry(nil), policy.RuntimeCatalog...)
	for i := range policy.RuntimeCatalog {
		policy.RuntimeCatalog[i].Purposes = append([]string(nil), policy.RuntimeCatalog[i].Purposes...)
	}
	if policy.RuntimeCapacity != nil {
		value := *policy.RuntimeCapacity
		policy.RuntimeCapacity = &value
	}
	if policy.ReadBudgetBasisSeconds != nil {
		value := *policy.ReadBudgetBasisSeconds
		policy.ReadBudgetBasisSeconds = &value
	}
	if policy.HistoryCatalog != nil {
		value := *policy.HistoryCatalog
		policy.HistoryCatalog = &value
	}
	checkpointPath, metricsPath := monitorEconomicNativePaths(checkpoint, metrics, policy.Role)
	owner, err := openMonitorCheckpoint(checkpointPath, expected, ctx)
	if err != nil {
		return nil, err
	}
	worker := &monitorEconomicNativeWorker{policy: policy, checkpoint: owner}
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
	seconds := monitorEconomicReadSeconds(policy.ReadBudgetSeconds)
	worker.client, err = newRpcClient(client.url, time.Duration(seconds)*time.Second)
	if err != nil {
		return nil, monitorAdmissionFailure(err, worker.close(hooks))
	}
	worker.client.retryWait = client.retryWait
	if hooks.rpcWait != nil {
		worker.client.retryWait = func(ctx context.Context, wait time.Duration) error { return hooks.rpcWait(ctx, policy.Role, wait) }
	}
	return worker, nil
}

func (self *monitorEconomicNativeWorker) close(hooks monitorServiceHooks) error {
	if self == nil {
		return nil
	}
	if self.client != nil {
		self.client.httpClient.CloseIdleConnections()
	}
	return errors.Join(self.closeArchive(), closeMonitorServiceOwners(self.policy.Role, self.metrics, self.checkpoint, hooks))
}

func (self *monitorEconomicNativeWorker) load(ctx context.Context) (*monitorEconomicNativeState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := self.checkpoint.requireOwner(); err != nil {
		return nil, err
	}
	raw, err := self.checkpoint.directory.read(filepath.Base(self.checkpoint.path), maxRpcReplyBytes, true)
	if monitorCheckpointAbsent(err) {
		return newMonitorEconomicNativeState(self.policy), nil
	}
	if err != nil {
		return nil, err
	}
	record, err := decodeMonitorEconomicNativeCheckpoint(raw, self.policy, ctx)
	if err != nil {
		return nil, err
	}
	if err := record.State.Catalog.checkPath(self.checkpoint.path); err != nil {
		return nil, err
	}
	self.archive, err = openMonitorEconomicNativeArchive(ctx, self.policy, &record.State)
	if err != nil {
		return nil, err
	}
	self.acknowledgeRuntime(record)
	return &record.State, nil
}

func (self *monitorEconomicNativeWorker) save(state *monitorEconomicNativeState) error {
	if err := self.checkArchive(); err != nil {
		return err
	}
	if err := self.checkpoint.requireOwner(); err != nil {
		return err
	}
	if err := state.validate(self.policy); err != nil {
		return err
	}
	capacity := self.policy.runtimeCapacity()
	seconds := monitorEconomicReadSeconds(self.policy.ReadBudgetSeconds)
	record := monitorEconomicNativeCheckpoint{Schema: monitorEconomicNativeCheckpointSchema, PolicyHash: self.policy.identityHash(), RuntimeCatalog: self.policy.RuntimeCatalog, RuntimeCapacity: &capacity, ReadBudgetSeconds: &seconds, State: *state}
	record.ContentHash = record.hash()
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(raw)+1 > maxRpcReplyBytes {
		return errMonitorEconomicCapacity
	}
	err = errors.Join(self.checkpoint.directory.publish(filepath.Base(self.checkpoint.path), append(raw, '\n'), 0600, self.checkpoint.syncDirectory), self.checkpoint.requireOwner(), self.checkArchive())
	if err == nil {
		self.acknowledgeRuntime(record)
	}
	return err
}

func monitorEconomicNativeReadCode(err error) string {
	switch {
	case errors.Is(err, errRpcIntegrity), errors.Is(err, errRpcIdentityMismatch), errors.Is(err, durablevolume.ErrIdentity), errors.Is(err, strecovery.ErrNativeFinalityConflict):
		return "identity-conflict"
	case errors.Is(err, errMonitorEconomicCapacity):
		return "capacity-held"
	case errors.Is(err, errRootReceiptProfileUnavailable):
		return "runtime-unavailable"
	default:
		return "unavailable"
	}
}

// Only this summary is exported. It does not emit unbounded retained history,
// arbitrary source labels, or a numeric zero for unproved economic amounts.
type monitorEconomicNativeSummary struct {
	TreasuryIncome               *nativeTreasuryAmounts                 `json:"treasury_income,omitempty"`
	ProducerCapacity             *nativeProducerCapacitySummary         `json:"producer_capacity,omitempty"`
	ExecutionAccounting          *nativeExecutionWindow                 `json:"execution_accounting,omitempty"`
	ExecutionAuthority           string                                 `json:"execution_authority,omitempty"`
	ArchivedEvents               uint64                                 `json:"archived_events"`
	ArchiveSegments              int                                    `json:"archive_segments"`
	ArchiveSegmentCapacity       int                                    `json:"archive_segment_capacity"`
	ConfiguredRuntimeCatalogHash string                                 `json:"configured_runtime_catalog_hash"`
	ConfiguredRuntimeEntries     int                                    `json:"configured_runtime_entries"`
	ConfiguredRuntimeCapacity    monitorEconomicRuntimeCapacity         `json:"configured_runtime_capacity"`
	ConfiguredReadBudgetSeconds  uint64                                 `json:"configured_read_budget_seconds"`
	RuntimeAcknowledgement       *monitorEconomicRuntimeAcknowledgement `json:"runtime_acknowledgement"`
	LastExecutionRuntime         *rootReceiptProfile                    `json:"last_execution_runtime,omitempty"`
	LastPostStateRuntime         *rootReceiptProfile                    `json:"last_post_state_runtime,omitempty"`
	Cursor                       economicEmissionBoundary               `json:"cursor"`
	PendingThrough               *economicEmissionBoundary              `json:"pending_through,omitempty"`
	Finalized                    *economicEmissionBoundary              `json:"observed_finalized,omitempty"`
	BatchCount                   uint64                                 `json:"batch_count"`
	BatchChainHash               string                                 `json:"batch_chain_hash"`
	ObservedAlpha                string                                 `json:"observed_alpha"`
	ObservedFeesRao              *string                                `json:"observed_fees_rao"`
	FeePayerCount                int                                    `json:"fee_payer_count"`
	HistoryEntries               int                                    `json:"history_entries"`
	CapacityRemaining            uint64                                 `json:"capacity_remaining"`
	CapacityBytesRemaining       uint64                                 `json:"capacity_bytes_remaining"`
	SampleAt                     time.Time                              `json:"sample_at"`
	LastReadAt                   time.Time                              `json:"last_read_at"`
	LastProgressAt               time.Time                              `json:"last_progress_at"`
	UnavailableSince             time.Time                              `json:"unavailable_since"`
	Incidents                    uint64                                 `json:"incidents"`
	Authority                    string                                 `json:"authority"`
	NativeMinerAllocationAlpha   *string                                `json:"native_miner_allocation_alpha"`
	ProviderEntitlementAlpha     *string                                `json:"provider_entitlement_alpha"`
	OwnerRecycledAlpha           *string                                `json:"owner_recycled_alpha"`
	ActualNativeOutcomeVerified  bool                                   `json:"actual_native_outcome_verified"`
}

func (self *monitorEconomicNativeState) summary(policy monitorEconomicNativePolicy) monitorEconomicNativeSummary {
	var fees *string
	if len(policy.Observation.FeePayers) != 0 && self.BatchCount != 0 {
		value := self.ObservedFeesRao
		fees = &value
	}
	summary := monitorEconomicNativeSummary{ConfiguredRuntimeCatalogHash: rootObjectHash(policy.RuntimeCatalog), ConfiguredRuntimeEntries: len(policy.RuntimeCatalog), ConfiguredRuntimeCapacity: policy.runtimeCapacity(), ConfiguredReadBudgetSeconds: monitorEconomicReadSeconds(policy.ReadBudgetSeconds), LastExecutionRuntime: self.LastExecutionRuntime, LastPostStateRuntime: self.LastPostStateRuntime, Cursor: self.Cursor, PendingThrough: self.PendingThrough, Finalized: self.Finalized, BatchCount: self.BatchCount, BatchChainHash: self.BatchChainHash,
		ObservedAlpha: self.ObservedAlpha, ObservedFeesRao: fees, FeePayerCount: len(policy.Observation.FeePayers),
		HistoryEntries: len(self.History), CapacityRemaining: self.CapacityRemaining, CapacityBytesRemaining: self.CapacityBytesRemaining,
		SampleAt: self.SampleAt, LastReadAt: self.LastReadAt, LastProgressAt: self.LastProgressAt, UnavailableSince: self.UnavailableSince,
		Incidents: self.Incidents, Authority: "owned-rpc-assertion"}
	if self.ExecutionAccounting != nil {
		value := *self.ExecutionAccounting
		value.Treasury = cloneNativeTreasuryAmounts(self.ExecutionAccounting.Treasury)
		summary.TreasuryIncome = cloneNativeTreasuryAmounts(value.Treasury)
		summary.ExecutionAccounting = &value
		summary.ExecutionAuthority = "independently-approved-runtime-layout-and-finalized-boundaries"
		summary.NativeMinerAllocationAlpha, summary.ProviderEntitlementAlpha, summary.OwnerRecycledAlpha = &value.MinerAllocation, &value.ProviderEntitlement, &value.OwnerRecycled
	}
	summary.ProducerCapacity = self.producerCapacity(policy)
	return summary
}

func (self *monitorEconomicNativeWorker) summary() monitorEconomicNativeSummary {
	summary := self.state.summary(self.policy)
	summary.RuntimeAcknowledgement = self.runtimeAcknowledgement
	summary.ArchiveSegmentCapacity = int(self.state.Catalog.capacity(self.policy.HistoryCatalog).Segments)
	if self.state.Archive != nil {
		summary.ArchiveSegments = len(self.state.Archive.Segments)
		summary.ArchivedEvents = self.state.Archive.Events
	}
	return summary
}

func renderMonitorEconomicNativeMetrics(policy monitorEconomicNativePolicy, state *monitorEconomicNativeState, code string, current, checkpointCurrent bool, now time.Time) []byte {
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
	stalled := !state.LastProgressAt.IsZero() && now.Sub(state.LastProgressAt) >= time.Duration(policy.StallSeconds)*time.Second
	catalog, _ := json.Marshal(policy.RuntimeCatalog)
	capacity := policy.runtimeCapacity()
	archiveSegments := 0
	archiveCapacity := state.Catalog.capacity(policy.HistoryCatalog)
	archiveCatalog, _ := monitorNativeCatalogBytes(*state)
	if state.Archive != nil {
		archiveSegments = len(state.Archive.Segments)
	}
	archiveWarning := 2*uint64(archiveSegments+1) >= archiveCapacity.Segments || 2*uint64(archiveSegments+1) >= archiveCapacity.HeldReaders
	if policy.HistoryCatalog != nil {
		archiveWarning = archiveWarning || 2*(uint64(len(archiveCatalog))+6*maximumMonitorHistoryPath+256) >= archiveCapacity.CatalogBytes || state.Catalog != nil && len(state.Catalog.Revisions)+1 >= maximumMonitorHistoryRevisions
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		value any
	}{
		{name: "status", value: monitorEconomicNativeStatusCodes[code]}, {name: "read_current", value: flag(current)},
		{name: "checkpoint_current", value: flag(checkpointCurrent)}, {name: "cursor_block", value: state.Cursor.Number},
		{name: "batches", value: state.BatchCount}, {name: "retained_events", value: len(state.History)},
		{name: "sample_timestamp_seconds", value: stamp(state.SampleAt)}, {name: "last_read_timestamp_seconds", value: stamp(state.LastReadAt)},
		{name: "last_progress_timestamp_seconds", value: stamp(state.LastProgressAt)}, {name: "progress_stalled", value: flag(stalled)},
		{name: "outage_started_timestamp_seconds", value: stamp(state.UnavailableSince)}, {name: "incidents", value: state.Incidents},
		{name: "history_capacity", value: policy.HistoryEntries}, {name: "history_remaining", value: state.CapacityRemaining},
		{name: "history_bytes_remaining", value: state.CapacityBytesRemaining},
		{name: "archive_segments", value: archiveSegments}, {name: "archive_segment_capacity", value: archiveCapacity.Segments},
		{name: "archive_catalog_byte_capacity", value: archiveCapacity.CatalogBytes}, {name: "archive_reader_capacity", value: archiveCapacity.HeldReaders},
		{name: "archive_catalog_bytes", value: len(archiveCatalog)}, {name: "archive_capacity_warning", value: flag(archiveWarning)},
		{name: "capacity_warning", value: flag(state.CapacityRemaining <= policy.HistoryEntries/4 || state.CapacityBytesRemaining <= maximumMonitorEconomicBytes/4)},
		{name: "fee_payer_count", value: len(policy.Observation.FeePayers)}, {name: "independent_finality_verified", value: 0},
		{name: "fee_observation_known", value: flag(len(policy.Observation.FeePayers) != 0 && state.BatchCount != 0)},
		{name: "native_allocation_known", value: flag(state.ExecutionAccounting != nil)}, {name: "provider_entitlement_known", value: flag(state.ExecutionAccounting != nil)}, {name: "owner_recycling_known", value: flag(state.ExecutionAccounting != nil)},
		{name: "actual_native_outcome_verified", value: 0},
		{name: "runtime_catalog_entries", value: len(policy.RuntimeCatalog)},
		{name: "runtime_catalog_bytes", value: len(catalog)},
		{name: "runtime_catalog_entry_capacity", value: capacity.Entries},
		{name: "runtime_catalog_byte_capacity", value: capacity.Bytes},
		{name: "runtime_catalog_capacity_warning", value: flag(uint64(len(policy.RuntimeCatalog))*5 >= capacity.Entries*4 || uint64(len(catalog))*5 >= capacity.Bytes*4)},
		{name: "configured_read_budget_seconds", value: monitorEconomicReadSeconds(policy.ReadBudgetSeconds)},
	} {
		fmt.Fprintf(&output, "sn_mainnet_native_economic_%s{role=%q} %v\n", metric.name, policy.Role, metric.value)
	}
	if producer := state.producerCapacity(policy); producer != nil {
		for _, metric := range []struct {
			name  string
			value any
		}{
			{name: "producer_completed_jobs", value: producer.Completed},
			{name: "producer_job_capacity", value: producer.Capacity.Jobs},
			{name: "producer_jobs_remaining", value: producer.JobsRemaining},
			{name: "producer_acknowledged_renewals", value: producer.AcknowledgedRenewals},
			{name: "producer_configured_renewals", value: producer.ConfiguredRenewals},
			{name: "producer_capacity_warning", value: flag(producer.CapacityWarning)},
			{name: "producer_disk_forecast_known", value: flag(producer.Forecast != nil)},
		} {
			fmt.Fprintf(&output, "sn_mainnet_native_economic_%s{role=%q} %v\n", metric.name, policy.Role, metric.value)
		}
	}
	return []byte(output.String())
}

func (self *monitorEconomicNativeWorker) resume(ctx context.Context, hooks monitorServiceHooks) error {
	prior := self.checkpoint
	return self.storage.resume(ctx, self.policy.Role, func() error {
		file := prior.lock
		err := errors.Join(self.closeArchive(), prior.close())
		if hooks.afterClose != nil {
			err = errors.Join(err, hooks.afterClose(self.policy.Role, "checkpoint", file))
		}
		return err
	}, func() error {
		owner, err := openMonitorCheckpoint(prior.path, prior.expected, ctx)
		if err != nil {
			return err
		}
		candidate := &monitorEconomicNativeWorker{policy: self.policy, checkpoint: owner}
		state, err := candidate.load(ctx)
		if err != nil {
			return monitorAdmissionFailure(err, owner.close())
		}
		owner.syncDirectory = prior.syncDirectory
		self.checkpoint, self.state = owner, state
		self.archive = candidate.archive
		self.runtimeAcknowledgement = candidate.runtimeAcknowledgement
		return nil
	}, hooks)
}

// Every RPC in a batch shares the role's one finite deadline. Exhaustion records
// a missing sample and keeps the same cursor for the next owned iteration.
func (self *monitorEconomicNativeWorker) run(ctx context.Context, interval time.Duration, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) int {
	for ctx.Err() == nil {
		var observation *economicEmissionObservation
		readErr := errors.Join(self.checkpoint.requireOwner(), self.checkArchive())
		if readErr == nil && self.state.Status == "capacity-held" {
			readErr = errMonitorEconomicCapacity
		}
		if readErr == nil {
			observation, readErr = observeMonitorEconomicNative(ctx, self.client, self.policy, self.state)
		}
		if errors.Is(readErr, errNativeProducerCleanup) {
			fmt.Fprintln(stderr, "native execution producer ownership:", readErr)
			return 3
		}
		if ctx.Err() != nil {
			if monitorEconomicNativeReadCode(readErr) == "identity-conflict" {
				fmt.Fprintln(stderr, "native execution contradictory evidence:", readErr)
				return 3
			}
			return 0
		}
		observedAt := now().UTC()
		candidate := *self.state
		code := "caught-up"
		if readErr == nil && observation != nil {
			var next *monitorEconomicNativeState
			next, readErr = self.state.append(self.policy, observation, observedAt, ctx)
			if readErr == nil {
				candidate, code = *next, next.Status
			}
		}
		if readErr != nil {
			code = monitorEconomicNativeReadCode(readErr)
		}
		if observedAt.IsZero() || observedAt.Before(self.state.SampleAt) {
			// A regressed wall clock cannot publish a future-dated successful sample.
			readErr = errors.Join(readErr, errors.New("native economic observation clock regressed"))
			if code != "identity-conflict" {
				code = "clock-unavailable"
			}
			candidate = *self.state
			observedAt = self.state.SampleAt
		}
		candidate.SampleAt, candidate.Status = observedAt, code
		if readErr == nil {
			candidate.LastReadAt, candidate.UnavailableSince = observedAt, time.Time{}
		} else if candidate.UnavailableSince.IsZero() {
			candidate.UnavailableSince = observedAt
			if candidate.Incidents < math.MaxUint64 {
				candidate.Incidents++
			}
		}
		checkpointErr := self.save(&candidate)
		if checkpointErr == nil {
			self.state = &candidate
		}
		var checkpointOwnership *monitorOutputOwnershipError
		if errors.Is(checkpointErr, durablevolume.ErrIdentity) || errors.As(checkpointErr, &checkpointOwnership) {
			code = "identity-conflict"
		}
		current := readErr == nil && checkpointErr == nil
		raw := renderMonitorEconomicNativeMetrics(self.policy, self.state, code, current, checkpointErr == nil, observedAt)
		raw = appendMonitorOutputMetrics(raw, "sn_mainnet_native_economic", self.policy.Role, monitorDiagnosticSnapshot(stdout, stderr))
		metricsErr := self.metrics.saveRaw(raw)
		combined := errors.Join(readErr, checkpointErr, metricsErr)
		var ownership *monitorOutputOwnershipError
		terminal := code == "identity-conflict" || errors.As(combined, &ownership) || errors.Is(combined, durablevolume.ErrIdentity)
		issue := ""
		if combined != nil {
			issue = combined.Error()
			if len(issue) > 1024 {
				issue = issue[:1024]
			}
		}
		event := struct {
			Schema            string                       `json:"schema"`
			Role              string                       `json:"role"`
			Status            string                       `json:"status"`
			Current           bool                         `json:"current"`
			CheckpointCurrent bool                         `json:"checkpoint_current"`
			MetricsCurrent    bool                         `json:"metrics_current"`
			State             monitorEconomicNativeSummary `json:"state"`
			Issue             string                       `json:"issue,omitempty"`
		}{Schema: "urnetwork-mainnet-native-economic-event-v1", Role: self.policy.Role, Status: code,
			Current: current, CheckpointCurrent: checkpointErr == nil, MetricsCurrent: metricsErr == nil, State: self.summary(), Issue: issue}
		if err := json.NewEncoder(stdout).Encode(event); err != nil {
			if ctx.Err() != nil {
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
			if err := self.resume(ctx, hooks); err != nil {
				if monitorCanceledCheckpointLoad(ctx, err) {
					return 0
				}
				fmt.Fprintln(stderr, "native economic checkpoint continuation:", err)
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
