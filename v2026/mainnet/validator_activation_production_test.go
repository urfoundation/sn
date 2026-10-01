// Current contract observations execute the reviewed contracts in the local
// EVM and use real HTTP reads. Public command tests preserve both start slots.
package main

import (
	"context"
	"encoding/hex"
	"maps"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urfoundation/sn/v2026/validator"
)

// The complete original graph executes once, then the actual owner fixes the
// evidence pointer in the test EVM. No RPC method executes this ninth action.
func newValidatorActivationContractFixture(t *testing.T) (*evmCreateFixture, *rpcClient, []evmCreatePlan, finalizedMapping) {
	t.Helper()
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal("synthetic evidence create", diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 {
		t.Fatal("synthetic evidence receipt", diagnostic)
	}
	func() {
		f.stateLock.Lock()
		defer f.stateLock.Unlock()
		vm := f.vm
		vm.Origin, vm.GasLimit = f.plan.Proxy.ProxyConstructor.Owner, 2_000_000
		if _, _, err := runtime.Call(f.plan.Proxy.Address, stabi.NewSTCoordinator().PackFixValidatorEvidence(f.plan.Address), &vm); err != nil {
			t.Fatal("synthetic actual evidence anchor", err)
		}
		f.advanceEmpty()
	}()
	client, err := newOwnedSubmissionClient(f.config.Plan.Route)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	identity, err := client.readIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	plans := make([]evmCreatePlan, 8)
	for i := range plans {
		if i == 7 {
			plans[i] = f.plan
		} else {
			plans[i] = f.plan.priorPlan(i)
		}
	}
	return f, client, plans, mapping
}

// All five real executables and their stable domain/storage views agree at
// the same native-header-authenticated EVM point, without sending anything.
func TestValidatorActivationProductionContractsObserveExecutedGraph(t *testing.T) {
	f, client, plans, mapping := newValidatorActivationContractFixture(t)
	transport := &bootstrapContractCurrentTransport{base: client.httpClient.Transport}
	client.httpClient.Transport = transport
	// The borrowed transport remains owned by the fixture client; retain its
	// concrete close hook because the tracing wrapper exposes only RoundTrip.
	t.Cleanup(func() { client.httpClient.Transport = transport.base })
	f.stateLock.Lock()
	writes := len(f.writes)
	before := maps.Clone(f.counts)
	f.stateLock.Unlock()
	got, err := client.observeValidatorActivationContracts(t.Context(), plans, mapping)
	if err != nil || len(got) != 5 {
		t.Fatal("actual approved contract graph refused", got, err)
	}
	f.stateLock.Lock()
	changed := len(f.writes) != writes || f.counts["eth_sendRawTransaction"] != before["eth_sendRawTransaction"]
	counts := maps.Clone(f.counts)
	f.stateLock.Unlock()
	if changed {
		t.Fatal("contract observation submitted a transaction")
	}
	expectedCounts := map[string]int{"eth_getCode": 5, "eth_call": 1, "eth_getStorageAt": 0}
	for i, index := range []int{2, 4, 5, 6, 7} {
		getters, storage := validatorActivationContractViews(plans[index], index, plans[7].Address)
		expectedCounts["eth_call"] += len(getters)
		expectedCounts["eth_getStorageAt"] += len(storage)
		if got[i].Address != plans[index].Address || !rootCanonicalHash(got[i].RuntimeHash) || !planSha256(got[i].GettersHash) || !planSha256(got[i].StorageHash) {
			t.Fatal("contract observation lost exact source", got[i])
		}
	}
	for method, expected := range expectedCounts {
		if counts[method]-before[method] != expected {
			t.Fatal("contract read profile omitted or repeated a method", method, counts[method]-before[method], expected)
		}
	}
	transport.stateLock.Lock()
	defer transport.stateLock.Unlock()
	if len(transport.blocks) != expectedCounts["eth_getCode"]+expectedCounts["eth_call"]+expectedCounts["eth_getStorageAt"] {
		t.Fatal("contract read bypassed the owned HTTP transport")
	}
	for i, block := range transport.blocks {
		if !reflect.DeepEqual(block, map[string]any{"blockHash": mapping.EvmHeader.Hash, "requireCanonical": true}) || transport.deadlines[i].IsZero() {
			t.Fatal("contract read lost its canonical selector or finite deadline", block, transport.deadlines[i])
		}
	}
}

// Actual current reads fail on changed runtime, getter or slot at each
// deployed account. The original approved plan and source stay untouched.
func TestValidatorActivationProductionContractsRejectEverySubstitution(t *testing.T) {
	f, client, plans, mapping := newValidatorActivationContractFixture(t)
	for _, index := range []int{2, 4, 5, 6, 7} {
		getters, storage := validatorActivationContractViews(plans[index], index, plans[7].Address)
		for _, fault := range []string{"runtime", "getter", "storage", "uppercase-runtime", "policy"} {
			if fault == "storage" && len(storage) == 0 {
				continue
			}
			if fault == "policy" && index != 4 {
				continue
			}
			reached := false
			f.stateLock.Lock()
			f.override = func(method string, params []any, value any) any {
				switch {
				case (fault == "runtime" || fault == "uppercase-runtime") && method == "eth_getCode" && params[0] == plans[index].Address.Hex():
					reached = true
					if fault == "uppercase-runtime" {
						return "0x" + strings.ToUpper(value.(string)[2:])
					}
					return value.(string) + "00"
				case fault == "getter" && method == "eth_call" && params[0].(map[string]any)["to"] == plans[index].Address.Hex() && params[0].(map[string]any)["data"] == getters[0].Data:
					reached = true
					return value.(string) + "00"
				case fault == "storage" && method == "eth_getStorageAt" && params[0] == plans[index].Address.Hex() && params[1] == storage[0].Slot:
					reached = true
					return common.Hash{0x55}.Hex()
				case fault == "policy" && method == "eth_call" && params[0].(map[string]any)["to"] == plans[index].Address.Hex() && params[0].(map[string]any)["data"] == "0x"+hex.EncodeToString(stabi.NewSTCoordinator().PackPolicyByIndex(big.NewInt(0))):
					reached = true
					return value.(string) + "00"
				}
				return value
			}
			f.stateLock.Unlock()
			got, err := client.observeValidatorActivationContracts(t.Context(), plans, mapping)
			f.stateLock.Lock()
			actualReached := reached
			f.stateLock.Unlock()
			if got != nil || err == nil || !actualReached {
				t.Fatal("changed contract source accepted or control not reached", index, fault, got, err)
			}
		}
	}
	f.stateLock.Lock()
	f.override = nil
	f.stateLock.Unlock()
}

// Nonzero accounting/census clocks are allowed after installation; changing
// pause/guardian/implementation remains disallowed. Source getter requests
// are counted so the positive result cannot accidentally use constructor zero.
func TestValidatorActivationProductionExcludesConstructorCountersOnly(t *testing.T) {
	f, client, plans, mapping := newValidatorActivationContractFixture(t)
	coordinator, reserve, vault := stabi.NewSTCoordinator(), stabi.NewSTReserveSink(), stabi.NewSTSettlementVault()
	forbidden := map[string]bool{}
	for _, data := range [][]byte{coordinator.PackCurrentEpoch(), coordinator.PackOperatorCount(), coordinator.PackCampaignReserved(), reserve.PackPrincipal(), vault.PackTotalCaptured(), vault.PackTotalPaid(), vault.PackPendingFunding(), vault.PackOutstandingLiability(), vault.PackEscrowAccounted()} {
		forbidden["0x"+hex.EncodeToString(data)] = true
	}
	seen := map[string]bool{}
	f.stateLock.Lock()
	f.override = func(method string, params []any, result any) any {
		if method == "eth_call" {
			input := params[0].(map[string]any)
			if input["to"] != plans[2].Address.Hex() && forbidden[input["data"].(string)] {
				t.Error("mutable constructor counter used as current invariant", input)
			}
			seen[input["data"].(string)] = true
		}
		return result
	}
	f.stateLock.Unlock()
	if _, err := client.observeValidatorActivationContracts(t.Context(), plans, mapping); err != nil {
		t.Fatal(err)
	}
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	for _, data := range [][]byte{coordinator.PackPaused(), coordinator.PackGuardian(), coordinator.PackPendingGuardian(), coordinator.PackValidatorEvidence(), reserve.PackRecorder(), vault.PackCoordinator()} {
		if !seen["0x"+hex.EncodeToString(data)] {
			t.Fatal("stable authority/binding disappeared from observation")
		}
	}
	f.override = nil
}

// The public operation requires the full original contract profile before
// network work and preserves the original custody, both starts and nil gate.
func TestValidatorActivationProductionCommandRefusesIncompleteOriginalProfile(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	original := f.chain.journals(t)
	before, code, diagnostic := f.command(t.Context(), "status", nil)
	if code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command(t.Context(), "admit-evidence", nil)
	if code != 3 || result.Status != "source-refused" || result.Operations != before.Operations+1 || !strings.Contains(diagnostic, "complete approved contract profile") || result.Readiness != nil || result.ActivationReady || f.starts != [2]int{} {
		t.Fatal("incomplete profile gained production readiness", code, result, diagnostic)
	}
	if !reflect.DeepEqual(original, f.chain.journals(t)) {
		t.Fatal("read-only evidence request altered original custody")
	}
	result, code, diagnostic = f.command(t.Context(), "start", nil)
	if code != 3 || result.Status != "activation-authority-unavailable" || result.Operations != before.Operations+1 || f.starts != [2]int{} {
		t.Fatal("evidence mode opened or consumed start", code, result, diagnostic)
	}
}

// A syntactically valid retained observation is deliberately constructed here
// to test the trust boundary: a journal can never install a current authority.
func TestValidatorActivationProductionRetainedProjectionCannotOpenPublicStart(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	admitted, code, diagnostic := f.command(t.Context(), "admit", nil)
	if code != 0 {
		t.Fatal(diagnostic)
	}
	store, err := openValidatorActivationStore(t.Context(), f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	projection := &validatorActivationProductionReadiness{Schema: validatorActivationProductionSchema, ContractPlanHash: rootObjectHash("synthetic contract plan"), MappingHash: rootObjectHash("synthetic mapping"), EvmBlock: 1, EvmHash: common.Hash{1}.Hex()}
	for i := 0; i < 5; i++ {
		projection.Contracts = append(projection.Contracts, validatorActivationContractObservation{Address: common.Address{byte(i + 1)}, RuntimeHash: common.Hash{byte(i + 1)}.Hex(), GettersHash: rootObjectHash("synthetic getters"), StorageHash: rootObjectHash("synthetic slots")})
	}
	for i, unit := range f.approval.Plan.Units {
		projection.Validators = append(projection.Validators, validator.ProductionBootstrapObservation{ConfigHash: unit.Unit.Config.Sha256, ValidatorId: unit.Source.ValidatorId, DeploymentId: unit.Source.DeploymentId,
			Native:   validator.ProductionBootstrapNativePoint{Block: record.Readiness.FinalizedNumber, Hash: common.HexToHash(record.Readiness.FinalizedHash), Epoch: record.Readiness.Native.NativeEpoch, Hotkey: common.HexToHash(record.Readiness.Roles[i].Expected.Hotkey)},
			EvmBlock: projection.EvmBlock, EvmHash: projection.EvmHash, Operators: []validator.ProductionBootstrapOperatorObservation{{NoId: 1, ActivationHash: common.Hash{1}.Hex(), PublishedBlock: 1, ClientId: "synthetic-client", ClientKey: common.Hash{2}.Hex(), ClientKeyGeneration: 1, ClientKeyRegistrationHash: common.Hash{3}.Hex(), ClientKeyResponseHash: common.Hash{4}.Hex(), ObservationNonce: common.Hash{5}.Hex(), RootSigner: common.Address{6}.Hex()}}})
	}
	projection.EvidenceHash = rootObjectHash(*projection)
	record.Readiness.Production = projection
	record.Status = "observed-operator-and-contract-evidence"
	if err := store.save(record); err != nil {
		_ = store.close()
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	result, code, diagnostic := f.command(context.Background(), "start", nil)
	if code != 3 || result.Status != "activation-authority-unavailable" || result.Operations != admitted.Operations || f.starts != [2]int{} || result.ActivationReady {
		t.Fatal("retained observation installed public authority", code, result, diagnostic)
	}
	for _, role := range result.Readiness.Roles {
		for _, blocker := range []string{"PRODUCTION_ADMISSION_AND_OPERATOR_HEALTH_UNVERIFIED", "DEPLOYED_CONTRACTS_UNVERIFIED", "SIGNING_DEVICE_AND_GLOBAL_CUSTODY_FENCE_UNVERIFIED"} {
			if !slices.Contains(role.ActivationBlockers, blocker) {
				t.Fatal("bounded projection erased unresolved authority", blocker)
			}
		}
	}
}
