// Recovery binds an exact journal byte range, not a raw evidence-file envelope.
// Its local journal reader streams that finite range with the normal record cap.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
)

type scenarioCampaignJournalCut struct {
	Bytes  uint64
	SHA256 string
}

type scenarioCampaignJournalHasher struct {
	hash hash.Hash
	size uint64
	last byte
}

func (self *scenarioCampaignJournalHasher) Write(raw []byte) (int, error) {
	n, err := self.hash.Write(raw)
	self.size += uint64(n)
	if n != 0 {
		self.last = raw[n-1]
	}
	return n, err
}

func (self *scenarioCampaignJournalHasher) cut() scenarioCampaignJournalCut {
	return scenarioCampaignJournalCut{Bytes: self.size, SHA256: fmt.Sprintf("sha256:%x", self.hash.Sum(nil))}
}

// A descriptor snapshot fixes all work before reading. The signed prefix must
// end on a complete record; every later record is still authenticated, but never
// supplies predecessor authority. No raw-file or public artifact cap is raised.
func readScenarioCampaignJournalSnapshot(stateDir string, expected *scenarioCampaignJournalCut, visit func(JournalEntry)) (scenarioCampaignJournalCut, scenarioCampaignJournalCut, error) {
	cut, snapshot, _, err := readScenarioCampaignJournalCheckpoint(stateDir, expected, visit, nil, false, nil)
	return cut, snapshot, err
}

