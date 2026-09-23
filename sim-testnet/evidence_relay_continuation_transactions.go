//go:build linux || darwin

// Stopped capture authenticates the complete signed nonce history before
// source replay, then rechecks the same immutable census before sealing it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
)

// The digest retains every signed attempt, including replacements sharing a
// nonce. Nonce coverage alone cannot authenticate that complete liability set.
type evidenceRelayContinuationTransactionCensus struct {
	exposure fleetRenewalExposure
	nonces   []FleetRenewalNonce
	sha256   string
}

// Original files, claim queues and approved renewal inputs are the evidence
// owners. Missing worker signatures require their original bytes to be retained.
func (self *Executor) readEvidenceRelayContinuationTransactionCensus(ctx context.Context, block uint64) (*evidenceRelayContinuationTransactionCensus, error) {
	if ctx == nil || self == nil || self.cfg == nil || self.cfg.Config == nil || self.plan == nil || self.journal == nil {
		return nil, errors.New("relay continuation transaction census has no retained owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transactions, err := readEvidenceRelayContinuationTransactions(self.cfg, self.stateDir, self.plan)
	if err != nil {
		return nil, err
	}
	exposure, err := fleetRenewalCampaignExposure(self.stateDir, self.plan, self.journal.Entries(), transactions)
	if err != nil {
		return nil, err
	}
	nonces, err := self.observeEvidenceRelayContinuationNonces(ctx, exposure, block)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(transactions)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &evidenceRelayContinuationTransactionCensus{exposure: exposure, nonces: nonces, sha256: bytesSHA256(encoded)}, nil
}

// A long replay cannot silently admit additions, removals, replacement attempts
// or an advanced nonce after the initial strict preflight has succeeded.
func (self *Executor) recheckEvidenceRelayContinuationTransactionCensus(ctx context.Context, block uint64, previous *evidenceRelayContinuationTransactionCensus) error {
	if previous == nil {
		return errors.New("relay continuation transaction census has no initial observation")
	}
	current, err := self.readEvidenceRelayContinuationTransactionCensus(ctx, block)
	if err != nil {
		return err
	}
	if current.sha256 != previous.sha256 || !reflect.DeepEqual(current.nonces, previous.nonces) || current.exposure.Liability != previous.exposure.Liability || current.exposure.SupersededCredit != previous.exposure.SupersededCredit {
		return errors.New("relay continuation signed transaction census changed during capture")
	}
	return nil
}
