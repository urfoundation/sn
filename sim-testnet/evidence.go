package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/merkle"
)

const releaseEvidenceSchema = "urnetwork-release-evidence-v1"

type ReleaseEvidenceEnvelope struct {
	Schema       string          `json:"schema"`
	DeploymentID string          `json:"deployment_id"`
	ChainID      uint64          `json:"chain_id"`
	GenesisHash  string          `json:"genesis_hash"`
	Netuid       uint16          `json:"netuid"`
	Kind         string          `json:"kind"`
	RunID        string          `json:"run_id,omitempty"`
	CreatedAt    string          `json:"created_at"`
	Payload      json.RawMessage `json:"payload"`
	Signer       common.Address  `json:"signer"`
	ContentHash  string          `json:"content_hash"`
	Signature    string          `json:"signature"`
}

func evidenceUnsignedBytes(e *ReleaseEvidenceEnvelope) ([]byte, error) {
	copy := *e
	copy.ContentHash = ""
	copy.Signature = ""
	return json.Marshal(copy)
}

func signEvidence(cfg *ResolvedConfig, kind, runID string, payload any, role EVMRoleSecret) (*ReleaseEvidenceEnvelope, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, err
	}
	e := &ReleaseEvidenceEnvelope{Schema: releaseEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: strings.ToLower(cfg.Public.Chain.GenesisHash), Netuid: cfg.Netuid, Kind: kind, RunID: runID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Payload: encoded, Signer: crypto.PubkeyToAddress(key.PublicKey)}
	unsigned, err := evidenceUnsignedBytes(e)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(unsigned)
	sig, err := crypto.Sign(h[:], key)
	if err != nil {
		return nil, err
	}
	e.ContentHash = "sha256:" + hex.EncodeToString(h[:])
	e.Signature = "0x" + hex.EncodeToString(sig)
	return e, verifyEvidence(e, &key.PublicKey)
}

func verifyEvidence(e *ReleaseEvidenceEnvelope, expected *ecdsa.PublicKey) error {
	if e == nil || e.Schema != releaseEvidenceSchema || e.DeploymentID == "" || e.ChainID == 0 || e.Netuid == 0 || e.Kind == "" || !json.Valid(e.Payload) || e.Signer == (common.Address{}) {
		return errors.New("invalid release evidence identity")
	}
	unsigned, err := evidenceUnsignedBytes(e)
	if err != nil {
		return err
	}
	h := sha256.Sum256(unsigned)
	if e.ContentHash != "sha256:"+hex.EncodeToString(h[:]) {
		return errors.New("release evidence hash mismatch")
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(e.Signature, "0x"))
	if err != nil || len(sig) != crypto.SignatureLength {
		return errors.New("invalid release evidence signature")
	}
	pub, err := crypto.SigToPub(h[:], sig)
	if err != nil || crypto.PubkeyToAddress(*pub) != e.Signer {
		return errors.New("release evidence signer mismatch")
	}
	if expected != nil && crypto.PubkeyToAddress(*expected) != e.Signer {
		return errors.New("unexpected release evidence signer")
	}
	return nil
}

type PublishedEvidence struct {
	ContentHash string `json:"content_hash"`
	ContentKey  string `json:"content_key"`
	HistoryKey  string `json:"history_key"`
	Bucket      string `json:"bucket"`
}

func publishEvidence(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, kind, runID string, payload any) ([]PublishedEvidence, error) {
	if roles == nil {
		return nil, errors.New("evidence publication requires role secrets")
	}
	var published []PublishedEvidence
	client := &http.Client{Timeout: 30 * time.Second}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		role, ok := roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)]
		if !ok {
			return nil, fmt.Errorf("operator %d artifact role is missing", operator)
		}
		localName := fmt.Sprintf("%s.operator-%d.evidence.json", kind, operator)
		localPath := filepath.Join(stateDir, "public", localName)
		if runID != "" {
			localPath = filepath.Join(stateDir, "runs", runID, localName)
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		var envelope *ReleaseEvidenceEnvelope
		if existing, readErr := os.ReadFile(localPath); readErr == nil {
			var prior ReleaseEvidenceEnvelope
			key, keyErr := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
			if json.Unmarshal(existing, &prior) != nil || keyErr != nil || verifyEvidence(&prior, &key.PublicKey) != nil || prior.Kind != kind || prior.RunID != runID || !bytes.Equal(prior.Payload, payloadBytes) {
				return nil, fmt.Errorf("immutable local evidence %s does not match this publication", localPath)
			}
			envelope = &prior
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		} else {
			envelope, err = signEvidence(cfg, kind, runID, payload, role)
			if err != nil {
				return nil, err
			}
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return nil, err
		}
		if _, statErr := os.Stat(localPath); errors.Is(statErr, os.ErrNotExist) {
			if err := atomicWrite(localPath, append(encoded, '\n'), 0o644); err != nil {
				return nil, err
			}
		}
		url := fmt.Sprintf("http://127.0.0.1:%d/sn/evidence", 18080+operator)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("operator %d evidence API: %w", operator, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("operator %d evidence API returned HTTP %d", operator, resp.StatusCode)
		}
		var result PublishedEvidence
		if err := json.Unmarshal(body, &result); err != nil || !strings.EqualFold(result.ContentHash, envelope.ContentHash) {
			return nil, fmt.Errorf("operator %d evidence receipt is invalid", operator)
		}
		published = append(published, result)
	}
	return published, nil
}

