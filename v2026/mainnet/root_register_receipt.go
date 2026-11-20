// Root registration recovery authenticates original mortal-era body bytes,
// dispatch/fee events and one root registration event. End-block seat state is a
// separate readback; it cannot establish the call's exact burn amount.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Recovery has no write port. Separately approved custody owns any original post
// and this route supplies RPC finality/storage observations, not consensus proofs.
type rootRegisterCanonicalChain struct {
	*rootCanonicalChain
	config rootRegisterConfig
	key    string
}

// Approved operator domain and finite archive-read budgets precede recovery.
func newRootRegisterCanonicalChain(config rootRegisterConfig, key string) (*rootRegisterCanonicalChain, error) {
	if err := config.validate(key); err != nil {
		return nil, err
	}
	client, err := newOwnedSubmissionClient(config.Route)
	if err != nil {
		return nil, err
	}
	native, err := newRootCanonicalChain(client, config.Action.Policy.identity(), []rootReceiptProfile{config.Action.Policy.runtime()})
	if err != nil {
		client.httpClient.CloseIdleConnections()
		return nil, err
	}
	return &rootRegisterCanonicalChain{rootCanonicalChain: native, config: config, key: key}, nil
}

// Authenticate the exact event's field shapes before interpreting its bytes.
// A same-name variant under another pallet or a duplicate event is not authority.
func rootRegisterReceiptEvents(metadata *types.Metadata) (map[[2]byte]rootReceiptEvent, error) {
	events, err := nativeReceiptEvents(metadata, false)
	if err != nil {
		return nil, err
	}
	count := 0
	for key, event := range events {
		if event.name != "SubtensorModule.NeuronRegistered" {
			continue
		}
		count++
		fields := event.variant.Fields
		if key[0] != 7 || len(fields) != 3 || !rootTypeMatches(metadata, fields[0].Type, "u16", 0) || !rootTypeMatches(metadata, fields[1].Type, "u16", 0) || !rootTypeMatches(metadata, fields[2].Type, "account", 0) {
			return nil, errors.New("root registration event encoding changed")
		}
	}
	if count != 1 {
		return nil, errors.New("root registration event missing or duplicated")
	}
	return events, nil
}

// Success needs exactly one matching registration in the original phase.
// Failure may retain account-creation effects but cannot borrow a success event.
func rootDecodeRegisterReceiptEvents(metadata *types.Metadata, raw []byte, index uint32, bodyCount int, policy rootRegisterPolicy) (rootActionReceipt, *uint16, error) {
	receipt, err := nativeDecodeReceiptEvents(metadata, raw, index, bodyCount, policy.Operator, nil)
	if err != nil {
		return receipt, nil, err
	}
	events, err := rootRegisterReceiptEvents(metadata)
	if err != nil {
		return receipt, nil, err
	}
	count := 0
	var uid uint16
	err = walkNativeEventRecords(metadata, raw, bodyCount, events, rootBodyCountLimit, func(record nativeEventRecord) error {
		if record.extrinsicIndex == nil || *record.extrinsicIndex != index || record.event.name != "SubtensorModule.NeuronRegistered" {
			return nil
		}
		count++
		if binary.LittleEndian.Uint16(record.fields[0]) != 0 || !rootRegisterSameAccount(record.fields[2], policy.Hotkey) {
			return errors.New("original root registration event names another netuid or hotkey")
		}
		uid = binary.LittleEndian.Uint16(record.fields[1])
		return nil
	})
	if err != nil || receipt.Success && count != 1 || !receipt.Success && count != 0 {
		return receipt, nil, errors.Join(errors.New("root registration event contradicts exact dispatch outcome"), err)
	}
	if receipt.Success {
		return receipt, &uid, nil
	}
	return receipt, nil, nil
}

// Original financial evidence survives readback failure. The event generation
// is distinct from end-block state, which later calls in the same block may alter.
// Numeric burn evidence is deliberately absent: neither a parent quote nor an
// end-block balance delta establishes this original call's recycled amount.
type rootRegisterReconciliation struct {
	FinalizedNumber uint64                   `json:"finalized_number"`
	FinalizedHash   string                   `json:"finalized_hash"`
	AnchorHash      string                   `json:"anchor_hash"`
	CheckedFrom     uint64                   `json:"checked_from"`
	CheckedThrough  uint64                   `json:"checked_through"`
	AccountNonce    *uint32                  `json:"account_nonce"`
	HeadIssue       string                   `json:"head_issue,omitempty"`
	Receipt         *rootActionReceipt       `json:"receipt,omitempty"`
	BodyCount       int                      `json:"inclusion_body_count,omitempty"`
	RawEvents       string                   `json:"inclusion_events_scale,omitempty"`
	Registration    *rootSeatExpectation     `json:"original_registration_event,omitempty"`
	ActualBurnRao   *uint64                  `json:"actual_burn_rao,omitempty"`
	Readback        *rootRegisterObservation `json:"inclusion_block_readback,omitempty"`
	ReadbackIssue   string                   `json:"readback_issue,omitempty"`
}

