package main

// validator_view_fault.go drives one deterministic operator-equivocation
// rehearsal through the real server /verify and validator scoring modules. A
// selected boundary fleet is withheld from one validator only; another
// validator continues measuring it. This proves that top-200 admission is a
// validator-local decision rather than a coordinator-supplied global list.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/urnetwork/server"
	servercontroller "github.com/urnetwork/server/controller"
)

const (
	validatorViewFilterSchema           = servercontroller.VerifySimulationAssignmentFilterSchemaV2
	validatorViewRestoreReceiptSchema   = "urnetwork-sim-verify-assignment-filter-restore-v1"
	validatorViewFilterPlanHashEnv      = servercontroller.VerifySimulationAssignmentFilterPlanHashEnv
	validatorViewFilterMaximumRules     = 16
	validatorViewFilterMaximumVPKs      = 8
	validatorViewFilterMaximumClientIDs = 4096
	validatorLocalHeadBoundaryValidator = 1
	validatorLocalHeadBoundaryFleet     = 4
	validatorLocalHeadBoundaryFaultID   = "validator-local-head-boundary"
	validatorLocalHeadBoundaryFaultKind = "validator-view-filter"
)

type validatorViewFilterRule struct {
	RuleID            string   `json:"rule_id"`
	ValidatorVPKs     []string `json:"validator_vpks"`
	ExcludedClientIDs []string `json:"excluded_client_ids"`
}

type validatorViewFilterFile struct {
	Schema       string                    `json:"schema"`
	DeploymentID string                    `json:"deployment_id"`
	PlanHash     string                    `json:"plan_hash"`
	ChainID      uint64                    `json:"chain_id"`
	GenesisHash  string                    `json:"genesis_hash"`
	Netuid       uint64                    `json:"netuid"`
	Coordinator  string                    `json:"coordinator"`
	OperatorNo   uint64                    `json:"operator_no"`
	Rules        []validatorViewFilterRule `json:"rules"`
}

// A durable removal intent closes the crash window between atomically
// rewriting the filter and clearing the active-fault ledger. It carries the
// exact pre-removal file, so absence is idempotent only after a plan-bound
// intent proved that the expected rule existed with its exact contents.
type validatorViewRestoreReceipt struct {
	Schema        string                  `json:"schema"`
	DeploymentID  string                  `json:"deployment_id"`
	PlanHash      string                  `json:"plan_hash"`
	ChainID       uint64                  `json:"chain_id"`
	GenesisHash   string                  `json:"genesis_hash"`
	Netuid        uint64                  `json:"netuid"`
	Coordinator   string                  `json:"coordinator"`
	OperatorNo    uint64                  `json:"operator_no"`
	Rule          validatorViewFilterRule `json:"rule"`
	PreFilter     validatorViewFilterFile `json:"pre_filter"`
	PreFilterHash string                  `json:"pre_filter_hash"`
}

var validatorViewFilterMu sync.Mutex

func verifyAssignmentFilterPath(stateDir string, operator int) string {
	return filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "verify-assignment-filter.json")
}

func validatorViewRestoreReceiptPath(stateDir string, operator int, ruleID string) string {
	return filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "verify-assignment-filter.restore-"+ruleID+".json")
}

func validatorViewFaultTarget(operator, validatorID int) string {
	return fmt.Sprintf("operator-%d-validator-%d-head-view", operator, validatorID)
}

func lifecycleValidatorViewFaultTarget(operator int) string {
	return fmt.Sprintf("operator-%d-all-validators-fleet-lifecycle-view", operator)
}

