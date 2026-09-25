//go:build linux || darwin

// Independent read-only capture stages preserve useful sources even when a
// different terminal class fails. Output is never a strict semantic closure.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// Actual local sources are sampled separately from original signed observations.
// A new observation can diagnose stale selectors but cannot rewrite history.
func collectTerminalDiagnosticSources(collector *terminalDiagnosticCollector, cfg *ResolvedConfig, stateDir, runDir string, attempt *scenarioCampaignAttempt, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation, terminalOk bool) {
	available := func(ok bool, reason string) string {
		if ok {
			return ""
		}
		return reason
	}
	var authority *finalOperatorPathAuthority
	authorityOk := collector.check("active-generation-path-authority", "", 5*time.Minute, func(ctx context.Context) (any, error) {
		var err error
		authority, err = loadFinalOperatorPathAuthorityV2(ctx, cfg, stateDir, finalConfiguredValidatorIDs(cfg))
		if err != nil {
			return nil, err
		}
		return authority.pathsByValidator, nil
	})
	for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
		noId := noId
		collector.check(fmt.Sprintf("operator-%d/current-signed-artifacts", noId), available(terminal != nil && terminal.Status != nil && terminal.Status.Contracts != nil && authorityOk, "signed contract checkpoint or public authority unavailable"), 5*time.Minute, func(ctx context.Context) (any, error) {
			signers, err := finalOperatorArtifactSigners(authority.identities, cfg.Config.Deployment.DeploymentID, cfg.Config.Topology.Operators)
			if err != nil {
				return nil, err
			}
			clients, err := inspectMinerClientIDsBytes(cfg, authority.publicBytes)
			if err != nil {
				return nil, err
			}
			probe := &liveScenarioProbe{cfg: cfg, stateDir: stateDir}
			base := fmt.Sprintf("http://127.0.0.1:%d", 18080+noId)
			var retries []evidenceRelayRetryObservation
			surfaces, err := readTerminalDiagnosticOperatorSurfaces(ctx, func(ctx context.Context) scenarioOperatorSurfaces {
				return probe.readOperatorSurfaces(ctx, []string{base})[0]
			}, waitFinalSemanticRPCRetry, func(observation evidenceRelayRetryObservation) error {
				retries = append(retries, observation)
				return writePublicJSON(filepath.Join(collector.output, fmt.Sprintf("operator-%d-read-retries.json", noId)), retries)
			})
			if err != nil {
				return retries, err
			}
			observed := probe.inspectOperatorWithSurfaces(ctx, terminal.Status.Contracts, noId, signers[uint64(noId)].Hex(), base, clients, surfaces)
			if observed.Error != "" {
				return map[string]any{"observation": observed, "read_retries": retries}, errors.New(observed.Error)
			}
			return map[string]any{"observation": observed, "read_retries": retries}, nil
		})
	}
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		id := id
		collector.check(fmt.Sprintf("validator-%d/local-intents", id), "", 5*time.Minute, func(ctx context.Context) (any, error) {
			observed, _ := observeProvisionalValidatorIntent(ctx, cfg, stateDir, id)
			if observed.Error != "" {
				return observed, errors.New(observed.Error)
			}
			if observed.LocalRuntimeIntents != nil && observed.LocalRuntimeIntents.State == "absent" {
				return observed, terminalDiagnosticFinding("authenticated generation has no local steering intent store")
			}
			return observed, nil
		})
		collector.check(fmt.Sprintf("validator-%d/path-proofs", id), available(terminal != nil, "signed operator verify-key history unavailable"), 5*time.Minute, func(ctx context.Context) (any, error) {
			return inspectValidatorPathProofsCached(ctx, cfg, stateDir, id, terminal.Operators, newScenarioPathProofCache())
		})
		collectTerminalDiagnosticValidator(collector, cfg, stateDir, uint64(id), authority, terminalOk && authorityOk)
	}
	collector.check("original-process-log-report", available(attempt != nil && attempt.payload.AcceptanceBoundary != nil, "signed process-log boundary unavailable"), 10*time.Minute, func(ctx context.Context) (any, error) {
		return collectTerminalDiagnosticProcessLogs(ctx, collector, cfg, stateDir, runDir, attempt.payload.AcceptanceBoundary)
	})
	collector.check("acceptance-fault-timing", available(attempt != nil && attempt.payload.AcceptanceBoundary != nil && terminalOk, "terminal signed fault ledger unavailable"), time.Minute, func(context.Context) (any, error) {
		started, err := time.Parse(time.RFC3339Nano, attempt.payload.AcceptanceBoundary.AcceptanceStartedAt)
		if err != nil {
			return nil, err
		}
		return terminalDiagnosticFaultTiming(attempt.payload.AcceptanceBoundary.Faults, started, terminal)
	})
	collector.check("companion-evidence-capture", available(terminalOk, "terminal observation unavailable"), 10*time.Minute, func(ctx context.Context) (any, error) {
		return captureFinalCompanionInputsV2(ctx, cfg, stateDir, collector.output, terminal)
	})
	collector.check("signed-payout-artifacts", available(terminalOk && authorityOk, "terminal observation or path authority unavailable"), 10*time.Minute, func(ctx context.Context) (any, error) {
		payouts, lifecycle, err := collectFinalPayoutArtifacts(ctx, cfg, collector.output, terminal, collector.report.Window, authority.identities)
		return map[string]any{"acceptance": payouts, "lifecycle": lifecycle}, err
	})
	collector.check("compact-validator-final-capture", available(terminalOk && authorityOk && result != nil, "terminal result or path authority unavailable"), 15*time.Minute, func(ctx context.Context) (any, error) {
		started, err := time.Parse(time.RFC3339Nano, result.StartedAt)
		if err != nil {
			return nil, err
		}
		completed, err := time.Parse(time.RFC3339Nano, result.CompletedAt)
		if err != nil {
			return nil, err
		}
		return collectFinalValidatorInputsV2(ctx, cfg, stateDir, collector.output, terminal, result.Name, collector.report.Window, started, completed, authority)
	})
	var matrix *AdversarialMatrix
	matrixOk := collector.check("adversarial-matrix", available(result != nil, "original result unavailable"), time.Minute, func(context.Context) (any, error) {
		artifact, value, err := captureFinalSemanticAdversarialMatrix(cfg, collector.output, result)
		matrix = value
		return artifact, err
	})
	collector.check("adversarial-campaign", available(matrixOk, "authenticated adversarial matrix unavailable"), time.Minute, func(context.Context) (any, error) {
		return captureFinalSemanticAdversaries(collector.output, result, matrix)
	})
	collector.check("closed-foundation-receipts-and-topology", available(result != nil && terminalOk, "terminal result or observation unavailable"), 10*time.Minute, func(ctx context.Context) (any, error) {
		bundles, resultRef, terminalRef, historyRef, err := captureFinalSemanticClosedInputsWithPriorConfigV2(ctx, cfg, stateDir, collector.output, result, terminal, history, cfg.Config.Topology.Miners, cfg.Config.Topology.MinerSwarmProcesses, cfg.Config.Topology.Operators, nil)
		return map[string]any{"bundles": bundles, "result": resultRef, "terminal": terminalRef, "history": historyRef}, err
	})
	collector.check("canonical-contract-receipts-and-native-rewards", available(result != nil && terminalOk, "terminal result or observation unavailable"), 15*time.Minute, func(ctx context.Context) (any, error) {
		return captureFinalSemanticLiveChain(ctx, cfg, stateDir, collector.output, result, terminal, history)
	})
	// Strict semantic reconstruction requires the original closed graph. This
	// reader never manufactures a passing candidate or signed completion for it.
	collector.check("original-strict-semantic-source", available(result != nil && terminalOk, "terminal result or observation unavailable"), 10*time.Minute, func(ctx context.Context) (any, error) {
		archive, err := openFinalSemanticArchive(ctx, cfg, stateDir, runDir)
		if err != nil {
			return nil, err
		}
		archive.artifactDeriver = func(kind, name string, raw []byte) (FinalArtifactLocator, error) {
			return persistFinalCollectedArtifactForConfigV2(cfg, collector.output, kind, name, raw)
		}
		return buildFinalSemanticSourceFromArchive(ctx, cfg, archive, result, terminal, history)
	})
}

