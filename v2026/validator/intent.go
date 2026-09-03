package validator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// SteeringIntentSchema identifies release intents that cryptographically bind
// their independently replayable measurement artifact.
const SteeringIntentSchema = "urnetwork-validator-steering-intent-v6"

const steeringIntentSchema = SteeringIntentSchema

var (
	ErrSteeringIntentPending = errors.New("a steering intent for this subnet epoch is already pending")
	ErrSteeringAlreadyFinal  = errors.New("weights for this subnet epoch are already finalized")
)

type RationalJSON struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}

type StaleHeadBinding struct {
	NoID      uint64 `json:"no_id"`
	ClientID  string `json:"client_id"`
	RecordUID uint16 `json:"record_uid"`
	LiveUID   uint16 `json:"live_uid"`
	Found     bool   `json:"found"`
}

type SteeringIntent struct {
	Schema                  string                   `json:"schema"`
	ValidatorID             uint64                   `json:"validator_id"`
	Netuid                  uint16                   `json:"netuid"`
	SubnetEpoch             uint64                   `json:"subnet_epoch"`
	NativeSnapshotBlock     uint64                   `json:"native_snapshot_block"`
	NativeSnapshotHash      string                   `json:"native_snapshot_hash"`
	EVMSnapshotBlock        uint64                   `json:"evm_snapshot_block"`
	EVMSnapshotHash         string                   `json:"evm_snapshot_hash"`
	SettlementEpoch         uint64                   `json:"settlement_epoch"`
	PolicyHash              string                   `json:"policy_hash"`
	MeasurementArtifactPath string                   `json:"measurement_artifact_path"`
	MeasurementArtifactHash string                   `json:"measurement_artifact_hash"`
	MeasurementArtifactSize uint64                   `json:"measurement_artifact_size"`
	MeasurementEnvelopePath string                   `json:"measurement_envelope_path"`
	MeasurementEnvelopeHash string                   `json:"measurement_envelope_hash"`
	MeasurementEnvelopeSize uint64                   `json:"measurement_envelope_size"`
	SelfUID                 uint16                   `json:"self_uid"`
	MaskedUIDs              []uint16                 `json:"masked_uids"`
	EligibleHeadUIDs        []uint16                 `json:"eligible_head_uids"`
	EligibleHeadScores      []RationalJSON           `json:"eligible_head_scores,omitempty"`
	SelectedHeadUIDs        []uint16                 `json:"selected_head_uids"`
	RejectedHeadUIDs        []uint16                 `json:"rejected_head_uids"`
	StaleHeadBindings       []StaleHeadBinding       `json:"stale_head_bindings"`
	DepositAudits           []DepositAudit           `json:"deposit_audits"`
	UIDs                    []uint16                 `json:"uids"`
	Scores                  []RationalJSON           `json:"scores"`
	Prepared                *crv4.PreparedSubmission `json:"prepared"`
	Values                  []uint16                 `json:"values,omitempty"`
	VectorHash              string                   `json:"vector_hash"`
	Status                  string                   `json:"status"`
	CreatedAt               string                   `json:"created_at"`
	UpdatedAt               string                   `json:"updated_at"`
	ExtrinsicHash           string                   `json:"extrinsic_hash,omitempty"`
	FinalizedBlock          uint64                   `json:"finalized_block,omitempty"`
	FinalizedBlockHash      string                   `json:"finalized_block_hash,omitempty"`
	RevealBlock             uint64                   `json:"reveal_block,omitempty"`
	ApplicationBlock        uint64                   `json:"application_block,omitempty"`
	ApplicationBlockHash    string                   `json:"application_block_hash,omitempty"`
	Error                   string                   `json:"error,omitempty"`
}

type steeringIntentFile struct {
	Schema  string           `json:"schema"`
	Current *SteeringIntent  `json:"current,omitempty"`
	History []SteeringIntent `json:"history"`
}

type IntentStore struct {
	mu       sync.Mutex
	path     string
	stateDir string
}

