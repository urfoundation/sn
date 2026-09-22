//go:build linux || darwin

// The provisional relay may remember a completely verified immutable prefix.
// The cache is an optimization only: strict acceptance never consults it, and
// every uncached suffix still enters the ordinary admission and sender paths.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"golang.org/x/sys/unix"
)

const (
	evidenceRelayStartupCacheSchema = "urnetwork-sim-relay-startup-cache-v2"
	// Change this contract when immutable prefix verification changes. Runner
	// rebuilds and driver provenance do not change the verified prefix identity.
	evidenceRelayStartupCacheVerifierVersion = "exact-plan-prefix-v1"
	evidenceRelayStartupCacheDirectoryName   = "relay-startup-cache-v2"
	evidenceRelayStartupCacheMaximumBytes    = 16 * 1024 * 1024
	evidenceRelayStartupCacheMaximumSlots    = evidenceRelayContinuationExpandedSlots
	evidenceRelayStartupDirectoryPageEntries = uint64(128)
)

type evidenceRelayStartupFileWitness struct {
	Path               string `json:"path"`
	Size               int64  `json:"size"`
	Mode               uint32 `json:"mode"`
	Device             uint64 `json:"device"`
	Inode              uint64 `json:"inode"`
	ModifiedNanosecond int64  `json:"modified_nanosecond"`
	ChangedNanosecond  int64  `json:"changed_nanosecond"`
}

type evidenceRelayStartupManifest struct {
	Kind             string                          `json:"kind"`
	Epoch            uint64                          `json:"epoch"`
	ObservationEpoch uint64                          `json:"observation_epoch,omitempty"`
	NativeEpoch      uint64                          `json:"native_epoch,omitempty"`
	IdentitySha256   string                          `json:"identity_sha256"`
	File             evidenceRelayStartupFileWitness `json:"file"`
}

type evidenceRelayStartupAction struct {
	PlanHash      string                           `json:"plan_hash"`
	ActionId      string                           `json:"action_id"`
	IntentHash    string                           `json:"intent_hash"`
	Header        protocol.ValidatorEvidenceHeader `json:"header"`
	RequestSha256 string                           `json:"request_sha256"`
	ReceiptSha256 string                           `json:"receipt_sha256"`
	Request       evidenceRelayStartupFileWitness  `json:"request"`
	Receipt       evidenceRelayStartupFileWitness  `json:"receipt"`
}

type evidenceRelayStartupSourceProof struct {
	ValidatorId uint64                         `json:"validator_id"`
	NextEpoch   uint64                         `json:"next_epoch"`
	Through     uint64                         `json:"through,omitempty"`
	Completed   bool                           `json:"completed"`
	ClosedRoot  string                         `json:"closed_root"`
	AuditRoot   string                         `json:"audit_root"`
	Closed      []evidenceRelayStartupManifest `json:"closed"`
	Audits      []evidenceRelayStartupManifest `json:"audits"`
}

type evidenceRelayStartupCacheProof struct {
	Schema           string                            `json:"schema"`
	VerifierVersion  string                            `json:"verifier_version"`
	ExecutableSha256 string                            `json:"executable_sha256,omitempty"` // Authenticated v1 import only.
	ContextHash      string                            `json:"context_hash"`
	JournalCount     uint64                            `json:"journal_prefix_count"`
	JournalHash      string                            `json:"journal_prefix_hash"`
	JournalRoot      string                            `json:"journal_prefix_root"`
	ActionRoot       string                            `json:"action_root"`
	ReceiptRoot      string                            `json:"receipt_root"`
	SourceRoot       string                            `json:"source_root"`
	Actions          []evidenceRelayStartupAction      `json:"actions"`
	Sources          []evidenceRelayStartupSourceProof `json:"sources"`
}

type evidenceRelayStartupCacheEnvelope struct {
	Proof evidenceRelayStartupCacheProof `json:"proof"`
	Mac   string                         `json:"hmac_sha256"`
}

type evidenceRelayStartupCacheEntry struct {
	stateDir string
	name     string
	key      [32]byte
	fixed    evidenceRelayStartupCacheProof
}

type evidenceRelayStartupSourceInventory struct {
	closed []evidenceRelayStartupFileWitness
	audits []evidenceRelayStartupFileWitness
}

type evidenceRelayStartupActionDraft struct {
	planHash string
	action   Action
	header   protocol.ValidatorEvidenceHeader
}

type evidenceRelayStartupSourceState struct {
	proof      evidenceRelayStartupSourceProof
	targetNext uint64
}

// Only the relay worker mutates a session. The optional hook exists solely to
// make the journal append race deterministic in a focused unit test.
type evidenceRelayStartupSession struct {
	entry                *evidenceRelayStartupCacheEntry
	hit                  bool
	journalPrefixCount   uint64
	actionKVs            map[string]evidenceRelayStartupAction
	actionDraftKVs       map[string]evidenceRelayStartupActionDraft
	sourceKVs            map[uint64]*evidenceRelayStartupSourceState
	auditTargetKVs       map[evidenceRelayAuditKey][32]byte
	dirty                bool
	beforeJournalRecheck func()
}

type evidenceRelayRetainedResult struct {
	Schema              string                                          `json:"schema"`
	PlanHash            string                                          `json:"plan_hash"`
	Action              Action                                          `json:"action"`
	SignedTransaction   []byte                                          `json:"winner_signed_transaction"`
	Receipt             *ethTypes.Receipt                               `json:"winner_receipt"`
	Publication         validatorcomponent.ValidatorEvidencePublication `json:"publication"`
	OwnReceipt          *ethTypes.Receipt                               `json:"own_receipt,omitempty"`
	LostPublicationRace bool                                            `json:"lost_publication_race"`
}

// Keep completed verification keyed to its semantic contract and exact inputs.
func (self *evidenceRelayRuntime) evidenceRelayStartupContextHash() (string, error) {
	return self.evidenceRelayStartupContextHashFor(evidenceRelayStartupCacheSchema, evidenceRelayStartupCacheVerifierVersion, "")
}

