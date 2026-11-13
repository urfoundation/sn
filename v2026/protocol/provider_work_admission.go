// Independent live admission receipts retain the facts observed before session,
// reservation, stream and settlement publication. They are not signed SQL views.
package protocol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
)

const ProviderWorkReceiptSchema = "urnetwork-provider-work-admission-v1"
const MaximumProviderWorkReceiptBytes = 64 * 1024
const MaximumProviderWorkEndpointEvents = 4096
const MaximumProviderWorkCohortMembers = 64
const MaximumProviderWorkDirectoryBytes = 16 * 1024

var ErrProviderWorkUnavailable = errors.New("original provider work admission is unavailable")
var ErrProviderWorkIntegrity = errors.New("original provider work admission contradicts its authority")
var ErrProviderWorkCapacity = errors.New("original provider work admission exceeds capacity")

// The independent whole-work authority approves a global persistent admission
// source and finite replay limits. A single handler cannot declare this scope.
type ProviderWorkSourceAuthority struct {
	DomainHash          [32]byte   `json:"domain_hash"`
	SourceId            string     `json:"source_id"`
	Generation          string     `json:"generation"`
	PublicKey           [32]byte   `json:"public_key"`
	FromUnixMicro       int64      `json:"from_unix_micro"`
	ThroughUnixMicro    int64      `json:"through_unix_micro"`
	MaxEndpointEvents   uint32     `json:"max_endpoint_events"`
	MaxCohortMembers    uint32     `json:"max_cohort_members"`
	DirectoryPublicKeys [][32]byte `json:"directory_public_keys"`
}

// An original directory record binds an extender key, not its provider owner.
// Missing activation/owner-binding originals cannot be promoted to attribution.
type ProviderWorkExtender struct {
	ExtenderId        string   `json:"extender_id"`
	ClientId          string   `json:"client_id"`
	NetworkId         string   `json:"network_id"`
	PublicKey         [32]byte `json:"public_key"`
	DirectoryOriginal []byte   `json:"directory_original,omitempty"`
}

// Baseline is an original empty genesis under the live admission fence. An
// ExtenderId proves presence only; retirement removes that original connection.
type ProviderWorkSessionEvent struct {
	ClientId            string                `json:"client_id"`
	NetworkId           string                `json:"network_id"`
	ConnectionId        string                `json:"connection_id,omitempty"`
	Sequence            uint64                `json:"sequence"`
	PreviousHash        [32]byte              `json:"previous_hash"`
	Kind                string                `json:"kind"`
	ObservedAtUnixMicro int64                 `json:"observed_at_unix_micro"`
	ExtenderId          string                `json:"extender_id,omitempty"`
	Extender            *ProviderWorkExtender `json:"extender,omitempty"`
}

// A reservation commits the complete prefix actually fenced at its admission,
// including an original empty baseline when an endpoint had no connections.
type ProviderWorkEndpointHead struct {
	ClientId  string   `json:"client_id"`
	NetworkId string   `json:"network_id"`
	Sequence  uint64   `json:"sequence"`
	HeadHash  [32]byte `json:"head_hash"`
}

// Both endpoint journals are read while the same live admission fence orders
// connection changes and this exact reservation. Complete=false preserves gaps.
type ProviderWorkReservation struct {
	ContractId           string                   `json:"contract_id"`
	SourceId             string                   `json:"source_id"`
	SourceNetworkId      string                   `json:"source_network_id"`
	DestinationId        string                   `json:"destination_id"`
	DestinationNetworkId string                   `json:"destination_network_id"`
	CreatedAtUnixMicro   int64                    `json:"created_at_unix_micro"`
	Capacity             uint64                   `json:"capacity"`
	RequestFrameHash     *[32]byte                `json:"request_frame_hash,omitempty"`
	UsageOriginIsSource  *bool                    `json:"usage_origin_is_source,omitempty"`
	SourceHead           ProviderWorkEndpointHead `json:"source_head"`
	DestinationHead      ProviderWorkEndpointHead `json:"destination_head"`
	Complete             bool                     `json:"complete"`
}

// Network identity is captured at the original stream membership admission.
type ProviderWorkParticipant struct {
	ClientId  string `json:"client_id"`
	NetworkId string `json:"network_id"`
}

