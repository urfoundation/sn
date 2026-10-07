// Canonical reconciliation scans original mortal-era bodies before any resend.
// Outer success never substitutes for MultisigExecuted's inner dispatch result.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// The result is scoped to the outer extrinsic and original inner call hash.
type treasuryDispatch struct {
	Kind         string             `json:"kind"`
	Timepoint    *treasuryTimepoint `json:"timepoint,omitempty"`
	InnerSuccess bool               `json:"inner_success"`
	InnerError   string             `json:"inner_error,omitempty"`
}

// Complete original event bytes are retained for deterministic replay.
type treasuryReconciliation struct {
	FinalizedNumber uint64               `json:"finalized_number"`
	FinalizedHash   string               `json:"finalized_hash"`
	AnchorHash      string               `json:"anchor_hash"`
	CheckedFrom     uint64               `json:"checked_from"`
	CheckedThrough  uint64               `json:"checked_through"`
	AccountNonce    *uint32              `json:"account_nonce,omitempty"`
	Receipt         *rootActionReceipt   `json:"receipt,omitempty"`
	BodyCount       int                  `json:"body_count,omitempty"`
	Events          string               `json:"events_scale,omitempty"`
	Dispatch        *treasuryDispatch    `json:"multisig_dispatch,omitempty"`
	Readback        *treasuryObservation `json:"inclusion_block_readback,omitempty"`
	ReadbackIssue   string               `json:"readback_issue,omitempty"`
}

// Exact field names and variants are authenticated, not guessed from lengths.
func decodeTreasuryDispatch(metadata *types.Metadata, eventsRaw []byte, bodyCount int, receipt rootActionReceipt, a treasuryAction) (treasuryDispatch, error) {
	var result treasuryDispatch
	events, err := nativeReceiptEvents(metadata, false)
	if err != nil {
		return result, err
	}
	inner, _ := a.innerCall()
	hash := rootExtrinsicHash(inner)
	found := 0
	err = walkNativeEventRecords(metadata, eventsRaw, bodyCount, events, rootBodyCountLimit, func(record nativeEventRecord) error {
		if record.extrinsicIndex == nil || *record.extrinsicIndex != receipt.ExtrinsicIndex || !strings.HasPrefix(record.event.name, "Multisig.") {
			return nil
		}
		found++
		fields := record.fields
		variant := record.event.variant
		names := []string{"approving", "timepoint", "multisig", "call_hash"}
		shapes := []string{"account", "timepoint", "account", "account"}
		switch record.event.name {
		case "Multisig.NewMultisig":
			names = []string{"approving", "multisig", "call_hash"}
			shapes = []string{"account", "account", "account"}
			result.Kind = "opened"
		case "Multisig.MultisigApproval":
			result.Kind = "approved"
		case "Multisig.MultisigExecuted":
			names = append(names, "result")
			shapes = append(shapes, "dispatch-result")
			result.Kind = "executed"
		case "Multisig.MultisigCancelled":
			names[0] = "cancelling"
			result.Kind = "cancelled"
		default:
			return fmt.Errorf("%w: unrelated multisig event inside treasury outer call", errRpcIntegrity)
		}
		if len(fields) != len(names) {
			return errors.New("treasury multisig event fields changed")
		}
		for i, field := range variant.Fields {
			if !field.HasName || string(field.Name) != names[i] {
				return errors.New("treasury multisig event names changed")
			}
			if shapes[i] == "dispatch-result" {
				entry := metadata.AsMetadataV14.EfficientLookup[field.Type.Int64()]
				if entry == nil || !entry.Def.IsVariant || len(entry.Def.Variant.Variants) != 2 {
					return errors.New("treasury inner dispatch result changed")
				}
				ok, bad := entry.Def.Variant.Variants[0], entry.Def.Variant.Variants[1]
				if ok.Name != "Ok" || ok.Index != 0 || len(ok.Fields) != 1 || !rootSigningType(metadata, ok.Fields[0].Type, "unit", 0) || bad.Name != "Err" || bad.Index != 1 || len(bad.Fields) != 1 {
					return errors.New("treasury inner dispatch result variants changed")
				}
			} else if !treasuryType(metadata, field.Type, shapes[i], 0) {
				return errors.New("treasury multisig event wire shape changed")
			}
		}
		if "0x"+hex.EncodeToString(fields[0]) != a.Owner {
			return errors.New("treasury multisig approving account differs")
		}
		position := 1
		if result.Kind != "opened" {
			point := treasuryTimepoint{Height: binary.LittleEndian.Uint32(fields[1][:4]), Index: binary.LittleEndian.Uint32(fields[1][4:])}
			result.Timepoint = &point
			position = 2
			if a.Timepoint == nil || point != *a.Timepoint {
				return errors.New("treasury multisig event changed original timepoint")
			}
		}
		if "0x"+hex.EncodeToString(fields[position]) != a.Descriptor.Multisig.AccountId || "0x"+hex.EncodeToString(fields[position+1]) != hash {
			return errors.New("treasury multisig event account or inner call hash differs")
		}
		if result.Kind == "opened" {
			if a.Timepoint != nil || a.Operation == "cancel_as_multi" || receipt.BlockNumber > uint64(^uint32(0)) {
				return errors.New("treasury opening event contradicts original operation")
			}
			result.Timepoint = &treasuryTimepoint{Height: uint32(receipt.BlockNumber), Index: receipt.ExtrinsicIndex}
		}
		if result.Kind == "executed" {
			if a.Operation != "as_multi" {
				return errors.New("hash-only treasury approval cannot execute")
			}
			raw := fields[len(fields)-1]
			if len(raw) == 1 && raw[0] == 0 {
				result.InnerSuccess = true
			} else if len(raw) > 1 && raw[0] == 1 {
				result.InnerError = "scale:0x" + hex.EncodeToString(raw[1:])
			} else {
				return errors.New("treasury inner dispatch result is malformed")
			}
		}
		if (result.Kind == "cancelled") != (a.Operation == "cancel_as_multi") {
			return errors.New("treasury cancellation event contradicts operation")
		}
		return nil
	})
	if err != nil {
		return treasuryDispatch{}, err
	}
	if receipt.Success && found != 1 || !receipt.Success && found != 0 {
		return treasuryDispatch{}, errors.New("treasury outer dispatch and inner event count disagree")
	}
	return result, nil
}

