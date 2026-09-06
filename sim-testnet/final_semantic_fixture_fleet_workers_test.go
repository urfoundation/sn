// Each fleet preparation owns its manifests, signatures and pending journal
// facts. Only a joined caller may assign journal positions or publish files.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

const finalSemanticFixtureFleetAssembly = "generation-fleet-assembly"

// These coordinates contain no journal sequence, previous hash or mutable
// postcondition map. The caller assigns those in the original canonical order.
type finalSemanticFixtureFleetMutation struct {
	actionID        string
	transactionHash string
	nativeHead      ChainHead
	evmHead         ChainHead
}

// One bounded fleet owns at most two versions, eight signed bindings and six
// mutation facts. No keypair or private key leaves its preparation owner.
type finalSemanticFixtureFleetJob struct {
	fleet       uint64
	versions    []finalSemanticFixtureFleetVersion
	files       map[string][]byte
	commitments []finalSemanticFixtureFleetMutation
	bindings    []finalSemanticFixtureFleetMutation
}

// Fixed context is copied before dispatch; config, role and action maps are
// read-only for the joined lifetime and must never be caller-mutated meanwhile.
type finalSemanticFixtureFleetContext struct {
	cfg          *ResolvedConfig
	roles        *RoleSecrets
	actions      map[string]Action
	deploymentID string
	planHash     string
	chainID      uint64
	netuid       uint16
	coordinator  common.Address
}

