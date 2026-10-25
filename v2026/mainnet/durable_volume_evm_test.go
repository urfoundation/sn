// Each action's physical head survives process close independently of its
// signed JSON. Missing members cannot renew the original finite send allowance.
package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Retain one test-only signature and counted attempt for the selected schema.
// The graph's actual constructor and calldata bytes remain unchanged.
func retainDurableEvmActionTest(t *testing.T, f *evmCreateFixture, index int) (*evmActionStore, string, []byte) {
	t.Helper()
	predecessor := ""
	if index != 0 {
		predecessor = "sha256:" + strings.Repeat("1", 64)
	}
	store, err := openEvmSelectedActionStore(f.config, index, predecessor, true, nil, f.storage.Context)
	if err != nil {
		t.Fatal(index, err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	private, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := f.config.Plan.Actions[index].unsigned()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(964)), private)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	record.Signed, record.TransactionHash, record.Attempts = "0x"+hex.EncodeToString(raw), tx.Hash().Hex(), 1
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	return store, predecessor, before
}

// Restarts retain the same counted bytes. Removing both named members after
// close cannot be mistaken for an unused action, even with an explicit apply.
func TestEvmDurableLostMembersCannotRecreateAcrossEightActions(t *testing.T) {
	for index := 0; index < 8; index++ {
		f := newEvmEvidenceFixture(t)
		store, predecessor, _ := retainDurableEvmActionTest(t, f, index)
		path := store.path
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		store, err = openEvmSelectedActionStore(f.config, index, predecessor, false, nil, f.storage.Context)
		if err != nil {
			t.Fatal("valid restart", index, err)
		}
		record, err := store.load()
		if err != nil || record.Attempts != 1 || record.Signed == "" {
			t.Fatal("restart lost the signed attempt", index, err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{path, path + ".lock"} {
			if err := os.Rename(name, name+".retained"); err != nil {
				t.Fatal(err)
			}
		}
		store, err = openEvmSelectedActionStore(f.config, index, predecessor, true, nil, f.storage.Context)
		if store != nil {
			_ = store.close()
		}
		if !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("lost action members replenished custody", index, err)
		}
		for _, name := range []string{path, path + ".lock"} {
			if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refused action recreated a member", index, filepath.Base(name), err)
			}
		}
		if retained, err := os.ReadFile(path + ".retained"); err != nil || !bytes.Equal(original, retained) || len(f.writes) != 0 {
			t.Fatal("refusal changed original bytes or sent", index, err)
		}
	}
}

// A syntactically valid earlier journal must not roll back the external head
// after the old process has closed and its in-memory retained hash is gone.
func TestEvmDurableEarlierJournalCannotRestoreAllowanceAcrossEightActions(t *testing.T) {
	for index := 0; index < 8; index++ {
		f := newEvmEvidenceFixture(t)
		store, predecessor, before := retainDurableEvmActionTest(t, f, index)
		path := store.path
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Fatal(err)
		}
		store, err := openEvmSelectedActionStore(f.config, index, predecessor, false, nil, f.storage.Context)
		if store != nil {
			_ = store.close()
		}
		if !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("valid earlier journal refreshed the original allowance", index, err)
		}
		if retained, err := os.ReadFile(path); err != nil || !bytes.Equal(retained, before) || len(f.writes) != 0 {
			t.Fatal("rollback refusal repaired or sent", index, err)
		}
	}
}
