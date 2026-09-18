//go:build linux || darwin

// A live decision retains the exact operator-signed key-history response before
// head assembly. Recovery reads that immutable capture and actual historical
// coordinator versions; it never asks today's API to sign a past observation.
package validator

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
	"golang.org/x/sys/unix"
)

// Inputs are owned values from the real validator/native decision, never an
// observation's proposed hotkey, domain, height or key.
func releaseClientKeyDecisionV2(cfg *ReleaseConfig, noID uint64, hotkey [32]byte, artifact *ReleaseMeasurementArtifact, clientID connect.Id) (protocol.ClientKeyHistoryDomain, protocol.ClientKeyObservationRequest, error) {
	var domain protocol.ClientKeyHistoryDomain
	var request protocol.ClientKeyObservationRequest
	if cfg == nil || artifact == nil || clientID == (connect.Id{}) || hotkey == ([32]byte{}) || noID == 0 {
		return domain, request, errors.New("client-key decision owner is incomplete")
	}
	genesis, err := canonicalAttemptHex32("client-key native genesis", cfg.GenesisHash, false)
	if err != nil {
		return domain, request, err
	}
	policy, err := canonicalAttemptHex32("client-key policy", cfg.PolicyHash, false)
	if err != nil {
		return domain, request, err
	}
	nativeHash, err := canonicalAttemptHex32("client-key native decision hash", artifact.NativeSnapshotHash, false)
	if err != nil {
		return domain, request, err
	}
	evmHash, err := canonicalAttemptHex32("client-key EVM decision hash", artifact.EVMSnapshotHash, false)
	if err != nil {
		return domain, request, err
	}
	domain = protocol.ClientKeyHistoryDomain{ChainID: cfg.ChainID, GenesisHash: genesis, Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), SettlementVault: common.HexToAddress(cfg.SettlementVault), DeploymentIDHash: sha256.Sum256([]byte(cfg.DeploymentID)), PolicyHash: policy, NoID: noID}
	request = protocol.ClientKeyObservationRequest{ClientID: [16]byte(clientID), ValidatorHotkey: hotkey, NativeBlock: artifact.NativeSnapshotBlock, NativeHash: nativeHash, NativeEpoch: artifact.SubnetEpoch, DecisionBoundary: protocol.ClientKeyEffectiveBoundary{Epoch: artifact.SettlementEpoch, Block: artifact.EVMSnapshotBlock, Hash: evmHash}}
	if artifact.DeploymentID != cfg.DeploymentID || artifact.ChainID != cfg.ChainID || artifact.GenesisHash != cfg.GenesisHash || artifact.Coordinator != cfg.Coordinator || artifact.SettlementVault != cfg.SettlementVault || artifact.Netuid != cfg.Netuid || artifact.PolicyHash != cfg.PolicyHash || artifact.ValidatorID != cfg.ValidatorID {
		return protocol.ClientKeyHistoryDomain{}, protocol.ClientKeyObservationRequest{}, errors.New("client-key capture differs from the admitted validator deployment")
	}
	return domain, request, domain.Validate()
}

// Nonce freshness belongs to live transport. The immutable local slot names
// the complete decision before that nonce exists, so retries reopen one file.
func releaseClientKeyCaptureV2Path(stateDir string, domain protocol.ClientKeyHistoryDomain, request protocol.ClientKeyObservationRequest) (string, error) {
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || filepath.Dir(stateDir) == stateDir {
		return "", errors.New("client-key capture has no private absolute state owner")
	}
	domainHash, err := domain.Digest()
	if err != nil {
		return "", err
	}
	request.Nonce = [32]byte{1}
	if err := request.Validate(); err != nil {
		return "", err
	}
	request.Nonce = [32]byte{}
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	data := append([]byte("urnetwork-client-key-capture-slot-v1\x00"), domainHash[:]...)
	hash := sha256.Sum256(append(data, encoded...))
	return filepath.Join(stateDir, "client-key-observations", fmt.Sprintf("%x.json", hash)), nil
}

