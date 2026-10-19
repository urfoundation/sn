// Treasury source renewal starts after the actual Alpha migration completes.
// A proof at the independently approved activation root cannot be replaced by a
// later RPC reply, phase guess or an asserted zero pending burn.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/validator"
)

const nativeTreasuryMigrationName = "migrate_alpha_v2_and_unstake_dust_v1"
const nativeTreasuryMigrationNodes = 512
const nativeTreasuryMigrationBytes = 128 * 1024

// The reviewed Identity map key includes the SCALE Vec length, not raw text.
func nativeTreasuryMigrationKey() []byte {
	key := append(xxhash.New128([]byte("SubtensorModule")).Sum(nil), xxhash.New128([]byte("HasMigrationRun")).Sum(nil)...)
	key = append(key, rootCompact(uint64(len(nativeTreasuryMigrationName)))...)
	return append(key, []byte(nativeTreasuryMigrationName)...)
}

// The original signed activation pins the header. Runtime code and the explicit
// true marker must both be committed by that root; future proofs cannot move it.
// The completion marker is monotonic in each reviewed post-migration source.
// Later executions still authenticate that source's original runtime artifact.
func (self *nativeTreasuryAuthority) validateMigration(runtime validator.OwnerRecycleRuntimePin) error {
	if !crv4.ReviewedPostAlphaMigrationSource(runtime.SourceCommit) {
		if self.MigrationComplete != nil {
			return errors.New("historical treasury authority cannot inherit a migration completion")
		}
		return nil
	}
	witness := self.MigrationComplete
	if witness == nil || witness.At != self.Deployment.Activation.Hash || len(witness.Nodes) == 0 || len(witness.Nodes) > nativeTreasuryMigrationNodes {
		return errors.New("native treasury requires its original migration-complete activation proof")
	}
	remaining := nativeTreasuryMigrationBytes
	for _, node := range witness.Nodes {
		if len(node) < 2 || len(node)-2 > 2*remaining {
			return errors.New("native treasury migration proof exceeds its signed byte bound")
		}
		remaining -= (len(node) - 2) / 2
	}
	number, err := witness.Header.authenticate(witness.At)
	if err != nil || number != self.Deployment.Activation.Number {
		return errors.Join(errors.New("native treasury migration proof changed its original activation header"), err)
	}
	// This pure validation owns a finite in-memory proof; no RPC, file or child
	// operation can occur beneath the context-free signed-authority validator.
	ctx := context.Background()
	trie, err := newSafeCurrentStorageTrie(ctx, witness.Nodes)
	if err != nil {
		return err
	}
	var root [32]byte
	raw, err := hex.DecodeString(witness.Header.StateRoot[2:])
	if err != nil || len(raw) != len(root) {
		return errors.New("native treasury migration proof has an invalid state root")
	}
	copy(root[:], raw)
	code, present, err := trie.commitment(ctx, root, []byte(":code"))
	if err != nil || !present || code != runtime.CodeHash {
		return errors.Join(errors.New("native treasury migration proof belongs to another runtime artifact"), err)
	}
	value, present, err := trie.read(ctx, root, nativeTreasuryMigrationKey())
	if err != nil || !present || !bytes.Equal(value, []byte{1}) {
		return errors.Join(errors.New("native treasury activation precedes proven Alpha migration completion"), err)
	}
	return nil
}
