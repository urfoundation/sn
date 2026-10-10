// Synthetic signed policy and retained-record tests cover the accounting
// boundary. They do not claim a deployed runtime or hardware signing ceremony.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// Accounts are public fixture bytes, unrelated to a deployed Ledger identity.
func nativeTreasuryTestAccount(value byte) [32]byte {
	var result [32]byte
	for index := range result {
		result[index] = value
	}
	return result
}

func nativeTreasuryTestAuthority(t *testing.T) (*nativeTreasuryAuthority, economicEmissionPolicy) {
	t.Helper()
	account, err := crv4.DeriveNativeMultisigAccount([][32]byte{nativeTreasuryTestAccount(0x31), nativeTreasuryTestAccount(0x32)}, 2)
	if err != nil {
		t.Fatal(err)
	}
	public := validator.TreasuryPolicy{Schema: validator.TreasuryPolicySchema, MultisigAccount: account, Threshold: 2, Signatories: [][32]byte{nativeTreasuryTestAccount(0x31), nativeTreasuryTestAccount(0x32)}, Recipients: []validator.TreasuryRecipient{{Uid: 0, Hotkey: nativeTreasuryTestAccount(0x11), RegistrationBlock: 20}, {Uid: 1, Hotkey: nativeTreasuryTestAccount(0x22), RegistrationBlock: 21}}, ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, TreasuryShare: protocol.Rational{Numerator: 9, Denominator: 10}, MaxWeightLimitU16: 32768}
	policy := newEconomicEmissionFixture(t).policy
	policy.Runtime.RuntimeSourceCommit = nativeTreasuryRuntimeSource
	policy.Runtime.RuntimeVersion.SpecVersion = 470
	decode := func(value string) [32]byte {
		t.Helper()
		raw, err := rootReceiptHex(value, 32)
		if err != nil {
			t.Fatal(err)
		}
		var result [32]byte
		copy(result[:], raw)
		return result
	}
	approval := validator.TreasuryApproval{Schema: validator.TreasuryApprovalSchema, ConfigHash: nativeTreasuryTestAccount(0x81), Proposal: validator.TreasuryProposal{Schema: validator.TreasuryProposalSchema, ParentPolicyHash: nativeTreasuryTestAccount(0x82), PolicyId: 2, EffectiveEpoch: 100, ProviderShare: public.ProviderShare, Remainder: "ordinary_treasury_credit", OwnerAllocation: "equal_exact_registered", Runtime: validator.OwnerRecycleRuntimePin{GenesisHash: decode(policy.Network.GenesisHash), Netuid: policy.Netuid, Version: policy.Runtime.RuntimeVersion, CodeHash: decode(policy.Runtime.RuntimeCodeHash), MetadataHash: decode(policy.Runtime.RuntimeMetadataHash), SourceCommit: policy.Runtime.RuntimeSourceCommit}, Treasury: &public}, NativeChain: policy.Network.NativeChain, RuntimeReviewHash: nativeTreasuryTestAccount(0x83), ValidatorHotkey: nativeTreasuryTestAccount(0x80), SubnetOwner: nativeTreasuryTestAccount(0x70), OwnerHotkeys: [][32]byte{nativeTreasuryTestAccount(0x71)}, FirstNativeEpoch: 8, ValidFromNativeBlock: policy.From.Number, ValidThroughNativeBlock: policy.Through.Number, MaximumSubnetUids: uint32(policy.MaximumUids), MaximumOwnedHotkeys: uint32(rootCensusLimit), Production: &validator.TreasuryProductionApproval{Schema: validator.TreasuryProductionScope, RuntimeCapability: "native-treasury-fixture", ValidatorHotkeys: [][32]byte{nativeTreasuryTestAccount(0x80)}, MaximumLastUpdateAge: 100, ValidThroughNativeEpoch: 20, ActivationNativeHash: decode(policy.From.Hash), ActivationNativeBlock: policy.From.Number}}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x39}, ed25519.SeedSize))
	message, err := approval.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope := validator.TreasuryApprovalEnvelope{Approval: approval, Signature: hex.EncodeToString(ed25519.Sign(key, message))}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	hash, err := public.Hash()
	if err != nil {
		t.Fatal(err)
	}
	result := &nativeTreasuryAuthority{Schema: nativeTreasuryAuthoritySchema, Policy: public, PolicyHash: hash, ApprovalHash: fmt.Sprintf("0x%x", sha256.Sum256(raw)), Approval: raw, Deployment: nativeTreasuryDeployment{ConfigHash: approval.ConfigHash, ParentPolicyHash: approval.Proposal.ParentPolicyHash, PolicyId: approval.Proposal.PolicyId, EffectiveEpoch: approval.Proposal.EffectiveEpoch, Activation: policy.From}}
	copy(result.ApprovalSigner[:], key.Public().(ed25519.PublicKey))
	if err := result.validate(); err != nil {
		t.Fatal("complete synthetic economic authority refused", err)
	}
	return result, policy
}