// Only typed transport failures retry. Invalid decoded content and mixed
// structural/transport errors never acquire another observation attempt.
func readTerminalDiagnosticOperatorSurfaces(ctx context.Context, read func(context.Context) scenarioOperatorSurfaces, wait func(context.Context, time.Duration) error, observe func(evidenceRelayRetryObservation) error) (scenarioOperatorSurfaces, error) {
	var result scenarioOperatorSurfaces
	if read == nil {
		return result, errors.New("diagnostic operator reader is absent")
	}
	err := runEvidenceRelayStep(ctx, func() error {
		result = read(ctx)
		var failures []error
		for _, value := range result {
			if value.err != nil {
				failures = append(failures, value.err)
			}
		}
		return errors.Join(failures...)
	}, wait, observe)
	return result, err
}

// A late apply after the scheduled restore is reported even while still active;
// terminal duration and restoration checks remain the original strict checks.
func terminalDiagnosticFaultTiming(faults []ScenarioFaultRecord, started time.Time, terminal *ScenarioObservation) (any, error) {
	assertions := appendFaultAssertions(nil, faults, started, terminal)
	var failures []error
	var delays []string
	for _, fault := range faults {
		if fault.AppliedBlock != 0 && fault.RestoreBlock != 0 && fault.AppliedBlock > fault.RestoreBlock {
			delays = append(delays, fmt.Sprintf("%s: applied at %d after scheduled restore %d; actual duration/restoration remains independently checked", fault.ID, fault.AppliedBlock, fault.RestoreBlock))
		}
	}
	for _, assertion := range assertions {
		if !assertion.Passed {
			failures = append(failures, fmt.Errorf("%s: %s", assertion.ID, assertion.Message))
		}
	}
	return map[string]any{"faults": faults, "assertions": assertions, "schedule_delays": delays}, errors.Join(failures...)
}

