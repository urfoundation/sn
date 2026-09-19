package main

// This is the source309 production protocol path, with current manifest
// identities and bounded, joined ownership for each durable tranche. The
// payload is real peer traffic; fixture credit is not commercial revenue.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urnetwork/connect"
	"github.com/urnetwork/connect/protocol"
)

type paidTrafficIdentity struct {
	ClientId       string            `json:"client_id"`
	NetworkId      string            `json:"network_id"`
	Jwt            string            `json:"jwt"`
	Active         bool              `json:"active"`
	RemainingBytes int64             `json:"remaining_bytes"`
	Providers      map[string]string `json:"providers"`
}

func validatePaidTrafficIdentity(manifest PaidTrafficManifest, identity paidTrafficIdentity, requiredBytes uint64) error {
	if !identity.Active || identity.ClientId != manifest.ClientId || identity.NetworkId != manifest.NetworkId || identity.Jwt == "" || identity.RemainingBytes < 0 || uint64(identity.RemainingBytes) < requiredBytes {
		return errors.New("paid traffic retained consumer identity or existing credit is unavailable")
	}
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(identity.Jwt, claims); err != nil || claims["client_id"] != manifest.ClientId || claims["network_id"] != manifest.NetworkId {
		return errors.New("paid traffic credential differs from its retained identity")
	}
	for _, provider := range manifest.Providers {
		if identity.Providers[provider.ClientId] != provider.NetworkId || provider.NetworkId == identity.NetworkId {
			return errors.New("paid traffic provider is inactive or no longer outside the consumer network")
		}
	}
	return nil
}

// Read the existing server-managed consumer only. No account creation, grants,
// provider mutation, usage insertion, or chain transaction is available here.
func readPaidTrafficIdentity(ctx context.Context, manifest PaidTrafficManifest, requiredBytes uint64) (paidTrafficIdentity, error) {
	providerIds := make([]string, len(manifest.Providers))
	for index, provider := range manifest.Providers {
		// Manifest validation has required canonical UUIDs before SQL creation.
		providerIds[index] = "'" + provider.ClientId + "'::uuid"
	}
	query := `BEGIN READ ONLY;
SET LOCAL statement_timeout='5000ms';
SELECT json_build_object(
 'client_id',p.client_id,'network_id',p.network_id,'jwt',p.by_client_jwt,'active',c.active,
 'remaining_bytes',COALESCE((SELECT SUM(b.balance_byte_count) FROM transfer_balance b
 WHERE b.network_id=p.network_id AND b.active AND b.start_time<=NOW() AND NOW()<b.end_time),0),
 'providers',(SELECT COALESCE(json_object_agg(d.client_id,d.network_id),'{}'::json)
 FROM network_client d WHERE d.active AND d.client_id IN (` + strings.Join(providerIds, ",") + `)))
FROM prober_identity p JOIN network_client c ON c.client_id=p.client_id AND c.network_id=p.network_id
WHERE p.singleton;
COMMIT;
`
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(readCtx, "docker", "exec", "-i", "--user", "postgres", manifest.DatabaseContainer,
		"psql", "-X", "-q", "-A", "-t", "-U", "postgres", "-d", "bringyour", "-v", "ON_ERROR_STOP=1")
	command.Stdin = strings.NewReader(query)
	data, err := command.Output()
	if err != nil {
		return paidTrafficIdentity{}, errors.New("paid traffic read-only identity lookup failed")
	}
	defer clear(data)
	var identity paidTrafficIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return paidTrafficIdentity{}, errors.New("paid traffic identity lookup returned invalid data")
	}
	if err := validatePaidTrafficIdentity(manifest, identity, requiredBytes); err != nil {
		return paidTrafficIdentity{}, err
	}
	return identity, nil
}

