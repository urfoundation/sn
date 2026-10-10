// Public command fixtures authenticate the exact committed census separately
// from byte-immutable approvals, nonce claims and adopted execution events.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Real member identities and bytes must match the complete, canonical head.
// A pending reservation is never silently accepted as public claim completion.
func bootstrapSuccessorExecutionTestMemberCensus(t *testing.T, directory string, registry bool) map[string]bootstrapSuccessorMember {
	t.Helper()
	spec := bootstrapSuccessorMemberSpec(registry)
	raw, err := os.ReadFile(filepath.Join(directory, spec.Name))
	if err != nil {
		t.Fatal(err)
	}
	var census bootstrapSuccessorMemberCensus
	if err := decodePlanJson(raw, &census); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(census)
	if err != nil || !bytes.Equal(raw, canonical) || census.Schema != bootstrapSuccessorMemberSchema || census.Kind != spec.Kind || census.Pending != nil {
		t.Fatal("public completion lacks canonical committed member custody", err)
	}
	members := map[string]bootstrapSuccessorMember{}
	previous := ""
	for _, member := range census.Members {
		if member.Name <= previous || member.Name != filepath.Base(member.Name) {
			t.Fatal("public census is not an ordered exact member set")
		}
		previous = member.Name
		path := filepath.Join(directory, member.Name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Ino != member.Inode || int64(len(data)) != member.Size || safeReleaseHash(data) != member.Sha256 {
			t.Fatal("public census differs from original physical member", member.Name)
		}
		members[member.Name] = member
	}
	return members
}
