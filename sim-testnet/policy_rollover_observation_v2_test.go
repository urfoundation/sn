//go:build linux || darwin

// Actual signed generation handoffs select current proof keys and local state.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

func policyRolloverObservationFixtureV2(t *testing.T) (*policyRolloverGenerationTestV2, *policyRolloverHandoffV2, []ProcessSpec) {
	t.Helper()
	g, p, handoff, journal := newPolicyRolloverHandoffTestV2(t, func(g *policyRolloverGenerationTestV2) {
		f := g.fixture
		plan, err := buildPlan(f.cfg, &f.plan.LiveFacts, f.plan.Roles, time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		f.plan = plan
		if err := writePublicJSON(filepath.Join(f.stateDir, "plan.json"), f.plan); err != nil {
			t.Fatal(err)
		}
		f.cfg.provisionalResume = &provisionalResumeState{AcceptedPlanHashes: []string{f.plan.PlanHash}, Record: &provisionalResumeRecord{Provisional: true, PlanHash: f.plan.PlanHash}}
	})
	activatePolicyRolloverHandoffTestV2(t, g, p, handoff, journal)
	f := g.fixture
	specs := buildClientSpecs(f.cfg, f.stateDir, map[string]string{"sim-testnet": "synthetic-binary"}, f.roles)
	if err := attachPolicyRolloverProcessConfigsV2(t.Context(), f.cfg, f.stateDir, f.plan, specs); err != nil {
		t.Fatal(err)
	}
	for _, selected := range handoff.Validators {
		if err := os.MkdirAll(selected.StateDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return g, handoff, specs
}

func policyRolloverSignedProofV2(t *testing.T, path string, server ed25519.PrivateKey, noID, depth int) []byte {
	t.Helper()
	seed, err := crv4.LoadRawSeedFile(filepath.Join(filepath.Dir(path), "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(seed[:])
	vpk := key.Public().(ed25519.PublicKey)
	trail := make([]connect.Id, depth)
	hops := make([]connect.VerifyProofHop, depth)
	for index := range trail {
		trail[index] = connect.Id{0: byte(noID), 15: byte(index + 1)}
		hops[index] = connect.VerifyProofHop{ClientId: trail[index], TimeMs: uint64(1000 + index)}
	}
	id := connect.Id{0: 0x67, 15: byte(noID)}
	nonce := bytes.Repeat([]byte{0x56}, connect.VerifyNonceSize)
	message, err := connect.BuildVerifyFinalMessage(1, id, nonce, vpk, byte(depth), hops)
	if err != nil {
		t.Fatal(err)
	}
	extend, err := connect.BuildVerifyExtendMessage(id, nonce, vpk, byte(depth), trail)
	if err != nil {
		t.Fatal(err)
	}
	digest, pathID := connect.VerifyFinalDigest(message), validatorpkg.TrailPathId(id, vpk, 1)
	record := validatorpkg.ProofRecord{Version: 1, Epoch: 10, TrailId: id, ServerNonce: nonce, Vpk: vpk, M: depth, Hops: hops,
		ServerKeyId: 1, FinalSig: ed25519.Sign(server, message), VerifierSig: ed25519.Sign(key, extend), FinalDigest: digest[:],
		VpkSig: ed25519.Sign(key, message), Coverage: uint64(depth - 1), PathId: pathID[:], CompleteTimeMs: hops[depth-1].TimeMs}
	wire, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	wire = append(wire, '\n')
	if err := os.WriteFile(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestPolicyRolloverObservationSelectsFreshProofNamespaceAndBounds(t *testing.T) {
	g, handoff, _ := policyRolloverObservationFixtureV2(t)
	f := g.fixture
	server := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize))
	operators := []OperatorObservation{}
	for noID := 1; noID <= 2; noID++ {
		operators = append(operators, OperatorObservation{NoID: noID, VerifyKeys: []VerifyKeyObservation{{ServerKeyID: 1, PublicKey: server.Public().(ed25519.PublicKey)}}})
	}
	for _, selected := range handoff.Validators {
		for noID := 1; noID <= 2; noID++ {
			old := filepath.Join(f.stateDir, "runtime", fmt.Sprintf("validator-%d", selected.ValidatorID), "state", "operators", fmt.Sprintf("no-%d", noID), "proofs.jsonl")
			if err := os.WriteFile(old, []byte("old source must never be replayed as fresh proof\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(selected.ClientStateDir, "operators", fmt.Sprintf("no-%d", noID), "proofs.jsonl")
			policyRolloverSignedProofV2(t, path, server, noID, f.cfg.Policy.Verify.TrailDepth)
		}
		cache := newScenarioPathProofCache()
		counts, err := inspectValidatorPathProofsCached(t.Context(), f.cfg, f.stateDir, int(selected.ValidatorID), operators, cache)
		if err != nil || !reflect.DeepEqual(counts, map[int]int{1: 1, 2: 1}) {
			t.Fatalf("fresh signed proofs: %v %v", counts, err)
		}
		for path := range cache.prefixes {
			if !strings.HasPrefix(path, selected.ClientStateDir+string(filepath.Separator)) {
				t.Fatal("proof cache mixed source generations")
			}
		}
		limits, err := scenarioPathProofLimitsV2(selected.Evidence.Bounds)
		if err != nil || limits.maximumProofs != selected.Evidence.Bounds.Disk.MaxTrailCount {
			t.Fatal("fresh source widened its approved proof census", err)
		}
	}
	counts, err := releaseTopologyProofCounts(f.cfg, f.stateDir)
	if err != nil || len(counts) != 4 {
		t.Fatalf("startup freshness selected old source: %v %v", counts, err)
	}
	for _, count := range counts {
		if count != 1 {
			t.Fatal("startup freshness credited old source")
		}
	}
	authority, err := loadFinalOperatorPathAuthorityV2(t.Context(), f.cfg, f.stateDir, finalConfiguredValidatorIDs(f.cfg))
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range handoff.Validators {
		release, raw, adoption, err := finalReleaseCaptureConfigWithAdoptionV2(t.Context(), f.cfg, f.stateDir, selected.ValidatorID)
		if err != nil || release.StateDir != selected.StateDir || !reflect.DeepEqual(release.EvidenceV2, selected.Evidence) || len(adoption) != 0 || bytesSHA256(raw) != "sha256:"+strings.TrimPrefix(selected.Config.SHA256, "0x") {
			t.Fatalf("terminal capture selected predecessor: %+v %v", release, err)
		}
		for noID := uint64(1); noID <= 2; noID++ {
			member := handoff.Members[(selected.ValidatorID-1)*2+noID-1]
			if !bytes.Equal(authority.keysByValidator[selected.ValidatorID][noID], member.Activation.VPK[:]) {
				t.Fatal("terminal collector retained predecessor public identity")
			}
		}
	}
	path := handoff.Validators[0].Evidence.Operators[0].Context.Path
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectValidatorPathProofsCached(t.Context(), f.cfg, f.stateDir, 1, operators, newScenarioPathProofCache()); err == nil {
		t.Fatal("corrupt active source fell back to old proof namespace")
	}
}

func TestPolicyRolloverObservationReadsFreshIntentOwnerWithoutLegacyHandoff(t *testing.T) {
	g, handoff, specs := policyRolloverObservationFixtureV2(t)
	f := g.fixture
	var spec ProcessSpec
	for _, value := range specs {
		if value.ID == "validator-1" {
			spec = value
		}
	}
	hotkey := handoff.Members[0].Activation.Hotkey
	observed, err := observeProvisionalValidatorSourceV2(t.Context(), f.cfg, f.stateDir, 1, spec, hotkey)
	if err != nil || observed.State != "absent" || observed.FinalAcceptance || observed.StateDirectory != handoff.Validators[0].StateDir || !validSHA256ContentHash(observed.HandoffSHA256) {
		t.Fatalf("fresh local intent source: %+v %v", observed, err)
	}
	for _, mutate := range []func(*ProcessSpec){
		func(s *ProcessSpec) {
			s.Args = []string{"__validator", "--config=" + filepath.Join(f.stateDir, "runtime", "validator-1", "validator.yml")}
		},
		func(s *ProcessSpec) { s.Args = append(s.Args, "--provisional-activation-setup=old") },
		func(s *ProcessSpec) {
			s.Env["URNETWORK_STATE_DIR"] = filepath.Join(f.stateDir, "runtime", "validator-1", "state")
		},
		func(s *ProcessSpec) { s.ID = "validator-2" },
	} {
		changed := spec
		changed.Args, changed.Env = append([]string(nil), spec.Args...), cloneStrings(spec.Env)
		mutate(&changed)
		if _, err := observeProvisionalValidatorSourceV2(t.Context(), f.cfg, f.stateDir, 1, changed, hotkey); err == nil {
			t.Fatal("mismatched live generation gained observation authority")
		}
	}
	hotkey[0] ^= 1
	if _, err := observeProvisionalValidatorSourceV2(t.Context(), f.cfg, f.stateDir, 1, spec, hotkey); err == nil {
		t.Fatal("foreign hotkey selected fresh generation")
	}
}
