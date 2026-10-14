// Actual WebSocket reconnect crosses the production read owner and capability
// checks, retaining its selected block and independent approval throughout.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/urfoundation/sn/v2026/crv4"
)

func TestProductionRuntimeActualWebsocketReconnectReobservesOriginalAuthority(t *testing.T) {
	f := newProductionContinuityPolicyTestFixture(t)
	fixtureClient := f.owner.rpc.native.API.Client
	var fixtureLock sync.Mutex
	var connections, versions atomic.Int64
	var enabled, disconnected atomic.Bool
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		connections.Add(1)
		defer connection.Close()
		for {
			var input struct {
				Id     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := connection.ReadJSON(&input); err != nil {
				return
			}
			args := make([]any, len(input.Params))
			for index, raw := range input.Params {
				if input.Method == "chain_getBlockHash" && index == 0 {
					var number uint64
					if err := json.Unmarshal(raw, &number); err != nil {
						t.Error(err)
						return
					}
					args[index] = number
				} else if err := json.Unmarshal(raw, &args[index]); err != nil {
					t.Error(err)
					return
				}
			}
			if enabled.Load() && input.Method == "state_getRuntimeVersion" && len(args) == 1 && args[0] == f.hashes[150].Hex() {
				if versions.Add(1) == 2 {
					disconnected.Store(true)
					return // The consumed API read loses its actual owned socket.
				}
			}
			fixtureLock.Lock()
			var result json.RawMessage
			err = fixtureClient.CallContext(request.Context(), &result, input.Method, args...)
			fixtureLock.Unlock()
			if err != nil {
				t.Errorf("owned socket fixture %s: %v", input.Method, err)
				return
			}
			if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": input.Id, "result": result}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	// The original external approver explicitly pins this fixture route before
	// admission. Reconnect cannot change that signed configuration later.
	f.owner.cfg.Substrate = []string{endpoint}
	f.owner.resignAndLoad(t)
	f.policy.OriginalApprovalSha256 = sha256.Sum256(f.owner.cfg.ownerRecycleProduction.encoded)
	f.policy.OriginalConfigHash = f.owner.approval.ConfigHash
	f.signPolicy(t)
	f.result.Request.PolicySha256 = sha256.Sum256(f.policyRaw)
	f.signCertificate(t)
	native, err := crv4.DialChainAtContext(t.Context(), endpoint, f.hashes[149])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.API.Client.Close)
	peer, err := crv4.DialChainAtContext(t.Context(), endpoint, f.hashes[149])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.API.Client.Close)
	peerArtifact, err := crv4.ReadRuntimeArtifactAtContext(t.Context(), peer, f.hashes[150], f.candidate)
	if err != nil {
		t.Fatal(err)
	}
	api, client := native.API, native.API.Client
	metadata, runtime := native.Meta, native.Runtime
	originalApproval := bytes.Clone(f.owner.cfg.ownerRecycleProduction.encoded)
	originalPolicy, originalCertificate := bytes.Clone(f.policyRaw), bytes.Clone(f.certificateRaw)
	owner := &ReleaseSteerer{cfg: f.owner.cfg}
	waits, attempts := 0, 0
	var budgets []time.Duration
	owner.productionReadHooks.withTimeout = func(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
		budgets = append(budgets, budget)
		return context.WithTimeout(ctx, budget)
	}
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		return errors.New("whole websocket observation escaped its original read attempt")
	}
	enabled.Store(true)
	var report *ProductionRuntimeContinuityInspection
	err = owner.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
		attempts++
		var err error
		report, err = InspectProductionRuntimeContinuityContext(ctx, native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
		return err
	})
	if err != nil || report == nil || report.NativeHash != f.hashes[150] || report.NativeNumber != 150 || report.SigningAuthority || report.ProductionSelectionInstalled ||
		!disconnected.Load() || attempts != 1 || waits != 0 || connections.Load() != 3 || native.API != api || native.API.Client != client || native.Meta != metadata || native.Runtime != runtime ||
		!slices.Equal(budgets, []time.Duration{300 * time.Second, 60 * time.Second}) || !bytes.Equal(originalApproval, f.owner.cfg.ownerRecycleProduction.encoded) ||
		!bytes.Equal(originalPolicy, f.policyRaw) || !bytes.Equal(originalCertificate, f.certificateRaw) {
		t.Fatalf("production owner failed actual reconnect with retained authority: attempts=%d waits=%d connections=%d budgets=%v report=%+v err=%v", attempts, waits, connections.Load(), budgets, report, err)
	}
	if err := crv4.ValidateRuntimeArtifactOwnerContext(t.Context(), peer, peerArtifact); err != nil {
		t.Fatal(fmt.Errorf("independent socket peer lost original proof: %w", err))
	}
	result, err := InspectProductionRuntimeContinuityContext(t.Context(), peer, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
	if err != nil || result == nil || result.NativeHash != f.hashes[150] {
		t.Fatalf("healthy actual socket peer lost its original authority: %+v %v", result, err)
	}
}
