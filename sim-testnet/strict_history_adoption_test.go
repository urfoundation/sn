//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStrictHistoryAdoptionOptionsKeepWritesAndProvisionalSeparate(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"history-adoption", "--first-native-epoch", "1410"},
		{"resume", "--plan-hash", "approved", "--strict-history-adoption", "/state/history-adoptions/request.json", "--strict-history-adoption-sha256", "sha256:pinned"},
	} {
		if _, _, err := parseCLI(args); err != nil { t.Fatal(err) }
	}
	for _, args := range [][]string{
		{"history-adoption"}, {"history-adoption","--first-native-epoch","1410","--apply"},
		{"history-adoption","--first-native-epoch","1410","--provisional-resume"},
		{"resume","--first-native-epoch","1410"},
		{"resume","--strict-history-adoption","/state/request.json"},
		{"scenario","--plan-hash","approved","--strict-history-adoption","/state/request.json","--strict-history-adoption-sha256","sha256:pinned"},
	} {
		if _, _, err := parseCLI(args); err == nil { t.Fatalf("incomplete or competing adoption admitted: %v",args) }
	}
}

// Genuine original activation signatures, signed transactions and immutable
// carried receipts feed capture and argv binding. The empty coordinator prefix
// remains absent; full chain/disk admission belongs to the validator tests.
func TestStrictHistoryAdoptionCapturesOriginalSourceAndBindsExactLaunch(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceSetupOriginalCarryV2Test(t)
	revised, _ := prepareRuntimeEvidenceSetupCarryV2Test(t,fixture)
	rolesBytes,err := json.MarshalIndent(fixture.roles,"","  ")
	if err != nil { t.Fatal(err) }
	if err := atomicWrite(filepath.Join(fixture.stateDir,"secrets","roles.json"),append(rolesBytes,'\n'),0o600); err != nil { t.Fatal(err) }
	for id:=1;id<=2;id++ {
		if err := ensurePrivateDir(filepath.Join(fixture.stateDir,"runtime",fmt.Sprintf("validator-%d",id),"coordinator-state-v2")); err != nil { t.Fatal(err) }
	}
	before := validatorNamespaceTreeSnapshot(t,fixture.stateDir)
	bundle,err := captureStrictHistoryAdoption(t.Context(),fixture.cfg,fixture.stateDir,20)
	if err != nil { t.Fatal(err) }
	if bundle.ApprovedPlanHash != revised.PlanHash || bundle.SourcePlanHash != fixture.plan.PlanHash || len(bundle.Validators)!=2 || !reflect.DeepEqual(before,validatorNamespaceTreeSnapshot(t,fixture.stateDir)) { t.Fatal("read-only capture changed the original source or namespace") }
	raw,err := json.MarshalIndent(bundle,"","  ")
	if err != nil { t.Fatal(err) }
	raw = append(raw,'\n')
	path := filepath.Join(fixture.stateDir,"history-adoptions","request.json")
	if err := atomicWrite(path,raw,0o600); err != nil { t.Fatal(err) }
	options := cliOptions{PlanHash:revised.PlanHash,StrictHistoryAdoption:path,StrictHistoryAdoptionSHA256:bytesSHA256(raw)}
	if err := prepareStrictHistoryAdoption(t.Context(),fixture.cfg,fixture.stateDir,options,revised); err != nil { t.Fatal(err) }
	before = validatorNamespaceTreeSnapshot(t,fixture.stateDir)
	if err := prepareSignedAttemptStateNamespaces(fixture.cfg,fixture.stateDir); err != nil { t.Fatal(err) }
	if provisionalResumeEnabled(fixture.cfg) || !reflect.DeepEqual(before,validatorNamespaceTreeSnapshot(t,fixture.stateDir)) { t.Fatal("strict adoption enabled provisional mode or migrated the retained namespace") }
	resolved,roles,base,_,_,_,err := strictHistoryAdoptionInputs(t.Context(),fixture.cfg,fixture.stateDir,revised)
	if err != nil { t.Fatal(err) }
	specs := []ProcessSpec{}
	for id:=1;id<=2;id++ {
		configBytes,err := marshalRuntimeValidatorConfig(resolved,fixture.stateDir,roles,base,id)
		if err != nil { t.Fatal(err) }
		configPath := filepath.Join(fixture.stateDir,"runtime",fmt.Sprintf("validator-%d",id),"validator.yml")
		if err := atomicWrite(configPath,configBytes,0o600); err != nil { t.Fatal(err) }
		specs = append(specs,ProcessSpec{ID:fmt.Sprintf("validator-%d",id),Role:"validator",Args:[]string{"__validator","--config="+configPath}})
	}
	if err := attachStrictHistoryAdoption(fixture.cfg,fixture.stateDir,revised,specs); err != nil { t.Fatal(err) }
	for index,spec := range specs {
		if len(spec.Args)!=4 || !strings.HasPrefix(spec.Args[2],"--strict-history-adoption=") || spec.Args[3] != "--strict-history-adoption-sha256="+bytesSHA256(fixture.cfg.strictHistoryAdoption.requests[index]) { t.Fatal("validator argv lost its exact request owner") }
		child,err := os.ReadFile(strings.TrimPrefix(spec.Args[2],"--strict-history-adoption="))
		if err != nil || !bytes.Equal(child,fixture.cfg.strictHistoryAdoption.requests[index]) { t.Fatalf("child handoff bytes changed: %v",err) }
	}
	before = validatorNamespaceTreeSnapshot(t,fixture.stateDir)
	changed := bytes.Clone(raw); changed[len(changed)-1] = ' '
	if err := atomicWrite(path,changed,0o600); err != nil { t.Fatal(err) }
	if err := prepareSignedAttemptStateNamespaces(fixture.cfg,fixture.stateDir); err == nil { t.Fatal("changed request reached namespace preparation") }
	if err := atomicWrite(path,raw,0o600); err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(before,validatorNamespaceTreeSnapshot(t,fixture.stateDir)) { t.Fatal("rejected request changed retained coordinator, inputs or signed history") }
	var unknown map[string]any
	if err := json.Unmarshal(bundle.Validators[0],&unknown); err != nil { t.Fatal(err) }
	unknown["allow_all_gaps"] = true
	bundle.Validators[0],err = json.Marshal(unknown)
	if err != nil { t.Fatal(err) }
	bad,err := json.MarshalIndent(bundle,"","  ")
	if err != nil { t.Fatal(err) }
	bad = append(bad,'\n')
	if err := atomicWrite(path,bad,0o600); err != nil { t.Fatal(err) }
	options.StrictHistoryAdoptionSHA256 = bytesSHA256(bad)
	if err := prepareStrictHistoryAdoption(t.Context(),fixture.cfg,fixture.stateDir,options,revised); err == nil { t.Fatal("unknown member authority survived canonical bundle admission") }
}
