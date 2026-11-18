//go:build linux || darwin

// Cache tests exercise actual sr25519 signatures, no-follow file ownership and
// the shared raw complete-body scanner. They never substitute an absence verdict.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// One genuine empty block is enough to qualify the small cache's physical
// owner; real nonempty intent/restart composition is covered separately.
func productionReceiptCheckpointTestOwner(t *testing.T) (*productionReceiptCheckpointOwner, *crv4.FinalizedExtrinsicScan) {
	t.Helper()
	header, hash := releaseReceiptTestHeader(t, types.Hash{2}, 100)
	client := &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		var value any
		switch method {
		case "chain_getFinalizedHead", "chain_getBlockHash":
			value = hash.Hex()
		case "chain_getHeader":
			value = releaseReceiptTestHeaderWire(header)
		case "chain_getBlock":
			value = map[string]any{"block": map[string]any{"header": releaseReceiptTestHeaderWire(header), "extrinsics": []string{}}}
		default:
			return errors.New("unexpected checkpoint fixture RPC")
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, target)
	}}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	scan, err := chain.ScanFinalizedExtrinsicRange(t.Context(), types.Hash{7}, crv4.FinalizedExtrinsicScanRange{First: 100, MaximumBlocks: 1})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crv4.KeypairFromSeed([32]byte{0x5a})
	if err != nil {
		t.Fatal(err)
	}
	owner := &productionReceiptCheckpointOwner{path: filepath.Join(identityTestStateDir(t), "cache"), key: key, scope: [32]byte{0x81}, preparedAt: 100, txHash: types.Hash{7}}
	return owner, scan
}

// Eviction and stale identity/semantics are misses, not missing intent state.
func TestProductionReceiptCheckpointMissesAreDisposable(t *testing.T) {
	owner, scan := productionReceiptCheckpointTestOwner(t)
	if value, err := owner.load(t.Context()); err != nil || value != nil {
		t.Fatalf("missing cache became a startup refusal: %v", err)
	}
	if _, err := os.Stat(owner.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cache miss created state during a read")
	}
	if err := owner.save(t.Context(), scan); err != nil {
		t.Fatal(err)
	}
	value, err := owner.load(t.Context())
	if err != nil || value == nil || value.Through != 100 {
		t.Fatalf("completed signed coverage was not reusable: %+v %v", value, err)
	}
	path := filepath.Join(owner.path, productionReceiptCheckpointName)
	for _, fault := range []string{"empty", "old-semantics", "other-intent", "missing"} {
		candidate := *value
		var raw []byte
		switch fault {
		case "old-semantics":
			candidate.Schema = "synthetic-retired-absence-v0"
			raw, _ = json.Marshal(candidate)
		case "other-intent":
			candidate.Scope[0] ^= 1
			raw, _ = json.Marshal(candidate)
		}
		if fault == "missing" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if value, err := owner.load(t.Context()); err != nil || value != nil {
			t.Fatalf("%s cache state granted coverage or stranded recovery: %v", fault, err)
		}
		if err := owner.save(t.Context(), scan); err != nil {
			t.Fatalf("%s cache could not be rebuilt from real complete bodies: %v", fault, err)
		}
	}
}

// Altering authenticated fields or physical ownership cannot manufacture a
// reusable prefix. The primitive reports the actual cache error; production's
// optional-cache owner disables reuse rather than classifying it as RPC failure.
func TestProductionReceiptCheckpointRejectsForgedCoverageAndCustody(t *testing.T) {
	owner, scan := productionReceiptCheckpointTestOwner(t)
	if err := owner.save(t.Context(), scan); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(owner.path, productionReceiptCheckpointName)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"height", "hash", "signature", "alias", "symlink", "permissions", "oversized"} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		var value productionReceiptCheckpoint
		if err := json.Unmarshal(original, &value); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0600)
		switch fault {
		case "height":
			value.Through++
		case "hash":
			value.BlockHash[0] ^= 1
		case "signature":
			value.Signature[0] ^= 1
		case "permissions":
			mode = 0644
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, '\n')
		if fault == "oversized" {
			raw = make([]byte, productionReceiptCheckpointLimit+1)
		}
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal(err)
		}
		if fault == "permissions" {
			// Creation permissions are filtered by the runner's umask. Create
			// and verify the intended public-readable fault explicitly.
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() || info.Mode().Perm() != mode {
				t.Fatalf("permissions fault not created: got %v, want regular %04o", info.Mode(), mode)
			}
		}
		if fault == "alias" || fault == "symlink" {
			alias := filepath.Join(owner.path, fault)
			if fault == "alias" {
				err = os.Link(path, alias)
			} else {
				err = os.Rename(path, alias)
				if err == nil {
					err = os.Symlink(alias, path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if result, err := owner.load(t.Context()); err == nil || result != nil || retryableProductionSteeringRead(err) {
			t.Fatalf("%s forged cache became coverage or a transport wait: %+v %v", fault, result, err)
		}
	}
}

// Even a valid complete-body witness cannot be relabeled for another original
// transaction or attached after a gap. Its inputs are part of the opaque result.
func TestProductionReceiptCheckpointBindsScannerInputAndContinuity(t *testing.T) {
	owner, scan := productionReceiptCheckpointTestOwner(t)
	for _, fault := range []string{"transaction", "gap", "overlap"} {
		candidate := *owner
		switch fault {
		case "transaction":
			candidate.txHash[0] ^= 1
		case "gap":
			candidate.preparedAt--
		case "overlap":
			candidate.checkpoint = &productionReceiptCheckpoint{Through: 100, BlockHash: types.Hash{1}}
		}
		if err := candidate.save(t.Context(), scan); err == nil {
			t.Fatalf("%s scanner witness acquired unrelated cache authority", fault)
		}
	}
	if _, err := os.Stat(owner.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused scanner witness created cache state")
	}
}
