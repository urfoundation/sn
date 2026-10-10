// Treasury accounting is a signed successor to owner recycling. Only original
// ordinary miner credits can become treasury income; stake stock and collateral
// remain separate from earned amounts and from independently proven availability.
package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/validator"
)

const nativeTreasuryAuthoritySchema = "urnetwork-native-treasury-execution-authority-v1"
const nativeTreasuryExecutionPolicySchema = "urnetwork-native-miner-execution-policy-v2"
const nativeTreasuryExecutionAdmissionSchema = "urnetwork-native-miner-execution-admission-v2"
const nativeTreasuryProducerAuthoritySchema = "urnetwork-native-execution-producer-authority-v2"
const nativeTreasuryRuntimeSource = "923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d"

// These independently selected economic commitments prevent reusing the same
// recipient roster from an approval for another deployment or activation.
type nativeTreasuryDeployment struct {
	ConfigHash       [32]byte                 `json:"validator_config_hash"`
	ParentPolicyHash [32]byte                 `json:"parent_policy_hash"`
	PolicyId         uint64                   `json:"policy_id"`
	EffectiveEpoch   uint64                   `json:"effective_policy_epoch"`
	Activation       economicEmissionBoundary `json:"native_activation"`
}

// The native approver pins the independently signed economic approval bytes.
// Public policy verification does not grant validator transaction authority.
type nativeTreasuryAuthority struct {
	MigrationComplete *safeCurrentStorageWitness `json:"migration_complete,omitempty"`
	Schema            string                     `json:"schema"`
	Policy            validator.TreasuryPolicy   `json:"public_policy"`
	PolicyHash        [32]byte                   `json:"public_policy_hash"`
	ApprovalSigner    [32]byte                   `json:"economic_approval_signer"`
	ApprovalHash      string                     `json:"economic_approval_hash"`
	Approval          []byte                     `json:"original_signed_economic_approval"`
	Deployment        nativeTreasuryDeployment   `json:"economic_deployment"`
}

// Legacy signatures cannot silently enroll a treasury through an optional field.
func nativeTreasurySchema(treasury *nativeTreasuryAuthority, legacy, successor string) string {
	if treasury != nil {
		return successor
	}
	return legacy
}

func (self *nativeTreasuryAuthority) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != nativeTreasuryAuthoritySchema || len(self.Approval) == 0 || len(self.Approval) > nativeExecutionAdmissionLimit/2 || self.ApprovalSigner == ([32]byte{}) || !rootCanonicalHash(self.ApprovalHash) {
		return errors.New("native treasury requires bounded original signed economic authority")
	}
	envelope, err := validator.VerifyTreasuryApproval(self.Approval, self.ApprovalSigner, self.ApprovalHash)
	if err != nil || envelope == nil || envelope.Approval.Proposal.Treasury == nil || !reflect.DeepEqual(*envelope.Approval.Proposal.Treasury, self.Policy) {
		return errors.Join(errors.New("native treasury differs from independently signed economic policy"), err)
	}
	approval := envelope.Approval
	activation := approval.Production.ActivationNativeBlock
	if activation == 0 {
		activation = approval.ValidFromNativeBlock
	}
	if self.Deployment.ConfigHash == ([32]byte{}) || self.Deployment.ConfigHash != approval.ConfigHash || self.Deployment.ParentPolicyHash == ([32]byte{}) || self.Deployment.ParentPolicyHash != approval.Proposal.ParentPolicyHash || self.Deployment.PolicyId != approval.Proposal.PolicyId || self.Deployment.EffectiveEpoch != approval.Proposal.EffectiveEpoch || self.Deployment.Activation.Number != activation || self.Deployment.Activation.Hash != fmt.Sprintf("0x%x", approval.Production.ActivationNativeHash) || !rootCanonicalHash(self.Deployment.Activation.Hash) || approval.ValidFromNativeBlock > approval.ValidThroughNativeBlock || activation > approval.ValidFromNativeBlock || approval.FirstNativeEpoch > approval.Production.ValidThroughNativeEpoch {
		return errors.New("native treasury original economic deployment or activation window differs")
	}
	if err := self.validateMigration(approval.Proposal.Runtime); err != nil {
		return err
	}
	hash, err := self.Policy.Hash()
	if err != nil || hash != self.PolicyHash {
		return errors.Join(errors.New("native treasury public policy hash differs"), err)
	}
	return nil
}

