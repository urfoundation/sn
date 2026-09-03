package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/server"
	"github.com/urnetwork/server/startifact"

	"github.com/urfoundation/sn/payoutartifact"
)

const (
	releaseEvidenceSchema                 = "urnetwork-release-evidence-v1"
	campaignEvidenceFileSchema            = "urnetwork-sim-campaign-evidence-file-v1"
	campaignEvidenceManifestSchema        = "urnetwork-sim-campaign-evidence-manifest-v1"
	campaignEvidenceFileKind              = "scenario-evidence-file"
	campaignEvidenceManifestKind          = "scenario-evidence-manifest"
	campaignEvidenceManifestFilename      = "campaign-evidence-manifest.evidence.json"
	maximumCampaignEvidenceRawFileBytes   = 32 * 1024 * 1024
	maximumCampaignEvidenceEnvelopeBytes  = 64 * 1024 * 1024
	maximumCampaignEvidenceAggregateBytes = 256 * 1024 * 1024
	maximumCampaignEvidenceObjects        = 4096
	maximumCampaignEvidenceJSONDepth      = 64
	campaignEvidenceLocalArchiveDirectory = "campaign-evidence"
)

type campaignEvidenceFilePayload struct {
	Schema      string `json:"schema"`
	RunID       string `json:"run_id"`
	Scope       string `json:"scope"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Size        uint64 `json:"size"`
	Data        []byte `json:"data"`
}

type campaignEvidenceFileEntry struct {
	Path         string `json:"path"`
	ContentHash  string `json:"content_hash"`
	Size         uint64 `json:"size"`
	EnvelopeHash string `json:"envelope_hash"`
}

type campaignEvidenceManifestPayload struct {
	Schema            string                      `json:"schema"`
	DeploymentID      string                      `json:"deployment_id"`
	ChainID           uint64                      `json:"chain_id"`
	GenesisHash       string                      `json:"genesis_hash"`
	Netuid            uint16                      `json:"netuid"`
	RunID             string                      `json:"run_id"`
	ResultHash        string                      `json:"result_hash"`
	BundlePayloadHash string                      `json:"bundle_payload_hash"`
	Files             []campaignEvidenceFileEntry `json:"files"`
	References        []campaignEvidenceFileEntry `json:"references,omitempty"`
}

type preparedCampaignEvidenceArchive struct {
	Manifest *ReleaseEvidenceEnvelope
	Files    []*ReleaseEvidenceEnvelope
}

type ReleaseEvidenceEnvelope struct {
	Schema       string          `json:"schema"`
	DeploymentID string          `json:"deployment_id"`
	ChainID      uint64          `json:"chain_id"`
	GenesisHash  string          `json:"genesis_hash"`
	Netuid       uint16          `json:"netuid"`
	Kind         string          `json:"kind"`
	RunID        string          `json:"run_id,omitempty"`
	CreatedAt    string          `json:"created_at"`
	Payload      json.RawMessage `json:"payload"`
	Signer       common.Address  `json:"signer"`
	ContentHash  string          `json:"content_hash"`
	Signature    string          `json:"signature"`
}

func evidenceUnsignedBytes(e *ReleaseEvidenceEnvelope) ([]byte, error) {
	copy := *e
	copy.ContentHash = ""
	copy.Signature = ""
	return json.Marshal(copy)
}

func signEvidence(cfg *ResolvedConfig, kind, runID string, payload any, role EVMRoleSecret) (*ReleaseEvidenceEnvelope, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, err
	}
	e := &ReleaseEvidenceEnvelope{Schema: releaseEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: strings.ToLower(cfg.Public.Chain.GenesisHash), Netuid: cfg.Netuid, Kind: kind, RunID: runID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Payload: encoded, Signer: crypto.PubkeyToAddress(key.PublicKey)}
	unsigned, err := evidenceUnsignedBytes(e)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(unsigned)
	sig, err := crypto.Sign(h[:], key)
	if err != nil {
		return nil, err
	}
	e.ContentHash = "sha256:" + hex.EncodeToString(h[:])
	e.Signature = "0x" + hex.EncodeToString(sig)
	return e, verifyEvidence(e, &key.PublicKey)
}

func verifyEvidence(e *ReleaseEvidenceEnvelope, expected *ecdsa.PublicKey) error {
	if e == nil || e.Schema != releaseEvidenceSchema || e.DeploymentID == "" || e.ChainID == 0 || e.Netuid == 0 || e.Kind == "" || !json.Valid(e.Payload) || e.Signer == (common.Address{}) {
		return errors.New("invalid release evidence identity")
	}
	unsigned, err := evidenceUnsignedBytes(e)
	if err != nil {
		return err
	}
	h := sha256.Sum256(unsigned)
	if e.ContentHash != "sha256:"+hex.EncodeToString(h[:]) {
		return errors.New("release evidence hash mismatch")
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(e.Signature, "0x"))
	if err != nil || len(sig) != crypto.SignatureLength {
		return errors.New("invalid release evidence signature")
	}
	pub, err := crypto.SigToPub(h[:], sig)
	if err != nil || crypto.PubkeyToAddress(*pub) != e.Signer {
		return errors.New("release evidence signer mismatch")
	}
	if expected != nil && crypto.PubkeyToAddress(*expected) != e.Signer {
		return errors.New("unexpected release evidence signer")
	}
	return nil
}

type PublishedEvidence struct {
	ContentHash string `json:"content_hash"`
	ContentKey  string `json:"content_key"`
	HistoryKey  string `json:"history_key"`
	Bucket      string `json:"bucket"`
}

type scenarioCompletionStoreFactory func(operator int) (server.BlobStore, error)

type renderedOperatorBlobConfig struct {
	Authority string `yaml:"authority"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	TLS       bool   `yaml:"tls"`
	Prefix    string `yaml:"prefix"`
}

