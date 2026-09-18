// Exact typed metadata boundaries exercise real writers, descriptor readers,
// signed carriers and both original public replicas. No source proof is waived.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026/startifact"
)

// Use the checked-in finite population with visibly synthetic public origins.
func campaignMetadataConfigTestV2(t *testing.T) *ResolvedConfig {
	t.Helper()
	cfg := runtimeEvidenceLaunchConfigTest(t)
	cfg.OperatorAPIOrigins = []string{"https://no1.example", "https://no2.example"}
	return cfg
}

// All geometry is derived from the real configured census, separately from
// raw tape storage and the private capture allowance before client rejection.
func TestCampaignEvidenceCapacityV2MetadataDerivesIndependentFullProfile(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if limits.maximumObjects != 1191936 || cfg.Config.Topology.Miners != 1000 || cfg.Config.Topology.Validators != 2 || cfg.Config.Topology.Operators != 2 {
		t.Fatal("full configured census changed")
	}
	for _, source := range cfg.Config.ValidatorEvidenceV2 {
		if source.Evidence.Bounds.CaptureFileLimit() != 700000 {
			t.Fatal("private pre-rejection capture count changed")
		}
	}
	metadata := limits.metadata
	for _, size := range []uint64{metadata.indexBytes, metadata.manifestBytes, metadata.completionBytes, metadata.graphBytes} {
		if size <= maximumCampaignEvidenceEnvelopeBytes || size > maximumCampaignMetadataDocumentV2 {
			t.Fatalf("full census has no finite metadata document owner: %d", size)
		}
	}
	if metadata.retainedBytes > maximumCampaignRetainedMetadataV2 || metadata.supplementBytes > maximumCampaignMetadataDocumentV2 || metadata.supplementBytes >= metadata.retainedBytes {
		t.Fatal("separate capture and supplement ownership changed")
	}
	if limits.rawFileBytes("ordinary.bin") != maximumCampaignEvidenceRawFileBytes || defaultCampaignEvidenceLimits().maximumBytes != maximumCampaignEvidenceAggregateBytes {
		t.Fatal("metadata changed ordinary source limits")
	}
	for _, mutate := range []func(*ResolvedConfig){
		func(value *ResolvedConfig) {
			value.OperatorAPIOrigins[0] = "https://" + strings.Repeat("x", 4096) + ".example"
		},
		func(value *ResolvedConfig) {
			value.Config.ValidatorEvidenceV2[0].Evidence.Bounds.Disk.MaxStorageFiles = ^uint64(0)
		},
	} {
		value := campaignMetadataConfigTestV2(t)
		mutate(value)
		if err := validateRuntimeEvidenceSourceCapacity(value); err == nil {
			t.Fatal("unrepresentable metadata reached full-campaign preparation admission")
		}
	}
	t.Logf("objects=%d index=%d manifest=%d completion=%d graph=%d retained-capture=%d separate-supplement=%d; these are not RSS", limits.maximumObjects, metadata.indexBytes, metadata.manifestBytes, metadata.completionBytes, metadata.graphBytes, metadata.retainedBytes, metadata.supplementBytes)
}

