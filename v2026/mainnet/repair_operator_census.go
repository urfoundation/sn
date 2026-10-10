// Original all-status attempts and raw signatures remain accountable across
// restart. New observations never clear original uncertainty or grant a nonce.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/urnetwork/server/v2026/strecovery"
)

// The upper nonce endpoint may extend the observed interval, never its start.
// Every source, role, network and cumulative capacity field stays fixed.
func repairOperatorSameSelection(a, b strecovery.Config) bool {
	a.Roles, b.Roles = slices.Clone(a.Roles), slices.Clone(b.Roles)
	for index := range a.Roles {
		a.Roles[index].NextNonce = 0
	}
	for index := range b.Roles {
		b.Roles[index].NextNonce = 0
	}
	return rootObjectHash(a) == rootObjectHash(b)
}

// A later incident or observation cannot discard an original signed interval.
func repairOperatorExtendsSelection(original, current strecovery.Config) bool {
	if !repairOperatorSameSelection(original, current) {
		return false
	}
	for index, role := range original.Roles {
		if current.Roles[index].NextNonce < role.NextNonce {
			return false
		}
	}
	return true
}

// Preserve actual I/O causes; only a completed semantic refusal is integrity.
func repairOperatorCensusError(err error) error {
	if err == nil {
		return nil
	}
	var refusal *strecovery.Refusal
	if errors.As(err, &refusal) {
		return errors.Join(errRpcIntegrity, err)
	}
	if strecovery.CensusReadUnavailable(err) {
		return errors.Join(errRepairProcessPending, err)
	}
	return err
}

// A single observed snapshot per original database feeds the real collector.
// It is a read port with no status or nonce mutation method.
type repairOperatorSnapshots map[string]*strecovery.DatabaseSnapshot

// Only the already observed exact source may be consumed by the union replay.
func (self repairOperatorSnapshots) Snapshot(ctx context.Context, source strecovery.DatabaseSource, limits strecovery.Limits) (*strecovery.DatabaseSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value := self[source.Id]
	if value == nil {
		return nil, errors.New("operator continued census lost an original database snapshot")
	}
	return value, nil
}

// An observed new nonce extends only the postcondition's read inventory. Its
// original signer/source roster and lifetime capacity bounds remain unchanged.
func (self *repairOperatorCustody) continuedCensus(ctx context.Context) (*strecovery.Archive, error) {
	selection := self.original.Selection
	selection.Roles = slices.Clone(selection.Roles)
	snapshots := repairOperatorSnapshots{}
	for _, source := range selection.Databases {
		value, err := self.envelope.reader.Snapshot(ctx, source, selection.Limits)
		if err != nil {
			return nil, repairOperatorCensusError(err)
		}
		if value == nil {
			return nil, errors.New("operator continued database census returned no original rows")
		}
		snapshots[source.Id] = value
		for _, intent := range value.Intents {
			if intent.ChainId != int64(selection.ChainId) || intent.Genesis != selection.Genesis || intent.Nonce < 0 || intent.AttemptCount == 0 {
				continue
			}
			for index := range selection.Roles {
				role := &selection.Roles[index]
				if role.Address != intent.From || !slices.Contains(source.Roles, role.Id) {
					continue
				}
				nonce := uint64(intent.Nonce)
				if nonce == math.MaxUint64 || nonce < role.FirstNonce || nonce+1-role.FirstNonce > uint64(selection.Limits.MaximumAttempts) {
					return nil, errors.Join(errRepairProcessPending, errors.New("operator continued nonce census exceeds original read capacity"))
				}
				role.NextNonce = max(role.NextNonce, nonce+1)
			}
		}
	}
	current, err := strecovery.Collect(ctx, selection, snapshots)
	if err != nil {
		return nil, repairOperatorCensusError(err)
	}
	if err := retainRepairOperatorCensus(self.original, current); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	return current, nil
}

// Immutable intent fields and every prior raw attempt survive status changes,
// replacements and successful reconciliation. Existing final receipts cannot
// be erased; the separately retained original archive preserves uncertainty.
func retainRepairOperatorCensus(original, current *strecovery.Archive) error {
	if original == nil || current == nil || !repairOperatorExtendsSelection(original.Selection, current.Selection) || len(original.Databases) != len(current.Databases) || len(original.Stores) != len(current.Stores) {
		return errors.New("operator continued census changes original source membership")
	}
	intentIdentity := func(value strecovery.Intent) string {
		value.Status, value.CurrentHash, value.AttemptCount, value.Error, value.UpdateTime = "", nil, 0, nil, time.Time{}
		return rootObjectHash(value)
	}
	attemptIdentity := func(value strecovery.Attempt) string {
		value.Status, value.InclusionBlock, value.InclusionHash, value.FinalizedBlock, value.FinalizedHash, value.Error, value.UpdateTime = "", nil, nil, nil, nil, nil, time.Time{}
		return rootObjectHash(value)
	}
	for index, prior := range original.Databases {
		next := current.Databases[index]
		if prior.Source != next.Source {
			return errors.New("operator continued database source order differs")
		}
		intents := map[string]strecovery.Intent{}
		for _, value := range next.Snapshot.Intents {
			intents[value.Id] = value
		}
		for _, value := range prior.Snapshot.Intents {
			retained, exists := intents[value.Id]
			if !exists || intentIdentity(value) != intentIdentity(retained) || retained.AttemptCount < value.AttemptCount || retained.UpdateTime.Before(value.UpdateTime) {
				return errors.New("operator continued census lost or changed an original transaction intent")
			}
		}
		attempts := map[string]strecovery.Attempt{}
		for _, value := range next.Snapshot.Attempts {
			attempts[fmt.Sprintf("%s/%d", value.IntentId, value.Number)] = value
		}
		for _, value := range prior.Snapshot.Attempts {
			retained, exists := attempts[fmt.Sprintf("%s/%d", value.IntentId, value.Number)]
			if !exists || attemptIdentity(value) != attemptIdentity(retained) || retained.UpdateTime.Before(value.UpdateTime) || value.FinalizedHash != nil && (rootObjectHash(value.FinalizedHash) != rootObjectHash(retained.FinalizedHash) || rootObjectHash(value.FinalizedBlock) != rootObjectHash(retained.FinalizedBlock)) {
				return errors.New("operator continued census lost original signed bytes, fee envelope or final receipt")
			}
		}
	}
	for index, prior := range original.Stores {
		next := current.Stores[index]
		if prior.Source != next.Source {
			return errors.New("operator continued evidence store identity differs")
		}
		files := map[string][]byte{}
		for _, file := range next.Files {
			files[file.Name] = file.Raw
		}
		for _, file := range prior.Files {
			if !bytes.Equal(file.Raw, files[file.Name]) {
				return errors.New("operator continued store lost or changed original signed evidence")
			}
		}
	}
	return nil
}
