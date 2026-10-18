// Offline Safe release verification authenticates published compiler inputs and
// proxy/singleton artifacts. It has no account, custody, signer or chain adapter.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
)

const safeReleasePinsSchema = "urnetwork-safe-published-release-pins-v1"
const safeReleasePinsSha256 = "sha256:a3bb95c9234181a5e3017a130655cfbf6cd53f64c5f19108b9a1555362cbf626"
const maximumSafeReleaseArchiveBytes = 3 * 1024 * 1024
const maximumSafeReleaseExpandedBytes = 32 * 1024 * 1024
const maximumSafeReleaseMemberBytes = 10 * 1024 * 1024

// The independent source pin is replaced only with a reviewed catalog revision.
//
//go:embed safe-release/profiles.json
var safeReleasePinsJson []byte

// Source hashes were compared with both published build-info and release tags;
// the separately locked dependency retains its own package provenance.
type safeReleaseDependencyPin struct {
	PackageName   string            `json:"package_name"`
	Version       string            `json:"version"`
	ArchiveSha256 string            `json:"archive_sha256"`
	SourceHashes  map[string]string `json:"source_hashes"`
}

// Raw artifact identity and independently derived ABI/code/layout pins prevent
// a version or variant label from substituting another compiled implementation.
type safeReleaseArtifactPin struct {
	Name                string `json:"name"`
	SourceName          string `json:"source_name"`
	ArchivePath         string `json:"archive_path"`
	ArtifactSha256      string `json:"artifact_sha256"`
	AbiSha256           string `json:"abi_sha256"`
	CreationKeccak256   string `json:"creation_keccak256"`
	RuntimeKeccak256    string `json:"runtime_keccak256"`
	CreationBytes       int    `json:"creation_bytes"`
	RuntimeBytes        int    `json:"runtime_bytes"`
	StorageLayoutSha256 string `json:"storage_layout_sha256"`
}

// Compiler provenance describes published inputs. It is not a compiler binary
// attestation or a local reproducible build of the release.
type safeReleasePin struct {
	Version         string                     `json:"version"`
	PackageName     string                     `json:"package_name"`
	ArchiveName     string                     `json:"archive_name"`
	ArchiveSha256   string                     `json:"archive_sha256"`
	SourceCommit    string                     `json:"source_commit"`
	SourceTree      string                     `json:"source_tree"`
	BuildInfoPath   string                     `json:"build_info_path"`
	BuildInfoSha256 string                     `json:"build_info_sha256"`
	SolcVersion     string                     `json:"solc_version"`
	SolcLongVersion string                     `json:"solc_long_version"`
	SettingsSha256  string                     `json:"settings_sha256"`
	SourceHashes    map[string]string          `json:"source_hashes"`
	Dependencies    []safeReleaseDependencyPin `json:"dependencies"`
	Artifacts       []safeReleaseArtifactPin   `json:"artifacts"`
}

// Published Hardhat envelopes are interpreted only after their exact-byte pin.
type safeReleaseArtifact struct {
	Name     string          `json:"contractName"`
	Source   string          `json:"sourceName"`
	Abi      json.RawMessage `json:"abi"`
	Creation string          `json:"bytecode"`
	Runtime  string          `json:"deployedBytecode"`
}

// Slot meanings are reported from compiler output after its complete layout pin.
type safeReleaseStorageSlot struct {
	Label  string `json:"label"`
	Offset int    `json:"offset"`
	Slot   string `json:"slot"`
	Type   string `json:"type"`
}

// Only the relevant published compiler fields are decoded after the build pin;
// unrelated AST/debug records cannot supply authority or executable configuration.
type safeReleaseBuild struct {
	SolcVersion     string `json:"solcVersion"`
	SolcLongVersion string `json:"solcLongVersion"`
	Input           struct {
		Language string          `json:"language"`
		Settings json.RawMessage `json:"settings"`
		Sources  map[string]struct {
			Content string `json:"content"`
		} `json:"sources"`
	} `json:"input"`
	Output struct {
		Contracts map[string]map[string]struct {
			Abi           json.RawMessage `json:"abi"`
			StorageLayout json.RawMessage `json:"storageLayout"`
			Evm           struct {
				Bytecode struct {
					Object string `json:"object"`
				} `json:"bytecode"`
				DeployedBytecode struct {
					Object string `json:"object"`
				} `json:"deployedBytecode"`
			} `json:"evm"`
		} `json:"contracts"`
	} `json:"output"`
}

// File and source hashes retain the mainnet review grammar.
func safeReleaseHash(raw []byte) string {
	hash := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// JSON whitespace is not ABI or layout semantics; field ordering stays pinned.
func safeReleaseJsonHash(raw []byte) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	return safeReleaseHash(compact.Bytes()), nil
}

