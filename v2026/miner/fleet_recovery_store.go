// Mainnet fleet writes retain signed liabilities in a bounded, exclusive local
// journal. Custody signatures cover both the original intent and its outcome;
// they attest local observations, never create runtime approval.
package miner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vedhavyas/go-subkey/v2/sr25519"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const fleetRecoverySchema = "urnetwork-mainnet-fleet-recovery-v1"
const fleetRecoveryMaxBytes = 16 * 1024 * 1024
const fleetRecoveryMaxRecords = 256
const fleetRecoveryMaxRaw = 64 * 1024

// The stable operation identity omits mutable approval and fee choices. A
// pending record also blocks different intents until its outcome is resolved.
type fleetRecoveryIntent struct {
	Action    string   `json:"action"`
	Genesis   string   `json:"genesis"`
	Manifest  []byte   `json:"manifest"`
	ClientId  [16]byte `json:"client_id"`
	FromEpoch uint64   `json:"from_epoch"`
	ToEpoch   uint64   `json:"to_epoch"`
}

// Exact original approval bytes, transaction and signing checkpoint survive
// every failure. A terminal proof is stored only after canonical readback.
type fleetRecoveryRecord struct {
	Schema          string                         `json:"schema"`
	Id              string                         `json:"id"`
	Intent          fleetRecoveryIntent            `json:"intent"`
	Authority       []byte                         `json:"authority"`
	AuthoritySha256 string                         `json:"authority_sha256"`
	NativeSigner    [32]byte                       `json:"native_signer"`
	EvmSigner       common.Address                 `json:"evm_signer"`
	Nonce           uint64                         `json:"nonce"`
	Raw             []byte                         `json:"raw"`
	TxHash          string                         `json:"tx_hash"`
	StartHash       types.Hash                     `json:"start_hash"`
	StartNumber     uint64                         `json:"start_number"`
	ScanNumber      uint64                         `json:"scan_number,omitempty"`
	ScanHash        types.Hash                     `json:"scan_hash,omitempty"`
	ScanProof       string                         `json:"scan_proof,omitempty"`
	Stage           string                         `json:"stage"`
	NativeReceipt   *crv4.FinalizedExtrinsic       `json:"native_receipt,omitempty"`
	EvmReceipt      *ethtypes.Receipt              `json:"evm_receipt,omitempty"`
	Mapping         *crv4.EVMCheckpointObservation `json:"mapping,omitempty"`
	Outcome         string                         `json:"outcome,omitempty"`
	Succeeded       bool                           `json:"succeeded,omitempty"`
	Signature       []byte                         `json:"signature"`
}

// This semantic version is signed with the complete original recovery record.
// Legacy cursors predate body commitments and must be rescanned once.
const fleetRecoveryNativeScanProof = "urnetwork-native-receipt-absence-v1"

// Only this owner signs checkpoint updates. The keys are never serialized.
type fleetRecoverySigner struct {
	native *crv4.Keypair
	evm    *ecdsa.PrivateKey
}

// The current actor signs the complete inventory as well as each record;
// removing another record cannot leave a valid-looking shortened journal.
type fleetRecoveryJournal struct {
	Schema    string                 `json:"schema"`
	Records   []*fleetRecoveryRecord `json:"records"`
	Signature []byte                 `json:"signature"`
}

// Inventory signatures are distinct from signatures over individual records.
func (self fleetRecoveryJournal) digest() [32]byte {
	self.Signature = nil
	raw, _ := json.Marshal(self)
	return sha256.Sum256(append([]byte(fleetRecoverySchema+"/inventory\x00"), raw...))
}

// Uses only the durable actor identity, without loading any private key.
func fleetRecoveryVerify(native [32]byte, evm common.Address, digest [32]byte, signature []byte) bool {
	if native != ([32]byte{}) && evm == (common.Address{}) {
		public, err := (sr25519.Scheme{}).FromPublicKey(native[:])
		return err == nil && public.Verify(digest[:], signature)
	}
	if native == ([32]byte{}) && evm != (common.Address{}) {
		public, err := crypto.SigToPub(digest[:], signature)
		return err == nil && crypto.PubkeyToAddress(*public) == evm
	}
	return false
}

