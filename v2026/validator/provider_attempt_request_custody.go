// Portable custody admission never writes a file or xattr. Preparation and
// restore publish rebound inode fields only after this original-prefix proof.
package validator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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

// Keep the registered opaque custody attribute inside its fixed 4096 bytes.
func encodeProviderAttemptRequestCheckpoint(value ProviderAttemptRequestCheckpoint) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) > 4096 {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	return raw, nil
}
