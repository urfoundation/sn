//go:build linux || darwin

package validator

// Configuration fixtures name finite test-only allowances and byte references,
// not historical activation authority or approved production capacities.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Values here exercise the real types; none are exported as runtime defaults.
func releaseEvidenceV2TestConfig(root string, operators []OperatorConfig) ReleaseEvidenceV2Config {
	stream := AttemptStreamV2Bounds{MaxDataBytes: 4 * 1024 * 1024, MaxItems: 128, MaxChunkBytes: 128 * 1024, MaxChunks: 128, MaxPages: 8, MaxPageBytes: 4096, MaxDescriptorsPerPage: 16, MaxManifestBytes: 1024}
	config := ReleaseEvidenceV2Config{Schema: ReleaseEvidenceV2ConfigSchema, Bounds: ReleaseEvidenceV2Bounds{
		Disk:         AttemptLedgerDiskLimits{MaxRecordBytes: 64 * 1024, MaxRecordCount: 128, MaxTrailCount: 16, MaxRawRecordBytes: 4 * 1024 * 1024, MaxStorageBytes: 16 * 1024 * 1024, MaxStorageFiles: 256, MaxLegacyBytes: 4 * 1024 * 1024, MaxProofBytes: 1024 * 1024},
		Cut:          AttemptCutV2Bounds{MaxHeaderBytes: max(stream.MaxManifestBytes, stream.MaxPageBytes), Records: stream, Proofs: stream},
		Replay:       AttemptCutV2ReplayBounds{MaxRecordBytes: 128 * 1024, MaxProofBytes: 128 * 1024, MaxTrails: 16, MaxScratchBytes: 16 * 1024 * 1024, MaxScratchFiles: 256},
		Persistence:  AttemptSettlementRuntimeV2PersistenceBounds{MaxSnapshotBytes: 2 * 1024 * 1024, MaxJournalBytes: 16 * 1024 * 1024},
		HeadEMA:      HeadEMAStoreV2Limits{MaxFileBytes: 2 * 1024 * 1024, MaxEntries: 64, MaxControlBytes: 8 * 1024 * 1024},
		MaxProviders: 128, MaxEgressHashes: 128, MaxFleetPrefixes: 128, MaxOperators: uint64(len(operators)), MaxHeadEntries: 64, MaxArtifactBytes: 64 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024,
		MaxInputJournalBytes: 4 * 1024 * 1024,
		MaxParticipants:      uint64(len(operators)), MaxTransitionBytes: 1024 * 1024, MaxClosureBytes: 4 * 1024 * 1024, MaxHistoryBytes: 1024 * 1024,
	}}
	for _, operator := range operators {
		input := filepath.Join(root, "evidence-v2", fmt.Sprintf("no-%d", operator.NoID))
		ref := func(name string, size uint64) ReleaseEvidenceV2File {
			return ReleaseEvidenceV2File{Path: filepath.Join(input, name), Bytes: size, SHA256: "0x" + strings.Repeat("ab", 32)}
		}
		config.Operators = append(config.Operators, ReleaseEvidenceV2OperatorConfig{NoID: operator.NoID,
			Activation: ref("activation.payload", uint64(protocol.ValidatorEvidenceActivationPayloadSize)), VPKSignature: ref("vpk.signature", 64), HotkeySignature: ref("hotkey.signature", 64), Context: ref("context.json", 32), History: ref("history.json", 32),
			ReplayScratchRoot: filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", operator.NoID), "replay"), SealScratchRoot: filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", operator.NoID), "seal"),
		})
	}
	return config
}

// The exact existing lower-case YAML names are stable configuration syntax.
func TestReleaseEvidenceV2ConfigExactRoundTrip(t *testing.T) {
	config := validReleaseConfig(t)
	wire, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"maxrecordbytes:", "maxsnapshotbytes:", "maxjournalbytes:", "maxcontrolbytes:", "maxdescriptorsperpage:"} {
		if !bytes.Contains(wire, []byte(name)) {
			t.Fatalf("real bound name %q absent", name)
		}
	}
	loaded, err := LoadReleaseConfig(writeReleaseConfig(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.EvidenceV2, config.EvidenceV2) {
		t.Fatal("rendered/parsed evidence bounds or references changed")
	}
	if loaded.RuntimeSpec != config.RuntimeSpec || loaded.RuntimeCodeHash != config.RuntimeCodeHash || loaded.RuntimeMetadataHash != config.RuntimeMetadataHash {
		t.Fatal("evidence configuration changed native pins")
	}
}

