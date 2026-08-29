package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	gsrpcTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// AssertionRecord is the stable machine-readable assertion format shared by
// assertions.json and junit.xml. ObservationHash points at the exact snapshot
// on which the assertion was evaluated.
type AssertionRecord struct {
	ID              string  `json:"id"`
	Passed          bool    `json:"passed"`
	Message         string  `json:"message"`
	StartedAt       string  `json:"started_at"`
	CompletedAt     string  `json:"completed_at"`
	DurationSeconds float64 `json:"duration_seconds"`
	ObservationHash string  `json:"observation_hash"`
}

type OperatorObservation struct {
	NoID                       int      `json:"no_id"`
	APIURL                     string   `json:"api_url"`
	Healthy                    bool     `json:"healthy"`
	StatusCode                 int      `json:"status_code"`
	StatsRows                  int      `json:"stats_rows"`
	Assignments                uint64   `json:"assignments"`
	Confirmations              uint64   `json:"confirmations"`
	ReliabilityPPM             uint32   `json:"reliability_ppm"`
	ProofRows                  int      `json:"proof_rows"`
	VerifyKeyIDs               []byte   `json:"verify_key_ids,omitempty"`
	ProofKeyIDs                []byte   `json:"proof_key_ids,omitempty"`
	StatsHash                  string   `json:"stats_hash,omitempty"`
	ProofsHash                 string   `json:"proofs_hash,omitempty"`
	StatsPolicyHash            string   `json:"stats_policy_hash,omitempty"`
	ProofsPolicyHash           string   `json:"proofs_policy_hash,omitempty"`
	ArtifactHistoryObjects     int      `json:"artifact_history_objects"`
	ValidArtifacts             int      `json:"valid_artifacts"`
	MatchingArtifacts          int      `json:"matching_onchain_artifacts"`
	ExpectedFinalizedArtifacts int      `json:"expected_finalized_artifacts"`
	ArtifactHashes             []string `json:"artifact_hashes,omitempty"`
	Error                      string   `json:"error,omitempty"`
}

type IntentWeightObservation struct {
	UID         uint16 `json:"uid"`
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
	Value       uint16 `json:"value"`
}

type ValidatorObservation struct {
	ValidatorID      int                       `json:"validator_id"`
	CurrentStatus    string                    `json:"current_status,omitempty"`
	CurrentEpoch     uint64                    `json:"current_epoch,omitempty"`
	VectorHash       string                    `json:"vector_hash,omitempty"`
	ValuesHash       string                    `json:"values_hash,omitempty"`
	FinalizedIntents int                       `json:"finalized_intents"`
	AppliedIntents   int                       `json:"applied_intents"`
	SelfUID          uint16                    `json:"self_uid"`
	MaskedUIDs       []uint16                  `json:"masked_uids,omitempty"`
	IntentHashes     []string                  `json:"intent_hashes,omitempty"`
	AppliedWeights   []IntentWeightObservation `json:"applied_weights,omitempty"`
	Error            string                    `json:"error,omitempty"`
}

type ClaimObservation struct {
	MinerID    int    `json:"miner_id"`
	NoID       int    `json:"no_id"`
	Discovered int    `json:"discovered"`
	Finalized  int    `json:"finalized"`
	NoClaim    int    `json:"no_claim"`
	Pending    int    `json:"pending"`
	Uncertain  int    `json:"uncertain"`
	Failed     int    `json:"failed"`
	LastTxHash string `json:"last_tx_hash,omitempty"`
	Error      string `json:"error,omitempty"`
}

type ScenarioObservation struct {
	Schema                     string                         `json:"schema"`
	ObservedAt                 string                         `json:"observed_at"`
	Status                     *DeploymentStatus              `json:"status"`
	Operators                  []OperatorObservation          `json:"operators"`
	Validators                 []ValidatorObservation         `json:"validators"`
	Claims                     []ClaimObservation             `json:"claims"`
	PublicIdentityCount        int                            `json:"public_identity_count"`
	PublicIdentitiesValid      bool                           `json:"public_identities_valid"`
	FleetCommitmentValid       bool                           `json:"fleet_commitment_valid"`
	FleetBindingCount          int                            `json:"fleet_binding_count"`
	FleetBindingsValid         bool                           `json:"fleet_bindings_valid"`
	HeadFleetUIDs              []uint16                       `json:"head_fleet_uids"`
	ReserveValidatorRegistered bool                           `json:"reserve_validator_registered"`
	ReserveValidatorUID        uint16                         `json:"reserve_validator_uid"`
	ReserveDelegateTake        *uint16                        `json:"reserve_delegate_take,omitempty"`
	EscrowHotkeyRegistered     bool                           `json:"escrow_hotkey_registered"`
	EscrowHotkeyUID            uint16                         `json:"escrow_hotkey_uid"`
	NativeCustodyError         string                         `json:"native_custody_error,omitempty"`
	VoluntaryConviction        *VoluntaryConvictionEvidence   `json:"voluntary_conviction,omitempty"`
	VoluntaryConvictionValid   bool                           `json:"voluntary_conviction_valid"`
	VoluntaryConvictionError   string                         `json:"voluntary_conviction_error,omitempty"`
	GovernanceDrill            *GovernanceDrillEvidence       `json:"governance_drill,omitempty"`
	GovernanceDrillError       string                         `json:"governance_drill_error,omitempty"`
	PrecompileConformance      *PrecompileConformanceEvidence `json:"precompile_conformance,omitempty"`
	PrecompileConformanceValid bool                           `json:"precompile_conformance_valid"`
	PrecompileConformanceError string                         `json:"precompile_conformance_error,omitempty"`
	ExpectedFaultIDs           []string                       `json:"expected_fault_ids,omitempty"`
	ExpectedFaultTargets       []string                       `json:"expected_fault_targets,omitempty"`
	ObservationHash            string                         `json:"observation_hash"`
}

type ScenarioResult struct {
	Schema               string                     `json:"schema"`
	Release              string                     `json:"release"`
	RunID                string                     `json:"run_id"`
	DeploymentID         string                     `json:"deployment_id"`
	Name                 string                     `json:"name"`
	ScenarioDefinition   string                     `json:"scenario_definition_hash"`
	ScenarioMatrix       string                     `json:"scenario_matrix_hash,omitempty"`
	AdversarialMatrix    string                     `json:"adversarial_matrix_hash,omitempty"`
	ConfigHash           string                     `json:"config_hash"`
	PolicyHash           string                     `json:"policy_hash"`
	ChainID              uint64                     `json:"chain_id"`
	GenesisHash          string                     `json:"genesis_hash"`
	Netuid               uint16                     `json:"netuid"`
	StartedAt            string                     `json:"started_at"`
	CompletedAt          string                     `json:"completed_at"`
	StartHead            ChainHead                  `json:"start_finalized_head"`
	EndHead              ChainHead                  `json:"end_finalized_head"`
	StartEpoch           uint64                     `json:"start_epoch"`
	EndEpoch             uint64                     `json:"end_epoch"`
	AssertionCount       int                        `json:"assertion_count"`
	FailedAssertionCount int                        `json:"failed_assertion_count"`
	Assertions           []AssertionRecord          `json:"assertions"`
	Faults               []ScenarioFaultRecord      `json:"faults,omitempty"`
	Adversaries          *AdversaryCampaignEvidence `json:"adversaries,omitempty"`
	Anomalies            *ScenarioAnomalyLedger     `json:"anomalies"`
	ValueReconciliation  map[string]string          `json:"value_reconciliation"`
	PublishedEvidence    []PublishedEvidence        `json:"published_evidence,omitempty"`
	EvidenceHash         string                     `json:"evidence_hash"`
	Result               string                     `json:"result"`
}

type ScenarioEvidenceBundle struct {
	Schema      string               `json:"schema"`
	Result      *ScenarioResult      `json:"result"`
	Observation *ScenarioObservation `json:"observation"`
	Analysis    *AnalysisReport      `json:"analysis"`
}

type scenarioProbe interface {
	Snapshot(context.Context) (*ScenarioObservation, error)
}

type liveScenarioProbe struct {
	cfg      *ResolvedConfig
	stateDir string
	client   *http.Client
}

type scenarioCheck struct {
	ID    string
	Check func(*scenarioEvaluation) (bool, string)
}

type scenarioDefinition struct {
	Name                  string
	GoalEpochs            uint64
	Checks                []scenarioCheck
	Faults                []scenarioFaultSpec
	MatrixHash            string
	AdversarialMatrixHash string
}

type scenarioEvaluation struct {
	Cfg        *ResolvedConfig
	Start      *ScenarioObservation
	Current    *ScenarioObservation
	GoalEpoch  uint64
	Definition scenarioDefinition
}

type scenarioRunOptions struct {
	Now          func() time.Time
	PollInterval time.Duration
	Timeout      time.Duration
	Roles        *RoleSecrets
	Publish      bool
	FaultDriver  scenarioFaultDriver
	Adversaries  adversaryCampaign
	Prepare      func(context.Context) error
}

func (p *liveScenarioProbe) Snapshot(ctx context.Context) (*ScenarioObservation, error) {
	status, err := Status(ctx, p.cfg, p.stateDir)
	if err != nil {
		return nil, err
	}
	observation := &ScenarioObservation{
		Schema:     "urnetwork-sim-scenario-observation-v1",
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status:     status,
	}
	identities, expectedSigners := inspectPublicIdentities(p.cfg, p.stateDir)
	observation.PublicIdentityCount = identities
	observation.PublicIdentitiesValid = identities > 0
	observation.FleetCommitmentValid, observation.FleetBindingCount, observation.FleetBindingsValid, observation.HeadFleetUIDs = inspectFleetEvidence(p.cfg, p.stateDir)
	observation.ReserveValidatorRegistered, observation.ReserveValidatorUID, observation.ReserveDelegateTake, observation.EscrowHotkeyRegistered, observation.EscrowHotkeyUID, observation.NativeCustodyError = inspectNativeCustodyRoles(p.cfg, p.stateDir)
	observation.VoluntaryConviction, observation.VoluntaryConvictionValid, observation.VoluntaryConvictionError = inspectVoluntaryConviction(ctx, p.cfg, p.stateDir, status.Contracts)
	if evidence, readErr := func() (*GovernanceDrillEvidence, error) {
		var value GovernanceDrillEvidence
		if err := readJSONFile(filepath.Join(p.stateDir, "public", "governance-drill.json"), &value); err != nil {
			return nil, err
		}
		return &value, nil
	}(); readErr == nil {
		observation.GovernanceDrill = evidence
	} else if !errors.Is(readErr, os.ErrNotExist) {
		observation.GovernanceDrillError = readErr.Error()
	}
	if evidence, readErr := loadPrecompileEvidence(p.stateDir); readErr == nil {
		observation.PrecompileConformance = evidence
		if status.Contracts == nil || status.Contracts.Deployment == nil {
			observation.PrecompileConformanceError = "contract deployment is unavailable"
		} else if validateErr := validatePrecompileEvidenceIdentity(p.cfg, status.Contracts.Deployment, evidence); validateErr != nil {
			observation.PrecompileConformanceError = validateErr.Error()
		} else {
			observation.PrecompileConformanceValid = precompileEvidenceComplete(evidence)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		observation.PrecompileConformanceError = readErr.Error()
	}
	for noID := 1; noID <= p.cfg.Config.Topology.Operators; noID++ {
		observation.Operators = append(observation.Operators, p.inspectOperator(ctx, status.Contracts, noID, expectedSigners[noID]))
	}
	for validatorID := 1; validatorID <= p.cfg.Config.Topology.Validators; validatorID++ {
		observation.Validators = append(observation.Validators, inspectValidatorIntent(p.stateDir, validatorID))
	}
	for minerID := 1; minerID <= p.cfg.Config.Topology.Miners; minerID++ {
		observation.Claims = append(observation.Claims, inspectClaimQueue(p.cfg, p.stateDir, minerID))
	}
	observation.ObservationHash = ""
	observation.ObservationHash, err = canonicalHashHex(observation)
	return observation, err
}

func inspectVoluntaryConviction(ctx context.Context, cfg *ResolvedConfig, stateDir string, contracts *ContractView) (*VoluntaryConvictionEvidence, bool, string) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "voluntary-conviction.json"))
	if err != nil {
		return nil, false, err.Error()
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil || len(roles.OperatorDepositSigners) == 0 {
		return nil, false, "planned operator-1 deposit signer is unavailable"
	}
	return inspectVoluntaryConvictionBytes(ctx, cfg, b, contracts, cfg.OperationalEVM, roles.OperatorDepositSigners[0])
}

