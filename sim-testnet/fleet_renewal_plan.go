package main

// Renewal appends a new, explicitly approved generation to the existing plan.
// It cannot register, fund, replace identities, or overwrite prior evidence.
import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

type FleetRenewal struct {
	Round                    uint64              `json:"round"`
	BatchSize                uint64              `json:"batch_size"`
	MaximumInFlight          uint64              `json:"maximum_in_flight"`
	SourcePlanHash           string              `json:"source_plan_hash"`
	JournalHash              string              `json:"journal_hash"`
	NativeHead               ChainHead           `json:"native_head"`
	EVMHead                  ChainHead           `json:"evm_head"`
	ObservedEpoch            uint64              `json:"observed_epoch"`
	ValidFromEpoch           uint64              `json:"valid_from_epoch"`
	ValidToEpoch             uint64              `json:"valid_to_epoch"`
	MaximumFeePerGasWei      uint64              `json:"maximum_fee_per_gas_wei"`
	Oracle                   common.Address      `json:"oracle"`
	Keeper                   common.Address      `json:"keeper"`
	OracleNonce              uint64              `json:"oracle_nonce"`
	KeeperNonce              uint64              `json:"keeper_nonce"`
	CampaignLiabilityWei     DecimalUint         `json:"campaign_committed_or_pending_max_wei"`
	SupersededGasCoveredWei  DecimalUint         `json:"gas_already_covered_by_superseded_allowance_wei"`
	CampaignReserveBeforeWei DecimalUint         `json:"campaign_reserve_before_wei"`
	TransactionEvidence      []string            `json:"external_signed_transactions"`
	EVMNonces                []FleetRenewalNonce `json:"evm_nonce_checkpoints"`
	Fleets                   []FleetRenewalFleet `json:"fleets"`
}

type FleetRenewalFleet struct {
	Fleet       int                  `json:"fleet"`
	HotkeyRole  string               `json:"hotkey_role"`
	Coldkey     string               `json:"coldkey"`
	UID         uint16               `json:"uid"`
	NativeNonce uint32               `json:"native_nonce"`
	Manifest    json.RawMessage      `json:"manifest"`
	Members     []FleetRenewalMember `json:"members"`
}

type FleetRenewalMember struct {
	Miner           int                  `json:"miner"`
	VersionCount    uint64               `json:"prior_version_count"`
	Prior           FleetBindingEvidence `json:"prior_binding"`
	Binding         FleetBindingEvidence `json:"binding"`
	RevokeSignature string               `json:"revoke_signature,omitempty"`
}

type fleetRenewalBudgetError struct {
	Approvable   bool         `json:"approvable"`
	Renewal      FleetRenewal `json:"renewal"`
	Maximum      Spend        `json:"maximum_spend"`
	AvailableWei DecimalUint  `json:"available_campaign_wei"`
	ShortfallWei DecimalUint  `json:"shortfall_wei"`
	Actions      []Action     `json:"actions"`
}

func (e *fleetRenewalBudgetError) Error() string {
	return fmt.Sprintf("renewal requires %s wei; unused campaign allowance is %s wei; shortfall is %s wei", e.Maximum.EVMGasWei, e.AvailableWei, e.ShortfallWei)
}

func isFleetRenewalAction(a Action) bool { return strings.HasPrefix(a.ID, "fleet.renew.") }
func fleetRenewalActionID(round uint64, fleet int, operation string, member int) string {
	id := fmt.Sprintf("fleet.renew.%d.%d.%s", round, fleet, operation)
	if member != 0 {
		id += fmt.Sprintf(".%d", member)
	}
	return id
}
func fleetRenewalStem(round uint64, fleet int) string {
	return fmt.Sprintf("fleet-%d.renewal-%d", fleet, round)
}

