// Release plan revisions preserve an immutable on-chain deployment baseline
// while allowing a newly locked binary/configuration to repair an interrupted
// campaign under a newly reviewed hash.
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	substrateTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
)

// Holds one deterministic native registration ownership pair.
type plannedRegistrationIdentity struct {
	hotkey  [32]byte
	coldkey [32]byte
}

// Merges the recovery and inclusion evidence for one prior action intent.
type planRevisionTransaction struct {
	PlanHash, ActionID, IntentHash, TransactionHash string
	Signer, Nonce                                   string
	RecoveryBlock                                   uint64
	RecoveryBlockHash                               string
	BlockNumber                                     uint64
	BlockHash                                       string
	JournalSequence                                 uint64
}

var (
	errPriorNativeTransactionSucceeded = errors.New("prior native transaction succeeded without durable postcondition verification")
	errPriorEVMTransactionSucceeded    = errors.New("prior EVM transaction succeeded without durable postcondition verification")
)

type coordinatorUpgradeMigration struct {
	Deployment ContractDeployment
	Baseline   CoordinatorUpgradeBaseline
	Upgrade    CoordinatorUpgrade
}

var abandonableDeploymentActions = []string{
	"evm.reserve-sink",
	"evm.settlement-vault",
	"evm.coordinator-implementation",
}

// Every deployer nonce in a completely installed release generation. Calls
// which do not CREATE still consume an EVM account nonce and must retain their
// exact approved intent in this sequence.
var supersedableDeploymentNonceActions = []string{
	"evm.reserve-sink",
	"evm.settlement-vault",
	"evm.coordinator-implementation",
	"evm.vault-register-escrow",
	"evm.coordinator-proxy",
	"evm.governance-drill-implementation",
	"evm.vault-fix-coordinator",
	"evm.sink-fix-recorder",
	"precompile.probe-deploy",
	"evm.coordinator-upgrade-implementation",
}

var supersedableDeploymentEffectActions = append(
	append([]string(nil), supersedableDeploymentNonceActions...),
	"evm.coordinator-upgrade-activate",
	"policy.schedule-bootstrap",
)

// These steps have only local, deterministic effects. A replacement rerenders
// the new contract addresses while preserving the same role identities; neither
// step proves that the old on-chain generation was economically used.
var supersedableDeploymentLocalActions = []string{
	"config.render",
	"accounts.provision",
}

func exactVerifiedPlanAction(prior *SetupPlan, entries []JournalEntry, actionID string) bool {
	if prior == nil {
		return false
	}
	var planned *Action
	for _, action := range prior.Actions {
		if action.ID == actionID {
			if planned != nil {
				return false
			}
			copy := action
			planned = &copy
		}
	}
	if planned == nil {
		return false
	}
	for _, entry := range entries {
		if prior.allowedPlanHashes()[entry.PlanHash] && entry.ActionID == actionID && actionAcceptsIntent(*planned, entry.IntentHash) && entry.Stage == StageVerified {
			return true
		}
	}
	return false
}

// Resolve the next safe deployer nonce after an active coordinator release.
// The testnet-only fleet batcher is a CREATE immediately after the coordinator
// implementation, so a verified batcher advances the repeated-upgrade boundary
// by one more nonce. Its exact plan envelope is authenticated here before that
// extra nonce can influence a replacement release.
func repeatedCoordinatorUpgradeBoundary(prior *SetupPlan, entries []JournalEntry) (uint64, *Action, error) {
	if prior == nil || prior.CoordinatorUpgrade.DeployerNonce > ^uint64(0)-2 {
		return 0, nil, errors.New("repeated coordinator upgrade nonce range overflows")
	}
	next := prior.CoordinatorUpgrade.DeployerNonce + 1
	if !exactVerifiedPlanAction(prior, entries, "fleet.refresh.deploy-batcher") {
		return next, nil, nil
	}
	var batcher *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID != "fleet.refresh.deploy-batcher" {
			continue
		}
		if batcher != nil {
			return 0, nil, errors.New("prior plan has duplicate fleet batcher deployment actions")
		}
		copy := prior.Actions[index]
		batcher = &copy
	}
	if batcher == nil || batcher.Kind != "evm-transaction" || !common.IsHexAddress(batcher.Target) {
		return 0, nil, errors.New("verified fleet batcher deployment has no exact EVM target")
	}
	deployer := common.HexToAddress(prior.Roles.Deployer)
	expectedAddress := crypto.CreateAddress(deployer, next)
	expectedNonce, nonceErr := strconv.ParseUint(batcher.Parameters["expected_nonce"], 10, 64)
	if deployer == (common.Address{}) || expectedNonce != next || nonceErr != nil || !strings.EqualFold(batcher.Parameters["expected_signer"], deployer.Hex()) || batcher.Parameters["expected_transaction_to"] != "create" || !strings.EqualFold(batcher.Parameters["expected_created_address"], expectedAddress.Hex()) || common.HexToAddress(batcher.Target) != expectedAddress {
		return 0, nil, stateMismatchError(nonceErr, "verified fleet batcher deployment does not bind deployer nonce %d and CREATE address %s", next, expectedAddress)
	}
	if _, err := decodeHex32("verified fleet batcher runtime hash", batcher.Parameters["runtime_code_hash"]); err != nil {
		return 0, nil, err
	}
	return next + 1, batcher, nil
}

// Accept only nonce positions authenticated by the migration itself. A fully
// activated repeated upgrade consumes one CREATE nonce, and the optional exact
// verified fleet batcher consumes the immediately following CREATE nonce. The
// latter boundary was previously recognized by live observation but omitted by
// the pure revision builder, making the next Go-only revision fail after a
// successful batcher deployment.
func coordinatorUpgradeMigrationNonceMatches(prior *SetupPlan, migration *coordinatorUpgradeMigration, payloads *DeploymentPayloads, currentNonce uint64, entries []JournalEntry) (bool, error) {
	if prior == nil || migration == nil || payloads == nil {
		return false, errors.New("coordinator upgrade migration nonce context is unavailable")
	}
	if currentNonce == migration.Baseline.DeployerNonce || currentNonce == payloads.CoordinatorUpgrade.DeployerNonce {
		return true, nil
	}
	if payloads.CoordinatorUpgrade.DeployerNonce != ^uint64(0) && currentNonce == payloads.CoordinatorUpgrade.DeployerNonce+1 && exactVerifiedPlanAction(prior, entries, "evm.coordinator-upgrade-implementation") {
		return true, nil
	}
	if migration.Upgrade != prior.CoordinatorUpgrade || !exactVerifiedPlanAction(prior, entries, "evm.coordinator-upgrade-implementation") || !exactVerifiedPlanAction(prior, entries, "evm.coordinator-upgrade-activate") {
		return false, nil
	}
	boundary, batcher, err := repeatedCoordinatorUpgradeBoundary(prior, entries)
	if err != nil {
		return false, err
	}
	return batcher != nil && currentNonce == boundary, nil
}

// Prove that every nonce consumed by an obsolete immutable deployment belongs
// to the exact verified CREATE prefix and that no contract-owned registration
// has made that deployment economically live.
func validateAbandonableDeployment(cfg *ResolvedConfig, prior *SetupPlan, manifest ContractDeployment, current *SetupFacts, entries []JournalEntry) error {
	if cfg == nil || prior == nil || current == nil || current.DeployerNonce < manifest.InitialNonce {
		return errors.New("obsolete deployment nonce facts are inconsistent")
	}
	consumed := current.DeployerNonce - manifest.InitialNonce
	fullGeneration := consumed == uint64(len(supersedableDeploymentNonceActions)) && planUsesCoordinatorUpgradeEnvelope(prior.Schema)
	if consumed > uint64(len(abandonableDeploymentActions)) && !fullGeneration {
		return fmt.Errorf("obsolete deployment consumed %d deployer nonces; only the %d pre-registration CREATEs or the exact %d-nonce inert release generation are supersedable", consumed, len(abandonableDeploymentActions), len(supersedableDeploymentNonceActions))
	}
	progress := int(current.ExistingUIDCount) - int(prior.LiveFacts.ExistingUIDCount)
	if progress < 0 || !fullGeneration && progress > cfg.Config.Topology.ChurnFloorUIDs {
		return fmt.Errorf("obsolete deployment cannot be abandoned after %d controlled registrations", progress)
	}
	allowedPlans := prior.allowedPlanHashes()
	type nonceEvidence struct {
		actionID, intentHash string
		verified             bool
	}
	byNonce := map[uint64]nonceEvidence{}
	priorActions := make(map[string]Action, len(prior.Actions))
	for _, action := range prior.Actions {
		priorActions[action.ID+"\x00"+action.IntentHash] = action
	}
	supersedable := map[string]bool{}
	allowedNonceActions := abandonableDeploymentActions
	allowedEffectActions := abandonableDeploymentActions
	if fullGeneration {
		allowedNonceActions = supersedableDeploymentNonceActions
		allowedEffectActions = supersedableDeploymentEffectActions
	}
	for _, actionID := range allowedEffectActions {
		supersedable[actionID] = true
	}
	if fullGeneration {
		supersedable["campaign.evm-gas-reserve"] = true
		for _, actionID := range supersedableDeploymentLocalActions {
			supersedable[actionID] = true
		}
		for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
			supersedable[fmt.Sprintf("operator.register.%d", operator)] = true
			supersedable[fmt.Sprintf("alpha.transfer.operator-deposit.%d", operator)] = true
		}
	}
	for _, entry := range entries {
		if !allowedPlans[entry.PlanHash] {
			continue
		}
		if entry.Signer != "" && strings.EqualFold(entry.Signer, prior.Roles.Deployer) && entry.Nonce != "" {
			nonce, err := strconv.ParseUint(entry.Nonce, 10, 64)
			if err != nil {
				return fmt.Errorf("decode obsolete deployment nonce: %w", err)
			}
			evidence := byNonce[nonce]
			if evidence.actionID != "" && (evidence.actionID != entry.ActionID || evidence.intentHash != entry.IntentHash) {
				return fmt.Errorf("deployer nonce %d has actions %s and %s", nonce, evidence.actionID, entry.ActionID)
			}
			evidence.actionID, evidence.intentHash = entry.ActionID, entry.IntentHash
			byNonce[nonce] = evidence
		}
		if entry.Stage == StageVerified {
			if action, ok := priorActions[entry.ActionID+"\x00"+entry.IntentHash]; ok && actionUsesContractDeployment(action) && !supersedable[action.ID] {
				return fmt.Errorf("obsolete deployment action %s was already verified and cannot be abandoned", action.ID)
			}
			for nonce, evidence := range byNonce {
				if evidence.actionID == entry.ActionID && evidence.intentHash == entry.IntentHash {
					evidence.verified = true
					byNonce[nonce] = evidence
				}
			}
		}
	}
	for offset := uint64(0); offset < consumed; offset++ {
		nonce := manifest.InitialNonce + offset
		evidence := byNonce[nonce]
		if evidence.actionID != allowedNonceActions[offset] || !evidence.verified {
			return fmt.Errorf("deployer nonce %d is not the verified supersedable action %s", nonce, allowedNonceActions[offset])
		}
	}
	if fullGeneration && !exactVerifiedPlanAction(prior, entries, "evm.coordinator-upgrade-activate") {
		return errors.New("fully installed obsolete deployment has no exact verified coordinator upgrade activation")
	}
	return nil
}

// Captures every finalized EVM fact which must remain inert before immutable
// deployment addresses can be retired under a replacement plan.
type supersededDeploymentObservation struct {
	FinalizedHead        ChainHead
	DeployBlockHash      string
	DeployerNonce        uint64
	RuntimeCodeHashes    map[common.Address]string
	Balances             map[common.Address]*big.Int
	NativeFreeBalances   map[common.Address]uint64
	EscrowRegistered     bool
	VaultCoordinator     common.Address
	ReserveRecorder      common.Address
	ProxyImplementation  common.Address
	UpgradeRuntimeHash   string
	UpgradeBalance       *big.Int
	UpgradeNativeFree    uint64
	OperatorCount        *big.Int
	CampaignReserved     *big.Int
	ReservePrincipal     *big.Int
	ReserveLiveStake     *big.Int
	TotalCaptured        *big.Int
	TotalPaid            *big.Int
	EscrowAccounted      *big.Int
	PendingFunding       *big.Int
	OutstandingLiability *big.Int
	LiveEscrowStake      *big.Int
	Operators            []supersededOperatorObservation
}

type supersededOperatorIdentity struct {
	NoID                               uint64
	Coldkey, PoolHotkey, DepositHotkey [32]byte
	DepositSigner, RootSigner          common.Address
}

type supersededOperatorObservation struct {
	Identity             supersededOperatorIdentity
	VersionCount         uint64
	Active               bool
	PoolUID              uint16
	PoolActive           bool
	NextDepositNonce     *big.Int
	CumulativeConviction *big.Int
	Carry                *big.Int
	ReservePrincipal     *big.Int
	PoolLiveStake        *big.Int
}

func expectedSupersededOperators(cfg *ResolvedConfig, roles *RoleSecrets, prior *SetupPlan, entries []JournalEntry) ([]supersededOperatorIdentity, error) {
	if cfg == nil || roles == nil || prior == nil {
		return nil, errors.New("superseded operator identities are unavailable")
	}
	result := make([]supersededOperatorIdentity, 0, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		if !exactVerifiedPlanAction(prior, entries, fmt.Sprintf("operator.register.%d", operator)) {
			continue
		}
		coldkey, err := roleBytes32(roles, fmt.Sprintf("operator-%d-coldkey", operator))
		if err != nil {
			return nil, err
		}
		pool, err := roleBytes32(roles, operatorPoolHotkeyLabelForGeneration(operator, prior.Deployment.RegistrationRoleGeneration))
		if err != nil {
			return nil, err
		}
		deposit, err := roleBytes32(roles, fmt.Sprintf("operator-%d-deposit-hotkey", operator))
		if err != nil {
			return nil, err
		}
		depositSigner, err := roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", operator))
		if err != nil {
			return nil, err
		}
		rootSigner, err := roles.EVMAddress(fmt.Sprintf("operator-%d-root", operator))
		if err != nil {
			return nil, err
		}
		result = append(result, supersededOperatorIdentity{
			NoID: uint64(operator), Coldkey: coldkey, PoolHotkey: pool, DepositHotkey: deposit,
			DepositSigner: depositSigner, RootSigner: rootSigner,
		})
	}
	return result, nil
}

// Validate a deterministic observation without depending on RPC timing. A
// partial generation is replaceable only through its first three CREATEs. A
// completely installed generation is replaceable only when every immutable
// runtime/link is exact and all economic and custody state remains zero.
func validateSupersededDeploymentObservation(manifest ContractDeployment, upgrade CoordinatorUpgrade, expectedOperators []supersededOperatorIdentity, current *SetupFacts, observation supersededDeploymentObservation) error {
	if current == nil || current.DeployerNonce < manifest.InitialNonce {
		return errors.New("superseded deployment observation has inconsistent nonce facts")
	}
	consumed := current.DeployerNonce - manifest.InitialNonce
	fullGeneration := consumed == uint64(len(supersedableDeploymentNonceActions))
	if consumed > uint64(len(abandonableDeploymentActions)) && !fullGeneration {
		return fmt.Errorf("superseded deployment observation consumed %d nonces", consumed)
	}
	if fullGeneration {
		if upgrade.Schema != "urnetwork-coordinator-upgrade-v1" || upgrade.DeploymentID != manifest.DeploymentID || upgrade.DeployerNonce != manifest.InitialNonce+9 || upgrade.Implementation == (common.Address{}) {
			return errors.New("superseded full generation has no exact coordinator upgrade identity")
		}
		if _, err := decodeHex32("superseded coordinator upgrade runtime hash", upgrade.RuntimeCodeHash); err != nil {
			return err
		}
	}
	if observation.FinalizedHead.Number < current.EVMFinalizedBlock || observation.DeployerNonce != current.DeployerNonce {
		return fmt.Errorf("superseded deployment changed while observed: finalized=%d/%d nonce=%d/%d", observation.FinalizedHead.Number, current.EVMFinalizedBlock, observation.DeployerNonce, current.DeployerNonce)
	}
	if _, err := decodeHex32("superseded deployment finalized EVM hash", observation.FinalizedHead.Hash); err != nil {
		return err
	}
	if observation.FinalizedHead.Number == current.EVMFinalizedBlock && current.EVMFinalizedBlockHash != "" && !strings.EqualFold(observation.FinalizedHead.Hash, current.EVMFinalizedBlockHash) {
		return errors.New("superseded deployment observation changed the approved finalized EVM head")
	}
	if manifest.DeployBlock == 0 {
		if manifest.DeployBlockHash != "" || consumed != 0 {
			return errors.New("superseded deployment has inconsistent empty deployment checkpoint")
		}
	} else {
		if _, err := decodeHex32("superseded deployment block hash", manifest.DeployBlockHash); err != nil {
			return err
		}
		if manifest.DeployBlock > observation.FinalizedHead.Number || !strings.EqualFold(manifest.DeployBlockHash, observation.DeployBlockHash) {
			return errors.New("superseded deployment checkpoint is not canonical and finalized")
		}
	}
	expectedHashes, err := normalizedDeploymentRuntimeHashes(manifest)
	if err != nil {
		return err
	}
	addresses := contractDeploymentAddresses(manifest)
	if len(observation.RuntimeCodeHashes) != len(addresses) || len(observation.Balances) != len(addresses) || len(observation.NativeFreeBalances) != len(addresses) {
		return errors.New("superseded deployment observation is missing an approved address")
	}
	createdAfterNonceCount := map[common.Address]uint64{
		manifest.ReserveSink:                   1,
		manifest.SettlementVault:               2,
		manifest.CoordinatorImplementation:     3,
		manifest.CoordinatorProxy:              5,
		manifest.GovernanceDrillImplementation: 6,
		manifest.PrecompileProbe:               9,
	}
	for _, address := range addresses {
		codeHash, present := observation.RuntimeCodeHashes[address]
		if !present {
			return fmt.Errorf("superseded deployment observation has no code fact for %s", address)
		}
		if consumed >= createdAfterNonceCount[address] {
			if codeHash == "" || !strings.EqualFold(codeHash, expectedHashes[address]) {
				return fmt.Errorf("superseded deployment runtime mismatch at %s", address)
			}
		} else if codeHash != "" {
			return fmt.Errorf("superseded deployment has unexpected later code at %s", address)
		}
		balance, present := observation.Balances[address]
		if !present || balance == nil || balance.Sign() != 0 {
			return fmt.Errorf("superseded deployment retains EVM balance at %s", address)
		}
		freeRao, present := observation.NativeFreeBalances[address]
		expectedFreeRao := uint64(0)
		if fullGeneration && (address == manifest.SettlementVault || address == manifest.CoordinatorProxy) {
			expectedFreeRao = current.ExistentialDepositRao
		}
		if !present || expectedFreeRao == 0 && fullGeneration && (address == manifest.SettlementVault || address == manifest.CoordinatorProxy) || freeRao != expectedFreeRao {
			return fmt.Errorf("superseded deployment native free balance at %s is %d rao, want exact non-transferable residue %d", address, freeRao, expectedFreeRao)
		}
	}
	if !fullGeneration {
		if consumed >= 1 && observation.ReserveRecorder != (common.Address{}) {
			return errors.New("partially superseded reserve sink already fixed its recorder")
		}
		if consumed >= 2 && (observation.EscrowRegistered || observation.VaultCoordinator != (common.Address{})) {
			return errors.New("partially superseded settlement vault already registered or fixed its coordinator")
		}
		return nil
	}
	if !observation.EscrowRegistered || observation.VaultCoordinator != manifest.CoordinatorProxy || observation.ReserveRecorder != manifest.CoordinatorProxy {
		return errors.New("superseded full generation bootstrap links differ from the approved proxy")
	}
	if observation.ProxyImplementation != upgrade.Implementation || !strings.EqualFold(observation.UpgradeRuntimeHash, upgrade.RuntimeCodeHash) {
		return errors.New("superseded full generation proxy or upgrade runtime differs from the approved repair")
	}
	if observation.UpgradeBalance == nil || observation.UpgradeBalance.Sign() != 0 || observation.UpgradeNativeFree != 0 {
		return errors.New("superseded coordinator upgrade retains an EVM or hidden native balance")
	}
	if observation.OperatorCount == nil || !observation.OperatorCount.IsUint64() || observation.OperatorCount.Uint64() != uint64(len(expectedOperators)) || len(observation.Operators) != len(expectedOperators) {
		return fmt.Errorf("superseded full generation operator count is %v/%d, want %d", observation.OperatorCount, len(observation.Operators), len(expectedOperators))
	}
	for index, expected := range expectedOperators {
		operator := observation.Operators[index]
		if operator.Identity != expected || operator.VersionCount != 1 || !operator.Active || !operator.PoolActive || operator.PoolUID == 0 {
			return fmt.Errorf("superseded operator %d identity, version, or active pool differs from its verified registration", expected.NoID)
		}
		operatorEconomics := []struct {
			name  string
			value *big.Int
		}{
			{"next deposit nonce", operator.NextDepositNonce},
			{"cumulative conviction", operator.CumulativeConviction},
			{"carry", operator.Carry},
			{"reserve principal", operator.ReservePrincipal},
			{"pool live stake", operator.PoolLiveStake},
		}
		for _, economic := range operatorEconomics {
			if economic.value == nil || economic.value.Sign() != 0 {
				return fmt.Errorf("superseded operator %d %s is %v, want zero", expected.NoID, economic.name, economic.value)
			}
		}
	}
	globalEconomics := []struct {
		name  string
		value *big.Int
	}{
		{"campaign reserve", observation.CampaignReserved},
		{"reserve principal", observation.ReservePrincipal},
		{"reserve live stake", observation.ReserveLiveStake},
		{"total captured", observation.TotalCaptured},
		{"total paid", observation.TotalPaid},
		{"escrow accounted", observation.EscrowAccounted},
		{"pending funding", observation.PendingFunding},
		{"outstanding liability", observation.OutstandingLiability},
		{"live escrow stake", observation.LiveEscrowStake},
	}
	for _, economic := range globalEconomics {
		if economic.value == nil || economic.value.Sign() != 0 {
			return fmt.Errorf("superseded full generation %s is %v, want zero", economic.name, economic.value)
		}
	}
	return nil
}