// renderedOperatorEvidenceStore opens the same isolated BlobStore namespace
// rendered into one supervised operator. Completion commits use this direct,
// simulator-owned path only after the final process-log scan, so publishing a
// public acceptance pointer cannot itself create an unscanned operator log.
func renderedOperatorEvidenceStore(cfg *ResolvedConfig, stateDir string, operator int) (server.BlobStore, error) {
	if cfg == nil || cfg.Config == nil || operator < 1 || operator > cfg.Config.Topology.Operators {
		return nil, errors.New("invalid operator evidence store identity")
	}
	path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", "minio.yml")
	var rendered renderedOperatorBlobConfig
	if err := strictYAML(path, &rendered); err != nil {
		return nil, fmt.Errorf("operator %d rendered MinIO config: %w", operator, err)
	}
	wantPrefix, err := operatorArtifactPrefix(cfg.Config, operator)
	if err != nil {
		return nil, err
	}
	if rendered.Prefix != wantPrefix {
		return nil, fmt.Errorf("operator %d rendered MinIO prefix %q, want %q", operator, rendered.Prefix, wantPrefix)
	}
	const objectStoreHostVariable = "{{ env:BRINGYOUR_MINIO_HOSTNAME }}"
	if strings.Contains(rendered.Authority, objectStoreHostVariable) && strings.TrimSpace(cfg.ObjectStoreHost) == "" {
		return nil, fmt.Errorf("operator %d rendered MinIO host is unavailable", operator)
	}
	authority := strings.TrimSpace(strings.ReplaceAll(rendered.Authority, objectStoreHostVariable, cfg.ObjectStoreHost))
	if authority == "" || strings.EqualFold(authority, "local") || strings.Contains(authority, "{{") || rendered.Bucket != "blob" || rendered.AccessKey == "" || rendered.SecretKey == "" {
		return nil, fmt.Errorf("operator %d rendered MinIO config is incomplete", operator)
	}
	store, err := server.NewBlobStore(&server.BlobStoreConfig{
		Authority: authority,
		AccessKey: rendered.AccessKey,
		SecretKey: rendered.SecretKey,
		Bucket:    rendered.Bucket,
		Tls:       rendered.TLS,
		Prefix:    rendered.Prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("operator %d direct evidence store: %w", operator, err)
	}
	return store, nil
}

func startifactEvidenceEnvelope(envelope *ReleaseEvidenceEnvelope) (*startifact.EvidenceEnvelope, error) {
	if envelope == nil {
		return nil, errors.New("release evidence envelope is missing")
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	var serverEnvelope startifact.EvidenceEnvelope
	if err := json.Unmarshal(encoded, &serverEnvelope); err != nil {
		return nil, err
	}
	if err := startifact.VerifyEvidence(&serverEnvelope); err != nil {
		return nil, fmt.Errorf("server evidence envelope: %w", err)
	}
	return &serverEnvelope, nil
}

func verifyDirectEvidencePublication(ctx context.Context, store server.BlobStore, envelope *startifact.EvidenceEnvelope, published *startifact.Published) error {
	if store == nil || envelope == nil || published == nil || !strings.EqualFold(published.ContentHash, envelope.ContentHash) || published.Bucket != store.Bucket() {
		return errors.New("direct evidence receipt is invalid")
	}
	wantContentKey, err := startifact.EvidenceContentKey(store, envelope.ContentHash)
	if err != nil {
		return err
	}
	hashHex := strings.TrimPrefix(strings.ToLower(envelope.ContentHash), "sha256:")
	wantHistoryKey := filepath.ToSlash(filepath.Join(store.Prefix(), "st", "v1", "evidence", "history", envelope.DeploymentID, fmt.Sprint(envelope.Netuid), envelope.Kind, envelope.RunID, hashHex+".json"))
	if published.ContentKey != wantContentKey || published.HistoryKey != wantHistoryKey {
		return errors.New("direct evidence receipt keys do not match the rendered store")
	}
	want, err := startifact.EvidenceBytes(envelope)
	if err != nil {
		return err
	}
	for _, key := range []string{published.ContentKey, published.HistoryKey} {
		reader, err := store.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("read direct evidence object %s: %w", key, err)
		}
		got, readErr := io.ReadAll(io.LimitReader(reader, int64(len(want)+1)))
		closeErr := reader.Close()
		if readErr != nil {
			return fmt.Errorf("read direct evidence object %s: %w", key, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close direct evidence object %s: %w", key, closeErr)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("direct evidence object %s differs from its signed envelope", key)
		}
	}
	prefix, err := startifact.EvidenceHistoryPrefix(store, envelope.DeploymentID, envelope.Netuid, envelope.Kind)
	if err != nil {
		return err
	}
	objects, err := store.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list direct evidence history: %w", err)
	}
	for _, object := range objects {
		if object.Key == published.HistoryKey {
			return nil
		}
	}
	return errors.New("direct evidence history does not contain the signed envelope")
}

func validateCampaignEvidencePath(name string) error {
	if name == "" || name != strings.TrimSpace(name) || strings.ContainsAny(name, "\\\x00") || path.Clean(name) != name || strings.HasPrefix(name, "/") || !filepath.IsLocal(filepath.FromSlash(name)) {
		return fmt.Errorf("campaign evidence path %q is not a canonical relative path", name)
	}
	if name == "complete.json" || name == campaignEvidenceManifestFilename || strings.HasPrefix(name, "scenario-complete-commit.operator-") && strings.HasSuffix(name, ".evidence.json") {
		return fmt.Errorf("campaign evidence path %q is reserved", name)
	}
	return nil
}

func readCampaignEvidenceRegularFile(root, name string) ([]byte, error) {
	if err := validateCampaignEvidencePath(name); err != nil {
		return nil, err
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer rootHandle.Close()
	file, err := rootHandle.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumCampaignEvidenceRawFileBytes {
		return nil, stateMismatchError(err, "campaign evidence file %q is non-regular or exceeds %d bytes", name, maximumCampaignEvidenceRawFileBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumCampaignEvidenceRawFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maximumCampaignEvidenceRawFileBytes {
		return nil, fmt.Errorf("campaign evidence file %q exceeds %d bytes", name, maximumCampaignEvidenceRawFileBytes)
	}
	return raw, nil
}

func campaignEvidenceManifestFiles(entries []campaignEvidenceFileEntry) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, errors.New("campaign evidence manifest has no files")
	}
	return campaignEvidenceEntryFiles(entries)
}

func campaignEvidenceEntryFiles(entries []campaignEvidenceFileEntry) (map[string]string, error) {
	if len(entries) > maximumCampaignEvidenceObjects {
		return nil, fmt.Errorf("campaign evidence manifest has %d objects, maximum %d", len(entries), maximumCampaignEvidenceObjects)
	}
	result := make(map[string]string, len(entries))
	previous := ""
	var aggregate uint64
	for index, entry := range entries {
		if err := validateCampaignEvidencePath(entry.Path); err != nil {
			return nil, err
		}
		if index != 0 && entry.Path <= previous {
			return nil, errors.New("campaign evidence manifest files are not strictly sorted and unique")
		}
		if !validSHA256ContentHash(entry.ContentHash) || !validSHA256ContentHash(entry.EnvelopeHash) || entry.Size > maximumCampaignEvidenceRawFileBytes {
			return nil, fmt.Errorf("campaign evidence manifest file %q has invalid content metadata", entry.Path)
		}
		if entry.Size > maximumCampaignEvidenceAggregateBytes-aggregate {
			return nil, fmt.Errorf("campaign evidence manifest exceeds %d aggregate bytes", maximumCampaignEvidenceAggregateBytes)
		}
		aggregate += entry.Size
		result[entry.Path] = strings.ToLower(entry.ContentHash)
		previous = entry.Path
	}
	return result, nil
}

func decodeCampaignEvidenceManifest(envelope *ReleaseEvidenceEnvelope) (*campaignEvidenceManifestPayload, error) {
	if envelope == nil || envelope.Kind != campaignEvidenceManifestKind || envelope.RunID == "" {
		return nil, errors.New("campaign evidence manifest envelope identity is invalid")
	}
	var manifest campaignEvidenceManifestPayload
	if err := decodeStrictJSONBytes(envelope.Payload, &manifest); err != nil {
		return nil, fmt.Errorf("campaign evidence manifest payload: %w", err)
	}
	if manifest.Schema != campaignEvidenceManifestSchema || manifest.DeploymentID == "" || manifest.ChainID == 0 || manifest.Netuid == 0 || manifest.RunID == "" || !validCanonicalHashHex(manifest.ResultHash) || !validSHA256ContentHash(manifest.BundlePayloadHash) {
		return nil, errors.New("campaign evidence manifest payload identity is invalid")
	}
	if manifest.DeploymentID != envelope.DeploymentID || manifest.ChainID != envelope.ChainID || manifest.Netuid != envelope.Netuid || manifest.RunID != envelope.RunID || !strings.EqualFold(manifest.GenesisHash, envelope.GenesisHash) {
		return nil, errors.New("campaign evidence manifest payload does not bind its signed envelope")
	}
	if _, err := campaignEvidenceManifestFiles(manifest.Files); err != nil {
		return nil, err
	}
	references, err := campaignEvidenceEntryFiles(manifest.References)
	if err != nil {
		return nil, fmt.Errorf("campaign evidence manifest references: %w", err)
	}
	files, _ := campaignEvidenceManifestFiles(manifest.Files)
	var aggregate uint64
	for _, entries := range [][]campaignEvidenceFileEntry{manifest.Files, manifest.References} {
		for _, entry := range entries {
			if entry.Size > maximumCampaignEvidenceAggregateBytes-aggregate {
				return nil, fmt.Errorf("campaign evidence manifest exceeds %d aggregate bytes", maximumCampaignEvidenceAggregateBytes)
			}
			aggregate += entry.Size
		}
	}
	if len(manifest.Files)+len(manifest.References) > maximumCampaignEvidenceObjects {
		return nil, fmt.Errorf("campaign evidence manifest exceeds %d objects", maximumCampaignEvidenceObjects)
	}
	for name := range references {
		if _, duplicate := files[name]; duplicate {
			return nil, fmt.Errorf("campaign evidence path %q is both a run file and an external reference", name)
		}
	}
	return &manifest, nil
}

type campaignArtifactReference struct {
	Kind        string
	URI         string
	ContentHash string
	Size        uint64
}

func collectCampaignArtifactReferences(raw json.RawMessage, references map[string]campaignArtifactReference, depth int) error {
	if depth > maximumCampaignEvidenceJSONDepth {
		return fmt.Errorf("campaign artifact JSON exceeds maximum depth %d", maximumCampaignEvidenceJSONDepth)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		var reference campaignArtifactReference
		_ = json.Unmarshal(object["kind"], &reference.Kind)
		_ = json.Unmarshal(object["uri"], &reference.URI)
		_ = json.Unmarshal(object["content_sha256"], &reference.ContentHash)
		_ = json.Unmarshal(object["size_bytes"], &reference.Size)
		if reference.URI != "" || reference.ContentHash != "" || reference.Size != 0 {
			if reference.Kind == "" || reference.URI == "" || !validSHA256ContentHash(reference.ContentHash) || reference.Size == 0 || reference.Size > maximumCampaignEvidenceRawFileBytes {
				return errors.New("campaign artifact locator is incomplete")
			}
			parsed, err := url.Parse(reference.URI)
			if err != nil || parsed.User != nil || parsed.Fragment != "" {
				return fmt.Errorf("campaign artifact locator %q is not canonical and credential-free", reference.URI)
			}
			if parsed.Scheme == "" {
				if parsed.RawQuery != "" {
					return fmt.Errorf("campaign artifact locator %q is not a canonical relative path", reference.URI)
				}
				if err := validateCampaignEvidencePath(reference.URI); err != nil {
					return err
				}
			} else if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.Path == "" || path.Clean(parsed.Path) != parsed.Path {
				return fmt.Errorf("campaign artifact locator %q is not a public HTTP(S) object", reference.URI)
			}
			reference.ContentHash = strings.ToLower(reference.ContentHash)
			if parsed.RawQuery != "" {
				values, queryErr := url.ParseQuery(parsed.RawQuery)
				canonicalQuery := "hash=" + url.QueryEscape(reference.ContentHash)
				literalQuery := "hash=" + reference.ContentHash
				if queryErr != nil || len(values) != 1 || len(values["hash"]) != 1 || values.Get("hash") != reference.ContentHash || parsed.RawQuery != canonicalQuery && parsed.RawQuery != literalQuery {
					return fmt.Errorf("campaign artifact locator %q does not have one exact content hash query", reference.URI)
				}
			}
			if prior, exists := references[reference.URI]; exists && prior != reference {
				return fmt.Errorf("campaign artifact locator %q has conflicting identities", reference.URI)
			}
			if _, exists := references[reference.URI]; !exists && len(references) >= maximumCampaignEvidenceObjects {
				return fmt.Errorf("campaign artifact graph exceeds %d objects", maximumCampaignEvidenceObjects)
			}
			references[reference.URI] = reference
		}
		for _, child := range object {
			if err := collectCampaignArtifactReferences(child, references, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	var array []json.RawMessage
	if json.Unmarshal(raw, &array) == nil {
		for _, child := range array {
			if err := collectCampaignArtifactReferences(child, references, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func campaignArtifactReferences(files map[string][]byte) (map[string]campaignArtifactReference, error) {
	references := map[string]campaignArtifactReference{}
	for name, raw := range files {
		switch {
		case strings.HasSuffix(name, ".json"):
			if err := collectCampaignArtifactReferences(raw, references, 0); err != nil {
				return nil, fmt.Errorf("campaign evidence file %q artifact locator: %w", name, err)
			}
		case strings.HasSuffix(name, ".jsonl"):
			for index, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
				if err := collectCampaignArtifactReferences(line, references, 0); err != nil {
					return nil, fmt.Errorf("campaign evidence file %q line %d artifact locator: %w", name, index+1, err)
				}
			}
		}
	}
	return references, nil
}

func mergeCampaignArtifactReferences(target, additions map[string]campaignArtifactReference) error {
	for name, reference := range additions {
		if prior, exists := target[name]; exists && prior != reference {
			return fmt.Errorf("campaign artifact locator %q has conflicting identities", name)
		}
		if _, exists := target[name]; !exists && len(target) >= maximumCampaignEvidenceObjects {
			return fmt.Errorf("campaign artifact graph exceeds %d objects", maximumCampaignEvidenceObjects)
		}
		target[name] = reference
	}
	return nil
}

func validateCampaignArtifactObjectCount(runFiles int, references map[string]campaignArtifactReference) error {
	if runFiles < 0 || runFiles > maximumCampaignEvidenceObjects-len(references) {
		return fmt.Errorf("campaign evidence graph exceeds %d objects", maximumCampaignEvidenceObjects)
	}
	return nil
}

func mergeCampaignArtifactSource(references map[string]campaignArtifactReference, edges map[string]map[string]bool, source string, raw []byte) error {
	nested, err := campaignArtifactReferences(map[string][]byte{source: raw})
	if err != nil {
		return err
	}
	if len(nested) != 0 {
		if edges[source] == nil {
			edges[source] = map[string]bool{}
		}
		for target := range nested {
			edges[source][target] = true
		}
	}
	return mergeCampaignArtifactReferences(references, nested)
}

func validateCampaignArtifactGraph(edges map[string]map[string]bool) error {
	states := map[string]uint8{}
	heights := map[string]int{}
	var visit func(string) (int, error)
	visit = func(node string) (int, error) {
		switch states[node] {
		case 1:
			return 0, fmt.Errorf("campaign artifact reference graph contains a cycle at %q", node)
		case 2:
			return heights[node], nil
		}
		states[node] = 1
		targets := make([]string, 0, len(edges[node]))
		for target := range edges[node] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		height := 0
		for _, target := range targets {
			targetHeight, err := visit(target)
			if err != nil {
				return 0, err
			}
			if targetHeight+1 > height {
				height = targetHeight + 1
			}
			if height > maximumCampaignEvidenceJSONDepth {
				return 0, fmt.Errorf("campaign artifact reference graph exceeds maximum depth %d", maximumCampaignEvidenceJSONDepth)
			}
		}
		states[node] = 2
		heights[node] = height
		return height, nil
	}
	nodes := make([]string, 0, len(edges))
	for node := range edges {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	for _, node := range nodes {
		if _, err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

func prepareCampaignEvidenceArchive(cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runID, resultHash, bundlePayloadHash string, hashes map[string]string) (*preparedCampaignEvidenceArchive, error) {
	if cfg == nil || cfg.Config == nil || roles == nil || runID == "" || !validCanonicalHashHex(resultHash) || !validSHA256ContentHash(bundlePayloadHash) || len(hashes) == 0 {
		return nil, errors.New("campaign evidence archive identity is incomplete")
	}
	if len(hashes) > maximumCampaignEvidenceObjects {
		return nil, fmt.Errorf("campaign evidence archive has %d run files, maximum %d", len(hashes), maximumCampaignEvidenceObjects)
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return nil, errors.New("campaign evidence archive requires the testnet owner role")
	}
	runDir := filepath.Join(stateDir, "runs", runID)
	names := make([]string, 0, len(hashes))
	for name, hash := range hashes {
		if err := validateCampaignEvidencePath(name); err != nil {
			return nil, err
		}
		if !validSHA256ContentHash(hash) {
			return nil, fmt.Errorf("campaign evidence file %q has an invalid expected hash", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	archive := &preparedCampaignEvidenceArchive{Files: make([]*ReleaseEvidenceEnvelope, 0, len(names))}
	entries := make([]campaignEvidenceFileEntry, 0, len(names))
	rawFiles := make(map[string][]byte, len(names))
	var aggregate uint64
	for _, name := range names {
		raw, err := readCampaignEvidenceRegularFile(runDir, name)
		if err != nil {
			return nil, fmt.Errorf("read campaign evidence file %q: %w", name, err)
		}
		if got := bytesSHA256(raw); !strings.EqualFold(got, hashes[name]) {
			return nil, fmt.Errorf("campaign evidence file %q hash %s does not match %s", name, got, hashes[name])
		}
		if uint64(len(raw)) > maximumCampaignEvidenceAggregateBytes-aggregate {
			return nil, fmt.Errorf("campaign evidence archive exceeds %d aggregate bytes", maximumCampaignEvidenceAggregateBytes)
		}
		aggregate += uint64(len(raw))
		rawFiles[name] = raw
		payload := campaignEvidenceFilePayload{
			Schema: campaignEvidenceFileSchema, RunID: runID, Scope: "run", Path: name,
			ContentHash: strings.ToLower(hashes[name]), Size: uint64(len(raw)), Data: raw,
		}
		pathHash := sha256.Sum256([]byte("run\x00" + name))
		localPath := filepath.Join(stateDir, "public", campaignEvidenceLocalArchiveDirectory, runID, "files", hex.EncodeToString(pathHash[:])+".evidence.json")
		envelope, _, err := prepareLocalEvidence(cfg, stateDir, localPath, campaignEvidenceFileKind, runID, payload, owner, 0)
		if err != nil {
			return nil, fmt.Errorf("prepare campaign evidence file %q: %w", name, err)
		}
		archive.Files = append(archive.Files, envelope)
		entries = append(entries, campaignEvidenceFileEntry{Path: name, ContentHash: strings.ToLower(hashes[name]), Size: uint64(len(raw)), EnvelopeHash: envelope.ContentHash})
	}
	references := map[string]campaignArtifactReference{}
	edges := map[string]map[string]bool{}
	for _, name := range names {
		if err := mergeCampaignArtifactSource(references, edges, name, rawFiles[name]); err != nil {
			return nil, err
		}
	}
	referenceFiles := map[string][]byte{}
	processedReferences := map[string]bool{}
	allowedOrigins, err := campaignArtifactAllowedOriginsForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("campaign evidence transport: %w", err)
	}
	for len(processedReferences) < len(references) {
		referenceNames := make([]string, 0, len(references)-len(processedReferences))
		for name := range references {
			if !processedReferences[name] {
				referenceNames = append(referenceNames, name)
			}
		}
		sort.Strings(referenceNames)
		name := referenceNames[0]
		processedReferences[name] = true
		reference := references[name]
		parsed, _ := url.Parse(name)
		if parsed.Scheme != "" {
			if err := validateCampaignArtifactOrigin(name, allowedOrigins); err != nil {
				return nil, err
			}
			continue
		}
		if raw, exists := rawFiles[name]; exists {
			if uint64(len(raw)) != reference.Size || !strings.EqualFold(bytesSHA256(raw), reference.ContentHash) {
				return nil, fmt.Errorf("campaign artifact locator %q does not match its run file", name)
			}
			continue
		}
		if strings.HasPrefix(name, "public/"+campaignEvidenceLocalArchiveDirectory+"/") || name != "public.json" && !strings.HasPrefix(name, "receipts/") && !strings.HasPrefix(name, "public/") {
			return nil, fmt.Errorf("campaign artifact locator %q is neither a run file nor an approved state artifact", name)
		}
		raw, err := readCampaignEvidenceRegularFile(stateDir, name)
		if err != nil {
			return nil, fmt.Errorf("read campaign referenced artifact %q: %w", name, err)
		}
		if uint64(len(raw)) != reference.Size {
			return nil, fmt.Errorf("campaign referenced artifact %q has the wrong size", name)
		}
		if got := bytesSHA256(raw); !strings.EqualFold(got, reference.ContentHash) {
			return nil, fmt.Errorf("campaign referenced artifact %q hash %s does not match %s", name, got, reference.ContentHash)
		}
		if uint64(len(raw)) > maximumCampaignEvidenceAggregateBytes-aggregate {
			return nil, fmt.Errorf("campaign evidence archive exceeds %d aggregate bytes", maximumCampaignEvidenceAggregateBytes)
		}
		aggregate += uint64(len(raw))
		referenceFiles[name] = raw
		if len(rawFiles)+len(referenceFiles) > maximumCampaignEvidenceObjects {
			return nil, fmt.Errorf("campaign evidence archive exceeds %d objects", maximumCampaignEvidenceObjects)
		}
		if err := mergeCampaignArtifactSource(references, edges, name, raw); err != nil {
			return nil, err
		}
	}
	if err := validateCampaignArtifactGraph(edges); err != nil {
		return nil, err
	}
	if err := validateCampaignArtifactObjectCount(len(rawFiles), references); err != nil {
		return nil, err
	}
	referenceNames := make([]string, 0, len(referenceFiles))
	for name := range referenceFiles {
		referenceNames = append(referenceNames, name)
	}
	sort.Strings(referenceNames)
	referenceEntries := make([]campaignEvidenceFileEntry, 0, len(referenceNames))
	for _, name := range referenceNames {
		reference := references[name]
		raw := referenceFiles[name]
		payload := campaignEvidenceFilePayload{
			Schema: campaignEvidenceFileSchema, RunID: runID, Scope: "reference", Path: name,
			ContentHash: reference.ContentHash, Size: uint64(len(raw)), Data: raw,
		}
		pathHash := sha256.Sum256([]byte("reference\x00" + name))
		localPath := filepath.Join(stateDir, "public", campaignEvidenceLocalArchiveDirectory, runID, "references", hex.EncodeToString(pathHash[:])+".evidence.json")
		envelope, _, err := prepareLocalEvidence(cfg, stateDir, localPath, campaignEvidenceFileKind, runID, payload, owner, 0)
		if err != nil {
			return nil, fmt.Errorf("prepare campaign referenced artifact %q: %w", name, err)
		}
		archive.Files = append(archive.Files, envelope)
		referenceEntries = append(referenceEntries, campaignEvidenceFileEntry{Path: name, ContentHash: reference.ContentHash, Size: uint64(len(raw)), EnvelopeHash: envelope.ContentHash})
	}
	manifestPayload := campaignEvidenceManifestPayload{
		Schema: campaignEvidenceManifestSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: strings.ToLower(cfg.Public.Chain.GenesisHash), Netuid: cfg.Netuid,
		RunID: runID, ResultHash: strings.ToLower(resultHash), BundlePayloadHash: strings.ToLower(bundlePayloadHash), Files: entries, References: referenceEntries,
	}
	manifestPath := filepath.Join(runDir, campaignEvidenceManifestFilename)
	manifest, _, err := prepareLocalEvidence(cfg, stateDir, manifestPath, campaignEvidenceManifestKind, runID, manifestPayload, owner, 0)
	if err != nil {
		return nil, fmt.Errorf("prepare campaign evidence manifest: %w", err)
	}
	if _, err := decodeCampaignEvidenceManifest(manifest); err != nil {
		return nil, err
	}
	archive.Manifest = manifest
	return archive, nil
}

func publishCampaignEvidenceArchive(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runID, resultHash, bundlePayloadHash string, hashes map[string]string, stores scenarioCompletionStoreFactory) (*ReleaseEvidenceEnvelope, error) {
	if err := verifyRuntimeBlobConfigManifest(cfg, stateDir); err != nil {
		return nil, fmt.Errorf("reauthenticate campaign evidence runtime config: %w", err)
	}
	archive, err := prepareCampaignEvidenceArchive(cfg, roles, stateDir, runID, resultHash, bundlePayloadHash, hashes)
	if err != nil {
		return nil, err
	}
	if stores == nil {
		stores = func(operator int) (server.BlobStore, error) {
			return renderedOperatorEvidenceStore(cfg, stateDir, operator)
		}
	}
	operatorStores := make(map[int]server.BlobStore, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		store, err := stores(operator)
		if err != nil {
			return nil, fmt.Errorf("operator %d campaign evidence store: %w", operator, err)
		}
		wantPrefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			return nil, err
		}
		if store == nil || store.Prefix() != wantPrefix {
			return nil, fmt.Errorf("operator %d campaign evidence store prefix is invalid", operator)
		}
		operatorStores[operator] = store
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		store := operatorStores[operator]
		for _, envelope := range append(append([]*ReleaseEvidenceEnvelope(nil), archive.Files...), archive.Manifest) {
			serverEnvelope, err := startifactEvidenceEnvelope(envelope)
			if err != nil {
				return nil, fmt.Errorf("operator %d campaign evidence envelope: %w", operator, err)
			}
			published, err := startifact.PublishEvidence(ctx, store, serverEnvelope)
			if err != nil {
				return nil, fmt.Errorf("operator %d direct campaign evidence publication: %w", operator, err)
			}
			if err := verifyDirectEvidencePublication(ctx, store, serverEnvelope, published); err != nil {
				return nil, fmt.Errorf("operator %d direct campaign evidence verification: %w", operator, err)
			}
		}
	}
	return archive.Manifest, nil
}

func publishedFinalSemanticReaderFactory(ctx context.Context, cfg *ResolvedConfig, stateDir string) (FinalSemanticChainReaderFactory, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil {
		return nil, errors.New("final semantic public manifest context is incomplete")
	}
	_, public, err := loadDeploymentReference(ctx, stateDir, filepath.Join(stateDir, "public.json"))
	if err != nil || public == nil {
		return nil, stateMismatchError(err, "load final semantic public deployment manifest")
	}
	if public.DeploymentID != cfg.Config.Deployment.DeploymentID || public.ConfigHash != cfg.ConfigHash || !strings.EqualFold(public.PolicyHash, cfg.PolicyHash) || public.ChainID != cfg.ChainID || public.Netuid != cfg.Netuid || !strings.EqualFold(public.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return nil, errors.New("final semantic public deployment manifest does not match the active configuration")
	}
	if err := validatePublicEvidenceManifestTransportAgainstConfig(cfg, public); err != nil {
		return nil, fmt.Errorf("final semantic public deployment transport: %w", err)
	}
	manifestHash, err := canonicalHashHex(public)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(public)
	if err != nil {
		return nil, err
	}
	_, signers := inspectPublicIdentityBytesForManifest(public.Identities, public.DeploymentID, public.Topology)
	if len(signers) != public.Topology.Operators || len(public.Operators) != public.Topology.Operators {
		return nil, errors.New("final semantic public manifest signer or operator directory is incomplete")
	}
	operators := make(map[int]PublicOperator, len(public.Operators))
	for _, operator := range public.Operators {
		if operator.NoID < 1 || operator.NoID > public.Topology.Operators || operator.APIURL == "" || operators[operator.NoID].NoID != 0 {
			return nil, errors.New("final semantic public manifest operator directory is invalid")
		}
		operators[operator.NoID] = operator
	}
	expectedHashes := make(map[int]string, public.Topology.Operators)
	for operator := 1; operator <= public.Topology.Operators; operator++ {
		exact, readErr := os.ReadFile(filepath.Join(stateDir, "public", fmt.Sprintf("deployment-manifest.operator-%d.evidence.json", operator)))
		if readErr != nil {
			return nil, fmt.Errorf("read operator %d signed deployment manifest: %w", operator, readErr)
		}
		envelope, envelopeErr := validateArchivedDeploymentManifestEnvelope(exact, public, payload, signers[operator])
		if envelopeErr != nil {
			return nil, fmt.Errorf("operator %d signed deployment manifest: %w", operator, envelopeErr)
		}
		expectedHashes[operator] = envelope.ContentHash
	}
	locatorBytes, err := os.ReadFile(filepath.Join(stateDir, "public", "deployment-manifest.locators.json"))
	if err != nil {
		return nil, fmt.Errorf("read final semantic deployment-manifest locators: %w", err)
	}
	directory, err := validateDeploymentManifestLocatorDirectory(locatorBytes, public, manifestHash, operators, expectedHashes)
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
		return nil, errors.New("final semantic public deployment-manifest discovery URI is missing")
	}
	return func(readerCtx context.Context, evidence *FinalSemanticEvidence) (FinalSemanticChainReader, error) {
		transport, transportErr := canonicalFinalSemanticRPCTransport(public, finalSemanticDefaultEVMRequestsPerMinute, finalSemanticDefaultSubstrateRequestsPerSecond)
		if transportErr != nil {
			return nil, transportErr
		}
		return newPublicFinalSemanticChainReaderWithTransport(readerCtx, public, evidence, discoveryURI, origins, transport)
	}, nil
}

// publishScenarioCompletionCommits creates the operator-authorized outer
// envelopes locally, publishes them through server/startifact directly into
// each rendered BlobStore namespace, and reads both content-addressed objects
// back. Storage-level WORM protection is a separate mainnet infrastructure
// gate documented in FINALIZE.md.
// It deliberately has no supervised HTTP dependency.
func publishScenarioCompletionCommits(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runID string, complete *ReleaseEvidenceEnvelope, stores scenarioCompletionStoreFactory) ([]PublishedEvidence, error) {
	if cfg == nil || cfg.Config == nil || roles == nil {
		return nil, errors.New("scenario completion publication requires role secrets")
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return nil, errors.New("scenario completion publication requires the testnet owner role")
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, err
	}
	var completePayload scenarioCompletePayload
	if verifyEvidence(complete, &ownerKey.PublicKey) != nil || complete.Kind != "scenario-complete" || complete.RunID != runID || complete.DeploymentID != cfg.Config.Deployment.DeploymentID || complete.ChainID != cfg.ChainID || complete.Netuid != cfg.Netuid || !strings.EqualFold(complete.GenesisHash, cfg.Public.Chain.GenesisHash) || decodeStrictJSONBytes(complete.Payload, &completePayload) != nil || !validCanonicalHashHex(completePayload.ResultHash) || !validSHA256ContentHash(completePayload.BundlePayloadHash) || !validSHA256ContentHash(completePayload.EvidenceManifestHash) || len(completePayload.Files) == 0 {
		return nil, errors.New("scenario completion owner envelope is invalid")
	}
	for name, hash := range completePayload.Files {
		if strings.TrimSpace(name) == "" || !validSHA256ContentHash(hash) {
			return nil, errors.New("scenario completion owner envelope has invalid file hashes")
		}
	}
	if err := verifyRuntimeBlobConfigManifest(cfg, stateDir); err != nil {
		return nil, fmt.Errorf("reauthenticate scenario completion runtime config: %w", err)
	}
	if stores == nil {
		stores = func(operator int) (server.BlobStore, error) {
			return renderedOperatorEvidenceStore(cfg, stateDir, operator)
		}
	}
	result := make([]PublishedEvidence, 0, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		role, ok := roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)]
		if !ok {
			return nil, fmt.Errorf("operator %d artifact role is missing", operator)
		}
		localPath := filepath.Join(stateDir, "runs", runID, fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator))
		envelope, _, err := prepareLocalEvidence(cfg, stateDir, localPath, "scenario-complete-commit", runID, complete, role, operator)
		if err != nil {
			return nil, fmt.Errorf("prepare operator %d scenario completion commit: %w", operator, err)
		}
		serverEnvelope, err := startifactEvidenceEnvelope(envelope)
		if err != nil {
			return nil, fmt.Errorf("operator %d scenario completion commit: %w", operator, err)
		}
		store, err := stores(operator)
		if err != nil {
			return nil, fmt.Errorf("operator %d scenario completion store: %w", operator, err)
		}
		wantPrefix, err := operatorArtifactPrefix(cfg.Config, operator)
		if err != nil {
			return nil, err
		}
		if store == nil || store.Prefix() != wantPrefix {
			return nil, fmt.Errorf("operator %d scenario completion store prefix is invalid", operator)
		}
		published, err := startifact.PublishEvidence(ctx, store, serverEnvelope)
		if err != nil {
			return nil, fmt.Errorf("operator %d direct scenario completion publication: %w", operator, err)
		}
		if err := verifyDirectEvidencePublication(ctx, store, serverEnvelope, published); err != nil {
			return nil, fmt.Errorf("operator %d direct scenario completion verification: %w", operator, err)
		}
		result = append(result, PublishedEvidence{
			ContentHash: published.ContentHash,
			ContentKey:  published.ContentKey,
			HistoryKey:  published.HistoryKey,
			Bucket:      published.Bucket,
		})
	}
	return result, nil
}

// commitPublishedScenarioCompletion is the fail-closed completion boundary:
// local complete.json becomes visible only after every direct public history
// commit has been written and independently read back from its operator store.
func commitPublishedScenarioCompletion(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, runID string, complete *ReleaseEvidenceEnvelope, encodedComplete []byte, stores scenarioCompletionStoreFactory) (string, error) {
	if _, err := publishScenarioCompletionCommits(ctx, cfg, roles, stateDir, runID, complete, stores); err != nil {
		return "complete_evidence_publication", err
	}
	if err := atomicWrite(filepath.Join(stateDir, "runs", runID, "complete.json"), encodedComplete, 0o644); err != nil {
		return "complete_evidence_write", err
	}
	return "", nil
}

func publishEvidence(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, stateDir, kind, runID string, payload any) ([]PublishedEvidence, error) {
	if roles == nil {
		return nil, errors.New("evidence publication requires role secrets")
	}
	var published []PublishedEvidence
	client := &http.Client{Timeout: 30 * time.Second}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		role, ok := roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)]
		if !ok {
			return nil, fmt.Errorf("operator %d artifact role is missing", operator)
		}
		localName := fmt.Sprintf("%s.operator-%d.evidence.json", kind, operator)
		localPath := filepath.Join(stateDir, "public", localName)
		if runID != "" {
			localPath = filepath.Join(stateDir, "runs", runID, localName)
		}
		envelope, encoded, err := prepareLocalEvidence(cfg, stateDir, localPath, kind, runID, payload, role, operator)
		if err != nil {
			return nil, err
		}
		url := fmt.Sprintf("http://127.0.0.1:%d/sn/evidence", 18080+operator)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("operator %d evidence API: %w", operator, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("operator %d evidence API returned HTTP %d", operator, resp.StatusCode)
		}
		var result PublishedEvidence
		if err := json.Unmarshal(body, &result); err != nil || !strings.EqualFold(result.ContentHash, envelope.ContentHash) {
			return nil, fmt.Errorf("operator %d evidence receipt is invalid", operator)
		}
		published = append(published, result)
	}
	return published, nil
}

// prepareLocalEvidence keeps ordinary run evidence immutable while permitting
// the deployment manifest's explicitly versioned current pointer to advance.
// Every superseded signed envelope is retained byte-for-byte, so a retry is
// idempotent and no previously published release claim can disappear locally.
func prepareLocalEvidence(cfg *ResolvedConfig, stateDir, localPath, kind, runID string, payload any, role EVMRoleSecret, operator int) (*ReleaseEvidenceEnvelope, []byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, nil, err
	}
	if existing, readErr := os.ReadFile(localPath); readErr == nil {
		var prior ReleaseEvidenceEnvelope
		if json.Unmarshal(existing, &prior) != nil || verifyEvidence(&prior, &key.PublicKey) != nil || prior.Kind != kind || prior.RunID != runID || prior.DeploymentID != cfg.Config.Deployment.DeploymentID || prior.ChainID != cfg.ChainID || prior.Netuid != cfg.Netuid || !strings.EqualFold(prior.GenesisHash, cfg.Public.Chain.GenesisHash) {
			return nil, nil, fmt.Errorf("immutable local evidence %s does not match this publication", localPath)
		}
		if bytes.Equal(prior.Payload, payloadBytes) {
			encoded, marshalErr := json.Marshal(&prior)
			return &prior, encoded, marshalErr
		}
		if kind != "deployment-manifest" || runID != "" {
			return nil, nil, fmt.Errorf("immutable local evidence %s does not match this publication", localPath)
		}
		archiveName := strings.TrimPrefix(strings.ToLower(prior.ContentHash), "sha256:") + fmt.Sprintf(".operator-%d.evidence.json", operator)
		archivePath := filepath.Join(stateDir, "public", "deployment-manifest-history", archiveName)
		if err := writeImmutableEvidenceArchive(archivePath, existing); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, nil, readErr
	}
	envelope, err := signEvidence(cfg, kind, runID, payload, role)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, err
	}
	if err := atomicWrite(localPath, append(encoded, '\n'), 0o644); err != nil {
		return nil, nil, err
	}
	return envelope, encoded, nil
}

func writeImmutableEvidenceArchive(path string, exact []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, exact) {
			return fmt.Errorf("immutable evidence archive %s conflicts with retained history", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(path, exact, 0o644)
}

func verifyPublishedEvidenceOrigins(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, published []PublishedEvidence) error {
	if len(published) != cfg.Config.Topology.Operators || len(cfg.OperatorAPIOrigins) != len(published) {
		return fmt.Errorf("published evidence/origin count does not match operator topology")
	}
	if _, err := resolvedPublicEvidenceTransportProfile(cfg); err != nil {
		return fmt.Errorf("published evidence transport: %w", err)
	}
	for i, receipt := range published {
		if err := verifyPublishedEvidenceOrigin(ctx, cfg, roles, i+1, cfg.OperatorAPIOrigins[i], receipt); err != nil {
			return fmt.Errorf("operator %d public evidence origin: %w", i+1, err)
		}
	}
	return nil
}

// verifyPublishedEvidenceOrigin proves that an externally advertised operator
// origin serves the exact immutable envelope accepted through its local API,
// and that the same content address is visible in public history. A launch is
// not portable to a second host until this check succeeds for every operator.
func verifyPublishedEvidenceOrigin(ctx context.Context, cfg *ResolvedConfig, roles *RoleSecrets, operator int, origin string, published PublishedEvidence) error {
	if operator < 1 || operator > cfg.Config.Topology.Operators || roles == nil {
		return errors.New("invalid public evidence operator")
	}
	role, ok := roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)]
	if !ok {
		return errors.New("artifact signer role is missing")
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
	if err != nil {
		return err
	}
	return verifyPublishedEvidenceOriginWithKey(ctx, cfg, &key.PublicKey, origin, published)
}

func verifyPublishedEvidenceOriginWithKey(ctx context.Context, cfg *ResolvedConfig, expected *ecdsa.PublicKey, origin string, published PublishedEvidence) error {
	if cfg == nil || expected == nil || origin == "" || published.ContentHash == "" {
		return errors.New("invalid public evidence origin verification")
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	contentURL := strings.TrimSuffix(origin, "/") + "/sn/evidence?hash=" + published.ContentHash
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, contentURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, (64*1024*1024)+1))
	resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("content endpoint returned HTTP %d", resp.StatusCode)
	}
	if len(body) > 64*1024*1024 {
		return errors.New("content endpoint exceeded 64 MiB")
	}
	var envelope ReleaseEvidenceEnvelope
	if json.Unmarshal(body, &envelope) != nil || verifyEvidence(&envelope, expected) != nil {
		return errors.New("content endpoint returned invalid signed evidence")
	}
	if !strings.EqualFold(envelope.ContentHash, published.ContentHash) || envelope.DeploymentID != cfg.Config.Deployment.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return errors.New("content endpoint returned evidence for a different deployment")
	}

	historyURL := fmt.Sprintf("%s/sn/evidence/history?deployment_id=%s&netuid=%d&kind=%s", strings.TrimSuffix(origin, "/"), cfg.Config.Deployment.DeploymentID, cfg.Netuid, envelope.Kind)
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
	if err != nil {
		return err
	}
	resp, err = client.Do(req)
	if err != nil {
		return err
	}
	history, readErr := io.ReadAll(io.LimitReader(resp.Body, (16*1024*1024)+1))
	resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("history endpoint returned HTTP %d", resp.StatusCode)
	}
	if len(history) > 16*1024*1024 {
		return errors.New("history endpoint exceeded 16 MiB")
	}
	want := strings.TrimPrefix(strings.ToLower(published.ContentHash), "sha256:")
	for _, object := range evidenceHistoryKeys(history) {
		if strings.Contains(strings.ToLower(object), want) {
			return nil
		}
	}
	return errors.New("public history does not contain the published content hash")
}

