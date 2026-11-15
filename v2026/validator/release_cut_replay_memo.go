//go:build linux || darwin

// One steering operation replays the same two cuts about 14 times, and every
// attempt replays the retained intent's cuts twice more: the seal, the decision, the
// production stage, the envelope, intent admission, the lineage join and each
// lifecycle write each verify the artifact from nothing. A replay reads and
// authenticates every record of the settlement epoch, so that work grows with
// the epoch until it no longer fits the operation's budget. On 10-30 it did not
// fit, and each lost native epoch needed a bridge.
//
// Within one process a cut's verdict depends only on its inputs: the streams are
// content addressed by the signed cut, and the projections are pure functions of
// their records. This owner remembers, for the life of the process, the result
// of a replay that completed, under the digest of every input it depended on.
// A later replay with exactly the same inputs takes the same result without
// reading the streams again. Nothing here is written, and a new process replays.
package validator

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// The newest cuts in use are the current one, its predecessor and the prior
// settlement's terminal, for each operator. Older entries are dropped whole.
const releaseCutReplayMemoEntries = 8

// One completed replay. Record hashes answer a later lineage join's checkpoint
// without another stream read: 32 bytes per record, in sequence order.
type releaseCutReplayMemoEntry struct {
	result        VerifiedAttemptCutV2Measurement
	firstSequence uint64
	hashes        []byte
}

type releaseCutReplayMemo struct {
	stateLock sync.Mutex
	entries   map[[32]byte]*releaseCutReplayMemoEntry
	order     [][32]byte
	replays   uint64
	reused    uint64
}

type releaseCutReplayMemoKey struct{}

func newReleaseCutReplayMemo() *releaseCutReplayMemo {
	return &releaseCutReplayMemo{entries: make(map[[32]byte]*releaseCutReplayMemoEntry)}
}

func withReleaseCutReplayMemo(ctx context.Context, owner *releaseCutReplayMemo) context.Context {
	if ctx == nil || owner == nil {
		return ctx
	}
	return context.WithValue(ctx, releaseCutReplayMemoKey{}, owner)
}

// Absent outside a production release run, and then every replay reads.
func releaseCutReplayMemoFrom(ctx context.Context) *releaseCutReplayMemo {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(releaseCutReplayMemoKey{}).(*releaseCutReplayMemo)
	return owner
}

// Every input a replay's result depends on, except the scratch namespace and the
// reader functions: they choose where bytes come from, and the signed cut's
// content addresses decide which bytes those are. A reader bound to no origin
// has no identity to remember and replays.
type releaseCutReplayMemoInputs struct {
	Measurement      ReleaseStatsMeasurement         `json:"measurement"`
	Cut              AttemptCutV2                    `json:"cut"`
	Expected         AttemptCutV2Context             `json:"expected"`
	Policy           [32]byte                        `json:"policy"`
	Bounds           AttemptCutV2Bounds              `json:"bounds"`
	ExpectedConfig   ReleaseStatsConfig              `json:"expected_config"`
	Bindings         []releaseCutReplayMemoBinding   `json:"bindings"`
	MaxProviders     uint64                          `json:"max_providers"`
	MaxEgressHashes  uint64                          `json:"max_egress_hashes"`
	MaxFleetPrefixes uint64                          `json:"max_fleet_prefixes"`
	Replay           AttemptCutV2ReplayBounds        `json:"replay"`
	ServerKeys       []releaseCutReplayMemoServerKey `json:"server_keys"`
	Origin           string                          `json:"origin"`
}

type releaseCutReplayMemoBinding struct {
	ClientID connect.Id    `json:"client_id"`
	Key      FleetScoreKey `json:"key"`
}

type releaseCutReplayMemoServerKey struct {
	Version byte   `json:"version"`
	Key     string `json:"key"`
}

func releaseCutReplayMemoDigest(measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2MeasurementOptions) ([32]byte, bool) {
	if options.Replay.origin == "" || options.Replay.VisitRecord != nil {
		return [32]byte{}, false
	}
	policyHash, err := policy.Hash()
	if err != nil {
		return [32]byte{}, false
	}
	inputs := releaseCutReplayMemoInputs{Measurement: measurement, Cut: cut, Expected: expected, Policy: policyHash, Bounds: bounds, ExpectedConfig: options.ExpectedConfig,
		MaxProviders: options.MaxProviders, MaxEgressHashes: options.MaxEgressHashes, MaxFleetPrefixes: options.MaxFleetPrefixes, Replay: options.Replay.Bounds, Origin: options.Replay.origin}
	for clientID, key := range options.CurrentBindingKVs {
		inputs.Bindings = append(inputs.Bindings, releaseCutReplayMemoBinding{ClientID: clientID, Key: key})
	}
	sort.Slice(inputs.Bindings, func(i, j int) bool { return inputs.Bindings[i].ClientID.LessThan(inputs.Bindings[j].ClientID) })
	for version, key := range options.Replay.ServerKeys {
		if len(key) != ed25519.PublicKeySize {
			return [32]byte{}, false
		}
		inputs.ServerKeys = append(inputs.ServerKeys, releaseCutReplayMemoServerKey{Version: version, Key: hex.EncodeToString(key)})
	}
	sort.Slice(inputs.ServerKeys, func(i, j int) bool { return inputs.ServerKeys[i].Version < inputs.ServerKeys[j].Version })
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return [32]byte{}, false
	}
	return sha256.Sum256(encoded), true
}

