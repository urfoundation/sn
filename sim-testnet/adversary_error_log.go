// Bounded error-only diagnostics preserve transient actor failures that later
// successful samples would otherwise overwrite. Acceptance counters are unchanged.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

const (
	adversaryErrorLogFilename     = "adversary-errors.jsonl"
	adversaryErrorSummaryFilename = "adversary-errors.summary.json"
	adversaryErrorLogMaxRecords   = 2048
	adversaryErrorLogMaxBytes     = 8 * 1024 * 1024
	adversaryErrorDetailMaxBytes  = 2048
)

// The summary records dropped diagnostics without changing actor error totals.
type adversaryErrorSummary struct {
	Schema         string `json:"schema"`
	Recorded       uint64 `json:"recorded"`
	Omitted        uint64 `json:"omitted"`
	Bytes          int64  `json:"bytes"`
	MaximumRecords uint64 `json:"maximum_records"`
	MaximumBytes   int64  `json:"maximum_bytes"`
	WriteError     string `json:"write_error,omitempty"`
}

// Each entry identifies the exact sample and both active and expiring scopes.
type adversaryErrorObservation struct {
	Schema             string               `json:"schema"`
	ActorId            string               `json:"actor_id"`
	Sequence           uint64               `json:"sequence"`
	Phase              adversarySamplePhase `json:"phase"`
	SampledAt          string               `json:"sampled_at"`
	DurationMillis     int64                `json:"duration_milliseconds"`
	ErrorCount         uint64               `json:"error_count"`
	Requests           uint64               `json:"requests"`
	Detail             string               `json:"detail"`
	DetailTruncated    bool                 `json:"detail_truncated,omitempty"`
	ActiveFaultTargets []string             `json:"active_fault_targets,omitempty"`
	GraceFaultTargets  []string             `json:"grace_fault_targets,omitempty"`
}

// One file owner serializes writes outside the campaign state lock. Limits also
// apply after an interrupted pre-acceptance invocation reopens the same run.
type adversaryErrorLog struct {
	path       string
	secrets    []string
	maxRecords uint64
	maxBytes   int64
	stateLock  sync.Mutex
	recorded   uint64
	omitted    uint64
	byteCount  int64
	writeErr   error
}

// Reopening never truncates an earlier diagnostic or follows a substituted link.
func newAdversaryErrorLog(runDir string, secrets ...string) (*adversaryErrorLog, error) {
	log := &adversaryErrorLog{
		path: filepath.Join(runDir, adversaryErrorLogFilename), secrets: append([]string(nil), secrets...),
		maxRecords: adversaryErrorLogMaxRecords, maxBytes: adversaryErrorLogMaxBytes,
	}
	info, err := os.Lstat(log.path)
	if errors.Is(err, os.ErrNotExist) {
		return log, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > log.maxBytes {
		return nil, errors.New("adversary error chronology is not a bounded regular file")
	}
	data, err := os.ReadFile(log.path)
	if err != nil {
		return nil, err
	}
	if len(data) != 0 && data[len(data)-1] != '\n' {
		return nil, errors.New("adversary error chronology has an incomplete record")
	}
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var record adversaryErrorObservation
		if err := json.Unmarshal(line, &record); err != nil || record.Schema != "urnetwork-adversary-error-v1" || record.ActorId == "" {
			return nil, errors.New("adversary error chronology has an invalid record")
		}
		log.recorded++
	}
	if log.recorded > log.maxRecords {
		return nil, errors.New("adversary error chronology exceeds its record limit")
	}
	log.byteCount = int64(len(data))
	summaryPath := filepath.Join(runDir, adversaryErrorSummaryFilename)
	summaryInfo, err := os.Lstat(summaryPath)
	if err == nil {
		if !summaryInfo.Mode().IsRegular() || summaryInfo.Size() > 4096 {
			return nil, errors.New("adversary error summary is not a bounded regular file")
		}
		encoded, readErr := os.ReadFile(summaryPath)
		var summary adversaryErrorSummary
		if readErr != nil || json.Unmarshal(encoded, &summary) != nil || summary.Schema != "urnetwork-adversary-error-summary-v1" || summary.Recorded > log.recorded || summary.Bytes > log.byteCount || summary.Bytes < 0 {
			return nil, errors.New("adversary error summary is inconsistent with retained chronology")
		}
		log.omitted = summary.Omitted
		if summary.WriteError != "" {
			return nil, errors.New("adversary error chronology retains an earlier write failure")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return log, nil
}

// Diagnostics cannot grant attribution. Snapshot both sets for later analysis.
func (self *adversaryFaultWindow) diagnosticTargets() (active, grace []string) {
	if self == nil {
		return nil, nil
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	now := self.now()
	for target := range self.active {
		active = append(active, target)
	}
	for target, until := range self.grace {
		if until.After(now) {
			grace = append(grace, target)
		}
	}
	sort.Strings(active)
	sort.Strings(grace)
	return
}

// Redact before truncating, then append one complete record under the size cap.
func (self *adversaryErrorLog) append(record adversaryErrorObservation) {
	if self == nil {
		return
	}
	record.Schema = "urnetwork-adversary-error-v1"
	record.Detail = redactText(record.Detail, self.secrets...)
	if len(record.Detail) > adversaryErrorDetailMaxBytes {
		record.Detail = record.Detail[:adversaryErrorDetailMaxBytes]
		record.DetailTruncated = true
	}
	encoded, err := json.Marshal(record)
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err != nil {
		self.writeErr = errors.Join(self.writeErr, err)
		return
	}
	encoded = append(encoded, '\n')
	if self.writeErr != nil || self.recorded >= self.maxRecords || int64(len(encoded)) > self.maxBytes-self.byteCount {
		self.omitted++
		return
	}
	file, err := os.OpenFile(self.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		self.writeErr = err
		self.omitted++
		return
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != self.byteCount {
		self.writeErr = errors.Join(errors.New("adversary error chronology changed outside its writer"), err, file.Close())
		self.omitted++
		return
	}
	written, writeErr := file.Write(encoded)
	if writeErr == nil && written != len(encoded) {
		writeErr = io.ErrShortWrite
	}
	self.byteCount += int64(written)
	self.writeErr = errors.Join(writeErr, file.Close())
	if self.writeErr != nil {
		self.omitted++
		return
	}
	self.recorded++
}

// Report omissions explicitly; a diagnostic write failure must remain visible.
func (self *adversaryErrorLog) finish() error {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	summary := adversaryErrorSummary{Schema: "urnetwork-adversary-error-summary-v1", Recorded: self.recorded, Omitted: self.omitted, Bytes: self.byteCount, MaximumRecords: self.maxRecords, MaximumBytes: self.maxBytes}
	if self.writeErr != nil {
		summary.WriteError = self.writeErr.Error()
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err == nil {
		err = atomicWrite(filepath.Join(filepath.Dir(self.path), adversaryErrorSummaryFilename), append(encoded, '\n'), 0o600)
	}
	if err = errors.Join(self.writeErr, err); err != nil {
		return fmt.Errorf("persist adversary error chronology: %w", err)
	}
	return nil
}
