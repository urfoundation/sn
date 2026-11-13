// Public command fixtures authenticate the exact committed census separately
// from byte-immutable approvals, nonce claims and adopted execution events.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// Execution appends members, so the census head alone may advance. This names
// an original file whose bytes changed, or an original member whose physical
// identity the committed census no longer retains; "" means neither exists.
func bootstrapSuccessorExecutionTestChangedOriginal(t *testing.T, directory string, files map[string]string, members map[string]bootstrapSuccessorMember) string {
	t.Helper()
	head := bootstrapSuccessorMemberSpec(false).Name
	for name, raw := range files {
		if name == head {
			continue
		}
		if retained, err := os.ReadFile(filepath.Join(directory, name)); err != nil || string(retained) != raw {
			return name
		}
	}
	current := bootstrapSuccessorExecutionTestMemberCensus(t, directory, false)
	for name, member := range members {
		if current[name] != member {
			return name
		}
	}
	return ""
}

// The census refuses an edited member before any event is replayed. A test
// of a replay refusal writes its edited bytes, or removes the file for a nil
// value, and acknowledges the result, as only an owner's own publication
// otherwise could, so that the replay refusal is the one reached.
func bootstrapSuccessorExecutionTestRewrite(t *testing.T, ctx context.Context, directory, claim string, files map[string][]byte) {
	t.Helper()
	root, err := bootstrapSuccessorPhysicalRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	active := openStorageMemberRestoreTestOwner(t, ctx, directory, root, claim, false, nil)
	census := active.members.census
	members := slices.Clone(census.Members)
	for name, raw := range files {
		members = slices.DeleteFunc(members, func(member bootstrapSuccessorMember) bool { return member.Name == name })
		path := filepath.Join(directory, name)
		if raw == nil {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, bootstrapSuccessorMember{Name: name, Inode: info.Sys().(*syscall.Stat_t).Ino, Size: int64(len(raw)), Sha256: safeReleaseHash(raw)})
	}
	slices.SortFunc(members, func(a, b bootstrapSuccessorMember) int { return strings.Compare(a.Name, b.Name) })
	census.Members = members
	if err := errors.Join(active.members.publish(census), active.close()); err != nil {
		t.Fatal(err)
	}
}
