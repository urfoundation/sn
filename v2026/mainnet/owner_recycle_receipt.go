// Read-only reconciliation authenticates the original bytes in every canonical
// mortal-era body, then retains dispatch/fee and inclusion-block mode separately.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"
)

// Recovery remains read-only; separately approved custody owns any post. The
// route asserts finalized head and storage, not GRANDPA or storage proofs.
type ownerRecycleCanonicalChain struct {
	*rootCanonicalChain
	config ownerRecycleConfig
	key    string
}

// Approved domain and finite archive-read budget are required even for recovery.
func newOwnerRecycleCanonicalChain(config ownerRecycleConfig, key string) (*ownerRecycleCanonicalChain, error) {
	if err := config.validate(key); err != nil {
		return nil, err
	}
	client, err := newOwnedSubmissionClient(config.Route)
	if err != nil {
		return nil, err
	}
	p := config.Action.Policy
	native, err := newRootCanonicalChain(client, identityExpectation{NativeChain: p.NativeChain, GenesisHash: p.GenesisHash, EvmChainId: p.EvmChainId}, []rootReceiptProfile{config.Action.runtime()})
	if err != nil {
		client.httpClient.CloseIdleConnections()
		return nil, err
	}
	return &ownerRecycleCanonicalChain{rootCanonicalChain: native, config: config, key: key}, nil
}

// A receipt survives a later storage/runtime outage. Expiry requires complete
// body coverage and an available unchanged owner nonce; mode alone is no receipt.
type ownerRecycleReconciliation struct {
	FinalizedNumber uint64                   `json:"finalized_number"`
	FinalizedHash   string                   `json:"finalized_hash"`
	AnchorHash      string                   `json:"anchor_hash"`
	CheckedFrom     uint64                   `json:"checked_from"`
	CheckedThrough  uint64                   `json:"checked_through"`
	AccountNonce    *uint32                  `json:"account_nonce"`
	HeadIssue       string                   `json:"head_issue,omitempty"`
	Receipt         *rootActionReceipt       `json:"receipt,omitempty"`
	Readback        *ownerRecycleObservation `json:"inclusion_block_readback,omitempty"`
	ReadbackIssue   string                   `json:"readback_issue,omitempty"`
}

// Validate evidence correspondence without promoting checksums to chain proofs.
func (self ownerRecycleReconciliation) validate(request ownerRecycleSigningRequest, raw []byte) error {
	a := request.Config.Action
	if len(raw) == 0 || self.FinalizedNumber < a.BirthBlock || self.FinalizedNumber-a.BirthBlock > rootAncestryLimit || !rootCanonicalHash(self.FinalizedHash) || self.FinalizedNumber == a.BirthBlock && self.FinalizedHash != a.BirthHash || self.AnchorHash != a.BirthHash || self.CheckedFrom != a.BirthBlock+1 || self.CheckedThrough != min(self.FinalizedNumber, a.BirthBlock+a.Period-1) {
		return errors.New("recycle reconciliation has incomplete canonical coverage")
	}
	if receipt := self.Receipt; receipt != nil {
		if receipt.RawExtrinsic != "0x"+hex.EncodeToString(raw) || receipt.BlockNumber < self.CheckedFrom || receipt.BlockNumber > self.CheckedThrough || !rootCanonicalHash(receipt.BlockHash) || !rootCanonicalHash(receipt.EventHash) || receipt.PostState != nil || receipt.Success == (receipt.DispatchError != "") || receipt.ExecutionRuntimeVersion != a.Policy.RuntimeVersion || receipt.ExecutionCodeHash != a.Policy.RuntimeCodeHash || receipt.ExecutionMetadataHash != a.Policy.RuntimeMetadataHash || receipt.BlockNumber == self.FinalizedNumber && receipt.BlockHash != self.FinalizedHash {
			return errors.New("recycle original receipt domain, inclusion or dispatch differs")
		}
		if self.Readback != nil {
			if self.ReadbackIssue != "" || self.Readback.FinalizedNumber != receipt.BlockNumber || self.Readback.FinalizedHash != receipt.BlockHash || self.Readback.Owner != a.Owner {
				return errors.New("recycle readback is not the exact inclusion block")
			}
			metadata, _, err := nativePinnedMetadata(request.Metadata, a.Policy.RuntimeMetadataHash)
			if err != nil {
				return err
			}
			_, err = self.Readback.window(a.Policy, metadata)
			if err != nil {
				return err
			}
		} else if self.ReadbackIssue == "" {
			return errors.New("recycle receipt has neither readback nor explicit gap")
		}
	} else if self.Readback != nil || self.ReadbackIssue != "" {
		return errors.New("recycle readback has no original receipt")
	}
	return nil
}

