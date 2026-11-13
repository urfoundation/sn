// Borrowed original custody must survive exact observation and host boundaries.
// Every fixture uses independent synthetic approvals and local-only transports.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Keep the original inode alive so identical replacement bytes cannot hide a
// lost flock. Restoring it is test cleanup, never a production recovery path.
func bootstrapReadinessTestReplace(t *testing.T, path string) func() {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := path + ".test-original"
	if err := os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Rename(original, path); err != nil {
			t.Fatal(err)
		}
	}
}

// Mutate after the first selected finalized-head response, while all original
// descriptors remain borrowed. No clock or scheduler timing drives the race.
func bootstrapReadinessTestAtRead(t *testing.T, client *rpcClient, change func()) *bool {
	t.Helper()
	original := client.httpClient.Transport
	fired := new(bool)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		var call struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		response, err := original.RoundTrip(request)
		if err == nil && call.Method == "chain_getFinalizedHead" && !*fired {
			*fired = true
			change()
		}
		return response, err
	})
	t.Cleanup(func() { client.httpClient.Transport = original })
	return fired
}

// Legacy preparation borrows five markers; passive preparation borrows three.
// Each identical-byte replacement previously allowed a complete readiness seal.
func TestBootstrapReadinessRejectsEveryBorrowedMarkerReplacement(t *testing.T) {
	for _, passive := range []bool{false, true} {
		count := 5
		if passive {
			count = 3
		}
		for index := 0; index < count; index++ {
			var f *bootstrapChainFixture
			if passive {
				f = newBootstrapRootPassiveFixture(t)
				f.result(t, "apply")
			} else {
				f = newBootstrapChainReadinessFixture(t)
			}
			before := f.journals(t)
			var restore func()
			fired := bootstrapReadinessTestAtRead(t, f.client, func() {
				restore = bootstrapReadinessTestReplace(t, f.preparation.childPaths()[index]+".lock")
			})
			result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
			if !*fired || !errors.Is(err, errRpcIntegrity) || result.ObservationComplete || result.Census != nil || result.PassiveRoot != nil || result.Status != "unresolved" {
				t.Errorf("passive=%v marker=%d: changed custody published readiness: fired=%v complete=%v status=%s err=%v", passive, index, *fired, result.ObservationComplete, result.Status, err)
			}
			if restore != nil {
				restore()
			}
			if after := f.journals(t); !maps.Equal(before, after) {
				t.Fatal("readiness rewrote an original preparation journal or signed intent")
			}
		}
	}
}

// A completed marker cannot make a deleted child into recoverable pending work.
func TestBootstrapReadinessRejectsDeletedCompletedJournalDuringRead(t *testing.T) {
	for _, index := range []int{1, 3} {
		f := newBootstrapChainReadinessFixture(t)
		path := f.preparation.childPaths()[index]
		fired := bootstrapReadinessTestAtRead(t, f.client, func() {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
		result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
		if !*fired || !errors.Is(err, errRpcIntegrity) || result.ObservationComplete || result.Census != nil {
			t.Errorf("journal=%d: completed custody deletion published readiness: fired=%v complete=%v err=%v", index, *fired, result.ObservationComplete, err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("read-only observation recreated a completed journal", err)
		}
	}
}

// Capture both sides of durable start reservation. A lost original lock may
// neither start a process nor erase an already consumed invocation allowance.
func TestRootPassiveHostRejectsBorrowedCustodyBeforeStart(t *testing.T) {
	for _, boundary := range []int{2, 3} {
		for index := 0; index < 3; index++ {
			f := newRootPassiveHostFixture(t)
			f.require("claim", "claimed")
			f.require("install", "installed")
			original := f.host.files.host.execute
			count := 0
			var restore func()
			f.host.files.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
				raw, err := original(ctx, path, args)
				if err == nil && strings.Contains(strings.Join(args, " "), " show ") && args[len(args)-1] == rootPassiveHostUnitName {
					count++
					if count == boundary {
						restore = bootstrapReadinessTestReplace(t, f.chain.preparation.childPaths()[index]+".lock")
					}
				}
				return raw, err
			}
			result, code, diagnostic := f.command("start")
			if restore == nil || code != 3 || f.starts != 0 || result.CurrentProcessRunning || result.Generation != nil || result.StartConsumed != (boundary == 3) {
				t.Errorf("boundary=%d marker=%d: lost borrowed custody reached start: code=%d starts=%d result=%+v diagnostic=%s", boundary, index, code, f.starts, result, diagnostic)
			}
			f.host.files.host.execute = original
			if restore != nil {
				restore()
			}
			historical, statusCode, detail := f.command("status")
			if statusCode != 0 || historical.StartConsumed != (boundary == 3) {
				t.Errorf("boundary=%d marker=%d: refusal rewrote start liability: code=%d result=%+v detail=%s", boundary, index, statusCode, historical, detail)
			}
		}
	}
}

// Losing custody after an issued effect cannot turn an unsaved in-memory
// acknowledgment into a successful public result or rewrite the journal.
func TestRootPassiveHostNeverPublishesCompletionAfterCustodyLoss(t *testing.T) {
	for _, operation := range []string{"install", "start"} {
		f := newRootPassiveHostFixture(t)
		f.require("claim", "claimed")
		if operation == "start" {
			f.require("install", "installed")
		}
		original := f.host.files.host.execute
		var restore func()
		var retained []byte
		f.host.files.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
			raw, err := original(ctx, path, args)
			command := strings.Join(args, " ")
			if err == nil && restore == nil && (operation == "install" && strings.HasSuffix(command, " daemon-reload") || operation == "start" && strings.Contains(command, " start -- ")) {
				restore = bootstrapReadinessTestReplace(t, f.chain.preparation.childPaths()[2]+".lock")
				retained, err = os.ReadFile(f.approval.Plan.StatePath)
			}
			return raw, err
		}
		result, code, detail := f.command(operation)
		if restore != nil {
			restore()
		}
		if restore == nil || code != 3 || result.Status != "original-custody-integrity-failure" || result.Installed || result.CurrentProcessRunning || result.Generation != nil || result.StartConsumed != (operation == "start") {
			t.Errorf("%s published unretained completion: changed=%v code=%d result=%+v detail=%s", operation, restore != nil, code, result, detail)
		}
		after, err := os.ReadFile(f.approval.Plan.StatePath)
		if err != nil || !bytes.Equal(retained, after) {
			t.Errorf("%s changed original host intent after custody loss: %v", operation, err)
		}
	}
}
