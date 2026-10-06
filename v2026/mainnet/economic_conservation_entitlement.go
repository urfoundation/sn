// Complete original payout artifacts are bound to the authorized coordinator
// commitment and vault finalization. Leaf amounts are obligations, not income;
// the artifact's signer is deliberately distinct from the on-chain committer.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

const economicEntitlementCensusSchema = "urnetwork-economic-entitlement-census-v1"
const maximumEconomicEntitlementProviders = 4096

// The original policy selects transport and contract purpose, never leaf
// amounts or an artifact signer. Actual authorization comes from the original
// epoch's rootSigner and its included OperatorRootCommitted receipt.
type economicConservationEntitlementSource struct {
	ProviderMeasurements *economicProviderMeasurementPolicy `json:"provider_measurements,omitempty"`
	PoolId               string                             `json:"pool_id"`
	Endpoint             string                             `json:"endpoint"`
	DeploymentId         string                             `json:"deployment_id"`
	Coordinator          string                             `json:"coordinator"`
	CoordinatorCodeHash  string                             `json:"coordinator_code_hash"`
}

type economicConservationEntitlementPolicy struct {
	Sources []economicConservationEntitlementSource `json:"sources"`
}

// These are original, block-pinned observations. RPC state/code assertions
// and the root-authenticated receipt inclusion remain explicitly different
// authority from independent consensus finality or measured provider usage.
type economicConservationEntitlementCensus struct {
	ProviderOriginals        *economicProviderOriginals               `json:"original_provider_evidence,omitempty"`
	ProviderMeasurements     *economicConservationProviderMeasurement `json:"original_provider_measurements,omitempty"`
	providerAttempts         *validator.VerifiedProviderAttemptMeasurement
	WindowClock              *payoutartifact.ClosedWorkWindowClock `json:"original_window_clock,omitempty"`
	Schema                   string                                `json:"schema"`
	Entitlement              string                                `json:"entitlement"`
	Finalization             monitorEconomicEvmEvent               `json:"original_finalization"`
	Commitment               monitorEconomicEvmEvent               `json:"original_commitment"`
	CoordinatorFinalization  monitorEconomicEvmEvent               `json:"coordinator_finalization"`
	CoordinatorCodeHash      string                                `json:"coordinator_code_hash"`
	CommitmentReceiptsRoot   string                                `json:"commitment_receipts_root"`
	FinalizationReceiptsRoot string                                `json:"finalization_receipts_root"`
	RootSigner               string                                `json:"original_root_signer"`
	OperatorColdkey          string                                `json:"original_operator_coldkey"`
	OperatorEffectiveEpoch   uint64                                `json:"operator_effective_epoch"`
	PolicyHash               string                                `json:"original_epoch_policy_hash"`
	Start                    payoutartifact.Boundary               `json:"original_epoch_start"`
	End                      payoutartifact.Boundary               `json:"original_epoch_end"`
	FundingHash              string                                `json:"original_funding_hash"`
	Artifact                 payoutartifact.Artifact               `json:"original_artifact"`
	ClosedWork               *economicConservationClosedWork       `json:"original_closed_work,omitempty"`
	LeafObligationsAlpha     string                                `json:"leaf_obligations_alpha"`
	FloorResidueAlpha        string                                `json:"floor_residue_alpha"`
	ContentHash              string                                `json:"content_hash"`
}

// A complete artifact can become cold while its unclaimed obligation remains
// active. The exact original snapshot supplies the receipt and leaf index at
// admission; this compact head is never accepted without that held archive.
type economicConservationEntitlementReference struct {
	ProviderMeasurements economicConservationProviderMeasurement `json:"original_provider_measurements,omitzero"`
	ClosedWork           economicConservationClosedWork          `json:"original_closed_work,omitzero"`
	Original             monitorHistoryReference                 `json:"original"`
	ContentHash          string                                  `json:"content_hash"`
	FundingHash          string                                  `json:"funding_hash"`
	Providers            uint64                                  `json:"providers"`
	Leaves               uint64                                  `json:"leaves"`
	LeafObligationsAlpha string                                  `json:"leaf_obligations_alpha"`
	FloorResidueAlpha    string                                  `json:"floor_residue_alpha"`
}

func (self economicConservationEntitlement) censusHash() string {
	if self.Census != nil {
		return self.Census.ContentHash
	}
	if self.CensusReference != nil {
		return self.CensusReference.ContentHash
	}
	return ""
}

func (self economicConservationEntitlement) censusSummary() (uint64, string, string) {
	if self.Census != nil {
		return uint64(len(self.Census.Artifact.Leaves)), self.Census.LeafObligationsAlpha, self.Census.FloorResidueAlpha
	}
	if self.CensusReference != nil {
		return self.CensusReference.Leaves, self.CensusReference.LeafObligationsAlpha, self.CensusReference.FloorResidueAlpha
	}
	return 0, "0", "0"
}