// The first actual stream creation retains its request and ordered membership.
// Reused and companion contracts retain that same original cohort identity.
type ProviderWorkStreamCohort struct {
	StreamId           string                    `json:"stream_id"`
	OriginContractId   string                    `json:"origin_contract_id"`
	SourceId           string                    `json:"source_id"`
	DestinationId      string                    `json:"destination_id"`
	RequestFrameHash   [32]byte                  `json:"request_frame_hash"`
	CreatedAtUnixMicro int64                     `json:"created_at_unix_micro"`
	Intermediaries     []ProviderWorkParticipant `json:"intermediaries"`
}

// The live settlement owner retains its selected outcome and original close
// time. Publisher close rows cannot independently choose another earning window.
type ProviderWorkOutcome struct {
	ContractId          string   `json:"contract_id"`
	ReservationHash     [32]byte `json:"reservation_hash"`
	StreamHash          [32]byte `json:"stream_hash"`
	ClosedAtUnixMicro   int64    `json:"closed_at_unix_micro"`
	Outcome             string   `json:"outcome"`
	SourceBytes         uint64   `json:"source_bytes"`
	DestinationBytes    uint64   `json:"destination_bytes"`
	Capacity            uint64   `json:"capacity"`
	SourceComplete      bool     `json:"source_complete"`
	DestinationComplete bool     `json:"destination_complete"`
}

// A live owner observes outcome IS NULL under the same contract fence that
// publishes the first terminal outcome. The exact boundary is never inferred
// from a later SQL label, and retries retain this first immutable observation.
type ProviderWorkOpenObservation struct {
	ContractId          string   `json:"contract_id"`
	ReservationHash     [32]byte `json:"reservation_hash"`
	Epoch               uint64   `json:"epoch"`
	Block               uint64   `json:"block"`
	BlockHash           [32]byte `json:"block_hash"`
	BoundaryUnixMicro   int64    `json:"boundary_unix_micro"`
	ObservedAtUnixMicro int64    `json:"observed_at_unix_micro"`
}

// Exactly one body belongs to an original. Signatures bind the full domain and
// persistent source generation before any public transport or SQL projection.
type ProviderWorkReceipt struct {
	Schema      string                       `json:"schema"`
	DomainHash  [32]byte                     `json:"domain_hash"`
	SourceId    string                       `json:"source_id"`
	Generation  string                       `json:"generation"`
	PublicKey   [32]byte                     `json:"public_key"`
	Session     *ProviderWorkSessionEvent    `json:"session,omitempty"`
	Reservation *ProviderWorkReservation     `json:"reservation,omitempty"`
	Stream      *ProviderWorkStreamCohort    `json:"stream,omitempty"`
	Outcome     *ProviderWorkOutcome         `json:"outcome,omitempty"`
	Open        *ProviderWorkOpenObservation `json:"open,omitempty"`
	Signature   [ed25519.SignatureSize]byte  `json:"signature"`
}

