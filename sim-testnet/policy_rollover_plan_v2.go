//go:build linux || darwin

package main

// Rollover approval is a separate immutable subplan. It never replaces the
// campaign plan, discards old evidence, or grants a fresh campaign allowance.
import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const policyRolloverPlanV2Schema = "urnetwork-sim-policy-rollover-plan-v2"
const policyRolloverHandoffV2Schema = "urnetwork-sim-policy-rollover-handoff-v2"
const policyRolloverActionPrefix = "evidence.policy-rollover."

type policyRolloverValidatorCheckpointV2 struct {
	ValidatorID         uint64                                   `json:"validator_id"`
	Config              validatorcomponent.ReleaseEvidenceV2File `json:"config"`
	StateDir            string                                   `json:"state_dir"`
	PreviousActivations []protocol.ValidatorEvidenceActivation   `json:"previous_activations"`
}

type policyRolloverPlanV2 struct {
	Schema                  string                                `json:"schema"`
	PlanHash                string                                `json:"plan_hash"`
	SourcePlanHash          string                                `json:"source_plan_hash"`
	SourceJournalHash       string                                `json:"source_journal_hash"`
	DeploymentID            string                                `json:"deployment_id"`
	StateDir                string                                `json:"state_dir"`
	ConfigHash              string                                `json:"config_hash"`
	PolicyHash              string                                `json:"policy_hash"`
	Generation              uint64                                `json:"generation"`
	LedgerContinuityClaimed bool                                  `json:"ledger_continuity_claimed"`
	Epoch                   uint64                                `json:"epoch"`
	Native                  ChainHead                             `json:"native"`
	EVM                     ChainHead                             `json:"evm"`
	Journal                 common.Address                        `json:"journal"`
	JournalRuntimeHash      common.Hash                           `json:"journal_runtime_hash"`
	Keeper                  common.Address                        `json:"keeper"`
	MaximumGasUnits         uint64                                `json:"maximum_gas_units"`
	MaximumFeePerGasWei     uint64                                `json:"maximum_fee_per_gas_wei"`
	MaximumAttempts         uint64                                `json:"maximum_attempts"`
	AttemptTimeoutSeconds   uint64                                `json:"attempt_timeout_seconds"`
	MaximumGasWei           DecimalUint                           `json:"maximum_gas_wei"`
	Validators              []policyRolloverValidatorCheckpointV2 `json:"validators"`
	Members                 []runtimeEvidenceActivationMemberV2   `json:"members"`
	Actions                 []Action                              `json:"actions"`
}

type policyRolloverValidatorHandoffV2 struct {
	ValidatorID             uint64                                     `json:"validator_id"`
	PreviousStateDir        string                                     `json:"previous_state_dir"`
	StateDir                string                                     `json:"state_dir"`
	ClientStateDir          string                                     `json:"client_state_dir"`
	Evidence                validatorcomponent.ReleaseEvidenceV2Config `json:"evidence"`
	Config                  validatorcomponent.ReleaseEvidenceV2File   `json:"config"`
	Identities              validatorcomponent.ReleaseEvidenceV2File   `json:"identities"`
	SourceRolePredecessorV2 *validatorcomponent.ReleaseEvidenceV2File  `json:"source_role_predecessor_v2,omitempty"`
}

// Activation switches only an immutable manifest after every publication and
// generation input is complete. Original ledgers and identities remain lineage;
// a fresh VPK starts an independent sequence and makes no continuity claim.
type policyRolloverHandoffV2 struct {
	Schema                  string                                    `json:"schema"`
	PlanHash                string                                    `json:"plan_hash"`
	SourcePlanHash          string                                    `json:"source_plan_hash"`
	DeploymentID            string                                    `json:"deployment_id"`
	Activated               bool                                      `json:"activated"`
	Generation              uint64                                    `json:"generation"`
	LedgerContinuityClaimed bool                                      `json:"ledger_continuity_claimed"`
	CutoffEpoch             uint64                                    `json:"cutoff_epoch"`
	FirstFullEpoch          uint64                                    `json:"first_full_epoch"`
	Native                  ChainHead                                 `json:"native"`
	EVM                     ChainHead                                 `json:"evm"`
	Boundary                ChainHead                                 `json:"boundary"`
	Members                 []runtimeEvidenceActivationMemberV2       `json:"members"`
	Validators              []policyRolloverValidatorHandoffV2        `json:"validators"`
	Identities              validatorcomponent.ReleaseEvidenceV2File  `json:"identities"`
	SourceRoleOverlay       *validatorcomponent.ReleaseEvidenceV2File `json:"source_role_overlay,omitempty"`
	sourceSHA256            string
}

