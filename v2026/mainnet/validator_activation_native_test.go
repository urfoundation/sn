// Independent synthetic approvals and the real owned-RPC reader exercise only
// native prerequisites. No local verdict supplies the missing production fence.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Request interception preserves the real parser, RPC retries and byte limits.
type validatorActivationNativeTestCall struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func validatorActivationNativeTestIntercept(t *testing.T, client *rpcClient, intercept func(validatorActivationNativeTestCall, *http.Response) (*http.Response, error)) {
	t.Helper()
	original := client.httpClient.Transport
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		var call validatorActivationNativeTestCall
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		response, err := original.RoundTrip(request)
		if err != nil {
			return response, err
		}
		return intercept(call, response)
	})
}

func validatorActivationNativeTestReply(response *http.Response, result any) (*http.Response, error) {
	response.Body.Close()
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, err
}

func validatorActivationNativeTestReadiness(t *testing.T, f *validatorActivationFixture) bootstrapChainReadiness {
	t.Helper()
	readiness, err := f.chain.client.observeBootstrapChainReadiness(t.Context(), f.chain.preparation)
	if err != nil || !readiness.ObservationComplete {
		t.Fatal("synthetic original readiness unavailable", err)
	}
	return readiness
}

// Native admission removes exactly its two old blockers. The concrete public
// command still has no current authority and spends no fresh-start allowance.
func TestValidatorActivationNativeAdmissionKeepsRemainingAuthorityClosed(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	original := f.chain.journals(t)
	result, code, detail := f.command(t.Context(), "admit", nil)
	if code != 0 || result.Status != "admitted-process-only" || result.Readiness == nil || result.Readiness.Native == nil {
		t.Fatal("native prerequisites did not reach real admission", code, result, detail)
	}
	native := result.Readiness.Native
	if native.NativeEpoch != 20 || native.ActivationEpoch != 20 || native.ActivationBlock != 100 || native.ActivationHash != testFinalizedHash || native.ActivationPendingServerAlpha != 0 ||
		native.RecycleMode != "Recycle" || native.MechanismCount != 1 || !planSha256(native.EvidenceHash) || native.RuntimeMetadataHash != f.chain.validators[0].config.RuntimeMetadataHash {
		t.Fatal("native projection lost its exact independently approved facts", native)
	}
	for i, role := range result.Readiness.Roles {
		if role.ApprovedCheckpointHash != testFinalizedHash || role.ObservedCheckpointHash != testFinalizedHash || len(role.ActivationBlockers) != 3+(1-i) ||
			slices.Contains(role.ActivationBlockers, "NATIVE_EPOCH_APPROVAL_WINDOW_UNVERIFIED") || slices.Contains(role.ActivationBlockers, "SIGNED_ACTIVATION_CHECKPOINT_UNVERIFIED") ||
			native.Roles[i].Registration != *role.Observed || native.Roles[i].LastUpdate != 0 {
			t.Fatal("native admission discharged unrelated authority or lost new-registration freshness", role, native.Roles[i])
		}
	}
	operations := result.Operations
	result, code, detail = f.command(t.Context(), "start", nil)
	if code == 0 || result.Status != "activation-authority-unavailable" || result.Operations != operations || f.starts != [2]int{} ||
		result.ActivationReady || result.RootServiceActive || result.ChainSuccessProven || !reflect.DeepEqual(original, f.chain.journals(t)) {
		t.Fatal("native observation fabricated missing authority or changed original custody", code, result, detail)
	}
}

// A current block in the signed block interval cannot replace the independent
// native epoch window. Denial happens before even the injected authority runs.
func TestValidatorActivationNativeEpochDenialPrecedesAuthority(t *testing.T) {
	for _, epoch := range []uint64{19, 31} {
		f := newValidatorActivationFixture(t)
		f.installed()
		f.chain.census.set(t, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, epoch), []byte{25, 0})
		result, code, detail := f.command(t.Context(), "start", f)
		if code == 0 || !strings.Contains(detail, "native epoch window") || f.starts != [2]int{} || f.authorityCalls != 0 || !result.Units[0].StartAt.IsZero() || result.Readiness != nil {
			t.Fatal("unapproved native epoch reached current authority or start", epoch, code, result, detail)
		}
	}
}

