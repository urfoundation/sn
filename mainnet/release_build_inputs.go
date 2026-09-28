// Compiler admission uses the same controlled environment for module listing,
// package listing and builds. Actual source files are hashed before and after.
package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"
)

const maximumReleaseBuildMetadataBytes = 32 * 1024 * 1024

// This is the subset of go list metadata used for physical resolution. Extra
// compiler metadata is deliberately not interpreted as an approval assertion.
type releaseBuildModule struct {
	Path    string              `json:"Path"`
	Version string              `json:"Version,omitempty"`
	Main    bool                `json:"Main,omitempty"`
	Dir     string              `json:"Dir,omitempty"`
	GoMod   string              `json:"GoMod,omitempty"`
	Sum     string              `json:"Sum,omitempty"`
	Replace *releaseBuildModule `json:"Replace,omitempty"`
}

// All compiler-selected source forms are considered, including assembler,
// prebuilt objects and go:embed files that a Go-only manifest would omit.
type releaseBuildPackage struct {
	ImportPath   string
	Name         string
	Dir          string
	Standard     bool
	Module       *releaseBuildModule
	GoFiles      []string
	CgoFiles     []string
	CFiles       []string
	CXXFiles     []string
	HFiles       []string
	FFiles       []string
	SFiles       []string
	SwigFiles    []string
	SwigCXXFiles []string
	SysoFiles    []string
	EmbedFiles   []string
	Error        *struct{ Err string }
}

// A bounded file hash records actual compiler bytes, not just a declared Git
// revision. Modes and paths remain visible to the final fence.
type releaseBuildFile struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Mode   uint32 `json:"mode"`
}

// Each package is tied to its resolved physical module and selected file set.
type releaseBuildPackageInputs struct {
	ImportPath string             `json:"import_path"`
	Module     string             `json:"module,omitempty"`
	Directory  string             `json:"directory"`
	Files      []releaseBuildFile `json:"files"`
}

// The module graph is captured even when some modules are not linked into a
// selected executable. Build info separately records the linked module subset.
type releaseBuildInputs struct {
	Role        string                      `json:"role"`
	Directory   string                      `json:"directory"`
	ImportPath  string                      `json:"import_path"`
	Modules     []releaseBuildModule        `json:"modules"`
	ModuleFiles []releaseBuildFile          `json:"module_files"`
	Packages    []releaseBuildPackageInputs `json:"packages"`
}

// Tool binaries and their physical roots are retained; full source inputs for
// the selected standard-library packages are captured alongside other packages.
type releaseBuildToolchain struct {
	GoVersion    string             `json:"go_version"`
	GoRoot       string             `json:"go_root"`
	GoModCache   string             `json:"go_mod_cache"`
	GoCache      string             `json:"go_cache"`
	GoExecutable string             `json:"go_executable"`
	Builder      releaseBuildFile   `json:"builder"`
	Files        []releaseBuildFile `json:"files"`
}

// Parsed Go build metadata is retained without executing the compiled program.
type releaseBuildInfo struct {
	GoVersion    string               `json:"go_version"`
	Package      string               `json:"package"`
	Main         debug.Module         `json:"main"`
	Dependencies []*debug.Module      `json:"dependencies"`
	Settings     []debug.BuildSetting `json:"settings"`
}

// One builder owns a fixed environment and output tree. It is used serially.
type releaseBuilder struct {
	config      releaseBuildConfig
	lock        sourceLock
	goPath      string
	environment []string
	toolchain   releaseBuildToolchain
	moduleDirs  map[string]string
}

// Output memory is bounded even when a compiler or VCS command fails noisily.
// Cancellation kills the process group before Wait joins pipe readers.
type releaseBuildOutput struct {
	bytes.Buffer
	cancel context.CancelFunc
}

// All writers supplied to exec are the same value, so exec serializes writes.
func (self *releaseBuildOutput) Write(raw []byte) (int, error) {
	if self.Len()+len(raw) > maximumReleaseBuildMetadataBytes {
		self.cancel()
		return 0, errors.New("release build command output exceeds 32 MiB")
	}
	return self.Buffer.Write(raw)
}

