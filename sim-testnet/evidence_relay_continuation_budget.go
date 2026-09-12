//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

func (e *Executor) evidenceRelayAdmissionEntries() ([]JournalEntry,uint64,error) {
	if e==nil || e.cfg==nil || e.plan==nil || e.journal==nil { return nil,0,errors.New("relay admission has no source owner") }
	entries:=e.journal.Entries()
	c:=e.plan.EvidenceRelayContinuation
	if c==nil { return entries,e.cfg.Config.ValidatorEvidenceRelay.MaxSlots,nil }
	if err:=validateEvidenceRelayContinuationBudget(e.plan);err!=nil { return nil,0,err }
	old:=map[string]bool{}
	for _,debit:=range c.Debits { old[debit.ActionID]=true }
	var result []JournalEntry
	for _,entry:=range entries {
		if !strings.HasPrefix(entry.ActionID,evidenceRelayActionPrefix) { continue }
		if !e.plan.allowedPlanHashes()[entry.PlanHash] || entry.DeploymentID!=e.plan.DeploymentID { return nil,0,errors.New("relay continuation encountered an unowned historical debit") }
		if old[entry.ActionID] { continue }
		entry.PlanHash=e.plan.PlanHash
		result=append(result,entry)
	}
	return result,c.NewSlots,nil
}

func readOwnedEvidenceRelayRequest(ctx context.Context,stateDir string,scope *SetupPlan,entries []JournalEntry,actionID string,owners ...map[string]*SetupPlan) (*SetupPlan,evidenceRelayRequestRecord,[]byte,error) {
	var record evidenceRelayRequestRecord
	if scope==nil || stringsTrimRelayPrefix(actionID)=="" { return nil,record,nil,errors.New("relay retained request has no exact slot owner") }
	raw,err:=validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx,filepath.Join(stateDir,"evidence-relay",stringsTrimRelayPrefix(actionID)+".json"),evidenceRelayActionBytes)
	if err!=nil { return nil,record,nil,err }
	if err:=decodeStrictJSONBytes(raw,&record);err!=nil { return nil,record,nil,err }
	if !scope.allowedPlanHashes()[record.PlanHash] { return nil,record,nil,errors.New("relay retained request names an unrelated source approval") }
	for _,entry:=range entries {
		if entry.ActionID==actionID && (entry.DeploymentID!=scope.DeploymentID || entry.PlanHash!=record.PlanHash) { return nil,record,nil,errors.New("relay slot has competing durable plan/deployment owners") }
	}
	owner:=scope
	if record.PlanHash!=scope.PlanHash {
		owner=nil
		if len(owners)==1 { owner=owners[0][record.PlanHash] }
		if owner==nil {
			owner,err=readValidatorEvidenceHistoricalPlan(stateDir,record.PlanHash)
			if err!=nil { return nil,record,nil,err }
			if len(owners)==1 { owners[0][record.PlanHash]=owner }
		}
	}
	action,err:=validateEvidenceRelayRequest(owner,entries,raw)
	if err!=nil || action.ID!=actionID || owner.DeploymentID!=scope.DeploymentID || owner.ConfigHash!=scope.ConfigHash || owner.ValidatorEvidence==nil || scope.ValidatorEvidence==nil || !reflect.DeepEqual(owner.ValidatorEvidence,scope.ValidatorEvidence) { return nil,record,nil,errors.Join(errors.New("relay retained request changed original source/deployment authority"),err) }
	return owner,record,raw,nil
}