// Both the enum and one-mechanism restriction come from actual storage. No
// absent mode or valid-looking prefix is reinterpreted as explicit Recycle.
func TestValidatorActivationNativeModeAndMechanismDenial(t *testing.T) {
	for _, change := range []string{"absent-mode", "burn", "trailing-mode", "mechanism-zero", "mechanism-two"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		arg := []byte{25, 0}
		switch change {
		case "absent-mode":
			subnetTestDelete(t, f.chain.census, "RecycleOrBurn", arg)
		case "burn":
			f.chain.census.set(t, "RecycleOrBurn", []byte{0}, arg)
		case "trailing-mode":
			f.chain.census.set(t, "RecycleOrBurn", []byte{1, 0}, arg)
		case "mechanism-zero":
			f.chain.census.set(t, "MechanismCountCurrent", []byte{0}, arg)
		case "mechanism-two":
			f.chain.census.set(t, "MechanismCountCurrent", []byte{2}, arg)
		}
		result, code, detail := f.command(t.Context(), "start", f)
		if code == 0 || result.Readiness != nil || f.authorityCalls != 0 || f.starts != [2]int{} ||
			!strings.Contains(detail, "Recycle") && !strings.Contains(detail, "mechanism") {
			t.Fatal("native mode or mechanism admitted", change, code, result, detail)
		}
	}
}

// Every role needs the complete exact census vector. Registration can supply
// bounded initial freshness, but cannot conceal a future last-update value.
func TestValidatorActivationNativeActivityDenial(t *testing.T) {
	for _, change := range []string{"future-majority", "future-secondary", "truncated-census"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		updates := make([]byte, 6*8)
		if change == "truncated-census" {
			updates = updates[:5*8]
		} else {
			uid := 2
			if change == "future-secondary" {
				uid = 3
			}
			binary.LittleEndian.PutUint64(updates[uid*8:], 101)
		}
		f.chain.census.set(t, "LastUpdate", subnetTestVector(updates, 8), []byte{25, 0})
		result, code, detail := f.command(t.Context(), "start", f)
		if code == 0 || !strings.Contains(detail, "activity") || result.Readiness != nil || f.authorityCalls != 0 || f.starts != [2]int{} {
			t.Fatal("native activity admitted", change, code, result, detail)
		}
	}
}

// Each role gets the same independently approved age bound; the inclusive
// boundary passes while either role one block older fails before authority.
func TestValidatorActivationNativeActivityHonorsSignedAge(t *testing.T) {
	for _, stale := range []int{-1, 2, 3} {
		f := newValidatorActivationFixtureWithNativeScope(t, nil, func(validator *bootstrapChainValidatorFixture) {
			validator.approval.Production.MaximumLastUpdateAge = 1
		})
		f.installed()
		updates := make([]byte, 6*8)
		for _, uid := range []int{2, 3} {
			last := uint64(99)
			if uid == stale {
				last--
			}
			binary.LittleEndian.PutUint64(updates[uid*8:], last)
		}
		f.chain.census.set(t, "LastUpdate", subnetTestVector(updates, 8), []byte{25, 0})
		result, code, detail := f.command(t.Context(), "admit", nil)
		if stale == -1 {
			if code != 0 || result.Readiness == nil || result.Readiness.Native == nil {
				t.Fatal("signed inclusive native activity boundary did not admit", code, result, detail)
			}
		} else if code == 0 || !strings.Contains(detail, "activity is stale") || result.Readiness != nil {
			t.Fatal("stale native activity ignored original signed maximum age", stale, code, result, detail)
		}
	}
}

// The drain is read at the signed checkpoint; passing current mode/activity
// cannot replace the original clean activation boundary.
func TestValidatorActivationNativeCheckpointDrainDenial(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	f.chain.census.set(t, "PendingServerEmission", binary.LittleEndian.AppendUint64(nil, 1), []byte{25, 0})
	result, code, detail := f.command(t.Context(), "start", f)
	if code == 0 || !strings.Contains(detail, "drained first epoch") || result.Readiness != nil || f.authorityCalls != 0 || f.starts != [2]int{} {
		t.Fatal("undrained activation checkpoint reached authority", code, result, detail)
	}
}

