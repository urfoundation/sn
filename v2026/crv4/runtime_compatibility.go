package crv4

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

type provisionalRuntimeCompatibility struct {
	genesis   types.Hash
	observe   func(AuthenticatedRuntimeArtifact) error
	mu        sync.Mutex
	artifacts map[RuntimeArtifactIdentity]AuthenticatedRuntimeArtifact
}

// A caller-owned proof retains the exact admitted object and owner. Cache
// eviction cannot revoke it or grant authority to another connection or tuple.
// Published proofs are immutable and safe for concurrent readers.
type runtimeCompatibilityProof struct {
	owner     *provisionalRuntimeCompatibility
	identity  RuntimeArtifactIdentity
	metadata  *types.Metadata
	transport runtimeTransportObservation
}

// Callers opt in only after validating their explicit testnet provisional
// authority. This policy is immutable after connection publication; copies of
// a Chain share its cache and durable observer without changing signing pins.
func (self *Chain) EnableProvisionalRuntimeCompatibility(expectedGenesis types.Hash, observe func(AuthenticatedRuntimeArtifact) error) error {
	if self == nil || expectedGenesis == (types.Hash{}) || self.GenesisHash != expectedGenesis || observe == nil {
		return errors.New("provisional runtime compatibility authority is incomplete")
	}
	runtimeMetadataArtifactCacheInitialization.stateLock.Lock()
	defer runtimeMetadataArtifactCacheInitialization.stateLock.Unlock()
	if self.provisionalRuntime != nil {
		if self.provisionalRuntime.genesis != expectedGenesis {
			return errors.New("provisional runtime genesis changed")
		}
		return nil
	}
	self.provisionalRuntime = &provisionalRuntimeCompatibility{genesis: expectedGenesis, observe: observe, artifacts: map[RuntimeArtifactIdentity]AuthenticatedRuntimeArtifact{}}
	return nil
}

func (self *Chain) ProvisionalRuntimeCompatibilityEnabled() bool {
	if self == nil {
		return false
	}
	runtimeMetadataArtifactCacheInitialization.stateLock.Lock()
	defer runtimeMetadataArtifactCacheInitialization.stateLock.Unlock()
	return self.provisionalRuntime != nil && self.provisionalRuntime.genesis == self.GenesisHash
}

// A marker on a candidate object is insufficient. The unexported immutable
// proof must belong to this owner and match every admitted artifact field.
func (self *Chain) RuntimeArtifactCompatible(artifact AuthenticatedRuntimeArtifact) bool {
	if !self.ProvisionalRuntimeCompatibilityEnabled() || artifact.CompatibilityProfile != ProvisionalRuntimeCompatibilityProfile || artifact.Metadata == nil || artifact.GenesisHash != self.GenesisHash {
		return false
	}
	key := RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}
	proof := artifact.compatibilityProof
	return proof != nil && proof.owner == self.provisionalRuntime && proof.identity == key && proof.metadata == artifact.Metadata && proof.transport.matches(self)
}

// Bind an already authorized artifact while the caller exclusively owns this
// Chain view. Strict callers still establish their own exact-hash authority;
// provisional bindings additionally require the connection-issued proof.
// Failure leaves both the current view and its retained proof unchanged.
func (self *Chain) BindRuntimeArtifact(artifact AuthenticatedRuntimeArtifact) error {
	if self == nil || artifact.BlockHash == (types.Hash{}) || artifact.Metadata == nil {
		return errors.New("runtime binding artifact is incomplete")
	}
	if _, err := canonicalRuntimeArtifactIdentity(RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}); err != nil {
		return err
	}
	if artifact.CompatibilityProfile != "" {
		if !self.RuntimeArtifactCompatible(artifact) {
			proof := artifact.compatibilityProof
			if proof != nil && self.ProvisionalRuntimeCompatibilityEnabled() && artifact.CompatibilityProfile == ProvisionalRuntimeCompatibilityProfile &&
				artifact.GenesisHash == self.GenesisHash && proof.owner == self.provisionalRuntime && proof.metadata == artifact.Metadata &&
				proof.identity == (RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}) && !proof.transport.matches(self) {
				return &runtimeTransportObservationError{}
			}
			return errors.New("runtime binding lacks this connection's authenticated compatibility proof")
		}
	} else if artifact.compatibilityProof != nil {
		return errors.New("provisional runtime proof cannot become strict authority")
	}
	if proof := artifact.authenticationProof; proof != nil {
		if !proof.matchesIdentity(self, artifact) {
			return errors.New("runtime binding differs from its exact-block authentication proof")
		}
		if !proof.transport.matches(self) {
			return &runtimeTransportObservationError{}
		}
	}
	self.Meta = artifact.Metadata
	self.Runtime = &types.RuntimeVersion{
		SpecName: artifact.Version.SpecName, SpecVersion: types.U32(artifact.Version.SpecVersion), TransactionVersion: types.U32(artifact.Version.TransactionVersion),
	}
	self.runtimeCompatibilityProof = artifact.compatibilityProof
	self.runtimeArtifactProof = artifact.authenticationProof
	self.validatorProducerProof = nil
	return nil
}