// The legacy arguments reproduce the original authenticated context exactly;
// only the v2 identity deliberately omits the executable's incidental digest.
func (self *evidenceRelayRuntime) evidenceRelayStartupContextHashFor(schema, verifierVersion, executableHash string) (string, error) {
	if self == nil || self.executor == nil || self.executor.plan == nil {
		return "", errors.New("relay startup cache context is absent")
	}
	cfg := self.executor.auditAuthorizedConfig
	if cfg == nil {
		cfg = self.executor.cfg
	}
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Release == nil || cfg.Hyperparameters == nil || cfg.Policy == nil {
		return "", errors.New("relay startup cache configuration is absent")
	}
	type sourceContext struct {
		ValidatorId uint64
		StateDir    string
		Bounds      validatorcomponent.ReleaseEvidenceV2Bounds
		Activations []protocol.ValidatorEvidenceActivation
	}
	sources := make([]sourceContext, 0, len(self.sources))
	for _, source := range self.sources {
		sources = append(sources, sourceContext{ValidatorId: source.validatorId, StateDir: source.stateDir, Bounds: source.bounds, Activations: source.activations})
	}
	provisional := self.executor.cfg.provisionalResume
	if provisional == nil || provisional.Record == nil {
		return "", errors.New("relay startup cache provisional identity is absent")
	}
	return canonicalHashHex(struct {
		Schema                 string
		VerifierVersion        string
		ExecutableSha256       string `json:",omitempty"`
		Plan                   *SetupPlan
		Config                 *HarnessConfig
		Public                 *PublicManifest
		Release                *ReleaseLock
		Hyperparameters        *Hyperparameters
		Policy                 any
		ConfigHash             string
		PolicyHash             string
		DeploymentId           string
		ChainId                uint64
		GenesisHash            string
		Netuid                 uint16
		Authority              string
		OperationalRpcMode     string
		OperationalSubstrate   string
		OperationalEvm         string
		IndependentRpc         bool
		WalletPublic           string
		WalletHotkeyPublic     string
		Origins                [2]string
		Sources                []sourceContext
		ProvisionalSchema      string
		Provisional            bool
		FinalAcceptance        bool
		ProvisionalCommand     string
		ProvisionalScenario    string
		ProvisionalDeployment  string
		ProvisionalPlanHash    string
		ProvisionalConfigHash  string
		ProvisionalReleaseHash string
		ProvisionalSnRepo      string
	}{
		Schema: schema, VerifierVersion: verifierVersion,
		ExecutableSha256: executableHash, Plan: self.executor.plan, Config: cfg.Config, Public: cfg.Public,
		Release: cfg.Release, Hyperparameters: cfg.Hyperparameters, Policy: cfg.Policy,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, DeploymentId: cfg.Config.Deployment.DeploymentID,
		ChainId: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, Authority: cfg.Authority,
		OperationalRpcMode: cfg.OperationalRPCMode, OperationalSubstrate: cfg.OperationalSubstrate,
		OperationalEvm: cfg.OperationalEVM, IndependentRpc: independentRPCRequired(cfg),
		WalletPublic: cfg.WalletPublic, WalletHotkeyPublic: cfg.WalletHotkeyPublic,
		Origins: self.origins, Sources: sources, ProvisionalSchema: provisional.Record.Schema,
		Provisional: provisional.Record.Provisional, FinalAcceptance: provisional.Record.FinalAcceptance,
		ProvisionalCommand: provisional.Record.Command, ProvisionalScenario: provisional.Record.Scenario,
		ProvisionalDeployment: provisional.Record.DeploymentID, ProvisionalPlanHash: provisional.Record.PlanHash,
		ProvisionalConfigHash: provisional.Record.ConfigHash, ProvisionalReleaseHash: provisional.Record.ReleaseLockHash,
		ProvisionalSnRepo: provisional.Record.RetainedSNRepo,
	})
}

// Strict acceptance deliberately receives no prepared entry. A malformed or
// unauthenticated optimization also becomes an ordinary cold verification.
func (self *evidenceRelayRuntime) newEvidenceRelayStartupSession(ctx context.Context, entries []JournalEntry, inventories map[uint64]evidenceRelayStartupSourceInventory) (*evidenceRelayStartupSession, error) {
	if ctx == nil || self == nil || self.executor == nil || self.executor.cfg == nil || self.executor.plan == nil || self.executor.plan.EvidenceRelayContinuation == nil {
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
	if cfg == nil || cfg.WalletMaterial == "" {
		return nil, nil
	}
	contextHash, err := self.evidenceRelayStartupContextHash()
	if err != nil {
		return nil, nil
	}
	entry := &evidenceRelayStartupCacheEntry{stateDir: self.executor.stateDir,
		name: strings.TrimPrefix(contextHash, "0x") + ".json", key: derive32(cfg, "relay-startup-cache/v2"),
		fixed: evidenceRelayStartupCacheProof{Schema: evidenceRelayStartupCacheSchema,
			VerifierVersion: evidenceRelayStartupCacheVerifierVersion, ContextHash: contextHash}}
	session := &evidenceRelayStartupSession{entry: entry, actionKVs: map[string]evidenceRelayStartupAction{},
		actionDraftKVs: map[string]evidenceRelayStartupActionDraft{}, sourceKVs: map[uint64]*evidenceRelayStartupSourceState{},
		auditTargetKVs: map[evidenceRelayAuditKey][32]byte{}}
	proof, hit := entry.read(ctx)
	if !hit {
		proof, hit = self.readCompatibleEvidenceRelayStartupProof(ctx, entry, derive32(cfg, "relay-startup-cache/v1"), entries, inventories)
		if hit {
			entry.save(ctx, proof)
		}
	}
	if !hit || !self.validateEvidenceRelayStartupProof(proof, entries, inventories) {
		for _, source := range self.sources {
			session.sourceKVs[source.validatorId] = &evidenceRelayStartupSourceState{proof: evidenceRelayStartupSourceProof{ValidatorId: source.validatorId, NextEpoch: source.nextEpoch}}
		}
		return session, ctx.Err()
	}
	session.hit, session.journalPrefixCount = true, proof.JournalCount
	for _, action := range proof.Actions {
		session.actionKVs[action.ActionId] = action
	}
	for _, sourceProof := range proof.Sources {
		copyProof := sourceProof
		session.sourceKVs[sourceProof.ValidatorId] = &evidenceRelayStartupSourceState{proof: copyProof, targetNext: sourceProof.NextEpoch}
		for _, audit := range sourceProof.Audits {
			identity, err := decodeHex32("relay startup audit identity", audit.IdentitySha256)
			if err != nil {
				return nil, nil
			}
			session.auditTargetKVs[evidenceRelayAuditKey{validatorId: sourceProof.ValidatorId, epoch: audit.Epoch, observationEpoch: audit.ObservationEpoch, nativeEpoch: audit.NativeEpoch}] = identity
		}
	}
	for index := range self.sources {
		source := &self.sources[index]
		state := session.sourceKVs[source.validatorId]
		if state == nil {
			return nil, nil
		}
		source.nextEpoch = state.proof.NextEpoch
	}
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		for _, source := range self.sources {
			state := session.sourceKVs[source.validatorId]
			self.through[source.validatorId], self.completed[source.validatorId] = state.proof.Through, state.proof.Completed
		}
	}()
	return session, ctx.Err()
}

