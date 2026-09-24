//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Only initial absence selects original runtime inputs. Corruption, empty
// bytes, incomplete activation or changed files always fail closed. Public
// contract finality is independently rechecked by the relay/validator root.
func readPolicyRolloverHandoffV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan) (*policyRolloverHandoffV2, error) {
	if cfg == nil || cfg.Config == nil || !cfg.Config.ProvisionValidatorEvidenceV2 {
		_, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(policyRolloverRoot(stateDir), "handoff.json"), 1)
		if validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
			return nil, nil
		}
		return nil, errors.Join(errors.New("rollover handoff requires explicit configured V2 provisioning"), err)
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	var h policyRolloverHandoffV2
	raw, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(policyRolloverRoot(stateDir), "handoff.json"), limit, &h)
	if validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p, err := readPolicyRolloverPlanV2(ctx, cfg, base, stateDir, policyRolloverPlanPathV2(stateDir, h.Generation, h.CutoffEpoch))
	if err != nil {
		return nil, err
	}
	if h.Schema != policyRolloverHandoffV2Schema || !h.Activated || h.LedgerContinuityClaimed || h.PlanHash != p.PlanHash || h.SourcePlanHash != p.SourcePlanHash || h.DeploymentID != p.DeploymentID || h.Generation != p.Generation ||
		h.CutoffEpoch != p.Epoch || h.FirstFullEpoch != p.Epoch+1 || h.Native != p.Native || h.EVM != p.EVM || h.Boundary.Number <= p.EVM.Number || !validCanonicalHashHex(h.Boundary.Hash) ||
		!reflect.DeepEqual(h.Members, p.Members) || len(h.Validators) != 2 {
		return nil, errors.New("activated rollover manifest differs from its exact approved generation")
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return nil, err
	}
	checkpoint := false
	for _, entry := range entries {
		checkpoint = checkpoint || entry.EntryHash == p.SourceJournalHash
	}
	if !checkpoint {
		return nil, errors.New("rollover handoff lost its immutable source checkpoint")
	}
	action, err := policyRolloverHandoffActionV2(p)
	if err != nil {
		return nil, err
	}
	_, verified := policyRolloverPriorV2(p, action, entries)
	if verified == nil || verified.PostconditionHash != fmt.Sprintf("0x%x", sha256.Sum256(raw)) {
		return nil, errors.New("rollover handoff has no exact durable activation receipt")
	}
	var recorded policyRolloverHandoffV2
	recordedBytes, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(stateDir, verified.PostconditionPath), limit, &recorded)
	if err != nil || !bytes.Equal(recordedBytes, raw) {
		return nil, errors.Join(errors.New("rollover handoff receipt bytes changed"), err)
	}
	for index, member := range p.Members {
		_, publicationEntry := policyRolloverPriorV2(p, p.Actions[index], entries)
		if publicationEntry == nil {
			return nil, errors.New("rollover handoff precedes all four finalized activations")
		}
		var publication policyRolloverPublicationV2
		encoded, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(stateDir, publicationEntry.PostconditionPath), limit, &publication)
		if err != nil {
			return nil, err
		}
		digest, err := member.Activation.Digest()
		if err != nil {
			return nil, err
		}
		if publicationEntry.PostconditionHash != fmt.Sprintf("0x%x", sha256.Sum256(encoded)) || publication.Schema != "urnetwork-sim-policy-rollover-publication-v2" || publication.PlanHash != p.PlanHash || publication.ValidatorID != member.ValidatorId || publication.NoID != member.NoId || publication.ActivationHash != fmt.Sprintf("0x%x", digest) || publication.PublishedBlock <= p.EVM.Number || publication.PublishedBlock > h.Boundary.Number {
			return nil, errors.New("rollover publication receipt differs from the exact signed member")
		}
	}
	identitiesPath := filepath.Join(stateDir, "evidence-generations", fmt.Sprintf("generation-%020d", h.Generation), "public", "identities.json")
	if h.Identities.Path != identitiesPath {
		return nil, errors.New("rollover public identity namespace differs")
	}
	if _, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, h.Identities, limit); err != nil {
		return nil, err
	}
	approved, err := evidenceRelaySourceCapacityConfig(cfg, base)
	if err != nil {
		return nil, err
	}
	for index, validator := range h.Validators {
		id := uint64(index + 1)
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", id), "evidence-generations", fmt.Sprintf("generation-%020d", h.Generation))
		if validator.ValidatorID != id || validator.PreviousStateDir != p.Validators[index].StateDir || validator.StateDir != filepath.Join(root, "coordinator-state-v2") || validator.ClientStateDir != filepath.Join(root, "state") || validator.Config.Path != filepath.Join(root, "validator.yml") || validator.Identities != h.Identities ||
			!reflect.DeepEqual(validator.Evidence.Bounds, approved.Config.ValidatorEvidenceV2[index].Evidence.Bounds) || len(validator.Evidence.Operators) != 2 {
			return nil, errors.New("rollover validator config, state namespace or approved bounds differ")
		}
		if _, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, p.Validators[index].Config, limit); err != nil {
			return nil, err
		}
		if _, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, validator.Config, limit); err != nil {
			return nil, err
		}
		config, err := validatorcomponent.LoadReleaseConfig(validator.Config.Path)
		if err != nil {
			return nil, err
		}
		if config.ValidatorID != id || config.DeploymentID != p.DeploymentID || config.StateDir != validator.StateDir || config.PolicyHash != p.PolicyHash || !reflect.DeepEqual(config.Policy, *cfg.Policy) || config.PreviousPolicy != nil || !reflect.DeepEqual(config.EvidenceV2, validator.Evidence) {
			return nil, errors.New("rollover rendered validator config changed its approved identity or policy")
		}
		for j, operator := range validator.Evidence.Operators {
			member := h.Members[index*2+j]
			input := filepath.Join(root, "evidence-v2", fmt.Sprintf("no-%d", j+1))
			scratch := filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", j+1))
			if operator.NoID != uint64(j+1) || operator.ReplayScratchRoot != filepath.Join(scratch, "replay") || operator.SealScratchRoot != filepath.Join(scratch, "seal") || config.Operators[j].StateDir != filepath.Join(validator.ClientStateDir, "operators", fmt.Sprintf("no-%d", j+1)) {
				return nil, errors.New("rollover operator namespace differs from its approved fresh source")
			}
			files := operator.Files()
			var contents [][]byte
			for n, name := range []string{"activation.payload", "vpk.signature", "hotkey.signature", "context.json", "history.json"} {
				if files[n].Path != filepath.Join(input, name) {
					return nil, errors.New("rollover evidence reference selects another source")
				}
				data, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, files[n], runtimeEvidenceV2ReferenceLimit(validator.Evidence.Bounds, n))
				if err != nil {
					return nil, err
				}
				contents = append(contents, data)
			}
			payload, err := member.Activation.Payload()
			if err != nil || !bytes.Equal(contents[0], payload) || !bytes.Equal(contents[1], member.VpkSignature) || !bytes.Equal(contents[2], member.HotkeySignature) {
				return nil, errors.Join(errors.New("rollover evidence differs from durable dual consents"), err)
			}
			var value validatorcomponent.ReleaseEvidenceV2ActivationContext
			if err := decodeStrictJSONBytes(contents[3], &value); err != nil {
				return nil, err
			}
			canonical, err := value.CanonicalJSON(validator.Evidence.Bounds.Cut.MaxHeaderBytes)
			if err != nil || !bytes.Equal(canonical, contents[3]) || value.Activation != member.Activation || value.ValidatorUID != member.ValidatorUid || value.Journal != [20]byte(p.Journal) || value.RuntimeHash != [32]byte(p.JournalRuntimeHash) || value.ObservedEVMBlock != h.Boundary.Number || fmt.Sprintf("0x%x", value.ObservedEVMHash) != strings.ToLower(h.Boundary.Hash) || value.InitialCut.Identity.ValidatorID != id || value.InitialCut.FirstSequence != 1 || value.InitialCut.EgressFirstSequence != 1 || value.InitialCut.EgressGeneration != 1 || value.InitialCut.Boundary.SettlementEpoch != p.Epoch {
				return nil, errors.Join(errors.New("rollover activation context differs from the exact fresh boundary"), err)
			}
			history, err := (validatorcomponent.ReleaseEvidenceV2ActivationHistory{Schema: validatorcomponent.ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}).CanonicalJSON(validator.Evidence.Bounds.MaxHistoryBytes)
			if err != nil || !bytes.Equal(contents[4], history) {
				return nil, errors.Join(errors.New("fresh rollover generation claims predecessor ledger history"), err)
			}
		}
	}
	h.sourceSHA256 = bytesSHA256(raw)
	return &h, ctx.Err()
}

// The ordinary launcher still owns stop, API re-render and readiness. This
// final process projection selects the completed generation and removes the
// predecessor-only provisional handoff from fresh validator arguments.
func attachPolicyRolloverProcessConfigsV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, specs []ProcessSpec) error {
	h, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, plan)
	if err != nil || h == nil {
		return err
	}
	for _, validator := range h.Validators {
		found := false
		for index := range specs {
			if specs[index].ID != fmt.Sprintf("validator-%d", validator.ValidatorID) || specs[index].Role != "validator" {
				continue
			}
			if found {
				return errors.New("rollover process manifest duplicates validator identity")
			}
			found = true
			args := []string{}
			for _, arg := range specs[index].Args {
				if strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--provisional-activation-setup") || strings.HasPrefix(arg, "--strict-history-adoption") {
					continue
				}
				args = append(args, arg)
			}
			specs[index].Args = append(args, "--config="+validator.Config.Path)
			specs[index].Env = cloneStrings(specs[index].Env)
			specs[index].Env["URNETWORK_STATE_DIR"] = validator.ClientStateDir
		}
		if !found {
			return errors.New("rollover handoff has no validator process owner")
		}
	}
	return nil
}
