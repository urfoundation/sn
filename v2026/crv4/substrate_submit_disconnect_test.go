// The production submission path must return accepted-but-unacknowledged
// writes even when disconnect dispatch precedes write completion.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
)

// Each accepted JSON request remains in its real GSRPC Write until released.
type substrateSubmitWrite struct {
	request []byte
	release chan struct{}
}

// The fixture owns its writer until cancellation and joins the real client.
type substrateSubmitWriter struct {
	ctx    context.Context
	writes chan substrateSubmitWrite
}

// JSON framing is preserved; only the physical write completion is gated.
func (self *substrateSubmitWriter) Write(raw []byte) (int, error) {
	write := substrateSubmitWrite{request: append([]byte(nil), raw...), release: make(chan struct{})}
	select {
	case self.writes <- write:
	case <-self.ctx.Done():
		return 0, self.ctx.Err()
	}
	select {
	case <-write.release:
		return len(raw), nil
	case <-self.ctx.Done():
		return 0, self.ctx.Err()
	}
}

// A prior pending call proves the disconnect has reached dispatch while the
// later submission's write is still blocked. This catches an inactive fork
// replacement through public APIs without timing or private transport hooks.
func TestSubmitRawReturnsDisconnectBeforeWriteCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader, peer := io.Pipe()
	writer := &substrateSubmitWriter{ctx: ctx, writes: make(chan substrateSubmitWrite)}
	transport, err := gsrpcgeth.DialIO(ctx, reader, writer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		reader.Close()
		peer.Close()
		transport.Close()
	})
	before := make(chan error, 1)
	go func() { before <- transport.CallContext(ctx, nil, "fixture_beforeSubmit") }()
	prior := <-writer.writes
	close(prior.release)

	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: &contextSubstrateClient{Client: transport, url: "stdio://rpc.example"}}}
	submitted := make(chan error, 1)
	go func() {
		receipt, err := chain.SubmitRawAndWatchFinalized(ctx, "0x0102")
		if receipt != nil {
			err = errors.New("uncertain send fabricated a finalized receipt")
		}
		submitted <- err
	}()
	accepted := <-writer.writes
	var request chainContextRPCRequest
	if err := json.Unmarshal(accepted.request, &request); err != nil || request.Method != "author_submitAndWatchExtrinsic" || len(request.Params) != 1 || string(request.Params[0]) != `"0x0102"` {
		t.Fatalf("submission did not preserve its exact bytes: request=%s err=%v", accepted.request, err)
	}
	if err := peer.CloseWithError(io.ErrUnexpectedEOF); err != nil {
		t.Fatal(err)
	}
	if err := <-before; !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("prior request did not witness disconnect dispatch: %v", err)
	}
	close(accepted.release)
	select {
	case err := <-submitted:
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("uncertain submission lost its physical cause: %v", err)
		}
	case write := <-writer.writes:
		t.Fatalf("uncertain submission was replayed: %s", write.request)
	case <-time.After(5 * time.Second):
		t.Fatal("accepted submission lost its response waiter after disconnect")
	}
}
