package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/ss58"
	"github.com/urnetwork/connect"
)

// Exact journal identity and checkpoints for one fixture action.
type finalSemanticFixtureJournalBinding struct {
	planHash        string
	intentHash      string
	transactionHash string
	block           uint64
	blockHash       string
	recovery        uint64
	recoveryHash    string
}

// Ancestor action evidence accepted by a revised fixture plan.
type finalSemanticFixtureCarriedAction struct {
	postcondition *ActionPostcondition
	receipt       FinalNativeReceipt
}

// Resolve whether a fixture action was executed under the current plan or is
// carried from an authenticated ancestor. Current actions need an explicit
// synthetic block; carried actions instead retain the exact prior-plan receipt
// identity accepted by the revised action.
func resolveFinalSemanticFixtureJournalBinding(plan *SetupPlan, action Action, block, recovery uint64, carried *finalSemanticFixtureCarriedAction) (finalSemanticFixtureJournalBinding, error) {
	if plan == nil || plan.PlanHash == "" || action.ID == "" || action.IntentHash == "" {
		return finalSemanticFixtureJournalBinding{}, fmt.Errorf("fixture journal action identity is incomplete")
	}
	found := false
	for _, planned := range plan.Actions {
		if planned.ID != action.ID {
			continue
		}
		if found || planned.IntentHash != action.IntentHash {
			return finalSemanticFixtureJournalBinding{}, fmt.Errorf("fixture plan action %s is duplicated or differs", action.ID)
		}
		found = true
	}
	if !found {
		return finalSemanticFixtureJournalBinding{}, fmt.Errorf("fixture plan lacks action %s", action.ID)
	}
	if carried == nil {
		if block == 0 || recovery == 0 || recovery >= block || len(action.AcceptedPriorIntentHashes) != 0 {
			return finalSemanticFixtureJournalBinding{}, fmt.Errorf("current fixture action %s has no exact block or is marked carried", action.ID)
		}
		return finalSemanticFixtureJournalBinding{
			planHash: plan.PlanHash, intentHash: action.IntentHash,
			block: block, blockHash: finalTestHex(byte(block)), recovery: recovery, recoveryHash: finalTestHex(byte(recovery)),
		}, nil
	}
	postcondition := carried.postcondition
	receipt := carried.receipt
	if block != 0 || recovery != 0 || postcondition == nil || postcondition.ActionID != action.ID || postcondition.PlanHash == plan.PlanHash || !plan.allowedPlanHashes()[postcondition.PlanHash] || len(action.AcceptedPriorIntentHashes) != 1 || action.AcceptedPriorIntentHashes[0] != postcondition.IntentHash || action.IntentHash == postcondition.IntentHash || receipt.ExtrinsicHash == "" || receipt.Block.Number < 2 || receipt.Block.Hash == "" || postcondition.SubstrateFinalized.Number < receipt.Block.Number {
		return finalSemanticFixtureJournalBinding{}, fmt.Errorf("carried fixture action %s has invalid prior-plan lineage", action.ID)
	}
	return finalSemanticFixtureJournalBinding{
		planHash: postcondition.PlanHash, intentHash: postcondition.IntentHash, transactionHash: receipt.ExtrinsicHash,
		block: receipt.Block.Number, blockHash: receipt.Block.Hash, recovery: receipt.Block.Number - 1, recoveryHash: finalTestHex(byte(receipt.Block.Number - 1)),
	}, nil
}

// Proves that verifier selection does not decide synthetic journal coverage:
// both current selected/rejected registrations use the current plan, while a
// challenger registration already proved by an ancestor retains that identity.
func TestFinalSemanticFixtureJournalBindingCoversSelectedRejectedAndCarriedRegistrations(t *testing.T) {
	priorPlanHash := finalTestHex(0x21)
	currentPlanHash := finalTestHex(0x22)
	priorIntentHash := finalTestHex(0x23)
	makeAction := func(id string, accepted ...string) Action {
		action := Action{ID: id, Kind: "substrate-extrinsic", Target: id, AcceptedPriorIntentHashes: accepted}
		intentHash, err := actionIntentHash(action)
		if err != nil {
			t.Fatal(err)
		}
		action.IntentHash = intentHash
		return action
	}
	selected := makeAction("fixture.selected.register")
	rejected := makeAction("fixture.rejected.register")
	carriedAction := makeAction("fleet.register.201", priorIntentHash)
	plan := &SetupPlan{PlanHash: currentPlanHash, PriorPlanHashes: []string{priorPlanHash}, Actions: []Action{selected, rejected, carriedAction}}

	for _, test := range []struct {
		name     string
		selected bool
		action   Action
		block    uint64
	}{
		{name: "selected current registration", selected: true, action: selected, block: 150},
		{name: "rejected current registration", selected: false, action: rejected, block: 160},
	} {
		binding, err := resolveFinalSemanticFixtureJournalBinding(plan, test.action, test.block, test.block-10, nil)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if binding.planHash != currentPlanHash || binding.intentHash != test.action.IntentHash || binding.block != test.block {
			t.Fatalf("%s selected=%t current binding=%+v", test.name, test.selected, binding)
		}
	}

	postcondition := &ActionPostcondition{
		PlanHash: priorPlanHash, ActionID: carriedAction.ID, IntentHash: priorIntentHash,
		SubstrateFinalized: ChainHead{Number: 90, Hash: finalTestHex(90)},
	}
	receipt := FinalNativeReceipt{ExtrinsicHash: finalTestHex(0x24), Block: ChainHead{Number: 80, Hash: finalTestHex(80)}}
	carried := &finalSemanticFixtureCarriedAction{postcondition: postcondition, receipt: receipt}
	binding, err := resolveFinalSemanticFixtureJournalBinding(plan, carriedAction, 0, 0, carried)
	if err != nil {
		t.Fatal(err)
	}
	if binding.planHash != priorPlanHash || binding.intentHash != priorIntentHash || binding.transactionHash != receipt.ExtrinsicHash || binding.block != receipt.Block.Number || binding.blockHash != receipt.Block.Hash {
		t.Fatalf("carried binding=%+v", binding)
	}
	if _, err := resolveFinalSemanticFixtureJournalBinding(plan, selected, 0, 0, nil); err == nil {
		t.Fatal("unmapped current registration was accepted")
	}
	if _, err := resolveFinalSemanticFixtureJournalBinding(plan, carriedAction, 80, 70, carried); err == nil {
		t.Fatal("carried registration with a current-plan block was accepted")
	}
	wrongPrior := *postcondition
	wrongPrior.IntentHash = finalTestHex(0x25)
	if _, err := resolveFinalSemanticFixtureJournalBinding(plan, carriedAction, 0, 0, &finalSemanticFixtureCarriedAction{postcondition: &wrongPrior, receipt: receipt}); err == nil {
		t.Fatal("carried registration with unaccepted prior intent was accepted")
	}
}

