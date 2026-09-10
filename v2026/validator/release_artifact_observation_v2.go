//go:build linux || darwin

package validator

// A validator's signed HTTP observation proves what that validator recorded at
// one chain boundary. It is not universal historical availability or wallclock
// truth. Replay consumes exact retained request/response/error bytes and runs
// the ordinary artifact parser; it never asks today's endpoint for a past fact.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urnetwork/connect/v2026"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"golang.org/x/sys/unix"
)

type releaseArtifactHttpExchangeV2 struct {
	Url            string `json:"url"`
	MaximumBytes   int64  `json:"maximum_bytes"`
	Status         int    `json:"status"`
	ContentType    string `json:"content_type"`
	ContentLength  int64  `json:"content_length"`
	Body           []byte `json:"body"`
	TransportError string `json:"transport_error,omitempty"`
	BodyError      string `json:"body_error,omitempty"`
	CloseError     string `json:"close_error,omitempty"`
}

type releaseArtifactHttpObservationV2 struct {
	Schema          string                          `json:"schema"`
	Decision        ReleaseMeasurementV2Decision    `json:"decision"`
	ValidatorHotkey string                          `json:"validator_hotkey"`
	NoId            uint64                          `json:"no_id"`
	SourceEpoch     uint64                          `json:"source_epoch"`
	Origin          string                          `json:"origin"`
	Exchanges       []releaseArtifactHttpExchangeV2 `json:"exchanges"`
	Signature       string                          `json:"signature"`
}

const releaseArtifactHttpObservationSchemaV2 = "urnetwork-validator-artifact-http-observation-v2"

func releaseArtifactHttpObservationDigestV2(value releaseArtifactHttpObservationV2) ([32]byte, error) {
	value.Signature = ""
	encoded, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte("urnetwork-validator-artifact-http-observation-v2\x00"), encoded...)), nil
}

func releaseArtifactHttpRequestV2(cfg *ReleaseConfig, hotkey [32]byte, decision *ReleaseMeasurementArtifact, noId, sourceEpoch uint64, reader *HTTPArtifactReader) (releaseArtifactHttpObservationV2, string, error) {
	if cfg == nil || decision == nil || reader == nil || reader.baseURL == nil || hotkey == ([32]byte{}) || noId == 0 {
		return releaseArtifactHttpObservationV2{}, "", errors.New("artifact HTTP observation owner is incomplete")
	}
	if decision.DeploymentID != cfg.DeploymentID || decision.ChainID != cfg.ChainID || decision.GenesisHash != cfg.GenesisHash || decision.Coordinator != cfg.Coordinator || decision.SettlementVault != cfg.SettlementVault || decision.PolicyHash != cfg.PolicyHash || decision.Netuid != cfg.Netuid || decision.ValidatorID != cfg.ValidatorID || decision.SettlementEpoch < cfg.Policy.Deposit.UsageLagEpochs || sourceEpoch != decision.SettlementEpoch-cfg.Policy.Deposit.UsageLagEpochs {
		return releaseArtifactHttpObservationV2{}, "", errors.New("artifact HTTP observation differs from the actual deployment/source epoch")
	}
	value := releaseArtifactHttpObservationV2{Schema: releaseArtifactHttpObservationSchemaV2, Decision: releaseMeasurementV2Decision(decision), ValidatorHotkey: releaseHex32(hotkey), NoId: noId, SourceEpoch: sourceEpoch, Origin: reader.baseURL.String(), Exchanges: []releaseArtifactHttpExchangeV2{}}
	// A retry can change artifact lineage after an explicitly failed nonce,
	// but may not manufacture a new HTTP fact at the same observed boundary.
	value.Decision.PreviousArtifactHash = ""
	encoded, err := json.Marshal(value)
	if err != nil {
		return value, "", err
	}
	hash := sha256.Sum256(append([]byte("urnetwork-validator-artifact-http-slot-v2\x00"), encoded...))
	return value, filepath.Join(cfg.StateDir, "artifact-http-observations", hex.EncodeToString(hash[:])+".json"), nil
}

