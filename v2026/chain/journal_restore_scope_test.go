//go:build linux || darwin

// Optional restore proof inputs cannot silently revise already reviewed scope
// bytes. This test grants no journal opening, signing or execution authority.
package chain

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
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

// JSON's last-key-wins rule cannot choose between two raw nominations. The
// public request decoder refuses duplicate keys first; the scope keeps its own
// refusal for any caller that reaches it with repeated bytes.
func TestNativeRestoreScopeRefusesRepeatedRawNomination(t *testing.T) {
	old := NativeJournalPreparationScope{Schema: NativeJournalPreparationSchema, MaximumJournalBytes: 16 * 1024 * 1024,
		MaximumRawBytes: 64 * 1024 * 1024, MaximumRawMembers: 10000, MaximumRawRecordBytes: 1024 * 1024}
	first, second := strings.Repeat("a", 64)+".scale", strings.Repeat("b", 64)+".scale"
	raw, err := json.Marshal(NativeJournalRestoreScope{NativeJournalPreparationScope: old, PendingRawMember: first})
	if err != nil {
		t.Fatal(err)
	}
	var scope NativeJournalRestoreScope
	if err := decodeNativeRestoreScope(raw, &scope); err != nil || scope.PendingRawMember != first {
		t.Fatal("single raw nomination was refused", err, scope.PendingRawMember)
	}
	repeated := append(bytes.TrimSuffix(raw, []byte("}")), []byte(`,"pending_raw_member":"`+second+`"}`)...)
	if err := decodeNativeRestoreScope(repeated, &NativeJournalRestoreScope{}); !errors.Is(err, ErrJournalUncertain) || !strings.Contains(err.Error(), "ambiguous repeated raw nomination") {
		t.Fatal("repeated raw nomination selected one witness", err)
	}
}
