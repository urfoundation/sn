// Admit one complete JSON-RPC envelope before geth's HTTP adapter can ignore
// foreign ids, duplicate responses or bytes following its first JSON value.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Raw result/error fields preserve null results and detect ambiguous success.
// The ordinary RPC decoder still owns typed error and result decoding.
type chainRpcEnvelope struct {
	Version string          `json:"jsonrpc"`
	Id      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// Requests and responses have the same singleton/batch shape. Only missing
// batch members may continue to geth's typed incomplete-response handling;
// every present response must belong to exactly one requested id.
func validateChainHttpEnvelope(request *http.Request, response []byte) error {
	if err := protocol.ValidateUniqueJsonKeys(response); err != nil {
		// A complete in-memory document has no transport eof authority.
		return fmt.Errorf("EVM RPC response envelope is malformed: %v", err)
	}
	if request.GetBody == nil {
		return errors.New("EVM RPC request cannot authenticate its response ids")
	}
	body, err := request.GetBody()
	if err != nil {
		return err
	}
	encoded, readErr := io.ReadAll(io.LimitReader(body, chainHTTPResponseLimit+1))
	if err := errors.Join(readErr, body.Close()); err != nil {
		return err
	}
	if int64(len(encoded)) > chainHTTPResponseLimit {
		return errors.New("EVM RPC request exceeds envelope byte limit")
	}
	encoded, response = bytes.TrimSpace(encoded), bytes.TrimSpace(response)
	batch := bytes.HasPrefix(encoded, []byte("["))
	var requests []chainRpcEnvelope
	if batch {
		err = json.Unmarshal(encoded, &requests)
	} else {
		var value chainRpcEnvelope
		err = json.Unmarshal(encoded, &value)
		requests = append(requests, value)
	}
	if err != nil || len(requests) == 0 {
		return errors.New("EVM RPC request envelope is malformed")
	}
	expectedIds := make(map[string]bool, len(requests))
	for _, value := range requests {
		id := string(value.Id)
		if value.Version != "2.0" || id == "" || id == "null" || expectedIds[id] {
			return errors.New("EVM RPC request envelope has an invalid version or repeated/missing id")
		}
		expectedIds[id] = true
	}
	if batch != bytes.HasPrefix(response, []byte("[")) {
		return errors.New("EVM RPC response envelope has the wrong singleton/batch shape")
	}
	var responses []chainRpcEnvelope
	if batch {
		err = json.Unmarshal(response, &responses)
	} else {
		var value chainRpcEnvelope
		err = json.Unmarshal(response, &value)
		responses = append(responses, value)
	}
	if err != nil {
		// An empty complete response is malformed, not a transport eof. The
		// actual body interruption has already been checked by RoundTrip.
		return fmt.Errorf("EVM RPC response envelope is malformed: %v", err)
	}
	for _, value := range responses {
		id := string(value.Id)
		if value.Version != "2.0" || !expectedIds[id] {
			return errors.New("EVM RPC response envelope has a foreign or duplicate id/version")
		}
		delete(expectedIds, id)
		if (len(value.Result) == 0) == (len(value.Error) == 0) || bytes.Equal(value.Error, []byte("null")) {
			return errors.New("EVM RPC response envelope has an ambiguous result/error")
		}
	}
	return nil
}
