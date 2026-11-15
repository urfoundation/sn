// Capture the flag splits the providers' share three ways from its start epoch
// E_c: the provider pools, each operator's CTF pool and the top-level miners
// (CAPTURETHEFLAG.md §2). A signed settlement amendment entry v2 fixes E_c and
// the shares, not a successor policy, so every payout consent stays in its
// domain (§2.4). The validator never probes a CTF pool: its one decision is the
// `ctf` setting, and a CTF pool takes its operator's pool weight (§2.2, §2.3).
//
// A decision carries the entries it applied, its setting and the CTF pools it
// resolved, so replay reaches the same row from the artifact, the chain and the
// decision's own configuration, which must hold that setting and any unsigned
// line it applied. An artifact without them was steered by its signed policy's
// theta and keeps the bytes it always had.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/urfoundation/sn/v2026/amendment"
)

// The `ctf` setting's values. An absent setting is support, the value the
// sample configuration names, so a signed configuration need not change to
// support capture the flag.
const (
	ReleaseCtfSupport = "support"
	ReleaseCtfOff     = "off"
)

// validateReleaseCtfSetting admits the two values and absence.
func validateReleaseCtfSetting(value string) error {
	switch value {
	case "", ReleaseCtfSupport, ReleaseCtfOff:
		return nil
	}
	return fmt.Errorf("ctf must be %q or %q", ReleaseCtfSupport, ReleaseCtfOff)
}

// releaseCtfSupported is whether cfg weights the CTF pools. Off treats the CTF
// channel as empty (CAPTURETHEFLAG.md §2.1, rule 3).
func releaseCtfSupported(cfg *ReleaseConfig) bool {
	return cfg.Ctf != ReleaseCtfOff
}

// releaseAmendmentFallback is cfg's configured amendment lines, which stand in
// only for a kind with no signed entry in force (WHITEPAPER.md §8.6.7).
func releaseAmendmentFallback(cfg *ReleaseConfig) (*amendment.Fallback, error) {
	if len(cfg.AmendmentFallback) == 0 {
		return nil, nil
	}
	fallback, err := amendment.ParseFallback(cfg.ChainID, cfg.Netuid, cfg.AmendmentFallback)
	if err != nil {
		return nil, fmt.Errorf("amendment_fallback: %w", err)
	}
	return fallback, nil
}

// ReleaseAmendmentEntry is one amendment entry as a measurement artifact carries
// it: the exact line and the pinned signer's signature, or no signature for a
// line the validator's configuration stood in with.
type ReleaseAmendmentEntry struct {
	Line      string `json:"line"`
	Signature string `json:"signature,omitempty"`
}

// ReleaseCaptureTheFlag is capture the flag as a decision applied it. Entries
// are the amendment entries v2 in force for the decision epoch, one for each
// active operator with one, by no_id. Support is the validator's `ctf` setting.
// Pools are the CTF pools it resolved, by no_id: with support, every operator
// with an entry whose CTF pool resolves (settlement_v2_resolver.go); without
// it, none.
type ReleaseCaptureTheFlag struct {
	Entries []ReleaseAmendmentEntry  `json:"entries"`
	Support bool                     `json:"support"`
	Pools   []ReleasePoolMeasurement `json:"pools"`
}

// Each deployment's pinned amendment signer and the file's published address.
// Tests point them at their own key and server once, from an init, before any
// test runs.
var (
	releaseAmendmentSigner = amendment.SignerFor
	releaseAmendmentURL    = amendment.URL
)

// newReleaseAmendmentEntry is entry's wire form.
func newReleaseAmendmentEntry(entry *amendment.Entry) (ReleaseAmendmentEntry, error) {
	line, err := entry.Line()
	if err != nil {
		return ReleaseAmendmentEntry{}, err
	}
	wire := ReleaseAmendmentEntry{Line: line}
	if !entry.Configured() {
		wire.Signature = "0x" + hex.EncodeToString(entry.Signature)
	}
	return wire, nil
}

