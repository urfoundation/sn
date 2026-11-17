// A complete per-contract chain needs each independently signed original and
// original registered key. The containing operator signature cannot fill holes.
package payoutartifact

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

func inventoryArtifact(t *testing.T) (*Artifact, *ecdsa.PrivateKey) {
	t.Helper()
	artifact, root := closedReportsTestArtifact(t)
	domain, _ := ClosedWorkReportDomain(artifact)
	digest, _ := domain.Digest()
	for index := range artifact.ClosedWork.Records {
		row := &artifact.ClosedWork.Records[index]
		reports := ClosedWorkReports{Schema: ClosedWorkInventoryReportsSchema, Count: 4}
		for party := 0; party < 2; party++ {
			var previous [32]byte
			var previousRegistration [32]byte
			for step := 0; step < 2; step++ {
				client := [16]byte{byte(party + 1)}
				id := [16]byte{byte(index + 20), byte(party + 1), byte(step + 1)}
				key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(80 + party*2 + step)}, 32))
				amount := uint64(40 + 20*step)
				unacked := uint64(3)
				checkpoint := step == 0
				original, err := coreprotocol.SignOriginalCloseReport(coreprotocol.OriginalCloseReport{DomainHash: digest, ClientId: client, ContractId: row.ContractId, ReportId: id, AckedByteCount: amount, UnackedByteCount: unacked, Checkpoint: checkpoint}, key)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := original.Bytes()
				total := uint64(40)
				if step == 1 {
					total = 100
				}
				inventory, err := coreprotocol.SignOriginalCloseInventory(coreprotocol.OriginalCloseInventory{DomainHash: digest, ClientId: client, ContractId: row.ContractId, ReportHash: sha256.Sum256(raw), Sequence: uint64(step + 1), CumulativeAckedBytes: total, Previous: previous, Terminal: !checkpoint}, key)
				if err != nil {
					t.Fatal(err)
				}
				wire, _ := inventory.Bytes()
				previous = sha256.Sum256(wire)
				registration := protocol.ClientKeyRegistration{Domain: domain, ClientID: client, NetworkID: [16]byte{byte((party + 1) * 10)}, Generation: uint64(step + 1), PreviousHash: previousRegistration, Present: true, PublicKey: original.PublicKey, EffectiveBoundary: protocol.ClientKeyEffectiveBoundary{Block: artifact.Start.Number + 1, Hash: [32]byte{byte(20 + step)}, Epoch: artifact.Epoch}}
				if err := protocol.SignClientKeyRegistration(&registration, root); err != nil {
					t.Fatal(err)
				}
				registered, _ := registration.Bytes()
				previousRegistration = sha256.Sum256(registered)
				idText := func(id [16]byte) string {
					return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
				}
				name := "source"
				if party == 1 {
					name = "destination"
				}
				reports.Reports = append(reports.Reports, ClosedWorkReport{ClientId: idText(client), ReportId: idText(id), Party: name, AckedBytes: &amount, UnackedBytes: &unacked, Checkpoint: &checkpoint, Original: raw, KeyRegistration: registered, Inventory: wire})
			}
		}
		row.OriginalReports, _ = json.Marshal(reports)
	}
	closedWorkTestSign(t, artifact)
	return artifact, root
}

func TestClosedInventoryOriginalRotationProvesEveryContractIncrement(t *testing.T) {
	artifact, root := inventoryArtifact(t)
	raw, err := BytesWithContext(t.Context(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWithContext(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := VerifyClosedWorkReports(t.Context(), decoded, crypto.PubkeyToAddress(root.PublicKey))
	if err != nil || value.InventoryReports != 8 || value.CompleteReportInventories != 2 || value.AmountJoins != 2 || value.RegisteredReports != 8 {
		t.Fatal("original signed inventory did not join every increment across rotation", value, err)
	}
}

func TestClosedInventoryMissingCheckpointAndTerminalRemainUnknown(t *testing.T) {
	for _, index := range []int{0, 1} {
		artifact, root := inventoryArtifact(t)
		changeClosedReports(t, artifact, func(c *ClosedWorkReports) { c.Reports = append(c.Reports[:index], c.Reports[index+1:]...); c.Count-- })
		value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey))
		if err != nil || value.CompleteReportInventories != 1 {
			t.Fatal("partial original inventory became complete", value, err)
		}
	}
}

func TestClosedInventoryResignedPredecessorCumulativeAndDuplicateRefuse(t *testing.T) {
	for _, change := range []func(*coreprotocol.OriginalCloseInventory){func(v *coreprotocol.OriginalCloseInventory) { v.Previous[0] ^= 1 }, func(v *coreprotocol.OriginalCloseInventory) { v.CumulativeAckedBytes++ }, func(v *coreprotocol.OriginalCloseInventory) { v.Sequence = 1; v.Previous = [32]byte{} }} {
		artifact, root := inventoryArtifact(t)
		changeClosedReports(t, artifact, func(c *ClosedWorkReports) {
			value, _ := coreprotocol.DecodeOriginalCloseInventory(c.Reports[1].Inventory)
			change(&value)
			value, err := coreprotocol.SignOriginalCloseInventory(value, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{81}, 32)))
			if err != nil {
				t.Fatal(err)
			}
			c.Reports[1].Inventory, _ = value.Bytes()
		})
		if _, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("validly signed inventory contradiction was accepted", err)
		}
	}
}