// No default version or variant can silently select a different account model.
func loadSafeReleasePin(version, variant string) (safeReleasePin, error) {
	if version != "1.4.1" && version != "1.5.0" || variant != "Safe" && variant != "SafeL2" {
		return safeReleasePin{}, errors.New("Safe release requires explicit version 1.4.1 or 1.5.0 and variant Safe or SafeL2")
	}
	if safeReleaseHash(safeReleasePinsJson) != safeReleasePinsSha256 {
		return safeReleasePin{}, errors.New("Safe release catalog differs from its independent pin")
	}
	var catalog struct {
		Schema   string           `json:"schema"`
		Profiles []safeReleasePin `json:"profiles"`
	}
	if err := decodePlanJson(safeReleasePinsJson, &catalog); err != nil {
		return safeReleasePin{}, err
	}
	if catalog.Schema != safeReleasePinsSchema || len(catalog.Profiles) != 2 {
		return safeReleasePin{}, errors.New("Safe release catalog census differs")
	}
	for _, profile := range catalog.Profiles {
		if profile.Version == version {
			return profile, nil
		}
	}
	return safeReleasePin{}, errors.New("Safe release profile is absent")
}

// A bounded, non-extracting reader retains required regular members only. Whole
// archive identity is authenticated before any compressed input is interpreted.
func readSafeReleaseMembers(ctx context.Context, raw []byte, profile safeReleasePin, variant string) (map[string][]byte, error) {
	if ctx == nil || ctx.Err() != nil || len(raw) == 0 || len(raw) > maximumSafeReleaseArchiveBytes || safeReleaseHash(raw) != profile.ArchiveSha256 {
		return nil, errors.New("Safe published archive differs from the selected release pin or context")
	}
	wanted := map[string]bool{profile.BuildInfoPath: true, "package/package.json": true}
	for name := range profile.SourceHashes {
		wanted["package/"+name] = true
	}
	for _, artifact := range profile.Artifacts {
		if artifact.Name == variant || artifact.Name == "SafeProxy" {
			wanted[artifact.ArchivePath] = true
		}
	}
	compressedInput := bytes.NewReader(raw)
	compressed, err := gzip.NewReader(compressedInput)
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	compressed.Multistream(false)
	bounded := &io.LimitedReader{R: releaseInventoryReader{ctx: ctx, reader: compressed}, N: maximumSafeReleaseExpandedBytes + 1}
	reader := tar.NewReader(bounded)
	members := map[string][]byte{}
	seen := map[string]bool{}
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if count >= 4096 || header.Size < 0 || header.Size > maximumSafeReleaseMemberBytes || bounded.N <= 0 || seen[header.Name] {
			return nil, errors.New("Safe archive member count, size or uniqueness differs")
		}
		seen[header.Name] = true
		if wanted[header.Name] {
			if header.Typeflag != tar.TypeReg {
				return nil, errors.New("Safe artifact member is not a regular file")
			}
			value, err := io.ReadAll(reader)
			if err != nil {
				return nil, err
			}
			members[header.Name] = value
		} else if _, err := io.Copy(io.Discard, reader); err != nil {
			return nil, err
		}
	}
	if _, err := io.Copy(io.Discard, bounded); err != nil {
		return nil, err
	}
	if bounded.N <= 0 || compressedInput.Len() != 0 || len(members) != len(wanted) {
		return nil, errors.New("Safe archive is oversized, has trailing streams or lacks required members")
	}
	return members, ctx.Err()
}

// Code and ABI are separately bound to the reviewed artifact, even if future
// catalog tooling changes an outer archive or artifact formatting pin.
func verifySafeReleaseArtifact(raw []byte, pin safeReleaseArtifactPin) (safeReleaseArtifact, error) {
	var artifact safeReleaseArtifact
	if len(raw) == 0 || len(raw) > 256*1024 || safeReleaseHash(raw) != pin.ArtifactSha256 {
		return artifact, errors.New("Safe artifact exact bytes differ")
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return artifact, err
	}
	if artifact.Name != pin.Name || artifact.Source != pin.SourceName || !strings.HasPrefix(artifact.Creation, "0x") || !strings.HasPrefix(artifact.Runtime, "0x") {
		return artifact, errors.New("Safe artifact version/variant identity differs")
	}
	creation, creationErr := contractCode(artifact.Creation[2:], 48*1024)
	runtime, runtimeErr := contractCode(artifact.Runtime[2:], 24*1024)
	if creationErr != nil || runtimeErr != nil || len(creation) != pin.CreationBytes || len(runtime) != pin.RuntimeBytes ||
		crypto.Keccak256Hash(creation).Hex() != pin.CreationKeccak256 || crypto.Keccak256Hash(runtime).Hex() != pin.RuntimeKeccak256 {
		return artifact, errors.New("Safe artifact creation/runtime bytecode differs")
	}
	abiHash, err := safeReleaseJsonHash(artifact.Abi)
	if err != nil || abiHash != pin.AbiSha256 {
		return artifact, errors.New("Safe artifact ABI differs")
	}
	if _, err := abi.JSON(bytes.NewReader(artifact.Abi)); err != nil {
		return artifact, err
	}
	return artifact, nil
}

