// Offline custody retains one approved request and one exact public signature.
// It never loads a native secret, signs, submits, or infers never-issued from
// a missing receipt. Independent eligibility and global custody fencing remain required.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

const rootOfflineTrustSchema = "urnetwork-mainnet-root-offline-custody-trust-v1"
const rootOfflineApprovalSchema = "urnetwork-mainnet-root-offline-approval-v1"
const rootOfflinePacketSchema = "urnetwork-mainnet-root-offline-request-v1"
const rootOfflineSignatureSchema = "urnetwork-mainnet-root-offline-signature-v1"
const rootOfflineStateSchema = "urnetwork-mainnet-root-offline-custody-state-v1"

var errRootOfflineSignatureRequired = errors.New("root offline custody has no retained signature; issuance remains unresolved")

// Both supported signature algorithms use exactly 64 bytes. Bound the public
// input before decoding so a supplied receipt cannot allocate arbitrary bytes.
func rootOfflineSignatureBytes(value string) ([]byte, error) {
	if len(value) != 128 {
		return nil, errors.New("root offline signature requires exactly 64 bytes of canonical hex")
	}
	signature, err := hex.DecodeString(value)
	if err != nil || value != hex.EncodeToString(signature) {
		return nil, errors.New("root offline signature is not canonical hex")
	}
	return signature, nil
}

// This trust configuration must be provisioned independently of imported files.
// The approval public key is separate from the native sr25519 hotkey. Its
// signatures approve an exact request, not live eligibility or a custody fence.
type rootOfflineCustodyTrust struct {
	Schema            string `json:"schema"`
	NativeChain       string `json:"native_chain"`
	GenesisHash       string `json:"genesis_hash"`
	EvmChainId        uint64 `json:"evm_chain_id"`
	Hotkey            string `json:"hotkey_account_id"`
	CustodyId         string `json:"custody_id"`
	PolicyHash        string `json:"policy_hash"`
	ApprovalPublicKey string `json:"approval_ed25519_public_key"`
	StatePath         string `json:"custody_state_path"`
}

// No endpoint, private key or default mainnet authority is inferred.
func (self rootOfflineCustodyTrust) validate() error {
	if self.Schema != rootOfflineTrustSchema || strings.TrimSpace(self.NativeChain) == "" || self.EvmChainId != mainnetEvmChainId ||
		!rootCanonicalHash(self.GenesisHash) || !rootCanonicalHash(self.Hotkey) || !rootCanonicalHash(self.ApprovalPublicKey) ||
		!planSha256(self.PolicyHash) || self.CustodyId == "" || len(self.CustodyId) > 128 ||
		!filepath.IsAbs(self.StatePath) || filepath.Clean(self.StatePath) != self.StatePath || self.StatePath == "/" {
		return errors.New("root offline custody requires independently pinned mainnet, hotkey, policy, approval key and private state path")
	}
	return nil
}

// ApprovalHash and RequestHash in this embedded action are empty to avoid a
// circular hash. Every other field, including payload, era, limits and paths,
// is signed. The complete approval artifact hash becomes Scope.ApprovalHash.
type rootOfflineApproval struct {
	Schema    string     `json:"schema"`
	Action    rootAction `json:"action"`
	Signature string     `json:"approval_signature_ed25519"`
}

// Domain separation prevents a signature for another document being reused.
func (self rootOfflineApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(rootOfflineApprovalSchema+"\x00"), raw...), err
}

// The full packet is immutable and separately hash-bound before any export.
// It is a signing proposal; current authority must still be checked by the owner.
type rootOfflineCustodyPacket struct {
	Schema      string              `json:"schema"`
	TrustHash   string              `json:"custody_trust_hash"`
	Action      rootAction          `json:"action"`
	Approval    rootOfflineApproval `json:"approval"`
	ContentHash string              `json:"content_hash"`
}