func validatorLocalHeadBoundaryFault(cfg *ResolvedConfig, head scenarioFaultSpec) (scenarioFaultSpec, error) {
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.Validators < 2 || cfg.Config.Topology.HeadFleets < validatorLocalHeadBoundaryFleet || head.ID != "head-boundary" || head.Kind != "miner-control" || head.TriggerOffsetBlocks < 2 {
		return scenarioFaultSpec{}, errors.New("validator-local head-boundary fault geometry is unavailable")
	}
	firstMiner := fleetMemberMinerIndex(cfg, validatorLocalHeadBoundaryFleet, 1)
	operator := operatorForMiner(cfg, firstMiner)
	if operator < 1 {
		return scenarioFaultSpec{}, errors.New("validator-local head-boundary fleet has no operator")
	}
	for member := 2; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		if operatorForMiner(cfg, fleetMemberMinerIndex(cfg, validatorLocalHeadBoundaryFleet, member)) != operator {
			return scenarioFaultSpec{}, errors.New("validator-local head-boundary fleet crosses operators")
		}
	}
	// Envelope the global boundary fault by one finalized block on each side.
	// This prevents an apply/restore ordering race from producing one last
	// common-view proof at either edge.
	return scenarioFaultSpec{
		ID: validatorLocalHeadBoundaryFaultID, Kind: validatorLocalHeadBoundaryFaultKind,
		Targets:     []string{validatorViewFaultTarget(operator, validatorLocalHeadBoundaryValidator)},
		Impacts:     []string{fmt.Sprintf("validator-%d", validatorLocalHeadBoundaryValidator)},
		ValidatorID: validatorLocalHeadBoundaryValidator, FleetIndex: validatorLocalHeadBoundaryFleet,
		RestoreCondition: "validator-local-head-boundary-diverged", MinimumDurationBlocks: cfg.Config.Scenarios.QualityFaultDurationBlocks,
		TriggerOffsetBlocks: head.TriggerOffsetBlocks - 1, DurationBlocks: head.DurationBlocks + 2,
	}, nil
}

func validatorViewRuleIDValid(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' || value[len(value)-1] == '-' {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func (d *liveScenarioFaultDriver) validatorViewFilterIdentity(operator int) (validatorViewFilterFile, error) {
	if d.cfg == nil || d.cfg.Config == nil || d.cfg.Public == nil || operator < 1 || operator > d.cfg.Config.Topology.Operators {
		return validatorViewFilterFile{}, errors.New("validator view filter identity is unavailable")
	}
	if _, ok := evidenceFixedHex(d.planHash, 32); !ok || d.planHash != strings.ToLower(d.planHash) {
		return validatorViewFilterFile{}, errors.New("validator view filter plan hash is not canonical")
	}
	if _, ok := evidenceFixedHex(d.cfg.Public.Chain.GenesisHash, 32); !ok || d.cfg.Public.Chain.GenesisHash != strings.ToLower(d.cfg.Public.Chain.GenesisHash) {
		return validatorViewFilterFile{}, errors.New("validator view filter genesis hash is not canonical")
	}
	coordinator := strings.ToLower(d.coordinator)
	if _, ok := evidenceFixedHex(coordinator, 20); !ok {
		return validatorViewFilterFile{}, errors.New("validator view filter coordinator is not canonical")
	}
	return validatorViewFilterFile{
		Schema: validatorViewFilterSchema, DeploymentID: d.cfg.Config.Deployment.DeploymentID,
		PlanHash: d.planHash, ChainID: d.cfg.ChainID, GenesisHash: d.cfg.Public.Chain.GenesisHash,
		Netuid: uint64(d.cfg.Netuid), Coordinator: coordinator, OperatorNo: uint64(operator),
	}, nil
}

