package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/ss58"
)

type ChainHead struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

type campaignFinalSemanticVerifier func(context.Context, *PublicDeploymentManifest, *FinalSemanticEvidence, string) error

// finalizedEVMHeadContextKey binds all nested reads in one postcondition to
// the exact finalized checkpoint selected by its caller. Without this, helper
// functions such as rawCoordinatorCall each resolve "finalized" again and a
// single durable observation can silently mix blocks (as well as multiplying
// public-RPC requests during resume audits).
type finalizedEVMHeadContextKey struct{}

func withFinalizedEVMHead(ctx context.Context, head ChainHead) (context.Context, error) {
	if ctx == nil {
		return nil, errors.New("EVM finalized-head context is unavailable")
	}
	if head.Number == 0 || head.Hash == "" {
		return nil, errors.New("EVM finalized checkpoint identity is incomplete")
	}
	if _, err := decodeHex32("EVM finalized block hash", head.Hash); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, finalizedEVMHeadContextKey{}, ChainHead{
		Number: head.Number,
		Hash:   strings.ToLower(head.Hash),
	}), nil
}

func boundFinalizedEVMHead(ctx context.Context) (ChainHead, bool) {
	if ctx == nil {
		return ChainHead{}, false
	}
	head, ok := ctx.Value(finalizedEVMHeadContextKey{}).(ChainHead)
	return head, ok
}

type ContractView struct {
	Deployment         *ContractDeployment `json:"deployment,omitempty"`
	CoordinatorUpgrade CoordinatorUpgrade  `json:"coordinator_upgrade"`
	FinalizedHead      ChainHead           `json:"finalized_head"`
	CurrentEpoch       uint64              `json:"current_epoch"`
	CurrentEpochStart  uint64              `json:"current_epoch_start_block"`
	CurrentEpochEnd    uint64              `json:"current_epoch_end_block"`
	CoordinatorOwner   string              `json:"coordinator_owner"`
	OperatorCount      uint64              `json:"operator_count"`
	PolicyHash         string              `json:"policy_hash,omitempty"`
	ConservationHolds  bool                `json:"conservation_holds"`
	MinimumTransferRao uint64              `json:"minimum_transfer_tao_rao"`
	TotalCaptured      string              `json:"total_captured_rao,omitempty"`
	TotalPaid          string              `json:"total_paid_rao,omitempty"`
	EscrowAccounted    string              `json:"escrow_accounted_rao,omitempty"`
	PendingFunding     string              `json:"pending_funding_rao,omitempty"`
	Outstanding        string              `json:"outstanding_liability_rao,omitempty"`
	LiveEscrowStake    string              `json:"live_escrow_stake_rao,omitempty"`
	ReservePrincipal   string              `json:"reserve_principal_rao,omitempty"`
	ReserveLiveStake   string              `json:"reserve_live_stake_rao,omitempty"`
	RuntimeCodeHashes  map[string]string   `json:"runtime_code_hashes,omitempty"`
	RuntimeCodeMatches bool                `json:"runtime_code_matches"`
	CustodyIdentity    ContractCustodyView `json:"custody_identity"`
	Policy             PolicyView          `json:"policy"`
	Operators          []OperatorView      `json:"operators"`
	Epochs             []EpochView         `json:"epochs"`
}

// ContractCustodyView records the complete immutable and one-shot-linked
// custody identity at the same finalized block as the accounting snapshot.
// These values are deliberately not inferred from deployment calldata: an
// external reviewer must be able to prove the live coordinator, vault and
// reserve are still wired to the intended netuid and contract-owned coldkeys.
type ContractCustodyView struct {
	CoordinatorNetuid                 uint16 `json:"coordinator_netuid"`
	CoordinatorSelfColdkey            string `json:"coordinator_self_coldkey"`
	CoordinatorGuardian               string `json:"coordinator_guardian"`
	CoordinatorActiveGuardian         string `json:"coordinator_active_guardian"`
	CoordinatorPaused                 bool   `json:"coordinator_paused"`
	CoordinatorCommitmentOracle       string `json:"coordinator_commitment_oracle"`
	CoordinatorActiveCommitmentOracle string `json:"coordinator_active_commitment_oracle"`
	CoordinatorVault                  string `json:"coordinator_settlement_vault"`
	CoordinatorReserve                string `json:"coordinator_reserve_sink"`
	VaultCoordinator                  string `json:"vault_coordinator"`
	VaultNetuid                       uint16 `json:"vault_netuid"`
	VaultSelfColdkey                  string `json:"vault_self_coldkey"`
	VaultEscrowHotkey                 string `json:"vault_escrow_hotkey"`
	VaultEscrowRegistered             bool   `json:"vault_escrow_registered"`
	VaultMinimumClaimTTLBlocks        uint64 `json:"vault_minimum_claim_ttl_blocks"`
	VaultMinimumTransferRao           uint64 `json:"vault_minimum_transfer_tao_rao"`
	ReserveRecorder                   string `json:"reserve_recorder"`
	ReserveNetuid                     uint16 `json:"reserve_netuid"`
	ReserveSelfColdkey                string `json:"reserve_self_coldkey"`
	ReserveHotkey                     string `json:"reserve_hotkey"`
}

type PolicyView struct {
	EffectiveEpoch         uint64 `json:"effective_epoch"`
	EffectiveBlock         uint64 `json:"effective_block"`
	EpochBlocks            uint64 `json:"epoch_blocks"`
	RootCommitWindowBlocks uint64 `json:"root_commit_window_blocks"`
	FinalizeOffsetBlocks   uint64 `json:"finalize_offset_blocks"`
	CloseGraceBlocks       uint64 `json:"close_grace_blocks"`
	ClaimTTLEpochs         uint64 `json:"claim_ttl_epochs"`
	ClaimGraceEpochs       uint64 `json:"claim_grace_epochs"`
	EpochDepositCapRao     string `json:"epoch_deposit_cap_rao"`
	CampaignDepositCapRao  string `json:"campaign_deposit_cap_rao"`
}

type OperatorView struct {
	NoID           uint64 `json:"no_id"`
	Coldkey        string `json:"coldkey"`
	PoolHotkey     string `json:"pool_hotkey"`
	DepositHotkey  string `json:"deposit_hotkey"`
	DepositSigner  string `json:"deposit_signer"`
	RootSigner     string `json:"root_signer"`
	EffectiveEpoch uint64 `json:"effective_epoch"`
	Active         bool   `json:"active"`
	PoolUID        uint16 `json:"pool_uid"`
	PoolLive       bool   `json:"pool_live"`
	ConvictionRao  string `json:"conviction_rao"`
	CarryRao       string `json:"carry_rao"`
}

type EpochOperatorView struct {
	NoID               uint64 `json:"no_id"`
	DepositRao         string `json:"deposit_rao"`
	ConvictionAddedRao string `json:"conviction_added_rao"`
	PayoutRoot         string `json:"payout_root"`
	ArtifactHash       string `json:"artifact_hash"`
	Committer          string `json:"committer,omitempty"`
	CommitBlock        uint64 `json:"commit_block"`
	FundedRao          string `json:"funded_rao"`
	TotalRao           string `json:"total_rao"`
	ClaimedRao         string `json:"claimed_rao"`
	ExpiryBlock        uint64 `json:"expiry_block"`
	Status             uint8  `json:"status"`
}

type EpochView struct {
	Epoch     uint64              `json:"epoch"`
	Operators []EpochOperatorView `json:"operators"`
}

