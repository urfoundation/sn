package main

// final_semantic_public_rpc.go implements the secretless public archive reader
// used by the external FINAL.md verifier. It deliberately accepts the already
// authenticated public deployment manifest instead of ResolvedConfig: a clean
// checkout must never fall back to stale local Operational* endpoints.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcregistry "github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	gsrpcparser "github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpccodec "github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/vedhavyas/go-subkey/v2"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

type PublicFinalSemanticChainReader struct {
	evidence              *FinalSemanticEvidence
	evidenceURI           string
	operatorOrigins       []FinalOperatorEvidenceOrigin
	manifestHash          string
	canonicalSubstrateRPC string
	canonicalEVMRPC       string
	native                *gsrpc.SubstrateAPI
	evm                   *rpc.Client
	evmRetry              finalSemanticRPCRetryPolicy
	runtimeVersion        runtimeVersionIdentity
	runtimeCodeHash       string
	runtimeMetadataHash   string
}

var _ FinalSemanticChainReader = (*PublicFinalSemanticChainReader)(nil)

// NewPublicFinalSemanticChainReader constructs a reader from an authenticated
// deployment manifest. evidenceURI is the immutable deployment-manifest
// discovery URI under its signed transport profile, not the later semantic object URI (which would create a
// content-hash cycle).
func NewPublicFinalSemanticChainReader(ctx context.Context, public *PublicDeploymentManifest, evidence *FinalSemanticEvidence, evidenceURI string) (*PublicFinalSemanticChainReader, error) {
	transport, err := canonicalFinalSemanticRPCTransport(public, finalSemanticDefaultEVMRequestsPerMinute, finalSemanticDefaultSubstrateRequestsPerSecond)
	if err != nil {
		return nil, err
	}
	if evidence == nil || evidence.PublicVerification == nil {
		return nil, errors.New("sealed final semantic evidence does not contain authenticated operator origins")
	}
	return newPublicFinalSemanticChainReaderWithTransport(ctx, public, evidence, evidenceURI, evidence.PublicVerification.OperatorEvidenceOrigins, transport)
}

