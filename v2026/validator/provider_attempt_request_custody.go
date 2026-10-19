// Portable custody admission never writes a file or xattr. Preparation and
// restore publish rebound inode fields only after this original-prefix proof.
package validator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Private admitted state prevents a caller from substituting an unchecked
// head when generating physical restore attributes.
type VerifiedProviderAttemptRequestCustody struct {
	checkpoint ProviderAttemptRequestCheckpoint
	pending    *ProviderAttemptRequestRecord
}

// Return a detached original checkpoint, including unresolved pending bytes.
func (self *VerifiedProviderAttemptRequestCustody) Checkpoint() ProviderAttemptRequestCheckpoint {
	result := self.checkpoint
	if result.Pending != nil {
		pending := *result.Pending
		result.Pending = &pending
	}
	if result.Closed != nil {
		closed := *result.Closed
		closed.Signature = slices.Clone(closed.Signature)
		result.Closed = &closed
	}
	return result
}

// Return the exact next original if its publication was interrupted. A stale
// cleanup file for the already committed head is not a new request.
func (self *VerifiedProviderAttemptRequestCustody) PendingRecord() *ProviderAttemptRequestRecord {
	if self.pending == nil {
		return nil
	}
	result := *self.pending
	result.Body = slices.Clone(result.Body)
	result.Message = slices.Clone(result.Message)
	result.RequestSignature = slices.Clone(result.RequestSignature)
	result.Signature = slices.Clone(result.Signature)
	return &result
}

// Exact expected birth and capacity come from an independently retained
// preparation scope. An old or larger file cannot choose another allowance.
func validateProviderAttemptRequestCheckpoint(value ProviderAttemptRequestCheckpoint, expected ProviderAttemptRequestPreparation) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	if value.Schema != ProviderAttemptRequestCheckpointSchema || value.Identity != expected.Identity || value.Limits != expected.Limits || value.Birth != expected.Birth || value.Committed.Sequence > expected.Limits.MaxRecords || value.Committed.Bytes > expected.Limits.MaxJournalBytes {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request original preparation or bounds differ"))
	}
	head := value.Committed
	if head.Sequence == 0 && head != (ProviderAttemptRequestHead{}) || head.Sequence > 0 && (head.Hash == ([32]byte{}) || head.Bytes == 0 || !providerAttemptRequestAtOrAfter(head.LastBoundary, value.Birth)) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request original committed head differs"))
	}
	if value.Pending != nil && (value.Pending.Bytes == 0 || value.Pending.Bytes >= expected.Limits.MaxRecordBytes || value.Pending.Hash == ([32]byte{})) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request original pending bound differs"))
	}
	if value.Closed != nil && (value.Closed.End.Sequence > value.Committed.Sequence || value.Closed.End.Bytes > value.Committed.Bytes) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider closed request frontier exceeds original committed head"))
	}
	return nil
}

