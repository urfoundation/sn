// Check the complete finite response before geth publishes a successful prefix
// or matches batch members independently. Typed result decoding stays in geth.
package evmrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Raw fields distinguish missing values from legitimate JSON null results.
type envelope struct {
	Version string          `json:"jsonrpc"`
	Id      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// Authenticate the exact singleton/batch shape and complete unique ID set.
// Generated geth requests have GetBody; absence cannot authenticate a reply.
func validateEnvelope(request *http.Request, encoded []byte) error {
	if err := protocol.ValidateUniqueJsonKeys(encoded); err != nil {
		return fmt.Errorf("EVM HTTP response envelope is malformed: %v", err)
	}
	if request.GetBody == nil {
		return errors.New("EVM HTTP request cannot authenticate response ids")
	}
	body, err := request.GetBody()
	if err != nil {
		return err
	}
	requestBytes, readErr := io.ReadAll(io.LimitReader(body, maximumResponseBytes+1))
	if err := errors.Join(readErr, body.Close()); err != nil {
		return err
	}
	if int64(len(requestBytes)) > maximumResponseBytes {
		return errors.New("EVM HTTP request exceeds envelope byte limit")
	}
	requestBytes, encoded = bytes.TrimSpace(requestBytes), bytes.TrimSpace(encoded)
	batch := bytes.HasPrefix(requestBytes, []byte("["))
	var requests []envelope
	if batch {
		err = json.Unmarshal(requestBytes, &requests)
	} else {
		var value envelope
		err = json.Unmarshal(requestBytes, &value)
		requests = append(requests, value)
	}
	if err != nil || len(requests) == 0 {
		return errors.New("EVM HTTP request envelope is malformed")
	}
	expectedIdKVs := make(map[string]bool, len(requests))
	for _, value := range requests {
		id := string(value.Id)
		if value.Version != "2.0" || id == "" || id == "null" || expectedIdKVs[id] {
			return errors.New("EVM HTTP request has a missing or repeated response id")
		}
		expectedIdKVs[id] = true
	}
	if batch != bytes.HasPrefix(encoded, []byte("[")) {
		return errors.New("EVM HTTP response has the wrong singleton/batch shape")
	}
	var responses []envelope
	if batch {
		err = json.Unmarshal(encoded, &responses)
	} else {
		var value envelope
		err = json.Unmarshal(encoded, &value)
		responses = append(responses, value)
	}
	if err != nil {
		return fmt.Errorf("EVM HTTP response envelope is malformed: %v", err)
	}
	for _, value := range responses {
		id := string(value.Id)
		if value.Version != "2.0" || !expectedIdKVs[id] {
			return errors.New("EVM HTTP response has a foreign or repeated id/version")
		}
		delete(expectedIdKVs, id)
		// Some compatible servers explicitly emit error:null on success.
		// Only a non-null error competes with the result field.
		hasError := len(value.Error) != 0 && !bytes.Equal(value.Error, []byte("null"))
		if (len(value.Result) != 0) == hasError {
			return errors.New("EVM HTTP response has an ambiguous result/error")
		}
	}
	if len(expectedIdKVs) != 0 {
		return errors.New("EVM HTTP response is missing requested ids")
	}
	return nil
}
