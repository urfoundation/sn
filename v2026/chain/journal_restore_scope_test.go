//go:build linux

// Optional restore proof inputs cannot silently revise already reviewed scope
// bytes. This test grants no journal opening, signing or execution authority.
package chain

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Existing omission retains exactly the old fixed capacity serialization.
func TestNativeRestoreScopeRetainsOriginalOmissionBytes(t *testing.T) {
	old := NativeJournalPreparationScope{Schema: NativeJournalPreparationSchema, MaximumJournalBytes: 16 * 1024 * 1024,
		MaximumRawBytes: 64 * 1024 * 1024, MaximumRawMembers: 10000, MaximumRawRecordBytes: 1024 * 1024}
	before, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(NativeJournalRestoreScope{NativeJournalPreparationScope: old})
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("omitted restore witness changed original scope bytes", err)
	}
}
