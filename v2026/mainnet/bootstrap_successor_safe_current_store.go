// Current-policy acceptance shares the original exclusive custody owner. Its
// journal changes authority without releasing any signed transaction liability.
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

const bootstrapSuccessorSafeCurrentFilePrefix = bootstrapSuccessorExecutionPrefix + ".safe-current-"

// An incomplete revision keeps its exact signed hash even before the first byte.
type bootstrapSuccessorSafeCurrentHistory struct {
	approvals   []bootstrapSuccessorSafeCurrentRevisionApproval
	pendingHash string
}

// Fixed ordinals and a bounded directory census expose gaps and extra stages.
func bootstrapSuccessorSafeCurrentName(sequence uint16) string {
	return fmt.Sprintf("%s%05d.json", bootstrapSuccessorSafeCurrentFilePrefix, sequence)
}

// A partial publication cannot be completed with a competing signed revision.
func bootstrapSuccessorSafeCurrentStageKind(hash string) string {
	return "safe-current-revision-" + strings.TrimPrefix(hash, "sha256:")
}

// Reopening authenticates every retained proposal, acceptance and predecessor.
func (self *bootstrapSuccessorExecutionStore) readSafeCurrentHistory(ctx context.Context, names []string, allowed map[string]bool) (bootstrapSuccessorSafeCurrentHistory, error) {
	var history bootstrapSuccessorSafeCurrentHistory
	for sequence := 1; sequence <= len(names)+1; sequence++ {
		name := bootstrapSuccessorSafeCurrentName(uint16(sequence))
		raw, err := self.local.read(name)
		if errors.Is(err, os.ErrNotExist) {
			prefix := self.local.stageName(name, "safe-current-revision-")
			for _, candidate := range names {
				if !strings.HasPrefix(candidate, prefix) {
					continue
				}
				hash := "sha256:" + strings.TrimPrefix(candidate, prefix)
				if history.pendingHash != "" || !planSha256(hash) || self.canonicalAuthority == nil {
					return history, errors.New("successor current policy has conflicting or unscoped partial authority")
				}
				partial, err := self.local.read(candidate)
				if err != nil || len(partial) > maximumBootstrapSuccessorSafeCurrentRevisionBytes {
					return history, errors.Join(errors.New("successor current-policy stage is unavailable or oversized"), err)
				}
				history.pendingHash, allowed[candidate] = hash, true
			}
			break
		}
		if err != nil {
			return history, err
		}
		if self.canonicalAuthority == nil || len(raw) == 0 || len(raw) > maximumBootstrapSuccessorSafeCurrentRevisionBytes {
			return history, errors.New("successor current policy lacks its base or exceeds its byte bound")
		}
		var approval bootstrapSuccessorSafeCurrentRevisionApproval
		if err := decodePlanJson(raw, &approval); err != nil {
			return history, err
		}
		canonical, err := json.Marshal(approval)
		if err != nil || !bytes.Equal(raw, canonical) {
			return history, errors.Join(errors.New("successor current-policy encoding differs"), err)
		}
		if _, err := approval.validate(ctx, self.planCopy(), *self.canonicalAuthority, self.runtimeHistory); err != nil {
			return history, err
		}
		if err := approval.extends(*self.canonicalAuthority, self.runtimeHistory, history.approvals); err != nil {
			return history, err
		}
		history.approvals, allowed[name] = append(history.approvals, approval), true
	}
	for _, name := range names {
		if (strings.HasPrefix(name, bootstrapSuccessorSafeCurrentFilePrefix) || strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix+bootstrapSuccessorSafeCurrentFilePrefix)) && !allowed[name] {
			return history, errors.New("successor current-policy history has a gap, fork or extra stage")
		}
	}
	return history, ctx.Err()
}

// Empty references preserve the previous complete-history policy and old bytes.
func (self bootstrapSuccessorSafeCurrentHistory) hash() string {
	if len(self.approvals) == 0 {
		return ""
	}
	return rootObjectHash(self.approvals[len(self.approvals)-1])
}