// Re-sign only synthetic fixture authority after the actual exported program,
// metadata and header identities have been selected by the public producer.
func nativeTreasuryTestBindPolicy(t *testing.T, authority *nativeTreasuryAuthority, policy economicEmissionPolicy) {
	t.Helper()
	var envelope validator.TreasuryApprovalEnvelope
	if err := json.Unmarshal(authority.Approval, &envelope); err != nil {
		t.Fatal(err)
	}
	decode := func(value string) [32]byte {
		t.Helper()
		raw, err := rootReceiptHex(value, 32)
		if err != nil {
			t.Fatal(err)
		}
		var result [32]byte
		copy(result[:], raw)
		return result
	}
	approval := &envelope.Approval
	approval.NativeChain = policy.Network.NativeChain
	approval.Proposal.Runtime = validator.OwnerRecycleRuntimePin{GenesisHash: decode(policy.Network.GenesisHash), Netuid: policy.Netuid, Version: policy.Runtime.RuntimeVersion, CodeHash: decode(policy.Runtime.RuntimeCodeHash), MetadataHash: decode(policy.Runtime.RuntimeMetadataHash), SourceCommit: policy.Runtime.RuntimeSourceCommit}
	approval.ValidFromNativeBlock, approval.ValidThroughNativeBlock = policy.From.Number, policy.Through.Number
	approval.MaximumSubnetUids = uint32(policy.MaximumUids)
	approval.Production.ActivationNativeBlock = policy.From.Number
	approval.Production.ActivationNativeHash = decode(policy.From.Hash)
	authority.Deployment.Activation = policy.From
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x39}, ed25519.SeedSize))
	message, err := approval.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	authority.Approval = append(raw, '\n')
	authority.ApprovalHash = fmt.Sprintf("0x%x", sha256.Sum256(authority.Approval))
	if err := authority.validateScope(policy, policy.Through, nil); err != nil {
		t.Fatal(err)
	}
}

func nativeTreasuryTestPrincipal(authority *nativeTreasuryAuthority, parent economicEmissionBoundary) *nativePrincipalPolicy {
	result := &nativePrincipalPolicy{Schema: nativePrincipalAvailabilitySchema, Api: historicalPrincipalApi, LayoutSha256: monitorReadDigest([]byte(historicalPrincipalLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic reviewed principal API")), Parent: parent, Availability: &nativeAvailabilityPolicy{Schema: historicalAvailabilitySchema, Api: historicalAvailabilityApi, LayoutSha256: monitorReadDigest([]byte(historicalAvailabilityLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic reviewed availability API"))}, Effects: &nativePrincipalEffectsPolicy{Schema: nativePrincipalEffectsSchema, ReviewSha256: monitorReadDigest([]byte("synthetic complete stake causes")), StoragePrefixes: []string{"0x01"}}}
	for _, recipient := range authority.Policy.Recipients {
		result.Queries = append(result.Queries, historicalPrincipalQuery{Hotkey: historicalReplayDigest(recipient.Hotkey), Coldkey: historicalReplayDigest(authority.Policy.MultisigAccount), Netuid: 25, Availability: true})
	}
	return result
}

