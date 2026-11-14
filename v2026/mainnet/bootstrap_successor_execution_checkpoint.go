// Read-only checkpoints fence the complete live execution prefix. Durable
// attempts and outcomes cannot be replaced by an owner's cached last record.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// Walk backward from the owner's authenticated terminal seal, checking both
// immutable copies of every event. Only explicit reopen can recover an intent;
// send/result admission cannot repair missing bytes or silently adopt new files.
func (self *bootstrapSuccessorExecutionStore) checkpointExecutionHistory() error {
	if self.last.Sequence >= 32 || !planSha256(self.last.ContentHash) {
		return errors.New("successor execution checkpoint lacks a bounded retained history")
	}
	names, err := self.local.names()
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	readExact := func(name string, expected []byte) error {
		raw, err := self.local.read(name)
		if err != nil || !bytes.Equal(raw, expected) {
			return errors.Join(errors.New("successor execution custody changed during ownership: "+name), err)
		}
		allowed[name] = true
		return nil
	}
	approval, err := json.Marshal(self.approval)
	if err != nil {
		return err
	}
	if err := errors.Join(readExact(bootstrapSuccessorExecutionPrefix+".claim", approval),
		readExact(bootstrapSuccessorExecutionPrefix+".ready", []byte(rootObjectHash(self.approval)+"\n"))); err != nil {
		return err
	}
	if err := self.includePhysicalRebindReceipts(allowed, false); err != nil {
		return err
	}
	expectedHash := self.last.ContentHash
	for sequence := int(self.last.Sequence); sequence >= 0; sequence-- {
		name := bootstrapSuccessorExecutionEventName(uint16(sequence))
		raw, err := self.local.read(name + ".intent")
		var event bootstrapSuccessorExecutionEvent
		if err == nil {
			err = decodePlanJson(raw, &event)
		}
		if err != nil || event.Sequence != uint16(sequence) || event.ContentHash != expectedHash {
			return errors.Join(errors.New("successor execution retained event prefix changed during ownership"), err)
		}
		canonical, err := json.Marshal(event)
		event.ContentHash = ""
		if err != nil || !bytes.Equal(raw, canonical) || rootObjectHash(event) != expectedHash {
			return errors.Join(errors.New("successor execution retained event seal changed during ownership"), err)
		}
		if err := readExact(name+".json", raw); err != nil {
			return err
		}
		allowed[name+".intent"] = true
		expectedHash = event.PreviousHash
	}
	if expectedHash != rootObjectHash(self.approval) {
		return errors.New("successor execution retained prefix lost its adoption authority")
	}
	if self.pending != "" {
		if self.pending != "installed" && self.pending != "outer-reverted" || !planSha256(self.pendingOutcomeHash) {
			return errors.New("successor execution checkpoint has an unresolved attempt or unknown outcome")
		}
		name := self.local.stageName(bootstrapSuccessorExecutionEventName(self.last.Sequence+1)+".intent", self.pending)
		raw, err := self.local.read(name)
		if err != nil || safeReleaseHash(raw) != self.pendingOutcomeHash {
			return errors.Join(errors.New("successor execution interrupted outcome changed during ownership"), err)
		}
		allowed[name] = true
	}
	// Authority readers independently authenticate these exact namespaces.
	// Naming only their retained prefix also rejects injected event/stage files.
	if self.canonicalAuthority != nil {
		allowed[bootstrapSuccessorCanonicalFile] = true
	} else {
		allowed[self.local.stageName(bootstrapSuccessorCanonicalFile, "canonical-authority")] = true
	}
	for _, revision := range self.runtimeHistory.approvals {
		allowed[bootstrapSuccessorRuntimeName(revision.Authorization.Sequence)] = true
	}
	if self.runtimeHistory.pendingHash != "" {
		name := bootstrapSuccessorRuntimeName(uint16(len(self.runtimeHistory.approvals) + 1))
		allowed[self.local.stageName(name, bootstrapSuccessorRuntimeStageKind(self.runtimeHistory.pendingHash))] = true
	}
	for _, revision := range self.safeCurrentHistory.approvals {
		allowed[bootstrapSuccessorSafeCurrentName(revision.Authorization.Sequence)] = true
	}
	if self.safeCurrentHistory.pendingHash != "" {
		name := bootstrapSuccessorSafeCurrentName(uint16(len(self.safeCurrentHistory.approvals) + 1))
		allowed[self.local.stageName(name, bootstrapSuccessorSafeCurrentStageKind(self.safeCurrentHistory.pendingHash))] = true
	}
	for _, name := range names {
		if (strings.HasPrefix(name, bootstrapSuccessorExecutionPrefix) || strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix)) && !allowed[name] {
			return errors.New("successor execution acquired an unexpected custody file during ownership")
		}
	}
	return self.local.ctx.Err()
}
