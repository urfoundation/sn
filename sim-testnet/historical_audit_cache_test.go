package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/unix"
)

func historicalAuditCacheTestExecutor(t *testing.T) *Executor {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://independent-native.example"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://independent-evm.example"
	return &Executor{
		cfg: cfg, stateDir: t.TempDir(),
		plan: &SetupPlan{
			Schema: "cache-test-plan", PlanHash: "0x" + strings.Repeat("1", 64),
			DeploymentID: cfg.Config.Deployment.DeploymentID, ReleaseLockHash: "0x" + strings.Repeat("2", 64),
			ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
			ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash,
			ResolvedInputsHash: "0x" + strings.Repeat("3", 64),
			PriorPlanHashes:    []string{"0x" + strings.Repeat("4", 64)},
		},
	}
}

func historicalAuditCacheTestInput() map[string]any {
	return map[string]any{
		"observer": "operational", "domain": "substrate",
		"transaction":        "0x" + strings.Repeat("a", 64),
		"block":              ChainHead{Number: 321, Hash: "0x" + strings.Repeat("b", 64)},
		"postcondition_hash": "0x" + strings.Repeat("c", 64),
	}
}

func historicalAuditCacheTestSeed(t *testing.T, e *Executor, input any) *historicalAuditCacheEntry {
	t.Helper()
	called := 0
	hit, err := e.withHistoricalAuditCache(context.Background(), "native-receipt", input, func(context.Context) error {
		called++
		return nil
	})
	if err != nil || hit || called != 1 {
		t.Fatalf("cold audit hit=%t called=%d err=%v", hit, called, err)
	}
	entry, hit := e.lookupHistoricalAuditCache(context.Background(), "native-receipt", input)
	if entry == nil || !hit {
		t.Fatal("successful historical proof was not persisted")
	}
	return entry
}

func historicalAuditCacheTestPath(entry *historicalAuditCacheEntry) string {
	return filepath.Join(entry.stateDir, historicalAuditCacheDirectoryName, entry.name)
}

func TestHistoricalAuditCacheReopensSuccessWithoutRepeatingVerification(t *testing.T) {
	e := historicalAuditCacheTestExecutor(t)
	input := historicalAuditCacheTestInput()
	entry := historicalAuditCacheTestSeed(t, e, input)
	reopened := &Executor{cfg: e.cfg, plan: e.plan, stateDir: e.stateDir}
	hit, err := reopened.withHistoricalAuditCache(context.Background(), "native-receipt", input, func(context.Context) error {
		t.Error("warm historical audit repeated its immutable proof")
		return errors.New("historical archive is unavailable")
	})
	if err != nil || !hit {
		t.Fatalf("reopened audit hit=%t err=%v", hit, err)
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Dir(historicalAuditCacheTestPath(entry)): 0o700,
		historicalAuditCacheTestPath(entry):               0o600,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("cache mode at %s: info=%v err=%v", path, info, err)
		}
	}
	wire, err := os.ReadFile(historicalAuditCacheTestPath(entry))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{e.cfg.WalletMaterial, e.cfg.WalletPassword, e.cfg.WalletPublic, e.cfg.OperationalEVM} {
		if strings.Contains(string(wire), secret) {
			t.Fatalf("cache serialized sensitive context %q", secret)
		}
	}
}

func TestHistoricalAuditCacheRetainsEarlierSuccessAfterLaterFailure(t *testing.T) {
	e := historicalAuditCacheTestExecutor(t)
	first := historicalAuditCacheTestInput()
	historicalAuditCacheTestSeed(t, e, first)
	second := historicalAuditCacheTestInput()
	second["transaction"] = "0x" + strings.Repeat("d", 64)
	want := errors.New("later historical proof failed")
	hit, err := e.withHistoricalAuditCache(context.Background(), "native-receipt", second, func(context.Context) error { return want })
	if hit || !errors.Is(err, want) {
		t.Fatalf("failed audit hit=%t err=%v", hit, err)
	}
	reopened := &Executor{cfg: e.cfg, plan: e.plan, stateDir: e.stateDir}
	if _, hit := reopened.lookupHistoricalAuditCache(context.Background(), "native-receipt", first); !hit {
		t.Fatal("later failure discarded the earlier successful proof")
	}
	for attempt := 0; attempt < 2; attempt++ {
		calls := 0
		hit, err := reopened.withHistoricalAuditCache(context.Background(), "native-receipt", second, func(context.Context) error {
			calls++
			return want
		})
		if hit || calls != 1 || !errors.Is(err, want) {
			t.Fatalf("failed proof was cached: hit=%t calls=%d err=%v", hit, calls, err)
		}
	}
}