// Runtime renewal cannot infer a new source capability from its version number.
func nativeExecutionRuntimeSource(treasury *nativeTreasuryAuthority) string {
	if treasury != nil {
		envelope, err := validator.VerifyTreasuryApproval(treasury.Approval, treasury.ApprovalSigner, treasury.ApprovalHash)
		if err != nil || envelope == nil || !crv4.ReviewedNativeOwnerSource(envelope.Approval.Proposal.Runtime.SourceCommit) {
			return ""
		}
		return envelope.Approval.Proposal.Runtime.SourceCommit
	}
	return frontierMappingSourceCommit
}

// Epochs come from original execution memory, never from a later RPC snapshot.
// Quiet blocks still remain inside the independently approved native block span.
func (self *nativeTreasuryAuthority) validateScope(policy economicEmissionPolicy, boundary economicEmissionBoundary, epoch *uint64) error {
	if self == nil {
		return nil
	}
	if err := self.validate(); err != nil {
		return err
	}
	envelope, err := validator.VerifyTreasuryApproval(self.Approval, self.ApprovalSigner, self.ApprovalHash)
	if err != nil {
		return err
	}
	approval, runtime := envelope.Approval, envelope.Approval.Proposal.Runtime
	if approval.NativeChain != policy.Network.NativeChain || fmt.Sprintf("0x%x", runtime.GenesisHash) != policy.Network.GenesisHash || runtime.Netuid != policy.Netuid || !crv4.ReviewedNativeOwnerSource(runtime.SourceCommit) || runtime.SourceCommit != policy.Runtime.RuntimeSourceCommit || runtime.Version != policy.Runtime.RuntimeVersion || fmt.Sprintf("0x%x", runtime.CodeHash) != policy.Runtime.RuntimeCodeHash || fmt.Sprintf("0x%x", runtime.MetadataHash) != policy.Runtime.RuntimeMetadataHash || policy.From.Number < self.Deployment.Activation.Number || policy.From.Number == self.Deployment.Activation.Number && policy.From.Hash != self.Deployment.Activation.Hash || boundary.Number < approval.ValidFromNativeBlock || boundary.Number > approval.ValidThroughNativeBlock || uint32(policy.MaximumUids) > approval.MaximumSubnetUids {
		return errors.New("native treasury approval belongs to another runtime, deployment or native block window")
	}
	if epoch != nil && (*epoch < approval.FirstNativeEpoch || *epoch > approval.Production.ValidThroughNativeEpoch) {
		return errors.New("native treasury execution lies outside its approved native epoch window")
	}
	return nil
}

// A projection retains the approved public role without duplicating a signed
// approval in every cumulative checkpoint. Original admissions retain its bytes.
type nativeTreasuryAmounts struct {
	Policy       validator.TreasuryPolicy `json:"public_policy"`
	PolicyHash   [32]byte                 `json:"public_policy_hash"`
	ApprovalHash string                   `json:"economic_approval_hash"`
	Gross        string                   `json:"gross_earned_alpha"`
	Liquid       string                   `json:"liquid_earned_alpha"`
	Collateral   string                   `json:"captured_earned_alpha"`
}

// Execution-time owner exclusions are retained beside the actual credit. A
// destination is required only when the runtime actually stakes a liquid reward.
type nativeTreasuryRecipientEffect struct {
	SubnetOwner          string   `json:"execution_subnet_owner"`
	SubnetOwnerHotkey    *string  `json:"execution_subnet_owner_hotkey"`
	OwnerHotkeys         []string `json:"execution_registered_owner_hotkeys"`
	AutoStakeDestination *string  `json:"execution_auto_stake_destination"`
	StakeDestination     *string  `json:"execution_stake_destination"`
}

// Public policy slices are copied when a cumulative window is extended.
func cloneNativeTreasuryAmounts(value *nativeTreasuryAmounts) *nativeTreasuryAmounts {
	if value == nil {
		return nil
	}
	result := *value
	if value.Policy.Signatories != nil {
		result.Policy.Signatories = append([][32]byte{}, value.Policy.Signatories...)
	}
	result.Policy.Recipients = append([]validator.TreasuryRecipient{}, value.Policy.Recipients...)
	if value.Policy.AutoStakeDestination != nil {
		destination := *value.Policy.AutoStakeDestination
		result.Policy.AutoStakeDestination = &destination
	}
	return &result
}