// Replay cannot transform an outer success, absent pending row or failed inner
// dispatch into a registration or a spend.
func (self treasuryReconciliation) validate(request treasurySigningRequest, signed []byte) error {
	a := request.Config.Action
	if self.FinalizedNumber < a.BirthBlock || self.FinalizedNumber-a.BirthBlock > rootAncestryLimit || !rootCanonicalHash(self.FinalizedHash) || self.AnchorHash != a.BirthHash || self.CheckedFrom != a.BirthBlock+1 || self.CheckedThrough != min(self.FinalizedNumber, a.BirthBlock+a.Period-1) || self.FinalizedNumber == a.BirthBlock && self.FinalizedHash != a.BirthHash {
		return errors.New("treasury original mortal scan is incomplete")
	}
	if self.Receipt == nil {
		if self.Dispatch != nil || self.Events != "" || self.Readback != nil || self.ReadbackIssue != "" || self.BodyCount != 0 {
			return errors.New("treasury terminal data lacks original inclusion")
		}
		return nil
	}
	r := self.Receipt
	if r.BlockNumber < self.CheckedFrom || r.BlockNumber > self.CheckedThrough || !rootCanonicalHash(r.BlockHash) || r.RawExtrinsic != "0x"+hex.EncodeToString(signed) || r.PostState != nil || r.ExecutionRuntimeVersion != a.Policy.RuntimeVersion || r.ExecutionCodeHash != a.Policy.RuntimeCodeHash || r.ExecutionMetadataHash != a.Policy.RuntimeMetadataHash || r.BlockNumber == self.FinalizedNumber && r.BlockHash != self.FinalizedHash {
		return errors.New("treasury receipt differs from original action/runtime/interval")
	}
	metadata, _, err := nativePinnedMetadata(request.Metadata, a.Policy.RuntimeMetadataHash)
	if err != nil {
		return err
	}
	raw, err := rootReceiptHex(self.Events, maxRpcReplyBytes)
	if err != nil {
		return err
	}
	decoded, err := nativeDecodeReceiptEvents(metadata, raw, r.ExtrinsicIndex, self.BodyCount, a.Owner, nil)
	if err != nil {
		return err
	}
	if decoded.EventHash != r.EventHash || decoded.Success != r.Success || decoded.DispatchError != r.DispatchError || decoded.ActualFeeRao != r.ActualFeeRao {
		return errors.New("treasury receipt no longer matches original events")
	}
	dispatch, err := decodeTreasuryDispatch(metadata, raw, self.BodyCount, *r, a)
	if err != nil {
		return err
	}
	if self.Dispatch == nil || rootObjectHash(dispatch) != rootObjectHash(*self.Dispatch) {
		return errors.New("treasury inner result differs from retained original events")
	}
	if self.Readback != nil {
		if self.ReadbackIssue != "" || self.Readback.FinalizedNumber != r.BlockNumber || self.Readback.FinalizedHash != r.BlockHash {
			return errors.New("treasury readback is not the original inclusion")
		}
		_, err = self.Readback.facts(context.Background(), a, metadata)
		return err
	}
	if self.ReadbackIssue == "" {
		return errors.New("treasury receipt lacks readback or explicit gap")
	}
	return nil
}

