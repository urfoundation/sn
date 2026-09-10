//go:build linux || darwin

package validator

// A qualified private-store producer supplies opaque on-disk fixtures for
// namespace safety tests without exporting a production database constructor.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Close and strictly reopen a real signed M8 store before exporting its exact
// private files. Optional export creates one new artifact and never overwrites.
func TestAttemptRecordStoreNamespaceFixture(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	path := filepath.Join(t.TempDir(), "store")
	store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	visited := 0
	if err := store.Walk(context.Background(), 1, 8, func(record AttemptRecord) error {
		if !reflect.DeepEqual(record, fixture.recordTs[visited]) {
			t.Fatal("fixture store changed its canonical signed prefix")
		}
		visited++
		return nil
	}); err != nil || visited != 8 {
		t.Fatalf("verified fixture census=%d: %v", visited, err)
	}
	head, err := store.Head()
	if err != nil || head.LastSequence != 8 || head.Root != fixture.recordTs[7].RecordHash {
		t.Fatalf("fixture head=%+v: %v", head, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	exportPath := os.Getenv("URNETWORK_ATTEMPT_NAMESPACE_FIXTURE")
	if exportPath == "" {
		return
	}
	if !filepath.IsAbs(exportPath) || filepath.Clean(exportPath) != exportPath {
		t.Fatal("fixture export needs an explicit clean absolute output path")
	}
	type file struct {
		Name string `json:"name"`
		Data []byte `json:"data"`
	}
	output := struct {
		Schema       string                 `json:"schema"`
		Head         attemptRecordStoreHead `json:"head"`
		RecordsJSONL []byte                 `json:"records_jsonl"`
		Files        []file                 `json:"files"`
	}{Schema: "urnetwork-test-attempt-namespace-store-v1", Head: head}
	for _, record := range fixture.recordTs {
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		output.RecordsJSONL = append(output.RecordsJSONL, raw...)
		output.RecordsJSONL = append(output.RecordsJSONL, '\n')
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !attemptStorePrivateFile(info) {
			t.Fatalf("fixture contains nonprivate file %s: %v", entry.Name(), err)
		}
		raw, err := os.ReadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		output.Files = append(output.Files, file{Name: entry.Name(), Data: raw})
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > 1024*1024 || len(output.Files) == 0 || bytes.Count(output.RecordsJSONL, []byte{'\n'}) != 8 {
		t.Fatal("fixture exceeds its exact small export census")
	}
	export, err := os.OpenFile(exportPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if written, err := export.Write(encoded); err != nil || written != len(encoded) {
		_ = export.Close()
		t.Fatalf("write fixture %d/%d: %v", written, len(encoded), err)
	}
	if err := export.Sync(); err != nil {
		_ = export.Close()
		t.Fatal(err)
	}
	if err := export.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("exported %d verified records in %d closed store files (%d JSON bytes)", visited, len(output.Files), len(encoded))
}
