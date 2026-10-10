//go:build linux || darwin

// Successor history uses its existing independent local and nonce-registry
// heads. This fresh adapter cannot synthesize historical members or approval.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// These dimensions describe actual runtime bounds, not accounting membership.
type storagePreparationMembersScope struct {
	Schema                  string `json:"schema"`
	Name                    string `json:"name"`
	MaximumBytes            int64  `json:"maximum_bytes"`
	MaximumMembers          int    `json:"maximum_members"`
	MaximumMemberBytes      int    `json:"maximum_member_bytes"`
	MaximumTotalBytes       int    `json:"maximum_total_bytes"`
	MaximumNamespaceEntries int    `json:"maximum_namespace_entries"`
	MaximumNameBytes        int    `json:"maximum_name_bytes"`
}

// The nonce registry owns its whole namespace; local custody may coexist with
// distinct bootstrap role snapshots, whose markers remain separately bound.
func storagePreparationMembersProfile(owner durablevolume.PreparationOwner, ownerLocal bool) (durablehead.Spec, json.RawMessage, error) {
	registry := owner.Kind == "mainnet-successor-nonce-members"
	spec := bootstrapSuccessorMemberSpec(registry)
	expected := storagePreparationMembersScope{Schema: "urnetwork-successor-members-preparation-v1", Name: spec.Name, MaximumBytes: spec.MaximumBytes,
		MaximumMembers: maximumBootstrapSuccessorMemberCount, MaximumMemberBytes: maximumBootstrapSuccessorExecutionBytes,
		MaximumTotalBytes: maximumBootstrapSuccessorMemberTotalBytes, MaximumNamespaceEntries: maximumBootstrapSuccessorMemberCount + 7, MaximumNameBytes: 255}
	var scope storagePreparationMembersScope
	if err := decodePlanJson(owner.Inputs, &scope); err != nil {
		return spec, nil, err
	}
	if ownerLocal || owner.Purpose != "fresh" && owner.Purpose != "restore" || owner.RelativePath != "." || owner.Kind != spec.Kind || scope != expected {
		return spec, nil, errors.New("successor preparation scope, name or capacities differ from its fixed runtime profile")
	}
	raw, err := json.Marshal(scope)
	return spec, raw, err
}

// No member file is needed for an explicitly absent fresh head.
func buildStoragePreparationMembers(ctx context.Context, parent *os.File, name string, owner durablevolume.PreparationOwner, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	spec, census, err := storagePreparationMembersProfile(owner, ownerLocal)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablehead.BuildFreshDirectoryPreparation(ctx, parent, name, owner, spec, census, owner.Kind == "mainnet-successor-nonce-members")
}

// The accepted plan cannot change the original checkpoint destination or scope.
func inspectStoragePreparationMembers(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	spec, census, err := storagePreparationMembersProfile(owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	return durablehead.InspectFreshDirectoryPreparation(ctx, root, owner, spec, census, owner.Owner.Kind == "mainnet-successor-nonce-members")
}
