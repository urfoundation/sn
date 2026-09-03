package validator

// The attempt ledger is the durable measurement authority for release
// validators. One record contains every server-signed assignment in a trail,
// its validator-observed outcome, and (for completed trails) the full signed
// proof. Records are validator-signed and hash chained. Statistics are applied
// only after the corresponding record is durable, so a public cut can replay
// every counter, latency bucket, and routable-prefix claim exactly.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/urnetwork/connect/v2026"
)

const (
	attemptLedgerRecordSchema = "urnetwork-validator-attempt-record-v1"
	attemptLedgerCutSchema    = "urnetwork-validator-attempt-cut-v1"
	attemptLedgerHashDomain   = "urnetwork-validator-attempt-record-hash-v1\x00"
	attemptLedgerSignDomain   = "urnetwork-validator-attempt-record-signature-v1\x00"
	attemptCutSignDomain      = "urnetwork-validator-attempt-cut-signature-v1\x00"
)

const (
	AttemptDispositionComplete       = "complete"
	AttemptDispositionPending        = "pending"
	AttemptDispositionHopFailure     = "hop_failure"
	AttemptDispositionProtocol       = "protocol_failure"
	AttemptDispositionUnknownFinal   = "unknown_final"
	AttemptDispositionValidatorError = "validator_error"
)

// AttemptLedgerIdentity binds a per-operator ledger to one release validator.
// ValidatorUID may validly be zero; the outer release envelope proves that it
// was the hotkey's UID at the pinned native block.
type AttemptLedgerIdentity struct {
	DeploymentID string `json:"deployment_id"`
	ChainID      uint64 `json:"chain_id"`
	GenesisHash  string `json:"genesis_hash"`
	Netuid       uint16 `json:"netuid"`
	ValidatorID  uint64 `json:"validator_id"`
	ValidatorUID uint16 `json:"validator_uid"`
	NoID         uint64 `json:"no_id"`
	ValidatorVPK string `json:"validator_vpk"`
}

// AttemptBoundary is the finalized EVM view pinned before the first measured
// assignment. Every later binding lookup in the trail must use this same view.
type AttemptBoundary struct {
	SettlementEpoch uint64 `json:"settlement_epoch"`
	EVMBlock        uint64 `json:"evm_block"`
	EVMBlockHash    string `json:"evm_block_hash"`
}

// AttemptBinding is the exact coordinator/native identity attributed to one
// server-assigned provider at the attempt boundary. Inactive providers retain
// their assignment statistics but cannot contribute routable-prefix evidence.
type AttemptBinding struct {
	ClientID   connect.Id `json:"client_id"`
	Active     bool       `json:"active"`
	FleetID    string     `json:"fleet_id"`
	Hotkey     string     `json:"hotkey"`
	Generation uint64     `json:"generation"`
	UIDFound   bool       `json:"uid_found"`
	UID        uint16     `json:"uid"`
}

// AttemptBoundaryResolver pins the first finalized boundary when pinned is
// nil. Subsequent calls must resolve the requested providers at exactly pinned.
// A missing or unprovable binding is returned as Active=false, not guessed.
type AttemptBoundaryResolver func(ctx context.Context, pinned *AttemptBoundary, clientIDs []connect.Id) (AttemptBoundary, []AttemptBinding, error)

// AttemptAssignment retains the reconstructable ASSIGN bytes, server
// signature, measured outcome, and binding identity for one exposed hop.
type AttemptAssignment struct {
	Trail           []connect.Id   `json:"trail"`
	NextHop         connect.Id     `json:"next_hop"`
	ServerKeyID     byte           `json:"server_key_id"`
	AssignMessage   []byte         `json:"assign_message"`
	AssignSignature []byte         `json:"assign_signature"`
	Confirmed       bool           `json:"confirmed"`
	HasLatency      bool           `json:"has_latency"`
	LatencyBucket   uint8          `json:"latency_bucket"`
	Binding         AttemptBinding `json:"binding"`
}

// AttemptRecord is one atomic trail outcome. RecordHash covers every field
// except itself and Signature; Signature covers the domain-separated hash.
type AttemptRecord struct {
	Schema       string                `json:"schema"`
	Identity     AttemptLedgerIdentity `json:"identity"`
	Sequence     uint64                `json:"sequence"`
	PreviousHash string                `json:"previous_hash"`
	Boundary     AttemptBoundary       `json:"boundary"`
	TrailID      connect.Id            `json:"trail_id"`
	ServerNonce  []byte                `json:"server_nonce"`
	VPK          []byte                `json:"vpk"`
	M            int                   `json:"M"`
	Assignments  []AttemptAssignment   `json:"assignments"`
	Disposition  string                `json:"disposition"`
	Proof        *ProofRecord          `json:"proof,omitempty"`
	RecordHash   string                `json:"record_hash"`
	Signature    []byte                `json:"signature"`
}

// AttemptLedgerCut is a signed, contiguous settlement-window replay. Records
// before EgressFirstSequence reconstruct quality counters only; records from
// that sequence onward also reconstruct the current native head window.
type AttemptLedgerCut struct {
	Schema              string                `json:"schema"`
	Identity            AttemptLedgerIdentity `json:"identity"`
	Boundary            AttemptBoundary       `json:"boundary"`
	FirstSequence       uint64                `json:"first_sequence"`
	EgressFirstSequence uint64                `json:"egress_first_sequence"`
	LastSequence        uint64                `json:"last_sequence"`
	RecordCount         uint64                `json:"record_count"`
	PriorRoot           string                `json:"prior_root"`
	Root                string                `json:"root"`
	Records             []AttemptRecord       `json:"records"`
	Signature           []byte                `json:"signature"`
}

