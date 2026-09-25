//go:build linux || darwin

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Only the exact invocation-local, non-accepting testnet record can turn the
// continuation's elapsed clock forecast into an advisory. The signed plan,
// source census and monetary authority remain the admission owners.
func evidenceRelayContinuationForecastAdvisory(cfg *ResolvedConfig, plan *SetupPlan) (bool, error) {
	if !provisionalResumeEnabled(cfg) {
		return false, nil
	}
	record := cfg.provisionalResume.Record
	if cfg.Config == nil || cfg.Public == nil || plan == nil || plan.EvidenceRelayContinuation == nil || cfg.ChainID != testnetChainID || plan.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) || !strings.EqualFold(plan.GenesisHash, testnetGenesis) || record.Schema != "urnetwork-sim-provisional-resume-v1" || !record.Provisional || record.FinalAcceptance || record.PlanHash != plan.PlanHash || !validCanonicalHashHex(record.PlanHash) || record.ConfigHash != cfg.ConfigHash || plan.ConfigHash != cfg.ConfigHash || record.DeploymentID != cfg.Config.Deployment.DeploymentID || plan.DeploymentID != cfg.Config.Deployment.DeploymentID || !filepath.IsAbs(cfg.provisionalResume.RecordPath) {
		return false, errors.New("provisional relay continuation forecast requires the exact non-accepting testnet approval")
	}
	return true, nil
}