func fleetRenewalBinding(manifest protocol.FleetManifest, member protocol.FleetMember, evidence FleetBindingEvidence) (protocol.FleetBinding, error) {
	binding, err := manifest.Binding(member, evidence.ValidFromEpoch, evidence.ValidToEpoch)
	if err != nil {
		return binding, err
	}
	digest, err := binding.Digest()
	if err != nil {
		return binding, err
	}
	fields := []struct {
		got  string
		want []byte
	}{
		{evidence.ClientID, binding.ClientID[:]}, {evidence.ClientKey, binding.ClientKey[:]},
		{evidence.FleetID, binding.FleetID[:]}, {evidence.Hotkey, binding.Hotkey[:]},
		{evidence.CommitmentHash, binding.CommitmentHash[:]}, {evidence.BindingDigest, digest[:]},
	}
	for _, field := range fields {
		raw, ok := evidenceFixedHex(field.got, len(field.want))
		if !ok || !bytes.Equal(raw, field.want) {
			return binding, errors.New("renewal binding identity differs from its manifest")
		}
	}
	client, ok := evidenceFixedHex(evidence.ClientSignature, 64)
	hotkey, hotOK := evidenceFixedHex(evidence.HotkeySignature, 64)
	if evidence.Schema != "urnetwork-fleet-binding-evidence-v1" || evidence.Generation != manifest.Generation || !ok || !hotOK || !binding.VerifyClient(client) || !binding.VerifyHotkey(hotkey) {
		return binding, errors.New("renewal binding signatures or generation are invalid")
	}
	return binding, nil
}

