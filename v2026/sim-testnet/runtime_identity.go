// Runtime identity keeps every release-critical Substrate read on one exact
// finalized runtime, including fields missing from the GSRPC RuntimeVersion.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
)

// runtimeVersionIdentity is the complete release-bound result returned by
// state_getRuntimeVersion. Presence is checked while decoding, so a missing
// stateVersion cannot be confused with the valid numeric value zero.
type runtimeVersionIdentity = crv4.RuntimeVersionIdentity

// Binds a requested block's exact version and code to content-addressed decoded
// metadata. On a cache hit, the large bytes were authenticated at another block
// carrying the identical reviewed artifact.
type authenticatedRuntimeMetadata struct {
	FinalizedHash types.Hash
	Version       runtimeVersionIdentity
	CodeHash      string
	MetadataHash  string
	Metadata      *types.Metadata
}

type historicalRuntimeArtifactIdentity struct {
	CodeHash     string
	MetadataHash string
}

// Carried setup evidence was finalized across the two runtimes immediately
// preceding v453. These are evidence-only compatibility identities: they may
// reconcile an already-finalized receipt, but current reads, signing and every
// broadcast continue to require v453 exactly.
func reviewedHistoricalRuntimeArtifact(version runtimeVersionIdentity) (historicalRuntimeArtifactIdentity, bool) {
	if version.SpecName != "node-subtensor" || version.TransactionVersion != 1 || version.StateVersion != 1 {
		return historicalRuntimeArtifactIdentity{}, false
	}
	switch version.SpecVersion {
	case 451:
		return historicalRuntimeArtifactIdentity{
			CodeHash:     "0xf3554a22dfcefa9b42b3a0a5e58c1e6c871795ecc9ea9da78bf0900e23e57c08",
			MetadataHash: "0xeecd7e7c00377caec23c3dc754fd621963cc456fa5d02a4f66ff267b0494bd9d",
		}, true
	case 452:
		return historicalRuntimeArtifactIdentity{
			CodeHash:     "0x40a8c3c99a47d6739b086236308535fab26d5fd4cc5c88eb83f6a3c8b928f7cc",
			MetadataHash: "0x2e1d4f992a978fdd58652c8cf434c26bb8f89170e6a0fdbc9362b29e8fe8a835",
		}, true
	default:
		return historicalRuntimeArtifactIdentity{}, false
	}
}

// Decode the authoritative RPC result while retaining required-field
// presence. Unknown forward-compatible fields are ignored, but duplicate
// release-bound fields and trailing JSON are rejected.
func decodeRuntimeVersionIdentity(raw json.RawMessage) (runtimeVersionIdentity, error) {
	return crv4.DecodeRuntimeVersionIdentity(raw)
}

// Read the complete runtime identity at one caller-selected block. A raw JSON
// result is necessary because the pinned GSRPC type has no stateVersion field.
func runtimeVersionAt(chain *crv4.Chain, finalized types.Hash) (runtimeVersionIdentity, error) {
	return crv4.RuntimeVersionAt(chain, finalized)
}

// Require the runtime name and all three numeric identity fields to match the
// release profile.
func validateRuntimeVersionIdentity(version runtimeVersionIdentity, expectedSpec, expectedTransaction uint32, expectedState uint8) error {
	if version.SpecName != "node-subtensor" {
		return fmt.Errorf("runtime spec name %q, want node-subtensor", version.SpecName)
	}
	if version.SpecVersion != expectedSpec {
		return fmt.Errorf("runtime spec %d, want %d", version.SpecVersion, expectedSpec)
	}
	if version.TransactionVersion != expectedTransaction {
		return fmt.Errorf("transaction version %d, want %d", version.TransactionVersion, expectedTransaction)
	}
	if version.StateVersion != expectedState {
		return fmt.Errorf("state version %d, want %d", version.StateVersion, expectedState)
	}
	return nil
}

