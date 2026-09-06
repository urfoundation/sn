// Preserve the pre-parallel serial fleet preparation as an independent test
// reference. It performs no full measurement, terminal or envelope resealing.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

// The old serial traversal retains its original monotonic native counter.
// Only publication callbacks are replaced by recording their exact arguments.
func finalSemanticFixtureFleetSerialReference(t *testing.T, source finalSemanticFixtureFleetContext) ([]finalSemanticFixtureFleetJob, []finalSemanticFixtureFleetMutation, []finalSemanticFixtureFleetMutation) {
	t.Helper()
	cfg, roles, coordinator := source.cfg, source.roles, source.coordinator
	files := map[string][]byte{}
	var commitments, bindings []finalSemanticFixtureFleetMutation
	action := func(id string) Action {
		value, exists := source.actions[id]
		if !exists || value.ID != id {
			t.Fatalf("serial fleet reference lacks action %s", id)
		}
		return value
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
		manifest := protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: source.chainID, Netuid: source.netuid, FleetID: fleetID, Hotkey: hotkey, Generation: generation}
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
		commitments = append(commitments, finalSemanticFixtureFleetMutation{actionID: commitmentAction.ID, transactionHash: transactionHash, nativeHead: nativeHead, evmHead: finalSemanticFixtureChainHead(100 + nativeCounter)})
		value := finalSemanticFixtureFleetVersion{
			manifest: manifest, canonical: canonical, commitmentHash: commitmentHash, validFrom: validFrom, validTo: validTo, nativeHead: nativeHead,
			commitment: FleetCommitmentEvidence{
				Schema: fleetCommitmentEvidenceSchemaV2, DeploymentID: source.deploymentID, PlanHash: source.planHash, ActionID: commitmentAction.ID, IntentHash: commitmentAction.IntentHash,
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
			bindings = append(bindings, finalSemanticFixtureFleetMutation{actionID: bindingAction.ID, transactionHash: bindingTransaction, nativeHead: bindingHead, evmHead: finalSemanticFixtureChainHead(900 + uint64(fleet*10+memberNumber))})
			evidence := FleetBindingEvidence{
				Schema: "urnetwork-fleet-binding-evidence-v1", DeploymentID: source.deploymentID, PlanHash: source.planHash, ActionID: bindingAction.ID, IntentHash: bindingAction.IntentHash,
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
			revoke := protocol.FleetRevoke{ChainID: source.chainID, Netuid: source.netuid, Generation: 1, EffectiveEpoch: refresh.validFrom, ClientID: binding.ClientID}
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

	jobs := make([]finalSemanticFixtureFleetJob, finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount)
	commitmentIndex := 0
	for fleet := uint64(1); fleet <= uint64(len(jobs)); fleet++ {
		job := finalSemanticFixtureFleetJob{fleet: fleet, files: map[string][]byte{}}
		job.versions = append(job.versions, versions[key(int(fleet), 1)])
		if fleet <= finalFleetGenerationSetupFleetCount {
			job.versions = append(job.versions, versions[key(int(fleet), 2)])
		}
		job.commitments = append([]finalSemanticFixtureFleetMutation(nil), commitments[commitmentIndex:commitmentIndex+len(job.versions)]...)
		commitmentIndex += len(job.versions)
		bindingIndex := (fleet - 1) * finalFleetGenerationMembersPerFleet
		job.bindings = append([]finalSemanticFixtureFleetMutation(nil), bindings[bindingIndex:bindingIndex+finalFleetGenerationMembersPerFleet]...)
		for generation := range job.versions {
			suffix := ""
			if generation == 1 {
				suffix = ".refresh"
			}
			for _, name := range []string{fmt.Sprintf("public/fleet-%d%s.json", fleet, suffix), fmt.Sprintf("public/fleet-%d%s.commitment.json", fleet, suffix)} {
				job.files[name] = files[name]
			}
			for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
				name := fmt.Sprintf("public/fleet-%d-member-%d%s.binding.json", fleet, member, suffix)
				job.files[name] = files[name]
			}
		}
		jobs[fleet-1] = job
	}
	return jobs, commitments, bindings
}
