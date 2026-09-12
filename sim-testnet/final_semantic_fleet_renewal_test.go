package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/protocol"
)

// Use real ABI bytes for every member, including the two retained active
// lifecycle predecessors, while preserving the immutable original partition.
func TestFinalFleetRenewalLineageRequiresEveryApprovedGenerationAndRevocation(t *testing.T) {
	t.Parallel()
	evidence, lineage := finalFleetRenewalLineageFixture(t)
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err != nil {
		t.Fatalf("complete approved renewal: %v", err)
	}
	evidence.FleetGeneration = lineage
	audit, err := finalPublicFleetGenerationAuditForEvidence(evidence)
	if err != nil || audit.RenewalRounds != 1 || audit.RenewedFleetVersions != 202 || audit.RenewalWrites != 1028 || audit.Generations != 400 {
		t.Fatalf("complete renewal audit %+v: %v", audit, err)
	}
	for _, test := range []struct {
		name   string
		change func(*FinalFleetGenerationLineageEvidence)
	}{
		{"omitted fleet", func(v *FinalFleetGenerationLineageEvidence) { v.Renewals[0].Fleets = v.Renewals[0].Fleets[:201] }},
		{"missing active revocation", func(v *FinalFleetGenerationLineageEvidence) { v.Renewals[0].Fleets[4].Members[0].Revocation = nil }},
		{"borrowed approval", func(v *FinalFleetGenerationLineageEvidence) {
			v.Renewals[0].ApprovedPlanHash = finalFleetGenerationTestHash(1)
		}},
		{"changed UID", func(v *FinalFleetGenerationLineageEvidence) { v.Renewals[0].Fleets[0].Version.Members[0].UID++ }},
		{"skipped generation", func(v *FinalFleetGenerationLineageEvidence) { v.Renewals[0].Fleets[0].Version.Generation++ }},
		{"replaced original", func(v *FinalFleetGenerationLineageEvidence) {
			v.SetupFleets[0].Refresh = v.Renewals[0].Fleets[0].Version
		}},
		{"missing lifecycle predecessor", func(v *FinalFleetGenerationLineageEvidence) {
			v.Renewals[0].Fleets[4].Previous = nil
			v.Renewals[0].Fleets[4].PreviousWrites = nil
		}},
		{"borrowed binding", func(v *FinalFleetGenerationLineageEvidence) {
			v.Renewals[0].Fleets[0].Members[0].Binding = v.Renewals[0].Fleets[0].Members[1].Binding
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(lineage)
			var changed FinalFleetGenerationLineageEvidence
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			test.change(&changed)
			if err := verifyFinalFleetGenerationLineage(evidence, &changed); err == nil {
				t.Fatal("accepted changed renewal lineage")
			}
		})
	}
}

// The additional scope is replayed against the same exact receipt/input path
// as original fleet writes; an operation alias cannot expand the event set.
func TestFinalFleetRenewalPublicReplayAndArchiveRejectSubstitutions(t *testing.T) {
	t.Parallel()
	evidence, lineage := finalFleetRenewalLineageFixture(t)
	write := lineage.Renewals[0].Fleets[4].Members[0].Binding
	reader := &finalFleetGenerationWriteTestReader{state: FinalFleetGenerationEVMWriteState{TransactionHash: write.Receipt.TransactionHash, To: write.CoordinatorProxy, Calldata: write.Calldata, Block: write.Receipt.Block, Status: "success", Logs: []finalCanonicalEVMLog{write.Events[0].Log}}}
	appendExchanges := func(string, ChainHead, []FinalRPCExchange) error { return nil }
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err != nil {
		t.Fatalf("approved public renewal: %v", err)
	}
	reader.state.Calldata = "0x00000000"
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted substituted renewal calldata")
	}
	reader.state.Calldata = write.Calldata
	reader.state.To = finalFleetGenerationTestBatcher
	if err := verifyFinalFleetGenerationEVMWrite(context.Background(), evidence, reader, write, appendExchanges); err == nil {
		t.Fatal("accepted another renewal target")
	}
	for _, id := range []string{"fleet.renew.01.5.bind.1", "fleet.renew.1.203.bind.1", "fleet.renew.1.5.bind.0", "fleet.renew.1.5.bind.1.extra", "fleet.renew.1.5.commitment"} {
		if _, err := finalFleetGenerationDecodeEvent(evidence, id, write.Events[0].Log); err == nil {
			t.Fatalf("accepted action alias %s", id)
		}
	}
	cache := map[string][]byte{}
	for _, batch := range lineage.Batches {
		writes := append([]FinalFleetGenerationWriteEvidence(nil), batch.CarriedHistory...)
		if batch.BatchWrite != nil {
			writes = append(writes, *batch.BatchWrite)
		}
		for _, item := range writes {
			finalFleetRenewalCacheReceipt(t, cache, item)
		}
	}
	for _, fleet := range lineage.Renewals[0].Fleets {
		for _, item := range finalFleetRenewalWrites(fleet) {
			finalFleetRenewalCacheReceipt(t, cache, item)
		}
	}
	if _, err := finalFleetGenerationArtifactEvents(lineage, cache); err != nil {
		t.Fatalf("complete renewal receipts: %v", err)
	}
	delete(cache, write.Receipt.Proof.URI)
	if _, err := finalFleetGenerationArtifactEvents(lineage, cache); err == nil {
		t.Fatal("accepted omitted renewal receipt artifact")
	}
}

