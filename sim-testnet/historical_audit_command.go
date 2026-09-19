// Historical auditing owns an authenticated read snapshot and signer-free RPC
// readers. It never acquires deployment ownership or repairs retained state.
package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Findings describe this snapshot only; even a successful audit is not final
// release acceptance and cannot change an active provisional campaign.
type historicalAuditReport struct {
	Schema          string  `json:"schema"`
	GeneratedAt     string  `json:"generated_at"`
	PlanHash        string  `json:"plan_hash"`
	ReleaseLockHash string  `json:"release_lock_hash"`
	JournalEntries  int     `json:"journal_entries"`
	JournalHeadHash string  `json:"journal_head_hash"`
	VerifiedActions int     `json:"verified_actions"`
	ReadOnly        bool    `json:"read_only"`
	FinalAcceptance bool    `json:"final_acceptance"`
	Passed          bool    `json:"passed"`
	Checks          []Check `json:"checks"`
}

// A read-only factory can return partial readers so one failed endpoint does
// not hide independent local evidence findings.
type historicalAuditFactory func(context.Context, *ResolvedConfig, *ResolvedConfig, string, *SetupPlan, []JournalEntry, *RoleSecrets) (*Executor, func(), error)

// Audit is intentionally independent of the startup and supervisor lifecycle.
func runHistoricalAudit(ctx context.Context, cfg, transportCfg *ResolvedConfig, stateDir, expectedPlanHash string) (historicalAuditReport, error) {
	return runHistoricalAuditWithFactory(ctx, cfg, transportCfg, stateDir, expectedPlanHash, newHistoricalAuditExecutor)
}

