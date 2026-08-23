package stabi

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// selector independently computes the 4-byte function selector,
// keccak256(canonicalSignature)[:4], without going through abi/bind.
func selector(sig string) []byte {
	return crypto.Keccak256([]byte(sig))[:4]
}

// TestMetaDataParses checks that the embedded ABI parses via both the v2
// MetaData path and plain abi.JSON, and that a BoundContract can be built.
func TestMetaDataParses(t *testing.T) {
	parsed, err := STSubnetMetaData.ParseABI()
	if err != nil {
		t.Fatalf("STSubnetMetaData.ParseABI: %v", err)
	}
	// v0.3 (D23): the effort-bounty surface (validator registry, trails
	// submit/prove/dispute, feePool/φ/ω/sampleK, claimValidator) left the v1
	// contract and the buyback reserve (reserveHotkey, buybackTotal) joined.
	// v0.4 (D25): the deposit weighting ledger (DT, totalDT) left too — the
	// contract does no deposit weighting/attribution (validators weight from
	// the Deposited event log).
	if got := len(parsed.Methods); got != 60 {
		t.Fatalf("parsed ABI has %d methods, want 60", got)
	}

	if _, err := abi.JSON(strings.NewReader(STSubnetMetaData.ABI)); err != nil {
		t.Fatalf("abi.JSON(STSubnetMetaData.ABI): %v", err)
	}

	// NewSTSubnet panics on an invalid embedded ABI; reaching here means it parsed.
	c := NewSTSubnet()
	addr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	if inst := c.Instance(nil, addr); inst == nil {
		t.Fatal("(*STSubnet).Instance returned nil BoundContract")
	}
	if bc := bind.NewBoundContract(addr, *parsed, nil, nil, nil); bc == nil {
		t.Fatal("bind.NewBoundContract returned nil")
	}
}

// TestCriticalSelectors asserts that, for the critical subnet functions, the
// selector packed by the generated wrappers and the selector recorded in the
// parsed ABI both match an independently computed keccak-256 selector.
func TestCriticalSelectors(t *testing.T) {
	c := NewSTSubnet()
	parsed, err := STSubnetMetaData.ParseABI()
	if err != nil {
		t.Fatalf("ParseABI: %v", err)
	}

	var (
		b32   [32]byte
		e     = big.NewInt(1)
		id    = big.NewInt(2)
		amt   = big.NewInt(3)
		proof = [][32]byte{}
	)

	cases := []struct {
		sig    string
		packed []byte
	}{
		{"claimMiner(uint256,uint256,bytes32,uint256,bytes32[])", c.PackClaimMiner(e, id, b32, amt, proof)},
		{"deposit(uint256,uint256)", c.PackDeposit(id, amt)},
		{"commitOperator(uint256,uint256,bytes32,bytes)", c.PackCommitOperator(e, id, b32, []byte{0x01})},
		{"finalizeEpoch(uint256)", c.PackFinalizeEpoch(e)},
		{"bindHead(bytes32,bytes32,bytes)", c.PackBindHead(b32, b32, []byte{0x01})},
	}

	for _, tc := range cases {
		want := selector(tc.sig)
		name := tc.sig[:strings.Index(tc.sig, "(")]

		if len(tc.packed) < 4 {
			t.Errorf("%s: packed calldata too short (%d bytes)", tc.sig, len(tc.packed))
			continue
		}
		if got := tc.packed[:4]; !bytes.Equal(got, want) {
			t.Errorf("%s: generated wrapper selector = %x, want %x", tc.sig, got, want)
		}

		m, ok := parsed.Methods[name]
		if !ok {
			t.Errorf("%s: method %q missing from parsed ABI", tc.sig, name)
			continue
		}
		if !bytes.Equal(m.ID, want) {
			t.Errorf("%s: ABI method ID = %x, want %x", tc.sig, m.ID, want)
		}
		if m.Sig != tc.sig {
			t.Errorf("canonical signature mismatch: ABI %q, want %q", m.Sig, tc.sig)
		}
	}
}

// TestViewWrappers asserts the generated package exposes callable Pack/Unpack
// wrappers for epoch(), operators(uint256) and noCommit(uint256,uint256), and
// that they round-trip ABI-encoded return data correctly.
func TestViewWrappers(t *testing.T) {
	c := NewSTSubnet()
	parsed, err := STSubnetMetaData.ParseABI()
	if err != nil {
		t.Fatalf("ParseABI: %v", err)
	}

	// epoch() -> uint256
	if got := c.PackEpoch(); !bytes.Equal(got[:4], selector("epoch()")) {
		t.Errorf("PackEpoch selector = %x, want %x", got[:4], selector("epoch()"))
	}
	ret, err := parsed.Methods["epoch"].Outputs.Pack(big.NewInt(42))
	if err != nil {
		t.Fatalf("pack epoch return: %v", err)
	}
	epoch, err := c.UnpackEpoch(ret)
	if err != nil {
		t.Fatalf("UnpackEpoch: %v", err)
	}
	if epoch.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("UnpackEpoch = %v, want 42", epoch)
	}

	// operators(uint256) -> (bytes32 coldkey, uint16 minerUid, bytes32 minerHotkey, bool active)
	if got := c.PackOperators(big.NewInt(7)); !bytes.Equal(got[:4], selector("operators(uint256)")) {
		t.Errorf("PackOperators selector = %x, want %x", got[:4], selector("operators(uint256)"))
	}
	coldkey := [32]byte{0x0c}
	hotkey := [32]byte{0x0d}
	ret, err = parsed.Methods["operators"].Outputs.Pack(coldkey, uint16(9), hotkey, true)
	if err != nil {
		t.Fatalf("pack operators return: %v", err)
	}
	op, err := c.UnpackOperators(ret)
	if err != nil {
		t.Fatalf("UnpackOperators: %v", err)
	}
	if op.Coldkey != coldkey || op.MinerUid != 9 || op.MinerHotkey != hotkey || !op.Active {
		t.Errorf("UnpackOperators = %+v, want {Coldkey:%x MinerUid:9 MinerHotkey:%x Active:true}", op, coldkey, hotkey)
	}

	// noCommit(uint256,uint256) -> (bytes32 payoutRoot, bytes off)
	if got := c.PackNoCommit(big.NewInt(1), big.NewInt(2)); !bytes.Equal(got[:4], selector("noCommit(uint256,uint256)")) {
		t.Errorf("PackNoCommit selector = %x, want %x", got[:4], selector("noCommit(uint256,uint256)"))
	}
	root := [32]byte{0xaa}
	off := []byte{0xde, 0xad, 0xbe, 0xef}
	ret, err = parsed.Methods["noCommit"].Outputs.Pack(root, off)
	if err != nil {
		t.Fatalf("pack noCommit return: %v", err)
	}
	nc, err := c.UnpackNoCommit(ret)
	if err != nil {
		t.Fatalf("UnpackNoCommit: %v", err)
	}
	if nc.PayoutRoot != root || !bytes.Equal(nc.Off, off) {
		t.Errorf("UnpackNoCommit = %+v, want {PayoutRoot:%x Off:%x}", nc, root, off)
	}
}
