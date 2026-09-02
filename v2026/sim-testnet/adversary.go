package main

// adversary.go provides the lifecycle and evidence model for adversarial
// actors which run continuously alongside sim-testnet's happy path. Actors
// are deliberately bounded and attributed: each sample is classified as a
// control or attack, all network calls pass through explicit request gates,
// and shared-testnet actors never mutate accounts or subnets outside this
// deployment.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	adversaryOutcomeSuccess           = "success"
	adversaryOutcomeExpectedRejection = "expected-rejection"
	adversaryOutcomeSkipped           = "skipped"
	adversaryOutcomeError             = "error"
)

var releaseAdversaryActorIDs = []string{
	"artifact-integrity-pressure",
	"consensus-cabal-emulation",
	"custody-boundary-emulation",
	"identity-churn-emulation",
	"operator-api-pressure",
	"rpc-consistency-pressure",
	"verify-replay-poison",
}

func adversaryMetricSet(names ...string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, name := range names {
		result[name] = true
	}
	return result
}

// releaseAdversaryMetricCatalog is a pre-launch capability declaration. It
// catches matrix rows which no mapped actor can possibly measure. The runtime
// vector gate independently requires an actual sample, so declaring a metric
// here can never manufacture passing evidence.
var releaseAdversaryMetricCatalog = map[string]map[string]bool{
	"operator-api-pressure": adversaryMetricSet(
		"scheduled_fault_rejections", "request_rate", "response_bytes", "5xx_count", "error_rate_ppm", "process_restarts",
	),
	"rpc-consistency-pressure": adversaryMetricSet(
		"finalized_head_lag_blocks", "finalized_lag_blocks", "head_lag_blocks", "hash_disagreement_count", "archive_error_rate_ppm", "rpc_latency_ms", "runtime_spec",
		"transaction_version",
		"best_finalized_lag_blocks", "sdk_mev_shield_expired_observations", "subnet_spot_alpha_price", "subnet_moving_alpha_price", "subnet_uid_count",
		"subnet_tao_reserve_rao", "subnet_alpha_reserve_rao", "spot_price", "moving_price", "tao_reserve_rao", "alpha_reserve_rao",
	),
	"artifact-integrity-pressure": adversaryMetricSet(
		"scheduled_fault_rejections", "missing_artifacts", "hash_mismatches", "origin_equivocations", "tamper_rejects", "artifact_tamper_rejections", "root_reproduction_mismatches",
	),
	"identity-churn-emulation": adversaryMetricSet(
		"commitment_parser_rejections", "canonical_commitment_accepts", "observer_panics", "stale_binding_rejects", "generation_monotonicity", "uid_rebind_rejects",
		"binding_generation", "prefix_claim_count", "duplicate_binding_rejects", "unresolved_affiliations", "burn_delta_rao", "uid_capacity", "registration_limit_rejects",
	),
	"custody-boundary-emulation": adversaryMetricSet(
		"allocation_sum_delta_rao", "domain_mutations_rejected", "domain_mismatch_rejects", "nonce_replays_rejected", "expired_signatures_rejected", "unit_boundary_cases", "budget_delta",
		"rounding_delta_rao", "maximum_leaves", "dense_index_entries", "live_root_coldkeys", "dead_index_entries", "dirty_destination_rejects", "claimed_watermark_delta", "future_owed_delta",
		"root_basket_proportional_claim_rao", "root_basket_remaining_reward_rao", "unclaimed_root_basket_rao", "proxy_stake_unshielded_loss_ppm", "proxy_stake_protected_rejection",
		"staking_execution_price_delta_ppm", "negative_flow_contribution", "forced_rollback_cases", "partial_state_deltas", "partial_writes", "false_paid_claims", "reserve_drift_rao",
		"migrated_fields", "missing_fields", "lock_mass_delta", "old_identity_residuals", "order_cases", "double_debit_rao", "overfill_rao", "zero_share_charges", "issuance_delta_rao",
		"migration_reserve_delta_rao", "dropped_emission_rao", "stale_flow_injection_rao", "bounded_items", "rejected_over_limit", "accepted_round_delta", "rejected_round_delta",
		"watermark_change_on_reject", "graph_nodes", "cycle_rejections", "empty_set_rejections", "maximum_traversal_nodes", "repatriated_alpha_rao", "repatriated_lock_rao",
		"residual_derived_rows", "value_delta_rao", "queued_lock_liability_rao", "escrow_backing_rao", "owner_unpriced_alpha_rao", "eviction_margin", "pending_emission_rao", "stranded_input_rao",
		"replay_rejects", "cross_no_rejects", "tier_snapshot_rate", "cap_remaining_rao", "custody_probe_rejects", "claim_availability", "keeper_delay_blocks", "same_no_carry_rao",
		"double_claim_rejects", "uncertain_claims", "terminal_holding_writeoffs", "healthy_holding_claims", "retryable_holding_preserved",
		"pending_basket_deposit_rao", "root_stake_change_rao", "pending_basket_stake_change_blocked",
		"settlement_transfer_floor_cases", "captured_subfloor_emission_rao", "premature_claim_payments", "lost_claim_credit_rao",
		"live_invalid_merkle_proof_rejections", "live_merkle_state_mutations",
	),
	"consensus-cabal-emulation": adversaryMetricSet(
		"consensus_delta_ppm", "honest_consensus_delta_ppm", "honest_incentive_delta_ppm", "follower_consensus_delta_ppm", "active_stake_ppm", "validator_permit_count",
		"threshold_margin_ppm", "honest_bond_ppm", "delayed_copier_bond_ppm", "dropout_reentry_bond_ppm", "continuous_validator_bond_ppm", "liquid_alpha_consensus_mode",
		"validator_live_count", "intent_recovery_seconds", "vector_hash_divergence", "pending_intents", "last_applied_epoch", "finalized_intents", "mask_coverage_ppm",
		"independent_validator_coverage", "unresolved_affiliations", "exact_split_error", "stake_sweep_ppm", "clipped_self_weight_ppm", "validator_trust_ppm",
		"adversary_validator_trust_ppm", "cabal_incentive_ppm",
	),
	"verify-replay-poison": adversaryMetricSet(
		"scheduled_fault_rejections", "signed_response_tamper_rejections", "canonical_body_mutation_rejections", "verified_final_responses", "constant_hash_collisions_accepted",
		"duplicate_hop_rejects", "source_mismatch_rejects", "path_id_collisions", "busy_responses", "replay_hash_mismatch", "assignment_confirmation_delta",
		"response_size_delta_bytes", "route_distinguishability_ppm", "poison_durable_rows", "stats_delta", "missing_signature_rejections", "invalid_signature_rejections",
		"unauthorized_trails_created", "requests_to_429", "vpk_count", "active_trails", "5xx_count",
	),
}

