// The original SQL window inventory is checked independently against the earning
// rows and caller-owned chain clock. SQL completeness is not physical-work truth.
package payoutartifact

import (
	"bytes"
	"context"
	"errors"
	"math"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

const ClosedWorkWindowSchema = "urnetwork-closed-work-window-inventory-v1"

// Canceled and open contracts remain explicit non-credit observations. A
// canceled row without a close clock cannot be assigned away from this window.
type ClosedWorkWindowRecord struct {
	ContractId  string  `json:"contract_id"`
	Disposition string  `json:"disposition"`
	ClosedAt    *string `json:"closed_at"`
	Original    []byte  `json:"original_snapshot"`
}

type ClosedWorkWindow struct {
	Schema  string                   `json:"schema"`
	Start   string                   `json:"start"`
	End     string                   `json:"end"`
	Records []ClosedWorkWindowRecord `json:"records"`
}

// Only an independently read original epoch boundary may populate this input.
// No boolean or artifact-supplied assertion can promote it to clock authority.
type ClosedWorkWindowClock struct {
	HeaderProfile string    `json:"header_profile,omitempty"`
	Start         Boundary  `json:"start"`
	End           Boundary  `json:"end"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	StartHeader   []byte    `json:"start_header,omitempty"`
	EndHeader     []byte    `json:"end_header,omitempty"`
}

// Reconstruct timestamps from exact header bytes whose hashes are the original
// artifact boundaries. A self-sealed clock counter cannot replace this evidence.
func (self *ClosedWorkWindowClock) matches(ctx context.Context, artifact *Artifact, start, end time.Time) bool {
	if artifact == nil {
		return false
	}
	return self.matchesBoundaries(ctx, artifact.Start, artifact.End, start, end)
}

// The publisher shares the exact raw-header checks with the full consumer.
// Its containing owner must independently admit the authority before this call;
// authenticating a clock supplies neither SDK completeness nor source authority.
func VerifyWholeWorkWindowClock(ctx context.Context, authority WholeWorkAuthority, clock *ClosedWorkWindowClock) error {
	if ctx == nil || clock == nil {
		return ErrClosedWorkUnavailable
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, context.Cause(ctx))
	}
	if clock.Start != authority.Start || clock.End != authority.End || clock.HeaderProfile != authority.ClockProfile {
		return ErrClosedWorkIntegrity
	}
	if !clock.matchesBoundaries(ctx, authority.Start, authority.End, clock.StartTime, clock.EndTime) {
		return errors.Join(ErrClosedWorkUnavailable, ctx.Err(), context.Cause(ctx))
	}
	return errors.Join(ctx.Err(), context.Cause(ctx))
}

// Boundary-based input avoids manufacturing an unsigned artifact merely to
// authenticate the already independently selected original header pair.
func (self *ClosedWorkWindowClock) matchesBoundaries(ctx context.Context, startBoundary, endBoundary Boundary, start, end time.Time) bool {
	if ctx == nil || self == nil || self.Start != startBoundary || self.End != endBoundary || !self.StartTime.Equal(start) || !self.EndTime.Equal(end) || !self.StartTime.Before(self.EndTime) {
		return false
	}
	for _, original := range []struct {
		raw      []byte
		boundary Boundary
		clock    time.Time
	}{{raw: self.StartHeader, boundary: self.Start, clock: self.StartTime}, {raw: self.EndHeader, boundary: self.End, clock: self.EndTime}} {
		if ctx.Err() != nil || len(original.raw) == 0 || len(original.raw) > 64*1024 {
			return false
		}
		var header types.Header
		if rlp.DecodeBytes(original.raw, &header) != nil || header.Number == nil || !header.Number.IsUint64() || header.Number.Uint64() != original.boundary.Number || header.Hash().Hex() != original.boundary.Hash {
			return false
		}
		seconds := header.Time
		switch self.HeaderProfile {
		case "":
		case FrontierWindowClockProfile:
			content, rest, err := rlp.SplitList(original.raw)
			count, countErr := rlp.CountValues(content)
			if err != nil || countErr != nil || len(rest) != 0 || count != 15 {
				return false
			}
			// The existing epoch policy uses the public JSON seconds projection.
			// Raw Frontier stores milliseconds; their exact remainder stays hashed.
			seconds /= 1000
		default:
			return false
		}
		if seconds > math.MaxInt64 || !time.Unix(int64(seconds), 0).UTC().Equal(original.clock) {
			return false
		}
	}
	return true
}

// This is deliberately not a ProviderMeasurementsAuthenticated certificate.
// Whole contract admission and original reliability/eligibility remain absent.
type VerifiedClosedWorkWindow struct {
	Hash               string
	Credited           uint64
	Canceled           uint64
	Open               uint64
	UnassignedCanceled uint64
	EpochClockMatched  bool
}

// Compare the complete sorted identities and exact original source bytes. An
// absent future component is unknown; a present contradictory census is refused.
func VerifyClosedWorkWindow(ctx context.Context, artifact *Artifact, window *ClosedWorkWindow, clock *ClosedWorkWindowClock) (*VerifiedClosedWorkWindow, error) {
	if ctx == nil {
		return nil, errors.New("closed-work window requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if window == nil || window.Schema != ClosedWorkWindowSchema {
		return nil, ErrClosedWorkUnavailable
	}
	if artifact == nil || artifact.ClosedWork == nil || window.Records == nil {
		return nil, ErrClosedWorkUnavailable
	}
	if len(window.Records) > MaxClosedWorkRecords {
		return nil, ErrClosedWorkCapacity
	}
	start, e1 := time.Parse(time.RFC3339Nano, window.Start)
	end, e2 := time.Parse(time.RFC3339Nano, window.End)
	claimedStart, e3 := time.Parse(time.RFC3339Nano, artifact.ClosedWork.WindowStart)
	claimedEnd, e4 := time.Parse(time.RFC3339Nano, artifact.ClosedWork.WindowEnd)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !start.Before(end) || !start.Equal(claimedStart) || !end.Equal(claimedEnd) {
		return nil, ErrClosedWorkIntegrity
	}
	rows := make(map[[16]byte]ClosedWorkRecord, len(artifact.ClosedWork.Records))
	for _, row := range artifact.ClosedWork.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := rows[row.ContractId]; ok {
			return nil, ErrClosedWorkIntegrity
		}
		rows[row.ContractId] = row
	}
	result := &VerifiedClosedWorkWindow{}
	var prior [16]byte
	used := 0
	for index, row := range window.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, err := closedWorkId(row.ContractId)
		if err != nil || index > 0 && bytes.Compare(prior[:], id[:]) >= 0 {
			return nil, ErrClosedWorkIntegrity
		}
		prior = id
		if len(row.Original) > MaxClosedWorkRecordBytes || len(row.Original) > MaxClosedWorkOriginalBytes-used {
			return nil, ErrClosedWorkCapacity
		}
		used += len(row.Original)
		var closed time.Time
		if row.ClosedAt != nil {
			closed, err = time.Parse(time.RFC3339Nano, *row.ClosedAt)
			if err != nil {
				return nil, ErrClosedWorkIntegrity
			}
		}
		original, credited := rows[id]
		switch row.Disposition {
		case "credited":
			actual, e := time.Parse(time.RFC3339Nano, original.ClosedAt)
			if !credited || row.ClosedAt == nil || e != nil || !actual.Equal(closed) || closed.Before(start) || !closed.Before(end) || !bytes.Equal(original.Original, row.Original) {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("window credit differs from exact original closure"))
			}
			delete(rows, id)
			result.Credited++
		case "canceled":
			if credited || len(row.Original) != 0 || row.ClosedAt == nil || closed.Before(start) || !closed.Before(end) {
				return nil, ErrClosedWorkIntegrity
			}
			result.Canceled++
		case "open", "unassigned_canceled":
			if credited || len(row.Original) != 0 || row.ClosedAt != nil {
				return nil, ErrClosedWorkIntegrity
			}
			if row.Disposition == "open" {
				result.Open++
			} else {
				result.UnassignedCanceled++
			}
		default:
			return nil, ErrClosedWorkIntegrity
		}
	}
	if len(rows) != 0 {
		return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("window omitted an original earning contract"))
	}
	if clock.matches(ctx, artifact, start, end) {
		result.EpochClockMatched = true
	}
	result.Hash = SnapshotHash(window)
	return result, ctx.Err()
}
