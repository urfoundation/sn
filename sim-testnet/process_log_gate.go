package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	processLogGateSchema        = "urnetwork-sim-process-log-gate-v1"
	processLogClassifierVersion = "urnetwork-sim-process-log-classifier-v2"
	processLogGateStateFilename = "process-log-gate.json"
	processLogEvidenceFilename  = "process-logs.json"
	processLogMaximumLineBytes  = 1024 * 1024
	processLogMaximumScanBytes  = 64 * 1024 * 1024
	processLogDigestChunkBytes  = 64 * 1024
	processLogDigestDomain      = "urnetwork-sim-process-log-chunk-v1"
)

// ProcessLogFinding is safe to publish: it identifies the fixed release rule
// and source offsets without copying arbitrary process output into evidence.
// The private append-only process logs retain the exact line for diagnosis.
type ProcessLogFinding struct {
	ProcessID       string   `json:"process_id"`
	Role            string   `json:"role"`
	Stream          string   `json:"stream"`
	Class           string   `json:"class"`
	Summary         string   `json:"summary"`
	Blocking        bool     `json:"blocking"`
	Disposition     string   `json:"disposition"`
	FaultIDs        []string `json:"fault_ids,omitempty"`
	FaultKinds      []string `json:"fault_kinds,omitempty"`
	Count           uint64   `json:"count"`
	FirstOffset     int64    `json:"first_offset"`
	LastOffset      int64    `json:"last_offset"`
	FirstLineSHA256 string   `json:"first_line_sha256,omitempty"`
	LastLineSHA256  string   `json:"last_line_sha256,omitempty"`
	FirstObservedAt string   `json:"first_observed_at"`
	LastObservedAt  string   `json:"last_observed_at"`
}

