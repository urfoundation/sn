// Command snclaim signs and submits settlement-vault claim transactions for UR
// subnet providers (PLAN.md §7.3, settled D-6).
//
// The stdlib-only `provider claim` (sn/miner) fetches the pool claim, verifies
// the Merkle proof against the on-chain root and prints ready-to-submit
// STSettlementVault.claim calldata plus the vault address and chain id;
// snclaim is the go-ethereum-equipped counterpart that signs and sends it.
// Claims are permissionless — any funded EVM key may relay a claim; the payout
// always goes to the coldkey committed in the Merkle leaf. Head-tier fleet
// registration and bindings are `provider fleet` (register_limit on the native
// chain, then the coordinator's dual-signed fleet bindings); snclaim has no
// head-binding commands.
package onchain

import (
	"fmt"
	"os"

	docopt "github.com/docopt/docopt-go"
)

// Version is stamped by the Makefile via -ldflags "-X main.Version=...".
var Version = "0.0.0-dev"

const usage = `snclaim - sign and submit settlement-vault claim transactions (UR subnet, D-6).

Usage:
    snclaim submit --calldata=<hex> --contract=<addr> --rpc=<url>... --key_file=<path> [--chain_id=<id>] [--gas_limit=<n>] [--dry-run]
    snclaim submit --epoch=<e> --no_id=<n> --coldkey=<key> --share_bps=<n> --proof=<nodes> --contract=<addr> --rpc=<url>... --key_file=<path> [--chain_id=<id>] [--dry-run]
    snclaim status --epoch=<e> --no_id=<n> [--coldkey=<key>] --contract=<addr> --rpc=<url>...
    snclaim -h | --help
    snclaim --version

Options:
    --calldata=<hex>   Ready-to-submit claim calldata as printed by
                       'provider claim' (0x hex). Sent byte-for-byte; the
                       4-byte selector must be the settlement vault's
                       claim(uint256,uint256,bytes32,uint256,bytes32[])
                       (0xce479a1b).
    --epoch=<e>        Finalized epoch number.
    --no_id=<n>        Network operator (pool) id.
    --coldkey=<key>    Recipient coldkey: ss58 address (Bittensor prefix 42)
                       or 32-byte hex pubkey (0x-optional).
    --share_bps=<n>    Payout share in basis points (1..10000).
    --proof=<nodes>    Comma-separated 32-byte hex Merkle proof nodes;
                       pass '' for a single-leaf tree (empty proof).
    --contract=<addr>  STSettlementVault address (0x...), printed by
                       'provider claim' as 'contract' and served by the
                       operator at GET /sn/epoch as settlement_vault_address.
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
// signs and broadcasts the settlement-vault claim whose calldata the miner
// builds offline — folded here under sn/miner as the go-ethereum-equipped
// submission counterpart.
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
