package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

func finalFleetRenewalMemberProjection(member uint64, binding FleetBindingEvidence) FinalFleetGenerationMemberEvidence {
	return FinalFleetGenerationMemberEvidence{Member: member, ClientID: strings.ToLower(binding.ClientID), ClientKey: strings.ToLower(binding.ClientKey), FleetKey: strings.ToLower(binding.FleetID), Hotkey: strings.ToLower(binding.Hotkey), CommitmentHash: strings.ToLower(binding.CommitmentHash), Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, UID: binding.UID}
}

func (self *finalFleetGenerationSource) renewalFinalizedAction(actionID, planHash string) (JournalEntry, error) {
	var result *JournalEntry
	for _, entry := range self.entries {
		if entry.Stage != StageFinalized || entry.ActionID != actionID || !self.current.allowedPlanHashes()[entry.PlanHash] || planHash != "" && entry.PlanHash != planHash {
			continue
		}
		if result != nil {
			return JournalEntry{}, fmt.Errorf("fleet renewal action %s has multiple finalized source transactions", actionID)
		}
		copy := entry
		result = &copy
	}
	if result == nil {
		return JournalEntry{}, fmt.Errorf("fleet renewal action %s has no exact finalized transaction", actionID)
	}
	return *result, nil
}

func (self *finalFleetGenerationSource) renewalWrite(actionID, planHash string, calldata []byte) (FinalFleetGenerationWriteEvidence, error) {
	entry, err := self.renewalFinalizedAction(actionID, planHash)
	if err != nil {
		return FinalFleetGenerationWriteEvidence{}, err
	}
	write, err := self.evmWrite(actionID, entry.TransactionHash, entry.BlockNumber, entry.BlockHash, calldata, false)
	if err != nil {
		return write, err
	}
	if _, _, _, _, renewal := finalFleetRenewalActionCoordinates(actionID); renewal {
		plan, err := self.recordPlan(entry.PlanHash)
		if err != nil {
			return write, err
		}
		action, err := exactPlanActionByID(plan, actionID)
		if err != nil {
			return write, err
		}
		raw, err := self.record("launch-foundation/transactions/" + stringsTrim0x(entry.TransactionHash) + ".rlp")
		if err != nil {
			return write, err
		}
		if err := verifyFinalFleetRenewalEnvelope(plan, action, entry.TransactionHash, raw, calldata); err != nil {
			return write, err
		}
	}
	return write, nil
}

