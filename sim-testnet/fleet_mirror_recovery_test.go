// Fleet-mirror recovery tests reproduce interrupted finality bookkeeping and
// prove that only the exact convergent transaction can cross a plan revision.
package main

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Holds the complete approved identity used by signed-transaction mutation
// tests. Every field is an explicit stable value; no network or clock is used.
type fleetMirrorRecoveryTestFixture struct {
	cfg                *ResolvedConfig
	plan               *SetupPlan
	action             Action
	privateKeyHex      string
	otherPrivateKeyHex string
	coordinator        common.Address
	hotkey             [32]byte
	commitmentHash     [32]byte
	finalizedBlock     uint64
	finalizedBlockHash [32]byte
}

// Describes all signed fields that an adjacent mutation may change.
type fleetMirrorSignedTestFields struct {
	privateKeyHex      string
	chainID            uint64
	to                 *common.Address
	value              uint64
	gas                uint64
	gasFeeCap          uint64
	gasTipCap          uint64
	hotkey             [32]byte
	commitmentHash     [32]byte
	finalizedBlock     uint64
	finalizedBlockHash [32]byte
	wrongMethod        bool
}

// Describes receipt and event mutations independently from signed calldata.
type fleetMirrorReceiptTestFields struct {
	status              uint64
	transactionHash     common.Hash
	eventHotkey         [32]byte
	eventCommitment     [32]byte
	eventBlock          uint64
	eventBlockHash      [32]byte
	eventCount          int
	malformedExtraEvent bool
}

// Construct the exact release-shaped action, signer, call, and commitment.
func newFleetMirrorRecoveryTestFixture(t *testing.T) fleetMirrorRecoveryTestFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	privateKeyHex := strings.Repeat("11", 32)
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	action := Action{
		ID: "fleet.mirror.4", Kind: "evm-transaction", Target: "head-fleet:4",
		Description: "mirror the independently verified finalized native commitment into the coordinator",
		Parameters: map[string]string{
			fleetCommitmentStorageParameter: fleetCommitmentStorageV2,
			evmMaximumGasUnitsParameter:     "200000",
			evmMaximumFeePerGasParameter:    "100",
		},
		Spend:     Spend{EVMGasWei: DecimalUint("20000000")},
		DependsOn: []string{"fleet.commitment.4", "evm.fund-commitment-oracle"},
	}
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := common.HexToAddress("0x1234567890123456789012345678901234567890")
	var hotkey, commitmentHash, finalizedBlockHash [32]byte
	hotkey[0], commitmentHash[0], finalizedBlockHash[0] = 0x41, 0x42, 0x43
	plan := &SetupPlan{
		PlanHash: "0x" + strings.Repeat("51", 32), ChainID: cfg.ChainID,
		Roles:      PublicRoles{CommitmentOracle: crypto.PubkeyToAddress(privateKey.PublicKey).Hex()},
		Deployment: ContractDeployment{CoordinatorProxy: coordinator}, Actions: []Action{action},
	}
	return fleetMirrorRecoveryTestFixture{
		cfg: cfg, plan: plan, action: action, privateKeyHex: privateKeyHex, otherPrivateKeyHex: strings.Repeat("22", 32),
		coordinator: coordinator, hotkey: hotkey, commitmentHash: commitmentHash, finalizedBlock: 7_897_001, finalizedBlockHash: finalizedBlockHash,
	}
}

// Return a fresh copy of the exact approved signed fields.
func (self fleetMirrorRecoveryTestFixture) signedFields() fleetMirrorSignedTestFields {
	to := self.coordinator
	return fleetMirrorSignedTestFields{
		privateKeyHex: self.privateKeyHex, chainID: self.cfg.ChainID, to: &to,
		gas: 200_000, gasFeeCap: 100, gasTipCap: 1, hotkey: self.hotkey,
		commitmentHash: self.commitmentHash, finalizedBlock: self.finalizedBlock, finalizedBlockHash: self.finalizedBlockHash,
	}
}

