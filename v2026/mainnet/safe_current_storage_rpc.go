// Native proof collection and scoped pending rechecks remain read-only. Neither
// helper supplies the mandatory complete-history capability used by submit.
package main

import (
	"context"
	"errors"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Every RPC owns a fresh bounded retry window under caller cancellation. A
// moving finalized tip does not invalidate the originally pinned canonical hash.
func (self *rpcClient) readSafeCurrentStorage(ctx context.Context, scope safeCurrentStorageScope, head chainIdentity) (*safeCurrentStorageObservation, error) {
	if ctx == nil {
		return nil, errors.New("Safe current proof requires a caller context")
	}
	if err := errors.Join(self.validateSnapshotIdentity(head), scope.validate()); err != nil {
		return nil, err
	}
	if head.runtimeVersion != scope.Runtime.RuntimeVersion {
		return nil, errors.New("Safe current proof snapshot runtime differs from signed scope")
	}
	witness := safeCurrentStorageWitness{At: head.FinalizedHash}
	if err := self.call(ctx, "chain_getHeader", []any{head.FinalizedHash}, &witness.Header); err != nil {
		return nil, err
	}
	if height, err := witness.Header.authenticate(head.FinalizedHash); err != nil || height != head.FinalizedNumber {
		return nil, errors.Join(errors.New("Safe current proof snapshot header differs"), err)
	}
	var proof struct {
		At    string   `json:"at"`
		Proof []string `json:"proof"`
	}
	// This exact method gets its own profile; the general read allowlist stays
	// unchanged. No pagination, tracing, mempool census or write is requested.
	if err := self.callAdmittedRead(ctx, "state_getReadProof", []any{scope.keys(), head.FinalizedHash}, &proof, false, 2*maximumSafeCurrentProofBytes+maxRpcReplyBytes); err != nil {
		return nil, err
	}
	witness.At, witness.Nodes = proof.At, proof.Proof
	observation, err := verifySafeCurrentStorage(ctx, scope, head.FinalizedHash, head.FinalizedNumber, witness)
	if err != nil {
		return nil, err
	}
	var canonical string
	if err := self.call(ctx, "chain_getBlockHash", []any{head.FinalizedNumber}, &canonical); err != nil {
		return nil, err
	}
	if canonical != head.FinalizedHash {
		return nil, errors.New("Safe current proof snapshot is no longer canonical")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return observation, nil
}

// This statement is deliberately weaker than a complete pending storage proof.
// The reviewed node creates a fresh overlay per call and exposes no proof root.
type safeCurrentPendingObservation struct {
	FinalizedObservationHash string `json:"finalized_observation_hash"`
	ScopedWordsMatched       bool   `json:"scoped_words_matched"`
	CompletePendingVerified  bool   `json:"complete_pending_verified"`
	SendAuthorized           bool   `json:"send_authorized"`
}

// Proof work must finish before this bounded recheck. Known words/code and
// explicit empty authority slots can detect changes, but cannot exclude a new
// pending orphan at an unknown hash or changes between independent RPC calls.
func (self *bootstrapSuccessorCanonicalChain) readSafeCurrentPending(ctx context.Context, scope safeCurrentStorageScope, finalized *safeCurrentStorageObservation) (*safeCurrentPendingObservation, error) {
	if ctx == nil || self == nil || self.chain == nil || finalized == nil || finalized.Schema != safeCurrentStorageSchema || !finalized.CompleteFinalizedStorage ||
		finalized.Safe != scope.Safe || finalized.Singleton != scope.Singleton || finalized.SafeProxyRuntimeHash != scope.SafeProxyRuntimeHash ||
		finalized.SingletonRuntimeHash != scope.SingletonRuntimeHash || finalized.RuntimeCodeHash != scope.Runtime.RuntimeCodeHash {
		return nil, errors.New("Safe scoped pending recheck lacks its finalized scope")
	}
	if err := scope.validate(); err != nil {
		return nil, err
	}
	for address, expected := range map[common.Address]common.Hash{scope.Safe: scope.SafeProxyRuntimeHash, scope.Singleton: scope.SingletonRuntimeHash} {
		code, err := self.code(ctx, address, "pending")
		if err != nil {
			return nil, err
		}
		if crypto.Keccak256Hash(code) != expected {
			return nil, errors.Join(errRpcIntegrity, errors.New("Safe scoped pending code changed"))
		}
	}
	expectedKVs := map[common.Hash]common.Hash{}
	entries := map[string][]byte{}
	for _, word := range finalized.Words {
		if _, exists := expectedKVs[word.Slot]; exists {
			return nil, errors.New("Safe scoped pending recheck repeats a finalized word")
		}
		expectedKVs[word.Slot] = word.Value
		entries[string(safeCurrentNativeStorageKey(scope.Safe, word.Slot))] = slices.Clone(word.Value[:])
	}
	if _, err := scope.words(entries); err != nil {
		return nil, err
	}
	slots := slices.Clone(scope.slots())
	for _, slot := range []byte{1, 2, 6, 7, 8} {
		slots = append(slots, common.BytesToHash([]byte{slot}))
	}
	for _, slot := range []string{"0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8", "0xb104e0b93118902c651344349b610029d694cfdec91c589c91ebafbcd0289947", "0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5"} {
		slots = append(slots, common.HexToHash(slot))
	}
	for _, slot := range slots {
		word, err := self.word(ctx, scope.Safe, slot, "pending")
		if err != nil {
			return nil, err
		}
		if word != expectedKVs[slot] {
			return nil, errors.Join(errRpcIntegrity, errors.New("Safe scoped pending authority word changed"))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &safeCurrentPendingObservation{FinalizedObservationHash: rootObjectHash(finalized), ScopedWordsMatched: true}, nil
}
