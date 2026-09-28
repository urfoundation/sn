// One private journal owns the original EVM signature and finite send allowance.
// A completed marker never permits a missing journal to become fresh authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const evmActionStateSchema = "urnetwork-mainnet-evm-action-state-v1"
const evmVaultActionStateSchema = "urnetwork-mainnet-evm-vault-state-v1"
const evmCoordinatorActionStateSchema = "urnetwork-mainnet-evm-coordinator-state-v1"
const evmEscrowActionStateSchema = "urnetwork-mainnet-evm-escrow-state-v1"
const evmProxyActionStateSchema = "urnetwork-mainnet-evm-proxy-state-v1"

// Receipt facts are reauthenticated on every online resume. Their retained hash
// detects accidental corruption; it is neither consensus nor external custody.
type evmActionRecord struct {
	Schema          string            `json:"schema"`
	ConfigHash      string            `json:"config_hash"`
	Signed          string            `json:"signed_transaction,omitempty"`
	TransactionHash string            `json:"transaction_hash,omitempty"`
	Attempts        uint8             `json:"attempts"`
	ScanNumber      uint64            `json:"scan_number"`
	ScanHash        string            `json:"scan_hash"`
	Receipt         *evmCreateReceipt `json:"receipt,omitempty"`
	PredecessorHash string            `json:"predecessor_hash,omitempty"`
	ContentHash     string            `json:"content_hash"`
}

// The original reserve schema and content hash remain byte-for-byte compatible.
func (self evmActionRecord) validate(config evmPhaseConfig) error {
	return self.validateForAction(config, 0)
}

// Every retained signature belongs to its selected action in the same approval.
// Each later journal seals the completed predecessor custody it consumed.
func (self evmActionRecord) validateForAction(config evmPhaseConfig, actionIndex int) error {
	claimed := self.ContentHash
	self.ContentHash = ""
	p := config.Plan
	schema := evmActionStateSchema
	if actionIndex == 1 {
		schema = evmVaultActionStateSchema
	}
	if actionIndex == 2 {
		schema = evmCoordinatorActionStateSchema
	}
	if actionIndex == 3 {
		schema = evmEscrowActionStateSchema
	}
	if actionIndex == 4 {
		schema = evmProxyActionStateSchema
	}
	if actionIndex < 0 || actionIndex > 4 || actionIndex >= len(p.Actions) || actionIndex == 0 && self.PredecessorHash != "" || actionIndex > 0 && !planSha256(self.PredecessorHash) {
		return errors.New("EVM journal action or prerequisite differs")
	}
	if self.Schema != schema || self.ConfigHash != rootObjectHash(config) || claimed != rootObjectHash(self) || self.Attempts > p.MaximumAttempts || self.ScanNumber < p.StartNativeNumber || !rootCanonicalHash(self.ScanHash) || self.ScanNumber == p.StartNativeNumber && self.ScanHash != p.StartNativeHash {
		return errors.New("EVM journal identity, continuity or allowance differs")
	}
	if self.Signed == "" {
		if self.TransactionHash != "" || self.Attempts != 0 || self.Receipt != nil || self.ScanNumber != p.StartNativeNumber {
			return errors.New("unsigned EVM journal contains advanced state")
		}
		return nil
	}
	raw, err := rootReceiptHex(self.Signed, 128*1024)
	if err != nil {
		return err
	}
	tx, err := p.Actions[actionIndex].signed(raw)
	if err != nil || tx.Hash().Hex() != self.TransactionHash {
		return errors.Join(errors.New("retained EVM signature differs"), err)
	}
	if self.Receipt != nil && (self.Receipt.TransactionHash != self.TransactionHash || !rootCanonicalHash(self.Receipt.NativeHash) || !rootCanonicalHash(self.Receipt.BlockHash) || self.Receipt.NativeNumber <= p.StartNativeNumber || self.Receipt.Status > 1) {
		return errors.New("retained EVM receipt is incomplete")
	}
	if self.Receipt != nil && (actionIndex != 3 || self.Receipt.Status == 0) && (self.Receipt.RegistrationHash != "" || self.Receipt.EscrowUid != 0 || self.Receipt.EscrowLogIndex != 0) {
		return errors.New("non-registration outcome contains escrow receipt fields")
	}
	return nil
}

// The action owner serializes these methods. Failed publication poisons that
// owner; reopening validates whichever complete file survived the interruption.
type evmActionStorage interface {
	load() (evmActionRecord, error)
	save(evmActionRecord) error
}

// A local flock is an instance fence, not a claim of distributed key exclusivity.
// Close is called only after the action owner joins all operations.
type evmActionStore struct {
	config          evmPhaseConfig
	actionIndex     int
	predecessorHash string
	path            string
	lock            *os.File
	syncDirectory   func(*os.File) error
}