func TestNativeTreasurySignedAuthorityRejectsForeignDeploymentAndRuntime(t *testing.T) {
	authority, policy := nativeTreasuryTestAuthority(t)
	epoch := uint64(8)
	if err := authority.validateScope(policy, policy.Through, &epoch); err != nil {
		t.Fatal("positive exact scope", err)
	}
	for _, mutate := range []func(*economicEmissionPolicy){
		func(value *economicEmissionPolicy) { value.Network.GenesisHash = "0x" + strings.Repeat("ab", 32) },
		func(value *economicEmissionPolicy) { value.Network.NativeChain += "-other" },
		func(value *economicEmissionPolicy) { value.Netuid++ },
		func(value *economicEmissionPolicy) { value.Runtime.RuntimeCodeHash = "0x" + strings.Repeat("ab", 32) },
		func(value *economicEmissionPolicy) {
			value.Runtime.RuntimeMetadataHash = "0x" + strings.Repeat("ab", 32)
		},
		func(value *economicEmissionPolicy) { value.Runtime.RuntimeSourceCommit = frontierMappingSourceCommit },
		func(value *economicEmissionPolicy) { value.Runtime.RuntimeVersion.SpecVersion++ },
		func(value *economicEmissionPolicy) { value.From.Hash = "0x" + strings.Repeat("ab", 32) },
	} {
		changed := policy
		mutate(&changed)
		if err := authority.validateScope(changed, policy.Through, &epoch); err == nil {
			t.Fatal("foreign runtime or deployment acquired treasury authority")
		}
	}
	for _, epoch := range []uint64{7, 21} {
		if err := authority.validateScope(policy, policy.Through, &epoch); err == nil {
			t.Fatal("expired or premature native epoch acquired authority")
		}
	}
	outside := policy.Through
	outside.Number++
	if err := authority.validateScope(policy, outside, nil); err == nil {
		t.Fatal("quiet block escaped signed native block window")
	}
	for _, mutate := range []func(*nativeTreasuryAuthority){
		func(value *nativeTreasuryAuthority) { value.Deployment.ConfigHash[0] ^= 1 },
		func(value *nativeTreasuryAuthority) { value.Deployment.ParentPolicyHash[0] ^= 1 },
		func(value *nativeTreasuryAuthority) { value.Deployment.PolicyId++ },
		func(value *nativeTreasuryAuthority) { value.Deployment.EffectiveEpoch++ },
		func(value *nativeTreasuryAuthority) { value.ApprovalSigner[0] ^= 1 },
		func(value *nativeTreasuryAuthority) { value.PolicyHash[0] ^= 1 },
		func(value *nativeTreasuryAuthority) {
			value.Approval = append(append([]byte{}, value.Approval...), ' ')
		},
	} {
		changed := *authority
		mutate(&changed)
		if err := changed.validate(); err == nil {
			t.Fatal("changed signed deployment or original approval accepted")
		}
	}
}

// Credit facts are synthetic original-memory records, not a native engine claim.
func nativeTreasuryTestCredit(t *testing.T, authority *nativeTreasuryAuthority, index int) (historicalReplayObservation, nativeExecutionEffect) {
	t.Helper()
	recipient := authority.Policy.Recipients[index]
	gross, captured := uint64(9), uint64(3)
	if index == 1 {
		gross, captured = 89, 0
	}
	_, record := nativeExecutionTestRecord("native-miner-credit", uint32(index+30), []nativeExecutionTestField{
		{name: "netuid", raw: []byte{25, 0}}, {name: "hotkey", raw: recipient.Hotkey[:]}, {name: "coldkey", raw: authority.Policy.MultisigAccount[:]}, {name: "gross", raw: nativeExecutionTestWords(gross)}, {name: "captured", raw: nativeExecutionTestWords(captured)}, {name: "liquid", raw: nativeExecutionTestWords(gross - captured)},
		{name: "subnet-owner", raw: bytes.Repeat([]byte{0x70}, 32)}, {name: "subnet-owner-hotkey", raw: append([]byte{1}, bytes.Repeat([]byte{0x71}, 32)...)}, {name: "owner-hotkeys", raw: bytes.Repeat([]byte{0x71}, 32)}, {name: "auto-stake-destination", raw: []byte{0}}, {name: "stake-destination", raw: recipient.Hotkey[:]},
	})
	record.Operation, record.Ordinal = "set", uint64(index+5)
	effect := nativeExecutionEffect{Ordinal: record.Ordinal, Recipient: nativeExecutionRecipient{Uid: recipient.Uid, Hotkey: fmt.Sprintf("0x%x", recipient.Hotkey), Registered: recipient.RegistrationBlock, Coldkey: fmt.Sprintf("0x%x", authority.Policy.MultisigAccount)}, Branch: record.Purpose, Gross: fmt.Sprint(gross), Liquid: fmt.Sprint(gross - captured), Collateral: fmt.Sprint(captured), Recycled: "0"}
	return record, effect
}

