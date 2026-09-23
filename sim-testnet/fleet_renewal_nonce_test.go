// Reproduce approval becoming stale only because a different deployment role
// kept working. Transaction owners and financial authority remain immutable.
package main

import (
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// This fixture models the actual pre-apply checkpoint boundary without any
// chain traffic, keys, signing, fleet changes or production identities.
func fleetRenewalNonceProgressFixture() FleetRenewal {
	oracle, keeper, publisher := common.Address{1}, common.Address{2}, common.Address{3}
	return FleetRenewal{Oracle: oracle, Keeper: keeper, OracleNonce: 4, KeeperNonce: 6,
		ObservedEpoch: 18, ValidFromEpoch: 20, ValidToEpoch: 40, CampaignLiabilityWei: "100", SupersededGasCoveredWei: "0",
		EVMNonces: []FleetRenewalNonce{
			{Role: "synthetic-oracle", Address: oracle, Finalized: 4, Latest: 4, Pending: 4},
			{Role: "synthetic-keeper", Address: keeper, Finalized: 6, Latest: 6, Pending: 6},
			{Role: "synthetic-root-publisher", Address: publisher, Finalized: 10, Latest: 10, Pending: 10},
		}}
}

// An explicit barrier places another role's transaction after approval and
// before the same pre-apply validator used by the live command. No replan,
// re-sign or altered approval is required to continue that exact renewal.
func TestFleetRenewalNonceProgressConcurrentPublisherKeepsApproval(t *testing.T) {
	approved := fleetRenewalNonceProgressFixture()
	before, err := canonicalHashHex(approved)
	if err != nil {
		t.Fatal(err)
	}
	advance := make(chan struct{})
	observed := make(chan fleetRenewalObservation, 1)
	go func() {
		<-advance
		fresh := approved
		fresh.EVMNonces = slices.Clone(approved.EVMNonces)
		fresh.EVMNonces[2].Finalized++
		fresh.EVMNonces[2].Latest++
		fresh.EVMNonces[2].Pending++
		observed <- fleetRenewalObservation{Renewal: fresh}
	}()
	close(advance)
	fresh := <-observed
	if err := validateFleetRenewalFreshPrestate(approved, fresh); err != nil {
		t.Fatal("unrelated publisher progress stopped the approved renewal", err)
	}
	if after, err := canonicalHashHex(approved); err != nil || after != before {
		t.Fatal("nonce reconciliation changed approved transactions or their authority", err)
	}
	if approved.OracleNonce != fresh.Renewal.OracleNonce || approved.KeeperNonce != fresh.Renewal.KeeperNonce {
		t.Fatal("renewal acquired a different transaction nonce")
	}
}

// Finalized observations cannot go backward. An unrelated unconfirmed nonce
// can disappear from the pending pool without authorizing any replacement.
func TestFleetRenewalNonceProgressReconcilesUnconfirmedObservations(t *testing.T) {
	approved := fleetRenewalNonceProgressFixture()
	approved.EVMNonces[2].Latest, approved.EVMNonces[2].Pending = 11, 12
	fresh := approved
	fresh.EVMNonces = slices.Clone(approved.EVMNonces)
	fresh.EVMNonces[2].Latest, fresh.EVMNonces[2].Pending = 10, 10
	slices.Reverse(fresh.EVMNonces)
	if err := validateFleetRenewalFreshPrestate(approved, fleetRenewalObservation{Renewal: fresh}); err != nil {
		t.Fatal("unconfirmed non-renewal observation required a new economic approval", err)
	}
	fresh.EVMNonces[0].Finalized = 9
	if err := validateFleetRenewalFreshPrestate(approved, fleetRenewalObservation{Renewal: fresh}); err == nil {
		t.Fatal("finalized non-renewal history regressed")
	}
}

// Apply still rejects changed transaction owners/nonces, an expired window,
// additional signed liabilities and changed custody before it can write.
func TestFleetRenewalNonceProgressRetainsTransactionAndBudgetGates(t *testing.T) {
	approved := fleetRenewalNonceProgressFixture()
	for _, fault := range []string{"oracle-checkpoint", "keeper-checkpoint", "oracle-transaction", "keeper-transaction", "oracle-custody", "keeper-custody", "liability", "credit", "transactions", "window"} {
		fresh := approved
		fresh.EVMNonces = slices.Clone(approved.EVMNonces)
		fresh.EVMNonces[2].Finalized, fresh.EVMNonces[2].Latest, fresh.EVMNonces[2].Pending = 11, 11, 11
		switch fault {
		case "oracle-checkpoint":
			fresh.EVMNonces[0].Pending++
		case "keeper-checkpoint":
			fresh.EVMNonces[1].Pending++
		case "oracle-transaction":
			fresh.OracleNonce++
		case "keeper-transaction":
			fresh.KeeperNonce++
		case "oracle-custody":
			fresh.Oracle = common.Address{4}
		case "keeper-custody":
			fresh.Keeper = common.Address{4}
		case "liability":
			fresh.CampaignLiabilityWei = "101"
		case "credit":
			fresh.SupersededGasCoveredWei = "1"
		case "transactions":
			fresh.TransactionEvidence = []string{"synthetic additional signed liability"}
		case "window":
			fresh.ObservedEpoch = approved.ValidFromEpoch
		}
		if err := validateFleetRenewalFreshPrestate(approved, fleetRenewalObservation{Renewal: fresh}); err == nil {
			t.Errorf("reconciliation waived the %s gate", fault)
		}
	}
}

// Complete, bounded custody is required for every role, including a role that
// is free to make progress. Missing/duplicate/foreign checkpoints cannot hide.
func TestFleetRenewalNonceProgressRejectsChangedOrUnboundedCensus(t *testing.T) {
	approved := fleetRenewalNonceProgressFixture()
	for _, fault := range []string{"missing", "duplicate-role", "duplicate-address", "changed-role", "changed-address", "zero-address", "finalized-above-latest", "latest-above-pending", "over-bound"} {
		fresh := slices.Clone(approved.EVMNonces)
		switch fault {
		case "missing":
			fresh = fresh[:2]
		case "duplicate-role":
			fresh[2].Role = fresh[0].Role
		case "duplicate-address":
			fresh[2].Address = fresh[0].Address
		case "changed-role":
			fresh[2].Role = "foreign-role"
		case "changed-address":
			fresh[2].Address = common.Address{4}
		case "zero-address":
			fresh[2].Address = common.Address{}
		case "finalized-above-latest":
			fresh[2].Finalized++
		case "latest-above-pending":
			fresh[2].Latest++
		case "over-bound":
			fresh[2].Pending = maximumFleetRenewalObservedNonce + 1
		}
		if err := validateFleetRenewalNonceProgress(approved, fresh); err == nil {
			t.Errorf("reconciliation accepted %s", fault)
		}
	}
	fresh := slices.Clone(approved.EVMNonces)
	fresh[2].Pending = maximumFleetRenewalObservedNonce
	if err := validateFleetRenewalNonceProgress(approved, fresh); err != nil {
		t.Fatal("exact bounded non-renewal observation refused", err)
	}
}
