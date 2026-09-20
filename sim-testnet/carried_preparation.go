package main

// Preparation validates independent retained actions even after another
// action fails. A failed prerequisite blocks only readers which need it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
)

func (self *Executor) collectCarriedActionHistory(ctx context.Context) error {
	if ctx == nil || self == nil || self.plan == nil || self.journal == nil {
		return errors.New("plan/journal or preparation context is unavailable")
	}
	return self.collectCarriedActionHistoryWithReaders(ctx, self.journal.Entries, readValidatorEvidenceHistoricalPlan)
}

// One detached journal snapshot and source-plan set belong to this read-only
// reconciliation. Tests count these real reads without replacing verifiers.
func (self *Executor) collectCarriedActionHistoryWithReaders(ctx context.Context, readEntries func() []JournalEntry, readSource func(string, string) (*SetupPlan, error)) error {
	if ctx == nil || self == nil || self.plan == nil || self.journal == nil || readEntries == nil || readSource == nil {
		return errors.New("plan/journal or preparation reader is unavailable")
	}
	readOnlyAudit := self.cfg != nil && self.cfg.readOnlyAudit
	self.carriedVerificationKeys = nil
	if provisionalResumeEnabled(self.cfg) {
		return self.verifyProvisionalActionHistoryWithReaders(ctx, readEntries, readSource)
	}
	entries := readEntries()
	verified := newCarriedPreparationIndex(self.plan, entries)
	readPostcondition := self.carriedPreparationPostconditionReader(ctx, readSource)
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
		entry, ok := verified.find(action, false)
		if action.ID == "topology.launch" && (!ok || readOnlyAudit) {
			entry, ok = verified.find(action, true)
			if ok && (entry.PlanHash != self.plan.PlanHash || readOnlyAudit) {
				if _, err := readPostcondition(entry); err != nil {
					actionErrors[index] = fmt.Errorf("action %s: persisted ancestor process receipt: %w", action.ID, err)
				}
			}
			continue
		}
		if !ok || entry.PlanHash == self.plan.PlanHash && !readOnlyAudit {
			continue
		}
		if err := ctx.Err(); err != nil {
			actionErrors[index] = fmt.Errorf("action %s: blocked by canceled preparation: %w", action.ID, err)
			continue
		}
		record, err := readPostcondition(entry)
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
	results := collectOrderedReadOnlyAudits(ctx, len(audits), carriedActionVerificationWorkersFor(self.cfg), func(index int) error {
		audit := audits[index]
		defer func() {
			count := completed.Add(1)
			if count%carriedActionProgressInterval == 0 || count == uint64(len(audits)) {
				fmt.Fprintf(os.Stderr, "sim-testnet: carried action audit %d/%d\n", count, len(audits))
			}
		}()
		blocked := func(stage string) error { return fmt.Errorf("action %s: blocked by %s", audit.action.ID, stage) }
		if self.carriedFleetHistoryKeys[carriedVerificationKey(audit.entry)] {
			return verifyCarriedActionWithTimeoutFor(ctx, self.cfg, func(auditCtx context.Context) error {
				return self.verifyVerifiedActionStateWithRecord(auditCtx, audit.action, audit.entry, audit.record, sharedEvmHead, sharedNativeHead)
			})
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
		if err := verifyCarriedActionWithTimeoutFor(ctx, self.cfg, func(auditCtx context.Context) error {
			return self.verifyVerifiedActionStateWithRecord(auditCtx, audit.action, audit.entry, audit.record, sharedEvmHead, sharedNativeHead)
		}); err != nil {
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
	// No action may inherit a partial or superseded reconciliation. Ordinary
	// execution still resolves the current journal before using any exact key.
	if !slices.Equal(entries, readEntries()) {
		stages = append(stages, errors.New("carried preparation journal changed during reconciliation"))
	} else if ctx.Err() == nil {
		self.carriedVerificationKeys = verifiedKeys
	}
	return errors.Join(errors.Join(append(stages, actionErrors...)...), ctx.Err())
}

// Every carried receipt can trigger historical RPC reads, including entries
// already classified by the fleet-history cache. Bound each action uniformly
// so one slow archive response reaches the retry/recovery path instead of
// holding the preparation collector indefinitely.
func verifyCarriedActionWithTimeout(ctx context.Context, verify func(context.Context) error) error {
	return verifyCarriedActionWithTimeoutFor(ctx, nil, verify)
}

// The dedicated LAN archive is unpaced but may take longer to materialize a
// large historical EVM proof. Keep that transient capacity condition inside a
// recoverable read-only window instead of treating it as failed evidence.
func verifyCarriedActionWithTimeoutFor(ctx context.Context, cfg *ResolvedConfig, verify func(context.Context) error) error {
	if ctx == nil || verify == nil {
		return errors.New("carried action verification context or callback is unavailable")
	}
	timeout := carriedActionVerificationTimeout
	if cfg != nil && cfg.OperationalRPCMode == rpcModeOwnedNode {
		timeout = carriedActionOwnedVerificationTimeout
	}
	// Historical verification is read-only. A single fresh context lets a
	// transient archive timeout recover without discarding the completed audit
	// prefix or restarting setup. Persistent failures still surface promptly.
	attempts := 1
	if cfg != nil && cfg.OperationalRPCMode == rpcModeOwnedNode {
		attempts = 2
	}
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		auditCtx, cancel := context.WithTimeout(ctx, timeout)
		err := verify(auditCtx)
		cancel()
		if err == nil {
			return nil
		}
		last = err
		if !errors.Is(err, context.DeadlineExceeded) || attempt+1 == attempts {
			return err
		}
	}
	return last
}

// The owned LAN archive node has no request-rate gate. Bound its historical
// reads to a modest pool so recovery is substantially faster without turning
// a transiently slow archive response into an unbounded fan-out. Each action
// still has its own cancellation deadline and retry budget.
func carriedActionVerificationWorkersFor(cfg *ResolvedConfig) int {
	if cfg != nil && cfg.OperationalRPCMode == rpcModeOwnedNode {
		return carriedActionOwnedVerificationWorkers
	}
	return carriedActionVerificationWorkers
}

// Share only authenticated immutable source decoding within one read-only
// call. Every receipt's bytes, hash and original route remain freshly checked.
func (self *Executor) carriedPreparationPostconditionReader(ctx context.Context, readSource func(string, string) (*SetupPlan, error)) func(JournalEntry) (*ActionPostcondition, error) {
	sourcePlanKVs := map[string]*SetupPlan{}
	return func(entry JournalEntry) (*ActionPostcondition, error) {
		return self.readPersistedPostconditionWithSource(entry, func(stateDir, hash string) (*SetupPlan, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if source := sourcePlanKVs[hash]; source != nil {
				return source, nil
			}
			source, err := readSource(stateDir, hash)
			if err = errors.Join(err, ctx.Err()); err != nil {
				return nil, err
			}
			sourcePlanKVs[hash] = source
			return source, nil
		})
	}
}

// These local/native checks do not dereference derived topology secrets.
func carriedActionWithoutRoles(action Action) bool {
	return action.Kind == "budget-reserve" || action.ID == "subnet.verify-owner" || action.ID == "topology.launch" || strings.HasPrefix(action.ID, "subnet.hyperparameter.") || strings.HasPrefix(action.ID, "production.hyperparameter.")
}
