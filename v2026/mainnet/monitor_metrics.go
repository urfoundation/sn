// A single local monitor publishes bounded textfile gauges for the existing
// telemetry collector. External alert rules detect stale or absent samples.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Numeric statuses are fixed; identity strings, endpoints, hashes and errors
// remain in restricted event logs instead of generating metric labels.
var monitorStatusCodes = map[string]int{
	"starting": 0, "ok": 1, "rpc-error": 2, "finality-stalled": 3,
	"identity-mismatch": 4, "finality-conflict": 5, "rpc-integrity": 6,
	"checkpoint-error": 7, "metrics-error": 8,
}

// The command owns this store until its synchronous loop has returned. The
// readable metrics contain no secrets; only this process can own publication.
type monitorMetricsStore struct {
	path          string
	lock          *os.File
	directory     *monitorDirectory
	directoryInfo os.FileInfo
	syncDirectory func(*os.File) error
}

// A precreated directory may be group-readable for the collector, but it must
// not be writable by other users. No output directory or route is inferred.
func openMonitorMetrics(path string, contexts ...context.Context) (*monitorMetricsStore, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.HasSuffix(path, ".prom") {
		return nil, errors.New("metrics path must be an absolute canonical .prom file")
	}
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return nil, errors.Join(errors.New("metrics directory cannot traverse aliases"), err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return nil, errors.Join(errors.New("metrics directory must not be writable by other users"), err)
	}
	owner, err := openMonitorDirectory(directory, contexts)
	if err != nil {
		return nil, err
	}
	lock, err := owner.open(filepath.Base(path)+".lock", syscall.O_CREAT|syscall.O_RDWR, 0600)
	if err != nil {
		return nil, monitorAdmissionFailure(fmt.Errorf("open metrics lock: %w", err), owner.close())
	}
	self := &monitorMetricsStore{path: path, lock: lock, directory: owner, directoryInfo: info}
	opened, err := lock.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return nil, monitorAdmissionFailure(errors.Join(errors.New("metrics lock is not a private regular file"), err), self.close())
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, monitorAdmissionFailure(fmt.Errorf("metrics already has an owner: %w", err), self.close())
	}
	if err := self.validateDestination(); err != nil {
		return nil, monitorAdmissionFailure(err, self.close())
	}
	return self, nil
}

// Retain the final metrics on shutdown so an independent collector sees their
// age growing. Removal would erase useful last-state evidence.
func (self *monitorMetricsStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.lock.Close(), self.directory.close())
	self.lock = nil
	return err
}

// Restart cannot clear a retained incident or refresh old sample time before
// the next observation completes. Only an absent file gets a startup marker.
func (self *monitorMetricsStore) initialize(state *monitorState) error {
	if err := self.validateDestination(); err != nil {
		return err
	}
	if _, err := self.directory.stat(filepath.Base(self.path)); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return self.save(monitorEvent{Status: "starting"}, state)
}