func TestNativeTreasuryOriginalCreditRefusesOwnerAndDestinationSubstitution(t *testing.T) {
	authority, _ := nativeTreasuryTestAuthority(t)
	record, effect := nativeTreasuryTestCredit(t, authority, 0)
	credit, err := deriveNativeTreasuryRecipient(authority, record, effect)
	if err != nil || credit == nil || credit.StakeDestination == nil || *credit.StakeDestination != effect.Recipient.Hotkey {
		t.Fatal("ordinary multisig credit refused", credit, err)
	}
	for _, mutate := range []func(*historicalReplayObservation, *nativeExecutionEffect){
		func(record *historicalReplayObservation, _ *nativeExecutionEffect) {
			nativeExecutionTestChange(t, record, "subnet-owner", authority.Policy.MultisigAccount[:])
		},
		func(record *historicalReplayObservation, _ *nativeExecutionEffect) {
			nativeExecutionTestChange(t, record, "subnet-owner-hotkey", append([]byte{1}, authority.Policy.Recipients[0].Hotkey[:]...))
		},
		func(record *historicalReplayObservation, _ *nativeExecutionEffect) {
			nativeExecutionTestChange(t, record, "owner-hotkeys", authority.Policy.Recipients[0].Hotkey[:])
		},
		func(record *historicalReplayObservation, _ *nativeExecutionEffect) {
			nativeExecutionTestChange(t, record, "auto-stake-destination", append([]byte{1}, bytes.Repeat([]byte{0x44}, 32)...))
		},
		func(record *historicalReplayObservation, _ *nativeExecutionEffect) {
			nativeExecutionTestChange(t, record, "stake-destination", bytes.Repeat([]byte{0x44}, 32))
		},
		func(record *historicalReplayObservation, _ *nativeExecutionEffect) {
			record.Native.Memory = record.Native.Memory[:6]
		},
		func(_ *historicalReplayObservation, effect *nativeExecutionEffect) { effect.Provider = true },
		func(_ *historicalReplayObservation, effect *nativeExecutionEffect) {
			effect.Branch = "native-owner-recycle"
		},
		func(_ *historicalReplayObservation, effect *nativeExecutionEffect) {
			effect.Recipient.Coldkey = "0x" + strings.Repeat("44", 32)
		},
	} {
		record, effect := nativeTreasuryTestCredit(t, authority, 0)
		mutate(&record, &effect)
		if _, err := deriveNativeTreasuryRecipient(authority, record, effect); err == nil {
			t.Fatal("unapproved owner/destination/role replaced execution custody")
		}
	}
}

