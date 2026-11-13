// Shared dial boundary for direct EVM clients. HTTP response admission is
// local to each client; WebSocket and IPC keep geth's existing transports.
package evmrpc

import (
	"context"
	"net/http"
	"net/url"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Construct a client without probing, changing endpoints or retrying calls.
// The caller retains ownership of deadlines, chain identity and signed work.
func DialContext(ctx context.Context, endpoint string) (*ethclient.Client, error) {
	return dialContext(ctx, endpoint, http.DefaultTransport)
}

// The immutable transport is shared only with this client's HTTP requests.
func dialContext(ctx context.Context, endpoint string, base http.RoundTripper) (*ethclient.Client, error) {
	options := []rpc.ClientOption{rpc.WithHTTPClient(&http.Client{
		Transport: &responseTransport{base: base, maximumBytes: maximumResponseBytes},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})}
	owner := &runtimeTransport{}
	owner.generation.Store(1)
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "ws", "wss":
		owner.websocket = true
		options = append(options, rpc.WithWebsocketDialer(runtimeWebsocketDialer(owner)))
	case "http", "https":
	default:
		// IPC retains its existing behavior without claiming a tracked native
		// runtime capability. Production fleet routes are HTTP or WebSocket.
		owner = nil
	}
	client, err := rpc.DialOptions(ctx, endpoint, options...)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		retainRuntimeTransport(client, owner)
	}
	return ethclient.NewClient(client), nil
}