type processLogCursor struct {
	ProcessID     string `json:"process_id"`
	Role          string `json:"role"`
	Stream        string `json:"stream"`
	Path          string `json:"path"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	InitialOffset int64  `json:"initial_offset"`
	Offset        int64  `json:"offset"`
	DigestOffset  int64  `json:"digest_offset"`
	ScannedBytes  uint64 `json:"scanned_bytes"`
	ScannedLines  uint64 `json:"scanned_lines"`
	ChunkCount    uint64 `json:"chunk_count"`
	ChunkChain    string `json:"scanned_chunk_chain_sha256"`
}

type processLogGateState struct {
	Schema                   string              `json:"schema"`
	Classifier               string              `json:"classifier"`
	DeploymentID             string              `json:"deployment_id"`
	ManifestHash             string              `json:"manifest_hash"`
	SupervisorPID            int                 `json:"supervisor_pid,omitempty"`
	SupervisorStartTimeTicks uint64              `json:"supervisor_start_time_ticks,omitempty"`
	GeneratedAt              string              `json:"generated_at"`
	UpdatedAt                string              `json:"updated_at"`
	Cursors                  []processLogCursor  `json:"cursors"`
	Findings                 []ProcessLogFinding `json:"findings"`
}

type processLogScanResult struct {
	Findings []ProcessLogFinding
}

type processLogFaultScope struct {
	ID      string
	Kind    string
	Targets []string
}

func activeProcessLogFaultScopes(records []ScenarioFaultRecord) []processLogFaultScope {
	scopes := make([]processLogFaultScope, 0, len(records))
	for _, record := range records {
		if record.Status != "active" || record.AppliedBlock == 0 {
			continue
		}
		targetSet := map[string]bool{}
		for _, target := range append(append([]string(nil), record.Targets...), record.Impacts...) {
			if target != "" {
				targetSet[target] = true
			}
		}
		targets := make([]string, 0, len(targetSet))
		for target := range targetSet {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		scopes = append(scopes, processLogFaultScope{ID: record.ID, Kind: record.Kind, Targets: targets})
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].ID < scopes[j].ID })
	return scopes
}

func mergeProcessLogFaultScopes(groups ...[]processLogFaultScope) []processLogFaultScope {
	targetsByFaultID := map[string]map[string]bool{}
	kindByFaultID := map[string]string{}
	for _, scopes := range groups {
		for _, scope := range scopes {
			if scope.ID == "" || scope.Kind == "" {
				continue
			}
			kindByFaultID[scope.ID] = scope.Kind
			if targetsByFaultID[scope.ID] == nil {
				targetsByFaultID[scope.ID] = map[string]bool{}
			}
			for _, target := range scope.Targets {
				targetsByFaultID[scope.ID][target] = true
			}
		}
	}
	faultIDs := make([]string, 0, len(targetsByFaultID))
	for faultID := range targetsByFaultID {
		faultIDs = append(faultIDs, faultID)
	}
	sort.Strings(faultIDs)
	result := make([]processLogFaultScope, 0, len(faultIDs))
	for _, faultID := range faultIDs {
		targets := make([]string, 0, len(targetsByFaultID[faultID]))
		for target := range targetsByFaultID[faultID] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		result = append(result, processLogFaultScope{ID: faultID, Kind: kindByFaultID[faultID], Targets: targets})
	}
	return result
}

// scenarioProcessLogGate is the narrow scenario seam. Tests can prove scan
// ordering without manufacturing live supervisor processes.
type scenarioProcessLogGate interface {
	Scan(final bool, faults ...processLogFaultScope) (processLogScanResult, error)
	WriteEvidence(runDir string) error
}

type processLogGate struct {
	stateLock sync.Mutex
	stateDir  string
	path      string
	state     processLogGateState

	readRangeForTest    func(*os.File, int64, int64) ([]byte, error)
	afterCursorsForTest func(bool)
}

type processLogClassification struct {
	class                  string
	summary                string
	faultAttributable      bool
	nonblockingDisposition string
}

// classifyProcessLogLine is intentionally release-locked and fail-closed for
// log severities. Expected adversarial HTTP 400/429 responses and bounded peer
// admission refusals are not errors; every unknown warning/error/fatal line is.
func classifyProcessLogLine(line []byte) (processLogClassification, bool) {
	text := strings.TrimSpace(string(line))
	if text == "" {
		return processLogClassification{}, false
	}
	lower := strings.ToLower(text)

	// Catastrophic and previously observed release signals take precedence over
	// benign allow rules, even if a future message happens to mention both.
	switch {
	case strings.Contains(lower, "panic:"):
		return processLogClassification{class: "panic", summary: "process reported a panic"}, true
	case structuredLogSeverity(lower, "panic"):
		return processLogClassification{class: "panic", summary: "process emitted a structured panic log"}, true
	case strings.Contains(lower, "fatal error:"):
		return processLogClassification{class: "fatal", summary: "process reported a fatal runtime error"}, true
	case structuredLogSeverity(lower, "fatal"):
		return processLogClassification{class: "fatal", summary: "process emitted an unclassified fatal log"}, true
	case strings.Contains(lower, "failed to sufficiently increase receive buffer size") || strings.Contains(lower, "failed to increase receive buffer size"):
		return processLogClassification{class: "quic-receive-buffer", summary: "QUIC receive-buffer configuration is ineffective"}, true
	case strings.Contains(lower, "tls handshake timeout"):
		return processLogClassification{class: "tls-handshake-timeout", summary: "TLS handshake timed out", faultAttributable: true}, true
	case strings.Contains(lower, "h3 connect err") && strings.Contains(lower, "tls: internal error"):
		classification := processLogClassification{class: "h3-tls-internal", summary: "HTTP/3 connection failed with a TLS internal error"}
		if !processLogExplicitWarningOrError(text, lower) {
			classification.nonblockingDisposition = "resilient-fallback"
		}
		return classification, true
	case strings.Contains(lower, "read packet timeout"):
		classification := processLogClassification{class: "packet-read-timeout", summary: "packet read timed out", faultAttributable: true}
		if !processLogExplicitWarningOrError(text, lower) {
			classification.nonblockingDisposition = "resilient-fallback"
		}
		return classification, true
	case strings.Contains(lower, "close timeout") || strings.Contains(lower, "timed out waiting for connections to close"):
		return processLogClassification{class: "connection-close-timeout", summary: "connection close timed out", faultAttributable: true}, true
	case strings.Contains(lower, "exit could not create contract"):
		return processLogClassification{class: "contract-create", summary: "worker exited because it could not create a contract"}, true
	case strings.Contains(lower, "no seed providers"):
		return processLogClassification{class: "seed-unavailable", summary: "worker could not find a seed provider"}, true
	case strings.Contains(lower, "invalid byte sequence for encoding") && strings.Contains(lower, "0x00"):
		return processLogClassification{class: "postgres-null-byte", summary: "PostgreSQL rejected a transaction intent containing a NUL byte"}, true
	case strings.Contains(lower, "completehandshake failed: context canceled"):
		return processLogClassification{class: "connection-canceled", summary: "connection handshake was canceled", faultAttributable: true, nonblockingDisposition: "lifecycle"}, true
	}

	// These exact classes are expected protocol/lifecycle noise and have their
	// own quantitative acceptance checks. Do not broaden this allowlist to a
	// generic "reject", HTTP status, timeout, or error substring.
	if strings.Contains(lower, "peer connection memory budget exhausted") ||
		strings.Contains(lower, "setup admission refused") && (strings.Contains(lower, "reason=budget") || strings.Contains(lower, "reason=priority")) {
		return processLogClassification{}, false
	}

	if klogSeverity(text, 'F') {
		return processLogClassification{class: "fatal", summary: "process emitted an unclassified fatal log"}, true
	}
	if klogSeverity(text, 'E') || structuredLogSeverity(lower, "error") {
		return processLogClassification{class: "error", summary: "process emitted an unclassified error log", faultAttributable: processLogConnectionLoss(lower)}, true
	}
	if klogSeverity(text, 'W') || structuredLogSeverity(lower, "warning") || structuredLogSeverity(lower, "warn") {
		return processLogClassification{class: "warning", summary: "process emitted an unclassified warning log", faultAttributable: processLogConnectionLoss(lower)}, true
	}
	if strings.HasPrefix(lower, "fatal ") || strings.HasPrefix(lower, "fatal:") {
		return processLogClassification{class: "fatal", summary: "process emitted an unclassified fatal log"}, true
	}
	if strings.HasPrefix(lower, "error ") || strings.HasPrefix(lower, "error:") {
		return processLogClassification{class: "error", summary: "process emitted an unclassified error log", faultAttributable: processLogConnectionLoss(lower)}, true
	}
	if strings.HasPrefix(lower, "warning ") || strings.HasPrefix(lower, "warning:") || strings.HasPrefix(lower, "warn ") || strings.HasPrefix(lower, "warn:") {
		return processLogClassification{class: "warning", summary: "process emitted an unclassified warning log", faultAttributable: processLogConnectionLoss(lower)}, true
	}
	return processLogClassification{}, false
}

func processLogConnectionLoss(lower string) bool {
	for _, signature := range []string{
		"connection refused",
		"connection reset by peer",
		"broken pipe",
		"use of closed network connection",
		"context canceled",
		"context deadline exceeded",
		"transport is closing",
		"server closed",
	} {
		if strings.Contains(lower, signature) {
			return true
		}
	}
	return false
}

func processLogExplicitWarningOrError(text, lower string) bool {
	return klogSeverity(text, 'E') || klogSeverity(text, 'W') || klogSeverity(text, 'F') ||
		structuredLogSeverity(lower, "error") || structuredLogSeverity(lower, "warning") || structuredLogSeverity(lower, "warn") ||
		strings.HasPrefix(lower, "fatal ") || strings.HasPrefix(lower, "fatal:") ||
		strings.HasPrefix(lower, "error ") || strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "warning ") || strings.HasPrefix(lower, "warning:") || strings.HasPrefix(lower, "warn ") || strings.HasPrefix(lower, "warn:")
}

func klogSeverity(text string, severity byte) bool {
	if len(text) < 6 || text[0] != severity || text[5] != ' ' {
		return false
	}
	for index := 1; index < 5; index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}

func structuredLogSeverity(lower, severity string) bool {
	return strings.Contains(lower, `"level":"`+severity+`"`) ||
		strings.Contains(lower, `"level": "`+severity+`"`) ||
		strings.Contains(lower, "level="+severity)
}

func processLogIdentity(info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("process log has no kernel inode identity")
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}

func processLogRelativePath(stateDir, path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("process log path must be absolute")
	}
	cleanState, err := filepath.Abs(stateDir)
	if err != nil {
		return "", err
	}
	cleanPath := filepath.Clean(path)
	relative, err := filepath.Rel(cleanState, cleanPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("process log path %s escapes the simulator state directory", path)
	}
	if relative != filepath.Join("processes", filepath.Base(relative)) {
		return "", fmt.Errorf("process log path %s is outside the flat processes directory", path)
	}
	return relative, nil
}

func processLogCursors(stateDir string, manifest SupervisorFile) ([]processLogCursor, error) {
	cursors := make([]processLogCursor, 0, len(manifest.Specs)*2)
	seenProcessIds := map[string]bool{}
	seenRelativePaths := map[string]bool{}
	for _, spec := range manifest.Specs {
		if spec.ID == "" || spec.Role == "" || seenProcessIds[spec.ID] {
			return nil, fmt.Errorf("process log manifest has an empty or duplicate process identity %q", spec.ID)
		}
		seenProcessIds[spec.ID] = true
		for _, stream := range []struct {
			name string
			path string
		}{{name: "stdout", path: spec.StdoutPath}, {name: "stderr", path: spec.StderrPath}} {
			relative, err := processLogRelativePath(stateDir, stream.path)
			if err != nil {
				return nil, fmt.Errorf("process %s %s: %w", spec.ID, stream.name, err)
			}
			if seenRelativePaths[relative] {
				return nil, fmt.Errorf("process log path %s is shared by multiple streams", relative)
			}
			seenRelativePaths[relative] = true
			cursor := processLogCursor{ProcessID: spec.ID, Role: spec.Role, Stream: stream.name, Path: relative}
			info, statErr := os.Lstat(filepath.Join(stateDir, relative))
			if statErr == nil {
				if !info.Mode().IsRegular() {
					return nil, fmt.Errorf("process %s %s log is not a regular file", spec.ID, stream.name)
				}
				cursor.Device, cursor.Inode, err = processLogIdentity(info)
				if err != nil {
					return nil, fmt.Errorf("process %s %s: %w", spec.ID, stream.name, err)
				}
				cursor.InitialOffset = info.Size()
				cursor.Offset = info.Size()
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("inspect process %s %s log: %w", spec.ID, stream.name, statErr)
			}
			cursor.DigestOffset = cursor.InitialOffset
			cursor.ChunkChain = initialProcessLogChunkChain()
			cursors = append(cursors, cursor)
		}
	}
	sort.Slice(cursors, func(i, j int) bool {
		if cursors[i].ProcessID == cursors[j].ProcessID {
			return cursors[i].Stream < cursors[j].Stream
		}
		return cursors[i].ProcessID < cursors[j].ProcessID
	})
	return cursors, nil
}

func initializeProcessLogGate(stateDir string, manifest SupervisorFile) (*processLogGate, error) {
	return initializeProcessLogGateAtBoundary(stateDir, manifest, nil)
}

func initializeProcessLogGateAtBoundary(stateDir string, manifest SupervisorFile, boundary []processLogCursor) (*processLogGate, error) {
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		return nil, err
	}
	cursors, err := processLogCursors(stateDir, manifest)
	if err != nil {
		return nil, err
	}
	boundaryPathCursors := map[string]processLogCursor{}
	for _, cursor := range boundary {
		if _, duplicate := boundaryPathCursors[cursor.Path]; duplicate {
			return nil, fmt.Errorf("process log boundary duplicates path %s", cursor.Path)
		}
		boundaryPathCursors[cursor.Path] = cursor
	}
	for index := range cursors {
		prior, present := boundaryPathCursors[cursors[index].Path]
		if !present {
			continue
		}
		if prior.ProcessID != cursors[index].ProcessID || prior.Role != cursors[index].Role || prior.Stream != cursors[index].Stream {
			return nil, fmt.Errorf("process log boundary identity changed for %s", cursors[index].Path)
		}
		cursors[index] = prior
		delete(boundaryPathCursors, prior.Path)
	}
	if len(boundaryPathCursors) != 0 {
		return nil, errors.New("process log boundary contains a stream absent from the final supervisor manifest")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	gate := &processLogGate{
		stateDir: stateDir,
		path:     filepath.Join(stateDir, processLogGateStateFilename),
		state: processLogGateState{
			Schema: processLogGateSchema, Classifier: processLogClassifierVersion,
			DeploymentID: manifest.DeploymentID, ManifestHash: manifestHash,
			GeneratedAt: now, UpdatedAt: now, Cursors: cursors, Findings: []ProcessLogFinding{},
		},
	}
	if err := gate.persistWithLock(); err != nil {
		return nil, err
	}
	return gate, nil
}

func loadProcessLogGate(stateDir string, manifest SupervisorFile, supervisor SupervisorState) (*processLogGate, error) {
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		return nil, err
	}
	var state processLogGateState
	path := filepath.Join(stateDir, processLogGateStateFilename)
	if err := readJSONFile(path, &state); err != nil {
		return nil, fmt.Errorf("read process log gate: %w", err)
	}
	if state.Schema != processLogGateSchema || state.Classifier != processLogClassifierVersion {
		return nil, errors.New("process log gate schema or classifier does not match this release")
	}
	if state.DeploymentID != manifest.DeploymentID || state.ManifestHash != manifestHash {
		return nil, errors.New("process log gate does not match the live supervisor manifest")
	}
	if err := validatePersistedProcessLogGate(state); err != nil {
		return nil, err
	}
	expected, err := processLogCursorsWithoutOffsets(stateDir, manifest)
	if err != nil {
		return nil, err
	}
	if !sameProcessLogCursorInventory(state.Cursors, expected) {
		return nil, errors.New("process log gate cursor inventory does not match the live supervisor manifest")
	}
	gate := &processLogGate{stateDir: stateDir, path: path, state: state}
	if err := gate.bindWithLock(supervisor); err != nil {
		return nil, err
	}
	return gate, nil
}

func loadLiveProcessLogGate(stateDir string) (*processLogGate, error) {
	var manifest SupervisorFile
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.json"), &manifest); err != nil {
		return nil, fmt.Errorf("read live supervisor manifest for process log gate: %w", err)
	}
	var supervisor SupervisorState
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.state.json"), &supervisor); err != nil {
		return nil, fmt.Errorf("read live supervisor state for process log gate: %w", err)
	}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		return nil, err
	}
	if !supervisorStateReady(supervisor, manifestHash, manifest.Specs) {
		return nil, errors.New("process log gate requires the exact healthy live supervisor generation")
	}
	return loadProcessLogGate(stateDir, manifest, supervisor)
}

// Loading must not recapture offsets: the persisted launch boundary is the
// evidence fence. This helper validates only the manifest-owned inventory.
func processLogCursorsWithoutOffsets(stateDir string, manifest SupervisorFile) ([]processLogCursor, error) {
	copy := manifest
	for index := range copy.Specs {
		// Lstat is still useful for path validation but its offsets are erased.
		copy.Specs[index] = manifest.Specs[index]
	}
	cursors, err := processLogCursors(stateDir, copy)
	if err != nil {
		return nil, err
	}
	for index := range cursors {
		cursors[index].Device = 0
		cursors[index].Inode = 0
		cursors[index].InitialOffset = 0
		cursors[index].Offset = 0
		cursors[index].DigestOffset = 0
		cursors[index].ScannedBytes = 0
		cursors[index].ScannedLines = 0
		cursors[index].ChunkCount = 0
		cursors[index].ChunkChain = ""
	}
	return cursors, nil
}

func sameProcessLogCursorInventory(actual, expected []processLogCursor) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index].ProcessID != expected[index].ProcessID || actual[index].Role != expected[index].Role || actual[index].Stream != expected[index].Stream || actual[index].Path != expected[index].Path {
			return false
		}
	}
	return true
}

func validatePersistedProcessLogGate(state processLogGateState) error {
	if state.GeneratedAt == "" || state.UpdatedAt == "" {
		return errors.New("process log gate timestamps are incomplete")
	}
	if (state.SupervisorPID == 0) != (state.SupervisorStartTimeTicks == 0) {
		return errors.New("process log gate supervisor generation is incomplete")
	}
	for _, cursor := range state.Cursors {
		if cursor.InitialOffset < 0 || cursor.Offset < cursor.InitialOffset || cursor.DigestOffset < cursor.InitialOffset || cursor.DigestOffset > cursor.Offset || (cursor.Device == 0) != (cursor.Inode == 0) || cursor.InitialOffset > 0 && cursor.Inode == 0 || cursor.ScannedBytes != uint64(cursor.Offset-cursor.InitialOffset) || cursor.ChunkChain == "" {
			return fmt.Errorf("process log gate cursor %s/%s is invalid", cursor.ProcessID, cursor.Stream)
		}
		if digest, err := hex.DecodeString(cursor.ChunkChain); err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("process log gate cursor %s/%s has an invalid chunk chain", cursor.ProcessID, cursor.Stream)
		}
	}
	for _, finding := range state.Findings {
		if finding.ProcessID == "" || finding.Stream == "" || finding.Class == "" || finding.Summary == "" || finding.Disposition == "" || len(finding.FaultIDs) != len(finding.FaultKinds) || finding.Count == 0 || finding.FirstOffset < 0 || finding.LastOffset < 0 || finding.FirstObservedAt == "" || finding.LastObservedAt == "" {
			return errors.New("process log gate contains an invalid finding")
		}
		if finding.Blocking != (finding.Disposition == "unexplained") || finding.Disposition == "expected-fault" && len(finding.FaultIDs) == 0 || finding.Disposition != "expected-fault" && len(finding.FaultIDs) != 0 {
			return errors.New("process log gate finding disposition is invalid")
		}
	}
	return nil
}

func (self *processLogGate) Bind(supervisor SupervisorState) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	priorPID, priorStart := self.state.SupervisorPID, self.state.SupervisorStartTimeTicks
	if err := self.bindWithLock(supervisor); err != nil {
		return err
	}
	if priorPID == self.state.SupervisorPID && priorStart == self.state.SupervisorStartTimeTicks {
		return nil
	}
	return self.persistWithLock()
}

func (self *processLogGate) bindWithLock(supervisor SupervisorState) error {
	if supervisor.ManifestHash != self.state.ManifestHash || supervisor.SupervisorPID <= 1 || supervisor.SupervisorStartTimeTicks == 0 {
		return errors.New("process log gate cannot bind an incomplete or mismatched supervisor generation")
	}
	if self.state.SupervisorPID == 0 && self.state.SupervisorStartTimeTicks == 0 {
		self.state.SupervisorPID = supervisor.SupervisorPID
		self.state.SupervisorStartTimeTicks = supervisor.SupervisorStartTimeTicks
		return nil
	}
	if self.state.SupervisorPID != supervisor.SupervisorPID || self.state.SupervisorStartTimeTicks != supervisor.SupervisorStartTimeTicks {
		return fmt.Errorf("process log gate supervisor generation changed from pid=%d start=%d to pid=%d start=%d", self.state.SupervisorPID, self.state.SupervisorStartTimeTicks, supervisor.SupervisorPID, supervisor.SupervisorStartTimeTicks)
	}
	return nil
}

// validateBoundSupervisorWithLock proves that a persisted gate still belongs
// to the same live kernel process generation. An unbound launch gate is valid
// until readiness binds it for the first time.
func (self *processLogGate) validateBoundSupervisorWithLock() error {
	if self.state.SupervisorPID == 0 && self.state.SupervisorStartTimeTicks == 0 {
		return nil
	}
	supervisor := SupervisorState{
		SupervisorPID:            self.state.SupervisorPID,
		SupervisorStartTimeTicks: self.state.SupervisorStartTimeTicks,
	}
	if err := validateSupervisorGeneration(supervisor); err != nil {
		return fmt.Errorf("process log gate supervisor generation: %w", err)
	}
	return nil
}

func (self *processLogGate) Scan(final bool, faults ...processLogFaultScope) (processLogScanResult, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	scanErr := self.validateBoundSupervisorWithLock()
	if scanErr == nil {
		for index := range self.state.Cursors {
			if err := self.scanCursorWithLock(&self.state.Cursors[index], final, faults, now); err != nil {
				scanErr = err
				break
			}
		}
		if self.afterCursorsForTest != nil {
			self.afterCursorsForTest(final)
		}
		scanErr = errors.Join(scanErr, self.validateBoundSupervisorWithLock())
	}
	self.state.UpdatedAt = now
	persistErr := self.persistWithLock()
	result := processLogScanResult{
		Findings: append([]ProcessLogFinding(nil), self.state.Findings...),
	}
	return result, errors.Join(scanErr, persistErr)
}

// readRangeWithLock reads one size-fenced region of a process log. The test
// seam deterministically proves concurrent short-read handling without racing
// filesystem scheduling.
func (self *processLogGate) readRangeWithLock(file *os.File, offset, length int64) ([]byte, error) {
	if self.readRangeForTest != nil {
		return self.readRangeForTest(file, offset, length)
	}
	return io.ReadAll(io.NewSectionReader(file, offset, length))
}

func (self *processLogGate) scanCursorWithLock(cursor *processLogCursor, final bool, faults []processLogFaultScope, observedAt string) error {
	path := filepath.Join(self.stateDir, cursor.Path)
	lstat, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-integrity", summary: "process log is missing"}, cursor.Offset, "", observedAt)
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect process %s %s log: %w", cursor.ProcessID, cursor.Stream, err)
	}
	if !lstat.Mode().IsRegular() {
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-integrity", summary: "process log is not a regular file"}, cursor.Offset, "", observedAt)
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open process %s %s log: %w", cursor.ProcessID, cursor.Stream, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat process %s %s log: %w", cursor.ProcessID, cursor.Stream, err)
	}
	device, inode, err := processLogIdentity(info)
	if err != nil {
		return err
	}
	lstatDevice, lstatInode, err := processLogIdentity(lstat)
	if err != nil {
		return err
	}
	if device != lstatDevice || inode != lstatInode {
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-integrity", summary: "process log changed while it was opened"}, cursor.Offset, "", observedAt)
		return nil
	}
	if cursor.Device == 0 && cursor.Inode == 0 {
		cursor.Device, cursor.Inode = device, inode
	} else if cursor.Device != device || cursor.Inode != inode {
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-integrity", summary: "process log inode changed after the launch boundary"}, cursor.Offset, "", observedAt)
		return nil
	}
	if info.Size() < cursor.Offset {
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-integrity", summary: "process log was truncated after the launch boundary"}, cursor.Offset, "", observedAt)
		return nil
	}
	delta := info.Size() - cursor.Offset
	if delta == 0 {
		if final {
			return self.extendChunkChainWithLock(file, cursor, true)
		}
		return nil
	}
	if delta > processLogMaximumScanBytes {
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-overrun", summary: "process log growth exceeded the bounded scanner capacity"}, cursor.Offset, "", observedAt)
		return nil
	}
	data, err := self.readRangeWithLock(file, cursor.Offset, delta)
	if err != nil {
		return fmt.Errorf("read process %s %s log: %w", cursor.ProcessID, cursor.Stream, err)
	}
	if int64(len(data)) != delta {
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-integrity", summary: "process log changed while it was read"}, cursor.Offset, "", observedAt)
		return nil
	}
	completed, classifiable := len(data), len(data)
	unterminatedFinal := final && data[len(data)-1] != '\n'
	if !final {
		if newline := bytes.LastIndexByte(data, '\n'); newline >= 0 {
			completed = newline + 1
		} else {
			completed = 0
		}
		classifiable = completed
	} else if unterminatedFinal {
		if newline := bytes.LastIndexByte(data, '\n'); newline >= 0 {
			classifiable = newline + 1
		} else {
			classifiable = 0
		}
		self.recordFindingWithLock(cursor, processLogClassification{class: "log-integrity", summary: "process log has an unterminated final line"}, cursor.Offset+int64(classifiable), hashProcessLogLine(data[classifiable:]), observedAt)
	}
	if completed == 0 {
		if len(data) > processLogMaximumLineBytes {
			self.recordFindingWithLock(cursor, processLogClassification{class: "log-overrun", summary: "process log contains an overlong unterminated line"}, cursor.Offset, hashProcessLogLine(data), observedAt)
		}
		return nil
	}
	consumed := data[:completed]
	completeLines := data[:classifiable]
	lineOffset := cursor.Offset
	for len(completeLines) != 0 {
		newline := bytes.IndexByte(completeLines, '\n')
		if newline < 0 {
			return errors.New("process log classifier received an incomplete line")
		}
		line := completeLines[:newline]
		if len(line) > processLogMaximumLineBytes {
			self.recordFindingWithLock(cursor, processLogClassification{class: "log-overrun", summary: "process log contains an overlong line"}, lineOffset, hashProcessLogLine(line), observedAt)
		} else if len(line) != 0 {
			classification, matched := classifyProcessLogLine(line)
			if matched {
				self.recordClassifiedLineWithLock(cursor, classification, faults, lineOffset, hashProcessLogLine(line), observedAt)
			}
		}
		lineOffset += int64(newline + 1)
		completeLines = completeLines[newline+1:]
	}
	cursor.ScannedBytes += uint64(completed)
	cursor.ScannedLines += uint64(bytes.Count(consumed, []byte{'\n'}))
	if consumed[len(consumed)-1] != '\n' {
		cursor.ScannedLines++
	}
	cursor.Offset += int64(completed)
	return self.extendChunkChainWithLock(file, cursor, final)
}

func initialProcessLogChunkChain() string {
	digest := sha256.Sum256([]byte(processLogDigestDomain + "\x00initial"))
	return hex.EncodeToString(digest[:])
}

func extendProcessLogChunkChain(previous string, start, end int64, chunk []byte) (string, error) {
	previousBytes, err := hex.DecodeString(previous)
	if err != nil || len(previousBytes) != sha256.Size || start < 0 || end <= start || int64(len(chunk)) != end-start {
		return "", errors.New("invalid process log chunk-chain input")
	}
	chunkDigest := sha256.Sum256(chunk)
	digest := sha256.New()
	_, _ = digest.Write([]byte(processLogDigestDomain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(previousBytes)
	var offsets [16]byte
	binary.BigEndian.PutUint64(offsets[0:8], uint64(start))
	binary.BigEndian.PutUint64(offsets[8:16], uint64(end))
	_, _ = digest.Write(offsets[:])
	_, _ = digest.Write(chunkDigest[:])
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (self *processLogGate) extendChunkChainWithLock(file *os.File, cursor *processLogCursor, final bool) error {
	remaining := cursor.Offset - cursor.DigestOffset
	if !final {
		remaining -= remaining % processLogDigestChunkBytes
	}
	if remaining == 0 {
		return nil
	}
	buffer := make([]byte, processLogDigestChunkBytes)
	for remaining > 0 {
		chunkBytes := int64(processLogDigestChunkBytes)
		if remaining < chunkBytes {
			chunkBytes = remaining
		}
		start := cursor.DigestOffset
		end := start + chunkBytes
		if _, err := io.ReadFull(io.NewSectionReader(file, start, chunkBytes), buffer[:chunkBytes]); err != nil {
			return fmt.Errorf("read process %s %s digest chunk: %w", cursor.ProcessID, cursor.Stream, err)
		}
		next, err := extendProcessLogChunkChain(cursor.ChunkChain, start, end, buffer[:chunkBytes])
		if err != nil {
			return err
		}
		cursor.ChunkChain = next
		cursor.ChunkCount++
		cursor.DigestOffset = end
		remaining -= chunkBytes
	}
	return nil
}

func hashProcessLogLine(line []byte) string {
	digest := sha256.Sum256(bytes.TrimSpace(line))
	return hex.EncodeToString(digest[:])
}

func (self *processLogGate) recordFindingWithLock(cursor *processLogCursor, classification processLogClassification, offset int64, lineHash, observedAt string) {
	for index := range self.state.Findings {
		finding := &self.state.Findings[index]
		if finding.ProcessID != cursor.ProcessID || finding.Stream != cursor.Stream || finding.Class != classification.class || finding.Summary != classification.summary || !finding.Blocking || finding.Disposition != "unexplained" {
			continue
		}
		if finding.LastOffset == offset && finding.LastLineSHA256 == lineHash {
			return
		}
		finding.Count++
		finding.LastOffset = offset
		finding.LastLineSHA256 = lineHash
		finding.LastObservedAt = observedAt
		return
	}
	self.state.Findings = append(self.state.Findings, ProcessLogFinding{
		ProcessID: cursor.ProcessID, Role: cursor.Role, Stream: cursor.Stream,
		Class: classification.class, Summary: classification.summary, Blocking: true, Disposition: "unexplained", Count: 1,
		FirstOffset: offset, LastOffset: offset, FirstLineSHA256: lineHash, LastLineSHA256: lineHash,
		FirstObservedAt: observedAt, LastObservedAt: observedAt,
	})
	sortProcessLogFindings(self.state.Findings)
}

func matchingProcessLogFaults(processID string, classification processLogClassification, faults []processLogFaultScope) ([]string, []string) {
	if !classification.faultAttributable {
		return nil, nil
	}
	kindByFaultID := map[string]string{}
	for _, fault := range faults {
		for _, target := range fault.Targets {
			if target == processID && fault.ID != "" && fault.Kind != "" {
				kindByFaultID[fault.ID] = fault.Kind
			}
		}
	}
	faultIDs := make([]string, 0, len(kindByFaultID))
	for faultID := range kindByFaultID {
		faultIDs = append(faultIDs, faultID)
	}
	sort.Strings(faultIDs)
	faultKinds := make([]string, len(faultIDs))
	for index, faultID := range faultIDs {
		faultKinds[index] = kindByFaultID[faultID]
	}
	return faultIDs, faultKinds
}

func (self *processLogGate) recordClassifiedLineWithLock(cursor *processLogCursor, classification processLogClassification, faults []processLogFaultScope, offset int64, lineHash, observedAt string) {
	faultIDs, faultKinds := matchingProcessLogFaults(cursor.ProcessID, classification, faults)
	blocking, disposition := true, "unexplained"
	if len(faultIDs) != 0 {
		blocking, disposition = false, "expected-fault"
	} else if classification.nonblockingDisposition != "" {
		blocking, disposition = false, classification.nonblockingDisposition
	}
	faultKey := strings.Join(faultIDs, "\x00")
	for index := range self.state.Findings {
		finding := &self.state.Findings[index]
		if finding.ProcessID != cursor.ProcessID || finding.Stream != cursor.Stream || finding.Class != classification.class || finding.Summary != classification.summary || finding.Blocking != blocking || finding.Disposition != disposition || strings.Join(finding.FaultIDs, "\x00") != faultKey {
			continue
		}
		if finding.LastOffset == offset && finding.LastLineSHA256 == lineHash {
			return
		}
		finding.Count++
		finding.LastOffset = offset
		finding.LastLineSHA256 = lineHash
		finding.LastObservedAt = observedAt
		return
	}
	self.state.Findings = append(self.state.Findings, ProcessLogFinding{
		ProcessID: cursor.ProcessID, Role: cursor.Role, Stream: cursor.Stream,
		Class: classification.class, Summary: classification.summary, Blocking: blocking, Disposition: disposition,
		FaultIDs: append([]string(nil), faultIDs...), FaultKinds: append([]string(nil), faultKinds...), Count: 1,
		FirstOffset: offset, LastOffset: offset, FirstLineSHA256: lineHash, LastLineSHA256: lineHash,
		FirstObservedAt: observedAt, LastObservedAt: observedAt,
	})
	sortProcessLogFindings(self.state.Findings)
}

func sortProcessLogFindings(findings []ProcessLogFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.ProcessID != right.ProcessID {
			return left.ProcessID < right.ProcessID
		}
		if left.Stream != right.Stream {
			return left.Stream < right.Stream
		}
		if left.Class != right.Class {
			return left.Class < right.Class
		}
		if left.Disposition != right.Disposition {
			return left.Disposition < right.Disposition
		}
		return strings.Join(left.FaultIDs, "\x00") < strings.Join(right.FaultIDs, "\x00")
	})
}

func (self *processLogGate) RequireClean(final bool) error {
	result, err := self.Scan(final)
	if err != nil {
		return fmt.Errorf("process log gate scan: %w", err)
	}
	blocking := blockingProcessLogFindings(result.Findings)
	if len(blocking) == 0 {
		return nil
	}
	first := blocking[0]
	return fmt.Errorf("process log gate found %d release-blocking class(es); first=%s/%s/%s count=%d", len(blocking), first.ProcessID, first.Stream, first.Class, first.Count)
}

func blockingProcessLogFindings(findings []ProcessLogFinding) []ProcessLogFinding {
	blocking := make([]ProcessLogFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Blocking {
			blocking = append(blocking, finding)
		}
	}
	return blocking
}

func (self *processLogGate) persistWithLock() error {
	raw, err := json.MarshalIndent(self.state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(self.path, append(raw, '\n'), 0o600)
}

func (self *processLogGate) WriteEvidence(runDir string) error {
	var evidence processLogGateState
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		evidence = self.state
		evidence.Cursors = append([]processLogCursor(nil), self.state.Cursors...)
		evidence.Findings = append([]ProcessLogFinding(nil), self.state.Findings...)
	}()
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(runDir, processLogEvidenceFilename), append(raw, '\n'), 0o644)
}

func processLogFindingsError(findings []ProcessLogFinding) error {
	blocking := blockingProcessLogFindings(findings)
	if len(blocking) == 0 {
		return nil
	}
	first := blocking[0]
	return fmt.Errorf("process log gate found %d release-blocking class(es); first=%s/%s/%s count=%d", len(blocking), first.ProcessID, first.Stream, first.Class, first.Count)
}

func scanScenarioProcessLogs(gate scenarioProcessLogGate, runDir string, observation *ScenarioObservation, final bool, faults ...processLogFaultScope) error {
	if gate == nil {
		return nil
	}
	result, scanErr := gate.Scan(final, faults...)
	var hashErr error
	if observation != nil {
		observation.ProcessLogFindings = append([]ProcessLogFinding(nil), result.Findings...)
		observation.ObservationHash = ""
		observation.ObservationHash, hashErr = canonicalHashHex(observation)
	}
	evidenceErr := gate.WriteEvidence(runDir)
	return errors.Join(scanErr, hashErr, evidenceErr, processLogFindingsError(result.Findings))
}
