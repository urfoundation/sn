package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// This synthetic process tests Go custody and supervision only. Actual Wasm and
// storage effects are exercised by the Rust executor tests, not this test binary.
func productionRuntimeReplayTestProcess() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, maximumProductionRuntimeReplayBytes+1))
	if err != nil {
		os.Exit(71)
	}
	var job productionRuntimeReplayJob
	if json.Unmarshal(raw, &job) != nil {
		os.Exit(72)
	}
	var cases []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(job.CasesJson), &cases) != nil || len(cases) != 1 {
		os.Exit(73)
	}
	mode := cases[0].Name
	if strings.HasPrefix(mode, "wait:") {
		connection, err := net.Dial("unix", strings.TrimPrefix(mode, "wait:"))
		if err != nil {
			os.Exit(74)
		}
		path, _ := os.Executable()
		_, _ = connection.Write([]byte(path + "\n"))
		_, _ = io.Copy(io.Discard, connection)
		os.Exit(75)
	}
	if mode == "failure" {
		os.Exit(76)
	}
	if mode == "overflow" {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte{'x'}, 128*1024))
		os.Exit(0)
	}
	if mode == "malformed" {
		_, _ = os.Stdout.Write([]byte("{"))
		os.Exit(0)
	}
	report := ProductionRuntimeReplayReport{Schema: productionRuntimeReplaySchema, JobSha256: sha256.Sum256(raw), RulesSha256: job.RulesSha256, CasesSha256: job.CasesSha256,
		SdkRevision: productionRuntimeReplaySdk, Cases: 1, Steps: 1, OutputsSha256: [32]byte{0x18}, FiniteReplayOnly: true}
	switch mode {
	case "wrong-job":
		report.JobSha256[0]++
	case "overclaim":
		report.CompleteSemanticEquivalence = true
	case "rules-overclaim":
		report.SemanticRulesVerified = true
	case "selection":
		report.ProductionSelection = true
	case "wrong-count":
		report.Steps++
	}
	if json.NewEncoder(os.Stdout).Encode(report) != nil {
		os.Exit(77)
	}
	os.Exit(0)
}

type productionRuntimeReplayTestFixture struct {
	authority  *productionContinuityPolicyTestFixture
	executable string
	job        productionRuntimeReplayJob
	raw        []byte
}

func newProductionRuntimeReplayTestFixture(t *testing.T, mode string) *productionRuntimeReplayTestFixture {
	t.Helper()
	authority := newProductionContinuityPolicyTestFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	copy(authority.policy.VerifierBuildSha256[:], hash.Sum(nil))
	cases, err := json.Marshal([]any{map[string]any{"name": mode, "initial_state": []any{}, "steps": []any{map[string]any{
		"method": "BlockBuilder_apply_extrinsic", "input": "0x", "expected_output": "0x", "expected_state": []any{}}}}})
	if err != nil {
		t.Fatal(err)
	}
	authority.signPolicy(t)
	artifact := func(identity crv4.RuntimeArtifactIdentity) productionRuntimeReplayArtifact {
		value := productionRuntimeReplayArtifact{WasmHex: "0x00"}
		value.Expected.SpecName, value.Expected.SpecVersion = identity.Version.SpecName, identity.Version.SpecVersion
		value.Expected.TransactionVersion, value.Expected.StateVersion = identity.Version.TransactionVersion, identity.Version.StateVersion
		value.Expected.MetadataVersion, value.Expected.CodeSize, value.Expected.MetadataSize = 14, 1, 1
		value.Expected.CodeSha256, value.Expected.MetadataSha256 = [32]byte{1}, [32]byte{2}
		for text, target := range map[string]*[32]byte{identity.CodeHash: &value.Expected.CodeBlake2b, identity.MetadataHash: &value.Expected.MetadataBlake2b} {
			raw, err := hex.DecodeString(strings.TrimPrefix(text, "0x"))
			if err != nil || len(raw) != 32 {
				t.Fatal("synthetic artifact hash", err)
			}
			copy(target[:], raw)
		}
		return value
	}
	self := &productionRuntimeReplayTestFixture{authority: authority, executable: executable, job: productionRuntimeReplayJob{
		Schema: productionRuntimeReplaySchema, PolicySha256: sha256.Sum256(authority.policyRaw),
		SourceBuildEvidenceSha256: authority.result.Request.SourceBuildEvidenceHash, RulesSha256: authority.policy.SemanticRulesSha256, CasesSha256: sha256.Sum256(cases),
		Base: artifact(authority.policy.BaseArtifact), Candidate: artifact(authority.candidate), CasesJson: string(cases)}}
	self.signEvidence(t)
	return self
}

func (self *productionRuntimeReplayTestFixture) signEvidence(t *testing.T) {
	t.Helper()
	var err error
	self.raw, err = json.Marshal(self.job)
	if err != nil {
		t.Fatal(err)
	}
	self.authority.result.Request.PolicySha256 = sha256.Sum256(self.authority.policyRaw)
	self.authority.result.VerifierBuildSha256 = self.authority.policy.VerifierBuildSha256
	self.authority.result.SemanticRulesSha256 = self.authority.policy.SemanticRulesSha256
	self.authority.result.EvidenceSha256 = sha256.Sum256(self.raw)
	self.authority.signCertificate(t)
}

func (self *productionRuntimeReplayTestFixture) replay(ctx context.Context) (*ProductionRuntimeReplayReport, error) {
	return ReplayProductionRuntimeContinuityContext(ctx, self.authority.owner.cfg, self.authority.policyRaw, self.authority.certificateRaw, self.raw, self.executable)
}