func NewIntentStore(stateDir string) (*IntentStore, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("intent state directory must be absolute")
	}
	if err := ensurePrivateStateDir(stateDir); err != nil {
		return nil, err
	}
	return &IntentStore{path: filepath.Join(stateDir, "steering-intents.json"), stateDir: stateDir}, nil
}

// readMeasurementArtifactLocked resolves only the content-addressed path that
// can be derived from the declared hash. The caller must hold the store lock.
func (s *IntentStore) readMeasurementArtifactLocked(intent *SteeringIntent) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, error) {
	if intent == nil || intent.MeasurementArtifactSize == 0 || intent.MeasurementArtifactSize > 64*1024*1024 {
		return nil, nil, errors.New("steering intent measurement artifact size is invalid")
	}
	if _, err := parseReleaseContentHash(intent.MeasurementArtifactHash); err != nil {
		return nil, nil, fmt.Errorf("steering intent measurement artifact hash: %w", err)
	}
	expectedPath := filepath.ToSlash(filepath.Join("measurements", strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:")+".json"))
	if intent.MeasurementArtifactPath != expectedPath || filepath.IsAbs(intent.MeasurementArtifactPath) || filepath.Clean(filepath.FromSlash(intent.MeasurementArtifactPath)) != filepath.FromSlash(intent.MeasurementArtifactPath) {
		return nil, nil, errors.New("steering intent measurement artifact path is not canonical")
	}
	absolutePath := filepath.Join(s.stateDir, filepath.FromSlash(intent.MeasurementArtifactPath))
	info, err := os.Lstat(absolutePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || uint64(info.Size()) != intent.MeasurementArtifactSize {
		return nil, nil, errors.New("steering intent measurement artifact is not the expected private regular file")
	}
	encoded, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, nil, err
	}
	if ReleaseMeasurementContentHash(encoded) != intent.MeasurementArtifactHash {
		return nil, nil, errors.New("steering intent measurement artifact content hash differs")
	}
	artifact, verified, err := DecodeReleaseMeasurementArtifact(encoded)
	if err != nil {
		return nil, nil, err
	}
	if err := s.verifyMeasurementEnvelopeLocked(intent, encoded); err != nil {
		return nil, nil, err
	}
	return artifact, verified, nil
}

func (s *IntentStore) verifyMeasurementEnvelopeLocked(intent *SteeringIntent, measurement []byte) error {
	if intent == nil || intent.Prepared == nil || intent.MeasurementEnvelopeSize == 0 || intent.MeasurementEnvelopeSize > 1024*1024 {
		return errors.New("steering intent measurement envelope size is invalid")
	}
	if _, err := parseReleaseContentHash(intent.MeasurementEnvelopeHash); err != nil {
		return fmt.Errorf("steering intent measurement envelope hash: %w", err)
	}
	expectedPath := filepath.ToSlash(filepath.Join("measurements", "envelopes", strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:")+".json"))
	if intent.MeasurementEnvelopePath != expectedPath || filepath.IsAbs(intent.MeasurementEnvelopePath) || filepath.Clean(filepath.FromSlash(intent.MeasurementEnvelopePath)) != filepath.FromSlash(intent.MeasurementEnvelopePath) {
		return errors.New("steering intent measurement envelope path is not canonical")
	}
	absolutePath := filepath.Join(s.stateDir, filepath.FromSlash(intent.MeasurementEnvelopePath))
	info, err := os.Lstat(absolutePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || uint64(info.Size()) != intent.MeasurementEnvelopeSize {
		return errors.New("steering intent measurement envelope is not the expected private regular file")
	}
	encoded, err := os.ReadFile(absolutePath)
	if err != nil {
		return err
	}
	if ReleaseMeasurementEnvelopeContentHash(encoded) != intent.MeasurementEnvelopeHash {
		return errors.New("steering intent measurement envelope content hash differs")
	}
	envelope, err := DecodeReleaseMeasurementEnvelope(encoded)
	if err != nil {
		return err
	}
	hotkey, err := parseReleaseHex32("prepared validator hotkey", strings.ToLower(intent.Prepared.HotkeyHex), false)
	if err != nil || intent.Prepared.HotkeyHex != strings.ToLower(intent.Prepared.HotkeyHex) {
		return errors.New("steering intent prepared validator hotkey is not canonical")
	}
	if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, measurement, hotkey, intent.SelfUID, strings.ToLower(intent.Prepared.ExtrinsicHash)); err != nil {
		return err
	}
	return nil
}

// MeasurementArtifact loads and independently verifies the exact artifact
// referenced by an intent. Release startup uses it to finish an idempotent EMA
// commit if the process stopped after the intent write but before submission.
func (s *IntentStore) MeasurementArtifact(intent *SteeringIntent) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readMeasurementArtifactLocked(intent)
}

func rationalJSON(scores []*big.Rat) ([]RationalJSON, error) {
	out := make([]RationalJSON, len(scores))
	for i, score := range scores {
		encoded, err := encodeRationalJSON(score)
		if err != nil {
			return nil, fmt.Errorf("score %d: %w", i, err)
		}
		out[i] = encoded
	}
	return out, nil
}

func (i *SteeringIntent) computeVectorHash() (string, error) {
	clone := *i
	clone.VectorHash = ""
	clone.Status = ""
	clone.CreatedAt = ""
	clone.UpdatedAt = ""
	clone.ExtrinsicHash = ""
	clone.Values = nil
	clone.FinalizedBlock = 0
	clone.FinalizedBlockHash = ""
	clone.RevealBlock = 0
	clone.ApplicationBlock = 0
	clone.ApplicationBlockHash = ""
	clone.Error = ""
	b, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "0x" + hex.EncodeToString(h[:]), nil
}

// ReconstructedVectorHash returns the canonical immutable intent hash without
// trusting the stored VectorHash field. It is useful to independent evidence
// tooling and deterministic cross-package fixtures.
func (i *SteeringIntent) ReconstructedVectorHash() (string, error) {
	if i == nil {
		return "", errors.New("steering intent is nil")
	}
	return i.computeVectorHash()
}

// VerifyVectorHash proves that the immutable decision evidence still matches
// the hash submitted with this intent. Release evidence readers use it so a
// changed audit, mask, score, snapshot, or prepared transaction cannot remain
// hidden behind an old vector hash.
func (i *SteeringIntent) VerifyVectorHash() error {
	if i == nil || i.VectorHash == "" {
		return errors.New("steering intent has no vector hash")
	}
	want, err := i.ReconstructedVectorHash()
	if err != nil {
		return err
	}
	if !strings.EqualFold(i.VectorHash, want) {
		return fmt.Errorf("steering intent vector hash %s, reconstructed %s", i.VectorHash, want)
	}
	return nil
}

func (s *IntentStore) readLocked() (*steeringIntentFile, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return &steeringIntentFile{Schema: steeringIntentSchema}, nil
	}
	if err != nil {
		return nil, err
	}
	var f steeringIntentFile
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&f); err != nil {
		return nil, fmt.Errorf("decode %s: %w", s.path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode %s: trailing JSON", s.path)
		}
		return nil, fmt.Errorf("decode %s: %w", s.path, err)
	}
	if f.Schema != steeringIntentSchema {
		return nil, fmt.Errorf("unsupported intent store schema %q", f.Schema)
	}
	canonical, err := json.MarshalIndent(&f, "", "  ")
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(b, append(canonical, '\n')) {
		return nil, errors.New("steering intent store is not canonically encoded")
	}
	all := make([]*SteeringIntent, 0, len(f.History)+1)
	for index := range f.History {
		all = append(all, &f.History[index])
	}
	if f.Current != nil {
		all = append(all, f.Current)
	}
	seenVectors := map[string]bool{}
	var previousIntent *SteeringIntent
	var previousMeasurement []byte
	for index, intent := range all {
		inHistory := index < len(f.History)
		if err := validateSteeringIntentLifecycle(intent, inHistory); err != nil {
			return nil, fmt.Errorf("steering intent %d: %w", index, err)
		}
		if err := intent.VerifyVectorHash(); err != nil {
			return nil, fmt.Errorf("steering intent %d: %w", index, err)
		}
		if seenVectors[intent.VectorHash] {
			return nil, fmt.Errorf("steering intent %d repeats vector hash %s", index, intent.VectorHash)
		}
		seenVectors[intent.VectorHash] = true
		artifact, verified, err := s.readMeasurementArtifactLocked(intent)
		if err != nil {
			return nil, fmt.Errorf("steering intent %d measurement: %w", index, err)
		}
		if err := VerifyReleaseMeasurementIntent(intent, artifact, verified); err != nil {
			return nil, fmt.Errorf("steering intent %d measurement: %w", index, err)
		}
		measurement, err := os.ReadFile(filepath.Join(s.stateDir, filepath.FromSlash(intent.MeasurementArtifactPath)))
		if err != nil {
			return nil, fmt.Errorf("steering intent %d measurement: %w", index, err)
		}
		if previousIntent == nil {
			if artifact.PreviousArtifactHash != "" {
				return nil, errors.New("first steering intent measurement has a predecessor")
			}
		} else {
			if err := validateSteeringIntentSuccessor(previousIntent, intent); err != nil {
				return nil, fmt.Errorf("steering intent %d: %w", index, err)
			}
			if err := VerifyReleaseMeasurementLineage(previousMeasurement, artifact); err != nil {
				return nil, fmt.Errorf("steering intent %d measurement lineage: %w", index, err)
			}
		}
		previousIntent = intent
		previousMeasurement = measurement
	}
	return &f, nil
}

func validateSteeringIntentLifecycle(intent *SteeringIntent, inHistory bool) error {
	if intent == nil || intent.Schema != steeringIntentSchema || intent.Prepared == nil || intent.CreatedAt == "" || intent.UpdatedAt == "" {
		return errors.New("lifecycle metadata is incomplete")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, intent.CreatedAt)
	if err != nil || intent.CreatedAt != createdAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("created time is not canonical UTC")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, intent.UpdatedAt)
	if err != nil || intent.UpdatedAt != updatedAt.UTC().Format(time.RFC3339Nano) || updatedAt.Before(createdAt) {
		return errors.New("updated time is not a canonical monotonic UTC time")
	}
	if _, err := intent.Prepared.Validate(); err != nil {
		return fmt.Errorf("prepared submission: %w", err)
	}
	finalized := intent.ExtrinsicHash != "" || intent.FinalizedBlock != 0 || intent.FinalizedBlockHash != "" || intent.RevealBlock != 0 || len(intent.Values) != 0
	applied := intent.ApplicationBlock != 0 || intent.ApplicationBlockHash != ""
	if finalized {
		if !strings.EqualFold(intent.ExtrinsicHash, intent.Prepared.ExtrinsicHash) || intent.FinalizedBlock == 0 || intent.FinalizedBlockHash == "" || intent.RevealBlock != intent.Prepared.RevealBlock || !slices.Equal(intent.Values, intent.Prepared.Values) {
			return errors.New("finalized lifecycle receipt differs from prepared submission")
		}
	}
	if applied && (intent.ApplicationBlock < intent.RevealBlock || intent.ApplicationBlockHash == "") {
		return errors.New("application lifecycle receipt is incomplete")
	}
	switch intent.Status {
	case "pending":
		if inHistory || finalized || applied || intent.Error != "" {
			return errors.New("pending lifecycle has terminal fields or was archived")
		}
	case "finalized":
		if inHistory || !finalized || applied || intent.Error != "" {
			return errors.New("finalized lifecycle fields are inconsistent")
		}
	case "applied":
		if !finalized || !applied || intent.Error != "" {
			return errors.New("applied lifecycle fields are inconsistent")
		}
	case "failed":
		if applied || strings.TrimSpace(intent.Error) == "" {
			return errors.New("failed lifecycle has no failure or has an application receipt")
		}
	default:
		return fmt.Errorf("lifecycle status %q is invalid", intent.Status)
	}
	return nil
}

