package main

// Preserves exact source-index identity and returned-byte ownership while
// bounding repeated plan indexing independently of the archive's byte size.

import (
	"bytes"
	"path/filepath"
	"testing"
)

// Rechecking one already-indexed plan needs no new copy of the entire plan.
// The constructor authenticates plans; this isolates the later index lookup.
func TestFinalFleetGenerationSourcePlanIndexDoesNotRecopyUnchangedBytes(t *testing.T) {
	path := "launch-foundation/plan.json"
	planHash := finalTestHex(1)
	plan := &SetupPlan{PlanHash: planHash}
	source := &finalFleetGenerationSource{
		archive: &finalSemanticArchive{files: map[string][]byte{path: bytes.Repeat([]byte("sealed plan"), 1024)}},
		plans:   map[string]*SetupPlan{planHash: plan}, planPaths: map[string]string{planHash: path}, raw: make(map[string][]byte),
	}
	if got, err := source.recordPlan(planHash); err != nil || got != plan {
		t.Fatalf("initial plan index = %p, %v", got, err)
	}
	var got *SetupPlan
	var err error
	allocations := testing.AllocsPerRun(8, func() { got, err = source.recordPlan(planHash) })
	if err != nil || got != plan {
		t.Fatalf("repeated plan index = %p, %v", got, err)
	}
	if allocations != 0 {
		t.Fatalf("rechecking indexed plan allocated %g objects, want no repeated source copies", allocations)
	}
}

// The source index and each returned slice have separate owners. Every later
// read still observes in-place changes, replacement, and disappearance in the
// archive even after a prior successful lookup.
func TestFinalFleetGenerationSourceRecordRetainsOwnedBytesAndDetectsChanges(t *testing.T) {
	path := "public/fleet.json"
	want := []byte("sealed fleet")
	archive := &finalSemanticArchive{files: map[string][]byte{path: bytes.Clone(want)}}
	planHash := finalTestHex(1)
	source := &finalFleetGenerationSource{
		archive: archive, raw: make(map[string][]byte),
		plans: map[string]*SetupPlan{planHash: {PlanHash: planHash}}, planPaths: map[string]string{planHash: path},
	}
	for range 2 {
		got, err := source.record(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("source record = %q, %v", got, err)
		}
		got[0] ^= 0xff
		if !bytes.Equal(archive.files[path], want) || !bytes.Equal(source.raw[path], want) {
			t.Fatal("returned byte mutation reached the archive or owned index")
		}
	}
	for _, mutation := range []struct {
		name   string
		change func()
		want   string
	}{
		{name: "in-place mutation", change: func() { archive.files[path][0] ^= 0xff }, want: "ordinary fleet generation source public/fleet.json changed while being indexed"},
		{name: "replacement", change: func() { archive.files[path] = []byte("other fleet") }, want: "ordinary fleet generation source public/fleet.json changed while being indexed"},
		{name: "disappearance", change: func() { delete(archive.files, path) }, want: "closed semantic graph is missing public/fleet.json"},
	} {
		archive.files[path] = bytes.Clone(want)
		mutation.change()
		got, err := source.record(path)
		if len(got) != 0 || err == nil || err.Error() != mutation.want {
			t.Errorf("%s returned %q, %v; want %s", mutation.name, got, err, mutation.want)
		}
		if plan, err := source.recordPlan(planHash); plan != nil || err == nil || err.Error() != mutation.want {
			t.Errorf("%s plan recheck returned %p, %v; want %s", mutation.name, plan, err, mutation.want)
		}
		if !bytes.Equal(source.raw[path], want) {
			t.Fatalf("%s rewrote the previously owned index", mutation.name)
		}
	}
}

// Path normalization belongs to archive lookup; the index keeps the original
// caller key and the missing-file diagnostic keeps its original spelling.
func TestFinalFleetGenerationSourceRecordKeepsPathLookupAndMissingErrors(t *testing.T) {
	path := filepath.FromSlash("public/fleet.json")
	want := []byte("sealed fleet")
	source := &finalFleetGenerationSource{
		archive: &finalSemanticArchive{files: map[string][]byte{filepath.ToSlash(path): want}},
		raw:     make(map[string][]byte),
	}
	if got, err := source.record(path); err != nil || !bytes.Equal(got, want) || !bytes.Equal(source.raw[path], want) || len(source.raw) != 1 {
		t.Fatalf("normalized source record = %q, %v; index=%v", got, err, source.raw)
	}
	delete(source.archive.files, filepath.ToSlash(path))
	if _, err := source.record(path); err == nil || err.Error() != "closed semantic graph is missing "+path {
		t.Fatalf("original missing-file diagnostic = %v", err)
	}
}