func TestProductionRuntimeReplayRetainsFiniteCustody(t *testing.T) {
	fixture := newProductionRuntimeReplayTestFixture(t, "clean")
	original := append([]byte(nil), fixture.authority.owner.cfg.ownerRecycleProduction.encoded...)
	report, err := fixture.replay(context.Background())
	if err != nil || report == nil {
		t.Fatal("signed finite replay refused", err)
	}
	if report.JobSha256 != sha256.Sum256(fixture.raw) || !report.FiniteReplayOnly || report.SemanticRulesVerified || report.CompleteSemanticEquivalence || report.ProductionSelection ||
		report.RulesSha256 != fixture.authority.policy.SemanticRulesSha256 || report.CasesSha256 != sha256.Sum256([]byte(fixture.job.CasesJson)) || report.CasesSha256 == report.RulesSha256 {
		t.Fatal("finite replay overclaimed production authority", report)
	}
	if fixture.authority.calls != 0 || !bytes.Equal(original, fixture.authority.owner.cfg.ownerRecycleProduction.encoded) {
		t.Fatal("offline replay changed original custody or touched Rpc")
	}
}

func TestProductionRuntimeReplayRequiresIndependentEvidence(t *testing.T) {
	for _, fault := range []string{"signature", "evidence", "policy", "source", "rules", "cases"} {
		fixture := newProductionRuntimeReplayTestFixture(t, "clean")
		switch fault {
		case "signature":
			fixture.authority.certificateRaw = bytes.Replace(fixture.authority.certificateRaw, []byte(`"compatible":true`), []byte(`"compatible":false`), 1)
		case "evidence":
			fixture.raw = append(fixture.raw, ' ')
		case "policy":
			fixture.job.PolicySha256[0]++
			fixture.signEvidence(t)
		case "source":
			fixture.job.SourceBuildEvidenceSha256[0]++
			fixture.signEvidence(t)
		case "rules":
			fixture.job.RulesSha256[0]++
			fixture.signEvidence(t)
		case "cases":
			fixture.job.CasesJson += " "
			fixture.signEvidence(t)
		}
		if report, err := fixture.replay(context.Background()); err == nil || report != nil {
			t.Fatal("unbound replay evidence accepted", fault, err)
		}
	}
}

func TestProductionRuntimeReplayBindsBothArtifacts(t *testing.T) {
	for _, base := range []bool{true, false} {
		fixture := newProductionRuntimeReplayTestFixture(t, "clean")
		if base {
			fixture.job.Base.Expected.CodeBlake2b[0]++
		} else {
			fixture.job.Candidate.Expected.MetadataBlake2b[0]++
		}
		fixture.signEvidence(t)
		if report, err := fixture.replay(context.Background()); err == nil || report != nil || !strings.Contains(err.Error(), "original or candidate artifact") {
			t.Fatal("replay detached original or candidate artifact", base, err)
		}
	}
}

func TestProductionRuntimeReplayRequiresExactExecutable(t *testing.T) {
	fixture := newProductionRuntimeReplayTestFixture(t, "clean")
	fixture.authority.policy.VerifierBuildSha256[0]++
	fixture.authority.signPolicy(t)
	fixture.job.PolicySha256 = sha256.Sum256(fixture.authority.policyRaw)
	fixture.signEvidence(t)
	if report, err := fixture.replay(context.Background()); err == nil || report != nil || !strings.Contains(err.Error(), "approved executable bytes") {
		t.Fatal("unapproved replay executable accepted", err)
	}
}

func TestProductionRuntimeReplayRefusesPartialOrOverclaimedResults(t *testing.T) {
	for _, mode := range []string{"failure", "overflow", "malformed", "wrong-job", "overclaim", "rules-overclaim", "selection", "wrong-count"} {
		fixture := newProductionRuntimeReplayTestFixture(t, mode)
		if report, err := fixture.replay(context.Background()); err == nil || report != nil {
			t.Fatal("incomplete or overclaimed replay result accepted", mode, err)
		}
	}
}

func TestProductionRuntimeReplayCancellationJoinsWorker(t *testing.T) {
	// A private Unix socket is an explicit ready barrier, not a timing guess.
	path := filepath.Join(t.TempDir(), "ready")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fixture := newProductionRuntimeReplayTestFixture(t, "wait:"+path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		report, err := fixture.replay(ctx)
		if report != nil {
			err = errors.New("cancelled replay emitted report")
		}
		finished <- err
	}()
	if err := listener.(*net.UnixListener).SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	connection, err := listener.Accept()
	if err != nil {
		cancel()
		<-finished
		t.Fatal("worker never reached ready barrier", err)
	}
	defer connection.Close()
	if err := connection.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var received bytes.Buffer
	for {
		var one [1]byte
		if _, err := io.ReadFull(connection, one[:]); err != nil {
			t.Fatal(err)
		}
		if one[0] == '\n' {
			break
		}
		received.WriteByte(one[0])
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal("replay cancellation did not join child", err)
	}
	if _, err := os.Stat(received.String()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled replay retained private executable", err)
	}
}

func TestProductionRuntimeReplayExpiredDeadlineStopsBeforeExecution(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if report, err := ReplayProductionRuntimeContinuityContext(ctx, nil, nil, nil, nil, "absent"); report != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expired replay deadline reached authority or process", err)
	}
}

func TestProductionRuntimeReplayMissingContextRefusesBeforeExecution(t *testing.T) {
	if report, err := ReplayProductionRuntimeContinuityContext(nil, nil, nil, nil, nil, "absent"); report != nil || err == nil || !strings.Contains(err.Error(), "context is absent") {
		t.Fatal("missing replay context reached authority or process", err)
	}
}
