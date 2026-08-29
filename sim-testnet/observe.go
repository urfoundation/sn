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
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type ChainHead struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

type ContractView struct {
	Deployment         *ContractDeployment `json:"deployment,omitempty"`
	FinalizedHead      ChainHead           `json:"finalized_head"`
	CurrentEpoch       uint64              `json:"current_epoch"`
	OperatorCount      uint64              `json:"operator_count"`
	PolicyHash         string              `json:"policy_hash,omitempty"`
	ConservationHolds  bool                `json:"conservation_holds"`
	TotalCaptured      string              `json:"total_captured_rao,omitempty"`
	TotalPaid          string              `json:"total_paid_rao,omitempty"`
	Outstanding        string              `json:"outstanding_liability_rao,omitempty"`
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
		observation.ReserveValidatorRegistered, observation.ReserveValidatorUID, observation.ReserveDelegateTake, observation.EscrowHotkeyRegistered, observation.EscrowHotkeyUID, observation.NativeCustodyError = inspectNativeCustodyRolesBytes(cfg, public.Identities, public.SubstrateRPC)
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
	_, expected := inspectPublicIdentityBytes(p.cfg, public.Identities)
	type candidate struct {
		bundle  *ScenarioEvidenceBundle
		payload string
		signers map[int]bool
		time    time.Time
	}
	candidates := map[string]*candidate{}
	for _, operator := range public.Operators {
		history, err := url.Parse(operator.HistoryURL)
		if err != nil {
			return nil, err
		}
		query := history.Query()
		query.Set("deployment_id", public.DeploymentID)
		query.Set("netuid", fmt.Sprint(public.Netuid))
		query.Set("kind", "scenario-bundle")
		history.RawQuery = query.Encode()
		body, _, err := p.get(ctx, history.String(), 16<<20)
		if err != nil {
			return nil, fmt.Errorf("operator %d history: %w", operator.NoID, err)
		}
		keys := evidenceHistoryKeys(body)
		if len(keys) == 0 {
			return nil, fmt.Errorf("operator %d has no scenario-bundle history", operator.NoID)
		}
		for _, key := range keys {
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
			if json.Unmarshal(envelope.Payload, &bundle) != nil || bundle.Schema != "urnetwork-sim-scenario-evidence-v1" || bundle.Result == nil || bundle.Observation == nil || bundle.Result.DeploymentID != public.DeploymentID || bundle.Result.Netuid != public.Netuid || bundle.Result.ConfigHash != public.ConfigHash || !strings.EqualFold(bundle.Result.PolicyHash, public.PolicyHash) {
				continue
			}
			payloadHash := bytesSHA256(envelope.Payload)
			completed, _ := time.Parse(time.RFC3339Nano, bundle.Result.CompletedAt)
			item := candidates[bundle.Result.RunID]
			if item == nil {
				copy := bundle
				item = &candidate{bundle: &copy, payload: payloadHash, signers: map[int]bool{}, time: completed}
				candidates[bundle.Result.RunID] = item
			}
			if item.payload == payloadHash {
				item.signers[operator.NoID] = true
			}
		}
	}
	var latest *candidate
	for _, item := range candidates {
		if len(item.signers) != len(public.Operators) {
			continue
		}
		if latest == nil || item.time.After(latest.time) {
			latest = item
		}
	}
	if latest == nil {
		return nil, errors.New("no scenario bundle has byte-identical payloads signed by every operator")
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
	callScalar := func(address common.Address, contractABI abi.ABI, method string, args ...any) (any, error) {
		values, callErr := contractCallAt(ctx, client, address, contractABI, method, head.Number, args...)
		if callErr != nil {
			return nil, callErr
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("%s returned %d values", method, len(values))
		}
		return values[0], nil
	}
	currentEpoch, err := callBigUint(callScalar(deployment.CoordinatorProxy, coord, "currentEpoch"))
	if err != nil {
		return nil, err
	}
	operatorCount, err := callBigUint(callScalar(deployment.CoordinatorProxy, coord, "operatorCount"))
	if err != nil {
		return nil, err
	}
	policyValues, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coord, "policyAt", head.Number, new(big.Int).SetUint64(currentEpoch))
	if err != nil {
		return nil, err
	}
	policyHash := extractFirstBytes32(policyValues)
	policy := PolicyView{
		EffectiveEpoch:         tupleUint64(policyValues[0], "EffectiveEpoch"),
		EpochBlocks:            tupleUint64(policyValues[0], "EpochBlocks"),
		RootCommitWindowBlocks: tupleUint64(policyValues[0], "RootCommitWindowBlocks"),
		FinalizeOffsetBlocks:   tupleUint64(policyValues[0], "FinalizeOffsetBlocks"),
		CloseGraceBlocks:       tupleUint64(policyValues[0], "CloseGraceBlocks"),
		ClaimTTLEpochs:         tupleUint64(policyValues[0], "ClaimTTLEpochs"),
		ClaimGraceEpochs:       tupleUint64(policyValues[0], "ClaimGraceEpochs"),
		EpochDepositCapRao:     tupleDecimal(policyValues[0], "EpochDepositCapRao"),
		CampaignDepositCapRao:  tupleDecimal(policyValues[0], "CampaignDepositCapRao"),
	}
	conservation, err := callBool(callScalar(deployment.SettlementVault, vault, "conservationHolds"))
	if err != nil {
		return nil, err
	}
	totalCaptured, err := callDecimal(callScalar(deployment.SettlementVault, vault, "totalCaptured"))
	if err != nil {
		return nil, err
	}
	totalPaid, err := callDecimal(callScalar(deployment.SettlementVault, vault, "totalPaid"))
	if err != nil {
		return nil, err
	}
	outstanding, err := callDecimal(callScalar(deployment.SettlementVault, vault, "outstandingLiability"))
	if err != nil {
		return nil, err
	}
	principal, err := callDecimal(callScalar(deployment.ReserveSink, reserve, "principal"))
	if err != nil {
		return nil, err
	}
	liveStake, err := callDecimal(callScalar(deployment.ReserveSink, reserve, "liveStake"))
	if err != nil {
		return nil, err
	}
	hashes := map[string]string{}
	matches := true
	addresses := []common.Address{deployment.ReserveSink, deployment.SettlementVault, deployment.CoordinatorImplementation, deployment.CoordinatorProxy, deployment.GovernanceDrillImplementation}
	if deployment.PrecompileProbe != (common.Address{}) {
		addresses = append(addresses, deployment.PrecompileProbe)
	}
	for _, address := range addresses {
		code, codeErr := client.CodeAt(ctx, address, new(big.Int).SetUint64(head.Number))
		if codeErr != nil {
			return nil, codeErr
		}
		hash := crypto.Keccak256Hash(code).Hex()
		hashes[address.Hex()] = hash
		if want := deployment.RuntimeHashes[address.Hex()]; want == "" || !strings.EqualFold(want, hash) {
			matches = false
		}
	}
	operators, epochs, err := inspectOperatorEpochs(ctx, client, deployment, coord, vault, head.Number, currentEpoch, operatorCount, policy)
	if err != nil {
		return nil, err
	}
	return &ContractView{Deployment: deployment, FinalizedHead: head, CurrentEpoch: currentEpoch, OperatorCount: operatorCount, PolicyHash: policyHash, ConservationHolds: conservation, TotalCaptured: totalCaptured, TotalPaid: totalPaid, Outstanding: outstanding, ReservePrincipal: principal, ReserveLiveStake: liveStake, RuntimeCodeHashes: hashes, RuntimeCodeMatches: matches, Policy: policy, Operators: operators, Epochs: epochs}, nil
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
		if public.Release != "1.0" || public.DeploymentID == "" || public.ChainID != testnetChainID || !strings.EqualFold(public.GenesisHash, testnetGenesis) || public.RuntimeSpec == 0 || public.Netuid == 0 || public.Contracts == nil || public.EVMRPC == "" || public.SubstrateRPC == "" || public.ConfigHash == "" || public.PolicyHash == "" || public.ReleaseLockHash == "" || len(public.SetupEvidence) == 0 || len(public.Operators) == 0 {
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

func inspectOperatorEpochs(ctx context.Context, client *ethclient.Client, deployment *ContractDeployment, coord, vault abi.ABI, block, currentEpoch, operatorCount uint64, policy PolicyView) ([]OperatorView, []EpochView, error) {
	operatorIDs := make([]uint64, 0, operatorCount)
	operators := make([]OperatorView, 0, operatorCount)
	for i := uint64(0); i < operatorCount; i++ {
		idValues, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coord, "operatorIdAt", block, new(big.Int).SetUint64(i))
		if err != nil || len(idValues) != 1 {
			return nil, nil, fmt.Errorf("operatorIdAt(%d): %w", i, err)
		}
		idBig, ok := idValues[0].(*big.Int)
		if !ok || !idBig.IsUint64() {
			return nil, nil, fmt.Errorf("operatorIdAt(%d) returned %T", i, idValues[0])
		}
		noID := idBig.Uint64()
		operatorIDs = append(operatorIDs, noID)
		opValues, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coord, "operatorAt", block, idBig, new(big.Int).SetUint64(currentEpoch))
		if err != nil || len(opValues) != 1 {
			return nil, nil, fmt.Errorf("operatorAt(%d): %w", noID, err)
		}
		poolValues, err := contractCallAt(ctx, client, deployment.SettlementVault, vault, "pools", block, idBig)
		if err != nil || len(poolValues) != 3 {
			return nil, nil, fmt.Errorf("pools(%d): %w", noID, err)
		}
		conviction, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coord, "cumulativeConviction", block, idBig)
		if err != nil || len(conviction) != 1 {
			return nil, nil, fmt.Errorf("cumulativeConviction(%d): %w", noID, err)
		}
		carry, err := contractCallAt(ctx, client, deployment.SettlementVault, vault, "carry", block, idBig)
		if err != nil || len(carry) != 1 {
			return nil, nil, fmt.Errorf("carry(%d): %w", noID, err)
		}
		operators = append(operators, OperatorView{NoID: noID, Coldkey: tupleHex(opValues[0], "Coldkey"), PoolHotkey: tupleHex(opValues[0], "PoolHotkey"), DepositHotkey: tupleHex(opValues[0], "DepositHotkey"), DepositSigner: tupleAddress(opValues[0], "DepositSigner"), RootSigner: tupleAddress(opValues[0], "RootSigner"), EffectiveEpoch: tupleUint64(opValues[0], "EffectiveEpoch"), Active: tupleBool(opValues[0], "Active"), PoolUID: valueUint16(poolValues[1]), PoolLive: valueBool(poolValues[2]), ConvictionRao: valueDecimal(conviction[0]), CarryRao: valueDecimal(carry[0])})
	}
	retention := policy.ClaimTTLEpochs + policy.ClaimGraceEpochs + 2
	if retention < 3 {
		retention = 3
	}
	if retention > 32 {
		retention = 32
	}
	first := uint64(0)
	if currentEpoch+1 > retention {
		first = currentEpoch + 1 - retention
	}
	epochs := make([]EpochView, 0, currentEpoch-first+1)
	for epoch := first; epoch <= currentEpoch; epoch++ {
		epochView := EpochView{Epoch: epoch}
		for _, noID := range operatorIDs {
			e := new(big.Int).SetUint64(epoch)
			n := new(big.Int).SetUint64(noID)
			deposit, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coord, "epochDeposits", block, e, n)
			if err != nil {
				return nil, nil, err
			}
			added, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coord, "epochConvictionAdded", block, e, n)
			if err != nil {
				return nil, nil, err
			}
			root, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coord, "rootCommitments", block, e, n)
			if err != nil || len(root) != 4 {
				return nil, nil, fmt.Errorf("rootCommitments(%d,%d): %w", epoch, noID, err)
			}
			entitlement, err := contractCallAt(ctx, client, deployment.SettlementVault, vault, "entitlement", block, e, n)
			if err != nil || len(entitlement) != 1 {
				return nil, nil, fmt.Errorf("entitlement(%d,%d): %w", epoch, noID, err)
			}
			epochView.Operators = append(epochView.Operators, EpochOperatorView{NoID: noID, DepositRao: valueDecimal(deposit[0]), ConvictionAddedRao: valueDecimal(added[0]), PayoutRoot: valueHex(root[0]), ArtifactHash: valueHex(root[1]), Committer: valueAddress(root[2]), CommitBlock: valueUint64(root[3]), FundedRao: tupleDecimal(entitlement[0], "Funded"), TotalRao: tupleDecimal(entitlement[0], "Total"), ClaimedRao: tupleDecimal(entitlement[0], "Claimed"), ExpiryBlock: tupleUint64(entitlement[0], "ExpiryBlock"), Status: uint8(tupleUint64(entitlement[0], "Status"))})
		}
		epochs = append(epochs, epochView)
		if epoch == math.MaxUint64 {
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

func finalizedEVMHead(ctx context.Context, client *ethclient.Client) (ChainHead, error) {
	var hash string
	if err := client.Client().CallContext(ctx, &hash, "chain_getFinalizedHead"); err != nil {
		return ChainHead{}, err
	}
	var header struct {
		Number string `json:"number"`
		Hash   string `json:"hash"`
	}
	if err := client.Client().CallContext(ctx, &header, "chain_getHeader", hash); err != nil {
		return ChainHead{}, err
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(header.Number, "0x"), 16)
	if !ok || !n.IsUint64() {
		return ChainHead{}, fmt.Errorf("invalid finalized block number %q", header.Number)
	}
	return ChainHead{Number: n.Uint64(), Hash: hash}, nil
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
