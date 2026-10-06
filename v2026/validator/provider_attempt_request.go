// Original requests are retained before a transport may send them. Returned
// assignments cannot reconstruct a lost first response, so this journal is a
// separate immutable sequence from the unchanged assignment/proof ledger.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

const (
	ProviderAttemptRequestSchema           = "urnetwork-provider-attempt-request-v1"
	ProviderAttemptRequestAttribute        = "user.urnetwork.validator.requests.v1"
	ProviderAttemptRequestJournalName      = "provider-attempt-requests.jsonl"
	ProviderAttemptRequestPendingName      = "provider-attempt-request.pending"
	ProviderAttemptRequestCheckpointSchema = "urnetwork-provider-attempt-request-custody-v1"
)

// The existing ledger identity supplies original deployment and signing lane;
// a network login cannot replace the configured client signing authority.
type ProviderAttemptRequestIdentity struct {
	Ledger      AttemptLedgerIdentity `json:"ledger"`
	Coordinator string                `json:"coordinator"`
	ClientId    connect.Id            `json:"client_id"`
	PolicyHash  [32]byte              `json:"policy_hash"`
}

// Explicit shared-owner capacity is selected with the actual durable profile.
// Nil configuration retains legacy behavior and cannot prove request coverage.
type ProviderAttemptRequestLimits struct {
	MaxRecords      uint64 `yaml:"max_records" json:"max_records"`
	MaxRecordBytes  uint64 `yaml:"max_record_bytes" json:"max_record_bytes"`
	MaxJournalBytes uint64 `yaml:"max_journal_bytes" json:"max_journal_bytes"`
}

// Offline preparation and the independent window reader retain this same
// original birth and capacity. Runtime absence never creates a new birth.
type ProviderAttemptRequestPreparation struct {
	Identity ProviderAttemptRequestIdentity `json:"identity"`
	Limits   ProviderAttemptRequestLimits   `json:"limits"`
	Birth    AttemptBoundary                `json:"birth"`
}

// Validate the explicit owner before opening a directory or allocating bytes.
func (self ProviderAttemptRequestPreparation) Validate() error {
	if err := errors.Join(self.Identity.Validate(), self.Limits.Validate(), validateAttemptBoundary(self.Birth)); err != nil {
		return err
	}
	return forecastProviderAttemptRequestCheckpoint(self)
}

// Original escaped identity bytes must fit the complete future closed/pending
// checkpoint before birth. No future send can discover an impossible xattr.
func forecastProviderAttemptRequestCheckpoint(expected ProviderAttemptRequestPreparation) error {
	var hash [32]byte
	for index := range hash {
		hash[index] = 255
	}
	boundary := AttemptBoundary{SettlementEpoch: ^uint64(0), EVMBlock: ^uint64(0), EVMBlockHash: "0x" + strings.Repeat("f", 64)}
	head := ProviderAttemptRequestHead{Sequence: expected.Limits.MaxRecords, Hash: hash, Bytes: expected.Limits.MaxJournalBytes, LastBoundary: boundary}
	closed := &ProviderAttemptRequestClosedHead{Schema: ProviderAttemptRequestWindowSchema, Preparation: expected, Window: protocol.ValidatorEvidenceWindow{Epoch: ^uint64(0), StartBlock: ^uint64(0) - 1, EndBlock: ^uint64(0), FinalizedBlock: ^uint64(0)}, PreviousCutHash: hash, Begin: head, End: head, RecordsHash: hash, Signature: make([]byte, ed25519.SignatureSize)}
	value := ProviderAttemptRequestCheckpoint{Schema: ProviderAttemptRequestCheckpointSchema, Identity: expected.Identity, Limits: expected.Limits, Birth: expected.Birth, DirectoryInode: ^uint64(0), FileInode: ^uint64(0), Committed: head, Pending: &ProviderAttemptRequestPending{Bytes: expected.Limits.MaxRecordBytes, Hash: hash}, Closed: closed}
	_, err := encodeProviderAttemptRequestCheckpoint(value)
	return err
}

