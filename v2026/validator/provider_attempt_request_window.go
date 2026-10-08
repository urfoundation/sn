// Closed request windows cover original sends, including requests whose first
// assignment reply was lost. The sequence fence is separate from result truth.
package validator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"

	"github.com/urfoundation/sn/v2026/protocol"
)

const ProviderAttemptRequestWindowSchema = "urnetwork-provider-attempt-request-window-v1"

// The first signature is retained inside the physical checkpoint. Reopening
// reconstructs the exact payload from its signed sequence interval, not a new
// clock observation or a re-signed approximation of the old window.
type ProviderAttemptRequestClosedHead struct {
	Schema          string                            `json:"schema"`
	Preparation     ProviderAttemptRequestPreparation `json:"preparation"`
	Window          protocol.ValidatorEvidenceWindow  `json:"window"`
	PreviousCutHash [32]byte                          `json:"previous_cut_hash"`
	Begin           ProviderAttemptRequestHead        `json:"begin"`
	End             ProviderAttemptRequestHead        `json:"end"`
	RecordsHash     [32]byte                          `json:"records_hash"`
	Signature       []byte                            `json:"signature"`
}

// Portable originals are independently replayed; their header cannot promote
// an absent Server response to zero exposure or to a successful confirmation.
type ProviderAttemptRequestWindow struct {
	Header   ProviderAttemptRequestClosedHead         `json:"header"`
	Records  []ProviderAttemptRequestRecord           `json:"records"`
	Closures []protocol.ProviderAttemptRequestClosure `json:"closures,omitempty"`
}

// After the exact physical fence is retained, derive request-local consent
// under the original fixed key. Ed25519 over these immutable bytes is identical
// on every lost-output retry; no new clock, nonce or generation is introduced.
func (self *ProviderAttemptRequestJournal) CloseRequests(ctx context.Context, cut *ProviderAttemptRequestWindow, scope protocol.ProviderAttemptReceiptScope) (resultErr error) {
	if ctx == nil || cut == nil {
		return errors.New("provider close cut or owner absent")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := self.directory.enter(ctx); err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, self.directory.leave())
		if resultErr != nil {
			cut.Closures = nil
		}
	}()
	if err := self.ready(ctx); err != nil {
		return err
	}
	if self.checkpoint.Closed == nil {
		return protocol.ErrProviderAttemptsUnavailable
	}
	hash, err := self.checkpoint.Closed.Hash()
	if err != nil {
		return err
	}
	cutHash, err := cut.Header.Hash()
	if err != nil {
		return err
	}
	if hash != cutHash || self.active[cut.Header.Window.Epoch] != 0 {
		return protocol.ErrProviderAttemptsIntegrity
	}
	payload := *cut
	payload.Closures = nil
	if err := VerifyProviderAttemptRequestWindow(ctx, payload, self.preparation, cut.Header.Begin, cut.Header.PreviousCutHash, cut.Header.Window, self.limits.MaxJournalBytes+8192); err != nil {
		return err
	}
	genesis, err := canonicalAttemptHex32("provider close genesis", self.identity.Ledger.GenesisHash, false)
	if err != nil {
		return err
	}
	if scope.GenesisHash != genesis || scope.PolicyHash != self.identity.PolicyHash || scope.NoId != self.identity.Ledger.NoID || scope.Netuid != uint64(self.identity.Ledger.Netuid) || scope.DeploymentId != self.identity.Ledger.DeploymentID {
		return protocol.ErrProviderAttemptsIntegrity
	}
	values := make([]protocol.ProviderAttemptRequestClosure, 0, len(cut.Records))
	for _, record := range cut.Records {
		value := protocol.ProviderAttemptRequestClosure{Schema: protocol.ProviderAttemptRequestCloseDomain, Scope: scope, ClientId: self.identity.ClientId, Message: record.Message, RequestSignature: record.RequestSignature, CutHash: hash, Epoch: cut.Header.Window.Epoch, EndBlock: cut.Header.Window.EndBlock}
		signed, err := protocol.SealProviderAttemptRequestClosure(ctx, value, self.key)
		if err != nil {
			return err
		}
		values = append(values, *signed)
	}
	cut.Closures = values
	return ctx.Err()
}

