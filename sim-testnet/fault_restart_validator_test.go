//go:build linux

// Real signed rollover inputs and proof bytes exercise restart restoration.
// The replacement PID is this test process; no fixture sends any signal.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

// A recorded original and independently healthy replacement retain the real
// active VPK authority. Only signed post-start trails grant restoration.
type validatorRestartFixture struct {
	driver  *liveScenarioFaultDriver
	fault   scenarioFaultSpec
	state   SupervisorState
	paths   []string
	started uint64
	intent  []byte
}

// Construct a durable restart intent without performing the destructive half.
func newValidatorRestartFixture(t *testing.T) *validatorRestartFixture {
	t.Helper()
	g, handoff, specs := policyRolloverObservationFixtureV2(t)
	f := g.fixture
	var target ProcessSpec
	for _, spec := range specs {
		if spec.ID == "validator-1" {
			target = spec
		}
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", Specs: []ProcessSpec{target}}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Unix(1_700_000_000, 0).UTC()
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: hash, SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks,
		Processes: []ProcessState{{ID: target.ID, Role: target.Role, Identity: target.Identity, PID: os.Getpid(), StartTimeTicks: ticks, StartedAt: started.Format(time.RFC3339), Healthy: true}}}
	for path, value := range map[string]any{"supervisor.json": manifest, "supervisor.state.json": state} {
		if err := writePublicJSON(filepath.Join(f.stateDir, path), value); err != nil {
			t.Fatal(err)
		}
	}
	driver := &liveScenarioFaultDriver{stateDir: f.stateDir, cfg: f.cfg}
	fault := scenarioFaultSpec{ID: "synthetic-validator-restart", Kind: "process-restart", Targets: []string{target.ID}, TriggerOffsetBlocks: 2, DurationBlocks: 3}
	if err := appendActiveFault(driver.activePath(), activeFaultFile{Schema: "urnetwork-sim-active-faults-v1"}, fault,
		[]FaultProcessEvidence{{ID: target.ID, Role: target.Role, Identity: target.Identity, PID: 1234, StartTimeTicks: 11}}); err != nil {
		t.Fatal(err)
	}
	intent, err := os.ReadFile(driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	fixture := &validatorRestartFixture{driver: driver, fault: fault, state: state, started: uint64(started.UnixMilli()), intent: intent}
	for noId := 1; noId <= f.cfg.Config.Topology.Operators; noId++ {
		fixture.paths = append(fixture.paths, filepath.Join(handoff.Validators[0].ClientStateDir, "operators", fmt.Sprintf("no-%d", noId), "proofs.jsonl"))
	}
	return fixture
}

// Create genuine FINAL, EXTEND and validator co-signatures for one domain.
func (self *validatorRestartFixture) proof(t *testing.T, noId int, firstHop uint64, edit func(*validatorpkg.ProofRecord)) []byte {
	t.Helper()
	seed, err := crv4.LoadRawSeedFile(filepath.Join(filepath.Dir(self.paths[noId-1]), "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(seed[:])
	vpk := key.Public().(ed25519.PublicKey)
	serverSeed := derive32(self.driver.cfg, fmt.Sprintf("server/operator-%d/verify-rotation-1", noId))
	server := ed25519.NewKeyFromSeed(serverSeed[:])
	depth := self.driver.cfg.Policy.Verify.TrailDepth
	trail := make([]connect.Id, depth)
	hops := make([]connect.VerifyProofHop, depth)
	for index := range trail {
		trail[index] = connect.Id{0: byte(noId), 15: byte(index + 1)}
		hops[index] = connect.VerifyProofHop{ClientId: trail[index], TimeMs: firstHop + uint64(index)}
	}
	id, nonce := connect.Id{0: 0x71, 15: byte(noId)}, bytes.Repeat([]byte{0x52}, connect.VerifyNonceSize)
	message, err := connect.BuildVerifyFinalMessage(1, id, nonce, vpk, byte(depth), hops)
	if err != nil {
		t.Fatal(err)
	}
	extend, err := connect.BuildVerifyExtendMessage(id, nonce, vpk, byte(depth), trail)
	if err != nil {
		t.Fatal(err)
	}
	digest, pathId := connect.VerifyFinalDigest(message), validatorpkg.TrailPathId(id, vpk, 1)
	record := validatorpkg.ProofRecord{Version: 1, Epoch: 10, TrailId: id, ServerNonce: nonce, Vpk: vpk, M: depth, Hops: hops,
		ServerKeyId: 1, FinalSig: ed25519.Sign(server, message), VerifierSig: ed25519.Sign(key, extend), FinalDigest: digest[:],
		VpkSig: ed25519.Sign(key, message), Coverage: uint64(depth - 1), PathId: pathId[:], CompleteTimeMs: hops[depth-1].TimeMs}
	if edit != nil {
		edit(&record)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(self.paths[noId-1], raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return raw
}

// A healthy process with no proofs cannot clear its intent, even after owner
// reentry. Both operator paths must start a complete signed trail after launch.
func TestValidatorRestartWaitsForEveryFreshSignedProofAfterReentry(t *testing.T) {
	f := newValidatorRestartFixture(t)
	for round := range 3 {
		if round == 1 {
			f.proof(t, 1, f.started+2000, nil)
		}
		if round == 2 {
			f.driver = &liveScenarioFaultDriver{stateDir: f.driver.stateDir, cfg: f.driver.cfg}
			f.proof(t, 2, f.started-1000, nil)
		}
		processes, err := f.driver.Restore(t.Context(), f.fault)
		retained, readErr := os.ReadFile(f.driver.activePath())
		if !processRestartPending(f.fault, err) || len(processes) != 0 || readErr != nil || !bytes.Equal(retained, f.intent) {
			t.Fatalf("round %d restored starved replacement: processes=%+v error=%v read=%v", round, processes, err, readErr)
		}
	}
	f.proof(t, 2, f.started+2000, nil)
	processes, err := f.driver.Restore(t.Context(), f.fault)
	if err != nil || len(processes) != 1 || processes[0].PID != os.Getpid() || processes[0].StartTimeTicks != f.state.Processes[0].StartTimeTicks {
		t.Fatalf("fresh signed replacement did not restore: %+v %v", processes, err)
	}
}

// A full old namespace or a final hop after launch cannot turn an earlier
// trail into current-generation work. Readiness needs the first signed hop.
func TestValidatorRestartRetainsActiveNamespaceAndWholeTrailBoundary(t *testing.T) {
	f := newValidatorRestartFixture(t)
	for noId := range f.paths {
		raw := f.proof(t, noId+1, f.started+2000, nil)
		old := filepath.Join(f.driver.stateDir, "runtime", "validator-1", "state", "operators", fmt.Sprintf("no-%d", noId+1), "proofs.jsonl")
		if err := os.WriteFile(old, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(f.paths[noId]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.driver.Restore(t.Context(), f.fault); !processRestartPending(f.fault, err) {
		t.Fatalf("old namespace authorized active replacement: %v", err)
	}
	f.proof(t, 1, f.started+999, nil)
	f.proof(t, 2, f.started+2000, nil)
	if _, err := f.driver.Restore(t.Context(), f.fault); !processRestartPending(f.fault, err) {
		t.Fatalf("late final hop authorized an old trail: %v", err)
	}
}

// Only the next signal boundary is a test double; actual restoration reads
// the signed source and the durable original intent through the live driver.
type validatorRestartSchedulerDriver struct {
	*liveScenarioFaultDriver
	applied []string
}

// Record the next requested mutation without signaling any process.
func (self *validatorRestartSchedulerDriver) Apply(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	self.applied = append(self.applied, spec.ID)
	return []FaultProcessEvidence{{ID: spec.Targets[0], Role: "validator", Identity: "synthetic-next", PID: 2345}}, nil
}

// The same scheduler that overlapped real replay blackouts now leaves the next
// restart pending until all fresh paths of the first replacement are proven.
func TestValidatorRestartStarvedProofSourceBlocksNextRollingFault(t *testing.T) {
	f := newValidatorRestartFixture(t)
	specs := []scenarioFaultSpec{f.fault, {ID: "synthetic-next", Kind: "process-restart", Targets: []string{"validator-2"}, TriggerOffsetBlocks: 8, DurationBlocks: 3}}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	records[0].Status, records[0].AppliedBlock, records[0].AppliedBlockHash = "active", 102, faultCompletionTestHead(102).Hash
	active, err := readActiveFaultFile(f.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	records[0].Processes = active.Processes
	driver := &validatorRestartSchedulerDriver{liveScenarioFaultDriver: f.driver}
	for range 2 {
		if err := advanceFaults(t.Context(), faultCompletionTestHead(150), specs, records, driver); err != nil {
			t.Fatal(err)
		}
		if records[0].Status != "active" || records[0].RestoredBlock != 0 || records[1].Status != "pending" || len(driver.applied) != 0 {
			t.Fatalf("healthy but starved validator authorized next restart: %+v applied=%v", records, driver.applied)
		}
	}
	for noId := range f.paths {
		f.proof(t, noId+1, f.started+2000, nil)
	}
	if err := advanceFaults(t.Context(), faultCompletionTestHead(151), specs, records, driver); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "restored" || records[0].RestorePendingRounds != 2 || records[0].RestoredBlock != 151 || records[1].Status != "active" || len(driver.applied) != 1 {
		t.Fatalf("fresh production failed to release next fault: %+v applied=%v", records, driver.applied)
	}
}

// Neither old signatures nor a foreign server/key namespace can manufacture
// a post-restart proof. Corruption remains a hard error rather than readiness.
func TestValidatorRestartRejectsChangedProofAuthority(t *testing.T) {
	f := newValidatorRestartFixture(t)
	f.proof(t, 2, f.started+2000, nil)
	for _, c := range []struct {
		name string
		edit func(*validatorpkg.ProofRecord)
	}{
		{name: "server signature", edit: func(p *validatorpkg.ProofRecord) { p.FinalSig[0] ^= 1 }},
		{name: "validator signature", edit: func(p *validatorpkg.ProofRecord) { p.VpkSig[0] ^= 1 }},
		{name: "completion timestamp", edit: func(p *validatorpkg.ProofRecord) { p.CompleteTimeMs++ }},
		{name: "signed first hop", edit: func(p *validatorpkg.ProofRecord) { p.Hops[0].TimeMs++ }},
		{name: "foreign VPK", edit: func(p *validatorpkg.ProofRecord) { p.Vpk[0] ^= 1 }},
		{name: "foreign server key id", edit: func(p *validatorpkg.ProofRecord) { p.ServerKeyId = 9 }},
	} {
		f.proof(t, 1, f.started+2000, c.edit)
		if _, err := f.driver.Restore(t.Context(), f.fault); err == nil || processRestartPending(f.fault, err) {
			t.Fatalf("%s acquired restoration: %v", c.name, err)
		}
	}
	f.proof(t, 1, f.started+2000, nil)
	f.state.Processes[0].StartTimeTicks++
	if err := writePublicJSON(filepath.Join(f.driver.stateDir, "supervisor.state.json"), f.state); err != nil {
		t.Fatal(err)
	}
	if _, err := f.driver.Restore(t.Context(), f.fault); err == nil || processRestartPending(f.fault, err) {
		t.Fatalf("wrong kernel generation acquired restoration: %v", err)
	}
}

// A partial append contributes no proof, while a complete malformed/oversized
// row fails closed. File size is a hard bound independent of tail allocation.
func TestValidatorRestartProofTailHonorsCompleteRowsAndBounds(t *testing.T) {
	f := newValidatorRestartFixture(t)
	raw := f.proof(t, 1, f.started+2000, nil)
	path := f.paths[0]
	limits := scenarioPathProofLimits{maximumBytes: uint64(len(raw) * 20), maximumLine: uint64(len(raw) + 10), maximumProofs: 20}
	for _, prefix := range [][]byte{nil, bytes.Repeat(raw, 5)} {
		data := append(bytes.Clone(prefix), raw...)
		data = append(data, raw[:len(raw)/2]...)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		proof, err := readRestartProofTail(t.Context(), path, limits)
		if err != nil || proof == nil || proof.Hops[0].TimeMs != f.started+2000 {
			t.Fatalf("bounded complete tail lost to partial append: %+v %v", proof, err)
		}
	}
	for _, bad := range [][]byte{[]byte("malformed\n"), append(bytes.Repeat([]byte("x"), int(limits.maximumLine+1)), '\n'), bytes.Repeat([]byte("x"), int(limits.maximumLine+1)), bytes.Repeat(raw, 21)} {
		if err := os.WriteFile(path, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRestartProofTail(t.Context(), path, limits); err == nil {
			t.Fatalf("bad proof tail with %d bytes was accepted", len(bad))
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".target", raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".target", path); err != nil {
		t.Fatal(err)
	}
	if _, err := readRestartProofTail(t.Context(), path, limits); err == nil {
		t.Fatal("a symlink authorized a proof-tail source")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.driver.Restore(ctx, f.fault); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled restore advanced: %v", err)
	}
}
