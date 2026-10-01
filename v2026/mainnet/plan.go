// Bootstrap planning is a pure dependency review over exact retained inputs.
// Its blocked graph is never transaction, custody or service-start authority.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
)

const bootstrapPlanConfigSchema = "urnetwork-mainnet-plan-config-v1"
const bootstrapReleaseInputSchema = "urnetwork-mainnet-release-input-v1"
const bootstrapPlanSchema = "urnetwork-mainnet-blocked-plan-v2"

// File hashes bind exact bytes, including whitespace. Paths are resolved only
// by the input loader, never fetched as URLs or interpreted as commands.
type planFileReference struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

// These expectations come from the operator's separate network review. A
// matching observation cannot create that approval or approve its runtime.
type planNetwork struct {
	NativeChain string `json:"native_chain"`
	GenesisHash string `json:"genesis_hash"`
	EvmChainId  uint64 `json:"evm_chain_id"`
}

// This narrow JSON config does not accept private keys, authority flags or
// action payloads. Rich executable configuration remains a separate interface.
type bootstrapPlanConfig struct {
	Schema       string            `json:"schema"`
	DeploymentId string            `json:"deployment_id"`
	Netuid       uint16            `json:"netuid"`
	Network      planNetwork       `json:"network"`
	Snapshot     planFileReference `json:"snapshot"`
	SourceLock   planFileReference `json:"source_lock"`
	Release      planFileReference `json:"release"`
}

// Supplemental manifests are retained by hash for review only. Their presence
// never satisfies the missing semantic validator or confers signing authority.
type planReviewInput struct {
	Requirement string `json:"requirement"`
	planFileReference
}

// A release input binds source/runtime/snapshot identities independently of
// local file names. This is an unapproved review manifest, not a release seal.
type bootstrapReleaseInput struct {
	Schema                string                      `json:"schema"`
	DeploymentId          string                      `json:"deployment_id"`
	Netuid                uint16                      `json:"netuid"`
	SnapshotContentHash   string                      `json:"snapshot_content_hash"`
	SourceLockContentHash string                      `json:"source_lock_content_hash"`
	RuntimeVersion        crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash       string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash   string                      `json:"runtime_metadata_hash"`
	ReviewInputs          []planReviewInput           `json:"review_inputs"`
}

// Exact input identities are useful to reviewers without copying potentially
// large evidence or local paths into every action. No timestamp is generated.
type planInputBinding struct {
	Kind        string `json:"kind"`
	Sha256      string `json:"sha256"`
	ContentHash string `json:"content_hash,omitempty"`
}

// The launch target is 10% of native miner allocation, before withholding;
// owner recycling is a chain outcome, with zero reserve-credit interpretation.
type planEconomics struct {
	Denominator         string `json:"denominator"`
	ProviderNumerator   uint64 `json:"provider_numerator"`
	FractionDenominator uint64 `json:"fraction_denominator"`
	RemainderNumerator  uint64 `json:"remainder_numerator"`
	Remainder           string `json:"remainder"`
	Assurance           string `json:"assurance"`
	ReserveCredit       bool   `json:"reserve_credit"`
}

// A supplied digest establishes availability only. Each requirement still
// names the semantic/capability evidence needed by a future execution planner.
type planRequirement struct {
	Id          string `json:"id"`
	Description string `json:"description"`
	ProducedBy  string `json:"produced_by,omitempty"`
	Status      string `json:"status"`
	InputSha256 string `json:"input_sha256,omitempty"`
}

// Dependencies refer to stable action IDs in topological order. There are no
// nonces, payloads, inferred CREATE addresses or executable placeholders.
type planAction struct {
	Id             string   `json:"id"`
	Phase          uint8    `json:"phase"`
	Description    string   `json:"description"`
	DependsOn      []string `json:"depends_on"`
	Requirements   []string `json:"requirements"`
	Postconditions []string `json:"postconditions,omitempty"`
	Status         string   `json:"status"`
	Executable     bool     `json:"executable"`
}