// Every subprocess has an owned group, a hard deadline and bounded output.
// The group kill handles compiler children as well as the immediate go process.
func releaseBuildCommand(ctx context.Context, dir string, environment []string, timeout time.Duration, executable string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, executable, args...)
	command.Dir, command.Env = dir, environment
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	output := &releaseBuildOutput{cancel: cancel}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	if err != nil {
		detail := output.String()
		if len(detail) > 4096 {
			detail = detail[:4096]
		}
		return nil, fmt.Errorf("%s %v: %w: %s", filepath.Base(executable), args, errors.Join(err, commandCtx.Err()), detail)
	}
	return output.Bytes(), nil
}

// Compiler-selected files must resolve directly to physical regular files. The
// shared inventory reader rechecks descriptor identity and honors cancellation.
func releaseBuildCaptureFile(ctx context.Context, path string) (releaseBuildFile, error) {
	physical, err := filepath.EvalSymlinks(path)
	if err != nil || physical != filepath.Clean(path) || !filepath.IsAbs(path) {
		return releaseBuildFile{}, errors.Join(errors.New("release build input has a symlink or nonphysical path"), err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return releaseBuildFile{}, errors.Join(errors.New("release build input is not a regular file"), err)
	}
	// Empty module sums and assembly marker files are valid compiler inputs.
	if info.Size() == 0 {
		return releaseBuildFile{Path: path, Sha256: releaseBuildDigest(nil), Mode: uint32(info.Mode().Perm())}, ctx.Err()
	}
	artifact, err := captureReleaseArtifact(ctx, releaseArtifactInput{Id: "compiler-input", Category: "dependency", Path: path}, "/", maximumReleaseInventoryArtifactBytes)
	if err != nil {
		return releaseBuildFile{}, err
	}
	return releaseBuildFile{Path: path, Sha256: artifact.Sha256, Bytes: artifact.Bytes, Mode: artifact.Mode}, nil
}

// Only cache locations and basic process lookup are inherited. Environment
// build flags, workspaces, toolchain switching, cgo and network downloads are off.
func newReleaseBuilder(ctx context.Context, config releaseBuildConfig, lock sourceLock) (*releaseBuilder, error) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return nil, err
	}
	goPath, err = filepath.EvalSymlinks(goPath)
	if err != nil || !filepath.IsAbs(goPath) {
		return nil, errors.Join(errors.New("go executable is not absolute"), err)
	}
	environment := []string{}
	for _, key := range []string{"PATH", "HOME", "GOCACHE", "GOMODCACHE"} {
		if value, ok := os.LookupEnv(key); ok {
			environment = append(environment, key+"="+value)
		}
	}
	environment = append(environment, "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOSUMDB=off", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOAUTH=off",
		"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOAMD64=v1", "GOMAXPROCS=2", "GOTMPDIR="+filepath.Join(config.OutputDir, "tmp"), "TMPDIR="+filepath.Join(config.OutputDir, "tmp"))
	self := &releaseBuilder{config: config, lock: lock, goPath: goPath, environment: environment, moduleDirs: map[string]string{lock.Module: config.SourceSnDir}}
	for _, replacement := range lock.LocalReplacements {
		path := replacement.LocalPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(config.SourceSnDir, path)
		}
		physical, err := filepath.EvalSymlinks(path)
		if err != nil || physical != filepath.Clean(path) {
			return nil, errors.Join(errors.New("local module replacement is not physical"), err)
		}
		self.moduleDirs[replacement.Module] = physical
	}
	raw, err := self.command(ctx, releaseBuildRole{Module: lock.Module}, time.Minute, "env", "-json", "GOVERSION", "GOROOT", "GOTOOLDIR", "GOMODCACHE", "GOCACHE", "GOPATH")
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	if values["GOVERSION"] == "" || !filepath.IsAbs(values["GOROOT"]) || !filepath.IsAbs(values["GOTOOLDIR"]) || !filepath.IsAbs(values["GOMODCACHE"]) || !filepath.IsAbs(values["GOCACHE"]) {
		return nil, errors.New("release build toolchain/cache environment is incomplete")
	}
	self.toolchain = releaseBuildToolchain{GoVersion: values["GOVERSION"], GoRoot: values["GOROOT"], GoModCache: values["GOMODCACHE"], GoCache: values["GOCACHE"], GoExecutable: goPath, Files: []releaseBuildFile{}}
	builderPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	builderPath, err = filepath.EvalSymlinks(builderPath)
	if err != nil {
		return nil, err
	}
	self.toolchain.Builder, err = releaseBuildCaptureFile(ctx, builderPath)
	if err != nil {
		return nil, err
	}
	paths := []string{goPath}
	for _, name := range []string{"compile", "link", "asm", "pack"} {
		paths = append(paths, filepath.Join(values["GOTOOLDIR"], name))
	}
	for _, path := range paths {
		file, err := releaseBuildCaptureFile(ctx, path)
		if err != nil {
			return nil, err
		}
		self.toolchain.Files = append(self.toolchain.Files, file)
	}
	return self, nil
}

