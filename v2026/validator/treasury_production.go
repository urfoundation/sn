//go:build linux || darwin

// Treasury tooling enters the same immutable approval and measured V2 owners
// as production. These explicit entry points never load a treasury signing key.
package validator

import (
	"context"
	"errors"

	"github.com/urfoundation/sn/v2026/crv4"
)

const treasuryMeasurementSchema = "urnetwork-native-treasury-measurement-v1"
const treasuryDecisionIntentSchema = "urnetwork-native-treasury-decision-intent-v1"

// The observation retains owner recognition as exclusions and ordinary
// treasury registrations separately; its booleans grant no launch authority.
type TreasuryAdmissionObservation = OwnerRecycleAdmissionObservation
type TreasuryMeasurementAuthority = OwnerRecycleMeasurementAuthority
type TreasuryMeasuredRow = OwnerRecycleMeasuredRow
type TreasuryDecisionIntent = OwnerRecycleDecisionIntent

// Requires the exact loaded treasury approval before the shared read owner runs.
func ObserveTreasuryAdmission(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain) (*TreasuryAdmissionObservation, error) {
	if cfg == nil || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return nil, errors.New("treasury observation requires its explicit production selector")
	}
	return ObserveOwnerRecycleAdmission(ctx, cfg, native)
}

// Historical replays consume the original registration generation and never
// infer old custody from today's state or another approval.
func ObserveTreasuryAdmissionAt(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, blockHash [32]byte) (*TreasuryAdmissionObservation, error) {
	if cfg == nil || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return nil, errors.New("treasury historical observation requires its explicit production selector")
	}
	return ObserveOwnerRecycleAdmissionAt(ctx, cfg, native, blockHash)
}

// Exact provider decision inputs remain unchanged inside the successor proof.
func ObserveTreasuryMeasurementAuthority(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, expected ReleaseMeasurementV2Decision) (*TreasuryMeasurementAuthority, error) {
	if cfg == nil || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return nil, errors.New("treasury measurement requires its explicit production selector")
	}
	return ObserveOwnerRecycleMeasurementAuthority(ctx, cfg, native, expected)
}

// Review capsules are distinct from signed production intents and transactions.
func SealTreasuryMeasurement(ctx context.Context, authority *TreasuryMeasurementAuthority, provider []byte, options ReleaseMeasurementV2Options) ([]byte, *TreasuryDecisionIntent, error) {
	if authority == nil || authority.config.TreasuryApproval == nil {
		return nil, nil, errors.New("treasury capsule lacks independently observed treasury authority")
	}
	return SealOwnerRecycleMeasurement(ctx, authority, provider, options)
}

// Replays the complete original provider proof under the retained approval.
func ReplayTreasuryMeasurement(ctx context.Context, authority *TreasuryMeasurementAuthority, encoded []byte, options ReleaseMeasurementV2Options) (*TreasuryDecisionIntent, error) {
	if authority == nil || authority.config.TreasuryApproval == nil {
		return nil, errors.New("treasury replay lacks independently observed treasury authority")
	}
	return ReplayOwnerRecycleMeasurement(ctx, authority, encoded, options)
}

// Retains exact bytes through the established no-replace file/directory owner.
func RetainTreasuryApproval(ctx context.Context, cfg *ReleaseConfig) (ReleaseEvidenceV2File, error) {
	if cfg == nil || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return ReleaseEvidenceV2File{}, errors.New("treasury retention requires its explicit production selector")
	}
	return RetainOwnerRecycleApproval(ctx, cfg)
}

// The resulting bundle can be selected as historical authority only; exporting
// it never signs an approval or authorizes a fresh native transaction.
func BuildTreasuryProductionAuthority(ctx context.Context, cfg *ReleaseConfig) ([]byte, error) {
	if cfg == nil || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return nil, errors.New("treasury export requires its explicit production selector")
	}
	return BuildOwnerRecycleProductionAuthority(ctx, cfg)
}