func TestClosedInventoryForeignProviderAndMissingRegistrationCannotAuthorize(t *testing.T) {
	artifact, root := inventoryArtifact(t)
	changeClosedReports(t, artifact, func(c *ClosedWorkReports) { c.Reports[0].KeyRegistration = nil })
	value, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey))
	if err != nil || value.CompleteReportInventories != 1 {
		t.Fatal("client chain replaced original key authority", value, err)
	}
	artifact, root = inventoryArtifact(t)
	changeClosedReports(t, artifact, func(c *ClosedWorkReports) {
		registration, _ := protocol.DecodeClientKeyRegistration(c.Reports[0].KeyRegistration)
		registration.NetworkID[0]++
		if err := protocol.SignClientKeyRegistration(&registration, root); err != nil {
			t.Fatal(err)
		}
		c.Reports[0].KeyRegistration, _ = registration.Bytes()
	})
	if _, err := VerifyClosedWorkReports(t.Context(), artifact, crypto.PubkeyToAddress(root.PublicKey)); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("inventory migrated a foreign provider identity", err)
	}
}

// Even an incomplete prefix must refuse a contradiction between the remaining
// original consecutive links. Missing evidence never hides known disagreement.
func TestClosedInventoryGapCannotHideConsecutiveConflictOrPostTerminalReport(t *testing.T) {
	for _, postTerminal := range []bool{false, true} {
		artifact, _ := inventoryArtifact(t)
		var census ClosedWorkReports
		if err := json.Unmarshal(artifact.ClosedWork.Records[0].OriginalReports, &census); err != nil {
			t.Fatal(err)
		}
		var previous [32]byte
		for index := 0; index < 2; index++ {
			row := &census.Reports[index]
			key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(80 + index)}, 32))
			original, err := coreprotocol.DecodeOriginalCloseReport(row.Original)
			if err != nil {
				t.Fatal(err)
			}
			if postTerminal && index == 0 {
				original.Checkpoint = false
				original, err = coreprotocol.SignOriginalCloseReport(original, key)
				if err != nil {
					t.Fatal(err)
				}
				row.Original, _ = original.Bytes()
			}
			inventory, err := coreprotocol.DecodeOriginalCloseInventory(row.Inventory)
			if err != nil {
				t.Fatal(err)
			}
			if postTerminal {
				inventory.Terminal = true
				inventory.Previous = previous
				inventory.ReportHash = sha256.Sum256(row.Original)
			} else {
				inventory.Sequence += 1
				inventory.Previous = [32]byte{99}
			}
			inventory, err = coreprotocol.SignOriginalCloseInventory(inventory, key)
			if err != nil {
				t.Fatal(err)
			}
			row.Inventory, _ = inventory.Bytes()
			previous = sha256.Sum256(row.Inventory)
		}
		if _, _, err := verifyClosedReportInventory(t.Context(), census); !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("missing prefix or terminal report concealed a present contradictory link", err)
		}
	}
}

func inventoryWindow(t *testing.T, artifact *Artifact) *ClosedWorkWindow {
	t.Helper()
	window := &ClosedWorkWindow{Schema: ClosedWorkWindowSchema, Start: artifact.ClosedWork.WindowStart, End: artifact.ClosedWork.WindowEnd, Records: []ClosedWorkWindowRecord{}}
	for _, row := range artifact.ClosedWork.Records {
		closed := row.ClosedAt
		id := row.ContractId
		window.Records = append(window.Records, ClosedWorkWindowRecord{ContractId: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), Disposition: "credited", ClosedAt: &closed, Original: bytes.Clone(row.Original)})
	}
	return window
}

