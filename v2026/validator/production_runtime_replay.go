// Finite offline replay checks retained evidence. It never installs a runtime
// selection, grants signing authority or substitutes test cases for equivalence.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const productionRuntimeReplaySchema = "urnetwork-runtime-transition-replay-v1"
const productionRuntimeReplayArgument = "--runtime-transition-replay-v1"
const productionRuntimeReplaySdk = "cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a"
const maximumProductionRuntimeReplayBytes = 48 * 1024 * 1024

// These fields match the pinned executor's complete artifact contract.
type productionRuntimeReplayArtifact struct {
	Expected struct {
		SpecName           string   `json:"spec_name"`
		SpecVersion        uint32   `json:"spec_version"`
		TransactionVersion uint32   `json:"transaction_version"`
		StateVersion       uint8    `json:"state_version"`
		MetadataVersion    uint32   `json:"metadata_version"`
		CodeSize           uint64   `json:"code_size"`
		MetadataSize       uint64   `json:"metadata_size"`
		CodeSha256         [32]byte `json:"code_sha256"`
		CodeBlake2b        [32]byte `json:"code_blake2b_256"`
		MetadataSha256     [32]byte `json:"metadata_sha256"`
		MetadataBlake2b    [32]byte `json:"metadata_blake2b_256"`
	} `json:"expected"`
	WasmHex string `json:"wasm_hex"`
}

type productionRuntimeReplayJob struct {
	Schema                    string                          `json:"schema"`
	PolicySha256              [32]byte                        `json:"policy_sha256"`
	SourceBuildEvidenceSha256 [32]byte                        `json:"source_build_evidence_sha256"`
	RulesSha256               [32]byte                        `json:"rules_sha256"`
	CasesSha256               [32]byte                        `json:"cases_sha256"`
	Base                      productionRuntimeReplayArtifact `json:"base"`
	Candidate                 productionRuntimeReplayArtifact `json:"candidate"`
	CasesJson                 string                          `json:"cases_json"`
}

// No authenticated chain state, semantic certificate or selection capability is
// minted here. Outputs describe only the exact supplied finite cases.
type ProductionRuntimeReplayReport struct {
	Schema                      string   `json:"schema"`
	JobSha256                   [32]byte `json:"job_sha256"`
	RulesSha256                 [32]byte `json:"rules_sha256"`
	CasesSha256                 [32]byte `json:"cases_sha256"`
	SdkRevision                 string   `json:"sdk_revision"`
	Cases                       uint32   `json:"cases"`
	Steps                       uint32   `json:"steps"`
	OutputsSha256               [32]byte `json:"outputs_sha256"`
	FiniteReplayOnly            bool     `json:"finite_replay_only"`
	SemanticRulesVerified       bool     `json:"semantic_rules_verified"`
	CompleteSemanticEquivalence bool     `json:"complete_semantic_equivalence"`
	ProductionSelection         bool     `json:"production_selection"`
}

// Each pipe has one os/exec copying goroutine. Overflow cancels the same owned
// process; Run joins those goroutines before their buffers are read.
type productionRuntimeReplayOutput struct {
	buffer bytes.Buffer
	cancel context.CancelFunc
}

func (self *productionRuntimeReplayOutput) Write(raw []byte) (int, error) {
	if self.buffer.Len()+len(raw) > 64*1024 {
		self.cancel()
		return 0, errors.New("runtime replay output exceeds bound")
	}
	return self.buffer.Write(raw)
}

// Duplicate, unknown and trailing fields cannot alias an evidence reference.
func decodeProductionRuntimeReplay(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > maximumProductionRuntimeReplayBytes {
		return errors.New("runtime replay document byte bound")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("runtime replay document has trailing JSON")
	}
	return nil
}

