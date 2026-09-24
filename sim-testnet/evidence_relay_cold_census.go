//go:build linux || darwin

// Provisional phase entry checkpoints each complete two-origin publication
// independently of relay sends. Strict acceptance and actual send readers never
// receive this cache; fresh clocks, journal debits and slot admission stay live.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"golang.org/x/sys/unix"
)

const (
	evidenceRelayColdCensusSchema = "urnetwork-sim-relay-cold-census-v1"
	// Change this contract whenever complete public authentication changes.
	evidenceRelayColdCensusVerifierVersion = "complete-public-replicas-v1"
	evidenceRelayColdCensusDirectoryName   = "relay-cold-census-v1"
	evidenceRelayColdCensusMaximumEntries  = 2 * evidenceRelayStartupCacheMaximumSlots
	evidenceRelayColdCensusMaximumBytes    = 64 * 1024 * 1024
)

// Only the preparation worker mutates counters and the successfully observed
// file witnesses. There is no transaction-completion or acceptance state here.
type evidenceRelayColdCensusSession struct {
	runtime     *evidenceRelayRuntime
	contextHash string
	key         [32]byte
	output      io.Writer
	witnesses   []evidenceRelayStartupFileWitness
	budget      evidenceRelayColdCensusBudget
}

// The directory lock serializes writers across sessions. Atomic replacement
// changes this stamp; an unchanged stamp reuses one bounded directory census.
type evidenceRelayColdCensusDirectoryStamp struct {
	device   uint64
	inode    uint64
	modified int64
	changed  int64
}

// The whole cache has finite disk ownership across plans and source revisions.
// Replacement may briefly add one temporary file; its bytes are inside the cap.
// A full or unsafe directory disables additional persistence without pruning.
type evidenceRelayColdCensusBudget struct {
	stamp       evidenceRelayColdCensusDirectoryStamp
	fileSizeKVs map[string]int64
	bytes       int64
}

// Counts refer to complete publication objects, including every configured
// member at both public origins. A failed read never advances these counters.
type evidenceRelayColdCensusSource struct {
	session            *evidenceRelayColdCensusSession
	source             *evidenceRelaySource
	sourceHash         string
	total              uint64
	completed          uint64
	resumed            uint64
	checkpointed       uint64
	checkpointFailures uint64
	lastReported       time.Time
}

// Epoch geometry is independently reread at each startup. FinalizedBlock is
// zero in the durable identity so a later head cannot erase completed work.
type evidenceRelayColdCensusIdentity struct {
	Schema          string                           `json:"schema"`
	VerifierVersion string                           `json:"verifier_version"`
	ContextHash     string                           `json:"context_hash"`
	ValidatorId     uint64                           `json:"validator_id"`
	SourceHash      string                           `json:"source_hash"`
	ManifestHash    string                           `json:"manifest_hash"`
	Window          protocol.ValidatorEvidenceWindow `json:"window"`
	File            evidenceRelayStartupFileWitness  `json:"file"`
}

// The authentication tag binds only headers returned after complete public
// authentication. Payload buffers are released and are never a cache input.
type evidenceRelayColdCensusProof struct {
	Identity evidenceRelayColdCensusIdentity                `json:"identity"`
	Members  []validatorcomponent.ValidatorEvidenceSignedV2 `json:"members"`
}

// Entries are authenticated as a whole before any retained member is returned.
type evidenceRelayColdCensusEnvelope struct {
	Proof evidenceRelayColdCensusProof `json:"proof"`
	Mac   string                       `json:"hmac_sha256"`
}

// Per-entry commit hooks force cancellation boundaries in tests only. A
// cancellation after atomic replacement must not delete an authenticated entry.
type evidenceRelayColdCensusEntry struct {
	source       *evidenceRelayColdCensusSource
	identity     evidenceRelayColdCensusIdentity
	name         string
	maximumBytes int64
	window       protocol.ValidatorEvidenceWindow
	kind         uint8
	censusHash   [32]byte
	references   []validatorcomponent.ValidatorEvidencePublicationV2MemberReference
	beforeCommit func()
	afterCommit  func()
}