func verifyPublishedEvidenceOrigins(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, published []PublishedEvidence) error {
	if len(published) != cfg.Config.Topology.Operators || len(cfg.OperatorAPIOrigins) != len(published) {
		return fmt.Errorf("published evidence/origin count does not match operator topology")
	}
	for i, receipt := range published {
		if err := verifyPublishedEvidenceOrigin(ctx, cfg, roles, i+1, cfg.OperatorAPIOrigins[i], receipt); err != nil {
			return fmt.Errorf("operator %d public evidence origin: %w", i+1, err)
		}
	}
	return nil
}

// verifyPublishedEvidenceOrigin proves that an externally advertised operator
// origin serves the exact immutable envelope accepted through its local API,
// and that the same content address is visible in public history. A launch is
// not portable to a second host until this check succeeds for every operator.
func verifyPublishedEvidenceOrigin(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, operator int, origin string, published PublishedEvidence) error {
	if operator < 1 || operator > cfg.Config.Topology.Operators || roles == nil {
		return errors.New("invalid public evidence operator")
	}
	role, ok := roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)]
	if !ok {
		return errors.New("artifact signer role is missing")
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	contentURL := strings.TrimSuffix(origin, "/") + "/sn/evidence?hash=" + published.ContentHash
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, contentURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("content endpoint returned HTTP %d", resp.StatusCode)
	}
	if len(body) > 64<<20 {
		return errors.New("content endpoint exceeded 64 MiB")
	}
	var envelope ReleaseEvidenceEnvelope
	if json.Unmarshal(body, &envelope) != nil || verifyEvidence(&envelope, &key.PublicKey) != nil {
		return errors.New("content endpoint returned invalid signed evidence")
	}
	if !strings.EqualFold(envelope.ContentHash, published.ContentHash) || envelope.DeploymentID != cfg.Config.Deployment.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return errors.New("content endpoint returned evidence for a different deployment")
	}

	historyURL := fmt.Sprintf("%s/sn/evidence/history?deployment_id=%s&netuid=%d&kind=%s", strings.TrimSuffix(origin, "/"), cfg.Config.Deployment.DeploymentID, cfg.Netuid, envelope.Kind)
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
	if err != nil {
		return err
	}
	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	history, readErr := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("history endpoint returned HTTP %d", resp.StatusCode)
	}
	if len(history) > 16<<20 {
		return errors.New("history endpoint exceeded 16 MiB")
	}
	want := strings.TrimPrefix(strings.ToLower(published.ContentHash), "sha256:")
	for _, object := range evidenceHistoryKeys(history) {
		if strings.Contains(strings.ToLower(object), want) {
			return nil
		}
	}
	return errors.New("public history does not contain the published content hash")
}

type PublicDeploymentManifest struct {
	Schema          string                     `json:"schema"`
	Release         string                     `json:"release"`
	DeploymentID    string                     `json:"deployment_id"`
	GeneratedAt     string                     `json:"generated_at"`
	ChainID         uint64                     `json:"chain_id"`
	GenesisHash     string                     `json:"genesis_hash"`
	RuntimeSpec     uint32                     `json:"runtime_spec"`
	Netuid          uint16                     `json:"netuid"`
	EVMRPC          string                     `json:"evm_rpc"`
	SubstrateRPC    string                     `json:"substrate_rpc"`
	ConfigHash      string                     `json:"config_hash"`
	PolicyHash      string                     `json:"policy_hash"`
	PlanHash        string                     `json:"plan_hash"`
	ReleaseLockHash string                     `json:"release_lock_hash"`
	Contracts       *ContractDeployment        `json:"contracts"`
	Identities      json.RawMessage            `json:"identities"`
	SetupEvidence   map[string]json.RawMessage `json:"setup_evidence"`
	Operators       []PublicOperator           `json:"operators"`
	Topology        TopologyConfig             `json:"topology"`
	ArtifactStores  []string                   `json:"artifact_history_endpoints"`
	EvidenceStores  []string                   `json:"release_evidence_history_endpoints"`
	Commands        map[string]string          `json:"commands"`
}