// Every original scalar allowance is independently mandatory, even in an
// empty start. The explicit compatibility count retains its prior hard limit.
func TestReleaseEvidenceV2ConfigRequiresEveryBound(t *testing.T) {
	config := validReleaseConfig(t)
	var paths [][]int
	var walk func(reflect.Type, []int)
	walk = func(valueType reflect.Type, prefix []int) {
		for index := 0; index < valueType.NumField(); index++ {
			if valueType == reflect.TypeFor[ReleaseEvidenceV2Bounds]() && valueType.Field(index).Name == "MaxCaptureFiles" {
				continue
			}
			path := append(append([]int(nil), prefix...), index)
			if valueType.Field(index).Type.Kind() == reflect.Struct {
				walk(valueType.Field(index).Type, path)
			} else {
				paths = append(paths, path)
			}
		}
	}
	walk(reflect.TypeFor[ReleaseEvidenceV2Bounds](), nil)
	for _, path := range paths {
		mutated := config.EvidenceV2.Bounds
		reflect.ValueOf(&mutated).Elem().FieldByIndex(path).SetUint(0)
		if err := mutated.Validate(uint64(len(config.Operators))); err == nil {
			t.Fatalf("zero bound %v accepted", path)
		}
	}
	if config.EvidenceV2.Bounds.MaxCaptureFiles != 0 || config.EvidenceV2.Bounds.CaptureFileLimit() != ReleaseEvidenceV2DefaultCaptureFiles {
		t.Fatal("absent optional capture count changed its explicit compatibility ceiling")
	}
	config.EvidenceV2 = ReleaseEvidenceV2Config{}
	if loaded, err := LoadReleaseConfig(writeReleaseConfig(t, config)); err == nil || loaded != nil {
		t.Fatal("production accepted absent V2 configuration")
	}
}

// Reject alternate numeric grammars before YAML can round or coerce them.
func TestReleaseEvidenceV2ConfigStrictIntegerGrammar(t *testing.T) {
	config := validReleaseConfig(t)
	wire, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	needle := "maxrecordbytes: 65536"
	if strings.Count(string(wire), needle) != 1 {
		t.Fatal("integer fixture does not identify one real disk bound")
	}
	for _, value := range []string{"\"65536\"", "65536.0", "6.5536e4", "+65536", "065536", "0x10000", "65_536", "-1", "18446744073709551616", "null", "true"} {
		mutated := strings.Replace(string(wire), needle, "maxrecordbytes: "+value, 1)
		path := filepath.Join(t.TempDir(), "validator.yml")
		if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
			t.Fatal(err)
		}
		if loaded, err := LoadReleaseConfig(path); err == nil || loaded != nil {
			t.Errorf("alternate integer %q accepted", value)
		}
	}
}

// KnownFields alone does not protect custom UnmarshalYAML's nested Decode.
func TestReleaseEvidenceV2ConfigRejectsUnknownDuplicateAndAlias(t *testing.T) {
	config := validReleaseConfig(t)
	wire, err := yaml.Marshal(config.EvidenceV2)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutated := range []string{
		strings.Replace(string(wire), "maxrecordbytes: 65536", "unknown_capacity: 65536", 1),
		strings.Replace(string(wire), "maxrecordbytes: 65536", "maxrecordbytes: 65536\n        maxrecordbytes: 65536", 1),
		strings.Replace(string(wire), "maxrecordbytes: 65536", "maxrecordbytes: &bound 65536", 1),
	} {
		var decoded ReleaseEvidenceV2Config
		err := yaml.Unmarshal([]byte(mutated), &decoded)
		if strings.Contains(mutated, "&bound") {
			// An unused anchor has no alias semantics; an actual alias must fail.
			mutated = strings.Replace(mutated, "maxrecordcount: 128", "maxrecordcount: *bound", 1)
			err = yaml.Unmarshal([]byte(mutated), &decoded)
		}
		if err == nil {
			t.Fatalf("non-exact evidence YAML accepted: %s", mutated)
		}
	}
}

