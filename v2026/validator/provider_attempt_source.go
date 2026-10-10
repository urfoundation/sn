//go:build linux || darwin

// Live acquisition and cold replay share the same verifier. Every transport
// byte needed by replay is retained; returned counters are never evidence.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// One operation owns captured objects and its aggregate byte allowance.
type providerAttemptObjectStore struct {
	stateLock  sync.Mutex
	original   *ProviderAttemptOriginal
	limit      uint64
	maxObjects uint64
	bytes      uint64
	indexKVs   map[string]int
	usedKVs    map[string]bool
	live       bool
	budget     *providerAttemptOriginalBudget
	pendingKVs map[string]chan struct{}
}

// Transport whitespace and omitted optional nulls are not signed metadata.
// Reject duplicate/unknown fields and trailing documents while preserving all
// original signed byte slices for their separate canonical verification.
func decodeProviderAttemptTransport(raw []byte, target any) error {
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider transport has trailing JSON"))
	}
	return nil
}

// Neither a fresh request nor a cold object can exceed the same full budget.
func newProviderAttemptObjectStore(original *ProviderAttemptOriginal, authority ProviderAttemptAuthority, live bool, budget *providerAttemptOriginalBudget) (*providerAttemptObjectStore, error) {
	self := &providerAttemptObjectStore{original: original, limit: authority.MaxOriginalBytes, maxObjects: authority.MaxObjects, indexKVs: map[string]int{}, usedKVs: map[string]bool{}, live: live, budget: budget, pendingKVs: map[string]chan struct{}{}}
	if uint64(len(original.Objects)) > self.maxObjects {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	for index, value := range original.Objects {
		key := value.Origin + "\x00" + value.Kind + "\x00" + value.Hash
		if _, exists := self.indexKVs[key]; exists {
			return nil, protocol.ErrProviderAttemptsIntegrity
		}
		if uint64(len(value.Body)) > self.limit-self.bytes {
			return nil, protocol.ErrProviderAttemptsCapacity
		}
		self.bytes += uint64(len(value.Body))
		self.indexKVs[key] = index
	}
	return self, nil
}

// Concurrent replica reads commit exact detached bytes under one charge lock.
func (self *providerAttemptObjectStore) read(ctx context.Context, reader *HTTPAttemptStreamV2Reader, origin, kind, hash string, size uint64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := origin + "\x00" + kind + "\x00" + hash
	self.stateLock.Lock()
	if pending := self.pendingKVs[key]; pending != nil {
		self.stateLock.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending:
			return self.read(ctx, reader, origin, kind, hash, size)
		}
	}
	if index, exists := self.indexKVs[key]; exists {
		value := self.original.Objects[index]
		self.usedKVs[key] = true
		self.stateLock.Unlock()
		digest := sha256.Sum256(value.Body)
		if uint64(len(value.Body)) != size || hash != attemptHex32(digest) {
			return nil, protocol.ErrProviderAttemptsIntegrity
		}
		return bytes.Clone(value.Body), ctx.Err()
	}
	if !self.live {
		self.stateLock.Unlock()
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	if size > self.limit-self.bytes || uint64(len(self.original.Objects)) >= self.maxObjects {
		self.stateLock.Unlock()
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	pending := make(chan struct{})
	self.pendingKVs[key] = pending
	self.stateLock.Unlock()
	defer func() {
		self.stateLock.Lock()
		delete(self.pendingKVs, key)
		close(pending)
		self.stateLock.Unlock()
	}()
	wireBytes, err := providerAttemptObjectWireBytes(origin, kind, hash, size)
	if err != nil {
		return nil, err
	}
	if err := self.budget.reserve(wireBytes); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			self.budget.release(wireBytes)
		}
	}()
	var raw []byte
	if kind == "metadata" {
		raw, err = reader.ReadMetadata(ctx, hash, size)
	} else {
		var body io.ReadCloser
		body, err = reader.OpenData(ctx, kind, hash, size)
		if body != nil {
			if err == nil {
				raw, err = io.ReadAll(io.LimitReader(body, int64(size)+1))
			}
			err = errors.Join(err, body.Close(), ctx.Err())
		}
	}
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	if uint64(len(raw)) != size || hash != attemptHex32(digest) {
		return nil, protocol.ErrProviderAttemptsIntegrity
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if index, exists := self.indexKVs[key]; exists {
		if !bytes.Equal(self.original.Objects[index].Body, raw) {
			return nil, protocol.ErrProviderAttemptsIntegrity
		}
	} else {
		if size > self.limit-self.bytes || uint64(len(self.original.Objects)) >= self.maxObjects {
			return nil, protocol.ErrProviderAttemptsCapacity
		}
		committed = true
		self.bytes += size
		self.indexKVs[key] = len(self.original.Objects)
		self.original.Objects = append(self.original.Objects, ProviderAttemptOriginalObject{Origin: origin, Kind: kind, Hash: hash, Body: bytes.Clone(raw)})
	}
	self.usedKVs[key] = true
	return raw, ctx.Err()
}

// Optional observer reads recover only recognized transport failures within
// one original 300-second owner. The only POSTs are exact original lookups or
// idempotent signed request-local close receipts; settlement sends are absent.
func providerAttemptHttp(ctx context.Context, endpoint, method string, request []byte, limit uint64) ([]byte, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return providerAttemptHttpWithClient(ctx, endpoint, method, request, limit, client)
}

// The client belongs to this invocation. Real request deadlines and body closes
// finish before classification; a transient sibling never hides a hard cause.
func providerAttemptHttpWithClient(ctx context.Context, endpoint, method string, request []byte, limit uint64, client *http.Client) ([]byte, error) {
	if ctx == nil || limit == 0 || limit > 1024*1024*1024 {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	if client == nil {
		return nil, protocol.ErrProviderAttemptsIntegrity
	}
	parsed, err := providerAttemptEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	owner, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	var lastErr error
	for {
		if err := evidenceReadContextError(owner); err != nil {
			return nil, errors.Join(lastErr, err)
		}
		attempt, stop := context.WithTimeout(owner, 60*time.Second)
		req, err := http.NewRequestWithContext(attempt, method, parsed.String(), bytes.NewReader(request))
		if err != nil {
			stop()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if err := evidenceReadContextError(attempt); err != nil {
			stop()
			return nil, errors.Join(lastErr, err)
		}
		response, readErr := client.Do(req)
		var raw []byte
		status := 0
		if response != nil {
			status = response.StatusCode
			raw, err = io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
			if err != nil {
				err = &url.Error{Op: "Read", URL: parsed.String(), Err: err}
			}
			closeErr := response.Body.Close()
			if closeErr != nil {
				closeErr = &url.Error{Op: "Close", URL: parsed.String(), Err: closeErr}
			}
			readErr = errors.Join(readErr, err, closeErr)
		}
		attemptErr := evidenceReadContextError(attempt)
		stop()
		if uint64(len(raw)) > limit {
			return nil, errors.Join(protocol.ErrProviderAttemptsCapacity, readErr, attemptErr, evidenceReadContextError(owner))
		}
		if readErr == nil && attemptErr == nil && status == http.StatusOK {
			if err := evidenceReadContextError(owner); err != nil {
				return nil, err
			}
			return raw, nil
		}
		if readErr == nil && status == http.StatusNotFound {
			return nil, errors.Join(protocol.ErrProviderAttemptsUnavailable, attemptErr, evidenceReadContextError(owner))
		}
		var statusErr error
		if status != 0 && status != http.StatusOK {
			statusErr = &releaseHttpGetStatusError{endpoint: parsed.String(), status: status}
		}
		lastErr = errors.Join(readErr, statusErr, attemptErr)
		if ownerErr := evidenceReadContextError(owner); ownerErr != nil {
			// Cancellation can accompany completed transport work, but cannot
			// erase a returned conflict or independent body/close failure.
			allowed, _ := classifyReleaseSnapshotRetryMode(lastErr, true, false, false)
			if !allowed {
				return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, lastErr, ownerErr)
			}
			return nil, errors.Join(lastErr, ownerErr)
		}
		if !RetryableEvidenceTransportError(lastErr) {
			return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, lastErr, evidenceReadContextError(owner))
		}
		select {
		case <-owner.Done():
			return nil, errors.Join(lastErr, evidenceReadContextError(owner))
		case <-time.After(time.Second):
		}
	}
}

// The endpoint serves per-window independently signed input selections. It is
// a discovery transport; neither its URL nor its JSON grants economic authority.
func (self *ProviderAttemptAuthoritySource) Read(ctx context.Context, artifact *payoutartifact.Artifact) (json.RawMessage, *VerifiedProviderAttemptMeasurement, error) {
	if self == nil {
		return nil, nil, protocol.ErrProviderAttemptsUnavailable
	}
	return self.ReadBounded(ctx, artifact, self.authority.MaxOriginalBytes)
}

// Clip this operation's acquisition to the caller's remaining physical frame.
// A larger configured authority is valid: it grants a ceiling, not an allocation.
func (self *ProviderAttemptAuthoritySource) ReadBounded(ctx context.Context, artifact *payoutartifact.Artifact, maximumOriginalBytes uint64) (json.RawMessage, *VerifiedProviderAttemptMeasurement, error) {
	if self == nil || artifact == nil {
		return nil, nil, protocol.ErrProviderAttemptsUnavailable
	}
	if maximumOriginalBytes == 0 {
		return nil, nil, protocol.ErrProviderAttemptsCapacity
	}
	maximumOriginalBytes = min(maximumOriginalBytes, self.authority.MaxOriginalBytes)
	endpoint, err := providerAttemptEndpoint(self.authority.WindowEndpoint)
	if err != nil {
		return nil, nil, err
	}
	endpoint.Path = fmt.Sprintf("%s/%d/%d", endpoint.Path, artifact.NoID, artifact.Epoch)
	raw, err := providerAttemptHttp(ctx, endpoint.String(), http.MethodGet, nil, min(self.authority.MaxWindowBytes, maximumOriginalBytes))
	if err != nil {
		return nil, nil, err
	}
	var response ProviderAttemptWindowResponse
	if err := decodeProviderAttemptTransport(raw, &response); err != nil {
		return nil, nil, err
	}
	original := ProviderAttemptOriginal{Schema: ProviderAttemptOriginalSchema, AuthorityHash: self.hash, Response: response, Objects: []ProviderAttemptOriginalObject{}, Receipts: []ProviderAttemptOriginalResponse{}}
	initial, err := json.Marshal(original)
	if err != nil {
		return nil, nil, err
	}
	budget := &providerAttemptOriginalBudget{maximum: maximumOriginalBytes}
	if err := budget.reserve(uint64(len(initial))); err != nil {
		return nil, nil, err
	}
	verified, err := self.verify(ctx, artifact, &original, true, budget)
	if err != nil {
		return nil, nil, err
	}
	// Stable ordering makes retries of identical originals hash identically.
	sort.Slice(original.Objects, func(i, j int) bool {
		a, b := original.Objects[i], original.Objects[j]
		return a.Origin+"\x00"+a.Kind+"\x00"+a.Hash < b.Origin+"\x00"+b.Kind+"\x00"+b.Hash
	})
	sort.Slice(original.Receipts, func(i, j int) bool {
		return bytes.Compare(original.Receipts[i].RequestHash[:], original.Receipts[j].RequestHash[:]) < 0
	})
	raw, err = json.Marshal(original)
	if err != nil {
		return nil, nil, err
	}
	if uint64(len(raw)) > maximumOriginalBytes {
		return nil, nil, protocol.ErrProviderAttemptsCapacity
	}
	verified.OriginalHash = sha256.Sum256(raw)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return raw, verified, nil
}

// Cold reopening authenticates all original bytes under the same original
// authority without consulting a live endpoint or trusting prior projections.
func (self *ProviderAttemptAuthoritySource) VerifyRetained(ctx context.Context, artifact *payoutartifact.Artifact, raw json.RawMessage) (*VerifiedProviderAttemptMeasurement, error) {
	if self == nil || artifact == nil {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	if ctx == nil {
		return nil, errors.New("provider retained owner absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 || uint64(len(raw)) > self.authority.MaxOriginalBytes {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	var original ProviderAttemptOriginal
	if err := attemptStoreDecode(raw, &original); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(original)
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
	}
	verified, err := self.verify(ctx, artifact, &original, false, nil)
	if err != nil {
		return nil, err
	}
	verified.OriginalHash = sha256.Sum256(raw)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return verified, nil
}

// Canonical headers, not receipt wall-time approximations, select the interval.
func (self *ProviderAttemptAuthoritySource) verifyArtifactWindow(ctx context.Context, artifact *payoutartifact.Artifact, authority ProviderAttemptWindowAuthority) (uint64, uint64, error) {
	if err := self.verifyAuthority(ctx, authority); err != nil {
		return 0, 0, err
	}
	genesis, err := canonicalAttemptHex32("provider artifact genesis", artifact.GenesisHash, false)
	if err != nil {
		return 0, 0, err
	}
	policy, err := canonicalAttemptHex32("provider artifact policy", artifact.PolicyHash, false)
	if err != nil {
		return 0, 0, err
	}
	domain := protocol.ProviderAttemptDomain{ChainId: artifact.ChainID, GenesisHash: genesis, Netuid: artifact.Netuid, Coordinator: [20]byte(artifact.Coordinator), SettlementVault: [20]byte(artifact.SettlementVault), DeploymentIdHash: sha256.Sum256([]byte(artifact.DeploymentID)), PolicyHash: policy}
	w := authority.Window
	if domain != self.authority.Registry.Domain || w.Epoch != artifact.Epoch || w.StartBlock != artifact.Start.Number || w.EndBlock != artifact.End.Number || w.FinalizedBlock != w.EndBlock || w.Subject != (protocol.ValidatorEvidenceSubject{}) {
		return 0, 0, protocol.ErrProviderAttemptsIntegrity
	}
	var start, end types.Header
	if len(authority.StartHeader) > 65536 || len(authority.EndHeader) > 65536 {
		return 0, 0, protocol.ErrProviderAttemptsCapacity
	}
	if err := json.Unmarshal(authority.StartHeader, &start); err != nil {
		return 0, 0, err
	}
	if err := json.Unmarshal(authority.EndHeader, &end); err != nil {
		return 0, 0, err
	}
	if start.Number == nil || end.Number == nil || !start.Number.IsUint64() || !end.Number.IsUint64() || start.Number.Uint64() != w.StartBlock || end.Number.Uint64() != w.EndBlock || start.Hash().Hex() != artifact.Start.Hash || end.Hash().Hex() != artifact.End.Hash || end.Time <= start.Time || end.Time > ^uint64(0)/1000 {
		return 0, 0, protocol.ErrProviderAttemptsIntegrity
	}
	return start.Time * 1000, end.Time * 1000, ctx.Err()
}

// Construct actual replay callbacks only after the independent signature has
// selected every profile/key. One new scratch child belongs to this operation.
func (self *ProviderAttemptAuthoritySource) verify(ctx context.Context, artifact *payoutartifact.Artifact, original *ProviderAttemptOriginal, live bool, budget *providerAttemptOriginalBudget) (result *VerifiedProviderAttemptMeasurement, resultErr error) {
	if ctx == nil {
		return nil, errors.New("provider attempt source owner absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if original.Schema != ProviderAttemptOriginalSchema || original.AuthorityHash != self.hash {
		return nil, protocol.ErrProviderAttemptsIntegrity
	}
	startMs, endMs, err := self.verifyArtifactWindow(ctx, artifact, original.Response.Authority)
	if err != nil {
		return nil, err
	}
	store, err := newProviderAttemptObjectStore(original, self.authority, live, budget)
	if err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp(self.authority.ScratchRoot, "provider-attempt-replay-")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(scratch)) }()
	options := ProviderAttemptWindowOptions{Registry: self.authority.Registry, Window: original.Response.Authority.Window, Validators: map[[32]byte]ProviderAttemptValidatorOptions{}, MaxProviders: self.authority.MaxProviders, MaxMetadataBytes: self.authority.MaxMetadataBytes, MaxRecordBytes: self.authority.MaxRecordBytes, MaxRecords: self.authority.MaxRecords}
	options.Registry.RequiredHeadHash = original.Response.Authority.RegistryHead
	var aMin uint64
	for index, validator := range original.Response.Authority.Validators {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := options.Validators[validator.Hotkey]; exists {
			return nil, protocol.ErrProviderAttemptsIntegrity
		}
		option := ProviderAttemptValidatorOptions{Publication: validator.Publication, Settlement: AttemptSettlementV2Options{Operators: map[uint64]AttemptSettlementV2OperatorOptions{}, MaxParticipants: validator.MaxParticipants, MaxTransitionBytes: validator.MaxTransitionBytes, MaxClosureBytes: validator.MaxClosureBytes}}
		// The signed selection names this validator's one or two replica origins;
		// the shared publication reader still checks them against its census.
		if err := validateReleaseEvidenceV2ReplicaCount(len(validator.Publication.Origins)); err != nil {
			return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
		}
		replicas := make([]ValidatorEvidenceRetainedReplicaV2, len(validator.Publication.Origins))
		for replica, origin := range validator.Publication.Origins {
			reader, err := newHttpAttemptStreamV2Reader(origin, validator.Publication.Bounds.Cut, validator.MaxClosureBytes)
			if err != nil {
				return nil, err
			}
			replicas[replica] = ValidatorEvidenceRetainedReplicaV2{Origin: origin, ReadMetadata: func(owner context.Context, hash string, size uint64) ([]byte, error) {
				return store.read(owner, reader, origin, "metadata", hash, size)
			}}
		}
		option.RetainedMetadata = replicas
		for position, operator := range validator.Operators {
			noId := operator.Expected.Identity.NoID
			if _, exists := option.Settlement.Operators[noId]; exists {
				return nil, protocol.ErrProviderAttemptsIntegrity
			}
			if operator.Requests.Identity.Ledger != operator.Expected.Identity || operator.Requests.Identity.PolicyHash != self.authority.Registry.Domain.PolicyHash || operator.Requests.Identity.Coordinator != fmt.Sprintf("0x%x", self.authority.Registry.Domain.Coordinator) || operator.ReceiptScope.GenesisHash != self.authority.Registry.Domain.GenesisHash || operator.ReceiptScope.PolicyHash != self.authority.Registry.Domain.PolicyHash || operator.ReceiptScope.Netuid != uint64(self.authority.Registry.Domain.Netuid) || operator.ReceiptScope.NoId != noId || sha256.Sum256([]byte(operator.ReceiptScope.DeploymentId)) != self.authority.Registry.Domain.DeploymentIdHash {
				return nil, protocol.ErrProviderAttemptsIntegrity
			}
			if err := operator.Requests.Validate(); err != nil {
				return nil, err
			}
			if aMin == 0 {
				aMin = operator.Policy.Verify.ReliabilityAMin
			}
			if aMin == 0 || aMin != operator.Policy.Verify.ReliabilityAMin {
				return nil, protocol.ErrProviderAttemptsIntegrity
			}
			bindings := map[connect.Id]FleetScoreKey{}
			for _, binding := range operator.Bindings {
				if _, exists := bindings[binding.ClientId]; exists {
					return nil, protocol.ErrProviderAttemptsIntegrity
				}
				bindings[binding.ClientId] = binding.Binding
			}
			keys := map[byte]ed25519.PublicKey{}
			for _, key := range operator.ServerKeys {
				if _, exists := keys[key.Id]; exists || key.Key == ([32]byte{}) {
					return nil, protocol.ErrProviderAttemptsIntegrity
				}
				keys[key.Id] = bytes.Clone(key.Key[:])
			}
			origin := validator.Publication.Origins[0]
			reader, err := newHttpAttemptStreamV2Reader(origin, operator.Bounds, self.authority.MaxMetadataBytes)
			if err != nil {
				return nil, err
			}
			replay := AttemptCutV2ReplayOptions{Bounds: operator.ReplayBounds, ScratchDirectory: filepath.Join(scratch, strconv.Itoa(index)+"-"+strconv.Itoa(position)), ServerKeys: keys, ReadMetadata: func(owner context.Context, hash string, size uint64) ([]byte, error) {
				return store.read(owner, reader, origin, "metadata", hash, size)
			}, OpenData: func(owner context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
				raw, err := store.read(owner, reader, origin, kind, hash, size)
				if err != nil {
					return nil, err
				}
				return io.NopCloser(bytes.NewReader(raw)), nil
			}}
			option.Settlement.Operators[noId] = AttemptSettlementV2OperatorOptions{Expected: operator.Expected, Policy: operator.Policy, Bounds: operator.Bounds, Measurement: AttemptCutV2MeasurementOptions{ExpectedConfig: operator.Stats, CurrentBindingKVs: bindings, MaxProviders: operator.MaxProviders, MaxEgressHashes: operator.MaxEgressHashes, MaxFleetPrefixes: operator.MaxFleetPrefixes, Replay: replay}}
		}
		options.Validators[validator.Hotkey] = option
	}
	verified, err := VerifyProviderAttemptWindow(ctx, original.Response.Window, options)
	if err != nil {
		return nil, err
	}
	rows, requestsHash, err := self.verifyRequests(ctx, original, startMs, endMs, live, budget)
	if err != nil {
		return nil, err
	}
	if len(store.usedKVs) != len(store.indexKVs) {
		return nil, protocol.ErrProviderAttemptsIntegrity
	}
	// The original request census includes first-response loss, unlike the
	// terminal-only projection retained above for independent causal checking.
	verified.Providers = rows
	verified.OwnedRequestsComplete = true
	combined := append(bytes.Clone(verified.WindowHash[:]), requestsHash[:]...)
	verified.WindowHash = sha256.Sum256(combined)
	return &VerifiedProviderAttemptMeasurement{VerifiedProviderAttemptWindow: verified, ReliabilityAMin: aMin, AuthorityHash: self.hash}, nil
}
