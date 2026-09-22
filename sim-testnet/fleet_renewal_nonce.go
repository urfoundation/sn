// Fleet renewal owns only its oracle and keeper transaction nonce streams.
// Other deployment roles keep running while approval is reviewed; their fresh
// bounded observations may advance without changing a renewal transaction.
package main

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// This is the same finite checkpoint domain enforced by liability coverage.
const maximumFleetRenewalObservedNonce uint64 = 20000

// Custody and complete role membership stay exact. Finalized progress on a
// different signer is reconciled, while reversible latest/pending observations
// may settle or disappear. Renewal signers retain every approved nonce field.
func validateFleetRenewalNonceProgress(renewal FleetRenewal, fresh []FleetRenewalNonce) error {
	if len(renewal.EVMNonces) != len(fresh) {
		return errors.New("renewal nonce observation changed the deployment role census")
	}
	checkpoints := func(points []FleetRenewalNonce) (map[string]FleetRenewalNonce, error) {
		roles := make(map[string]FleetRenewalNonce, len(points))
		addresses := make(map[common.Address]bool, len(points))
		for _, point := range points {
			if _, duplicate := roles[point.Role]; duplicate || point.Role == "" || point.Address == (common.Address{}) || addresses[point.Address] || point.Finalized > point.Latest || point.Latest > point.Pending || point.Pending > maximumFleetRenewalObservedNonce {
				return nil, errors.New("renewal nonce observation has invalid or duplicated custody or exceeds its bound")
			}
			roles[point.Role], addresses[point.Address] = point, true
		}
		return roles, nil
	}
	if _, err := checkpoints(renewal.EVMNonces); err != nil {
		return err
	}
	observed, err := checkpoints(fresh)
	if err != nil {
		return err
	}
	for _, approved := range renewal.EVMNonces {
		current, exists := observed[approved.Role]
		if !exists || current.Address != approved.Address {
			return fmt.Errorf("renewal nonce observation changed role %s custody", approved.Role)
		}
		if approved.Address == renewal.Oracle || approved.Address == renewal.Keeper {
			if current != approved {
				return fmt.Errorf("renewal signer %s nonce changed since approval: finalized/latest/pending %d/%d/%d to %d/%d/%d", approved.Role, approved.Finalized, approved.Latest, approved.Pending, current.Finalized, current.Latest, current.Pending)
			}
		} else if current.Finalized < approved.Finalized {
			return fmt.Errorf("renewal role %s finalized nonce regressed since approval", approved.Role)
		}
	}
	return nil
}