// Error details are diagnostic only and cannot consume unbounded journal space.
// Retaining a prefix preserves the explicit readback gap without interpreting it.
func rootRegisterReceiptIssue(err error) string {
	if err == nil {
		return ""
	}
	value := strings.ToValidUTF8(err.Error(), "?")
	if len(value) > 4096 {
		value = strings.ToValidUTF8(value[:4093], "?") + "..."
	}
	return value
}

// Re-decode retained events rather than trusting a mutable derived UID or fee.
// Body coverage remains route-attested; these seals do not prove RPC honesty.
func (self rootRegisterReconciliation) validate(request rootRegisterSigningRequest, raw []byte) error {
	a := request.Config.Action
	if len(self.HeadIssue) > 4096 || len(self.ReadbackIssue) > 4096 {
		return errors.New("root registration diagnostic exceeds retained byte bound")
	}
	if len(raw) == 0 || self.FinalizedNumber < a.BirthBlock || self.FinalizedNumber-a.BirthBlock > rootAncestryLimit || !rootCanonicalHash(self.FinalizedHash) || self.FinalizedNumber == a.BirthBlock && self.FinalizedHash != a.BirthHash || self.AnchorHash != a.BirthHash || self.CheckedFrom != a.BirthBlock+1 || self.CheckedThrough != min(self.FinalizedNumber, a.BirthBlock+a.Period-1) || self.ActualBurnRao != nil {
		return errors.New("root registration reconciliation has incomplete coverage or unsupported numeric burn evidence")
	}
	if self.AccountNonce != nil && self.HeadIssue != "" {
		return errors.New("root registration current nonce contradicts explicit observation gap")
	}
	if receipt := self.Receipt; receipt != nil {
		if receipt.RawExtrinsic != "0x"+hex.EncodeToString(raw) || receipt.BlockNumber < self.CheckedFrom || receipt.BlockNumber > self.CheckedThrough || !rootCanonicalHash(receipt.BlockHash) || !rootCanonicalHash(receipt.EventHash) || receipt.PostState != nil || receipt.Success == (receipt.DispatchError != "") || receipt.ExecutionRuntimeVersion != a.Policy.RuntimeVersion || receipt.ExecutionCodeHash != a.Policy.RuntimeCodeHash || receipt.ExecutionMetadataHash != a.Policy.RuntimeMetadataHash || receipt.BlockNumber == self.FinalizedNumber && receipt.BlockHash != self.FinalizedHash || self.BodyCount < 1 || self.BodyCount > rootBodyCountLimit {
			return errors.New("root registration original receipt domain or inclusion differs")
		}
		metadata, _, err := nativePinnedMetadata(request.Metadata, a.Policy.RuntimeMetadataHash)
		if err != nil {
			return err
		}
		events, err := rootReceiptHex(self.RawEvents, rootBodyBytesLimit)
		if err != nil || rootExtrinsicHash(events) != receipt.EventHash {
			return errors.New("root registration retained event bytes differ from receipt")
		}
		decoded, uid, err := rootDecodeRegisterReceiptEvents(metadata, events, receipt.ExtrinsicIndex, self.BodyCount, a.Policy)
		if err != nil || decoded.Success != receipt.Success || decoded.DispatchError != receipt.DispatchError || decoded.ActualFeeRao != receipt.ActualFeeRao {
			return errors.Join(errors.New("root registration retained events contradict receipt outcome"), err)
		}
		if uid == nil && self.Registration != nil || uid != nil && (self.Registration == nil || self.Registration.Uid != *uid || self.Registration.RegistrationBlock != receipt.BlockNumber) {
			return errors.New("root registration original event generation differs")
		}
		if self.Readback != nil {
			if self.ReadbackIssue != "" || self.Readback.FinalizedNumber != receipt.BlockNumber || self.Readback.FinalizedHash != receipt.BlockHash {
				return errors.New("root registration readback is not the exact inclusion block")
			}
			if _, err := self.Readback.eligibility(a.Policy, metadata); err != nil {
				return err
			}
		} else if self.ReadbackIssue == "" {
			return errors.New("root registration receipt lacks both readback and explicit gap")
		}
	} else if self.Readback != nil || self.ReadbackIssue != "" || self.Registration != nil || self.RawEvents != "" || self.BodyCount != 0 {
		return errors.New("root registration readback or events lack an original receipt")
	}
	return nil
}

