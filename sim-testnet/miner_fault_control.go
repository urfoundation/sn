// Miner fault controls retain recovery intent across ambiguous local requests.
// A durable completion is telemetry; resumption always reconciles live state.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	minerControlRequestTimeout  = 5 * time.Second
	minerControlTargetTimeout   = 3 * time.Minute
	minerControlMaximumAttempts = 3
	minerControlMaximumPolls    = 4
	minerControlRetryDelay      = 250 * time.Millisecond
	minerControlMaximumBody     = 64 * 1024
	minerControlMaximumError    = 1024
)

// The exact fault specification owns one finite progress record. Applying and
// restoring are recovery intents, never evidence that the fault was activated.
type minerFaultControlProgress struct {
	FaultId        string                 `json:"fault_id"`
	FaultHash      string                 `json:"fault_hash"`
	Phase          string                 `json:"phase"`
	Total          int                    `json:"total"`
	CompletedCount int                    `json:"completed_count"`
	Completed      []FaultProcessEvidence `json:"completed,omitempty"`
	Pending        string                 `json:"pending,omitempty"`
	PendingTargets []string               `json:"pending_targets,omitempty"`
	Attempts       uint64                 `json:"attempts"`
	LastError      string                 `json:"last_error,omitempty"`
}

// Reject substituted targets, phase changes and orphan progress before any
// recovery request. Historical ledgers without progress remain recoverable.
func validateMinerFaultControlProgress(active activeFaultFile) error {
	if len(active.MinerControls) != 0 && active.Schema != "urnetwork-sim-active-faults-v2" && active.Schema != minerControlProgressSchema {
		return errors.New("miner control progress requires the versioned recovery ledger")
	}
	seen := map[string]bool{}
	for _, progress := range active.MinerControls {
		var spec *scenarioFaultSpec
		for index := range active.Faults {
			if active.Faults[index].ID == progress.FaultId {
				spec = &active.Faults[index]
				break
			}
		}
		if spec == nil || spec.Kind != "miner-control" || seen[progress.FaultId] {
			return errors.New("miner control progress has an unknown or duplicate fault")
		}
		seen[progress.FaultId] = true
		hash, err := canonicalHashHex(*spec)
		if err != nil || progress.FaultHash != hash || progress.Total != len(spec.Targets) || progress.CompletedCount != len(progress.Completed) || len(progress.LastError) > minerControlMaximumError {
			return errors.New("miner control progress differs from its exact fault specification")
		}
		if progress.Phase != "applying" && progress.Phase != "active" && progress.Phase != "restoring" {
			return errors.New("miner control progress has an invalid phase")
		}
		targets := map[string]bool{}
		for _, target := range spec.Targets {
			targets[target] = true
		}
		completed := map[string]bool{}
		for _, process := range progress.Completed {
			if !targets[process.ID] || completed[process.ID] || process.PID <= 1 || process.Role != "miner" || process.Identity == "" {
				return errors.New("miner control progress has invalid completed process evidence")
			}
			completed[process.ID] = true
		}
		if progress.Pending != "" && (!targets[progress.Pending] || completed[progress.Pending]) {
			return errors.New("miner control progress has an invalid pending target")
		}
		if active.Schema != minerControlProgressSchema && len(progress.PendingTargets) != 0 {
			return errors.New("parallel miner control progress has a legacy schema")
		}
		pending := map[string]bool{}
		for index, target := range progress.PendingTargets {
			if !targets[target] || pending[target] || completed[target] || index > 0 && progress.PendingTargets[index-1] >= target {
				return errors.New("miner control progress has invalid parallel pending targets")
			}
			pending[target] = true
		}
		if active.Schema == minerControlProgressSchema && (len(progress.PendingTargets) == 0 && progress.Pending != "" || len(progress.PendingTargets) != 0 && progress.Pending != progress.PendingTargets[0]) {
			return errors.New("miner control progress changed its pending target projection")
		}
		if progress.Phase == "active" && (progress.CompletedCount != progress.Total || progress.Pending != "" || progress.LastError != "") {
			return errors.New("active miner control progress is incomplete")
		}
	}
	return nil
}