func TestClosedWindowIndependentClockAndCanceledInventoryStayDistinct(t *testing.T) {
	artifact, _ := inventoryArtifact(t)
	window := inventoryWindow(t, artifact)
	closed := window.Records[0].ClosedAt
	window.Records = append(window.Records, ClosedWorkWindowRecord{ContractId: "f1000000-0000-0000-0000-000000000000", Disposition: "canceled", ClosedAt: closed}, ClosedWorkWindowRecord{ContractId: "f2000000-0000-0000-0000-000000000000", Disposition: "open"}, ClosedWorkWindowRecord{ContractId: "f3000000-0000-0000-0000-000000000000", Disposition: "unassigned_canceled"})
	start, _ := time.Parse(time.RFC3339Nano, window.Start)
	end, _ := time.Parse(time.RFC3339Nano, window.End)
	clock := &ClosedWorkWindowClock{Start: artifact.Start, End: artifact.End, StartTime: start, EndTime: end}
	startHeader := &types.Header{Number: new(big.Int).SetUint64(artifact.Start.Number), Time: uint64(start.Unix())}
	endHeader := &types.Header{Number: new(big.Int).SetUint64(artifact.End.Number), Time: uint64(end.Unix())}
	clock.Start.Hash, clock.End.Hash = startHeader.Hash().Hex(), endHeader.Hash().Hex()
	artifact.Start, artifact.End = clock.Start, clock.End
	clock.StartHeader, _ = rlp.EncodeToBytes(startHeader)
	clock.EndHeader, _ = rlp.EncodeToBytes(endHeader)
	value, err := VerifyClosedWorkWindow(t.Context(), artifact, window, clock)
	if err != nil || value.Credited != 2 || value.Canceled != 1 || value.Open != 1 || value.UnassignedCanceled != 1 || !value.EpochClockMatched {
		t.Fatal("original closure dispositions or independent clock were lost", value, err)
	}
	for _, change := range []func(*ClosedWorkWindowClock){func(v *ClosedWorkWindowClock) { v.Start.Hash = "0xforeign" }, func(v *ClosedWorkWindowClock) { v.StartTime = v.StartTime.Add(time.Second) }, func(v *ClosedWorkWindowClock) { v.End.Number++ }, func(v *ClosedWorkWindowClock) { v.StartHeader = nil }, func(v *ClosedWorkWindowClock) {
		v.EndHeader = bytes.Clone(v.EndHeader)
		v.EndHeader[len(v.EndHeader)-1] ^= 1
	}} {
		other := *clock
		change(&other)
		value, err := VerifyClosedWorkWindow(t.Context(), artifact, window, &other)
		if err != nil || value.EpochClockMatched {
			t.Fatal("foreign or skewed clock authorized the claimed window", value, err)
		}
	}
}

func TestClosedWindowOmittedExtraDuplicateAndCrossWindowCreditRefuse(t *testing.T) {
	for _, change := range []func(*ClosedWorkWindow){func(v *ClosedWorkWindow) { v.Records = v.Records[1:] }, func(v *ClosedWorkWindow) { v.Records = append(v.Records, v.Records[len(v.Records)-1]) }, func(v *ClosedWorkWindow) { v.Records[0].ContractId = "03000000-0000-0000-0000-000000000000" }, func(v *ClosedWorkWindow) { late := v.End; v.Records[0].ClosedAt = &late }, func(v *ClosedWorkWindow) { v.Records[0].Disposition = "canceled" }} {
		artifact, _ := inventoryArtifact(t)
		window := inventoryWindow(t, artifact)
		change(window)
		if _, err := VerifyClosedWorkWindow(t.Context(), artifact, window, nil); !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("conflicting original window census was accepted", err)
		}
	}
}

func TestClosedWindowCancellationCapacityAndMissingAuthorityStayTyped(t *testing.T) {
	artifact, root := inventoryArtifact(t)
	window := inventoryWindow(t, artifact)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := VerifyClosedWorkWindow(ctx, artifact, window, nil); !errors.Is(err, context.Canceled) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("window owner cancellation became an integrity hold", err)
	}
	if _, err := VerifyClosedWorkReports(ctx, artifact, crypto.PubkeyToAddress(root.PublicKey)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	window.Records = make([]ClosedWorkWindowRecord, MaxClosedWorkRecords+1)
	if _, err := VerifyClosedWorkWindow(t.Context(), artifact, window, nil); !errors.Is(err, ErrClosedWorkCapacity) {
		t.Fatal("unbounded window inventory was admitted", err)
	}
	if _, err := VerifyClosedWorkWindow(t.Context(), artifact, nil, nil); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("missing original inventory became authority", err)
	}
}
