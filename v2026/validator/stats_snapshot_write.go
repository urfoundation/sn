package validator

// Snapshot bytes, their actual schema version and the destination travel as
// one private value. Native custody is independent of snapshot encoding; the
// existing successful-write/late-cancellation publication boundary is kept.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// The version is taken from the same snapshot passed to the real encoder,
// never inferred from an operation label, ledger backend or caller path.
type statsSnapshotWrite struct {
	path    string
	data    []byte
	version int
}

// Observers follow real syscalls and cannot replace them. They run outside
// Stats.mu; injected errors are error-contract controls, not physical errors.
type statsSnapshotIOHooks struct {
	after func(stage string, file *os.File) error
}

// An optional observer cannot hide an actual syscall error.
func (self statsSnapshotIOHooks) observe(stage string, file *os.File) error {
	if self.after != nil {
		return self.after(stage, file)
	}
	return nil
}

// Actual Close happens exactly once before the user-visible observer.
func (self statsSnapshotIOHooks) close(stage string, file *os.File) error {
	return errors.Join(file.Close(), self.observe(stage, file))
}

// Pre-acquire without creating or chmod-ing anything, before actual replay or
// journal callbacks. Every later write must use this exact retained owner.
func (self *statsWriteOwner) prepareSnapshot(dir string, physical bool) error {
	if self.snapshot != nil {
		return errors.New("statistics snapshot owner was prepared twice")
	}
	if physical {
		if err := validateStatsSnapshotPhysicalDirectory(dir); err != nil {
			return err
		}
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	owner, err := acquireStatsSnapshotDirectory(filepath.Join(dir, "stats.json"), physical, self.hooks.snapshotIO)
	self.snapshot = owner
	return err
}

// New authority is an exact physical namespace, not a candidate to normalize.
// Legacy-only relative/alias compatibility lives in its separate admission.
func validateStatsSnapshotPhysicalDirectory(dir string) error {
	if !utf8.ValidString(dir) || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || filepath.Dir(dir) == dir {
		return errors.New("statistics v2 snapshot directory must be canonical physical absolute non-root UTF-8")
	}
	return nil
}

// Cleanup is once-owned. Callers join its result before publication; repeat
// calls only return the already established completion result.
func (self *statsWriteOwner) finishSnapshot() error {
	if self.snapshot == nil {
		return nil
	}
	return self.snapshot.finish()
}

// An external after-save journal callback can change the snapshot namespace
// even after its own writer has closed. Recheck its copied final witness.
func (self *statsWriteOwner) checkSnapshot() error {
	if self.snapshot == nil {
		return nil
	}
	return self.snapshot.checkFinal()
}

// Legacy batch callbacks already receive canonically encoded snapshot bytes.
// Read only the leading version field rather than allocate/decode the whole
// state again. The real encoder always emits v first; other header layouts
// are refused. Complete snapshot validation belongs to the existing caller.
func encodedStatsSnapshotWrite(path string, data []byte) (statsSnapshotWrite, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return statsSnapshotWrite{}, errors.Join(errors.New("statistics snapshot encoding has no object header"), err)
	}
	field, err := decoder.Token()
	if err != nil || field != "v" {
		return statsSnapshotWrite{}, errors.Join(errors.New("statistics snapshot encoding has no leading version"), err)
	}
	var version int
	if err := decoder.Decode(&version); err != nil {
		return statsSnapshotWrite{}, err
	}
	if version < 1 || version > 6 {
		return statsSnapshotWrite{}, errors.New("statistics snapshot encoding version is unsupported")
	}
	return statsSnapshotWrite{path: path, data: data, version: version}, nil
}

// Individual legacy batch snapshot callbacks use the same native writer.
// Their separate coordinator journal/all-target recovery custody is not
// established by this single-file helper.
func writeEncodedStatsSnapshot(path string, data []byte) (resultErr error) {
	write, err := encodedStatsSnapshotWrite(path, data)
	if err != nil {
		return err
	}
	owner, err := acquireStatsSnapshotDirectory(path, write.version >= 6, statsSnapshotIOHooks{})
	if owner != nil {
		defer func() { resultErr = errors.Join(resultErr, owner.finish()) }()
	}
	if err != nil {
		return err
	}
	return writeStatsSnapshotOwned(owner, write)
}
