// An independently supplied finite role census routes every service metric.
// Source values never select their own expected identity or metric labels.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/urfoundation/sn/v2026/diagnostics"
	"github.com/urfoundation/sn/v2026/protocol"
)

const monitorServicesSchema = "urnetwork-mainnet-monitor-services-v1"
const maxMonitorServicesBytes = 64 * 1024
const maxMonitorValidatorRoles = 8

// Callers distinguish an exhausted role census from unrelated source or wire faults.
var errMonitorServicesCensus = errors.New("service policy requires at least one role, at most eight validators and four each of operators, providers, claim sources, native and EVM economic readers within shared output capacity")

// Roles are fixed by the local expected census, with no candidate-supplied
// label values. Each role has independent source, checkpoint and metric owners.
type monitorServicesPolicy struct {
	Schema          string                        `json:"schema"`
	Validators      []monitorValidatorPolicy      `json:"validators"`
	Operators       []monitorOperatorPolicy       `json:"operators,omitempty"`
	Providers       []monitorProviderPolicy       `json:"providers,omitempty"`
	Claims          []monitorClaimPolicy          `json:"claims,omitempty"`
	NativeEconomics []monitorEconomicNativePolicy `json:"native_economics,omitempty"`
	EvmEconomics    []monitorEconomicEvmPolicy    `json:"evm_economics,omitempty"`
}

// The full current source is exact. A retained intent may still name its older
// original config; no observer grants approval to either configuration.
type monitorValidatorPolicy struct {
	Role             string                           `json:"role"`
	ProgressFile     string                           `json:"progress_file"`
	ExpectedSource   protocol.ValidatorProgressSource `json:"expected_source"`
	NativeDeadline   *monitorNativeDeadlinePolicy     `json:"native_deadline,omitempty"`
	SteeringLiveness *monitorSteeringLivenessPolicy   `json:"steering_liveness,omitempty"`
}

