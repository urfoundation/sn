// Command snclaim signs and submits STSubnet claimMiner transactions for UR
// subnet providers (PLAN.md §7.3, settled D-6).
//
// The stdlib-only `provider claim` (connect/provider) fetches the pool claim,
// verifies the Merkle proof against the on-chain root and prints
// ready-to-submit claimMiner calldata plus the contract address and chain id;
// snclaim is the go-ethereum-equipped counterpart that signs and sends it.
// Claims are permissionless — any funded EVM key may relay a claim; the payout
// always goes to the coldkey committed in the Merkle leaf.
package onchain

import (
	"fmt"
	"os"

	docopt "github.com/docopt/docopt-go"
)

// Version is stamped by the Makefile via -ldflags "-X main.Version=...".
var Version = "0.0.0-dev"

const usage = `snclaim - sign and submit STSubnet claimMiner transactions (UR subnet, D-6).

Usage:
    snclaim submit --calldata=<hex> --contract=<addr> --rpc=<url>... --key_file=<path> [--chain_id=<id>] [--gas_limit=<n>] [--dry-run]
    snclaim submit --epoch=<e> --no_id=<n> --coldkey=<key> --share_bps=<n> --proof=<nodes> --contract=<addr> --rpc=<url>... --key_file=<path> [--chain_id=<id>] [--dry-run]
    snclaim status --epoch=<e> --no_id=<n> [--coldkey=<key>] --contract=<addr> --rpc=<url>...
    snclaim bind-head --hotkey=<hex> --client_id=<hex> --sig=<hex> --contract=<addr> --rpc=<url>... --key_file=<path> [--chain_id=<id>] [--gas_limit=<n>] [--dry-run]
    snclaim unbind-head --hotkey=<hex> --contract=<addr> --rpc=<url>... --key_file=<path> [--chain_id=<id>] [--gas_limit=<n>] [--dry-run]
    snclaim -h | --help
    snclaim --version

Options:
    --calldata=<hex>   Ready-to-submit claimMiner calldata as printed by
                       'provider claim' (0x hex). Sent byte-for-byte; the
                       4-byte selector must be claimMiner (0x4c207962).
    --epoch=<e>        Finalized epoch number.
    --no_id=<n>        Network operator (pool) id.
    --coldkey=<key>    Recipient coldkey: ss58 address (Bittensor prefix 42)
                       or 32-byte hex pubkey (0x-optional).
    --hotkey=<hex>     Head-tier miner hotkey: 32-byte hex (0x-optional) or
                       ss58 address (Bittensor prefix 42).
    --client_id=<hex>  Provider client Ed25519 public key (ckey), 32-byte hex,
                       as printed by 'provider bind-head'.
    --sig=<hex>        64-byte Ed25519 client_id signature (R‖S) over
                       headBindDigest, as printed by 'provider bind-head'.
    --share_bps=<n>    Payout share in basis points (1..10000).
    --proof=<nodes>    Comma-separated 32-byte hex Merkle proof nodes;
                       pass '' for a single-leaf tree (empty proof).
    --contract=<addr>  STSubnet proxy contract address (0x...).
    --rpc=<url>        EVM JSON-RPC endpoint; repeatable, tried in order
                       until one answers (failover).
    --key_file=<path>  File holding the hex-encoded 32-byte secp256k1 EVM
                       private key that signs the transaction.
    --chain_id=<id>    Expected chain id; errors if the RPC reports a
                       different one. Fetched via eth_chainId when omitted.
    --gas_limit=<n>    Gas limit override (default: estimated gas + 20%).
    --dry-run          Stop after the eth_call preflight: print the decoded
                       intent and estimated gas, send nothing.
    -h --help          Show this help.
    --version          Show version.
`

// Run is the snclaim CLI entry point (the executable lives at cli/snclaim). It
// signs and broadcasts the on-chain miner actions (claimMiner, bind/unbind head)
// whose calldata the miner builds offline — folded here under sn/miner as the
// go-ethereum-equipped submission counterpart.
func Run(args []string) {
	opts, err := docopt.ParseArgs(usage, args, "snclaim "+Version)
	if err != nil {
		// The default help handler exits on usage errors, so this is only
		// reachable if the parser itself is misconfigured.
		fmt.Fprintln(os.Stderr, "snclaim:", err)
		os.Exit(64)
	}

	var cmdErr error
	switch {
	case boolOpt(opts, "submit"):
		cmdErr = cmdSubmit(opts)
	case boolOpt(opts, "status"):
		cmdErr = cmdStatus(opts)
	case boolOpt(opts, "bind-head"):
		cmdErr = cmdBindHead(opts)
	case boolOpt(opts, "unbind-head"):
		cmdErr = cmdUnbindHead(opts)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(64)
	}
	if cmdErr != nil {
		fmt.Fprintln(os.Stderr, "snclaim:", cmdErr)
		os.Exit(1)
	}
}

func boolOpt(opts docopt.Opts, key string) bool {
	v, _ := opts.Bool(key)
	return v
}

// strOpt returns the option's string value, or "" when absent.
func strOpt(opts docopt.Opts, key string) string {
	if v, ok := opts[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// strsOpt returns a repeatable option's accumulated values.
func strsOpt(opts docopt.Opts, key string) []string {
	if v, ok := opts[key]; ok && v != nil {
		if ss, ok := v.([]string); ok {
			return ss
		}
	}
	return nil
}

// hasOpt reports whether the option was supplied at all (docopt stores nil
// for value options that were not given).
func hasOpt(opts docopt.Opts, key string) bool {
	v, ok := opts[key]
	return ok && v != nil
}
