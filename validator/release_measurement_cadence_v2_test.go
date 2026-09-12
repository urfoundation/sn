//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Save only bytes read by genuine full verification of the source fixtures.
// The HTTP replay below still authenticates hashes, signatures, record streams,
// pool folds and exact cumulative checkpoints using production readers.
func collectCadenceReplayObjectsV2(t *testing.T, artifact *ReleaseMeasurementArtifact, options ReleaseMeasurementV2Options, objects map[string][]byte) {
	t.Helper()
	retain := func(kind,hash string,raw []byte) {
		key := kind+":"+hash
		if previous,found:=objects[key];found && !bytes.Equal(previous,raw) { t.Fatal("fixture rewrote an immutable replay object") }
		objects[key] = bytes.Clone(raw)
	}
	wrap := func(replay *AttemptCutV2ReplayOptions) {
		metadata,data:=replay.ReadMetadata,replay.OpenData
		replay.ReadMetadata = func(ctx context.Context,hash string,size uint64)([]byte,error) {
			raw,err:=metadata(ctx,hash,size)
			if err==nil { retain("metadata",hash,raw) }
			return raw,err
		}
		replay.OpenData = func(ctx context.Context,kind,hash string,size uint64)(io.ReadCloser,error) {
			reader,err:=data(ctx,kind,hash,size)
			if err!=nil { return nil,err }
			raw,err:=io.ReadAll(reader)
			err=errors.Join(err,reader.Close())
			if err!=nil { return nil,err }
			retain(kind,hash,raw)
			return io.NopCloser(bytes.NewReader(raw)),nil
		}
	}
	for id,operator:=range options.Operators { wrap(&operator.Measurement.Replay);options.Operators[id]=operator }
	if options.Settlement!=nil {
		for id,operator:=range options.Settlement.Operators { wrap(&operator.Measurement.Replay);options.Settlement.Operators[id]=operator }
	}
	if _,err:=VerifyReleaseMeasurementArtifactV2(t.Context(),artifact,options);err!=nil { t.Fatalf("actual cadence fixture body failed: %v",err) }
}

