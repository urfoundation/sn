// The composed public path joins bootstrap's prepared head to the actual
// monitor writer, without signing or invoking a service manager.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"golang.org/x/sys/unix"
)

// Bootstrap leaves a fresh, independently prepared monitor head. The public
// passive command must consume that exact head and retain its completed bytes
// across restart; missing custody cannot become a fresh observation campaign.
func TestDurableCompositionPassiveBootstrapStartsRetainedMonitor(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	prepared := f.result(t, "apply")
	if prepared.NetworkEffects || prepared.ActivationReady || prepared.Root.Broadcasts != 0 {
		t.Fatal("synthetic bootstrap widened authority", prepared)
	}
	config := rootPassiveRuntimeConfig{Schema: rootPassiveRuntimeSchema, Root: f.config.Root, Role: *f.config.RootValidator}
	reference := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "composed-passive-runtime.json"), config)
	args := []string{"root-passive-service", "run", "--config", reference.Path, "--accept-runtime-sha256", reference.Sha256}
	ctx := f.storageContext(t.Context())
	var output, diagnostic bytes.Buffer
	if code := runMain(ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("prepared bootstrap and monitor did not compose", code, diagnostic.String())
	}
	var event rootMonitorEvent
	if err := json.Unmarshal(output.Bytes(), &event); err != nil || event.Status != "ready" || event.Observation == nil {
		t.Fatal("composed observer omitted its real sample", err, output.String())
	}
	path := f.root.plan.PassiveService.CheckpointPath
	original, err := os.ReadFile(path)
	if err != nil || len(original) == 0 {
		t.Fatal("composed monitor did not retain its checkpoint", err)
	}
	output.Reset()
	diagnostic.Reset()
	if code := runMain(ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("retained monitor did not reopen after joined public run", code, diagnostic.String())
	}
	retained := path + ".original"
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	completed, err := os.ReadFile(retained)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	diagnostic.Reset()
	if code := runMain(ctx, args, &output, &diagnostic); code == 0 {
		t.Fatal("completed monitor loss was silently initialized", output.String())
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("refused composed restart recreated custody", err)
	}
	if raw, err := os.ReadFile(retained); err != nil || !bytes.Equal(raw, completed) {
		t.Fatal("refused composed restart changed retained checkpoint", err)
	}
}

// Direct public entry cannot omit CLI dispatch and acquire legacy persistence.
// Checkpoint and derived metrics paths both require the explicit daemon policy.
func TestDurableCompositionRootPersistentEntrypointsRequireDeclaration(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	policy := rootTestPolicyFile(t, fixture.policy)
	for _, kind := range []string{"checkpoint", "metrics"} {
		path := filepath.Join(monitorMetricsTestDir(t), "root.prom")
		args := []string{"root-monitor", "--rpc", server.URL, "--policy", policy}
		if kind == "checkpoint" {
			args = append(args, "--checkpoint", path)
		} else {
			args = append(args, "--metrics-file", path, "--metrics-role", "synthetic-root")
		}
		if code := runRootCommand(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Error("direct persistent root observer bypassed declaration", kind, code)
		}
		for _, member := range []string{path, path + ".lock"} {
			if _, err := os.Lstat(member); !os.IsNotExist(err) {
				t.Error("refused direct entry created an unguarded member", kind, member, err)
			}
		}
	}
}

// A matching old JSON record and marker grammar cannot replace the durable
// head that identifies bootstrap's retained physical preparation generation.
func TestDurableCompositionPassivePreparationRequiresRetainedHead(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	f.result(t, "apply")
	path := filepath.Join(f.root.plan.RunDirectory, bootstrapRootProgressFile)
	marker, err := os.Open(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Fremovexattr(int(marker.Fd()), durablehead.Attribute("mainnet-bootstrap-root", filepath.Base(path))); err != nil {
		marker.Close()
		t.Fatal("fixture did not retain its original bootstrap head", err)
	}
	if err := marker.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := openRootPassivePreparation(f.storageContext(t.Context()), f.root.plan)
	if reader != nil {
		reader.Close()
	}
	if err == nil {
		t.Fatal("passive runtime accepted preparation after its retained head disappeared")
	}
}
