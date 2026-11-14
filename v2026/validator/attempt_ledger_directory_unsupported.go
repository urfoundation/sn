//go:build !linux && !darwin

package validator

// Unsupported systems fail closed rather than pretend that pathname checks
// provide the migration exclusion required by the durable ledger.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
)

var errAttemptLedgerPlatform = errors.New("private attempt ledger supports Linux and Darwin only")

// Directory checks cannot establish the required platform lock contract.
func attemptLedgerPrivateDirectory(os.FileInfo) bool { return false }

// No unsupported backend is permitted to open evidence files.
func attemptLedgerPrivateFile(os.FileInfo) bool { return false }

// No warm projection cursor can be established on an unsupported platform.
func attemptLedgerSameFileState(os.FileInfo, os.FileInfo) bool { return false }

// This path is unreachable after the platform guard.
func attemptLedgerNoFollowFlag() int { return 0 }

// Unsupported platforms do not advertise advisory locking.
func attemptLedgerTryLock(*os.File) (bool, error) { return false, errAttemptLedgerPlatform }

// Unsupported platforms cannot release ownership they never acquired.
func attemptLedgerUnlock(*os.File) error { return errAttemptLedgerPlatform }

// Unsupported platforms cannot publish durable migration receipts.
func attemptLedgerRenameNoReplace(*os.File, string, string) error { return errAttemptLedgerPlatform }

// The API remains explicit on platforms without a qualified backend.
func NewDiskAttemptLedger(context.Context, string, AttemptLedgerIdentity, string, ed25519.PrivateKey, AttemptLedgerDiskLimits) (*AttemptLedger, error) {
	return nil, errAttemptLedgerPlatform
}
