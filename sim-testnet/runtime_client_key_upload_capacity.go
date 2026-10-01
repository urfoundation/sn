package main

// Full-population observation admission forecasts logical quota charges,
// not resident memory or stored bytes. Rpc batching never reduces that charge.

import (
	"errors"
	"fmt"

	"github.com/urfoundation/sn/v2026/protocol"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026/model"
)

// Preserve the existing ordinary traffic allowance in addition to the new
// observations; assigning all shared bytes to key reads would starve uploads.
func runtimeOrdinaryUploadAllowance() model.StAttemptUploadBudget {
	return model.StAttemptUploadBudget{
		RequestsPerHour: 32768, BytesPerHour: 1024 * 1024 * 1024,
		AccountRequestsPerHour: 4096, AccountBytesPerHour: 128 * 1024 * 1024,
	}
}

// One partial native window is included at the hour boundary. Every native
// decision and every permitted process restart receives the real retry limit.
// These are the configured cadence bounds, not a claim about measured traffic
// or arbitrary accelerated chains. Both Rpc modes use the same accounting.
func requiredRuntimeClientKeyUploadBudget(cfg *ResolvedConfig) (model.StAttemptUploadBudget, error) {
	var zero model.StAttemptUploadBudget
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Hyperparameters == nil {
		return zero, errors.New("client-key upload workload has no complete configuration")
	}
	topology := cfg.Config.Topology
	if topology.Operators <= 0 || topology.Miners <= 0 || topology.Validators <= 0 || topology.OperatorAssignment != "balanced" || topology.Miners%topology.Operators != 0 {
		return zero, errors.New("client-key upload workload has an invalid full-population assignment")
	}
	// Reject every overflow before forming an intermediate Lua counter. Even
	// a later subtraction must never conceal an unrepresentable reservation.
	const maximum = uint64(9007199254740991)
	add := func(left, right uint64) (uint64, error) {
		if left > maximum || right > maximum-left {
			return 0, errors.New("client-key upload workload exceeds exact counters")
		}
		return left + right, nil
	}
	multiply := func(left, right uint64) (uint64, error) {
		if left > maximum || right > maximum || right != 0 && left > maximum/right {
			return 0, errors.New("client-key upload workload exceeds exact counters")
		}
		return left * right, nil
	}
	normalized, err := normalizeYAMLValue(cfg.Hyperparameters.OwnerControlled["tempo"], hyperShapes["tempo"].Kind)
	if err != nil {
		return zero, fmt.Errorf("client-key upload native tempo: %w", err)
	}
	tempo, validTempo := normalized.(uint64)
	if !validTempo || tempo == 0 || cfg.Public.Chain.ExpectedBlockSeconds == 0 {
		return zero, errors.New("client-key upload workload has no native cadence")
	}
	period, err := add(tempo, 1)
	if err != nil {
		return zero, err
	}
	periodSeconds, err := multiply(period, cfg.Public.Chain.ExpectedBlockSeconds)
	if err != nil || periodSeconds == 0 {
		return zero, errors.Join(errors.New("client-key upload native period exceeds its bound"), err)
	}
	decisions := uint64(1) + uint64(3599)/periodSeconds + 1
	attempts, err := multiply(decisions+uint64(validatorProcessRestartLimit), uint64(validatorcomponent.ReleaseSteeringFailureLimit))
	if err != nil {
		return zero, err
	}
	perAccount, err := multiply(uint64(topology.Miners/topology.Operators), attempts)
	if err != nil {
		return zero, err
	}
	perAccount, err = multiply(perAccount, uint64(protocol.MaxClientKeyObservationReservationAttempts))
	if err != nil {
		return zero, err
	}
	perDeployment, err := multiply(perAccount, uint64(topology.Validators))
	if err != nil {
		return zero, err
	}
	accountBytes, err := multiply(perAccount, uint64(protocol.MaxClientKeyHistoryResponseBytes))
	if err != nil {
		return zero, err
	}
	deploymentBytes, err := multiply(perDeployment, uint64(protocol.MaxClientKeyHistoryResponseBytes))
	if err != nil {
		return zero, err
	}
	attackRate := cfg.Config.Scenarios.Adversaries.MaximumOperatorRequestsPerSec
	if attackRate < 1 || attackRate > 20 {
		return zero, errors.New("client-key upload workload has no finite adversarial request rate")
	}
	attackRequests := uint64(attackRate) * 3600
	attackBytes, err := multiply(attackRequests, uint64(model.StAttemptUploadMaximumObjectBytes))
	if err != nil {
		return zero, err
	}
	budget := runtimeOrdinaryUploadAllowance()
	for _, field := range []struct {
		value      *uint64
		additional uint64
	}{
		{value: &budget.AccountRequestsPerHour, additional: perAccount},
		{value: &budget.AccountBytesPerHour, additional: accountBytes},
		{value: &budget.RequestsPerHour, additional: perDeployment},
		{value: &budget.RequestsPerHour, additional: attackRequests},
		{value: &budget.BytesPerHour, additional: deploymentBytes},
		{value: &budget.BytesPerHour, additional: attackBytes},
	} {
		*field.value, err = add(*field.value, field.additional)
		if err != nil {
			return zero, err
		}
	}
	if err := budget.Validate(); err != nil {
		return zero, err
	}
	return budget, nil
}

// Supplied quota is never auto-corrected. Equality passes; each one-byte or
// one-request shortfall refuses before launch mutation, wallet or Rpc work.
func validateRuntimeClientKeyUploadCapacity(cfg *ResolvedConfig, actual model.StAttemptUploadBudget) error {
	minimum, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil {
		return err
	}
	for _, field := range []struct {
		name    string
		actual  uint64
		minimum uint64
	}{
		{name: "requests_per_hour", actual: actual.RequestsPerHour, minimum: minimum.RequestsPerHour},
		{name: "bytes_per_hour", actual: actual.BytesPerHour, minimum: minimum.BytesPerHour},
		{name: "account_requests_per_hour", actual: actual.AccountRequestsPerHour, minimum: minimum.AccountRequestsPerHour},
		{name: "account_bytes_per_hour", actual: actual.AccountBytesPerHour, minimum: minimum.AccountBytesPerHour},
	} {
		if field.actual < field.minimum {
			return fmt.Errorf("artifacts.attempt_upload.%s=%d is below full-population client-key workload minimum %d", field.name, field.actual, field.minimum)
		}
	}
	return nil
}