// Signature grammar does not depend on JSON indentation of a transport wrapper.
func (self ProviderAttemptRequestClosedHead) signingBytes() ([]byte, error) {
	self.Signature = nil
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(ProviderAttemptRequestWindowSchema+"\x00"), raw...), nil
}

// The exact first closed header is the successor's immutable predecessor.
func (self ProviderAttemptRequestClosedHead) Hash() ([32]byte, error) {
	raw, err := json.Marshal(self)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// Validate a retained header before a runtime close fence or a restored
// checkpoint can rely on it. Full payload verification remains mandatory.
func verifyProviderAttemptRequestClosedHead(ctx context.Context, header ProviderAttemptRequestClosedHead, expected ProviderAttemptRequestPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := expected.Validate(); err != nil {
		return err
	}
	w := header.Window
	if header.Schema != ProviderAttemptRequestWindowSchema || header.Preparation != expected || w.Subject != (protocol.ValidatorEvidenceSubject{}) || w.StartBlock == 0 || w.EndBlock <= w.StartBlock || w.FinalizedBlock != w.EndBlock || w.Epoch < expected.Birth.SettlementEpoch || w.EndBlock <= expected.Birth.EVMBlock || header.End.Sequence < header.Begin.Sequence || header.End.Bytes < header.Begin.Bytes || header.End.Sequence > expected.Limits.MaxRecords || header.End.Bytes > expected.Limits.MaxJournalBytes || header.RecordsHash == ([32]byte{}) || len(header.Signature) != ed25519.SignatureSize {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider closed request original authority differs"))
	}
	if header.PreviousCutHash == ([32]byte{}) && (header.Begin != (ProviderAttemptRequestHead{}) || w.Epoch != expected.Birth.SettlementEpoch) {
		return errors.Join(protocol.ErrProviderAttemptsUnavailable, errors.New("provider request window lacks original prior coverage"))
	}
	key, err := canonicalAttemptHex32("provider request cut key", expected.Identity.Ledger.ValidatorVPK, false)
	if err != nil {
		return err
	}
	raw, err := header.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(key[:]), raw, header.Signature) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider closed request owner signature differs"))
	}
	return ctx.Err()
}

// The physical prefix must actually contain both signed frontier identities.
// A signature over a different prefix does not make that prefix locally owned.
func verifyProviderAttemptClosedPrefix(closed *ProviderAttemptRequestClosedHead, head ProviderAttemptRequestHead) error {
	if closed != nil && (head.Sequence == closed.Begin.Sequence && head != closed.Begin || head.Sequence == closed.End.Sequence && head != closed.End) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider closed request frontier is not the original prefix"))
	}
	return nil
}