func TestHistoricalAuditCacheInvalidatesChangedIdentity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Executor, map[string]any)
	}{
		{"plan", func(e *Executor, _ map[string]any) { e.plan.PlanHash += "-changed" }},
		{"lineage", func(e *Executor, _ map[string]any) { e.plan.PriorPlanHashes = nil }},
		{"resolved-input", func(e *Executor, _ map[string]any) { e.plan.ResolvedInputsHash += "-changed" }},
		{"release-lock", func(e *Executor, _ map[string]any) { e.plan.ReleaseLockHash += "-changed" }},
		{"runtime-code", func(e *Executor, _ map[string]any) { e.cfg.Release.Runtime.CodeHash += "-changed" }},
		{"runtime-metadata", func(e *Executor, _ map[string]any) { e.cfg.Release.Runtime.MetadataHash += "-changed" }},
		{"deployment", func(e *Executor, _ map[string]any) { e.cfg.Config.Deployment.DeploymentID += "-changed" }},
		{"genesis", func(e *Executor, _ map[string]any) { e.cfg.Public.Chain.GenesisHash += "-changed" }},
		{"chain", func(e *Executor, _ map[string]any) { e.cfg.ChainID++ }},
		{"policy", func(e *Executor, _ map[string]any) { e.cfg.PolicyHash += "-changed" }},
		{"native-rpc", func(e *Executor, _ map[string]any) { e.cfg.OperationalSubstrate = "wss://other-native.example" }},
		{"evm-rpc", func(e *Executor, _ map[string]any) { e.cfg.OperationalEVM = "https://other-evm.example" }},
		{"independent-native", func(e *Executor, _ map[string]any) { e.cfg.Public.Chain.SubstratePublicReadEndpoint += "/changed" }},
		{"independent-evm", func(e *Executor, _ map[string]any) { e.cfg.Public.Chain.EVMPublicReadEndpoint += "/changed" }},
		{"rpc-mode", func(e *Executor, _ map[string]any) { e.cfg.OperationalRPCMode = rpcModePublicOverride }},
		{"secret", func(e *Executor, _ map[string]any) { e.cfg.WalletMaterial += " changed" }},
		{"observer", func(_ *Executor, input map[string]any) { input["observer"] = "independent" }},
		{"hash-domain", func(_ *Executor, input map[string]any) { input["domain"] = "evm-rpc" }},
		{"receipt", func(_ *Executor, input map[string]any) { input["postcondition_hash"] = "changed" }},
		{"transaction", func(_ *Executor, input map[string]any) { input["transaction"] = "changed" }},
		{"block", func(_ *Executor, input map[string]any) { input["block"] = ChainHead{Number: 322, Hash: "changed"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := historicalAuditCacheTestExecutor(t)
			input := historicalAuditCacheTestInput()
			historicalAuditCacheTestSeed(t, e, input)
			test.change(e, input)
			calls := 0
			want := errors.New("changed identity requires fresh historical proof")
			hit, err := e.withHistoricalAuditCache(context.Background(), "native-receipt", input, func(context.Context) error {
				calls++
				return want
			})
			if hit || calls != 1 || !errors.Is(err, want) {
				t.Fatalf("changed identity reused old proof: hit=%t calls=%d err=%v", hit, calls, err)
			}
		})
	}
}

