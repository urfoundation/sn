//go:build runtime_rehearsal

package rehearsal

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/evmrpc"
)

const (
	// The rehearsal runs only on a runtime whose source was reviewed (§14 gate
	// 2): crv4.ReviewedNativeOwnerSpecSource names it for the node's version.
	specName = "node-subtensor"
	// run-node.sh writes mainnet's EVM chain ID into genesis.
	evmChainID = 964
)

// node is one rehearsal chain. Blocks are sealed on demand, one per submitted
// transaction, and finalized at once. staking and neuron are the precompile
// interfaces the contracts compile against (evm/src/interfaces). http is the
// EVM JSON-RPC route that the eth client and sn-mainnet read.
type node struct {
	ctx     context.Context
	sub     *crv4.Chain
	eth     *ethclient.Client
	http    string
	staking artifact
	neuron  artifact
}

// startNode runs scripts/runtime-rehearsal/run-node.sh on free ports in a
// temporary directory and stops the node when the test ends.
func startNode(t *testing.T) *node {
	t.Helper()
	binary := os.Getenv("RUNTIME_REHEARSAL_NODE")
	if binary == "" {
		t.Fatal("RUNTIME_REHEARSAL_NODE must name the node-subtensor binary built by scripts/runtime-rehearsal/build.sh")
	}
	launcher, err := filepath.Abs(filepath.Join("..", "scripts", "runtime-rehearsal", "run-node.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "node.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	rpcPort := freePort(t)
	cmd := exec.Command(launcher)
	cmd.Env = append(os.Environ(),
		"RUNTIME_REHEARSAL_NODE="+binary,
		"RUNTIME_REHEARSAL_BASE_PATH="+filepath.Join(dir, "chain"),
		"RUNTIME_REHEARSAL_RPC_PORT="+strconv.Itoa(rpcPort),
		"RUNTIME_REHEARSAL_P2P_PORT="+strconv.Itoa(freePort(t)),
	)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var exitErr error
	exited := make(chan struct{})
	go func() {
		exitErr = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		_ = log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(logPath)
			if len(raw) > 8192 {
				raw = raw[len(raw)-8192:]
			}
			t.Logf("node log tail:\n%s", bytes.TrimSpace(raw))
		}
	})

	n := &node{
		ctx:     t.Context(),
		staking: forgeArtifact(t, "src/interfaces/stakingV2.sol", "IStaking"),
		neuron:  forgeArtifact(t, "src/interfaces/neuron.sol", "INeuron"),
	}
	endpoint := "127.0.0.1:" + strconv.Itoa(rpcPort)
	deadline := time.Now().Add(2 * time.Minute)
	for {
		select {
		case <-exited:
			t.Fatalf("node exited before serving RPC: %v", exitErr)
		default:
		}
		if n.sub, err = crv4.DialChainContext(n.ctx, "ws://"+endpoint); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node RPC at %s not ready: %v", endpoint, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Cleanup(n.sub.API.Client.Close)
	source, reviewed := crv4.ReviewedNativeOwnerSpecSource(uint32(n.sub.Runtime.SpecVersion))
	if n.sub.Runtime.SpecName != specName || !reviewed {
		t.Fatalf("node runs %s/%d, which has no reviewed source", n.sub.Runtime.SpecName, n.sub.Runtime.SpecVersion)
	}
	t.Logf("node runs %s/%d, reviewed source %s", specName, n.sub.Runtime.SpecVersion, source)
	n.http = "http://" + endpoint
	if n.eth, err = evmrpc.DialContext(n.ctx, n.http); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.eth.Close)
	chainID, err := n.eth.ChainID(n.ctx)
	if err != nil || chainID.Uint64() != evmChainID {
		t.Fatalf("EVM chain ID %v (%v), want %d", chainID, err, evmChainID)
	}
	return n
}

// freePort reserves a loopback TCP port for the node.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// seal produces and finalizes one block holding every pending transaction.
func (self *node) seal(t *testing.T) types.Hash {
	t.Helper()
	var created struct {
		Hash string `json:"hash"`
	}
	if err := self.sub.API.Client.CallContext(self.ctx, &created, "engine_createBlock", true, true, nil); err != nil {
		t.Fatalf("seal block: %v", err)
	}
	hash, err := types.NewHashFromHexString(created.Hash)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// advance seals empty blocks, for example to cross a tempo or a window.
func (self *node) advance(t *testing.T, blocks int) {
	t.Helper()
	for range blocks {
		self.seal(t)
	}
}

// head returns the latest block, which is also finalized.
func (self *node) head(t *testing.T) types.Hash {
	t.Helper()
	hash, err := crv4.FinalizedHeadContext(self.ctx, self.sub)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// number returns the latest block's number.
func (self *node) number(t *testing.T) uint64 {
	t.Helper()
	header, err := self.sub.HeaderAtContext(self.ctx, self.head(t))
	if err != nil {
		t.Fatal(err)
	}
	return uint64(header.Number)
}

// sealUntil seals empty blocks until the latest block is `block`, so that
// the next transaction executes in block + 1.
func (self *node) sealUntil(t *testing.T, block uint64) {
	t.Helper()
	current := self.number(t)
	if current > block {
		t.Fatalf("block %d has passed (now %d)", block, current)
	}
	self.advance(t, int(block-current))
}