// Sign one deterministic dynamic-fee transaction from supplied fields.
func (self fleetMirrorRecoveryTestFixture) sign(t *testing.T, fields fleetMirrorSignedTestFields) *ethTypes.Transaction {
	t.Helper()
	contract := stabi.NewSTCoordinator()
	data, err := contract.TryPackMirrorCommitment(fields.hotkey, fields.commitmentHash, fields.finalizedBlock, fields.finalizedBlockHash)
	if fields.wrongMethod {
		data = contract.PackCurrentEpoch()
	}
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := crypto.HexToECDSA(fields.privateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	transaction := ethTypes.NewTx(&ethTypes.DynamicFeeTx{
		ChainID: new(big.Int).SetUint64(fields.chainID), Nonce: 7,
		GasTipCap: new(big.Int).SetUint64(fields.gasTipCap), GasFeeCap: new(big.Int).SetUint64(fields.gasFeeCap), Gas: fields.gas,
		To: fields.to, Value: new(big.Int).SetUint64(fields.value), Data: data,
	})
	signed, err := ethTypes.SignTx(transaction, ethTypes.LatestSignerForChainID(transaction.ChainId()), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// Return a fresh exact successful receipt description for one signed call.
func (self fleetMirrorRecoveryTestFixture) receiptFields(signed *ethTypes.Transaction) fleetMirrorReceiptTestFields {
	return fleetMirrorReceiptTestFields{
		status: ethTypes.ReceiptStatusSuccessful, transactionHash: signed.Hash(), eventHotkey: self.hotkey,
		eventCommitment: self.commitmentHash, eventBlock: self.finalizedBlock, eventBlockHash: self.finalizedBlockHash, eventCount: 1,
	}
}

// Encode one or more deterministic CommitmentMirrored logs.
func (self fleetMirrorRecoveryTestFixture) receipt(t *testing.T, fields fleetMirrorReceiptTestFields) *ethTypes.Receipt {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events[stabi.STCoordinatorCommitmentMirroredEventName]
	data, err := event.Inputs.NonIndexed().Pack(fields.eventBlock, fields.eventBlockHash)
	if err != nil {
		t.Fatal(err)
	}
	logs := make([]*ethTypes.Log, 0, fields.eventCount+1)
	for index := 0; index < fields.eventCount; index++ {
		logs = append(logs, &ethTypes.Log{
			Address: self.coordinator,
			Topics:  []common.Hash{event.ID, common.BytesToHash(fields.eventHotkey[:]), common.BytesToHash(fields.eventCommitment[:])},
			Data:    data,
		})
	}
	if fields.malformedExtraEvent {
		logs = append(logs, &ethTypes.Log{Address: self.coordinator, Topics: []common.Hash{event.ID}, Data: []byte{0x01}})
	}
	return &ethTypes.Receipt{
		Status: fields.status, TxHash: fields.transactionHash, BlockNumber: big.NewInt(77),
		BlockHash: common.BytesToHash([]byte{0x77}), Logs: logs,
	}
}

// Build the canonical fleet-4 manifest from deterministic public client keys
// while assigning stable non-derivable client IDs as the operator API does.
func fleetMirrorManifestTestFixture(t *testing.T, fixture fleetMirrorRecoveryTestFixture) (*RoleSecrets, protocol.FleetManifest) {
	t.Helper()
	roles, err := BuildRoleSecrets(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := roleBytes32(roles, fleetHotkeyLabel(4))
	if err != nil {
		t.Fatal(err)
	}
	manifest := protocol.FleetManifest{
		Schema: protocol.FleetManifestSchema, ChainID: fixture.cfg.ChainID, Netuid: fixture.cfg.Netuid,
		FleetID: derive32(fixture.cfg, "fleet-id/4"), Hotkey: hotkey, Generation: 1,
	}
	copy(manifest.Coordinator[:], fixture.coordinator.Bytes())
	for member := 1; member <= fixture.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(fixture.cfg, 4, member)
		role := roles.Clients["miner-"+strconv.Itoa(miner)]
		keyBytes, err := hex.DecodeString(role.PublicKeyHex)
		if err != nil || len(keyBytes) != 32 {
			t.Fatalf("miner-%d public key: %v", miner, err)
		}
		var client protocol.FleetMember
		client.ClientID[15] = byte(member)
		copy(client.ClientKey[:], keyBytes)
		manifest.Members = append(manifest.Members, client)
	}
	return roles, manifest
}

// Reproduce the provisioned-identity boundary: derivation has no client IDs,
// yet the canonical public preimage remains fully authenticatable read-only.
func TestFleetMirrorRecoveryLoadsProvisionedClientIDsFromCommittedManifest(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	roles, manifest := fleetMirrorManifestTestFixture(t, fixture)
	for miner := 13; miner <= 16; miner++ {
		if roles.Clients["miner-"+strconv.Itoa(miner)].ClientIDHex != "" {
			t.Fatalf("miner-%d unexpectedly had a derivable client ID", miner)
		}
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := atomicWrite(filepath.Join(stateDir, "public", "fleet-4.json"), append(canonical, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	got, gotHash, err := loadFleetMirrorManifest(fixture.cfg, stateDir, roles, 4, fixture.coordinator)
	if err != nil {
		t.Fatalf("canonical provisioned manifest was rejected: %v", err)
	}
	wantHash, err := manifest.CommitmentHash()
	gotCanonical, canonicalErr := got.Canonical()
	if err != nil || canonicalErr != nil || !bytes.Equal(gotCanonical, canonical) || gotHash != wantHash {
		t.Fatalf("loaded manifest/hash changed: manifest=%+v hash=0x%x want=0x%x error=%v", got, gotHash, wantHash, err)
	}
}

// Reject every valid but identity-confused public manifest adjacent to the
// provisioned client-ID recovery path.
func TestFleetMirrorRecoveryRejectsWrongCommittedManifestIdentity(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	type mutation struct {
		name   string
		change func(*protocol.FleetManifest)
	}
	mutations := []mutation{
		{name: "chain", change: func(manifest *protocol.FleetManifest) { manifest.ChainID++ }},
		{name: "netuid", change: func(manifest *protocol.FleetManifest) { manifest.Netuid++ }},
		{name: "coordinator", change: func(manifest *protocol.FleetManifest) { manifest.Coordinator[0]++ }},
		{name: "fleet id", change: func(manifest *protocol.FleetManifest) { manifest.FleetID[0]++ }},
		{name: "hotkey", change: func(manifest *protocol.FleetManifest) { manifest.Hotkey[0]++ }},
		{name: "generation", change: func(manifest *protocol.FleetManifest) { manifest.Generation++ }},
		{name: "client key", change: func(manifest *protocol.FleetManifest) { manifest.Members[0].ClientKey[0]++ }},
		{name: "member count", change: func(manifest *protocol.FleetManifest) { manifest.Members = manifest.Members[:len(manifest.Members)-1] }},
	}
	for _, mutation := range mutations {
		roles, manifest := fleetMirrorManifestTestFixture(t, fixture)
		mutation.change(&manifest)
		canonical, err := manifest.Canonical()
		if err != nil {
			t.Fatalf("%s fixture is not a valid adjacent manifest: %v", mutation.name, err)
		}
		stateDir := t.TempDir()
		if err := atomicWrite(filepath.Join(stateDir, "public", "fleet-4.json"), append(canonical, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadFleetMirrorManifest(fixture.cfg, stateDir, roles, 4, fixture.coordinator); err == nil {
			t.Errorf("%s manifest mutation was accepted", mutation.name)
		}
	}
}

// Reproduce the live defect: the journal has only a broadcast checkpoint, but
// the provider proves an exact canonical successful receipt. The successful
// receipt is recoverable only after all signed/event fields authenticate.
func TestFinalizedFleetMirrorRecoveryAcceptsMissingJournalInclusionCheckpoint(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	signed := fixture.sign(t, fixture.signedFields())
	receipt := fixture.receipt(t, fixture.receiptFields(signed))
	canonical := ChainHead{Number: 77, Hash: receipt.BlockHash.Hex()}
	reader := &evmFinalityFixture{
		finalized: ChainHead{Number: 78, Hash: common.BytesToHash([]byte{0x78}).Hex()},
		canonical: map[uint64]ChainHead{77: canonical}, receipt: receipt,
	}
	transaction := planRevisionTransaction{TransactionHash: signed.Hash().Hex(), RecoveryBlock: 76, RecoveryBlockHash: common.BytesToHash([]byte{0x76}).Hex()}
	observed, err := canonicalFinalizedEVMRevisionReceiptFromReader(t.Context(), reader, transaction)
	if err != nil || observed != receipt {
		t.Fatalf("canonical successful receipt without journal inclusion was rejected: receipt=%p/%p error=%v", observed, receipt, err)
	}
	if err := validateFinalizedFleetMirrorTransaction(fixture.cfg, fixture.plan, fixture.action, signed, receipt, fixture.coordinator, fixture.hotkey, fixture.commitmentHash, fixture.finalizedBlock, fixture.finalizedBlockHash); err != nil {
		t.Fatalf("exact finalized fleet mirror was rejected: %v", err)
	}
}

// Reject every adjacent mutation of the signed call or its receipt even when
// the transaction remains syntactically valid and successful.
func TestFinalizedFleetMirrorRecoveryRejectsSignedAndReceiptMutations(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	type mutation struct {
		name          string
		changeSigned  func(*fleetMirrorSignedTestFields)
		changeReceipt func(*fleetMirrorReceiptTestFields)
	}
	mutations := []mutation{
		{name: "signer", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.privateKeyHex = fixture.otherPrivateKeyHex }},
		{name: "chain", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.chainID-- }},
		{name: "destination", changeSigned: func(fields *fleetMirrorSignedTestFields) { other := common.HexToAddress("0x1"); fields.to = &other }},
		{name: "creation destination", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.to = nil }},
		{name: "value", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.value = 1 }},
		{name: "gas", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.gas++ }},
		{name: "fee cap", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.gasFeeCap++ }},
		{name: "tip cap", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.gasTipCap = 101; fields.gasFeeCap = 101 }},
		{name: "hotkey calldata", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.hotkey[1] = 1 }},
		{name: "commitment calldata", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.commitmentHash[1] = 1 }},
		{name: "block calldata", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.finalizedBlock++ }},
		{name: "block hash calldata", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.finalizedBlockHash[1] = 1 }},
		{name: "method", changeSigned: func(fields *fleetMirrorSignedTestFields) { fields.wrongMethod = true }},
		{name: "receipt status", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.status = ethTypes.ReceiptStatusFailed }},
		{name: "receipt transaction", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.transactionHash = common.BytesToHash([]byte{0x99}) }},
		{name: "event hotkey", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.eventHotkey[1] = 1 }},
		{name: "event commitment", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.eventCommitment[1] = 1 }},
		{name: "event block", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.eventBlock++ }},
		{name: "event block hash", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.eventBlockHash[1] = 1 }},
		{name: "missing event", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.eventCount = 0 }},
		{name: "duplicate event", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.eventCount = 2 }},
		{name: "malformed extra event", changeReceipt: func(fields *fleetMirrorReceiptTestFields) { fields.malformedExtraEvent = true }},
	}
	for _, mutation := range mutations {
		signedFields := fixture.signedFields()
		if mutation.changeSigned != nil {
			mutation.changeSigned(&signedFields)
		}
		signed := fixture.sign(t, signedFields)
		receiptFields := fixture.receiptFields(signed)
		if mutation.changeReceipt != nil {
			mutation.changeReceipt(&receiptFields)
		}
		receipt := fixture.receipt(t, receiptFields)
		if err := validateFinalizedFleetMirrorTransaction(fixture.cfg, fixture.plan, fixture.action, signed, receipt, fixture.coordinator, fixture.hotkey, fixture.commitmentHash, fixture.finalizedBlock, fixture.finalizedBlockHash); err == nil {
			t.Errorf("%s mutation was accepted", mutation.name)
		}
	}
}