// Fixed per-role experiments are applied to metadata and builds alike.
func (self *releaseBuilder) command(ctx context.Context, role releaseBuildRole, timeout time.Duration, args ...string) ([]byte, error) {
	dir, ok := self.moduleDirs[role.Module]
	if !ok {
		return nil, errors.New("selected role module is absent from the source lock")
	}
	environment := append(append([]string{}, self.environment...), "GOEXPERIMENT="+role.GoExperiment)
	return releaseBuildCommand(ctx, dir, environment, timeout, self.goPath, args...)
}

// Go's output path does not enter the executable, but the exact command profile
// remains explicit in the manifest. Build concurrency never exceeds two.
func (self *releaseBuilder) arguments(role releaseBuildRole) []string {
	args := []string{"build", "-p=2", "-trimpath", "-buildvcs=true", "-buildmode=exe", "-compiler=gc"}
	if role.Ldflags != "" {
		args = append(args, "-ldflags", role.Ldflags)
	}
	return args
}

// Every local module actually resolved by the compiler must match the physical
// path declared by the SN source lock, including server's reverse SN replacement.
func (self *releaseBuilder) admitModule(module releaseBuildModule) error {
	if module.Path == "" {
		return errors.New("compiler module has no path")
	}
	local := module.Main || module.Replace != nil && module.Replace.Version == ""
	if !local {
		return nil
	}
	dir := module.Dir
	if module.Replace != nil {
		dir = module.Replace.Dir
	}
	expected, ok := self.moduleDirs[module.Path]
	physical, err := filepath.EvalSymlinks(dir)
	if !ok || err != nil || physical != expected || dir != physical {
		return errors.Join(fmt.Errorf("compiler local module %s resolves outside its locked physical path", module.Path), err)
	}
	return nil
}

// Git's tracked set prevents ignored generated Go or embed files from silently
// borrowing a clean repository identity. Source bytes receive a separate seal.
func (self *releaseBuilder) trackedFiles(ctx context.Context) (map[string]bool, error) {
	trackedKVs := map[string]bool{}
	for _, repository := range self.lock.Repositories {
		dir := filepath.Clean(filepath.Join(self.config.SourceSnDir, repository.Path))
		raw, err := releaseBuildCommand(ctx, dir, os.Environ(), time.Minute, "git", "ls-files", "-z")
		if err != nil {
			return nil, err
		}
		for _, path := range bytes.Split(raw, []byte{0}) {
			if len(path) != 0 {
				trackedKVs[filepath.Join(dir, string(path))] = true
			}
		}
	}
	return trackedKVs, nil
}