type PublicDeploymentManifest struct {
	Schema                   string                     `json:"schema"`
	Release                  string                     `json:"release"`
	DeploymentID             string                     `json:"deployment_id"`
	Revision                 uint64                     `json:"revision,omitempty"`
	PreviousManifestHash     string                     `json:"previous_manifest_hash,omitempty"`
	GeneratedAt              string                     `json:"generated_at"`
	ChainID                  uint64                     `json:"chain_id"`
	GenesisHash              string                     `json:"genesis_hash"`
	RuntimeSpec              uint32                     `json:"runtime_spec"`
	Netuid                   uint16                     `json:"netuid"`
	EVMRPC                   string                     `json:"evm_rpc"`
	SubstrateRPC             string                     `json:"substrate_rpc"`
	OperationalEVMRPC        string                     `json:"operational_evm_rpc,omitempty"`
	OperationalSubstrateRPC  string                     `json:"operational_substrate_rpc,omitempty"`
	OperationalRPCMode       string                     `json:"operational_rpc_mode"`
	IndependentRPC           bool                       `json:"independent_rpc"`
	EvidenceTransportProfile string                     `json:"evidence_transport_profile,omitempty"`
	ConfigHash               string                     `json:"config_hash"`
	PolicyHash               string                     `json:"policy_hash"`
	PlanHash                 string                     `json:"plan_hash"`
	ReleaseLockHash          string                     `json:"release_lock_hash"`
	Contracts                *ContractDeployment        `json:"contracts"`
	CoordinatorUpgrade       CoordinatorUpgrade         `json:"coordinator_upgrade"`
	Identities               json.RawMessage            `json:"identities"`
	SetupEvidence            map[string]json.RawMessage `json:"setup_evidence"`
	Operators                []PublicOperator           `json:"operators"`
	Topology                 TopologyConfig             `json:"topology"`
	ArtifactStores           []string                   `json:"artifact_history_endpoints"`
	EvidenceStores           []string                   `json:"release_evidence_history_endpoints"`
	Commands                 map[string]string          `json:"commands"`
}

