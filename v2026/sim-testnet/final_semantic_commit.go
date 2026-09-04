package main

// final_semantic_commit.go owns the asynchronous, authenticated handoff from
// a completed scenario archive to its later semantic verification. The
// scenario-complete envelope remains immutable: this supplement binds that
// exact owner signature to every derived/output byte without pretending those
// later files were present when the live capture closed.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/startifact"
	"gopkg.in/yaml.v3"
)

const (
	finalSemanticSupplementSchema          = "urnetwork-final-semantic-supplement-v1"
	finalSemanticSupplementFileSchema      = "urnetwork-final-semantic-supplement-file-v1"
	finalSemanticSupplementKind            = "scenario-semantic-verified"
	finalSemanticSupplementFileKind        = "scenario-semantic-file"
	finalSemanticSupplementStatus          = "semantic_verified"
	finalSemanticSupplementFilename        = "final-semantic-verified.evidence.json"
	finalSemanticSupplementArchiveDir      = "final-semantic-supplements"
	finalSemanticSupplementStageFilename   = "semantic-verified.staged.evidence.json"
	finalSemanticSupplementPublicationLock = ".publish.lock"
)

// FinalSemanticSupplementFile binds one post-capture raw file to the separate
// owner-signed envelope which carries its exact bytes.
type FinalSemanticSupplementFile struct {
	Path         string `json:"path"`
	ContentHash  string `json:"content_hash"`
	Size         uint64 `json:"size"`
	EnvelopeHash string `json:"envelope_hash"`
}

