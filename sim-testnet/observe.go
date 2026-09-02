package main

import (
	"context"
	"encoding/json"
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
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type ChainHead struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

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
	Policy             PolicyView          `json:"policy"`
	Operators          []OperatorView      `json:"operators"`
	Epochs             []EpochView         `json:"epochs"`
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
		_, publicManifest, err = loadDeploymentReference(ctx, stateDir, manifest)
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
	var observation *ScenarioObservation
	var err error
	if manifest == "" {
		observation, err = probe.Snapshot(ctx)
	} else {
		view, viewErr := inspectContracts(ctx, cfg, stateDir, manifest)
		if viewErr != nil {
			return nil, viewErr
		}
		_, public, loadErr := loadDeploymentReference(ctx, stateDir, manifest)
		if loadErr != nil {
			return nil, loadErr
		}
		if public == nil {
			return nil, errors.New("independent analysis requires a public deployment manifest, not a bare contract manifest")
		}
		bundle, bundleErr := probe.fetchLatestScenarioBundle(ctx, public)
		if bundleErr != nil {
			return nil, fmt.Errorf("public scenario evidence: %w", bundleErr)
		}
		if bundle.Observation == nil {
			return nil, errors.New("public scenario evidence has no observation")
		}
		observation = bundle.Observation
		observation.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		observation.Status = &DeploymentStatus{Schema: "urnetwork-sim-status-v1", DeploymentID: public.DeploymentID, ConfigHash: public.ConfigHash, PolicyHash: public.PolicyHash, Netuid: public.Netuid, Contracts: view, Healthy: view.ConservationHolds && view.RuntimeCodeMatches}
		var expectedSigners map[int]string
		observation.PublicIdentityCount, expectedSigners = inspectPublicIdentityBytes(cfg, public.Identities)
		observation.PublicIdentitiesValid = observation.PublicIdentityCount > 0
		minerClients, minerClientsErr := inspectMinerClientIDsBytes(cfg, public.Identities)
		if minerClientsErr != nil {
			observation.PublicIdentitiesValid = false
		}
		expectedCoordinator := common.Address{}
		if public.Contracts != nil {
			expectedCoordinator = public.Contracts.CoordinatorProxy
		}
		observation.FleetCommitmentValid, observation.FleetBindingCount, observation.FleetBindingsValid, observation.CandidateFleetUIDs = inspectFleetEvidenceBytes(cfg, public.SetupEvidence, expectedCoordinator)
		generation := uint64(0)
		if public.Contracts != nil {
			generation = public.Contracts.RegistrationRoleGeneration
		}
		observation.ReserveValidatorRegistered, observation.ReserveValidatorUID, observation.ReserveDelegateTake, observation.EscrowHotkeyRegistered, observation.EscrowHotkeyUID, observation.NativeCustodyError = inspectNativeCustodyRolesBytes(cfg, public.Identities, public.SubstrateRPC, generation)
		observation.NativeRewards, observation.NativeRewardsError = inspectNativeRewards(cfg, public.SubstrateRPC)
		depositSigner := publicEVMRole(public.Identities, "operator-1-deposit")
		observation.VoluntaryConviction, observation.VoluntaryConvictionValid, observation.VoluntaryConvictionError = inspectVoluntaryConvictionBytes(ctx, cfg, public.SetupEvidence["voluntary_conviction"], view, public.EVMRPC, depositSigner)
		observation.Operators = nil
		for _, operator := range public.Operators {
			observation.Operators = append(observation.Operators, probe.inspectOperatorAt(ctx, view, operator.NoID, expectedSigners[operator.NoID], operator.APIURL, minerClients))
		}
		observation.ObservationHash = ""
		observation.ObservationHash, err = canonicalHashHex(observation)
	}
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

func (p *liveScenarioProbe) fetchLatestScenarioBundle(ctx context.Context, public *PublicDeploymentManifest) (*ScenarioEvidenceBundle, error) {
	if public == nil {
		return nil, errors.New("public deployment manifest is missing")
	}
	_, expected := inspectPublicIdentityBytes(p.cfg, public.Identities)
	var identities struct {
		EVM map[string]string `json:"evm"`
	}
	if json.Unmarshal(public.Identities, &identities) != nil || identities.EVM["testnet-owner"] == "" || len(expected) != len(public.Operators) {
		return nil, errors.New("public scenario evidence signer directory is invalid")
	}
	ownerSigner := identities.EVM["testnet-owner"]
	type candidate struct {
		bundle  *ScenarioEvidenceBundle
		payload string
		signers map[int]bool
		time    time.Time
	}
	type completionCandidate struct {
		envelope  *ReleaseEvidenceEnvelope
		payload   scenarioCompletePayload
		operators map[int]bool
	}
	candidates := map[string]*candidate{}
	completions := map[string]*completionCandidate{}
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
			body, _, err := p.get(ctx, history.String(), 16<<20)
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
			encoded, _, fetchErr := p.get(ctx, evidenceURL, 64<<20)
			if fetchErr != nil {
				continue
			}
			var envelope ReleaseEvidenceEnvelope
			if json.Unmarshal(encoded, &envelope) != nil || verifyEvidence(&envelope, nil) != nil || envelope.Kind != "scenario-bundle" || envelope.DeploymentID != public.DeploymentID || envelope.ChainID != public.ChainID || envelope.Netuid != public.Netuid || !strings.EqualFold(envelope.GenesisHash, public.GenesisHash) || !strings.EqualFold(envelope.Signer.Hex(), expected[operator.NoID]) {
				continue
			}
			var bundle ScenarioEvidenceBundle
			if json.Unmarshal(envelope.Payload, &bundle) != nil || bundle.Schema != "urnetwork-sim-scenario-evidence-v1" || bundle.Result == nil || bundle.Observation == nil || bundle.Result.Result != "pass" || bundle.Result.DeploymentID != public.DeploymentID || bundle.Result.Netuid != public.Netuid || bundle.Result.ConfigHash != public.ConfigHash || !strings.EqualFold(bundle.Result.PolicyHash, public.PolicyHash) {
				continue
			}
			resultHash, hashErr := canonicalScenarioResultHash(bundle.Result)
			if hashErr != nil || !strings.EqualFold(resultHash, bundle.Result.EvidenceHash) {
				continue
			}
			payloadHash := bytesSHA256(envelope.Payload)
			completed, _ := time.Parse(time.RFC3339Nano, bundle.Result.CompletedAt)
			candidateKey := bundle.Result.RunID + "\x00" + payloadHash
			item := candidates[candidateKey]
			if item == nil {
				copy := bundle
				item = &candidate{bundle: &copy, payload: payloadHash, signers: map[int]bool{}, time: completed}
				candidates[candidateKey] = item
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
			encoded, _, fetchErr := p.get(ctx, evidenceURL, 64<<20)
			if fetchErr != nil {
				continue
			}
			var operatorEnvelope ReleaseEvidenceEnvelope
			if decodeStrictJSONBytes(encoded, &operatorEnvelope) != nil || verifyEvidence(&operatorEnvelope, nil) != nil || operatorEnvelope.Kind != "scenario-complete-commit" || operatorEnvelope.RunID == "" || operatorEnvelope.DeploymentID != public.DeploymentID || operatorEnvelope.ChainID != public.ChainID || operatorEnvelope.Netuid != public.Netuid || !strings.EqualFold(operatorEnvelope.GenesisHash, public.GenesisHash) || !strings.EqualFold(operatorEnvelope.Signer.Hex(), expected[operator.NoID]) {
				continue
			}
			var envelope ReleaseEvidenceEnvelope
			if decodeStrictJSONBytes(operatorEnvelope.Payload, &envelope) != nil || verifyEvidence(&envelope, nil) != nil || envelope.Kind != "scenario-complete" || envelope.RunID != operatorEnvelope.RunID || envelope.DeploymentID != public.DeploymentID || envelope.ChainID != public.ChainID || envelope.Netuid != public.Netuid || !strings.EqualFold(envelope.GenesisHash, public.GenesisHash) || !strings.EqualFold(envelope.Signer.Hex(), ownerSigner) {
				continue
			}
			var payload scenarioCompletePayload
			if decodeStrictJSONBytes(envelope.Payload, &payload) != nil || !validCanonicalHashHex(payload.ResultHash) || !validSHA256ContentHash(payload.BundlePayloadHash) || len(payload.Files) == 0 {
				continue
			}
			item := completions[envelope.ContentHash]
			if item == nil {
				copyEnvelope := envelope
				item = &completionCandidate{envelope: &copyEnvelope, payload: payload, operators: map[int]bool{}}
				completions[envelope.ContentHash] = item
			}
			item.operators[operator.NoID] = true
		}
	}
	var latest *candidate
	for _, item := range candidates {
		if len(item.signers) != len(public.Operators) {
			continue
		}
		committed := false
		for _, completion := range completions {
			if len(completion.operators) == len(public.Operators) && completion.envelope.RunID == item.bundle.Result.RunID && strings.EqualFold(completion.payload.ResultHash, item.bundle.Result.EvidenceHash) && strings.EqualFold(completion.payload.BundlePayloadHash, item.payload) {
				committed = true
				break
			}
		}
		if !committed {
			continue
		}
		if latest == nil || item.time.After(latest.time) {
			latest = item
		}
	}
	if latest == nil {
		return nil, errors.New("no scenario bundle has byte-identical operator signatures and a replicated owner completion commit")
	}
	return latest.bundle, nil
}