func newNativeTreasuryAmounts(authority *nativeTreasuryAuthority) *nativeTreasuryAmounts {
	if authority == nil {
		return nil
	}
	return cloneNativeTreasuryAmounts(&nativeTreasuryAmounts{Policy: authority.Policy, PolicyHash: authority.PolicyHash, ApprovalHash: authority.ApprovalHash, Gross: "0", Liquid: "0", Collateral: "0"})
}

func (self *nativeTreasuryAmounts) validate() error {
	if self == nil {
		return nil
	}
	hash, err := self.Policy.Hash()
	if err != nil || hash != self.PolicyHash || !rootCanonicalHash(self.ApprovalHash) {
		return errors.Join(errors.New("native treasury retained public authority differs"), err)
	}
	for _, amount := range []string{self.Gross, self.Liquid, self.Collateral} {
		zero := "0"
		if err := nativeExecutionAdd(&zero, amount, false); err != nil {
			return err
		}
	}
	total, err := economicConservationSum(self.Liquid, self.Collateral)
	if err != nil || total != self.Gross {
		return errors.Join(errors.New("native treasury gross differs from liquid plus captured rewards"), err)
	}
	return nil
}

func sameNativeTreasuryAuthority(left, right *nativeTreasuryAmounts) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.PolicyHash == right.PolicyHash && left.ApprovalHash == right.ApprovalHash && reflect.DeepEqual(left.Policy, right.Policy)
}

// Accumulation never changes the signed membership or counts locked rewards twice.
func addNativeTreasuryAmounts(target **nativeTreasuryAmounts, value *nativeTreasuryAmounts) error {
	if *target == nil && value != nil {
		*target = cloneNativeTreasuryAmounts(value)
		return value.validate()
	}
	if !sameNativeTreasuryAuthority(*target, value) {
		return errors.New("native treasury cumulative window changed signed authority")
	}
	if value == nil {
		return nil
	}
	if err := value.validate(); err != nil {
		return err
	}
	for _, item := range []struct {
		target *string
		value  string
	}{
		{target: &(*target).Gross, value: value.Gross}, {target: &(*target).Liquid, value: value.Liquid}, {target: &(*target).Collateral, value: value.Collateral},
	} {
		if err := nativeExecutionAdd(item.target, item.value, false); err != nil {
			return err
		}
	}
	return (*target).validate()
}

func nativeTreasuryRecipient(policy validator.TreasuryPolicy, identity nativeExecutionRecipient) bool {
	for _, recipient := range policy.Recipients {
		if recipient.Uid == identity.Uid && fmt.Sprintf("0x%x", recipient.Hotkey) == identity.Hotkey && recipient.RegistrationBlock == identity.Registered {
			return true
		}
	}
	return false
}

// The full execution census must retain every approved registration, even when
// its reward is zero. A missing reserve never causes roster renormalization.
func validateNativeTreasuryRoster(authority *nativeTreasuryAuthority, identities map[string]nativeExecutionRecipient, providers map[string]nativeExecutionRecipient) error {
	if authority == nil {
		return nil
	}
	for _, recipient := range authority.Policy.Recipients {
		hotkey := fmt.Sprintf("0x%x", recipient.Hotkey)
		identity, exists := identities[hotkey]
		_, provider := providers[hotkey]
		if !exists || !nativeTreasuryRecipient(authority.Policy, identity) || provider {
			return errors.New("native treasury registration is absent, replaced or also a provider")
		}
	}
	return nil
}

// Optional AccountId32 values use the actual SCALE option discriminant.
func nativeTreasuryCaptureOption(record historicalReplayObservation, label string) (*string, error) {
	raw, err := nativeCapture(record, label, -1)
	if err != nil {
		return nil, err
	}
	if len(raw) == 1 && raw[0] == 0 {
		return nil, nil
	}
	if len(raw) != 33 || raw[0] != 1 || bytes.Equal(raw[1:], make([]byte, 32)) {
		return nil, errors.New("native treasury option capture is noncanonical: " + label)
	}
	value := "0x" + hex.EncodeToString(raw[1:])
	return &value, nil
}