// Advancing finality stays on the same synthetic branch. Only the two new
// identity replies are replaced; all census/storage bytes still use real reads.
func TestValidatorActivationNativeArchiveCheckpointAndCurrentEpoch(t *testing.T) {
	for _, wrongCheckpointEpoch := range []bool{false, true} {
		f := newValidatorActivationFixture(t)
		header, hash := rootReceiptHeaderFixture(t, testFinalizedHash, 101, nil, false)
		original := f.chain.client.httpClient.Transport
		epochKey := f.chain.census.set(t, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 20), []byte{25, 0})
		drainKey := f.chain.census.set(t, "PendingServerEmission", make([]byte, 8), []byte{25, 0})
		checkpointDrains := 0
		f.chain.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			var call struct {
				JsonRpc string            `json:"jsonrpc"`
				Id      int               `json:"id"`
				Method  string            `json:"method"`
				Params  []json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				return nil, err
			}
			atCurrent := len(call.Params) != 0 && string(call.Params[len(call.Params)-1]) == `"`+hash+`"`
			currentHeight := call.Method == "chain_getBlockHash" && string(call.Params[0]) == "101"
			if atCurrent {
				call.Params[len(call.Params)-1] = json.RawMessage(`"` + testFinalizedHash + `"`)
			}
			if currentHeight {
				call.Params[0] = json.RawMessage("100")
			}
			raw, err := json.Marshal(call)
			if err != nil {
				return nil, err
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			response, err := original.RoundTrip(request)
			if err != nil {
				return response, err
			}
			if call.Method == "chain_getFinalizedHead" || currentHeight {
				return validatorActivationNativeTestReply(response, hash)
			}
			if call.Method == "chain_getHeader" && atCurrent {
				return validatorActivationNativeTestReply(response, header)
			}
			if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+epochKey+`"` && (atCurrent || wrongCheckpointEpoch) {
				return validatorActivationNativeTestReply(response, "0x1500000000000000")
			}
			if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+drainKey+`"` {
				if atCurrent {
					response.Body.Close()
					return nil, errors.New("activation required a later current block to be drained")
				}
				checkpointDrains++
			}
			return response, nil
		})
		readiness := validatorActivationNativeTestReadiness(t, f)
		got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
		if wrongCheckpointEpoch {
			if got != nil || !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "drained first epoch") {
				t.Fatal("current epoch concealed wrong independently approved checkpoint epoch", got, err)
			}
		} else if err != nil || got == nil || got.NativeEpoch != 21 || got.ActivationEpoch != 20 || got.ActivationBlock != 100 || got.ActivationHash != testFinalizedHash || checkpointDrains != 1 {
			t.Fatal("advancing finality relabeled the original activation checkpoint or required current drain", got, err, checkpointDrains)
		}
	}
}

// Approval of an artifact is not approval to reinterpret its storage codec or
// defaults. These independently signed fixtures fail before any native state.
func TestValidatorActivationNativeRejectsApprovedCodecDrift(t *testing.T) {
	for _, change := range []string{"pending-default", "pending-hasher", "epoch-type"} {
		f := newValidatorActivationFixtureWithNativeMetadata(t, func(metadata *types.Metadata) {
			for pi := range metadata.AsMetadataV14.Pallets {
				pallet := &metadata.AsMetadataV14.Pallets[pi]
				if pallet.Name != "SubtensorModule" {
					continue
				}
				for i := range pallet.Storage.Items {
					entry := &pallet.Storage.Items[i]
					if change == "pending-default" && entry.Name == "PendingServerEmission" {
						entry.Fallback[0] = 1
					}
					if change == "pending-hasher" && entry.Name == "PendingServerEmission" {
						entry.Type.AsMap.Hashers = []types.StorageHasherV10{{IsTwox64Concat: true}}
					}
					if change == "epoch-type" && entry.Name == "SubnetEpochIndex" {
						entry.Type.AsMap.Value = entry.Type.AsMap.Key
						entry.Fallback = []byte{0, 0}
					}
				}
			}
		})
		readiness := validatorActivationNativeTestReadiness(t, f)
		before := f.chain.census.count("state_getStorage")
		got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
		if got != nil || !errors.Is(err, errRpcIntegrity) || f.chain.census.count("state_getStorage") != before {
			t.Fatal("approved native codec drift reached storage or partial evidence", change, got, err)
		}
	}
}

