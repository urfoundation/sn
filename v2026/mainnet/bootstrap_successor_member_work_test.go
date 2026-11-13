// Actual payload-read counters distinguish bounded startup authentication from
// repeated historical scans during unchanged public execution checkpoints.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Retained named descriptors and their metadata are checked on every boundary,
// but an unchanged lifetime prefix is not read and hashed again.
func TestBootstrapSuccessorUnchangedMembersAvoidRepeatedPayloadReads(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	defer owner.close()
	readBytes := 0
	observe := func(_ string, size int) { readBytes += size }
	owner.local.members.afterRead, owner.registry.members.afterRead = observe, observe
	for range 8 {
		if err := owner.checkpoint("synthetic-unchanged-member-work"); err != nil {
			t.Fatal(err)
		}
	}
	if readBytes != 0 || len(f.writes) != 0 {
		t.Fatal("unchanged continuation reread acknowledged payloads", readBytes, len(f.writes))
	}
}

// A real counted attempt adds its own intent/event while every preceding
// signed preparation, approval and global nonce member remains unchanged.
func TestBootstrapSuccessorPendingMembersReadOnlyTheirNewPayload(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	defer owner.close()
	oldNames := map[string]bool{}
	for _, members := range []*bootstrapSuccessorMembers{owner.local.members, owner.registry.members} {
		for _, member := range members.census.Members {
			oldNames[member.Name] = true
		}
	}
	oldBytes, newBytes := 0, 0
	observe := func(name string, size int) {
		if oldNames[name] {
			oldBytes += size
		} else {
			newBytes += size
		}
	}
	owner.local.members.afterRead, owner.registry.members.afterRead = observe, observe
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil || owner.last.CumulativeAttempts != 9 || len(f.writes) != 1 {
		t.Fatal("original captured attempt did not advance", err)
	}
	if oldBytes != 0 || newBytes == 0 {
		t.Fatal("pending publication reread old history or skipped actual new bytes", oldBytes, newBytes)
	}
	after := newBytes
	if err := owner.checkpoint("synthetic-after-counted-publication"); err != nil || oldBytes != 0 || newBytes != after {
		t.Fatal("completed publication did not become an unchanged retained member", err, oldBytes, newBytes)
	}
}

// Positive member loss still refuses the actual pre-send checkpoint. No scan
// of unrelated historical payloads is needed to identify a changed size/inode.
func TestBootstrapSuccessorChangedMemberDoesNotRehashUnrelatedHistory(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	defer owner.close()
	name := bootstrapSuccessorExecutionEventName(0) + ".json"
	path := filepath.Join(owner.local.path, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	unrelatedBytes := 0
	owner.local.members.afterRead = func(observed string, size int) {
		if observed != name {
			unrelatedBytes += size
		}
	}
	if err := owner.checkpoint("synthetic-changed-member-before-send"); !errors.Is(err, durablevolume.ErrIdentity) || unrelatedBytes != 0 || len(f.writes) != 0 {
		t.Fatal("changed member escaped custody or scanned unrelated history", err, unrelatedBytes)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := owner.checkpoint("synthetic-restored-member-same-owner"); !errors.Is(err, durablevolume.ErrIdentity) || len(f.writes) != 0 {
		t.Fatal("same owner forgot its confirmed member loss", err)
	}
}