// The reader must cover the complete named journal, not an already truncated
// projection. pending is the complete named .pending file or nil when absent;
// a .tmp file is refused separately by the namespace owner, never normalized.
func VerifyProviderAttemptRequestPrefix(ctx context.Context, checkpoint ProviderAttemptRequestCheckpoint, expected ProviderAttemptRequestPreparation, journal io.Reader, pending []byte) (*VerifiedProviderAttemptRequestCustody, error) {
	if ctx == nil || journal == nil {
		return nil, errors.New("provider request prefix owner or reader absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateProviderAttemptRequestCheckpoint(checkpoint, expected); err != nil {
		return nil, err
	}
	if checkpoint.Closed != nil {
		if err := verifyProviderAttemptRequestClosedHead(ctx, *checkpoint.Closed, expected); err != nil {
			return nil, err
		}
	}
	prefix := &io.LimitedReader{R: journal, N: int64(checkpoint.Committed.Bytes)}
	scanner := bufio.NewScanner(prefix)
	scanner.Buffer(make([]byte, 1024), int(expected.Limits.MaxRecordBytes))
	var head ProviderAttemptRequestHead
	if err := verifyProviderAttemptClosedPrefix(checkpoint.Closed, head); err != nil {
		return nil, err
	}
	closedReplay := newProviderRequestClosedReplay(checkpoint.Closed)
	var last ProviderAttemptRequestRecord
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw := scanner.Bytes()
		var record ProviderAttemptRequestRecord
		if err := attemptStoreDecode(raw, &record); err != nil {
			return nil, err
		}
		if err := VerifyProviderAttemptRequest(ctx, record, expected.Identity, head, expected.Limits); err != nil {
			return nil, err
		}
		if !providerAttemptRequestAtOrAfter(record.Boundary, expected.Birth) {
			return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider original request predates original birth"))
		}
		canonical, err := json.Marshal(record)
		if err != nil || !bytes.Equal(raw, canonical) {
			return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, err, errors.New("provider request original prefix encoding differs"))
		}
		hash, err := record.Hash()
		if err != nil {
			return nil, err
		}
		head = ProviderAttemptRequestHead{Sequence: record.Sequence, Hash: hash, Bytes: head.Bytes + uint64(len(raw)) + 1, LastBoundary: record.Boundary}
		if err := verifyProviderAttemptClosedPrefix(checkpoint.Closed, head); err != nil {
			return nil, err
		}
		if err := closedReplay.record(record, raw); err != nil {
			return nil, err
		}
		last = record
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if prefix.N != 0 || head != checkpoint.Committed {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider request original prefix is incomplete or changed"))
	}
	if err := closedReplay.finish(); err != nil {
		return nil, err
	}
	tail, err := io.ReadAll(io.LimitReader(journal, int64(expected.Limits.MaxRecordBytes)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(tail)) > expected.Limits.MaxRecordBytes {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	result := &VerifiedProviderAttemptRequestCustody{checkpoint: checkpoint}
	result.checkpoint = result.Checkpoint()
	if len(pending) == 0 {
		if checkpoint.Pending != nil || len(tail) != 0 {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider request pending original is missing"))
		}
		return result, ctx.Err()
	}
	if uint64(len(pending))+1 > expected.Limits.MaxRecordBytes {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	var record ProviderAttemptRequestRecord
	if err := attemptStoreDecode(pending, &record); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(pending, canonical) {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, err, errors.New("provider request pending original encoding differs"))
	}
	hash, err := record.Hash()
	if err != nil {
		return nil, err
	}
	if checkpoint.Pending == nil && record.Sequence == head.Sequence && hash == head.Hash {
		original, err := json.Marshal(last)
		if err != nil || !bytes.Equal(original, pending) || len(tail) != 0 {
			return nil, errors.Join(durablevolume.ErrIdentity, err, errors.New("provider request committed cleanup differs"))
		}
		return result, ctx.Err()
	}
	if err := VerifyProviderAttemptRequest(ctx, record, expected.Identity, head, expected.Limits); err != nil {
		return nil, err
	}
	if !providerAttemptRequestAtOrAfter(record.Boundary, expected.Birth) {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider pending request predates original birth"))
	}
	if err := verifyProviderRequestAfterClose(checkpoint.Closed, record); err != nil {
		return nil, err
	}
	if checkpoint.Pending != nil && (checkpoint.Pending.Bytes != uint64(len(pending)) || checkpoint.Pending.Hash != sha256.Sum256(pending)) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider request original pending commitment differs"))
	}
	if !bytes.HasPrefix(append(slices.Clone(pending), '\n'), tail) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider request interrupted suffix differs"))
	}
	result.pending = &record
	return result, ctx.Err()
}

// The preparation adapter creates only an empty staged file. Core publishes
// the returned attribute with the complete owner; this helper writes nothing.
func FreshProviderAttemptRequestCheckpoint(expected ProviderAttemptRequestPreparation, directory, file os.FileInfo) ([]byte, error) {
	if err := expected.Validate(); err != nil {
		return nil, err
	}
	if !directory.IsDir() || !attemptLedgerPrivateFile(file) || file.Size() != 0 {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider request fresh physical owner differs"))
	}
	_, directoryInode, err := attemptLedgerLocalFileID(directory)
	if err != nil {
		return nil, err
	}
	_, fileInode, err := attemptLedgerLocalFileID(file)
	if err != nil {
		return nil, err
	}
	value := ProviderAttemptRequestCheckpoint{Schema: ProviderAttemptRequestCheckpointSchema, Identity: expected.Identity, Limits: expected.Limits, Birth: expected.Birth, DirectoryInode: directoryInode, FileInode: fileInode}
	return encodeProviderAttemptRequestCheckpoint(value)
}