// A successful event cannot activate a vanished, reassigned or later generation.
// Failures retain their readback but never produce a successful registration seat.
func rootRegisterObservedSeat(request rootRegisterSigningRequest, evidence rootRegisterReconciliation) *rootSeatExpectation {
	a := request.Config.Action
	if evidence.Receipt == nil || !evidence.Receipt.Success || evidence.Registration == nil || evidence.Readback == nil || evidence.ReadbackIssue != "" || evidence.Registration.RegistrationBlock != evidence.Receipt.BlockNumber || evidence.Readback.FinalizedHash != evidence.Receipt.BlockHash || evidence.Readback.FinalizedNumber != evidence.Receipt.BlockNumber {
		return nil
	}
	metadata, _, err := nativePinnedMetadata(request.Metadata, a.Policy.RuntimeMetadataHash)
	if err != nil {
		return nil
	}
	raw, err := rootReceiptHex(evidence.RawEvents, rootBodyBytesLimit)
	if err != nil || rootExtrinsicHash(raw) != evidence.Receipt.EventHash {
		return nil
	}
	decoded, uid, err := rootDecodeRegisterReceiptEvents(metadata, raw, evidence.Receipt.ExtrinsicIndex, evidence.BodyCount, a.Policy)
	if err != nil || !decoded.Success || uid == nil || *uid != evidence.Registration.Uid || decoded.ActualFeeRao != evidence.Receipt.ActualFeeRao || !rootRegisterEventGenerationCurrent(metadata, raw, evidence.Receipt.ExtrinsicIndex, evidence.BodyCount, a.Policy.Hotkey, *uid) {
		return nil
	}
	eligibility, err := evidence.Readback.eligibility(a.Policy, metadata)
	if err != nil || eligibility.ExistingSeat == nil || *eligibility.ExistingSeat != *evidence.Registration || eligibility.ExistingOwner != a.Policy.Operator {
		return nil
	}
	seat := *eligibility.ExistingSeat
	return &seat
}

// BlockAtRegistration has only block precision. A later same-block registration
// can reuse the UID or hotkey without changing that tuple, so it must not be
// mistaken for continuing custody of this original event's seat generation.
func rootRegisterEventGenerationCurrent(metadata *types.Metadata, raw []byte, index uint32, bodyCount int, hotkey string, uid uint16) bool {
	events, err := rootRegisterReceiptEvents(metadata)
	if err != nil {
		return false
	}
	seen, superseded := false, false
	err = walkNativeEventRecords(metadata, raw, bodyCount, events, rootBodyCountLimit, func(record nativeEventRecord) error {
		if record.event.name != "SubtensorModule.NeuronRegistered" || binary.LittleEndian.Uint16(record.fields[0]) != 0 {
			return nil
		}
		if record.extrinsicIndex != nil && *record.extrinsicIndex == index {
			seen = true
			return nil
		}
		if seen && (binary.LittleEndian.Uint16(record.fields[1]) == uid || rootRegisterSameAccount(record.fields[2], hotkey)) {
			superseded = true
		}
		return nil
	})
	return err == nil && seen && !superseded
}

// Financial incidents remain distinct from successful generation observation.
// Expiry requires complete body coverage and an unchanged available nonce.
func rootRegisterPhase(request rootRegisterSigningRequest, evidence rootRegisterReconciliation) string {
	a := request.Config.Action
	if receipt := evidence.Receipt; receipt != nil {
		if receipt.ActualFeeRao > a.FeeReserveRao {
			return "fee-overrun"
		}
		if !receipt.Success {
			return "dispatch-failed"
		}
		if evidence.Readback == nil {
			return "finalized-readback-pending"
		}
		if rootRegisterObservedSeat(request, evidence) == nil {
			return "finalized-state-conflict"
		}
		return "finalized"
	}
	if evidence.AccountNonce != nil {
		if *evidence.AccountNonce > a.Nonce {
			return "nonce-conflict"
		}
		if evidence.FinalizedNumber >= a.BirthBlock+a.Period && *evidence.AccountNonce == a.Nonce {
			return "expired"
		}
	}
	return "signed"
}

