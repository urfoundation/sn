// Package durablehead retains one application snapshot's acknowledged physical
// generation. Enrollment is external and explicit; admission never creates a
// missing checkpoint, marker or history. Replaying all valid predecessor bytes
// is outside this local continuity contract.
package durablehead

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
)

const Schema = "urnetwork-durable-snapshot-head-v2"

// Actual publication may have completed. Join the old owner before attempting
// one bounded reconciliation of the retained pending bytes.
var ErrUncertain = errors.New("snapshot publication requires joined reconciliation")

// Passive custody inspection never acquires publication or reconciliation
// authority, even when its caller also holds a writable volume declaration.
var ErrReadOnly = errors.New("snapshot owner is read-only")

// Every name is one basename relative to the independently guarded directory.
// LockName is empty only when the existing application flocks that directory.
// Auxiliary markers preexist; their byte grammar remains application-owned.
type Spec struct {
	Kind           string
	Name           string
	MaximumBytes   int64
	LockName       string
	AuxiliaryNames []string
}

// Each kind/name has a distinct bounded external authority. A file-lock owner
// keeps it on that preprovisioned lock; a directory-flock owner uses its directory.
func Attribute(kind, name string) string {
	digest := sha256.Sum256([]byte(kind + "\x00" + name))
	return "user.urnetwork.snapshot." + hex.EncodeToString(digest[:])
}

// These records are also the provisioning/backup format. Runtime writers only
// replace a previously admitted attribute; they never install a missing one.
type Checkpoint struct {
	Schema         string      `json:"schema"`
	Kind           string      `json:"kind"`
	Name           string      `json:"name"`
	MaximumBytes   int64       `json:"maximum_bytes"`
	DirectoryInode uint64      `json:"directory_inode"`
	LockName       string      `json:"lock_name"`
	Auxiliaries    []Auxiliary `json:"auxiliaries"`
	Committed      Member      `json:"committed"`
	Pending        *Pending    `json:"pending,omitempty"`
}

type Auxiliary struct {
	Name  string `json:"name"`
	Inode uint64 `json:"inode"`
}

type Member struct {
	Present bool   `json:"present"`
	Inode   uint64 `json:"inode"`
	Size    int64  `json:"size"`
	Sha256  string `json:"sha256"`
}

// A single retained temporary is the only recoverable unfinished operation.
type Pending struct {
	Temporary string `json:"temporary"`
	Next      Member `json:"next"`
}

// Observers run after the named real I/O and cannot replace write/rename/sync.
// They preserve existing application failure barriers without global hooks.
type PublicationHooks struct {
	AfterFileSync      func() error
	AfterRename        func() error
	AfterDirectorySync func(*os.File) error
}