// A fresh, bounded read closes each validator independently. Captured source
// bytes contain signed records/configuration, never client or hotkey seeds.
func collectTerminalDiagnosticValidator(collector *terminalDiagnosticCollector, cfg *ResolvedConfig, stateDir string, id uint64, authority *finalOperatorPathAuthority, terminalOk bool) {
	var release *validatorpkg.ReleaseConfig
	configOk := collector.check(fmt.Sprintf("validator-%d/exact-production-config", id), "", 5*time.Minute, func(ctx context.Context) (any, error) {
		value, raw, err := finalReleaseCaptureConfigV2(ctx, cfg, stateDir, id)
		if err != nil {
			return nil, err
		}
		release = value
		locator, err := persistFinalCollectedArtifact(collector.output, "diagnostic-validator-config", fmt.Sprintf("validator-%d/config.yml", id), raw)
		return locator, err
	})
	prerequisite := ""
	if !configOk || !terminalOk {
		prerequisite = "terminal observation, exact config or public path authority unavailable"
	}
	var captured *validatorpkg.ReleaseEvidenceV2Capture
	var sources []FinalCollectedValidatorSourceV2
	var checkpoints []FinalNativeCheckpointV2
	lastEpoch := uint64(0)
	if collector.report.Window != nil {
		lastEpoch, _ = checkedAdd(collector.report.Window.FirstEpoch, collector.report.Window.EpochCount-1)
	}
	collector.check(fmt.Sprintf("validator-%d/signed-source-capture", id), prerequisite, 15*time.Minute, func(ctx context.Context) (any, error) {
		bounds, err := campaignValidatorLimitsV2(release.EvidenceV2.Bounds, uint64(len(release.EvidenceV2.Operators)), cfg.Config.ValidatorEvidenceRelay.MaxSlots)
		if err != nil {
			return nil, err
		}
		hotkey, err := decodeHex32("diagnostic validator hotkey", authority.identities.Substrate[validatorHotkeyLabel(int(id))].PublicKey)
		if err != nil {
			return nil, err
		}
		retain := func(ctx context.Context, source validatorpkg.ReleaseEvidenceV2CaptureSource, raw []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			name, err := finalValidatorSourcePathV2(source.Kind, source.Name, source.Origin, bytesSHA256(raw))
			if err != nil {
				return err
			}
			locator, err := persistFinalCollectedArtifactForConfigV2(cfg, collector.output, "diagnostic-validator-source", name, raw)
			if err != nil {
				return err
			}
			sources = append(sources, FinalCollectedValidatorSourceV2{Source: source, Artifact: locator})
			return nil
		}
		chain, err := validatorpkg.DialReleaseChainContext(ctx, []string{cfg.OperationalEVM}, common.HexToAddress(release.Coordinator))
		if err != nil {
			return nil, err
		}
		defer chain.Close()
		native, err := crv4.DialChainContext(ctx, cfg.OperationalSubstrate)
		if err != nil {
			return nil, err
		}
		defer native.API.Client.Close()
		if err := enableProvisionalRuntimeCompatibility(native, cfg); err != nil {
			return nil, err
		}
		captured, err = validatorpkg.CaptureReleaseEvidenceV2(ctx, release, chain, native, validatorpkg.ReleaseEvidenceV2CaptureOptions{Hotkey: hotkey, Origins: [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]}, MaximumBytes: bounds.dataBytes + bounds.controlBytes, MaximumObjects: bounds.maximumObjects, MaximumDataBytes: bounds.dataBytes, MaximumControlBytes: bounds.controlBytes, ThroughEpoch: lastEpoch}, retain)
		if err != nil {
			return sources, err
		}
		// These two readbacks are separate checks even if one fails; they share
		// only the captured, authenticated original source and open read clients.
		collector.check(fmt.Sprintf("validator-%d/native-application-coverage", id), "", 10*time.Minute, func(ctx context.Context) (any, error) {
			var err error
			checkpoints, err = collectFinalNativeCoverageV2(ctx, release, chain, native, hotkey, captured, *collector.report.Window, retain)
			return checkpoints, err
		})
		collector.check(fmt.Sprintf("validator-%d/relay-publication-readback", id), "", 10*time.Minute, func(ctx context.Context) (any, error) {
			return nil, captureFinalValidatorRelayV2(ctx, cfg, stateDir, captured.Publications, retain)
		})
		return map[string]any{"sources": sources, "intents": len(captured.Intents), "closures": len(captured.Closures), "publications": len(captured.Publications)}, nil
	})
	if captured == nil {
		for _, suffix := range []string{"native-application-coverage", "relay-publication-readback"} {
			collector.check(fmt.Sprintf("validator-%d/%s", id, suffix), "authenticated source capture unavailable", time.Minute, nil)
		}
	}
}

