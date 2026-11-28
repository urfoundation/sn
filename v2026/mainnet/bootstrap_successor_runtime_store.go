// The additive artifact journal shares the existing exclusive custody owner.
// Fixed ordinal files and hash-bound stages retain exact prefix recovery.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

const bootstrapSuccessorRuntimeFilePrefix = bootstrapSuccessorExecutionPrefix + ".runtime-"

// Only one incomplete next revision is recoverable. Its signed seal is retained
// even when no bytes reached the stage; a valid alternative cannot replace it.
type bootstrapSuccessorRuntimeHistory struct {
	approvals   []bootstrapSuccessorRuntimeApproval
	pendingHash string
}

// Five digits cover the uint16 wire sequence without limiting artifact history
// to the checkpoint reader's per-call allowlist. The directory census is bounded.
func bootstrapSuccessorRuntimeName(sequence uint16) string {
	return fmt.Sprintf("%s%05d.json", bootstrapSuccessorRuntimeFilePrefix, sequence)
}

// Hashes in the stage kind fence competing approvals before the first write.
func bootstrapSuccessorRuntimeStageKind(hash string) string {
	return "runtime-revision-" + strings.TrimPrefix(hash, "sha256:")
}

// Reauthenticate every retained signature, evidence file and predecessor. The
// bounded complete name census detects gaps, forks and unrecognized stages.
func (self *bootstrapSuccessorExecutionStore) readRuntimeHistory(ctx context.Context, names []string, allowed map[string]bool) (bootstrapSuccessorRuntimeHistory, error) {
	var history bootstrapSuccessorRuntimeHistory
	for sequence := 1; sequence <= len(names)+1; sequence++ {
		name := bootstrapSuccessorRuntimeName(uint16(sequence))
		raw, err := self.local.read(name)
		if errors.Is(err, os.ErrNotExist) {
			prefix := self.local.stageName(name, "runtime-revision-")
			for _, candidate := range names {
				if !strings.HasPrefix(candidate, prefix) {
					continue
				}
				hash := "sha256:" + strings.TrimPrefix(candidate, prefix)
				if history.pendingHash != "" || !planSha256(hash) || self.canonicalAuthority == nil {
					return history, errors.New("successor runtime revision has conflicting or unscoped partial authority")
				}
				partial, err := self.local.read(candidate)
				if err != nil || len(partial) > maximumBootstrapSuccessorRuntimeBytes {
					return history, errors.Join(errors.New("successor runtime revision stage is unavailable or oversized"), err)
				}
				history.pendingHash, allowed[candidate] = hash, true
			}
			break
		}
		if err != nil {
			return history, err
		}
		if self.canonicalAuthority == nil || len(raw) == 0 || len(raw) > maximumBootstrapSuccessorRuntimeBytes {
			return history, errors.New("successor runtime revision lacks its base or exceeds its byte bound")
		}
		var approval bootstrapSuccessorRuntimeApproval
		if err := decodePlanJson(raw, &approval); err != nil {
			return history, err
		}
		canonical, err := json.Marshal(approval)
		if err != nil || !bytes.Equal(raw, canonical) {
			return history, errors.Join(errors.New("successor runtime revision encoding differs"), err)
		}
		if err := errors.Join(approval.validate(ctx, self.planCopy(), *self.canonicalAuthority), approval.extends(*self.canonicalAuthority, history.approvals)); err != nil {
			return history, err
		}
		history.approvals, allowed[name] = append(history.approvals, approval), true
	}
	for _, name := range names {
		if (strings.HasPrefix(name, bootstrapSuccessorRuntimeFilePrefix) || strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix+bootstrapSuccessorRuntimeFilePrefix)) && !allowed[name] {
			return history, errors.New("successor runtime history has a gap, fork or extra stage")
		}
	}
	return history, ctx.Err()
}

// An empty optional event reference represents the immutable base profile.
func (self bootstrapSuccessorRuntimeHistory) hash() string {
	if len(self.approvals) == 0 {
		return ""
	}
	return rootObjectHash(self.approvals[len(self.approvals)-1])
}

// Old event encodings remain unchanged while every new reference resolves to
// complete retained authority and never rolls back a preceding event's history.
func (self bootstrapSuccessorRuntimeHistory) validateEvent(event bootstrapSuccessorExecutionEvent, previous *bootstrapSuccessorExecutionEvent) error {
	positions := map[string]int{"": 0}
	for i, approval := range self.approvals {
		positions[rootObjectHash(approval)] = i + 1
	}
	position, exists := positions[event.RuntimeRevisionHash]
	if !exists {
		return errors.New("successor counted event lost its runtime revision authority")
	}
	if previous != nil {
		prior, exists := positions[previous.RuntimeRevisionHash]
		if !exists || position < prior {
			return errors.New("successor execution event rolled back runtime revision authority")
		}
	}
	return nil
}