func fleetRenewalActions(p *SetupPlan, renewal FleetRenewal) ([]Action, error) {
	if renewal.BatchSize != fleetRenewalBatchSize || renewal.MaximumInFlight != fleetRenewalMaximumInFlight {
		return nil, errors.New("renewal execution bounds differ from the supported pipeline")
	}
	if p == nil || renewal.Round == 0 || renewal.ValidFromEpoch <= renewal.ObservedEpoch || renewal.ValidToEpoch < renewal.ValidFromEpoch || renewal.MaximumFeePerGasWei == 0 || renewal.MaximumFeePerGasWei > p.MaximumEVMFeePerGasWei || renewal.Oracle == (common.Address{}) || renewal.Keeper == (common.Address{}) || renewal.Oracle == renewal.Keeper || len(renewal.Fleets) == 0 {
		return nil, errors.New("fleet renewal has invalid epochs, fee ceiling, signers, or fleet count")
	}
	if renewal.NativeHead.Number == 0 || renewal.EVMHead.Number == 0 || !validCanonicalHashHex(renewal.NativeHead.Hash) || !validCanonicalHashHex(renewal.EVMHead.Hash) || !validCanonicalHashHex(renewal.SourcePlanHash) || !validCanonicalHashHex(renewal.JournalHash) {
		return nil, errors.New("fleet renewal has no authenticated source checkpoint")
	}
	deploymentHash, err := contractDeploymentIdentityHash(p.Deployment)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	var actions []Action
	oracleNonce, keeperNonce := renewal.OracleNonce, renewal.KeeperNonce
	barrier := ""
	seenClients, seenHotkeys, seenUIDs := map[[16]byte]bool{}, map[[32]byte]bool{}, map[uint16]bool{}
	for index, fleet := range renewal.Fleets {
		parsed, err := protocol.ParseFleetManifest(fleet.Manifest)
		if err != nil {
			return nil, err
		}
		manifest := *parsed
		if fleet.Fleet != index+1 || manifest.ChainID != p.ChainID || manifest.Netuid != p.Netuid || common.BytesToAddress(manifest.Coordinator[:]) != p.Deployment.CoordinatorProxy || manifest.Generation < 2 || len(manifest.Members) != len(fleet.Members) || len(fleet.Members) == 0 || seenHotkeys[manifest.Hotkey] || seenUIDs[fleet.UID] || fleet.HotkeyRole == "" || !validCanonicalHashHex(fleet.Coldkey) {
			return nil, fmt.Errorf("renewal fleet %d has invalid identity, members, or UID", fleet.Fleet)
		}
		seenHotkeys[manifest.Hotkey], seenUIDs[fleet.UID] = true, true
		commitment, err := manifest.CommitmentHash()
		if err != nil {
			return nil, err
		}
		add := func(operation string, member int, kind string, signer common.Address, nonce uint64, gas uint64, data []byte, dependencies []string) error {
			parameters := map[string]string{deploymentManifestHashParameter: deploymentHash, "round": strconv.FormatUint(renewal.Round, 10), "fleet": strconv.Itoa(fleet.Fleet), "operation": operation, "member": strconv.Itoa(member), "generation": strconv.FormatUint(manifest.Generation, 10), "hotkey_role": fleet.HotkeyRole, "hotkey": fleetLifecycleHex(manifest.Hotkey), "expected_uid": strconv.Itoa(int(fleet.UID)), "renewal_expected_nonce": strconv.FormatUint(nonce, 10), "valid_from_epoch": strconv.FormatUint(renewal.ValidFromEpoch, 10), "valid_to_epoch": strconv.FormatUint(renewal.ValidToEpoch, 10), "commitment_hash": fleetLifecycleHex(commitment)}
			action := Action{ID: fleetRenewalActionID(renewal.Round, fleet.Fleet, operation, member), Kind: kind, Target: p.Deployment.CoordinatorProxy.Hex(), Description: "renew the existing fleet under an exact identity, nonce, and validity window", Parameters: parameters, DependsOn: dependencies}
			if kind == "evm-transaction" {
				parameters[evmMaximumGasUnitsParameter], parameters[evmMaximumFeePerGasParameter] = strconv.FormatUint(gas, 10), strconv.FormatUint(renewal.MaximumFeePerGasWei, 10)
				parameters["renewal_expected_signer"] = signer.Hex()
				if len(data) != 0 {
					parameters["renewal_calldata"] = "0x" + hex.EncodeToString(data)
				}
				action.Spend.EVMGasWei = DecimalUint(new(big.Int).Mul(new(big.Int).SetUint64(gas), new(big.Int).SetUint64(renewal.MaximumFeePerGasWei)).String())
			} else {
				parameters[fleetCommitmentStorageParameter] = fleetCommitmentStorageV2
				action.Target = fleet.HotkeyRole
				action.Spend.TAORao = p.NativeTransactionFeeLimitRao
			}
			action.IntentHash, err = actionIntentHash(action)
			if err != nil {
				return err
			}
			actions = append(actions, action)
			return nil
		}
		deps := []string{}
		if barrier != "" {
			deps = append(deps, barrier)
		}
		if err := add("commitment", 0, "substrate-extrinsic", common.Address{}, uint64(fleet.NativeNonce), 0, nil, deps); err != nil {
			return nil, err
		}
		commitID := actions[len(actions)-1].ID
		if oracleNonce == ^uint64(0) {
			return nil, errors.New("renewal oracle nonce overflows")
		}
		if err := add("mirror", 0, "evm-transaction", renewal.Oracle, oracleNonce, 200_000, nil, []string{commitID}); err != nil {
			return nil, err
		}
		oracleNonce++
		barrier = actions[len(actions)-1].ID
		for memberIndex, member := range fleet.Members {
			manifestMember := manifest.Members[memberIndex]
			if seenClients[manifestMember.ClientID] || member.VersionCount == 0 || member.VersionCount == ^uint64(0) || member.Prior.Generation == ^uint64(0) || member.Prior.Generation+1 != manifest.Generation || member.Prior.UID != fleet.UID || member.Binding.UID != fleet.UID || member.Binding.ValidFromEpoch != renewal.ValidFromEpoch || member.Binding.ValidToEpoch != renewal.ValidToEpoch {
				return nil, errors.New("renewal member changes identity or skips its exact next generation")
			}
			seenClients[manifestMember.ClientID] = true
			priorManifest := manifest
			priorManifest.Generation = member.Prior.Generation
			if _, err := fleetRenewalBinding(priorManifest, manifestMember, member.Prior); err != nil {
				return nil, fmt.Errorf("renewal predecessor: %w", err)
			}
			binding, err := fleetRenewalBinding(manifest, manifestMember, member.Binding)
			if err != nil {
				return nil, err
			}
			if member.Prior.ValidToEpoch >= renewal.ValidFromEpoch {
				revoke := protocol.FleetRevoke{ChainID: manifest.ChainID, Netuid: manifest.Netuid, Coordinator: manifest.Coordinator, ClientID: manifestMember.ClientID, Generation: member.Prior.Generation, EffectiveEpoch: renewal.ValidFromEpoch}
				sig, ok := evidenceFixedHex(member.RevokeSignature, 64)
				if !ok || !revoke.VerifyClient(manifestMember.ClientKey[:], sig) {
					return nil, errors.New("renewal overlap lacks exact client revoke consent")
				}
				data, err := coordinator.TryPackRevokeFleetBinding(manifestMember.ClientID, member.Prior.Generation, renewal.ValidFromEpoch, sig)
				if err != nil {
					return nil, err
				}
				if keeperNonce == ^uint64(0) {
					return nil, errors.New("renewal keeper nonce overflows")
				}
				if err := add("revoke", memberIndex+1, "evm-transaction", renewal.Keeper, keeperNonce, 200_000, data, []string{barrier}); err != nil {
					return nil, err
				}
				keeperNonce++
				barrier = actions[len(actions)-1].ID
			} else if member.RevokeSignature != "" {
				return nil, errors.New("expired renewal predecessor cannot authorize an unnecessary revoke")
			}
			contractBinding := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: p.Deployment.CoordinatorProxy, FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
			clientSig, _ := evidenceFixedHex(member.Binding.ClientSignature, 64)
			hotSig, _ := evidenceFixedHex(member.Binding.HotkeySignature, 64)
			data, err := coordinator.TryPackBindFleetMember(contractBinding, clientSig, hotSig)
			if err != nil {
				return nil, err
			}
			if keeperNonce == ^uint64(0) {
				return nil, errors.New("renewal keeper nonce overflows")
			}
			if err := add("bind", memberIndex+1, "evm-transaction", renewal.Keeper, keeperNonce, 400_000, data, []string{barrier}); err != nil {
				return nil, err
			}
			keeperNonce++
			barrier = actions[len(actions)-1].ID
		}
	}
	return orderFleetRenewalActions(actions, renewal)
}

