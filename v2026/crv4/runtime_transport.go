// Runtime authority belongs to the exact transport owner which completed its
// observation. A pooled API container or a reconnect is not that authority.
package crv4

import (
	"errors"
	"reflect"

	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
)

// A lost observation is retryable by the existing bounded read owner. It
// establishes neither a different runtime nor permission to replay a write.
type runtimeTransportObservationError struct{}

func (self *runtimeTransportObservationError) Error() string {
	return "runtime transport observation expired; reobserve the original pinned block"
}

func (self *runtimeTransportObservationError) Timeout() bool   { return false }
func (self *runtimeTransportObservationError) Temporary() bool { return true }

// Custom clients without a generation retain only their immutable client and
// endpoint identity. Production clients also expose every socket replacement.
type runtimeTransportObservation struct {
	client     gsrpcclient.Client
	endpoint   string
	generation uint64
	tracked    bool
}

// The caller exclusively owns changes to Chain/API fields. A transport's own
// concurrent reconnect is observed through its atomic generation instead.
func observeRuntimeTransport(chain *Chain) (runtimeTransportObservation, error) {
	if chain == nil || chain.API == nil || chain.API.Client == nil {
		return runtimeTransportObservation{}, errors.New("runtime transport owner is unavailable")
	}
	client := chain.API.Client
	value := reflect.ValueOf(client)
	if !value.Comparable() || value.Kind() == reflect.Pointer && value.IsNil() {
		return runtimeTransportObservation{}, errors.New("runtime transport has no stable client identity")
	}
	observation := runtimeTransportObservation{client: client, endpoint: client.URL()}
	if tracked, ok := client.(interface{ TransportGeneration() uint64 }); ok {
		observation.tracked = true
		observation.generation = tracked.TransportGeneration()
		if observation.generation == 0 {
			return runtimeTransportObservation{}, &runtimeTransportObservationError{}
		}
	}
	return observation, nil
}

// Incomplete or changed custody requires a new observation; it cannot alter
// retained transaction bytes or establish a different chain/runtime identity.
func (self runtimeTransportObservation) matches(chain *Chain) bool {
	current, err := observeRuntimeTransport(chain)
	return err == nil && self.client != nil && self == current
}
