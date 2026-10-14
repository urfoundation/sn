// Exact retained EVM custody survives unavailable native parent observations;
// complete contradictory parent values remain hard refusals before publication.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEvmRetainedParentObservationFailureKeepsOriginalReceipt(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	record, err := owner.store.load()
	if err != nil || record.Receipt == nil {
		t.Fatal("retained receipt fixture unavailable", err)
	}
	profiles := []rootReceiptProfile{f.config.Plan.Runtime}
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)
	parent := f.headers[record.Receipt.NativeHash].ParentHash
	base := chain.client.httpClient.Transport
	calls := 0
	chain.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		var input struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		err = errors.Join(json.NewDecoder(body).Decode(&input), body.Close())
		if err != nil {
			return nil, err
		}
		if input.Method == "state_getRuntimeVersion" && len(input.Params) == 1 && string(input.Params[0]) == fmt.Sprintf("%q", parent) {
			calls++
			return nil, context.DeadlineExceeded
		}
		return base.RoundTrip(request)
	})
	chain.client.retryWait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	_, err = chain.authenticatePositionWithRuntimeHistory(t.Context(), f.config.Plan, record, *record.Receipt, 0, profiles)
	if calls == 0 || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errRpcIntegrity) || strings.Contains(err.Error(), "parent identity differs") ||
		!maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) || len(f.writes) != 1 {
		t.Fatalf("unread historical parent became different authority or changed original receipt: calls=%d err=%v", calls, err)
	}
	chain.client.httpClient.Transport, chain.client.retryWait = base, nil
	observed, err := chain.authenticatePositionWithRuntimeHistory(t.Context(), f.config.Plan, record, *record.Receipt, 0, profiles)
	if err != nil || observed.FinalizedHash != record.Receipt.NativeHash || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) || len(f.writes) != 1 {
		t.Fatalf("same original receipt did not recover its native parent: %+v %v", observed, err)
	}
}

func TestEvmRetainedReturnedParentIdentityRemainsHard(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	record, err := owner.store.load()
	if err != nil || record.Receipt == nil {
		t.Fatal("retained receipt fixture unavailable", err)
	}
	profiles := []rootReceiptProfile{f.config.Plan.Runtime}
	if _, err := chain.authenticatePositionWithRuntimeHistory(t.Context(), f.config.Plan, record, *record.Receipt, 0, profiles); err != nil {
		t.Fatal("complete historical parent baseline failed", err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)
	base := chain.client.httpClient.Transport
	identities, waits := 0, 0
	chain.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		var input struct {
			Method string `json:"method"`
		}
		err = errors.Join(json.NewDecoder(body).Decode(&input), body.Close())
		if err != nil {
			return nil, err
		}
		if input.Method == "system_chain" {
			identities++
			if identities == 2 {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"result":"synthetic-foreign-parent"}`))}, nil
			}
		}
		return base.RoundTrip(request)
	})
	chain.client.retryWait = func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }
	_, err = chain.authenticatePositionWithRuntimeHistory(t.Context(), f.config.Plan, record, *record.Receipt, 0, profiles)
	if err == nil || !strings.Contains(err.Error(), "parent identity differs") || waits != 0 || identities != 2 ||
		!maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) || len(f.writes) != 1 {
		t.Fatalf("returned parent identity conflict was weakened or changed custody: identities=%d waits=%d err=%v", identities, waits, err)
	}
}

func TestEvmNativeHeaderFailureDoesNotInventParentLinkChange(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	record, err := owner.store.load()
	if err != nil || record.Receipt == nil {
		t.Fatal("retained receipt fixture unavailable", err)
	}
	receipt := *record.Receipt
	number := receipt.NativeNumber
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)
	originalHeader := f.headers[receipt.NativeHash]
	f.stateLock.Lock()
	f.override = func(method string, params []any, result any) any {
		if method == "chain_getHeader" && len(params) == 1 && params[0] == receipt.NativeHash {
			changed := originalHeader
			changed.StateRoot = "0x" + strings.Repeat("41", 32)
			return changed
		}
		return result
	}
	f.stateLock.Unlock()
	cursor := evmActionRecord{ScanNumber: number - 1, ScanHash: originalHeader.ParentHash}
	observed, err := chain.locate(t.Context(), chainIdentity{FinalizedNumber: number}, cursor, receipt)
	if err == nil || strings.Contains(err.Error(), "parent link differs") || observed.ScanNumber != cursor.ScanNumber || observed.ScanHash != cursor.ScanHash || observed.Receipt != nil ||
		!maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) {
		t.Fatalf("unauthenticated header acquired a parent-link conclusion or advanced cursor: %+v %v", observed, err)
	}
	f.stateLock.Lock()
	f.override = nil
	f.stateLock.Unlock()
	observed, err = chain.locate(t.Context(), chainIdentity{FinalizedNumber: number}, cursor, receipt)
	if err != nil || observed.Receipt == nil || observed.ScanNumber != number || observed.Receipt.BlockHash != receipt.BlockHash {
		t.Fatalf("same original cursor did not recover its authenticated header: %+v %v", observed, err)
	}
}