type PublicOperator struct {
	NoID       int    `json:"no_id"`
	APIURL     string `json:"api_url"`
	ConnectURL string `json:"connect_url,omitempty"`
	VerifyURL  string `json:"verify_url"`
	HistoryURL string `json:"history_url"`
}

type deploymentManifestLocator struct {
	OperatorNoID int    `json:"operator_no_id"`
	ContentHash  string `json:"content_hash"`
	URL          string `json:"url"`
}

type deploymentManifestLocatorDirectory struct {
	Schema               string                      `json:"schema"`
	ManifestHash         string                      `json:"manifest_hash"`
	ManifestRevision     uint64                      `json:"manifest_revision"`
	PreviousManifestHash string                      `json:"previous_manifest_hash"`
	Locators             []deploymentManifestLocator `json:"locators"`
}

func writePublicDeploymentManifest(cfg *ResolvedConfig, stateDir string, plan *SetupPlan) (*PublicDeploymentManifest, error) {
	contracts, err := loadContractDeployment(stateDir)
	if err != nil {
		return nil, err
	}
	identities, err := os.ReadFile(filepath.Join(stateDir, "public", "identities.json"))
	if err != nil || !json.Valid(identities) {
		return nil, errors.New("public identities are missing or invalid")
	}
	setupEvidence, err := loadPublicSetupEvidence(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	releaseLockHash, err := canonicalHashHex(cfg.Release)
	if err != nil {
		return nil, err
	}
	transportProfile, err := resolvedPublicEvidenceTransportProfile(cfg)
	if err != nil {
		return nil, fmt.Errorf("public evidence transport: %w", err)
	}
	manifest := &PublicDeploymentManifest{Schema: "urnetwork-sim-public-deployment-v1", Release: "1.0", DeploymentID: cfg.Config.Deployment.DeploymentID, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, RuntimeSpec: cfg.Public.Chain.ExpectedRuntimeSpec, Netuid: cfg.Netuid, EVMRPC: cfg.Public.Chain.EVMPublicReadEndpoint, SubstrateRPC: cfg.Public.Chain.SubstratePublicReadEndpoint, OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg), EvidenceTransportProfile: transportProfile, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ReleaseLockHash: releaseLockHash, Contracts: contracts, Identities: append(json.RawMessage(nil), identities...), SetupEvidence: setupEvidence, Topology: cfg.Config.Topology, Commands: map[string]string{}}
	if cfg.OperationalRPCMode == rpcModePublicOverride {
		manifest.OperationalEVMRPC = cfg.OperationalEVM
		manifest.OperationalSubstrateRPC = cfg.OperationalSubstrate
	}
	if plan != nil {
		manifest.PlanHash = plan.PlanHash
		manifest.CoordinatorUpgrade = plan.CoordinatorUpgrade
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		if len(cfg.OperatorAPIOrigins) != cfg.Config.Topology.Operators {
			return nil, fmt.Errorf("public operator API origins are not configured")
		}
		base := strings.TrimSuffix(cfg.OperatorAPIOrigins[operator-1], "/")
		manifest.Operators = append(manifest.Operators, PublicOperator{NoID: operator, APIURL: base, VerifyURL: base + "/verify", HistoryURL: base + "/sn/evidence/history"})
		manifest.ArtifactStores = append(manifest.ArtifactStores, fmt.Sprintf("%s/sn/artifacts?deployment_id=%s&netuid=%d", base, cfg.Config.Deployment.DeploymentID, cfg.Netuid))
		manifest.EvidenceStores = append(manifest.EvidenceStores, fmt.Sprintf("%s/sn/evidence/history?deployment_id=%s&netuid=%d", base, cfg.Config.Deployment.DeploymentID, cfg.Netuid))
	}
	if err := validatePublicCampaignOperatorOrigins(manifest); err != nil {
		return nil, fmt.Errorf("public deployment manifest evidence transport: %w", err)
	}
	for _, command := range []string{"status", "tail", "scenario", "stop", "resume"} {
		manifest.Commands[command] = fmt.Sprintf("sim-testnet %s --config <path-to-testnet.yml> --state-dir <deployment-state-dir>", command)
	}
	for _, command := range []string{"inspect", "analyze"} {
		manifest.Commands[command] = fmt.Sprintf("sim-testnet %s --config <path-to-testnet.yml> --manifest <public-manifest-url-or-file>", command)
	}
	path := filepath.Join(stateDir, "public.json")
	if existing, readErr := os.ReadFile(path); readErr == nil {
		var prior PublicDeploymentManifest
		if json.Unmarshal(existing, &prior) != nil || validatePublicManifestRevision(&prior) != nil {
			return nil, errors.New("existing public deployment manifest is invalid")
		}
		if err := validateLocalPublicManifestHistory(stateDir, &prior); err != nil {
			return nil, err
		}
		if err := requireSamePublicDeployment(&prior, manifest); err != nil {
			return nil, err
		}
		same, err := publicManifestEquivalent(&prior, manifest)
		if err != nil {
			return nil, err
		}
		if same {
			return &prior, nil
		}
		if strings.EqualFold(prior.PlanHash, manifest.PlanHash) {
			return nil, errors.New("existing public deployment manifest changed without a new setup plan")
		}
		if plan == nil || plan.PlanHash == "" || !strings.EqualFold(plan.PlanHash, manifest.PlanHash) || !containsHashFold(plan.PriorPlanHashes, prior.PlanHash) {
			return nil, errors.New("existing public deployment manifest plan is outside the approved revision lineage")
		}
		priorHash, err := canonicalHashHex(&prior)
		if err != nil {
			return nil, err
		}
		archivePath := filepath.Join(stateDir, "public", "deployment-manifests", stringsTrim0x(priorHash)+".json")
		if err := writeImmutableEvidenceArchive(archivePath, existing); err != nil {
			return nil, fmt.Errorf("archive superseded public deployment manifest: %w", err)
		}
		if err := archiveCurrentDeploymentPublication(stateDir, &prior, priorHash); err != nil {
			return nil, err
		}
		manifest.Revision = effectivePublicManifestRevision(&prior) + 1
		manifest.PreviousManifestHash = priorHash
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	} else {
		manifest.Revision = 1
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(path, append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	return manifest, nil
}

// Before a manifest pointer advances, preserve and validate every active
// signer pointer. A locator set proves a complete prior publication and is
// therefore invalid if even one corresponding local signed envelope vanished.
// A publication interrupted before locators existed may contain zero or more
// envelopes; those are retained and cleared so the retry can complete cleanly.
func archiveCurrentDeploymentPublication(stateDir string, prior *PublicDeploymentManifest, priorHash string) error {
	if prior == nil {
		return errors.New("superseded deployment publication has no manifest")
	}
	canonicalPriorHash, err := canonicalHashHex(prior)
	if err != nil {
		return err
	}
	if !strings.EqualFold(canonicalPriorHash, priorHash) {
		return errors.New("superseded deployment publication manifest hash is invalid")
	}
	priorHash = canonicalPriorHash
	_, signers := inspectPublicIdentityBytesForManifest(prior.Identities, prior.DeploymentID, prior.Topology)
	if len(signers) != prior.Topology.Operators || len(prior.Operators) != prior.Topology.Operators {
		return errors.New("superseded deployment publication has an invalid signer directory")
	}
	operators := make(map[int]PublicOperator, len(prior.Operators))
	for _, operator := range prior.Operators {
		if operator.NoID < 1 || operator.NoID > prior.Topology.Operators || operator.APIURL == "" || operators[operator.NoID].NoID != 0 {
			return errors.New("superseded deployment publication has an invalid operator directory")
		}
		operators[operator.NoID] = operator
	}
	expectedPayload, err := json.Marshal(prior)
	if err != nil {
		return err
	}
	type activeEnvelope struct {
		operator int
		path     string
		exact    []byte
		value    ReleaseEvidenceEnvelope
	}
	active := make([]activeEnvelope, 0, prior.Topology.Operators)
	activeByOperator := make(map[int]activeEnvelope, prior.Topology.Operators)
	expectedActivePaths := make(map[string]bool, prior.Topology.Operators)
	for operator := 1; operator <= prior.Topology.Operators; operator++ {
		path := filepath.Join(stateDir, "public", fmt.Sprintf("deployment-manifest.operator-%d.evidence.json", operator))
		expectedActivePaths[path] = true
		exact, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return readErr
		}
		envelope, envelopeErr := validateArchivedDeploymentManifestEnvelope(exact, prior, expectedPayload, signers[operator])
		if envelopeErr != nil {
			return fmt.Errorf("superseded deployment manifest operator %d evidence is invalid", operator)
		}
		item := activeEnvelope{operator: operator, path: path, exact: exact, value: envelope}
		active = append(active, item)
		activeByOperator[operator] = item
	}
	activePaths, err := filepath.Glob(filepath.Join(stateDir, "public", "deployment-manifest.operator-*.evidence.json"))
	if err != nil {
		return err
	}
	for _, path := range activePaths {
		if !expectedActivePaths[path] {
			return fmt.Errorf("superseded deployment publication has an unexpected signed envelope %s", filepath.Base(path))
		}
	}
	locatorPath := filepath.Join(stateDir, "public", "deployment-manifest.locators.json")
	locatorArchive := filepath.Join(stateDir, "public", "deployment-manifests", stringsTrim0x(priorHash)+".locators.json")
	locators, locatorErr := os.ReadFile(locatorPath)
	if locatorErr != nil && !errors.Is(locatorErr, os.ErrNotExist) {
		return locatorErr
	}
	if locatorErr == nil && len(active) != prior.Topology.Operators {
		return errors.New("superseded deployment locators have an incomplete signed envelope set")
	}
	if locatorErr == nil {
		want := make(map[int]string, len(active))
		for _, envelope := range active {
			want[envelope.operator] = envelope.value.ContentHash
		}
		if _, err := validateDeploymentManifestLocatorDirectory(locators, prior, priorHash, operators, want); err != nil {
			return err
		}
	} else if archivedLocators, archiveErr := os.ReadFile(locatorArchive); archiveErr == nil {
		// A durable archived locator proves that an earlier attempt passed the
		// complete-publication check and unlinked the current locator. Validate
		// every archived envelope before completing any remaining unlinks.
		directory, err := validateDeploymentManifestLocatorDirectory(archivedLocators, prior, priorHash, operators, nil)
		if err != nil {
			return fmt.Errorf("recover superseded deployment locators: %w", err)
		}
		want := make(map[int]string, len(directory.Locators))
		for _, locator := range directory.Locators {
			want[locator.OperatorNoID] = locator.ContentHash
			archiveName := strings.TrimPrefix(strings.ToLower(locator.ContentHash), "sha256:") + fmt.Sprintf(".operator-%d.evidence.json", locator.OperatorNoID)
			envelopePath := filepath.Join(stateDir, "public", "deployment-manifest-history", archiveName)
			exact, readErr := os.ReadFile(envelopePath)
			if readErr != nil {
				return fmt.Errorf("recover superseded deployment manifest operator %d evidence: %w", locator.OperatorNoID, readErr)
			}
			envelope, envelopeErr := validateArchivedDeploymentManifestEnvelope(exact, prior, expectedPayload, signers[locator.OperatorNoID])
			if envelopeErr != nil || !strings.EqualFold(envelope.ContentHash, locator.ContentHash) {
				return fmt.Errorf("recover superseded deployment manifest operator %d evidence is invalid", locator.OperatorNoID)
			}
			if current, ok := activeByOperator[locator.OperatorNoID]; ok && !bytes.Equal(current.exact, exact) {
				return fmt.Errorf("recover superseded deployment manifest operator %d active/archive evidence differs", locator.OperatorNoID)
			}
		}
		if _, err := validateDeploymentManifestLocatorDirectory(archivedLocators, prior, priorHash, operators, want); err != nil {
			return fmt.Errorf("recover superseded deployment locators: %w", err)
		}
	} else if !errors.Is(archiveErr, os.ErrNotExist) {
		return archiveErr
	}
	for _, envelope := range active {
		archiveName := strings.TrimPrefix(strings.ToLower(envelope.value.ContentHash), "sha256:") + fmt.Sprintf(".operator-%d.evidence.json", envelope.operator)
		archivePath := filepath.Join(stateDir, "public", "deployment-manifest-history", archiveName)
		if err := writeImmutableEvidenceArchive(archivePath, envelope.exact); err != nil {
			return err
		}
	}
	if locatorErr == nil {
		if err := writeImmutableEvidenceArchive(locatorArchive, locators); err != nil {
			return fmt.Errorf("archive superseded public deployment locators: %w", err)
		}
		if err := removeFileAndSync(locatorPath); err != nil {
			return fmt.Errorf("clear superseded public deployment locators: %w", err)
		}
	}
	for _, envelope := range active {
		if err := removeFileAndSync(envelope.path); err != nil {
			return fmt.Errorf("clear superseded signed deployment evidence: %w", err)
		}
	}
	return nil
}

func validateArchivedDeploymentManifestEnvelope(exact []byte, prior *PublicDeploymentManifest, expectedPayload []byte, expectedSigner string) (ReleaseEvidenceEnvelope, error) {
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(exact, &envelope); err != nil {
		return envelope, err
	}
	if verifyEvidence(&envelope, nil) != nil || envelope.Kind != "deployment-manifest" || envelope.RunID != "" || envelope.DeploymentID != prior.DeploymentID || envelope.ChainID != prior.ChainID || envelope.Netuid != prior.Netuid || !strings.EqualFold(envelope.GenesisHash, prior.GenesisHash) || !strings.EqualFold(envelope.Signer.Hex(), expectedSigner) || !bytes.Equal(envelope.Payload, expectedPayload) {
		return envelope, errors.New("signed deployment evidence does not match its manifest")
	}
	return envelope, nil
}

func validateDeploymentManifestLocatorDirectory(exact []byte, prior *PublicDeploymentManifest, priorHash string, operators map[int]PublicOperator, expectedContentHashes map[int]string) (*deploymentManifestLocatorDirectory, error) {
	var directory deploymentManifestLocatorDirectory
	if err := decodeStrictJSONBytes(exact, &directory); err != nil {
		return nil, errors.New("superseded deployment locator directory is invalid")
	}
	if directory.Schema != "urnetwork-public-manifest-locators-v1" || !strings.EqualFold(directory.ManifestHash, priorHash) || directory.ManifestRevision != effectivePublicManifestRevision(prior) || !strings.EqualFold(directory.PreviousManifestHash, prior.PreviousManifestHash) || len(directory.Locators) != len(operators) {
		return nil, errors.New("superseded deployment locator directory does not match its manifest revision")
	}
	seen := make(map[int]bool, len(directory.Locators))
	for index, locator := range directory.Locators {
		operator, ok := operators[locator.OperatorNoID]
		if !ok || locator.OperatorNoID != index+1 || seen[locator.OperatorNoID] || !validSHA256ContentHash(locator.ContentHash) {
			return nil, errors.New("superseded deployment locator directory is incomplete")
		}
		seen[locator.OperatorNoID] = true
		expectedURL := strings.TrimSuffix(operator.APIURL, "/") + "/sn/evidence?hash=" + locator.ContentHash
		if locator.URL != expectedURL {
			return nil, errors.New("superseded deployment locator directory has an invalid operator URL")
		}
		if expectedContentHashes != nil && !strings.EqualFold(locator.ContentHash, expectedContentHashes[locator.OperatorNoID]) {
			return nil, errors.New("superseded deployment locator directory does not match signed evidence")
		}
	}
	return &directory, nil
}

func finalOperatorEvidenceOrigins(directory *deploymentManifestLocatorDirectory) ([]FinalOperatorEvidenceOrigin, error) {
	if directory == nil || len(directory.Locators) != 2 {
		return nil, errors.New("deployment locator directory must contain exactly two operator origins")
	}
	origins := make([]FinalOperatorEvidenceOrigin, len(directory.Locators))
	for index, locator := range directory.Locators {
		if locator.OperatorNoID != index+1 || locator.URL == "" {
			return nil, errors.New("deployment locator directory is not in canonical operator order")
		}
		origins[index] = FinalOperatorEvidenceOrigin{OperatorNoID: locator.OperatorNoID, ManifestURI: locator.URL}
	}
	return origins, nil
}

func containsHashFold(hashes []string, want string) bool {
	if want == "" {
		return false
	}
	for _, hash := range hashes {
		if strings.EqualFold(hash, want) {
			return true
		}
	}
	return false
}

func removeFileAndSync(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func effectivePublicManifestRevision(manifest *PublicDeploymentManifest) uint64 {
	if manifest != nil && manifest.Revision != 0 {
		return manifest.Revision
	}
	// The pre-revision v1 encoding is revision one of its deployment.
	return 1
}

func validatePublicManifestRevision(manifest *PublicDeploymentManifest) error {
	if manifest == nil {
		return errors.New("public deployment manifest is missing")
	}
	if manifest.Revision <= 1 {
		if manifest.PreviousManifestHash != "" {
			return errors.New("initial public deployment manifest has a predecessor")
		}
		return nil
	}
	previous := strings.TrimPrefix(strings.ToLower(manifest.PreviousManifestHash), "0x")
	if len(previous) != sha256.Size*2 {
		return errors.New("revised public deployment manifest has an invalid predecessor hash")
	}
	if _, err := hex.DecodeString(previous); err != nil {
		return errors.New("revised public deployment manifest has an invalid predecessor hash")
	}
	return nil
}

func validateLocalPublicManifestHistory(stateDir string, current *PublicDeploymentManifest) error {
	if current == nil {
		return errors.New("public deployment manifest history has no current revision")
	}
	next := *current
	for next.Revision > 1 {
		path := filepath.Join(stateDir, "public", "deployment-manifests", stringsTrim0x(strings.ToLower(next.PreviousManifestHash))+".json")
		exact, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read public deployment manifest predecessor revision %d: %w", next.Revision-1, err)
		}
		var previous PublicDeploymentManifest
		if json.Unmarshal(exact, &previous) != nil || validatePublicManifestRevision(&previous) != nil {
			return fmt.Errorf("public deployment manifest predecessor revision %d is invalid", next.Revision-1)
		}
		hash, err := canonicalHashHex(&previous)
		if err != nil {
			return err
		}
		if !strings.EqualFold(hash, next.PreviousManifestHash) {
			return fmt.Errorf("public deployment manifest predecessor revision %d hash mismatch", next.Revision-1)
		}
		if effectivePublicManifestRevision(&previous)+1 != next.Revision {
			return fmt.Errorf("public deployment manifest revision chain jumps from %d to %d", effectivePublicManifestRevision(&previous), next.Revision)
		}
		if err := requireSamePublicDeployment(&previous, current); err != nil {
			return fmt.Errorf("public deployment manifest predecessor revision %d: %w", next.Revision-1, err)
		}
		next = previous
	}
	return nil
}

func requireSamePublicDeployment(prior, current *PublicDeploymentManifest) error {
	if prior.Schema != current.Schema || prior.Release != current.Release || prior.DeploymentID != current.DeploymentID || prior.ChainID != current.ChainID || !strings.EqualFold(prior.GenesisHash, current.GenesisHash) || prior.Netuid != current.Netuid {
		return errors.New("existing public deployment manifest does not match this deployment")
	}
	return nil
}

func publicManifestEquivalent(prior, current *PublicDeploymentManifest) (bool, error) {
	left, right := *prior, *current
	// A pre-profile manifest with the exact signed origin tuple has the same
	// transport meaning as the explicit profile emitted by new publications.
	// Normalize only after strict derivation so a missing field can never turn
	// arbitrary HTTP into a compatibility path.
	leftProfile, leftErr := effectivePublicEvidenceTransportProfile(&left)
	rightProfile, rightErr := effectivePublicEvidenceTransportProfile(&right)
	if leftErr != nil || rightErr != nil {
		return false, errors.Join(leftErr, rightErr)
	}
	left.EvidenceTransportProfile, right.EvidenceTransportProfile = leftProfile, rightProfile
	left.GeneratedAt, right.GeneratedAt = "", ""
	left.Revision, right.Revision = 0, 0
	left.PreviousManifestHash, right.PreviousManifestHash = "", ""
	before, err := canonicalHashHex(&left)
	if err != nil {
		return false, err
	}
	after, err := canonicalHashHex(&right)
	return before == after, err
}

func loadPublicSetupEvidence(cfg *ResolvedConfig, stateDir string) (map[string]json.RawMessage, error) {
	paths := map[string]string{
		"voluntary_conviction": filepath.Join(stateDir, "public", "voluntary-conviction.json"),
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		paths[fmt.Sprintf("fleet_%d_manifest", fleet)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.json", fleet))
		paths[fmt.Sprintf("fleet_%d_commitment", fleet)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.commitment.json", fleet))
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			paths[fmt.Sprintf("fleet_%d_binding_%d", fleet, member)] = filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
		}
	}
	result := make(map[string]json.RawMessage, len(paths))
	for name, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil || !json.Valid(b) {
			return nil, stateMismatchError(err, "public setup evidence %s is missing or invalid", name)
		}
		result[name] = append(json.RawMessage(nil), bytes.TrimSpace(b)...)
	}
	return result, nil
}

// Analysis shares the production schema/verifier while remaining independent
// of the server module and its database.
type payoutArtifact = payoutartifact.Artifact

func verifyPayoutArtifact(artifact *payoutArtifact) error {
	return payoutartifact.Verify(artifact)
}

type assertionFile struct {
	Schema     string            `json:"schema"`
	Assertions []AssertionRecord `json:"assertions"`
}

type junitTestsuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []junitTestcase `xml:"testcase"`
}
type junitTestcase struct {
	Name    string        `xml:"name,attr"`
	Time    string        `xml:"time,attr,omitempty"`
	Failure *junitFailure `xml:"failure,omitempty"`
}
type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func writeJUnit(path, name string, assertions []AssertionRecord) error {
	suite := junitTestsuite{Name: name, Tests: len(assertions)}
	for _, assertion := range assertions {
		item := junitTestcase{Name: assertion.ID, Time: fmt.Sprintf("%.3f", assertion.DurationSeconds)}
		if !assertion.Passed {
			suite.Failures++
			item.Failure = &junitFailure{Message: assertion.Message, Body: assertion.ObservationHash}
		}
		suite.Cases = append(suite.Cases, item)
	}
	b, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	b = append([]byte(xml.Header), append(b, '\n')...)
	return atomicWrite(path, b, 0o644)
}