// Cleanup has a separate owner from the failed observation. Give each pending
// member its bounded startup budget and retain time to reconcile completed ones.
func (self *liveScenarioFaultDriver) RecoveryTimeout() time.Duration {
	active, err := readActiveFaultFile(self.activePath())
	if err != nil {
		return 30 * time.Second
	}
	budget := 30 * time.Second
	for _, spec := range active.Faults {
		if spec.Kind != "miner-control" {
			continue
		}
		completed := 0
		for _, progress := range active.MinerControls {
			if progress.FaultId == spec.ID && progress.Phase == "restoring" {
				completed = progress.CompletedCount
			}
		}
		for index := range spec.Targets {
			if index < completed {
				budget += 2 * minerControlRequestTimeout
			} else {
				budget += minerControlTargetTimeout
			}
			if budget >= 30*time.Minute {
				return 30 * time.Minute
			}
		}
	}
	return budget
}

// Validate the complete census before the first request. A later malformed
// target must not leave an earlier member disabled without recovery evidence.
func (self *liveScenarioFaultDriver) minerControlProcesses(spec scenarioFaultSpec, enable bool) ([]FaultProcessEvidence, error) {
	if self.cfg == nil || spec.Kind != "miner-control" || len(spec.Targets) == 0 {
		return nil, fmt.Errorf("unsupported miner control fault %q", spec.Kind)
	}
	states, specs, err := self.processSnapshot()
	if err != nil {
		return nil, err
	}
	targets := append([]string(nil), spec.Targets...)
	sort.Strings(targets)
	processes := make([]FaultProcessEvidence, 0, len(targets))
	var unavailable []string
	for index, target := range targets {
		var miner int
		if _, err := fmt.Sscanf(target, "miner-%d", &miner); err != nil || target != fmt.Sprintf("miner-%d", miner) || (index > 0 && targets[index-1] == target) {
			return nil, fmt.Errorf("invalid miner control target %q", target)
		}
		swarm, err := minerSwarmFor(self.cfg, miner)
		if err != nil {
			return nil, err
		}
		processId := fmt.Sprintf("miner-swarm-%d", swarm)
		state, stateOk := states[processId]
		process, specOk := specs[processId]
		if !stateOk || !specOk || process.Role != "miner-swarm" || process.Identity == "" || state.Role != process.Role || state.Identity != process.Identity || state.PID < 0 || state.PID == 1 || state.Restarts < 0 {
			return nil, fmt.Errorf("miner control target %s has an invalid owning swarm", target)
		}
		processes = append(processes, FaultProcessEvidence{ID: target, Role: "miner", Identity: process.Identity, PID: state.PID})
		if state.PID == 0 || !enable && !state.Healthy {
			unavailable = append(unavailable, target)
		}
	}
	if len(unavailable) != 0 {
		return processes, &minerControlOwnerUnavailableError{targets: unavailable}
	}
	return processes, nil
}

