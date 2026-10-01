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

	"github.com/urfoundation/sn/v2026/protocol"
)

const monitorServicesSchema = "urnetwork-mainnet-monitor-services-v1"
const maxMonitorServicesBytes = 16 * 1024
const maxMonitorValidatorRoles = 8

// Callers distinguish an exhausted role census from unrelated source or wire faults.
var errMonitorServicesCensus = errors.New("service policy requires at least one role, at most eight validators and at most four operators")

// Roles are fixed by the local expected census, with no candidate-supplied
// label values. Each role has independent source, checkpoint and metric owners.
type monitorServicesPolicy struct {
	Schema     string                   `json:"schema"`
	Validators []monitorValidatorPolicy `json:"validators"`
	Operators  []monitorOperatorPolicy  `json:"operators,omitempty"`
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
	raw, err := readMonitorServiceFile(ctx, path, maxMonitorServicesBytes, false, monitorServiceReadHooks{})
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
	if len(policy.Validators)+len(policy.Operators) == 0 || len(policy.Validators) > maxMonitorValidatorRoles || len(policy.Operators) > maxMonitorOperators {
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
	slices.SortFunc(policy.Validators, func(a, b monitorValidatorPolicy) int { return strings.Compare(a.Role, b.Role) })
	return &policy, nil
}

// Distinct fixed role suffixes keep per-domain writes and locks independent.
func monitorValidatorPaths(checkpointPath, metricsPath, role string) (string, string) {
	return strings.TrimSuffix(checkpointPath, ".json") + ".validator-" + role + ".json",
		strings.TrimSuffix(metricsPath, ".prom") + ".validator-" + role + ".prom"
}