func validateSteeringIntentSuccessor(previous, current *SteeringIntent) error {
	if previous == nil || current == nil {
		return errors.New("intent successor is nil")
	}
	if current.SubnetEpoch == previous.SubnetEpoch {
		if previous.Status != "failed" {
			return errors.New("same-epoch successor does not follow a failed intent")
		}
		return nil
	}
	if previous.SubnetEpoch == ^uint64(0) || current.SubnetEpoch != previous.SubnetEpoch+1 {
		return errors.New("intent successor is not consecutive by native epoch")
	}
	if previous.Status != "applied" && previous.Status != "failed" {
		return errors.New("next-epoch successor follows an unfinished intent")
	}
	return nil
}

func (s *IntentStore) writeLocked(f *steeringIntentFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicStateWrite(s.path, append(b, '\n'), 0o600)
}

func (s *IntentStore) Begin(intent SteeringIntent) (*SteeringIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	if intent.Prepared == nil {
		return nil, errors.New("steering intent has no exact prepared submission")
	}
	artifact, verifiedMeasurement, err := s.readMeasurementArtifactLocked(&intent)
	if err != nil {
		return nil, fmt.Errorf("steering intent measurement artifact: %w", err)
	}
	if err := VerifyReleaseMeasurementIntent(&intent, artifact, verifiedMeasurement); err != nil {
		return nil, err
	}
	if _, err := intent.Prepared.Validate(); err != nil {
		return nil, fmt.Errorf("steering intent prepared submission: %w", err)
	}
	if intent.Prepared.Netuid != intent.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || !slices.Equal(intent.Prepared.UIDs, intent.UIDs) {
		return nil, errors.New("steering intent does not match prepared submission")
	}
	if len(intent.DepositAudits) == 0 {
		return nil, errors.New("steering intent has no operator deposit audits")
	}
	if len(intent.EligibleHeadUIDs) != 0 {
		if len(intent.SelectedHeadUIDs) > int(^uint16(0)) {
			return nil, errors.New("steering intent selected head count exceeds uint16")
		}
		if err := ValidateHeadSelectionEvidence(intent.EligibleHeadUIDs, intent.EligibleHeadScores, intent.SelectedHeadUIDs, intent.RejectedHeadUIDs, uint16(len(intent.SelectedHeadUIDs))); err != nil {
			return nil, fmt.Errorf("steering intent head selection: %w", err)
		}
	} else if len(intent.EligibleHeadScores) != 0 || len(intent.SelectedHeadUIDs) != 0 || len(intent.RejectedHeadUIDs) != 0 {
		return nil, errors.New("steering intent head selection is partial")
	}
	for index, audit := range intent.DepositAudits {
		if audit.NoID == 0 || audit.Epoch != intent.SettlementEpoch || audit.Status == "" || audit.Disposition == "" || (index > 0 && audit.NoID <= intent.DepositAudits[index-1].NoID) {
			return nil, errors.New("steering intent deposit audits are incomplete or non-canonical")
		}
	}
	if f.Current != nil && f.Current.SubnetEpoch == intent.SubnetEpoch {
		switch f.Current.Status {
		case "pending":
			return nil, ErrSteeringIntentPending
		case "finalized", "applied":
			return nil, ErrSteeringAlreadyFinal
		}
	}
	if f.Current != nil {
		priorArtifact, priorVerified, priorErr := s.readMeasurementArtifactLocked(f.Current)
		if priorErr != nil {
			return nil, fmt.Errorf("prior steering intent measurement: %w", priorErr)
		}
		if err := VerifyReleaseMeasurementIntent(f.Current, priorArtifact, priorVerified); err != nil {
			return nil, fmt.Errorf("prior steering intent: %w", err)
		}
	}
	previousArtifactHash := ""
	if f.Current != nil {
		previousArtifactHash = f.Current.MeasurementArtifactHash
	}
	if artifact.PreviousArtifactHash != previousArtifactHash {
		return nil, errors.New("steering measurement artifact does not extend the intent lineage")
	}
	if f.Current != nil {
		if err := validateSteeringIntentSuccessor(f.Current, &intent); err != nil {
			return nil, err
		}
		previousEncoded, err := os.ReadFile(filepath.Join(s.stateDir, filepath.FromSlash(f.Current.MeasurementArtifactPath)))
		if err != nil {
			return nil, fmt.Errorf("read prior steering measurement: %w", err)
		}
		if err := VerifyReleaseMeasurementLineage(previousEncoded, artifact); err != nil {
			return nil, fmt.Errorf("steering measurement lineage: %w", err)
		}
	}
	if f.Current != nil {
		f.History = append(f.History, *f.Current)
	}
	intent.Schema = steeringIntentSchema
	intent.Status = "pending"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	intent.CreatedAt, intent.UpdatedAt = now, now
	intent.VectorHash, err = intent.computeVectorHash()
	if err != nil {
		return nil, err
	}
	f.Current = &intent
	if err := s.writeLocked(f); err != nil {
		return nil, err
	}
	copy := intent
	return &copy, nil
}