func (self *finalFleetGenerationSource) renewalNativeVersion(fleet uint64, generation uint64, stem, actionID string, expectedManifest []byte) (FinalFleetGenerationVersionEvidence, protocol.FleetManifest, error) {
	var zero FinalFleetGenerationVersionEvidence
	manifestData, err := self.record("public/" + stem + ".json")
	if err != nil {
		return zero, protocol.FleetManifest{}, err
	}
	manifest, err := protocol.ParseFleetManifest(manifestData)
	if err != nil || manifest.Generation != generation || manifest.ChainID != self.current.ChainID || manifest.Netuid != self.current.Netuid || common.BytesToAddress(manifest.Coordinator[:]) != self.current.Deployment.CoordinatorProxy || len(manifest.Members) != int(finalFleetGenerationMembersPerFleet) {
		return zero, protocol.FleetManifest{}, stateMismatchError(err, "fleet renewal native manifest differs from approved identity")
	}
	if len(expectedManifest) != 0 {
		approved, err := protocol.ParseFleetManifest(expectedManifest)
		if err != nil {
			return zero, protocol.FleetManifest{}, err
		}
		actualHash, actualErr := manifest.CommitmentHash()
		approvedHash, approvedErr := approved.CommitmentHash()
		if actualErr != nil || approvedErr != nil || actualHash != approvedHash {
			return zero, protocol.FleetManifest{}, errors.Join(errors.New("fleet renewal manifest differs from its signed approval"), actualErr, approvedErr)
		}
	}
	commitmentData, err := self.record("public/" + stem + ".commitment.json")
	if err != nil {
		return zero, protocol.FleetManifest{}, err
	}
	var commitment FleetCommitmentEvidence
	if err := decodeStrictJSONBytes(commitmentData, &commitment); err != nil {
		return zero, protocol.FleetManifest{}, err
	}
	hash, err := manifest.CommitmentHash()
	if err != nil || commitment.Schema != fleetCommitmentEvidenceSchemaV2 || commitment.CommitmentHash != fleetLifecycleHex(hash) || commitment.Hotkey != fleetLifecycleHex(manifest.Hotkey) || commitment.ManifestURI != stem+".json" {
		return zero, protocol.FleetManifest{}, stateMismatchError(err, "fleet renewal commitment does not bind its manifest")
	}
	_, entry, post, postData, err := self.verifiedMutation(actionID, commitment.ExtrinsicHash, commitment.FinalizedBlock, commitment.FinalizedBlockHash, nil)
	if err != nil {
		return zero, protocol.FleetManifest{}, err
	}
	if _, _, _, _, renewal := finalFleetRenewalActionCoordinates(actionID); renewal && (commitment.DeploymentID != self.current.DeploymentID || commitment.PlanHash != entry.PlanHash || commitment.ActionID != actionID || commitment.IntentHash != entry.IntentHash) {
		return zero, protocol.FleetManifest{}, errors.New("fleet renewal public commitment changed its exact journal ownership")
	}
	if post.SubstrateFinalized.Number < entry.BlockNumber || post.SubstrateFinalized.Number > self.evidence.NativeTerminalHead.Number {
		return zero, protocol.FleetManifest{}, errors.New("fleet renewal native finality is outside semantic boundaries")
	}
	manifestLocator, err := self.archive.derivedBytes("fleet-generation-manifest", filepath.ToSlash(filepath.Join("fleet-generation", "manifests", stem+".json")), manifestData)
	if err != nil {
		return zero, protocol.FleetManifest{}, err
	}
	commitmentLocator, err := self.archive.derivedBytes("fleet-generation-commitment", filepath.ToSlash(filepath.Join("fleet-generation", "commitments", stem+".json")), commitmentData)
	if err != nil {
		return zero, protocol.FleetManifest{}, err
	}
	proof, err := self.postconditionProof(entry, postData)
	if err != nil {
		return zero, protocol.FleetManifest{}, err
	}
	version := FinalFleetGenerationVersionEvidence{Generation: generation, Manifest: manifestLocator, Commitment: commitmentLocator, CommitmentAction: FinalFleetGenerationActionEvidence{ActionID: actionID, PlanHash: entry.PlanHash, IntentHash: entry.IntentHash}, CommitmentExtrinsicHash: commitment.ExtrinsicHash, CommitmentPostcondition: proof, CommitmentHash: commitment.CommitmentHash, Hotkey: commitment.Hotkey, NativeHead: ChainHead{Number: commitment.FinalizedBlock, Hash: commitment.FinalizedBlockHash}}
	return version, *manifest, nil
}

func (self *finalFleetGenerationSource) renewalBinding(path string, manifest protocol.FleetManifest, member int, expected *FleetBindingEvidence) (FleetBindingEvidence, []byte, error) {
	raw, err := self.record(path)
	if err != nil {
		return FleetBindingEvidence{}, nil, err
	}
	var retained FleetBindingEvidence
	if err := decodeStrictJSONBytes(raw, &retained); err != nil {
		return retained, nil, err
	}
	if member < 1 || member > len(manifest.Members) {
		return retained, nil, errors.New("fleet renewal binding member is outside its manifest")
	}
	binding, err := fleetRenewalBinding(manifest, manifest.Members[member-1], retained)
	if err != nil {
		return retained, nil, err
	}
	if expected != nil {
		stripReceipt := func(value FleetBindingEvidence) FleetBindingEvidence {
			value.DeploymentID, value.PlanHash, value.ActionID, value.IntentHash = "", "", "", ""
			value.TransactionHash, value.BlockHash, value.BlockNumber = "", "", 0
			return value
		}
		if !finalJSONEqual(stripReceipt(retained), stripReceipt(*expected)) {
			return retained, nil, errors.New("fleet renewal binding differs from its exact signed approval")
		}
	}
	client, _ := evidenceFixedHex(retained.ClientSignature, 64)
	hotkey, _ := evidenceFixedHex(retained.HotkeySignature, 64)
	contract := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: common.BytesToAddress(binding.Coordinator[:]), FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
	data, err := stabi.NewSTCoordinator().TryPackBindFleetMember(contract, client, hotkey)
	return retained, data, err
}