func newPublicFinalSemanticChainReaderWithTransport(ctx context.Context, public *PublicDeploymentManifest, evidence *FinalSemanticEvidence, evidenceURI string, origins []FinalOperatorEvidenceOrigin, transport finalSemanticRPCTransport) (*PublicFinalSemanticChainReader, error) {
	if ctx == nil || public == nil || evidence == nil {
		return nil, errors.New("public final semantic reader context is incomplete")
	}
	if err := verifyFinalSemanticEvidence(evidence, false); err != nil {
		return nil, err
	}
	if err := verifyFinalPublicEndpoint("Substrate", public.SubstrateRPC, "wss", "https"); err != nil {
		return nil, err
	}
	if err := verifyFinalPublicEndpoint("EVM", public.EVMRPC, "https", "wss"); err != nil {
		return nil, err
	}
	if err := validateFinalSemanticRPCTransport(public, transport); err != nil {
		return nil, err
	}
	if err := validatePublicCampaignOperatorOrigins(public); err != nil {
		return nil, fmt.Errorf("authenticated public manifest operator transport: %w", err)
	}
	if err := validatePublishedRuntimeIdentityShape(public); err != nil {
		return nil, fmt.Errorf("authenticated public manifest runtime identity: %w", err)
	}
	transportProfile, err := effectivePublicEvidenceTransportProfile(public)
	if err != nil {
		return nil, fmt.Errorf("authenticated public manifest evidence transport: %w", err)
	}
	if err := verifyFinalEvidenceURI("evidence discovery", evidenceURI, transportProfile, public.ChainID, public.GenesisHash); err != nil {
		return nil, err
	}
	if err := validateFinalOperatorEvidenceOrigins(origins, evidenceURI, transportProfile, public.ChainID, public.GenesisHash); err != nil {
		return nil, err
	}
	if len(public.Operators) != len(origins) {
		return nil, errors.New("authenticated public manifest does not name exactly two evidence operators")
	}
	for index, origin := range origins {
		operator := public.Operators[index]
		manifestOrigin, manifestErr := publicEvidenceOrigin(origin.ManifestURI, transportProfile, public.ChainID, public.GenesisHash)
		operatorOrigin, operatorErr := publicEvidenceOrigin(operator.APIURL, transportProfile, public.ChainID, public.GenesisHash)
		if manifestErr != nil || operatorErr != nil || operator.NoID != origin.OperatorNoID || manifestOrigin != operatorOrigin {
			return nil, errors.Join(manifestErr, operatorErr, fmt.Errorf("operator %d deployment-manifest origin does not match the authenticated operator directory", origin.OperatorNoID))
		}
	}
	allowedOrigins, err := campaignArtifactAllowedOrigins(public, "")
	if err != nil {
		return nil, err
	}
	if err := validateCampaignArtifactOrigin(evidenceURI, allowedOrigins); err != nil {
		return nil, fmt.Errorf("evidence discovery origin: %w", err)
	}
	if public.Schema != "urnetwork-sim-public-deployment-v1" || public.Contracts == nil || public.DeploymentID != evidence.DeploymentID || public.ChainID != evidence.ChainID || public.Netuid != evidence.Netuid || !strings.EqualFold(public.GenesisHash, evidence.GenesisHash) || public.ConfigHash != evidence.ConfigHash || !strings.EqualFold(public.PolicyHash, evidence.PolicyHash) || (public.PlanHash != "" && public.PlanHash != evidence.PlanHash) {
		return nil, errors.New("authenticated public manifest does not match final semantic identity")
	}
	if !finalManifestContractsMatch(public, &evidence.Deployment) {
		return nil, errors.New("authenticated public manifest contract identity does not match final semantic evidence")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manifestHash, err := canonicalHashHex(public)
	if err != nil {
		return nil, fmt.Errorf("hash authenticated public manifest: %w", err)
	}
	retryPolicy := defaultFinalSemanticRPCRetryPolicy()
	native, genesis, err := dialFinalSemanticSubstrate(ctx, transport, retryPolicy)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(genesis.Hex(), evidence.GenesisHash) {
		native.Client.Close()
		return nil, fmt.Errorf("public Substrate genesis %s, want %s", genesis.Hex(), evidence.GenesisHash)
	}
	eth, err := dialEVMClient(ctx, transport.dialEVMRPC, transport.evmRequestsPerMinute)
	if err != nil {
		native.Client.Close()
		return nil, fmt.Errorf("dial public EVM RPC: %w", err)
	}
	evm := eth.Client()
	reader := &PublicFinalSemanticChainReader{
		evidence: evidence, evidenceURI: evidenceURI, operatorOrigins: append([]FinalOperatorEvidenceOrigin(nil), origins...), manifestHash: manifestHash,
		canonicalSubstrateRPC: transport.canonicalSubstrateRPC, canonicalEVMRPC: transport.canonicalEVMRPC,
		native: native, evm: evm, evmRetry: retryPolicy,
		runtimeVersion: runtimeVersionIdentity{
			SpecName: "node-subtensor", SpecVersion: public.RuntimeSpec,
			TransactionVersion: public.TransactionVersion, StateVersion: public.StateVersion,
		},
		runtimeCodeHash: strings.ToLower(public.RuntimeCodeHash), runtimeMetadataHash: strings.ToLower(public.RuntimeMetadataHash),
	}
	var chainID string
	if err := retryFinalSemanticRPCCall(ctx, nil, retryPolicy, func(attemptCtx context.Context) error {
		chainID = ""
		return evm.CallContext(attemptCtx, &chainID, "eth_chainId")
	}); err != nil {
		reader.Close()
		return nil, fmt.Errorf("read public EVM chain id: %w", err)
	}
	gotChainID, err := hexutil.DecodeUint64(chainID)
	if err != nil || gotChainID != evidence.ChainID {
		reader.Close()
		return nil, fmt.Errorf("public EVM chain id %q, want %d", chainID, evidence.ChainID)
	}
	return reader, nil
}

func finalManifestContractsMatch(public *PublicDeploymentManifest, evidence *FinalContractDeploymentEvidence) bool {
	if public == nil || public.Contracts == nil || evidence == nil {
		return false
	}
	manifest := public.Contracts
	activeImplementation := manifest.CoordinatorImplementation
	activeRuntimeHash := ""
	// Release manifests retain the immutable deployment record and publish the
	// subsequently approved active UUPS implementation separately.
	// Final semantic evidence names the implementation observed in ERC1967.
	if public.CoordinatorUpgrade.Implementation != (common.Address{}) {
		activeImplementation = public.CoordinatorUpgrade.Implementation
		activeRuntimeHash = public.CoordinatorUpgrade.RuntimeCodeHash
	} else {
		for address, runtimeHash := range manifest.RuntimeHashes {
			if strings.EqualFold(address, activeImplementation.Hex()) {
				activeRuntimeHash = runtimeHash
				break
			}
		}
	}
	return strings.EqualFold(manifest.CoordinatorProxy.Hex(), evidence.CoordinatorProxy) &&
		strings.EqualFold(activeImplementation.Hex(), evidence.CoordinatorImplementation) &&
		(activeRuntimeHash == "" || strings.EqualFold(activeRuntimeHash, evidence.ImplementationCodeHash)) &&
		strings.EqualFold(manifest.SettlementVault.Hex(), evidence.SettlementVault) &&
		strings.EqualFold(manifest.ReserveSink.Hex(), evidence.ReserveSink)
}

func (self *PublicFinalSemanticChainReader) Close() error {
	if self == nil {
		return nil
	}
	if self.native != nil && self.native.Client != nil {
		self.native.Client.Close()
	}
	if self.evm != nil {
		self.evm.Close()
	}
	return nil
}

func (self *PublicFinalSemanticChainReader) Endpoints() (string, string, string) {
	if self == nil {
		return "", "", ""
	}
	return self.canonicalSubstrateRPC, self.canonicalEVMRPC, self.evidenceURI
}

func (self *PublicFinalSemanticChainReader) PublicManifestHash() string {
	if self == nil {
		return ""
	}
	return self.manifestHash
}

func (self *PublicFinalSemanticChainReader) OperatorEvidenceOrigins() []FinalOperatorEvidenceOrigin {
	if self == nil {
		return nil
	}
	return append([]FinalOperatorEvidenceOrigin(nil), self.operatorOrigins...)
}

func (self *PublicFinalSemanticChainReader) substrateRaw(ctx context.Context, head ChainHead, method string, args ...any) (json.RawMessage, FinalRPCExchange, error) {
	if self == nil || self.native == nil || self.native.Client == nil {
		return nil, FinalRPCExchange{}, errors.New("public Substrate reader is closed")
	}
	params, err := json.Marshal(args)
	if err != nil {
		return nil, FinalRPCExchange{}, err
	}
	var result json.RawMessage
	if err := self.native.Client.CallContext(ctx, &result, method, args...); err != nil {
		return nil, FinalRPCExchange{}, fmt.Errorf("%s: %w", method, err)
	}
	if len(result) == 0 || !json.Valid(result) {
		return nil, FinalRPCExchange{}, fmt.Errorf("%s returned invalid JSON", method)
	}
	exchange := FinalRPCExchange{Chain: "substrate", Method: method, Params: params, PinnedHead: head, Result: append(json.RawMessage(nil), result...)}
	return result, exchange, nil
}

func (self *PublicFinalSemanticChainReader) evmRaw(ctx context.Context, head ChainHead, method string, args ...any) (json.RawMessage, FinalRPCExchange, error) {
	if self == nil || self.evm == nil {
		return nil, FinalRPCExchange{}, errors.New("public EVM reader is closed")
	}
	params, err := json.Marshal(args)
	if err != nil {
		return nil, FinalRPCExchange{}, err
	}
	var result json.RawMessage
	if err := retryFinalSemanticRPCCall(ctx, nil, self.evmRetry, func(attemptCtx context.Context) error {
		result = nil
		return self.evm.CallContext(attemptCtx, &result, method, args...)
	}); err != nil {
		return nil, FinalRPCExchange{}, fmt.Errorf("%s: %w", method, err)
	}
	if len(result) == 0 || !json.Valid(result) {
		return nil, FinalRPCExchange{}, fmt.Errorf("%s returned invalid JSON", method)
	}
	exchange := FinalRPCExchange{Chain: "evm", Method: method, Params: params, PinnedHead: head, Result: append(json.RawMessage(nil), result...)}
	return result, exchange, nil
}

func finalDecodeRPCString(method string, raw json.RawMessage) (string, error) {
	var value string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || value == "" {
		return "", fmt.Errorf("%s returned an empty value", method)
	}
	return value, nil
}

func finalDecodeBlockNumber(label, encoded string) (uint64, error) {
	value, err := hexutil.DecodeUint64(encoded)
	if err != nil {
		return 0, fmt.Errorf("decode %s block number %q: %w", label, encoded, err)
	}
	return value, nil
}

func (self *PublicFinalSemanticChainReader) CanonicalSubstrateHead(ctx context.Context, head ChainHead) ([]FinalRPCExchange, error) {
	if err := verifyFinalHead("Substrate checkpoint", head); err != nil {
		return nil, err
	}
	// The moving finalized tip is a live monotonicity guard only. Embedding it
	// would make an otherwise valid historical replay differ as the chain
	// advances. The immutable transcript below contains only target-pinned
	// calls (plus the immutable genesis identity).
	finalizedRaw, _, err := self.substrateRaw(ctx, head, "chain_getFinalizedHead")
	if err != nil {
		return nil, err
	}
	finalizedHash, err := finalDecodeRPCString("chain_getFinalizedHead", finalizedRaw)
	if err != nil {
		return nil, err
	}
	finalizedHeaderRaw, _, err := self.substrateRaw(ctx, head, "chain_getHeader", finalizedHash)
	if err != nil {
		return nil, err
	}
	finalizedNumber, _, err := finalSubstrateHeader(finalizedHeaderRaw)
	if err != nil {
		return nil, err
	}
	if finalizedNumber < head.Number {
		return nil, fmt.Errorf("Substrate checkpoint %d is ahead of finalized head %d", head.Number, finalizedNumber)
	}
	exchanges := make([]FinalRPCExchange, 0, 3)
	blockHashRaw, exchange, err := self.substrateRaw(ctx, head, "chain_getBlockHash", head.Number)
	if err != nil {
		return nil, err
	}
	exchanges = append(exchanges, exchange)
	blockHash, err := finalDecodeRPCString("chain_getBlockHash", blockHashRaw)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(blockHash, head.Hash) {
		return nil, fmt.Errorf("Substrate canonical block %d hash %s, want %s", head.Number, blockHash, head.Hash)
	}
	headerRaw, exchange, err := self.substrateRaw(ctx, head, "chain_getHeader", head.Hash)
	if err != nil {
		return nil, err
	}
	exchanges = append(exchanges, exchange)
	number, hash, err := finalSubstrateHeader(headerRaw)
	if err != nil {
		return nil, err
	}
	if number != head.Number || (hash != "" && !strings.EqualFold(hash, head.Hash)) {
		return nil, fmt.Errorf("Substrate header does not match checkpoint %d/%s", head.Number, head.Hash)
	}
	if head == self.evidence.NativeStartHead {
		genesisRaw, genesisExchange, genesisErr := self.substrateRaw(ctx, head, "chain_getBlockHash", uint64(0))
		if genesisErr != nil {
			return nil, genesisErr
		}
		exchanges = append(exchanges, genesisExchange)
		genesis, decodeErr := finalDecodeRPCString("chain_getBlockHash(0)", genesisRaw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if !strings.EqualFold(genesis, self.evidence.GenesisHash) {
			return nil, fmt.Errorf("Substrate genesis %s, want %s", genesis, self.evidence.GenesisHash)
		}
	}
	return exchanges, nil
}

func finalSubstrateHeader(raw json.RawMessage) (uint64, string, error) {
	var header struct {
		Number string `json:"number"`
		Hash   string `json:"hash,omitempty"`
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &header) != nil || header.Number == "" {
		return 0, "", errors.New("chain_getHeader returned an invalid header")
	}
	number, err := finalDecodeBlockNumber("Substrate", header.Number)
	return number, header.Hash, err
}

func (self *PublicFinalSemanticChainReader) CanonicalEVMHead(ctx context.Context, head ChainHead) ([]FinalRPCExchange, error) {
	if err := verifyFinalHead("EVM checkpoint", head); err != nil {
		return nil, err
	}
	exchanges := make([]FinalRPCExchange, 0, 3)
	if head == self.evidence.Window.BaselineHead {
		chainRaw, exchange, err := self.evmRaw(ctx, head, "eth_chainId")
		if err != nil {
			return nil, err
		}
		exchanges = append(exchanges, exchange)
		encoded, err := finalDecodeRPCString("eth_chainId", chainRaw)
		if err != nil {
			return nil, err
		}
		chainID, err := hexutil.DecodeUint64(encoded)
		if err != nil || chainID != self.evidence.ChainID {
			return nil, fmt.Errorf("EVM chain id %q, want %d", encoded, self.evidence.ChainID)
		}
	}
	// As on Substrate, the moving finalized tip is checked but deliberately
	// excluded from the immutable target transcript.
	finalizedRaw, _, err := self.evmRaw(ctx, head, "eth_getBlockByNumber", "finalized", false)
	if err != nil {
		return nil, err
	}
	finalized, err := finalEVMBlock(finalizedRaw)
	if err != nil {
		return nil, err
	}
	if finalized.Number < head.Number {
		return nil, fmt.Errorf("EVM checkpoint %d is ahead of finalized head %d", head.Number, finalized.Number)
	}
	targetRaw, exchange, err := self.evmRaw(ctx, head, "eth_getBlockByNumber", hexutil.EncodeUint64(head.Number), false)
	if err != nil {
		return nil, err
	}
	exchanges = append(exchanges, exchange)
	target, err := finalEVMBlock(targetRaw)
	if err != nil {
		return nil, err
	}
	if target.Number != head.Number || !strings.EqualFold(target.Hash, head.Hash) {
		return nil, fmt.Errorf("EVM canonical block does not match checkpoint %d/%s", head.Number, head.Hash)
	}
	return exchanges, nil
}

func finalEVMBlock(raw json.RawMessage) (ChainHead, error) {
	var block struct {
		Number string `json:"number"`
		Hash   string `json:"hash"`
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &block) != nil || block.Number == "" || block.Hash == "" {
		return ChainHead{}, errors.New("eth_getBlockByNumber returned an invalid block")
	}
	number, err := finalDecodeBlockNumber("EVM", block.Number)
	if err != nil {
		return ChainHead{}, err
	}
	return ChainHead{Number: number, Hash: strings.ToLower(block.Hash)}, nil
}

func (self *PublicFinalSemanticChainReader) substrateMetadata(ctx context.Context, head ChainHead) (*gsrpctypes.Metadata, []FinalRPCExchange, error) {
	versionRaw, versionExchange, err := self.substrateRaw(ctx, head, "state_getRuntimeVersion", head.Hash)
	if err != nil {
		return nil, nil, err
	}
	version, err := decodeRuntimeVersionIdentity(versionRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("decode public runtime identity at %s: %w", head.Hash, err)
	}
	codeRaw, codeExchange, err := self.substrateRaw(ctx, head, "state_getStorageHash", "0x3a636f6465", head.Hash)
	if err != nil {
		return nil, nil, err
	}
	var codeHash string
	if err := json.Unmarshal(codeRaw, &codeHash); err != nil {
		return nil, nil, fmt.Errorf("decode public runtime code hash at %s: %w", head.Hash, err)
	}
	metadataRaw, metadataExchange, err := self.substrateRaw(ctx, head, "state_getMetadata", head.Hash)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := finalDecodeRPCString("state_getMetadata", metadataRaw)
	if err != nil {
		return nil, nil, err
	}
	metadata, metadataHash, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("decode public runtime metadata at %s: %w", head.Hash, err)
	}
	if currentErr := validateRuntimeVersionIdentity(version, self.runtimeVersion.SpecVersion, self.runtimeVersion.TransactionVersion, self.runtimeVersion.StateVersion); currentErr == nil {
		if err := validateRuntimeCodeHash(codeHash, self.runtimeCodeHash); err != nil {
			return nil, nil, fmt.Errorf("authenticate public runtime code at %s: %w", head.Hash, err)
		}
		if err := validateRuntimeMetadataHash(metadataHash, self.runtimeMetadataHash); err != nil {
			return nil, nil, fmt.Errorf("authenticate public runtime metadata at %s: %w", head.Hash, err)
		}
		return metadata, []FinalRPCExchange{versionExchange, codeExchange, metadataExchange}, nil
	}

	// Carried setup receipts can precede the v453 campaign start. Authenticate
	// their exact v451/v452 code and metadata identities, but never admit those
	// compatibility runtimes at or after the signed campaign boundary. A v453
	// setup receipt takes the current branch above even when it predates start.
	if self.evidence == nil || head.Number >= self.evidence.NativeStartHead.Number {
		return nil, nil, fmt.Errorf(
			"public runtime at %s is unreviewed %s/%d/%d/%d",
			head.Hash, version.SpecName, version.SpecVersion,
			version.TransactionVersion, version.StateVersion,
		)
	}
	historical, ok := reviewedHistoricalRuntimeArtifact(version)
	if !ok {
		return nil, nil, fmt.Errorf(
			"historical runtime at %s is unreviewed %s/%d/%d/%d",
			head.Hash, version.SpecName, version.SpecVersion,
			version.TransactionVersion, version.StateVersion,
		)
	}
	if err := validateRuntimeCodeHash(codeHash, historical.CodeHash); err != nil {
		return nil, nil, fmt.Errorf("authenticate historical runtime code at %s: %w", head.Hash, err)
	}
	if err := validateRuntimeMetadataHash(metadataHash, historical.MetadataHash); err != nil {
		return nil, nil, fmt.Errorf("authenticate historical runtime metadata at %s: %w", head.Hash, err)
	}
	return metadata, []FinalRPCExchange{versionExchange, codeExchange, metadataExchange}, nil
}

func (self *PublicFinalSemanticChainReader) substrateStorage(ctx context.Context, head ChainHead, metadata *gsrpctypes.Metadata, pallet, item string, out any, required bool, args ...[]byte) (bool, FinalRPCExchange, error) {
	key, err := gsrpctypes.CreateStorageKey(metadata, pallet, item, args...)
	if err != nil {
		return false, FinalRPCExchange{}, fmt.Errorf("create %s.%s key: %w", pallet, item, err)
	}
	raw, exchange, err := self.substrateRaw(ctx, head, "state_getStorage", key.Hex(), head.Hash)
	if err != nil {
		return false, FinalRPCExchange{}, err
	}
	var encoded *string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return false, FinalRPCExchange{}, fmt.Errorf("decode %s.%s storage response: %w", pallet, item, err)
	}
	if encoded != nil && *encoded != "" && *encoded != "0x" {
		decoded, err := gsrpccodec.HexDecodeString(*encoded)
		if err != nil {
			return false, FinalRPCExchange{}, fmt.Errorf("decode %s.%s hex: %w", pallet, item, err)
		}
		if err := gsrpccodec.Decode(decoded, out); err != nil {
			return false, FinalRPCExchange{}, fmt.Errorf("decode %s.%s SCALE: %w", pallet, item, err)
		}
		return true, exchange, nil
	}
	entry, err := metadata.FindStorageEntryMetadata(pallet, item)
	if err != nil {
		return false, FinalRPCExchange{}, err
	}
	present, err := decodeStorageFallback(entry, out)
	if err != nil {
		return false, FinalRPCExchange{}, fmt.Errorf("decode %s.%s fallback: %w", pallet, item, err)
	}
	if required && !present {
		return false, FinalRPCExchange{}, fmt.Errorf("%s.%s storage is absent at %s", pallet, item, head.Hash)
	}
	return present, exchange, nil
}