func relayStartupActionsRoot(actions []evidenceRelayStartupAction) (string, string, error) {
	actionRoot, err := canonicalHashHex(actions)
	if err != nil {
		return "", "", err
	}
	type receiptRootItem struct {
		ActionId string
		Sha256   string
	}
	receipts := make([]receiptRootItem, 0, len(actions))
	for _, action := range actions {
		receipts = append(receipts, receiptRootItem{ActionId: action.ActionId, Sha256: action.ReceiptSha256})
	}
	receiptRoot, err := canonicalHashHex(receipts)
	return actionRoot, receiptRoot, err
}

func (self *evidenceRelayRuntime) validateEvidenceRelayStartupProof(proof evidenceRelayStartupCacheProof, entries []JournalEntry, inventories map[uint64]evidenceRelayStartupSourceInventory) bool {
	if proof.Schema != evidenceRelayStartupCacheSchema || proof.VerifierVersion != evidenceRelayStartupCacheVerifierVersion || proof.JournalCount == 0 ||
		proof.JournalCount > uint64(len(entries)) || uint64(len(proof.Actions)) > evidenceRelayStartupCacheMaximumSlots || len(proof.Sources) != len(self.sources) {
		return false
	}
	prefix := entries[:proof.JournalCount]
	if prefix[len(prefix)-1].EntryHash != proof.JournalHash {
		return false
	}
	journalRoot, err := canonicalHashHex(prefix)
	if err != nil || journalRoot != proof.JournalRoot {
		return false
	}
	actionRoot, receiptRoot, err := relayStartupActionsRoot(proof.Actions)
	if err != nil || actionRoot != proof.ActionRoot || receiptRoot != proof.ReceiptRoot {
		return false
	}
	actionKVs := map[string]evidenceRelayStartupAction{}
	for index, action := range proof.Actions {
		if index > 0 && action.ActionId <= proof.Actions[index-1].ActionId || actionKVs[action.ActionId].ActionId != "" ||
			!validCanonicalHashHex(action.PlanHash) || !validCanonicalHashHex(action.IntentHash) || stringsTrimRelayPrefix(action.ActionId) == "" ||
			!validSHA256ContentHash(action.RequestSha256) || !validSHA256ContentHash(action.ReceiptSha256) ||
			!self.matchEvidenceRelayStartupWitness(action.Request) || !self.matchEvidenceRelayStartupWitness(action.Receipt) {
			return false
		}
		owned := false
		for _, entry := range prefix {
			if entry.ActionID == action.ActionId {
				if entry.PlanHash != action.PlanHash || entry.IntentHash != action.IntentHash || entry.DeploymentID != self.executor.plan.DeploymentID {
					return false
				}
				owned = true
			}
		}
		if !owned {
			return false
		}
		for _, entry := range entries[proof.JournalCount:] {
			if entry.ActionID == action.ActionId {
				return false
			}
		}
		actionKVs[action.ActionId] = action
	}
	for _, retained := range self.executor.plan.EvidenceRelayContinuation.Retained {
		slot, err := retained.Evidence.Header.SlotKey()
		if err != nil {
			return false
		}
		id := evidenceRelayActionPrefix + hex.EncodeToString(slot[:])
		if action, found := actionKVs[id]; !found || action.Header != retained.Evidence.Header {
			return false
		}
	}
	for index, source := range self.sources {
		sourceProof := proof.Sources[index]
		inventory, found := inventories[source.validatorId]
		if !found || sourceProof.ValidatorId != source.validatorId || sourceProof.NextEpoch < source.nextEpoch ||
			len(sourceProof.Closed) > len(inventory.closed) || len(sourceProof.Audits) > len(inventory.audits) {
			return false
		}
		closedRoot, err := canonicalHashHex(sourceProof.Closed)
		if err != nil || closedRoot != sourceProof.ClosedRoot {
			return false
		}
		auditRoot, err := canonicalHashHex(sourceProof.Audits)
		if err != nil || auditRoot != sourceProof.AuditRoot {
			return false
		}
		next := source.nextEpoch
		for manifestIndex, manifest := range sourceProof.Closed {
			if manifest.Kind != "closed" || manifest.Epoch != next || manifest.ObservationEpoch != 0 || manifest.NativeEpoch != 0 ||
				manifest.File != inventory.closed[manifestIndex] || !self.matchEvidenceRelayStartupWitness(manifest.File) || !validCanonicalHashHex(manifest.IdentitySha256) {
				return false
			}
			if next == ^uint64(0) {
				return false
			}
			next++
		}
		if sourceProof.NextEpoch != next || sourceProof.Completed != (len(sourceProof.Closed) != 0) ||
			sourceProof.Completed && sourceProof.Through != next-1 || !sourceProof.Completed && sourceProof.Through != 0 {
			return false
		}
		for manifestIndex, manifest := range sourceProof.Audits {
			if manifest.Kind != "audit" || manifest.ObservationEpoch <= manifest.Epoch || manifest.File != inventory.audits[manifestIndex] ||
				!self.matchEvidenceRelayStartupWitness(manifest.File) || !validCanonicalHashHex(manifest.IdentitySha256) {
				return false
			}
			if manifestIndex > 0 && manifest.File.Path <= sourceProof.Audits[manifestIndex-1].File.Path {
				return false
			}
		}
	}
	sourceRoot, err := canonicalHashHex(proof.Sources)
	return err == nil && sourceRoot == proof.SourceRoot
}