// Keep the successful prefix after cancellation or a later failure. Durable
// intent and completion are batched by the coordinator, before later requests.
func (self *liveScenarioFaultDriver) controlMiners(ctx context.Context, spec scenarioFaultSpec, enable bool) ([]FaultProcessEvidence, error) {
	processes, err := self.waitMinerControlProcesses(ctx, spec, enable)
	if err != nil {
		return nil, err
	}
	active, err := readActiveFaultFile(self.activePath())
	if err != nil {
		return nil, err
	}
	if _, err := activeFaultIndex(active, spec); err != nil {
		return nil, err
	}
	index := -1
	for candidate := range active.MinerControls {
		if active.MinerControls[candidate].FaultId == spec.ID {
			index = candidate
			break
		}
	}
	if index == -1 {
		if !enable {
			return nil, errors.New("miner disable has no durable activation intent")
		}
		hash, err := canonicalHashHex(spec)
		if err != nil {
			return nil, err
		}
		active.MinerControls = append(active.MinerControls, minerFaultControlProgress{FaultId: spec.ID, FaultHash: hash, Phase: "restoring", Total: len(processes)})
		index = len(active.MinerControls) - 1
	}
	progress := &active.MinerControls[index]
	upgradeMinerControlProgress(&active)
	if enable && progress.Phase != "restoring" {
		progress.Phase = "restoring"
		progress.Completed = nil
		progress.CompletedCount = 0
		progress.Pending = ""
		progress.PendingTargets = nil
		progress.Attempts = 0
		progress.LastError = ""
	}
	if !enable && progress.Phase != "applying" {
		return nil, errors.New("miner disable cannot reuse completed or restoring fault intent")
	}
	persist := func() error {
		progress.CompletedCount = len(progress.Completed)
		if err := validateMinerFaultControlProgress(active); err != nil {
			return err
		}
		raw, err := json.MarshalIndent(active, "", "  ")
		if err != nil {
			return err
		}
		if self.minerControlPersist != nil {
			return self.minerControlPersist(self.activePath(), append(raw, '\n'))
		}
		return atomicWrite(self.activePath(), append(raw, '\n'), 0o600)
	}
	if err := persist(); err != nil {
		return processes, err
	}
	action := "disable"
	if enable {
		action = "enable"
	}
	if err := self.controlMinerRound(ctx, processes, action, progress, persist); err != nil {
		return processes, err
	}
	if self.minerControlHead != nil {
		headCtx, cancel := context.WithTimeout(ctx, minerControlRequestTimeout)
		head, err := self.minerControlHead(headCtx)
		cancel()
		if err != nil {
			if ctx.Err() == nil && minerControlTransientError(err) {
				return processes, &minerControlPendingError{cause: err}
			}
			return processes, err
		}
		if head.Number == 0 || !validCanonicalHashHex(head.Hash) {
			return processes, errors.New("miner control completion has an invalid finalized head")
		}
		self.minerControlCompleted = minerControlCompletedTransition{faultId: spec.ID, action: action, head: head}
	}
	if !enable {
		progress.Phase = "active"
		for index := range active.Processes {
			for _, process := range processes {
				if active.Processes[index].ID == process.ID {
					active.Processes[index] = process
				}
			}
		}
		if err := persist(); err != nil {
			return processes, err
		}
	}
	delete(self.minerControlReconciled, progress.FaultHash+":"+action)
	return processes, nil
}

// Status failures retain their typed origin so text containing "timeout" can
// never authorize another mutation.
type minerControlHttpError struct {
	status int
	detail string
}

func (self *minerControlHttpError) Error() string {
	return fmt.Sprintf("miner control HTTP %d: %s", self.status, self.detail)
}

// Decoding is a semantic boundary even when its cause is EOF. Only the body
// reader, before decoding, can classify an incomplete transport as retryable.
type minerControlInvalidStatusError struct{ cause error }

func (self *minerControlInvalidStatusError) Error() string {
	return fmt.Sprintf("invalid miner control status: %v", self.cause)
}

func (self *minerControlInvalidStatusError) Unwrap() error { return self.cause }

func minerControlTransientError(err error) bool {
	var invalid *minerControlInvalidStatusError
	if errors.As(err, &invalid) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !minerControlTransientError(cause) {
				return false
			}
		}
		return true
	}
	var response *minerControlHttpError
	if errors.As(err, &response) {
		switch response.status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	return scenarioSnapshotTransportError(err, true)
}

