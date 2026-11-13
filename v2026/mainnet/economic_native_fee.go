// Fees are transaction-attributed native event observations, independent of
// incentive alpha, balance deltas, or a claimed native economic denominator.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// ActualFeeRao is the runtime event's field. TipRao is retained separately and
// is never added again to actual_fee or inferred from an account balance.
type economicNativeFee struct {
	EventIndex      uint64 `json:"event_index"`
	Phase           string `json:"phase"`
	ExtrinsicIndex  uint32 `json:"extrinsic_index"`
	ExtrinsicHash   string `json:"extrinsic_hash"`
	Payer           string `json:"payer"`
	ActualFeeRao    string `json:"actual_fee_rao"`
	TipRao          string `json:"tip_rao"`
	DispatchSuccess bool   `json:"dispatch_success"`
	DispatchError   string `json:"dispatch_error,omitempty"`
}

// The caller authenticates the complete ordered body against its block header
// and supplies exact parent-execution metadata before this decoder is used.
func decodeEconomicNativeFees(metadata *types.Metadata, raw []byte, body [][]byte, payers []string) ([]economicNativeFee, error) {
	if len(payers) == 0 || len(payers) > 16 {
		return nil, errors.New("native fee observation requires 1..16 independent payer accounts")
	}
	selected := map[string]bool{}
	for _, payer := range payers {
		if !rootCanonicalHash(payer) || selected[payer] {
			return nil, errors.New("native fee payer identity is missing or repeated")
		}
		selected[payer] = true
	}
	events, err := economicEmissionEventProfile(metadata)
	if err != nil {
		return nil, err
	}
	type dispatch struct {
		count   int
		success bool
		failure string
	}
	dispatches := map[uint32]dispatch{}
	feeCounts := map[uint32]int{}
	fees := []economicNativeFee{}
	err = walkNativeEventRecords(metadata, raw, len(body), events, economicEmissionEventLimit, func(record nativeEventRecord) error {
		if record.event.name != "TransactionPayment.TransactionFeePaid" && record.event.name != "System.ExtrinsicSuccess" && record.event.name != "System.ExtrinsicFailed" {
			return nil
		}
		if record.extrinsicIndex == nil {
			return errors.New("native fee or dispatch event is outside an exact ApplyExtrinsic phase")
		}
		index := *record.extrinsicIndex
		if record.event.name != "TransactionPayment.TransactionFeePaid" {
			value := dispatches[index]
			value.count++
			value.success = record.event.name == "System.ExtrinsicSuccess"
			if !value.success {
				value.failure = "scale:0x" + hex.EncodeToString(record.fields[0])
			}
			dispatches[index] = value
			return nil
		}
		feeCounts[index]++
		payer := "0x" + hex.EncodeToString(record.fields[0])
		if !selected[payer] {
			return nil
		}
		if len(fees) == economicEmissionEventLimit {
			return errors.New("native fee observation count exceeds its bound")
		}
		fees = append(fees, economicNativeFee{EventIndex: record.index, Phase: "ApplyExtrinsic", ExtrinsicIndex: index,
			ExtrinsicHash: rootExtrinsicHash(body[index]), Payer: payer,
			ActualFeeRao: strconv.FormatUint(binary.LittleEndian.Uint64(record.fields[1]), 10), TipRao: strconv.FormatUint(binary.LittleEndian.Uint64(record.fields[2]), 10)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for index := range fees {
		fee := &fees[index]
		dispatch := dispatches[fee.ExtrinsicIndex]
		if feeCounts[fee.ExtrinsicIndex] != 1 || dispatch.count != 1 {
			return nil, errors.New("native fee has missing or contradictory transaction outcome evidence")
		}
		fee.DispatchSuccess, fee.DispatchError = dispatch.success, dispatch.failure
	}
	return fees, nil
}
