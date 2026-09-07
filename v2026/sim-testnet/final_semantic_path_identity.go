package main

// Routes validator proof authority by operator. Public role declarations bind
// the expected keys; local seed custody and signed records cannot redefine them.

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Canonical NoID order makes the complete key census explicit in new schemas.
// A validator's legacy PathVPK field is only its checked operator-one summary.
type FinalOperatorPathIdentity struct {
	NoID    uint64 `json:"no_id"`
	PathVPK string `json:"path_vpk"`
}

// One invocation owns these decoded public bytes and maps; no mutable global
// authority cache or private key material crosses this boundary.
type finalOperatorPathAuthority struct {
	publicBytes      []byte
	identities       *finalPublicIdentities
	pathsByValidator map[uint64][]FinalOperatorPathIdentity
	keysByValidator  map[uint64]map[uint64]ed25519.PublicKey
}

// Validates shape without inferring authority from a signed record or summary.
func finalOperatorPathKeys(summary string, paths []FinalOperatorPathIdentity, operatorCount int) (map[uint64]ed25519.PublicKey, error) {
	if operatorCount < 1 || len(paths) != operatorCount {
		return nil, errors.New("operator path identity census is incomplete")
	}
	keys := make(map[uint64]ed25519.PublicKey, operatorCount)
	for index, path := range paths {
		if path.NoID != uint64(index+1) {
			return nil, errors.New("operator path identity census is not canonical")
		}
		key, err := finalEd25519PublicKey("operator path VPK", path.PathVPK)
		if err != nil {
			return nil, err
		}
		keys[path.NoID] = key
	}
	if summary != paths[0].PathVPK {
		return nil, errors.New("validator path VPK summary differs from operator one")
	}
	return keys, nil
}

// The full configured public census permits shared keys within one validator,
// but never lets another validator claim any of those operator keys.
func decodeFinalOperatorPathAuthority(data []byte, deploymentID string, validatorCount, operatorCount int) (*finalOperatorPathAuthority, error) {
	if deploymentID == "" || validatorCount < 1 || operatorCount < 1 {
		return nil, errors.New("public operator path authority context is incomplete")
	}
	var identities finalPublicIdentities
	if err := decodeStrictJSONBytes(data, &identities); err != nil {
		return nil, fmt.Errorf("decode public operator path identities: %w", err)
	}
	if identities.Schema != "urnetwork-sim-public-identities-v1" || identities.DeploymentID != deploymentID {
		return nil, errors.New("public operator path identity deployment or schema differs")
	}
	authority := &finalOperatorPathAuthority{
		publicBytes: append([]byte(nil), data...), identities: &identities,
		pathsByValidator: map[uint64][]FinalOperatorPathIdentity{},
		keysByValidator:  map[uint64]map[uint64]ed25519.PublicKey{},
	}
	expectedLabels := map[string]bool{}
	keyOwners := map[string]uint64{}
	for validatorID := 1; validatorID <= validatorCount; validatorID++ {
		keys := map[uint64]ed25519.PublicKey{}
		paths := make([]FinalOperatorPathIdentity, 0, operatorCount)
		for noID := 1; noID <= operatorCount; noID++ {
			label := fmt.Sprintf("validator-%d-no-%d", validatorID, noID)
			expectedLabels[label] = true
			identity, exists := identities.Clients[label]
			if !exists {
				return nil, fmt.Errorf("public operator path role %s is missing", label)
			}
			key, err := finalEd25519PublicKey("public operator path role "+label, identity.ClientKey)
			if err != nil {
				return nil, err
			}
			if owner := keyOwners[string(key)]; owner != 0 && owner != uint64(validatorID) {
				return nil, errors.New("public operator path VPK is reused across validators")
			}
			keyOwners[string(key)] = uint64(validatorID)
			keys[uint64(noID)] = key
			paths = append(paths, FinalOperatorPathIdentity{NoID: uint64(noID), PathVPK: identity.ClientKey})
		}
		authority.pathsByValidator[uint64(validatorID)] = paths
		authority.keysByValidator[uint64(validatorID)] = keys
	}
	for label := range identities.Clients {
		if strings.HasPrefix(label, "validator-") && !expectedLabels[label] {
			return nil, fmt.Errorf("public operator path role %s is outside the configured census", label)
		}
	}
	return authority, nil
}

// Exact declared vectors must match the independently decoded public roles.
func (self *finalOperatorPathAuthority) verify(validatorID uint64, summary string, paths []FinalOperatorPathIdentity) error {
	if self == nil || self.keysByValidator[validatorID] == nil {
		return errors.New("public validator path authority is unavailable")
	}
	keys, err := finalOperatorPathKeys(summary, paths, len(self.keysByValidator[validatorID]))
	if err != nil {
		return err
	}
	for noID, expected := range self.keysByValidator[validatorID] {
		if !bytes.Equal(keys[noID], expected) {
			return fmt.Errorf("validator %d operator %d path VPK differs from its public provisioner role", validatorID, noID)
		}
	}
	return nil
}

