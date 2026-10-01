//go:build linux || darwin

// Bounded admission observations authenticate public activation artifacts and
// one fresh operator response. They create no producer, key or durable state.
package validator

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/protocol"
)

const productionBootstrapMaximumOperators = 16
const productionBootstrapMaximumContextBytes = 64 * 1024
const productionBootstrapMaximumResponseBytes = 256 * 1024

// The caller supplies an independently observed current native boundary and
// the original approved validator identity, never fields from an API reply.
type ProductionBootstrapNativePoint struct {
	Block  uint64   `json:"block"`
	Hash   [32]byte `json:"hash"`
	Epoch  uint64   `json:"epoch"`
	Hotkey [32]byte `json:"hotkey"`
}

// The admission owner binds this EVM point to its observed native header.
// A later finalized head cannot silently replace the contract snapshot.
type ProductionBootstrapEvmPoint struct {
	Block uint64   `json:"block"`
	Hash  [32]byte `json:"hash"`
}

// A response is verified before hashing; a digest alone grants no authority.
// This proves endpoint/key responsiveness, not worker health or proof replay.
type ProductionBootstrapOperatorObservation struct {
	NoId                      uint64 `json:"no_id"`
	ActivationHash            string `json:"activation_hash"`
	PublishedBlock            uint64 `json:"published_block"`
	ClientId                  string `json:"client_id"`
	ClientKey                 string `json:"client_key"`
	ClientKeyGeneration       uint64 `json:"client_key_generation"`
	ClientKeyRegistrationHash string `json:"client_key_registration_hash"`
	ClientKeyResponseHash     string `json:"client_key_response_hash"`
	ObservationNonce          string `json:"observation_nonce"`
	RootSigner                string `json:"root_signer"`
}

// Separate native and EVM clocks remain explicit. Every operator belongs to
// this same finalized EVM boundary; errors discard the complete projection.
type ProductionBootstrapObservation struct {
	ConfigHash      string                                   `json:"config_hash"`
	DeploymentId    string                                   `json:"deployment_id"`
	ValidatorId     uint64                                   `json:"validator_id"`
	Native          ProductionBootstrapNativePoint           `json:"native"`
	EvmBlock        uint64                                   `json:"evm_block"`
	EvmHash         string                                   `json:"evm_hash"`
	SettlementEpoch uint64                                   `json:"settlement_epoch"`
	Operators       []ProductionBootstrapOperatorObservation `json:"operators"`
}

