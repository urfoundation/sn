//go:build linux || darwin

// Offline preparation accepts public owner schemas and exact reviewed plans.
// No RPC, signing key, runtime constructor or service manager is reachable.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The command chooses one fixed registry scope before parsing any owner input.
// Unsupported retained/restore formats never become empty state.
func storagePreparationAdapter(ownerLocal bool) durablevolume.PreparationAdapter {
	return durablevolume.PreparationAdapter{
		Build: func(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner) (durablevolume.PreparationOwnerPlan, error) {
			return buildStoragePreparationFixedOwner(ctx, staging, name, owner, ownerLocal)
		},
		Inspect: func(ctx context.Context, target *os.File, owner durablevolume.PreparationOwnerPlan) ([]durablevolume.PreparedAttribute, error) {
			return inspectStoragePreparationFixedOwner(ctx, target, owner, ownerLocal)
		},
		Restore: func(ctx context.Context, name string, owner durablevolume.PreparationOwner, inventory durablevolume.Inventory) (durablevolume.PreparationOwnerPlan, error) {
			return planStoragePreparationRestore(ctx, name, owner, inventory, ownerLocal)
		},
		InspectRestore: func(ctx context.Context, target *os.File, owner durablevolume.PreparationOwnerPlan, inventory durablevolume.Inventory) ([]durablevolume.PreparedAttribute, error) {
			return inspectStoragePreparationRestore(ctx, target, owner, inventory, ownerLocal)
		},
		RebindRestore: func(ctx context.Context, owner durablevolume.PreparationOwnerPlan, inventory durablevolume.Inventory, original []byte, targets []durablevolume.PreparationSource) ([]byte, error) {
			if owner.Owner.Kind == storageSdkWorkKind {
				return rebindStorageSdkWorkRestore(ctx, owner, inventory, original, targets, ownerLocal)
			}
			return rebindStoragePreparationMembersRestore(ctx, owner, inventory, original, targets, ownerLocal)
		},
	}
}

// Only public ledger identity and finite limits can create a fresh staging bundle.
func buildStoragePreparationOwner(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner) (durablevolume.PreparationOwnerPlan, error) {
	if owner.Kind != validator.AttemptLedgerPreparationKind || owner.Purpose != "fresh" || owner.RelativePath != "." {
		return durablevolume.PreparationOwnerPlan{}, errors.New("storage preparation owner kind/purpose is not in the implemented fixed registry")
	}
	var scope validator.AttemptLedgerPreparationScope
	if err := decodePlanJson(owner.Inputs, &scope); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	census, err := validator.BuildFreshAttemptLedgerPreparation(ctx, staging, name, scope)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	raw, err := json.Marshal(census)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	files := make([]durablevolume.PreparationFile, 0, len(census.Files))
	for _, file := range census.Files {
		files = append(files, durablevolume.PreparationFile{Path: file.Path, Kind: file.Kind, Mode: file.Mode, Bytes: file.Bytes, Sha256: file.Sha256})
	}
	attributes := []durablevolume.PreparationAttributeSpec{{Path: ".", Name: "user.urnetwork.attempt-ledger-custody"}}
	if scope.Requests != nil {
		attributes = append(attributes, durablevolume.PreparationAttributeSpec{Path: ".", Name: validator.ProviderAttemptRequestAttribute})
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, Files: files, Census: raw, Attributes: attributes}, nil
}

// The ledger verifies exact public identity/head and every signed record before
// returning inode-bound checkpoint bytes. Publication belongs to the base stage.
func inspectStoragePreparationOwner(ctx context.Context, target *os.File, owner durablevolume.PreparationOwnerPlan) ([]durablevolume.PreparedAttribute, error) {
	if owner.Owner.Kind != validator.AttemptLedgerPreparationKind || owner.Owner.Purpose != "fresh" || owner.Owner.RelativePath != "." {
		return nil, errors.New("storage preparation cannot inspect an unknown owner kind")
	}
	var scope validator.AttemptLedgerPreparationScope
	var census validator.AttemptLedgerPreparationCensus
	if err := errors.Join(decodePlanJson(owner.Owner.Inputs, &scope), decodePlanJson(owner.Census, &census)); err != nil {
		return nil, err
	}
	expected := durablevolume.PreparationAttributeSpec{Path: ".", Name: "user.urnetwork.attempt-ledger-custody"}
	count := 1
	requestSpec := durablevolume.PreparationAttributeSpec{Path: ".", Name: validator.ProviderAttemptRequestAttribute}
	if scope.Requests != nil {
		count++
	}
	if len(owner.Attributes) != count || owner.Attributes[0] != expected || count == 2 && owner.Attributes[1] != requestSpec {
		return nil, errors.New("prepared ledger changed its fixed checkpoint destination")
	}
	raw, err := validator.BuildAttemptLedgerPreparationCheckpoint(ctx, target, scope, census)
	if err != nil {
		return nil, err
	}
	attributes := []durablevolume.PreparedAttribute{{Spec: expected, Raw: raw}}
	if scope.Requests != nil {
		requestRaw, err := validator.BuildAttemptLedgerPreparationRequestCheckpoint(ctx, target, scope, census)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, durablevolume.PreparedAttribute{Spec: requestSpec, Raw: requestRaw})
	}
	return attributes, nil
}

// The independently selected command fixes daemon or owner-local scope before
// parsing any policy. Only apply accepts an exact accepted plan digest.
func runStoragePreparationCommand(ctx context.Context, args []string, stdout, stderr io.Writer, ownerLocal bool) int {
	if len(args) != 0 && (args[0] == "cohort-check" || args[0] == "cohort-apply" || args[0] == "cohort-config") {
		return runStoragePreparationCohort(ctx, args, stdout, stderr, ownerLocal)
	}
	if len(args) != 0 && args[0] == "export" {
		return runStoragePreparationExport(ctx, args[1:], stdout, stderr, ownerLocal)
	}
	if len(args) == 0 || args[0] != "plan" && args[0] != "apply" {
		fmt.Fprintln(stderr, "usage: storage-prepare plan --request FILE --request-sha256 HASH | apply --plan FILE --plan-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("storage-prepare "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path, hash := "", ""
	if mode == "plan" {
		flags.StringVar(&path, "request", "", "exact public preparation request")
		flags.StringVar(&hash, "request-sha256", "", "accepted request sha256")
	} else {
		flags.StringVar(&path, "plan", "", "exact reviewed preparation plan")
		flags.StringVar(&hash, "plan-sha256", "", "accepted plan sha256")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || path == "" || hash == "" {
		fmt.Fprintln(stderr, "preparation requires only its explicit local file and exact digest")
		return 2
	}
	if ctx == nil || ctx.Err() != nil {
		fmt.Fprintln(stderr, "preparation context is unavailable")
		return 2
	}
	reference := durablevolume.Reference{Path: path, Sha256: hash}
	adapter := storagePreparationAdapter(ownerLocal)
	var result any
	var err error
	if mode == "plan" {
		operation := durablepath.PlanPreparation
		if ownerLocal {
			operation = durablepath.PlanOwnerLocalPreparation
		}
		result, err = operation(ctx, reference, adapter)
	} else {
		operation := durablepath.ApplyPreparation
		if ownerLocal {
			operation = durablepath.ApplyOwnerLocalPreparation
		}
		result, err = operation(ctx, reference, adapter)
	}
	if err != nil {
		fmt.Fprintln(stderr, "storage preparation:", err)
		return 2
	}
	raw, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintln(stderr, "preparation report:", err)
		return 2
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "preparation report was not fully delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