// Read and verify the obsolete deployment at one newer finalized EVM head.
// Any concurrent deployer transaction or hidden contract state blocks revision.
func validateSupersededDeploymentOnChain(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, manifest ContractDeployment, upgrade CoordinatorUpgrade, current *SetupFacts, prior *SetupPlan, entries []JournalEntry) error {
	if cfg == nil || roles == nil || current == nil {
		return errors.New("superseded deployment live validation is unavailable")
	}
	deployer, err := roles.EVMAddress("deployer")
	if err != nil {
		return err
	}
	if err := validateContractDeploymentIdentity(manifest, deployer); err != nil {
		return err
	}
	if current.DeployerNonce < manifest.InitialNonce {
		return errors.New("superseded deployment deployer nonce moved backwards")
	}
	consumed := current.DeployerNonce - manifest.InitialNonce
	fullGeneration := consumed == uint64(len(supersedableDeploymentNonceActions))
	expectedOperators := []supersededOperatorIdentity(nil)
	if fullGeneration {
		if err := validateCoordinatorUpgradeIdentity(upgrade, deployer, manifest); err != nil {
			return fmt.Errorf("validate superseded coordinator upgrade: %w", err)
		}
		expectedOperators, err = expectedSupersededOperators(cfg, roles, prior, entries)
		if err != nil {
			return err
		}
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return err
	}
	defer client.Close()
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return err
	}
	block := new(big.Int).SetUint64(head.Number)
	observation := supersededDeploymentObservation{
		FinalizedHead: head, RuntimeCodeHashes: map[common.Address]string{}, Balances: map[common.Address]*big.Int{}, NativeFreeBalances: map[common.Address]uint64{},
	}
	observation.DeployerNonce, err = client.NonceAt(ctx, deployer, block)
	if err != nil {
		return fmt.Errorf("read superseded deployer nonce: %w", err)
	}
	if manifest.DeployBlock != 0 {
		observation.DeployBlockHash, err = canonicalEVMBlockHash(ctx, ethEVMBlockReader{client: client}, manifest.DeployBlock)
		if err != nil {
			return fmt.Errorf("read superseded deployment checkpoint: %w", err)
		}
	}
	nativeChain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		return fmt.Errorf("dial native chain for superseded balances: %w", err)
	}
	defer nativeChain.API.Client.Close()
	if strings.ToLower(nativeChain.GenesisHash.Hex()) != testnetGenesis || uint32(nativeChain.Runtime.SpecVersion) != cfg.Public.Chain.ExpectedRuntimeSpec {
		return errors.New("native superseded-balance RPC identity mismatch")
	}
	nativeFinalizedHash, err := nativeChain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return err
	}
	nativeFinalizedHeader, err := nativeChain.API.RPC.Chain.GetHeader(nativeFinalizedHash)
	if err != nil {
		return err
	}
	if uint64(nativeFinalizedHeader.Number) < head.Number {
		return fmt.Errorf("native finalized head %d is behind EVM observation %d", nativeFinalizedHeader.Number, head.Number)
	}
	nativeObservationHash, err := nativeChain.API.RPC.Chain.GetBlockHash(head.Number)
	if err != nil {
		return err
	}
	for _, address := range contractDeploymentAddresses(manifest) {
		code, codeErr := client.CodeAt(ctx, address, block)
		if codeErr != nil {
			return fmt.Errorf("read superseded runtime at %s: %w", address, codeErr)
		}
		if len(code) != 0 {
			observation.RuntimeCodeHashes[address] = crypto.Keccak256Hash(code).Hex()
		} else {
			observation.RuntimeCodeHashes[address] = ""
		}
		balance, balanceErr := client.BalanceAt(ctx, address, block)
		if balanceErr != nil {
			return fmt.Errorf("read superseded balance at %s: %w", address, balanceErr)
		}
		observation.Balances[address] = new(big.Int).Set(balance)
		freeRao, freeErr := readFreeBalanceAtHash(nativeChain, ss58Mirror(address), nativeObservationHash)
		if freeErr != nil {
			return fmt.Errorf("read superseded native free balance at %s: %w", address, freeErr)
		}
		observation.NativeFreeBalances[address] = freeRao
	}
	if fullGeneration {
		upgradeCode, codeErr := client.CodeAt(ctx, upgrade.Implementation, block)
		if codeErr != nil {
			return fmt.Errorf("read superseded coordinator upgrade runtime: %w", codeErr)
		}
		if len(upgradeCode) != 0 {
			observation.UpgradeRuntimeHash = crypto.Keccak256Hash(upgradeCode).Hex()
		}
		observation.UpgradeBalance, err = client.BalanceAt(ctx, upgrade.Implementation, block)
		if err != nil {
			return fmt.Errorf("read superseded coordinator upgrade balance: %w", err)
		}
		observation.UpgradeNativeFree, err = readFreeBalanceAtHash(nativeChain, ss58Mirror(upgrade.Implementation), nativeObservationHash)
		if err != nil {
			return fmt.Errorf("read superseded coordinator upgrade native free balance: %w", err)
		}
		implementationSlot, storageErr := client.StorageAt(ctx, manifest.CoordinatorProxy, common.HexToHash(erc1967ImplementationSlot), block)
		if storageErr != nil || len(implementationSlot) != 32 {
			return stateMismatchError(storageErr, "read superseded proxy implementation slot length=%d", len(implementationSlot))
		}
		observation.ProxyImplementation = common.BytesToAddress(implementationSlot[12:])
	}
	if consumed >= 1 {
		reserveABI, parseErr := abi.JSON(strings.NewReader(ReserveSinkABI))
		if parseErr != nil {
			return parseErr
		}
		values, callErr := contractCallAt(ctx, client, manifest.ReserveSink, reserveABI, "recorder", head.Number)
		if callErr != nil {
			return fmt.Errorf("read superseded reserve recorder: %w", callErr)
		}
		if len(values) != 1 {
			return fmt.Errorf("read superseded reserve recorder: got %d values", len(values))
		}
		var ok bool
		observation.ReserveRecorder, ok = values[0].(common.Address)
		if !ok {
			return errors.New("decode superseded reserve recorder")
		}
	}
	if consumed >= 2 {
		vaultABI, parseErr := abi.JSON(strings.NewReader(SettlementVaultABI))
		if parseErr != nil {
			return parseErr
		}
		registered, callErr := contractCallAt(ctx, client, manifest.SettlementVault, vaultABI, "escrowRegistered", head.Number)
		if callErr != nil {
			return fmt.Errorf("read superseded escrow registration: %w", callErr)
		}
		if len(registered) != 1 {
			return fmt.Errorf("read superseded escrow registration: got %d values", len(registered))
		}
		var ok bool
		observation.EscrowRegistered, ok = registered[0].(bool)
		if !ok {
			return errors.New("decode superseded escrow registration")
		}
		coordinator, callErr := contractCallAt(ctx, client, manifest.SettlementVault, vaultABI, "coordinator", head.Number)
		if callErr != nil {
			return fmt.Errorf("read superseded vault coordinator: %w", callErr)
		}
		if len(coordinator) != 1 {
			return fmt.Errorf("read superseded vault coordinator: got %d values", len(coordinator))
		}
		observation.VaultCoordinator, ok = coordinator[0].(common.Address)
		if !ok {
			return errors.New("decode superseded vault coordinator")
		}
	}
	if fullGeneration {
		reserveABI, parseErr := abi.JSON(strings.NewReader(ReserveSinkABI))
		if parseErr != nil {
			return parseErr
		}
		vaultABI, parseErr := abi.JSON(strings.NewReader(SettlementVaultABI))
		if parseErr != nil {
			return parseErr
		}
		coordinatorABI, parseErr := abi.JSON(strings.NewReader(CoordinatorABI))
		if parseErr != nil {
			return parseErr
		}
		readUint := func(address common.Address, parsed abi.ABI, method string, args ...any) (*big.Int, error) {
			values, callErr := contractCallAt(ctx, client, address, parsed, method, head.Number, args...)
			if callErr != nil {
				return nil, fmt.Errorf("read superseded %s: %w", method, callErr)
			}
			if len(values) != 1 {
				return nil, fmt.Errorf("read superseded %s: got %d values", method, len(values))
			}
			value, ok := values[0].(*big.Int)
			if !ok || value.Sign() < 0 {
				return nil, fmt.Errorf("decode superseded %s as uint256: %T", method, values[0])
			}
			return new(big.Int).Set(value), nil
		}
		reads := []struct {
			target      string
			destination **big.Int
		}{
			{"operatorCount", &observation.OperatorCount},
			{"campaignReserved", &observation.CampaignReserved},
			{"principal", &observation.ReservePrincipal},
			{"liveStake", &observation.ReserveLiveStake},
			{"totalCaptured", &observation.TotalCaptured},
			{"totalPaid", &observation.TotalPaid},
			{"escrowAccounted", &observation.EscrowAccounted},
			{"pendingFunding", &observation.PendingFunding},
			{"outstandingLiability", &observation.OutstandingLiability},
			{"liveEscrowStake", &observation.LiveEscrowStake},
		}
		for _, read := range reads {
			target, destination := read.target, read.destination
			address, parsed := manifest.SettlementVault, vaultABI
			if target == "operatorCount" || target == "campaignReserved" {
				address, parsed = manifest.CoordinatorProxy, coordinatorABI
			} else if target == "principal" || target == "liveStake" {
				address, parsed = manifest.ReserveSink, reserveABI
			}
			*destination, err = readUint(address, parsed, target)
			if err != nil {
				return err
			}
		}
		currentEpoch, readErr := readUint(manifest.CoordinatorProxy, coordinatorABI, "currentEpoch")
		if readErr != nil {
			return readErr
		}
		vaultColdkeyValues, callErr := contractCallAt(ctx, client, manifest.SettlementVault, vaultABI, "selfColdkey", head.Number)
		if callErr != nil || len(vaultColdkeyValues) != 1 {
			return stateMismatchError(callErr, "read superseded vault selfColdkey: got %d values", len(vaultColdkeyValues))
		}
		vaultColdkey, decodeErr := decodeHex32("superseded vault selfColdkey", valueHex(vaultColdkeyValues[0]))
		if decodeErr != nil || vaultColdkey != ss58Mirror(manifest.SettlementVault) {
			return stateMismatchError(decodeErr, "superseded vault coldkey differs from its EVM mirror")
		}
		stakingABI, parseErr := abi.JSON(strings.NewReader(stakingPrecompileABI))
		if parseErr != nil {
			return parseErr
		}
		for index, expected := range expectedOperators {
			id := new(big.Int).SetUint64(expected.NoID)
			observedID, readErr := readUint(manifest.CoordinatorProxy, coordinatorABI, "operatorIdAt", new(big.Int).SetUint64(uint64(index)))
			if readErr != nil || !observedID.IsUint64() || observedID.Uint64() != expected.NoID {
				return stateMismatchError(readErr, "superseded operatorIdAt(%d)=%v want=%d", index, observedID, expected.NoID)
			}
			versionCount, readErr := readUint(manifest.CoordinatorProxy, coordinatorABI, "operatorVersionCount", id)
			if readErr != nil || !versionCount.IsUint64() {
				return stateMismatchError(readErr, "superseded operator %d version count=%v", expected.NoID, versionCount)
			}
			operatorValues, callErr := contractCallAt(ctx, client, manifest.CoordinatorProxy, coordinatorABI, "operatorAt", head.Number, id, currentEpoch)
			if callErr != nil || len(operatorValues) != 1 {
				return stateMismatchError(callErr, "read superseded operator %d: got %d values", expected.NoID, len(operatorValues))
			}
			coldkey, coldErr := decodeHex32("superseded operator coldkey", tupleHex(operatorValues[0], "Coldkey"))
			poolHotkey, poolErr := decodeHex32("superseded operator pool hotkey", tupleHex(operatorValues[0], "PoolHotkey"))
			depositHotkey, depositErr := decodeHex32("superseded operator deposit hotkey", tupleHex(operatorValues[0], "DepositHotkey"))
			depositSignerText, rootSignerText := tupleAddress(operatorValues[0], "DepositSigner"), tupleAddress(operatorValues[0], "RootSigner")
			if coldErr != nil || poolErr != nil || depositErr != nil || !common.IsHexAddress(depositSignerText) || !common.IsHexAddress(rootSignerText) {
				return stateMismatchError(errors.Join(coldErr, poolErr, depositErr), "decode superseded operator %d identity", expected.NoID)
			}
			poolValues, callErr := contractCallAt(ctx, client, manifest.SettlementVault, vaultABI, "pools", head.Number, id)
			if callErr != nil || len(poolValues) != 3 {
				return stateMismatchError(callErr, "read superseded operator %d pool: got %d values", expected.NoID, len(poolValues))
			}
			vaultPoolHotkey, poolDecodeErr := decodeHex32("superseded vault pool hotkey", valueHex(poolValues[0]))
			if poolDecodeErr != nil || vaultPoolHotkey != poolHotkey {
				return stateMismatchError(poolDecodeErr, "superseded operator %d coordinator/vault pool hotkeys differ", expected.NoID)
			}
			poolStakeValues, callErr := contractCallAt(
				ctx, client, stakingPrecompileAddress, stakingABI, "getStake", head.Number,
				poolHotkey, vaultColdkey, new(big.Int).SetUint64(uint64(cfg.Netuid)),
			)
			if callErr != nil || len(poolStakeValues) != 1 {
				return stateMismatchError(callErr, "read superseded operator %d pool stake: got %d values", expected.NoID, len(poolStakeValues))
			}
			poolStake, ok := poolStakeValues[0].(*big.Int)
			if !ok || poolStake.Sign() < 0 {
				return fmt.Errorf("decode superseded operator %d pool stake: %T", expected.NoID, poolStakeValues[0])
			}
			nextNonce, readErr := readUint(manifest.CoordinatorProxy, coordinatorABI, "nextDepositNonce", id)
			if readErr != nil {
				return readErr
			}
			conviction, readErr := readUint(manifest.CoordinatorProxy, coordinatorABI, "cumulativeConviction", id)
			if readErr != nil {
				return readErr
			}
			carry, readErr := readUint(manifest.SettlementVault, vaultABI, "carry", id)
			if readErr != nil {
				return readErr
			}
			operatorPrincipal, readErr := readUint(manifest.ReserveSink, reserveABI, "operatorPrincipal", id)
			if readErr != nil {
				return readErr
			}
			observation.Operators = append(observation.Operators, supersededOperatorObservation{
				Identity: supersededOperatorIdentity{
					NoID: expected.NoID, Coldkey: coldkey, PoolHotkey: poolHotkey, DepositHotkey: depositHotkey,
					DepositSigner: common.HexToAddress(depositSignerText), RootSigner: common.HexToAddress(rootSignerText),
				},
				VersionCount: versionCount.Uint64(), Active: tupleBool(operatorValues[0], "Active"),
				PoolUID: valueUint16(poolValues[1]), PoolActive: valueBool(poolValues[2]),
				NextDepositNonce: nextNonce, CumulativeConviction: conviction, Carry: carry,
				ReservePrincipal: operatorPrincipal, PoolLiveStake: new(big.Int).Set(poolStake),
			})
		}
	}
	return validateSupersededDeploymentObservation(manifest, upgrade, expectedOperators, current, observation)
}