// Exact wire body and signature remain available for original Server lookup.
// The boundary is the already admitted trail owner, never wall-clock guessing.
type ProviderAttemptRequestRecord struct {
	Schema           string                         `json:"schema"`
	Identity         ProviderAttemptRequestIdentity `json:"identity"`
	Sequence         uint64                         `json:"sequence"`
	PreviousHash     [32]byte                       `json:"previous_hash"`
	Boundary         AttemptBoundary                `json:"boundary"`
	Hop              connect.Id                     `json:"hop"`
	Body             []byte                         `json:"body"`
	Message          []byte                         `json:"message"`
	RequestSignature []byte                         `json:"request_signature"`
	Signature        []byte                         `json:"signature"`
}

// A portable committed prefix is distinct from its physical local custody.
type ProviderAttemptRequestHead struct {
	Sequence     uint64          `json:"sequence"`
	Hash         [32]byte        `json:"hash"`
	Bytes        uint64          `json:"bytes"`
	LastBoundary AttemptBoundary `json:"last_boundary"`
}

// Only this bounded reference is stored in the directory xattr. The pending
// signed original has its own protected file and is never regenerated on retry.
type ProviderAttemptRequestPending struct {
	Bytes uint64   `json:"bytes"`
	Hash  [32]byte `json:"hash"`
}

// Restores may rebind physical inodes only after complete original prefix and
// pending-file admission. Birth after a window opens cannot prove that window.
type ProviderAttemptRequestCheckpoint struct {
	Schema         string                            `json:"schema"`
	Identity       ProviderAttemptRequestIdentity    `json:"identity"`
	Limits         ProviderAttemptRequestLimits      `json:"limits"`
	Birth          AttemptBoundary                   `json:"birth"`
	DirectoryInode uint64                            `json:"directory_inode"`
	FileInode      uint64                            `json:"file_inode"`
	Committed      ProviderAttemptRequestHead        `json:"committed"`
	Pending        *ProviderAttemptRequestPending    `json:"pending,omitempty"`
	Closed         *ProviderAttemptRequestClosedHead `json:"closed,omitempty"`
}

// Same-height observations must retain the original canonical block hash.
func providerAttemptRequestAtOrAfter(value, original AttemptBoundary) bool {
	return value.SettlementEpoch >= original.SettlementEpoch && value.EVMBlock >= original.EVMBlock && (value.EVMBlock != original.EVMBlock || value.EVMBlockHash == original.EVMBlockHash)
}

// Bound allocations before file/stream reads and reject accidental zero caps.
func (self ProviderAttemptRequestLimits) Validate() error {
	if self.MaxRecords == 0 || self.MaxRecords > 1000000000 || self.MaxRecordBytes < 1024 || self.MaxRecordBytes > 8192 || self.MaxJournalBytes < self.MaxRecordBytes || self.MaxJournalBytes > 1024*1024*1024*1024 {
		return protocol.ErrProviderAttemptsCapacity
	}
	return nil
}

// Identity comes from a separately admitted owner and its original client key.
func (self ProviderAttemptRequestIdentity) Validate() error {
	if err := validateAttemptLedgerIdentity(self.Ledger, nil); err != nil {
		return err
	}
	if self.ClientId == (connect.Id{}) || self.PolicyHash == ([32]byte{}) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request original identity incomplete"))
	}
	address, err := hex.DecodeString(strings.TrimPrefix(self.Coordinator, "0x"))
	if err != nil || len(address) != 20 || self.Coordinator != "0x"+hex.EncodeToString(address) || bytes.Equal(address, make([]byte, 20)) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request coordinator differs"))
	}
	return nil
}

// The original signature binds both the sequence and all transport bytes.
func (self ProviderAttemptRequestRecord) signingBytes() ([]byte, error) {
	self.Signature = nil
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(ProviderAttemptRequestSchema+"\x00"), raw...), nil
}