func TestNativeTreasuryWindowCarriesTenthsAndCountsCapturedIncomeOnce(t *testing.T) {
	authority, policy := nativeTreasuryTestAuthority(t)
	window := nativeExecutionEmpty(policy.From)
	window.Treasury = newNativeTreasuryAmounts(authority)
	window.Treasury.Gross, window.Treasury.Liquid, window.Treasury.Collateral = "9", "6", "3"
	window.MinerAllocation, window.CollateralCapture = "9", "3"
	window.Blocks, window.Through = 1, economicEmissionBoundary{Number: policy.From.Number + 1, Hash: "0x" + strings.Repeat("41", 32)}
	if err := window.references(); err != nil {
		t.Fatal(err)
	}
	if err := window.validate(); err != nil {
		t.Fatal(err)
	}
	next := nativeExecutionEmpty(window.Through)
	next.Treasury = newNativeTreasuryAmounts(authority)
	next.MinerAllocation, next.ProviderEntitlement = "1", "1"
	next.Blocks, next.Through = 1, economicEmissionBoundary{Number: window.Through.Number + 1, Hash: "0x" + strings.Repeat("42", 32)}
	if err := next.references(); err != nil {
		t.Fatal(err)
	}
	joined, err := appendNativeExecution(&window, economicEmissionObservation{ExecutionWindow: &next}, policy.From)
	if err != nil || joined.Treasury.Gross != "9" || joined.Treasury.Collateral != "3" || joined.ProviderReference != "1" || joined.TreasuryReference == nil || *joined.TreasuryReference != "9" || joined.OwnerRecycleReference != "0" || window.Treasury.Gross != "9" {
		t.Fatal("treasury carry or collateral conservation differs", joined, err)
	}
	if _, err := appendNativeExecution(joined, economicEmissionObservation{ExecutionWindow: &next}, policy.From); err == nil {
		t.Fatal("treasury income counted twice")
	}
	changed := *joined
	changed.Treasury = cloneNativeTreasuryAmounts(joined.Treasury)
	changed.Treasury.ApprovalHash = "0x" + strings.Repeat("ff", 32)
	changed.ContentHash = changed.hash()
	if err := nativeExecutionRetains(&changed, &window); err == nil {
		t.Fatal("archive rebound treasury approval")
	}
	input := economicReferenceInput{Schema: economicTreasuryReferenceInputSchema, TreasuryPolicy: &authority.Policy, NativeMinerAllocations: []string{"9", "1"}}
	reference, err := calculateEconomicReference(input, "synthetic-input")
	if err != nil || *reference.Intervals[0].TreasuryAlpha != "9" || *reference.Intervals[1].TreasuryAlpha != "0" || *reference.Intervals[1].TreasuryTotalAlpha != "9" || reference.ReserveCreditAlpha != "0" || reference.ActualNativeOutcomeVerified {
		t.Fatal("reference manufactured credit or lost cumulative rounding", reference, err)
	}
}

// Original M survives zero-incentive redirection and a signed allocation
// difference. Neither branch changes the independently governed denominator.
func TestNativeTreasuryTrancheRetainsRedirectedAndSignedAllocationDifference(t *testing.T) {
	authority, policy := nativeTreasuryTestAuthority(t)
	for _, mode := range []string{"redirected", "negative-allocation-difference"} {
		window := nativeExecutionEmpty(policy.From)
		window.Treasury = newNativeTreasuryAmounts(authority)
		window.Blocks, window.Through = policy.Through.Number-policy.From.Number, policy.Through
		window.MinerAllocation, window.AllocationDifference, window.RedirectedToValidators = "100", "100", "100"
		if mode == "negative-allocation-difference" {
			window.MinerAllocation, window.AllocationDifference, window.RedirectedToValidators = "97", "-1", "0"
			window.Treasury.Gross, window.Treasury.Liquid = "98", "98"
		}
		if err := window.references(); err != nil {
			t.Fatal(mode, err)
		}
		if err := window.validate(); err != nil {
			t.Fatal("original signed difference lost conserved mint", mode, err)
		}
		within, _, _, _, err := economicTreasurySplit(window, "0")
		if err != nil || within || mode == "redirected" && (window.ProviderReference != "10" || window.TreasuryReference == nil || *window.TreasuryReference != "90") {
			t.Fatal("redirected or excess allocation acquired policy conformity", mode, window, within, err)
		}
	}
}