// Require exact native finalized and verified journal evidence, including the
// same plan, intent, transaction, block, and block hash.
func TestFleetMirrorRecoveryRequiresExactVerifiedNativeEvidence(t *testing.T) {
	planHash := "0x" + strings.Repeat("11", 32)
	intentHash := "0x" + strings.Repeat("22", 32)
	transactionHash := "0x" + strings.Repeat("33", 32)
	blockHash := "0x" + strings.Repeat("44", 32)
	prior := &SetupPlan{
		PlanHash: planHash,
		Actions:  []Action{{ID: "fleet.commitment.4", Kind: "substrate-extrinsic", IntentHash: intentHash}},
	}
	evidence := FleetCommitmentEvidence{ExtrinsicHash: transactionHash, FinalizedBlock: 123, FinalizedBlockHash: blockHash}
	baseline := []JournalEntry{
		{PlanHash: planHash, ActionID: "fleet.commitment.4", IntentHash: intentHash, Stage: StageFinalized, TransactionHash: transactionHash, BlockNumber: 123, BlockHash: blockHash},
		{PlanHash: planHash, ActionID: "fleet.commitment.4", IntentHash: intentHash, Stage: StageVerified},
	}
	if !fleetCommitmentEvidenceWasVerified(prior, baseline, 4, &evidence) {
		t.Fatal("exact native commitment evidence was not recognized")
	}
	type mutation struct {
		name   string
		change func(*SetupPlan, *[]JournalEntry, *FleetCommitmentEvidence)
	}
	mutations := []mutation{
		{name: "missing verification", change: func(_ *SetupPlan, entries *[]JournalEntry, _ *FleetCommitmentEvidence) { *entries = (*entries)[:1] }},
		{name: "wrong verification intent", change: func(_ *SetupPlan, entries *[]JournalEntry, _ *FleetCommitmentEvidence) {
			(*entries)[1].IntentHash = "other"
		}},
		{name: "wrong transaction", change: func(_ *SetupPlan, _ *[]JournalEntry, evidence *FleetCommitmentEvidence) {
			evidence.ExtrinsicHash = "other"
		}},
		{name: "wrong block", change: func(_ *SetupPlan, _ *[]JournalEntry, evidence *FleetCommitmentEvidence) { evidence.FinalizedBlock++ }},
		{name: "wrong block hash", change: func(_ *SetupPlan, _ *[]JournalEntry, evidence *FleetCommitmentEvidence) {
			evidence.FinalizedBlockHash = "other"
		}},
		{name: "foreign plan", change: func(_ *SetupPlan, entries *[]JournalEntry, _ *FleetCommitmentEvidence) {
			(*entries)[0].PlanHash = "foreign"
		}},
		{name: "wrong planned intent", change: func(plan *SetupPlan, _ *[]JournalEntry, _ *FleetCommitmentEvidence) {
			plan.Actions[0].IntentHash = "other"
		}},
		{name: "wrong planned kind", change: func(plan *SetupPlan, _ *[]JournalEntry, _ *FleetCommitmentEvidence) { plan.Actions[0].Kind = "local" }},
		{name: "duplicate planned action", change: func(plan *SetupPlan, _ *[]JournalEntry, _ *FleetCommitmentEvidence) {
			plan.Actions = append(plan.Actions, plan.Actions[0])
		}},
	}
	for _, mutation := range mutations {
		plan := *prior
		plan.Actions = append([]Action(nil), prior.Actions...)
		entries := append([]JournalEntry(nil), baseline...)
		candidateEvidence := evidence
		mutation.change(&plan, &entries, &candidateEvidence)
		if fleetCommitmentEvidenceWasVerified(&plan, entries, 4, &candidateEvidence) {
			t.Errorf("%s mutation retained native verification", mutation.name)
		}
	}
}

