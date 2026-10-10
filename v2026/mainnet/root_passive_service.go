// Passive root service authority approves bounded observation of one existing
// seat. It carries no native action, signing device, nonce or spending budget.
package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"reflect"
)

const rootPassiveServiceSchema = "urnetwork-mainnet-root-passive-service-v1"
const rootPassiveStrategy = "passive_accumulate_in_place"
const bootstrapRootPassiveConfigSchema = "urnetwork-mainnet-bootstrap-root-config-v2"
const bootstrapRootPassivePlanSchema = "urnetwork-mainnet-bootstrap-root-passive-plan-v2"
const bootstrapRootPassivePhase = "prepare-passive-root-observation"
const rootPassiveCheckpointDirectory = "root-passive-observation"

// Existing approvals retain their flat checkpoint. Fresh static hosting uses
// one dedicated child so the process cannot write any preparation journal.
func rootPassiveCheckpointWithin(path, directory string) bool {
	return filepath.Dir(path) == directory || filepath.Dir(path) == filepath.Join(directory, rootPassiveCheckpointDirectory)
}

// The signed configuration pins its own route, checkpoint and finite cadence.
// Repeated runs spend no native allowance; checkpoint locking joins each owner.
type rootPassiveServiceConfig struct {
	Schema            string              `json:"schema"`
	Policy            rootValidatorPolicy `json:"policy"`
	RpcUrl            string              `json:"rpc_url"`
	ReadRetrySeconds  uint32              `json:"read_retry_seconds"`
	CheckpointPath    string              `json:"checkpoint_path"`
	MaximumSamples    uint32              `json:"maximum_samples"`
	IntervalSeconds   uint32              `json:"interval_seconds"`
	StallAfterSeconds uint32              `json:"stall_after_seconds"`
}

// Copy pointer-bearing approved policy before handing it to another owner.
func copyRootPassivePolicy(policy rootValidatorPolicy) rootValidatorPolicy {
	if policy.ExpectedSeat != nil {
		seat := *policy.ExpectedSeat
		policy.ExpectedSeat = &seat
	}
	if policy.ExpectedDelegateTake != nil {
		take := *policy.ExpectedDelegateTake
		policy.ExpectedDelegateTake = &take
	}
	return policy
}

// Legacy policy, credentials in a route and unbounded service inputs fail before
// any checkpoint or connection opens. Runtime hashes remain independent pins.
func (self rootPassiveServiceConfig) validate() error {
	if self.Schema != rootPassiveServiceSchema || self.Policy.Schema != rootPassivePolicySchema ||
		self.ReadRetrySeconds < 60 || self.ReadRetrySeconds > 900 || self.MaximumSamples == 0 || self.MaximumSamples > 10000 ||
		self.IntervalSeconds == 0 || self.IntervalSeconds > 3600 || self.StallAfterSeconds < 120 || self.StallAfterSeconds > 3600 ||
		!bootstrapRootAbsolutePath(self.CheckpointPath) {
		return errors.New("passive root service requires its exact policy, checkpoint and bounded read configuration")
	}
	if err := self.Policy.validate(); err != nil {
		return err
	}
	route, err := url.Parse(self.RpcUrl)
	if err != nil || route.Scheme != "http" && route.Scheme != "https" || route.Host == "" || route.User != nil || route.Fragment != "" {
		return errors.New("passive root service route is invalid")
	}
	return nil
}

// Preparation owns only its local progress marker. The later read-only service
// owns a separate checkpoint; neither can reopen a legacy custody journal.
func (self bootstrapRootPlan) validatePassive() error {
	if self.Phase != bootstrapRootPassivePhase || self.PassiveService == nil || !reflect.DeepEqual(self.Service, rootServiceConfig{}) ||
		!planLabel(self.DeploymentId) || self.NetworkEffects || self.NativeSigning || !planSha256(self.ConfigSha256) ||
		!planSha256(self.ServiceInput.Sha256) || !bootstrapRootAbsolutePath(self.RunDirectory) || self.ContentHash != bootstrapRootPlanHash(self) {
		return errors.New("passive root plan carries invalid scope or legacy action authority")
	}
	if err := self.PassiveService.validate(); err != nil {
		return err
	}
	p := self.PassiveService.Policy
	if self.Network != (planNetwork{NativeChain: p.NativeChain, GenesisHash: p.GenesisHash, EvmChainId: p.EvmChainId}) {
		return errors.New("passive root plan and service network differ")
	}
	seen := map[string]bool{}
	for _, path := range []string{filepath.Join(self.RunDirectory, bootstrapRootProgressFile), self.PassiveService.CheckpointPath} {
		if !rootPassiveCheckpointWithin(path, self.RunDirectory) || seen[path] || seen[path+".lock"] {
			return errors.New("passive root checkpoint overlaps preparation or leaves its private directory")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	for _, path := range []string{self.ConfigPath, self.ServiceInput.Path} {
		if !bootstrapRootAbsolutePath(path) || seen[path] || seen[path+".lock"] {
			return errors.New("passive root input overlaps another input or retained state")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	return nil
}

// This projection compares only identity. Its zero action/nonce fields grant
// no native-action authority and cannot pass rootActionScope validation.
func (self bootstrapRootPlan) identityScope() rootActionScope {
	if self.PassiveService == nil {
		return self.Service.Packet.Action.Scope
	}
	p := self.PassiveService.Policy
	return rootActionScope{Role: p.Role, Netuid: p.Netuid, NativeChain: p.NativeChain, GenesisHash: p.GenesisHash, EvmChainId: p.EvmChainId,
		RuntimeSourceCommit: p.RuntimeSourceCommit, RuntimeVersion: p.RuntimeVersion, RuntimeCodeHash: p.RuntimeCodeHash, RuntimeMetadataHash: p.RuntimeMetadataHash,
		Hotkey: p.Hotkey, Coldkey: p.Coldkey, Seat: *p.ExpectedSeat, Strategy: rootPassiveStrategy}
}

// Only legacy preparation has signing-custody children. Passive checkpoints
// are owned by the observer and are never created during local preparation.
func (self bootstrapRootPlan) custodyChildPaths() []string {
	if self.PassiveService != nil {
		return nil
	}
	return []string{self.Service.CustodyTrust.StatePath, self.Service.Packet.Action.Scope.StatePath}
}

// Approval commits the selected service without changing the legacy hash.
func (self bootstrapRootPlan) serviceHash() string {
	if self.PassiveService != nil {
		return rootObjectHash(self.PassiveService)
	}
	return rootObjectHash(self.Service)
}
