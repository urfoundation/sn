package stabi

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// The STSettlement2 binding is generated from its newest implementation,
// STSettlement2Ctf (CAPTURETHEFLAG.md §3.1): it keeps every phase A and phase C
// method, so existing callers are unchanged.
func TestSettlement2BindingKeepsEveryEarlierMethod(t *testing.T) {
	parsed, err := STSettlement2MetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	for _, signature := range []string{
		"initialize(address,address,address,address[],uint256)",
		"setAuditors(address[],uint256)",
		"registerOperatorPool(uint256,bytes32,uint64,uint64)",
		"settlementAt(uint256,uint256)",
		"closeOperatorEpoch(uint256,uint256)",
		"commitDistribution(uint256,uint256,uint256,(bytes32,uint256,uint64,bytes32))",
		"approveDistribution(uint256,uint64,bytes32,bytes32)",
		"finalizeDistribution(uint256)",
		"approvalCount(uint256)",
		"hasApproved(uint256,address)",
		"auditors()",
		"quorum()",
		"auditorSetVersion()",
		"vault()",
		"v1Coordinator()",
		"upgradeToAndCall(address,bytes)",
		"assignSurplus(uint256,uint256,uint256)",
		"recordV1Sweep(uint256,uint256,bytes32)",
		"v1Sweep(uint256)",
		"registerCtfPool(uint256,uint64,uint64)",
		"commitCtfCensus(uint256,uint256,bytes32)",
		"ctfCensus(uint256,uint256)",
		"ctfSettlementAt(uint256,uint256)",
		"ctfPoolId(uint256)",
	} {
		method, err := parsed.MethodById(selector(signature))
		if err != nil || method.Sig != signature {
			t.Errorf("%s: %v %v", signature, method, err)
		}
	}
	for _, signature := range []string{
		"AuditorsSet(uint64,address[],uint256)",
		"DistributionApproved(uint256,uint64,address,uint64,uint256)",
		"V1SweepRecorded(uint256,uint256,bytes32,bytes32)",
		"CtfCensusCommitted(uint256,uint256,bytes32,uint64)",
	} {
		if _, err := parsed.EventByID(crypto.Keccak256Hash([]byte(signature))); err != nil {
			t.Errorf("event %s: %v", signature, err)
		}
	}
}