// A later release may preserve the mirror call while rewiring the native
// install dependency. Recovery must authenticate the native evidence against
// the transaction's source plan, not the newest plan's different intent.
func TestFleetMirrorRecoveryUsesTransactionSourcePlanForNativeEvidence(t *testing.T) {
	sourceHash := "0x" + strings.Repeat("11", 32)
	sourceIntent := "0x" + strings.Repeat("22", 32)
	transactionHash := "0x" + strings.Repeat("33", 32)
	blockHash := "0x" + strings.Repeat("44", 32)
	source := &SetupPlan{
		PlanHash: sourceHash,
		Actions:  []Action{{ID: "fleet.commitment.4", Kind: "substrate-extrinsic", IntentHash: sourceIntent}},
	}
	evidence := FleetCommitmentEvidence{ExtrinsicHash: transactionHash, FinalizedBlock: 123, FinalizedBlockHash: blockHash}
	entries := []JournalEntry{
		{PlanHash: sourceHash, ActionID: "fleet.commitment.4", IntentHash: sourceIntent, Stage: StageFinalized, TransactionHash: transactionHash, BlockNumber: 123, BlockHash: blockHash},
		{PlanHash: sourceHash, ActionID: "fleet.commitment.4", IntentHash: sourceIntent, Stage: StageVerified},
	}
	current := &SetupPlan{
		PlanHash:        "0x" + strings.Repeat("55", 32),
		PriorPlanHashes: []string{sourceHash},
		Actions: []Action{{
			ID: "fleet.commitment.4", Kind: "substrate-extrinsic",
			IntentHash: "0x" + strings.Repeat("66", 32),
		}},
	}
	if !fleetCommitmentEvidenceWasVerified(source, entries, 4, &evidence) {
		t.Fatal("transaction source plan did not authenticate its exact native evidence")
	}
	if fleetCommitmentEvidenceWasVerified(current, entries, 4, &evidence) {
		t.Fatal("newest dependency-rewired native intent impersonated the transaction source plan")
	}
}