type attemptRecordHashPayload struct {
	Schema       string                `json:"schema"`
	Identity     AttemptLedgerIdentity `json:"identity"`
	Sequence     uint64                `json:"sequence"`
	PreviousHash string                `json:"previous_hash"`
	Boundary     AttemptBoundary       `json:"boundary"`
	TrailID      connect.Id            `json:"trail_id"`
	ServerNonce  []byte                `json:"server_nonce"`
	VPK          []byte                `json:"vpk"`
	M            int                   `json:"M"`
	Assignments  []AttemptAssignment   `json:"assignments"`
	Disposition  string                `json:"disposition"`
	Proof        *ProofRecord          `json:"proof,omitempty"`
}

type attemptCutSignaturePayload struct {
	Schema              string                `json:"schema"`
	Identity            AttemptLedgerIdentity `json:"identity"`
	Boundary            AttemptBoundary       `json:"boundary"`
	FirstSequence       uint64                `json:"first_sequence"`
	EgressFirstSequence uint64                `json:"egress_first_sequence"`
	LastSequence        uint64                `json:"last_sequence"`
	RecordCount         uint64                `json:"record_count"`
	PriorRoot           string                `json:"prior_root"`
	Root                string                `json:"root"`
	RecordHashes        []string              `json:"record_hashes"`
}

// AttemptLedger owns one append-only operator journal. Methods are safe for
// concurrent use; callers serialize it after StatsEngine when both are held.
type AttemptLedger struct {
	stateLock sync.Mutex
	path      string
	identity  AttemptLedgerIdentity
	vsk       ed25519.PrivateKey
	vpk       ed25519.PublicKey
	records   []AttemptRecord
	pending   map[connect.Id]AttemptRecord
	terminal  map[connect.Id]bool
	appendFn  func(string, []byte) error
}

func canonicalAttemptHex32(name, encoded string, zeroAllowed bool) ([32]byte, error) {
	var value [32]byte
	if encoded != strings.ToLower(encoded) || len(encoded) != 66 || !strings.HasPrefix(encoded, "0x") {
		return value, fmt.Errorf("%s is not canonical 32-byte hex", name)
	}
	decoded, err := hex.DecodeString(encoded[2:])
	if err != nil || len(decoded) != len(value) {
		return value, fmt.Errorf("%s is not 32-byte hex", name)
	}
	copy(value[:], decoded)
	if !zeroAllowed && value == ([32]byte{}) {
		return value, fmt.Errorf("%s is zero", name)
	}
	return value, nil
}

func attemptHex32(value [32]byte) string {
	return "0x" + hex.EncodeToString(value[:])
}

func zeroAttemptHash() string {
	return attemptHex32([32]byte{})
}

func validateAttemptLedgerIdentity(identity AttemptLedgerIdentity, expectedVPK ed25519.PublicKey) error {
	if identity.DeploymentID == "" || identity.ChainID == 0 || identity.Netuid == 0 || identity.ValidatorID == 0 || identity.NoID == 0 {
		return errors.New("attempt ledger identity is incomplete")
	}
	if _, err := canonicalAttemptHex32("attempt ledger genesis hash", identity.GenesisHash, false); err != nil {
		return err
	}
	vpk, err := canonicalAttemptHex32("attempt ledger validator vpk", identity.ValidatorVPK, false)
	if err != nil {
		return err
	}
	if len(expectedVPK) != 0 && !bytes.Equal(vpk[:], expectedVPK) {
		return errors.New("attempt ledger validator vpk differs")
	}
	return nil
}

func validateAttemptBoundary(boundary AttemptBoundary) error {
	if boundary.EVMBlock == 0 {
		return errors.New("attempt EVM boundary block is zero")
	}
	_, err := canonicalAttemptHex32("attempt EVM boundary hash", boundary.EVMBlockHash, false)
	return err
}

func validateAttemptBinding(binding AttemptBinding, clientID connect.Id) error {
	if binding.ClientID != clientID || binding.ClientID == (connect.Id{}) {
		return errors.New("attempt binding client id differs")
	}
	fleetID, fleetErr := canonicalAttemptHex32("attempt binding fleet id", binding.FleetID, true)
	hotkey, hotkeyErr := canonicalAttemptHex32("attempt binding hotkey", binding.Hotkey, true)
	if fleetErr != nil || hotkeyErr != nil {
		return errors.Join(fleetErr, hotkeyErr)
	}
	if binding.Active {
		if fleetID == ([32]byte{}) || hotkey == ([32]byte{}) || binding.Generation == 0 || (!binding.UIDFound && binding.UID != 0) {
			return errors.New("active attempt binding is incomplete")
		}
	} else if fleetID != ([32]byte{}) || hotkey != ([32]byte{}) || binding.Generation != 0 || binding.UIDFound || binding.UID != 0 {
		return errors.New("inactive attempt binding carries active identity")
	}
	return nil
}

func attemptRecordPayload(record *AttemptRecord) attemptRecordHashPayload {
	return attemptRecordHashPayload{
		Schema: record.Schema, Identity: record.Identity, Sequence: record.Sequence,
		PreviousHash: record.PreviousHash, Boundary: record.Boundary, TrailID: record.TrailID,
		ServerNonce: record.ServerNonce, VPK: record.VPK, M: record.M,
		Assignments: record.Assignments, Disposition: record.Disposition, Proof: record.Proof,
	}
}

func attemptRecordHash(record *AttemptRecord) ([32]byte, error) {
	var value [32]byte
	encoded, err := json.Marshal(attemptRecordPayload(record))
	if err != nil {
		return value, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(attemptLedgerHashDomain))
	_, _ = hash.Write(encoded)
	copy(value[:], hash.Sum(nil))
	return value, nil
}