func validateAdversarialMetricCoverage(matrix *AdversarialMatrix, catalog map[string]map[string]bool) error {
	if matrix == nil {
		return errors.New("adversarial matrix is absent")
	}
	for _, row := range matrix.Rows {
		measurable := false
		for _, actorID := range row.ActorIDs {
			for _, metric := range row.Metrics {
				if catalog[actorID][metric] {
					measurable = true
				}
			}
		}
		if !measurable {
			return fmt.Errorf("adversarial matrix row %s has no metric emitted by its mapped actors", row.ID)
		}
	}
	return nil
}

type adversarySamplePhase string

const (
	adversaryControlPhase adversarySamplePhase = "control"
	adversaryAttackPhase  adversarySamplePhase = "attack"
)

type adversarySampleResult struct {
	Outcome     string
	Detail      string
	Requests    uint64
	MaxInFlight uint64
	Metrics     map[string]uint64
}

type adversaryActor interface {
	ID() string
	Sample(context.Context, adversarySamplePhase, uint64) adversarySampleResult
}

type AdversaryActorEvidence struct {
	ID                       string                             `json:"id"`
	VectorIDs                []string                           `json:"vector_ids"`
	StartedAt                string                             `json:"started_at"`
	StoppedAt                string                             `json:"stopped_at,omitempty"`
	Status                   string                             `json:"status"`
	Samples                  uint64                             `json:"samples"`
	ControlSamples           uint64                             `json:"control_samples"`
	AttackSamples            uint64                             `json:"attack_samples"`
	Successful               uint64                             `json:"successful"`
	ExpectedRejections       uint64                             `json:"expected_rejections"`
	Errors                   uint64                             `json:"errors"`
	Skipped                  uint64                             `json:"skipped"`
	Requests                 uint64                             `json:"requests"`
	MaximumInFlight          uint64                             `json:"maximum_in_flight"`
	ErrorRatePPM             uint32                             `json:"error_rate_ppm"`
	P50LatencyMilliseconds   int64                              `json:"p50_latency_milliseconds"`
	P95LatencyMilliseconds   int64                              `json:"p95_latency_milliseconds"`
	P99LatencyMilliseconds   int64                              `json:"p99_latency_milliseconds"`
	ControlP95Milliseconds   int64                              `json:"control_p95_latency_milliseconds"`
	AttackP95Milliseconds    int64                              `json:"attack_p95_latency_milliseconds"`
	AttackControlP95RatioPPM uint64                             `json:"attack_control_p95_ratio_ppm"`
	Metrics                  map[string]AdversaryMetricEvidence `json:"metrics,omitempty"`
	LastDetail               string                             `json:"last_detail,omitempty"`
}