// Checks both independently pinned approval and the native action encoding.
// A rehashed caller file cannot replace the approval public key or its scope.
func (self rootOfflineCustodyPacket) validate(trust rootOfflineCustodyTrust) error {
	if err := errors.Join(trust.validate(), self.Action.validate()); err != nil {
		return err
	}
	scope := self.Action.Scope
	if self.Schema != rootOfflinePacketSchema || self.TrustHash != rootObjectHash(trust) ||
		scope.NativeChain != trust.NativeChain || scope.GenesisHash != trust.GenesisHash || scope.EvmChainId != trust.EvmChainId ||
		scope.Hotkey != trust.Hotkey || scope.CustodyId != trust.CustodyId || scope.PolicyHash != trust.PolicyHash ||
		scope.StatePath == trust.StatePath || scope.StatePath+".lock" == trust.StatePath || trust.StatePath+".lock" == scope.StatePath ||
		self.Approval.Schema != rootOfflineApprovalSchema ||
		scope.ApprovalHash != rootObjectHash(self.Approval) {
		return errors.New("root offline packet differs from independent custody or approval scope")
	}
	normalized := copyRootAction(self.Action)
	normalized.Scope.ApprovalHash, normalized.RequestHash = "", ""
	if rootObjectHash(normalized) != rootObjectHash(self.Approval.Action) {
		return errors.New("root offline approval does not bind the exact action")
	}
	key, _ := hex.DecodeString(trust.ApprovalPublicKey[2:])
	signature, err := rootOfflineSignatureBytes(self.Approval.Signature)
	message, messageErr := self.Approval.signingBytes()
	if err != nil || messageErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("root offline approval signature is invalid")
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if !planSha256(claimed) || claimed != rootObjectHash(self) {
		return errors.New("root offline packet seal differs")
	}
	return nil
}

// Packet construction checks existing external approval; it cannot issue one.
func newRootOfflineCustodyPacket(trust rootOfflineCustodyTrust, action rootAction, approval rootOfflineApproval) (rootOfflineCustodyPacket, error) {
	packet := rootOfflineCustodyPacket{Schema: rootOfflinePacketSchema, TrustHash: rootObjectHash(trust), Action: copyRootAction(action), Approval: approval}
	packet.Approval.Action = copyRootAction(approval.Action)
	packet.ContentHash = rootObjectHash(packet)
	if err := packet.validate(trust); err != nil {
		return rootOfflineCustodyPacket{}, err
	}
	return packet, nil
}

// The public receipt contains only the exact native signature and packet hash.
// It cannot select another action, nonce, era, hotkey or signing algorithm.
type rootOfflineSignature struct {
	Schema     string `json:"schema"`
	PacketHash string `json:"packet_hash"`
	Signature  string `json:"signature_sr25519"`
}

// Requested and signed are lifetime states, not renewable budget allocations.
type rootOfflineCustodyRecord struct {
	Schema        string                   `json:"schema"`
	Packet        rootOfflineCustodyPacket `json:"packet"`
	Phase         string                   `json:"phase"`
	Signature     string                   `json:"signature_sr25519,omitempty"`
	ExtrinsicHash string                   `json:"extrinsic_hash,omitempty"`
	ContentHash   string                   `json:"content_hash"`
}

// Every load verifies the native signature as well as both immutable seals.
func (self rootOfflineCustodyRecord) validate(trust rootOfflineCustodyTrust) error {
	if err := self.Packet.validate(trust); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != rootOfflineStateSchema || claimed != rootObjectHash(self) {
		return errors.New("root offline custody state seal differs")
	}
	switch self.Phase {
	case "requested":
		if self.Signature != "" || self.ExtrinsicHash != "" {
			return errors.New("unsigned custody intent carries a signature")
		}
	case "signed":
		signature, err := rootOfflineSignatureBytes(self.Signature)
		if err != nil {
			return err
		}
		raw, err := self.Packet.Action.signed(signature)
		if err != nil || rootExtrinsicHash(raw) != self.ExtrinsicHash {
			return errors.New("root offline custody signature or signed bytes differ")
		}
	default:
		return errors.New("root offline custody state has an unknown phase")
	}
	return nil
}

// The durable owner is testable across both sides of an ambiguous store write.
type rootOfflineCustodyStorage interface {
	load() (rootOfflineCustodyRecord, error)
	save(rootOfflineCustodyRecord) error
}

// Calls serialize with cancellation-aware ownership. A durability failure
// poisons this instance; reopening resolves the surviving complete record.
// No method creates a native signature or reports authoritative never-issued.
type rootOfflineCustody struct {
	trust    rootOfflineCustodyTrust
	store    rootOfflineCustodyStorage
	ownerCh  chan struct{}
	poisoned bool
	failure  error
}