func validateValidatorViewFilter(filter validatorViewFilterFile, expected validatorViewFilterFile) error {
	if filter.Schema != validatorViewFilterSchema || filter.DeploymentID != expected.DeploymentID || filter.PlanHash != expected.PlanHash || filter.ChainID != expected.ChainID || filter.GenesisHash != expected.GenesisHash || filter.Netuid != expected.Netuid || filter.Coordinator != expected.Coordinator || filter.OperatorNo != expected.OperatorNo || len(filter.Rules) == 0 || len(filter.Rules) > validatorViewFilterMaximumRules {
		return errors.New("validator view filter deployment identity or rule census is invalid")
	}
	pairs := make(map[string]bool)
	priorRuleID := ""
	for _, rule := range filter.Rules {
		if !validatorViewRuleIDValid(rule.RuleID) || rule.RuleID <= priorRuleID || len(rule.ValidatorVPKs) == 0 || len(rule.ValidatorVPKs) > validatorViewFilterMaximumVPKs || len(rule.ExcludedClientIDs) == 0 || len(rule.ExcludedClientIDs) > validatorViewFilterMaximumClientIDs || !sort.StringsAreSorted(rule.ValidatorVPKs) || !sort.StringsAreSorted(rule.ExcludedClientIDs) {
			return errors.New("validator view filter rule is empty, unordered, duplicated, or out of bounds")
		}
		priorRuleID = rule.RuleID
		priorVPK := ""
		for _, vpk := range rule.ValidatorVPKs {
			if _, ok := evidenceFixedHex("0x"+vpk, 32); !ok || vpk != strings.ToLower(vpk) || vpk <= priorVPK {
				return errors.New("validator view filter VPK is not canonical and unique")
			}
			priorVPK = vpk
			priorClient := ""
			for _, clientID := range rule.ExcludedClientIDs {
				parsed, err := server.ParseId(clientID)
				if err != nil || parsed.String() != clientID || clientID <= priorClient {
					return errors.New("validator view filter client id is not canonical and unique")
				}
				priorClient = clientID
				pair := vpk + "\x00" + clientID
				if pairs[pair] {
					return errors.New("validator view filter rules contain an ambiguous duplicate validator/client pair")
				}
				pairs[pair] = true
			}
		}
	}
	return nil
}

func validateValidatorViewRestoreReceipt(receipt validatorViewRestoreReceipt, identity validatorViewFilterFile, expectedRule validatorViewFilterRule) error {
	if receipt.Schema != validatorViewRestoreReceiptSchema || receipt.DeploymentID != identity.DeploymentID || receipt.PlanHash != identity.PlanHash || receipt.ChainID != identity.ChainID || receipt.GenesisHash != identity.GenesisHash || receipt.Netuid != identity.Netuid || receipt.Coordinator != identity.Coordinator || receipt.OperatorNo != identity.OperatorNo {
		return errors.New("validator view restore receipt identity is invalid")
	}
	if err := validateValidatorViewFilter(receipt.PreFilter, identity); err != nil {
		return fmt.Errorf("validator view restore receipt pre-filter: %w", err)
	}
	ruleHash, ruleErr := canonicalHashHex(receipt.Rule)
	expectedRuleHash, expectedRuleErr := canonicalHashHex(expectedRule)
	if ruleErr != nil || expectedRuleErr != nil || ruleHash != expectedRuleHash {
		return errors.New("validator view restore receipt rule differs from its exact fault")
	}
	found := false
	for _, rule := range receipt.PreFilter.Rules {
		candidateHash, candidateErr := canonicalHashHex(rule)
		if candidateErr == nil && rule.RuleID == expectedRule.RuleID && candidateHash == expectedRuleHash {
			found = true
		}
	}
	preFilterHash, hashErr := canonicalHashHex(receipt.PreFilter)
	if !found || hashErr != nil || receipt.PreFilterHash != preFilterHash {
		return errors.New("validator view restore receipt does not bind its exact pre-removal filter")
	}
	return nil
}

func writeValidatorViewRestoreReceipt(path string, receipt validatorViewRestoreReceipt) error {
	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(encoded, '\n'), 0o600)
}

