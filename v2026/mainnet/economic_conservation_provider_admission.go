// Admission joins portable originals to the containing held archive owner.
// Cached counters cannot authorize a new epoch, and a refused candidate cannot
// become the predecessor of an unrelated future window.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// The parent serial owner is the only caller which can seal a derived result.
// On restart all original payloads are verified again; private child results
// are neither serialized nor reconstructed from supplied summary counters.
func (self *economicConservationArchiveView) verifyProviderOriginals(ctx context.Context, policy economicConservationPolicy, state *economicConservationState, record economicConservationEntitlement) (*economicProviderAdmitted, error) {
	source, exists := policy.entitlementSource(record.PoolId)
	if !exists || record.Census == nil {
		return nil, payoutartifact.ErrClosedWorkUnavailable
	}
	census := record.Census
	selected := source.ProviderMeasurements
	if selected == nil {
		if census.ProviderOriginals != nil || census.ProviderMeasurements != nil {
			return nil, errors.New("economic provider evidence lacks original selected authority")
		}
		return nil, ctx.Err()
	}
	if self == nil {
		return nil, payoutartifact.ErrClosedWorkUnavailable
	}
	if err := self.checkAdmission(); err != nil {
		return nil, err
	}
	originals := census.ProviderOriginals
	if originals == nil || originals.Work == nil || len(originals.Attempts) == 0 {
		return nil, payoutartifact.ErrClosedWorkUnavailable
	}
	if err := originals.bounded(selected.MaximumOriginalBytes); err != nil {
		return nil, err
	}
	authority, err := payoutartifact.DecodeWholeWorkAuthority(ctx, originals.Work.Authority, common.HexToAddress(selected.WholeWorkAuthoritySigner))
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	domain, err := payoutartifact.ClosedWorkReportDomain(&census.Artifact)
	if err != nil {
		return nil, err
	}
	if authority.Domain != domain || authority.Epoch != census.Artifact.Epoch || authority.Start != census.Start || authority.End != census.End {
		return nil, errors.Join(errRpcIntegrity, payoutartifact.ErrClosedWorkIntegrity)
	}
	expected, err := selected.workExpectation(&census.Artifact, census.RootSigner, nil)
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	var requested [][16]byte
	if expected.AttributionSigner != (common.Address{}) && len(originals.Work.AttributionOriginals) != 0 {
		requested, err = payoutartifact.ReadWholeWorkPriorCreationRequests(ctx, &census.Artifact, originals.Work, expected)
		if err != nil {
			return nil, economicProviderEvidenceError(err)
		}
	}
	expected.PriorContracts, expected.PriorCreations, err = self.providerPriorOriginals(ctx, state, &authority, originals.Work, requested)
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	work, err := payoutartifact.VerifyWholeWorkInventoryWithWitness(ctx, &census.Artifact, originals.Work, expected)
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	if !work.Complete || !work.AttributionComplete {
		return nil, payoutartifact.ErrClosedWorkUnavailable
	}
	closed, err := economicClosedWorkProjection(ctx, &census.Artifact, work.Reports, originals.Work.Clock)
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	if census.ClosedWork != nil && *census.ClosedWork != *closed {
		return nil, errors.Join(errRpcIntegrity, errors.New("economic closed-work counters differ from complete original provider source"))
	}
	// the verified work roster is the authority's roster, in the same order
	if len(authority.ExpectedProviders) != len(work.ExpectedProviders) {
		return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
	}
	ids := make([][16]byte, len(work.ExpectedProviders))
	for index, provider := range work.ExpectedProviders {
		if authority.ExpectedProviders[index].ClientId != provider.ClientId || authority.ExpectedProviders[index].NetworkId != provider.NetworkId {
			return nil, errors.Join(errRpcIntegrity, protocol.ErrWalletMappingIntegrity)
		}
		ids[index] = provider.ClientId
	}
	wallets, err := economicProviderEarningWallets(ctx, &authority, originals, common.HexToAddress(census.RootSigner), census.Start.Number, originals.Work.Clock.StartTime.Unix())
	if err != nil {
		return nil, err
	}
	bindings, _, err := validator.VerifyProviderAttemptBindings(ctx, originals.Bindings, economicProviderBindingExpectation(census, domain, ids))
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	attempts := census.providerAttempts
	if attempts == nil {
		reader, err := selected.openAttemptSource(ctx)
		if err != nil {
			return nil, economicProviderEvidenceError(err)
		}
		attempts, err = reader.VerifyRetained(ctx, &census.Artifact, originals.Attempts)
		if err != nil {
			return nil, economicProviderEvidenceError(err)
		}
	}
	_, authorityHash, err := selected.attemptReference()
	if err != nil || attempts == nil || attempts.OriginalHash != sha256.Sum256(originals.Attempts) || attempts.AuthorityHash != authorityHash {
		return nil, errors.Join(errRpcIntegrity, protocol.ErrProviderAttemptsIntegrity)
	}
	trialValues, err := economicProviderTrialProjection(ctx, &census.Artifact, work, attempts, wallets, bindings)
	if err != nil {
		return nil, economicProviderEvidenceError(err)
	}
	workValues := &economicProviderWorkValues{artifactHash: census.Artifact.ContentHash, authorityHash: work.AuthorityHash, windowHash: work.WindowHash, complete: true}
	for _, provider := range work.ExpectedProviders {
		workValues.providers = append(workValues.providers, economicProviderWorkValue{clientId: provider.ClientId, networkId: provider.NetworkId, usageBytes: provider.UsageBytes})
	}
	measurement, err := reconcileEconomicProviderMeasurements(ctx, &census.Artifact, workValues, trialValues)
	if err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if measurement == nil {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	measurement.WorkInventoryHash = work.InventoryHash
	measurement.WalletOriginalsHash = rootObjectHash(originals.Wallets)
	if 0 < len(originals.NetworkWallets) {
		measurement.NetworkWalletOriginalsHash = rootObjectHash(originals.NetworkWallets)
	}
	measurement.BindingOriginalsHash = rootObjectHash(originals.Bindings)
	measurement.TrialAuthorityHash = selected.AttemptAuthority.SHA256
	measurement.TrialOriginalsHash = hex.EncodeToString(attempts.OriginalHash[:])
	result := &economicProviderAdmitted{Domain: domain, Epoch: work.Epoch, InventoryHash: work.InventoryHash, Measurement: *measurement, ClosedWork: *closed, NewContracts: map[[16]byte]payoutartifact.WholeWorkPriorContract{}}
	for _, contract := range work.ReconciledContracts {
		if contract.ReconciledEpoch == work.Epoch && contract.InventoryHash == work.InventoryHash {
			result.NewContracts[contract.ContractId] = contract
		}
	}
	if len(work.RetainedCreations) != 0 {
		result.NewCreations = make(map[[16]byte]payoutartifact.WholeWorkRetainedCreation, len(work.RetainedCreations))
		for _, creation := range work.RetainedCreations {
			id := creation.Original.ContractId
			checkpoint, exists := result.NewContracts[id]
			if _, duplicate := result.NewCreations[id]; duplicate || !exists || checkpoint != creation.Checkpoint {
				return nil, errors.Join(errRpcIntegrity, payoutartifact.ErrClosedWorkIntegrity)
			}
			result.NewCreations[id] = cloneEconomicProviderCreation(creation)
		}
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return nil, err
	}
	return result, nil
}

// Only the first detached worker result can acquire a projection. Once the
// census is immutable, a missing or altered projection is an integrity refusal.
func (self *economicConservationArchiveView) sealProviderOriginals(ctx context.Context, policy economicConservationPolicy, state *economicConservationState, record *economicConservationEntitlement) error {
	value, err := self.verifyProviderOriginals(ctx, policy, state, *record)
	if err != nil {
		return err
	}
	if value != nil {
		if record.Census.ProviderMeasurements != nil && *record.Census.ProviderMeasurements != value.Measurement {
			return errors.Join(errRpcIntegrity, errors.New("economic provider projection differs before original publication"))
		}
		record.Census.ProviderMeasurements = &value.Measurement
		record.Census.ClosedWork = &value.ClosedWork
		record.Census.WindowClock = record.Census.ProviderOriginals.Work.Clock
		record.Census.ContentHash = record.Census.hash()
		return self.retainProviderOriginals(ctx, record.Census.ContentHash, value)
	}
	return nil
}

// Charge only actual new indexed originals. A cached value is unusable as a
// prior unless its complete census is still present in the admitted state.
func (self *economicConservationArchiveView) retainProviderOriginals(ctx context.Context, key string, value *economicProviderAdmitted) error {
	if value == nil {
		return nil
	}
	self.providerCandidate.captureProvider(key, value)
	if prior := self.providerCensuses[key]; prior != nil {
		if prior.Measurement != value.Measurement || prior.InventoryHash != value.InventoryHash || prior.ClosedWork != value.ClosedWork || !reflect.DeepEqual(prior.NewContracts, value.NewContracts) || !reflect.DeepEqual(prior.NewCreations, value.NewCreations) {
			return errors.Join(errRpcIntegrity, errors.New("economic original provider census changed after admission"))
		}
		return errors.Join(ctx.Err(), self.checkAdmission())
	}
	entries, bytes := self.entries, self.bytes
	published := false
	defer func() {
		if !published {
			self.entries, self.bytes = entries, bytes
		}
	}()
	if err := self.charge(struct {
		Measurement economicConservationProviderMeasurement
		ClosedWork  economicConservationClosedWork
	}{Measurement: value.Measurement, ClosedWork: value.ClosedWork}); err != nil {
		return err
	}
	for _, contract := range value.NewContracts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := self.charge(contract); err != nil {
			return err
		}
		if err := self.charge(struct {
			Key    economicProviderContractKey
			Census string
		}{Key: economicProviderContractKey{Domain: value.Domain, ContractId: contract.ContractId}, Census: key}); err != nil {
			return err
		}
	}
	if len(value.NewCreations) > payoutartifact.MaxClosedWorkRecords {
		return payoutartifact.ErrClosedWorkCapacity
	}
	for id, creation := range value.NewCreations {
		if err := ctx.Err(); err != nil {
			return err
		}
		checkpoint, exists := value.NewContracts[id]
		if !exists || creation.Original.ContractId != id || creation.Checkpoint != checkpoint {
			return errors.Join(errRpcIntegrity, payoutartifact.ErrClosedWorkIntegrity)
		}
		if err := self.charge(creation); err != nil {
			return err
		}
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return err
	}
	if self.providerCensuses == nil {
		self.providerCensuses = map[string]*economicProviderAdmitted{}
	}
	self.providerCensuses[key] = value
	if self.providerContracts == nil {
		self.providerContracts = map[economicProviderContractKey]map[string]struct{}{}
	}
	for contractId := range value.NewContracts {
		originalKey := economicProviderContractKey{Domain: value.Domain, ContractId: contractId}
		if self.providerContracts[originalKey] == nil {
			self.providerContracts[originalKey] = map[string]struct{}{}
		}
		self.providerContracts[originalKey][key] = struct{}{}
	}
	published = true
	return nil
}

