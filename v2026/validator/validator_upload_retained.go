//go:build linux || darwin

package validator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
)

const provisionalRetainedValidatorUploadContexts = 4

// The API owner explicitly grants bounded staging to these already pinned
// local contexts. This creates no VerifiedReleaseActivationV2, native stake
// observation, publication height or current-chain freshness claim. Every
// request still proves VPK consent and its exact session/object/replica/expiry.
func (self *ValidatorUploadAdmission) installProvisionalRetainedContextAuthority(contexts []ReleaseEvidenceV2ActivationContext) error {
	if self == nil || !self.config.ProvisionalRetainedContextAuthority || len(contexts) != provisionalRetainedValidatorUploadContexts {
		return errors.New("provisional retained staging context allowance is incomplete")
	}
	deployment := self.config.Deployment
	owners := make(map[ValidatorUploadOwner]bool, len(contexts))
	for _, value := range contexts {
		record, domain := value.Activation, value.Activation.Domain
		if domain.ChainID != deployment.ChainID || domain.GenesisHash != deployment.GenesisHash || domain.Netuid != deployment.Netuid ||
			domain.Coordinator != deployment.Coordinator || domain.SettlementVault != deployment.SettlementVault || domain.DeploymentIDHash != deployment.DeploymentIDHash ||
			value.Journal != deployment.Journal || value.RuntimeHash != deployment.RuntimeHash || value.ObservedEVMBlock < deployment.DeploymentBlock ||
			sha256.Sum256([]byte(value.InitialCut.Identity.DeploymentID)) != deployment.DeploymentIDHash || uint32(value.ValidatorUID) >= deployment.MaximumSubnetUIDs {
			return errors.New("provisional retained staging context differs from the exact approved deployment")
		}
		owner := ValidatorUploadOwner{Hotkey: record.Hotkey, OperatorNoID: record.NoID}
		if owners[owner] {
			return errors.New("provisional retained staging contexts repeat a stable owner")
		}
		owners[owner] = true
	}
	for _, value := range contexts {
		digest, err := value.Activation.Digest()
		if err != nil {
			return err
		}
		owner := ValidatorUploadOwner{Hotkey: value.Activation.Hotkey, OperatorNoID: value.Activation.NoID}
		slots := &validatorUploadOwnerSlots{fresh: make(chan struct{}, int(self.config.FreshActivePerOwner)), retry: make(chan struct{}, int(self.config.RetryActivePerOwner))}
		entryCtx, cancel := context.WithCancel(self.ctx)
		self.ownerSlots[owner] = slots
		self.entries[digest] = &validatorUploadAdmissionEntry{record: value.Activation, ctx: entryCtx, cancel: cancel, slots: slots}
	}
	self.lastError = nil
	return nil
}

func (self *ValidatorUploadAdmission) logProvisionalRetainedContextAuthority(stage string) {
	fmt.Fprintf(os.Stderr, "validator staging: provisional_retained_context_authority=true; stage=%s; replica=%d; contexts=%d; readiness_scope=local-pinned-contexts; historical_authentication=waived; current_chain_eligibility=unrun; final_acceptance=false\n", stage, self.config.ReplicaNoID, len(self.discoveryDigests))
}
