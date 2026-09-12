//go:build linux || darwin

package validator

// Public replay owns content-addressed source bytes and fresh scratch. It
// cannot open a validator's ledger, read a signing key, or grant a runtime
// startup waiver. Mathematical replay and live historical source observation
// are separate checks; the final consumer must complete both.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The containing deployment archive authenticates this complete finite census
// and the original configuration independently of candidate measurements.
type ReleaseEvidenceV2ArchiveSource struct {
	Source      ReleaseEvidenceV2CaptureSource `json:"source"`
	SizeBytes   uint64                         `json:"size_bytes"`
	ContentHash string                         `json:"content_hash"`
}

type ReleaseEvidenceV2ArchiveOptions struct {
	Config         *ReleaseConfig
	Hotkey         [32]byte
	Origins        [2]string
	Sources        []ReleaseEvidenceV2ArchiveSource
	ReadSource     func(context.Context, ReleaseEvidenceV2CaptureSource) ([]byte, error)
	ScratchRoot    string
	MaximumBytes   uint64
	MaximumObjects uint64
	Adoption       *ReleaseHistoryAdoptionV2
}

// Neither an exported struct literal nor decoded JSON can create this owner.
// Returned measurements are available only after all signed history succeeds.
// A caller must Close and join its error before publishing any acceptance.
type ReleaseEvidenceV2Archive struct {
	owner        *releaseEvidenceV2ArchiveOwner
	history      *releaseEvidenceV2StartupHistory
	inputs       []releaseEvidenceV2ActivationInput
	intents      []ReleaseEvidenceV2CapturedIntent
	adoption     *releaseHistoryAdoptionV2
	measurements map[string]releaseEvidenceV2ArchiveMeasurement
	closed       bool
	closeErr     error
}

type releaseEvidenceV2ArchiveOwner struct {
	ctx        context.Context
	cfg        ReleaseConfig
	origins    [2]string
	root       string
	anchor     os.FileInfo
	sources    map[ReleaseEvidenceV2CaptureSource]ReleaseEvidenceV2ArchiveSource
	readSource func(context.Context, ReleaseEvidenceV2CaptureSource) ([]byte, error)
	ledgers    map[uint64]*releaseEvidenceV2ArchiveLedger
	closed     bool
}

// Lifetime trail state uses the existing bounded disk index. Only cut roots
// grow with the finite control census; no record-sized history map is kept.
type releaseEvidenceV2ArchiveLedger struct {
	identity AttemptLedgerIdentity
	vpk      ed25519.PublicKey
	index    *attemptCutV2ReplayScratch
	bounds   AttemptCutV2ReplayBounds
	last     uint64
	root     string
	rawBytes uint64
	anchors  map[uint64]string
}

type releaseEvidenceV2ArchiveCut struct {
	owner     *releaseEvidenceV2ArchiveOwner
	ledger    *releaseEvidenceV2ArchiveLedger
	cut       AttemptCutV2
	priorLast uint64
	priorRoot string
	seen      bool
	finished  bool
}

func newReleaseEvidenceV2ArchiveOwner(ctx context.Context, options ReleaseEvidenceV2ArchiveOptions) (*releaseEvidenceV2ArchiveOwner, error) {
	if ctx == nil || options.Config == nil || options.ReadSource == nil || options.Hotkey == ([32]byte{}) || options.MaximumBytes == 0 || options.MaximumObjects == 0 || len(options.Sources) == 0 || uint64(len(options.Sources)) > options.MaximumObjects {
		return nil, errors.New("archive V2 source owner or finite census is absent")
	}
	if err := options.Config.Validate(); err != nil {
		return nil, err
	}
	if options.Config.ProvisionalDeferClosedNativeInput {
		return nil, errors.New("public archive replay requires complete strict source verification")
	}
	if len(options.Config.Operators) != 2 || options.Origins != [2]string{options.Config.Operators[0].APIURL, options.Config.Operators[1].APIURL} {
		return nil, errors.New("archive V2 origins differ from the approved operator census")
	}
	if _, err := newReleaseEvidenceV2StartupReaders(options.Origins, options.Config.EvidenceV2.Bounds.Cut); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(options.Config)
	if err != nil {
		return nil, err
	}
	var cfg ReleaseConfig
	if err := json.Unmarshal(encoded, &cfg); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(options.ScratchRoot) || filepath.Clean(options.ScratchRoot) != options.ScratchRoot {
		return nil, errors.New("archive scratch root is not a clean absolute path")
	}
	anchor, err := os.Lstat(options.ScratchRoot)
	if err != nil || !attemptStorePrivateDirectory(anchor) {
		return nil, errors.Join(errors.New("archive scratch root must be private and owned"), err)
	}
	resolved, err := filepath.EvalSymlinks(options.ScratchRoot)
	if err != nil || resolved != options.ScratchRoot {
		return nil, errors.Join(errors.New("archive scratch root contains an alias"), err)
	}
	owner := &releaseEvidenceV2ArchiveOwner{ctx: ctx, cfg: cfg, origins: options.Origins, root: options.ScratchRoot, anchor: anchor, sources: make(map[ReleaseEvidenceV2CaptureSource]ReleaseEvidenceV2ArchiveSource, len(options.Sources)), readSource: options.ReadSource, ledgers: map[uint64]*releaseEvidenceV2ArchiveLedger{}}
	remaining := options.MaximumBytes
	contents := map[string]uint64{}
	for _, source := range options.Sources {
		if source.SizeBytes == 0 || source.Source.Kind == "" || source.Source.Name == "" || len(source.Source.Name) > 4096 || len(source.Source.Origin) > 4096 {
			return nil, errors.New("archive V2 source exceeds its finite admission")
		}
		if _, err := parseReleaseContentHash(source.ContentHash); err != nil {
			return nil, err
		}
		if _, exists := owner.sources[source.Source]; exists {
			return nil, errors.New("archive V2 source census repeats an identity")
		}
		owner.sources[source.Source] = source
		if prior, found := contents[source.ContentHash]; found {
			if prior != source.SizeBytes {
				return nil, errors.New("archive duplicate content has a conflicting exact size")
			}
		} else {
			if source.SizeBytes > remaining {
				return nil, errors.New("archive source bytes exceed their independent allowance")
			}
			remaining -= source.SizeBytes
			contents[source.ContentHash] = source.SizeBytes
		}
	}
	return owner, owner.check(ctx)
}

