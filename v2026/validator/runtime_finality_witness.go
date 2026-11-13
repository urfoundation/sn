// Runtime reads retain their original finality witnesses through transport
// replay. A lagging node supplies unavailable evidence, never a replacement pin.
package validator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

type runtimeFinalityWitness struct {
	hash   types.Hash
	number uint64
}

// Both are local to one bounded public read. Reconnect can advance the latest
// witness, but cannot discard the original finalized authority it already used.
type runtimeFinalityObservation struct {
	original runtimeFinalityWitness
	latest   runtimeFinalityWitness
}

func withRuntimeFinalityOwner(ctx context.Context) context.Context {
	return crv4.WithFinalityReadOwnerContext(ctx)
}

// Nullable results distinguish a missing RPC response from a returned hash
// that contradicts the original identity. Short padded hashes are not evidence.
func readRuntimeWitnessHash(ctx context.Context, native *crv4.Chain, field, method string, args ...any) (types.Hash, error) {
	var encoded *string
	if err := native.API.Client.CallContext(ctx, &encoded, method, args...); err != nil {
		return types.Hash{}, err
	}
	if encoded == nil || *encoded == "" {
		return types.Hash{}, &crv4.ReceiptEvidenceUnavailableError{Field: field}
	}
	if len(*encoded) != 66 || !strings.HasPrefix(*encoded, "0x") {
		return types.Hash{}, fmt.Errorf("runtime observation %s is not an exact native hash", field)
	}
	hash, err := types.NewHashFromHexString(*encoded)
	if err != nil || hash == (types.Hash{}) {
		return types.Hash{}, errors.Join(fmt.Errorf("runtime observation %s is invalid", field), err)
	}
	// A completed hash remains evidence even if cancellation arrives just
	// afterward. The caller compares it before rejecting a late success.
	return hash, nil
}

// Complete SCALE header authentication binds the height used by every later
// comparison. The canonical read below independently binds its native hash.
func readRuntimeFinalityWitness(ctx context.Context, native *crv4.Chain) (runtimeFinalityWitness, error) {
	hash, err := readRuntimeWitnessHash(ctx, native, "finalized head", "chain_getFinalizedHead")
	if err != nil {
		return runtimeFinalityWitness{}, err
	}
	number, _, err := native.ReceiptHeaderAtContext(ctx, hash)
	if err != nil {
		return runtimeFinalityWitness{}, err
	}
	return runtimeFinalityWitness{hash: hash, number: number}, nil
}

// Check all returned canonical identities before interpreting a lower head as
// unavailable. A real hash change must not be hidden by lagging finality.
func checkRuntimeCanonicalWitnesses(ctx context.Context, native *crv4.Chain, block types.Hash, current runtimeFinalityWitness, retained ...runtimeFinalityWitness) error {
	for _, witness := range retained {
		if witness.number == current.number && witness.hash != current.hash {
			return errors.Join(errors.New("runtime observation completed finalized witnesses disagree at the same height"), ctx.Err())
		}
	}
	for index, witness := range append([]runtimeFinalityWitness{current}, retained...) {
		canonical, err := readRuntimeWitnessHash(ctx, native, "canonical block hash", "chain_getBlockHash", witness.number)
		if err != nil {
			return err
		}
		if canonical != witness.hash {
			return errors.Join(errors.New("runtime observation block is not canonical at its original height"), ctx.Err())
		}
		if index == 0 {
			if err := crv4.CheckRetainedFinalityReadWitnessContext(ctx, native, block, current.hash, current.number); err != nil {
				return err
			}
		}
	}
	return nil
}

// Successful checks retain both the first witness and highest admitted head.
// The original selected block is always independently checked at its own height.
func (self *runtimeFinalityObservation) check(ctx context.Context, native *crv4.Chain, current, block runtimeFinalityWitness) error {
	retained := []runtimeFinalityWitness{block}
	if self.original.hash != (types.Hash{}) && self.original.hash != current.hash {
		retained = append(retained, self.original)
	}
	if self.latest.hash != (types.Hash{}) && self.latest.hash != current.hash && self.latest.hash != self.original.hash {
		retained = append(retained, self.latest)
	}
	if err := checkRuntimeCanonicalWitnesses(ctx, native, block.hash, current, retained...); err != nil {
		return err
	}
	if err := crv4.RetainFinalityReadWitnessContext(ctx, native, block.hash, current.hash, current.number); err != nil {
		return err
	}
	for _, witness := range retained {
		if current.number < witness.number {
			return errors.Join(&crv4.ReceiptEvidenceUnavailableError{BlockHash: witness.hash, Field: "finalized head through original runtime witness"}, ctx.Err())
		}
	}
	if self.original.hash == (types.Hash{}) {
		self.original = current
	}
	self.latest = current
	return nil
}

// The closing head can advance, but neither its header nor finality may replace
// the selected block or either previously admitted finalized witness.
func (self *runtimeFinalityObservation) close(ctx context.Context, native *crv4.Chain, block runtimeFinalityWitness) error {
	closing, err := readRuntimeFinalityWitness(ctx, native)
	if err != nil {
		return err
	}
	return self.check(ctx, native, closing, block)
}
