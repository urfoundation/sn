//go:build linux || darwin

// The running validator publishes original request windows independently of
// settlement refresh. Exact local bytes precede dual public replication.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
)

// Fixed paths form a finite, explicit component of the coordinator owner.
func ProviderAttemptPublicationPath(stateDir string, epoch, noId uint64) (string, error) {
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || noId == 0 {
		return "", protocol.ErrProviderAttemptsIntegrity
	}
	return filepath.Join(stateDir, "provider-attempt-publications", fmt.Sprintf("epoch-%020d-noid-%020d.json", epoch, noId)), nil
}

// One no-overwrite owner serializes its complete namespace census with write.
// Existing configured history/file bounds remain finite; no free-space guess
// or new runtime allowance is introduced by the optional publisher.
func retainProviderAttemptPublication(ctx context.Context, path string, raw []byte, maximum, historyMaximum, maximumFiles uint64, namespace *ProviderAttemptPublicationNamespace) (resultErr error) {
	if uint64(len(raw)) > maximum || maximum == 0 || historyMaximum == 0 || maximumFiles == 0 {
		return protocol.ErrProviderAttemptsCapacity
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, maximum, releaseMeasurementInputV2ReadHooks{}, false)
	defer func() { resultErr = errors.Join(resultErr, owner.finish(), ctx.Err()) }()
	if err != nil {
		return err
	}
	if namespace == nil {
		return protocol.ErrProviderAttemptsUnavailable
	}
	if err := namespace.Check(ctx, owner.directory.file); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, namespace.Check(ctx, owner.directory.file)) }()
	if err := unix.Flock(int(owner.directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	used, count, err := censusReleaseEvidenceCapturesV2(ctx, owner.directory, historyMaximum, maximumFiles)
	if err != nil {
		return err
	}
	old, err := owner.read()
	if err == nil {
		if !bytes.Equal(old, raw) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request publication changed original bytes"))
		}
		if err := reconcileProviderPublicationLinks(ctx, owner, raw, maximumFiles); err != nil {
			return err
		}
	} else if !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(err) {
		return err
	} else if count >= maximumFiles || uint64(len(raw)) > historyMaximum-used {
		return protocol.ErrProviderAttemptsCapacity
	}
	return owner.write(raw)
}

// One exact payload is uploaded with the original source reservation to both
// configured origins, then read back without credentials from each origin.
func replicateProviderAttemptPublication(ctx context.Context, raw []byte, replicas [2]AttemptCutV2Replica, bounds AttemptCutV2Bounds, maximum uint64) error {
	if len(raw) == 0 || uint64(len(raw)) > maximum || replicas[0].Origin == replicas[1].Origin {
		return protocol.ErrProviderAttemptsCapacity
	}
	hash := attemptHex32(sha256.Sum256(raw))
	for _, replica := range replicas {
		if replica.WriteMetadata == nil {
			return protocol.ErrProviderAttemptsUnavailable
		}
		if err := replica.WriteMetadata(ctx, hash, raw); err != nil {
			return err
		}
		reader, err := newHttpAttemptStreamV2Reader(replica.Origin, bounds, maximum)
		if err != nil {
			return err
		}
		observed, err := reader.ReadMetadata(ctx, hash, uint64(len(raw)))
		if err != nil {
			return err
		}
		if !bytes.Equal(observed, raw) {
			return protocol.ErrProviderAttemptsIntegrity
		}
	}
	return ctx.Err()
}

// A cursor moves only after exact local persistence and both public readbacks.
// Restart begins at original birth and reuses each retained file byte-for-byte.
type providerAttemptPublicationCursor struct {
	epoch     uint64
	prior     ProviderAttemptRequestHead
	priorHash [32]byte
}

