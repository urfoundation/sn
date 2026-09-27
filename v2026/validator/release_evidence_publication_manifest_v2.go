//go:build linux || darwin

// A local immutable locator hands a successfully replicated closed census to
// the permissionless relay. It is discovery, never chain or replay authority.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
)

const ValidatorEvidencePublicationV2Schema = "urnetwork-validator-closed-evidence-publication-v2"

// Hashes locate exact canonical ValidatorEvidenceSignedV2 objects containing
// both public consents. A relay must read both origins and authenticate them.
type ValidatorEvidencePublicationV2MemberReference struct {
	NoId                uint64   `json:"no_id"`
	SignedArtifactHash  [32]byte `json:"signed_artifact_hash"`
	SignedArtifactBytes uint64   `json:"signed_artifact_bytes"`
}

// This type describes only a closed all-operator census. Audit commitments
// have different logical slots and cannot be represented by this file grammar.
type ValidatorEvidencePublicationV2Manifest struct {
	Schema      string                                          `json:"schema"`
	Kind        byte                                            `json:"kind"`
	Epoch       uint64                                          `json:"epoch"`
	Origins     [2]string                                       `json:"origins"`
	CensusHash  [32]byte                                        `json:"census_hash"`
	CensusBytes uint64                                          `json:"census_bytes"`
	Members     []ValidatorEvidencePublicationV2MemberReference `json:"members"`
}

// Paths are fixed relative to the caller's admitted validator state owner;
// epochs and identifiers never supply arbitrary filesystem components.
func ValidatorEvidencePublicationV2ManifestPath(stateDir string, epoch uint64) (string, error) {
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || filepath.Dir(stateDir) == stateDir {
		return "", errors.New("closed evidence publication path owner is invalid")
	}
	return filepath.Join(stateDir, "evidence-publications", fmt.Sprintf("epoch-%020d.json", epoch)), nil
}

// Metadata limits reuse the approved closure/member capacities. Origin
// syntax rejects credentials, paths and obvious aliases before any HTTP I/O.
func (self *ValidatorEvidencePublicationV2Manifest) validate(maxBytes, maxMembers uint64) error {
	if err := validateReleaseMeasurementInputV2Limit(maxBytes); err != nil {
		return err
	}
	if self == nil || self.Schema != ValidatorEvidencePublicationV2Schema || self.Kind != protocol.ValidatorEvidenceClosedCensus ||
		maxMembers == 0 || len(self.Members) == 0 || uint64(len(self.Members)) > maxMembers || self.CensusHash == ([32]byte{}) || self.CensusBytes == 0 || self.CensusBytes > maxBytes {
		return errors.New("closed evidence publication manifest or finite bounds differ")
	}
	var canonical [2]string
	for index, origin := range self.Origins {
		parsed, err := url.Parse(origin)
		if err != nil || origin == "" || uint64(len(origin)) > maxBytes || parsed.String() != origin || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
			return errors.New("closed evidence publication origin is not canonical")
		}
		host := strings.ToLower(parsed.Hostname())
		if address := net.ParseIP(host); address != nil {
			host = address.String()
		}
		port := parsed.Port()
		if port == "" {
			port = "443"
			if parsed.Scheme == "http" {
				port = "80"
			}
		}
		canonical[index] = parsed.Scheme + "://" + net.JoinHostPort(host, port)
	}
	if canonical[0] == canonical[1] {
		return errors.New("closed evidence publication origins are not independent")
	}
	for index, member := range self.Members {
		if member.NoId == 0 || index > 0 && member.NoId <= self.Members[index-1].NoId || member.SignedArtifactHash == ([32]byte{}) || member.SignedArtifactBytes == 0 || member.SignedArtifactBytes > maxBytes {
			return errors.New("closed evidence publication member census is invalid")
		}
	}
	return nil
}

// The strict bounded private descriptor reader joins final Close and rejects
// aliases, changed bytes and noncanonical/trailing JSON. The caller compares
// this discovered census/origins with its independent approved configuration.
func ReadValidatorEvidencePublicationV2Manifest(ctx context.Context, path string, maxBytes, maxMembers uint64) (result *ValidatorEvidencePublicationV2Manifest, resultErr error) {
	return readValidatorEvidencePublicationV2Manifest(ctx, path, maxBytes, maxMembers, releaseMeasurementInputV2ReadHooks{})
}