// A request owns its response through close, with no redirects to a different
// control origin. Transport retries remain separate from semantic validation.
func (self *liveScenarioFaultDriver) minerControlRequest(ctx context.Context, method, endpoint string) ([]byte, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, minerControlRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, method, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	client := *http.DefaultClient
	if self.minerControlClient != nil {
		client = *self.minerControlClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	raw, err := readEvidenceHttpBody(requestCtx, response.Body, minerControlMaximumBody)
	return raw, response.StatusCode, err
}

// The legacy status provides a conservative fallback for already-running
// swarms. Readiness counts only prove enabled completion when all enabled
// members are ready; disabled publication alone does not prove teardown ended.
type minerControlSwarmStatus struct {
	Schema     string            `json:"schema"`
	Configured int               `json:"configured"`
	Running    int               `json:"running"`
	Disabled   []string          `json:"disabled,omitempty"`
	Failures   map[string]string `json:"failures,omitempty"`
}

func (self *liveScenarioFaultDriver) decodeMinerControlSwarmStatus(raw []byte, swarm int) (minerControlSwarmStatus, error) {
	var status minerControlSwarmStatus
	var wire struct {
		Schema     string            `json:"schema"`
		Configured *int              `json:"configured"`
		Running    *int              `json:"running"`
		Disabled   []string          `json:"disabled,omitempty"`
		Failures   map[string]string `json:"failures,omitempty"`
	}
	if err := decodeStrictJSONBytes(raw, &wire); err != nil {
		return status, &minerControlInvalidStatusError{cause: err}
	}
	if wire.Configured == nil || wire.Running == nil {
		return status, &minerControlInvalidStatusError{cause: errors.New("miner control status omits its required population")}
	}
	status = minerControlSwarmStatus{Schema: wire.Schema, Configured: *wire.Configured, Running: *wire.Running, Disabled: wire.Disabled, Failures: wire.Failures}
	want := self.cfg.Config.Topology.Miners / self.cfg.Config.Topology.MinerSwarmProcesses
	if status.Schema != "urnetwork-provider-swarm-v1" || status.Configured != want || status.Running < 0 || status.Running > want-len(status.Disabled) {
		return status, &minerControlInvalidStatusError{cause: errors.New("miner control status has invalid schema or population")}
	}
	seen := map[string]bool{}
	validateId := func(id string) bool {
		var miner int
		if _, err := fmt.Sscanf(id, "miner-%d", &miner); err != nil || id != fmt.Sprintf("miner-%d", miner) {
			return false
		}
		owner, err := minerSwarmFor(self.cfg, miner)
		return err == nil && owner == swarm
	}
	for _, id := range status.Disabled {
		if !validateId(id) || seen[id] {
			return status, &minerControlInvalidStatusError{cause: errors.New("miner control status has invalid disabled identities")}
		}
		seen[id] = true
	}
	for id := range status.Failures {
		if !validateId(id) {
			return status, &minerControlInvalidStatusError{cause: errors.New("miner control status has a foreign failed identity")}
		}
	}
	return status, nil
}

// A member state describes lifecycle completion independently of unrelated
// carrier readiness. Legacy observations are marked because teardown is weaker.
type minerControlObservation struct {
	Schema  string `json:"schema"`
	Id      string `json:"id"`
	State   string `json:"state"`
	Failure string `json:"failure,omitempty"`
	legacy  bool
}

func (self *liveScenarioFaultDriver) observeMinerControl(ctx context.Context, swarm int, target string) (minerControlObservation, error) {
	endpoint := self.controlURL(swarm, target, "status")
	raw, code, err := self.minerControlRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return minerControlObservation{}, err
	}
	if code == http.StatusOK {
		var observation minerControlObservation
		if err := decodeStrictJSONBytes(raw, &observation); err != nil {
			return observation, &minerControlInvalidStatusError{cause: err}
		}
		if observation.Schema != "urnetwork-provider-swarm-member-v1" || observation.Id != target {
			return observation, &minerControlInvalidStatusError{cause: errors.New("miner control member status has a different identity")}
		}
		if observation.State == "running" && observation.Failure != "" {
			return observation, &minerControlInvalidStatusError{cause: errors.New("miner control running status contains a failure")}
		}
		switch observation.State {
		case "running", "disabled", "starting", "stopping", "failed":
			return observation, nil
		default:
			return observation, &minerControlInvalidStatusError{cause: errors.New("miner control member status has an unknown lifecycle state")}
		}
	}
	if code != http.StatusNotFound {
		return minerControlObservation{}, &minerControlHttpError{status: code, detail: strings.TrimSpace(string(raw))}
	}
	legacyUrl, err := url.Parse(endpoint)
	if err != nil {
		return minerControlObservation{}, err
	}
	legacyUrl.Path, legacyUrl.RawPath, legacyUrl.RawQuery, legacyUrl.Fragment = "/status", "", "", ""
	raw, code, err = self.minerControlRequest(ctx, http.MethodGet, legacyUrl.String())
	if err != nil {
		return minerControlObservation{}, err
	}
	if code != http.StatusOK && code != http.StatusServiceUnavailable {
		return minerControlObservation{}, &minerControlHttpError{status: code, detail: strings.TrimSpace(string(raw))}
	}
	status, err := self.decodeMinerControlSwarmStatus(raw, swarm)
	if err != nil {
		return minerControlObservation{}, err
	}
	observation := minerControlObservation{Id: target, State: "starting", legacy: true}
	for _, id := range status.Disabled {
		if id == target {
			observation.State = "disabled"
			return observation, nil
		}
	}
	if status.Running == status.Configured-len(status.Disabled) && len(status.Failures) == 0 {
		observation.State = "running"
	}
	return observation, nil
}

