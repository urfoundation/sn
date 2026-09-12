//go:build linux || darwin

// Native intent references cannot hide a missing immutable statistics input.
// This bounded reader authenticates custody, provenance and independently read
// chain source facts, not head decisions or permission to submit prepared weights.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/crv4"
)

// Every referenced path remains physically owned through startup publication.
// Empty history retains actual steering-intents.json absence without creation.
type releaseEvidenceV2StartupReferences struct {
	owners    []*releaseMeasurementInputV2Owner
	remaining uint64
	closed    bool
	closeErr  error
}

// Path policy is fixed by the caller below. The existing descriptor reader
// enforces private regular files, exact bounded reads and joined real closes.
func (self *releaseEvidenceV2StartupReferences) read(ctx context.Context, path string, limit uint64, absent bool) ([]byte, error) {
	if self.closed || limit == 0 || self.remaining == 0 && !absent {
		return nil, errors.New("startup reference byte owner is exhausted or closed")
	}
	readLimit := min(limit, self.remaining)
	if readLimit == 0 {
		readLimit = limit
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, readLimit, releaseMeasurementInputV2ReadHooks{}, false)
	self.owners = append(self.owners, owner)
	if err != nil {
		return nil, err
	}
	if self.remaining == 0 {
		// A fully used byte allowance can still prove actual absence without
		// reading a single extra byte or treating an empty file as missing.
		_, err := owner.directory.stat(owner.name)
		if !releaseMeasurementInputV2OnlyMissing(err) {
			return nil, errors.Join(errors.New("startup exhausted history allowance has an occupied intent file"), err)
		}
		owner.observed, owner.initialMissing = true, true
		return nil, owner.check()
	}
	encoded, err := owner.read()
	if err != nil {
		if absent && owner.initialMissing && releaseMeasurementInputV2OnlyMissing(err) {
			return nil, owner.check()
		}
		return nil, err
	}
	self.remaining -= uint64(len(encoded))
	return encoded, owner.check()
}

// No callback is allowed to turn a shortened or retargeted file into the same
// authenticated reference set. Late errors survive all owner releases.
func (self *releaseEvidenceV2StartupReferences) check() error {
	if self == nil || self.closed {
		return errors.New("startup reference owner is closed")
	}
	var err error
	for _, owner := range self.owners {
		err = errors.Join(err, owner.check())
	}
	return err
}

func (self *releaseEvidenceV2StartupReferences) close() error {
	if self == nil {
		return nil
	}
	if self.closed {
		return self.closeErr
	}
	self.closed = true
	for index := len(self.owners) - 1; index >= 0; index-- {
		self.closeErr = errors.Join(self.closeErr, self.owners[index].finish())
	}
	return self.closeErr
}

