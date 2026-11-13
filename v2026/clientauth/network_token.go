package clientauth

// The network token file and its renewal.
//
// A rejected client's marker (MarkRejected) names the network token file as
// it was when the server rejected the client: a fingerprint of the token and
// the file's modification time. Startup refuses to recreate that client until
// the fingerprint changes, which an explicit sign-in (an auth command or a
// hotkey sign-in) does on purpose. A renewal continues the same sign-in, so it
// must not change what a marker matches.
//
// Each renewal therefore records, in the token's lineage file
// (<token>.lineage), the fingerprint of the file it replaces and of the file
// it writes. A marker blocks while the current file's fingerprint and the
// marker both belong to the lineage. The lineage is written before the renewed
// file is renamed into place, so a crash at any point leaves a state that
// still blocks: the old file with the old lineage, the old file with the
// extended lineage, or the renewed file with the extended lineage.
//
// An explicit sign-in writes a file whose fingerprint is in no lineage and
// removes the lineage, which unblocks on purpose. That holds only because the
// sign-in and the renewal are serialized: both hold the token's registration
// owner lock (<token>.registration.lock). An unserialized sign-in landing
// between a renewal's lineage write and its rename would leave the renewal of
// the old sign-in with no lineage: the sign-in lost, and a revoked client
// unblocked. So an explicit sign-in waits, bounded, for the lock and never
// writes without it (WriteNetworkTokenWithContext).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urnetwork/sdk/v2026"
)

const networkCredentialLineageSchema = "urnetwork-network-jwt-lineage-v1"

// At a renewal every 15 days this is about ten years of renewals of one
// sign-in. The root, the explicit sign-in's own file, is never dropped.
const maximumNetworkCredentialLineage = 256

// A network token is about a kilobyte; anything near this bound is not one.
const maximumNetworkTokenBytes = 64 * 1024

// The registration store's exclusive owner lock is held by another owner.
var errRegistrationOwnerActive = errors.New("registration credential already has an active owner")

// ErrNetworkTokenBusy is a renewal that found the token file's registration
// owner lock held, for example by a measurement registration that borrows
// the token. The renewal is retried later.
var ErrNetworkTokenBusy = errors.New("the network token file is in use by a registration")

type networkCredentialLineage struct {
	Schema  string   `json:"schema"`
	Members []string `json:"members"`
}

// NetworkCredentialRejectedError is the server's confirmed rejection (401) of
// the network token that a registration or a renewal sent: the sign-in has
// expired, its credentials were rotated, or its network is gone. Only a new
// sign-in resolves it.
type NetworkCredentialRejectedError struct{ cause error }

func (self *NetworkCredentialRejectedError) Error() string {
	return "the network sign-in was rejected or has expired"
}

func (self *NetworkCredentialRejectedError) Unwrap() error { return self.cause }

// networkCredentialRejection types a confirmed rejection of the network token
// a request carried. Every other error is returned as it is.
func networkCredentialRejection(err error) error {
	if err != nil && sdk.ConfirmedClientRefreshRejection(err) {
		return &NetworkCredentialRejectedError{cause: err}
	}
	return err
}

// IsNetworkCredentialRejected reports whether err holds the server's
// confirmed rejection of a network token (NetworkCredentialRejectedError).
func IsNetworkCredentialRejected(err error) bool {
	var rejected *NetworkCredentialRejectedError
	return errors.As(err, &rejected)
}

func networkCredentialFingerprintOf(token string, modTimeNano int64, hasModTime bool) string {
	generation := strings.TrimSpace(token)
	if hasModTime {
		generation += fmt.Sprintf("\n%d", modTimeNano)
	}
	sum := sha256.Sum256([]byte(generation))
	return hex.EncodeToString(sum[:])
}

// readNetworkCredential reads the token and its fingerprint from one open
// file, so a rename between the read and the stat cannot mix two files.
func readNetworkCredential(path string) (token string, fingerprint string, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return "", "", err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumNetworkTokenBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(raw) > maximumNetworkTokenBytes {
		return "", "", fmt.Errorf("%s exceeds %d bytes", path, maximumNetworkTokenBytes)
	}
	token = strings.TrimSpace(string(raw))
	if token == "" {
		return "", "", fmt.Errorf("%s is empty", path)
	}
	return token, networkCredentialFingerprintOf(token, info.ModTime().UnixNano(), true), nil
}

