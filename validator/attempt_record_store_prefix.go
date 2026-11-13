//go:build linux || darwin

package validator

// A production start verified every attempt record again in full: canonical
// bytes, hash, identity and signature. That work grew with the lifetime ledger
// (about 4 ms a record on snow), so every start took longer than the last.
//
// The startup verified owner now remembers each complete verification of a
// store as a prefix unit. The unit names the store's identity and key, the last
// sequence, its record hash, and a digest chained over every raw record byte
// through it. A later verification that meets a remembered prefix still reads
// every record and runs every chain, limit, lifecycle, marker and key-census
// check. The prefix's bytes must match that digest, so the only check it skips
// is the per-record hash and signature of records the digest proves unchanged
// since they passed. Records after the prefix are verified in full, and so is
// every record when no prefix matches, as without an owner.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/syndtr/goleveldb/leveldb/opt"
)

const attemptRecordStorePrefixKind = "attempt-ledger-prefix"

// Everything a remembered prefix's verdict depends on. The bounds are not in
// it, because every limit check runs again.
type attemptRecordStorePrefixUnit struct {
	Identity attemptRecordStoreIdentity `json:"identity"`
	Key      string                     `json:"key"`
	Sequence uint64                     `json:"sequence"`
	Root     string                     `json:"root"`
	Digest   string                     `json:"digest"`
}

// Each step binds the prior digest, the sequence and the record's exact stored
// bytes, so a digest names one byte-exact prefix.
func attemptRecordStorePrefixStep(prior [32]byte, sequence uint64, raw []byte) [32]byte {
	digest := sha256.New()
	digest.Write(prior[:])
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], sequence)
	digest.Write(encoded[:])
	digest.Write(raw)
	return [32]byte(digest.Sum(nil))
}

func (self *attemptRecordStore) prefixUnit(sequence uint64, root string, digest [32]byte) ([32]byte, bool) {
	return releaseStartupVerifiedUnit(attemptRecordStorePrefixKind, attemptRecordStorePrefixUnit{
		Identity: self.identity, Key: hex.EncodeToString(self.vpk), Sequence: sequence, Root: root, Digest: hex.EncodeToString(digest[:]),
	})
}

// A record inside a remembered prefix, whose exact bytes the prefix digest
// matched: every check decodeRecord makes except the hash and signature, which
// passed when the prefix was remembered.
func (self *attemptRecordStore) readRememberedRecord(sequence uint64) (AttemptRecord, error) {
	var record AttemptRecord
	raw, err := self.db.Get(attemptStoreRecordKey(sequence), &opt.ReadOptions{Strict: opt.StrictAll})
	if err != nil {
		return record, err
	}
	if uint64(len(raw)) > self.bounds.MaxRecordBytes {
		return record, fmt.Errorf("%w: stored record bytes", errAttemptRecordStoreLimit)
	}
	if err := attemptStoreDecode(raw, &record); err != nil {
		return record, err
	}
	if err := self.checkRecordShape(record); err != nil {
		return record, err
	}
	if self.disk != nil && self.disk.hooks.Step != nil {
		if err := self.disk.step("decode-record", strconv.Itoa(len(raw))); err != nil {
			return record, err
		}
	}
	if record.Sequence != sequence {
		return record, errors.New("attempt record store key and sequence differ")
	}
	return record, nil
}
