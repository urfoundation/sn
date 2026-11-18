// The bounded census must carry every accepted publisher payload, including
// JSON escaping and base64 expansion, without reducing the signed profile.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This deliberately exceeds the valid total-byte budget: it is a conservative
// serialization upper bound for every member's independently bounded fields.
func TestBootstrapSuccessorPendingCapacityCoversBoundedMemberLayout(t *testing.T) {
	payload := bytes.Repeat([]byte{255}, maximumBootstrapSuccessorExecutionBytes)
	census := bootstrapSuccessorMemberCensus{Schema: bootstrapSuccessorMemberSchema, Kind: bootstrapSuccessorMemberSpec(true).Kind,
		Members: make([]bootstrapSuccessorMember, maximumBootstrapSuccessorMemberCount),
		Pending: &bootstrapSuccessorMemberPending{Name: strings.Repeat("\x01", 255), Stage: strings.Repeat("\x02", 255),
			Size: int64(len(payload)), Sha256: safeReleaseHash(payload), Payload: base64.StdEncoding.EncodeToString(payload), StageInode: ^uint64(0)}}
	for i := range census.Members {
		census.Members[i] = bootstrapSuccessorMember{Name: fmt.Sprintf("%04d", i) + strings.Repeat("\x01", 251), Inode: ^uint64(0),
			Size: maximumBootstrapSuccessorExecutionBytes, Sha256: safeReleaseHash(payload)}
	}
	raw, err := json.Marshal(census)
	if err != nil || len(raw) > maximumBootstrapSuccessorMemberCensusBytes {
		t.Fatal("bounded member profile cannot retain its exact maximum pending payload", err, len(raw), maximumBootstrapSuccessorMemberCensusBytes)
	}
	if maximumBootstrapSuccessorPreparationBytes > maximumBootstrapSuccessorExecutionBytes || len(raw) <= 2*1024*1024 {
		t.Fatal("capacity control does not cover both original profiles and escaped-name expansion", len(raw))
	}
}

// The real publisher reserves and recovers a full512KiB public JSON payload.
// Oversize input refuses before creating any member or changing its head.
func TestBootstrapSuccessorMaximumPendingPayloadRecoversWithoutNewAuthority(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	name := bootstrapSuccessorExecutionPrefix + "-synthetic-capacity.json"
	owner := f.open(true, nil)
	root := owner.local.path
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	if err := owner.local.publish(name, "synthetic-capacity", bytes.Repeat([]byte{'x'}, maximumBootstrapSuccessorExecutionBytes+1)); err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) {
		_ = owner.close()
		t.Fatal("oversize input mutated pending authority", err)
	}
	prefix, suffix := []byte(`{"padding":"`), []byte(`"}`)
	payload := append(append(bytes.Clone(prefix), bytes.Repeat([]byte{'x'}, maximumBootstrapSuccessorExecutionBytes-len(prefix)-len(suffix))...), suffix...)
	owner.local.hook = func(stage string) error {
		if stage == name+":reserved" {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	if err := owner.local.publish(name, "synthetic-capacity", payload); !errors.Is(err, errMainnetDurablePublicationUncertain) {
		_ = owner.close()
		t.Fatal("maximum payload could not retain its pre-stage intent", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	reader, _, err := openBootstrapSuccessorPreparationReaderMode(f.storageContext(t.Context()), f.approval.Plan.Review.Preparation.Approval.Plan, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	writer := &bootstrapSuccessorExecutionDirectory{storage: reader.storage, members: reader.members, ctx: reader.ctx, path: root,
		root: reader.approval.Plan.Root, file: reader.directory, claim: rootObjectHash(f.approval)}
	if err := writer.resumePending(); err != nil {
		t.Fatal("maximum pending publisher payload could not resume", err)
	}
	if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) || writer.members.census.Pending == nil {
		t.Fatal("pre-stage recovery finalized a publication before application reconciliation", err)
	}
	if err := writer.publish(name, "synthetic-capacity", payload); err != nil {
		t.Fatal("original maximum-payload publication could not complete", err)
	}
	retained, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || !bytes.Equal(retained, payload) || writer.members.census.Pending != nil || len(f.writes) != 0 {
		t.Fatal("maximum payload recovery changed original public bytes or caused a send", err)
	}
}