// substrateStorageExact is reserved for evidence whose distinction between an
// explicitly stored zero and an absent ValueQuery default is security
// relevant. FINAL reward evidence must prove that the exact hotkey's stake was
// returned by the pinned archive node; metadata fallback is therefore not an
// acceptable substitute for a state_getStorage result.
func (self *PublicFinalSemanticChainReader) substrateStorageExact(ctx context.Context, head ChainHead, metadata *gsrpctypes.Metadata, pallet, item string, out any, args ...[]byte) (FinalRPCExchange, error) {
	key, err := gsrpctypes.CreateStorageKey(metadata, pallet, item, args...)
	if err != nil {
		return FinalRPCExchange{}, fmt.Errorf("create %s.%s key: %w", pallet, item, err)
	}
	raw, exchange, err := self.substrateRaw(ctx, head, "state_getStorage", key.Hex(), head.Hash)
	if err != nil {
		return FinalRPCExchange{}, err
	}
	if err := decodeFinalRequiredSubstrateStorage(pallet, item, raw, out); err != nil {
		return FinalRPCExchange{}, err
	}
	return exchange, nil
}

func decodeFinalRequiredSubstrateStorage(pallet, item string, raw json.RawMessage, out any) error {
	var encoded *string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return fmt.Errorf("decode %s.%s storage response: %w", pallet, item, err)
	}
	if encoded == nil || *encoded == "" || *encoded == "0x" {
		return fmt.Errorf("%s.%s storage is absent", pallet, item)
	}
	decoded, err := gsrpccodec.HexDecodeString(*encoded)
	if err != nil {
		return fmt.Errorf("decode %s.%s hex: %w", pallet, item, err)
	}
	if err := gsrpccodec.Decode(decoded, out); err != nil {
		return fmt.Errorf("decode %s.%s SCALE: %w", pallet, item, err)
	}
	return nil
}

func (self *PublicFinalSemanticChainReader) substrateQueryStorageExact(ctx context.Context, head ChainHead, keys []gsrpctypes.StorageKey) (map[string][]byte, FinalRPCExchange, error) {
	if len(keys) == 0 {
		return nil, FinalRPCExchange{}, errors.New("historical Substrate storage query has no keys")
	}
	encodedKeys := make([]string, len(keys))
	want := make(map[string]bool, len(keys))
	for index, key := range keys {
		encodedKeys[index] = strings.ToLower(key.Hex())
		if want[encodedKeys[index]] {
			return nil, FinalRPCExchange{}, errors.New("historical Substrate storage query repeats a key")
		}
		want[encodedKeys[index]] = true
	}
	raw, exchange, err := self.substrateRaw(ctx, head, "state_queryStorageAt", encodedKeys, head.Hash)
	if err != nil {
		return nil, FinalRPCExchange{}, err
	}
	var sets []struct {
		Block   string              `json:"block"`
		Changes [][]json.RawMessage `json:"changes"`
	}
	if err := json.Unmarshal(raw, &sets); err != nil || len(sets) != 1 || !strings.EqualFold(sets[0].Block, head.Hash) {
		return nil, FinalRPCExchange{}, stateMismatchError(err, "state_queryStorageAt returned %d change sets or another block", len(sets))
	}
	result := make(map[string][]byte, len(keys))
	for _, change := range sets[0].Changes {
		if len(change) != 2 {
			return nil, FinalRPCExchange{}, errors.New("state_queryStorageAt returned a malformed change")
		}
		var key string
		var value *string
		if json.Unmarshal(change[0], &key) != nil || json.Unmarshal(change[1], &value) != nil || value == nil {
			return nil, FinalRPCExchange{}, errors.New("state_queryStorageAt returned an absent or malformed value")
		}
		key = strings.ToLower(key)
		if !want[key] || result[key] != nil {
			return nil, FinalRPCExchange{}, fmt.Errorf("state_queryStorageAt returned unexpected or duplicate key %s", key)
		}
		decoded, err := gsrpccodec.HexDecodeString(*value)
		if err != nil {
			return nil, FinalRPCExchange{}, fmt.Errorf("decode state_queryStorageAt value for %s: %w", key, err)
		}
		result[key] = decoded
	}
	if len(result) != len(want) {
		return nil, FinalRPCExchange{}, fmt.Errorf("state_queryStorageAt returned %d values, want %d", len(result), len(want))
	}
	return result, exchange, nil
}

func decodeFinalSubstrateQueryValue(values map[string][]byte, key gsrpctypes.StorageKey, label string, out any) error {
	raw := values[strings.ToLower(key.Hex())]
	if len(raw) == 0 {
		return fmt.Errorf("%s storage is absent", label)
	}
	if err := gsrpccodec.Decode(raw, out); err != nil {
		return fmt.Errorf("decode %s SCALE: %w", label, err)
	}
	return nil
}

func (self *PublicFinalSemanticChainReader) NativePruneSnapshot(ctx context.Context, netuid uint16, head ChainHead) (FleetLifecyclePruneSnapshot, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || netuid != self.evidence.Netuid {
		return FleetLifecyclePruneSnapshot{}, nil, errors.New("public lifecycle prune reader has another netuid")
	}
	metadata, exchanges, err := self.substrateMetadata(ctx, head)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	readU16 := func(item string) (uint16, error) {
		var value gsrpctypes.U16
		exchange, readErr := self.substrateStorageExact(ctx, head, metadata, crv4.PalletName, item, &value, netuidArg(netuid))
		if readErr == nil {
			exchanges = append(exchanges, exchange)
		}
		return uint16(value), readErr
	}
	count, err := readU16("SubnetworkN")
	if err != nil || count == 0 {
		return FleetLifecyclePruneSnapshot{}, nil, stateMismatchError(err, "public lifecycle SubnetworkN is zero or unavailable")
	}
	immunity, err := readU16("ImmunityPeriod")
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	minimumNonImmune, err := readU16("MinNonImmuneUids")
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	maximum, err := readU16("MaxAllowedUids")
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	var owner gsrpctypes.AccountID
	exchange, err := self.substrateStorageExact(ctx, head, metadata, crv4.PalletName, "SubnetOwnerHotkey", &owner, netuidArg(netuid))
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	var emissions []gsrpctypes.U64
	exchange, err = self.substrateStorageExact(ctx, head, metadata, crv4.PalletName, "Emission", &emissions, netuidArg(netuid))
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	exchanges = append(exchanges, exchange)

	hotkeyKeys := make([]gsrpctypes.StorageKey, count)
	for uid := uint16(0); uid < count; uid++ {
		hotkeyKeys[uid], err = gsrpctypes.CreateStorageKey(metadata, crv4.PalletName, "Keys", netuidArg(netuid), netuidArg(uid))
		if err != nil {
			return FleetLifecyclePruneSnapshot{}, nil, err
		}
	}
	hotkeyValues, batchExchange, err := self.substrateQueryStorageExact(ctx, head, hotkeyKeys)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	exchanges = append(exchanges, batchExchange)
	hotkeys := make([]gsrpctypes.AccountID, count)
	secondary := make([]gsrpctypes.StorageKey, 0, 2*int(count))
	ownerKeys, registrationKeys := make([]gsrpctypes.StorageKey, count), make([]gsrpctypes.StorageKey, count)
	for uid := uint16(0); uid < count; uid++ {
		if err := decodeFinalSubstrateQueryValue(hotkeyValues, hotkeyKeys[uid], crv4.PalletName+".Keys", &hotkeys[uid]); err != nil {
			return FleetLifecyclePruneSnapshot{}, nil, err
		}
		ownerKeys[uid], err = gsrpctypes.CreateStorageKey(metadata, crv4.PalletName, "Owner", hotkeys[uid][:])
		if err != nil {
			return FleetLifecyclePruneSnapshot{}, nil, err
		}
		registrationKeys[uid], err = gsrpctypes.CreateStorageKey(metadata, crv4.PalletName, "BlockAtRegistration", netuidArg(netuid), netuidArg(uid))
		if err != nil {
			return FleetLifecyclePruneSnapshot{}, nil, err
		}
		secondary = append(secondary, ownerKeys[uid], registrationKeys[uid])
	}
	secondaryValues, batchExchange, err := self.substrateQueryStorageExact(ctx, head, secondary)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	exchanges = append(exchanges, batchExchange)
	result := FleetLifecyclePruneSnapshot{Head: head, UIDCount: count, MaximumUIDs: maximum, ImmunityPeriodBlocks: immunity, MinimumNonImmuneUIDs: minimumNonImmune, Inputs: make([]FleetLifecyclePruneInput, 0, count)}
	rows := make([]runtime453PruneNeuron, 0, count)
	for uid := uint16(0); uid < count; uid++ {
		var coldkey gsrpctypes.AccountID
		var registered gsrpctypes.U64
		if err := decodeFinalSubstrateQueryValue(secondaryValues, ownerKeys[uid], crv4.PalletName+".Owner", &coldkey); err != nil {
			return FleetLifecyclePruneSnapshot{}, nil, err
		}
		if err := decodeFinalSubstrateQueryValue(secondaryValues, registrationKeys[uid], crv4.PalletName+".BlockAtRegistration", &registered); err != nil {
			return FleetLifecyclePruneSnapshot{}, nil, err
		}
		var hotkey [32]byte
		copy(hotkey[:], hotkeys[uid][:])
		emission := uint64(0)
		if int(uid) < len(emissions) {
			emission = uint64(emissions[uid])
		}
		registrationBlock := uint64(registered)
		age := uint64(0)
		if head.Number >= registrationBlock {
			age = head.Number - registrationBlock
		}
		immune, immortal := age < uint64(immunity), bytes.Equal(hotkey[:], owner[:])
		if !immune && !immortal {
			result.NonImmuneUIDs++
		}
		row := FleetLifecyclePruneInput{UID: uid, Hotkey: "0x" + fmt.Sprintf("%x", hotkey[:]), Coldkey: "0x" + fmt.Sprintf("%x", coldkey[:]), EmissionRao: emission, RegistrationBlock: registrationBlock, Immune: immune, Immortal: immortal}
		result.Inputs = append(result.Inputs, row)
		rows = append(rows, runtime453PruneNeuron{UID: uid, Hotkey: hotkey, EmissionRao: emission, RegistrationBlock: registrationBlock, Immune: immune, Immortal: immortal})
	}
	result.RuntimePruneUID, err = runtime453PruneCandidate(rows, minimumNonImmune)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, nil, err
	}
	return result, exchanges, nil
}

