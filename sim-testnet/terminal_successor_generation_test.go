//go:build linux || darwin

// Terminal succession must retain both frozen and active source generations.
// These review gates exercise current capture and refusal, not a new approval.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Real activated generation configs feed the ordinary capture reader. Inert
// applied-shaped intents prove only canonical prefix and namespace custody;
// the independent validator tests own native and signed-ledger authentication.
func TestTerminalSuccessorCaptureKeepsFrozenAndActivePrefixesSeparate(t *testing.T) {
	g, plan, handoff, journal := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, plan, handoff, journal)
	selected, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range selected.Validators {
		originalConfig := filepath.Join(f.stateDir, "runtime", fmt.Sprintf("validator-%d", owner.ValidatorID), "validator.yml")
		configPaths := []string{originalConfig, owner.Config.Path}
		statePaths := []string{owner.PreviousStateDir, owner.StateDir}
		var requests [][]byte
		var configWires [][]byte
		frozenFiles := map[string][]byte{}
		for index, configPath := range configPaths {
			if err := ensurePrivateDir(statePaths[index]); err != nil {
				t.Fatal(err)
			}
			intent := &validatorcomponent.SteeringIntent{SubnetEpoch: uint64(40 + index*3), SettlementEpoch: uint64(12 + index), Status: "applied",
				VectorHash: bytesSHA256([]byte{byte(index)}), MeasurementArtifactHash: bytesSHA256([]byte{byte(index + 5)}),
				Prepared: &crv4.PreparedSubmission{SourceCommitment: &crv4.PreparedSourceCommitment{Hash: "inert-prefix-binding-fixture"}}}
			prefix := struct {
				Schema  string                              `json:"schema"`
				Current *validatorcomponent.SteeringIntent  `json:"current,omitempty"`
				History []validatorcomponent.SteeringIntent `json:"history"`
			}{Schema: validatorcomponent.SteeringIntentSchema, Current: intent}
			raw, err := json.MarshalIndent(prefix, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, '\n')
			intentPath := filepath.Join(statePaths[index], "steering-intents.json")
			if err := atomicWrite(intentPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			frozenFiles[intentPath] = raw
			configBytes, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			configWires = append(configWires, configBytes)
			captured, err := validatorcomponent.CaptureReleaseHistoryAdoptionV2(t.Context(), configPath, configBytes, plan.PlanHash, f.plan.PlanHash, 50)
			if err != nil {
				t.Fatal(err)
			}
			var request validatorcomponent.ReleaseHistoryAdoptionV2
			if err := json.Unmarshal(captured, &request); err != nil {
				t.Fatal(err)
			}
			if request.CoordinatorStateDir != statePaths[index] || request.IntentPrefixCount != 1 || request.IntentPrefixSHA256 != bytesSHA256(raw) || request.LastNativeEpoch != intent.SubnetEpoch || request.LastArtifactHash != intent.MeasurementArtifactHash {
				t.Fatal("capture lost the exact selected generation prefix")
			}
			if err := validatorcomponent.CheckReleaseHistoryAdoptionV2Source(t.Context(), configPath, configBytes, captured, bytesSHA256(captured)); err != nil {
				t.Fatal(err)
			}
			requests = append(requests, captured)
		}
		if bytes.Equal(requests[0], requests[1]) || statePaths[0] == statePaths[1] {
			t.Fatal("capture flattened the two independent source owners")
		}
		for index := range configPaths {
			other := 1 - index
			if err := validatorcomponent.CheckReleaseHistoryAdoptionV2Source(t.Context(), configPaths[index], configWires[index], requests[other], bytesSHA256(requests[other])); err == nil {
				t.Fatal("another generation's valid prefix authorized this source")
			}
		}
		for path, before := range frozenFiles {
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("capture rewrote frozen or active history", err)
			}
		}
	}
	for path, before := range g.original {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("capture changed an original source input", err)
		}
	}
}

// A separately dual-signed activation and signed header are valid evidence,
// but the existing v6 approval cannot describe both generations in one source.
func TestTerminalSuccessorV6RefusesFlattenedActiveGeneration(t *testing.T) {
	f, executor := newEvidenceRelayExpansionTest(t)
	continuation := evidenceRelaySourceExpansionRequestTest(t, f, executor)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, continuation)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationPlan(plan); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(plan.EvidenceRelayContinuation.Sources)
	if err != nil {
		t.Fatal(err)
	}
	member := f.prepared.Members[0]
	request := evidenceRelayLaunchRequestTest(t, f, member, member.Activation.Domain.Epoch+1, false, 0)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0xd7}, ed25519.SeedSize))
	request.Activation.VPK = [32]byte(key[ed25519.SeedSize:])
	request.Activation.Domain.Epoch++
	request.Activation.Domain.PolicyHash = [32]byte{0xe7}
	hotkey, _, err := runtimeEvidenceActivationKeysV2(f.roles, member.ValidatorId, member.NoId)
	if err != nil {
		t.Fatal(err)
	}
	activationSignature, err := request.Activation.SignVPK(key)
	if err != nil {
		t.Fatal(err)
	}
	activationHash, err := request.Activation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkey.Sign(activationHash[:])
	if err != nil || request.Activation.Verify(request.Activation, activationSignature, hotkeySignature) != nil {
		t.Fatal("fixture lacks exact dual activation consent", err)
	}
	request.Evidence.Header.VPK = request.Activation.VPK
	request.Evidence.Header.Domain, err = request.Activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	request.Evidence.VPKSignature, err = request.Evidence.Header.SignVPK(key)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := request.Evidence.Header.Digest()
	if err != nil {
		t.Fatal(err)
	}
	request.Evidence.HotkeySignature, err = hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	reserve, err := exactPlanActionByID(plan, evidenceRelayReserveId)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildEvidenceRelayAction(plan, reserve, request); err != nil {
		t.Fatal("signed request failed before the continuation boundary", err)
	}
	plan.EvidenceRelayContinuation.Retained, err = canonicalEvidenceRelayContinuationRequests(append(plan.EvidenceRelayContinuation.Retained, request))
	if err != nil {
		t.Fatal(err)
	}
	err = validateEvidenceRelayContinuationPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "retained request replaced original activation") {
		t.Fatal("v6 failed to retain its original activation boundary", err)
	}
	after, err := json.Marshal(plan.EvidenceRelayContinuation.Sources)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refusal rewrote the original source census", err)
	}
}
