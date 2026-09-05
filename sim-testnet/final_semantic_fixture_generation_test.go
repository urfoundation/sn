package main

// This file gives the release-scale fixture a real ordinary-fleet and
// temporary-oracle source graph.  It intentionally uses the production
// archive builders, so the broad FINAL test cannot claim a complete release
// while skipping a newly mandatory evidence domain.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
	"github.com/urnetwork/connect"
)

// Holds the private construction data needed to serialize one signed
// ordinary-fleet generation before the production source builder projects it.
type finalSemanticFixtureFleetVersion struct {
	manifest       protocol.FleetManifest
	canonical      []byte
	commitmentHash [32]byte
	commitment     FleetCommitmentEvidence
	validFrom      uint64
	validTo        uint64
	nativeHead     ChainHead
}

// Aligns generated fixture checkpoints with the release fixture's canonical
// block-to-hash convention, so independently assembled evidence domains use
// the same immutable identity when they name one block number.
func finalSemanticFixtureChainHead(number uint64) ChainHead {
	marker := byte(number)
	if marker == 0 {
		marker = 1
	}
	return ChainHead{Number: number, Hash: finalTestHex(marker)}
}

// Adds the complete fixed 200-fleet renewal graph plus its historical
// coordinator/oracle closure.  Inputs are kept in a detached archive map so
// the same source and artifact builders used by production perform every
// proof join before the fixture is returned.
func attachFinalSemanticFixtureGeneration(t *testing.T, source *FinalSemanticEvidence, artifacts map[string][]byte) {
	t.Helper()
	if source == nil || source.FleetLifecycle == nil || source.PlanArtifact.URI == "" {
		t.Fatal("release fixture generation context is incomplete")
	}
	planData, found := artifacts[source.PlanArtifact.URI]
	if !found {
		t.Fatalf("release fixture setup plan %s is absent", source.PlanArtifact.URI)
	}
	current, err := decodePersistedPlanBytes(planData)
	if err != nil || current.PlanHash != source.PlanHash {
		t.Fatalf("release fixture setup plan differs from semantic identity: %v", err)
	}
	if len(current.PriorPlanHashes) != 1 {
		t.Fatalf("release fixture expected one predecessor, got %d", len(current.PriorPlanHashes))
	}
	prior := *current
	prior.PriorPlanHashes = nil
	prior.PlanHash = ""
	priorHash, err := prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	prior.PlanHash = priorHash
	if current.PriorPlanHashes[0] != prior.PlanHash {
		t.Fatalf("release fixture predecessor=%s, want %s", current.PriorPlanHashes[0], prior.PlanHash)
	}
	priorData, err := json.Marshal(&prior)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, decodeErr := decodePersistedPlanBytes(priorData); decodeErr != nil || decoded.PlanHash != prior.PlanHash {
		t.Fatalf("release fixture predecessor is not persisted: %v", decodeErr)
	}

	var lifecycle finalFleetLifecycleLineageArtifact
	lifecycleData, found := artifacts[source.FleetLifecycle.LineageArtifact.URI]
	if !found || json.Unmarshal(lifecycleData, &lifecycle) != nil {
		t.Fatal("release fixture lifecycle lineage is unavailable")
	}
	files := make(map[string][]byte, len(lifecycle.Files)+2_000)
	for _, item := range lifecycle.Files {
		files[item.Path] = append([]byte(nil), item.Data...)
	}
	files["launch-foundation/plan.json"] = append([]byte(nil), planData...)
	files[filepath.ToSlash(filepath.Join("plan-history", stringsTrim0x(prior.PlanHash)+".json"))] = append([]byte(nil), priorData...)
	policyData, found := artifacts[source.PolicyArtifact.URI]
	if !found {
		t.Fatalf("release fixture policy %s is absent", source.PolicyArtifact.URI)
	}
	files[source.PolicyArtifact.URI] = append([]byte(nil), policyData...)

	entries, err := decodeFinalSemanticJournalBytes(files["launch-foundation/journal.jsonl"])
	if err != nil {
		t.Fatalf("decode release fixture lifecycle journal: %v", err)
	}
	appendEntry := func(entry JournalEntry) JournalEntry {
		entry.Schema = "urnetwork-sim-journal-v1"
		entry.Sequence = uint64(len(entries) + 1)
		entry.Time = time.Unix(1_700_010_000+int64(entry.Sequence), 0).UTC().Format(time.RFC3339Nano)
		if len(entries) != 0 {
			entry.PreviousHash = entries[len(entries)-1].EntryHash
		}
		entry.EntryHash = ""
		hash, hashErr := canonicalHashHex(entry)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		entry.EntryHash = hash
		entries = append(entries, entry)
		return entry
	}
	actionByID := make(map[string]Action, len(current.Actions))
	for _, action := range current.Actions {
		if _, duplicate := actionByID[action.ID]; duplicate {
			t.Fatalf("release fixture plan duplicates action %s", action.ID)
		}
		actionByID[action.ID] = action
	}
	action := func(id string) Action {
		value, exists := actionByID[id]
		if !exists {
			t.Fatalf("release fixture plan lacks action %s", id)
		}
		return value
	}
	finalized := func(planHash string, value Action, transactionHash string, head ChainHead) JournalEntry {
		for _, entry := range entries {
			if entry.Stage != StageFinalized || entry.PlanHash != planHash || entry.ActionID != value.ID || entry.IntentHash != value.IntentHash {
				continue
			}
			if entry.TransactionHash != transactionHash || entry.BlockNumber != head.Number || entry.BlockHash != head.Hash {
				t.Fatalf("release fixture action %s finalization differs", value.ID)
			}
			return entry
		}
		return appendEntry(JournalEntry{
			DeploymentID: source.DeploymentID, PlanHash: planHash, ActionID: value.ID, IntentHash: value.IntentHash,
			Stage: StageFinalized, TransactionHash: transactionHash, BlockNumber: head.Number, BlockHash: head.Hash,
		})
	}
	postcondition := func(planHash string, value Action, native, evm ChainHead, observed map[string]any) *ActionPostcondition {
		if len(observed) == 0 {
			observed = map[string]any{"fixture_action": value.ID}
		}
		clone := make(map[string]any, len(observed))
		for key, item := range observed {
			clone[key] = item
		}
		return &ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: source.DeploymentID, PlanHash: planHash, ActionID: value.ID, IntentHash: value.IntentHash,
			OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false,
			SubstrateFinalized: native, EVMFinalized: evm, EVMHashDomain: "evm-rpc", Observed: observed,
			IndependentSubstrateFinalized: native, IndependentEVMFinalized: evm, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: clone,
		}
	}
	verified := func(planHash string, value Action, record *ActionPostcondition) JournalEntry {
		if record == nil {
			t.Fatalf("release fixture action %s has nil postcondition", value.ID)
		}
		path, pathErr := postconditionRelativePath(planHash, value.ID)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		data, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		hash, hashErr := canonicalHashHex(record)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		for _, entry := range entries {
			if entry.Stage != StageVerified || entry.PlanHash != planHash || entry.ActionID != value.ID || entry.IntentHash != value.IntentHash {
				continue
			}
			if entry.PostconditionHash != hash || entry.PostconditionPath != path {
				t.Fatalf("release fixture action %s verified record differs", value.ID)
			}
			files[path] = append([]byte(nil), data...)
			return entry
		}
		files[path] = append([]byte(nil), data...)
		return appendEntry(JournalEntry{
			DeploymentID: source.DeploymentID, PlanHash: planHash, ActionID: value.ID, IntentHash: value.IntentHash,
			Stage: StageVerified, PostconditionHash: hash, PostconditionPath: path,
		})
	}

	cfg := testResolvedConfig(t)
	cfg.Config.Deployment.DeploymentID = source.DeploymentID
	cfg.Netuid = source.Netuid
	cfg.ChainID = source.ChainID
	cfg.ConfigHash = source.ConfigHash
	cfg.PolicyHash = source.PolicyHash
	policy, policyErr := finalSemanticFixturePolicy(source, artifacts)
	if policyErr != nil {
		t.Fatal(policyErr)
	}
	cfg.Policy = policy
	roles, rolesErr := BuildRoleSecrets(cfg)
	if rolesErr != nil {
		t.Fatal(rolesErr)
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

	batcher, _, batcherErr := finalPlanFleetBatcher(current)
	if batcherErr != nil {
		t.Fatal(batcherErr)
	}
	batcherRoot, rootErr := finalReleaseRuntimeRootByName(source, "fleet_batcher")
	if rootErr != nil || !strings.EqualFold(batcherRoot.Address, batcher.Hex()) {
		t.Fatalf("release fixture batcher root differs: %v", rootErr)
	}
	coordinator := common.HexToAddress(source.Deployment.CoordinatorProxy)
	if coordinator == (common.Address{}) {
		t.Fatal("release fixture coordinator is zero")
	}

	versions := make(map[string]finalSemanticFixtureFleetVersion, finalFleetGenerationSetupFleetCount*2+finalFleetGenerationChallengerFleetCount)
	key := func(fleet int, generation uint64) string { return fmt.Sprintf("%d/%d", fleet, generation) }
	nativeCounter := uint64(300)
	makeVersion := func(fleet int, generation uint64) finalSemanticFixtureFleetVersion {
		var fleetID [32]byte
		fleetID = derive32(cfg, fmt.Sprintf("fleet-id/%d", fleet))
		hotkey, hotkeyErr := roleBytes32(roles, fleetHotkeyLabel(fleet))
		if hotkeyErr != nil {
			t.Fatal(hotkeyErr)
		}
		manifest := protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: source.ChainID, Netuid: source.Netuid, FleetID: fleetID, Hotkey: hotkey, Generation: generation}
		copy(manifest.Coordinator[:], coordinator.Bytes())
		for member := 1; member <= int(finalFleetGenerationMembersPerFleet); member++ {
			client := roles.Clients[fmt.Sprintf("miner-%d", fleetMemberMinerIndex(cfg, fleet, member))]
			id, idErr := hex.DecodeString(client.ClientIDHex)
			publicKey, keyErr := hex.DecodeString(client.PublicKeyHex)
			if idErr != nil || keyErr != nil || len(id) != 16 || len(publicKey) != ed25519.PublicKeySize {
				t.Fatalf("release fixture fleet %d member %d identity is invalid", fleet, member)
			}
			var value protocol.FleetMember
			copy(value.ClientID[:], id)
			copy(value.ClientKey[:], publicKey)
			manifest.Members = append(manifest.Members, value)
		}
		canonical, canonicalErr := manifest.Canonical()
		if canonicalErr != nil {
			t.Fatal(canonicalErr)
		}
		commitmentHash, commitmentErr := manifest.CommitmentHash()
		if commitmentErr != nil {
			t.Fatal(commitmentErr)
		}
		validFrom, validTo := uint64(10), uint64(20)
		if generation == 2 {
			validFrom, validTo = 11, 21
		}
		nativeCounter++
		nativeHead := finalSemanticFixtureChainHead(nativeCounter)
		commitmentActionID := fmt.Sprintf("fleet.commitment.%d", fleet)
		suffix := ""
		if generation == 2 {
			commitmentActionID = fmt.Sprintf("fleet.refresh.commitment.%d", fleet)
			suffix = ".refresh"
		}
		commitmentAction := action(commitmentActionID)
		transactionHash := finalFleetGenerationTestHash(1_000_000 + nativeCounter)
		finalized(current.PlanHash, commitmentAction, transactionHash, nativeHead)
		verified(current.PlanHash, commitmentAction, postcondition(current.PlanHash, commitmentAction, nativeHead, finalSemanticFixtureChainHead(100+nativeCounter), map[string]any{"fixture_action": commitmentAction.ID}))
		value := finalSemanticFixtureFleetVersion{
			manifest: manifest, canonical: canonical, commitmentHash: commitmentHash, validFrom: validFrom, validTo: validTo, nativeHead: nativeHead,
			commitment: FleetCommitmentEvidence{
				Schema: fleetCommitmentEvidenceSchemaV2, DeploymentID: source.DeploymentID, PlanHash: current.PlanHash, ActionID: commitmentAction.ID, IntentHash: commitmentAction.IntentHash,
				ManifestURI: fmt.Sprintf("fleet-%d%s.json", fleet, suffix), CommitmentHash: "0x" + hex.EncodeToString(commitmentHash[:]), Hotkey: "0x" + hex.EncodeToString(manifest.Hotkey[:]),
				ExtrinsicHash: transactionHash, CommitmentBlock: nativeHead.Number, FinalizedBlock: nativeHead.Number, FinalizedBlockHash: nativeHead.Hash,
			},
		}
		files[fmt.Sprintf("public/fleet-%d%s.json", fleet, suffix)] = append([]byte(nil), canonical...)
		commitmentData, marshalErr := json.Marshal(value.commitment)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		files[fmt.Sprintf("public/fleet-%d%s.commitment.json", fleet, suffix)] = commitmentData
		return value
	}
	for fleet := 1; fleet <= int(finalFleetGenerationSetupFleetCount); fleet++ {
		versions[key(fleet, 1)] = makeVersion(fleet, 1)
		versions[key(fleet, 2)] = makeVersion(fleet, 2)
	}
	for fleet := int(finalFleetGenerationSetupFleetCount) + 1; fleet <= int(finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount); fleet++ {
		versions[key(fleet, 1)] = makeVersion(fleet, 1)
	}

	hotkeyPair := func(fleet int) *crv4.Keypair {
		role := roles.Substrate[fleetHotkeyLabel(fleet)]
		seed, seedErr := hex.DecodeString(role.SeedHex)
		if seedErr != nil || len(seed) != 32 {
			t.Fatalf("release fixture fleet %d hotkey is invalid", fleet)
		}
		var fixed [32]byte
		copy(fixed[:], seed)
		pair, pairErr := crv4.KeypairFromSeed(fixed)
		if pairErr != nil {
			t.Fatal(pairErr)
		}
		return pair
	}
	clientPrivate := func(fleet, member int) ed25519.PrivateKey {
		role := roles.Clients[fmt.Sprintf("miner-%d", fleetMemberMinerIndex(cfg, fleet, member))]
		seed, seedErr := hex.DecodeString(role.SeedHex)
		if seedErr != nil || len(seed) != ed25519.SeedSize {
			t.Fatalf("release fixture fleet %d member %d seed is invalid", fleet, member)
		}
		return ed25519.NewKeyFromSeed(seed)
	}
	for fleet := 1; fleet <= int(finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount); fleet++ {
		initial := versions[key(fleet, 1)]
		pair := hotkeyPair(fleet)
		for memberIndex, member := range initial.manifest.Members {
			memberNumber := memberIndex + 1
			binding, bindingErr := initial.manifest.Binding(member, initial.validFrom, initial.validTo)
			if bindingErr != nil {
				t.Fatal(bindingErr)
			}
			clientSignature, clientErr := binding.SignClient(clientPrivate(fleet, memberNumber))
			if clientErr != nil {
				t.Fatal(clientErr)
			}
			digest, digestErr := binding.Digest()
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			hotkeySignature, hotkeyErr := pair.Sign(digest[:])
			if hotkeyErr != nil {
				t.Fatal(hotkeyErr)
			}
			bindingAction := action(fmt.Sprintf("fleet.bind.%d.%d", fleet, memberNumber))
			bindingHead := finalSemanticFixtureChainHead(800 + uint64(fleet*10+memberNumber))
			bindingTransaction := finalFleetGenerationTestHash(2_000_000 + uint64(fleet*10+memberNumber))
			finalized(current.PlanHash, bindingAction, bindingTransaction, bindingHead)
			verified(current.PlanHash, bindingAction, postcondition(current.PlanHash, bindingAction, bindingHead, finalSemanticFixtureChainHead(900+uint64(fleet*10+memberNumber)), map[string]any{"fixture_action": bindingAction.ID}))
			evidence := FleetBindingEvidence{
				Schema: "urnetwork-fleet-binding-evidence-v1", DeploymentID: source.DeploymentID, PlanHash: current.PlanHash, ActionID: bindingAction.ID, IntentHash: bindingAction.IntentHash,
				ClientID: "0x" + hex.EncodeToString(binding.ClientID[:]), ClientKey: "0x" + hex.EncodeToString(binding.ClientKey[:]), FleetID: "0x" + hex.EncodeToString(binding.FleetID[:]), Hotkey: "0x" + hex.EncodeToString(binding.Hotkey[:]),
				Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: "0x" + hex.EncodeToString(binding.CommitmentHash[:]),
				BindingDigest: "0x" + hex.EncodeToString(digest[:]), ClientSignature: "0x" + hex.EncodeToString(clientSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature),
				TransactionHash: bindingTransaction, BlockNumber: bindingHead.Number, BlockHash: bindingHead.Hash, UID: uint16(fleet),
			}
			data, marshalErr := json.Marshal(evidence)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			files[fmt.Sprintf("public/fleet-%d-member-%d.binding.json", fleet, memberNumber)] = data
		}
		if fleet > int(finalFleetGenerationSetupFleetCount) {
			continue
		}
		refresh := versions[key(fleet, 2)]
		for memberIndex, member := range refresh.manifest.Members {
			memberNumber := memberIndex + 1
			priorBinding, priorErr := initial.manifest.Binding(initial.manifest.Members[memberIndex], initial.validFrom, initial.validTo)
			if priorErr != nil {
				t.Fatal(priorErr)
			}
			binding, bindingErr := refresh.manifest.Binding(member, refresh.validFrom, refresh.validTo)
			if bindingErr != nil {
				t.Fatal(bindingErr)
			}
			revoke := protocol.FleetRevoke{ChainID: source.ChainID, Netuid: source.Netuid, Generation: 1, EffectiveEpoch: refresh.validFrom, ClientID: binding.ClientID}
			copy(revoke.Coordinator[:], coordinator.Bytes())
			revokeSignature, revokeErr := revoke.SignClient(clientPrivate(fleet, memberNumber))
			if revokeErr != nil {
				t.Fatal(revokeErr)
			}
			bindingSignature, bindingErr := binding.SignClient(clientPrivate(fleet, memberNumber))
			if bindingErr != nil {
				t.Fatal(bindingErr)
			}
			bindingDigest, digestErr := binding.Digest()
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			hotkeySignature, hotkeyErr := pair.Sign(bindingDigest[:])
			if hotkeyErr != nil {
				t.Fatal(hotkeyErr)
			}
			revokeDigest, digestErr := revoke.Digest()
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			batch := (fleet-1)/int(finalFleetGenerationBatchSize) + 1
			refreshHead := finalSemanticFixtureChainHead(3)
			refreshTransaction := finalFleetGenerationTestHash(3_000_000 + uint64(batch))
			evidence := FleetRefreshBindingEvidence{
				Schema: fleetRefreshBindingEvidenceSchema, Fleet: fleet, Member: memberNumber,
				ClientID: "0x" + hex.EncodeToString(binding.ClientID[:]), ClientKey: "0x" + hex.EncodeToString(binding.ClientKey[:]), FleetID: "0x" + hex.EncodeToString(binding.FleetID[:]), Hotkey: "0x" + hex.EncodeToString(binding.Hotkey[:]),
				PriorGeneration: 1, PriorValidFromEpoch: priorBinding.ValidFromEpoch, PriorOriginalValidToEpoch: priorBinding.ValidToEpoch, PriorCommitmentHash: "0x" + hex.EncodeToString(priorBinding.CommitmentHash[:]),
				ReplacementGeneration: 2, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: "0x" + hex.EncodeToString(binding.CommitmentHash[:]),
				RevokeDigest: "0x" + hex.EncodeToString(revokeDigest[:]), RevokeSignature: "0x" + hex.EncodeToString(revokeSignature), BindingDigest: "0x" + hex.EncodeToString(bindingDigest[:]),
				ClientSignature: "0x" + hex.EncodeToString(bindingSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature), UID: uint16(fleet),
				TransactionHash: refreshTransaction, BlockNumber: refreshHead.Number, BlockHash: refreshHead.Hash,
			}
			data, marshalErr := json.Marshal(evidence)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			files[fmt.Sprintf("public/fleet-%d-member-%d.refresh.binding.json", fleet, memberNumber)] = data
		}
	}

	// Establish the deployment initialization transition before the temporary
	// oracle window.  It is intentionally a finalized entry only: proxy
	// construction is proved by its raw Upgraded event and timeline baseline.
	proxyAction := action("evm.coordinator-proxy")
	proxyHead := finalSemanticFixtureChainHead(1)
	proxyTransaction := finalFleetGenerationTestHash(900_001)
	finalized(current.PlanHash, proxyAction, proxyTransaction, proxyHead)
	activate := action("fleet.refresh.oracle-activate")
	awaitActive := action("fleet.refresh.oracle-await-active")
	restore := action("fleet.refresh.oracle-restore")
	awaitRestored := action("fleet.refresh.oracle-await-restored")
	activateOracle, activateErr := plannedFleetRefreshOracleTarget(activate)
	restoreOracle, restoreErr := plannedFleetRefreshOracleTarget(restore)
	if activateErr != nil || restoreErr != nil || activateOracle != batcher || restoreOracle == activateOracle {
		t.Fatalf("release fixture oracle targets are invalid: %v %v", activateErr, restoreErr)
	}
	activateHead := finalSemanticFixtureChainHead(2)
	activateTransaction := finalFleetGenerationTestHash(900_002)
	finalized(current.PlanHash, activate, activateTransaction, activateHead)
	activateObserved := map[string]any{
		"current_epoch": uint64(1), "pending_epoch": uint64(2), "immutable_oracle": strings.ToLower(restoreOracle.Hex()),
		"active_oracle": strings.ToLower(restoreOracle.Hex()), "pending_oracle": strings.ToLower(activateOracle.Hex()), "target_oracle": strings.ToLower(activateOracle.Hex()),
	}
	verified(current.PlanHash, activate, postcondition(current.PlanHash, activate, activateHead, activateHead, activateObserved))
	awaitActiveObserved := map[string]any{"target_oracle": strings.ToLower(activateOracle.Hex()), "active_oracle": strings.ToLower(activateOracle.Hex())}
	verified(current.PlanHash, awaitActive, postcondition(current.PlanHash, awaitActive, activateHead, activateHead, awaitActiveObserved))

	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		installAction := action(finalFleetGenerationActionID(1, batch))
		installHead := finalSemanticFixtureChainHead(100 + batch)
		installTransaction := finalFleetGenerationTestHash(2_500_000 + batch)
		finalized(current.PlanHash, installAction, installTransaction, installHead)
		verified(current.PlanHash, installAction, postcondition(current.PlanHash, installAction, finalSemanticFixtureChainHead(500+batch), installHead, map[string]any{"fixture_action": installAction.ID}))
		first := int((batch-1)*finalFleetGenerationBatchSize + 1)
		last := first + int(finalFleetGenerationBatchSize) - 1
		members := make([]string, 0, finalFleetGenerationBatchSize*finalFleetGenerationMembersPerFleet)
		installed := make([]int, 0, finalFleetGenerationBatchSize)
		for fleet := first; fleet <= last; fleet++ {
			installed = append(installed, fleet)
			for member := 1; member <= int(finalFleetGenerationMembersPerFleet); member++ {
				members = append(members, fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
			}
		}
		install := FleetInstallBatchEvidence{Schema: fleetInstallBatchEvidenceSchema, Batch: int(batch), FirstFleet: first, LastFleet: last, Generation: 1, EffectiveEpoch: 10, ValidToEpoch: 20, InstalledFleets: installed, MemberEvidence: members, TransactionHash: installTransaction, BlockNumber: installHead.Number, BlockHash: installHead.Hash}
		data, marshalErr := json.Marshal(install)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		files[fmt.Sprintf("public/fleet-install-batch-%d.json", batch)] = data

		refreshAction := action(finalFleetGenerationActionID(2, batch))
		refreshHead := finalSemanticFixtureChainHead(3)
		refreshTransaction := finalFleetGenerationTestHash(3_000_000 + batch)
		finalized(current.PlanHash, refreshAction, refreshTransaction, refreshHead)
		verified(current.PlanHash, refreshAction, postcondition(current.PlanHash, refreshAction, finalSemanticFixtureChainHead(700+batch), refreshHead, map[string]any{"fixture_action": refreshAction.ID}))
		refreshMembers := make([]string, 0, finalFleetGenerationBatchSize*finalFleetGenerationMembersPerFleet)
		for fleet := first; fleet <= last; fleet++ {
			for member := 1; member <= int(finalFleetGenerationMembersPerFleet); member++ {
				refreshMembers = append(refreshMembers, fmt.Sprintf("fleet-%d-member-%d.refresh.binding.json", fleet, member))
			}
		}
		refresh := FleetRefreshBatchEvidence{Schema: fleetRefreshBatchEvidenceSchema, Batch: int(batch), FirstFleet: first, LastFleet: last, Generation: 2, EffectiveEpoch: 11, ValidToEpoch: 21, FleetCount: int(finalFleetGenerationBatchSize), MemberCount: len(refreshMembers), MemberEvidence: refreshMembers, TransactionHash: refreshTransaction, BlockNumber: refreshHead.Number, BlockHash: refreshHead.Hash}
		data, marshalErr = json.Marshal(refresh)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		files[fmt.Sprintf("public/fleet-refresh-batch-%d.json", batch)] = data
	}
	restoreHead := finalSemanticFixtureChainHead(3)
	restoreTransaction := finalFleetGenerationTestHash(900_003)
	finalized(current.PlanHash, restore, restoreTransaction, restoreHead)
	restoreObserved := map[string]any{
		"current_epoch": uint64(2), "pending_epoch": uint64(3), "immutable_oracle": strings.ToLower(restoreOracle.Hex()),
		"active_oracle": strings.ToLower(activateOracle.Hex()), "pending_oracle": strings.ToLower(restoreOracle.Hex()), "target_oracle": strings.ToLower(restoreOracle.Hex()),
	}
	verified(current.PlanHash, restore, postcondition(current.PlanHash, restore, restoreHead, restoreHead, restoreObserved))
	awaitRestoredObserved := map[string]any{"target_oracle": strings.ToLower(restoreOracle.Hex()), "active_oracle": strings.ToLower(restoreOracle.Hex())}
	verified(current.PlanHash, awaitRestored, postcondition(current.PlanHash, awaitRestored, restoreHead, restoreHead, awaitRestoredObserved))

	// Challenger registrations already exist in the lifecycle transition graph.
	// Add their direct predecessor verified records so ordinary-fleet replay
	// reaches the same native registration receipts rather than a mock copy.
	for _, transition := range source.HeadTransitions {
		fleet := int(transition.ChallengerFleetID)
		registrationAction := action(fmt.Sprintf("fleet.register.%d", fleet))
		proofData, proofFound := artifacts[transition.Registration.Proof.URI]
		if !proofFound {
			t.Fatalf("release fixture challenger %d registration proof is absent", fleet)
		}
		var record ActionPostcondition
		if err := json.Unmarshal(proofData, &record); err != nil || record.PlanHash != prior.PlanHash || record.ActionID != registrationAction.ID || record.IntentHash != registrationAction.IntentHash {
			t.Fatalf("release fixture challenger %d carried postcondition differs: %v", fleet, err)
		}
		verified(prior.PlanHash, registrationAction, &record)
	}

	// Re-encode the now complete journal before calculating direct batch
	// calldata.  The source builder authenticates every signature and finality
	// edge while rebuilding the two ABI payload families.
	journalData := finalSemanticFixtureJournalBytes(t, entries)
	files["launch-foundation/journal.jsonl"] = journalData
	archive := &finalSemanticArchive{
		cfg: cfg, files: files, collected: &FinalSemanticCollectedInputs{Policy: source.PolicyArtifact},
		artifactDeriver: func(kind, name string, data []byte) (FinalArtifactLocator, error) {
			artifacts[name] = append([]byte(nil), data...)
			return FinalArtifactLocator{Kind: kind, URI: name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}, nil
		},
	}
	preChain := &FinalCollectedChainSnapshot{FleetBatcher: strings.ToLower(batcher.Hex())}
	preEvents := &finalSemanticEventIndex{byName: map[string][]finalSemanticEvent{}, byTx: map[string][]finalCanonicalEVMLog{}}
	generationSource, contextErr := newFinalFleetGenerationSource(archive, source, preChain, preEvents)
	if contextErr != nil {
		t.Fatalf("open release fixture ordinary-fleet source: %v", contextErr)
	}
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		first := (batch-1)*finalFleetGenerationBatchSize + 1
		last := first + finalFleetGenerationBatchSize - 1
		installed := make([]uint64, 0, finalFleetGenerationBatchSize)
		for fleet := first; fleet <= last; fleet++ {
			installed = append(installed, fleet)
		}
		calldata, calldataErr := generationSource.installCalldata(FinalFleetGenerationBatchEvidence{Batch: batch, Generation: 1}, installed)
		if calldataErr != nil {
			t.Fatalf("release fixture install batch %d calldata: %v", batch, calldataErr)
		}
		var install FleetInstallBatchEvidence
		path := fmt.Sprintf("public/fleet-install-batch-%d.json", batch)
		if err := json.Unmarshal(files[path], &install); err != nil {
			t.Fatal(err)
		}
		install.CalldataHash = common.BytesToHash(crypto.Keccak256(calldata)).Hex()
		data, marshalErr := json.Marshal(install)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		files[path] = data

		var refresh FleetRefreshBatchEvidence
		refreshPath := fmt.Sprintf("public/fleet-refresh-batch-%d.json", batch)
		if err := json.Unmarshal(files[refreshPath], &refresh); err != nil {
			t.Fatal(err)
		}
		refreshData, refreshErr := generationSource.refreshCalldata(FinalFleetGenerationBatchEvidence{Batch: batch, Generation: 2, FirstFleet: first, LastFleet: last}, refresh)
		if refreshErr != nil {
			t.Fatalf("release fixture refresh batch %d calldata: %v", batch, refreshErr)
		}
		refresh.CalldataHash = common.BytesToHash(crypto.Keccak256(refreshData)).Hex()
		data, marshalErr = json.Marshal(refresh)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		files[refreshPath] = data
	}

	logs := make([]finalCanonicalEVMLog, 0, 5_000)
	coordinatorABI, _, abiErr := finalFleetGenerationABIs()
	if abiErr != nil {
		t.Fatal(abiErr)
	}
	upgraded := coordinatorABI.Events["Upgraded"]
	logs = append(logs, finalSemanticFixtureRawLog(strings.ToLower(coordinator.Hex()), upgraded.ID.Hex(), []common.Hash{common.BytesToHash(current.Deployment.CoordinatorImplementation.Bytes())}, nil, proxyHead, proxyTransaction, 0, 0))
	scheduled := coordinatorABI.Events["CommitmentOracleScheduled"]
	logs = append(logs, finalSemanticFixtureRawLog(strings.ToLower(coordinator.Hex()), scheduled.ID.Hex(), []common.Hash{common.BytesToHash(activateOracle.Bytes()), common.BigToHash(big.NewInt(2))}, nil, activateHead, activateTransaction, 0, 0))
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		installAction := finalFleetGenerationActionID(1, batch)
		installVersion := versions[key(int((batch-1)*finalFleetGenerationBatchSize+1), 1)]
		installHead := finalSemanticFixtureChainHead(100 + batch)
		installTransaction := finalFleetGenerationTestHash(2_500_000 + batch)
		for fleet := int((batch-1)*finalFleetGenerationBatchSize + 1); fleet <= int(batch*finalFleetGenerationBatchSize); fleet++ {
			version := versions[key(fleet, 1)]
			logs = append(logs, finalSemanticFixtureFleetEvents(t, source, installAction, version, uint16(fleet), strings.ToLower(batcher.Hex()), installHead, installTransaction, 0, uint64((fleet-int((batch-1)*finalFleetGenerationBatchSize+1))*10))...)
		}
		_ = installVersion
		refreshAction := finalFleetGenerationActionID(2, batch)
		refreshHead := finalSemanticFixtureChainHead(3)
		refreshTransaction := finalFleetGenerationTestHash(3_000_000 + batch)
		for fleet := int((batch-1)*finalFleetGenerationBatchSize + 1); fleet <= int(batch*finalFleetGenerationBatchSize); fleet++ {
			version := versions[key(fleet, 2)]
			logs = append(logs, finalSemanticFixtureFleetEvents(t, source, refreshAction, version, uint16(fleet), strings.ToLower(batcher.Hex()), refreshHead, refreshTransaction, batch-1, uint64((fleet-int((batch-1)*finalFleetGenerationBatchSize+1))*14))...)
		}
	}
	logs = append(logs, finalSemanticFixtureRawLog(strings.ToLower(coordinator.Hex()), scheduled.ID.Hex(), []common.Hash{common.BytesToHash(restoreOracle.Bytes()), common.BigToHash(big.NewInt(3))}, nil, restoreHead, restoreTransaction, finalFleetGenerationBatchCount, finalFleetGenerationBatchCount*1_000))
	canonicalLogs, canonicalErr := finalCanonicalizeLogs(logs)
	if canonicalErr != nil {
		t.Fatalf("canonicalize release fixture EVM logs: %v", canonicalErr)
	}

	publicDeployment := current.Deployment
	publicDeployment.DeployBlock, publicDeployment.DeployBlockHash = proxyHead.Number, proxyHead.Hash
	public := PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", Release: "1.0", DeploymentID: source.DeploymentID, GeneratedAt: source.CampaignStartedAt,
		ChainID: source.ChainID, GenesisHash: source.GenesisHash, Netuid: source.Netuid, ConfigHash: source.ConfigHash, PolicyHash: source.PolicyHash, PlanHash: source.PlanHash,
		Contracts: &publicDeployment, CoordinatorUpgrade: current.CoordinatorUpgrade, Topology: cfg.Config.Topology,
	}
	publicData, marshalErr := json.Marshal(public)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	files["launch-foundation/public.json"] = publicData
	plans := map[string]*SetupPlan{strings.ToLower(current.PlanHash): current, strings.ToLower(prior.PlanHash): &prior}
	census, censusErr := finalCaptureReleaseContractCensusForLineage(current, &publicDeployment, batcher, plans, entries)
	if censusErr != nil {
		t.Fatalf("release fixture contract census: %v", censusErr)
	}
	proxyRuntime := strings.ToLower(current.Deployment.RuntimeHashes[current.Deployment.CoordinatorProxy.Hex()])
	implementationRuntime := strings.ToLower(current.Deployment.RuntimeHashes[current.Deployment.CoordinatorImplementation.Hex()])
	chain := &FinalCollectedChainSnapshot{
		Schema: "urnetwork-final-collected-chain-v1", Phase: source.Phase, RunID: source.RunID, DeploymentID: source.DeploymentID,
		FleetBatcher: strings.ToLower(batcher.Hex()), EVMFromBlock: census.fromBlock, CurrentReleaseFromBlock: publicDeployment.DeployBlock,
		EVMHead: source.EVMTerminalHead, CurrentReleaseAddresses: census.currentAddresses, ReleaseContractAddresses: census.releaseAddresses,
		EVMLogs: canonicalLogs, NativeHead: source.NativeTerminalHead,
		CoordinatorBaselines: []FinalCollectedCoordinatorBaseline{{Proxy: strings.ToLower(coordinator.Hex()), Head: proxyHead, Implementation: strings.ToLower(current.Deployment.CoordinatorImplementation.Hex()), ImplementationRuntimeHash: implementationRuntime, ProxyRuntimeHash: proxyRuntime}},
	}
	activateInput, activatePackErr := stabi.NewSTCoordinator().TryPackScheduleCommitmentOracle(activateOracle, 2)
	restoreInput, restorePackErr := stabi.NewSTCoordinator().TryPackScheduleCommitmentOracle(restoreOracle, 3)
	if activatePackErr != nil || restorePackErr != nil {
		t.Fatalf("release fixture oracle calldata: %v %v", activatePackErr, restorePackErr)
	}
	chain.EVMTransactions = []FinalCollectedEVMTransaction{
		{TransactionHash: activateTransaction, Block: activateHead, From: strings.ToLower(current.Roles.Owner), To: strings.ToLower(coordinator.Hex()), Input: "0x" + hex.EncodeToString(activateInput), ValueWei: "0"},
		{TransactionHash: restoreTransaction, Block: restoreHead, From: strings.ToLower(current.Roles.Owner), To: strings.ToLower(coordinator.Hex()), Input: "0x" + hex.EncodeToString(restoreInput), ValueWei: "0"},
	}
	events, eventErr := indexFinalSemanticEvents(chain)
	if eventErr != nil {
		t.Fatalf("index release fixture EVM events: %v", eventErr)
	}
	if err := archive.buildFleetGeneration(source, chain, events); err != nil {
		t.Fatalf("build release fixture ordinary fleets: %v", err)
	}
	sealedFiles, err := finalFleetGenerationArtifactFiles(source, artifacts[source.FleetGeneration.Artifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	checkPostcondition := func(action FinalFleetGenerationActionEvidence, proof FinalArtifactLocator) {
		path, err := postconditionRelativePath(action.PlanHash, action.ActionID)
		proofData, proofFound := artifacts[proof.URI]
		if err != nil || !proofFound || len(proofData) == 0 || string(sealedFiles[path]) != string(proofData) {
			t.Fatalf("release fixture action %s consumed a postcondition absent from sealed path %s: %v", action.ActionID, path, err)
		}
	}
	commitments := 0
	for _, fleet := range source.FleetGeneration.SetupFleets {
		for _, version := range []FinalFleetGenerationVersionEvidence{fleet.Initial, fleet.Refresh} {
			checkPostcondition(version.CommitmentAction, version.CommitmentPostcondition)
			commitments++
		}
	}
	for _, challenger := range source.FleetGeneration.ChallengerFleets {
		checkPostcondition(challenger.Initial.CommitmentAction, challenger.Initial.CommitmentPostcondition)
		commitments++
		var registration ActionPostcondition
		if err := json.Unmarshal(artifacts[challenger.Registration.Proof.URI], &registration); err != nil {
			t.Fatal(err)
		}
		checkPostcondition(FinalFleetGenerationActionEvidence{ActionID: registration.ActionID, PlanHash: registration.PlanHash, IntentHash: registration.IntentHash}, challenger.Registration.Proof)
	}
	if commitments != 402 {
		t.Fatalf("release fixture sealed %d commitment postconditions, want 402", commitments)
	}
	for _, batch := range source.FleetGeneration.Batches {
		for _, write := range batch.CarriedHistory {
			checkPostcondition(write.Action, write.Postcondition)
		}
		if batch.BatchWrite != nil {
			checkPostcondition(batch.BatchWrite.Action, batch.BatchWrite.Postcondition)
		}
	}
	if err := archive.buildHistoricalCoordinatorReceipts(source, chain, events); err != nil {
		t.Fatalf("build release fixture coordinator chronology: %v", err)
	}
	if source.FleetGeneration == nil || source.FleetRefreshOracleWindow.Artifact.URI == "" || len(source.HistoricalCoordinatorTimeline) == 0 {
		t.Fatal("release fixture mandatory fleet/chronology evidence was not built")
	}
	// Compare all nested locators independently, then replay only the exact
	// bytes selected by the real loader. Passing the entire source store here
	// would conceal omissions in the verifier's load census.
	uses, err := finalSemanticArtifactUses(source)
	if err != nil {
		t.Fatal(err)
	}
	assertFinalSemanticArtifactCensus(t, source, uses)
	cache, err := loadFinalSemanticArtifactUses(context.Background(), uses, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, found := artifacts[locator.URI]
		if !found {
			return nil, fmt.Errorf("release fixture selected artifact %s is absent", locator.URI)
		}
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorReceiptArtifacts(source, current, cache); err != nil {
		t.Fatalf("replay release fixture coordinator chronology census: %v", err)
	}
}

// Rebuilds a canonical journal wire after deterministic fixture entries are
// appended.  Using the production decoder immediately afterwards catches an
// accidental hash-chain or terminal-stage violation at its creation point.
func finalSemanticFixtureJournalBytes(t *testing.T, entries []JournalEntry) []byte {
	t.Helper()
	var result []byte
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, data...)
		result = append(result, '\n')
	}
	if _, err := decodeFinalSemanticJournalBytes(result); err != nil {
		t.Fatalf("release fixture journal is invalid: %v", err)
	}
	return result
}

