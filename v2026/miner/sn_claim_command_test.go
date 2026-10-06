package miner

// Exercise the actual parsed finite claim command with a private credential
// and local server. No key or RPC option permits a transaction to be signed.

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/merkle"
)

// Synthetic complete one-leaf proof for the signer-free finite command.
func finiteClaimTestResult() *sdk.SnPoolClaimResult {
	coldkey := [32]byte{17, 29, 41}
	root := merkle.PayoutLeaf(coldkey, big.NewInt(10000))
	return &sdk.SnPoolClaimResult{Epoch: 1, NoId: []byte{7}, Coldkey: coldkey[:], ShareBps: 10000, PayoutRoot: root[:], Proof: [][]byte{}, ContractAddress: "0x0000000000000000000000000000000000000007", ChainId: 945}
}

// A child isolates the existing CLI exit/signal lifecycle from the test owner.
func TestFiniteClaimChild(t *testing.T) {
	if os.Getenv("URNETWORK_FINITE_CLAIM_TEST_CHILD") != "1" {
		return
	}
	args := []string{"claim", "--api_url=" + os.Getenv("URNETWORK_FINITE_CLAIM_TEST_URL")}
	if os.Getenv("URNETWORK_FINITE_CLAIM_TEST_EPOCH") == "1" {
		args = append(args, "--epoch=1")
	}
	Run(args)
}

// A finite status outage outlasts the SDK's parallel route attempts and short
// retry. The command's longer read-only operation must reach recovery.
func testFiniteClaimPublicGetRecovery(t *testing.T, target string) {
	t.Helper()
	const unavailableResponses = 16
	var targetReads, epochReads, poolReads, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
			http.Error(w, "unexpected mutation", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/hello" {
			return
		}
		if r.URL.Path == "/sn/epoch" {
			epochReads.Add(1)
		} else if r.URL.Path == "/sn/pool/claim" {
			poolReads.Add(1)
		} else {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		if r.URL.Path == target && targetReads.Add(1) <= unavailableResponses {
			http.Error(w, "synthetic temporary outage", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/sn/epoch" {
			_ = json.NewEncoder(w).Encode(&sdk.SnEpochResult{Epoch: 2})
		} else {
			_ = json.NewEncoder(w).Encode(finiteClaimTestResult())
		}
	}))
	defer server.Close()
	state := t.TempDir()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, ".provider.jwt"), []byte(financialTestJwt(t, financialTestClientId, "public-finite")), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFiniteClaimChild$", "-test.timeout=80s")
	cmd.Env = append(os.Environ(), "URNETWORK_STATE_DIR="+state, "URNETWORK_FINITE_CLAIM_TEST_CHILD=1", "URNETWORK_FINITE_CLAIM_TEST_URL="+server.URL)
	if target == "/sn/pool/claim" {
		cmd.Env = append(cmd.Env, "URNETWORK_FINITE_CLAIM_TEST_EPOCH=1")
	}
	output, err := cmd.CombinedOutput()
	if err != nil || targetReads.Load() <= unavailableResponses || posts.Load() != 0 || !strings.Contains(string(output), "status: VERIFIED against the server root only") {
		t.Fatalf("public finite claim abandoned a recoverable GET: target=%s reads=%d posts=%d err=%v output=%s", target, targetReads.Load(), posts.Load(), err, output)
	}
	if target == "/sn/epoch" && poolReads.Load() == 0 || target == "/sn/pool/claim" && epochReads.Load() != 0 {
		t.Fatal("recovery repeated a completed operation", epochReads.Load(), poolReads.Load())
	}
}

func TestFiniteClaimPublicEpochGetRecovers(t *testing.T) {
	testFiniteClaimPublicGetRecovery(t, "/sn/epoch")
}

func TestFiniteClaimPublicPoolGetRecovers(t *testing.T) {
	testFiniteClaimPublicGetRecovery(t, "/sn/pool/claim")
}
