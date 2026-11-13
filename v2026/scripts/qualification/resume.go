// Immutable stage receipts allow interrupted selected unions to resume.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
)

type retainedStage struct {
	Origin string               `json:"origin"`
	Result stageResult          `json:"result"`
	Files  map[string]fileProof `json:"files"`
}

type stageReceipt struct {
	Path  string    `json:"path"`
	Proof fileProof `json:"proof"`
}

type matrixCheckpoint struct {
	Version     int                     `json:"version"`
	Source      fileProof               `json:"source"`
	Plan        planSpec                `json:"plan"`
	Environment map[string]string       `json:"environment"`
	Inputs      map[string]fileProof    `json:"inputs"`
	Stages      map[string]stageReceipt `json:"stages"`
}

// Captured receipts can be larger than individual input documents, but retain
// the same strict JSON grammar and the existing 64 MiB capture ceiling.
func readCheckpoint(path string, value any) error {
	data, err := readBounded(path, capturedJSONLimit)
	if err != nil {
		return err
	}
	return decodeJSONLimit(data, value, capturedJSONLimit)
}

func writeCheckpoint(path string, value any) error {
	if err := writeJSON(path, value); err != nil {
		return err
	}
	file, _, err := openRegular(path)
	if err != nil {
		return err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// A kernel-owned lock cannot match a watcher argv or a recycled pid. The old
// capture stays locked for the entire continuation, and is never overwritten.
func lockCapture(capture string, create bool) (*os.File, error) {
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		flags |= os.O_CREATE | os.O_EXCL
	}
	path := filepath.Join(capture, "run.lock")
	file, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() != 0) {
		err = errors.New("invalid capture lock file")
	}
	if err == nil {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		return nil, errors.Join(errors.New("capture is active or cannot be locked"), err, file.Close())
	}
	return file, nil
}

func resumeEnvironment(owner *stageOwner) map[string]string {
	environment := executionEnvironment(os.Environ(), owner.Environment)
	// Each invocation intentionally has a new private scratch root. These are
	// routing/lifecycle values, not compiler, test selection, or source controls.
	for _, name := range []string{"TMPDIR", "GOTMPDIR", "TMP", "TEMP", "SN_TEST_COMMAND_ROOT", "CARGO_TARGET_DIR", "SHLVL", "_"} {
		delete(environment, name)
	}
	// Inherited environment values can contain credentials. Bind them without
	// copying their contents into qualification evidence.
	for name, value := range environment {
		digest := sha256.Sum256([]byte(value))
		environment[name] = hex.EncodeToString(digest[:])
	}
	return environment
}

// Save each completed receipt once. Checkpoint publication is atomic; a lost
// final status can lose at most the unrecorded stage, never promote an unfinished
// body. Failed/blocked stages remain in the report and are not reusable.
func retainStage(capture, name string, result stageResult) (stageReceipt, error) {
	if result.Status != "passed" || result.Command == nil || !result.Command.Joined {
		return stageReceipt{}, errors.New("only a completed joined stage can be retained")
	}
	files := map[string]fileProof{}
	commands := []string{name}
	var paths []string
	if strings.HasPrefix(name, "build-") {
		commands = append(commands, name+"-identity", name+"-modules")
		paths = append(paths, filepath.Join(capture, name+".testbin"), filepath.Join(capture, name+".testbin.json"))
	} else {
		commands = append(commands, name+"-list", name+"-events")
	}
	for _, command := range commands {
		for _, suffix := range []string{".request.json", ".command.json", ".result.json", ".stdout", ".stderr", ".owner.log", ".joined"} {
			paths = append(paths, filepath.Join(capture, command+suffix))
		}
	}
	for _, path := range paths {
		proof, err := regularProof(path)
		if err != nil {
			return stageReceipt{}, err
		}
		files[path] = proof
	}
	path := filepath.Join(capture, name+".receipt.json")
	if err := writeCheckpoint(path, retainedStage{Origin: capture, Result: result, Files: files}); err != nil {
		return stageReceipt{}, err
	}
	proof, err := regularProof(path)
	return stageReceipt{Path: path, Proof: proof}, err
}

func sameResumePlan(previous, current planSpec) error {
	if len(previous.Suites) != len(current.Suites) {
		return errors.New("resume suite census changed")
	}
	// Captured input paths change; their complete parsed identities and outcomes
	// must not. All other plan fields (including budgets and mode) remain exact.
	copyPlan := previous
	copyPlan.Suites = append([]suiteSpec(nil), previous.Suites...)
	for index := range copyPlan.Suites {
		old, now := copyPlan.Suites[index], current.Suites[index]
		oldExpected, oldErr := expectedInputs(old.Outcomes, old.FailureLiterals)
		nowExpected, nowErr := expectedInputs(now.Outcomes, now.FailureLiterals)
		if err := errors.Join(oldErr, nowErr); err != nil {
			return err
		}
		if !reflect.DeepEqual(oldExpected, nowExpected) {
			return errors.New("resume root, descendant, outcome or failure literal changed")
		}
		copyPlan.Suites[index].Outcomes, copyPlan.Suites[index].FailureLiterals = now.Outcomes, now.FailureLiterals
	}
	if !reflect.DeepEqual(copyPlan, current) {
		return errors.New("resume plan or resource limits changed")
	}
	return nil
}