// Borrows the exact independently pinned config bytes. Only existing client
// JWTs and content-addressed public evidence are read; no seed, network JWT,
// refresh/registration endpoint, signing command or producer is invoked.
// The signed config selects RPC/API routes. The finite caller deadline is
// clamped to two minutes, and the full census is admitted before any network.
func ObserveProductionBootstrapOperators(ctx context.Context, path string, raw []byte, native ProductionBootstrapNativePoint, evm ProductionBootstrapEvmPoint) (result *ProductionBootstrapObservation, resultErr error) {
	if ctx == nil {
		return nil, errors.New("production bootstrap observation context is absent")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	inspection, err := InspectProductionBootstrapConfig(ctx, path, raw)
	if err != nil {
		return nil, err
	}
	approval := inspection.Approval
	if approval.Production == nil || !slices.Contains(approval.Production.ValidatorHotkeys, native.Hotkey) ||
		native.Block < approval.ValidFromNativeBlock || native.Block > approval.ValidThroughNativeBlock ||
		native.Epoch < approval.FirstNativeEpoch || native.Epoch > approval.Production.ValidThroughNativeEpoch || native.Hash == ([32]byte{}) || evm.Block == 0 || evm.Hash == ([32]byte{}) {
		return nil, errors.New("production bootstrap observation differs from its approved native scope")
	}
	// Inspection above authenticates these same bytes, without returning a
	// producer authority capsule. This second decode performs no file access.
	cfg, err := decodeReleaseConfigDocument(path, raw)
	if err != nil {
		return nil, err
	}
	cfg.Coordinator, cfg.SettlementVault = strings.ToLower(cfg.Coordinator), strings.ToLower(cfg.SettlementVault)
	inputs, err := readProductionBootstrapPublicInputs(ctx, cfg, native.Hotkey)
	if err != nil {
		return nil, err
	}
	chain, err := DialReleaseChainContext(ctx, cfg.RPC, common.HexToAddress(cfg.Coordinator))
	if err != nil {
		return nil, err
	}
	defer chain.Close()
	result, err = observeProductionBootstrapOperators(ctx, cfg, inputs, native, evm, chain)
	if err != nil {
		return nil, err
	}
	result.ConfigHash = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	return result, nil
}

// Only public evidence is retained in memory. Loading all bounded members
// first prevents a late malformed member from causing earlier network calls.
type productionBootstrapPublicInput struct {
	operator OperatorConfig
	context  ReleaseEvidenceV2ActivationContext
}

// The original signed config independently pins context and signature bytes.
// History and secret files are deliberately outside this narrow proof scope.
func readProductionBootstrapPublicInputs(ctx context.Context, cfg *ReleaseConfig, hotkey [32]byte) ([]productionBootstrapPublicInput, error) {
	if ctx == nil || cfg == nil || len(cfg.Operators) == 0 || len(cfg.Operators) > productionBootstrapMaximumOperators || len(cfg.EvidenceV2.Operators) != len(cfg.Operators) || hotkey == ([32]byte{}) {
		return nil, errors.New("production bootstrap operator census is absent or exceeds the bounded profile")
	}
	operators := make(map[uint64]OperatorConfig, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		if _, exists := operators[operator.NoID]; exists || operator.NoID == 0 {
			return nil, errors.New("production bootstrap duplicate operator")
		}
		operators[operator.NoID] = operator
	}
	inputs := make([]productionBootstrapPublicInput, 0, len(operators))
	for _, files := range cfg.EvidenceV2.Operators {
		operator, exists := operators[files.NoID]
		if !exists {
			return nil, errors.New("production bootstrap evidence census differs")
		}
		delete(operators, files.NoID)
		maximum := min(cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes, uint64(productionBootstrapMaximumContextBytes))
		encoded, err := ReadReleaseEvidenceV2File(ctx, files.Context, maximum)
		if err != nil {
			return nil, err
		}
		configured, err := decodeReleaseEvidenceV2ActivationContext(encoded, maximum)
		if err != nil {
			return nil, err
		}
		if err := validateReleaseMeasurementInputV2Context(cfg, files.NoID, configured.InitialCut); err != nil {
			return nil, err
		}
		if configured.Activation.Hotkey != hotkey {
			return nil, errors.New("production bootstrap activation hotkey differs from original validator")
		}
		encoded, err = ReadReleaseEvidenceV2File(ctx, files.Activation, uint64(protocol.ValidatorEvidenceActivationPayloadSize))
		if err != nil {
			return nil, err
		}
		candidate, err := protocol.DecodeValidatorEvidenceActivationPayload(encoded)
		if err != nil {
			return nil, err
		}
		vpkSignature, err := ReadReleaseEvidenceV2File(ctx, files.VPKSignature, ed25519.SignatureSize)
		if err != nil {
			return nil, err
		}
		hotkeySignature, err := ReadReleaseEvidenceV2File(ctx, files.HotkeySignature, 64)
		if err != nil {
			return nil, err
		}
		if err := candidate.Verify(configured.Activation, vpkSignature, hotkeySignature); err != nil {
			return nil, err
		}
		inputs = append(inputs, productionBootstrapPublicInput{operator: operator, context: configured})
	}
	return inputs, ctx.Err()
}

// This real reader uses the existing canonical historical-root verifier. It
// never accepts a config artifact signer or API-supplied root as authority.
func observeProductionBootstrapOperators(ctx context.Context, cfg *ReleaseConfig, inputs []productionBootstrapPublicInput, native ProductionBootstrapNativePoint, evm ProductionBootstrapEvmPoint, chain *ChainClient) (result *ProductionBootstrapObservation, resultErr error) {
	if ctx == nil || cfg == nil || chain == nil || chain.chainId == nil || !chain.chainId.IsUint64() || chain.chainId.Uint64() != cfg.ChainID || len(inputs) == 0 || len(inputs) > productionBootstrapMaximumOperators || evm.Block == 0 || evm.Hash == ([32]byte{}) {
		return nil, errors.New("production bootstrap chain or complete census differs")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	finalizedBlock, finalizedHash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	block, hash := evm.Block, evm.Hash
	if finalizedBlock < block || finalizedBlock == block && finalizedHash != hash {
		return nil, errors.New("production bootstrap mapped EVM point is not finalized")
	}
	epochBytes, err := chain.ethCallAtHashContext(ctx, chain.contractAddr, chain.coordinator.PackCurrentEpoch(), block, hash)
	if err != nil {
		return nil, err
	}
	if err := canonicalReleaseDecisionV2View("currentEpoch", epochBytes); err != nil {
		return nil, err
	}
	epoch, err := chain.coordinator.UnpackCurrentEpoch(epochBytes)
	if err != nil || epoch == nil || !epoch.IsUint64() {
		return nil, errors.Join(errors.New("production bootstrap settlement epoch differs"), err)
	}
	result = &ProductionBootstrapObservation{DeploymentId: cfg.DeploymentID, ValidatorId: cfg.ValidatorID, Native: native, EvmBlock: block, EvmHash: attemptHex32(hash), SettlementEpoch: epoch.Uint64()}
	for _, input := range inputs {
		configured := input.context
		publication, err := chain.ValidatorEvidenceActivationAtHashContext(ctx, common.Address(configured.Journal), configured.RuntimeHash, configured.Activation, block, hash)
		if err != nil {
			return nil, err
		}
		if publication.PublishedBlock > configured.InitialCut.Boundary.EVMBlock {
			return nil, errors.New("production bootstrap activation publication follows its initial cut")
		}
		observation, err := observeProductionBootstrapClientKey(ctx, chain, cfg, input, native, protocol.ClientKeyEffectiveBoundary{Epoch: epoch.Uint64(), Block: block, Hash: hash})
		if err != nil {
			return nil, fmt.Errorf("production bootstrap operator %d: %w", input.operator.NoID, err)
		}
		digest, err := configured.Activation.Digest()
		if err != nil {
			return nil, err
		}
		observation.ActivationHash, observation.PublishedBlock = attemptHex32(digest), publication.PublishedBlock
		result.Operators = append(result.Operators, observation)
	}
	// The immutable hash cache cannot prove the height is still canonical.
	// Re-read the actual mapping after the last API body has closed.
	var confirmed *chainRPCBlock
	if err := chain.client.Client().CallContext(ctx, &confirmed, "eth_getBlockByNumber", hexutil.EncodeUint64(block), false); err != nil {
		return nil, err
	}
	confirmedBlock, confirmedHash, err := confirmed.identity()
	if err != nil || confirmedBlock != block || confirmedHash != hash {
		return nil, errors.Join(errors.New("production bootstrap EVM anchor changed during observation"), err)
	}
	return result, nil
}

// Existing credentials are read through the private bounded descriptor owner.
// A fresh nonce is mandatory even when observing the same chain boundary twice.
func observeProductionBootstrapClientKey(ctx context.Context, chain *ChainClient, cfg *ReleaseConfig, input productionBootstrapPublicInput, native ProductionBootstrapNativePoint, boundary protocol.ClientKeyEffectiveBoundary) (result ProductionBootstrapOperatorObservation, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ProductionBootstrapOperatorObservation{}
		}
	}()
	credential, err := ReadReleaseEvidenceV2SetupFile(ctx, input.operator.ClientJWTFile, 16*1024)
	if err != nil {
		return result, errors.Join(errors.New("production bootstrap existing private client credential is unavailable"), err)
	}
	token := strings.TrimSpace(string(credential))
	clientId, err := clientauth.ClientIdFromJwt(token)
	if err != nil {
		return result, errors.New("production bootstrap client credential has no client identity")
	}
	domain := protocol.ClientKeyHistoryDomain{ChainID: cfg.ChainID, GenesisHash: common.HexToHash(cfg.GenesisHash), Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), SettlementVault: common.HexToAddress(cfg.SettlementVault), DeploymentIDHash: sha256.Sum256([]byte(cfg.DeploymentID)), PolicyHash: common.HexToHash(cfg.PolicyHash), NoID: input.operator.NoID}
	request := protocol.ClientKeyObservationRequest{ClientID: [16]byte(clientId), ValidatorHotkey: native.Hotkey, NativeBlock: native.Block, NativeHash: native.Hash, NativeEpoch: native.Epoch, DecisionBoundary: boundary}
	if _, err := rand.Read(request.Nonce[:]); err != nil {
		return result, err
	}
	reader, err := NewHTTPClientKeyHistoryReader(input.operator.APIURL, func() string { return token })
	if err != nil {
		return result, err
	}
	// This finite admission owns its transport; it leaves no default shared
	// idle pool or renewable API worker behind after the response closes.
	transport := &http.Transport{DisableKeepAlives: true, MaxConnsPerHost: 1, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	reader.client.Transport = transport
	defer transport.CloseIdleConnections()
	encoded, err := reader.Read(ctx, request, productionBootstrapMaximumResponseBytes)
	if err != nil {
		return result, err
	}
	registration, err := verifyReservedReleaseClientKeyCaptureV2(ctx, chain, encoded, productionBootstrapMaximumResponseBytes, domain, request, false)
	if err != nil {
		return result, err
	}
	if !registration.Present || registration.PublicKey != input.context.Activation.VPK {
		return result, errors.New("production bootstrap current client key differs from signed activation")
	}
	registrationHash, err := registration.ContentHash()
	if err != nil {
		return result, err
	}
	// Registration can predate a root rotation. Report the already verified
	// observation signer at the current boundary, not the historical signer.
	response, err := protocol.DecodeClientKeyHistoryResponse(encoded, productionBootstrapMaximumResponseBytes)
	if err != nil {
		return result, err
	}
	envelope, err := protocol.DecodeClientKeyEvidence(response.Observation, domain, protocol.ClientKeyObservationEvidenceKind)
	if err != nil {
		return result, err
	}
	observation, err := protocol.DecodeClientKeyObservation(envelope.Payload)
	if err != nil {
		return result, err
	}
	return ProductionBootstrapOperatorObservation{NoId: input.operator.NoID, ClientId: clientId.String(), ClientKey: attemptHex32(registration.PublicKey), ClientKeyGeneration: registration.Generation, ClientKeyRegistrationHash: attemptHex32(registrationHash), ClientKeyResponseHash: attemptHex32(sha256.Sum256(encoded)), ObservationNonce: attemptHex32(request.Nonce), RootSigner: strings.ToLower(observation.Signer.Hex())}, nil
}
