package main

// final_semantic_capture.go freezes every local, public input which could be
// lost when supervised services stop. The bundles contain exact source bytes;
// semantic interpretation intentionally happens later, after the terminal log
// scan, from this closed input graph only.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const finalCollectedFileBundleSchema = "urnetwork-final-collected-file-bundle-v1"

const (
	finalCollectedBundleMaximumRawBytes = 24 * 1024 * 1024
	finalCollectedBundleMaximumBytes    = 48 * 1024 * 1024
)

type FinalCollectedFileBundleEntry struct {
	Path        string `json:"path"`
	ContentHash string `json:"sha256"`
	SizeBytes   uint64 `json:"bytes"`
	Data        []byte `json:"data"`
}

type FinalCollectedFileBundle struct {
	Schema string                          `json:"schema"`
	Name   string                          `json:"name"`
	Files  []FinalCollectedFileBundleEntry `json:"files"`
}

func captureFinalSemanticClosedInputs(stateRoot, runRoot string, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation, topologyMiners, topologySwarms, topologyOperators int) ([]FinalArtifactLocator, FinalArtifactLocator, FinalArtifactLocator, FinalArtifactLocator, error) {
	resultData, err := json.Marshal(result)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	resultLocator, err := persistFinalCollectedArtifact(runRoot, "scenario-result-candidate", "final-inputs/scenario-result.json", resultData)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	terminalData, err := json.Marshal(terminal)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	terminalLocator, err := persistFinalCollectedArtifact(runRoot, "scenario-terminal-observation", "final-inputs/terminal-observation.json", terminalData)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	historyData, err := json.Marshal(history)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	historyLocator, err := persistFinalCollectedArtifact(runRoot, "scenario-observation-history", "final-inputs/observation-history.json", historyData)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}

	bundles := make([]FinalArtifactLocator, 0, 8)
	for _, directory := range []struct {
		name string
		path string
	}{
		{name: "public", path: filepath.Join(stateRoot, "public")},
		{name: "receipts", path: filepath.Join(stateRoot, "receipts")},
	} {
		include := func(relative string) bool { return strings.HasSuffix(relative, ".json") }
		if directory.name == "public" {
			include = finalSemanticPublicCapturePath
		}
		entries, err := finalCollectedDirectoryEntries(directory.path, include)
		if err != nil {
			return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, fmt.Errorf("capture %s: %w", directory.name, err)
		}
		locators, err := persistFinalCollectedBundleChunks(runRoot, directory.name, entries)
		if err != nil {
			return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
		}
		bundles = append(bundles, locators...)
	}

	// public.json is the signed deployment root whose canonical hash is bound by
	// the archive-retention receipt. Capture its exact bytes so the offline
	// semantic builder never has to trust a mutable state-directory read.
	foundationNames := finalSemanticLaunchFoundationNames()
	foundation, err := finalCollectedNamedEntries(stateRoot, foundationNames)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	locators, err := persistFinalCollectedBundleChunks(runRoot, "launch-foundation", foundation)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	bundles = append(bundles, locators...)

	plans, err := finalCollectedDirectoryEntries(filepath.Join(stateRoot, "plans"), func(relative string) bool { return strings.HasSuffix(relative, ".json") })
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, fmt.Errorf("capture plan history: %w", err)
	}
	locators, err = persistFinalCollectedBundleChunks(runRoot, "plan-history", plans)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	bundles = append(bundles, locators...)

	topologyNames := make([]string, 0, 2*topologyMiners+topologySwarms+topologyOperators)
	for minerID := 1; minerID <= topologyMiners; minerID++ {
		topologyNames = append(topologyNames, filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("miner-%d", minerID), "miner.yml")))
		topologyNames = append(topologyNames, filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("miner-%d", minerID), "claim-daemon.yml")))
	}
	for swarmID := 1; swarmID <= topologySwarms; swarmID++ {
		topologyNames = append(topologyNames, filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("miner-swarm-%d", swarmID), "swarm.json")))
	}
	for operatorID := 1; operatorID <= topologyOperators; operatorID++ {
		topologyNames = append(topologyNames, filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("claim-relayer-%d", operatorID), "swarm.json")))
	}
	topology, err := finalCollectedNamedEntries(stateRoot, topologyNames)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, fmt.Errorf("capture miner topology: %w", err)
	}
	locators, err = persistFinalCollectedBundleChunks(runRoot, "miner-topology", topology)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	bundles = append(bundles, locators...)

	claimQueues, err := finalCollectedNamedEntries(stateRoot, finalClaimQueueNames(topologyMiners))
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, fmt.Errorf("capture claim queues: %w", err)
	}
	locators, err = persistFinalCollectedBundleChunks(runRoot, "claim-runtime", claimQueues)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	bundles = append(bundles, locators...)

	cleanup, err := finalContractCleanupEntries(stateRoot, topologyOperators)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, fmt.Errorf("capture accepted contract cleanup: %w", err)
	}
	locators, err = persistFinalCollectedBundleChunks(runRoot, "accepted-contract-cleanup", cleanup)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	bundles = append(bundles, locators...)

	sort.Slice(bundles, func(i, j int) bool { return bundles[i].URI < bundles[j].URI })
	return bundles, resultLocator, terminalLocator, historyLocator, nil
}

