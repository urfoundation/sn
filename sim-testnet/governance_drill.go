package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/stabi"
)

const erc1967ImplementationSlot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"

type GovernanceEntitlementSnapshot struct {
	Epoch        uint64 `json:"epoch"`
	NoID         uint64 `json:"no_id"`
	PayoutRoot   string `json:"payout_root,omitempty"`
	ArtifactHash string `json:"artifact_hash,omitempty"`
	FundedRao    string `json:"funded_rao,omitempty"`
	TotalRao     string `json:"total_rao,omitempty"`
	ClaimedRao   string `json:"claimed_rao,omitempty"`
	ExpiryBlock  uint64 `json:"expiry_block,omitempty"`
	Status       uint8  `json:"status"`
}

type GovernanceCustodySnapshot struct {
	FinalizedHead       ChainHead                     `json:"finalized_head"`
	Implementation      string                        `json:"implementation"`
	Owner               string                        `json:"owner"`
	Guardian            string                        `json:"guardian"`
	Paused              bool                          `json:"paused"`
	PolicyHash          string                        `json:"policy_hash"`
	VaultCoordinator    string                        `json:"vault_coordinator"`
	ReserveRecorder     string                        `json:"reserve_recorder"`
	ReservePrincipalRao string                        `json:"reserve_principal_rao"`
	ReserveLiveStakeRao string                        `json:"reserve_live_stake_rao"`
	Entitlement         GovernanceEntitlementSnapshot `json:"entitlement"`
}

type GovernanceDrillEvidence struct {
	Schema       string                     `json:"schema"`
	DeploymentID string                     `json:"deployment_id"`
	PlanHash     string                     `json:"plan_hash"`
	Stage        string                     `json:"stage"`
	Before       GovernanceCustodySnapshot  `json:"before"`
	After        *GovernanceCustodySnapshot `json:"after,omitempty"`
	ProbeResults map[string]bool            `json:"probe_call_succeeded,omitempty"`
	Transactions map[string]string          `json:"transactions"`
}

func governanceStageRank(stage string) int {
	switch stage {
	case "paused":
		return 1
	case "adversary-active":
		return 2
	case "probed":
		return 3
	case "restored":
		return 4
	case "complete":
		return 5
	default:
		return 0
	}
}

func (e *Executor) governanceEvidencePath() string {
	return filepath.Join(e.stateDir, "public", "governance-drill.json")
}