// Prepares exactly the old initial-to-refresh work for one fleet, returning
// errors rather than terminating a test goroutine. It does not publish output.
func prepareFinalSemanticFixtureFleet(ctx context.Context, source finalSemanticFixtureFleetContext, fleet uint64, work finalSemanticFixtureWorkControl, index int) (finalSemanticFixtureFleetJob, error) {
	job := finalSemanticFixtureFleetJob{fleet: fleet, files: map[string][]byte{}}
	if ctx == nil || source.cfg == nil || source.cfg.Config == nil || source.cfg.Config.Topology.ClientsPerHeadFleet != int(finalFleetGenerationMembersPerFleet) || source.roles == nil || source.actions == nil || source.deploymentID == "" || source.planHash == "" || source.coordinator == (common.Address{}) || fleet == 0 || fleet > finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount {
		return job, errors.New("fixture fleet preparation context is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return job, err
	}
	action := func(id string) (Action, error) {
		value, exists := source.actions[id]
		if !exists || value.ID != id {
			return Action{}, fmt.Errorf("fixture fleet plan lacks action %s", id)
		}
		return value, nil
	}
	makeVersion := func(generation uint64) (finalSemanticFixtureFleetVersion, error) {
		hotkey, err := roleBytes32(source.roles, fleetHotkeyLabel(int(fleet)))
		if err != nil {
			return finalSemanticFixtureFleetVersion{}, err
		}
		manifest := protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: source.chainID, Netuid: source.netuid, FleetID: derive32(source.cfg, fmt.Sprintf("fleet-id/%d", fleet)), Hotkey: hotkey, Generation: generation}
		copy(manifest.Coordinator[:], source.coordinator.Bytes())
		for member := 1; member <= int(finalFleetGenerationMembersPerFleet); member++ {
			client := source.roles.Clients[fmt.Sprintf("miner-%d", fleetMemberMinerIndex(source.cfg, int(fleet), member))]
			id, idErr := hex.DecodeString(client.ClientIDHex)
			key, keyErr := hex.DecodeString(client.PublicKeyHex)
			if idErr != nil || keyErr != nil || len(id) != 16 || len(key) != ed25519.PublicKeySize {
				return finalSemanticFixtureFleetVersion{}, fmt.Errorf("fixture fleet %d member %d identity is invalid", fleet, member)
			}
			var value protocol.FleetMember
			copy(value.ClientID[:], id)
			copy(value.ClientKey[:], key)
			manifest.Members = append(manifest.Members, value)
		}
		canonical, err := manifest.Canonical()
		if err != nil {
			return finalSemanticFixtureFleetVersion{}, err
		}
		commitmentHash, err := manifest.CommitmentHash()
		if err != nil {
			return finalSemanticFixtureFleetVersion{}, err
		}
		validFrom, validTo := uint64(10), uint64(20)
		suffix := ""
		actionID := fmt.Sprintf("fleet.commitment.%d", fleet)
		// Original serial order is two generations for each of the 200 setup
		// fleets, then one generation for each of the two challengers.
		nativeNumber := uint64(300) + (fleet-1)*2 + generation
		if fleet > finalFleetGenerationSetupFleetCount {
			nativeNumber = 300 + finalFleetGenerationSetupFleetCount*2 + fleet - finalFleetGenerationSetupFleetCount
		}
		if generation == 2 {
			validFrom, validTo = 11, 21
			suffix = ".refresh"
			actionID = fmt.Sprintf("fleet.refresh.commitment.%d", fleet)
		}
		approved, err := action(actionID)
		if err != nil {
			return finalSemanticFixtureFleetVersion{}, err
		}
		nativeHead := finalSemanticFixtureChainHead(nativeNumber)
		transactionHash := finalFleetGenerationTestHash(1_000_000 + nativeNumber)
		value := finalSemanticFixtureFleetVersion{
			manifest: manifest, canonical: canonical, commitmentHash: commitmentHash, validFrom: validFrom, validTo: validTo, nativeHead: nativeHead,
			commitment: FleetCommitmentEvidence{
				Schema: fleetCommitmentEvidenceSchemaV2, DeploymentID: source.deploymentID, PlanHash: source.planHash, ActionID: approved.ID, IntentHash: approved.IntentHash,
				ManifestURI: fmt.Sprintf("fleet-%d%s.json", fleet, suffix), CommitmentHash: "0x" + hex.EncodeToString(commitmentHash[:]), Hotkey: "0x" + hex.EncodeToString(manifest.Hotkey[:]),
				ExtrinsicHash: transactionHash, CommitmentBlock: nativeHead.Number, FinalizedBlock: nativeHead.Number, FinalizedBlockHash: nativeHead.Hash,
			},
		}
		data, err := json.Marshal(value.commitment)
		if err != nil {
			return finalSemanticFixtureFleetVersion{}, err
		}
		job.files[fmt.Sprintf("public/fleet-%d%s.json", fleet, suffix)] = append([]byte(nil), canonical...)
		job.files[fmt.Sprintf("public/fleet-%d%s.commitment.json", fleet, suffix)] = data
		job.commitments = append(job.commitments, finalSemanticFixtureFleetMutation{actionID: approved.ID, transactionHash: transactionHash, nativeHead: nativeHead, evmHead: finalSemanticFixtureChainHead(100 + nativeNumber)})
		return value, nil
	}
	initial, err := makeVersion(1)
	if err != nil {
		return job, err
	}
	job.versions = append(job.versions, initial)
	if fleet <= finalFleetGenerationSetupFleetCount {
		refresh, err := makeVersion(2)
		if err != nil {
			return job, err
		}
		job.versions = append(job.versions, refresh)
	}
	role := source.roles.Substrate[fleetHotkeyLabel(int(fleet))]
	seedBytes, err := hex.DecodeString(role.SeedHex)
	if err != nil || len(seedBytes) != 32 {
		return job, fmt.Errorf("fixture fleet %d hotkey is invalid", fleet)
	}
	var seed [32]byte
	copy(seed[:], seedBytes)
	pair, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return job, err
	}
	clientPrivate := func(member int) (ed25519.PrivateKey, error) {
		client := source.roles.Clients[fmt.Sprintf("miner-%d", fleetMemberMinerIndex(source.cfg, int(fleet), member))]
		seed, err := hex.DecodeString(client.SeedHex)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("fixture fleet %d member %d seed is invalid", fleet, member)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	stageWork := work
	stageWork.ctx = ctx
	// This observes the real signing body after manifest/key preparation;
	// it deliberately does not represent total job CPU attribution.
	leave, err := stageWork.enter(finalSemanticFixtureFleetAssembly, index)
	if err != nil {
		return job, err
	}
	defer leave()
	for memberIndex, member := range initial.manifest.Members {
		if err := ctx.Err(); err != nil {
			return job, err
		}
		memberNumber := memberIndex + 1
		binding, err := initial.manifest.Binding(member, initial.validFrom, initial.validTo)
		if err != nil {
			return job, err
		}
		privateKey, err := clientPrivate(memberNumber)
		if err != nil {
			return job, err
		}
		clientSignature, err := binding.SignClient(privateKey)
		if err != nil {
			return job, err
		}
		digest, err := binding.Digest()
		if err != nil {
			return job, err
		}
		hotkeySignature, err := pair.Sign(digest[:])
		if err != nil {
			return job, err
		}
		approved, err := action(fmt.Sprintf("fleet.bind.%d.%d", fleet, memberNumber))
		if err != nil {
			return job, err
		}
		head := finalSemanticFixtureChainHead(800 + fleet*10 + uint64(memberNumber))
		transactionHash := finalFleetGenerationTestHash(2_000_000 + fleet*10 + uint64(memberNumber))
		value := FleetBindingEvidence{
			Schema: "urnetwork-fleet-binding-evidence-v1", DeploymentID: source.deploymentID, PlanHash: source.planHash, ActionID: approved.ID, IntentHash: approved.IntentHash,
			ClientID: "0x" + hex.EncodeToString(binding.ClientID[:]), ClientKey: "0x" + hex.EncodeToString(binding.ClientKey[:]), FleetID: "0x" + hex.EncodeToString(binding.FleetID[:]), Hotkey: "0x" + hex.EncodeToString(binding.Hotkey[:]),
			Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: "0x" + hex.EncodeToString(binding.CommitmentHash[:]),
			BindingDigest: "0x" + hex.EncodeToString(digest[:]), ClientSignature: "0x" + hex.EncodeToString(clientSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature),
			TransactionHash: transactionHash, BlockNumber: head.Number, BlockHash: head.Hash, UID: uint16(fleet),
		}
		data, err := json.Marshal(value)
		if err != nil {
			return job, err
		}
		job.files[fmt.Sprintf("public/fleet-%d-member-%d.binding.json", fleet, memberNumber)] = data
		job.bindings = append(job.bindings, finalSemanticFixtureFleetMutation{actionID: approved.ID, transactionHash: transactionHash, nativeHead: head, evmHead: finalSemanticFixtureChainHead(900 + fleet*10 + uint64(memberNumber))})
	}
	if fleet <= finalFleetGenerationSetupFleetCount {
		refresh := job.versions[1]
		for memberIndex, member := range refresh.manifest.Members {
			if err := ctx.Err(); err != nil {
				return job, err
			}
			memberNumber := memberIndex + 1
			priorBinding, err := initial.manifest.Binding(initial.manifest.Members[memberIndex], initial.validFrom, initial.validTo)
			if err != nil {
				return job, err
			}
			binding, err := refresh.manifest.Binding(member, refresh.validFrom, refresh.validTo)
			if err != nil {
				return job, err
			}
			revoke := protocol.FleetRevoke{ChainID: source.chainID, Netuid: source.netuid, Generation: 1, EffectiveEpoch: refresh.validFrom, ClientID: binding.ClientID}
			copy(revoke.Coordinator[:], source.coordinator.Bytes())
			privateKey, err := clientPrivate(memberNumber)
			if err != nil {
				return job, err
			}
			revokeSignature, err := revoke.SignClient(privateKey)
			if err != nil {
				return job, err
			}
			privateKey, err = clientPrivate(memberNumber)
			if err != nil {
				return job, err
			}
			bindingSignature, err := binding.SignClient(privateKey)
			if err != nil {
				return job, err
			}
			bindingDigest, err := binding.Digest()
			if err != nil {
				return job, err
			}
			hotkeySignature, err := pair.Sign(bindingDigest[:])
			if err != nil {
				return job, err
			}
			revokeDigest, err := revoke.Digest()
			if err != nil {
				return job, err
			}
			batch := (fleet-1)/finalFleetGenerationBatchSize + 1
			head := finalSemanticFixtureChainHead(3)
			transactionHash := finalFleetGenerationTestHash(3_000_000 + batch)
			value := FleetRefreshBindingEvidence{
				Schema: fleetRefreshBindingEvidenceSchema, Fleet: int(fleet), Member: memberNumber,
				ClientID: "0x" + hex.EncodeToString(binding.ClientID[:]), ClientKey: "0x" + hex.EncodeToString(binding.ClientKey[:]), FleetID: "0x" + hex.EncodeToString(binding.FleetID[:]), Hotkey: "0x" + hex.EncodeToString(binding.Hotkey[:]),
				PriorGeneration: 1, PriorValidFromEpoch: priorBinding.ValidFromEpoch, PriorOriginalValidToEpoch: priorBinding.ValidToEpoch, PriorCommitmentHash: "0x" + hex.EncodeToString(priorBinding.CommitmentHash[:]),
				ReplacementGeneration: 2, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: "0x" + hex.EncodeToString(binding.CommitmentHash[:]),
				RevokeDigest: "0x" + hex.EncodeToString(revokeDigest[:]), RevokeSignature: "0x" + hex.EncodeToString(revokeSignature), BindingDigest: "0x" + hex.EncodeToString(bindingDigest[:]),
				ClientSignature: "0x" + hex.EncodeToString(bindingSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature), UID: uint16(fleet),
				TransactionHash: transactionHash, BlockNumber: head.Number, BlockHash: head.Hash,
			}
			data, err := json.Marshal(value)
			if err != nil {
				return job, err
			}
			job.files[fmt.Sprintf("public/fleet-%d-member-%d.refresh.binding.json", fleet, memberNumber)] = data
		}
	}
	return job, ctx.Err()
}

