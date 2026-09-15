# Runtime 460 adoption

The owned archive returned `node-subtensor/460/1/1` at finalized block
8,014,242, hash
`0x6ba8842851af6d0d53ca97f867d5075c5d2a4e4580a60300e3d1b975e1d0966d`.
Genesis remains
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`.
All new chain requests used the approved owned LAN endpoint directly, without
pacing or public fallback, and record `independent_rpc=false`.

| Artifact | Bytes | SHA256 | BLAKE2b256 |
|---|---:|---|---|
| On-chain code | 2,525,524 | `12b9affec176cbb79c7e5db253d3d0e47f4cb575ce4501ef10de6578afbb817f` | `a2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d` |
| SCALE metadata v14 | 336,358 | `0e18eed4701255a567411bdc646c76eba355cf41bcb5fcbed8f673a458118e1a` | `98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c` |

The code digest equals the storage hash at the same explicit block. GitHub's
[artifact10420673699](https://api.github.com/repos/RaoFoundation/subtensor/actions/artifacts/10420673699)
links [workflow35029090314](https://github.com/RaoFoundation/subtensor/actions/runs/35029090314)
to [source8d5f20ec1a5e5d90295d43046dacdefc54aaed06](https://github.com/RaoFoundation/subtensor/commit/8d5f20ec1a5e5d90295d43046dacdefc54aaed06).
The unauthenticated archive API returned401. The public mirror ZIP has the
authoritative API SHA256
`4c00445deb37516aa9bde60a87d65a461721a864ab342a88a5ffed72e4068cca`.
Its extracted CI Wasm is byte-identical to the owned archive's code. The srtool
digest binds that commit, production profile, srtool0.18.3 and Rust1.89.0.
The exact-commit source tarball is retained with SHA256
`d5a63034391d919ac62ddb1c2c36db1a67728464b40a3487cdc9d42acb938bf4`;
Git fetch's unresolved-delta failures are retained separately from source review.

The [459 review](runtime-459-audit.md) remains applicable except for five native
Rust paths in [the source delta](https://github.com/RaoFoundation/subtensor/compare/70378404b56c12a85bc8cd163aca2f32cf4d1b80...8d5f20ec1a5e5d90295d43046dacdefc54aaed06).
The [460 manifest](runtime-v460-source.sha256) retains all71 reviewed paths;
only those five hashes change. Cargo.lock and the SDK revision
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a` are unchanged. The metadata source
manifest preserves all21 previous rows and adds three460 rows.

The native change increments a share-pool epoch on both zero-to-nonzero and
nonzero-to-zero denominator transitions. This retires pre-epoch leftovers when
a drained pool reopens and keeps retired rows absent if dividends arrive while
the denominator remains closed. Simulated deposits also accept a pool that has
shares but no value, matching real deposit behavior. Two staking/dissolution
test files cover these changes; runtime/src/lib.rs changes only the spec number.
Five generated Python binding files change only their version header.

Precompiles, dispatch indices, storage declarations, selective metagraph APIs,
signed extensions and weights submission are unchanged. The metadata has one
byte difference from459, at zero-based offset267231 (`cb` to `cc`), in the
System.Version spec constant. SN consumes weighted stake through the runtime's
selective metagraph API and does not independently value raw share rows, so no
SN accounting or decoding algorithm changes. Existing custody, threshold,
permit, owner, exact-block and balance checks remain mandatory.

Terra's existing exact-Wasm probe passed with these bytes under the unchanged
SDK, reproducing the exact version and metadata without stateful host access or
chain requests. Its sealed receipt is
`/mnt/data/sn-testnet/qualification/runtime460-terra-offline-probe-20260915-r1`,
index SHA256 `8864ae0f66179e0107e47470f3d721691f3c4eacee6e7b072efe0736331b8429`.

The admission patch keeps current signing exact460 and preserves451–455/458/459
as evidence. A shared immutable artifact catalog supplies current identity
constants, predecessor selection and the finite metadata-cache capacity.
Each catalog row retains explicit version/code/metadata literals independently
of the current selection. Original455,458 and459 companion owners retain only
their original predecessor domains. Configuration identity remains455; archived
source locks, approvals, intent hashes, budgets and finalized receipts retain
their bytes. Root renders release source hashes from the combined physical
checkout before native adoption.

A runtime spec bump requires a new signed payload domain even when call encoding
is unchanged. This release also compiles the reviewed artifact authority into
the fleet CLI, validator and simulator, explaining the binary refresh. The
catalog removes duplicated identity and history counts; it does not make future
unknown code trusted. Avoiding a future rebuild would require an authenticated
external review catalog shared by those consumers and an explicit persisted
runtime refresh path. That deployment change is outside this bounded patch.

Restricted raw capture and source evidence live under
`/mnt/data/sn-testnet/qualification/runtime460-20260915-r1`. This review and
probe do not establish live campaign acceptance or require new funding,
renewal, replay of completed recovery checks, or repetition of the459 gates.
