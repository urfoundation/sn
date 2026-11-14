//go:build linux || darwin

// The optional companion shares one complete-root byte envelope with its
// original assignment owner. Legacy scopes retain their existing grammar.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Two individually legal owners cannot jointly promise more bytes than the
// exporter can carry. The nil companion is not silently narrowed by that gate.
func TestAttemptLedgerRequestPreparationSharesOriginalRootCapacity(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	scope := AttemptLedgerPreparationScope{Identity: fixture.identity, Coordinator: attemptLedgerDiskTestCoordinator, Limits: attemptLedgerDiskTestLimits(), ExpectedHead: AttemptLedgerHead{Root: zeroAttemptHash()}}
	scope.Limits.MaxStorageBytes, scope.Limits.MaxLegacyBytes = 1024*1024*1024*1024, 1024*1024*1024*1024
	if _, err := scope.validate(); err != nil {
		t.Fatal("optional request capacity changed legacy scope admission", err)
	}
	raw, err := json.Marshal(scope)
	if err != nil || bytes.Contains(raw, []byte(`"requests"`)) {
		t.Fatal("absent request companion changed legacy scope bytes", err)
	}
	scope.Requests = &AttemptLedgerRequestPreparationScope{Preparation: ProviderAttemptRequestPreparation{Identity: ProviderAttemptRequestIdentity{Ledger: fixture.identity, Coordinator: scope.Coordinator, ClientId: connect.Id{7}, PolicyHash: [32]byte{9}},
		Limits: ProviderAttemptRequestLimits{MaxRecords: 8, MaxRecordBytes: 8192, MaxJournalBytes: 64 * 1024}, Birth: attemptLedgerTestBoundary()}}
	if _, err := scope.validate(); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatal("offline scope accepted an unrestorable combined original allowance", err)
	}
	scope.Limits = attemptLedgerDiskTestLimits()
	if _, err := scope.validate(); err != nil {
		t.Fatal("bounded original request companion became unavailable", err)
	}
}
