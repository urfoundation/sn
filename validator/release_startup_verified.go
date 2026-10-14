//go:build linux || darwin

// Production startup proves the whole retained history again: every input's
// native and EVM boundary, every cut's public replay, every terminal, every
// intent's chain sources and the complete intent lineage. That work grows with
// each epoch, and one read that ran out of time used to discard all of it,
// because a failed startup stage begins again from nothing.
//
// This owner remembers, for the life of the process, the digest of each unit
// whose complete verification passed. A later attempt, and the live service,
// skip only a unit already verified with exactly the same inputs. A unit's
// digest covers every input its verdict depends on except the process
// authority (configuration, hotkey, server keys and bridge), which cannot
// change within one process. Nothing here is written, so every new process
// still verifies the complete history once.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"slices"
	"sync"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

const releaseStartupVerifiedSchema = "urnetwork-validator-startup-verified-v1"

// Units this process verified completely, and the longest intent prefix whose
// complete lineage it verified.
type releaseStartupVerified struct {
	stateLock sync.Mutex
	units     map[[32]byte]struct{}
	intents   [][32]byte
	skipped   uint64
}

type releaseStartupVerifiedKey struct{}

func newReleaseStartupVerified() *releaseStartupVerified {
	return &releaseStartupVerified{units: make(map[[32]byte]struct{})}
}

func withReleaseStartupVerified(ctx context.Context, owner *releaseStartupVerified) context.Context {
	if ctx == nil || owner == nil {
		return ctx
	}
	return context.WithValue(ctx, releaseStartupVerifiedKey{}, owner)
}

// Absent outside a production release run. Every method accepts a nil owner
// and then verifies exactly as before.
func releaseStartupVerifiedFrom(ctx context.Context) *releaseStartupVerified {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(releaseStartupVerifiedKey{}).(*releaseStartupVerified)
	return owner
}

// One complete verification's identity: its kind and the canonical JSON of
// every input that decides its verdict. A value that cannot be encoded is
// never remembered.
func releaseStartupVerifiedUnit(kind string, inputs any) ([32]byte, bool) {
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return [32]byte{}, false
	}
	digest := sha256.New()
	digest.Write([]byte(releaseStartupVerifiedSchema))
	digest.Write([]byte{0})
	digest.Write([]byte(kind))
	digest.Write([]byte{0})
	digest.Write(encoded)
	return [32]byte(digest.Sum(nil)), true
}

func (self *releaseStartupVerified) has(unit [32]byte) bool {
	if self == nil {
		return false
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	_, found := self.units[unit]
	if found {
		self.skipped++
	}
	return found
}

func (self *releaseStartupVerified) add(unit [32]byte) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.units[unit] = struct{}{}
}

// Runs verify unless an identical unit already passed. Only a complete
// success is remembered; verify reports whether its success may be.
func (self *releaseStartupVerified) onceIf(kind string, inputs any, verify func() (bool, error)) error {
	if self == nil {
		_, err := verify()
		return err
	}
	unit, ok := releaseStartupVerifiedUnit(kind, inputs)
	if ok && self.has(unit) {
		return nil
	}
	remember, err := verify()
	if err == nil && ok && remember {
		self.add(unit)
	}
	return err
}

func (self *releaseStartupVerified) once(kind string, inputs any, verify func() error) error {
	return self.onceIf(kind, inputs, func() (bool, error) { return true, verify() })
}

// A signed cut's statistics replayed from one public origin. The scratch path
// and transport functions do not decide the verdict; the origin names the
// replica that served it, and the server keys belong to the process authority.
type releaseStartupVerifiedStatsCut struct {
	Measurement     ReleaseStatsMeasurement  `json:"measurement"`
	Cut             AttemptCutV2             `json:"cut"`
	Expected        AttemptCutV2Context      `json:"expected"`
	Policy          [32]byte                 `json:"policy"`
	Bounds          AttemptCutV2Bounds       `json:"bounds"`
	ExpectedConfig  ReleaseStatsConfig       `json:"expected_config"`
	MaxProviders    uint64                   `json:"max_providers"`
	MaxEgressHashes uint64                   `json:"max_egress_hashes"`
	Replay          AttemptCutV2ReplayBounds `json:"replay"`
	Origin          string                   `json:"origin"`
}

