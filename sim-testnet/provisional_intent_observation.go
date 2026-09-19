//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/urfoundation/sn/ss58"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// The completed live handoff, manifest and actual argv select the state owner.
// A failed observation never supplies strict authenticated scenario counters.
func observeProvisionalValidatorIntent(ctx context.Context, cfg *ResolvedConfig, stateDir string, validatorID int) (ValidatorObservation, string) {
	result := ValidatorObservation{ValidatorID: validatorID, LocalRuntimeIntents: &validatorpkg.ProvisionalIntentObservationV2{
		Scope: "local-runtime-observation", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "unknown",
	}}
	fail := func(err error) (ValidatorObservation, string) {
		result.LocalRuntimeIntents.Error = fmt.Sprintf("%.1024s", err.Error())
		result.Error = "local runtime intents: " + result.LocalRuntimeIntents.Error
		return result, ""
	}
	if !provisionalResumeEnabled(cfg) || cfg.provisionalResume.Record == nil {
		return fail(errors.New("provisional intent observation has no admitted plan"))
	}
	acceptedPlanHashes := cfg.provisionalResume.AcceptedPlanHashes
	if len(acceptedPlanHashes) == 0 || !slices.Contains(acceptedPlanHashes, cfg.provisionalResume.Record.PlanHash) {
		return fail(errors.New("provisional intent observation has no authenticated plan lineage"))
	}
	var adoption provisionalLiveTopology
	if err := readJSONFile(filepath.Join(stateDir, "provisional-resumes", "live-topology.json"), &adoption); err != nil {
		return fail(err)
	}
	if adoption.Schema != "urnetwork-sim-provisional-live-topology-v1" || !adoption.Provisional || adoption.FinalAcceptance || adoption.CompletedAt == "" || adoption.PlanHash != cfg.provisionalResume.Record.PlanHash {
		return fail(errors.New("local intent observation lacks the completed live handoff"))
	}
	var live SupervisorState
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.state.json"), &live); err != nil {
		return fail(err)
	}
	if err := provisionalAdoptionGeneration(&adoption, live); err != nil {
		return fail(err)
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "supervisor.json", 1024*1024)
	if err != nil {
		return fail(err)
	}
	var manifest SupervisorFile
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fail(err)
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil || hash != adoption.ManifestHash || bytesSHA256(raw) != adoption.ManifestBytesSHA256 || manifest.DeploymentID != cfg.Config.Deployment.DeploymentID {
		return fail(errors.New("local intent manifest differs from the admitted live generation"))
	}
	id := fmt.Sprintf("validator-%d", validatorID)
	for _, spec := range manifest.Specs {
		if spec.ID != id {
			continue
		}
		if spec.Role != "validator" || len(spec.Args) == 0 || spec.Args[0] != "__validator" {
			return fail(errors.New("local intent owner is not a validator process"))
		}
		matched := false
		generation := ""
		for _, process := range live.Processes {
			if process.ID != id {
				continue
			}
			if process.Role != spec.Role || process.Identity != spec.Identity || process.PID <= 1 {
				return fail(errors.New("local intent process differs from its manifest"))
			}
			args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", process.PID))
			argv := strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00")
			if err != nil || len(argv) < 2 || strings.Join(argv[1:], "\x00") != strings.Join(spec.Args, "\x00") {
				return fail(errors.New("local intent process arguments differ from its handoff"))
			}
			ticks, err := processStartTimeTicks(process.PID)
			if err != nil {
				return fail(err)
			}
			generation = fmt.Sprintf("%d:%d:%s:%d:%d:%s", live.SupervisorPID, live.SupervisorStartTimeTicks, adoption.ManifestBytesSHA256, process.PID, ticks, bytesSHA256(args))
			matched = true
		}
		if !matched {
			return fail(errors.New("local intent validator is absent from the live generation"))
		}
		var configPath, handoffPath, handoffHash string
		fs := flag.NewFlagSet(id, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.StringVar(&configPath, "config", "", "")
		fs.StringVar(&handoffPath, "provisional-activation-setup", "", "")
		fs.StringVar(&handoffHash, "provisional-activation-setup-sha256", "", "")
		if err := fs.Parse(spec.Args[1:]); err != nil || fs.NArg() != 0 || configPath != filepath.Join(stateDir, "runtime", id, "validator.yml") || handoffPath == "" || handoffHash == "" {
			return fail(errors.New("local intent validator lacks its explicit activation handoff"))
		}
		handoff, err := readProvisionalActivationSetup(configPath, handoffPath)
		if err != nil {
			return fail(err)
		}
		hotkey, err := ss58.DecodeWithPrefix(spec.Identity, ss58.BittensorPrefix)
		if err != nil {
			return fail(err)
		}
		observed, err := validatorpkg.ObserveProvisionalIntentsV2(ctx, validatorpkg.ProvisionalIntentObservationV2Options{
			ConfigPath: configPath, Handoff: handoff, HandoffSHA256: handoffHash, PlanHash: cfg.provisionalResume.Record.PlanHash,
			AcceptedPlanHashes: slices.Clone(acceptedPlanHashes), DeploymentID: manifest.DeploymentID, ValidatorID: uint64(validatorID), Netuid: cfg.Netuid, Hotkey: hotkey,
		})
		result.LocalRuntimeIntents = observed
		if err != nil {
			return fail(err)
		}
		return result, generation
	}
	return fail(errors.New("local intent validator is missing from the manifest"))
}

