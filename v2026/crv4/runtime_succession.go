// Mainnet producers continue across a Subtensor runtime upgrade only through
// this explicit, caller-installed admission. A successor is an exact artifact
// authenticated at its own block, never a relabeled approval or a testnet
// provisional proof: every strict reader, purpose check and signing domain
// sees its real version, code and metadata.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Bounds connection-local reuse; eviction costs one more metadata read and
// profile check, never admission or an already issued proof.
const maximumRuntimeSuccessorAdmissions = 8

// One first observation is offered to the caller's consumed-storage checks
// before it is admitted. Metadata is borrowed read-only for the call.
type RuntimeSuccessor struct {
	Approved  RuntimeArtifactIdentity
	Artifact  RuntimeArtifactIdentity
	BlockHash types.Hash
	Metadata  *types.Metadata
}

type runtimeSuccessorKey struct {
	approved RuntimeArtifactIdentity
	version  RuntimeVersionIdentity
	code     string
}

// Immutable once published. A strict proof retains it after cache eviction.
type runtimeSuccessorAdmission struct {
	policy   *runtimeSuccessionPolicy
	approved RuntimeArtifactIdentity
	artifact RuntimeArtifactIdentity
}

// The approved anchors and caller checks never change after installation.
type runtimeSuccessionPolicy struct {
	genesis  types.Hash
	profile  string
	approved []RuntimeArtifactIdentity
	admit    func(RuntimeSuccessor) error
	mu       sync.Mutex
	admitted map[runtimeSuccessorKey]*runtimeSuccessorAdmission
}

// Callers install this before sharing the connection, only after validating a
// signed opt-in naming the producer interface profile for exactly these
// approved artifacts. Copies of the Chain share the policy. It is exclusive
// with provisional compatibility and does not change any exact pin: a caller
// still names an approved anchor in each allowlist, and an allowlist without
// one keeps its exact meaning.
func (self *Chain) EnableRuntimeSuccession(expectedGenesis types.Hash, profile string, approved []RuntimeArtifactIdentity, admit func(RuntimeSuccessor) error) error {
	if self == nil || expectedGenesis == (types.Hash{}) || self.GenesisHash != expectedGenesis || profile != ValidatorProducerRuntimeProfile || admit == nil ||
		len(approved) == 0 || len(approved) > maximumRuntimeMetadataArtifactsPerChain {
		return errors.New("runtime successor authority is incomplete")
	}
	anchors := make([]RuntimeArtifactIdentity, 0, len(approved))
	for _, identity := range approved {
		canonical, err := canonicalRuntimeArtifactIdentity(identity)
		if err != nil {
			return err
		}
		version := canonical.Version
		if version.SpecName != "node-subtensor" || version.SpecVersion == 0 || version.TransactionVersion != 1 || version.StateVersion != 1 {
			return errors.New("runtime successor anchor is outside the producer encoding family")
		}
		if slices.Contains(anchors, canonical) {
			return errors.New("runtime successor anchor is duplicated")
		}
		anchors = append(anchors, canonical)
	}
	runtimeMetadataArtifactCacheInitialization.stateLock.Lock()
	defer runtimeMetadataArtifactCacheInitialization.stateLock.Unlock()
	if self.provisionalRuntime != nil {
		return errors.New("runtime successor admission cannot join provisional compatibility")
	}
	if existing := self.runtimeSuccession; existing != nil {
		if existing.genesis != expectedGenesis || existing.profile != profile || !slices.Equal(existing.approved, anchors) {
			return errors.New("runtime successor authority changed after installation")
		}
		return nil
	}
	self.runtimeSuccession = &runtimeSuccessionPolicy{genesis: expectedGenesis, profile: profile, approved: anchors, admit: admit,
		admitted: map[runtimeSuccessorKey]*runtimeSuccessorAdmission{}}
	return nil
}

// Reads the installed policy under the same lock that publishes it.
func (self *Chain) runtimeSuccessionPolicy() *runtimeSuccessionPolicy {
	if self == nil {
		return nil
	}
	runtimeMetadataArtifactCacheInitialization.stateLock.Lock()
	defer runtimeMetadataArtifactCacheInitialization.stateLock.Unlock()
	if self.runtimeSuccession == nil || self.runtimeSuccession.genesis != self.GenesisHash {
		return nil
	}
	return self.runtimeSuccession
}

// Reports whether this connection carries an installed successor policy.
func (self *Chain) RuntimeSuccessionEnabled() bool {
	return self.runtimeSuccessionPolicy() != nil
}