// Signing uses the real spec version from the checked metadata, and never
// upgrades a retained signature by changing its version or compatibility tag.
func (self *Chain) CurrentRuntimeCompatibilityProfile() string {
	if !self.ProvisionalRuntimeCompatibilityEnabled() || self.Runtime == nil || self.Meta == nil {
		return ""
	}
	proof := self.runtimeCompatibilityProof
	if proof != nil && proof.owner == self.provisionalRuntime && proof.metadata == self.Meta && proof.identity.Version.SpecName == self.Runtime.SpecName && proof.identity.Version.SpecVersion == uint32(self.Runtime.SpecVersion) && proof.identity.Version.TransactionVersion == uint32(self.Runtime.TransactionVersion) && proof.transport.matches(self) {
		return ProvisionalRuntimeCompatibilityProfile
	}
	return ""
}

func authenticateProvisionalRuntimeArtifact(ctx context.Context, chain *Chain, block types.Hash, version RuntimeVersionIdentity, allowed []RuntimeArtifactIdentity) (AuthenticatedRuntimeArtifact, error) {
	empty := AuthenticatedRuntimeArtifact{BlockHash: block}
	transport, err := observeRuntimeTransport(chain)
	if err != nil {
		return empty, err
	}
	// Metadata/profile history can be reused, but each returned capability owns
	// this fresh network observation, including after a reconnect or handoff.
	finish := func(artifact AuthenticatedRuntimeArtifact) (AuthenticatedRuntimeArtifact, error) {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if !transport.matches(chain) {
			return empty, &runtimeTransportObservationError{}
		}
		artifact.compatibilityProof = &runtimeCompatibilityProof{
			owner: chain.provisionalRuntime, identity: RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash},
			metadata: artifact.Metadata, transport: transport,
		}
		return artifact, nil
	}
	if !chain.ProvisionalRuntimeCompatibilityEnabled() || version.SpecName != "node-subtensor" || version.SpecVersion <= ReviewedRuntimeSpecVersion || version.TransactionVersion != 1 || version.StateVersion != 1 {
		return empty, fmt.Errorf("provisional runtime identity %s/%d/%d/%d is outside the consumed profile", version.SpecName, version.SpecVersion, version.TransactionVersion, version.StateVersion)
	}
	// A foreign caller-supplied pin cannot inherit this profile. All authority
	// must originate in the exact reviewed catalog, or a prior authenticated
	// provisional artifact on this same connection.
	for _, identity := range allowed {
		reviewed, ok := ReviewedRuntimeArtifact(identity.Version)
		if ok && identity == reviewed {
			continue
		}
		if proof := chain.runtimeCompatibilityProof; proof != nil && proof.owner == chain.provisionalRuntime && proof.identity == identity {
			continue
		}
		chain.provisionalRuntime.mu.Lock()
		_, ok = chain.provisionalRuntime.artifacts[identity]
		chain.provisionalRuntime.mu.Unlock()
		if !ok {
			return empty, errors.New("provisional runtime caller has an unreviewed authority")
		}
	}
	var genesis types.Hash
	if err := chain.API.Client.CallContext(ctx, &genesis, "chain_getBlockHash", uint64(0)); err != nil {
		return empty, err
	}
	if genesis != chain.GenesisHash {
		return empty, errors.New("provisional runtime genesis differs")
	}
	var raw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &raw, "state_getRuntimeVersion", block.Hex()); err != nil {
		return empty, err
	}
	actualVersion, err := DecodeRuntimeVersionIdentity(raw)
	if err != nil {
		return empty, err
	}
	if actualVersion != version {
		return empty, errors.New("provisional runtime version changed during observation")
	}
	if err := validateProvisionalRuntimeApis(raw); err != nil {
		return empty, err
	}
	code, err := RuntimeCodeHashAtContext(ctx, chain, block)
	if err != nil {
		return empty, err
	}
	code, err = canonicalRuntimeArtifactHash("provisional runtime code hash", code)
	if err != nil {
		return empty, err
	}
	for _, identity := range allowed {
		if identity.Version == version && identity.CodeHash != code {
			return empty, errors.New("explicit provisional runtime code pin changed")
		}
	}
	policy := chain.provisionalRuntime
	// A retained view owns this immutable proof even when the bounded shared
	// cache has evicted it. Fresh block/version/code/API checks above still run.
	if proof := chain.runtimeCompatibilityProof; proof != nil && proof.owner == policy && proof.identity.Version == version && proof.identity.CodeHash == code {
		for _, identity := range allowed {
			if identity.Version == version && identity.MetadataHash != proof.identity.MetadataHash {
				return empty, errors.New("explicit provisional runtime metadata pin changed")
			}
		}
		return finish(AuthenticatedRuntimeArtifact{BlockHash: block, Version: version, CodeHash: code, MetadataHash: proof.identity.MetadataHash, Metadata: proof.metadata, CompatibilityProfile: ProvisionalRuntimeCompatibilityProfile, GenesisHash: genesis})
	}
	policy.mu.Lock()
	var cached AuthenticatedRuntimeArtifact
	for key, artifact := range policy.artifacts {
		if key.Version == version && key.CodeHash == code {
			cached = artifact
			break
		}
	}
	policy.mu.Unlock()
	if cached.Metadata != nil {
		for _, identity := range allowed {
			if identity.Version == version && identity.MetadataHash != cached.MetadataHash {
				return empty, errors.New("explicit provisional runtime metadata pin changed")
			}
		}
		cached.BlockHash = block
		return finish(cached)
	}
	metadata, hash, err := RuntimeMetadataAtContext(ctx, chain, block)
	if err != nil {
		return empty, err
	}
	for _, identity := range allowed {
		if identity.Version == version && identity.MetadataHash != hash {
			return empty, errors.New("explicit provisional runtime metadata pin changed")
		}
	}
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		return empty, err
	}
	artifact := AuthenticatedRuntimeArtifact{BlockHash: block, Version: version, CodeHash: code, MetadataHash: hash, Metadata: metadata, CompatibilityProfile: ProvisionalRuntimeCompatibilityProfile, GenesisHash: genesis}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := policy.observe(artifact); err != nil {
		return empty, fmt.Errorf("persist provisional runtime observation: %w", err)
	}
	artifact, err = finish(artifact)
	if err != nil {
		return empty, err
	}
	key := RuntimeArtifactIdentity{Version: version, CodeHash: code, MetadataHash: hash}
	policy.mu.Lock()
	// Bounded metadata reuse never makes a ninth compatible runtime a launch
	// failure. Eviction affects performance only; exact block checks repeat.
	if len(policy.artifacts) >= 8 {
		for old := range policy.artifacts {
			delete(policy.artifacts, old)
			break
		}
	}
	if prior, exists := policy.artifacts[key]; exists {
		artifact.Metadata = prior.Metadata
		artifact.compatibilityProof.metadata = prior.Metadata
	}
	policy.artifacts[key] = artifact
	policy.mu.Unlock()
	return artifact, ctx.Err()
}

