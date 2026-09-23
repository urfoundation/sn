// Storage and request delays retain exact fault intent without turning a
// heartbeat deadline into terminal failure. Malformed evidence remains hard.
package main

import (
	"errors"
	"sort"
	"syscall"
)

var errMinerControlLifecyclePending = errors.New("miner control lifecycle remains pending after bounded observations")

// A failed checkpoint may join transport, storage and cancellation causes.
// Every leaf must be recoverable; one semantic error keeps the result hard.
func minerControlRetryable(err error, canceled bool) bool {
	if err == nil {
		return false
	}
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
			if !minerControlRetryable(cause, canceled) {
				return false
			}
		}
		return true
	}
	if minerControlRoundRetryable(err, canceled) || minerControlStorageRetryable(err) {
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return minerControlRetryable(wrapped.Unwrap(), canceled)
	}
	return false
}

// Match every leaf so a joined integrity error cannot inherit retry authority
// from a timeout. These storage failures can recover without changing intent.
func minerControlStorageRetryable(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !minerControlStorageRetryable(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return minerControlStorageRetryable(wrapped.Unwrap())
	}
	return err == syscall.EINTR || err == syscall.EAGAIN || err == syscall.EBUSY || err == syscall.ETIMEDOUT || err == syscall.ENOSPC || err == syscall.EDQUOT || err == syscall.EIO
}

// A returned complete census or the authenticated persisted fault supplies
// pending attribution. No request is permitted by this recovery classification.
func (self *liveScenarioFaultDriver) minerControlResult(spec scenarioFaultSpec, processes []FaultProcessEvidence, err error) ([]FaultProcessEvidence, error) {
	if spec.Kind != "miner-control" || err == nil || minerControlPending(err) {
		return processes, err
	}
	if !minerControlRetryable(err, true) {
		return processes, err
	}
	targets := make(map[string]bool, len(spec.Targets))
	for _, target := range spec.Targets {
		targets[target] = true
	}
	if len(processes) == 0 {
		active, readErr := readActiveFaultFile(self.activePath())
		if readErr != nil {
			return processes, errors.Join(err, readErr)
		}
		if _, exactErr := activeFaultIndex(active, spec); exactErr != nil {
			return processes, err
		}
		processes = active.Processes
	}
	retained := make([]FaultProcessEvidence, 0, len(targets))
	for _, process := range processes {
		if targets[process.ID] && process.Role == "miner" && process.PID > 1 && process.Identity != "" {
			retained = append(retained, process)
			delete(targets, process.ID)
		}
	}
	if len(targets) != 0 || len(retained) != len(spec.Targets) {
		return processes, err
	}
	sort.Slice(retained, func(i, j int) bool { return retained[i].ID < retained[j].ID })
	return retained, &minerControlPendingError{cause: err}
}