// Fresh snapshots prevent an owner or adapter from silently adopting files
// added, removed or changed during its lifetime. Import is the sole transition.
func (self *bootstrapSuccessorExecutionStore) checkpointRuntimeHistory() error {
	names, err := self.local.names()
	if err != nil {
		return err
	}
	if self.canonicalAuthority != nil {
		raw, err := self.local.read(bootstrapSuccessorCanonicalFile)
		expected, encodeErr := json.Marshal(self.canonicalAuthority)
		if err != nil || encodeErr != nil || !bytes.Equal(raw, expected) || rootObjectHash(self.canonicalAuthority) != self.canonicalAuthorityHash {
			return errors.Join(errors.New("successor runtime history lost its immutable canonical authority"), err, encodeErr)
		}
	}
	history, err := self.readRuntimeHistory(self.local.ctx, names, map[string]bool{})
	if err != nil || !slices.Equal(history.approvals, self.runtimeHistory.approvals) || history.pendingHash != self.runtimeHistory.pendingHash {
		return errors.Join(errors.New("successor runtime history changed during ownership"), err)
	}
	return nil
}

// Import never rewrites old custody. A pending terminal intent must finish
// first so adding authority cannot change its deterministic reconstructed bytes.
func (self *bootstrapSuccessorExecutionStore) retainRuntimeRevision(ctx context.Context, approval bootstrapSuccessorRuntimeApproval) error {
	if ctx == nil || self == nil || self.closed || self.canonicalAuthority == nil {
		return errors.New("successor runtime revision requires retained canonical ownership")
	}
	if err := errors.Join(ctx.Err(), self.checkpoint("runtime-revision-admission"), approval.validate(ctx, self.planCopy(), *self.canonicalAuthority)); err != nil {
		return err
	}
	index := int(approval.Authorization.Sequence) - 1
	if index >= 0 && index < len(self.runtimeHistory.approvals) {
		if approval != self.runtimeHistory.approvals[index] {
			return errors.New("successor runtime revision changed retained authority")
		}
		return nil
	}
	if self.pending != "" {
		return errors.New("successor runtime revision cannot change an interrupted execution outcome")
	}
	if self.safeCurrentHistory.pendingHash != "" {
		return errors.New("successor runtime revision cannot change an interrupted current-policy revision")
	}
	for _, revision := range self.safeCurrentHistory.approvals {
		if approval.Authorization.RuntimeEvidence.Path == revision.Authorization.Proposal.Authorization.ReviewEvidence.Path {
			return errors.New("successor runtime revision reuses retained current-policy review evidence")
		}
	}
	if err := approval.extends(*self.canonicalAuthority, self.runtimeHistory.approvals); err != nil {
		return err
	}
	hash := rootObjectHash(approval)
	if self.runtimeHistory.pendingHash != "" && self.runtimeHistory.pendingHash != hash {
		return errors.New("successor runtime revision differs from its retained partial authority")
	}
	raw, err := json.Marshal(approval)
	if err != nil || len(raw) > maximumBootstrapSuccessorRuntimeBytes {
		return errors.Join(errors.New("successor runtime revision exceeds its byte bound"), err)
	}
	if err := self.local.publish(bootstrapSuccessorRuntimeName(approval.Authorization.Sequence), bootstrapSuccessorRuntimeStageKind(hash), raw); err != nil {
		return errors.Join(err, self.close())
	}
	self.runtimeHistory.approvals = append(self.runtimeHistory.approvals, approval)
	self.runtimeHistory.pendingHash = ""
	return self.checkpoint("runtime-revision-retained")
}

// The full history remains available; only an inclusion/parent pair is passed
// to any individual CRv4 checkpoint read.
func (self *bootstrapSuccessorExecutionStore) runtimeProfiles() []rootReceiptProfile {
	if self.canonicalAuthority == nil {
		return nil
	}
	profiles := []rootReceiptProfile{self.canonicalAuthority.Authorization.CurrentRuntime}
	for _, revision := range self.runtimeHistory.approvals {
		profiles = append(profiles, revision.Authorization.Runtime)
	}
	return profiles
}
