// Each exclusive lease retains processed client registration across hop calls.
// Request packet paths are private; shutdown owns the reusable identities and
// waits until their last request, control operation and transport have joined.
package validator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// The packet exchange and registration lifetime are separate ownership units.
// A request's tunnel tries its family's first IP family (4 or 6) first, and
// only that one when the family is pinned.
type tunnelTransportClient interface {
	postVerify(ctx context.Context, hop connect.Id, body []byte, family hopIpFamily) ([]byte, error)
	done() <-chan struct{}
	closeAndWait(context.Context) error
}

const tunnelClientRegistrationTimeout = 2 * time.Minute

// The request lease owns this pointer; the producer publishes its result once
// by closing done. A timed-out waiter does not cancel or replace that producer.
type tunnelClientRegistration struct {
	done   chan struct{}
	client tunnelTransportClient
	err    error
}

// One retained identity and its pending registration. Exactly one request
// owns a lease at a time; a lease's client and registration never move to
// another lease.
type tunnelClientLease struct {
	client       tunnelTransportClient
	registration *tunnelClientRegistration
}

// Retains exactly one generated client and its private generator. The
// generator's initial destination is identity bookkeeping, not packet routing.
type registeredTunnelClient struct {
	owner     *TunnelTransport
	client    *connect.Client
	generator *connect.ApiMultiClientGenerator
	cleanup   *tunnelAttempt
	ctx       context.Context
	// Kept until the request's real packet workers join, including a failed
	// bounded close. The exclusive lease bounds this to one packet path.
	packetCleanup *tunnelAttempt
	// When the identity's attempts close their contracts; the lease
	// serializes it.
	contracts tunnelHopContracts
}

// Safe for concurrent callers: waiting for a free lease consumes each
// request's original deadline. Application rejection does not retire a client.
func (self *TunnelTransport) PostVerify(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("tunnel request context is absent")
	}
	if hop == (connect.Id{}) || hop == self.cfg.SourceClientId {
		return nil, errors.New("tunnel destination is absent or is the validator itself")
	}
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(self.ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	var lease *tunnelClientLease
	select {
	case <-callCtx.Done():
		return nil, tunnelVerifyFailure(trailStageLease, callCtx.Err())
	case <-self.ctx.Done():
		return nil, self.ctx.Err()
	case lease = <-self.clientLeases:
	}
	defer func() { self.clientLeases <- lease }()
	if err := errors.Join(callCtx.Err(), self.ctx.Err()); err != nil {
		return nil, err
	}
	if lease.client != nil {
		select {
		case <-lease.client.done():
			if err := self.retireClient(lease); err != nil {
				return nil, err
			}
		default:
		}
	}
	if lease.client == nil {
		if err := self.waitForClientRegistration(callCtx, lease, hop); err != nil {
			return nil, tunnelVerifyFailure(trailStageRegistration, err)
		}
	}
	observation := newTunnelVerifyObservation(lease.client, hop)
	// the trail engine alternates the family each hop tries first, and pins
	// one after a source-egress-unresolved answer
	response, err := lease.client.postVerify(observation.context(callCtx), hop, body, hopIpFamilyOf(ctx))
	observation.close()
	err = observation.failure(err)
	select {
	case <-lease.client.done():
		err = errors.Join(err, self.retireClient(lease))
	default:
	}
	return response, err
}

// Called only with the request lease. Preparation keeps its own finite budget,
// while every waiter and the eventual packet exchange retain the step deadline.
func (self *TunnelTransport) waitForClientRegistration(ctx context.Context, lease *tunnelClientLease, firstHop connect.Id) error {
	if lease.registration == nil {
		load := &tunnelClientRegistration{done: make(chan struct{})}
		lease.registration = load
		newClient := self.newClient
		go func() {
			defer close(load.done)
			setupCtx, cancel := context.WithTimeout(self.ctx, tunnelClientRegistrationTimeout)
			load.client, load.err = newClient(setupCtx, firstHop)
			load.err = errors.Join(load.err, setupCtx.Err(), self.ctx.Err())
			cancel()
			if load.client == nil && load.err == nil {
				load.err = errors.New("tunnel registration returned no client")
			}
			if load.err != nil && load.client != nil {
				closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
				closeErr := load.client.closeAndWait(closeCtx)
				closeCancel()
				load.err = errors.Join(load.err, closeErr)
				if closeErr != nil {
					// The shutdown owner retains this exact client for a final join.
					self.cancel()
				} else {
					load.client = nil
				}
			}
		}()
	}
	load := lease.registration
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-self.ctx.Done():
		return self.ctx.Err()
	case <-load.done:
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	lease.registration = nil
	lease.client = load.client
	if load.err != nil {
		return errors.Join(load.err, self.retireClient(lease))
	}
	select {
	case <-lease.client.done():
		return errors.Join(errors.New("registered tunnel client closed during setup"), self.retireClient(lease))
	default:
		return nil
	}
}

// Called only with the lease. Failed retirement closes admission and leaves
// the exact client with the parent worker for a final joined shutdown.
func (self *TunnelTransport) retireClient(lease *tunnelClientLease) error {
	if lease.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := lease.client.closeAndWait(ctx); err != nil {
		self.cancel()
		return fmt.Errorf("retire registered tunnel client: %w", err)
	}
	lease.client = nil
	return nil
}

