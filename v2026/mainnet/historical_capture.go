// Complete historical proof capture uses a read-only parent trie and the
// original block. The exported job must also pass the independent strict
// replay before any report is returned. Source and finality stay unapproved.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/unix"
)

const historicalCaptureSchema = "urnetwork-historical-execution-capture-v1"
const historicalCaptureRequestLimit = 20 * 1024 * 1024
const historicalCaptureReportLimit = 104 * 1024 * 1024

// No proof key enumeration is accepted as a completeness assertion. The
// worker reads the original parent code and captures all executed trie paths.
type historicalCaptureInput struct {
	Schema                string                              `json:"schema"`
	ParentHeaderHex       string                              `json:"parent_header_hex"`
	ParentHash            historicalReplayDigest              `json:"parent_hash"`
	ChildHeaderHex        string                              `json:"child_header_hex"`
	ChildHash             historicalReplayDigest              `json:"child_hash"`
	ExtrinsicsHex         []string                            `json:"extrinsics_hex"`
	RuntimeCodeSha256     historicalReplayDigest              `json:"runtime_code_sha256"`
	RuntimeCodeBlake2b256 historicalReplayDigest              `json:"runtime_code_blake2b_256"`
	ExecutionStateVersion uint8                               `json:"execution_state_version"`
	ObservationProfile    *historicalReplayObservationProfile `json:"observation_profile,omitempty"`
	PrincipalQueries      []historicalPrincipalQuery          `json:"principal_queries,omitempty"`
	PrincipalEffects      bool                                `json:"principal_effects,omitempty"`
}

type historicalCaptureReport struct {
	Schema           string                 `json:"schema"`
	RequestSha256    historicalReplayDigest `json:"request_sha256"`
	SdkRevision      string                 `json:"sdk_revision"`
	CaptureMethod    string                 `json:"capture_method"`
	BackendReads     uint64                 `json:"backend_reads"`
	BackendReadBytes uint64                 `json:"backend_read_bytes"`
	JobJSON          string                 `json:"job_json"`
	Replay           historicalReplayReport `json:"replay"`
}

type historicalCaptureRequest struct {
	Engine planFileReference
	Input  planFileReference
	Nodes  string
	Budget time.Duration
	Feed   *historicalNativeFeed
}

func (self historicalCaptureInput) validate() error {
	if self.Schema != historicalCaptureSchema || len(self.ExtrinsicsHex) > 16384 || self.ExecutionStateVersion > 1 || self.ParentHash == (historicalReplayDigest{}) || self.ChildHash == (historicalReplayDigest{}) || self.RuntimeCodeSha256 == (historicalReplayDigest{}) || self.RuntimeCodeBlake2b256 == (historicalReplayDigest{}) {
		return errors.New("historical capture request identity or protocol bound differs")
	}
	for _, value := range []string{self.ParentHeaderHex, self.ChildHeaderHex} {
		raw, err := historicalReplayHex(value, 64*1024)
		if err != nil || len(raw) == 0 {
			return errors.Join(errors.New("historical capture header shape differs"), err)
		}
	}
	var total int
	for _, value := range self.ExtrinsicsHex {
		raw, err := historicalReplayHex(value, 8*1024*1024-total)
		if err != nil || len(raw) == 0 {
			return errors.Join(errors.New("historical capture body shape or bound differs"), err)
		}
		total += len(raw)
	}
	return errors.Join(self.ObservationProfile.validate(historicalReplayJob{RuntimeCodeSha256: self.RuntimeCodeSha256}), validateHistoricalPrincipalQueries(self.PrincipalQueries), validateHistoricalPrincipalEffectsRequest(self.PrincipalEffects, self.PrincipalQueries))
}