// Sum only exact verified effects retired with the active immutable
// deployment. Older role funding and unrelated registrations are reconciled by
// their own revision logic and must not be mistaken for superseded spend.
func supersededVerifiedSpend(prior *SetupPlan, entries []JournalEntry) (Spend, error) {
	if prior == nil {
		return Spend{}, errors.New("prior deployment plan is unavailable")
	}
	result := prior.SupersededSpend
	actionIDs := abandonableDeploymentActions
	if planUsesCoordinatorUpgradeEnvelope(prior.Schema) {
		actionIDs = append([]string(nil), supersedableDeploymentEffectActions...)
		// Pool registration is a contract-owned native side effect of the old
		// coordinator generation. Count each exact verified operator action in
		// addition to the fixed install sequence; operator alpha transfers are
		// preserved in place and therefore are not superseded spend.
		for _, action := range prior.Actions {
			if strings.HasPrefix(action.ID, "operator.register.") {
				actionIDs = append(actionIDs, action.ID)
			}
		}
	}
	priorActions := make(map[string]Action, len(actionIDs))
	for _, action := range prior.Actions {
		for _, actionID := range actionIDs {
			if action.ID != actionID {
				continue
			}
			if _, duplicate := priorActions[action.ID]; duplicate {
				return Spend{}, fmt.Errorf("prior plan has duplicate abandoned deployment action %s", action.ID)
			}
			priorActions[action.ID] = action
		}
	}
	verified := map[string]bool{}
	allowedPlans := prior.allowedPlanHashes()
	for _, entry := range entries {
		if allowedPlans[entry.PlanHash] && entry.Stage == StageVerified {
			verified[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	for _, actionID := range actionIDs {
		action, found := priorActions[actionID]
		if !found {
			return Spend{}, fmt.Errorf("prior plan has no abandoned deployment action %s", actionID)
		}
		key := action.ID + "\x00" + action.IntentHash
		if !verified[key] {
			continue
		}
		var err error
		result, err = addSpends(result, action.Spend)
		if err != nil {
			return Spend{}, fmt.Errorf("sum superseded action %s: %w", action.ID, err)
		}
	}
	return result, nil
}

// Charge superseded EVM reservations against the flexible live-campaign
// reserve. Superseded pool-registration counts and verified operator alpha
// from a pre-campaign migration remain explicit so every cumulative budget
// dimension stays approval-bound.
func applySupersededSpend(plan *SetupPlan, spend Spend) error {
	if plan == nil {
		return errors.New("replacement plan is unavailable")
	}
	if spend.TAORao != 0 || spend.SubnetCreations != 0 {
		return fmt.Errorf("automatic plan recovery cannot supersede TAO or subnet creations: %+v", spend)
	}
	if spend.EVMGasWei.IsZero() {
		plan.SupersededSpend = spend
		return nil
	}
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != "campaign.evm-gas-reserve" {
			continue
		}
		remaining, err := subtractDecimalUint(action.Spend.EVMGasWei, spend.EVMGasWei)
		if err != nil || remaining.IsZero() {
			return stateMismatchError(err, "superseded EVM spend %s exhausts campaign reserve %s", spend.EVMGasWei, action.Spend.EVMGasWei)
		}
		action.Spend.EVMGasWei = remaining
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
		plan.SupersededSpend = spend
		plan.MaximumSpend, err = maximumActionSpend(plan.Actions)
		return err
	}
	return errors.New("replacement plan has no live-campaign EVM reserve")
}

// Shrink only the fungible live-campaign reserve when carried historical gas
// ceilings make cumulative active-plus-superseded approval exceed the cap.
// Executable transaction ceilings are never rewritten to make a plan fit.
func trimLiveCampaignEVMReserveToLimit(plan *SetupPlan) error {
	if plan == nil {
		return errors.New("replacement plan is unavailable")
	}
	total, err := addSpends(plan.MaximumSpend, plan.SupersededSpend)
	if err != nil {
		return err
	}
	overComparison, err := total.EVMGasWei.Cmp(plan.Limits.EVMGasWei)
	if err != nil || overComparison <= 0 {
		return err
	}
	over, err := subtractDecimalUint(total.EVMGasWei, plan.Limits.EVMGasWei)
	if err != nil {
		return err
	}
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID != "campaign.evm-gas-reserve" || action.Kind != "budget-reserve" {
			continue
		}
		remaining, subtractErr := subtractDecimalUint(action.Spend.EVMGasWei, over)
		if subtractErr != nil || remaining.IsZero() {
			return stateMismatchError(subtractErr, "carried EVM ceiling excess %s exhausts campaign reserve %s", over, action.Spend.EVMGasWei)
		}
		action.Spend.EVMGasWei = remaining
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
		plan.MaximumSpend, err = maximumActionSpend(plan.Actions)
		return err
	}
	return errors.New("replacement plan has no live-campaign EVM reserve to trim")
}

// Find transaction-bearing ancestor intents that never reached durable
// postcondition verification. Each one needs a canonical failure proof before
// a revised plan may create another transaction for the same logical action.
func pendingPlanRevisionTransactions(prior *SetupPlan, entries []JournalEntry) ([]planRevisionTransaction, error) {
	if prior == nil {
		return nil, errors.New("prior plan is unavailable")
	}
	type transactionState struct {
		transaction planRevisionTransaction
		verified    bool
	}
	states := map[string]*transactionState{}
	allowedPlans := prior.allowedPlanHashes()
	for _, entry := range entries {
		if !allowedPlans[entry.PlanHash] {
			continue
		}
		key := entry.PlanHash + "\x00" + entry.ActionID + "\x00" + entry.IntentHash
		state := states[key]
		if state == nil {
			state = &transactionState{transaction: planRevisionTransaction{PlanHash: entry.PlanHash, ActionID: entry.ActionID, IntentHash: entry.IntentHash}}
			states[key] = state
		}
		if entry.Stage == StageVerified {
			state.verified = true
		}
		if entry.TransactionHash == "" {
			continue
		}
		if state.transaction.TransactionHash != "" && !strings.EqualFold(state.transaction.TransactionHash, entry.TransactionHash) {
			return nil, fmt.Errorf("plan %s action %s intent %s has multiple transactions", entry.PlanHash, entry.ActionID, entry.IntentHash)
		}
		state.transaction.TransactionHash = entry.TransactionHash
		if entry.Sequence > state.transaction.JournalSequence {
			state.transaction.JournalSequence = entry.Sequence
		}
		if entry.Signer != "" {
			if state.transaction.Signer != "" && state.transaction.Signer != entry.Signer {
				return nil, fmt.Errorf("plan %s action %s intent %s has multiple transaction signers", entry.PlanHash, entry.ActionID, entry.IntentHash)
			}
			state.transaction.Signer = entry.Signer
		}
		if entry.Nonce != "" {
			if state.transaction.Nonce != "" && state.transaction.Nonce != entry.Nonce {
				return nil, fmt.Errorf("plan %s action %s intent %s has multiple transaction nonces", entry.PlanHash, entry.ActionID, entry.IntentHash)
			}
			state.transaction.Nonce = entry.Nonce
		}
		if entry.RecoveryBlock != 0 {
			state.transaction.RecoveryBlock = entry.RecoveryBlock
			state.transaction.RecoveryBlockHash = entry.RecoveryBlockHash
		}
		if entry.BlockNumber != 0 {
			state.transaction.BlockNumber = entry.BlockNumber
			state.transaction.BlockHash = entry.BlockHash
		}
	}
	var pending []planRevisionTransaction
	for _, state := range states {
		if !state.verified && state.transaction.TransactionHash != "" {
			pending = append(pending, state.transaction)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].PlanHash != pending[j].PlanHash {
			return pending[i].PlanHash < pending[j].PlanHash
		}
		if pending[i].ActionID != pending[j].ActionID {
			return pending[i].ActionID < pending[j].ActionID
		}
		return pending[i].IntentHash < pending[j].IntentHash
	})
	return pending, nil
}

// Prove that one prior native transaction is finalized and dispatch-failed.
func validateFailedSubstrateRevisionTransaction(ctx context.Context, chain *crv4.Chain, transaction planRevisionTransaction) error {
	txHash, err := substrateTypes.NewHashFromHexString(transaction.TransactionHash)
	if err != nil {
		return err
	}
	proveFailure := func(blockHash substrateTypes.Hash) error {
		verificationErr := chain.VerifyFinalizedExtrinsic(blockHash, txHash)
		var dispatchError *crv4.FinalizedDispatchError
		if errors.As(verificationErr, &dispatchError) {
			return nil
		}
		if verificationErr == nil {
			return errPriorNativeTransactionSucceeded
		}
		return fmt.Errorf("prior native transaction outcome is not a canonical dispatch failure: %w", verificationErr)
	}
	if transaction.BlockNumber != 0 && transaction.BlockHash != "" {
		blockHash, hashErr := substrateTypes.NewHashFromHexString(transaction.BlockHash)
		if hashErr != nil {
			return hashErr
		}
		finalizedHash, finalizedErr := chain.API.RPC.Chain.GetFinalizedHead()
		if finalizedErr != nil {
			return finalizedErr
		}
		finalizedHeader, finalizedErr := chain.API.RPC.Chain.GetHeader(finalizedHash)
		if finalizedErr != nil {
			return finalizedErr
		}
		canonicalHash, canonicalErr := chain.API.RPC.Chain.GetBlockHash(transaction.BlockNumber)
		if canonicalErr != nil {
			return canonicalErr
		}
		if uint64(finalizedHeader.Number) < transaction.BlockNumber || canonicalHash != blockHash {
			return errors.New("prior native transaction inclusion is not canonical and finalized")
		}
		return proveFailure(blockHash)
	}
	if transaction.RecoveryBlock == 0 {
		return errors.New("prior native transaction has no recovery checkpoint")
	}
	_, found, findErr := chain.FindFinalizedExtrinsic(ctx, txHash, transaction.RecoveryBlock)
	var dispatchError *crv4.FinalizedDispatchError
	if errors.As(findErr, &dispatchError) {
		return nil
	}
	if findErr != nil {
		return fmt.Errorf("scan prior native transaction: %w", findErr)
	}
	if found {
		return errPriorNativeTransactionSucceeded
	}
	return errors.New("prior native transaction is absent from finalized history and may still be pending")
}

func recoverableFinalizedAlphaTransaction(prior *SetupPlan, entries []JournalEntry, transaction planRevisionTransaction) bool {
	if prior == nil || transaction.BlockNumber == 0 || transaction.BlockHash == "" || transaction.TransactionHash == "" {
		return false
	}
	var exactAction bool
	for _, action := range prior.Actions {
		if action.ID == transaction.ActionID && action.IntentHash == transaction.IntentHash && strings.HasPrefix(action.ID, "alpha.transfer.") && action.Kind == "substrate-extrinsic" && action.Spend.AlphaRao > 0 {
			exactAction = true
			break
		}
	}
	if !exactAction {
		return false
	}
	for _, entry := range entries {
		if entry.PlanHash == transaction.PlanHash && entry.ActionID == transaction.ActionID && entry.IntentHash == transaction.IntentHash && entry.Stage == StageFinalized && strings.EqualFold(entry.TransactionHash, transaction.TransactionHash) && entry.BlockNumber == transaction.BlockNumber && strings.EqualFold(entry.BlockHash, transaction.BlockHash) {
			return true
		}
	}
	return false
}

// Recognize a successful ancestor transfer only after a descendant plan has
// bound that exact transaction into a no-broadcast reconciliation and durably
// verified its bounded destination-share postcondition. This closes the
// revision lineage without treating an unrelated action with the same ID as
// verification of the original mutation.
func verifiedReconciliationForFinalizedAlphaTransaction(prior *SetupPlan, entries []JournalEntry, transaction planRevisionTransaction) bool {
	if prior == nil || transaction.BlockNumber == 0 || transaction.BlockHash == "" || transaction.TransactionHash == "" {
		return false
	}
	allowedPlans := prior.allowedPlanHashes()
	for _, action := range prior.Actions {
		if action.ID != transaction.ActionID || action.Kind != "substrate-reconciliation" || action.Parameters[alphaRecoveryPlanHashParameter] != transaction.PlanHash || action.Parameters[alphaRecoveryIntentHashParameter] != transaction.IntentHash || !strings.EqualFold(action.Parameters[alphaRecoveryTransactionHashParameter], transaction.TransactionHash) || action.Parameters[alphaRecoveryBlockParameter] != strconv.FormatUint(transaction.BlockNumber, 10) || !strings.EqualFold(action.Parameters[alphaRecoveryBlockHashParameter], transaction.BlockHash) {
			continue
		}
		if !hasFinalizedAlphaRecoveryEvidence(prior, action, entries) {
			return false
		}
		for _, entry := range entries {
			if allowedPlans[entry.PlanHash] && entry.ActionID == action.ID && entry.IntentHash == action.IntentHash && entry.Stage == StageVerified {
				return true
			}
		}
		return false
	}
	return false
}

// Prove that one prior EVM transaction has a canonical finalized revert.
func validateFailedEVMRevisionTransaction(ctx context.Context, client *ethclient.Client, transaction planRevisionTransaction) error {
	if client == nil {
		return errors.New("prior EVM transaction client is unavailable")
	}
	return validateFailedEVMRevisionTransactionFromReader(ctx, ethEVMReceiptFinalityReader{client: client}, transaction)
}

// Keep outcome classification independently testable from a concrete RPC
// transport so pending, orphaned, reverted, and successful receipts remain
// distinct fail-closed states.
func canonicalFinalizedEVMRevisionReceiptFromReader(ctx context.Context, reader evmReceiptFinalityReader, transaction planRevisionTransaction) (*ethTypes.Receipt, error) {
	receipt, ready, err := observeEVMReceiptFinality(ctx, reader, common.HexToHash(transaction.TransactionHash))
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, errors.New("prior EVM transaction has no canonical receipt and may still be pending")
	}
	if !ready {
		return nil, errors.New("prior EVM transaction receipt is not canonical and finalized")
	}
	if transaction.BlockNumber != 0 && (receipt.BlockNumber.Uint64() != transaction.BlockNumber || !strings.EqualFold(receipt.BlockHash.Hex(), transaction.BlockHash)) {
		return nil, errors.New("prior EVM receipt does not match its journaled inclusion")
	}
	return receipt, nil
}

// Classify only a canonical finalized receipt as a generic retryable revert.
func validateFailedEVMRevisionTransactionFromReader(ctx context.Context, reader evmReceiptFinalityReader, transaction planRevisionTransaction) error {
	receipt, err := canonicalFinalizedEVMRevisionReceiptFromReader(ctx, reader, transaction)
	if err != nil {
		return err
	}
	if receipt.Status == ethTypes.ReceiptStatusSuccessful {
		return errPriorEVMTransactionSucceeded
	}
	if receipt.Status != ethTypes.ReceiptStatusFailed {
		return fmt.Errorf("prior EVM transaction has unsupported receipt status %d", receipt.Status)
	}
	return nil
}

// Require a chain-proven revert for every unverified transaction in the plan
// lineage. A missing artifact, pending transaction, successful mutation, or
// observer error blocks revision rather than risking a duplicate side effect.
func planRevisionTransactionRecoveries(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry) (planRevisionRecoveries, error) {
	pending, err := pendingPlanRevisionTransactions(prior, entries)
	if err != nil || len(pending) == 0 {
		return planRevisionRecoveries{}, err
	}
	recoveries := planRevisionRecoveries{}
	var substrateChain *crv4.Chain
	var evmClient *ethclient.Client
	defer func() {
		if substrateChain != nil {
			substrateChain.API.Client.Close()
		}
		if evmClient != nil {
			evmClient.Close()
		}
	}()
	for _, transaction := range pending {
		stem := stringsTrim0x(transaction.TransactionHash)
		scalePath := filepath.Join(stateDir, "transactions", stem+".scale")
		rlpPath := filepath.Join(stateDir, "transactions", stem+".rlp")
		_, scaleErr := os.Stat(scalePath)
		_, rlpErr := os.Stat(rlpPath)
		hasScale := scaleErr == nil
		hasRLP := rlpErr == nil
		if (scaleErr != nil && !errors.Is(scaleErr, os.ErrNotExist)) || (rlpErr != nil && !errors.Is(rlpErr, os.ErrNotExist)) {
			return planRevisionRecoveries{}, errors.Join(scaleErr, rlpErr)
		}
		if hasScale == hasRLP {
			return planRevisionRecoveries{}, fmt.Errorf("prior transaction %s must have exactly one native SCALE or EVM RLP artifact", transaction.TransactionHash)
		}
		if hasScale {
			raw, readErr := os.ReadFile(scalePath)
			if readErr != nil {
				return planRevisionRecoveries{}, readErr
			}
			digest := blake2b.Sum256(raw)
			if !strings.EqualFold(substrateTypes.Hash(digest).Hex(), transaction.TransactionHash) {
				return planRevisionRecoveries{}, fmt.Errorf("prior native transaction artifact hash does not match %s", transaction.TransactionHash)
			}
			if substrateChain == nil {
				substrateChain, err = crv4.DialChain(cfg.OperationalSubstrate)
				if err != nil {
					return planRevisionRecoveries{}, err
				}
			}
			if err := validateFailedSubstrateRevisionTransaction(ctx, substrateChain, transaction); err != nil {
				// Runtime dispatch succeeded, but the observer rejected a bounded
				// share-pool postcondition. A v8 plan may reconcile this exact
				// finalized transfer locally and, if necessary, execute only a
				// separately bounded minimum-size repair. No duplicate allocation is
				// permitted for any other successful unverified transaction.
				if errors.Is(err, errPriorNativeTransactionSucceeded) {
					if recoverableFinalizedAlphaTransaction(prior, entries, transaction) || verifiedReconciliationForFinalizedAlphaTransaction(prior, entries, transaction) {
						continue
					}
					recovery, recoveryErr := detectFinalizedSubstrateFundingRecovery(cfg, stateDir, prior, entries, substrateChain, raw, transaction)
					if recoveryErr == nil {
						recoveries.SubstrateFundings = append(recoveries.SubstrateFundings, recovery)
						continue
					}
					return planRevisionRecoveries{}, fmt.Errorf("plan %s action %s: %w: %v", transaction.PlanHash, transaction.ActionID, errPriorNativeTransactionSucceeded, recoveryErr)
				}
				return planRevisionRecoveries{}, fmt.Errorf("plan %s action %s: %w", transaction.PlanHash, transaction.ActionID, err)
			}
			continue
		}
		raw, readErr := os.ReadFile(rlpPath)
		if readErr != nil {
			return planRevisionRecoveries{}, readErr
		}
		var signed ethTypes.Transaction
		if decodeErr := signed.UnmarshalBinary(raw); decodeErr != nil {
			return planRevisionRecoveries{}, fmt.Errorf("decode prior EVM transaction artifact %s: %w", transaction.TransactionHash, decodeErr)
		}
		if !strings.EqualFold(signed.Hash().Hex(), transaction.TransactionHash) {
			return planRevisionRecoveries{}, fmt.Errorf("prior EVM transaction artifact hash does not match %s", transaction.TransactionHash)
		}
		if evmClient == nil {
			evmClient, err = ethclient.DialContext(ctx, cfg.OperationalEVM)
			if err != nil {
				return planRevisionRecoveries{}, err
			}
		}
		receipt, receiptErr := canonicalFinalizedEVMRevisionReceiptFromReader(ctx, ethEVMReceiptFinalityReader{client: evmClient}, transaction)
		if receiptErr != nil {
			return planRevisionRecoveries{}, fmt.Errorf("plan %s action %s: %w", transaction.PlanHash, transaction.ActionID, receiptErr)
		}
		if receipt.Status == ethTypes.ReceiptStatusFailed {
			continue
		}
		if receipt.Status != ethTypes.ReceiptStatusSuccessful {
			return planRevisionRecoveries{}, fmt.Errorf("plan %s action %s: prior EVM transaction has unsupported receipt status %d", transaction.PlanHash, transaction.ActionID, receipt.Status)
		}
		switch {
		case transaction.ActionID == voluntaryConvictionActionID:
			recovery, recoveryErr := detectVoluntaryConvictionDuplicateRecovery(ctx, cfg, stateDir, prior, entries, evmClient, &signed, transaction)
			if recoveryErr != nil {
				return planRevisionRecoveries{}, fmt.Errorf("plan %s action %s: %w: %v", transaction.PlanHash, transaction.ActionID, errPriorEVMTransactionSucceeded, recoveryErr)
			}
			recoveries.VoluntaryConvictions = append(recoveries.VoluntaryConvictions, recovery)
		case strings.HasPrefix(transaction.ActionID, "fleet.mirror."):
			if substrateChain == nil {
				substrateChain, err = crv4.DialChain(cfg.OperationalSubstrate)
				if err != nil {
					return planRevisionRecoveries{}, err
				}
			}
			recovery, recoveryErr := detectFinalizedFleetMirrorRecovery(ctx, cfg, stateDir, prior, entries, substrateChain, evmClient, &signed, receipt, transaction)
			if recoveryErr != nil {
				return planRevisionRecoveries{}, fmt.Errorf("plan %s action %s: %w: %v", transaction.PlanHash, transaction.ActionID, errPriorEVMTransactionSucceeded, recoveryErr)
			}
			recoveries.FleetMirrors = append(recoveries.FleetMirrors, recovery)
		default:
			return planRevisionRecoveries{}, fmt.Errorf("plan %s action %s: %w", transaction.PlanHash, transaction.ActionID, errPriorEVMTransactionSucceeded)
		}
	}
	if len(recoveries.VoluntaryConvictions) > 1 {
		return planRevisionRecoveries{}, fmt.Errorf("plan lineage has %d successful duplicate voluntary convictions; exactly one is recoverable", len(recoveries.VoluntaryConvictions))
	}
	return recoveries, nil
}