// Historical state is immutable recovery evidence. Current state must also be
// exact unless a separately authenticated descendant has already closed the
// old write before a later generation replaced it.
func TestFleetMirrorRecoveryRequiresExactHistoricalAndCurrentState(t *testing.T) {
	var commitmentHash, blockHash [32]byte
	commitmentHash[0], blockHash[0] = 1, 2
	exact := stabi.MirroredCommitmentsOutput{CommitmentHash: commitmentHash, FinalizedBlock: 123, FinalizedBlockHash: blockHash}
	if err := validateFleetMirrorRecoveryState(exact, exact, commitmentHash, 123, blockHash, true); err != nil {
		t.Fatalf("exact mirror state was rejected: %v", err)
	}
	for _, mutation := range []struct {
		name       string
		historical stabi.MirroredCommitmentsOutput
		current    stabi.MirroredCommitmentsOutput
	}{
		{name: "historical commitment", historical: stabi.MirroredCommitmentsOutput{FinalizedBlock: 123, FinalizedBlockHash: blockHash}, current: exact},
		{name: "historical block", historical: stabi.MirroredCommitmentsOutput{CommitmentHash: commitmentHash, FinalizedBlock: 124, FinalizedBlockHash: blockHash}, current: exact},
		{name: "historical block hash", historical: stabi.MirroredCommitmentsOutput{CommitmentHash: commitmentHash, FinalizedBlock: 123}, current: exact},
		{name: "current commitment", historical: exact, current: stabi.MirroredCommitmentsOutput{FinalizedBlock: 123, FinalizedBlockHash: blockHash}},
		{name: "current block", historical: exact, current: stabi.MirroredCommitmentsOutput{CommitmentHash: commitmentHash, FinalizedBlock: 124, FinalizedBlockHash: blockHash}},
		{name: "current block hash", historical: exact, current: stabi.MirroredCommitmentsOutput{CommitmentHash: commitmentHash, FinalizedBlock: 123}},
	} {
		if err := validateFleetMirrorRecoveryState(mutation.historical, mutation.current, commitmentHash, 123, blockHash, true); err == nil {
			t.Errorf("%s mutation was accepted", mutation.name)
		}
	}
	advanced := stabi.MirroredCommitmentsOutput{CommitmentHash: blockHash, FinalizedBlock: 456, FinalizedBlockHash: commitmentHash}
	if err := validateFleetMirrorRecoveryState(exact, advanced, commitmentHash, 123, blockHash, false); err != nil {
		t.Fatalf("authenticated descendant did not permit advanced current state: %v", err)
	}
	if err := validateFleetMirrorRecoveryState(advanced, exact, commitmentHash, 123, blockHash, false); err == nil {
		t.Fatal("authenticated descendant permitted inexact historical state")
	}
}

