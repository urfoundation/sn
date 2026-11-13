// This command reads bounded local published artifacts and emits an unsigned
// provenance result. Account authority and execution have no input surface.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

const safeReleaseVerificationSchema = "urnetwork-mainnet-safe-release-verification-v1"

// Selected artifact facts retain the exact upstream identity and compiler slots.
type safeReleaseArtifactObservation struct {
	Pin          safeReleaseArtifactPin   `json:"artifact"`
	StorageSlots []safeReleaseStorageSlot `json:"storage_slots"`
}

// Static file agreement cannot establish deployed code, owner control, a signed
// successor or installation. Those false fields are part of the public result.
type safeReleaseVerification struct {
	Schema                          string                           `json:"schema"`
	Status                          string                           `json:"status"`
	Version                         string                           `json:"version"`
	Variant                         string                           `json:"variant"`
	PackageName                     string                           `json:"package_name"`
	ArchivePath                     string                           `json:"archive_path"`
	ArchiveSha256                   string                           `json:"archive_sha256"`
	CatalogSha256                   string                           `json:"catalog_sha256"`
	SourceCommit                    string                           `json:"source_commit"`
	SourceTree                      string                           `json:"source_tree"`
	SourceInventoryHash             string                           `json:"source_inventory_hash"`
	SourceFileCount                 int                              `json:"source_file_count"`
	Dependencies                    []safeReleaseDependencyPin       `json:"dependencies"`
	BuildInfoSha256                 string                           `json:"build_info_sha256"`
	SolcVersion                     string                           `json:"solc_version"`
	SolcLongVersion                 string                           `json:"solc_long_version"`
	CompilerSettingsSha256          string                           `json:"compiler_settings_sha256"`
	Artifacts                       []safeReleaseArtifactObservation `json:"artifacts"`
	ArtifactIntegrityVerified       bool                             `json:"artifact_integrity_verified"`
	PublishedBuildInputsVerified    bool                             `json:"published_build_inputs_verified"`
	IndependentRebuildVerified      bool                             `json:"independent_rebuild_verified"`
	CurrentChainVerified            bool                             `json:"current_chain_verified"`
	SafeAddressVerified             bool                             `json:"safe_address_verified"`
	InitializerOwnerBindingVerified bool                             `json:"initializer_owner_binding_verified"`
	SafeAuthorityVerified           bool                             `json:"safe_authority_verified"`
	ApprovalSigningPayloadProvided  bool                             `json:"approval_signing_payload_provided"`
	Executable                      bool                             `json:"executable"`
	NetworkEffects                  bool                             `json:"network_effects"`
	InstallationComplete            bool                             `json:"installation_complete"`
	ActivationReady                 bool                             `json:"activation_ready"`
	RequiredPrerequisites           []string                         `json:"required_prerequisites"`
	ContentHash                     string                           `json:"content_hash"`
}

// No default profile, path expansion, route, signer, address or custody flags
// are accepted. Zero means artifact verification only, not deployment readiness.
func runSafeReleaseVerify(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("safe-release-verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.String("version", "", "explicit published Safe version: 1.4.1 or 1.5.0")
	variant := flags.String("variant", "", "explicit singleton variant: Safe or SafeL2")
	archivePath := flags.String("archive", "", "absolute local path to the exact published npm archive")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !filepath.IsAbs(*archivePath) {
		fmt.Fprintln(stderr, "safe-release-verify requires --version, --variant and --archive ABSOLUTE_FILE")
		return 2
	}
	profile, err := loadSafeReleasePin(*version, *variant)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw, _, err := readPlanFile(ctx, *archivePath, maximumSafeReleaseArchiveBytes)
	if err != nil {
		fmt.Fprintln(stderr, "Safe published archive:", err)
		return 1
	}
	result, _, err := inspectSafeReleaseArchive(ctx, profile, *variant, *archivePath, raw)
	if err != nil {
		fmt.Fprintln(stderr, "Safe published artifact provenance:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "Safe verification output:", err)
		return 1
	}
	return 0
}

// Shared archive admission authenticates the full published release before a
// successor may use its singleton ABI. Returned members are local owned bytes.
func inspectSafeReleaseArchive(ctx context.Context, profile safeReleasePin, variant, path string, raw []byte) (safeReleaseVerification, map[string][]byte, error) {
	members, err := readSafeReleaseMembers(ctx, raw, profile, variant)
	if err != nil {
		return safeReleaseVerification{}, nil, err
	}
	var metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(members["package/package.json"], &metadata); err != nil || metadata.Name != profile.PackageName || metadata.Version != profile.Version {
		return safeReleaseVerification{}, nil, errors.New("Safe published package identity differs")
	}
	artifacts, err := verifySafeReleaseBuild(members, profile, variant)
	if err != nil {
		return safeReleaseVerification{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return safeReleaseVerification{}, nil, err
	}
	result := safeReleaseVerification{Schema: safeReleaseVerificationSchema, Status: "published-release-artifacts-verified",
		Version: profile.Version, Variant: variant, PackageName: profile.PackageName, ArchivePath: path,
		ArchiveSha256: profile.ArchiveSha256, CatalogSha256: safeReleaseHash(safeReleasePinsJson), SourceCommit: profile.SourceCommit, SourceTree: profile.SourceTree,
		SourceInventoryHash: rootObjectHash(profile.SourceHashes), SourceFileCount: len(profile.SourceHashes), Dependencies: profile.Dependencies,
		BuildInfoSha256: profile.BuildInfoSha256, SolcVersion: profile.SolcVersion, SolcLongVersion: profile.SolcLongVersion, CompilerSettingsSha256: profile.SettingsSha256,
		Artifacts: artifacts, ArtifactIntegrityVerified: true, PublishedBuildInputsVerified: true,
		RequiredPrerequisites: []string{"INDEPENDENT_COMPILER_REBUILD_AND_RELEASE_REVIEW", "CURRENT_CHAIN_AND_SELECTED_PROXY_SINGLETON_CODE",
			"SELECTED_SAFE_EQUALS_RETAINED_INITIALIZER_OWNER_OR_SEPARATE_AUTHORIZED_MIGRATION", "CURRENT_OWNERS_THRESHOLD_MODULES_GUARDS_AND_FALLBACK",
			"CURRENT_SAFE_NONCE_AND_GLOBAL_CUSTODY", "SIGNED_SUCCESSOR_ADOPTION_AND_CUMULATIVE_CAPS", "EXACT_SAFE_DIGEST_SIGNATURES_AND_RELAYER",
			"CANONICAL_RECEIPT_SAFE_INNER_SUCCESS_AND_EVIDENCE_DOMAIN"}}
	result.ContentHash = rootObjectHash(result)
	return result, members, nil
}