func (self *economicConservationEntitlement) retireCensus(reference monitorHistoryReference) {
	if self.Census == nil {
		return
	}
	value := self.Census
	self.CensusReference = &economicConservationEntitlementReference{Original: reference, ContentHash: value.ContentHash, FundingHash: value.FundingHash, Providers: uint64(len(value.Artifact.Providers)), Leaves: uint64(len(value.Artifact.Leaves)), LeafObligationsAlpha: value.LeafObligationsAlpha, FloorResidueAlpha: value.FloorResidueAlpha}
	if value.ClosedWork != nil {
		self.CensusReference.ClosedWork = *value.ClosedWork
	}
	if value.ProviderMeasurements != nil {
		self.CensusReference.ProviderMeasurements = *value.ProviderMeasurements
	}
	self.Census = nil
}

type economicConservationEntitlementSummary struct {
	ProviderMeasurementRoots          uint64  `json:"original_provider_measurement_roots,omitempty"`
	ProviderMeasurementProviders      uint64  `json:"original_provider_measurement_providers,omitempty"`
	ProviderMeasurementBytes          string  `json:"original_provider_completed_bytes,omitempty"`
	ProviderMeasurementAssignments    string  `json:"original_provider_assignments,omitempty"`
	ProviderMeasurementConfirmations  string  `json:"original_provider_confirmations,omitempty"`
	ClosedWorkWindows                 uint64  `json:"closed_work_windows,omitempty"`
	ClockMatchedWindows               uint64  `json:"clock_matched_windows,omitempty"`
	CompleteReportInventories         uint64  `json:"complete_report_inventories,omitempty"`
	InventoryReports                  uint64  `json:"original_inventory_reports,omitempty"`
	SignedCloseReports                uint64  `json:"signed_close_reports,omitempty"`
	RegisteredCloseReports            uint64  `json:"registered_close_reports,omitempty"`
	CloseAmountJoins                  uint64  `json:"close_amount_joins,omitempty"`
	ClosedWorkRoots                   uint64  `json:"original_closed_work_roots,omitempty"`
	ClosedWorkContracts               uint64  `json:"original_closed_work_contracts,omitempty"`
	ClosedWorkUsageBytes              string  `json:"original_closed_work_usage_bytes,omitempty"`
	SelectedPools                     uint64  `json:"selected_pools"`
	FinalizedRoots                    uint64  `json:"finalized_roots"`
	CompleteRoots                     uint64  `json:"complete_original_roots"`
	PendingRoots                      uint64  `json:"pending_roots"`
	HeldRoots                         uint64  `json:"held_roots"`
	CapacityHeldRoots                 uint64  `json:"capacity_held_roots"`
	UnknownOriginalRoots              uint64  `json:"unknown_original_roots"`
	RootsWithUnattributedFunding      uint64  `json:"roots_with_unattributed_funding"`
	Leaves                            uint64  `json:"leaves"`
	LeafObligationsAlpha              string  `json:"leaf_obligations_alpha"`
	FloorResidueAlpha                 string  `json:"floor_residue_alpha"`
	CompleteObservedCensus            bool    `json:"complete_observed_census"`
	ProviderMeasurementsAuthenticated bool    `json:"provider_measurements_authenticated"`
	IndependentFinalityAuthenticated  bool    `json:"independent_finality_authenticated"`
	NativeIncomeFundingAlpha          *string `json:"native_income_funding_alpha"`
	CapitalFundingAlpha               *string `json:"capital_funding_alpha"`
	CapitalSubsidyAuthorized          bool    `json:"capital_subsidy_authorized"`
	FundingComposition                string  `json:"funding_composition"`
	Authority                         string  `json:"authority"`
}

func (self economicConservationPolicy) validateEntitlementSources() error {
	if self.EntitlementSources == nil {
		return nil
	}
	sources := self.EntitlementSources.Sources
	if len(sources) != len(self.Vault.PoolIds) {
		return errors.New("economic entitlement source census must cover every originally selected pool")
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if err := source.ProviderMeasurements.validate(self); err != nil {
			return err
		}
		if !slices.Contains(self.Vault.PoolIds, source.PoolId) || seen[source.PoolId] || !monitorEvmAddress(source.Coordinator) || !rootCanonicalHash(source.CoordinatorCodeHash) || source.Coordinator == self.Vault.Address {
			return errors.New("economic entitlement source changed original pool or contract identity")
		}
		reader, err := validator.NewHTTPArtifactReader(source.Endpoint, source.DeploymentId, self.Vault.Netuid)
		if err != nil {
			return err
		}
		reader.CloseIdleConnections()
		seen[source.PoolId] = true
	}
	return nil
}

func (self economicConservationPolicy) entitlementSource(pool string) (economicConservationEntitlementSource, bool) {
	if self.EntitlementSources != nil {
		for _, source := range self.EntitlementSources.Sources {
			if source.PoolId == pool {
				return source, true
			}
		}
	}
	return economicConservationEntitlementSource{}, false
}

