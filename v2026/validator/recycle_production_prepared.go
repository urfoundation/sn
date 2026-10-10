// A signed production config authorizes a policy, not arbitrary prepared
// weights. Only the real durable V2 intent verifier grants one exact replay.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/urfoundation/sn/v2026/crv4"
)

// This private value is issued only after complete source/row/intent replay.
// All prepared fields are frozen, including expiry and the source commitment.
type ownerRecyclePreparedAuthorization struct {
	configHash   [32]byte
	preparedHash [32]byte
}

// The native writer calls this before any RPC. Equal artifact versions or a
// valid config signature cannot stand in for the actual measured intent.
func validateOwnerRecyclePreparedAuthorization(cfg *ReleaseConfig, prepared *crv4.PreparedSubmission) error {
	if !isOwnerRecycleProductionConfig(cfg) {
		return nil
	}
	if err := validateOwnerRecycleProductionConfig(cfg); err != nil {
		return err
	}
	if cfg.ownerRecycleProduction.historicalOnly {
		return errors.New("original production authority cannot authorize a current submission")
	}
	if prepared == nil || cfg.ownerRecycleProduction.prepared == nil {
		return errors.New("owner-recycle submission lacks an authenticated durable production intent")
	}
	if _, err := prepared.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(prepared)
	grant := cfg.ownerRecycleProduction.prepared
	if err != nil || grant.configHash != cfg.ownerRecycleProduction.configHash || grant.preparedHash != sha256.Sum256(raw) {
		return errors.Join(errors.New("owner-recycle prepared bytes differ from their authenticated intent"), err)
	}
	return nil
}

// The caller holds the V2 intent operation owner and has just completed its
// independent native/source/measurement replay. No candidate bool is stored.
func (self *IntentStore) retainOwnerRecyclePreparedAuthorization(intent *SteeringIntent) error {
	if !isOwnerRecycleProductionConfig(&self.v2.runtime.cfg) {
		return nil
	}
	if intent == nil || productionEconomicIntent(intent) == nil || intent.Prepared == nil {
		return errors.New("owner-recycle verified intent is incomplete")
	}
	decisionCfg, err := productionConfigForIntent(&self.v2.runtime.cfg, intent)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(intent.Prepared)
	if err != nil {
		return err
	}
	self.v2.productionPrepared = &ownerRecyclePreparedAuthorization{
		configHash: decisionCfg.ownerRecycleProduction.configHash, preparedHash: sha256.Sum256(raw)}
	return nil
}

// Reuse is confined to this store's last fully authenticated immutable intent;
// it does not bypass fresh runtime, nonce, epoch and receipt checks at send.
func (self *IntentStore) ownerRecyclePreparedConfig(ctx context.Context, cfg *ReleaseConfig, prepared *crv4.PreparedSubmission) (*ReleaseConfig, error) {
	if !isOwnerRecycleProductionConfig(cfg) {
		return cfg, nil
	}
	release, err := self.acquireV2(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := validateOwnerRecycleProductionConfig(cfg); err != nil {
		return nil, err
	}
	owned := *cfg
	owner := *cfg.ownerRecycleProduction
	owner.prepared = self.v2.productionPrepared
	owned.ownerRecycleProduction = &owner
	if err := validateOwnerRecyclePreparedAuthorization(&owned, prepared); err != nil {
		return nil, err
	}
	return &owned, ctx.Err()
}