// All views use the actual concrete RPC transport and canonical hash selector.
// A candidate, config artifact signer, or current binding cannot supply roots.
func (self *ChainClient) readReleaseClientKeyAuthorityV2(ctx context.Context, domain protocol.ClientKeyHistoryDomain, boundary protocol.ClientKeyEffectiveBoundary) (result common.Address, resultErr error) {
	if ctx != nil {
		if owner, ok := ctx.Value(releaseClientKeyAuthorityV2ContextKey{}).(*releaseClientKeyAuthorityV2Reads); ok {
			return owner.read(ctx, self, domain, boundary)
		}
	}
	return self.readReleaseClientKeyAuthorityUnsharedV2(ctx, domain, boundary)
}

// Unowned callers and every scoped miss perform the same complete real reads.
func (self *ChainClient) readReleaseClientKeyAuthorityUnsharedV2(ctx context.Context, domain protocol.ClientKeyHistoryDomain, boundary protocol.ClientKeyEffectiveBoundary) (result common.Address, resultErr error) {
	if ctx == nil || self == nil || self.client == nil || self.coordinator == nil || self.chainId == nil || !self.release || self.chainId.Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 || self.contractAddr != domain.Coordinator {
		return result, errors.New("client-key historical chain differs from its independent domain")
	}
	if err := errors.Join(ctx.Err(), domain.Validate(), boundary.Validate()); err != nil {
		return result, err
	}
	chain := &ChainClient{client: self.client, coordinator: self.coordinator, chainId: new(big.Int).Set(self.chainId), contractAddr: self.contractAddr, release: true}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = common.Address{}
		}
	}()
	chainID, err := chain.client.ChainID(ctx)
	if err != nil || chainID == nil || chainID.Cmp(chain.chainId) != 0 {
		return result, errors.Join(errors.New("client-key authority RPC chain identity differs"), err)
	}
	var nativeGenesis *common.Hash
	err = chain.client.Client().CallContext(ctx, &nativeGenesis, "chain_getBlockHash", uint64(0))
	if err != nil || nativeGenesis == nil || [32]byte(*nativeGenesis) != domain.GenesisHash {
		return result, errors.Join(errors.New("client-key authority native RPC genesis identity differs"), err)
	}
	finalized, finalizedHash, err := chain.FinalizedBlockContext(ctx)
	if err != nil || finalized < boundary.Block || finalized == boundary.Block && finalizedHash != boundary.Hash {
		return result, errors.Join(errors.New("client-key authority boundary is not finalized"), err)
	}
	epoch := new(big.Int).SetUint64(boundary.Epoch)
	methods := []string{"currentEpoch", "netuid", "settlementVault", "policyAt", "operatorAt", "validatorEvidence"}
	calldata := [][]byte{chain.coordinator.PackCurrentEpoch(), chain.coordinator.PackNetuid(), chain.coordinator.PackSettlementVault(), chain.coordinator.PackPolicyAt(epoch), chain.coordinator.PackOperatorAt(new(big.Int).SetUint64(domain.NoID), epoch), chain.coordinator.PackValidatorEvidence()}
	values := make([][]byte, len(methods))
	for index, method := range methods {
		encoded, err := chain.ethCallAtHashContext(ctx, domain.Coordinator, calldata[index], boundary.Block, boundary.Hash)
		if err != nil {
			return result, err
		}
		if err := canonicalReleaseDecisionV2View(method, encoded); err != nil {
			return result, err
		}
		values[index] = encoded
	}
	actualEpoch, epochErr := chain.coordinator.UnpackCurrentEpoch(values[0])
	netuid, netuidErr := chain.coordinator.UnpackNetuid(values[1])
	vault, vaultErr := chain.coordinator.UnpackSettlementVault(values[2])
	policy, policyErr := chain.coordinator.UnpackPolicyAt(values[3])
	operator, operatorErr := chain.coordinator.UnpackOperatorAt(values[4])
	anchor, anchorErr := chain.coordinator.UnpackValidatorEvidence(values[5])
	if err := errors.Join(epochErr, netuidErr, vaultErr, policyErr, operatorErr, anchorErr); err != nil {
		return result, err
	}
	if actualEpoch == nil || actualEpoch.Cmp(epoch) != 0 || netuid != domain.Netuid || vault != domain.SettlementVault || policy.PolicyHash != domain.PolicyHash || policy.EffectiveBlock > boundary.Block || policy.EffectiveEpoch > boundary.Epoch || !operator.Active || operator.RootSigner == (common.Address{}) || operator.EffectiveEpoch > boundary.Epoch {
		return result, errors.New("client-key root has no actual authority at the exact historical boundary")
	}
	if anchor == (common.Address{}) || anchor == domain.Coordinator || anchor == domain.SettlementVault {
		return result, errors.New("client-key authority immutable companion is absent or invalid")
	}
	companion := stabi.NewSTValidatorEvidence()
	fields := []struct {
		data     []byte
		expected [32]byte
	}{
		{data: companion.PackCoordinator(), expected: [32]byte(common.BytesToHash(domain.Coordinator[:]))},
		{data: companion.PackSettlementVault(), expected: [32]byte(common.BytesToHash(domain.SettlementVault[:]))},
		{data: companion.PackChainId(), expected: [32]byte(common.BigToHash(new(big.Int).SetUint64(domain.ChainID)))},
		{data: companion.PackNetuid(), expected: [32]byte(common.BigToHash(new(big.Int).SetUint64(uint64(domain.Netuid))))},
		{data: companion.PackGenesisHash(), expected: domain.GenesisHash},
		{data: companion.PackDeploymentIdHash(), expected: domain.DeploymentIDHash},
	}
	for _, field := range fields {
		encoded, err := chain.ethCallAtHashContext(ctx, anchor, field.data, boundary.Block, boundary.Hash)
		if err != nil || !bytes.Equal(encoded, field.expected[:]) {
			return result, errors.Join(errors.New("client-key companion immutable native/deployment identity differs"), err)
		}
	}
	return operator.RootSigner, nil
}