// Callers own what they receive; a remembered result never shares a map.
func cloneVerifiedAttemptCutV2Measurement(value VerifiedAttemptCutV2Measurement) VerifiedAttemptCutV2Measurement {
	stats := VerifiedReleaseStats{Providers: maps.Clone(value.Stats.Providers)}
	for id, provider := range stats.Providers {
		provider.EgressIPHashes = maps.Clone(provider.EgressIPHashes)
		stats.Providers[id] = provider
	}
	var head map[FleetScoreKey]map[[32]byte]bool
	if value.HeadPrefixes != nil {
		head = make(map[FleetScoreKey]map[[32]byte]bool, len(value.HeadPrefixes))
		for key, prefixes := range value.HeadPrefixes {
			head[key] = maps.Clone(prefixes)
		}
	}
	return VerifiedAttemptCutV2Measurement{Stats: stats, HeadPrefixes: head, Replay: value.Replay}
}

func (self *releaseCutReplayMemo) lookup(digest [32]byte) *releaseCutReplayMemoEntry {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	entry := self.entries[digest]
	if entry != nil {
		self.reused++
	}
	return entry
}

func (self *releaseCutReplayMemo) remember(digest [32]byte, entry *releaseCutReplayMemoEntry) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.replays++
	if _, found := self.entries[digest]; found {
		return
	}
	for len(self.order) >= releaseCutReplayMemoEntries {
		delete(self.entries, self.order[0])
		self.order = slices.Delete(self.order, 0, 1)
	}
	self.entries[digest] = entry
	self.order = append(self.order, digest)
}

// The answer the checkpoint's own visit would reach: the record at its sequence
// has its root, or the replay never saw that sequence.
func (self *releaseCutReplayMemoEntry) answer(checkpoint *releaseMeasurementV2Checkpoint) error {
	if checkpoint == nil || checkpoint.sequence < self.firstSequence {
		return nil
	}
	offset := checkpoint.sequence - self.firstSequence
	if offset >= uint64(len(self.hashes))/32 {
		return nil
	}
	if checkpoint.seen {
		return errors.New("compact measurement rewrites its authenticated prior prefix")
	}
	root, err := canonicalAttemptHex32("compact measurement prior root", checkpoint.root, false)
	if err != nil || !slices.Equal(root[:], self.hashes[offset*32:(offset+1)*32]) {
		return errors.Join(errors.New("compact measurement rewrites its authenticated prior prefix"), err)
	}
	checkpoint.seen = true
	return nil
}

// One replay of the operation's joint verification, through the memo when this
// process has one. A failed or canceled replay is never remembered, and a
// checkpoint is answered the same way on either path.
func verifyReleaseStatsAndHeadOnce(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2MeasurementOptions, checkpoint *releaseMeasurementV2Checkpoint) (VerifiedAttemptCutV2Measurement, error) {
	var visit func(AttemptRecord) error
	if checkpoint != nil {
		visit = checkpoint.visit
	}
	memo := releaseCutReplayMemoFrom(ctx)
	if memo == nil {
		return verifyReleaseStatsAndHeadWithAttemptCutV2(ctx, measurement, cut, expected, policy, bounds, options, visit)
	}
	digest, memoizable := releaseCutReplayMemoDigest(measurement, cut, expected, policy, bounds, options)
	if !memoizable {
		return verifyReleaseStatsAndHeadWithAttemptCutV2(ctx, measurement, cut, expected, policy, bounds, options, visit)
	}
	if err := ctx.Err(); err != nil {
		return VerifiedAttemptCutV2Measurement{}, err
	}
	if entry := memo.lookup(digest); entry != nil {
		if err := entry.answer(checkpoint); err != nil {
			return VerifiedAttemptCutV2Measurement{}, err
		}
		return cloneVerifiedAttemptCutV2Measurement(entry.result), nil
	}
	entry := &releaseCutReplayMemoEntry{firstSequence: expected.FirstSequence}
	result, err := verifyReleaseStatsAndHeadWithAttemptCutV2(ctx, measurement, cut, expected, policy, bounds, options, func(record AttemptRecord) error {
		hash, err := canonicalAttemptHex32("compact measurement record hash", record.RecordHash, false)
		if err != nil {
			return err
		}
		if record.Sequence != entry.firstSequence+uint64(len(entry.hashes))/32 {
			return errors.New("compact measurement replay visited records out of sequence")
		}
		entry.hashes = append(entry.hashes, hash[:]...)
		if visit != nil {
			return visit(record)
		}
		return nil
	})
	if err != nil {
		return VerifiedAttemptCutV2Measurement{}, err
	}
	entry.result = cloneVerifiedAttemptCutV2Measurement(result)
	memo.remember(digest, entry)
	return result, nil
}
