// The current launch review receives the 90 percent remainder at an explicit
// public account. Historical owner-recycle inputs keep their original meaning.
package main

import "errors"

const bootstrapTreasuryPlanConfigSchema = "urnetwork-mainnet-plan-config-v2"
const bootstrapTreasuryReleaseInputSchema = "urnetwork-mainnet-release-input-v2"
const bootstrapTreasuryPlanSchema = "urnetwork-mainnet-blocked-plan-v3"

// A destination is a public declaration, not approval or evidence of ownership.
// The original input schema cannot acquire the successor through a new field.
func validateBootstrapPlanDestination(config bootstrapPlanConfig, treasury bool) error {
	if !treasury {
		if config.TreasuryDestination != nil {
			return errors.New("historical owner-recycle plan cannot contain a treasury destination")
		}
		return nil
	}
	destination := config.TreasuryDestination
	if destination == nil {
		return errors.New("treasury plan requires the public receive-only destination")
	}
	if err := destination.validate(); err != nil {
		return err
	}
	if destination.GenesisHash != config.Network.GenesisHash || destination.Netuid != config.Netuid {
		return errors.New("treasury plan destination differs from the selected native network")
	}
	return nil
}

// ReserveCredit denotes the target's recipient role, never an observed payment.
func bootstrapTreasuryEconomics() planEconomics {
	return planEconomics{Denominator: "native_miner_allocation_before_withholding", ProviderNumerator: 1, FractionDenominator: 10,
		RemainderNumerator: 9, Remainder: "ordinary-native-treasury", Assurance: "observed-native-target", ReserveCredit: true}
}

// All unchanged deployment and operational gates remain present. Receiving
// substitutes public account and native recipient proofs for owner recycling.
func bootstrapTreasuryRequirements() []planRequirement {
	requirements := bootstrapRequirements()
	for index := range requirements {
		requirement := &requirements[index]
		switch requirement.Id {
		case "roles":
			requirement.Description = "Public owner/deployer/Safe/guardian/oracle/pool/escrow/provider identities, receive-only reserve account and recipient hotkeys, the policy's UR validators (one sole validator for the SN25 launch), and the netuid-0 role, which never counts as a UR validator"
		case "custody":
			requirement.Description = "Bounded transaction signer/coldkey/Safe adapters, permissions, single-writer leases and durable nonce/receipt ownership; the receive-only reserve requires no signing or spending configuration"
		case "registration-plan":
			requirement.Description = "Approved pool/escrow/head/validator identities and reserve recipient hotkeys, existing native registrations or separately authorized bounded registration payloads, cost ceilings and expected postconditions"
		case "recycle-policy":
			requirement.Id = "treasury-policy"
			requirement.Description = "Independently approved receive-only treasury policy matching the public destination, complete native Owner/UID/hotkey registration generations, owner exclusions, provider-role separation, masks, cap, auto-stake destination and drained activation boundary; no reserve signatory, device or fee requirement"
		case "emission-activation-state":
			requirement.Description = "Finalized native activation boundary, exact applied treasury policy generation and complete ordinary recipient roster"
		case "native-emission-outcomes":
			requirement.Description = "Observed native miner allocation, provider 10 percent and receive-only reserve 90 percent targets within approved tolerance, actual reward credits, collateral and treasury conservation over the accepted production interval"
		}
	}
	return requirements
}

// The actual activation stays behind authenticated registered identities and
// independent policy authority. A receiving descriptor never enables an action.
func bootstrapTreasuryActions() []planAction {
	actions := bootstrapActions()
	for index := range actions {
		action := &actions[index]
		for dependency, id := range action.Requirements {
			if id == "recycle-policy" {
				action.Requirements[dependency] = "treasury-policy"
			}
		}
		switch action.Id {
		case "register-subnet-roles":
			action.Description = "Establish the approved pool, escrow, miner, reserve recipient and UR-validator registration generations"
		case "activate-native-miner-emissions":
			action.Description = "Activate the approved 10/90 evidence-based producer with ordinary native credit to the receive-only reserve; retain actual rows and native state before economic acceptance"
		}
	}
	return actions
}
