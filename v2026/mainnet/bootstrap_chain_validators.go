// Schema-3 config inspection binds signed public facts to independent bootstrap
// roles. It leaves key possession, current generations and live eligibility open.
package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/urfoundation/sn/v2026/validator"
)

// Both declarations must agree on one approved initial production scope while
// retaining separate validator IDs, hotkeys, configurations and custody paths.
func validateBootstrapChainValidatorInspections(config bootstrapChainConfig, inspections []validator.ProductionBootstrapInspection) error {
	if len(inspections) != 2 {
		return errors.New("bootstrap chain requires two verified production config inspections")
	}
	for index, inspection := range inspections {
		role := config.Validators[index]
		approval := inspection.Approval
		if inspection.ValidatorId != role.ValidatorId || inspection.DeploymentId != config.DeploymentId || inspection.Netuid != config.Netuid ||
			inspection.EvmChainId != config.Network.EvmChainId || approval.NativeChain != config.Network.NativeChain ||
			fmt.Sprintf("0x%x", approval.Proposal.Runtime.GenesisHash) != config.Network.GenesisHash ||
			fmt.Sprintf("0x%x", approval.ValidatorHotkey) != role.Hotkey || inspection.ApprovalSigner != role.ApprovalPublicKey {
			return errors.New("bootstrap chain validator signed identity, network, deployment or independent signer differs")
		}
		if approval.Production == nil || len(inspection.DeclaredPaths) == 0 {
			return errors.New("bootstrap chain validator lacks an initial production approval or custody namespaces")
		}
		for _, expected := range config.Validators {
			if !slices.ContainsFunc(approval.Production.ValidatorHotkeys, func(hotkey [32]byte) bool { return fmt.Sprintf("0x%x", hotkey) == expected.Hotkey }) {
				return errors.New("bootstrap chain production validator census omits a protected UR role")
			}
		}
	}
	first, second := inspections[0], inspections[1]
	first.Approval.ConfigHash, second.Approval.ConfigHash = [32]byte{}, [32]byte{}
	first.Approval.ValidatorHotkey, second.Approval.ValidatorHotkey = [32]byte{}, [32]byte{}
	if !reflect.DeepEqual(first.Approval, second.Approval) || first.PolicyHash != second.PolicyHash || first.Coordinator != second.Coordinator ||
		first.SettlementVault != second.SettlementVault || first.DeployBlock != second.DeployBlock {
		return errors.New("bootstrap chain validator approvals disagree on the initial production scope or contract declarations")
	}
	return nil
}

// Sorting bounds comparisons by path depth rather than multiplying two config
// censuses. Paths within a role may nest; different custody owners may not.
func validateBootstrapChainValidatorPaths(inspections []validator.ProductionBootstrapInspection, bootstrapPaths map[string]bool) error {
	type claim struct {
		path  string
		owner int
	}
	claims := []claim{}
	for _, inspection := range inspections {
		path := inspection.ApprovalReference.Path
		if !bootstrapRootAbsolutePath(path) || bootstrapPaths[path] {
			return errors.New("bootstrap chain validator approval overlaps another input or journal")
		}
		bootstrapPaths[path] = true
	}
	for path := range bootstrapPaths {
		claims = append(claims, claim{path: path, owner: -1})
	}
	for index, inspection := range inspections {
		for _, path := range inspection.DeclaredPaths {
			if !bootstrapRootAbsolutePath(path) {
				return errors.New("bootstrap chain validator custody path is not canonical absolute")
			}
			claims = append(claims, claim{path: path, owner: index})
		}
	}
	// A trailing separator keeps /a/child next to /a despite the sibling /a-x.
	slices.SortFunc(claims, func(first, second claim) int {
		return strings.Compare(first.path+string(filepath.Separator), second.path+string(filepath.Separator))
	})
	ancestors := []claim{}
	for _, current := range claims {
		for len(ancestors) != 0 {
			prior := ancestors[len(ancestors)-1]
			if current.path == prior.path || strings.HasPrefix(current.path, prior.path+string(filepath.Separator)) {
				break
			}
			ancestors = ancestors[:len(ancestors)-1]
		}
		for _, prior := range ancestors {
			if current.owner != prior.owner {
				return errors.New("bootstrap chain validator custody namespaces overlap another role or bootstrap input/journal")
			}
		}
		ancestors = append(ancestors, current)
	}
	return nil
}
