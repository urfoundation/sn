//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

const strictHistoryAdoptionSchema = "urnetwork-sim-strict-history-adoption-v2"
const strictHistoryAdoptionMaximumBytes = 256 * 1024

// This is a source-bound request for normal strict startup, never an audit or
// campaign acceptance receipt. Both the original setup and current approval
// remain separate, immutable authorities.
type strictHistoryAdoptionBundle struct {
	Schema string `json:"schema"`
	DeploymentID string `json:"deployment_id"`
	ApprovedPlanHash string `json:"approved_plan_hash"`
	SourcePlanHash string `json:"source_plan_hash"`
	PreparedSHA256 string `json:"prepared_sha256"`
	CompletedSHA256 string `json:"completed_sha256"`
	FirstNativeEpoch uint64 `json:"first_native_epoch"`
	Validators []json.RawMessage `json:"validators"`
}

type strictHistoryAdoptionState struct {
	path, hash string
	bundle strictHistoryAdoptionBundle
	requests [][]byte
}

func validateStrictHistoryAdoptionOptions(command string, options cliOptions) error {
	requested := options.StrictHistoryAdoption != "" || options.StrictHistoryAdoptionSHA256 != ""
	if options.FirstNativeEpoch != 0 && command != "history-adoption" { return errors.New("--first-native-epoch requires the read-only history-adoption command") }
	if command == "history-adoption" {
		if requested || options.Apply || options.ProvisionalResume || options.FirstNativeEpoch == 0 { return errors.New("history-adoption requires a fresh explicit native epoch and read-only strict mode") }
	}
	if requested && ((command != "launch" && command != "resume") || options.StrictHistoryAdoption == "" || options.StrictHistoryAdoptionSHA256 == "" || options.PlanHash == "" || options.ProvisionalResume) {
		return errors.New("strict history adoption requires launch/resume with its exact request path, hash and approved plan; provisional mode is incompatible")
	}
	return nil
}

// Authenticate the retained activation source through the ordinary carry
// reader, then build the same bytes the actual renderer will use. No signer,
// setup input, coordinator state or process file is changed here.
func strictHistoryAdoptionInputs(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan) (*ResolvedConfig, *RoleSecrets, map[string]any, string, string, string, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || plan == nil || provisionalResumeEnabled(cfg) || !cfg.Config.ProvisionValidatorEvidenceV2 || cfg.ChainID != testnetChainID || cfg.Config.Topology.Validators != 2 { return nil,nil,nil,"","","",errors.New("strict history adoption requires the existing two-validator testnet V2 setup") }
	if err := strictHistorySupervisorStopped(stateDir); err != nil { return nil,nil,nil,"","","",err }
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		if err := requireValidatorStateStopped(stateDir,id); err != nil { return nil,nil,nil,"","","",err }
	}
	current, err := loadPersistedPlan(cfg,stateDir)
	if err != nil || current.PlanHash != plan.PlanHash { return nil,nil,nil,"","","",errors.Join(errors.New("strict history adoption current approved plan changed"),err) }
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg,stateDir)
	if err != nil { return nil,nil,nil,"","","",err }
	roles, err := loadExistingProvisionalRoles(cfg,stateDir)
	if err != nil { return nil,nil,nil,"","","",err }
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil { return nil,nil,nil,"","","",err }
	var prepared runtimeEvidenceActivationPreparedV2
	preparedBytes, err := readRuntimeEvidenceSetupV2(ctx,filepath.Join(stateDir,"evidence-v2-setup","prepared.json"),limit,&prepared)
	if err != nil { return nil,nil,nil,"","","",err }
	var completed runtimeEvidenceActivationCompletedV2
	completedBytes, err := readRuntimeEvidenceSetupV2(ctx,filepath.Join(stateDir,"evidence-v2-setup","completed.json"),limit,&completed)
	if err != nil { return nil,nil,nil,"","","",err }
	if prepared.PlanHash == plan.PlanHash || !plan.allowedPlanHashes()[prepared.PlanHash] { return nil,nil,nil,"","","",errors.New("strict history adoption requires its approved original activation ancestor") }
	contracts, err := loadContractDeployment(stateDir)
	if err != nil { return nil,nil,nil,"","","",err }
	eventSyncBlock, err := contractDeploymentEventSyncBlock(contracts)
	if err != nil { return nil,nil,nil,"","","",err }
	return resolved,roles,runtimeComponentConfigBase(resolved,contracts,eventSyncBlock),prepared.PlanHash,bytesSHA256(preparedBytes),bytesSHA256(completedBytes),ctx.Err()
}

