// Production fleet authority is an independently reviewed document, never a
// pin learned from the endpoint or inherited from testnet compatibility.
package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const fleetMainnetRuntimeAuthoritySchema = "urnetwork-mainnet-fleet-runtime-authority-v1"
const fleetMainnetRuntimeReviewScope = "urnetwork-fleet-register-commitment-frontier-v1"
const fleetMainnetAuthorityLimit = 16 * 1024

// The review digest identifies the independent source-to-Wasm and consumed
// interface audit. Matching an Rpc response does not create that provenance.
// There is deliberately no shipped production genesis, source or runtime pin.
type fleetMainnetRuntimeAuthority struct {
	document            []byte
	Schema              string                      `json:"schema"`
	NativeChain         string                      `json:"native_chain"`
	GenesisHash         string                      `json:"genesis_hash"`
	EvmChainId          uint64                      `json:"evm_chain_id"`
	Netuid              uint16                      `json:"netuid"`
	Coordinator         string                      `json:"coordinator"`
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeReviewScope  string                      `json:"runtime_review_scope"`
	RuntimeReviewSha256 string                      `json:"runtime_review_sha256"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
	RuntimeCatalog      []fleetRuntimeCatalogEntry  `json:"runtime_catalog,omitempty"`
}

// Canonical nonzero digests keep authority comparison unambiguous.
func fleetAuthorityHex(value string, size int, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+2*size || strings.ToLower(value) != value {
		return false
	}
	raw, err := hex.DecodeString(value[len(prefix):])
	return err == nil && !bytes.Equal(raw, make([]byte, size))
}

// Rejects scope drift before any key, journal or network access. Chain945
// keeps its existing release/provisional path; no other network inherits it.
func loadFleetMainnetRuntimeAuthority(opts docopt.Opts, manifest *protocol.FleetManifest) (*fleetMainnetRuntimeAuthority, error) {
	path := fleetOpt(opts, "--mainnet-runtime-authority")
	expected := fleetOpt(opts, "--mainnet-runtime-authority-sha256")
	if manifest == nil {
		return nil, errors.New("fleet manifest is unavailable")
	}
	if manifest.ChainID == 945 && path == "" && expected == "" {
		if err := validateFleetRuntimeCompatibility(manifest, fleetOpt(opts, "--provisional-runtime-compatibility"), fleetOpt(opts, "--runtime-observation-dir")); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if manifest.ChainID != 964 || path == "" || !fleetAuthorityHex(expected, 32, "") || fleetOpt(opts, "--provisional-runtime-compatibility") != "" || fleetOpt(opts, "--runtime-observation-dir") != "" {
		return nil, errors.New("mainnet runtime authority requires chain964, an independently reviewed document and SHA-256, without testnet provisional flags")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mainnet runtime authority: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, fleetMainnetAuthorityLimit+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	if len(raw) > fleetMainnetAuthorityLimit || hex.EncodeToString(digest[:]) != expected {
		return nil, errors.New("mainnet runtime authority bytes differ from the reviewed SHA-256 or exceed the size bound")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, fmt.Errorf("mainnet runtime authority: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var authority fleetMainnetRuntimeAuthority
	if err := decoder.Decode(&authority); err != nil {
		return nil, fmt.Errorf("mainnet runtime authority: %w", err)
	}
	if err := authority.validate(manifest); err != nil {
		return nil, err
	}
	authority.document = append([]byte(nil), raw...)
	return &authority, nil
}

// Approval binds both signing domains and the complete reviewed native tuple.
func (self *fleetMainnetRuntimeAuthority) validate(manifest *protocol.FleetManifest) error {
	if self == nil || manifest == nil || self.EvmChainId != 964 || manifest.ChainID != self.EvmChainId || self.Netuid == 0 || self.Netuid != manifest.Netuid || self.NativeChain == "" || strings.TrimSpace(self.NativeChain) != self.NativeChain {
		return errors.New("mainnet runtime authority network or subnet scope differs")
	}
	if !fleetAuthorityHex(self.Coordinator, 20, "0x") || common.HexToAddress(self.Coordinator) != common.Address(manifest.Coordinator) {
		return errors.New("mainnet runtime authority coordinator differs from the manifest")
	}
	if !fleetAuthorityHex(self.GenesisHash, 32, "0x") || self.GenesisHash == fleetProvisionalTestnetGenesis {
		return errors.New("mainnet runtime authority requires independent nonzero genesis, code and metadata hashes")
	}
	return self.validateRuntimeCatalog()
}

// Nil is the existing testnet release authority, never a production fallback.
func (self *fleetMainnetRuntimeAuthority) artifactIdentity() crv4.RuntimeArtifactIdentity {
	if self == nil {
		return fleetReleaseRuntimeArtifact()
	}
	return crv4.RuntimeArtifactIdentity{Version: self.RuntimeVersion, CodeHash: self.RuntimeCodeHash, MetadataHash: self.RuntimeMetadataHash}
}

// Every selected block checks fresh genesis/name, its complete committed
// header and canonical height around the exact runtime read. A connection
// carrying testnet compatibility is inadmissible in production.
func (self *fleetMainnetRuntimeAuthority) authenticateAt(ctx context.Context, chain *crv4.Chain, block types.Hash) (crv4.AuthenticatedRuntimeArtifact, error) {
	return crv4.ReadRuntimeObservationContext(ctx, chain, func(ctx context.Context) (crv4.AuthenticatedRuntimeArtifact, error) {
		return self.authenticateAtAttempt(ctx, chain, block)
	})
}

// A replacement transport must repeat fresh network identity as well as the
// artifact; only one complete original-block attempt may leave this boundary.
func (self *fleetMainnetRuntimeAuthority) authenticateAtAttempt(ctx context.Context, chain *crv4.Chain, block types.Hash) (crv4.AuthenticatedRuntimeArtifact, error) {
	if ctx == nil {
		return crv4.AuthenticatedRuntimeArtifact{}, errors.New("fleet runtime context is absent")
	}
	if err := ctx.Err(); err != nil {
		return crv4.AuthenticatedRuntimeArtifact{}, err
	}
	if self == nil {
		return authenticateFleetRuntimeAtContext(ctx, chain, block)
	}
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || block == (types.Hash{}) || chain.ProvisionalRuntimeCompatibilityEnabled() {
		return crv4.AuthenticatedRuntimeArtifact{}, errors.New("mainnet runtime authority dependencies or isolation are invalid")
	}
	expected, err := types.NewHashFromHexString(self.GenesisHash)
	if err != nil || expected == (types.Hash{}) || chain.GenesisHash != expected {
		return crv4.AuthenticatedRuntimeArtifact{}, errors.New("mainnet runtime authority genesis differs from the connection")
	}
	var genesis types.Hash
	var nativeChain string
	if err := chain.API.Client.CallContext(ctx, &genesis, "chain_getBlockHash", uint64(0)); err != nil {
		return crv4.AuthenticatedRuntimeArtifact{}, err
	}
	if err := chain.API.Client.CallContext(ctx, &nativeChain, "system_chain"); err != nil {
		return crv4.AuthenticatedRuntimeArtifact{}, err
	}
	if genesis != expected || nativeChain != self.NativeChain {
		return crv4.AuthenticatedRuntimeArtifact{}, errors.New("mainnet runtime authority fresh network identity differs")
	}
	number, _, err := chain.ReceiptHeaderAtContext(ctx, block)
	if err != nil {
		return crv4.AuthenticatedRuntimeArtifact{}, err
	}
	checkCanonical := func() error {
		var canonical types.Hash
		if err := chain.API.Client.CallContext(ctx, &canonical, "chain_getBlockHash", number); err != nil {
			return err
		}
		if canonical != block {
			return errors.New("mainnet runtime authority header is not canonical at its authenticated height")
		}
		return ctx.Err()
	}
	if err := checkCanonical(); err != nil {
		return crv4.AuthenticatedRuntimeArtifact{}, err
	}
	artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, chain, block, self.artifactIdentities()...)
	if err != nil {
		return artifact, err
	}
	if artifact.CompatibilityProfile != "" || artifact.Metadata == nil {
		return crv4.AuthenticatedRuntimeArtifact{}, errors.New("mainnet runtime authority exact artifact differs")
	}
	if err := checkCanonical(); err != nil {
		return crv4.AuthenticatedRuntimeArtifact{}, err
	}
	artifact.GenesisHash = genesis
	return artifact, ctx.Err()
}

// The returned signing/read view is block-local; existing shared readers keep
// their metadata and version. The underlying connection retains ownership.
func (self *fleetMainnetRuntimeAuthority) viewAt(ctx context.Context, chain *crv4.Chain, block types.Hash) (*crv4.Chain, error) {
	artifact, err := self.authenticateAt(ctx, chain, block)
	if err != nil {
		return nil, err
	}
	view := *chain
	if err := view.BindRuntimeArtifact(artifact); err != nil {
		return nil, err
	}
	return &view, nil
}

// Current admission and receipt admission share one immutable authority.
func (self *fleetMainnetRuntimeAuthority) finalizedView(ctx context.Context, chain *crv4.Chain) (*crv4.Chain, types.Hash, error) {
	block, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return nil, block, err
	}
	view, err := self.viewAt(ctx, chain, block)
	return view, block, err
}

// Native status does not reinterpret a current runtime through cached release
// metadata, even when the requested commitment has the expected hash.
func (self *fleetMainnetRuntimeAuthority) commitmentFinalized(ctx context.Context, chain *crv4.Chain, netuid uint16, hotkey [32]byte) (*crv4.FinalizedCommitment, error) {
	var block types.Hash
	return crv4.ReadRuntimeObservationContext(ctx, chain, func(ctx context.Context) (*crv4.FinalizedCommitment, error) {
		if block == (types.Hash{}) {
			selected, err := crv4.FinalizedHeadContext(ctx, chain)
			if err != nil {
				return nil, err
			}
			block = selected
		}
		view, err := self.viewFor(ctx, chain, block, crv4.FleetCommitmentRead)
		if err != nil {
			return nil, err
		}
		return view.FleetCommitmentAtContext(ctx, netuid, hotkey, block)
	})
}

// Receipt runtime authentication precedes all commitment storage decoding.
func (self *fleetMainnetRuntimeAuthority) commitmentWrite(ctx context.Context, chain *crv4.Chain, netuid uint16, hotkey, expected [32]byte, receipt *crv4.FinalizedExtrinsic) (*crv4.FinalizedCommitment, error) {
	if receipt == nil || receipt.BlockHash == (types.Hash{}) || receipt.BlockNumber == 0 {
		return nil, errors.New("fleet finalized write receipt is incomplete")
	}
	return crv4.ReadRuntimeObservationContext(ctx, chain, func(ctx context.Context) (*crv4.FinalizedCommitment, error) {
		view, err := self.viewFor(ctx, chain, receipt.BlockHash, crv4.FleetCommitmentRead)
		if err != nil {
			return nil, err
		}
		observed, err := view.FleetCommitmentAtContext(ctx, netuid, hotkey, receipt.BlockHash)
		if err != nil {
			return nil, err
		}
		if err := crv4.ValidateFleetCommitmentWrite(expected, receipt.BlockNumber, observed); err != nil {
			return nil, err
		}
		observed.ExtrinsicHash = receipt.ExtrinsicHash
		return observed, nil
	})
}

// Publication uses authenticated signing inputs and rechecks before sending.
// Upgrades at inclusion fail receipt admission instead of acquiring authority.
func (self *fleetMainnetRuntimeAuthority) publish(ctx context.Context, chain *crv4.Chain, key *crv4.Keypair, netuid uint16, hash [32]byte) (*crv4.FinalizedCommitment, error) {
	view, prepared, err := self.finalizedFor(ctx, chain, crv4.FleetCommitmentWrite)
	if err != nil {
		return nil, err
	}
	call, err := view.NewSetFleetCommitmentCall(netuid, hash)
	if err != nil {
		return nil, err
	}
	nonce, err := view.AccountNonceContext(ctx, key.Address())
	if err != nil {
		return nil, err
	}
	if err := self.signingAdmission(ctx, chain, prepared, crv4.FleetCommitmentWrite); err != nil {
		return nil, err
	}
	signed, err := view.NewSignedExtrinsic(key, call, nonce)
	if err != nil {
		return nil, err
	}
	if err := self.signingAdmission(ctx, chain, prepared, crv4.FleetCommitmentWrite); err != nil {
		return nil, err
	}
	encoded, err := codec.EncodeToHex(*signed)
	if err != nil {
		return nil, err
	}
	var executionRuntime func(context.Context, types.Hash) (crv4.AuthenticatedRuntimeArtifact, error)
	if self != nil {
		executionRuntime = self.executionAdmission(chain, prepared, crv4.FleetCommitmentWrite)
	}
	receipt, err := view.SubmitRawAndWatchFinalizedRuntime(ctx, encoded, executionRuntime)
	if err != nil {
		return nil, err
	}
	return self.commitmentWrite(ctx, chain, netuid, key.PublicKey(), hash, receipt)
}

// Registration receives the same exact approval as fleet publication. A zero
// block requests a fresh finalized checkpoint, not an unpinned latest read.
func (self *fleetMainnetRuntimeAuthority) nativeAdmission(chain *crv4.Chain) func(context.Context, types.Hash) error {
	if self == nil {
		return nil
	}
	return func(ctx context.Context, block types.Hash) error {
		if block == (types.Hash{}) {
			var err error
			block, err = crv4.FinalizedHeadContext(ctx, chain)
			if err != nil {
				return err
			}
		}
		_, err := self.authenticateAt(ctx, chain, block)
		return err
	}
}