func finalFleetRenewalMirrorCalldata(manifest protocol.FleetManifest, version FinalFleetGenerationVersionEvidence) ([]byte, error) {
	commitment, err := decodeHex32("fleet renewal mirror commitment", version.CommitmentHash)
	if err != nil {
		return nil, err
	}
	blockHash, err := decodeHex32("fleet renewal mirror native block", version.NativeHead.Hash)
	if err != nil {
		return nil, err
	}
	return stabi.NewSTCoordinator().TryPackMirrorCommitment(manifest.Hotkey, commitment, version.NativeHead.Number, blockHash)
}

func (self *finalFleetGenerationSource) renewalPrevious(fleet FleetRenewalFleet, before FinalFleetGenerationVersionEvidence) (*FinalFleetGenerationVersionEvidence, []FinalFleetGenerationWriteEvidence, error) {
	if len(fleet.Members) == 0 {
		return nil, nil, errors.New("fleet renewal lacks predecessor bindings")
	}
	prior := fleet.Members[0].Prior
	if before.Generation == prior.Generation && before.CommitmentHash == prior.CommitmentHash {
		return nil, nil, nil
	}
	prefix := finalFleetRenewalPreviousPrefix(uint64(fleet.Fleet), prior.Generation)
	if prefix == "" || prior.Generation != before.Generation+1 {
		return nil, nil, errors.New("fleet renewal predecessor is outside the approved lifecycle edge")
	}
	sourcePlan, err := self.recordPlan(prior.PlanHash)
	if err != nil {
		return nil, nil, err
	}
	name, err := fleetLifecycleVariantFromAction(prefix + ".commitment")
	if err != nil {
		return nil, nil, err
	}
	variant, err := fleetLifecycleVariantForPlan(sourcePlan, name)
	if err != nil || variant.Fleet != fleet.Fleet || variant.Generation != prior.Generation {
		return nil, nil, stateMismatchError(err, "fleet renewal lifecycle predecessor differs from its source approval")
	}
	stem := strings.TrimSuffix(variant.ManifestName, ".json")
	version, manifest, err := self.renewalNativeVersion(uint64(fleet.Fleet), prior.Generation, stem, prefix+".commitment", nil)
	if err != nil {
		return nil, nil, err
	}
	mirrorData, err := finalFleetRenewalMirrorCalldata(manifest, version)
	if err != nil {
		return nil, nil, err
	}
	mirror, err := self.renewalWrite(prefix+".mirror", "", mirrorData)
	if err != nil {
		return nil, nil, err
	}
	writes := []FinalFleetGenerationWriteEvidence{mirror}
	for member := 1; member <= len(manifest.Members); member++ {
		path := "public/" + variant.BindingName(member)
		binding, data, err := self.renewalBinding(path, manifest, member, nil)
		if err != nil {
			return nil, nil, err
		}
		write, err := self.evmWrite(fmt.Sprintf("%s.bind.%d", prefix, member), binding.TransactionHash, binding.BlockNumber, binding.BlockHash, data, false)
		if err != nil {
			return nil, nil, err
		}
		version.Members = append(version.Members, finalFleetRenewalMemberProjection(uint64(member), binding))
		writes = append(writes, write)
	}
	return &version, writes, nil
}