// The same physical bounded reader accepts its final byte and refuses the
// actual file grown by one byte, for every larger current/prior/derived path.
func TestCampaignEvidenceCapacityV2MetadataTypedReadersExactAndOneOver(t *testing.T) {
	limits, err := campaignEvidenceLimitsForConfig(campaignMetadataConfigTestV2(t))
	if err != nil {
		t.Fatal(err)
	}
	metadata := *limits.metadata
	metadata.indexBytes, metadata.manifestBytes, metadata.completionBytes = 128, 128, 128
	limits.metadata = &metadata
	intentEnvelope, err := limits.fileEnvelopeBytes("final-inputs/validators/validator-1-steering-intents.json", metadata.intentBytes)
	if err != nil || uint64(intentEnvelope) > limits.maximumEnvelopeBytes() || uint64(intentEnvelope) <= maximumCampaignEvidenceEnvelopeBytes {
		t.Fatalf("small flat census truncated its independently bounded intent carrier: %d %v", intentEnvelope, err)
	}
	root := t.TempDir()
	for _, name := range []string{campaignCollectedIndexPathV2, campaignPriorIndexPathV2, campaignEvidenceManifestFilename, campaignPriorManifestPathV2, "complete.json", campaignPriorCompletionPathV2, campaignDerivedPriorManifestPathV2, campaignDerivedPriorCompletionPathV2, "scenario-complete-commit.operator-1.evidence.json", "scenario-complete-commit.operator-2.evidence.json"} {
		maximum := limits.rawFileBytes(name)
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		raw := bytes.Repeat([]byte{' '}, int(maximum))
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if actual, err := readCampaignEvidenceFileWithLimitsV2(root, name, limits, true); err != nil || !bytes.Equal(actual, raw) {
			t.Fatalf("exact %s: %v", name, err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(file.Truncate(int64(maximum)+1), file.Close()); err != nil {
			t.Fatal(err)
		}
		if actual, err := readCampaignEvidenceFileWithLimitsV2(root, name, limits, true); err == nil || actual != nil {
			t.Fatalf("one-over %s reached a returned body", name)
		}
		if _, err := limits.fileEnvelopeBytes(name, maximum); err != nil {
			t.Fatal(err)
		}
		if _, err := limits.fileEnvelopeBytes(name, maximum+1); err == nil {
			t.Fatalf("one-over %s received a carrier allocation", name)
		}
	}
	for _, name := range []string{"final-inputs/manifest.json.extra", "final-derived/prior-release/complete.json.extra", "scenario-complete-commit.operator-3.evidence.json", "final-inputs/validators/validator-3-steering-intents.json", "final-inputs/validators/v2/not-a-hash.bin"} {
		if limits.rawFileBytes(name) != maximumCampaignEvidenceRawFileBytes {
			t.Fatalf("unowned sibling acquired metadata authority: %s", name)
		}
	}
}

// The actual writer and hash census cross the original 32 MiB refusal. An
// ordinary sibling and fake hash-addressed proof cannot inherit that grant.
func TestCampaignEvidenceCapacityV2MetadataWriterAndHashReaderCrossLegacyLimit(t *testing.T) {
	t.Parallel()
	cfg := campaignMetadataConfigTestV2(t)
	root := t.TempDir()
	raw := append([]byte("{}"), bytes.Repeat([]byte{' '}, maximumCampaignEvidenceRawFileBytes-1)...)
	locator, err := persistFinalCollectedArtifactForConfigV2(cfg, root, "final-semantic-input-manifest", campaignCollectedIndexPathV2, raw)
	if err != nil || locator.SizeBytes != uint64(len(raw)) {
		t.Fatalf("typed writer: %+v %v", locator, err)
	}
	hashes, err := evidenceFileHashesForConfigV2(t.Context(), cfg, root, 2)
	if err != nil || hashes[campaignCollectedIndexPathV2] != bytesSHA256(raw) {
		t.Fatalf("typed hash census: %v %v", hashes, err)
	}
	if _, err := readCampaignEvidenceRegularFile(root, campaignCollectedIndexPathV2); err == nil {
		t.Fatal("legacy reader no longer reproduces its original refusal")
	}
	for _, name := range []string{"ordinary.bin", "final-inputs/validators/v2/" + strings.TrimPrefix(bytesSHA256(raw), "sha256:") + ".bin"} {
		if _, err := persistFinalCollectedArtifactForConfigV2(cfg, root, "source", name, raw); err == nil {
			t.Fatalf("ordinary or fake intent body inherited metadata capacity: %s", name)
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused ordinary source reached a physical write: %v", err)
		}
	}
	intent := []byte(fmt.Sprintf("{\"schema\":%q,\"current\":{},\"history\":[]}", validatorpkg.SteeringIntentSchema))
	intent = append(intent, bytes.Repeat([]byte{' '}, len(raw)-len(intent))...)
	name := "final-inputs/validators/v2/" + strings.TrimPrefix(bytesSHA256(intent), "sha256:") + ".bin"
	if _, err := persistFinalCollectedArtifactForConfigV2(cfg, root, "validator-evidence-v2-source", name, intent); err != nil {
		t.Fatalf("original bounded intent-control shape was refused: %v", err)
	}
	// Shape admission does not assert semantic validity of this synthetic intent.
}

// Both actual retention loops call this debit before retaining. A metadata
// grant neither funds a ninth ordinary raw file nor mutates counters on error.
func TestCampaignEvidenceCapacityV2MetadataCannotFundOrdinaryResidual(t *testing.T) {
	limits, err := campaignEvidenceLimitsForConfig(campaignMetadataConfigTestV2(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, derived := range []bool{false, true} {
		name := campaignCollectedIndexPathV2
		if derived {
			name = campaignDerivedPriorManifestPathV2
		}
		used, ordinary, err := admitCampaignMetadataRetentionV2(limits, name, limits.rawFileBytes(name), derived, 0, 0)
		if err != nil || ordinary != 0 {
			t.Fatalf("exact metadata owner: %d %d %v", used, ordinary, err)
		}
		for index := 0; index < 8; index++ {
			used, ordinary, err = admitCampaignMetadataRetentionV2(limits, fmt.Sprintf("final-derived/ordinary-%d.bin", index), maximumCampaignEvidenceRawFileBytes, derived, used, ordinary)
			if err != nil {
				t.Fatal(err)
			}
		}
		if ordinary != maximumCampaignEvidenceAggregateBytes {
			t.Fatal("exact ordinary residual was not reached")
		}
		if next, residual, err := admitCampaignMetadataRetentionV2(limits, "final-derived/one-over.bin", 1, derived, used, ordinary); err == nil || next != used || residual != ordinary {
			t.Fatal("one-over ordinary residual borrowed metadata or mutated ownership")
		}
	}
}

// Whitespace keeps the valid decoded manifest identical while changing the
// precise payload size. Envelope overhead is never a payload-size extension.
func TestCampaignEvidenceCapacityV2MetadataManifestPayloadExactAndOneOver(t *testing.T) {
	fixture := newFinalPriorCarrierFixtureV2(t)
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	metadata := *limits.metadata
	metadata.manifestBytes = uint64(len(raw))
	limits.metadata = &metadata
	envelope := &ReleaseEvidenceEnvelope{Kind: campaignEvidenceManifestKind, RunID: fixture.manifest.RunID, DeploymentID: fixture.manifest.DeploymentID, ChainID: fixture.manifest.ChainID, Netuid: fixture.manifest.Netuid, GenesisHash: fixture.manifest.GenesisHash, Payload: raw}
	if _, err := decodeCampaignEvidenceManifestWithLimits(envelope, limits); err != nil {
		t.Fatalf("exact typed manifest payload: %v", err)
	}
	envelope.Payload = append(envelope.Payload, ' ')
	if _, err := decodeCampaignEvidenceManifestWithLimits(envelope, limits); err == nil {
		t.Fatal("one-over manifest payload used signed-envelope overhead")
	}
}

// A genuine >32 MiB raw index is signed, retained locally and published to
// both original isolated stores, then read through the public and prior paths.
func TestCampaignEvidenceCapacityV2MetadataSignedPublicAndPriorReadback(t *testing.T) {
	t.Parallel()
	fixture := newCampaignReadbackFixtureV2(t)
	cfg := fixture.prior.archive.cfg
	full := campaignMetadataConfigTestV2(t)
	cfg.Config.ValidatorEvidenceV2 = full.Config.ValidatorEvidenceV2
	cfg.Config.ValidatorEvidenceRelay = full.Config.ValidatorEvidenceRelay
	cfg.Config.Topology.Validators = full.Config.Topology.Validators
	raw := append([]byte("{}"), bytes.Repeat([]byte{' '}, maximumCampaignEvidenceRawFileBytes-1)...)
	entry := campaignEvidenceFileEntry{Path: campaignCollectedIndexPathV2, Size: uint64(len(raw)), ContentHash: bytesSHA256(raw)}
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: fixture.prior.archive.runId, Scope: "run", Path: entry.Path, ContentHash: entry.ContentHash, Size: entry.Size, Data: raw}
	localName, err := finalPriorCarrierLocalPathV2(payload.RunID, payload.Scope, payload.Path)
	if err != nil {
		t.Fatal(err)
	}
	role := fixture.prior.archive.roles.EVM["testnet-owner"]
	envelope, wire, err := prepareLocalEvidence(cfg, fixture.prior.archive.stateDir, filepath.Join(fixture.prior.archive.stateDir, filepath.FromSlash(localName)), campaignEvidenceFileKind, payload.RunID, payload, role, 0)
	if err != nil {
		t.Fatal(err)
	}
	entry.EnvelopeHash = envelope.ContentHash
	serverEnvelope, err := startifactEvidenceEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	for operator := 1; operator <= 2; operator++ {
		store := fixture.prior.archive.stores[operator]
		published, err := startifact.PublishEvidence(t.Context(), store, serverEnvelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyDirectEvidencePublication(t.Context(), store, serverEnvelope, published); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyFinalPriorCarrierWireV2(cfg, payload.RunID, payload.Scope, entry, envelope.Signer, wire); err != nil {
		t.Fatalf("large original prior carrier: %v", err)
	}
	if err := verifyFinalPriorCarrierWireV2(cfg, payload.RunID, payload.Scope, entry, envelope.Signer, append(append([]byte(nil), wire...), ' ')); err == nil {
		t.Fatal("noncanonical prior carrier acquired original custody")
	}
	before := fixture.requests.Load()
	actual, err := fixture.probe.readPublicCampaignSourceV2(t.Context(), fixture.public, payload.RunID, envelope.Signer.Hex(), payload.Scope, entry)
	if err != nil || !bytes.Equal(actual, raw) || fixture.requests.Load()-before != 2 || fixture.activeReaders.Load() != 0 {
		t.Fatalf("actual large two-replica readback: %v requests=%d", err, fixture.requests.Load()-before)
	}
	malformedIndex := &campaignEvidenceManifestPayload{RunID: payload.RunID, Files: []campaignEvidenceFileEntry{entry}}
	if _, err := fixture.probe.streamPublicCampaignArchiveV2(t.Context(), fixture.public, envelope.Signer.Hex(), malformedIndex); err == nil || !strings.Contains(err.Error(), "unsupported final semantic collected-inputs schema") || isFinalSemanticAnalysisPending(err) {
		t.Fatalf("large malformed index escaped the actual retained-index verifier: %v", err)
	}
	localHash, err := finalPriorCarrierLocalHashV2(wire, []byte{'\n'})
	if err != nil {
		t.Fatal(err)
	}
	carrier := FinalCollectedPriorCarrierV2{Scope: payload.Scope, Path: payload.Path, EnvelopeHash: envelope.ContentHash, WireHash: bytesSHA256(wire), WireBytes: uint64(len(wire)), LocalHash: localHash, LocalBytes: uint64(len(wire) + 1), LocalSuffix: []byte{'\n'}}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior := &FinalCollectedPriorPhaseInputs{SemanticStatus: finalSemanticCapturePendingStatus, RunID: payload.RunID, PublicCarriers: []FinalCollectedPriorCarrierV2{carrier}}
	excluded, err := finalPriorCarrierExcludedPathsV2(prior)
	if err != nil || len(excluded) != 1 {
		t.Fatalf("exact original carrier path census: %v %v", excluded, err)
	}
	for relative, original := range excluded {
		if "public/"+relative != localName {
			t.Fatalf("capture path does not rejoin the actual producer namespace: %q %q", relative, localName)
		}
		if err := verifyFinalPriorCarrierLocalFileWithLimitsV2(t.Context(), fixture.prior.archive.stateDir, relative, original, limits); err != nil {
			t.Fatal(err)
		}
		wrongRoot := t.TempDir()
		wrongPath := filepath.Join(wrongRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(wrongPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(fixture.prior.archive.stateDir, filepath.FromSlash(localName)), wrongPath); err != nil {
			t.Fatal(err)
		}
		if err := verifyFinalPriorCarrierLocalFileWithLimitsV2(t.Context(), wrongRoot, relative, original, limits); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("same original bytes outside the public namespace acquired prior custody: %v", err)
		}
	}
	fixture.changedSecond.Store(true)
	if _, err := fixture.probe.readPublicCampaignSourceV2(t.Context(), fixture.public, payload.RunID, envelope.Signer.Hex(), payload.Scope, entry); err == nil {
		t.Fatal("changed second actual replica acquired custody")
	}
}

// Correct raw hashes and recomputed outer signatures cannot excuse the wrong
// file schema or scope. Identical bad signatures at both public stores fail.
func TestCampaignEvidenceCapacityV2MetadataPublicAndPriorKeepSchemaAndSignatureChecks(t *testing.T) {
	fixture := newCampaignReadbackFixtureV2(t)
	cfg := fixture.prior.archive.cfg
	full := campaignMetadataConfigTestV2(t)
	cfg.Config.ValidatorEvidenceV2 = full.Config.ValidatorEvidenceV2
	cfg.Config.ValidatorEvidenceRelay = full.Config.ValidatorEvidenceRelay
	cfg.Config.Topology.Validators = full.Config.Topology.Validators
	role := fixture.prior.archive.roles.EVM["testnet-owner"]
	for _, mode := range []string{"schema", "scope", "signature"} {
		raw := []byte("{}")
		payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "metadata-reject-" + mode, Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
		if mode == "schema" {
			payload.Schema = "wrong-file-schema"
		}
		if mode == "scope" {
			payload.Scope = "reference"
		}
		envelope, err := signEvidence(cfg, campaignEvidenceFileKind, payload.RunID, payload, role)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "signature" {
			envelope.Signature = "0x" + strings.Repeat("0", 130)
		}
		wire, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
		if err := verifyFinalPriorCarrierWireV2(cfg, payload.RunID, "run", entry, envelope.Signer, wire); err == nil {
			t.Fatalf("prior carrier ignored %s", mode)
		}
		local := filepath.Join(t.TempDir(), "malicious-replica.json")
		if err := os.WriteFile(local, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		for operator := 1; operator <= 2; operator++ {
			store := fixture.prior.archive.stores[operator].BlobStore
			key, err := startifact.EvidenceContentKey(store, envelope.ContentHash)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Put(t.Context(), key, local, "application/json"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := fixture.probe.readPublicCampaignSourceV2(t.Context(), fixture.public, payload.RunID, envelope.Signer.Hex(), "run", entry); err == nil || isFinalSemanticAnalysisPending(err) {
			t.Fatalf("actual public decoder ignored %s: %v", mode, err)
		}
	}
}

// Real derivedBytes, staged file signing and staged readback share the exact
// prior-control owner. Ordinary derived files retain the old size/aggregate.
func TestCampaignEvidenceCapacityV2MetadataDerivedSupplementWriterAndReadback(t *testing.T) {
	t.Parallel()
	cfg := campaignMetadataConfigTestV2(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	runId := "metadata-derived"
	runRoot := filepath.Join(stateRoot, "runs", runId)
	archive := &finalSemanticArchive{cfg: cfg, runRoot: runRoot}
	raw := append([]byte("{}"), bytes.Repeat([]byte{' '}, maximumCampaignEvidenceRawFileBytes-1)...)
	for _, item := range []struct{ kind, name string }{{kind: "scenario-complete", name: "prior-release/complete.json"}, {kind: "campaign-evidence-manifest", name: "prior-release/campaign-evidence-manifest.json"}} {
		if _, err := archive.derivedBytes(item.kind, item.name, raw); err != nil {
			t.Fatalf("actual copied prior control: %v", err)
		}
	}
	if _, err := archive.derivedBytes("source", "ordinary.bin", raw); err == nil {
		t.Fatal("ordinary derived writer inherited prior metadata allowance")
	}
	for _, name := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		if _, err := persistFinalCollectedArtifact(runRoot, "test-output", name, []byte("{}\n")); err != nil {
			t.Fatal(err)
		}
	}
	files, err := enumerateFinalSemanticRawFilesForConfigV2(cfg, runRoot, true)
	if err != nil || len(files) != 4 {
		t.Fatalf("derived output census: %d %v", len(files), err)
	}
	owner := roles.EVM["testnet-owner"]
	key, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	envelopes, entries, err := prepareFinalSemanticSupplementFiles(cfg, stateRoot, runId, owner, files)
	if err != nil {
		t.Fatal(err)
	}
	payload := &FinalSemanticSupplementPayload{RunID: runId, Files: entries}
	closure := &finalSemanticOriginalClosure{ownerPublicKey: &key.PublicKey}
	reread, err := loadFinalSemanticSupplementFileEnvelopes(cfg, closure, payload, stateRoot)
	if err != nil || len(reread) != len(envelopes) {
		t.Fatalf("staged large metadata carrier: %v", err)
	}
	decoded, err := decodeFinalSemanticSupplementFiles(payload, reread)
	if err != nil || !finalSemanticRawFilesEqual(files, decoded) {
		t.Fatalf("exact derived carrier readback: %v", err)
	}
	if err := validateFinalSemanticSupplementFileManifest(payload); err == nil {
		t.Fatal("legacy derived manifest no longer reproduces its original refusal")
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for index := range payload.Files {
		if campaignDerivedMetadataPathV2(payload.Files[index].Path) {
			payload.Files[index].Size = limits.rawFileBytes(payload.Files[index].Path) + 1
			if err := validateFinalSemanticSupplementFileManifestWithLimitsV2(payload, limits); err == nil {
				t.Fatal("one-over declared derived metadata admitted a staged read")
			}
			break
		}
	}
}

// The actual signed supplement entry validator retains exactly 256 MiB of
// ordinary outputs even when its independent metadata owner is much larger.
func TestCampaignEvidenceCapacityV2MetadataSupplementOrdinaryAggregateExactAndOneOver(t *testing.T) {
	limits, err := campaignEvidenceLimitsForConfig(campaignMetadataConfigTestV2(t))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename}
	for index := 0; index < 6; index++ {
		paths = append(paths, fmt.Sprintf("final-derived/ordinary-%d.bin", index))
	}
	sort.Strings(paths)
	payload := &FinalSemanticSupplementPayload{}
	for index, path := range paths {
		digest := sha256.Sum256([]byte(fmt.Sprint(index)))
		payload.Files = append(payload.Files, FinalSemanticSupplementFile{Path: path, Size: maximumCampaignEvidenceRawFileBytes, ContentHash: "sha256:" + strings.Repeat("1", 64), EnvelopeHash: "sha256:" + hex.EncodeToString(digest[:])})
	}
	if err := validateFinalSemanticSupplementFileManifestWithLimitsV2(payload, limits); err != nil {
		t.Fatalf("exact ordinary output aggregate: %v", err)
	}
	payload.Files = append(payload.Files, FinalSemanticSupplementFile{Path: "final-derived/one-over.bin", Size: 1, ContentHash: "sha256:" + strings.Repeat("2", 64), EnvelopeHash: "sha256:" + strings.Repeat("3", 64)})
	sort.Slice(payload.Files, func(i, j int) bool { return payload.Files[i].Path < payload.Files[j].Path })
	if err := validateFinalSemanticSupplementFileManifestWithLimitsV2(payload, limits); err == nil {
		t.Fatal("ordinary signed supplement entries borrowed the metadata residual")
	}
}