// Reads the complete public document through the existing bounded descriptor
// capture reader, then checks every requested local seed with strict raw-only I/O.
func loadFinalOperatorPathAuthority(cfg *ResolvedConfig, stateRoot string, validatorIDs []uint64) (*finalOperatorPathAuthority, error) {
	if cfg == nil || cfg.Config == nil || len(validatorIDs) == 0 {
		return nil, errors.New("local operator path authority context is incomplete")
	}
	entry, err := finalCollectedFileEntry(stateRoot, "public/identities.json")
	if err != nil {
		return nil, err
	}
	authority, err := decodeFinalOperatorPathAuthority(entry.Data, cfg.Config.Deployment.DeploymentID, cfg.Config.Topology.Validators, cfg.Config.Topology.Operators)
	if err != nil {
		return nil, err
	}
	seen := map[uint64]bool{}
	for _, validatorID := range validatorIDs {
		expected := authority.keysByValidator[validatorID]
		if expected == nil || seen[validatorID] {
			return nil, errors.New("requested local validator path census is invalid")
		}
		seen[validatorID] = true
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			path := filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "operators", fmt.Sprintf("no-%d", noID), "client.key")
			seed, err := crv4.LoadRawSeedFile(path)
			if err != nil {
				return nil, fmt.Errorf("validator %d operator %d raw path seed: %w", validatorID, noID, err)
			}
			key := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
			if !bytes.Equal(key, expected[uint64(noID)]) {
				return nil, fmt.Errorf("validator %d operator %d local path seed differs from public provisioner role", validatorID, noID)
			}
		}
	}
	return authority, nil
}

// Expands the already configured validator count, never a new topology default.
func finalConfiguredValidatorIDs(cfg *ResolvedConfig) []uint64 {
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.Validators < 1 {
		return nil
	}
	ids := make([]uint64, cfg.Config.Topology.Validators)
	for index := range ids {
		ids[index] = uint64(index + 1)
	}
	return ids
}

// The caller already owns and hash-checks each bundle. Identical duplicate
// uses are allowed; conflicting copies can never create two public authorities.
func finalCollectedPublicIdentityBytes(value *FinalSemanticCollectedInputs, loaded map[string][]byte) ([]byte, error) {
	if value == nil {
		return nil, errors.New("collected public identity context is absent")
	}
	if err := validateFinalArtifactLocatorReuse(value.ClosedInputBundles); err != nil {
		return nil, err
	}
	var result []byte
	for _, locator := range value.ClosedInputBundles {
		data := loaded[locator.URI]
		if uint64(len(data)) != locator.SizeBytes || bytesSHA256(data) != locator.ContentHash {
			return nil, errors.New("collected public identity bundle content differs")
		}
		bundle, err := decodeFinalCollectedFileBundle(data)
		if err != nil {
			return nil, err
		}
		if finalSemanticBundleClass(bundle.Name) != "public" {
			continue
		}
		for _, entry := range bundle.Files {
			if entry.Path != "identities.json" {
				continue
			}
			if result != nil && !bytes.Equal(result, entry.Data) {
				return nil, errors.New("collected graph contains conflicting public identity copies")
			}
			result = append([]byte(nil), entry.Data...)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("collected public operator identities are missing")
	}
	return result, nil
}

// A repeated URI may reuse checked bytes only under an identical declaration.
// This runs before loading so a later conflicting claim cannot select content.
func validateFinalArtifactLocatorReuse(locators []FinalArtifactLocator) error {
	seen := map[string]FinalArtifactLocator{}
	for _, locator := range locators {
		if prior, exists := seen[locator.URI]; exists && prior != locator {
			return fmt.Errorf("artifact %s has conflicting locator declarations", locator.URI)
		}
		seen[locator.URI] = locator
	}
	return nil
}

// Stages only incoming records. All operators and overlaps are validated before
// the caller-owned maps change, including a late operator's conflicting prefix.
func mergeFinalAttemptCutsAtomically(cuts []*validatorpkg.AttemptLedgerCut, recordsByNO map[uint64]map[uint64]validatorpkg.AttemptRecord) error {
	incoming := map[uint64]map[uint64]validatorpkg.AttemptRecord{}
	for _, cut := range cuts {
		if cut == nil || recordsByNO[cut.Identity.NoID] == nil {
			return errors.New("signed attempt merge authority is incomplete")
		}
		noID := cut.Identity.NoID
		if incoming[noID] == nil {
			incoming[noID] = map[uint64]validatorpkg.AttemptRecord{}
		}
		for _, record := range cut.Records {
			if prior, exists := recordsByNO[noID][record.Sequence]; exists && !finalJSONEqual(prior, record) {
				return fmt.Errorf("operator %d attempt sequence %d conflicts with prior authority", noID, record.Sequence)
			}
			if prior, exists := incoming[noID][record.Sequence]; exists && !finalJSONEqual(prior, record) {
				return fmt.Errorf("operator %d attempt sequence %d conflicts within incoming authority", noID, record.Sequence)
			}
			incoming[noID][record.Sequence] = record
		}
	}
	for noID, records := range incoming {
		for sequence, record := range records {
			recordsByNO[noID][sequence] = record
		}
	}
	return nil
}
