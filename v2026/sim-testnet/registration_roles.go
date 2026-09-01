package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Contract-owned hotkeys cannot be reused by a replacement immutable vault.
// Subtensor permanently associates each hotkey with its first coldkey owner,
// so every replacement generation receives a distinct deterministic escrow
// and pool identity. Generation zero retains the original wire labels.
func escrowHotkeyLabelForGeneration(generation uint64) string {
	if generation == 0 {
		return "escrow-hotkey"
	}
	return fmt.Sprintf("escrow-hotkey-generation-%d", generation)
}

func operatorPoolHotkeyLabelForGeneration(operator int, generation uint64) string {
	if generation == 0 {
		return fmt.Sprintf("operator-%d-pool-hotkey", operator)
	}
	return fmt.Sprintf("operator-%d-pool-hotkey-generation-%d", operator, generation)
}

func contractRegistrationRoleLabels(topology TopologyConfig, generation uint64) []string {
	labels := []string{escrowHotkeyLabelForGeneration(generation)}
	for operator := 1; operator <= topology.Operators; operator++ {
		labels = append(labels, operatorPoolHotkeyLabelForGeneration(operator, generation))
	}
	return labels
}

func contractRegistrationRoleCount(topology TopologyConfig) int {
	return topology.Operators + 1
}

func maximumContractRegistrationGeneration(topology TopologyConfig) uint64 {
	count := contractRegistrationRoleCount(topology)
	available := topology.ChurnFloorUIDs - topology.ChallengerFleets
	if count <= 0 || available < count {
		return 0
	}
	return uint64(available / count)
}

func validateContractRegistrationGeneration(topology TopologyConfig, generation uint64) error {
	maximum := maximumContractRegistrationGeneration(topology)
	if generation > maximum {
		return fmt.Errorf("contract registration generation %d exceeds churn-backed maximum %d", generation, maximum)
	}
	return nil
}

func contractRegistrationGenerationFromSupersededSpend(topology TopologyConfig, prior *SetupPlan) (uint64, error) {
	if prior == nil {
		return 0, errors.New("prior plan is unavailable")
	}
	count := uint32(contractRegistrationRoleCount(topology))
	if count == 0 || prior.SupersededSpend.Registrations%count != 0 {
		return 0, fmt.Errorf("superseded registration spend %d is not an exact contract-generation multiple %d", prior.SupersededSpend.Registrations, count)
	}
	generation := uint64(prior.SupersededSpend.Registrations / count)
	if err := validateContractRegistrationGeneration(topology, generation); err != nil {
		return 0, err
	}
	return generation, nil
}

func parseContractRegistrationRoleLabel(topology TopologyConfig, label string) (generation uint64, operator int, escrow bool, ok bool) {
	if label == "escrow-hotkey" {
		return 0, 0, true, true
	}
	const escrowPrefix = "escrow-hotkey-generation-"
	if strings.HasPrefix(label, escrowPrefix) {
		generation, err := strconv.ParseUint(strings.TrimPrefix(label, escrowPrefix), 10, 64)
		if err == nil && generation > 0 && validateContractRegistrationGeneration(topology, generation) == nil {
			return generation, 0, true, true
		}
		return 0, 0, false, false
	}
	for candidate := 1; candidate <= topology.Operators; candidate++ {
		if label == fmt.Sprintf("operator-%d-pool-hotkey", candidate) {
			return 0, candidate, false, true
		}
		prefix := fmt.Sprintf("operator-%d-pool-hotkey-generation-", candidate)
		if strings.HasPrefix(label, prefix) {
			generation, err := strconv.ParseUint(strings.TrimPrefix(label, prefix), 10, 64)
			if err == nil && generation > 0 && validateContractRegistrationGeneration(topology, generation) == nil {
				return generation, candidate, false, true
			}
			return 0, 0, false, false
		}
	}
	return 0, 0, false, false
}