// Constructor-owned shutdown runs outside the client callback tree. It first
// joins every exclusive request lease and any initial registration producer,
// then closes each retained identity.
func (self *TunnelTransport) run() {
	defer close(self.closed)
	<-self.ctx.Done()
	var closeErrors []error
	for range self.clientCount {
		lease := <-self.clientLeases
		if lease.registration != nil {
			<-lease.registration.done
			lease.client = lease.registration.client
			lease.registration = nil
		}
		if lease.client != nil {
			closeErrors = append(closeErrors, lease.client.closeAndWait(context.Background()))
			lease.client = nil
		}
	}
	self.closeErr = errors.Join(closeErrors...)
}

// Stops admission and active requests without joining inside callbacks.
func (self *TunnelTransport) Close() {
	self.cancel()
}

// Safe for concurrent waiters; a timed-out waiter does not abandon the parent
// cleanup worker, and a later waiter can still join its actual completion.
func (self *TunnelTransport) CloseAndWait(ctx context.Context) error {
	self.Close()
	if ctx == nil {
		return errors.New("tunnel shutdown context is absent")
	}
	select {
	case <-self.closed:
		return self.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Registration uses the transport's setup budget, while the completed client
// is owned by the operator. Every partial identity has the same cleanup.
func (self *TunnelTransport) newRegisteredClient(ctx context.Context, firstHop connect.Id) (tunnelTransportClient, error) {
	jwt, err := self.currentByClientJwt()
	if err != nil {
		return nil, err
	}
	clientCtx, cancel := context.WithCancel(self.ctx)
	registered := &registeredTunnelClient{owner: self, cleanup: &tunnelAttempt{cancel: cancel}, ctx: clientCtx}
	settings := connect.DefaultApiMultiClientGeneratorSettings()
	settings.PlatformTransportCreated = func(_ *connect.Client, transport *connect.PlatformTransport) {
		// This private generator never changes transport policy or adds clients.
		registered.cleanup.addTransport(transport)
	}
	registered.generator = connect.NewApiMultiClientGenerator(
		context.WithoutCancel(self.ctx), nil, self.clientStrategy,
		[]connect.Id{self.cfg.SourceClientId}, self.cfg.ApiUrl, jwt, self.cfg.ConnectUrl,
		"validator", "validator", RequireVersion(), &self.cfg.SourceClientId,
		self.closeReportClientSettings, settings,
	)
	registered.cleanup.generator = registered.generator
	destination, err := connect.NewMultiHopId(firstHop)
	if err != nil {
		return registered, fmt.Errorf("tunnel destination: %w", err)
	}
	args, err := registered.generator.NewClientArgsForDestinationContext(ctx, destination)
	if err != nil {
		return registered, fmt.Errorf("tunnel identity: %w", err)
	}
	registered.cleanup.removeIdentity = func(joinCtx context.Context) error {
		jwt, err := self.currentByClientJwt()
		if err != nil {
			return err
		}
		result, err := connect.HttpPostWithStrategy(joinCtx, self.clientStrategy,
			self.cfg.ApiUrl+"/network/remove-client", &connect.RemoveNetworkClientArgs{ClientId: args.ClientId}, jwt,
			&connect.RemoveNetworkClientResult{}, connect.NewNoopApiCallback[*connect.RemoveNetworkClientResult]())
		if err != nil {
			return err
		}
		if result == nil {
			return errors.New("client removal returned no result")
		}
		if result.Error != nil && result.Error.Message != "Client does not exist." {
			return errors.New(result.Error.Message)
		}
		return nil
	}
	client, err := registered.generator.NewClientContext(clientCtx, ctx, args, self.closeReportClientSettings())
	if err != nil {
		return registered, fmt.Errorf("tunnel client setup: %w", err)
	}
	registered.client = client
	registered.contracts.flush = client.Flush
	var retireOnce sync.Once
	registered.cleanup.retireClient = func(joinCtx context.Context) error {
		retireOnce.Do(func() { registered.generator.RemoveClientWithArgs(client, args) })
		client.Cancel()
		joinErr := client.CloseAndWait(joinCtx)
		if control, ok := client.ClientOob().(interface{ CloseAndWait(context.Context) error }); ok {
			joinErr = errors.Join(joinErr, control.CloseAndWait(joinCtx))
		}
		return joinErr
	}
	return registered, ctx.Err()
}

// A canceled or failed underlying client cannot be admitted by a later lease.
func (self *registeredTunnelClient) done() <-chan struct{} {
	if self.client != nil {
		return self.client.Done()
	}
	return self.ctx.Done()
}

// Refreshes the parent credential before final contract/identity removal. No
// request API, response or provider authority is reused by this owner.
func (self *registeredTunnelClient) closeAndWait(ctx context.Context) error {
	var packetErr error
	if self.packetCleanup != nil {
		packetErr = self.packetCleanup.closeAndWait(ctx)
		if packetErr == nil {
			self.packetCleanup = nil
		}
	}
	if jwt, err := self.owner.currentByClientJwt(); err == nil {
		self.generator.SetByJwt(jwt)
	}
	return errors.Join(packetErr, self.cleanup.closeAndWait(ctx))
}