// Only exact non-accepting continuation invocations may remember historical
// public authentication. Construction neither reads nor creates cache files.
func (self *evidenceRelayRuntime) newEvidenceRelayColdCensusSession(ctx context.Context) (*evidenceRelayColdCensusSession, error) {
	if ctx == nil || self == nil || self.executor == nil || self.executor.cfg == nil || self.executor.plan == nil || self.executor.plan.EvidenceRelayContinuation == nil || self.executor.cfg.readOnlyAudit || self.retainedPublications != nil {
		return nil, nil
	}
	if self.hasPolicyRolloverSources() {
		return nil, nil
	}
	advisory, err := evidenceRelayContinuationForecastAdvisory(self.executor.cfg, self.executor.plan)
	if err != nil || !advisory {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := self.executor.auditAuthorizedConfig
	if cfg == nil {
		cfg = self.executor.cfg
	}
	if cfg.WalletMaterial == "" {
		return nil, nil
	}
	identity, err := self.evidenceRelayStartupContextHashFor(evidenceRelayColdCensusSchema, evidenceRelayColdCensusVerifierVersion, "")
	if err != nil {
		return nil, nil
	}
	return &evidenceRelayColdCensusSession{runtime: self, contextHash: identity, key: derive32(cfg, "relay-cold-census/v1"), output: os.Stderr}, nil
}

// The existing bounded inventory determines the complete workload before any
// entry is reused; a new manifest suffix does not invalidate earlier objects.
func (self *evidenceRelayColdCensusSession) beginSource(source *evidenceRelaySource, total uint64) *evidenceRelayColdCensusSource {
	if self == nil || source == nil || total > evidenceRelayStartupCacheMaximumSlots {
		return nil
	}
	identity, err := canonicalHashHex(struct {
		ValidatorId uint64
		StateDir    string
		Bounds      validatorcomponent.ReleaseEvidenceV2Bounds
		Activations []protocol.ValidatorEvidenceActivation
	}{ValidatorId: source.validatorId, StateDir: source.stateDir, Bounds: source.bounds, Activations: source.activations})
	if err != nil {
		return nil
	}
	result := &evidenceRelayColdCensusSource{session: self, source: source, sourceHash: identity, total: total}
	result.report()
	return result
}

// A cache key includes the full canonical locator and its exact private file
// witness, so a replaced locator cannot borrow an earlier verification.
func (self *evidenceRelayColdCensusSource) entry(ctx context.Context, manifest any, window protocol.ValidatorEvidenceWindow) *evidenceRelayColdCensusEntry {
	if self == nil || ctx == nil || ctx.Err() != nil || self.completed >= self.total {
		return nil
	}
	var path string
	var err error
	result := &evidenceRelayColdCensusEntry{source: self, window: window}
	switch manifest := manifest.(type) {
	case *validatorcomponent.ValidatorEvidencePublicationV2Manifest:
		path, err = validatorcomponent.ValidatorEvidencePublicationV2ManifestPath(self.source.stateDir, manifest.Epoch)
		result.kind, result.censusHash, result.references = protocol.ValidatorEvidenceClosedCensus, manifest.CensusHash, manifest.Members
	case *validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest:
		path, err = validatorcomponent.ValidatorEvidenceDepositAuditV2ManifestPath(self.source.stateDir, manifest.Epoch, manifest.Subject)
		result.kind, result.censusHash, result.references = protocol.ValidatorEvidenceDepositAudit, manifest.CensusHash, manifest.Members
	default:
		return nil
	}
	if err != nil || len(result.references) != len(self.source.activations) || len(result.references) == 0 {
		return nil
	}
	witness, err := evidenceRelayStartupWitness(self.session.runtime.executor.stateDir, path)
	if err != nil {
		return nil
	}
	manifestHash, _, err := evidenceRelayStartupManifestIdentity(manifest)
	if err != nil {
		return nil
	}
	window.FinalizedBlock = 0
	result.identity = evidenceRelayColdCensusIdentity{Schema: evidenceRelayColdCensusSchema, VerifierVersion: evidenceRelayColdCensusVerifierVersion,
		ContextHash: self.session.contextHash, ValidatorId: self.source.validatorId, SourceHash: self.sourceHash, ManifestHash: manifestHash, Window: window, File: witness}
	name, err := canonicalHashHex(result.identity)
	if err != nil {
		return nil
	}
	result.name = strings.TrimPrefix(name, "0x") + ".json"
	maximumBytes := uint64(4 * 1024)
	for _, reference := range result.references {
		maximum, ok := checkedAdd(maximumBytes, reference.SignedArtifactBytes)
		if !ok || maximum > evidenceRelayStartupCacheMaximumBytes {
			return nil
		}
		maximumBytes = maximum
	}
	result.maximumBytes = int64(maximumBytes)
	return result
}

// The tag is domain-separated from every other local optimization. All identity
// fields and every retained consent are covered by the same authentication.
func (self *evidenceRelayColdCensusEntry) authenticationTag(proof evidenceRelayColdCensusProof) []byte {
	wire, _ := json.Marshal(proof)
	mac := hmac.New(sha256.New, self.source.session.key[:])
	mac.Write([]byte(evidenceRelayColdCensusSchema + "\x00"))
	mac.Write(wire)
	return mac.Sum(nil)
}

// Recheck exact membership, locator hashes and both signatures with the fresh
// chain window. The Hmac additionally attests that both payload replicas were
// read completely; a signed header alone cannot create such an attestation.
func (self *evidenceRelayColdCensusEntry) validMembers(members []validatorcomponent.ValidatorEvidenceSignedV2) bool {
	if len(members) != len(self.references) {
		return false
	}
	for index, signed := range members {
		activation := self.source.source.activations[index]
		header, reference := signed.Header, self.references[index]
		domain, err := activation.EvidenceDomain()
		if err != nil || signed.Schema != validatorcomponent.ValidatorEvidenceSignedV2Schema || header.Hotkey != activation.Hotkey || header.NoID != activation.NoID ||
			header.VPK != activation.VPK || header.Kind != self.kind || header.CensusHash != self.censusHash || reference.NoId != activation.NoID ||
			header.Verify(domain, self.window, signed.VPKSignature, signed.HotkeySignature) != nil {
			return false
		}
		raw, err := json.Marshal(signed)
		if err != nil || uint64(len(raw)+1) != reference.SignedArtifactBytes || sha256.Sum256(append(raw, '\n')) != reference.SignedArtifactHash {
			return false
		}
	}
	return true
}

// Corrupt, substituted, excessive or unsafe candidates fall through to full
// public authentication. No partial result survives cancellation or failed close.
func (self *evidenceRelayColdCensusEntry) read(ctx context.Context) *validatorcomponent.ValidatorEvidenceCensusV2Publication {
	if self == nil || ctx == nil || ctx.Err() != nil {
		return nil
	}
	directory, err := openEvidenceRelayPrivateCacheDirectory(self.source.session.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, false)
	if err != nil {
		return nil
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), self.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	file := os.NewFile(uintptr(fd), self.name)
	defer file.Close()
	if requireHistoricalAuditPrivateMode(fd, unix.S_IFREG, 0o600) != nil {
		return nil
	}
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > self.maximumBytes {
		return nil
	}
	wire, err := io.ReadAll(io.LimitReader(file, self.maximumBytes+1))
	if err != nil || int64(len(wire)) > self.maximumBytes || file.Close() != nil || rejectDuplicatePostconditionJSONFields(wire) != nil {
		return nil
	}
	var envelope evidenceRelayColdCensusEnvelope
	if decodeStrictJSONBytes(wire, &envelope) != nil || envelope.Proof.Identity != self.identity {
		return nil
	}
	tag, err := hex.DecodeString(envelope.Mac)
	if err != nil || !hmac.Equal(tag, self.authenticationTag(envelope.Proof)) || !self.validMembers(envelope.Proof.Members) ||
		!self.source.session.runtime.matchEvidenceRelayStartupWitness(self.identity.File) || ctx.Err() != nil {
		return nil
	}
	result := &validatorcomponent.ValidatorEvidenceCensusV2Publication{Members: make([]validatorcomponent.ValidatorEvidenceMemberV2Publication, len(envelope.Proof.Members))}
	for index, member := range envelope.Proof.Members {
		result.Members[index].Evidence = member
	}
	self.source.session.witnesses = append(self.source.session.witnesses, self.identity.File)
	self.source.record(true, true)
	return result
}

// Publish only after the complete original reader returned successfully. The
// private temporary file is synced before replacement, then its directory is
// synced even if cancellation arrives just after the atomic commit.
func (self *evidenceRelayColdCensusEntry) save(ctx context.Context, publication *validatorcomponent.ValidatorEvidenceCensusV2Publication) bool {
	if self == nil || ctx == nil || ctx.Err() != nil || publication == nil {
		return false
	}
	proof := evidenceRelayColdCensusProof{Identity: self.identity, Members: make([]validatorcomponent.ValidatorEvidenceSignedV2, len(publication.Members))}
	for index, member := range publication.Members {
		proof.Members[index] = member.Evidence
	}
	if !self.validMembers(proof.Members) || !self.source.session.runtime.matchEvidenceRelayStartupWitness(self.identity.File) {
		return false
	}
	envelope := evidenceRelayColdCensusEnvelope{Proof: proof, Mac: hex.EncodeToString(self.authenticationTag(proof))}
	wire, err := json.Marshal(envelope)
	if err != nil || int64(len(wire)+1) > self.maximumBytes {
		return false
	}
	directory, err := openEvidenceRelayPrivateCacheDirectory(self.source.session.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, true)
	if err != nil {
		return false
	}
	defer directory.Close()
	if unix.Flock(int(directory.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return false
	}
	defer unix.Flock(int(directory.Fd()), unix.LOCK_UN)
	if !self.source.session.budget.admit(ctx, directory, self.name, int64(len(wire)+1)) {
		return false
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return false
	}
	temporary := ".tmp-" + hex.EncodeToString(random[:])
	directoryFd := int(directory.Fd())
	fd, err := unix.Openat(directoryFd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return false
	}
	defer unix.Unlinkat(directoryFd, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	if _, err := file.Write(append(wire, '\n')); err != nil || file.Sync() != nil || file.Close() != nil {
		return false
	}
	if self.beforeCommit != nil {
		self.beforeCommit()
	}
	if ctx.Err() != nil || !self.source.session.runtime.matchEvidenceRelayStartupWitness(self.identity.File) {
		return false
	}
	if err := unix.Renameat(directoryFd, temporary, directoryFd, self.name); err != nil {
		return false
	}
	if self.afterCommit != nil {
		self.afterCommit()
	}
	if directory.Sync() != nil {
		return false
	}
	self.source.session.budget.remember(directory, self.name, int64(len(wire)+1))
	return true
}

// A descriptor-owned identity avoids another directory traversal after every
// successful object. Only cache writers mutate retained entries, by rename.
func evidenceRelayColdCensusDirectoryIdentity(directory *os.File) (evidenceRelayColdCensusDirectoryStamp, error) {
	info, err := directory.Stat()
	if err != nil {
		return evidenceRelayColdCensusDirectoryStamp{}, err
	}
	device, inode, _, changed, err := evidenceRelayStartupFileIdentity(info)
	return evidenceRelayColdCensusDirectoryStamp{device: device, inode: inode, modified: info.ModTime().UnixNano(), changed: changed}, err
}

// Called under the directory flock. Changed directories are inventoried in
// bounded pages before a temporary entry can consume any additional disk.
func (self *evidenceRelayColdCensusBudget) admit(ctx context.Context, directory *os.File, name string, size int64) bool {
	stamp, err := evidenceRelayColdCensusDirectoryIdentity(directory)
	if err != nil || size <= 0 || size > evidenceRelayColdCensusMaximumBytes {
		return false
	}
	if self.fileSizeKVs == nil || self.stamp != stamp {
		if _, err := directory.Seek(0, io.SeekStart); err != nil {
			return false
		}
		self.fileSizeKVs, self.bytes = nil, 0
		sizes := map[string]int64{}
		for {
			if ctx.Err() != nil {
				return false
			}
			files, err := directory.ReadDir(int(evidenceRelayStartupDirectoryPageEntries))
			if err != nil && !errors.Is(err, io.EOF) || uint64(len(sizes)+len(files)) > evidenceRelayColdCensusMaximumEntries {
				return false
			}
			for _, file := range files {
				var stat unix.Stat_t
				if unix.Fstatat(int(directory.Fd()), file.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o777 != 0o600 || stat.Uid != uint32(os.Geteuid()) || stat.Size < 0 || stat.Size > evidenceRelayColdCensusMaximumBytes-self.bytes {
					return false
				}
				sizes[file.Name()] = stat.Size
				self.bytes += stat.Size
			}
			if errors.Is(err, io.EOF) {
				break
			}
		}
		self.fileSizeKVs, self.stamp = sizes, stamp
	}
	_, replacing := self.fileSizeKVs[name]
	// The old entry remains present while its temporary replacement is synced.
	return ctx.Err() == nil && (replacing || uint64(len(self.fileSizeKVs)) < evidenceRelayColdCensusMaximumEntries) && size <= evidenceRelayColdCensusMaximumBytes-self.bytes
}

// A successful replacement updates the owned byte census before releasing the
// directory lock. Failed stamp reads force a fresh bounded scan on the next save.
func (self *evidenceRelayColdCensusBudget) remember(directory *os.File, name string, size int64) {
	self.bytes += size - self.fileSizeKVs[name]
	self.fileSizeKVs[name] = size
	stamp, err := evidenceRelayColdCensusDirectoryIdentity(directory)
	if err != nil {
		self.fileSizeKVs = nil
		return
	}
	self.stamp = stamp
}

// A persistence failure changes only the reported resumable count. The verified
// publication remains subject to the caller's ordinary fresh slot admission.
func (self *evidenceRelayColdCensusSource) complete(ctx context.Context, entry *evidenceRelayColdCensusEntry, publication *validatorcomponent.ValidatorEvidenceCensusV2Publication) {
	if self == nil {
		return
	}
	durable := entry.save(ctx, publication)
	if entry != nil {
		self.session.witnesses = append(self.session.witnesses, entry.identity.File)
	}
	self.record(false, durable)
}

// Report initial, periodic and final progress, with no acceptance or relay
// completion implied by the number of authenticated publication objects.
func (self *evidenceRelayColdCensusSource) record(resumed, durable bool) {
	self.completed++
	if resumed {
		self.resumed++
	}
	if durable {
		self.checkpointed++
	} else {
		self.checkpointFailures++
	}
	if self.completed == self.total || self.completed%10 == 0 || time.Since(self.lastReported) >= 5*time.Second {
		self.report()
	}
}

// The worker owns the counters; the writer receives one complete progress line.
func (self *evidenceRelayColdCensusSource) report() {
	self.lastReported = time.Now()
	fmt.Fprintf(self.session.output, "sim-testnet: provisional relay cold public census; validator_id=%d total=%d completed=%d resumed=%d checkpointed=%d checkpoint_failures=%d final_acceptance=false\n",
		self.source.validatorId, self.total, self.completed, self.resumed, self.checkpointed, self.checkpointFailures)
}

// Admission may outlast a manifest replacement; cache hits and newly verified
// objects must still refer to their original witnesses before readiness.
func (self *evidenceRelayColdCensusSession) revalidate() error {
	if self == nil {
		return nil
	}
	for _, witness := range self.witnesses {
		if !self.runtime.matchEvidenceRelayStartupWitness(witness) {
			return errors.New("relay cold census publication changed during admission")
		}
	}
	return nil
}