// Rescan through cloned in-memory cursors; no live gate or observation changes.
// Every stream is attempted even when another stream fails integrity checks.
func collectTerminalDiagnosticProcessLogs(ctx context.Context, collector *terminalDiagnosticCollector, cfg *ResolvedConfig, stateDir, runDir string, boundary *scenarioCampaignAcceptanceBoundary) (any, error) {
	name := filepath.ToSlash(filepath.Join("runs", filepath.Base(runDir), "process-logs.json"))
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, name, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	if err := collector.retain(filepath.Join(stateDir, filepath.FromSlash(name)), raw); err != nil {
		return nil, err
	}
	var state processLogGateState
	if err := decodeStrictJSONBytes(raw, &state); err != nil {
		return nil, err
	}
	if err := validatePersistedProcessLogGate(state); err != nil {
		return nil, err
	}
	if state.DeploymentID != cfg.Config.Deployment.DeploymentID || state.AcceptanceBoundary == nil || state.AcceptanceBoundary.ContentHash != boundary.ProcessLogBoundaryHash {
		return nil, errors.New("process-log report differs from signed campaign boundary")
	}
	gate := &processLogGate{stateDir: stateDir, state: state}
	var failures []error
	for index := range gate.state.Cursors {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		cursor := &gate.state.Cursors[index]
		if _, err := processLogRelativePath(stateDir, filepath.Join(stateDir, cursor.Path)); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := gate.scanCursorWithLock(cursor, true, activeProcessLogFaultScopes(boundary.Faults), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			failures = append(failures, fmt.Errorf("%s/%s: %w", cursor.ProcessID, cursor.Stream, err))
		}
	}
	observed := gate.projectFindingsWithLock()
	for _, finding := range observed.Findings {
		if finding.Blocking {
			failures = append(failures, fmt.Errorf("%s/%s: %s (%d)", finding.ProcessID, finding.Stream, finding.Class, finding.Count))
		}
	}
	// The new scan has its own diagnostic provenance, never the original report.
	encoded, err := json.MarshalIndent(gate.state, "", "  ")
	if err == nil {
		_, err = persistFinalCollectedArtifact(collector.output, "diagnostic-process-log-scan", "process-log-scan.json", append(encoded, '\n'))
	}
	return observed.Findings, errors.Join(errors.Join(failures...), err)
}
