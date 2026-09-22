package miner

// Member controls serialize startup and teardown by identity. An admitted
// operation belongs to the swarm; disconnected callers only stop waiting.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/urnetwork/connect"
)

const providerSwarmMemberSchema = "urnetwork-provider-swarm-member-v1"

// Lifecycle state is independent of carrier readiness, including unrelated
// members' readiness. A failure can accompany a completed disabled startup.
type providerSwarmMemberStatus struct {
	Schema  string `json:"schema"`
	Id      string `json:"id"`
	State   string `json:"state"`
	Failure string `json:"failure,omitempty"`
}

// The operation pointer also identifies the generation created by an enable.
// Mutable fields use the swarm state lock; closing done publishes the result.
type providerSwarmMemberOperation struct {
	enable  bool
	done    chan struct{}
	cancel  context.CancelCauseFunc
	failure error
	err     error
}

// Reads only lifecycle bookkeeping, so status cannot wait on SDK readiness.
func (self *ProviderSwarm) memberStatus(id string) (providerSwarmMemberStatus, bool) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if _, ok := self.members[id]; !ok {
		return providerSwarmMemberStatus{}, false
	}
	status := providerSwarmMemberStatus{
		Schema: providerSwarmMemberSchema, Id: id, State: "disabled", Failure: self.failures[id],
	}
	if operation := self.memberOperations[id]; operation != nil {
		if operation.enable {
			status.State = "starting"
		} else {
			status.State = "stopping"
		}
	} else if status.Failure != "" {
		status.State = "failed"
	} else if self.instances[id] != nil {
		status.State = "running"
	}
	return status, true
}

// Admits at most one operation per member. Duplicate callers share its exact
// result; an opposite caller waits for cleanup before reconsidering the state.
func (self *ProviderSwarm) controlMember(ctx context.Context, id string, enable bool) error {
	for {
		self.stateLock.Lock()
		if err := ctx.Err(); err != nil {
			self.stateLock.Unlock()
			return err
		}
		member, ok := self.members[id]
		if !ok {
			self.stateLock.Unlock()
			return fmt.Errorf("unknown swarm member %q", id)
		}
		if self.stopping || self.runCtx == nil {
			self.stateLock.Unlock()
			return fmt.Errorf("swarm is stopping: %w", context.Canceled)
		}
		runCtx := self.runCtx
		if err := runCtx.Err(); err != nil {
			self.stateLock.Unlock()
			return err
		}
		operation := self.memberOperations[id]
		if operation == nil {
			if enable && self.instances[id] != nil && self.running[id] {
				self.stateLock.Unlock()
				return nil
			}
			if !enable && self.instances[id] == nil && self.disabled[id] {
				delete(self.failures, id)
				self.stateLock.Unlock()
				return nil
			}
			if enable && !self.disabled[id] || !enable && self.instances[id] == nil {
				self.stateLock.Unlock()
				return fmt.Errorf("swarm member %q cannot change from its current state", id)
			}
			operation = &providerSwarmMemberOperation{enable: enable, done: make(chan struct{})}
			self.memberOperations[id] = operation
			self.operationWaitGroup.Add(1)
			delete(self.failures, id)
			if enable {
				self.memberGenerations[id] = operation
				delete(self.disabled, id)
				startMember := self.startMember
				startTimeout := self.startTimeout
				self.stateLock.Unlock()
				go self.startMemberOperation(runCtx, member, operation, startMember, startTimeout)
			} else {
				instance := self.instances[id]
				generation := self.memberGenerations[id]
				delete(self.memberGenerations, id)
				delete(self.running, id)
				self.stateLock.Unlock()
				go self.stopMemberOperation(id, operation, generation, instance)
			}
		} else {
			self.stateLock.Unlock()
		}
		select {
		case <-operation.done:
			if operation.enable == enable {
				return operation.err
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-runCtx.Done():
			return runCtx.Err()
		}
	}
}

// Startup has its own deadline; once published, the same context remains a
// child of the swarm until disable. No signed wallet request is retried here.
func (self *ProviderSwarm) startMemberOperation(
	runCtx context.Context,
	member ProviderSwarmMember,
	operation *providerSwarmMemberOperation,
	startMember func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error),
	startTimeout time.Duration,
) {
	defer self.operationWaitGroup.Done()
	memberCtx, cancel := context.WithCancelCause(runCtx)
	self.stateLock.Lock()
	operation.cancel = cancel
	self.stateLock.Unlock()
	deadline := time.AfterFunc(startTimeout, func() { cancel(context.DeadlineExceeded) })
	instance, err := startMember(memberCtx, member, func(failure error) {
		self.memberFailed(member.ID, operation, failure)
	})
	if !deadline.Stop() {
		cancel(context.DeadlineExceeded)
	}
	self.stateLock.Lock()
	if operation.failure != nil {
		err = operation.failure
	} else if cause := context.Cause(memberCtx); cause != nil {
		err = cause
	} else if self.stopping {
		err = context.Canceled
	} else if err == nil && instance == nil {
		err = errors.New("startup returned no member instance")
	}
	if err == nil {
		self.instances[member.ID] = instance
		self.running[member.ID] = true
		self.finishMemberOperationWithLock(member.ID, operation, nil)
		self.stateLock.Unlock()
		return
	}
	// Keep the operation slot until all startup resources have joined. Its
	// callbacks are stale as soon as startup is rejected.
	delete(self.memberGenerations, member.ID)
	self.stateLock.Unlock()
	cancel(err)
	instance.close()
	self.stateLock.Lock()
	self.disabled[member.ID] = true
	self.failures[member.ID] = err.Error()
	self.finishMemberOperationWithLock(member.ID, operation, fmt.Errorf("enable swarm member %s: %w", member.ID, err))
	self.stateLock.Unlock()
}

