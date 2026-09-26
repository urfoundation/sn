//go:build linux || darwin

// The diagnostic copies the selected original campaign into its external
// archive. A retained failed campaign remains failed evidence.
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

func collectTerminalDiagnosticAdversaries(collector *terminalDiagnosticCollector, cfg *ResolvedConfig, runDir string, result *ScenarioResult) {
	available := func(ok bool, detail string) string {
		if ok {
			return ""
		}
		return detail
	}
	var matrix *AdversarialMatrix
	matrixOk := collector.check("adversarial-matrix", available(result != nil, "original result unavailable"), time.Minute, func(context.Context) (any, error) {
		artifact, value, err := captureFinalSemanticAdversarialMatrix(cfg, collector.output, result)
		matrix = value
		return artifact, err
	})
	collector.check("adversarial-campaign", available(matrixOk, "authenticated adversarial matrix unavailable"), time.Minute, func(context.Context) (any, error) {
		raw, err := readFinalSemanticAdversaries(runDir, result, matrix)
		if err != nil {
			return nil, err
		}
		if err := collector.retain(filepath.Join(runDir, "adversaries.json"), raw); err != nil {
			return nil, err
		}
		locator, err := persistFinalCollectedArtifact(collector.output, "scenario-adversaries", "final-inputs/adversaries.json", raw)
		if err != nil {
			return nil, err
		}
		if _, err := summarizeFinalAdversarialCampaign(result.Adversaries, matrix); err != nil {
			return locator, fmt.Errorf("verify original adversaries.json: %w", err)
		}
		return locator, nil
	})
}