// A domain-separated blocked review record cannot be confused with a future
// executable plan schema or signed authorization.
type bootstrapPlan struct {
	Schema            string                      `json:"schema"`
	Status            string                      `json:"status"`
	ApplyAuthority    bool                        `json:"apply_authority"`
	ActivationReady   bool                        `json:"activation_ready"`
	DeploymentId      string                      `json:"deployment_id"`
	Netuid            uint16                      `json:"netuid"`
	Network           planNetwork                 `json:"network"`
	FinalizedHash     string                      `json:"finalized_hash"`
	FinalizedNumber   uint64                      `json:"finalized_number"`
	EvmHash           string                      `json:"evm_hash"`
	EvmNumber         uint64                      `json:"evm_number"`
	ObservationTrust  string                      `json:"observation_trust"`
	RuntimeVersion    crv4.RuntimeVersionIdentity `json:"runtime_version"`
	Inputs            []planInputBinding          `json:"inputs"`
	Economics         planEconomics               `json:"economics"`
	Requirements      []planRequirement           `json:"requirements"`
	Actions           []planAction                `json:"actions"`
	ExecutionBlockers []string                    `json:"execution_blockers"`
	ContentHash       string                      `json:"content_hash"`
}

// Keep requirements explicit rather than accepting a boolean "approved"
// supplied by a manifest. The descriptions identify missing integration seams.
func bootstrapRequirements() []planRequirement {
	return []planRequirement{
		{Id: "runtime-authority", Description: "Independent mainnet source-to-Wasm, code/metadata/version and supported capability approval"},
		{Id: "owned-rpc", Description: "Owned mainnet route attestation, archive capability and independently corroborated finalized identity"},
		{Id: "testnet-closure", Description: "Final testnet report preserving R48 failure and the actual completed scope"},
		{Id: "testnet-exceptions", Description: "Disposition of every testnet exception; no provisional testnet authority inherited"},
		{Id: "production-qualification", Description: "Exact composed binary/contract/server release and controlled production-path rehearsal results"},
		{Id: "contract-artifacts", Description: "Approved exact bytecode, ABI, constructor arguments, origins/nonces, expected addresses and expected custody getter values; observed getters are installation postconditions"},
		{Id: "deployed-contract-state", ProducedBy: "install-contracts", Description: "Finalized deployment receipts, exact runtime code and actual custody/immutable getters, including the validator-evidence journal and coordinator binding"},
		{Id: "binary-artifacts", Description: "Exact miner, validator, operator, root-service and monitor executable/config/image identities"},
		{Id: "migration-cutover", Description: "Qualified server migration order, schema/writer compatibility and revision-fenced writer cutover"},
		{Id: "roles", Description: "Public owner/deployer/Safe/guardian/oracle/pool/escrow/provider identities, two distinct UR validators, and separate netuid-0 role"},
		{Id: "custody", Description: "Bounded signer/coldkey/Safe custody adapters, permissions, single-writer leases and durable nonce/receipt ownership"},
		{Id: "limits", Description: "Integer fee/value/stake/registration/lifetime ceilings, collateral exposure and validity windows; no inherited testnet budgets"},
		{Id: "subnet-census", Description: "Exact SN25 generation and both mapping directions; removal/preservation hotkey generations, residual stake and capacity"},
		{Id: "reset-capability", Description: "Best-effort owner-authorized trim with execution-time selection guard; prove protected roles, custody, renumbering and explicitly retained old generations"},
		{Id: "registration-plan", Description: "Approved bounded pool/escrow/head/validator identities, exact registration payloads, cost ceilings and expected postconditions"},
		{Id: "registered-subnet-roles", ProducedBy: "register-subnet-roles", Description: "Finalized exact registration generations, ownership, mappings and bounded registration cost receipts"},
		{Id: "root-seat", Description: "Existing netuid-0 hotkey/coldkey seat generation, eligibility, stake/retention, delegation and basket policy; no assumed seat or unbounded registration"},
		{Id: "ur-validators", Description: "Two independent UR identities, approved standard-validator configs, current permits/effective stake and evidence-based CRv4 policy; no prior revealed row is required to start"},
		{Id: "ur-validator-services", ProducedBy: "start-ur-validators", Description: "Both admitted standard validator services own their approved config and durable state; first revealed/applied rows remain later output"},
		{Id: "ur-validator-rows", ProducedBy: "activate-native-miner-emissions", Description: "Actual independently observed committed/revealed/applied evidence-based rows from both UR validators"},
		{Id: "operator-quorum", Description: "Two healthy operators and authenticated domains; root-only membership cannot count toward UR quorum"},
		{Id: "services", Description: "Independent root/UR services with complete child joining, durable state, config hashes and bounded restart/rollout"},
		{Id: "monitor-oncall", Description: "Independent reconciliation, delivered alert tests, primary/backup on-call and bounded repair/rollback policy"},
		{Id: "emission-mechanism", Description: "Approved runtime-qualified 10/90 producer policy, exact native calls, allocation denominator and accepted quantization/outcome tolerance; actual Yuma results are later postconditions"},
		{Id: "recycle-policy", Description: "Approved owner-hotkey destinations, exact Recycle-mode transition/precondition, drain boundary and remainder-accounting policy with no reserve credit"},
		{Id: "emission-activation-state", ProducedBy: "activate-native-miner-emissions", Description: "Finalized native Recycle mode, activation boundary and exact applied policy generation"},
		{Id: "native-emission-outcomes", ProducedBy: "accept-and-reconcile", Description: "Observed native miner allocation, provider 10% target within approved tolerance and realized 90% recycling over the accepted production interval"},
		{Id: "payout-policy", Description: "Signed UR policy and equal paid/free completed-byte attribution with durable usage, source continuity and settlement proofs"},
		{Id: "activation-boundary", Description: "Fresh finalized native boundary and exact admitted policy/capability generation after setup completes"},
		{Id: "acceptance-policy", Description: "Production interval, settlement/claim reconciliation, observed 10% outcome tolerance and terminal evidence predicates"},
	}
}