// Preserve the validation-only interface used by focused failure tests.
func validatePlanRevisionTransactions(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry) error {
	_, err := planRevisionTransactionRecoveries(ctx, cfg, stateDir, prior, entries)
	return err
}

// Derive the exact hotkey/coldkey pairs for controlled registration roles.
func expectedRegistrationIdentities(cfg *ResolvedConfig, prior *SetupPlan, activeGeneration uint64, roles *RoleSecrets, labels []string) (map[string]plannedRegistrationIdentity, error) {
	result := make(map[string]plannedRegistrationIdentity, len(labels))
	nativeColdkeys := map[string]string{}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		nativeColdkeys[churnHotkeyLabel(churn)] = churnColdkeyLabel(churn)
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		nativeColdkeys[fleetHotkeyLabel(fleet)] = fleetColdkeyLabel(fleet)
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		nativeColdkeys[validatorHotkeyLabel(validator)] = fmt.Sprintf("validator-%d-coldkey", validator)
	}
	vaultByGeneration := map[uint64]common.Address{}
	vaultNonceByGeneration := map[uint64]uint64{}
	for _, deployment := range prior.SupersededDeployments {
		if currentNonce, ok := vaultNonceByGeneration[deployment.RegistrationRoleGeneration]; !ok || deployment.InitialNonce > currentNonce {
			vaultByGeneration[deployment.RegistrationRoleGeneration] = deployment.SettlementVault
			vaultNonceByGeneration[deployment.RegistrationRoleGeneration] = deployment.InitialNonce
		}
	}
	// A partially deployed replacement can predate the generation field while
	// only its generation-independent reserve CREATE has finalized. The active
	// plan's vault address is nevertheless the only approved owner for the next
	// generation; executable-runtime compatibility later proves that the vault
	// constructor itself has not been consumed under the legacy identity.
	vaultByGeneration[activeGeneration] = prior.Deployment.SettlementVault
	for _, label := range labels {
		hotkey, err := roleBytes32(roles, label)
		if err != nil {
			return nil, err
		}
		var coldkey [32]byte
		if coldLabel := nativeColdkeys[label]; coldLabel != "" {
			coldkey, err = roleBytes32(roles, coldLabel)
		} else {
			for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
				if label == fmt.Sprintf("operator-%d-deposit-hotkey", operator) {
					address, addressErr := roles.EVMAddress(fmt.Sprintf("operator-%d-deposit", operator))
					if addressErr != nil {
						return nil, addressErr
					}
					coldkey = ss58Mirror(address)
				}
			}
			if coldkey == ([32]byte{}) {
				generation, _, _, contractRole := parseContractRegistrationRoleLabel(cfg.Config.Topology, label)
				vault, found := vaultByGeneration[generation]
				if !contractRole || !found || vault == (common.Address{}) {
					return nil, fmt.Errorf("registered role %s has no authenticated generation-%d vault coldkey", label, generation)
				}
				coldkey = ss58Mirror(vault)
			}
		}
		if err != nil {
			return nil, err
		}
		result[label] = plannedRegistrationIdentity{hotkey: hotkey, coldkey: coldkey}
	}
	return result, nil
}

// Compare one finalized UID fact to its deterministic role identity.
func registrationIdentityMatches(fact ExistingUIDFact, uid uint16, expected plannedRegistrationIdentity) bool {
	if fact.UID != uid || fact.RegistrationBlock == 0 || fact.SubnetOwner {
		return false
	}
	hotkey, hotkeyErr := decodeHex32("revision hotkey", fact.Hotkey)
	coldkey, coldkeyErr := decodeHex32("revision coldkey", fact.Coldkey)
	return hotkeyErr == nil && coldkeyErr == nil && hotkey == expected.hotkey && coldkey == expected.coldkey
}

// Preserve only immutable UID identity across a plan revision. Stake is a live
// economic observation that changes with emissions and transfers; v5 records
// it for validator sizing, but a pre-v5 zero must never look like a hotkey,
// ownership, or registration-history mutation.
func bootstrapUIDIdentityMatches(current, baseline ExistingUIDFact) bool {
	return current.UID == baseline.UID &&
		current.Hotkey == baseline.Hotkey &&
		current.Coldkey == baseline.Coldkey &&
		current.RegistrationBlock == baseline.RegistrationBlock &&
		current.SubnetOwner == baseline.SubnetOwner
}

// Require the current subnet to be either an exact prefix of the initial
// deterministic registration order or one of the bounded tournament states.
// Any external insertion, ownership change, gap, or unexpected prune aborts
// the revision before a new approval hash is produced.
func validatePlanRevisionTopology(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, roles *RoleSecrets) error {
	if cfg == nil || prior == nil || current == nil || roles == nil {
		return errors.New("plan revision topology inputs are incomplete")
	}
	baseline := prior.LiveFacts.ExistingUIDs
	if len(baseline) == 0 || int(prior.LiveFacts.ExistingUIDCount) != len(baseline) || int(current.ExistingUIDCount) != len(current.ExistingUIDs) || len(current.ExistingUIDs) < len(baseline) {
		return fmt.Errorf("plan revision has inconsistent baseline/current UID facts: baseline=%d/%d current=%d/%d", prior.LiveFacts.ExistingUIDCount, len(baseline), current.ExistingUIDCount, len(current.ExistingUIDs))
	}
	if current.SubnetOwnerHotkey != prior.LiveFacts.SubnetOwnerHotkey || current.UIDZeroHotkey != prior.LiveFacts.UIDZeroHotkey {
		return errors.New("plan revision changed the subnet owner or UID-zero hotkey")
	}
	for index := range baseline {
		if !bootstrapUIDIdentityMatches(current.ExistingUIDs[index], baseline[index]) {
			return fmt.Errorf("plan revision bootstrap UID %d changed: current=%+v baseline=%+v", index, current.ExistingUIDs[index], baseline[index])
		}
	}
	generation, err := contractRegistrationGenerationFromSupersededSpend(cfg.Config.Topology, prior)
	if err != nil {
		return err
	}
	initialLabels := baseInitialTopologyRoleLabels(cfg.Config.Topology)
	progress := len(current.ExistingUIDs) - len(baseline)
	if progress > len(initialLabels) {
		return fmt.Errorf("plan revision current controlled UIDs %d exceed initial topology %d", progress, len(initialLabels))
	}
	labelsNeeded := append([]string(nil), initialLabels[:progress]...)
	if progress == len(initialLabels) {
		for candidateGeneration := uint64(1); candidateGeneration <= generation; candidateGeneration++ {
			labelsNeeded = append(labelsNeeded, contractRegistrationRoleLabels(cfg.Config.Topology, candidateGeneration)...)
		}
		for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
			labelsNeeded = append(labelsNeeded, fleetHotkeyLabel(cfg.Config.Topology.HeadFleets+challenger))
		}
	}
	identities, err := expectedRegistrationIdentities(cfg, prior, generation, roles, labelsNeeded)
	if err != nil {
		return err
	}
	if progress < len(initialLabels) {
		if generation != 0 {
			return errors.New("replacement contract generation requires the prior full topology")
		}
		for index, label := range initialLabels[:progress] {
			uid := uint16(len(baseline) + index)
			if !registrationIdentityMatches(current.ExistingUIDs[len(baseline)+index], uid, identities[label]) {
				return fmt.Errorf("plan revision UID %d is not the expected registration prefix role %s", uid, label)
			}
		}
		return nil
	}
	contractProgresses := []int{0}
	if generation > 0 {
		contractProgresses = make([]int, contractRegistrationRoleCount(cfg.Config.Topology)+1)
		for index := range contractProgresses {
			contractProgresses[index] = index
		}
	}
	for _, contractProgress := range contractProgresses {
		maximumChallengers := 0
		if generation == 0 || contractProgress == contractRegistrationRoleCount(cfg.Config.Topology) {
			maximumChallengers = cfg.Config.Topology.ChallengerFleets
		}
		for replacements := 0; replacements <= maximumChallengers; replacements++ {
			candidate, candidateErr := topologyRoleLabelsAtProgress(cfg.Config.Topology, generation, contractProgress, replacements)
			if candidateErr != nil {
				return candidateErr
			}
			matched := true
			for index, label := range candidate {
				uid := uint16(len(baseline) + index)
				if !registrationIdentityMatches(current.ExistingUIDs[len(baseline)+index], uid, identities[label]) {
					matched = false
					break
				}
			}
			if matched {
				return nil
			}
		}
	}
	return errors.New("plan revision full topology is neither an approved contract-generation prefix nor a bounded challenger replacement state")
}

// Keep immutable deployment identity and deterministic roles across revisions.
func validatePlanRevisionIdentity(cfg *ResolvedConfig, prior *SetupPlan, roles PublicRoles) error {
	if cfg == nil || prior == nil {
		return errors.New("plan revision identity is unavailable")
	}
	if prior.Release != "1.0" || prior.DeploymentID != cfg.Config.Deployment.DeploymentID || prior.ChainID != testnetChainID || prior.GenesisHash != testnetGenesis || prior.Netuid != cfg.Netuid || prior.Owner != cfg.WalletPublic {
		return errors.New("prior plan does not describe the same release deployment, chain, owner, and netuid")
	}
	// readPersistedPlan authenticates every supported schema against its exact
	// historical wire representation before a revision reaches this function.
	// Re-marshalling an older plan through the current struct is not equivalent:
	// newly added non-omitempty fields would be inserted and change its digest.
	// Current-schema plans have the current representation, so retain this
	// defense in depth for native current-to-current revisions.
	if prior.Schema == currentSetupPlanSchema {
		priorHash, err := prior.hash()
		if err != nil || priorHash != prior.PlanHash {
			return errors.New("prior plan hash does not authenticate its revision identity")
		}
	}
	currentRoles, err := canonicalHashHex(roles)
	if err != nil {
		return err
	}
	priorRoles, err := canonicalHashHex(prior.Roles)
	if err != nil || currentRoles != priorRoles {
		return errors.New("prior plan deterministic public roles changed")
	}
	return validatePlanBudget(prior)
}

func callBytes32SelectorAt(ctx context.Context, client *ethclient.Client, address common.Address, signature string, block uint64) (string, error) {
	selector := crypto.Keccak256([]byte(signature))[:4]
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &address, Data: selector}, new(big.Int).SetUint64(block))
	if err != nil {
		return "", err
	}
	if len(result) != 32 {
		return "", fmt.Errorf("%s at %s returned %d bytes", signature, address, len(result))
	}
	return common.BytesToHash(result).Hex(), nil
}

// Rebind the release payload to an already-active upgrade only when its full
// address-specific runtime and the complete compatibility baseline are still
// exact. This prevents a Go-only release-lock revision from consuming another
// deployer nonce and activating byte-identical coordinator code. Any contract
// or baseline drift restores the new candidate nonce and follows the ordinary
// reviewed repeated-upgrade path.
func reuseExactActiveCoordinatorUpgrade(prior *SetupPlan, built *DeploymentPayloads) (bool, error) {
	if prior == nil || built == nil || !prior.CoordinatorUpgradeBaseline.isRepeated() {
		return false, nil
	}
	candidateNonce := built.CoordinatorUpgrade.DeployerNonce
	if err := configureCoordinatorUpgradeNonce(built, prior.CoordinatorUpgrade.DeployerNonce); err != nil {
		return false, err
	}
	batcherMatches := true
	var priorBatcher *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID != "fleet.refresh.deploy-batcher" {
			continue
		}
		if priorBatcher != nil {
			return false, errors.New("prior plan has duplicate fleet batcher deployment actions")
		}
		copy := prior.Actions[index]
		priorBatcher = &copy
	}
	if priorBatcher != nil {
		batcherMatches = priorBatcher.Kind == "evm-transaction" && common.IsHexAddress(priorBatcher.Target) &&
			common.HexToAddress(priorBatcher.Target) == built.FleetBatcherAddress &&
			strings.EqualFold(priorBatcher.Parameters["expected_created_address"], built.FleetBatcherAddress.Hex()) &&
			strings.EqualFold(priorBatcher.Parameters["runtime_code_hash"], crypto.Keccak256Hash(built.FleetBatcherRuntime).Hex())
	}
	reuse := batcherMatches && built.CoordinatorUpgrade == prior.CoordinatorUpgrade &&
		validateCoordinatorUpgradeBaselineRelease(prior.CoordinatorUpgradeBaseline, prior.Deployment, built.Manifest, built.CoordinatorUpgrade) == nil &&
		validateCoordinatorUpgradePayloadBaseline(prior.CoordinatorUpgradeBaseline, built) == nil
	if reuse {
		return true, nil
	}
	if err := configureCoordinatorUpgradeNonce(built, candidateNonce); err != nil {
		return false, err
	}
	return false, nil
}

