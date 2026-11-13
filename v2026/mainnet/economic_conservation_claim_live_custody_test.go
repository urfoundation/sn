// Public Claim sampling keeps one fresh custody fence at dispatch and one at
// publication. Intervening owner loss cannot grant effects from a cached index.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Retire one full original window through the real proposal/signature/apply path.
func economicConservationClaimLiveCustodyFixture(t *testing.T) *economicConservationArchiveFixture {
	t.Helper()
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	_, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic original live Claim custody review")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("original Claim custody fixture did not apply", code, issue)
	}
	return f
}

// Matching bytes in a replacement inode never replace the original held owner.
func economicConservationClaimReplaceHeldArchive(t *testing.T, f *economicConservationArchiveFixture) {
	t.Helper()
	path := f.request.ArchivePath
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(filepath.Dir(path), "retained-"+filepath.Base(path))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// A completed first sample does not authorize another read after owner loss.
func TestEconomicConservationClaimNextSampleChecksFreshOriginalCustody(t *testing.T) {
	f := economicConservationClaimLiveCustodyFixture(t)
	var events int
	var reads uint64
	var retained []byte
	var output, diagnostic bytes.Buffer
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	hooks := monitorServiceHooks{
		afterEvent: func(context.Context, string) {
			events++
			if events == 1 {
				var err error
				retained, err = os.ReadFile(f.source.checkpoint)
				reads = f.source.claimReads.Load()
				if err != nil || reads == 0 {
					t.Fatal("first real sample did not establish the dependent operation", reads, err)
				}
				economicConservationClaimReplaceHeldArchive(t, f)
			} else {
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			f.source.now = f.source.now.Add(time.Second)
			return ctx.Err() == nil
		},
	}
	code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	current, err := os.ReadFile(f.source.checkpoint)
	if code == 0 || events != 1 || f.source.claimReads.Load() != reads || err != nil || !bytes.Equal(current, retained) || len(bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})) != 1 {
		t.Fatal("lost original custody reached another dependent sample", code, events, reads, f.source.claimReads.Load(), err, diagnostic.String())
	}
}

// Loss or cancellation occurs inside an actual source read after the sample
// fence. The source's original response then joins before publication.
func TestEconomicConservationClaimPublicationKeepsOriginalAfterMidSampleFault(t *testing.T) {
	for _, fault := range []string{"owner-loss", "cancel"} {
		f := economicConservationClaimLiveCustodyFixture(t)
		original, err := os.ReadFile(f.source.checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
		entered, proceed := make(chan struct{}), make(chan struct{})
		var once, released sync.Once
		release := func() { released.Do(func() { close(proceed) }) }
		originalFault := f.source.native.chain.fault
		f.source.native.chain.fault = func(method string, params []json.RawMessage, call int) (any, bool) {
			if method == "chain_getFinalizedHead" {
				once.Do(func() { close(entered); <-proceed })
			}
			return originalFault(method, params, call)
		}
		var output, diagnostic bytes.Buffer
		done := make(chan int, 1)
		joined := false
		defer func() {
			cancel()
			release()
			if !joined {
				<-done
			}
		}()
		args := f.source.args(t)
		go func() {
			done <- runMainWithMonitorHooks(ctx, args, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
		}()
		select {
		case <-entered:
			if fault == "owner-loss" {
				economicConservationClaimReplaceHeldArchive(t, f)
			} else {
				cancel()
			}
			release()
		case code := <-done:
			joined = true
			t.Fatal("Claim sample did not reach its actual native read", fault, code, diagnostic.String())
		case <-ctx.Done():
			cancel()
			release()
			<-done
			joined = true
			t.Fatal("Claim sample did not join its native read barrier", fault, diagnostic.String())
		}
		code := <-done
		joined = true
		cancel()
		current, readErr := os.ReadFile(f.source.checkpoint)
		if output.Len() != 0 || readErr != nil || !bytes.Equal(current, original) || fault == "owner-loss" && code == 0 || fault == "cancel" && (code != 0 || strings.Contains(diagnostic.String(), "changed its original")) {
			t.Fatal("mid-sample fault published a dependent Claim checkpoint", fault, code, readErr, diagnostic.String())
		}
	}
}
