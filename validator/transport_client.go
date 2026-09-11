// One exclusive lease retains processed client registration across hop calls.
// Request packet paths are private; shutdown owns the reusable identity and
// waits until its last request, control operation and transport have joined.
package validator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/urnetwork/connect"
)

// The packet exchange and registration lifetime are separate ownership units.
type tunnelTransportClient interface {
	postVerify(context.Context, connect.Id, []byte) ([]byte, error)
	done() <-chan struct{}
	closeAndWait(context.Context) error
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
}

// Safe for concurrent callers: waiting for the single lease consumes each
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
	select {
	case <-callCtx.Done():
		return nil, callCtx.Err()
	case <-self.ctx.Done():
		return nil, self.ctx.Err()
	case <-self.clientLease:
	}
	defer func() { self.clientLease <- struct{}{} }()
	if err := errors.Join(callCtx.Err(), self.ctx.Err()); err != nil {
		return nil, err
	}
	if self.client != nil {
		select {
		case <-self.client.done():
			if err := self.retireClient(); err != nil {
				return nil, err
			}
		default:
		}
	}
	if self.client == nil {
		client, err := self.newClient(callCtx, hop)
		self.client = client
		err = errors.Join(err, callCtx.Err(), self.ctx.Err())
		if err != nil {
			return nil, errors.Join(err, self.retireClient())
		}
		if client == nil {
			return nil, errors.New("tunnel registration returned no client")
		}
	}
	response, err := self.client.postVerify(callCtx, hop, body)
	select {
	case <-self.client.done():
		err = errors.Join(err, self.retireClient())
	default:
	}
	return response, err
}

// Called only with the lease. Failed retirement closes admission and leaves
// the exact client with the parent worker for a final joined shutdown.
func (self *TunnelTransport) retireClient() error {
	if self.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := self.client.closeAndWait(ctx); err != nil {
		self.cancel()
		return fmt.Errorf("retire registered tunnel client: %w", err)
	}
	self.client = nil
	return nil
}

// Constructor-owned shutdown runs outside the client callback tree. It first
// joins the exclusive request lease, then closes the retained identity.
func (self *TunnelTransport) run() {
	defer close(self.closed)
	<-self.ctx.Done()
	<-self.clientLease
	if self.client != nil {
		self.closeErr = self.client.closeAndWait(context.Background())
		self.client = nil
	}
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

// Registration uses the caller's setup budget, while the completed client is
// owned by the operator. Every partially created identity has the same cleanup.
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
		newTunnelClientSettings, settings,
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
	client, err := registered.generator.NewClientContext(clientCtx, ctx, args, newTunnelClientSettings())
	if err != nil {
		return registered, fmt.Errorf("tunnel client setup: %w", err)
	}
	registered.client = client
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