// The actual TrailEngine borrows this lane until its last possible send joins.
// A closed epoch cannot reacquire it; unrelated future epochs remain live.
func (self *ProviderAttemptRequestJournal) beginTrail(ctx context.Context, boundary AttemptBoundary) (func(), error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if self.checkpoint == nil || self.failure != nil {
		return nil, errors.Join(protocol.ErrProviderAttemptsUnavailable, self.failure)
	}
	if self.checkpoint.Closed != nil && boundary.SettlementEpoch <= self.checkpoint.Closed.Window.Epoch {
		return nil, protocol.ErrProviderAttemptsUnavailable
	}
	if self.active == nil {
		self.active = map[uint64]uint64{}
	}
	if self.active[boundary.SettlementEpoch] == ^uint64(0) {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	self.active[boundary.SettlementEpoch]++
	var once sync.Once
	return func() {
		once.Do(func() {
			self.stateLock.Lock()
			self.active[boundary.SettlementEpoch]--
			if self.active[boundary.SettlementEpoch] == 0 {
				delete(self.active, boundary.SettlementEpoch)
			}
			self.stateLock.Unlock()
		})
	}, nil
}

// Seal only after the actual transport borrowers drain. Logical bounds are
// provided by the containing immutable publication profile, not host free RAM.
func (self *ProviderAttemptRequestJournal) SealWindow(ctx context.Context, window protocol.ValidatorEvidenceWindow, maxBytes uint64) (result *ProviderAttemptRequestWindow, resultErr error) {
	if ctx == nil || maxBytes == 0 {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := self.directory.enter(ctx); err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, self.directory.leave())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := self.ready(ctx); err != nil {
		return nil, err
	}
	if self.active[window.Epoch] != 0 {
		return nil, errors.Join(protocol.ErrProviderAttemptsUnavailable, errors.New("provider request window still owns active sends"))
	}
	window.FinalizedBlock = window.EndBlock
	header := ProviderAttemptRequestClosedHead{Schema: ProviderAttemptRequestWindowSchema, Preparation: self.preparation, Window: window}
	retained := self.checkpoint.Closed
	if retained != nil {
		if retained.Window.Epoch == window.Epoch {
			if retained.Window != window {
				return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request retry changed original window"))
			}
			header = *retained
			header.Signature = slices.Clone(retained.Signature)
		} else {
			if retained.Window.Epoch == ^uint64(0) || retained.Window.Epoch+1 != window.Epoch || retained.Window.EndBlock != window.StartBlock {
				return nil, protocol.ErrProviderAttemptsUnavailable
			}
			var err error
			header.PreviousCutHash, err = retained.Hash()
			if err != nil {
				return nil, err
			}
			header.Begin = retained.End
		}
	}
	result = &ProviderAttemptRequestWindow{Header: header, Records: []ProviderAttemptRequestRecord{}}
	digest := sha256.New()
	head := header.Begin
	if len(header.Signature) == 0 {
		header.End = header.Begin
	}
	if err := self.walkWindowWithLock(ctx, header, func(record ProviderAttemptRequestRecord) error {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		hash, err := record.Hash()
		if err != nil {
			return err
		}
		head = ProviderAttemptRequestHead{Sequence: record.Sequence, Hash: hash, Bytes: head.Bytes + uint64(len(raw)) + 1, LastBoundary: record.Boundary}
		if record.Sequence <= header.Begin.Sequence || len(header.Signature) > 0 && record.Sequence > header.End.Sequence || record.Boundary.SettlementEpoch > window.Epoch {
			return nil
		}
		if record.Boundary.SettlementEpoch != window.Epoch || record.Boundary.EVMBlock < window.StartBlock || record.Boundary.EVMBlock >= window.EndBlock {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request census crosses original window"))
		}
		if len(header.Signature) == 0 {
			header.End = head
		}
		if head.Bytes < header.Begin.Bytes || head.Bytes-header.Begin.Bytes > maxBytes {
			return protocol.ErrProviderAttemptsCapacity
		}
		_, _ = digest.Write(raw)
		_, _ = digest.Write([]byte{'\n'})
		result.Records = append(result.Records, record)
		return nil
	}); err != nil {
		return nil, err
	}
	var recordsHash [32]byte
	copy(recordsHash[:], digest.Sum(nil))
	if len(header.Signature) != 0 {
		if recordsHash != header.RecordsHash {
			return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request closed payload changed"))
		}
	} else {
		header.RecordsHash = recordsHash
		raw, err := header.signingBytes()
		if err != nil {
			return nil, err
		}
		header.Signature = ed25519.Sign(self.key, raw)
		result.Header = header
	}
	if err := VerifyProviderAttemptRequestWindow(ctx, *result, self.preparation, header.Begin, header.PreviousCutHash, window, maxBytes); err != nil {
		return nil, err
	}
	if retained == nil || retained.Window.Epoch != window.Epoch {
		self.checkpoint.Closed = &header
		if err := self.saveCheckpoint(false); err != nil {
			self.dirty = true
			return nil, err
		}
	}
	return result, nil
}

// Independently selected original preparation/prior frontier and exact chain
// window prevent a valid signing key from offering a convenient subset.
func VerifyProviderAttemptRequestWindow(ctx context.Context, cut ProviderAttemptRequestWindow, expected ProviderAttemptRequestPreparation, begin ProviderAttemptRequestHead, previousHash [32]byte, window protocol.ValidatorEvidenceWindow, maxBytes uint64) error {
	if ctx == nil {
		return errors.New("provider request window context absent")
	}
	window.FinalizedBlock = window.EndBlock
	if cut.Header.Begin != begin || cut.Header.PreviousCutHash != previousHash || cut.Header.Window != window {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request original window or predecessor differs"))
	}
	if err := verifyProviderAttemptRequestClosedHead(ctx, cut.Header, expected); err != nil {
		return err
	}
	if cut.Header.End.Sequence-begin.Sequence != uint64(len(cut.Records)) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request complete sequence census differs"))
	}
	digest := sha256.New()
	head := begin
	for _, record := range cut.Records {
		if err := VerifyProviderAttemptRequest(ctx, record, expected.Identity, head, expected.Limits); err != nil {
			return err
		}
		if record.Boundary.SettlementEpoch != window.Epoch || record.Boundary.EVMBlock < window.StartBlock || record.Boundary.EVMBlock >= window.EndBlock || !providerAttemptRequestAtOrAfter(record.Boundary, expected.Birth) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request original clock differs"))
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		hash, err := record.Hash()
		if err != nil {
			return err
		}
		_, _ = digest.Write(raw)
		_, _ = digest.Write([]byte{'\n'})
		head = ProviderAttemptRequestHead{Sequence: record.Sequence, Hash: hash, Bytes: head.Bytes + uint64(len(raw)) + 1, LastBoundary: record.Boundary}
	}
	if head != cut.Header.End || !bytes.Equal(digest.Sum(nil), cut.Header.RecordsHash[:]) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request original payload hash or frontier differs"))
	}
	raw, err := json.Marshal(cut)
	if err != nil {
		return err
	}
	if uint64(len(raw)) > maxBytes {
		return protocol.ErrProviderAttemptsCapacity
	}
	return ctx.Err()
}

