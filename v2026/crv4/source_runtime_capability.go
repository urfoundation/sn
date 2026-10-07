// Source encoding is a purpose-specific capability of an independently
// authenticated block view. It grants neither artifact approval nor authority
// to relabel, replace or broadcast a retained signed attempt.
package crv4

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Immutable strict proof, issued only after exact caller-policy authentication.
// Copies share their connection's owner but do not depend on cache membership.
type runtimeArtifactProof struct {
	owner       *runtimeMetadataArtifactCache
	api         *gsrpc.SubstrateAPI
	blockHash   types.Hash
	genesisHash types.Hash
	identity    RuntimeArtifactIdentity
	metadata    *types.Metadata
	transport   runtimeTransportObservation
}

// Exported artifact fields cannot transfer a proof to another tuple or owner.
// Strict legacy callers may bind without a proof, but cannot gain a successor
// source capability from that unproved binding.
func (self *runtimeArtifactProof) matches(chain *Chain, artifact AuthenticatedRuntimeArtifact) bool {
	return self.matchesIdentity(chain, artifact) && self.transport.matches(chain)
}

// Identity refusal precedes generation expiry, including a forged artifact
// presented while its old transport also happens to have disconnected.
func (self *runtimeArtifactProof) matchesIdentity(chain *Chain, artifact AuthenticatedRuntimeArtifact) bool {
	return self != nil && chain != nil && self.owner == chain.runtimeMetadataArtifactCache() && self.api == chain.API &&
		self.genesisHash == chain.GenesisHash && self.blockHash == artifact.BlockHash && self.metadata == artifact.Metadata &&
		self.identity == (RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}) &&
		(artifact.GenesisHash == (types.Hash{}) || artifact.GenesisHash == self.genesisHash) && artifact.CompatibilityProfile == ""
}

// Preparation requires its exact authenticated block. Retained-byte validation
// instead consumes the caller's authenticated current or historical view and
// checks the original signing domain separately; its old bytes never change.
func (self *Chain) validateSourceRuntimeCapabilityAt(block types.Hash, mecid *uint8) error {
	if self == nil || self.Meta == nil || self.Meta.Version != 14 || self.Runtime == nil {
		return errors.New("crv4: source runtime view is incomplete")
	}
	if self.runtimeArtifactProof != nil && !self.runtimeArtifactProof.transport.matches(self) {
		proof := self.runtimeArtifactProof
		if proof.owner != self.runtimeMetadataArtifactCache() || proof.api != self.API || proof.genesisHash != self.GenesisHash || proof.metadata != self.Meta ||
			proof.identity.Version.SpecName != self.Runtime.SpecName || proof.identity.Version.SpecVersion != uint32(self.Runtime.SpecVersion) || proof.identity.Version.TransactionVersion != uint32(self.Runtime.TransactionVersion) {
			return errors.New("crv4: source runtime identity differs from its retained proof")
		}
		return &runtimeTransportObservationError{}
	}
	if proof := self.runtimeCompatibilityProof; proof != nil && !proof.transport.matches(self) {
		if !self.ProvisionalRuntimeCompatibilityEnabled() || proof.owner != self.provisionalRuntime || proof.metadata != self.Meta ||
			proof.identity.Version.SpecName != self.Runtime.SpecName || proof.identity.Version.SpecVersion != uint32(self.Runtime.SpecVersion) || proof.identity.Version.TransactionVersion != uint32(self.Runtime.TransactionVersion) {
			return errors.New("crv4: provisional source runtime identity differs from its retained proof")
		}
		return &runtimeTransportObservationError{}
	}
	if reviewedSourceEncodingVersion(uint32(self.Runtime.SpecVersion), uint32(self.Runtime.TransactionVersion)) || self.CurrentRuntimeCompatibilityProfile() != "" {
		return nil
	}
	proof := self.runtimeArtifactProof
	if proof == nil || proof.owner != self.runtimeMetadataArtifactCache() || proof.api != self.API || proof.genesisHash == (types.Hash{}) || proof.genesisHash != self.GenesisHash ||
		proof.metadata != self.Meta || proof.blockHash == (types.Hash{}) || block != (types.Hash{}) && proof.blockHash != block ||
		proof.identity.Version.SpecName != "node-subtensor" || self.Runtime.SpecName != proof.identity.Version.SpecName ||
		uint32(self.Runtime.SpecVersion) != proof.identity.Version.SpecVersion || uint32(self.Runtime.TransactionVersion) != proof.identity.Version.TransactionVersion ||
		proof.identity.Version.SpecVersion == 0 || proof.identity.Version.TransactionVersion != 1 || proof.identity.Version.StateVersion != 1 {
		return errors.New("crv4: source successor lacks an exact authenticated block view")
	}
	baseline, err := runtimeProfileBaseline()
	if err != nil {
		return err
	}
	commit := CallCommitTimelocked
	if mecid != nil {
		commit = CallCommitTimelockedMech
	}
	callNamesKVs := map[string]string{
		"Utility": "batch_all", "Commitments": "set_commitment", PalletName: commit,
	}
	expectedKVs, err := runtimeProfileSelectedShape(baseline, nil, callNamesKVs, nil, true)
	if err != nil {
		return fmt.Errorf("crv4: source baseline capability: %w", err)
	}
	actualKVs, err := runtimeProfileSelectedShape(self.Meta, nil, callNamesKVs, nil, true)
	if err != nil {
		return fmt.Errorf("crv4: source encoding capability: %w", err)
	}
	var names []string
	for name := range expectedKVs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !reflect.DeepEqual(actualKVs[name], expectedKVs[name]) {
			return fmt.Errorf("crv4: source consumed interface %s changed", name)
		}
	}
	return nil
}
