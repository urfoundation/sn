//go:build linux || darwin

package validator

// Exact read-budget controls use persisted arithmetic and an independently
// enumerated allowance. They do not claim bounded live writes or process RSS.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Enumerates the documented logical reservation without using the production
// tokenizer or charge helper. JSON escape bytes are not decoded string bytes.
func headEMAStoreV2TestControlAllowance(path string, file headEMAFile) uint64 {
	allowance := uint64(len(path)+len(file.Schema)+len(file.UpdatedAt)) +
		uint64(reflect.TypeFor[headEMAFile]().Size()) +
		uint64(reflect.TypeFor[HeadEMAStore]().Size())
	if file.LastSubnetEpoch != nil {
		allowance += uint64(reflect.TypeFor[uint64]().Size())
	}
	if file.LastAlpha != nil {
		allowance += uint64(reflect.TypeFor[protocol.Rational]().Size())
	}
	stringHeaderBytes := uint64(reflect.TypeFor[string]().Size())
	entryBytes := uint64(reflect.TypeFor[headEMAEntry]().Size())
	rationalBytes := uint64(reflect.TypeFor[RationalJSON]().Size())
	foldBytes := uint64(reflect.TypeFor[HeadEMAMeasurement]().Size())
	for _, entry := range file.Entries {
		allowance += 2*entryBytes + 156 + stringHeaderBytes + 2048
		allowance += 33 * uint64(len(entry.Numerator)+len(entry.Denominator))
	}
	for _, fold := range file.LastFold {
		allowance += foldBytes + 156 + stringHeaderBytes + rationalBytes + 2048
		for _, rational := range []RationalJSON{fold.Raw, fold.Prior, fold.Next} {
			allowance += 33 * uint64(len(rational.Numerator)+len(rational.Denominator))
		}
	}
	return allowance
}

// Both attempts read the same real file. Refusal must precede the actual
// arithmetic boundary; success must return the complete unchanged state.
func assertHeadEMAStoreV2ExactControl(t *testing.T, stateDir string, encoded []byte, file headEMAFile) {
	t.Helper()
	path := filepath.Join(stateDir, "head-ema.json")
	allowance := headEMAStoreV2TestControlAllowance(path, file)
	expectedEntries := make(map[string]headEMAEntry, len(file.Entries))
	for _, entry := range file.Entries {
		expectedEntries[entry.Key.String()] = entry
	}
	for _, limit := range []uint64{allowance - 1, allowance} {
		parsed := 0
		store, err := newHeadEMAStoreV2(context.Background(), stateDir, HeadEMAStoreV2Limits{
			MaxFileBytes: uint64(len(encoded)),
			MaxEntries: uint64(max(1, len(file.Entries), len(file.LastFold))),
			MaxControlBytes: limit,
		}, headEMAStoreV2LoadHooks{beforeRational: func() error { parsed++; return nil }})
		if limit < allowance {
			if err == nil || store != nil || parsed != 0 {
				t.Fatalf("one-short combined control limit=%d: store=%v parsed=%d error=%v", limit, store, parsed, err)
			}
			continue
		}
		if err != nil || store == nil || parsed != 1 {
			t.Fatalf("exact combined control limit=%d: store=%v parsed=%d error=%v", limit, store, parsed, err)
		}
		if store.path != path || !reflect.DeepEqual(store.values, expectedEntries) ||
			!reflect.DeepEqual(store.lastSubnetEpoch, file.LastSubnetEpoch) ||
			!reflect.DeepEqual(store.lastAlpha, file.LastAlpha) ||
			!reflect.DeepEqual(store.lastFold, file.LastFold) {
			t.Fatal("exact control admission changed persisted arithmetic or its ownership path")
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, encoded) {
		t.Fatalf("control-boundary read changed its persisted input: %v", err)
	}
}

// Entries and the retained fold must share one exact allowance, including
// both fixed owners, optional pointers, index keys and every decimal string.
func TestHeadEMAStoreV2ExactCombinedControlBoundary(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	var file headEMAFile
	if err := json.Unmarshal(fixture.encoded, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Entries) != 2 || len(file.LastFold) != 2 || file.LastAlpha == nil || file.LastSubnetEpoch == nil {
		t.Fatal("real persisted fixture lacks both independent row groups")
	}
	assertHeadEMAStoreV2ExactControl(t, fixture.stateDir, fixture.encoded, file)
}

// A genuine alpha-one decay removes all entries but retains the complete
// fold. A zero entry census must not erase that transcript's reservation.
func TestHeadEMAStoreV2ExactControlWithOnlyFoldHistory(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	if _, _, err := fixture.store.FoldForEpoch(11, nil, protocol.Rational{Numerator: 1, Denominator: 1}); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	var file headEMAFile
	if err := json.Unmarshal(encoded, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Entries) != 0 || len(file.LastFold) != 2 {
		t.Fatal("real zero fold lost its independent history census")
	}
	assertHeadEMAStoreV2ExactControl(t, fixture.stateDir, encoded, file)
}

// Historical schema-one entries have no epoch/alpha pointer or fold owners.
// This is an encoding-compatibility fixture, not a live migration claim.
func TestHeadEMAStoreV2ExactControlForLegacyEntries(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	var file headEMAFile
	if err := json.Unmarshal(fixture.encoded, &file); err != nil {
		t.Fatal(err)
	}
	file.Schema, file.LastSubnetEpoch, file.LastAlpha, file.LastFold = headEMASchemaV1, nil, nil, nil
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHeadEMAStoreV2ExactControl(t, fixture.stateDir, encoded, file)
}

// Equivalent JSON escaping changes wire bytes, not decoded decimal charges.
// A multibyte physical path and ignored timestamp metadata cover byte length
// independently of rune count without changing any persisted score or fold.
func TestHeadEMAStoreV2ExactControlUsesDecodedStringsAndPathBytes(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	stateDir := fixture.stateDir + "-☃"
	if err := os.Rename(fixture.stateDir, stateDir); err != nil {
		t.Fatal(err)
	}
	var file headEMAFile
	if err := json.Unmarshal(fixture.encoded, &file); err != nil {
		t.Fatal(err)
	}
	file.UpdatedAt = "<>&\t\\\"\u2028☃"
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	from, to := []byte("\"numerator\":\"4\""), []byte("\"numerator\":\"\\u0034\"")
	if !bytes.Contains(encoded, from) {
		t.Fatal("real fold fixture lacks its expected canonical decimal")
	}
	encoded = bytes.Replace(encoded, from, to, 1)
	if !bytes.Contains(encoded, []byte("\\u003c")) || !bytes.Contains(encoded, to) {
		t.Fatal("format fixture lacks both ordinary-string and rational escapes")
	}
	var decoded headEMAFile
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, file) {
		t.Fatalf("equivalent wire escaping changed decoded state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "head-ema.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHeadEMAStoreV2ExactControl(t, stateDir, encoded, file)
}