func inspectVoluntaryConvictionBytes(ctx context.Context, cfg *ResolvedConfig, b []byte, contracts *ContractView, endpoint, expectedFunder string) (*VoluntaryConvictionEvidence, bool, string) {
	var evidence VoluntaryConvictionEvidence
	if json.Unmarshal(b, &evidence) != nil {
		return nil, false, "voluntary conviction evidence is not valid JSON"
	}
	txBytes, txErr := hex.DecodeString(strings.TrimPrefix(evidence.TransactionHash, "0x"))
	if evidence.Schema != "urnetwork-voluntary-conviction-evidence-v1" || evidence.DeploymentID != cfg.Config.Deployment.DeploymentID || evidence.NoID != 1 || evidence.AmountRao != fmt.Sprint(cfg.Config.Scenarios.VoluntaryConvictionRao) || evidence.BeforeConvictionRao != "0" || evidence.AfterConvictionRao != evidence.AmountRao || !strings.EqualFold(evidence.PolicyHash, cfg.PolicyHash) || txErr != nil || len(txBytes) != 32 {
		return &evidence, false, "voluntary conviction evidence identity is invalid"
	}
	if contracts == nil || contracts.Deployment == nil {
		return &evidence, false, "contract deployment is unavailable"
	}
	if expectedFunder == "" || !strings.EqualFold(evidence.Funder, expectedFunder) {
		return &evidence, false, "voluntary conviction funder is not the planned operator-1 deposit signer"
	}
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return &evidence, false, err.Error()
	}
	defer client.Close()
	receipt, err := client.TransactionReceipt(ctx, common.HexToHash(evidence.TransactionHash))
	if err != nil || receipt.Status != 1 || receipt.BlockNumber == nil || receipt.BlockNumber.Uint64() != evidence.FinalizedBlock || !strings.EqualFold(receipt.BlockHash.Hex(), evidence.FinalizedHash) {
		return &evidence, false, fmt.Sprintf("voluntary conviction receipt mismatch: %v", err)
	}
	head, err := finalizedEVMHead(ctx, client)
	if err != nil || head.Number < evidence.FinalizedBlock {
		return &evidence, false, fmt.Sprintf("voluntary conviction is not finalized: %v", err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return &evidence, false, err.Error()
	}
	event := parsed.Events["ConvictionAdded"]
	validEvent := false
	for _, log := range receipt.Logs {
		if log.Address != contracts.Deployment.CoordinatorProxy || len(log.Topics) != 4 || log.Topics[0] != event.ID || log.Topics[1].Big().Cmp(big.NewInt(1)) != 0 || !log.Topics[2].Big().IsUint64() || log.Topics[2].Big().Uint64() != evidence.Epoch || !strings.EqualFold(common.BytesToAddress(log.Topics[3].Bytes()[12:]).Hex(), evidence.Funder) {
			continue
		}
		values, unpackErr := event.Inputs.NonIndexed().Unpack(log.Data)
		if unpackErr != nil || len(values) != 3 {
			continue
		}
		amount, amountOK := values[0].(*big.Int)
		policyHash, policyOK := values[1].([32]byte)
		nonce, nonceOK := values[2].(*big.Int)
		if amountOK && policyOK && nonceOK && amount.String() == evidence.AmountRao && "0x"+hex.EncodeToString(policyHash[:]) == strings.ToLower(evidence.PolicyHash) && nonce.String() == evidence.Nonce {
			validEvent = true
			break
		}
	}
	if !validEvent {
		return &evidence, false, "finalized receipt lacks the recorded ConvictionAdded event"
	}
	values, err := contractCallAt(ctx, client, contracts.Deployment.CoordinatorProxy, parsed, "cumulativeConviction", head.Number, big.NewInt(1))
	if err != nil || len(values) != 1 {
		return &evidence, false, fmt.Sprintf("read current cumulative conviction: %v", err)
	}
	conviction, ok := values[0].(*big.Int)
	if !ok || conviction.Cmp(new(big.Int).SetUint64(cfg.Config.Scenarios.VoluntaryConvictionRao)) < 0 {
		return &evidence, false, "current cumulative conviction is below the voluntary amount"
	}
	return &evidence, true, ""
}

func inspectNativeCustodyRoles(cfg *ResolvedConfig, stateDir string) (bool, uint16, *uint16, bool, uint16, string) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "identities.json"))
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	return inspectNativeCustodyRolesBytes(cfg, b, cfg.OperationalSubstrate)
}

func inspectNativeCustodyRolesBytes(cfg *ResolvedConfig, b []byte, endpoint string) (bool, uint16, *uint16, bool, uint16, string) {
	var identities struct {
		Substrate map[string]map[string]string `json:"substrate"`
	}
	if json.Unmarshal(b, &identities) != nil {
		return false, 0, nil, false, 0, "invalid public identities"
	}
	decode := func(label string) ([32]byte, error) {
		var out [32]byte
		raw, err := hex.DecodeString(strings.TrimPrefix(identities.Substrate[label]["public_key"], "0x"))
		if err != nil || len(raw) != 32 {
			return out, fmt.Errorf("%s public key is invalid", label)
		}
		copy(out[:], raw)
		return out, nil
	}
	reserve, err := decode("reserve-hotkey")
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	escrow, err := decode("escrow-hotkey")
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	defer chain.API.Client.Close()
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	readUID := func(hotkey [32]byte) (bool, uint16, error) {
		key, err := gsrpcTypes.CreateStorageKey(chain.Meta, crv4.PalletName, "Uids", netuidArg(cfg.Netuid), hotkey[:])
		if err != nil {
			return false, 0, err
		}
		var uid gsrpcTypes.U16
		ok, err := chain.API.RPC.State.GetStorage(key, &uid, finalized)
		return ok, uint16(uid), err
	}
	reserveOK, reserveUID, err := readUID(reserve)
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	escrowOK, escrowUID, err := readUID(escrow)
	if err != nil {
		return reserveOK, reserveUID, nil, false, 0, err.Error()
	}
	takeKey, err := gsrpcTypes.CreateStorageKey(chain.Meta, crv4.PalletName, "Delegates", reserve[:])
	if err != nil {
		return reserveOK, reserveUID, nil, escrowOK, escrowUID, err.Error()
	}
	var take gsrpcTypes.U16
	if _, err := chain.API.RPC.State.GetStorage(takeKey, &take, finalized); err != nil {
		return reserveOK, reserveUID, nil, escrowOK, escrowUID, err.Error()
	}
	takeValue := uint16(take)
	return reserveOK, reserveUID, &takeValue, escrowOK, escrowUID, ""
}

func inspectPublicIdentities(cfg *ResolvedConfig, stateDir string) (int, map[int]string) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "identities.json"))
	if err != nil {
		return 0, nil
	}
	return inspectPublicIdentityBytes(cfg, b)
}

func inspectPublicIdentityBytes(cfg *ResolvedConfig, b []byte) (int, map[int]string) {
	return inspectPublicIdentityBytesForManifest(b, cfg.Config.Deployment.DeploymentID, cfg.Config.Topology)
}

func inspectPublicIdentityBytesForManifest(b []byte, deploymentID string, topology TopologyConfig) (int, map[int]string) {
	if !json.Valid(b) {
		return 0, nil
	}
	lower := strings.ToLower(string(b))
	if strings.Contains(lower, "private_key") || strings.Contains(lower, "seed_hex") || strings.Contains(lower, "wallet_secret") {
		return 0, nil
	}
	var value struct {
		Schema       string                       `json:"schema"`
		DeploymentID string                       `json:"deployment_id"`
		EVM          map[string]string            `json:"evm"`
		Substrate    map[string]map[string]string `json:"substrate"`
		Clients      map[string]map[string]string `json:"clients"`
	}
	if json.Unmarshal(b, &value) != nil || value.Schema != "urnetwork-sim-public-identities-v1" || value.DeploymentID != deploymentID {
		return 0, nil
	}
	minimumEVM := 5 + 3*topology.Operators
	minimumSubstrate := 2 + 2*topology.HeadFleets + 3*topology.Operators + 2*topology.Validators + topology.Miners
	minimumClients := topology.Miners + topology.Validators*topology.Operators
	if len(value.EVM) < minimumEVM || len(value.Substrate) < minimumSubstrate || len(value.Clients) < minimumClients {
		return 0, nil
	}
	for _, client := range value.Clients {
		if len(strings.TrimPrefix(client["client_id"], "0x")) != 32 || len(strings.TrimPrefix(client["client_key"], "0x")) != 64 {
			return 0, nil
		}
	}
	signers := map[int]string{}
	for noID := 1; noID <= topology.Operators; noID++ {
		signers[noID] = value.EVM[fmt.Sprintf("operator-%d-artifact", noID)]
		if signers[noID] == "" {
			return 0, nil
		}
	}
	return len(value.EVM) + len(value.Substrate) + len(value.Clients), signers
}

func inspectFleetEvidence(cfg *ResolvedConfig, stateDir string) (bool, int, bool, []uint16) {
	setup := map[string]json.RawMessage{}
	paths := map[string]string{}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		paths[fmt.Sprintf("fleet_%d_manifest", fleet)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.json", fleet))
		paths[fmt.Sprintf("fleet_%d_commitment", fleet)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.commitment.json", fleet))
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			paths[fmt.Sprintf("fleet_%d_binding_%d", fleet, member)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
		}
	}
	for name, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return false, 0, false, nil
		}
		setup[name] = b
	}
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		return false, 0, false, nil
	}
	return inspectFleetEvidenceBytes(cfg, setup, deployment.CoordinatorProxy)
}

func evidenceFixedHex(value string, size int) ([]byte, bool) {
	b, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	return b, err == nil && len(b) == size && value == "0x"+hex.EncodeToString(b)
}

func inspectFleetEvidenceBytes(cfg *ResolvedConfig, setup map[string]json.RawMessage, expectedCoordinator common.Address) (bool, int, bool, []uint16) {
	allCommitmentsValid, allBindingsValid := true, true
	totalBindings := 0
	uids := make([]uint16, 0, cfg.Config.Topology.HeadFleets)
	seenUIDs := map[uint16]bool{}
	seenClients := map[[16]byte]bool{}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		commitmentOK, count, bindingsOK, uid, clients := inspectOneFleetEvidenceBytes(cfg, setup, expectedCoordinator, fleet)
		allCommitmentsValid = allCommitmentsValid && commitmentOK
		allBindingsValid = allBindingsValid && bindingsOK
		totalBindings += count
		if bindingsOK {
			if seenUIDs[uid] {
				allBindingsValid = false
			}
			seenUIDs[uid] = true
			uids = append(uids, uid)
		}
		for _, client := range clients {
			if seenClients[client] {
				allBindingsValid = false
			}
			seenClients[client] = true
		}
	}
	return allCommitmentsValid, totalBindings, allBindingsValid && len(uids) == cfg.Config.Topology.HeadFleets, uids
}