func (entry *evidenceRelayStartupCacheEntry) authenticationTag(proof evidenceRelayStartupCacheProof) []byte {
	wire, _ := json.Marshal(proof)
	mac := hmac.New(sha256.New, entry.key[:])
	mac.Write([]byte(proof.Schema + "\x00"))
	mac.Write(wire)
	return mac.Sum(nil)
}

func (entry *evidenceRelayStartupCacheEntry) read(ctx context.Context) (evidenceRelayStartupCacheProof, bool) {
	var proof evidenceRelayStartupCacheProof
	if entry == nil || ctx == nil || ctx.Err() != nil {
		return proof, false
	}
	directory, err := openEvidenceRelayStartupCacheDirectory(entry.stateDir, false)
	if err != nil {
		return proof, false
	}
	defer directory.Close()
	envelope, _, ok := readEvidenceRelayStartupCacheEnvelope(directory, entry.name, evidenceRelayStartupCacheMaximumBytes)
	if !ok || envelope.Proof.Schema != entry.fixed.Schema ||
		envelope.Proof.VerifierVersion != entry.fixed.VerifierVersion || envelope.Proof.ExecutableSha256 != entry.fixed.ExecutableSha256 || envelope.Proof.ContextHash != entry.fixed.ContextHash {
		return proof, false
	}
	tag, err := hex.DecodeString(envelope.Mac)
	if err != nil || !hmac.Equal(tag, entry.authenticationTag(envelope.Proof)) || ctx.Err() != nil {
		return proof, false
	}
	return envelope.Proof, true
}

// Decode a bounded private candidate; the caller must authenticate all bytes.
func readEvidenceRelayStartupCacheEnvelope(directory *os.File, name string, maximumBytes int64) (evidenceRelayStartupCacheEnvelope, int64, bool) {
	var envelope evidenceRelayStartupCacheEnvelope
	if maximumBytes <= 0 || maximumBytes > evidenceRelayStartupCacheMaximumBytes {
		return envelope, 0, false
	}
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return envelope, 0, false
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	if err := requireHistoricalAuditPrivateMode(fd, unix.S_IFREG, 0o600); err != nil {
		return envelope, 0, false
	}
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > maximumBytes {
		return envelope, 0, false
	}
	wire, err := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	readBytes := int64(len(wire))
	if err != nil || readBytes > maximumBytes || rejectDuplicatePostconditionJSONFields(wire) != nil {
		return envelope, readBytes, false
	}
	if err := decodeStrictJSONBytes(wire, &envelope); err != nil {
		return envelope, readBytes, false
	}
	return envelope, readBytes, true
}

func openEvidenceRelayStartupCacheDirectory(stateDir string, create bool) (*os.File, error) {
	return openEvidenceRelayPrivateCacheDirectory(stateDir, evidenceRelayStartupCacheDirectoryName, create)
}

// Both provisional caches retain private descriptor-owned entries beneath the
// original state owner; a link in any path component cannot redirect writes.
func openEvidenceRelayPrivateCacheDirectory(stateDir, name string, create bool) (*os.File, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return nil, errors.New("relay cache directory name is not canonical")
	}
	path, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		next, openErr := unix.Openat(fd, component, flags, 0)
		unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	defer unix.Close(fd)
	if err := requireHistoricalAuditPrivateMode(fd, unix.S_IFDIR, 0o700); err != nil {
		return nil, err
	}
	if create {
		if err := unix.Mkdirat(fd, name, 0o700); err == nil {
			if err := unix.Fsync(fd); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, unix.EEXIST) {
			return nil, err
		}
	}
	cacheFD, err := unix.Openat(fd, name, flags, 0)
	if err != nil {
		return nil, err
	}
	if err := requireHistoricalAuditPrivateMode(cacheFD, unix.S_IFDIR, 0o700); err != nil {
		unix.Close(cacheFD)
		return nil, err
	}
	return os.NewFile(uintptr(cacheFD), name), nil
}

