// Readiness tests use independently signed synthetic preparation and the real
// finalized census reader. No native key, external route or deployment exists.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Align approval windows with the synthetic finalized block, then claim the
// actual five journals. This is independent test approval, not runtime repair.
func newBootstrapChainReadinessFixture(t *testing.T, ownerOverride ...[]byte) *bootstrapChainFixture {
	t.Helper()
	return newBootstrapChainReadinessFixtureWithCensus(t, nil, nil, ownerOverride...)
}

// Native activation fixtures approve their complete storage profile before
// preparation; ordinary bootstrap fixtures retain the original census profile.
func newBootstrapChainReadinessFixtureWithCensus(t *testing.T, configure func(*rootRpcFixture, *subnetCensusPolicy), configureApproval func(*bootstrapChainValidatorFixture), ownerOverride ...[]byte) *bootstrapChainFixture {
	t.Helper()
	f := newBootstrapChainFixtureWithCensus(t, configure, ownerOverride...)
	for i, fixture := range f.validators {
		fixture.approval.ValidFromNativeBlock = 100
		fixture.approval.Production.ActivationNativeHash = bootstrapChainTestAccount(t, testFinalizedHash)
		if configureApproval != nil {
			configureApproval(fixture)
		}
		f.config.Validators[i].Config = fixture.publish(t)
	}
	action := copyRootAction(f.root.offline.packet.Action)
	action.BirthBlock, action.BirthHash, action.Period = 100, testFinalizedHash, 64
	var err error
	action, err = prepareRootAction(action, f.census.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	f.root.offline.packet = rootOfflineApprove(t, f.root.offline.trust, action, f.root.offline.approvalKey)
	service := copyRootServiceConfig(f.root.plan.Service)
	service.Packet = f.root.offline.packet
	f.rootRole.service(t, service)
	f.rootRole.approve(t)
	bootstrapRootTestWrite(t, f.path, f.config)
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.census.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 1, 0, 0}, 1), []byte{25, 0})
	f.result(t, "apply")
	return f
}

// The public command gets an owned local test route; the existing census
// transport refuses every unpinned or mutating request by construction.
func bootstrapReadinessTestServer(t *testing.T, fixture *rootRpcFixture) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response, err := fixture.roundTrip(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		defer response.Body.Close()
		writer.WriteHeader(response.StatusCode)
		io.Copy(writer, response.Body)
	}))
	t.Cleanup(server.Close)
	return server
}

// Passing observed prerequisites must keep all live authority unresolved, and
// repeat invocations must retain exact child bytes and accepted plan lineage.
func TestBootstrapChainReadinessCommandPreservesCustodyAndNoAuthority(t *testing.T) {
	f := newBootstrapChainReadinessFixture(t)
	if result, code, diagnostic := f.contracts.command("resume", "--signed-transaction", f.contracts.signedPath, "--signed-transaction-hash", f.contracts.signedHash); code != 0 || result.Status != "signed-custody-complete" {
		t.Fatalf("original contract signature import: %d %s", code, diagnostic)
	}
	receipt := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "native-readiness-signature.json"), f.root.offline.receipt(t))
	f.root.result(t, "resume", "--signature-file", receipt.Path, "--signature-sha256", receipt.Sha256)
	f.result(t, "resume")
	server := bootstrapReadinessTestServer(t, f.census)
	original := f.journals(t)
	for attempt := 0; attempt < 2; attempt++ {
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "readiness", &stdout, &stderr, "--rpc", server.URL); code != 0 {
			t.Fatalf("readiness exit %d: %s", code, stderr.String())
		}
		var result bootstrapChainReadiness
		if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		claimed := result.ContentHash
		result.ContentHash = ""
		if claimed != rootObjectHash(result) || result.Status != "observed-prerequisites" || !result.ObservationComplete || result.Census == nil || result.LocalPreparation == nil ||
			result.PlanHash != f.preparation.Plan.ContentHash || result.ActivationReady || result.CurrentAuthorityVerified || result.NativeSigning || result.NetworkEffects || len(result.PendingChainPhases) != 5 {
			t.Fatalf("readiness changed scope or claimed authority: %+v", result)
		}
		if result.LocalPreparation.ContractTransactionHash != f.contracts.tx.Hash().Hex() || result.LocalPreparation.RootExtrinsicHash == "" {
			t.Fatal("readiness lost original public signature lineage", result.LocalPreparation)
		}
		for _, role := range append(result.UrValidators, result.RootValidator) {
			if role.Observed == nil || len(role.ObservationBlockers) != 0 || len(role.ActivationBlockers) == 0 {
				t.Fatalf("role observation conflated admission: %+v", role)
			}
		}
		if !reflect.DeepEqual(original, f.journals(t)) {
			t.Fatal("readiness altered an original marker, signature or allowance")
		}
	}
	if f.census.count("chain_getFinalizedHead") != 14 {
		t.Fatal("readiness did not retain one census and close finality at each dependent boundary")
	}
}

