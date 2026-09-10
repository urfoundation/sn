//go:build linux || darwin

// The root's startup replay and the real mutation owner must consume identical
// bounded bytes. This fence proves exact image custody only; it never supplies
// activation, current cursor, prior EMA or signed history authentication.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// Absence is an explicit original image, never an empty-file substitute.
// Owned immutable bytes cannot be changed by later transport/physical hooks.
type attemptSettlementV2StartupImage struct {
	encoded []byte
	present bool
}

// Private to one startup invocation, before workers can acquire any ledger.
// Only pinned paths participate; ordinary runtime compatibility calls have no
// startup fence and remain subject to their existing full journal verifier.
type attemptSettlementV2StartupImages struct {
	paths map[string]attemptSettlementV2StartupImage
}

// Own every participant's snapshot and the original coordinator journal before
// entering a physical owner or external replay. No callback returns a verdict.
func newAttemptSettlementV2StartupImages(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, snapshots [][]byte, present []bool, journal []byte, journalPresent bool, bounds AttemptSettlementRuntimeV2PersistenceBounds) (*attemptSettlementV2StartupImages, error) {
	if ctx == nil || len(participants) == 0 || len(snapshots) != len(participants) || len(present) != len(participants) {
		return nil, errors.New("compact startup exact-image census is incomplete")
	}
	if err := bounds.validate(); err != nil {
		return nil, err
	}
	paths := []string{coordinator}
	seen := map[uint64]bool{}
	for index, participant := range participants {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if participant.NoID == 0 || seen[participant.NoID] || uint64(len(snapshots[index])) > bounds.MaxSnapshotBytes || present[index] != (len(snapshots[index]) != 0) {
			return nil, errors.New("compact startup snapshot identity, bytes or absence differs")
		}
		seen[participant.NoID] = true
		paths = append(paths, participant.StateDir)
	}
	if err := validateAttemptSettlementV2Paths(paths); err != nil {
		return nil, err
	}
	if uint64(len(journal)) > bounds.MaxJournalBytes || journalPresent != (len(journal) != 0) {
		return nil, errors.New("compact startup journal bytes or absence differs")
	}
	owned := &attemptSettlementV2StartupImages{paths: make(map[string]attemptSettlementV2StartupImage, len(participants)+1)}
	owned.paths[attemptSettlementTransactionV2Path(coordinator)] = attemptSettlementV2StartupImage{encoded: bytes.Clone(journal), present: journalPresent}
	for index, participant := range participants {
		owned.paths[filepath.Join(participant.StateDir, "stats.json")] = attemptSettlementV2StartupImage{encoded: bytes.Clone(snapshots[index]), present: present[index]}
	}
	return owned, ctx.Err()
}

// The complete replay owns additional immutable terminal bytes too. This is
// byte admission, not permission to select an earlier terminal or generation.
func (self *attemptSettlementV2StartupImages) addClosure(ctx context.Context, coordinator string, epoch uint64, encoded []byte, limit uint64) error {
	if self == nil || self.paths == nil || ctx == nil {
		return errors.New("compact startup immutable-image owner is absent")
	}
	if err := validateAttemptSettlementV2MetadataLimit(limit); err != nil {
		return err
	}
	if err := validateAttemptSettlementV2Paths([]string{coordinator}); err != nil {
		return err
	}
	if len(encoded) == 0 || uint64(len(encoded)) > limit {
		return errors.New("compact startup immutable closure exceeds its byte bound")
	}
	path := AttemptSettlementClosureV2Path(coordinator, epoch)
	if _, duplicate := self.paths[path]; duplicate {
		return errors.New("compact startup immutable closure pin is duplicated")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.paths[path] = attemptSettlementV2StartupImage{encoded: bytes.Clone(encoded), present: true}
	return nil
}

// Called at the real bounded byte-consumer boundary, after actual descriptor
// read/Close and before canonical decoding or any recovery write. The existing
// retained physical witnesses protect subsequent pathname/inode transitions.
func (self *attemptSettlementV2StartupImages) match(path string, encoded []byte, present bool) error {
	if self == nil {
		return nil
	}
	expected, pinned := self.paths[path]
	if !pinned {
		return fmt.Errorf("compact startup consumed an unpinned metadata image: %s", path)
	}
	if present != expected.present || !bytes.Equal(encoded, expected.encoded) {
		return fmt.Errorf("compact startup metadata differs from the independently replayed exact image: %s", path)
	}
	return nil
}