// One finite pass; no subscriptions, nonce inference of success or RPC sends.
func (self *ownerRecycleCanonicalChain) reconcile(ctx context.Context, request ownerRecycleSigningRequest, signed []byte) (ownerRecycleReconciliation, error) {
	var result ownerRecycleReconciliation
	if ctx == nil {
		return result, errors.New("recycle reconciliation context missing")
	}
	select {
	case self.reconcileCh <- struct{}{}:
		defer func() { <-self.reconcileCh }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	a := request.Config.Action
	if rootObjectHash(self.config) != rootObjectHash(request.Config) || len(signed) == 0 {
		return result, errors.New("recycle receipt requires original approved signed action")
	}
	if err := request.validate(ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: self.key, Owner: a.Owner, Genesis: a.Policy.GenesisHash}); err != nil {
		return result, err
	}
	if err := ownerRecycleSignedAction(a, signed); err != nil {
		return result, err
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
	if number < a.BirthBlock || number-a.BirthBlock > rootAncestryLimit {
		return result, errors.New("recycle finalized head is outside bounded recovery interval")
	}
	result.FinalizedNumber, result.FinalizedHash = number, finalized
	headers, hashes := map[uint64]rootReceiptHeader{number: header}, map[uint64]string{number: finalized}
	for height := number; height > a.BirthBlock; height-- {
		parent := headers[height].ParentHash
		parentHeader, parentNumber, err := self.header(operationCtx, parent)
		if err != nil {
			return result, err
		}
		if parentNumber != height-1 {
			return result, errors.New("recycle finalized ancestry skips a height")
		}
		headers[parentNumber], hashes[parentNumber] = parentHeader, parent
	}
	if hashes[a.BirthBlock] != a.BirthHash {
		return result, errors.New("recycle mortal anchor changed")
	}
	result.AnchorHash, result.CheckedFrom, result.CheckedThrough = a.BirthHash, a.BirthBlock+1, min(number, a.BirthBlock+a.Period-1)
	for height := result.CheckedFrom; height <= result.CheckedThrough; height++ {
		body, err := self.body(operationCtx, hashes[height], headers[height])
		if err != nil {
			return ownerRecycleReconciliation{}, err
		}
		for index, raw := range body {
			if !bytes.Equal(raw, signed) {
				continue
			}
			if result.Receipt != nil {
				return ownerRecycleReconciliation{}, errors.New("recycle original extrinsic appears twice")
			}
			runtime, err := self.nativeRuntimeAt(operationCtx, hashes[height-1])
			if err != nil {
				return ownerRecycleReconciliation{}, err
			}
			call, err := ownerRecycleCall(runtime.metadata)
			if err != nil || call != a.CallIndex {
				return ownerRecycleReconciliation{}, errors.Join(errors.New("recycle execution call profile changed"), err)
			}
			root, err := rootExtrinsicsRoot(body, 0)
			if err != nil || root != headers[height].ExtrinsicsRoot {
				return ownerRecycleReconciliation{}, errors.New("recycle execution body layout changed")
			}
			events, exists, err := self.storage(operationCtx, runtime.metadata, "System", "Events", hashes[height])
			if err != nil || !exists {
				return ownerRecycleReconciliation{}, errors.Join(errors.New("recycle inclusion lacks event storage"), err)
			}
			receipt, err := nativeDecodeReceiptEvents(runtime.metadata, events, uint32(index), len(body), a.Owner, nil)
			if err != nil {
				return ownerRecycleReconciliation{}, err
			}
			receipt.BlockNumber, receipt.BlockHash, receipt.ExtrinsicIndex, receipt.RawExtrinsic = height, hashes[height], uint32(index), "0x"+hex.EncodeToString(raw)
			receipt.ExecutionRuntimeVersion, receipt.ExecutionCodeHash, receipt.ExecutionMetadataHash = runtime.profile.RuntimeVersion, runtime.profile.RuntimeCodeHash, runtime.profile.RuntimeMetadataHash
			result.Receipt = &receipt
			readback, err := self.recycleObservationAt(operationCtx, a.Policy, a.Owner, hashes[height], height)
			if err != nil {
				result.ReadbackIssue = err.Error()
			} else {
				result.Readback = &readback
			}
		}
	}
	// Current nonce is independently useful for expiry. A changed runtime blocks
	// this observation while preserving an earlier authenticated financial result.
	err = func() error {
		runtime, err := self.nativeRuntimeAt(operationCtx, finalized)
		if err != nil {
			return err
		}
		account, _ := hex.DecodeString(a.Owner[2:])
		data, exists, err := self.storage(operationCtx, runtime.metadata, "System", "Account", finalized, account)
		if err != nil || !exists || len(data) != 56 {
			return errors.Join(errors.New("recycle current owner nonce unavailable"), err)
		}
		nonce := binary.LittleEndian.Uint32(data[:4])
		result.AccountNonce = &nonce
		return nil
	}()
	if err != nil {
		result.HeadIssue = err.Error()
	}
	var canonical string
	if err := self.client.call(operationCtx, "chain_getBlockHash", []any{number}, &canonical); err != nil {
		return ownerRecycleReconciliation{}, err
	}
	if canonical != finalized {
		return ownerRecycleReconciliation{}, errors.New("recycle finalized mapping changed")
	}
	if err := self.network(operationCtx); err != nil {
		return ownerRecycleReconciliation{}, err
	}
	return result, result.validate(request, signed)
}