func inspectOneFleetEvidenceBytes(cfg *ResolvedConfig, setup map[string]json.RawMessage, expectedCoordinator common.Address, fleetIndex int) (bool, int, bool, uint16, [][16]byte) {
	prefix := fmt.Sprintf("fleet_%d", fleetIndex)
	manifest, err := protocol.ParseFleetManifest(setup[prefix+"_manifest"])
	if err != nil || manifest.ChainID != cfg.ChainID || manifest.Netuid != cfg.Netuid || common.BytesToAddress(manifest.Coordinator[:]) != expectedCoordinator || len(manifest.Members) != cfg.Config.Topology.ClientsPerHeadFleet {
		return false, 0, false, 0, nil
	}
	commitmentHash, err := manifest.CommitmentHash()
	if err != nil {
		return false, 0, false, 0, nil
	}
	commitmentBytes := setup[prefix+"_commitment"]
	var commitment FleetCommitmentEvidence
	if json.Unmarshal(commitmentBytes, &commitment) != nil {
		return false, 0, false, 0, nil
	}
	commitmentValue, commitmentOK := evidenceFixedHex(commitment.CommitmentHash, 32)
	hotkeyValue, hotkeyOK := evidenceFixedHex(commitment.Hotkey, 32)
	_, finalizedHashOK := evidenceFixedHex(commitment.FinalizedBlockHash, 32)
	if commitment.Schema != "urnetwork-fleet-commitment-evidence-v1" || commitment.CommitmentBlock == 0 || commitment.FinalizedBlock < commitment.CommitmentBlock || !commitmentOK || !hotkeyOK || !finalizedHashOK || !bytes.Equal(commitmentValue, commitmentHash[:]) || !bytes.Equal(hotkeyValue, manifest.Hotkey[:]) {
		return false, 0, false, 0, nil
	}
	valid := true
	count := 0
	var fleetUID uint16
	fleetUIDSet := false
	members := map[[16]byte]protocol.FleetMember{}
	for _, member := range manifest.Members {
		members[member.ClientID] = member
	}
	seen := map[[16]byte]bool{}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		b := setup[fmt.Sprintf("%s_binding_%d", prefix, member)]
		if len(b) == 0 {
			valid = false
			continue
		}
		var binding FleetBindingEvidence
		if json.Unmarshal(b, &binding) != nil || binding.Schema != "urnetwork-fleet-binding-evidence-v1" || binding.BlockNumber == 0 || binding.Generation != manifest.Generation || binding.ValidFromEpoch == 0 || binding.ValidToEpoch < binding.ValidFromEpoch || binding.ValidToEpoch-binding.ValidFromEpoch+1 > cfg.Policy.Binding.MaximumValidityEpochs {
			valid = false
			continue
		}
		clientID, idOK := evidenceFixedHex(binding.ClientID, 16)
		clientKey, keyOK := evidenceFixedHex(binding.ClientKey, 32)
		fleetID, fleetOK := evidenceFixedHex(binding.FleetID, 32)
		hotkey, hotOK := evidenceFixedHex(binding.Hotkey, 32)
		bindingCommitment, hashOK := evidenceFixedHex(binding.CommitmentHash, 32)
		digestValue, digestOK := evidenceFixedHex(binding.BindingDigest, 32)
		clientSignature, clientSigOK := evidenceFixedHex(binding.ClientSignature, 64)
		hotkeySignature, hotSigOK := evidenceFixedHex(binding.HotkeySignature, 64)
		_, txOK := evidenceFixedHex(binding.TransactionHash, 32)
		_, blockOK := evidenceFixedHex(binding.BlockHash, 32)
		if !idOK || !keyOK || !fleetOK || !hotOK || !hashOK || !digestOK || !clientSigOK || !hotSigOK || !txOK || !blockOK {
			valid = false
			continue
		}
		var canonical protocol.FleetBinding
		canonical.ChainID, canonical.Netuid, canonical.Coordinator = manifest.ChainID, manifest.Netuid, manifest.Coordinator
		copy(canonical.ClientID[:], clientID)
		copy(canonical.ClientKey[:], clientKey)
		copy(canonical.FleetID[:], fleetID)
		copy(canonical.Hotkey[:], hotkey)
		copy(canonical.CommitmentHash[:], bindingCommitment)
		canonical.Generation, canonical.ValidFromEpoch, canonical.ValidToEpoch = binding.Generation, binding.ValidFromEpoch, binding.ValidToEpoch
		digest, digestErr := canonical.Digest()
		manifestMember, memberOK := members[canonical.ClientID]
		if digestErr != nil || !memberOK || seen[canonical.ClientID] || manifestMember.ClientKey != canonical.ClientKey || canonical.FleetID != manifest.FleetID || canonical.Hotkey != manifest.Hotkey || canonical.CommitmentHash != commitmentHash || !bytes.Equal(digestValue, digest[:]) || !canonical.VerifyClient(clientSignature) || !canonical.VerifyHotkey(hotkeySignature) {
			valid = false
			continue
		}
		if fleetUIDSet && fleetUID != binding.UID {
			valid = false
			continue
		}
		fleetUID, fleetUIDSet = binding.UID, true
		seen[canonical.ClientID] = true
		count++
	}
	clients := make([][16]byte, 0, len(seen))
	for client := range seen {
		clients = append(clients, client)
	}
	return true, count, valid && count == cfg.Config.Topology.ClientsPerHeadFleet && fleetUIDSet, fleetUID, clients
}

func (p *liveScenarioProbe) get(ctx context.Context, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(b)) > limit {
		return nil, resp.StatusCode, errors.New("response exceeds evidence size limit")
	}
	if resp.StatusCode/100 != 2 {
		return b, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return b, resp.StatusCode, nil
}

func bytesSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func (p *liveScenarioProbe) inspectOperator(ctx context.Context, contracts *ContractView, noID int, expectedSigner string) OperatorObservation {
	base := fmt.Sprintf("http://127.0.0.1:%d", 18080+noID)
	return p.inspectOperatorAt(ctx, contracts, noID, expectedSigner, base)
}

func (p *liveScenarioProbe) inspectOperatorAt(ctx context.Context, contracts *ContractView, noID int, expectedSigner, base string) OperatorObservation {
	base = strings.TrimSuffix(base, "/")
	o := OperatorObservation{NoID: noID, APIURL: base}
	if contracts != nil {
		for _, epoch := range contracts.Epochs {
			for _, operator := range epoch.Operators {
				if operator.NoID == uint64(noID) && operator.Status == 2 {
					o.ExpectedFinalizedArtifacts++
				}
			}
		}
	}
	_, statusCode, statusErr := p.get(ctx, base+"/status", 1<<20)
	o.StatusCode = statusCode
	o.Healthy = statusErr == nil
	var problems []string
	if statusErr != nil {
		problems = append(problems, "status: "+statusErr.Error())
	}
	keysBytes, _, err := p.get(ctx, base+"/verify/keys", 1<<20)
	if err != nil {
		problems = append(problems, "verify keys: "+err.Error())
	} else {
		var keys struct {
			Keys []struct {
				ServerKeyID byte   `json:"server_key_id"`
				PublicKey   []byte `json:"public_key"`
			} `json:"keys"`
		}
		seen := map[byte]bool{}
		if json.Unmarshal(keysBytes, &keys) != nil || len(keys.Keys) == 0 {
			problems = append(problems, "verify keys: invalid response")
		} else {
			for _, key := range keys.Keys {
				if len(key.PublicKey) != 32 || seen[key.ServerKeyID] {
					problems = append(problems, "verify keys: invalid/duplicate key")
					continue
				}
				seen[key.ServerKeyID] = true
				o.VerifyKeyIDs = append(o.VerifyKeyIDs, key.ServerKeyID)
			}
			sort.Slice(o.VerifyKeyIDs, func(i, j int) bool { return o.VerifyKeyIDs[i] < o.VerifyKeyIDs[j] })
		}
	}
	statsBytes, _, err := p.get(ctx, base+"/verify/stats?limit=100000", 32<<20)
	if err != nil {
		problems = append(problems, "stats: "+err.Error())
	} else {
		var stats struct {
			Schema     string `json:"schema"`
			PolicyHash string `json:"policy_hash"`
			Rows       []struct {
				Assignments   uint64 `json:"assignments"`
				Confirmations uint64 `json:"confirmations"`
			} `json:"rows"`
		}
		if json.Unmarshal(statsBytes, &stats) != nil || stats.Schema != "urnetwork-verify-stats-index-v1" {
			problems = append(problems, "stats: invalid schema")
		} else {
			o.StatsRows, o.StatsHash, o.StatsPolicyHash = len(stats.Rows), bytesSHA256(statsBytes), stats.PolicyHash
			for _, row := range stats.Rows {
				o.Assignments += row.Assignments
				o.Confirmations += row.Confirmations
			}
			o.ReliabilityPPM = protocol.ReliabilityPPM(o.Confirmations, o.Assignments, p.cfg.Policy.Verify.ReliabilityAMin)
		}
	}
	proofBytes, _, err := p.get(ctx, base+"/verify/proofs?limit=10000", 32<<20)
	if err != nil {
		problems = append(problems, "proofs: "+err.Error())
	} else {
		var proofs struct {
			Schema     string `json:"schema"`
			PolicyHash string `json:"policy_hash"`
			Rows       []struct {
				ServerKeyID byte `json:"server_key_id"`
			} `json:"rows"`
		}
		if json.Unmarshal(proofBytes, &proofs) != nil || proofs.Schema != "urnetwork-verify-proof-index-v1" {
			problems = append(problems, "proofs: invalid schema")
		} else {
			o.ProofRows, o.ProofsHash, o.ProofsPolicyHash = len(proofs.Rows), bytesSHA256(proofBytes), proofs.PolicyHash
			seen := map[byte]bool{}
			for _, proof := range proofs.Rows {
				if !seen[proof.ServerKeyID] {
					seen[proof.ServerKeyID] = true
					o.ProofKeyIDs = append(o.ProofKeyIDs, proof.ServerKeyID)
				}
			}
			sort.Slice(o.ProofKeyIDs, func(i, j int) bool { return o.ProofKeyIDs[i] < o.ProofKeyIDs[j] })
		}
	}
	historyURL := fmt.Sprintf("%s/sn/artifacts?deployment_id=%s&netuid=%d", base, p.cfg.Config.Deployment.DeploymentID, p.cfg.Netuid)
	historyBytes, _, err := p.get(ctx, historyURL, 16<<20)
	if err != nil {
		problems = append(problems, "artifacts: "+err.Error())
	} else {
		keys := artifactHistoryKeys(historyBytes)
		o.ArtifactHistoryObjects = len(keys)
		seen := map[string]bool{}
		for _, key := range keys {
			hash := strings.TrimSuffix(filepath.Base(key), filepath.Ext(key))
			if len(hash) != 64 || seen[hash] {
				continue
			}
			seen[hash] = true
			artifactBytes, _, artifactErr := p.get(ctx, base+"/sn/artifact?hash=sha256:"+hash, 32<<20)
			if artifactErr != nil {
				problems = append(problems, "artifact "+hash+": "+artifactErr.Error())
				continue
			}
			var artifact payoutArtifact
			if json.Unmarshal(artifactBytes, &artifact) != nil || verifyPayoutArtifact(&artifact) != nil {
				problems = append(problems, "artifact "+hash+": integrity failure")
				continue
			}
			if artifact.DeploymentID != p.cfg.Config.Deployment.DeploymentID || artifact.ChainID != p.cfg.ChainID || artifact.Netuid != p.cfg.Netuid || artifact.NoID != uint64(noID) || !strings.EqualFold(artifact.GenesisHash, p.cfg.Public.Chain.GenesisHash) || !strings.EqualFold(artifact.PolicyHash, p.cfg.PolicyHash) || (expectedSigner != "" && !strings.EqualFold(artifact.Signer.Hex(), expectedSigner)) {
				problems = append(problems, "artifact "+hash+": deployment identity mismatch")
				continue
			}
			o.ValidArtifacts++
			o.ArtifactHashes = append(o.ArtifactHashes, artifact.ContentHash)
			if payoutArtifactMatchesChain(&artifact, contracts) {
				o.MatchingArtifacts++
			}
		}
	}
	sort.Strings(o.ArtifactHashes)
	o.Error = strings.Join(problems, "; ")
	return o
}