// The HTTP status and exact bounded body are retained even when the content
// type or status makes the ordinary public reader reject them. Transport and
// actual Body.Close failures are signed observations, never success overrides.
func readArtifactHttpExchangeV2(ctx context.Context, reader *HTTPArtifactReader, endpoint string, maximum int64, remaining uint64) (releaseArtifactHttpExchangeV2, error) {
	value := releaseArtifactHttpExchangeV2{Url: endpoint, MaximumBytes: maximum, Body: []byte{}}
	if ctx == nil || reader == nil || reader.client == nil || maximum <= 0 || remaining == 0 {
		return value, errors.New("artifact HTTP byte owner is unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return value, err
	}
	response, err := reader.client.Do(request)
	if err != nil {
		if len(err.Error()) > 4096 {
			return value, errors.New("artifact HTTP transport error exceeds its finite observation bound")
		}
		value.TransportError = err.Error()
		if response != nil && response.Body != nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				value.CloseError = closeErr.Error()
			}
		}
		return value, ctx.Err()
	}
	if response == nil || response.Body == nil {
		return value, errors.New("artifact HTTP response has no real body")
	}
	value.Status, value.ContentLength = response.StatusCode, response.ContentLength
	value.ContentType = response.Header.Get("Content-Type")
	limit := min(uint64(maximum)+1, remaining)
	value.Body, err = io.ReadAll(io.LimitReader(response.Body, int64(limit)))
	if err != nil {
		value.BodyError = err.Error()
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		value.CloseError = closeErr.Error()
	}
	if len(value.ContentType) > 4096 || len(value.BodyError) > 4096 || len(value.CloseError) > 4096 || uint64(len(value.Body)) == remaining {
		return value, errors.New("artifact HTTP observation exceeds its admitted raw byte/error allowance")
	}
	return value, ctx.Err()
}

func interpretArtifactHttpExchangeV2(value releaseArtifactHttpExchangeV2) ([]byte, error) {
	if value.TransportError != "" {
		return nil, errors.New(value.TransportError)
	}
	if value.Status != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", value.Url, value.Status)
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(value.ContentType, ";")[0]))
	if mediaType != "application/json" {
		return nil, fmt.Errorf("%s returned content type %q", value.Url, mediaType)
	}
	if value.ContentLength > value.MaximumBytes || int64(len(value.Body)) > value.MaximumBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", value.Url, value.MaximumBytes)
	}
	if value.BodyError != "" {
		return nil, errors.New(value.BodyError)
	}
	if value.CloseError != "" {
		return nil, errors.New(value.CloseError)
	}
	return value.Body, nil
}