// The native YAML library skips field custom decoders for null and aliases.
func TestReleaseEvidenceV2ConfigAdmitsOriginalNodesAndClearsReusedDecode(t *testing.T) {
	for _, wire := range []string{"evidence_v2: null\n", "prior: &owner {}\nevidence_v2: *owner\n", "validator_evidence_v2: null\n", "prior: &owners []\nvalidator_evidence_v2: *owners\n"} {
		var err error
		if strings.Contains(wire, "validator_evidence_v2:") {
			err = ValidateReleaseValidatorEvidenceV2YAML([]byte(wire))
		} else {
			err = ValidateReleaseEvidenceV2ConfigYAML([]byte(wire))
		}
		if err == nil {
			t.Fatalf("original alias/null node admitted: %s", wire)
		}
	}
	config := validReleaseConfig(t)
	wire, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	index := strings.Index(string(wire), "\nevidence_v2:")
	if index < 0 {
		t.Fatal("original-node fixture missed the evidence field")
	}
	path := filepath.Join(t.TempDir(), "validator.yml")
	if err := os.WriteFile(path, []byte(string(wire[:index])+"\nevidence_v2: null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReleaseConfig(path); err == nil || loaded != nil || !strings.Contains(err.Error(), "requires an object") {
		t.Fatalf("file loader did not inspect the original null node: %v", err)
	}
	decoded := config.EvidenceV2
	if err := yaml.Unmarshal([]byte("schema: "+ReleaseEvidenceV2ConfigSchema+"\n"), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds != (ReleaseEvidenceV2Bounds{}) || decoded.Operators != nil {
		t.Fatal("reused YAML destination retained omitted capacities or references")
	}
}

// Signed conversions, retained ceilings and stream/disk composition are checked.
func TestReleaseEvidenceV2ConfigRejectsOverflowAndMismatchedBounds(t *testing.T) {
	config := validReleaseConfig(t)
	for _, mutate := range []func(*ReleaseEvidenceV2Bounds){
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.Persistence.MaxJournalBytes = ^uint64(0) },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.MaxArtifactBytes++ },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.HeadEMA.MaxFileBytes = 64*1024*1024 + 1 },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.MaxOperators = 1 },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.MaxParticipants = 1 },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.MaxTransitionBytes = bounds.MaxClosureBytes + 1 },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.Cut.Records.MaxItems = bounds.Disk.MaxRecordCount - 1 },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.Cut.Proofs.MaxDataBytes = bounds.Disk.MaxProofBytes - 1 },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.Replay.MaxRecordBytes = bounds.Disk.MaxRecordBytes - 1 },
		func(bounds *ReleaseEvidenceV2Bounds) { bounds.Cut.Records.MaxPageBytes = 1 },
		func(bounds *ReleaseEvidenceV2Bounds) {
			bounds.Cut.Records.MaxChunks = uint64(^uint(0)>>1) / attemptStreamV2DescriptorBytes
			bounds.Cut.Proofs.MaxChunks = bounds.Cut.Records.MaxChunks
		},
	} {
		bounds := config.EvidenceV2.Bounds
		mutate(&bounds)
		if err := bounds.Validate(2); err == nil {
			t.Fatal("overflow or incompatible bound accepted")
		}
	}
}

