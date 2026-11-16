//go:build linux || darwin

// Actual signed files and canonical RPC fixtures exercise prefix admission;
// timing hooks control deadlines only and never return a proof verdict.
package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Config-selected files contain the same explicitly empty complete batch.
// No current mutable state or signing file is consumed by the new reader.
func productionBootstrapPrefixTestInputs(t *testing.T, fixture *releaseBootstrapV2TestFixture) ProductionBootstrapObservation {
	t.Helper()
	history := ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}
	raw, err := history.CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	result := ProductionBootstrapObservation{ConfigHash: "sha256:" + strings.Repeat("1", 64), DeploymentId: fixture.cfg.DeploymentID, ValidatorId: fixture.cfg.ValidatorID,
		Native:   ProductionBootstrapNativePoint{Block: fixture.contexts[0].Activation.NativeBlock + 1, Hash: [32]byte{0xa1}, Epoch: 8, Hotkey: fixture.contexts[0].Activation.Hotkey},
		EvmBlock: fixture.contexts[0].InitialCut.Boundary.EVMBlock, EvmHash: fixture.contexts[0].InitialCut.Boundary.EVMBlockHash}
	for i := range fixture.cfg.EvidenceV2.Operators {
		file := &fixture.cfg.EvidenceV2.Operators[i].History
		*file = writeReleaseBootstrapV2TestFile(t, file.Path, raw)
		digest, err := fixture.contexts[i].Activation.Digest()
		if err != nil {
			t.Fatal(err)
		}
		result.Operators = append(result.Operators, ProductionBootstrapOperatorObservation{NoId: fixture.cfg.Operators[i].NoID, ActivationHash: attemptHex32(digest),
			ClientId: "synthetic-client", ClientKeyGeneration: 1, ClientKey: attemptHex32(fixture.contexts[i].Activation.VPK), ClientKeyRegistrationHash: attemptHex32([32]byte{0xb2}), ClientKeyResponseHash: attemptHex32([32]byte{0xb3}), ObservationNonce: attemptHex32([32]byte{0xb4})})
	}
	return result
}

// Valid signatures and explicit empty history prove an approved zero prefix,
// never an absent file, mutable-prefix completeness or a source-auth token.
func TestProductionBootstrapPrefixReplaysExplicitOrigin(t *testing.T) {
	f := newReleaseBootstrapV2TestFixture(t)
	observed := productionBootstrapPrefixTestInputs(t, f)
	before := f.calls()
	inputs, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, observed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := replayProductionBootstrapPrefix(t.Context(), &f.cfg, inputs, nil, observed)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if f.calls() != before || len(got.Prefixes) != 2 || got.CurrentPrefixProven || got.HistoricalSources || got.ContentHash != "" || got.ClientDomainHash != productionBootstrapPrefixHash(observed.Operators) {
		t.Fatal("mathematics acquired source or current-worker authority", got)
	}
	for _, prefix := range got.Prefixes {
		if prefix.LastSequence != 0 || prefix.Root != zeroAttemptHash() || prefix.Generation != 1 {
			t.Fatal("pristine signed prefix differs", prefix)
		}
	}
	if err := os.Remove(f.cfg.EvidenceV2.Operators[1].History.Path); err != nil {
		t.Fatal(err)
	}
	if inputs, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, observed); err == nil || inputs != nil {
		t.Fatal("missing history became an empty origin")
	}
}

// Every route selector has a negative control after the genuine full census
// succeeds. Current client identities cannot replace historical proof domains.
func TestProductionBootstrapPrefixRejectsDomainAndPinSubstitution(t *testing.T) {
	for _, fault := range []string{"operator", "activation", "client-key", "generation", "hotkey", "future-native", "future-evm", "history", "signature"} {
		f := newReleaseBootstrapV2TestFixture(t)
		observed := productionBootstrapPrefixTestInputs(t, f)
		if _, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, observed); err != nil {
			t.Fatal("control", fault, err)
		}
		switch fault {
		case "operator":
			observed.Operators[1].NoId++
		case "activation":
			observed.Operators[1].ActivationHash = attemptHex32([32]byte{0xcc})
		case "client-key":
			observed.Operators[1].ClientKey = ""
		case "generation":
			observed.Operators[1].ClientKeyGeneration = 0
		case "hotkey":
			observed.Native.Hotkey[0] ^= 1
		case "future-native":
			observed.Native.Block = f.contexts[0].Activation.NativeBlock - 1
		case "future-evm":
			observed.EvmBlock--
		case "history":
			if err := os.WriteFile(f.cfg.EvidenceV2.Operators[1].History.Path, []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		case "signature":
			if err := os.WriteFile(f.cfg.EvidenceV2.Operators[1].VPKSignature.Path, make([]byte, 64), 0600); err != nil {
				t.Fatal(err)
			}
		}
		before := f.calls()
		if got, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, observed); err == nil || got != nil || f.calls() != before {
			t.Fatal("substitution reached network or admission", fault, err)
		}
	}
}