// Capture-time clock geometry stays strict. Only elapsed time after that
// approval may be advisory for an exact provisional invocation.
func validateEvidenceRelayContinuationStartupRunway(cfg *ResolvedConfig, plan *SetupPlan, current *EvidenceRelayContinuation, block uint64) error {
	advisory, approvalErr := evidenceRelayContinuationForecastAdvisory(cfg, plan)
	if approvalErr != nil {
		return approvalErr
	}
	err := validateEvidenceRelayContinuationRunway(current, block)
	if err == nil {
		return nil
	}
	if !advisory || current == nil || current.RequiredWorkBlocks == 0 || block < current.EVMHead.Number {
		return err
	}
	workCfg := *cfg
	workCfg.provisionalResume = nil
	work, workErr := evidenceRelayConfiguredWork(&workCfg)
	if workErr != nil {
		return errors.Join(err, workErr)
	}
	if clockErr := current.validateClocks(work); clockErr != nil {
		return errors.Join(err, clockErr)
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: provisional relay continuation elapsed forecast advisory; observed_block=%d required_work=%d forecast_end=%d forecast_waived=true runtime_limits_unchanged=true final_acceptance=false\n", block, current.RequiredWorkBlocks, current.EndBlock)
	return nil
}

// Once the forecast end has elapsed, startup still checks the observed ledger
// plus one bounded block and the existing restart tail without unsigned wrap.
func evidenceRelayContinuationCapacitySpan(current *EvidenceRelayContinuation, block uint64) (uint64, error) {
	if current == nil || block < current.EVMHead.Number {
		return 0, errors.New("relay continuation capacity clock precedes its approved snapshot")
	}
	if block >= current.EndBlock {
		return 1, nil
	}
	return current.EndBlock - block, nil
}

func validateEvidenceRelayContinuationRetained(c *EvidenceRelayContinuation, observed []validatorcomponent.ValidatorEvidenceTransactionV2Expected) error {
	current, err := canonicalEvidenceRelayContinuationRequests(observed)
	if err != nil {
		return err
	}
	bySlot := map[[32]byte]validatorcomponent.ValidatorEvidenceTransactionV2Expected{}
	for _, request := range current {
		slot, err := request.Evidence.Header.SlotKey()
		if err != nil {
			return err
		}
		bySlot[slot] = request
	}
	for _, request := range c.Retained {
		slot, err := request.Evidence.Header.SlotKey()
		if err != nil {
			return err
		}
		observed, found := bySlot[slot]
		if !found || !evidenceRelayContinuationSameJSON(request, observed) {
			return errors.New("relay continuation public replay omitted or changed an original approved subject")
		}
	}
	return nil
}

// Admission of the exact fresh bridge binds the approved relay namespace and
// signed lifetime prefix. Subsequent stopped resume may append real records;
// it must preserve the original root and exact bounded source capacity.
func preflightEvidenceRelayContinuationHistory(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan) error {
	c := plan.EvidenceRelayContinuation
	if c == nil {
		return nil
	}
	if cfg.strictHistoryAdoption == nil {
		return errors.New("relay continuation launch requires its exact authenticated strict history adoption")
	}
	if err := validateEvidenceRelayContinuationPlan(plan); err != nil {
		return err
	}
	state := cfg.strictHistoryAdoption
	if state.bundle.SourcePlanHash != c.ActivationPlanHash || state.bundle.PreparedSHA256 != c.PreparedSHA256 || state.bundle.CompletedSHA256 != c.CompletedSHA256 {
		return errors.New("relay continuation changed the original signed activation setup")
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
	if err := validateEvidenceRelayContinuationStartupRunway(cfg, plan, c, head.Number); err != nil {
		return err
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		return err
	}
	var activeConfigs []*validatorcomponent.ReleaseConfig
	if c.ActiveGeneration != nil {
		handoff, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, plan)
		if err != nil || handoff == nil {
			return errors.Join(errors.New("relay history active handoff is absent"), err)
		}
		for _, owner := range handoff.Validators {
			config, err := policyRolloverSourceRoleConfigV2(ctx, owner.Config, owner.Config.Bytes)
			if err != nil {
				return err
			}
			activeConfigs = append(activeConfigs, config)
		}
	}
	sources, requests := c.Sources, state.requests
	if c.ActiveGeneration != nil {
		sources, requests = c.ActiveGeneration.Frozen, state.frozenRequests
	}
	check := func(source EvidenceRelayContinuationSource, requestBytes []byte, operatorDir string, bounds validatorcomponent.ReleaseEvidenceV2Bounds, forecast bool) error {
		var request validatorcomponent.ReleaseHistoryAdoptionV2
		if err := json.Unmarshal(requestBytes, &request); err != nil {
			return err
		}
		if request.CoordinatorStateDir != source.CoordinatorStateDir || request.IntentPrefixSHA256 != source.IntentPrefixSHA256 || request.IntentPrefixCount != source.IntentPrefixCount || request.LastNativeEpoch != source.LastNativeEpoch || request.LastArtifactHash != source.LastArtifactHash {
			return errors.New("relay continuation adopted another coordinator history prefix")
		}
		capacity, err := state.capacityCache.Read(ctx, operatorDir, source.Capacity.Identity, source.Capacity.Coordinator, ed25519.PublicKey(source.Activation.VPK[:]), bounds.Disk, source.Capacity.Head)
		if err != nil {
			return err
		}
		if !forecast {
			return nil
		}
		span, err := evidenceRelayContinuationCapacitySpan(c, head.Number)
		if err != nil {
			return err
		}
		return validateEvidenceRelayContinuationCapacity(cfg, bounds, span, capacity)
	}
	for _, source := range sources {
		if source.ValidatorID == 0 || source.ValidatorID > uint64(len(requests)) || source.ValidatorID > uint64(len(resolved.Config.ValidatorEvidenceV2)) {
			return errors.New("relay continuation history source is absent")
		}
		bounds := resolved.Config.ValidatorEvidenceV2[source.ValidatorID-1].Evidence.Bounds
		operatorDir := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", source.ValidatorID), "state", "operators", fmt.Sprintf("no-%d", source.NoID))
		if err := check(source, requests[source.ValidatorID-1], operatorDir, bounds, c.ActiveGeneration == nil); err != nil {
			return err
		}
	}
	if c.ActiveGeneration != nil {
		for _, source := range c.ActiveGeneration.Sources {
			if source.ValidatorID == 0 || source.ValidatorID > uint64(len(state.requests)) || source.ValidatorID > uint64(len(activeConfigs)) {
				return errors.New("relay continuation active source is absent")
			}
			config := activeConfigs[source.ValidatorID-1]
			if source.NoID == 0 || source.NoID > uint64(len(config.Operators)) {
				return errors.New("relay continuation active operator is absent")
			}
			if err := check(source, state.requests[source.ValidatorID-1], config.Operators[source.NoID-1].StateDir, config.EvidenceV2.Bounds, true); err != nil {
				return err
			}
		}
	}
	latest, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return err
	}
	return validateEvidenceRelayContinuationStartupRunway(cfg, plan, c, latest.Number)
}