// AdversaryMetricEvidence preserves bounded numeric observations across the
// full campaign instead of reporting only the actor's last sample.
type AdversaryMetricEvidence struct {
	Samples uint64 `json:"samples"`
	Minimum uint64 `json:"minimum"`
	Maximum uint64 `json:"maximum"`
	Last    uint64 `json:"last"`
}

// AdversaryVectorEvidence is the per-researched-vector projection of the
// concurrent actor measurements. Local-runtime-only vectors deliberately
// report sentinel-plus-release-test coverage rather than claiming that a
// chain-wide exploit was executed against the shared Bittensor testnet.
type AdversaryVectorEvidence struct {
	ID                            string   `json:"id"`
	Class                         string   `json:"class"`
	ExecutionMode                 string   `json:"execution_mode"`
	ConcurrentCoverage            string   `json:"concurrent_coverage"`
	ActorIDs                      []string `json:"actor_ids"`
	LocalTests                    []string `json:"local_tests"`
	RequiredMetrics               []string `json:"required_metrics"`
	MeasuredMetrics               []string `json:"measured_metrics"`
	Oracle                        string   `json:"oracle"`
	SampleFloor                   uint64   `json:"sample_floor"`
	Errors                        uint64   `json:"errors"`
	MaximumP99LatencyMilliseconds int64    `json:"maximum_p99_latency_milliseconds"`
	Status                        string   `json:"status"`
}

type AdversaryCampaignEvidence struct {
	Schema                    string                    `json:"schema"`
	Release                   string                    `json:"release"`
	Seed                      uint64                    `json:"seed"`
	MatrixHash                string                    `json:"matrix_hash"`
	StartedAt                 string                    `json:"started_at"`
	HappyPathStartedAt        string                    `json:"happy_path_started_at,omitempty"`
	HappyPathCompletedAt      string                    `json:"happy_path_completed_at,omitempty"`
	StoppedAt                 string                    `json:"stopped_at,omitempty"`
	StartedBeforeHappyPath    bool                      `json:"started_before_happy_path"`
	StoppedAfterHappyPath     bool                      `json:"stopped_after_happy_path"`
	MinimumSamplesPerActor    int                       `json:"minimum_samples_per_actor"`
	MaximumActorErrorRatePPM  uint32                    `json:"maximum_actor_error_rate_ppm"`
	MaximumP99Milliseconds    int                       `json:"maximum_p99_latency_milliseconds"`
	MaximumAttackControlRatio uint64                    `json:"maximum_attack_control_p95_ratio_ppm"`
	OperatorRequestCeilingQPS int                       `json:"operator_request_ceiling_qps"`
	RPCRequestCeilingQPS      int                       `json:"rpc_request_ceiling_qps"`
	Actors                    []AdversaryActorEvidence  `json:"actors"`
	Vectors                   []AdversaryVectorEvidence `json:"vectors"`
	Status                    string                    `json:"status"`
}

type adversaryActorState struct {
	evidence         AdversaryActorEvidence
	latencies        []int64
	controlLatencies []int64
	attackLatencies  []int64
}

