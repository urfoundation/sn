//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/urfoundation/sn/crv4"
)

// This read-only source observation is not a Verified measurement. The final
// archive still replays all cuts, statistics, scoring and public source facts.
type ReleaseNativeSourceReferenceV2 struct {
	Intent SteeringIntent
	Artifact *ReleaseMeasurementArtifact
	Envelope []byte
	Lifecycle ReleaseEvidenceV2DecisionObservation
}

type ReleaseNativeSourceObservationV2 struct {
	StoreSHA256 string
	References []ReleaseNativeSourceReferenceV2
}

// ObserveReleaseNativeSourcesV2 reads only the exact configured coordinator
// namespace. It authenticates original signed source bytes, native inclusion
// and application rows; it never opens a live ledger or constructs a writer.
func ObserveReleaseNativeSourcesV2(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, hotkey [32]byte, adoption *ReleaseHistoryAdoptionV2) (result *ReleaseNativeSourceObservationV2, resultErr error) {
	if ctx == nil || cfg == nil || native == nil || native.API == nil || native.API.Client == nil || hotkey == ([32]byte{}) {
		return nil, errors.New("V2 native observation owner is incomplete")
	}
	ownedConfig := *cfg
	cfg = &ownedConfig
	if adoption != nil {
		ownedAdoption := *adoption
		adoption = &ownedAdoption
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.ProvisionalDeferClosedNativeInput {
		return nil, errors.New("V2 strict native observation cannot use provisional gap authority")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil { result = nil }
	}()
	result, err := readReleaseNativeSourceReferencesV2(ctx, cfg, hotkey, adoption)
	if err != nil { return nil, err }
	for index := range result.References {
		value := &result.References[index]
		if err := authenticateReleaseNativeSourceReferenceV2(ctx, native, cfg, &value.Intent, value.Artifact); err != nil { return nil, err }
		value.Lifecycle.MeasurementHash = value.Intent.MeasurementArtifactHash
		if err := observeReleaseDecisionLifecycleV2(ctx, native, cfg, &value.Intent, &value.Lifecycle); err != nil { return nil, err }
		if value.Intent.Status == "applied" {
			if err := authenticateAdoptedIntentApplicationV2(ctx, native, cfg, &value.Intent); err != nil { return nil, err }
		}
	}
	return result, nil
}

// Close the bounded local snapshot before historical RPC. Later producer
// appends are another observation, not permission to change these owned bytes.
func readReleaseNativeSourceReferencesV2(ctx context.Context, cfg *ReleaseConfig, hotkey [32]byte, adoption *ReleaseHistoryAdoptionV2) (result *ReleaseNativeSourceObservationV2, resultErr error) {
	if ctx == nil || cfg == nil || hotkey == ([32]byte{}) || !filepath.IsAbs(cfg.StateDir) || cfg.EvidenceV2.Bounds.MaxHistoryBytes == 0 {
		return nil, errors.New("V2 native source has no bounded private owner")
	}
	bounds := cfg.EvidenceV2.Bounds
	owned := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
	defer func() {
		resultErr = errors.Join(resultErr, owned.check(), owned.close(), ctx.Err())
		if resultErr != nil { result = nil }
	}()
	raw, err := owned.read(ctx, filepath.Join(cfg.StateDir, "steering-intents.json"), bounds.IntentFileLimit(), true)
	if err != nil { return nil, err }
	checkPublication := func() error {
		for _, name := range []string{releaseIntentV2Marker, releaseIntentV2Candidate} {
			_, err := owned.owners[0].directory.stat(name)
			if !releaseMeasurementInputV2OnlyMissing(err) { return errors.Join(errors.New("V2 native publication is unresolved"), err) }
		}
		return nil
	}
	if err := checkPublication(); err != nil { return nil, err }
	defer func() { resultErr = errors.Join(resultErr, checkPublication()); if resultErr != nil { result = nil } }()
	file := steeringIntentFile{Schema: SteeringIntentSchema}
	if raw != nil {
		if err := decodeAttemptStreamV2JSON(raw, bounds.IntentFileLimit(), &file); err != nil { return nil, err }
		canonical, err := marshalAttemptSettlementV2JSON(ctx, &file, bounds.IntentFileLimit(), true, true)
		if err != nil || file.Schema != SteeringIntentSchema || !bytes.Equal(canonical, raw) { return nil, errors.Join(errors.New("V2 native source store is not canonical"), err) }
	}
	if adoption != nil && (adoption.DeploymentID != cfg.DeploymentID || adoption.ValidatorID != cfg.ValidatorID || adoption.CoordinatorStateDir != cfg.StateDir) {
		return nil, errors.New("V2 native observation changes adopted namespace")
	}
	prefix, err := adoption.matchPrefix(ctx, &file, raw, bounds.IntentFileLimit())
	if err != nil { return nil, err }
	all := append([]SteeringIntent(nil), file.History...)
	if file.Current != nil { all = append(all, *file.Current) }
	result = &ReleaseNativeSourceObservationV2{StoreSHA256: ReleaseMeasurementContentHash(raw)}
	history := &releaseEvidenceV2StartupHistory{cfg: *cfg}
	var previous *SteeringIntent
	var priorArtifact *ReleaseMeasurementArtifact
	seen := map[string]bool{}
	for index := range all {
		intent := &all[index]
		if err := validateSteeringIntentLifecycle(intent, index < len(file.History)); err != nil { return nil, err }
		if err := intent.VerifyVectorHash(); err != nil { return nil, err }
		if seen[intent.VectorHash] { return nil, errors.New("V2 native source repeats an intent") }
		seen[intent.VectorHash] = true
		allowGap := prefix.allowsIntentEdge(previous, intent)
		if previous != nil {
			if err := validateSteeringIntentSuccessorWithGapsV2(previous, intent, allowGap); err != nil { return nil, err }
		}
		measurement, err := history.readContentReference(ctx, owned, intent.MeasurementArtifactPath, intent.MeasurementArtifactHash, intent.MeasurementArtifactSize, bounds.MaxArtifactBytes, false)
		if err != nil { return nil, err }
		envelopeRaw, err := history.readContentReference(ctx, owned, intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize, bounds.MaxControlBytes, true)
		if err != nil { return nil, err }
		artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
		if err != nil { return nil, err }
		envelope, err := DecodeReleaseMeasurementEnvelopeV2(ctx, envelopeRaw, bounds.MaxControlBytes)
		if err != nil { return nil, err }
		if err := matchObservedNativeSourceV2(cfg, hotkey, intent, artifact, envelope); err != nil { return nil, err }
		if previous == nil {
			if artifact.PreviousArtifactHash != "" { return nil, errors.New("V2 native source omits its first measurement") }
		} else {
			if artifact.PreviousArtifactHash != previous.MeasurementArtifactHash || !allowGap && !consecutiveNativeSettlementGapV2(priorArtifact, artifact) && artifact.SettlementEpoch > priorArtifact.SettlementEpoch+1 {
				return nil, errors.New("V2 native source changes its predecessor or permitted clocks")
			}
			if err := verifyReleaseMeasurementHeadLineage(priorArtifact, artifact); err != nil { return nil, err }
		}
		result.References = append(result.References, ReleaseNativeSourceReferenceV2{Intent: *intent, Artifact: artifact, Envelope: bytes.Clone(envelopeRaw)})
		previous, priorArtifact = intent, artifact
	}
	return result, nil
}

func matchObservedNativeSourceV2(cfg *ReleaseConfig, hotkey [32]byte, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact, envelope *ReleaseMeasurementEnvelope) error {
	if intent == nil || intent.Prepared == nil || artifact == nil || envelope == nil { return errors.New("V2 native observation reference is incomplete") }
	if err := verifyReleaseMeasurementCommonIdentity(artifact); err != nil { return err }
	if artifact.Schema != ReleaseMeasurementSchemaV2 || artifact.DeploymentID != cfg.DeploymentID || artifact.ChainID != cfg.ChainID || artifact.GenesisHash != strings.ToLower(cfg.GenesisHash) || artifact.Coordinator != strings.ToLower(cfg.Coordinator) || artifact.SettlementVault != strings.ToLower(cfg.SettlementVault) || artifact.PolicyHash != strings.ToLower(cfg.PolicyHash) || artifact.ValidatorID != cfg.ValidatorID || artifact.Netuid != cfg.Netuid || artifact.ValidatorID != intent.ValidatorID || artifact.Netuid != intent.Netuid || artifact.PolicyHash != intent.PolicyHash || artifact.SubnetEpoch != intent.SubnetEpoch || artifact.SettlementEpoch != intent.SettlementEpoch || artifact.SelfUID != intent.SelfUID || artifact.NativeSnapshotBlock != intent.NativeSnapshotBlock || artifact.NativeSnapshotHash != intent.NativeSnapshotHash || artifact.EVMSnapshotBlock != intent.EVMSnapshotBlock || artifact.EVMSnapshotHash != intent.EVMSnapshotHash || !reflect.DeepEqual(artifact.DepositAudits, intent.DepositAudits) {
		return errors.New("V2 native source differs from its deployment or intent")
	}
	if envelope.ValidatorHotkey != releaseHex32(hotkey) || intent.Prepared.HotkeyHex != envelope.ValidatorHotkey || intent.Prepared.Netuid != intent.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || envelope.PreparedExtrinsicHash != intent.Prepared.ExtrinsicHash || envelope.MeasurementArtifactHash != intent.MeasurementArtifactHash || envelope.MeasurementArtifactSize != intent.MeasurementArtifactSize || !releaseMeasurementEnvelopeMatchesArtifact(envelope, artifact, intent.SelfUID) || !slices.Equal(intent.UIDs, intent.Prepared.UIDs) || (intent.Status == "applied" || intent.Status == "finalized") && !slices.Equal(intent.Values, intent.Prepared.Values) {
		return errors.New("V2 native source does not bind its exact original signed envelope")
	}
	return nil
}