type PublicOperator struct {
	NoID       int    `json:"no_id"`
	APIURL     string `json:"api_url"`
	ConnectURL string `json:"connect_url,omitempty"`
	VerifyURL  string `json:"verify_url"`
	HistoryURL string `json:"history_url"`
}

func writePublicDeploymentManifest(cfg *ResolvedConfig, stateDir string, plan *SetupPlan) (*PublicDeploymentManifest, error) {
	contracts, err := loadContractDeployment(stateDir)
	if err != nil {
		return nil, err
	}
	identities, err := os.ReadFile(filepath.Join(stateDir, "public", "identities.json"))
	if err != nil || !json.Valid(identities) {
		return nil, errors.New("public identities are missing or invalid")
	}
	setupEvidence, err := loadPublicSetupEvidence(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	releaseLockHash, err := canonicalHashHex(cfg.Release)
	if err != nil {
		return nil, err
	}
	manifest := &PublicDeploymentManifest{Schema: "urnetwork-sim-public-deployment-v1", Release: "1.0", DeploymentID: cfg.Config.Deployment.DeploymentID, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, RuntimeSpec: cfg.Public.Chain.ExpectedRuntimeSpec, Netuid: cfg.Netuid, EVMRPC: cfg.Public.Chain.EVMPublicReadEndpoint, SubstrateRPC: cfg.Public.Chain.SubstratePublicReadEndpoint, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ReleaseLockHash: releaseLockHash, Contracts: contracts, Identities: append(json.RawMessage(nil), identities...), SetupEvidence: setupEvidence, Topology: cfg.Config.Topology, Commands: map[string]string{}}
	if plan != nil {
		manifest.PlanHash = plan.PlanHash
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		if len(cfg.OperatorAPIOrigins) != cfg.Config.Topology.Operators {
			return nil, fmt.Errorf("public operator API origins are not configured")
		}
		base := strings.TrimSuffix(cfg.OperatorAPIOrigins[operator-1], "/")
		manifest.Operators = append(manifest.Operators, PublicOperator{NoID: operator, APIURL: base, VerifyURL: base + "/verify", HistoryURL: base + "/sn/evidence/history"})
		manifest.ArtifactStores = append(manifest.ArtifactStores, fmt.Sprintf("%s/sn/artifacts?deployment_id=%s&netuid=%d", base, cfg.Config.Deployment.DeploymentID, cfg.Netuid))
		manifest.EvidenceStores = append(manifest.EvidenceStores, fmt.Sprintf("%s/sn/evidence/history?deployment_id=%s&netuid=%d", base, cfg.Config.Deployment.DeploymentID, cfg.Netuid))
	}
	for _, command := range []string{"status", "tail", "scenario", "stop", "resume"} {
		manifest.Commands[command] = fmt.Sprintf("sim-testnet %s --config <path-to-testnet.yml> --state-dir <deployment-state-dir>", command)
	}
	for _, command := range []string{"inspect", "analyze"} {
		manifest.Commands[command] = fmt.Sprintf("sim-testnet %s --config <path-to-testnet.yml> --manifest <public-manifest-url-or-file>", command)
	}
	path := filepath.Join(stateDir, "public.json")
	if existing, readErr := os.ReadFile(path); readErr == nil {
		var prior PublicDeploymentManifest
		if json.Unmarshal(existing, &prior) != nil || prior.Schema != manifest.Schema || prior.Release != manifest.Release || prior.DeploymentID != manifest.DeploymentID || prior.ChainID != manifest.ChainID || !strings.EqualFold(prior.GenesisHash, manifest.GenesisHash) || prior.Netuid != manifest.Netuid || prior.ConfigHash != manifest.ConfigHash || prior.PolicyHash != manifest.PolicyHash || prior.PlanHash != manifest.PlanHash || prior.ReleaseLockHash != manifest.ReleaseLockHash {
			return nil, errors.New("existing public deployment manifest does not match this deployment")
		}
		priorContracts, _ := canonicalHashHex(prior.Contracts)
		currentContracts, _ := canonicalHashHex(manifest.Contracts)
		priorIdentities, _ := canonicalHashHex(prior.Identities)
		currentIdentities, _ := canonicalHashHex(manifest.Identities)
		priorSetup, _ := canonicalHashHex(prior.SetupEvidence)
		currentSetup, _ := canonicalHashHex(manifest.SetupEvidence)
		priorOperators, _ := canonicalHashHex(prior.Operators)
		currentOperators, _ := canonicalHashHex(manifest.Operators)
		if priorContracts != currentContracts || priorIdentities != currentIdentities || priorSetup != currentSetup || priorOperators != currentOperators {
			return nil, errors.New("existing public deployment manifest contract/identity hash mismatch")
		}
		return &prior, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(path, append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	return manifest, nil
}

func loadPublicSetupEvidence(cfg *ResolvedConfig, stateDir string) (map[string]json.RawMessage, error) {
	paths := map[string]string{
		"voluntary_conviction": filepath.Join(stateDir, "public", "voluntary-conviction.json"),
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		paths[fmt.Sprintf("fleet_%d_manifest", fleet)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.json", fleet))
		paths[fmt.Sprintf("fleet_%d_commitment", fleet)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.commitment.json", fleet))
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			paths[fmt.Sprintf("fleet_%d_binding_%d", fleet, member)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
		}
	}
	result := make(map[string]json.RawMessage, len(paths))
	for name, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil || !json.Valid(b) {
			return nil, fmt.Errorf("public setup evidence %s is missing or invalid: %w", name, err)
		}
		result[name] = append(json.RawMessage(nil), bytes.TrimSpace(b)...)
	}
	return result, nil
}

// The following artifact mirror is intentionally independent of server code:
// inspect/analyze must work without importing the server module or trusting
// its database.
type payoutBoundary struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}
type payoutProvider struct {
	ClientID          [16]byte `json:"client_id"`
	NetworkID         [16]byte `json:"network_id"`
	Coldkey           [32]byte `json:"coldkey"`
	UsageBytes        uint64   `json:"usage_bytes"`
	Assignments       uint64   `json:"assignments"`
	Confirmations     uint64   `json:"confirmations"`
	ReliabilityPPM    uint32   `json:"reliability_ppm"`
	Eligible          bool     `json:"eligible"`
	HeadExcluded      bool     `json:"head_excluded"`
	ExclusionReason   string   `json:"exclusion_reason,omitempty"`
	BindingGeneration uint64   `json:"binding_generation,omitempty"`
}
type payoutLeaf struct {
	Index    uint64     `json:"index"`
	ClientID [16]byte   `json:"allocation_client_id"`
	Coldkey  [32]byte   `json:"coldkey"`
	ShareBPS uint64     `json:"share_bps"`
	Proof    [][32]byte `json:"proof"`
}
type payoutArtifact struct {
	Schema               string           `json:"schema"`
	DeploymentID         string           `json:"deployment_id"`
	ChainID              uint64           `json:"chain_id"`
	GenesisHash          string           `json:"genesis_hash"`
	Netuid               uint16           `json:"netuid"`
	Coordinator          common.Address   `json:"coordinator"`
	SettlementVault      common.Address   `json:"settlement_vault"`
	Epoch                uint64           `json:"epoch"`
	NoID                 uint64           `json:"no_id"`
	PolicyHash           string           `json:"policy_hash"`
	Start                payoutBoundary   `json:"start"`
	End                  payoutBoundary   `json:"end"`
	OperatorSnapshotHash string           `json:"operator_snapshot_hash"`
	FleetSnapshotHash    string           `json:"fleet_snapshot_hash"`
	ProviderSnapshotHash string           `json:"provider_snapshot_hash"`
	Providers            []payoutProvider `json:"providers"`
	Leaves               []payoutLeaf     `json:"leaves"`
	PayoutRoot           [32]byte         `json:"payout_root"`
	TotalUsageBytes      uint64           `json:"total_usage_bytes"`
	EligibleUsageBytes   uint64           `json:"eligible_usage_bytes"`
	ExcludedUsageBytes   uint64           `json:"excluded_usage_bytes"`
	SharesTotalBPS       uint64           `json:"shares_total_bps"`
	CreatedAt            string           `json:"created_at"`
	Signer               common.Address   `json:"signer"`
	ContentHash          string           `json:"content_hash"`
	Signature            string           `json:"signature"`
}

func payoutUnsignedBytes(a *payoutArtifact) ([]byte, error) {
	copy := *a
	copy.ContentHash = ""
	copy.Signature = ""
	copy.Signer = common.Address{}
	return json.Marshal(copy)
}

func verifyPayoutArtifact(a *payoutArtifact) error {
	if a == nil || a.Schema != "urnetwork-payout-artifact-v1" || !strings.HasPrefix(a.ContentHash, "sha256:") {
		return errors.New("invalid payout artifact schema")
	}
	b, err := payoutUnsignedBytes(a)
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	if a.ContentHash != "sha256:"+hex.EncodeToString(h[:]) {
		return errors.New("payout artifact hash mismatch")
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(a.Signature, "0x"))
	if err != nil || len(sig) != crypto.SignatureLength {
		return errors.New("invalid payout artifact signature")
	}
	pub, err := crypto.SigToPub(h[:], sig)
	if err != nil || crypto.PubkeyToAddress(*pub) != a.Signer {
		return errors.New("payout artifact signer mismatch")
	}
	leaves := make([]merkle.Leaf, len(a.Leaves))
	var total uint64
	for i, leaf := range a.Leaves {
		if leaf.Index != uint64(i) || leaf.ShareBPS == 0 {
			return errors.New("invalid payout leaf order")
		}
		total += leaf.ShareBPS
		leaves[i] = merkle.PayoutLeaf(leaf.Coldkey, new(big.Int).SetUint64(leaf.ShareBPS))
	}
	if len(leaves) == 0 {
		if a.PayoutRoot != ([32]byte{}) || a.SharesTotalBPS != 0 {
			return errors.New("empty payout artifact has a root")
		}
		return nil
	}
	if total != 10_000 || a.SharesTotalBPS != 10_000 {
		return fmt.Errorf("payout shares sum to %d", total)
	}
	tree, err := merkle.NewTree(leaves)
	if err != nil || tree.Root() != a.PayoutRoot {
		return errors.New("payout root mismatch")
	}
	for i, leaf := range a.Leaves {
		if !merkle.Verify(a.PayoutRoot, leaves[i], leaf.Proof) {
			return fmt.Errorf("payout proof %d is invalid", i)
		}
	}
	return nil
}

type assertionFile struct {
	Schema     string            `json:"schema"`
	Assertions []AssertionRecord `json:"assertions"`
}

type junitTestsuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []junitTestcase `xml:"testcase"`
}
type junitTestcase struct {
	Name    string        `xml:"name,attr"`
	Time    string        `xml:"time,attr,omitempty"`
	Failure *junitFailure `xml:"failure,omitempty"`
}
type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func writeJUnit(path, name string, assertions []AssertionRecord) error {
	suite := junitTestsuite{Name: name, Tests: len(assertions)}
	for _, assertion := range assertions {
		item := junitTestcase{Name: assertion.ID, Time: fmt.Sprintf("%.3f", assertion.DurationSeconds)}
		if !assertion.Passed {
			suite.Failures++
			item.Failure = &junitFailure{Message: assertion.Message, Body: assertion.ObservationHash}
		}
		suite.Cases = append(suite.Cases, item)
	}
	b, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	b = append([]byte(xml.Header), append(b, '\n')...)
	return atomicWrite(path, b, 0o644)
}

func evidenceFileHashes(root string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Base(path) == "complete.json" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		rel, _ := filepath.Rel(root, path)
		result[filepath.ToSlash(rel)] = "sha256:" + hex.EncodeToString(h[:])
		return nil
	})
	return result, err
}