// Require signed public deployment evidence to disclose the same complete
// runtime identity as the release lock instead of relying on spec_version as a
// lossy proxy for code, metadata, transaction, and state compatibility.
func validatePublishedRuntimeIdentity(public *PublicDeploymentManifest, cfg *ResolvedConfig) error {
	if public == nil || cfg == nil || cfg.Public == nil || cfg.Release == nil {
		return errors.New("published runtime identity dependencies are unavailable")
	}
	if err := validatePublishedRuntimeIdentityShape(public); err != nil {
		return err
	}
	if public.RuntimeSpec != cfg.Public.Chain.ExpectedRuntimeSpec ||
		public.TransactionVersion != cfg.Public.Chain.ExpectedTransactionVersion ||
		public.StateVersion != cfg.Public.Chain.ExpectedStateVersion {
		return errors.New("published runtime version identity does not match the release")
	}
	if err := validateRuntimeCodeHash(public.RuntimeCodeHash, cfg.Release.Runtime.CodeHash); err != nil {
		return fmt.Errorf("published runtime identity: %w", err)
	}
	if err := validateRuntimeMetadataHash(public.RuntimeMetadataHash, cfg.Release.Runtime.MetadataHash); err != nil {
		return fmt.Errorf("published runtime identity: %w", err)
	}
	return nil
}

// Require a signed release-1.0 public manifest to carry the reviewed runtime
// identity even in secretless readers which do not load local config. A
// manifest signer cannot redefine which artifact the release name denotes.
func validatePublishedRuntimeIdentityShape(public *PublicDeploymentManifest) error {
	if public == nil ||
		public.RuntimeSpec != reviewedRuntimeSpecVersion ||
		public.TransactionVersion != reviewedRuntimeTransactionVersion ||
		public.StateVersion != reviewedRuntimeStateVersion {
		return errors.New("published runtime version identity is not the reviewed node-subtensor/453/1/1 release")
	}
	if err := validateRuntimeCodeHash(public.RuntimeCodeHash, reviewedRuntimeCodeHash); err != nil {
		return fmt.Errorf("published runtime identity: %w", err)
	}
	if err := validateRuntimeMetadataHash(public.RuntimeMetadataHash, reviewedRuntimeMetadataHash); err != nil {
		return fmt.Errorf("published runtime identity: %w", err)
	}
	return nil
}

// Pin the exact on-chain Wasm at the same finalized block as its version and
// metadata. A spec version alone is not a content identity.
func runtimeCodeHashAt(chain *crv4.Chain, finalized types.Hash) (string, error) {
	codeHash, err := crv4.RuntimeCodeHashAt(chain, finalized)
	if err != nil {
		return "", err
	}
	if err := validateRuntimeCodeHash(codeHash, codeHash); err != nil {
		return "", err
	}
	return strings.ToLower(codeHash), nil
}

// Validate both encodings before comparing the finalized and release-lock
// hashes, preventing malformed equal strings from passing.
func validateRuntimeCodeHash(observed, expected string) error {
	validate := func(label, value string) error {
		if len(value) != 66 || !strings.HasPrefix(value, "0x") {
			return fmt.Errorf("invalid %s runtime code hash %q", label, value)
		}
		if _, err := hex.DecodeString(value[2:]); err != nil {
			return fmt.Errorf("invalid %s runtime code hash %q: %w", label, value, err)
		}
		return nil
	}
	if err := validate("finalized", observed); err != nil {
		return err
	}
	if err := validate("release-lock", expected); err != nil {
		return err
	}
	if !strings.EqualFold(observed, expected) {
		return fmt.Errorf("finalized runtime code hash %s, want %s", observed, expected)
	}
	return nil
}

// Read and hash the exact SCALE metadata bytes instead of re-encoding the
// decoded structure. This preserves an authoritative byte identity while also
// proving the bytes are valid metadata before they are used for calls/storage.
func runtimeMetadataAt(chain *crv4.Chain, finalized types.Hash) (*types.Metadata, string, error) {
	return crv4.RuntimeMetadataAt(chain, finalized)
}

