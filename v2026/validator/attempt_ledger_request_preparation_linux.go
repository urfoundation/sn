//go:build linux

// The request journal is an explicit companion of the original assignment
// ledger. Its birth, limits and acknowledged head never come from the copy.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Original enrollment is separate from the exact portable committed head.
// A nil scope leaves the legacy ledger's preparation representation unchanged.
type AttemptLedgerRequestPreparationScope struct {
	Preparation  ProviderAttemptRequestPreparation `json:"preparation"`
	ExpectedHead ProviderAttemptRequestHead        `json:"expected_head"`
}

// Reject another signing lane, widened capacity or a head predating its birth.
func (self AttemptLedgerRequestPreparationScope) validate(identity AttemptLedgerIdentity, coordinator string) error {
	preparation, head := self.Preparation, self.ExpectedHead
	if preparation.Identity.Ledger != identity || preparation.Identity.Coordinator != coordinator {
		return attemptLedgerCustodyLoss("request preparation differs from its original assignment owner", nil)
	}
	if err := preparation.Validate(); err != nil {
		return err
	}
	if head.Sequence > preparation.Limits.MaxRecords || head.Bytes > preparation.Limits.MaxJournalBytes {
		return attemptLedgerCustodyLoss("request preparation head exceeds its original limits", nil)
	}
	if head.Sequence == 0 {
		if head != (ProviderAttemptRequestHead{}) {
			return attemptLedgerCustodyLoss("request preparation changed the original empty head", nil)
		}
		return nil
	}
	if head.Hash == ([32]byte{}) || head.Bytes == 0 {
		return attemptLedgerCustodyLoss("request preparation lost its original committed prefix", nil)
	}
	if err := validateAttemptBoundary(head.LastBoundary); err != nil {
		return err
	}
	if !providerAttemptRequestAtOrAfter(head.LastBoundary, preparation.Birth) {
		return attemptLedgerCustodyLoss("request preparation head predates or conflicts with original birth", nil)
	}
	return nil
}

// Stream the complete protected journal through the same pure signed-prefix
// verifier as runtime. A restore supplies the original checkpoint separately;
// an existing target checkpoint must already equal the verified rebound bytes.
func (self *attemptPreparationView) requestCheckpoint(scope AttemptLedgerPreparationScope, original []byte, restoring bool) (_ []byte, resultErr error) {
	if scope.Requests == nil {
		if !self.requestAnchorAbsent || len(original) != 0 {
			return nil, attemptLedgerCustodyLoss("request preparation lost its explicit original scope", nil)
		}
		return nil, nil
	}
	request := scope.Requests
	if err := request.validate(scope.Identity, scope.Coordinator); err != nil {
		return nil, err
	}
	if _, present := self.members[ProviderAttemptRequestJournalName]; !present {
		return nil, attemptLedgerCustodyLoss("request preparation lost its complete original journal", nil)
	}
	file, err := attemptPreparationOpen(self.root, ProviderAttemptRequestJournalName, false)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	directoryInfo, err := self.root.Stat()
	if err != nil {
		return nil, err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if len(original) == 0 {
		if restoring {
			return nil, attemptLedgerCustodyLoss("request restore lost original checkpoint bytes", nil)
		}
		if self.requestAnchorAbsent {
			if request.ExpectedHead != (ProviderAttemptRequestHead{}) {
				return nil, attemptLedgerCustodyLoss("request preparation cannot recreate a lost original head", nil)
			}
			original, err = FreshProviderAttemptRequestCheckpoint(request.Preparation, directoryInfo, fileInfo)
			if err != nil {
				return nil, err
			}
		} else {
			original = self.requestAnchor
		}
	}
	var checkpoint ProviderAttemptRequestCheckpoint
	if err := attemptStoreDecode(original, &checkpoint); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(checkpoint)
	if err != nil || !bytes.Equal(canonical, original) || len(original) > 4096 || checkpoint.Committed != request.ExpectedHead {
		return nil, errors.Join(attemptLedgerCustodyLoss("request preparation differs from original canonical checkpoint and head", nil), err)
	}
	if !restoring && (checkpoint.DirectoryInode != self.rootStat.Ino || checkpoint.FileInode != self.members[ProviderAttemptRequestJournalName].stat.Ino) {
		return nil, attemptLedgerCustodyLoss("request preparation changed original physical custody", nil)
	}
	var pending []byte
	if _, present := self.members[ProviderAttemptRequestPendingName]; present {
		pending, err = self.readRoot(ProviderAttemptRequestPendingName)
		if err != nil {
			return nil, err
		}
	}
	verified, err := VerifyProviderAttemptRequestPrefix(self.ctx, checkpoint, request.Preparation, io.LimitReader(file, int64(request.Preparation.Limits.MaxJournalBytes)+1), pending)
	if err != nil {
		return nil, err
	}
	raw, err := verified.ReboundCheckpoint(directoryInfo, fileInfo)
	if err != nil {
		return nil, err
	}
	if !self.requestAnchorAbsent && !bytes.Equal(raw, self.requestAnchor) {
		return nil, attemptLedgerCustodyLoss("request target retains a different original checkpoint", nil)
	}
	return raw, self.checkCensus()
}

// The fixed adapter publishes these bytes beside the assignment checkpoint.
// The complete reviewed census is checked again; this function writes nothing.
func BuildAttemptLedgerPreparationRequestCheckpoint(ctx context.Context, directory *os.File, scope AttemptLedgerPreparationScope, reviewed AttemptLedgerPreparationCensus) (_ []byte, resultErr error) {
	if scope.Requests == nil {
		return nil, errors.New("request preparation requires its explicit original scope")
	}
	observed, err := InspectAttemptLedgerPreparation(ctx, directory, scope)
	if err != nil {
		return nil, err
	}
	if reviewed.RestartAuthorized || !reflect.DeepEqual(reviewed, observed) {
		return nil, errors.New("request preparation differs from its reviewed complete owner census")
	}
	physical, err := openAttemptPreparationView(ctx, directory, scope.Limits)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, physical.close()) }()
	if _, err := physical.census(scope); err != nil {
		return nil, err
	}
	return physical.requestCheckpoint(scope, nil, false)
}