func (entry *evidenceRelayStartupCacheEntry) save(ctx context.Context, proof evidenceRelayStartupCacheProof) bool {
	if entry == nil || ctx == nil || ctx.Err() != nil || proof.Schema != entry.fixed.Schema || proof.VerifierVersion != entry.fixed.VerifierVersion ||
		proof.ExecutableSha256 != entry.fixed.ExecutableSha256 || proof.ContextHash != entry.fixed.ContextHash {
		return false
	}
	envelope := evidenceRelayStartupCacheEnvelope{Proof: proof, Mac: hex.EncodeToString(entry.authenticationTag(proof))}
	wire, err := json.Marshal(envelope)
	if err != nil || len(wire) >= evidenceRelayStartupCacheMaximumBytes {
		return false
	}
	directory, err := openEvidenceRelayStartupCacheDirectory(entry.stateDir, true)
	if err != nil {
		return false
	}
	defer directory.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return false
	}
	temporary := ".tmp-" + hex.EncodeToString(random[:])
	directoryFD := int(directory.Fd())
	fd, err := unix.Openat(directoryFD, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return false
	}
	defer unix.Unlinkat(directoryFD, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	if _, err := file.Write(append(wire, '\n')); err != nil || file.Sync() != nil || file.Close() != nil || ctx.Err() != nil {
		return false
	}
	if err := unix.Renameat(directoryFD, temporary, directoryFD, entry.name); err != nil {
		return false
	}
	if err := errors.Join(directory.Sync(), ctx.Err()); err != nil {
		_ = unix.Unlinkat(directoryFD, entry.name, 0)
		_ = directory.Sync()
		return false
	}
	return true
}

func evidenceRelayStartupWitness(stateDir, path string) (evidenceRelayStartupFileWitness, error) {
	var witness evidenceRelayStartupFileWitness
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return witness, errors.New("relay startup witness path is not canonical")
	}
	relative, err := filepath.Rel(stateDir, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return witness, errors.New("relay startup witness escapes its state owner")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return witness, err
	}
	device, inode, uid, changed, err := evidenceRelayStartupFileIdentity(info)
	if err != nil || !info.Mode().IsRegular() || uid != uint32(os.Geteuid()) || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 {
		return witness, errors.Join(errors.New("relay startup witness has an unsafe file type, owner, mode or size"), err)
	}
	return evidenceRelayStartupFileWitness{Path: filepath.ToSlash(relative), Size: info.Size(), Mode: uint32(info.Mode().Perm()), Device: device,
		Inode: inode, ModifiedNanosecond: info.ModTime().UnixNano(), ChangedNanosecond: changed}, nil
}

func (self *evidenceRelayRuntime) matchEvidenceRelayStartupWitness(want evidenceRelayStartupFileWitness) bool {
	if self == nil || self.executor == nil || want.Path == "" || filepath.IsAbs(want.Path) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(want.Path))) != want.Path {
		return false
	}
	got, err := evidenceRelayStartupWitness(self.executor.stateDir, filepath.Join(self.executor.stateDir, filepath.FromSlash(want.Path)))
	return err == nil && got == want
}

func readEvidenceRelayStartupDirectory(ctx context.Context, stateDir, path string, slotsPerFile uint64, remainingSlots, remainingBytes *uint64, maximumBytes uint64) ([]evidenceRelayStartupFileWitness, error) {
	if ctx == nil || remainingSlots == nil || remainingBytes == nil || slotsPerFile == 0 || maximumBytes == 0 || *remainingBytes == 0 {
		return nil, errors.New("relay startup discovery bound is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(stateDir, path)
	if err != nil || !filepath.IsAbs(stateDir) || !filepath.IsAbs(path) || filepath.Clean(path) != path || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.Join(errors.New("relay startup manifest directory escapes its state owner"), err)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), path)
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return nil, err
	}
	_, _, uid, _, identityErr := evidenceRelayStartupFileIdentity(info)
	if identityErr != nil || !info.IsDir() || uid != uint32(os.Geteuid()) || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.Join(errors.New("relay startup manifest directory is unsafe"), identityErr)
	}
	maximumFiles := *remainingSlots / slotsPerFile
	if *remainingSlots > evidenceRelayStartupCacheMaximumSlots {
		return nil, errors.New("relay startup discovery exceeds its finite aggregate slot bound")
	}
	var entries []os.DirEntry
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		readLimit := min(evidenceRelayStartupDirectoryPageEntries, maximumFiles-uint64(len(entries))+1)
		page, readErr := directory.ReadDir(int(readLimit))
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		if uint64(len(entries))+uint64(len(page)) > maximumFiles {
			return nil, errors.New("evidence relay observed manifest slots exceed remaining approved capacity before historical reads")
		}
		entries = append(entries, page...)
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	used := uint64(len(entries)) * slotsPerFile
	*remainingSlots -= used
	result := make([]evidenceRelayStartupFileWitness, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		witness, err := evidenceRelayStartupWitness(stateDir, filepath.Join(path, entry.Name()))
		if err != nil || uint64(witness.Size) > min(*remainingBytes, maximumBytes) {
			return nil, errors.Join(errors.New("relay startup manifest exceeds its bounded private file owner"), err)
		}
		*remainingBytes -= uint64(witness.Size)
		result = append(result, witness)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, ctx.Err()
}

// Count the complete configured-member slots before opening any historical
// locator object. Each approval supplies its own monetary cap; the separate
// page and process bounds do not increase old 1024-slot approvals.
func (self *evidenceRelayRuntime) evidenceRelayStartupInventories(ctx context.Context, maximum uint64) (map[uint64]evidenceRelayStartupSourceInventory, error) {
	if maximum == 0 || maximum > evidenceRelayStartupCacheMaximumSlots {
		return nil, errors.New("relay startup slot bound is absent or exceeds 2048")
	}
	remaining := maximum
	result := map[uint64]evidenceRelayStartupSourceInventory{}
	for _, source := range self.sources {
		members := uint64(len(source.activations))
		if members == 0 {
			return nil, errors.New("relay startup source has no configured members")
		}
		closedBytes := source.bounds.MaxHistoryBytes
		closed, err := readEvidenceRelayStartupDirectory(ctx, self.executor.stateDir, filepath.Join(source.stateDir, "evidence-publications"), members, &remaining, &closedBytes, source.bounds.MaxClosureBytes)
		if err != nil {
			return nil, err
		}
		auditBytes := source.bounds.MaxHistoryBytes
		audits, err := readEvidenceRelayStartupDirectory(ctx, self.executor.stateDir, filepath.Join(source.stateDir, "evidence-deposit-audits"), members, &remaining, &auditBytes, source.bounds.MaxClosureBytes)
		if err != nil {
			return nil, err
		}
		result[source.validatorId] = evidenceRelayStartupSourceInventory{closed: closed, audits: audits}
	}
	return result, ctx.Err()
}