func (s *IntentStore) update(vectorHash, status string, mutate func(*SteeringIntent) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readLocked()
	if err != nil {
		return err
	}
	if f.Current == nil || f.Current.VectorHash != vectorHash {
		return errors.New("steering intent does not match current vector")
	}
	artifact, verified, err := s.readMeasurementArtifactLocked(f.Current)
	if err != nil {
		return fmt.Errorf("current steering intent measurement: %w", err)
	}
	if err := VerifyReleaseMeasurementIntent(f.Current, artifact, verified); err != nil {
		return fmt.Errorf("current steering intent: %w", err)
	}
	if mutate != nil {
		if err := mutate(f.Current); err != nil {
			return err
		}
	}
	f.Current.Status = status
	f.Current.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return s.writeLocked(f)
}

func (s *IntentStore) MarkFinalized(vectorHash, extrinsicHash string, finalizedBlock uint64, finalizedHash string, revealBlock uint64, values []uint16) error {
	return s.update(vectorHash, "finalized", func(i *SteeringIntent) error {
		if i.Status != "pending" || i.Prepared == nil || !strings.EqualFold(extrinsicHash, i.Prepared.ExtrinsicHash) || finalizedBlock == 0 || finalizedHash == "" || revealBlock != i.Prepared.RevealBlock || !slices.Equal(values, i.Prepared.Values) {
			return errors.New("finalized receipt does not match exact prepared submission")
		}
		i.ExtrinsicHash = extrinsicHash
		i.FinalizedBlock = finalizedBlock
		i.FinalizedBlockHash = finalizedHash
		i.RevealBlock = revealBlock
		i.Values = append([]uint16(nil), values...)
		i.Error = ""
		return nil
	})
}