// Converts the release manifest tuple to the generic CRv4 identity boundary.
func runtimeArtifactIdentity(version runtimeVersionIdentity, codeHash, metadataHash string) crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
}

// Builds the sole artifact admitted by current-state and signing dependencies.
func currentReleaseRuntimeArtifact(cfg *ResolvedConfig) crv4.RuntimeArtifactIdentity {
	return runtimeArtifactIdentity(runtimeVersionIdentity{
		SpecName: "node-subtensor", SpecVersion: cfg.Public.Chain.ExpectedRuntimeSpec,
		TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion,
		StateVersion:       cfg.Public.Chain.ExpectedStateVersion,
	}, cfg.Release.Runtime.CodeHash, cfg.Release.Runtime.MetadataHash)
}

// Builds the current artifact plus the two evidence-only predecessor tuples.
func releaseHistoryRuntimeArtifacts(cfg *ResolvedConfig) ([]crv4.RuntimeArtifactIdentity, error) {
	result := []crv4.RuntimeArtifactIdentity{currentReleaseRuntimeArtifact(cfg)}
	for _, spec := range []uint32{451, 452} {
		version := runtimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: spec, TransactionVersion: 1, StateVersion: 1}
		artifact, ok := reviewedHistoricalRuntimeArtifact(version)
		if !ok {
			return nil, fmt.Errorf("reviewed historical runtime %d is unavailable", spec)
		}
		result = append(result, runtimeArtifactIdentity(version, artifact.CodeHash, artifact.MetadataHash))
	}
	return result, nil
}

// The public provider may briefly reject expensive historical work. Metadata
// is fetched at most once per exact runtime artifact, so this bounded retry is
// both sufficient and incapable of turning a persistent/archive error into an
// unbounded setup stall.
func releaseRuntimeRPCRetryPolicy() finalSemanticRPCRetryPolicy {
	policy := defaultFinalSemanticRPCRetryPolicy()
	policy.initialRetryDelay = 5 * time.Second
	policy.maximumRetryDelay = 20 * time.Second
	return policy
}

// Applies the bounded transient policy around one complete exact-block runtime
// authentication. Permanent identity and archive errors fail immediately.
func readRuntimeArtifactWithPolicy(ctx context.Context, chain *crv4.Chain, finalized types.Hash, allowedIdentities []crv4.RuntimeArtifactIdentity, policy finalSemanticRPCRetryPolicy) (authenticatedRuntimeMetadata, error) {
	result := authenticatedRuntimeMetadata{FinalizedHash: finalized}
	if ctx == nil || chain == nil || finalized == (types.Hash{}) || len(allowedIdentities) == 0 {
		return result, errors.New("runtime artifact authentication context is incomplete")
	}
	var authenticated crv4.AuthenticatedRuntimeArtifact
	err := retryFinalSemanticRPCCall(ctx, nil, policy, func(attemptCtx context.Context) error {
		var attemptErr error
		authenticated, attemptErr = crv4.AuthenticateRuntimeArtifactAtContext(attemptCtx, chain, finalized, allowedIdentities...)
		return attemptErr
	})
	if err != nil {
		return result, err
	}
	return authenticatedRuntimeMetadata{
		FinalizedHash: authenticated.BlockHash,
		Version:       authenticated.Version,
		CodeHash:      authenticated.CodeHash,
		MetadataHash:  authenticated.MetadataHash,
		Metadata:      authenticated.Metadata,
	}, nil
}

// Require the exact reviewed metadata bytes served for the finalized runtime.
func validateRuntimeMetadataHash(observed, expected string) error {
	if err := validateRuntimeCodeHash(observed, expected); err != nil {
		return errors.New(strings.NewReplacer("runtime code hash", "runtime metadata hash").Replace(err.Error()))
	}
	return nil
}