func (e *Executor) loadGovernanceEvidence() (*GovernanceDrillEvidence, error) {
	var evidence GovernanceDrillEvidence
	if err := readJSONFile(e.governanceEvidencePath(), &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != "urnetwork-governance-drill-evidence-v1" || evidence.DeploymentID != e.cfg.Config.Deployment.DeploymentID || evidence.PlanHash != e.plan.PlanHash {
		return nil, errors.New("governance drill evidence identity mismatch")
	}
	return &evidence, nil
}

func (e *Executor) writeGovernanceEvidence(value *GovernanceDrillEvidence) error {
	if value.Transactions == nil {
		value.Transactions = map[string]string{}
	}
	return writePublicJSON(e.governanceEvidencePath(), value)
}

func implementationAt(ctx context.Context, manager *EVMTxManager, proxy common.Address, block uint64) (common.Address, error) {
	raw, err := manager.client.StorageAt(ctx, proxy, common.HexToHash(erc1967ImplementationSlot), new(big.Int).SetUint64(block))
	if err != nil {
		return common.Address{}, err
	}
	if len(raw) != 32 {
		return common.Address{}, fmt.Errorf("ERC1967 implementation slot returned %d bytes", len(raw))
	}
	return common.BytesToAddress(raw[12:]), nil
}

func (e *Executor) findFinalizedEntitlement(ctx context.Context) (uint64, uint64, stabi.STSettlementVaultEntitlement, error) {
	coordinator := stabi.NewSTCoordinator()
	vault := stabi.NewSTSettlementVault()
	address := e.payloads.Manifest.CoordinatorProxy
	current, err := rawCoordinatorCall(ctx, e.owner, address, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch)
	if err != nil || !current.IsUint64() {
		return 0, 0, stabi.STSettlementVaultEntitlement{}, stateMismatchError(err, "read current epoch is not uint64")
	}
	for epoch := current.Uint64(); ; epoch-- {
		for noID := 1; noID <= e.cfg.Config.Topology.Operators; noID++ {
			value, readErr := rawCoordinatorCall(ctx, e.owner, e.payloads.Manifest.SettlementVault, vault.PackEntitlement(new(big.Int).SetUint64(epoch), big.NewInt(int64(noID))), vault.UnpackEntitlement)
			if readErr != nil {
				return 0, 0, value, readErr
			}
			if value.Status == 2 && value.PayoutRoot != ([32]byte{}) && value.Total != nil {
				return epoch, uint64(noID), value, nil
			}
		}
		if epoch == 0 {
			break
		}
	}
	return 0, 0, stabi.STSettlementVaultEntitlement{}, errors.New("no finalized entitlement is available for the governance drill")
}

func entitlementSnapshot(epoch, noID uint64, value stabi.STSettlementVaultEntitlement) GovernanceEntitlementSnapshot {
	decimal := func(v *big.Int) string {
		if v == nil {
			return ""
		}
		return v.String()
	}
	return GovernanceEntitlementSnapshot{Epoch: epoch, NoID: noID, PayoutRoot: "0x" + hex.EncodeToString(value.PayoutRoot[:]), ArtifactHash: "0x" + hex.EncodeToString(value.ArtifactHash[:]), FundedRao: decimal(value.Funded), TotalRao: decimal(value.Total), ClaimedRao: decimal(value.Claimed), ExpiryBlock: value.ExpiryBlock, Status: value.Status}
}

func (e *Executor) governanceSnapshot(ctx context.Context, epoch, noID uint64) (GovernanceCustodySnapshot, error) {
	head, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	coordinator, vault, reserve := stabi.NewSTCoordinator(), stabi.NewSTSettlementVault(), stabi.NewSTReserveSink()
	proxy := e.payloads.Manifest.CoordinatorProxy
	implementation, err := implementationAt(ctx, e.owner, proxy, head.Number)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	owner, err := rawCoordinatorCall(ctx, e.owner, proxy, coordinator.PackOwner(), coordinator.UnpackOwner)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	guardian, err := rawCoordinatorCall(ctx, e.owner, proxy, coordinator.PackGuardian(), coordinator.UnpackGuardian)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	paused, err := rawCoordinatorCall(ctx, e.owner, proxy, coordinator.PackPaused(), coordinator.UnpackPaused)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	current, err := rawCoordinatorCall(ctx, e.owner, proxy, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	policy, err := rawCoordinatorCall(ctx, e.owner, proxy, coordinator.PackPolicyAt(current), coordinator.UnpackPolicyAt)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	vaultCoordinator, err := rawCoordinatorCall(ctx, e.owner, e.payloads.Manifest.SettlementVault, vault.PackCoordinator(), vault.UnpackCoordinator)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	reserveRecorder, err := rawCoordinatorCall(ctx, e.owner, e.payloads.Manifest.ReserveSink, reserve.PackRecorder(), reserve.UnpackRecorder)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	principal, err := rawCoordinatorCall(ctx, e.owner, e.payloads.Manifest.ReserveSink, reserve.PackPrincipal(), reserve.UnpackPrincipal)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	live, err := rawCoordinatorCall(ctx, e.owner, e.payloads.Manifest.ReserveSink, reserve.PackLiveStake(), reserve.UnpackLiveStake)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	entitlement, err := rawCoordinatorCall(ctx, e.owner, e.payloads.Manifest.SettlementVault, vault.PackEntitlement(new(big.Int).SetUint64(epoch), new(big.Int).SetUint64(noID)), vault.UnpackEntitlement)
	if err != nil {
		return GovernanceCustodySnapshot{}, err
	}
	return GovernanceCustodySnapshot{FinalizedHead: head, Implementation: implementation.Hex(), Owner: owner.Hex(), Guardian: guardian.Hex(), Paused: paused, PolicyHash: "0x" + hex.EncodeToString(policy.PolicyHash[:]), VaultCoordinator: vaultCoordinator.Hex(), ReserveRecorder: reserveRecorder.Hex(), ReservePrincipalRao: principal.String(), ReserveLiveStakeRao: live.String(), Entitlement: entitlementSnapshot(epoch, noID, entitlement)}, nil
}

func (e *Executor) executeGovernanceDrillAction(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	switch a.ID {
	case "governance.guardian-pause":
		return e.governancePause(ctx, a)
	case "governance.upgrade-adversary":
		return e.governanceUpgrade(ctx, a, e.payloads.Manifest.GovernanceDrillImplementation, "adversary-active")
	case "governance.probe-custody":
		return e.governanceProbe(ctx, a)
	case "governance.restore-coordinator":
		return e.governanceUpgrade(ctx, a, e.payloads.CoordinatorUpgrade.Implementation, "restored")
	case "governance.guardian-unpause":
		return e.governanceUnpause(ctx, a)
	default:
		return fmt.Errorf("unknown governance drill action %s", a.ID)
	}
}

func (e *Executor) governancePause(ctx context.Context, a Action) error {
	if prior, err := e.loadGovernanceEvidence(); err == nil && governanceStageRank(prior.Stage) >= 1 {
		return nil
	}
	epoch, noID, entitlement, err := e.findFinalizedEntitlement(ctx)
	if err != nil {
		return err
	}
	before, err := e.governanceSnapshot(ctx, epoch, noID)
	if err != nil {
		return err
	}
	if before.Paused || !strings.EqualFold(before.Implementation, e.payloads.CoordinatorUpgrade.Implementation.Hex()) {
		return errors.New("governance drill must start unpaused on the reviewed implementation")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	data, err := parsed.Pack("setPaused", true)
	if err != nil {
		return err
	}
	proxy := e.payloads.Manifest.CoordinatorProxy
	receipt, err := e.guardian.Send(ctx, e.plan.PlanHash, a, &proxy, big.NewInt(0), data)
	if err != nil {
		return err
	}
	after, err := e.governanceSnapshot(ctx, epoch, noID)
	if err != nil || !after.Paused {
		return stateMismatchError(err, "guardian pause postcondition is false")
	}
	evidence := &GovernanceDrillEvidence{Schema: "urnetwork-governance-drill-evidence-v1", DeploymentID: e.cfg.Config.Deployment.DeploymentID, PlanHash: e.plan.PlanHash, Stage: "paused", Before: before, Transactions: map[string]string{a.ID: receipt.TxHash.Hex()}}
	_ = entitlement // selected value is represented in before at one finalized checkpoint
	return e.writeGovernanceEvidence(evidence)
}

func (e *Executor) governanceUpgrade(ctx context.Context, a Action, implementation common.Address, stage string) error {
	evidence, err := e.loadGovernanceEvidence()
	if err != nil {
		return err
	}
	if governanceStageRank(evidence.Stage) >= governanceStageRank(stage) {
		if governanceStageRank(evidence.Stage) > governanceStageRank(stage) {
			return nil
		}
		head, headErr := finalizedEVMHead(ctx, e.owner.client)
		if headErr != nil {
			return headErr
		}
		got, slotErr := implementationAt(ctx, e.owner, e.payloads.Manifest.CoordinatorProxy, head.Number)
		if slotErr == nil && got == implementation {
			return nil
		}
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	data, err := parsed.Pack("upgradeToAndCall", implementation, []byte{})
	if err != nil {
		return err
	}
	proxy := e.payloads.Manifest.CoordinatorProxy
	receipt, err := e.owner.Send(ctx, e.plan.PlanHash, a, &proxy, big.NewInt(0), data)
	if err != nil {
		return err
	}
	head, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return err
	}
	got, err := implementationAt(ctx, e.owner, proxy, head.Number)
	if err != nil || got != implementation {
		return stateMismatchError(err, "ERC1967 implementation=%s want=%s", got, implementation)
	}
	evidence.Stage = stage
	evidence.Transactions[a.ID] = receipt.TxHash.Hex()
	return e.writeGovernanceEvidence(evidence)
}

func bytes32Label(value string) [32]byte { var out [32]byte; copy(out[:], []byte(value)); return out }

func (e *Executor) governanceProbe(ctx context.Context, a Action) error {
	evidence, err := e.loadGovernanceEvidence()
	if err != nil {
		return err
	}
	if governanceStageRank(evidence.Stage) >= 3 && len(evidence.ProbeResults) == 4 {
		return nil
	}
	if evidence.Stage != "adversary-active" {
		return errors.New("custody probes require the adversary implementation")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorAdversaryABI))
	if err != nil {
		return err
	}
	head, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return err
	}
	if head.Number > ^uint64(0)-e.cfg.Policy.Settlement.EpochBlocks*e.cfg.Policy.Settlement.ClaimTTLEpochs-100 {
		return errors.New("governance probe expiry overflows")
	}
	replacementRoot := derive32(e.cfg, "governance-drill/replacement-root")
	replacementArtifact := derive32(e.cfg, "governance-drill/replacement-artifact")
	destination, err := roleBytes32(e.roles, "miner-1-payout")
	if err != nil {
		return err
	}
	reserveHotkey, err := roleBytes32(e.roles, "reserve-hotkey")
	if err != nil {
		return err
	}
	data, err := parsed.Pack("runCustodyProbes", new(big.Int).SetUint64(evidence.Before.Entitlement.Epoch), new(big.Int).SetUint64(evidence.Before.Entitlement.NoID), replacementRoot, replacementArtifact, head.Number+e.cfg.Policy.Settlement.EpochBlocks*e.cfg.Policy.Settlement.ClaimTTLEpochs+100, destination, reserveHotkey)
	if err != nil {
		return err
	}
	owner, _ := e.roles.EVMAddress("testnet-owner")
	proxy := e.payloads.Manifest.CoordinatorProxy
	out, err := e.owner.client.CallContract(ctx, ethereum.CallMsg{From: owner, To: &proxy, Data: data}, new(big.Int).SetUint64(head.Number))
	if err != nil {
		return fmt.Errorf("preflight hostile custody probes: %w", err)
	}
	values, err := parsed.Unpack("runCustodyProbes", out)
	result, resultOK := func() (*big.Int, bool) {
		if len(values) != 1 {
			return nil, false
		}
		value, ok := values[0].(*big.Int)
		return value, ok && value != nil
	}()
	if err != nil || !resultOK || result.Sign() != 0 {
		return errors.New("hostile custody probe preflight found an unexpectedly callable boundary")
	}
	receipt, err := e.owner.Send(ctx, e.plan.PlanHash, a, &proxy, big.NewInt(0), data)
	if err != nil {
		return err
	}
	probeEvent := parsed.Events["CustodyProbe"]
	expected := map[[32]byte]string{bytes32Label("rewrite-finalized-entitlement"): "rewrite-finalized-entitlement", bytes32Label("reset-vault-coordinator"): "reset-vault-coordinator", bytes32Label("reset-reserve-recorder"): "reset-reserve-recorder", bytes32Label("source-reserve-principal"): "source-reserve-principal"}
	results := map[string]bool{}
	for _, log := range receipt.Logs {
		if log.Address != proxy || len(log.Topics) != 2 || log.Topics[0] != probeEvent.ID {
			continue
		}
		var label [32]byte
		copy(label[:], log.Topics[1].Bytes())
		name, ok := expected[label]
		if !ok {
			return errors.New("governance drill emitted an unknown custody probe")
		}
		decoded, unpackErr := probeEvent.Inputs.NonIndexed().Unpack(log.Data)
		if unpackErr != nil || len(decoded) != 2 {
			return stateMismatchError(unpackErr, "decode custody probe returned %d values", len(decoded))
		}
		succeeded, ok := decoded[0].(bool)
		if !ok || succeeded {
			return fmt.Errorf("custody probe %s unexpectedly succeeded", name)
		}
		results[name] = succeeded
	}
	if len(results) != len(expected) {
		return fmt.Errorf("custody probe receipt has %d/%d results", len(results), len(expected))
	}
	evidence.Stage, evidence.ProbeResults, evidence.Transactions[a.ID] = "probed", results, receipt.TxHash.Hex()
	return e.writeGovernanceEvidence(evidence)
}

func sameImmutableEntitlement(before, after GovernanceEntitlementSnapshot) bool {
	return before.Epoch == after.Epoch && before.NoID == after.NoID && before.PayoutRoot == after.PayoutRoot && before.ArtifactHash == after.ArtifactHash && before.FundedRao == after.FundedRao && before.TotalRao == after.TotalRao && before.ExpiryBlock == after.ExpiryBlock && before.Status == after.Status
}

func (e *Executor) governanceUnpause(ctx context.Context, a Action) error {
	evidence, err := e.loadGovernanceEvidence()
	if err != nil {
		return err
	}
	if evidence.Stage == "complete" {
		return e.validateCompletedGovernanceDrill(evidence)
	}
	if evidence.Stage != "restored" {
		return errors.New("governance unpause requires the reviewed implementation to be restored")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	data, err := parsed.Pack("setPaused", false)
	if err != nil {
		return err
	}
	proxy := e.payloads.Manifest.CoordinatorProxy
	receipt, err := e.guardian.Send(ctx, e.plan.PlanHash, a, &proxy, big.NewInt(0), data)
	if err != nil {
		return err
	}
	after, err := e.governanceSnapshot(ctx, evidence.Before.Entitlement.Epoch, evidence.Before.Entitlement.NoID)
	if err != nil {
		return err
	}
	evidence.Stage, evidence.After, evidence.Transactions[a.ID] = "complete", &after, receipt.TxHash.Hex()
	if err := e.validateCompletedGovernanceDrill(evidence); err != nil {
		return err
	}
	return e.writeGovernanceEvidence(evidence)
}

func (e *Executor) validateCompletedGovernanceDrill(evidence *GovernanceDrillEvidence) error {
	if evidence.Stage != "complete" || evidence.After == nil || len(evidence.ProbeResults) != 4 {
		return errors.New("governance drill is incomplete")
	}
	before, after := evidence.Before, *evidence.After
	for name, succeeded := range evidence.ProbeResults {
		if succeeded {
			return fmt.Errorf("custody probe %s succeeded", name)
		}
	}
	if after.Paused || !strings.EqualFold(after.Implementation, e.payloads.CoordinatorUpgrade.Implementation.Hex()) || !strings.EqualFold(before.Owner, after.Owner) || !strings.EqualFold(before.Guardian, after.Guardian) || !strings.EqualFold(before.PolicyHash, after.PolicyHash) || !strings.EqualFold(before.VaultCoordinator, after.VaultCoordinator) || !strings.EqualFold(before.ReserveRecorder, after.ReserveRecorder) || before.ReservePrincipalRao != after.ReservePrincipalRao || !sameImmutableEntitlement(before.Entitlement, after.Entitlement) {
		return errors.New("governance drill changed an immutable custody, role, policy, or entitlement field")
	}
	liveBefore, ok1 := new(big.Int).SetString(before.ReserveLiveStakeRao, 10)
	liveAfter, ok2 := new(big.Int).SetString(after.ReserveLiveStakeRao, 10)
	claimedBefore, ok3 := new(big.Int).SetString(before.Entitlement.ClaimedRao, 10)
	claimedAfter, ok4 := new(big.Int).SetString(after.Entitlement.ClaimedRao, 10)
	if !ok1 || !ok2 || liveAfter.Cmp(liveBefore) < 0 || !ok3 || !ok4 || claimedAfter.Cmp(claimedBefore) < 0 {
		return errors.New("governance drill reduced reserve stake or claimed accounting")
	}
	return nil
}

func (e *Executor) verifyGovernanceDrillPostState(ctx context.Context, a Action, state map[string]any) (map[string]any, error) {
	evidence, err := e.loadGovernanceEvidence()
	if err != nil {
		return nil, err
	}
	want := map[string]int{"governance.guardian-pause": 1, "governance.upgrade-adversary": 2, "governance.probe-custody": 3, "governance.restore-coordinator": 4, "governance.guardian-unpause": 5}[a.ID]
	if governanceStageRank(evidence.Stage) < want {
		return nil, fmt.Errorf("governance drill stage %s is before %s", evidence.Stage, a.ID)
	}
	if a.ID == "governance.guardian-unpause" {
		if err := e.validateCompletedGovernanceDrill(evidence); err != nil {
			return nil, err
		}
	}
	head, err := finalizedEVMHead(ctx, e.owner.client)
	if err != nil {
		return nil, err
	}
	implementation, err := implementationAt(ctx, e.owner, e.payloads.Manifest.CoordinatorProxy, head.Number)
	if err != nil {
		return nil, err
	}
	state["stage"], state["implementation"], state["evidence"] = evidence.Stage, implementation.Hex(), "public/governance-drill.json"
	state["probe_failures"] = len(evidence.ProbeResults)
	return state, nil
}

func waitForGovernanceDrillReady(ctx context.Context, executor *Executor, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := executor.ensurePayloads(ctx); err != nil {
			return err
		}
		if _, _, _, err := executor.findFinalizedEntitlement(ctx); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for a finalized entitlement before the governance drill")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(12 * time.Second):
		}
	}
}
