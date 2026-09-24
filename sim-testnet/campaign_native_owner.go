// Retained process-only startup owns approved metadata but deliberately opens
// no native reader. Campaign handoff acquires that missing owner exactly once.
package main

import (
	"context"
	"errors"
	"maps"
	"reflect"
)

// Only transport construction is replaceable in deterministic orchestration
// tests. Production supplies the ordinary identity-verifying constructor.
type campaignExecutorFactory func(context.Context, *ResolvedConfig, *ResolvedConfig, string, *SetupPlan, *Journal, *RoleSecrets, *Executor) (*Executor, error)

// A real parent keeps its native connection. Only the exact non-accepting
// release command's metadata-only executor may request a newly owned reader;
// incomplete, foreign, or partially live executors cannot be silently replaced.
func retainedCampaignNativeOwner(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, journal *Journal, roles *RoleSecrets, owner *Executor) (*Executor, error) {
	if owner == nil || owner.substrate != nil {
		return owner, nil
	}
	if !provisionalResumeEnabled(cfg) || cfg.provisionalResume.Record.Command != "scenario" || !provisionalRetainedStartupAllowed(cfg.provisionalResume.Record) || plan == nil || journal == nil || roles == nil || owner.roles == nil || owner.cfg == nil || owner.stateDir != stateDir || owner.plan != plan || owner.journal != journal || cfg.provisionalResume.Record.PlanHash != plan.PlanHash || (owner.auditAuthorizedConfig != nil && owner.auditAuthorizedConfig != owner.cfg) {
		return nil, errors.New("retained campaign reader acquisition lacks its exact approved local owner")
	}
	if owner.cfg != cfg {
		// Scenario entry adds approved rate history to an owned config copy.
		// Reconstruct that exact copy; no other authority or runtime field may vary.
		if plan.PolicyRateAmendment == nil {
			return nil, errors.New("retained campaign reader acquisition changed its local configuration owner")
		}
		amended, err := configWithPolicyRateAmendment(owner.cfg, plan)
		if err != nil || !reflect.DeepEqual(amended, cfg) {
			return nil, errors.Join(errors.New("retained campaign reader acquisition differs from its approved rate configuration"), err)
		}
	}
	// Scenario startup reloads authenticated secrets. Equal credentials must not
	// require the earlier allocation, but any changed role remains a hard error.
	if owner.roles.Schema != roles.Schema || owner.roles.DeploymentID != roles.DeploymentID || !maps.Equal(owner.roles.EVM, roles.EVM) || !maps.Equal(owner.roles.Substrate, roles.Substrate) || !maps.Equal(owner.roles.Clients, roles.Clients) {
		return nil, errors.New("retained campaign reader acquisition changed its authenticated roles")
	}
	if owner.preparationIncomplete || owner.nativeOwner != nil || owner.independentSubstrate != nil || owner.independentEVM != nil || owner.deployer != nil || owner.owner != nil || owner.guardian != nil || owner.oracle != nil || owner.keeper != nil || len(owner.deposits) != 0 || owner.payloads != nil {
		return nil, errors.New("retained campaign cannot replace a partial or borrowed connection owner")
	}
	return nil, nil
}
