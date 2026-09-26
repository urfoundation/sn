# Runtime 458 adoption

The owned archive reported `node-subtensor/458/1/1` at finalized block
8,006,567, hash
`0xc814242904668bad31b388b36ed31a0c1ffd3b180b173727f5e3d20ea5c8aba4`.
The chain remains testnet 945 with genesis
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`.
The observation and all three artifact reads used only the approved owned LAN
endpoint, without pacing or public fallback. The manifest records this endpoint
on the 458 entry as `observation_rpc_url`; its top-level public endpoint retains
the original 451–455 provenance. It does not describe a fresh public comparison.

The exact observed code is 2,519,192 bytes, SHA256
`d763c0210bbd113c065a4e8d538cdd3f5e9b40ba259a5136b77e0a495c364241`,
BLAKE2b256
`2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a`.
The latter equals `state_getStorageHash(0x3a636f6465, finalized_hash)`.
The exact version 14 metadata is 335,297 bytes, SHA256
`17ebfa2551978a1567da990ac9650e6f01802f578696c552a7c6f9f4b1391405`,
BLAKE2b256
`040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d`.
The unchanged storage-free Wasm probe, pinned to Polkadot SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`, reproduced this complete tuple from
these exact Wasm bytes. That offline execution made no blockchain request.
Fresh full release gates still have to verify their own final inputs.

## Source linkage and build mismatch