func artifactHistoryKeys(b []byte) []string {
	var result struct {
		Schema  string            `json:"schema"`
		Objects []json.RawMessage `json:"objects"`
	}
	if json.Unmarshal(b, &result) != nil || result.Schema != "urnetwork-payout-artifact-history-v1" {
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

func payoutArtifactMatchesChain(a *payoutArtifact, contracts *ContractView) bool {
	if contracts == nil || contracts.Deployment == nil || a.End.Number > contracts.FinalizedHead.Number || !strings.EqualFold(a.Coordinator.Hex(), contracts.Deployment.CoordinatorProxy.Hex()) || !strings.EqualFold(a.SettlementVault.Hex(), contracts.Deployment.SettlementVault.Hex()) {
		return false
	}
	for _, epoch := range contracts.Epochs {
		if epoch.Epoch != a.Epoch {
			continue
		}
		for _, operator := range epoch.Operators {
			if operator.NoID != a.NoID {
				continue
			}
			return strings.EqualFold(operator.PayoutRoot, "0x"+hex.EncodeToString(a.PayoutRoot[:])) && strings.EqualFold(operator.ArtifactHash, "0x"+strings.TrimPrefix(a.ContentHash, "sha256:"))
		}
	}
	return false
}

func inspectValidatorIntent(stateDir string, validatorID int) ValidatorObservation {
	result := ValidatorObservation{ValidatorID: validatorID}
	b, err := os.ReadFile(filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "steering-intents.json"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	type intent struct {
		SubnetEpoch uint64   `json:"subnet_epoch"`
		SelfUID     uint16   `json:"self_uid"`
		MaskedUIDs  []uint16 `json:"masked_uids"`
		VectorHash  string   `json:"vector_hash"`
		Status      string   `json:"status"`
		UIDs        []uint16 `json:"uids"`
		Scores      []struct {
			Numerator   string `json:"numerator"`
			Denominator string `json:"denominator"`
		} `json:"scores"`
		Values []uint16 `json:"values"`
	}
	var store struct {
		Schema  string   `json:"schema"`
		Current *intent  `json:"current"`
		History []intent `json:"history"`
	}
	if json.Unmarshal(b, &store) != nil || store.Schema != "urnetwork-validator-steering-intent-v2" {
		result.Error = "invalid steering intent schema"
		return result
	}
	all := append([]intent(nil), store.History...)
	if store.Current != nil {
		all = append(all, *store.Current)
		result.CurrentStatus = store.Current.Status
		result.CurrentEpoch = store.Current.SubnetEpoch
		result.VectorHash = store.Current.VectorHash
	}
	var latestApplied *intent
	for index := range all {
		item := &all[index]
		if item.Status == "finalized" || item.Status == "applied" {
			result.FinalizedIntents++
		}
		if item.Status == "applied" {
			result.AppliedIntents++
			if latestApplied == nil || item.SubnetEpoch > latestApplied.SubnetEpoch {
				latestApplied = item
			}
			if len(item.Values) != 0 {
				values, _ := json.Marshal(item.Values)
				result.ValuesHash = bytesSHA256(values)
			}
		}
		if item.VectorHash != "" {
			result.IntentHashes = append(result.IntentHashes, item.VectorHash)
		}
	}
	if latestApplied != nil && len(latestApplied.UIDs) == len(latestApplied.Scores) && len(latestApplied.UIDs) == len(latestApplied.Values) {
		result.SelfUID = latestApplied.SelfUID
		result.MaskedUIDs = append([]uint16(nil), latestApplied.MaskedUIDs...)
		for i, uid := range latestApplied.UIDs {
			result.AppliedWeights = append(result.AppliedWeights, IntentWeightObservation{UID: uid, Numerator: latestApplied.Scores[i].Numerator, Denominator: latestApplied.Scores[i].Denominator, Value: latestApplied.Values[i]})
		}
	}
	sort.Strings(result.IntentHashes)
	return result
}

func inspectClaimQueue(cfg *ResolvedConfig, stateDir string, minerID int) ClaimObservation {
	result := ClaimObservation{MinerID: minerID, NoID: operatorForMiner(cfg, minerID)}
	b, err := os.ReadFile(filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", minerID), "claims", "claim-queue.json"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var queue struct {
		Schema  string `json:"schema"`
		Entries map[string]struct {
			Status string `json:"status"`
			TxHash string `json:"tx_hash"`
		} `json:"entries"`
	}
	if json.Unmarshal(b, &queue) != nil || queue.Schema != "urnetwork-provider-claim-queue-v1" {
		result.Error = "invalid claim queue schema"
		return result
	}
	result.Discovered = len(queue.Entries)
	for _, entry := range queue.Entries {
		switch entry.Status {
		case "finalized":
			result.Finalized++
		case "no-claim":
			result.NoClaim++
		case "uncertain", "submitting":
			result.Uncertain++
		case "failed":
			result.Failed++
		default:
			result.Pending++
		}
		if entry.TxHash != "" {
			result.LastTxHash = entry.TxHash
		}
	}
	return result
}

func commonScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "topology_healthy", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.Status != nil && e.Current.Status.Supervisor != nil && e.Current.Status.Healthy
			return ok, fmt.Sprintf("healthy=%t", ok)
		}},
		{ID: "contracts_installed", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.Status != nil && e.Current.Status.Contracts != nil && e.Current.Status.Contracts.Deployment != nil
			return ok, fmt.Sprintf("installed=%t", ok)
		}},
		{ID: "operator_count", Check: func(e *scenarioEvaluation) (bool, string) {
			got := uint64(0)
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				got = e.Current.Status.Contracts.OperatorCount
			}
			return got == uint64(e.Cfg.Config.Topology.Operators), fmt.Sprintf("got=%d want=%d", got, e.Cfg.Config.Topology.Operators)
		}},
		{ID: "runtime_code_matches", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.Status != nil && e.Current.Status.Contracts != nil && e.Current.Status.Contracts.RuntimeCodeMatches
			return ok, fmt.Sprintf("matches=%t", ok)
		}},
		{ID: "policy_hash_matches", Check: func(e *scenarioEvaluation) (bool, string) {
			got := ""
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				got = e.Current.Status.Contracts.PolicyHash
			}
			return strings.EqualFold(got, e.Cfg.PolicyHash), fmt.Sprintf("got=%s want=%s", got, e.Cfg.PolicyHash)
		}},
		{ID: "rao_conservation", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract counters unavailable"
			}
			c := e.Current.Status.Contracts
			captured, a := new(big.Int).SetString(c.TotalCaptured, 10)
			paid, b := new(big.Int).SetString(c.TotalPaid, 10)
			outstanding, d := new(big.Int).SetString(c.Outstanding, 10)
			ok := a && b && d && c.ConservationHolds && captured.Cmp(new(big.Int).Add(paid, outstanding)) == 0
			return ok, fmt.Sprintf("captured=%s paid=%s outstanding=%s", c.TotalCaptured, c.TotalPaid, c.Outstanding)
		}},
		{ID: "public_identities", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.PublicIdentitiesValid, fmt.Sprintf("valid=%t count=%d", e.Current.PublicIdentitiesValid, e.Current.PublicIdentityCount)
		}},
		{ID: "fleet_commitment_and_bindings", Check: func(e *scenarioEvaluation) (bool, string) {
			want := e.Cfg.Config.Topology.HeadFleets * e.Cfg.Config.Topology.ClientsPerHeadFleet
			ok := e.Current.FleetCommitmentValid && e.Current.FleetBindingsValid && e.Current.FleetBindingCount == want
			return ok, fmt.Sprintf("commitment=%t bindings=%d/%d valid=%t", e.Current.FleetCommitmentValid, e.Current.FleetBindingCount, want, e.Current.FleetBindingsValid)
		}},
		{ID: "native_custody_hotkeys", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.ReserveValidatorRegistered && e.Current.EscrowHotkeyRegistered && e.Current.NativeCustodyError == ""
			return ok, fmt.Sprintf("reserve_registered=%t reserve_uid=%d escrow_registered=%t escrow_uid=%d error=%s", e.Current.ReserveValidatorRegistered, e.Current.ReserveValidatorUID, e.Current.EscrowHotkeyRegistered, e.Current.EscrowHotkeyUID, e.Current.NativeCustodyError)
		}},
		{ID: "operator_public_apis", Check: func(e *scenarioEvaluation) (bool, string) {
			if len(e.Current.Operators) != e.Cfg.Config.Topology.Operators {
				return false, fmt.Sprintf("operators=%d", len(e.Current.Operators))
			}
			for _, operator := range e.Current.Operators {
				if !operator.Healthy || operator.Error != "" {
					return false, fmt.Sprintf("operator %d: %s", operator.NoID, operator.Error)
				}
			}
			return true, fmt.Sprintf("operators=%d", len(e.Current.Operators))
		}},
	}
}

func epochScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "required_finalized_epochs", Check: func(e *scenarioEvaluation) (bool, string) {
			got := uint64(0)
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				got = e.Current.Status.Contracts.CurrentEpoch
			}
			return got >= e.GoalEpoch, fmt.Sprintf("current=%d goal=%d", got, e.GoalEpoch)
		}},
		{ID: "per_no_verify_coverage", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, operator := range e.Current.Operators {
				if operator.StatsRows == 0 || operator.ProofRows == 0 || !strings.EqualFold(operator.StatsPolicyHash, e.Cfg.PolicyHash) || !strings.EqualFold(operator.ProofsPolicyHash, e.Cfg.PolicyHash) {
					return false, fmt.Sprintf("no=%d stats=%d proofs=%d", operator.NoID, operator.StatsRows, operator.ProofRows)
				}
			}
			return len(e.Current.Operators) > 0, "all operators have policy-bound stats and proofs"
		}},
		{ID: "payout_artifacts_reconstruct", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, operator := range e.Current.Operators {
				if operator.ValidArtifacts == 0 || operator.ExpectedFinalizedArtifacts == 0 || operator.MatchingArtifacts < operator.ExpectedFinalizedArtifacts {
					return false, fmt.Sprintf("no=%d valid=%d matching=%d expected_finalized=%d", operator.NoID, operator.ValidArtifacts, operator.MatchingArtifacts, operator.ExpectedFinalizedArtifacts)
				}
			}
			return len(e.Current.Operators) > 0, "all fetched artifacts match finalized roots"
		}},
		{ID: "validator_intents_finalized", Check: func(e *scenarioEvaluation) (bool, string) {
			if len(e.Current.Validators) != e.Cfg.Config.Topology.Validators {
				return false, "validator evidence missing"
			}
			for _, validator := range e.Current.Validators {
				if validator.Error != "" || validator.FinalizedIntents == 0 || validator.AppliedIntents == 0 {
					return false, fmt.Sprintf("validator=%d finalized=%d applied=%d error=%s", validator.ValidatorID, validator.FinalizedIntents, validator.AppliedIntents, validator.Error)
				}
			}
			return true, "all validator intents finalized and applied"
		}},
	}
}

func releaseScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "governance_adversarial_drill_complete", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.GovernanceDrill == nil {
				return false, "governance evidence unavailable: " + e.Current.GovernanceDrillError
			}
			evidence := e.Current.GovernanceDrill
			ok := evidence.Stage == "complete" && evidence.After != nil && len(evidence.ProbeResults) == 4
			for _, succeeded := range evidence.ProbeResults {
				ok = ok && !succeeded
			}
			return ok, fmt.Sprintf("stage=%s probes=%d", evidence.Stage, len(evidence.ProbeResults))
		}},
		{ID: "reserve_delegate_take_zero", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.ReserveDelegateTake == nil {
				return false, "reserve delegate take is unavailable"
			}
			return *e.Current.ReserveDelegateTake == 0, fmt.Sprintf("take_parts_per_65535=%d", *e.Current.ReserveDelegateTake)
		}},
		{ID: "per_no_deposits_and_conviction", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			deposited := map[uint64]bool{}
			for _, epoch := range e.Current.Status.Contracts.Epochs {
				for _, op := range epoch.Operators {
					amount, ok := new(big.Int).SetString(op.DepositRao, 10)
					if ok && amount.Sign() > 0 {
						deposited[op.NoID] = true
					}
				}
			}
			for _, op := range e.Current.Status.Contracts.Operators {
				conviction, ok := new(big.Int).SetString(op.ConvictionRao, 10)
				if !deposited[op.NoID] || !ok || conviction.Sign() <= 0 {
					return false, fmt.Sprintf("no=%d deposited=%t conviction=%s", op.NoID, deposited[op.NoID], op.ConvictionRao)
				}
			}
			return len(deposited) == e.Cfg.Config.Topology.Operators, "every NO has isolated deposit and conviction"
		}},
		{ID: "voluntary_conviction_crosses_tier_prospectively", Check: func(e *scenarioEvaluation) (bool, string) {
			report := analyzeScenarioObservation(e.Cfg, e.Current)
			ok := e.Current.VoluntaryConvictionValid && report.TierReconstructionComplete && report.VoluntaryConvictionObserved && report.ConvictionTierCrossed
			return ok, fmt.Sprintf("evidence_valid=%t reconstructed=%t voluntary=%t tier_crossed=%t error=%s", e.Current.VoluntaryConvictionValid, report.TierReconstructionComplete, report.VoluntaryConvictionObserved, report.ConvictionTierCrossed, e.Current.VoluntaryConvictionError)
		}},
		{ID: "quality_cohorts_differ", Check: func(e *scenarioEvaluation) (bool, string) {
			byNO := map[int]OperatorObservation{}
			for _, operator := range e.Current.Operators {
				byNO[operator.NoID] = operator
			}
			faulted := byNO[e.Cfg.Config.Scenarios.QualityFaultOperator]
			if faulted.Assignments < e.Cfg.Policy.Verify.ReliabilityAMin {
				return false, fmt.Sprintf("faulted_no=%d assignments=%d", faulted.NoID, faulted.Assignments)
			}
			for noID, operator := range byNO {
				if noID != faulted.NoID && operator.Assignments >= e.Cfg.Policy.Verify.ReliabilityAMin && operator.ReliabilityPPM > faulted.ReliabilityPPM {
					return true, fmt.Sprintf("healthy_no=%d reliability_ppm=%d faulted_no=%d reliability_ppm=%d", noID, operator.ReliabilityPPM, faulted.NoID, faulted.ReliabilityPPM)
				}
			}
			return false, fmt.Sprintf("faulted_no=%d reliability_ppm=%d cohorts=%v", faulted.NoID, faulted.ReliabilityPPM, byNO)
		}},
		{ID: "validator_vectors_independently_applied", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, validator := range e.Current.Validators {
				if validator.ValuesHash == "" {
					return false, fmt.Sprintf("validator %d has no applied vector", validator.ValidatorID)
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, fmt.Sprintf("applied_vectors=%d", len(e.Current.Validators))
		}},
		{ID: "signed_weight_cap_enforced", Check: func(e *scenarioEvaluation) (bool, string) {
			cap := e.Cfg.Policy.Steering.MaxWeightLimitU16
			for _, validator := range e.Current.Validators {
				ok, maximum, sum := weightValuesRespectCap(validator.AppliedWeights, cap)
				if !ok {
					return false, fmt.Sprintf("validator=%d max=%d sum=%d cap=%d", validator.ValidatorID, maximum, sum, cap)
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, fmt.Sprintf("validators=%d cap=%d", len(e.Current.Validators), cap)
		}},
		{ID: "native_head_weight_observed", Check: func(e *scenarioEvaluation) (bool, string) {
			if !e.Current.FleetBindingsValid {
				return false, "fleet binding evidence is invalid"
			}
			if len(e.Current.HeadFleetUIDs) != e.Cfg.Config.Topology.HeadFleets {
				return false, fmt.Sprintf("head fleet UIDs=%v, want %d", e.Current.HeadFleetUIDs, e.Cfg.Config.Topology.HeadFleets)
			}
			observed := map[uint16]bool{}
			for _, validator := range e.Current.Validators {
				masked := uint16Set(validator.MaskedUIDs)
				for _, headUID := range e.Current.HeadFleetUIDs {
					if masked[headUID] {
						continue
					}
					found := false
					for _, weight := range validator.AppliedWeights {
						if weight.UID == headUID && weight.Value > 0 {
							found = true
							observed[headUID] = true
						}
					}
					if !found {
						return false, fmt.Sprintf("validator=%d head_uid=%d has no nonzero applied native weight", validator.ValidatorID, headUID)
					}
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators && len(observed) == len(e.Current.HeadFleetUIDs), fmt.Sprintf("head_uids=%v observed=%v validators=%d", e.Current.HeadFleetUIDs, observed, len(e.Current.Validators))
		}},
		{ID: "two_fleet_shared_prefix_split", Check: func(e *scenarioEvaluation) (bool, string) {
			if len(e.Current.HeadFleetUIDs) != 2 {
				return false, fmt.Sprintf("head_uids=%v", e.Current.HeadFleetUIDs)
			}
			want := new(big.Rat).SetFrac64(1, 2)
			checked := 0
			for _, validator := range e.Current.Validators {
				masked := uint16Set(validator.MaskedUIDs)
				if masked[e.Current.HeadFleetUIDs[0]] || masked[e.Current.HeadFleetUIDs[1]] {
					continue
				}
				weights := map[uint16]*big.Rat{}
				for _, weight := range validator.AppliedWeights {
					n, nOK := new(big.Int).SetString(weight.Numerator, 10)
					d, dOK := new(big.Int).SetString(weight.Denominator, 10)
					if nOK && dOK && d.Sign() > 0 {
						weights[weight.UID] = new(big.Rat).SetFrac(n, d)
					}
				}
				first := weights[e.Current.HeadFleetUIDs[0]]
				if first == nil || first.Cmp(want) != 0 {
					return false, fmt.Sprintf("validator=%d first head score=%v, want 1/2", validator.ValidatorID, first)
				}
				for _, uid := range e.Current.HeadFleetUIDs[1:] {
					if weights[uid] == nil || weights[uid].Cmp(want) != 0 {
						return false, fmt.Sprintf("validator=%d head_uid=%d score=%v, want 1/2", validator.ValidatorID, uid, weights[uid])
					}
				}
				checked++
			}
			return checked > 0, fmt.Sprintf("unaffiliated_validators=%d exact_head_scores=1/2", checked)
		}},
		{ID: "validator_self_uids_masked", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, validator := range e.Current.Validators {
				masked := uint16Set(validator.MaskedUIDs)
				expected := map[uint16]bool{validator.SelfUID: true}
				controlled := uint64Set(controlledNOIDsForValidator(validator.ValidatorID))
				if e.Current.Status != nil && e.Current.Status.Contracts != nil {
					for _, operator := range e.Current.Status.Contracts.Operators {
						if controlled[operator.NoID] && operator.PoolLive {
							expected[operator.PoolUID] = true
						}
					}
				}
				for fleetIndex, uid := range e.Current.HeadFleetUIDs {
					for member := 1; member <= e.Cfg.Config.Topology.ClientsPerHeadFleet; member++ {
						miner := fleetMemberMinerIndex(e.Cfg, fleetIndex+1, member)
						noID := uint64(operatorForMiner(e.Cfg, miner))
						if controlled[noID] {
							expected[uid] = true
						}
					}
				}
				for uid := range expected {
					if !masked[uid] {
						return false, fmt.Sprintf("validator=%d required_uid=%d missing from mask=%v", validator.ValidatorID, uid, validator.MaskedUIDs)
					}
				}
				for _, weight := range validator.AppliedWeights {
					if expected[weight.UID] && weight.Value != 0 {
						return false, fmt.Sprintf("validator=%d masked_uid=%d value=%d", validator.ValidatorID, weight.UID, weight.Value)
					}
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, fmt.Sprintf("validators=%d", len(e.Current.Validators))
		}},
		{ID: "validator_pool_scores_are_non_global", Check: func(e *scenarioEvaluation) (bool, string) {
			poolUID := map[int]uint16{}
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				for _, operator := range e.Current.Status.Contracts.Operators {
					if operator.PoolLive {
						poolUID[int(operator.NoID)] = operator.PoolUID
					}
				}
			}
			faultedNO := e.Cfg.Config.Scenarios.QualityFaultOperator
			if _, ok := poolUID[faultedNO]; len(poolUID) != e.Cfg.Config.Topology.Operators || !ok {
				return false, fmt.Sprintf("pool_uids=%v", poolUID)
			}
			compared := 0
			for _, validator := range e.Current.Validators {
				weights := map[uint16]*big.Rat{}
				masked := uint16Set(validator.MaskedUIDs)
				for _, weight := range validator.AppliedWeights {
					n, nOK := new(big.Int).SetString(weight.Numerator, 10)
					d, dOK := new(big.Int).SetString(weight.Denominator, 10)
					if nOK && dOK && d.Sign() > 0 {
						weights[weight.UID] = new(big.Rat).SetFrac(n, d)
					}
				}
				faultedUID := poolUID[faultedNO]
				if masked[faultedUID] {
					continue
				}
				faultedScore := weights[faultedUID]
				if faultedScore == nil {
					return false, fmt.Sprintf("validator=%d has no faulted pool uid=%d", validator.ValidatorID, faultedUID)
				}
				different := false
				for noID, uid := range poolUID {
					if noID != faultedNO && !masked[uid] && weights[uid] != nil && weights[uid].Cmp(faultedScore) != 0 {
						different = true
					}
				}
				if len(poolUID)-len(controlledNOIDsForValidator(validator.ValidatorID)) < 2 {
					continue
				}
				if !different {
					return false, fmt.Sprintf("validator=%d pool scores are global/equal", validator.ValidatorID)
				}
				compared++
			}
			return compared > 0, fmt.Sprintf("unaffiliated_validator_comparisons=%d", compared)
		}},
		{ID: "claims_finalized_per_no", Check: func(e *scenarioEvaluation) (bool, string) {
			finalized := map[int]int{}
			uncertain := 0
			for _, claim := range e.Current.Claims {
				finalized[claim.NoID] += claim.Finalized
				uncertain += claim.Uncertain + claim.Failed
			}
			for noID := 1; noID <= e.Cfg.Config.Topology.Operators; noID++ {
				if finalized[noID] == 0 {
					return false, fmt.Sprintf("no=%d finalized_claims=0 uncertain_or_failed=%d", noID, uncertain)
				}
			}
			return uncertain == 0, fmt.Sprintf("finalized_by_no=%v uncertain_or_failed=%d", finalized, uncertain)
		}},
		{ID: "pool_payments_observed", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			paid, ok := new(big.Int).SetString(e.Current.Status.Contracts.TotalPaid, 10)
			return ok && paid.Sign() > 0, "total_paid_rao=" + e.Current.Status.Contracts.TotalPaid
		}},
		{ID: "reserve_principal_backed", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			principal, a := new(big.Int).SetString(e.Current.Status.Contracts.ReservePrincipal, 10)
			stake, b := new(big.Int).SetString(e.Current.Status.Contracts.ReserveLiveStake, 10)
			return a && b && principal.Sign() > 0 && stake.Cmp(principal) >= 0, fmt.Sprintf("principal=%s live_stake=%s", e.Current.Status.Contracts.ReservePrincipal, e.Current.Status.Contracts.ReserveLiveStake)
		}},
		{ID: "reserve_yield_auto_compounds", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			principal, a := new(big.Int).SetString(e.Current.Status.Contracts.ReservePrincipal, 10)
			stake, b := new(big.Int).SetString(e.Current.Status.Contracts.ReserveLiveStake, 10)
			return a && b && principal.Sign() > 0 && stake.Cmp(principal) > 0, fmt.Sprintf("principal=%s live_stake=%s", e.Current.Status.Contracts.ReservePrincipal, e.Current.Status.Contracts.ReserveLiveStake)
		}},
		{ID: "finalized_entitlements_per_no", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			finalized := map[uint64]bool{}
			for _, epoch := range e.Current.Status.Contracts.Epochs {
				for _, op := range epoch.Operators {
					if op.Status == 2 && op.PayoutRoot != "0x"+strings.Repeat("0", 64) {
						finalized[op.NoID] = true
					}
				}
			}
			return len(finalized) == e.Cfg.Config.Topology.Operators, fmt.Sprintf("finalized_no_count=%d", len(finalized))
		}},
	}
}