func finalSemanticLaunchFoundationNames() []string {
	return []string{"journal.jsonl", "plan.json", "public.json", "runtime-config-manifest.json", "supervisor.json", "supervisor.service.json", "supervisor.state.json"}
}

func finalSemanticPublicCapturePath(relative string) bool {
	return strings.HasSuffix(relative, ".json") && !strings.HasPrefix(relative, finalSemanticSupplementArchiveDir+"/")
}

func finalClaimQueueNames(topologyMiners int) []string {
	if topologyMiners < 1 {
		return nil
	}
	names := make([]string, 0, topologyMiners)
	for minerID := 1; minerID <= topologyMiners; minerID++ {
		names = append(names, filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("miner-%d", minerID), "claims", "claim-queue.json")))
	}
	return names
}

func finalContractCleanupEntries(stateRoot string, topologyOperators int) ([]FinalCollectedFileBundleEntry, error) {
	if topologyOperators < 1 {
		return nil, errors.New("contract cleanup operator count is invalid")
	}
	var supervisor SupervisorState
	if err := decodeStrictJSONFile(filepath.Join(stateRoot, "supervisor.state.json"), &supervisor); err != nil {
		return nil, err
	}
	cutoff, err := time.Parse(time.RFC3339Nano, supervisor.ContractCleanupCutoff)
	if err != nil || supervisor.ContractCleanupCutoff != cutoff.UTC().Format(time.RFC3339Nano) || cutoff.UnixNano() <= 0 {
		return nil, errors.New("accepted supervisor contract cleanup cutoff is invalid")
	}
	taskworkers := map[string]ProcessState{}
	for _, process := range supervisor.Processes {
		if process.Role == "operator-taskworker" {
			if process.ID == "" || taskworkers[process.ID].ID != "" {
				return nil, errors.New("accepted supervisor has duplicate or empty taskworker identity")
			}
			taskworkers[process.ID] = process
		}
	}
	if len(taskworkers) != topologyOperators {
		return nil, errors.New("accepted supervisor taskworker census differs from topology")
	}
	generation := strconv.FormatInt(cutoff.UnixNano(), 10)
	names := make([]string, 0, 2*topologyOperators)
	for noID := 1; noID <= topologyOperators; noID++ {
		id := fmt.Sprintf("operator-%d-taskworker", noID)
		process, ok := taskworkers[id]
		if !ok || process.Identity != fmt.Sprintf("no:%d", noID) || process.PID <= 1 || !process.Healthy || process.ExitError != "" {
			return nil, fmt.Errorf("accepted supervisor taskworker %s is absent or unhealthy", id)
		}
		base := filepath.ToSlash(filepath.Join("processes", id+"-contract-cleanup-"+generation))
		names = append(names, base+".json", base+".log")
	}
	return finalCollectedNamedEntries(stateRoot, names)
}