// The factory is the network boundary; all plan, journal and receipt validation
// remains the production implementation in deterministic offline tests.
func runHistoricalAuditWithFactory(ctx context.Context, cfg, transportCfg *ResolvedConfig, stateDir, expectedPlanHash string, factory historicalAuditFactory) (historicalAuditReport, error) {
	report := historicalAuditReport{Schema: "urnetwork-sim-historical-audit-v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), ReadOnly: true}
	var findings []error
	add := func(name string, err error) {
		check := Check{Name: name, Hard: true, OK: err == nil}
		if err != nil {
			check.Detail = err.Error()
			findings = append(findings, fmt.Errorf("%s: %w", name, err))
		}
		report.Checks = append(report.Checks, check)
	}
	if ctx == nil || cfg == nil || transportCfg == nil || factory == nil {
		add("audit-inputs", errors.New("historical audit context, configuration or reader factory is unavailable"))
		return report, errors.Join(findings...)
	}
	canonical, transport := *cfg, *transportCfg
	canonical.readOnlyAudit, transport.readOnlyAudit = true, true
	canonical.provisionalResume, transport.provisionalResume = nil, nil
	canonical.strictHistoryAdoption, transport.strictHistoryAdoption = nil, nil
	_, strictErr := loadPersistedPlan(&canonical, stateDir)
	add("current-release-plan", strictErr)
	plan, planErr := loadPersistedPlanIdentity(&canonical, stateDir, true)
	add("retained-plan-identity", planErr)
	entries, journalErr := readJournalEntries(stateDir)
	add("journal-authentication", journalErr)
	report.JournalEntries = len(entries)
	if len(entries) > 0 {
		report.JournalHeadHash = entries[len(entries)-1].EntryHash
	}
	roles, roleErr := loadExistingProvisionalRoles(&canonical, stateDir)
	add("retained-role-identities", roleErr)
	if plan != nil {
		report.PlanHash, report.ReleaseLockHash = plan.PlanHash, plan.ReleaseLockHash
		if expectedPlanHash != "" && expectedPlanHash != plan.PlanHash {
			planErr = errors.New("audit --plan-hash differs from the authenticated retained plan")
			add("requested-plan", planErr)
		}
	}
	if planErr == nil && journalErr == nil && ctx.Err() == nil {
		index := newCarriedPreparationIndex(plan, entries)
		for _, action := range plan.Actions {
			if _, present := index.find(action, true); present {
				report.VerifiedActions++
			}
		}
		executor, closeReaders, readerErr := factory(ctx, &canonical, &transport, stateDir, plan, entries, roles)
		if closeReaders != nil {
			defer closeReaders()
		}
		add("historical-readers", readerErr)
		if executor != nil {
			add("verified-action-history", executor.collectCarriedActionHistory(ctx))
		} else {
			add("verified-action-history", errors.New("historical readers are unavailable"))
		}
		latest, err := readJournalEntries(stateDir)
		if err == nil && (len(latest) < len(entries) || !slices.Equal(entries, latest[:len(entries)])) {
			err = errors.New("authenticated journal prefix changed during audit")
		}
		add("journal-snapshot-retained", err)
		latestPlan, err := loadPersistedPlanIdentity(&canonical, stateDir, true)
		if err == nil && latestPlan.PlanHash != plan.PlanHash {
			err = errors.New("approved plan changed during audit")
		}
		add("plan-snapshot-retained", err)
	}
	if err := ctx.Err(); err != nil {
		add("audit-context", err)
	}
	report.Passed = len(findings) == 0
	return report, errors.Join(findings...)
}

// No signing key, transaction writer, process owner or mutable journal is
// opened. Shared clients are closed once by the returned lifecycle closure.
func newHistoricalAuditExecutor(ctx context.Context, cfg, transportCfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, roles *RoleSecrets) (*Executor, func(), error) {
	if cfg == nil || transportCfg == nil || !cfg.readOnlyAudit || !transportCfg.readOnlyAudit {
		return nil, nil, errors.New("historical reader requires read-only audit configuration")
	}
	if err := errors.Join(validateOwnedRPCPlan(cfg, plan), validateExecutionRPCConfiguration(cfg), validateCampaignRPCTransport(cfg, transportCfg)); err != nil {
		return nil, nil, fmt.Errorf("historical audit reader authority: %w", err)
	}
	self := &Executor{cfg: transportCfg, auditAuthorizedConfig: cfg, stateDir: stateDir, plan: plan,
		journal: &Journal{entries: slices.Clone(entries)}, roles: roles, deposits: map[int]*EvmTxManager{}, preparationIncomplete: true}
	var closers []func()
	var failures []error
	for _, reader := range []struct {
		name     string
		endpoint string
		target   **SubstrateManager
	}{
		{name: "operational native reader", endpoint: transportCfg.OperationalSubstrate, target: &self.substrate},
		{name: "independent native reader", endpoint: verificationSubstrateEndpoint(transportCfg), target: &self.independentSubstrate},
	} {
		if reader.target == &self.independentSubstrate && !independentRPCRequired(transportCfg) {
			continue
		}
		chain, _, err := dialReleaseSubstrateChainContext(ctx, transportCfg, reader.endpoint)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", reader.name, err))
			continue
		}
		*reader.target = &SubstrateManager{chain: chain, cfg: transportCfg, journal: self.journal}
		closers = append(closers, chain.API.Client.Close)
	}
	for _, reader := range []struct {
		name        string
		endpoint    string
		independent bool
	}{
		{name: "operational EVM reader", endpoint: transportCfg.OperationalEVM},
		{name: "independent EVM reader", endpoint: verificationEVMEndpoint(transportCfg), independent: true},
	} {
		if reader.independent && !independentRPCRequired(transportCfg) {
			continue
		}
		client, err := dialConfiguredEVMClient(ctx, transportCfg, reader.endpoint)
		if err == nil {
			id, identityErr := client.ChainID(ctx)
			err = identityErr
			if err == nil && (id == nil || !id.IsUint64() || id.Uint64() != testnetChainID) {
				err = errors.New("reader is not the approved testnet chain")
			}
			if err == nil {
				closers = append(closers, client.Close)
				if reader.independent {
					self.independentEVM = client
				} else {
					manager := &EvmTxManager{client: client, chainID: id, journal: self.journal}
					self.deployer, self.owner, self.guardian, self.oracle, self.keeper = manager, manager, manager, manager, manager
					for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
						self.deposits[operator] = manager
					}
				}
			} else {
				client.Close()
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", reader.name, err))
		}
	}
	return self, func() {
		for _, closeReader := range closers {
			closeReader()
		}
	}, errors.Join(failures...)
}