func evidenceFileHashes(root string, operators int) (map[string]string, error) {
	if operators < 1 {
		return nil, errors.New("evidence file hash operator count must be positive")
	}
	excluded := map[string]bool{"complete.json": true, campaignEvidenceManifestFilename: true}
	for operator := 1; operator <= operators; operator++ {
		excluded[fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator)] = true
	}
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if excluded[rel] || isFinalSemanticPostCapturePath(rel) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		result[rel] = "sha256:" + hex.EncodeToString(h[:])
		return nil
	})
	return result, err
}

type finalSemanticSecretMatcher struct {
	needles   [][]byte
	lowercase func([]byte) []byte
}

// Build one shared, canonical matcher for both the full evidence-tree scan and
// the post-capture output scan. Launch-scale role inventories contain more
// than a thousand entries; lowercasing a 667-KiB artifact once per role made a
// single retry take minutes and allocate gigabytes. Needles are normalized and
// deduplicated once, and each haystack is normalized exactly once.
func newFinalSemanticSecretMatcher(roles *RoleSecrets, walletSecrets ...string) *finalSemanticSecretMatcher {
	candidates := make([]string, 0, len(walletSecrets)+8)
	candidates = append(candidates, walletSecrets...)
	if roles != nil {
		for _, role := range roles.EVM {
			candidates = append(candidates, role.PrivateKeyHex)
		}
		for _, role := range roles.Substrate {
			candidates = append(candidates, role.SeedHex)
		}
		for _, role := range roles.Clients {
			candidates = append(candidates, role.SeedHex)
		}
	}
	seen := make(map[string]struct{}, len(candidates))
	matcher := &finalSemanticSecretMatcher{lowercase: bytes.ToLower}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		normalized := bytes.ToLower([]byte(candidate))
		if len(normalized) < 8 {
			continue
		}
		key := string(normalized)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		matcher.needles = append(matcher.needles, normalized)
	}
	sort.Slice(matcher.needles, func(i, j int) bool { return bytes.Compare(matcher.needles[i], matcher.needles[j]) < 0 })
	return matcher
}