// A directory descriptor pins the namespace and owns the cross-process lock.
// No method is concurrent; callers hold the owner for the whole operation.
type fleetRecoveryStore struct {
	directory *os.File
	guard     *durablepath.Directory
	head      *durablehead.Owner
	records   []*fleetRecoveryRecord
	signer    fleetRecoverySigner
	// Deterministic durability barriers belong to this owner, never globals.
	checkpoint func(string) error
}

// Domain-separated canonical bytes define the immutable logical operation.
func (self fleetRecoveryIntent) id() string {
	raw, _ := json.Marshal(self)
	digest := sha256.Sum256(append([]byte(fleetRecoverySchema+"/intent\x00"), raw...))
	return hex.EncodeToString(digest[:])
}

// The custody signature excludes only itself, including all terminal proof.
func (self *fleetRecoveryRecord) digest() [32]byte {
	copy := *self
	copy.Signature = nil
	raw, _ := json.Marshal(copy)
	return sha256.Sum256(append([]byte(fleetRecoverySchema+"/record\x00"), raw...))
}

// Authenticates the original authority without consulting mutable CLI files.
func (self *fleetRecoveryRecord) authority() (*fleetMainnetRuntimeAuthority, *protocol.FleetManifest, error) {
	manifest, err := protocol.ParseFleetManifest(self.Intent.Manifest)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(self.Authority)
	if len(self.Authority) > fleetMainnetAuthorityLimit || hex.EncodeToString(digest[:]) != self.AuthoritySha256 {
		return nil, nil, errors.New("fleet recovery original approval digest differs")
	}
	var authority fleetMainnetRuntimeAuthority
	if err := fleetRecoveryDecode(self.Authority, &authority); err != nil {
		return nil, nil, err
	}
	if err := authority.validate(manifest); err != nil || authority.GenesisHash != self.Intent.Genesis {
		return nil, nil, errors.Join(errors.New("fleet recovery original approval scope differs"), err)
	}
	authority.document = append([]byte(nil), self.Authority...)
	return &authority, manifest, nil
}