func (self *releaseEvidenceV2ArchiveOwner) check(ctx context.Context) error {
	if self == nil || self.closed || ctx == nil {
		return errors.New("archive V2 owner is closed")
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	info, err := os.Lstat(self.root)
	if err != nil || !attemptStorePrivateDirectory(info) || !os.SameFile(self.anchor, info) {
		return errors.Join(errors.New("archive scratch root changed ownership"), err)
	}
	return nil
}

func (self *releaseEvidenceV2ArchiveOwner) read(ctx context.Context, source ReleaseEvidenceV2CaptureSource, maximum uint64) ([]byte, error) {
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	ref, exists := self.sources[source]
	if !exists || maximum == 0 || ref.SizeBytes > maximum {
		return nil, errors.New("archive V2 source is absent or exceeds its independent limit")
	}
	encoded, err := self.readSource(ctx, source)
	if err != nil {
		return nil, err
	}
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	if uint64(len(encoded)) != ref.SizeBytes || ReleaseMeasurementContentHash(encoded) != ref.ContentHash {
		return nil, errors.New("archive V2 source changed its original content address")
	}
	return bytes.Clone(encoded), nil
}

func (self *releaseEvidenceV2ArchiveOwner) readPrivate(ctx context.Context, path string, maximum uint64, absent bool) ([]byte, error) {
	name, err := filepath.Rel(self.cfg.StateDir, path)
	if err != nil || name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
		return nil, errors.New("archive reference escapes its original validator namespace")
	}
	source := ReleaseEvidenceV2CaptureSource{Kind: "private", Name: filepath.ToSlash(name)}
	if _, found := self.sources[source]; !found && absent {
		return nil, self.check(ctx)
	}
	return self.read(ctx, source, maximum)
}

func (self *releaseEvidenceV2ArchiveOwner) scratch(ctx context.Context, noID uint64, purpose string) (string, error) {
	if err := self.check(ctx); err != nil {
		return "", err
	}
	if !validAttemptPrivateLeaf(purpose) || noID == 0 {
		return "", errors.New("archive replay scratch purpose is invalid")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return filepath.Join(self.root, fmt.Sprintf("no-%d-%s-%s", noID, purpose, hex.EncodeToString(nonce[:]))), nil
}

func (self *releaseEvidenceV2ArchiveOwner) reader(replica int) (AttemptStreamV2MetadataReader, AttemptStreamV2DataOpener) {
	origin := self.origins[replica]
	read := func(ctx context.Context, kind, hash string, size uint64) ([]byte, error) {
		value, err := self.read(ctx, ReleaseEvidenceV2CaptureSource{Kind: kind, Name: hash, Origin: origin}, size)
		if err != nil {
			return nil, err
		}
		if uint64(len(value)) != size || attemptHex32(sha256.Sum256(value)) != hash {
			return nil, errors.New("archive stream content address or exact size differs")
		}
		return value, nil
	}
	return func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			return read(ctx, "metadata", hash, size)
		}, func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			if kind != AttemptStreamV2Records && kind != AttemptStreamV2Proofs {
				return nil, errors.New("archive refuses an unknown data stream")
			}
			encoded, err := read(ctx, kind, hash, size)
			if err != nil {
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(encoded)), nil
		}
}

