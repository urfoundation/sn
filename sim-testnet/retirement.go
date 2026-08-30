package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/stabi"
)

type RetirementPlan struct {
	Schema         string      `json:"schema"`
	Release        string      `json:"release"`
	DeploymentID   string      `json:"deployment_id"`
	ChainID        uint64      `json:"chain_id"`
	GenesisHash    string      `json:"genesis_hash"`
	Netuid         uint16      `json:"netuid"`
	Coordinator    string      `json:"coordinator"`
	SetupPlanHash  string      `json:"setup_plan_hash"`
	CurrentEpoch   uint64      `json:"current_epoch"`
	EffectiveEpoch uint64      `json:"effective_epoch"`
	Actions        []Action    `json:"actions"`
	MaximumSpend   Spend       `json:"maximum_spend"`
	ReservedGasWei DecimalUint `json:"reserved_gas_wei"`
	PlanHash       string      `json:"plan_hash"`
	GeneratedAt    string      `json:"generated_at,omitempty"`
}

func (p RetirementPlan) hash() (string, error) {
	p.PlanHash = ""
	p.GeneratedAt = ""
	return canonicalHashHex(p)
}

func retirementGasReserve(setup *SetupPlan) (DecimalUint, error) {
	for _, action := range setup.Actions {
		if action.ID == "retirement.evm-gas-reserve" && action.Kind == "budget-reserve" && !action.Spend.EVMGasWei.IsZero() {
			return action.Spend.EVMGasWei, nil
		}
	}
	return "", errors.New("approved setup plan has no retirement gas reserve")
}

func operatorVersion(values []any) (stabi.STCoordinatorOperatorVersion, error) {
	if len(values) != 1 {
		return stabi.STCoordinatorOperatorVersion{}, fmt.Errorf("operator getter returned %d values", len(values))
	}
	converted, ok := abi.ConvertType(values[0], new(stabi.STCoordinatorOperatorVersion)).(*stabi.STCoordinatorOperatorVersion)
	if !ok || converted == nil {
		return stabi.STCoordinatorOperatorVersion{}, fmt.Errorf("operator getter returned %T", values[0])
	}
	return *converted, nil
}

func buildRetirementPlan(cfg *ResolvedConfig, setup *SetupPlan, deployment *ContractDeployment, currentEpoch uint64, operators []stabi.STCoordinatorOperatorVersion, generatedAt time.Time) (*RetirementPlan, error) {
	if setup == nil || deployment == nil || deployment.CoordinatorProxy == (common.Address{}) {
		return nil, errors.New("retirement requires the installed deployment and approved setup plan")
	}
	if len(operators) != cfg.Config.Topology.Operators || currentEpoch == ^uint64(0) {
		return nil, errors.New("retirement operator snapshot is incomplete")
	}
	reserved, err := retirementGasReserve(setup)
	if err != nil {
		return nil, err
	}
	effective := currentEpoch + 1
	p := &RetirementPlan{
		Schema: "urnetwork-sim-retirement-plan-v1", Release: "1.0", DeploymentID: cfg.Config.Deployment.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		Coordinator: deployment.CoordinatorProxy.Hex(), SetupPlanHash: setup.PlanHash,
		CurrentEpoch: currentEpoch, EffectiveEpoch: effective, ReservedGasWei: reserved, GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
	}
	allocated := decimalUint64(0)
	for index, operator := range operators {
		if !operator.Active || operator.DepositHotkey == ([32]byte{}) || operator.DepositSigner == (common.Address{}) || operator.RootSigner == (common.Address{}) {
			return nil, fmt.Errorf("operator %d is not an active complete operator at epoch %d", index+1, currentEpoch)
		}
		cap, capErr := divideDecimalUint(reserved, uint64(len(operators)))
		if capErr != nil {
			return nil, capErr
		}
		if index == len(operators)-1 {
			cap, capErr = subtractDecimalUint(reserved, allocated)
			if capErr != nil {
				return nil, capErr
			}
		}
		allocated, capErr = addDecimalUint(allocated, cap)
		if capErr != nil {
			return nil, capErr
		}
		maximumGasUnits, capErr := divideDecimalUint(cap, setup.MaximumEVMFeePerGasWei)
		if capErr != nil {
			return nil, capErr
		}
		action := Action{
			ID: fmt.Sprintf("operator.retire.%d.epoch.%d", index+1, effective), Kind: "evm-transaction", Target: fmt.Sprintf("no:%d", index+1),
			Description: "schedule operator inactive at the next epoch while preserving prior entitlements and claim paths",
			Parameters: map[string]string{
				"no_id": fmt.Sprint(index + 1), "effective_epoch": fmt.Sprint(effective),
				"deposit_hotkey": "0x" + hex.EncodeToString(operator.DepositHotkey[:]),
				"deposit_signer": operator.DepositSigner.Hex(), "root_signer": operator.RootSigner.Hex(),
				evmMaximumGasUnitsParameter: maximumGasUnits.String(), evmMaximumFeePerGasParameter: strconv.FormatUint(setup.MaximumEVMFeePerGasWei, 10),
			},
			Spend: Spend{EVMGasWei: cap},
		}
		hash, hashErr := actionIntentHash(action)
		if hashErr != nil {
			return nil, hashErr
		}
		action.IntentHash = hash
		p.Actions = append(p.Actions, action)
		p.MaximumSpend.EVMGasWei, capErr = addDecimalUint(p.MaximumSpend.EVMGasWei, cap)
		if capErr != nil {
			return nil, capErr
		}
	}
	if p.MaximumSpend.EVMGasWei != reserved {
		return nil, errors.New("retirement gas allocation does not equal its setup reserve")
	}
	p.PlanHash, err = p.hash()
	return p, err
}

