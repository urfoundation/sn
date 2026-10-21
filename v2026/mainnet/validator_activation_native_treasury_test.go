// Synthetic signed treasury configs exercise the public activation owner and
// exact native reader. Receiving never supplies process or spending authority.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// Both original approvals select the same complete receiving policy before
// custody preparation. The destination has no signing metadata or private key.
func newValidatorActivationTreasuryFixture(t *testing.T, mutate func(*types.Metadata)) *validatorActivationFixture {
	t.Helper()
	destination := [32]byte(bytes.Repeat([]byte{0x77}, 32))
	chain := newBootstrapRootPassiveFixtureWithCensus(t, func(census *rootRpcFixture, policy *subnetCensusPolicy) {
		metadata, encoded, hash := economicEmissionTestMetadata(t, mutate)
		census.metadata, census.metadataHex, census.policy.RuntimeMetadataHash = metadata, encoded, hash
		policy.RuntimeMetadataHash = hash
		arg := []byte{25, 0}
		census.set(t, "MechanismCountCurrent", []byte{1}, arg)
		census.set(t, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 20), arg)
		census.set(t, "PendingServerEmission", make([]byte, 8), arg)
		census.set(t, "LastUpdate", subnetTestVector(make([]byte, 6*8), 8), arg)
		census.set(t, "RecycleOrBurn", []byte{1}, arg)
		for index := range policy.Remove {
			policy.Remove[index].Coldkey = fmt.Sprintf("0x%x", destination)
			census.set(t, "Owner", destination[:], bytes.Repeat([]byte{byte(0x45 + index)}, 32))
		}
	}, bootstrapChainTreasuryApproval(destination))
	chain.result(t, "apply")
	return newValidatorActivationFixtureForChain(t, chain)
}

// Every producer approval selects the same receiving policy for the two
// synthetic recipient seats. Selection carries no recipient signing key.
func bootstrapChainTreasuryApproval(destination [32]byte) func(*bootstrapChainValidatorFixture) {
	return func(fixture *bootstrapChainValidatorFixture) {
		fixture.config.TreasuryApproval, fixture.config.OwnerRecycleApproval = fixture.config.OwnerRecycleApproval, nil
		approval := &fixture.approval
		approval.Schema, approval.Proposal.Schema, approval.Production.Schema = validator.TreasuryApprovalSchema, validator.TreasuryProposalSchema, validator.TreasuryProductionScope
		approval.Proposal.Remainder, approval.Proposal.OwnerAllocation = "ordinary_treasury_credit", "equal_exact_registered"
		approval.Proposal.Treasury = &validator.TreasuryPolicy{Schema: validator.TreasuryReceivePolicySchema, MultisigAccount: destination,
			Recipients: []validator.TreasuryRecipient{
				{Uid: 4, Hotkey: [32]byte(bytes.Repeat([]byte{0x45}, 32)), RegistrationBlock: 44},
				{Uid: 5, Hotkey: [32]byte(bytes.Repeat([]byte{0x46}, 32)), RegistrationBlock: 45},
			}, ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, TreasuryShare: protocol.Rational{Numerator: 9, Denominator: 10}, MaxWeightLimitU16: 32768}
	}
}