func policyRolloverRoot(stateDir string) string { return filepath.Join(stateDir, "policy-rollover") }
func policyRolloverPlanRootV2(stateDir string, generation, epoch uint64) string {
	return filepath.Join(policyRolloverRoot(stateDir), "plans", fmt.Sprintf("generation-%020d-epoch-%020d", generation, epoch))
}
func policyRolloverPlanPathV2(stateDir string, generation, epoch uint64) string {
	return filepath.Join(policyRolloverPlanRootV2(stateDir, generation, epoch), "plan.json")
}
func policyRolloverActionID(validatorID, noID uint64) string {
	return fmt.Sprintf("%s%d.%d", policyRolloverActionPrefix, validatorID, noID)
}
func (p *policyRolloverPlanV2) hash() (string, error) {
	copy := *p
	copy.PlanHash = ""
	return canonicalHashHex(copy)
}

func policyRolloverActionsV2(p *policyRolloverPlanV2) ([]Action, error) {
	if p == nil || p.Keeper == (common.Address{}) || p.Journal == (common.Address{}) || p.MaximumGasUnits == 0 || p.MaximumFeePerGasWei == 0 || len(p.Members) != 4 {
		return nil, errors.New("rollover requires four members and an explicit keeper gas envelope")
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(p.MaximumGasUnits), new(big.Int).SetUint64(p.MaximumFeePerGasWei))
	actions := make([]Action, 0, 4)
	for _, member := range p.Members {
		data, err := stabi.PackValidatorEvidenceActivation(member.Activation, member.Activation, member.VpkSignature, member.HotkeySignature)
		if err != nil {
			return nil, err
		}
		a := Action{ID: policyRolloverActionID(member.ValidatorId, member.NoId), Kind: "evm-transaction", Target: p.Journal.Hex(), Description: "publish exact dual-consent V2 policy rollover through the existing keeper", Spend: Spend{EVMGasWei: DecimalUint(cost.String())}, Parameters: map[string]string{
			"rollover_keeper": p.Keeper.Hex(), "rollover_calldata_keccak256": crypto.Keccak256Hash(data).Hex(),
			"rollover_source_plan": p.SourcePlanHash, "rollover_epoch": strconv.FormatUint(p.Epoch, 10),
			evmMaximumGasUnitsParameter: strconv.FormatUint(p.MaximumGasUnits, 10), evmMaximumFeePerGasParameter: strconv.FormatUint(p.MaximumFeePerGasWei, 10),
		}}
		a.IntentHash, err = actionIntentHash(a)
		if err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	return actions, nil
}

// Nonces remain the existing keeper's durable ownership; third-party exact
// publication can satisfy any member without reserving a skipped nonce.
func validatePolicyRolloverEVMFields(a Action, signer common.Address, to *common.Address, value *big.Int, data []byte) error {
	if !strings.HasPrefix(a.ID, policyRolloverActionPrefix) {
		return nil
	}
	if a.Kind != "evm-transaction" || to == nil || !common.IsHexAddress(a.Target) || *to != common.HexToAddress(a.Target) ||
		!common.IsHexAddress(a.Parameters["rollover_keeper"]) || signer != common.HexToAddress(a.Parameters["rollover_keeper"]) ||
		value == nil || value.Sign() != 0 || crypto.Keccak256Hash(data).Hex() != a.Parameters["rollover_calldata_keccak256"] {
		return errors.New("rollover transaction differs from its approved keeper, journal, zero value or exact consent bytes")
	}
	return nil
}

func validatePolicyRolloverPlanV2(cfg *ResolvedConfig, base *SetupPlan, stateDir string, p *policyRolloverPlanV2) error {
	if cfg == nil || cfg.Config == nil || base == nil || base.ValidatorEvidence == nil || p == nil || cfg.ChainID != testnetChainID || cfg.Config.Topology.Validators != 2 || cfg.Config.Topology.Operators != 2 {
		return errors.New("rollover requires the existing two-validator, two-operator testnet deployment")
	}
	if p.Schema != policyRolloverPlanV2Schema || p.SourcePlanHash != base.PlanHash || p.ConfigHash != cfg.ConfigHash || p.PolicyHash != cfg.PolicyHash || p.DeploymentID != base.DeploymentID ||
		p.Generation == 0 || p.LedgerContinuityClaimed || p.StateDir != stateDir || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || !validCanonicalHashHex(p.SourceJournalHash) || p.Epoch == 0 || p.Epoch == ^uint64(0) ||
		p.Native.Number == 0 || p.EVM.Number == 0 || !validCanonicalHashHex(p.Native.Hash) || !validCanonicalHashHex(p.EVM.Hash) ||
		p.Journal != base.ValidatorEvidence.Address || p.JournalRuntimeHash != base.ValidatorEvidence.RuntimeCodeHash ||
		p.MaximumGasUnits != cfg.Config.ValidatorEvidenceActivationGasUnits || p.MaximumFeePerGasWei != base.MaximumEVMFeePerGasWei ||
		p.MaximumAttempts == 0 || p.MaximumAttempts > 8 || p.AttemptTimeoutSeconds == 0 || p.AttemptTimeoutSeconds > 300 || len(p.Validators) != 2 || len(p.Members) != 4 {
		return errors.New("rollover checkpoint, census, retry limits or source authority differs")
	}
	policy, err := decodeHex32("rollover policy", p.PolicyHash)
	if err != nil {
		return err
	}
	native, err := decodeHex32("rollover native hash", p.Native.Hash)
	if err != nil {
		return err
	}
	evm, err := decodeHex32("rollover EVM hash", p.EVM.Hash)
	if err != nil {
		return err
	}
	for index, checkpoint := range p.Validators {
		id := uint64(index + 1)
		if checkpoint.ValidatorID != id || checkpoint.Config.Path != filepath.Join(policyRolloverPlanRootV2(stateDir, p.Generation, p.Epoch), "source", fmt.Sprintf("validator-%d.yml", id)) || checkpoint.Config.Bytes == 0 ||
			!validCanonicalHashHex(checkpoint.Config.SHA256) || !filepath.IsAbs(checkpoint.StateDir) || filepath.Clean(checkpoint.StateDir) != checkpoint.StateDir || len(checkpoint.PreviousActivations) != 2 {
			return errors.New("rollover original config or complete checkpoint census differs")
		}
		for j, prior := range checkpoint.PreviousActivations {
			member := p.Members[index*2+j]
			wantDomain := protocol.ValidatorEvidenceActivationDomain{ChainID: testnetChainID, GenesisHash: [32]byte(base.ValidatorEvidence.GenesisHash), Netuid: cfg.Netuid,
				Coordinator: [20]byte(base.ValidatorEvidence.Coordinator), SettlementVault: [20]byte(base.ValidatorEvidence.SettlementVault), DeploymentIDHash: [32]byte(base.ValidatorEvidence.DeploymentIDHash), PolicyHash: policy, Epoch: p.Epoch}
			if err := prior.Validate(); err != nil {
				return err
			}
			priorDomain := prior.Domain
			priorDomain.PolicyHash, priorDomain.Epoch = policy, p.Epoch
			if priorDomain != wantDomain || prior.NoID != uint64(j+1) || member.ValidatorId != id || member.NoId != uint64(j+1) || member.Activation.VPK == prior.VPK || member.Activation.Hotkey != prior.Hotkey ||
				member.Activation.FirstSequence != 1 || member.Activation.PriorRoot != ([32]byte{}) || prior.Domain.PolicyHash == policy || prior.Domain.Epoch >= p.Epoch {
				return errors.New("rollover fresh generation changed lineage or claimed ledger continuity")
			}
			if member.Activation.Domain != wantDomain || member.Activation.NoID != member.NoId || member.Activation.NativeBlock != p.Native.Number || member.Activation.NativeHash != native || member.Activation.EVMBlock != p.EVM.Number || member.Activation.EVMHash != evm {
				return errors.New("rollover activation changed deployment or immutable snapshots")
			}
			if err := member.Activation.Verify(member.Activation, member.VpkSignature, member.HotkeySignature); err != nil {
				return err
			}
		}
	}

	actions, err := policyRolloverActionsV2(p)
	if err != nil || !reflect.DeepEqual(actions, p.Actions) {
		return errors.Join(errors.New("rollover actions differ from exact signed intents"), err)
	}
	maximum := new(big.Int).Mul(new(big.Int).SetUint64(p.MaximumGasUnits), new(big.Int).SetUint64(p.MaximumFeePerGasWei))
	maximum.Mul(maximum, big.NewInt(4))
	if p.MaximumGasWei != DecimalUint(maximum.String()) {
		return errors.New("rollover aggregate gas allowance differs from four fixed transactions")
	}
	hash, err := p.hash()
	if err != nil || hash != p.PlanHash {
		return errors.Join(errors.New("rollover plan hash differs"), err)
	}
	return nil
}

func policyRolloverFile(path string, data []byte) validatorcomponent.ReleaseEvidenceV2File {
	return validatorcomponent.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(data)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(data))}
}

func readPolicyRolloverPlanV2(ctx context.Context, cfg *ResolvedConfig, base *SetupPlan, stateDir, path string) (*policyRolloverPlanV2, error) {
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	var p policyRolloverPlanV2
	if _, err := readRuntimeEvidenceSetupV2(ctx, path, limit, &p); err != nil {
		return nil, err
	}
	if err := validatePolicyRolloverPlanV2(cfg, base, stateDir, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
