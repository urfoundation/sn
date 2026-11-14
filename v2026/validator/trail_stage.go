// A failed trail's stage says where it stopped. Every trail failure used to be
// reported only as its kind with an unknown cause, so an operator could not
// tell a missing seed provider from a pending attempt cut, a contract the
// platform never granted, a provider that never carried the connection, or a
// /verify request that failed after connecting. Stages and their transport
// evidence are observation-only labels from a closed set: none of them changes
// retry, attribution, custody or what a trail measures.
package validator

import (
	"context"
	"net/http/httptrace"
	"sync"

	"github.com/urfoundation/sn/v2026/diagnostics"
	"github.com/urnetwork/connect/v2026"
	connectprotocol "github.com/urnetwork/connect/v2026/protocol"
)

type trailStage uint8

const (
	trailStageUnknown      trailStage = iota
	trailStageDiscovery               // the seed discovery pace wait
	trailStagePick                    // the seed provider pick
	trailStageInternal                // local nonce, message or encoding
	trailStagePace                    // the seed attempt pace wait
	trailStageReserve                 // attempt reservation: cut or boundary pending, resolver
	trailStageJournal                 // the provider request journal
	trailStageSend                    // the request left the engine; the transport refines it
	trailStageLease                   // waiting for the shared tunnel client lease
	trailStageRegistration            // tunnel client identity, setup or key registration
	trailStageContract                // no contract for the hop: refused or never answered
	trailStageHandshake               // contract granted, the connection through the hop never completed
	trailStagePost                    // connected; TLS, the request or its response failed
	trailStageStatus                  // the API answered with a non-200 status
	trailStageCount
)

var trailStageNames = [trailStageCount]string{"unknown", "discovery", "pick", "internal", "pace", "reserve", "journal", "send", "lease", "registration", "contract", "handshake", "post", "status"}

func (self trailStage) String() string {
	if self < trailStageCount {
		return trailStageNames[self]
	}
	return "unknown"
}

// What the hop's contract manager reported during one /verify exchange.
type trailContractOutcome uint8

const (
	trailContractUnobserved trailContractOutcome = iota
	trailContractUnanswered
	trailContractGranted
	trailContractRefused
)

// The transport's stage evidence for one failed /verify exchange. Its text is
// exactly its cause's, so logs, the sim-testnet log gate and every errors.Is
// or errors.As check are unchanged.
type tunnelVerifyError struct {
	stage        trailStage
	contract     trailContractOutcome
	contractCode connectprotocol.ContractError
	status       int
	cause        error
}

func (self *tunnelVerifyError) Error() string { return self.cause.Error() }
func (self *tunnelVerifyError) Unwrap() error { return self.cause }

// Marks a transport failure with its stage; a nil cause stays nil.
func tunnelVerifyFailure(stage trailStage, cause error) error {
	if cause == nil {
		return nil
	}
	return &tunnelVerifyError{stage: stage, cause: cause}
}

// Marks a non-200 /verify answer with its status code.
func tunnelVerifyStatusFailure(status int, cause error) error {
	if cause == nil {
		return nil
	}
	return &tunnelVerifyError{stage: trailStageStatus, status: status, cause: cause}
}

// Records one /verify exchange's contract status for its hop and its HTTP
// progress. Callbacks only set fields under the mutex; nothing they record can
// change the request.
type tunnelVerifyObservation struct {
	hop          connect.Id
	mutex        sync.Mutex
	contract     trailContractOutcome
	contractCode connectprotocol.ContractError
	started      bool // the HTTP transport began obtaining a connection
	dialed       bool // a connection to the API exists (TLS began, or a plain connection)
	wrote        bool
	responded    bool
	stop         func()
}

// The contract manager is optional; a client without one records HTTP
// progress only.
type tunnelVerifyContractSource interface {
	verifyContractManager() *connect.ContractManager
}

// Begins observing before the request's first packet. Only the hop's own,
// non-companion contract statuses count.
func newTunnelVerifyObservation(client any, hop connect.Id) *tunnelVerifyObservation {
	self := &tunnelVerifyObservation{hop: hop, contract: trailContractUnanswered, stop: func() {}}
	source, ok := client.(tunnelVerifyContractSource)
	if !ok {
		return self
	}
	manager := source.verifyContractManager()
	if manager == nil {
		return self
	}
	self.stop = manager.AddContractStatusCallback(self.observeContract)
	return self
}