// A hotkey that reappears at the same uid with a new birth or owner must not
// inherit either role's original signed generation. Root remains independent.
func TestBootstrapChainReadinessRejectsStaleRoleGenerations(t *testing.T) {
	for _, change := range []string{"majority-birth", "secondary-owner", "root-birth"} {
		f := newBootstrapChainReadinessFixture(t)
		switch change {
		case "majority-birth":
			f.census.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 99), []byte{25, 0}, []byte{2, 0})
		case "secondary-owner":
			hotkey := bootstrapChainTestAccount(t, f.config.Validators[1].Hotkey)
			f.census.set(t, "Owner", bytes.Repeat([]byte{0x75}, 32), hotkey[:])
		case "root-birth":
			f.census.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 99), []byte{0, 0}, []byte{0, 0})
		}
		result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
		if err != nil || !result.ObservationComplete || result.Status != "blocked" {
			t.Fatalf("%s did not produce complete blocked observation: %+v %v", change, result, err)
		}
		index, blocker := 0, "APPROVED_REGISTRATION_GENERATION_DIFFERS"
		if change == "secondary-owner" {
			index = 1
		}
		role := result.UrValidators[index]
		if change == "root-birth" {
			role, blocker = result.RootValidator, "APPROVED_ROOT_SEAT_GENERATION_DIFFERS"
		}
		if !slices.Contains(role.ObservationBlockers, blocker) {
			t.Fatalf("%s reused old generation: %+v", change, role)
		}
	}
}

// Activity and permit are observed prerequisites rather than inferred from a
// role label or protected registration in the old owner-trim review.
func TestBootstrapChainReadinessReportsActivityPermitAndSubnetConflict(t *testing.T) {
	for _, change := range []string{"inactive", "permit", "subnet-generation"} {
		f := newBootstrapChainReadinessFixture(t)
		blocker := ""
		switch change {
		case "inactive":
			f.census.set(t, "Active", subnetTestVector([]byte{1, 1, 0, 1, 1, 0}, 1), []byte{25, 0})
			blocker = "VALIDATOR_INACTIVE"
		case "permit":
			f.census.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 0, 1, 0, 0}, 1), []byte{25, 0})
			blocker = "VALIDATOR_PERMIT_ABSENT"
		case "subnet-generation":
			f.census.set(t, "RegisteredSubnetCounter", binary.LittleEndian.AppendUint64(nil, 4), []byte{25, 0})
			blocker = "SUBNET_GENERATION_DIFFERS_FROM_APPROVED_POLICY"
		}
		result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
		if err != nil || result.Status != "blocked" || !slices.Contains(result.UrValidators[0].ObservationBlockers, blocker) || len(result.RootValidator.ObservationBlockers) != 0 {
			t.Fatalf("%s hid current prerequisite or changed separate root: %+v %v", change, result, err)
		}
	}
}

// A future signed action and validator approval stay closed at the old census;
// preserving their valid signatures cannot turn a time gate into an admission.
func TestBootstrapChainReadinessRejectsFutureSignedWindows(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if err != nil || result.Status != "blocked" || !slices.Contains(result.RootValidator.ObservationBlockers, "SIGNED_ROOT_MORTAL_WINDOW_CLOSED") ||
		!slices.Contains(result.RootValidator.ObservationBlockers, "SIGNED_ROOT_CHECKPOINT_UNCONFIRMED") {
		t.Fatalf("future root action inherited current authority: %+v %v", result, err)
	}
	for _, role := range result.UrValidators {
		if !slices.Contains(role.ObservationBlockers, "SIGNED_NATIVE_BLOCK_WINDOW_CLOSED") {
			t.Fatal("future validator window admitted", role)
		}
	}
}

// An authenticated census remains useful when the root action was approved on
// a different checkpoint. That semantic conflict blocks the root specifically.
func TestBootstrapChainReadinessRejectsConflictingRootCheckpoint(t *testing.T) {
	f := newBootstrapChainReadinessFixture(t)
	original := f.client.httpClient.Transport
	finalizedQueries := 0
	f.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(request.Body)
		request.Body = io.NopCloser(bytes.NewReader(raw))
		var call struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		response, err := original.RoundTrip(request)
		if err == nil && call.Method == "chain_getBlockHash" && string(call.Params[0]) == "100" {
			finalizedQueries++
			if finalizedQueries == 8 {
				response.Body.Close()
				response.Body = io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x` + strings.Repeat("e", 64) + `"}`))
			}
		}
		return response, err
	})
	result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if err != nil || !result.ObservationComplete || result.Status != "blocked" || !slices.Contains(result.RootValidator.ObservationBlockers, "SIGNED_ROOT_CHECKPOINT_UNCONFIRMED") {
		t.Fatalf("conflicting approved checkpoint admitted: %+v %v", result, err)
	}
}