// Refreshes derive solely from original admitted per-root values, including
// cold roots. An absent component cannot retain a prior true summary value.
func (self economicConservationEntitlement) providerMeasurement() *economicConservationProviderMeasurement {
	if self.Census != nil {
		return self.Census.ProviderMeasurements
	}
	if self.CensusReference != nil && self.CensusReference.ProviderMeasurements.ArtifactHash != "" {
		return &self.CensusReference.ProviderMeasurements
	}
	return nil
}

// All totals remain observations of original rows; only the exact full root
// count can make the enclosing authentication predicate true.
func (self *economicConservationEntitlementSummary) addProviderMeasurement(value *economicConservationProviderMeasurement) error {
	if value == nil {
		return nil
	}
	self.ProviderMeasurementRoots++
	self.ProviderMeasurementProviders += value.Providers
	for _, amount := range []struct {
		target *string
		value  uint64
	}{{target: &self.ProviderMeasurementBytes, value: value.CompletedBytes}, {target: &self.ProviderMeasurementAssignments, value: value.Assignments}, {target: &self.ProviderMeasurementConfirmations, value: value.Confirmations}} {
		if *amount.target == "" {
			*amount.target = "0"
		}
		next, err := economicConservationSum(*amount.target, strconv.FormatUint(amount.value, 10))
		if err != nil {
			return err
		}
		*amount.target = next
	}
	return nil
}

// Cold totals come only from admitted original references. Empty legacy totals
// carry no authenticated roots and retain the original omission grammar.
func (self *economicConservationEntitlementSummary) mergeProviderMeasurements(cold *economicConservationEntitlementSummary) error {
	if cold.ProviderMeasurementRoots == 0 {
		return nil
	}
	self.ProviderMeasurementRoots += cold.ProviderMeasurementRoots
	self.ProviderMeasurementProviders += cold.ProviderMeasurementProviders
	for _, amount := range []struct {
		target *string
		value  string
	}{{target: &self.ProviderMeasurementBytes, value: cold.ProviderMeasurementBytes}, {target: &self.ProviderMeasurementAssignments, value: cold.ProviderMeasurementAssignments}, {target: &self.ProviderMeasurementConfirmations, value: cold.ProviderMeasurementConfirmations}} {
		if *amount.target == "" {
			*amount.target = "0"
		}
		next, err := economicConservationSum(*amount.target, amount.value)
		if err != nil {
			return err
		}
		*amount.target = next
	}
	return nil
}
