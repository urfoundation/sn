package miner

// Wallet responses retain one body owner through cancellation and close. Only
// a fully read and successfully closed response can reach JSON decoding.

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/urnetwork/connect"
)

// Takes ownership of response.Body and closes it exactly once on every path.
// The request deadline stays live through close; its owner cancels it afterward.
func readSwarmWalletResponse(
	ownerCtx context.Context,
	requestCtx context.Context,
	response *http.Response,
	maxResponseBytes int64,
) (responseBytes []byte, err error) {
	defer func() {
		closeErr := response.Body.Close()
		err = errors.Join(err, closeErr, ownerCtx.Err(), requestCtx.Err())
		if response.StatusCode != http.StatusOK {
			statusError := &connect.HttpStatusError{StatusCode: response.StatusCode, Status: response.Status}
			if err == nil {
				statusError.Body = responseBytes
			}
			err = errors.Join(err, statusError)
		}
		if err != nil {
			responseBytes = nil
		}
	}()
	if maxResponseBytes < response.ContentLength {
		return nil, connect.ErrHttpResponseBodyTooLarge
	}
	responseBytes, err = io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if maxResponseBytes < int64(len(responseBytes)) {
		return nil, connect.ErrHttpResponseBodyTooLarge
	}
	return responseBytes, nil
}
