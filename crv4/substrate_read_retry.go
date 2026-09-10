package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/gorilla/websocket"
)

// GSRPC reconnects its shared client after a lost socket, but returns the
// disconnect to in-flight calls. Only exact observation methods may replay.
// Subscriptions and transaction submission retain the original transport path.
func substrateRPCReadMayReplay(method string) bool {
	switch method {
	case "chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash", "chain_getBlock",
		"state_getStorage", "state_getStorageHash", "state_getMetadata", "state_getRuntimeVersion",
		"state_queryStorageAt", "state_getKeys", "state_getKeysPaged", "state_call", "system_accountNextIndex":
		return true
	default:
		return false
	}
}

func substrateRPCDisconnected(err error) bool {
	var rpcError gsrpcgeth.Error
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, gsrpcgeth.ErrClientQuit) || errors.As(err, &rpcError) {
		return false
	}
	var closed *websocket.CloseError
	if errors.As(err, &closed) {
		return closed.Code == websocket.CloseAbnormalClosure || closed.Code == websocket.CloseGoingAway
	}
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || err.Error() == "client reconnected"
}

// Route GSRPC's contextless storage helpers through the same bounded read path.
func (self *contextSubstrateClient) Call(result any, method string, args ...any) error {
	return self.CallContext(context.Background(), result, method, args...)
}

func (self *contextSubstrateClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if !substrateRPCReadMayReplay(method) {
		return self.Client.CallContext(ctx, result, method, args...)
	}
	// One total bound includes reconnect attempts and backoff, even for legacy
	// contextless readers. A shorter caller deadline always takes precedence.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Freeze the encoded parameters before the first attempt. In particular,
	// a historical block hash and storage key cannot change during recovery.
	frozen := make([]any, len(args))
	for index, argument := range args {
		encoded, err := json.Marshal(argument)
		if err != nil {
			return err
		}
		frozen[index] = json.RawMessage(encoded)
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := self.Client.CallContext(ctx, result, method, frozen...)
		if !substrateRPCDisconnected(err) || attempt == 3 {
			return err
		}
		// Reuse the existing client's serialized reconnect and joined Close;
		// replacing it here would disrupt the other concurrent audit readers.
		timer := time.NewTimer(time.Second << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
