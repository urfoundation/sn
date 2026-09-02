package main

// validator_view_fault.go drives one deterministic operator-equivocation
// rehearsal through the real server /verify and validator scoring modules. A
// selected boundary fleet is withheld from one validator only; another
// validator continues measuring it. This proves that top-200 admission is a
// validator-local decision rather than a coordinator-supplied global list.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urnetwork/server"
	servercontroller "github.com/urnetwork/server/controller"
)

const (
	validatorViewFilterSchema           = "urnetwork-sim-verify-assignment-filter-v1"
	validatorLocalHeadBoundaryValidator = 1
	validatorLocalHeadBoundaryFleet     = 4
	validatorLocalHeadBoundaryFaultID   = "validator-local-head-boundary"
	validatorLocalHeadBoundaryFaultKind = "validator-view-filter"
)

type validatorViewFilterFile struct {
	Schema            string   `json:"schema"`
	ValidatorVPK      string   `json:"validator_vpk"`
	ExcludedClientIDs []string `json:"excluded_client_ids"`
}

func verifyAssignmentFilterPath(stateDir string, operator int) string {
	return filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "verify-assignment-filter.json")
}

func validatorViewFaultTarget(operator, validatorID int) string {
	return fmt.Sprintf("operator-%d-validator-%d-head-view", operator, validatorID)
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

func (d *liveScenarioFaultDriver) validatorViewFilter(spec scenarioFaultSpec) (int, string, []byte, error) {
	if d.cfg == nil || d.cfg.Config == nil || spec.Kind != validatorLocalHeadBoundaryFaultKind || spec.ValidatorID < 1 || spec.ValidatorID > d.cfg.Config.Topology.Validators || spec.FleetIndex < 1 || spec.FleetIndex > d.cfg.Config.Topology.fleetCandidates() {
		return 0, "", nil, errors.New("validator view filter specification is invalid")
	}
	operator := operatorForMiner(d.cfg, fleetMemberMinerIndex(d.cfg, spec.FleetIndex, 1))
	wantTarget := validatorViewFaultTarget(operator, spec.ValidatorID)
	if operator < 1 || len(spec.Targets) != 1 || spec.Targets[0] != wantTarget {
		return 0, "", nil, errors.New("validator view filter target is not canonical")
	}
	var roles RoleSecrets
	if err := readJSONFile(filepath.Join(d.stateDir, "secrets", "roles.json"), &roles); err != nil {
		return 0, "", nil, err
	}
	if roles.Schema != "urnetwork-sim-role-secrets-v1" || roles.DeploymentID != d.cfg.Config.Deployment.DeploymentID {
		return 0, "", nil, errors.New("validator view filter role store identity is invalid")
	}
	validatorRole, ok := roles.Clients[fmt.Sprintf("validator-%d-no-%d", spec.ValidatorID, operator)]
	if !ok || len(validatorRole.PublicKeyHex) != 64 || validatorRole.PublicKeyHex != strings.ToLower(validatorRole.PublicKeyHex) {
		return 0, "", nil, errors.New("validator view filter validator key is invalid")
	}
	if _, err := hex.DecodeString(validatorRole.PublicKeyHex); err != nil {
		return 0, "", nil, errors.New("validator view filter validator key is invalid")
	}
	clientIDs := make([]string, 0, d.cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= d.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(d.cfg, spec.FleetIndex, member)
		if operatorForMiner(d.cfg, miner) != operator {
			return 0, "", nil, errors.New("validator view filter fleet crosses operators")
		}
		role, found := roles.Clients[fmt.Sprintf("miner-%d", miner)]
		raw, err := hex.DecodeString(role.ClientIDHex)
		if !found || err != nil || len(raw) != 16 {
			return 0, "", nil, fmt.Errorf("validator view filter miner-%d client id is unavailable", miner)
		}
		var clientID server.Id
		copy(clientID[:], raw)
		if clientID == (server.Id{}) {
			return 0, "", nil, fmt.Errorf("validator view filter miner-%d client id is zero", miner)
		}
		clientIDs = append(clientIDs, clientID.String())
	}
	sort.Strings(clientIDs)
	for index := 1; index < len(clientIDs); index++ {
		if clientIDs[index] == clientIDs[index-1] {
			return 0, "", nil, errors.New("validator view filter client ids are duplicated")
		}
	}
	encoded, err := json.MarshalIndent(validatorViewFilterFile{
		Schema: validatorViewFilterSchema, ValidatorVPK: validatorRole.PublicKeyHex, ExcludedClientIDs: clientIDs,
	}, "", "  ")
	if err != nil {
		return 0, "", nil, err
	}
	return operator, wantTarget, append(encoded, '\n'), nil
}

func (d *liveScenarioFaultDriver) validatorViewProcess(operator int, target string) (FaultProcessEvidence, error) {
	states, specs, err := d.processSnapshot()
	if err != nil {
		return FaultProcessEvidence{}, err
	}
	processID := fmt.Sprintf("operator-%d-api", operator)
	state, stateOK := states[processID]
	process, specOK := specs[processID]
	if !stateOK || !specOK || state.PID <= 1 || !state.Healthy || process.Env[servercontroller.VerifySimulationAssignmentFilterFileEnv] != verifyAssignmentFilterPath(d.stateDir, operator) {
		return FaultProcessEvidence{}, fmt.Errorf("validator view filter has no healthy configured operator API %s", processID)
	}
	return FaultProcessEvidence{ID: target, Role: "operator-api-validator-view", Identity: process.Identity, PID: state.PID}, nil
}

func (d *liveScenarioFaultDriver) applyValidatorViewFilter(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operator, target, encoded, err := d.validatorViewFilter(spec)
	if err != nil {
		return nil, err
	}
	process, err := d.validatorViewProcess(operator, target)
	if err != nil {
		return nil, err
	}
	path := verifyAssignmentFilterPath(d.stateDir, operator)
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("validator view filter path is not clean: %v", err)
	}
	if err := atomicWrite(path, encoded, 0o600); err != nil {
		return nil, err
	}
	return []FaultProcessEvidence{process}, nil
}

func (d *liveScenarioFaultDriver) restoreValidatorViewFilter(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operator, target, expected, err := d.validatorViewFilter(spec)
	if err != nil {
		return nil, err
	}
	process, err := d.validatorViewProcess(operator, target)
	if err != nil {
		return nil, err
	}
	path := verifyAssignmentFilterPath(d.stateDir, operator)
	actual, err := os.ReadFile(path)
	if err == nil && !bytes.Equal(actual, expected) {
		return nil, errors.New("validator view filter contents differ from the active fault")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := os.Remove(path); err != nil {
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
	}
	return nil
}