func cloneAttemptRecord(record AttemptRecord) (AttemptRecord, error) {
	encoded, err := json.Marshal(&record)
	if err != nil {
		return AttemptRecord{}, err
	}
	var cloned AttemptRecord
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return AttemptRecord{}, err
	}
	return cloned, nil
}

func attemptRecordSignatureMessage(recordHash [32]byte) []byte {
	message := make([]byte, 0, len(attemptLedgerSignDomain)+len(recordHash))
	message = append(message, attemptLedgerSignDomain...)
	return append(message, recordHash[:]...)
}

func verifyAttemptRecord(record *AttemptRecord, expectedIdentity AttemptLedgerIdentity, expectedVPK ed25519.PublicKey, serverKeys map[byte]ed25519.PublicKey, requireServerKeys bool) error {
	if record == nil || record.Schema != attemptLedgerRecordSchema || record.Sequence == 0 || record.TrailID == (connect.Id{}) || len(record.ServerNonce) != connect.VerifyNonceSize || record.M < connect.VerifyMMin || record.M > connect.VerifyMMax {
		return errors.New("attempt record identity is incomplete")
	}
	if record.Identity != expectedIdentity {
		return errors.New("attempt record ledger identity differs")
	}
	if err := validateAttemptLedgerIdentity(record.Identity, expectedVPK); err != nil {
		return err
	}
	if !bytes.Equal(record.VPK, expectedVPK) {
		return errors.New("attempt record vpk differs")
	}
	if _, err := canonicalAttemptHex32("attempt previous hash", record.PreviousHash, true); err != nil {
		return err
	}
	if err := validateAttemptBoundary(record.Boundary); err != nil {
		return err
	}
	if len(record.Assignments) == 0 || len(record.Assignments) > record.M-1 {
		return errors.New("attempt assignment count is invalid")
	}
	for index := range record.Assignments {
		assignment := &record.Assignments[index]
		if len(assignment.Trail) != index+1 || assignment.NextHop == (connect.Id{}) || len(assignment.AssignSignature) != ed25519.SignatureSize {
			return fmt.Errorf("attempt assignment %d is incomplete", index)
		}
		if index > 0 {
			prior := record.Assignments[index-1]
			if !prior.Confirmed || len(assignment.Trail) != len(prior.Trail)+1 || !equalAttemptTrails(assignment.Trail[:len(prior.Trail)], prior.Trail) || assignment.Trail[len(assignment.Trail)-1] != prior.NextHop {
				return fmt.Errorf("attempt assignment %d does not extend its predecessor", index)
			}
		}
		walked := append(append([]connect.Id(nil), assignment.Trail...), assignment.NextHop)
		wantMessage, err := connect.BuildVerifyAssignMessage(assignment.ServerKeyID, record.TrailID, record.ServerNonce, record.VPK, byte(record.M), walked)
		if err != nil || !bytes.Equal(assignment.AssignMessage, wantMessage) {
			return fmt.Errorf("attempt assignment %d bytes differ from canonical ASSIGN", index)
		}
		if requireServerKeys {
			serverKey, ok := serverKeys[assignment.ServerKeyID]
			if !ok || len(serverKey) != ed25519.PublicKeySize || !connect.VerifyVerifyMessageSignature(serverKey, assignment.AssignMessage, assignment.AssignSignature) {
				return fmt.Errorf("attempt assignment %d server signature is invalid", index)
			}
		}
		if assignment.Confirmed != assignment.HasLatency {
			return fmt.Errorf("attempt assignment %d confirmation and latency differ", index)
		}
		if assignment.LatencyBucket >= statsLatencyBuckets || (!assignment.HasLatency && assignment.LatencyBucket != 0) {
			return fmt.Errorf("attempt assignment %d latency bucket is invalid", index)
		}
		if err := validateAttemptBinding(assignment.Binding, assignment.NextHop); err != nil {
			return fmt.Errorf("attempt assignment %d binding: %w", index, err)
		}
	}
	seedHop := record.Assignments[0].Trail[0]
	if seedHop == (connect.Id{}) {
		return errors.New("attempt seed hop is zero")
	}
	seenTrail := map[connect.Id]bool{seedHop: true}
	for index, assignment := range record.Assignments {
		if assignment.Trail[0] != seedHop {
			return fmt.Errorf("attempt assignment %d changed its seed hop", index)
		}
		if seenTrail[assignment.NextHop] {
			return fmt.Errorf("attempt assignment %d repeats a trail hop", index)
		}
		seenTrail[assignment.NextHop] = true
	}
	last := record.Assignments[len(record.Assignments)-1]
	switch record.Disposition {
	case AttemptDispositionComplete:
		if record.Proof == nil || len(record.Assignments) != record.M-1 || !last.Confirmed {
			return errors.New("completed attempt is partial")
		}
		if record.Proof.Version != 1 || record.Proof.M != record.M || len(record.Proof.Hops) != record.M || record.Proof.TrailId != record.TrailID || !bytes.Equal(record.Proof.ServerNonce, record.ServerNonce) || !bytes.Equal(record.Proof.Vpk, record.VPK) {
			return errors.New("attempt proof identity or depth differs")
		}
		if record.Proof.Epoch != record.Boundary.SettlementEpoch {
			return errors.New("completed attempt proof epoch differs from signed boundary")
		}
		if requireServerKeys {
			if err := VerifyProofRecord(record.Proof, expectedVPK, serverKeys, record.M); err != nil {
				return fmt.Errorf("attempt proof: %w", err)
			}
		}
		for index, assignment := range record.Assignments {
			if record.Proof.Hops[index+1].ClientId != assignment.NextHop {
				return fmt.Errorf("attempt proof hop %d differs from its assignment", index+1)
			}
		}
		if record.Proof.Hops[0].ClientId != seedHop {
			return errors.New("attempt proof seed hop differs from its assignment trail")
		}
	case AttemptDispositionPending, AttemptDispositionHopFailure, AttemptDispositionProtocol, AttemptDispositionUnknownFinal, AttemptDispositionValidatorError:
		if record.Proof != nil || last.Confirmed {
			return errors.New("failed attempt contains a completed final hop")
		}
	default:
		return fmt.Errorf("attempt disposition %q is invalid", record.Disposition)
	}
	hash, err := attemptRecordHash(record)
	if err != nil {
		return err
	}
	encodedHash, err := canonicalAttemptHex32("attempt record hash", record.RecordHash, false)
	if err != nil || encodedHash != hash {
		return errors.New("attempt record hash differs")
	}
	if len(record.Signature) != ed25519.SignatureSize || !ed25519.Verify(expectedVPK, attemptRecordSignatureMessage(hash), record.Signature) {
		return errors.New("attempt record validator signature is invalid")
	}
	return nil
}