// The response contains each generation from one, a signed current head, and
// its exact live request. Recovery supplies that original retained nonce only
// after checking the artifact's complete capture hash and independent clocks.
func verifyReleaseClientKeyCaptureV2(ctx context.Context, chain *ChainClient, encoded []byte, maximum uint64, domain protocol.ClientKeyHistoryDomain, request protocol.ClientKeyObservationRequest, retained bool) (result protocol.ClientKeyRegistration, resultErr error) {
	if ctx == nil {
		return result, errors.New("client-key capture verifier context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = protocol.ClientKeyRegistration{}
		}
	}()
	if owner, ok := ctx.Value(releaseClientKeyAuthorityV2ContextKey{}).(*releaseClientKeyAuthorityV2Reads); ok {
		if err := owner.reserveResponse(ctx, chain, domain, request, uint64(len(encoded))); err != nil {
			return result, err
		}
	}
	return verifyReservedReleaseClientKeyCaptureV2(ctx, chain, encoded, maximum, domain, request, retained)
}

// Batch callers have already charged the exact complete body before their
// authority prefetch. Every canonical wrapper, signature and request is still
// checked here; reservation changes only memory accounting, not acceptance.
func verifyReservedReleaseClientKeyCaptureV2(ctx context.Context, chain *ChainClient, encoded []byte, maximum uint64, domain protocol.ClientKeyHistoryDomain, request protocol.ClientKeyObservationRequest, retained bool) (result protocol.ClientKeyRegistration, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = protocol.ClientKeyRegistration{}
		}
	}()
	response, err := protocol.DecodeClientKeyHistoryResponse(encoded, maximum)
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
	if retained {
		request.Nonce = observation.Request.Nonce
	}
	if err := request.Validate(); err != nil || observation.Request != request || observation.Domain != domain || observation.Generation != uint64(len(response.History)) {
		return result, errors.Join(errors.New("client-key capture does not answer the independent decision request"), err)
	}
	var prior *protocol.ClientKeyRegistration
	var registrationSigner common.Address
	// Repeated boundaries share reads in this response and, when owned by the
	// caller, this one decision. Every new decision uses fresh actual reads.
	authorities := make(map[protocol.ClientKeyEffectiveBoundary]common.Address)
	for _, registrationBytes := range response.History {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		wrapper, err := protocol.DecodeClientKeyEvidence(registrationBytes, domain, protocol.ClientKeyRegistrationEvidenceKind)
		if err != nil {
			return result, err
		}
		registration, err := protocol.DecodeClientKeyRegistration(wrapper.Payload)
		if err != nil || registration.Domain != domain || registration.ClientID != request.ClientID {
			return result, errors.Join(errors.New("client-key capture registration identity differs"), err)
		}
		if err := registration.Follows(prior); err != nil {
			return result, err
		}
		boundary := registration.EffectiveBoundary
		if boundary.Block > request.DecisionBoundary.Block || boundary.Epoch > request.DecisionBoundary.Epoch || boundary.Block == request.DecisionBoundary.Block && boundary != request.DecisionBoundary {
			return result, errors.New("client-key registration follows the independently pinned decision")
		}
		registrationSigner = authorities[boundary]
		if registrationSigner == (common.Address{}) {
			registrationSigner, err = chain.readReleaseClientKeyAuthorityV2(ctx, domain, boundary)
			if err != nil {
				return result, err
			}
			authorities[boundary] = registrationSigner
		}
		if registration.Signer != registrationSigner {
			return result, errors.New("client-key registration root differs from its historical operator")
		}
		result = registration
		prior = &result
	}
	observationSigner := authorities[request.DecisionBoundary]
	if observationSigner == (common.Address{}) {
		observationSigner, err = chain.readReleaseClientKeyAuthorityV2(ctx, domain, request.DecisionBoundary)
		if err != nil {
			return result, err
		}
	}
	if err := observation.VerifyRegistration(result, domain, request, registrationSigner, observationSigner); err != nil {
		return result, err
	}
	return result, nil
}