// One owner-scoped finite scan; no nonce inference of success, subscriptions or
// submission. Execution metadata is pinned at each inclusion block's parent.
func (self *rootRegisterCanonicalChain) reconcile(ctx context.Context, request rootRegisterSigningRequest, signed []byte) (rootRegisterReconciliation, error) {
	var result rootRegisterReconciliation
	if ctx == nil {
		return result, errors.New("root registration reconciliation context missing")
	}
	select {
	case self.reconcileCh <- struct{}{}:
		defer func() { <-self.reconcileCh }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	a := request.Config.Action
	if rootObjectHash(self.config) != rootObjectHash(request.Config) || len(signed) == 0 {
		return result, errors.New("root registration receipt requires original approved signed action")
	}
	if err := request.validate(ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: self.key, Owner: a.Policy.Operator, Genesis: a.Policy.GenesisHash}); err != nil {
		return result, err
	}
	if err := rootRegisterSignedAction(a, signed); err != nil {
		return result, err
	}
	operationCtx, cancel := context.WithTimeout(ctx, self.client.retryWindow)
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
		return result, errors.New("root registration finalized head is outside bounded recovery interval")
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
			return result, errors.New("root registration finalized ancestry skips a height")
		}
		headers[parentNumber], hashes[parentNumber] = parentHeader, parent
	}
	if hashes[a.BirthBlock] != a.BirthHash {
		return result, errors.New("root registration mortal anchor changed")
	}
	result.AnchorHash, result.CheckedFrom, result.CheckedThrough = a.BirthHash, a.BirthBlock+1, min(number, a.BirthBlock+a.Period-1)
	for height := result.CheckedFrom; height <= result.CheckedThrough; height++ {
		body, err := self.body(operationCtx, hashes[height], headers[height])
		if err != nil {
			return rootRegisterReconciliation{}, err
		}
		for index, raw := range body {
			if !bytes.Equal(raw, signed) {
				continue
			}
			if result.Receipt != nil {
				return rootRegisterReconciliation{}, errors.New("root registration original extrinsic appears twice")
			}
			runtime, err := self.nativeRuntimeAt(operationCtx, hashes[height-1])
			if err != nil {
				return rootRegisterReconciliation{}, err
			}
			call, err := rootRegisterCall(runtime.metadata)
			if err != nil || call != a.CallIndex || runtime.profile != a.Policy.runtime() {
				return rootRegisterReconciliation{}, errors.Join(errors.New("root registration execution call or runtime profile changed"), err)
			}
			root, err := rootExtrinsicsRoot(body, 0)
			if err != nil || root != headers[height].ExtrinsicsRoot {
				return rootRegisterReconciliation{}, errors.New("root registration execution body layout changed")
			}
			events, exists, err := self.storage(operationCtx, runtime.metadata, "System", "Events", hashes[height])
			if err != nil || !exists {
				return rootRegisterReconciliation{}, errors.Join(errors.New("root registration inclusion lacks event storage"), err)
			}
			receipt, uid, err := rootDecodeRegisterReceiptEvents(runtime.metadata, events, uint32(index), len(body), a.Policy)
			if err != nil {
				return rootRegisterReconciliation{}, err
			}
			receipt.BlockNumber, receipt.BlockHash, receipt.ExtrinsicIndex, receipt.RawExtrinsic = height, hashes[height], uint32(index), "0x"+hex.EncodeToString(raw)
			receipt.ExecutionRuntimeVersion, receipt.ExecutionCodeHash, receipt.ExecutionMetadataHash = runtime.profile.RuntimeVersion, runtime.profile.RuntimeCodeHash, runtime.profile.RuntimeMetadataHash
			result.Receipt, result.BodyCount, result.RawEvents = &receipt, len(body), "0x"+hex.EncodeToString(events)
			if uid != nil {
				result.Registration = &rootSeatExpectation{Uid: *uid, RegistrationBlock: height}
			}
			readback, err := self.rootRegisterObservationAt(operationCtx, a.Policy, hashes[height], height)
			if err != nil {
				result.ReadbackIssue = rootRegisterReceiptIssue(err)
			} else {
				result.Readback = &readback
			}
		}
	}
	// An unapproved current runtime or reaped account cannot invent an unchanged
	// nonce for expiry; neither gap erases an earlier exact financial receipt.
	err = func() error {
		runtime, err := self.nativeRuntimeAt(operationCtx, finalized)
		if err != nil {
			return err
		}
		operator, _ := hex.DecodeString(a.Policy.Operator[2:])
		data, exists, err := self.storage(operationCtx, runtime.metadata, "System", "Account", finalized, operator)
		if err != nil || !exists || len(data) != 56 {
			return errors.Join(errors.New("root registration current operator nonce unavailable"), err)
		}
		nonce := binary.LittleEndian.Uint32(data[:4])
		result.AccountNonce = &nonce
		return nil
	}()
	if err != nil {
		result.HeadIssue = rootRegisterReceiptIssue(err)
	}
	if _, err := self.client.readNativeFinalityCovering(operationCtx, nativeFinalityPoint{Number: number, Hash: finalized}, nativeFinalityPoint{Number: a.BirthBlock, Hash: a.BirthHash}); err != nil {
		return rootRegisterReconciliation{}, err
	}
	if err := self.network(operationCtx); err != nil {
		return rootRegisterReconciliation{}, err
	}
	if err := operationCtx.Err(); err != nil {
		return rootRegisterReconciliation{}, err
	}
	return result, result.validate(request, signed)
}