func economicEntitlementFundingHash(record economicConservationEntitlement) string {
	return rootObjectHash(struct {
		Id      string                        `json:"id"`
		Funded  string                        `json:"funded"`
		Total   *string                       `json:"total"`
		Sources []economicConservationBacking `json:"sources"`
	}{Id: record.Id, Funded: record.Funded, Total: record.Total, Sources: record.Sources})
}

func (self economicConservationEntitlementCensus) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

// Recompute every share and proof from the original complete provider vector.
// Equal aggregate totals cannot authorize a different recipient allocation.
func (self *economicConservationEntitlementCensus) validate(ctx context.Context, policy economicConservationPolicy, record economicConservationEntitlement) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, known := policy.entitlementSource(record.PoolId)
	if self == nil || !known || record.Total == nil || record.Finalization == nil || self.Schema != economicEntitlementCensusSchema || self.ContentHash != self.hash() || self.Entitlement != record.Id || self.FundingHash != economicEntitlementFundingHash(record) || rootObjectHash(self.Finalization) != rootObjectHash(*record.Finalization) {
		return errors.New("economic entitlement census lost original root, funding or finalization")
	}
	artifact := &self.Artifact
	epoch, e1 := monitorEconomicInteger(record.Epoch)
	pool, e2 := monitorEconomicInteger(record.PoolId)
	if e1 != nil || e2 != nil || !epoch.IsUint64() || !pool.IsUint64() || artifact.Epoch != epoch.Uint64() || artifact.NoID != pool.Uint64() || artifact.DeploymentID != source.DeploymentId || artifact.ChainID != policy.Vault.Network.EvmChainId || artifact.Netuid != policy.Vault.Netuid || artifact.GenesisHash != policy.Vault.Network.GenesisHash || artifact.Coordinator != common.HexToAddress(source.Coordinator) || artifact.SettlementVault != common.HexToAddress(policy.Vault.Address) || artifact.PolicyHash != self.PolicyHash || artifact.Start != self.Start || artifact.End != self.End || artifact.ContentHash != "sha256:"+strings.TrimPrefix(record.ArtifactHash, "0x") || common.Hash(artifact.PayoutRoot).Hex() != record.PayoutRoot {
		return errors.New("economic payout artifact differs from original authorized root or epoch")
	}
	if len(artifact.Providers) == 0 || len(artifact.Providers) > maximumEconomicEntitlementProviders || len(artifact.Leaves) == 0 || len(artifact.Leaves) > maximumEconomicEntitlementProviders {
		return errMonitorEconomicCapacity
	}
	commit, final := self.Commitment, self.CoordinatorFinalization
	if !monitorEvmAddress(self.RootSigner) || self.CoordinatorCodeHash != source.CoordinatorCodeHash || !rootCanonicalHash(self.CommitmentReceiptsRoot) || !rootCanonicalHash(self.FinalizationReceiptsRoot) || !rootCanonicalHash(self.OperatorColdkey) || self.OperatorEffectiveEpoch > artifact.Epoch || !rootCanonicalHash(self.PolicyHash) || commit.Name != "OperatorRootCommitted" || commit.Values["epoch"] != record.Epoch || commit.Values["noId"] != record.PoolId || commit.Values["payoutRoot"] != record.PayoutRoot || commit.Values["artifactHash"] != record.ArtifactHash || commit.Values["committer"] != self.RootSigner || !planSha256(commit.ReceiptHash) || !rootCanonicalHash(commit.TransactionHash) || commit.Block.Number < self.End.Number || commit.Block.Number > self.Finalization.Block.Number || final.Name != "OperatorEpochFinalized" || final.Values["epoch"] != record.Epoch || final.Values["noId"] != record.PoolId || final.Values["rootPresent"] != "true" || final.Block != self.Finalization.Block || final.TransactionHash != self.Finalization.TransactionHash || final.ReceiptHash != self.Finalization.ReceiptHash {
		return errors.New("economic entitlement lost original coordinator authorization or included finalization")
	}
	if err := payoutartifact.VerifyWithContext(ctx, artifact); err != nil {
		return err
	}
	if err := self.validateClosedWork(ctx, source.ProviderMeasurements != nil); err != nil {
		return err
	}
	total, err := monitorEconomicInteger(*record.Total)
	if err != nil {
		return err
	}
	allocated := new(big.Int)
	for _, leaf := range artifact.Leaves {
		if err := ctx.Err(); err != nil {
			return err
		}
		allocated.Add(allocated, new(big.Int).Quo(new(big.Int).Mul(total, new(big.Int).SetUint64(leaf.ShareBPS)), big.NewInt(10000)))
	}
	if allocated.Cmp(total) > 0 || allocated.String() != self.LeafObligationsAlpha || new(big.Int).Sub(total, allocated).String() != self.FloorResidueAlpha {
		return errors.New("economic entitlement changed exact leaf floor or original residue")
	}
	return ctx.Err()
}