// The descriptor owner spans read, real HTTP/RPC callbacks, publication and
// readback. A raced file is never replaced and late Close failure refuses it.
func captureReleaseClientKeyV2(ctx context.Context, chain *ChainClient, reader *HTTPClientKeyHistoryReader, stateDir string, domain protocol.ClientKeyHistoryDomain, request protocol.ClientKeyObservationRequest, maximum, maximumHistory uint64) (result protocol.ClientKeyRegistration, contentHash string, bytesUsed uint64, resultErr error) {
	if ctx == nil || reader == nil || maximum == 0 || maximum > protocol.MaxClientKeyHistoryResponseBytes || maximumHistory == 0 {
		return result, "", 0, errors.New("live client-key capture owner or bound is incomplete")
	}
	maximum, err := releaseClientKeyCaptureV2Maximum(ctx, maximum)
	if err != nil {
		return result, "", 0, err
	}
	path, err := releaseClientKeyCaptureV2Path(stateDir, domain, request)
	if err != nil {
		return result, "", 0, err
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, maximum, releaseMeasurementInputV2ReadHooks{}, true)
	defer func() {
		resultErr = errors.Join(resultErr, owner.finish(), ctx.Err())
		if resultErr != nil {
			result, contentHash, bytesUsed = protocol.ClientKeyRegistration{}, "", 0
		}
	}()
	if err != nil {
		return result, "", 0, err
	}
	// Nonblocking native locking serializes this finite namespace across
	// processes. Closing its retained directory releases the lock as well.
	if err := unix.Flock(int(owner.directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return result, "", 0, errors.Join(errors.New("client-key capture namespace is busy"), err)
	}
	maximumFiles := ReleaseEvidenceV2DefaultCaptureFiles
	if reads, ok := ctx.Value(releaseClientKeyAuthorityV2ContextKey{}).(*releaseClientKeyAuthorityV2Reads); ok && reads != nil {
		maximumFiles = reads.maximumCaptureFiles
	}
	historyBytes, entries, err := censusReleaseEvidenceCapturesV2(ctx, owner.directory, maximumHistory, maximumFiles)
	if err != nil {
		return result, "", 0, err
	}
	encoded, err := owner.read()
	retained := err == nil
	if err != nil {
		if !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(err) {
			return result, "", 0, err
		}
		if entries >= maximumFiles || historyBytes >= maximumHistory {
			return result, "", 0, errors.New("client-key capture namespace reached its finite history allowance")
		}
		maximum = min(maximum, maximumHistory-historyBytes)
		if _, err := rand.Read(request.Nonce[:]); err != nil {
			return result, "", 0, err
		}
		if err := owner.check(); err != nil {
			return result, "", 0, err
		}
		encoded, err = reader.Read(ctx, request, maximum)
		if err != nil {
			return result, "", 0, err
		}
	}
	result, err = verifyReleaseClientKeyCaptureV2(ctx, chain, encoded, maximum, domain, request, retained)
	if err != nil {
		return result, "", 0, err
	}
	if retained {
		err = owner.sync()
	} else {
		err = owner.write(encoded)
	}
	if err != nil {
		return result, "", 0, err
	}
	return result, ReleaseMeasurementContentHash(encoded), uint64(len(encoded)), owner.check()
}

// Batches of one bound census storage even for a hostile directory. Unknown
// entries and crash temporaries consume the same finite byte/count allowance.
func censusReleaseClientKeyCapturesV2(ctx context.Context, parent *attemptPrivateDirectory, maximum uint64) (used uint64, count uint64, resultErr error) {
	return censusReleaseEvidenceCapturesV2(ctx, parent, maximum, ReleaseEvidenceV2DefaultCaptureFiles)
}

// One shared directory counts all operators, unknown names and crash temps.
// Count and byte bounds are independent, even for empty hostile files.
func censusReleaseEvidenceCapturesV2(ctx context.Context, parent *attemptPrivateDirectory, maximum, maximumFiles uint64) (used uint64, count uint64, resultErr error) {
	if ctx == nil || parent == nil || maximum == 0 || maximumFiles == 0 || maximumFiles >= uint64(^uint(0)>>1) {
		return 0, 0, errors.New("client-key capture census owner is incomplete")
	}
	directory, err := openAttemptPrivateDirectory(parent.path)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, parent.check(), directory.check(), directory.close(), ctx.Err())
	}()
	if directory.anchor.dev != parent.anchor.dev || directory.anchor.ino != parent.anchor.ino {
		return 0, 0, errors.New("client-key capture directory changed before census")
	}
	for {
		if err := ctx.Err(); err != nil {
			return used, count, err
		}
		entries, err := directory.file.ReadDir(1)
		if err != nil && !errors.Is(err, io.EOF) {
			return used, count, err
		}
		for _, entry := range entries {
			state, err := directory.stat(entry.Name())
			if err != nil || !state.regular() || state.uid != uint32(os.Geteuid()) || state.mode&0o077 != 0 || state.size < 0 || count >= maximumFiles || uint64(state.size) > maximum-used {
				return used, count, errors.Join(errors.New("client-key capture history census exceeds its finite private allowance"), err)
			}
			count++
			used += uint64(state.size)
		}
		if errors.Is(err, io.EOF) {
			return used, count, nil
		}
	}
}

