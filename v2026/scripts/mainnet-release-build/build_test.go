package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

// Synthetic artifacts exercise the admission boundary without executing builds.
func buildFixtureDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func buildFixtureManifest() buildManifest {
	manifest := buildManifest{Schema: buildSchema, Roles: releaseRoles(), SourceToBytecodeExact: true, Artifacts: []buildArtifact{}, Contracts: []buildContract{}, Images: []buildImage{}, MissingImages: []string{}}
	for _, role := range manifest.Roles {
		binary := buildArtifact{Id: role.Id, Kind: "binary", Path: "binaries/" + role.Id, Sha256: buildFixtureDigest(role.Id), Bytes: 64}
		manifest.Artifacts = append(manifest.Artifacts, binary)
		if role.Image {
			name := strings.TrimPrefix(role.Id, "server-")
			recipeHash := buildFixtureDigest("recipe-" + name)
			manifest.Artifacts = append(manifest.Artifacts, buildArtifact{Id: "image-" + name + "-binary", Kind: "image-input", Path: "contexts/" + name + "/binary", Sha256: binary.Sha256, Bytes: 64}, buildArtifact{Id: "image-" + name + "-dockerfile", Kind: "image-input", Path: "contexts/" + name + "/Dockerfile", Sha256: recipeHash, Bytes: 32})
			manifest.Images = append(manifest.Images, buildImage{Id: role.Id, BinarySha256: binary.Sha256, DockerfileSha256: recipeHash})
			manifest.MissingImages = append(manifest.MissingImages, role.Id)
		}
	}
	for _, name := range []string{"Coordinator", "ERC1967Proxy", "ReserveSink", "SettlementVault", "ValidatorEvidence"} {
		artifact := buildArtifact{Id: "contract-" + name + ".json", Kind: "foundry-contract", Path: "inputs/" + name + ".json", Sha256: buildFixtureDigest(name), Bytes: 256}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
		creation, runtime := buildFixtureDigest(name+"-creation"), buildFixtureDigest(name+"-runtime")
		runtimeHash, artifactHash := "0x"+strings.Repeat("1", 64), "0x"+strings.Repeat("2", 64)
		manifest.Contracts = append(manifest.Contracts, buildContract{Name: name, FoundryArtifactId: artifact.Id, FoundryArtifactSha256: artifact.Sha256, RetainedCreationSha256: creation, RetainedRuntimeSha256: runtime, SelectedCreationSha256: creation, SelectedRuntimeSha256: runtime, SelectedRuntimeHash: runtimeHash, SelectedArtifactHash: artifactHash, RetainedRuntimeHash: runtimeHash, RetainedArtifactHash: artifactHash, RebuiltCreationSha256: creation, RebuiltRuntimeSha256: runtime, CreationBytes: 128, RuntimeBytes: 64, ExactBytes: true})
	}
	return manifest
}

// The older seven-binary list excluded current workers, monitoring and recovery.
func TestReleaseBuildKeepsSeventeenCommandsAndEightImages(t *testing.T) {
	manifest := buildFixtureManifest()
	if err := sealBuildManifest(&manifest); err != nil {
		t.Fatal(err)
	}
	wanted := []string{"sn-mainnet", "sn-miner", "sn-validator", "sn-snclaim", "server-api", "server-taskworker", "server-proxy", "server-connect", "server-alt", "server-gossip", "server-mcp", "server-competitionworker", "server-monitor", "server-strecovery", "server-competitiondbinit", "server-competitionpatch", "server-geolite2export"}
	actual := []string{}
	for _, role := range manifest.Roles {
		actual = append(actual, role.Id)
	}
	if !reflect.DeepEqual(actual, wanted) || len(manifest.Images) != 8 || len(manifest.Contracts) != 5 {
		t.Fatalf("release selection narrowed: roles=%v images=%d contracts=%d", actual, len(manifest.Images), len(manifest.Contracts))
	}
	if manifest.ContentHash == "" || manifest.ReleaseComplete || manifest.DeploymentApproved || manifest.SourceToImageVerified || manifest.ReproducibilityVerified {
		t.Fatalf("composition granted unsupported authority: %+v", manifest)
	}
}