// No source session is borrowed before the authentication owner publishes it.
func providerAttemptRuntimeJournal(runtime *releaseOperatorRuntime) (*ProviderAttemptRequestJournal, error) {
	if runtime == nil {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	engine := runtime.engine
	if runtime.authentication != nil {
		if !runtime.authentication.ready.Load() {
			return nil, protocol.ErrProviderAttemptsUnavailable
		}
		engine = runtime.authentication.engine
	}
	trail, ok := engine.(*TrailEngine)
	if !ok || trail.cfg.RequestJournal == nil {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	return trail.cfg.RequestJournal, nil
}

// The metadata lane observes only an already completed actual settlement.
// RPC/public I/O occurs after the short shared runtime cursor borrow ends.
func (self *releaseRuntimeV2) publishProviderRequestLane(ctx context.Context, op OperatorConfig, runtime *releaseOperatorRuntime, cursor *providerAttemptPublicationCursor) (resultErr error) {
	if cursor == nil {
		return protocol.ErrProviderAttemptsUnavailable
	}
	originalCursor := *cursor
	defer func() {
		if resultErr != nil {
			*cursor = originalCursor
		}
	}()
	journal, err := providerAttemptRuntimeJournal(runtime)
	if err != nil {
		return err
	}
	if op.RequestReceiptScope == nil {
		return protocol.ErrProviderAttemptsUnavailable
	}
	if cursor.epoch == 0 {
		cursor.epoch = journal.preparation.Birth.SettlementEpoch
	}
	release, err := self.acquire(ctx)
	if err != nil {
		return err
	}
	_, complete := self.history.terminals[cursor.epoch]
	view := *self
	view.cfg = self.cfg
	release()
	if !complete {
		return protocol.ErrProviderAttemptsUnavailable
	}
	snapshot, err := view.chain.ReleaseSnapshotContext(ctx)
	if err != nil {
		return err
	}
	window, _, err := view.window(ctx, snapshot, cursor.epoch)
	if err != nil {
		return err
	}
	window.FinalizedBlock = window.EndBlock
	preparation, err := providerAttemptPublicationPreparation(ctx, &view.cfg)
	if err != nil {
		return err
	}
	matched := false
	for _, operator := range preparation.Operators {
		if operator.Preparation.Identity.Ledger.NoID == op.NoID {
			matched = operator.Preparation == journal.preparation && operator.ReceiptScope == *op.RequestReceiptScope
		}
	}
	if !matched {
		return protocol.ErrProviderAttemptsIntegrity
	}
	namespace, err := OpenProviderAttemptPublicationNamespace(ctx, preparation)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, namespace.Close(), ctx.Err()) }()
	maximum := preparation.MaxWindowBytes
	path, err := ProviderAttemptPublicationPath(view.cfg.StateDir, cursor.epoch, op.NoID)
	if err != nil {
		return err
	}
	raw, err := readProviderAttemptPublication(ctx, path, maximum, namespace)
	var cut ProviderAttemptRequestWindow
	if err == nil {
		if err := attemptStoreDecode(raw, &cut); err != nil {
			return err
		}
		canonical, err := json.Marshal(cut)
		if err != nil || !bytes.Equal(raw, canonical) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
		}
	} else if releaseMeasurementInputV2InitialAbsence(err) {
		sealed, err := journal.SealWindow(ctx, window, maximum)
		if err != nil {
			return err
		}
		if err := journal.CloseRequests(ctx, sealed, *op.RequestReceiptScope); err != nil {
			return err
		}
		cut = *sealed
		raw, err = json.Marshal(cut)
		if err != nil {
			return err
		}
	} else {
		return err
	}
	if err := VerifyProviderAttemptPublishedWindow(ctx, cut, journal.preparation, *op.RequestReceiptScope, cursor.prior, cursor.priorHash, window, maximum); err != nil {
		return err
	}
	cutHash, err := cut.Header.Hash()
	if err != nil {
		return err
	}
	if err := retainProviderAttemptPublication(ctx, path, raw, maximum, view.cfg.EvidenceV2.Bounds.MaxHistoryBytes, view.cfg.EvidenceV2.Bounds.CaptureFileLimit(), namespace); err != nil {
		return err
	}
	replicas, err := releaseReservedAttemptCensusReplicasV2(&view.cfg, view.origins, view.runtimes)
	if err != nil {
		return err
	}
	if err := replicateProviderAttemptPublication(ctx, raw, replicas[op.NoID], view.cfg.EvidenceV2.Bounds.Cut, maximum); err != nil {
		return err
	}
	if cursor.epoch == ^uint64(0) {
		return protocol.ErrProviderAttemptsCapacity
	}
	cursor.epoch++
	cursor.prior = cut.Header.End
	cursor.priorHash = cutHash
	return nil
}

// Separate per-operator owners keep optional source outages from delaying
// settlement, native liabilities, healthy trials, or another publication lane.
func (self *releaseRuntimeV2) runProviderRequestPublications(ctx context.Context) error {
	var workers sync.WaitGroup
	for index, op := range self.cfg.Operators {
		if op.RequestPreparation == nil {
			continue
		}
		runtime := self.runtimes[index]
		workers.Go(func() {
			cursor := &providerAttemptPublicationCursor{}
			for ctx.Err() == nil {
				owner, cancel := context.WithTimeout(ctx, 300*time.Second)
				err := self.publishProviderRequestLane(owner, op, runtime, cursor)
				cancel()
				if ctx.Err() != nil {
					return
				}
				if err == nil {
					continue
				}
				releaseDiagnostic(ctx, "operator", "publication_unavailable_retrying", cursor.epoch, true, 0, releaseDiagnosticFacts{cause: releaseDiagnosticReadCause(err), operatorId: op.NoID, operatorKnown: true})
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Minute):
				}
			}
		})
	}
	workers.Wait()
	return ctx.Err()
}

