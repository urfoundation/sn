//go:build linux || darwin

package validator

// The production V2 intent owner consumes the complete retained transcript
// through the real runtime replay owner. Legacy constructors remain legacy;
// no artifact-supplied bindings or caller-built verdict authorizes a write.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/sys/unix"
)

type releaseIntentV2Owner struct {
	ctx     context.Context
	runtime *releaseRuntimeV2
	active  atomic.Bool
	fault   error
}

type releaseIntentV2Read struct {
	file         *steeringIntentFile
	custody      *releaseEvidenceV2StartupReferences
	encoded      []byte
	lastArtifact *ReleaseMeasurementArtifact
	lastBytes    []byte
	lastOptions  ReleaseMeasurementV2Options
}

const releaseIntentV2Marker = ".steering-intents-v2-pending"
const releaseIntentV2Candidate = ".steering-intents-v2-next"

func (self *IntentStore) acquireV2(ctx context.Context) (func(), error) {
	if ctx == nil || self == nil || self.v2 == nil || self.v2.runtime == nil {
		return nil, errors.New("V2 intent operation owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !self.v2.active.CompareAndSwap(false, true) {
		return nil, errors.New("V2 intent operation is already owned")
	}
	release := func() { self.v2.active.Store(false) }
	if self.v2.fault != nil {
		release()
		return nil, self.v2.fault
	}
	return release, nil
}

func (self *IntentStore) readMeasurementV2(ctx context.Context, custody *releaseEvidenceV2StartupReferences, intent *SteeringIntent) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, []byte, ReleaseMeasurementV2Options, error) {
	runtime, bounds := self.v2.runtime, self.v2.runtime.cfg.EvidenceV2.Bounds
	nativeCopy := *runtime.native
	native := &nativeCopy
	var options ReleaseMeasurementV2Options
	measurement, err := runtime.history.readContentReference(ctx, custody, intent.MeasurementArtifactPath, intent.MeasurementArtifactHash, intent.MeasurementArtifactSize, bounds.MaxArtifactBytes, false)
	if err != nil {
		return nil, nil, nil, options, err
	}
	envelopeBytes, err := runtime.history.readContentReference(ctx, custody, intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize, bounds.MaxControlBytes, true)
	if err != nil {
		return nil, nil, nil, options, err
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
	if err != nil {
		return nil, nil, nil, options, err
	}
	options, err = runtime.measurementOptionsForIntent(ctx, intent, artifact)
	if err != nil {
		return nil, nil, nil, options, err
	}
	envelope, err := DecodeReleaseMeasurementEnvelopeV2(ctx, envelopeBytes, bounds.MaxControlBytes)
	if err != nil {
		return nil, nil, nil, options, err
	}
	if intent.Prepared == nil || intent.Prepared.SourceCommitment == nil || intent.Prepared.SourceCommitment.Hash != releaseHex32(releaseNativeSourceHashV2(measurement)) {
		return nil, nil, nil, options, errors.New("V2 intent lacks its exact pre-Prepared native source commitment")
	}
	hotkey := runtime.hotkey.PublicKey()
	if intent.Prepared.HotkeyHex != releaseHex32(hotkey) {
		return nil, nil, nil, options, errors.New("V2 prepared hotkey differs from independently owned validator identity")
	}
	_, verified, err := VerifyReleaseMeasurementEnvelopeV2(ctx, envelope, measurement, hotkey, intent.SelfUID, intent.Prepared.ExtrinsicHash, options)
	if err != nil {
		return nil, nil, nil, options, err
	}
	if err := VerifyReleaseMeasurementIntent(intent, artifact, verified.Decision); err != nil {
		return nil, nil, nil, options, err
	}
	if intent.Prepared.Netuid != intent.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || !slices.Equal(intent.Prepared.UIDs, intent.UIDs) {
		return nil, nil, nil, options, errors.New("V2 prepared vector differs from the independently replayed decision")
	}
	if err := authenticateReleaseNativeSourceReferenceV2(ctx, native, &runtime.cfg, intent, artifact); err != nil {
		return nil, nil, nil, options, err
	}
	preparedHash, err := types.NewHashFromHexString(intent.Prepared.PreparedAtBlockHash)
	if err != nil {
		return nil, nil, nil, options, err
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, native, &runtime.cfg, preparedHash); err != nil {
		return nil, nil, nil, options, err
	}
	if err := native.ValidatePreparedSourceWeightsContext(ctx, intent.Prepared, verified.Decision.UIDs, verified.Decision.Scores, releaseSubmitOptions(&runtime.cfg)); err != nil {
		return nil, nil, nil, options, err
	}
	return artifact, verified.Decision, measurement, options, custody.check()
}

func (self *IntentStore) readV2(ctx context.Context) (result *releaseIntentV2Read, resultErr error) {
	bounds := self.v2.runtime.cfg.EvidenceV2.Bounds
	result = &releaseIntentV2Read{file: &steeringIntentFile{Schema: steeringIntentSchema}, custody: &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, result.custody.close())
			result = nil
		}
	}()
	encoded, err := result.custody.read(ctx, self.path, bounds.IntentFileLimit(), true)
	if err != nil {
		return result, err
	}
	owner := result.custody.owners[0]
	if err := unix.Flock(int(owner.directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return result, errors.Join(errors.New("V2 intent namespace is already owned"), err)
	}
	for _, name := range []string{releaseIntentV2Marker, releaseIntentV2Candidate} {
		_, err := owner.directory.stat(name)
		if !releaseMeasurementInputV2OnlyMissing(err) {
			return result, errors.Join(errors.New("V2 intent publication is unresolved; retained marker/preimage requires explicit recovery"), err)
		}
	}
	result.encoded = encoded
	if encoded == nil {
		return result, result.custody.check()
	}
	if err := decodeAttemptStreamV2JSON(encoded, bounds.IntentFileLimit(), result.file); err != nil {
		return result, err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, result.file, bounds.IntentFileLimit(), true, true)
	if err != nil || !bytes.Equal(encoded, canonical) || result.file.Schema != steeringIntentSchema || len(result.file.History) > 16384 {
		return result, errors.Join(errors.New("V2 intent history bytes or finite census differ"), err)
	}
	all := make([]*SteeringIntent, 0, len(result.file.History)+1)
	for index := range result.file.History {
		all = append(all, &result.file.History[index])
	}
	if result.file.Current != nil {
		all = append(all, result.file.Current)
	}
	seen := make(map[string]bool, len(all))
	var previous *SteeringIntent
	for index, intent := range all {
		if err := validateSteeringIntentLifecycle(intent, index < len(result.file.History)); err != nil {
			return result, err
		}
		if err := intent.VerifyVectorHash(); err != nil {
			return result, err
		}
		if seen[intent.VectorHash] {
			return result, errors.New("V2 intent history repeats a vector")
		}
		seen[intent.VectorHash] = true
		artifact, _, measurement, options, err := self.readMeasurementV2(ctx, result.custody, intent)
		if err != nil {
			return result, fmt.Errorf("V2 intent %d: %w", index, err)
		}
		if previous == nil {
			if artifact.PreviousArtifactHash != "" {
				return result, errors.New("V2 intent history omits a predecessor")
			}
		} else {
			if err := validateSteeringIntentSuccessor(previous, intent); err != nil {
				return result, err
			}
			previousReplay, err := self.v2.runtime.measurementReplayOptionsV2(ctx, result.lastOptions, "history-lineage-previous")
			if err != nil {
				return result, err
			}
			currentReplay, err := self.v2.runtime.measurementReplayOptionsV2(ctx, options, "history-lineage-current")
			if err != nil {
				return result, err
			}
			if _, err := VerifyReleaseMeasurementLineageV2(ctx, result.lastBytes, previousReplay, artifact, currentReplay); err != nil {
				return result, err
			}
		}
		previous, result.lastArtifact, result.lastBytes, result.lastOptions = intent, artifact, measurement, options
	}
	return result, errors.Join(result.custody.check(), ctx.Err())
}