// A second native artifact authentication is required even after the bootstrap
// census succeeded. Drift is rejected before the newly interpreted state.
func TestValidatorActivationNativeRejectsRuntimeAndCheckpointHeaderDrift(t *testing.T) {
	for _, change := range []string{"runtime", "code", "metadata", "checkpoint-header"} {
		f := newValidatorActivationFixture(t)
		readiness := validatorActivationNativeTestReadiness(t, f)
		headers := 0
		validatorActivationNativeTestIntercept(t, f.chain.client, func(call validatorActivationNativeTestCall, response *http.Response) (*http.Response, error) {
			if call.Method == "chain_getHeader" {
				headers++
				if change == "checkpoint-header" && headers == 2 {
					header := identityTestHeader()
					header.StateRoot = testGenesisHash
					return validatorActivationNativeTestReply(response, header)
				}
			}
			if change == "runtime" && call.Method == "state_getRuntimeVersion" {
				version := f.chain.census.version
				version.StateVersion++
				return validatorActivationNativeTestReply(response, version)
			}
			if change == "code" && call.Method == "state_getStorageHash" {
				return validatorActivationNativeTestReply(response, testGenesisHash)
			}
			if change == "metadata" && call.Method == "state_getMetadata" {
				return validatorActivationNativeTestReply(response, "0x00")
			}
			return response, nil
		})
		got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
		if got != nil || !errors.Is(err, errRpcIntegrity) {
			t.Fatal("native runtime or complete checkpoint header drift admitted", change, got, err)
		}
	}
}

// Initial bootstrap config inspection excludes signed runtime/authority
// histories. A newer artifact has no implied continuity under these old bytes.
func TestValidatorActivationInitialScopeRejectsRuntimeUpgrade(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	original := f.chain.journals(t)
	f.chain.census.version.SpecVersion++
	result, code, detail := f.command(t.Context(), "start", f)
	if code == 0 || result.Readiness != nil || f.authorityCalls != 0 || f.starts != [2]int{} ||
		!strings.Contains(detail, "runtime") || !reflect.DeepEqual(original, f.chain.journals(t)) {
		t.Fatal("initial runtime approval silently acquired upgrade authority", code, result, detail)
	}
}

// Both closing canonical barriers occur after the final prerequisite read.
// Neither a stale successful census nor the first barrier survives the second.
func TestValidatorActivationNativeClosingAnchorsDiscardPartialEvidence(t *testing.T) {
	for _, barrier := range []int{1, 2} {
		f := newValidatorActivationFixture(t)
		readiness := validatorActivationNativeTestReadiness(t, f)
		key := f.chain.census.set(t, "PendingServerEmission", make([]byte, 8), []byte{25, 0})
		drain, anchors := false, 0
		validatorActivationNativeTestIntercept(t, f.chain.client, func(call validatorActivationNativeTestCall, response *http.Response) (*http.Response, error) {
			if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+key+`"` {
				drain = true
			}
			if drain && call.Method == "chain_getBlockHash" && string(call.Params[0]) == "100" {
				anchors++
				if anchors == barrier {
					return validatorActivationNativeTestReply(response, testGenesisHash)
				}
			}
			return response, nil
		})
		got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
		if got != nil || !errors.Is(err, errRpcIntegrity) || anchors != barrier || !strings.Contains(err.Error(), "anchor changed") {
			t.Fatal("closing canonical fork published native prerequisites", barrier, got, err, anchors)
		}
	}
}