// Root service readiness is independent of UR reset/deployment work, while
// activation waits for both roles and the separate UR/operator safety quorum.
func bootstrapActions() []planAction {
	return []planAction{
		{Id: "qualify-release", Phase: 0, Description: "Review exact source, artifacts and production qualification", DependsOn: []string{}, Requirements: []string{"runtime-authority", "testnet-closure", "testnet-exceptions", "production-qualification", "contract-artifacts", "binary-artifacts", "migration-cutover"}},
		{Id: "review-authority", Phase: 1, Description: "Establish network, public roles, custody and bounded authority", DependsOn: []string{"qualify-release"}, Requirements: []string{"owned-rpc", "roles", "custody", "limits"}},
		{Id: "reset-miner-uids", Phase: 2, Description: "Apply the safest owner-authorized partial SN25 trim; prove protected identities survive and report every residual old generation without claiming a full reset", DependsOn: []string{"review-authority"}, Requirements: []string{"subnet-census", "reset-capability"}},
		{Id: "install-contracts", Phase: 3, Description: "Deploy exact custody graph, validator-evidence journal and coordinator binding; then prove code, ownership and constructor state", DependsOn: []string{"reset-miner-uids"}, Requirements: []string{"contract-artifacts", "custody", "limits"}, Postconditions: []string{"deployed-contract-state"}},
		{Id: "register-subnet-roles", Phase: 3, Description: "Register approved pool, escrow, miner and UR-validator generations", DependsOn: []string{"install-contracts"}, Requirements: []string{"registration-plan", "subnet-census", "roles", "limits", "deployed-contract-state"}, Postconditions: []string{"registered-subnet-roles"}},
		{Id: "start-root-validator", Phase: 4, Description: "Run the separate netuid-0 role only for an approved existing seat and strategy", DependsOn: []string{"review-authority"}, Requirements: []string{"root-seat", "services", "custody", "limits"}},
		{Id: "start-ur-validators", Phase: 4, Description: "Start both independent standard UR validators with the two-operator safety quorum before requiring their first production rows", DependsOn: []string{"register-subnet-roles"}, Requirements: []string{"ur-validators", "operator-quorum", "services", "payout-policy", "registered-subnet-roles"}, Postconditions: []string{"ur-validator-services"}},
		{Id: "admit-operations", Phase: 4, Description: "Start independent monitoring and verify alert/repair ownership", DependsOn: []string{"review-authority"}, Requirements: []string{"monitor-oncall", "services"}},
		{Id: "activate-native-miner-emissions", Phase: 5, Description: "Activate the approved 10/90 evidence-based producer and owner Recycle mode; retain actual rows and native state before economic acceptance", DependsOn: []string{"start-root-validator", "start-ur-validators", "admit-operations"}, Requirements: []string{"emission-mechanism", "recycle-policy", "payout-policy", "activation-boundary", "ur-validator-services"}, Postconditions: []string{"ur-validator-rows", "emission-activation-state"}},
		{Id: "accept-and-reconcile", Phase: 6, Description: "Observe actual production outcomes, settle accrued value and publish complete evidence", DependsOn: []string{"activate-native-miner-emissions"}, Requirements: []string{"acceptance-policy", "monitor-oncall", "recycle-policy", "ur-validator-rows", "emission-activation-state"}, Postconditions: []string{"native-emission-outcomes"}},
	}
}