func (self *PublicFinalSemanticChainReader) NativeFleetCommitment(ctx context.Context, netuid uint16, hotkeyEncoded string, head ChainHead) (FinalNativeFleetCommitmentState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || netuid != self.evidence.Netuid {
		return FinalNativeFleetCommitmentState{}, nil, errors.New("public fleet commitment reader has another netuid")
	}
	hotkey, err := decodeHex32("public fleet commitment hotkey", strings.ToLower(hotkeyEncoded))
	if err != nil {
		return FinalNativeFleetCommitmentState{}, nil, err
	}
	metadata, exchanges, err := self.substrateMetadata(ctx, head)
	if err != nil {
		return FinalNativeFleetCommitmentState{}, nil, err
	}
	commitmentKey, err := gsrpctypes.CreateStorageKey(metadata, crv4.CommitmentsPalletName, "CommitmentOf", netuidArg(netuid), hotkey[:])
	if err != nil {
		return FinalNativeFleetCommitmentState{}, nil, err
	}
	lastKey, err := gsrpctypes.CreateStorageKey(metadata, crv4.CommitmentsPalletName, "LastCommitment", netuidArg(netuid), hotkey[:])
	if err != nil {
		return FinalNativeFleetCommitmentState{}, nil, err
	}
	values, exchange, err := self.substrateQueryStorageExact(ctx, head, []gsrpctypes.StorageKey{commitmentKey, lastKey})
	if err != nil {
		return FinalNativeFleetCommitmentState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	commitmentHash, err := crv4.DecodeFleetCommitmentRegistrationV453(values[strings.ToLower(commitmentKey.Hex())])
	if err != nil {
		return FinalNativeFleetCommitmentState{}, nil, err
	}
	lastRaw := values[strings.ToLower(lastKey.Hex())]
	if len(lastRaw) != 4 {
		return FinalNativeFleetCommitmentState{}, nil, fmt.Errorf("Commitments.LastCommitment has %d bytes, want 4", len(lastRaw))
	}
	commitmentBlock := uint64(binary.LittleEndian.Uint32(lastRaw))
	if commitmentBlock == 0 {
		return FinalNativeFleetCommitmentState{}, nil, errors.New("Commitments.LastCommitment is zero")
	}
	return FinalNativeFleetCommitmentState{Hotkey: strings.ToLower(hotkeyEncoded), CommitmentHash: "0x" + fmt.Sprintf("%x", commitmentHash[:]), CommitmentBlock: commitmentBlock, Block: head}, exchanges, nil
}

func finalAccountMatches(expected string, actual []byte) error {
	if strings.HasPrefix(expected, "0x") {
		decoded, err := hexutil.Decode(expected)
		if err != nil || len(decoded) != 32 || !bytes.Equal(decoded, actual) {
			return errors.New("hex account does not match on-chain public key")
		}
		return nil
	}
	_, decoded, err := subkey.SS58Decode(expected)
	if err != nil || len(decoded) != 32 || !bytes.Equal(decoded, actual) {
		return errors.New("SS58 account does not match on-chain public key")
	}
	return nil
}

func (self *PublicFinalSemanticChainReader) expectedUID(uid uint16) (string, string, error) {
	for _, pool := range self.evidence.Pools {
		if pool.UID == uid {
			return pool.Hotkey, pool.Coldkey, nil
		}
	}
	for _, validator := range self.evidence.Validators {
		if validator.UID == uid {
			return validator.Hotkey, validator.Coldkey, nil
		}
	}
	for _, fleet := range self.evidence.HeadFleets {
		if fleet.UID == uid {
			return fleet.Hotkey, fleet.Coldkey, nil
		}
	}
	return "", "", fmt.Errorf("UID %d is absent from final semantic evidence", uid)
}

func (self *PublicFinalSemanticChainReader) expectedRewardHotkey(uid uint16, head ChainHead) (string, error) {
	if self == nil || self.evidence == nil {
		return "", errors.New("native reward identity evidence is unavailable")
	}
	var expected string
	for _, reward := range self.evidence.NativeRewards {
		if reward.UID != uid || (reward.Before != head && reward.After != head) {
			continue
		}
		if expected != "" && expected != reward.Hotkey {
			return "", fmt.Errorf("UID %d has conflicting native reward hotkeys at %d", uid, head.Number)
		}
		expected = reward.Hotkey
	}
	if expected == "" {
		return "", fmt.Errorf("UID %d has no native reward identity at %d/%s", uid, head.Number, head.Hash)
	}
	return expected, nil
}

func (self *PublicFinalSemanticChainReader) NativeUID(ctx context.Context, netuid, uid uint16, head ChainHead) (FinalNativeUIDState, []FinalRPCExchange, error) {
	if netuid != self.evidence.Netuid {
		return FinalNativeUIDState{}, nil, fmt.Errorf("netuid %d, want %d", netuid, self.evidence.Netuid)
	}
	metadata, exchanges, err := self.substrateMetadata(ctx, head)
	if err != nil {
		return FinalNativeUIDState{}, nil, err
	}
	var hotkey, coldkey gsrpctypes.AccountID
	present, exchange, err := self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Keys", &hotkey, true, netuidArg(netuid), netuidArg(uid))
	if err != nil {
		return FinalNativeUIDState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	if !present {
		return FinalNativeUIDState{}, nil, fmt.Errorf("UID %d is not registered", uid)
	}
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Owner", &coldkey, true, hotkey[:])
	if err != nil {
		return FinalNativeUIDState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	var stake gsrpctypes.U64
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "TotalHotkeyAlpha", &stake, false, hotkey[:], netuidArg(netuid))
	if err != nil {
		return FinalNativeUIDState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	var permits []bool
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "ValidatorPermit", &permits, false, netuidArg(netuid))
	if err != nil {
		return FinalNativeUIDState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	var trusts []gsrpctypes.U16
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "ValidatorTrust", &trusts, false, netuidArg(netuid))
	if err != nil {
		return FinalNativeUIDState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	expectedHotkey, expectedColdkey, err := self.expectedUID(uid)
	if err != nil {
		return FinalNativeUIDState{}, nil, err
	}
	if err := finalAccountMatches(expectedHotkey, hotkey[:]); err != nil {
		return FinalNativeUIDState{}, nil, fmt.Errorf("UID %d hotkey: %w", uid, err)
	}
	if err := finalAccountMatches(expectedColdkey, coldkey[:]); err != nil {
		return FinalNativeUIDState{}, nil, fmt.Errorf("UID %d coldkey: %w", uid, err)
	}
	state := FinalNativeUIDState{UID: uid, Hotkey: expectedHotkey, Coldkey: expectedColdkey, Registered: true, StakeRao: new(big.Int).SetUint64(uint64(stake)).String()}
	if int(uid) < len(permits) {
		state.ValidatorPermit = permits[uid]
	}
	if int(uid) < len(trusts) {
		state.ValidatorTrustU16 = uint16(trusts[uid])
	}
	return state, exchanges, nil
}

func (self *PublicFinalSemanticChainReader) NativeEvent(ctx context.Context, receipt FinalNativeReceipt, event string) (FinalNativeEventState, []FinalRPCExchange, error) {
	if event == "" {
		return FinalNativeEventState{}, nil, errors.New("native event kind is empty")
	}
	operation := map[string]string{
		"registration": finalNativeOperationRegistration,
		"commit":       finalNativeOperationCommit,
		"reveal":       finalNativeOperationReveal,
		"application":  finalNativeOperationApplication,
	}[event]
	if operation != "" {
		if err := verifyFinalNativeReceiptCall(receipt, operation); err != nil {
			return FinalNativeEventState{}, nil, err
		}
	}
	raw, exchange, err := self.substrateRaw(ctx, receipt.Block, "chain_getBlock", receipt.Block.Hash)
	if err != nil {
		return FinalNativeEventState{}, nil, err
	}
	exchanges := []FinalRPCExchange{exchange}
	var block struct {
		Block struct {
			Extrinsics []string `json:"extrinsics"`
		} `json:"block"`
	}
	if json.Unmarshal(raw, &block) != nil || block.Block.Extrinsics == nil {
		return FinalNativeEventState{}, nil, errors.New("chain_getBlock returned an invalid block body")
	}
	if receipt.ExtrinsicHash != "" {
		var extrinsicIndex uint32
		var extrinsicBytes []byte
		found := false
		for index, encoded := range block.Block.Extrinsics {
			decoded, decodeErr := gsrpccodec.HexDecodeString(encoded)
			if decodeErr != nil {
				return FinalNativeEventState{}, nil, decodeErr
			}
			digest := blake2b.Sum256(decoded)
			if strings.EqualFold("0x"+fmt.Sprintf("%x", digest[:]), receipt.ExtrinsicHash) {
				extrinsicIndex = uint32(index)
				extrinsicBytes = decoded
				found = true
				break
			}
		}
		if !found {
			return FinalNativeEventState{}, nil, fmt.Errorf("extrinsic %s is absent from finalized block %s", receipt.ExtrinsicHash, receipt.Block.Hash)
		}
		metadata, metadataExchanges, err := self.substrateMetadata(ctx, receipt.Block)
		if err != nil {
			return FinalNativeEventState{}, nil, err
		}
		exchanges = append(exchanges, metadataExchanges...)
		key, err := gsrpctypes.CreateStorageKey(metadata, "System", "Events")
		if err != nil {
			return FinalNativeEventState{}, nil, err
		}
		eventsRaw, eventsExchange, err := self.substrateRaw(ctx, receipt.Block, "state_getStorage", key.Hex(), receipt.Block.Hash)
		if err != nil {
			return FinalNativeEventState{}, nil, err
		}
		exchanges = append(exchanges, eventsExchange)
		if bytes.Equal(bytes.TrimSpace(eventsRaw), []byte("null")) {
			return FinalNativeEventState{}, nil, errors.New("finalized System.Events storage is absent")
		}
		if err := verifyFinalSemanticSubstrateDispatch(metadata, eventsRaw, extrinsicIndex, receipt); err != nil {
			return FinalNativeEventState{}, nil, err
		}
		if operation == finalNativeOperationRegistration || operation == finalNativeOperationCommit {
			decoded, err := finalDecodeNativeExtrinsic(metadata, extrinsicBytes)
			if err != nil {
				return FinalNativeEventState{}, nil, fmt.Errorf("decode exact native %s extrinsic: %w", event, err)
			}
			if err := verifyFinalNativeExtrinsicIdentity(decoded, *receipt.Call); err != nil {
				return FinalNativeEventState{}, nil, fmt.Errorf("verify exact native %s extrinsic: %w", event, err)
			}
			events, err := finalDecodeSemanticSubstrateEvents(metadata, eventsRaw, receipt)
			if err != nil {
				return FinalNativeEventState{}, nil, err
			}
			if err := verifyFinalNativeOperationEvents(events, &extrinsicIndex, *receipt.Call); err != nil {
				return FinalNativeEventState{}, nil, err
			}
		}
	} else if event != "reveal" && event != "application" {
		return FinalNativeEventState{}, nil, fmt.Errorf("native %s evidence has no extrinsic", event)
	} else if event == "reveal" {
		metadata, metadataExchanges, err := self.substrateMetadata(ctx, receipt.Block)
		if err != nil {
			return FinalNativeEventState{}, nil, err
		}
		exchanges = append(exchanges, metadataExchanges...)
		key, err := gsrpctypes.CreateStorageKey(metadata, "System", "Events")
		if err != nil {
			return FinalNativeEventState{}, nil, err
		}
		eventsRaw, eventsExchange, err := self.substrateRaw(ctx, receipt.Block, "state_getStorage", key.Hex(), receipt.Block.Hash)
		if err != nil {
			return FinalNativeEventState{}, nil, err
		}
		exchanges = append(exchanges, eventsExchange)
		events, err := finalDecodeSemanticSubstrateEvents(metadata, eventsRaw, receipt)
		if err != nil {
			return FinalNativeEventState{}, nil, err
		}
		if err := verifyFinalNativeOperationEvents(events, nil, *receipt.Call); err != nil {
			return FinalNativeEventState{}, nil, err
		}
	}
	return FinalNativeEventState{ExtrinsicHash: receipt.ExtrinsicHash, Block: receipt.Block, Success: true, Event: event}, exchanges, nil
}

// verifyFinalSemanticSubstrateDispatch decodes the already transcripted,
// block-pinned System.Events value. Keeping dispatch verification here avoids
// the contextless duplicate reads performed by crv4.VerifyFinalizedExtrinsic
// while retaining its fail-closed success/failure rule.
func verifyFinalSemanticSubstrateDispatch(metadata *gsrpctypes.Metadata, eventsRaw json.RawMessage, extrinsicIndex uint32, receipt FinalNativeReceipt) error {
	events, err := finalDecodeSemanticSubstrateEvents(metadata, eventsRaw, receipt)
	if err != nil {
		return err
	}
	success := false
	for _, record := range events {
		if record == nil || record.Phase == nil || !record.Phase.IsApplyExtrinsic || record.Phase.AsApplyExtrinsic != extrinsicIndex {
			continue
		}
		switch record.Name {
		case "System.ExtrinsicFailed", "ExtrinsicFailed":
			return fmt.Errorf("extrinsic %s failed dispatch in finalized block %s", receipt.ExtrinsicHash, receipt.Block.Hash)
		case "System.ExtrinsicSuccess", "ExtrinsicSuccess":
			success = true
		}
	}
	if !success {
		return fmt.Errorf("extrinsic %s has no System.ExtrinsicSuccess event in finalized block %s", receipt.ExtrinsicHash, receipt.Block.Hash)
	}
	return nil
}

// Decodes the exact already-transcripted event bytes with authenticated
// historical metadata and rejects malformed or trailing parser input.
func finalDecodeSemanticSubstrateEvents(metadata *gsrpctypes.Metadata, eventsRaw json.RawMessage, receipt FinalNativeReceipt) ([]*gsrpcparser.Event, error) {
	if metadata == nil {
		return nil, errors.New("finalized Substrate metadata is missing")
	}
	encoded, err := finalDecodeRPCString("System.Events", eventsRaw)
	if err != nil {
		return nil, err
	}
	decoded, err := gsrpccodec.HexDecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode finalized System.Events hex: %w", err)
	}
	eventRegistry, err := gsrpcregistry.NewFactory().CreateEventRegistry(metadata)
	if err != nil {
		return nil, fmt.Errorf("initialize finalized event decoder: %w", err)
	}
	storage := gsrpctypes.StorageDataRaw(decoded)
	events, err := gsrpcparser.NewEventParser().ParseEvents(eventRegistry, &storage)
	if err != nil {
		return nil, fmt.Errorf("decode finalized events at %s: %w", receipt.Block.Hash, err)
	}
	return events, nil
}

func (self *PublicFinalSemanticChainReader) NativeWeights(ctx context.Context, netuid, validatorUID uint16, head ChainHead) (FinalNativeWeightState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || netuid != self.evidence.Netuid {
		return FinalNativeWeightState{}, nil, fmt.Errorf("native applied weights netuid %d differs from semantic evidence", netuid)
	}
	metadata, exchanges, err := self.substrateMetadata(ctx, head)
	if err != nil {
		return FinalNativeWeightState{}, nil, err
	}
	var hotkey gsrpctypes.AccountID
	present, exchange, err := self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Keys", &hotkey, true, netuidArg(netuid), netuidArg(validatorUID))
	if err != nil {
		return FinalNativeWeightState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	if !present {
		return FinalNativeWeightState{}, nil, fmt.Errorf("validator UID %d has no Keys owner at applied head", validatorUID)
	}
	var lastUpdates []gsrpctypes.U64
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "LastUpdate", &lastUpdates, false, netuidArg(netuid))
	if err != nil {
		return FinalNativeWeightState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	if int(validatorUID) >= len(lastUpdates) || lastUpdates[validatorUID] == 0 {
		return FinalNativeWeightState{}, nil, fmt.Errorf("validator UID %d has no fresh LastUpdate at applied head", validatorUID)
	}
	var row []crv4.WeightPair
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Weights", &row, false, netuidArg(netuid), netuidArg(validatorUID))
	if err != nil {
		return FinalNativeWeightState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	state := FinalNativeWeightState{ValidatorUID: validatorUID, ValidatorHotkey: "0x" + hex.EncodeToString(hotkey[:]), LastUpdate: uint64(lastUpdates[validatorUID]), Block: head, UIDs: make([]uint16, len(row)), Values: make([]uint16, len(row))}
	for i, pair := range row {
		state.UIDs[i], state.Values[i] = uint16(pair.UID), uint16(pair.Value)
	}
	return state, exchanges, nil
}

func (self *PublicFinalSemanticChainReader) NativeReward(ctx context.Context, netuid, uid uint16, head ChainHead) (FinalNativeRewardState, []FinalRPCExchange, error) {
	if netuid != self.evidence.Netuid {
		return FinalNativeRewardState{}, nil, fmt.Errorf("netuid %d, want %d", netuid, self.evidence.Netuid)
	}
	metadata, exchanges, err := self.substrateMetadata(ctx, head)
	if err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	var hotkey gsrpctypes.AccountID
	_, exchange, err := self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Keys", &hotkey, true, netuidArg(netuid), netuidArg(uid))
	if err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	expectedHotkey, err := self.expectedRewardHotkey(uid, head)
	if err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	if err := validateFinalNativeRewardHotkey(uid, expectedHotkey, hotkey[:]); err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	var stake gsrpctypes.U64
	exchange, err = self.substrateStorageExact(ctx, head, metadata, crv4.PalletName, "TotalHotkeyAlpha", &stake, hotkey[:], netuidArg(netuid))
	if err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	var emissions []gsrpctypes.U64
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Emission", &emissions, false, netuidArg(netuid))
	if err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	var incentives, dividends []gsrpctypes.U16
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Incentive", &incentives, false, netuidArg(netuid))
	if err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	_, exchange, err = self.substrateStorage(ctx, head, metadata, crv4.PalletName, "Dividends", &dividends, false, netuidArg(netuid))
	if err != nil {
		return FinalNativeRewardState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	if int(uid) >= len(emissions) || int(uid) >= len(incentives) || int(uid) >= len(dividends) || len(emissions) != len(incentives) || len(emissions) != len(dividends) {
		return FinalNativeRewardState{}, nil, fmt.Errorf("native reward vectors do not contain UID %d consistently", uid)
	}
	return FinalNativeRewardState{
		UID: uid, StakeRao: new(big.Int).SetUint64(uint64(stake)).String(), EmissionRao: new(big.Int).SetUint64(uint64(emissions[uid])).String(),
		IncentiveU16: uint16(incentives[uid]), DividendsU16: uint16(dividends[uid]), Block: head,
	}, exchanges, nil
}

func (self *PublicFinalSemanticChainReader) NativeOwnerStake(ctx context.Context, hotkeyEncoded, coldkeyEncoded string, head ChainHead) (FinalNativeOwnerStakeState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil {
		return FinalNativeOwnerStakeState{}, nil, errors.New("public native owner-stake reader is unavailable")
	}
	hotkey, err := decodeHex32("native owner stake hotkey", strings.ToLower(hotkeyEncoded))
	if err != nil {
		return FinalNativeOwnerStakeState{}, nil, err
	}
	coldkey, err := decodeHex32("native owner stake coldkey", strings.ToLower(coldkeyEncoded))
	if err != nil {
		return FinalNativeOwnerStakeState{}, nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		return FinalNativeOwnerStakeState{}, nil, err
	}
	data, err := parsed.Pack("getStake", hotkey, coldkey, new(big.Int).SetUint64(uint64(self.evidence.Netuid)))
	if err != nil {
		return FinalNativeOwnerStakeState{}, nil, err
	}
	out, exchanges, err := self.evmCall(ctx, head, stakingPrecompileAddress.Hex(), data)
	if err != nil {
		return FinalNativeOwnerStakeState{}, nil, err
	}
	values, err := parsed.Unpack("getStake", out)
	if err != nil || len(values) != 1 {
		return FinalNativeOwnerStakeState{}, nil, stateMismatchError(err, "decode native owner getStake returned %d values", len(values))
	}
	stake, ok := values[0].(*big.Int)
	if !ok || stake.Sign() < 0 {
		return FinalNativeOwnerStakeState{}, nil, fmt.Errorf("native owner getStake returned %T or a negative value", values[0])
	}
	return FinalNativeOwnerStakeState{
		HotkeyPublicKey: strings.ToLower(fmt.Sprintf("0x%x", hotkey[:])), ColdkeyPublicKey: strings.ToLower(fmt.Sprintf("0x%x", coldkey[:])),
		StakeRao: stake.String(), Block: head,
	}, exchanges, nil
}

func validateFinalNativeRewardHotkey(uid uint16, expected string, actual []byte) error {
	if err := finalAccountMatches(expected, actual); err != nil {
		return fmt.Errorf("UID %d hotkey: %w", uid, err)
	}
	return nil
}

func (self *PublicFinalSemanticChainReader) EVMReceipt(ctx context.Context, receipt FinalEVMReceipt) (FinalEVMReceiptState, []FinalRPCExchange, error) {
	raw, exchange, err := self.evmRaw(ctx, receipt.Block, "eth_getTransactionReceipt", receipt.TransactionHash)
	if err != nil {
		return FinalEVMReceiptState{}, nil, err
	}
	var wire struct {
		TransactionHash string          `json:"transactionHash"`
		BlockHash       string          `json:"blockHash"`
		BlockNumber     string          `json:"blockNumber"`
		Status          string          `json:"status"`
		Logs            json.RawMessage `json:"logs"`
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &wire) != nil || wire.TransactionHash == "" || wire.BlockNumber == "" || len(wire.Logs) == 0 {
		return FinalEVMReceiptState{}, nil, errors.New("eth_getTransactionReceipt returned an invalid receipt")
	}
	number, err := finalDecodeBlockNumber("receipt", wire.BlockNumber)
	if err != nil {
		return FinalEVMReceiptState{}, nil, err
	}
	status := "failed"
	if wire.Status == "0x1" {
		status = "success"
	} else if wire.Status != "0x0" {
		return FinalEVMReceiptState{}, nil, fmt.Errorf("receipt %s has invalid status %q", receipt.TransactionHash, wire.Status)
	}
	allowed, err := finalSemanticReleaseContractAddresses(self.evidence)
	if err != nil {
		return FinalEVMReceiptState{}, nil, err
	}
	logs, err := finalCanonicalLogsFromRPC(wire.Logs, allowed)
	if err != nil {
		return FinalEVMReceiptState{}, nil, err
	}
	logsHash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil {
		return FinalEVMReceiptState{}, nil, err
	}
	payload, err := verifyFinalSemanticReceiptPayload(self.evidence, receipt, logs)
	if err != nil {
		return FinalEVMReceiptState{}, nil, fmt.Errorf("decode release receipt %s: %w", receipt.TransactionHash, err)
	}
	return FinalEVMReceiptState{TransactionHash: strings.ToLower(wire.TransactionHash), Block: ChainHead{Number: number, Hash: strings.ToLower(wire.BlockHash)}, Status: status, LogsHash: logsHash, receiptPayload: payload}, []FinalRPCExchange{exchange}, nil
}

// Returns the exact immutable release-contract address census used to filter
// receipts. The batcher belongs here because a generation-two replacement is
// a release-contract mutation even though generic economic receipt decoding
// intentionally does not assign it deposit or payout semantics.
func finalSemanticReleaseContractAddresses(evidence *FinalSemanticEvidence) (map[common.Address]bool, error) {
	if evidence == nil {
		return nil, errors.New("semantic evidence is unavailable for release-contract addresses")
	}
	batcher, err := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
	if err != nil || !common.IsHexAddress(batcher.Address) || common.HexToAddress(batcher.Address) == (common.Address{}) {
		return nil, stateMismatchError(err, "release fleet batcher address is unavailable")
	}
	addresses := map[common.Address]bool{}
	for _, address := range []string{evidence.Deployment.CoordinatorProxy, evidence.Deployment.SettlementVault, evidence.Deployment.ReserveSink, batcher.Address} {
		if !common.IsHexAddress(address) || common.HexToAddress(address) == (common.Address{}) {
			return nil, fmt.Errorf("release contract address %q is invalid", address)
		}
		addresses[common.HexToAddress(address)] = true
	}
	return addresses, nil
}

// Reads a fleet write without routing its coordinator/batcher logs through
// the generic economic event decoder. Both receipt and transaction are bound
// to the same immutable block before the caller compares exact calldata and
// canonical receipt logs to the sealed lineage.
func (self *PublicFinalSemanticChainReader) FleetGenerationEVMWrite(ctx context.Context, write FinalFleetGenerationWriteEvidence) (FinalFleetGenerationEVMWriteState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil {
		return FinalFleetGenerationEVMWriteState{}, nil, errors.New("public fleet generation reader is unavailable")
	}
	receipt := write.Receipt
	if err := verifyFinalEVMReceipt("public fleet generation receipt", receipt, 1, self.evidence.EVMTerminalHead.Number); err != nil {
		return FinalFleetGenerationEVMWriteState{}, nil, err
	}
	allowed, err := finalFleetGenerationWriteReleaseContractAddresses(write)
	if err != nil {
		return FinalFleetGenerationEVMWriteState{}, nil, err
	}
	target, err := finalFleetGenerationWriteTarget(self.evidence, write)
	if err != nil {
		return FinalFleetGenerationEVMWriteState{}, nil, err
	}
	receiptRaw, receiptExchange, err := self.evmRaw(ctx, receipt.Block, "eth_getTransactionReceipt", receipt.TransactionHash)
	if err != nil {
		return FinalFleetGenerationEVMWriteState{}, nil, err
	}
	var receiptWire struct {
		TransactionHash string          `json:"transactionHash"`
		BlockHash       string          `json:"blockHash"`
		BlockNumber     string          `json:"blockNumber"`
		Status          string          `json:"status"`
		Logs            json.RawMessage `json:"logs"`
	}
	if bytes.Equal(bytes.TrimSpace(receiptRaw), []byte("null")) || json.Unmarshal(receiptRaw, &receiptWire) != nil || receiptWire.TransactionHash == "" || receiptWire.BlockHash == "" || receiptWire.BlockNumber == "" || len(receiptWire.Logs) == 0 {
		return FinalFleetGenerationEVMWriteState{}, nil, errors.New("fleet generation receipt is incomplete")
	}
	blockNumber, err := finalDecodeBlockNumber("fleet generation receipt", receiptWire.BlockNumber)
	if err != nil {
		return FinalFleetGenerationEVMWriteState{}, nil, err
	}
	status := ""
	switch receiptWire.Status {
	case "0x1":
		status = "success"
	case "0x0":
		status = "failed"
	default:
		return FinalFleetGenerationEVMWriteState{}, nil, fmt.Errorf("fleet generation receipt %s has invalid status %q", receipt.TransactionHash, receiptWire.Status)
	}
	logs, err := finalCanonicalLogsFromRPC(receiptWire.Logs, allowed)
	if err != nil {
		return FinalFleetGenerationEVMWriteState{}, nil, err
	}
	logsHash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil || !strings.EqualFold(logsHash, receipt.LogsHash) || !strings.EqualFold(receiptWire.TransactionHash, receipt.TransactionHash) || blockNumber != receipt.Block.Number || !strings.EqualFold(receiptWire.BlockHash, receipt.Block.Hash) || status != receipt.Status {
		return FinalFleetGenerationEVMWriteState{}, nil, stateMismatchError(err, "fleet generation receipt differs from sealed coordinates")
	}
	transactionRaw, transactionExchange, err := self.evmRaw(ctx, receipt.Block, "eth_getTransactionByHash", receipt.TransactionHash)
	if err != nil {
		return FinalFleetGenerationEVMWriteState{}, nil, err
	}
	var transactionWire struct {
		Hash        string `json:"hash"`
		BlockHash   string `json:"blockHash"`
		BlockNumber string `json:"blockNumber"`
		To          string `json:"to"`
		Input       string `json:"input"`
	}
	if bytes.Equal(bytes.TrimSpace(transactionRaw), []byte("null")) || json.Unmarshal(transactionRaw, &transactionWire) != nil || transactionWire.Hash == "" || transactionWire.BlockHash == "" || transactionWire.BlockNumber == "" || transactionWire.To == "" || transactionWire.Input == "" {
		return FinalFleetGenerationEVMWriteState{}, nil, errors.New("fleet generation transaction is incomplete")
	}
	transactionBlock, err := finalDecodeBlockNumber("fleet generation transaction", transactionWire.BlockNumber)
	if err != nil || !strings.EqualFold(transactionWire.Hash, receipt.TransactionHash) || transactionBlock != receipt.Block.Number || !strings.EqualFold(transactionWire.BlockHash, receipt.Block.Hash) || !common.IsHexAddress(transactionWire.To) || !allowed[common.HexToAddress(transactionWire.To)] || !strings.EqualFold(transactionWire.To, target) {
		return FinalFleetGenerationEVMWriteState{}, nil, stateMismatchError(err, "fleet generation transaction differs from sealed receipt")
	}
	calldata, err := hexutil.Decode(transactionWire.Input)
	if err != nil || len(calldata) < 4 || hexutil.Encode(calldata) != strings.ToLower(transactionWire.Input) {
		return FinalFleetGenerationEVMWriteState{}, nil, stateMismatchError(err, "fleet generation transaction calldata is malformed")
	}
	return FinalFleetGenerationEVMWriteState{
		TransactionHash: strings.ToLower(transactionWire.Hash), To: strings.ToLower(common.HexToAddress(transactionWire.To).Hex()), Calldata: hexutil.Encode(calldata),
		Block: ChainHead{Number: transactionBlock, Hash: strings.ToLower(transactionWire.BlockHash)}, Status: status, Logs: append([]finalCanonicalEVMLog(nil), logs...),
	}, []FinalRPCExchange{receiptExchange, transactionExchange}, nil
}

// Reads the coordinator's complete historical deposit prefix at one validator
// audit checkpoint. The block-hash selector in evmCall prevents
// a public provider from answering with current state instead of that exact
// immutable snapshot.
func (self *PublicFinalSemanticChainReader) EpochDeposit(ctx context.Context, epoch, noID uint64, head ChainHead) (FinalEpochDepositChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || noID == 0 {
		return FinalEpochDepositChainState{}, nil, errors.New("historical epoch deposit query is incomplete")
	}
	coordinator := stabi.NewSTCoordinator()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackEpochDeposits(new(big.Int).SetUint64(epoch), new(big.Int).SetUint64(noID)))
	if err != nil {
		return FinalEpochDepositChainState{}, nil, err
	}
	amount, err := coordinator.UnpackEpochDeposits(out)
	if err != nil || amount == nil || amount.Sign() < 0 {
		return FinalEpochDepositChainState{}, nil, stateMismatchError(err, "decode historical epoch deposit")
	}
	return FinalEpochDepositChainState{Epoch: epoch, NoID: noID, AmountRao: amount.String(), Block: head}, exchanges, nil
}

// Reads both immutable operator-version cardinality and the version selected
// by the evidence's effective epoch. Keeping both calls at
// the terminal block detects a substituted active signer as well as omitted
// schedule history.
func (self *PublicFinalSemanticChainReader) OperatorVersion(ctx context.Context, noID, epoch uint64, head ChainHead) (FinalOperatorVersionChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || noID == 0 {
		return FinalOperatorVersionChainState{}, nil, errors.New("operator version query is incomplete")
	}
	coordinator := stabi.NewSTCoordinator()
	operatorID := new(big.Int).SetUint64(noID)
	countOut, countExchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackOperatorVersionCount(operatorID))
	if err != nil {
		return FinalOperatorVersionChainState{}, nil, err
	}
	count, err := coordinator.UnpackOperatorVersionCount(countOut)
	if err != nil || count == nil || !count.IsUint64() || count.Sign() <= 0 {
		return FinalOperatorVersionChainState{}, nil, stateMismatchError(err, "decode operator version count")
	}
	versionOut, versionExchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackOperatorAt(operatorID, new(big.Int).SetUint64(epoch)))
	if err != nil {
		return FinalOperatorVersionChainState{}, nil, err
	}
	version, err := coordinator.UnpackOperatorAt(versionOut)
	if err != nil || version.EffectiveEpoch > epoch || version.Coldkey == ([32]byte{}) || version.PoolHotkey == ([32]byte{}) || version.DepositHotkey == ([32]byte{}) || version.DepositSigner == (common.Address{}) || version.RootSigner == (common.Address{}) {
		return FinalOperatorVersionChainState{}, nil, stateMismatchError(err, "decode operator version")
	}
	coldkey, err := ss58.Encode(version.Coldkey, ss58.BittensorPrefix)
	if err != nil {
		return FinalOperatorVersionChainState{}, nil, fmt.Errorf("encode operator coldkey: %w", err)
	}
	poolHotkey, err := ss58.Encode(version.PoolHotkey, ss58.BittensorPrefix)
	if err != nil {
		return FinalOperatorVersionChainState{}, nil, fmt.Errorf("encode operator pool hotkey: %w", err)
	}
	depositHotkey, err := ss58.Encode(version.DepositHotkey, ss58.BittensorPrefix)
	if err != nil {
		return FinalOperatorVersionChainState{}, nil, fmt.Errorf("encode operator deposit hotkey: %w", err)
	}
	return FinalOperatorVersionChainState{
		NoID: noID, VersionCount: count.Uint64(), Coldkey: coldkey, PoolHotkey: poolHotkey, DepositHotkey: depositHotkey,
		DepositSigner: strings.ToLower(version.DepositSigner.Hex()), RootSigner: strings.ToLower(version.RootSigner.Hex()), EffectiveEpoch: version.EffectiveEpoch,
		Active: version.Active, Block: head,
	}, append(countExchanges, versionExchanges...), nil
}

type finalEVMBlockSelector struct {
	BlockHash        string `json:"blockHash"`
	RequireCanonical bool   `json:"requireCanonical"`
}

func (self *PublicFinalSemanticChainReader) evmCall(ctx context.Context, head ChainHead, address string, data []byte) ([]byte, []FinalRPCExchange, error) {
	selector := finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true}
	raw, exchange, err := self.evmRaw(ctx, head, "eth_call", map[string]string{"to": address, "data": hexutil.Encode(data)}, selector)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := finalDecodeRPCString("eth_call", raw)
	if err != nil {
		return nil, nil, err
	}
	decoded, err := hexutil.Decode(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("decode eth_call result: %w", err)
	}
	return decoded, []FinalRPCExchange{exchange}, nil
}

func finalLifecycleClientID(value string) ([16]byte, error) {
	raw, ok := evidenceFixedHex(strings.ToLower(value), 16)
	if !ok {
		return [16]byte{}, fmt.Errorf("fleet lifecycle client id %q is invalid", value)
	}
	var out [16]byte
	copy(out[:], raw)
	if out == ([16]byte{}) {
		return [16]byte{}, errors.New("fleet lifecycle client id is zero")
	}
	return out, nil
}

func finalFleetBindingChainState(clientID [16]byte, active bool, record stabi.STCoordinatorBindingRecord, head ChainHead) FinalFleetBindingChainState {
	return FinalFleetBindingChainState{
		Active: active, ClientID: fleetLifecycleHex16(clientID), FleetID: fleetLifecycleHex(record.FleetId), Hotkey: fleetLifecycleHex(record.Hotkey),
		ClientKey: fleetLifecycleHex(record.ClientKey), CommitmentHash: fleetLifecycleHex(record.CommitmentHash), Generation: record.Generation,
		ValidFromEpoch: record.ValidFromEpoch, ValidToEpoch: record.ValidToEpoch, CleanedAtEpoch: record.CleanedAtEpoch, UID: record.Uid, Cleaned: record.Cleaned, Block: head,
	}
}

func (self *PublicFinalSemanticChainReader) FleetMirror(ctx context.Context, hotkeyEncoded string, head ChainHead) (FinalFleetMirrorChainState, []FinalRPCExchange, error) {
	hotkey, err := decodeHex32("fleet mirror hotkey", strings.ToLower(hotkeyEncoded))
	if err != nil {
		return FinalFleetMirrorChainState{}, nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackMirroredCommitments(hotkey))
	if err != nil {
		return FinalFleetMirrorChainState{}, nil, err
	}
	mirror, err := coordinator.UnpackMirroredCommitments(out)
	if err != nil {
		return FinalFleetMirrorChainState{}, nil, err
	}
	return FinalFleetMirrorChainState{Hotkey: strings.ToLower(hotkeyEncoded), CommitmentHash: fleetLifecycleHex(mirror.CommitmentHash), FinalizedBlock: mirror.FinalizedBlock, FinalizedBlockHash: fleetLifecycleHex(mirror.FinalizedBlockHash), Block: head}, exchanges, nil
}

func (self *PublicFinalSemanticChainReader) FleetBinding(ctx context.Context, clientEncoded string, epoch uint64, head ChainHead) (FinalFleetBindingChainState, []FinalRPCExchange, error) {
	clientID, err := finalLifecycleClientID(clientEncoded)
	if err != nil || epoch == 0 {
		return FinalFleetBindingChainState{}, nil, stateMismatchError(err, "fleet binding lookup epoch is zero or client is invalid")
	}
	coordinator := stabi.NewSTCoordinator()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackBindingAt(clientID, new(big.Int).SetUint64(epoch)))
	if err != nil {
		return FinalFleetBindingChainState{}, nil, err
	}
	binding, err := coordinator.UnpackBindingAt(out)
	if err != nil {
		return FinalFleetBindingChainState{}, nil, err
	}
	return finalFleetBindingChainState(clientID, binding.Active, binding.Record, head), exchanges, nil
}