// Counts only the hop's own non-companion contracts. A grant outranks a
// refusal, so a retried request that finally got its contract reads granted.
func (self *tunnelVerifyObservation) observeContract(status *connect.ContractStatus) {
	if status == nil || status.Key.CompanionContract || status.Key.Destination.DestinationId != self.hop {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if status.Error == nil {
		self.contract = trailContractGranted
	} else if self.contract != trailContractGranted {
		self.contract, self.contractCode = trailContractRefused, *status.Error
	}
}

// The request context carries an HTTP trace into the transport's own client.
func (self *tunnelVerifyObservation) context(ctx context.Context) context.Context {
	mark := func(field *bool) {
		self.mutex.Lock()
		*field = true
		self.mutex.Unlock()
	}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn:              func(string) { mark(&self.started) },
		TLSHandshakeStart:    func() { mark(&self.dialed) },
		GotConn:              func(httptrace.GotConnInfo) { mark(&self.dialed) },
		WroteRequest:         func(httptrace.WroteRequestInfo) { mark(&self.wrote) },
		GotFirstResponseByte: func() { mark(&self.responded) },
	})
}

// Stops the contract callback; statuses after the request are not counted.
func (self *tunnelVerifyObservation) close() {
	self.stop()
}

// Marks a failed exchange with the stage its evidence shows. An existing
// stage from the request itself (a status code) is kept.
func (self *tunnelVerifyObservation) failure(cause error) error {
	if cause == nil {
		return nil
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	marked := &tunnelVerifyError{contract: self.contract, contractCode: self.contractCode, cause: cause}
	switch {
	case !self.started:
		marked.stage = trailStageRegistration
	case self.responded:
		marked.stage = trailStageStatus
	case self.dialed:
		marked.stage = trailStagePost
	case self.contract == trailContractGranted:
		marked.stage = trailStageHandshake
	default:
		marked.stage = trailStageContract
	}
	return marked
}

// The registered tunnel client's contract manager, for the observation only.
func (self *registeredTunnelClient) verifyContractManager() *connect.ContractManager {
	if self == nil || self.client == nil {
		return nil
	}
	return self.client.ContractManager()
}

// A failed trail's closed facts. Reading them opens only this package's
// concrete types and the standard library's own wrappers; no other error
// method runs and no error text is read.
type trailFailureFacts struct {
	kind         string
	stage        trailStage
	cause        string
	depth        int
	contract     trailContractOutcome
	contractCode connectprotocol.ContractError
	status       int
}

// Opens the trail's own transparent types for cause classification.
func trailCauseEnvelope(err error) ([]error, diagnostics.Cause, bool) {
	switch value := err.(type) {
	case *TrailError:
		if value != nil {
			return []error{value.Err}, diagnostics.CauseUnknown, true
		}
	case *tunnelVerifyError:
		if value != nil {
			return []error{value.cause}, diagnostics.CauseUnknown, true
		}
	}
	return nil, diagnostics.CauseUnknown, false
}

const (
	trailFactsMaximumNodes = 64
	trailFactsMaximumDepth = 32
)

// The first transport marker in graph order supplies the stage evidence; the
// first status code anywhere in its chain supplies the code.
func trailTransportFacts(err error, facts *trailFailureFacts) {
	remaining := trailFactsMaximumNodes
	var visit func(error, int) bool
	visit = func(err error, depth int) bool {
		for ; err != nil && depth < trailFactsMaximumDepth && 0 < remaining; depth++ {
			remaining--
			if marked, ok := err.(*tunnelVerifyError); ok && marked != nil {
				if facts.stage == trailStageSend || facts.stage == trailStageUnknown {
					facts.stage = marked.stage
					facts.contract, facts.contractCode = marked.contract, marked.contractCode
				}
				if facts.status == 0 && marked.status != 0 {
					facts.status = marked.status
				}
				err = marked.cause
				continue
			}
			children, ok := diagnostics.StandardWrapped(err)
			if !ok {
				return facts.stage != trailStageSend && facts.stage != trailStageUnknown
			}
			if len(children) == 1 {
				err = children[0]
				continue
			}
			for _, child := range children {
				if visit(child, depth+1) {
					return true
				}
			}
			return false
		}
		return false
	}
	visit(err, 0)
}

func trailFailureFactsOf(err error) trailFailureFacts {
	facts := trailFailureFacts{kind: "unknown", cause: "unknown"}
	var cause error = err
	if trailErr, ok := err.(*TrailError); ok && trailErr != nil {
		switch trailErr.Kind {
		case TrailErrorSeed:
			facts.kind = "seed"
		case TrailErrorHop:
			facts.kind = "hop"
		case TrailErrorProtocol:
			facts.kind = "protocol"
		case TrailErrorUnknownOutcome:
			facts.kind = "unknown_final"
		}
		facts.stage, facts.depth = trailErr.stage, max(0, trailErr.depth)
		cause = trailErr.Err
		if facts.stage == trailStageSend {
			trailTransportFacts(cause, &facts)
		}
	}
	switch diagnostics.ClassifyWrappedCause(cause, trailCauseEnvelope) {
	case diagnostics.CauseCanceled:
		facts.cause = "canceled"
	case diagnostics.CauseTimeout:
		facts.cause = "timeout"
	case diagnostics.CauseTransport:
		facts.cause = "transport"
	}
	return facts
}