// One complete compiler view captures the actual module graph and selected
// package bytes. Network access stays disabled and missing cache inputs refuse.
func (self *releaseBuilder) capture(ctx context.Context, role releaseBuildRole) (releaseBuildInputs, error) {
	result := releaseBuildInputs{Role: role.Id, Directory: self.moduleDirs[role.Module], ImportPath: role.Module + strings.TrimPrefix(role.Package, "."), Modules: []releaseBuildModule{}, ModuleFiles: []releaseBuildFile{}, Packages: []releaseBuildPackageInputs{}}
	if err := verifyReleaseInventorySources(ctx, self.config.SourceSnDir, self.lock); err != nil {
		return result, err
	}
	if err := self.verifyToolchain(ctx); err != nil {
		return result, err
	}
	trackedKVs, err := self.trackedFiles(ctx)
	if err != nil {
		return result, err
	}
	raw, err := self.command(ctx, role, 2*time.Minute, "list", "-m", "-json", "all")
	if err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	moduleKVs := map[string]releaseBuildModule{}
	moduleFileKVs := map[string]bool{}
	for {
		var module releaseBuildModule
		err := decoder.Decode(&module)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
		if _, exists := moduleKVs[module.Path]; exists || len(moduleKVs) >= 8192 {
			return result, errors.New("compiler module graph is duplicated or oversized")
		}
		if err := self.admitModule(module); err != nil {
			return result, err
		}
		if module.Main && (module.Path != role.Module || module.Dir != result.Directory) {
			return result, errors.New("compiler main module differs from selected role")
		}
		moduleKVs[module.Path] = module
		result.Modules = append(result.Modules, module)
		goMod := module.GoMod
		if module.Replace != nil {
			goMod = module.Replace.GoMod
		}
		if goMod != "" {
			moduleFileKVs[goMod] = true
		}
		if module.Main || module.Replace != nil && module.Replace.Version == "" {
			if !trackedKVs[goMod] {
				return result, errors.New("compiler local go.mod is not tracked by the locked source graph")
			}
			sum := filepath.Join(filepath.Dir(goMod), "go.sum")
			if _, err := os.Lstat(sum); err == nil {
				if !trackedKVs[sum] {
					return result, errors.New("compiler local go.sum is not tracked by the locked source graph")
				}
				moduleFileKVs[sum] = true
			} else if !errors.Is(err, os.ErrNotExist) {
				return result, err
			}
		}
	}
	if module, ok := moduleKVs[role.Module]; !ok || !module.Main {
		return result, errors.New("compiler graph omits the selected main module")
	}
	for path := range moduleFileKVs {
		file, err := releaseBuildCaptureFile(ctx, path)
		if err != nil {
			return result, err
		}
		result.ModuleFiles = append(result.ModuleFiles, file)
	}
	sort.Slice(result.ModuleFiles, func(i, j int) bool { return result.ModuleFiles[i].Path < result.ModuleFiles[j].Path })
	sort.Slice(result.Modules, func(i, j int) bool { return result.Modules[i].Path < result.Modules[j].Path })
	raw, err = self.command(ctx, role, 3*time.Minute, "list", "-deps", "-json", role.Package)
	if err != nil {
		return result, err
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	packageKVs := map[string]bool{}
	var totalBytes int64
	totalFiles := 0
	for {
		var pkg releaseBuildPackage
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
		if pkg.Error != nil || pkg.ImportPath == "" || packageKVs[pkg.ImportPath] || len(packageKVs) >= 8192 {
			return result, errors.New("compiler package closure is invalid, duplicated or oversized")
		}
		packageKVs[pkg.ImportPath] = true
		if pkg.ImportPath == result.ImportPath && pkg.Name != "main" {
			return result, errors.New("selected role is not an executable package")
		}
		input := releaseBuildPackageInputs{ImportPath: pkg.ImportPath, Directory: pkg.Dir, Files: []releaseBuildFile{}}
		local := false
		if pkg.Module != nil {
			if err := self.admitModule(*pkg.Module); err != nil {
				return result, err
			}
			module, ok := moduleKVs[pkg.Module.Path]
			if !ok {
				return result, errors.New("compiler package module is absent from the resolved graph")
			}
			dir := module.Dir
			if module.Replace != nil {
				dir = module.Replace.Dir
			}
			if dir == "" || !releaseBuildWithin(dir, pkg.Dir) || pkg.Module.Version != module.Version {
				return result, errors.New("compiler package resolves outside its admitted module")
			}
			input.Module = pkg.Module.Path
			local = pkg.Module.Main || pkg.Module.Replace != nil && pkg.Module.Replace.Version == ""
		} else if !pkg.Standard || !releaseBuildWithin(filepath.Join(self.toolchain.GoRoot, "src"), pkg.Dir) {
			return result, errors.New("compiler package is outside the standard library or has no resolved module")
		}
		fileKVs := map[string]bool{}
		for _, names := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.CFiles, pkg.CXXFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.SwigCXXFiles, pkg.SysoFiles, pkg.EmbedFiles} {
			for _, name := range names {
				fileKVs[filepath.Join(pkg.Dir, name)] = true
			}
		}
		for path := range fileKVs {
			if !releaseBuildWithin(pkg.Dir, path) || local && !trackedKVs[path] {
				return result, fmt.Errorf("compiler input %s is outside its package or untracked in the locked source graph", path)
			}
			file, err := releaseBuildCaptureFile(ctx, path)
			if err != nil {
				return result, err
			}
			totalBytes += file.Bytes
			totalFiles++
			if totalBytes > 2*1024*1024*1024 || totalFiles > 65536 {
				return result, errors.New("compiler input closure exceeds resource bounds")
			}
			input.Files = append(input.Files, file)
		}
		sort.Slice(input.Files, func(i, j int) bool { return input.Files[i].Path < input.Files[j].Path })
		result.Packages = append(result.Packages, input)
	}
	if !packageKVs[result.ImportPath] {
		return result, errors.New("compiler closure omits the selected role package")
	}
	sort.Slice(result.Packages, func(i, j int) bool { return result.Packages[i].ImportPath < result.Packages[j].ImportPath })
	return result, nil
}