func scanEvidenceSecrets(stateDir, runDir string, roles *RoleSecrets, walletSecrets ...string) error {
	var needles [][]byte
	for _, walletSecret := range walletSecrets {
		if strings.TrimSpace(walletSecret) != "" {
			needles = append(needles, []byte(walletSecret))
		}
	}
	if roles != nil {
		for _, role := range roles.EVM {
			needles = append(needles, []byte(role.PrivateKeyHex))
		}
		for _, role := range roles.Substrate {
			needles = append(needles, []byte(role.SeedHex))
		}
		for _, role := range roles.Clients {
			needles = append(needles, []byte(role.SeedHex))
		}
	}
	roots := []string{runDir, filepath.Join(stateDir, "public"), filepath.Join(stateDir, "public.json"), filepath.Join(stateDir, "processes")}
	for _, root := range roots {
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		visit := func(path string) error {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, needle := range needles {
				if len(needle) >= 8 && bytes.Contains(bytes.ToLower(b), bytes.ToLower(needle)) {
					return fmt.Errorf("secret material found in evidence file %s", path)
				}
			}
			return nil
		}
		if !info.IsDir() {
			if err := visit(root); err != nil {
				return err
			}
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			return visit(path)
		}); err != nil {
			return err
		}
	}
	return nil
}

func sortedAssertionIDs(assertions []AssertionRecord) []string {
	ids := make([]string, len(assertions))
	for i, assertion := range assertions {
		ids[i] = assertion.ID
	}
	sort.Strings(ids)
	return ids
}
