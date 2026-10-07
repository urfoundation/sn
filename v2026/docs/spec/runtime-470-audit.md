# Runtime470 mainnet compatibility review

This is a source/artifact and consumed-interface review of one observed public
mainnet runtime. It supplies no genesis/checkpoint approval, signed launch plan,
hardware authorization or permission to submit a transaction. Reviewed testnet
runtime467 and retained historical approvals remain unchanged. Independent
qualification of this source candidate is required before integration.

## Exact provenance

The October 1 archive capture at finalized native block 9,184,596,
`0x78a8b744510ab93a81e8edc8e7673acec077720cea94b79c6684114c527b6f30`,
reported `node-subtensor/470/1/1`, Bittensor mainnet genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
and EVM chain ID 964. The source snapshot SHA-256 is
`423afcbad084c3d89bf0788c2d152b82ea363014ee8d52c4b837fb1a076c7a76`.
It is an unapproved public-RPC observation, not independent finality authority.

The official [v470 release][release] and tag bind exact source commit
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`. The release is still labelled
proposed/prerelease; that label is not used to infer on-chain activation.
The release asset's exact bytes equal the captured `:code` bytes. Git tag,
release API asset digests, upgrade manifest and srtool digest were checked and
retained separately. [The source manifest](runtime-v470-source.sha256) pins 139
files: all 120 prior consumed files, every changed runtime/pallet/primitive source
path, and the build and current root behavior references.

| Artifact | Bytes | SHA-256 | BLAKE2b-256 |
| --- | ---: | --- | --- |
| Compressed runtime Wasm | 2,556,358 | `e5abec692e3988352da818823d9729f139820e205ea17048f93816974106c005` | `5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47` |
| Metadata v14 | 354,056 | `ccad189c41970e1d33fe665b697b4bc763bece0212e81e15a3035d4091c79b1d` | `8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34` |

The pinned SN metadata probe was rebuilt with its locked SDK revision
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`. Executing the exact observed Wasm
reproduced the runtime version and every metadata byte/hash above. This is
execution of the Wasm metadata API, not comparison to a second RPC assertion.

The independent clean source rebuild uses the upstream pinned srtool v0.18.3
commit `0a446889c5e60abe92a41e426377276f5c7295e6`, Rust 1.89.0,
`PROFILE=production`, and `BUILD_OPTS=--features=metadata-hash`. The original
`scripts/srtool/Dockerfile`, lockfile and runtime build inputs are retained.
The build completed, but **deterministic source-to-Wasm equality failed**.
Compressed output is 2,556,742 bytes, SHA-256
`516922c9b4c1161bc8dcaf1ebd69c5c81a89f9759355bbd1bfdb6054760e5dfd`;
compact output is 10,457,817 bytes, SHA-256
`1eb5b4e51224a63e6e9fba9dc75ed79331063be9c8355ce7119b35935320df8f`.
The released compact artifact is 10,457,810 bytes, SHA-256
`d0659a3f70342932efb0483a8e1c069e9db13eaf5b8a88b74b2ba127c9d30b60`.

The independent Go section/function comparator and Rust `wasmparser=0.102.0`
instruction comparator isolate the difference completely: all non-code sections
are exact, 7,779 of 7,780 defined function bodies are exact, and the single
changed function is index 2001,
`wasmi_collections::hash::RandomStateImpl::default`. Its local declarations,
instruction count and every instruction other than 22 `i64.const` operands are
exact. These are generated hash-table seed constants. The locked
`wasmi_collections=0.32.3` [source][wasmi-hash] calls `ahash::RandomState::new`;
the built aHash fingerprint enables `compile-time-rng` and `const-random`.
The locked `ahash=0.8.12` [implementation][ahash-seeds] uses compile-time random
constants. `const-random-macro=0.1.16` uses host entropy unless
`CONST_RANDOM_SEED` was set when compiling the macro. The retained crate archive
hashes match `Cargo.lock`. Neither the release recipe nor its published digest
provides that seed. The rebuilt artifact also executes to the exact same
354,056-byte metadata14 and runtime identity.

The first Docker build's Git diagnostics additionally exposed an inaccessible
absolute alternate from a shared clone. The host source/tree were exact, and the
Wasm comparison above independently establishes the complete emitted delta; the
container's source-descriptor failure is retained, not treated as successful Git
attestation. A second clean shallow official-tag clone with self-contained Git
objects was verified; a second blind full build was not used to manufacture an
exact-build claim. The seed must be recovered/pinned by upstream for exact
reproduction of the published artifact.

**Assessment:** the official release artifact, exact tag/source identity,
GitHub-verified source commit, byte-identical observed on-chain code, executed
metadata, source review and complete one-function difference support a scoped
artifact-identity review independently of build reproducibility. GitHub's commit
verification is recorded as its assertion; no detached release-asset signature
or independently trusted local GPG key verification is claimed. A final runtime
approver must explicitly accept the documented reproducibility exception and
exact official artifact. On 2026-10-01 the subnet owner approved the exact
observed v470 artifact and this one-function reproducibility exception **for
launch planning only**. This decision does not approve the mainnet genesis,
finality checkpoint, signing, deployment, or activation. The rebuilt artifact
must not replace the official artifact in an approval or deployment.

The changed seed can change Wasmi hash-table layout, iteration and resource
behavior. This review does not prove universal semantic equivalence of arbitrary
Wasm smart-contract execution or DoS behavior. It does establish exact code for
all other runtime functions and unchanged consumed metadata; the selected
owner/UR/passive-root paths do not intentionally invoke Wasmi contract execution.
Keep Wasm-contract behavior and any broader equivalence claim outside this
conditional approval. Independent finality/genesis and live signing remain
separate gates even if the artifact exception is accepted.