func TestFinalFleetRenewalSourceRequiresExactArchivedApprovalAndSignatures(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	approved, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	baseBytes, _ := json.Marshal(fixture.base)
	approvedBytes, _ := json.Marshal(approved)
	basePath := "plan-history/" + stringsTrim0x(fixture.base.PlanHash) + ".json"
	archive := &finalSemanticArchive{files: map[string][]byte{basePath: baseBytes, "launch-foundation/plan.json": approvedBytes}, artifactDeriver: func(kind, uri string, raw []byte) (FinalArtifactLocator, error) {
		return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw))}, nil
	}}
	source := &finalFleetGenerationSource{archive: archive, current: approved, plans: map[string]*SetupPlan{approved.PlanHash: approved, fixture.base.PlanHash: fixture.base}, planPaths: map[string]string{approved.PlanHash: "launch-foundation/plan.json", fixture.base.PlanHash: basePath}, raw: map[string][]byte{}}
	round, _, err := source.renewalApproval(fixture.renewal)
	if err != nil || round.ApprovedPlanHash != approved.PlanHash || round.Approval.ContentHash != bytesSHA256(approvedBytes) {
		t.Fatalf("authenticate original append approval: %v", err)
	}
	changed := cloneFleetRenewalForTest(t, fixture.renewal)
	changed.MaximumFeePerGasWei++
	if _, _, err := source.renewalApproval(changed); err == nil {
		t.Fatal("accepted an unapproved alternate renewal fee")
	}
	delete(archive.files, basePath)
	if _, _, err := source.renewalApproval(fixture.renewal); err == nil {
		t.Fatal("accepted renewal without original approval source")
	}
	archive.files[basePath] = baseBytes
	member := fixture.renewal.Fleets[0].Members[0]
	manifest, err := protocol.ParseFleetManifest(fixture.renewal.Fleets[0].Manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := "public/fleet-1.renewal-1-member-1.binding.json"
	archive.files[path], _ = json.Marshal(member.Binding)
	if _, _, err := source.renewalBinding(path, *manifest, 1, &member.Binding); err != nil {
		t.Fatalf("exact signed binding: %v", err)
	}
	member.Binding.UID++
	if _, _, err := source.renewalBinding(path, *manifest, 1, &member.Binding); err == nil {
		t.Fatal("accepted a substituted unsigned UID projection")
	}
	delete(source.raw, path)
	member.Binding.ClientSignature = "0x" + strings.Repeat("00", 64)
	archive.files[path], _ = json.Marshal(member.Binding)
	if _, _, err := source.renewalBinding(path, *manifest, 1, nil); err == nil {
		t.Fatal("accepted invalid renewal consent")
	}
}

