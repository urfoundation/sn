package miner

// A catalog is external immutable review authority, not an observed-runtime
// cache. Each artifact has only its explicitly reviewed consumed purposes.
import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

const fleetRuntimeCatalogSchema = "urnetwork-mainnet-fleet-runtime-authority-v2"
const fleetRuntimeCatalogLimit = 8

// The per-artifact digest names the source/build and listed semantic review.
type fleetRuntimeCatalogEntry struct {
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeReviewSha256 string                      `json:"runtime_review_sha256"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
	Purposes            []crv4.FleetRuntimePurpose  `json:"purposes"`
}

func (self fleetRuntimeCatalogEntry) identity() crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{Version: self.RuntimeVersion, CodeHash: self.RuntimeCodeHash, MetadataHash: self.RuntimeMetadataHash}
}

// Common exact coordinates are required for both retained v1 and catalog v2.
func validFleetRuntimeCoordinates(source, review string, identity crv4.RuntimeArtifactIdentity) bool {
	return fleetAuthorityHex(source, 20, "") && fleetAuthorityHex(review, 32, "") && fleetAuthorityHex(identity.CodeHash, 32, "0x") && fleetAuthorityHex(identity.MetadataHash, 32, "0x") && identity.Version.SpecName != "" && strings.TrimSpace(identity.Version.SpecName) == identity.Version.SpecName && identity.Version.SpecVersion != 0 && identity.Version.TransactionVersion != 0 && identity.Version.StateVersion != 0
}

func (self *fleetMainnetRuntimeAuthority) validateRuntimeCatalog() error {
	if self.Schema == fleetMainnetRuntimeAuthoritySchema {
		if len(self.RuntimeCatalog) != 0 || self.RuntimeReviewScope != fleetMainnetRuntimeReviewScope || !validFleetRuntimeCoordinates(self.RuntimeSourceCommit, self.RuntimeReviewSha256, self.artifactIdentity()) {
			return errors.New("mainnet runtime authority lacks reviewed source, build provenance or fleet interface scope")
		}
		return nil
	}
	if self.Schema != fleetRuntimeCatalogSchema || len(self.RuntimeCatalog) == 0 || len(self.RuntimeCatalog) > fleetRuntimeCatalogLimit || self.RuntimeSourceCommit != "" || self.RuntimeReviewScope != "" || self.RuntimeReviewSha256 != "" || self.RuntimeVersion != (crv4.RuntimeVersionIdentity{}) || self.RuntimeCodeHash != "" || self.RuntimeMetadataHash != "" {
		return errors.New("mainnet runtime catalog has mixed authority, missing entries or excessive bounds")
	}
	seen := map[crv4.RuntimeVersionIdentity]bool{}
	for _, entry := range self.RuntimeCatalog {
		if !validFleetRuntimeCoordinates(entry.RuntimeSourceCommit, entry.RuntimeReviewSha256, entry.identity()) || seen[entry.RuntimeVersion] || len(entry.Purposes) == 0 || len(entry.Purposes) > 7 {
			return errors.New("mainnet runtime catalog exact artifact/review or purpose bound differs")
		}
		seen[entry.RuntimeVersion] = true
		purposes := map[crv4.FleetRuntimePurpose]bool{}
		for _, purpose := range entry.Purposes {
			if !crv4.KnownFleetRuntimePurpose(purpose) || purposes[purpose] {
				return errors.New("mainnet runtime catalog repeats or does not implement a reviewed purpose")
			}
			purposes[purpose] = true
		}
	}
	return nil
}

func (self *fleetMainnetRuntimeAuthority) artifactIdentities() []crv4.RuntimeArtifactIdentity {
	if self == nil || self.Schema == fleetMainnetRuntimeAuthoritySchema {
		return []crv4.RuntimeArtifactIdentity{self.artifactIdentity()}
	}
	identities := make([]crv4.RuntimeArtifactIdentity, 0, len(self.RuntimeCatalog))
	for _, entry := range self.RuntimeCatalog {
		identities = append(identities, entry.identity())
	}
	return identities
}

// Purpose refusal is local to this operation. No source data or completed
// custody is changed; another readable capability can still use the same owner.
func (self *fleetMainnetRuntimeAuthority) authenticateFor(ctx context.Context, chain *crv4.Chain, block types.Hash, purpose crv4.FleetRuntimePurpose) (crv4.AuthenticatedRuntimeArtifact, error) {
	if !crv4.KnownFleetRuntimePurpose(purpose) {
		return crv4.AuthenticatedRuntimeArtifact{}, fmt.Errorf("fleet runtime purpose %q is unavailable", purpose)
	}
	artifact, err := self.authenticateAt(ctx, chain, block)
	if err != nil {
		return artifact, err
	}
	if self == nil || self.Schema == fleetMainnetRuntimeAuthoritySchema {
		return artifact, nil
	}
	identity := crv4.RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}
	for _, entry := range self.RuntimeCatalog {
		if entry.identity() == identity {
			if !slices.Contains(entry.Purposes, purpose) {
				return crv4.AuthenticatedRuntimeArtifact{}, fmt.Errorf("fleet runtime %d has no reviewed %s capability", artifact.Version.SpecVersion, purpose)
			}
			if err := crv4.ValidateFleetRuntimeMetadata(artifact.Metadata, purpose); err != nil {
				return crv4.AuthenticatedRuntimeArtifact{}, err
			}
			return artifact, ctx.Err()
		}
	}
	return crv4.AuthenticatedRuntimeArtifact{}, errors.New("fleet runtime selected artifact is outside original review")
}

func (self *fleetMainnetRuntimeAuthority) viewFor(ctx context.Context, chain *crv4.Chain, block types.Hash, purpose crv4.FleetRuntimePurpose) (*crv4.Chain, error) {
	artifact, err := self.authenticateFor(ctx, chain, block, purpose)
	if err != nil {
		return nil, err
	}
	view := *chain
	if err := view.BindRuntimeArtifact(artifact); err != nil {
		return nil, err
	}
	return &view, nil
}

func (self *fleetMainnetRuntimeAuthority) finalizedFor(ctx context.Context, chain *crv4.Chain, purpose crv4.FleetRuntimePurpose) (*crv4.Chain, types.Hash, error) {
	block, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return nil, block, err
	}
	view, err := self.viewFor(ctx, chain, block, purpose)
	return view, block, err
}

func sameFleetRuntime(left, right crv4.AuthenticatedRuntimeArtifact) error {
	if left.Version != right.Version || left.CodeHash != right.CodeHash || left.MetadataHash != right.MetadataHash {
		return errors.New("fleet native signing artifact changed; original signed bytes cannot be relabeled")
	}
	return nil
}

// The pinned preparation view and fresh current view are independent checks.
// Both being in a catalog does not preserve the original signature domain.
func (self *fleetMainnetRuntimeAuthority) signingAdmission(ctx context.Context, chain *crv4.Chain, prepared types.Hash, purpose crv4.FleetRuntimePurpose) error {
	original, err := self.authenticateFor(ctx, chain, prepared, purpose)
	if err != nil {
		return err
	}
	current, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return err
	}
	artifact, err := self.authenticateFor(ctx, chain, current, purpose)
	if err != nil {
		return err
	}
	return errors.Join(sameFleetRuntime(original, artifact), ctx.Err())
}

func fleetNativeWritePurpose(action string) crv4.FleetRuntimePurpose {
	if action == "register" {
		return crv4.FleetRegistrationWrite
	}
	return crv4.FleetCommitmentWrite
}

// Execution reads use the parent artifact, while postconditions select the
// inclusion block's state separately. This callback never signs or sends.
func (self *fleetMainnetRuntimeAuthority) executionAdmission(chain *crv4.Chain, prepared types.Hash, purpose crv4.FleetRuntimePurpose) func(context.Context, types.Hash) (crv4.AuthenticatedRuntimeArtifact, error) {
	return func(ctx context.Context, parent types.Hash) (crv4.AuthenticatedRuntimeArtifact, error) {
		original, err := self.authenticateFor(ctx, chain, prepared, purpose)
		if err != nil {
			return crv4.AuthenticatedRuntimeArtifact{}, err
		}
		execution, err := self.authenticateFor(ctx, chain, parent, crv4.FleetDispatchRead)
		if err != nil {
			return execution, err
		}
		return execution, errors.Join(sameFleetRuntime(original, execution), ctx.Err())
	}
}
