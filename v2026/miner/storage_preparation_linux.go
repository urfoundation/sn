//go:build linux

// These two fixed offline adapters preserve the existing fleet/claim formats.
// They accept only public capacities; no signer, runtime owner or RPC is used.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Fleet has additional record/raw/manifest bounds. Claim's byte bound is its
// existing retained capacity; no new record-count policy is silently imposed.
type minerStoragePreparationScope struct {
	Schema                string `json:"schema"`
	Name                  string `json:"name"`
	MaximumBytes          int64  `json:"maximum_bytes"`
	MaximumRecords        uint64 `json:"maximum_records,omitempty"`
	MaximumRawRecordBytes uint64 `json:"maximum_raw_record_bytes,omitempty"`
	MaximumManifestBytes  uint64 `json:"maximum_manifest_bytes,omitempty"`
}

// The separately chosen public command scope cannot be changed by its JSON.
func minerStoragePreparationProfile(ownerLocal bool, owner durablevolume.PreparationOwner) (durablehead.Spec, json.RawMessage, error) {
	var spec durablehead.Spec
	var expected minerStoragePreparationScope
	switch owner.Kind {
	case "fleet-recovery":
		spec = durablehead.Spec{Kind: "fleet-recovery", Name: "journal.json", MaximumBytes: fleetRecoveryMaxBytes, AuxiliaryNames: []string{"initialized"}}
		expected = minerStoragePreparationScope{Schema: "urnetwork-miner-snapshot-preparation-v1", Name: spec.Name, MaximumBytes: spec.MaximumBytes, MaximumRecords: fleetRecoveryMaxRecords, MaximumRawRecordBytes: fleetRecoveryMaxRaw, MaximumManifestBytes: 256 * 1024}
	case "provider-claim-queue":
		spec = durablehead.Spec{Kind: "provider-claim-queue", Name: "claim-queue.json", MaximumBytes: maximumClaimQueueBytes}
		expected = minerStoragePreparationScope{Schema: "urnetwork-miner-snapshot-preparation-v1", Name: spec.Name, MaximumBytes: spec.MaximumBytes}
	default:
		return spec, nil, errors.New("miner preparation kind is not in its fixed registry")
	}
	var scope minerStoragePreparationScope
	decoder := json.NewDecoder(bytes.NewReader(owner.Inputs))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&scope); err != nil {
		return spec, nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return spec, nil, errors.New("miner preparation input contains trailing data")
	}
	if owner.Purpose != "fresh" && owner.Purpose != "restore" || owner.RelativePath != "." || ownerLocal != (owner.Kind == "fleet-recovery") || scope != expected {
		return spec, nil, errors.New("miner preparation scope, name or capacities differ from its fixed runtime profile")
	}
	raw, err := json.Marshal(scope)
	return spec, raw, err
}

// The dispatcher uses this fixed registry to build only empty staging bytes.
func BuildFreshStoragePreparation(ctx context.Context, parent *os.File, name string, owner durablevolume.PreparationOwner, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	spec, census, err := minerStoragePreparationProfile(ownerLocal, owner)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablehead.BuildFreshDirectoryPreparation(ctx, parent, name, owner, spec, census, true)
}

// The live target is only observed; the accepted base stage owns all writes.
func InspectFreshStoragePreparation(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	spec, census, err := minerStoragePreparationProfile(ownerLocal, owner.Owner)
	if err != nil {
		return nil, err
	}
	return durablehead.InspectFreshDirectoryPreparation(ctx, root, owner, spec, census, true)
}