// These are actual memory observations from the independently reviewed original
// callsite, after any same-drain subnet-owner transition and before crediting.
func deriveNativeTreasuryRecipient(authority *nativeTreasuryAuthority, record historicalReplayObservation, effect nativeExecutionEffect) (*nativeTreasuryRecipientEffect, error) {
	if authority == nil || !nativeTreasuryRecipient(authority.Policy, effect.Recipient) {
		return nil, nil
	}
	if effect.Branch != "native-miner-credit" || effect.Provider || effect.Recipient.Coldkey != fmt.Sprintf("0x%x", authority.Policy.MultisigAccount) {
		return nil, errors.New("native treasury reward is not an approved ordinary credit to its multisig")
	}
	owner, err := nativeCapture(record, "subnet-owner", 32)
	if err != nil {
		return nil, err
	}
	result := &nativeTreasuryRecipientEffect{SubnetOwner: "0x" + hex.EncodeToString(owner), OwnerHotkeys: []string{}}
	result.SubnetOwnerHotkey, err = nativeTreasuryCaptureOption(record, "subnet-owner-hotkey")
	if err != nil {
		return nil, err
	}
	owners, err := nativeCapture(record, "owner-hotkeys", -1)
	if err != nil || len(owners)%32 != 0 || len(owners) > rootCensusLimit*32 {
		return nil, errors.Join(errors.New("native treasury owner census capture is incomplete or exceeds bound"), err)
	}
	for offset := 0; offset < len(owners); offset += 32 {
		result.OwnerHotkeys = append(result.OwnerHotkeys, "0x"+hex.EncodeToString(owners[offset:offset+32]))
	}
	if effect.Liquid != "0" {
		result.AutoStakeDestination, err = nativeTreasuryCaptureOption(record, "auto-stake-destination")
		if err != nil {
			return nil, err
		}
		destination, err := nativeCapture(record, "stake-destination", 32)
		if err != nil {
			return nil, err
		}
		value := "0x" + hex.EncodeToString(destination)
		result.StakeDestination = &value
	}
	return result, result.validate(authority.Policy, effect)
}

func (self *nativeTreasuryRecipientEffect) validate(policy validator.TreasuryPolicy, effect nativeExecutionEffect) error {
	if self == nil || !nativeTreasuryRecipient(policy, effect.Recipient) || effect.Provider || effect.Branch != "native-miner-credit" || effect.Recipient.Coldkey != fmt.Sprintf("0x%x", policy.MultisigAccount) || !rootCanonicalHash(self.SubnetOwner) || self.SubnetOwner == effect.Recipient.Coldkey || len(self.OwnerHotkeys) > rootCensusLimit {
		return errors.New("native treasury effect changed its ordinary registration or custody")
	}
	if self.SubnetOwnerHotkey != nil && (!rootCanonicalHash(*self.SubnetOwnerHotkey) || *self.SubnetOwnerHotkey == effect.Recipient.Hotkey) {
		return errors.New("native treasury is an explicit subnet-owner hotkey")
	}
	seen := map[string]bool{}
	for _, hotkey := range self.OwnerHotkeys {
		if !rootCanonicalHash(hotkey) || seen[hotkey] || hotkey == effect.Recipient.Hotkey {
			return errors.New("native treasury is in the execution-time registered owner census")
		}
		seen[hotkey] = true
	}
	if effect.Liquid == "0" {
		if self.AutoStakeDestination != nil || self.StakeDestination != nil {
			return errors.New("native treasury zero-liquid effect invented a stake transfer")
		}
		return nil
	}
	expected := effect.Recipient.Hotkey
	if policy.AutoStakeDestination == nil {
		if self.AutoStakeDestination != nil {
			return errors.New("native treasury auto-stake setting was not independently approved")
		}
	} else {
		expected = fmt.Sprintf("0x%x", *policy.AutoStakeDestination)
		if self.AutoStakeDestination == nil || *self.AutoStakeDestination != expected {
			return errors.New("native treasury auto-stake setting differs from signed policy")
		}
	}
	if self.StakeDestination == nil || *self.StakeDestination != expected {
		return errors.New("native treasury actual stake destination differs from approved custody")
	}
	return nil
}