type adversaryCampaign interface {
	Start(context.Context) error
	MarkHappyPathStarted(time.Time)
	MarkHappyPathCompleted(time.Time)
	SetExpectedFaultTargets([]string)
	Ready() bool
	Stop(context.Context) (*AdversaryCampaignEvidence, error)
	Snapshot() *AdversaryCampaignEvidence
}

type liveAdversaryCampaign struct {
	cfg    AdversaryConfig
	matrix *AdversarialMatrix
	actors []adversaryActor
	now    func() time.Time

	mu                   sync.Mutex
	states               map[string]*adversaryActorState
	startedAt            time.Time
	happyPathStartedAt   time.Time
	happyPathCompletedAt time.Time
	stoppedAt            time.Time
	cancel               context.CancelFunc
	wait                 chan struct{}
	started              bool
	stopping             bool
	stopped              bool
	faultWindow          *adversaryFaultWindow
}

func newAdversaryCampaign(cfg AdversaryConfig, matrix *AdversarialMatrix, actors []adversaryActor) (*liveAdversaryCampaign, error) {
	if !cfg.Enabled || matrix == nil || len(actors) == 0 {
		return nil, errors.New("continuous adversarial campaign is incomplete")
	}
	ids := make([]string, len(actors))
	for index, actor := range actors {
		if actor == nil {
			return nil, errors.New("continuous adversarial campaign has a nil actor")
		}
		ids[index] = actor.ID()
	}
	sort.Strings(ids)
	want := append([]string(nil), releaseAdversaryActorIDs...)
	sort.Strings(want)
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		return nil, fmt.Errorf("continuous adversarial actor set %v, want %v", ids, want)
	}
	if err := validateAdversarialActorCoverage(matrix, ids); err != nil {
		return nil, err
	}
	if err := validateAdversarialMetricCoverage(matrix, releaseAdversaryMetricCatalog); err != nil {
		return nil, err
	}
	var faultWindow *adversaryFaultWindow
	for _, actor := range actors {
		provider, ok := actor.(interface{ FaultWindow() *adversaryFaultWindow })
		if !ok || provider.FaultWindow() == nil {
			continue
		}
		if faultWindow != nil && faultWindow != provider.FaultWindow() {
			return nil, errors.New("continuous adversarial actors do not share one fault-attribution window")
		}
		faultWindow = provider.FaultWindow()
	}
	return &liveAdversaryCampaign{
		cfg: cfg, matrix: matrix, actors: actors, now: time.Now,
		states: map[string]*adversaryActorState{}, faultWindow: faultWindow,
	}, nil
}

func vectorIDsByActor(matrix *AdversarialMatrix) map[string][]string {
	result := map[string][]string{}
	if matrix == nil {
		return result
	}
	for _, row := range matrix.Rows {
		for _, actorID := range row.ActorIDs {
			result[actorID] = append(result[actorID], row.ID)
		}
	}
	for actorID := range result {
		sort.Strings(result[actorID])
	}
	return result
}

func (self *liveAdversaryCampaign) Start(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	self.mu.Lock()
	if self.started {
		if !self.stopping && !self.stopped {
			self.mu.Unlock()
			return nil
		}
		self.mu.Unlock()
		return errors.New("continuous adversarial campaign cannot restart after stop")
	}
	self.started = true
	self.startedAt = self.now().UTC()
	ctx, cancel := context.WithCancel(parent)
	self.cancel = cancel
	self.wait = make(chan struct{})
	vectors := vectorIDsByActor(self.matrix)
	ready := make(chan struct{}, len(self.actors))
	var workers sync.WaitGroup
	for _, actor := range self.actors {
		self.states[actor.ID()] = &adversaryActorState{evidence: AdversaryActorEvidence{
			ID: actor.ID(), VectorIDs: vectors[actor.ID()], StartedAt: self.startedAt.Format(time.RFC3339Nano), Status: "running",
		}}
		workers.Add(1)
		go self.runActor(ctx, &workers, ready, actor)
	}
	self.mu.Unlock()
	go func() {
		workers.Wait()
		close(self.wait)
	}()
	for range self.actors {
		select {
		case <-ready:
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		}
	}
	return nil
}