// Both wire schemas are explicit. A V2 artifact's bindings are not copied into
// ReleaseMeasurementV2Options as fabricated authority; all statistics inputs
// instead have to equal the already fully replayed, independently pinned cut.
func (self *releaseEvidenceV2StartupHistory) readIntentReferences(ctx context.Context, chain *ChainClient, native *crv4.Chain, runtime crv4.RuntimeArtifactIdentity) (result *releaseEvidenceV2StartupReferences, resultErr error) {
	bounds := self.cfg.EvidenceV2.Bounds
	owned := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owned.close())
			result = nil
		}
	}()
	// Charge all immutable namespace bytes, including unpromoted crash temps,
	// against the same complete-history allowance before reference allocation.
	for _, directory := range self.files.directories {
		for _, state := range directory.entries {
			if state.size < 0 || uint64(state.size) > owned.remaining {
				return nil, errors.New("startup combined immutable/reference history exceeds its byte bound")
			}
			owned.remaining -= uint64(state.size)
		}
	}
	encoded, err := owned.read(ctx, filepath.Join(self.cfg.StateDir, "steering-intents.json"), bounds.IntentFileLimit(), true)
	if err != nil {
		return nil, err
	}
	if encoded == nil {
		return owned, owned.check()
	}
	var file steeringIntentFile
	if err := decodeAttemptStreamV2JSON(encoded, bounds.IntentFileLimit(), &file); err != nil {
		return nil, err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, &file, bounds.IntentFileLimit(), true, true)
	if err != nil || file.Schema != steeringIntentSchema || !bytes.Equal(encoded, canonical) {
		return nil, errors.Join(errors.New("startup intent reference file is not canonical"), err)
	}
	all := make([]*SteeringIntent, 0, len(file.History)+1)
	for index := range file.History {
		all = append(all, &file.History[index])
	}
	if file.Current != nil {
		all = append(all, file.Current)
	}
	seen := map[string]bool{}
	var previous *SteeringIntent
	var priorArtifact *ReleaseMeasurementArtifact
	// A retained provisional testnet startup may legitimately omit native
	// submissions from epochs that were closed before this process resumed.
	// The live runtime still authenticates every terminal around the gap; this
	// flag only prevents the read-only startup reference pass from rejecting
	// that already authenticated retained history too early.
	allowProvisionalGaps := self.retainedStartup && provisionalClosedNativeInputEnabled(&self.cfg)
	for index, intent := range all {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateSteeringIntentLifecycle(intent, index < len(file.History)); err != nil {
			return nil, err
		}
		if err := intent.VerifyVectorHash(); err != nil {
			return nil, err
		}
		if seen[intent.VectorHash] {
			return nil, errors.New("startup intent references repeat a vector identity")
		}
		seen[intent.VectorHash] = true
		if previous != nil {
			if err := validateSteeringIntentSuccessorWithGapsV2(previous, intent, allowProvisionalGaps); err != nil {
				return nil, err
			}
		}
		artifactBytes, err := self.readContentReference(ctx, owned, intent.MeasurementArtifactPath, intent.MeasurementArtifactHash, intent.MeasurementArtifactSize, bounds.MaxArtifactBytes, false)
		if err != nil {
			return nil, err
		}
		envelopeBytes, err := self.readContentReference(ctx, owned, intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize, bounds.MaxControlBytes, true)
		if err != nil {
			return nil, err
		}
		var label struct {
			Schema string `json:"schema"`
		}
		// Only this one field selects an explicitly implemented wire branch;
		// the branch's full strict decoder and signature domain follow below.
		if err := decodeStartupReferenceSchema(artifactBytes, &label); err != nil {
			return nil, err
		}
		var artifact *ReleaseMeasurementArtifact
		var envelope *ReleaseMeasurementEnvelope
		switch label.Schema {
		case ReleaseMeasurementSchemaV2:
			artifact, err = decodeReleaseMeasurementV2Bytes(ctx, artifactBytes, bounds.MaxArtifactBytes, bounds.MaxOperators)
			if err == nil {
				envelope, err = DecodeReleaseMeasurementEnvelopeV2(ctx, envelopeBytes, bounds.MaxControlBytes)
			}
		case ReleaseMeasurementSchema:
			artifact, _, err = DecodeReleaseMeasurementArtifact(artifactBytes)
			if err == nil {
				envelope, err = DecodeReleaseMeasurementEnvelope(envelopeBytes)
			}
		default:
			return nil, errors.New("startup reference artifact schema is unsupported")
		}
		if err != nil {
			return nil, err
		}
		if err := self.matchIntentReference(ctx, intent, artifact, envelope); err != nil {
			return nil, err
		}
		if previous == nil {
			if artifact.PreviousArtifactHash != "" {
				return nil, errors.New("startup intent history omits its first predecessor")
			}
		} else {
			if artifact.PreviousArtifactHash != previous.MeasurementArtifactHash {
				return nil, errors.New("startup intent artifact predecessor differs")
			}
			if priorArtifact.Schema == ReleaseMeasurementSchemaV2 && artifact.Schema != ReleaseMeasurementSchemaV2 ||
				artifact.SettlementEpoch < priorArtifact.SettlementEpoch ||
				!allowProvisionalGaps && priorArtifact.SettlementEpoch != ^uint64(0) && artifact.SettlementEpoch > priorArtifact.SettlementEpoch+1 ||
				!releaseBlockAtOrBefore(priorArtifact.NativeSnapshotBlock, priorArtifact.NativeSnapshotHash, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash) ||
				!releaseBlockAtOrBefore(priorArtifact.EVMSnapshotBlock, priorArtifact.EVMSnapshotHash, artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash) {
				return nil, errors.New("startup signed artifact lineage regresses or skips a boundary")
			}
			if err := verifyReleaseMeasurementHeadLineage(priorArtifact, artifact); err != nil {
				return nil, err
			}
		}
		if !self.retainedStartup {
			if err := self.authenticateIntentChainReferenceWithKeyCustody(ctx, chain, native, runtime, intent, artifact, owned); err != nil {
				return nil, err
			}
		}
		previous, priorArtifact = intent, artifact
	}
	return owned, owned.check()
}

