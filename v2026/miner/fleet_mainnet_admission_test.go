// Fleet production commands must refuse missing independent authority before
// key loading, local custody creation or any online operation.
package miner

import (
	"context"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Missing approval is refused at every online command entry before other work.
func TestFleetMainnetCommandsRequireAuthorityBeforeSideEffects(t *testing.T) {
	manifest := &protocol.FleetManifest{ChainID: 964, Netuid: 25, Coordinator: [20]byte{1}}
	for _, command := range []struct {
		name string
		run  func(context.Context, docopt.Opts, *protocol.FleetManifest) error
	}{
		{name: "register", run: fleetRegister}, {name: "publish", run: fleetPublish},
		{name: "bind", run: fleetBind}, {name: "status", run: fleetStatus}, {name: "revoke", run: fleetRevoke},
	} {
		err := command.run(t.Context(), docopt.Opts{}, manifest)
		if err == nil || !strings.Contains(err.Error(), "mainnet runtime authority") {
			t.Errorf("%s reached another operation before refusing missing mainnet runtime authority: %v", command.name, err)
		}
	}
}

// A change after durable preparation cannot authorize broadcast.
func TestFleetMainnetEvmUpgradeAfterPreparedRetainsBytesWithoutSend(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	key, err := onchain.LoadKeyFile(fleetOpt(fixture.opts, "--relayer_key_file"))
	if err != nil {
		t.Fatal(err)
	}
	var prepared []byte
	_, err = onchain.SubmitWithHooks(t.Context(), onchain.SubmitParams{Contract: common.Address(fixture.manifest.Coordinator), Rpcs: []string{fixture.server.URL}, Key: key, Calldata: []byte{1, 2, 3, 4}, RuntimeAdmission: fixture.authority.evmAdmission()}, onchain.SubmitHooks{Prepared: func(_ common.Hash, raw []byte) error {
		prepared = append([]byte(nil), raw...)
		fixture.stateLock.Lock()
		fixture.version.SpecVersion++
		fixture.stateLock.Unlock()
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "runtime admission before EVM broadcast") || len(prepared) == 0 {
		t.Fatalf("prepared boundary: bytes=%d err=%v", len(prepared), err)
	}
	if fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("prepared runtime drift broadcast a transaction")
	}
}

// Known inclusion remains available even when its runtime is unapproved.
func TestFleetMainnetEvmReceiptIsReturnedWithFailedRuntimeAdmission(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	key, err := onchain.LoadKeyFile(fleetOpt(fixture.opts, "--relayer_key_file"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "eth_sendRawTransaction" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	receipt, err := onchain.Submit(t.Context(), onchain.SubmitParams{Contract: common.Address(fixture.manifest.Coordinator), Rpcs: []string{fixture.server.URL}, Key: key, Calldata: []byte{1, 2, 3, 4}, RuntimeAdmission: fixture.authority.evmAdmission()})
	if err == nil || receipt == nil || receipt.BlockNumber.Uint64() != 102 || receipt.TxHash == (common.Hash{}) {
		t.Fatalf("lost known inclusion after failed runtime admission: %+v %v", receipt, err)
	}
	if fixture.count("eth_sendRawTransaction") != 1 {
		t.Fatal("failed admission duplicated broadcast")
	}
}

// Cancellation cannot fall through to a contract call or transaction.
func TestFleetMainnetRuntimeCancellationNeverReachesSigning(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.authority.prepareEvm(ctx, []string{fixture.server.URL}); err == nil {
		t.Fatal("canceled operation admitted production signing")
	}
	if fixture.count("eth_call") != 0 || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("canceled admission reached contract operations")
	}
}

// Each command grammar preserves the independently supplied document digest.
func TestFleetMainnetCliRetainsIndependentAuthorityFlags(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	for _, command := range [][]string{
		{"register", "--hotkey_seed_file=hotkey.seed", "--coldkey_seed_file=coldkey.seed", "--substrate=wss://native.example"},
		{"publish", "--hotkey_seed_file=hotkey.seed", "--substrate=wss://native.example"},
		{"bind", "--client_id=01", "--client_seed_file=client.seed", "--hotkey_seed_file=hotkey.seed", "--valid_from_epoch=1", "--valid_to_epoch=2", "--rpc=https://evm.example", "--relayer_key_file=relayer.seed"},
		{"status", "--client_id=01", "--substrate=wss://native.example", "--rpc=https://evm.example"},
		{"revoke", "--client_id=01", "--client_seed_file=client.seed", "--effective_epoch=2", "--rpc=https://evm.example", "--relayer_key_file=relayer.seed"},
	} {
		args := append([]string{"fleet"}, command...)
		args = append(args, "--manifest=fleet.json", "--mainnet-runtime-authority=reviewed.json", "--mainnet-runtime-authority-sha256="+strings.Repeat("1", 64))
		opts, err := parser.ParseArgs(mainUsage(), args, "test")
		if err != nil || fleetOpt(opts, "--mainnet-runtime-authority") != "reviewed.json" || fleetOpt(opts, "--mainnet-runtime-authority-sha256") != strings.Repeat("1", 64) {
			t.Errorf("%s lost explicit production authority flags: %v", command[0], err)
		}
	}
}

// Mainnet dry-run goes through the public command, exact metadata signing and
// native fee quote; a testnet release pin cannot satisfy this synthetic tuple.
func TestFleetMainnetRegisterCommandSignsApprovedRuntime(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	if err := fleetRegister(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fixture.count("payment_queryInfo") != 1 || fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("registration did not quote exactly one signed dry run")
	}
	fixture.stateLock.Lock()
	signed := fixture.nativeSigned
	fixture.stateLock.Unlock()
	if signed == "" {
		t.Fatal("registration did not encode a signed transaction")
	}
}

// The binding handler authenticates its actual target before consent and preflight.
func TestFleetMainnetBindCommandRequiresActualEvmRuntime(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	if err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fixture.count("eth_call") != 1 || fixture.count("eth_estimateGas") != 1 || fixture.count("eth_sendRawTransaction") != 0 || fixture.count("state_getStorageHash") < 2 {
		t.Fatal("binding lost its mainnet admission or dry-run boundary")
	}
}

// The revocation handler verifies a finalized digest against its local domain.
func TestFleetMainnetRevokeCommandChecksLocalSigningDomain(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	if err := fleetRevoke(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fixture.count("eth_call") != 2 || fixture.count("eth_estimateGas") != 1 || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("revocation lost its finalized digest/preflight boundary")
	}
}

// Both sides of status require runtime authority on their own connections.
func TestFleetMainnetStatusCommandReadsApprovedNativeAndEvmState(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	if err := fleetStatus(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fixture.count("state_getStorage") != 2 || fixture.count("eth_call") != 1 || fixture.count("state_getStorageHash") < 4 {
		t.Fatal("status bypassed native or EVM runtime authority")
	}
}

// A correct EVM id cannot conceal a foreign native genesis.
func TestFleetMainnetBindRejectsWrongActualEvmGenesisBeforeKeys(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--client_seed_file"] = "missing-synthetic.seed"
	fixture.stateLock.Lock()
	fixture.genesis[0]++
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "fresh network identity differs") {
		t.Fatalf("wrong EVM native genesis was not refused before client seed: %v", err)
	}
	if fixture.count("eth_call") != 0 || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("wrong-chain endpoint reached contract operations")
	}
}

// A reached wrong chain is refused instead of searching for a favorable reply.
func TestFleetMainnetEvmChainMismatchNeverFallsBack(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	alternate := newFleetMainnetTestFixture(t)
	t.Setenv("URNETWORK_STATE_DIR", fixture.durable.Roots[0])
	fixture.opts["--rpc"] = []string{fixture.server.URL, alternate.server.URL}
	fixture.stateLock.Lock()
	fixture.evmChainId = 945
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "chain id differs") {
		t.Fatalf("wrong EVM chain admitted: %v", err)
	}
	if alternate.count("eth_chainId") != 0 || fixture.count("eth_call") != 0 {
		t.Fatal("chain mismatch fell through to another authority")
	}
}

// The gas-price reply is an explicit barrier before signing admission.
func TestFleetMainnetEvmUpgradeBeforeSigningDoesNotSend(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "eth_gasPrice" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "runtime admission before EVM signing") {
		t.Fatalf("upgrade before signing: %v", err)
	}
	if fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatal("runtime drift reached broadcast")
	}
}

// A post-send upgrade leaves one known submission and an admission error.
func TestFleetMainnetEvmUpgradeAtReceiptNeverRebroadcasts(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--dry-run"] = false
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "eth_sendRawTransaction" {
			fixture.version.SpecVersion++
		}
	}
	fixture.stateLock.Unlock()
	if err := fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest); err == nil || !strings.Contains(err.Error(), "runtime admission at EVM receipt") {
		t.Fatalf("included runtime drift: %v", err)
	}
	if fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_getTransactionReceipt") != 1 {
		t.Fatal("receipt refusal retried an uncertain transaction")
	}
}
