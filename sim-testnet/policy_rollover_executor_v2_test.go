//go:build linux || darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestPolicyRolloverExecutorUsesExactActivationAuthorityWithoutDeploymentReplay(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2TestFixture(t)
	f.cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://verification-rpc.example"
	f.cfg.Public.Chain.EVMPublicReadEndpoint = "https://verification-rpc.example"
	// Unrelated historical deployment evidence cannot be read by this owner.
	if err := os.MkdirAll(filepath.Dir(precompileEvidencePath(f.stateDir)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(precompileEvidencePath(f.stateDir), []byte("unrelated retained deployment history"), 0o600); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournalSnapshot(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	e, err := preparePolicyRolloverExecutorV2(t.Context(), f.cfg, f.cfg, f.stateDir, f.plan, j, f.roles)
	if err != nil {
		t.Fatal(err)
	}
	if e.plan != f.plan || e.journal != j || e.roles != f.roles || e.payloads != nil || e.deployer != nil || e.owner != nil || e.guardian != nil || e.oracle != nil || len(e.deposits) != 0 {
		t.Fatal("rollover owner gained unrelated deployment authority")
	}
	for _, mutation := range []string{"keeper", "policy", "transport", "canceled"} {
		t.Run(mutation, func(t *testing.T) {
			plan := *f.plan
			runtime := f.cfg
			ctx := t.Context()
			switch mutation {
			case "keeper":
				plan.Roles.Keeper = common.Address{1}.Hex()
			case "policy":
				plan.PolicyHash = common.Hash{1}.Hex()
			case "transport":
				copy := *f.cfg
				copy.OperationalEVM = "https://foreign.example"
				runtime = &copy
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := preparePolicyRolloverExecutorV2(ctx, f.cfg, runtime, f.stateDir, &plan, j, f.roles); err == nil {
				t.Fatal("changed rollover authority accepted")
			}
		})
	}
}