func (m *finalSemanticSecretMatcher) scan(path string, data []byte) error {
	if m == nil || len(m.needles) == 0 {
		return nil
	}
	lowercase := m.lowercase
	if lowercase == nil {
		lowercase = bytes.ToLower
	}
	haystack := lowercase(data)
	for _, needle := range m.needles {
		if bytes.Contains(haystack, needle) {
			return fmt.Errorf("secret material found in evidence file %s", path)
		}
	}
	return nil
}

func scanEvidenceSecrets(stateDir, runDir string, roles *RoleSecrets, walletSecrets ...string) error {
	matcher := newFinalSemanticSecretMatcher(roles, walletSecrets...)
	return scanEvidenceSecretsWithMatcher(stateDir, runDir, matcher)
}

func scanEvidenceSecretsWithMatcher(stateDir, runDir string, matcher *finalSemanticSecretMatcher) error {
	if matcher == nil {
		return errors.New("evidence secret matcher is unavailable")
	}
	roots := []string{runDir, filepath.Join(stateDir, "public"), filepath.Join(stateDir, "public.json"), filepath.Join(stateDir, "receipts"), filepath.Join(stateDir, "processes")}
	visited := map[string]struct{}{}
	for _, root := range roots {
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		visit := func(path string) error {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			absolute = filepath.Clean(absolute)
			if _, duplicate := visited[absolute]; duplicate {
				return nil
			}
			visited[absolute] = struct{}{}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := matcher.scan(path, b); err != nil {
				return err
			}
			if filepath.Ext(path) == ".json" {
				if bundle, decodeErr := decodeFinalCollectedFileBundle(b); decodeErr == nil {
					for _, entry := range bundle.Files {
						if err := matcher.scan(path+"["+entry.Path+"]", entry.Data); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		if !info.IsDir() {
			if err := visit(root); err != nil {
				return err
			}
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			return visit(path)
		}); err != nil {
			return err
		}
	}
	return nil
}

func sortedAssertionIDs(assertions []AssertionRecord) []string {
	ids := make([]string, len(assertions))
	for i, assertion := range assertions {
		ids[i] = assertion.ID
	}
	sort.Strings(ids)
	return ids
}
