//go:build linux || darwin

// Restoration preserves the runtime's original checkpoint grammar as well as
// its history. A new physical root cannot legitimize a refused old head.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Real preparation, publication, export and restart distinguish pristine
// absence from retained completion before each original-byte grammar control.
func TestStoragePreparationRestorePreservesRuntimeCheckpointGrammar(t *testing.T) {
	for _, phase := range []string{"empty", "committed"} {
		for _, coverage := range []string{"", durablevolume.PreparationCompleteUnion} {
			for _, grammar := range []string{"canonical", "whitespace", "duplicate"} {
				func() {
					source := newStoragePreparationCommandFixture(t)
					owner := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "monitor.json", maxRpcReplyBytes)
					storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
					ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
					path := filepath.Join(source.root, "monitor.json")
					expected := identityExpectation{NativeChain: "synthetic-chain", GenesisHash: "0x" + strings.Repeat("25", 32), EvmChainId: mainnetEvmChainId}
					store, err := openMonitorCheckpoint(path, expected, ctx)
					if err != nil {
						t.Fatal(err)
					}
					state := &monitorState{}
					if phase == "committed" {
						state = &monitorState{lastNumber: 71, lastHash: "0x" + strings.Repeat("26", 32), lastProgressAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
						err = store.save(state)
					}
					if err := errors.Join(err, store.close()); err != nil {
						t.Fatal(err)
					}
					attribute := durablehead.Attribute(owner.Kind, "monitor.json")
					original := make([]byte, 4096)
					n, err := unix.Getxattr(path+".lock", attribute, original)
					if err != nil {
						t.Fatal(err)
					}
					original = original[:n]
					switch grammar {
					case "whitespace":
						original = append(original, '\n')
					case "duplicate":
						original = append([]byte(`{"schema":"not-the-original-schema",`), original[1:]...)
					}
					if err := unix.Setxattr(path+".lock", attribute, original, unix.XATTR_REPLACE); err != nil {
						t.Fatal(err)
					}
					store, err = openMonitorCheckpoint(path, expected, ctx)
					if grammar != "canonical" {
						if store != nil {
							store.close()
							t.Fatal("runtime admitted a rewritten original checkpoint")
						}
						if !errors.Is(err, durablevolume.ErrIdentity) {
							t.Fatal("runtime did not reject the original checkpoint grammar", err)
						}
					} else {
						if err != nil {
							t.Fatal("canonical original cannot restart", err)
						}
						if err := store.close(); err != nil {
							t.Fatal(err)
						}
					}
					owner.RestoreCoverage = coverage
					f := storageSnapshotRestoreTarget(t, source, ctx, owner, false)
					if grammar != "canonical" {
						var output, diagnostic bytes.Buffer
						code := runMain(f.target.ctx, []string{"storage-prepare", "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic)
						if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "snapshot restore original checkpoint is not canonical") {
							t.Fatal("restore normalized a runtime-refused original head", code, diagnostic.String())
						}
						entries, err := os.ReadDir(f.target.root)
						if err != nil || len(entries) != 0 {
							t.Fatal("refused restore changed target members", err)
						}
					} else {
						restored := f.apply(t)
						store, err := openMonitorCheckpoint(path, expected, restored)
						if err != nil {
							t.Fatal("canonical restore cannot restart", err)
						}
						actual, readErr := store.load()
						if err := errors.Join(readErr, store.close()); err != nil || actual == nil || actual.lastNumber != state.lastNumber || actual.lastHash != state.lastHash || !actual.lastProgressAt.Equal(state.lastProgressAt) {
							t.Fatal("canonical restore lost original absence or completion", err)
						}
					}
					for _, root := range []string{f.heldSource, f.archive} {
						retained := make([]byte, 4096)
						n, err := unix.Getxattr(filepath.Join(root, "monitor.json.lock"), attribute, retained)
						if err != nil || !bytes.Equal(retained[:n], original) {
							t.Fatal("restore changed retained original checkpoint bytes", err)
						}
					}
				}()
			}
		}
	}
}