// Rebind only physical inodes after the complete original prefix and pending
// bytes have passed the pure verifier. Logical birth/head/bounds stay exact.
func (self *VerifiedProviderAttemptRequestCustody) ReboundCheckpoint(directory, file os.FileInfo) ([]byte, error) {
	if self == nil || !directory.IsDir() || !attemptLedgerPrivateFile(file) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider request restore physical owner differs"))
	}
	value := self.Checkpoint()
	_, directoryInode, err := attemptLedgerLocalFileID(directory)
	if err != nil {
		return nil, err
	}
	_, fileInode, err := attemptLedgerLocalFileID(file)
	if err != nil {
		return nil, err
	}
	value.DirectoryInode, value.FileInode = directoryInode, fileInode
	return encodeProviderAttemptRequestCheckpoint(value)
}

// The attribute holds the same checkpoint at one fixed width. Its original
// preparation is a digest and the closed head omits its preparation copy:
// both must equal the separately retained preparation every reader supplies.
// Preparation writes the full width at birth, so a replacement never grows.
func encodeProviderAttemptRequestCheckpoint(value ProviderAttemptRequestCheckpoint) ([]byte, error) {
	preparation := ProviderAttemptRequestPreparation{Identity: value.Identity, Limits: value.Limits, Birth: value.Birth}
	if value.Schema != ProviderAttemptRequestCheckpointSchema || value.Closed != nil && (value.Closed.Schema != ProviderAttemptRequestWindowSchema || value.Closed.Preparation != preparation || len(value.Closed.Signature) != ed25519.SignatureSize) {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request checkpoint has no fixed-width encoding"))
	}
	digest, err := providerAttemptRequestPreparationDigest(preparation)
	if err != nil {
		return nil, err
	}
	raw := append(make([]byte, 0, providerAttemptRequestAttributeBytes), providerAttemptRequestAttributeSchema...)
	raw = append(raw, digest[:]...)
	raw = binary.BigEndian.AppendUint64(raw, value.DirectoryInode)
	raw = binary.BigEndian.AppendUint64(raw, value.FileInode)
	pending, closed := ProviderAttemptRequestPending{}, ProviderAttemptRequestClosedHead{Signature: make([]byte, ed25519.SignatureSize)}
	flags := [2]byte{}
	if value.Pending != nil {
		pending, flags[0] = *value.Pending, 1
	}
	if value.Closed != nil {
		closed, flags[1] = *value.Closed, 1
	}
	if raw, err = appendProviderAttemptRequestHead(raw, value.Committed); err != nil {
		return nil, err
	}
	raw = append(raw, flags[0])
	raw = binary.BigEndian.AppendUint64(raw, pending.Bytes)
	raw = append(raw, pending.Hash[:]...)
	raw = append(raw, flags[1])
	window := closed.Window
	for _, field := range []uint64{window.Epoch, window.StartBlock, window.EndBlock, window.FinalizedBlock, window.Subject.ObservationEpoch, window.Subject.NativeEpoch} {
		raw = binary.BigEndian.AppendUint64(raw, field)
	}
	raw = append(raw, closed.PreviousCutHash[:]...)
	for _, head := range []ProviderAttemptRequestHead{closed.Begin, closed.End} {
		if raw, err = appendProviderAttemptRequestHead(raw, head); err != nil {
			return nil, err
		}
	}
	raw = append(raw, closed.RecordsHash[:]...)
	return append(raw, closed.Signature...), nil
}

// An empty head has no block hash; every other head names a canonical one.
func appendProviderAttemptRequestHead(raw []byte, head ProviderAttemptRequestHead) ([]byte, error) {
	var block [32]byte
	if head.LastBoundary.EVMBlockHash != "" {
		var err error
		if block, err = canonicalAttemptHex32("provider request head block hash", head.LastBoundary.EVMBlockHash, false); err != nil {
			return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
		}
	}
	raw = binary.BigEndian.AppendUint64(raw, head.Sequence)
	raw = append(raw, head.Hash[:]...)
	raw = binary.BigEndian.AppendUint64(raw, head.Bytes)
	raw = binary.BigEndian.AppendUint64(raw, head.LastBoundary.SettlementEpoch)
	raw = binary.BigEndian.AppendUint64(raw, head.LastBoundary.EVMBlock)
	return append(raw, block[:]...), nil
}