func observeRepeatedCoordinatorUpgrade(ctx context.Context, cfg *ResolvedConfig, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, roles *RoleSecrets, built *DeploymentPayloads) (*coordinatorUpgradeMigration, error) {
	implementationVerified := exactVerifiedPlanAction(prior, entries, "evm.coordinator-upgrade-implementation")
	activationVerified := exactVerifiedPlanAction(prior, entries, "evm.coordinator-upgrade-activate")
	if !implementationVerified {
		return nil, errors.New("coordinator upgrade has no exact verified implementation intent")
	}
	expectedNonce, verifiedBatcher, boundaryErr := repeatedCoordinatorUpgradeBoundary(prior, entries)
	if boundaryErr != nil {
		return nil, boundaryErr
	}
	if current.DeployerNonce != expectedNonce {
		return nil, fmt.Errorf("repeated coordinator upgrade nonce=%d, want next authenticated nonce %d", current.DeployerNonce, expectedNonce)
	}
	continuingActivation := !activationVerified
	if continuingActivation && verifiedBatcher != nil {
		return nil, errors.New("fleet batcher was verified before its coordinator activation dependency")
	}
	upgradeNonce := current.DeployerNonce
	if continuingActivation {
		upgradeNonce = prior.CoordinatorUpgrade.DeployerNonce
	}
	if err := configureCoordinatorUpgradeNonce(built, upgradeNonce); err != nil {
		return nil, err
	}
	if continuingActivation && built.CoordinatorUpgrade != prior.CoordinatorUpgrade {
		return nil, errors.New("interrupted coordinator implementation differs from the current locked release")
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return nil, err
	}
	if head.Number < current.EVMFinalizedBlock {
		return nil, fmt.Errorf("repeated-upgrade finalized head %d is behind facts %d", head.Number, current.EVMFinalizedBlock)
	}
	block := new(big.Int).SetUint64(head.Number)
	deployer, err := roles.EVMAddress("deployer")
	if err != nil {
		return nil, err
	}
	nonce, err := client.NonceAt(ctx, deployer, block)
	if err != nil || nonce != current.DeployerNonce {
		return nil, stateMismatchError(err, "repeated-upgrade deployer nonce=%d want=%d", nonce, current.DeployerNonce)
	}
	if verifiedBatcher != nil {
		batcherAddress := common.HexToAddress(verifiedBatcher.Target)
		code, codeErr := client.CodeAt(ctx, batcherAddress, block)
		if codeErr != nil {
			return nil, fmt.Errorf("read verified fleet batcher runtime: %w", codeErr)
		}
		if got, want := crypto.Keccak256Hash(code).Hex(), verifiedBatcher.Parameters["runtime_code_hash"]; len(code) == 0 || !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("verified fleet batcher runtime mismatch at %s: got=%s want=%s", batcherAddress, got, want)
		}
	}
	priorHashes, priorErr := normalizedDeploymentRuntimeHashes(prior.Deployment)
	builtHashes, builtErr := normalizedDeploymentRuntimeHashes(built.Manifest)
	if priorErr != nil || builtErr != nil || len(priorHashes) != 6 || len(builtHashes) != 6 {
		return nil, errors.New("active or release deployment runtime manifest is incomplete")
	}
	codeByAddress := map[common.Address][]byte{}
	for _, address := range contractDeploymentAddresses(prior.Deployment) {
		code, codeErr := client.CodeAt(ctx, address, block)
		if codeErr != nil {
			return nil, fmt.Errorf("read active immutable runtime at %s: %w", address, codeErr)
		}
		if got, want := crypto.Keccak256Hash(code).Hex(), priorHashes[address]; len(code) == 0 || !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("active immutable runtime mismatch at %s: got=%s want=%s", address, got, want)
		}
		codeByAddress[address] = code
	}
	implementationSlot, err := client.StorageAt(ctx, prior.Deployment.CoordinatorProxy, common.HexToHash(erc1967ImplementationSlot), block)
	if err != nil || len(implementationSlot) != 32 {
		return nil, stateMismatchError(err, "read active coordinator implementation slot length=%d", len(implementationSlot))
	}
	activeImplementation := common.BytesToAddress(implementationSlot[12:])
	activeCode, err := client.CodeAt(ctx, activeImplementation, block)
	if err != nil {
		return nil, err
	}
	activeHash := crypto.Keccak256Hash(activeCode).Hex()
	if continuingActivation {
		baselineAddress := prior.Deployment.CoordinatorImplementation
		baselineHash := priorHashes[baselineAddress]
		if prior.CoordinatorUpgradeBaseline.isRepeated() {
			baselineAddress = common.HexToAddress(prior.CoordinatorUpgradeBaseline.ActiveImplementation)
			baselineHash = prior.CoordinatorUpgradeBaseline.ActiveImplementationHash
		}
		if activeImplementation != baselineAddress || len(activeCode) == 0 || !strings.EqualFold(activeHash, baselineHash) {
			return nil, fmt.Errorf("interrupted activation proxy/runtime=%s/%s want baseline %s/%s", activeImplementation, activeHash, baselineAddress, baselineHash)
		}
	} else if activeImplementation != prior.CoordinatorUpgrade.Implementation || len(activeCode) == 0 || !strings.EqualFold(activeHash, prior.CoordinatorUpgrade.RuntimeCodeHash) {
		return nil, fmt.Errorf("active coordinator runtime=%s/%s want prior approved %s/%s", activeImplementation, activeHash, prior.CoordinatorUpgrade.Implementation, prior.CoordinatorUpgrade.RuntimeCodeHash)
	}
	if !continuingActivation {
		reuse, reuseErr := reuseExactActiveCoordinatorUpgrade(prior, built)
		if reuseErr != nil {
			return nil, reuseErr
		}
		if reuse {
			return &coordinatorUpgradeMigration{Deployment: prior.Deployment, Baseline: prior.CoordinatorUpgradeBaseline, Upgrade: prior.CoordinatorUpgrade}, nil
		}
	}
	newCode, err := client.CodeAt(ctx, built.CoordinatorUpgrade.Implementation, block)
	if err != nil {
		return nil, err
	}
	if continuingActivation {
		if got := crypto.Keccak256Hash(newCode).Hex(); len(newCode) == 0 || !strings.EqualFold(got, built.CoordinatorUpgrade.RuntimeCodeHash) {
			return nil, fmt.Errorf("interrupted coordinator implementation runtime=%s want=%s", got, built.CoordinatorUpgrade.RuntimeCodeHash)
		}
	} else if len(newCode) != 0 {
		return nil, fmt.Errorf("repeated coordinator upgrade CREATE address already has %d runtime bytes", len(newCode))
	}

	proxyExecutable, err := matchingNormalizedSolidityExecutableHash("coordinator proxy", codeByAddress[prior.Deployment.CoordinatorProxy], built.ExpectedRuntime[built.Manifest.CoordinatorProxy], artifactByName("ERC1967Proxy"))
	if err != nil {
		return nil, err
	}
	reserveExecutable, err := matchingNormalizedSolidityExecutableHash("reserve sink", codeByAddress[prior.Deployment.ReserveSink], built.ExpectedRuntime[built.Manifest.ReserveSink], artifactByName("ReserveSink"))
	if err != nil {
		return nil, err
	}
	vaultExecutable, err := matchingNormalizedSolidityExecutableHash("settlement vault", codeByAddress[prior.Deployment.SettlementVault], built.ExpectedRuntime[built.Manifest.SettlementVault], artifactByName("SettlementVault"))
	if err != nil {
		return nil, err
	}
	probeExecutable, err := matchingNormalizedSolidityExecutableHash("precompile probe", codeByAddress[prior.Deployment.PrecompileProbe], built.ExpectedRuntime[built.Manifest.PrecompileProbe], TestnetPrecompileProbeArtifact)
	if err != nil {
		return nil, err
	}
	version, err := callBytes32SelectorAt(ctx, client, prior.Deployment.GovernanceDrillImplementation, "DRILL_VERSION()", head.Number)
	if err != nil {
		return nil, fmt.Errorf("active governance drill version: %w", err)
	}
	uuid, err := callBytes32SelectorAt(ctx, client, prior.Deployment.GovernanceDrillImplementation, "proxiableUUID()", head.Number)
	if err != nil {
		return nil, fmt.Errorf("active governance drill UUPS UUID: %w", err)
	}
	priorHash, err := contractDeploymentIdentityHash(prior.Deployment)
	if err != nil {
		return nil, err
	}
	releaseHash, err := contractDeploymentIdentityHash(built.Manifest)
	if err != nil {
		return nil, err
	}
	if continuingActivation {
		baseline := prior.CoordinatorUpgradeBaseline
		if baseline.isZero() {
			return nil, errors.New("interrupted coordinator activation has no compatibility baseline")
		}
		if err := validateCoordinatorUpgradeBaselineRelease(baseline, prior.Deployment, built.Manifest, built.CoordinatorUpgrade); err != nil {
			return nil, err
		}
		if err := validateCoordinatorUpgradePayloadBaseline(baseline, built); err != nil {
			return nil, err
		}
		return &coordinatorUpgradeMigration{Deployment: prior.Deployment, Baseline: baseline, Upgrade: built.CoordinatorUpgrade}, nil
	}
	baseline := CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v3", PriorDeploymentHash: priorHash,
		ReleaseDeploymentHash: releaseHash, ReboundDeploymentHash: priorHash,
		ReserveSinkExecutableHash: reserveExecutable, SettlementVaultExecutableHash: vaultExecutable,
		GovernanceDrillVersion: version, GovernanceProxiableUUID: uuid,
		DeployerNonce: current.DeployerNonce, ProbeAddressEmpty: false,
		ActiveImplementation: activeImplementation.Hex(), ActiveImplementationHash: activeHash,
		PrecompileProbeExecutableHash:  probeExecutable,
		CoordinatorProxyExecutableHash: proxyExecutable,
		FinalizedBlock:                 head.Number, FinalizedBlockHash: head.Hash,
	}
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, prior.Deployment, built.Manifest, built.CoordinatorUpgrade); err != nil {
		return nil, err
	}
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, built); err != nil {
		return nil, err
	}
	return &coordinatorUpgradeMigration{Deployment: prior.Deployment, Baseline: baseline, Upgrade: built.CoordinatorUpgrade}, nil
}

// Observe the one supported in-place migration shape at one finalized head:
// the authenticated v5 deployment is complete through nonce+7, nonce+8 is
// still empty for the current probe, and the release repair will consume
// nonce+9. Full legacy hashes are checked first; only sink/vault executable
// equivalence ignores constructor words and Solidity metadata.
func observeCoordinatorUpgradeMigration(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, roles *RoleSecrets) (*coordinatorUpgradeMigration, error) {
	if prior == nil || current == nil || roles == nil || !planUsesAlphaTransferEnvelope(prior.Schema) {
		return nil, nil
	}
	existing, err := loadContractDeployment(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	built, err := buildDeploymentPayloadsWithRegistrationGeneration(cfg, roles, existing.InitialNonce, prior.Deployment.RegistrationRoleGeneration)
	if err != nil {
		return nil, err
	}
	if contractDeploymentAddressesEqual(*existing, built.Manifest) && contractDeploymentRuntimeHashesCompatible(*existing, built.Manifest) {
		return nil, nil
	}
	if contractDeploymentUpgradeBaselineCompatible(contractDeploymentIdentity(prior.Deployment), built.Manifest) && contractDeploymentAddressesEqual(*existing, prior.Deployment) && contractDeploymentRuntimeHashesCompatible(*existing, prior.Deployment) {
		return nil, nil
	}
	if !contractDeploymentAddressesEqual(prior.Deployment, built.Manifest) || !contractDeploymentAddressesEqual(*existing, prior.Deployment) || !contractDeploymentRuntimeHashesCompatible(*existing, prior.Deployment) {
		return nil, errors.New("legacy deployment addresses or recorded runtime hashes differ from the authenticated prior plan")
	}
	if (prior.CoordinatorUpgrade.Schema == "urnetwork-coordinator-upgrade-v1" || prior.CoordinatorUpgrade.Schema == "urnetwork-coordinator-upgrade-v2") && prior.CoordinatorUpgrade.DeployerNonce != ^uint64(0) && current.DeployerNonce > prior.CoordinatorUpgrade.DeployerNonce {
		return observeRepeatedCoordinatorUpgrade(ctx, cfg, prior, current, entries, roles, built)
	}
	if prior.Deployment.InitialNonce > ^uint64(0)-9 {
		return nil, errors.New("legacy deployment nonce range overflows")
	}
	probeBoundary := prior.Deployment.InitialNonce + 8
	probeAlreadyDeployed := !prior.CoordinatorUpgradeBaseline.isZero() && current.DeployerNonce == prior.CoordinatorUpgrade.DeployerNonce
	if current.DeployerNonce != probeBoundary && !probeAlreadyDeployed {
		return nil, fmt.Errorf("legacy deployment nonce=%d, want probe boundary %d or post-probe boundary %d", current.DeployerNonce, probeBoundary, prior.CoordinatorUpgrade.DeployerNonce)
	}
	probeIntent := ""
	for _, action := range prior.Actions {
		if action.ID == "precompile.probe-deploy" {
			probeIntent = action.IntentHash
			break
		}
	}
	probeVerified := false
	for _, entry := range entries {
		if !prior.allowedPlanHashes()[entry.PlanHash] || entry.ActionID != "precompile.probe-deploy" {
			continue
		}
		if entry.Stage == StageVerified && probeIntent != "" && entry.IntentHash == probeIntent {
			probeVerified = true
		}
		if !probeAlreadyDeployed && (entry.Stage == StageBroadcast || entry.Stage == StageFinalized || entry.Stage == StageVerified) {
			return nil, fmt.Errorf("legacy probe action already reached %s", entry.Stage)
		}
	}
	if probeAlreadyDeployed && !probeVerified {
		return nil, errors.New("post-probe upgrade baseline has no exact verified probe intent")
	}
	priorHashes, priorErr := normalizedDeploymentRuntimeHashes(prior.Deployment)
	builtHashes, builtErr := normalizedDeploymentRuntimeHashes(built.Manifest)
	if priorErr != nil || builtErr != nil || len(priorHashes) != 6 || len(builtHashes) != 6 {
		return nil, errors.New("legacy or release deployment runtime manifest is incomplete")
	}
	if !strings.EqualFold(priorHashes[prior.Deployment.CoordinatorProxy], builtHashes[built.Manifest.CoordinatorProxy]) {
		return nil, errors.New("immutable coordinator proxy runtime changed across the release")
	}

	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return nil, err
	}
	if head.Number < current.EVMFinalizedBlock {
		return nil, fmt.Errorf("upgrade baseline finalized head %d is behind facts %d", head.Number, current.EVMFinalizedBlock)
	}
	block := new(big.Int).SetUint64(head.Number)
	deployer, err := roles.EVMAddress("deployer")
	if err != nil {
		return nil, err
	}
	nonce, err := client.NonceAt(ctx, deployer, block)
	if err != nil || nonce != current.DeployerNonce {
		return nil, stateMismatchError(err, "upgrade baseline deployer nonce=%d want=%d", nonce, current.DeployerNonce)
	}
	codeByAddress := map[common.Address][]byte{}
	for _, address := range []common.Address{prior.Deployment.ReserveSink, prior.Deployment.SettlementVault, prior.Deployment.CoordinatorImplementation, prior.Deployment.CoordinatorProxy, prior.Deployment.GovernanceDrillImplementation} {
		code, codeErr := client.CodeAt(ctx, address, block)
		if codeErr != nil {
			return nil, fmt.Errorf("read legacy runtime at %s: %w", address, codeErr)
		}
		if got, want := crypto.Keccak256Hash(code).Hex(), priorHashes[address]; len(code) == 0 || !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("legacy runtime mismatch at %s: got=%s want=%s", address, got, want)
		}
		codeByAddress[address] = code
	}
	probeCode, err := client.CodeAt(ctx, prior.Deployment.PrecompileProbe, block)
	if err != nil {
		return nil, err
	}
	if !probeAlreadyDeployed && len(probeCode) != 0 {
		return nil, errors.New("legacy probe address is not empty at the finalized nonce boundary")
	}
	if probeAlreadyDeployed {
		probeHash := crypto.Keccak256Hash(probeCode).Hex()
		if len(probeCode) == 0 || !strings.EqualFold(probeHash, priorHashes[prior.Deployment.PrecompileProbe]) || !strings.EqualFold(probeHash, builtHashes[built.Manifest.PrecompileProbe]) {
			return nil, fmt.Errorf("finalized probe runtime mismatch: got=%s prior=%s release=%s", probeHash, priorHashes[prior.Deployment.PrecompileProbe], builtHashes[built.Manifest.PrecompileProbe])
		}
	}
	upgradeCode, err := client.CodeAt(ctx, built.CoordinatorUpgrade.Implementation, block)
	if err != nil {
		return nil, err
	}
	if len(upgradeCode) != 0 {
		return nil, errors.New("coordinator upgrade address is not empty before its approved CREATE")
	}

	reserveArtifact, vaultArtifact := artifactByName("ReserveSink"), artifactByName("SettlementVault")
	reserveLegacyHash, err := normalizedSolidityExecutableHash(codeByAddress[prior.Deployment.ReserveSink], reserveArtifact)
	if err != nil {
		return nil, fmt.Errorf("normalize legacy reserve sink: %w", err)
	}
	reserveReleaseHash, err := normalizedSolidityExecutableHash(built.ExpectedRuntime[built.Manifest.ReserveSink], reserveArtifact)
	if err != nil || reserveLegacyHash != reserveReleaseHash {
		return nil, stateMismatchError(err, "reserve sink executable changed: legacy=%s release=%s", reserveLegacyHash, reserveReleaseHash)
	}
	vaultLegacyHash, err := normalizedSolidityExecutableHash(codeByAddress[prior.Deployment.SettlementVault], vaultArtifact)
	if err != nil {
		return nil, fmt.Errorf("normalize legacy settlement vault: %w", err)
	}
	vaultReleaseHash, err := normalizedSolidityExecutableHash(built.ExpectedRuntime[built.Manifest.SettlementVault], vaultArtifact)
	if err != nil || vaultLegacyHash != vaultReleaseHash {
		return nil, stateMismatchError(err, "settlement vault executable changed: legacy=%s release=%s", vaultLegacyHash, vaultReleaseHash)
	}
	version, err := callBytes32SelectorAt(ctx, client, prior.Deployment.GovernanceDrillImplementation, "DRILL_VERSION()", head.Number)
	if err != nil {
		return nil, fmt.Errorf("legacy governance drill version: %w", err)
	}
	uuid, err := callBytes32SelectorAt(ctx, client, prior.Deployment.GovernanceDrillImplementation, "proxiableUUID()", head.Number)
	if err != nil {
		return nil, fmt.Errorf("legacy governance drill UUPS UUID: %w", err)
	}

	rebound := contractDeploymentIdentity(prior.Deployment)
	rebound.RuntimeHashes = cloneStrings(prior.Deployment.RuntimeHashes)
	rebound.RuntimeHashes[rebound.PrecompileProbe.Hex()] = built.Manifest.RuntimeHashes[built.Manifest.PrecompileProbe.Hex()]
	priorHash, err := contractDeploymentIdentityHash(prior.Deployment)
	if err != nil {
		return nil, err
	}
	releaseHash, err := contractDeploymentIdentityHash(built.Manifest)
	if err != nil {
		return nil, err
	}
	reboundHash, err := contractDeploymentIdentityHash(rebound)
	if err != nil {
		return nil, err
	}
	baseline := prior.CoordinatorUpgradeBaseline
	if baseline.isZero() {
		baseline = CoordinatorUpgradeBaseline{
			Schema: "urnetwork-coordinator-upgrade-baseline-v1", PriorDeploymentHash: priorHash,
			ReleaseDeploymentHash: releaseHash, ReboundDeploymentHash: reboundHash,
			ReserveSinkExecutableHash: reserveLegacyHash, SettlementVaultExecutableHash: vaultLegacyHash,
			GovernanceDrillVersion: version, GovernanceProxiableUUID: uuid,
			DeployerNonce: probeBoundary, ProbeAddressEmpty: true, FinalizedBlock: head.Number, FinalizedBlockHash: head.Hash,
		}
	} else if baseline.ReleaseDeploymentHash != releaseHash || baseline.ReboundDeploymentHash != reboundHash || baseline.ReserveSinkExecutableHash != reserveLegacyHash || baseline.SettlementVaultExecutableHash != vaultLegacyHash || !strings.EqualFold(baseline.GovernanceDrillVersion, version) || !strings.EqualFold(baseline.GovernanceProxiableUUID, uuid) {
		return nil, errors.New("authenticated coordinator upgrade baseline changed during continuation")
	}
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, rebound, built.Manifest, built.CoordinatorUpgrade); err != nil {
		return nil, err
	}
	return &coordinatorUpgradeMigration{Deployment: rebound, Baseline: baseline, Upgrade: built.CoordinatorUpgrade}, nil
}

func validatePolicyRevisionJournal(prior *SetupPlan, entries []JournalEntry) error {
	if prior == nil {
		return errors.New("prior policy plan is unavailable")
	}
	unsafe := map[string]bool{
		"campaign.voluntary-conviction.1": true,
		dishonestDepositActionID:          true,
		"production.schedule-policy":      true,
		"topology.launch":                 true,
	}
	allowedPlans := prior.allowedPlanHashes()
	for _, entry := range entries {
		if !allowedPlans[entry.PlanHash] || !unsafe[entry.ActionID] {
			continue
		}
		if entry.Stage == StageBroadcast || entry.Stage == StageFinalized || entry.Stage == StageVerified {
			return fmt.Errorf("policy revision is forbidden after %s reached %s", entry.ActionID, entry.Stage)
		}
	}
	return nil
}