// Bind a chain to metadata and signing versions authenticated at one immutable
// finalized hash. The state version has already been checked separately.
func bindAuthenticatedRuntime(chain *crv4.Chain, authenticated authenticatedRuntimeMetadata) {
	chain.Meta = authenticated.Metadata
	chain.Runtime = &types.RuntimeVersion{
		SpecName:           authenticated.Version.SpecName,
		SpecVersion:        types.U32(authenticated.Version.SpecVersion),
		TransactionVersion: types.U32(authenticated.Version.TransactionVersion),
	}
}

// Authenticate every runtime identity dimension at a caller-selected finalized
// hash. This function is read-only: shared release chains bind once during
// initialization so concurrent checks cannot race by replacing Meta/Runtime.
func readAuthenticatedRuntimeMetadataAtContext(ctx context.Context, chain *crv4.Chain, cfg *ResolvedConfig, finalized types.Hash) (authenticatedRuntimeMetadata, error) {
	result := authenticatedRuntimeMetadata{FinalizedHash: finalized}
	if ctx == nil || cfg == nil || cfg.Public == nil || cfg.Release == nil {
		return result, errors.New("release runtime manifests are unavailable")
	}
	if err := validateReviewedRuntimeIdentity(cfg.Release); err != nil {
		return result, err
	}
	if cfg.Release.Runtime.SpecVersion != cfg.Public.Chain.ExpectedRuntimeSpec ||
		cfg.Release.Runtime.TransactionVersion != cfg.Public.Chain.ExpectedTransactionVersion ||
		cfg.Release.Runtime.StateVersion != cfg.Public.Chain.ExpectedStateVersion {
		return result, errors.New("release/runtime manifest mismatch")
	}
	var err error
	result, err = readRuntimeArtifactWithPolicy(ctx, chain, finalized, []crv4.RuntimeArtifactIdentity{currentReleaseRuntimeArtifact(cfg)}, releaseRuntimeRPCRetryPolicy())
	if err != nil {
		return result, err
	}
	if err := validateRuntimeVersionIdentity(result.Version, cfg.Public.Chain.ExpectedRuntimeSpec, cfg.Public.Chain.ExpectedTransactionVersion, cfg.Public.Chain.ExpectedStateVersion); err != nil {
		return result, err
	}
	if err := validateRuntimeCodeHash(result.CodeHash, cfg.Release.Runtime.CodeHash); err != nil {
		return result, err
	}
	if err := validateRuntimeMetadataHash(result.MetadataHash, cfg.Release.Runtime.MetadataHash); err != nil {
		return result, err
	}
	return result, nil
}

// Preserves the contextless compatibility path for existing current readers.
func readAuthenticatedRuntimeMetadataAt(chain *crv4.Chain, cfg *ResolvedConfig, finalized types.Hash) (authenticatedRuntimeMetadata, error) {
	return readAuthenticatedRuntimeMetadataAtContext(context.Background(), chain, cfg, finalized)
}

