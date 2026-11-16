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

// Uploads into the api go over HTTP/1.1. On 2026-11-16 the path into the api's
// site lost packets at about 90 ms RTT (the den1 to sjc2 to fmt2 Hurricane
// Electric segment). Over HTTP/2 the load balancer gives an upload stream the
// receive window of its 64 KiB client body buffer and opens it again only as
// it drains that buffer, so a body moves at most one buffer per round trip,
// and less once losses stall the connection. Uploads measured about 100 KB/s
// that way, against 275 to 850 KB/s over HTTP/1.1, where TCP alone paces the
// body and its window grows. Reads keep HTTP/2. Shared like the read
// transport, so an origin's idle upload connections stay pooled across owners.
var releaseApiUploadHttpTransport = newReleaseApiUploadHttpTransport(DefaultAttemptUploadSettings())

// The shared transport's proxy, dial and TLS settings with HTTP/1.1 only, and
// the upload settings' pool and write buffer.
func newReleaseApiUploadHttpTransport(settings AttemptUploadSettings) *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: releaseAPIHTTPDialTimeout, KeepAlive: releaseAPIHTTPKeepAlive}).DialContext,
		Protocols:             protocols,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   settings.IdleConnsPerHost,
		IdleConnTimeout:       settings.IdleConnTimeout,
		TLSHandshakeTimeout:   releaseAPIHTTPTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		WriteBufferSize:       settings.WriteBufferBytes,
	}
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
