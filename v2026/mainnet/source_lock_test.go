package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sourceLockTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func sourceLockTestRepository(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sourceLockTestGit(t, dir, "init", "-q")
	sourceLockTestGit(t, dir, "config", "user.name", "Fixture")
	sourceLockTestGit(t, dir, "config", "user.email", "fixture@example.test")
	sourceLockTestGit(t, dir, "add", ".")
	sourceLockTestGit(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "synthetic source")
	return sourceLockTestGit(t, dir, "rev-parse", "HEAD")
}

func sourceLockTestWorkspace(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	dependencyDir := filepath.Join(root, "dependency")
	dependencyCommit := sourceLockTestRepository(t, dependencyDir, map[string]string{"go.mod": "module example.test/dependency\n\ngo 1.26.5\n", "dependency.go": "package dependency\n"})
	snDir := filepath.Join(root, "sn")
	snCommit := sourceLockTestRepository(t, snDir, map[string]string{
		"go.mod": "module github.com/urfoundation/sn\n\ngo 1.26.5\n\nrequire example.test/dependency v0.0.0\nreplace example.test/dependency => ../dependency\n",
		"go.sum": "",
	})
	return snDir, snCommit, dependencyCommit
}

func TestSourceLockBindsCleanLocalDependencies(t *testing.T) {
	snDir, snCommit, dependencyCommit := sourceLockTestWorkspace(t)
	first, err := buildSourceLock(context.Background(), snDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildSourceLock(context.Background(), snDir)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != second.ContentHash || first.ContentHash == "" {
		t.Fatalf("source lock content hash is unstable: %s %s", first.ContentHash, second.ContentHash)
	}
	if first.Schema != sourceLockSchema || first.Scope != "clean-git-sources-and-local-go-replacements-only" || len(first.Repositories) != 2 || len(first.LocalReplacements) != 1 {
		t.Fatalf("source lock omitted required source: %+v", first)
	}
	if first.Repositories[0].Path != "." || first.Repositories[0].Commit != snCommit || first.Repositories[1].Path != "../dependency" || first.Repositories[1].Commit != dependencyCommit {
		t.Fatalf("source lock repositories differ: %+v", first.Repositories)
	}
	if first.LocalReplacements[0].Module != "example.test/dependency" || first.LocalReplacements[0].Repository != "../dependency" {
		t.Fatalf("source lock replacement differs: %+v", first.LocalReplacements)
	}
	var output bytes.Buffer
	var errors bytes.Buffer
	if code := runMain(context.Background(), []string{"source-lock", "--sn-dir", snDir}, &output, &errors); code != 0 {
		t.Fatalf("source-lock exit %d: %s", code, errors.String())
	}
	var encoded sourceLock
	if err := json.Unmarshal(output.Bytes(), &encoded); err != nil || encoded.ContentHash != first.ContentHash {
		t.Fatalf("source-lock command differs: %+v %v", encoded, err)
	}
}

func TestSourceLockRejectsDirtyOrMissingLocalSource(t *testing.T) {
	snDir, _, _ := sourceLockTestWorkspace(t)
	dependencyDir := filepath.Join(filepath.Dir(snDir), "dependency")
	if err := os.WriteFile(filepath.Join(dependencyDir, "new.go"), []byte("package dependency\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildSourceLock(context.Background(), snDir); err == nil || !strings.Contains(err.Error(), "uncommitted or untracked") {
		t.Fatalf("untracked local replacement was accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(dependencyDir, "new.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snDir, "go.sum"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildSourceLock(context.Background(), snDir); err == nil || !strings.Contains(err.Error(), "uncommitted or untracked") {
		t.Fatalf("modified SN source was accepted: %v", err)
	}
}

func TestSourceLockRejectsIgnoredReplacementModule(t *testing.T) {
	snDir, _, _ := sourceLockTestWorkspace(t)
	dependencyDir := filepath.Join(filepath.Dir(snDir), "dependency")
	sourceLockTestGit(t, dependencyDir, "rm", "--cached", "go.mod")
	if err := os.WriteFile(filepath.Join(dependencyDir, ".gitignore"), []byte("go.mod\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sourceLockTestGit(t, dependencyDir, "add", ".gitignore")
	sourceLockTestGit(t, dependencyDir, "-c", "commit.gpgsign=false", "commit", "-qm", "synthetic ignored module")
	if status := sourceLockTestGit(t, dependencyDir, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("synthetic repository is not clean: %s", status)
	}
	if _, err := buildSourceLock(context.Background(), snDir); err == nil || !strings.Contains(err.Error(), "not tracked") {
		t.Fatalf("ignored replacement module was accepted: %v", err)
	}
}

func TestSourceLockRejectsActiveWorkspaceOverride(t *testing.T) {
	snDir, _, _ := sourceLockTestWorkspace(t)
	workspacePath := filepath.Join(filepath.Dir(snDir), "go.work")
	if err := os.WriteFile(workspacePath, []byte("go 1.26.5\nuse ./sn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", workspacePath)
	if _, err := buildSourceLock(context.Background(), snDir); err == nil || !strings.Contains(err.Error(), "active go.work") {
		t.Fatalf("active workspace override was accepted: %v", err)
	}
}