func (d *liveScenarioFaultDriver) validatorViewRule(spec scenarioFaultSpec) (int, string, validatorViewFilterRule, error) {
	if d.cfg == nil || d.cfg.Config == nil || spec.Kind != validatorLocalHeadBoundaryFaultKind {
		return 0, "", validatorViewFilterRule{}, errors.New("validator view filter specification is invalid")
	}
	lifecycle := len(spec.FleetIndices) != 0
	fleets := []int{spec.FleetIndex}
	if lifecycle {
		if spec.ValidatorID != 0 || spec.FleetIndex != 0 || len(spec.FleetIndices) != 1 {
			return 0, "", validatorViewFilterRule{}, errors.New("fleet lifecycle validator view filter must identify exactly one operator-local fleet")
		}
		fleets = append([]int(nil), spec.FleetIndices...)
	} else if spec.ValidatorID < 1 || spec.ValidatorID > d.cfg.Config.Topology.Validators || spec.FleetIndex < 1 || spec.FleetIndex > d.cfg.Config.Topology.fleetCandidates() {
		return 0, "", validatorViewFilterRule{}, errors.New("validator view filter specification is invalid")
	}
	operator := operatorForMiner(d.cfg, fleetMemberMinerIndex(d.cfg, fleets[0], 1))
	wantTarget := validatorViewFaultTarget(operator, spec.ValidatorID)
	if lifecycle {
		wantTarget = lifecycleValidatorViewFaultTarget(operator)
	}
	if operator < 1 || len(spec.Targets) != 1 || spec.Targets[0] != wantTarget {
		return 0, "", validatorViewFilterRule{}, errors.New("validator view filter target is not canonical")
	}
	var roles RoleSecrets
	if err := readJSONFile(filepath.Join(d.stateDir, "secrets", "roles.json"), &roles); err != nil {
		return 0, "", validatorViewFilterRule{}, err
	}
	if roles.Schema != "urnetwork-sim-role-secrets-v1" || roles.DeploymentID != d.cfg.Config.Deployment.DeploymentID {
		return 0, "", validatorViewFilterRule{}, errors.New("validator view filter role store identity is invalid")
	}
	validatorVPKs := make([]string, 0, d.cfg.Config.Topology.Validators)
	validatorIDs := []int{spec.ValidatorID}
	if lifecycle {
		validatorIDs = validatorIDs[:0]
		for validator := 1; validator <= d.cfg.Config.Topology.Validators; validator++ {
			validatorIDs = append(validatorIDs, validator)
		}
	}
	for _, validatorID := range validatorIDs {
		validatorRole, ok := roles.Clients[fmt.Sprintf("validator-%d-no-%d", validatorID, operator)]
		if !ok || len(validatorRole.PublicKeyHex) != 64 || validatorRole.PublicKeyHex != strings.ToLower(validatorRole.PublicKeyHex) {
			return 0, "", validatorViewFilterRule{}, errors.New("validator view filter validator key is invalid")
		}
		if _, err := hex.DecodeString(validatorRole.PublicKeyHex); err != nil {
			return 0, "", validatorViewFilterRule{}, errors.New("validator view filter validator key is invalid")
		}
		validatorVPKs = append(validatorVPKs, validatorRole.PublicKeyHex)
	}
	if lifecycle && len(validatorVPKs) != d.cfg.Config.Topology.Validators {
		return 0, "", validatorViewFilterRule{}, errors.New("fleet lifecycle validator key census is incomplete")
	}
	sort.Strings(validatorVPKs)
	clientIDs := make([]string, 0, len(fleets)*d.cfg.Config.Topology.ClientsPerHeadFleet)
	for _, fleet := range fleets {
		if fleet < 1 || fleet > d.cfg.Config.Topology.fleetCandidates() {
			return 0, "", validatorViewFilterRule{}, errors.New("validator view filter fleet is out of range")
		}
		for member := 1; member <= d.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miner := fleetMemberMinerIndex(d.cfg, fleet, member)
			if operatorForMiner(d.cfg, miner) != operator {
				return 0, "", validatorViewFilterRule{}, errors.New("validator view filter fleet crosses operators")
			}
			role, found := roles.Clients[fmt.Sprintf("miner-%d", miner)]
			raw, err := hex.DecodeString(role.ClientIDHex)
			if !found || err != nil || len(raw) != 16 {
				return 0, "", validatorViewFilterRule{}, fmt.Errorf("validator view filter miner-%d client id is unavailable", miner)
			}
			var clientID server.Id
			copy(clientID[:], raw)
			if clientID == (server.Id{}) {
				return 0, "", validatorViewFilterRule{}, fmt.Errorf("validator view filter miner-%d client id is zero", miner)
			}
			clientIDs = append(clientIDs, clientID.String())
		}
	}
	sort.Strings(clientIDs)
	for index := 1; index < len(clientIDs); index++ {
		if clientIDs[index] == clientIDs[index-1] {
			return 0, "", validatorViewFilterRule{}, errors.New("validator view filter client ids are duplicated")
		}
	}
	if !validatorViewRuleIDValid(spec.ID) {
		return 0, "", validatorViewFilterRule{}, errors.New("validator view filter rule id is not canonical")
	}
	return operator, wantTarget, validatorViewFilterRule{RuleID: spec.ID, ValidatorVPKs: validatorVPKs, ExcludedClientIDs: clientIDs}, nil
}

