//go:build linux || darwin

package validator

// A V2 intent read re-authenticates every retained intent: its measurement
// options, signed envelope, native source, historical runtime, prepared
// weights and the lineage replay to its predecessor all read the native chain
// and the public attempt objects again. That work grows with history and
// shares one bounded steering read. Within one process, an entry whose exact
// canonical bytes, position and predecessors already passed that complete
// verification under the same invocation authority keeps its verdict; only
// new or changed entries repeat it. Nothing here is persisted, so every
// startup still verifies the complete history.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"maps"
	"slices"
)

// The entries nearest the end keep their measurement options so a changed
// current intent or an appended successor can replay lineage from them.
const releaseIntentV2VerifiedTail = 8

const releaseIntentV2VerifiedDomain = "urnetwork-validator-intent-v2-verified-prefix-v1"

// Every input of an entry's verdict that is not in its own bytes. A change to
// any of these discards the whole memo.
type releaseIntentV2VerifiedAuthority struct {
	runtime              *releaseRuntimeV2
	history              *releaseEvidenceV2StartupHistory
	retainedStartup      bool
	historyAdoption      *releaseHistoryAdoptionV2
	historyBridge        *releaseNativeGapBridgeV2
	provisionalEpochGaps bool
	adoption             *releaseHistoryAdoptionV2
	bridge               *releaseNativeGapBridgeV2
}

// Process-local memo of the longest retained prefix readV2 has completely
// verified. digests[i] chains the canonical bytes of entries 0 through i.
type releaseIntentV2VerifiedHistory struct {
	authority releaseIntentV2VerifiedAuthority
	digests   [][32]byte
	options   map[int]ReleaseMeasurementV2Options
}

// The custody identities of a reused entry's artifact and envelope when this
// process last read and hashed both in full.
type releaseIntentV2RetainedFiles struct {
	artifact attemptPrivateFileState
	envelope attemptPrivateFileState
}

func (self *releaseIntentV2Owner) verifiedAuthority() releaseIntentV2VerifiedAuthority {
	authority := releaseIntentV2VerifiedAuthority{runtime: self.runtime, provisionalEpochGaps: self.provisionalEpochGaps,
		adoption: self.historyAdoption, bridge: self.nativeGapBridge}
	if self.runtime != nil && self.runtime.history != nil {
		history := self.runtime.history
		authority.history, authority.retainedStartup = history, history.retainedStartup
		authority.historyAdoption, authority.historyBridge = history.historyAdoption, history.nativeGapBridge
	}
	return authority
}

// Each digest binds an entry's position and canonical bytes to every earlier
// entry, so a changed, reordered, inserted or removed entry changes it and all
// later digests.
func releaseIntentV2EntryDigests(all []*SteeringIntent) ([][32]byte, bool) {
	digests := make([][32]byte, len(all))
	previous := sha256.Sum256([]byte(releaseIntentV2VerifiedDomain))
	for index, intent := range all {
		if intent == nil {
			return nil, false
		}
		encoded, err := json.Marshal(intent)
		if err != nil {
			return nil, false
		}
		entry := sha256.Sum256(encoded)
		var position [8]byte
		binary.BigEndian.PutUint64(position[:], uint64(index))
		hash := sha256.New()
		hash.Write(previous[:])
		hash.Write(position[:])
		hash.Write(entry[:])
		copy(digests[index][:], hash.Sum(nil))
		previous = digests[index]
	}
	return digests, true
}

// How many leading entries keep their verdict: all memoized entries when the
// retained history only grew after them, all but the last when only the last
// memoized entry (the current intent) changed, and none otherwise. A
// truncation, a change before the last memoized entry or a different authority
// starts again from complete verification. The entry before the first one
// verified again must still have its options for the lineage replay.
func (self *releaseIntentV2VerifiedHistory) reusablePrefix(authority releaseIntentV2VerifiedAuthority, digests [][32]byte) int {
	if self == nil || self.authority != authority || len(self.digests) == 0 || len(digests) < len(self.digests) {
		return 0
	}
	matched := 0
	for matched < len(self.digests) && digests[matched] == self.digests[matched] {
		matched++
	}
	if matched < len(self.digests)-1 {
		return 0
	}
	if _, found := self.options[matched-1]; matched == 0 || !found {
		return 0
	}
	return matched
}

