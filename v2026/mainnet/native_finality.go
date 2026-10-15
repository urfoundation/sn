// A finalized read retains its opening witness independently of the selected
// historical block. Canonical membership alone does not establish finality.
package main

import (
	"context"
	"fmt"
	"strings"
)

// This private point is authenticated from the owned route's complete header.
// It is not consensus proof or an importable authority envelope.
type nativeFinalityPoint struct {
	Number uint64
	Hash   string
}

// Current finality is read without recursively selecting runtime or mapping
// evidence. Every call stays inside its caller's original observation deadline.
func (self *rpcClient) readNativeFinality(ctx context.Context) (nativeFinalityPoint, error) {
	var point nativeFinalityPoint
	if err := self.call(ctx, "chain_getFinalizedHead", []any{}, &point.Hash); err != nil {
		return nativeFinalityPoint{}, err
	}
	point.Hash = strings.ToLower(point.Hash)
	if !rootCanonicalHash(point.Hash) {
		return nativeFinalityPoint{}, fmt.Errorf("%w: native finalized head hash is invalid", errRpcIntegrity)
	}
	var header rootReceiptHeader
	if err := self.call(ctx, "chain_getHeader", []any{point.Hash}, &header); err != nil {
		return nativeFinalityPoint{}, err
	}
	header.normalizeHashes()
	var err error
	point.Number, err = header.authenticate(point.Hash)
	if err != nil {
		return nativeFinalityPoint{}, fmt.Errorf("%w: native finalized header: %v", errRpcIntegrity, err)
	}
	if err := self.checkNativeCanonical(ctx, point); err != nil {
		return nativeFinalityPoint{}, err
	}
	return point, nil
}

// A transport failure remains its original read cause. Only an actual returned
// contradiction is classified as integrity failure; nothing retries a write.
func (self *rpcClient) checkNativeCanonical(ctx context.Context, point nativeFinalityPoint) error {
	var hash string
	if err := self.call(ctx, "chain_getBlockHash", []any{point.Number}, &hash); err != nil {
		return err
	}
	hash = strings.ToLower(hash)
	if !rootCanonicalHash(hash) || !strings.EqualFold(hash, point.Hash) {
		return fmt.Errorf("%w: native finalized header number does not resolve to its authenticated hash", errRpcIntegrity)
	}
	return ctx.Err()
}

// In-memory composition retains the opening finality witness after selecting a
// historical block. Serialized observations cannot reconstruct this authority.
func (self *rpcClient) closeSnapshotFinality(ctx context.Context, identity chainIdentity) error {
	if err := self.validateSnapshotIdentity(identity); err != nil {
		return err
	}
	if !rootCanonicalHash(identity.finalityWitness.Hash) {
		return fmt.Errorf("%w: snapshot lost its authenticated native finality witness", errRpcIntegrity)
	}
	return self.closeNativeFinality(ctx, identity.finalityWitness, nativeFinalityPoint{Number: identity.FinalizedNumber, Hash: identity.FinalizedHash})
}

// Normal advancement preserves the original selected snapshot. A lagging read
// waits for coverage; changed opening or selected hashes invalidate the read.
func (self *rpcClient) closeNativeFinality(ctx context.Context, opening, selected nativeFinalityPoint) error {
	if selected.Number > opening.Number {
		return fmt.Errorf("%w: selected snapshot exceeds its original native finality witness", errRpcIntegrity)
	}
	_, err := self.readNativeFinalityCovering(ctx, opening, selected)
	return err
}