func evidenceRelayStartupWitnessPath(stateDir string, witness evidenceRelayStartupFileWitness) string {
	return filepath.Join(stateDir, filepath.FromSlash(witness.Path))
}

func (self *evidenceRelayRuntime) readEvidenceRelayStartupManifests(ctx context.Context, source *evidenceRelaySource, inventory evidenceRelayStartupSourceInventory) ([]validatorcomponent.ValidatorEvidencePublicationV2Manifest, []validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest, error) {
	closedStart, auditStart := 0, 0
	if self.startupCache != nil && self.startupCache.hit {
		state := self.startupCache.sourceKVs[source.validatorId]
		if state == nil {
			return nil, nil, errors.New("relay startup cache lost a configured source")
		}
		closedStart, auditStart = len(state.proof.Closed), len(state.proof.Audits)
	}
	closed := make([]validatorcomponent.ValidatorEvidencePublicationV2Manifest, 0, len(inventory.closed)-closedStart)
	for _, witness := range inventory.closed[closedStart:] {
		manifest, err := validatorcomponent.ReadValidatorEvidencePublicationV2Manifest(ctx, evidenceRelayStartupWitnessPath(self.executor.stateDir, witness), source.bounds.MaxClosureBytes, source.bounds.MaxParticipants)
		if err != nil || !self.matchEvidenceRelayStartupWitness(witness) {
			return nil, nil, errors.Join(errors.New("relay startup closed manifest changed during suffix read"), err)
		}
		closed = append(closed, *manifest)
	}
	audits := make([]validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest, 0, len(inventory.audits)-auditStart)
	for _, witness := range inventory.audits[auditStart:] {
		manifest, err := validatorcomponent.ReadValidatorEvidenceDepositAuditV2Manifest(ctx, evidenceRelayStartupWitnessPath(self.executor.stateDir, witness), source.bounds.MaxClosureBytes, source.bounds.MaxParticipants)
		if err != nil || !self.matchEvidenceRelayStartupWitness(witness) {
			return nil, nil, errors.Join(errors.New("relay startup audit manifest changed during suffix read"), err)
		}
		audits = append(audits, *manifest)
	}
	return closed, audits, ctx.Err()
}

func (session *evidenceRelayStartupSession) observeSource(source *evidenceRelaySource, closed []validatorcomponent.ValidatorEvidencePublicationV2Manifest, audits []validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest) error {
	if session == nil {
		return nil
	}
	state := session.sourceKVs[source.validatorId]
	if state == nil {
		return errors.New("relay startup source target is absent")
	}
	next := state.proof.NextEpoch
	for _, manifest := range closed {
		if manifest.Epoch < next {
			continue
		}
		if manifest.Epoch != next {
			break
		}
		if next == ^uint64(0) {
			return errors.New("relay startup target epoch overflows")
		}
		next++
	}
	state.targetNext = next
	for _, manifest := range audits {
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		key := evidenceRelayAuditKey{validatorId: source.validatorId, epoch: manifest.Epoch, observationEpoch: manifest.Subject.ObservationEpoch, nativeEpoch: manifest.Subject.NativeEpoch}
		session.auditTargetKVs[key] = sha256.Sum256(encoded)
	}
	return nil
}

func (session *evidenceRelayStartupSession) cachedAction(actionId string) (evidenceRelayStartupAction, bool) {
	if session == nil || !session.hit {
		return evidenceRelayStartupAction{}, false
	}
	action, found := session.actionKVs[actionId]
	return action, found
}

func (session *evidenceRelayStartupSession) revalidate(runtime *evidenceRelayRuntime) bool {
	if session == nil || !session.hit || runtime == nil {
		return session == nil || !session.hit
	}
	for _, action := range session.actionKVs {
		if !runtime.matchEvidenceRelayStartupWitness(action.Request) || !runtime.matchEvidenceRelayStartupWitness(action.Receipt) {
			return false
		}
	}
	for _, state := range session.sourceKVs {
		for _, manifest := range append(append([]evidenceRelayStartupManifest(nil), state.proof.Closed...), state.proof.Audits...) {
			if !runtime.matchEvidenceRelayStartupWitness(manifest.File) {
				return false
			}
		}
	}
	return true
}

func (session *evidenceRelayStartupSession) seedCompletedAudits(result map[evidenceRelayAuditKey][32]byte) {
	if session == nil || !session.hit {
		return
	}
	for validatorId, state := range session.sourceKVs {
		for _, audit := range state.proof.Audits {
			identity, err := decodeHex32("relay startup audit identity", audit.IdentitySha256)
			if err != nil {
				continue
			}
			key := evidenceRelayAuditKey{validatorId: validatorId, epoch: audit.Epoch, observationEpoch: audit.ObservationEpoch, nativeEpoch: audit.NativeEpoch}
			result[key] = identity
		}
	}
}

func (session *evidenceRelayStartupSession) rememberAction(planHash string, action Action, header protocol.ValidatorEvidenceHeader) error {
	if session == nil {
		return nil
	}
	if cached, found := session.actionKVs[action.ID]; found {
		if cached.PlanHash != planHash || cached.IntentHash != action.IntentHash || cached.Header != header {
			return errors.New("relay startup cached action changed during verification")
		}
		return nil
	}
	if prior, found := session.actionDraftKVs[action.ID]; found {
		if prior.planHash != planHash || !reflect.DeepEqual(prior.action, action) || prior.header != header {
			return errors.New("relay startup action changed within one verification")
		}
		return nil
	}
	session.actionDraftKVs[action.ID] = evidenceRelayStartupActionDraft{planHash: planHash, action: action, header: header}
	session.dirty = true
	return nil
}

