//go:build linux || darwin

package validator

// A final witness is operation-owned, not an anchor borrowed from an object
// passed to a close callback. All observable closes finish before any final
// validation, so the last callback cannot invalidate an earlier root's check.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
)

// Absence is an exact expected state only after an admitted observation or
// successful native journal removal, never a fallback for a late read error.
type attemptSettlementV2LeafWitness struct {
	state attemptPrivateFileState
	exists bool
}

// Directory identity omits mutable entry counts/timestamps, while metadata
// leaves retain their complete write-state. Both are copied before callbacks.
type attemptSettlementV2RootWitness struct {
	anchor attemptPrivateFileState
	leaves map[string]attemptSettlementV2LeafWitness
}

// Values survive physical owner removal. The once-owned cleanup result is
// stable across explicit completion and deferred failure cleanup.
type attemptSettlementV2WitnessRegistry struct {
	roots map[string]*attemptSettlementV2RootWitness
	closed map[*attemptPrivateDirectory]bool
	owners []*attemptPrivateDirectory
	closeErr error
	finished bool
	resultErr error
}

// Runtime operations replace any caller's previous registry at admission.
func (self attemptSettlementV2IO) withWitnesses(ctx context.Context) attemptSettlementV2IO {
	self.ctx = ctx
	self.witnesses = &attemptSettlementV2WitnessRegistry{roots: map[string]*attemptSettlementV2RootWitness{}, closed: map[*attemptPrivateDirectory]bool{}}
	return self
}

// No callback may supply the identity used by the final witness.
func (self attemptSettlementV2IO) retainWitness(root *attemptPrivateDirectory) error {
	if self.witnesses == nil { return nil } // Explicit old v1 compatibility owner.
	if self.witnesses.finished || root == nil || root.file == nil { return errors.New("compact witness owner is unavailable or already finished") }
	if _, exists := self.witnesses.closed[root]; !exists {
		self.witnesses.closed[root] = false
		self.witnesses.owners = append(self.witnesses.owners, root)
	}
	anchor := root.anchor
	if old := self.witnesses.roots[root.path]; old != nil {
		if old.anchor.dev != anchor.dev || old.anchor.ino != anchor.ino || old.anchor.mode != anchor.mode || old.anchor.uid != anchor.uid { return errors.New("compact witness directory changed within operation") }
		return nil
	}
	self.witnesses.roots[root.path] = &attemptSettlementV2RootWitness{anchor: anchor, leaves: map[string]attemptSettlementV2LeafWitness{}}
	return nil
}

// Writes may replace an admitted mutable image, but only the writer's exact
// bounded readback grants a new expected leaf. Late Close checks cannot update it.
func (self attemptSettlementV2IO) witnessLeaf(root *attemptPrivateDirectory, name string, expected attemptSettlementV2LeafWitness) error {
	if self.witnesses == nil { return nil }
	if err := self.retainWitness(root); err != nil { return err }
	self.witnesses.roots[root.path].leaves[name] = expected
	return nil
}

// Closes are attempted once even when a real close or later observer fails.
func (self attemptSettlementV2IO) closeOwnedRoot(root *attemptPrivateDirectory) error {
	if self.witnesses != nil {
		if self.witnesses.closed[root] { return nil }
		self.witnesses.closed[root] = true
	}
	err := self.closeRoot(root)
	if self.witnesses != nil { self.witnesses.closeErr = errors.Join(self.witnesses.closeErr, err) }
	return err
}

// There are no hooks in this final pass. Even after one root fails, validate
// and join every other root and every real witness Close before returning.
func (self attemptSettlementV2IO) finishWitnesses() error {
	if self.witnesses == nil { return nil }
	registry := self.witnesses
	if registry.finished { return registry.resultErr }
	registry.finished = true
	paths := make([]string, 0, len(registry.roots))
	for path := range registry.roots { paths = append(paths, path) }
	slices.Sort(paths)
	for _, path := range paths {
		expected := registry.roots[path]
		root, err := openAttemptPrivateDirectory(path)
		if err != nil { registry.resultErr = errors.Join(registry.resultErr, fmt.Errorf("compact final directory witness %s: %w", path, err)); continue }
		anchor := root.anchor
		if anchor.dev != expected.anchor.dev || anchor.ino != expected.anchor.ino || anchor.mode != expected.anchor.mode || anchor.uid != expected.anchor.uid {
			registry.resultErr = errors.Join(registry.resultErr, fmt.Errorf("compact final directory witness changed: %s", path))
		} else {
			names := make([]string, 0, len(expected.leaves))
			for name := range expected.leaves { names = append(names, name) }
			slices.Sort(names)
			for _, name := range names {
				leaf := expected.leaves[name]
				current, statErr := root.stat(name)
				if !leaf.exists && errors.Is(statErr, os.ErrNotExist) { continue }
				if statErr != nil || !leaf.exists || current != leaf.state { registry.resultErr = errors.Join(registry.resultErr, fmt.Errorf("compact final leaf witness changed: %s/%s", path, name), statErr) }
			}
		}
		registry.resultErr = errors.Join(registry.resultErr, root.close())
	}
	if self.ctx != nil { registry.resultErr = errors.Join(registry.resultErr, self.ctx.Err()) }
	return registry.resultErr
}

// New v2 reads always carry an explicit context; legacy compatibility owns its
// separate adapter directly and does not gain an implicit persistence bound.
func (self attemptSettlementV2IO) context() context.Context {
	if self.ctx != nil { return self.ctx }
	return context.Background() // Standalone metadata setup/read helpers only.
}

// A physical callback cannot silently publish a different image and then
// have that image blessed as the final expected leaf merely by returning nil.
func (self attemptSettlementV2IO) writeImage(root *attemptPrivateDirectory, name string, data []byte, limit uint64, write func(*attemptPrivateDirectory, string, []byte) error) error {
	if err := validateAttemptSettlementV2MetadataLimit(limit); err != nil { return err }
	if uint64(len(data)) > limit { return errors.New("compact persisted image exceeds its independent byte allowance") }
	if err := self.context().Err(); err != nil { return err }
	if err := write(root, name, data); err != nil { return err }
	actual, exists, err := readAttemptSettlementV2File(root, name, limit, self)
	if err != nil || !exists || !bytes.Equal(data, actual) { return errors.Join(errors.New("compact persisted image differs from exact writer postimage"), err) }
	return self.context().Err()
}

// Missing is admitted only after successful owned removal and a real stat.
func (self attemptSettlementV2IO) removeOwnedJournal(root *attemptPrivateDirectory, name string) error {
	if err := self.removeJournal(root, name); err != nil { return err }
	if _, err := root.stat(name); !errors.Is(err, os.ErrNotExist) { return errors.Join(errors.New("compact removed journal is not absent"), err) }
	if err := root.check(); err != nil { return err }
	return self.witnessLeaf(root, name, attemptSettlementV2LeafWitness{})
}