// Encodes a minimal ABI log with an exact event topic and canonical chain
// coordinate.  The two manually produced coordinator events use no data;
// fleet events below are encoded through the shared ABI decoder helper.
func finalSemanticFixtureRawLog(address, topic string, indexed []common.Hash, data []byte, head ChainHead, transactionHash string, transactionIndex, logIndex uint64) finalCanonicalEVMLog {
	topics := make([]string, 1, len(indexed)+1)
	topics[0] = strings.ToLower(topic)
	for _, value := range indexed {
		topics = append(topics, strings.ToLower(value.Hex()))
	}
	return finalCanonicalEVMLog{
		Address: address, Topics: topics, Data: "0x" + hex.EncodeToString(data), BlockNumber: head.Number, BlockHash: head.Hash,
		TransactionHash: transactionHash, TransactionIndex: transactionIndex, LogIndex: logIndex,
	}
}

// Emits the exact production event order for one installed or refreshed
// fleet.  The shared decoder constructs ABI-valid records; only the chain
// transaction index is supplied here because the helper's small unit fixtures
// intentionally use index zero.
func finalSemanticFixtureFleetEvents(t *testing.T, source *FinalSemanticEvidence, actionID string, version finalSemanticFixtureFleetVersion, uid uint16, batcher string, head ChainHead, transactionHash string, transactionIndex, logBase uint64) []finalCanonicalEVMLog {
	t.Helper()
	logs := make([]finalCanonicalEVMLog, 0, 14)
	appendEvent := func(name string, member protocol.FleetMember, logIndex uint64) {
		values := finalFleetGenerationArtifactTestValues{
			hotkey: version.manifest.Hotkey, commitment: version.commitmentHash, fleet: version.manifest.FleetID, client: member.ClientID,
			nativeHead: version.nativeHead, generation: version.manifest.Generation, validFrom: version.validFrom, validTo: version.validTo, uid: uid,
		}
		event := finalFleetGenerationArtifactTestEvent(t, source, actionID, name, values, batcher, head, transactionHash, logIndex)
		event.Log.TransactionIndex = transactionIndex
		logs = append(logs, event.Log)
	}
	appendEvent("CommitmentMirrored", version.manifest.Members[0], logBase)
	for index, member := range version.manifest.Members {
		base := logBase + 1 + uint64(index)*2
		if version.manifest.Generation == 2 {
			base = logBase + 1 + uint64(index)*3
			appendEvent("FleetBindingRevoked", member, base)
			base++
		}
		appendEvent("FleetBound", member, base)
		appendEvent("FleetMemberBound", member, base+1)
	}
	if version.manifest.Generation == 1 {
		appendEvent("FleetInstalled", version.manifest.Members[0], logBase+9)
	} else {
		appendEvent("FleetRefreshed", version.manifest.Members[0], logBase+13)
	}
	return logs
}