// verifiedReleaseAmendmentEntry authenticates a carried capture the flag entry:
// a version 2 line for this deployment, in force at epoch, and signed by the
// deployment's pinned signer. An entry without a signature is the validator's
// own configured line, which replay finds in the decision's configuration
// (verifyReleaseCtfConfiguration); a signature on a deployment with no pinned
// signer is refused.
func verifiedReleaseAmendmentEntry(wire ReleaseAmendmentEntry, chainID uint64, netuid uint16, epoch uint64) (*amendment.Entry, error) {
	entry, err := amendment.ParseLine(wire.Line)
	if err != nil {
		return nil, err
	}
	if entry.Kind() != amendment.KindCaptureTheFlag || entry.Ctf == nil {
		return nil, fmt.Errorf("capture the flag carries %s, not a version 2 entry", entry)
	}
	if entry.ChainID != chainID || entry.Netuid != netuid {
		return nil, fmt.Errorf("capture the flag %s is for chain %d sn %d, not chain %d sn %d", entry, entry.ChainID, entry.Netuid, chainID, netuid)
	}
	if entry.FromEpoch > epoch {
		return nil, fmt.Errorf("capture the flag %s is not in force at epoch %d", entry, epoch)
	}
	if wire.Signature == "" {
		return entry, nil
	}
	signature, err := hex.DecodeString(wire.Signature[min(2, len(wire.Signature)):])
	if err != nil || "0x"+hex.EncodeToString(signature) != wire.Signature {
		return nil, errors.New("capture the flag entry signature is not canonical hex")
	}
	signer, pinned := releaseAmendmentSigner(chainID, netuid)
	if !pinned {
		return nil, errors.New("signed capture the flag entry names a deployment with no pinned signer")
	}
	entry.Signature = signature
	if err := entry.Verify(chainID, netuid, signer); err != nil {
		return nil, err
	}
	return entry, nil
}

// releaseCtfDecision is an artifact's capture the flag, authenticated.
type releaseCtfDecision struct {
	shares  amendment.Shares
	entries map[uint64]*amendment.Entry
	// pools are the CTF pools the decision weights, by operator.
	pools map[uint64]ReleasePoolMeasurement
}

// verifiedReleaseCaptureTheFlag authenticates artifact's capture the flag, nil
// when it has none. Every entry is in force for an active pool's operator, one
// per operator, by no_id; every CTF pool is for an operator with an entry, by
// no_id, at a UID no provider pool or other CTF pool holds; and an artifact
// that does not support capture the flag resolves no CTF pool. The shares are
// every entry's, which a version 2 line fixes.
func verifiedReleaseCaptureTheFlag(artifact *ReleaseMeasurementArtifact) (*releaseCtfDecision, error) {
	ctf := artifact.CaptureTheFlag
	if ctf == nil {
		return nil, nil
	}
	if len(ctf.Entries) == 0 || ctf.Pools == nil || !ctf.Support && len(ctf.Pools) != 0 {
		return nil, errors.New("capture the flag names no entry, or resolves CTF pools it does not support")
	}
	active := make(map[uint64]bool, len(artifact.Pools))
	uids := make(map[uint16]bool, len(artifact.Pools)+len(ctf.Pools))
	for _, pool := range artifact.Pools {
		active[pool.NoID] = true
		uids[pool.UID] = true
	}
	decision := &releaseCtfDecision{entries: make(map[uint64]*amendment.Entry, len(ctf.Entries)), pools: make(map[uint64]ReleasePoolMeasurement, len(ctf.Pools))}
	for index, wire := range ctf.Entries {
		entry, err := verifiedReleaseAmendmentEntry(wire, artifact.ChainID, artifact.Netuid, artifact.SettlementEpoch)
		if err != nil {
			return nil, err
		}
		if !active[entry.NoID] || index > 0 && entry.NoID <= ctf.Entries[index-1].noID() {
			return nil, errors.New("capture the flag entries are not one per active operator in no_id order")
		}
		if index == 0 {
			decision.shares = entry.Ctf.Shares
		} else if entry.Ctf.Shares != decision.shares {
			return nil, errors.New("capture the flag entries split the providers' share differently")
		}
		decision.entries[entry.NoID] = entry
	}
	for index, pool := range ctf.Pools {
		if decision.entries[pool.NoID] == nil || index > 0 && pool.NoID <= ctf.Pools[index-1].NoID || uids[pool.UID] {
			return nil, errors.New("capture the flag pools are not one per operator with an entry, in no_id order, at a UID of their own")
		}
		if _, err := parseReleaseHex32("CTF pool hotkey", pool.PoolHotkey, false); err != nil {
			return nil, err
		}
		uids[pool.UID] = true
		decision.pools[pool.NoID] = pool
	}
	return decision, nil
}

// noID is the operator a carried line names, zero for a line that does not
// parse; verifiedReleaseAmendmentEntry has already parsed every earlier one.
func (self ReleaseAmendmentEntry) noID() uint64 {
	entry, err := amendment.ParseLine(self.Line)
	if err != nil {
		return 0
	}
	return entry.NoID
}