// The same reviewed native no-replace/exchange primitive used by bounded EMA
// retains the exact displaced predecessor. A durable marker remains on every
// uncertain phase; no rename can erase conflicting user state silently.
func (self *IntentStore) writeV2(ctx context.Context, read *releaseIntentV2Read) (resultErr error) {
	limit := self.v2.runtime.cfg.EvidenceV2.Bounds.IntentFileLimit()
	encoded, err := marshalAttemptSettlementV2JSON(ctx, read.file, limit, true, true)
	if err != nil {
		return err
	}
	if err := read.custody.check(); err != nil {
		return err
	}
	owner := read.custody.owners[0]
	directory := owner.directory
	markerFile, err := directory.openFile(releaseIntentV2Marker, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	started := true
	defer func() {
		if resultErr != nil && started {
			self.v2.fault = errors.Join(errors.New("V2 intent publication is uncertain; exact marker/preimage retained"), resultErr)
		}
	}()
	markerBytes := []byte(fmt.Sprintf("%x\n%x\n", sha256.Sum256(read.encoded), sha256.Sum256(encoded)))
	markerWritten, writeErr := markerFile.Write(markerBytes)
	if markerWritten != len(markerBytes) {
		writeErr = errors.Join(writeErr, errors.New("V2 intent marker write was short"))
	}
	if err := errors.Join(writeErr, markerFile.Sync(), markerFile.Close(), directory.file.Sync()); err != nil {
		return err
	}
	marker, err := directory.stat(releaseIntentV2Marker)
	if err != nil {
		return err
	}
	candidateFile, err := directory.openFile(releaseIntentV2Candidate, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, writeErr := candidateFile.Write(encoded)
	if written != len(encoded) {
		writeErr = errors.Join(writeErr, errors.New("V2 intent write was short"))
	}
	syncErr := candidateFile.Sync()
	candidate, candidateErr := statAttemptPrivateFile(candidateFile)
	if err := errors.Join(writeErr, syncErr, candidateErr, candidateFile.Close()); err != nil {
		return err
	}
	currentCandidate, err := directory.stat(releaseIntentV2Candidate)
	if err != nil || currentCandidate != candidate || !candidate.regular() || candidate.mode&0o777 != 0o600 || candidate.links != 1 || candidate.size != int64(len(encoded)) {
		return errors.Join(errors.New("V2 intent candidate descriptor/name or private shape differs"), err)
	}
	if err := errors.Join(read.custody.check(), ctx.Err()); err != nil {
		return err
	}
	if err := publishHeadEMAStoreV2File(directory.file, releaseIntentV2Candidate, owner.name, !owner.initialMissing); err != nil {
		return err
	}
	published, err := directory.stat(owner.name)
	if err != nil || !sameHeadEMAStoreV2RenamedFile(published, candidate) {
		return errors.Join(errors.New("V2 intent candidate changed at publication"), err)
	}
	publishedFile, err := directory.openFile(owner.name, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	publishedDigest, publishedErr := hashHeadEMAStoreV2File(ctx, publishedFile, published, limit)
	if err := errors.Join(publishedErr, publishedFile.Close()); err != nil {
		return err
	}
	if publishedDigest != sha256.Sum256(encoded) {
		return errors.New("V2 intent published bytes differ from the owned candidate")
	}
	var displaced *attemptPrivateFileState
	if !owner.initialMissing {
		state, err := directory.stat(releaseIntentV2Candidate)
		if err != nil || owner.witness == nil || !sameHeadEMAStoreV2RenamedFile(state, *owner.witness) {
			return errors.Join(errors.New("V2 intent displaced predecessor differs; retained for recovery"), err)
		}
		file, err := directory.openFile(releaseIntentV2Candidate, unix.O_RDONLY, 0)
		if err != nil {
			return err
		}
		digest, hashErr := hashHeadEMAStoreV2File(ctx, file, state, limit)
		if err := errors.Join(hashErr, file.Close()); err != nil {
			return err
		}
		if digest != sha256.Sum256(read.encoded) {
			return errors.New("V2 intent displaced predecessor bytes differ")
		}
		displaced = &state
	}
	owner.witness, owner.initialMissing = &published, false
	if err := errors.Join(directory.file.Sync(), read.custody.check(), ctx.Err()); err != nil {
		return err
	}
	if displaced != nil {
		if err := unlinkHeadEMAStoreV2Owned(directory, releaseIntentV2Candidate, *displaced); err != nil {
			return err
		}
	}
	if err := unlinkHeadEMAStoreV2Owned(directory, releaseIntentV2Marker, marker); err != nil {
		return err
	}
	if err := errors.Join(directory.file.Sync(), read.custody.check(), ctx.Err()); err != nil {
		return err
	}
	started = false
	return nil
}

func (self *IntentStore) currentV2(ctx context.Context) (result *SteeringIntent, resultErr error) {
	release, err := self.acquireV2(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	read, err := self.readV2(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, read.custody.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	return read.file.Current, nil
}

func (self *IntentStore) authenticatedIntentsV2(ctx context.Context) (result []SteeringIntent, resultErr error) {
	release, err := self.acquireV2(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	read, err := self.readV2(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, read.custody.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	result = slices.Clone(read.file.History)
	if read.file.Current != nil {
		result = append(result, *read.file.Current)
	}
	return result, nil
}

func (self *IntentStore) measurementArtifactV2(ctx context.Context, intent *SteeringIntent) (artifact *ReleaseMeasurementArtifact, verified *VerifiedReleaseMeasurement, resultErr error) {
	release, err := self.acquireV2(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	custody := &releaseEvidenceV2StartupReferences{remaining: self.v2.runtime.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	defer func() {
		resultErr = errors.Join(resultErr, custody.close(), ctx.Err())
		if resultErr != nil {
			artifact, verified = nil, nil
		}
	}()
	artifact, verified, _, _, resultErr = self.readMeasurementV2(ctx, custody, intent)
	return
}

func (self *IntentStore) beginV2(ctx context.Context, intent SteeringIntent) (result *SteeringIntent, resultErr error) {
	release, err := self.acquireV2(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	read, err := self.readV2(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, read.custody.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	current := read.file.Current
	if current != nil && current.SubnetEpoch == intent.SubnetEpoch {
		if current.Status == "pending" {
			return nil, ErrSteeringIntentPending
		}
		if current.Status == "finalized" || current.Status == "applied" {
			return nil, ErrSteeringAlreadyFinal
		}
	}
	artifact, _, _, options, err := self.readMeasurementV2(ctx, read.custody, &intent)
	if err != nil {
		return nil, err
	}
	if current == nil {
		if artifact.PreviousArtifactHash != "" {
			return nil, errors.New("first V2 intent invents a predecessor")
		}
	} else {
		if err := validateSteeringIntentSuccessor(current, &intent); err != nil {
			return nil, err
		}
		previousReplay, err := self.v2.runtime.measurementReplayOptionsV2(ctx, read.lastOptions, "begin-lineage-previous")
		if err != nil {
			return nil, err
		}
		currentReplay, err := self.v2.runtime.measurementReplayOptionsV2(ctx, options, "begin-lineage-current")
		if err != nil {
			return nil, err
		}
		if _, err := VerifyReleaseMeasurementLineageV2(ctx, read.lastBytes, previousReplay, artifact, currentReplay); err != nil {
			return nil, err
		}
		read.file.History = append(read.file.History, *current)
	}
	intent.Schema, intent.Status = steeringIntentSchema, "pending"
	intent.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	intent.UpdatedAt = intent.CreatedAt
	intent.VectorHash, err = intent.computeVectorHash()
	if err != nil {
		return nil, err
	}
	if err := validateSteeringIntentLifecycle(&intent, false); err != nil {
		return nil, err
	}
	read.file.Current = &intent
	if err := self.writeV2(ctx, read); err != nil {
		return nil, err
	}
	return &intent, nil
}

func (self *IntentStore) updateV2(ctx context.Context, vectorHash, status string, mutate func(*SteeringIntent) error) (resultErr error) {
	release, err := self.acquireV2(ctx)
	if err != nil {
		return err
	}
	defer release()
	read, err := self.readV2(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, read.custody.close(), ctx.Err()) }()
	if read.file.Current == nil || read.file.Current.VectorHash != vectorHash {
		return errors.New("V2 intent differs from the current immutable vector")
	}
	if mutate != nil {
		if err := mutate(read.file.Current); err != nil {
			return err
		}
	}
	read.file.Current.Status, read.file.Current.UpdatedAt = status, time.Now().UTC().Format(time.RFC3339Nano)
	if err := validateSteeringIntentLifecycle(read.file.Current, false); err != nil {
		return err
	}
	if _, _, _, _, err := self.readMeasurementV2(ctx, read.custody, read.file.Current); err != nil {
		return err
	}
	return self.writeV2(ctx, read)
}

// Construction follows semantic startup; no opaque callback supplies replay
// approval, and no legacy store is upgraded by examining a candidate schema.
func newReleaseIntentStoreV2(runtime *releaseRuntimeV2) (*IntentStore, error) {
	if runtime == nil || runtime.ctx == nil || runtime.history == nil {
		return nil, errors.New("V2 intent startup owner is absent")
	}
	if !filepath.IsAbs(runtime.cfg.StateDir) {
		return nil, errors.New("V2 intent state path is not absolute")
	}
	return &IntentStore{path: filepath.Join(runtime.cfg.StateDir, "steering-intents.json"), stateDir: runtime.cfg.StateDir, v2: &releaseIntentV2Owner{ctx: runtime.ctx, runtime: runtime}}, nil
}

func (self *IntentStore) markFinalizedV2(ctx context.Context, vectorHash, extrinsicHash string, block uint64, blockHash string, revealBlock uint64, values []uint16) error {
	return self.updateV2(ctx, vectorHash, "finalized", func(intent *SteeringIntent) error {
		if intent.Status != "pending" || intent.Prepared == nil || extrinsicHash != intent.Prepared.ExtrinsicHash || block == 0 || blockHash == "" || revealBlock != intent.Prepared.RevealBlock || !slices.Equal(values, intent.Prepared.Values) {
			return errors.New("V2 finalized receipt differs from exact prepared bytes")
		}
		intent.ExtrinsicHash, intent.FinalizedBlock, intent.FinalizedBlockHash, intent.RevealBlock, intent.Values = extrinsicHash, block, blockHash, revealBlock, slices.Clone(values)
		intent.Error = ""
		return nil
	})
}

func (self *IntentStore) markFailedV2(ctx context.Context, vectorHash string, cause error) error {
	return self.updateV2(ctx, vectorHash, "failed", func(intent *SteeringIntent) error {
		if cause == nil || cause.Error() == "" || intent.Status != "pending" && intent.Status != "finalized" {
			return errors.New("V2 failure has no real unfinished intent/cause")
		}
		intent.Error = cause.Error()
		return nil
	})
}

func (self *IntentStore) markAppliedV2(ctx context.Context, vectorHash string, block uint64, blockHash string) error {
	return self.updateV2(ctx, vectorHash, "applied", func(intent *SteeringIntent) error {
		if intent.Status != "finalized" || block < intent.RevealBlock || blockHash == "" {
			return errors.New("V2 application receipt precedes finalized reveal")
		}
		intent.ApplicationBlock, intent.ApplicationBlockHash = block, blockHash
		return nil
	})
}
