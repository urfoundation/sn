//go:build linux || darwin

// Live final capture closes the original compact source graph before services
// stop. It proves byte custody and signatures, not policy replay or scoring.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// The enclosing campaign supplies its already approved archive limits. These
// are not new stream, history, upload or protocol capacity defaults.
type ReleaseEvidenceV2CaptureOptions struct {
	Hotkey              [32]byte
	Origins             [2]string
	MaximumBytes        uint64
	MaximumObjects      uint64
	MaximumDataBytes    uint64
	MaximumControlBytes uint64
	ThroughEpoch        uint64
}

// Private names are relative to the validator state, except configured setup
// inputs which use fixed activation/no-N/field names. No secret seed is read
// by capture or passed to this byte-retention callback.
type ReleaseEvidenceV2CaptureSource struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Origin string `json:"origin,omitempty"`
}

// Original intent JSON is a slice from the exact v6 store, not a remarshal.
// Decoded fields route capture only; downstream semantic replay is mandatory.
type ReleaseEvidenceV2CapturedIntent struct {
	Sequence    uint64
	Encoded     []byte
	Intent      SteeringIntent
	Measurement []byte
	Envelope    []byte
}

// This is a raw source handoff, deliberately not a Verified measurement type.
type ReleaseEvidenceV2Capture struct {
	Store        []byte
	Intents      []ReleaseEvidenceV2CapturedIntent
	Closures     []*AttemptSettlementClosureV2
	Publications []ReleaseEvidenceV2CapturedPublication
}

// Independently configured activation and observed geometry accompany the
// actual signed public member for the campaign's existing relay readback.
type ReleaseEvidenceV2CapturedPublication struct {
	Activation protocol.ValidatorEvidenceActivation
	Window     protocol.ValidatorEvidenceWindow
	Evidence   ValidatorEvidenceSignedV2
}

