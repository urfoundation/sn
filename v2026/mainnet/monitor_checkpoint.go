// Local checkpoint continuity records both finalized history and availability
// gaps. A checksum detects corruption; it is not independent chain approval.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/protocol"
)

const monitorCheckpointSchema = "urnetwork-mainnet-monitor-checkpoint-v3"
const maxMonitorCheckpointBytes = 2 * 1024 * 1024
const monitorCheckpointOutageSchema = "urnetwork-mainnet-monitor-checkpoint-v2"
const monitorCheckpointLegacySchema = "urnetwork-mainnet-monitor-checkpoint-v1"

// The checkpoint is local continuity evidence, not an approval or an
// independent attestation of the RPC route.
type monitorCheckpointRecord struct {
	Schema           string `json:"schema"`
	NativeChain      string `json:"native_chain"`
	GenesisHash      string `json:"genesis_hash"`
	EvmChainId       uint64 `json:"evm_chain_id"`
	FinalizedHash    string `json:"finalized_hash"`
	FinalizedAt      uint64 `json:"finalized_number"`
	LastProgressAt   string `json:"last_progress_at"`
	UnavailableSince string `json:"unavailable_since,omitempty"`
	LastSuccessAt    string `json:"last_success_at,omitempty"`
	ContentHash      string `json:"content_hash"`
}

type monitorCheckpointStore struct {
	path          string
	lock          *os.File
	directory     *monitorDirectory
	expected      identityExpectation
	syncDirectory func(*os.File) error
}

// A process owns one checkpoint for its entire monitoring lifetime. The lock
// prevents two monitors from alternately replacing the same finality history.
func openMonitorCheckpoint(path string, expected identityExpectation, contexts ...context.Context) (*monitorCheckpointStore, error) {
	// The borrowed-read ceiling does not enlarge the original physical owner.
	return openMonitorCheckpointProfile(path, expected, "mainnet-monitor-checkpoint", maxRpcReplyBytes, contexts...)
}

func openMonitorCheckpointProfile(path string, expected identityExpectation, kind string, maximum int, contexts ...context.Context) (*monitorCheckpointStore, error) {
	if err := validateMonitorCheckpointProfile(kind, maximum); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Base(path) == "." || expected.NativeChain == "" || !validHash(expected.GenesisHash) || expected.EvmChainId != mainnetEvmChainId {
		return nil, errors.New("checkpoint path or approved network identity is incomplete")
	}
	if len(contexts) == 0 {
		var err error
		path, err = resolveMonitorDestination(path)
		if err != nil {
			return nil, err
		}
	}
	directory, err := openMonitorDirectory(filepath.Dir(path), contexts)
	if err != nil {
		return nil, err
	}
	flags := syscall.O_RDWR
	if directory.guard == nil {
		flags |= syscall.O_CREAT
	}
	lock, err := directory.open(filepath.Base(path)+".lock", flags, 0600)
	if err != nil {
		return nil, monitorAdmissionFailure(fmt.Errorf("open checkpoint lock: %w", err), directory.close())
	}
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, monitorAdmissionFailure(errors.Join(errors.New("checkpoint lock is not a private regular file"), err), errors.Join(lock.Close(), directory.close()))
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, monitorAdmissionFailure(fmt.Errorf("checkpoint already has an owner: %w", err), errors.Join(lock.Close(), directory.close()))
	}
	if directory.guard != nil {
		name := filepath.Base(path)
		spec := durablehead.Spec{Kind: kind, Name: name, MaximumBytes: int64(maximum), LockName: name + ".lock", AuxiliaryNames: []string{name + ".lock"}}
		directory.head, err = durablehead.Open(directory.ctx, directory.guard, lock, spec)
		// A failed opener has no live users. One exact pending-byte recovery
		// runs under the same retained exclusive lock before observations start.
		if errors.Is(err, durablehead.ErrUncertain) {
			directory.head, err = durablehead.Reconcile(directory.ctx, directory.guard, lock, spec)
		}
		if err != nil {
			return nil, monitorAdmissionFailure(monitorCustodyError(err), errors.Join(lock.Close(), directory.close()))
		}
		directory.headName = name
	}
	return &monitorCheckpointStore{path: path, lock: lock, directory: directory, expected: expected}, nil
}

func (self *monitorCheckpointStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.lock.Close(), self.directory.close())
	self.lock = nil
	return err
}

func (self *monitorCheckpointStore) load() (*monitorState, error) {
	if self == nil || self.lock == nil {
		return nil, errors.New("monitor checkpoint store is closed")
	}
	if err := self.requireOwner(); err != nil {
		return nil, err
	}
	state := &monitorState{}
	raw, err := self.directory.read(filepath.Base(self.path), maxMonitorCheckpointBytes, true)
	if monitorCheckpointAbsent(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint cannot be read within 1 MiB: %w", err)
	}
	if len(raw) > maxMonitorCheckpointBytes {
		return nil, errors.New("checkpoint cannot be read within 1 MiB")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record monitorCheckpointRecord
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("checkpoint has trailing JSON")
	}
	if err := self.validate(record); err != nil {
		return nil, err
	}
	progressAt, _ := time.Parse(time.RFC3339Nano, record.LastProgressAt)
	state = &monitorState{lastHash: record.FinalizedHash, lastNumber: record.FinalizedAt, lastProgressAt: progressAt}
	if record.LastSuccessAt != "" {
		state.lastSuccessAt, _ = time.Parse(time.RFC3339Nano, record.LastSuccessAt)
	}
	if record.UnavailableSince != "" {
		state.unavailableSince, _ = time.Parse(time.RFC3339Nano, record.UnavailableSince)
	}
	return state, nil
}