// Authenticate metadata-driven reads of immutable carried setup evidence.
// Current v453 is always accepted through the release lock. Only the two exact
// historical artifact identities present in the persisted campaign history are
// admitted as compatibility inputs; this helper must never guard a write.
func readReleaseHistoryRuntimeMetadataAtContext(ctx context.Context, chain *crv4.Chain, cfg *ResolvedConfig, finalized types.Hash) (authenticatedRuntimeMetadata, error) {
	result := authenticatedRuntimeMetadata{FinalizedHash: finalized}
	if ctx == nil || cfg == nil || cfg.Public == nil || cfg.Release == nil {
		return result, errors.New("release runtime manifests are unavailable")
	}
	if err := validateReviewedRuntimeIdentity(cfg.Release); err != nil {
		return result, err
	}
	if cfg.Release.Runtime.SpecVersion != cfg.Public.Chain.ExpectedRuntimeSpec ||
		cfg.Release.Runtime.TransactionVersion != cfg.Public.Chain.ExpectedTransactionVersion ||
		cfg.Release.Runtime.StateVersion != cfg.Public.Chain.ExpectedStateVersion {
		return result, errors.New("release/runtime manifest mismatch")
	}
	allowed, err := releaseHistoryRuntimeArtifacts(cfg)
	if err != nil {
		return result, err
	}
	result, err = readRuntimeArtifactWithPolicy(ctx, chain, finalized, allowed, releaseRuntimeRPCRetryPolicy())
	if err != nil {
		return result, err
	}
	if currentErr := validateRuntimeVersionIdentity(result.Version, cfg.Public.Chain.ExpectedRuntimeSpec, cfg.Public.Chain.ExpectedTransactionVersion, cfg.Public.Chain.ExpectedStateVersion); currentErr == nil {
		if err := validateRuntimeCodeHash(result.CodeHash, cfg.Release.Runtime.CodeHash); err != nil {
			return result, err
		}
		if err := validateRuntimeMetadataHash(result.MetadataHash, cfg.Release.Runtime.MetadataHash); err != nil {
			return result, err
		}
		return result, nil
	}
	historical, ok := reviewedHistoricalRuntimeArtifact(result.Version)
	if !ok {
		return result, fmt.Errorf(
			"runtime history at %s is unreviewed %s/%d/%d/%d",
			finalized.Hex(), result.Version.SpecName, result.Version.SpecVersion,
			result.Version.TransactionVersion, result.Version.StateVersion,
		)
	}
	if err := validateRuntimeCodeHash(result.CodeHash, historical.CodeHash); err != nil {
		return result, fmt.Errorf("historical runtime %d: %w", result.Version.SpecVersion, err)
	}
	if err := validateRuntimeMetadataHash(result.MetadataHash, historical.MetadataHash); err != nil {
		return result, fmt.Errorf("historical runtime %d: %w", result.Version.SpecVersion, err)
	}
	return result, nil
}

// Proves a carried native receipt using cancellable, retryable reads bound to
// the exact runtime artifact present at its finalized block.
func verifyReleaseHistoryFinalizedExtrinsicContext(ctx context.Context, chain *crv4.Chain, cfg *ResolvedConfig, blockHash, extrinsicHash types.Hash) error {
	authenticated, err := readReleaseHistoryRuntimeMetadataAtContext(ctx, chain, cfg, blockHash)
	if err != nil {
		return fmt.Errorf("authenticate finalized extrinsic runtime at %s: %w", blockHash.Hex(), err)
	}
	historical := *chain
	bindAuthenticatedRuntime(&historical, authenticated)
	return retryFinalSemanticRPCCall(ctx, nil, releaseRuntimeRPCRetryPolicy(), func(attemptCtx context.Context) error {
		return historical.VerifyFinalizedExtrinsicContext(attemptCtx, blockHash, extrinsicHash)
	})
}

// Dial one release endpoint, authenticate genesis, then bind metadata and all
// runtime versions from a single finalized hash.
func dialReleaseSubstrateChain(cfg *ResolvedConfig, endpoint string) (*crv4.Chain, authenticatedRuntimeMetadata, error) {
	if cfg == nil || cfg.Public == nil {
		return nil, authenticatedRuntimeMetadata{}, errors.New("release public manifest is unavailable")
	}
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		return nil, authenticatedRuntimeMetadata{}, err
	}
	closeWithError := func(err error) (*crv4.Chain, authenticatedRuntimeMetadata, error) {
		chain.API.Client.Close()
		return nil, authenticatedRuntimeMetadata{}, err
	}
	if !strings.EqualFold(chain.GenesisHash.Hex(), cfg.Public.Chain.GenesisHash) {
		return closeWithError(fmt.Errorf("genesis %s, want %s", chain.GenesisHash.Hex(), cfg.Public.Chain.GenesisHash))
	}
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return closeWithError(err)
	}
	authenticated, err := readAuthenticatedRuntimeMetadataAt(chain, cfg, finalized)
	if err != nil {
		return closeWithError(err)
	}
	bindAuthenticatedRuntime(chain, authenticated)
	return chain, authenticated, nil
}
