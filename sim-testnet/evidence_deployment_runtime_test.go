// Real HTTP JSON-RPC fixtures exercise canonical selectors and batch decoding.
// Their explicit synthetic block hash is not presented as on-chain evidence.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Every field except the counter is frozen before starting its HTTP server.
type validatorEvidenceInstallRPC struct {
	head      ChainHead
	address   common.Address
	code      []byte
	responses map[validatorEvidenceInstallCall][]byte
	fail      validatorEvidenceInstallCall
	codeHook  func(context.Context) error
	calls     atomic.Uint64
}

// The full destination and calldata are the fixture routing key.
type validatorEvidenceInstallCall struct {
	address common.Address
	data    string
}

// Public fields are decoded by the real RPC server, not a hand-decoded mock.
type ValidatorEvidenceInstallRPCMessage struct {
	To   common.Address `json:"to"`
	Data hexutil.Bytes  `json:"data"`
}

// Refuses a number/latest selector or another block hash at the transport edge.
func (self *validatorEvidenceInstallRPC) GetCode(ctx context.Context, address common.Address, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if address != self.address || selector.BlockHash != self.head.Hash || !selector.RequireCanonical {
		return nil, errors.New("wrong canonical code request")
	}
	if self.codeHook != nil {
		if err := self.codeHook(ctx); err != nil {
			return nil, err
		}
	}
	return bytes.Clone(self.code), nil
}

// Each lookup checks the exact ABI method, address and EIP-1898 selector.
func (self *validatorEvidenceInstallRPC) Call(_ context.Context, message ValidatorEvidenceInstallRPCMessage, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if selector.BlockHash != self.head.Hash || !selector.RequireCanonical {
		return nil, errors.New("wrong canonical view request")
	}
	key := validatorEvidenceInstallCall{address: message.To, data: hexutil.Encode(message.Data)}
	if key == self.fail {
		return nil, errors.New("fixture canonical history refusal")
	}
	value, exists := self.responses[key]
	if !exists {
		return nil, fmt.Errorf("unexpected full target/calldata %v", key)
	}
	return bytes.Clone(value), nil
}

// Expected getter tuples use independent Solidity signatures and ABI words.
func validatorEvidenceInstallRPCTest(t *testing.T, payloads *validatorEvidenceDeploymentPayloads) *validatorEvidenceInstallRPC {
	t.Helper()
	manifest := payloads.Manifest
	self := &validatorEvidenceInstallRPC{head: testEVMHead(100, 0xee), address: manifest.Address, code: bytes.Clone(payloads.Runtime), responses: map[validatorEvidenceInstallCall][]byte{}}
	for _, row := range []struct {
		address   common.Address
		signature string
		value     []byte
	}{
		{address: manifest.Address, signature: "coordinator()", value: abiWordAddress(manifest.Coordinator)},
		{address: manifest.Address, signature: "settlementVault()", value: abiWordAddress(manifest.SettlementVault)},
		{address: manifest.Address, signature: "chainId()", value: abiWordUint(manifest.ChainID)},
		{address: manifest.Address, signature: "netuid()", value: abiWordUint(uint64(manifest.Netuid))},
		{address: manifest.Address, signature: "genesisHash()", value: manifest.GenesisHash.Bytes()},
		{address: manifest.Address, signature: "deploymentIdHash()", value: manifest.DeploymentIDHash.Bytes()},
		{address: manifest.Coordinator, signature: "settlementVault()", value: abiWordAddress(manifest.SettlementVault)},
		{address: manifest.Coordinator, signature: "netuid()", value: abiWordUint(uint64(manifest.Netuid))},
		{address: manifest.Coordinator, signature: "validatorEvidence()", value: abiWordAddress(manifest.Address)},
	} {
		self.responses[validatorEvidenceInstallCall{address: row.address, data: hexutil.Encode(crypto.Keccak256([]byte(row.signature))[:4])}] = row.value
	}
	return self
}