// Operator identity is complete and references never authorize shared storage.
func TestReleaseEvidenceV2ConfigRejectsReferenceAndOwnerMismatch(t *testing.T) {
	for _, mutate := range []func(*ReleaseConfig){
		func(config *ReleaseConfig) { config.EvidenceV2.Operators = config.EvidenceV2.Operators[:1] },
		func(config *ReleaseConfig) { config.EvidenceV2.Operators[1].NoID = 1 },
		func(config *ReleaseConfig) {
			config.EvidenceV2.Operators[0], config.EvidenceV2.Operators[1] = config.EvidenceV2.Operators[1], config.EvidenceV2.Operators[0]
		},
		func(config *ReleaseConfig) { config.EvidenceV2.Operators[0].Activation.Bytes-- },
		func(config *ReleaseConfig) {
			config.EvidenceV2.Operators[0].Context.SHA256 = "0x" + strings.Repeat("AB", 32)
		},
		func(config *ReleaseConfig) {
			config.EvidenceV2.Operators[0].Context.Bytes = config.EvidenceV2.Bounds.Cut.MaxHeaderBytes + 1
		},
		func(config *ReleaseConfig) {
			config.EvidenceV2.Operators[0].ReplayScratchRoot = config.Operators[0].StateDir
		},
		func(config *ReleaseConfig) {
			config.EvidenceV2.Operators[1].SealScratchRoot = config.EvidenceV2.Operators[0].ReplayScratchRoot
		},
		func(config *ReleaseConfig) {
			config.EvidenceV2.Operators[1].Context.Path = config.EvidenceV2.Operators[0].Context.Path
		},
		func(config *ReleaseConfig) {
			config.Operators[1].StateDir = filepath.Join(config.Operators[0].StateDir, "child")
		},
		func(config *ReleaseConfig) {
			config.Operators[1].ClientJWTFile = filepath.Join(config.Operators[0].StateDir, "foreign.jwt")
		},
		func(config *ReleaseConfig) { config.Operators[1].ClientKeySeedFile = config.HotkeySeedFile },
	} {
		config := validReleaseConfig(t)
		mutate(&config)
		if loaded, err := LoadReleaseConfig(writeReleaseConfig(t, config)); err == nil || loaded != nil {
			t.Fatal("incomplete reference or conflicting owner accepted")
		}
	}
}

// Invalid spelling and physical ancestors are rejected before normalization.
func TestReleaseEvidenceV2ConfigRejectsNoncanonicalPhysicalPaths(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative", "/", root + "/../escape", root + "//child", root + "/bad\x00", root + "/bad\xff", filepath.Join(alias, "not-created", "file")} {
		if err := ValidateReleaseEvidenceV2Path(path); err == nil {
			t.Errorf("invalid physical path %q accepted", path)
		}
	}
	config := validReleaseConfig(t)
	config.Operators[0].StateDir += "/../normalized"
	if loaded, err := LoadReleaseConfig(writeReleaseConfig(t, config)); err == nil || loaded != nil {
		t.Fatal("legacy normalization legitimized V2 state path")
	}
}

// Coordinator nesting is permitted only in the intended ownership direction.
func TestReleaseEvidenceV2ConfigAllowsPrivateCoordinatorOperatorChildren(t *testing.T) {
	config := validReleaseConfig(t)
	for index := range config.Operators {
		config.Operators[index].StateDir = filepath.Join(config.StateDir, "operators", fmt.Sprintf("no-%d", index+1))
		if err := os.MkdirAll(config.Operators[index].StateDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, config)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.Operators[1].StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReleaseConfig(writeReleaseConfig(t, config)); err == nil || loaded != nil {
		t.Fatal("non-private operator owner was repaired or accepted")
	}
	info, err := os.Stat(config.Operators[1].StateDir)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("configuration changed directory permissions")
	}
}

// Valid configuration reaches the real non-creating seed owner, not a staged
// unconditional refusal. The deliberately missing seed still prevents I/O.
func TestRunReleaseV2ReachesOwnedSeedAdmissionBeforeRpcOrState(t *testing.T) {
	config := validReleaseConfig(t)
	parent := filepath.Join(t.TempDir(), "seed-owner")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	config.HotkeySeedFile = filepath.Join(parent, "hotkey.seed")
	if err := RunRelease(context.Background(), writeReleaseConfig(t, config)); !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "production hotkey seed") {
		t.Fatalf("entrypoint did not reach its actual non-creating seed owner: %v", err)
	}
	for _, path := range []string{config.StateDir, config.HotkeySeedFile, config.Operators[0].StateDir, config.Operators[1].StateDir} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("entrypoint touched %s: %v", path, err)
		}
	}
}