// A retained slot always uses its original action, fee ceiling and plan hash.
// No revision re-signs it or overwrites its immutable request file. New slots
// debit the aggregate remaining allowance across every continuation descendant.
func (e *Executor) admitOwnedEvidenceRelayAction(ctx context.Context,supplied validatorcomponent.ValidatorEvidenceTransactionV2Expected) (Action,string,error) {
	if e==nil || e.plan==nil || e.journal==nil { return Action{},"",errors.New("relay slot owner is absent") }
	if e.plan.EvidenceRelayContinuation==nil {
		action,err:=e.admitEvidenceRelayAction(ctx,supplied)
		return action,e.plan.PlanHash,err
	}
	slot,err:=supplied.Evidence.Header.SlotKey();if err!=nil { return Action{},"",err }
	id:=evidenceRelayActionPrefix+strings.TrimPrefix(fleetLifecycleHex(slot),"0x")
	for _,entry:=range e.journal.Entries() {
		if entry.ActionID!=id { continue }
		owner,record,_,err:=readOwnedEvidenceRelayRequest(ctx,e.stateDir,e.plan,e.journal.Entries(),id)
		if err!=nil { return Action{},"",err }
		if supplied.Activation!=record.Evidence.Activation || supplied.Window!=record.Evidence.Window || !reflect.DeepEqual(supplied.Evidence,record.Evidence.Evidence) || supplied.Journal!=record.Evidence.Journal || supplied.RuntimeHash!=record.Evidence.RuntimeHash { return Action{},"",errors.New("relay continuation retry changes the original signed request") }
		return record.Action,owner.PlanHash,nil
	}
	action,err:=e.admitEvidenceRelayAction(ctx,supplied)
	return action,e.plan.PlanHash,err
}

func validateEvidenceRelayContinuationNamespace(plan *SetupPlan,validatorID uint64,path string) error {
	if plan==nil || plan.EvidenceRelayContinuation==nil { return errors.New("relay continuation namespace has no approval") }
	found:=0
	for _,source:=range plan.EvidenceRelayContinuation.Sources {
		if source.ValidatorID!=validatorID { continue }
		if source.CoordinatorStateDir!=path { return errors.New("relay continuation changed the adopted coordinator namespace") }
		found++
	}
	if found!=2 { return errors.New("relay continuation namespace lost its two original operators") }
	return nil
}

func evidenceRelayContinuationJournalPrefix(entries []JournalEntry,c *EvidenceRelayContinuation) ([]JournalEntry,error) {
	if c==nil { return nil,errors.New("relay continuation journal owner is absent") }
	for index,entry:=range entries { if entry.EntryHash==c.JournalHash { return entries[:index+1],nil } }
	return nil,errors.New("relay continuation original journal checkpoint is absent")
}

func validateEvidenceRelayContinuationSource(stateDir string,current *SetupPlan,entries []JournalEntry) error {
	if current==nil { return errors.New("relay continuation source plan is absent") }
	c:=current.EvidenceRelayContinuation
	if c==nil { return errors.New("relay continuation source is absent") }
	base,err:=readValidatorEvidenceHistoricalPlan(stateDir,c.SourcePlanHash)
	if err!=nil { return err }
	want,err:=appendEvidenceRelayContinuationPlan(base,*c)
	if err!=nil { return err }
	if want.PlanHash!=current.PlanHash && !current.allowedPlanHashes()[want.PlanHash] { return errors.New("relay continuation no longer contains its exact approved append") }
	if _,err:=evidenceRelayContinuationJournalPrefix(entries,c);err!=nil { return err }
	owners:=map[string]*SetupPlan{}
	for _,debit:=range c.Debits {
		owner,record,raw,err:=readOwnedEvidenceRelayRequest(context.Background(),stateDir,current,entries,debit.ActionID,owners)
		if err!=nil || owner.PlanHash!=debit.PlanHash || bytesSHA256(raw)!=debit.RequestSHA256 || record.Action.Spend.EVMGasWei!=debit.AllowanceWei { return errors.Join(errors.New("relay continuation original liability source changed"),err) }
	}
	return nil
}

func evidenceRelayContinuationSameJSON(left,right any) bool {
	a,err:=json.Marshal(left);if err!=nil { return false }
	b,err:=json.Marshal(right)
	return err==nil && string(a)==string(b)
}