func releaseStartupVerifiedStatsCutInputs(measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2StatsOptions) (releaseStartupVerifiedStatsCut, bool) {
	if options.Replay.origin == "" || options.Replay.VisitRecord != nil {
		return releaseStartupVerifiedStatsCut{}, false
	}
	hash, err := policy.Hash()
	if err != nil {
		return releaseStartupVerifiedStatsCut{}, false
	}
	return releaseStartupVerifiedStatsCut{Measurement: measurement, Cut: cut, Expected: expected, Policy: hash, Bounds: bounds, ExpectedConfig: options.ExpectedConfig,
		MaxProviders: options.MaxProviders, MaxEgressHashes: options.MaxEgressHashes, Replay: options.Replay.Bounds, Origin: options.Replay.origin}, true
}

// The complete public statistics replay of one cut, skipped only when the
// same cut, statistics, context, policy, bounds and origin already passed.
func (self *releaseStartupVerified) verifyStatsCut(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2StatsOptions) error {
	verify := func() error {
		_, _, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(ctx, measurement, cut, expected, policy, bounds, options)
		return err
	}
	inputs, ok := releaseStartupVerifiedStatsCutInputs(measurement, cut, expected, policy, bounds, options)
	if !ok {
		return verify()
	}
	return self.once("stats-cut", inputs, verify)
}

// One retained input's historical native schedule and EVM boundary. The
// journal digest covers the exact retained bytes, including the cut's
// boundaries and the original policy.
type releaseStartupVerifiedInput struct {
	Journal  [32]byte                           `json:"journal"`
	NoID     uint64                             `json:"no_id"`
	Legacy   bool                               `json:"legacy"`
	Retained bool                               `json:"retained"`
	Runtime  crv4.RuntimeArtifactIdentity       `json:"runtime"`
	Initial  ReleaseEvidenceV2ActivationContext `json:"initial"`
}

// One historical EVM boundary: its canonical finality, on-chain epoch window,
// policy and the operator's eligibility, as a terminal checks them.
type releaseStartupVerifiedBoundary struct {
	Domain     protocol.ValidatorEvidenceDomain `json:"domain"`
	NoID       uint64                           `json:"no_id"`
	Boundary   AttemptBoundary                  `json:"boundary"`
	Terminal   bool                             `json:"terminal"`
	Retained   bool                             `json:"retained"`
	PolicyHash string                           `json:"policy_hash"`
}

// One terminal's complete closure replay at one origin, with every operator's
// independently reconstructed context.
type releaseStartupVerifiedTerminal struct {
	Closure            [32]byte                                 `json:"closure"`
	Operators          []releaseStartupVerifiedTerminalOperator `json:"operators"`
	MaxParticipants    uint64                                   `json:"max_participants"`
	MaxTransitionBytes uint64                                   `json:"max_transition_bytes"`
	MaxClosureBytes    uint64                                   `json:"max_closure_bytes"`
	Retained           bool                                     `json:"retained"`
}

type releaseStartupVerifiedTerminalOperator struct {
	NoID             uint64                   `json:"no_id"`
	Expected         AttemptCutV2Context      `json:"expected"`
	Policy           [32]byte                 `json:"policy"`
	Bounds           AttemptCutV2Bounds       `json:"bounds"`
	ExpectedConfig   ReleaseStatsConfig       `json:"expected_config"`
	MaxProviders     uint64                   `json:"max_providers"`
	MaxEgressHashes  uint64                   `json:"max_egress_hashes"`
	MaxFleetPrefixes uint64                   `json:"max_fleet_prefixes"`
	Replay           AttemptCutV2ReplayBounds `json:"replay"`
	Origin           string                   `json:"origin"`
}