// Reorgs at the census barrier and at the later checkpoint barrier discard all
// partial role facts. Both negative paths are forced by exact request sequence.
func TestBootstrapChainReadinessReorgNeverPublishesPartialEligibility(t *testing.T) {
	for _, barrier := range []string{"census", "after-checkpoint"} {
		f := newBootstrapChainReadinessFixture(t)
		original := f.journals(t)
		if barrier == "census" {
			f.census.stateLock.Lock()
			f.census.methodCounts = map[string]int{}
			f.census.stateLock.Unlock()
			f.census.forkAfterStorage = true
		} else {
			transport := f.client.httpClient.Transport
			queries := 0
			f.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(request.Body)
				request.Body = io.NopCloser(bytes.NewReader(raw))
				var call struct {
					Method string            `json:"method"`
					Params []json.RawMessage `json:"params"`
				}
				if err := json.Unmarshal(raw, &call); err != nil {
					return nil, err
				}
				response, err := transport.RoundTrip(request)
				if err == nil && call.Method == "chain_getBlockHash" && string(call.Params[0]) == "100" {
					queries++
					if queries == 9 {
						response.Body.Close()
						response.Body = io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x` + strings.Repeat("e", 64) + `"}`))
					}
				}
				return response, err
			})
		}
		result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
		if !errors.Is(err, errRpcIntegrity) || result.ObservationComplete || result.Census != nil || result.Status != "unresolved" || !reflect.DeepEqual(original, f.journals(t)) {
			t.Fatalf("%s published partial evidence or changed custody: %+v %v", barrier, result, err)
		}
		for _, role := range append(result.UrValidators, result.RootValidator) {
			if role.Observed != nil || len(role.ObservationBlockers) == 0 {
				t.Fatal("reorg retained partial role success", role)
			}
		}
	}
}

// Missing runtime support and deterministic transport unavailability emit
// unresolved results, never a success inferred from the retained census.
func TestBootstrapChainReadinessUnavailableRuntimeAndRouteRemainUnresolved(t *testing.T) {
	for _, failure := range []string{"runtime", "route"} {
		f := newBootstrapChainReadinessFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		if failure == "runtime" {
			f.census.version.SpecVersion++
		} else {
			f.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				cancel()
				return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("synthetic unavailable route"))}, nil
			})
		}
		result, err := f.client.observeBootstrapChainReadiness(f.storageContext(ctx), f.preparation)
		cancel()
		if err == nil || result.Status != "unresolved" || result.ObservationComplete || result.Census != nil || result.LocalPreparation == nil || len(result.Blockers) == 0 {
			t.Fatalf("%s synthesized current evidence: %+v %v", failure, result, err)
		}
	}
}

// Every retained owner is needed, even when the parent journal says prepared.
// Readiness cannot recreate missing children or complete interrupted markers.
func TestBootstrapChainReadinessMissingStateNeverRepairsCustody(t *testing.T) {
	for index := 0; index < 5; index++ {
		for _, loss := range []string{"state", "marker", "incomplete-marker"} {
			f := newBootstrapChainReadinessFixture(t)
			path := f.preparation.childPaths()[index]
			if loss != "state" {
				path += ".lock"
			}
			if loss == "incomplete-marker" {
				if err := os.WriteFile(path, []byte("incomplete\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			original, calls := f.journals(t), f.census.count("system_chain")
			result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
			if err == nil || result.Status != "unresolved" || result.LocalPreparation != nil || !reflect.DeepEqual(original, f.journals(t)) || calls != f.census.count("system_chain") {
				t.Fatalf("%d %s repaired state or contacted rpc: %+v %v", index, loss, result, err)
			}
		}
	}
}

// Shared reads conflict with active custody owners and release all earlier
// markers on failure so the original owner can resume unchanged afterward.
func TestBootstrapChainReadinessConflictingOwnerDoesNotLeakLocks(t *testing.T) {
	f := newBootstrapChainReadinessFixture(t)
	owner, err := openRootServiceStore(f.preparation.Root.Service, false, f.storageContext(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if err == nil || result.LocalPreparation != nil {
		t.Fatal("active custody owner was ignored", result, err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if result := f.result(t, "resume"); !result.LocalPreparationComplete {
		t.Fatal("failed readiness leaked original locks")
	}
	result, err = f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if err != nil || result.Status != "observed-prerequisites" {
		t.Fatal("readiness could not reopen", result, err)
	}
}

// Each invocation reloads original signed files before reading custody or rpc;
// accepted parent hashes never supply fallback authority for a lost approval.
func TestBootstrapChainReadinessRestartRequiresOriginalApproval(t *testing.T) {
	f := newBootstrapChainReadinessFixture(t)
	if err := os.Remove(f.validators[0].config.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	original, calls := f.journals(t), f.census.count("system_chain")
	var stdout, stderr bytes.Buffer
	code := f.command(t.Context(), "readiness", &stdout, &stderr, "--rpc", "https://readiness.example")
	if code != 2 || stdout.Len() != 0 || !reflect.DeepEqual(original, f.journals(t)) || calls != f.census.count("system_chain") {
		t.Fatal("missing approval inherited prior admission", code, stderr.String())
	}
}