// The original inventory must bind both physical request coordinates and the
// separately reviewed portable head. Payload signatures are checked again on
// the complete copied stream before any rebound target attribute is returned.
func validateAttemptLedgerRequestRestore(scope AttemptLedgerPreparationScope, rootInode uint64, entries map[string]durablevolume.InventoryEntry, original []byte) error {
	if scope.Requests == nil {
		if len(original) != 0 {
			return attemptLedgerCustodyLoss("ledger restore omitted original request scope", nil)
		}
		return nil
	}
	request := scope.Requests
	journal, present := entries[ProviderAttemptRequestJournalName]
	if len(original) == 0 || !present || journal.Physical == nil {
		return attemptLedgerCustodyLoss("ledger restore lost original request journal or checkpoint", nil)
	}
	var checkpoint ProviderAttemptRequestCheckpoint
	if err := attemptStoreDecode(original, &checkpoint); err != nil {
		return err
	}
	canonical, err := json.Marshal(checkpoint)
	if err != nil || !bytes.Equal(canonical, original) || len(original) > 4096 {
		return errors.Join(attemptLedgerCustodyLoss("ledger restore changed canonical request checkpoint bytes", nil), err)
	}
	if err := validateProviderAttemptRequestCheckpoint(checkpoint, request.Preparation); err != nil {
		return err
	}
	if checkpoint.DirectoryInode == 0 || checkpoint.DirectoryInode != rootInode || checkpoint.FileInode == 0 || checkpoint.FileInode != journal.Physical.Inode || checkpoint.Committed != request.ExpectedHead || journal.Size < checkpoint.Committed.Bytes || journal.Size-checkpoint.Committed.Bytes > request.Preparation.Limits.MaxRecordBytes {
		return attemptLedgerCustodyLoss("ledger restore changed original request inode or acknowledged head", nil)
	}
	pending, hasPending := entries[ProviderAttemptRequestPendingName]
	if checkpoint.Pending != nil {
		if !hasPending || pending.Size != checkpoint.Pending.Bytes || pending.Sha256 != "sha256:"+hex.EncodeToString(checkpoint.Pending.Hash[:]) {
			return attemptLedgerCustodyLoss("ledger restore lost complete original pending request bytes", nil)
		}
	}
	if !hasPending && journal.Size != checkpoint.Committed.Bytes {
		return errors.Join(ErrDurablePublicationUncertain, errors.New("ledger restore cannot explain original request journal suffix"))
	}
	return nil
}