// Hash-derived namespace grammar precedes the first parent lookup; a declared
// size can narrow but never enlarge either the per-file or aggregate bound.
func (self *releaseEvidenceV2StartupHistory) readContentReference(ctx context.Context, owned *releaseEvidenceV2StartupReferences, path, hash string, size, limit uint64, envelope bool) ([]byte, error) {
	if _, err := parseReleaseContentHash(hash); err != nil {
		return nil, err
	}
	parts := []string{"measurements"}
	if envelope {
		limit = min(limit, ReleaseMeasurementEnvelopeV2MaximumBytes)
		parts = append(parts, "envelopes")
	}
	parts = append(parts, strings.TrimPrefix(hash, "sha256:")+".json")
	want := filepath.ToSlash(filepath.Join(parts...))
	if path != want || size == 0 || size > limit || size > owned.remaining {
		return nil, errors.New("startup content reference path or finite size differs")
	}
	encoded, err := owned.read(ctx, filepath.Join(self.cfg.StateDir, filepath.FromSlash(want)), size, false)
	if err != nil || uint64(len(encoded)) != size || ReleaseMeasurementContentHash(encoded) != hash {
		return nil, errors.Join(errors.New("startup content reference bytes differ from the signed content address"), err)
	}
	return encoded, nil
}