func evidenceRelayStartupManifestIdentity(value any) (string, [32]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", [32]byte{}, err
	}
	identity := sha256.Sum256(encoded)
	return "0x" + hex.EncodeToString(identity[:]), identity, nil
}

func (session *evidenceRelayStartupSession) completeClosed(runtime *evidenceRelayRuntime, source *evidenceRelaySource, manifest *validatorcomponent.ValidatorEvidencePublicationV2Manifest) error {
	if session == nil {
		return nil
	}
	state := session.sourceKVs[source.validatorId]
	if state == nil || manifest.Epoch != state.proof.NextEpoch {
		return errors.New("relay startup closed checkpoint is not the earliest incomplete epoch")
	}
	path, err := validatorcomponent.ValidatorEvidencePublicationV2ManifestPath(source.stateDir, manifest.Epoch)
	if err != nil {
		return err
	}
	witness, err := evidenceRelayStartupWitness(runtime.executor.stateDir, path)
	if err != nil {
		return err
	}
	identity, _, err := evidenceRelayStartupManifestIdentity(manifest)
	if err != nil {
		return err
	}
	state.proof.Closed = append(state.proof.Closed, evidenceRelayStartupManifest{Kind: "closed", Epoch: manifest.Epoch, IdentitySha256: identity, File: witness})
	state.proof.NextEpoch++
	state.proof.Through, state.proof.Completed = manifest.Epoch, true
	session.dirty = true
	return nil
}

func (session *evidenceRelayStartupSession) completeAudit(runtime *evidenceRelayRuntime, source *evidenceRelaySource, manifest *validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest) ([32]byte, error) {
	if session == nil {
		_, identity, err := evidenceRelayStartupManifestIdentity(manifest)
		return identity, err
	}
	state := session.sourceKVs[source.validatorId]
	if state == nil {
		return [32]byte{}, errors.New("relay startup audit checkpoint source is absent")
	}
	identityHex, identity, err := evidenceRelayStartupManifestIdentity(manifest)
	if err != nil {
		return identity, err
	}
	path, err := validatorcomponent.ValidatorEvidenceDepositAuditV2ManifestPath(source.stateDir, manifest.Epoch, manifest.Subject)
	if err != nil {
		return identity, err
	}
	witness, err := evidenceRelayStartupWitness(runtime.executor.stateDir, path)
	if err != nil {
		return identity, err
	}
	for _, retained := range state.proof.Audits {
		if retained.Epoch == manifest.Epoch && retained.ObservationEpoch == manifest.Subject.ObservationEpoch && retained.NativeEpoch == manifest.Subject.NativeEpoch {
			if retained.IdentitySha256 != identityHex || retained.File != witness {
				return identity, errors.New("relay startup completed audit changed")
			}
			return identity, nil
		}
	}
	if len(state.proof.Audits) != 0 && witness.Path <= state.proof.Audits[len(state.proof.Audits)-1].File.Path {
		return identity, errors.New("relay startup audit prefix was reordered")
	}
	state.proof.Audits = append(state.proof.Audits, evidenceRelayStartupManifest{Kind: "audit", Epoch: manifest.Epoch,
		ObservationEpoch: manifest.Subject.ObservationEpoch, NativeEpoch: manifest.Subject.NativeEpoch, IdentitySha256: identityHex, File: witness})
	session.dirty = true
	return identity, nil
}

func (session *evidenceRelayStartupSession) ready(runtime *evidenceRelayRuntime, completedAudits map[evidenceRelayAuditKey][32]byte) bool {
	if session == nil || runtime == nil {
		return false
	}
	for _, source := range runtime.sources {
		state := session.sourceKVs[source.validatorId]
		if state == nil || source.nextEpoch < state.targetNext {
			return false
		}
	}
	for key, identity := range session.auditTargetKVs {
		if completedAudits[key] != identity {
			return false
		}
	}
	return true
}

func (session *evidenceRelayStartupSession) buildAction(ctx context.Context, runtime *evidenceRelayRuntime, entries []JournalEntry, draft evidenceRelayStartupActionDraft, owners map[string]*SetupPlan) (evidenceRelayStartupAction, error) {
	owner, request, requestRaw, err := readOwnedEvidenceRelayRequest(ctx, runtime.executor.stateDir, runtime.executor.plan, entries, draft.action.ID, owners)
	if err != nil || owner.PlanHash != draft.planHash || !reflect.DeepEqual(request.Action, draft.action) || request.Evidence.Evidence.Header != draft.header {
		return evidenceRelayStartupAction{}, errors.Join(errors.New("relay startup action no longer matches its successful verification"), err)
	}
	receiptPath := filepath.Join(runtime.executor.stateDir, "evidence-relay", draft.action.ID+".receipt.json")
	receiptRaw, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, receiptPath, 4*1024*1024)
	if err != nil {
		return evidenceRelayStartupAction{}, err
	}
	var result evidenceRelayRetainedResult
	if err := decodeStrictJSONBytes(receiptRaw, &result); err != nil || result.Schema != "urnetwork-sim-evidence-relay-result-v2" || result.PlanHash != owner.PlanHash ||
		!reflect.DeepEqual(result.Action, request.Action) || result.Publication.Header != draft.header || result.Receipt == nil || len(result.SignedTransaction) == 0 {
		return evidenceRelayStartupAction{}, errors.Join(errors.New("relay startup successful result is incomplete or changed"), err)
	}
	if err := validateFinalRelayRetainedReceiptState(result.Receipt, result.OwnReceipt, result.LostPublicationRace); err != nil {
		return evidenceRelayStartupAction{}, err
	}
	transaction := new(ethTypes.Transaction)
	if decodeErr := transaction.UnmarshalBinary(result.SignedTransaction); decodeErr != nil {
		return evidenceRelayStartupAction{}, decodeErr
	}
	canonical, marshalErr := transaction.MarshalBinary()
	if marshalErr != nil || !reflect.DeepEqual(canonical, result.SignedTransaction) || transaction.Hash() != result.Receipt.TxHash || result.Receipt.BlockNumber == nil || result.Publication.PublishedBlock != result.Receipt.BlockNumber.Uint64() {
		return evidenceRelayStartupAction{}, errors.Join(errors.New("relay startup result transaction or receipt changed"), marshalErr)
	}
	requestPath := filepath.Join(runtime.executor.stateDir, "evidence-relay", stringsTrimRelayPrefix(draft.action.ID)+".json")
	requestWitness, err := evidenceRelayStartupWitness(runtime.executor.stateDir, requestPath)
	if err != nil {
		return evidenceRelayStartupAction{}, err
	}
	receiptWitness, err := evidenceRelayStartupWitness(runtime.executor.stateDir, receiptPath)
	if err != nil {
		return evidenceRelayStartupAction{}, err
	}
	return evidenceRelayStartupAction{PlanHash: draft.planHash, ActionId: draft.action.ID, IntentHash: draft.action.IntentHash, Header: draft.header,
		RequestSha256: bytesSHA256(requestRaw), ReceiptSha256: bytesSHA256(receiptRaw), Request: requestWitness, Receipt: receiptWitness}, nil
}