// Counted attempts name complete policy authority. Their outcomes retain that
// exact reference even after another revision was imported for a later attempt.
func (self bootstrapSuccessorSafeCurrentHistory) validateEvent(event bootstrapSuccessorExecutionEvent, previous *bootstrapSuccessorExecutionEvent, runtime bootstrapSuccessorRuntimeHistory) error {
	positions := map[string]int{"": 0}
	for i, approval := range self.approvals {
		positions[rootObjectHash(approval)] = i + 1
	}
	position, exists := positions[event.SafeCurrentRevisionHash]
	if !exists {
		return errors.New("successor counted event lost its current-policy revision authority")
	}
	if previous != nil {
		prior, exists := positions[previous.SafeCurrentRevisionHash]
		if !exists || position < prior || event.Phase != "attempt-reserved" && event.SafeCurrentRevisionHash != previous.SafeCurrentRevisionHash {
			return errors.New("successor execution event changed counted current-policy authority")
		}
	}
	if position > 0 {
		policyRuntime, err := runtime.prefix(self.approvals[position-1].Authorization.Proposal.Authorization.RuntimeRevisionHash)
		eventRuntime, eventErr := runtime.prefix(event.RuntimeRevisionHash)
		if err != nil || eventErr != nil || len(policyRuntime.approvals) > len(eventRuntime.approvals) {
			return errors.Join(errors.New("successor event predates its current-policy runtime authority"), err, eventErr)
		}
	}
	return nil
}

// Rechecking cannot silently adopt changed, added or missing authority files.
func (self *bootstrapSuccessorExecutionStore) checkpointSafeCurrentHistory() error {
	names, err := self.local.names()
	if err != nil {
		return err
	}
	history, err := self.readSafeCurrentHistory(self.local.ctx, names, map[string]bool{})
	if err != nil || !slices.Equal(history.approvals, self.safeCurrentHistory.approvals) || history.pendingHash != self.safeCurrentHistory.pendingHash {
		return errors.Join(errors.New("successor current-policy history changed during ownership"), err)
	}
	return nil
}

// Import preserves all prior events and liabilities. Runtime publication and
// terminal intents finish first; interruption closes the owner for exact reopen.
func (self *bootstrapSuccessorExecutionStore) retainSafeCurrentRevision(ctx context.Context, approval bootstrapSuccessorSafeCurrentRevisionApproval) error {
	if ctx == nil || self == nil || self.closed || self.canonicalAuthority == nil {
		return errors.New("successor current-policy revision requires retained canonical ownership")
	}
	if err := errors.Join(ctx.Err(), self.checkpoint("safe-current-revision-admission")); err != nil {
		return err
	}
	if _, err := approval.validate(ctx, self.planCopy(), *self.canonicalAuthority, self.runtimeHistory); err != nil {
		return err
	}
	index := int(approval.Authorization.Sequence) - 1
	if index >= 0 && index < len(self.safeCurrentHistory.approvals) {
		if approval != self.safeCurrentHistory.approvals[index] {
			return errors.New("successor current-policy revision changed retained authority")
		}
		return nil
	}
	if self.pending != "" || self.runtimeHistory.pendingHash != "" {
		return errors.New("successor current policy cannot change an interrupted outcome or runtime revision")
	}
	if approval.Authorization.Proposal.Authorization.RuntimeRevisionHash != self.runtimeHistory.hash() {
		return errors.New("successor new current policy must bind the complete retained runtime tip")
	}
	if err := approval.extends(*self.canonicalAuthority, self.runtimeHistory, self.safeCurrentHistory.approvals); err != nil {
		return err
	}
	hash := rootObjectHash(approval)
	if self.safeCurrentHistory.pendingHash != "" && self.safeCurrentHistory.pendingHash != hash {
		return errors.New("successor current-policy revision differs from its retained partial authority")
	}
	raw, err := json.Marshal(approval)
	if err != nil || len(raw) > maximumBootstrapSuccessorSafeCurrentRevisionBytes {
		return errors.Join(errors.New("successor current-policy revision exceeds its byte bound"), err)
	}
	if err := self.local.publish(bootstrapSuccessorSafeCurrentName(approval.Authorization.Sequence), bootstrapSuccessorSafeCurrentStageKind(hash), raw); err != nil {
		return errors.Join(err, self.close())
	}
	self.safeCurrentHistory.approvals = append(self.safeCurrentHistory.approvals, approval)
	self.safeCurrentHistory.pendingHash = ""
	return self.checkpoint("safe-current-revision-retained")
}