// Shared canonical UUID spelling preserves the actual server identifier bytes.
func ParseProviderWorkId(value string) ([16]byte, error) {
	var id [16]byte
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || strings.ToLower(value) != value {
		return id, ErrProviderWorkIntegrity
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != len(id) {
		return id, ErrProviderWorkIntegrity
	}
	copy(id[:], raw)
	if id == ([16]byte{}) {
		return id, ErrProviderWorkIntegrity
	}
	return id, nil
}

// Authority is always supplied by the independently admitted roster, never by
// the receipt whose original facts the caller is trying to authenticate.
func (self ProviderWorkSourceAuthority) Validate() error {
	if self.DomainHash == ([32]byte{}) || self.PublicKey == ([32]byte{}) || self.FromUnixMicro <= 0 || self.ThroughUnixMicro <= self.FromUnixMicro || self.MaxEndpointEvents == 0 || self.MaxEndpointEvents > MaximumProviderWorkEndpointEvents || self.MaxCohortMembers > MaximumProviderWorkCohortMembers || len(self.DirectoryPublicKeys) > 8 {
		return ErrProviderWorkIntegrity
	}
	for _, id := range []string{self.SourceId, self.Generation} {
		if _, err := ParseProviderWorkId(id); err != nil {
			return err
		}
	}
	for index, key := range self.DirectoryPublicKeys {
		if key == ([32]byte{}) || index > 0 && bytes.Compare(self.DirectoryPublicKeys[index-1][:], key[:]) >= 0 {
			return ErrProviderWorkIntegrity
		}
	}
	return nil
}

// Return the exact original event clock for independent authority/window joins.
func (self ProviderWorkReceipt) ObservedUnixMicro() int64 {
	switch {
	case self.Session != nil:
		return self.Session.ObservedAtUnixMicro
	case self.Reservation != nil:
		return self.Reservation.CreatedAtUnixMicro
	case self.Stream != nil:
		return self.Stream.CreatedAtUnixMicro
	case self.Outcome != nil:
		return self.Outcome.ClosedAtUnixMicro
	case self.Open != nil:
		return self.Open.ObservedAtUnixMicro
	default:
		return 0
	}
}

// Finite structural checks precede signing and decoding. Complete replay and
// identity enrollment are separate obligations of the trusted consumer.
func (self ProviderWorkReceipt) signingBytes(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		return nil, ErrProviderWorkUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if self.Schema != ProviderWorkReceiptSchema || self.DomainHash == ([32]byte{}) || self.PublicKey == ([32]byte{}) || self.ObservedUnixMicro() <= 0 {
		return nil, ErrProviderWorkIntegrity
	}
	ids := []string{self.SourceId, self.Generation}
	bodies := 0
	if event := self.Session; event != nil {
		bodies++
		ids = append(ids, event.ClientId, event.NetworkId)
		if event.Sequence == 0 || event.Sequence > MaximumProviderWorkEndpointEvents {
			return nil, ErrProviderWorkCapacity
		}
		switch event.Kind {
		case "baseline":
			if event.Sequence != 1 || event.PreviousHash != ([32]byte{}) || event.ConnectionId != "" || event.Extender != nil || event.ExtenderId != "" {
				return nil, ErrProviderWorkIntegrity
			}
		case "admit", "retire":
			ids = append(ids, event.ConnectionId)
			if event.Sequence < 2 || event.PreviousHash == ([32]byte{}) || event.Kind == "retire" && (event.Extender != nil || event.ExtenderId != "") {
				return nil, ErrProviderWorkIntegrity
			}
		default:
			return nil, ErrProviderWorkIntegrity
		}
		if event.ExtenderId != "" {
			ids = append(ids, event.ExtenderId)
		}
		if extender := event.Extender; extender != nil {
			if event.ExtenderId != "" && event.ExtenderId != extender.ExtenderId {
				return nil, ErrProviderWorkIntegrity
			}
			ids = append(ids, extender.ExtenderId, extender.ClientId, extender.NetworkId)
			if len(extender.DirectoryOriginal) > MaximumProviderWorkDirectoryBytes {
				return nil, ErrProviderWorkCapacity
			}
		}
	}
	if reservation := self.Reservation; reservation != nil {
		bodies++
		ids = append(ids, reservation.ContractId, reservation.SourceId, reservation.SourceNetworkId, reservation.DestinationId, reservation.DestinationNetworkId)
		if reservation.SourceId == reservation.DestinationId || reservation.Capacity > math.MaxInt64 || reservation.SourceHead.ClientId != reservation.SourceId || reservation.SourceHead.NetworkId != reservation.SourceNetworkId || reservation.DestinationHead.ClientId != reservation.DestinationId || reservation.DestinationHead.NetworkId != reservation.DestinationNetworkId {
			return nil, ErrProviderWorkIntegrity
		}
		if reservation.RequestFrameHash != nil && *reservation.RequestFrameHash == ([32]byte{}) {
			return nil, ErrProviderWorkIntegrity
		}
		for _, head := range []ProviderWorkEndpointHead{reservation.SourceHead, reservation.DestinationHead} {
			if head.Sequence > MaximumProviderWorkEndpointEvents || (head.Sequence == 0) != (head.HeadHash == ([32]byte{})) || reservation.Complete && head.Sequence == 0 {
				return nil, ErrProviderWorkIntegrity
			}
		}
	}
	if stream := self.Stream; stream != nil {
		bodies++
		ids = append(ids, stream.StreamId, stream.OriginContractId, stream.SourceId, stream.DestinationId)
		if stream.SourceId == stream.DestinationId || stream.RequestFrameHash == ([32]byte{}) || stream.Intermediaries == nil || len(stream.Intermediaries) > MaximumProviderWorkCohortMembers {
			return nil, ErrProviderWorkIntegrity
		}
		for _, participant := range stream.Intermediaries {
			ids = append(ids, participant.ClientId, participant.NetworkId)
		}
	}
	if outcome := self.Outcome; outcome != nil {
		bodies++
		ids = append(ids, outcome.ContractId)
		if outcome.ReservationHash == ([32]byte{}) || outcome.Capacity > math.MaxInt64 || outcome.SourceBytes > math.MaxInt64 || outcome.DestinationBytes > math.MaxInt64 {
			return nil, ErrProviderWorkIntegrity
		}
		switch outcome.Outcome {
		case "settled", "dispute_resolved_to_source", "dispute_resolved_to_destination":
		default:
			return nil, ErrProviderWorkIntegrity
		}
	}
	if open := self.Open; open != nil {
		bodies++
		ids = append(ids, open.ContractId)
		if open.ReservationHash == ([32]byte{}) || open.Block == 0 || open.BlockHash == ([32]byte{}) || open.BoundaryUnixMicro <= 0 || open.ObservedAtUnixMicro < open.BoundaryUnixMicro {
			return nil, ErrProviderWorkIntegrity
		}
	}
	if bodies != 1 {
		return nil, ErrProviderWorkIntegrity
	}
	for _, id := range ids {
		if _, err := ParseProviderWorkId(id); err != nil {
			return nil, err
		}
	}
	self.Signature = [ed25519.SignatureSize]byte{}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > MaximumProviderWorkReceiptBytes {
		return nil, errors.Join(ErrProviderWorkCapacity, err)
	}
	return raw, nil
}

// The actual live producer signs once before publishing its immutable original.
func SignProviderWorkReceipt(ctx context.Context, value ProviderWorkReceipt, key ed25519.PrivateKey) (ProviderWorkReceipt, error) {
	if len(key) != ed25519.PrivateKeySize || !bytes.Equal(ed25519.NewKeyFromSeed(key[:ed25519.SeedSize]), key) {
		return ProviderWorkReceipt{}, ErrProviderWorkUnavailable
	}
	value.Schema = ProviderWorkReceiptSchema
	copy(value.PublicKey[:], key[ed25519.SeedSize:])
	raw, err := value.signingBytes(ctx)
	if err != nil {
		return ProviderWorkReceipt{}, err
	}
	copy(value.Signature[:], ed25519.Sign(key, raw))
	return value, nil
}

// A valid signature alone cannot select the global admission source authority.
func (self ProviderWorkReceipt) Verify(ctx context.Context) error {
	raw, err := self.signingBytes(ctx)
	if err != nil {
		return err
	}
	if !ed25519.Verify(self.PublicKey[:], raw, self.Signature[:]) {
		return ErrProviderWorkIntegrity
	}
	return ctx.Err()
}

// Original canonical bytes include their source signature and exact body.
func (self ProviderWorkReceipt) Bytes(ctx context.Context) ([]byte, error) {
	if err := self.Verify(ctx); err != nil {
		return nil, err
	}
	return json.Marshal(self)
}

// Reject aliases, duplicate JSON fields and oversized originals before replay.
func DecodeProviderWorkReceipt(ctx context.Context, raw []byte) (ProviderWorkReceipt, error) {
	var value ProviderWorkReceipt
	if ctx == nil {
		return value, ErrProviderWorkUnavailable
	}
	if err := ctx.Err(); err != nil {
		return value, err
	}
	if len(raw) == 0 {
		return value, ErrProviderWorkUnavailable
	}
	if len(raw) > MaximumProviderWorkReceiptBytes {
		return value, ErrProviderWorkCapacity
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return ProviderWorkReceipt{}, errors.Join(ErrProviderWorkIntegrity, err)
	}
	canonical, err := value.Bytes(ctx)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ProviderWorkReceipt{}, errors.Join(ErrProviderWorkIntegrity, err)
	}
	return value, nil
}

// Immutable references cover the signature as well as the original fact body.
func (self ProviderWorkReceipt) ContentHash(ctx context.Context) ([32]byte, error) {
	raw, err := self.Bytes(ctx)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// Check the independently admitted source, generation and validity interval.
func VerifyProviderWorkReceiptAuthority(ctx context.Context, value ProviderWorkReceipt, expected ProviderWorkSourceAuthority) error {
	if err := errors.Join(expected.Validate(), value.Verify(ctx)); err != nil {
		return err
	}
	if value.DomainHash != expected.DomainHash || value.SourceId != expected.SourceId || value.Generation != expected.Generation || value.PublicKey != expected.PublicKey {
		return ErrProviderWorkIntegrity
	}
	if at := value.ObservedUnixMicro(); at < expected.FromUnixMicro || at >= expected.ThroughUnixMicro {
		return ErrProviderWorkUnavailable
	}
	if value.Session != nil && value.Session.Sequence > uint64(expected.MaxEndpointEvents) || value.Stream != nil && len(value.Stream.Intermediaries) > int(expected.MaxCohortMembers) {
		return ErrProviderWorkCapacity
	}
	return ctx.Err()
}
