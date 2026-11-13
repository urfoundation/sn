//go:build linux || darwin

package validator

// The stopped recovery command uses existing configured keys, historical policy
// and original-client refresh. It never creates keys, clients, ledgers or trails.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

type ValidatorEvidenceConsentRecoveryManagedV2Settings struct {
	// ShutdownTimeout bounds joining the optional managed API client after repair.
	ShutdownTimeout time.Duration
}

func DefaultValidatorEvidenceConsentRecoveryManagedV2Settings() ValidatorEvidenceConsentRecoveryManagedV2Settings {
	return ValidatorEvidenceConsentRecoveryManagedV2Settings{ShutdownTimeout: 10 * time.Second}
}

// Decode the very bytes whose independent pin was checked, through the normal
// complete production loader. No second file read can change its authority.
func LoadValidatorEvidenceConsentRecoveryConfig(path, expectedSha256 string) (*ReleaseConfig, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(expectedSha256) != 64 || expectedSha256 != strings.ToLower(expectedSha256) {
		return nil, errors.New("consent recovery requires an independently pinned absolute configuration")
	}
	abs, raw, err := readReleaseConfigFile(path)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != expectedSha256 {
		return nil, errors.New("managed recovery configuration differs from its independent pin")
	}
	return decodeReleaseConfigBytesMode(abs, raw, releaseConfigLoadMode{})
}

// The original activation and cut contexts are separate public owner inputs;
// today's rendered activation must never substitute for the historical header.
func validateConsentRecoveryManagedV2Source(cfg *ReleaseConfig, header protocol.ValidatorEvidenceHeader, activation ReleaseEvidenceV2ActivationContext, expected AttemptCutV2Context, scratch string, keys map[byte]ed25519.PublicKey) (OperatorConfig, protocol.Policy, error) {
	var empty OperatorConfig
	if cfg == nil || len(cfg.Operators) != 1 || len(cfg.EvidenceV2.Operators) != 1 || len(keys) == 0 || !filepath.IsAbs(scratch) || filepath.Clean(scratch) != scratch || filepath.Dir(scratch) == scratch {
		return empty, protocol.Policy{}, errors.New("consent recovery requires the complete single managed operator and fresh replay namespace")
	}
	if err := header.Validate(); err != nil {
		return empty, protocol.Policy{}, err
	}
	if _, err := activation.CanonicalJSON(cfg.EvidenceV2.Bounds.MaxControlBytes); err != nil {
		return empty, protocol.Policy{}, err
	}
	if err := expected.Validate(); err != nil {
		return empty, protocol.Policy{}, err
	}
	domain, err := activation.Activation.EvidenceDomain()
	if err != nil {
		return empty, protocol.Policy{}, err
	}
	op := cfg.Operators[0]
	identity := expected.Identity
	if header.Kind != protocol.ValidatorEvidenceClosedCensus || header.Subject != (protocol.ValidatorEvidenceSubject{}) || domain != header.Domain || domain != expected.Activation.Domain || activation.Activation.Hotkey != header.Hotkey || activation.Activation.VPK != header.VPK || activation.Activation.NoID != header.NoID ||
		op.NoID != header.NoID || cfg.EvidenceV2.Operators[0].NoID != header.NoID || activation.InitialCut.Identity != identity || identity.NoID != header.NoID || identity.ValidatorVPK != attemptHex32(header.VPK) || expected.Activation.Hotkey != header.Hotkey || expected.Boundary.SettlementEpoch != header.Epoch || expected.Boundary.EVMBlock != header.BoundaryBlock || expected.Boundary.EVMBlockHash != attemptHex32(header.BoundaryHash) ||
		identity.DeploymentID != cfg.DeploymentID || identity.ValidatorID != cfg.ValidatorID || identity.ChainID != cfg.ChainID || identity.GenesisHash != strings.ToLower(cfg.GenesisHash) || identity.Netuid != cfg.Netuid || domain.Coordinator != [20]byte(common.HexToAddress(cfg.Coordinator)) || domain.SettlementVault != [20]byte(common.HexToAddress(cfg.SettlementVault)) {
		return empty, protocol.Policy{}, errors.New("managed recovery source differs from the independently pinned complete original header")
	}
	if consentRecoveryPathsOverlap(scratch, cfg.StateDir) || consentRecoveryPathsOverlap(scratch, op.StateDir) {
		return empty, protocol.Policy{}, errors.New("recovery scratch overlaps retained managed state")
	}
	for _, key := range keys {
		if len(key) != ed25519.PublicKeySize {
			return empty, protocol.Policy{}, errors.New("recovery public server-key census contains an invalid key")
		}
	}
	policy, err := ReleasePolicyForHash(cfg, attemptHex32(header.Domain.PolicyHash))
	if err != nil {
		return empty, protocol.Policy{}, err
	}
	return op, policy, nil
}

func consentRecoveryPathsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+string(filepath.Separator)) || strings.HasPrefix(right, left+string(filepath.Separator))
}

// No private input is accepted from argv. The ordinary no-create seed readers
// bind the same managed client VPK and hotkey that run the configured validator.
func OpenValidatorEvidenceConsentRecoveryV2(ctx context.Context, cfg *ReleaseConfig, header protocol.ValidatorEvidenceHeader, activation ReleaseEvidenceV2ActivationContext, expected AttemptCutV2Context, scratch string, keys map[byte]ed25519.PublicKey) (result ValidatorEvidenceConsentRecoveryV2Options, closeManaged func() error, resultErr error) {
	settings := DefaultValidatorEvidenceConsentRecoveryManagedV2Settings()
	if ctx == nil {
		return result, nil, errors.New("managed consent recovery context is absent")
	}
	if err := ctx.Err(); err != nil {
		return result, nil, err
	}
	op, policy, err := validateConsentRecoveryManagedV2Source(cfg, header, activation, expected, scratch, keys)
	if err != nil {
		return result, nil, err
	}
	chain, err := DialReleaseChainContext(ctx, cfg.RPC, common.HexToAddress(cfg.Coordinator))
	if err != nil {
		return result, nil, err
	}
	ownedCtx, cancel := context.WithCancel(ctx)
	var api *sdk.Api
	var strategy *connect.ClientStrategy
	var upload *releaseAttemptUploadV2
	var initOnce, closeOnce sync.Once
	var initErr, closeErr error
	closeManaged = func() error {
		closeOnce.Do(func() {
			cancel()
			initOnce.Do(func() { initErr = errors.New("managed recovery upload owner is closed") })
			if upload != nil {
				upload.close()
			}
			if api != nil {
				shutdown, stop := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
				closeErr = api.CloseAndWait(shutdown)
				stop()
			}
			if strategy != nil {
				strategy.Close()
			}
			chain.Close()
		})
		return closeErr
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, closeManaged())
			result, closeManaged = ValidatorEvidenceConsentRecoveryV2Options{}, nil
		}
	}()
	block, hash, err := chain.FinalizedBlockContext(ownedCtx)
	if err != nil {
		return result, closeManaged, err
	}
	epoch := new(big.Int).SetUint64(header.Epoch)
	start, err := chain.ReleaseEpochStartBlockAtHashContext(ownedCtx, block, hash, epoch)
	if err != nil {
		return result, closeManaged, err
	}
	end, err := chain.ReleaseEpochEndBlockAtHashContext(ownedCtx, block, hash, epoch)
	if err != nil {
		return result, closeManaged, err
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: header.Epoch, StartBlock: start, EndBlock: end, FinalizedBlock: block}
	if err := header.ValidateAt(header.Domain, window); err != nil {
		return result, closeManaged, err
	}
	seed, err := loadClientSeed(op.ClientKeySeedFile)
	if err != nil {
		return result, closeManaged, err
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	if err := attemptCutV2PrivateKey(privateKey, header.VPK[:]); err != nil {
		return result, closeManaged, err
	}
	hotkeySeed, err := crv4.LoadSeedFile(cfg.HotkeySeedFile)
	if err != nil {
		return result, closeManaged, err
	}
	hotkey, err := crv4.KeypairFromSeed(hotkeySeed)
	if err != nil || hotkey.PublicKey() != header.Hotkey {
		return result, closeManaged, errors.Join(errors.New("managed original hotkey does not match expected consent"), err)
	}
	signer, err := newValidatorAttemptUploadSigner(activation.Activation, op.NoID, cfg.EvidenceV2.UploadIntentSeconds, privateKey)
	if err != nil {
		return result, closeManaged, err
	}
	reader, err := newHttpAttemptStreamV2Reader(op.APIURL, cfg.EvidenceV2.Bounds.Cut, max(attemptStreamV2MetadataBytes(cfg.EvidenceV2.Bounds.Cut), cfg.EvidenceV2.Bounds.MaxTransitionBytes))
	if err != nil {
		return result, closeManaged, err
	}
	// Refresh only the existing normal client, lazily on the first protected
	// metadata write after core replay, authority checks and durable PREPARED.
	writeMetadata := func(operation context.Context, contentHash string, raw []byte) error {
		if observed, err := reader.ReadMetadata(operation, contentHash, uint64(len(raw))); err == nil {
			if !bytes.Equal(observed, raw) {
				return errors.New("existing recovery metadata differs from exact prepared bytes")
			}
			return nil
		}
		initOnce.Do(func() {
			strategy = connect.NewClientStrategy(ownedCtx, connect.DefaultClientStrategySettings())
			api = sdk.NewApi(ownedCtx, strategy, op.APIURL)
			endpoint, err := api.NetworkClientRegistrationEndpoint()
			if err != nil {
				initErr = err
				return
			}
			scope := clientauth.RegistrationScope{Endpoint: endpoint, DeploymentId: cfg.DeploymentID, ChainId: cfg.ChainID, GenesisHash: strings.ToLower(cfg.GenesisHash), Netuid: cfg.Netuid, ValidatorId: cfg.ValidatorID, OperatorNoId: op.NoID, ClientKey: "0x" + hex.EncodeToString(privateKey.Public().(ed25519.PublicKey))}
			if _, _, err := clientauth.LoadOrRegisterClientJwt(operation, api, op.NetworkJWTFile, op.ClientJWTFile, fmt.Sprintf("validator-%d no-%d", cfg.ValidatorID, op.NoID), scope, false); err != nil {
				initErr = err
				return
			}
			upload, initErr = newReleaseAttemptUploadV2(ownedCtx, op, cfg.EvidenceV2.Bounds, api.GetByJwt)
		})
		if initErr != nil {
			return initErr
		}
		return upload.writeWithSigner(operation, "metadata", contentHash, raw, signer)
	}
	noStreamWrites := func(context.Context, string, []byte) error {
		return errors.New("consent recovery may publish metadata only")
	}
	bounds := cfg.EvidenceV2.Bounds
	operator := AttemptSettlementV2OperatorOptions{Expected: expected, Policy: policy, Bounds: bounds.Cut, Measurement: AttemptCutV2MeasurementOptions{ExpectedConfig: ReleaseStatsConfig{AMin: policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis}, MaxProviders: bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes, MaxFleetPrefixes: bounds.MaxFleetPrefixes, Replay: AttemptCutV2ReplayOptions{Bounds: bounds.Replay, ScratchDirectory: scratch, ServerKeys: keys, ReadMetadata: reader.ReadMetadata, OpenData: reader.OpenData, origin: reader.endpoint.String()}}}
	result = ValidatorEvidenceConsentRecoveryV2Options{StateDir: cfg.StateDir, OperatorStateDir: op.StateDir, ExpectedHeader: header, ActivationContext: activation, Chain: chain, Census: ValidatorEvidenceCensusV2Options{Settlement: AttemptSettlementV2Options{Operators: map[uint64]AttemptSettlementV2OperatorOptions{op.NoID: operator}, MaxParticipants: bounds.MaxParticipants, MaxTransitionBytes: bounds.MaxTransitionBytes, MaxClosureBytes: bounds.MaxClosureBytes}, Window: window, PrivateKeys: map[uint64]ed25519.PrivateKey{op.NoID: privateKey}, Hotkey: hotkey, Replicas: []AttemptCutV2Replica{{Origin: op.APIURL, WriteMetadata: writeMetadata, WriteRecords: noStreamWrites, WriteProofs: noStreamWrites}}}}
	return result, closeManaged, nil
}