type provisionalRuntimeObservation struct {
	Schema          string                  `json:"schema"`
	Profile         string                  `json:"profile"`
	BlockHash       string                  `json:"first_observed_block_hash"`
	GenesisHash     string                  `json:"genesis_hash"`
	Runtime         RuntimeArtifactIdentity `json:"runtime"`
	Provisional     bool                    `json:"provisional"`
	FinalAcceptance bool                    `json:"final_acceptance"`
}

// Writes an immutable artifact observation before admission. The first block
// remains unchanged when the same bytes occur at later finalized blocks.
func WriteProvisionalRuntimeObservation(directory string, artifact AuthenticatedRuntimeArtifact) error {
	if !filepath.IsAbs(directory) || artifact.BlockHash == (types.Hash{}) || artifact.GenesisHash == (types.Hash{}) || artifact.CompatibilityProfile != ProvisionalRuntimeCompatibilityProfile {
		return errors.New("provisional runtime observation path or artifact is incomplete")
	}
	identity, err := canonicalRuntimeArtifactIdentity(RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(errors.New("runtime observation directory is not a regular directory"), err)
	}
	key, err := json.Marshal(struct {
		Genesis string
		Runtime RuntimeArtifactIdentity
	}{artifact.GenesisHash.Hex(), identity})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(key)
	path := filepath.Join(directory, hex.EncodeToString(digest[:])+".json")
	record := provisionalRuntimeObservation{Schema: "urnetwork-provisional-runtime-observation-v1", Profile: artifact.CompatibilityProfile, BlockHash: artifact.BlockHash.Hex(), GenesisHash: artifact.GenesisHash.Hex(), Runtime: identity, Provisional: true, FinalAcceptance: false}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("runtime observation is not a regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var prior provisionalRuntimeObservation
		if err := json.Unmarshal(raw, &prior); err != nil {
			return err
		}
		if prior.Schema != record.Schema || prior.Profile != record.Profile || prior.GenesisHash != record.GenesisHash || prior.Runtime != identity || !prior.Provisional || prior.FinalAcceptance || prior.BlockHash == "" {
			return errors.New("retained runtime observation differs")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(directory, ".runtime-observation-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(encoded)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	// Link is atomic and cannot replace an existing observation or a symlink.
	if err := os.Link(file.Name(), path); err != nil {
		if os.IsExist(err) {
			return WriteProvisionalRuntimeObservation(directory, artifact)
		}
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