type DeploymentStatus struct {
	Schema       string           `json:"schema"`
	GeneratedAt  string           `json:"generated_at"`
	DeploymentID string           `json:"deployment_id"`
	ConfigHash   string           `json:"config_hash"`
	PolicyHash   string           `json:"policy_hash"`
	Netuid       uint16           `json:"netuid"`
	Supervisor   *SupervisorState `json:"supervisor,omitempty"`
	Journal      JournalSummary   `json:"journal"`
	Contracts    *ContractView    `json:"contracts,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
	Healthy      bool             `json:"healthy"`
}

type JournalSummary struct {
	Entries       int             `json:"entries"`
	LastHash      string          `json:"last_hash,omitempty"`
	LatestByStage map[string]int  `json:"latest_by_stage,omitempty"`
	Actions       map[string]bool `json:"verified_actions,omitempty"`
}

func Status(ctx context.Context, cfg *ResolvedConfig, stateDir string) (*DeploymentStatus, error) {
	s := &DeploymentStatus{
		Schema:       "urnetwork-sim-status-v1",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		DeploymentID: cfg.Config.Deployment.DeploymentID,
		ConfigHash:   cfg.ConfigHash,
		PolicyHash:   cfg.PolicyHash,
		Netuid:       cfg.Netuid,
		Healthy:      true,
	}
	if b, err := os.ReadFile(filepath.Join(stateDir, "supervisor.state.json")); err == nil {
		var supervisor SupervisorState
		if err := json.Unmarshal(b, &supervisor); err != nil {
			return nil, fmt.Errorf("supervisor state: %w", err)
		}
		s.Supervisor = &supervisor
		if err := validateSupervisorGeneration(supervisor); err != nil {
			s.Warnings = append(s.Warnings, err.Error())
			s.Healthy = false
		}
		for _, process := range supervisor.Processes {
			if !process.Healthy || process.PID <= 1 {
				s.Healthy = false
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		s.Warnings = append(s.Warnings, "local supervisor has not started")
		s.Healthy = false
	}
	entries, err := readJournal(filepath.Join(stateDir, "journal.jsonl"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.Journal = summarizeJournal(entries)
	if _, err := os.Stat(filepath.Join(stateDir, "public", "contracts.json")); err == nil {
		view, viewErr := inspectContracts(ctx, cfg, stateDir, "")
		if viewErr != nil {
			s.Warnings = append(s.Warnings, viewErr.Error())
			s.Healthy = false
		} else {
			s.Contracts = view
			if !view.ConservationHolds || !view.RuntimeCodeMatches {
				s.Healthy = false
			}
		}
	}
	return s, nil
}

func Inspect(ctx context.Context, cfg *ResolvedConfig, stateDir, manifest string) (map[string]any, error) {
	view, err := inspectContracts(ctx, cfg, stateDir, manifest)
	if err != nil {
		return nil, err
	}
	identities := any(nil)
	var supervisor *SupervisorState
	journal := JournalSummary{}
	var publicManifest *PublicDeploymentManifest
	if manifest != "" {
		_, publicManifest, err = loadDeploymentReferenceForConfig(ctx, cfg, stateDir, manifest)
		if err != nil {
			return nil, err
		}
		if publicManifest != nil && len(publicManifest.Identities) != 0 {
			if err := json.Unmarshal(publicManifest.Identities, &identities); err != nil {
				return nil, err
			}
		}
	} else {
		status, statusErr := Status(ctx, cfg, stateDir)
		if statusErr != nil {
			return nil, statusErr
		}
		supervisor, journal = status.Supervisor, status.Journal
		identityPath := filepath.Join(stateDir, "public", "identities.json")
		if b, readErr := os.ReadFile(identityPath); readErr == nil {
			if unmarshalErr := json.Unmarshal(b, &identities); unmarshalErr != nil {
				return nil, unmarshalErr
			}
		}
	}
	return map[string]any{
		"schema":          "urnetwork-sim-inspect-v1",
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"deployment_id":   cfg.Config.Deployment.DeploymentID,
		"release":         "1.0",
		"chain_id":        cfg.Public.Chain.ChainID,
		"genesis_hash":    cfg.Public.Chain.GenesisHash,
		"netuid":          cfg.Netuid,
		"config_hash":     cfg.ConfigHash,
		"policy_hash":     cfg.PolicyHash,
		"contracts":       view,
		"identities":      identities,
		"processes":       supervisor,
		"journal":         journal,
		"public_manifest": publicManifest,
	}, nil
}

func Analyze(ctx context.Context, cfg *ResolvedConfig, stateDir, manifest string) (*AnalysisReport, error) {
	probe := &liveScenarioProbe{cfg: cfg, stateDir: stateDir, client: &http.Client{Timeout: 30 * time.Second}}
	if manifest != "" {
		probe.publicManifestURI = manifest
		_, public, loadErr := loadDeploymentReferenceForConfig(ctx, cfg, stateDir, manifest)
		if loadErr != nil {
			return nil, loadErr
		}
		if public == nil {
			return nil, errors.New("independent analysis requires a public deployment manifest, not a bare contract manifest")
		}
		if err := validatePublicCampaignOperatorOrigins(public); err != nil {
			return nil, err
		}
		bundle, bundleErr := probe.fetchLatestScenarioBundle(ctx, public)
		if bundleErr != nil {
			return nil, fmt.Errorf("public scenario evidence: %w", bundleErr)
		}
		if bundle.Analysis == nil {
			return nil, errors.New("public scenario evidence has no signed analysis")
		}
		report := *bundle.Analysis
		if cfg.Config.Analysis.WriteJSON || cfg.Config.Analysis.WriteHTML {
			if err := writeStandaloneAnalysis(stateDir, &report); err != nil {
				return nil, err
			}
		}
		return &report, nil
	}
	observation, err := probe.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	report := analyzeScenarioObservation(cfg, observation)
	if cfg.Config.Analysis.WriteJSON || cfg.Config.Analysis.WriteHTML {
		if err := writeStandaloneAnalysis(stateDir, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func publicEVMRole(raw json.RawMessage, label string) string {
	var identities struct {
		EVM map[string]string `json:"evm"`
	}
	if json.Unmarshal(raw, &identities) != nil {
		return ""
	}
	return identities.EVM[label]
}

func evidenceHistoryKeys(b []byte) []string {
	var result struct {
		Schema  string            `json:"schema"`
		Objects []json.RawMessage `json:"objects"`
	}
	if json.Unmarshal(b, &result) != nil || result.Schema != "urnetwork-release-evidence-history-v1" {
		return nil
	}
	keys := make([]string, 0, len(result.Objects))
	for _, raw := range result.Objects {
		var object struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(raw, &object) == nil && object.Key != "" {
			keys = append(keys, object.Key)
			continue
		}
		var key string
		if json.Unmarshal(raw, &key) == nil && key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func validatePublicCampaignOperatorOrigins(public *PublicDeploymentManifest) error {
	if public == nil || len(public.Operators) == 0 {
		return errors.New("public campaign operator directory is empty")
	}
	profile, err := effectivePublicEvidenceTransportProfile(public)
	if err != nil {
		return fmt.Errorf("public campaign evidence transport: %w", err)
	}
	if public.Topology.Operators != 0 && len(public.Operators) != public.Topology.Operators {
		return errors.New("public campaign operator directory does not match the signed topology")
	}
	seen := make(map[string]bool, len(public.Operators))
	seenOperators := make(map[int]bool, len(public.Operators))
	for index, operator := range public.Operators {
		if operator.NoID != index+1 || seenOperators[operator.NoID] {
			return fmt.Errorf("public campaign operator %d identity is invalid or duplicated", operator.NoID)
		}
		seenOperators[operator.NoID] = true
		parsed, err := url.Parse(operator.APIURL)
		origin, originErr := canonicalBarePublicEvidenceOrigin(operator.APIURL, profile, public.ChainID, public.GenesisHash)
		if err != nil || originErr != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
			return fmt.Errorf("public campaign operator %d API URL is not an allowed bare evidence origin", operator.NoID)
		}
		if profile == publicEvidenceTransportTestnetLoopbackHTTP && (operator.NoID < 1 || operator.NoID > len(testnetLoopbackEvidenceOrigins) || origin != testnetLoopbackEvidenceOrigins[operator.NoID-1]) {
			return fmt.Errorf("public campaign operator %d does not use its fixed testnet loopback evidence origin", operator.NoID)
		}
		if seen[origin] {
			return fmt.Errorf("public campaign operator %d duplicates API origin %q", operator.NoID, origin)
		}
		seen[origin] = true
		wantVerify := strings.TrimSuffix(operator.APIURL, "/") + "/verify"
		wantHistory := strings.TrimSuffix(operator.APIURL, "/") + "/sn/evidence/history"
		if operator.VerifyURL != wantVerify || operator.HistoryURL != wantHistory {
			return fmt.Errorf("public campaign operator %d verify/history URL is outside its authenticated API origin", operator.NoID)
		}
	}
	if profile == publicEvidenceTransportTestnetLoopbackHTTP && len(seen) != len(testnetLoopbackEvidenceOrigins) {
		return errors.New("testnet loopback evidence transport requires exactly two fixed operator origins")
	}
	return nil
}

func (p *liveScenarioProbe) fetchReplicatedCampaignEnvelope(ctx context.Context, public *PublicDeploymentManifest, hash, kind, runID, ownerSigner string) (*ReleaseEvidenceEnvelope, error) {
	if public == nil || !validSHA256ContentHash(hash) || kind == "" || runID == "" || !common.IsHexAddress(ownerSigner) {
		return nil, errors.New("campaign evidence fetch identity is invalid")
	}
	var firstBytes []byte
	var first *ReleaseEvidenceEnvelope
	for _, operator := range public.Operators {
		evidenceURL := strings.TrimSuffix(operator.APIURL, "/") + "/sn/evidence?hash=" + strings.ToLower(hash)
		encoded, _, err := p.get(ctx, evidenceURL, maximumCampaignEvidenceEnvelopeBytes)
		if err != nil {
			return nil, fmt.Errorf("operator %d campaign evidence %s: %w", operator.NoID, hash, err)
		}
		var envelope ReleaseEvidenceEnvelope
		if decodeStrictJSONBytes(encoded, &envelope) != nil || verifyEvidence(&envelope, nil) != nil || !strings.EqualFold(envelope.ContentHash, hash) || envelope.Kind != kind || envelope.RunID != runID || envelope.DeploymentID != public.DeploymentID || envelope.ChainID != public.ChainID || envelope.Netuid != public.Netuid || !strings.EqualFold(envelope.GenesisHash, public.GenesisHash) || !strings.EqualFold(envelope.Signer.Hex(), ownerSigner) {
			return nil, fmt.Errorf("operator %d returned invalid campaign evidence %s", operator.NoID, hash)
		}
		if first == nil {
			copyEnvelope := envelope
			first = &copyEnvelope
			firstBytes = append([]byte(nil), encoded...)
			continue
		}
		if !bytes.Equal(firstBytes, encoded) {
			return nil, fmt.Errorf("campaign evidence %s differs between operator replicas", hash)
		}
	}
	if first == nil || len(public.Operators) == 0 {
		return nil, errors.New("campaign evidence has no operator replicas")
	}
	return first, nil
}

func (p *liveScenarioProbe) resolveCampaignEvidenceOwner(ctx context.Context, public *PublicDeploymentManifest, result *ScenarioResult) (common.Address, error) {
	if p.trustedEvidenceOwner != (common.Address{}) {
		return p.trustedEvidenceOwner, nil
	}
	if p.cfg == nil || public == nil || public.Contracts == nil || public.Contracts.DeploymentID != public.DeploymentID || public.Contracts.CoordinatorProxy == (common.Address{}) || strings.TrimSpace(public.EVMRPC) == "" || result == nil || result.EndHead.Number == 0 || result.EndHead.Hash == "" {
		return common.Address{}, errors.New("historical campaign owner lookup has incomplete deployment or checkpoint identity")
	}
	if _, err := decodeHex32("campaign terminal EVM block hash", result.EndHead.Hash); err != nil {
		return common.Address{}, err
	}
	client, err := dialConfiguredEVMClient(ctx, p.cfg, public.EVMRPC)
	if err != nil {
		return common.Address{}, fmt.Errorf("historical EVM archive required for campaign owner lookup: %w", err)
	}
	defer client.Close()
	chainID, err := client.ChainID(ctx)
	if err != nil || !chainID.IsUint64() || chainID.Uint64() != public.ChainID {
		return common.Address{}, stateMismatchError(err, "historical campaign owner RPC chain identity=%v, want %d", chainID, public.ChainID)
	}
	finalized, err := finalizedEVMHead(ctx, client)
	if err != nil || finalized.Number < result.EndHead.Number {
		return common.Address{}, stateMismatchError(err, "campaign terminal EVM block %d is not finalized", result.EndHead.Number)
	}
	hash, err := canonicalEVMBlockHash(ctx, ethEVMBlockReader{client: client}, result.EndHead.Number)
	if err != nil {
		return common.Address{}, fmt.Errorf("historical EVM archive cannot serve campaign block %d: %w", result.EndHead.Number, err)
	}
	if !strings.EqualFold(hash, result.EndHead.Hash) {
		return common.Address{}, fmt.Errorf("campaign terminal EVM block %d hash %s does not match canonical %s", result.EndHead.Number, result.EndHead.Hash, hash)
	}
	coordinator, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return common.Address{}, err
	}
	values, err := contractCallAt(ctx, client, public.Contracts.CoordinatorProxy, coordinator, "owner", result.EndHead.Number)
	if err != nil {
		return common.Address{}, fmt.Errorf("historical EVM archive cannot read coordinator owner at campaign block %d: %w", result.EndHead.Number, err)
	}
	if len(values) != 1 {
		return common.Address{}, fmt.Errorf("coordinator owner at campaign block %d returned %d values", result.EndHead.Number, len(values))
	}
	owner, ok := values[0].(common.Address)
	if !ok || owner == (common.Address{}) {
		return common.Address{}, fmt.Errorf("coordinator owner at campaign block %d is invalid", result.EndHead.Number)
	}
	return owner, nil
}

func validateCampaignEvidenceJSONSchemas(files map[string][]byte) error {
	for name, raw := range files {
		switch {
		case strings.HasSuffix(name, ".json"):
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) != nil || len(object) == 0 {
				return fmt.Errorf("campaign evidence file %q is not a JSON object", name)
			}
			var schema string
			if json.Unmarshal(object["schema"], &schema) != nil || strings.TrimSpace(schema) == "" {
				return fmt.Errorf("campaign evidence file %q has no schema", name)
			}
		case strings.HasSuffix(name, ".jsonl"):
			lines := bytes.Split(bytes.TrimSpace(raw), []byte{'\n'})
			if len(lines) == 0 || len(lines[0]) == 0 {
				return fmt.Errorf("campaign evidence file %q is empty", name)
			}
			for index, line := range lines {
				var object map[string]json.RawMessage
				var schema string
				if json.Unmarshal(line, &object) != nil || json.Unmarshal(object["schema"], &schema) != nil || strings.TrimSpace(schema) == "" {
					return fmt.Errorf("campaign evidence file %q line %d has no valid schema", name, index+1)
				}
			}
		case strings.HasSuffix(name, ".xml"):
			decoder := xml.NewDecoder(bytes.NewReader(raw))
			for {
				if _, err := decoder.Token(); err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					return fmt.Errorf("campaign evidence file %q is invalid XML: %w", name, err)
				}
			}
		}
	}
	return nil
}

func validateCampaignEvidenceSemantics(files map[string][]byte, completion scenarioCompletePayload, bundle *ScenarioEvidenceBundle) error {
	required := []string{"result.json", "assertions.json", "anomalies.json", "adversaries.json", "analysis.json", "analysis.html", "junit.xml", "observations.jsonl", finalSemanticEvidenceFilename, finalSemanticMarkdownFilename}
	for _, name := range required {
		if len(files[name]) == 0 {
			return fmt.Errorf("campaign evidence required file %q is missing", name)
		}
	}
	if err := validateCampaignEvidenceJSONSchemas(files); err != nil {
		return err
	}
	if bundle == nil || bundle.Result == nil || bundle.Observation == nil || bundle.Analysis == nil {
		return errors.New("campaign evidence bundle is incomplete")
	}
	var result ScenarioResult
	if err := decodeStrictJSONBytes(files["result.json"], &result); err != nil {
		return fmt.Errorf("campaign evidence result: %w", err)
	}
	resultHash, err := canonicalScenarioResultHash(&result)
	if err != nil || result.Schema != "urnetwork-sim-scenario-result-v1" || !strings.EqualFold(result.EvidenceHash, completion.ResultHash) || !strings.EqualFold(resultHash, completion.ResultHash) || result.RunID != bundle.Result.RunID {
		return stateMismatchError(err, "campaign evidence result does not match the signed completion and bundle")
	}
	var assertions assertionFile
	if err := decodeStrictJSONBytes(files["assertions.json"], &assertions); err != nil || assertions.Schema != "urnetwork-sim-assertions-v1" || !reflect.DeepEqual(assertions.Assertions, result.Assertions) {
		return stateMismatchError(err, "campaign evidence assertions do not match the result")
	}
	var anomalies ScenarioAnomalyLedger
	if err := decodeStrictJSONBytes(files["anomalies.json"], &anomalies); err != nil || anomalies.Schema != "urnetwork-sim-anomaly-ledger-v1" || result.Anomalies == nil || !reflect.DeepEqual(&anomalies, result.Anomalies) {
		return stateMismatchError(err, "campaign evidence anomalies do not match the result")
	}
	var adversaries AdversaryCampaignEvidence
	if err := decodeStrictJSONBytes(files["adversaries.json"], &adversaries); err != nil || strings.TrimSpace(adversaries.Schema) == "" || result.Adversaries == nil || !reflect.DeepEqual(&adversaries, result.Adversaries) {
		return stateMismatchError(err, "campaign evidence adversaries do not match the result")
	}
	var analysis AnalysisReport
	if err := decodeStrictJSONBytes(files["analysis.json"], &analysis); err != nil || analysis.Schema != "urnetwork-sim-analysis-v1" || !reflect.DeepEqual(&analysis, bundle.Analysis) {
		return stateMismatchError(err, "campaign evidence analysis does not match the signed bundle")
	}
	if bundle.Observation.Schema != "urnetwork-sim-scenario-observation-v1" {
		return errors.New("campaign evidence bundle observation schema is invalid")
	}
	return nil
}

func verifyPublicFinalSemanticEvidence(ctx context.Context, public *PublicDeploymentManifest, evidence *FinalSemanticEvidence, evidenceURI string) error {
	reader, err := NewPublicFinalSemanticChainReader(ctx, public, evidence, evidenceURI)
	if err != nil {
		return err
	}
	verifyErr := VerifyFinalSemanticEvidenceOnChain(ctx, evidence, reader)
	return errors.Join(verifyErr, reader.Close())
}

type authenticatedCampaignSemantic struct {
	Evidence         *FinalSemanticEvidence
	EvidenceManifest *ReleaseEvidenceEnvelope
	PriorCompletion  *ReleaseEvidenceEnvelope
	PriorPayload     *scenarioCompletePayload
	PriorManifest    *ReleaseEvidenceEnvelope
	Artifacts        map[string][]byte
}

func authenticatePriorPhaseArtifacts(public *PublicDeploymentManifest, semantic *FinalSemanticEvidence, allFiles map[string][]byte) (*ReleaseEvidenceEnvelope, *scenarioCompletePayload, *ReleaseEvidenceEnvelope, error) {
	if semantic == nil || semantic.PriorPhase == nil {
		return nil, nil, nil, nil
	}
	prior := semantic.PriorPhase
	completionBytes := allFiles[prior.Completion.URI]
	manifestBytes := allFiles[prior.EvidenceManifest.URI]
	if len(completionBytes) == 0 || len(manifestBytes) == 0 {
		return nil, nil, nil, errors.New("prior release completion or evidence manifest is absent from the authenticated campaign graph")
	}
	var completion ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(completionBytes, &completion); err != nil || verifyEvidence(&completion, nil) != nil || public == nil || completion.Kind != "scenario-complete" || completion.RunID != prior.RunID || !strings.EqualFold(completion.ContentHash, prior.OwnerCompletionEnvelopeHash) || completion.DeploymentID != public.DeploymentID || completion.ChainID != public.ChainID || completion.Netuid != public.Netuid || !strings.EqualFold(completion.GenesisHash, public.GenesisHash) {
		return nil, nil, nil, stateMismatchError(err, "prior release owner completion is invalid")
	}
	var payload scenarioCompletePayload
	if err := decodeStrictJSONBytes(completion.Payload, &payload); err != nil || !strings.EqualFold(payload.ResultHash, prior.ResultHash) || !validSHA256ContentHash(payload.BundlePayloadHash) || !validSHA256ContentHash(payload.EvidenceManifestHash) || len(payload.Files) == 0 {
		return nil, nil, nil, stateMismatchError(err, "prior release completion payload is invalid")
	}
	var manifestEnvelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(manifestBytes, &manifestEnvelope); err != nil || verifyEvidence(&manifestEnvelope, nil) != nil || manifestEnvelope.Kind != campaignEvidenceManifestKind || manifestEnvelope.RunID != prior.RunID || !strings.EqualFold(manifestEnvelope.ContentHash, prior.EvidenceManifestEnvelopeHash) || manifestEnvelope.DeploymentID != public.DeploymentID || manifestEnvelope.ChainID != public.ChainID || manifestEnvelope.Netuid != public.Netuid || !strings.EqualFold(manifestEnvelope.GenesisHash, public.GenesisHash) || manifestEnvelope.Signer != completion.Signer || !strings.EqualFold(manifestEnvelope.ContentHash, payload.EvidenceManifestHash) {
		return nil, nil, nil, stateMismatchError(err, "prior release evidence manifest envelope is invalid")
	}
	manifest, err := decodeCampaignEvidenceManifest(&manifestEnvelope)
	if err != nil || !strings.EqualFold(manifest.ResultHash, prior.ResultHash) || !strings.EqualFold(manifest.BundlePayloadHash, payload.BundlePayloadHash) {
		return nil, nil, nil, stateMismatchError(err, "prior release evidence manifest does not bind its completion")
	}
	files, err := campaignEvidenceManifestFiles(manifest.Files)
	if err != nil || !stringMapsEqual(files, payload.Files) {
		return nil, nil, nil, stateMismatchError(err, "prior release evidence manifest files do not match its completion")
	}
	return &completion, &payload, &manifestEnvelope, nil
}

func (p *liveScenarioProbe) verifyCampaignFinalSemanticEvidence(ctx context.Context, public *PublicDeploymentManifest, ownerSigner string, bundle *ScenarioEvidenceBundle, files, referencedFiles, httpsFiles map[string][]byte) (*authenticatedCampaignSemantic, error) {
	type semanticObject struct {
		name string
		raw  []byte
	}
	semanticObjects := make([]semanticObject, 0, 1)
	allFiles := make(map[string][]byte, len(files)+len(referencedFiles)+len(httpsFiles))
	for _, source := range []map[string][]byte{files, referencedFiles, httpsFiles} {
		for name, raw := range source {
			allFiles[name] = raw
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			var header struct {
				Schema string `json:"schema"`
			}
			if json.Unmarshal(raw, &header) == nil && header.Schema == finalSemanticEvidenceSchema {
				semanticObjects = append(semanticObjects, semanticObject{name: name, raw: raw})
			}
		}
	}
	if len(semanticObjects) != 1 {
		return nil, fmt.Errorf("authenticated campaign graph contains %d final semantic evidence objects, want exactly 1", len(semanticObjects))
	}
	object := semanticObjects[0]
	var semantic FinalSemanticEvidence
	if err := decodeStrictJSONBytes(object.raw, &semantic); err != nil {
		return nil, fmt.Errorf("campaign final semantic evidence %q: %w", object.name, err)
	}
	if public == nil || public.Contracts == nil || bundle == nil || bundle.Result == nil || bundle.Result.AcceptanceWindow == nil || public.PlanHash == "" ||
		semantic.Phase != bundle.Result.Name || semantic.RunID != bundle.Result.RunID || !strings.EqualFold(semantic.ResultHash, bundle.Result.EvidenceHash) || semantic.DeploymentID != public.DeploymentID || semantic.PlanHash != public.PlanHash || semantic.ConfigHash != public.ConfigHash || !strings.EqualFold(semantic.PolicyHash, public.PolicyHash) ||
		semantic.ChainID != public.ChainID || semantic.Netuid != public.Netuid || !strings.EqualFold(semantic.GenesisHash, public.GenesisHash) || semantic.EVMTerminalHead != bundle.Result.EndHead ||
		!reflect.DeepEqual(semantic.Window, *bundle.Result.AcceptanceWindow) || semantic.ExpectedOperators != public.Topology.Operators || semantic.ExpectedValidators != public.Topology.Validators || semantic.ExpectedMiners != public.Topology.Miners ||
		semantic.ExpectedCandidates != public.Topology.HeadFleets+public.Topology.ChallengerFleets || semantic.ExpectedHeadSlots != public.Topology.HeadSlots ||
		!strings.EqualFold(semantic.Deployment.CoordinatorProxy, public.Contracts.CoordinatorProxy.Hex()) || !strings.EqualFold(semantic.Deployment.SettlementVault, public.Contracts.SettlementVault.Hex()) ||
		!strings.EqualFold(semantic.Deployment.ReserveSink, public.Contracts.ReserveSink.Hex()) || !strings.EqualFold(semantic.Deployment.CoordinatorImplementation, public.Contracts.CoordinatorImplementation.Hex()) ||
		!strings.EqualFold(semantic.Deployment.GovernanceOwner, ownerSigner) {
		return nil, errors.New("final semantic evidence does not bind the signed campaign, deployment, topology, and terminal checkpoint")
	}
	loader := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		raw, ok := allFiles[locator.URI]
		if !ok {
			return nil, fmt.Errorf("artifact locator %q is not in the authenticated campaign graph", locator.URI)
		}
		return append([]byte(nil), raw...), nil
	}
	if err := VerifyFinalSemanticArtifacts(ctx, &semantic, loader); err != nil {
		return nil, err
	}
	if semantic.PublicVerification == nil || semantic.PublicVerification.EvidenceURI == "" {
		return nil, errors.New("final semantic evidence does not bind a public deployment-manifest URI")
	}
	if p == nil || p.publicManifestURI == "" || !slices.ContainsFunc(semantic.PublicVerification.OperatorEvidenceOrigins, func(origin FinalOperatorEvidenceOrigin) bool {
		return origin.ManifestURI == p.publicManifestURI
	}) {
		return nil, errors.New("current deployment-manifest URI is not one of the two signed operator origins")
	}
	if p.finalSemanticVerify == nil {
		allowedOrigins, err := campaignArtifactAllowedOrigins(public, "")
		if err != nil {
			return nil, fmt.Errorf("final semantic deployment-manifest transport: %w", err)
		}
		for _, origin := range semantic.PublicVerification.OperatorEvidenceOrigins {
			if err := validateCampaignArtifactOrigin(origin.ManifestURI, allowedOrigins); err != nil {
				return nil, fmt.Errorf("final semantic operator %d deployment-manifest URI: %w", origin.OperatorNoID, err)
			}
		}
		publicManifestHash, err := canonicalHashHex(public)
		publicProfile, profileErr := effectivePublicEvidenceTransportProfile(public)
		if err != nil || profileErr != nil || semantic.PublicVerification.PublicManifestHash != publicManifestHash || semantic.PublicVerification.SubstrateRPC != public.SubstrateRPC || semantic.PublicVerification.EVMRPC != public.EVMRPC || semantic.PublicVerification.EvidenceTransportProfile != publicProfile {
			err = errors.Join(err, profileErr)
			return nil, stateMismatchError(err, "final semantic evidence does not bind the authenticated public manifest hash and exact public RPC endpoints")
		}
		if err := p.authenticateFinalOperatorManifestOrigins(ctx, public, semantic.PublicVerification); err != nil {
			return nil, err
		}
	}
	verify := p.finalSemanticVerify
	if verify == nil {
		verify = verifyPublicFinalSemanticEvidence
	}
	if err := verify(ctx, public, &semantic, semantic.PublicVerification.EvidenceURI); err != nil {
		return nil, fmt.Errorf("final semantic public archive replay: %w", err)
	}
	markdown, err := RenderFinalSemanticEvidenceMarkdown(&semantic)
	if err != nil {
		return nil, fmt.Errorf("render authenticated final semantic evidence: %w", err)
	}
	if !bytes.Equal(files[finalSemanticMarkdownFilename], markdown) {
		return nil, errors.New("authenticated FINAL.md does not match the sealed final semantic evidence")
	}
	priorCompletion, priorPayload, priorManifest, err := authenticatePriorPhaseArtifacts(public, &semantic, allFiles)
	if err != nil {
		return nil, err
	}
	return &authenticatedCampaignSemantic{Evidence: &semantic, PriorCompletion: priorCompletion, PriorPayload: priorPayload, PriorManifest: priorManifest, Artifacts: allFiles}, nil
}

func (p *liveScenarioProbe) authenticateFinalOperatorManifestOrigins(ctx context.Context, public *PublicDeploymentManifest, verification *FinalPublicChainVerification) error {
	if p == nil || public == nil || verification == nil {
		return errors.New("final operator manifest authentication context is incomplete")
	}
	var runtimeErr error
	if p.cfg == nil {
		runtimeErr = validatePublishedRuntimeIdentityShape(public)
	} else {
		runtimeErr = validatePublishedRuntimeIdentity(public, p.cfg)
	}
	if runtimeErr != nil {
		return stateMismatchError(runtimeErr, "final semantic deployment-manifest runtime identity differs from the release")
	}
	profile, err := effectivePublicEvidenceTransportProfile(public)
	if err != nil {
		return err
	}
	if err := validateFinalOperatorEvidenceOrigins(verification.OperatorEvidenceOrigins, verification.EvidenceURI, profile, public.ChainID, public.GenesisHash); err != nil {
		return err
	}
	payload, err := json.Marshal(public)
	if err != nil {
		return err
	}
	_, signers := inspectPublicIdentityBytesForManifest(public.Identities, public.DeploymentID, public.Topology)
	if len(signers) != 2 || len(public.Operators) != 2 {
		return errors.New("authenticated deployment manifest does not contain exactly two operator signers/origins")
	}
	seenSigners := map[string]bool{}
	for operator := 1; operator <= 2; operator++ {
		signer := strings.ToLower(signers[operator])
		if !common.IsHexAddress(signer) || common.HexToAddress(signer) == (common.Address{}) || seenSigners[signer] {
			return errors.New("authenticated deployment manifest operator signers are invalid or duplicated")
		}
		seenSigners[signer] = true
	}
	for index, origin := range verification.OperatorEvidenceOrigins {
		operator := public.Operators[index]
		manifestOrigin, manifestErr := publicEvidenceOrigin(origin.ManifestURI, profile, public.ChainID, public.GenesisHash)
		operatorOrigin, operatorErr := publicEvidenceOrigin(operator.APIURL, profile, public.ChainID, public.GenesisHash)
		if manifestErr != nil || operatorErr != nil || operator.NoID != origin.OperatorNoID || manifestOrigin != operatorOrigin {
			return stateMismatchError(errors.Join(manifestErr, operatorErr), "operator %d signed manifest URI is outside its authenticated origin", origin.OperatorNoID)
		}
		raw, _, readErr := p.get(ctx, origin.ManifestURI, 16*1024*1024)
		if readErr != nil {
			return fmt.Errorf("read operator %d signed deployment manifest: %w", origin.OperatorNoID, readErr)
		}
		envelope, envelopeErr := validateArchivedDeploymentManifestEnvelope(raw, public, payload, signers[origin.OperatorNoID])
		parsed, parseErr := url.Parse(origin.ManifestURI)
		values, queryErr := url.ParseQuery(parsed.RawQuery)
		if envelopeErr != nil || parseErr != nil || queryErr != nil || values.Get("hash") != envelope.ContentHash {
			return stateMismatchError(errors.Join(envelopeErr, parseErr, queryErr), "operator %d deployment-manifest URI does not resolve to its exact independently signed payload", origin.OperatorNoID)
		}
	}
	return nil
}

func (p *liveScenarioProbe) verifyPublicCampaignEvidence(ctx context.Context, public *PublicDeploymentManifest, ownerSigner string, complete *ReleaseEvidenceEnvelope, completion scenarioCompletePayload, bundle *ScenarioEvidenceBundle) (*authenticatedCampaignSemantic, error) {
	if complete == nil || !validSHA256ContentHash(completion.EvidenceManifestHash) {
		return nil, errors.New("scenario completion has no campaign evidence manifest")
	}
	manifestEnvelope, err := p.fetchReplicatedCampaignEnvelope(ctx, public, completion.EvidenceManifestHash, campaignEvidenceManifestKind, complete.RunID, ownerSigner)
	if err != nil {
		return nil, err
	}
	manifest, err := decodeCampaignEvidenceManifest(manifestEnvelope)
	if err != nil || !strings.EqualFold(manifest.ResultHash, completion.ResultHash) || !strings.EqualFold(manifest.BundlePayloadHash, completion.BundlePayloadHash) {
		return nil, stateMismatchError(err, "campaign evidence manifest does not bind the signed completion")
	}
	manifestFiles, err := campaignEvidenceManifestFiles(manifest.Files)
	if err != nil || !stringMapsEqual(manifestFiles, completion.Files) {
		return nil, stateMismatchError(err, "campaign evidence manifest files do not match the signed completion")
	}
	files := make(map[string][]byte, len(manifest.Files))
	referencedFiles := make(map[string][]byte, len(manifest.References))
	for _, group := range []struct {
		scope   string
		entries []campaignEvidenceFileEntry
		target  map[string][]byte
	}{{"run", manifest.Files, files}, {"reference", manifest.References, referencedFiles}} {
		for _, entry := range group.entries {
			envelope, err := p.fetchReplicatedCampaignEnvelope(ctx, public, entry.EnvelopeHash, campaignEvidenceFileKind, complete.RunID, ownerSigner)
			if err != nil {
				return nil, err
			}
			var payload campaignEvidenceFilePayload
			if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil || payload.Schema != campaignEvidenceFileSchema || payload.RunID != complete.RunID || payload.Scope != group.scope || payload.Path != entry.Path || payload.Size != entry.Size || !strings.EqualFold(payload.ContentHash, entry.ContentHash) || uint64(len(payload.Data)) != entry.Size || !strings.EqualFold(bytesSHA256(payload.Data), entry.ContentHash) {
				return nil, stateMismatchError(err, "campaign evidence %s file %q has invalid signed content", group.scope, entry.Path)
			}
			group.target[entry.Path] = append([]byte(nil), payload.Data...)
		}
	}
	if err := validateCampaignEvidenceSemantics(files, completion, bundle); err != nil {
		return nil, err
	}
	references := map[string]campaignArtifactReference{}
	edges := map[string]map[string]bool{}
	fileNames := make([]string, 0, len(files))
	var aggregate uint64
	for name, raw := range files {
		fileNames = append(fileNames, name)
		aggregate += uint64(len(raw))
	}
	for _, raw := range referencedFiles {
		aggregate += uint64(len(raw))
	}
	if aggregate > maximumCampaignEvidenceAggregateBytes {
		return nil, fmt.Errorf("campaign evidence graph exceeds %d aggregate bytes", maximumCampaignEvidenceAggregateBytes)
	}
	sort.Strings(fileNames)
	for _, name := range fileNames {
		if err := mergeCampaignArtifactSource(references, edges, name, files[name]); err != nil {
			return nil, err
		}
		if err := validateCampaignArtifactObjectCount(len(files), references); err != nil {
			return nil, err
		}
	}
	expectedReferences := make(map[string]string)
	httpsFiles := make(map[string][]byte)
	processedReferences := map[string]bool{}
	allowedOrigins, err := campaignArtifactAllowedOrigins(public, p.publicManifestURI)
	if err != nil {
		return nil, fmt.Errorf("campaign artifact transport: %w", err)
	}
	for len(processedReferences) < len(references) {
		names := make([]string, 0, len(references)-len(processedReferences))
		for name := range references {
			if !processedReferences[name] {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		name := names[0]
		processedReferences[name] = true
		reference := references[name]
		parsed, _ := url.Parse(name)
		var raw []byte
		if parsed.Scheme == "" {
			if runRaw, exists := files[name]; exists {
				raw = runRaw
				if uint64(len(raw)) != reference.Size || !strings.EqualFold(bytesSHA256(raw), reference.ContentHash) {
					return nil, fmt.Errorf("campaign artifact locator %q does not match its run file", name)
				}
			} else {
				expectedReferences[name] = reference.ContentHash
				var exists bool
				raw, exists = referencedFiles[name]
				if !exists || uint64(len(raw)) != reference.Size || !strings.EqualFold(bytesSHA256(raw), reference.ContentHash) {
					return nil, fmt.Errorf("campaign referenced artifact %q is unavailable or has invalid content", name)
				}
			}
		} else {
			if err := validateCampaignArtifactOrigin(name, allowedOrigins); err != nil {
				return nil, err
			}
			if reference.Size > maximumCampaignEvidenceAggregateBytes-aggregate {
				return nil, fmt.Errorf("campaign evidence graph exceeds %d aggregate bytes", maximumCampaignEvidenceAggregateBytes)
			}
			var err error
			raw, _, err = p.get(ctx, name, int64(reference.Size))
			if err != nil || uint64(len(raw)) != reference.Size || !strings.EqualFold(bytesSHA256(raw), reference.ContentHash) {
				return nil, stateMismatchError(err, "public campaign artifact %q is unavailable or has invalid content", name)
			}
			httpsFiles[name] = raw
			aggregate += uint64(len(raw))
		}
		if err := mergeCampaignArtifactSource(references, edges, name, raw); err != nil {
			return nil, err
		}
		if err := validateCampaignArtifactObjectCount(len(files), references); err != nil {
			return nil, err
		}
	}
	if err := validateCampaignArtifactGraph(edges); err != nil {
		return nil, err
	}
	manifestReferences, err := campaignEvidenceEntryFiles(manifest.References)
	if err != nil || !stringMapsEqual(manifestReferences, expectedReferences) {
		return nil, stateMismatchError(err, "campaign evidence manifest external references do not match its raw locators")
	}
	authenticated, err := p.verifyCampaignFinalSemanticEvidence(ctx, public, ownerSigner, bundle, files, referencedFiles, httpsFiles)
	if err != nil {
		return nil, err
	}
	authenticated.EvidenceManifest = manifestEnvelope
	return authenticated, nil
}

type publicScenarioCandidate struct {
	bundle       *ScenarioEvidenceBundle
	payload      string
	payloadBytes []byte
	signers      map[int]bool
	time         time.Time
}

type publicScenarioCompletionCandidate struct {
	envelope     *ReleaseEvidenceEnvelope
	payload      scenarioCompletePayload
	payloadBytes []byte
	operators    map[int]bool
}

type authenticatedPublicScenarioCandidate struct {
	candidate  *publicScenarioCandidate
	completion *publicScenarioCompletionCandidate
	semantic   *authenticatedCampaignSemantic
	prior      *authenticatedPublicScenarioCandidate
}

// AuthenticatedPublicCampaign is the complete secretless replay input rooted
// in one all-operator history commit. Artifacts contains only bytes reached
// through the signed, bounded campaign manifest graph.
type AuthenticatedPublicCampaign struct {
	PublicManifestURI string
	PublicManifest    *PublicDeploymentManifest
	Bundle            *ScenarioEvidenceBundle
	Semantic          *FinalSemanticEvidence
	OwnerCompletion   *ReleaseEvidenceEnvelope
	EvidenceManifest  *ReleaseEvidenceEnvelope
	Artifacts         map[string][]byte
	Prior             *AuthenticatedPublicCampaign
}

func (campaign *AuthenticatedPublicCampaign) ArtifactLoader() FinalArtifactLoader {
	return func(ctx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		if campaign == nil {
			return nil, errors.New("authenticated public campaign is unavailable")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, ok := campaign.Artifacts[locator.URI]
		if !ok {
			return nil, fmt.Errorf("artifact locator %q is outside the authenticated public campaign graph", locator.URI)
		}
		return append([]byte(nil), raw...), nil
	}
}

func materializeAuthenticatedPublicCampaign(public *PublicDeploymentManifest, manifestURI string, authenticated *authenticatedPublicScenarioCandidate) *AuthenticatedPublicCampaign {
	if authenticated == nil || authenticated.semantic == nil {
		return nil
	}
	artifacts := make(map[string][]byte, len(authenticated.semantic.Artifacts))
	for name, raw := range authenticated.semantic.Artifacts {
		artifacts[name] = append([]byte(nil), raw...)
	}
	result := &AuthenticatedPublicCampaign{
		PublicManifestURI: manifestURI,
		PublicManifest:    public,
		Bundle:            authenticated.candidate.bundle,
		Semantic:          authenticated.semantic.Evidence,
		OwnerCompletion:   authenticated.completion.envelope,
		EvidenceManifest:  authenticated.semantic.EvidenceManifest,
		Artifacts:         artifacts,
	}
	result.Prior = materializeAuthenticatedPublicCampaign(public, manifestURI, authenticated.prior)
	return result
}

func evidenceEnvelopesEqual(left, right *ReleaseEvidenceEnvelope) bool {
	return left != nil && right != nil && left.ContentHash == right.ContentHash && left.Signature == right.Signature && reflect.DeepEqual(left, right)
}

func validateAuthenticatedPublicPhaseLineage(current, prior *authenticatedPublicScenarioCandidate) error {
	if current == nil || current.candidate == nil || current.candidate.bundle == nil || current.candidate.bundle.Result == nil || current.candidate.bundle.Result.AcceptanceWindow == nil || current.completion == nil || current.semantic == nil || current.semantic.Evidence == nil {
		return errors.New("current public campaign phase is incomplete")
	}
	result := current.candidate.bundle.Result
	semantic := current.semantic.Evidence
	if result.Name == "release-1.0" {
		if semantic.Phase != "release-1.0" || semantic.PriorPhase != nil || prior != nil {
			return errors.New("release-1.0 public campaign must not claim a predecessor")
		}
		return nil
	}
	if result.Name != "production-soak" || semantic.Phase != "production-soak" || semantic.PriorPhase == nil {
		return errors.New("production public campaign lacks an authenticated release-1.0 predecessor")
	}
	if prior == nil || prior.candidate == nil || prior.candidate.bundle == nil || prior.candidate.bundle.Result == nil || prior.candidate.bundle.Result.AcceptanceWindow == nil || prior.completion == nil || prior.semantic == nil || prior.semantic.Evidence == nil || prior.semantic.EvidenceManifest == nil {
		return errors.New("production public campaign predecessor is incomplete")
	}
	priorResult := prior.candidate.bundle.Result
	priorSemantic := prior.semantic.Evidence
	binding := semantic.PriorPhase
	if priorResult.Name != "release-1.0" || priorSemantic.Phase != "release-1.0" || priorSemantic.PriorPhase != nil ||
		binding.RunID != priorResult.RunID || !strings.EqualFold(binding.ResultHash, priorResult.EvidenceHash) ||
		!strings.EqualFold(binding.OwnerCompletionEnvelopeHash, prior.completion.envelope.ContentHash) || !strings.EqualFold(binding.EvidenceManifestEnvelopeHash, prior.semantic.EvidenceManifest.ContentHash) ||
		!reflect.DeepEqual(binding.AcceptanceWindow, *priorResult.AcceptanceWindow) || binding.TerminalEVMHead != priorResult.EndHead ||
		binding.TerminalNativeHead != priorSemantic.NativeTerminalHead ||
		result.DeploymentID != priorResult.DeploymentID || result.ConfigHash != priorResult.ConfigHash || !strings.EqualFold(result.PolicyHash, priorResult.PolicyHash) ||
		result.ChainID != priorResult.ChainID || result.Netuid != priorResult.Netuid || !strings.EqualFold(result.GenesisHash, priorResult.GenesisHash) {
		return errors.New("production public campaign predecessor does not bind the authenticated release result and semantic checkpoints")
	}
	if current.semantic.PriorCompletion == nil || current.semantic.PriorPayload == nil || current.semantic.PriorManifest == nil ||
		!evidenceEnvelopesEqual(current.semantic.PriorCompletion, prior.completion.envelope) ||
		!reflect.DeepEqual(*current.semantic.PriorPayload, prior.completion.payload) ||
		!evidenceEnvelopesEqual(current.semantic.PriorManifest, prior.semantic.EvidenceManifest) {
		return errors.New("production public campaign predecessor objects do not match the independently replicated release history")
	}
	priorCompleted, completedErr := time.Parse(time.RFC3339Nano, priorResult.CompletedAt)
	currentStarted, startedErr := time.Parse(time.RFC3339Nano, result.StartedAt)
	if completedErr != nil || startedErr != nil || currentStarted.Before(priorCompleted) || priorResult.EndHead.Number >= result.AcceptanceWindow.BaselineHead.Number {
		return errors.Join(completedErr, startedErr, errors.New("production public campaign does not follow the authenticated release boundary"))
	}
	return nil
}

func findPublicCampaignPredecessor(semantic *authenticatedCampaignSemantic, candidates map[string]*publicScenarioCandidate, completions map[string]*publicScenarioCompletionCandidate, operatorCount int) (*publicScenarioCandidate, *publicScenarioCompletionCandidate, error) {
	if semantic == nil || semantic.Evidence == nil || semantic.Evidence.PriorPhase == nil || semantic.PriorCompletion == nil || semantic.PriorPayload == nil || operatorCount <= 0 {
		return nil, nil, errors.New("production public campaign has no authenticated predecessor objects")
	}
	encodedCompletion, err := json.Marshal(semantic.PriorCompletion)
	if err != nil {
		return nil, nil, err
	}
	completion := completions[bytesSHA256(encodedCompletion)]
	candidate := candidates[semantic.PriorCompletion.RunID+"\x00"+semantic.PriorPayload.BundlePayloadHash]
	if completion == nil || candidate == nil {
		return nil, nil, errors.New("production predecessor is absent from replicated public scenario history")
	}
	if len(completion.operators) != operatorCount || len(candidate.signers) != operatorCount || !bytes.Equal(completion.payloadBytes, encodedCompletion) || !evidenceEnvelopesEqual(semantic.PriorCompletion, completion.envelope) || !reflect.DeepEqual(*semantic.PriorPayload, completion.payload) {
		return nil, nil, errors.New("production predecessor does not have byte-identical bundle and completion commits at every operator")
	}
	return candidate, completion, nil
}

func selectAuthenticatedPublicCampaign(current, candidate *authenticatedPublicScenarioCandidate, requestedRunID string) (*authenticatedPublicScenarioCandidate, error) {
	if candidate == nil || candidate.candidate == nil || candidate.candidate.bundle == nil || candidate.candidate.bundle.Result == nil || candidate.completion == nil || candidate.completion.envelope == nil {
		return nil, errors.New("authenticated public campaign selection candidate is incomplete")
	}
	if current == nil {
		return candidate, nil
	}
	if current.candidate == nil || current.candidate.bundle == nil || current.candidate.bundle.Result == nil || current.completion == nil || current.completion.envelope == nil {
		return nil, errors.New("authenticated public campaign selection state is incomplete")
	}
	currentResult := current.candidate.bundle.Result
	candidateResult := candidate.candidate.bundle.Result
	sameIdentity := currentResult.RunID == candidateResult.RunID && current.candidate.payload == candidate.candidate.payload && evidenceEnvelopesEqual(current.completion.envelope, candidate.completion.envelope)
	if requestedRunID != "" {
		if !sameIdentity {
			return nil, fmt.Errorf("public campaign run %q has multiple authenticated signed bundles or owner completions", requestedRunID)
		}
		return current, nil
	}
	if candidate.candidate.time.After(current.candidate.time) {
		return candidate, nil
	}
	if candidate.candidate.time.Equal(current.candidate.time) && !sameIdentity {
		return nil, errors.New("public campaign selection is ambiguous at the latest completion time; specify an exact run id")
	}
	return current, nil
}

func (p *liveScenarioProbe) fetchAuthenticatedScenarioCampaign(ctx context.Context, public *PublicDeploymentManifest, requestedRunID, requestedPhase string) (*authenticatedPublicScenarioCandidate, error) {
	if public == nil {
		return nil, errors.New("public deployment manifest is missing")
	}
	if requestedRunID != "" && (requestedRunID != strings.TrimSpace(requestedRunID) || strings.ContainsAny(requestedRunID, "/\\\r\n\x00")) {
		return nil, errors.New("requested public campaign run id is invalid")
	}
	if requestedPhase != "" && requestedPhase != "release-1.0" && requestedPhase != "production-soak" {
		return nil, fmt.Errorf("requested public campaign phase %q is invalid", requestedPhase)
	}
	_, expected := inspectPublicIdentityBytes(p.cfg, public.Identities)
	if len(expected) != len(public.Operators) {
		return nil, errors.New("public scenario evidence signer directory is invalid")
	}
	candidates := map[string]*publicScenarioCandidate{}
	completions := map[string]*publicScenarioCompletionCandidate{}
	for _, operator := range public.Operators {
		if operator.NoID < 1 || operator.NoID > len(public.Operators) || expected[operator.NoID] == "" || operator.APIURL == "" || operator.HistoryURL == "" {
			return nil, errors.New("public scenario evidence operator directory is invalid")
		}
		loadHistory := func(kind string) ([]string, error) {
			history, err := url.Parse(operator.HistoryURL)
			if err != nil {
				return nil, err
			}
			query := history.Query()
			query.Set("deployment_id", public.DeploymentID)
			query.Set("netuid", fmt.Sprint(public.Netuid))
			query.Set("kind", kind)
			history.RawQuery = query.Encode()
			body, _, err := p.get(ctx, history.String(), 16*1024*1024)
			if err != nil {
				return nil, err
			}
			return evidenceHistoryKeys(body), nil
		}
		bundleKeys, err := loadHistory("scenario-bundle")
		if err != nil {
			return nil, fmt.Errorf("operator %d scenario-bundle history: %w", operator.NoID, err)
		}
		if len(bundleKeys) == 0 {
			return nil, fmt.Errorf("operator %d has no scenario-bundle history", operator.NoID)
		}
		for _, key := range bundleKeys {
			hash := strings.TrimSuffix(filepath.Base(key), filepath.Ext(key))
			if len(hash) != 64 {
				continue
			}
			evidenceURL := strings.TrimSuffix(operator.APIURL, "/") + "/sn/evidence?hash=sha256:" + hash
			encoded, _, fetchErr := p.get(ctx, evidenceURL, 64*1024*1024)
			if fetchErr != nil {
				continue
			}
			var envelope ReleaseEvidenceEnvelope
			if json.Unmarshal(encoded, &envelope) != nil || verifyEvidence(&envelope, nil) != nil || envelope.Kind != "scenario-bundle" || envelope.DeploymentID != public.DeploymentID || envelope.ChainID != public.ChainID || envelope.Netuid != public.Netuid || !strings.EqualFold(envelope.GenesisHash, public.GenesisHash) || !strings.EqualFold(envelope.Signer.Hex(), expected[operator.NoID]) {
				continue
			}
			var bundle ScenarioEvidenceBundle
			if decodeStrictJSONBytes(envelope.Payload, &bundle) != nil || bundle.Schema != "urnetwork-sim-scenario-evidence-v1" || bundle.Result == nil || bundle.Observation == nil || bundle.Analysis == nil || bundle.Result.Schema != "urnetwork-sim-scenario-result-v1" || bundle.Result.Release != "1.0" || bundle.Result.RunID == "" || bundle.Result.Result != "pass" || bundle.Result.DeploymentID != public.DeploymentID || bundle.Result.ChainID != public.ChainID || !strings.EqualFold(bundle.Result.GenesisHash, public.GenesisHash) || bundle.Result.Netuid != public.Netuid || bundle.Result.ConfigHash != public.ConfigHash || !strings.EqualFold(bundle.Result.PolicyHash, public.PolicyHash) {
				continue
			}
			resultHash, hashErr := canonicalScenarioResultHash(bundle.Result)
			if hashErr != nil || !strings.EqualFold(resultHash, bundle.Result.EvidenceHash) {
				continue
			}
			verifyResult := p.campaignResultVerify
			if verifyResult == nil {
				verifyResult = validateScenarioCampaignResult
			}
			if verifyResult(p.cfg, bundle.Result, bundle.Result.Name) != nil {
				continue
			}
			payloadHash := bytesSHA256(envelope.Payload)
			completed, _ := time.Parse(time.RFC3339Nano, bundle.Result.CompletedAt)
			candidateKey := bundle.Result.RunID + "\x00" + payloadHash
			item := candidates[candidateKey]
			if item == nil {
				copy := bundle
				item = &publicScenarioCandidate{bundle: &copy, payload: payloadHash, payloadBytes: append([]byte(nil), envelope.Payload...), signers: map[int]bool{}, time: completed}
				candidates[candidateKey] = item
			} else if !bytes.Equal(item.payloadBytes, envelope.Payload) {
				return nil, errors.New("operator scenario bundle payloads share a hash but differ in exact bytes")
			}
			item.signers[operator.NoID] = true
		}

		completionKeys, err := loadHistory("scenario-complete-commit")
		if err != nil {
			return nil, fmt.Errorf("operator %d scenario-complete-commit history: %w", operator.NoID, err)
		}
		for _, key := range completionKeys {
			hash := strings.TrimSuffix(filepath.Base(key), filepath.Ext(key))
			if len(hash) != 64 {
				continue
			}
			evidenceURL := strings.TrimSuffix(operator.APIURL, "/") + "/sn/evidence?hash=sha256:" + hash
			encoded, _, fetchErr := p.get(ctx, evidenceURL, 64*1024*1024)
			if fetchErr != nil {
				continue
			}
			var operatorEnvelope ReleaseEvidenceEnvelope
			if decodeStrictJSONBytes(encoded, &operatorEnvelope) != nil || verifyEvidence(&operatorEnvelope, nil) != nil || operatorEnvelope.Kind != "scenario-complete-commit" || operatorEnvelope.RunID == "" || operatorEnvelope.DeploymentID != public.DeploymentID || operatorEnvelope.ChainID != public.ChainID || operatorEnvelope.Netuid != public.Netuid || !strings.EqualFold(operatorEnvelope.GenesisHash, public.GenesisHash) || !strings.EqualFold(operatorEnvelope.Signer.Hex(), expected[operator.NoID]) {
				continue
			}
			var envelope ReleaseEvidenceEnvelope
			if decodeStrictJSONBytes(operatorEnvelope.Payload, &envelope) != nil || verifyEvidence(&envelope, nil) != nil || envelope.Kind != "scenario-complete" || envelope.RunID != operatorEnvelope.RunID || envelope.DeploymentID != public.DeploymentID || envelope.ChainID != public.ChainID || envelope.Netuid != public.Netuid || !strings.EqualFold(envelope.GenesisHash, public.GenesisHash) {
				continue
			}
			var payload scenarioCompletePayload
			if decodeStrictJSONBytes(envelope.Payload, &payload) != nil || !validCanonicalHashHex(payload.ResultHash) || !validSHA256ContentHash(payload.BundlePayloadHash) || !validSHA256ContentHash(payload.EvidenceManifestHash) || len(payload.Files) == 0 {
				continue
			}
			completionKey := bytesSHA256(operatorEnvelope.Payload)
			item := completions[completionKey]
			if item == nil {
				copyEnvelope := envelope
				item = &publicScenarioCompletionCandidate{envelope: &copyEnvelope, payload: payload, payloadBytes: append([]byte(nil), operatorEnvelope.Payload...), operators: map[int]bool{}}
				completions[completionKey] = item
			} else if !bytes.Equal(item.payloadBytes, operatorEnvelope.Payload) {
				return nil, errors.New("operator completion payloads share a hash but differ in exact bytes")
			}
			item.operators[operator.NoID] = true
		}
	}
	var latest *authenticatedPublicScenarioCandidate
	var verificationErr error
	authenticated := map[string]*authenticatedPublicScenarioCandidate{}
	visiting := map[string]bool{}
	var authenticate func(*publicScenarioCandidate, *publicScenarioCompletionCandidate) (*authenticatedPublicScenarioCandidate, error)
	authenticate = func(item *publicScenarioCandidate, completion *publicScenarioCompletionCandidate) (*authenticatedPublicScenarioCandidate, error) {
		if item == nil || item.bundle == nil || item.bundle.Result == nil || completion == nil || completion.envelope == nil || len(item.signers) != len(public.Operators) || len(completion.operators) != len(public.Operators) ||
			completion.envelope.RunID != item.bundle.Result.RunID || !strings.EqualFold(completion.payload.ResultHash, item.bundle.Result.EvidenceHash) || !strings.EqualFold(completion.payload.BundlePayloadHash, item.payload) {
			return nil, errors.New("scenario campaign candidate and completion identity is incomplete")
		}
		key := item.bundle.Result.RunID + "\x00" + item.payload + "\x00" + completion.envelope.ContentHash + "\x00" + completion.envelope.Signature
		if result := authenticated[key]; result != nil {
			return result, nil
		}
		if visiting[key] {
			return nil, errors.New("public campaign phase lineage contains a cycle")
		}
		visiting[key] = true
		defer delete(visiting, key)
		owner, err := p.resolveCampaignEvidenceOwner(ctx, public, item.bundle.Result)
		if err != nil {
			return nil, err
		}
		if owner != completion.envelope.Signer {
			return nil, errors.New("scenario completion owner does not match the coordinator owner at the campaign terminal block")
		}
		semantic, err := p.verifyPublicCampaignEvidence(ctx, public, owner.Hex(), completion.envelope, completion.payload, item.bundle)
		if err != nil {
			return nil, err
		}
		current := &authenticatedPublicScenarioCandidate{candidate: item, completion: completion, semantic: semantic}
		if semantic.Evidence.Phase == "release-1.0" {
			if err := validateAuthenticatedPublicPhaseLineage(current, nil); err != nil {
				return nil, err
			}
		} else {
			if semantic.PriorCompletion == nil || semantic.PriorPayload == nil {
				return nil, errors.New("production public campaign has no authenticated predecessor objects")
			}
			priorItem, priorCompletion, err := findPublicCampaignPredecessor(semantic, candidates, completions, len(public.Operators))
			if err != nil {
				return nil, err
			}
			prior, err := authenticate(priorItem, priorCompletion)
			if err != nil {
				return nil, fmt.Errorf("authenticate production predecessor: %w", err)
			}
			if err := validateAuthenticatedPublicPhaseLineage(current, prior); err != nil {
				return nil, err
			}
			current.prior = prior
		}
		authenticated[key] = current
		return current, nil
	}
	for _, item := range candidates {
		if len(item.signers) != len(public.Operators) || requestedRunID != "" && item.bundle.Result.RunID != requestedRunID || requestedPhase != "" && item.bundle.Result.Name != requestedPhase {
			continue
		}
		var committed *authenticatedPublicScenarioCandidate
		ambiguousCompletion := false
		for _, completion := range completions {
			if len(completion.operators) == len(public.Operators) && completion.envelope.RunID == item.bundle.Result.RunID && strings.EqualFold(completion.payload.ResultHash, item.bundle.Result.EvidenceHash) && strings.EqualFold(completion.payload.BundlePayloadHash, item.payload) {
				if verified, err := authenticate(item, completion); err == nil {
					if committed != nil && !evidenceEnvelopesEqual(committed.completion.envelope, verified.completion.envelope) {
						verificationErr = errors.New("public campaign has multiple fully replicated owner completions for one signed bundle")
						ambiguousCompletion = true
						break
					}
					committed = verified
				} else {
					verificationErr = err
				}
			}
		}
		if committed == nil || ambiguousCompletion {
			continue
		}
		selected, selectionErr := selectAuthenticatedPublicCampaign(latest, committed, requestedRunID)
		if selectionErr != nil {
			return nil, selectionErr
		}
		latest = selected
	}
	if latest == nil {
		if verificationErr != nil {
			return nil, fmt.Errorf("no scenario bundle has a fully authenticated public completion: %w", verificationErr)
		}
		return nil, errors.New("no scenario bundle has byte-identical operator signatures and a replicated owner completion commit")
	}
	return latest, nil
}

func (p *liveScenarioProbe) fetchLatestScenarioBundle(ctx context.Context, public *PublicDeploymentManifest) (*ScenarioEvidenceBundle, error) {
	authenticated, err := p.fetchAuthenticatedScenarioCampaign(ctx, public, "", "")
	if err != nil {
		return nil, err
	}
	return authenticated.candidate.bundle, nil
}

// FetchAuthenticatedPublicCampaign starts only from a public manifest URI
// admitted by the configured evidence transport profile. It performs the
// all-operator history walk, recursive phase authentication, closed artifact
// fetch, and pinned archive replay before returning any campaign.
func FetchAuthenticatedPublicCampaign(ctx context.Context, cfg *ResolvedConfig, manifestURI, runID, phase string) (*AuthenticatedPublicCampaign, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil {
		return nil, errors.New("public campaign replay context is incomplete")
	}
	profile, err := resolvedPublicEvidenceTransportProfile(cfg)
	if err != nil {
		return nil, fmt.Errorf("public campaign evidence transport: %w", err)
	}
	if err := verifyFinalEvidenceURI("public campaign manifest", manifestURI, profile, cfg.ChainID, cfg.Public.Chain.GenesisHash); err != nil {
		return nil, err
	}
	if err := adoptPublicManifest(ctx, cfg, manifestURI); err != nil {
		return nil, err
	}
	_, public, err := loadDeploymentReferenceForConfig(ctx, cfg, "", manifestURI)
	if err != nil || public == nil {
		return nil, stateMismatchError(err, "authenticate public campaign deployment manifest")
	}
	if err := validatePublicCampaignOperatorOrigins(public); err != nil {
		return nil, err
	}
	probe := &liveScenarioProbe{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}, publicManifestURI: manifestURI}
	authenticated, err := probe.fetchAuthenticatedScenarioCampaign(ctx, public, runID, phase)
	if err != nil {
		return nil, err
	}
	return materializeAuthenticatedPublicCampaign(public, manifestURI, authenticated), nil
}

// adoptPublicManifest lets a clean second checkout inspect a deployment
// without copying the launch host's filled testnet vault. The checked-in
// release config and policy still have to hash to the public manifest; only
// deployment facts discovered by setup are adopted.
func adoptPublicManifest(ctx context.Context, cfg *ResolvedConfig, source string) error {
	profile, err := resolvedPublicEvidenceTransportProfile(cfg)
	if err != nil {
		return fmt.Errorf("public manifest evidence transport: %w", err)
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		if err := verifyFinalEvidenceURI("public deployment manifest", source, profile, cfg.ChainID, cfg.Public.Chain.GenesisHash); err != nil {
			return err
		}
	}
	_, public, err := loadDeploymentReferenceWithTransport(ctx, "", source, profile, cfg.ChainID, cfg.Public.Chain.GenesisHash)
	if err != nil {
		return err
	}
	if public == nil {
		return errors.New("independent inspect/analyze requires a public deployment manifest")
	}
	lockHash, err := canonicalHashHex(cfg.Release)
	if err != nil {
		return err
	}
	topologyHash, _ := canonicalHashHex(cfg.Config.Topology)
	publicTopologyHash, _ := canonicalHashHex(public.Topology)
	if public.ConfigHash != cfg.ConfigHash || !strings.EqualFold(public.PolicyHash, cfg.PolicyHash) || public.ReleaseLockHash != lockHash || topologyHash != publicTopologyHash {
		return errors.New("public manifest does not match this checkout's config, policy, release lock, topology, and runtime pin")
	}
	if err := validatePublishedRuntimeIdentity(public, cfg); err != nil {
		return err
	}
	if len(public.Operators) != cfg.Config.Topology.Operators {
		return errors.New("public manifest operator topology is incomplete")
	}
	if err := validatePublicEvidenceManifestTransportAgainstConfig(cfg, public); err != nil {
		return err
	}
	cfg.Netuid = public.Netuid
	cfg.ChainID = public.ChainID
	cfg.Public.Chain.EVMPublicReadEndpoint = public.EVMRPC
	cfg.Public.Chain.SubstratePublicReadEndpoint = public.SubstrateRPC
	return nil
}

func inspectContracts(ctx context.Context, cfg *ResolvedConfig, stateDir, manifestPath string) (*ContractView, error) {
	deployment, publicManifest, err := loadDeploymentReferenceForConfig(ctx, cfg, stateDir, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("contract manifest: %w", err)
	}
	upgrade := CoordinatorUpgrade{}
	if publicManifest != nil {
		upgrade = publicManifest.CoordinatorUpgrade
	} else if plan, planErr := readPersistedPlan(stateDir); planErr == nil {
		upgrade = plan.CoordinatorUpgrade
	}
	endpoint := ""
	if publicManifest != nil {
		endpoint = publicManifest.EVMRPC
	}
	if endpoint == "" {
		endpoint = cfg.OperationalEVM
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, endpoint)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	chainID, err := client.ChainID(ctx)
	if err != nil || chainID.Uint64() != testnetChainID {
		return nil, fmt.Errorf("unexpected EVM chain identity: chain_id=%v err=%v", chainID, err)
	}
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return nil, err
	}
	coord, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, err
	}
	vault, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		return nil, err
	}
	reserve, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		return nil, err
	}
	baseResults, err := readContractBatchAt(ctx, client, head.Number, []contractReadSpec{
		{ID: "current_epoch", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "currentEpoch", Args: []any{}},
		{ID: "operator_count", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "operatorCount", Args: []any{}},
		{ID: "coordinator_owner", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "owner", Args: []any{}},
		{ID: "coordinator_netuid", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "netuid", Args: []any{}},
		{ID: "coordinator_self_coldkey", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "selfColdkey", Args: []any{}},
		{ID: "coordinator_guardian", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "guardian", Args: []any{}},
		{ID: "coordinator_active_guardian", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "activeGuardian", Args: []any{}},
		{ID: "coordinator_paused", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "paused", Args: []any{}},
		{ID: "coordinator_commitment_oracle", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "commitmentOracle", Args: []any{}},
		{ID: "coordinator_active_commitment_oracle", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "activeCommitmentOracle", Args: []any{}},
		{ID: "coordinator_vault", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "settlementVault", Args: []any{}},
		{ID: "coordinator_reserve", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "reserveSink", Args: []any{}},
		{ID: "conservation_holds", Address: deployment.SettlementVault, ContractABI: vault, Method: "conservationHolds", Args: []any{}},
		{ID: "total_captured", Address: deployment.SettlementVault, ContractABI: vault, Method: "totalCaptured", Args: []any{}},
		{ID: "total_paid", Address: deployment.SettlementVault, ContractABI: vault, Method: "totalPaid", Args: []any{}},
		{ID: "vault_minimum_claim_ttl", Address: deployment.SettlementVault, ContractABI: vault, Method: "minimumClaimTTLBlocks", Args: []any{}},
		{ID: "vault_minimum_transfer", Address: deployment.SettlementVault, ContractABI: vault, Method: "minimumTransferTaoRao", Args: []any{}},
		{ID: "vault_coordinator", Address: deployment.SettlementVault, ContractABI: vault, Method: "coordinator", Args: []any{}},
		{ID: "vault_netuid", Address: deployment.SettlementVault, ContractABI: vault, Method: "netuid", Args: []any{}},
		{ID: "vault_self_coldkey", Address: deployment.SettlementVault, ContractABI: vault, Method: "selfColdkey", Args: []any{}},
		{ID: "vault_escrow_hotkey", Address: deployment.SettlementVault, ContractABI: vault, Method: "escrowHotkey", Args: []any{}},
		{ID: "vault_escrow_registered", Address: deployment.SettlementVault, ContractABI: vault, Method: "escrowRegistered", Args: []any{}},
		{ID: "escrow_accounted", Address: deployment.SettlementVault, ContractABI: vault, Method: "escrowAccounted", Args: []any{}},
		{ID: "pending_funding", Address: deployment.SettlementVault, ContractABI: vault, Method: "pendingFunding", Args: []any{}},
		{ID: "outstanding_liability", Address: deployment.SettlementVault, ContractABI: vault, Method: "outstandingLiability", Args: []any{}},
		{ID: "live_escrow_stake", Address: deployment.SettlementVault, ContractABI: vault, Method: "liveEscrowStake", Args: []any{}},
		{ID: "reserve_principal", Address: deployment.ReserveSink, ContractABI: reserve, Method: "principal", Args: []any{}},
		{ID: "reserve_live_stake", Address: deployment.ReserveSink, ContractABI: reserve, Method: "liveStake", Args: []any{}},
		{ID: "reserve_recorder", Address: deployment.ReserveSink, ContractABI: reserve, Method: "recorder", Args: []any{}},
		{ID: "reserve_netuid", Address: deployment.ReserveSink, ContractABI: reserve, Method: "netuid", Args: []any{}},
		{ID: "reserve_self_coldkey", Address: deployment.ReserveSink, ContractABI: reserve, Method: "selfColdkey", Args: []any{}},
		{ID: "reserve_hotkey", Address: deployment.ReserveSink, ContractABI: reserve, Method: "reserveHotkey", Args: []any{}},
	})
	if err != nil {
		return nil, err
	}
	custodyIdentity, err := decodeContractCustodyView(baseResults, deployment, cfg)
	if err != nil {
		return nil, err
	}
	currentEpoch, err := callBigUint(requiredContractScalar(baseResults, "current_epoch"))
	if err != nil {
		return nil, err
	}
	operatorCount, err := callBigUint(requiredContractScalar(baseResults, "operator_count"))
	if err != nil {
		return nil, err
	}
	if operatorCount != uint64(cfg.Config.Topology.Operators) {
		return nil, fmt.Errorf("contract operator count %d does not match release topology %d", operatorCount, cfg.Config.Topology.Operators)
	}
	ownerValue, err := requiredContractScalar(baseResults, "coordinator_owner")
	if err != nil {
		return nil, err
	}
	coordinatorOwner, ok := ownerValue.(common.Address)
	if !ok || coordinatorOwner == (common.Address{}) {
		return nil, fmt.Errorf("coordinator owner returned invalid value %T", ownerValue)
	}
	policySpecs := make([]contractReadSpec, 0, operatorCount+3)
	policySpecs = append(policySpecs, contractReadSpec{
		ID: "policy", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "policyAt",
		Args: []any{new(big.Int).SetUint64(currentEpoch)},
	}, contractReadSpec{
		ID: "current_epoch_start", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "epochStartBlock",
		Args: []any{new(big.Int).SetUint64(currentEpoch)},
	}, contractReadSpec{
		ID: "current_epoch_end", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "epochEndBlock",
		Args: []any{new(big.Int).SetUint64(currentEpoch)},
	})
	for index := uint64(0); index < operatorCount; index++ {
		policySpecs = append(policySpecs, contractReadSpec{
			ID: fmt.Sprintf("operator_id_%d", index), Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "operatorIdAt",
			Args: []any{new(big.Int).SetUint64(index)},
		})
	}
	policyResults, err := readContractBatchAt(ctx, client, head.Number, policySpecs)
	if err != nil {
		return nil, err
	}
	policyValues := policyResults["policy"]
	if len(policyValues) != 1 {
		return nil, fmt.Errorf("policyAt returned %d values", len(policyValues))
	}
	policyHash := extractFirstBytes32(policyValues)
	policy := PolicyView{
		EffectiveEpoch:         tupleUint64(policyValues[0], "EffectiveEpoch"),
		EffectiveBlock:         tupleUint64(policyValues[0], "EffectiveBlock"),
		EpochBlocks:            tupleUint64(policyValues[0], "EpochBlocks"),
		RootCommitWindowBlocks: tupleUint64(policyValues[0], "RootCommitWindowBlocks"),
		FinalizeOffsetBlocks:   tupleUint64(policyValues[0], "FinalizeOffsetBlocks"),
		CloseGraceBlocks:       tupleUint64(policyValues[0], "CloseGraceBlocks"),
		ClaimTTLEpochs:         tupleUint64(policyValues[0], "ClaimTTLEpochs"),
		ClaimGraceEpochs:       tupleUint64(policyValues[0], "ClaimGraceEpochs"),
		EpochDepositCapRao:     tupleDecimal(policyValues[0], "EpochDepositCapRao"),
		CampaignDepositCapRao:  tupleDecimal(policyValues[0], "CampaignDepositCapRao"),
	}
	currentEpochStart, err := callBigUint(requiredContractScalar(policyResults, "current_epoch_start"))
	if err != nil {
		return nil, fmt.Errorf("current epoch start: %w", err)
	}
	currentEpochEnd, err := callBigUint(requiredContractScalar(policyResults, "current_epoch_end"))
	if err != nil {
		return nil, fmt.Errorf("current epoch end: %w", err)
	}
	if currentEpochStart > head.Number || head.Number >= currentEpochEnd || currentEpochEnd-currentEpochStart != policy.EpochBlocks {
		return nil, fmt.Errorf("current epoch %d has inconsistent finalized geometry: head=%d start=%d end=%d policy_blocks=%d", currentEpoch, head.Number, currentEpochStart, currentEpochEnd, policy.EpochBlocks)
	}
	operatorIDs := make([]uint64, 0, operatorCount)
	seenOperatorIDs := make(map[uint64]bool, operatorCount)
	for index := uint64(0); index < operatorCount; index++ {
		id := fmt.Sprintf("operator_id_%d", index)
		value, valueErr := requiredContractScalar(policyResults, id)
		idBig, ok := value.(*big.Int)
		if valueErr != nil || !ok || !idBig.IsUint64() || idBig.Uint64() == 0 || seenOperatorIDs[idBig.Uint64()] {
			return nil, stateMismatchError(valueErr, "operatorIdAt(%d) returned invalid or duplicate value %T", index, value)
		}
		seenOperatorIDs[idBig.Uint64()] = true
		operatorIDs = append(operatorIDs, idBig.Uint64())
	}
	conservation, err := callBool(requiredContractScalar(baseResults, "conservation_holds"))
	if err != nil {
		return nil, err
	}
	totalCaptured, err := callDecimal(requiredContractScalar(baseResults, "total_captured"))
	if err != nil {
		return nil, err
	}
	totalPaid, err := callDecimal(requiredContractScalar(baseResults, "total_paid"))
	if err != nil {
		return nil, err
	}
	minimumTransfer := custodyIdentity.VaultMinimumTransferRao
	escrowAccounted, err := callDecimal(requiredContractScalar(baseResults, "escrow_accounted"))
	if err != nil {
		return nil, err
	}
	pendingFunding, err := callDecimal(requiredContractScalar(baseResults, "pending_funding"))
	if err != nil {
		return nil, err
	}
	outstanding, err := callDecimal(requiredContractScalar(baseResults, "outstanding_liability"))
	if err != nil {
		return nil, err
	}
	liveEscrowStake, err := callDecimal(requiredContractScalar(baseResults, "live_escrow_stake"))
	if err != nil {
		return nil, err
	}
	principal, err := callDecimal(requiredContractScalar(baseResults, "reserve_principal"))
	if err != nil {
		return nil, err
	}
	liveStake, err := callDecimal(requiredContractScalar(baseResults, "reserve_live_stake"))
	if err != nil {
		return nil, err
	}
	addresses := []common.Address{deployment.ReserveSink, deployment.SettlementVault, deployment.CoordinatorImplementation, deployment.CoordinatorProxy, deployment.GovernanceDrillImplementation}
	if deployment.PrecompileProbe != (common.Address{}) {
		addresses = append(addresses, deployment.PrecompileProbe)
	}
	if upgrade.Implementation != (common.Address{}) {
		addresses = append(addresses, upgrade.Implementation)
	}
	hashes, matches, err := inspectRuntimeCodeAt(ctx, client, deployment, upgrade, addresses, head.Number)
	if err != nil {
		return nil, err
	}
	operators, epochs, err := inspectOperatorEpochs(ctx, client, deployment, coord, vault, head.Number, currentEpoch, operatorIDs, policy)
	if err != nil {
		return nil, err
	}
	return &ContractView{Deployment: deployment, CoordinatorUpgrade: upgrade, FinalizedHead: head, CurrentEpoch: currentEpoch, CurrentEpochStart: currentEpochStart, CurrentEpochEnd: currentEpochEnd, CoordinatorOwner: coordinatorOwner.Hex(), OperatorCount: operatorCount, PolicyHash: policyHash, ConservationHolds: conservation, MinimumTransferRao: minimumTransfer, TotalCaptured: totalCaptured, TotalPaid: totalPaid, EscrowAccounted: escrowAccounted, PendingFunding: pendingFunding, Outstanding: outstanding, LiveEscrowStake: liveEscrowStake, ReservePrincipal: principal, ReserveLiveStake: liveStake, RuntimeCodeHashes: hashes, RuntimeCodeMatches: matches, CustodyIdentity: custodyIdentity, Policy: policy, Operators: operators, Epochs: epochs}, nil
}

func decodeContractCustodyView(results map[string][]any, deployment *ContractDeployment, cfg *ResolvedConfig) (ContractCustodyView, error) {
	if deployment == nil || cfg == nil || cfg.Config == nil || cfg.Policy == nil || cfg.Netuid == 0 {
		return ContractCustodyView{}, errors.New("contract custody identity context is incomplete")
	}
	address := func(id string) (string, error) {
		value, err := requiredContractScalar(results, id)
		if err != nil {
			return "", err
		}
		decoded, ok := value.(common.Address)
		if !ok || decoded == (common.Address{}) {
			return "", fmt.Errorf("contract read %s returned invalid address %T", id, value)
		}
		return strings.ToLower(decoded.Hex()), nil
	}
	hex32 := func(id string) (string, error) {
		value, err := requiredContractScalar(results, id)
		if err != nil {
			return "", err
		}
		encoded := strings.ToLower(valueHex(value))
		decoded, decodeErr := decodeHex32(id, encoded)
		if decodeErr != nil || decoded == ([32]byte{}) {
			return "", stateMismatchError(decodeErr, "contract read %s returned an invalid zero bytes32", id)
		}
		return encoded, nil
	}
	netuid := func(id string) (uint16, error) {
		value, err := requiredContractScalar(results, id)
		if err != nil {
			return 0, err
		}
		decoded := valueUint64(value)
		if decoded == 0 || decoded > math.MaxUint16 {
			return 0, fmt.Errorf("contract read %s returned invalid netuid %T/%d", id, value, decoded)
		}
		return uint16(decoded), nil
	}
	boolean := func(id string) (bool, error) {
		return callBool(requiredContractScalar(results, id))
	}
	uint64Value := func(id string) (uint64, error) {
		return callUint64(requiredContractScalar(results, id))
	}

	var view ContractCustodyView
	var err error
	if view.CoordinatorNetuid, err = netuid("coordinator_netuid"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorSelfColdkey, err = hex32("coordinator_self_coldkey"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorGuardian, err = address("coordinator_guardian"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorActiveGuardian, err = address("coordinator_active_guardian"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorPaused, err = boolean("coordinator_paused"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorCommitmentOracle, err = address("coordinator_commitment_oracle"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorActiveCommitmentOracle, err = address("coordinator_active_commitment_oracle"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorVault, err = address("coordinator_vault"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.CoordinatorReserve, err = address("coordinator_reserve"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.VaultCoordinator, err = address("vault_coordinator"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.VaultNetuid, err = netuid("vault_netuid"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.VaultSelfColdkey, err = hex32("vault_self_coldkey"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.VaultEscrowHotkey, err = hex32("vault_escrow_hotkey"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.VaultEscrowRegistered, err = boolean("vault_escrow_registered"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.VaultMinimumClaimTTLBlocks, err = uint64Value("vault_minimum_claim_ttl"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.VaultMinimumTransferRao, err = uint64Value("vault_minimum_transfer"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.ReserveRecorder, err = address("reserve_recorder"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.ReserveNetuid, err = netuid("reserve_netuid"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.ReserveSelfColdkey, err = hex32("reserve_self_coldkey"); err != nil {
		return ContractCustodyView{}, err
	}
	if view.ReserveHotkey, err = hex32("reserve_hotkey"); err != nil {
		return ContractCustodyView{}, err
	}

	proxy := strings.ToLower(deployment.CoordinatorProxy.Hex())
	vault := strings.ToLower(deployment.SettlementVault.Hex())
	reserve := strings.ToLower(deployment.ReserveSink.Hex())
	coordinatorMirror := ss58.EvmMirrorPubkey(deployment.CoordinatorProxy)
	vaultMirror := ss58.EvmMirrorPubkey(deployment.SettlementVault)
	reserveMirror := ss58.EvmMirrorPubkey(deployment.ReserveSink)
	minimumTTL, ok := checkedMul(cfg.Policy.Settlement.EpochBlocks, cfg.Policy.Settlement.ClaimTTLEpochs)
	if !ok {
		return ContractCustodyView{}, errors.New("release minimum claim TTL overflows uint64")
	}
	if view.CoordinatorNetuid != cfg.Netuid || view.VaultNetuid != cfg.Netuid || view.ReserveNetuid != cfg.Netuid ||
		view.CoordinatorVault != vault || view.CoordinatorReserve != reserve || view.VaultCoordinator != proxy || view.ReserveRecorder != proxy ||
		view.CoordinatorSelfColdkey != fmt.Sprintf("0x%x", coordinatorMirror) || view.VaultSelfColdkey != fmt.Sprintf("0x%x", vaultMirror) || view.ReserveSelfColdkey != fmt.Sprintf("0x%x", reserveMirror) ||
		!view.VaultEscrowRegistered || view.VaultMinimumClaimTTLBlocks != minimumTTL || view.VaultMinimumTransferRao == 0 {
		return ContractCustodyView{}, errors.New("live contract custody identity differs from immutable release wiring")
	}
	return view, nil
}

func readDeploymentReference(ctx context.Context, source string) ([]byte, error) {
	return readDeploymentReferenceWithTransport(ctx, source, publicEvidenceTransportHTTPS, 0, "")
}

func readDeploymentReferenceWithTransport(ctx context.Context, source, profile string, chainID uint64, genesisHash string) ([]byte, error) {
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		if err := verifyPublicEvidenceObjectURI("deployment manifest", source, profile, chainID, genesisHash); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("manifest HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, (16*1024*1024)+1))
		if err != nil {
			return nil, err
		}
		if len(b) > 16*1024*1024 {
			return nil, errors.New("manifest exceeds 16 MiB")
		}
		return b, nil
	}
	return os.ReadFile(source)
}

func loadDeploymentReferenceForConfig(ctx context.Context, cfg *ResolvedConfig, stateDir, source string) (*ContractDeployment, *PublicDeploymentManifest, error) {
	var deployment *ContractDeployment
	var public *PublicDeploymentManifest
	var err error
	if source == "" || !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		deployment, public, err = loadDeploymentReference(ctx, stateDir, source)
	} else {
		profile, profileErr := resolvedPublicEvidenceTransportProfile(cfg)
		if profileErr != nil {
			return nil, nil, profileErr
		}
		deployment, public, err = loadDeploymentReferenceWithTransport(ctx, stateDir, source, profile, cfg.ChainID, cfg.Public.Chain.GenesisHash)
	}
	if err != nil || public == nil {
		return deployment, public, err
	}
	if err := validatePublishedRuntimeIdentity(public, cfg); err != nil {
		return nil, nil, err
	}
	return deployment, public, nil
}

func loadDeploymentReference(ctx context.Context, stateDir, source string) (*ContractDeployment, *PublicDeploymentManifest, error) {
	return loadDeploymentReferenceWithTransport(ctx, stateDir, source, publicEvidenceTransportHTTPS, 0, "")
}

func loadDeploymentReferenceWithTransport(ctx context.Context, stateDir, source, profile string, chainID uint64, genesisHash string) (*ContractDeployment, *PublicDeploymentManifest, error) {
	if source == "" {
		deployment, err := loadContractDeployment(stateDir)
		return deployment, nil, err
	}
	b, err := readDeploymentReferenceWithTransport(ctx, source, profile, chainID, genesisHash)
	if err != nil {
		return nil, nil, err
	}
	var envelope ReleaseEvidenceEnvelope
	if json.Unmarshal(b, &envelope) == nil && envelope.Schema == releaseEvidenceSchema {
		if envelope.Kind != "deployment-manifest" || envelope.RunID != "" || verifyEvidence(&envelope, nil) != nil {
			return nil, nil, errors.New("invalid signed deployment-manifest envelope")
		}
		b = envelope.Payload
	}
	var public PublicDeploymentManifest
	if json.Unmarshal(b, &public) == nil && public.Schema == "urnetwork-sim-public-deployment-v1" {
		if public.Release != "1.0" || public.DeploymentID == "" || public.ChainID != testnetChainID || !strings.EqualFold(public.GenesisHash, testnetGenesis) || public.RuntimeSpec == 0 || public.TransactionVersion == 0 || public.StateVersion == 0 || public.RuntimeCodeHash == "" || public.RuntimeMetadataHash == "" || public.Netuid == 0 || public.Contracts == nil || public.CoordinatorUpgrade.Implementation == (common.Address{}) || public.EVMRPC == "" || public.SubstrateRPC == "" || public.ConfigHash == "" || public.PolicyHash == "" || public.ReleaseLockHash == "" || len(public.SetupEvidence) == 0 || len(public.Operators) == 0 || validatePublicManifestRevision(&public) != nil {
			return nil, nil, errors.New("invalid public deployment manifest identity")
		}
		if err := validatePublishedRuntimeIdentityShape(&public); err != nil {
			return nil, nil, fmt.Errorf("invalid public deployment manifest runtime identity: %w", err)
		}
		if err := validatePublicCampaignOperatorOrigins(&public); err != nil {
			return nil, nil, fmt.Errorf("invalid public deployment manifest evidence transport: %w", err)
		}
		if envelope.Schema == releaseEvidenceSchema {
			_, signers := inspectPublicIdentityBytesForManifest(public.Identities, public.DeploymentID, public.Topology)
			matched := false
			for _, signer := range signers {
				if strings.EqualFold(signer, envelope.Signer.Hex()) {
					matched = true
				}
			}
			if !matched || envelope.DeploymentID != public.DeploymentID || envelope.ChainID != public.ChainID || envelope.Netuid != public.Netuid || !strings.EqualFold(envelope.GenesisHash, public.GenesisHash) {
				return nil, nil, errors.New("signed deployment manifest signer or chain identity mismatch")
			}
		}
		return public.Contracts, &public, nil
	}
	var deployment ContractDeployment
	if err := json.Unmarshal(b, &deployment); err != nil {
		return nil, nil, err
	}
	if deployment.Schema != "urnetwork-contract-deployment-v1" || deployment.DeploymentID == "" || deployment.CoordinatorProxy == (common.Address{}) || deployment.SettlementVault == (common.Address{}) {
		return nil, nil, errors.New("invalid contract deployment manifest identity")
	}
	return &deployment, nil, nil
}

// inspectOperatorEpochs reconstructs all retained operator and settlement
// state from one finalized block using bounded read batches. Every logical
// result retains a unique identifier and its exact ABI cardinality checks.
func inspectOperatorEpochs(ctx context.Context, client *ethclient.Client, deployment *ContractDeployment, coord, vault abi.ABI, block, currentEpoch uint64, operatorIDs []uint64, policy PolicyView) ([]OperatorView, []EpochView, error) {
	if client == nil || deployment == nil || block == 0 || len(operatorIDs) == 0 {
		return nil, nil, errors.New("operator epoch read context is incomplete")
	}
	retention := policy.ClaimTTLEpochs + policy.ClaimGraceEpochs + 2
	if retention < 3 {
		retention = 3
	}
	if retention > 32 {
		retention = 32
	}
	first := uint64(0)
	if currentEpoch >= retention {
		first = currentEpoch - retention + 1
	}
	specs := make([]contractReadSpec, 0, len(operatorIDs)*4+int(currentEpoch-first+1)*len(operatorIDs)*4)
	seenOperatorIDs := make(map[uint64]bool, len(operatorIDs))
	for _, noID := range operatorIDs {
		if noID == 0 || seenOperatorIDs[noID] {
			return nil, nil, fmt.Errorf("operator id %d is zero or duplicated", noID)
		}
		seenOperatorIDs[noID] = true
		n := new(big.Int).SetUint64(noID)
		specs = append(specs,
			contractReadSpec{ID: fmt.Sprintf("operator_%d_at", noID), Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "operatorAt", Args: []any{n, new(big.Int).SetUint64(currentEpoch)}},
			contractReadSpec{ID: fmt.Sprintf("operator_%d_pool", noID), Address: deployment.SettlementVault, ContractABI: vault, Method: "pools", Args: []any{n}},
			contractReadSpec{ID: fmt.Sprintf("operator_%d_conviction", noID), Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "cumulativeConviction", Args: []any{n}},
			contractReadSpec{ID: fmt.Sprintf("operator_%d_carry", noID), Address: deployment.SettlementVault, ContractABI: vault, Method: "carry", Args: []any{n}},
		)
	}
	for epoch := first; ; epoch++ {
		for _, noID := range operatorIDs {
			e := new(big.Int).SetUint64(epoch)
			n := new(big.Int).SetUint64(noID)
			prefix := fmt.Sprintf("epoch_%d_operator_%d", epoch, noID)
			specs = append(specs,
				contractReadSpec{ID: prefix + "_deposit", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "epochDeposits", Args: []any{e, n}},
				contractReadSpec{ID: prefix + "_conviction", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "epochConvictionAdded", Args: []any{e, n}},
				contractReadSpec{ID: prefix + "_root", Address: deployment.CoordinatorProxy, ContractABI: coord, Method: "rootCommitments", Args: []any{e, n}},
				contractReadSpec{ID: prefix + "_entitlement", Address: deployment.SettlementVault, ContractABI: vault, Method: "entitlement", Args: []any{e, n}},
			)
		}
		if epoch == currentEpoch {
			break
		}
	}
	results, err := readContractBatchAt(ctx, client, block, specs)
	if err != nil {
		return nil, nil, err
	}
	operators := make([]OperatorView, 0, len(operatorIDs))
	for _, noID := range operatorIDs {
		opValue, opErr := requiredContractScalar(results, fmt.Sprintf("operator_%d_at", noID))
		poolValues := results[fmt.Sprintf("operator_%d_pool", noID)]
		convictionValue, convictionErr := requiredContractScalar(results, fmt.Sprintf("operator_%d_conviction", noID))
		carryValue, carryErr := requiredContractScalar(results, fmt.Sprintf("operator_%d_carry", noID))
		if opErr != nil || len(poolValues) != 3 || convictionErr != nil || carryErr != nil {
			return nil, nil, stateMismatchError(errors.Join(opErr, convictionErr, carryErr), "operator %d batch result cardinality is invalid", noID)
		}
		operators = append(operators, OperatorView{
			NoID: noID, Coldkey: tupleHex(opValue, "Coldkey"), PoolHotkey: tupleHex(opValue, "PoolHotkey"), DepositHotkey: tupleHex(opValue, "DepositHotkey"),
			DepositSigner: tupleAddress(opValue, "DepositSigner"), RootSigner: tupleAddress(opValue, "RootSigner"), EffectiveEpoch: tupleUint64(opValue, "EffectiveEpoch"), Active: tupleBool(opValue, "Active"),
			PoolUID: valueUint16(poolValues[1]), PoolLive: valueBool(poolValues[2]), ConvictionRao: valueDecimal(convictionValue), CarryRao: valueDecimal(carryValue),
		})
	}
	epochs := make([]EpochView, 0, currentEpoch-first+1)
	for epoch := first; ; epoch++ {
		epochView := EpochView{Epoch: epoch}
		for _, noID := range operatorIDs {
			prefix := fmt.Sprintf("epoch_%d_operator_%d", epoch, noID)
			depositValue, depositErr := requiredContractScalar(results, prefix+"_deposit")
			convictionValue, convictionErr := requiredContractScalar(results, prefix+"_conviction")
			root := results[prefix+"_root"]
			entitlementValue, entitlementErr := requiredContractScalar(results, prefix+"_entitlement")
			if depositErr != nil || convictionErr != nil || len(root) != 4 || entitlementErr != nil {
				return nil, nil, stateMismatchError(errors.Join(depositErr, convictionErr, entitlementErr), "epoch %d operator %d batch result cardinality is invalid", epoch, noID)
			}
			epochView.Operators = append(epochView.Operators, EpochOperatorView{
				NoID: noID, DepositRao: valueDecimal(depositValue), ConvictionAddedRao: valueDecimal(convictionValue),
				PayoutRoot: valueHex(root[0]), ArtifactHash: valueHex(root[1]), Committer: valueAddress(root[2]), CommitBlock: valueUint64(root[3]),
				FundedRao: tupleDecimal(entitlementValue, "Funded"), TotalRao: tupleDecimal(entitlementValue, "Total"), ClaimedRao: tupleDecimal(entitlementValue, "Claimed"),
				ExpiryBlock: tupleUint64(entitlementValue, "ExpiryBlock"), Status: uint8(tupleUint64(entitlementValue, "Status")),
			})
		}
		epochs = append(epochs, epochView)
		if epoch == currentEpoch {
			break
		}
	}
	return operators, epochs, nil
}

func contractCallAt(ctx context.Context, client *ethclient.Client, address common.Address, contractABI abi.ABI, method string, block uint64, args ...any) ([]any, error) {
	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &address, Data: data}, new(big.Int).SetUint64(block))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return contractABI.Unpack(method, out)
}

// contractReadSpec names one ABI call in a pinned, bounded JSON-RPC batch.
// The identifier is local evidence metadata and never enters calldata.
type contractReadSpec struct {
	ID          string
	Address     common.Address
	ContractABI abi.ABI
	Method      string
	Args        []any
}

// readContractBatchAt packs, transports, and decodes every requested call at
// one exact block. rawCoordinatorBatchCallsAt enforces the public endpoint's
// fifty-element limit; identifiers and result positions are revalidated here
// so a missing, duplicate, or reordered logical read cannot be accepted.
func readContractBatchAt(ctx context.Context, client *ethclient.Client, block uint64, specs []contractReadSpec) (map[string][]any, error) {
	if client == nil || block == 0 || len(specs) == 0 {
		return nil, errors.New("contract read batch is unavailable")
	}
	requests := make([]coordinatorCallAt, len(specs))
	seen := make(map[string]bool, len(specs))
	for index, spec := range specs {
		if strings.TrimSpace(spec.ID) == "" || spec.ID != strings.TrimSpace(spec.ID) || seen[spec.ID] {
			return nil, fmt.Errorf("contract read batch element %d has an invalid or duplicate id %q", index, spec.ID)
		}
		if spec.Address == (common.Address{}) || strings.TrimSpace(spec.Method) == "" {
			return nil, fmt.Errorf("contract read batch element %s has an incomplete target or method", spec.ID)
		}
		data, err := spec.ContractABI.Pack(spec.Method, spec.Args...)
		if err != nil {
			return nil, fmt.Errorf("contract read batch element %s pack %s: %w", spec.ID, spec.Method, err)
		}
		seen[spec.ID] = true
		requests[index] = coordinatorCallAt{Address: spec.Address, Data: data, Block: block}
	}
	outputs, err := rawCoordinatorBatchCallsAt(ctx, client, requests)
	if err != nil {
		return nil, err
	}
	if len(outputs) != len(specs) {
		return nil, fmt.Errorf("contract read batch returned %d outputs, want %d", len(outputs), len(specs))
	}
	results := make(map[string][]any, len(specs))
	for index, spec := range specs {
		values, err := spec.ContractABI.Unpack(spec.Method, outputs[index])
		if err != nil {
			return nil, fmt.Errorf("contract read batch element %s decode %s: %w", spec.ID, spec.Method, err)
		}
		results[spec.ID] = values
	}
	return results, nil
}

// requiredContractScalar extracts one named single-output call without
// allowing an absent map entry or unexpected ABI result cardinality.
func requiredContractScalar(results map[string][]any, id string) (any, error) {
	values, ok := results[id]
	if !ok || len(values) != 1 {
		return nil, fmt.Errorf("contract read %s returned %d values", id, len(values))
	}
	return values[0], nil
}

// inspectRuntimeCodeAt reads every release runtime and the proxy
// implementation slot in one bounded mixed-method batch at the same finalized
// block as contract state. It returns observed code hashes even when a hash
// mismatch makes the release unhealthy.
func inspectRuntimeCodeAt(ctx context.Context, client *ethclient.Client, deployment *ContractDeployment, upgrade CoordinatorUpgrade, addresses []common.Address, block uint64) (map[string]string, bool, error) {
	if client == nil || deployment == nil || block == 0 || len(addresses) == 0 {
		return nil, false, errors.New("runtime-code batch is unavailable")
	}
	seen := make(map[common.Address]bool, len(addresses))
	codes := make([]hexutil.Bytes, len(addresses))
	batch := make([]rpc.BatchElem, 0, len(addresses)+1)
	for index, address := range addresses {
		if address == (common.Address{}) || seen[address] {
			return nil, false, fmt.Errorf("runtime-code batch address %d is zero or duplicated", index)
		}
		seen[address] = true
		batch = append(batch, rpc.BatchElem{
			Method: "eth_getCode",
			Args:   []any{address, hexutil.EncodeUint64(block)},
			Result: &codes[index],
		})
	}
	var implementationSlot hexutil.Bytes
	if upgrade.Implementation != (common.Address{}) {
		batch = append(batch, rpc.BatchElem{
			Method: "eth_getStorageAt",
			Args:   []any{deployment.CoordinatorProxy, common.HexToHash(erc1967ImplementationSlot), hexutil.EncodeUint64(block)},
			Result: &implementationSlot,
		})
	}
	for start := 0; start < len(batch); start += maximumEVMRPCBatchCalls {
		end := min(start+maximumEVMRPCBatchCalls, len(batch))
		if err := client.Client().BatchCallContext(ctx, batch[start:end]); err != nil {
			return nil, false, err
		}
		for index := start; index < end; index++ {
			if batch[index].Error != nil {
				return nil, false, fmt.Errorf("runtime-code batch element %d: %w", index, batch[index].Error)
			}
		}
	}
	hashes := make(map[string]string, len(addresses))
	matches := true
	for index, address := range addresses {
		hash := crypto.Keccak256Hash(codes[index]).Hex()
		hashes[address.Hex()] = hash
		want := deployment.RuntimeHashes[address.Hex()]
		if address == upgrade.Implementation {
			want = upgrade.RuntimeCodeHash
		}
		if want == "" || !strings.EqualFold(want, hash) {
			matches = false
		}
	}
	if upgrade.Implementation != (common.Address{}) {
		matches = matches && len(implementationSlot) == 32 && common.BytesToAddress(implementationSlot[12:]) == upgrade.Implementation
	}
	return hashes, matches, nil
}

type evmBlockReader interface {
	EVMBlockByNumber(context.Context, *big.Int) (ChainHead, error)
}

type ethEVMBlockReader struct {
	client *ethclient.Client
}

type evmRPCBlock struct {
	Number string `json:"number"`
	Hash   string `json:"hash"`
}

// Decode the explicit RPC identity without substituting go-ethereum's local
// header hash, which is a different domain on Subtensor's synthetic EVM.
func decodeEVMRPCBlock(block *evmRPCBlock, requested *big.Int) (ChainHead, error) {
	if block == nil {
		return ChainHead{}, ethereum.NotFound
	}
	parsed, ok := new(big.Int).SetString(strings.TrimPrefix(block.Number, "0x"), 16)
	if !ok || !parsed.IsUint64() {
		return ChainHead{}, fmt.Errorf("invalid EVM block number %q", block.Number)
	}
	if _, err := decodeHex32("EVM block hash", block.Hash); err != nil {
		return ChainHead{}, err
	}
	if requested != nil && requested.Sign() >= 0 && parsed.Cmp(requested) != 0 {
		return ChainHead{}, fmt.Errorf("EVM block response number %d does not match request %s", parsed.Uint64(), requested)
	}
	return ChainHead{Number: parsed.Uint64(), Hash: strings.ToLower(block.Hash)}, nil
}

// Read the explicit number and hash returned by eth_getBlockByNumber.
// Subtensor's synthetic EVM block hash is neither its Substrate block hash nor
// go-ethereum's locally recomputed Header.Hash(), so both shortcuts are wrong.
func (self ethEVMBlockReader) EVMBlockByNumber(ctx context.Context, number *big.Int) (ChainHead, error) {
	if self.client == nil || number == nil {
		return ChainHead{}, errors.New("EVM block reader is unavailable")
	}
	argument := ""
	if number.Sign() < 0 {
		if !number.IsInt64() || rpc.BlockNumber(number.Int64()) != rpc.FinalizedBlockNumber {
			return ChainHead{}, fmt.Errorf("unsupported EVM block selector %s", number)
		}
		argument = rpc.FinalizedBlockNumber.String()
	} else {
		argument = "0x" + number.Text(16)
	}
	var block *evmRPCBlock
	if err := self.client.Client().CallContext(ctx, &block, "eth_getBlockByNumber", argument, false); err != nil {
		return ChainHead{}, err
	}
	return decodeEVMRPCBlock(block, number)
}

// Read the EVM block selected by the finalized tag in its explicit RPC hash
// domain.
func finalizedEVMHeadFromReader(ctx context.Context, reader evmBlockReader) (ChainHead, error) {
	if reader == nil {
		return ChainHead{}, errors.New("EVM finalized-block reader is unavailable")
	}
	return reader.EVMBlockByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
}

func finalizedEVMHead(ctx context.Context, client *ethclient.Client) (ChainHead, error) {
	if head, ok := boundFinalizedEVMHead(ctx); ok {
		return head, nil
	}
	if client == nil {
		return ChainHead{}, errors.New("EVM finalized-header client is unavailable")
	}
	return finalizedEVMHeadFromReader(ctx, ethEVMBlockReader{client: client})
}

// Resolve one numbered block in the EVM RPC hash domain and reject an RPC
// response whose embedded height does not match the requested checkpoint.
func canonicalEVMBlockHash(ctx context.Context, reader evmBlockReader, number uint64) (string, error) {
	if reader == nil || number == 0 {
		return "", errors.New("EVM canonical checkpoint is incomplete")
	}
	head, err := reader.EVMBlockByNumber(ctx, new(big.Int).SetUint64(number))
	if err != nil {
		return "", err
	}
	if head.Number != number || head.Hash == "" {
		return "", fmt.Errorf("EVM canonical header at block %d has a mismatched or missing number", number)
	}
	return head.Hash, nil
}

func callBigUint(v any, err error) (uint64, error) {
	if err != nil {
		return 0, err
	}
	n, ok := v.(*big.Int)
	if !ok || !n.IsUint64() {
		return 0, fmt.Errorf("expected uint256, got %T", v)
	}
	return n.Uint64(), nil
}

func callUint64(v any, err error) (uint64, error) {
	if err != nil {
		return 0, err
	}
	switch n := v.(type) {
	case uint64:
		return n, nil
	case *big.Int:
		if n != nil && n.IsUint64() {
			return n.Uint64(), nil
		}
	}
	return 0, fmt.Errorf("expected uint64, got %T", v)
}

func callBool(v any, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("expected bool, got %T", v)
	}
	return b, nil
}

func callDecimal(v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	n, ok := v.(*big.Int)
	if !ok {
		return "", fmt.Errorf("expected uint256, got %T", v)
	}
	return n.String(), nil
}

func extractFirstBytes32(values []any) string {
	if len(values) == 0 {
		return ""
	}
	value := reflect.ValueOf(values[0])
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.IsValid() && value.Kind() == reflect.Struct {
		typeOf := value.Type()
		for i := 0; i < value.NumField(); i++ {
			if strings.EqualFold(typeOf.Field(i).Name, "policyHash") {
				field := value.Field(i)
				if field.Kind() == reflect.Array && field.Len() == 32 && field.Type().Elem().Kind() == reflect.Uint8 {
					bytes := make([]byte, 32)
					for j := range bytes {
						bytes[j] = byte(field.Index(j).Uint())
					}
					return "0x" + fmt.Sprintf("%x", bytes)
				}
			}
		}
	}
	return ""
}

func tupleField(tuple any, name string) any {
	value := reflect.ValueOf(tuple)
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return nil
	}
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	return field.Interface()
}

func valueUint64(value any) uint64 {
	switch v := value.(type) {
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	case *big.Int:
		if v != nil && v.IsUint64() {
			return v.Uint64()
		}
	}
	return 0
}

func valueUint16(value any) uint16 {
	v := valueUint64(value)
	if v > math.MaxUint16 {
		return 0
	}
	return uint16(v)
}

func valueBool(value any) bool {
	v, _ := value.(bool)
	return v
}

func valueDecimal(value any) string {
	if n, ok := value.(*big.Int); ok && n != nil {
		return n.String()
	}
	return fmt.Sprint(valueUint64(value))
}

func valueAddress(value any) string {
	if address, ok := value.(common.Address); ok {
		return address.Hex()
	}
	return ""
}

func valueHex(value any) string {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || (rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice) || rv.Type().Elem().Kind() != reflect.Uint8 {
		return ""
	}
	b := make([]byte, rv.Len())
	for i := range b {
		b[i] = byte(rv.Index(i).Uint())
	}
	return "0x" + fmt.Sprintf("%x", b)
}

func tupleUint64(tuple any, name string) uint64  { return valueUint64(tupleField(tuple, name)) }
func tupleBool(tuple any, name string) bool      { return valueBool(tupleField(tuple, name)) }
func tupleDecimal(tuple any, name string) string { return valueDecimal(tupleField(tuple, name)) }
func tupleAddress(tuple any, name string) string { return valueAddress(tupleField(tuple, name)) }
func tupleHex(tuple any, name string) string     { return valueHex(tupleField(tuple, name)) }

func readJournal(path string) ([]JournalEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	entries := make([]JournalEntry, 0, len(lines))
	previous := ""
	for i, line := range lines {
		var entry JournalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("journal line %d: %w", i+1, err)
		}
		want := entry.EntryHash
		entry.EntryHash = ""
		got, err := canonicalHashHex(entry)
		if err != nil {
			return nil, err
		}
		if got != want || entry.PreviousHash != previous || entry.Sequence != uint64(i+1) {
			return nil, fmt.Errorf("journal line %d failed hash-chain validation", i+1)
		}
		entry.EntryHash = want
		previous = want
		entries = append(entries, entry)
	}
	return entries, nil
}

func summarizeJournal(entries []JournalEntry) JournalSummary {
	s := JournalSummary{Entries: len(entries), LatestByStage: map[string]int{}, Actions: map[string]bool{}}
	latest := map[string]JournalStage{}
	for _, entry := range entries {
		s.LastHash = entry.EntryHash
		latest[entry.ActionID] = entry.Stage
	}
	for action, stage := range latest {
		s.LatestByStage[string(stage)]++
		if stage == StageVerified {
			s.Actions[action] = true
		}
	}
	return s
}

func renderHuman(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v\n", v)
	}
	return string(b) + "\n"
}
