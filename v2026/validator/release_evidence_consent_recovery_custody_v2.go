//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

// Observers can fail only after the corresponding real operation completes.
// They never replace replay, chain observations, writes, exchange or fsync.
type evidenceConsentRecoveryHooks struct {
	afterPrepared  func() error
	afterPublish   func() error
	beforeExchange func() error
	afterExchange  func() error
}

// Complete public consent bytes precede the first public object write. The
// immutable old preimage and exchange basename survive every crash boundary.
type evidenceConsentRecoveryPrepared struct {
	Schema         string   `json:"schema"`
	OldManifest    []byte   `json:"old_manifest"`
	NewManifest    []byte   `json:"new_manifest"`
	SignedArtifact []byte   `json:"signed_artifact"`
	ExchangeName   string   `json:"exchange_name"`
	FinalizedBlock uint64   `json:"finalized_block"`
	FinalizedHash  [32]byte `json:"finalized_hash"`
}

// One physical ledger fence blocks its ordinary writer; state-directory flock
// additionally serializes explicit repair invocations without inventing locks.
type evidenceConsentRecoveryOwner struct {
	ledger       *os.File
	operator     *attemptPrivateDirectory
	ledgerAnchor attemptPrivateFileState
	state        *attemptPrivateDirectory
	locators     *attemptPrivateDirectory
	audit        *attemptPrivateDirectory
}

func openEvidenceConsentRecoveryOwner(ctx context.Context, options ValidatorEvidenceConsentRecoveryV2Options, createAudit bool) (_ *evidenceConsentRecoveryOwner, resultErr error) {
	owner := &evidenceConsentRecoveryOwner{}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owner.close())
		}
	}()
	if !filepath.IsAbs(options.OperatorStateDir) || filepath.Clean(options.OperatorStateDir) != options.OperatorStateDir {
		return nil, errors.New("consent recovery operator path is not canonical")
	}
	var err error
	owner.operator, err = openAttemptPrivateDirectory(options.OperatorStateDir, ctx)
	if err != nil {
		return nil, err
	}
	ledger, err := owner.operator.openFile(attemptLedgerStoreName, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	owner.ledger = ledger
	owner.ledgerAnchor, err = statAttemptPrivateFile(ledger)
	if err != nil || !owner.ledgerAnchor.directory() {
		return nil, errors.Join(errors.New("consent recovery ledger fence is not the original private directory"), err)
	}
	if err := attemptStoreLockFile(ledger); err != nil {
		return nil, fmt.Errorf("consent recovery requires the stopped original writer: %w", err)
	}
	owner.state, err = openAttemptPrivateDirectory(options.StateDir, ctx)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(owner.state.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("consent recovery already has an owner: %w", err)
	}
	owner.locators, err = openAttemptPrivateDirectory(filepath.Join(options.StateDir, "evidence-publications"), ctx)
	if err != nil {
		return nil, err
	}
	if createAudit {
		if err := unix.Mkdirat(int(owner.state.file.Fd()), "evidence-consent-recoveries", 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, err
		}
		if err := owner.state.file.Sync(); err != nil {
			return nil, err
		}
	}
	owner.audit, err = openAttemptPrivateDirectory(filepath.Join(options.StateDir, "evidence-consent-recoveries"), ctx)
	if err != nil {
		return nil, err
	}
	return owner, nil
}

// Close failures retain an incomplete verdict even after a durable operation.
func (self *evidenceConsentRecoveryOwner) close() error {
	var failures []error
	if self.ledger != nil && self.operator != nil {
		failures = append(failures, self.check())
	}
	for _, directory := range []*attemptPrivateDirectory{self.audit, self.locators, self.state, self.operator} {
		if directory != nil {
			failures = append(failures, directory.check(), directory.close())
		}
	}
	if self.ledger != nil {
		failures = append(failures, self.ledger.Close())
	}
	return errors.Join(failures...)
}

// The existing bounded owner reader rejects aliases, changed inodes, short
// reads, permission changes and late close failures on each physical read.
func readEvidenceConsentRecoveryFile(ctx context.Context, directory *attemptPrivateDirectory, name string, maximum uint64) ([]byte, bool, error) {
	return readAttemptPrivateMetadata(ctx, directory, name, maximum, attemptPrivateMetadataReadIO{closeFile: func(file *os.File) error { return file.Close() }})
}