// adoptPublicManifest lets a clean second checkout inspect a deployment
// without copying the launch host's filled testnet vault. The checked-in
// release config and policy still have to hash to the public manifest; only
// deployment facts discovered by setup are adopted.
func adoptPublicManifest(ctx context.Context, cfg *ResolvedConfig, source string) error {
	_, public, err := loadDeploymentReference(ctx, "", source)
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
	if public.ConfigHash != cfg.ConfigHash || !strings.EqualFold(public.PolicyHash, cfg.PolicyHash) || public.ReleaseLockHash != lockHash || topologyHash != publicTopologyHash || public.RuntimeSpec != cfg.Public.Chain.ExpectedRuntimeSpec {
		return errors.New("public manifest does not match this checkout's config, policy, release lock, topology, and runtime pin")
	}
	if len(public.Operators) != cfg.Config.Topology.Operators {
		return errors.New("public manifest operator topology is incomplete")
	}
	origins := make([]string, len(public.Operators))
	seen := map[int]bool{}
	for _, operator := range public.Operators {
		if operator.NoID < 1 || operator.NoID > len(origins) || seen[operator.NoID] || operator.APIURL == "" {
			return errors.New("public manifest operator directory is invalid")
		}
		seen[operator.NoID] = true
		origins[operator.NoID-1] = strings.TrimSuffix(operator.APIURL, "/")
	}
	cfg.Netuid = public.Netuid
	cfg.ChainID = public.ChainID
	cfg.Public.Chain.EVMPublicReadEndpoint = public.EVMRPC
	cfg.Public.Chain.SubstratePublicReadEndpoint = public.SubstrateRPC
	cfg.OperatorAPIOrigins = origins
	return nil
}