func (d *liveScenarioFaultDriver) validatorViewProcess(operator int, target string) (FaultProcessEvidence, error) {
	states, specs, err := d.processSnapshot()
	if err != nil {
		return FaultProcessEvidence{}, err
	}
	processID := fmt.Sprintf("operator-%d-api", operator)
	state, stateOK := states[processID]
	process, specOK := specs[processID]
	if !stateOK || !specOK || state.PID <= 1 || !state.Healthy || process.Env[servercontroller.VerifySimulationAssignmentFilterFileEnv] != verifyAssignmentFilterPath(d.stateDir, operator) || process.Env[validatorViewFilterPlanHashEnv] != d.planHash {
		return FaultProcessEvidence{}, fmt.Errorf("validator view filter has no healthy configured operator API %s", processID)
	}
	return FaultProcessEvidence{ID: target, Role: "operator-api-validator-view", Identity: process.Identity, PID: state.PID}, nil
}

func (d *liveScenarioFaultDriver) applyValidatorViewFilter(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operator, target, rule, err := d.validatorViewRule(spec)
	if err != nil {
		return nil, err
	}
	process, err := d.validatorViewProcess(operator, target)
	if err != nil {
		return nil, err
	}
	identity, err := d.validatorViewFilterIdentity(operator)
	if err != nil {
		return nil, err
	}
	path := verifyAssignmentFilterPath(d.stateDir, operator)
	restorePath := validatorViewRestoreReceiptPath(d.stateDir, operator, rule.RuleID)
	validatorViewFilterMu.Lock()
	defer validatorViewFilterMu.Unlock()
	filter := identity
	if raw, readErr := os.ReadFile(path); readErr == nil {
		if json.Unmarshal(raw, &filter) != nil {
			return nil, errors.New("validator view filter contains invalid JSON")
		}
		if err := validateValidatorViewFilter(filter, identity); err != nil {
			return nil, err
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	// A new application is a new restoration lifecycle even when it reuses the
	// same campaign rule. Clear only the exact prior receipt after the current
	// filter has passed identity validation, before adopting or adding the rule.
	if err := os.Remove(restorePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, existing := range filter.Rules {
		if existing.RuleID != rule.RuleID {
			continue
		}
		left, _ := canonicalHashHex(existing)
		right, _ := canonicalHashHex(rule)
		if left != right {
			return nil, errors.New("validator view filter rule id is already bound to different contents")
		}
		return []FaultProcessEvidence{process}, nil
	}
	filter.Rules = append(filter.Rules, rule)
	sort.Slice(filter.Rules, func(i, j int) bool { return filter.Rules[i].RuleID < filter.Rules[j].RuleID })
	if err := validateValidatorViewFilter(filter, identity); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(filter, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(path, append(encoded, '\n'), 0o600); err != nil {
		return nil, err
	}
	return []FaultProcessEvidence{process}, nil
}

func (d *liveScenarioFaultDriver) restoreValidatorViewFilter(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operator, target, expectedRule, err := d.validatorViewRule(spec)
	if err != nil {
		return nil, err
	}
	process, err := d.validatorViewProcess(operator, target)
	if err != nil {
		return nil, err
	}
	path := verifyAssignmentFilterPath(d.stateDir, operator)
	restorePath := validatorViewRestoreReceiptPath(d.stateDir, operator, expectedRule.RuleID)
	identity, err := d.validatorViewFilterIdentity(operator)
	if err != nil {
		return nil, err
	}
	validatorViewFilterMu.Lock()
	defer validatorViewFilterMu.Unlock()
	var receipt validatorViewRestoreReceipt
	receiptFound := false
	if raw, readErr := os.ReadFile(restorePath); readErr == nil {
		if json.Unmarshal(raw, &receipt) != nil {
			return nil, errors.New("validator view restore receipt contains invalid JSON")
		}
		if err := validateValidatorViewRestoreReceipt(receipt, identity, expectedRule); err != nil {
			return nil, err
		}
		receiptFound = true
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && receiptFound {
		return []FaultProcessEvidence{process}, nil
	}
	if err != nil {
		return nil, err
	}
	var filter validatorViewFilterFile
	if json.Unmarshal(raw, &filter) != nil {
		return nil, errors.New("validator view filter contains invalid JSON")
	}
	if err := validateValidatorViewFilter(filter, identity); err != nil {
		return nil, err
	}
	index := -1
	for candidate := range filter.Rules {
		if filter.Rules[candidate].RuleID == expectedRule.RuleID {
			index = candidate
			left, _ := canonicalHashHex(filter.Rules[candidate])
			right, _ := canonicalHashHex(expectedRule)
			if left != right {
				return nil, errors.New("validator view filter exact removal contents differ")
			}
			break
		}
	}
	if index < 0 {
		if receiptFound {
			return []FaultProcessEvidence{process}, nil
		}
		return nil, errors.New("validator view filter exact removal rule is absent")
	}
	if !receiptFound {
		preFilterHash, hashErr := canonicalHashHex(filter)
		if hashErr != nil {
			return nil, hashErr
		}
		receipt = validatorViewRestoreReceipt{
			Schema: validatorViewRestoreReceiptSchema, DeploymentID: identity.DeploymentID, PlanHash: identity.PlanHash,
			ChainID: identity.ChainID, GenesisHash: identity.GenesisHash, Netuid: identity.Netuid,
			Coordinator: identity.Coordinator, OperatorNo: identity.OperatorNo,
			Rule: expectedRule, PreFilter: filter, PreFilterHash: preFilterHash,
		}
		if err := writeValidatorViewRestoreReceipt(restorePath, receipt); err != nil {
			return nil, err
		}
	}
	filter.Rules = append(filter.Rules[:index], filter.Rules[index+1:]...)
	if len(filter.Rules) == 0 {
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else {
		if err := validateValidatorViewFilter(filter, identity); err != nil {
			return nil, err
		}
		encoded, err := json.MarshalIndent(filter, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := atomicWrite(path, append(encoded, '\n'), 0o600); err != nil {
			return nil, err
		}
	}
	return []FaultProcessEvidence{process}, nil
}

// Removes only the simulator-owned fixed paths. This covers a host crash
// after the filter file was durably renamed but before the active-fault ledger
// was committed; ordinary clean starts therefore cannot inherit a stale view.
func (d *liveScenarioFaultDriver) removeOrphanValidatorViewFilters() error {
	if d.cfg == nil || d.cfg.Config == nil {
		return nil
	}
	for operator := 1; operator <= d.cfg.Config.Topology.Operators; operator++ {
		if err := os.Remove(verifyAssignmentFilterPath(d.stateDir, operator)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		directory := filepath.Dir(verifyAssignmentFilterPath(d.stateDir, operator))
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "verify-assignment-filter.restore-") || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