// Borrows all inputs for this synchronous call. The independently approved
// executable is copied and hashed in a private directory before execution, so a
// mutable caller pathname cannot replace the checked bytes. A two-minute budget
// includes copying, child execution and pipe ownership; caller cancellation wins.
func ReplayProductionRuntimeContinuityContext(ctx context.Context, cfg *ReleaseConfig, policyRaw, certificateRaw, replayRaw []byte, executable string) (*ProductionRuntimeReplayReport, error) {
	if ctx == nil {
		return nil, errors.New("runtime replay context is absent")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	envelope, certificate, err := verifyProductionRuntimeContinuity(cfg, policyRaw, certificateRaw)
	if err != nil {
		return nil, err
	}
	policy, result := envelope.Policy, certificate.Result
	var job productionRuntimeReplayJob
	if err := decodeProductionRuntimeReplay(replayRaw, &job); err != nil {
		return nil, err
	}
	if sha256.Sum256(replayRaw) != result.EvidenceSha256 || job.Schema != productionRuntimeReplaySchema ||
		job.PolicySha256 != sha256.Sum256(policyRaw) || job.SourceBuildEvidenceSha256 != result.Request.SourceBuildEvidenceHash ||
		job.RulesSha256 != policy.SemanticRulesSha256 || sha256.Sum256([]byte(job.CasesJson)) != job.CasesSha256 {
		return nil, errors.New("runtime replay signed evidence or rules differs")
	}
	var cases []struct {
		Name         string            `json:"name"`
		InitialState json.RawMessage   `json:"initial_state"`
		Steps        []json.RawMessage `json:"steps"`
	}
	if err := decodeProductionRuntimeReplay([]byte(job.CasesJson), &cases); err != nil {
		return nil, err
	}
	var steps uint32
	for _, item := range cases {
		if len(item.Steps) == 0 || len(item.Steps) > 16 {
			return nil, errors.New("runtime replay declared step bound")
		}
		steps += uint32(len(item.Steps))
	}
	if len(cases) == 0 || len(cases) > 16 {
		return nil, errors.New("runtime replay declared case bound")
	}
	for _, pair := range []struct {
		artifact productionRuntimeReplayArtifact
		identity crv4.RuntimeArtifactIdentity
	}{{artifact: job.Base, identity: policy.BaseArtifact}, {artifact: job.Candidate, identity: result.Request.Artifact}} {
		expected, identity := pair.artifact.Expected, pair.identity
		if expected.SpecName != identity.Version.SpecName || expected.SpecVersion != identity.Version.SpecVersion ||
			expected.TransactionVersion != identity.Version.TransactionVersion || expected.StateVersion != identity.Version.StateVersion ||
			"0x"+hex.EncodeToString(expected.CodeBlake2b[:]) != identity.CodeHash || "0x"+hex.EncodeToString(expected.MetadataBlake2b[:]) != identity.MetadataHash {
			return nil, errors.New("runtime replay original or candidate artifact differs")
		}
	}
	input, err := os.Open(executable)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512*1024*1024 {
		return nil, errors.Join(errors.New("runtime replay executable is invalid or oversized"), err)
	}
	directory, err := os.MkdirTemp("", "runtime-replay-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	privatePath := filepath.Join(directory, "executor")
	output, err := os.OpenFile(privatePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0500)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	buffer := make([]byte, 64*1024)
	var copied int64
	for err == nil {
		if err = ctx.Err(); err != nil {
			break
		}
		var count int
		count, err = input.Read(buffer)
		copied += int64(count)
		if copied > info.Size() {
			err = errors.New("runtime replay executable grew while copying")
			break
		}
		if count != 0 {
			_, writeErr := io.MultiWriter(output, hash).Write(buffer[:count])
			if writeErr != nil {
				err = writeErr
				break
			}
		}
	}
	closeErr := output.Close()
	if !errors.Is(err, io.EOF) || closeErr != nil || copied != info.Size() || !bytes.Equal(hash.Sum(nil), policy.VerifierBuildSha256[:]) {
		return nil, errors.Join(errors.New("runtime replay approved executable bytes differ"), err, closeErr)
	}
	stdout, stderr := &productionRuntimeReplayOutput{cancel: cancel}, &productionRuntimeReplayOutput{cancel: cancel}
	command := exec.CommandContext(ctx, privatePath, productionRuntimeReplayArgument)
	command.Dir, command.Env = directory, []string{}
	command.Stdin, command.Stdout, command.Stderr = bytes.NewReader(replayRaw), stdout, stderr
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		return nil, errors.Join(fmt.Errorf("runtime replay worker refused: %w", err), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var report ProductionRuntimeReplayReport
	if err := decodeProductionRuntimeReplay(stdout.buffer.Bytes(), &report); err != nil {
		return nil, err
	}
	if report.Schema != productionRuntimeReplaySchema || report.JobSha256 != sha256.Sum256(replayRaw) || report.RulesSha256 != job.RulesSha256 || report.CasesSha256 != job.CasesSha256 ||
		report.SdkRevision != productionRuntimeReplaySdk || report.Cases != uint32(len(cases)) || report.Steps != steps ||
		report.OutputsSha256 == ([32]byte{}) || !report.FiniteReplayOnly || report.SemanticRulesVerified || report.CompleteSemanticEquivalence || report.ProductionSelection {
		return nil, errors.New("runtime replay result identity or finite scope differs")
	}
	if _, _, err := verifyProductionRuntimeContinuity(cfg, policyRaw, certificateRaw); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &report, nil
}