// The singleton occupies slot zero and the Safe nonce slot five. Both selected
// releases use this layout, independently of their different runtime bytecode.
func verifySafeReleaseStorage(raw []byte, pin safeReleaseArtifactPin) ([]safeReleaseStorageSlot, error) {
	hash, err := safeReleaseJsonHash(raw)
	if err != nil || hash != pin.StorageLayoutSha256 {
		return nil, errors.New("Safe storage layout pin differs")
	}
	var layout struct {
		Storage []safeReleaseStorageSlot `json:"storage"`
	}
	if err := json.Unmarshal(raw, &layout); err != nil {
		return nil, err
	}
	labels := []string{"singleton", "modules", "owners", "ownerCount", "threshold", "nonce", "_deprecatedDomainSeparator", "signedMessages", "approvedHashes"}
	types := []string{"t_address", "t_mapping(t_address,t_address)", "t_mapping(t_address,t_address)", "t_uint256", "t_uint256", "t_uint256", "t_bytes32", "t_mapping(t_bytes32,t_uint256)", "t_mapping(t_address,t_mapping(t_bytes32,t_uint256))"}
	if pin.Name == "SafeProxy" {
		labels, types = labels[:1], types[:1]
	}
	if len(layout.Storage) != len(labels) {
		return nil, errors.New("Safe storage layout slot census differs")
	}
	for i, slot := range layout.Storage {
		if slot.Label != labels[i] || slot.Slot != strconv.Itoa(i) || slot.Offset != 0 || slot.Type != types[i] {
			return nil, errors.New("Safe singleton/owner/nonce storage semantics differ")
		}
	}
	return layout.Storage, nil
}

// Published compiler input sources must match separately captured tag/dependency
// hashes, and emitted ABI/code/layout must agree with each selected artifact.
func verifySafeReleaseBuild(members map[string][]byte, profile safeReleasePin, variant string) ([]safeReleaseArtifactObservation, error) {
	raw := members[profile.BuildInfoPath]
	if len(raw) == 0 || len(raw) > maximumSafeReleaseMemberBytes || safeReleaseHash(raw) != profile.BuildInfoSha256 {
		return nil, errors.New("Safe published build-info differs")
	}
	var build safeReleaseBuild
	if err := json.Unmarshal(raw, &build); err != nil {
		return nil, err
	}
	settingsHash, err := safeReleaseJsonHash(build.Input.Settings)
	if err != nil || build.Input.Language != "Solidity" || build.SolcVersion != profile.SolcVersion || build.SolcLongVersion != profile.SolcLongVersion || settingsHash != profile.SettingsSha256 {
		return nil, errors.New("Safe published compiler/settings provenance differs")
	}
	sourceCount := len(profile.SourceHashes)
	for _, dependency := range profile.Dependencies {
		sourceCount += len(dependency.SourceHashes)
	}
	if len(build.Input.Sources) != sourceCount {
		return nil, errors.New("Safe compiler source census differs")
	}
	for name, expected := range profile.SourceHashes {
		source, ok := build.Input.Sources[name]
		if !ok || safeReleaseHash([]byte(source.Content)) != expected || string(members["package/"+name]) != source.Content {
			return nil, fmt.Errorf("Safe compiler source/tag/archive provenance differs at %s", name)
		}
	}
	for _, dependency := range profile.Dependencies {
		for name, expected := range dependency.SourceHashes {
			source, ok := build.Input.Sources[name]
			if !ok || safeReleaseHash([]byte(source.Content)) != expected {
				return nil, fmt.Errorf("Safe locked dependency source differs at %s", name)
			}
		}
	}
	observations := []safeReleaseArtifactObservation{}
	for _, pin := range profile.Artifacts {
		if pin.Name != variant && pin.Name != "SafeProxy" {
			continue
		}
		artifact, err := verifySafeReleaseArtifact(members[pin.ArchivePath], pin)
		if err != nil {
			return nil, err
		}
		output, ok := build.Output.Contracts[pin.SourceName][pin.Name]
		outputAbiHash, abiErr := safeReleaseJsonHash(output.Abi)
		if !ok || abiErr != nil || outputAbiHash != pin.AbiSha256 || "0x"+output.Evm.Bytecode.Object != artifact.Creation || "0x"+output.Evm.DeployedBytecode.Object != artifact.Runtime {
			return nil, errors.New("Safe published compiler output differs from artifact")
		}
		slots, err := verifySafeReleaseStorage(output.StorageLayout, pin)
		if err != nil {
			return nil, err
		}
		observations = append(observations, safeReleaseArtifactObservation{Pin: pin, StorageSlots: slots})
	}
	if len(observations) != 2 {
		return nil, errors.New("Safe selected proxy/singleton census differs")
	}
	return observations, nil
}