// Only a valid complete preexisting intent can supply this signing port.
func newRootOfflineCustody(trust rootOfflineCustodyTrust, store rootOfflineCustodyStorage) (*rootOfflineCustody, error) {
	if err := trust.validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("root offline custody store is missing")
	}
	record, err := store.load()
	if err != nil {
		return nil, err
	}
	if err := record.validate(trust); err != nil {
		return nil, err
	}
	return &rootOfflineCustody{trust: trust, store: store, ownerCh: make(chan struct{}, 1)}, nil
}

// A canceled waiter cannot read or mutate another operation's retained state.
func (self *rootOfflineCustody) acquire(ctx context.Context) error {
	if ctx == nil {
		return errors.New("root offline custody requires a context")
	}
	select {
	case self.ownerCh <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-self.ownerCh
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Any missing, corrupt or ambiguous state remains an unresolved custody event.
func (self *rootOfflineCustody) load() (rootOfflineCustodyRecord, error) {
	if self.poisoned {
		return rootOfflineCustodyRecord{}, errors.Join(errors.New("root offline custody must be reopened after a durability or integrity error"), self.failure)
	}
	record, err := self.store.load()
	if err == nil {
		err = record.validate(self.trust)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.poisoned, self.failure = true, err
	}
	return record, err
}

// Export borrows no mutable action vectors from the retained record.
func (self *rootOfflineCustody) packet(ctx context.Context) (rootOfflineCustodyPacket, error) {
	if err := self.acquire(ctx); err != nil {
		return rootOfflineCustodyPacket{}, err
	}
	defer func() { <-self.ownerCh }()
	record, err := self.load()
	if err != nil {
		return rootOfflineCustodyPacket{}, err
	}
	packet := record.Packet
	packet.Action = copyRootAction(packet.Action)
	packet.Approval.Action = copyRootAction(packet.Approval.Action)
	if err := ctx.Err(); err != nil {
		return rootOfflineCustodyPacket{}, err
	}
	return packet, nil
}

// The online action owner can request only the independently approved original.
// It receives a retained signature or a pending result, never a new signing effect.
func (self *rootOfflineCustody) signOnce(ctx context.Context, action rootAction) ([]byte, error) {
	if err := action.validate(); err != nil {
		return nil, err
	}
	return self.recoverSignature(ctx, action.RequestHash)
}

// Missing receipts are unknown, including after a lost offline-device response.
// No generic not-found can reopen rootActionOwner's signing allowance.
func (self *rootOfflineCustody) recoverSignature(ctx context.Context, requestHash string) ([]byte, error) {
	if err := self.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-self.ownerCh }()
	record, err := self.load()
	if err != nil {
		return nil, err
	}
	if requestHash != record.Packet.Action.RequestHash {
		return nil, errors.New("root offline custody request differs from its lifetime intent")
	}
	if record.Phase != "signed" {
		return nil, errRootOfflineSignatureRequired
	}
	signature, _ := hex.DecodeString(record.Signature)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return signature, nil
}

// Verify and sync before publishing a signature to an online action owner.
// Reimporting the exact receipt is idempotent; another valid signature for the
// same native payload cannot replace already retained extrinsic bytes.
func (self *rootOfflineCustody) importSignature(ctx context.Context, receipt rootOfflineSignature) error {
	if err := self.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-self.ownerCh }()
	record, err := self.load()
	if err != nil {
		return err
	}
	if receipt.Schema != rootOfflineSignatureSchema || receipt.PacketHash != record.Packet.ContentHash {
		return errors.New("root offline signature names another packet or schema")
	}
	signature, err := rootOfflineSignatureBytes(receipt.Signature)
	if err != nil {
		return err
	}
	raw, err := record.Packet.Action.signed(signature)
	if err != nil {
		return err
	}
	if record.Phase == "signed" {
		if record.Signature != receipt.Signature {
			return errors.New("root offline custody already retained different signature bytes")
		}
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	record.Phase, record.Signature, record.ExtrinsicHash = "signed", receipt.Signature, rootExtrinsicHash(raw)
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := self.store.save(record); err != nil {
		if !mainnetDurableAdmissionPending(err) {
			self.poisoned, self.failure = true, err
		}
		return err
	}
	return ctx.Err()
}
