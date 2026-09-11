//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfoundation/sn/ss58"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// The completed live handoff, manifest and actual argv select the state owner.
// A failed observation never supplies strict authenticated scenario counters.
func inspectProvisionalValidatorIntent(ctx context.Context, cfg *ResolvedConfig, stateDir string, validatorID int) ValidatorObservation {
	result := ValidatorObservation{ValidatorID: validatorID, LocalRuntimeIntents: &validatorpkg.ProvisionalIntentObservationV2{
		Scope: "local-runtime-observation", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "unknown",
	}}
	fail := func(err error) ValidatorObservation {
		result.LocalRuntimeIntents.Error = fmt.Sprintf("%.1024s", err.Error())
		result.Error = "local runtime intents: " + result.LocalRuntimeIntents.Error
		return result
	}
	if !provisionalResumeEnabled(cfg) || cfg.provisionalResume.Record == nil {
		return fail(errors.New("provisional intent observation has no admitted plan"))
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
			DeploymentID: manifest.DeploymentID, ValidatorID: uint64(validatorID), Netuid: cfg.Netuid, Hotkey: hotkey,
		})
		result.LocalRuntimeIntents = observed
		if err != nil {
			return fail(err)
		}
		return result
	}
	return fail(errors.New("local intent validator is missing from the manifest"))
}