func equalAttemptTrails(left, right []connect.Id) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// VerifyAttemptRecord validates the complete validator and server attestation.
func VerifyAttemptRecord(record *AttemptRecord, expectedIdentity AttemptLedgerIdentity, expectedVPK ed25519.PublicKey, serverKeys map[byte]ed25519.PublicKey) error {
	return verifyAttemptRecord(record, expectedIdentity, expectedVPK, serverKeys, true)
}

func appendAttemptLedgerFile(path string, payload []byte) error {
	created := false
	if info, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		created = true
	} else if err != nil {
		return err
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("attempt ledger is not a private regular file")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("attempt ledger is not a private regular file")
	}
	written, err := file.Write(payload)
	if err != nil {
		return err
	}
	if written != len(payload) {
		return fmt.Errorf("attempt ledger wrote %d of %d bytes", written, len(payload))
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if !created {
		return nil
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// NewAttemptLedger loads and verifies one private operator ledger. A torn or
// malformed line fails closed; measurement history is never silently skipped.
func NewAttemptLedger(stateDir string, identity AttemptLedgerIdentity, vsk ed25519.PrivateKey) (*AttemptLedger, error) {
	if len(vsk) != ed25519.PrivateKeySize {
		return nil, errors.New("attempt ledger validator private key is invalid")
	}
	vpk := vsk.Public().(ed25519.PublicKey)
	identity.ValidatorVPK = attemptHex32(*(*[32]byte)(vpk))
	if err := validateAttemptLedgerIdentity(identity, vpk); err != nil {
		return nil, err
	}
	if err := ensurePrivateStateDir(stateDir); err != nil {
		return nil, err
	}
	ledger := &AttemptLedger{
		path: filepath.Join(stateDir, "attempt-ledger.jsonl"), identity: identity,
		vsk: append(ed25519.PrivateKey(nil), vsk...), vpk: append(ed25519.PublicKey(nil), vpk...),
		pending: map[connect.Id]AttemptRecord{}, terminal: map[connect.Id]bool{}, appendFn: appendAttemptLedgerFile,
	}
	encoded, err := os.ReadFile(ledger.path)
	if errors.Is(err, os.ErrNotExist) {
		return ledger, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(ledger.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("attempt ledger is not a private regular file")
	}
	if len(encoded) != 0 && encoded[len(encoded)-1] != '\n' {
		return nil, errors.New("attempt ledger has a torn final line")
	}
	previousHash := zeroAttemptHash()
	for lineIndex, line := range bytes.Split(encoded, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var record AttemptRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("attempt ledger line %d: %w", lineIndex+1, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("attempt ledger line %d contains trailing JSON", lineIndex+1)
		}
		canonical, err := json.Marshal(&record)
		if err != nil || !bytes.Equal(canonical, line) {
			return nil, fmt.Errorf("attempt ledger line %d is not canonical", lineIndex+1)
		}
		if record.Sequence != uint64(len(ledger.records))+1 || record.PreviousHash != previousHash {
			return nil, fmt.Errorf("attempt ledger line %d breaks sequence or hash chain", lineIndex+1)
		}
		if err := verifyAttemptRecord(&record, identity, vpk, nil, false); err != nil {
			return nil, fmt.Errorf("attempt ledger line %d: %w", lineIndex+1, err)
		}
		ledger.records = append(ledger.records, record)
		previousHash = record.RecordHash
	}
	pending, terminal, err := attemptLifecycle(ledger.records)
	if err != nil {
		return nil, fmt.Errorf("attempt ledger lifecycle: %w", err)
	}
	ledger.pending, ledger.terminal = pending, terminal
	return ledger, nil
}

func (self *AttemptLedger) lastRootWithLock() string {
	if len(self.records) == 0 {
		return zeroAttemptHash()
	}
	return self.records[len(self.records)-1].RecordHash
}

// LastSequence returns the durable end of the chain.
func (self *AttemptLedger) LastSequence() uint64 {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return uint64(len(self.records))
}

// RecordsAfter returns independent copies after the supplied sequence.
func (self *AttemptLedger) RecordsAfter(sequence uint64) ([]AttemptRecord, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if sequence > uint64(len(self.records)) {
		return nil, errors.New("attempt ledger cursor exceeds durable sequence")
	}
	records := make([]AttemptRecord, len(self.records)-int(sequence))
	for index := range records {
		cloned, err := cloneAttemptRecord(self.records[int(sequence)+index])
		if err != nil {
			return nil, err
		}
		records[index] = cloned
	}
	return records, nil
}

// Append signs and durably appends one already validated trail outcome.
func (self *AttemptLedger) Append(record AttemptRecord) (*AttemptRecord, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	record.Schema = attemptLedgerRecordSchema
	record.Identity = self.identity
	record.Sequence = uint64(len(self.records)) + 1
	record.PreviousHash = self.lastRootWithLock()
	record.VPK = append([]byte(nil), self.vpk...)
	record.RecordHash = ""
	record.Signature = nil
	hash, err := attemptRecordHash(&record)
	if err != nil {
		return nil, err
	}
	record.RecordHash = attemptHex32(hash)
	record.Signature = ed25519.Sign(self.vsk, attemptRecordSignatureMessage(hash))
	if err := verifyAttemptRecord(&record, self.identity, self.vpk, nil, false); err != nil {
		return nil, err
	}
	if err := validateAttemptLifecycleRecord(self.pending, self.terminal, record, len(self.records)); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(&record)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if err := self.appendFn(self.path, encoded); err != nil {
		return nil, err
	}
	stored, err := cloneAttemptRecord(record)
	if err != nil {
		return nil, err
	}
	self.records = append(self.records, stored)
	applyAttemptLifecycleRecord(self.pending, self.terminal, stored)
	copy, err := cloneAttemptRecord(stored)
	return &copy, err
}

func attemptAssignmentsEqual(left, right []AttemptAssignment) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func attemptAssignmentsWithUnconfirmedLast(assignments []AttemptAssignment) []AttemptAssignment {
	cloned := append([]AttemptAssignment(nil), assignments...)
	if len(cloned) != 0 {
		last := len(cloned) - 1
		cloned[last].Confirmed = false
		cloned[last].HasLatency = false
		cloned[last].LatencyBucket = 0
	}
	return cloned
}

func validateAttemptLifecycleRecord(pending map[connect.Id]AttemptRecord, terminal map[connect.Id]bool, record AttemptRecord, index int) error {
	prior, exists := pending[record.TrailID]
	if record.Disposition == AttemptDispositionPending {
		if terminal[record.TrailID] {
			return fmt.Errorf("attempt record %d reuses a terminal trail id", index)
		}
		if !exists && len(record.Assignments) != 1 {
			return fmt.Errorf("attempt record %d skips its first pending checkpoint", index)
		}
		if exists {
			if len(record.Assignments) != len(prior.Assignments)+1 {
				return fmt.Errorf("attempt record %d does not extend its pending checkpoint", index)
			}
			prefix := attemptAssignmentsWithUnconfirmedLast(record.Assignments[:len(prior.Assignments)])
			if !attemptAssignmentsEqual(prefix, prior.Assignments) {
				return fmt.Errorf("attempt record %d does not extend its pending checkpoint", index)
			}
		}
		return nil
	}
	assignmentsMatch := attemptAssignmentsEqual(record.Assignments, prior.Assignments)
	if record.Disposition == AttemptDispositionComplete {
		assignmentsMatch = attemptAssignmentsEqual(attemptAssignmentsWithUnconfirmedLast(record.Assignments), prior.Assignments)
	}
	if !exists || !assignmentsMatch || record.Boundary != prior.Boundary || record.M != prior.M || !bytes.Equal(record.ServerNonce, prior.ServerNonce) {
		return fmt.Errorf("attempt record %d has no matching pending checkpoint", index)
	}
	return nil
}

func applyAttemptLifecycleRecord(pending map[connect.Id]AttemptRecord, terminal map[connect.Id]bool, record AttemptRecord) {
	if record.Disposition == AttemptDispositionPending {
		pending[record.TrailID] = record
		return
	}
	delete(pending, record.TrailID)
	terminal[record.TrailID] = true
}

func attemptLifecycle(records []AttemptRecord) (map[connect.Id]AttemptRecord, map[connect.Id]bool, error) {
	pending := map[connect.Id]AttemptRecord{}
	terminal := map[connect.Id]bool{}
	for index, record := range records {
		if err := validateAttemptLifecycleRecord(pending, terminal, record, index); err != nil {
			return nil, nil, err
		}
		applyAttemptLifecycleRecord(pending, terminal, record)
	}
	return pending, terminal, nil
}

func pendingAttemptRecords(records []AttemptRecord) (map[connect.Id]AttemptRecord, error) {
	pending, _, err := attemptLifecycle(records)
	return pending, err
}

// RecoverPending appends conservative validator-error outcomes for ASSIGNs
// that were fsynced before an abrupt process exit. The final assigned hop stays
// unconfirmed, so a crash cannot erase a denominator or manufacture success.
func (self *AttemptLedger) RecoverPending() ([]AttemptRecord, error) {
	self.stateLock.Lock()
	records := append([]AttemptRecord(nil), self.records...)
	self.stateLock.Unlock()
	pending, err := pendingAttemptRecords(records)
	if err != nil {
		return nil, err
	}
	trailIDs := make([]connect.Id, 0, len(pending))
	for trailID := range pending {
		trailIDs = append(trailIDs, trailID)
	}
	sort.Slice(trailIDs, func(i, j int) bool { return trailIDs[i].LessThan(trailIDs[j]) })
	recovered := make([]AttemptRecord, 0, len(trailIDs))
	for _, trailID := range trailIDs {
		record := pending[trailID]
		record.Disposition = AttemptDispositionValidatorError
		record.Proof = nil
		committed, err := self.Append(record)
		if err != nil {
			return nil, err
		}
		recovered = append(recovered, *committed)
	}
	return recovered, nil
}

func attemptCutSignaturePayloadFor(cut *AttemptLedgerCut) attemptCutSignaturePayload {
	recordHashes := make([]string, len(cut.Records))
	for index := range cut.Records {
		recordHashes[index] = cut.Records[index].RecordHash
	}
	return attemptCutSignaturePayload{
		Schema: cut.Schema, Identity: cut.Identity, Boundary: cut.Boundary,
		FirstSequence: cut.FirstSequence, EgressFirstSequence: cut.EgressFirstSequence,
		LastSequence: cut.LastSequence, RecordCount: cut.RecordCount,
		PriorRoot: cut.PriorRoot, Root: cut.Root, RecordHashes: recordHashes,
	}
}

func attemptCutSignatureMessage(cut *AttemptLedgerCut) ([]byte, error) {
	encoded, err := json.Marshal(attemptCutSignaturePayloadFor(cut))
	if err != nil {
		return nil, err
	}
	message := make([]byte, 0, len(attemptCutSignDomain)+len(encoded))
	message = append(message, attemptCutSignDomain...)
	return append(message, encoded...), nil
}

// BuildCut signs the complete current-settlement replay through the current
// chain root. firstSequence and egressFirstSequence are one-based cursors.
func (self *AttemptLedger) BuildCut(boundary AttemptBoundary, firstSequence, egressFirstSequence uint64) (*AttemptLedgerCut, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := validateAttemptBoundary(boundary); err != nil {
		return nil, err
	}
	nextSequence := uint64(len(self.records)) + 1
	if firstSequence == 0 || firstSequence > nextSequence || egressFirstSequence < firstSequence || egressFirstSequence > nextSequence {
		return nil, errors.New("attempt cut cursor is outside the durable ledger")
	}
	records := make([]AttemptRecord, len(self.records)-int(firstSequence)+1)
	if firstSequence <= uint64(len(self.records)) {
		for index := range records {
			cloned, err := cloneAttemptRecord(self.records[int(firstSequence)-1+index])
			if err != nil {
				return nil, err
			}
			records[index] = cloned
		}
	}
	priorRoot := zeroAttemptHash()
	if firstSequence > 1 {
		priorRoot = self.records[firstSequence-2].RecordHash
	}
	for index := range records {
		if records[index].Boundary.SettlementEpoch != boundary.SettlementEpoch || records[index].Boundary.EVMBlock > boundary.EVMBlock {
			return nil, fmt.Errorf("attempt record %d is outside the cut boundary", records[index].Sequence)
		}
	}
	cut := &AttemptLedgerCut{
		Schema: attemptLedgerCutSchema, Identity: self.identity, Boundary: boundary,
		FirstSequence: firstSequence, EgressFirstSequence: egressFirstSequence,
		LastSequence: uint64(len(self.records)), RecordCount: uint64(len(records)),
		PriorRoot: priorRoot, Root: self.lastRootWithLock(), Records: records,
	}
	message, err := attemptCutSignatureMessage(cut)
	if err != nil {
		return nil, err
	}
	cut.Signature = ed25519.Sign(self.vsk, message)
	return cut, nil
}

func verifyAttemptLedgerCut(cut *AttemptLedgerCut, expectedVPK ed25519.PublicKey, serverKeys map[byte]ed25519.PublicKey, requireServerKeys bool) error {
	if cut == nil || cut.Schema != attemptLedgerCutSchema || cut.FirstSequence == 0 || cut.EgressFirstSequence < cut.FirstSequence {
		return errors.New("attempt ledger cut identity is incomplete")
	}
	if err := validateAttemptLedgerIdentity(cut.Identity, expectedVPK); err != nil {
		return err
	}
	if err := validateAttemptBoundary(cut.Boundary); err != nil {
		return err
	}
	if cut.RecordCount != uint64(len(cut.Records)) {
		return errors.New("attempt ledger cut record count differs")
	}
	if _, err := canonicalAttemptHex32("attempt cut prior root", cut.PriorRoot, true); err != nil {
		return err
	}
	if _, err := canonicalAttemptHex32("attempt cut root", cut.Root, true); err != nil {
		return err
	}
	previousHash := cut.PriorRoot
	cutRecords := make([]AttemptRecord, 0, len(cut.Records))
	for index := range cut.Records {
		record := &cut.Records[index]
		if record.Sequence != cut.FirstSequence+uint64(index) || record.PreviousHash != previousHash || record.Boundary.SettlementEpoch != cut.Boundary.SettlementEpoch || record.Boundary.EVMBlock > cut.Boundary.EVMBlock {
			return fmt.Errorf("attempt cut record %d breaks its range or boundary", index)
		}
		if err := verifyAttemptRecord(record, cut.Identity, expectedVPK, serverKeys, requireServerKeys); err != nil {
			return fmt.Errorf("attempt cut record %d: %w", index, err)
		}
		previousHash = record.RecordHash
		cutRecords = append(cutRecords, *record)
	}
	if len(cut.Records) == 0 {
		if cut.LastSequence+1 != cut.FirstSequence || cut.Root != cut.PriorRoot || cut.EgressFirstSequence != cut.FirstSequence {
			return errors.New("empty attempt cut range is inconsistent")
		}
	} else if cut.LastSequence != cut.Records[len(cut.Records)-1].Sequence || cut.Root != previousHash || cut.EgressFirstSequence > cut.LastSequence+1 {
		return errors.New("attempt cut terminal root or cursor differs")
	}
	if pending, err := pendingAttemptRecords(cutRecords); err != nil {
		return err
	} else if len(pending) != 0 {
		return errors.New("attempt cut contains an unfinished trail")
	}
	message, err := attemptCutSignatureMessage(cut)
	if err != nil {
		return err
	}
	if len(cut.Signature) != ed25519.SignatureSize || !ed25519.Verify(expectedVPK, message, cut.Signature) {
		return errors.New("attempt cut validator signature is invalid")
	}
	return nil
}

// VerifyAttemptLedgerCut verifies every server assignment, completed proof,
// validator record signature, range link, and cut signature.
func VerifyAttemptLedgerCut(cut *AttemptLedgerCut, expectedVPK ed25519.PublicKey, serverKeys map[byte]ed25519.PublicKey) error {
	return verifyAttemptLedgerCut(cut, expectedVPK, serverKeys, true)
}

// SortedAttemptBindings returns every distinct binding identity represented in
// a cut. One client may legitimately appear in more than one generation; the
// identities remain separate so old work cannot transfer to its new owner.
func SortedAttemptBindings(cut *AttemptLedgerCut) ([]AttemptBinding, error) {
	if cut == nil {
		return nil, errors.New("attempt cut is nil")
	}
	bindings := map[string]AttemptBinding{}
	for _, record := range cut.Records {
		for _, assignment := range record.Assignments {
			binding := assignment.Binding
			key := fmt.Sprintf("%s:%t:%s:%s:%020d:%t:%05d", binding.ClientID, binding.Active, binding.FleetID, binding.Hotkey, binding.Generation, binding.UIDFound, binding.UID)
			bindings[key] = binding
		}
	}
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]AttemptBinding, len(keys))
	for index, key := range keys {
		result[index] = bindings[key]
	}
	return result, nil
}

// AttemptEgressClaim binds one server-attested prefix to the exact binding
// generation captured before the trail. It is the only head-score input that
// should be derived from an attempt cut.
type AttemptEgressClaim struct {
	Sequence     uint64         `json:"sequence"`
	Binding      AttemptBinding `json:"binding"`
	EgressIPHash string         `json:"egress_ip_hash"`
}

// AttemptCutEgressClaims returns deterministic, generation-separated prefix
// evidence from successful records in the cut's native head range.
func AttemptCutEgressClaims(cut *AttemptLedgerCut) ([]AttemptEgressClaim, error) {
	if cut == nil {
		return nil, errors.New("attempt cut is nil")
	}
	var claims []AttemptEgressClaim
	for _, record := range cut.Records {
		if record.Sequence < cut.EgressFirstSequence || record.Disposition != AttemptDispositionComplete || record.Proof == nil {
			continue
		}
		bindings := map[connect.Id]AttemptBinding{}
		for _, assignment := range record.Assignments {
			bindings[assignment.NextHop] = assignment.Binding
		}
		for hopIndex := 1; hopIndex < len(record.Proof.Hops); hopIndex++ {
			hop := record.Proof.Hops[hopIndex]
			binding, ok := bindings[hop.ClientId]
			if !ok {
				return nil, fmt.Errorf("attempt sequence %d proof hop %s has no binding", record.Sequence, hop.ClientId)
			}
			if hop.EgressIpHash == ([32]byte{}) || !binding.Active || !binding.UIDFound {
				continue
			}
			claims = append(claims, AttemptEgressClaim{Sequence: record.Sequence, Binding: binding, EgressIPHash: attemptHex32(hop.EgressIpHash)})
		}
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Sequence != claims[j].Sequence {
			return claims[i].Sequence < claims[j].Sequence
		}
		if claims[i].Binding.ClientID != claims[j].Binding.ClientID {
			return claims[i].Binding.ClientID.LessThan(claims[j].Binding.ClientID)
		}
		return claims[i].EgressIPHash < claims[j].EgressIPHash
	})
	return claims, nil
}