func TestReleaseBuildRejectsEveryMissingCommandBinary(t *testing.T) {
	for _, role := range releaseRoles() {
		manifest := buildFixtureManifest()
		for index, artifact := range manifest.Artifacts {
			if artifact.Id == role.Id {
				manifest.Artifacts = append(manifest.Artifacts[:index], manifest.Artifacts[index+1:]...)
				break
			}
		}
		if err := sealBuildManifest(&manifest); err == nil {
			t.Fatalf("accepted missing binary %s", role.Id)
		}
	}
}

func TestReleaseBuildRejectsMissingOrDuplicateProductionContract(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		manifest := buildFixtureManifest()
		if duplicate {
			manifest.Contracts[4] = manifest.Contracts[0]
		} else {
			manifest.Contracts = manifest.Contracts[:4]
		}
		if err := sealBuildManifest(&manifest); err == nil {
			t.Fatalf("accepted incomplete contract census; duplicate=%t", duplicate)
		}
	}
}

func TestReleaseBuildRejectsMissingOrReboundFoundryArtifact(t *testing.T) {
	for _, missing := range []bool{false, true} {
		manifest := buildFixtureManifest()
		id := manifest.Contracts[0].FoundryArtifactId
		for index, artifact := range manifest.Artifacts {
			if artifact.Id == id {
				if missing {
					manifest.Artifacts = append(manifest.Artifacts[:index], manifest.Artifacts[index+1:]...)
				} else {
					manifest.Artifacts[index].Sha256 = buildFixtureDigest("different-foundry-input")
				}
				break
			}
		}
		if err := sealBuildManifest(&manifest); err == nil {
			t.Fatalf("accepted absent/rebound Foundry artifact; missing=%t", missing)
		}
	}
}

func TestReleaseBuildMetadataDriftCannotBecomeExactBytes(t *testing.T) {
	manifest := buildFixtureManifest()
	manifest.Contracts[0].RebuiltRuntimeSha256 = buildFixtureDigest("changed-metadata")
	if err := sealBuildManifest(&manifest); err == nil {
		t.Fatal("accepted a stale per-contract exactness claim")
	}
	manifest.Contracts[0].ExactBytes = false
	if err := sealBuildManifest(&manifest); err == nil {
		t.Fatal("accepted a stale aggregate exactness claim")
	}
	manifest.SourceToBytecodeExact = false
	if err := sealBuildManifest(&manifest); err != nil {
		t.Fatalf("honest retained/fresh distinction rejected: %v", err)
	}
}

func TestReleaseBuildRejectsMissingImageContextAndBinarySubstitution(t *testing.T) {
	manifest := buildFixtureManifest()
	manifest.Images = manifest.Images[:7]
	if err := sealBuildManifest(&manifest); err == nil {
		t.Fatal("accepted seven of eight image contexts")
	}
	manifest = buildFixtureManifest()
	manifest.Images[0].BinarySha256 = buildFixtureDigest("unselected-image-binary")
	if err := sealBuildManifest(&manifest); err == nil {
		t.Fatal("accepted an image bound to a different binary")
	}
}

func TestReleaseBuildContextsCannotClaimImageOrDeploymentApproval(t *testing.T) {
	for _, claim := range []string{"image", "digest", "release", "deployment", "reproducibility", "aggregate-image"} {
		manifest := buildFixtureManifest()
		switch claim {
		case "image":
			manifest.Images[0].SourceToImageVerified = true
		case "digest":
			manifest.Images[0].PlatformDigest = buildFixtureDigest("unverified-image")
		case "release":
			manifest.ReleaseComplete = true
		case "deployment":
			manifest.DeploymentApproved = true
		case "reproducibility":
			manifest.ReproducibilityVerified = true
		case "aggregate-image":
			manifest.SourceToImageVerified = true
		}
		if err := sealBuildManifest(&manifest); err == nil {
			t.Fatalf("composition accepted unsupported %s claim", claim)
		}
	}
}