// The digest binds the omitted identity, limits and birth to one preparation.
func providerAttemptRequestPreparationDigest(value ProviderAttemptRequestPreparation) ([32]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// Read the fixed-width attribute, or the canonical v1 JSON that earlier
// releases wrote. legacy reports v1, which the next save replaces with v2.
// Either format must name the expected preparation; callers still validate
// the decoded heads against its bounds.
func decodeProviderAttemptRequestCheckpoint(raw []byte, expected ProviderAttemptRequestPreparation) (value ProviderAttemptRequestCheckpoint, legacy bool, err error) {
	if !bytes.HasPrefix(raw, []byte(providerAttemptRequestAttributeSchema)) {
		if err := attemptStoreDecode(raw, &value); err != nil {
			return ProviderAttemptRequestCheckpoint{}, true, err
		}
		// v1 spells its preparation out where v2 has the digest, and it must
		// also have the v2 record that its next save writes.
		_, encodeErr := encodeProviderAttemptRequestCheckpoint(value)
		if encodeErr != nil || (ProviderAttemptRequestPreparation{Identity: value.Identity, Limits: value.Limits, Birth: value.Birth}) != expected {
			return ProviderAttemptRequestCheckpoint{}, true, errors.Join(durablevolume.ErrIdentity, encodeErr, errors.New("provider request v1 checkpoint differs from its original preparation or v2 record"))
		}
		return value, true, nil
	}
	if len(raw) != providerAttemptRequestAttributeBytes {
		return value, false, errors.Join(durablevolume.ErrIdentity, errors.New("provider request checkpoint has another width"))
	}
	digest, err := providerAttemptRequestPreparationDigest(expected)
	if err != nil {
		return value, false, err
	}
	body := raw[len(providerAttemptRequestAttributeSchema):]
	if !bytes.Equal(body[:len(digest)], digest[:]) {
		return value, false, errors.Join(durablevolume.ErrIdentity, errors.New("provider request checkpoint belongs to another original preparation"))
	}
	offset := len(digest)
	next := func(size int) []byte {
		offset += size
		return body[offset-size : offset]
	}
	number := func() uint64 { return binary.BigEndian.Uint64(next(8)) }
	hash := func() (result [32]byte) {
		copy(result[:], next(32))
		return result
	}
	head := func() ProviderAttemptRequestHead {
		result := ProviderAttemptRequestHead{Sequence: number(), Hash: hash(), Bytes: number(), LastBoundary: AttemptBoundary{SettlementEpoch: number(), EVMBlock: number()}}
		if block := hash(); block != ([32]byte{}) {
			result.LastBoundary.EVMBlockHash = attemptHex32(block)
		}
		return result
	}
	value = ProviderAttemptRequestCheckpoint{Schema: ProviderAttemptRequestCheckpointSchema, Identity: expected.Identity, Limits: expected.Limits, Birth: expected.Birth, DirectoryInode: number(), FileInode: number(), Committed: head()}
	pending := next(1)[0] != 0
	value.Pending = &ProviderAttemptRequestPending{Bytes: number(), Hash: hash()}
	if !pending {
		value.Pending = nil
	}
	closed := next(1)[0] != 0
	window := protocol.ValidatorEvidenceWindow{Epoch: number(), StartBlock: number(), EndBlock: number(), FinalizedBlock: number(), Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: number(), NativeEpoch: number()}}
	value.Closed = &ProviderAttemptRequestClosedHead{Schema: ProviderAttemptRequestWindowSchema, Preparation: expected, Window: window, PreviousCutHash: hash(), Begin: head(), End: head(), RecordsHash: hash(), Signature: slices.Clone(next(ed25519.SignatureSize))}
	if !closed {
		value.Closed = nil
	}
	// Flags other than 0/1 and bytes in an absent section are not canonical.
	canonical, err := encodeProviderAttemptRequestCheckpoint(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return ProviderAttemptRequestCheckpoint{}, false, errors.Join(durablevolume.ErrIdentity, err, errors.New("provider request checkpoint is not canonical"))
	}
	return value, false, nil
}

// Other packages read the directory attribute with the runtime's strict decoder.
func DecodeProviderAttemptRequestCheckpoint(raw []byte, expected ProviderAttemptRequestPreparation) (ProviderAttemptRequestCheckpoint, error) {
	value, _, err := decodeProviderAttemptRequestCheckpoint(raw, expected)
	return value, err
}