// ctfInputs is the CTF channel: each weighted CTF pool takes its operator's
// pool score, so the channel divides by the validator's own pool weights
// (CAPTURETHEFLAG.md §2.2). A controlled operator's mask covers both of its
// UIDs, and a pool the audit left without weight gives its CTF pool none.
func (self *releaseCtfDecision) ctfInputs(pools []VerifiedReleasePool, masked map[uint16]bool) []ExactWeightInput {
	if self == nil {
		return nil
	}
	inputs := []ExactWeightInput{}
	for _, pool := range pools {
		ctf, found := self.pools[pool.NoID]
		if !found {
			continue
		}
		if pool.Controlled {
			masked[ctf.UID] = true
			continue
		}
		if pool.Eligible && pool.Score != nil && pool.Score.Sign() > 0 {
			inputs = append(inputs, ExactWeightInput{UID: ctf.UID, Score: new(big.Rat).Set(pool.Score)})
		}
	}
	return inputs
}

// releaseCtfPoolsExpected is the CTF pools a decision with ctf must carry, given
// the CTF pools resolved at its snapshot for every active operator: with
// support, those of the operators with an entry; otherwise none.
func releaseCtfPoolsExpected(ctf *ReleaseCaptureTheFlag, resolved []ReleasePoolMeasurement) []ReleasePoolMeasurement {
	if ctf == nil || !ctf.Support {
		return []ReleasePoolMeasurement{}
	}
	entries := make(map[uint64]bool, len(ctf.Entries))
	for _, entry := range ctf.Entries {
		entries[entry.noID()] = true
	}
	pools := []ReleasePoolMeasurement{}
	for _, pool := range resolved {
		if entries[pool.NoID] {
			pools = append(pools, pool)
		}
	}
	return pools
}

// releaseCtfResolves is whether replaying artifact must resolve CTF pools: only
// a decision that weights capture the flag has any to compare.
func releaseCtfResolves(artifact *ReleaseMeasurementArtifact) bool {
	return artifact.CaptureTheFlag != nil && artifact.CaptureTheFlag.Support
}

// verifyReleaseCtfPools requires artifact's CTF pools to be exactly those of
// resolved, the CTF pools independently resolved at its snapshot, that its
// entries and setting select. An artifact without capture the flag carries
// none, whatever is resolved.
func verifyReleaseCtfPools(artifact *ReleaseMeasurementArtifact, resolved []ReleasePoolMeasurement) error {
	if artifact.CaptureTheFlag == nil {
		return nil
	}
	if !slices.Equal(artifact.CaptureTheFlag.Pools, releaseCtfPoolsExpected(artifact.CaptureTheFlag, resolved)) {
		return errors.New("capture the flag pools differ from the CTF pools resolved at the decision's snapshot")
	}
	return nil
}

// verifyReleaseCtfConfiguration requires artifact's capture the flag to be its
// configuration's: the ctf setting support, and every unsigned entry one of the
// configured amendment lines fallback, so no decision stands on a line nobody
// signed or configured.
func verifyReleaseCtfConfiguration(artifact *ReleaseMeasurementArtifact, support bool, fallback []string) error {
	ctf := artifact.CaptureTheFlag
	if ctf == nil {
		return nil
	}
	if ctf.Support != support {
		return errors.New("capture the flag declares another ctf setting than its configuration's")
	}
	for _, entry := range ctf.Entries {
		if entry.Signature == "" && !slices.Contains(fallback, entry.Line) {
			return errors.New("capture the flag stands on an unsigned line its configuration does not name")
		}
	}
	return nil
}

// newReleaseCaptureTheFlag is a decision's capture the flag: entries in force,
// by operator, with the CTF pools resolved for their operators when cfg
// supports capture the flag. No entry in force is no capture the flag.
func newReleaseCaptureTheFlag(cfg *ReleaseConfig, entries []*amendment.Entry, resolved []ReleasePoolMeasurement) (*ReleaseCaptureTheFlag, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	ctf := &ReleaseCaptureTheFlag{Entries: make([]ReleaseAmendmentEntry, 0, len(entries)), Support: releaseCtfSupported(cfg)}
	for _, entry := range entries {
		wire, err := newReleaseAmendmentEntry(entry)
		if err != nil {
			return nil, err
		}
		ctf.Entries = append(ctf.Entries, wire)
	}
	ctf.Pools = releaseCtfPoolsExpected(ctf, resolved)
	return ctf, nil
}