// Cleanup ordering closes the client, joins HTTP handlers, then stops RPC.
func (self *validatorEvidenceInstallRPC) client(t *testing.T) *ethclient.Client {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("eth", self); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	client, err := ethclient.DialContext(context.Background(), httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestValidatorEvidenceInstallReadbackPinsCompleteDomainAndAnchor(t *testing.T) {
	_, _, deployment := validatorEvidenceInstallTest(t)
	payloads := deployment.ValidatorEvidence
	fixture := validatorEvidenceInstallRPCTest(t, payloads)
	got, err := readValidatorEvidenceDeploymentAtHead(context.Background(), fixture.client(t), payloads, fixture.head, true)
	if err != nil || got != payloads.Manifest.Address {
		t.Fatalf("approved journal readback: %s %v", got, err)
	}
	if fixture.calls.Load() != 10 {
		t.Fatalf("canonical code+getter census=%d, want10", fixture.calls.Load())
	}
}

func TestValidatorEvidenceInstallReadbackRejectsEveryMalformedOrForeignView(t *testing.T) {
	_, _, deployment := validatorEvidenceInstallTest(t)
	payloads := deployment.ValidatorEvidence
	baseline := validatorEvidenceInstallRPCTest(t, payloads)
	for key := range baseline.responses {
		for _, fault := range []string{"mutation", "padding-or-trailing", "rpc"} {
			fixture := validatorEvidenceInstallRPCTest(t, payloads)
			switch fault {
			case "mutation":
				fixture.responses[key][31] ^= 1
			case "padding-or-trailing":
				fixture.responses[key] = append(fixture.responses[key], 0)
			case "rpc":
				fixture.fail = key
			}
			got, err := readValidatorEvidenceDeploymentAtHead(context.Background(), fixture.client(t), payloads, fixture.head, true)
			if err == nil || got != (common.Address{}) {
				t.Fatalf("%v/%s accepted: %s %v", key, fault, got, err)
			}
		}
	}
}

func TestValidatorEvidenceInstallReadbackDistinguishesUnanchoredFromConflicting(t *testing.T) {
	_, _, deployment := validatorEvidenceInstallTest(t)
	payloads := deployment.ValidatorEvidence
	key := validatorEvidenceInstallCall{address: payloads.Manifest.Coordinator, data: hexutil.Encode(crypto.Keccak256([]byte("validatorEvidence()"))[:4])}
	for _, row := range []struct {
		anchor   common.Address
		required bool
		valid    bool
	}{
		{required: false, valid: true}, {required: true, valid: false}, {anchor: payloads.Manifest.Address, required: true, valid: true}, {anchor: common.HexToAddress("0x1111111111111111111111111111111111111111"), valid: false},
	} {
		fixture := validatorEvidenceInstallRPCTest(t, payloads)
		fixture.responses[key] = abiWordAddress(row.anchor)
		got, err := readValidatorEvidenceDeploymentAtHead(context.Background(), fixture.client(t), payloads, fixture.head, row.required)
		if row.valid {
			if err != nil || got != row.anchor {
				t.Fatalf("valid anchor state: %s %v", got, err)
			}
		} else if err == nil || got != (common.Address{}) {
			t.Fatal("foreign/required-missing anchor accepted")
		}
	}
}

func TestValidatorEvidenceInstallReadbackRejectsRuntimeBeforeViews(t *testing.T) {
	_, _, deployment := validatorEvidenceInstallTest(t)
	payloads := deployment.ValidatorEvidence
	for _, fault := range []string{"absent", "foreign", "oversized"} {
		fixture := validatorEvidenceInstallRPCTest(t, payloads)
		switch fault {
		case "absent":
			fixture.code = nil
		case "foreign":
			fixture.code[0] ^= 1
		case "oversized":
			fixture.code = make([]byte, 24*1024+1)
		}
		got, err := readValidatorEvidenceDeploymentAtHead(context.Background(), fixture.client(t), payloads, fixture.head, true)
		wantCalls := uint64(1)
		if fault == "absent" {
			wantCalls = finalSemanticRPCMaximumAttempts
		}
		if err == nil || got != (common.Address{}) || fixture.calls.Load() != wantCalls {
			t.Fatalf("%s runtime reached views or passed: %s %v calls%d", fault, got, err, fixture.calls.Load())
		}
	}
}

func TestValidatorEvidenceInstallReadbackRetriesTransientEmptyCode(t *testing.T) {
	_, _, deployment := validatorEvidenceInstallTest(t)
	payloads := deployment.ValidatorEvidence
	fixture := validatorEvidenceInstallRPCTest(t, payloads)
	var attempts atomic.Uint64
	fixture.codeHook = func(context.Context) error {
		if attempts.Add(1) == 1 {
			fixture.code = nil
		} else {
			fixture.code = bytes.Clone(payloads.Runtime)
		}
		return nil
	}
	got, err := readValidatorEvidenceDeploymentAtHead(context.Background(), fixture.client(t), payloads, fixture.head, true)
	if err != nil || got != payloads.Manifest.Address || attempts.Load() != 2 {
		t.Fatalf("transient empty code recovery: address=%s attempts=%d error=%v", got, attempts.Load(), err)
	}
}

func TestValidatorEvidenceInstallReadbackCancellationJoinsInflightRequest(t *testing.T) {
	_, _, deployment := validatorEvidenceInstallTest(t)
	payloads := deployment.ValidatorEvidence
	fixture := validatorEvidenceInstallRPCTest(t, payloads)
	entered, joined := make(chan struct{}), make(chan struct{})
	fixture.codeHook = func(ctx context.Context) error { close(entered); defer close(joined); <-ctx.Done(); return ctx.Err() }
	client := fixture.client(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		address common.Address
		err     error
	}
	done := make(chan result, 1)
	go func() {
		address, err := readValidatorEvidenceDeploymentAtHead(ctx, client, payloads, fixture.head, true)
		done <- result{address: address, err: err}
	}()
	<-entered
	cancel()
	actual := <-done
	<-joined
	if !errors.Is(actual.err, context.Canceled) || actual.address != (common.Address{}) || fixture.calls.Load() != 1 {
		t.Fatalf("cancellation accepted state or reached views: %+v", actual)
	}
}

func TestValidatorEvidenceInstallReadbackPreCancellationSendsNoRPC(t *testing.T) {
	_, _, deployment := validatorEvidenceInstallTest(t)
	payloads := deployment.ValidatorEvidence
	fixture := validatorEvidenceInstallRPCTest(t, payloads)
	client := fixture.client(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := readValidatorEvidenceDeploymentAtHead(ctx, client, payloads, fixture.head, true)
	if !errors.Is(err, context.Canceled) || got != (common.Address{}) || fixture.calls.Load() != 0 {
		t.Fatalf("pre-cancellation reached RPC or accepted state: %s %v", got, err)
	}
}