// Observe both sides of the local projection. Neither the shared projection nor
// local receipt claims can grant strict source replay or final acceptance.
func inspectProvisionalValidatorIntent(ctx context.Context, cfg *ResolvedConfig, stateDir string, validatorID int) ValidatorObservation {
	return inspectProvisionalValidatorIntentObserved(ctx, cfg, validatorID, func() (ValidatorObservation, string) {
		return observeProvisionalValidatorIntent(ctx, cfg, stateDir, validatorID)
	})
}

func inspectProvisionalValidatorIntentObserved(ctx context.Context, cfg *ResolvedConfig, validatorId int, observe func() (ValidatorObservation, string)) ValidatorObservation {
	failure := func(err error) ValidatorObservation {
		return ValidatorObservation{ValidatorID: validatorId, Error: "local runtime intents: " + err.Error(), LocalRuntimeIntents: &validatorpkg.ProvisionalIntentObservationV2{
			Scope: "local-runtime-observation", State: "unknown", Error: fmt.Sprintf("%.1024s", err.Error()),
		}}
	}
	if ctx == nil || !provisionalResumeEnabled(cfg) || cfg.provisionalResume.Record == nil || !cfg.provisionalResume.Record.Provisional || cfg.provisionalResume.Record.FinalAcceptance || cfg.Config == nil || observe == nil {
		return failure(errors.New("local intent projection has no provisional owner"))
	}
	before, generation := observe()
	local := before.LocalRuntimeIntents
	if before.Error != "" {
		return failure(fmt.Errorf("local intent observation: %s", before.Error))
	}
	if local != nil && local.Error != "" {
		return failure(fmt.Errorf("local intent observation: %s", local.Error))
	}
	if before.ValidatorID != validatorId || generation == "" || local == nil || local.Scope != "local-runtime-observation" || local.FinalAcceptance || (local.State != "observed" && local.State != "absent") {
		return failure(errors.New("local intent projection has no authenticated observation"))
	}
	result := ValidatorObservation{ValidatorID: validatorId}
	if local.State == "observed" {
		if !validSHA256ContentHash(local.StoreSHA256) || !validSHA256ContentHash(local.HandoffSHA256) || !filepath.IsAbs(local.StateDirectory) {
			return failure(errors.New("local intent projection lacks its pinned store"))
		}
		raw, err := readValidatorEvidenceHistoricalFile(local.StateDirectory, "steering-intents.json", maximumCampaignEvidenceRawFileBytes)
		if err != nil || bytesSHA256(raw) != local.StoreSHA256 {
			return failure(errors.Join(errors.New("local intent store changed before projection"), err))
		}
		var file struct {
			Schema  string                        `json:"schema"`
			Current *validatorpkg.SteeringIntent  `json:"current,omitempty"`
			History []validatorpkg.SteeringIntent `json:"history"`
		}
		if err := json.Unmarshal(raw, &file); err != nil || file.Schema != validatorpkg.SteeringIntentSchema {
			return failure(errors.Join(errors.New("local intent projection store differs"), err))
		}
		all := append([]validatorpkg.SteeringIntent(nil), file.History...)
		if file.Current != nil {
			all = append(all, *file.Current)
		}
		if len(all) != len(local.Receipts) {
			return failure(errors.New("local intent projection receipt census differs"))
		}
		for index := range all {
			intent, receipt := &all[index], local.Receipts[index]
			if intent.ValidatorID != uint64(validatorId) || intent.Netuid != cfg.Netuid || intent.PolicyHash != cfg.PolicyHash || intent.Status != receipt.Status || intent.VectorHash != receipt.VectorHash || intent.SubnetEpoch != receipt.SubnetEpoch {
				return failure(errors.New("local intent projection identity differs"))
			}
			if err := intent.VerifyVectorHash(); err != nil {
				return failure(err)
			}
		}
		result = projectValidatorIntent(all, validatorId, cfg.Config.Topology.HeadSlots, cfg.Config.Topology.fleetCandidates(), func(intent *validatorpkg.SteeringIntent) (*validatorpkg.ReleaseMeasurementArtifact, error) {
			return readProvisionalIntentMeasurement(ctx, cfg, local.StateDirectory, intent)
		})
		// The counters above belong only to the shared projection. Local claims
		// stay in LocalRuntimeIntents and never become strict chain evidence.
		result.FinalizedIntents, result.AppliedIntents = 0, 0
		for _, decision := range result.HeadDecisions {
			if decision.Error != "" {
				return failure(errors.New(decision.Error))
			}
		}
	}
	after, currentGeneration := observe()
	if ctx.Err() != nil || after.Error != "" || after.ValidatorID != validatorId || after.LocalRuntimeIntents == nil || currentGeneration != generation {
		return failure(errors.New("local intent generation changed during projection"))
	}
	first, last := *local, *after.LocalRuntimeIntents
	first.ObservedAt, last.ObservedAt = "", ""
	if !reflect.DeepEqual(first, last) {
		return failure(errors.New("local intent store or handoff changed during projection"))
	}
	result.LocalRuntimeIntents = after.LocalRuntimeIntents
	return result
}

