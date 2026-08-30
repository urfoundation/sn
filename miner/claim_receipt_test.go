package miner

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/stabi"
)

func claimEventLog(t *testing.T, contract common.Address, name string, indexed []common.Hash, values ...any) *types.Log {
	t.Helper()
	parsed, err := stabi.STSettlementVaultMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	event, ok := parsed.Events[name]
	if !ok {
		t.Fatalf("settlement-vault ABI has no %s event", name)
	}
	data, err := event.Inputs.NonIndexed().Pack(values...)
	if err != nil {
		t.Fatal(err)
	}
	return &types.Log{Address: contract, Topics: append([]common.Hash{event.ID}, indexed...), Data: data}
}

func TestDecodeMinerClaimReceiptDistinguishesCreditDeferralAndPayment(t *testing.T) {
	contract := common.HexToAddress("0x0000000000000000000000000000000000000521")
	coldkey := common.HexToHash("0xabcd")
	relayer := common.HexToAddress("0x0000000000000000000000000000000000000007")
	receipt := &types.Receipt{Logs: []*types.Log{
		claimEventLog(t, contract, "Claimed", []common.Hash{common.BigToHash(big.NewInt(4)), common.BigToHash(big.NewInt(2)), coldkey}, big.NewInt(2500), big.NewInt(175_960_612), relayer),
		claimEventLog(t, contract, "ClaimPaymentDeferred", []common.Hash{coldkey}, big.NewInt(175_960_612), big.NewInt(99_999), uint64(100_000), uint8(0)),
		claimEventLog(t, contract, "ClaimPaid", []common.Hash{coldkey, common.BytesToHash(relayer.Bytes())}, big.NewInt(351_921_226)),
		claimEventLog(t, common.HexToAddress("0x0000000000000000000000000000000000009999"), "ClaimPaid", []common.Hash{coldkey, common.BytesToHash(relayer.Bytes())}, big.NewInt(1)),
	}}
	events := decodeMinerClaimReceipt(receipt, contract)
	if len(events.Claimed) != 1 || len(events.Deferred) != 1 || len(events.Paid) != 1 {
		t.Fatalf("claim receipt classes = claimed:%d deferred:%d paid:%d", len(events.Claimed), len(events.Deferred), len(events.Paid))
	}
	if events.Claimed[0].Amount.Uint64() != 175_960_612 || events.Deferred[0].CreditAlphaRao.Uint64() != 175_960_612 || events.Paid[0].Amount.Uint64() != 351_921_226 {
		t.Fatalf("claim receipt amounts were not preserved: %+v", events)
	}
}

func TestDecodeMinerClaimReceiptRejectsNilAndForeignLogs(t *testing.T) {
	contract := common.HexToAddress("0x0000000000000000000000000000000000000521")
	if events := decodeMinerClaimReceipt(nil, contract); len(events.Claimed)+len(events.Deferred)+len(events.Paid) != 0 {
		t.Fatalf("nil receipt produced events: %+v", events)
	}
	receipt := &types.Receipt{Logs: []*types.Log{{Address: contract, Topics: []common.Hash{common.HexToHash("0x01")}}}}
	if events := decodeMinerClaimReceipt(receipt, contract); len(events.Claimed)+len(events.Deferred)+len(events.Paid) != 0 {
		t.Fatalf("unknown event produced claim outcome: %+v", events)
	}
}