func TestNativeTreasuryPrincipalRequiresIndependentAvailabilityAuthority(t *testing.T) {
	authority, policy := nativeTreasuryTestAuthority(t)
	principal := nativeTreasuryTestPrincipal(authority, policy.From)
	if err := principal.validate(); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeTreasuryPrincipal(authority, principal); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*nativePrincipalPolicy){
		func(value *nativePrincipalPolicy) { value.Schema = historicalPrincipalSchema },
		func(value *nativePrincipalPolicy) { value.Availability = nil },
		func(value *nativePrincipalPolicy) { value.Effects = nil },
		func(value *nativePrincipalPolicy) { value.Queries = value.Queries[:1] },
	} {
		changed := *principal
		mutate(&changed)
		if changed.validate() == nil && validateNativeTreasuryPrincipal(authority, &changed) == nil {
			t.Fatal("treasury acquired unreviewed or incomplete availability")
		}
	}
}

func TestNativeTreasuryUnselectedPreservesLegacyPolicyAndSignatureDomain(t *testing.T) {
	policy := nativeExecutionPolicy{Schema: nativeExecutionPolicySchema}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	const original = `{"schema":"urnetwork-native-miner-execution-policy-v1","approval_ed25519_public_key":"","runtime_semantics_review_sha256":"","original_callsite_profile_sha256":"","engine":{"path":"","sha256":""},"admission_directory":""}`
	if string(raw) != original {
		t.Fatal("unselected treasury changed historical policy bytes", string(raw))
	}
	admission := nativeExecutionAdmission{Schema: nativeExecutionAdmissionSchema}
	message, err := admission.signingBytes()
	if err != nil || !bytes.HasPrefix(message, []byte(nativeExecutionAdmissionSchema+"\x00")) || bytes.Contains(message, []byte("treasury")) {
		t.Fatal("legacy admission signature domain changed", err)
	}
	authority, _ := nativeTreasuryTestAuthority(t)
	admission.Treasury, admission.Schema = authority, nativeTreasuryExecutionAdmissionSchema
	message, err = admission.signingBytes()
	if err != nil || !bytes.HasPrefix(message, []byte(nativeTreasuryExecutionAdmissionSchema+"\x00")) {
		t.Fatal("treasury signature borrowed the owner domain", err)
	}
}

func TestNativeTreasuryRecipientProjectionKeepsOrdinaryIncomeOutOfResidual(t *testing.T) {
	authority, policy := nativeTreasuryTestAuthority(t)
	outcome := nativeExecutionOutcome{Treasury: newNativeTreasuryAmounts(authority), Boundary: economicEmissionBoundary{Number: policy.From.Number + 1, Hash: "0x" + strings.Repeat("45", 32)}, AdmissionHash: monitorReadDigest([]byte("admission")), JobHash: monitorReadDigest([]byte("job")), TraceHash: monitorReadDigest([]byte("trace")), MinerAllocation: "100", ProviderEntitlement: "0", OwnerRecycled: "0", ResidualEntitlement: "0", CollateralCapture: "3", FixedPointDust: "2", FixedPointTolerance: "2", AllocationDifference: "2", RedirectedToValidators: "0", AmountsAuthenticated: true}
	outcome.Treasury.Gross, outcome.Treasury.Liquid, outcome.Treasury.Collateral = "98", "95", "3"
	var effects []nativeExecutionEffect
	identities := map[string]nativeExecutionRecipient{}
	for index := range authority.Policy.Recipients {
		record, effect := nativeTreasuryTestCredit(t, authority, index)
		var err error
		effect.Treasury, err = deriveNativeTreasuryRecipient(authority, record, effect)
		if err != nil {
			t.Fatal(err)
		}
		effects = append(effects, effect)
		outcome.Recipients = append(outcome.Recipients, effect.Recipient)
		identities[effect.Recipient.Hotkey] = effect.Recipient
	}
	if err := validateNativeTreasuryRoster(authority, identities, nil); err != nil {
		t.Fatal(err)
	}
	outcome.ContentHash = outcome.hash()
	event, ordinal := uint64(0), uint64(4)
	projection := newNativeExecutionEffects(policy.From, outcome, &event, &ordinal, effects)
	if err := projection.validate(outcome); err != nil {
		t.Fatal("complete synthetic treasury effects refused", err)
	}
	projection.Effects[0].Treasury = nil
	projection.ContentHash = projection.hash()
	if err := projection.validate(outcome); err == nil {
		t.Fatal("treasury credit became residual after replay")
	}
	missing := authority.Policy.Recipients[1]
	delete(identities, fmt.Sprintf("0x%x", missing.Hotkey))
	if err := validateNativeTreasuryRoster(authority, identities, nil); err == nil {
		t.Fatal("missing treasury recipient shrank signed roster")
	}
}