func TestFinalFleetRenewalCaptureOwnsExactSignedEnvelope(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	chain := big.NewInt(964)
	target := common.HexToAddress(finalFleetGenerationTestCoordinator)
	data := []byte{1, 2, 3, 4}
	action := Action{ID: "fleet.renew.1.1.bind.1", Kind: "evm-transaction", Target: target.Hex(), Parameters: map[string]string{"renewal_expected_nonce": "7", "renewal_expected_signer": crypto.PubkeyToAddress(key.PublicKey).Hex(), "renewal_calldata": "0x01020304", "operation": "bind", evmMaximumGasUnitsParameter: "400000", evmMaximumFeePerGasParameter: "25000000000"}, Spend: Spend{EVMGasWei: "10000000000000000"}}
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	plan := &SetupPlan{ChainID: chain.Uint64(), PlanHash: finalFleetGenerationTestHash(123), Actions: []Action{action}}
	sign := func(nonce, gas, fee uint64, input []byte) (*ethTypes.Transaction, []byte) {
		t.Helper()
		tx, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.LegacyTx{Nonce: nonce, To: &target, Gas: gas, GasPrice: new(big.Int).SetUint64(fee), Value: new(big.Int), Data: input}), ethTypes.LatestSignerForChainID(chain), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return tx, raw
	}
	tx, raw := sign(7, 400000, 25000000000, data)
	if err := verifyFinalFleetRenewalEnvelope(plan, action, tx.Hash().Hex(), raw, data); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		nonce, gas, fee uint64
		input           []byte
	}{{8, 400000, 25000000000, data}, {7, 400001, 25000000000, data}, {7, 400000, 25000000001, data}, {7, 400000, 25000000000, []byte{4, 3, 2, 1}}} {
		changed, raw := sign(mutation.nonce, mutation.gas, mutation.fee, mutation.input)
		if err := verifyFinalFleetRenewalEnvelope(plan, action, changed.Hash().Hex(), raw, data); err == nil {
			t.Fatal("accepted an envelope outside exact approval")
		}
	}
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(state, "transactions"), 0o700); err != nil {
		t.Fatal(err)
	}
	name := "transactions/" + stringsTrim0x(tx.Hash().Hex()) + ".rlp"
	if err := os.WriteFile(filepath.Join(state, name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{Stage: StageFinalized, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, TransactionHash: tx.Hash().Hex()}
	files, err := captureFinalFleetRenewalTransactionEntries(context.Background(), state, plan, []JournalEntry{entry})
	if err != nil || len(files) != 1 || files[0].Path != name || !bytes.Equal(files[0].Data, raw) {
		t.Fatalf("capture exact finalized RLP: %v", err)
	}
	entry.IntentHash = finalFleetGenerationTestHash(456)
	if _, err := captureFinalFleetRenewalTransactionEntries(context.Background(), state, plan, []JournalEntry{entry}); err == nil {
		t.Fatal("captured another action's finalized envelope")
	}
	if _, err := captureFinalFleetRenewalTransactionEntries(context.Background(), state, plan, nil); err == nil {
		t.Fatal("accepted missing renewal transaction")
	}
}

func finalFleetRenewalCacheReceipt(t *testing.T, cache map[string][]byte, write FinalFleetGenerationWriteEvidence) {
	t.Helper()
	logs := make([]finalCanonicalEVMLog, 0, len(write.Events))
	for _, event := range write.Events {
		logs = append(logs, event.Log)
	}
	raw, err := json.Marshal(finalFleetGenerationReceiptArtifact{TransactionHash: write.Receipt.TransactionHash, Block: write.Receipt.Block, Status: write.Receipt.Status, Logs: logs})
	if err != nil {
		t.Fatal(err)
	}
	cache[write.Receipt.Proof.URI] = raw
}

func finalFleetRenewalLineageFixture(t *testing.T) (*FinalSemanticEvidence, *FinalFleetGenerationLineageEvidence) {
	t.Helper()
	evidence, lineage := finalFleetGenerationTestFixture(t)
	evidence.Deployment = finalFleetGenerationEventFixture(t).Deployment
	// The older structural fixture deliberately uses local counter ranges;
	// generation-two counters overlap historical per-member counters. A full
	// receipt archive needs globally distinct transaction identities.
	for index := range lineage.Batches {
		batch := &lineage.Batches[index]
		if batch.BatchWrite == nil {
			continue
		}
		write := finalFleetGenerationTestWrite(40_000+batch.Generation*100+batch.Batch, batch.Action, finalFleetGenerationTestBatcher, batch.BatcherRuntimeHash)
		write.Postcondition = batch.Postcondition
		batch.BatchWrite = &write
		batch.CalldataHash, batch.EventHash, batch.CoordinatorRuntimeHash = write.CalldataHash, write.EventHash, write.CoordinatorRuntimeHash
	}
	latest := map[uint64]FinalFleetGenerationVersionEvidence{}
	fixMembers := func(version *FinalFleetGenerationVersionEvidence, fleet uint64) {
		for i := range version.Members {
			version.Members[i].ClientID = fmt.Sprintf("0x%032x", fleet*4+uint64(i)+1)
			version.Members[i].UID = uint16(fleet + 2)
		}
	}
	for i := range lineage.SetupFleets {
		row := &lineage.SetupFleets[i]
		fixMembers(&row.Initial, row.FleetID)
		fixMembers(&row.Refresh, row.FleetID)
		latest[row.FleetID] = row.Refresh
	}
	for i := range lineage.ChallengerFleets {
		row := &lineage.ChallengerFleets[i]
		fixMembers(&row.Initial, row.FleetID)
		latest[row.FleetID] = row.Initial
	}
	round := FinalFleetRenewalRoundEvidence{Round: 1, SourcePlanHash: finalFleetGenerationTestHash(999001), ApprovedPlanHash: finalFleetGenerationTestHash(999002), MetadataHash: finalFleetGenerationTestHash(999003), Approval: finalFleetGenerationTestArtifact("fleet-renewal-approval", "renewal-1"), ValidFromEpoch: 318, ValidToEpoch: 349}
	serial := uint64(0)
	write := func(id, name string, version FinalFleetGenerationVersionEvidence, member FinalFleetGenerationMemberEvidence) FinalFleetGenerationWriteEvidence {
		serial++
		return finalFleetRenewalABIWrite(t, evidence, id, round.ApprovedPlanHash, name, version, member, serial)
	}
	for fleet := uint64(1); fleet <= 202; fleet++ {
		before := latest[fleet]
		row := FinalFleetRenewalFleetEvidence{FleetID: fleet}
		advance := func(before FinalFleetGenerationVersionEvidence, generation uint64, id string) FinalFleetGenerationVersionEvidence {
			version := finalFleetGenerationTestVersion(fleet, generation, 0)
			version.Hotkey = before.Hotkey
			version.CommitmentAction.ActionID, version.CommitmentAction.PlanHash = id, round.ApprovedPlanHash
			version.NativeHead = finalFleetGenerationTestHead(before.NativeHead.Number + 100)
			for i := range version.Members {
				prior := before.Members[i]
				version.Members[i].ClientID, version.Members[i].ClientKey, version.Members[i].FleetKey, version.Members[i].Hotkey, version.Members[i].UID = prior.ClientID, prior.ClientKey, prior.FleetKey, version.Hotkey, prior.UID
				version.Members[i].ValidFromEpoch, version.Members[i].ValidToEpoch = round.ValidFromEpoch, round.ValidToEpoch
			}
			return version
		}
		if fleet == 5 || fleet == 6 {
			prefix := finalFleetRenewalPreviousPrefix(fleet, 3)
			previous := advance(before, 3, prefix+".commitment")
			for i := range previous.Members {
				previous.Members[i].ValidFromEpoch, previous.Members[i].ValidToEpoch = 294, 325
			}
			row.Previous = &previous
			row.PreviousWrites = append(row.PreviousWrites, write(prefix+".mirror", "CommitmentMirrored", previous, previous.Members[0]))
			for i, member := range previous.Members {
				row.PreviousWrites = append(row.PreviousWrites, write(fmt.Sprintf("%s.bind.%d", prefix, i+1), "FleetBound", previous, member))
			}
			before = previous
		}
		row.Version = advance(before, before.Generation+1, fleetRenewalActionID(1, int(fleet), "commitment", 0))
		row.Mirror = write(fleetRenewalActionID(1, int(fleet), "mirror", 0), "CommitmentMirrored", row.Version, row.Version.Members[0])
		for i, prior := range before.Members {
			member := FinalFleetRenewalMemberEvidence{Member: uint64(i + 1), Prior: prior}
			if prior.ValidToEpoch >= round.ValidFromEpoch {
				revoke := write(fleetRenewalActionID(1, int(fleet), "revoke", i+1), "FleetBindingRevoked", row.Version, prior)
				member.Revocation = &revoke
			}
			member.Binding = write(fleetRenewalActionID(1, int(fleet), "bind", i+1), "FleetBound", row.Version, row.Version.Members[i])
			row.Members = append(row.Members, member)
		}
		round.Fleets = append(round.Fleets, row)
	}
	lineage.Renewals = []FinalFleetRenewalRoundEvidence{round}
	return evidence, lineage
}

func finalFleetRenewalABIWrite(t *testing.T, evidence *FinalSemanticEvidence, id, planHash, name string, version FinalFleetGenerationVersionEvidence, member FinalFleetGenerationMemberEvidence, serial uint64) FinalFleetGenerationWriteEvidence {
	t.Helper()
	coordinator, _, err := finalFleetGenerationABIs()
	if err != nil {
		t.Fatal(err)
	}
	event := coordinator.Events[name]
	decode32 := func(value string) [32]byte { return common.HexToHash(value) }
	var client [16]byte
	copy(client[:], common.FromHex(member.ClientID))
	values := map[string]any{"hotkey": decode32(version.Hotkey), "commitmentHash": decode32(version.CommitmentHash), "fleetId": decode32(member.FleetKey), "clientId": client, "uid": member.UID, "generation": member.Generation, "validFromEpoch": member.ValidFromEpoch, "validToEpoch": member.ValidToEpoch, "effectiveEpoch": uint64(318), "finalizedBlock": version.NativeHead.Number, "finalizedBlockHash": decode32(version.NativeHead.Hash)}
	topics := []string{strings.ToLower(event.ID.Hex())}
	dataValues := []any{}
	for _, input := range event.Inputs {
		value, ok := values[input.Name]
		if !ok {
			t.Fatalf("missing ABI field %s", input.Name)
		}
		if input.Indexed {
			var topic common.Hash
			switch typed := value.(type) {
			case [32]byte:
				copy(topic[:], typed[:])
			case [16]byte:
				copy(topic[:], typed[:])
			default:
				t.Fatalf("unexpected indexed field %s", input.Name)
			}
			topics = append(topics, strings.ToLower(topic.Hex()))
		} else {
			dataValues = append(dataValues, value)
		}
	}
	data, err := event.Inputs.NonIndexed().Pack(dataValues...)
	if err != nil {
		t.Fatal(err)
	}
	write := finalFleetGenerationTestWrite(50000+serial, FinalFleetGenerationActionEvidence{ActionID: id, PlanHash: planHash, IntentHash: finalFleetGenerationTestHash(990000 + serial)}, evidence.Deployment.CoordinatorProxy, "")
	write.CoordinatorProxy, write.CoordinatorImplementation = evidence.Deployment.CoordinatorProxy, evidence.Deployment.CoordinatorImplementation
	write.CoordinatorImplementationSlot = evidence.Deployment.ObservedImplementationSlot
	log := finalCanonicalEVMLog{Address: write.CoordinatorProxy, Topics: topics, Data: "0x" + common.Bytes2Hex(data), TransactionHash: write.Receipt.TransactionHash, BlockNumber: write.Receipt.Block.Number, BlockHash: write.Receipt.Block.Hash}
	decoded, err := finalFleetGenerationDecodeEvent(evidence, id, log)
	if err != nil {
		t.Fatal(err)
	}
	write.Events = []FinalFleetGenerationEventEvidence{decoded.Evidence}
	write.EventHash, err = canonicalHashHex(write.Events)
	if err != nil {
		t.Fatal(err)
	}
	write.Receipt.LogsHash, err = finalCanonicalReceiptLogsHash([]finalCanonicalEVMLog{log})
	if err != nil {
		t.Fatal(err)
	}
	return write
}