// The exact tools that were admitted must remain present with unchanged bytes.
func (self *releaseBuilder) verifyToolchain(ctx context.Context) error {
	files := append(append([]releaseBuildFile{}, self.toolchain.Files...), self.toolchain.Builder)
	for _, expected := range files {
		current, err := releaseBuildCaptureFile(ctx, expected.Path)
		if err != nil || current != expected {
			return errors.Join(errors.New("release build toolchain bytes changed"), err)
		}
	}
	return nil
}

// Reading build info verifies role, target, compiler and Git identity in the
// actual output rather than trusting its filename or a declared source hash.
func (self *releaseBuilder) artifact(ctx context.Context, role releaseBuildRole, path string, inputs releaseBuildInputs) (releaseArtifact, releaseBuildInfo, error) {
	artifact, err := captureReleaseArtifact(ctx, releaseArtifactInput{Id: role.Id, Category: "executable", Path: path, SourceLockContentHash: self.lock.ContentHash}, "/", maximumReleaseInventoryArtifactBytes)
	if err != nil {
		return artifact, releaseBuildInfo{}, err
	}
	if err := releaseBuildStaticElf(path); err != nil {
		return artifact, releaseBuildInfo{}, err
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return artifact, releaseBuildInfo{}, err
	}
	retained := releaseBuildInfo{GoVersion: info.GoVersion, Package: info.Path, Main: info.Main, Dependencies: info.Deps, Settings: info.Settings}
	settingsKVs := map[string]string{}
	for _, setting := range info.Settings {
		if _, ok := settingsKVs[setting.Key]; ok {
			return artifact, retained, errors.New("executable build settings are duplicated")
		}
		settingsKVs[setting.Key] = setting.Value
	}
	repository, err := sourceLockGitRepository(ctx, self.config.SourceSnDir, self.moduleDirs[role.Module])
	if err != nil {
		return artifact, retained, err
	}
	if info.Path != inputs.ImportPath || info.Main.Path != role.Module || info.GoVersion != self.toolchain.GoVersion ||
		settingsKVs["GOOS"] != "linux" || settingsKVs["GOARCH"] != "amd64" || settingsKVs["GOAMD64"] != "v1" || settingsKVs["CGO_ENABLED"] != "0" ||
		settingsKVs["-trimpath"] != "true" || settingsKVs["-buildmode"] != "exe" || settingsKVs["-compiler"] != "gc" || settingsKVs["GOEXPERIMENT"] != role.GoExperiment ||
		settingsKVs["vcs"] != "git" || settingsKVs["vcs.revision"] != repository.Commit || settingsKVs["vcs.modified"] != "false" {
		return artifact, retained, errors.New("executable role, target, build profile or source revision differs")
	}
	moduleKVs := map[string]releaseBuildModule{}
	for _, module := range inputs.Modules {
		moduleKVs[module.Path] = module
	}
	for _, dependency := range info.Deps {
		module, ok := moduleKVs[dependency.Path]
		if !ok || dependency.Version != module.Version || dependency.Sum != module.Sum || (dependency.Replace == nil) != (module.Replace == nil) {
			return artifact, retained, errors.New("executable linked module differs from the compiler graph")
		}
		if dependency.Replace != nil && (dependency.Replace.Path != module.Replace.Path || dependency.Replace.Version != module.Replace.Version || dependency.Replace.Sum != module.Replace.Sum) {
			return artifact, retained, errors.New("executable replacement differs from the compiler graph")
		}
	}
	if err := releaseBuildVerifyArtifact(ctx, artifact); err != nil {
		return artifact, retained, err
	}
	return artifact, retained, nil
}