// networkCredentialLineagePath is the lineage file of the token at path, next
// to the file the path resolves to.
func networkCredentialLineagePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path + ".lineage"
}

func parseNetworkCredentialLineage(raw []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var lineage networkCredentialLineage
	if err := decoder.Decode(&lineage); err != nil {
		return nil, fmt.Errorf("network token lineage is malformed: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("network token lineage has trailing bytes")
	}
	if lineage.Schema != networkCredentialLineageSchema || len(lineage.Members) == 0 || len(lineage.Members) > maximumNetworkCredentialLineage {
		return nil, errors.New("network token lineage has an unknown schema or size")
	}
	for _, member := range lineage.Members {
		raw, err := hex.DecodeString(member)
		if err != nil || len(raw) != sha256.Size || member != hex.EncodeToString(raw) {
			return nil, errors.New("network token lineage has an invalid member")
		}
	}
	return lineage.Members, nil
}

func encodeNetworkCredentialLineage(members []string) ([]byte, error) {
	return json.Marshal(networkCredentialLineage{Schema: networkCredentialLineageSchema, Members: members})
}

// appendNetworkCredentialLineage adds a renewed file's fingerprint. At the
// bound it drops the oldest renewal, never the root.
func appendNetworkCredentialLineage(members []string, fingerprint string) []string {
	if slices.Contains(members, fingerprint) {
		return members
	}
	members = slices.Clone(members)
	if len(members) >= maximumNetworkCredentialLineage {
		members = slices.Delete(members, 1, 2)
	}
	return append(members, fingerprint)
}

// readNetworkCredentialLineage returns the lineage members of the token at
// path, or none when it has no lineage file.
func readNetworkCredentialLineage(path string) ([]string, error) {
	raw, err := os.ReadFile(networkCredentialLineagePath(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseNetworkCredentialLineage(raw)
}

// networkCredentialMarkerBlocks reports whether a rejection marker still
// blocks recreating its client while the network token file has fingerprint:
// a "blocked" marker always does, a fingerprint marker does while it names the
// current file or a file of the current file's renewal lineage. An unreadable
// lineage blocks, and its error is returned for the caller to surface.
func networkCredentialMarkerBlocks(networkPath, fingerprint, marker string) (bool, error) {
	if marker == "blocked" || marker == fingerprint {
		return true, nil
	}
	members, err := readNetworkCredentialLineage(networkPath)
	if err != nil {
		return true, err
	}
	return slices.Contains(members, fingerprint) && slices.Contains(members, marker), nil
}

// NetworkJwtRenewable reports whether a token is a network token the server
// renews at /auth/network-refresh: a JWT that names a network and a user and
// no client. An API key does not expire and is not renewed, and a client token
// refreshes as a client.
func NetworkJwtRenewable(token string) bool {
	identity, err := registrationIdentity(token)
	return err == nil && identity.clientId == "" && identity.deviceId == "" &&
		validRegistrationIdentityId(identity.NetworkId) && validRegistrationIdentityId(identity.UserId)
}

// ValidateRenewedNetworkJwt checks that a renewal is a network token of the
// same network, user, roles and principal as the network token it renews.
// The server signed it; this keeps an answer from replacing the sign-in with
// another identity or with a client token.
func ValidateRenewedNetworkJwt(original, renewed string) error {
	before, beforeErr := registrationIdentity(original)
	after, afterErr := registrationIdentity(renewed)
	if beforeErr != nil || afterErr != nil ||
		before.clientId != "" || before.deviceId != "" || after.clientId != "" || after.deviceId != "" ||
		!validRegistrationIdentityId(before.NetworkId) || !validRegistrationIdentityId(before.UserId) ||
		!before.registrationPrincipal.equal(after.registrationPrincipal) {
		return errors.New("renewed network credential changed its original identity")
	}
	return nil
}

// WriteNetworkToken is WriteNetworkTokenWithContext without a context: the
// bounded wait for the owner lock still applies.
func WriteNetworkToken(path string, token string) error {
	return WriteNetworkTokenWithContext(context.Background(), path, token)
}

// ErrNetworkTokenInUse is an explicit sign-in that waited its bound for the
// token's owner lock: a running miner or validator holds it (a renewal, or a
// registration that borrows the token). Nothing was written.
var ErrNetworkTokenInUse = errors.New("the network token is in use by a running miner or validator")

func networkTokenInUse(path string) error {
	return fmt.Errorf("%w: a running miner or validator is using %s; stop it or try again", ErrNetworkTokenInUse, path)
}