// attachFinalFleetLifecycleFixture adds a complete, independently verifiable
// ordered lifecycle graph to the launch-scale semantic fixture. Keeping this
// construction adjacent to the lifecycle verifier makes every plan, journal,
// signature, historical UID, and applied-vector substitution testable without
// accepting a summary-only fixture.
func attachFinalFleetLifecycleFixture(t *testing.T, source *FinalSemanticEvidence, artifacts map[string][]byte) {
	t.Helper()
	if source == nil {
		t.Fatal("nil semantic fixture")
	}
	addArtifact := func(kind, name string, data []byte) FinalArtifactLocator {
		uri := "artifacts/" + name
		artifacts[uri] = append([]byte(nil), data...)
		return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	}

	cfg := testResolvedConfig(t)
	cfg.Config.Deployment.DeploymentID = source.DeploymentID
	cfg.Netuid = source.Netuid
	cfg.ChainID = source.ChainID
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var miners []FinalMinerProcessEvidence
	if err := json.Unmarshal(artifacts[source.Topology.MinerManifest.URI], &miners); err != nil {
		t.Fatal(err)
	}
	for index := range miners {
		role := roles.Clients[fmt.Sprintf("miner-%d", index+1)]
		clientID, parseErr := connect.ParseId(miners[index].ClientID)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		role.ClientIDHex = hex.EncodeToString(clientID[:])
		roles.Clients[role.Label] = role
	}
	identities := finalPublicIdentities{
		Schema: "urnetwork-sim-public-identities-v1", DeploymentID: source.DeploymentID,
		Substrate: map[string]finalPublicIdentity{}, EVM: map[string]string{}, Clients: map[string]finalPublicClientIdentity{},
	}
	for label, role := range roles.Substrate {
		identities.Substrate[label] = finalPublicIdentity{PublicKey: "0x" + role.PublicKeyHex, SS58: role.SS58}
	}
	for label, role := range roles.EVM {
		identities.EVM[label] = strings.ToLower(role.Address)
	}
	for label, role := range roles.Clients {
		if role.ClientIDHex != "" {
			identities.Clients[label] = finalPublicClientIdentity{ClientID: "0x" + role.ClientIDHex, ClientKey: "0x" + role.PublicKeyHex}
		}
	}

	coordinator := common.HexToAddress(source.Deployment.CoordinatorProxy)
	manifestByVariant := map[string]protocol.FleetManifest{}
	manifestBytesByVariant := map[string][]byte{}
	commitmentHashByVariant := map[string][32]byte{}
	for _, name := range finalFleetLifecycleVariantNames() {
		variant, variantErr := fleetLifecycleVariantFor(name)
		if variantErr != nil {
			t.Fatal(variantErr)
		}
		var fleetID [32]byte
		if variant.Fallback {
			fleetID = derive32(cfg, "fleet-lifecycle/fallback-id")
		} else {
			fleetID = derive32(cfg, fmt.Sprintf("fleet-id/%d", variant.Fleet))
		}
		hotkey, roleErr := roleBytes32(roles, variant.HotkeyLabel)
		if roleErr != nil {
			t.Fatal(roleErr)
		}
		manifest := protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: source.ChainID, Netuid: source.Netuid, FleetID: fleetID, Hotkey: hotkey, Generation: variant.Generation}
		copy(manifest.Coordinator[:], coordinator.Bytes())
		for member := 1; member <= 4; member++ {
			miner := fleetMemberMinerIndex(cfg, variant.Fleet, member)
			if variant.Fallback {
				miner, err = fleetLifecycleFallbackMinerIndex(cfg, member)
				if err != nil {
					t.Fatal(err)
				}
			}
			client := roles.Clients[fmt.Sprintf("miner-%d", miner)]
			idRaw, idErr := hex.DecodeString(client.ClientIDHex)
			keyRaw, keyErr := hex.DecodeString(client.PublicKeyHex)
			if idErr != nil || keyErr != nil || len(idRaw) != 16 || len(keyRaw) != 32 {
				t.Fatalf("invalid lifecycle member %d/%d", variant.Fleet, member)
			}
			var value protocol.FleetMember
			copy(value.ClientID[:], idRaw)
			copy(value.ClientKey[:], keyRaw)
			manifest.Members = append(manifest.Members, value)
		}
		canonical, canonicalErr := manifest.Canonical()
		if canonicalErr != nil {
			t.Fatal(canonicalErr)
		}
		hash, hashErr := manifest.CommitmentHash()
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		manifestByVariant[name] = manifest
		manifestBytesByVariant[name] = canonical
		commitmentHashByVariant[name] = hash
	}

	actions := make([]Action, 0, 64)
	carriedActions := make(map[string]finalSemanticFixtureCarriedAction, len(source.HeadTransitions))
	addAction := func(action Action) {
		hash, hashErr := actionIntentHash(action)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		action.IntentHash = hash
		actions = append(actions, action)
	}
	for _, name := range finalFleetLifecycleVariantNames() {
		variant, _ := fleetLifecycleVariantFor(name)
		commitmentID, _ := fleetLifecycleCommitmentActionID(name)
		addAction(Action{ID: commitmentID, Kind: "substrate-extrinsic", Target: variant.HotkeyLabel, Description: name + " commitment", Parameters: map[string]string{"generation": fmt.Sprint(variant.Generation)}})
		mirrorID, _ := fleetLifecycleMirrorActionID(name)
		addAction(Action{ID: mirrorID, Kind: "evm-transaction", Target: coordinator.Hex(), Description: name + " mirror"})
		for member := 1; member <= 4; member++ {
			bindingID, _ := fleetLifecycleBindingActionID(name, member)
			miner := fleetMemberMinerIndex(cfg, variant.Fleet, member)
			if variant.Fallback {
				miner, err = fleetLifecycleFallbackMinerIndex(cfg, member)
				if err != nil {
					t.Fatal(err)
				}
			}
			addAction(Action{ID: bindingID, Kind: "evm-transaction", Target: fmt.Sprintf("miner:%d", miner), Description: name + " binding"})
		}
	}
	for _, name := range []string{fleetLifecycleVariantFallback, fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal} {
		expected, expectationErr := fleetLifecycleRegistrationExpectationFor(name)
		if expectationErr != nil {
			t.Fatal(expectationErr)
		}
		actionID, _ := fleetLifecycleRegistrationActionIDFor(name)
		addAction(Action{ID: actionID, Kind: "substrate-extrinsic", Target: expected.replacementHotkeyLabel, Description: name + " registration", Parameters: map[string]string{
			"expected_pruned_fleet": fmt.Sprint(expected.victimFleet), "expected_pruned_hotkey": expected.victimHotkeyLabel,
			"expected_pruned_uid": fmt.Sprint(expected.expectedUID), "expected_replacement_hotkey": expected.replacementHotkeyLabel,
		}})
	}
	for _, name := range []string{fleetLifecycleVariantTargetTakeover, fleetLifecycleVariantCompanionTakeover, fleetLifecycleVariantFallback} {
		variant, _ := fleetLifecycleVariantFor(name)
		for member := 1; member <= 4; member++ {
			cleanupID, _ := fleetLifecycleCleanupActionID(name, member)
			miner := fleetMemberMinerIndex(cfg, variant.Fleet, member)
			if variant.Fallback {
				miner, err = fleetLifecycleFallbackMinerIndex(cfg, member)
				if err != nil {
					t.Fatal(err)
				}
			}
			addAction(Action{ID: cleanupID, Kind: "evm-transaction", Target: fmt.Sprintf("miner:%d", miner), Description: name + " cleanup"})
		}
	}
	for _, transition := range source.HeadTransitions {
		var artifact finalHeadTournamentTransitionArtifact
		if err := json.Unmarshal(artifacts[transition.Artifact.URI], &artifact); err != nil || artifact.Postcondition == nil {
			t.Fatalf("decode carried tournament postcondition: %v", err)
		}
		actionID := fmt.Sprintf("fleet.register.%d", transition.ChallengerFleetID)
		if _, exists := carriedActions[actionID]; exists {
			t.Fatalf("duplicate carried tournament action %s", actionID)
		}
		carriedActions[actionID] = finalSemanticFixtureCarriedAction{postcondition: artifact.Postcondition, receipt: transition.Registration}
		addAction(Action{
			ID:                        actionID,
			Kind:                      "substrate-extrinsic",
			Target:                    fmt.Sprintf("fleet-%d-hotkey", transition.ChallengerFleetID),
			Description:               "head tournament challenger registration",
			AcceptedPriorIntentHashes: []string{artifact.Postcondition.IntentHash},
		})
	}
	plan := SetupPlan{
		Schema: currentSetupPlanSchema, Release: "1.0", DeploymentID: source.DeploymentID,
		ChainID: source.ChainID, GenesisHash: source.GenesisHash, Netuid: source.Netuid,
		PriorPlanHashes: []string{finalTestHex(2)}, ConfigHash: source.ConfigHash, PolicyHash: source.PolicyHash, Actions: actions,
		LiveFacts: SetupFacts{FinalizedBlock: 1, FinalizedBlockHash: finalTestHex(1)},
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if persisted, hashErr := persistedSetupPlanHash(planBytes, plan.Schema); hashErr != nil || persisted != plan.PlanHash {
		t.Fatalf("lifecycle fixture plan hash=%q: %v", persisted, hashErr)
	}
	source.PlanHash = plan.PlanHash
	planURI := "artifacts/setup-plan.json"
	artifacts[planURI] = append([]byte(nil), planBytes...)
	source.PlanArtifact = FinalArtifactLocator{Kind: "setup-plan", URI: planURI, ContentHash: bytesSHA256(planBytes), SizeBytes: uint64(len(planBytes))}

	actionByID := make(map[string]Action, len(actions))
	for _, action := range actions {
		actionByID[action.ID] = action
	}
	blockForVariant := map[string]struct{ commitment, mirror, binding uint64 }{
		fleetLifecycleVariantTargetTakeover:    {20, 30, 40},
		fleetLifecycleVariantCompanionTakeover: {21, 31, 45},
		fleetLifecycleVariantFallback:          {160, 170, 180},
		fleetLifecycleVariantProvider:          {760, 770, 780},
		fleetLifecycleVariantTerminal:          {1410, 1420, 1430},
	}
	registrationBlock := map[string]uint64{fleetLifecycleVariantFallback: 150, fleetLifecycleVariantProvider: 750, fleetLifecycleVariantTerminal: 1400}
	cleanupBlock := map[string]uint64{fleetLifecycleVariantTargetTakeover: 710, fleetLifecycleVariantCompanionTakeover: 1310, fleetLifecycleVariantFallback: 1320}
	type journalBlock struct {
		block, recovery uint64
	}
	currentActionBlocks := make(map[string]journalBlock, len(actions)-len(carriedActions))
	bindCurrentAction := func(actionID string, block, recovery uint64) {
		if _, exists := currentActionBlocks[actionID]; exists {
			t.Fatalf("duplicate fixture block for action %s", actionID)
		}
		currentActionBlocks[actionID] = journalBlock{block: block, recovery: recovery}
	}
	for name, blocks := range blockForVariant {
		commitmentID, _ := fleetLifecycleCommitmentActionID(name)
		bindCurrentAction(commitmentID, blocks.commitment, 1)
		mirrorID, _ := fleetLifecycleMirrorActionID(name)
		bindCurrentAction(mirrorID, blocks.mirror, 1)
		for member := 1; member <= 4; member++ {
			bindingID, _ := fleetLifecycleBindingActionID(name, member)
			bindCurrentAction(bindingID, blocks.binding+uint64(member-1), 1)
		}
	}
	for name, block := range registrationBlock {
		actionID, _ := fleetLifecycleRegistrationActionIDFor(name)
		bindCurrentAction(actionID, block, block-10)
	}
	for name, block := range cleanupBlock {
		for member := 1; member <= 4; member++ {
			actionID, _ := fleetLifecycleCleanupActionID(name, member)
			bindCurrentAction(actionID, block+uint64(member-1), block-1)
		}
	}

	journalEntries := make([]JournalEntry, 0, len(actions))
	appendJournal := func(action Action, binding finalSemanticFixtureJournalBinding) {
		transactionHash := binding.transactionHash
		if transactionHash == "" {
			transactionHash = finalTestHex(byte(60 + len(journalEntries)))
		}
		entry := JournalEntry{
			Schema: "urnetwork-sim-journal-v1", Sequence: uint64(len(journalEntries) + 1), Time: time.Unix(1_700_000_000+int64(len(journalEntries)), 0).UTC().Format(time.RFC3339Nano),
			DeploymentID: source.DeploymentID, PlanHash: binding.planHash, ActionID: action.ID, IntentHash: binding.intentHash, Stage: StageFinalized,
			TransactionHash: transactionHash, BlockNumber: binding.block, BlockHash: binding.blockHash,
			RecoveryBlock: binding.recovery, RecoveryBlockHash: binding.recoveryHash,
		}
		if len(journalEntries) != 0 {
			entry.PreviousHash = journalEntries[len(journalEntries)-1].EntryHash
		}
		entry.EntryHash = ""
		hash, hashErr := canonicalHashHex(entry)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		entry.EntryHash = hash
		journalEntries = append(journalEntries, entry)
	}
	for _, action := range actions {
		current := currentActionBlocks[action.ID]
		var carried *finalSemanticFixtureCarriedAction
		if value, exists := carriedActions[action.ID]; exists {
			copy := value
			carried = &copy
		}
		binding, bindingErr := resolveFinalSemanticFixtureJournalBinding(&plan, action, current.block, current.recovery, carried)
		if bindingErr != nil {
			t.Fatal(bindingErr)
		}
		appendJournal(action, binding)
	}
	journalByAction := make(map[string]JournalEntry, len(journalEntries))
	var journalBytes []byte
	for _, entry := range journalEntries {
		journalByAction[entry.ActionID] = entry
		line, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		journalBytes = append(journalBytes, line...)
		journalBytes = append(journalBytes, '\n')
	}
	if _, err := decodeFinalSemanticJournalBytes(journalBytes); err != nil {
		t.Fatalf("lifecycle fixture journal: %v", err)
	}

	roleValue := func(label string) (string, string) {
		hot, hotErr := roleBytes32(roles, label)
		coldLabel := strings.Replace(label, "-hotkey", "-coldkey", 1)
		cold, coldErr := roleBytes32(roles, coldLabel)
		if hotErr != nil || coldErr != nil {
			t.Fatal(hotErr, coldErr)
		}
		return fleetLifecycleHex(hot), fleetLifecycleHex(cold)
	}
	pruneSnapshot := func(head uint64, identitiesByUID map[uint16][2]string, victim uint16) FleetLifecyclePruneSnapshot {
		inputs := make([]FleetLifecyclePruneInput, 10)
		for uid := range inputs {
			hot := derive32(cfg, fmt.Sprintf("fixture-prune-hotkey/%d/%d", head, uid))
			cold := derive32(cfg, fmt.Sprintf("fixture-prune-coldkey/%d/%d", head, uid))
			inputs[uid] = FleetLifecyclePruneInput{UID: uint16(uid), Hotkey: fleetLifecycleHex(hot), Coldkey: fleetLifecycleHex(cold), EmissionRao: 100, RegistrationBlock: uint64(100 + uid), Immune: true}
		}
		inputs[0].Immortal = true
		for uid, identity := range identitiesByUID {
			inputs[uid].Hotkey, inputs[uid].Coldkey = identity[0], identity[1]
		}
		inputs[victim].EmissionRao = 0
		inputs[victim].RegistrationBlock = 1
		return FleetLifecyclePruneSnapshot{Head: ChainHead{Number: head, Hash: finalTestHex(byte(head))}, UIDCount: 10, MaximumUIDs: 10, ImmunityPeriodBlocks: 1_000, MinimumNonImmuneUIDs: 10, RuntimePruneUID: victim, Inputs: inputs}
	}
	c6Hot, c6Cold := roleValue(churnHotkeyLabel(fleetLifecycleTargetChurn))
	c7Hot, c7Cold := roleValue(churnHotkeyLabel(fleetLifecycleCompanionChurn))
	c8Hot, c8Cold := roleValue(churnHotkeyLabel(fleetLifecycleTerminalVictimChurn))
	c1Hot, c1Cold := roleValue(churnHotkeyLabel(fleetLifecycleFallbackChurn))
	launch := pruneSnapshot(90, map[uint16][2]string{7: {c6Hot, c6Cold}, 8: {c7Hot, c7Cold}, 9: {c8Hot, c8Cold}}, 7)
	registrationByVariant := map[string]*FleetLifecycleRegistrationEvidence{}
	for _, name := range []string{fleetLifecycleVariantFallback, fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal} {
		expected, _ := fleetLifecycleRegistrationExpectationFor(name)
		block := registrationBlock[name]
		identitiesAtPre := map[uint16][2]string{}
		switch name {
		case fleetLifecycleVariantFallback:
			identitiesAtPre = map[uint16][2]string{7: {c6Hot, c6Cold}, 8: {c7Hot, c7Cold}, 9: {c8Hot, c8Cold}}
		case fleetLifecycleVariantProvider:
			identitiesAtPre = map[uint16][2]string{7: {c1Hot, c1Cold}, 8: {c7Hot, c7Cold}, 9: {c8Hot, c8Cold}}
		case fleetLifecycleVariantTerminal:
			identitiesAtPre = map[uint16][2]string{7: {c1Hot, c1Cold}, 8: {c6Hot, c6Cold}, 9: {c8Hot, c8Cold}}
		}
		pre := pruneSnapshot(block-10, identitiesAtPre, uint16(expected.expectedUID))
		post := pre
		post.Head = ChainHead{Number: block, Hash: finalTestHex(byte(block))}
		post.Inputs = append([]FleetLifecyclePruneInput(nil), pre.Inputs...)
		replacementHot, replacementCold := roleValue(expected.replacementHotkeyLabel)
		post.Inputs[expected.expectedUID] = FleetLifecyclePruneInput{UID: uint16(expected.expectedUID), Hotkey: replacementHot, Coldkey: replacementCold, RegistrationBlock: block, Immune: true}
		post.RuntimePruneUID = 0
		victimHot, victimCold := roleValue(expected.victimHotkeyLabel)
		actionID, _ := fleetLifecycleRegistrationActionIDFor(name)
		action := actionByID[actionID]
		entry := journalByAction[actionID]
		registration := &FleetLifecycleRegistrationEvidence{
			Schema: "urnetwork-sim-fleet-registration-replacement-v1", DeploymentID: source.DeploymentID, PlanHash: plan.PlanHash, ActionID: actionID, IntentHash: action.IntentHash,
			VictimFleet: expected.victimFleet, VictimRole: expected.victimHotkeyLabel, VictimUID: uint16(expected.expectedUID), VictimHotkey: victimHot, VictimColdkey: victimCold,
			ReplacementHotkey: replacementHot, ReplacementColdkey: replacementCold, PrePrune: pre, PostRegistration: post,
			TransactionHash: entry.TransactionHash, BlockNumber: block, BlockHash: entry.BlockHash,
		}
		registrationByVariant[name] = registration
	}

	files := map[string][]byte{}
	files["launch-foundation/plan.json"] = planBytes
	files["launch-foundation/journal.jsonl"] = journalBytes
	files["public/identities.json"], err = json.Marshal(identities)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range finalFleetLifecycleVariantNames() {
		variant, _ := fleetLifecycleVariantFor(name)
		manifest, hash := manifestByVariant[name], commitmentHashByVariant[name]
		ids := blockForVariant[name]
		commitmentActionID, _ := fleetLifecycleCommitmentActionID(name)
		commitmentAction := actionByID[commitmentActionID]
		commitmentEntry := journalByAction[commitmentActionID]
		commitment := FleetCommitmentEvidence{
			Schema: fleetCommitmentEvidenceSchemaV2, DeploymentID: source.DeploymentID, PlanHash: plan.PlanHash, ActionID: commitmentActionID, IntentHash: commitmentAction.IntentHash,
			ManifestURI: variant.ManifestName, CommitmentHash: fleetLifecycleHex(hash), Hotkey: fleetLifecycleHex(manifest.Hotkey),
			ExtrinsicHash: commitmentEntry.TransactionHash, CommitmentBlock: ids.commitment, FinalizedBlock: ids.commitment, FinalizedBlockHash: commitmentEntry.BlockHash,
		}
		mirrorActionID, _ := fleetLifecycleMirrorActionID(name)
		mirrorAction := actionByID[mirrorActionID]
		mirrorEntry := journalByAction[mirrorActionID]
		mirror := FleetLifecycleMirrorEvidence{
			Schema: "urnetwork-sim-fleet-commitment-mirror-v1", DeploymentID: source.DeploymentID, PlanHash: plan.PlanHash, ActionID: mirrorActionID, IntentHash: mirrorAction.IntentHash,
			Hotkey: commitment.Hotkey, CommitmentHash: commitment.CommitmentHash, FinalizedBlock: commitment.FinalizedBlock, FinalizedBlockHash: commitment.FinalizedBlockHash,
			TransactionHash: mirrorEntry.TransactionHash, BlockNumber: mirrorEntry.BlockNumber, BlockHash: mirrorEntry.BlockHash,
		}
		files["public/"+variant.ManifestName] = manifestBytesByVariant[name]
		files["public/"+variant.CommitmentName], err = json.Marshal(commitment)
		if err != nil {
			t.Fatal(err)
		}
		files["public/"+fleetLifecycleMirrorEvidenceName(name)], err = json.Marshal(mirror)
		if err != nil {
			t.Fatal(err)
		}
		validFrom := uint64(10)
		switch name {
		case fleetLifecycleVariantFallback:
			validFrom = 11
		case fleetLifecycleVariantProvider:
			validFrom = 13
		case fleetLifecycleVariantTerminal:
			validFrom = 15
		}
		hotkeyRole := roles.Substrate[variant.HotkeyLabel]
		hotkeySeed, seedErr := hex.DecodeString(hotkeyRole.SeedHex)
		if seedErr != nil || len(hotkeySeed) != 32 {
			t.Fatal(seedErr)
		}
		var seed [32]byte
		copy(seed[:], hotkeySeed)
		hotkeyPair, pairErr := crv4.KeypairFromSeed(seed)
		if pairErr != nil {
			t.Fatal(pairErr)
		}
		for memberIndex, member := range manifest.Members {
			memberNo := memberIndex + 1
			binding, bindingErr := manifest.Binding(member, validFrom, 20)
			if bindingErr != nil {
				t.Fatal(bindingErr)
			}
			miner := fleetMemberMinerIndex(cfg, variant.Fleet, memberNo)
			if variant.Fallback {
				miner, err = fleetLifecycleFallbackMinerIndex(cfg, memberNo)
				if err != nil {
					t.Fatal(err)
				}
			}
			client := roles.Clients[fmt.Sprintf("miner-%d", miner)]
			clientSeed, seedErr := hex.DecodeString(client.SeedHex)
			if seedErr != nil {
				t.Fatal(seedErr)
			}
			clientPrivate := ed25519.NewKeyFromSeed(clientSeed)
			clientSignature, signErr := binding.SignClient(clientPrivate)
			if signErr != nil {
				t.Fatal(signErr)
			}
			digest, digestErr := binding.Digest()
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			hotkeySignature, signErr := hotkeyPair.Sign(digest[:])
			if signErr != nil {
				t.Fatal(signErr)
			}
			actionID, _ := fleetLifecycleBindingActionID(name, memberNo)
			action := actionByID[actionID]
			entry := journalByAction[actionID]
			uid, _ := finalFleetLifecycleVariantUID(name)
			evidence := FleetBindingEvidence{
				Schema: "urnetwork-fleet-binding-evidence-v1", DeploymentID: source.DeploymentID, PlanHash: plan.PlanHash, ActionID: actionID, IntentHash: action.IntentHash,
				ClientID: fleetLifecycleHex16(binding.ClientID), ClientKey: fleetLifecycleHex(binding.ClientKey), FleetID: fleetLifecycleHex(binding.FleetID), Hotkey: fleetLifecycleHex(binding.Hotkey),
				Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: fleetLifecycleHex(binding.CommitmentHash),
				BindingDigest: fleetLifecycleHex(digest), ClientSignature: "0x" + hex.EncodeToString(clientSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature),
				TransactionHash: entry.TransactionHash, BlockNumber: entry.BlockNumber, BlockHash: entry.BlockHash, UID: uid,
			}
			files["public/"+variant.BindingName(memberNo)], err = json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	for name, registration := range registrationByVariant {
		preName, registrationName := fleetLifecycleRegistrationNames(name)
		pre := FinalFleetLifecycleRegistrationPreparation{Schema: "urnetwork-sim-fleet-registration-pre-v1", PlanHash: plan.PlanHash, ActionID: registration.ActionID, IntentHash: registration.IntentHash, VictimFleet: registration.VictimFleet, VictimHotkey: registration.VictimHotkey, Snapshot: registration.PrePrune}
		files["public/"+preName], err = json.Marshal(pre)
		if err != nil {
			t.Fatal(err)
		}
		files["public/"+registrationName], err = json.Marshal(registration)
		if err != nil {
			t.Fatal(err)
		}
	}

	cleanupByVariant := map[string][]FleetLifecycleCleanupEvidence{}
	for _, name := range []string{fleetLifecycleVariantTargetTakeover, fleetLifecycleVariantCompanionTakeover, fleetLifecycleVariantFallback} {
		manifest := manifestByVariant[name]
		for memberIndex, member := range manifest.Members {
			memberNo := memberIndex + 1
			actionID, _ := fleetLifecycleCleanupActionID(name, memberNo)
			action := actionByID[actionID]
			entry := journalByAction[actionID]
			cleanup := FleetLifecycleCleanupEvidence{
				Schema: "urnetwork-sim-fleet-binding-cleanup-v2", DeploymentID: source.DeploymentID, PlanHash: plan.PlanHash, ActionID: actionID, IntentHash: action.IntentHash,
				ClientID: fleetLifecycleHex16(member.ClientID), FleetID: fleetLifecycleHex(manifest.FleetID), Generation: manifest.Generation,
				CleanedAtEpoch:    map[string]uint64{fleetLifecycleVariantTargetTakeover: 12, fleetLifecycleVariantCompanionTakeover: 14, fleetLifecycleVariantFallback: 14}[name],
				MemberCountBefore: uint64(5 - memberNo), MemberCountAfter: uint64(4 - memberNo), TransactionHash: entry.TransactionHash,
				BeforeBlock: ChainHead{Number: entry.BlockNumber - 1, Hash: finalTestHex(byte(entry.BlockNumber - 1))}, BlockNumber: entry.BlockNumber, BlockHash: entry.BlockHash,
			}
			cleanupByVariant[name] = append(cleanupByVariant[name], cleanup)
			files["public/"+fleetLifecycleCleanupEvidenceName(name, memberNo)], err = json.Marshal(cleanup)
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	cycleFor := func(validatorID int, epoch uint64) *FinalCRv4Cycle {
		for index := range source.Validators[validatorID-1].Cycles {
			if source.Validators[validatorID-1].Cycles[index].SettlementEpoch == epoch {
				return &source.Validators[validatorID-1].Cycles[index]
			}
		}
		return nil
	}
	lifecycleIndex := &FinalFleetLifecycleEvidence{State: FleetLifecycleEvidence{FirstAcceptedEpoch: 10, TakeoverEffectiveEpoch: 10, FallbackEffectiveEpoch: 11, ProviderEffectiveEpoch: 13, TerminalEffectiveEpoch: 15}, Roles: mustFinalFleetLifecycleRoles(t, &identities)}
	milestones := []struct {
		epoch uint64
		name  string
	}{
		{10, fleetLifecycleMilestoneTakeoverRejected},
		{11, fleetLifecycleMilestoneFallbackActive},
		{13, fleetLifecycleMilestoneProviderActive},
	}
	censuses := make([]FleetLifecycleCandidateCensus, 0, len(milestones))
	for milestoneIndex, milestone := range milestones {
		epoch := milestone.epoch
		cycle := cycleFor(1, epoch)
		if cycle == nil {
			t.Fatalf("missing lifecycle source cycle for settlement epoch %d", epoch)
		}
		candidateUIDs := make([]uint16, 0, len(cycle.Candidates))
		candidateHotkeys := make([]string, 0, len(cycle.Candidates))
		for _, candidate := range cycle.Candidates {
			candidateUIDs = append(candidateUIDs, candidate.UID)
			hotkey := derive32(cfg, fmt.Sprintf("fixture-candidate/%d/%d", epoch, candidate.FleetID))
			candidateHotkeys = append(candidateHotkeys, fleetLifecycleHex(hotkey))
		}
		for _, fleetID := range []uint64{fleetLifecycleTargetFleet, fleetLifecycleCompanionFleet} {
			uid, hotkey, _, headErr := finalFleetLifecycleHeadAt(lifecycleIndex, fleetID, epoch)
			if headErr != nil {
				t.Fatal(headErr)
			}
			found := false
			for candidateIndex, candidateUID := range candidateUIDs {
				if candidateUID == uid {
					candidateHotkeys[candidateIndex], found = hotkey, true
					break
				}
			}
			if !found {
				t.Fatalf("settlement epoch %d lifecycle fleet %d UID %d is absent", epoch, fleetID, uid)
			}
		}
		census := FleetLifecycleCandidateCensus{
			Phase: "release-1.0", Milestone: milestone.name, ObservationHash: finalTestHex(byte(180 + milestoneIndex)),
			ObservedHead:  ChainHead{Number: cycle.EVMSnapshot.Number + 5, Hash: finalTestHex(byte(cycle.EVMSnapshot.Number + 5))},
			CandidateUIDs: candidateUIDs, CandidateHotkeys: candidateHotkeys,
		}
		for validatorID := 1; validatorID <= 2; validatorID++ {
			cycle := cycleFor(validatorID, epoch)
			if cycle == nil {
				t.Fatalf("missing lifecycle validator %d cycle for settlement epoch %d", validatorID, epoch)
			}
			row := FleetLifecycleValidatorCensus{
				ValidatorID: validatorID, SettlementEpoch: cycle.SettlementEpoch, SubnetEpoch: cycle.SubnetEpoch,
				NativeSnapshot: cycle.NativeSnapshot, EVMSnapshot: cycle.EVMSnapshot, MeasurementArtifactHash: cycle.MeasurementArtifact.ContentHash,
				VectorHash: cycle.IntentVectorHash, ExtrinsicHash: cycle.Commit.ExtrinsicHash, Commit: cycle.Commit.Block,
				RevealBlock: cycle.Reveal.Block.Number, RevealBlockHash: cycle.Reveal.Block.Hash, Application: cycle.Application.Block,
			}
			for _, candidate := range cycle.Candidates {
				row.EligibleUIDs = append(row.EligibleUIDs, candidate.UID)
				row.AppliedWeights = append(row.AppliedWeights, IntentWeightObservation{UID: candidate.UID, Value: candidate.AppliedWeight})
				if candidate.Selected {
					row.SelectedUIDs = append(row.SelectedUIDs, candidate.UID)
				} else {
					row.RejectedUIDs = append(row.RejectedUIDs, candidate.UID)
				}
			}
			census.Validators = append(census.Validators, row)
			if row.Application.Number+5 > census.NativeObservedHead.Number {
				census.NativeObservedHead = ChainHead{Number: row.Application.Number + 5, Hash: finalTestHex(byte(row.Application.Number + 5))}
			}
		}
		censuses = append(censuses, census)
	}

	rowFor := func(epoch uint64, noID uint64) FinalEpochOperatorEvidence {
		for _, row := range source.Epochs {
			if row.Epoch == epoch && row.NoID == noID {
				return row
			}
		}
		t.Fatalf("missing epoch row %d/%d", epoch, noID)
		return FinalEpochOperatorEvidence{}
	}
	clientIDs := func(name string) []string {
		manifest := manifestByVariant[name]
		result := make([]string, len(manifest.Members))
		for index, member := range manifest.Members {
			result[index] = fleetLifecycleHex16(member.ClientID)
		}
		sort.Strings(result)
		return result
	}
	e2, e4, e4Companion := rowFor(11, 1), rowFor(13, 1), rowFor(13, 2)
	payoutContentHash := func(row FinalEpochOperatorEvidence) string {
		if !strings.HasPrefix(row.ArtifactHash, "0x") || len(row.ArtifactHash) != 66 {
			t.Fatalf("invalid fixture payout artifact hash for epoch %d operator %d: %q", row.Epoch, row.NoID, row.ArtifactHash)
		}
		return "sha256:" + strings.TrimPrefix(strings.ToLower(row.ArtifactHash), "0x")
	}
	releaseSchedule := fleetLifecycleNativeScheduleFixture(t, "release-1.0", 100, 1750)
	state := FleetLifecycleEvidence{
		Schema: fleetLifecycleEvidenceSchema, DeploymentID: source.DeploymentID, PlanHash: plan.PlanHash, RunID: source.RunID, Stage: fleetLifecycleStageReleaseHandoff,
		FirstAcceptedEpoch: 10, AcceptanceStartBlock: 100, AcceptanceEndBlock: 1600, AcceptanceTerminalBlock: 1750,
		ReleaseHandoffSchedule: releaseSchedule, ReleaseEVMEvidenceDeadlineBlock: releaseSchedule.ApplicationDeadlineBlock,
		TakeoverEffectiveEpoch: 10, FallbackEffectiveEpoch: 11, ProviderEffectiveEpoch: 13, TerminalEffectiveEpoch: 15,
		PostRegistrationRewardBaseline: ChainHead{Number: 900, Hash: finalTestHex(132)}, LaunchPrune: &launch,
		FallbackRegistration: registrationByVariant[fleetLifecycleVariantFallback], ProviderRegistration: registrationByVariant[fleetLifecycleVariantProvider], TerminalRegistration: registrationByVariant[fleetLifecycleVariantTerminal],
		TargetCleanup: cleanupByVariant[fleetLifecycleVariantTargetTakeover], CompanionCleanup: cleanupByVariant[fleetLifecycleVariantCompanionTakeover], FallbackCleanup: cleanupByVariant[fleetLifecycleVariantFallback], CandidateCensuses: censuses,
		Payouts: []FleetLifecyclePayoutEvidence{
			{Epoch: 11, NoID: 1, ContentHash: payoutContentHash(e2), PayoutRoot: e2.PayoutRoot, ClientIDs: clientIDs(fleetLifecycleVariantTargetTakeover), Disposition: "pruned-provider-returned-to-operator-pool"},
			{Epoch: 11, NoID: 1, ContentHash: payoutContentHash(e2), PayoutRoot: e2.PayoutRoot, ClientIDs: clientIDs(fleetLifecycleVariantFallback), Disposition: "fallback-provider-head-excluded"},
			{Epoch: 13, NoID: 1, ContentHash: payoutContentHash(e4), PayoutRoot: e4.PayoutRoot, ClientIDs: clientIDs(fleetLifecycleVariantProvider), Disposition: "reregistered-provider-head-excluded"},
			{Epoch: 13, NoID: 2, ContentHash: payoutContentHash(e4Companion), PayoutRoot: e4Companion.PayoutRoot, ClientIDs: clientIDs(fleetLifecycleVariantCompanionTakeover), Disposition: "second-pruned-provider-returned-to-operator-pool"},
		},
	}
	files["public/fleet-lifecycle.json"], err = fleetLifecycleCanonicalBytes(&state)
	if err != nil {
		t.Fatal(err)
	}
	rolesIndex := mustFinalFleetLifecycleRoles(t, &identities)
	semantic := &FinalFleetLifecycleEvidence{
		ClientsPerHeadFleet: 4, ReleaseHandoffHash: bytesSHA256(files["public/fleet-lifecycle.json"]),
		ReleaseHandoffSize: uint64(len(files["public/fleet-lifecycle.json"])), Roles: rolesIndex, State: state,
	}
	semantic.Variants, err = verifyAndIndexFinalFleetLifecycle(source, semantic, &plan, journalEntries, &identities, files)
	if err != nil {
		t.Fatalf("index lifecycle fixture: %v", err)
	}
	for censusIndex := range state.CandidateCensuses {
		for _, row := range state.CandidateCensuses[censusIndex].Validators {
			cycle := cycleFor(row.ValidatorID, row.SettlementEpoch)
			if cycle == nil {
				t.Fatalf("missing lifecycle applied-decision cycle %d/%d", row.ValidatorID, row.SettlementEpoch)
			}
			semantic.AppliedDecisions = append(semantic.AppliedDecisions, FinalFleetLifecycleAppliedDecision{
				CensusIndex: uint64(censusIndex), ValidatorID: uint64(row.ValidatorID), SettlementEpoch: row.SettlementEpoch,
				SubnetEpoch: row.SubnetEpoch, VectorHash: row.VectorHash, Intent: cycle.IntentArtifact,
				Measurement: cycle.MeasurementArtifact, Envelope: cycle.MeasurementEnvelope,
			})
		}
	}
	for _, row := range []FinalEpochOperatorEvidence{e2, e4, e4Companion} {
		if row.Root == nil || row.PayoutArtifact == nil {
			t.Fatalf("missing lifecycle payout row %d/%d", row.Epoch, row.NoID)
		}
		semantic.PayoutArtifacts = append(semantic.PayoutArtifacts, FinalFleetLifecyclePayoutArtifact{Epoch: row.Epoch, NoID: row.NoID, Root: *row.Root, Artifact: *row.PayoutArtifact})
	}
	paths, err := finalFleetLifecycleExpectedPaths(4)
	if err != nil {
		t.Fatal(err)
	}
	lineage := finalFleetLifecycleLineageArtifact{Schema: finalFleetLifecycleLineageSchema, DeploymentID: source.DeploymentID, PlanHash: plan.PlanHash, RunID: source.RunID}
	for _, path := range paths {
		data := files[path]
		if data == nil {
			t.Fatalf("missing lifecycle fixture file %s", path)
		}
		lineage.Files = append(lineage.Files, finalFleetLifecycleLineageFile{Path: path, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data)), Data: data})
	}
	lineageBytes, err := json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	semantic.LineageArtifact = addArtifact("fleet-lifecycle-lineage", "fleet-lifecycle-lineage.json", lineageBytes)
	source.FleetLifecycle = semantic

	terminal := ChainHead{Number: 1750, Hash: finalTestHex(0xd6)}
	source.NativeTerminalHead = terminal
	for index := range source.HeadFleets {
		source.HeadFleets[index].Snapshot = terminal
	}
	for index := range source.Pools {
		source.Pools[index].Snapshot = terminal
	}
	for index := range source.Validators {
		source.Validators[index].Snapshot = terminal
	}
	for _, fleetID := range []uint64{fleetLifecycleTargetFleet, fleetLifecycleCompanionFleet} {
		uid, hotkey, coldkey, headErr := finalFleetLifecycleHeadAt(semantic, fleetID, 15)
		if headErr != nil {
			t.Fatal(headErr)
		}
		fleet := &source.HeadFleets[fleetID-1]
		fleet.UID, fleet.Generation = uid, fleetLifecycleGeneration
		hotkeyBytes, _ := decodeHex32("fixture lifecycle hotkey", hotkey)
		coldkeyBytes, _ := decodeHex32("fixture lifecycle coldkey", coldkey)
		fleet.Hotkey = mustFinalSS58(t, hotkeyBytes)
		fleet.Coldkey = mustFinalSS58(t, coldkeyBytes)
		registration := state.ProviderRegistration
		if fleetID == fleetLifecycleCompanionFleet {
			registration = state.TerminalRegistration
		}
		proof := addArtifact("native-receipt", fmt.Sprintf("fleet-%d-lifecycle-registration.json", fleetID), []byte(fmt.Sprintf("fleet-%d lifecycle registration", fleetID)))
		fleet.Registration = FinalNativeReceipt{ExtrinsicHash: registration.TransactionHash, Block: ChainHead{Number: registration.BlockNumber, Hash: registration.BlockHash}, Proof: proof}
	}

	var topologyBindings []FinalFleetMemberBindingEvidence
	if err := json.Unmarshal(artifacts[source.Topology.BindingManifest.URI], &topologyBindings); err != nil {
		t.Fatal(err)
	}
	for index := range topologyBindings {
		if topologyBindings[index].FleetID == fleetLifecycleTargetFleet {
			topologyBindings[index].HeadUID, topologyBindings[index].Generation = fleetLifecycleCompanionExpectedUID, fleetLifecycleGeneration
		} else if topologyBindings[index].FleetID == fleetLifecycleCompanionFleet {
			topologyBindings[index].HeadUID, topologyBindings[index].Generation = fleetLifecycleTerminalVictimUID, fleetLifecycleGeneration
		}
	}
	topologyBytes, err := json.Marshal(topologyBindings)
	if err != nil {
		t.Fatal(err)
	}
	artifacts[source.Topology.BindingManifest.URI] = topologyBytes
	source.Topology.BindingManifest.ContentHash = bytesSHA256(topologyBytes)
	source.Topology.BindingManifest.SizeBytes = uint64(len(topologyBytes))
	source.Topology.BindingManifestHash = source.Topology.BindingManifest.ContentHash

	// Rebind every reward row and its full snapshot artifact to the historical
	// lifecycle owner/UID at that settlement epoch. The provider-active interval begins at the exact
	// authenticated post-registration baseline.
	rewardHeads := map[uint64][2]ChainHead{
		// Keep each observation after its applied vector and before the next
		// native owner mutation. Reusing a boundary while changing its value
		// would assert two different states for one immutable block.
		10: {{Number: 100, Hash: finalTestHex(100)}, {Number: 140, Hash: finalTestHex(140)}},
		11: {{Number: 400, Hash: finalTestHex(144)}, {Number: 690, Hash: finalTestHex(178)}},
		12: {{Number: 700, Hash: finalTestHex(188)}, {Number: 740, Hash: finalTestHex(228)}},
		13: {state.PostRegistrationRewardBaseline, {Number: 1290, Hash: finalTestHex(10)}},
		14: {{Number: 1300, Hash: finalTestHex(20)}, {Number: 1390, Hash: finalTestHex(110)}},
	}
	for index := range source.NativeRewards {
		reward := &source.NativeRewards[index]
		heads := rewardHeads[reward.Epoch]
		reward.Before, reward.After = heads[0], heads[1]
		reward.OwnerStakeBeforeEVM, reward.OwnerStakeAfterEVM = heads[0], heads[1]
		if reward.Role != "head" {
			continue
		}
		uid, hotkey, coldkey, headErr := finalFleetLifecycleHeadAt(semantic, reward.SubjectID, reward.Epoch)
		if headErr == nil {
			reward.UID, reward.Hotkey, reward.OwnerColdkey = uid, hotkey, coldkey
		}
		selected := false
		if cycle := cycleFor(1, reward.Epoch); cycle != nil {
			for _, candidate := range cycle.Candidates {
				if candidate.FleetID == reward.SubjectID {
					selected = candidate.Selected
				}
			}
		}
		if selected {
			reward.Expected, reward.BeforeRao, reward.AfterRao, reward.DeltaRao = "positive", "10", "20", "10"
			reward.StakeBeforeRao, reward.StakeAfterRao, reward.StakeDeltaRao = "1000", "1010", "10"
			reward.OwnerStakeBeforeRao, reward.OwnerStakeAfterRao, reward.OwnerStakeDeltaRao = "1000", "1010", "10"
			reward.BeforeIncentiveU16, reward.AfterIncentiveU16 = 1, 2
		} else {
			reward.Expected, reward.BeforeRao, reward.AfterRao, reward.DeltaRao = "zero", "0", "0", "0"
			reward.StakeBeforeRao, reward.StakeAfterRao, reward.StakeDeltaRao = "100", "100", "0"
			reward.OwnerStakeBeforeRao, reward.OwnerStakeAfterRao, reward.OwnerStakeDeltaRao = "100", "100", "0"
			reward.BeforeIncentiveU16, reward.AfterIncentiveU16 = 0, 0
		}
	}
	rebuildFinalSemanticRewardFixtureArtifacts(t, source, artifacts)

	// The plan hash changed when the exact lifecycle actions were bound. Update
	// the already-authenticated deployment artifact without changing any other
	// signed deployment field.
	var deploymentArtifact map[string]json.RawMessage
	if err := json.Unmarshal(artifacts[source.Deployment.Artifact.URI], &deploymentArtifact); err != nil {
		t.Fatal(err)
	}
	deploymentArtifact["plan_hash"], _ = json.Marshal(source.PlanHash)
	data, err := json.Marshal(deploymentArtifact)
	if err != nil {
		t.Fatal(err)
	}
	artifacts[source.Deployment.Artifact.URI] = data
	source.Deployment.Artifact.ContentHash = bytesSHA256(data)
	source.Deployment.Artifact.SizeBytes = uint64(len(data))

	setArtifact := func(locator *FinalArtifactLocator, data []byte) {
		artifacts[locator.URI] = append([]byte(nil), data...)
		locator.ContentHash = bytesSHA256(data)
		locator.SizeBytes = uint64(len(data))
	}
	for index := range source.HeadFleets {
		fleet := &source.HeadFleets[index]
		var wrapper struct {
			Manifest json.RawMessage `json:"manifest"`
			UID      uint16          `json:"uid"`
			Snapshot ChainHead       `json:"snapshot"`
		}
		if err := json.Unmarshal(artifacts[fleet.BindingArtifact.URI], &wrapper); err != nil {
			t.Fatal(err)
		}
		switch fleet.FleetID {
		case fleetLifecycleTargetFleet:
			wrapper.Manifest = manifestBytesByVariant[fleetLifecycleVariantProvider]
		case fleetLifecycleCompanionFleet:
			wrapper.Manifest = manifestBytesByVariant[fleetLifecycleVariantTerminal]
		}
		wrapper.UID, wrapper.Snapshot = fleet.UID, fleet.Snapshot
		encoded, marshalErr := json.Marshal(wrapper)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		setArtifact(&fleet.BindingArtifact, encoded)
	}
	for index := range source.Pools {
		pool := &source.Pools[index]
		hotkey, coldkey, identityErr := finalSemanticSS58Pair("lifecycle fixture pool", pool.Hotkey, pool.Coldkey)
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		state := FinalCollectedNativeUIDState{UID: pool.UID, HotkeyPublicKey: "0x" + hex.EncodeToString(hotkey[:]), ColdkeyPublicKey: "0x" + hex.EncodeToString(coldkey[:]), RegistrationBlock: pool.Registration.Block.Number}
		encoded, marshalErr := json.Marshal(map[string]any{
			"snapshot": pool.Snapshot, "state": state, "settlement_vault": source.Deployment.SettlementVault,
			"vault_mirror_coldkey": pool.Coldkey, "operator_registry_coldkey": pool.OperatorColdkey,
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		setArtifact(&pool.OwnershipArtifact, encoded)
	}
	for index := range source.Validators {
		validator := &source.Validators[index]
		hotkey, coldkey, identityErr := finalSemanticSS58Pair("lifecycle fixture validator", validator.Hotkey, validator.Coldkey)
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		state := FinalCollectedNativeUIDState{
			UID: validator.UID, HotkeyPublicKey: "0x" + hex.EncodeToString(hotkey[:]), ColdkeyPublicKey: "0x" + hex.EncodeToString(coldkey[:]),
			RegistrationBlock: validator.Registration.Block.Number, StakeRao: validator.StakeRao,
			ValidatorPermit: validator.ValidatorPermit, ValidatorTrustU16: validator.ValidatorTrustU16,
		}
		encoded, marshalErr := json.Marshal(map[string]any{"snapshot": validator.Snapshot, "state": state})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		setArtifact(&validator.SnapshotArtifact, encoded)
	}
	for index := range source.HeadTransitions {
		transition := &source.HeadTransitions[index]
		var artifact finalHeadTournamentTransitionArtifact
		if err := json.Unmarshal(artifacts[transition.Artifact.URI], &artifact); err != nil || artifact.Postcondition == nil {
			t.Fatalf("decode lifecycle fixture tournament artifact: %v", err)
		}
		pruned, roleErr := finalFleetLifecycleRole(source.FleetLifecycle, fmt.Sprintf("churn-%d-hotkey", transition.PrunedChurn))
		if roleErr != nil {
			t.Fatal(roleErr)
		}
		artifact.Pruned = finalHeadTournamentIdentity{Role: pruned.Label, PublicKey: pruned.PublicKey, SS58: pruned.SS58}
		transition.PrunedHotkey = pruned.SS58
		postconditionBytes, marshalErr := json.Marshal(artifact.Postcondition)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		setArtifact(&transition.Registration.Proof, postconditionBytes)
		source.HeadFleets[transition.ChallengerFleetID-1].Registration.Proof = transition.Registration.Proof
		artifactBytes, marshalErr := json.Marshal(artifact)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		setArtifact(&transition.Artifact, artifactBytes)
	}
}

func mustFinalFleetLifecycleRoles(t *testing.T, identities *finalPublicIdentities) []FinalFleetLifecycleRole {
	t.Helper()
	roles, err := finalFleetLifecycleRoles(identities)
	if err != nil {
		t.Fatal(err)
	}
	return roles
}

func mustFinalSS58(t *testing.T, key [32]byte) string {
	t.Helper()
	encoded, err := ss58.Encode(key, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func rebuildFinalSemanticRewardFixtureArtifacts(t *testing.T, source *FinalSemanticEvidence, artifacts map[string][]byte) {
	t.Helper()
	for epoch := source.Window.FirstEpoch; epoch < source.Window.FirstEpoch+source.Window.EpochCount; epoch++ {
		indices := make([]int, 0, finalHeadCandidateCount+4)
		maximumUID := uint16(0)
		for index := range source.NativeRewards {
			if source.NativeRewards[index].Epoch == epoch {
				indices = append(indices, index)
				if source.NativeRewards[index].UID > maximumUID {
					maximumUID = source.NativeRewards[index].UID
				}
			}
		}
		before := &NativeRewardObservation{FinalizedHead: source.NativeRewards[indices[0]].Before, EmissionRao: make([]string, int(maximumUID)+1), Incentive: make([]uint16, int(maximumUID)+1), Dividends: make([]uint16, int(maximumUID)+1), TotalHotkeyAlphaRao: make([]string, int(maximumUID)+1)}
		after := &NativeRewardObservation{FinalizedHead: source.NativeRewards[indices[0]].After, EmissionRao: make([]string, int(maximumUID)+1), Incentive: make([]uint16, int(maximumUID)+1), Dividends: make([]uint16, int(maximumUID)+1), TotalHotkeyAlphaRao: make([]string, int(maximumUID)+1)}
		for index := range before.EmissionRao {
			before.EmissionRao[index], before.TotalHotkeyAlphaRao[index] = "0", "0"
			after.EmissionRao[index], after.TotalHotkeyAlphaRao[index] = "0", "0"
		}
		beforePositions, afterPositions := map[string]FinalCollectedRewardStakePosition{}, map[string]FinalCollectedRewardStakePosition{}
		for _, index := range indices {
			reward := &source.NativeRewards[index]
			uid := int(reward.UID)
			before.EmissionRao[uid], after.EmissionRao[uid] = reward.BeforeRao, reward.AfterRao
			before.TotalHotkeyAlphaRao[uid], after.TotalHotkeyAlphaRao[uid] = reward.StakeBeforeRao, reward.StakeAfterRao
			before.Incentive[uid], after.Incentive[uid] = reward.BeforeIncentiveU16, reward.AfterIncentiveU16
			before.Dividends[uid], after.Dividends[uid] = reward.BeforeDividendsU16, reward.AfterDividendsU16
			key := reward.Hotkey + "/" + reward.OwnerColdkey
			beforePositions[key] = FinalCollectedRewardStakePosition{Identity: reward.Role + "-" + fmt.Sprint(reward.SubjectID) + "-owner", HotkeyPublicKey: reward.Hotkey, ColdkeyPublicKey: reward.OwnerColdkey, StakeRao: reward.OwnerStakeBeforeRao}
			afterPositions[key] = FinalCollectedRewardStakePosition{Identity: reward.Role + "-" + fmt.Sprint(reward.SubjectID) + "-owner", HotkeyPublicKey: reward.Hotkey, ColdkeyPublicKey: reward.OwnerColdkey, StakeRao: reward.OwnerStakeAfterRao}
			if reward.ReserveColdkey != "" {
				reserveKey := reward.Hotkey + "/" + reward.ReserveColdkey
				beforePositions[reserveKey] = FinalCollectedRewardStakePosition{Identity: "reserve-validator-sink", HotkeyPublicKey: reward.Hotkey, ColdkeyPublicKey: reward.ReserveColdkey, StakeRao: reward.ReserveStakeBeforeRao}
				afterPositions[reserveKey] = FinalCollectedRewardStakePosition{Identity: "reserve-validator-sink", HotkeyPublicKey: reward.Hotkey, ColdkeyPublicKey: reward.ReserveColdkey, StakeRao: reward.ReserveStakeAfterRao}
			}
		}
		beforeSnapshot := &FinalCollectedRewardStakeSnapshot{NativeHead: before.FinalizedHead, EVMHead: source.NativeRewards[indices[0]].OwnerStakeBeforeEVM}
		afterSnapshot := &FinalCollectedRewardStakeSnapshot{NativeHead: after.FinalizedHead, EVMHead: source.NativeRewards[indices[0]].OwnerStakeAfterEVM}
		for _, position := range beforePositions {
			beforeSnapshot.Positions = append(beforeSnapshot.Positions, position)
		}
		for _, position := range afterPositions {
			afterSnapshot.Positions = append(afterSnapshot.Positions, position)
		}
		sort.Slice(beforeSnapshot.Positions, func(i, j int) bool {
			return beforeSnapshot.Positions[i].Identity < beforeSnapshot.Positions[j].Identity
		})
		sort.Slice(afterSnapshot.Positions, func(i, j int) bool { return afterSnapshot.Positions[i].Identity < afterSnapshot.Positions[j].Identity })
		application, err := finalSemanticApplicationBlock(source, epoch)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(map[string]any{"epoch": epoch, "application_block": application, "before": before, "after": after, "before_owner_stakes": beforeSnapshot, "after_owner_stakes": afterSnapshot})
		if err != nil {
			t.Fatal(err)
		}
		locator := FinalArtifactLocator{Kind: "native-reward-snapshot", URI: fmt.Sprintf("artifacts/native-reward-epoch-%d.json", epoch), ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
		artifacts[locator.URI] = data
		for _, index := range indices {
			source.NativeRewards[index].SnapshotArtifact = locator
		}
	}
}

func TestFinalFleetLifecycleHeadAtUsesExactSettlementTransitions(t *testing.T) {
	role := func(churn int, cold bool) FinalFleetLifecycleRole {
		label := churnHotkeyLabel(churn)
		if cold {
			label = churnColdkeyLabel(churn)
		}
		return FinalFleetLifecycleRole{Label: label, PublicKey: finalTestHex(byte(20 + 2*churn + map[bool]int{true: 1}[cold])), SS58: label}
	}
	lifecycle := &FinalFleetLifecycleEvidence{
		State: FleetLifecycleEvidence{
			FirstAcceptedEpoch:     100,
			TakeoverEffectiveEpoch: 101,
			FallbackEffectiveEpoch: 104,
			ProviderEffectiveEpoch: 111,
			TerminalEffectiveEpoch: 123,
		},
	}
	for _, churn := range []int{fleetLifecycleFallbackChurn, fleetLifecycleTargetChurn, fleetLifecycleCompanionChurn} {
		lifecycle.Roles = append(lifecycle.Roles, role(churn, false), role(churn, true))
	}
	tests := []struct {
		name    string
		fleet   uint64
		epoch   uint64
		uid     uint16
		churn   int
		wantErr bool
	}{
		{name: "pre-takeover", fleet: fleetLifecycleTargetFleet, epoch: 100, wantErr: true},
		{name: "target takeover", fleet: fleetLifecycleTargetFleet, epoch: 101, uid: fleetLifecycleTargetExpectedUID, churn: fleetLifecycleTargetChurn},
		{name: "target before nonadjacent fallback", fleet: fleetLifecycleTargetFleet, epoch: 103, uid: fleetLifecycleTargetExpectedUID, churn: fleetLifecycleTargetChurn},
		{name: "fallback active", fleet: fleetLifecycleTargetFleet, epoch: 104, uid: fleetLifecycleTargetExpectedUID, churn: fleetLifecycleFallbackChurn},
		{name: "provider restored", fleet: fleetLifecycleTargetFleet, epoch: 111, uid: fleetLifecycleCompanionExpectedUID, churn: fleetLifecycleTargetChurn},
		{name: "companion takeover", fleet: fleetLifecycleCompanionFleet, epoch: 101, uid: fleetLifecycleCompanionExpectedUID, churn: fleetLifecycleCompanionChurn},
		{name: "fallback fills companion", fleet: fleetLifecycleCompanionFleet, epoch: 111, uid: fleetLifecycleTargetExpectedUID, churn: fleetLifecycleFallbackChurn},
		{name: "terminal restored", fleet: fleetLifecycleCompanionFleet, epoch: 123, uid: fleetLifecycleTerminalVictimUID, churn: fleetLifecycleCompanionChurn},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uid, hotkey, coldkey, err := finalFleetLifecycleHeadAt(lifecycle, test.fleet, test.epoch)
			if test.wantErr {
				if err == nil {
					t.Fatal("pre-takeover settlement epoch was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if uid != test.uid || hotkey != role(test.churn, false).PublicKey || coldkey != role(test.churn, true).PublicKey {
				t.Fatalf("head=(%d,%s,%s), want UID %d churn-%d owner", uid, hotkey, coldkey, test.uid, test.churn)
			}
		})
	}

	malformed := *lifecycle
	malformed.State = lifecycle.State
	malformed.State.ProviderEffectiveEpoch = malformed.State.FallbackEffectiveEpoch
	if _, _, _, err := finalFleetLifecycleHeadAt(&malformed, fleetLifecycleTargetFleet, malformed.State.ProviderEffectiveEpoch); err == nil {
		t.Fatal("overlapping settlement transition epochs were accepted")
	}
}

func TestFinalSemanticFleetByUIDAtRejectsTerminalBackdatingAndAmbiguity(t *testing.T) {
	lifecycle := &FinalFleetLifecycleEvidence{State: FleetLifecycleEvidence{
		FirstAcceptedEpoch: 100, TakeoverEffectiveEpoch: 101, FallbackEffectiveEpoch: 104,
		ProviderEffectiveEpoch: 111, TerminalEffectiveEpoch: 123,
	}}
	for _, churn := range []int{fleetLifecycleFallbackChurn, fleetLifecycleTargetChurn, fleetLifecycleCompanionChurn} {
		lifecycle.Roles = append(lifecycle.Roles,
			FinalFleetLifecycleRole{Label: churnHotkeyLabel(churn), PublicKey: finalTestHex(byte(40 + 2*churn)), SS58: churnHotkeyLabel(churn)},
			FinalFleetLifecycleRole{Label: churnColdkeyLabel(churn), PublicKey: finalTestHex(byte(41 + 2*churn)), SS58: churnColdkeyLabel(churn)},
		)
	}
	source := &FinalSemanticEvidence{
		FleetLifecycle: lifecycle,
		HeadFleets: []FinalHeadFleetEvidence{
			{FleetID: fleetLifecycleTargetFleet, UID: fleetLifecycleCompanionExpectedUID, Registered: true},
			{FleetID: fleetLifecycleCompanionFleet, UID: fleetLifecycleTerminalVictimUID, Registered: true},
			{FleetID: 7, UID: 77, Registered: true},
		},
	}
	for _, test := range []struct {
		epoch uint64
		want  map[uint16]uint64
	}{
		{101, map[uint16]uint64{fleetLifecycleTargetExpectedUID: fleetLifecycleTargetFleet, fleetLifecycleCompanionExpectedUID: fleetLifecycleCompanionFleet, 77: 7}},
		{104, map[uint16]uint64{fleetLifecycleTargetExpectedUID: fleetLifecycleTargetFleet, fleetLifecycleCompanionExpectedUID: fleetLifecycleCompanionFleet, 77: 7}},
		{111, map[uint16]uint64{fleetLifecycleCompanionExpectedUID: fleetLifecycleTargetFleet, fleetLifecycleTargetExpectedUID: fleetLifecycleCompanionFleet, 77: 7}},
		{123, map[uint16]uint64{fleetLifecycleCompanionExpectedUID: fleetLifecycleTargetFleet, fleetLifecycleTerminalVictimUID: fleetLifecycleCompanionFleet, 77: 7}},
	} {
		got, err := finalSemanticFleetByUIDAt(source, test.epoch)
		if err != nil {
			t.Fatalf("epoch %d: %v", test.epoch, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("epoch %d UID map=%v, want %v", test.epoch, got, test.want)
		}
	}
	if _, err := finalSemanticFleetByUIDAt(source, 100); err == nil {
		t.Fatal("pre-takeover candidate map was accepted")
	}
	ambiguous := *source
	ambiguous.HeadFleets = append(append([]FinalHeadFleetEvidence(nil), source.HeadFleets...), FinalHeadFleetEvidence{FleetID: 8, UID: fleetLifecycleTargetExpectedUID, Registered: true})
	if _, err := finalSemanticFleetByUIDAt(&ambiguous, 104); err == nil || !strings.Contains(err.Error(), "ambiguously maps UID") {
		t.Fatalf("reused UID ambiguity was not rejected: %v", err)
	}
}

func TestFinalPayoutAssignmentsAtUsesExactLifecycleEpochMembership(t *testing.T) {
	ids := make([]connect.Id, 0, 16)
	memberSet := func(offset byte) []FinalFleetLifecycleMember {
		members := make([]FinalFleetLifecycleMember, 4)
		for index := range members {
			var id connect.Id
			id[0], id[15] = offset, byte(index+1)
			ids = append(ids, id)
			members[index] = FinalFleetLifecycleMember{ClientID: "0x" + hex.EncodeToString(id[:]), ClientKey: finalTestHex(byte(offset + byte(index) + 1))}
		}
		return members
	}
	variants := []FinalFleetLifecycleVariantEvidence{
		{Name: fleetLifecycleVariantTargetTakeover, Members: memberSet(0x10)},
		{Name: fleetLifecycleVariantFallback, Members: memberSet(0x20)},
		{Name: fleetLifecycleVariantProvider, Members: memberSet(0x30)},
		{Name: fleetLifecycleVariantCompanionTakeover, Members: memberSet(0x40)},
	}
	variants[2].Members = append([]FinalFleetLifecycleMember(nil), variants[0].Members...)
	variants = append(variants, FinalFleetLifecycleVariantEvidence{Name: fleetLifecycleVariantTerminal, Members: append([]FinalFleetLifecycleMember(nil), variants[3].Members...)})
	clientIDs := func(variant int) []string {
		result := make([]string, len(variants[variant].Members))
		for index, member := range variants[variant].Members {
			result[index] = member.ClientID
		}
		return result
	}
	lifecycle := &FinalFleetLifecycleEvidence{Variants: variants, State: FleetLifecycleEvidence{TakeoverEffectiveEpoch: 101, FallbackEffectiveEpoch: 107, ProviderEffectiveEpoch: 119, TerminalEffectiveEpoch: 131, Payouts: []FleetLifecyclePayoutEvidence{
		{Epoch: 107, NoID: 1, Disposition: "pruned-provider-returned-to-operator-pool", ClientIDs: clientIDs(0)},
		{Epoch: 107, NoID: 1, Disposition: "fallback-provider-head-excluded", ClientIDs: clientIDs(1)},
		{Epoch: 119, NoID: 1, Disposition: "reregistered-provider-head-excluded", ClientIDs: clientIDs(2)},
		{Epoch: 119, NoID: 1, Disposition: "second-pruned-provider-returned-to-operator-pool", ClientIDs: clientIDs(3)},
	}}}
	assignments := make(map[connect.Id]finalMinerAssignment, len(ids))
	for _, id := range ids {
		assignments[id] = finalMinerAssignment{NoID: 1, Tier: "head-candidate"}
	}
	evidence := &FinalSemanticEvidence{FleetLifecycle: lifecycle}
	got, records, err := finalPayoutAssignmentsAt(evidence, &finalPayoutArtifactExpectation{Epoch: 107, NoID: 1}, assignments)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("lifecycle payout records=%d, want 2", len(records))
	}
	for index, id := range ids[:8] {
		want := "pool-tail"
		if index >= 4 {
			want = "head-candidate"
		}
		if got[id].Tier != want {
			t.Fatalf("client %s tier=%s, want %s", id.String(), got[id].Tier, want)
		}
	}

	substituted := *lifecycle
	substituted.State = lifecycle.State
	substituted.State.Payouts = append([]FleetLifecyclePayoutEvidence(nil), lifecycle.State.Payouts...)
	substituted.State.Payouts[0].ClientIDs = append([]string(nil), substituted.State.Payouts[0].ClientIDs...)
	substituted.State.Payouts[0].ClientIDs[0] = variants[1].Members[0].ClientID
	evidence.FleetLifecycle = &substituted
	if _, _, err := finalPayoutAssignmentsAt(evidence, &finalPayoutArtifactExpectation{Epoch: 107, NoID: 1}, assignments); err == nil {
		t.Fatal("cross-variant lifecycle payout client substitution was accepted")
	}

	singleton := *lifecycle
	singleton.State = lifecycle.State
	singleton.State.Payouts = append([]FleetLifecyclePayoutEvidence(nil), lifecycle.State.Payouts...)
	singleton.State.Payouts[1].NoID = 2
	evidence.FleetLifecycle = &singleton
	if _, records, err := finalPayoutAssignmentsAt(evidence, &finalPayoutArtifactExpectation{Epoch: 107, NoID: 1}, assignments); err != nil || len(records) != 1 {
		t.Fatalf("operator-partitioned singleton lifecycle payout rejected: records=%d err=%v", len(records), err)
	}

	duplicated := *lifecycle
	duplicated.Variants = append([]FinalFleetLifecycleVariantEvidence(nil), lifecycle.Variants...)
	duplicated.Variants[1] = lifecycle.Variants[1]
	duplicated.Variants[1].Members = append([]FinalFleetLifecycleMember(nil), lifecycle.Variants[1].Members...)
	duplicated.Variants[1].Members[0] = lifecycle.Variants[0].Members[0]
	duplicated.State = lifecycle.State
	duplicated.State.Payouts = append([]FleetLifecyclePayoutEvidence(nil), lifecycle.State.Payouts...)
	duplicated.State.Payouts[1].ClientIDs = append([]string(nil), lifecycle.State.Payouts[1].ClientIDs...)
	duplicated.State.Payouts[1].ClientIDs[0] = lifecycle.State.Payouts[0].ClientIDs[0]
	evidence.FleetLifecycle = &duplicated
	if _, _, err := finalPayoutAssignmentsAt(evidence, &finalPayoutArtifactExpectation{Epoch: 107, NoID: 1}, assignments); err == nil {
		t.Fatalf("lifecycle client duplicated across included/excluded tiers was accepted: %v", err)
	}
}

func TestFinalFleetLifecyclePublicReplayRejectsEventAndVectorSubstitution(t *testing.T) {
	head := ChainHead{Number: 77, Hash: finalTestHex(0x77)}
	tx := finalTestHex(0x78)
	wantEvent := FinalFleetLifecycleEventState{Kind: "fleet-bound", TransactionHash: tx, Block: head, ClientID: "0x" + strings.Repeat("11", 16)}
	if got, err := finalFleetLifecycleEvent([]FinalFleetLifecycleEventState{wantEvent}, "fleet-bound", tx, head); err != nil || got != wantEvent {
		t.Fatalf("exact lifecycle event rejected: got=%+v err=%v", got, err)
	}
	extra := []FinalFleetLifecycleEventState{wantEvent, {Kind: "fleet-binding-cleaned", TransactionHash: tx, Block: head}}
	if _, err := finalFleetLifecycleEvent(extra, "fleet-bound", tx, head); err == nil || !strings.Contains(err.Error(), "want one") {
		t.Fatalf("extra lifecycle mutation event accepted: %v", err)
	}
	substituted := wantEvent
	substituted.TransactionHash = finalTestHex(0x79)
	if _, err := finalFleetLifecycleEvent([]FinalFleetLifecycleEventState{substituted}, "fleet-bound", tx, head); err == nil {
		t.Fatal("cross-transaction lifecycle event was accepted")
	}

	validator := FleetLifecycleValidatorCensus{
		Application:    head,
		EligibleUIDs:   []uint16{7, 8},
		AppliedWeights: []IntentWeightObservation{{UID: 7, Value: 100}, {UID: 8, Value: 0}},
	}
	public := FinalNativeWeightState{ValidatorUID: 1, UIDs: []uint16{7, 8}, Values: []uint16{100, 0}, Block: head}
	if err := finalFleetLifecycleAppliedWeights(public, validator); err != nil {
		t.Fatalf("exact public lifecycle vector rejected: %v", err)
	}
	public.UIDs = public.UIDs[:1]
	public.Values = public.Values[:1]
	if err := finalFleetLifecycleAppliedWeights(public, validator); err != nil {
		t.Fatalf("implicit public zero lifecycle weight rejected: %v", err)
	}
	public.UIDs = []uint16{8}
	public.Values = []uint16{0}
	if err := finalFleetLifecycleAppliedWeights(public, validator); err == nil || !strings.Contains(err.Error(), "UID 7") {
		t.Fatalf("missing positive lifecycle UID accepted: %v", err)
	}
	public.UIDs = []uint16{7, 8}
	public.Values = []uint16{100, 0}
	public.Values[1] = 1
	if err := finalFleetLifecycleAppliedWeights(public, validator); err == nil || !strings.Contains(err.Error(), "UID 8") {
		t.Fatalf("substituted lifecycle vector accepted: %v", err)
	}
	public.Values[1] = 0
	public.UIDs[1] = 7
	if err := finalFleetLifecycleAppliedWeights(public, validator); err == nil || !strings.Contains(err.Error(), "duplicates UID") {
		t.Fatalf("duplicate lifecycle UID accepted: %v", err)
	}
}