// A retained state skips only record semantics already proved for exact bytes.
// Both the old prefix and the fixed current snapshot are still hashed on use.
func readScenarioCampaignJournalCheckpoint(stateDir string, expected *scenarioCampaignJournalCut, visit func(JournalEntry), retained *scenarioCampaignJournalCheckpoint, retain bool, validated func()) (cut, snapshot scenarioCampaignJournalCut, next *scenarioCampaignJournalCheckpoint, resultErr error) {
	file, err := openFinalCollectedFile(stateDir, "journal.jsonl")
	if err != nil {
		return cut, snapshot, nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			cut, snapshot, next = scenarioCampaignJournalCut{}, scenarioCampaignJournalCut{}, nil
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return cut, snapshot, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return cut, snapshot, nil, errors.New("campaign recovery journal is not a nonempty regular file")
	}
	bound := uint64(info.Size())
	if expected != nil {
		if expected.Bytes == 0 || !validSHA256String(expected.SHA256) {
			return cut, snapshot, nil, errors.New("campaign recovery journal signed prefix is empty or malformed")
		}
		if expected.Bytes > bound {
			return cut, snapshot, nil, errors.New("campaign recovery journal is shorter than its signed prefix")
		}
		bound = expected.Bytes
	}
	prefixHash := &scenarioCampaignJournalHasher{hash: sha256.New()}
	allHash := &scenarioCampaignJournalHasher{hash: sha256.New()}
	journal := &Journal{validationKVs: map[journalActionKey][]JournalEntry{}}
	var count uint64
	var reused uint64
	if retained != nil {
		if retained.journal == nil || retained.journal.validationKVs == nil || retained.cut.Bytes == 0 || retained.count == 0 || !validSHA256String(retained.cut.SHA256) {
			return cut, snapshot, nil, errors.New("campaign recovery journal checkpoint is incomplete")
		}
		if retained.info == nil || !os.SameFile(retained.info, info) || retained.cut.Bytes > uint64(info.Size()) {
			return cut, snapshot, nil, errors.New("campaign recovery journal retained source was replaced or truncated")
		}
		// A signed predecessor may end inside the validated snapshot. Hash
		// that cut separately without decoding its already proved suffix.
		prefixBytes := min(bound, retained.cut.Bytes)
		if _, err := io.CopyN(io.MultiWriter(prefixHash, allHash), file, int64(prefixBytes)); err != nil {
			return cut, snapshot, nil, fmt.Errorf("read campaign recovery retained prefix: %w", err)
		}
		if _, err := io.CopyN(allHash, file, int64(retained.cut.Bytes-prefixBytes)); err != nil {
			return cut, snapshot, nil, fmt.Errorf("read campaign recovery retained snapshot: %w", err)
		}
		if allHash.cut() != retained.cut {
			return cut, snapshot, nil, errors.New("campaign recovery journal retained prefix hash mismatch")
		}
		journal = cloneScenarioCampaignJournalWitnesses(retained.journal)
		count, reused = retained.count, retained.cut.Bytes
		if visit != nil {
			// Visitors receive complete detached records from this file, not
			// the compact validation witnesses stored in the checkpoint.
			visited := &scenarioCampaignJournalHasher{hash: sha256.New()}
			if err := visitScenarioCampaignJournalPrefix(io.TeeReader(io.NewSectionReader(file, 0, int64(prefixBytes)), visited), visit); err != nil {
				return cut, snapshot, nil, err
			}
			if visited.cut() != prefixHash.cut() {
				return cut, snapshot, nil, errors.New("campaign recovery journal retained visitor source changed while reading")
			}
		}
	}
	read := func(reader io.Reader, prefix bool) error {
		scan := bufio.NewScanner(reader)
		scan.Buffer(make([]byte, 64*1024), maximumJournalRecordBytes)
		for scan.Scan() {
			count++
			if validated != nil {
				validated()
			}
			var entry JournalEntry
			if err := json.Unmarshal(scan.Bytes(), &entry); err != nil {
				return fmt.Errorf("journal line %d: %w", count, err)
			}
			want := entry.EntryHash
			entry.EntryHash = ""
			digest, err := canonicalHashHex(entry)
			if err != nil {
				return err
			}
			if digest != want {
				return fmt.Errorf("journal line %d hash mismatch", count)
			}
			if entry.PreviousHash != journal.lastHash {
				return fmt.Errorf("journal line %d chain mismatch", count)
			}
			if err := journal.validateEntry(entry); err != nil {
				return fmt.Errorf("journal line %d: %w", count, err)
			}
			entry.EntryHash = want
			journal.lastHash = want
			// Diagnostic text participates in the hash, but has no effect on
			// action validation or predecessor provenance after that check.
			witness := entry
			witness.Error = ""
			journal.rememberValidationWitness(witness)
			if prefix && visit != nil {
				visit(entry)
			}
		}
		return scan.Err()
	}
	if bound > reused {
		if err := read(io.TeeReader(io.LimitReader(file, int64(bound-reused)), io.MultiWriter(prefixHash, allHash)), true); err != nil {
			return cut, snapshot, nil, fmt.Errorf("campaign recovery journal prefix: %w", err)
		}
	}
	if prefixHash.size != bound {
		return cut, snapshot, nil, errors.New("campaign recovery journal is shorter than its signed prefix")
	}
	if prefixHash.last != '\n' {
		return cut, snapshot, nil, errors.New("campaign recovery journal prefix does not end at a durable record boundary")
	}
	cut = prefixHash.cut()
	if expected != nil && cut != *expected {
		return cut, snapshot, nil, errors.New("campaign recovery journal signed prefix hash mismatch")
	}
	if err := read(io.TeeReader(io.LimitReader(file, info.Size()-int64(max(bound, reused))), allHash), false); err != nil {
		return cut, snapshot, nil, fmt.Errorf("campaign recovery journal suffix: %w", err)
	}
	if allHash.size != uint64(info.Size()) || allHash.last != '\n' {
		return cut, snapshot, nil, errors.New("campaign recovery journal snapshot is truncated or ends outside a durable record boundary")
	}
	// Filesystem timestamps can have coarser resolution than a same-size
	// rewrite. Rehash the fixed range through the owned descriptor so neither
	// scanner read-ahead nor unchanged metadata can hide that substitution.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return cut, snapshot, nil, err
	}
	confirmed := sha256.New()
	if _, err := io.CopyN(confirmed, file, info.Size()); err != nil {
		return cut, snapshot, nil, fmt.Errorf("confirm campaign recovery journal snapshot: %w", err)
	}
	if fmt.Sprintf("sha256:%x", confirmed.Sum(nil)) != allHash.cut().SHA256 {
		return cut, snapshot, nil, errors.New("campaign recovery journal snapshot changed while reading")
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || after.Size() < info.Size() {
		return cut, snapshot, nil, errors.Join(errors.New("campaign recovery journal was truncated while reading"), err)
	}
	if after.Size() == info.Size() && !sameFinalCollectedFileState(info, after) {
		return cut, snapshot, nil, errors.New("campaign recovery journal snapshot changed while reading")
	}
	// The original descriptor alone cannot detect replacement of the path
	// while a visitor runs. Reopen safely and require the same source inode.
	current, err := openFinalCollectedFile(stateDir, "journal.jsonl")
	if err != nil {
		return cut, snapshot, nil, fmt.Errorf("confirm campaign recovery journal source: %w", err)
	}
	currentInfo, statErr := current.Stat()
	if err := errors.Join(statErr, current.Close()); err != nil {
		return cut, snapshot, nil, err
	}
	if !os.SameFile(info, currentInfo) {
		return cut, snapshot, nil, errors.New("campaign recovery journal source was replaced while reading")
	}
	snapshot = allHash.cut()
	if retain && scenarioCampaignJournalWitnessBytes(journal) <= scenarioCampaignJournalCheckpointBytes {
		next = &scenarioCampaignJournalCheckpoint{info: info, cut: snapshot, count: count, journal: journal}
	}
	return cut, snapshot, next, nil
}

// A new recovery captures the current finite snapshot; an existing recovery
// verifies only its signed cut while authenticating the current suffix as well.
func readScenarioCampaignRecoveryJournalPrefix(attempt, prior *scenarioCampaignAttempt, visit func(JournalEntry)) (*scenarioCampaignJournalCut, error) {
	var expected *scenarioCampaignJournalCut
	if recovery := attempt.payload.Recovery; recovery != nil && recovery.PriorRunID == prior.payload.RunID {
		expected = &scenarioCampaignJournalCut{Bytes: recovery.PriorJournalBytes, SHA256: recovery.PriorJournalSha256}
	}
	cut, _, err := readScenarioCampaignJournalSnapshotMemo(attempt.cfg, attempt.stateDir, expected, visit)
	if err != nil {
		return nil, err
	}
	return &cut, nil
}