// This is a local signed-content join, not a Verified measurement decision.
// Full policy/lineage replay and native receipt authentication remain deferred.
func readProvisionalIntentMeasurement(ctx context.Context, cfg *ResolvedConfig, stateDir string, intent *validatorpkg.SteeringIntent) (*validatorpkg.ReleaseMeasurementArtifact, error) {
	read := func(path, hash string, size uint64, directory string, maximum uint64) ([]byte, error) {
		if !validSHA256ContentHash(hash) || size == 0 || size > maximum || path != directory+"/"+strings.TrimPrefix(hash, "sha256:")+".json" {
			return nil, errors.New("local measurement reference differs")
		}
		encoded, err := readValidatorEvidenceHistoricalFile(stateDir, path, int64(size))
		if err != nil || uint64(len(encoded)) != size || bytesSHA256(encoded) != hash {
			return nil, errors.Join(errors.New("local measurement content differs"), err)
		}
		return encoded, nil
	}
	encoded, err := read(intent.MeasurementArtifactPath, intent.MeasurementArtifactHash, intent.MeasurementArtifactSize, "measurements", 64*1024*1024)
	if err != nil {
		return nil, err
	}
	var artifact validatorpkg.ReleaseMeasurementArtifact
	if err := decodeStrictJSONBytes(encoded, &artifact); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(&artifact)
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) || artifact.Schema != validatorpkg.ReleaseMeasurementSchemaV2 {
		return nil, errors.Join(errors.New("local measurement bytes are not canonical V2"), err)
	}
	if artifact.DeploymentID != cfg.Config.Deployment.DeploymentID || artifact.ChainID != cfg.ChainID || artifact.GenesisHash != cfg.Public.Chain.GenesisHash || artifact.ValidatorID != intent.ValidatorID || artifact.Netuid != intent.Netuid || artifact.PolicyHash != intent.PolicyHash || artifact.SubnetEpoch != intent.SubnetEpoch || artifact.SettlementEpoch != intent.SettlementEpoch || artifact.SelfUID != intent.SelfUID || artifact.NativeSnapshotBlock != intent.NativeSnapshotBlock || artifact.NativeSnapshotHash != intent.NativeSnapshotHash || artifact.EVMSnapshotBlock != intent.EVMSnapshotBlock || artifact.EVMSnapshotHash != intent.EVMSnapshotHash || !reflect.DeepEqual(artifact.DepositAudits, intent.DepositAudits) {
		return nil, errors.New("local measurement identity or decision differs")
	}
	if intent.Prepared == nil || intent.Prepared.Netuid != intent.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || !slices.Equal(intent.Prepared.UIDs, intent.UIDs) || !slices.Equal(intent.Prepared.Values, intent.Values) {
		return nil, errors.New("local measurement prepared decision differs")
	}
	envelopeBytes, err := read(intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize, "measurements/envelopes", validatorpkg.ReleaseMeasurementEnvelopeV2MaximumBytes)
	if err != nil {
		return nil, err
	}
	envelope, err := validatorpkg.DecodeReleaseMeasurementEnvelopeV2(ctx, envelopeBytes, validatorpkg.ReleaseMeasurementEnvelopeV2MaximumBytes)
	if err != nil {
		return nil, err
	}
	if envelope.MeasurementArtifactHash != intent.MeasurementArtifactHash || envelope.MeasurementArtifactSize != intent.MeasurementArtifactSize || envelope.PreparedExtrinsicHash != intent.Prepared.ExtrinsicHash || envelope.ValidatorHotkey != intent.Prepared.HotkeyHex || envelope.ValidatorUID != intent.SelfUID || envelope.MeasurementSchema != artifact.Schema || envelope.DeploymentID != artifact.DeploymentID || envelope.ChainID != artifact.ChainID || envelope.GenesisHash != artifact.GenesisHash || envelope.Coordinator != artifact.Coordinator || envelope.SettlementVault != artifact.SettlementVault || envelope.ValidatorID != artifact.ValidatorID || envelope.Netuid != artifact.Netuid || envelope.SubnetEpoch != artifact.SubnetEpoch || envelope.SettlementEpoch != artifact.SettlementEpoch || envelope.PolicyHash != artifact.PolicyHash || envelope.PreviousArtifactHash != artifact.PreviousArtifactHash || envelope.NativeSnapshotBlock != artifact.NativeSnapshotBlock || envelope.NativeSnapshotHash != artifact.NativeSnapshotHash || envelope.EVMSnapshotBlock != artifact.EVMSnapshotBlock || envelope.EVMSnapshotHash != artifact.EVMSnapshotHash {
		return nil, errors.New("local measurement signed envelope binding differs")
	}
	return &artifact, nil
}