func decodeArtifactHttpObservationV2(ctx context.Context, encoded []byte, maximum uint64, expected releaseArtifactHttpObservationV2) (releaseArtifactHttpObservationV2, error) {
	var value releaseArtifactHttpObservationV2
	if err := decodeAttemptStreamV2JSON(encoded, maximum, &value); err != nil {
		return value, err
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(encoded, canonical) || len(value.Exchanges) == 0 || len(value.Exchanges) > 2 {
		return value, errors.Join(errors.New("artifact HTTP observation is not the exact canonical finite exchange"), err)
	}
	if value.Schema != expected.Schema || value.Decision != expected.Decision || value.ValidatorHotkey != expected.ValidatorHotkey || value.NoId != expected.NoId || value.SourceEpoch != expected.SourceEpoch || value.Origin != expected.Origin {
		return value, errors.New("artifact HTTP observation differs from the independently selected request/domain")
	}
	for _, exchange := range value.Exchanges {
		if len(exchange.Url) > 8192 || len(exchange.ContentType) > 4096 || len(exchange.TransportError) > 4096 || len(exchange.BodyError) > 4096 || len(exchange.CloseError) > 4096 || exchange.MaximumBytes <= 0 || exchange.MaximumBytes > maximumPayoutArtifactBytes {
			return value, errors.New("artifact HTTP exchange labels exceed their bounds")
		}
	}
	digest, err := releaseArtifactHttpObservationDigestV2(value)
	if err != nil {
		return value, err
	}
	hotkey, err := canonicalAttemptHex32("artifact HTTP observer hotkey", expected.ValidatorHotkey, false)
	if err != nil {
		return value, err
	}
	public, err := (sr25519.Scheme{}).FromPublicKey(hotkey[:])
	if err != nil {
		return value, err
	}
	signature, err := hex.DecodeString(value.Signature)
	if err != nil || len(signature) != 64 || hex.EncodeToString(signature) != value.Signature || !public.Verify(digest[:], signature) {
		return value, errors.New("artifact HTTP observation signature is invalid")
	}
	return value, ctx.Err()
}

// Replay installs only exact owned raw exchanges behind the same ordinary
// public parser. Each generated URL and byte ceiling must match in order;
// surplus/unconsumed evidence and live fallback are refused.
func replayArtifactHttpObservationV2(ctx context.Context, reader *HTTPArtifactReader, value releaseArtifactHttpObservationV2) (*payoutartifact.Artifact, error, error) {
	owned := *reader
	index := 0
	var mismatch error
	owned.observedGet = func(ctx context.Context, endpoint string, maximum int64) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			mismatch = err
			return nil, err
		}
		if index >= len(value.Exchanges) || value.Exchanges[index].Url != endpoint || value.Exchanges[index].MaximumBytes != maximum {
			mismatch = errors.New("retained artifact HTTP request order or exact byte bound differs")
			return nil, mismatch
		}
		exchange := value.Exchanges[index]
		index++
		return interpretArtifactHttpExchangeV2(exchange)
	}
	artifact, observedErr := owned.Read(ctx, value.SourceEpoch, value.NoId)
	if mismatch != nil || index != len(value.Exchanges) {
		return nil, nil, errors.Join(errors.New("retained artifact HTTP exchange census differs"), mismatch)
	}
	return artifact, observedErr, ctx.Err()
}

type releaseArtifactCaptureReaderV2 struct {
	ctx      context.Context
	cfg      *ReleaseConfig
	hotkey   *crv4.Keypair
	decision *ReleaseMeasurementArtifact
	reader   *HTTPArtifactReader
	hash     string
	fatal    error
}

func (self *releaseArtifactCaptureReaderV2) Read(ctx context.Context, epoch, noId uint64) (result *payoutartifact.Artifact, observedErr error) {
	result, observedErr, self.hash, self.fatal = self.capture(ctx, epoch, noId)
	if self.fatal != nil {
		return nil, self.fatal
	}
	return result, observedErr
}

