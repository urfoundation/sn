// Operator journal observations extend the existing monitor owners. The source
// is a read-only database snapshot, never a signer, receipt verifier or repairer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/server/v2026/stmonitor"
)

const maxMonitorOperators = 4

// The independently supplied source and age policy cannot be selected by DB rows.
// Credentials live separately in a private file and never enter retained output.
type monitorOperatorPolicy struct {
	Role                   string           `json:"role"`
	DatabaseFile           string           `json:"database_file"`
	DatabaseSha256         string           `json:"database_sha256"`
	ExpectedSource         stmonitor.Source `json:"expected_source"`
	PendingWarningSeconds  uint64           `json:"pending_warning_seconds"`
	PendingCriticalSeconds uint64           `json:"pending_critical_seconds"`
}

func (self monitorOperatorPolicy) validate(expected identityExpectation) error {
	if !validMonitorReadDigest(self.DatabaseSha256) || !monitorRolePattern.MatchString(self.Role) || self.ExpectedSource.Validate() != nil || self.ExpectedSource.ChainId != expected.EvmChainId || self.ExpectedSource.GenesisHash != strings.ToLower(expected.GenesisHash) || self.PendingWarningSeconds == 0 || self.PendingCriticalSeconds < self.PendingWarningSeconds || self.PendingCriticalSeconds > 86400 {
		return errors.New("operator policy identity or age thresholds are incomplete")
	}
	return nil
}

func monitorOperatorPaths(checkpoint, metrics, role string) (string, string) {
	return strings.TrimSuffix(checkpoint, ".json") + ".operator-" + role + ".json", strings.TrimSuffix(metrics, ".prom") + ".operator-" + role + ".prom"
}

// One current problem per fixed domain retains first/latest evidence through
// restart and read outages. Counts and IDs belong to this checkpoint lineage.
type monitorOperatorEpisode struct {
	FirstRecordHash string    `json:"first_record_hash,omitempty"`
	LastRecordHash  string    `json:"last_record_hash,omitempty"`
	Id              string    `json:"id"`
	Sequence        uint64    `json:"sequence"`
	FirstAt         time.Time `json:"first_at"`
	LastAt          time.Time `json:"last_at"`
	FirstCode       string    `json:"first_code"`
	LastCode        string    `json:"last_code"`
	Observations    uint64    `json:"observations"`
	RecoveredAt     time.Time `json:"recovered_at"`
	RecoveryHash    string    `json:"recovery_hash,omitempty"`
}

type monitorOperatorHistory struct {
	Count  uint64                  `json:"count"`
	First  *monitorOperatorEpisode `json:"first,omitempty"`
	Latest *monitorOperatorEpisode `json:"latest,omitempty"`
}

type monitorOperatorState struct {
	SampleAt                 time.Time              `json:"sample_at"`
	HighWaterAt              time.Time              `json:"high_water_at"`
	LastReadSuccessAt        time.Time              `json:"last_read_success_at"`
	ReadStatus               string                 `json:"read_status"`
	Record                   *stmonitor.Snapshot    `json:"record,omitempty"`
	ScanProgressAt           time.Time              `json:"scan_progress_at"`
	PublicationLastSuccessAt time.Time              `json:"publication_last_success_at"`
	Read                     monitorOperatorHistory `json:"read_incidents"`
	Scan                     monitorOperatorHistory `json:"scan_incidents"`
	Transactions             monitorOperatorHistory `json:"transaction_incidents"`
	Publications             monitorOperatorHistory `json:"publication_incidents"`
	current                  bool
}

type monitorOperatorCondition struct {
	Code     string `json:"code"`
	Severity string `json:"severity,omitempty"`
	Known    bool   `json:"known"`
	Problem  bool   `json:"problem"`
}

var monitorOperatorCodes = map[string]int{
	"starting": 0, "observed": 1, "unavailable": 2, "identity": 3, "invalid": 4, "capacity": 5, "clock": 6,
	"unknown": 7, "stalled": 8, "pending": 9, "aged": 10, "uncertain": 11, "failed": 12, "changed": 13,
}

