//go:build linux || darwin

// Later audits have coordinate-selected immutable locators separate from the
// closed-census grammar. Discovery is bounded private custody, not authority.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/urfoundation/sn/v2026/protocol"
)

const ValidatorEvidenceDepositAuditV2ManifestSchema = "urnetwork-validator-deposit-audit-publication-v2"

// The decision pins locate the actual observation; a relay supplies independent
// epoch geometry and consent authority, never derives truth from this locator.
type ValidatorEvidenceDepositAuditV2Manifest struct {
	Schema      string                                          `json:"schema"`
	Kind        byte                                            `json:"kind"`
	Epoch       uint64                                          `json:"epoch"`
	Subject     protocol.ValidatorEvidenceSubject               `json:"subject"`
	Decision    ReleaseMeasurementV2Decision                    `json:"decision"`
	Origins     [2]string                                       `json:"origins"`
	CensusHash  [32]byte                                        `json:"census_hash"`
	CensusBytes uint64                                          `json:"census_bytes"`
	Members     []ValidatorEvidencePublicationV2MemberReference `json:"members"`
}

// Numeric coordinates cannot inject a filesystem component or a verdict hash.
func ValidatorEvidenceDepositAuditV2ManifestPath(stateDir string, epoch uint64, subject protocol.ValidatorEvidenceSubject) (string, error) {
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || filepath.Dir(stateDir) == stateDir || subject.ObservationEpoch <= epoch {
		return "", errors.New("deposit audit publication owner or later subject is invalid")
	}
	return filepath.Join(stateDir, "evidence-deposit-audits", fmt.Sprintf("epoch-%020d-observation-%020d-native-%020d.json", epoch, subject.ObservationEpoch, subject.NativeEpoch)), nil
}

// Reuse the existing bounded member/origin grammar without admitting audit
// coordinates into any closed-census reader or filename.
func (self *ValidatorEvidenceDepositAuditV2Manifest) validate(maxBytes, maxMembers uint64) error {
	if self == nil || self.Schema != ValidatorEvidenceDepositAuditV2ManifestSchema || self.Kind != protocol.ValidatorEvidenceDepositAudit || self.Subject.ObservationEpoch <= self.Epoch || self.Decision.SettlementEpoch != self.Subject.ObservationEpoch || self.Decision.SubnetEpoch != self.Subject.NativeEpoch || self.Decision.PreviousArtifactHash != "" {
		return errors.New("deposit audit locator is not a later coordinate-bound observation")
	}
	shape := ValidatorEvidencePublicationV2Manifest{Schema: ValidatorEvidencePublicationV2Schema, Kind: protocol.ValidatorEvidenceClosedCensus, Epoch: self.Epoch, Origins: self.Origins, CensusHash: self.CensusHash, CensusBytes: self.CensusBytes, Members: self.Members}
	return shape.validate(maxBytes, maxMembers)
}

// Exact private bytes are re-encoded and compared after descriptor-owned read.
func ReadValidatorEvidenceDepositAuditV2Manifest(ctx context.Context, path string, maxBytes, maxMembers uint64) (result *ValidatorEvidenceDepositAuditV2Manifest, resultErr error) {
	return readValidatorEvidenceDepositAuditV2Manifest(ctx, path, maxBytes, maxMembers, releaseMeasurementInputV2ReadHooks{})
}