func (self *releaseIntentV2VerifiedHistory) optionsAt(index int) (ReleaseMeasurementV2Options, bool) {
	if self == nil {
		return ReleaseMeasurementV2Options{}, false
	}
	options, found := self.options[index]
	return options, found
}

// Called only after a complete read verified every entry it did not reuse and
// passed its final custody check.
func (self *releaseIntentV2Owner) rememberVerifiedHistory(authority releaseIntentV2VerifiedAuthority, digests [][32]byte, options map[int]ReleaseMeasurementV2Options) {
	if len(digests) == 0 {
		self.verified = nil
		return
	}
	tail := make(map[int]ReleaseMeasurementV2Options, releaseIntentV2VerifiedTail)
	for index := max(0, len(digests)-releaseIntentV2VerifiedTail); index < len(digests); index++ {
		if value, found := options[index]; found {
			tail[index] = value
		}
	}
	self.verified = &releaseIntentV2VerifiedHistory{authority: authority, digests: digests, options: tail}
}

// A successful owned application has already reverified the changed measurement.
// Keep its proof local until publication, late closes and cancellation finish.
type releaseIntentV2OwnedApplicationMemo struct {
	previous        *releaseIntentV2VerifiedHistory
	authority       releaseIntentV2VerifiedAuthority
	previousDigests [][32]byte
	digests         [][32]byte
	options         map[int]ReleaseMeasurementV2Options
}

// Missing or changed proof simply leaves the normal full-replay path in place.
func (self *releaseIntentV2Owner) captureOwnedApplication(read *releaseIntentV2Read) *releaseIntentV2OwnedApplicationMemo {
	if self == nil || read == nil || read.file == nil || read.file.Current == nil || read.file.Current.Status != "finalized" || len(read.all) == 0 || read.all[len(read.all)-1] != read.file.Current {
		return nil
	}
	previous, authority := self.verified, self.verifiedAuthority()
	digests, ok := releaseIntentV2EntryDigests(read.all)
	if !ok || previous == nil || previous.authority != authority || len(digests) != len(previous.digests) || !slices.Equal(digests, previous.digests) {
		return nil
	}
	return &releaseIntentV2OwnedApplicationMemo{previous: previous, authority: authority, previousDigests: slices.Clone(digests), options: maps.Clone(previous.options)}
}

// Only the same prefix and a newly verified applied tail form a candidate.
func (self *releaseIntentV2OwnedApplicationMemo) complete(read *releaseIntentV2Read, options ReleaseMeasurementV2Options) {
	if self == nil || read == nil || read.file == nil || read.file.Current == nil || read.file.Current.Status != "applied" || len(read.all) != len(self.previousDigests) || len(read.all) == 0 || read.all[len(read.all)-1] != read.file.Current {
		return
	}
	digests, ok := releaseIntentV2EntryDigests(read.all)
	last := len(digests) - 1
	if !ok || last < 0 || !slices.Equal(digests[:last], self.previousDigests[:last]) || digests[last] == self.previousDigests[last] {
		return
	}
	if self.options == nil {
		self.options = make(map[int]ReleaseMeasurementV2Options)
	}
	self.options[last] = options
	self.digests = digests
}

// Called while the intent owner is still held, after all physical close checks.
// Retained-file identities are not transferred to the new lifecycle digest.
func (self *releaseIntentV2Owner) rememberOwnedApplication(candidate *releaseIntentV2OwnedApplicationMemo, read *releaseIntentV2Read) {
	if self == nil || candidate == nil || len(candidate.digests) == 0 || self.verified != candidate.previous || self.verifiedAuthority() != candidate.authority || !slices.Equal(self.verified.digests, candidate.previousDigests) {
		return
	}
	digests, ok := releaseIntentV2EntryDigests(read.all)
	if !ok || !slices.Equal(digests, candidate.digests) {
		return
	}
	self.rememberVerifiedHistory(candidate.authority, digests, candidate.options)
}