func validateFleetRenewalPlan(p *SetupPlan) error {
	generated := map[string]Action{}
	for index, renewal := range p.FleetRenewals {
		if renewal.Round != uint64(index+1) || !slices.Contains(p.PriorPlanHashes, renewal.SourcePlanHash) {
			return errors.New("fleet renewal round has no immutable predecessor plan")
		}
		actions, err := fleetRenewalActions(p, renewal)
		if err != nil {
			return err
		}
		for _, a := range actions {
			generated[a.ID] = a
		}
	}
	for _, action := range p.Actions {
		if !isFleetRenewalAction(action) {
			continue
		}
		want, ok := generated[action.ID]
		gotHash, _ := canonicalHashHex(action)
		wantHash, _ := canonicalHashHex(want)
		if !ok || gotHash != wantHash {
			return fmt.Errorf("renewal action %s differs from its approved generation", action.ID)
		}
		delete(generated, action.ID)
	}
	if len(generated) != 0 {
		return errors.New("fleet renewal is missing executable actions")
	}
	return validateFleetLifecycleRenewalPlan(p)
}

func appendFleetRenewalPlan(base *SetupPlan, renewal FleetRenewal) (*SetupPlan, error) {
	raw, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	var plan SetupPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, err
	}
	if renewal.SourcePlanHash != base.PlanHash || renewal.Round != uint64(len(base.FleetRenewals)+1) {
		return nil, errors.New("renewal does not extend the current plan")
	}
	actions, err := fleetRenewalActions(base, renewal)
	if err != nil {
		return nil, err
	}
	spend, err := maximumActionSpend(actions)
	if err != nil {
		return nil, err
	}
	found := false
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != "campaign.evm-gas-reserve" {
			continue
		}
		if action.Kind != "budget-reserve" || action.Spend.EVMGasWei != renewal.CampaignReserveBeforeWei {
			return nil, errors.New("renewal campaign reserve differs from source approval")
		}
		available, availableErr := subtractDecimalUint(action.Spend.EVMGasWei, renewal.CampaignLiabilityWei)
		if availableErr != nil {
			available = "0"
		}
		comparison, comparisonErr := spend.EVMGasWei.Cmp(available)
		if comparisonErr != nil {
			return nil, comparisonErr
		}
		if comparison > 0 {
			shortfall, err := subtractDecimalUint(spend.EVMGasWei, available)
			if err != nil {
				return nil, err
			}
			return nil, &fleetRenewalBudgetError{Renewal: renewal, Maximum: spend, AvailableWei: available, ShortfallWei: shortfall, Actions: actions}
		}
		remaining, err := subtractDecimalUint(action.Spend.EVMGasWei, spend.EVMGasWei)
		if err != nil {
			return nil, fmt.Errorf("renewal maximum gas %s exceeds campaign reserve %s: %w", spend.EVMGasWei, action.Spend.EVMGasWei, err)
		}
		if comparison, err := remaining.Cmp(renewal.CampaignLiabilityWei); err != nil || comparison < 0 {
			return nil, fmt.Errorf("renewal gas %s leaves %s for campaign liabilities %s", spend.EVMGasWei, remaining, renewal.CampaignLiabilityWei)
		}
		action.Spend.EVMGasWei = remaining
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return nil, err
		}
		found = true
	}
	if !found {
		return nil, errors.New("renewal has no retained campaign reserve")
	}
	plan.PriorPlanHashes = append(plan.PriorPlanHashes, base.PlanHash)
	plan.FleetRenewals = append(plan.FleetRenewals, renewal)
	plan.Actions = append(plan.Actions, actions...)
	if err := rebindFleetLifecycleRenewalPlan(&plan, &renewal); err != nil {
		return nil, err
	}
	plan.MaximumSpend, err = maximumActionSpend(plan.Actions)
	if err != nil {
		return nil, err
	}
	plan.PlanHash = ""
	plan.PlanHash, err = plan.hash()
	if err != nil {
		return nil, err
	}
	if err := validatePlanBudget(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func validateFleetRenewalEVMFields(action Action, signer common.Address, nonce uint64, to *common.Address, value *big.Int, data []byte) error {
	if !isFleetRenewalAction(action) {
		return nil
	}
	wantNonce, err := strconv.ParseUint(action.Parameters["renewal_expected_nonce"], 10, 64)
	if err != nil || nonce != wantNonce || !common.IsHexAddress(action.Parameters["renewal_expected_signer"]) || signer != common.HexToAddress(action.Parameters["renewal_expected_signer"]) || to == nil || !common.IsHexAddress(action.Target) || *to != common.HexToAddress(action.Target) || value == nil || value.Sign() != 0 {
		return errors.New("renewal EVM transaction changed signer, nonce, target, or zero value")
	}
	if encoded := action.Parameters["renewal_calldata"]; encoded != "" {
		want, err := hex.DecodeString(stringsTrim0x(encoded))
		if err != nil || !bytes.Equal(data, want) {
			return errors.New("renewal EVM transaction changed approved calldata")
		}
	} else if action.Parameters["operation"] != "mirror" {
		return errors.New("renewal EVM operation has no exact calldata")
	}
	return nil
}

func validateFleetRenewalSignedTransaction(action Action, tx *ethTypes.Transaction, chainID *big.Int) error {
	if !isFleetRenewalAction(action) {
		return nil
	}
	if tx == nil || chainID == nil || !tx.Protected() || tx.ChainId().Cmp(chainID) != 0 {
		return errors.New("renewal signed transaction belongs to a different or unprotected chain")
	}
	signer, err := ethTypes.Sender(ethTypes.LatestSignerForChainID(chainID), tx)
	if err != nil {
		return err
	}
	if err := validateFleetRenewalEVMFields(action, signer, tx.Nonce(), tx.To(), tx.Value(), tx.Data()); err != nil {
		return err
	}
	gas, fee, err := evmActionFeeEnvelope(action)
	if err != nil {
		return err
	}
	if tx.Gas() > gas || !tx.GasFeeCap().IsUint64() || tx.GasFeeCap().Uint64() > fee {
		return errors.New("renewal signed transaction exceeds its approved fee envelope")
	}
	return nil
}

// Gives externally reviewed plans a compact stable identity for wire fields.
func fleetRenewalCalldataHash(data []byte) string { return crypto.Keccak256Hash(data).Hex() }