func TestReleaseBuildEnvironmentDisablesAmbientWorkspaceAndCompilerOverrides(t *testing.T) {
	for key, value := range map[string]string{"GOWORK": "/synthetic/go.work", "GOFLAGS": "-modfile=/synthetic/override.mod", "GOENV": "/synthetic/go.env", "GOROOT": "/synthetic/compiler", "GOTOOLCHAIN": "auto", "GOPROXY": "https://modules.example", "FOUNDRY_PROFILE": "unreviewed", "FOUNDRY_OPTIMIZER_RUNS": "1", "DAPP_SOLC": "/synthetic/solc", "LD_PRELOAD": "/synthetic/inject.so"} {
		t.Setenv(key, value)
	}
	environment := buildEnvironment(t.TempDir())
	values := map[string]string{}
	for _, item := range environment {
		key, value, _ := strings.Cut(item, "=")
		if _, exists := values[key]; exists {
			t.Fatalf("duplicate environment override: %s", key)
		}
		values[key] = value
	}
	for key, wanted := range map[string]string{"GOWORK": "off", "GOFLAGS": "", "GOENV": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off", "CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "amd64"} {
		if values[key] != wanted {
			t.Fatalf("ambient override survived: %s=%q", key, values[key])
		}
	}
	for _, key := range []string{"GOROOT", "FOUNDRY_PROFILE", "FOUNDRY_OPTIMIZER_RUNS", "DAPP_SOLC", "LD_PRELOAD"} {
		if _, exists := values[key]; exists {
			t.Fatalf("ambient %s survived", key)
		}
	}
}

func TestReleaseBuildRejectsLocalApiModuleOrChecksumSubstitution(t *testing.T) {
	for _, path := range []string{"github.com/urnetwork/sdk/v2026", "github.com/urnetwork/connect/v2026", "github.com/pion/sctp"} {
		for _, replacement := range []*goModule{nil, {Path: "../synthetic-sibling"}, {Path: path, Version: "v0.0.0-synthetic", Sum: "h1:synthetic", GoModSum: "h1:synthetic"}} {
			if err := validateReleaseModule(goModule{Path: path, Replace: replacement}); err == nil {
				t.Fatalf("accepted unreviewed API module %s: %+v", path, replacement)
			}
		}
	}
}

func TestReleaseBuildRetainsBinaryBytesInEveryImageContext(t *testing.T) {
	root := t.TempDir()
	config := buildConfig{Workspace: filepath.Join(root, "source"), Output: filepath.Join(root, "output"), SourceDateEpoch: 1}
	binaries := map[string]buildArtifact{}
	if err := os.MkdirAll(filepath.Join(config.Output, "binaries"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, role := range releaseRoles() {
		if !role.Image {
			continue
		}
		name := strings.TrimPrefix(role.Id, "server-")
		directory := filepath.Join(config.Workspace, "server/cli", name)
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "Dockerfile"), []byte("FROM scratch\n# synthetic "+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		relative := filepath.Join("binaries", role.Id)
		path := filepath.Join(config.Output, relative)
		if err := os.WriteFile(path, []byte("synthetic executable bytes "+role.Id), 0755); err != nil {
			t.Fatal(err)
		}
		artifact, err := buildFile(path)
		if err != nil {
			t.Fatal(err)
		}
		artifact.Id, artifact.Kind, artifact.Path = role.Id, "binary", relative
		binaries[role.Id] = artifact
	}
	images, artifacts, err := prepareBuildImages(config, binaries)
	if err != nil {
		t.Fatal(err)
	}
	byId := map[string]buildArtifact{}
	for id, artifact := range binaries {
		byId[id] = artifact
	}
	for _, artifact := range artifacts {
		byId[artifact.Id] = artifact
	}
	missing := []string{}
	for _, image := range images {
		missing = append(missing, image.Id)
		command := strings.Join(image.BuildArguments, " ")
		if !strings.Contains(command, "rewrite-timestamp=true") || strings.Contains(command, "--push") || strings.Contains(command, "--load") {
			t.Fatalf("image command widens authority or omits epoch: %v", image.BuildArguments)
		}
	}
	if err := validateImageCensus(images, missing, byId); err != nil {
		t.Fatal(err)
	}
	for _, image := range images {
		if image.BinarySha256 != binaries[image.Id].Sha256 {
			t.Fatalf("context copied other bytes: %s", image.Id)
		}
	}
}

func TestReleaseBuildRejectsBinaryFromWrongSourceOrPlatform(t *testing.T) {
	role := releaseRoles()[0]
	repo := repositoryPin{Commit: strings.Repeat("a", 40)}
	info := &debug.BuildInfo{Path: "github.com/urfoundation/sn/mainnet", Main: debug.Module{Path: "github.com/urfoundation/sn/v2026"}, Settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "amd64"}, {Key: "CGO_ENABLED", Value: "0"}, {Key: "-trimpath", Value: "true"}, {Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: repo.Commit}, {Key: "vcs.modified", Value: "false"}}}
	if err := validateBuildInfo(info, role, repo); err != nil {
		t.Fatal(err)
	}
	for index, setting := range info.Settings {
		prior := setting.Value
		info.Settings[index].Value = "synthetic-mismatch"
		if err := validateBuildInfo(info, role, repo); err == nil {
			t.Fatalf("accepted wrong binary %s", setting.Key)
		}
		info.Settings[index].Value = prior
	}
}

