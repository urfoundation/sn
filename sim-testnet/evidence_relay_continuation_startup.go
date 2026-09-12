//go:build linux || darwin

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

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
// it must preserve the original root and fit only the fixed remaining runway.
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
	if head.Number < c.EVMHead.Number || head.Number >= c.EndBlock {
		return errors.New("relay continuation fixed source-capacity runway has expired")
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		return err
	}
	for _, source := range c.Sources {
		if source.ValidatorID == 0 || source.ValidatorID > uint64(len(state.requests)) || source.ValidatorID > uint64(len(resolved.Config.ValidatorEvidenceV2)) {
			return errors.New("relay continuation history source is absent")
		}
		var request validatorcomponent.ReleaseHistoryAdoptionV2
		if err := json.Unmarshal(state.requests[source.ValidatorID-1], &request); err != nil {
			return err
		}
		if request.CoordinatorStateDir != source.CoordinatorStateDir || request.IntentPrefixSHA256 != source.IntentPrefixSHA256 || request.IntentPrefixCount != source.IntentPrefixCount || request.LastNativeEpoch != source.LastNativeEpoch || request.LastArtifactHash != source.LastArtifactHash {
			return errors.New("relay continuation adopted another coordinator history prefix")
		}
		bounds := resolved.Config.ValidatorEvidenceV2[source.ValidatorID-1].Evidence.Bounds
		operatorDir := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", source.ValidatorID), "state", "operators", fmt.Sprintf("no-%d", source.NoID))
		capacity, err := validatorcomponent.ReadStoppedAttemptLedgerCapacity(ctx, operatorDir, source.Capacity.Identity, source.Capacity.Coordinator, ed25519.PublicKey(source.Activation.VPK[:]), bounds.Disk, source.Capacity.Head)
		if err != nil {
			return err
		}
		if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, c.EndBlock-head.Number, capacity); err != nil {
			return err
		}
	}
	return nil
}