func (self *PublicFinalSemanticChainReader) FleetBindingRecord(ctx context.Context, clientEncoded string, head ChainHead) (FinalFleetBindingChainState, []FinalRPCExchange, error) {
	clientID, err := finalLifecycleClientID(clientEncoded)
	if err != nil {
		return FinalFleetBindingChainState{}, nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackGetFleetBinding(clientID))
	if err != nil {
		return FinalFleetBindingChainState{}, nil, err
	}
	record, err := coordinator.UnpackGetFleetBinding(out)
	if err != nil {
		return FinalFleetBindingChainState{}, nil, err
	}
	return finalFleetBindingChainState(clientID, !record.Cleaned, record, head), exchanges, nil
}

func (self *PublicFinalSemanticChainReader) FleetMemberCount(ctx context.Context, fleetEncoded string, head ChainHead) (uint64, []FinalRPCExchange, error) {
	fleetID, err := decodeHex32("fleet member-count id", strings.ToLower(fleetEncoded))
	if err != nil {
		return 0, nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackFleetMemberCount(fleetID))
	if err != nil {
		return 0, nil, err
	}
	count, err := coordinator.UnpackFleetMemberCount(out)
	if err != nil || count == nil || !count.IsUint64() {
		return 0, nil, stateMismatchError(err, "fleet member count is not uint64")
	}
	return count.Uint64(), exchanges, nil
}