func uint16Set(values []uint16) map[uint16]bool {
	result := make(map[uint16]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

// weightValuesRespectCap checks the exact integer inequality used by
// Subtensor's normalized max-weight rule without introducing float rounding.
func weightValuesRespectCap(weights []IntentWeightObservation, cap uint16) (bool, uint16, uint64) {
	var maximum uint16
	var sum uint64
	for _, weight := range weights {
		sum += uint64(weight.Value)
		if weight.Value > maximum {
			maximum = weight.Value
		}
	}
	if cap == 0 || sum == 0 {
		return false, maximum, sum
	}
	return uint64(maximum)*uint64(^uint16(0)) <= sum*uint64(cap), maximum, sum
}

func uint64Set(values []uint64) map[uint64]bool {
	result := make(map[uint64]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func annotateScenarioExpectedFaults(observation *ScenarioObservation, records []ScenarioFaultRecord) error {
	if observation == nil {
		return nil
	}
	observation.ExpectedFaultIDs = nil
	observation.ExpectedFaultTargets = nil
	seenTargets := map[string]bool{}
	for _, record := range records {
		if record.Status != "active" {
			continue
		}
		observation.ExpectedFaultIDs = append(observation.ExpectedFaultIDs, record.ID)
		for _, target := range append(append([]string(nil), record.Targets...), record.Impacts...) {
			if !seenTargets[target] {
				seenTargets[target] = true
				observation.ExpectedFaultTargets = append(observation.ExpectedFaultTargets, target)
			}
		}
	}
	sort.Strings(observation.ExpectedFaultIDs)
	sort.Strings(observation.ExpectedFaultTargets)
	observation.ObservationHash = ""
	var err error
	observation.ObservationHash, err = canonicalHashHex(observation)
	return err
}

func scenarioFaultTargets(records []ScenarioFaultRecord, head uint64, includeDue bool) []string {
	seen := map[string]bool{}
	for _, record := range records {
		expected := record.Status == "active"
		if includeDue && record.Status == "pending" && head >= record.TriggerBlock {
			expected = true
		}
		if expected {
			for _, target := range append(append([]string(nil), record.Targets...), record.Impacts...) {
				seen[target] = true
			}
		}
	}
	targets := make([]string, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func enableContinuousAdversaries(cfg *ResolvedConfig, definition *scenarioDefinition) error {
	if cfg == nil || cfg.Config == nil || definition == nil {
		return errors.New("continuous adversarial scenario configuration is incomplete")
	}
	matrix, err := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		return fmt.Errorf("adversarial scenario matrix: %w", err)
	}
	if err := validateAdversarialActorCoverage(matrix, releaseAdversaryActorIDs); err != nil {
		return err
	}
	definition.AdversarialMatrixHash = matrix.Hash
	return nil
}

func scenarioDefinitionFor(cfg *ResolvedConfig, name string) (scenarioDefinition, error) {
	if name == "" {
		name = cfg.Config.Scenarios.Launch
	}
	definition := scenarioDefinition{Name: name, Checks: commonScenarioChecks()}
	switch name {
	case "smoke":
		return definition, nil
	case "precompile-conformance":
		definition.Checks = append(definition.Checks, scenarioCheck{ID: "precompile_conformance_complete", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.PrecompileConformanceValid, fmt.Sprintf("valid=%t error=%s", e.Current.PrecompileConformanceValid, e.Current.PrecompileConformanceError)
		}})
		return definition, nil
	case "epoch":
		definition.GoalEpochs = 1
		definition.Checks = append(definition.Checks, epochScenarioChecks()...)
		return definition, nil
	case "release-1.0":
		if cfg.Config.Scenarios.ShortEpochs < 1 {
			return scenarioDefinition{}, errors.New("release scenario requires short_epochs > 0")
		}
		definition.GoalEpochs = uint64(cfg.Config.Scenarios.ShortEpochs)
		faults, err := releaseCampaignFaults(cfg)
		if err != nil {
			return scenarioDefinition{}, err
		}
		definition.Faults = faults
		definition.Checks = append(definition.Checks, epochScenarioChecks()...)
		definition.Checks = append(definition.Checks, releaseScenarioChecks()...)
		matrix, err := loadScenarioMatrix(cfg.Repos.SN)
		if err != nil {
			return scenarioDefinition{}, fmt.Errorf("release scenario matrix: %w", err)
		}
		definition.MatrixHash = matrix.Hash
		if err := validateScenarioMatrixCoverage(matrix, definition.Checks, definition.Faults); err != nil {
			return scenarioDefinition{}, err
		}
		definition.Checks = append(definition.Checks, scenarioCheck{ID: "scenario_matrix_coverage", Check: func(*scenarioEvaluation) (bool, string) {
			return true, fmt.Sprintf("20/20 matrix rows mapped; hash=%s", matrix.Hash)
		}})
		if err := enableContinuousAdversaries(cfg, &definition); err != nil {
			return scenarioDefinition{}, err
		}
		return definition, nil
	case "production-soak":
		if cfg.Config.Scenarios.ProductionEpochs < 2 {
			return scenarioDefinition{}, errors.New("production soak requires at least two complete production epochs")
		}
		// One epoch reaches the scheduled boundary; the configured count then
		// represents complete production epochs after that boundary.
		definition.GoalEpochs = uint64(cfg.Config.Scenarios.ProductionEpochs) + 1
		definition.Faults = productionRollingFaults(cfg)
		definition.Checks = append(definition.Checks, epochScenarioChecks()...)
		definition.Checks = append(definition.Checks, productionScenarioChecks()...)
		if err := enableContinuousAdversaries(cfg, &definition); err != nil {
			return scenarioDefinition{}, err
		}
		return definition, nil
	default:
		if fault, ok := namedProcessFault(cfg, name); ok {
			definition.GoalEpochs = 1
			definition.Faults = []scenarioFaultSpec{fault}
			definition.Checks = append(definition.Checks, epochScenarioChecks()...)
			return definition, nil
		}
		return scenarioDefinition{}, fmt.Errorf("unknown scenario %q", name)
	}
}

func scenarioTimeout(cfg *ResolvedConfig, definition scenarioDefinition) time.Duration {
	blocks := definition.GoalEpochs * cfg.Policy.Settlement.EpochBlocks
	if definition.Name == "production-soak" {
		blocks = cfg.Policy.Settlement.EpochBlocks + uint64(cfg.Config.Scenarios.ProductionEpochs)*cfg.Policy.ProductionCadence.EpochBlocks + cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
	}
	for _, fault := range definition.Faults {
		if end := fault.TriggerOffsetBlocks + fault.DurationBlocks; end > blocks {
			blocks = end
		}
	}
	if blocks == 0 {
		return 2 * time.Minute
	}
	seconds := blocks*cfg.Public.Chain.ExpectedBlockSeconds + 10*cfg.Public.Chain.ExpectedBlockSeconds + 120
	timeout := time.Duration(seconds) * time.Second
	if definition.AdversarialMatrixHash != "" {
		minimum := time.Duration(cfg.Config.Scenarios.Adversaries.MinimumSamplesPerActor+2) * time.Duration(cfg.Config.Scenarios.Adversaries.SampleIntervalMilliseconds) * time.Millisecond
		minimum += time.Duration(cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds) * time.Millisecond
		if timeout < minimum {
			timeout = minimum
		}
	}
	return timeout
}

func productionScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "verify_key_rotation_preserves_history", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, operator := range e.Current.Operators {
				keys := map[byte]bool{}
				proofs := map[byte]bool{}
				for _, id := range operator.VerifyKeyIDs {
					keys[id] = true
				}
				for _, id := range operator.ProofKeyIDs {
					proofs[id] = true
				}
				if !keys[0] || !keys[1] || !proofs[0] || !proofs[1] {
					return false, fmt.Sprintf("no=%d keys=%v proof_keys=%v", operator.NoID, operator.VerifyKeyIDs, operator.ProofKeyIDs)
				}
			}
			return len(e.Current.Operators) == e.Cfg.Config.Topology.Operators, "both old and rotated proofs remain publicly verifiable"
		}},
		{ID: "production_cadence_active", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract policy unavailable"
			}
			got := e.Current.Status.Contracts.Policy
			want := e.Cfg.Policy.ProductionCadence
			ok := got.EffectiveEpoch >= want.AfterAcceleratedEpochs && got.EpochBlocks == want.EpochBlocks && got.RootCommitWindowBlocks == want.RootCommitWindowBlocks && got.FinalizeOffsetBlocks == want.FinalizeOffsetBlocks && got.CloseGraceBlocks == want.CloseGraceBlocks
			return ok, fmt.Sprintf("effective=%d epoch=%d root=%d finalize=%d close=%d", got.EffectiveEpoch, got.EpochBlocks, got.RootCommitWindowBlocks, got.FinalizeOffsetBlocks, got.CloseGraceBlocks)
		}},
		{ID: "complete_production_epochs", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract policy unavailable"
			}
			contracts := e.Current.Status.Contracts
			effective := contracts.Policy.EffectiveEpoch
			complete := uint64(0)
			if contracts.CurrentEpoch > effective {
				complete = contracts.CurrentEpoch - effective
			}
			want := uint64(e.Cfg.Config.Scenarios.ProductionEpochs)
			return complete >= want, fmt.Sprintf("complete=%d want=%d effective=%d current=%d", complete, want, effective, contracts.CurrentEpoch)
		}},
	}
}

