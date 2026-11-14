# Rao archive unsigned preparation, October 1

The existing production read path completes a fresh combined native/EVM/runtime
observation on `https://archive.chain.opentensor.ai` without a public
`debug_getRawHeader` implementation. An exact clean-source lock, partial release
inventory, release input and bound blocked plan are retained at
`/mnt/data/sn-testnet/mainnet-public-unsigned-preparation-astra-20261001`.
No application code or module pin needed changing. This increment corrects
stale documentation and makes the current unsigned preparation reviewable.

## Exact observation

`sn-mainnet finalized-snapshot --rpc https://archive.chain.opentensor.ai
--retry-window 300s` exited 0. It selected one finalized native identity at
`2026-10-01T07:02:47.145963717Z`; runtime bytes, native header and recovered EVM
RLP all belong to that identity. Expected-network flags were omitted because
independent approval has not been supplied.

| Field | Retained value |
| --- | --- |
| Native chain / EVM chain ID | Bittensor / 964 |
| Genesis | `0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03` |
| Native finalized height / hash | 9,186,298 / `0x31fcfb6485f601c46ababd946755ad1dc792e84928c4a7d1a2070c277b8e95ae` |
| EVM height / hash | 9,186,298 / `0x59619a60759b7f020bceca7e6918c116379190c27aa9d4997968465849aa05b5` |
| Complete runtime tuple | `node-subtensor`, spec 470, transaction 1, state 1 |
| Runtime code Blake2b-256 | `0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47` |
| Runtime metadata Blake2b-256 | `0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34` |
| Combined content seal | `sha256:dabe6829b9445bce6d3ea26aede6da967b2ea67b5b278897087ca0bfeb9f047f` |

Separate read-only probes at that exact EVM hash retain raw-method error
`-32601` and a successful public `eth_getBlockByHash` response. The combined
reader's existing bounded Frontier RLP15 recovery and final canonical checks
pass. The snapshot remains `unapproved_observation`, with
`runtime_source_proven=false` and `finality_authority=owned-rpc-assertion`.
This establishes header commitments; it does not create independent finality
or network/runtime approval. The [runtime470 audit](../../docs/spec/runtime-470-audit.md)
still requires independent disposition of the nonidentical local Wasm rebuild.

## Captured source and dependency identities

Remote main observations were retained at `2026-10-01T07:06:37Z`. Mutable heads
are recorded separately from the exact immutable sources consumed by the build.

| Repository | Observed remote main | Consumed source |
| --- | --- | --- |
| SN | `de0823ce76032909f3d15ea8cec171423cf127a4` | Same commit; clean tree `f33ba7e0671ed3a2d289d30dcf4d7f31d2a740da` |
| Server | `0b8e758db9ce5516de867e1b5d0a1c9660a0880b` | Same commit; clean tree `500391d3919ff8d3301455d7c735aa91cb74af5c` |
| Connect | `aade060f078842b498a8e22ba54b5e139fb402ed` | `e1b5d77b5029a4d8ce75ff8c21c7360b51c66773` |
| SDK | `8f4d101d460716249b503d25f25b2254fd0bfc45` | `5d37be3876e53225740828cb1d0a8c2b11413efd` |

Both SN and server module graphs independently select Connect and its SCTP fork
at `v0.0.0-20261001021459-e1b5d77b5029`, and SDK at
`v0.0.0-20261001021058-5d37be3876e5`, with identical module/body sums. The release
builder's current admission checks agree. Both consumed commits are ancestors
of the recorded main heads. Newer Connect changes cover UDP portability and
IP lists; newer SDK changes cover network-space migration and a JS dependency.
They were not repinned or qualified by this preparation.

`source-context.json` retains full module sums, repository commits/trees,
module-file hashes, exact Go module graphs, remote observations, unconsumed
commit lists and the tool's build-info hash. `source-lock.json` keeps its honest
`clean-git-sources-and-local-go-replacements-only` scope. Versioned dependency
identity comes from the separately retained effective graphs.

The readonly offline `go build -trimpath ./mainnet` used Go 1.26.6 with
`GOWORK=off`, `GOFLAGS=-mod=readonly`, `GOPROXY=off` and `GOSUMDB=off`.
The resulting 51,109,440-byte tool has SHA256
`fb26731a46378f26b58701ba0b26edcb31a5ceddc11fdd51277b984a1bf39dbc`.
Its build info records toolchain/dependency settings but omits VCS fields under
the default build mode. The clean revision comes from the separately captured
source lock and build invocation; this partial bundle does not prove build
provenance. The complete release builder explicitly uses `-buildvcs=true` and
requires the exact clean revision. A preliminary invocation from the workspace
parent failed before compilation because no `go.mod` was present there; its log
is retained separately and the corrected build passed.