// Existing no-overwrite publication preserves exact prepared bytes on retry.
func writeEvidenceConsentRecoveryFile(ctx context.Context, directory *attemptPrivateDirectory, name string, raw []byte, maximum uint64) error {
	if !validAttemptPrivateLeaf(name) {
		return errors.New("consent recovery audit leaf is invalid")
	}
	if err := directory.check(); err != nil {
		return err
	}
	if err := writeReleaseMeasurementInputV2Context(ctx, filepath.Join(directory.path, name), raw, maximum, releaseMeasurementInputV2ReadHooks{}); err != nil {
		return err
	}
	return errors.Join(directory.file.Sync(), directory.check(), ctx.Err())
}

// Exchange atomically retains the displaced old inode in the audit directory.
// An adopted retry is valid only with both the exact new active bytes and old
// displaced preimage; a crash cannot bless a raced third locator.
func adoptEvidenceConsentRecoveryLocator(ctx context.Context, owner *evidenceConsentRecoveryOwner, leaf string, prepared evidenceConsentRecoveryPrepared, maximum uint64, hooks evidenceConsentRecoveryHooks) error {
	active, exists, err := readEvidenceConsentRecoveryFile(ctx, owner.locators, leaf, maximum)
	if err != nil || !exists {
		return errors.Join(errors.New("consent recovery active locator disappeared"), err)
	}
	if bytes.Equal(active, prepared.NewManifest) {
		displaced, exists, err := readEvidenceConsentRecoveryFile(ctx, owner.audit, prepared.ExchangeName, maximum)
		if err != nil || !exists || !bytes.Equal(displaced, prepared.OldManifest) {
			return errors.Join(errors.New("adopted consent recovery lacks its exact displaced old locator"), err)
		}
		return errors.Join(owner.locators.file.Sync(), owner.audit.file.Sync(), owner.locators.check(), owner.audit.check(), ctx.Err())
	}
	if !bytes.Equal(active, prepared.OldManifest) {
		return errors.New("consent recovery locator compare-and-swap lost its exact old bytes")
	}
	if err := writeEvidenceConsentRecoveryFile(ctx, owner.audit, prepared.ExchangeName, prepared.NewManifest, maximum); err != nil {
		return err
	}
	before, err := owner.locators.stat(leaf)
	if err != nil {
		return err
	}
	if hooks.beforeExchange != nil {
		if err := hooks.beforeExchange(); err != nil {
			return err
		}
	}
	again, exists, err := readEvidenceConsentRecoveryFile(ctx, owner.locators, leaf, maximum)
	after, statErr := owner.locators.stat(leaf)
	if err != nil || statErr != nil || !exists || before != after || !bytes.Equal(again, prepared.OldManifest) {
		return errors.Join(errors.New("consent recovery old locator changed before atomic exchange"), err, statErr)
	}
	if err := errors.Join(owner.locators.check(), owner.audit.check(), ctx.Err()); err != nil {
		return err
	}
	if err := exchangeEvidenceConsentRecoveryFiles(int(owner.audit.file.Fd()), prepared.ExchangeName, int(owner.locators.file.Fd()), leaf); err != nil {
		return err
	}
	if err := errors.Join(owner.locators.file.Sync(), owner.audit.file.Sync()); err != nil {
		return err
	}
	if hooks.afterExchange != nil {
		if err := hooks.afterExchange(); err != nil {
			return err
		}
	}
	return adoptEvidenceConsentRecoveryLocator(ctx, owner, leaf, prepared, maximum, evidenceConsentRecoveryHooks{})
}