type finalSemanticSupplementFilePayload struct {
	Schema      string `json:"schema"`
	RunID       string `json:"run_id"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Size        uint64 `json:"size"`
	Data        []byte `json:"data"`
}

// FinalSemanticSupplementPayload is the owner-authorized semantic_verified
// statement. It binds both the original live closure and every offline byte.
type FinalSemanticSupplementPayload struct {
	Schema                       string                        `json:"schema"`
	Status                       string                        `json:"status"`
	Phase                        string                        `json:"phase"`
	RunID                        string                        `json:"run_id"`
	ResultHash                   string                        `json:"result_hash"`
	ScenarioCompleteHash         string                        `json:"scenario_complete_hash"`
	ScenarioEvidenceManifestHash string                        `json:"scenario_evidence_manifest_hash"`
	CaptureStatusHash            string                        `json:"capture_status_hash"`
	CollectedInputsHash          string                        `json:"collected_inputs_hash"`
	SemanticEvidenceHash         string                        `json:"semantic_evidence_hash"`
	PublicTranscriptHash         string                        `json:"public_transcript_hash"`
	Files                        []FinalSemanticSupplementFile `json:"files"`
}

type finalSemanticSupplementDependencies struct {
	Load                  FinalArtifactLoader
	NewReader             FinalSemanticChainReaderFactory
	Stores                scenarioCompletionStoreFactory
	VerifyRuntime         func(*ResolvedConfig, string) error
	ResolveCapturedStores func(context.Context, *ResolvedConfig, string, string) (map[int]server.BlobStore, error)
}

type finalSemanticOriginalClosure struct {
	runRoot          string
	complete         *ReleaseEvidenceEnvelope
	completePayload  scenarioCompletePayload
	manifest         *ReleaseEvidenceEnvelope
	manifestPayload  *campaignEvidenceManifestPayload
	collected        *FinalSemanticCollectedInputs
	capture          *FinalSemanticCaptureStatus
	owner            EVMRoleSecret
	ownerPublicKey   *ecdsa.PublicKey
	authenticatedRaw map[string][]byte
}

type finalSemanticRawFile struct {
	Path        string
	ContentHash string
	Data        []byte
}

// PublishOrResumeFinalSemanticSupplement is the real asynchronous orchestration
// API. It resumes an immutable staged publication, validates an existing output
// pair, or generates the pair from the closed capture when both outputs are
// absent. The local semantic_verified marker is written only after every
// operator replica has been written and read back.
func PublishOrResumeFinalSemanticSupplement(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runDir string, result *ScenarioResult) (*ReleaseEvidenceEnvelope, error) {
	return publishOrResumeFinalSemanticSupplement(ctx, cfg, roles, stateDir, runDir, result, finalSemanticSupplementDependencies{})
}

// ValidateFinalSemanticSupplement authenticates the committed supplement,
// owner-carried file bytes, original scenario closure, semantic reconstruction,
// and every operator-store replica. Loose derived/output files are never an
// authority; if present, they must exactly match the signed bytes.
func ValidateFinalSemanticSupplement(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runDir string, result *ScenarioResult) (*ReleaseEvidenceEnvelope, error) {
	return validateFinalSemanticSupplement(ctx, cfg, roles, stateDir, runDir, result, finalSemanticSupplementDependencies{})
}

func publishOrResumeFinalSemanticSupplement(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runDir string, result *ScenarioResult, dependencies finalSemanticSupplementDependencies) (*ReleaseEvidenceEnvelope, error) {
	stateRoot, runRoot, err := finalSemanticSupplementRoots(ctx, cfg, roles, stateDir, runDir, result)
	if err != nil {
		return nil, err
	}
	stageRoot := finalSemanticSupplementStageRoot(stateRoot, result.RunID)
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return nil, err
	}
	if err := rejectFinalArtifactSymlinkComponents(stateRoot, stageRoot); err != nil {
		return nil, fmt.Errorf("semantic supplement staging directory: %w", err)
	}
	lockPath := filepath.Join(stageRoot, finalSemanticSupplementPublicationLock)
	if info, statErr := os.Lstat(lockPath); statErr == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("semantic supplement publication lock is not a regular file")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := lockFinalSemanticSupplement(ctx, lock); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	closure, err := authenticateFinalSemanticOriginalClosure(cfg, roles, stateRoot, runRoot, result)
	if err != nil {
		return nil, fmt.Errorf("authenticate original semantic capture: %w", err)
	}
	load := dependencies.Load
	if load == nil {
		load, err = NewFinalSemanticCampaignArtifactLoader(stateRoot, runRoot)
		if err != nil {
			return nil, err
		}
	}
	stores, err := resolveFinalSemanticSupplementStores(ctx, cfg, stateRoot, runRoot, closure, dependencies)
	if err != nil {
		return nil, err
	}
	committedPath := filepath.Join(runRoot, finalSemanticSupplementFilename)
	if committed, statErr := finalSemanticRegularFileExists(committedPath); statErr != nil {
		return nil, statErr
	} else if committed {
		return validateFinalSemanticSupplementWithClosure(ctx, cfg, roles, stateRoot, runRoot, result, closure, load, stores)
	}
	if err := ensureFinalSemanticSupplementOutputs(ctx, cfg, roles, stateRoot, runRoot, result, closure, load, dependencies.NewReader); err != nil {
		return nil, err
	}
	rawFiles, semantic, err := loadAndVerifyFinalSemanticRawFiles(ctx, cfg, roles, runRoot, result, load)
	if err != nil {
		return nil, err
	}
	scan := NewFinalSemanticSecretScanner(roles, cfg.WalletSecret, cfg.WalletMaterial, cfg.WalletPasswordSecret, cfg.WalletPassword)
	for _, raw := range rawFiles {
		if err := scan(raw.Path, append([]byte(nil), raw.Data...)); err != nil {
			return nil, fmt.Errorf("scan semantic supplement file %s: %w", raw.Path, err)
		}
	}
	fileEnvelopes, entries, err := prepareFinalSemanticSupplementFiles(cfg, stateRoot, result.RunID, closure.owner, rawFiles)
	if err != nil {
		return nil, err
	}
	payload := FinalSemanticSupplementPayload{
		Schema: finalSemanticSupplementSchema, Status: finalSemanticSupplementStatus,
		Phase: result.Name, RunID: result.RunID, ResultHash: strings.ToLower(result.EvidenceHash),
		ScenarioCompleteHash: closure.complete.ContentHash, ScenarioEvidenceManifestHash: closure.manifest.ContentHash,
		CaptureStatusHash: closure.capture.EvidenceHash, CollectedInputsHash: closure.collected.EvidenceHash,
		SemanticEvidenceHash: semantic.EvidenceHash, PublicTranscriptHash: semantic.PublicVerification.TranscriptHash,
		Files: entries,
	}
	manifestPath := filepath.Join(stageRoot, finalSemanticSupplementStageFilename)
	manifest, encodedManifest, err := prepareFinalSemanticLocalEvidence(cfg, stateRoot, manifestPath, finalSemanticSupplementKind, result.RunID, payload, closure.owner)
	if err != nil {
		return nil, fmt.Errorf("prepare semantic supplement: %w", err)
	}
	stagedPayload, stagedFiles, err := verifyFinalSemanticSupplementBinding(cfg, result, closure, manifest, stateRoot)
	if err != nil {
		return nil, fmt.Errorf("verify staged semantic supplement: %w", err)
	}
	if !finalJSONEqual(*stagedPayload, payload) || !finalSemanticRawFilesEqual(rawFiles, stagedFiles) {
		return nil, errors.New("staged semantic supplement differs from the verified output graph")
	}
	// Publish every carried file everywhere before making semantic_verified
	// visible in any history. A failed later replica remains safely resumable.
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		for _, envelope := range fileEnvelopes {
			if err := publishAndReadBackFinalSemanticEnvelope(ctx, stores[operator], envelope); err != nil {
				return nil, fmt.Errorf("operator %d semantic file publication: %w", operator, err)
			}
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		if err := publishAndReadBackFinalSemanticEnvelope(ctx, stores[operator], manifest); err != nil {
			return nil, fmt.Errorf("operator %d semantic supplement publication: %w", operator, err)
		}
	}
	// Re-read the mutable source paths after remote publication. A concurrent
	// replacement cannot be blessed by the final local commit.
	again, err := enumerateFinalSemanticRawFiles(runRoot)
	if err != nil || !finalSemanticRawFilesEqual(rawFiles, again) {
		return nil, stateMismatchError(err, "semantic output files changed during publication")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	closureAgain, err := authenticateFinalSemanticOriginalClosure(cfg, roles, stateRoot, runRoot, result)
	if err != nil || !finalSemanticOriginalClosuresEqual(closure, closureAgain) {
		return nil, stateMismatchError(err, "original semantic closure changed during publication")
	}
	stagedAgain, stagedFilesAgain, err := verifyFinalSemanticSupplementBinding(cfg, result, closureAgain, manifest, stateRoot)
	if err != nil || !finalJSONEqual(*stagedAgain, payload) || !finalSemanticRawFilesEqual(rawFiles, stagedFilesAgain) {
		return nil, stateMismatchError(err, "staged semantic supplement changed during publication")
	}
	if err := writeFinalSemanticImmutableLocal(committedPath, append(encodedManifest, '\n')); err != nil {
		return nil, fmt.Errorf("commit semantic supplement: %w", err)
	}
	var committed ReleaseEvidenceEnvelope
	if err := decodeFinalSemanticRegularJSON(committedPath, &committed); err != nil || !finalJSONEqual(committed, *manifest) {
		return nil, stateMismatchError(err, "committed semantic supplement differs from the published owner envelope")
	}
	return &committed, nil
}

func validateFinalSemanticSupplement(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runDir string, result *ScenarioResult, dependencies finalSemanticSupplementDependencies) (*ReleaseEvidenceEnvelope, error) {
	stateRoot, runRoot, err := finalSemanticSupplementRoots(ctx, cfg, roles, stateDir, runDir, result)
	if err != nil {
		return nil, err
	}
	closure, err := authenticateFinalSemanticOriginalClosure(cfg, roles, stateRoot, runRoot, result)
	if err != nil {
		return nil, fmt.Errorf("authenticate original semantic capture: %w", err)
	}
	load := dependencies.Load
	if load == nil {
		load, err = NewFinalSemanticCampaignArtifactLoader(stateRoot, runRoot)
		if err != nil {
			return nil, err
		}
	}
	stores, err := resolveFinalSemanticSupplementStores(ctx, cfg, stateRoot, runRoot, closure, dependencies)
	if err != nil {
		return nil, err
	}
	return validateFinalSemanticSupplementWithClosure(ctx, cfg, roles, stateRoot, runRoot, result, closure, load, stores)
}

func validateFinalSemanticSupplementWithClosure(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateRoot, runRoot string, result *ScenarioResult, closure *finalSemanticOriginalClosure, load FinalArtifactLoader, stores map[int]server.BlobStore) (*ReleaseEvidenceEnvelope, error) {
	committedPath := filepath.Join(runRoot, finalSemanticSupplementFilename)
	if err := requireFinalSemanticRegularFile(committedPath); err != nil {
		return nil, fmt.Errorf("semantic supplement commit: %w", err)
	}
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(committedPath, &envelope); err != nil {
		return nil, fmt.Errorf("decode semantic supplement commit: %w", err)
	}
	payload, files, err := verifyFinalSemanticSupplementBinding(cfg, result, closure, &envelope, stateRoot)
	if err != nil {
		return nil, err
	}
	loose, err := enumeratePresentFinalSemanticRawFiles(runRoot)
	if err != nil {
		return nil, fmt.Errorf("read loose semantic outputs: %w", err)
	}
	if mismatch := finalSemanticLooseRawFileMismatch(files, loose); mismatch != "" {
		return nil, fmt.Errorf("loose semantic output %s differs from owner-signed supplement bytes", mismatch)
	}
	fileEnvelopes, err := loadFinalSemanticSupplementFileEnvelopes(cfg, closure, payload, stateRoot)
	if err != nil {
		return nil, err
	}
	if stores != nil {
		for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
			for _, fileEnvelope := range fileEnvelopes {
				if err := readBackFinalSemanticEnvelope(ctx, stores[operator], fileEnvelope); err != nil {
					return nil, fmt.Errorf("operator %d semantic file replica: %w", operator, err)
				}
			}
			if err := readBackFinalSemanticEnvelope(ctx, stores[operator], &envelope); err != nil {
				return nil, fmt.Errorf("operator %d semantic supplement replica: %w", operator, err)
			}
		}
	}
	verified, _, err := loadAndVerifyFinalSemanticBytes(ctx, cfg, roles, result, files, load)
	if err != nil {
		return nil, fmt.Errorf("verify owner-signed semantic output graph: %w", err)
	}
	if !strings.EqualFold(verified.EvidenceHash, payload.SemanticEvidenceHash) || verified.PublicVerification == nil || !strings.EqualFold(verified.PublicVerification.TranscriptHash, payload.PublicTranscriptHash) {
		return nil, errors.New("verified semantic evidence hashes differ from supplement")
	}
	return &envelope, nil
}

func resolveFinalSemanticSupplementStores(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, closure *finalSemanticOriginalClosure, dependencies finalSemanticSupplementDependencies) (map[int]server.BlobStore, error) {
	if dependencies.Stores != nil {
		if dependencies.VerifyRuntime == nil {
			return nil, errors.New("injected semantic supplement stores require an authenticated config verifier")
		}
		if err := dependencies.VerifyRuntime(cfg, stateRoot); err != nil {
			return nil, fmt.Errorf("authenticate semantic supplement BlobStore config: %w", err)
		}
		return finalSemanticSupplementStores(cfg, stateRoot, dependencies.Stores)
	}
	resolve := dependencies.ResolveCapturedStores
	if resolve == nil {
		return capturedFinalSemanticSupplementStores(ctx, cfg, stateRoot, runRoot, closure)
	}
	stores, err := resolve(ctx, cfg, stateRoot, runRoot)
	if err != nil {
		return nil, fmt.Errorf("authenticate captured semantic supplement BlobStore config: %w", err)
	}
	if err := validateFinalSemanticSupplementStores(cfg, stores); err != nil {
		return nil, fmt.Errorf("captured semantic supplement BlobStore set: %w", err)
	}
	return stores, nil
}

func lockFinalSemanticSupplement(ctx context.Context, lock *os.File) error {
	if ctx == nil || lock == nil {
		return errors.New("semantic supplement publication lock context is incomplete")
	}
	const retryInterval = 25 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return err
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func finalSemanticSupplementRoots(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runDir string, result *ScenarioResult) (string, string, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || roles == nil || result == nil || strings.TrimSpace(stateDir) == "" || strings.TrimSpace(runDir) == "" {
		return "", "", errors.New("semantic supplement context is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if result.RunID == "" || result.RunID != strings.TrimSpace(result.RunID) || strings.ContainsAny(result.RunID, "/\\\r\n\x00") {
		return "", "", errors.New("semantic supplement run identity is unsafe")
	}
	stateRoot, err := filepath.Abs(stateDir)
	if err != nil {
		return "", "", err
	}
	runRoot, err := filepath.Abs(runDir)
	if err != nil || runRoot != filepath.Join(stateRoot, "runs", result.RunID) || !pathWithinRoot(stateRoot, runRoot) {
		return "", "", errors.New("semantic supplement run directory does not match the result")
	}
	for label, root := range map[string]string{"state": stateRoot, "run": runRoot} {
		info, statErr := os.Lstat(root)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", stateMismatchError(statErr, "semantic supplement %s root is not a real directory", label)
		}
	}
	if err := rejectFinalArtifactSymlinkComponents(stateRoot, runRoot); err != nil {
		return "", "", err
	}
	return stateRoot, runRoot, nil
}

func authenticateFinalSemanticOriginalClosure(cfg *ResolvedConfig, roles *RoleSecrets, stateRoot, runRoot string, result *ScenarioResult) (*finalSemanticOriginalClosure, error) {
	if cfg == nil || cfg.Config == nil || roles == nil || result == nil || result.Result != "pass" || (result.Name != "release-1.0" && result.Name != "production-soak") || !validCanonicalHashHex(result.EvidenceHash) {
		return nil, errors.New("completed semantic scenario identity is invalid")
	}
	wantResultHash, err := canonicalScenarioResultHash(result)
	if err != nil || !strings.EqualFold(wantResultHash, result.EvidenceHash) {
		return nil, stateMismatchError(err, "completed semantic scenario result hash differs")
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return nil, errors.New("semantic supplement testnet owner role is missing")
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil || !strings.EqualFold(crypto.PubkeyToAddress(ownerKey.PublicKey).Hex(), owner.Address) {
		return nil, stateMismatchError(err, "semantic supplement owner identity is invalid")
	}
	var complete ReleaseEvidenceEnvelope
	if err := decodeFinalSemanticRegularJSON(filepath.Join(runRoot, "complete.json"), &complete); err != nil {
		return nil, fmt.Errorf("scenario complete: %w", err)
	}
	if err := verifyFinalSemanticOwnerEnvelope(cfg, &complete, &ownerKey.PublicKey, "scenario-complete", result.RunID); err != nil {
		return nil, fmt.Errorf("scenario complete: %w", err)
	}
	var completePayload scenarioCompletePayload
	if err := decodeStrictJSONBytes(complete.Payload, &completePayload); err != nil || !strings.EqualFold(completePayload.ResultHash, result.EvidenceHash) || !validSHA256ContentHash(completePayload.BundlePayloadHash) || !validSHA256ContentHash(completePayload.EvidenceManifestHash) || len(completePayload.Files) == 0 {
		return nil, stateMismatchError(err, "scenario complete payload is invalid")
	}
	for name, hash := range completePayload.Files {
		if err := validateCampaignEvidencePath(name); err != nil || !validSHA256ContentHash(hash) || isFinalSemanticPostCapturePath(name) {
			return nil, stateMismatchError(err, "scenario complete contains an invalid or post-capture path %q", name)
		}
	}
	var manifest ReleaseEvidenceEnvelope
	if err := decodeFinalSemanticRegularJSON(filepath.Join(runRoot, campaignEvidenceManifestFilename), &manifest); err != nil {
		return nil, fmt.Errorf("scenario evidence manifest: %w", err)
	}
	if err := verifyFinalSemanticOwnerEnvelope(cfg, &manifest, &ownerKey.PublicKey, campaignEvidenceManifestKind, result.RunID); err != nil || !strings.EqualFold(manifest.ContentHash, completePayload.EvidenceManifestHash) {
		return nil, stateMismatchError(err, "scenario evidence manifest envelope is invalid")
	}
	manifestPayload, err := decodeCampaignEvidenceManifest(&manifest)
	if err != nil || !strings.EqualFold(manifestPayload.ResultHash, result.EvidenceHash) || !strings.EqualFold(manifestPayload.BundlePayloadHash, completePayload.BundlePayloadHash) {
		return nil, stateMismatchError(err, "scenario evidence manifest payload is invalid")
	}
	manifestFiles, err := campaignEvidenceManifestFiles(manifestPayload.Files)
	if err != nil || !stringMapsEqual(manifestFiles, completePayload.Files) {
		return nil, stateMismatchError(err, "scenario evidence manifest files differ from completion")
	}
	authenticatedRaw := make(map[string][]byte, len(manifestPayload.Files))
	for _, entry := range manifestPayload.Files {
		raw, err := readCampaignEvidenceRegularFile(runRoot, entry.Path)
		if err != nil || uint64(len(raw)) != entry.Size || !strings.EqualFold(bytesSHA256(raw), entry.ContentHash) {
			return nil, stateMismatchError(err, "original scenario file %q differs from its owner-signed manifest", entry.Path)
		}
		authenticatedRaw[entry.Path] = raw
	}
	resultRaw, ok := authenticatedRaw["result.json"]
	if !ok {
		return nil, errors.New("scenario completion does not contain result.json")
	}
	var archivedResult ScenarioResult
	if err := decodeStrictJSONBytes(resultRaw, &archivedResult); err != nil || !finalJSONEqual(archivedResult, *result) {
		return nil, stateMismatchError(err, "scenario result differs from the owner-signed archive")
	}
	for _, required := range []string{"final-inputs/manifest.json", finalSemanticCaptureStatusFilename} {
		if _, ok := authenticatedRaw[required]; !ok {
			return nil, fmt.Errorf("scenario completion does not contain %s", required)
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		role, ok := roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)]
		if !ok {
			return nil, fmt.Errorf("semantic supplement operator %d artifact role is missing", operator)
		}
		key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
		if err != nil {
			return nil, err
		}
		var commit ReleaseEvidenceEnvelope
		commitPath := filepath.Join(runRoot, fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator))
		if err := decodeFinalSemanticRegularJSON(commitPath, &commit); err != nil {
			return nil, fmt.Errorf("operator %d scenario completion commit: %w", operator, err)
		}
		if err := verifyFinalSemanticOwnerEnvelope(cfg, &commit, &key.PublicKey, "scenario-complete-commit", result.RunID); err != nil {
			return nil, fmt.Errorf("operator %d scenario completion commit: %w", operator, err)
		}
		var nested ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(commit.Payload, &nested); err != nil || verifyEvidence(&nested, &ownerKey.PublicKey) != nil || !strings.EqualFold(nested.ContentHash, complete.ContentHash) || nested.Signature != complete.Signature || !finalJSONEqual(nested, complete) {
			return nil, stateMismatchError(err, "operator %d scenario completion commit binds another owner envelope", operator)
		}
	}
	var collected FinalSemanticCollectedInputs
	if err := decodeStrictJSONBytes(authenticatedRaw["final-inputs/manifest.json"], &collected); err != nil {
		return nil, err
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, &collected); err != nil {
		return nil, fmt.Errorf("verify owner-authenticated collected inputs: %w", err)
	}
	var capture FinalSemanticCaptureStatus
	if err := decodeStrictJSONBytes(authenticatedRaw[finalSemanticCaptureStatusFilename], &capture); err != nil {
		return nil, err
	}
	manifestRaw := authenticatedRaw["final-inputs/manifest.json"]
	if err := verifyFinalSemanticCaptureStatus(&capture, &collected); err != nil || capture.Phase != result.Name || capture.RunID != result.RunID || !strings.EqualFold(capture.ResultHash, result.EvidenceHash) || capture.ClosedAt != result.CompletedAt || capture.CollectedInputsManifest.URI != "final-inputs/manifest.json" || capture.CollectedInputsManifest.SizeBytes != uint64(len(manifestRaw)) || !strings.EqualFold(capture.CollectedInputsManifest.ContentHash, bytesSHA256(manifestRaw)) {
		return nil, stateMismatchError(err, "owner-authenticated semantic capture status does not bind the scenario and collected inputs")
	}
	return &finalSemanticOriginalClosure{
		runRoot: runRoot, complete: &complete, completePayload: completePayload,
		manifest: &manifest, manifestPayload: manifestPayload, collected: &collected, capture: &capture,
		owner: owner, ownerPublicKey: &ownerKey.PublicKey, authenticatedRaw: authenticatedRaw,
	}, nil
}

func ensureFinalSemanticSupplementOutputs(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateRoot, runRoot string, result *ScenarioResult, closure *finalSemanticOriginalClosure, load FinalArtifactLoader, newReader FinalSemanticChainReaderFactory) error {
	return recoverOrGenerateFinalSemanticOutputPair(ctx, stateRoot, runRoot, result.RunID, func() error {
		terminalRaw, err := loadCheckedFinalSemanticArtifact(ctx, load, closure.collected.TerminalObservation)
		if err != nil {
			return fmt.Errorf("load captured terminal observation: %w", err)
		}
		var terminal ScenarioObservation
		if err := decodeStrictJSONBytes(terminalRaw, &terminal); err != nil {
			return err
		}
		historyRaw, err := loadCheckedFinalSemanticArtifact(ctx, load, closure.collected.ObservationHistory)
		if err != nil {
			return fmt.Errorf("load captured observation history: %w", err)
		}
		var history []*ScenarioObservation
		if err := decodeStrictJSONBytes(historyRaw, &history); err != nil || len(history) == 0 {
			return stateMismatchError(err, "captured observation history is invalid")
		}
		archive, err := openAuthenticatedFinalSemanticArchive(ctx, cfg, stateRoot, runRoot, closure)
		if err != nil {
			return fmt.Errorf("open owner-authenticated final semantic archive: %w", err)
		}
		source, err := buildFinalSemanticSourceFromArchive(ctx, cfg, archive, result, &terminal, history)
		if err != nil {
			return fmt.Errorf("build asynchronous final semantic source: %w", err)
		}
		if err := validateScenarioFinalSemanticSource(cfg, roles, result, source); err != nil {
			return err
		}
		if newReader == nil {
			newReader, err = finalSemanticReaderFactoryFromCapturedFiles(cfg, archive.files)
			if err != nil {
				return err
			}
		}
		scan := NewFinalSemanticSecretScanner(roles, cfg.WalletSecret, cfg.WalletMaterial, cfg.WalletPasswordSecret, cfg.WalletPassword)
		if _, err := ProduceFinalSemanticOutputs(ctx, runRoot, *source, load, newReader, scan); err != nil {
			return fmt.Errorf("produce asynchronous final semantic outputs: %w", err)
		}
		return nil
	})
}

func recoverOrGenerateFinalSemanticOutputPair(ctx context.Context, stateRoot, runRoot, runID string, regenerate func() error) error {
	if ctx == nil || regenerate == nil {
		return errors.New("final semantic output recovery context is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	evidenceExists, err := finalSemanticRegularFileExists(filepath.Join(runRoot, finalSemanticEvidenceFilename))
	if err != nil {
		return err
	}
	markdownExists, err := finalSemanticRegularFileExists(filepath.Join(runRoot, finalSemanticMarkdownFilename))
	if err != nil {
		return err
	}
	if evidenceExists && markdownExists {
		return nil
	}
	if evidenceExists != markdownExists {
		partial := filepath.Join(runRoot, finalSemanticEvidenceFilename)
		if markdownExists {
			partial = filepath.Join(runRoot, finalSemanticMarkdownFilename)
		}
		if err := quarantineFinalSemanticPartialOutput(ctx, stateRoot, runRoot, runID, partial); err != nil {
			return fmt.Errorf("preserve partial final semantic output: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return regenerate()
}

// quarantineFinalSemanticPartialOutput preserves a crash-left byte stream but
// removes it from the authoritative output path. Regeneration never consumes
// this unauthenticated loose file; it starts again from the closed capture.
func quarantineFinalSemanticPartialOutput(ctx context.Context, stateRoot, runRoot, runID, partialPath string) error {
	if ctx == nil {
		return errors.New("partial semantic output recovery context is missing")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name := filepath.Base(partialPath)
	if name != finalSemanticEvidenceFilename && name != finalSemanticMarkdownFilename {
		return errors.New("partial semantic output recovery path is invalid")
	}
	if filepath.Dir(partialPath) != runRoot {
		return errors.New("partial semantic output is outside the run directory")
	}
	raw, err := readCampaignEvidenceRegularFile(runRoot, name)
	if err != nil {
		return err
	}
	recoveryRoot := filepath.Join(finalSemanticSupplementStageRoot(stateRoot, runID), "recovery")
	if err := os.MkdirAll(recoveryRoot, 0o700); err != nil {
		return err
	}
	if err := rejectFinalArtifactSymlinkComponents(stateRoot, recoveryRoot); err != nil {
		return err
	}
	digest := strings.TrimPrefix(bytesSHA256(raw), "sha256:")
	target := filepath.Join(recoveryRoot, name+"."+digest+".partial")
	if err := writeImmutableEvidenceArchive(target, raw); err != nil {
		return err
	}
	existing, readErr := readCampaignEvidenceRegularFile(recoveryRoot, filepath.Base(target))
	if readErr != nil || !bytes.Equal(existing, raw) {
		return stateMismatchError(readErr, "partial semantic recovery record differs at %s", target)
	}
	// Persist the recovery copy before removing the authoritative loose name.
	// A host loss at any later point therefore cannot discard the only copy.
	if err := syncFinalSemanticDirectory(recoveryRoot); err != nil {
		return err
	}
	current, err := readCampaignEvidenceRegularFile(runRoot, name)
	if err != nil || !bytes.Equal(current, raw) {
		return stateMismatchError(err, "partial semantic output changed while being preserved")
	}
	if err := os.Remove(partialPath); err != nil {
		return err
	}
	return syncFinalSemanticDirectory(runRoot)
}

func syncFinalSemanticDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}

// openAuthenticatedFinalSemanticArchive authenticates the closed graph before
// any reader or publication dependency is derived from it. It must not consult
// stateRoot/public.json: production preparation may legitimately advance that
// mutable pointer while the prior release analyzer is still running.
func openAuthenticatedFinalSemanticArchive(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, closure *finalSemanticOriginalClosure) (*finalSemanticArchive, error) {
	if closure == nil || closure.collected == nil {
		return nil, errors.New("owner-authenticated final semantic closure is missing")
	}
	authenticatedManifest, ok := closure.authenticatedRaw["final-inputs/manifest.json"]
	if !ok {
		return nil, errors.New("owner-authenticated collected-input manifest is missing")
	}
	current, err := readCampaignEvidenceRegularFile(runRoot, "final-inputs/manifest.json")
	if err != nil || !bytes.Equal(current, authenticatedManifest) {
		return nil, stateMismatchError(err, "collected-input manifest differs from the owner-authenticated closure")
	}
	archive, err := openFinalSemanticArchive(ctx, cfg, stateRoot, runRoot)
	if err != nil {
		return nil, err
	}
	if archive.collected == nil || !finalJSONEqual(*archive.collected, *closure.collected) {
		return nil, errors.New("opened semantic archive differs from the owner-authenticated collected inputs")
	}
	current, err = readCampaignEvidenceRegularFile(runRoot, "final-inputs/manifest.json")
	if err != nil || !bytes.Equal(current, authenticatedManifest) {
		return nil, stateMismatchError(err, "collected-input manifest changed while opening the authenticated archive")
	}
	return archive, nil
}

func capturedFinalSemanticReaderFactory(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, closure *finalSemanticOriginalClosure) (FinalSemanticChainReaderFactory, error) {
	archive, err := openAuthenticatedFinalSemanticArchive(ctx, cfg, stateRoot, runRoot, closure)
	if err != nil {
		return nil, err
	}
	return finalSemanticReaderFactoryFromCapturedFiles(cfg, archive.files)
}

func finalSemanticReaderFactoryFromCapturedFiles(cfg *ResolvedConfig, files map[string][]byte) (FinalSemanticChainReaderFactory, error) {
	if cfg == nil || cfg.Config == nil || len(files) == 0 {
		return nil, errors.New("captured final semantic public manifest context is incomplete")
	}
	publicBytes, ok := files["launch-foundation/public.json"]
	if !ok {
		return nil, errors.New("closed semantic graph has no captured public manifest")
	}
	var public PublicDeploymentManifest
	if err := decodeStrictJSONBytes(publicBytes, &public); err != nil {
		return nil, fmt.Errorf("decode captured public manifest: %w", err)
	}
	if err := validatePublicManifestRevision(&public); err != nil {
		return nil, err
	}
	if err := validatePublishedRuntimeIdentity(&public, cfg); err != nil {
		return nil, fmt.Errorf("captured public manifest runtime identity: %w", err)
	}
	if public.DeploymentID != cfg.Config.Deployment.DeploymentID || public.ConfigHash != cfg.ConfigHash || !strings.EqualFold(public.PolicyHash, cfg.PolicyHash) || public.ChainID != cfg.ChainID || public.Netuid != cfg.Netuid || !strings.EqualFold(public.GenesisHash, cfg.Public.Chain.GenesisHash) || public.Topology.Operators != cfg.Config.Topology.Operators || public.Contracts == nil {
		return nil, errors.New("captured public manifest does not match the semantic configuration")
	}
	manifestHash, err := canonicalHashHex(&public)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(&public)
	if err != nil {
		return nil, err
	}
	_, signers := inspectPublicIdentityBytesForManifest(public.Identities, public.DeploymentID, public.Topology)
	if len(signers) != public.Topology.Operators || len(public.Operators) != public.Topology.Operators {
		return nil, errors.New("captured public manifest signer or operator directory is incomplete")
	}
	operators := make(map[int]PublicOperator, len(public.Operators))
	for _, operator := range public.Operators {
		if operator.NoID < 1 || operator.NoID > public.Topology.Operators || operator.APIURL == "" || operators[operator.NoID].NoID != 0 {
			return nil, errors.New("captured public manifest operator directory is invalid")
		}
		operators[operator.NoID] = operator
	}
	expectedHashes := make(map[int]string, public.Topology.Operators)
	for operator := 1; operator <= public.Topology.Operators; operator++ {
		exact, ok := files[fmt.Sprintf("public/deployment-manifest.operator-%d.evidence.json", operator)]
		if !ok {
			return nil, fmt.Errorf("captured operator %d deployment manifest is missing", operator)
		}
		envelope, err := validateArchivedDeploymentManifestEnvelope(exact, &public, payload, signers[operator])
		if err != nil {
			return nil, fmt.Errorf("captured operator %d deployment manifest: %w", operator, err)
		}
		expectedHashes[operator] = envelope.ContentHash
	}
	locatorBytes, ok := files["public/deployment-manifest.locators.json"]
	if !ok {
		return nil, errors.New("captured deployment-manifest locator directory is missing")
	}
	directory, err := validateDeploymentManifestLocatorDirectory(locatorBytes, &public, manifestHash, operators, expectedHashes)
	if err != nil {
		return nil, err
	}
	origins, err := finalOperatorEvidenceOrigins(directory)
	if err != nil {
		return nil, err
	}
	discoveryURI := ""
	for _, locator := range directory.Locators {
		if locator.OperatorNoID == 1 {
			discoveryURI = locator.URL
			break
		}
	}
	if discoveryURI == "" {
		return nil, errors.New("captured public manifest discovery URI is missing")
	}
	transport, err := finalSemanticRPCTransportForCapturedFiles(cfg, &public, files)
	if err != nil {
		return nil, fmt.Errorf("authenticate captured final semantic RPC transport: %w", err)
	}
	return func(readerCtx context.Context, evidence *FinalSemanticEvidence) (FinalSemanticChainReader, error) {
		return newPublicFinalSemanticChainReaderWithTransport(readerCtx, &public, evidence, discoveryURI, origins, transport)
	}, nil
}

// capturedFinalSemanticSupplementStores accepts current secret-bearing MinIO
// files only when their exact bytes match the runtime manifest frozen inside
// the completed capture. It builds each store from those already-hashed bytes,
// avoiding a check/use race and ignoring unrelated later runtime revisions.
func capturedFinalSemanticSupplementStores(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, closure *finalSemanticOriginalClosure) (map[int]server.BlobStore, error) {
	archive, err := openAuthenticatedFinalSemanticArchive(ctx, cfg, stateRoot, runRoot, closure)
	if err != nil {
		return nil, err
	}
	planBytes, _, err := archive.file("launch-foundation/plan.json")
	if err != nil {
		return nil, err
	}
	if err := verifyFinalSemanticCapturedPublicationPlan(cfg, planBytes); err != nil {
		return nil, err
	}
	var manifest RuntimeConfigManifest
	if err := archive.decode("launch-foundation/runtime-config-manifest.json", &manifest); err != nil {
		return nil, err
	}
	return finalSemanticStoresFromCapturedRuntimeManifest(cfg, stateRoot, &manifest)
}

func verifyFinalSemanticCapturedPublicationPlan(cfg *ResolvedConfig, planBytes []byte) error {
	if cfg == nil || cfg.Config == nil || len(planBytes) == 0 {
		return errors.New("captured semantic publication plan context is incomplete")
	}
	var plan SetupPlan
	if err := decodeStrictJSONBytes(planBytes, &plan); err != nil {
		return fmt.Errorf("decode captured semantic publication plan: %w", err)
	}
	if plan.Schema != currentSetupPlanSchema {
		return errors.New("captured semantic publication plan schema is invalid")
	}
	observedPlanHash, err := persistedSetupPlanHash(planBytes, plan.Schema)
	if err != nil || !strings.EqualFold(observedPlanHash, plan.PlanHash) {
		return stateMismatchError(err, "captured semantic publication plan hash is invalid")
	}
	wantResolvedInputs, err := resolvedInputsHash(cfg)
	if err != nil || plan.DeploymentID != cfg.Config.Deployment.DeploymentID || !strings.EqualFold(plan.ConfigHash, cfg.ConfigHash) || !strings.EqualFold(plan.PolicyHash, cfg.PolicyHash) || !strings.EqualFold(plan.ResolvedInputsHash, wantResolvedInputs) {
		return stateMismatchError(err, "captured plan does not bind the semantic publication endpoint inputs")
	}
	return nil
}

func finalSemanticStoresFromCapturedRuntimeManifest(cfg *ResolvedConfig, stateRoot string, manifest *RuntimeConfigManifest) (map[int]server.BlobStore, error) {
	if cfg == nil || cfg.Config == nil || manifest == nil {
		return nil, errors.New("captured semantic BlobStore context is incomplete")
	}
	wantManifestHash, err := runtimeConfigManifestHash(*manifest)
	if err != nil || manifest.Schema != runtimeConfigManifestSchema || manifest.DeploymentID != cfg.Config.Deployment.DeploymentID || manifest.ConfigHash != cfg.ConfigHash || !strings.EqualFold(manifest.PolicyHash, cfg.PolicyHash) || !strings.EqualFold(manifest.ManifestHash, wantManifestHash) {
		return nil, stateMismatchError(err, "captured runtime config manifest is invalid")
	}
	entries := make(map[string]RuntimeConfigFile, len(manifest.Files))
	previous := ""
	for index, file := range manifest.Files {
		if file.Path == "" || filepath.IsAbs(file.Path) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path))) != file.Path || !validSHA256ContentHash(file.SHA256) || file.Mode == "" || index > 0 && file.Path <= previous {
			return nil, errors.New("captured runtime config manifest file inventory is invalid")
		}
		entries[file.Path] = file
		previous = file.Path
	}
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		relative := filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("operator-%d", operator), "vault", "minio.yml"))
		entry, ok := entries[relative]
		if !ok || entry.Mode != "0600" {
			return nil, fmt.Errorf("captured runtime config manifest is missing exact %s", relative)
		}
		if err := validateRuntimeConfigPathAncestry(stateRoot, relative); err != nil {
			return nil, err
		}
		absolute := filepath.Join(stateRoot, filepath.FromSlash(relative))
		info, err := os.Lstat(absolute)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return nil, stateMismatchError(err, "operator %d current MinIO config is not the captured private regular file", operator)
		}
		raw, err := os.ReadFile(absolute)
		if err != nil || !strings.EqualFold(bytesSHA256(raw), entry.SHA256) {
			return nil, stateMismatchError(err, "operator %d current MinIO config differs from the captured digest", operator)
		}
		var rendered renderedOperatorBlobConfig
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		if err := decoder.Decode(&rendered); err != nil {
			return nil, fmt.Errorf("operator %d captured MinIO config: %w", operator, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err != nil {
				return nil, fmt.Errorf("operator %d captured MinIO config trailing YAML: %w", operator, err)
			}
			return nil, fmt.Errorf("operator %d captured MinIO config contains multiple YAML documents", operator)
		}
		wantPrefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			return nil, err
		}
		const objectStoreHostVariable = "{{ env:BRINGYOUR_MINIO_HOSTNAME }}"
		if strings.Contains(rendered.Authority, objectStoreHostVariable) && strings.TrimSpace(cfg.ObjectStoreHost) == "" {
			return nil, fmt.Errorf("operator %d captured MinIO host is unavailable", operator)
		}
		authority := strings.TrimSpace(strings.ReplaceAll(rendered.Authority, objectStoreHostVariable, cfg.ObjectStoreHost))
		if rendered.Prefix != wantPrefix || authority == "" || strings.EqualFold(authority, "local") || strings.Contains(authority, "{{") || rendered.Bucket != "blob" || rendered.AccessKey == "" || rendered.SecretKey == "" {
			return nil, fmt.Errorf("operator %d captured MinIO config is incomplete or has the wrong prefix", operator)
		}
		store, err := server.NewBlobStore(&server.BlobStoreConfig{Authority: authority, AccessKey: rendered.AccessKey, SecretKey: rendered.SecretKey, Bucket: rendered.Bucket, Tls: rendered.TLS, Prefix: rendered.Prefix})
		if err != nil {
			return nil, fmt.Errorf("operator %d captured semantic evidence store: %w", operator, err)
		}
		if store == nil || store.Prefix() != wantPrefix {
			return nil, fmt.Errorf("operator %d captured semantic evidence store prefix is invalid", operator)
		}
		stores[operator] = store
	}
	return stores, nil
}

func loadAndVerifyFinalSemanticRawFiles(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, runRoot string, result *ScenarioResult, load FinalArtifactLoader) ([]finalSemanticRawFile, *FinalSemanticEvidence, error) {
	files, err := enumerateFinalSemanticRawFiles(runRoot)
	if err != nil {
		return nil, nil, err
	}
	semantic, _, err := loadAndVerifyFinalSemanticBytes(ctx, cfg, roles, result, files, load)
	if err != nil {
		return nil, nil, fmt.Errorf("verify semantic supplement output graph: %w", err)
	}
	return files, semantic, nil
}

func enumerateFinalSemanticRawFiles(runRoot string) ([]finalSemanticRawFile, error) {
	return enumerateFinalSemanticRawFilesWithPair(runRoot, true)
}

func enumeratePresentFinalSemanticRawFiles(runRoot string) ([]finalSemanticRawFile, error) {
	return enumerateFinalSemanticRawFilesWithPair(runRoot, false)
}

func enumerateFinalSemanticRawFilesWithPair(runRoot string, requirePair bool) ([]finalSemanticRawFile, error) {
	paths := make([]string, 0, 2)
	for _, name := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		exists, err := finalSemanticRegularFileExists(filepath.Join(runRoot, name))
		if err != nil {
			return nil, err
		}
		if exists {
			paths = append(paths, name)
		} else if requirePair {
			return nil, fmt.Errorf("final semantic output is missing: %s", name)
		}
	}
	derivedRoot := filepath.Join(runRoot, "final-derived")
	if info, err := os.Lstat(derivedRoot); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("final-derived is not a real directory")
		}
		if err := filepath.WalkDir(derivedRoot, func(candidate string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if candidate == derivedRoot {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("derived semantic path %s is a symlink", candidate)
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return stateMismatchError(err, "derived semantic path %s is not a regular file", candidate)
			}
			relative, err := filepath.Rel(runRoot, candidate)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(relative))
			return nil
		}); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) > maximumCampaignEvidenceObjects {
		return nil, fmt.Errorf("semantic supplement has %d files, maximum %d", len(paths), maximumCampaignEvidenceObjects)
	}
	result := make([]finalSemanticRawFile, 0, len(paths))
	var aggregate uint64
	for _, name := range paths {
		if err := validateFinalSemanticPostCapturePath(name); err != nil {
			return nil, err
		}
		raw, err := readCampaignEvidenceRegularFile(runRoot, name)
		if err != nil {
			return nil, fmt.Errorf("read semantic output %s: %w", name, err)
		}
		if len(raw) == 0 || uint64(len(raw)) > maximumCampaignEvidenceAggregateBytes-aggregate {
			return nil, fmt.Errorf("semantic supplement files are empty or exceed %d aggregate bytes", maximumCampaignEvidenceAggregateBytes)
		}
		aggregate += uint64(len(raw))
		result = append(result, finalSemanticRawFile{Path: name, ContentHash: bytesSHA256(raw), Data: raw})
	}
	return result, nil
}

func prepareFinalSemanticSupplementFiles(cfg *ResolvedConfig, stateRoot, runID string, owner EVMRoleSecret, files []finalSemanticRawFile) ([]*ReleaseEvidenceEnvelope, []FinalSemanticSupplementFile, error) {
	stageRoot := finalSemanticSupplementStageRoot(stateRoot, runID)
	envelopes := make([]*ReleaseEvidenceEnvelope, 0, len(files))
	entries := make([]FinalSemanticSupplementFile, 0, len(files))
	for _, raw := range files {
		payload := finalSemanticSupplementFilePayload{Schema: finalSemanticSupplementFileSchema, RunID: runID, Path: raw.Path, ContentHash: raw.ContentHash, Size: uint64(len(raw.Data)), Data: raw.Data}
		pathHash := sha256.Sum256([]byte(raw.Path))
		localPath := filepath.Join(stageRoot, "files", hex.EncodeToString(pathHash[:])+".evidence.json")
		envelope, _, err := prepareFinalSemanticLocalEvidence(cfg, stateRoot, localPath, finalSemanticSupplementFileKind, runID, payload, owner)
		if err != nil {
			return nil, nil, fmt.Errorf("prepare semantic supplement file %s: %w", raw.Path, err)
		}
		envelopes = append(envelopes, envelope)
		entries = append(entries, FinalSemanticSupplementFile{Path: raw.Path, ContentHash: raw.ContentHash, Size: uint64(len(raw.Data)), EnvelopeHash: envelope.ContentHash})
	}
	return envelopes, entries, nil
}

func verifyFinalSemanticSupplementEnvelope(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, result *ScenarioResult, closure *finalSemanticOriginalClosure, envelope *ReleaseEvidenceEnvelope, stateRoot string, load FinalArtifactLoader) (*FinalSemanticSupplementPayload, []finalSemanticRawFile, error) {
	payload, files, err := verifyFinalSemanticSupplementBinding(cfg, result, closure, envelope, stateRoot)
	if err != nil {
		return nil, nil, err
	}
	byPath := make(map[string][]byte, len(files))
	for _, file := range files {
		byPath[file.Path] = file.Data
	}
	var semantic FinalSemanticEvidence
	if err := decodeStrictJSONBytes(byPath[finalSemanticEvidenceFilename], &semantic); err != nil || semantic.PublicVerification == nil || !strings.EqualFold(semantic.EvidenceHash, payload.SemanticEvidenceHash) || !strings.EqualFold(semantic.PublicVerification.TranscriptHash, payload.PublicTranscriptHash) {
		return nil, nil, stateMismatchError(err, "semantic supplement object hashes differ")
	}
	verified, _, err := loadAndVerifyFinalSemanticBytes(ctx, cfg, roles, result, files, load)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(verified.EvidenceHash, payload.SemanticEvidenceHash) {
		return nil, nil, errors.New("verified semantic evidence hash differs from supplement")
	}
	return payload, files, nil
}

func verifyFinalSemanticSupplementBinding(cfg *ResolvedConfig, result *ScenarioResult, closure *finalSemanticOriginalClosure, envelope *ReleaseEvidenceEnvelope, stateRoot string) (*FinalSemanticSupplementPayload, []finalSemanticRawFile, error) {
	if closure == nil || closure.ownerPublicKey == nil {
		return nil, nil, errors.New("semantic supplement closure is missing")
	}
	if err := verifyFinalSemanticOwnerEnvelope(cfg, envelope, closure.ownerPublicKey, finalSemanticSupplementKind, result.RunID); err != nil {
		return nil, nil, err
	}
	var payload FinalSemanticSupplementPayload
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return nil, nil, err
	}
	if payload.Schema != finalSemanticSupplementSchema || payload.Status != finalSemanticSupplementStatus || payload.Phase != result.Name || payload.RunID != result.RunID || !strings.EqualFold(payload.ResultHash, result.EvidenceHash) || !strings.EqualFold(payload.ScenarioCompleteHash, closure.complete.ContentHash) || !strings.EqualFold(payload.ScenarioEvidenceManifestHash, closure.manifest.ContentHash) || !strings.EqualFold(payload.CaptureStatusHash, closure.capture.EvidenceHash) || !strings.EqualFold(payload.CollectedInputsHash, closure.collected.EvidenceHash) || !validCanonicalHashHex(payload.SemanticEvidenceHash) || !validCanonicalHashHex(payload.PublicTranscriptHash) {
		return nil, nil, errors.New("semantic supplement payload does not bind the completed capture")
	}
	files, err := loadFinalSemanticSupplementFiles(cfg, closure, &payload, stateRoot)
	if err != nil {
		return nil, nil, err
	}
	return &payload, files, nil
}

func loadAndVerifyFinalSemanticBytes(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, result *ScenarioResult, files []finalSemanticRawFile, load FinalArtifactLoader) (*FinalSemanticEvidence, map[string][]byte, error) {
	byPath := make(map[string][]byte, len(files))
	for _, file := range files {
		byPath[file.Path] = file.Data
	}
	var semantic FinalSemanticEvidence
	if err := decodeStrictJSONBytes(byPath[finalSemanticEvidenceFilename], &semantic); err != nil {
		return nil, nil, err
	}
	if err := VerifyFinalSemanticEvidence(&semantic); err != nil || semantic.PublicVerification == nil {
		return nil, nil, stateMismatchError(err, "signed final semantic evidence is invalid")
	}
	if err := validateScenarioFinalSemanticSource(cfg, roles, result, &semantic); err != nil {
		return nil, nil, err
	}
	canonicalJSON, err := json.MarshalIndent(&semantic, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if canonicalJSON = append(canonicalJSON, '\n'); !bytes.Equal(canonicalJSON, byPath[finalSemanticEvidenceFilename]) {
		return nil, nil, errors.New("signed final semantic evidence bytes are not canonical")
	}
	markdown, err := RenderFinalSemanticEvidenceMarkdown(&semantic)
	if err != nil || !bytes.Equal(markdown, byPath[finalSemanticMarkdownFilename]) {
		return nil, nil, stateMismatchError(err, "signed FINAL.md differs from semantic evidence")
	}
	encoded, err := json.Marshal(&semantic)
	if err != nil {
		return nil, nil, err
	}
	references := map[string]campaignArtifactReference{}
	if err := collectCampaignArtifactReferences(encoded, references, 0); err != nil {
		return nil, nil, err
	}
	wantDerived := map[string]campaignArtifactReference{}
	for uri, reference := range references {
		if strings.HasPrefix(uri, "final-derived/") {
			wantDerived[uri] = reference
		}
	}
	for path, reference := range wantDerived {
		data, ok := byPath[path]
		if !ok || uint64(len(data)) != reference.Size || !strings.EqualFold(bytesSHA256(data), reference.ContentHash) {
			return nil, nil, fmt.Errorf("signed derived artifact %s differs from semantic locator", path)
		}
	}
	for path := range byPath {
		if strings.HasPrefix(path, "final-derived/") {
			if _, ok := wantDerived[path]; !ok {
				return nil, nil, fmt.Errorf("signed semantic supplement contains unreferenced derived file %s", path)
			}
		}
	}
	signedLoad := func(loadCtx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		if data, ok := byPath[locator.URI]; ok {
			return append([]byte(nil), data...), nil
		}
		return load(loadCtx, locator)
	}
	if err := VerifyFinalSemanticArtifacts(ctx, &semantic, signedLoad); err != nil {
		return nil, nil, err
	}
	return &semantic, byPath, nil
}

func loadFinalSemanticSupplementFiles(cfg *ResolvedConfig, closure *finalSemanticOriginalClosure, payload *FinalSemanticSupplementPayload, stateRoot string) ([]finalSemanticRawFile, error) {
	envelopes, err := loadFinalSemanticSupplementFileEnvelopes(cfg, closure, payload, stateRoot)
	if err != nil {
		return nil, err
	}
	result := make([]finalSemanticRawFile, 0, len(envelopes))
	for index, envelope := range envelopes {
		var filePayload finalSemanticSupplementFilePayload
		if err := decodeStrictJSONBytes(envelope.Payload, &filePayload); err != nil {
			return nil, err
		}
		entry := payload.Files[index]
		if filePayload.Schema != finalSemanticSupplementFileSchema || filePayload.RunID != payload.RunID || filePayload.Path != entry.Path || filePayload.ContentHash != entry.ContentHash || filePayload.Size != entry.Size || uint64(len(filePayload.Data)) != entry.Size || !strings.EqualFold(bytesSHA256(filePayload.Data), entry.ContentHash) {
			return nil, fmt.Errorf("semantic supplement file %s payload differs", entry.Path)
		}
		result = append(result, finalSemanticRawFile{Path: entry.Path, ContentHash: strings.ToLower(entry.ContentHash), Data: append([]byte(nil), filePayload.Data...)})
	}
	return result, nil
}

func loadFinalSemanticSupplementFileEnvelopes(cfg *ResolvedConfig, closure *finalSemanticOriginalClosure, payload *FinalSemanticSupplementPayload, stateRoot string) ([]*ReleaseEvidenceEnvelope, error) {
	if cfg == nil || closure == nil || payload == nil || len(payload.Files) < 2 || len(payload.Files) > maximumCampaignEvidenceObjects {
		return nil, errors.New("semantic supplement file manifest is incomplete")
	}
	seenEvidence, seenMarkdown := false, false
	previous := ""
	var aggregate uint64
	result := make([]*ReleaseEvidenceEnvelope, 0, len(payload.Files))
	for index, entry := range payload.Files {
		if err := validateFinalSemanticPostCapturePath(entry.Path); err != nil || (index > 0 && entry.Path <= previous) || entry.Size == 0 || entry.Size > maximumCampaignEvidenceRawFileBytes || !validSHA256ContentHash(entry.ContentHash) || !validSHA256ContentHash(entry.EnvelopeHash) || entry.Size > maximumCampaignEvidenceAggregateBytes-aggregate {
			return nil, stateMismatchError(err, "semantic supplement file manifest is invalid at %q", entry.Path)
		}
		aggregate += entry.Size
		seenEvidence = seenEvidence || entry.Path == finalSemanticEvidenceFilename
		seenMarkdown = seenMarkdown || entry.Path == finalSemanticMarkdownFilename
		pathHash := sha256.Sum256([]byte(entry.Path))
		path := filepath.Join(finalSemanticSupplementStageRoot(stateRoot, payload.RunID), "files", hex.EncodeToString(pathHash[:])+".evidence.json")
		var envelope ReleaseEvidenceEnvelope
		if err := decodeFinalSemanticRegularJSON(path, &envelope); err != nil {
			return nil, fmt.Errorf("read staged semantic file %s: %w", entry.Path, err)
		}
		if err := verifyFinalSemanticOwnerEnvelope(cfg, &envelope, closure.ownerPublicKey, finalSemanticSupplementFileKind, payload.RunID); err != nil || !strings.EqualFold(envelope.ContentHash, entry.EnvelopeHash) {
			return nil, stateMismatchError(err, "staged semantic file %s envelope is invalid", entry.Path)
		}
		result = append(result, &envelope)
		previous = entry.Path
	}
	if !seenEvidence || !seenMarkdown {
		return nil, errors.New("semantic supplement omits its evidence or markdown output")
	}
	return result, nil
}

func finalSemanticSupplementStores(cfg *ResolvedConfig, stateRoot string, factory scenarioCompletionStoreFactory) (map[int]server.BlobStore, error) {
	if factory == nil {
		factory = func(operator int) (server.BlobStore, error) {
			return renderedOperatorEvidenceStore(cfg, stateRoot, operator)
		}
	}
	stores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		store, err := factory(operator)
		if err != nil {
			return nil, fmt.Errorf("operator %d semantic supplement store: %w", operator, err)
		}
		wantPrefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			return nil, err
		}
		if store == nil || store.Prefix() != wantPrefix {
			return nil, fmt.Errorf("operator %d semantic supplement store prefix is invalid", operator)
		}
		stores[operator] = store
	}
	if err := validateFinalSemanticSupplementStores(cfg, stores); err != nil {
		return nil, err
	}
	return stores, nil
}

func validateFinalSemanticSupplementStores(cfg *ResolvedConfig, stores map[int]server.BlobStore) error {
	if cfg == nil || cfg.Config == nil || len(stores) != cfg.Config.Topology.Operators {
		return errors.New("semantic supplement store census differs from operator topology")
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		store, ok := stores[operator]
		wantPrefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			return err
		}
		if !ok || store == nil || store.Prefix() != wantPrefix {
			return fmt.Errorf("operator %d semantic supplement store prefix is invalid", operator)
		}
	}
	return nil
}

func publishAndReadBackFinalSemanticEnvelope(ctx context.Context, store server.BlobStore, envelope *ReleaseEvidenceEnvelope) error {
	serverEnvelope, err := startifactEvidenceEnvelope(envelope)
	if err != nil {
		return err
	}
	published, err := startifact.PublishEvidence(ctx, store, serverEnvelope)
	if err != nil {
		return err
	}
	return verifyDirectEvidencePublication(ctx, store, serverEnvelope, published)
}

func readBackFinalSemanticEnvelope(ctx context.Context, store server.BlobStore, envelope *ReleaseEvidenceEnvelope) error {
	serverEnvelope, err := startifactEvidenceEnvelope(envelope)
	if err != nil {
		return err
	}
	contentKey, err := startifact.EvidenceContentKey(store, serverEnvelope.ContentHash)
	if err != nil {
		return err
	}
	hashHex := strings.TrimPrefix(strings.ToLower(serverEnvelope.ContentHash), "sha256:")
	historyKey := filepath.ToSlash(filepath.Join(store.Prefix(), "st", "v1", "evidence", "history", serverEnvelope.DeploymentID, fmt.Sprint(serverEnvelope.Netuid), serverEnvelope.Kind, serverEnvelope.RunID, hashHex+".json"))
	return verifyDirectEvidencePublication(ctx, store, serverEnvelope, &startifact.Published{ContentHash: serverEnvelope.ContentHash, ContentKey: contentKey, HistoryKey: historyKey, Bucket: store.Bucket()})
}

func verifyFinalSemanticOwnerEnvelope(cfg *ResolvedConfig, envelope *ReleaseEvidenceEnvelope, signer *ecdsa.PublicKey, kind, runID string) error {
	if cfg == nil || cfg.Config == nil || envelope == nil || signer == nil {
		return errors.New("semantic evidence envelope context is incomplete")
	}
	if err := verifyEvidence(envelope, signer); err != nil {
		return err
	}
	created, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil || envelope.CreatedAt != created.UTC().Format(time.RFC3339Nano) {
		return errors.New("semantic evidence envelope time is not canonical UTC")
	}
	if envelope.Kind != kind || envelope.RunID != runID || envelope.DeploymentID != cfg.Config.Deployment.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return errors.New("semantic evidence envelope deployment identity differs")
	}
	return nil
}

func prepareFinalSemanticLocalEvidence(cfg *ResolvedConfig, stateRoot, localPath, kind, runID string, payload any, owner EVMRoleSecret) (*ReleaseEvidenceEnvelope, []byte, error) {
	if info, err := os.Lstat(localPath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("semantic supplement local evidence is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	directory := filepath.Dir(localPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, nil, err
	}
	if err := rejectFinalArtifactSymlinkComponents(stateRoot, directory); err != nil {
		return nil, nil, err
	}
	envelope, encoded, err := prepareLocalEvidence(cfg, stateRoot, localPath, kind, runID, payload, owner, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := requireFinalSemanticRegularFile(localPath); err != nil {
		return nil, nil, err
	}
	return envelope, encoded, nil
}

func writeFinalSemanticImmutableLocal(path string, exact []byte) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("semantic supplement commit is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeImmutableEvidenceArchive(path, exact)
}

func decodeFinalSemanticRegularJSON(path string, value any) error {
	if err := requireFinalSemanticRegularFile(path); err != nil {
		return err
	}
	return decodeStrictJSONFile(path, value)
}

func requireFinalSemanticRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > maximumCampaignEvidenceEnvelopeBytes {
		return fmt.Errorf("%s is not a bounded regular file", path)
	}
	return nil
}

func finalSemanticRegularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("final semantic output %s is not a regular file", filepath.Base(path))
	}
	return true, nil
}

func loadCheckedFinalSemanticArtifact(ctx context.Context, load FinalArtifactLoader, locator FinalArtifactLocator) ([]byte, error) {
	if load == nil {
		return nil, errors.New("semantic artifact loader is missing")
	}
	data, err := load(ctx, locator)
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) != locator.SizeBytes || !strings.EqualFold(bytesSHA256(data), locator.ContentHash) {
		return nil, errors.New("semantic artifact bytes differ from their locator")
	}
	return data, nil
}

func validateFinalSemanticPostCapturePath(name string) error {
	if err := validateCampaignEvidencePath(name); err != nil {
		return err
	}
	if !isFinalSemanticPostCapturePath(name) || name == finalSemanticSupplementFilename || strings.HasPrefix(name, finalSemanticStagePrefix) {
		return fmt.Errorf("semantic supplement path %q is outside the output graph", name)
	}
	return nil
}

func isFinalSemanticPostCapturePath(name string) bool {
	return name == finalSemanticEvidenceFilename || name == finalSemanticMarkdownFilename || name == finalSemanticSupplementFilename || strings.HasPrefix(name, "final-derived/") || isFinalSemanticPrivateStagePath(name)
}

func isFinalSemanticPrivateStagePath(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], finalSemanticStagePrefix) || len(parts[0]) == len(finalSemanticStagePrefix) {
		return false
	}
	return parts[1] == finalSemanticEvidenceFilename || parts[1] == finalSemanticMarkdownFilename
}

func finalSemanticSupplementStageRoot(stateRoot, runID string) string {
	return filepath.Join(stateRoot, "public", finalSemanticSupplementArchiveDir, runID)
}

func finalSemanticRawFilesEqual(left, right []finalSemanticRawFile) bool {
	return finalSemanticRawFileMismatch(left, right) == ""
}

func finalSemanticOriginalClosuresEqual(left, right *finalSemanticOriginalClosure) bool {
	if left == nil || right == nil || left.complete == nil || right.complete == nil || left.manifest == nil || right.manifest == nil || left.collected == nil || right.collected == nil || left.capture == nil || right.capture == nil {
		return false
	}
	if !finalJSONEqual(*left.complete, *right.complete) || !finalJSONEqual(*left.manifest, *right.manifest) || !finalJSONEqual(*left.collected, *right.collected) || !finalJSONEqual(*left.capture, *right.capture) || len(left.authenticatedRaw) != len(right.authenticatedRaw) {
		return false
	}
	for path, raw := range left.authenticatedRaw {
		if !bytes.Equal(raw, right.authenticatedRaw[path]) {
			return false
		}
	}
	return true
}

func finalSemanticRawFileMismatch(left, right []finalSemanticRawFile) string {
	if len(left) != len(right) {
		return "file inventory"
	}
	for index := range left {
		if left[index].Path != right[index].Path || left[index].ContentHash != right[index].ContentHash || !bytes.Equal(left[index].Data, right[index].Data) {
			if left[index].Path == right[index].Path {
				return left[index].Path
			}
			return fmt.Sprintf("file inventory at %s/%s", left[index].Path, right[index].Path)
		}
	}
	return ""
}

func finalSemanticLooseRawFileMismatch(signed, loose []finalSemanticRawFile) string {
	byPath := make(map[string]finalSemanticRawFile, len(signed))
	for _, file := range signed {
		byPath[file.Path] = file
	}
	for _, file := range loose {
		expected, ok := byPath[file.Path]
		if !ok || expected.ContentHash != file.ContentHash || !bytes.Equal(expected.Data, file.Data) {
			return file.Path
		}
	}
	return ""
}