// One finalized hash must not yield two different epoch values even when each
// independently lies in an approved window and the checkpoint is drained.
func TestValidatorActivationNativeRejectsContradictorySameBlockStorage(t *testing.T) {
	f := newValidatorActivationFixture(t)
	readiness := validatorActivationNativeTestReadiness(t, f)
	key := f.chain.census.set(t, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 20), []byte{25, 0})
	reads := 0
	validatorActivationNativeTestIntercept(t, f.chain.client, func(call validatorActivationNativeTestCall, response *http.Response) (*http.Response, error) {
		if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+key+`"` {
			reads++
			if reads == 1 {
				return validatorActivationNativeTestReply(response, "0x1500000000000000")
			}
		}
		return response, nil
	})
	got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
	if got != nil || !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "changed at one native hash") || reads != 2 {
		t.Fatal("contradictory same-block native evidence admitted", got, err, reads)
	}
}

// A second observation cannot silently relabel the original complete census at
// the same hash. Current generation, owner and cardinality all stay bound.
func TestValidatorActivationNativeRejectsCensusContradiction(t *testing.T) {
	for _, change := range []string{"generation", "owner", "count"} {
		f := newValidatorActivationFixture(t)
		readiness := validatorActivationNativeTestReadiness(t, f)
		switch change {
		case "generation":
			f.chain.census.set(t, "RegisteredSubnetCounter", binary.LittleEndian.AppendUint64(nil, 4), []byte{25, 0})
		case "owner":
			f.chain.census.set(t, "SubnetOwner", bytes.Repeat([]byte{0x73}, 32), []byte{25, 0})
		case "count":
			f.chain.census.set(t, "SubnetworkN", []byte{5, 0}, []byte{25, 0})
		}
		got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
		if got != nil || !errors.Is(err, errRpcIntegrity) {
			t.Fatal("native reread replaced the original finalized census", change, got, err)
		}
	}
}

// A temporary unavailable reply retries the identical storage key and block;
// no new finalized head or changed signed epoch is selected by that retry.
func TestValidatorActivationNativeRetriesTransientAtExactHash(t *testing.T) {
	f := newValidatorActivationFixture(t)
	readiness := validatorActivationNativeTestReadiness(t, f)
	key := f.chain.census.set(t, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 20), []byte{25, 0})
	attempts := 0
	validatorActivationNativeTestIntercept(t, f.chain.client, func(call validatorActivationNativeTestCall, response *http.Response) (*http.Response, error) {
		if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+key+`"` {
			attempts++
			if string(call.Params[1]) != `"`+testFinalizedHash+`"` {
				return nil, errors.New("native retry changed its exact hash")
			}
			if attempts == 1 {
				response.StatusCode = http.StatusServiceUnavailable
			}
		}
		return response, nil
	})
	got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness)
	if err != nil || got == nil || got.NativeEpoch != 20 || attempts != 3 {
		t.Fatal("transient native read did not retry the original exact scope", got, err, attempts)
	}
}

// Cancellation at the checkpoint read synchronously discards the candidate;
// the next call can reuse original custody and the same owned client.
func TestValidatorActivationNativeCancellationRetainsOriginalCustody(t *testing.T) {
	f := newValidatorActivationFixture(t)
	readiness := validatorActivationNativeTestReadiness(t, f)
	original := f.chain.journals(t)
	key := f.chain.census.set(t, "PendingServerEmission", make([]byte, 8), []byte{25, 0})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	transport := f.chain.client.httpClient.Transport
	validatorActivationNativeTestIntercept(t, f.chain.client, func(call validatorActivationNativeTestCall, response *http.Response) (*http.Response, error) {
		if call.Method == "state_getStorage" && string(call.Params[0]) == `"`+key+`"` {
			cancel()
		}
		return response, nil
	})
	got, err := f.chain.client.observeValidatorActivationNative(ctx, f.chain.preparation, readiness)
	if got != nil || !errors.Is(err, context.Canceled) || !reflect.DeepEqual(original, f.chain.journals(t)) {
		t.Fatal("canceled native observation published partial evidence or changed custody", got, err)
	}
	f.chain.client.httpClient.Transport = transport
	if got, err := f.chain.client.observeValidatorActivationNative(t.Context(), f.chain.preparation, readiness); err != nil || got == nil {
		t.Fatal("canceled native observation retained an owner or poisoned retry", got, err)
	}
}