func TestHistoricalAuditCacheUsesAuthorizedConfigurationAcrossTransportCopies(t *testing.T) {
	e := historicalAuditCacheTestExecutor(t)
	e.auditAuthorizedConfig = e.cfg
	input := historicalAuditCacheTestInput()
	historicalAuditCacheTestSeed(t, e, input)
	runtimeCfg, err := campaignRPCConfig(e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	reopened := &Executor{cfg: runtimeCfg, auditAuthorizedConfig: e.cfg, plan: e.plan, stateDir: e.stateDir}
	if _, hit := reopened.lookupHistoricalAuditCache(context.Background(), "native-receipt", input); !hit {
		t.Fatal("validated campaign transport copy changed the authorized cache domain")
	}
	reopened.auditAuthorizedConfig.OperationalEVM = "https://new-authorized-evm.example"
	if _, hit := reopened.lookupHistoricalAuditCache(context.Background(), "native-receipt", input); hit {
		t.Fatal("changed authorized endpoint reused the old proof")
	}
}

func TestHistoricalAuditCacheRejectsForgedOrMalformedEntries(t *testing.T) {
	tests := []struct {
		name   string
		change func([]byte) []byte
	}{
		{"unkeyed-digest", func(wire []byte) []byte {
			var envelope historicalAuditCacheEnvelope
			if err := json.Unmarshal(wire, &envelope); err != nil {
				panic(err)
			}
			proof, _ := json.Marshal(envelope.Proof)
			digest := sha256.Sum256(proof)
			envelope.MAC = hex.EncodeToString(digest[:])
			changed, _ := json.Marshal(envelope)
			return changed
		}},
		{"success-bit", func(wire []byte) []byte {
			return []byte(strings.Replace(string(wire), `"success":true`, `"success":false`, 1))
		}},
		{"verifier-version", func(wire []byte) []byte {
			return []byte(strings.Replace(string(wire), historicalAuditCacheVerifierVersion, "future-verifier", 1))
		}},
		{"schema", func(wire []byte) []byte {
			return []byte(strings.Replace(string(wire), historicalAuditCacheSchema, "future-schema", 1))
		}},
		{"unknown-field", func(wire []byte) []byte { return append([]byte(`{"unexpected":true,`), wire[1:]...) }},
		{"duplicate-field", func(wire []byte) []byte {
			return []byte(strings.Replace(string(wire), `"success":true`, `"success":false,"success":true`, 1))
		}},
		{"trailing-value", func(wire []byte) []byte { return append(wire, []byte(`{}`)...) }},
		{"truncated", func(wire []byte) []byte { return wire[:len(wire)/2] }},
		{"oversized", func([]byte) []byte { return []byte(strings.Repeat(" ", historicalAuditCacheMaximumBytes+1)) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := historicalAuditCacheTestExecutor(t)
			input := historicalAuditCacheTestInput()
			entry := historicalAuditCacheTestSeed(t, e, input)
			path := historicalAuditCacheTestPath(entry)
			wire, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, test.change(wire), 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			hit, err := e.withHistoricalAuditCache(context.Background(), "native-receipt", input, func(context.Context) error {
				calls++
				return nil
			})
			if err != nil || hit || calls != 1 {
				t.Fatalf("altered entry skipped verification: hit=%t calls=%d err=%v", hit, calls, err)
			}
			if _, hit := e.lookupHistoricalAuditCache(context.Background(), "native-receipt", input); !hit {
				t.Fatal("successful re-audit did not replace malformed entry")
			}
		})
	}
}

func TestHistoricalAuditCacheCannotReplayAcrossExecutableOrVerifier(t *testing.T) {
	e := historicalAuditCacheTestExecutor(t)
	entry := historicalAuditCacheTestSeed(t, e, historicalAuditCacheTestInput())
	for _, field := range []string{"executable", "verifier", "schema", "kind"} {
		changed := *entry
		switch field {
		case "executable":
			changed.proof.ExecutableSHA256 = strings.Repeat("0", 64)
		case "verifier":
			changed.proof.VerifierVersion += "-changed"
		case "schema":
			changed.proof.Schema += "-changed"
		case "kind":
			changed.proof.Kind = "other-proof-kind"
		}
		if changed.readSuccess() {
			t.Fatalf("valid older HMAC replayed across changed %s identity", field)
		}
	}
}

func TestHistoricalAuditCacheDisabledInputsStillExecuteOriginalVerifier(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Executor) *Executor
	}{
		{"nil-executor", func(*Executor) *Executor { return nil }},
		{"nil-config", func(e *Executor) *Executor { e.cfg = nil; return e }},
		{"missing-secret", func(e *Executor) *Executor { e.cfg.WalletMaterial = ""; return e }},
		{"missing-plan", func(e *Executor) *Executor { e.plan = nil; return e }},
		{"missing-plan-hash", func(e *Executor) *Executor { e.plan.PlanHash = ""; return e }},
		{"missing-state", func(e *Executor) *Executor { e.stateDir = ""; return e }},
		{"nonexistent-state", func(e *Executor) *Executor { e.stateDir = filepath.Join(e.stateDir, "absent"); return e }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := test.change(historicalAuditCacheTestExecutor(t))
			for attempt := 0; attempt < 2; attempt++ {
				calls := 0
				hit, err := e.withHistoricalAuditCache(context.Background(), "native-receipt", historicalAuditCacheTestInput(), func(context.Context) error {
					calls++
					return nil
				})
				if err != nil || hit || calls != 1 {
					t.Fatalf("disabled cache changed verification: hit=%t calls=%d err=%v", hit, calls, err)
				}
			}
		})
	}
}