const (
	// A steering pass reads the published amendments at most this often, and for
	// no longer than the timeout, as it reads the price sheet.
	releaseAmendmentCacheTTL       = 5 * time.Minute
	releaseAmendmentRefreshTimeout = 10 * time.Second
)

// releaseAmendmentStore keeps the last verified amendments file in the
// validator's state directory. A file read back is verified again before use.
type releaseAmendmentStore struct {
	path string
}

func (self *releaseAmendmentStore) Load(ctx context.Context) ([]byte, error) {
	raw, err := os.ReadFile(self.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return raw, ctx.Err()
}

func (self *releaseAmendmentStore) Save(ctx context.Context, raw []byte) error {
	if existing, err := os.ReadFile(self.path); err == nil && bytes.Equal(existing, raw) {
		return ctx.Err()
	}
	return atomicStateWrite(self.path, raw, 0o600)
}

// releaseAmendments holds one deployment's settlement amendments for steering.
// A deployment with a pinned signer reads the published file; one without has
// no published amendments, and only its configured lines can put one in force.
type releaseAmendments struct {
	source   *amendment.Source
	fallback *amendment.Fallback
	now      func() time.Time
	timeout  time.Duration

	// One refresh at a time. It is never held while the file is read, so a slow
	// fetch does not hold up captureTheFlagAt.
	refreshing sync.Mutex
	lock       sync.Mutex
	file       *amendment.File
	refreshed  time.Time
}

// newReleaseAmendments reads cfg's deployment's amendments with its configured
// fallback lines.
func newReleaseAmendments(cfg *ReleaseConfig) (*releaseAmendments, error) {
	fallback, err := releaseAmendmentFallback(cfg)
	if err != nil {
		return nil, err
	}
	self := &releaseAmendments{fallback: fallback, now: time.Now, timeout: releaseAmendmentRefreshTimeout}
	signer, pinned := releaseAmendmentSigner(cfg.ChainID, cfg.Netuid)
	if !pinned {
		self.file, err = amendment.Unpublished(cfg.ChainID, cfg.Netuid)
		return self, err
	}
	store := &releaseAmendmentStore{path: filepath.Join(cfg.StateDir, "amendments", "amendments.yml")}
	self.source = amendment.NewSource(releaseAmendmentURL, cfg.ChainID, cfg.Netuid, signer, store)
	return self, nil
}

// refresh reads the published file, at most once per cache interval. A failure
// is logged and never fails steering: the last verified file stays in force,
// and with none ever verified the amendments in force are unknown.
func (self *releaseAmendments) refresh(ctx context.Context) {
	if self == nil || self.source == nil {
		return
	}
	self.refreshing.Lock()
	defer self.refreshing.Unlock()
	now := self.now()
	self.lock.Lock()
	due := self.refreshed.IsZero() || now.Sub(self.refreshed) >= releaseAmendmentCacheTTL
	self.lock.Unlock()
	if !due {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, self.timeout)
	defer cancel()
	// The source keeps the newest verified file itself and refuses one that drops
	// or changes an entry, so a failed fetch returns the file held before it.
	file, problem := self.source.Refresh(bounded)
	self.lock.Lock()
	if file != nil {
		self.file = file
	}
	if ctx.Err() == nil {
		self.refreshed = now
	}
	kept := self.file != nil
	self.lock.Unlock()
	if problem != nil {
		fallback := "capture the flag is not applied, and theta splits the providers' share"
		if kept {
			fallback = "the last verified file stays in force"
		}
		fmt.Printf("release steer: settlement amendments: %v; %s\n", problem, fallback)
	}
}

// captureTheFlagAt is the capture the flag entries in force for epoch for the
// operators noIDs, in their order, from the file the last refresh kept. Without
// a verified file what is in force is unknown, and the answer is no entry: the
// pass steers by its signed policy, as it does with no verified price sheet, and
// configuration does not stand in for the file.
func (self *releaseAmendments) captureTheFlagAt(noIDs []uint64, epoch uint64) ([]*amendment.Entry, error) {
	if self == nil {
		return nil, nil
	}
	self.lock.Lock()
	file := self.file
	self.lock.Unlock()
	var entries []*amendment.Entry
	for _, noID := range noIDs {
		in, err := file.At(noID, epoch, self.fallback)
		if errors.Is(err, amendment.ErrUnknown) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if in.CaptureTheFlag != nil {
			entries = append(entries, in.CaptureTheFlag)
		}
	}
	return entries, nil
}