The source capture worktree remains clean at the recorded commit under
`/home/by/urnetwork/temp/sn-mainnet-public-unsigned-preparation-astra-20261001/sn`,
with the exact server in its sibling `server`. This documentation is committed
from the separate sibling `sn-docs`; it does not relabel the retained binary
or source lock as a successor Git commit.

## Exact bundle and blocked admissions

All paths below are relative to the retained evidence directory. Hashes are
SHA256 of the complete file bytes, including final newlines, rather than
internal domain-separated content seals.

| File | SHA256 |
| --- | --- |
| `finalized-snapshot.json` | `c817e45c96bc48b214aabdeff0d1b152a093dd3ad6ea489e7c7747826c928ad8` |
| `source-lock.json` | `edc724dd64c9b81c43ef677501b46d5d38cdea1dbc9df7fcd0fd01770329e8bf` |
| `source-context.json` | `b4290a1925cbd76a848335a0897d07e0a6db0eac488160cd8c0de7671e7c8ec1` |
| `release-inventory.json` | `3617db03233e57c45f6f611a86c52e3b904c39eb33968c2b9ba6ff6869a73cad` |
| `release-input.json` | `853c0d904f659c5be92d48a400c8ed3c40a4adf5674c8ed3aea7c1fbd6c5ac14` |
| `plan-config.json` | `d7be9a522ca8bd4584f43214ab9e338d2b015d3cb88a1d8431c81e9ad296de69` |
| `blocked-plan.json` | `0a054e766c3f073d9a1fbd49f61b073dceb7d481086a4affdf86ba5e22d5c6e1` |

The source-lock content seal is
`0xbfe1e95b35abd51e0cd399671d82159878cbc76edf70b60f813f5e8c0442b794`.
The blocked-plan content seal is
`sha256:70ea49ce5b6999052975c1b9fe87ff76bf022a792857dc0e3da4be1f469d0003`.

The plan configuration separately declares the repository's intended
Bittensor/EVM964 mainnet target. It is a review expectation, not an independent
approval inferred from this snapshot. Every one of ten actions is `blocked`
and `executable=false`; `apply_authority=false` and `activation_ready=false`.
The 30 requirements comprise 28 `missing` and two `supplied_unvalidated`:
`binary-artifacts` supplies the partial inventory and `production-qualification`
supplies source context. `owned-rpc` and `runtime-authority` remain missing.

The inventory hashes five selected files: the new mainnet tool, both module
graphs, source context and tool build info. Contract, config, policy, migration
and image-identity categories remain missing. Release completion, provenance
and deployment approval stay false. The earlier eight-image aggregate belongs
to SN `2d53e6f2` / server `ecbf3aad`; it is preserved without lending its local
source-to-image claim to these successor sources. A full current release build,
complete role/artifact qualification, approved reproducibility or exact-artifact
exceptions, independent authority, custody and live deployment remain open.

## Recheck and validation

From the evidence directory, the retained binary can recheck the captured clean
sources and inventory, then reproduce the offline plan:

```sh
GOWORK=off ./bin/sn-mainnet source-lock \
  --sn-dir /home/by/urnetwork/temp/sn-mainnet-public-unsigned-preparation-astra-20261001/sn
GOWORK=off ./bin/sn-mainnet release-inventory --config "$PWD/inventory-config.json"
./bin/sn-mainnet plan --config "$PWD/plan-config.json"
sha256sum -c SHA256SUMS
```

All three commands exited 0. Two offline plan executions produced byte-identical
output. The local `bundle-audit.json` checks exact file references, source and
snapshot bindings, every blocked action, missing authority, partial inventory
flags and the complete test process/package/root results. It is an author
audit, not independent qualification.

Fifty-two existing mainnet roots covering public mapping, combined snapshots,
source-lock refusal, inventory and planning passed normal (26.640 seconds) and
race (124.235 seconds), with package PASS and no skipped roots. Four release
builder source-pin/fork identity roots also passed normal/race. `go vet
./mainnet ./scripts/mainnet-release-build` passed. No production failure was
found and no new implementation or regression test was needed. The retained
logs do not claim full-package or whole-release qualification.

No signing key was read, transaction submitted, service deployed, or image
published. MG-01 and MG-02 remain open for the independent and complete release
evidence above.
