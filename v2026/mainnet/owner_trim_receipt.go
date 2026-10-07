// Trim receipt recovery uses complete finalized native bodies and phase-bound
// dispatch/fee events. Census gaps cannot erase an authenticated financial result.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// The fixed approved route is shared by bounded reads and at-most-once posts.
// Construction never installs authority or a signer from observed data.
type ownerTrimCanonicalChain struct {
	*rootCanonicalChain
	config    ownerTrimExecutionConfig
	key       string
	policy    subnetCensusPolicy
	authority ownerTrimAuthority
}

// A nil authority supports recovery while making direct submission unavailable.
func newOwnerTrimCanonicalChain(config ownerTrimExecutionConfig, key string, policy subnetCensusPolicy, authority ownerTrimAuthority) (*ownerTrimCanonicalChain, error) {
	if err := errors.Join(config.validate(key), policy.validate()); err != nil {
		return nil, err
	}
	a := config.Action
	if a.Network.NativeChain != policy.NativeChain || a.Network.GenesisHash != policy.GenesisHash || a.Network.EvmChainId != policy.EvmChainId ||
		a.Coldkey != policy.SubnetOwnerColdkey || a.SubnetGeneration != *policy.SubnetGeneration || a.SubnetRegistrationBlock != *policy.SubnetRegistrationBlock ||
		a.Runtime.RuntimeSourceCommit != policy.RuntimeSourceCommit || a.Runtime.RuntimeVersion != policy.RuntimeVersion || a.Runtime.RuntimeCodeHash != policy.RuntimeCodeHash || a.Runtime.RuntimeMetadataHash != policy.RuntimeMetadataHash {
		return nil, errors.New("owner trim canonical policy differs from independently approved action")
	}
	client, err := newOwnedSubmissionClient(config.Route)
	if err != nil {
		return nil, err
	}
	native, err := newRootCanonicalChain(client, identityExpectation{NativeChain: a.Network.NativeChain, GenesisHash: a.Network.GenesisHash, EvmChainId: a.Network.EvmChainId}, []rootReceiptProfile{a.Runtime})
	if err != nil {
		return nil, err
	}
	// Decouple caller-owned policy pointers/slices before any concurrent reads.
	copyRaw, _ := json.Marshal(policy)
	var copied subnetCensusPolicy
	if err := decodePlanJson(copyRaw, &copied); err != nil {
		return nil, err
	}
	return &ownerTrimCanonicalChain{rootCanonicalChain: native, config: config, key: key, policy: copied, authority: authority}, nil
}

