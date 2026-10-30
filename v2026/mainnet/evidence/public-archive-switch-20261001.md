# Interim Rao Foundation mainnet RPC, October 1

At the user's direction, mainnet preparation now uses the Rao Foundation
public archive RPC at `https://archive.chain.opentensor.ai` until Snow finishes
syncing. The [official network guide](https://preview.bittensor.com/docs/concepts/network)
lists the public archive and distinguishes it from the lite endpoint for
historical reads. This route selection is not a signed-chain approval.

The [read-only route census](public-rpc-route-20261001.json) queried archive,
lite and entrypoint over HTTPS. Each returned `Bittensor`, genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
EVM chain ID `0x3c4` (964), and the same finalized hash in that sample. The
archive returned runtime spec 393 for historical block 8,000,000, confirming
that this tested historical runtime method is available. These are observations
from Rao-operated endpoints, not three independent chain authorities.

The project's read-only `inspect` command succeeded against the archive at
01:21 UTC. Its [raw JSON](/mnt/data/sn-testnet/mainnet-public-rpc-20261001/archive-inspect.json)
has SHA256 `220daff5419f6d13b9b8a10e93759a10e1e21cc6b06dcc62e0c6585805c8cb23`;
it observed finalized block 9,184,591, node version
`4.0.0-dev-4b19dc31fc2`, and runtime spec 470. The separate
[`runtime-snapshot` capture](/mnt/data/sn-testnet/mainnet-public-rpc-20261001/archive-runtime-snapshot.json)
also succeeded, SHA256
`423afcbad084c3d89bf0788c2d152b82ea363014ee8d52c4b837fb1a076c7a76`,
at finalized block 9,184,596. It retained exact runtime code and metadata
bytes with their declared hashes and `unapproved_observation` status. These
two captures selected different finalized blocks and are not joined.
The observed spec 470 is newer than the repository's highest reviewed runtime
entry, spec 467. Its observed code hash is
`0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`;
metadata hash is
`0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`.
This observation does not approve a new runtime or its consumed operations.

The stricter `finalized-snapshot` command returned exit 4 with no output:
`debug_getRawHeader` is not exposed (`-32601 Method not found`). The [error
record](/mnt/data/sn-testnet/mainnet-public-rpc-20261001/archive-finalized-snapshot.err)
and direct probes show the same missing method at archive, lite and entrypoint.
Native/EVM header linkage and any historical capture requiring that raw debug
method remain open until an exact independently qualified public-RPC fallback
or a capable synced Snow route exists. No mutating bootstrap action has run.

The subsequent [public header fallback qualification](public-header-fallback-20261001.md)
closes this specific combined finalized-snapshot read on the public archive. It
reconstructs only the reviewed Frontier RLP15 header and accepts it only when
its Keccak hash equals the native committed EVM digest. The independent live
read-only snapshot succeeded. This does not add the raw-history methods needed
by the Safe history collector or approve runtime 470 for transactions.

The [advertised method catalogue](public-archive-methods-20261001.json)
includes `author_submitExtrinsic`, `eth_sendRawTransaction`,
`eth_getBlockByHash` and `state_getReadProof`, but omits
`debug_getRawHeader`. Advertisement is not a successful submission or proof
that the method will accept this launch's payloads.

Use `https://archive.chain.opentensor.ai` for current read-only identity,
historical prerequisites, unsigned planning and later explicitly approved
submissions that its method profile supports. The URL must be pinned into each
new reviewed plan; already signed plan bytes must not be silently retargeted.
Keep retry budgets and finite public-RPC concurrency; the no-limit LAN policy
does not apply to this public service. Before any transaction, independently
approve the observed mainnet genesis/runtime/source identity and recheck the
same route's current finalized state. Snow remains a future failover, not a
source of current mainnet evidence.
