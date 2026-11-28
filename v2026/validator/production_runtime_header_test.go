// Current production admission uses complete committed headers before choosing
// a signed runtime window, with original historical authority kept read-only.
package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// The Rpc serves independently encoded upgrade bytes. Runtime and state
// replies retain their original block-local fixture inputs under the new hash.
func installProductionRuntimeUpgradeHeaderTest(t *testing.T, fixture *productionRuntimeTestFixture, number uint64) types.Hash {
	t.Helper()
	header := mainnetRuntimeTestHeader(number)
	raw, err := codec.Encode(header)
	if err != nil || len(raw) == 0 || raw[len(raw)-1] != 0 {
		t.Fatal("synthetic header does not end in its empty digest vector")
	}
	raw = append(raw[:len(raw)-1], 4, 8)
	hash := types.Hash(blake2b.Sum256(raw))
	var wire map[string]any
	encoded, err := json.Marshal(releaseReceiptTestHeaderWire(header))
	if err != nil || json.Unmarshal(encoded, &wire) != nil {
		t.Fatal("synthetic complete header cannot be encoded")
	}
	wire["digest"] = map[string]any{"logs": []string{"0x08"}}
	client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if strings.HasPrefix(method, "author_") {
			return fmt.Errorf("runtime observation attempted mutation %s", method)
		}
		switch method {
		case "chain_getFinalizedHead":
			if fixture.rpc.head == number {
				return setReleaseHistoricalTestResult(target, hash.Hex())
			}
		case "chain_getBlockHash":
			if len(args) == 1 && args[0] == number {
				return setReleaseHistoricalTestResult(target, hash.Hex())
			}
		case "chain_getHeader":
			if len(args) == 1 && args[0] == hash.Hex() {
				return setReleaseHistoricalTestResult(target, wire)
			}
		}
		translated := append([]any(nil), args...)
		if len(translated) != 0 && translated[len(translated)-1] == hash.Hex() {
			translated[len(translated)-1] = mainnetRuntimeTestBlock(number).Hex()
		}
		return original(ctx, target, method, translated...)
	}
	return hash
}

// An independently approved current runtime at the finalized upgrade block
// reaches the producer capability gate without losing its update digest.
func TestProductionRuntimeCurrentUpgradeHeaderPreservesPurpose(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	hash := installProductionRuntimeUpgradeHeaderTest(t, fixture, 150)
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, hash); err != nil {
		t.Fatalf("approved current upgrade header failed: %v", err)
	}
	if err := validateReleaseNativeSigningRuntime(fixture.rpc.native, fixture.cfg); err != nil {
		t.Fatalf("complete header bypassed or lost producer purpose authentication: %v", err)
	}
}

// A historical upgrade header stays attached to its original approved window;
// it cannot be relabeled as fresh current signing authority.
func TestProductionRuntimeHistoricalUpgradeHeaderRemainsReadOnly(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	hash := installProductionRuntimeUpgradeHeaderTest(t, fixture, 100)
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, hash); err != nil {
		t.Fatalf("approved historical upgrade header failed: %v", err)
	}
	if validateReleaseNativeSigningRuntime(fixture.rpc.native, fixture.cfg) == nil {
		t.Fatal("historical upgrade header acquired current signing authority")
	}
	priorMeta, priorRuntime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, hash); err == nil {
		t.Fatal("current admission backdated the historical upgrade")
	}
	if fixture.rpc.native.Meta != priorMeta || fixture.rpc.native.Runtime != priorRuntime {
		t.Fatal("failed current admission changed the original historical binding")
	}
}

// Even consistent same-height Rpc answers cannot move an out-of-window
// committed header into a signed approval by substituting its number alone.
func TestProductionRuntimeRejectsHeaderWindowSubstitution(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	fixture.rpc.head = 250
	hash := mainnetRuntimeTestBlock(250)
	fixture.rpc.canonicalHashKVs[150] = hash
	client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "chain_getHeader" && args[0] == hash.Hex() {
			header := mainnetRuntimeTestHeader(250)
			header.Number = 150
			return setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(header))
		}
		return original(ctx, target, method, args...)
	}
	priorMeta, priorRuntime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
	err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, hash)
	if err == nil || !strings.Contains(err.Error(), "header SCALE hash") {
		t.Fatalf("substituted header height acquired a signed runtime window: %v", err)
	}
	if fixture.rpc.callKVs["state_getRuntimeVersion"] != 0 || fixture.rpc.native.Meta != priorMeta || fixture.rpc.native.Runtime != priorRuntime {
		t.Fatal("uncommitted height selected an artifact or changed prior authority")
	}
}

// Required roots and digest bytes are part of current authority, not optional
// decorations around an Rpc number. Refusal precedes every artifact read.
func TestProductionRuntimeRejectsIncompleteOrChangedHeader(t *testing.T) {
	for _, fault := range []string{"missing", "state", "parent", "digest"} {
		fixture := newProductionRuntimeTestFixture(t, false)
		client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
		original := client.callContext
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			if method == "chain_getHeader" {
				header := mainnetRuntimeTestHeader(150)
				var wire map[string]any
				encoded, err := json.Marshal(releaseReceiptTestHeaderWire(header))
				if err != nil || json.Unmarshal(encoded, &wire) != nil {
					return fmt.Errorf("synthetic header encoding failed")
				}
				switch fault {
				case "missing":
					delete(wire, "stateRoot")
				case "state":
					wire["stateRoot"] = (types.Hash{0xa5}).Hex()
				case "parent":
					wire["parentHash"] = (types.Hash{0xa6}).Hex()
				case "digest":
					wire["digest"] = map[string]any{"logs": []string{"0x08"}}
				}
				return setReleaseHistoricalTestResult(target, wire)
			}
			return original(ctx, target, method, args...)
		}
		priorMeta, priorRuntime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
		if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(150)); err == nil {
			t.Fatalf("%s header acquired production authority", fault)
		}
		if fixture.rpc.callKVs["state_getRuntimeVersion"] != 0 || fixture.rpc.native.Meta != priorMeta || fixture.rpc.native.Runtime != priorRuntime {
			t.Fatalf("%s header reached artifact selection or changed prior authority", fault)
		}
	}
}
