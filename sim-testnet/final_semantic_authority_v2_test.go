//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

// Real simulator provisioning creates both consents and original fixed input
// files. Offline authority reconstruction receives only public bytes; removing
// all live private input files before its call proves the closed boundary.
func TestFinalValidatorAuthorityV2ReconstructsOriginalRendererWithoutLiveKeys(t *testing.T) {
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	deployment := fixture.plan.Deployment
	deployment.DeployBlock, deployment.CoordinatorEventStartBlock = 100, 100
	deployment.DeployBlockHash = common.Hash{8}.Hex()
	deployment.CoordinatorEventStartBlockHash = deployment.DeployBlockHash
	if err := saveContractDeployment(fixture.stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	if err := renderValidatorMinerConfigs(fixture.cfg, fixture.stateDir, fixture.roles, &deployment); err != nil {
		t.Fatal(err)
	}
	authority, err := captureFinalValidatorAuthorityV2(t.Context(), fixture.cfg, fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := os.ReadFile(filepath.Join(fixture.stateDir, "runtime", "validator-1", "validator.yml"))
	if err != nil {
		t.Fatal(err)
	}
	identities, err := os.ReadFile(filepath.Join(fixture.stateDir, "public", "identities.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := fixture.cfg.Policy.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	release, err := yaml.Marshal(fixture.cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	manifest := RuntimeConfigManifest{Schema: runtimeConfigManifestSchema, DeploymentID: fixture.plan.DeploymentID, ConfigHash: fixture.cfg.ConfigHash, PolicyHash: fixture.cfg.PolicyHash, Files: []RuntimeConfigFile{{Path: "runtime/validator-1/validator.yml", SHA256: bytesSHA256(runtime), Mode: "0600"}}}
	manifest.ManifestHash, err = runtimeConfigManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	public := PublicDeploymentManifest{DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ConfigHash: fixture.cfg.ConfigHash, PolicyHash: fixture.cfg.PolicyHash, ChainID: fixture.cfg.ChainID, Netuid: fixture.cfg.Netuid, Contracts: &deployment}
	for index, origin := range fixture.cfg.OperatorAPIOrigins {
		public.Operators = append(public.Operators, PublicOperator{NoID: index + 1, APIURL: origin})
	}
	publicBytes, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	evidence := &FinalSemanticEvidence{DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ConfigHash: fixture.cfg.ConfigHash, PolicyHash: fixture.cfg.PolicyHash, ChainID: fixture.cfg.ChainID, GenesisHash: fixture.cfg.Public.Chain.GenesisHash, Netuid: fixture.cfg.Netuid, ExpectedValidators: fixture.cfg.Config.Topology.Validators, ExpectedOperators: fixture.cfg.Config.Topology.Operators}
	evidence.Deployment.CoordinatorProxy = deployment.CoordinatorProxy.Hex()
	evidence.Deployment.SettlementVault = deployment.SettlementVault.Hex()
	evidence.Deployment.ReserveSink = deployment.ReserveSink.Hex()
	// This isolated test directory contains the fixture's actual seed/setup
	// files. Detached replay must succeed after they cease to be accessible.
	retired := fixture.stateDir + "-retired"
	if err := os.Rename(fixture.stateDir, retired); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Rename(retired, fixture.stateDir); err != nil {
			t.Error(err)
		}
	}()
	verify := func(source, config []byte) error {
		_, _, err := finalValidatorConfigAuthorityV2(t.Context(), evidence, fixture.plan, fixture.plan, source, config, manifestBytes, publicBytes, identities, policy, release, 1)
		return err
	}
	if err := verify(authority, runtime); err != nil {
		t.Fatalf("original public renderer replay: %v", err)
	}
	for _, change := range []string{"runtime-origin", "template-origin", "original-prepared-pin"} {
		t.Run(change, func(t *testing.T) {
			changedAuthority, changedRuntime := bytes.Clone(authority), bytes.Clone(runtime)
			switch change {
			case "runtime-origin":
				changedRuntime = bytes.ReplaceAll(changedRuntime, []byte(fixture.cfg.OperatorAPIOrigins[0]), []byte("https://unapproved-source.example"))
			case "template-origin":
				changedAuthority = bytes.ReplaceAll(changedAuthority, []byte(fixture.cfg.OperatorAPIOrigins[0]), []byte("https://unapproved-source.example"))
			case "original-prepared-pin":
				var value finalValidatorAuthorityV2
				if err := json.Unmarshal(changedAuthority, &value); err != nil {
					t.Fatal(err)
				}
				value.Prepared = append(value.Prepared, '\n')
				changedAuthority, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := verify(changedAuthority, changedRuntime); err == nil {
				t.Fatal(fmt.Sprintf("changed %s redefined source authority", change))
			}
		})
	}
}