func readPaidTrafficSeed(manifest PaidTrafficManifest) ([]byte, error) {
	file, err := os.OpenFile(manifest.ClientKeySeedFile, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("open existing paid traffic identity seed failed")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != ed25519.SeedSize {
		return nil, errors.New("paid traffic seed must be a private regular 32-byte file")
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(file, seed); err != nil {
		clear(seed)
		return nil, errors.New("read paid traffic seed failed")
	}
	key := ed25519.NewKeyFromSeed(seed)
	defer clear(key)
	if hex.EncodeToString(key[ed25519.SeedSize:]) != manifest.ClientPublicKeyHex {
		clear(seed)
		return nil, errors.New("paid traffic seed differs from its approved public identity")
	}
	return seed, nil
}

func sendPaidTrafficProtocolBatch(ctx context.Context, manifest PaidTrafficManifest, allocations []PaidTrafficAllocation) (result PaidTrafficBatchResult, returnErr error) {
	// Errors before construction have no outstanding client ownership.
	result.CleanupJoined = true
	var required uint64
	for _, allocation := range allocations {
		required += allocation.PayloadBytes
	}
	identity, err := readPaidTrafficIdentity(ctx, manifest, required)
	if err != nil {
		return result, err
	}
	seed, err := readPaidTrafficSeed(manifest)
	if err != nil {
		return result, err
	}
	defer func() {
		// A timed-out join can still have client-owned users of the settings.
		// The next owner waits for this process generation to exit in that case.
		if result.CleanupJoined {
			clear(seed)
		}
	}()
	clientId, _ := connect.ParseId(manifest.ClientId)
	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	apiCtx, cancelApi := context.WithCancel(context.Background())
	defer cancelApi()
	strategySettings := connect.DefaultClientStrategySettings()
	strategySettings.Log = connect.NewNoopLogger()
	strategy := connect.NewClientStrategy(apiCtx, strategySettings)
	api := connect.NewBringYourApi(apiCtx, strategy, manifest.ApiUrl)
	api.SetByJwt(identity.Jwt)
	oob := connect.NewApiOutOfBandControlWithApi(api)
	settings := connect.DefaultClientSettings()
	settings.Log = connect.NewNoopLogger()
	settings.EncryptionSettings.Mode = connect.EncryptionModeOpportunistic
	settings.ClientKeyRegistrationRequired = true
	settings.ClientKeySeed = seed
	settings.SendBufferSettings.SelectiveAckTimeout = 2 * time.Second
	settings.SendBufferSettings.ResendQueueMaxByteCount = 1024 * 1024
	settings.SendBufferSettings.ResendQueueMinByteCount = 0
	settings.SendBufferSettings.ResendQueueBudget = connect.NewTransferMemoryBudget(8 * 1024 * 1024)
	client := connect.NewClient(workCtx, clientId, oob, settings)
	auth := &connect.ClientAuth{ByJwt: identity.Jwt, InstanceId: connect.NewId(), AppVersion: "sim-testnet-bounded-traffic-v1"}
	var transports []*connect.PlatformTransport
	for range 4 {
		transportSettings := connect.DefaultPlatformTransportSettings()
		transportSettings.Log = connect.NewNoopLogger()
		transportSettings.FramerSettings = connect.DefaultFramerSettings(8192)
		transportSettings.H1MaxMessageByteCount = 8192
		transports = append(transports, connect.NewPlatformTransport(workCtx, strategy, client.RouteManager(), manifest.ConnectUrl, auth, transportSettings))
	}
	acknowledged := make([]atomic.Uint64, len(allocations))
	defer func() {
		client.CloseContractStats()
		cancelWork()
		closeCtx, cancelClose := context.WithTimeout(context.Background(), time.Minute)
		defer cancelClose()
		closeErr := client.CloseAndWait(closeCtx)
		for _, transport := range transports {
			closeErr = errors.Join(closeErr, transport.CloseAndWait(closeCtx))
		}
		closeErr = errors.Join(closeErr, oob.CloseAndWait(closeCtx))
		api.Close()
		strategy.Close()
		result.CleanupJoined = closeErr == nil
		for index, allocation := range allocations {
			result.Acknowledged = append(result.Acknowledged, PaidTrafficAllocation{ClientId: allocation.ClientId, PayloadBytes: acknowledged[index].Load()})
		}
		returnErr = errors.Join(returnErr, closeErr)
	}()
	if err := client.ClientKeyManager().WaitForRegistration(workCtx); err != nil {
		return result, fmt.Errorf("paid traffic client registration: %w", err)
	}
	provideAck := make(chan error, 1)
	client.ContractManager().SetProvideModesWithReturnTrafficWithOobAckCallback(map[protocol.ProvideMode]bool{protocol.ProvideMode_Network: true}, func(err error) {
		select {
		case provideAck <- err:
		default:
		}
	})
	select {
	case err := <-provideAck:
		if err != nil {
			return result, err
		}
	case <-workCtx.Done():
		return result, workCtx.Err()
	}
	var senders sync.WaitGroup
	var pending atomic.Int64
	drained := make(chan struct{}, 1)
	failures := make(chan error, len(allocations))
	content := strings.Repeat("bounded testnet peer payload ", 160)[:4096]
	for index, allocation := range allocations {
		senders.Add(1)
		go func() {
			defer senders.Done()
			providerCtx, cancelProvider := context.WithCancel(workCtx)
			defer cancelProvider()
			providerId, _ := connect.ParseId(allocation.ClientId)
			slots := make(chan struct{}, max(1, manifest.InflightMessages/len(allocations)))
			var firstFailure sync.Once
			fail := func(err error) {
				firstFailure.Do(func() {
					failures <- fmt.Errorf("provider %s: %w", allocation.ClientId, err)
					cancelProvider()
				})
			}
			for sent := uint64(0); sent < allocation.PayloadBytes; {
				select {
				case slots <- struct{}{}:
				case <-providerCtx.Done():
					return
				}
				if providerCtx.Err() != nil {
					<-slots
					return
				}
				size := min(uint64(len(content)), allocation.PayloadBytes-sent)
				frame, err := connect.ToFrame(&protocol.SimpleMessage{Content: content[:int(size)]}, connect.DefaultProtocolVersion)
				if err != nil {
					<-slots
					fail(err)
					return
				}
				pending.Add(1)
				sent += size
				var once sync.Once
				finish := func(err error, fromAck bool) {
					once.Do(func() {
						if fromAck && err == nil {
							acknowledged[index].Add(size)
						}
						if pending.Add(-1) == 0 {
							select {
							case drained <- struct{}{}:
							default:
							}
						}
						<-slots
						if err != nil {
							fail(err)
						}
					})
				}
				ok, sendErr := client.SendWithTimeoutDetailed(frame, providerId, func(err error) { finish(err, true) }, time.Second)
				if !ok || sendErr != nil {
					// A rejected enqueue retains caller ownership of this frame.
					connect.MessagePoolReturn(frame.MessageBytes)
					if sendErr == nil {
						sendErr = errors.New("paid traffic enqueue timed out")
					}
					finish(sendErr, false)
					return
				}
			}
		}()
	}
	senders.Wait()
	for pending.Load() > 0 {
		select {
		case <-drained:
		case <-workCtx.Done():
			return result, workCtx.Err()
		}
	}
	for {
		select {
		case err := <-failures:
			returnErr = errors.Join(returnErr, err)
		default:
			return result, errors.Join(returnErr, workCtx.Err())
		}
	}
}