// Observers may fault real acquisition/Close; none substitutes locator bytes
// or initial-absence authority. Exported readers always use physical defaults.
func readValidatorEvidenceDepositAuditV2Manifest(ctx context.Context, path string, maxBytes, maxMembers uint64, hooks releaseMeasurementInputV2ReadHooks) (result *ValidatorEvidenceDepositAuditV2Manifest, resultErr error) {
	if ctx == nil {
		return nil, errors.New("deposit audit locator context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(filepath.Dir(path)) != "evidence-deposit-audits" {
		return nil, errors.New("deposit audit locator path is not canonical")
	}
	encoded, err := readReleaseMeasurementInputV2Context(ctx, path, maxBytes, hooks)
	if err != nil {
		return nil, err
	}
	var value ValidatorEvidenceDepositAuditV2Manifest
	if err := decodeAttemptStreamV2JSON(encoded, maxBytes, &value); err != nil {
		return nil, err
	}
	if err := value.validate(maxBytes, maxMembers); err != nil {
		return nil, err
	}
	want, err := ValidatorEvidenceDepositAuditV2ManifestPath(filepath.Dir(filepath.Dir(path)), value.Epoch, value.Subject)
	if err != nil || want != path {
		return nil, errors.Join(errors.New("deposit audit locator coordinates differ from its filename"), err)
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, &value, maxBytes, true, true)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return nil, errors.Join(errors.New("deposit audit locator is not canonical"), err)
	}
	return &value, nil
}

// Enumerate completed subjects without creating a missing directory. Every
// object and aggregate byte/count limit is admitted before append or decoding.
func DiscoverValidatorEvidenceDepositAuditV2Manifests(ctx context.Context, stateDir string, bounds ReleaseEvidenceV2Bounds) ([]ValidatorEvidenceDepositAuditV2Manifest, error) {
	return discoverValidatorEvidenceDepositAuditV2Manifests(ctx, stateDir, bounds, nil)
}

// The per-call seam forces either side of the genuine read without changing
// decoding, complete-result refusal or the charged outer file witness.
func discoverValidatorEvidenceDepositAuditV2Manifests(ctx context.Context, stateDir string, bounds ReleaseEvidenceV2Bounds, step func(operation, path string) error) (result []ValidatorEvidenceDepositAuditV2Manifest, resultErr error) {
	if ctx == nil || bounds.MaxHistoryBytes == 0 || bounds.MaxParticipants == 0 {
		return nil, errors.New("deposit audit discovery owner or bounds are absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	probe, err := ValidatorEvidenceDepositAuditV2ManifestPath(stateDir, 0, protocol.ValidatorEvidenceSubject{ObservationEpoch: 1})
	if err != nil {
		return nil, err
	}
	if err := validateReleaseMeasurementInputV2Limit(bounds.MaxClosureBytes); err != nil {
		return nil, err
	}
	directory, err := openAttemptPrivateDirectory(filepath.Dir(probe))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, directory.check(), directory.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	remaining := bounds.MaxHistoryBytes
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.file.ReadDir(1)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		for _, entry := range entries {
			state, err := directory.stat(entry.Name())
			if err != nil || !state.regular() || state.uid != uint32(os.Geteuid()) || state.mode&0o077 != 0 || state.size <= 0 || uint64(state.size) > min(remaining, bounds.MaxClosureBytes) || len(result) >= 16384 {
				return nil, errors.Join(errors.New("deposit audit discovery exceeds its private history census"), err)
			}
			remaining -= uint64(state.size)
			path := filepath.Join(directory.path, entry.Name())
			if step != nil {
				if err := step("before-read", path); err != nil {
					return nil, err
				}
			}
			manifest, err := ReadValidatorEvidenceDepositAuditV2Manifest(ctx, path, bounds.MaxClosureBytes, bounds.MaxParticipants)
			if err != nil {
				return nil, err
			}
			if step != nil {
				if err := step("after-read", path); err != nil {
					return nil, err
				}
			}
			after, err := directory.stat(entry.Name())
			if err != nil || after != state {
				return nil, errors.Join(errors.New("deposit audit publication changed during bounded discovery"), err)
			}
			if err := directory.check(); err != nil {
				return nil, err
			}
			result = append(result, *manifest)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	slices.SortFunc(result, func(left, right ValidatorEvidenceDepositAuditV2Manifest) int {
		for _, pair := range [][2]uint64{{left.Epoch, right.Epoch}, {left.Subject.ObservationEpoch, right.Subject.ObservationEpoch}, {left.Subject.NativeEpoch, right.Subject.NativeEpoch}} {
			if pair[0] < pair[1] {
				return -1
			}
			if pair[0] > pair[1] {
				return 1
			}
		}
		return 0
	})
	return result, nil
}