// Funding identifiers may recur only through one named carry edge. A repeated
// source is never additional income, including two roots with equal totals.
func validateEconomicEntitlementFunding(record economicConservationEntitlement) error {
	seen := map[string]bool{}
	total := new(big.Int)
	for _, source := range record.Sources {
		key := source.Kind + "/" + source.Id
		if source.Id == "" || seen[key] {
			return errors.New("economic entitlement repeats an original funding source")
		}
		amount, err := monitorEconomicInteger(source.Amount)
		if err != nil {
			return err
		}
		switch source.Kind {
		case "capture", "opening-funding-unattributed", "opening-carry-unattributed", "root-missed", "expired-entitlement":
		default:
			return errors.New("economic entitlement funding has an unknown original cause")
		}
		total.Add(total, amount)
		seen[key] = true
	}
	expected := record.Funded
	if record.Total != nil {
		expected = *record.Total
	}
	if total.String() != expected {
		return errors.New("economic entitlement funding does not conserve its original total")
	}
	return nil
}

// Follow only indexed original edges. Carry is spent once in its own pool.
// Late finalization can consume a newer epoch's already available carry, so
// original receipt order, not the epoch number, determines its availability.
func (self *economicConservationState) reconcileEntitlementFunding(ctx context.Context) error {
	active := make(map[string]economicConservationEntitlement, len(self.Entitlements))
	captures := make(map[string]economicConservationCapture, len(self.Captures))
	used := map[string]string{}
	for _, record := range self.Entitlements {
		if _, exists := active[record.Id]; exists || record.Id != economicConservationEntitlementId(record.Epoch, record.PoolId) {
			return errors.New("economic funding repeated or relabelled an original entitlement")
		}
		active[record.Id] = record
	}
	for _, capture := range self.Captures {
		captures[capture.Id] = capture
	}
	for _, record := range self.Entitlements {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateEconomicEntitlementFunding(record); err != nil {
			return err
		}
		for _, source := range record.Sources {
			key := source.Kind + "/" + source.Id
			if prior := used[key]; prior != "" && prior != record.Id {
				return errors.New("economic original funding was consumed by two entitlements")
			}
			if self.archiveView != nil {
				if prior := self.archiveView.entitlementFundingUses[key]; prior != "" && prior != record.Id {
					return errors.New("economic original archived funding was consumed twice")
				}
			}
			used[key] = record.Id
			switch source.Kind {
			case "opening-funding-unattributed":
				if source.Id != record.Id || source.Amount != record.Funded {
					return errors.New("economic opening funding changed its explicitly unknown original obligation")
				}
			case "opening-carry-unattributed":
				if self.OpeningVault == nil || source.Id != rootObjectHash(*self.OpeningVault)+"/"+record.PoolId || self.OpeningVault.Pools[record.PoolId] != source.Amount {
					return errors.New("economic opening carry moved to another pool")
				}
			case "capture":
				if source.Amount != record.Funded {
					return errors.New("economic funding differs from its original capture amount")
				}
				if capture, exists := captures[source.Id]; exists {
					if capture.Event.Values["epoch"] != record.Epoch || capture.Event.Values["noId"] != record.PoolId || capture.Event.Values["amount"] != source.Amount {
						return errors.New("economic funding changed its original capture pool, epoch or amount")
					}
				} else if self.archiveView == nil || self.archiveView.captureKeys[record.Id] != source.Id {
					return errors.New("economic funding lost its original capture receipt")
				}
			case "root-missed", "expired-entitlement":
				prior, exists := active[source.Id]
				if !exists && self.archiveView != nil {
					prior, exists = self.archiveView.entitlements[source.Id]
				}
				from, fromErr := monitorEconomicInteger(prior.Epoch)
				to, toErr := monitorEconomicInteger(record.Epoch)
				if !exists || prior.PoolId != record.PoolId || fromErr != nil || toErr != nil || from.Cmp(to) == 0 || prior.CarryEvent == nil || record.Finalization == nil {
					return errors.New("economic carry lost its original source epoch and pool receipt")
				}
				carry, final := prior.CarryEvent, record.Finalization
				if carry.Block.Number > final.Block.Number || carry.Block.Number == final.Block.Number && (carry.Block.Hash != final.Block.Hash || carry.LogIndex >= final.LogIndex || carry.TransactionIndex > final.TransactionIndex || carry.TransactionIndex == final.TransactionIndex && (carry.TransactionHash != final.TransactionHash || carry.ReceiptHash != final.ReceiptHash)) {
					return errors.New("economic carry was not available before original finalization receipt")
				}
				if source.Kind == "root-missed" {
					if prior.Status != "root-missed" || prior.CarryEvent.Name != "RootMissed" || prior.CarryEvent.Values["carried"] != source.Amount || prior.Funded != source.Amount {
						return errors.New("economic missed-root carry changed original captured funding")
					}
				} else if prior.Status != "carried" || prior.CarryEvent.Name != "EntitlementExpired" || prior.CarryEvent.Values["unclaimed"] != source.Amount {
					return errors.New("economic expired entitlement changed its original unclaimed carry")
				}
			}
		}
	}
	return ctx.Err()
}