var _ FinalSemanticLifecycleChainReader = (*PublicFinalSemanticChainReader)(nil)

func (self *PublicFinalSemanticChainReader) FleetLifecycleEvents(ctx context.Context, transactionHash string, head ChainHead) ([]FinalFleetLifecycleEventState, []FinalRPCExchange, error) {
	if _, err := decodeHex32("fleet lifecycle transaction", strings.ToLower(transactionHash)); err != nil {
		return nil, nil, err
	}
	raw, exchange, err := self.evmRaw(ctx, head, "eth_getTransactionReceipt", transactionHash)
	if err != nil {
		return nil, nil, err
	}
	var receipt struct {
		TransactionHash string          `json:"transactionHash"`
		BlockHash       string          `json:"blockHash"`
		BlockNumber     string          `json:"blockNumber"`
		Status          string          `json:"status"`
		Logs            []*ethtypes.Log `json:"logs"`
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &receipt) != nil || !strings.EqualFold(receipt.TransactionHash, transactionHash) || !strings.EqualFold(receipt.BlockHash, head.Hash) || receipt.Status != "0x1" {
		return nil, nil, errors.New("fleet lifecycle receipt is absent, failed, or has another identity")
	}
	number, err := finalDecodeBlockNumber("fleet lifecycle receipt", receipt.BlockNumber)
	if err != nil || number != head.Number {
		return nil, nil, stateMismatchError(err, "fleet lifecycle receipt block=%d, want %d", number, head.Number)
	}
	coordinatorAddress := common.HexToAddress(self.evidence.Deployment.CoordinatorProxy)
	coordinator := stabi.NewSTCoordinator()
	result := make([]FinalFleetLifecycleEventState, 0, len(receipt.Logs))
	for _, log := range receipt.Logs {
		if log == nil || log.Address != coordinatorAddress {
			continue
		}
		base := FinalFleetLifecycleEventState{TransactionHash: strings.ToLower(transactionHash), Block: head}
		if event, unpackErr := coordinator.UnpackCommitmentMirroredEvent(log); unpackErr == nil {
			base.Kind, base.Hotkey, base.CommitmentHash = "commitment-mirrored", fleetLifecycleHex(event.Hotkey), fleetLifecycleHex(event.CommitmentHash)
			base.FinalizedBlock, base.FinalizedBlockHash = event.FinalizedBlock, fleetLifecycleHex(event.FinalizedBlockHash)
			result = append(result, base)
			continue
		}
		if event, unpackErr := coordinator.UnpackFleetBoundEvent(log); unpackErr == nil {
			base.Kind, base.ClientID, base.FleetID, base.Hotkey = "fleet-bound", fleetLifecycleHex16(event.ClientId), fleetLifecycleHex(event.FleetId), fleetLifecycleHex(event.Hotkey)
			base.Generation, base.UID, base.ValidFromEpoch, base.ValidToEpoch = event.Generation, event.Uid, event.ValidFromEpoch, event.ValidToEpoch
			result = append(result, base)
			continue
		}
		if event, unpackErr := coordinator.UnpackFleetBindingCleanedEvent(log); unpackErr == nil {
			base.Kind, base.ClientID, base.CleanedAtEpoch = "fleet-binding-cleaned", fleetLifecycleHex16(event.ClientId), event.CleanedAtEpoch
			result = append(result, base)
		}
	}
	if len(result) == 0 {
		return nil, nil, errors.New("fleet lifecycle receipt has no recognized exact coordinator event")
	}
	return result, []FinalRPCExchange{exchange}, nil
}