// Actual startup custody owns the file through publication. Older missing
// observations remain an explicit unsupported historical evidence gap.
func readRetainedReleaseClientKeyV2(ctx context.Context, chain *ChainClient, custody *releaseEvidenceV2StartupReferences, stateDir string, domain protocol.ClientKeyHistoryDomain, request protocol.ClientKeyObservationRequest, contentHash string, maximum uint64) (protocol.ClientKeyRegistration, error) {
	maximum = min(maximum, uint64(protocol.MaxClientKeyHistoryResponseBytes))
	maximum, err := releaseClientKeyCaptureV2Maximum(ctx, maximum)
	if err != nil {
		return protocol.ClientKeyRegistration{}, err
	}
	if custody == nil || maximum == 0 || contentHash == "" {
		return protocol.ClientKeyRegistration{}, errReleaseHistoricalClientKeyV2
	}
	if _, err := parseReleaseContentHash(contentHash); err != nil {
		return protocol.ClientKeyRegistration{}, err
	}
	path, err := releaseClientKeyCaptureV2Path(stateDir, domain, request)
	if err != nil {
		return protocol.ClientKeyRegistration{}, err
	}
	encoded, err := custody.read(ctx, path, min(maximum, uint64(protocol.MaxClientKeyHistoryResponseBytes)), false)
	if err != nil || ReleaseMeasurementContentHash(encoded) != contentHash {
		return protocol.ClientKeyRegistration{}, errors.Join(errReleaseHistoricalClientKeyV2, errors.New("retained client-key capture differs from its complete byte identity"), err)
	}
	registration, err := verifyReleaseClientKeyCaptureV2(ctx, chain, encoded, maximum, domain, request, true)
	return registration, errors.Join(err, custody.check(), ctx.Err())
}