// Original record ages and actual cursor movement remain distinct from reads.
func (self *monitorOperatorState) observe(now time.Time, policy monitorOperatorPolicy, value *stmonitor.Snapshot, code string, stallAfter time.Duration) error {
	self.SampleAt, self.current = now, false
	if self.HighWaterAt.After(now.Add(monitorServiceClockAllowance)) {
		code = "clock"
	}
	if now.After(self.HighWaterAt) {
		self.HighWaterAt = now
	}
	if value != nil {
		if value.Validate() != nil {
			code = "invalid"
		} else if !value.Source.Equal(policy.ExpectedSource) {
			code = "identity"
		}
		times := []time.Time{value.DatabaseAt}
		if value.Mirror != nil {
			times = append(times, value.Mirror.UpdatedAt)
		}
		for _, row := range []*stmonitor.Pending{value.OldestIntent, value.OldestPublication, value.LatestPublication} {
			if row != nil {
				times = append(times, row.CreatedAt, row.UpdatedAt)
			}
		}
		for _, at := range times {
			if at.After(now.Add(monitorServiceClockAllowance)) {
				code = "clock"
				if at.After(self.HighWaterAt) {
					self.HighWaterAt = at
				}
			}
		}
		if prior := self.Record; prior != nil {
			if value.DatabaseAt.Before(prior.DatabaseAt) {
				code = "changed"
			}
			if prior.Mirror != nil && (value.Mirror == nil || value.Mirror.NextBlock < prior.Mirror.NextBlock || value.Mirror.NextBlock == prior.Mirror.NextBlock && value.Mirror.BlockHash != prior.Mirror.BlockHash || value.Mirror.UpdatedAt.Before(prior.Mirror.UpdatedAt)) {
				code = "changed"
			}
			for _, pair := range [][2]*stmonitor.Pending{{prior.OldestIntent, value.OldestIntent}, {prior.OldestPublication, value.OldestPublication}} {
				if pair[0] != nil && pair[1] != nil && pair[0].Id == pair[1].Id && (!pair[0].CreatedAt.Equal(pair[1].CreatedAt) || pair[0].Account != pair[1].Account || pair[0].Nonce != pair[1].Nonce || pair[1].UpdatedAt.Before(pair[0].UpdatedAt)) {
					code = "changed"
				}
			}
		}
	}
	if code == "observed" && value == nil {
		code = "invalid"
	}
	self.ReadStatus = code
	if code == "observed" {
		if value.Mirror != nil && (self.Record == nil || self.Record.Mirror == nil || value.Mirror.NextBlock > self.Record.Mirror.NextBlock) {
			self.ScanProgressAt = now
		}
		self.Record, self.LastReadSuccessAt, self.current = value, now, true
	}
	conditions := self.conditions(now, policy, stallAfter)
	histories := []*monitorOperatorHistory{&self.Read, &self.Scan, &self.Transactions, &self.Publications}
	domains := []string{"read", "scan", "transactions", "publications"}
	for index, condition := range conditions {
		if err := histories[index].observe(policy, domains[index], now, condition, self.Record); err != nil {
			return err
		}
	}
	return nil
}

// Successful DB access can clear only the read incident. Unknown domains leave
// prior incidents open; an empty census never establishes canonical success.
func (self *monitorOperatorState) conditions(now time.Time, policy monitorOperatorPolicy, stallAfter time.Duration) [4]monitorOperatorCondition {
	unknown := monitorOperatorCondition{Code: "unknown"}
	result := [4]monitorOperatorCondition{unknown, unknown, unknown, unknown}
	if !self.current {
		code := self.ReadStatus
		if code == "observed" {
			code = "unknown"
		}
		severity := "warning"
		if code == "identity" || code == "invalid" || code == "changed" || code == "clock" || code == "capacity" {
			severity = "critical"
		}
		if self.Read.Latest != nil && (now.Before(self.Read.Latest.FirstAt) || now.Sub(self.Read.Latest.FirstAt) >= 5*time.Minute) {
			severity = "critical"
		}
		result[0] = monitorOperatorCondition{Code: code, Severity: severity, Known: code != "starting" && code != "unknown", Problem: true}
		return result
	}
	result[0] = monitorOperatorCondition{Code: "observed", Known: true}
	record := self.Record
	if record.Mirror != nil && record.Mirror.NextBlock > 0 {
		result[1] = monitorOperatorCondition{Code: "observed", Known: true}
		if self.ScanProgressAt.IsZero() || now.Sub(self.ScanProgressAt) >= stallAfter || now.Sub(record.Mirror.UpdatedAt) >= stallAfter || now.Before(self.ScanProgressAt) {
			result[1] = monitorOperatorCondition{Code: "stalled", Severity: "critical", Known: true, Problem: true}
		}
	}
	pending := func(count uint64, oldest *stmonitor.Pending) monitorOperatorCondition {
		if count == 0 {
			return monitorOperatorCondition{Code: "observed", Known: true}
		}
		value := monitorOperatorCondition{Code: "pending", Known: true}
		age := now.Sub(oldest.CreatedAt)
		if age >= time.Duration(policy.PendingWarningSeconds)*time.Second {
			value = monitorOperatorCondition{Code: "aged", Known: true, Problem: true, Severity: "warning"}
		}
		if age >= time.Duration(policy.PendingCriticalSeconds)*time.Second {
			value.Severity = "critical"
		}
		return value
	}
	result[2] = pending(record.PendingIntents, record.OldestIntent)
	if record.UncertainIntents > 0 {
		result[2] = monitorOperatorCondition{Code: "uncertain", Known: true, Problem: true, Severity: "critical"}
	}
	if record.FailedIntents > 0 {
		result[2] = monitorOperatorCondition{Code: "failed", Known: true, Problem: true, Severity: "critical"}
	}
	result[3] = pending(record.PendingPublications, record.OldestPublication)
	if record.LatestPublication != nil && record.LatestPublication.Status == "failed" {
		result[3] = monitorOperatorCondition{Code: "failed", Known: true, Problem: true, Severity: "critical"}
	}
	return result
}