func (self *PublicFinalSemanticChainReader) PoolEpoch(ctx context.Context, epoch, noID uint64, head ChainHead) (FinalPoolEpochChainState, []FinalRPCExchange, error) {
	vault := stabi.NewSTSettlementVault()
	data := vault.PackEntitlement(new(big.Int).SetUint64(epoch), new(big.Int).SetUint64(noID))
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.SettlementVault, data)
	if err != nil {
		return FinalPoolEpochChainState{}, nil, err
	}
	entitlement, err := vault.UnpackEntitlement(out)
	if err != nil {
		return FinalPoolEpochChainState{}, nil, fmt.Errorf("decode settlement entitlement: %w", err)
	}
	return FinalPoolEpochChainState{
		Epoch: epoch, NoID: noID, PayoutRoot: "0x" + fmt.Sprintf("%x", entitlement.PayoutRoot[:]), ArtifactHash: "0x" + fmt.Sprintf("%x", entitlement.ArtifactHash[:]),
		FundedRao: entitlement.Funded.String(), TotalRao: entitlement.Total.String(), ClaimedRao: entitlement.Claimed.String(), ExpiryBlock: entitlement.ExpiryBlock, Status: entitlement.Status, Block: head,
	}, exchanges, nil
}

func (self *PublicFinalSemanticChainReader) evmCode(ctx context.Context, head ChainHead, address string) ([]byte, []FinalRPCExchange, error) {
	raw, exchange, err := self.evmRaw(ctx, head, "eth_getCode", address, finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true})
	if err != nil {
		return nil, nil, err
	}
	encoded, err := finalDecodeRPCString("eth_getCode", raw)
	if err != nil {
		return nil, nil, err
	}
	code, err := hexutil.Decode(encoded)
	if err != nil || len(code) == 0 {
		return nil, nil, fmt.Errorf("contract %s has empty or invalid code at %s", address, head.Hash)
	}
	return code, []FinalRPCExchange{exchange}, nil
}