func validateRegistrationRoleGenerationPromotion(cfg *ResolvedConfig, prior *SetupPlan, existing, planned ContractDeployment, currentNonce uint64, entries []JournalEntry) error {
	if cfg == nil || prior == nil {
		return errors.New("registration-role generation promotion context is unavailable")
	}
	wantGeneration, err := contractRegistrationGenerationFromSupersededSpend(cfg.Config.Topology, prior)
	if err != nil {
		return err
	}
	if planned.RegistrationRoleGeneration != wantGeneration || planned.RegistrationRoleGeneration != existing.RegistrationRoleGeneration+1 {
		return fmt.Errorf("registration-role generation promotion %d to %d is not the one approved generation %d", existing.RegistrationRoleGeneration, planned.RegistrationRoleGeneration, wantGeneration)
	}
	if !contractDeploymentAddressesEqual(existing, planned) || currentNonce != existing.InitialNonce+1 {
		return fmt.Errorf("registration-role generation promotion requires the same addresses at the exact post-reserve nonce %d; current nonce=%d", existing.InitialNonce+1, currentNonce)
	}
	existingHashes, existingErr := normalizedDeploymentRuntimeHashes(existing)
	plannedHashes, plannedErr := normalizedDeploymentRuntimeHashes(planned)
	if existingErr != nil || plannedErr != nil || !strings.EqualFold(existingHashes[existing.ReserveSink], plannedHashes[planned.ReserveSink]) {
		return errors.New("registration-role generation promotion changed the finalized reserve runtime identity")
	}
	if !exactVerifiedPlanAction(prior, entries, "evm.reserve-sink") {
		return errors.New("registration-role generation promotion has no exact verified reserve action")
	}
	unsafeIntents := map[string]Action{}
	for _, action := range prior.Actions {
		if action.ID != "evm.reserve-sink" && actionUsesContractDeployment(action) {
			unsafeIntents[action.ID] = action
		}
	}
	for _, entry := range entries {
		action, found := unsafeIntents[entry.ActionID]
		if !found || !prior.allowedPlanHashes()[entry.PlanHash] || !actionAcceptsIntent(action, entry.IntentHash) {
			continue
		}
		if entry.Stage == StageBroadcast || entry.Stage == StageIncluded || entry.Stage == StageFinalized || entry.Stage == StageVerified || entry.TransactionHash != "" {
			return fmt.Errorf("registration-role generation promotion is forbidden after %s reached %s", entry.ActionID, entry.Stage)
		}
	}
	return nil
}

func carryVerifiedGenerationIndependentReserve(revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil || !exactVerifiedPlanAction(prior, entries, "evm.reserve-sink") {
		return errors.New("generation-independent reserve carry has no exact verified ancestor")
	}
	var previous *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID == "evm.reserve-sink" {
			if previous != nil {
				return errors.New("prior plan has duplicate reserve actions")
			}
			copy := prior.Actions[index]
			previous = &copy
		}
	}
	if previous == nil {
		return errors.New("prior plan has no reserve action")
	}
	for index := range revised.Actions {
		action := &revised.Actions[index]
		if action.ID != "evm.reserve-sink" {
			continue
		}
		oldComparable, newComparable := *previous, *action
		oldComparable.IntentHash, newComparable.IntentHash = "", ""
		oldComparable.AcceptedPriorIntentHashes, newComparable.AcceptedPriorIntentHashes = nil, nil
		oldComparable.Parameters, newComparable.Parameters = cloneStrings(oldComparable.Parameters), cloneStrings(newComparable.Parameters)
		delete(oldComparable.Parameters, deploymentManifestHashParameter)
		delete(newComparable.Parameters, deploymentManifestHashParameter)
		oldHash, oldErr := canonicalHashHex(oldComparable)
		newHash, newErr := canonicalHashHex(newComparable)
		if oldErr != nil {
			return oldErr
		}
		if newErr != nil {
			return newErr
		}
		if oldHash != newHash {
			return errors.New("generation promotion changed the reserve transaction envelope")
		}
		action.AcceptedPriorIntentHashes = []string{previous.IntentHash}
		var err error
		action.IntentHash, err = actionIntentHash(*action)
		return err
	}
	return errors.New("revised plan has no reserve action")
}

func rebindPlanDeployment(plan *SetupPlan, deployment ContractDeployment) error {
	if plan == nil {
		return errors.New("revised plan is unavailable")
	}
	deployment = contractDeploymentIdentity(deployment)
	hash, err := contractDeploymentIdentityHash(deployment)
	if err != nil {
		return err
	}
	plan.Deployment = deployment
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if !actionUsesContractDeployment(*action) {
			continue
		}
		parameters := cloneStrings(action.Parameters)
		parameters[deploymentManifestHashParameter] = hash
		action.Parameters = parameters
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
	}
	return nil
}

func rebindPlanCoordinatorUpgrade(plan *SetupPlan, payloads *DeploymentPayloads) error {
	if plan == nil || payloads == nil {
		return errors.New("revised coordinator upgrade payload is unavailable")
	}
	if err := validateCoordinatorUpgradeIdentity(payloads.CoordinatorUpgrade, payloads.Deployer, plan.Deployment); err != nil {
		return err
	}
	plan.CoordinatorUpgrade = payloads.CoordinatorUpgrade
	implementationCount, activationCount, batcherCount, installBatchCount, refreshBatchCount := 0, 0, 0, 0, 0
	for index := range plan.Actions {
		action := &plan.Actions[index]
		switch action.ID {
		case "evm.coordinator-upgrade-implementation":
			implementationCount++
			action.Target = payloads.CoordinatorUpgrade.Implementation.Hex()
			action.Description = "deploy the exact release-1.0 coordinator implementation at the approval-bound deployer nonce"
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters["runtime_code_hash"] = payloads.CoordinatorUpgrade.RuntimeCodeHash
			envelope, ok := deploymentActionEnvelope(payloads, action.ID, plan.RegistrationBurnLimitRao)
			if !ok {
				return errors.New("coordinator upgrade implementation has no exact transaction envelope")
			}
			for key, value := range envelope {
				action.Parameters[key] = value
			}
		case "evm.coordinator-upgrade-activate":
			activationCount++
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters["implementation"] = payloads.CoordinatorUpgrade.Implementation.Hex()
			action.Parameters["runtime_code_hash"] = payloads.CoordinatorUpgrade.RuntimeCodeHash
		case "fleet.refresh.deploy-batcher":
			batcherCount++
			action.Target = payloads.FleetBatcherAddress.Hex()
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters["coordinator"] = payloads.Manifest.CoordinatorProxy.Hex()
			action.Parameters["commitment_oracle"] = payloads.CommitmentOracle.Hex()
			action.Parameters["runtime_code_hash"] = crypto.Keccak256Hash(payloads.FleetBatcherRuntime).Hex()
			envelope, ok := deploymentActionEnvelope(payloads, action.ID, plan.RegistrationBurnLimitRao)
			if !ok {
				return errors.New("fleet batcher deployment has no exact transaction envelope")
			}
			for key, value := range envelope {
				action.Parameters[key] = value
			}
		case "fleet.refresh.oracle-activate":
			action.Target = payloads.Manifest.CoordinatorProxy.Hex()
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters["oracle"] = payloads.FleetBatcherAddress.Hex()
		case "fleet.refresh.oracle-await-active":
			action.Target = payloads.FleetBatcherAddress.Hex()
		default:
			if strings.HasPrefix(action.ID, "fleet.install.batch.") {
				installBatchCount++
				action.Target = payloads.FleetBatcherAddress.Hex()
			} else if strings.HasPrefix(action.ID, "fleet.refresh.batch.") {
				refreshBatchCount++
				action.Target = payloads.FleetBatcherAddress.Hex()
			} else {
				continue
			}
		}
		var err error
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
	}
	if implementationCount != 1 || activationCount != 1 || batcherCount != 1 || installBatchCount == 0 || installBatchCount != refreshBatchCount {
		return fmt.Errorf("revised plan has %d coordinator implementation, %d activation, %d fleet batcher deployment, %d install batches, and %d refresh batches", implementationCount, activationCount, batcherCount, installBatchCount, refreshBatchCount)
	}
	return nil
}

func preserveVerifiedBaselineDeploymentActions(revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil {
		return errors.New("baseline deployment plans are unavailable")
	}
	baseline := map[string]bool{
		"evm.reserve-sink": true, "evm.settlement-vault": true,
		"evm.coordinator-implementation": true, "evm.vault-register-escrow": true,
		"evm.coordinator-proxy": true, "evm.governance-drill-implementation": true,
		"evm.vault-fix-coordinator": true, "evm.sink-fix-recorder": true,
	}
	verified := map[string]bool{}
	for _, entry := range entries {
		if prior.allowedPlanHashes()[entry.PlanHash] && entry.Stage == StageVerified {
			verified[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	priorActions := map[string]Action{}
	for _, action := range prior.Actions {
		priorActions[action.ID] = action
	}
	for index := range revised.Actions {
		action := revised.Actions[index]
		priorAction, ok := priorActions[action.ID]
		if !baseline[action.ID] || !ok || !verified[priorAction.ID+"\x00"+priorAction.IntentHash] {
			continue
		}
		revised.Actions[index] = priorAction
	}
	return nil
}

// Compare the executable transaction semantics while excluding only the
// approval gas-unit ceiling and its derived wei spend. A different fee cap,
// dependency, target, calldata-bound parameter, or non-gas spend is a
// different action and must never inherit an ancestor receipt.
func sameEVMTransactionExceptGasUnits(left, right Action) bool {
	if left.ID != right.ID || left.Kind != "evm-transaction" || right.Kind != "evm-transaction" || left.Target != right.Target || left.Description != right.Description || !slices.Equal(left.DependsOn, right.DependsOn) {
		return false
	}
	leftParameters := cloneStrings(left.Parameters)
	rightParameters := cloneStrings(right.Parameters)
	delete(leftParameters, evmMaximumGasUnitsParameter)
	delete(rightParameters, evmMaximumGasUnitsParameter)
	if !maps.Equal(leftParameters, rightParameters) {
		return false
	}
	leftSpend, rightSpend := left.Spend, right.Spend
	leftSpend.EVMGasWei, rightSpend.EVMGasWei = "0", "0"
	return leftSpend == rightSpend
}

// Recognize the one release transition which replaces an already-finalized
// per-fleet EVM write with a read-back proof after the atomic batch installer.
// A verified ancestor is retained verbatim so its consumed gas remains in the
// cumulative approval; an unverified write is never inherited.
func sameVerifiedLegacyFleetWriteAsReadProof(ancestor, proof Action) bool {
	if ancestor.ID != proof.ID || ancestor.Target != proof.Target || ancestor.Kind != "evm-transaction" || proof.Kind != "evm-read" || proof.Parameters["batch_installed"] != "true" {
		return false
	}
	if !strings.HasPrefix(proof.ID, "fleet.mirror.") && !strings.HasPrefix(proof.ID, "fleet.bind.") {
		return false
	}
	ancestorParameters := cloneStrings(ancestor.Parameters)
	proofParameters := cloneStrings(proof.Parameters)
	delete(ancestorParameters, evmMaximumGasUnitsParameter)
	delete(ancestorParameters, evmMaximumFeePerGasParameter)
	delete(proofParameters, "batch_installed")
	if !maps.Equal(ancestorParameters, proofParameters) {
		return false
	}
	ancestorHasOnlyGas := ancestor.Spend.TAORao == 0 && ancestor.Spend.AlphaRao == 0 && ancestor.Spend.Registrations == 0 && ancestor.Spend.SubnetCreations == 0 && !ancestor.Spend.EVMGasWei.IsZero()
	proofHasNoSpend := proof.Spend.TAORao == 0 && proof.Spend.AlphaRao == 0 && proof.Spend.Registrations == 0 && proof.Spend.SubnetCreations == 0 && proof.Spend.EVMGasWei.IsZero()
	return ancestorHasOnlyGas && proofHasNoSpend
}

// Carry an exact finalized transaction when a plan revision only reallocates
// gas units or replaces a verified legacy fleet write with its batch read-back
// proof. Replacing the new action with the authenticated ancestor also retains
// the original maximum spend, so cumulative approval cannot be understated.
func preserveVerifiedEVMGasReallocations(stateDir string, revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil {
		return errors.New("revised and prior plans are required to preserve EVM gas reallocations")
	}
	allowedPlans := prior.allowedPlanHashes()
	planCache := map[string]*SetupPlan{prior.PlanHash: prior}
	loadPlan := func(planHash string) (*SetupPlan, error) {
		if plan := planCache[planHash]; plan != nil {
			return plan, nil
		}
		if !allowedPlans[planHash] {
			return nil, fmt.Errorf("plan %s is outside the approved lineage", planHash)
		}
		if _, err := decodeHex32("ancestor plan hash", planHash); err != nil {
			return nil, err
		}
		plan, err := readPersistedPlanFile(filepath.Join(stateDir, "plans", stringsTrim0x(planHash)+".json"))
		if err != nil {
			return nil, fmt.Errorf("read ancestor plan %s: %w", planHash, err)
		}
		if !strings.EqualFold(plan.PlanHash, planHash) {
			return nil, fmt.Errorf("ancestor plan identity %s does not match requested %s", plan.PlanHash, planHash)
		}
		planCache[planHash] = plan
		return plan, nil
	}
	verifiedEntries := map[string][]JournalEntry{}
	for _, entry := range entries {
		if allowedPlans[entry.PlanHash] && entry.Stage == StageVerified {
			verifiedEntries[entry.ActionID] = append(verifiedEntries[entry.ActionID], entry)
		}
	}
	for index := range revised.Actions {
		current := revised.Actions[index]
		candidates := verifiedEntries[current.ID]
		for entryIndex := len(candidates) - 1; entryIndex >= 0; entryIndex-- {
			entry := candidates[entryIndex]
			plan, err := loadPlan(entry.PlanHash)
			if err != nil {
				return err
			}
			var ancestor *Action
			for actionIndex := range plan.Actions {
				action := &plan.Actions[actionIndex]
				if action.ID == current.ID && action.IntentHash == entry.IntentHash {
					if ancestor != nil {
						return fmt.Errorf("ancestor plan %s has duplicate exact action %s", plan.PlanHash, current.ID)
					}
					ancestor = action
				}
			}
			if ancestor == nil || (!sameEVMTransactionExceptGasUnits(*ancestor, current) && !sameVerifiedLegacyFleetWriteAsReadProof(*ancestor, current)) {
				continue
			}
			revised.Actions[index] = *ancestor
			break
		}
	}
	maximum, err := maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	revised.MaximumSpend = maximum
	return nil
}

// Move the ceiling of each exact verified EVM transaction out of the active
// action set when an in-place release revision no longer accepts its intent.
// The retired ceiling is charged once through SupersededSpend, while the new
// action retains its own executable ceiling. This keeps cumulative approval
// conservative across repeated upgrades and fleet-batcher replacements.
func addRetiredVerifiedEVMGas(prior, revised *SetupPlan, entries []JournalEntry, spend Spend) (Spend, error) {
	if prior == nil || revised == nil {
		return Spend{}, errors.New("revised and prior plans are required to retain retired EVM gas")
	}
	revisedActions := make(map[string]Action, len(revised.Actions))
	for _, action := range revised.Actions {
		if _, duplicate := revisedActions[action.ID]; duplicate {
			return Spend{}, fmt.Errorf("revised plan has duplicate action %s", action.ID)
		}
		revisedActions[action.ID] = action
	}
	allowedPlans := prior.allowedPlanHashes()
	for _, action := range prior.Actions {
		if action.Kind != "evm-transaction" || action.Spend.EVMGasWei.IsZero() {
			continue
		}
		verifiedIntents := map[string]bool{}
		for _, entry := range entries {
			if allowedPlans[entry.PlanHash] && entry.Stage == StageVerified && entry.ActionID == action.ID && actionAcceptsIntent(action, entry.IntentHash) {
				verifiedIntents[entry.IntentHash] = true
			}
		}
		if len(verifiedIntents) == 0 {
			continue
		}
		if replacement, found := revisedActions[action.ID]; found {
			carried := false
			for intent := range verifiedIntents {
				carried = carried || actionAcceptsIntent(replacement, intent)
			}
			if carried {
				continue
			}
		}
		var err error
		spend.EVMGasWei, err = addDecimalUint(spend.EVMGasWei, action.Spend.EVMGasWei)
		if err != nil {
			return Spend{}, fmt.Errorf("retain retired EVM gas for %s: %w", action.ID, err)
		}
	}
	return spend, nil
}

func verifiedOperatorAlphaSpend(prior *SetupPlan, entries []JournalEntry) (uint64, error) {
	if prior == nil {
		return 0, errors.New("prior alpha plan is unavailable")
	}
	verified := map[string]bool{}
	for _, entry := range entries {
		if prior.allowedPlanHashes()[entry.PlanHash] && entry.Stage == StageVerified {
			verified[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	total := prior.SupersededSpend.AlphaRao
	for _, action := range prior.Actions {
		if !strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") || !verified[action.ID+"\x00"+action.IntentHash] {
			continue
		}
		var ok bool
		total, ok = checkedAdd(total, action.Spend.AlphaRao)
		if !ok {
			return 0, errors.New("verified operator alpha spend overflows uint64")
		}
	}
	return total, nil
}

func validatePolicyRevisionOnChain(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan) error {
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		return fmt.Errorf("load policy-migration deployment: %w", err)
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return err
	}
	defer client.Close()
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return err
	}
	coordinator, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	vault, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		return err
	}
	reserve, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		return err
	}
	scalar := func(address common.Address, parsed abi.ABI, method string, args ...any) (any, error) {
		values, callErr := contractCallAt(ctx, client, address, parsed, method, head.Number, args...)
		if callErr != nil {
			return nil, callErr
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("%s returned %d values", method, len(values))
		}
		return values[0], nil
	}
	currentValue, err := scalar(deployment.CoordinatorProxy, coordinator, "currentEpoch")
	current, ok := currentValue.(*big.Int)
	if err != nil || !ok || !current.IsUint64() {
		return stateMismatchError(err, "policy migration currentEpoch=%T", currentValue)
	}
	policyValues, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, coordinator, "policyAt", head.Number, current)
	if err != nil || !strings.EqualFold(extractFirstBytes32(policyValues), prior.PolicyHash) {
		return stateMismatchError(err, "active policy changed before approved migration")
	}
	countValue, err := scalar(deployment.CoordinatorProxy, coordinator, "policyCount")
	count, ok := countValue.(*big.Int)
	if err != nil || !ok || count.Cmp(big.NewInt(1)) != 0 {
		return stateMismatchError(err, "policy migration policyCount=%v, want 1", countValue)
	}
	for _, check := range []struct {
		address common.Address
		parsed  abi.ABI
		method  string
	}{
		{deployment.CoordinatorProxy, coordinator, "campaignReserved"},
		{deployment.SettlementVault, vault, "totalCaptured"},
		{deployment.ReserveSink, reserve, "principal"},
	} {
		value, readErr := scalar(check.address, check.parsed, check.method)
		amount, amountOK := value.(*big.Int)
		if readErr != nil || !amountOK || amount.Sign() != 0 {
			return stateMismatchError(readErr, "policy migration %s=%v, want zero", check.method, value)
		}
	}
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		value, readErr := scalar(deployment.CoordinatorProxy, coordinator, "nextDepositNonce", big.NewInt(int64(noID)))
		nonce, nonceOK := value.(*big.Int)
		if readErr != nil || !nonceOK || nonce.Sign() != 0 {
			return stateMismatchError(readErr, "policy migration operator %d nonce=%v, want zero", noID, value)
		}
	}
	return nil
}

