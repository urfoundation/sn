//go:build urnetwork_control_composition_test

// This explicit test build connects the actual manager to the simulator's
// production HTTP coordinator. Only external member resources are synthetic.
package miner

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// The fixture uses actual operation admission, generations, cancellation,
// teardown and HTTP serialization. It never starts an SDK or signs a wallet.
type ProviderSwarmControlComposition struct {
	swarm     *ProviderSwarm
	stateLock sync.Mutex
	starts    map[string]int
	stops     map[string]int
}

// Only this build tag makes the boundary available to cross-package tests.
// Configuration validation has separate public tests; each synthetic instance
// below still acquires and releases the real manager's operation ownership.
func NewProviderSwarmControlComposition(ctx context.Context, ids []string) (*ProviderSwarmControlComposition, error) {
	if ctx == nil || len(ids) == 0 || len(ids) > 64 {
		return nil, errors.New("composition requires a bounded manager census")
	}
	self := &ProviderSwarmControlComposition{starts: map[string]int{}, stops: map[string]int{}}
	runCtx, cancel := context.WithCancel(ctx)
	swarm := &ProviderSwarm{
		config: &ProviderSwarmConfig{Schema: ProviderSwarmSchema}, members: map[string]ProviderSwarmMember{}, running: map[string]bool{}, disabled: map[string]bool{}, failures: map[string]string{},
		instances: map[string]*providerSwarmInstance{}, memberOperations: map[string]*providerSwarmMemberOperation{}, memberGenerations: map[string]*providerSwarmMemberOperation{},
		runCtx: runCtx, runCancel: cancel, terminalErrors: make(chan error, 1), startTimeout: time.Minute,
	}
	self.swarm = swarm
	for _, id := range ids {
		if id == "" {
			cancel()
			return nil, errors.New("composition identity is absent")
		}
		if _, exists := swarm.members[id]; exists {
			cancel()
			return nil, errors.New("composition identity repeats")
		}
		member := ProviderSwarmMember{ID: id}
		swarm.members[id] = member
		swarm.config.Members = append(swarm.config.Members, member)
		swarm.disabled[id] = true
	}
	swarm.startMember = func(ctx context.Context, member ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		self.stateLock.Lock()
		self.starts[member.ID]++
		self.stateLock.Unlock()
		var once sync.Once
		return &providerSwarmInstance{connectedOverride: func() bool { return true }, cancel: func() {
			once.Do(func() { self.stateLock.Lock(); self.stops[member.ID]++; self.stateLock.Unlock() })
		}}, nil
	}
	for _, id := range ids {
		if err := swarm.controlMember(ctx, id, true); err != nil {
			self.Close()
			return nil, err
		}
	}
	return self, nil
}

// Actual manager handlers own status serialization and mutation semantics.
func (self *ProviderSwarmControlComposition) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.swarm.ServeHTTP(writer, request)
}

// Snapshots count real instance acquisition/close, rather than HTTP requests.
func (self *ProviderSwarmControlComposition) Counts(id string) (int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.starts[id], self.stops[id]
}

// All admitted operations join before the fixture's lifetime is released.
func (self *ProviderSwarmControlComposition) Close() { self.swarm.stopMembers() }