func (s *IntentStore) MarkApplied(vectorHash string, block uint64, blockHash string) error {
	return s.update(vectorHash, "applied", func(i *SteeringIntent) error {
		if i.Status != "finalized" || block < i.RevealBlock || blockHash == "" {
			return errors.New("application receipt precedes finalized reveal")
		}
		i.ApplicationBlock = block
		i.ApplicationBlockHash = blockHash
		return nil
	})
}

func (s *IntentStore) MarkFailed(vectorHash string, failure error) error {
	return s.update(vectorHash, "failed", func(i *SteeringIntent) error {
		if (i.Status != "pending" && i.Status != "finalized") || failure == nil || strings.TrimSpace(failure.Error()) == "" {
			return errors.New("steering failure requires an unfinished intent and a nonempty cause")
		}
		i.Error = failure.Error()
		return nil
	})
}

func (s *IntentStore) Current() (*SteeringIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readLocked()
	if err != nil || f.Current == nil {
		return nil, err
	}
	copy := *f.Current
	return &copy, nil
}

// AuthenticatedIntents returns the complete canonical intent lifecycle only
// after readLocked has replayed every measurement envelope, predecessor link,
// status transition, and vector hash. Evidence readers must use this instead
// of decoding steering-intents.json independently.
func (s *IntentStore) AuthenticatedIntents() ([]SteeringIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	result := append([]SteeringIntent(nil), f.History...)
	if f.Current != nil {
		result = append(result, *f.Current)
	}
	return result, nil
}