func (self *releaseEvidenceV2ArchiveOwner) newLedger(ctx context.Context, initial AttemptCutV2Context) error {
	noID := initial.Identity.NoID
	if self.ledgers[noID] != nil {
		return errors.New("archive lifetime ledger is duplicated")
	}
	path, err := self.scratch(ctx, noID, "lifetime")
	if err != nil {
		return err
	}
	disk := self.cfg.EvidenceV2.Bounds.Disk
	bounds := AttemptCutV2ReplayBounds{MaxRecordBytes: disk.MaxRecordBytes, MaxProofBytes: disk.MaxProofBytes, MaxTrails: disk.MaxTrailCount, MaxScratchBytes: disk.MaxStorageBytes, MaxScratchFiles: disk.MaxStorageFiles}
	index, err := newAttemptCutV2ReplayScratchWithParent(ctx, path, bounds, attemptCutV2ReplayHooks{}, nil)
	if err != nil {
		return err
	}
	vpk, err := canonicalAttemptHex32("archive lifetime VPK", initial.Identity.ValidatorVPK, false)
	if err != nil {
		return errors.Join(err, index.close())
	}
	self.ledgers[noID] = &releaseEvidenceV2ArchiveLedger{identity: initial.Identity, vpk: slices.Clone(vpk[:]), index: index, bounds: bounds, root: zeroAttemptHash(), anchors: map[uint64]string{0: zeroAttemptHash()}}
	return nil
}

func (self *releaseEvidenceV2ArchiveOwner) beginCut(ctx context.Context, noID uint64, cut AttemptCutV2) (*releaseEvidenceV2ArchiveCut, error) {
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	ledger := self.ledgers[noID]
	if ledger == nil || cut.Context.Identity != ledger.identity || cut.Context.FirstSequence == 0 || cut.Context.FirstSequence-1 > ledger.last || cut.LastSequence < ledger.last {
		return nil, errors.New("archive cut omits or regresses its complete lifetime prefix")
	}
	if root, exists := ledger.anchors[cut.Context.FirstSequence-1]; !exists || root != cut.Context.PriorRoot {
		return nil, errors.New("archive cut begins at an unauthenticated prefix")
	}
	return &releaseEvidenceV2ArchiveCut{owner: self, ledger: ledger, cut: cut, priorLast: ledger.last, priorRoot: ledger.root, seen: ledger.last < cut.Context.FirstSequence}, nil
}

func (self *releaseEvidenceV2ArchiveCut) visit(record AttemptRecord) error {
	if self == nil || self.finished {
		return errors.New("archive cut visitor is unavailable")
	}
	if err := self.owner.check(self.owner.ctx); err != nil {
		return err
	}
	if record.Sequence <= self.priorLast {
		if record.Sequence == self.priorLast {
			if self.seen || record.RecordHash != self.priorRoot {
				return errors.New("archive cut rewrites its authenticated prior prefix")
			}
			self.seen = true
		}
		return nil
	}
	if !self.seen || record.Sequence != self.ledger.last+1 || record.PreviousHash != self.ledger.root {
		return errors.New("archive cumulative signed record sequence is discontinuous")
	}
	disk := self.owner.cfg.EvidenceV2.Bounds.Disk
	if self.ledger.last >= disk.MaxRecordCount {
		return errors.New("archive lifetime record count exceeds its approved bound")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if uint64(len(raw)) > disk.MaxRecordBytes || uint64(len(raw)) > disk.MaxRawRecordBytes-self.ledger.rawBytes {
		return errors.New("archive lifetime record bytes exceed their approved bound")
	}
	if err := self.ledger.index.append(self.owner.ctx, record, raw, nil, 0, self.ledger.identity, self.ledger.vpk, self.ledger.bounds, int(self.ledger.last), nil); err != nil {
		return err
	}
	self.ledger.last, self.ledger.root = record.Sequence, record.RecordHash
	self.ledger.rawBytes += uint64(len(raw))
	return nil
}

func (self *releaseEvidenceV2ArchiveCut) finish(ctx context.Context) error {
	if self == nil || self.finished || !self.seen || self.ledger.last != self.cut.LastSequence || self.ledger.root != self.cut.Root {
		return errors.New("archive cut failed its complete cumulative prefix join")
	}
	if err := self.owner.check(ctx); err != nil {
		return err
	}
	if self.ledger.index.pending != 0 {
		return errors.New("archive cut retains an unfinished lifetime trail")
	}
	if self.ledger.last != 0 {
		if err := self.ledger.index.sync(ctx); err != nil {
			return err
		}
	}
	self.ledger.anchors[self.cut.LastSequence] = self.cut.Root
	self.finished = true
	return self.ledger.index.check()
}

func (self *releaseEvidenceV2ArchiveOwner) matchPrefix(ctx context.Context, noID, last uint64, root string) error {
	if err := self.check(ctx); err != nil {
		return err
	}
	ledger := self.ledgers[noID]
	if ledger == nil || ledger.anchors[last] != root {
		return errors.New("archive prefix has not passed complete signed lifetime replay")
	}
	return ledger.index.check()
}

func (self *ReleaseEvidenceV2Archive) Close() error {
	if self == nil {
		return nil
	}
	if self.closed {
		return self.closeErr
	}
	self.closed = true
	self.owner.closed = true
	var result error
	ids := make([]uint64, 0, len(self.owner.ledgers))
	for noID := range self.owner.ledgers {
		ids = append(ids, noID)
	}
	slices.Sort(ids)
	for _, noID := range ids {
		result = errors.Join(result, self.owner.ledgers[noID].index.close())
	}
	self.closeErr = errors.Join(result, self.owner.ctx.Err())
	return self.closeErr
}