// The pre-fix public command refuses both Burn and authenticated absence even
// though the independently signed treasury producer admits those native states.
func TestValidatorActivationTreasuryNativePublicAdmission(t *testing.T) {
	for _, mode := range []string{"recycle", "burn", "absent"} {
		f := newValidatorActivationTreasuryFixture(t, nil)
		f.installed()
		original := f.chain.journals(t)
		preparationHash := f.chain.preparation.Plan.ContentHash
		prepared := f.chain.result(t, "resume")
		if prepared.PlanHash != preparationHash || prepared.CurrentEconomicAcceptance != "native-10-percent-provider-allocation-and-90-percent-native-treasury-acceptance" ||
			!reflect.DeepEqual(prepared.PendingChainPhases, f.chain.preparation.Plan.PendingChainPhases) || !reflect.DeepEqual(original, f.chain.journals(t)) {
			t.Fatal("treasury outcome report changed original sealed phases or custody", prepared)
		}
		want := "Burn"
		switch mode {
		case "recycle":
			want = "Recycle"
		case "burn":
			f.chain.census.set(t, "RecycleOrBurn", []byte{0}, []byte{25, 0})
		case "absent":
			subnetTestDelete(t, f.chain.census, "RecycleOrBurn", []byte{25, 0})
		}
		result, code, detail := f.command(t.Context(), "admit", nil)
		if code != 0 || result.Status != "admitted-process-only" || result.Readiness == nil || result.Readiness.Native == nil {
			t.Fatal("signed receiving policy did not reach public native admission", mode, code, result.Status, detail)
		}
		native := result.Readiness.Native
		hash, err := f.chain.preparation.Plan.ValidatorInspections[0].Approval.Proposal.Treasury.Hash()
		if err != nil || native.Schema != validatorActivationNativeTreasurySchema || native.TreasuryPolicyHash != fmt.Sprintf("sha256:%x", hash) ||
			native.RecycleMode != want || native.ActivationRecycleMode != want || native.ActivationPendingServerAlpha != 0 || native.MechanismCount != 1 {
			t.Fatal("treasury readiness lost its exact policy or native observations", mode, native, err)
		}
		result, code, detail = f.command(t.Context(), "start", nil)
		if code == 0 || result.Status != "activation-authority-unavailable" || f.starts != [2]int{} || f.authorityCalls != 0 ||
			result.ActivationReady || !reflect.DeepEqual(original, f.chain.journals(t)) {
			t.Fatal("receiving admission acquired process authority or changed original custody", mode, code, result.Status, detail)
		}
	}
}

// Relaxing the economic precondition does not relax the authenticated enum,
// exact value width or the requirement for one native emission mechanism.
func TestValidatorActivationTreasuryNativeMalformedModeRefused(t *testing.T) {
	for _, raw := range [][]byte{{}, {2}, {0, 0}, {1, 0}} {
		f := newValidatorActivationTreasuryFixture(t, nil)
		f.installed()
		f.chain.census.set(t, "RecycleOrBurn", raw, []byte{25, 0})
		result, code, detail := f.command(t.Context(), "start", f)
		if code == 0 || result.Readiness != nil || f.starts != [2]int{} || f.authorityCalls != 0 || !strings.Contains(detail, "mode is malformed") {
			t.Fatal("malformed treasury enum reached start authority", raw, code, result.Status, detail)
		}
	}
}

// Even separately approved metadata cannot reinterpret the reviewed Burn
// default. This fails before state reads and cannot turn an absent value ready.
func TestValidatorActivationTreasuryNativeDefaultCodecRefused(t *testing.T) {
	f := newValidatorActivationTreasuryFixture(t, func(metadata *types.Metadata) {
		for pi := range metadata.AsMetadataV14.Pallets {
			pallet := &metadata.AsMetadataV14.Pallets[pi]
			if pallet.Name == "SubtensorModule" {
				for index := range pallet.Storage.Items {
					if pallet.Storage.Items[index].Name == "RecycleOrBurn" {
						pallet.Storage.Items[index].Fallback = types.Bytes{1}
					}
				}
			}
		}
	})
	readiness := validatorActivationNativeTestReadiness(t, f)
	before := f.chain.census.count("state_getStorage")
	got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
	if got != nil || !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "fallback") || f.chain.census.count("state_getStorage") != before {
		t.Fatal("treasury metadata default drift reached storage or readiness", got, err)
	}
}

