// Generated provider role inputs reach actual initial and rotated key frames,
// using the same complete namespace as the separately tested signed close path.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// Two independently configured launch outputs must reach their own original
// key-enrollment namespace without a manual re-binding or a new readiness gate.
func TestBootstrapProviderRoleFilesReachActualEnrollmentAndRotation(t *testing.T) {
	f, _, request, output := newBootstrapProviderRoleFixture(t)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, request, output), &stdout, &stderr); code != 0 {
		t.Fatal("provider enrollment role generation failed", code, stderr.String())
	}
	var result bootstrapProviderRoleConfig
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, launch := range result.ProviderDeclarations {
		func() {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			domain, err := miner.ReadProviderCloseReportDomain(launch.DomainFile.Path)
			want, digestErr := launch.EnrollmentDomain.Digest()
			if err != nil || digestErr != nil || domain != want {
				t.Fatal("generated file did not supply original provider enrollment", err, digestErr)
			}
			settings := miner.ProviderDeviceSettings(domain).ClientSettings
			settings.ControlPingTimeout = 0
			settings.EncryptionSettings.Mode = connect.EncryptionModeOff
			settings.Log = connect.NewNoopLogger()
			if settings.ClientKeyRegistrationRequired {
				t.Fatal("optional provider namespace introduced a processed-registration startup gate")
			}
			provider := connect.NewClient(ctx, connect.NewId(), connect.NewNoContractClientOob(), &settings)
			receiverSettings := connect.DefaultClientSettings()
			receiverSettings.ControlPingTimeout = 0
			receiverSettings.EncryptionSettings.Mode = connect.EncryptionModeOff
			receiverSettings.Log = connect.NewNoopLogger()
			receiver := connect.NewClient(ctx, connect.ControlId, connect.NewNoContractClientOob(), receiverSettings)
			defer func() {
				cancel()
				join, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				for _, client := range []*connect.Client{provider, receiver} {
					if err := client.CloseAndWait(join); err != nil {
						t.Error(err)
					}
				}
			}()
			provider.ContractManager().AddNoContractPeer(connect.ControlId)
			receiver.ContractManager().AddNoContractPeer(provider.ClientId())
			observed := make(chan *protocol.ClientKey, 16)
			receiver.AddReceiveCallback(func(_ connect.TransferPath, frames []*protocol.Frame, _ connect.Peer) {
				for _, frame := range frames {
					message, err := connect.FromFrame(frame)
					if key, ok := message.(*protocol.ClientKey); err == nil && ok {
						select {
						case observed <- proto.Clone(key).(*protocol.ClientKey):
						default:
						}
					}
				}
			})
			forward, reverse := make(chan []byte), make(chan []byte)
			provider.RouteManager().UpdateTransport(connect.NewSendGatewayTransport(), []connect.Route{forward})
			receiver.RouteManager().UpdateTransport(connect.NewReceiveGatewayTransport(), []connect.Route{forward})
			receiver.RouteManager().UpdateTransport(connect.NewSendGatewayTransport(), []connect.Route{reverse})
			provider.RouteManager().UpdateTransport(connect.NewReceiveGatewayTransport(), []connect.Route{reverse})
			next := func(expected []byte) *protocol.ClientKey {
				t.Helper()
				for {
					select {
					case key := <-observed:
						if bytes.Equal(key.PublicKey, expected) {
							return key
						}
					case <-ctx.Done():
						t.Fatal("generated provider role did not publish its actual key frame", ctx.Err())
						return nil
					}
				}
			}
			first := next(provider.ClientKeyManager().PublicKey())
			if !bytes.Equal(first.HistoryDomainHash, want[:]) {
				t.Fatal("generated provider domain was merely metadata and did not reach enrollment")
			}
			settings.ContractManagerSettings.CloseReportDomainHash[0]++
			if err := provider.ClientKeyManager().SetSeed(bytes.Repeat([]byte{63}, ed25519.SeedSize)); err != nil {
				t.Fatal(err)
			}
			rotated := next(provider.ClientKeyManager().PublicKey())
			if !bytes.Equal(rotated.HistoryDomainHash, want[:]) || bytes.Equal(first.PublicKey, rotated.PublicKey) {
				t.Fatal("provider rotation left its original generated enrollment domain")
			}
		}()
	}
}