// Bind recovery to a canonical fleet action and require the fresh revision to
// preserve its complete action intent.
func TestFleetMirrorRecoveryRequiresCanonicalActionAndUnchangedRevision(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	action, fleet, err := exactFleetMirrorPlanAction(fixture.plan, fixture.action.ID, fixture.action.IntentHash, 200, 202)
	if err != nil || fleet != 4 || action.IntentHash != fixture.action.IntentHash {
		t.Fatalf("exact fleet-mirror plan action was rejected: fleet=%d action=%+v error=%v", fleet, action, err)
	}
	for _, actionID := range []string{"fleet.mirror.0", "fleet.mirror.203", "fleet.mirror.04", "fleet.bind.4", "fleet.mirror.-1"} {
		if _, err := fleetMirrorRecoveryIndex(actionID, 202); err == nil {
			t.Errorf("noncanonical fleet mirror %q was accepted", actionID)
		}
	}
	recovery := finalizedFleetMirrorRecovery{
		Transaction: planRevisionTransaction{
			PlanHash: fixture.plan.PlanHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash,
			TransactionHash: "0x" + strings.Repeat("61", 32), BlockNumber: 77, BlockHash: "0x" + strings.Repeat("62", 32),
		},
		Action: fixture.action, Fleet: 4, Hotkey: fixture.hotkey, CommitmentHash: fixture.commitmentHash,
		FinalizedBlock: fixture.finalizedBlock, FinalizedBlockHash: fixture.finalizedBlockHash,
	}
	revised := &SetupPlan{Actions: []Action{fixture.action}}
	if err := validateRevisedFleetMirrorRecoveries(revised, []finalizedFleetMirrorRecovery{recovery}); err != nil {
		t.Fatalf("unchanged revised fleet mirror was rejected: %v", err)
	}
	changed := fixture.action
	changed.Description += " changed"
	changed.IntentHash, err = actionIntentHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRevisedFleetMirrorRecoveries(&SetupPlan{Actions: []Action{changed}}, []finalizedFleetMirrorRecovery{recovery}); err == nil {
		t.Fatal("changed revised fleet-mirror semantics were accepted")
	}
	if err := validateRevisedFleetMirrorRecoveries(revised, []finalizedFleetMirrorRecovery{recovery, recovery}); err == nil {
		t.Fatal("duplicate fleet-mirror recovery was accepted")
	}
}