// Every failure keeps the old locator or durable prepared/displaced evidence.
// The ordinary runtime cannot enter this path by retrying a failed HTTP read.
func repairValidatorEvidenceConsentV2(ctx context.Context, closure *AttemptSettlementClosureV2, options ValidatorEvidenceConsentRecoveryV2Options, chain evidenceConsentRecoveryChain, hooks evidenceConsentRecoveryHooks) (result *ValidatorEvidenceConsentRecoveryV2Result, resultErr error) {
	if ctx == nil || chain == nil || !options.OriginalConsentUnavailable || options.ExpectedManifestBytes == 0 {
		return nil, errors.New("consent recovery owner or chain is absent")
	}
	owner, err := openEvidenceConsentRecoveryOwner(ctx, options, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, owner.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	publication, publisher, err := prepareEvidenceConsentRecoveryV2(ctx, closure, options)
	if err != nil {
		return nil, err
	}
	header := options.ExpectedHeader
	limit := options.Census.Settlement.MaxClosureBytes
	if limit == 0 || limit > (^uint64(0))/4 {
		return nil, errors.New("consent recovery audit capacity is absent or overflowing")
	}
	auditLimit := 4 * limit
	leaf := fmt.Sprintf("epoch-%020d.json", header.Epoch)
	base := fmt.Sprintf("epoch-%020d-%x", header.Epoch, options.ExpectedManifestHash)
	preparedName, completeName := base+".prepared.json", base+".complete.json"
	raw, exists, err := readEvidenceConsentRecoveryFile(ctx, owner.audit, preparedName, auditLimit)
	if err != nil {
		return nil, err
	}
	var prepared evidenceConsentRecoveryPrepared
	if exists {
		if err := json.Unmarshal(raw, &prepared); err != nil {
			return nil, err
		}
		canonical, err := marshalAttemptSettlementV2JSON(ctx, prepared, auditLimit, true, true)
		if err != nil || !bytes.Equal(canonical, raw) {
			return nil, errors.Join(errors.New("consent recovery prepared record is noncanonical"), err)
		}
	} else {
		active, found, err := readEvidenceConsentRecoveryFile(ctx, owner.locators, leaf, limit)
		if err != nil || !found || sha256.Sum256(active) != options.ExpectedManifestHash || uint64(len(active)) != options.ExpectedManifestBytes {
			return nil, errors.Join(errors.New("consent recovery exact original locator pin differs"), err)
		}
		old, err := ReadValidatorEvidencePublicationV2Manifest(ctx, filepath.Join(owner.locators.path, leaf), limit, 1)
		if err != nil {
			return nil, err
		}
		if old.Epoch != header.Epoch || old.CensusHash != publication.CensusHash || old.CensusBytes != uint64(len(publication.Census)) || len(old.Members) != 1 || old.Members[0].NoId != header.NoID || !slices.Equal(old.Origins, publication.Origins) {
			return nil, errors.New("consent recovery old locator differs from unchanged public census")
		}
		if _, err := publisher.readers[0].ReadMetadata(ctx, attemptHex32(old.Members[0].SignedArtifactHash), old.Members[0].SignedArtifactBytes); err == nil {
			return nil, errors.New("original consent remains publicly readable; restore/reuse it instead")
		}
		if err := owner.check(); err != nil {
			return nil, err
		}
		block, hash, err := chain.authenticate(ctx, options, false)
		if err != nil {
			return nil, err
		}
		if err := signEvidenceConsentRecoveryV2(ctx, publication, options); err != nil {
			return nil, err
		}
		member := publication.Members[0]
		updated := *old
		updated.Members = []ValidatorEvidencePublicationV2MemberReference{{NoId: header.NoID, SignedArtifactHash: member.SignedArtifactHash, SignedArtifactBytes: uint64(len(member.SignedArtifact))}}
		newRaw, err := marshalAttemptSettlementV2JSON(ctx, updated, limit, true, true)
		if err != nil {
			return nil, err
		}
		prepared = evidenceConsentRecoveryPrepared{Schema: "urnetwork-validator-consent-recovery-prepared-v1", OldManifest: active, NewManifest: newRaw, SignedArtifact: member.SignedArtifact, ExchangeName: base + ".displaced.json", FinalizedBlock: block, FinalizedHash: hash}
		raw, err = marshalAttemptSettlementV2JSON(ctx, prepared, auditLimit, true, true)
		if err != nil {
			return nil, err
		}
		if err := writeEvidenceConsentRecoveryFile(ctx, owner.audit, preparedName, raw, auditLimit); err != nil {
			return nil, err
		}
	}
	if prepared.Schema != "urnetwork-validator-consent-recovery-prepared-v1" || sha256.Sum256(prepared.OldManifest) != options.ExpectedManifestHash || uint64(len(prepared.OldManifest)) != options.ExpectedManifestBytes || prepared.ExchangeName != base+".displaced.json" || prepared.FinalizedBlock == 0 || prepared.FinalizedHash == ([32]byte{}) {
		return nil, errors.New("consent recovery prepared ownership differs")
	}
	signed, err := validatePreparedEvidenceConsentRecoveryV2(prepared.SignedArtifact, header, options)
	if err != nil {
		return nil, err
	}
	var old, updated ValidatorEvidencePublicationV2Manifest
	if err := json.Unmarshal(prepared.OldManifest, &old); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(prepared.NewManifest, &updated); err != nil {
		return nil, err
	}
	if err := old.validate(limit, 1); err != nil {
		return nil, err
	}
	if err := updated.validate(limit, 1); err != nil {
		return nil, err
	}
	expected := old
	expected.Members = []ValidatorEvidencePublicationV2MemberReference{{NoId: header.NoID, SignedArtifactHash: sha256.Sum256(prepared.SignedArtifact), SignedArtifactBytes: uint64(len(prepared.SignedArtifact))}}
	expectedRaw, err := marshalAttemptSettlementV2JSON(ctx, expected, limit, true, true)
	if err != nil || !bytes.Equal(expectedRaw, prepared.NewManifest) || old.CensusHash != publication.CensusHash || old.CensusBytes != uint64(len(publication.Census)) || old.Epoch != header.Epoch || !slices.Equal(old.Origins, publication.Origins) {
		return nil, errors.Join(errors.New("prepared recovery changed another locator field"), err)
	}
	active, found, err := readEvidenceConsentRecoveryFile(ctx, owner.locators, leaf, limit)
	if err != nil || !found {
		return nil, errors.Join(errors.New("prepared recovery active locator unavailable"), err)
	}
	alreadyAdopted := bytes.Equal(active, prepared.NewManifest)
	if alreadyAdopted {
		if err := adoptEvidenceConsentRecoveryLocator(ctx, owner, leaf, prepared, limit, evidenceConsentRecoveryHooks{}); err != nil {
			return nil, err
		}
	}
	if hooks.afterPrepared != nil {
		if err := hooks.afterPrepared(); err != nil {
			return nil, err
		}
	}
	if _, _, err := chain.authenticate(ctx, options, alreadyAdopted); err != nil {
		return nil, err
	}
	if err := owner.check(); err != nil {
		return nil, err
	}
	if err := publisher.writer("metadata")(ctx, attemptHex32(sha256.Sum256(prepared.SignedArtifact)), prepared.SignedArtifact); err != nil {
		return nil, err
	}
	if hooks.afterPublish != nil {
		if err := hooks.afterPublish(); err != nil {
			return nil, err
		}
	}
	if _, _, err := chain.authenticate(ctx, options, alreadyAdopted); err != nil {
		return nil, err
	}
	if err := owner.check(); err != nil {
		return nil, err
	}
	if err := adoptEvidenceConsentRecoveryLocator(ctx, owner, leaf, prepared, limit, hooks); err != nil {
		return nil, err
	}
	digest, err := signed.Header.Digest()
	if err != nil {
		return nil, err
	}
	slot, err := signed.Header.SlotKey()
	if err != nil {
		return nil, err
	}
	result = &ValidatorEvidenceConsentRecoveryV2Result{OldManifestHash: options.ExpectedManifestHash, NewManifestHash: sha256.Sum256(prepared.NewManifest), SignedArtifactHash: sha256.Sum256(prepared.SignedArtifact), HeaderDigest: digest, Slot: slot, PreparedPath: filepath.Join(owner.audit.path, preparedName), CompletionPath: filepath.Join(owner.audit.path, completeName), Completed: true}
	completeRaw, err := marshalAttemptSettlementV2JSON(ctx, result, auditLimit, true, true)
	if err != nil {
		return nil, err
	}
	if err := writeEvidenceConsentRecoveryFile(ctx, owner.audit, completeName, completeRaw, auditLimit); err != nil {
		return nil, err
	}
	return result, nil
}

// Every namespace witness includes the original backend inode held by the
// daemon's writer, so path replacement cannot redirect a later restarted owner.
func (self *evidenceConsentRecoveryOwner) check() error {
	if self.operator == nil || self.ledger == nil {
		return errors.New("consent recovery writer fence is closed")
	}
	current, err := self.operator.stat(attemptLedgerStoreName)
	if err != nil {
		return err
	}
	anchor := self.ledgerAnchor
	if current.dev != anchor.dev || current.ino != anchor.ino || current.uid != anchor.uid || current.mode != anchor.mode {
		return errors.New("consent recovery original backend namespace changed")
	}
	for _, directory := range []*attemptPrivateDirectory{self.operator, self.state, self.locators, self.audit} {
		if directory != nil {
			if err := directory.check(); err != nil {
				return err
			}
		}
	}
	return nil
}
