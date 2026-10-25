// Optional original-request custody is opened by each real authenticated
// operator. Legacy configurations keep unknown coverage without a new gate.
package validator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// The hash-pinned preparation input is separate from mutable JWT and directory
// state. The authenticated client and original ledger must both match it.
func openReleaseProviderAttemptRequests(ctx context.Context, cfg *ReleaseConfig, op OperatorConfig, ledger *AttemptLedger, clientId connect.Id, key ed25519.PrivateKey) (*ProviderAttemptRequestJournal, error) {
	if op.RequestPreparation == nil {
		return nil, nil
	}
	if cfg == nil || ledger == nil {
		return nil, errors.New("provider request runtime has no original ledger owner")
	}
	raw, err := ReadReleaseEvidenceV2File(ctx, *op.RequestPreparation, 4096)
	if err != nil {
		return nil, err
	}
	var expected ProviderAttemptRequestPreparation
	if err := attemptStoreDecode(raw, &expected); err != nil {
		return nil, err
	}
	policyHash, err := parseHash32("provider request policy", cfg.PolicyHash)
	if err != nil {
		return nil, err
	}
	if expected.Identity.Ledger != ledger.identity || expected.Identity.ClientId != clientId || expected.Identity.PolicyHash != policyHash || expected.Identity.Coordinator != strings.ToLower(cfg.Coordinator) {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request prepared identity differs from authenticated operator"))
	}
	if err := ValidateProviderAttemptRequestRootCapacity(ledger.diskLimits, expected.Limits); err != nil {
		return nil, err
	}
	return OpenProviderAttemptRequestJournal(ctx, op.StateDir, expected, key)
}

// The shared complete owner cannot promise independent maxima whose sum is
// larger than the finite Core inventory profile. This is a logical sizing
// check; physical free capacity and other sibling owners remain independently
// admitted by the complete production preparation scope.
func ValidateProviderAttemptRequestRootCapacity(ledger AttemptLedgerDiskLimits, request ProviderAttemptRequestLimits) error {
	if err := request.Validate(); err != nil {
		return err
	}
	const maximum = uint64(1024 * 1024 * 1024 * 1024)
	var total uint64
	// Import receipt reserves six rows; the assignment pending record reserves
	// one more. Legacy bytes remain retained outside the bounded LevelDB tree.
	for _, value := range []uint64{ledger.MaxStorageBytes, ledger.MaxLegacyBytes, ledger.MaxRecordBytes, ledger.MaxRecordBytes, ledger.MaxRecordBytes, ledger.MaxRecordBytes, ledger.MaxRecordBytes, ledger.MaxRecordBytes, ledger.MaxRecordBytes, 4096, 4096, 4096, request.MaxJournalBytes, request.MaxRecordBytes, request.MaxRecordBytes, 4096} {
		if value > maximum-total {
			return errors.Join(protocol.ErrProviderAttemptsCapacity, errors.New("provider request and assignment owners exceed complete-root byte profile"))
		}
		total += value
	}
	return nil
}