## Consumed behavior and limits

The source delta is from official v467 commit
`c6bcb4a7400764c94c1d1b1938514c6c2dd3d33b` to exact v470. The external metadata
diagnostic finds all 87 selected CRv4 storage, calls, events, constants, runtime
types and signed-extension profiles unchanged. Mainnet codecs additionally
accept the actual v470 account, subnet, owner-window, validator stake/eligibility,
emission, trim, proxy and receipt/event shapes. These checks validate consumed
wire formats; source review is still needed for effects behind those formats.

| Consumer | Source review and result | Authority remaining separate |
| --- | --- | --- |
| UR producer | CRv4 timelock/commitment, weight setter and epoch behavior are unchanged; commitments and selective-metagraph wire implementation are unchanged. `SubnetInfoRuntimeApi` remains v2. | Independently signed complete production config, current capability/window, full provider/operator evidence, native signing and actual applied outcome. |
| Owner/bootstrap | Trim call and owner proxy shape, registration generation, admin windows, immunity, explicit Recycle mode, account nonce, receipts and current Safe reads remain supported. | Hardware metadata15/proof/device qualification, exact new owner plan, globally fenced custody and live admission. |
| EVM contracts | Precompile implementation and transaction-payment wrapper are unchanged from467; consumed native/EVM mapping and receipt shapes pass. | Exact contract build, combined finalized mapping, gas/native fee attribution and approved deployment/receipt authority. |
| Stake/registration | Transfer/swap stake now recognize the full-stake sentinel at execution; exact full withdrawal clears residual fractional pool shares. Failed registration refunds distinguish precheck failure from pruning/payment work. | No cached full-balance amount or fee quote may authorize a later send; retained original receipts and current quotes remain required. |
| Fees | Alpha-paid overestimates can refund TAO to free balance; `TransactionFeePaidWithAlpha.tao_amount` is net of that refund. Disabled-subtoken/root-unlock constraints apply to fee sources. Basket fee discounts also changed. | Generic balance events/deltas are not sufficient for actual gas attribution. No new fee/debit/refund verifier is qualified here. |
| Root | Retired `set_root_weights`, `RootWeightSettingEnabled` and `RootWeightsCap` are absent. Their old service/profile rejects v470. New passive observation reads exact existing seat/stake/delegation/basket state without a target vector. | Registration, funding, claims, take/delegation changes, basket trades and full fund accounting are not authorized by observation. |

The root removal precedes467. It is not a newly invented470 compatibility rule:
the pinned [migration][migration] clears netuid-0 weights, deletes the enable
flag and transfers the old cap into `BasketConcentrationCap`. The current
[Root Reborn guide][root-reborn] describes dividend accumulation where earned,
deposits reflecting current holdings and optional `swap_basket` trades. The
[basket implementation][basket] authenticates coldkey control; the runtime
[proxy filters][proxy] support explicitly authorized basket trading, including
the new `swap_basket_many`. No replacement target-weight vector or mandatory
trade is inferred. `BetaBasketRuntimeApi` changed v4→v5 with claim previews;
this observer does not decode that API or authorize those trades.

The [passive-root service](../../mainnet/ROOT-PASSIVE-SERVICE.md) is additive
bootstrap v4 with fresh independent config approval. Legacy signed v3
`explicit_root_weights` inputs, bytes, nonces and custody remain untouched.
Both UR roles and the distinct netuid-0 role remain mandatory. Codec source
guards can name the actual reviewed v470 source in newly signed owner/validator
inputs while all exact runtime pins and approval checks remain mandatory. This
does not add470 to the legacy reviewed-testnet catalogue or automatically admit
a future runtime.

Native Sr25519 and owner Ed25519 metadata shapes pass. ECDSA exists in the
runtime's `MultiSignature` with a 65-byte signature, but SN's selected native
64-byte adapter rejects it. Therefore native ECDSA signing remains unqualified;
no conclusion about EVM secp256k1 transaction support follows from that refusal.
Sr25519 wire compatibility is conditional on independent artifact authority and
current signing admission, not blanket production approval.

## Evidence and remaining launch gates

Author evidence is retained at
`/mnt/data/sn-testnet/mainnet-runtime470-astra-20261001/evidence/`: raw observed
artifacts and compact summary; release assets/API; immutable source manifest;
full build logs including failed image acquisition attempts; rebuilt exact-Wasm
probe; 87-interface comparison; explicit current-profile/retired-profile checks;
synthetic normal/race tests and negative controls. Necessary raw chain identity
remains outside test fixtures. No transaction was signed or submitted.

The first launch plan still needs independent genesis/checkpoint/runtime
approval, a qualified exact combined native/EVM mapping, current finalized
identity/stake census, externally held signature reconciliation, exact production
service/config approvals, release/image readback, and real operator/validator
admission. Source/artifact equality cannot close these gates.

[release]: https://github.com/RaoFoundation/subtensor/releases/tag/v470
[migration]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/migrations/migrate_remove_root_weights.rs
[root-reborn]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/docs/guides/root-reborn.mdx
[basket]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/staking/basket_trade.rs
[proxy]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/runtime/src/proxy_filters/mod.rs
[wasmi-hash]: https://github.com/wasmi-labs/wasmi/blob/3c42a099f031fb84f8c0acdb6ea756c8bbd24a0a/crates/collections/src/hash.rs
[ahash-seeds]: https://github.com/tkaitchuck/aHash/blob/9aa1ba20f05ed582eda04ea625d5658c92195a57/src/random_state.rs