// Strict decoding refuses ambiguous journal fields or appended values.
func fleetRecoveryDecode(raw []byte, target any) error {
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// Verifies bounded exact transaction custody before any network or signing.
func (self *fleetRecoveryRecord) validate() error {
	if self == nil || self.Schema != fleetRecoverySchema || self.Id != self.Intent.id() || len(self.Raw) == 0 || len(self.Raw) > fleetRecoveryMaxRaw || len(self.Intent.Manifest) > 256*1024 || self.StartHash == (types.Hash{}) || self.StartNumber == 0 {
		return errors.New("fleet recovery record identity or bounds differ")
	}
	if self.Stage != "prepared" && self.Stage != "may_have_sent" && self.Stage != "finalized" {
		return errors.New("fleet recovery record stage is invalid")
	}
	if (self.ScanNumber == 0) != (self.ScanHash == (types.Hash{})) || (self.ScanNumber != 0 && self.ScanNumber < self.StartNumber) {
		return errors.New("fleet recovery scan checkpoint differs")
	}
	if self.ScanProof != "" && (self.ScanProof != fleetRecoveryNativeScanProof || self.ScanNumber <= self.StartNumber || self.Intent.Action != "register" && self.Intent.Action != "publish") {
		return errors.New("fleet recovery native scan proof scope differs")
	}
	if (self.Stage == "finalized") != (self.Outcome != "") || (self.Stage != "finalized" && (self.NativeReceipt != nil || self.EvmReceipt != nil || self.Mapping != nil || self.Succeeded)) {
		return errors.New("fleet recovery terminal proof differs from its stage")
	}
	authority, manifest, err := self.authority()
	if err != nil {
		return err
	}
	digest := self.digest()
	switch self.Intent.Action {
	case "register", "publish":
		if self.EvmSigner != (common.Address{}) || self.NativeSigner == ([32]byte{}) || self.Nonce > uint64(^uint32(0)) || snchain.ExtrinsicHash(self.Raw).Hex() != self.TxHash {
			return errors.New("fleet recovery native transaction identity differs")
		}
		if self.Intent.Action == "publish" && self.NativeSigner != manifest.Hotkey {
			return errors.New("fleet recovery publication signer differs")
		}
		public, err := (sr25519.Scheme{}).FromPublicKey(self.NativeSigner[:])
		if err != nil || !public.Verify(digest[:], self.Signature) {
			return errors.New("fleet recovery native custody signature differs")
		}
		if self.Stage == "finalized" && (self.NativeReceipt == nil || self.NativeReceipt.ExtrinsicHash.Hex() != self.TxHash || self.NativeReceipt.BlockNumber <= self.StartNumber || self.NativeReceipt.BlockHash == (types.Hash{}) || self.EvmReceipt != nil || self.Mapping != nil) {
			return errors.New("fleet recovery native terminal evidence differs")
		}
	case "bind", "revoke":
		var tx ethtypes.Transaction
		if self.NativeSigner != ([32]byte{}) || self.EvmSigner == (common.Address{}) || tx.UnmarshalBinary(self.Raw) != nil || !tx.Protected() || !tx.ChainId().IsUint64() || tx.ChainId().Uint64() != authority.EvmChainId || tx.To() == nil || *tx.To() != common.HexToAddress(authority.Coordinator) || tx.Value().Sign() != 0 || tx.Nonce() != self.Nonce || tx.Hash().Hex() != self.TxHash {
			return errors.New("fleet recovery EVM transaction identity differs")
		}
		from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), &tx)
		public, signatureErr := crypto.SigToPub(digest[:], self.Signature)
		if err != nil || from != self.EvmSigner || signatureErr != nil || crypto.PubkeyToAddress(*public) != from {
			return errors.New("fleet recovery EVM custody signature differs")
		}
		if self.Stage == "finalized" && (self.NativeReceipt != nil || self.EvmReceipt == nil || self.Mapping == nil || self.EvmReceipt.TxHash != tx.Hash() || self.EvmReceipt.BlockNumber == nil || !self.EvmReceipt.BlockNumber.IsUint64() || self.Mapping.Query.EVMNumber != self.EvmReceipt.BlockNumber.Uint64() || common.Hash(self.Mapping.Query.EVMHash) != self.EvmReceipt.BlockHash || self.Mapping.Query.NativeNumber <= self.StartNumber || self.Mapping.Query.GenesisHash.Hex() != authority.GenesisHash) {
			return errors.New("fleet recovery EVM terminal evidence differs")
		}
	default:
		return errors.New("fleet recovery action is unknown")
	}
	return nil
}

// Signing updates the local custody proof, not the on-chain transaction.
func (self fleetRecoverySigner) sign(record *fleetRecoveryRecord) error {
	digest := record.digest()
	var err error
	if self.native != nil && self.native.PublicKey() == record.NativeSigner && self.evm == nil {
		record.Signature, err = self.native.Sign(digest[:])
	} else if self.evm != nil && crypto.PubkeyToAddress(self.evm.PublicKey) == record.EvmSigner && self.native == nil {
		record.Signature, err = crypto.Sign(digest[:], self.evm)
	} else {
		return errors.New("fleet recovery original signer differs")
	}
	return err
}