func (self *economicConservationState) validateEntitlementShapes(policy economicConservationPolicy) error {
	for pool, id := range self.EntitlementReadAfter {
		if _, known := policy.entitlementSource(pool); !known || id == "" || len(id) > 160 {
			return errors.New("economic artifact read cursor changed original pool")
		}
	}
	for _, record := range self.Entitlements {
		if policy.EntitlementSources == nil {
			if record.Census != nil || record.CensusReference != nil || record.Finalization != nil || record.CarryEvent != nil || record.CensusIssue != "" || record.CensusHeld || record.CensusCapacityBasis != "" {
				return errors.New("economic entitlement evidence has no original selected source")
			}
			continue
		}
		if err := validateEconomicEntitlementFunding(record); err != nil {
			return err
		}
		if len(record.CensusIssue) > 2048 || record.CensusHeld && record.CensusIssue == "" || record.CensusCapacityBasis != "" && (!planSha256(record.CensusCapacityBasis) || record.CensusIssue == "") {
			return errors.New("economic entitlement issue has no bounded original cause")
		}
		if record.Finalization != nil && (record.Finalization.Name != "EntitlementFinalized" || record.Finalization.Values["epoch"] != record.Epoch || record.Finalization.Values["noId"] != record.PoolId || record.Finalization.Values["payoutRoot"] != record.PayoutRoot || record.Finalization.Values["artifactHash"] != record.ArtifactHash || record.Total == nil || record.Finalization.Values["total"] != *record.Total) {
			return errors.New("economic entitlement finalization differs from original obligation")
		}
		if record.CarryEvent != nil && (record.CarryEvent.Values["epoch"] != record.Epoch || record.CarryEvent.Values["noId"] != record.PoolId || record.CarryEvent.Name != "RootMissed" && record.CarryEvent.Name != "EntitlementExpired") {
			return errors.New("economic entitlement carry changed its original pool or source epoch")
		}
		if record.Census != nil {
			if record.CensusReference != nil || policy.EntitlementSources == nil || record.Census.Entitlement != record.Id || record.Census.ContentHash != record.Census.hash() || record.Census.FundingHash != economicEntitlementFundingHash(record) || len(record.Census.Artifact.Providers) > maximumEconomicEntitlementProviders || len(record.Census.Artifact.Leaves) > maximumEconomicEntitlementProviders {
				return errors.New("economic retained entitlement census shape differs")
			}
		}
		if reference := record.CensusReference; reference != nil {
			if policy.EntitlementSources == nil || record.Finalization == nil || !planSha256(reference.ContentHash) || reference.FundingHash != economicEntitlementFundingHash(record) || reference.Providers == 0 || reference.Providers > maximumEconomicEntitlementProviders || reference.Leaves == 0 || reference.Leaves > reference.Providers {
				return errors.New("economic compact entitlement lost original census identity")
			}
			if err := policy.validateReference(reference.Original); err != nil {
				return err
			}
			amount, err := economicConservationSum(reference.LeafObligationsAlpha, reference.FloorResidueAlpha)
			if err != nil || record.Total == nil || amount != *record.Total {
				return errors.New("economic compact entitlement changed original leaf obligations or residue")
			}
		}
	}
	return nil
}

func (self economicConservationState) entitlementCensusFacts() uint64 {
	var count uint64
	for _, record := range self.Entitlements {
		if record.Census != nil {
			count += 1 + uint64(max(len(record.Census.Artifact.Providers), len(record.Census.Artifact.Leaves)))
			if originals := record.Census.ProviderOriginals; originals != nil {
				count += uint64(len(originals.Wallets))
				for _, wallet := range originals.Wallets {
					count += uint64(len(wallet.Originals))
				}
				count += uint64(len(originals.NetworkWallets))
				for _, wallet := range originals.NetworkWallets {
					count += uint64(len(wallet.Originals))
				}
				if originals.Work != nil {
					count += uint64(len(originals.Work.Owners))
				}
			}
			if record.Census.Artifact.ClosedWork != nil {
				count += uint64(len(record.Census.Artifact.ClosedWork.Records))
			}
		} else if record.CensusReference != nil {
			count++
		}
	}
	return count
}