// Reports the approved anchor when an installed successor policy admitted this
// exact artifact. Exported fields alone cannot create or transfer that status.
func (self AuthenticatedRuntimeArtifact) RuntimeSuccessorOf() (RuntimeArtifactIdentity, bool) {
	if self.authenticationProof == nil || self.authenticationProof.successor == nil {
		return RuntimeArtifactIdentity{}, false
	}
	return self.authenticationProof.successor.approved, true
}

func describeRuntimeVersion(version RuntimeVersionIdentity) string {
	return fmt.Sprintf("%s/%d/%d/%d", version.SpecName, version.SpecVersion, version.TransactionVersion, version.StateVersion)
}

// Selects an admitted successor of the one approved anchor in a caller's
// allowlist. A nil admission without error means no installed policy applies,
// so the caller reports its ordinary unreviewed identity. Prefetched metadata
// is returned only for a first observation, to avoid a second large read.
func authenticateRuntimeSuccessorAtContext(ctx context.Context, chain *Chain, blockHash types.Hash, version RuntimeVersionIdentity, allowed []RuntimeArtifactIdentity) (*runtimeSuccessorAdmission, *types.Metadata, error) {
	policy := chain.runtimeSuccessionPolicy()
	if policy == nil || chain.ProvisionalRuntimeCompatibilityEnabled() {
		return nil, nil, nil
	}
	var approved *RuntimeArtifactIdentity
	for index := range allowed {
		if slices.Contains(policy.approved, allowed[index]) {
			if approved != nil {
				return nil, nil, errors.New("runtime successor allowlist names more than one approved anchor")
			}
			approved = &allowed[index]
		}
	}
	if approved == nil {
		return nil, nil, nil
	}
	if version.SpecName != approved.Version.SpecName || version.TransactionVersion != approved.Version.TransactionVersion ||
		version.StateVersion != approved.Version.StateVersion || version.SpecVersion <= approved.Version.SpecVersion {
		return nil, nil, fmt.Errorf("runtime at %s has identity %s, which is not a successor of approved %s", blockHash.Hex(), describeRuntimeVersion(version), describeRuntimeVersion(approved.Version))
	}
	code, err := RuntimeCodeHashAtContext(ctx, chain, blockHash)
	if err != nil {
		return nil, nil, fmt.Errorf("read runtime successor code hash at %s: %w", blockHash.Hex(), err)
	}
	code, err = canonicalRuntimeArtifactHash("observed runtime successor code hash", code)
	if err != nil {
		return nil, nil, err
	}
	key := runtimeSuccessorKey{approved: *approved, version: version, code: code}
	policy.mu.Lock()
	admitted := policy.admitted[key]
	policy.mu.Unlock()
	if admitted != nil {
		return admitted, nil, nil
	}
	refuse := func(err error) error {
		return fmt.Errorf("runtime %s (code %s) at %s is not an admitted successor of approved %s: %w", describeRuntimeVersion(version), code, blockHash.Hex(), describeRuntimeVersion(approved.Version), err)
	}
	metadata, metadataHash, err := RuntimeMetadataAtContext(ctx, chain, blockHash)
	if err != nil {
		return nil, nil, fmt.Errorf("read runtime successor metadata at %s: %w", blockHash.Hex(), err)
	}
	metadataHash, err = canonicalRuntimeArtifactHash("observed runtime successor metadata hash", metadataHash)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &raw, "state_getRuntimeVersion", blockHash.Hex()); err != nil {
		return nil, nil, fmt.Errorf("read runtime successor APIs at %s: %w", blockHash.Hex(), err)
	}
	observed, err := DecodeRuntimeVersionIdentity(raw)
	if err != nil {
		return nil, nil, err
	}
	if observed != version {
		return nil, nil, errors.New("runtime successor version changed during observation")
	}
	if err := validateValidatorProducerRuntimeApis(raw); err != nil {
		return nil, nil, refuse(fmt.Errorf("validator producer selective-metagraph API: %w", err))
	}
	if err := validateValidatorProducerMetadata(metadata); err != nil {
		return nil, nil, refuse(err)
	}
	artifact := RuntimeArtifactIdentity{Version: version, CodeHash: code, MetadataHash: metadataHash}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := policy.admit(RuntimeSuccessor{Approved: *approved, Artifact: artifact, BlockHash: blockHash, Metadata: metadata}); err != nil {
		return nil, nil, refuse(err)
	}
	admission := &runtimeSuccessorAdmission{policy: policy, approved: *approved, artifact: artifact}
	policy.mu.Lock()
	if prior := policy.admitted[key]; prior != nil && prior.artifact == artifact {
		admission = prior
	} else {
		if len(policy.admitted) >= maximumRuntimeSuccessorAdmissions {
			for old := range policy.admitted {
				delete(policy.admitted, old)
				break
			}
		}
		policy.admitted[key] = admission
	}
	policy.mu.Unlock()
	return admission, metadata, ctx.Err()
}
