// Installation inspection borrows original action journals under shared locks.
// Missing descendants stay unclaimed; partial claims require their original owner.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// This destination set contains only already implemented original owners.
// The unimplemented Safe reservation has no invented journal or lock marker.
func bootstrapContractStateFile(index int) string {
	return []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile, evmVaultLinkStateFile, evmEvidenceCreateStateFile}[index]
}

// A marker must already be complete, private and regular. Shared ownership
// cannot recover an interrupted claim or advance its original action.
func openBootstrapContractReadinessMarker(path, expected string) (_ *os.File, resultErr error) {
	fd, err := syscall.Open(path+".lock", syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, lock.Close())
		}
	}()
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("contract readiness requires a private regular retained marker"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("contract readiness conflicts with an active custody owner"), err)
	}
	raw, err := io.ReadAll(io.LimitReader(lock, int64(len(expected)+1)))
	if err != nil || string(raw) != expected {
		return nil, errors.Join(errors.New("contract readiness marker is incomplete or belongs to another original approval or predecessor"), err)
	}
	return lock, nil
}

// All retained ancestors stay locked until inspection ends. A later incomplete
// journal leaves earlier validated receipts visible, with accounting unresolved.
func inspectBootstrapContractCustody(ctx context.Context, plans []evmCreatePlan, result *bootstrapChainContractReadiness) (resultErr error) {
	if ctx == nil || result == nil || len(plans) == 0 || len(plans) > 8 || len(result.Actions) != 9 {
		return errors.New("contract custody inspection lacks its approved projections")
	}
	locks := []*os.File{}
	defer func() {
		for i := len(locks) - 1; i >= 0; i-- {
			resultErr = errors.Join(resultErr, locks[i].Close())
		}
		if resultErr != nil {
			result.Status, result.CustodyInspectionComplete, result.RemainingOriginalAttempts = "unresolved", false, nil
		}
	}()
	config := plans[0].Config
	configHash := rootObjectHash(config)
	records := []evmActionRecord{}
	for i, plan := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		action := &result.Actions[i]
		action.CustodyStatus = "unresolved"
		path := filepath.Join(config.Plan.RunDirectory, bootstrapContractStateFile(i))
		_, markerErr := os.Lstat(path + ".lock")
		_, journalErr := os.Lstat(path)
		if i > 0 && errors.Is(markerErr, os.ErrNotExist) && errors.Is(journalErr, os.ErrNotExist) {
			action.CustodyStatus = "not-claimed"
			continue
		}
		if markerErr != nil || journalErr != nil {
			return errors.Join(fmt.Errorf("contract readiness %s has missing or incomplete retained custody", action.Id), markerErr, journalErr)
		}
		marker := configHash + "\n"
		if i > 0 {
			if len(records) != i || records[i-1].Receipt == nil || records[i-1].Receipt.Status != 1 {
				return fmt.Errorf("contract readiness %s exists without its complete original predecessor", action.Id)
			}
			marker = rootObjectHash(struct{ ConfigHash, ActionId, PredecessorHash string }{ConfigHash: configHash, ActionId: action.Id, PredecessorHash: rootObjectHash(records[i-1])}) + "\n"
		}
		lock, err := openBootstrapContractReadinessMarker(path, marker+bootstrapRootClaimComplete)
		if err != nil {
			return fmt.Errorf("contract readiness %s: %w", action.Id, err)
		}
		locks = append(locks, lock)
		raw, _, err := readBootstrapRootFile(ctx, path, 512*1024)
		if err != nil {
			return err
		}
		var record evmActionRecord
		if err := decodePlanJson(raw, &record); err != nil {
			return err
		}
		plan.Prerequisites = records
		if err := errors.Join(record.validateForAction(config, i), validateEvmCreatePrerequisite(plan, record)); err != nil {
			return fmt.Errorf("contract readiness %s: %w", action.Id, err)
		}
		if record.Receipt != nil && record.Receipt.Status == 1 {
			if err := validateEvmCreateCompletion(plan, record); err != nil {
				return fmt.Errorf("contract readiness %s completion: %w", action.Id, err)
			}
			if i > 0 && (record.Receipt.NativeNumber < records[i-1].Receipt.NativeNumber || record.Receipt.BlockNumber < records[i-1].Receipt.BlockNumber) {
				return fmt.Errorf("contract readiness %s inclusion precedes its original predecessor", action.Id)
			}
		}
		result.RetainedAttempts += uint16(record.Attempts)
		if result.RetainedAttempts > uint16(config.Plan.MaximumAttempts) {
			return errors.New("contract readiness cumulative attempts exceed the original graph allowance")
		}
		action.JournalHash, action.CustodyHash, action.TransactionHash = record.ContentHash, rootObjectHash(record), record.TransactionHash
		action.Attempts, action.Receipt = record.Attempts, record.Receipt
		action.CustodyStatus = "retained-awaiting-signature"
		if record.Signed != "" {
			action.CustodyStatus = "retained-signature-reconciliation-pending"
		}
		if record.Receipt != nil {
			action.ReceiptObservation, action.CustodyStatus = "retained", "retained-reverted"
			if record.Receipt.Status == 1 {
				action.CustodyStatus = "retained-complete"
				result.RetainedCompletedActions++
				result.SuccessorRequirements.UnfinishedActions = result.SuccessorRequirements.UnfinishedActions[1:]
			} else {
				result.Blockers = append(result.Blockers, "REVERTED_ORIGINAL_ACTION_REQUIRES_SEPARATE_RECOVERY_AUTHORITY")
			}
		}
		records = append(records, record)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	remaining := uint16(config.Plan.MaximumAttempts) - result.RetainedAttempts
	result.Status, result.CustodyInspectionComplete, result.RemainingOriginalAttempts = "blocked", true, &remaining
	if int(result.RetainedCompletedActions) != len(plans) {
		result.Blockers = append(result.Blockers, "APPROVED_EXECUTABLE_PREFIX_INCOMPLETE")
	}
	return nil
}