func validateRetainedStage(name string, ref stageReceipt, node stage, plan planSpec, packages map[string]packageSpec) (retainedStage, error) {
	var retained retainedStage
	if err := checkProofs(map[string]fileProof{ref.Path: ref.Proof}); err != nil {
		return retained, err
	}
	if err := readCheckpoint(ref.Path, &retained); err != nil {
		return retained, err
	}
	if ref.Path != filepath.Join(retained.Origin, name+".receipt.json") || retained.Result.Status != "passed" || retained.Result.Command == nil || len(retained.Files) == 0 || len(retained.Result.ModuleSources) == 0 {
		return retained, errors.New("invalid completed stage receipt")
	}
	for path := range retained.Files {
		if filepath.Dir(path) != retained.Origin {
			return retained, errors.New("retained evidence is outside its capture")
		}
	}
	if err := errors.Join(checkProofs(retained.Files), checkModuleSourceProofs(retained.Result.ModuleSources)); err != nil {
		return retained, err
	}
	// The checkpoint's summary must exactly match the joined owner receipt.
	commandPath := filepath.Join(retained.Origin, name+".result.json")
	var command commandResult
	if retained.Files[commandPath].SHA256 == "" {
		return retained, errors.New("stage has no retained command result")
	}
	if err := readJSON(commandPath, &command); err != nil || !reflect.DeepEqual(command, *retained.Result.Command) {
		return retained, errors.Join(err, errors.New("stage differs from its original command receipt"))
	}
	if node.Build != nil {
		binary := filepath.Join(retained.Origin, name+".testbin")
		if !commandMatches(command, 0) || retained.Files[binary] != (fileProof{SHA256: retained.Result.BinarySHA256, Mode: retained.Result.BinaryMode}) {
			return retained, errors.New("retained build has no exact successful binary")
		}
		return retained, nil
	}
	expected, err := expectedInputs(node.Suite.Outcomes, node.Suite.FailureLiterals)
	if err != nil {
		return retained, err
	}
	wanted := 0
	if len(expected.Markers) != 0 {
		wanted = 1
	}
	if !commandMatches(command, wanted) || retained.Files[retained.Result.Events].SHA256 == "" {
		return retained, errors.New("retained suite lacks successful body or events")
	}
	file, info, err := openRegular(retained.Result.Events)
	if err != nil {
		return retained, err
	}
	checked, verifyErr := verifyEvents(file, expected, packages[node.Suite.Package].ImportPath, *command.Exit)
	closeErr := errors.Join(sameRegular(retained.Result.Events, file, info), file.Close())
	if err := errors.Join(verifyErr, closeErr); err != nil {
		return retained, err
	}
	if retained.Result.Verification == nil || !reflect.DeepEqual(checked, *retained.Result.Verification) {
		return retained, errors.New("retained event verification differs")
	}
	return retained, nil
}

// Refuse changed evidence before starting any test. Compatible completed stages
// keep their original evidence locators; only the compiled bytes are copied for
// new bodies, and their parent-owned hash is checked again by executeStage.
func loadResume(ctx context.Context, previous string, current matrixCheckpoint, nodes map[string]stage, packages map[string]packageSpec, capture string) (map[string]stageResult, map[string]stageReceipt, error) {
	var checkpoint matrixCheckpoint
	if err := readCheckpoint(filepath.Join(previous, "checkpoint.json"), &checkpoint); err != nil {
		return nil, nil, err
	}
	if checkpoint.Version != 1 || checkpoint.Source != current.Source || !reflect.DeepEqual(checkpoint.Environment, current.Environment) {
		return nil, nil, errors.New("resume source or execution environment changed")
	}
	if err := errors.Join(sameResumePlan(checkpoint.Plan, current.Plan), checkProofs(checkpoint.Inputs)); err != nil {
		return nil, nil, err
	}
	if checkpoint.Inputs[filepath.Join(previous, "runner")] != current.Inputs[filepath.Join(capture, "runner")] {
		return nil, nil, errors.New("resume qualification runner changed")
	}
	results, retained := map[string]stageResult{}, map[string]retainedStage{}
	for _, name := range sortedKeys(checkpoint.Stages) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		node, exists := nodes[name]
		if !exists {
			return nil, nil, fmt.Errorf("foreign resume stage %s", name)
		}
		item, err := validateRetainedStage(name, checkpoint.Stages[name], node, current.Plan, packages)
		if err != nil {
			return nil, nil, fmt.Errorf("resume %s: %w", name, err)
		}
		results[name], retained[name] = item.Result, item
	}
	for name, item := range retained {
		node := nodes[name]
		for _, dependency := range node.Dependencies {
			parent, exists := results[dependency]
			if !exists || parent.BinarySHA256 != item.Result.BinarySHA256 || parent.BinaryMode != item.Result.BinaryMode || !reflect.DeepEqual(parent.ModuleSources, item.Result.ModuleSources) {
				return nil, nil, errors.New("resume suite has no matching completed parent build")
			}
		}
	}
	for name, item := range retained {
		if nodes[name].Build == nil {
			continue
		}
		binary := filepath.Join(capture, name+".testbin")
		proof := fileProof{SHA256: item.Result.BinarySHA256, Mode: item.Result.BinaryMode}
		if err := copyFile(filepath.Join(item.Origin, name+".testbin"), binary, os.FileMode(proof.Mode)); err != nil {
			return nil, nil, err
		}
		if err := errors.Join(checkProofs(map[string]fileProof{binary: proof}), writeJSON(binary+".json", proof)); err != nil {
			return nil, nil, err
		}
	}
	return results, checkpoint.Stages, nil
}
