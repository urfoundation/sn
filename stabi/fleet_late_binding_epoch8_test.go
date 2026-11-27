package stabi

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/protocol"
)

// lateBindingVersion is one binding version in a simulated coordinator: the
// block it landed in, its signed validity, and the later events that change it.
type lateBindingVersion struct {
	landed           uint64
	generation       uint64
	validFrom        uint64
	validTo          uint64
	revokedAt        uint64
	revokedEffective uint64
	cleanedBlock     uint64
	cleanedAtEpoch   uint64
	uidLostAt        uint64
}

// lateBindingChain answers bindingAt(client, epoch) at any block as
// STCoordinator does: the newest version landed by the block with validFrom at
// most epoch, active within its validity, before any cleanup, with its UID.
type lateBindingChain struct {
	versions map[[16]byte][]lateBindingVersion
	reads    []uint64
}

func (self *lateBindingChain) read(ctx context.Context, block uint64, clientIds [][16]byte, epoch uint64) ([]BindingAtOutput, error) {
	self.reads = append(self.reads, block)
	answers := make([]BindingAtOutput, len(clientIds))
	for index, clientId := range clientIds {
		versions := self.versions[clientId]
		for i := len(versions) - 1; 0 <= i; i-- {
			version := versions[i]
			if block < version.landed || epoch < version.validFrom {
				continue
			}
			record := STCoordinatorBindingRecord{FleetId: [32]byte{1}, Hotkey: [32]byte{2}, ClientKey: [32]byte{3}, CommitmentHash: [32]byte{4}, Generation: version.generation, ValidFromEpoch: version.validFrom, ValidToEpoch: version.validTo, Uid: 7}
			if version.revokedAt != 0 && version.revokedAt <= block {
				record.ValidToEpoch = version.revokedEffective - 1
			}
			if version.cleanedBlock != 0 && version.cleanedBlock <= block {
				record.Cleaned, record.CleanedAtEpoch = true, version.cleanedAtEpoch
			}
			active := epoch <= record.ValidToEpoch && (!record.Cleaned || epoch < record.CleanedAtEpoch)
			if version.uidLostAt != 0 && version.uidLostAt <= block {
				active = false
			}
			answers[index] = BindingAtOutput{Active: active, Record: record}
			break
		}
	}
	return answers, nil
}

