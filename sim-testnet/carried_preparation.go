package main

// Preparation validates independent retained actions even after another
// action fails. A failed prerequisite blocks only readers which need it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

func (self *Executor) collectCarriedActionHistory(ctx context.Context) error {
	if ctx == nil || self == nil || self.plan == nil || self.journal == nil {
		return errors.New("plan/journal or preparation context is unavailable")
	}
	self.carriedVerificationKeys = nil
	if provisionalResumeEnabled(self.cfg) {
		return self.verifyProvisionalActionHistory(ctx)
	}
	var stages []error
	var carryErr error
	if self.plan.ValidatorEvidenceCarry != nil {
		carryErr = self.preparationPayloadReadersError()
		if carryErr == nil {
			_, carryErr = self.authenticateValidatorEvidenceCarry(ctx)
		}
		if carryErr != nil {
			stages = append(stages, fmt.Errorf("validator evidence immutable source history: %w", carryErr))
		}
	}
	actionErrors := make([]error, len(self.plan.Actions))
	actionIndexes := map[string]int{}
	audits := make([]carriedActionAudit, 0)
	for index, action := range self.plan.Actions {
		actionIndexes[action.ID] = index
		entry, ok := self.verifiedActionEntry(action)
		if !ok && action.ID == "topology.launch" {
			entry, ok = self.verifiedActionEntryForScope(action, true)
			if ok && entry.PlanHash != self.plan.PlanHash {
				if _, err := self.readPersistedPostcondition(entry); err != nil {
					actionErrors[index] = fmt.Errorf("action %s: persisted ancestor process receipt: %w", action.ID, err)
				}
			}
			continue
		}
		if !ok || entry.PlanHash == self.plan.PlanHash {
			continue
		}
		if err := ctx.Err(); err != nil {
			actionErrors[index] = fmt.Errorf("action %s: blocked by canceled preparation: %w", action.ID, err)
			continue
		}
		record, err := self.readPersistedPostcondition(entry)
		if err != nil {
			actionErrors[index] = fmt.Errorf("action %s: persisted postcondition: %w", action.ID, err)
			continue
		}
		audits = append(audits, carriedActionAudit{action: action, entry: entry, record: record})
	}
	var payloadErr error
	if len(audits) > 0 && planUsesContractDeploymentEnvelope(self.plan.Schema) {
		if carryErr != nil {
			payloadErr = errors.New("blocked by validator evidence immutable source history")
		} else if self.payloads == nil && self.roles == nil {
			payloadErr = errors.New("blocked by role secrets")
		} else {
			payloadErr = self.ensurePayloads(ctx)
		}
		if payloadErr != nil {
			stages = append(stages, fmt.Errorf("prepare carried contract payloads: %w", payloadErr))
		}
	}
	var fleetErr error
	if payloadErr == nil {
		self.carriedFleetHistoryKeys, fleetErr = self.verifyCarriedFleetGenerationOneHistory(ctx, audits)
	} else {
		fleetErr = errors.New("blocked by carried contract payloads")
	}
	if fleetErr != nil {
		stages = append(stages, fmt.Errorf("prepare carried fleet history: %w", fleetErr))
	}
	defer func() { self.carriedFleetHistoryKeys = nil }()
	var sharedEvmHead, sharedNativeHead *ChainHead
	var evmErr, nativeErr error
	for _, audit := range audits {
		if !actionPostStateRequiresEVMCheckpoint(audit.action) {
			continue
		}
		if self.deployer == nil || self.deployer.client == nil {
			evmErr = errors.New("EVM postcondition client is unavailable")
		} else {
			head, err := finalizedEVMHead(ctx, self.deployer.client)
			evmErr = err
			if err == nil {
				sharedEvmHead = &head
			}
		}
		if evmErr != nil {
			stages = append(stages, fmt.Errorf("prepare carried EVM checkpoint: %w", evmErr))
		}
		break
	}
	for _, audit := range audits {
		if actionRequiresCurrentPostcondition(audit.action) {
			continue
		}
		if _, err := self.consumedActionTransaction(audit.action, audit.entry); err != nil {
			continue
		}
		if self.substrate == nil {
			nativeErr = errors.New("Substrate postcondition client is unavailable")
		} else {
			hash, number, err := self.substrate.finalizedHeadContext(ctx)
			nativeErr = err
			if err == nil {
				sharedNativeHead = &ChainHead{Number: number, Hash: hash.Hex()}
			}
		}
		if nativeErr != nil {
			stages = append(stages, fmt.Errorf("prepare carried native checkpoint: %w", nativeErr))
		}
		break
	}
	var completed atomic.Uint64
	results := collectOrderedReadOnlyAudits(ctx, len(audits), carriedActionVerificationWorkers, func(index int) error {
		audit := audits[index]
		defer func() {
			count := completed.Add(1)
			if count%carriedActionProgressInterval == 0 || count == uint64(len(audits)) {
				fmt.Fprintf(os.Stderr, "sim-testnet: carried action audit %d/%d\n", count, len(audits))
			}
		}()
		blocked := func(stage string) error { return fmt.Errorf("action %s: blocked by %s", audit.action.ID, stage) }
		if self.carriedFleetHistoryKeys[carriedVerificationKey(audit.entry)] {
			return self.verifyVerifiedActionStateWithRecord(ctx, audit.action, audit.entry, audit.record, sharedEvmHead, sharedNativeHead)
		}
		consumed := false
		if !actionRequiresCurrentPostcondition(audit.action) {
			_, err := self.consumedActionTransaction(audit.action, audit.entry)
			consumed = err == nil
		}
		if consumed && nativeErr != nil {
			return blocked("carried native checkpoint")
		}
		needsNative := consumed || strings.HasPrefix(audit.action.Kind, "substrate-") || audit.action.ID == "config.render" || strings.HasPrefix(audit.action.ID, "evidence.activate.") || audit.action.ID == runtimeEvidenceActivationBoundaryActionId || isFleetRenewalAction(audit.action) || strings.HasPrefix(audit.action.ID, "operator.register.") || strings.HasPrefix(audit.action.ID, "alpha.") || strings.HasPrefix(audit.action.ID, "precompile.") && audit.action.ID != "precompile.probe-deploy"
		if self.preparationIncomplete && independentRPCRequired(self.cfg) {
			if needsNative && self.independentSubstrate == nil {
				return blocked("independent native reader")
			}
			if actionPostStateRequiresEVMCheckpoint(audit.action) && self.independentEVM == nil {
				return blocked("independent EVM reader")
			}
		}
		needsPayloads := !consumed && (actionPostStateRequiresEVMCheckpoint(audit.action) || audit.action.ID == "config.render" || strings.HasPrefix(audit.action.ID, "fleet.mirror.") || strings.HasPrefix(audit.action.ID, "fleet.bind."))
		if needsPayloads && payloadErr != nil {
			return blocked("carried contract payloads")
		}
		if !consumed && actionPostStateRequiresEVMCheckpoint(audit.action) && evmErr != nil {
			return blocked("carried EVM checkpoint")
		}
		if self.roles == nil && !carriedActionWithoutRoles(audit.action) {
			return blocked("role secrets")
		}
		if self.substrate == nil && needsNative {
			return blocked("native reader")
		}
		if self.preparationIncomplete && !consumed && actionPostStateRequiresEVMCheckpoint(audit.action) && (self.owner == nil || self.owner.client == nil) {
			return blocked("owner EVM reader")
		}
		if self.preparationIncomplete && !consumed {
			if (strings.HasPrefix(audit.action.ID, "fleet.mirror.") || strings.HasPrefix(audit.action.ID, "fleet.refresh.batch.") || strings.HasPrefix(audit.action.ID, "fleet.install.batch.") || isFleetRenewalAction(audit.action) && audit.action.Parameters["operation"] == "mirror") && (self.oracle == nil || self.oracle.client == nil) {
				return blocked("commitment-oracle EVM reader")
			}
			if (strings.HasPrefix(audit.action.ID, "fleet.bind.") || strings.HasPrefix(audit.action.ID, "evidence.activate.") || audit.action.ID == runtimeEvidenceActivationBoundaryActionId || isFleetRenewalAction(audit.action) && audit.action.Parameters["operation"] != "mirror" && audit.action.Parameters["operation"] != "commitment") && (self.keeper == nil || self.keeper.client == nil) {
				return blocked("keeper EVM reader")
			}
		}
		if fleetErr != nil {
			coordinates, applicable, err := fleetGenerationOneCoordinates(self.cfg, audit.action)
			if err != nil {
				return fmt.Errorf("action %s generation-1 coordinates: %w", audit.action.ID, err)
			}
			if applicable && !coordinates.Install {
				superseded, err := self.fleetGenerationOneActionSuperseded(audit.action, audit.entry, audit.record)
				if err != nil {
					return fmt.Errorf("action %s generation-1 successor: %w", audit.action.ID, err)
				}
				if superseded {
					return blocked("carried fleet history")
				}
			}
		}
		auditCtx, cancel := context.WithTimeout(ctx, carriedActionVerificationTimeout)
		defer cancel()
		if err := self.verifyVerifiedActionStateWithRecord(auditCtx, audit.action, audit.entry, audit.record, sharedEvmHead, sharedNativeHead); err != nil {
			return fmt.Errorf("action %s: %w", audit.action.ID, err)
		}
		return nil
	})
	verifiedKeys := map[string]bool{}
	for index, audit := range audits {
		if results[index] != nil {
			err := results[index]
			if !strings.HasPrefix(err.Error(), "action "+audit.action.ID+":") {
				err = fmt.Errorf("action %s: %w", audit.action.ID, err)
			}
			actionErrors[actionIndexes[audit.action.ID]] = err
		} else {
			verifiedKeys[carriedVerificationKey(audit.entry)] = true
		}
	}
	self.carriedVerificationKeys = verifiedKeys
	return errors.Join(append(stages, actionErrors...)...)
}

// These local/native checks do not dereference derived topology secrets.
func carriedActionWithoutRoles(action Action) bool {
	return action.Kind == "budget-reserve" || action.ID == "subnet.verify-owner" || action.ID == "topology.launch" || strings.HasPrefix(action.ID, "subnet.hyperparameter.") || strings.HasPrefix(action.ID, "production.hyperparameter.")
}
