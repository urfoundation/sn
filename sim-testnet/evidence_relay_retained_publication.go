//go:build linux || darwin

// A stopped capture reads immutable objects from both original operator stores.
// No API process, upload, replacement origin or cache verdict is required.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/startifact"
	"gopkg.in/yaml.v3"
)

type evidenceRelayRetainedPublications struct {
	origins [2]string
	stores  [2]server.BlobStore
}

// Capture already owns the stopped supervisor and validator checks. Authenticate
// the exact current runtime manifest before constructing any store transport.
func newEvidenceRelayRetainedPublications(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan) (*evidenceRelayRetainedPublications, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || plan == nil || !cfg.readOnlyAudit || cfg.Config.Topology.Operators != 2 || len(cfg.OperatorAPIOrigins) != 2 {
		return nil, errors.New("retained publication requires a stopped read-only two-operator capture")
	}
	if provisionalResumeEnabled(cfg) {
		if err := validateProvisionalRelayCaptureContext(cfg, plan); err != nil {
			return nil, err
		}
	} else if cfg.relayCapturePlanHash != plan.PlanHash {
		return nil, errors.New("retained publication has no exact strict capture approval")
	}
	manifest, _, err := authenticatedRetainedRuntimeConfigManifest(cfg, stateDir, plan)
	if err != nil {
		return nil, err
	}
	result := &evidenceRelayRetainedPublications{origins: [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]}}
	for index := range result.stores {
		operator := index + 1
		relative := filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("operator-%d", operator), "vault", "minio.yml"))
		var expected *RuntimeConfigFile
		for fileIndex := range manifest.Files {
			file := &manifest.Files[fileIndex]
			if file.Path == relative {
				expected = file
				break
			}
		}
		if expected == nil || expected.Mode != "0600" {
			return nil, errors.New("retained publication lost an original private operator store")
		}
		raw, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(stateDir, filepath.FromSlash(relative)), 64*1024)
		if err != nil {
			return nil, fmt.Errorf("read retained publication operator %d store: %w", operator, err)
		}
		if bytesSHA256(raw) != expected.SHA256 {
			return nil, fmt.Errorf("retained publication operator %d store differs from its authenticated runtime manifest", operator)
		}
		var rendered renderedOperatorBlobConfig
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		if err := decoder.Decode(&rendered); err != nil {
			return nil, err
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, errors.Join(errors.New("retained publication store has trailing configuration"), err)
		}
		result.stores[index], err = renderedOperatorEvidenceStoreConfig(cfg, operator, rendered)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// Each callback reads only its own namespace and exact bounded object. The
// shared validator reader still verifies complete membership and both consents.
func (self *evidenceRelayRetainedPublications) readers(bounds validatorcomponent.ReleaseEvidenceV2Bounds) ([2]validatorcomponent.ValidatorEvidenceRetainedReplicaV2, error) {
	var result [2]validatorcomponent.ValidatorEvidenceRetainedReplicaV2
	if self == nil || self.stores[0] == nil || self.stores[1] == nil || self.origins[0] == self.origins[1] {
		return result, errors.New("retained publication requires two original replica stores")
	}
	maximum := max(bounds.Cut.Records.MaxManifestBytes, bounds.Cut.Records.MaxPageBytes, bounds.Cut.Proofs.MaxManifestBytes, bounds.Cut.Proofs.MaxPageBytes, bounds.MaxTransitionBytes)
	objectBounds := startifact.AttemptObjectBounds{MetadataBytes: maximum, RecordBytes: bounds.Cut.Records.MaxChunkBytes, ProofBytes: bounds.Cut.Proofs.MaxChunkBytes}
	if err := objectBounds.Validate(); err != nil {
		return result, err
	}
	for index, store := range self.stores {
		read := func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			if size == 0 || size > maximum {
				return nil, errors.New("retained publication metadata exceeds its approved bound")
			}
			limited := objectBounds
			limited.MetadataBytes = size
			var raw bytes.Buffer
			count, err := startifact.ReadAttemptObjectTo(ctx, store, limited, "metadata", hash, &raw)
			if err != nil {
				return nil, err
			}
			if count != size {
				return nil, errors.New("retained publication object differs from its declared size")
			}
			return raw.Bytes(), nil
		}
		result[index] = validatorcomponent.ValidatorEvidenceRetainedReplicaV2{Origin: self.origins[index], ReadMetadata: read}
	}
	return result, nil
}
