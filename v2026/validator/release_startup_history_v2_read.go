//go:build linux || darwin

// Startup inventories the complete immutable namespaces independently of any
// snapshot-selected generation or steering-intent prefix. Reading these owned
// bytes is not authentication; the history replay consumes every admitted cut.
package validator

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Filenames carry routing only. Every decoded member must independently match
// its configured identity, signed context and real historical chain clocks.
type releaseEvidenceV2HistoryFile struct {
	name    string
	epoch   uint64
	noID    uint64
	legacy  bool
	encoded []byte
}

// A missing suffix retains its first absent name under an actual parent.
// An existing namespace retains its complete finite name/native-state census.
type releaseEvidenceV2HistoryDirectory struct {
	ctx        context.Context
	root       *attemptPrivateDirectory
	absent     string
	entries    map[string]attemptPrivateFileState
	maxEntries uint64
	hooks      releaseMeasurementInputV2ReadHooks
	closed     bool
	closeErr   error
}

// All bytes are detached from the filesystem and all namespace owners remain
// held through semantic replay. A failed read closes the entire acquired set.
type releaseEvidenceV2HistoryFiles struct {
	inputs      []releaseEvidenceV2HistoryFile
	closures    []releaseEvidenceV2HistoryFile
	directories []*releaseEvidenceV2HistoryDirectory
}