// The ordinary launch probe may create its lock file. Capture is read-only,
// so inspect only an existing descriptor and preserve actual lock absence.
func strictHistorySupervisorStopped(stateDir string) error {
	lock,err := openFinalCollectedFile(stateDir,"supervisor.lock")
	if err != nil && !errors.Is(err,os.ErrNotExist) { return err }
	if err == nil {
		if err := syscall.Flock(int(lock.Fd()),syscall.LOCK_EX|syscall.LOCK_NB); err != nil { return errors.Join(errors.New("strict history adoption requires a stopped supervisor lock"),err,lock.Close()) }
		if err := errors.Join(syscall.Flock(int(lock.Fd()),syscall.LOCK_UN),lock.Close()); err != nil { return err }
	}
	active,err := liveRecordedSupervisor(stateDir)
	if err != nil { return err }
	if active != nil { return errors.New("strict history adoption requires the recorded supervisor to be stopped") }
	return nil
}

func captureStrictHistoryAdoption(ctx context.Context, cfg *ResolvedConfig, stateDir string, firstNativeEpoch uint64) (*strictHistoryAdoptionBundle,error) {
	plan, err := loadPersistedPlan(cfg,stateDir)
	if err != nil { return nil,err }
	resolved,roles,base,source,prepared,completed,err := strictHistoryAdoptionInputs(ctx,cfg,stateDir,plan)
	if err != nil { return nil,err }
	bundle := &strictHistoryAdoptionBundle{Schema:strictHistoryAdoptionSchema,DeploymentID:plan.DeploymentID,ApprovedPlanHash:plan.PlanHash,SourcePlanHash:source,PreparedSHA256:prepared,CompletedSHA256:completed,FirstNativeEpoch:firstNativeEpoch}
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		configBytes, err := marshalRuntimeValidatorConfig(resolved,stateDir,roles,base,id)
		if err != nil { return nil,err }
		path := filepath.Join(stateDir,"runtime",fmt.Sprintf("validator-%d",id),"validator.yml")
		raw,err := validatorcomponent.CaptureReleaseHistoryAdoptionV2(ctx,path,configBytes,plan.PlanHash,source,firstNativeEpoch)
		if err != nil { return nil,fmt.Errorf("validator %d history capture: %w",id,err) }
		bundle.Validators = append(bundle.Validators,json.RawMessage(raw))
	}
	return bundle,nil
}

func readStrictHistoryAdoptionFile(stateDir,path string, maximum int64) ([]byte,error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path { return nil,errors.New("strict history request path must be clean and absolute") }
	relative,err := filepath.Rel(stateDir,path)
	if err != nil || !strings.HasPrefix(filepath.ToSlash(relative),"history-adoptions/") { return nil,errors.New("strict history request is outside its simulator provenance") }
	return readValidatorEvidenceHistoricalFile(stateDir,filepath.ToSlash(relative),maximum)
}

func prepareStrictHistoryAdoption(ctx context.Context,cfg *ResolvedConfig,stateDir string,options cliOptions,plan *SetupPlan) error {
	if options.StrictHistoryAdoption == "" { return nil }
	if err := requireApproved(true,options.PlanHash,plan.PlanHash); err != nil { return err }
	raw,err := readStrictHistoryAdoptionFile(stateDir,options.StrictHistoryAdoption,strictHistoryAdoptionMaximumBytes)
	if err != nil || bytesSHA256(raw) != options.StrictHistoryAdoptionSHA256 { return errors.Join(errors.New("strict history adoption request changed its approval pin"),err) }
	var bundle strictHistoryAdoptionBundle
	if err := json.Unmarshal(raw,&bundle); err != nil { return err }
	canonical,err := json.MarshalIndent(bundle,"","  ")
	if err != nil || !bytes.Equal(raw,append(canonical,'\n')) { return errors.New("strict history adoption bundle is not canonical") }
	if bundle.Schema != strictHistoryAdoptionSchema || bundle.DeploymentID != plan.DeploymentID || bundle.ApprovedPlanHash != plan.PlanHash || bundle.FirstNativeEpoch == 0 || len(bundle.Validators) != cfg.Config.Topology.Validators { return errors.New("strict history adoption bundle changed its approved owner or census") }
	state := &strictHistoryAdoptionState{path:options.StrictHistoryAdoption,hash:options.StrictHistoryAdoptionSHA256,bundle:bundle}
	for index,value := range bundle.Validators {
		var request validatorcomponent.ReleaseHistoryAdoptionV2
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil { return err }
		encoded,err := json.MarshalIndent(request,"","  ")
		if err != nil { return err }
		encoded = append(encoded,'\n')
		if _,err := validatorcomponent.DecodeReleaseHistoryAdoptionV2(encoded,bytesSHA256(encoded)); err != nil { return err }
		if request.DeploymentID != bundle.DeploymentID || request.ValidatorID != uint64(index+1) || request.ApprovedPlanHash != bundle.ApprovedPlanHash || request.SourcePlanHash != bundle.SourcePlanHash || request.FirstNativeEpoch != bundle.FirstNativeEpoch { return errors.New("strict history adoption member differs from its bundle authority") }
		state.requests = append(state.requests,encoded)
	}
	previous := cfg.strictHistoryAdoption
	cfg.strictHistoryAdoption = state
	if err := preflightStrictHistoryAdoption(ctx,cfg,stateDir); err != nil { cfg.strictHistoryAdoption = previous; return err }
	return nil
}