// A role label cannot contain arbitrary paths, hashes, error text or quoting.
var monitorRolePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Policy admission precedes all source workers. The same independent chain
// identity must describe chain observations and every supplied producer role.
func loadMonitorServices(ctx context.Context, path string, expected identityExpectation, checkpointPath, metricsPath string) (*monitorServicesPolicy, error) {
	raw, err := readMonitorServicesPolicyFile(ctx, path, monitorServiceReadHooks{})
	if err != nil {
		return nil, errors.Join(errors.New("service policy must be bounded protected regular JSON"), err)
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var policy monitorServicesPolicy
	if err := decoder.Decode(&policy); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("service policy contains trailing JSON")
	}
	if policy.Schema != monitorServicesSchema {
		return nil, errors.New("service policy schema is unknown")
	}
	if err := policy.validateFrame(len(raw)); err != nil {
		return nil, err
	}
	rolesCount := len(policy.Validators) + len(policy.Operators) + len(policy.Providers) + len(policy.Claims) + len(policy.NativeEconomics) + len(policy.EvmEconomics)
	if rolesCount == 0 || rolesCount > diagnostics.MaximumDomains-2 || len(policy.Validators) > maxMonitorValidatorRoles || len(policy.Operators) > maxMonitorOperators || len(policy.Providers) > maxMonitorProviders || len(policy.Claims) > maxMonitorClaims || len(policy.NativeEconomics) > maximumMonitorEconomicRoles || len(policy.EvmEconomics) > maximumMonitorEconomicRoles {
		return nil, errMonitorServicesCensus
	}
	paths := map[string]bool{}
	addPath := func(path string) error {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 2048 {
			return errors.New("service path must be bounded canonical absolute")
		}
		if resolved, err := resolveMonitorDestination(path); err == nil {
			path = resolved
		}
		if paths[path] {
			return errors.New("service inputs, outputs and locks must be separate")
		}
		paths[path] = true
		return nil
	}
	for _, path := range []string{path, checkpointPath, checkpointPath + ".lock", metricsPath, metricsPath + ".lock"} {
		if err := addPath(path); err != nil {
			return nil, err
		}
	}
	roles := map[string]bool{}
	sources := map[protocol.ValidatorProgressSource]bool{}
	for _, validator := range policy.Validators {
		if err := validator.SteeringLiveness.validate(); err != nil {
			return nil, err
		}
		if err := validator.NativeDeadline.validate(); err != nil {
			return nil, err
		}
		if !monitorRolePattern.MatchString(validator.Role) || roles[validator.Role] {
			return nil, errors.New("service roles must be unique bounded names")
		}
		roles[validator.Role] = true
		source := validator.ExpectedSource
		probe := protocol.ValidatorProgress{Schema: protocol.ValidatorProgressSchema, Source: source,
			InstanceId: strings.Repeat("1", 32), StartedAt: "2000-01-01T00:00:00Z", HeartbeatAt: "2000-01-01T00:00:00Z",
			Publisher: protocol.ValidatorPublicationObservation{Outcome: "starting"}}
		if err := probe.Validate(); err != nil || source.ChainId != expected.EvmChainId || !strings.EqualFold(source.GenesisHash, expected.GenesisHash) {
			return nil, errors.New("service expected source is incomplete or differs from approved chain")
		}
		// One producer cannot impersonate two independently expected roles by
		// varying only its running config hash.
		source.ConfigHash = ""
		if sources[source] {
			return nil, errors.New("service census repeats a producer identity")
		}
		sources[source] = true
		checkpoint, metrics := monitorValidatorPaths(checkpointPath, metricsPath, validator.Role)
		for _, path := range []string{validator.ProgressFile, checkpoint, checkpoint + ".lock", metrics, metrics + ".lock"} {
			if err := addPath(path); err != nil {
				return nil, err
			}
		}
	}

	operatorSources := map[string]bool{}
	for _, operator := range policy.Operators {
		if err := operator.validate(expected); err != nil {
			return nil, err
		}
		if roles[operator.Role] {
			return nil, errors.New("service roles must be unique bounded names")
		}
		roles[operator.Role] = true
		source := operator.ExpectedSource
		key := source.Database + "\x00" + source.DeploymentKey() + "\x00" + fmt.Sprint(source.OperatorId)
		if operatorSources[key] {
			return nil, errors.New("operator census repeats a source role")
		}
		operatorSources[key] = true
		for _, path := range monitorOperatorInputPaths(operator, checkpointPath, metricsPath) {
			if err := addPath(path); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(policy.Operators, func(a, b monitorOperatorPolicy) int { return strings.Compare(a.Role, b.Role) })
	providerSources := map[protocol.ProviderProgressSource]bool{}
	providerEndpoints := map[string]bool{}
	providerClients := map[string]bool{}
	for index := range policy.Providers {
		provider := &policy.Providers[index]
		if err := provider.validate(); err != nil {
			return nil, err
		}
		if roles[provider.Role] || providerSources[provider.ExpectedSource] || providerEndpoints[provider.Endpoint] {
			return nil, errors.New("provider role, endpoint or source repeats the independent census")
		}
		roles[provider.Role], providerSources[provider.ExpectedSource], providerEndpoints[provider.Endpoint] = true, true, true
		for _, member := range provider.Members {
			if providerClients[member.ClientId] {
				return nil, errors.New("provider client identity appears in two expected roles")
			}
			providerClients[member.ClientId] = true
		}
		slices.SortFunc(provider.Members, func(a, b monitorExpectedProviderMember) int { return strings.Compare(a.Slot, b.Slot) })
		checkpoint, metrics := monitorProviderPaths(checkpointPath, metricsPath, provider.Role)
		for _, path := range []string{checkpoint, checkpoint + ".lock", metrics, metrics + ".lock"} {
			if err := addPath(path); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(policy.Providers, func(a, b monitorProviderPolicy) int { return strings.Compare(a.Role, b.Role) })
	claimSources := map[string]bool{}
	claimPools := map[protocol.ClaimProgressPool]bool{}
	for index := range policy.Claims {
		claim := &policy.Claims[index]
		if err := claim.validate(expected); err != nil {
			return nil, err
		}
		key := claim.Endpoint + "\x00" + claim.ExpectedMember
		if roles[claim.Role] || claimSources[key] || claimPools[claim.ExpectedPool] {
			return nil, errors.New("claim role, endpoint member or pool repeats the independent census")
		}
		roles[claim.Role], claimSources[key], claimPools[claim.ExpectedPool] = true, true, true
		slices.SortFunc(claim.Epochs, func(a, b monitorClaimEpochPolicy) int {
			if a.Epoch < b.Epoch {
				return -1
			}
			if a.Epoch > b.Epoch {
				return 1
			}
			return 0
		})
		checkpoint, metrics := monitorClaimPaths(checkpointPath, metricsPath, claim.Role)
		for _, path := range []string{checkpoint, checkpoint + ".lock", metrics, metrics + ".lock"} {
			if err := addPath(path); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(policy.Claims, func(a, b monitorClaimPolicy) int { return strings.Compare(a.Role, b.Role) })
	economicSources := map[string]bool{}
	for _, economic := range policy.NativeEconomics {
		if err := economic.validate(expected); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s/%d/%d/%d", economic.Observation.Network.GenesisHash, economic.Observation.Netuid, *economic.Observation.SubnetRegistrationBlock, *economic.Observation.SubnetGeneration)
		if roles[economic.Role] || economicSources[key] {
			return nil, errors.New("native economic role or subnet generation repeats the independent census")
		}
		roles[economic.Role], economicSources[key] = true, true
		checkpoint, metrics := monitorEconomicNativePaths(checkpointPath, metricsPath, economic.Role)
		for _, path := range []string{checkpoint, checkpoint + ".lock", metrics, metrics + ".lock"} {
			if err := addPath(path); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(policy.NativeEconomics, func(a, b monitorEconomicNativePolicy) int { return strings.Compare(a.Role, b.Role) })
	evmSources := map[string]bool{}
	for _, economic := range policy.EvmEconomics {
		if err := economic.validate(expected); err != nil {
			return nil, err
		}
		key := economic.EvmGenesisHash + "/" + economic.Address
		if roles[economic.Role] || evmSources[key] {
			return nil, errors.New("EVM economic role or contract repeats the independent census")
		}
		roles[economic.Role], evmSources[key] = true, true
		checkpoint, metrics := monitorEconomicEvmPaths(checkpointPath, metricsPath, economic.Role)
		for _, path := range []string{checkpoint, checkpoint + ".lock", metrics, metrics + ".lock"} {
			if err := addPath(path); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(policy.EvmEconomics, func(a, b monitorEconomicEvmPolicy) int { return strings.Compare(a.Role, b.Role) })
	slices.SortFunc(policy.Validators, func(a, b monitorValidatorPolicy) int { return strings.Compare(a.Role, b.Role) })
	return &policy, nil
}

// Distinct fixed role suffixes keep per-domain writes and locks independent.
func monitorValidatorPaths(checkpointPath, metricsPath, role string) (string, string) {
	return strings.TrimSuffix(checkpointPath, ".json") + ".validator-" + role + ".json",
		strings.TrimSuffix(metricsPath, ".prom") + ".validator-" + role + ".prom"
}