func appendObservation(path string, observation *ScenarioObservation) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	b, err := json.Marshal(observation)
	if err == nil {
		_, err = w.Write(append(b, '\n'))
	}
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func evaluateScenario(cfg *ResolvedConfig, definition scenarioDefinition, start, current *ScenarioObservation, started time.Time) []AssertionRecord {
	goal := uint64(0)
	if start != nil && start.Status != nil && start.Status.Contracts != nil {
		goal = start.Status.Contracts.CurrentEpoch + definition.GoalEpochs
	}
	evaluation := &scenarioEvaluation{Cfg: cfg, Start: start, Current: current, GoalEpoch: goal, Definition: definition}
	now := time.Now().UTC()
	assertions := make([]AssertionRecord, 0, len(definition.Checks))
	for _, check := range definition.Checks {
		passed, message := check.Check(evaluation)
		assertions = append(assertions, AssertionRecord{ID: check.ID, Passed: passed, Message: message, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	return assertions
}

func appendFaultAssertions(assertions []AssertionRecord, records []ScenarioFaultRecord, started time.Time, current *ScenarioObservation) []AssertionRecord {
	now := time.Now().UTC()
	for _, record := range records {
		passed := record.Status == "restored" && record.AppliedBlock >= record.TriggerBlock && record.RestoredBlock >= record.RestoreBlock && len(record.Processes) == len(record.Targets)
		if passed && (record.Kind == "process-restart" || record.Kind == "container-restart") {
			if len(record.RestoredProcesses) != len(record.Processes) {
				passed = false
			} else {
				before := map[string]int{}
				for _, process := range record.Processes {
					before[process.ID] = process.PID
				}
				for _, process := range record.RestoredProcesses {
					if process.PID <= 1 || process.PID == before[process.ID] {
						passed = false
					}
				}
			}
		}
		message := fmt.Sprintf("status=%s targets=%v applied=%d restored=%d", record.Status, record.Targets, record.AppliedBlock, record.RestoredBlock)
		if record.Error != "" {
			message += " error=" + record.Error
		}
		assertions = append(assertions, AssertionRecord{ID: "fault_" + record.ID, Passed: passed, Message: message, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	return assertions
}

func assertionsPass(assertions []AssertionRecord) bool {
	if len(assertions) == 0 {
		return false
	}
	for _, assertion := range assertions {
		if !assertion.Passed {
			return false
		}
	}
	return true
}

func writeInitialScenarioFailure(cfg *ResolvedConfig, runDir, runID, definitionHash string, definition scenarioDefinition, started time.Time, observation *ScenarioObservation, failure error) (*ScenarioResult, error) {
	completed := time.Now().UTC()
	observationHash := ""
	if observation != nil {
		observationHash = observation.ObservationHash
		_ = appendObservation(filepath.Join(runDir, "observations.jsonl"), observation)
	}
	assertion := AssertionRecord{ID: "initial_observation", Passed: false, Message: failure.Error(), StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano), DurationSeconds: completed.Sub(started).Seconds(), ObservationHash: observationHash}
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: runID,
		DeploymentID: cfg.Config.Deployment.DeploymentID, Name: definition.Name, ScenarioDefinition: definitionHash, ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano),
		Assertions: []AssertionRecord{assertion}, Result: "fail",
	}
	attachScenarioAnomalyGate(result, completed, nil, observation)
	result.EvidenceHash, _ = canonicalScenarioResultHash(result)
	if err := writeScenarioOutputs(cfg, runDir, result, observation); err != nil {
		return result, err
	}
	return result, failure
}

func finalizeAdversaryEvidence(campaign adversaryCampaign, runDir string, started time.Time, observationHash string, completed time.Time) (*AdversaryCampaignEvidence, []AssertionRecord, error) {
	if campaign == nil {
		return nil, nil, nil
	}
	campaign.MarkHappyPathCompleted(completed)
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	evidence, stopErr := campaign.Stop(stopCtx)
	if evidence != nil {
		b, err := json.MarshalIndent(evidence, "", "  ")
		if err == nil {
			err = atomicWrite(filepath.Join(runDir, "adversaries.json"), append(b, '\n'), 0o644)
		}
		if err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	assertions := adversaryAssertions(evidence, started, observationHash)
	if stopErr != nil {
		now := time.Now().UTC()
		assertions = append(assertions, AssertionRecord{ID: "adversary_campaign_stop", Passed: false, Message: stopErr.Error(), StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: observationHash})
		sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	}
	return evidence, assertions, stopErr
}

func runScenarioWithProbe(ctx context.Context, cfg *ResolvedConfig, stateDir string, definition scenarioDefinition, probe scenarioProbe, options scenarioRunOptions) (*ScenarioResult, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Duration(max64(1, cfg.Public.Chain.ExpectedBlockSeconds)) * time.Second
	}
	if options.Timeout <= 0 {
		options.Timeout = scenarioTimeout(cfg, definition)
	}
	started := options.Now().UTC()
	runID := fmt.Sprintf("%s-%s", started.Format("20060102T150405.000000000Z"), definition.Name)
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	definitionHash, _ := canonicalHashHex(struct {
		Name                  string              `json:"name"`
		GoalEpochs            uint64              `json:"goal_epochs"`
		Checks                []string            `json:"checks"`
		Faults                []scenarioFaultSpec `json:"faults,omitempty"`
		MatrixHash            string              `json:"scenario_matrix_hash,omitempty"`
		AdversarialMatrixHash string              `json:"adversarial_matrix_hash,omitempty"`
	}{Name: definition.Name, GoalEpochs: definition.GoalEpochs, Checks: func() []string {
		ids := make([]string, len(definition.Checks))
		for i := range definition.Checks {
			ids[i] = definition.Checks[i].ID
		}
		return ids
	}(), Faults: definition.Faults, MatrixHash: definition.MatrixHash, AdversarialMatrixHash: definition.AdversarialMatrixHash})
	if definition.AdversarialMatrixHash != "" {
		if options.Adversaries == nil {
			return nil, errors.New("release scenario requires a continuous adversarial campaign")
		}
		if err := options.Adversaries.Start(ctx); err != nil {
			return nil, fmt.Errorf("start continuous adversarial campaign: %w", err)
		}
		options.Adversaries.MarkHappyPathStarted(options.Now().UTC())
	}
	adversariesFinalized := false
	observationHistory := []*ScenarioObservation{}
	defer func() {
		if options.Adversaries == nil || adversariesFinalized {
			return
		}
		options.Adversaries.MarkHappyPathCompleted(options.Now().UTC())
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = options.Adversaries.Stop(cleanupCtx)
	}()
	initialFailure := func(observation *ScenarioObservation, failure error) (*ScenarioResult, error) {
		failureHistory := append([]*ScenarioObservation(nil), observationHistory...)
		if observation != nil && (len(failureHistory) == 0 || failureHistory[len(failureHistory)-1] != observation) {
			failureHistory = append(failureHistory, observation)
		}
		result, resultErr := writeInitialScenarioFailure(cfg, runDir, runID, definitionHash, definition, started, observation, failure)
		observationHash := ""
		if len(failureHistory) != 0 {
			observationHash = failureHistory[len(failureHistory)-1].ObservationHash
		}
		evidence, adversaryRecords, stopErr := finalizeAdversaryEvidence(options.Adversaries, runDir, started, observationHash, options.Now().UTC())
		adversariesFinalized = true
		var rewriteErr error
		if result != nil {
			result.Adversaries = evidence
			result.Assertions = append(result.Assertions, adversaryRecords...)
			attachScenarioAnomalyGate(result, options.Now().UTC(), nil, observation, failureHistory...)
			result.EvidenceHash, _ = canonicalScenarioResultHash(result)
			rewriteErr = writeScenarioOutputs(cfg, runDir, result, observation)
		}
		return result, errors.Join(resultErr, stopErr, rewriteErr)
	}
	// Preparation is part of the measured happy path. In particular, release
	// governance and key-rotation actions run while adversaries are active, and
	// a preparation failure is persisted with the same anomaly/evidence gates
	// as a failure after the first chain observation.
	if options.Prepare != nil {
		if err := options.Prepare(ctx); err != nil {
			return initialFailure(nil, fmt.Errorf("prepare scenario: %w", err))
		}
	}
	if len(definition.Faults) != 0 {
		if options.FaultDriver == nil {
			return initialFailure(nil, errors.New("scenario fault schedule requires a fault driver"))
		}
		if err := options.FaultDriver.Recover(ctx); err != nil {
			return initialFailure(nil, fmt.Errorf("recover prior scenario fault: %w", err))
		}
	}

	start, err := probe.Snapshot(ctx)
	if err != nil {
		return initialFailure(nil, fmt.Errorf("initial scenario observation: %w", err))
	}
	if start.Status == nil || start.Status.Contracts == nil {
		return initialFailure(start, errors.New("scenario requires an installed contract deployment"))
	}
	observationHistory = append(observationHistory, start)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), start); err != nil {
		return initialFailure(start, fmt.Errorf("persist initial scenario observation: %w", err))
	}
	faults, err := initializeFaultRecords(start.Status.Contracts.FinalizedHead.Number, definition.Faults)
	if err != nil {
		return initialFailure(start, fmt.Errorf("initialize scenario faults: %w", err))
	}
	defer func() {
		if options.FaultDriver == nil {
			return
		}
		for i := range faults {
			if faults[i].Status == "active" {
				_, _ = options.FaultDriver.Restore(context.Background(), definition.Faults[i])
			}
		}
	}()
	current := start
	assertions := appendFaultAssertions(evaluateScenario(cfg, definition, start, current, started), faults, started, current)
	deadline := started.Add(options.Timeout)
	var faultErr error
	var terminalErr error
	var runtimeAssertions []AssertionRecord
	snapshotFailureCount := 0
scenarioLoop:
	for (!assertionsPass(assertions) || !faultsComplete(faults) || (options.Adversaries != nil && !options.Adversaries.Ready())) && options.Now().Before(deadline) {
		timer := time.NewTimer(options.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			terminalErr = ctx.Err()
			break scenarioLoop
		case <-timer.C:
		}
		next, snapshotErr := probe.Snapshot(ctx)
		if snapshotErr != nil {
			snapshotFailureCount++
			now := options.Now().UTC()
			runtimeAssertions = append(runtimeAssertions, AssertionRecord{
				ID: fmt.Sprintf("scenario_snapshot_%06d", snapshotFailureCount), Passed: false,
				Message:   fmt.Sprintf("transient scenario observation failed: %v", snapshotErr),
				StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano),
				DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
			})
			continue
		}
		previous := current
		current = next
		if err := annotateScenarioExpectedFaults(current, faults); err != nil {
			return initialFailure(current, fmt.Errorf("annotate expected scenario faults: %w", err))
		}
		observationHistory = append(observationHistory, current)
		if current.Status == nil || current.Status.Contracts == nil {
			snapshotFailureCount++
			now := options.Now().UTC()
			runtimeAssertions = append(runtimeAssertions, AssertionRecord{
				ID: fmt.Sprintf("scenario_snapshot_%06d", snapshotFailureCount), Passed: false,
				Message:   "scenario observation lost deployment or contract state",
				StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano),
				DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
			})
			terminalErr = errors.New("scenario observation lost deployment or contract state")
			current = previous
			break scenarioLoop
		}
		if len(faults) != 0 {
			if options.Adversaries != nil {
				// Attribute endpoint failures before applying a due process fault;
				// the actor may have an in-flight request when SIGTERM/SIGSTOP lands.
				options.Adversaries.SetExpectedFaultTargets(scenarioFaultTargets(faults, current.Status.Contracts.FinalizedHead.Number, true))
			}
			faultErr = advanceFaults(ctx, current.Status.Contracts.FinalizedHead, definition.Faults, faults, options.FaultDriver)
			if options.Adversaries != nil {
				options.Adversaries.SetExpectedFaultTargets(scenarioFaultTargets(faults, current.Status.Contracts.FinalizedHead.Number, false))
			}
			faultBytes, _ := json.MarshalIndent(struct {
				Schema string                `json:"schema"`
				Faults []ScenarioFaultRecord `json:"faults"`
			}{"urnetwork-sim-faults-v1", faults}, "", "  ")
			if err := atomicWrite(filepath.Join(runDir, "faults.json"), append(faultBytes, '\n'), 0o644); err != nil {
				return initialFailure(current, fmt.Errorf("persist scenario faults: %w", err))
			}
		}
		if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), current); err != nil {
			return initialFailure(current, fmt.Errorf("persist scenario observation: %w", err))
		}
		assertions = appendFaultAssertions(evaluateScenario(cfg, definition, start, current, started), faults, started, current)
		if faultErr != nil {
			break
		}
	}
	if terminalErr != nil {
		now := options.Now().UTC()
		assertions = append(assertions, AssertionRecord{ID: "scenario_context", Passed: false, Message: terminalErr.Error(), StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
		sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	}
	completed := options.Now().UTC()
	adversaryEvidence, adversaryRecords, adversaryErr := finalizeAdversaryEvidence(options.Adversaries, runDir, started, current.ObservationHash, completed)
	adversariesFinalized = true
	assertions = append(assertions, runtimeAssertions...)
	assertions = append(assertions, adversaryRecords...)
	if adversaryErr != nil && terminalErr == nil {
		terminalErr = adversaryErr
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	completed = options.Now().UTC()
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: runID,
		DeploymentID: cfg.Config.Deployment.DeploymentID, Name: definition.Name, ScenarioDefinition: definitionHash, ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano),
		StartHead: start.Status.Contracts.FinalizedHead, EndHead: current.Status.Contracts.FinalizedHead,
		StartEpoch: start.Status.Contracts.CurrentEpoch, EndEpoch: current.Status.Contracts.CurrentEpoch,
		Assertions: assertions, Faults: faults, Adversaries: adversaryEvidence,
		ValueReconciliation: map[string]string{"captured_rao": current.Status.Contracts.TotalCaptured, "paid_rao": current.Status.Contracts.TotalPaid, "outstanding_liability_rao": current.Status.Contracts.Outstanding, "reserve_principal_rao": current.Status.Contracts.ReservePrincipal, "reserve_live_stake_rao": current.Status.Contracts.ReserveLiveStake},
		Result:              "pass",
	}
	attachScenarioAnomalyGate(result, completed, start, current, observationHistory...)
	result.EvidenceHash, _ = canonicalScenarioResultHash(result)
	if err := writeScenarioOutputs(cfg, runDir, result, current); err != nil {
		return nil, err
	}
	if options.Publish {
		publishResult := *result
		publishResult.PublishedEvidence = nil
		bundle := ScenarioEvidenceBundle{Schema: "urnetwork-sim-scenario-evidence-v1", Result: &publishResult, Observation: current, Analysis: analyzeScenarioObservation(cfg, current)}
		published, publishErr := publishEvidence(ctx, cfg, options.Roles, stateDir, "scenario-bundle", runID, bundle)
		if publishErr == nil {
			publishErr = verifyPublishedEvidenceOrigins(ctx, cfg, options.Roles, published)
		}
		if publishErr != nil {
			result.Assertions = append(result.Assertions, AssertionRecord{ID: "evidence_publication", Passed: false, Message: publishErr.Error(), StartedAt: result.StartedAt, CompletedAt: options.Now().UTC().Format(time.RFC3339Nano), ObservationHash: current.ObservationHash})
		} else {
			result.PublishedEvidence = published
		}
		attachScenarioAnomalyGate(result, options.Now().UTC(), start, current, observationHistory...)
		result.EvidenceHash, _ = canonicalScenarioResultHash(result)
		if err := writeScenarioOutputs(cfg, runDir, result, current); err != nil {
			return nil, err
		}
	}
	finalEvidenceFailure := func(id string, failure error) (*ScenarioResult, error) {
		now := options.Now().UTC()
		result.Assertions = append(result.Assertions, AssertionRecord{
			ID: id, Passed: false, Message: failure.Error(), StartedAt: result.StartedAt,
			CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(),
			ObservationHash: current.ObservationHash,
		})
		attachScenarioAnomalyGate(result, now, start, current, observationHistory...)
		result.EvidenceHash, _ = canonicalScenarioResultHash(result)
		return result, errors.Join(failure, writeScenarioOutputs(cfg, runDir, result, current))
	}
	if result.Result == "pass" {
		if err := scanEvidenceSecrets(stateDir, runDir, options.Roles, cfg.WalletSecret, cfg.WalletMaterial, cfg.WalletPasswordSecret, cfg.WalletPassword); err != nil {
			return finalEvidenceFailure("evidence_secret_scan", err)
		}
		hashes, err := evidenceFileHashes(runDir)
		if err != nil {
			return finalEvidenceFailure("evidence_file_hashes", err)
		}
		if options.Roles == nil {
			complete := map[string]any{"schema": "urnetwork-sim-complete-v1", "run_id": runID, "result_hash": result.EvidenceHash, "files": hashes}
			b, marshalErr := json.MarshalIndent(complete, "", "  ")
			if marshalErr != nil {
				return finalEvidenceFailure("complete_evidence_encoding", marshalErr)
			}
			if err := atomicWrite(filepath.Join(runDir, "complete.json"), append(b, '\n'), 0o644); err != nil {
				return finalEvidenceFailure("complete_evidence_write", err)
			}
		} else {
			owner, ok := options.Roles.EVM["testnet-owner"]
			if !ok {
				return finalEvidenceFailure("complete_evidence_signer", errors.New("testnet owner role is missing"))
			}
			complete, err := signEvidence(cfg, "scenario-complete", runID, map[string]any{"result_hash": result.EvidenceHash, "files": hashes}, owner)
			if err != nil {
				return finalEvidenceFailure("complete_evidence_signature", err)
			}
			b, marshalErr := json.MarshalIndent(complete, "", "  ")
			if marshalErr != nil {
				return finalEvidenceFailure("complete_evidence_encoding", marshalErr)
			}
			if err := atomicWrite(filepath.Join(runDir, "complete.json"), append(b, '\n'), 0o644); err != nil {
				return finalEvidenceFailure("complete_evidence_write", err)
			}
		}
	}
	if result.Result != "pass" {
		return result, fmt.Errorf("scenario %s failed; evidence %s", definition.Name, filepath.Join(runDir, "result.json"))
	}
	return result, nil
}