func TestReleaseMeasurementV2CadenceReplaysTwoSettlementsForOneNativeSuccessor(t *testing.T) {
	t.Parallel()
	previous:=newReleaseMeasurementV2TestFixture(t,15)
	previousBytes,err:=canonicalReleaseMeasurementBytes(previous.artifact)
	if err!=nil { t.Fatal(err) }
	objects:=map[string][]byte{}
	collectCadenceReplayObjectsV2(t,previous.artifact,previous.options(t),objects)
	history:=&releaseEvidenceV2StartupHistory{terminals:map[uint64]*AttemptSettlementClosureV2{},terminalContexts:map[uint64]map[uint64]AttemptCutV2Context{},keys:map[uint64]map[byte]ed25519.PublicKey{}}
	current:=previous
	var last *releaseMeasurementV2SettlementTestFixture
	for step:=0;step<2;step++ {
		var inputs []*attemptCutV2StatsTestFixture
		contexts:=map[uint64]AttemptCutV2Context{}
		for _,input:=range current.artifact.Inputs {
			operator:=current.operators[input.NoID]
			inputs=append(inputs,&attemptCutV2StatsTestFixture{seal:operator.seal,cut:*input.AttemptCutV2,measurement:input.Stats,metadata:operator.metadata,data:operator.data})
			contexts[input.NoID]=operator.seal.expected
		}
		last=newReleaseMeasurementV2SettlementTestFromTerminal(t,current,inputs,step==0,1)
		history.terminals[current.artifact.SettlementEpoch]=last.current.artifact.SettlementClosureV2
		history.terminalContexts[current.artifact.SettlementEpoch]=contexts
		current=last.current
		collectCadenceReplayObjectsV2(t,current.artifact,last.options(t),objects)
	}
	// Only the two real native observations contribute to head EMA. Settlement
	// closure helpers above do not manufacture an intervening native sample.
	last.previous=previous
	last.rebuildHead(t,false)
	current.artifact.PreviousArtifactHash=ReleaseMeasurementContentHash(previousBytes)
	collectCadenceReplayObjectsV2(t,current.artifact,last.options(t),objects)
	if !consecutiveNativeSettlementGapV2(previous.artifact,current.artifact) { t.Fatal("fixture lost its independent 1-native/2-settlement cadence") }
	first:=previous.operators[9].seal
	cfg:=ReleaseConfig{ChainID:945,Policy:previous.artifact.Policy,EvidenceV2:ReleaseEvidenceV2Config{Bounds:ReleaseEvidenceV2Bounds{Cut:first.bounds,Replay:first.replay,MaxParticipants:2,MaxTransitionBytes:256*1024,MaxClosureBytes:1024*1024,MaxProviders:64,MaxEgressHashes:64,MaxFleetPrefixes:64}}}
	positive:=false
	for _,input:=range previous.artifact.Inputs {
		operator:=previous.operators[input.NoID]
		history.participants=append(history.participants,AttemptSettlementRuntimeV2Participant{NoID:input.NoID,Ledger:operator.seal.ledger})
		history.keys[input.NoID]=operator.seal.server.serverPublicKeys()
		cfg.EvidenceV2.Operators=append(cfg.EvidenceV2.Operators,ReleaseEvidenceV2OperatorConfig{NoID:input.NoID,ReplayScratchRoot:newReleaseHeadV2TestStateDir(t),SealScratchRoot:newReleaseHeadV2TestStateDir(t)})
		for _,quality:=range history.terminals[previous.artifact.SettlementEpoch].Transitions[len(history.participants)-1].PostFold { positive=positive || quality.QualityPPM>0 }
	}
	if !positive { t.Fatal("cadence regression requires real positive pool quality") }
	var corrupt atomic.Bool
	var dataReads atomic.Uint64
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
		kind,hash:=r.URL.Query().Get("kind"),r.URL.Query().Get("hash")
		raw,found:=objects[kind+":"+hash]
		if r.URL.Path!="/sn/attempt-artifact" || !found { http.NotFound(w,r);return }
		contentType:="application/json"
		if kind!="metadata" { contentType="application/x-ndjson";dataReads.Add(1) }
		w.Header().Set("Content-Type",contentType)
		if corrupt.Load() && kind==AttemptStreamV2Records { raw=bytes.Clone(raw);raw[0]^=1 }
		_,_=w.Write(raw)
	}))
	defer server.Close()
	reader,err:=NewHTTPAttemptStreamV2Reader(server.URL,first.bounds)
	if err!=nil { t.Fatal(err) }
	history.readers[0],history.readers[1]=reader,reader
	runtime:=&releaseRuntimeV2{cfg:cfg,history:history,gate:make(chan struct{},1)}
	store:=&IntentStore{v2:&releaseIntentV2Owner{runtime:runtime}}
	verify:=func()error { return store.verifyMeasurementLineageV2(t.Context(),previousBytes,previous.options(t),current.artifact,last.options(t)) }
	if err:=verify();err!=nil { t.Fatalf("consecutive native cadence failed complete terminal replay: %v",err) }
	if dataReads.Load()==0 || store.v2.historyAdoption!=nil || store.v2.provisionalEpochGaps || history.retainedStartup { t.Fatal("cadence did not use actual strict record replay") }
	corrupt.Store(true)
	if err:=verify();err==nil { t.Fatal("cadence accepted changed real terminal records") }
	corrupt.Store(false)
	interior:=previous.artifact.SettlementEpoch
	original:=history.terminals[interior]
	delete(history.terminals,interior)
	if err:=verify();err==nil { t.Fatal("cadence omitted an interior settlement") }
	history.terminals[interior]=original
	original.Transitions[0].Signature[0]^=1
	if err:=verify();err==nil { t.Fatal("cadence accepted changed interior signature") }
	original.Transitions[0].Signature[0]^=1
	current.artifact.SubnetEpoch++
	if err:=verify();err==nil { t.Fatal("natural cadence allowed a missed native epoch") }
	current.artifact.SubnetEpoch--
	if _,err:=VerifyReleaseMeasurementLineageV2(t.Context(),previousBytes,previous.options(t),current.artifact,last.options(t));err==nil { t.Fatal("standalone lineage fabricated missing interior terminal authority") }
}