// A recovered ancestor transaction may cross into the atomic installer plan
// only after an exact descendant postcondition verified the legacy intent. A
// finalized-only receipt remains blocked by the unchanged-action boundary.
func TestFleetMirrorRecoveryAllowsOnlyVerifiedLegacyWriteReadProofTransition(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	recovery := finalizedFleetMirrorRecovery{
		Transaction: planRevisionTransaction{
			PlanHash: fixture.plan.PlanHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash,
			TransactionHash: "0x" + strings.Repeat("61", 32), BlockNumber: 77, BlockHash: "0x" + strings.Repeat("62", 32),
		},
		Action: fixture.action, Fleet: 4, Hotkey: fixture.hotkey, CommitmentHash: fixture.commitmentHash,
		FinalizedBlock: fixture.finalizedBlock, FinalizedBlockHash: fixture.finalizedBlockHash,
	}
	proof := fixture.action
	proof.Kind = "evm-read"
	proof.Parameters = cloneStrings(fixture.action.Parameters)
	delete(proof.Parameters, evmMaximumGasUnitsParameter)
	delete(proof.Parameters, evmMaximumFeePerGasParameter)
	proof.Parameters["batch_installed"] = "true"
	proof.Spend = Spend{}
	proof.DependsOn = []string{"fleet.install.batch.1"}
	var err error
	proof.IntentHash, err = actionIntentHash(proof)
	if err != nil {
		t.Fatal(err)
	}

	revised := &SetupPlan{Actions: []Action{proof}}
	verified := []JournalEntry{{
		PlanHash: fixture.plan.PlanHash, ActionID: fixture.action.ID,
		IntentHash: fixture.action.IntentHash, Stage: StageVerified,
	}}
	if err := preserveVerifiedEVMGasReallocations(t.TempDir(), revised, fixture.plan, verified); err != nil {
		t.Fatal(err)
	}
	if err := validateRevisedFleetMirrorRecoveries(revised, []finalizedFleetMirrorRecovery{recovery}); err != nil {
		t.Fatalf("verified legacy mirror was not carried before recovery validation: %v", err)
	}
	if revised.Actions[0].IntentHash != fixture.action.IntentHash || revised.MaximumSpend.EVMGasWei != fixture.action.Spend.EVMGasWei {
		t.Fatalf("verified legacy mirror spend was not retained: action=%+v maximum=%+v", revised.Actions[0], revised.MaximumSpend)
	}

	finalizedOnly := &SetupPlan{Actions: []Action{proof}}
	entries := []JournalEntry{{
		PlanHash: fixture.plan.PlanHash, ActionID: fixture.action.ID,
		IntentHash: fixture.action.IntentHash, Stage: StageFinalized,
	}}
	if err := preserveVerifiedEVMGasReallocations(t.TempDir(), finalizedOnly, fixture.plan, entries); err != nil {
		t.Fatal(err)
	}
	if err := validateRevisedFleetMirrorRecoveries(finalizedOnly, []finalizedFleetMirrorRecovery{recovery}); err == nil {
		t.Fatal("finalized-only legacy mirror crossed into a changed read-proof action")
	}
}

// Persist one exact descendant postcondition and prove it durably closes the
// ancestor transaction even if later native commitment state legitimately
// advances during conformance testing.
func TestFleetMirrorRecoveryRecognizesExactVerifiedDescendant(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	sourceHash := fixture.plan.PlanHash
	currentHash := "0x" + strings.Repeat("71", 32)
	fixture.plan.PlanHash = currentHash
	fixture.plan.PriorPlanHashes = []string{sourceHash}
	stateDir := t.TempDir()
	wantObserved := map[string]any{
		"fleet": 4, "commitment_hash": common.BytesToHash(fixture.commitmentHash[:]).Hex(), "finalized_block": fixture.finalizedBlock,
		"kind": fixture.action.Kind, "target": fixture.action.Target,
	}
	record := ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: fixture.cfg.Config.Deployment.DeploymentID,
		PlanHash: currentHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash,
		OperationalRPCMode: fixture.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(fixture.cfg),
		SubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("81", 32)},
		EVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("82", 32)}, EVMHashDomain: "evm-rpc",
		Observed:                      wantObserved,
		IndependentSubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("81", 32)},
		IndependentEVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("82", 32)}, IndependentEVMHashDomain: "evm-rpc",
		IndependentObserved: wantObserved,
	}
	path, err := postconditionRelativePath(currentHash, fixture.action.ID)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, filepath.FromSlash(path)), record); err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{
		Sequence: 11,
		PlanHash: currentHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash, Stage: StageVerified,
		PostconditionHash: hash, PostconditionPath: path,
	}
	transaction := planRevisionTransaction{PlanHash: sourceHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash, JournalSequence: 10, BlockNumber: 100}
	verified, err := verifiedFleetMirrorDescendant(fixture.cfg, stateDir, fixture.plan, []JournalEntry{entry}, transaction, fixture.action, 4, fixture.commitmentHash, fixture.finalizedBlock)
	if err != nil || !verified {
		t.Fatalf("exact verified descendant was not recognized: verified=%t error=%v", verified, err)
	}
}

