package ledger

// verify_extenders.go — check 11 and its parts of checks 3, 4 and 6
// (WHITEPAPER.md §8.6.4): which epochs carry an extender component, its
// census, and its share rows. Validators recompute the arithmetic only. The
// census is the operator's attestation of its extenders at end(k), published
// as keyed prefix hashes and never as addresses, so no validator can check
// which extenders are active, who controls them or their wallets; it checks
// that the census is well formed, that its head flags agree with the chain,
// and that the share rows are exactly the census's even split of the capped
// pool bytes.

import (
	"errors"
	"fmt"

	"github.com/urfoundation/sn/protocol"
)

// Runs check 11's first part: every covered epoch from ExtendersFrom that
// is not declared unrecoverable carries an extender component, and no
// other epoch carries one. It runs once the pending accounts are read, as
// check 10 does, so a dispute over which epochs the amendment covers is a
// refusal with the root reproduced, never a demand.
func (v *verifier) extenderDeclarations(h *Header) error {
	if v.in.ExtendersUnchecked {
		return nil
	}
	from := v.in.ExtendersFrom
	for i := range h.Epochs {
		epoch := &h.Epochs[i]
		covered := from != 0 && from <= epoch.Epoch && !epoch.Null
		switch {
		case covered && epoch.Extenders == nil:
			return fail(CheckExtenders, fmt.Errorf("%w: epoch %d, covered from epoch %d", ErrExtendersMissing, epoch.Epoch, from))
		case !covered && epoch.Extenders != nil:
			return fail(CheckExtenders, fmt.Errorf("%w: epoch %d", ErrExtendersUnexpected, epoch.Epoch))
		}
	}
	return nil
}

// Runs check 3 for extender components: an epoch's component, once custody
// holds it as valid, is fixed like its provider rows, and an epoch whose rows
// were fixed without one cannot gain one. It returns, per covered epoch,
// whether custody holds its valid component.
func (v *verifier) fixedExtenders(h *Header, held []bool) ([]bool, error) {
	heldExtenders := make([]bool, len(h.Epochs))
	for i := range h.Epochs {
		epoch := &h.Epochs[i]
		component, ok, err := v.in.Custody.ValidExtenders(v.ctx, v.in.NoID, epoch.Epoch)
		if err != nil {
			return nil, err
		}
		switch {
		case ok && !component.Equal(epoch.Extenders):
			return nil, fail(CheckFixedInputs, fmt.Errorf("%w: epoch %d extender component", ErrFixedInputs, epoch.Epoch))
		case !ok && held[i] && epoch.Extenders != nil:
			return nil, fail(CheckFixedInputs, fmt.Errorf("%w: epoch %d was fixed without an extender component", ErrFixedInputs, epoch.Epoch))
		}
		heldExtenders[i] = ok
	}
	return heldExtenders, nil
}

// Checks a covered epoch's extender component against its provider rows and
// returns its share rows: its census chunks (check 4), the census rules (11),
// the census head flags against the epoch's bindings (6), then that the share
// rows it lists are the split of its bytes, capped at the provider rows'
// eligible usage, over the census (11), and that they are published (4).
func (v *verifier) extenderRows(epoch *HeaderEpoch, providers []protocol.LedgerUsageRow) ([]protocol.LedgerUsageRow, error) {
	extenders := epoch.Extenders
	label := fmt.Sprintf("epoch %d census", epoch.Epoch)
	var census []CensusRow
	for j, ref := range extenders.Census {
		data, err := v.get(ref, &censusChunkRole)
		if err != nil {
			return nil, err
		}
		if err := checkChunk(len(data), CensusRowBytes, j, len(extenders.Census)); err != nil {
			return nil, fail(CheckChunks, fmt.Errorf("%s: %w", label, err))
		}
		census = decodeCensusRows(census, data)
	}
	if err := CheckCensusRows(census); err != nil {
		check := CheckExtenders
		if errors.Is(err, ErrCensusEncoding) {
			check = CheckChunks
		}
		return nil, fail(check, fmt.Errorf("%s: %w", label, err))
	}
	if err := v.censusHeads(epoch.Epoch, census, label); err != nil {
		return nil, err
	}
	split, err := SplitExtenderPool(extenders.Bytes, providers, census)
	if err != nil {
		return nil, fail(CheckExtenders, fmt.Errorf("epoch %d: %w", epoch.Epoch, err))
	}
	wantChunks := EncodeProviderChunks(split.Rows)
	if len(wantChunks) != len(extenders.Rows) {
		return nil, fail(CheckExtenders, fmt.Errorf("%w: epoch %d lists %d share chunks, not %d", ErrExtenderRows, epoch.Epoch, len(extenders.Rows), len(wantChunks)))
	}
	for j := range wantChunks {
		if RefOf(wantChunks[j]) != extenders.Rows[j] {
			return nil, fail(CheckExtenders, fmt.Errorf("%w: epoch %d share chunk %d", ErrExtenderRows, epoch.Epoch, j))
		}
	}
	// The rows are known now; reading them proves only that they are
	// published, as every object a header lists must be.
	for _, ref := range extenders.Rows {
		if _, err := v.get(ref, &providerChunkRole); err != nil {
			return nil, err
		}
	}
	return split.Rows, nil
}

// Runs check 6 on a census: a row's head-bound flag is set exactly for the
// candidates the epoch's bindings map to true, as a provider row's
// head_excluded is. Nil Heads skips it, as it skips check 6.
func (v *verifier) censusHeads(epoch uint64, census []CensusRow, label string) error {
	if v.in.Heads == nil || len(census) == 0 {
		return nil
	}
	boundClientIds, err := v.in.Heads.HeadBindings(v.ctx, epoch)
	if err != nil {
		return err
	}
	for i := range census {
		flagged := census[i].Flags&CensusHeadBound != 0
		switch want := boundClientIds[census[i].ClientID]; {
		case flagged && !want:
			return fail(CheckHeadExclusion, fmt.Errorf("%w: %s row %d", ErrHeadFlagSet, label, i))
		case !flagged && want:
			return fail(CheckHeadExclusion, fmt.Errorf("%w: %s row %d", ErrHeadFlagCleared, label, i))
		}
	}
	return nil
}
