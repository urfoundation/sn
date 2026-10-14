// transport_contracts_test.go pins the trail tunnel clients' contract policy:
// one small fixed contract size, server replies on regular contracts, and
// contracts closed when a hop's packet path has joined.
package validator

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// Every contract of a send sequence, including its prefetched successor, uses
// the one small size, and it still fits connect's largest transport message.
func TestTunnelClientSettingsUseSmallFixedContracts(t *testing.T) {
	settings := newTunnelClientSettings()
	contracts := settings.ContractManagerSettings
	for name, value := range map[string]connect.ByteCount{
		"initial":              contracts.InitialContractTransferByteCount,
		"initial network peer": contracts.InitialNetworkPeerContractTransferByteCount,
		"standard":             contracts.StandardContractTransferByteCount,
	} {
		if value != tunnelContractTransferByteCount {
			t.Errorf("%s contract transfer byte count = %d, want %d", name, value, tunnelContractTransferByteCount)
		}
	}
	effective := connect.ByteCount(float32(tunnelContractTransferByteCount) * settings.SendBufferSettings.ContractFillFraction)
	if largest := settings.MinimumMessageLenLimit() + settings.SendBufferSettings.MinMessageByteCount; effective <= largest {
		t.Fatalf("effective contract %d bytes cannot carry a %d byte message", effective, largest)
	}
	if settings.EncryptionSettings.EncryptionControlUseCompanion {
		t.Fatal("tunnel server replies ride companion contracts, which public providers refuse")
	}
}

// Contract settings are per attempt like the encryption settings.
func TestTunnelClientContractSettingsAreIndependent(t *testing.T) {
	first, second := newTunnelClientSettings(), newTunnelClientSettings()
	if first.ContractManagerSettings == second.ContractManagerSettings {
		t.Fatal("tunnel contract settings are shared between attempts")
	}
	first.ContractManagerSettings.StandardContractTransferByteCount = 1
	first.EncryptionSettings.EncryptionControlUseCompanion = true
	if second.ContractManagerSettings.StandardContractTransferByteCount != tunnelContractTransferByteCount || second.EncryptionSettings.EncryptionControlUseCompanion {
		t.Fatal("mutating one attempt's settings changed another's")
	}
}

// Contracts close after the packet path and pump have joined and before the
// identity's client is retired.
func TestTunnelAttemptClosesContractsAfterPacketPathJoins(t *testing.T) {
	events := []string{}
	pumpDone := make(chan struct{})
	attempt := &tunnelAttempt{
		cancel: func() { events = append(events, "cancel") },
		tun: tunnelAttemptTunCloseFunc(func() error {
			events = append(events, "tun")
			return nil
		}),
		packetClient: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			events = append(events, "packet-client")
			close(pumpDone)
			return nil
		}),
		pumpDone:       pumpDone,
		closeContracts: func() { events = append(events, "close-contracts") },
		retireClient: func(context.Context) error {
			events = append(events, "retire-client")
			return nil
		},
		generator: tunnelAttemptCloseAndWaitFunc(func(context.Context) error {
			events = append(events, "generator")
			return nil
		}),
	}
	if err := attempt.close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"cancel", "tun", "packet-client", "close-contracts", "retire-client", "generator"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("close order = %v, want %v", events, want)
	}
}

// A packet path that did not join may still be sending; its identity is
// retired instead, so its contracts are not flushed from under it.
func TestTunnelAttemptSkipsContractCloseWhenPumpDoesNotJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	flushed, retired := false, false
	attempt := &tunnelAttempt{
		pumpDone:       make(chan struct{}),
		closeContracts: func() { flushed = true },
		retireClient: func(context.Context) error {
			retired = true
			return nil
		},
	}
	if err := attempt.closeAndWait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("close error = %v, want pump join cancellation", err)
	}
	if flushed {
		t.Fatal("contracts were flushed from an unjoined packet path")
	}
	if !retired {
		t.Fatal("unjoined packet path did not retire its client")
	}
}

// Grants every CreateContract it receives with the peer's network provide key
// and records each request. Close reports need no answer.
type tunnelContractAuthority struct {
	sourceId connect.Id
	peer     func() *connect.Client
	settings *connect.ContractManagerSettings

	stateLock sync.Mutex
	requests  []*protocol.CreateContract
}

func (self *tunnelContractAuthority) SendControl(frames []*protocol.Frame, callback connect.OobResultFunction) {
	self.SendControlWithCtx(context.Background(), frames, callback)
}

func (self *tunnelContractAuthority) SendControlWithCtx(_ context.Context, frames []*protocol.Frame, callback connect.OobResultFunction) {
	var responses []*protocol.Frame
	for _, frame := range frames {
		message, err := connect.FromFrame(frame)
		connect.MessagePoolReturn(frame.MessageBytes)
		if err != nil {
			panic(err)
		}
		if request, ok := message.(*protocol.CreateContract); ok {
			responses = append(responses, self.grant(request))
		}
	}
	if callback != nil {
		callback(responses, nil)
	}
}