// Reject a descendant marker whose durable postcondition was altered, whose
// hash does not authenticate it, or whose plan is outside the approved lineage.
func TestFleetMirrorRecoveryRejectsInexactVerifiedDescendant(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	sourceHash := fixture.plan.PlanHash
	currentHash := "0x" + strings.Repeat("71", 32)
	fixture.plan.PlanHash = currentHash
	fixture.plan.PriorPlanHashes = []string{sourceHash}
	stateDir := t.TempDir()
	wrongObserved := map[string]any{
		"fleet": 5, "commitment_hash": common.BytesToHash(fixture.commitmentHash[:]).Hex(), "finalized_block": fixture.finalizedBlock,
		"kind": fixture.action.Kind, "target": fixture.action.Target,
	}
	record := ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: fixture.cfg.Config.Deployment.DeploymentID,
		PlanHash: currentHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash,
		OperationalRPCMode: fixture.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(fixture.cfg),
		SubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("81", 32)},
		EVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("82", 32)}, EVMHashDomain: "evm-rpc",
		Observed:                      wrongObserved,
		IndependentSubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("81", 32)},
		IndependentEVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("82", 32)}, IndependentEVMHashDomain: "evm-rpc",
		IndependentObserved: wrongObserved,
	}
	path, err := postconditionRelativePath(currentHash, fixture.action.ID)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, filepath.FromSlash(path)), record); err != nil {
		t.Fatal(err)
	}
	transaction := planRevisionTransaction{PlanHash: sourceHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash, JournalSequence: 10, BlockNumber: 100}
	entry := JournalEntry{
		Sequence: 11,
		PlanHash: currentHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash, Stage: StageVerified,
		PostconditionHash: hash, PostconditionPath: path,
	}
	if verified, err := verifiedFleetMirrorDescendant(fixture.cfg, stateDir, fixture.plan, []JournalEntry{entry}, transaction, fixture.action, 4, fixture.commitmentHash, fixture.finalizedBlock); err == nil || verified {
		t.Fatalf("wrong observed descendant was accepted: verified=%t error=%v", verified, err)
	}
	entry.PostconditionHash = "0x" + strings.Repeat("91", 32)
	if verified, err := verifiedFleetMirrorDescendant(fixture.cfg, stateDir, fixture.plan, []JournalEntry{entry}, transaction, fixture.action, 4, fixture.commitmentHash, fixture.finalizedBlock); err == nil || verified {
		t.Fatalf("wrong descendant receipt hash was accepted: verified=%t error=%v", verified, err)
	}
	entry.PlanHash = "0x" + strings.Repeat("92", 32)
	if verified, err := verifiedFleetMirrorDescendant(fixture.cfg, stateDir, fixture.plan, []JournalEntry{entry}, transaction, fixture.action, 4, fixture.commitmentHash, fixture.finalizedBlock); err != nil || verified {
		t.Fatalf("foreign descendant marker was not ignored: verified=%t error=%v", verified, err)
	}
}

// A same-intent receipt is a descendant only when both its append-only journal
// order and its independently observed finalized checkpoint follow the old
// transaction. Earlier observations cannot authorize current-state drift.
func TestFleetMirrorRecoveryDescendantMustFollowRecoveredTransaction(t *testing.T) {
	fixture := newFleetMirrorRecoveryTestFixture(t)
	sourceHash := fixture.plan.PlanHash
	currentHash := "0x" + strings.Repeat("71", 32)
	fixture.plan.PlanHash = currentHash
	fixture.plan.PriorPlanHashes = []string{sourceHash}
	stateDir := t.TempDir()
	wantObserved := map[string]any{
		"fleet": 4, "commitment_hash": common.BytesToHash(fixture.commitmentHash[:]).Hex(), "finalized_block": fixture.finalizedBlock,
		"kind": fixture.action.Kind, "target": fixture.action.Target,
	}
	record := ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: fixture.cfg.Config.Deployment.DeploymentID,
		PlanHash: currentHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash,
		OperationalRPCMode: fixture.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(fixture.cfg),
		SubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("81", 32)},
		EVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("82", 32)}, EVMHashDomain: "evm-rpc",
		Observed:                      wantObserved,
		IndependentSubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("81", 32)},
		IndependentEVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("82", 32)}, IndependentEVMHashDomain: "evm-rpc",
		IndependentObserved: wantObserved,
	}
	path, err := postconditionRelativePath(currentHash, fixture.action.ID)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, filepath.FromSlash(path)), record); err != nil {
		t.Fatal(err)
	}
	transaction := planRevisionTransaction{
		PlanHash: sourceHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash,
		JournalSequence: 10, BlockNumber: 100,
	}
	entry := JournalEntry{
		Sequence: 10, PlanHash: currentHash, ActionID: fixture.action.ID, IntentHash: fixture.action.IntentHash,
		Stage: StageVerified, PostconditionHash: hash, PostconditionPath: path,
	}
	if verified, err := verifiedFleetMirrorDescendant(fixture.cfg, stateDir, fixture.plan, []JournalEntry{entry}, transaction, fixture.action, 4, fixture.commitmentHash, fixture.finalizedBlock); err != nil || verified {
		t.Fatalf("earlier journal observation was not ignored: verified=%t error=%v", verified, err)
	}
	entry.Sequence = 11
	transaction.BlockNumber = 101
	if verified, err := verifiedFleetMirrorDescendant(fixture.cfg, stateDir, fixture.plan, []JournalEntry{entry}, transaction, fixture.action, 4, fixture.commitmentHash, fixture.finalizedBlock); err == nil || verified {
		t.Fatalf("pre-transaction finalized checkpoint was accepted: verified=%t error=%v", verified, err)
	}
}