// Stable identity contains independent source, domain, sequence and first fact.
func monitorOperatorIncidentId(policy monitorOperatorPolicy, domain string, value monitorOperatorEpisode) string {
	raw, _ := json.Marshal(struct {
		Kind            string
		Role            string
		Source          stmonitor.Source
		Domain          string
		Sequence        uint64
		At              time.Time
		Code            string
		FirstRecordHash string
	}{
		Kind: "urnetwork-mainnet-operator-incident-v1", Role: policy.Role, Source: policy.ExpectedSource, Domain: domain, Sequence: value.Sequence, At: value.FirstAt, Code: value.FirstCode, FirstRecordHash: value.FirstRecordHash,
	})
	return monitorReadDigest(raw)
}

func (self *monitorOperatorHistory) observe(policy monitorOperatorPolicy, domain string, now time.Time, condition monitorOperatorCondition, record *stmonitor.Snapshot) error {
	if !condition.Known {
		return nil
	}
	recordHash := ""
	if domain != "read" && record != nil {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		recordHash = monitorReadDigest(raw)
	}
	if condition.Problem {
		if self.Latest == nil || !self.Latest.RecoveredAt.IsZero() {
			if self.Count == math.MaxUint64 {
				return errors.New("operator incident sequence exhausted")
			}
			self.Count++
			value := monitorOperatorEpisode{Sequence: self.Count, FirstAt: now, FirstCode: condition.Code}
			value.FirstRecordHash = recordHash
			value.Id = monitorOperatorIncidentId(policy, domain, value)
			self.Latest = &value
		}
		value := *self.Latest
		value.LastAt, value.LastCode = now, condition.Code
		value.LastRecordHash = recordHash
		if value.Observations < math.MaxUint64 {
			value.Observations++
		}
		self.Latest = &value
	} else if self.Latest != nil && self.Latest.RecoveredAt.IsZero() {
		if record == nil {
			return errors.New("operator incident recovery has no accepted snapshot")
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		value := *self.Latest
		value.RecoveredAt, value.RecoveryHash = now, monitorReadDigest(raw)
		self.Latest = &value
	}
	if self.Count == 1 {
		self.First = self.Latest
	}
	return nil
}

// The checkpoint reuses the existing protected lock, atomic replace and sync
// owners. Operator data has its own schema and independently bound role source.
type monitorOperatorWorker struct {
	policy     monitorOperatorPolicy
	checkpoint *monitorServiceCheckpoint
	metrics    *monitorMetricsStore
	state      *monitorOperatorState
}

type monitorOperatorCheckpointRecord struct {
	Schema      string               `json:"schema"`
	Role        string               `json:"role"`
	Source      stmonitor.Source     `json:"source"`
	State       monitorOperatorState `json:"state"`
	ContentHash string               `json:"content_hash"`
}

func (self *monitorOperatorWorker) load(ctx context.Context) error {
	if err := self.checkpoint.validateOwner(); err != nil {
		return err
	}
	raw, err := readMonitorServiceFile(ctx, self.checkpoint.owner.path, 32*1024, true, monitorServiceReadHooks{})
	if err != nil {
		var readErr *monitorServiceReadError
		if errors.As(err, &readErr) && readErr.code == "missing" {
			self.state = &monitorOperatorState{ReadStatus: "starting"}
			return nil
		}
		return err
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	var record monitorOperatorCheckpointRecord
	if err := decodePlanJson(raw, &record); err != nil {
		return err
	}
	hash := record.ContentHash
	record.ContentHash = ""
	encoded, _ := json.Marshal(record)
	if record.Schema != "urnetwork-mainnet-operator-checkpoint-v1" || record.Role != self.policy.Role || !record.Source.Equal(self.policy.ExpectedSource) || hash != monitorReadDigest(encoded) {
		return errors.New("operator checkpoint identity or checksum differs")
	}
	if err := record.State.validate(self.policy); err != nil {
		return err
	}
	self.state = &record.State
	return nil
}

func (self *monitorOperatorState) validate(policy monitorOperatorPolicy) error {
	if self.SampleAt.IsZero() || self.HighWaterAt.IsZero() || self.ReadStatus == "starting" {
		return errors.New("operator checkpoint has no completed observation")
	}
	if _, ok := monitorOperatorCodes[self.ReadStatus]; !ok {
		return errors.New("operator checkpoint status is unknown")
	}
	if (self.Record == nil) != self.LastReadSuccessAt.IsZero() || self.ReadStatus == "observed" && self.Record == nil {
		return errors.New("operator checkpoint read evidence is incomplete")
	}
	if self.Record != nil {
		if !self.Record.Source.Equal(policy.ExpectedSource) {
			return errors.New("operator checkpoint source differs")
		}
		if err := self.Record.Validate(); err != nil {
			return err
		}
	}
	histories := []monitorOperatorHistory{self.Read, self.Scan, self.Transactions, self.Publications}
	domains := []string{"read", "scan", "transactions", "publications"}
	for index, history := range histories {
		if (history.Count == 0) != (history.First == nil) || (history.Count == 0) != (history.Latest == nil) {
			return errors.New("operator incident history is incomplete")
		}
		if history.Count == 0 {
			continue
		}
		if history.First.Sequence != 1 || history.Latest.Sequence != history.Count {
			return errors.New("operator incident sequence differs")
		}
		for _, value := range []*monitorOperatorEpisode{history.First, history.Latest} {
			if value.FirstAt.IsZero() || value.LastAt.IsZero() || value.Observations == 0 || monitorOperatorIncidentId(policy, domains[index], *value) != value.Id || value.RecoveredAt.IsZero() != (value.RecoveryHash == "") {
				return errors.New("operator incident evidence differs")
			}
			if _, ok := monitorOperatorCodes[value.FirstCode]; !ok {
				return errors.New("operator incident first status is unknown")
			}
			if _, ok := monitorOperatorCodes[value.LastCode]; !ok {
				return errors.New("operator incident last status is unknown")
			}
			if (value.FirstRecordHash != "" && !validMonitorReadDigest(value.FirstRecordHash)) || (value.LastRecordHash != "" && !validMonitorReadDigest(value.LastRecordHash)) || domains[index] != "read" && (value.FirstRecordHash == "" || value.LastRecordHash == "") {
				return errors.New("operator incident lost its snapshot evidence")
			}
			if value.RecoveryHash != "" && !validMonitorReadDigest(value.RecoveryHash) {
				return errors.New("operator incident recovery differs")
			}
		}
		if history.Count == 1 {
			first, _ := json.Marshal(history.First)
			last, _ := json.Marshal(history.Latest)
			if string(first) != string(last) {
				return errors.New("operator first incident differs")
			}
		}
	}
	return nil
}

func (self *monitorOperatorWorker) save() error {
	if err := self.checkpoint.validateOwner(); err != nil {
		return err
	}
	if err := self.state.validate(self.policy); err != nil {
		return err
	}
	record := monitorOperatorCheckpointRecord{Schema: "urnetwork-mainnet-operator-checkpoint-v1", Role: self.policy.Role, Source: self.policy.ExpectedSource, State: *self.state}
	raw, _ := json.Marshal(record)
	record.ContentHash = monitorReadDigest(raw)
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > 32*1024 {
		return errors.New("operator checkpoint exceeds its bound")
	}
	return publishMonitorFile(self.checkpoint.owner.path, append(raw, '\n'), 0600, self.checkpoint.owner.syncDirectory)
}

// Reading a protected credential file never loads the operator's broad vault.
func readMonitorOperator(ctx context.Context, policy monitorOperatorPolicy, hooks monitorServiceReadHooks) (*stmonitor.Snapshot, string) {
	raw, err := readMonitorServiceFile(ctx, policy.DatabaseFile, 8192, true, hooks)
	if err != nil {
		return nil, "unavailable"
	}
	if monitorReadDigest(raw) != policy.DatabaseSha256 {
		return nil, "identity"
	}
	value, err := stmonitor.Read(ctx, strings.TrimSpace(string(raw)), policy.ExpectedSource)
	if err != nil {
		var classified *stmonitor.ReadError
		if errors.As(err, &classified) {
			return nil, classified.Code
		}
		return nil, "unavailable"
	}
	return value, "observed"
}

// Fixed labels and explicit unknown capabilities prevent an empty DB census
// from being advertised as a healthy or fully reconciled mainnet deployment.
func renderMonitorOperatorMetrics(policy monitorOperatorPolicy, state *monitorOperatorState, publication string, checkpoint bool, stallAfter time.Duration) ([]byte, error) {
	var output strings.Builder
	gauge := func(name string, value any) {
		fmt.Fprintf(&output, "sn_mainnet_operator_%s{role=%q} %v\n", name, policy.Role, value)
	}
	bit := func(value bool) int {
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
	gauge("sample_timestamp_seconds", stamp(state.SampleAt))
	gauge("read_last_success_timestamp_seconds", stamp(state.LastReadSuccessAt))
	gauge("read_current", bit(state.current))
	gauge("checkpoint_current", bit(checkpoint))
	gauge("export_last_success_timestamp_seconds", stamp(state.PublicationLastSuccessAt))
	gauge("export_retrying", bit(publication == "retrying"))
	gauge("scan_progress_timestamp_seconds", stamp(state.ScanProgressAt))
	for _, name := range []string{"independent_rpc", "canonical_receipts_verified", "mirror_genesis_verified", "operator_authority_verified", "provider_readiness_known", "client_key_readiness_known", "liabilities_known", "protocol_deadlines_known", "root_validator_known", "repair_authorized"} {
		gauge(name, 0)
	}
	conditions := state.conditions(state.SampleAt, policy, stallAfter)
	histories := []monitorOperatorHistory{state.Read, state.Scan, state.Transactions, state.Publications}
	for index, domain := range []string{"read", "scan", "transactions", "publications"} {
		condition, history := conditions[index], histories[index]
		code, ok := monitorOperatorCodes[condition.Code]
		if !ok {
			return nil, errors.New("unknown operator metric condition")
		}
		severity := 0
		if condition.Severity == "warning" {
			severity = 1
		}
		if condition.Severity == "critical" {
			severity = 2
		}
		gauge(domain+"_status", code)
		gauge(domain+"_known", bit(condition.Known))
		gauge(domain+"_severity", severity)
		gauge(domain+"_incident_count", history.Count)
		open := history.Latest != nil && history.Latest.RecoveredAt.IsZero()
		gauge(domain+"_incident_open", bit(open))
		var first, last, recovered time.Time
		var observations uint64
		if history.Latest != nil {
			first, last, recovered, observations = history.Latest.FirstAt, history.Latest.LastAt, history.Latest.RecoveredAt, history.Latest.Observations
		}
		gauge(domain+"_incident_first_timestamp_seconds", stamp(first))
		gauge(domain+"_incident_last_timestamp_seconds", stamp(last))
		gauge(domain+"_incident_recovery_timestamp_seconds", stamp(recovered))
		gauge(domain+"_incident_observations", observations)
	}
	record := state.Record
	gauge("has_record", bit(record != nil))
	if record == nil {
		record = &stmonitor.Snapshot{}
	}
	gauge("pending_intents", record.PendingIntents)
	gauge("signed_attempts", record.SignedAttempts)
	gauge("uncertain_intents", record.UncertainIntents)
	gauge("failed_intents", record.FailedIntents)
	gauge("foreign_deployment_intents", record.ForeignDeploymentIntents)
	gauge("pending_publications", record.PendingPublications)
	var intentAt, publicationAt, mirrorAt time.Time
	var next uint64
	if record.OldestIntent != nil {
		intentAt = record.OldestIntent.CreatedAt
	}
	if record.OldestPublication != nil {
		publicationAt = record.OldestPublication.CreatedAt
	}
	if record.Mirror != nil {
		mirrorAt, next = record.Mirror.UpdatedAt, record.Mirror.NextBlock
	}
	gauge("oldest_intent_timestamp_seconds", stamp(intentAt))
	gauge("oldest_publication_timestamp_seconds", stamp(publicationAt))
	gauge("mirror_update_timestamp_seconds", stamp(mirrorAt))
	gauge("mirror_next_block", next)
	return []byte(output.String()), nil
}

func (self *monitorOperatorWorker) run(ctx context.Context, interval, stallAfter time.Duration, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) int {
	encoder := json.NewEncoder(stdout)
	publication := "starting"
	backoff := time.Second
	for ctx.Err() == nil {
		readHooks := monitorServiceReadHooks{}
		if hooks.read != nil {
			readHooks = hooks.read(self.policy.Role)
		}
		value, code := readMonitorOperator(ctx, self.policy, readHooks)
		if ctx.Err() != nil {
			return 0
		}
		sampledAt := now().UTC()
		if err := self.state.observe(sampledAt, self.policy, value, code, stallAfter); err != nil {
			return 3
		}
		checkpointErr := self.save()
		if checkpointErr != nil {
			publication = "retrying"
		}
		raw, err := renderMonitorOperatorMetrics(self.policy, self.state, publication, checkpointErr == nil, stallAfter)
		if err != nil {
			return 3
		}
		diagnostic := monitorDiagnosticSnapshot(stdout, stderr)
		raw = appendMonitorOutputMetrics(raw, "sn_mainnet_operator", self.policy.Role, diagnostic)
		metricsErr := self.metrics.saveRaw(raw)
		combined := errors.Join(checkpointErr, metricsErr)
		var ownership *monitorOutputOwnershipError
		terminal := errors.As(combined, &ownership)
		if combined == nil {
			publication = "published"
			self.state.PublicationLastSuccessAt = sampledAt
		} else {
			publication = "retrying"
		}
		event := struct {
			Schema                    string                        `json:"schema"`
			Role                      string                        `json:"role"`
			Publication               string                        `json:"publication"`
			State                     *monitorOperatorState         `json:"state"`
			IndependentRpc            bool                          `json:"independent_rpc"`
			CanonicalReceiptsVerified bool                          `json:"canonical_receipts_verified"`
			Diagnostics               *monitorDiagnosticObservation `json:"diagnostics,omitempty"`
			Conditions                [4]monitorOperatorCondition   `json:"conditions"`
		}{Schema: "urnetwork-mainnet-operator-event-v1", Role: self.policy.Role, Publication: publication, State: self.state, Diagnostics: diagnostic, Conditions: self.state.conditions(sampledAt, self.policy, stallAfter)}
		if err := encoder.Encode(event); err != nil {
			return 3
		}
		if terminal {
			return 3
		}
		delay := interval
		if combined != nil {
			delay = backoff
			backoff = min(2*backoff, time.Minute)
		} else {
			backoff = time.Second
		}
		if !waitMonitorService(ctx, self.policy.Role, delay, hooks) {
			return 0
		}
	}
	return 0
}

// Admission uses the same physical checkpoint owner and telemetry lock. No DB
// connection starts until every role's files have been admitted successfully.
func openMonitorOperatorWorker(ctx context.Context, policy monitorOperatorPolicy, expected identityExpectation, checkpointPath, metricsPath string, hooks monitorServiceHooks) (*monitorOperatorWorker, error) {
	checkpointPath, metricsPath = monitorOperatorPaths(checkpointPath, metricsPath, policy.Role)
	checkpoint, err := openMonitorServiceCheckpoint(checkpointPath, expected, monitorValidatorPolicy{})
	if err != nil {
		return nil, err
	}
	worker := &monitorOperatorWorker{policy: policy, checkpoint: checkpoint}
	metrics, err := openMonitorMetrics(metricsPath)
	if err != nil {
		return nil, errors.Join(err, checkpoint.owner.close())
	}
	worker.metrics = metrics
	if err := worker.load(ctx); err != nil {
		return nil, errors.Join(err, metrics.close(), checkpoint.owner.close())
	}
	if hooks.syncDirectory != nil {
		checkpoint.owner.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "checkpoint", file) }
		metrics.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "metrics", file) }
	}
	return worker, nil
}

// Helpers below keep path admission in the one service census implementation.
func monitorOperatorInputPaths(policy monitorOperatorPolicy, checkpoint, metrics string) []string {
	checkpoint, metrics = monitorOperatorPaths(checkpoint, metrics, policy.Role)
	return []string{policy.DatabaseFile, checkpoint, checkpoint + ".lock", metrics, metrics + ".lock"}
}