func readRetirementPlan(ctx context.Context, cfg *ResolvedConfig, stateDir string, setup *SetupPlan) (*RetirementPlan, error) {
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		return nil, fmt.Errorf("retirement requires installed contracts: %w", err)
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
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, err
	}
	currentValues, err := contractCallAt(ctx, client, deployment.CoordinatorProxy, parsed, "currentEpoch", head.Number)
	if err != nil || len(currentValues) != 1 {
		return nil, stateMismatchError(err, "read finalized current epoch returned %d values", len(currentValues))
	}
	current, ok := currentValues[0].(*big.Int)
	if !ok || !current.IsUint64() {
		return nil, fmt.Errorf("currentEpoch returned %T", currentValues[0])
	}
	operators := make([]stabi.STCoordinatorOperatorVersion, cfg.Config.Topology.Operators)
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		values, readErr := contractCallAt(ctx, client, deployment.CoordinatorProxy, parsed, "operatorAt", head.Number, big.NewInt(int64(noID)), current)
		if readErr != nil {
			return nil, readErr
		}
		operators[noID-1], err = operatorVersion(values)
		if err != nil {
			return nil, err
		}
	}
	return buildRetirementPlan(cfg, setup, deployment, current.Uint64(), operators, time.Now().UTC())
}

func runRetirement(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) error {
	setup, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		return fmt.Errorf("retirement requires a completed persisted setup plan: %w", err)
	}
	plan, err := readRetirementPlan(ctx, cfg, stateDir, setup)
	if err != nil {
		return err
	}
	if !options.Apply {
		return printResult(options.Format, map[string]any{
			"dry_run": true, "command": "retire", "plan": plan,
			"apply_command": fmt.Sprintf("sim-testnet retire --config %s --state-dir %s --apply --plan-hash %s", cfg.ConfigPath, stateDir, plan.PlanHash),
		}, nil)
	}
	if err := requireApproved(true, options.PlanHash, plan.PlanHash); err != nil {
		return err
	}
	if err := ensurePrivateDir(stateDir); err != nil {
		return err
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		return err
	}
	defer journal.Close()
	remaining, err := remainingPlanSpend(setup, journal.Entries())
	if err != nil {
		return err
	}
	doctor := runDoctor(ctx, cfg, &doctorPlanBudget{Plan: setup, Remaining: remaining, StateDir: stateDir})
	if err := doctor.Error(); err != nil {
		return fmt.Errorf("doctor must pass immediately before retirement apply: %w", err)
	}
	roles, err := LoadOrWriteRoleSecrets(cfg, stateDir)
	if err != nil {
		return err
	}
	executionPlan := *setup
	executionPlan.PlanHash = plan.PlanHash
	executionPlan.Actions = append([]Action(nil), plan.Actions...)
	executor, err := NewExecutor(ctx, cfg, stateDir, &executionPlan, journal, roles)
	if err != nil {
		return err
	}
	defer executor.Close()
	for _, action := range plan.Actions {
		if err := executor.Execute(ctx, action); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(stateDir, "public", "retirement-plan.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return printResult(options.Format, map[string]any{
		"schema": "urnetwork-sim-retirement-result-v1", "deployment_id": cfg.Config.Deployment.DeploymentID,
		"plan_hash": plan.PlanHash, "effective_epoch": plan.EffectiveEpoch, "status": "scheduled",
		"note": "keep services and the immutable vault available until all prior epochs settle and claims expire/carry",
	}, nil)
}