// Recheck the complete action, canonical history and independent authority even
// when called directly. The executor has already synced the numbered attempt.
func (self *ownerTrimCanonicalChain) submit(ctx context.Context, config ownerTrimExecutionConfig, raw []byte) error {
	if err := config.validate(self.key); err != nil || rootObjectHash(config) != rootObjectHash(self.config) || len(raw) == 0 {
		return errors.Join(errors.New("owner trim submission names another approved action or route"), err)
	}
	if err := ownerTrimSignedAction(config.Action, raw); err != nil {
		return err
	}
	if self.authority == nil {
		return errors.New("owner trim submission requires independent current window and global custody authority")
	}
	evidence, err := self.reconcile(ctx, config.Action, raw)
	if err != nil {
		return err
	}
	if ownerTrimTerminalPhase(config.Action, evidence, true) != "" {
		return nil
	}
	observation := evidence.Observation
	if observation.AccountNonce == nil || *observation.AccountNonce != config.Action.Nonce || observation.FinalizedNumber >= config.Action.BirthBlock+config.Action.Period || observation.Census == nil || observation.Issue != "" {
		return errors.New("owner trim owned submission current nonce, census or era is unavailable")
	}
	if err := self.authority.authorize(ctx, config, evidence); err != nil {
		return err
	}
	if err := self.network(ctx); err != nil {
		return err
	}
	_, err = ownedSubmissionPost(ctx, self.client, config.Route, "author_submitExtrinsic", "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw))
	return err
}

// The scan is complete, bounded and anchored by authenticated parent links.
// No subscription, pool rejection or nonce change substitutes for exact inclusion.
func (self *ownerTrimCanonicalChain) reconcile(ctx context.Context, action ownerTrimAction, signed []byte) (ownerTrimActionReconciliation, error) {
	var result ownerTrimActionReconciliation
	if ctx == nil {
		return result, errors.New("owner trim receipt requires context")
	}
	select {
	case self.reconcileCh <- struct{}{}:
		defer func() { <-self.reconcileCh }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	if err := errors.Join(action.validate(), ownerTrimSignedAction(action, signed)); err != nil || action.RequestHash != self.config.Action.RequestHash {
		return result, errors.Join(errors.New("owner trim reconciliation names another approved action"), err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := self.network(operationCtx); err != nil {
		return result, err
	}
	var finalized string
	if err := self.client.call(operationCtx, "chain_getFinalizedHead", []any{}, &finalized); err != nil {
		return result, err
	}
	header, number, err := self.header(operationCtx, finalized)
	if err != nil {
		return result, err
	}
	if number < action.BirthBlock || number-action.BirthBlock > rootAncestryLimit {
		return result, errors.New("owner trim finalized ancestry exceeds recovery bound or predates anchor")
	}
	headers, hashes := map[uint64]rootReceiptHeader{number: header}, map[uint64]string{number: finalized}
	for height := number; height > action.BirthBlock; height-- {
		parent := headers[height].ParentHash
		parentHeader, parentNumber, err := self.header(operationCtx, parent)
		if err != nil {
			return result, err
		}
		if parentNumber != height-1 {
			return result, errors.New("owner trim finalized ancestry skips a height")
		}
		headers[parentNumber], hashes[parentNumber] = parentHeader, parent
	}
	if hashes[action.BirthBlock] != action.BirthHash {
		return result, errors.New("owner trim finalized ancestry changed the signed era anchor")
	}
	result.AnchorHash, result.CheckedFrom, result.CheckedThrough = action.BirthHash, action.BirthBlock+1, min(number, action.BirthBlock+action.Period-1)
	for height := result.CheckedFrom; height <= result.CheckedThrough; height++ {
		body, err := self.body(operationCtx, hashes[height], headers[height])
		if err != nil {
			return ownerTrimActionReconciliation{}, err
		}
		for index, raw := range body {
			if len(signed) == 0 || !bytes.Equal(raw, signed) {
				continue
			}
			if result.Receipt != nil {
				return ownerTrimActionReconciliation{}, errors.New("owner trim original signature appears more than once")
			}
			runtime, err := self.nativeRuntimeAt(operationCtx, hashes[height-1])
			if err != nil {
				return ownerTrimActionReconciliation{}, err
			}
			call, err := subnetOwnerTrimCall(runtime.metadata)
			if err != nil || action.CallIndex != [2]byte{call.PalletIndex, call.CallIndex} {
				return ownerTrimActionReconciliation{}, errors.Join(errors.New("owner trim execution call profile changed"), err)
			}
			root, err := rootExtrinsicsRoot(body, 0)
			if err != nil || root != headers[height].ExtrinsicsRoot {
				return ownerTrimActionReconciliation{}, errors.New("owner trim execution profile/body layout differs")
			}
			events, exists, err := self.storage(operationCtx, runtime.metadata, "System", "Events", hashes[height])
			if err != nil || !exists {
				return ownerTrimActionReconciliation{}, errors.Join(errors.New("owner trim inclusion has no complete event storage"), err)
			}
			receipt, err := nativeDecodeReceiptEvents(runtime.metadata, events, uint32(index), len(body), action.Coldkey, nil)
			if err != nil {
				return ownerTrimActionReconciliation{}, err
			}
			receipt.BlockNumber, receipt.BlockHash, receipt.ExtrinsicIndex, receipt.RawExtrinsic = height, hashes[height], uint32(index), "0x"+hex.EncodeToString(raw)
			receipt.ExecutionRuntimeVersion, receipt.ExecutionCodeHash, receipt.ExecutionMetadataHash = runtime.profile.RuntimeVersion, runtime.profile.RuntimeCodeHash, runtime.profile.RuntimeMetadataHash
			result.Receipt = &receipt
			result.Census = self.receiptCensus(operationCtx, action, hashes[height-1], hashes[height])
		}
	}
	result.Observation = self.trimObservation(operationCtx, action, finalized, number)
	var canonical string
	if err := self.client.call(operationCtx, "chain_getBlockHash", []any{number}, &canonical); err != nil {
		return ownerTrimActionReconciliation{}, err
	}
	if canonical != finalized {
		return ownerTrimActionReconciliation{}, errors.New("owner trim finalized mapping changed during reconciliation")
	}
	if err := self.network(operationCtx); err != nil {
		return ownerTrimActionReconciliation{}, err
	}
	if err := result.validate(action, signed); err != nil {
		return ownerTrimActionReconciliation{}, err
	}
	return result, nil
}

// Current account/census failure blocks effects and expiry while preserving an
// earlier independently authenticated receipt. No partial census is published.
func (self *ownerTrimCanonicalChain) trimObservation(ctx context.Context, action ownerTrimAction, hash string, number uint64) ownerTrimObservation {
	result := ownerTrimObservation{FinalizedNumber: number, FinalizedHash: hash}
	err := func() error {
		runtime, err := self.nativeRuntimeAt(ctx, hash)
		if err != nil {
			return err
		}
		call, err := subnetOwnerTrimCall(runtime.metadata)
		if err != nil || action.CallIndex != [2]byte{call.PalletIndex, call.CallIndex} {
			return errors.Join(errors.New("owner trim current call profile changed"), err)
		}
		account, _ := hex.DecodeString(action.Coldkey[2:])
		row, exists, err := self.storage(ctx, runtime.metadata, "System", "Account", hash, account)
		if err != nil || exists && len(row) != 56 {
			return errors.Join(errors.New("owner trim account nonce layout is unavailable"), err)
		}
		nonce := uint32(0)
		if exists {
			nonce = binary.LittleEndian.Uint32(row[:4])
		}
		// Nonce is independently useful for expiry even when census is unavailable.
		result.AccountNonce = &nonce
		census, err := self.client.readSubnetPreviewAt(ctx, self.policy, action.PolicyHash, hash)
		if err != nil {
			return err
		}
		sealed, err := sealSubnetPreview(census)
		if err != nil {
			return err
		}
		result.Census = &sealed
		return nil
	}()
	if err != nil {
		result.Issue = err.Error()
	}
	return result
}

// Read the exact parent and inclusion block independently of the later head.
// A same-block later call can change this post-state, so it is not causation.
func (self *ownerTrimCanonicalChain) receiptCensus(ctx context.Context, action ownerTrimAction, parent, block string) *ownerTrimReceiptCensus {
	result := &ownerTrimReceiptCensus{}
	err := func() error {
		before, err := self.client.readSubnetPreviewAt(ctx, self.policy, action.PolicyHash, parent)
		if err != nil {
			return err
		}
		after, err := self.client.readSubnetPreviewAt(ctx, self.policy, action.PolicyHash, block)
		if err != nil {
			return err
		}
		correspondence, err := reconcileOwnerTrimActualSubset(action, self.policy, before, after)
		if err != nil {
			return err
		}
		sealedBefore, _ := sealSubnetPreview(before)
		sealedAfter, _ := sealSubnetPreview(after)
		result.Before, result.After, result.Correspondence = &sealedBefore, &sealedAfter, &correspondence
		return nil
	}()
	if err != nil {
		result.Issue = err.Error()
	}
	return result
}