func (self *finalFleetGenerationSource) buildRenewalRounds(lineage *FinalFleetGenerationLineageEvidence) error {
	latest := map[uint64]FinalFleetGenerationVersionEvidence{}
	for _, fleet := range lineage.SetupFleets {
		latest[fleet.FleetID] = fleet.Refresh
	}
	for _, fleet := range lineage.ChallengerFleets {
		latest[fleet.FleetID] = fleet.Initial
	}
	for _, renewal := range self.current.FleetRenewals {
		round, approved, err := self.renewalApproval(renewal)
		if err != nil {
			return err
		}
		for _, planned := range renewal.Fleets {
			fleet := FinalFleetRenewalFleetEvidence{FleetID: uint64(planned.Fleet)}
			fleet.Previous, fleet.PreviousWrites, err = self.renewalPrevious(planned, latest[fleet.FleetID])
			if err != nil {
				return err
			}
			manifest, err := protocol.ParseFleetManifest(planned.Manifest)
			if err != nil {
				return err
			}
			stem := fleetRenewalStem(renewal.Round, planned.Fleet)
			fleet.Version, _, err = self.renewalNativeVersion(fleet.FleetID, manifest.Generation, stem, fleetRenewalActionID(renewal.Round, planned.Fleet, "commitment", 0), planned.Manifest)
			if err != nil {
				return err
			}
			mirrorData, err := finalFleetRenewalMirrorCalldata(*manifest, fleet.Version)
			if err != nil {
				return err
			}
			fleet.Mirror, err = self.renewalWrite(fleetRenewalActionID(renewal.Round, planned.Fleet, "mirror", 0), approved.PlanHash, mirrorData)
			if err != nil {
				return err
			}
			for index, member := range planned.Members {
				number := index + 1
				binding, data, err := self.renewalBinding(fmt.Sprintf("public/%s-member-%d.binding.json", stem, number), *manifest, number, &member.Binding)
				if err != nil {
					return err
				}
				id := fleetRenewalActionID(renewal.Round, planned.Fleet, "bind", number)
				write, err := self.renewalWrite(id, approved.PlanHash, data)
				if err != nil {
					return err
				}
				if binding.DeploymentID != self.current.DeploymentID || binding.PlanHash != approved.PlanHash || binding.ActionID != id || binding.IntentHash != write.Action.IntentHash || binding.TransactionHash != write.Receipt.TransactionHash || binding.BlockNumber != write.Receipt.Block.Number || binding.BlockHash != write.Receipt.Block.Hash {
					return errors.New("fleet renewal public binding does not name its exact approved receipt")
				}
				row := FinalFleetRenewalMemberEvidence{Member: uint64(number), Prior: finalFleetRenewalMemberProjection(uint64(number), member.Prior), Binding: write}
				if member.RevokeSignature != "" {
					signature, err := hex.DecodeString(stringsTrim0x(member.RevokeSignature))
					if err != nil {
						return err
					}
					data, err := stabi.NewSTCoordinator().TryPackRevokeFleetBinding(manifest.Members[index].ClientID, member.Prior.Generation, renewal.ValidFromEpoch, signature)
					if err != nil {
						return err
					}
					revoke, err := self.renewalWrite(fleetRenewalActionID(renewal.Round, planned.Fleet, "revoke", number), approved.PlanHash, data)
					if err != nil {
						return err
					}
					row.Revocation = &revoke
				}
				fleet.Version.Members = append(fleet.Version.Members, finalFleetRenewalMemberProjection(uint64(number), binding))
				fleet.Members = append(fleet.Members, row)
			}
			latest[fleet.FleetID] = fleet.Version
			round.Fleets = append(round.Fleets, fleet)
		}
		lineage.Renewals = append(lineage.Renewals, round)
	}
	return nil
}

func (self *finalFleetGenerationSource) renewalApproval(renewal FleetRenewal) (FinalFleetRenewalRoundEvidence, *SetupPlan, error) {
	base, err := self.recordPlan(renewal.SourcePlanHash)
	if err != nil {
		return FinalFleetRenewalRoundEvidence{}, nil, err
	}
	expected, err := appendFleetRenewalPlan(base, renewal)
	if err != nil {
		return FinalFleetRenewalRoundEvidence{}, nil, err
	}
	if !self.current.allowedPlanHashes()[expected.PlanHash] {
		return FinalFleetRenewalRoundEvidence{}, nil, errors.New("fleet renewal exact append approval is missing from current lineage")
	}
	approved, err := self.recordPlan(expected.PlanHash)
	if err != nil {
		return FinalFleetRenewalRoundEvidence{}, nil, err
	}
	approvalData, err := self.record(self.planPaths[approved.PlanHash])
	if err != nil {
		return FinalFleetRenewalRoundEvidence{}, nil, err
	}
	approval, err := self.archive.derivedBytes("fleet-renewal-approval", fmt.Sprintf("fleet-generation/renewal-%d-approval.json", renewal.Round), approvalData)
	if err != nil {
		return FinalFleetRenewalRoundEvidence{}, nil, err
	}
	metadataHash, err := canonicalHashHex(renewal)
	if err != nil {
		return FinalFleetRenewalRoundEvidence{}, nil, err
	}
	round := FinalFleetRenewalRoundEvidence{Round: renewal.Round, SourcePlanHash: base.PlanHash, ApprovedPlanHash: approved.PlanHash, MetadataHash: metadataHash, Approval: approval, ValidFromEpoch: renewal.ValidFromEpoch, ValidToEpoch: renewal.ValidToEpoch}
	return round, approved, nil
}