// Reject a changed owner directory and special or aliased targets before each
// publication. This is local process ownership, not hostile-host attestation.
func (self *monitorMetricsStore) validateDestination() error {
	if self.lock == nil {
		return errors.New("metrics store is closed")
	}
	if err := self.directory.check(); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(self.path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 || !os.SameFile(info, self.directoryInfo) {
		return &monitorOutputOwnershipError{reason: "metrics directory changed"}
	}
	ownedLock, lockErr := self.lock.Stat()
	namedLock, nameErr := self.directory.stat(filepath.Base(self.path) + ".lock")
	if err := errors.Join(lockErr, nameErr); err != nil {
		return monitorNamedObservation(err)
	}
	if !namedLock.Mode().IsRegular() || namedLock.Mode().Perm()&0077 != 0 || !os.SameFile(ownedLock, namedLock) {
		return &monitorOutputOwnershipError{reason: "metrics lock changed"}
	}
	info, err = self.directory.stat(filepath.Base(self.path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return &monitorOutputOwnershipError{reason: "metrics destination is not a protected regular file"}
	}
	return nil
}

// Snapshot data stays finite regardless of the size or contents of an RPC error.
// Unknown states fail publication rather than silently turning into healthy zeroes.
func renderMonitorMetrics(event monitorEvent, state *monitorState) ([]byte, error) {
	status, known := monitorStatusCodes[event.Status]
	if !known || state == nil {
		return nil, errors.New("metrics event or state is incomplete")
	}
	severity := 0
	switch event.Severity {
	case "":
	case "warning":
		severity = 1
	case "critical":
		severity = 2
	default:
		return nil, errors.New("metrics event severity is unknown")
	}
	var observed time.Time
	if event.Status != "starting" {
		var err error
		observed, err = time.Parse(time.RFC3339Nano, event.ObservedAt)
		if err != nil || observed.IsZero() {
			return nil, errors.Join(errors.New("metrics sample time is invalid"), err)
		}
	}
	unix := func(value time.Time) int64 {
		if value.IsZero() {
			return 0
		}
		return value.Unix()
	}
	healthy, hasFinality, outage := 0, 0, 0
	if event.Status == "ok" {
		healthy = 1
	}
	if state.lastHash != "" {
		hasFinality = 1
	}
	var outageAge int64
	if !state.unavailableSince.IsZero() {
		outage = 1
		if !observed.IsZero() && observed.After(state.unavailableSince) {
			outageAge = int64(observed.Sub(state.unavailableSince) / time.Second)
		}
	}
	comparisonStatus, independentRpc, comparisonTime := 0, 0, int64(0)
	if comparison := event.RpcComparison; comparison != nil {
		switch comparison.Status {
		case "agreement":
			comparisonStatus = 1
		case "disagreement":
			comparisonStatus = 2
			healthy = 0
		}
		if comparison.IndependentRpc {
			independentRpc = 1
		}
		if timestamp, err := time.Parse(time.RFC3339Nano, comparison.ObservedAt); err == nil {
			comparisonTime = timestamp.Unix()
		}
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		help  string
		value string
	}{
		{name: "sample_timestamp_seconds", help: "Last completed monitor sample; zero before the first sample.", value: fmt.Sprint(unix(observed))},
		{name: "last_success_timestamp_seconds", help: "Last complete identity and continuity read; zero if unknown.", value: fmt.Sprint(unix(state.lastSuccessAt))},
		{name: "healthy", help: "One only for a complete healthy sample; sample freshness is required separately.", value: fmt.Sprint(healthy)},
		{name: "status", help: "0 starting, 1 healthy, 2 unavailable, 3 stalled, 4 identity, 5 finality, 6 integrity, 7 checkpoint, 8 metrics.", value: fmt.Sprint(status)},
		{name: "severity", help: "0 none, 1 warning, 2 critical; stale samples need independent alerts.", value: fmt.Sprint(severity)},
		{name: "has_finalized_evidence", help: "One only after a checked finalized position.", value: fmt.Sprint(hasFinality)},
		{name: "finalized_block", help: "Last checked native finalized height; use has_finalized_evidence.", value: fmt.Sprint(state.lastNumber)},
		{name: "finalized_progress_timestamp_seconds", help: "Last finalized advance; zero when unobserved.", value: fmt.Sprint(unix(state.lastProgressAt))},
		{name: "read_outage_active", help: "One during an unresolved read outage.", value: fmt.Sprint(outage)},
		{name: "read_outage_started_timestamp_seconds", help: "Start of the unresolved read outage; zero if none.", value: fmt.Sprint(unix(state.unavailableSince))},
		{name: "read_outage_age_seconds", help: "Outage age at the completed sample; freezes if the monitor stops.", value: fmt.Sprint(outageAge)},
		{name: "rpc_comparison_status", help: "0 input unknown, 1 scoped agreement, 2 disagreement; never activation authority.", value: fmt.Sprint(comparisonStatus)},
		{name: "independent_rpc", help: "One only after both separately admitted routes completed fixed-boundary comparison.", value: fmt.Sprint(independentRpc)},
		{name: "rpc_comparison_sample_timestamp_seconds", help: "Last completed independent-route comparison attempt; zero if absent.", value: fmt.Sprint(comparisonTime)},
	} {
		fmt.Fprintf(&output, "# HELP sn_mainnet_monitor_%s %s\n# TYPE sn_mainnet_monitor_%s gauge\nsn_mainnet_monitor_%s %s\n", metric.name, metric.help, metric.name, metric.name, metric.value)
	}
	return appendMonitorOutputMetrics([]byte(output.String()), "sn_mainnet_monitor", "", event.Diagnostics), nil
}

// Failed publication never refreshes the last successful read. A directory-sync
// error can follow a visible rename and remains a failure for the command owner.
func (self *monitorMetricsStore) save(event monitorEvent, state *monitorState) error {
	if err := self.validateDestination(); err != nil {
		return err
	}
	raw, err := renderMonitorMetrics(event, state)
	if err != nil {
		return err
	}
	return self.saveRaw(raw)
}

// Both chain and role metrics use the same bounded atomic textfile owner.
func (self *monitorMetricsStore) saveRaw(raw []byte) error {
	if len(raw) == 0 || len(raw) > 32*1024 {
		return errors.New("monitor metrics exceed their finite textfile bound")
	}
	if err := self.validateDestination(); err != nil {
		return err
	}
	return errors.Join(self.directory.publish(filepath.Base(self.path), raw, 0644, self.syncDirectory), self.validateDestination())
}
