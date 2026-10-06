// Bootstrap tests bind synthetic original registration custody to one intended
// passive deployment without creating service or contract approvals.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Original success contributes the exact netuid0 generation while the emitted
// passive role remains incomplete until its own independent service approval.
func TestRootRegisterBootstrapProjectsFinalizedSeatWithoutApproval(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	evidence, signed := rootRegisterTestReconciliation(t, f, true)
	custody, store, request := rootRegisterSignedTestCustody(t, f)
	if err := evidence.validate(request, signed); err != nil {
		t.Fatal(err)
	}
	chain := rootRegisterReconcileBoundary(func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error) {
		return evidence, nil
	})
	result, err := custody.reconcile(t.Context(), chain)
	if err != nil || result.Phase != "finalized" || result.Seat == nil {
		t.Fatal("original synthetic registration did not finalize", result, err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	input := newRootRegisterBootstrapTestInput(t, f)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.storage.Context, input.args, &output, &diagnostic); code != 0 {
		t.Fatal("finalized registration handoff unavailable", code, diagnostic.String())
	}
	var handoff rootRegisterBootstrapHandoff
	if err := decodePlanJson(output.Bytes(), &handoff); err != nil {
		t.Fatal(err)
	}
	role := handoff.RootValidator
	if handoff.Schema != rootRegisterBootstrapHandoffSchema || handoff.Status != "passive-service-configuration-and-approval-required" ||
		handoff.DeploymentId != input.input.DeploymentId || handoff.RootInput != input.input.RootInput || handoff.RegistrationConfig != input.input.RegistrationConfig ||
		handoff.RegistrationConfigHash != rootObjectHash(f.config) || handoff.ApprovalPublicKey != f.key || handoff.ActionHash != f.config.Action.RequestHash || handoff.RequestHash != request.ContentHash ||
		handoff.ExtrinsicHash != rootExtrinsicHash(signed) || rootObjectHash(handoff.Receipt) != rootObjectHash(*evidence.Receipt) ||
		role.Netuid == nil || *role.Netuid != 0 || role.Seat == nil || *role.Seat != *result.Seat || role.Hotkey != f.config.Action.Policy.Hotkey || role.Coldkey != f.config.Action.Policy.Operator ||
		role.Strategy != rootPassiveStrategy || role.ActionApprovalPublicKey != "" || role.ApprovalPublicKey != "" || role.Approval != (planFileReference{}) ||
		handoff.NativeSigning || handoff.NetworkEffects || handoff.ActivationReady || len(handoff.RequiredSteps) == 0 {
		t.Fatal("handoff changed the original seat or acquired independent bootstrap approval", handoff)
	}
	if err := role.validate(); err == nil {
		t.Fatal("proposed role became an approved passive service")
	}
	seal := handoff.ContentHash
	handoff.ContentHash = ""
	if seal != rootObjectHash(handoff) {
		t.Fatal("handoff does not seal its original receipt and proposed role")
	}
}

// The future passive-service input is a pinned planning artifact; this helper
// deliberately supplies no approver, approval signature or active service.
type rootRegisterBootstrapTestInput struct {
	input rootRegisterBootstrapInput
	root  bootstrapRootConfig
	path  string
	hash  string
	args  []string
}

// Private synthetic inputs are independent of the retained registration state.
func newRootRegisterBootstrapTestInput(t *testing.T, f *rootRegisterTestFixture) rootRegisterBootstrapTestInput {
	t.Helper()
	directory := filepath.Dir(f.config.Action.StatePath)
	configRaw, _ := json.Marshal(f.config)
	configPath, configHash := ownerRecycleTestFile(t, directory, "registration-config.json", configRaw)
	servicePath, serviceHash := ownerRecycleTestFile(t, directory, "future-passive-service.json", []byte("{}\n"))
	policy := f.config.Action.Policy
	network := planNetwork{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}
	root := bootstrapRootConfig{Schema: bootstrapRootPassiveConfigSchema, DeploymentId: "synthetic-root-bootstrap", Network: network, RunDirectory: directory,
		RootService: planFileReference{Path: servicePath, Sha256: serviceHash}}
	rootRaw, _ := json.Marshal(root)
	rootPath, rootHash := ownerRecycleTestFile(t, directory, "intended-root-input.json", rootRaw)
	input := rootRegisterBootstrapInput{Schema: rootRegisterBootstrapInputSchema, DeploymentId: root.DeploymentId, Network: network,
		RootInput: planFileReference{Path: rootPath, Sha256: rootHash}, RegistrationConfig: planFileReference{Path: configPath, Sha256: configHash}, Operator: policy.Operator, Hotkey: policy.Hotkey}
	inputRaw, _ := json.Marshal(input)
	path, digest := ownerRecycleTestFile(t, directory, "bootstrap-registration-input.json", inputRaw)
	args := []string{"bootstrap-chain", "root-registration", "bootstrap-handoff", "--input", path, "--input-sha256", digest,
		"--deployment-id", root.DeploymentId, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash}
	return rootRegisterBootstrapTestInput{input: input, root: root, path: path, hash: digest, args: args}
}

// Neither a reservation nor a pinned target role can stand in for the original
// finalized event and inclusion readback. Refusal leaves custody unchanged.
func TestRootRegisterBootstrapRequiresFinalizedOriginalSeat(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	input := newRootRegisterBootstrapTestInput(t, f)
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := runMain(f.storage.Context, input.args, &out, &diagnostic); code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "finalized registration custody") {
		t.Fatal("pending custody produced a bootstrap seat", code, diagnostic.String())
	}
	current, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("handoff refusal changed original registration custody", err)
	}
}

// A different deployment, file generation or operator is rejected before
// retained registration evidence can be projected into the proposed role.
func TestRootRegisterBootstrapPinsRemainIndependent(t *testing.T) {
	for _, changed := range []string{"deployment", "file", "operator", "hotkey", "action"} {
		f := newRootRegisterTestFixture(t, false)
		input := newRootRegisterBootstrapTestInput(t, f)
		switch changed {
		case "deployment":
			for i := range input.args {
				if input.args[i] == "--deployment-id" {
					input.args[i+1] = "synthetic-other-deployment"
				}
			}
		case "file":
			raw, err := os.ReadFile(input.path)
			if err != nil || os.WriteFile(input.path, append(raw, '\n'), 0600) != nil {
				t.Fatal("cannot change synthetic input", err)
			}
		case "operator", "hotkey":
			if changed == "operator" {
				input.input.Operator = f.config.Action.Policy.Reserve
			} else {
				input.input.Hotkey = f.config.Action.Policy.SubnetOwner
			}
			raw, _ := json.Marshal(input.input)
			_, input.hash = ownerRecycleTestFile(t, filepath.Dir(input.path), filepath.Base(input.path), raw)
			for i := range input.args {
				if input.args[i] == "--input-sha256" {
					input.args[i+1] = input.hash
				}
			}
		case "action":
			for i := range input.args {
				if input.args[i] == "--accept-action-hash" {
					input.args[i+1] = rootObjectHash("synthetic different registration action")
				}
			}
		}
		var out, diagnostic bytes.Buffer
		if code := runMain(f.storage.Context, input.args, &out, &diagnostic); code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "differs") {
			t.Fatal("changed bootstrap pin accepted", changed, code, diagnostic.String())
		}
		if _, err := os.Stat(f.config.Action.StatePath); !os.IsNotExist(err) {
			t.Fatal("invalid bootstrap input claimed registration custody", changed, err)
		}
	}
}
