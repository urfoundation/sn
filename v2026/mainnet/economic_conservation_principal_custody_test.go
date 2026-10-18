// The actual principal adapter needs both the synthetic volume declaration and
// the separately provisioned physical checkpoint owner before public startup.
package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEconomicConservationPrincipalFixtureProvisionsAbsentHeadAndRefusesLostLock(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	if _, err := os.Lstat(f.source.checkpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fresh principal checkpoint is not explicitly absent", err)
	}
	info, err := os.Lstat(f.source.checkpoint + ".lock")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("principal fixture did not provision its actual checkpoint owner", info, err)
	}
	summary := f.sample(t, monitorServiceHooks{})
	if summary.OpeningPrincipalAlpha == nil || *summary.OpeningPrincipalAlpha != "14" || !summary.NativeCurrent || !summary.VaultCurrent {
		t.Fatal("provisioned principal fixture did not reach actual public observations", summary)
	}
	before, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reads := f.source.claimReads.Load()
	if err := os.Remove(f.source.checkpoint + ".lock"); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	after, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 || output.Len() != 0 || reads != f.source.claimReads.Load() || !bytes.Equal(before, after) || !strings.Contains(diagnostic.String(), "open checkpoint lock: no such file or directory") {
		t.Fatal("public principal reopen repaired a lost owner or read another source", code, diagnostic.String())
	}
	if _, err := os.Lstat(f.source.checkpoint + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("runtime recreated a missing principal checkpoint owner", err)
	}
}

func TestEconomicConservationPrincipalFixtureEmptyCommittedHeadIsNotAbsence(t *testing.T) {
	f, _ := newEconomicConservationPrincipalFixture(t, "present", true, nil)
	owner, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish([]byte{}, nil), owner.close()); err != nil {
		t.Fatal("publish actual empty owned checkpoint", err)
	}
	reads := f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	after, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 || output.Len() != 0 || reads != f.source.claimReads.Load() || len(after) != 0 || !strings.Contains(diagnostic.String(), "invalid JSON value: EOF") {
		t.Fatal("empty committed principal checkpoint became a fresh observation", code, diagnostic.String())
	}
}