func (self *lateBindingChain) bindingsAt(t *testing.T, block uint64, clientIds ...[16]byte) []BindingAtOutput {
	t.Helper()
	answers, err := self.read(t.Context(), block, clientIds, 8)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Epoch8BindingsAt(t.Context(), protocol.Epoch8LateBindingChainID, common.Address(protocol.Epoch8LateBindingCoordinator), 8, block, clientIds, answers, self.read)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var (
	lateClient   = [16]byte{0xa1}
	beforeClient = [16]byte{0xb1}
	afterClient  = [16]byte{0xc1}
)

// A binding signed from epoch 9 that lands at 9,600,029 is bound for epoch 8
// from that block, with the epoch it counts from; before it lands it is not.
func TestEpoch8BindingsAtCountsAWindowBindingFromItsBlock(t *testing.T) {
	chain := &lateBindingChain{versions: map[[16]byte][]lateBindingVersion{
		lateClient: {{landed: 9_600_029, generation: 1, validFrom: 9, validTo: 20}},
	}}
	if got := chain.bindingsAt(t, 9_600_028, lateClient)[0]; got.Active {
		t.Fatal("bound for epoch 8 before it landed")
	}
	for _, block := range []uint64{9_600_029, 9_615_060, 9_615_061, 9_650_427, 9_650_428} {
		got := chain.bindingsAt(t, block, lateClient)[0]
		if !got.Active || got.Record.ValidFromEpoch != 8 || got.Record.ValidToEpoch != 20 || got.Record.Generation != 1 {
			t.Fatalf("at block %d: %+v, want bound for epoch 8 from epoch 8", block, got)
		}
	}
}

// One landing in the window's last block counts for epoch 8; one landing in
// the deadline block, or later, waits for epoch 9.
func TestEpoch8BindingsAtDeadline(t *testing.T) {
	chain := &lateBindingChain{versions: map[[16]byte][]lateBindingVersion{
		lateClient:  {{landed: 9_615_060, generation: 1, validFrom: 9, validTo: 20}},
		afterClient: {{landed: 9_615_061, generation: 1, validFrom: 9, validTo: 20}},
	}}
	for _, block := range []uint64{9_615_061, 9_650_427, 9_650_428} {
		got := chain.bindingsAt(t, block, lateClient, afterClient)
		if !got[0].Active || got[0].Record.ValidFromEpoch != 8 {
			t.Fatalf("at block %d the window's last block's binding is %+v", block, got[0])
		}
		if got[1].Active || got[1].Record.Generation != 0 {
			t.Fatalf("at block %d the deadline block's binding is %+v, want the coordinator's unbound answer for epoch 8", block, got[1])
		}
	}
}

// A binding made before epoch 8 keeps its coordinator answer: one signed from
// epoch 9 waits for epoch 9, and one in force for epoch 8 is never displaced by
// a window binding of the same client.
func TestEpoch8BindingsAtLeavesBindingsBeforeEpoch8(t *testing.T) {
	chain := &lateBindingChain{versions: map[[16]byte][]lateBindingVersion{
		beforeClient: {{landed: 9_600_027, generation: 1, validFrom: 9, validTo: 20}},
		lateClient: {
			{landed: 9_590_000, generation: 1, validFrom: 8, validTo: 8},
			{landed: 9_600_100, generation: 2, validFrom: 9, validTo: 20},
		},
	}}
	for _, block := range []uint64{9_600_100, 9_615_061, 9_650_428} {
		got := chain.bindingsAt(t, block, beforeClient, lateClient)
		if got[0].Active {
			t.Fatalf("at block %d a binding from before epoch 8 signed from epoch 9 counts for epoch 8", block)
		}
		if !got[1].Active || got[1].Record.Generation != 1 || got[1].Record.ValidFromEpoch != 8 {
			t.Fatalf("at block %d the binding in force for epoch 8 was displaced: %+v", block, got[1])
		}
	}
}

// Other epochs, other blocks and other coordinators read nothing more.
func TestEpoch8BindingsAtLeavesOtherReadsAlone(t *testing.T) {
	chain := &lateBindingChain{versions: map[[16]byte][]lateBindingVersion{
		lateClient: {{landed: 9_600_029, generation: 1, validFrom: 9, validTo: 20}},
	}}
	clientIds := [][16]byte{lateClient}
	for _, c := range []struct {
		chainId     uint64
		coordinator common.Address
		epoch       uint64
		block       uint64
	}{
		{protocol.Epoch8LateBindingChainID, common.Address(protocol.Epoch8LateBindingCoordinator), 9, 9_650_428},
		{protocol.Epoch8LateBindingChainID, common.Address(protocol.Epoch8LateBindingCoordinator), 7, 9_600_029},
		{945, common.Address(protocol.Epoch8LateBindingCoordinator), 8, 9_600_029},
		{protocol.Epoch8LateBindingChainID, common.HexToAddress("0x1"), 8, 9_600_029},
	} {
		chain.reads = nil
		answers, _ := chain.read(t.Context(), c.block, clientIds, c.epoch)
		chain.reads = nil
		out, err := Epoch8BindingsAt(t.Context(), c.chainId, c.coordinator, c.epoch, c.block, clientIds, answers, chain.read)
		if err != nil || len(chain.reads) != 0 || out[0] != answers[0] {
			t.Fatalf("%+v: %v, %d reads, %+v", c, err, len(chain.reads), out[0])
		}
	}
}

// Epoch 9's late binding, signed from epoch 10 in epoch 9, starts at epoch 10.
func TestEpoch8BindingsAtLeavesEpoch9LateBinding(t *testing.T) {
	chain := &lateBindingChain{versions: map[[16]byte][]lateBindingVersion{
		lateClient: {{landed: 9_650_500, generation: 1, validFrom: 10, validTo: 21}},
	}}
	for _, epoch := range []uint64{8, 9} {
		answers, _ := chain.read(t.Context(), 9_650_600, [][16]byte{lateClient}, epoch)
		out, err := Epoch8BindingsAt(t.Context(), protocol.Epoch8LateBindingChainID, common.Address(protocol.Epoch8LateBindingCoordinator), epoch, 9_650_600, [][16]byte{lateClient}, answers, chain.read)
		if err != nil || out[0].Active {
			t.Fatalf("epoch %d: %v, %+v", epoch, err, out[0])
		}
	}
}

// A window binding revoked to end with epoch 8 still counts for epoch 8, as
// every revocation takes effect at the next epoch, and so does one replaced
// after the window; one whose hotkey lost its UID or that was cleaned in
// epoch 8 no longer counts.
func TestEpoch8BindingsAtRevokedReplacedAndLost(t *testing.T) {
	revoked, replaced, lost, cleaned := [16]byte{0xd1}, [16]byte{0xd2}, [16]byte{0xd3}, [16]byte{0xd4}
	chain := &lateBindingChain{versions: map[[16]byte][]lateBindingVersion{
		revoked: {{landed: 9_600_100, generation: 1, validFrom: 9, validTo: 20, revokedAt: 9_610_000, revokedEffective: 9}},
		replaced: {
			{landed: 9_600_100, generation: 1, validFrom: 9, validTo: 20, revokedAt: 9_610_000, revokedEffective: 9},
			{landed: 9_620_000, generation: 2, validFrom: 9, validTo: 20},
		},
		lost:    {{landed: 9_600_100, generation: 1, validFrom: 9, validTo: 20, uidLostAt: 9_640_000}},
		cleaned: {{landed: 9_600_100, generation: 1, validFrom: 9, validTo: 20, uidLostAt: 9_630_000, cleanedBlock: 9_630_001, cleanedAtEpoch: 8}},
	}}
	got := chain.bindingsAt(t, 9_650_428, revoked, replaced, lost, cleaned)
	if !got[0].Active || got[0].Record.ValidFromEpoch != 8 || got[0].Record.ValidToEpoch != 8 {
		t.Fatalf("revoked: %+v", got[0])
	}
	if !got[1].Active || got[1].Record.Generation != 1 || got[1].Record.ValidFromEpoch != 8 {
		t.Fatalf("replaced: %+v", got[1])
	}
	if got[2].Active || got[3].Active {
		t.Fatalf("lost %+v, cleaned %+v", got[2], got[3])
	}
	if early := chain.bindingsAt(t, 9_620_000, lost, cleaned); !early[0].Active || !early[1].Active {
		t.Fatalf("before the UID loss: %+v", early)
	}
}