var errAttemptCutPending = errors.New("attempt ledger cut is waiting for active trails")

// AttachAttemptLedger binds durable statistics to one journal and replays any
// records fsynced after the last stats snapshot. Attaching legacy nonempty
// statistics to a new empty ledger would make completeness unprovable and is
// therefore rejected.
func (self *StatsEngine) AttachAttemptLedger(ledger *AttemptLedger, stateDir string) error {
	if ledger == nil {
		return errors.New("attempt ledger is nil")
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.attemptLedger != nil && self.attemptLedger != ledger {
		return errors.New("statistics engine already has a different attempt ledger")
	}
	if _, err := ledger.RecoverPending(); err != nil {
		return fmt.Errorf("recover pending attempt: %w", err)
	}
	lastSequence := ledger.LastSequence()
	if self.attemptSettlementFirstSequence == 0 {
		if lastSequence != 0 || len(self.window) != 0 || len(self.egress) != 0 {
			return errors.New("existing measurement state requires an authoritative attempt ledger migration")
		}
		self.attemptSettlementFirstSequence = 1
		self.attemptEgressFirstSequence = 1
	}
	if self.attemptLastAppliedSequence > lastSequence || self.attemptSettlementFirstSequence > lastSequence+1 || self.attemptEgressFirstSequence > lastSequence+1 {
		return errors.New("statistics attempt cursor exceeds the durable ledger")
	}
	self.attemptLedger = ledger
	records, err := ledger.RecordsAfter(self.attemptLastAppliedSequence)
	if err != nil {
		return err
	}
	for index := range records {
		record := &records[index]
		if !self.settlementEpochKnown || record.Boundary.SettlementEpoch != self.settlementEpoch {
			return fmt.Errorf("unapplied attempt sequence %d belongs to settlement epoch %d, active %d", record.Sequence, record.Boundary.SettlementEpoch, self.settlementEpoch)
		}
		if record.Disposition != AttemptDispositionPending {
			if err := self.validateAttemptStatsWithLock(record); err != nil {
				return err
			}
			self.applyAttemptStatsWithLock(record)
		}
		self.attemptLastAppliedSequence = record.Sequence
	}
	return self.saveWithLock(stateDir)
}

func (self *StatsEngine) checkpointAttempt(ledger *AttemptLedger, record AttemptRecord) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.activeAttemptCount == 0 || self.attemptLedger != ledger || ledger == nil {
		return errors.New("checkpoint trail attempt without active ownership")
	}
	record.Disposition = AttemptDispositionPending
	record.Proof = nil
	committed, err := ledger.Append(record)
	if err != nil {
		return fmt.Errorf("attempt checkpoint append: %w", err)
	}
	if committed.Sequence != self.attemptLastAppliedSequence+1 {
		return fmt.Errorf("attempt checkpoint sequence %d does not follow applied %d", committed.Sequence, self.attemptLastAppliedSequence)
	}
	self.attemptLastAppliedSequence = committed.Sequence
	return nil
}