func inspectContracts(ctx context.Context, cfg *ResolvedConfig, stateDir, manifestPath string) (*ContractView, error) {
	deployment, publicManifest, err := loadDeploymentReference(ctx, stateDir, manifestPath)
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
		{ID: "conservation_holds", Address: deployment.SettlementVault, ContractABI: vault, Method: "conservationHolds", Args: []any{}},
		{ID: "total_captured", Address: deployment.SettlementVault, ContractABI: vault, Method: "totalCaptured", Args: []any{}},
		{ID: "total_paid", Address: deployment.SettlementVault, ContractABI: vault, Method: "totalPaid", Args: []any{}},
		{ID: "minimum_transfer", Address: deployment.SettlementVault, ContractABI: vault, Method: "minimumTransferTaoRao", Args: []any{}},
		{ID: "escrow_accounted", Address: deployment.SettlementVault, ContractABI: vault, Method: "escrowAccounted", Args: []any{}},
		{ID: "pending_funding", Address: deployment.SettlementVault, ContractABI: vault, Method: "pendingFunding", Args: []any{}},
		{ID: "outstanding_liability", Address: deployment.SettlementVault, ContractABI: vault, Method: "outstandingLiability", Args: []any{}},
		{ID: "live_escrow_stake", Address: deployment.SettlementVault, ContractABI: vault, Method: "liveEscrowStake", Args: []any{}},
		{ID: "reserve_principal", Address: deployment.ReserveSink, ContractABI: reserve, Method: "principal", Args: []any{}},
		{ID: "reserve_live_stake", Address: deployment.ReserveSink, ContractABI: reserve, Method: "liveStake", Args: []any{}},
	})
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
	minimumTransfer, err := callUint64(requiredContractScalar(baseResults, "minimum_transfer"))
	if err != nil {
		return nil, fmt.Errorf("minimumTransferTaoRao: %w", err)
	}
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
	return &ContractView{Deployment: deployment, CoordinatorUpgrade: upgrade, FinalizedHead: head, CurrentEpoch: currentEpoch, CurrentEpochStart: currentEpochStart, CurrentEpochEnd: currentEpochEnd, OperatorCount: operatorCount, PolicyHash: policyHash, ConservationHolds: conservation, MinimumTransferRao: minimumTransfer, TotalCaptured: totalCaptured, TotalPaid: totalPaid, EscrowAccounted: escrowAccounted, PendingFunding: pendingFunding, Outstanding: outstanding, LiveEscrowStake: liveEscrowStake, ReservePrincipal: principal, ReserveLiveStake: liveStake, RuntimeCodeHashes: hashes, RuntimeCodeMatches: matches, Policy: policy, Operators: operators, Epochs: epochs}, nil
}

func readDeploymentReference(ctx context.Context, source string) ([]byte, error) {
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("manifest HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
		if err != nil {
			return nil, err
		}
		if len(b) > 16<<20 {
			return nil, errors.New("manifest exceeds 16 MiB")
		}
		return b, nil
	}
	return os.ReadFile(source)
}

func loadDeploymentReference(ctx context.Context, stateDir, source string) (*ContractDeployment, *PublicDeploymentManifest, error) {
	if source == "" {
		deployment, err := loadContractDeployment(stateDir)
		return deployment, nil, err
	}
	b, err := readDeploymentReference(ctx, source)
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
		if public.Release != "1.0" || public.DeploymentID == "" || public.ChainID != testnetChainID || !strings.EqualFold(public.GenesisHash, testnetGenesis) || public.RuntimeSpec == 0 || public.Netuid == 0 || public.Contracts == nil || public.CoordinatorUpgrade.Implementation == (common.Address{}) || public.EVMRPC == "" || public.SubstrateRPC == "" || public.ConfigHash == "" || public.PolicyHash == "" || public.ReleaseLockHash == "" || len(public.SetupEvidence) == 0 || len(public.Operators) == 0 || validatePublicManifestRevision(&public) != nil {
			return nil, nil, errors.New("invalid public deployment manifest identity")
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