// Name the registration that terminally consumes one independently funded
// native coldkey. Other funding roles retain their current revised targets.
func registrationConsumerForFunding(actionID string) (string, bool) {
	for _, pair := range []struct {
		fundingPrefix, registrationPrefix string
	}{
		{fundingPrefix: "churn.fund.", registrationPrefix: "churn.register."},
		{fundingPrefix: "fleet.fund.", registrationPrefix: "fleet.register."},
		{fundingPrefix: "validator.fund.", registrationPrefix: "validator.register."},
	} {
		if !strings.HasPrefix(actionID, pair.fundingPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(actionID, pair.fundingPrefix)
		index, err := strconv.Atoi(suffix)
		if err != nil || index <= 0 || strconv.Itoa(index) != suffix {
			return "", false
		}
		return pair.registrationPrefix + suffix, true
	}
	return "", false
}

// Retain an ancestor funding intent only after both its exact transfer and its
// unchanged consuming registration have durable proofs. An interrupted or
// changed registration keeps the newly reviewed repair funding instead.
func preserveConsumedRegistrationFunding(revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil {
		return errors.New("revised and prior plans are required to preserve consumed funding")
	}
	priorActions := make(map[string]Action, len(prior.Actions))
	revisedActions := make(map[string]Action, len(revised.Actions))
	for _, action := range prior.Actions {
		priorActions[action.ID] = action
	}
	for _, action := range revised.Actions {
		revisedActions[action.ID] = action
	}
	verifiedIntents := map[string]bool{}
	allowedPlans := prior.allowedPlanHashes()
	for _, entry := range entries {
		if allowedPlans[entry.PlanHash] && entry.Stage == StageVerified {
			verifiedIntents[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	for index, revisedFunding := range revised.Actions {
		consumerID, ok := registrationConsumerForFunding(revisedFunding.ID)
		if !ok {
			continue
		}
		priorFunding, fundingExists := priorActions[revisedFunding.ID]
		priorConsumer, priorConsumerExists := priorActions[consumerID]
		revisedConsumer, revisedConsumerExists := revisedActions[consumerID]
		if !fundingExists || !priorConsumerExists || !revisedConsumerExists || priorConsumer.IntentHash != revisedConsumer.IntentHash {
			continue
		}
		if !verifiedIntents[priorFunding.ID+"\x00"+priorFunding.IntentHash] || !verifiedIntents[priorConsumer.ID+"\x00"+priorConsumer.IntentHash] {
			continue
		}
		revised.Actions[index] = priorFunding
	}
	maximumSpend, err := maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	revised.MaximumSpend = maximumSpend
	return nil
}

// Preserve verified operator custody without repeating a campaign allocation.
// A stricter release may increase the absolute reserve requirement; carry every
// verified repair into the cumulative budget and add only one runtime-minimum
// top-up when their conservative destination credits remain insufficient.
func preserveVerifiedOperatorAlphaTransfers(revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil {
		return errors.New("revised and prior plans are required to preserve operator alpha transfers")
	}
	allowedPlans := prior.allowedPlanHashes()
	isVerified := func(action Action) bool {
		for _, entry := range entries {
			if allowedPlans[entry.PlanHash] && entry.ActionID == action.ID && entry.Stage == StageVerified && actionAcceptsIntent(action, entry.IntentHash) {
				return true
			}
		}
		return false
	}
	priorActions := make(map[string]Action, len(prior.Actions))
	for _, action := range prior.Actions {
		priorActions[action.ID] = action
	}
	revisedActions := make(map[string]Action, len(revised.Actions))
	for _, action := range revised.Actions {
		revisedActions[action.ID] = action
	}
	minimumCredit := func(action Action) (uint64, error) {
		if !planUsesAlphaTransferEnvelope(prior.Schema) {
			if action.Spend.AlphaRao == 0 {
				return 0, fmt.Errorf("verified alpha action %s has no spend", action.ID)
			}
			return action.Spend.AlphaRao, nil
		}
		if encoded := action.Parameters["minimum_destination_credit_rao"]; encoded != "" {
			credit, err := strconv.ParseUint(encoded, 10, 64)
			if err != nil || credit == 0 || credit > action.Spend.AlphaRao {
				return 0, fmt.Errorf("verified alpha action %s has invalid minimum credit", action.ID)
			}
			return credit, nil
		}
		// Before v8, StageVerified meant the old exact-delta postcondition
		// passed. Treat that stronger historical proof as its exact credit.
		if action.Spend.AlphaRao == 0 {
			return 0, fmt.Errorf("verified alpha action %s has no spend", action.ID)
		}
		return action.Spend.AlphaRao, nil
	}
	usedIDs := map[string]bool{}
	for _, action := range revised.Actions {
		usedIDs[action.ID] = true
	}
	for _, action := range prior.Actions {
		usedIDs[action.ID] = true
	}
	result := make([]Action, 0, len(revised.Actions)+2)
	replacementTails := map[string]string{}
	repairParents := map[string]string{}
	for _, action := range revised.Actions {
		if !strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") {
			result = append(result, action)
			continue
		}
		priorAction, ok := priorActions[action.ID]
		if !ok || priorAction.Target != action.Target || !isVerified(priorAction) {
			result = append(result, action)
			continue
		}
		requiredCredit, err := strconv.ParseUint(action.Parameters["minimum_destination_credit_rao"], 10, 64)
		if err != nil || requiredCredit == 0 {
			return fmt.Errorf("revised operator alpha transfer %s has no minimum destination credit", action.ID)
		}
		credited, err := minimumCredit(priorAction)
		if err != nil {
			return err
		}
		result = append(result, priorAction)
		tail := priorAction.ID
		_, operator, targetErr := alphaTransferTargetFromActionID(action.ID)
		if targetErr != nil {
			return targetErr
		}
		for _, priorRepair := range prior.Actions {
			kind, repairOperator, repairErr := alphaTransferTargetFromActionID(priorRepair.ID)
			if repairErr != nil || kind != "operator-deposit" || repairOperator != operator || !strings.HasPrefix(priorRepair.ID, "alpha.repair.") || priorRepair.Target != action.Target || !isVerified(priorRepair) {
				continue
			}
			credit, creditErr := minimumCredit(priorRepair)
			if creditErr != nil {
				return creditErr
			}
			var addOK bool
			credited, addOK = checkedAdd(credited, credit)
			if !addOK {
				return fmt.Errorf("operator %d verified alpha credit overflows", operator)
			}
			// A specialized recovery may already have placed this exact repair
			// after an external reconciliation dependency. Count its custody
			// credit once, but do not hoist or duplicate it beside the base
			// transfer. The specialized recovery owns its global ordering.
			if revisedRepair, placed := revisedActions[priorRepair.ID]; placed {
				if revisedRepair.IntentHash != priorRepair.IntentHash {
					return fmt.Errorf("placed operator alpha repair %s differs from its verified ancestor", priorRepair.ID)
				}
				continue
			}
			result = append(result, priorRepair)
			repairParents[priorRepair.ID] = priorAction.ID
			tail = priorRepair.ID
		}
		if credited < requiredCredit {
			minimumTransfer, parseErr := strconv.ParseUint(action.Parameters["minimum_alpha_at_approved_price_rao"], 10, 64)
			if parseErr != nil || minimumTransfer == 0 {
				return fmt.Errorf("revised operator alpha transfer %s has no runtime minimum", action.ID)
			}
			deficit := requiredCredit - credited
			exact, addOK := checkedAdd(deficit, alphaTransferDestinationRoundingAllowance)
			if !addOK {
				return fmt.Errorf("operator %d alpha top-up overflows", operator)
			}
			exact = max64(exact, minimumTransfer)
			baseID := fmt.Sprintf("alpha.repair.operator-deposit.%d", operator)
			repairID := baseID
			for sequence := 2; usedIDs[repairID]; sequence++ {
				repairID = fmt.Sprintf("%s.%d", baseID, sequence)
			}
			usedIDs[repairID] = true
			parameters := alphaTransferActionParameters(exact, 0, minimumTransfer, &revised.LiveFacts, revised.AlphaTransferMarginBPS)
			parameters[alphaRepairForActionParameter] = priorAction.ID
			parameters[alphaRepairMinimumDestinationParameter] = strconv.FormatUint(requiredCredit, 10)
			parameters["campaign_policy_hash"] = revised.PolicyHash
			parameters[deploymentManifestHashParameter] = action.Parameters[deploymentManifestHashParameter]
			repair := Action{
				ID: repairID, Kind: "substrate-extrinsic", Target: action.Target,
				Description: "top up verified operator custody for the stricter two-leg runtime rounding envelope",
				Parameters:  parameters, Spend: Spend{AlphaRao: exact}, DependsOn: []string{tail},
			}
			result = append(result, repair)
			repairParents[repairID] = priorAction.ID
			tail = repairID
		}
		replacementTails[priorAction.ID] = tail
	}
	for index := range result {
		// A verified action is immutable historical evidence. A later custody
		// repair may gate unverified descendants, but must never rewrite the
		// dependency list (and therefore intent) of an action already on chain.
		if isVerified(result[index]) {
			continue
		}
		parent := repairParents[result[index].ID]
		for dependency := range result[index].DependsOn {
			original := result[index].DependsOn[dependency]
			if tail := replacementTails[original]; tail != "" && parent != original {
				result[index].DependsOn[dependency] = tail
			}
		}
	}
	revised.Actions = result
	maximum, err := maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	revised.MaximumSpend = maximum
	return nil
}

// V5 and V6 validator bootstrap transfers already bind the final stake,
// reserve-majority target, price snapshot, and exact amount. V7 corrected the
// runtime transfer-floor field and V8 bound destination-share rounding; neither
// stricter envelope may turn an otherwise identical, proven exact-credit
// validator transfer into a duplicate. Carry
// only exact verified intents whose complete non-floor economics are unchanged;
// legacy V4 validator sizing remains deliberately ineligible.
func preserveVerifiedValidatorAlphaTransfers(revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil {
		return errors.New("revised and prior plans are required to preserve validator alpha transfers")
	}
	if !planUsesAlphaTransferEnvelope(prior.Schema) {
		return nil
	}
	verified := map[string]bool{}
	allowedPlans := prior.allowedPlanHashes()
	for _, entry := range entries {
		if allowedPlans[entry.PlanHash] && entry.Stage == StageVerified {
			verified[entry.ActionID+"\x00"+entry.IntentHash] = true
		}
	}
	priorActions := make(map[string]Action, len(prior.Actions))
	for _, action := range prior.Actions {
		priorActions[action.ID] = action
	}
	unchangedKeys := []string{
		"approved_alpha_price_q9",
		"campaign_requirement_rao",
		"minimum_tao_equivalent_margin_bps",
		"planned_existing_stake_rao",
		"planned_final_stake_rao",
		"registered_alpha_snapshot_rao",
		"reserve_minimum_share_bps",
		"reserve_target_share_bps",
	}
	for index, action := range revised.Actions {
		if !strings.HasPrefix(action.ID, "alpha.transfer.validator.") {
			continue
		}
		priorAction, ok := priorActions[action.ID]
		if !ok || priorAction.Target != action.Target || !verified[priorAction.ID+"\x00"+priorAction.IntentHash] {
			continue
		}
		equal, equalErr := equalSpend(priorAction.Spend, action.Spend)
		if equalErr != nil {
			return equalErr
		}
		legacyExactCredit := false
		priorNonAlpha, revisedNonAlpha := priorAction.Spend, action.Spend
		priorNonAlpha.AlphaRao, revisedNonAlpha.AlphaRao = 0, 0
		nonAlphaEqual, nonAlphaErr := equalSpend(priorNonAlpha, revisedNonAlpha)
		if nonAlphaErr != nil {
			return nonAlphaErr
		}
		legacyRoundingEnvelope := priorAction.Parameters["maximum_destination_rounding_shortfall_rao"] == "" && priorAction.Parameters["minimum_destination_credit_rao"] == ""
		if legacyRoundingEnvelope && nonAlphaEqual {
			withBootstrapAllowance, addOK := checkedAdd(priorAction.Spend.AlphaRao, alphaTransferDestinationRoundingAllowance)
			legacyExactCredit = addOK && withBootstrapAllowance == action.Spend.AlphaRao
		}
		if !equal && !legacyExactCredit {
			continue
		}
		unchanged := true
		for _, key := range unchangedKeys {
			if priorAction.Parameters[key] != action.Parameters[key] {
				unchanged = false
				break
			}
		}
		if unchanged && (equal || priorAction.Parameters["exact_amount_rao"] == strconv.FormatUint(priorAction.Spend.AlphaRao, 10)) {
			revised.Actions[index] = priorAction
		}
	}
	maximum, err := maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	revised.MaximumSpend = maximum
	return nil
}

// Convert an exact finalized-but-unverified alpha transfer into a local
// reconciliation action. The original cumulative spend remains in the plan,
// while a separate runtime-minimum transfer repairs a possible one-rao funding
// deficit without ever repeating the campaign allocation.
func reconcileFinalizedAlphaTransfers(revised, prior *SetupPlan, entries []JournalEntry) error {
	if revised == nil || prior == nil {
		return errors.New("revised and prior plans are required to reconcile alpha transfers")
	}
	verified := map[string]bool{}
	finalized := map[string]JournalEntry{}
	allowed := prior.allowedPlanHashes()
	for _, entry := range entries {
		if !allowed[entry.PlanHash] {
			continue
		}
		key := entry.PlanHash + "\x00" + entry.ActionID + "\x00" + entry.IntentHash
		if entry.Stage == StageVerified {
			verified[key] = true
		}
		if entry.Stage == StageFinalized && entry.TransactionHash != "" && entry.BlockNumber != 0 && entry.BlockHash != "" {
			finalized[key] = entry
		}
	}
	priorActions := make(map[string]Action, len(prior.Actions))
	for _, action := range prior.Actions {
		priorActions[action.ID] = action
	}
	reconciled := map[string]bool{}

	for index := 0; index < len(revised.Actions); index++ {
		planned := revised.Actions[index]
		targetKind, targetIndex, targetErr := alphaTransferTargetFromActionID(planned.ID)
		if targetErr != nil || !strings.HasPrefix(planned.ID, "alpha.transfer.") {
			continue
		}
		previous, ok := priorActions[planned.ID]
		if !ok || previous.Target != planned.Target || previous.Kind != "substrate-extrinsic" || previous.Spend.AlphaRao == 0 {
			continue
		}
		if targetKind == "operator-deposit" && previous.Parameters[deploymentManifestHashParameter] != planned.Parameters[deploymentManifestHashParameter] {
			continue
		}
		isVerified := false
		for planHash := range allowed {
			isVerified = isVerified || verified[planHash+"\x00"+previous.ID+"\x00"+previous.IntentHash]
		}
		if isVerified {
			continue
		}
		var receipt JournalEntry
		found := false
		for entryIndex := len(entries) - 1; entryIndex >= 0; entryIndex-- {
			candidate := entries[entryIndex]
			if allowed[candidate.PlanHash] && candidate.ActionID == previous.ID && candidate.IntentHash == previous.IntentHash && candidate.Stage == StageFinalized && candidate.TransactionHash != "" && candidate.BlockNumber != 0 && candidate.BlockHash != "" {
				receipt, found = candidate, true
				break
			}
		}
		if !found {
			continue
		}

		minimumCredit, err := strconv.ParseUint(planned.Parameters["minimum_destination_credit_rao"], 10, 64)
		if err != nil || minimumCredit == 0 {
			return fmt.Errorf("revised alpha transfer %s has no minimum destination credit", planned.ID)
		}
		minimumTransfer, err := strconv.ParseUint(planned.Parameters["minimum_alpha_at_approved_price_rao"], 10, 64)
		if err != nil || minimumTransfer == 0 {
			return fmt.Errorf("revised alpha transfer %s has no runtime minimum", planned.ID)
		}

		reconciliation := planned
		reconciliation.Kind = "substrate-reconciliation"
		reconciliation.Description = "reconcile the exact finalized runtime-452 alpha transfer without broadcasting it again"
		reconciliation.Spend = previous.Spend
		reconciliation.Parameters = cloneStrings(planned.Parameters)
		reconciliation.Parameters["exact_amount_rao"] = strconv.FormatUint(previous.Spend.AlphaRao, 10)
		priorMinimumCredit, creditErr := alphaTransferMinimumCreditRao(previous.Spend.AlphaRao)
		if creditErr != nil {
			return creditErr
		}
		reconciliation.Parameters["minimum_destination_credit_rao"] = strconv.FormatUint(priorMinimumCredit, 10)
		reconciliation.Parameters[alphaRecoveryPlanHashParameter] = receipt.PlanHash
		reconciliation.Parameters[alphaRecoveryIntentHashParameter] = receipt.IntentHash
		reconciliation.Parameters[alphaRecoveryTransactionHashParameter] = receipt.TransactionHash
		reconciliation.Parameters[alphaRecoveryBlockParameter] = strconv.FormatUint(receipt.BlockNumber, 10)
		reconciliation.Parameters[alphaRecoveryBlockHashParameter] = receipt.BlockHash

		repairID := fmt.Sprintf("alpha.repair.%s.%d", targetKind, targetIndex)
		repairParameters := alphaTransferActionParameters(minimumTransfer, 0, minimumTransfer, &revised.LiveFacts, revised.AlphaTransferMarginBPS)
		repairParameters[alphaRepairForActionParameter] = reconciliation.ID
		repairParameters[alphaRepairMinimumIncrementParameter] = strconv.FormatUint(minimumCredit, 10)
		if targetKind == "operator-deposit" {
			repairParameters["campaign_policy_hash"] = revised.PolicyHash
			repairParameters[deploymentManifestHashParameter] = planned.Parameters[deploymentManifestHashParameter]
		}
		repair := Action{
			ID: repairID, Kind: "substrate-extrinsic", Target: planned.Target,
			Description: "repair a bounded runtime-452 destination-share rounding deficit without repeating the campaign allocation",
			Parameters:  repairParameters, Spend: Spend{AlphaRao: minimumTransfer}, DependsOn: []string{reconciliation.ID},
		}
		revised.Actions[index] = reconciliation
		revised.Actions = append(revised.Actions[:index+1], append([]Action{repair}, revised.Actions[index+1:]...)...)
		for later := index + 2; later < len(revised.Actions); later++ {
			for dependency := range revised.Actions[later].DependsOn {
				if revised.Actions[later].DependsOn[dependency] == reconciliation.ID {
					revised.Actions[later].DependsOn[dependency] = repairID
				}
			}
		}
		reconciled[previous.ID+"\x00"+previous.IntentHash] = true
		index++
	}
	for _, previous := range prior.Actions {
		if !strings.HasPrefix(previous.ID, "alpha.transfer.") {
			continue
		}
		isVerified, isFinalized := false, false
		for planHash := range allowed {
			key := planHash + "\x00" + previous.ID + "\x00" + previous.IntentHash
			isVerified = isVerified || verified[key]
			_, isFinalizedHere := finalized[key]
			isFinalized = isFinalized || isFinalizedHere
		}
		if isFinalized && !isVerified && !reconciled[previous.ID+"\x00"+previous.IntentHash] {
			return fmt.Errorf("finalized alpha transfer %s cannot be reconciled into the active deployment", previous.ID)
		}
	}
	for index := range revised.Actions {
		intent, err := actionIntentHash(revised.Actions[index])
		if err != nil {
			return err
		}
		revised.Actions[index].IntentHash = intent
	}
	maximum, err := maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	revised.MaximumSpend = maximum
	return nil
}

// A v5+ plan already binds the complete alpha-transfer economics and exact
// validator bootstrap amounts. Rebuilding those intents from an advancing
// emission snapshot can otherwise turn an already verified transfer into a
// second transfer, lose the first transfer from the cumulative budget, and
// make the approval hash change between plan and apply. Keep the approved v5
// sizing snapshot; current source capacity, runtime minimums, destination
// registration, and reserve majority are still checked immediately before an
// unverified transfer and while carried postconditions are audited.
func preserveAlphaSizingFacts(normalized *SetupFacts, prior *SetupPlan) {
	if normalized == nil || prior == nil || !planUsesAlphaTransferEnvelope(prior.Schema) {
		return
	}
	// V5/v6 authenticated InitialMinStake under the old interpretation. A v7+
	// revision must instead use the current finalized DefaultMinTransfer; never
	// carry the historical field into the corrected runtime-floor envelope.
	if planUsesDefaultMinTransferEnvelope(prior.Schema) {
		normalized.DefaultMinTransferRao = prior.LiveFacts.DefaultMinTransferRao
	}
	normalized.AlphaPriceQ9 = prior.LiveFacts.AlphaPriceQ9
	normalized.RegisteredAlphaRao = prior.LiveFacts.RegisteredAlphaRao
	normalized.ReserveValidatorAlphaRao = prior.LiveFacts.ReserveValidatorAlphaRao
	normalized.IndependentValidatorAlphaRao = prior.LiveFacts.IndependentValidatorAlphaRao
}

// Build a deterministic revision from already finalized, caller-supplied facts.
func buildPlanRevisionFromFacts(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, generatedAt time.Time) (*SetupPlan, error) {
	return buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg, stateDir, prior, current, entries, generatedAt, nil, nil)
}

func buildPlanRevisionFromFactsWithMigration(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, generatedAt time.Time, migration *coordinatorUpgradeMigration) (*SetupPlan, error) {
	return buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg, stateDir, prior, current, entries, generatedAt, migration, nil)
}

func buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, generatedAt time.Time, migration *coordinatorUpgradeMigration, recoveries []voluntaryConvictionDuplicateRecovery) (*SetupPlan, error) {
	return buildPlanRevisionFromFactsWithAllRecoveries(cfg, stateDir, prior, current, entries, generatedAt, migration, planRevisionRecoveries{VoluntaryConvictions: recoveries})
}

