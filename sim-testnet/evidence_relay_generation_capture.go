//go:build linux || darwin

// Stopped generation capture authenticates both activation consents, immutable
// intent prefixes and signed lifetime counters before exposing a new approval.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Capture consumes authenticated handoff metadata and the already captured old
// namespace. It writes no runtime config and grants no authority by itself.
func captureEvidenceRelayActiveGeneration(ctx context.Context, cfg *ResolvedConfig, executor *Executor, runtime *evidenceRelayRuntime, handoff *policyRolloverHandoffV2, continuation *EvidenceRelayContinuation, hash [32]byte) error {
	prior := executor.plan.EvidenceRelayContinuation
	if handoff == nil || prior == nil || (prior.Schema != evidenceRelayContinuationSourceExpansionSchema && prior.Schema != evidenceRelayContinuationGenerationSchema) || provisionalResumeEnabled(cfg) || len(continuation.Sources) != 4 || handoff.Generation == 0 {
		return errors.New("relay generation requires one strict activated successor of the existing v6 reserve")
	}
	generation := &evidenceRelayActiveGeneration{Generation: handoff.Generation, RolloverPlanHash: handoff.PlanHash, SourcePlanHash: handoff.SourcePlanHash, HandoffSha256: handoff.sourceSHA256, CutoffEpoch: handoff.CutoffEpoch, FirstFullEpoch: handoff.FirstFullEpoch, SourceRoleOverlay: handoff.SourceRoleOverlay, Frozen: continuation.Sources}
	continuation.Sources = append([]EvidenceRelayContinuationSource(nil), prior.Sources...)
	continuation.ActiveGeneration, continuation.Schema = generation, evidenceRelayContinuationGenerationSchema
	for index, owner := range handoff.Validators {
		overlay, err := captureEvidenceRelayGenerationRuntime(ctx, cfg, executor.plan, owner)
		if err != nil {
			return err
		}
		generation.Runtime = append(generation.Runtime, *overlay)
		raw, err := validatorcomponent.CaptureReleaseHistoryAdoptionV2(ctx, overlay.Config.Path, []byte(overlay.Content), executor.plan.PlanHash, continuation.ActivationPlanHash, continuation.NativeEpoch+1)
		if err != nil {
			return err
		}
		var request validatorcomponent.ReleaseHistoryAdoptionV2
		if err := json.Unmarshal(raw, &request); err != nil {
			return err
		}
		if request.CoordinatorStateDir != owner.StateDir {
			return errors.New("relay generation capture selected another coordinator")
		}
		config, err := policyRolloverSourceRoleConfigV2(ctx, owner.Config, owner.Config.Bytes)
		if err != nil {
			return err
		}
		for member, operator := range owner.Evidence.Operators {
			activation := handoff.Members[index*2+member].Activation
			contextBytes, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.Context, owner.Evidence.Bounds.MaxControlBytes)
			if err != nil {
				return err
			}
			var value validatorcomponent.ReleaseEvidenceV2ActivationContext
			if err := decodeStrictJSONBytes(contextBytes, &value); err != nil {
				return err
			}
			canonical, err := value.CanonicalJSON(owner.Evidence.Bounds.MaxControlBytes)
			if err != nil || !bytes.Equal(canonical, contextBytes) || value.Activation != activation {
				return errors.Join(errors.New("relay generation activation context changed"), err)
			}
			vpk, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.VPKSignature, 64)
			if err != nil {
				return err
			}
			hotkey, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.HotkeySignature, 64)
			if err != nil {
				return err
			}
			authority := validatorcomponent.ReleaseActivationV2Authority{Expected: activation, Journal: executor.plan.ValidatorEvidence.Address, RuntimeHash: [32]byte(executor.plan.ValidatorEvidence.RuntimeCodeHash), ValidatorUID: value.ValidatorUID, NativeRuntime: executor.runtimeEvidenceNativeIdentityV2()}
			if _, err := runtime.chain.AuthenticateReleaseActivationV2Context(ctx, executor.substrate.chain, authority, activation, vpk, hotkey, continuation.EVMHead.Number, hash); err != nil {
				return err
			}
			var prefixes []validatorcomponent.AttemptLedgerHead
			if prior.ActiveGeneration != nil {
				previous := prior.ActiveGeneration.Sources[index*2+member]
				if err := checkEvidenceRelayContinuationHistoryPrefix(ctx, overlay.Config.Path, []byte(overlay.Content), request, previous); err != nil {
					return err
				}
				prefixes = append(prefixes, previous.Capacity.Head)
			}
			capacity, err := validatorcomponent.ReadStoppedAttemptLedgerCapacity(ctx, config.Operators[member].StateDir, value.InitialCut.Identity, strings.ToLower(executor.plan.Deployment.CoordinatorProxy.Hex()), ed25519.PublicKey(activation.VPK[:]), owner.Evidence.Bounds.Disk, prefixes...)
			if err != nil {
				return err
			}
			if err := validateEvidenceRelayContinuationCapacity(cfg, owner.Evidence.Bounds, continuation.EndBlock-continuation.EVMHead.Number, capacity); err != nil {
				return err
			}
			generation.Sources = append(generation.Sources, EvidenceRelayContinuationSource{ValidatorID: owner.ValidatorID, NoID: operator.NoID, CoordinatorStateDir: request.CoordinatorStateDir, IntentPrefixSHA256: request.IntentPrefixSHA256, IntentPrefixCount: request.IntentPrefixCount, LastNativeEpoch: request.LastNativeEpoch, LastArtifactHash: request.LastArtifactHash, Activation: activation, Capacity: capacity})
		}
	}
	if prior.ActiveGeneration != nil && !reflect.DeepEqual(prior.ActiveGeneration.Frozen, generation.Frozen) {
		return errors.New("relay capture changed a frozen generation")
	}
	return generation.matchesHandoff(handoff)
}

// Import publishes new immutable runtime inputs only after exact stopped
// recapture matches the reviewed hash, before plan.json selects those inputs.
func installEvidenceRelayGenerationRuntime(ctx context.Context, plan *SetupPlan) error {
	if plan.EvidenceRelayContinuation == nil || plan.EvidenceRelayContinuation.ActiveGeneration == nil {
		return nil
	}
	for _, runtime := range plan.EvidenceRelayContinuation.ActiveGeneration.Runtime {
		if runtime.Config != policyRolloverFile(runtime.Config.Path, []byte(runtime.Content)) {
			return errors.New("relay runtime content differs from its approved reference")
		}
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, runtime.Config.Path, []byte(runtime.Content), runtime.Config.Bytes); err != nil {
			return err
		}
	}
	return ctx.Err()
}