// Current head bindings or a caller visitor make the replay's verdict depend
// on more than these inputs; such a terminal is always verified.
func releaseStartupVerifiedTerminalInputs(closure *AttemptSettlementClosureV2, options AttemptSettlementV2Options) (releaseStartupVerifiedTerminal, bool) {
	encoded, err := json.Marshal(closure)
	if err != nil || closure == nil {
		return releaseStartupVerifiedTerminal{}, false
	}
	result := releaseStartupVerifiedTerminal{Closure: sha256.Sum256(encoded), MaxParticipants: options.MaxParticipants, MaxTransitionBytes: options.MaxTransitionBytes, MaxClosureBytes: options.MaxClosureBytes, Retained: options.retainedStartup}
	noIDs := make([]uint64, 0, len(options.Operators))
	for noID := range options.Operators {
		noIDs = append(noIDs, noID)
	}
	slices.Sort(noIDs)
	for _, noID := range noIDs {
		operator := options.Operators[noID]
		measurement := operator.Measurement
		if measurement.Replay.origin == "" || measurement.Replay.VisitRecord != nil || len(measurement.CurrentBindingKVs) != 0 {
			return releaseStartupVerifiedTerminal{}, false
		}
		hash, err := operator.Policy.Hash()
		if err != nil {
			return releaseStartupVerifiedTerminal{}, false
		}
		result.Operators = append(result.Operators, releaseStartupVerifiedTerminalOperator{NoID: noID, Expected: operator.Expected, Policy: hash, Bounds: operator.Bounds,
			ExpectedConfig: measurement.ExpectedConfig, MaxProviders: measurement.MaxProviders, MaxEgressHashes: measurement.MaxEgressHashes, MaxFleetPrefixes: measurement.MaxFleetPrefixes,
			Replay: measurement.Replay.Bounds, Origin: measurement.Replay.origin})
	}
	return result, true
}

// One intent's chain sources and native source reference, as the disk stage
// authenticates them. The intent bytes pin its artifact by content hash.
type releaseStartupVerifiedIntent struct {
	Intent  json.RawMessage              `json:"intent"`
	Runtime crv4.RuntimeArtifactIdentity `json:"runtime"`
}

// An entry that cannot be encoded yields an empty intent, which is still a
// distinct unit from every encodable one.
func releaseStartupVerifiedIntentInputs(intent *SteeringIntent, runtime crv4.RuntimeArtifactIdentity) releaseStartupVerifiedIntent {
	encoded, err := json.Marshal(intent)
	if err != nil || intent == nil {
		encoded = nil
	}
	return releaseStartupVerifiedIntent{Intent: encoded, Runtime: runtime}
}

// Entries whose complete verification included this exact prefix. The rules
// are the in-process memo's: an unchanged prefix, a successor appended after
// it, or only its last entry changed. Anything else verifies everything.
func (self *releaseStartupVerified) intentPrefix(digests [][32]byte) int {
	if self == nil {
		return 0
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.intents) == 0 || len(digests) < len(self.intents) {
		return 0
	}
	matched := 0
	for matched < len(self.intents) && digests[matched] == self.intents[matched] {
		matched++
	}
	if matched == 0 || matched < len(self.intents)-1 {
		return 0
	}
	return matched
}

// Called with a complete read's digests, or with the prefix a failed read
// verified before the entry it could not, after its final custody check. A
// failed read keeps a remembered chain that already extends its prefix.
func (self *releaseStartupVerified) rememberIntents(digests [][32]byte, complete bool) {
	if self == nil || len(digests) == 0 {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if last := len(digests) - 1; !complete && last < len(self.intents) && self.intents[last] == digests[last] {
		return
	}
	self.intents = slices.Clone(digests)
}

// How many verifications this process skipped, for the startup notice.
func (self *releaseStartupVerified) skippedUnits() uint64 {
	if self == nil {
		return 0
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.skipped
}