// Initial claim recovery is limited to the exact untouched prepared record.
// The complete marker is synced before signed bytes can ever be retained.
func openEvmActionStore(config evmPhaseConfig, create bool, claimHook func(string) error) (*evmActionStore, error) {
	return openEvmSelectedActionStore(config, 0, "", create, claimHook)
}

// The caller holds the reserve lock through this store's lifetime. The vault
// marker and state bind exactly that successful prerequisite, including attempts.
func openEvmVaultActionStore(plan evmCreatePlan, reserve evmActionRecord, create bool, claimHook func(string) error) (*evmActionStore, error) {
	if err := plan.validateSelection(); err != nil {
		return nil, err
	}
	if plan.ActionIndex != 1 {
		return nil, errors.New("vault custody requires explicit vault selection")
	}
	if err := validateEvmReservePrerequisite(*plan.Reserve, reserve); err != nil {
		return nil, err
	}
	return openEvmSelectedActionStore(plan.Config, 1, rootObjectHash(reserve), create, claimHook)
}

// The vault hash transitively seals its reserve; all three original records
// remain required, under their existing locks, for every coordinator operation.
func openEvmCoordinatorActionStore(plan evmCreatePlan, reserve, vault evmActionRecord, create bool, claimHook func(string) error) (*evmActionStore, error) {
	if err := plan.validateSelection(); err != nil {
		return nil, err
	}
	if plan.ActionIndex != 2 {
		return nil, errors.New("coordinator custody requires explicit coordinator selection")
	}
	plan.Prerequisites = []evmActionRecord{reserve, vault}
	if err := validateEvmCreatePrerequisite(plan, evmActionRecord{PredecessorHash: rootObjectHash(vault)}); err != nil {
		return nil, err
	}
	return openEvmSelectedActionStore(plan.Config, 2, rootObjectHash(vault), create, claimHook)
}

// The coordinator record seals both older ancestors. Its hash is immutable
// while the fourth journal owns its original signature and attempt allowance.
func openEvmEscrowActionStore(plan evmCreatePlan, reserve, vault, coordinator evmActionRecord, create bool, claimHook func(string) error) (*evmActionStore, error) {
	if err := plan.validateSelection(); err != nil {
		return nil, err
	}
	if plan.ActionIndex != 3 {
		return nil, errors.New("escrow custody requires explicit registration selection")
	}
	plan.Prerequisites = []evmActionRecord{reserve, vault, coordinator}
	if err := validateEvmCreatePrerequisite(plan, evmActionRecord{PredecessorHash: rootObjectHash(coordinator)}); err != nil {
		return nil, err
	}
	return openEvmSelectedActionStore(plan.Config, 3, rootObjectHash(coordinator), create, claimHook)
}

// Escrow completion seals all four predecessors before the proxy opens custody.
func openEvmProxyActionStore(plan evmCreatePlan, reserve, vault, coordinator, escrow evmActionRecord, create bool, claimHook func(string) error) (*evmActionStore, error) {
	if err := plan.validateSelection(); err != nil {
		return nil, err
	}
	if plan.ActionIndex != 4 {
		return nil, errors.New("proxy custody requires explicit initialized CREATE selection")
	}
	plan.Prerequisites = []evmActionRecord{reserve, vault, coordinator, escrow}
	if err := validateEvmCreatePrerequisite(plan, evmActionRecord{PredecessorHash: rootObjectHash(escrow)}); err != nil {
		return nil, err
	}
	return openEvmSelectedActionStore(plan.Config, 4, rootObjectHash(escrow), create, claimHook)
}

