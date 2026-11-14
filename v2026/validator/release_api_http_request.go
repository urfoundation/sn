// A failed Http/2 ping has no exported Go error identity. A single request's
// transport owns that health event and cancels only its own request before the
// standard transport publishes its opaque connection-loss error.
package validator

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
)

// Clone a health-checking transport so its callback belongs to this request alone.
// Go's callback names the health event but not its connection; a shared callback
// cannot safely reclassify or cancel unrelated requests. All configured TLS,
// proxy, protocol and timeout policy remains intact. This deliberately gives
// up connection reuse for these bounded API operations. Custom transports keep
// their own error policy and are never granted this health-event authority.
func doReleaseApiHttpRequest(client *http.Client, request *http.Request) (resultResponse *http.Response, resultErr error) {
	transport, concrete := client.Transport.(*http.Transport)
	var diagnostic *releasePreparationDiagnostic
	if request != nil {
		diagnostic = newReleasePreparationHttpDiagnostic(request.Context(), releasePreparationHttpOpen)
	}
	defer func() { diagnostic.finish(resultErr) }()
	// Go installs an empty Http/2 config on first use even without health
	// checks. Keep that transport's pooling and error policy unchanged.
	if !concrete || transport == nil || transport.HTTP2 == nil || transport.HTTP2.SendPingTimeout <= 0 {
		return client.Do(request)
	}
	ownedTransport := transport.Clone()
	// A custom protocol hook may close over another transport's pool. Clone
	// omits Go's automatically installed hooks, including after first use.
	if ownedTransport.TLSNextProto != nil {
		return client.Do(request)
	}
	config := *transport.HTTP2
	ownedTransport.HTTP2 = &config
	ownedClient := *client
	ownedClient.Transport = ownedTransport
	parent := request.Context()
	operation, cancel := context.WithCancelCause(parent)
	// The unique concrete cause belongs only to this health callback, never
	// to an error's diagnostic text or an HTTP response.
	lostPing := &net.OpError{Op: "ping", Net: "http2", Err: net.ErrClosed}
	finished := make(chan struct{})
	previousCountError := config.CountError
	config.CountError = func(code string) {
		if previousCountError != nil {
			previousCountError(code)
		}
		if code == "conn_close_lost_ping" {
			cancel(lostPing)
			// This callback runs before Go aborts the connection's streams.
			// Let Do join our cancellation first. A concurrent independent
			// protocol/security error is still returned unchanged below.
			<-finished
		}
	}
	release := func() {
		cancel(nil)
		ownedTransport.CloseIdleConnections()
	}
	response, err := ownedClient.Do(request.WithContext(operation))
	close(finished)
	if err != nil {
		if original, ok := err.(*url.Error); ok && original != nil &&
			original.Err == context.Canceled && context.Cause(operation) == lostPing && parent.Err() == nil {
			copy := *original
			copy.Err = lostPing
			err = &copy
		}
		release()
		return response, err
	}
	if response == nil || response.Body == nil {
		release()
		return response, nil
	}
	response.Body = &releaseApiHttpResponseBody{ReadCloser: response.Body, parent: parent, operation: operation, lostPing: lostPing, release: release,
		diagnosticRead: newReleasePreparationHttpDiagnostic(operation, releasePreparationHttpRead)}
	return response, nil
}

// A successful header transfers the same request's health/cancellation owner to
// its physical body. Only this owner's exact cancellation is normalized; Close
// errors retain their independent verdict and Close retires the transport.
type releaseApiHttpResponseBody struct {
	io.ReadCloser
	parent         context.Context
	operation      context.Context
	lostPing       *net.OpError
	release        func()
	closeOnce      sync.Once
	closeErr       error
	diagnosticRead *releasePreparationDiagnostic
}

// Preserve independent body errors while identifying this owner's cancellation.
func (self *releaseApiHttpResponseBody) Read(value []byte) (int, error) {
	count, err := self.ReadCloser.Read(value)
	if err == context.Canceled && context.Cause(self.operation) == self.lostPing && self.parent.Err() == nil {
		err = self.lostPing
	}
	if err != io.EOF {
		self.diagnosticRead.finish(err)
	}
	return count, err
}

// Retire the private transport once, retaining the physical Close verdict.
func (self *releaseApiHttpResponseBody) Close() error {
	self.closeOnce.Do(func() {
		diagnostic := newReleasePreparationHttpDiagnostic(self.operation, releasePreparationHttpClose)
		self.closeErr = self.ReadCloser.Close()
		diagnostic.finish(self.closeErr)
		self.release()
	})
	return self.closeErr
}