func (self *tunnelContractAuthority) grant(request *protocol.CreateContract) *protocol.Frame {
	peer := self.peer()
	secret, ok := peer.ContractManager().GetProvideSecretKey(protocol.ProvideMode_Network)
	if !ok {
		panic("tunnel contract peer has no network provide key")
	}
	contractId := connect.NewId()
	expiration := time.Now().Add(time.Hour).UnixMilli()
	storedBytes, err := proto.Marshal(&protocol.StoredContract{
		ContractId:                 contractId.Bytes(),
		TransferByteCount:          request.TransferByteCount,
		SourceId:                   self.sourceId.Bytes(),
		DestinationId:              peer.ClientId().Bytes(),
		DestinationClientPublicKey: peer.ClientKeyManager().PublicKey(),
		ExpirationTimeUnixMilli:    &expiration,
	})
	if err != nil {
		panic(err)
	}
	responseBytes, err := proto.Marshal(&protocol.CreateContractResult{Contract: &protocol.Contract{
		StoredContractBytes: storedBytes,
		StoredContractHmac:  connect.SignStoredContract(self.settings, secret, storedBytes),
		ProvideMode:         protocol.ProvideMode_Network,
	}})
	if err != nil {
		panic(err)
	}
	self.stateLock.Lock()
	self.requests = append(self.requests, proto.Clone(request).(*protocol.CreateContract))
	self.stateLock.Unlock()
	return &protocol.Frame{MessageType: protocol.MessageType_TransferCreateContractResult, MessageBytes: responseBytes}
}

func (self *tunnelContractAuthority) snapshot() []*protocol.CreateContract {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]*protocol.CreateContract(nil), self.requests...)
}

// A real sender with the tunnel settings requests only small regular
// contracts, keeps its contract open after the ack, and closes it on the
// client flush the attempt runs at hop end, long before connect's idle timeout.
func TestTunnelClientContractsAreSmallAndCloseOnFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	senderSettings := newTunnelClientSettings()
	// The in-process peer has no platform TLS or key registration service.
	senderSettings.EncryptionSettings.Mode = connect.EncryptionModeOff
	senderSettings.ClientKeyRegistrationRequired = false
	senderSettings.Log = connect.NewNoopLogger()
	senderSettings.ControlPingTimeout = 0
	receiverSettings := connect.DefaultClientSettings()
	receiverSettings.EncryptionSettings.Mode = connect.EncryptionModeOff
	receiverSettings.Log = connect.NewNoopLogger()
	receiverSettings.ControlPingTimeout = 0

	senderId, receiverId := connect.NewId(), connect.NewId()
	var sender, receiver *connect.Client
	authority := &tunnelContractAuthority{sourceId: senderId, peer: func() *connect.Client { return receiver }, settings: receiverSettings.ContractManagerSettings}
	reverseAuthority := &tunnelContractAuthority{sourceId: receiverId, peer: func() *connect.Client { return sender }, settings: senderSettings.ContractManagerSettings}
	sender = connect.NewClient(ctx, senderId, authority, senderSettings)
	receiver = connect.NewClient(ctx, receiverId, reverseAuthority, receiverSettings)
	forward, reverse := make(connect.Route, 64), make(connect.Route, 64)
	t.Cleanup(func() {
		cancel()
		joinCtx, joinCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer joinCancel()
		for _, client := range []*connect.Client{sender, receiver} {
			if err := client.CloseAndWait(joinCtx); err != nil {
				t.Errorf("close tunnel contract client: %v", err)
			}
		}
		for _, route := range []connect.Route{forward, reverse} {
			for len(route) != 0 {
				connect.MessagePoolReturn(<-route)
			}
		}
	})
	for _, client := range []*connect.Client{sender, receiver} {
		client.ContractManager().SetProvideModes(map[protocol.ProvideMode]bool{protocol.ProvideMode_Network: true})
	}
	sender.RouteManager().UpdateTransport(connect.NewSendClientTransport(connect.DestinationId(receiverId)), []connect.Route{forward})
	receiver.RouteManager().UpdateTransport(connect.NewReceiveGatewayTransport(), []connect.Route{forward})
	receiver.RouteManager().UpdateTransport(connect.NewSendClientTransport(connect.DestinationId(senderId)), []connect.Route{reverse})
	sender.RouteManager().UpdateTransport(connect.NewReceiveGatewayTransport(), []connect.Route{reverse})

	acked := make(chan error, 1)
	frame := connect.RequireToFrameWithDefaultProtocolVersion(&protocol.SimpleMessage{Content: "verify"})
	if !sender.SendWithTimeout(frame, receiverId, func(err error) { acked <- err }, 10*time.Second) {
		connect.MessagePoolReturn(frame.MessageBytes)
		t.Fatal("sender did not admit the message")
	}
	select {
	case err := <-acked:
		if err != nil {
			t.Fatalf("message ack: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("message was not acknowledged")
	}

	if len(sender.ContractManager().LocalStats().ContractOpenByteCounts) == 0 {
		t.Fatal("no open contract after the ack; the flush below would prove nothing")
	}
	sender.Flush()
	deadline := time.Now().Add(10 * time.Second)
	for {
		stats := sender.ContractManager().LocalStats()
		if len(stats.ContractOpenByteCounts) == 0 && 0 < stats.ContractCloseCount {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("flush left %d contracts open (closed %d)", len(stats.ContractOpenByteCounts), stats.ContractCloseCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	requests := authority.snapshot()
	if len(requests) == 0 {
		t.Fatal("sender requested no contract")
	}
	for index, request := range requests {
		if connect.ByteCount(request.TransferByteCount) != tunnelContractTransferByteCount {
			t.Errorf("contract request %d asks for %d bytes, want %d", index, request.TransferByteCount, tunnelContractTransferByteCount)
		}
		if request.Companion {
			t.Errorf("contract request %d is a companion contract", index)
		}
	}
}