// Current and activation samples may not disagree at the same block, including
// explicit Burn versus metadata-default Burn with contradictory storage absence.
func TestValidatorActivationTreasuryNativeSameBlockModeConflict(t *testing.T) {
	for _, first := range []any{"0x01", nil} {
		f := newValidatorActivationTreasuryFixture(t, nil)
		readiness := validatorActivationNativeTestReadiness(t, f)
		key := f.chain.census.set(t, "RecycleOrBurn", []byte{0}, []byte{25, 0})
		reads := 0
		validatorActivationNativeTestIntercept(t, f.chain.client, func(call validatorActivationNativeTestCall, response *http.Response) (*http.Response, error) {
			if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+key+`"` {
				reads++
				if reads == 1 {
					return validatorActivationNativeTestReply(response, first)
				}
			}
			return response, nil
		})
		got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
		if got != nil || !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "changed at one native hash") || reads != 2 {
			t.Fatal("contradictory treasury mode observations admitted", first, got, err, reads)
		}
	}
}

// One role's policy cannot relax the other role's economic domain or substitute
// another treasury recipient set before native observations begin.
func TestValidatorActivationTreasuryNativeBothApprovalScopesRequired(t *testing.T) {
	for _, fault := range []string{"domain", "recipient", "production"} {
		f := newValidatorActivationTreasuryFixture(t, nil)
		readiness := validatorActivationNativeTestReadiness(t, f)
		preparation := f.chain.preparation
		preparation.Plan.ValidatorInspections = append([]validator.ProductionBootstrapInspection(nil), preparation.Plan.ValidatorInspections...)
		approval := &preparation.Plan.ValidatorInspections[1].Approval
		switch fault {
		case "domain":
			approval.Schema = "urnetwork-owner-recycle-approval-v2"
		case "recipient":
			policy := *approval.Proposal.Treasury
			policy.MultisigAccount[0]++
			approval.Proposal.Treasury = &policy
		case "production":
			approval.Production = nil
		}
		before := f.chain.census.count("state_getStorage")
		got, err := f.chain.client.observeValidatorActivationNative(t.Context(), preparation, readiness)
		if got != nil || !errors.Is(err, errRpcIntegrity) || f.chain.census.count("state_getStorage") != before {
			t.Fatal("foreign or incomplete treasury authority reached native state", fault, got, err)
		}
	}
}

// Legacy readiness keeps its exact omitted-field wire shape and strict mode;
// new treasury projections cannot be relabeled into that historical domain.
func TestValidatorActivationTreasuryNativeProjectionPreservesLegacy(t *testing.T) {
	legacy := newValidatorActivationFixture(t)
	legacy.installed()
	if prepared := legacy.chain.result(t, "resume"); prepared.CurrentEconomicAcceptance != "" {
		t.Fatal("legacy preparation acquired a treasury outcome", prepared)
	}
	result, code, detail := legacy.command(t.Context(), "admit", nil)
	if code != 0 || result.Readiness == nil || result.Readiness.Native == nil {
		t.Fatal("legacy native readiness unavailable", code, detail)
	}
	raw, err := json.Marshal(result.Readiness.Native)
	if err != nil || bytes.Contains(raw, []byte("treasury_policy_hash")) || bytes.Contains(raw, []byte("activation_recycle_mode")) || result.Readiness.Native.Schema != validatorActivationNativeSchema {
		t.Fatal("legacy readiness wire acquired treasury fields", err, string(raw))
	}
	changed := *result.Readiness.Native
	changed.RecycleMode = "Burn"
	if err := changed.validate(*result.Readiness); err == nil {
		t.Fatal("legacy readiness admitted Burn")
	}
	f := newValidatorActivationTreasuryFixture(t, nil)
	f.installed()
	result, code, detail = f.command(t.Context(), "admit", nil)
	if code != 0 || result.Readiness == nil || result.Readiness.Native == nil {
		t.Fatal("treasury native readiness unavailable", code, detail)
	}
	for _, fault := range []string{"legacy-schema", "missing-policy", "activation-mode"} {
		changed = *result.Readiness.Native
		switch fault {
		case "legacy-schema":
			changed.Schema = validatorActivationNativeSchema
		case "missing-policy":
			changed.TreasuryPolicyHash = ""
		case "activation-mode":
			changed.ActivationRecycleMode = "unknown"
		}
		if err := changed.validate(*result.Readiness); err == nil {
			t.Fatal("treasury readiness lost its economic domain or mode", fault)
		}
	}
}