// The exact approved pinned transport is shared by reads and one-shot sends.
type treasuryCanonicalChain struct {
	*rootCanonicalChain
	config treasuryConfig
	key    string
}

// Runtime/source pins come from approval, never from the observed endpoint.
func newTreasuryCanonicalChain(config treasuryConfig, key string) (*treasuryCanonicalChain, error) {
	if err := config.validate(key); err != nil {
		return nil, err
	}
	client, err := newOwnedSubmissionClient(config.Route)
	if err != nil {
		return nil, err
	}
	p := config.Action.Policy
	native, err := newRootCanonicalChain(client, identityExpectation{NativeChain: p.NativeChain, GenesisHash: p.GenesisHash, EvmChainId: p.EvmChainId}, []rootReceiptProfile{p.rootReceiptProfile})
	if err != nil {
		client.httpClient.CloseIdleConnections()
		return nil, err
	}
	return &treasuryCanonicalChain{rootCanonicalChain: native, config: config, key: key}, nil
}

// One caller-owned read budget covers network, ancestry, bodies and state.
func (self *treasuryCanonicalChain) reconcile(ctx context.Context, request treasurySigningRequest, signed []byte) (treasuryReconciliation, error) {
	var result treasuryReconciliation
	if ctx == nil {
		return result, errors.New("treasury reconciliation needs context")
	}
	select {
	case self.reconcileCh <- struct{}{}:
		defer func() { <-self.reconcileCh }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	a := request.Config.Action
	if rootObjectHash(self.config) != rootObjectHash(request.Config) {
		return result, errors.New("treasury reconciliation route/config changed")
	}
	if err := request.validate(ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: self.key, Owner: a.Owner, Genesis: a.Policy.GenesisHash}); err != nil {
		return result, err
	}
	if err := treasurySignedAction(a, signed); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(self.config.Route.ReadRetrySeconds)*time.Second)
	defer cancel()
	if err := self.network(ctx); err != nil {
		return result, err
	}
	point, err := self.client.readNativeFinalityCovering(ctx, nativeFinalityPoint{Number: a.BirthBlock, Hash: a.BirthHash})
	if err != nil {
		return result, err
	}
	if point.Number-a.BirthBlock > rootAncestryLimit {
		return result, errors.New("treasury recovery exceeds approved bounded ancestry")
	}
	header, number, err := self.header(ctx, point.Hash)
	if err != nil {
		return result, err
	}
	headers := map[uint64]rootReceiptHeader{number: header}
	hashes := map[uint64]string{number: point.Hash}
	for height := number; height > a.BirthBlock; height-- {
		parent := headers[height].ParentHash
		h, n, err := self.header(ctx, parent)
		if err != nil {
			return result, err
		}
		if n != height-1 {
			return result, fmt.Errorf("%w: treasury ancestry skips a height", errRpcIntegrity)
		}
		headers[n], hashes[n] = h, parent
	}
	if hashes[a.BirthBlock] != a.BirthHash {
		return result, fmt.Errorf("%w: treasury original birth hash changed", errRpcIntegrity)
	}
	result.FinalizedNumber, result.FinalizedHash, result.AnchorHash, result.CheckedFrom, result.CheckedThrough = number, point.Hash, a.BirthHash, a.BirthBlock+1, min(number, a.BirthBlock+a.Period-1)
	for height := result.CheckedFrom; height <= result.CheckedThrough; height++ {
		body, err := self.body(ctx, hashes[height], headers[height])
		if err != nil {
			return treasuryReconciliation{}, err
		}
		bodyRoot, err := rootExtrinsicsRoot(body, 0)
		if err != nil || bodyRoot != headers[height].ExtrinsicsRoot {
			return treasuryReconciliation{}, errors.Join(fmt.Errorf("%w: treasury native body layout differs from reviewed runtime", errRpcIntegrity), err)
		}
		for index, raw := range body {
			if !bytes.Equal(raw, signed) {
				continue
			}
			if result.Receipt != nil {
				return treasuryReconciliation{}, fmt.Errorf("%w: treasury original extrinsic included twice", errRpcIntegrity)
			}
			runtime, err := self.nativeRuntimeAt(ctx, hashes[height-1])
			if err != nil {
				return treasuryReconciliation{}, err
			}
			if _, err := treasuryMetadataProfile(runtime.metadata, a); err != nil {
				return treasuryReconciliation{}, err
			}
			events, exists, err := self.storage(ctx, runtime.metadata, "System", "Events", hashes[height])
			if err != nil || !exists || len(events) > maxRpcReplyBytes {
				return treasuryReconciliation{}, errors.Join(errors.New("treasury inclusion events unavailable or exceed retained bound"), err)
			}
			r, err := nativeDecodeReceiptEvents(runtime.metadata, events, uint32(index), len(body), a.Owner, nil)
			if err != nil {
				return treasuryReconciliation{}, err
			}
			r.BlockNumber, r.BlockHash, r.ExtrinsicIndex, r.RawExtrinsic = height, hashes[height], uint32(index), "0x"+hex.EncodeToString(raw)
			r.ExecutionRuntimeVersion, r.ExecutionCodeHash, r.ExecutionMetadataHash = runtime.profile.RuntimeVersion, runtime.profile.RuntimeCodeHash, runtime.profile.RuntimeMetadataHash
			dispatch, err := decodeTreasuryDispatch(runtime.metadata, events, len(body), r, a)
			if err != nil {
				return treasuryReconciliation{}, err
			}
			result.Receipt, result.Dispatch, result.Events, result.BodyCount = &r, &dispatch, "0x"+hex.EncodeToString(events), len(body)
			observation, err := self.treasuryObservationAt(ctx, a, r.BlockHash, height)
			if err != nil {
				if errors.Is(err, errRpcIntegrity) {
					return treasuryReconciliation{}, err
				}
				result.ReadbackIssue = err.Error()
			} else {
				result.Readback = &observation
			}
		}
	}
	runtime, err := self.nativeRuntimeAt(ctx, point.Hash)
	if err == nil {
		account, _ := hex.DecodeString(a.Owner[2:])
		raw, exists, readErr := self.storage(ctx, runtime.metadata, "System", "Account", point.Hash, account)
		if readErr == nil && exists && len(raw) == 56 {
			nonce := binary.LittleEndian.Uint32(raw[:4])
			result.AccountNonce = &nonce
		} else if errors.Is(readErr, errRpcIntegrity) {
			return treasuryReconciliation{}, readErr
		}
	} else if errors.Is(err, errRpcIntegrity) {
		return treasuryReconciliation{}, err
	}
	if err := self.client.closeNativeFinality(ctx, point, point); err != nil {
		return treasuryReconciliation{}, err
	}
	if err := self.network(ctx); err != nil {
		return treasuryReconciliation{}, err
	}
	return result, result.validate(request, signed)
}