func baseInitialTopologyRoleLabels(topology TopologyConfig) []string {
	labels := make([]string, 0, topology.ChurnFloorUIDs+1+2*topology.Operators+topology.HeadFleets+topology.Validators)
	for churn := 1; churn <= topology.ChurnFloorUIDs; churn++ {
		labels = append(labels, churnHotkeyLabel(churn))
	}
	labels = append(labels, escrowHotkeyLabelForGeneration(0))
	for operator := 1; operator <= topology.Operators; operator++ {
		labels = append(labels, fmt.Sprintf("operator-%d-deposit-hotkey", operator), operatorPoolHotkeyLabelForGeneration(operator, 0))
	}
	for fleet := 1; fleet <= topology.HeadFleets; fleet++ {
		labels = append(labels, fleetHotkeyLabel(fleet))
	}
	for validator := 1; validator <= topology.Validators; validator++ {
		labels = append(labels, validatorHotkeyLabel(validator))
	}
	return labels
}

// Return the full fixed-size topology immediately before this generation's
// contract-owned registrations. Earlier generations occupy the churn UIDs
// they deterministically replaced; generation-zero roles remain retired but
// live until later bounded pruning.
func topologyRoleLabelsBeforeGeneration(topology TopologyConfig, generation uint64) ([]string, error) {
	if err := validateContractRegistrationGeneration(topology, generation); err != nil {
		return nil, err
	}
	labels := baseInitialTopologyRoleLabels(topology)
	count := contractRegistrationRoleCount(topology)
	for completed := uint64(1); completed < generation; completed++ {
		offset := int(completed-1) * count
		copy(labels[offset:offset+count], contractRegistrationRoleLabels(topology, completed))
	}
	return labels, nil
}

func topologyRoleLabelsAtProgress(topology TopologyConfig, generation uint64, contractRegistrations, challengers int) ([]string, error) {
	count := contractRegistrationRoleCount(topology)
	if contractRegistrations < 0 || contractRegistrations > count || challengers < 0 || challengers > topology.ChallengerFleets {
		return nil, errors.New("topology registration progress is out of range")
	}
	if generation == 0 && contractRegistrations != 0 {
		return nil, errors.New("generation zero has no replacement contract registrations")
	}
	if generation > 0 && contractRegistrations != count && challengers != 0 {
		return nil, errors.New("challengers cannot precede the complete active contract generation")
	}
	labels, err := topologyRoleLabelsBeforeGeneration(topology, generation)
	if err != nil {
		return nil, err
	}
	if generation > 0 {
		offset := int(generation-1) * count
		copy(labels[offset:offset+contractRegistrations], contractRegistrationRoleLabels(topology, generation)[:contractRegistrations])
	}
	challengerOffset := int(generation) * count
	for index := 0; index < challengers; index++ {
		labels[challengerOffset+index] = fleetHotkeyLabel(topology.HeadFleets + index + 1)
	}
	return labels, nil
}

func initialTopologyRoleLabels(topology TopologyConfig, generation uint64) []string {
	registrations := 0
	if generation > 0 {
		registrations = contractRegistrationRoleCount(topology)
	}
	labels, err := topologyRoleLabelsAtProgress(topology, generation, registrations, 0)
	if err != nil {
		return nil
	}
	return labels
}

func tournamentTopologyRoleLabels(topology TopologyConfig, generation uint64) []string {
	registrations := 0
	if generation > 0 {
		registrations = contractRegistrationRoleCount(topology)
	}
	labels, err := topologyRoleLabelsAtProgress(topology, generation, registrations, topology.ChallengerFleets)
	if err != nil {
		return nil
	}
	return labels
}

func churnIndexForContractRegistration(topology TopologyConfig, generation uint64, registration int) (int, error) {
	count := contractRegistrationRoleCount(topology)
	if generation == 0 || registration < 1 || registration > count {
		return 0, errors.New("contract registration does not identify a replacement churn UID")
	}
	if err := validateContractRegistrationGeneration(topology, generation); err != nil {
		return 0, err
	}
	return int(generation-1)*count + registration, nil
}

func churnIndexForChallenger(topology TopologyConfig, generation uint64, challenger int) (int, error) {
	if challenger < 1 || challenger > topology.ChallengerFleets {
		return 0, errors.New("challenger index is out of range")
	}
	if err := validateContractRegistrationGeneration(topology, generation); err != nil {
		return 0, err
	}
	index := int(generation)*contractRegistrationRoleCount(topology) + challenger
	if index > topology.ChurnFloorUIDs {
		return 0, errors.New("challenger has no reserved churn UID")
	}
	return index, nil
}