// Opens only private regular files relative to the pinned private directory.
func (self *fleetRecoveryStore) file(name string, flags int) (*os.File, error) {
	if err := self.requireOwner(); err != nil {
		return nil, err
	}
	file, err := fleetRecoveryOpenFile(self.directory, name, flags)
	if err != nil {
		return nil, err
	}
	if err := self.requireOwner(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// The actual persistent store never accepts absent policy or a lost generation.
func (self *fleetRecoveryStore) requireOwner() error {
	if self == nil || self.guard == nil || self.directory == nil {
		return errors.New("fleet recovery directory owner is absent")
	}
	if self.head != nil {
		return self.head.Check()
	}
	return self.guard.CheckRead()
}

// Journal creation precedes signing; an existing owner never silently resets
// a missing or corrupt journal. An interrupted replacement remains recoverable.
func openFleetRecoveryStore(ctx context.Context) (*fleetRecoveryStore, error) {
	if err := fleetRecoveryPlatformSupported(); err != nil {
		return nil, err
	}
	stateDir, err := providerStateDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(stateDir, "fleet-mainnet-recovery")
	guard, err := durablepath.OpenOwnerLocal(ctx, dir, durablevolume.ReadWrite, false)
	if err != nil {
		return nil, err
	}
	directory := guard.File()
	self := &fleetRecoveryStore{directory: directory, guard: guard}
	fail := func(err error) (*fleetRecoveryStore, error) { self.close(); return nil, err }
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fail(errors.Join(errors.New("fleet recovery directory must be private"), err))
	}
	if err := fleetRecoveryLock(directory); err != nil {
		return fail(fmt.Errorf("fleet recovery already has an exclusive owner: %w", err))
	}
	// Pre-anchor legacy candidates need explicit reviewed provisioning; a new
	// opener cannot guess their relationship to the current signed inventory.
	if candidate, candidateErr := self.file("journal.next", os.O_RDONLY); candidateErr == nil {
		return fail(errors.Join(durablehead.ErrUncertain, errors.New("legacy fleet candidate requires explicit custody adoption"), candidate.Close()))
	} else if !errors.Is(candidateErr, os.ErrNotExist) {
		return fail(candidateErr)
	}
	spec := durablehead.Spec{Kind: "fleet-recovery", Name: "journal.json", MaximumBytes: fleetRecoveryMaxBytes, AuxiliaryNames: []string{"initialized"}}
	self.head, err = durablehead.Open(ctx, guard, directory, spec)
	// A failed opener has no running users. The existing exclusive lock stays
	// held across one bounded exact-byte recovery; no transaction is resent.
	if errors.Is(err, durablehead.ErrUncertain) {
		self.head, err = durablehead.Reconcile(ctx, guard, directory, spec)
	}
	if err != nil {
		return fail(err)
	}
	read := func(raw []byte) ([]*fleetRecoveryRecord, error) {
		var journal fleetRecoveryJournal
		if len(raw) > fleetRecoveryMaxBytes {
			return nil, errors.New("fleet recovery journal exceeds byte bound")
		}
		if err := fleetRecoveryDecode(raw, &journal); err != nil {
			return nil, err
		}
		if journal.Schema != fleetRecoverySchema {
			return nil, errors.New("fleet recovery journal schema differs")
		}
		records := journal.Records
		if len(records) > fleetRecoveryMaxRecords {
			return nil, errors.New("fleet recovery journal exceeds record bound")
		}
		seen := map[string]bool{}
		pending := 0
		for _, record := range records {
			if err := record.validate(); err != nil {
				return nil, err
			}
			if seen[record.Id] {
				return nil, errors.New("fleet recovery repeats an intent")
			}
			seen[record.Id] = true
			if record.Stage != "finalized" {
				pending++
			}
		}
		if pending > 1 {
			return nil, errors.New("fleet recovery has multiple pending owners")
		}
		if len(records) != 0 {
			last := records[len(records)-1]
			if !fleetRecoveryVerify(last.NativeSigner, last.EvmSigner, journal.digest(), journal.Signature) {
				return nil, errors.New("fleet recovery complete inventory signature differs")
			}
		} else if len(journal.Signature) != 0 {
			return nil, errors.New("fleet recovery empty inventory has a signature")
		}
		return records, nil
	}
	raw, present, err := self.head.Read()
	if err != nil {
		return fail(err)
	}
	if present {
		self.records, err = read(raw)
		if err != nil {
			return fail(err)
		}
	} else if err := self.save(); err != nil {
		return fail(err)
	}
	if err := self.completeMarker(); err != nil {
		return fail(err)
	}
	if err := self.requireOwner(); err != nil {
		return fail(err)
	}
	return self, nil
}

// Closing the directory releases the process-owned lock.
func (self *fleetRecoveryStore) close() error {
	if self == nil || self.guard == nil {
		return nil
	}
	guard := self.guard
	self.guard, self.directory = nil, nil
	err := self.head.Close()
	self.head = nil
	return errors.Join(err, guard.Close())
}

// An interrupted candidate can complete an operation but cannot roll back
// immutable transaction custody, a finalized outcome or an archive cursor.
func fleetRecoveryAdvance(old, next *fleetRecoveryRecord) error {
	oldRaw, _ := json.Marshal(old)
	nextRaw, _ := json.Marshal(next)
	if old.Stage == "finalized" {
		if !bytes.Equal(oldRaw, nextRaw) {
			return errors.New("fleet recovery finalized record replacement refused")
		}
		return nil
	}
	before, after := *old, *next
	before.Stage, after.Stage = "", ""
	before.Signature, after.Signature = nil, nil
	before.NativeReceipt, after.NativeReceipt = nil, nil
	before.EvmReceipt, after.EvmReceipt = nil, nil
	before.Mapping, after.Mapping = nil, nil
	before.Outcome, after.Outcome = "", ""
	before.Succeeded, after.Succeeded = false, false
	before.ScanNumber, after.ScanNumber = 0, 0
	before.ScanHash, after.ScanHash = types.Hash{}, types.Hash{}
	before.ScanProof, after.ScanProof = "", ""
	oldRaw, _ = json.Marshal(before)
	nextRaw, _ = json.Marshal(after)
	// A legacy cursor never proved committed absence. Its one-way semantic
	// migration may replace that cursor after rescanning the original attempt.
	proofUpgrade := old.ScanProof == "" && next.ScanProof == fleetRecoveryNativeScanProof && (old.Intent.Action == "register" || old.Intent.Action == "publish") && next.ScanNumber > next.StartNumber
	if !bytes.Equal(oldRaw, nextRaw) || (old.Stage == "may_have_sent" && next.Stage == "prepared") ||
		(old.ScanProof != next.ScanProof && !proofUpgrade) ||
		(!proofUpgrade && (next.ScanNumber < old.ScanNumber || (next.ScanNumber == old.ScanNumber && next.ScanHash != old.ScanHash))) {
		return errors.New("fleet recovery immutable record replacement refused")
	}
	return nil
}

// Persistence never authorizes a send until file and directory fsync finish.
// A failed or interrupted save leaves its exact candidate for restart recovery.
func (self *fleetRecoveryStore) save() error {
	if self == nil || self.head == nil {
		return errors.New("fleet recovery checkpoint is absent")
	}
	if err := self.head.CheckWrite(); err != nil {
		return err
	}
	journal := fleetRecoveryJournal{Schema: fleetRecoverySchema, Records: self.records}
	if len(self.records) != 0 {
		digest := journal.digest()
		var err error
		if self.signer.native != nil {
			journal.Signature, err = self.signer.native.Sign(digest[:])
		} else if self.signer.evm != nil {
			journal.Signature, err = crypto.Sign(digest[:], self.signer.evm)
		} else {
			return errors.New("fleet recovery inventory signer unavailable")
		}
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(journal)
	if err != nil || len(raw) > fleetRecoveryMaxBytes || len(self.records) > fleetRecoveryMaxRecords {
		return errors.Join(errors.New("fleet recovery journal capacity exhausted"), err)
	}
	at := func(stage string) error {
		if self.checkpoint != nil {
			return self.checkpoint(stage)
		}
		return nil
	}
	if err := self.head.PublishWithHooks(raw, durablehead.PublicationHooks{
		AfterFileSync:      func() error { return at("file-synced") },
		AfterRename:        func() error { return at("renamed") },
		AfterDirectorySync: func(*os.File) error { return at("directory-synced") },
	}); err != nil {
		return err
	}
	if err := self.completeMarker(); err != nil {
		return err
	}
	return self.head.CheckWrite()
}

// A different logical intent cannot escape an unresolved signed liability.
func (self *fleetRecoveryStore) find(intent fleetRecoveryIntent) (*fleetRecoveryRecord, error) {
	if err := self.requireOwner(); err != nil {
		return nil, err
	}
	var found *fleetRecoveryRecord
	for _, record := range self.records {
		if record.Id == intent.id() {
			found = record
		}
		if record.Stage != "finalized" && record.Id != intent.id() {
			return nil, fmt.Errorf("fleet recovery unresolved %s transaction %s blocks a new intent", record.Intent.Action, record.TxHash)
		}
	}
	if found == nil && len(self.records) >= fleetRecoveryMaxRecords {
		return nil, errors.New("fleet recovery journal record capacity exhausted")
	}
	return found, nil
}

// Publishes a new signed transaction or a signed outcome without replacing
// its immutable authority, transaction, nonce, signer, or signing checkpoint.
func (self *fleetRecoveryStore) put(record *fleetRecoveryRecord, signer fleetRecoverySigner) error {
	if self == nil || self.head == nil {
		return errors.New("fleet recovery checkpoint is absent")
	}
	if err := self.head.CheckWrite(); err != nil {
		return err
	}
	if err := signer.sign(record); err != nil {
		return err
	}
	if err := record.validate(); err != nil {
		return err
	}
	self.signer = signer
	for i, old := range self.records {
		if old.Id != record.Id {
			continue
		}
		if old.Stage == "finalized" {
			return errors.New("fleet recovery finalized record replacement refused")
		}
		if err := fleetRecoveryAdvance(old, record); err != nil {
			return err
		}
		next := append([]*fleetRecoveryRecord(nil), self.records...)
		next[i] = record
		return self.saveRecords(next)
	}
	if record.Stage != "prepared" || len(self.records) >= fleetRecoveryMaxRecords {
		return errors.New("fleet recovery new record stage or capacity differs")
	}
	if _, err := self.find(record.Intent); err != nil {
		return err
	}
	next := append(append([]*fleetRecoveryRecord(nil), self.records...), record)
	return self.saveRecords(next)
}

// Pre-admission failures do not advance process-local custody. Uncertain
// publication separately poisons the head, retaining actual disk evidence.
func (self *fleetRecoveryStore) saveRecords(next []*fleetRecoveryRecord) error {
	previous := self.records
	self.records = next
	if err := self.save(); err != nil {
		self.records = previous
		return err
	}
	return nil
}

// The original marker inode is never created or replaced. Only a complete
// authenticated retained inventory can finish its empty-to-signed grammar.
func (self *fleetRecoveryStore) completeMarker() error {
	var raw []byte
	if err := self.head.WithAuxiliary("initialized", false, func(file *os.File) error {
		var err error
		raw, err = io.ReadAll(io.LimitReader(file, 16))
		return err
	}); err != nil {
		return err
	}
	if len(raw) != 0 && string(raw) != "signed\n" {
		return errors.New("fleet initialized marker grammar differs")
	}
	if len(self.records) == 0 {
		if len(raw) != 0 {
			return errors.New("fleet initialized inventory was removed")
		}
		return nil
	}
	if len(raw) != 0 {
		return nil
	}
	return self.head.WithAuxiliary("initialized", true, func(file *os.File) error {
		n, err := file.WriteAt([]byte("signed\n"), 0)
		if n != len("signed\n") && err == nil {
			err = io.ErrShortWrite
		}
		return err
	})
}

// The current request and the retained owner independently authorize handoff.
func (self *fleetRecoveryStore) beforeExternal(ctx context.Context) error {
	if ctx == nil {
		return errors.New("fleet request context is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if self == nil || self.head == nil {
		return errors.New("fleet recovery checkpoint is absent")
	}
	return errors.Join(self.head.CheckWrite(), ctx.Err())
}