// A produced fact may be consumed only after its producing ancestor. Describing
// an eventual outcome or supplying its hash cannot turn it into a startup input.
func validateBootstrapDependencies(requirements []planRequirement, actions []planAction) error {
	requirementKVs := map[string]planRequirement{}
	for _, requirement := range requirements {
		if requirement.Id == "" || requirementKVs[requirement.Id].Id != "" {
			return errors.New("bootstrap requirement identity is absent or duplicated")
		}
		requirementKVs[requirement.Id] = requirement
	}
	ancestorKVs := map[string]map[string]bool{}
	produced := map[string]bool{}
	for _, action := range actions {
		if action.Id == "" || ancestorKVs[action.Id] != nil {
			return errors.New("bootstrap action identity is absent or duplicated")
		}
		ancestors := map[string]bool{}
		for _, dependency := range action.DependsOn {
			prior := ancestorKVs[dependency]
			if prior == nil || ancestors[dependency] {
				return fmt.Errorf("bootstrap dependency %s is not a unique earlier action", dependency)
			}
			ancestors[dependency] = true
			for ancestor := range prior {
				ancestors[ancestor] = true
			}
		}
		for _, id := range action.Requirements {
			requirement, exists := requirementKVs[id]
			if !exists || requirement.ProducedBy != "" && (!ancestors[requirement.ProducedBy] || !produced[id]) {
				return fmt.Errorf("bootstrap action %s requires unavailable or future output %s", action.Id, id)
			}
		}
		for _, id := range action.Postconditions {
			if requirementKVs[id].ProducedBy != action.Id || produced[id] {
				return fmt.Errorf("bootstrap output %s has no unique declared producer", id)
			}
			produced[id] = true
		}
		ancestorKVs[action.Id] = ancestors
	}
	for _, requirement := range requirements {
		if requirement.ProducedBy != "" && !produced[requirement.Id] {
			return fmt.Errorf("bootstrap output %s has no producing action", requirement.Id)
		}
	}
	return nil
}