func (self *liveAdversaryCampaign) runActor(ctx context.Context, workers *sync.WaitGroup, ready chan<- struct{}, actor adversaryActor) {
	defer workers.Done()
	ready <- struct{}{}
	interval := time.Duration(self.cfg.SampleIntervalMilliseconds) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var sequence uint64
	for {
		phase := adversaryAttackPhase
		if sequence%5 == 0 {
			phase = adversaryControlPhase
		}
		started := self.now()
		sampleCtx, cancel := context.WithTimeout(ctx, time.Duration(self.cfg.RequestTimeoutMilliseconds)*time.Millisecond)
		result := actor.Sample(sampleCtx, phase, sequence)
		cancel()
		// Campaign cancellation deliberately interrupts an in-flight sample. It
		// is lifecycle cleanup, not an adversarial observation; recording it
		// would turn every otherwise healthy zero-error campaign into a teardown
		// failure. A sample-local timeout still records normally because its
		// parent remains live.
		if ctx.Err() != nil {
			return
		}
		self.record(actor.ID(), phase, self.now().Sub(started), result)
		sequence++
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (self *liveAdversaryCampaign) record(actorID string, phase adversarySamplePhase, duration time.Duration, result adversarySampleResult) {
	self.mu.Lock()
	defer self.mu.Unlock()
	state := self.states[actorID]
	if state == nil {
		return
	}
	evidence := &state.evidence
	evidence.LastDetail = result.Detail
	evidence.Requests += result.Requests
	if result.MaxInFlight > evidence.MaximumInFlight {
		evidence.MaximumInFlight = result.MaxInFlight
	}
	if evidence.Metrics == nil {
		evidence.Metrics = map[string]AdversaryMetricEvidence{}
	}
	for name, value := range result.Metrics {
		metric := evidence.Metrics[name]
		if metric.Samples == 0 || value < metric.Minimum {
			metric.Minimum = value
		}
		if metric.Samples == 0 || metric.Maximum < value {
			metric.Maximum = value
		}
		metric.Samples++
		metric.Last = value
		evidence.Metrics[name] = metric
	}
	if result.Outcome == adversaryOutcomeSkipped {
		evidence.Skipped++
		return
	}
	evidence.Samples++
	latency := duration.Milliseconds()
	state.latencies = append(state.latencies, latency)
	if phase == adversaryControlPhase {
		evidence.ControlSamples++
		state.controlLatencies = append(state.controlLatencies, latency)
	} else {
		evidence.AttackSamples++
		state.attackLatencies = append(state.attackLatencies, latency)
	}
	switch result.Outcome {
	case adversaryOutcomeSuccess:
		evidence.Successful++
	case adversaryOutcomeExpectedRejection:
		evidence.ExpectedRejections++
	default:
		evidence.Errors++
	}
}

func (self *liveAdversaryCampaign) MarkHappyPathStarted(at time.Time) {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.happyPathStartedAt.IsZero() {
		self.happyPathStartedAt = at.UTC()
	}
}

func (self *liveAdversaryCampaign) MarkHappyPathCompleted(at time.Time) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.happyPathCompletedAt = at.UTC()
}

func (self *liveAdversaryCampaign) SetExpectedFaultTargets(targets []string) {
	if self.faultWindow != nil {
		self.faultWindow.Update(targets)
	}
}

func (self *liveAdversaryCampaign) Ready() bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	if !self.started || len(self.states) != len(self.actors) {
		return false
	}
	for _, state := range self.states {
		actor := state.evidence
		if actor.Samples < uint64(self.cfg.MinimumSamplesPerActor) || actor.ControlSamples == 0 || actor.AttackSamples == 0 {
			return false
		}
	}
	return true
}

func (self *liveAdversaryCampaign) Stop(ctx context.Context) (*AdversaryCampaignEvidence, error) {
	self.mu.Lock()
	if !self.started {
		self.mu.Unlock()
		return nil, errors.New("continuous adversarial campaign was not started")
	}
	if self.stopped {
		evidence := self.snapshotLocked()
		self.mu.Unlock()
		return evidence, nil
	}
	if !self.stopping {
		self.stopping = true
		self.cancel()
	}
	cancel := self.cancel
	wait := self.wait
	self.mu.Unlock()
	// Keep the local copy live for an older in-memory campaign value while
	// making cancellation idempotent for concurrent Stop callers.
	cancel()
	select {
	case <-wait:
	case <-ctx.Done():
		return self.Snapshot(), ctx.Err()
	}
	self.mu.Lock()
	if !self.stopped {
		self.stopped = true
		self.stoppedAt = self.now().UTC()
		for _, state := range self.states {
			state.evidence.StoppedAt = self.stoppedAt.Format(time.RFC3339Nano)
			state.evidence.Status = "stopped"
		}
	}
	evidence := self.snapshotLocked()
	self.mu.Unlock()
	return evidence, nil
}