// Build from a stable journal snapshot and recheck it after every file read.
// A concurrent append leaves the prior cache intact and never publishes the
// candidate proof.
func (session *evidenceRelayStartupSession) checkpoint(ctx context.Context, runtime *evidenceRelayRuntime, completedAudits map[evidenceRelayAuditKey][32]byte) bool {
	if session == nil || session.entry == nil || !session.dirty || ctx == nil || ctx.Err() != nil || !session.ready(runtime, completedAudits) {
		return false
	}
	entries := runtime.executor.journal.Entries()
	if len(entries) == 0 {
		return false
	}
	actions := make([]evidenceRelayStartupAction, 0, len(session.actionKVs)+len(session.actionDraftKVs))
	for _, action := range session.actionKVs {
		if !runtime.matchEvidenceRelayStartupWitness(action.Request) || !runtime.matchEvidenceRelayStartupWitness(action.Receipt) {
			return false
		}
		actions = append(actions, action)
	}
	owners := map[string]*SetupPlan{}
	for _, draft := range session.actionDraftKVs {
		action, err := session.buildAction(ctx, runtime, entries, draft, owners)
		if err != nil {
			return false
		}
		actions = append(actions, action)
	}
	if uint64(len(actions)) > evidenceRelayStartupCacheMaximumSlots {
		return false
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].ActionId < actions[j].ActionId })
	actionKVs := make(map[string]evidenceRelayStartupAction, len(actions))
	for index := 1; index < len(actions); index++ {
		if actions[index-1].ActionId == actions[index].ActionId {
			return false
		}
	}
	for _, action := range actions {
		actionKVs[action.ActionId] = action
	}
	for _, retained := range runtime.executor.plan.EvidenceRelayContinuation.Retained {
		slot, err := retained.Evidence.Header.SlotKey()
		if err != nil {
			return false
		}
		action, found := actionKVs[evidenceRelayActionPrefix+hex.EncodeToString(slot[:])]
		if !found || action.Header != retained.Evidence.Header {
			return false
		}
	}
	actionRoot, receiptRoot, err := relayStartupActionsRoot(actions)
	if err != nil {
		return false
	}
	sources := make([]evidenceRelayStartupSourceProof, 0, len(runtime.sources))
	for _, source := range runtime.sources {
		state := session.sourceKVs[source.validatorId]
		if state == nil || source.nextEpoch != state.proof.NextEpoch {
			return false
		}
		for _, manifest := range append(append([]evidenceRelayStartupManifest(nil), state.proof.Closed...), state.proof.Audits...) {
			if !runtime.matchEvidenceRelayStartupWitness(manifest.File) {
				return false
			}
		}
		state.proof.ClosedRoot, err = canonicalHashHex(state.proof.Closed)
		if err != nil {
			return false
		}
		state.proof.AuditRoot, err = canonicalHashHex(state.proof.Audits)
		if err != nil {
			return false
		}
		sources = append(sources, state.proof)
	}
	sourceRoot, err := canonicalHashHex(sources)
	if err != nil {
		return false
	}
	journalRoot, err := canonicalHashHex(entries)
	if err != nil {
		return false
	}
	proof := session.entry.fixed
	proof.JournalCount, proof.JournalHash, proof.JournalRoot = uint64(len(entries)), entries[len(entries)-1].EntryHash, journalRoot
	proof.ActionRoot, proof.ReceiptRoot, proof.SourceRoot = actionRoot, receiptRoot, sourceRoot
	proof.Actions, proof.Sources = actions, sources
	if session.beforeJournalRecheck != nil {
		session.beforeJournalRecheck()
	}
	// Block appenders across the final comparison and durable rename. An append
	// that completed earlier rejects this construction; one that starts later
	// is an ordinary suffix for the next invocation.
	journal := runtime.executor.journal
	journal.mu.Lock()
	defer journal.mu.Unlock()
	after := append([]JournalEntry(nil), journal.entries...)
	if ctx.Err() != nil || len(after) != len(entries) || after[len(after)-1].EntryHash != proof.JournalHash {
		return false
	}
	afterRoot, err := canonicalHashHex(after)
	if err != nil || afterRoot != proof.JournalRoot || !session.entry.save(ctx, proof) {
		return false
	}
	session.hit, session.journalPrefixCount, session.dirty = true, proof.JournalCount, false
	session.actionKVs = map[string]evidenceRelayStartupAction{}
	for _, action := range actions {
		session.actionKVs[action.ActionId] = action
	}
	session.actionDraftKVs = map[string]evidenceRelayStartupActionDraft{}
	return true
}