// A correctly content-addressed but semantically altered batch must reach the
// real replay refusal rather than being projected directly from its headers.
func TestProductionBootstrapPrefixRejectsRepinnedFalseHistory(t *testing.T) {
	f := newReleaseBootstrapV2TestFixture(t)
	observed := productionBootstrapPrefixTestInputs(t, f)
	for i := range f.cfg.EvidenceV2.Operators {
		ref := &f.cfg.EvidenceV2.Operators[i].History
		*ref = writeReleaseBootstrapV2TestFile(t, ref.Path, []byte("{\"schema\":\"urnetwork-validator-activation-history-v2\",\"legacy_closures\":null}\n"))
	}
	inputs, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, observed)
	if err != nil {
		t.Fatal("semantic control did not reach replay", err)
	}
	if got, err := replayProductionBootstrapPrefix(t.Context(), &f.cfg, inputs, nil, observed); got != nil || err == nil {
		t.Fatal("repinned false empty history became proof")
	}
}

// Original activation RPC and initial coordinator views share one actual
// endpoint; independent native metadata/stake reads keep their original owner.
type productionBootstrapPrefixChainFixture struct {
	*releaseInitialBoundaryV2TestFixture
}

// Journal and coordinator selectors route to their existing exact ABI oracles.
func (self *productionBootstrapPrefixChainFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	initial := self.contexts[0]
	if common.BytesToAddress(call["to"]) == common.Address(initial.Activation.Domain.Coordinator) && selector.BlockHash != nil && *selector.BlockHash == common.Hash(initial.ObservedEVMHash) {
		coordinator := stabi.NewSTCoordinator()
		epoch := new(big.Int).SetUint64(initial.InitialCut.Boundary.SettlementEpoch)
		if bytes.Equal(call["input"], coordinator.PackCurrentEpoch()) || bytes.Equal(call["input"], coordinator.PackPolicyAt(epoch)) {
			return self.releaseInitialBoundaryV2TestFixture.Call(ctx, call, selector)
		}
		for _, configured := range self.contexts {
			if bytes.Equal(call["input"], coordinator.PackOperatorAt(new(big.Int).SetUint64(configured.Activation.NoID), epoch)) {
				return self.releaseInitialBoundaryV2TestFixture.Call(ctx, call, selector)
			}
		}
	}
	return self.releaseBootstrapV2TestFixture.Call(ctx, call, selector)
}

// Actual native activation eligibility and both initial policy boundaries must
// succeed. A changed coordinator policy is refused even with valid signatures.
func TestProductionBootstrapPrefixAuthenticatesActualHistoricalSources(t *testing.T) {
	for _, fault := range []string{"", "policy"} {
		f := &productionBootstrapPrefixChainFixture{releaseInitialBoundaryV2TestFixture: newReleaseInitialBoundaryV2TestFixture(t, fault)}
		observed := productionBootstrapPrefixTestInputs(t, f.releaseBootstrapV2TestFixture)
		inputs, err := readProductionBootstrapPrefixInputs(t.Context(), &f.cfg, observed)
		if err != nil {
			t.Fatal(err)
		}
		server := gethrpc.NewServer()
		if err := server.RegisterName("eth", f); err != nil {
			t.Fatal(err)
		}
		endpoint := httptest.NewServer(server)
		t.Cleanup(func() { endpoint.Close(); server.Stop() })
		chain, err := DialReleaseChainContext(t.Context(), []string{endpoint.URL}, common.HexToAddress(f.cfg.Coordinator))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(chain.Close)
		err = authenticateProductionBootstrapPrefix(t.Context(), &f.cfg, inputs, chain, f.native, f.providers[0].native.expected)
		if fault == "" && (err != nil || f.views.Load() != 6) {
			t.Fatal("real historical control", err, f.views.Load())
		}
		if fault != "" && err == nil {
			t.Fatal("historical policy substitution accepted")
		}
	}
}