// Retry only the same idempotent action after observing its lifecycle. A
// pending startup or teardown is polled without issuing another operation.
func (self *liveScenarioFaultDriver) controlMiner(ctx context.Context, swarm int, target, action string, before func() error, recordFailure func(error) error) error {
	desired := "disabled"
	if action == "enable" {
		desired = "running"
	}
	wait := self.minerControlWait
	if wait == nil {
		wait = waitSupervisorRestart
	}
	attempts, observationFailures := 0, 0
	var conflict, lastFailure error
	for poll := 0; poll < minerControlMaximumPolls; poll++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		observation, err := self.observeMinerControl(ctx, swarm, target)
		if err != nil {
			lastFailure = err
			if saveErr := recordFailure(err); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			observationFailures++
			if !minerControlTransientError(err) || observationFailures >= minerControlMaximumAttempts {
				return err
			}
		} else {
			observationFailures = 0
			if observation.State == desired {
				if !observation.legacy || action == "enable" {
					return nil
				}
				if attempts != 0 {
					return errors.New("legacy swarm cannot confirm teardown after an ambiguous disable")
				}
			}
			pending := observation.State == "starting" || observation.State == "stopping"
			if !pending {
				if conflict != nil {
					return conflict
				}
				if attempts >= minerControlMaximumAttempts {
					return fmt.Errorf("miner control transient retry budget exhausted: %w", lastFailure)
				}
				if err := before(); err != nil {
					return err
				}
				attempts++
				raw, code, requestErr := self.minerControlRequest(ctx, http.MethodPost, self.controlURL(swarm, target, action))
				if requestErr == nil && code == http.StatusOK {
					status, err := self.decodeMinerControlSwarmStatus(raw, swarm)
					if err != nil {
						return err
					}
					disabled := false
					for _, id := range status.Disabled {
						disabled = disabled || id == target
					}
					if disabled != (action == "disable") || status.Failures[target] != "" {
						return &minerControlInvalidStatusError{cause: errors.New("miner control success response does not confirm the requested state")}
					}
					return nil
				}
				if requestErr == nil {
					requestErr = &minerControlHttpError{status: code, detail: strings.TrimSpace(string(raw))}
				}
				lastFailure = requestErr
				if saveErr := recordFailure(requestErr); saveErr != nil {
					return errors.Join(requestErr, saveErr)
				}
				if code == http.StatusConflict {
					conflict = requestErr
				} else if !minerControlTransientError(requestErr) {
					return requestErr
				}
			}
		}
		if err := wait(ctx, minerControlRetryDelay); err != nil {
			return err
		}
	}
	return errMinerControlLifecyclePending
}
