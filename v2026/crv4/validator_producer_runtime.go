// Production validator capability covers its actual storage, source batch,
// receipt events and signing interfaces after independent exact authentication.
package crv4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const ValidatorProducerRuntimeProfile = "urnetwork-validator-producer-interface-v1"

// Admission is deliberately independent of compiled spec-version catalogs and
// of testnet compatibility. The complete artifact remains caller authority.
func ValidateValidatorProducerRuntimeArtifactContext(ctx context.Context, chain *Chain, artifact AuthenticatedRuntimeArtifact) error {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || chain.ProvisionalRuntimeCompatibilityEnabled() ||
		artifact.BlockHash == (types.Hash{}) || artifact.CompatibilityProfile != "" || !artifact.authenticationProof.matches(chain, artifact) {
		return errors.New("validator producer needs an exact authenticated non-provisional block artifact")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if artifact.Version.SpecName != "node-subtensor" || artifact.Version.SpecVersion == 0 || artifact.Version.TransactionVersion != 1 || artifact.Version.StateVersion != 1 {
		return errors.New("validator producer runtime family or encoding version is unsupported")
	}
	baseline, err := runtimeProfileBaseline()
	if err != nil {
		return err
	}
	storageNamesKVs := map[string]string{
		"System":      "Account Events",
		"Timestamp":   "Now",
		"Ethereum":    "BlockHash",
		"Commitments": "CommitmentOf LastCommitment MaxSpace UsedSpaceOf",
		PalletName:    "SubnetworkN Keys Uids Owner TotalHotkeyAlpha ValidatorPermit StakeThreshold SubnetOwner SubnetOwnerHotkey SubnetEpochIndex Tempo LastEpochBlock PendingEpochAt BlocksSinceLastStep RevealPeriodEpochs CommitRevealWeightsEnabled CommitRevealWeightsVersion MaxWeightsLimit WeightsVersionKey Weights LastUpdate MechanismCountCurrent",
	}
	callNamesKVs := map[string]string{
		"Utility": "batch_all", "Commitments": "set_commitment",
		PalletName: CallCommitTimelocked + " " + CallCommitTimelockedMech,
	}
	expectedKVs, err := runtimeProfileSelectedShape(baseline, storageNamesKVs, callNamesKVs, runtimeProfileEvents, true)
	if err != nil {
		return fmt.Errorf("validator producer baseline: %w", err)
	}
	actualKVs, err := runtimeProfileSelectedShape(artifact.Metadata, storageNamesKVs, callNamesKVs, runtimeProfileEvents, true)
	if err != nil {
		return fmt.Errorf("validator producer capability: %w", err)
	}
	var names []string
	for name := range expectedKVs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !reflect.DeepEqual(actualKVs[name], expectedKVs[name]) {
			return fmt.Errorf("validator producer consumed interface %s changed", name)
		}
	}
	var raw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &raw, "state_getRuntimeVersion", artifact.BlockHash.Hex()); err != nil {
		return fmt.Errorf("validator producer runtime API: %w", err)
	}
	version, err := DecodeRuntimeVersionIdentity(raw)
	if err != nil || version != artifact.Version {
		return errors.Join(errors.New("validator producer runtime changed during capability authentication"), err)
	}
	if err := validateValidatorProducerRuntimeApis(raw); err != nil {
		return fmt.Errorf("validator producer selective-metagraph API: %w", err)
	}
	return ctx.Err()
}

// Runtime tuple decoding has already validated the object framing. Its API
// field must also be unambiguous; JSON's last-key-wins rule grants no authority.
func validateValidatorProducerRuntimeApis(raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		return err
	}
	found := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if key == "apis" {
			if found {
				return errors.New("runtime API declaration is duplicated")
			}
			found = true
		}
	}
	return validateProvisionalRuntimeApis(raw)
}

// The caller exclusively owns this signing view. Failed purpose checks leave
// its prior binding intact; successful generic binds revoke this capability.
func (self *Chain) BindValidatorProducerRuntimeArtifactContext(ctx context.Context, artifact AuthenticatedRuntimeArtifact) error {
	if err := ValidateValidatorProducerRuntimeArtifactContext(ctx, self, artifact); err != nil {
		return err
	}
	if err := self.BindRuntimeArtifact(artifact); err != nil {
		return err
	}
	self.validatorProducerProof = artifact.authenticationProof
	return nil
}

// Matching exported version fields alone cannot authorize production signing.
// The immutable capability belongs to this connection, exact artifact and view.
func (self *Chain) ValidateValidatorProducerRuntime(expected RuntimeArtifactIdentity) error {
	if self == nil || self.Meta == nil || self.Runtime == nil || self.ProvisionalRuntimeCompatibilityEnabled() {
		return errors.New("validator producer runtime view is unavailable or provisional")
	}
	proof := self.validatorProducerProof
	if proof == nil || proof != self.runtimeArtifactProof || proof.metadata != self.Meta ||
		proof.identity != expected || proof.identity.Version.SpecName != self.Runtime.SpecName ||
		proof.identity.Version.SpecVersion != uint32(self.Runtime.SpecVersion) || proof.identity.Version.TransactionVersion != uint32(self.Runtime.TransactionVersion) ||
		!proof.matches(self, AuthenticatedRuntimeArtifact{BlockHash: proof.blockHash, Version: proof.identity.Version,
			CodeHash: proof.identity.CodeHash, MetadataHash: proof.identity.MetadataHash, Metadata: self.Meta, GenesisHash: self.GenesisHash}) {
		return errors.New("validator producer runtime lacks this view's exact purpose authentication")
	}
	return nil
}