func (self *StatsEngine) beginAttempt(settlementEpoch uint64, ledger *AttemptLedger) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	if ledger == nil || self.attemptLedger != ledger {
		return errors.New("trail attempt ledger is not attached to statistics")
	}
	if !self.settlementEpochKnown || settlementEpoch != self.settlementEpoch {
		return fmt.Errorf("trail attempt settlement epoch %d, active %d", settlementEpoch, self.settlementEpoch)
	}
	if self.attemptCutPending {
		return errAttemptCutPending
	}
	if self.activeAttemptCount == ^uint64(0) {
		return errors.New("active trail attempt count overflow")
	}
	self.activeAttemptCount++
	return nil
}

func (self *StatsEngine) abortAttempt() {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.activeAttemptCount == 0 {
		panic("abort trail attempt without active ownership")
	}
	self.activeAttemptCount--
}

func (self *StatsEngine) validateAttemptStatsWithLock(record *AttemptRecord) error {
	assignments := map[connect.Id]uint64{}
	confirmations := map[connect.Id]uint64{}
	latencyBuckets := map[connect.Id][statsLatencyBuckets]uint64{}
	for _, assignment := range record.Assignments {
		if assignments[assignment.NextHop] == ^uint64(0) {
			return errors.New("attempt assignment delta overflows uint64")
		}
		assignments[assignment.NextHop]++
		if assignment.Confirmed {
			if confirmations[assignment.NextHop] == ^uint64(0) {
				return errors.New("attempt confirmation delta overflows uint64")
			}
			confirmations[assignment.NextHop]++
			buckets := latencyBuckets[assignment.NextHop]
			if buckets[assignment.LatencyBucket] == ^uint64(0) {
				return errors.New("attempt latency delta overflows uint64")
			}
			buckets[assignment.LatencyBucket]++
			latencyBuckets[assignment.NextHop] = buckets
		}
	}
	for clientID, delta := range assignments {
		window := self.window[clientID]
		if window != nil && window.Assignments > ^uint64(0)-delta {
			return fmt.Errorf("provider %s assignment counter overflows", clientID)
		}
	}
	for clientID, delta := range confirmations {
		window := self.window[clientID]
		if window != nil && window.Confirmations > ^uint64(0)-delta {
			return fmt.Errorf("provider %s confirmation counter overflows", clientID)
		}
		for bucket, bucketDelta := range latencyBuckets[clientID] {
			if window != nil && window.LatencyBuckets[bucket] > ^uint64(0)-bucketDelta {
				return fmt.Errorf("provider %s latency bucket %d overflows", clientID, bucket)
			}
		}
	}
	return nil
}

