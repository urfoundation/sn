// Report-chain completeness is independently reconstructed from client originals.
// It is narrower than whole-window contract admission or physical traffic truth.
package payoutartifact

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"sort"

	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Missing companions remain unknown. Present contradictions are refused even
// when another report is missing, so an omission cannot conceal a forged link.
func verifyClosedReportInventory(ctx context.Context, census ClosedWorkReports) (bool, uint64, error) {
	type entry struct {
		value    coreprotocol.OriginalCloseInventory
		original coreprotocol.OriginalCloseReport
		hash     [32]byte
	}
	var parties [2][]entry
	complete := len(census.Reports) > 0
	var count uint64
	for _, report := range census.Reports {
		if err := ctx.Err(); err != nil {
			return false, 0, err
		}
		if len(report.Inventory) == 0 {
			complete = false
			continue
		}
		if census.Schema != ClosedWorkInventoryReportsSchema {
			return false, 0, errors.Join(ErrClosedWorkIntegrity, errors.New("inventory requires its explicit optional component version"))
		}
		value, err := coreprotocol.DecodeOriginalCloseInventory(report.Inventory)
		original, originalErr := coreprotocol.DecodeOriginalCloseReport(report.Original)
		if err != nil || originalErr != nil || !value.Matches(original) {
			return false, 0, errors.Join(ErrClosedWorkIntegrity, err, originalErr)
		}
		party := 0
		if report.Party == "destination" {
			party = 1
		} else if report.Party != "source" {
			return false, 0, ErrClosedWorkIntegrity
		}
		parties[party] = append(parties[party], entry{value: value, original: original, hash: sha256.Sum256(report.Inventory)})
		count++
	}
	for _, entries := range parties {
		if len(entries) == 0 {
			complete = false
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].value.Sequence < entries[j].value.Sequence })
		for index, entry := range entries {
			if err := ctx.Err(); err != nil {
				return false, 0, err
			}
			v := entry.value
			if index > 0 && (v.Sequence == entries[index-1].value.Sequence || entries[index-1].value.Terminal) {
				return false, 0, errors.Join(ErrClosedWorkIntegrity, errors.New("original inventory duplicates a position or continues after terminal"))
			}
			if v.Sequence != uint64(index+1) {
				complete = false
			}
			if v.Sequence == 1 && (v.Previous != ([32]byte{}) || v.CumulativeAckedBytes != entry.original.AckedByteCount) {
				return false, 0, errors.Join(ErrClosedWorkIntegrity, errors.New("original inventory first cumulative work differs"))
			}
			// A missing earlier link cannot conceal a contradiction between two
			// present consecutive links; completeness and integrity stay separate.
			if index > 0 && entries[index-1].value.Sequence+1 == v.Sequence {
				prior := entries[index-1]
				if entry.original.AckedByteCount > math.MaxUint64-prior.value.CumulativeAckedBytes || v.Previous != prior.hash || v.CumulativeAckedBytes != prior.value.CumulativeAckedBytes+entry.original.AckedByteCount {
					return false, 0, errors.Join(ErrClosedWorkIntegrity, errors.New("original inventory predecessor or cumulative work differs"))
				}
			}
		}
		if !entries[len(entries)-1].value.Terminal {
			complete = false
		}
	}
	return complete, count, ctx.Err()
}