// The existing history byte bound also bounds entry count before allocation;
// no candidate epoch or directory size selects an unbounded scan or slice.
func openReleaseEvidenceV2HistoryDirectory(ctx context.Context, coordinator string, suffix []string, maxEntries uint64, hooks releaseMeasurementInputV2ReadHooks) (result *releaseEvidenceV2HistoryDirectory, resultErr error) {
	if ctx == nil || maxEntries == 0 || maxEntries >= uint64(^uint(0)>>1) {
		return nil, errors.New("startup history directory context or bound is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := openAttemptPrivateDirectory(coordinator)
	if err != nil {
		return nil, err
	}
	owned := &releaseEvidenceV2HistoryDirectory{ctx: ctx, root: root, entries: map[string]attemptPrivateFileState{}, maxEntries: maxEntries, hooks: hooks}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owned.close())
			result = nil
		}
	}()
	for _, name := range suffix {
		if !validAttemptPrivateLeaf(name) {
			return nil, errors.New("startup history directory suffix is invalid")
		}
		if err := errors.Join(ctx.Err(), owned.root.check()); err != nil {
			return nil, err
		}
		before, err := owned.root.stat(name)
		if releaseMeasurementInputV2OnlyMissing(err) {
			owned.absent = name
			return owned, owned.check()
		}
		if err != nil || !before.directory() || before.mode&0o077 != 0 || before.uid != uint32(os.Geteuid()) {
			return nil, errors.Join(errors.New("startup history namespace is not a private physical directory"), err)
		}
		child, err := openAttemptPrivateDirectory(filepath.Join(owned.root.path, name))
		if err != nil {
			return nil, err
		}
		if child.anchor.dev != before.dev || child.anchor.ino != before.ino || child.anchor.mode != before.mode || child.anchor.uid != before.uid {
			return nil, errors.Join(errors.New("startup history namespace changed during acquisition"), child.close())
		}
		parent := owned.root
		owned.root = child
		file := parent.file
		closeErr := parent.close()
		if hooks.afterClose != nil {
			closeErr = errors.Join(closeErr, hooks.afterClose(file))
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	entries, err := owned.readEntries()
	if err != nil {
		return nil, err
	}
	owned.entries = entries
	return owned, owned.check()
}

// Each census scan uses a separate actual descriptor, keeping directory seek
// offsets out of ownership. Native leaf stat never follows a listed symlink.
func (self *releaseEvidenceV2HistoryDirectory) readEntries() (result map[string]attemptPrivateFileState, resultErr error) {
	if err := errors.Join(self.ctx.Err(), self.root.check()); err != nil {
		return nil, err
	}
	reader, err := openAttemptPrivateDirectory(self.root.path)
	if err != nil {
		return nil, err
	}
	defer func() {
		file := reader.file
		resultErr = errors.Join(resultErr, reader.close())
		if self.hooks.afterClose != nil {
			resultErr = errors.Join(resultErr, self.hooks.afterClose(file))
		}
		resultErr = errors.Join(resultErr, self.root.check(), self.ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if reader.anchor.dev != self.root.anchor.dev || reader.anchor.ino != self.root.anchor.ino || reader.anchor.mode != self.root.anchor.mode || reader.anchor.uid != self.root.anchor.uid {
		return nil, errors.New("startup history census reader changed namespace")
	}
	result = map[string]attemptPrivateFileState{}
	for {
		if err := self.ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := reader.file.ReadDir(1)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if uint64(len(result)) >= self.maxEntries || len(name) > 96 || !validAttemptPrivateLeaf(name) {
				return nil, errors.New("startup history directory census exceeds its finite bound")
			}
			if _, duplicate := result[name]; duplicate {
				return nil, errors.New("startup history directory census repeats a name")
			}
			state, err := self.root.stat(name)
			if err != nil || !state.regular() || state.mode&0o077 != 0 || state.uid != uint32(os.Geteuid()) || state.size < 0 {
				return nil, errors.Join(errors.New("startup history entry is not a private regular file"), err)
			}
			result[name] = state
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if err := self.hooks.observe("history-census-read", reader.file, 0); err != nil {
		return nil, err
	}
	return result, nil
}

// Neither a late new input nor a removed terminal may shorten the replayed
// prefix. File identity includes size, ownership, link count and native times.
func (self *releaseEvidenceV2HistoryDirectory) check() error {
	if self == nil || self.closed || self.root == nil {
		return errors.New("startup history directory owner is closed")
	}
	if err := errors.Join(self.ctx.Err(), self.root.check()); err != nil {
		return err
	}
	if self.absent != "" {
		_, err := self.root.stat(self.absent)
		if releaseMeasurementInputV2OnlyMissing(err) {
			return nil
		}
		return errors.Join(errors.New("startup history namespace appeared after its absence"), err)
	}
	entries, err := self.readEntries()
	if err != nil || !maps.Equal(entries, self.entries) {
		return errors.Join(errors.New("startup history complete file census changed"), err)
	}
	// The last observer/Close above cannot mutate an entry after its checked
	// stat. A final scan has no callbacks and compares the entire same census.
	final := *self
	final.hooks = releaseMeasurementInputV2ReadHooks{}
	entries, err = final.readEntries()
	if err != nil || !maps.Equal(entries, self.entries) {
		return errors.Join(errors.New("startup history census changed after actual reader Close"), err)
	}
	return nil
}

// Every real Close runs once. The final native witness has no observers, so
// even an after-Close namespace replacement is joined before bytes escape.
func (self *releaseEvidenceV2HistoryDirectory) close() error {
	if self == nil || self.closed {
		if self == nil {
			return nil
		}
		return self.closeErr
	}
	self.closed = true
	if self.root == nil {
		return nil
	}
	root, file := self.root, self.root.file
	self.closeErr = root.close()
	if self.hooks.afterClose != nil {
		self.closeErr = errors.Join(self.closeErr, self.hooks.afterClose(file))
	}
	witness, err := openAttemptPrivateDirectory(root.path)
	self.closeErr = errors.Join(self.closeErr, err)
	if err != nil {
		return self.closeErr
	}
	if witness.anchor.dev != root.anchor.dev || witness.anchor.ino != root.anchor.ino || witness.anchor.mode != root.anchor.mode || witness.anchor.uid != root.anchor.uid {
		self.closeErr = errors.Join(self.closeErr, errors.New("startup history namespace changed after actual Close"))
	} else {
		final := &releaseEvidenceV2HistoryDirectory{ctx: context.WithoutCancel(self.ctx), root: witness, absent: self.absent, entries: self.entries, maxEntries: self.maxEntries}
		self.closeErr = errors.Join(self.closeErr, final.check())
	}
	self.closeErr = errors.Join(self.closeErr, witness.close(), self.ctx.Err())
	return self.closeErr
}

// Exact fixed decimal filenames are distinct from the explicitly retained
// legacy namespace. A temporary inode is never a committed history member.
func releaseEvidenceV2HistoryInputName(name string) (epoch, noID uint64, legacy bool, err error) {
	legacy = !strings.HasSuffix(name, "-v2.json")
	suffix := "-v2.json"
	if legacy {
		suffix = ".json"
	}
	if len(name) != len("subnet-")+20+len("-no-")+20+len(suffix) || !strings.HasPrefix(name, "subnet-") || name[27:31] != "-no-" || !strings.HasSuffix(name, suffix) {
		return 0, 0, false, errors.New("startup history input filename is not canonical")
	}
	epoch, epochErr := strconv.ParseUint(name[7:27], 10, 64)
	noID, noErr := strconv.ParseUint(name[31:51], 10, 64)
	want := fmt.Sprintf("subnet-%020d-no-%020d%s", epoch, noID, suffix)
	if epochErr != nil || noErr != nil || noID == 0 || want != name {
		return 0, 0, false, errors.Join(errors.New("startup history input filename identity is invalid"), epochErr, noErr)
	}
	return epoch, noID, legacy, nil
}

// Crash-leftover temporary names are recognized exactly and counted/bounded,
// but are not promoted to cuts and are never deleted by this read-only owner.
func releaseEvidenceV2HistoryTemporary(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+32 {
		return false
	}
	decoded, err := hex.DecodeString(name[len(prefix):])
	return err == nil && hex.EncodeToString(decoded) == name[len(prefix):]
}

// Acquire both namespaces before decoding any candidate. The aggregate byte
// allowance covers every observed file, including crash-leftover temporaries.
func readReleaseEvidenceV2HistoryFiles(ctx context.Context, coordinator string, bounds ReleaseEvidenceV2Bounds, hooks releaseMeasurementInputV2ReadHooks) (result *releaseEvidenceV2HistoryFiles, resultErr error) {
	if ctx == nil {
		return nil, errors.New("startup history file context is absent")
	}
	if err := validateReleaseMeasurementInputV2Limit(bounds.MaxHistoryBytes); err != nil {
		return nil, err
	}
	for _, limit := range []uint64{bounds.MaxInputJournalBytes, bounds.MaxClosureBytes} {
		if err := validateReleaseMeasurementInputV2Limit(limit); err != nil {
			return nil, err
		}
	}
	owned := &releaseEvidenceV2HistoryFiles{}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owned.close())
			result = nil
		}
	}()
	remaining := bounds.MaxHistoryBytes
	for index, suffix := range [][]string{{"measurements", "inputs"}, {"settlement-closures-v2"}} {
		directory, err := openReleaseEvidenceV2HistoryDirectory(ctx, coordinator, suffix, bounds.MaxHistoryBytes, hooks)
		if err != nil {
			return nil, err
		}
		owned.directories = append(owned.directories, directory)
		if directory.absent != "" {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(directory.entries)) {
			state := directory.entries[name]
			if uint64(state.size) > remaining {
				return nil, errors.New("startup complete history exceeds its aggregate byte bound")
			}
			remaining -= uint64(state.size)
			prefix, limit := ".compact-input-", bounds.MaxInputJournalBytes
			if index == 1 {
				prefix, limit = ".closure-", bounds.MaxClosureBytes
			}
			if uint64(state.size) > limit {
				return nil, errors.New("startup history member exceeds its existing typed byte bound")
			}
			if releaseEvidenceV2HistoryTemporary(name, prefix) {
				continue
			}
			member := releaseEvidenceV2HistoryFile{name: name}
			if index == 0 {
				member.epoch, member.noID, member.legacy, err = releaseEvidenceV2HistoryInputName(name)
			} else {
				member.epoch, err = strconv.ParseUint(strings.TrimSuffix(name, ".json"), 10, 64)
				if err == nil && strconv.FormatUint(member.epoch, 10)+".json" != name {
					err = errors.New("startup terminal history filename is not canonical")
				}
			}
			if err != nil {
				return nil, err
			}
			path := filepath.Join(directory.root.path, name)
			owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, limit, hooks, false)
			if err != nil {
				return nil, errors.Join(err, owner.finish())
			}
			member.encoded, err = owner.read()
			finishErr := owner.finish()
			if err != nil || finishErr != nil || owner.witness == nil || *owner.witness != state {
				return nil, errors.Join(errors.New("startup history member changed around bounded read"), err, finishErr)
			}
			if index == 0 {
				owned.inputs = append(owned.inputs, member)
			} else {
				owned.closures = append(owned.closures, member)
			}
		}
	}
	return owned, owned.check()
}

// Every namespace is rechecked even when an earlier check has failed.
func (self *releaseEvidenceV2HistoryFiles) check() error {
	if self == nil {
		return errors.New("startup history file owner is absent")
	}
	var failures []error
	for _, directory := range self.directories {
		failures = append(failures, directory.check())
	}
	return errors.Join(failures...)
}

// Close joins all retained directories, including a partially read census.
func (self *releaseEvidenceV2HistoryFiles) close() error {
	if self == nil {
		return nil
	}
	var failures []error
	for _, directory := range self.directories {
		failures = append(failures, directory.close())
	}
	return errors.Join(failures...)
}
