// Pending nonce observations can lag an earlier prepared/send timeout. The
// shared admission therefore remembers every durably signed nonce, including
// all member queues loaded before startup, until this process ends.
package miner

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"path/filepath"
)

type claimNonceSafetyError struct{ cause error }

func (self *claimNonceSafetyError) Error() string {
	return "claim nonce custody is unavailable: " + self.cause.Error()
}
func (self *claimNonceSafetyError) Unwrap() error { return self.cause }

func (self *claimAdmission) checkChain(chainId *big.Int) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.nonceError != nil {
		return self.nonceError
	}
	if chainId == nil || chainId.Sign() <= 0 {
		self.nonceError = &claimNonceSafetyError{cause: errors.New("invalid chain identity")}
	} else if self.chainId != nil && self.chainId.Cmp(chainId) != 0 {
		self.nonceError = &claimNonceSafetyError{cause: errors.New("shared relayer has signed intents on a different chain")}
	} else if self.chainId == nil {
		self.chainId = new(big.Int).Set(chainId)
	}
	return self.nonceError
}

// An authenticated nonce-custody failure stops new signatures immediately,
// before the failing member unwinds and the swarm cancels its siblings.
func (self *claimAdmission) failNonce(err error) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.nonceError == nil {
		self.nonceError = &claimNonceSafetyError{cause: err}
	}
	return self.nonceError
}

func (self *claimAdmission) nonceMinimum() uint64 {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.nonceFloor
}

// Call only after the exact raw/hash checkpoint succeeded, or while seeding
// already durable files before workers start. No receipt/finality is inferred.
func (self *claimAdmission) rememberSigned(cfg *ClaimDaemonConfig, entry *ClaimQueueEntry) error {
	tx, _, _, err := authenticateSignedClaim(cfg, entry)
	if err != nil {
		return self.failNonce(err)
	}
	if tx.Nonce() == math.MaxUint64 {
		return self.failNonce(errors.New("signed nonce has no successor"))
	}
	if err := self.checkChain(tx.ChainId()); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.nonceFloor = max(self.nonceFloor, tx.Nonce()+1)
	return nil
}

func (self *claimAdmission) seedMember(cfg *ClaimDaemonConfig) error {
	store := &claimQueueStore{path: filepath.Join(cfg.StateDir, "claim-queue.json")}
	queue, err := store.load()
	if err != nil {
		return err
	}
	self.observeEpoch(queue.LastDiscovered)
	for _, entry := range queue.Entries {
		if entry == nil {
			return errors.New("claim queue contains a nil entry")
		}
		if entry.TxHash == "" && entry.RawTxHex == "" {
			continue
		}
		if err := self.rememberSigned(cfg, entry); err != nil {
			return fmt.Errorf("claim epoch %d: %w", entry.Epoch, err)
		}
	}
	return nil
}