// The sink owns durable output. Complete local observation bytes are emitted
// before the first historical RPC, and no exclusive live ledger is acquired.
func CaptureReleaseEvidenceV2(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, options ReleaseEvidenceV2CaptureOptions, retain func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error) (result *ReleaseEvidenceV2Capture, resultErr error) {
	if ctx == nil || cfg == nil || chain == nil || native == nil || options.Hotkey == ([32]byte{}) || options.MaximumBytes == 0 || options.MaximumObjects == 0 || retain == nil {
		return nil, errors.New("compact final capture owner is incomplete")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(cfg.Operators) != 2 || options.Origins != [2]string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL} {
		return nil, errors.New("compact capture origins differ from the complete configured operator census")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	bounds := cfg.EvidenceV2.Bounds
	keyConfig := *cfg
	keyConfig.Operators = slices.Clone(cfg.Operators)
	budget, err := newReleaseEvidenceCaptureBudgetV2(ctx, options, retain)
	if err != nil {
		return nil, err
	}
	emit := budget.emit
	readPrivate := func(path string, maximum uint64, contentHash string) ([]byte, error) {
		name, err := filepath.Rel(cfg.StateDir, path)
		if err != nil || name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return nil, errors.New("compact capture private source escapes validator state")
		}
		encoded, err := ReadReleaseEvidenceV2SetupFile(ctx, path, maximum)
		if err != nil {
			return nil, err
		}
		if contentHash != "" && ReleaseMeasurementContentHash(encoded) != contentHash {
			return nil, errors.New("compact capture private source hash differs")
		}
		if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "private", Name: filepath.ToSlash(name)}, encoded); err != nil {
			return nil, err
		}
		return encoded, nil
	}
	initials := map[uint64]ReleaseEvidenceV2ActivationContext{}
	activations := make([]protocol.ValidatorEvidenceActivation, 0, len(cfg.EvidenceV2.Operators))
	for _, operator := range cfg.EvidenceV2.Operators {
		fields := []struct {
			name string
			ref  ReleaseEvidenceV2File
		}{
			{name: "activation", ref: operator.Activation}, {name: "vpk-signature", ref: operator.VPKSignature}, {name: "hotkey-signature", ref: operator.HotkeySignature}, {name: "context", ref: operator.Context}, {name: "history", ref: operator.History},
		}
		values := make(map[string][]byte, len(fields))
		for _, field := range fields {
			encoded, err := ReadReleaseEvidenceV2File(ctx, field.ref, bounds.MaxControlBytes)
			if err != nil {
				return nil, err
			}
			if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: fmt.Sprintf("activation/no-%d/%s", operator.NoID, field.name)}, encoded); err != nil {
				return nil, err
			}
			values[field.name] = encoded
		}
		initial, err := decodeReleaseEvidenceV2ActivationContext(values["context"], bounds.Cut.MaxHeaderBytes)
		if err != nil {
			return nil, err
		}
		activation, err := protocol.DecodeValidatorEvidenceActivationPayload(values["activation"])
		if err != nil {
			return nil, err
		}
		if activation != initial.Activation || activation.Hotkey != options.Hotkey || activation.NoID != operator.NoID || initials[operator.NoID].Schema != "" {
			return nil, errors.New("compact capture activation differs from its original context/census")
		}
		if err := validateReleaseMeasurementInputV2Context(cfg, operator.NoID, initial.InitialCut); err != nil {
			return nil, err
		}
		if err := activation.Verify(initial.Activation, values["vpk-signature"], values["hotkey-signature"]); err != nil {
			return nil, err
		}
		initials[operator.NoID] = initial
		activations = append(activations, activation)
	}
	store, err := readPrivate(filepath.Join(cfg.StateDir, "steering-intents.json"), bounds.IntentFileLimit(), "")
	if err != nil {
		return nil, err
	}
	var file struct {
		Schema  string            `json:"schema"`
		Current json.RawMessage   `json:"current"`
		History []json.RawMessage `json:"history"`
	}
	if err := decodeAttemptStreamV2JSON(store, bounds.IntentFileLimit(), &file); err != nil {
		return nil, err
	}
	if file.Schema != SteeringIntentSchema {
		return nil, errors.New("compact capture intent store schema differs")
	}
	rawIntents := append([]json.RawMessage(nil), file.History...)
	if len(file.Current) != 0 && !bytes.Equal(bytes.TrimSpace(file.Current), []byte("null")) {
		rawIntents = append(rawIntents, file.Current)
	}
	if len(rawIntents) == 0 || uint64(len(rawIntents)) > options.MaximumObjects {
		return nil, errors.New("compact capture intent census is empty or exceeds its bound")
	}
	result = &ReleaseEvidenceV2Capture{Store: store}
	var observationChecks []func() error
	var cuts []*AttemptCutV2
	for index, raw := range rawIntents {
		var intent SteeringIntent
		if err := decodeAttemptStreamV2JSON(raw, bounds.IntentFileLimit(), &intent); err != nil {
			return nil, err
		}
		if intent.Schema != SteeringIntentSchema || intent.ValidatorID != cfg.ValidatorID || intent.Netuid != cfg.Netuid || intent.Prepared == nil || intent.Prepared.HotkeyHex != releaseHex32(options.Hotkey) {
			return nil, errors.New("compact capture intent identity/source is incomplete")
		}
		if err := intent.VerifyVectorHash(); err != nil {
			return nil, err
		}
		measurementHash, err := parseReleaseContentHash(intent.MeasurementArtifactHash)
		if err != nil {
			return nil, err
		}
		measurementPath := filepath.ToSlash(filepath.Join("measurements", fmt.Sprintf("%x.json", measurementHash)))
		if intent.MeasurementArtifactPath != measurementPath {
			return nil, errors.New("compact capture measurement path differs from its content address")
		}
		measurement, err := readPrivate(filepath.Join(cfg.StateDir, filepath.FromSlash(measurementPath)), bounds.MaxArtifactBytes, intent.MeasurementArtifactHash)
		if err != nil {
			return nil, err
		}
		artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
		if err != nil {
			return nil, err
		}
		if uint64(len(measurement)) != intent.MeasurementArtifactSize || artifact.DeploymentID != cfg.DeploymentID || artifact.ChainID != cfg.ChainID || artifact.GenesisHash != cfg.GenesisHash || artifact.Coordinator != cfg.Coordinator || artifact.SettlementVault != cfg.SettlementVault || artifact.ValidatorID != cfg.ValidatorID || artifact.Netuid != cfg.Netuid || artifact.PolicyHash != cfg.PolicyHash || artifact.SubnetEpoch != intent.SubnetEpoch || artifact.SettlementEpoch != intent.SettlementEpoch || artifact.SelfUID != intent.SelfUID || artifact.NativeSnapshotBlock != intent.NativeSnapshotBlock || artifact.NativeSnapshotHash != intent.NativeSnapshotHash || artifact.EVMSnapshotBlock != intent.EVMSnapshotBlock || artifact.EVMSnapshotHash != intent.EVMSnapshotHash || !reflect.DeepEqual(artifact.DepositAudits, intent.DepositAudits) {
			return nil, errors.New("compact capture measurement differs from its configured deployment or exact intent")
		}
		envelopeHash, err := parseReleaseContentHash(intent.MeasurementEnvelopeHash)
		if err != nil {
			return nil, err
		}
		envelopePath := filepath.ToSlash(filepath.Join("measurements", "envelopes", fmt.Sprintf("%x.json", envelopeHash)))
		if intent.MeasurementEnvelopePath != envelopePath {
			return nil, errors.New("compact capture envelope path differs from its content address")
		}
		envelopeBytes, err := readPrivate(filepath.Join(cfg.StateDir, filepath.FromSlash(envelopePath)), min(bounds.MaxControlBytes, ReleaseMeasurementEnvelopeV2MaximumBytes), intent.MeasurementEnvelopeHash)
		if err != nil {
			return nil, err
		}
		envelope, err := DecodeReleaseMeasurementEnvelopeV2(ctx, envelopeBytes, bounds.MaxControlBytes)
		if err != nil {
			return nil, err
		}
		if uint64(len(envelopeBytes)) != intent.MeasurementEnvelopeSize || envelope.ValidatorHotkey != releaseHex32(options.Hotkey) || envelope.ValidatorUID != intent.SelfUID || envelope.PreparedExtrinsicHash != intent.Prepared.ExtrinsicHash || envelope.MeasurementArtifactHash != intent.MeasurementArtifactHash || envelope.MeasurementArtifactSize != intent.MeasurementArtifactSize || !releaseMeasurementEnvelopeMatchesArtifact(envelope, artifact, intent.SelfUID) {
			return nil, errors.New("compact capture envelope does not sign the exact original measurement/prepared source")
		}
		result.Intents = append(result.Intents, ReleaseEvidenceV2CapturedIntent{Sequence: uint64(index + 1), Encoded: bytes.Clone(raw), Intent: intent, Measurement: measurement, Envelope: envelopeBytes})
		for _, input := range artifact.Inputs {
			if input.AttemptCutV2 == nil {
				return nil, errors.New("compact capture input lacks its typed cut")
			}
			cuts = append(cuts, input.AttemptCutV2)
		}
		var keyChecks []releaseClientKeyCapturedV2Response
		for _, binding := range artifact.Bindings {
			if binding.ClientKeyObservationHash == "" {
				if binding.Active {
					return nil, errReleaseHistoricalClientKeyV2
				}
				continue
			}
			clientId, err := connect.ParseId(binding.ClientID)
			if err != nil {
				return nil, err
			}
			domain, request, err := releaseClientKeyDecisionV2(cfg, binding.NoID, options.Hotkey, artifact, clientId)
			if err != nil {
				return nil, err
			}
			path, err := releaseClientKeyCaptureV2Path(cfg.StateDir, domain, request)
			if err != nil {
				return nil, err
			}
			maximum := min(bounds.MaxArtifactBytes, bounds.MaxControlBytes/8, uint64(protocol.MaxClientKeyHistoryResponseBytes))
			encoded, err := readPrivate(path, maximum, binding.ClientKeyObservationHash)
			if err != nil {
				return nil, err
			}
			keyChecks = append(keyChecks, releaseClientKeyCapturedV2Response{encoded: encoded, maximum: maximum, domain: domain, request: request})
		}
		if len(keyChecks) != 0 {
			observationChecks = append(observationChecks, func() error {
				return verifyReleaseClientKeyCapturedResponsesV2(ctx, chain, &keyConfig, artifact, options.Hotkey, keyChecks, bounds.MaxControlBytes)
			})
		}
		for _, audit := range artifact.DepositAudits {
			if audit.HttpObservationHash == "" {
				continue
			}
			var operator *OperatorConfig
			for index := range cfg.Operators {
				if cfg.Operators[index].NoID == audit.NoID {
					operator = &cfg.Operators[index]
					break
				}
			}
			if operator == nil {
				return nil, errors.New("compact capture audit operator is not configured")
			}
			reader, err := NewHTTPArtifactReader(operator.APIURL, cfg.DeploymentID, cfg.Netuid)
			if err != nil {
				return nil, err
			}
			expected, path, err := releaseArtifactHttpRequestV2(cfg, options.Hotkey, artifact, audit.NoID, audit.SourceEpoch, reader)
			if err != nil {
				return nil, err
			}
			maximum := min(bounds.MaxArtifactBytes, bounds.MaxControlBytes/8)
			encoded, err := readPrivate(path, maximum, audit.HttpObservationHash)
			if err != nil {
				return nil, err
			}
			if _, err := decodeArtifactHttpObservationV2(ctx, encoded, maximum, expected); err != nil {
				return nil, err
			}
		}
	}
	// Inventory immutable controls once. Later producer appends are not this
	// capture's semantic boundary; selected accepted epochs remain mandatory.
	history, err := readReleaseEvidenceV2HistoryFiles(ctx, cfg.StateDir, bounds, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		return nil, err
	}
	if err := errors.Join(history.check(), history.close()); err != nil {
		return nil, err
	}
	for _, input := range history.inputs {
		if input.legacy {
			return nil, errors.New("compact capture encountered an unsupported legacy migration input")
		}
		if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "private", Name: filepath.ToSlash(filepath.Join("measurements", "inputs", input.name))}, input.encoded); err != nil {
			return nil, err
		}
		var wire releaseMeasurementInputV2Wire
		if err := decodeAttemptStreamV2JSON(input.encoded, bounds.MaxInputJournalBytes, &wire); err != nil {
			return nil, err
		}
		if wire.Schema != releaseMeasurementInputV2Schema || wire.SubnetEpoch != input.epoch || wire.MeasurementInput.NoID != input.noID || wire.MeasurementInput.AttemptCutV2 == nil {
			return nil, errors.New("compact capture input journal routing differs")
		}
		cuts = append(cuts, wire.MeasurementInput.AttemptCutV2)
	}
	manifests := make([]*ValidatorEvidencePublicationV2Manifest, 0, len(history.closures))
	for _, member := range history.closures {
		if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "private", Name: filepath.ToSlash(filepath.Join("settlement-closures-v2", member.name))}, member.encoded); err != nil {
			return nil, err
		}
		closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, member.encoded, bounds.MaxClosureBytes, bounds.MaxParticipants)
		if err != nil {
			return nil, err
		}
		if closure.Schema != AttemptSettlementClosureV2Schema || closure.Epoch != member.epoch || len(closure.Transitions) != len(activations) {
			return nil, errors.New("compact capture closure has a partial or misrouted source census")
		}
		for index, transition := range closure.Transitions {
			if transition.Identity.NoID != activations[index].NoID || transition.FromBoundary.SettlementEpoch != closure.Epoch {
				return nil, errors.New("compact capture closure member order differs")
			}
			cuts = append(cuts, &transition.Cut)
		}
		// All retained controls need their signed chunks for complete history
		// replay, including a producer append beyond the accepted window. Only
		// accepted-window publication selection stops at ThroughEpoch.
		if member.epoch > options.ThroughEpoch {
			continue
		}
		path, err := ValidatorEvidencePublicationV2ManifestPath(cfg.StateDir, member.epoch)
		if err != nil {
			return nil, err
		}
		if _, err := readPrivate(path, bounds.MaxClosureBytes, ""); err != nil {
			return nil, err
		}
		manifest, err := ReadValidatorEvidencePublicationV2Manifest(ctx, path, bounds.MaxClosureBytes, bounds.MaxParticipants)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
		result.Closures = append(result.Closures, closure)
	}
	auditManifests, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(ctx, cfg.StateDir, bounds)
	if err != nil {
		return nil, err
	}
	auditSubjects := map[protocol.ValidatorEvidenceSubject]bool{}
	for _, manifest := range auditManifests {
		if manifest.Subject.ObservationEpoch > options.ThroughEpoch {
			continue
		}
		path, err := ValidatorEvidenceDepositAuditV2ManifestPath(cfg.StateDir, manifest.Epoch, manifest.Subject)
		if err != nil {
			return nil, err
		}
		if _, err := readPrivate(path, bounds.MaxClosureBytes, ""); err != nil {
			return nil, err
		}
		preparedPath := filepath.Join(cfg.StateDir, "evidence-deposit-audit-prepared", filepath.Base(path))
		if _, err := readPrivate(preparedPath, bounds.MaxHistoryBytes, ""); err != nil {
			return nil, err
		}
		auditSubjects[manifest.Subject] = true
	}
	for _, captured := range result.Intents {
		intent := &captured.Intent
		if intent.SettlementEpoch > options.ThroughEpoch || intent.SettlementEpoch < cfg.Policy.Deposit.UsageLagEpochs {
			continue
		}
		subject := protocol.ValidatorEvidenceSubject{ObservationEpoch: intent.SettlementEpoch, NativeEpoch: intent.SubnetEpoch}
		if !auditSubjects[subject] {
			return nil, errors.New("compact capture lacks the later-audit slot required by an original durable intent")
		}
	}
	// Local source and signed observations are durable before historical state
	// requests. Native result bytes include the original atomic source storage.
	readIndex := uint64(0)
	for index := range result.Intents {
		captured := &result.Intents[index]
		if err := CaptureReleaseNativeSourceV2(ctx, native, cfg, &captured.Intent, captured.Measurement, func(ctx context.Context, read ReleaseEvidenceV2NativeRead) error {
			encoded, err := json.Marshal(read)
			if err != nil {
				return err
			}
			readIndex++
			return emit(ReleaseEvidenceV2CaptureSource{Kind: "native-rpc", Name: fmt.Sprintf("read-%020d.json", readIndex)}, append(encoded, '\n'))
		}); err != nil {
			return nil, err
		}
	}
	if _, err := readReleaseServerKeysV2WithCapture(ctx, cfg, emit); err != nil {
		return nil, err
	}
	if err := captureReleaseDecisionObservationsV2(ctx, cfg, chain, native, initials, history, result.Intents, emit); err != nil {
		return nil, err
	}
	for _, check := range observationChecks {
		if err := check(); err != nil {
			return nil, err
		}
	}
	for _, cut := range cuts {
		initial, found := initials[cut.Context.Identity.NoID]
		if !found || cut.Context.Identity != initial.InitialCut.Identity || cut.Context.Activation != initial.InitialCut.Activation {
			return nil, errors.New("compact capture cut changes the configured activation identity")
		}
		// Signature custody only. Cursor, clocks, priors, counts and projection
		// are deliberately left to the independent complete offline replay.
		if err := cut.VerifyHeader(cut.Context, bounds.Cut); err != nil {
			return nil, err
		}
		for _, origin := range options.Origins {
			reader, err := NewHTTPAttemptStreamV2Reader(origin, bounds.Cut)
			if err != nil {
				return nil, err
			}
			for _, stream := range []struct {
				kind      string
				reference AttemptStreamV2Reference
				limits    AttemptStreamV2Bounds
			}{{kind: AttemptStreamV2Records, reference: cut.Records, limits: bounds.Cut.Records}, {kind: AttemptStreamV2Proofs, reference: cut.Proofs, limits: bounds.Cut.Proofs}} {
				_, err := WalkAttemptStreamV2Descriptors(ctx, stream.kind, stream.reference, stream.limits, func(ctx context.Context, hash string, size uint64) ([]byte, error) {
					encoded, err := reader.ReadMetadata(ctx, hash, size)
					if err != nil {
						return nil, err
					}
					if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "metadata", Name: hash, Origin: origin}, encoded); err != nil {
						return nil, err
					}
					return encoded, nil
				}, func(chunk AttemptStreamV2Chunk) error {
					body, err := reader.OpenData(ctx, stream.kind, chunk.ContentHash, chunk.DataBytes)
					if err != nil {
						return err
					}
					encoded, readErr := io.ReadAll(body)
					if err := errors.Join(readErr, body.Close(), ctx.Err()); err != nil {
						return err
					}
					return emit(ReleaseEvidenceV2CaptureSource{Kind: stream.kind, Name: chunk.ContentHash, Origin: origin}, encoded)
				})
				if err != nil {
					return nil, err
				}
			}
		}
	}
	block, hash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	for index, manifest := range manifests {
		epoch := new(big.Int).SetUint64(manifest.Epoch)
		start, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, epoch)
		if err != nil {
			return nil, err
		}
		end, err := chain.ReleaseEpochEndBlockAtHashContext(ctx, block, hash, epoch)
		if err != nil {
			return nil, err
		}
		window := protocol.ValidatorEvidenceWindow{Epoch: manifest.Epoch, StartBlock: start, EndBlock: end, FinalizedBlock: block}
		publication, err := ReadValidatorEvidencePublicationV2(ctx, manifest, ValidatorEvidencePublicationV2ReadOptions{Activations: activations, Window: window, Origins: options.Origins, Bounds: bounds})
		if err != nil {
			return nil, err
		}
		for index, member := range publication.Members {
			result.Publications = append(result.Publications, ReleaseEvidenceV2CapturedPublication{Activation: activations[index], Window: window, Evidence: member.Evidence})
		}
		for _, origin := range options.Origins {
			if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "closed-census", Name: attemptHex32(publication.CensusHash), Origin: origin}, publication.Census); err != nil {
				return nil, err
			}
			for memberIndex, member := range publication.Members {
				transition, err := json.Marshal(result.Closures[index].Transitions[memberIndex])
				if err != nil || !bytes.Equal(append(transition, '\n'), member.Payload) {
					return nil, errors.Join(errors.New("public terminal payload differs from original private closure"), err)
				}
				if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "signed-evidence", Name: attemptHex32(member.SignedArtifactHash), Origin: origin}, member.SignedArtifact); err != nil {
					return nil, err
				}
				if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "terminal-payload", Name: attemptHex32(sha256.Sum256(member.Payload)), Origin: origin}, member.Payload); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, manifest := range auditManifests {
		if manifest.Subject.ObservationEpoch > options.ThroughEpoch {
			continue
		}
		epoch := new(big.Int).SetUint64(manifest.Epoch)
		start, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, epoch)
		if err != nil {
			return nil, err
		}
		end, err := chain.ReleaseEpochEndBlockAtHashContext(ctx, block, hash, epoch)
		if err != nil {
			return nil, err
		}
		window := protocol.ValidatorEvidenceWindow{Epoch: manifest.Epoch, Subject: manifest.Subject, StartBlock: start, EndBlock: end, FinalizedBlock: block}
		publication, err := ReadValidatorEvidenceDepositAuditV2(ctx, &manifest, ValidatorEvidencePublicationV2ReadOptions{Activations: activations, Window: window, Origins: options.Origins, Bounds: bounds})
		if err != nil {
			return nil, err
		}
		for index, member := range publication.Members {
			result.Publications = append(result.Publications, ReleaseEvidenceV2CapturedPublication{Activation: activations[index], Window: window, Evidence: member.Evidence})
		}
		for _, origin := range options.Origins {
			if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "audit-census", Name: attemptHex32(publication.CensusHash), Origin: origin}, publication.Census); err != nil {
				return nil, err
			}
			for _, member := range publication.Members {
				if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "signed-audit", Name: attemptHex32(member.SignedArtifactHash), Origin: origin}, member.SignedArtifact); err != nil {
					return nil, err
				}
				if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "audit-payload", Name: attemptHex32(sha256.Sum256(member.Payload)), Origin: origin}, member.Payload); err != nil {
					return nil, err
				}
			}
		}
	}
	return result, ctx.Err()
}