// Open already verified the whole original prefix. Later closure reads only
// its new signed interval while retaining the same inode/metadata custody.
// No process-global cache or skipped cold verification is introduced.
func (self *ProviderAttemptRequestJournal) walkWindowWithLock(ctx context.Context, header ProviderAttemptRequestClosedHead, visit func(ProviderAttemptRequestRecord) error) error {
	if err := self.check(); err != nil {
		return err
	}
	begin, end := header.Begin, self.checkpoint.Committed
	if len(header.Signature) != 0 {
		end = header.End
	}
	if begin.Bytes > end.Bytes || end.Bytes > self.checkpoint.Committed.Bytes || begin.Sequence > end.Sequence {
		return protocol.ErrProviderAttemptsIntegrity
	}
	scanner := bufio.NewScanner(io.NewSectionReader(self.file, int64(begin.Bytes), int64(end.Bytes-begin.Bytes)))
	scanner.Buffer(make([]byte, 1024), int(self.limits.MaxRecordBytes))
	head := begin
	stopped := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if self.step != nil {
			if err := self.step("provider-request-window-record"); err != nil {
				return err
			}
		}
		raw := scanner.Bytes()
		var record ProviderAttemptRequestRecord
		if err := attemptStoreDecode(raw, &record); err != nil {
			return err
		}
		if err := VerifyProviderAttemptRequest(ctx, record, self.identity, head, self.limits); err != nil {
			return err
		}
		canonical, err := json.Marshal(record)
		if err != nil || !bytes.Equal(raw, canonical) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
		}
		if len(header.Signature) == 0 && record.Boundary.SettlementEpoch > header.Window.Epoch {
			stopped = true
			break
		}
		hash, err := record.Hash()
		if err != nil {
			return err
		}
		head = ProviderAttemptRequestHead{Sequence: record.Sequence, Hash: hash, Bytes: head.Bytes + uint64(len(raw)) + 1, LastBoundary: record.Boundary}
		if err := visit(record); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !stopped && head != end {
		return protocol.ErrProviderAttemptsIntegrity
	}
	return errors.Join(self.check(), ctx.Err())
}

// The optional retained close is copied under its original physical owner.
// This lets an unattended publisher resume the prior output before advancing.
func (self *ProviderAttemptRequestJournal) ClosedWindow(ctx context.Context) (result *ProviderAttemptRequestClosedHead, resultErr error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := self.directory.enter(ctx); err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, self.directory.leave())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := self.ready(ctx); err != nil {
		return nil, err
	}
	if self.checkpoint.Closed == nil {
		return nil, nil
	}
	copy := *self.checkpoint.Closed
	copy.Signature = bytes.Clone(copy.Signature)
	return &copy, nil
}