// The summary counts original roots exactly once. Archived obligations retain
// their original total and floor residue; later payments are never new funding.
func (self *economicConservationState) entitlementCensusSummary(ctx context.Context, policy economicConservationPolicy) (*economicConservationEntitlementSummary, error) {
	if policy.EntitlementSources == nil {
		return nil, nil
	}
	providerSelected := false
	for _, source := range policy.EntitlementSources.Sources {
		providerSelected = providerSelected || source.ProviderMeasurements != nil
	}
	if providerSelected {
		if self.archiveView == nil {
			return nil, errors.New("economic provider summary lacks its admitted original owner")
		}
		if err := errors.Join(ctx.Err(), self.archiveView.checkAdmission()); err != nil {
			return nil, err
		}
	}
	result := &economicConservationEntitlementSummary{SelectedPools: uint64(len(policy.EntitlementSources.Sources)), LeafObligationsAlpha: "0", FloorResidueAlpha: "0", Authority: "committed-original-artifact-and-rpc-pinned-authorized-receipts", FundingComposition: "vault-obligations-observed-native-income-capital-deposits-and-refunds-unresolved"}
	add := func(record economicConservationEntitlement) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.PayoutRoot == "" {
			if record.Status == "opening-obligation-unattributed" || record.Status == "carried" && record.Total == nil {
				result.UnknownOriginalRoots++
			}
			return nil
		}
		for _, source := range record.Sources {
			if strings.HasPrefix(source.Kind, "opening-") {
				result.RootsWithUnattributedFunding++
				break
			}
		}
		result.FinalizedRoots++
		if record.censusHash() == "" {
			result.PendingRoots++
			if record.CensusHeld {
				result.HeldRoots++
			}
			if record.CensusCapacityBasis != "" {
				result.CapacityHeldRoots++
			}
			return nil
		}
		result.CompleteRoots++
		if err := result.addProviderMeasurement(record.providerMeasurement()); err != nil {
			return err
		}
		result.addClosedWork(record.closedWork())
		leaves, obligations, residue := record.censusSummary()
		result.Leaves += leaves
		var err error
		result.LeafObligationsAlpha, err = economicConservationSum(result.LeafObligationsAlpha, obligations)
		if err != nil {
			return err
		}
		result.FloorResidueAlpha, err = economicConservationSum(result.FloorResidueAlpha, residue)
		return err
	}
	for _, record := range self.Entitlements {
		if err := add(record); err != nil {
			return nil, err
		}
	}
	if self.archiveView != nil && self.archiveView.entitlementCold != nil {
		cold := self.archiveView.entitlementCold
		result.FinalizedRoots += cold.FinalizedRoots
		result.CompleteRoots += cold.CompleteRoots
		result.PendingRoots += cold.PendingRoots
		result.HeldRoots += cold.HeldRoots
		result.CapacityHeldRoots += cold.CapacityHeldRoots
		result.UnknownOriginalRoots += cold.UnknownOriginalRoots
		result.RootsWithUnattributedFunding += cold.RootsWithUnattributedFunding
		result.Leaves += cold.Leaves
		if err := result.mergeProviderMeasurements(cold); err != nil {
			return nil, err
		}
		result.mergeClosedWork(cold)
		var err error
		result.LeafObligationsAlpha, err = economicConservationSum(result.LeafObligationsAlpha, cold.LeafObligationsAlpha)
		if err != nil {
			return nil, err
		}
		result.FloorResidueAlpha, err = economicConservationSum(result.FloorResidueAlpha, cold.FloorResidueAlpha)
		if err != nil {
			return nil, err
		}
	}
	result.CompleteObservedCensus = result.FinalizedRoots > 0 && result.PendingRoots == 0 && result.UnknownOriginalRoots == 0
	result.ProviderMeasurementsAuthenticated = result.CompleteObservedCensus && result.ProviderMeasurementRoots == result.FinalizedRoots
	if providerSelected {
		if err := errors.Join(ctx.Err(), self.archiveView.checkAdmission()); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// A claimed leaf must be in the complete original vector, even when another
// share assignment would leave the aggregate paid amount unchanged.
func (self *economicConservationState) reconcileEntitlementLeaves(ctx context.Context) error {
	if self.archiveView == nil {
		for _, record := range self.Entitlements {
			if record.censusHash() != "" {
				return errors.New("economic leaf reconciliation lacks its admitted original vector")
			}
		}
		return ctx.Err()
	}
	for _, claim := range self.Claims {
		if err := ctx.Err(); err != nil {
			return err
		}
		index, known := self.entitlementIds[claim.Entitlement]
		if !known {
			continue
		}
		record := self.Entitlements[index]
		if record.censusHash() == "" {
			continue
		}
		if err := economicEntitlementClaimMatches(self.archiveView.entitlementLeaves[record.censusHash()], claim); err != nil {
			return err
		}
	}
	return nil
}

// The private admission cache avoids repeating original signature/Merkle work
// on every sample. Only exact verified digests enter it; payloads remain in the
// checkpoint or retained original archive, under their held custody owners.
func (self *economicConservationArchiveView) admitEntitlementCensuses(ctx context.Context, policy economicConservationPolicy, state *economicConservationState) error {
	if self == nil || self.entitlementVerified == nil || state == nil {
		return errors.New("economic entitlement verification requires owned admission")
	}
	if err := self.checkAdmission(); err != nil {
		return err
	}
	providerSelected := false
	if policy.EntitlementSources != nil {
		for _, source := range policy.EntitlementSources.Sources {
			providerSelected = providerSelected || source.ProviderMeasurements != nil
		}
	}
	if providerSelected && state.entitlementIds == nil {
		if err := state.index(); err != nil {
			return err
		}
	}
	if err := self.requireOriginalEntitlementCensuses(state); err != nil {
		return err
	}
	if policy.EntitlementSources != nil {
		checked := *state
		checked.archiveView = self
		if err := checked.reconcileEntitlementFunding(ctx); err != nil {
			return err
		}
	}
	records := state.Entitlements
	if providerSelected {
		records = economicProviderAdmissionOrder(records)
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.CensusReference != nil {
			if known := self.entitlementReferences[record.CensusReference.ContentHash]; known == nil || *known != *record.CensusReference {
				return errors.New("economic compact census differs from its exact original archived receipt")
			}
			continue
		}
		if record.Census == nil {
			continue
		}
		key := record.Census.ContentHash
		binding := economicEntitlementFundingHash(record)
		if self.entitlementVerified[key] == binding {
			continue
		}
		if err := record.Census.validate(ctx, policy, record); err != nil {
			return err
		}
		if source, _ := policy.entitlementSource(record.PoolId); source.ProviderMeasurements != nil || record.Census.ProviderOriginals != nil || record.Census.ProviderMeasurements != nil {
			known := self.providerCensuses[key]
			if known == nil {
				var err error
				known, err = self.verifyProviderOriginals(ctx, policy, state, record)
				if err != nil {
					return err
				}
			}
			if known == nil || record.Census.ProviderMeasurements == nil || *record.Census.ProviderMeasurements != known.Measurement || record.Census.ClosedWork == nil || *record.Census.ClosedWork != known.ClosedWork {
				return errors.New("economic original provider projection differs from complete original witnesses")
			}
			if err := self.retainProviderOriginals(ctx, key, known); err != nil {
				return err
			}
		}
		leaves := make(map[string]string, len(record.Census.Artifact.Leaves))
		for _, leaf := range record.Census.Artifact.Leaves {
			if err := ctx.Err(); err != nil {
				return err
			}
			coldkey := common.Hash(leaf.Coldkey).Hex()
			if _, exists := leaves[coldkey]; exists {
				return errors.New("economic original artifact repeated a recipient")
			}
			leaves[coldkey] = fmt.Sprint(leaf.ShareBPS)
		}
		// Earlier paid claims may already be cold when an unavailable artifact
		// arrives. Index by entitlement once; do not scan the full archive here.
		for _, id := range self.entitlementClaimIds[record.Id] {
			if err := ctx.Err(); err != nil {
				return err
			}
			claim, exists := self.claims[id]
			if !exists {
				return errors.New("economic original claim index lost its retained receipt")
			}
			if err := economicEntitlementClaimMatches(leaves, claim); err != nil {
				return err
			}
		}
		if err := self.charge([]string{key, binding}); err != nil {
			return err
		}
		for coldkey, share := range leaves {
			if err := self.charge([]string{key, coldkey, share}); err != nil {
				return err
			}
		}
		self.providerCandidate.captureCensus(key)
		self.entitlementVerified[key] = binding
		self.entitlementLeaves[key] = leaves
	}
	return nil
}

// The key is the original entitlement and recipient, not the equal aggregate.
func economicEntitlementClaimMatches(leaves map[string]string, claim economicConservationClaim) error {
	share, exists := leaves[claim.Event.Values["coldkey"]]
	if !exists || share != claim.Event.Values["shareBps"] {
		return errors.New("economic accepted claim differs from complete original payout leaf census")
	}
	return nil
}

// Snapshot admission pins the first complete receipt. A later active head can
// neither remove it nor substitute another independently valid allocation.
func (self *economicConservationArchiveView) retainEntitlementCensuses(state, compacted *economicConservationState) error {
	if err := self.requireOriginalEntitlementCensuses(state); err != nil {
		return err
	}
	for _, record := range state.Entitlements {
		if record.Census == nil || self.entitlementOriginal[record.Id] != "" {
			continue
		}
		if self.entitlementVerified[record.Census.ContentHash] != economicEntitlementFundingHash(record) {
			return errors.New("economic archived entitlement lacks original census admission")
		}
		if err := self.charge([]string{record.Id, record.Census.ContentHash}); err != nil {
			return err
		}
		self.entitlementOriginal[record.Id] = record.Census.ContentHash
		self.entitlementRequired[record.Id] = record.Census.ContentHash
		if compacted.Archive == nil || len(compacted.Archive.Segments) == 0 {
			return errors.New("economic census retirement lacks its original snapshot")
		}
		record.retireCensus(compacted.Archive.Segments[len(compacted.Archive.Segments)-1])
		if err := self.charge(record.CensusReference); err != nil {
			return err
		}
		self.entitlementReferences[record.CensusReference.ContentHash] = record.CensusReference
	}
	return nil
}

func (self *economicConservationArchiveView) requireOriginalEntitlementCensuses(state *economicConservationState) error {
	if len(self.entitlementOriginal) == 0 {
		return nil
	}
	active := make(map[string]string, len(state.Entitlements))
	for _, record := range state.Entitlements {
		if hash := self.entitlementOriginal[record.Id]; hash != "" && record.censusHash() != hash {
			return errors.New("economic active entitlement omitted or replaced original archived census")
		}
		active[record.Id] = record.censusHash()
	}
	// Only obligations still required in the active head are traversed. Cold
	// receipts are looked up by exact identity; they are not rescanned per save.
	for id, hash := range self.entitlementRequired {
		if value, exists := active[id]; exists {
			if value != hash {
				return errors.New("economic active entitlement omitted or replaced original archived census")
			}
		} else if value, exists := self.entitlements[id]; !exists || value.censusHash() != hash {
			return errors.New("economic head lost original entitlement census lineage")
		}
	}
	return nil
}

func (self *economicConservationState) requireEntitlementHistory() error {
	for _, record := range self.Entitlements {
		if record.censusHash() != "" && (self.archiveView == nil || self.archiveView.entitlementVerified[record.censusHash()] != economicEntitlementFundingHash(record)) {
			return errors.New("economic entitlement publication requires original complete census admission")
		}
		if record.CensusReference != nil {
			if known := self.archiveView.entitlementReferences[record.CensusReference.ContentHash]; known == nil || *known != *record.CensusReference {
				return errors.New("economic compact census differs from its exact original archived receipt")
			}
		}
	}
	if self.archiveView != nil {
		return self.archiveView.requireOriginalEntitlementCensuses(self)
	}
	return nil
}
func (self *economicConservationArchiveView) indexColdEntitlement(record economicConservationEntitlement) error {
	if self.entitlementCold == nil {
		self.entitlementCold = &economicConservationEntitlementSummary{LeafObligationsAlpha: "0", FloorResidueAlpha: "0"}
	}
	value := self.entitlementCold
	if record.PayoutRoot == "" {
		if record.Status == "opening-obligation-unattributed" || record.Status == "carried" && record.Total == nil {
			value.UnknownOriginalRoots++
		}
		return nil
	}
	for _, source := range record.Sources {
		if strings.HasPrefix(source.Kind, "opening-") {
			value.RootsWithUnattributedFunding++
			break
		}
	}
	value.FinalizedRoots++
	if record.censusHash() == "" {
		value.PendingRoots++
		if record.CensusHeld {
			value.HeldRoots++
		}
		return nil
	}
	value.CompleteRoots++
	if err := value.addProviderMeasurement(record.providerMeasurement()); err != nil {
		return err
	}
	value.addClosedWork(record.closedWork())
	leaves, obligations, residue := record.censusSummary()
	value.Leaves += leaves
	var err error
	value.LeafObligationsAlpha, err = economicConservationSum(value.LeafObligationsAlpha, obligations)
	if err != nil {
		return err
	}
	value.FloorResidueAlpha, err = economicConservationSum(value.FloorResidueAlpha, residue)
	return err
}

// Only a completed validation failure establishes a contradiction. Resource
// refusal and cancellation retain their original non-financial causes.
func economicEntitlementEvidenceError(err error) error {
	if err == nil || errors.Is(err, errMonitorEconomicCapacity) || monitorOnlyCancellationCauses(err, 0) {
		return err
	}
	if errors.Is(err, payoutartifact.ErrClosedWorkUnavailable) || errors.Is(err, protocol.ErrProviderAttemptsUnavailable) || errors.Is(err, protocol.ErrWalletMappingUnavailable) {
		return economicProviderEvidenceError(err)
	}
	return errors.Join(errRpcIntegrity, err)
}

// An unchanged capacity refusal is not an instruction to fetch and verify the
// same large artifact on every sample. A new admitted resource revision or
// archive publication permits another bounded attempt against the same root.
func (self *economicConservationState) entitlementCapacityBasis(policy economicConservationPolicy) (string, error) {
	resources, err := self.resources(policy)
	if err != nil {
		return "", err
	}
	archive := ""
	if self.Archive != nil && len(self.Archive.Segments) != 0 {
		archive = self.Archive.Segments[len(self.Archive.Segments)-1].Sha256
	}
	return rootObjectHash(struct {
		Resources economicConservationResources `json:"resources"`
		Archive   string                        `json:"archive"`
	}{Resources: resources, Archive: archive}), nil
}