// The same original configured operator roster constructs the offline and
// live publication profile. No authenticated session or current SQL row picks it.
func providerAttemptPublicationPreparation(ctx context.Context, cfg *ReleaseConfig) (ProviderAttemptPublicationPreparation, error) {
	if cfg == nil || cfg.EvidenceV2.Schema == "" {
		return ProviderAttemptPublicationPreparation{}, protocol.ErrProviderAttemptsUnavailable
	}
	result := ProviderAttemptPublicationPreparation{StateDir: cfg.StateDir, MaxWindowBytes: min(cfg.EvidenceV2.Bounds.MaxControlBytes, cfg.EvidenceV2.Bounds.MaxHistoryBytes), MaxHistoryBytes: cfg.EvidenceV2.Bounds.MaxHistoryBytes, MaxFiles: cfg.EvidenceV2.Bounds.CaptureFileLimit()}
	genesis, err := parseHash32("provider publication genesis", cfg.GenesisHash)
	if err != nil {
		return result, err
	}
	policy, err := parseHash32("provider publication policy", cfg.PolicyHash)
	if err != nil {
		return result, err
	}
	for _, op := range cfg.Operators {
		if op.RequestPreparation == nil {
			continue
		}
		if op.RequestReceiptScope == nil {
			return result, protocol.ErrProviderAttemptsUnavailable
		}
		raw, err := ReadReleaseEvidenceV2File(ctx, *op.RequestPreparation, 4096)
		if err != nil {
			return result, err
		}
		var original ProviderAttemptRequestPreparation
		if err := attemptStoreDecode(raw, &original); err != nil {
			return result, err
		}
		if err := original.Validate(); err != nil {
			return result, err
		}
		identity := original.Identity
		originalGenesis, err := parseHash32("provider original genesis", identity.Ledger.GenesisHash)
		if err != nil {
			return result, err
		}
		if identity.Ledger.NoID != op.NoID || identity.Ledger.ChainID != cfg.ChainID || originalGenesis != genesis || identity.PolicyHash != policy || identity.Ledger.Netuid != cfg.Netuid || identity.Coordinator != strings.ToLower(cfg.Coordinator) || identity.Ledger.DeploymentID != cfg.DeploymentID {
			return result, protocol.ErrProviderAttemptsIntegrity
		}
		deploymentKey := fmt.Sprintf("%d:%s", original.Identity.Ledger.ChainID, original.Identity.Coordinator)
		if op.RequestReceiptScope.DeploymentKey != deploymentKey {
			return result, protocol.ErrProviderAttemptsIntegrity
		}
		result.Operators = append(result.Operators, ProviderAttemptPublicationOperator{Preparation: original, ReceiptScope: *op.RequestReceiptScope})
	}
	return result, result.Validate()
}

// Verify the complete signed payload and every request-local close consent.
// The independently supplied profile/canonical window remains separate.
func VerifyProviderAttemptPublishedWindow(ctx context.Context, cut ProviderAttemptRequestWindow, preparation ProviderAttemptRequestPreparation, scope protocol.ProviderAttemptReceiptScope, begin ProviderAttemptRequestHead, previousHash [32]byte, window protocol.ValidatorEvidenceWindow, maximum uint64) error {
	if err := VerifyProviderAttemptRequestWindow(ctx, cut, preparation, begin, previousHash, window, maximum); err != nil {
		return err
	}
	if len(cut.Closures) != len(cut.Records) {
		return protocol.ErrProviderAttemptsIntegrity
	}
	cutHash, err := cut.Header.Hash()
	if err != nil {
		return err
	}
	for index, closure := range cut.Closures {
		record := cut.Records[index]
		if closure.CutHash != cutHash || closure.ClientId != record.Identity.ClientId || closure.Epoch != window.Epoch || closure.EndBlock != window.EndBlock || !bytes.Equal(closure.Message, record.Message) || !bytes.Equal(closure.RequestSignature, record.RequestSignature) {
			return protocol.ErrProviderAttemptsIntegrity
		}
		if err := protocol.VerifyProviderAttemptRequestClosure(ctx, closure, scope); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Borrow the already prepared directory through each exact local read. An
// absent leaf is distinct from a lost, replaced or unprepared namespace.
func readProviderAttemptPublication(ctx context.Context, path string, maximum uint64, namespace *ProviderAttemptPublicationNamespace) (raw []byte, resultErr error) {
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, maximum, releaseMeasurementInputV2ReadHooks{}, false)
	defer func() {
		resultErr = errors.Join(resultErr, owner.finish(), ctx.Err())
		if resultErr != nil {
			raw = nil
		}
	}()
	if err != nil {
		return nil, err
	}
	if namespace == nil {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	if err := namespace.Check(ctx, owner.directory.file); err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, namespace.Check(ctx, owner.directory.file)) }()
	raw, err = owner.read()
	if err != nil && owner.initialMissing && releaseMeasurementInputV2OnlyMissing(err) {
		err = errors.Join(errReleaseMeasurementInputV2InitiallyMissing, err)
	}
	return raw, err
}