// Teardown cannot be abandoned when a waiter disconnects. The slot stays
// stopping until both the member lifetime and its owned SDK resources close.
func (self *ProviderSwarm) stopMemberOperation(
	id string,
	operation *providerSwarmMemberOperation,
	generation *providerSwarmMemberOperation,
	instance *providerSwarmInstance,
) {
	defer self.operationWaitGroup.Done()
	if generation != nil && generation.cancel != nil {
		generation.cancel(context.Canceled)
	}
	instance.close()
	self.stateLock.Lock()
	delete(self.instances, id)
	self.disabled[id] = true
	self.finishMemberOperationWithLock(id, operation, nil)
	self.stateLock.Unlock()
}

// Publishes the operation result only after its resources have one clear owner.
func (self *ProviderSwarm) finishMemberOperationWithLock(id string, operation *providerSwarmMemberOperation, err error) {
	operation.err = err
	delete(self.memberOperations, id)
	close(operation.done)
}

// A callback can reject only its own generation. During startup its error is
// returned to the control caller; a published member failure ends the swarm.
func (self *ProviderSwarm) memberFailed(id string, generation *providerSwarmMemberOperation, err error) {
	if err == nil {
		return
	}
	self.stateLock.Lock()
	if generation == nil || self.stopping || self.memberGenerations[id] != generation {
		self.stateLock.Unlock()
		return
	}
	if self.memberOperations[id] == generation {
		if generation.failure == nil {
			generation.failure = err
		}
		cancel := generation.cancel
		self.stateLock.Unlock()
		cancel(err)
		return
	}
	delete(self.running, id)
	self.failures[id] = err.Error()
	terminalErrors := self.terminalErrors
	cancel := self.runCancel
	self.stateLock.Unlock()
	if terminalErrors != nil {
		select {
		case terminalErrors <- fmt.Errorf("member %s: %w", id, err):
		default:
		}
	}
	if cancel != nil {
		cancel()
	}
}

// Closes admission before canceling, then joins operations before collecting
// remaining instances. Callers may still read status throughout shutdown.
func (self *ProviderSwarm) stopMembers() {
	self.stateLock.Lock()
	self.stopping = true
	cancel := self.runCancel
	self.memberGenerations = map[string]*providerSwarmMemberOperation{}
	self.stateLock.Unlock()
	if cancel != nil {
		cancel()
	}
	self.operationWaitGroup.Wait()
	self.stateLock.Lock()
	instances := make([]*providerSwarmInstance, 0, len(self.instances))
	for _, instance := range self.instances {
		instances = append(instances, instance)
	}
	self.instances = map[string]*providerSwarmInstance{}
	self.running = map[string]bool{}
	self.runCtx = nil
	self.runCancel = nil
	self.terminalErrors = nil
	self.stateLock.Unlock()
	for _, instance := range instances {
		instance.close()
	}
}

// Only typed transport failures are retryable. Semantic wallet refusals remain
// conflicts, even when their messages contain words such as timeout or busy.
func swarmControlErrorStatus(err error) int {
	for _, transient := range []error{
		context.Canceled, context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF,
		net.ErrClosed, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE,
		syscall.ENETUNREACH, syscall.EHOSTUNREACH,
	} {
		if errors.Is(err, transient) {
			return http.StatusServiceUnavailable
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary()) {
		return http.StatusServiceUnavailable
	}
	var statusError *connect.HttpStatusError
	if errors.As(err, &statusError) {
		switch statusError.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return http.StatusServiceUnavailable
		}
	}
	return http.StatusConflict
}

// Keeps the legacy swarm response on successful mutations and exposes a
// separate member lifecycle read for reconciling timed-out control requests.
func (self *ProviderSwarm) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/status" && request.Method == http.MethodGet {
		status := self.status()
		writer.Header().Set("Content-Type", "application/json")
		if status.Running != status.Configured || len(status.Failures) != 0 {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(writer).Encode(status)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "control" {
		http.NotFound(writer, request)
		return
	}
	if request.Method == http.MethodGet && parts[2] == "status" {
		status, ok := self.memberStatus(parts[1])
		if !ok {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(status)
		return
	}
	if request.Method != http.MethodPost || parts[2] != "disable" && parts[2] != "enable" {
		http.NotFound(writer, request)
		return
	}
	if err := self.controlMember(request.Context(), parts[1], parts[2] == "enable"); err != nil {
		http.Error(writer, err.Error(), swarmControlErrorStatus(err))
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(self.status())
}
