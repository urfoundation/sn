// Real migration filenames remain release inputs when the monitor splits its
// catalog into separate source files; only production bytes may be retained.
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The server's renamed catalogs and adjacent migration implementations must
// survive both selection and exact-byte retention, beside the original signal.
func TestReleaseBuildRetainsRenamedAndAdjacentMigrationInputs(t *testing.T) {
	config := buildConfig{Workspace: t.TempDir(), Output: t.TempDir()}
	if err := os.Mkdir(filepath.Join(config.Output, "inputs"), 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"server/db_migration_catalog.go",
		"server/db_migrations.go",
		"server/db_migrations_code.go",
		"server/monitor/signal_migrations.go",
		"server/monitor/migration_fp2.go",
		"server/monitor/migration_reliability_observation.go",
		"server/monitor/migration_staging_approval.go",
		"server/monitor/migration_url_completion.go",
		"server/monitor/migrations_net_escrow.go",
		"server/monitor/migrations_registration_subscriber.go",
		"server/monitor/migrations_usage_archive.go",
		"server/monitor/migrations_usage_time.go",
	}
	wantedInputPathKVs := map[string]string{}
	for _, relative := range paths {
		path := filepath.Join(config.Workspace, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package synthetic\n// "+relative+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		wantedInputPathKVs[strings.ReplaceAll(relative, "/", "-")] = filepath.FromSlash(relative)
	}
	inputPathKVs, err := buildMigrationInputs(config.Workspace)
	if err != nil || !reflect.DeepEqual(inputPathKVs, wantedInputPathKVs) {
		t.Fatalf("renamed migration source disappeared from release inputs: got=%v wanted=%v error=%v", inputPathKVs, wantedInputPathKVs, err)
	}
	for id, relative := range inputPathKVs {
		artifact, err := retainBuildInput(config, id, "source-input", filepath.Join(config.Workspace, relative))
		if err != nil {
			t.Fatal(err)
		}
		wantedRaw := "package synthetic\n// " + filepath.ToSlash(relative) + "\n"
		retained, err := os.ReadFile(filepath.Join(config.Output, artifact.Path))
		if err != nil || string(retained) != wantedRaw || artifact.Id != id || artifact.Kind != "source-input" || artifact.Path != "inputs/"+id || artifact.Bytes != int64(len(wantedRaw)) || artifact.Sha256 != buildFixtureDigest(wantedRaw) {
			t.Fatalf("retained migration source identity differs: %+v error=%v", artifact, err)
		}
	}
}

// Broad monitor matching must not admit test fixtures, non-Go files or sibling
// packages; it keeps the existing top-level database-migration boundary.
func TestReleaseBuildMigrationInputsExcludeTestsAndUnrelatedFiles(t *testing.T) {
	workspace := t.TempDir()
	for _, relative := range []string{
		"server/db_migrations_test.go",
		"server/monitor/signal_migrations_test.go",
		"server/monitor/migrations_registration_subscriber_test.go",
		"server/monitor/migration_staging_approval_test.go",
		"server/monitor/migrations_usage_time.go.bak",
		"server/monitor/migrations.md",
		"server/monitor/signal_api_release_proof.go",
		"server/other/migrations.go",
		"sn/monitor/migrations.go",
	} {
		path := filepath.Join(workspace, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("unselected input\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inputPathKVs, err := buildMigrationInputs(workspace)
	if err != nil || len(inputPathKVs) != 0 {
		t.Fatalf("non-production migration input entered release: %v error=%v", inputPathKVs, err)
	}
}