// Hash the exact complete original, including its first signature.
func (self ProviderAttemptRequestRecord) Hash() ([32]byte, error) {
	raw, err := json.Marshal(self)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// Validate original request grammar without issuing a transport call. Both
// seed and extend envelopes are reconstructed from the canonical signed wire.
func VerifyProviderAttemptRequest(ctx context.Context, record ProviderAttemptRequestRecord, identity ProviderAttemptRequestIdentity, prior ProviderAttemptRequestHead, limits ProviderAttemptRequestLimits) error {
	if ctx == nil {
		return errors.New("provider request context absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if err := identity.Validate(); err != nil {
		return err
	}
	if prior.Bytes > limits.MaxJournalBytes || prior.Sequence >= limits.MaxRecords {
		return protocol.ErrProviderAttemptsCapacity
	}
	if record.Schema != ProviderAttemptRequestSchema || record.Identity != identity || record.Sequence != prior.Sequence+1 || record.Sequence == 0 || record.Sequence > limits.MaxRecords || record.PreviousHash != prior.Hash || record.Hop == (connect.Id{}) || len(record.Body) == 0 || len(record.Body) > 2048 || len(record.Message) == 0 || len(record.Message) > 1024 || len(record.RequestSignature) != ed25519.SignatureSize || len(record.Signature) != ed25519.SignatureSize {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request sequence or original body differs"))
	}
	if err := validateAttemptBoundary(record.Boundary); err != nil {
		return err
	}
	// Concurrent trails retain their original pinned block; a later append in
	// the same epoch may legitimately finish an older-block trail.
	if prior.Sequence > 0 && (record.Boundary.SettlementEpoch < prior.LastBoundary.SettlementEpoch || record.Boundary.SettlementEpoch > prior.LastBoundary.SettlementEpoch && record.Boundary.EVMBlock < prior.LastBoundary.EVMBlock || record.Boundary.EVMBlock == prior.LastBoundary.EVMBlock && record.Boundary.EVMBlockHash != prior.LastBoundary.EVMBlockHash) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request original boundary went backwards"))
	}
	key, err := canonicalAttemptHex32("provider request key", identity.Ledger.ValidatorVPK, false)
	if err != nil {
		return err
	}
	prefix := []byte(connect.VerifyCtx)
	if len(record.Message) <= len(prefix) || !bytes.HasPrefix(record.Message, prefix) || !ed25519.Verify(ed25519.PublicKey(key[:]), record.Message, record.RequestSignature) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request wire signature differs"))
	}
	var canonical []byte
	switch record.Message[len(prefix)] {
	case connect.VerifyMsgTypeSeed:
		var args connect.VerifySeedArgs
		if err := attemptStoreDecode(record.Body, &args); err != nil {
			return err
		}
		if args.ClientId != identity.ClientId || args.M < 0 || args.M > 255 || !bytes.Equal(args.Vpk, key[:]) || !bytes.Equal(args.SeedSig, record.RequestSignature) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider original seed owner differs"))
		}
		canonical, err = connect.BuildVerifySeedMessage(args.Vpk, args.ClientNonce, byte(args.M))
		raw, rawErr := json.Marshal(args)
		if rawErr != nil || !bytes.Equal(raw, record.Body) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, rawErr, errors.New("provider original seed JSON differs"))
		}
	case connect.VerifyMsgTypeExtend:
		var args connect.VerifyExtendArgs
		if err := attemptStoreDecode(record.Body, &args); err != nil {
			return err
		}
		offset := len(prefix) + 1
		if args.ClientId != identity.ClientId || !bytes.Equal(args.ExtendSig, record.RequestSignature) || len(record.Message) < offset+16+32+32+2 || !bytes.Equal(record.Message[offset:offset+16], args.TrailId[:]) || !bytes.Equal(record.Message[offset+16+32:offset+16+32+32], key[:]) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider original extend owner differs"))
		}
		canonical, err = connect.BuildVerifyExtendMessage(args.TrailId, record.Message[offset+16:offset+16+32], key[:], record.Message[offset+16+32+32], args.Trail)
		raw, rawErr := json.Marshal(args)
		if rawErr != nil || !bytes.Equal(raw, record.Body) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, rawErr, errors.New("provider original extend JSON differs"))
		}
	default:
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider original request message kind differs"))
	}
	if err != nil || !bytes.Equal(canonical, record.Message) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, err, errors.New("provider original request message does not match body"))
	}
	message, err := record.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(key[:]), message, record.Signature) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request owner signature differs"))
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if uint64(len(raw))+1 > limits.MaxRecordBytes || uint64(len(raw))+1 > limits.MaxJournalBytes-prior.Bytes {
		return protocol.ErrProviderAttemptsCapacity
	}
	return ctx.Err()
}