func finalCollectedDirectoryEntries(root string, include func(string) bool) ([]FinalCollectedFileBundleEntry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries := make([]FinalCollectedFileBundleEntry, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("captured path %s is a symlink", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("captured path %s is not regular", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !include(relative) {
			return nil
		}
		item, err := finalCollectedFileEntry(root, relative)
		if err != nil {
			return err
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("captured directory has no required files")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func finalCollectedNamedEntries(root string, names []string) ([]FinalCollectedFileBundleEntry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries := make([]FinalCollectedFileBundleEntry, 0, len(names))
	for _, name := range names {
		item, err := finalCollectedFileEntry(root, filepath.ToSlash(name))
		if err != nil {
			return nil, err
		}
		entries = append(entries, item)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	for index := 1; index < len(entries); index++ {
		if entries[index].Path == entries[index-1].Path {
			return nil, errors.New("captured file names contain a duplicate")
		}
	}
	return entries, nil
}

func finalCollectedFileEntry(root, relative string) (FinalCollectedFileBundleEntry, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if relative == "" || filepath.IsAbs(clean) || filepath.ToSlash(clean) != relative || clean == "." || strings.HasPrefix(relative, "../") {
		return FinalCollectedFileBundleEntry{}, errors.New("captured file path is unsafe")
	}
	absolute := filepath.Join(root, clean)
	if !pathWithinRoot(root, absolute) {
		return FinalCollectedFileBundleEntry{}, errors.New("captured file escapes its root")
	}
	file, err := openFinalCollectedFile(root, clean)
	if err != nil {
		return FinalCollectedFileBundleEntry{}, &os.PathError{Op: "open captured file", Path: absolute, Err: err}
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > finalCollectedBundleMaximumRawBytes {
		return FinalCollectedFileBundleEntry{}, fmt.Errorf("captured file %s is not regular or exceeds the bundle limit", relative)
	}
	data, err := io.ReadAll(io.LimitReader(file, finalCollectedBundleMaximumRawBytes+1))
	if err != nil {
		return FinalCollectedFileBundleEntry{}, err
	}
	after, err := file.Stat()
	if err != nil || len(data) > finalCollectedBundleMaximumRawBytes || !sameFinalCollectedFileState(before, after) || uint64(len(data)) != uint64(before.Size()) {
		return FinalCollectedFileBundleEntry{}, fmt.Errorf("captured file %s changed while read", relative)
	}
	return FinalCollectedFileBundleEntry{Path: relative, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data)), Data: data}, nil
}

// openFinalCollectedFile walks each component relative to an already-open
// directory descriptor. O_NOFOLLOW on every step prevents a concurrent rename
// from redirecting capture through either a leaf or parent-directory symlink.
func openFinalCollectedFile(root, relative string) (*os.File, error) {
	directoryFD, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	components := strings.Split(relative, string(filepath.Separator))
	for _, component := range components[:len(components)-1] {
		nextFD, openErr := syscall.Openat(directoryFD, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		_ = syscall.Close(directoryFD)
		if openErr != nil {
			return nil, openErr
		}
		directoryFD = nextFD
	}
	leafFD, openErr := syscall.Openat(directoryFD, components[len(components)-1], syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	_ = syscall.Close(directoryFD)
	if openErr != nil {
		return nil, openErr
	}
	file := os.NewFile(uintptr(leafFD), relative)
	if file == nil {
		_ = syscall.Close(leafFD)
		return nil, errors.New("construct captured file descriptor")
	}
	return file, nil
}

func sameFinalCollectedFileState(before, after os.FileInfo) bool {
	if before == nil || after == nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return false
	}
	beforeStat, beforeOK := before.Sys().(*syscall.Stat_t)
	afterStat, afterOK := after.Sys().(*syscall.Stat_t)
	if beforeOK != afterOK {
		return false
	}
	return !beforeOK || beforeStat.Dev == afterStat.Dev && beforeStat.Ino == afterStat.Ino && beforeStat.Nlink == afterStat.Nlink && beforeStat.Ctim == afterStat.Ctim && beforeStat.Mtim == afterStat.Mtim
}

func persistFinalCollectedBundleChunks(runRoot, name string, entries []FinalCollectedFileBundleEntry) ([]FinalArtifactLocator, error) {
	if name == "" || len(entries) == 0 {
		return nil, errors.New("collected bundle is empty")
	}
	chunks := make([][]FinalCollectedFileBundleEntry, 0, 1)
	current := make([]FinalCollectedFileBundleEntry, 0)
	currentBytes := 0
	for _, entry := range entries {
		entryBytes := len(entry.Data) + len(entry.Path) + len(entry.ContentHash) + 128
		if entryBytes > finalCollectedBundleMaximumRawBytes {
			return nil, fmt.Errorf("captured file %s exceeds bundle limit", entry.Path)
		}
		if len(current) > 0 && currentBytes+entryBytes > finalCollectedBundleMaximumRawBytes {
			chunks = append(chunks, current)
			current = nil
			currentBytes = 0
		}
		current = append(current, entry)
		currentBytes += entryBytes
	}
	chunks = append(chunks, current)
	locators := make([]FinalArtifactLocator, 0, len(chunks))
	for index, chunk := range chunks {
		bundleName := name
		if len(chunks) > 1 {
			bundleName = fmt.Sprintf("%s-%03d-of-%03d", name, index+1, len(chunks))
		}
		bundle := FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: bundleName, Files: chunk}
		if err := verifyFinalCollectedFileBundle(&bundle); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(&bundle)
		if err != nil {
			return nil, err
		}
		locator, err := persistFinalCollectedArtifact(runRoot, "closed-input-bundle", "final-inputs/bundles/"+bundleName+".json", encoded)
		if err != nil {
			return nil, err
		}
		locators = append(locators, locator)
	}
	return locators, nil
}

func verifyFinalCollectedFileBundle(bundle *FinalCollectedFileBundle) error {
	if bundle == nil || bundle.Schema != finalCollectedFileBundleSchema || bundle.Name == "" || strings.ContainsAny(bundle.Name, "/\\\r\n\x00") || len(bundle.Files) == 0 {
		return errors.New("collected file bundle identity is incomplete")
	}
	for index, entry := range bundle.Files {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(entry.Path)))
		if entry.Path == "" || clean != entry.Path || filepath.IsAbs(filepath.FromSlash(entry.Path)) || strings.HasPrefix(entry.Path, "../") || (index > 0 && entry.Path <= bundle.Files[index-1].Path) {
			return errors.New("collected file bundle paths are unsafe, duplicated, or non-canonical")
		}
		if entry.SizeBytes != uint64(len(entry.Data)) || entry.ContentHash != bytesSHA256(entry.Data) {
			return fmt.Errorf("collected file %s size or hash differs", entry.Path)
		}
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	if len(encoded) > finalCollectedBundleMaximumBytes {
		return errors.New("collected file bundle exceeds encoded size limit")
	}
	return nil
}

func decodeFinalCollectedFileBundle(encoded []byte) (*FinalCollectedFileBundle, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var bundle FinalCollectedFileBundle
	if err := decoder.Decode(&bundle); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("collected file bundle has trailing JSON")
		}
		return nil, err
	}
	if err := verifyFinalCollectedFileBundle(&bundle); err != nil {
		return nil, err
	}
	return &bundle, nil
}
