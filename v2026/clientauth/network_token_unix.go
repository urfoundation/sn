//go:build linux || darwin

package clientauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// How long an explicit sign-in waits for the token's owner lock. A renewal
// holds it for a moment; a registration that borrows the token can hold it
// for minutes, and then the operator is told to stop it or try again.
var networkTokenOwnerWait = 45 * time.Second

const (
	networkTokenOwnerFirstPoll   = 50 * time.Millisecond
	networkTokenOwnerLongestPoll = 2 * time.Second
)

// WriteNetworkTokenWithContext persists a network token from an explicit
// sign-in (an auth command or a hotkey sign-in) and removes the token's
// renewal lineage, both under the token's registration owner lock, so a
// renewal can neither overwrite the sign-in nor be left without its lineage
// (network_token.go). It waits for the lock up to networkTokenOwnerWait, then
// answers ErrNetworkTokenInUse and writes nothing. It never writes without the
// lock.
func WriteNetworkTokenWithContext(ctx context.Context, path string, token string) (returnErr error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("refusing to persist an empty token")
	}
	store, err := openNetworkTokenOwner(ctx, path)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, store.close()) }()
	name := filepath.Base(path)
	if err := store.write(name, []byte(token)); err != nil {
		return err
	}
	return store.remove(name + ".lineage")
}