func parseRetirementAction(a Action) (uint64, uint64, [32]byte, common.Address, common.Address, error) {
	var hotkey [32]byte
	noID, err := strconv.ParseUint(a.Parameters["no_id"], 10, 64)
	if err != nil || noID == 0 {
		return 0, 0, hotkey, common.Address{}, common.Address{}, errors.New("retirement no_id is invalid")
	}
	effective, err := strconv.ParseUint(a.Parameters["effective_epoch"], 10, 64)
	if err != nil || effective == 0 {
		return 0, 0, hotkey, common.Address{}, common.Address{}, errors.New("retirement effective_epoch is invalid")
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(a.Parameters["deposit_hotkey"], "0x"))
	if err != nil || len(raw) != 32 {
		return 0, 0, hotkey, common.Address{}, common.Address{}, errors.New("retirement deposit hotkey is invalid")
	}
	copy(hotkey[:], raw)
	deposit := common.HexToAddress(a.Parameters["deposit_signer"])
	root := common.HexToAddress(a.Parameters["root_signer"])
	if deposit == (common.Address{}) || root == (common.Address{}) || deposit == root {
		return 0, 0, hotkey, common.Address{}, common.Address{}, errors.New("retirement signer set is invalid")
	}
	return noID, effective, hotkey, deposit, root, nil
}

func (e *Executor) scheduleOperatorRetirement(ctx context.Context, a Action) error {
	if err := e.ensurePayloads(ctx); err != nil {
		return err
	}
	noID, effective, hotkey, depositSigner, rootSigner, err := parseRetirementAction(a)
	if err != nil {
		return err
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	address := e.payloads.Manifest.CoordinatorProxy
	values, err := contractCall(ctx, e.owner.client, address, parsed, "operatorAt", new(big.Int).SetUint64(noID), new(big.Int).SetUint64(effective))
	if err != nil {
		return err
	}
	atEffective, err := operatorVersion(values)
	if err != nil {
		return err
	}
	if !atEffective.Active {
		if !retirementVersionMatches(atEffective, effective, hotkey, depositSigner, rootSigner) {
			return errors.New("existing operator retirement does not match the approved plan")
		}
		return nil
	}
	currentValues, err := contractCall(ctx, e.owner.client, address, parsed, "currentEpoch")
	if err != nil || len(currentValues) != 1 {
		return stateMismatchError(err, "read current epoch returned %d values", len(currentValues))
	}
	current := currentValues[0].(*big.Int)
	if !current.IsUint64() || current.Uint64() >= effective {
		return errors.New("retirement approval expired; regenerate the next-epoch plan")
	}
	data, err := parsed.Pack("scheduleOperator", new(big.Int).SetUint64(noID), hotkey, depositSigner, rootSigner, false, effective)
	if err != nil {
		return err
	}
	if _, err := e.owner.Send(ctx, e.plan.PlanHash, a, &address, big.NewInt(0), data); err != nil {
		return err
	}
	postValues, err := contractCall(ctx, e.owner.client, address, parsed, "operatorAt", new(big.Int).SetUint64(noID), new(big.Int).SetUint64(effective))
	if err != nil {
		return err
	}
	post, err := operatorVersion(postValues)
	if err != nil || !retirementVersionMatches(post, effective, hotkey, depositSigner, rootSigner) {
		return errors.New("operator retirement post-state does not match the approved plan")
	}
	return nil
}