func (self *PublicFinalSemanticChainReader) ContractDeployment(ctx context.Context, head ChainHead) (FinalContractDeploymentState, []FinalRPCExchange, error) {
	deployment := self.evidence.Deployment
	addresses := []string{deployment.CoordinatorProxy, deployment.CoordinatorImplementation, deployment.SettlementVault, deployment.ReserveSink}
	hashes := make([]string, len(addresses))
	exchanges := make([]FinalRPCExchange, 0, 28)
	for i, address := range addresses {
		code, codeExchanges, err := self.evmCode(ctx, head, address)
		if err != nil {
			return FinalContractDeploymentState{}, nil, err
		}
		exchanges = append(exchanges, codeExchanges...)
		hashes[i] = strings.ToLower(crypto.Keccak256Hash(code).Hex())
	}
	slotRaw, exchange, err := self.evmRaw(ctx, head, "eth_getStorageAt", deployment.CoordinatorProxy, deployment.ERC1967ImplementationSlot, finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true})
	if err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	exchanges = append(exchanges, exchange)
	slot, err := finalDecodeRPCString("eth_getStorageAt", slotRaw)
	if err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	slotBytes, err := hexutil.Decode(slot)
	if err != nil || len(slotBytes) != common.HashLength || !bytes.Equal(slotBytes[common.HashLength-common.AddressLength:], common.HexToAddress(deployment.CoordinatorImplementation).Bytes()) {
		return FinalContractDeploymentState{}, nil, errors.New("ERC1967 implementation slot does not match immutable deployment")
	}
	state := FinalContractDeploymentState{
		CoordinatorProxy: deployment.CoordinatorProxy, CoordinatorImplementation: deployment.CoordinatorImplementation,
		SettlementVault: deployment.SettlementVault, ReserveSink: deployment.ReserveSink,
		CoordinatorProxyCodeHash: hashes[0], ImplementationCodeHash: hashes[1], SettlementVaultCodeHash: hashes[2], ReserveSinkCodeHash: hashes[3],
		ObservedImplementationSlot: strings.ToLower(slot), Block: head,
	}
	call := func(label, address string, data []byte, decode func([]byte) error) error {
		out, callExchanges, callErr := self.evmCall(ctx, head, address, data)
		if callErr != nil {
			return fmt.Errorf("read %s: %w", label, callErr)
		}
		exchanges = append(exchanges, callExchanges...)
		if decodeErr := decode(out); decodeErr != nil {
			return fmt.Errorf("decode %s: %w", label, decodeErr)
		}
		return nil
	}
	coordinator := stabi.NewSTCoordinator()
	if err := call("coordinator owner", deployment.CoordinatorProxy, coordinator.PackOwner(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackOwner(out)
		state.GovernanceOwner = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator netuid", deployment.CoordinatorProxy, coordinator.PackNetuid(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackNetuid(out)
		state.CoordinatorNetuid = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator self coldkey", deployment.CoordinatorProxy, coordinator.PackSelfColdkey(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackSelfColdkey(out)
		state.CoordinatorSelfColdkey = fmt.Sprintf("0x%x", value[:])
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator settlement vault", deployment.CoordinatorProxy, coordinator.PackSettlementVault(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackSettlementVault(out)
		state.CoordinatorSettlementVault = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator reserve sink", deployment.CoordinatorProxy, coordinator.PackReserveSink(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackReserveSink(out)
		state.CoordinatorReserveSink = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator guardian", deployment.CoordinatorProxy, coordinator.PackGuardian(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackGuardian(out)
		state.CoordinatorGuardian = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator active guardian", deployment.CoordinatorProxy, coordinator.PackActiveGuardian(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackActiveGuardian(out)
		state.CoordinatorActiveGuardian = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator paused", deployment.CoordinatorProxy, coordinator.PackPaused(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackPaused(out)
		state.CoordinatorPaused = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator commitment oracle", deployment.CoordinatorProxy, coordinator.PackCommitmentOracle(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackCommitmentOracle(out)
		state.CoordinatorCommitmentOracle = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("coordinator active commitment oracle", deployment.CoordinatorProxy, coordinator.PackActiveCommitmentOracle(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackActiveCommitmentOracle(out)
		state.CoordinatorActiveCommitmentOracle = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}

	vault := stabi.NewSTSettlementVault()
	if err := call("vault coordinator", deployment.SettlementVault, vault.PackCoordinator(), func(out []byte) error {
		value, unpackErr := vault.UnpackCoordinator(out)
		state.VaultCoordinator = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("vault netuid", deployment.SettlementVault, vault.PackNetuid(), func(out []byte) error {
		value, unpackErr := vault.UnpackNetuid(out)
		state.VaultNetuid = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("vault self coldkey", deployment.SettlementVault, vault.PackSelfColdkey(), func(out []byte) error {
		value, unpackErr := vault.UnpackSelfColdkey(out)
		state.VaultSelfColdkey = fmt.Sprintf("0x%x", value[:])
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("vault escrow hotkey", deployment.SettlementVault, vault.PackEscrowHotkey(), func(out []byte) error {
		value, unpackErr := vault.UnpackEscrowHotkey(out)
		state.VaultEscrowHotkey = fmt.Sprintf("0x%x", value[:])
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("vault escrow registration", deployment.SettlementVault, vault.PackEscrowRegistered(), func(out []byte) error {
		value, unpackErr := vault.UnpackEscrowRegistered(out)
		state.VaultEscrowRegistered = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("vault minimum claim TTL", deployment.SettlementVault, vault.PackMinimumClaimTTLBlocks(), func(out []byte) error {
		value, unpackErr := vault.UnpackMinimumClaimTTLBlocks(out)
		state.VaultMinimumClaimTTLBlocks = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("vault minimum transfer", deployment.SettlementVault, vault.PackMinimumTransferTaoRao(), func(out []byte) error {
		value, unpackErr := vault.UnpackMinimumTransferTaoRao(out)
		state.VaultMinimumTransferTaoRao = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}

	reserve := stabi.NewSTReserveSink()
	if err := call("reserve recorder", deployment.ReserveSink, reserve.PackRecorder(), func(out []byte) error {
		value, unpackErr := reserve.UnpackRecorder(out)
		state.ReserveRecorder = strings.ToLower(value.Hex())
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("reserve netuid", deployment.ReserveSink, reserve.PackNetuid(), func(out []byte) error {
		value, unpackErr := reserve.UnpackNetuid(out)
		state.ReserveNetuid = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("reserve self coldkey", deployment.ReserveSink, reserve.PackSelfColdkey(), func(out []byte) error {
		value, unpackErr := reserve.UnpackSelfColdkey(out)
		state.ReserveSelfColdkey = fmt.Sprintf("0x%x", value[:])
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if err := call("reserve hotkey", deployment.ReserveSink, reserve.PackReserveHotkey(), func(out []byte) error {
		value, unpackErr := reserve.UnpackReserveHotkey(out)
		state.ReserveHotkey = fmt.Sprintf("0x%x", value[:])
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}

	var policyCount *big.Int
	if err := call("coordinator policy count", deployment.CoordinatorProxy, coordinator.PackPolicyCount(), func(out []byte) error {
		value, unpackErr := coordinator.UnpackPolicyCount(out)
		policyCount = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	if policyCount == nil || !policyCount.IsUint64() || policyCount.Sign() == 0 {
		return FinalContractDeploymentState{}, nil, errors.New("coordinator policy count is invalid")
	}
	lastEpoch := self.evidence.Window.FirstEpoch + self.evidence.Window.EpochCount - 1
	var policy stabi.STCoordinatorPolicySnapshot
	if err := call("coordinator active policy", deployment.CoordinatorProxy, coordinator.PackPolicyAt(new(big.Int).SetUint64(lastEpoch)), func(out []byte) error {
		value, unpackErr := coordinator.UnpackPolicyAt(out)
		policy = value
		return unpackErr
	}); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	state.PolicyHash = "0x" + fmt.Sprintf("%x", policy.PolicyHash[:])
	state.PolicyVersion = policyCount.Uint64()
	state.PolicyEffectiveEpoch = policy.EffectiveEpoch
	state.PolicyEffectiveBlock = policy.EffectiveBlock
	if err := validateFinalContractCustodyState(state, self.evidence.Netuid); err != nil {
		return FinalContractDeploymentState{}, nil, err
	}
	return state, exchanges, nil
}

func validateFinalContractCustodyState(state FinalContractDeploymentState, netuid uint16) error {
	if state.CoordinatorNetuid != netuid || state.VaultNetuid != netuid || state.ReserveNetuid != netuid {
		return fmt.Errorf("contract custody netuids %d/%d/%d do not match deployment netuid %d", state.CoordinatorNetuid, state.VaultNetuid, state.ReserveNetuid, netuid)
	}
	if !strings.EqualFold(state.CoordinatorSettlementVault, state.SettlementVault) || !strings.EqualFold(state.CoordinatorReserveSink, state.ReserveSink) ||
		!strings.EqualFold(state.VaultCoordinator, state.CoordinatorProxy) || !strings.EqualFold(state.ReserveRecorder, state.CoordinatorProxy) {
		return errors.New("contract custody address linkage does not match immutable deployment")
	}
	for _, item := range []struct{ label, value string }{
		{"governance owner", state.GovernanceOwner},
		{"coordinator guardian", state.CoordinatorGuardian},
		{"coordinator active guardian", state.CoordinatorActiveGuardian},
		{"coordinator commitment oracle", state.CoordinatorCommitmentOracle},
		{"coordinator active commitment oracle", state.CoordinatorActiveCommitmentOracle},
	} {
		label, value := item.label, item.value
		if !common.IsHexAddress(value) || common.HexToAddress(value) == (common.Address{}) {
			return fmt.Errorf("%s is an invalid zero address", label)
		}
	}
	for _, item := range []struct {
		label    string
		observed string
		expected [32]byte
	}{
		{"coordinator", state.CoordinatorSelfColdkey, ss58.EvmMirrorPubkey(common.HexToAddress(state.CoordinatorProxy))},
		{"vault", state.VaultSelfColdkey, ss58.EvmMirrorPubkey(common.HexToAddress(state.SettlementVault))},
		{"reserve", state.ReserveSelfColdkey, ss58.EvmMirrorPubkey(common.HexToAddress(state.ReserveSink))},
	} {
		if !strings.EqualFold(item.observed, fmt.Sprintf("0x%x", item.expected[:])) {
			return fmt.Errorf("%s self coldkey does not match its immutable EVM mirror", item.label)
		}
	}
	if state.VaultEscrowHotkey == "0x"+strings.Repeat("0", 64) || state.ReserveHotkey == "0x"+strings.Repeat("0", 64) {
		return errors.New("contract custody hotkey is zero")
	}
	if !state.VaultEscrowRegistered {
		return errors.New("settlement vault escrow hotkey is not registered")
	}
	return nil
}

func (self *PublicFinalSemanticChainReader) SettlementVaultState(ctx context.Context, head ChainHead) (FinalSettlementVaultChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil {
		return FinalSettlementVaultChainState{}, nil, errors.New("public settlement-vault reader is unavailable")
	}
	vault := stabi.NewSTSettlementVault()
	state := FinalSettlementVaultChainState{Block: head}
	exchanges := make([]FinalRPCExchange, 0, 6)
	call := func(label string, data []byte, decode func([]byte) (*big.Int, error), destination *string) error {
		out, callExchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.SettlementVault, data)
		if err != nil {
			return fmt.Errorf("read settlement-vault %s: %w", label, err)
		}
		exchanges = append(exchanges, callExchanges...)
		value, err := decode(out)
		if err != nil || value == nil || value.Sign() < 0 {
			return stateMismatchError(err, "decode settlement-vault %s", label)
		}
		*destination = value.String()
		return nil
	}
	if err := call("total captured", vault.PackTotalCaptured(), vault.UnpackTotalCaptured, &state.TotalCapturedRao); err != nil {
		return FinalSettlementVaultChainState{}, nil, err
	}
	if err := call("total paid", vault.PackTotalPaid(), vault.UnpackTotalPaid, &state.TotalPaidRao); err != nil {
		return FinalSettlementVaultChainState{}, nil, err
	}
	if err := call("escrow accounted", vault.PackEscrowAccounted(), vault.UnpackEscrowAccounted, &state.EscrowAccountedRao); err != nil {
		return FinalSettlementVaultChainState{}, nil, err
	}
	if err := call("pending funding", vault.PackPendingFunding(), vault.UnpackPendingFunding, &state.PendingFundingRao); err != nil {
		return FinalSettlementVaultChainState{}, nil, err
	}
	if err := call("outstanding liability", vault.PackOutstandingLiability(), vault.UnpackOutstandingLiability, &state.OutstandingLiabilityRao); err != nil {
		return FinalSettlementVaultChainState{}, nil, err
	}
	if err := call("live escrow stake", vault.PackLiveEscrowStake(), vault.UnpackLiveEscrowStake, &state.LiveEscrowStakeRao); err != nil {
		return FinalSettlementVaultChainState{}, nil, err
	}
	return state, exchanges, nil
}

func (self *PublicFinalSemanticChainReader) ReserveState(ctx context.Context, head ChainHead) (FinalReserveState, []FinalRPCExchange, error) {
	sink := stabi.NewSTReserveSink()
	principalOut, principalExchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.ReserveSink, sink.PackPrincipal())
	if err != nil {
		return FinalReserveState{}, nil, err
	}
	principal, err := sink.UnpackPrincipal(principalOut)
	if err != nil {
		return FinalReserveState{}, nil, err
	}
	liveOut, liveExchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.ReserveSink, sink.PackLiveStake())
	if err != nil {
		return FinalReserveState{}, nil, err
	}
	live, err := sink.UnpackLiveStake(liveOut)
	if err != nil {
		return FinalReserveState{}, nil, err
	}
	return FinalReserveState{PrincipalRao: principal.String(), LiveStakeRao: live.String(), Block: head}, append(principalExchanges, liveExchanges...), nil
}