func latencyQuantile(values []int64, numerator, denominator int) int64 {
	if len(values) == 0 || numerator < 0 || denominator <= 0 || numerator > denominator {
		return 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := (len(ordered)*numerator + denominator - 1) / denominator
	if index > 0 {
		index--
	}
	return ordered[index]
}

func actorEvidenceSnapshot(state *adversaryActorState) AdversaryActorEvidence {
	evidence := state.evidence
	evidence.VectorIDs = append([]string(nil), state.evidence.VectorIDs...)
	if len(state.evidence.Metrics) != 0 {
		evidence.Metrics = make(map[string]AdversaryMetricEvidence, len(state.evidence.Metrics))
		for name, metric := range state.evidence.Metrics {
			evidence.Metrics[name] = metric
		}
	}
	evidence.P50LatencyMilliseconds = latencyQuantile(state.latencies, 50, 100)
	evidence.P95LatencyMilliseconds = latencyQuantile(state.latencies, 95, 100)
	evidence.P99LatencyMilliseconds = latencyQuantile(state.latencies, 99, 100)
	evidence.ControlP95Milliseconds = latencyQuantile(state.controlLatencies, 95, 100)
	evidence.AttackP95Milliseconds = latencyQuantile(state.attackLatencies, 95, 100)
	if evidence.Samples != 0 {
		evidence.ErrorRatePPM = uint32(evidence.Errors * 1_000_000 / evidence.Samples)
	}
	controlFloorMilliseconds := max64(1, uint64(evidence.ControlP95Milliseconds))
	evidence.AttackControlP95RatioPPM = uint64(evidence.AttackP95Milliseconds) * 1_000_000 / controlFloorMilliseconds
	return evidence
}

func adversaryCoverageForMode(mode string) string {
	switch mode {
	case "live-safe":
		return "live-exercised"
	case "bounded-emulation":
		return "continuous-emulation"
	case "observation-only":
		return "continuous-sentinel"
	case "local-runtime-only":
		return "continuous-sentinel-plus-release-test"
	default:
		return "invalid"
	}
}

func (self *liveAdversaryCampaign) vectorEvidenceLocked(actors map[string]AdversaryActorEvidence) []AdversaryVectorEvidence {
	vectors := make([]AdversaryVectorEvidence, 0, len(self.matrix.Rows))
	for _, row := range self.matrix.Rows {
		vector := AdversaryVectorEvidence{
			ID: row.ID, Class: row.Class, ExecutionMode: row.ExecutionMode,
			ConcurrentCoverage: adversaryCoverageForMode(row.ExecutionMode),
			ActorIDs:           append([]string(nil), row.ActorIDs...), LocalTests: append([]string(nil), row.LocalTests...), RequiredMetrics: append([]string(nil), row.Metrics...),
			Oracle: row.Oracle, Status: "running",
		}
		sort.Strings(vector.ActorIDs)
		sort.Strings(vector.LocalTests)
		sort.Strings(vector.RequiredMetrics)
		minimumSet := false
		healthy := true
		measured := map[string]bool{}
		for _, actorID := range vector.ActorIDs {
			actor, ok := actors[actorID]
			if !ok {
				healthy = false
				continue
			}
			if !minimumSet || actor.Samples < vector.SampleFloor {
				vector.SampleFloor = actor.Samples
				minimumSet = true
			}
			vector.Errors += actor.Errors
			if actor.P99LatencyMilliseconds > vector.MaximumP99LatencyMilliseconds {
				vector.MaximumP99LatencyMilliseconds = actor.P99LatencyMilliseconds
			}
			for _, metricName := range row.Metrics {
				if metric, ok := actor.Metrics[metricName]; ok && metric.Samples > 0 {
					measured[metricName] = true
				}
			}
			errorHealthy := actor.ErrorRatePPM <= self.cfg.MaximumActorErrorRatePPM && (self.cfg.MaximumActorErrorRatePPM != 0 || actor.Errors == 0)
			ratioHealthy := actor.AttackControlP95RatioPPM == 0 || actor.AttackControlP95RatioPPM <= self.cfg.MaximumAttackControlP95Ratio
			if actor.Status != "stopped" || actor.Samples < uint64(self.cfg.MinimumSamplesPerActor) || actor.ControlSamples == 0 || actor.AttackSamples == 0 || !errorHealthy || !ratioHealthy || actor.P99LatencyMilliseconds > int64(self.cfg.MaximumP99LatencyMilliseconds) {
				healthy = false
			}
		}
		for metricName := range measured {
			vector.MeasuredMetrics = append(vector.MeasuredMetrics, metricName)
		}
		sort.Strings(vector.MeasuredMetrics)
		if len(vector.MeasuredMetrics) == 0 {
			healthy = false
		}
		if self.stopped {
			vector.Status = "pass"
			if !healthy {
				vector.Status = "fail"
			}
		}
		vectors = append(vectors, vector)
	}
	sort.Slice(vectors, func(i, j int) bool { return vectors[i].ID < vectors[j].ID })
	return vectors
}

func (self *liveAdversaryCampaign) Snapshot() *AdversaryCampaignEvidence {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.snapshotLocked()
}

func (self *liveAdversaryCampaign) snapshotLocked() *AdversaryCampaignEvidence {
	if !self.started {
		return nil
	}
	evidence := &AdversaryCampaignEvidence{
		Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", Seed: self.cfg.Seed, MatrixHash: self.matrix.Hash,
		StartedAt: self.startedAt.Format(time.RFC3339Nano), MinimumSamplesPerActor: self.cfg.MinimumSamplesPerActor,
		MaximumActorErrorRatePPM: self.cfg.MaximumActorErrorRatePPM, MaximumP99Milliseconds: self.cfg.MaximumP99LatencyMilliseconds,
		MaximumAttackControlRatio: self.cfg.MaximumAttackControlP95Ratio,
		OperatorRequestCeilingQPS: self.cfg.MaximumOperatorRequestsPerSec, RPCRequestCeilingQPS: self.cfg.MaximumRPCRequestsPerSec,
		Status: "running",
	}
	if !self.happyPathStartedAt.IsZero() {
		evidence.HappyPathStartedAt = self.happyPathStartedAt.Format(time.RFC3339Nano)
		evidence.StartedBeforeHappyPath = !self.startedAt.After(self.happyPathStartedAt)
	}
	if !self.happyPathCompletedAt.IsZero() {
		evidence.HappyPathCompletedAt = self.happyPathCompletedAt.Format(time.RFC3339Nano)
	}
	if !self.stoppedAt.IsZero() {
		evidence.StoppedAt = self.stoppedAt.Format(time.RFC3339Nano)
		evidence.StoppedAfterHappyPath = !self.stoppedAt.Before(self.happyPathCompletedAt) && !self.happyPathCompletedAt.IsZero()
		evidence.Status = "stopped"
	}
	ids := make([]string, 0, len(self.states))
	for id := range self.states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	actors := map[string]AdversaryActorEvidence{}
	for _, id := range ids {
		actor := actorEvidenceSnapshot(self.states[id])
		evidence.Actors = append(evidence.Actors, actor)
		actors[id] = actor
	}
	evidence.Vectors = self.vectorEvidenceLocked(actors)
	return evidence
}

func adversaryAssertions(evidence *AdversaryCampaignEvidence, started time.Time, observationHash string) []AssertionRecord {
	now := time.Now().UTC()
	build := func(id string, passed bool, message string) AssertionRecord {
		return AssertionRecord{ID: id, Passed: passed, Message: message, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: observationHash}
	}
	if evidence == nil {
		return []AssertionRecord{build("adversary_campaign_evidence", false, "continuous adversarial evidence is absent")}
	}
	assertions := []AssertionRecord{
		build("adversary_matrix_coverage", evidence.MatrixHash != "" && len(evidence.Actors) == len(releaseAdversaryActorIDs) && len(evidence.Vectors) == len(requiredAdversarialVectors), fmt.Sprintf("matrix=%s actors=%d vectors=%d", evidence.MatrixHash, len(evidence.Actors), len(evidence.Vectors))),
		build("adversaries_overlap_happy_path", evidence.StartedBeforeHappyPath && evidence.StoppedAfterHappyPath, fmt.Sprintf("started_before=%t stopped_after=%t", evidence.StartedBeforeHappyPath, evidence.StoppedAfterHappyPath)),
	}
	seenVectors := map[string]bool{}
	for _, vector := range evidence.Vectors {
		seenVectors[vector.ID] = true
		passed := vector.Status == "pass" && vector.SampleFloor >= uint64(evidence.MinimumSamplesPerActor) && vector.ConcurrentCoverage != "invalid" && len(vector.ActorIDs) != 0 && len(vector.LocalTests) != 0 && len(vector.RequiredMetrics) != 0 && len(vector.MeasuredMetrics) != 0
		assertions = append(assertions, build("adversary_vector_"+vector.ID, passed, fmt.Sprintf("mode=%s coverage=%s status=%s sample_floor=%d errors=%d max_p99_ms=%d measured_metrics=%v", vector.ExecutionMode, vector.ConcurrentCoverage, vector.Status, vector.SampleFloor, vector.Errors, vector.MaximumP99LatencyMilliseconds, vector.MeasuredMetrics)))
	}
	liveMerklePassed := false
	liveMerkleMessage := "custody actor or live Merkle metrics are absent"
	for _, actor := range evidence.Actors {
		if actor.ID != "custody-boundary-emulation" {
			continue
		}
		rejections, rejected := actor.Metrics["live_invalid_merkle_proof_rejections"]
		mutations, measured := actor.Metrics["live_merkle_state_mutations"]
		liveMerklePassed = rejected && measured && rejections.Samples > 0 && rejections.Minimum >= 2 && mutations.Samples > 0 && mutations.Maximum == 0
		liveMerkleMessage = fmt.Sprintf("rejection_samples=%d rejection_min=%d mutation_samples=%d mutation_max=%d", rejections.Samples, rejections.Minimum, mutations.Samples, mutations.Maximum)
		break
	}
	assertions = append(assertions, build("adversary_live_invalid_merkle_proof", liveMerklePassed, liveMerkleMessage))
	for _, id := range requiredAdversarialVectors {
		if !seenVectors[id] {
			assertions = append(assertions, build("adversary_vector_"+id, false, "researched vector is absent from campaign evidence"))
		}
	}
	for _, actor := range evidence.Actors {
		minimum := actor.Samples >= uint64(evidence.MinimumSamplesPerActor) && actor.ControlSamples > 0 && actor.AttackSamples > 0
		errorHealthy := actor.ErrorRatePPM <= evidence.MaximumActorErrorRatePPM && (evidence.MaximumActorErrorRatePPM != 0 || actor.Errors == 0)
		ratioHealthy := actor.AttackControlP95RatioPPM == 0 || actor.AttackControlP95RatioPPM <= evidence.MaximumAttackControlRatio
		healthy := actor.Status == "stopped" && errorHealthy && ratioHealthy && actor.P99LatencyMilliseconds <= int64(evidence.MaximumP99Milliseconds)
		assertions = append(assertions,
			build("adversary_"+actor.ID+"_samples", minimum, fmt.Sprintf("samples=%d control=%d attack=%d skipped=%d minimum=%d", actor.Samples, actor.ControlSamples, actor.AttackSamples, actor.Skipped, evidence.MinimumSamplesPerActor)),
			build("adversary_"+actor.ID+"_resilience", healthy, fmt.Sprintf("status=%s errors_ppm=%d p99_ms=%d attack_control_p95_ratio_ppm=%d maximum_ratio_ppm=%d requests=%d max_in_flight=%d", actor.Status, actor.ErrorRatePPM, actor.P99LatencyMilliseconds, actor.AttackControlP95RatioPPM, evidence.MaximumAttackControlRatio, actor.Requests, actor.MaximumInFlight)),
		)
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	return assertions
}