// Availability is a coldkey-wide API, while stake causes retain exact hotkey
// positions. Every approved recipient and optional destination remains queried.
func nativeTreasuryPrincipalHotkeys(policy validator.TreasuryPolicy) map[[32]byte]bool {
	result := make(map[[32]byte]bool, len(policy.Recipients)+1)
	for _, recipient := range policy.Recipients {
		result[recipient.Hotkey] = true
	}
	if policy.AutoStakeDestination != nil {
		result[*policy.AutoStakeDestination] = true
	}
	return result
}

func nativeTreasuryPrincipalQuery(authority *nativeTreasuryAuthority, query historicalPrincipalQuery, netuid uint16) bool {
	return authority != nil && query.Netuid == netuid && query.Availability && [32]byte(query.Coldkey) == authority.Policy.MultisigAccount
}

// The successor cannot advertise spendability with the legacy StakeInfo API,
// whose locked field is a constant zero in the admitted runtime source.
func validateNativeTreasuryPrincipal(authority *nativeTreasuryAuthority, principal *nativePrincipalPolicy) error {
	if authority == nil {
		return nil
	}
	if principal == nil || principal.Effects == nil || principal.Availability == nil {
		return errors.New("native treasury requires original stake availability and complete stake causes")
	}
	if authority.MigrationComplete != nil && principal.Parent != authority.Deployment.Activation {
		return errors.New("native treasury principal opening differs from its original migration-complete activation")
	}
	hotkeys := nativeTreasuryPrincipalHotkeys(authority.Policy)
	for _, query := range principal.Queries {
		if [32]byte(query.Coldkey) == authority.Policy.MultisigAccount && query.Availability {
			delete(hotkeys, [32]byte(query.Hotkey))
		}
	}
	if len(hotkeys) != 0 {
		return errors.New("native treasury principal query omitted a recipient or stake destination")
	}
	return nil
}

// Captured stake causes identify accounts, not optional read-only API choices.
// Rebind only to the exact independently approved query with those account bytes.
func nativePrincipalAdmittedQuery(query historicalPrincipalQuery, queries []historicalPrincipalQuery) historicalPrincipalQuery {
	for _, expected := range queries {
		if expected.Hotkey == query.Hotkey && expected.Coldkey == query.Coldkey && expected.Netuid == query.Netuid {
			return expected
		}
	}
	return query
}

// Liquid transfer placement does not relabel the original earning recipient.
func nativeTreasuryLiquidDestination(effect nativeExecutionEffect) string {
	if effect.Treasury != nil && effect.Treasury.StakeDestination != nil {
		return *effect.Treasury.StakeDestination
	}
	return effect.Recipient.Hotkey
}

// Enrollment checks the selected reader contract before starting a producer.
// Actual observations still have to reproduce every field at each execution.
func validateNativeTreasuryProfile(authority nativeProducerAuthority) error {
	if authority.Treasury == nil {
		return nil
	}
	if authority.Profile == nil {
		return errors.New("native treasury has no original callsite reader")
	}
	if err := errors.Join(authority.Profile.validateEpochLayout(), authority.Profile.validateRecipientLayout()); err != nil {
		return err
	}
	if authority.Profile.RecipientLayout != nil {
		return validateNativeStorageTreasuryProfile(authority.Profile)
	}
	epoch, credit := false, false
	counter := false
	for _, rule := range authority.Profile.Rules {
		var required []string
		switch rule.Purpose {
		case "native-epoch":
			epoch = true
			if authority.Profile.EpochLayout == nil {
				required = []string{"subnet-epoch"}
			}
		case "native-epoch-index":
			counter = true
		case "native-miner-credit":
			credit = true
			required = []string{"subnet-owner", "subnet-owner-hotkey", "owner-hotkeys", "auto-stake-destination", "stake-destination"}
		default:
			continue
		}
		for _, name := range required {
			found := false
			for _, capture := range rule.Memory {
				found = found || capture.Name == name
			}
			if !found {
				return errors.New("native treasury reader omitted execution-time custody: " + name)
			}
		}
	}
	if !epoch || !credit || authority.Profile.EpochLayout != nil && !counter {
		return errors.New("native treasury reader omitted original epoch or ordinary credit")
	}
	return nil
}
