//go:build windows

// Package npipe provides the named-pipe API used by the substrate RPC client.
package npipe

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// Listen starts a listener on address.
func Listen(address string) (net.Listener, error) {
	return winio.ListenPipe(address, nil)
}

// DialTimeout connects to address before timeout elapses.
func DialTimeout(address string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(address, &timeout)
}