func TestHistoricalAuditCacheCancellationNeverPublishesSuccess(t *testing.T) {
	e := historicalAuditCacheTestExecutor(t)
	input := historicalAuditCacheTestInput()
	ctx, cancel := context.WithCancel(context.Background())
	hit, err := e.withHistoricalAuditCache(ctx, "native-receipt", input, func(context.Context) error {
		cancel()
		return nil
	})
	if hit || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled verifier hit=%t err=%v", hit, err)
	}
	entry, hit := e.lookupHistoricalAuditCache(context.Background(), "native-receipt", input)
	if entry == nil || hit {
		t.Fatal("canceled verifier produced a reusable success")
	}
	entry.saveSuccess(ctx)
	if entry.readSuccess() {
		t.Fatal("canceled batched completion persisted a success")
	}
	historicalAuditCacheTestSeed(t, e, input)
	if hit, err := e.withHistoricalAuditCache(ctx, "native-receipt", input, func(context.Context) error {
		t.Error("already canceled audit executed verification")
		return nil
	}); hit || !errors.Is(err, context.Canceled) {
		t.Fatalf("warm cache ignored cancellation: hit=%t err=%v", hit, err)
	}
}

func TestHistoricalAuditCacheRejectsUnsafeFilesystemEntries(t *testing.T) {
	for _, kind := range []string{"symlink-leaf", "symlink-directory", "symlink-state", "fifo", "public-file", "public-directory"} {
		t.Run(kind, func(t *testing.T) {
			e := historicalAuditCacheTestExecutor(t)
			input := historicalAuditCacheTestInput()
			entry := historicalAuditCacheTestSeed(t, e, input)
			path := historicalAuditCacheTestPath(entry)
			switch kind {
			case "symlink-leaf", "fifo":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink-leaf" {
					if err := os.Symlink(path+".original", path); err != nil {
						t.Fatal(err)
					}
				} else if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink-directory":
				directory := filepath.Dir(path)
				if err := os.Rename(directory, directory+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(directory+".original", directory); err != nil {
					t.Fatal(err)
				}
			case "symlink-state":
				alias := filepath.Join(t.TempDir(), "state-link")
				if err := os.Symlink(e.stateDir, alias); err != nil {
					t.Fatal(err)
				}
				e.stateDir = alias
			case "public-file":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "public-directory":
				if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			want := errors.New("fresh historical verifier still required")
			hit, err := e.withHistoricalAuditCache(context.Background(), "native-receipt", input, func(context.Context) error {
				calls++
				return want
			})
			if hit || calls != 1 || !errors.Is(err, want) {
				t.Fatalf("unsafe filesystem entry reused: hit=%t calls=%d err=%v", hit, calls, err)
			}
		})
	}
}

func TestHistoricalAuditCacheConcurrentSuccessfulWritesRemainReusable(t *testing.T) {
	e := historicalAuditCacheTestExecutor(t)
	const workers = 12
	ctx := context.Background()
	entry, hit := e.lookupHistoricalAuditCache(ctx, "native-receipt", historicalAuditCacheTestInput())
	if entry == nil || hit {
		t.Fatal("concurrent test requires an enabled cold cache")
	}
	var wait sync.WaitGroup
	var successes atomic.Int32
	errorsSeen := make(chan error, workers)
	start := make(chan struct{})
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			entry.saveSuccess(ctx)
			if !entry.readSuccess() {
				errorsSeen <- fmt.Errorf("concurrent success was not atomically readable")
				return
			}
			successes.Add(1)
		}()
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
	if got := successes.Load(); got != workers {
		t.Fatalf("completed durable writes=%d, want %d", got, workers)
	}
	reopened := &Executor{cfg: e.cfg, plan: e.plan, stateDir: e.stateDir}
	if _, hit := reopened.lookupHistoricalAuditCache(ctx, "native-receipt", historicalAuditCacheTestInput()); !hit {
		t.Fatal("concurrent writers left no reusable proof")
	}
	files, err := os.ReadDir(filepath.Join(e.stateDir, historicalAuditCacheDirectoryName))
	if err != nil || len(files) != 1 || files[0].Name() != entry.name {
		t.Fatalf("concurrent writes left partial files: count=%d err=%v", len(files), err)
	}
}
