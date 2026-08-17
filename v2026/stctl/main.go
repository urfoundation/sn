// stctl is the subtensor ops CLI for the UR subnet: runtime operations and
// inspection for the STSubnet settlement contract (PLAN.md §2), talking to
// the subtensor EVM json-rpc through the generated sn/stabi bindings.
//
// Deployment itself stays in `forge script` (see sn/evm); stctl covers the
// runtime surface: initialize (for a proxy deployed uninitialized), operator
// registration, deposits (push-then-credit buybacks), payout-root commits,
// finalize, miner claims, and epoch/state inspection, plus the H160 -> SS58
// mirror funding helper (PLAN.md §3.6).
package main

import (
	"fmt"
	"os"

	"github.com/docopt/docopt-go"
)

// Version is set via the linker, e.g.
// -ldflags "-X main.Version=$(STCTL_VERSION)" (see stctl/Makefile).
var Version string

// verbosity is the count of -v flags (0 = quiet).
var verbosity int

// mainUsage returns the docopt usage string. Package-level (rather than
// inline in `main`) so tests can parse argv against the real usage.
func mainUsage() string {
	return fmt.Sprintf(
		`UR subtensor ops CLI: runtime ops + inspection for the STSubnet contract.

Reads a YAML config (default %s).
When the config file is missing, "stctl deploy-status" prints a commented
example to copy from. Amounts are rao (1 alpha = 1e9 rao).

Usage:
    stctl deploy-status [--config=<path>] [-v...]
    stctl initialize --owner=<h160> --treasury_hotkey=<key> --reserve_hotkey=<key>
        [--guardian=<h160>] [--t_epoch=<blocks>] [--commit_window=<blocks>]
        [--trails_window=<blocks>] [--finalize_offset=<blocks>] [--self_coldkey=<key>]
        [--config=<path>] [-v...]
    stctl register-operator --no_id=<id> --coldkey=<key> --miner_hotkey=<key>
        [--config=<path>] [-v...]
    stctl deposit --no_id=<id> --alpha=<rao> [--push]
        [--config=<path>] [-v...]
    stctl commit-root --epoch=<e> --no_id=<id> --root=<hex32> [--off=<hex>]
        [--config=<path>] [-v...]
    stctl finalize --epoch=<e> [--config=<path>] [-v...]
    stctl claim-miner --epoch=<e> --no_id=<id> --coldkey=<key> --share_bps=<n> --proof=<proof>
        [--force] [--config=<path>] [-v...]
    stctl epoch [--config=<path>] [-v...]
    stctl state [--epoch=<e>] [--no_id=<id>] [--config=<path>] [-v...]
    stctl evm-address <h160>

Options:
    -h --help              Show this help and exit.
    --version              Show version.
    -v...                  Enable verbose mode. -v implies verbose level 1,
                           -vv implies level 2... etc.
    --config=<path>        Config file path [default: %s].
    --owner=<h160>         Contract owner (admin authority), a 0x-hex H160.
    --guardian=<h160>      Pause-only guardian H160; omit for none (zero).
    --treasury_hotkey=<key>
                           Claims-escrow custody hotkey: ss58 address
                           (prefix 42) or 32-byte hex.
    --reserve_hotkey=<key>
                           Buyback-reserve hotkey, the owner-validator hotkey
                           every deposit is staked to (WHITEPAPER §7.4, D23):
                           ss58 address (prefix 42) or 32-byte hex. Must
                           differ from --treasury_hotkey; set once, no setter.
    --t_epoch=<blocks>     Epoch length in blocks. Profile default 50400
                           mainnet, 300 testnet.
    --commit_window=<blocks>
                           Commit window after close(e). Profile default 1200
                           mainnet, 50 testnet.
    --trails_window=<blocks>
                           Trails window after close(e); a reserved dial for
                           the deferred effort-bounty phase (gates nothing in
                           v1). Profile default 7200 mainnet, 100 testnet.
    --finalize_offset=<blocks>
                           finalizeEpoch opens at close(e)+offset. Profile
                           default 14400 mainnet, 150 testnet.
    --self_coldkey=<key>   mirror(proxy) bytes32 override; omit to compute
                           on-chain via blake2f (0x09).
    --no_id=<id>           Network operator (pool) id, a uint256 decimal.
    --epoch=<e>            Epoch index, a uint256 decimal.
    --alpha=<rao>          Alpha amount in rao (1 alpha = 1e9 rao), decimal.
    --coldkey=<key>        Account key: ss58 address (prefix 42) or 32-byte hex.
    --miner_hotkey=<key>   Pool miner hotkey: ss58 address (prefix 42) or 32-byte hex.
    --root=<hex32>         32-byte merkle root, hex (0x-optional).
    --off=<hex>            Off-chain payload pointer bytes, hex (0x-optional).
                           Defaults to empty.
    --share_bps=<n>        Miner share of the pool in basis points (0..10000).
    --proof=<proof>        Merkle proof: comma-separated 32-byte hex nodes
                           (0x-optional). Empty string for a single-leaf tree.
    --force                Skip the local merkle proof pre-verification.
    --push                 Before deposit(), push the stake to the contract via
                           the StakingV2 precompile (0x805) transferStake, per
                           the contract's push-then-credit flow.
    <h160>                 An EVM address (20-byte hex) to convert to its
                           substrate mirror (fund this ss58 via btcli).`,
		defaultConfigPath(),
		defaultConfigPath(),
	)
}

func main() {
	opts, err := docopt.ParseArgs(mainUsage(), os.Args[1:], RequireVersion())
	if err != nil {
		panic(err)
	}

	// docopt stores the -v... count as an int; Opts.Int only handles strings
	if count, ok := opts["-v"].(int); ok {
		verbosity = count
	}

	run := func(cmd func(docopt.Opts) error) {
		if err := cmd(opts); err != nil {
			fmt.Fprintf(os.Stderr, "stctl: error: %v\n", err)
			os.Exit(1)
		}
	}

	if b, _ := opts.Bool("deploy-status"); b {
		run(cmdDeployStatus)
	} else if b, _ := opts.Bool("initialize"); b {
		run(cmdInitialize)
	} else if b, _ := opts.Bool("register-operator"); b {
		run(cmdRegisterOperator)
	} else if b, _ := opts.Bool("deposit"); b {
		run(cmdDeposit)
	} else if b, _ := opts.Bool("commit-root"); b {
		run(cmdCommitRoot)
	} else if b, _ := opts.Bool("finalize"); b {
		run(cmdFinalize)
	} else if b, _ := opts.Bool("claim-miner"); b {
		run(cmdClaimMiner)
	} else if b, _ := opts.Bool("epoch"); b {
		run(cmdEpoch)
	} else if b, _ := opts.Bool("state"); b {
		run(cmdState)
	} else if b, _ := opts.Bool("evm-address"); b {
		run(cmdEvmAddress)
	}
}

// RequireVersion mirrors the provider CLI convention: the linker-set Version,
// overridable with the STCTL_VERSION environment variable.
func RequireVersion() string {
	if version := os.Getenv("STCTL_VERSION"); version != "" {
		return version
	}
	return Version
}