func (self *StatsEngine) applyAttemptStatsWithLock(record *AttemptRecord) {
	for _, assignment := range record.Assignments {
		window := self.windowFor(assignment.NextHop)
		window.Assignments++
		if assignment.Confirmed {
			window.Confirmations++
			window.LatencyBuckets[assignment.LatencyBucket]++
		}
	}
	if record.Disposition != AttemptDispositionComplete || record.Proof == nil {
		return
	}
	for hopIndex := 1; hopIndex < len(record.Proof.Hops); hopIndex++ {
		hop := record.Proof.Hops[hopIndex]
		if hop.EgressIpHash == ([32]byte{}) {
			continue
		}
		if self.egress[hop.ClientId] == nil {
			self.egress[hop.ClientId] = map[[32]byte]bool{}
		}
		self.egress[hop.ClientId][hop.EgressIpHash] = true
	}
}

// commitAttempt makes the signed WAL append and derived statistics one ordered
// operation with respect to every cut. The legacy proof file is an idempotent
// projection written only after the authoritative terminal record exists.
func (self *StatsEngine) commitAttempt(ledger *AttemptLedger, store *ProofStore, record AttemptRecord) (*AttemptRecord, error) {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.activeAttemptCount == 0 {
		return nil, errors.New("commit trail attempt without active ownership")
	}
	defer func() { self.activeAttemptCount-- }()
	if self.attemptLedger != ledger || ledger == nil {
		return nil, errors.New("trail attempt ledger differs from statistics")
	}
	if record.Boundary.SettlementEpoch != self.settlementEpoch {
		return nil, fmt.Errorf("trail attempt completed in settlement epoch %d after boundary advanced to %d", record.Boundary.SettlementEpoch, self.settlementEpoch)
	}
	if err := self.validateAttemptStatsWithLock(&record); err != nil {
		return nil, err
	}
	committed, err := ledger.Append(record)
	if err != nil {
		return nil, fmt.Errorf("attempt ledger append: %w", err)
	}
	if committed.Sequence != self.attemptLastAppliedSequence+1 {
		return nil, fmt.Errorf("attempt ledger sequence %d does not follow applied %d", committed.Sequence, self.attemptLastAppliedSequence)
	}
	self.applyAttemptStatsWithLock(committed)
	self.attemptLastAppliedSequence = committed.Sequence
	if committed.Proof != nil && store != nil {
		if err := store.projectAttemptProof(ledger, committed.Proof); err != nil {
			return committed, fmt.Errorf("proof projection persist: %w", err)
		}
	}
	return committed, nil
}