// Signature verification was performed by the explicit envelope decoder.
// Mirror fields and every statistical cut must match independently replayed
// inputs. This function deliberately does not return a verified decision.
func (self *releaseEvidenceV2StartupHistory) matchIntentReference(ctx context.Context, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact, envelope *ReleaseMeasurementEnvelope) error {
	if artifact == nil || envelope == nil || intent == nil || intent.Prepared == nil {
		return errors.New("startup intent reference is incomplete")
	}
	if err := verifyReleaseMeasurementCommonIdentity(artifact); err != nil {
		return err
	}
	if uint64(len(artifact.HeadEMA)) > self.cfg.EvidenceV2.Bounds.MaxHeadEntries {
		return errors.New("startup reference head transcript exceeds its finite census")
	}
	first := self.initial[self.participants[0].NoID].InitialCut
	if artifact.DeploymentID != self.cfg.DeploymentID || artifact.ChainID != self.cfg.ChainID || artifact.GenesisHash != strings.ToLower(self.cfg.GenesisHash) || artifact.Coordinator != strings.ToLower(self.cfg.Coordinator) || artifact.SettlementVault != strings.ToLower(self.cfg.SettlementVault) || artifact.ValidatorID != self.cfg.ValidatorID || artifact.Netuid != self.cfg.Netuid || artifact.PolicyHash != strings.ToLower(self.cfg.PolicyHash) || artifact.ValidatorID != intent.ValidatorID || artifact.Netuid != intent.Netuid || artifact.SubnetEpoch != intent.SubnetEpoch || artifact.SettlementEpoch != intent.SettlementEpoch || artifact.PolicyHash != intent.PolicyHash || artifact.NativeSnapshotBlock != intent.NativeSnapshotBlock || artifact.NativeSnapshotHash != intent.NativeSnapshotHash || artifact.EVMSnapshotBlock != intent.EVMSnapshotBlock || artifact.EVMSnapshotHash != intent.EVMSnapshotHash || artifact.SelfUID != intent.SelfUID {
		return errors.New("startup intent reference differs from configured deployment or signed coordinates")
	}
	if !releaseMeasurementEnvelopeMatchesArtifact(envelope, artifact, intent.SelfUID) || envelope.ValidatorHotkey != attemptHex32(first.Activation.Hotkey) || intent.Prepared.HotkeyHex != envelope.ValidatorHotkey || intent.Prepared.Netuid != intent.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || !releaseBlockAtOrBefore(artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash, intent.Prepared.PreparedAtBlock, intent.Prepared.PreparedAtBlockHash) || envelope.PreparedExtrinsicHash != normalizeReleasePreparedHex32(intent.Prepared.ExtrinsicHash, canonicalHexWork{}) || envelope.MeasurementArtifactHash != intent.MeasurementArtifactHash || envelope.MeasurementArtifactSize != intent.MeasurementArtifactSize {
		return errors.New("startup intent reference differs from its actual hotkey envelope")
	}
	if len(artifact.Inputs) != len(self.participants) {
		return errors.New("startup referenced artifact omits the configured input census")
	}
	for index, input := range artifact.Inputs {
		if input.NoID != self.participants[index].NoID {
			return errors.New("startup referenced artifact changes the configured operator order")
		}
		journal := self.inputByEpoch[artifact.SubnetEpoch][input.NoID]
		if journal == nil {
			return errors.New("startup intent references a missing immutable native input")
		}
		want, err := marshalAttemptSettlementV2JSON(ctx, journal.MeasurementInput, self.cfg.EvidenceV2.Bounds.MaxInputJournalBytes, false, false)
		if err != nil {
			return err
		}
		actual, err := marshalAttemptSettlementV2JSON(ctx, input, self.cfg.EvidenceV2.Bounds.MaxInputJournalBytes, false, false)
		if err != nil || !bytes.Equal(actual, want) {
			return errors.Join(errors.New("startup artifact input differs from its independently replayed immutable journal"), err)
		}
		if (artifact.Schema == ReleaseMeasurementSchemaV2) != (journal.Schema == releaseMeasurementInputV2Schema) || input.SettlementEpoch != artifact.SettlementEpoch || !releaseBlockAtOrBefore(input.CutNativeBlock, input.CutNativeBlockHash, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash) || !releaseBlockAtOrBefore(input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash, artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash) {
			return errors.New("startup artifact input schema or cut chronology differs")
		}
	}
	if closure := artifact.SettlementClosureV2; closure != nil {
		known := self.terminals[closure.Epoch]
		if known == nil || closure.Epoch == ^uint64(0) || closure.Epoch+1 != artifact.SettlementEpoch {
			return errors.New("startup referenced terminal is absent from complete authenticated history")
		}
		want, err := marshalAttemptSettlementV2JSON(ctx, known, self.cfg.EvidenceV2.Bounds.MaxClosureBytes, false, true)
		if err != nil {
			return err
		}
		actual, err := marshalAttemptSettlementV2JSON(ctx, closure, self.cfg.EvidenceV2.Bounds.MaxClosureBytes, false, true)
		if err != nil || !bytes.Equal(actual, want) {
			return errors.Join(errors.New("startup referenced terminal rewrites complete authenticated history"), err)
		}
	}
	return ctx.Err()
}

// A small schema probe permits extra fields only here; the selected complete
// decoder immediately rejects unknown fields, alternate bytes and bad domains.
func decodeStartupReferenceSchema(encoded []byte, label any) error {
	if err := json.Unmarshal(encoded, label); err != nil {
		return fmt.Errorf("startup reference schema: %w", err)
	}
	return nil
}