// Dispatch owns a fixed four-wide batch; every admitted preparation is joined
// before any result is returned, and an error returns no publishable slots.
func prepareFinalSemanticFixtureFleets(source finalSemanticFixtureFleetContext, work finalSemanticFixtureWorkControl) ([]finalSemanticFixtureFleetJob, error) {
	jobs := make([]finalSemanticFixtureFleetJob, finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount)
	if err := work.run(finalSemanticFixtureFleetAssembly, len(jobs), 4, func(ctx context.Context, index int) error {
		job, err := prepareFinalSemanticFixtureFleet(ctx, source, uint64(index+1), work, index)
		if err == nil {
			jobs[index] = job
		}
		return err
	}); err != nil {
		return nil, err
	}
	return joinFinalSemanticFixtureFleetJobs(work.ctx, jobs)
}

// Canonicalize and detach the entire fixed fleet census before publication.
// Exact owner namespaces prevent cross-fleet collisions or last-writer wins.
func joinFinalSemanticFixtureFleetJobs(ctx context.Context, jobs []finalSemanticFixtureFleetJob) ([]finalSemanticFixtureFleetJob, error) {
	const count = finalFleetGenerationSetupFleetCount + finalFleetGenerationChallengerFleetCount
	if ctx == nil || len(jobs) != int(count) {
		return nil, errors.New("fixture fleet join has an incomplete census")
	}
	ordered := make([]finalSemanticFixtureFleetJob, count)
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if job.fleet == 0 || job.fleet > count || ordered[job.fleet-1].fleet != 0 {
			return nil, errors.New("fixture fleet join has a duplicate or invalid owner")
		}
		versionCount := 1
		if job.fleet <= finalFleetGenerationSetupFleetCount {
			versionCount = 2
		}
		if len(job.versions) != versionCount || len(job.commitments) != versionCount || len(job.bindings) != int(finalFleetGenerationMembersPerFleet) || len(job.files) != versionCount*(2+int(finalFleetGenerationMembersPerFleet)) {
			return nil, fmt.Errorf("fixture fleet %d has incomplete prepared output", job.fleet)
		}
		owned := finalSemanticFixtureFleetJob{
			fleet: job.fleet, files: make(map[string][]byte, len(job.files)),
			commitments: append([]finalSemanticFixtureFleetMutation(nil), job.commitments...),
			bindings:    append([]finalSemanticFixtureFleetMutation(nil), job.bindings...),
		}
		for index, version := range job.versions {
			generation := uint64(index + 1)
			if version.manifest.Generation != generation || len(version.manifest.Members) != int(finalFleetGenerationMembersPerFleet) {
				return nil, fmt.Errorf("fixture fleet %d has a wrong prepared generation", job.fleet)
			}
			value := version
			value.canonical = append([]byte(nil), version.canonical...)
			value.manifest.Members = append([]protocol.FleetMember(nil), version.manifest.Members...)
			owned.versions = append(owned.versions, value)
			suffix := ""
			actionID := fmt.Sprintf("fleet.commitment.%d", job.fleet)
			if generation == 2 {
				suffix = ".refresh"
				actionID = fmt.Sprintf("fleet.refresh.commitment.%d", job.fleet)
			}
			if owned.commitments[index].actionID != actionID {
				return nil, fmt.Errorf("fixture fleet %d has a wrong commitment owner", job.fleet)
			}
			names := []string{fmt.Sprintf("public/fleet-%d%s.json", job.fleet, suffix), fmt.Sprintf("public/fleet-%d%s.commitment.json", job.fleet, suffix)}
			for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
				names = append(names, fmt.Sprintf("public/fleet-%d-member-%d%s.binding.json", job.fleet, member, suffix))
			}
			for _, name := range names {
				data, exists := job.files[name]
				if !exists || len(data) == 0 {
					return nil, fmt.Errorf("fixture fleet %d has a missing or conflicting file %s", job.fleet, name)
				}
				owned.files[name] = append([]byte(nil), data...)
			}
		}
		for index, binding := range owned.bindings {
			if binding.actionID != fmt.Sprintf("fleet.bind.%d.%d", job.fleet, index+1) {
				return nil, fmt.Errorf("fixture fleet %d has a wrong binding owner", job.fleet)
			}
		}
		ordered[job.fleet-1] = owned
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ordered, nil
}