func TestReleaseBuildRejectsDuplicateJsonAtEveryNestingLevel(t *testing.T) {
	for _, raw := range []string{`{"workspace":"one","workspace":"two"}`, `{"go":{"path":"one","path":"two"}}`, `{"repositories":[{"commit":"one","commit":"two"}]}`, `{} {}`} {
		if err := validateBuildJson([]byte(raw)); err == nil {
			t.Fatalf("accepted ambiguous build config: %s", raw)
		}
	}
	if err := validateBuildJson([]byte(`{"go":{"path":"synthetic"},"repositories":[]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseBuildBoundsCompilerOutputBeforeAllocation(t *testing.T) {
	buffer := &buildBuffer{limit: 8}
	if _, err := buffer.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte("9")); err == nil {
		t.Fatal("metadata bound was applied after accepting excess output")
	}
	if buffer.Len() != 8 {
		t.Fatal("excess bytes were retained")
	}
}

func TestReleaseBuildRejectsSymlinkAndOversizedInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "input")
	if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBuildInput(path, 3); err == nil {
		t.Fatal("accepted oversized input")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readBuildInput(link, 64); err == nil {
		t.Fatal("accepted linked source input")
	}
	if _, err := buildFile(link); err == nil {
		t.Fatal("hashed a linked artifact as regular")
	}
}

func TestReleaseBuildContractBytecodeHonorsDeploymentSizeAndLinkBounds(t *testing.T) {
	for _, maximum := range []int{24576, 49152} {
		value := hex.EncodeToString(bytes.Repeat([]byte{0x60}, maximum))
		if _, size, err := hashBuildBytecode(value, maximum); err != nil || size != maximum {
			t.Fatalf("rejected exact code bound: %d %v", size, err)
		}
		if _, _, err := hashBuildBytecode(value+"00", maximum); err == nil {
			t.Fatal("accepted bytecode beyond EVM deployment bound")
		}
	}
	for _, value := range []string{"", "0x", "0", "__$synthetic_link$__"} {
		if _, _, err := hashBuildBytecode(value, 24576); err == nil {
			t.Fatalf("accepted incomplete bytecode %q", value)
		}
	}
}

func TestReleaseBuildManifestSealIsStableWithoutApproval(t *testing.T) {
	left, right := buildFixtureManifest(), buildFixtureManifest()
	for i, j := 0, len(right.Artifacts)-1; i < j; i, j = i+1, j-1 {
		right.Artifacts[i], right.Artifacts[j] = right.Artifacts[j], right.Artifacts[i]
	}
	if err := sealBuildManifest(&left); err != nil {
		t.Fatal(err)
	}
	if err := sealBuildManifest(&right); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	if !bytes.Equal(a, b) {
		t.Fatal("artifact enumeration order changes manifest seal")
	}
}

func TestReleaseBuildExportCannotSilentlyReplaceRetainedTransactionBytes(t *testing.T) {
	contract := releaseContract{Name: "Synthetic", Abi: "[]", Creation: "6000", Runtime: "6001", RuntimeHash: "runtime-hash", ArtifactHash: "artifact-hash", StorageLayoutHash: "storage-hash"}
	constants, err := retainedContractConstants([]byte("package fixture\nconst SyntheticABI = `[]`\nconst SyntheticCreationBytecode = \"6000\"\nconst SyntheticRuntimeBytecode = \"6001\"\nconst SyntheticRuntimeBytecodeHash = \"runtime-hash\"\nconst SyntheticFoundryArtifactHash = \"artifact-hash\"\nconst SyntheticStorageLayoutHash = \"storage-hash\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRetainedContract(contract, constants); err != nil {
		t.Fatal(err)
	}
	contract.Creation = "6002"
	if err := validateRetainedContract(contract, constants); err == nil {
		t.Fatal("fresh export silently replaced retained creation bytes")
	}
	contract.Creation = "6000"
	contract.Runtime = "6003"
	if err := validateRetainedContract(contract, constants); err == nil {
		t.Fatal("fresh export silently replaced retained runtime bytes")
	}
}

func TestReleaseBuildRejectsDuplicateArtifactsAndEscapingPaths(t *testing.T) {
	manifest := buildFixtureManifest()
	manifest.Artifacts = append(manifest.Artifacts, manifest.Artifacts[0])
	if err := sealBuildManifest(&manifest); err == nil {
		t.Fatal("accepted a duplicate artifact")
	}
	for _, path := range []string{"../escape", "/absolute", "contexts/../binary"} {
		manifest = buildFixtureManifest()
		manifest.Artifacts[0].Path = path
		if err := sealBuildManifest(&manifest); err == nil {
			t.Fatalf("accepted non-canonical artifact path %s", path)
		}
	}
}

// Lazy tool-only graph nodes remain visible without becoming linked provenance.
func TestReleaseBuildRetainsLazyAndPartiallyMaterializedGraphNodes(t *testing.T) {
	for _, module := range []goModule{{Path: "modules.example/lazy", Version: "v1.0.0"}, {Path: "modules.example/cached-body", Version: "v1.0.0", Dir: "/synthetic/cache", Sum: "h1:synthetic"}, {Path: "modules.example/metadata-only", Version: "v1.0.0", GoMod: "/synthetic/go.mod", GoModSum: "h1:synthetic"}} {
		if len(moduleMissingProvenance(module)) == 0 {
			t.Fatalf("incomplete graph node claimed provenance: %+v", module)
		}
	}
	complete := goModule{Path: "modules.example/linked", Version: "v1.0.0", Dir: "/synthetic/cache", GoMod: "/synthetic/go.mod", Sum: "h1:body", GoModSum: "h1:mod"}
	if missing := moduleMissingProvenance(complete); len(missing) != 0 {
		t.Fatalf("complete linked metadata rejected: %v", missing)
	}
	counts := countBuildModuleQualification([]buildModule{{Path: "modules.example/lazy", GraphOnly: true}, {Path: "modules.example/cached-body", GraphOnly: true, Sum: "h1:body"}, {Path: "modules.example/metadata-only", GraphOnly: true, GoModSha256: buildFixtureDigest("go.mod"), GoModSum: "h1:mod"}, {Path: "modules.example/linked"}})
	if counts.GraphNodes != 4 || counts.UnqualifiedGraphNodes != 3 || counts.MissingGoModMetadata != 2 || counts.GoModOnly != 1 {
		t.Fatalf("graph-only missing-field counts differ: %+v", counts)
	}
}

func buildFixtureLinkedModule() (buildModule, *debug.Module) {
	module := buildModule{Path: "modules.example/linked", Version: "v1.2.3", EffectivePath: "modules.example/linked", EffectiveVersion: "v1.2.3", Sum: "h1:synthetic-body", GoModSum: "h1:synthetic-module", Directory: "/synthetic/source", GoModSha256: buildFixtureDigest("synthetic go.mod")}
	dependency := &debug.Module{Path: module.Path, Version: module.Version, Sum: module.Sum}
	return module, dependency
}

func TestReleaseBuildLazyGraphNodeCannotBecomeLinkedEvidence(t *testing.T) {
	module, dependency := buildFixtureLinkedModule()
	lazy := buildModule{Path: "modules.example/lazy", Version: "v1.0.0", EffectivePath: "modules.example/lazy", EffectiveVersion: "v1.0.0", GraphOnly: true}
	info := &debug.BuildInfo{Deps: []*debug.Module{dependency}}
	modules := []buildModule{module, lazy}
	if err := validateLinkedBuildModules(info, modules); err != nil {
		t.Fatalf("unused lazy graph node rejected a complete binary: %v", err)
	}
	info.Deps = append(info.Deps, &debug.Module{Path: lazy.Path, Version: lazy.Version})
	if err := validateLinkedBuildModules(info, modules); err == nil {
		t.Fatal("lazy graph identity was promoted to linked-module provenance")
	}
}

func TestReleaseBuildLinkedRemoteModulesRequireBodyAndGoModAuthentication(t *testing.T) {
	for _, field := range []string{"body-sum", "go-mod-sum", "go-mod-file", "directory", "lazy", "version", "wrong-body"} {
		module, dependency := buildFixtureLinkedModule()
		switch field {
		case "body-sum":
			module.Sum = ""
		case "go-mod-sum":
			module.GoModSum = ""
		case "go-mod-file":
			module.GoModSha256 = ""
		case "directory":
			module.Directory = ""
		case "lazy":
			module.GraphOnly = true
		case "version":
			dependency.Version = "(devel)"
		case "wrong-body":
			dependency.Sum = "h1:different-body"
		}
		if err := validateLinkedBuildModules(&debug.BuildInfo{Deps: []*debug.Module{dependency}}, []buildModule{module}); err == nil {
			t.Fatalf("linked remote %s absence/change was accepted", field)
		}
	}
	if err := validateLinkedBuildModules(nil, nil); err == nil {
		t.Fatal("accepted unreadable build info")
	}
	if err := validateBuildInfo(nil, buildRole{}, repositoryPin{}); err == nil {
		t.Fatal("binary source validation accepted unreadable build info")
	}
}

func TestReleaseBuildLocalDevelVersionRequiresPinnedRepository(t *testing.T) {
	module := buildModule{Path: "modules.example/local", Version: "v0.0.0", EffectivePath: "../synthetic-local", Directory: "/synthetic/local", GoModSha256: buildFixtureDigest("local module"), Repository: "synthetic-local", Commit: strings.Repeat("a", 40)}
	dependency := &debug.Module{Path: module.Path, Version: module.Version, Replace: &debug.Module{Path: module.EffectivePath, Version: "(devel)"}}
	info := &debug.BuildInfo{Deps: []*debug.Module{dependency}}
	if err := validateLinkedBuildModules(info, []buildModule{module}); err != nil {
		t.Fatalf("pinned local '(devel)' version was not normalized: %v", err)
	}
	module.Repository = ""
	if err := validateLinkedBuildModules(info, []buildModule{module}); err == nil {
		t.Fatal("unowned local '(devel)' source was accepted")
	}
	module.Repository = "synthetic-local"
	module.Commit = ""
	if err := validateLinkedBuildModules(info, []buildModule{module}); err == nil {
		t.Fatal("unpinned local '(devel)' source was accepted")
	}
}