// The retained lock and actual parent remain one admitted physical generation.
func (self *monitorCheckpointStore) requireOwner() error {
	if self == nil || self.lock == nil {
		return errors.New("monitor checkpoint store is closed")
	}
	if err := self.directory.check(); err != nil {
		return err
	}
	if self.directory.head != nil {
		// The shared head checked both actual named inodes and the protected
		// ancestry. A redundant pathname probe must not turn unavailable
		// observation into an invented custody mismatch.
		return self.directory.ctx.Err()
	}
	opened, openErr := self.lock.Stat()
	named, nameErr := self.directory.stat(filepath.Base(self.path) + ".lock")
	if err := errors.Join(openErr, nameErr); err != nil {
		return monitorNamedObservation(err)
	}
	if !named.Mode().IsRegular() || named.Mode().Perm()&0077 != 0 || !os.SameFile(opened, named) {
		return &monitorOutputOwnershipError{reason: "checkpoint lock changed"}
	}
	return nil
}

func (self *monitorCheckpointStore) validate(record monitorCheckpointRecord) error {
	if record.Schema != monitorCheckpointSchema && record.Schema != monitorCheckpointOutageSchema && record.Schema != monitorCheckpointLegacySchema ||
		record.Schema == monitorCheckpointLegacySchema && record.UnavailableSince != "" ||
		record.Schema != monitorCheckpointSchema && record.LastSuccessAt != "" ||
		record.NativeChain != self.expected.NativeChain || !strings.EqualFold(record.GenesisHash, self.expected.GenesisHash) || record.EvmChainId != self.expected.EvmChainId {
		return errors.New("checkpoint identity or finalized position differs")
	}
	if record.FinalizedHash == "" && record.FinalizedAt == 0 && record.LastProgressAt == "" {
		if record.Schema != monitorCheckpointSchema || record.UnavailableSince == "" || record.LastSuccessAt != "" {
			return errors.New("checkpoint without finality requires an initial read outage")
		}
	} else {
		if !validHash(record.FinalizedHash) || record.FinalizedAt == 0 {
			return errors.New("checkpoint finalized position is incomplete")
		}
		if value, err := time.Parse(time.RFC3339Nano, record.LastProgressAt); err != nil || value.IsZero() {
			return errors.Join(errors.New("checkpoint progress time is invalid"), err)
		}
	}
	if record.UnavailableSince != "" {
		if value, err := time.Parse(time.RFC3339Nano, record.UnavailableSince); err != nil || value.IsZero() {
			return errors.Join(errors.New("checkpoint read outage time is invalid"), err)
		}
	}
	if record.LastSuccessAt != "" {
		if value, err := time.Parse(time.RFC3339Nano, record.LastSuccessAt); err != nil || value.IsZero() {
			return errors.Join(errors.New("checkpoint successful read time is invalid"), err)
		}
	}
	claimed := record.ContentHash
	record.ContentHash = ""
	actual, err := hashMonitorCheckpoint(record)
	if err != nil || claimed != actual {
		return errors.Join(errors.New("checkpoint content hash differs"), err)
	}
	return nil
}

func hashMonitorCheckpoint(record monitorCheckpointRecord) (string, error) {
	record.ContentHash = ""
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Publish one complete position before reporting a sample as healthy.
// Failed writes may precede or follow rename; callers stop and reopen the
// complete checkpoint instead of assuming which generation reached storage.
func (self *monitorCheckpointStore) save(state *monitorState) error {
	if self == nil || self.lock == nil {
		return errors.New("monitor checkpoint store is closed")
	}
	if state == nil {
		return errors.New("monitor checkpoint state is absent")
	}
	if err := self.requireOwner(); err != nil {
		return err
	}
	if info, err := self.directory.stat(filepath.Base(self.path)); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("checkpoint destination is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	record := monitorCheckpointRecord{
		Schema: monitorCheckpointSchema, NativeChain: self.expected.NativeChain,
		GenesisHash: strings.ToLower(self.expected.GenesisHash), EvmChainId: self.expected.EvmChainId,
		FinalizedHash: strings.ToLower(state.lastHash), FinalizedAt: state.lastNumber,
	}
	if !state.lastProgressAt.IsZero() {
		record.LastProgressAt = state.lastProgressAt.UTC().Format(time.RFC3339Nano)
	}
	if !state.lastSuccessAt.IsZero() {
		record.LastSuccessAt = state.lastSuccessAt.UTC().Format(time.RFC3339Nano)
	}
	if !state.unavailableSince.IsZero() {
		record.UnavailableSince = state.unavailableSince.UTC().Format(time.RFC3339Nano)
	}
	var err error
	record.ContentHash, err = hashMonitorCheckpoint(record)
	if err != nil {
		return err
	}
	if err := self.validate(record); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return errors.Join(self.directory.publish(filepath.Base(self.path), raw, 0600, self.syncDirectory), self.requireOwner())
}