// Validation is intentionally distinct from approval; all actions remain
// blocked even if every supplemental review file has a matching hash.
func buildBootstrapPlan(config bootstrapPlanConfig, snapshot finalizedSnapshotEnvelope, lock sourceLock, release bootstrapReleaseInput, inputs []planInputBinding) (bootstrapPlan, error) {
	if config.Schema != bootstrapPlanConfigSchema || config.Netuid != 25 || !planLabel(config.DeploymentId) ||
		config.Network.EvmChainId != mainnetEvmChainId || strings.TrimSpace(config.Network.NativeChain) == "" ||
		!rootCanonicalHash(config.Network.GenesisHash) {
		return bootstrapPlan{}, errors.New("plan requires its exact schema, deployment ID, SN25 and independently supplied mainnet chain/genesis/EVM964")
	}
	expected := identityExpectation{NativeChain: config.Network.NativeChain, GenesisHash: config.Network.GenesisHash, EvmChainId: mainnetEvmChainId}
	if err := expected.match(snapshot.Runtime.Identity); err != nil {
		return bootstrapPlan{}, err
	}
	if err := validatePlanSnapshot(snapshot); err != nil {
		return bootstrapPlan{}, err
	}
	if err := validatePlanSourceLock(lock); err != nil {
		return bootstrapPlan{}, err
	}
	if release.Schema != bootstrapReleaseInputSchema || release.DeploymentId != config.DeploymentId || release.Netuid != config.Netuid ||
		release.SnapshotContentHash != snapshot.ContentHash || release.SourceLockContentHash != lock.ContentHash ||
		release.RuntimeVersion != snapshot.Runtime.Version || release.RuntimeCodeHash != snapshot.Runtime.CodeHash || release.RuntimeMetadataHash != snapshot.Runtime.MetadataHash {
		return bootstrapPlan{}, errors.New("release input does not bind the exact deployment, snapshot, source lock and complete runtime artifacts")
	}
	requirements := bootstrapRequirements()
	inputByRequirement := map[string]planReviewInput{}
	for _, input := range release.ReviewInputs {
		if _, ok := inputByRequirement[input.Requirement]; ok {
			return bootstrapPlan{}, errors.New("duplicate release review requirement")
		}
		inputByRequirement[input.Requirement] = input
	}
	for index := range requirements {
		requirement := &requirements[index]
		requirement.Status = "missing"
		if input, ok := inputByRequirement[requirement.Id]; ok {
			requirement.Status = "supplied_unvalidated"
			requirement.InputSha256 = input.Sha256
			delete(inputByRequirement, requirement.Id)
		}
	}
	if len(inputByRequirement) != 0 {
		return bootstrapPlan{}, errors.New("unrecognized release review requirement")
	}
	actions := bootstrapActions()
	if err := validateBootstrapDependencies(requirements, actions); err != nil {
		return bootstrapPlan{}, err
	}
	for index := range actions {
		action := &actions[index]
		action.Status = "blocked"
	}
	plan := bootstrapPlan{
		Schema: bootstrapPlanSchema, Status: "blocked", DeploymentId: config.DeploymentId, Netuid: config.Netuid, Network: config.Network,
		FinalizedHash: snapshot.FinalizedHash, FinalizedNumber: snapshot.FinalizedNumber, EvmHash: snapshot.Mapping.EvmHeader.Hash, EvmNumber: snapshot.Mapping.EvmHeader.Number,
		ObservationTrust: "owned-rpc-assertion; content hashes are not approval or independent consensus proof", RuntimeVersion: snapshot.Runtime.Version,
		Inputs: inputs, Requirements: requirements, Actions: actions,
		Economics: planEconomics{Denominator: "native_miner_allocation_before_withholding", ProviderNumerator: 1, FractionDenominator: 10, RemainderNumerator: 9, Remainder: "owner-recycle", Assurance: "observed-native-target"},
		ExecutionBlockers: []string{
			"Semantic validators for every supplied review manifest are required; byte identity does not satisfy a capability or custody gate",
			"Exact per-action origin/payload/nonce/address/postcondition adapters and bounded lifetime ledger are not implemented by this planner",
			"This review-only graph cannot authorize the separate executable local-custody bootstrap phase or any chain phase; each chain phase needs its own signed action, ceilings and expiry",
			"Revalidate current identity, runtime, generations and retained receipts at execution; an offline snapshot cannot establish present readiness",
		},
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return bootstrapPlan{}, err
	}
	digest := sha256.Sum256(append([]byte(bootstrapPlanSchema+"\x00"), encoded...))
	plan.ContentHash = "sha256:" + hex.EncodeToString(digest[:])
	return plan, nil
}

// Stable deployment labels exclude shell/environment substitutions and paths.
func planLabel(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
