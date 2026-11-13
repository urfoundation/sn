package validator

// Go's default transport never health-checks an HTTP/2 connection. When an lb
// or API container behind a pooled connection is recreated, the severed
// connection keeps taking requests until each one reaches its own deadline,
// spending a whole steering read attempt on a dead socket. These transports
// ping a connection that has received no frame for the send-ping timeout and
// close it when no answer arrives within the ping timeout, so a later attempt
// dials afresh well inside a 60-second read attempt.

import (
	"net"
	"net/http"
	"time"
)

const (
	releaseAPIHTTPDialTimeout         = 30 * time.Second
	releaseAPIHTTPKeepAlive           = 30 * time.Second
	releaseAPIHTTPTLSHandshakeTimeout = 10 * time.Second
	releaseAPIHTTPIdleConnTimeout     = 90 * time.Second
	releaseAPIHTTP2SendPingTimeout    = 15 * time.Second
	releaseAPIHTTP2PingTimeout        = 10 * time.Second
)

// Shared like http.DefaultTransport, which these API clients used before, so
// one origin's connections stay pooled across readers and writers.
var releaseAPIHTTPTransport = newReleaseAPIHTTPTransport()

// Mirrors http.DefaultTransport's proxy, dial and pool settings and adds the
// HTTP/2 health check. Request semantics, bounds and credentials are unchanged.
func newReleaseAPIHTTPTransport() *http.Transport {
	return withReleaseAPIHTTP2Health(&http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: releaseAPIHTTPDialTimeout, KeepAlive: releaseAPIHTTPKeepAlive}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       releaseAPIHTTPIdleConnTimeout,
		TLSHandshakeTimeout:   releaseAPIHTTPTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	})
}

// Adds the health check to a transport before its first request, keeping any
// other HTTP/2 settings it already carries.
func withReleaseAPIHTTP2Health(transport *http.Transport) *http.Transport {
	config := &http.HTTP2Config{}
	if transport.HTTP2 != nil {
		copied := *transport.HTTP2
		config = &copied
	}
	config.SendPingTimeout = releaseAPIHTTP2SendPingTimeout
	config.PingTimeout = releaseAPIHTTP2PingTimeout
	transport.HTTP2 = config
	return transport
}
