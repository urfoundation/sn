//go:build linux || darwin

// Terminal projections retain the same successful, owned full-lineage
// admission as measurement projections. Equal names cannot replace custody.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// A real second owner proves invalidation remains local. The replaced owner
// must refuse cached terminals without rereading source history; the healthy
// owner must keep returning independently owned copies of both original cuts.
func TestReleaseArchiveFullLineageTerminalRefusesLostOwner(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	options, work := f.measuredOptions(t)
	archive, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err != nil || archive == nil {
		t.Fatalf("full terminal lineage admission: %v", err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	healthyOptions, healthyWork := f.measuredOptions(t)
	healthy, err := OpenReleaseEvidenceV2Archive(t.Context(), healthyOptions)
	if err != nil || healthy == nil {
		t.Fatalf("independent terminal lineage admission: %v", err)
	}
	defer func() {
		if err := healthy.Close(); err != nil {
			t.Error(err)
		}
	}()
	if archive.owner.root == healthy.owner.root || len(archive.intents) != 2 || len(archive.history.terminals) != 2 || len(healthy.history.terminals) != 2 {
		t.Fatal("terminal fixture omitted original predecessors or independent custody")
	}
	before, beforeBytes := work.snapshot()
	healthyBefore, healthyBeforeBytes := healthyWork.snapshot()
	terminal, err := archive.TerminalClosure(f.last.Epoch)
	if err != nil || terminal == nil || !bytes.Equal(mustArchiveV2JSONTest(t, terminal), mustArchiveV2JSONTest(t, f.last)) {
		t.Fatalf("original terminal differs before custody loss: %v", err)
	}
	root := archive.owner.root
	if err := os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(root); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
		if err := os.Rename(root+"-retained", root); err != nil {
			t.Error(err)
		}
	}()
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for epoch := range archive.history.terminals {
		terminal, err := archive.TerminalClosure(epoch)
		if terminal != nil || err == nil || !strings.Contains(err.Error(), "changed ownership") {
			t.Fatalf("cached terminal ignored lost original scratch custody: epoch=%d terminal=%v error=%v", epoch, terminal != nil, err)
		}
		for range 3 {
			terminal, err := healthy.TerminalClosure(epoch)
			if err != nil || terminal == nil || !bytes.Equal(mustArchiveV2JSONTest(t, terminal), mustArchiveV2JSONTest(t, healthy.history.terminals[epoch])) {
				t.Fatalf("lost sibling custody changed healthy original terminal: %v", err)
			}
			terminal.Transitions = nil
		}
	}
	after, afterBytes := work.snapshot()
	healthyAfter, healthyAfterBytes := healthyWork.snapshot()
	if !reflect.DeepEqual(before, after) || beforeBytes != afterBytes || !reflect.DeepEqual(healthyBefore, healthyAfter) || healthyBeforeBytes != healthyAfterBytes {
		t.Fatal("terminal custody checks replayed original source history")
	}
}

// A canceled original owner cannot return retained terminals. A fresh owner
// must read the whole signed lineage again, including the failed predecessor.
func TestReleaseArchiveFullLineageTerminalOwnerCancellation(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	options, work := f.measuredOptions(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	archive, err := OpenReleaseEvidenceV2Archive(ctx, options)
	if err != nil || archive == nil {
		t.Fatalf("cancelable full terminal admission: %v", err)
	}
	defer func() {
		if err := archive.Close(); !errors.Is(err, context.Canceled) {
			t.Errorf("canceled owner close lost its original cause: %v", err)
		}
	}()
	before, beforeBytes := work.snapshot()
	cancel()
	if terminal, err := archive.TerminalClosure(f.last.Epoch); terminal != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled owner returned retained terminal authority: %v", err)
	}
	after, afterBytes := work.snapshot()
	if !reflect.DeepEqual(before, after) || beforeBytes != afterBytes {
		t.Fatal("canceled terminal lookup read original source bytes")
	}
	if err := archive.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled original owner did not close: %v", err)
	}
	if terminal, err := archive.TerminalClosure(f.last.Epoch); terminal != nil || err == nil {
		t.Fatal("closed original owner returned retained terminal authority")
	}
	freshOptions, freshWork := f.measuredOptions(t)
	fresh, err := OpenReleaseEvidenceV2Archive(t.Context(), freshOptions)
	if err != nil || fresh == nil {
		t.Fatalf("fresh original terminal owner: %v", err)
	}
	defer func() {
		if err := fresh.Close(); err != nil {
			t.Error(err)
		}
	}()
	freshReads, freshBytes := freshWork.snapshot()
	if !reflect.DeepEqual(before, freshReads) || beforeBytes != freshBytes {
		t.Fatal("fresh terminal owner inherited canceled lineage work")
	}
	if terminal, err := fresh.TerminalClosure(f.last.Epoch); err != nil || terminal == nil || !bytes.Equal(mustArchiveV2JSONTest(t, terminal), mustArchiveV2JSONTest(t, f.last)) {
		t.Fatalf("fresh full lineage did not recover its original terminal: %v", err)
	}
}
