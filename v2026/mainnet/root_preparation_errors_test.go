// Actual retained preparation observations classify every joined hard cause
// before retry or cancellation. No fixture supplies successful custody facts.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A real physical observation fails after original admission. Mixed integrity,
// uncertainty and unknown refusals cannot borrow unavailable retry permission.
func TestRootPreparationHardCausesDominateUnavailableAndCancellation(t *testing.T) {
	cases := []struct {
		name   string
		cause  error
		cancel bool
	}{
		{name: "identity-cancellation", cause: durablevolume.ErrIdentity, cancel: true},
		{name: "integrity", cause: errRpcIntegrity},
		{name: "uncertain", cause: errMainnetDurablePublicationUncertain},
		{name: "unknown", cause: errors.New("synthetic retained authority refusal")},
	}
	for _, c := range cases {
		fixture := newBootstrapRootPassiveFixture(t)
		compositionPassiveArguments(t, fixture)
		parent, cancel := context.WithCancel(fixture.storageContext(t.Context()))
		defer cancel()
		armed, observed := false, 0
		host := &compositionObservationHost{Host: fixture.root.storage.Host, observe: func(file *os.File) error {
			if _, err := file.Stat(); err != nil {
				return err
			}
			if armed {
				observed++
				if c.cancel {
					cancel()
				}
				return errors.Join(durablevolume.ErrUnavailable, c.cause)
			}
			return nil
		}}
		ctx := durablepath.WithHost(parent, host)
		reader, err := openRootPassivePreparation(ctx, fixture.root.plan)
		if err != nil {
			t.Fatal(c.name, err)
		}
		defer reader.Close()
		before := mainnetNamespaceTest(t, fixture.root.plan.RunDirectory)
		reads := compositionRpcReads(fixture.census)
		service := fixture.root.plan.PassiveService
		args := []string{"root-monitor", "--rpc", service.RpcUrl, "--policy", fixture.root.plan.ServiceInput.Path, "--checkpoint", service.CheckpointPath}
		waits := 0
		hooks := monitorServiceHooks{wait: func(context.Context, string, time.Duration) bool { waits++; return false }}
		armed = true
		var out, diagnostic bytes.Buffer
		code := runRootCommandWithPolicy(ctx, args, &out, &diagnostic, time.Now, hooks, &service.Policy, rootObjectHash(service.Policy), reader)
		file := reader.reader.file
		closeErr := reader.Close()
		if code != 3 || waits != 0 || observed == 0 || out.Len() != 0 || compositionRpcReads(fixture.census) != reads || closeErr != nil || !reflect.DeepEqual(before, mainnetNamespaceTest(t, fixture.root.plan.RunDirectory)) {
			t.Fatal("root preparation softened a completed hard observation", c.name, code, waits, observed, closeErr, diagnostic.String())
		}
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("root preparation retained its descriptor", c.name, err)
		}
	}
}