// Slow read-only admission had no terminal age check. Clock movement is driven
// by the owner's exact now calls, without sleeps or a short timeout oracle.
func TestValidatorActivationAdmissionRejectsExpiredObservationAndRollback(t *testing.T) {
	for _, change := range []string{"age", "rollback", "advance"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		result, err := advanceValidatorActivation(t.Context(), store, f.host, nil, "admit", func() time.Time {
			calls++
			if change == "advance" && calls >= 3 {
				return f.now.Add(time.Second)
			}
			if calls == 3 {
				if change == "age" {
					return f.now.Add(121 * time.Second)
				}
				return f.now.Add(-time.Second)
			}
			return f.now
		})
		retained, loadErr := store.load(t.Context())
		closeErr := store.close()
		if change == "advance" {
			if err != nil || loadErr != nil || closeErr != nil || !retained.HighWaterAt.Equal(f.now.Add(time.Second)) || result.Readiness == nil || result.Status != "admitted-process-only" {
				t.Fatal("successful admission did not retain its completion clock", result, err, loadErr, closeErr)
			}
			continue
		}
		if err == nil || closeErr != nil || calls != 3 || result.Readiness != nil || result.Status != "source-refused" || f.starts != [2]int{} {
			t.Fatal("admission labeled a stale or backwards-clock observation current", change, result, err, closeErr, calls)
		}
	}
}

// Publishing the observation can block too. Post-sync refusal keeps that
// historical evidence and the spent operation, without reporting it current.
func TestValidatorActivationAdmissionPostSyncFreshness(t *testing.T) {
	for _, change := range []string{"age", "rollback", "cancel"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		ctx, cancel := context.WithCancel(t.Context())
		store, err := openValidatorActivationStore(ctx, f.approval, f.key, false, f.now)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		before, err := store.load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		reached := false
		store.syncDirectory = func(file *os.File) error {
			raw, err := os.ReadFile(store.path)
			if err != nil {
				return err
			}
			var record validatorActivationRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			if record.Status == "admitted-process-only" {
				reached = true
				switch change {
				case "age":
					f.now = f.now.Add(121 * time.Second)
				case "rollback":
					f.now = f.now.Add(-time.Second)
				case "cancel":
					cancel()
				}
			}
			return file.Sync()
		}
		result, err := advanceValidatorActivation(ctx, store, f.host, nil, "admit", func() time.Time { return f.now })
		closeErr := store.close()
		cancel()
		if err == nil || closeErr != nil || !reached || result.Status != "source-refused" || result.Readiness == nil || result.Operations != before.Operations+1 || f.starts != [2]int{} {
			t.Fatal("post-sync admission reported stale evidence current or refunded its operation", change, result, err, closeErr)
		}
	}
}

// Old consumed-start journals retain their original scope after the new native
// observer is added. Recovery does not request new authority or repeat a start.
func TestValidatorActivationLegacyReadinessRecoversConsumedStart(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	f.progress[0] = false
	if _, code, detail := f.command(t.Context(), "start", f); code == 0 || f.starts != [2]int{1, 0} {
		t.Fatal("synthetic initial partial start unavailable", code, detail)
	}
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, readiness := range []*validatorActivationReadiness{record.Readiness, record.Units[0].Readiness} {
		readiness.Native = nil
		for i := range readiness.Roles {
			role := &readiness.Roles[i]
			role.ApprovedCheckpointHash, role.ObservedCheckpointHash = "", ""
			role.ActivationBlockers = append([]string{"NATIVE_EPOCH_APPROVAL_WINDOW_UNVERIFIED", "SIGNED_ACTIVATION_CHECKPOINT_UNVERIFIED"}, role.ActivationBlockers...)
		}
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	f.publish(0)
	before := f.chain.census.count("chain_getFinalizedHead")
	result, code, detail := f.command(t.Context(), "resume", nil)
	if code == 0 || result.Status != "activation-authority-unavailable" || result.Units[0].Completed == nil || result.Units[0].Readiness.Native != nil ||
		f.starts != [2]int{1, 0} || !result.Units[1].StartAt.IsZero() || f.chain.census.count("chain_getFinalizedHead") != before {
		t.Fatal("legacy start recovery acquired new native scope or renewed consumption", code, result, detail)
	}
}