// Build a deterministic revision with every transaction recovery class already
// authenticated against live finalized state.
func buildPlanRevisionFromFactsWithAllRecoveries(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, generatedAt time.Time, migration *coordinatorUpgradeMigration, recoveries planRevisionRecoveries) (*SetupPlan, error) {
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		return nil, err
	}
	if err := validatePlanRevisionIdentity(cfg, prior, roles); err != nil {
		return nil, err
	}
	policyChanged := !strings.EqualFold(prior.PolicyHash, cfg.PolicyHash)
	if policyChanged {
		if err := validatePolicyRevisionJournal(prior, entries); err != nil {
			return nil, err
		}
	}
	roleSecrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	if err := validatePlanRevisionTopology(cfg, stateDir, prior, current, roleSecrets); err != nil {
		return nil, err
	}
	remaining, err := remainingPlanSpend(prior, entries)
	if err != nil {
		return nil, err
	}
	normalized := *current
	normalized.ExistingUIDCount = prior.LiveFacts.ExistingUIDCount
	normalized.ExistingUIDs = append([]ExistingUIDFact(nil), prior.LiveFacts.ExistingUIDs...)
	normalized.SubnetOwnerHotkey = prior.LiveFacts.SubnetOwnerHotkey
	normalized.UIDZeroHotkey = prior.LiveFacts.UIDZeroHotkey
	preserveAlphaSizingFacts(&normalized, prior)
	supersededDeployments := append([]ContractDeployment(nil), prior.SupersededDeployments...)
	deploymentSuperseded := false
	deploymentUpgradedInPlace := false
	deploymentGenerationPromoted := false
	upgradeDeployment := prior.Deployment
	var existingDeployment *ContractDeployment
	var currentPayloads *DeploymentPayloads
	if stored, loadErr := loadContractDeployment(stateDir); loadErr == nil {
		existingDeployment = stored
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return nil, fmt.Errorf("load active contract deployment for revision: %w", loadErr)
	} else if planUsesContractDeploymentEnvelope(prior.Schema) && prior.Deployment.Schema != "" {
		copy := prior.Deployment
		existingDeployment = &copy
	}
	if existingDeployment != nil {
		activeGeneration, generationErr := contractRegistrationGenerationFromSupersededSpend(cfg.Config.Topology, prior)
		if generationErr != nil {
			return nil, generationErr
		}
		var payloadErr error
		currentPayloads, payloadErr = buildDeploymentPayloadsWithRegistrationGeneration(cfg, roleSecrets, existingDeployment.InitialNonce, activeGeneration)
		if payloadErr != nil {
			return nil, payloadErr
		}
		if migration != nil && migration.Upgrade.Schema != "" {
			if err := configureCoordinatorUpgradeNonce(currentPayloads, migration.Upgrade.DeployerNonce); err != nil || currentPayloads.CoordinatorUpgrade != migration.Upgrade {
				return nil, stateMismatchError(err, "coordinator upgrade migration payload differs from its approved identity")
			}
		}
		if contractDeploymentAddressesEqual(*existingDeployment, currentPayloads.Manifest) && contractDeploymentRuntimeHashesCompatible(*existingDeployment, currentPayloads.Manifest) {
			normalized.DeployerNonce = existingDeployment.InitialNonce
		} else if promotionErr := validateRegistrationRoleGenerationPromotion(cfg, prior, *existingDeployment, currentPayloads.Manifest, current.DeployerNonce, entries); promotionErr == nil {
			normalized.DeployerNonce = existingDeployment.InitialNonce
			deploymentGenerationPromoted = true
		} else if migration != nil {
			priorHash, hashErr := contractDeploymentIdentityHash(prior.Deployment)
			wantPriorHash := migration.Baseline.PriorDeploymentHash
			if !prior.CoordinatorUpgradeBaseline.isZero() {
				wantPriorHash = migration.Baseline.ReboundDeploymentHash
			}
			if hashErr != nil || priorHash != wantPriorHash {
				return nil, errors.New("coordinator upgrade migration does not authenticate the prior deployment")
			}
			nonceMatches, nonceErr := coordinatorUpgradeMigrationNonceMatches(prior, migration, currentPayloads, current.DeployerNonce, entries)
			if nonceErr != nil {
				return nil, nonceErr
			}
			if !nonceMatches || !contractDeploymentAddressesEqual(*existingDeployment, migration.Deployment) || !contractDeploymentRuntimeHashesCompatible(*existingDeployment, migration.Deployment) {
				return nil, errors.New("coordinator upgrade migration no longer matches finalized deployment facts")
			}
			if err := validateCoordinatorUpgradeBaselineRelease(migration.Baseline, migration.Deployment, currentPayloads.Manifest, currentPayloads.CoordinatorUpgrade); err != nil {
				return nil, err
			}
			if err := validateCoordinatorUpgradePayloadBaseline(migration.Baseline, currentPayloads); err != nil {
				return nil, err
			}
			normalized.DeployerNonce = existingDeployment.InitialNonce
			upgradeDeployment = migration.Deployment
			deploymentUpgradedInPlace = true
		} else if contractDeploymentUpgradeBaselineCompatible(contractDeploymentIdentity(prior.Deployment), currentPayloads.Manifest) && contractDeploymentAddressesEqual(*existingDeployment, prior.Deployment) && contractDeploymentRuntimeHashesCompatible(*existingDeployment, prior.Deployment) {
			normalized.DeployerNonce = existingDeployment.InitialNonce
			deploymentUpgradedInPlace = true
		} else {
			if err := validateAbandonableDeployment(cfg, prior, *existingDeployment, current, entries); err != nil {
				return nil, fmt.Errorf("immutable deployment replacement safety: %w", err)
			}
			deploymentSuperseded = true
			exactHash, exactErr := canonicalHashHex(*existingDeployment)
			if exactErr != nil {
				return nil, exactErr
			}
			seenDeployment := false
			for _, candidate := range supersededDeployments {
				candidateHash, candidateErr := canonicalHashHex(candidate)
				if candidateErr != nil {
					return nil, candidateErr
				}
				seenDeployment = seenDeployment || candidateHash == exactHash
			}
			if !seenDeployment {
				supersededDeployments = append(supersededDeployments, *existingDeployment)
			}
		}
	}
	spentAlpha := prior.MaximumSpend.AlphaRao - remaining.AlphaRao
	if available, ok := checkedAdd(normalized.AlphaAvailableRao, spentAlpha); ok {
		normalized.AlphaAvailableRao = available
	} else {
		return nil, errors.New("plan revision alpha availability overflow")
	}
	if transferable, ok := checkedAdd(normalized.AlphaTransferableRao, spentAlpha); ok {
		normalized.AlphaTransferableRao = transferable
	} else {
		return nil, errors.New("plan revision transferable alpha overflow")
	}
	if coldkeyTotal, ok := checkedAdd(normalized.WalletNetuidAlphaRao, spentAlpha); ok {
		normalized.WalletNetuidAlphaRao = coldkeyTotal
	} else {
		return nil, errors.New("plan revision coldkey alpha overflow")
	}
	supersededSpend := prior.SupersededSpend
	if policyChanged {
		alphaSpend, alphaErr := verifiedOperatorAlphaSpend(prior, entries)
		if alphaErr != nil {
			return nil, alphaErr
		}
		supersededSpend.AlphaRao = alphaSpend
	}
	if deploymentSuperseded {
		supersededSpend, err = supersededVerifiedSpend(prior, entries)
		if err != nil {
			return nil, err
		}
	}
	if len(recoveries.VoluntaryConvictions) > 1 {
		return nil, errors.New("only one duplicate voluntary-conviction recovery is permitted")
	}
	if len(recoveries.VoluntaryConvictions) == 1 {
		recovery := recoveries.VoluntaryConvictions[0]
		gasAfter, gasErr := voluntaryConvictionRecoveryGasAfter(recovery)
		if gasErr != nil {
			return nil, gasErr
		}
		if recovery.AlreadyPlanned {
			comparison, compareErr := supersededSpend.EVMGasWei.Cmp(gasAfter)
			if compareErr != nil || comparison < 0 {
				return nil, stateMismatchError(compareErr, "prior superseded EVM spend %s is below reconciled duplicate total %s", supersededSpend.EVMGasWei, gasAfter)
			}
		} else {
			comparison, compareErr := recovery.SupersededGasBefore.Cmp(supersededSpend.EVMGasWei)
			if compareErr != nil || comparison != 0 {
				return nil, stateMismatchError(compareErr, "duplicate recovery prior gas %s differs from plan lineage %s", recovery.SupersededGasBefore, supersededSpend.EVMGasWei)
			}
			supersededSpend.EVMGasWei = gasAfter
		}
	}
	generationPlan := *prior
	generationPlan.SupersededSpend = supersededSpend
	registrationGeneration, err := contractRegistrationGenerationFromSupersededSpend(cfg.Config.Topology, &generationPlan)
	if err != nil {
		return nil, err
	}
	revised, err := buildPlanWithRegistrationGeneration(cfg, &normalized, roles, generatedAt, registrationGeneration)
	if err != nil {
		return nil, err
	}
	revised.SupersededDeployments = supersededDeployments
	if deploymentGenerationPromoted {
		if err := carryVerifiedGenerationIndependentReserve(revised, prior, entries); err != nil {
			return nil, fmt.Errorf("carry generation-independent reserve: %w", err)
		}
	}
	if deploymentUpgradedInPlace {
		if err := rebindPlanDeployment(revised, upgradeDeployment); err != nil {
			return nil, fmt.Errorf("retain immutable deployment baseline: %w", err)
		}
		if migration != nil {
			revised.CoordinatorUpgradeBaseline = migration.Baseline
			if err := rebindPlanCoordinatorUpgrade(revised, currentPayloads); err != nil {
				return nil, fmt.Errorf("bind repeated coordinator upgrade: %w", err)
			}
		}
		if err := preserveVerifiedBaselineDeploymentActions(revised, prior, entries); err != nil {
			return nil, fmt.Errorf("preserve verified deployment baseline: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, hash := range append(append([]string(nil), prior.PriorPlanHashes...), prior.PlanHash) {
		if !seen[hash] {
			revised.PriorPlanHashes = append(revised.PriorPlanHashes, hash)
			seen[hash] = true
		}
	}
	if err := preserveConsumedRegistrationFunding(revised, prior, entries); err != nil {
		return nil, fmt.Errorf("preserve consumed registration funding: %w", err)
	}
	if err := validateRevisedSubstrateFundingRecoveries(revised, recoveries.SubstrateFundings); err != nil {
		return nil, fmt.Errorf("reconcile finalized substrate funding: %w", err)
	}
	if len(recoveries.VoluntaryConvictions) == 1 {
		if err := applyVoluntaryConvictionDuplicateRecovery(cfg, revised, prior, entries, recoveries.VoluntaryConvictions[0]); err != nil {
			return nil, fmt.Errorf("reconcile duplicate voluntary conviction: %w", err)
		}
	}
	// Preserve an exact verified legacy write before validating a recovered
	// receipt for that action. This ordering permits only the reviewed
	// transaction-to-read-proof transition; a merely finalized recovery still
	// retains the new intent and fails the unchanged-action check below.
	if err := preserveVerifiedEVMGasReallocations(stateDir, revised, prior, entries); err != nil {
		return nil, fmt.Errorf("preserve verified EVM gas reallocations: %w", err)
	}
	if err := validateRevisedFleetMirrorRecoveries(revised, recoveries.FleetMirrors); err != nil {
		return nil, fmt.Errorf("reconcile finalized fleet mirror: %w", err)
	}
	if !policyChanged {
		if err := preserveVerifiedOperatorAlphaTransfers(revised, prior, entries); err != nil {
			return nil, fmt.Errorf("preserve verified operator alpha transfers: %w", err)
		}
	}
	if err := preserveVerifiedValidatorAlphaTransfers(revised, prior, entries); err != nil {
		return nil, fmt.Errorf("preserve verified validator alpha transfers: %w", err)
	}
	if err := reconcileFinalizedAlphaTransfers(revised, prior, entries); err != nil {
		return nil, fmt.Errorf("reconcile finalized alpha transfers: %w", err)
	}
	if !deploymentSuperseded {
		supersededSpend, err = addRetiredVerifiedEVMGas(prior, revised, entries, supersededSpend)
		if err != nil {
			return nil, fmt.Errorf("retain retired EVM gas: %w", err)
		}
	}
	if !supersededSpend.EVMGasWei.IsZero() || supersededSpend.TAORao != 0 || supersededSpend.AlphaRao != 0 || supersededSpend.Registrations != 0 || supersededSpend.SubnetCreations != 0 {
		if err := applySupersededSpend(revised, supersededSpend); err != nil {
			return nil, err
		}
	}
	if err := applyFleetCommitmentRecoveries(cfg, stateDir, revised, prior, current, entries); err != nil {
		return nil, fmt.Errorf("recover expiring fleet commitments: %w", err)
	}
	if err := trimLiveCampaignEVMReserveToLimit(revised); err != nil {
		return nil, err
	}
	revised.PlanHash, err = revised.hash()
	if err != nil {
		return nil, err
	}
	if err := validatePlanBudget(revised); err != nil {
		return nil, err
	}
	revisedRemaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		return nil, err
	}
	if err := validateApprovedSetupFacts(revised, current, revisedRemaining); err != nil {
		return nil, fmt.Errorf("revised plan is not affordable from current finalized state: %w", err)
	}
	return revised, nil
}

// Recheck live safety, transaction outcomes, and topology before revising.
func BuildPlanRevision(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry) (*SetupPlan, error) {
	remaining, err := remainingPlanSpend(prior, entries)
	if err != nil {
		return nil, err
	}
	doctor := runDoctor(ctx, cfg, &doctorPlanBudget{Plan: prior, Remaining: remaining, StateDir: stateDir})
	if err := doctor.Error(); err != nil {
		return nil, fmt.Errorf("doctor must pass before revising the plan: %w", err)
	}
	recoveries, err := planRevisionTransactionRecoveries(ctx, cfg, stateDir, prior, entries)
	if err != nil {
		return nil, fmt.Errorf("prior transaction revision safety: %w", err)
	}
	if !strings.EqualFold(prior.PolicyHash, cfg.PolicyHash) {
		if err := validatePolicyRevisionOnChain(ctx, cfg, stateDir, prior); err != nil {
			return nil, fmt.Errorf("policy revision finalized-chain safety: %w", err)
		}
	}
	current, err := ReadSetupFacts(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("read finalized setup facts for plan revision: %w", err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, err
	}
	migration, err := observeCoordinatorUpgradeMigration(ctx, cfg, stateDir, prior, current, entries, roles)
	if err != nil {
		return nil, fmt.Errorf("coordinator upgrade finalized baseline: %w", err)
	}
	revised, err := buildPlanRevisionFromFactsWithAllRecoveries(cfg, stateDir, prior, current, entries, time.Now().UTC(), migration, recoveries)
	if err != nil {
		return nil, err
	}
	priorSuperseded := map[string]bool{}
	for _, deployment := range prior.SupersededDeployments {
		hash, hashErr := canonicalHashHex(deployment)
		if hashErr != nil {
			return nil, hashErr
		}
		priorSuperseded[hash] = true
	}
	for _, deployment := range revised.SupersededDeployments {
		hash, hashErr := canonicalHashHex(deployment)
		if hashErr != nil {
			return nil, hashErr
		}
		if priorSuperseded[hash] {
			continue
		}
		upgrade := CoordinatorUpgrade{}
		if deployment.InitialNonce == prior.Deployment.InitialNonce {
			upgrade = prior.CoordinatorUpgrade
		}
		if err := validateSupersededDeploymentOnChain(ctx, cfg, roles, deployment, upgrade, current, prior, entries); err != nil {
			return nil, fmt.Errorf("immutable deployment finalized-chain safety: %w", err)
		}
	}
	return revised, nil
}

// Resolve the active immutable plan, construct an initial plan, or construct a
// read-only revision when a valid stored ancestor no longer matches the locked
// release/configuration.
func BuildPlanForState(ctx context.Context, cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err == nil {
		entries, readErr := readJournalEntries(stateDir)
		if readErr != nil {
			return nil, readErr
		}
		revisionRequired, recoveryErr := fleetCommitmentRecoveryRequired(ctx, cfg, stateDir, plan, entries)
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		if revisionRequired {
			return BuildPlanRevision(ctx, cfg, stateDir, plan, entries)
		}
		return plan, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return BuildPlan(ctx, cfg)
	}
	if !errors.Is(err, errPersistedPlanIdentityMismatch) {
		return nil, err
	}
	prior, err := readPersistedPlan(stateDir)
	if err != nil {
		return nil, err
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return nil, err
	}
	return BuildPlanRevision(ctx, cfg, stateDir, prior, entries)
}