// openNetworkTokenOwner takes the token's registration owner lock, polling a
// held lock with a growing interval until networkTokenOwnerWait.
func openNetworkTokenOwner(ctx context.Context, path string) (*registrationStore, error) {
	deadline := time.Now().Add(networkTokenOwnerWait)
	poll := networkTokenOwnerFirstPoll
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		store, err := openRegistrationStore(path)
		if err == nil {
			return store, nil
		}
		if !errors.Is(err, errRegistrationOwnerActive) {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, networkTokenInUse(path)
		}
		timer := time.NewTimer(min(poll, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		poll = min(poll*2, networkTokenOwnerLongestPoll)
	}
}

// RenewNetworkToken replaces the network token at path with its renewal,
// while the file still holds previous. It answers false, without an error,
// when the file holds another token: an explicit sign-in or another renewer
// wrote it first, and the renewal is discarded. Explicit sign-ins hold the
// same owner lock (WriteNetworkTokenWithContext), so none lands between this
// renewal's read and its rename.
//
// The renewal holds the token's registration owner lock (the lock a
// measurement registration takes to borrow the token), and is published in
// the lineage before the renewed file is renamed into place
// (network_token.go). ErrNetworkTokenBusy is a held lock; try again later.
func RenewNetworkToken(path string, previous string, renewed string) (_ bool, returnErr error) {
	previous, renewed = strings.TrimSpace(previous), strings.TrimSpace(renewed)
	if previous == "" || renewed == "" || previous == renewed {
		return false, errors.New("network token renewal needs a previous and a different renewed token")
	}
	store, err := openRegistrationStore(path)
	if err != nil {
		if errors.Is(err, errRegistrationOwnerActive) {
			return false, ErrNetworkTokenBusy
		}
		return false, err
	}
	defer func() { returnErr = errors.Join(returnErr, store.close()) }()
	name := filepath.Base(path)
	if err := store.removeNetworkRenewalLeftovers(name); err != nil {
		return false, err
	}
	current, fingerprint, err := store.readNetworkCredential(name)
	if err != nil {
		return false, err
	}
	if current != previous {
		return false, nil
	}
	lineageName := name + ".lineage"
	var members []string
	if raw, err := store.read(lineageName); err == nil {
		members, err = parseNetworkCredentialLineage(raw)
		if err != nil {
			return false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if !slices.Contains(members, fingerprint) {
		// the current file began its sign-in: an explicit sign-in, or a file
		// renewed before lineages existed
		members = []string{fingerprint}
	}

	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return false, err
	}
	temporary := networkRenewalLeftoverPrefix(name) + hex.EncodeToString(random[:])
	file, err := store.open(temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return false, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			returnErr = errors.Join(returnErr, store.remove(temporary))
		}
	}()
	_, writeErr := io.WriteString(file, renewed)
	syncErr := file.Sync()
	var stat unix.Stat_t
	statErr := unix.Fstat(int(file.Fd()), &stat)
	if err := errors.Join(writeErr, syncErr, statErr, file.Close()); err != nil {
		return false, err
	}
	if hook := networkRenewalTestHooks.afterTemporary; hook != nil && hook() {
		cleanup = false
		return false, errNetworkRenewalTestStop
	}
	renewedFingerprint := networkCredentialFingerprintOf(renewed, stat.Mtim.Nano(), true)
	encoded, err := encodeNetworkCredentialLineage(appendNetworkCredentialLineage(members, renewedFingerprint))
	if err != nil {
		return false, err
	}
	// published first: a crash before the rename leaves the old file, whose
	// fingerprint stays in the lineage, so every marker still blocks
	if err := store.write(lineageName, encoded); err != nil {
		return false, err
	}
	if hook := networkRenewalTestHooks.afterLineage; hook != nil && hook() {
		cleanup = false
		return false, errNetworkRenewalTestStop
	}
	if hook := networkRenewalTestHooks.beforeRename; hook != nil {
		hook()
	}
	if err := store.check(); err != nil {
		return false, err
	}
	if err := unix.Renameat(int(store.directory.Fd()), temporary, int(store.directory.Fd()), name); err != nil {
		return false, err
	}
	cleanup = false
	return true, errors.Join(store.directory.Sync(), store.check())
}

// networkRenewalTestHooks stop a renewal right after one of its durable
// steps the way a process that dies there would: nothing after the step runs,
// and nothing is cleaned up. A hook that answers true stops the renewal.
var networkRenewalTestHooks struct {
	afterTemporary func() bool
	afterLineage   func() bool
	// runs with the owner lock held, after the lineage and before the
	// rename; it may block to hold a renewal in that window
	beforeRename func()
}

var errNetworkRenewalTestStop = errors.New("network renewal stopped by its test hook")

func networkRenewalLeftoverPrefix(name string) string {
	return "." + name + ".renew-"
}

// A renewal interrupted before its rename leaves its renewed file. That file
// was never installed, so it is removed; the lineage member it added names a
// file that never existed and blocks nothing on its own.
func (self *registrationStore) removeNetworkRenewalLeftovers(name string) error {
	names, err := self.names(4096)
	if err != nil {
		return err
	}
	prefix := networkRenewalLeftoverPrefix(name)
	for _, leftover := range names {
		if strings.HasPrefix(leftover, prefix) {
			if err := self.remove(leftover); err != nil {
				return err
			}
		}
	}
	return nil
}

// The store's read of the token, with the fingerprint from the same file.
func (self *registrationStore) readNetworkCredential(name string) (token string, fingerprint string, returnErr error) {
	if err := self.check(); err != nil {
		return "", "", err
	}
	file, err := self.open(name, unix.O_RDONLY)
	if err != nil {
		return "", "", err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return "", "", err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumRegistrationBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(raw) > maximumRegistrationBytes {
		return "", "", errors.New("network token exceeds its finite bound")
	}
	token = strings.TrimSpace(string(raw))
	if token == "" {
		return "", "", errors.New("network token is empty")
	}
	return token, networkCredentialFingerprintOf(token, stat.Mtim.Nano(), true), self.check()
}

// QuarantineNetworkToken sets the rejected network token at path aside: it
// renames the file, keeping its mode and content, to
// <name>.rejected-<unixnano>-<consecutive> in the same directory
// (network_token_quarantine.go). It holds the token's registration owner
// lock, the lock every explicit sign-in and renewal holds, and acts only while
// the file still holds rejected: a sign-in that replaced it first is never set
// aside (false, nil), and none lands between the check and the rename. It
// keeps the newest maximumNetworkTokenQuarantine set-aside files, the one it
// writes always among them. The lineage stays beside the missing token; the
// next sign-in replaces it under the same lock. ErrNetworkTokenBusy is a held
// lock; try again later.
func QuarantineNetworkToken(path string, rejected string, now time.Time) (_ NetworkTokenQuarantine, _ bool, returnErr error) {
	rejected = strings.TrimSpace(rejected)
	if rejected == "" {
		return NetworkTokenQuarantine{}, false, errors.New("network token quarantine needs the rejected token")
	}
	store, err := openRegistrationStore(path)
	if err != nil {
		if errors.Is(err, errRegistrationOwnerActive) {
			return NetworkTokenQuarantine{}, false, ErrNetworkTokenBusy
		}
		return NetworkTokenQuarantine{}, false, err
	}
	defer func() { returnErr = errors.Join(returnErr, store.close()) }()
	name := filepath.Base(path)
	current, _, err := store.readNetworkCredential(name)
	if errors.Is(err, os.ErrNotExist) {
		return NetworkTokenQuarantine{}, false, nil
	}
	if err != nil {
		return NetworkTokenQuarantine{}, false, err
	}
	if current != rejected {
		return NetworkTokenQuarantine{}, false, nil
	}
	names, err := store.names(4096)
	if err != nil {
		return NetworkTokenQuarantine{}, false, err
	}
	quarantines := networkTokenQuarantines(store.path, name, names)
	at, consecutive := nextNetworkTokenQuarantine(quarantines, now)
	target := networkTokenQuarantineName(name, at, consecutive)
	if hook := networkQuarantineTestHooks.beforeRename; hook != nil {
		hook()
	}
	if err := store.check(); err != nil {
		return NetworkTokenQuarantine{}, false, err
	}
	if err := unix.Renameat(int(store.directory.Fd()), name, int(store.directory.Fd()), target); err != nil {
		return NetworkTokenQuarantine{}, false, err
	}
	if err := errors.Join(store.directory.Sync(), store.check()); err != nil {
		return NetworkTokenQuarantine{}, false, err
	}
	quarantine := NetworkTokenQuarantine{Path: filepath.Join(store.path, target), Time: at, Consecutive: consecutive}
	// the oldest beyond the bound go; the one just written is the newest
	kept := append(quarantines, quarantine)
	for _, old := range kept[:max(0, len(kept)-maximumNetworkTokenQuarantine)] {
		if err := store.remove(filepath.Base(old.Path)); err != nil {
			return quarantine, true, err
		}
	}
	return quarantine, true, nil
}

// networkQuarantineTestHooks holds a quarantine with its owner lock held,
// after its check and before its rename.
var networkQuarantineTestHooks struct {
	beforeRename func()
}