The reviewed source is the exact commit
[`a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7`](https://github.com/RaoFoundation/subtensor/commit/a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7).
GitHub's [runtime 458 artifact record](https://api.github.com/repos/RaoFoundation/subtensor/actions/artifacts/10356621595)
links artifact 10356621595 to
[workflow 34863910486](https://github.com/RaoFoundation/subtensor/actions/runs/34863910486)
and that commit. The downloaded ZIP was verified against the API's SHA256
`8c6ea303f56d7afb07ef34c7da7b081b88232b5a212ab1d6f33adad4bddf4972`
before extraction. The included srtool digest identifies the production
metadata-hash build, srtool 0.18.3 and Rust 1.89.0. This is commit provenance;
no runtime 458 tag or mainnet multisig proposal is asserted.

The CI Wasm is **not byte-identical** to the chain Wasm. Its compressed size is
2,518,923 bytes, SHA256
`94e85d3d0ca077a8a8f8e1e65edfe18036895a20313f7195bb17060b145610c6`,
BLAKE2b256
`3708442dc6aae2ea654d827d8b9985d36b6640b2447cfd48125a1a0205c8f1d3`.
After decompression, all sections except code are identical. Every function
body is identical except function 1785,
`wasmi_collections::hash::RandomStateImpl::default`. Its local declarations
and 1,228 instructions agree except 22 `i64.const` operands. Every other opcode
and operand agrees. The respective decompressed sizes are 10,316,191 and
10,316,194 bytes.

This localized difference is consistent with the pinned build dependencies'
compile-time random seed path. The reviewed
[Cargo.lock](https://github.com/RaoFoundation/subtensor/blob/a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7/Cargo.lock)
pins `wasmi_collections0.32.3`, whose no-std default calls `ahash0.8.12`'s
`RandomState::new`. The pinned `lencode1.2.1` enables ahash's
`compile-time-rng` feature. Its `const-random0.1.18` /
`const-random-macro0.1.16` expansion uses `CONST_RANDOM_SEED` when supplied,
otherwise host randomness at compile time. Exact registry checksums are:

| Crate | Cargo.lock checksum |
|---|---|
| wasmi_collections0.32.3 | `9c128c039340ffd50d4195c3f8ce31aac357f06804cfc494c8b9508d4b30dca4` |
| ahash0.8.12 | `5a15f179cd60c4584b8a8c596927aadc462e27f2ca70c04e0071964a73ba7a75` |
| lencode1.2.1 | `333212e14789d230ad297599d2dbf3911cd80d9d70a6a8261bbc81a0f3a89bea` |
| const-random0.1.18 | `87e00182fe74b066627d63b85fd550ac2998d4b0bd86bfed477a0ae4c7c71359` |
| const-random-macro0.1.16 | `f9d839f2a20b0aee515dc581a6172f2321f96cab76c1a38a4c584a194955390e` |

Relevant exact crate sources are `wasmi_collections/src/hash.rs`,
`lencode/Cargo.toml`'s ahash dependency, `ahash/src/random_state.rs`'s
`get_fixed_seeds` and `new`, and `const-random-macro/src/span.rs`'s `get_seed`.
This bounded comparison explains the observed difference; it does not claim
reproducible byte identity or authorize arbitrary seed variants. Current
admission pins only the exact LAN code and metadata. A regression explicitly
rejects the official CI code hash and compressed SHA256 as current authority.
There is no hash normalization in the checker or clients.

## Native compatibility and adjacent review

The source comparison is 455 commit
`67dcf7f791dc495064c293f080a0702cb433e51e` to the exact 458 commit above.
The [458 source manifest](runtime-v458-source.sha256) contains 62 paths: all 33
previously reviewed 455 paths plus every changed path in this range. All 40
changed files are included. The 454 29-path and 455 33-path manifests remain
unchanged. The metadata static-source manifest adds the same three exact
source/metadata-generation paths for458; it now covers 18 rows across six
reviewed artifacts. Cargo.lock and the Polkadot SDK revision are unchanged.

- `precompiles/src` is unchanged, including address mapping at 0x080c, native
  staking, commitment, uid, weights and balance-transfer routes. No Solidity
  code, probe bytecode, immutable generation or deployment intent changes.
- Native dispatch indices/arguments, `Utility.batch_all`, commitments and both
  CRv4 timelock variants retain their encoding. `runtime/src/lib.rs` retains
  the signed-extension layout while changing spec 455 to 458. New tests decode
  the actual 458 metadata and compare it with the independent signed call
  encoder. Cross-version signature relabeling remains rejected.
- `rpc_info/metagraph.rs`, the selective metagraph runtime API and its weighted
  stake layout are unchanged. The client reads actual finalized state and
  authenticates exact code/metadata before decoding; it does not reproduce an
  old share-pool quote. The458 layout joins only 454/455 as a reviewed decoder
  domain. Unknown 456/457/459, changed transaction/state/name and foreign
  code/metadata remain refused.
- `stake_utils.rs` now credits the actual pool debit and requires full debits
  on withdrawal/transfer. `share-pool` caps member value at the pool total.
  These strengthen conservation; native call shapes and the harness's
  before/after custody checks remain exact. A move or withdrawal can still
  refuse if current state fails the stricter runtime condition. No allowance,
  custody tolerance or successful receipt is invented by this adoption.
- Registration collateral is now fill-or-kill for the complete burn/bond
  charge; leasing checks funding before distribution. Conviction locks advance
  individual and aggregate clocks independently. These are reviewed runtime
  semantic changes, not claims that all 455 economics are unchanged. Existing
  setup/live balance and registration postconditions remain in force.
- Proxy fee consent has a new storage prefix and keeps inherited call filters;
  root registration additionally verifies ownership. Basket deposits/claims,
  limit orders, crowdloan settlement, lease/coldkey swaps and Wasm contract
  caller-origin extensions have related conservation or authorization fixes.
  Searches of production `crv4`, `miner`, `validator`, `sim-testnet` and
  `evm/src` found no direct consumers of the changed basket, root-register,
  proxy-fee-consent, crowdloan or contract caller-origin interfaces. The
  unchanged EVM probe uses the native precompiles, not Wasm chain extensions.

The release, miner and validator now require the exact 458 tuple for current
authority. Exact 455 bytes move into evidence-only history beside 451–454.
Historical public observations and retained signatures remain bound to their
original version and bytes. This preserves previously finalized renewal and
probe/native receipts without granting an old signature a new runtime domain.
Existing final-semantic synthetic 455 cases intentionally remain historical
decoder controls; current 458 tests are added separately.

The shared native metadata cache and authority-list limit is exactly six:
current 458 plus the five reviewed predecessors. The same finite constant
guards runtime authentication, validator identity/schedule and EVM checkpoint
readers. Tests exercise the actual six-authority reader, all six hot cache
entries and seventh/duplicate/incomplete refusal, including the simulator's
real history constructor. No unbounded cache or generic version range is added.

## Transport and integration boundary

`check-runtime-metadata-artifacts.sh` always performs fresh blockchain reads
through the explicit owned HTTP endpoint. `curl --disable`, `--proto '=http'`
and `--noproxy '*'` prevent curl configuration, protocol and proxy redirection;
there is no redirect, retry loop, sleep or public fallback. Historical public
provenance stays in the manifest. GitHub source/artifact downloads are source
retrieval, separate from blockchain RPC. The script's Cargo target cache can
reuse compilation, but neither old nor new checker skips its fresh genesis,
canonical block and storage-hash reads based on a content cache.

The deterministic shell controls execute the actual checker/function with
fake curl/cargo/sleep executables. They cover the real caller's LAN selection,
one exact request, response identity and transport/error refusals without any
network access. Both release scripts continue to invoke this checker in their
source preflight.

Adoption requires rendering the final release lock and CLI from the combined
SN source plus the separately reviewed xops 458 expected-version pin. This
patch updates the runtime tuple in the lock fixture; its source hashes must be
rendered by the release tool after integration. Existing approvals and plan
action/spend identity must be compared by that native admission. This source
patch changes no journal, signed receipt, approved limit, native nonce,
retained generation, probe successor anchor or transaction intent.

Restricted incident captures are retained under
`sn-rpc-gateway-unpaced-20260914/post-deployment-lan-20260914T212328Z`;
bounded artifact/source comparison records under
`sn-runtime458-adoption-20260914/upstream-records`; and the passing offline
exact-Wasm capture under
`sn-rpc-gateway-unpaced-20260914/terra-runtime/runtime458-offline-exact-wasm-r1`.
Their custody is separate from source fixtures and final gate acceptance.