func canonicalScenarioResultHash(result *ScenarioResult) (string, error) {
	copy := *result
	copy.EvidenceHash = ""
	copy.PublishedEvidence = nil
	return canonicalHashHex(copy)
}

func writeScenarioOutputs(cfg *ResolvedConfig, runDir string, result *ScenarioResult, observation *ScenarioObservation) error {
	if result.Anomalies == nil {
		return errors.New("scenario result has no anomaly ledger")
	}
	anomalies, err := json.MarshalIndent(result.Anomalies, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(runDir, "anomalies.json"), append(anomalies, '\n'), 0o644); err != nil {
		return err
	}
	assertions, err := json.MarshalIndent(assertionFile{Schema: "urnetwork-sim-assertions-v1", Assertions: result.Assertions}, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(runDir, "assertions.json"), append(assertions, '\n'), 0o644); err != nil {
		return err
	}
	analysis := analyzeScenarioObservation(cfg, observation)
	analysisBytes, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(runDir, "analysis.json"), append(analysisBytes, '\n'), 0o644); err != nil {
		return err
	}
	if err := writeAnalysisHTML(filepath.Join(runDir, "analysis.html"), analysis); err != nil {
		return err
	}
	if err := writeJUnit(filepath.Join(runDir, "junit.xml"), result.Name, result.Assertions); err != nil {
		return err
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(runDir, "result.json"), append(b, '\n'), 0o644)
}

type verifyKeyRotationOperator struct {
	NoID         int    `json:"no_id"`
	OldPublicKey string `json:"old_public_key"`
	NewPublicKey string `json:"new_public_key"`
	BeforePID    int    `json:"before_pid,omitempty"`
	AfterPID     int    `json:"after_pid"`
}

type verifyKeyRotationEvidence struct {
	Schema       string                      `json:"schema"`
	DeploymentID string                      `json:"deployment_id"`
	PolicyHash   string                      `json:"policy_hash"`
	Operators    []verifyKeyRotationOperator `json:"operators"`
}

func fetchVerifyPublicKeys(ctx context.Context, endpoint string) (map[byte]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/")+"/verify/keys", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, errors.New("verify keys endpoint exceeded 1 MiB")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("verify keys endpoint returned HTTP %d", resp.StatusCode)
	}
	var value struct {
		Keys []struct {
			ServerKeyID byte   `json:"server_key_id"`
			PublicKey   []byte `json:"public_key"`
		} `json:"keys"`
	}
	if json.Unmarshal(b, &value) != nil || len(value.Keys) == 0 {
		return nil, errors.New("verify keys endpoint returned an invalid response")
	}
	result := map[byte]string{}
	for _, key := range value.Keys {
		if len(key.PublicKey) != ed25519.PublicKeySize || result[key.ServerKeyID] != "" {
			return nil, errors.New("verify keys endpoint returned an invalid/duplicate key")
		}
		result[key.ServerKeyID] = "0x" + hex.EncodeToString(key.PublicKey)
	}
	return result, nil
}

func rotateOperatorVerifyKeys(ctx context.Context, cfg *ResolvedConfig, stateDir string) error {
	driver := &liveScenarioFaultDriver{stateDir: stateDir, cfg: cfg}
	record := verifyKeyRotationEvidence{Schema: "urnetwork-verify-key-rotation-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, PolicyHash: cfg.PolicyHash}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		configBytes, expected, err := operatorVerifyConfig(cfg, operator, true)
		if err != nil {
			return err
		}
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", "verify.yml")
		if err := atomicWrite(path, configBytes, 0o600); err != nil {
			return err
		}
		endpoint := fmt.Sprintf("http://127.0.0.1:%d", 18080+operator)
		observed, readErr := fetchVerifyPublicKeys(ctx, endpoint)
		states, _, stateErr := driver.processSnapshot()
		if stateErr != nil {
			return stateErr
		}
		processID := fmt.Sprintf("operator-%d-api", operator)
		beforePID := states[processID].PID
		if readErr != nil || !strings.EqualFold(observed[0], expected[0]) || !strings.EqualFold(observed[1], expected[1]) {
			fault := scenarioFaultSpec{ID: fmt.Sprintf("verify-key-rotation-%d", operator), Kind: "process-restart", Targets: []string{processID}, TriggerOffsetBlocks: 1, DurationBlocks: 1}
			before, err := driver.Apply(ctx, fault)
			if err != nil {
				return err
			}
			after, err := driver.Restore(ctx, fault)
			if err != nil {
				return err
			}
			if len(before) != 1 || len(after) != 1 || before[0].PID == after[0].PID {
				return fmt.Errorf("operator %d verify-key restart did not replace the API process", operator)
			}
			beforePID = before[0].PID
		}
		observed, err = fetchVerifyPublicKeys(ctx, endpoint)
		if err != nil || !strings.EqualFold(observed[0], expected[0]) || !strings.EqualFold(observed[1], expected[1]) {
			if err == nil {
				err = errors.New("published key set does not match the deterministic rotation")
			}
			return fmt.Errorf("operator %d did not publish both verify keys after rotation: %w", operator, err)
		}
		states, _, err = driver.processSnapshot()
		if err != nil || states[processID].PID <= 1 {
			if err == nil {
				err = errors.New("API process is not live")
			}
			return fmt.Errorf("operator %d API state unavailable after key rotation: %w", operator, err)
		}
		record.Operators = append(record.Operators, verifyKeyRotationOperator{NoID: operator, OldPublicKey: expected[0], NewPublicKey: expected[1], BeforePID: beforePID, AfterPID: states[processID].PID})
	}
	return writePublicJSON(filepath.Join(stateDir, "public", "verify-key-rotation.json"), record)
}

func RunScenario(ctx context.Context, cfg *ResolvedConfig, stateDir, name string, _ *Journal, executor *Executor) error {
	// Validate the immutable definition before any live action. A malformed
	// release matrix must not be able to leave the coordinator paused/upgraded.
	definition, err := scenarioDefinitionFor(cfg, name)
	if err != nil {
		return err
	}
	roles, err := LoadOrWriteRoleSecrets(cfg, stateDir)
	if err != nil {
		return err
	}
	var campaign adversaryCampaign
	if definition.AdversarialMatrixHash != "" {
		matrix, matrixErr := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
		if matrixErr != nil {
			return matrixErr
		}
		if matrix.Hash != definition.AdversarialMatrixHash {
			return errors.New("adversarial matrix changed after scenario definition validation")
		}
		actors, actorErr := newLiveAdversaryActors(cfg, stateDir, roles)
		if actorErr != nil {
			return actorErr
		}
		liveCampaign, campaignErr := newAdversaryCampaign(cfg.Config.Scenarios.Adversaries, matrix, actors)
		if campaignErr != nil {
			return campaignErr
		}
		campaign = liveCampaign
	}
	prepare := func(prepareCtx context.Context) error {
		if name == "precompile-conformance" {
			if executor == nil {
				return errors.New("precompile conformance requires the approved deployment executor")
			}
			count := 0
			for _, action := range executor.plan.Actions {
				if strings.HasPrefix(action.ID, "precompile.") {
					if err := executor.Execute(prepareCtx, action); err != nil {
						return err
					}
					count++
				}
			}
			if count != 10 {
				return fmt.Errorf("approved plan has %d precompile actions, want exactly 10", count)
			}
		}
		if name == "release-1.0" {
			if executor == nil {
				return errors.New("release scenario requires the approved deployment executor")
			}
			precompile, readErr := loadPrecompileEvidence(stateDir)
			if readErr != nil {
				return fmt.Errorf("release scenario requires a completed precompile-conformance gate: %w", readErr)
			}
			if executor.payloads == nil {
				return errors.New("release scenario requires installed deployment payloads")
			}
			if identityErr := validatePrecompileEvidenceIdentity(cfg, &executor.payloads.Manifest, precompile); identityErr != nil {
				return fmt.Errorf("release scenario precompile evidence identity: %w", identityErr)
			}
			if !precompileEvidenceComplete(precompile) {
				return errors.New("release scenario requires a completed precompile-conformance gate")
			}
			waitBlocks := cfg.Policy.Settlement.EpochBlocks + cfg.Policy.Settlement.FinalizeOffsetBlocks + 20
			if err := waitForGovernanceDrillReady(prepareCtx, executor, time.Duration(waitBlocks*cfg.Public.Chain.ExpectedBlockSeconds)*time.Second); err != nil {
				return err
			}
			for _, action := range executor.plan.Actions {
				if strings.HasPrefix(action.ID, "governance.") {
					if err := executor.Execute(prepareCtx, action); err != nil {
						return err
					}
				}
			}
		}
		if name == "production-soak" {
			if executor == nil {
				return errors.New("production soak requires the approved deployment executor")
			}
			for _, action := range executor.plan.Actions {
				if action.ID == "production.schedule-policy" || action.ID == "production.hyperparameter.immunity_period" {
					if err := executor.Execute(prepareCtx, action); err != nil {
						return err
					}
				}
			}
			if err := rotateOperatorVerifyKeys(prepareCtx, cfg, stateDir); err != nil {
				return fmt.Errorf("rotate operator verify keys: %w", err)
			}
		}
		return nil
	}
	probe := &liveScenarioProbe{cfg: cfg, stateDir: stateDir, client: &http.Client{Timeout: 30 * time.Second}}
	_, err = runScenarioWithProbe(ctx, cfg, stateDir, definition, probe, scenarioRunOptions{Roles: roles, Publish: true, FaultDriver: &liveScenarioFaultDriver{stateDir: stateDir, cfg: cfg}, Adversaries: campaign, Prepare: prepare})
	return err
}