// The byte reader uses real files and native descriptor-relative operations.
func releaseEvidenceV2FileTest(t *testing.T) (ReleaseEvidenceV2File, []byte) {
	t.Helper()
	data := []byte("exact independently supplied context bytes\n")
	// testing.TempDir's numbered child uses 0777, subject to the process
	// umask. The real reader requires an independently private 0700 owner.
	directory := filepath.Join(t.TempDir(), "owner")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "context.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	reference := ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(data)), SHA256: "0x" + hex.EncodeToString(digest[:])}
	if actual, err := ReadReleaseEvidenceV2File(context.Background(), reference, reference.Bytes); err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("valid private reference prerequisite: %v", err)
	}
	return reference, data
}

func TestReleaseEvidenceV2ReferenceExactBoundAndCancellation(t *testing.T) {
	reference, data := releaseEvidenceV2FileTest(t)
	actual, err := ReadReleaseEvidenceV2File(context.Background(), reference, reference.Bytes)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("exact reference: %v", err)
	}
	if actual, err := ReadReleaseEvidenceV2File(context.Background(), reference, reference.Bytes-1); err == nil || actual != nil {
		t.Fatal("one-short reference bound accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if actual, err := ReadReleaseEvidenceV2File(ctx, reference, reference.Bytes); !errors.Is(err, context.Canceled) || actual != nil {
		t.Fatal("canceled reference published bytes")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if actual, err := readReleaseEvidenceV2File(ctx, reference, reference.Bytes, nil, cancel); !errors.Is(err, context.Canceled) || actual != nil {
		t.Fatal("late cancellation published reference bytes")
	}
}

func TestReleaseEvidenceV2ReferenceRejectsMutationAbsenceAndAliases(t *testing.T) {
	for _, mutate := range []func(*testing.T, ReleaseEvidenceV2File){
		func(t *testing.T, reference ReleaseEvidenceV2File) {
			if err := os.Chmod(filepath.Dir(reference.Path), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		func(t *testing.T, reference ReleaseEvidenceV2File) {
			if err := os.Remove(reference.Path); err != nil {
				t.Fatal(err)
			}
		},
		func(t *testing.T, reference ReleaseEvidenceV2File) {
			if err := os.WriteFile(reference.Path, bytes.Repeat([]byte("x"), int(reference.Bytes)), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		func(t *testing.T, reference ReleaseEvidenceV2File) {
			if err := os.Chmod(reference.Path, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		func(t *testing.T, reference ReleaseEvidenceV2File) {
			if err := os.Link(reference.Path, reference.Path+".alias"); err != nil {
				t.Fatal(err)
			}
		},
	} {
		reference, _ := releaseEvidenceV2FileTest(t)
		mutate(t, reference)
		if actual, err := ReadReleaseEvidenceV2File(context.Background(), reference, reference.Bytes); err == nil || actual != nil {
			t.Fatal("changed/missing/aliased reference accepted")
		}
	}
}

// A FIFO substituted after lstat cannot block the native no-follow open.
func TestReleaseEvidenceV2ReferencePostStatFIFORefused(t *testing.T) {
	reference, _ := releaseEvidenceV2FileTest(t)
	replaced := false
	actual, err := readReleaseEvidenceV2File(context.Background(), reference, reference.Bytes, func() {
		if err := os.Rename(reference.Path, reference.Path+".retained"); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(reference.Path, 0o600); err != nil {
			t.Fatal(err)
		}
		replaced = true
	}, nil)
	if !replaced {
		t.Fatal("post-stat FIFO fixture never reached its substitution boundary")
	}
	if err == nil || actual != nil {
		t.Fatal("post-stat FIFO replacement accepted")
	}
}

// Same bytes at a new inode do not inherit the earlier descriptor owner.
func TestReleaseEvidenceV2ReferencePostReadReplacementRefused(t *testing.T) {
	reference, data := releaseEvidenceV2FileTest(t)
	replaced := false
	actual, err := readReleaseEvidenceV2File(context.Background(), reference, reference.Bytes, nil, func() {
		if err := os.Rename(reference.Path, reference.Path+".retained"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reference.Path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		replaced = true
	})
	if !replaced {
		t.Fatal("post-read replacement fixture never reached its substitution boundary")
	}
	if err == nil || actual != nil {
		t.Fatal("same-byte replacement inherited reference ownership")
	}
}
