package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Shared options preserve the existing command bytes and carry no signing or
// filesystem authority until an actual owner opens the exact declaration.
func TestMainnetDurableVolumeArgumentsPreserveCommand(t *testing.T) {
	want := []string{"bootstrap-chain", "resume", "--config", "/synthetic/config.json", "--run-dir=/synthetic/state"}
	reference := durablevolume.Reference{Path: "/synthetic/volumes.json", Sha256: "sha256:reviewed"}
	args := []string{"bootstrap-chain", "--durable-volumes", reference.Path, "resume", "--config", want[3], "--durable-volumes-sha256=" + reference.Sha256, want[4]}
	ctx, command, err := mainnetDurableVolumeArguments(context.Background(), args)
	if err != nil || !reflect.DeepEqual(command, want) {
		t.Fatalf("command changed: %q, %v", command, err)
	}
	got, present := durablevolume.ReferenceFromContext(ctx)
	if !present || got != reference {
		t.Fatalf("explicit reference changed: %+v, present=%t", got, present)
	}
	_, again, err := mainnetDurableVolumeArguments(ctx, command)
	if err != nil || !reflect.DeepEqual(again, command) {
		t.Fatalf("retained reference changed dispatch: %q, %v", again, err)
	}
}

// A partial, repeated, or conflicting declaration cannot be silently dropped
// by one subcommand's parser and interpreted as an unguarded legacy invocation.
func TestMainnetDurableVolumeArgumentsRejectAmbiguity(t *testing.T) {
	for _, args := range [][]string{
		{"monitor", "--durable-volumes", "/synthetic/volumes.json"},
		{"monitor", "--durable-volumes-sha256=sha256:reviewed"},
		{"monitor", "--durable-volumes="},
		{"monitor", "--durable-volumes"},
		{"monitor", "--durable-volumes", "--rpc", "https://rpc.example"},
		{"monitor", "--durable-volumes=a", "--durable-volumes=a", "--durable-volumes-sha256=b"},
		{"monitor", "--durable-volumes=a", "--durable-volumes-sha256=b", "--durable-volumes-sha256=b"},
	} {
		if _, _, err := mainnetDurableVolumeArguments(context.Background(), args); err == nil {
			t.Fatalf("ambiguous declaration accepted: %q", args)
		}
	}
	ctx := durablevolume.WithReference(context.Background(), durablevolume.Reference{Path: "/synthetic/original.json", Sha256: "sha256:original"})
	if _, _, err := mainnetDurableVolumeArguments(ctx, []string{"monitor", "--durable-volumes=/synthetic/replacement.json", "--durable-volumes-sha256=sha256:replacement"}); err == nil {
		t.Fatal("command replaced its caller's retained declaration")
	}
}