// The claimed static Linux/amd64 profile is checked in ELF headers as well as
// Go build metadata. A dynamic loader or different machine is outside v1.
func releaseBuildStaticElf(path string) error {
	file, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Type != elf.ET_EXEC || file.Machine != elf.EM_X86_64 {
		return errors.New("executable is outside the static Linux/amd64 ELF profile")
	}
	for _, program := range file.Progs {
		if program.Type == elf.PT_INTERP || program.Type == elf.PT_DYNAMIC {
			return errors.New("executable requires a dynamic loader outside the static ELF profile")
		}
	}
	return nil
}

// Reconstruct the exact inventory declaration for a retained generated artifact.
func releaseBuildVerifyArtifact(ctx context.Context, artifact releaseArtifact) error {
	input := releaseArtifactInput{Id: artifact.Id, Category: artifact.Category, Path: artifact.Path, SourceLockContentHash: artifact.SourceLockContentHash, ImageReference: artifact.ImageReference}
	return verifyReleaseArtifact(ctx, input, "/", artifact)
}

// The existing inventory consumes first-pass executable files, exact compiler
// input records, the copied lock and tool binaries. It retains its partial scope.
func (self *releaseBuilder) writeInventory(ctx context.Context, manifest releaseBuildManifest) (planFileReference, error) {
	config := releaseInventoryConfig{Schema: releaseInventoryConfigSchema, CandidateId: manifest.CandidateId, SourceSnDir: manifest.SourceSnDir, SourceLock: manifest.SourceLock, Artifacts: []releaseArtifactInput{}}
	for _, result := range manifest.Roles {
		config.Artifacts = append(config.Artifacts, releaseArtifactInput{Id: result.Role.Id, Category: "executable", Path: result.First.Path, SourceLockContentHash: manifest.SourceLockContentHash, ExpectedSha256: result.First.Sha256},
			releaseArtifactInput{Id: result.Role.Id + "-compiler-inputs", Category: "dependency", Path: result.Inputs.Path, SourceLockContentHash: manifest.SourceLockContentHash, ExpectedSha256: result.Inputs.Sha256})
	}
	config.Artifacts = append(config.Artifacts, releaseArtifactInput{Id: "source-lock", Category: "dependency", Path: manifest.SourceLock.Path, SourceLockContentHash: manifest.SourceLockContentHash, ExpectedSha256: manifest.SourceLock.Sha256})
	for index, file := range manifest.Toolchain.Files {
		config.Artifacts = append(config.Artifacts, releaseArtifactInput{Id: fmt.Sprintf("go-tool-%d-%s", index, filepath.Base(file.Path)), Category: "toolchain", Path: file.Path, SourceLockContentHash: manifest.SourceLockContentHash, ExpectedSha256: file.Sha256})
	}
	path := filepath.Join(self.config.OutputDir, "release-inventory-config.json")
	if _, err := releaseBuildWrite(path, config); err != nil {
		return planFileReference{}, err
	}
	inventory, err := buildReleaseInventory(ctx, path)
	if err != nil {
		return planFileReference{}, err
	}
	return releaseBuildWrite(filepath.Join(self.config.OutputDir, "release-inventory.json"), inventory)
}

// A cached module graph is not enough to detect edits to downloaded package
// directories; go mod verify checks those bytes without enabling downloads.
func (self *releaseBuilder) verifyModuleCache(ctx context.Context, role releaseBuildRole) error {
	_, err := self.command(ctx, role, 3*time.Minute, "mod", "verify")
	return err
}
