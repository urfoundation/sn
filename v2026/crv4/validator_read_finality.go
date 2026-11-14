// Nested validator observations close finality after their own dependent reads.
// All witnesses remain tied to the selected block and its original read owner.
package crv4

import (
	"context"
	"errors"
)

// Authenticate a fresh head, then recheck the original canonical constraints
// before treating lower coverage as unavailable. No lower head selects a block.
func closeValidatorReadFinalityContext(ctx context.Context, chain *Chain, selected, original finalityReadWitness) error {
	current, number, err := readFinalityReadWitnessContext(ctx, chain, selected.hash)
	if err != nil {
		return err
	}
	for index, witness := range []finalityReadWitness{selected, original} {
		if witness.hash == current || index != 0 && witness.hash == selected.hash {
			continue
		}
		if err := checkFinalityReadCanonicalContext(ctx, chain, witness.hash, witness.number); err != nil {
			return errors.Join(err, ctx.Err())
		}
	}
	if err := RetainFinalityReadWitnessContext(ctx, chain, selected.hash, current, number); err != nil {
		return err
	}
	if number < selected.number || number < original.number {
		return errors.Join(&ReceiptEvidenceUnavailableError{BlockHash: selected.hash, Field: "validator read closing finalized coverage"}, ctx.Err())
	}
	return ctx.Err()
}