// The same physical reader supports deterministic post-syscall custody
// faults without replacing a locator, consent or replay result.
func readValidatorEvidencePublicationV2Manifest(ctx context.Context, path string, maxBytes, maxMembers uint64, hooks releaseMeasurementInputV2ReadHooks) (result *ValidatorEvidencePublicationV2Manifest, resultErr error) {
	if ctx == nil {
		return nil, errors.New("closed evidence publication read context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(filepath.Dir(path)) != "evidence-publications" || maxMembers == 0 {
		return nil, errors.New("closed evidence publication read path or member bound is invalid")
	}
	encoded, err := readReleaseMeasurementInputV2Context(ctx, path, maxBytes, hooks)
	if err != nil {
		return nil, err
	}
	var value ValidatorEvidencePublicationV2Manifest
	if err := decodeAttemptStreamV2JSON(encoded, maxBytes, &value); err != nil {
		return nil, err
	}
	if err := value.validate(maxBytes, maxMembers); err != nil {
		return nil, err
	}
	wantPath, err := ValidatorEvidencePublicationV2ManifestPath(filepath.Dir(filepath.Dir(path)), value.Epoch)
	if err != nil || path != wantPath {
		return nil, errors.New("closed evidence publication epoch differs from its fixed filename")
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, &value, maxBytes, true, true)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return nil, errors.Join(errors.New("closed evidence publication JSON is not canonical"), err)
	}
	return &value, nil
}

// Call only with the successful complete publisher output. Recompute exact
// hash/size references and enforce the independently configured sorted census
// before the existing no-overwrite private immutable writer is entered.
func WriteValidatorEvidencePublicationV2Manifest(ctx context.Context, stateDir string, publication *ValidatorEvidenceCensusV2Publication, expectedNoIds []uint64, maxBytes, maxMembers uint64) (*ValidatorEvidencePublicationV2Manifest, error) {
	if ctx == nil || publication == nil || maxMembers == 0 || len(publication.Members) == 0 || len(publication.Members) != len(expectedNoIds) || uint64(len(expectedNoIds)) > maxMembers ||
		publication.CensusHash != sha256.Sum256(publication.Census) || uint64(len(publication.Census)) > maxBytes {
		return nil, errors.New("closed evidence publication output differs from its complete owner")
	}
	value := &ValidatorEvidencePublicationV2Manifest{Schema: ValidatorEvidencePublicationV2Schema, Kind: protocol.ValidatorEvidenceClosedCensus,
		Epoch: publication.Members[0].Evidence.Header.Epoch, Origins: publication.Origins, CensusHash: publication.CensusHash, CensusBytes: uint64(len(publication.Census)),
		Members: make([]ValidatorEvidencePublicationV2MemberReference, len(expectedNoIds))}
	for index, member := range publication.Members {
		header := member.Evidence.Header
		if header.NoID != expectedNoIds[index] || header.Kind != protocol.ValidatorEvidenceClosedCensus || header.Subject != (protocol.ValidatorEvidenceSubject{}) || header.Epoch != value.Epoch || header.CensusHash != value.CensusHash || member.SignedArtifactHash != sha256.Sum256(member.SignedArtifact) {
			return nil, errors.New("closed evidence publication member differs from the configured source or slot")
		}
		value.Members[index] = ValidatorEvidencePublicationV2MemberReference{NoId: header.NoID, SignedArtifactHash: member.SignedArtifactHash, SignedArtifactBytes: uint64(len(member.SignedArtifact))}
	}
	if err := value.validate(maxBytes, maxMembers); err != nil {
		return nil, err
	}
	path, err := ValidatorEvidencePublicationV2ManifestPath(stateDir, value.Epoch)
	if err != nil {
		return nil, err
	}
	encoded, err := marshalAttemptSettlementV2JSON(ctx, value, maxBytes, true, true)
	if err != nil {
		return nil, err
	}
	if err := writeReleaseMeasurementInputV2Context(ctx, path, encoded, maxBytes, releaseMeasurementInputV2ReadHooks{}); err != nil {
		return nil, err
	}
	return ReadValidatorEvidencePublicationV2Manifest(ctx, path, maxBytes, maxMembers)
}
