//go:build linux

// Every restore kind comes from the existing fixed runtime registry. A
// snapshot's physical head cannot stand in for an immutable member registry or
// a signed validator ledger, which require their own semantic adapters.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Runtime source fixes capacities and marker layout; request bytes cannot
// introduce a new kind, rename its required file or authorize a larger journal.
func planStoragePreparationRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	if owner.RestoreCoverage != "" {
		switch owner.Kind {
		case chain.NativeJournalPreparationKind, "fleet-recovery", "provider-claim-queue", validator.AttemptLedgerPreparationKind:
			return durablevolume.PreparationOwnerPlan{}, errors.New("restore coverage has no shared namespace for this exclusive owner")
		}
	}
	switch owner.Kind {
	case storageSdkWorkKind:
		return planStorageSdkWorkRestore(ctx, name, owner, report, ownerLocal)
	case storageOriginalContractKind:
		return planStorageOriginalContractRestore(ctx, name, owner, report, ownerLocal)
	case storageProviderPublicationKind:
		return planStorageProviderPublicationRestore(ctx, name, owner, report, ownerLocal)
	case storageNativeApprovalKind:
		return planStorageNativeApprovalRestore(ctx, name, owner, report, ownerLocal)
	case storageNativeProducerKind:
		return planStorageNativeProducerRestore(ctx, name, owner, report, ownerLocal)
	case storageMonitorTreeKind:
		return planStorageMonitorTreeRestore(ctx, name, owner, report, ownerLocal)
	case chain.NativeJournalPreparationKind:
		return chain.PlanNativeJournalRestore(ctx, name, owner, report)
	case "fleet-recovery", "provider-claim-queue":
		return miner.PlanStorageRestore(ctx, name, owner, report, ownerLocal)
	case validator.AttemptLedgerPreparationKind:
		if ownerLocal {
			return durablevolume.PreparationOwnerPlan{}, errors.New("validator ledger restore requires its daemon declaration scope")
		}
		return validator.PlanAttemptLedgerRestore(ctx, name, owner, report)
	case "mainnet-successor-local-members", "mainnet-successor-nonce-members":
		return planStoragePreparationMembersRestore(ctx, name, owner, report, ownerLocal)
	}
	spec, scope, err := storagePreparationSnapshotSpec(ownerLocal, owner)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	profile, err := json.Marshal(scope)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablehead.PlanRestore(ctx, name, owner, spec, profile, report)
}

// The core owns all writes. Inspect returns only exact new physical metadata
// after checking original bytes, never a replacement signed protocol record.
func inspectStoragePreparationRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	switch owner.Owner.Kind {
	case storageSdkWorkKind:
		return inspectStorageSdkWorkRestore(ctx, root, owner, report, ownerLocal)
	case storageOriginalContractKind:
		return inspectStorageOriginalContractRestore(ctx, root, owner, report, ownerLocal)
	case storageProviderPublicationKind:
		return inspectStorageProviderPublicationRestore(ctx, root, owner, report, ownerLocal)
	case storageNativeApprovalKind:
		return inspectStorageNativeApprovalRestore(ctx, root, owner, report, ownerLocal)
	case storageNativeProducerKind:
		return inspectStorageNativeProducerRestore(ctx, root, owner, report, ownerLocal)
	case storageMonitorTreeKind:
		return inspectStorageMonitorTreeRestore(ctx, root, owner, report, ownerLocal)
	case chain.NativeJournalPreparationKind:
		return chain.InspectNativeJournalRestore(ctx, root, owner, report)
	case "fleet-recovery", "provider-claim-queue":
		return miner.InspectStorageRestore(ctx, root, owner, report, ownerLocal)
	case validator.AttemptLedgerPreparationKind:
		if ownerLocal {
			return nil, errors.New("validator ledger restore requires its daemon declaration scope")
		}
		return validator.InspectAttemptLedgerRestore(ctx, root, owner, report)
	case "mainnet-successor-local-members", "mainnet-successor-nonce-members":
		return inspectStoragePreparationMembersRestore(ctx, root, owner, report, ownerLocal)
	}
	spec, scope, err := storagePreparationSnapshotSpec(ownerLocal, owner.Owner)
	if err != nil {
		return nil, err
	}
	profile, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	return durablehead.InspectRestore(ctx, root, owner, spec, profile, report)
}
