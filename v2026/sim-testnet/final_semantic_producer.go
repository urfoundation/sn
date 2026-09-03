package main

// final_semantic_producer.go is the single fail-closed production boundary
// for final-semantic-evidence.json and FINAL.md. ScenarioResult and
// ScenarioObservation do not contain every required raw receipt/census, so the
// caller must supply the complete typed source instead of synthesizing absent
// facts from a pass boolean.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	finalSemanticEvidenceFilename = "final-semantic-evidence.json"
	finalSemanticMarkdownFilename = "FINAL.md"
	finalSemanticStagePrefix      = ".final-semantic-stage-"
)

type FinalSemanticChainReaderFactory func(context.Context, *FinalSemanticEvidence) (FinalSemanticChainReader, error)
type FinalSemanticOutputScanner func(name string, content []byte) error

// ProduceFinalSemanticOutputs writes exactly one semantic object and one
// human-readable rendering. No destination is created until the draft,
// artifacts, public archive replay, sealed object, markdown, and byte scanner
// all pass. Existing output is an error; publication is never overwritten.
func ProduceFinalSemanticOutputs(ctx context.Context, runDir string, source FinalSemanticEvidence, load FinalArtifactLoader, newReader FinalSemanticChainReaderFactory, scan FinalSemanticOutputScanner) (*FinalSemanticEvidence, error) {
	if ctx == nil || load == nil || newReader == nil || scan == nil {
		return nil, errors.New("final semantic producer dependencies are incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runDir, err := filepath.Abs(runDir)
	if err != nil || filepath.Clean(runDir) != runDir {
		return nil, errors.New("final semantic run directory is invalid")
	}
	info, err := os.Lstat(runDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("final semantic run directory does not exist")
	}
	evidencePath := filepath.Join(runDir, finalSemanticEvidenceFilename)
	markdownPath := filepath.Join(runDir, finalSemanticMarkdownFilename)
	if err := finalSemanticOutputsAbsent(evidencePath, markdownPath); err != nil {
		return nil, err
	}
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		return nil, fmt.Errorf("build final semantic evidence: %w", err)
	}
	if err := VerifyFinalSemanticArtifacts(ctx, draft, load); err != nil {
		return nil, fmt.Errorf("verify final semantic artifacts before public replay: %w", err)
	}
	reader, err := newReader(ctx, draft)
	if err != nil {
		return nil, fmt.Errorf("construct public final semantic reader: %w", err)
	}
	if reader == nil {
		return nil, errors.New("public final semantic reader factory returned nil")
	}
	if closer, ok := reader.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	sealed, err := SealFinalSemanticEvidenceOnChain(ctx, draft, reader)
	if err != nil {
		return nil, fmt.Errorf("seal final semantic evidence on public chains: %w", err)
	}
	if err := VerifyFinalSemanticArtifacts(ctx, sealed, load); err != nil {
		return nil, fmt.Errorf("verify sealed final semantic artifacts: %w", err)
	}
	markdown, err := RenderFinalSemanticEvidenceMarkdown(sealed)
	if err != nil {
		return nil, fmt.Errorf("render sealed FINAL.md: %w", err)
	}
	encoded, err := json.MarshalIndent(sealed, "", "  ")
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if err := scan(finalSemanticEvidenceFilename, append([]byte(nil), encoded...)); err != nil {
		return nil, fmt.Errorf("scan %s: %w", finalSemanticEvidenceFilename, err)
	}
	if err := scan(finalSemanticMarkdownFilename, append([]byte(nil), markdown...)); err != nil {
		return nil, fmt.Errorf("scan %s: %w", finalSemanticMarkdownFilename, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := finalSemanticOutputsAbsent(evidencePath, markdownPath); err != nil {
		return nil, err
	}
	if err := writeExclusiveFinalSemanticOutputs(evidencePath, encoded, markdownPath, markdown); err != nil {
		return nil, err
	}
	return sealed, nil
}

func finalSemanticOutputsAbsent(paths ...string) error {
	for _, path := range paths {
		_, err := os.Lstat(path)
		if err == nil {
			return fmt.Errorf("final semantic output already exists: %s", filepath.Base(path))
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func writeExclusiveFinalSemanticOutputs(evidencePath string, evidence []byte, markdownPath string, markdown []byte) error {
	directoryPath := filepath.Dir(evidencePath)
	if directoryPath != filepath.Dir(markdownPath) {
		return errors.New("final semantic output pair has different directories")
	}
	stageRoot, stagedEvidence, stagedMarkdown, err := stageFinalSemanticOutputs(directoryPath, evidence, markdown)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageRoot) // exact private staging directory

	created := make([]string, 0, 2)
	rollback := func() {
		for _, path := range created {
			_ = os.Remove(path)
		}
		_ = syncFinalSemanticDirectory(directoryPath)
	}
	if err := os.Link(stagedEvidence, evidencePath); err != nil {
		rollback()
		return fmt.Errorf("install exclusive %s: %w", filepath.Base(evidencePath), err)
	}
	created = append(created, evidencePath)
	if err := os.Link(stagedMarkdown, markdownPath); err != nil {
		rollback()
		return fmt.Errorf("install exclusive %s: %w", filepath.Base(markdownPath), err)
	}
	created = append(created, markdownPath)
	if err := syncFinalSemanticDirectory(directoryPath); err != nil {
		rollback()
		return err
	}
	return nil
}

// stageFinalSemanticOutputs makes both byte streams durable before either
// authoritative filename can exist. Linking a staged inode is atomic, so a
// host loss can leave only a complete pair or one complete half; the latter is
// quarantined and regenerated by the asynchronous supplement recovery path.
func stageFinalSemanticOutputs(directoryPath string, evidence, markdown []byte) (string, string, string, error) {
	stageRoot, err := os.MkdirTemp(directoryPath, finalSemanticStagePrefix)
	if err != nil {
		return "", "", "", err
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(stageRoot)
		}
	}()
	write := func(name string, content []byte) (string, error) {
		path := filepath.Join(stageRoot, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return "", err
		}
		written, writeErr := file.Write(content)
		if writeErr == nil && written != len(content) {
			writeErr = errors.New("short final semantic staging write")
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return "", err
		}
		return path, nil
	}
	stagedEvidence, err := write(finalSemanticEvidenceFilename, evidence)
	if err != nil {
		return "", "", "", fmt.Errorf("stage %s: %w", finalSemanticEvidenceFilename, err)
	}
	stagedMarkdown, err := write(finalSemanticMarkdownFilename, markdown)
	if err != nil {
		return "", "", "", fmt.Errorf("stage %s: %w", finalSemanticMarkdownFilename, err)
	}
	if err := syncFinalSemanticDirectory(stageRoot); err != nil {
		return "", "", "", err
	}
	failed = false
	return stageRoot, stagedEvidence, stagedMarkdown, nil
}

// NewFinalSemanticSecretScanner produces the required pre-write byte scanner
// from the same secret inventory used by the wider evidence-tree scan.
func NewFinalSemanticSecretScanner(roles *RoleSecrets, walletSecrets ...string) FinalSemanticOutputScanner {
	matcher := newFinalSemanticSecretMatcher(roles, walletSecrets...)
	return func(name string, content []byte) error {
		if err := matcher.scan(name, content); err != nil {
			return fmt.Errorf("secret material found in generated evidence file %s", name)
		}
		return nil
	}
}