// All five actions share publication and initial-claim recovery mechanics with
// separate schemas/paths/markers; a completed marker never refreshes budget.
func openEvmSelectedActionStore(config evmPhaseConfig, actionIndex int, predecessorHash string, create bool, claimHook func(string) error) (*evmActionStore, error) {
	if err := errors.Join(config.validate(), bootstrapRootDirectory(config.Plan.RunDirectory)); err != nil {
		return nil, err
	}
	if actionIndex < 0 || actionIndex > 4 || actionIndex >= len(config.Plan.Actions) || actionIndex == 0 && predecessorHash != "" || actionIndex > 0 && !planSha256(predecessorHash) {
		return nil, errors.New("EVM store action or prerequisite differs")
	}
	name, schema := evmCreateStateFile, evmActionStateSchema
	marker := rootObjectHash(config) + "\n"
	if actionIndex == 1 {
		name, schema = evmVaultCreateStateFile, evmVaultActionStateSchema
	}
	if actionIndex == 2 {
		name, schema = evmCoordinatorCreateStateFile, evmCoordinatorActionStateSchema
	}
	if actionIndex == 3 {
		name, schema = evmEscrowRegisterStateFile, evmEscrowActionStateSchema
	}
	if actionIndex == 4 {
		name, schema = evmProxyCreateStateFile, evmProxyActionStateSchema
	}
	if actionIndex > 0 {
		marker = rootObjectHash(struct{ ConfigHash, ActionId, PredecessorHash string }{ConfigHash: rootObjectHash(config), ActionId: config.Plan.Actions[actionIndex].Id, PredecessorHash: predecessorHash}) + "\n"
	}
	path := filepath.Join(config.Plan.RunDirectory, name)
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		for _, name := range []string{path, path + ".lock"} {
			if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				return nil, errors.Join(errors.New("EVM apply requires unused paths; use resume for retained custody"), err)
			}
		}
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path+".lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	store := &evmActionStore{config: copyEvmPhaseConfig(config), actionIndex: actionIndex, predecessorHash: predecessorHash, path: path, lock: os.NewFile(uintptr(fd), path+".lock")}
	success := false
	defer func() {
		if !success {
			store.close()
		}
	}()
	info, err := store.lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("EVM marker is not a private regular file"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("EVM journal already has an owner"), err)
	}
	if create {
		written, err := store.lock.WriteString(marker)
		if written != len(marker) && err == nil {
			err = io.ErrShortWrite
		}
		if err := errors.Join(err, store.lock.Sync(), store.syncParent()); err != nil {
			return nil, err
		}
		if claimHook != nil {
			if err := claimHook("marker-synced"); err != nil {
				return nil, err
			}
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(store.lock, int64(len(marker)+len(bootstrapRootClaimComplete)+1)))
		if err != nil {
			return nil, err
		}
		if string(raw) == marker+bootstrapRootClaimComplete {
			if _, err := store.load(); err != nil {
				return nil, err
			}
			success = true
			return store, nil
		}
		if string(raw) != marker {
			return nil, errors.New("EVM marker differs from independent phase approval")
		}
	}
	record, err := store.load()
	if errors.Is(err, os.ErrNotExist) {
		record = evmActionRecord{Schema: schema, ConfigHash: rootObjectHash(config), ScanNumber: config.Plan.StartNativeNumber, ScanHash: config.Plan.StartNativeHash, PredecessorHash: predecessorHash}
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			return nil, err
		}
	} else if err != nil || record.Signed != "" {
		return nil, errors.Join(errors.New("EVM interrupted claim has corrupt or advanced custody"), err)
	}
	if claimHook != nil {
		if err := claimHook("record-synced"); err != nil {
			return nil, err
		}
	}
	written, err := store.lock.WriteAt([]byte(bootstrapRootClaimComplete), int64(len(marker)))
	if written != len(bootstrapRootClaimComplete) && err == nil {
		err = io.ErrShortWrite
	}
	if err := errors.Join(err, store.lock.Sync()); err != nil {
		return nil, err
	}
	success = true
	return store, nil
}

// Idempotent close releases the fence and disables all future reads and writes.
func (self *evmActionStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := self.lock.Close()
	self.lock = nil
	return err
}

// A malformed or missing completed journal is never reconstructed from a plan.
func (self *evmActionStore) load() (evmActionRecord, error) {
	var record evmActionRecord
	if self.lock == nil {
		return record, errors.New("EVM journal is closed")
	}
	raw, _, err := readBootstrapRootFile(context.Background(), self.path, 512*1024)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	if record.PredecessorHash != self.predecessorHash {
		return record, errors.New("EVM journal reserve custody changed")
	}
	return record, record.validateForAction(self.config, self.actionIndex)
}

// Atomic rename is followed by directory sync before acknowledging custody.
func (self *evmActionStore) save(record evmActionRecord) error {
	if self.lock == nil {
		return errors.New("EVM journal is closed")
	}
	if record.PredecessorHash != self.predecessorHash {
		return errors.New("EVM publication changes reserve custody")
	}
	if err := record.validateForAction(self.config, self.actionIndex); err != nil {
		return err
	}
	if info, err := os.Lstat(self.path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("EVM journal destination is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > 512*1024 {
		return errors.Join(errors.New("EVM journal exceeds its bound"), err)
	}
	file, err := os.CreateTemp(self.config.Plan.RunDirectory, ".sn-mainnet-evm-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	written, err := file.Write(append(raw, '\n'))
	if written != len(raw)+1 && err == nil {
		err = io.ErrShortWrite
	}
	if err := errors.Join(err, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), self.path); err != nil {
		return err
	}
	return self.syncParent()
}

// Scoped fault injection exposes the durability boundary without time sleeps.
func (self *evmActionStore) syncParent() error {
	directory, err := os.Open(self.config.Plan.RunDirectory)
	if err != nil {
		return err
	}
	syncDirectory := self.syncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(directory), directory.Close())
}