// Retry hooks select no source or proof value. The first actual HTTP timeout
// response closes before a second read succeeds, using unchanged proof bytes.
func TestProductionBootstrapPrefixReadRetriesWithSixtySecondAttempts(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte("source"))
	}))
	t.Cleanup(server.Close)
	var durations []time.Duration
	waits := 0
	hooks := releaseHttpGetRetryHooks{withTimeout: func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
		durations = append(durations, d)
		return context.WithTimeout(ctx, d)
	}, wait: func(ctx context.Context, d time.Duration) error { waits++; return ctx.Err() }}
	read := func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			return err
		}
		response, err := server.Client().Do(request)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if response.StatusCode != 200 {
			return errors.Join(&releaseHttpGetStatusError{status: response.StatusCode}, readErr, closeErr)
		}
		if !bytes.Equal(raw, []byte("source")) {
			return errors.New("changed response")
		}
		return errors.Join(readErr, closeErr)
	}
	if err := retryProductionBootstrapPrefixRead(t.Context(), read, hooks); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || waits != 1 || !reflect.DeepEqual(durations, []time.Duration{300 * time.Second, 60 * time.Second, 60 * time.Second}) {
		t.Fatal("retry changed bounds or restarted completed reads", calls.Load(), waits, durations)
	}
}

// A simultaneous permanent proof failure cannot be erased by transport retry.
func TestProductionBootstrapPrefixReadRejectsMixedIntegrityFailure(t *testing.T) {
	calls, waits := 0, 0
	hard := errors.New("synthetic signed-prefix contradiction")
	err := retryProductionBootstrapPrefixRead(t.Context(), func(context.Context) error { calls++; return errors.Join(context.DeadlineExceeded, hard) }, releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error { waits++; return nil }})
	if !errors.Is(err, hard) || calls != 1 || waits != 0 {
		t.Fatal("mixed integrity failure retried", err, calls, waits)
	}
}

// Logical deadline closure deterministically exhausts the five-minute owner;
// no short sleeps or negative scheduler observations stand in for expiry.
func TestProductionBootstrapPrefixReadBudgetExpiresWithoutRestart(t *testing.T) {
	var operation *releaseHttpGetTestDeadline
	calls, waits := 0, 0
	hooks := releaseHttpGetRetryHooks{withTimeout: func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
		if d == 300*time.Second {
			operation = &releaseHttpGetTestDeadline{Context: ctx, deadline: time.Now().Add(d), done: make(chan struct{})}
			return operation, func() {
				select {
				case <-operation.done:
				default:
					close(operation.done)
				}
			}
		}
		if d != 60*time.Second {
			t.Fatalf("unexpected attempt budget %s", d)
		}
		return context.WithCancel(ctx)
	}, wait: func(ctx context.Context, d time.Duration) error {
		waits++
		if waits == 5 {
			close(operation.done)
		}
		return ctx.Err()
	}}
	err := retryProductionBootstrapPrefixRead(t.Context(), func(context.Context) error { calls++; return context.DeadlineExceeded }, hooks)
	if !errors.Is(err, context.DeadlineExceeded) || calls != 5 || waits != 5 {
		t.Fatal("bounded source retry restarted or failed to expire", err, calls, waits)
	}
}

// The actual in-flight request observes cancellation and joins before return.
func TestProductionBootstrapPrefixReadCancellationJoinsRequest(t *testing.T) {
	entered, left := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(left) }))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- retryProductionBootstrapPrefixRead(ctx, func(ctx context.Context) error {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			if err != nil {
				return err
			}
			response, err := server.Client().Do(request)
			if response != nil {
				err = errors.Join(err, response.Body.Close())
			}
			return err
		}, releaseHttpGetRetryHooks{})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatal("request failed before its cancellation barrier", err)
	case <-ctx.Done():
		t.Fatal("request never entered", ctx.Err())
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	<-left
}

// The production wrapper refuses substituted original config/current clocks
// before accessing evidence, credentials or live endpoints.
func TestProductionBootstrapPrefixPublicScopeBeforeNetwork(t *testing.T) {
	f := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ObserveProductionBootstrapPrefix(t.Context(), f.path, raw, ProductionBootstrapObservation{}); err == nil || got != nil {
		t.Fatal("unscoped current observation was admitted")
	}
	if _, err := os.Lstat(f.cfg.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("public refusal accessed producer state", err)
	}
}