// The capture the flag calls pack with independently computed selectors and
// round-trip their answers, events and errors.
func TestCaptureTheFlagBindings(t *testing.T) {
	settlement, carry := NewSTSettlement2(), NewSTCtfCarry()
	noId, epoch, census := big.NewInt(1), big.NewInt(8), [32]byte{0xce}
	for _, test := range []struct {
		signature string
		packed    []byte
	}{
		{"registerCtfPool(uint256,uint64,uint64)", settlement.PackRegisterCtfPool(noId, 1_000_000_000, 8)},
		{"commitCtfCensus(uint256,uint256,bytes32)", settlement.PackCommitCtfCensus(epoch, noId, census)},
		{"ctfCensus(uint256,uint256)", settlement.PackCtfCensus(noId, epoch)},
		{"ctfSettlementAt(uint256,uint256)", settlement.PackCtfSettlementAt(noId, epoch)},
		{"ctfPoolId(uint256)", settlement.PackCtfPoolId(noId)},
		{"recycle(uint256,(uint256,bytes32,uint256),(bytes32,uint256)[])", carry.PackRecycle(big.NewInt(3),
			LedgerTreeAccount{Index: big.NewInt(1), Coldkey: [32]byte{0xca}, Cumulative: big.NewInt(700)}, []LedgerTreeProofNode{{Hash: [32]byte{1}, Sum: big.NewInt(300)}})},
		{"recycledFor(uint256)", carry.PackRecycledFor(epoch)},
		{"coldkey()", carry.PackColdkey()},
		{"poolHotkey()", carry.PackPoolHotkey()},
		{"poolId()", carry.PackPoolId()},
	} {
		if !bytes.Equal(test.packed[:4], selector(test.signature)) {
			t.Errorf("%s: selector %x", test.signature, test.packed[:4])
		}
	}

	parsed, err := STSettlement2MetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	vault, hotkey := common.HexToAddress("0x00000000000000000000000000000000000000a2"), [32]byte{0x55}
	answer, err := parsed.Methods["ctfSettlementAt"].Outputs.Pack(vault, hotkey, uint64(8))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := settlement.UnpackCtfSettlementAt(answer)
	if err != nil || resolved.Vault != vault || resolved.Hotkey != hotkey || resolved.FirstEpoch != 8 {
		t.Fatalf("UnpackCtfSettlementAt = %+v, %v", resolved, err)
	}
	answer, err = parsed.Methods["ctfCensus"].Outputs.Pack(census, uint64(9_651_000))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := settlement.UnpackCtfCensus(answer)
	if err != nil || committed.CensusHash != census || committed.BlockNumber != 9_651_000 {
		t.Fatalf("UnpackCtfCensus = %+v, %v", committed, err)
	}
	answer, err = parsed.Methods["ctfPoolId"].Outputs.Pack(big.NewInt(4_294_967_297))
	if err != nil {
		t.Fatal(err)
	}
	if id, err := settlement.UnpackCtfPoolId(answer); err != nil || id.Cmp(big.NewInt(4_294_967_297)) != 0 {
		t.Fatalf("UnpackCtfPoolId = %v, %v", id, err)
	}

	data, err := parsed.Events["CtfCensusCommitted"].Inputs.NonIndexed().Pack(census, uint64(9_651_000))
	if err != nil {
		t.Fatal(err)
	}
	log := &types.Log{Topics: []common.Hash{crypto.Keccak256Hash([]byte("CtfCensusCommitted(uint256,uint256,bytes32,uint64)")),
		common.BigToHash(noId), common.BigToHash(epoch)}, Data: data}
	event, err := settlement.UnpackCtfCensusCommittedEvent(log)
	if err != nil || event.NoId.Cmp(noId) != 0 || event.Epoch.Cmp(epoch) != 0 || event.CensusHash != census || event.BlockNumber != 9_651_000 {
		t.Fatalf("UnpackCtfCensusCommittedEvent = %+v, %v", event, err)
	}
	carryAbi, err := STCtfCarryMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	data, err = carryAbi.Events["Recycled"].Inputs.NonIndexed().Pack(big.NewInt(700))
	if err != nil {
		t.Fatal(err)
	}
	log = &types.Log{Topics: []common.Hash{crypto.Keccak256Hash([]byte("Recycled(uint256,uint256)")), common.BigToHash(epoch)}, Data: data}
	recycled, err := carry.UnpackRecycledEvent(log)
	if err != nil || recycled.Epoch.Cmp(epoch) != 0 || recycled.Amount.Cmp(big.NewInt(700)) != 0 {
		t.Fatalf("UnpackRecycledEvent = %+v, %v", recycled, err)
	}

	for _, test := range []struct {
		signature string
		id        common.Hash
	}{
		{"NoIdOutOfRange()", STSettlement2NoIdOutOfRangeErrorID()},
		{"InvalidCensusHash()", STSettlement2InvalidCensusHashErrorID()},
		{"CensusAlreadyCommitted()", STSettlement2CensusAlreadyCommittedErrorID()},
		{"BlockHashUnavailable()", STSettlement2BlockHashUnavailableErrorID()},
		{"NothingToRecycle()", STCtfCarryNothingToRecycleErrorID()},
		{"InvalidColdkey()", STCtfCarryInvalidColdkeyErrorID()},
		{"RuntimeAccountingMismatch()", STCtfCarryRuntimeAccountingMismatchErrorID()},
	} {
		if !bytes.Equal(test.id.Bytes()[:4], selector(test.signature)) {
			t.Errorf("%s: error ID %x", test.signature, test.id.Bytes()[:4])
		}
	}
	if failure, err := settlement.UnpackError(selector("CensusAlreadyCommitted()")); err != nil {
		t.Fatalf("UnpackError(CensusAlreadyCommitted) = %v", err)
	} else if _, ok := failure.(*STSettlement2CensusAlreadyCommitted); !ok {
		t.Fatalf("UnpackError(CensusAlreadyCommitted) = %T", failure)
	}
}