func preflightStrictHistoryAdoption(ctx context.Context,cfg *ResolvedConfig,stateDir string) error {
	state := cfg.strictHistoryAdoption
	if state == nil { return errors.New("strict history adoption invocation owner is absent") }
	raw,err := readStrictHistoryAdoptionFile(stateDir,state.path,strictHistoryAdoptionMaximumBytes)
	if err != nil || bytesSHA256(raw) != state.hash { return errors.Join(errors.New("strict history adoption bundle changed after admission"),err) }
	plan,err := loadPersistedPlan(cfg,stateDir)
	if err != nil { return err }
	if plan.PlanHash != state.bundle.ApprovedPlanHash { return errors.New("strict history adoption approval was replaced") }
	resolved,roles,base,source,prepared,completed,err := strictHistoryAdoptionInputs(ctx,cfg,stateDir,plan)
	if err != nil { return err }
	if source != state.bundle.SourcePlanHash || prepared != state.bundle.PreparedSHA256 || completed != state.bundle.CompletedSHA256 { return errors.New("strict history adoption changed its retained original activation inputs") }
	for index,request := range state.requests {
		configBytes,err := marshalRuntimeValidatorConfig(resolved,stateDir,roles,base,index+1)
		if err != nil { return err }
		path := filepath.Join(stateDir,"runtime",fmt.Sprintf("validator-%d",index+1),"validator.yml")
		if err := validatorcomponent.CheckReleaseHistoryAdoptionV2Source(ctx,path,configBytes,request,bytesSHA256(request)); err != nil { return err }
	}
	return preflightRuntimeEvidenceV2(resolved,stateDir)
}

func attachStrictHistoryAdoption(cfg *ResolvedConfig,stateDir string,plan *SetupPlan,specs []ProcessSpec) error {
	if cfg.strictHistoryAdoption == nil { return nil }
	if plan.PlanHash != cfg.strictHistoryAdoption.bundle.ApprovedPlanHash { return errors.New("strict history process owner changed approved plan") }
	if err := preflightStrictHistoryAdoption(context.Background(),cfg,stateDir); err != nil { return err }
	for index,raw := range cfg.strictHistoryAdoption.requests {
		id := fmt.Sprintf("validator-%d",index+1)
		configPath := filepath.Join(stateDir,"runtime",id,"validator.yml")
		var request validatorcomponent.ReleaseHistoryAdoptionV2
		if err := json.Unmarshal(raw,&request); err != nil { return err }
		configHash,err := fileSHA256(configPath)
		if err != nil || configHash != request.ConfigSHA256 { return errors.Join(errors.New("strict history actual rendered config differs from approved bytes"),err) }
		path := filepath.Join(stateDir,"history-adoptions",strings.TrimPrefix(cfg.strictHistoryAdoption.hash,"sha256:"),id+".json")
		if previous,err := os.ReadFile(path); err == nil && !bytes.Equal(previous,raw) { return errors.New("strict history immutable child request was replaced") } else if err != nil && !errors.Is(err,os.ErrNotExist) { return err }
		if err := atomicWrite(path,raw,0o600); err != nil { return err }
		found := 0
		for index := range specs {
			if specs[index].ID == id && specs[index].Role == "validator" {
				specs[index].Args = append(specs[index].Args,"--strict-history-adoption="+path,"--strict-history-adoption-sha256="+bytesSHA256(raw))
				found++
			}
		}
		if found != 1 { return errors.New("strict history handoff requires one exact validator process owner") }
	}
	return nil
}