// This root requires the separate Rust treasury exporter and current protected
// engines. Its original Wasm is synthetic; no deployed v470 claim is inferred.
func TestNativeTreasuryPublicProducerRetainsOriginalCreditAndCustody(t *testing.T) {
	authority, _ := nativeTreasuryTestAuthority(t)
	fixture := nativeProducerPublicFixtureWithTreasury(t, false, nil, false, authority)
	observation, code, issue := fixture.command(t)
	if code != 0 || !observation.Complete || observation.ExecutionProducer == nil || observation.ExecutionProducer.Completed != 1 || observation.ExecutionProducer.Cursor != fixture.source.policy.Through || observation.ExecutionWindow == nil || len(observation.Blocks) != 1 || observation.Blocks[0].ExecutionOutcome == nil {
		t.Fatal("actual treasury producer did not publish the owned original boundary", code, issue)
	}
	outcome := observation.Blocks[0].ExecutionOutcome
	if outcome.Treasury == nil || outcome.Treasury.Gross != "98" || outcome.Treasury.Liquid != "95" || outcome.Treasury.Collateral != "3" || outcome.MinerAllocation != "100" || outcome.ProviderEntitlement != "0" || outcome.OwnerRecycled != "0" || outcome.ResidualEntitlement != "0" || outcome.CollateralCapture != "3" || outcome.AllocationDifference != "2" || outcome.PrincipalEffects == nil || outcome.OpeningPrincipals == nil || outcome.CertifiedWindow == nil || fixture.proofs.Load() == 0 || observation.ActualNativeOutcomeVerified || observation.ActivationReady || observation.RuntimeSourceProven {
		t.Fatal("ordinary treasury mint was relabeled or became deployment authority", outcome)
	}
	if err := outcome.RecipientEffects.validate(*outcome); err != nil {
		t.Fatal(err)
	}
	within, providerDeviation, treasuryDeviation, _, err := economicTreasurySplit(*observation.ExecutionWindow, "2")
	if err != nil || within || providerDeviation != "-100" || treasuryDeviation != "80" {
		t.Fatal("authenticated synthetic custody concealed the wrong ten/ninety allocation", within, providerDeviation, treasuryDeviation, err)
	}
	reconciliation, err := outcome.PrincipalEffects.reconcile(*outcome)
	if err != nil || reconciliation.Status != "original-stake-cause-census-reconciled" || len(reconciliation.Pools) != 2 {
		t.Fatal("actual treasury original causes did not reconcile", reconciliation, err)
	}
	for index, expected := range []struct{ before, after, liquid, captured string }{
		{before: "14", after: "23", liquid: "6", captured: "3"},
		{before: "17", after: "106", liquid: "89", captured: "0"},
	} {
		pool := reconciliation.Pools[index]
		if !pool.Complete || pool.Before == nil || *pool.Before != expected.before || pool.After == nil || *pool.After != expected.after || pool.NativeEarnings != expected.liquid || pool.CapturedEarnings == nil || *pool.CapturedEarnings != expected.captured || pool.Residual == nil || *pool.Residual != "0" {
			t.Fatal("actual treasury gross, liquid or captured stake differs", index, pool)
		}
	}
	policy := economicConservationPolicy{Native: monitorEconomicNativePolicy{Observation: fixture.source.policy}, MaximumFacts: 65536}
	state := economicConservationState{Native: monitorEconomicNativeState{Cursor: outcome.Boundary, ExecutionAccounting: observation.ExecutionWindow, ExecutionProducer: observation.ExecutionProducer}}
	if err := state.appendPrincipalEffects(policy, *outcome, &state.Native); err != nil {
		t.Fatal(err)
	}
	summary, err := state.treasurySummary(policy)
	if err != nil || summary == nil || !summary.CauseCensusComplete || !summary.StockCovered || !summary.AvailabilityKnown || summary.PositionStockAlpha == nil || *summary.PositionStockAlpha != "129" || summary.Availability == nil || summary.Availability.TotalAlpha == nil || *summary.Availability.TotalAlpha != "129" || *summary.Availability.LockedAlpha != "2" || *summary.Availability.AvailableAlpha != "124" || summary.SpendableIncomeLowerAlpha == nil || *summary.SpendableIncomeLowerAlpha != "93" || *summary.SpendableIncomeUpperAlpha != "98" || summary.IncomeUnlockedOrSpent != nil {
		t.Fatal("actual shared-coldkey availability became duplicated or unearned income", summary, err)
	}
	archive, err := mergeEconomicPrincipalArchive(nil, state.PrincipalExecutions[0])
	if err != nil {
		t.Fatal(err)
	}
	state.Archive = &economicConservationArchive{PrincipalEffects: archive}
	state.PrincipalExecutions = nil
	archived, err := state.treasurySummary(policy)
	if err != nil || !reflect.DeepEqual(summary, archived) {
		t.Fatal("treasury archive changed original custody", archived, err)
	}
	root := fixture.source.policy.Execution.Directory
	before := mainnetNamespaceTest(t, root)
	proofs := fixture.proofs.Load()
	restored, code, issue := fixture.command(t)
	if code != 0 || !restored.Complete || len(restored.Blocks) != 1 || !reflect.DeepEqual(outcome, restored.Blocks[0].ExecutionOutcome) || fixture.proofs.Load() != proofs || !reflect.DeepEqual(before, mainnetNamespaceTest(t, root)) {
		t.Fatal("treasury restart recaptured or reinterpreted original proof custody", code, issue)
	}
	completionRaw, err := os.ReadFile(observation.ExecutionProducer.Completion.Path)
	if err != nil {
		t.Fatal(err)
	}
	var completion nativeProducerCompletion
	if err := decodePlanJson(completionRaw, &completion); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(completion.Admission.Job.Path, root+string(filepath.Separator)) {
		t.Fatal("producer job escaped its owned originals")
	}
	if err := os.Rename(completion.Admission.Job.Path, completion.Admission.Job.Path+".held"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(completion.Admission.Job.Path+".held", completion.Admission.Job.Path); err != nil {
			t.Error(err)
		}
	})
	refused, code, issue := fixture.command(t)
	if code == 0 || refused.Complete || refused.ExecutionProducer != nil || fixture.proofs.Load() != proofs || !strings.Contains(issue, "no such file") {
		t.Fatal("missing treasury original acquired replacement income or cursor", code, issue)
	}
}

// The negative export retains the original program and headers while removing
// treasury dependencies from the parent trie. Missing state cannot become zero.
func TestNativeTreasuryReplayMissingOriginalCannotEstablishCustody(t *testing.T) {
	enginePath, creditPath := os.Getenv("URNETWORK_NATIVE_EXECUTION_ENGINE"), os.Getenv("URNETWORK_NATIVE_TREASURY_FIXTURE")
	if enginePath == "" || creditPath == "" {
		t.Fatal("treasury replay requires explicit original export and replay engine")
	}
	_, engineHash, err := readPlanFile(t.Context(), enginePath, historicalReplayEngineLimit)
	if err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(filepath.Dir(creditPath), "treasury-missing-original.json")
	_, jobHash, err := readPlanFile(t.Context(), missingPath, historicalNativeJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	report, err := runHistoricalReplay(t.Context(), historicalReplayRequest{Engine: planFileReference{Path: enginePath, Sha256: engineHash}, Job: planFileReference{Path: missingPath, Sha256: jobHash}, Budget: time.Minute}, historicalReplayHooks{})
	if err == nil || report != nil || !strings.Contains(err.Error(), "principal") {
		t.Fatal("missing original treasury custody became a successful replay", report, err)
	}
}