func (self *releaseArtifactCaptureReaderV2) capture(ctx context.Context, epoch, noId uint64) (result *payoutartifact.Artifact, observedErr error, hash string, resultErr error) {
	value, path, err := releaseArtifactHttpRequestV2(self.cfg, self.hotkey.PublicKey(), self.decision, noId, epoch, self.reader)
	if err != nil {
		return nil, nil, "", err
	}
	maximum := min(self.cfg.EvidenceV2.Bounds.MaxArtifactBytes, self.cfg.EvidenceV2.Bounds.MaxControlBytes/8)
	if maximum < 32768 {
		return nil, nil, "", errors.New("artifact HTTP observation has insufficient admitted control bytes")
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, maximum, releaseMeasurementInputV2ReadHooks{}, true)
	defer func() {
		resultErr = errors.Join(resultErr, owner.finish(), ctx.Err())
		if resultErr != nil {
			result, observedErr, hash = nil, nil, ""
		}
	}()
	if err != nil {
		return nil, nil, "", err
	}
	if err := unix.Flock(int(owner.directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, nil, "", err
	}
	used, count, err := censusReleaseEvidenceCapturesV2(ctx, owner.directory, self.cfg.EvidenceV2.Bounds.MaxHistoryBytes, self.cfg.EvidenceV2.Bounds.CaptureFileLimit())
	if err != nil {
		return nil, nil, "", err
	}
	encoded, err := owner.read()
	if err == nil {
		value, err = decodeArtifactHttpObservationV2(ctx, encoded, maximum, value)
		if err != nil {
			return nil, nil, "", err
		}
		result, observedErr, err = replayArtifactHttpObservationV2(ctx, self.reader, value)
		if err != nil {
			return nil, nil, "", err
		}
		return result, observedErr, ReleaseMeasurementContentHash(encoded), owner.check()
	}
	if !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(err) {
		return nil, nil, "", err
	}
	if count >= self.cfg.EvidenceV2.Bounds.CaptureFileLimit() || used >= self.cfg.EvidenceV2.Bounds.MaxHistoryBytes {
		return nil, nil, "", errors.New("artifact HTTP observation namespace exceeds its finite history allowance")
	}
	maximum = min(maximum, self.cfg.EvidenceV2.Bounds.MaxHistoryBytes-used)
	remaining := maximum / 2
	ownedReader := *self.reader
	var captureErr error
	ownedReader.observedGet = func(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
		if len(value.Exchanges) >= 2 {
			captureErr = errors.New("artifact HTTP observation exceeds two exact requests")
			return nil, captureErr
		}
		exchange, err := readArtifactHttpExchangeV2(ctx, self.reader, endpoint, limit, remaining)
		if err != nil {
			captureErr = err
			return nil, err
		}
		remaining -= uint64(len(exchange.Body))
		value.Exchanges = append(value.Exchanges, exchange)
		return interpretArtifactHttpExchangeV2(exchange)
	}
	result, observedErr = ownedReader.Read(ctx, epoch, noId)
	if err := errors.Join(captureErr, ctx.Err()); err != nil {
		return nil, nil, "", err
	}
	digest, err := releaseArtifactHttpObservationDigestV2(value)
	if err != nil {
		return nil, nil, "", err
	}
	signature, err := self.hotkey.Sign(digest[:])
	if err != nil {
		return nil, nil, "", err
	}
	value.Signature = hex.EncodeToString(signature)
	encoded, err = json.Marshal(value)
	if err != nil || uint64(len(encoded)) > maximum {
		return nil, nil, "", errors.Join(errors.New("artifact HTTP observation exceeds its canonical wire allowance"), err)
	}
	if _, err := decodeArtifactHttpObservationV2(ctx, encoded, maximum, value); err != nil {
		return nil, nil, "", err
	}
	if err := owner.write(encoded); err != nil {
		return nil, nil, "", err
	}
	return result, observedErr, ReleaseMeasurementContentHash(encoded), owner.check()
}

func (self *ReleaseSteerer) gatherPoolsV2(ctx context.Context, snapshot *ReleaseSnapshot, bound map[uint64]map[connect.Id]bool, hotkeys map[[32]byte]uint16, decision *ReleaseMeasurementArtifact) ([]ReleasePoolMeasurement, []DepositAudit, error) {
	copy := *self
	copy.contexts = make(map[uint64]*ReleaseMeasurementContext, len(self.contexts))
	readers := make(map[uint64]*releaseArtifactCaptureReaderV2, len(self.contexts))
	for noId, measurement := range self.contexts {
		reader, ok := measurement.Artifacts.(*HTTPArtifactReader)
		if !ok {
			return nil, nil, errors.New("V2 pool observation requires actual bounded HTTP transport")
		}
		captured := &releaseArtifactCaptureReaderV2{ctx: ctx, cfg: self.cfg, hotkey: self.hotkey, decision: decision, reader: reader}
		readers[noId] = captured
		owned := *measurement
		owned.Artifacts = captured
		copy.contexts[noId] = &owned
	}
	var pools []ReleasePoolMeasurement
	_, _, audits, err := copy.gatherPools(ctx, snapshot, bound, hotkeys, &pools)
	if err != nil {
		return nil, nil, err
	}
	for index := range audits {
		reader := readers[audits[index].NoID]
		if reader.fatal != nil {
			return nil, nil, reader.fatal
		}
		audits[index].HttpObservationHash = reader.hash
	}
	return pools, audits, ctx.Err()
}

func (self *releaseEvidenceV2StartupHistory) historicalDepositAuditWithCaptureV2(ctx context.Context, observed *releaseDecisionChainV2Observation, operator releaseDecisionChainV2Operator, claimed DepositAudit, decision *ReleaseMeasurementArtifact, custody *releaseEvidenceV2StartupReferences) (DepositAudit, error) {
	if claimed.HttpObservationHash == "" {
		return self.historicalDepositAudit(ctx, observed, operator, claimed.Status)
	}
	if operator.commitment.CommitBlock == 0 || observed.boundary.SettlementEpoch < self.cfg.Policy.Deposit.UsageLagEpochs || custody == nil {
		return DepositAudit{}, errors.New("artifact HTTP capture has no actual committed source request")
	}
	var config *OperatorConfig
	for index := range self.cfg.Operators {
		if self.cfg.Operators[index].NoID == operator.noID {
			config = &self.cfg.Operators[index]
			break
		}
	}
	if config == nil {
		return DepositAudit{}, errors.New("retained artifact HTTP origin is not configured")
	}
	reader, err := NewHTTPArtifactReader(config.APIURL, self.cfg.DeploymentID, self.cfg.Netuid)
	if err != nil {
		return DepositAudit{}, err
	}
	first := self.initial[self.participants[0].NoID].InitialCut
	expected, path, err := releaseArtifactHttpRequestV2(&self.cfg, first.Activation.Hotkey, decision, operator.noID, observed.sourceEpoch, reader)
	if err != nil {
		return DepositAudit{}, err
	}
	maximum := min(self.cfg.EvidenceV2.Bounds.MaxArtifactBytes, self.cfg.EvidenceV2.Bounds.MaxControlBytes/8)
	encoded, err := custody.read(ctx, path, maximum, false)
	if err != nil || ReleaseMeasurementContentHash(encoded) != claimed.HttpObservationHash {
		return DepositAudit{}, errors.Join(errors.New("retained artifact HTTP observation hash or custody differs"), err)
	}
	value, err := decodeArtifactHttpObservationV2(ctx, encoded, maximum, expected)
	if err != nil {
		return DepositAudit{}, err
	}
	artifact, observedErr, err := replayArtifactHttpObservationV2(ctx, reader, value)
	if err != nil {
		return DepositAudit{}, err
	}
	var audit DepositAudit
	epoch := observed.boundary.SettlementEpoch
	if observedErr != nil {
		status := DepositAuditInvalid
		if errors.Is(observedErr, ErrArtifactEquivocation) {
			status = DepositAuditEquivocation
		} else if errors.Is(observedErr, ErrArtifactUnavailable) {
			status = DepositAuditUnavailablePending
			if observed.boundary.EVMBlock > observed.artifactDeadline {
				status = DepositAuditUnavailable
			}
		}
		audit = FailedDepositAudit(epoch, observed.sourceEpoch, operator.noID, operator.deposit, operator.convictionBefore, status, observedErr)
	} else {
		audit = EvaluateDepositArtifact(artifact, DepositArtifactExpectation{DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: self.cfg.GenesisHash, Netuid: self.cfg.Netuid, Coordinator: common.HexToAddress(self.cfg.Coordinator), SettlementVault: common.HexToAddress(self.cfg.SettlementVault), PolicyHash: self.cfg.PolicyHash, Epoch: observed.sourceEpoch, NoID: operator.noID, Signer: common.HexToAddress(config.ArtifactSigner), Start: payoutartifact.Boundary{Number: observed.sourceStart, Hash: releaseHex32(observed.sourceStartHash)}, End: payoutartifact.Boundary{Number: observed.sourceEnd, Hash: releaseHex32(observed.sourceEndHash)}, PayoutRoot: operator.commitment.PayoutRoot, ArtifactHash: operator.commitment.ArtifactHash, Committer: operator.commitment.Committer, RootSigner: operator.sourceVersion.RootSigner, CommitBlock: operator.commitment.CommitBlock}, epoch, operator.deposit, operator.convictionBefore, self.cfg.Policy.Deposit)
	}
	audit.ObservedAtBlock, audit.ArtifactDeadlineBlock, audit.HttpObservationHash = observed.boundary.EVMBlock, observed.artifactDeadline, claimed.HttpObservationHash
	return audit, custody.check()
}