func validateHistoricalCaptureReport(input historicalCaptureInput, raw []byte, report historicalCaptureReport) error {
	maximumJob, maximumNodes, maximumProofBytes := historicalJobLimits(input.ObservationProfile)
	_, maximumReads, maximumReadBytes := historicalCaptureLimits(input.ObservationProfile)
	if report.Schema != historicalCaptureSchema || report.RequestSha256 != historicalReplayDigest(sha256.Sum256(raw)) || report.SdkRevision != historicalReplaySdk || report.CaptureMethod != "pinned-sdk-execution-proof-plus-strict-replay" || report.BackendReads == 0 || report.BackendReads > maximumReads || report.BackendReadBytes == 0 || report.BackendReadBytes > maximumReadBytes || len(report.JobJSON) == 0 || len(report.JobJSON) > maximumJob {
		return errors.New("historical capture report identity or resource bound differs")
	}
	var job historicalReplayJob
	if err := decodePlanJson([]byte(report.JobJSON), &job); err != nil {
		return err
	}
	if job.Schema != historicalReplaySchema || job.ParentHeaderHex != input.ParentHeaderHex || job.ParentHash != input.ParentHash || job.ChildHeaderHex != input.ChildHeaderHex || job.ChildHash != input.ChildHash || !slices.Equal(job.ExtrinsicsHex, input.ExtrinsicsHex) || job.RuntimeCodeSha256 != input.RuntimeCodeSha256 || job.RuntimeCodeBlake2b256 != input.RuntimeCodeBlake2b256 || job.ExecutionStateVersion != input.ExecutionStateVersion || !reflect.DeepEqual(job.ObservationProfile, input.ObservationProfile) || !reflect.DeepEqual(job.PrincipalQueries, input.PrincipalQueries) || job.PrincipalEffects != input.PrincipalEffects || len(job.ProofNodesHex) == 0 || len(job.ProofNodesHex) > maximumNodes {
		return errors.New("historical capture job differs from original request")
	}
	code, err := historicalReplayHex(job.RuntimeCodeHex, 8*1024*1024)
	if err != nil || historicalReplayDigest(sha256.Sum256(code)) != input.RuntimeCodeSha256 || historicalReplayDigest(blake2b.Sum256(code)) != input.RuntimeCodeBlake2b256 {
		return errors.Join(errors.New("historical capture original runtime code differs"), err)
	}
	var proofBytes int
	for _, value := range job.ProofNodesHex {
		node, err := historicalReplayHex(value, min(historicalProofNodeLimit(job.ObservationProfile), maximumProofBytes-proofBytes))
		if err != nil || len(node) == 0 {
			return errors.Join(errors.New("historical capture proof node shape or bound differs"), err)
		}
		proofBytes += len(node)
	}
	if report.Replay.ProofBytes != uint64(proofBytes) {
		return errors.New("historical capture proof byte census differs")
	}
	return validateHistoricalReplayReport(job, []byte(report.JobJSON), report.Replay)
}

// Hold the directory descriptor across capture. Trie nodes are individually
// content-addressed and bounded by the worker; a path replacement cannot choose
// a different directory. No node DB is opened for writing or state committed.
func runHistoricalCapture(ctx context.Context, request historicalCaptureRequest, hooks historicalReplayHooks) (result *historicalCaptureReport, resultErr error) {
	if request.Budget < time.Minute || request.Budget > 15*time.Minute || !planSha256(request.Input.Sha256) || !bootstrapRootAbsolutePath(request.Nodes) {
		return nil, errors.New("historical capture requires exact input, absolute nodes and a60–900second owned budget")
	}
	owner, cancel := context.WithTimeout(ctx, request.Budget)
	defer cancel()
	raw, digest, err := readBootstrapRootFile(owner, request.Input.Path, historicalCaptureRequestLimit)
	if err != nil {
		return nil, fmt.Errorf("read historical capture request: %w", err)
	}
	if digest != request.Input.Sha256 {
		return nil, errors.New("historical capture request differs from exact input pin")
	}
	var input historicalCaptureInput
	if err := decodePlanJson(raw, &input); err != nil {
		return nil, err
	}
	if err := input.validate(); err != nil {
		return nil, err
	}
	if request.Feed != nil {
		if err := request.Feed.validate(input, raw, request.Nodes); err != nil {
			return nil, err
		}
	}
	fd, err := unix.Open(request.Nodes, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	nodes := os.NewFile(uintptr(fd), request.Nodes)
	defer func() {
		resultErr = errors.Join(resultErr, nodes.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	maximumReport, _, _ := historicalCaptureLimits(input.ObservationProfile)
	// The matching Rust profile caps the complete serialized capture, including
	// both principal boundaries. Replay-only additions do not enlarge this cap.
	output, err := runHistoricalProofWorker(owner, cancel, historicalProofWorkerRequest{Engine: request.Engine, Input: raw, Directory: filepath.Dir(request.Input.Path), MaximumReport: maximumReport, Nodes: nodes, Feed: request.Feed}, hooks)
	if err != nil {
		return nil, err
	}
	var report historicalCaptureReport
	if err := decodePlanJson(output, &report); err != nil {
		return nil, err
	}
	if err := validateHistoricalCaptureReport(input, raw, report); err != nil {
		return nil, err
	}
	return &report, owner.Err()
}
