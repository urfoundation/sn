//go:build linux || darwin

// A signed validator config can name its approval sources in bootstrap
// custody: a 0700 tree another account owns, which the validator account
// cannot search. The validator then loads the same signed bytes from the
// copies it retained in state_dir, exactly as when a source is absent.
package validator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// Every signed source a production load reaches: the current approval, its
// runtime and original-authority history, and each original's own approval
// and runtime documents, whose bytes travel inside its authority bundle.
func productionRetainedSourceTestPaths(cfg *ReleaseConfig) []string {
	paths := []string{productionEconomicSelection(cfg).Approval.Path}
	for _, reference := range append(slices.Clone(cfg.ProductionRuntimeApprovals), cfg.ProductionAuthorityHistory...) {
		paths = append(paths, reference.Path)
	}
	if cfg.productionAuthorityHistory != nil {
		for _, entry := range cfg.productionAuthorityHistory.entries {
			paths = append(paths, productionRetainedSourceTestPaths(entry.config)...)
		}
	}
	return paths
}

// Both signed shapes that reach a validator: a renewal with runtime and
// original-authority history, and the activated treasury successor whose
// original approval stays in bootstrap custody. Once every source directory is
// untraversable, the producer loader authenticates the same authority from the
// retained copies; a wrong or missing copy, or any other path error, refuses.
func TestProductionAuthorityRetainedCopiesReplaceUntraversableSources(t *testing.T) {
	for _, treasury := range []bool{false, true} {
		var path string
		if treasury {
			pending := newProductionPendingTestFixture(t, true)
			boundary := productionRenderingTestBoundary(pending.approval.FirstNativeEpoch+3, 150)
			_, options := productionRenderingTestPublish(t, pending, nil, &boundary)
			path = options.RenderedConfigPath
		} else {
			production := newProductionRuntimeTestFixture(t, true)
			current, _ := productionAuthorityTestSuccessor(t, production.cfg, production.approval, production.private, true)
			path = writeReleaseConfig(t, *current)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// The config itself stays readable, apart from every source directory.
		pinned := filepath.Join(identityTestStateDir(t), "validator.yml")
		if err := os.WriteFile(pinned, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadReleaseConfig(pinned)
		if err != nil {
			t.Fatal(treasury, err)
		}
		want, err := BuildOwnerRecycleProductionAuthority(t.Context(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		// The validator retains its signed inputs in state_dir when it starts.
		if _, err := RetainOwnerRecycleApproval(t.Context(), cfg); err != nil {
			t.Fatal(treasury, err)
		}
		locked := map[string]bool{}
		for _, source := range productionRetainedSourceTestPaths(cfg) {
			directory := filepath.Dir(source)
			if locked[directory] {
				continue
			}
			for _, kept := range []string{cfg.StateDir, pinned} {
				if strings.HasPrefix(kept, directory+string(filepath.Separator)) {
					t.Fatal("fixture source directory also holds validator state or the pinned config", directory)
				}
			}
			custodyTestUntraversable(t, directory)
			locked[directory] = true
		}
		loaded, err := LoadReleaseConfig(pinned)
		if err != nil {
			t.Fatal("producer loader refused retained copies behind untraversable sources", treasury, err)
		}
		got, err := BuildOwnerRecycleProductionAuthority(t.Context(), loaded)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("retained copies changed the authenticated production authority", treasury, err)
		}
		if len(loaded.productionAuthorityHistory.entries) != len(cfg.productionAuthorityHistory.entries) {
			t.Fatal("retained copies changed the original authority history", treasury)
		}
		for index, entry := range loaded.productionAuthorityHistory.entries {
			if !bytes.Equal(entry.encoded, cfg.productionAuthorityHistory.entries[index].encoded) {
				t.Fatal("retained copies changed an original authority", treasury, index)
			}
		}
		if _, err := RetainOwnerRecycleApproval(t.Context(), loaded); err != nil {
			t.Fatal("a restart could not retain its loaded authority behind untraversable sources", treasury, err)
		}
		// A retained copy is admitted only at the signed size and digest. A
		// same-length change refuses the load and reports the source refusal.
		copies := []string{retainedProductionApprovalPath(cfg)}
		for _, reference := range cfg.ProductionRuntimeApprovals {
			copies = append(copies, retainedProductionRuntimePath(cfg, reference.SHA256))
		}
		for _, reference := range cfg.ProductionAuthorityHistory {
			copies = append(copies, retainedProductionAuthorityPath(cfg, reference.SHA256))
		}
		for _, copied := range copies {
			original, err := os.ReadFile(copied)
			if err != nil {
				t.Fatal(err)
			}
			changed := bytes.Clone(original)
			changed[len(changed)/2] ^= 1
			if err := os.WriteFile(copied, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadReleaseConfig(pinned); err == nil || !errors.Is(err, syscall.EACCES) {
				t.Fatal("a wrong retained copy was admitted or hid the source refusal", treasury, filepath.Base(copied), err)
			}
			if err := os.Remove(copied); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadReleaseConfig(pinned); err == nil || !errors.Is(err, syscall.EACCES) {
				t.Fatal("an untraversable source without its retained copy was admitted", treasury, filepath.Base(copied), err)
			}
			if err := os.WriteFile(copied, original, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := LoadReleaseConfig(pinned); err != nil {
			t.Fatal("restored retained copies were refused", treasury, err)
		}
		// Any other source path error stays fatal even with a valid retained
		// copy: here the approval directory becomes a symlink to its contents.
		source := filepath.Dir(productionEconomicSelection(cfg).Approval.Path)
		if err := os.Chmod(source, 0o700); err != nil {
			t.Fatal(err)
		}
		moved := source + "-moved"
		if err := os.Rename(source, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, source); err != nil {
			t.Fatal(err)
		}
		_, err = LoadReleaseConfig(pinned)
		if err == nil || !strings.Contains(err.Error(), "symlink or non-directory ancestor") {
			t.Fatal("a redirected source ancestor was excused like an untraversable one", treasury, err)
		}
		if err := os.Remove(source); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(moved, source); err != nil {
			t.Fatal(err)
		}
	}
}
