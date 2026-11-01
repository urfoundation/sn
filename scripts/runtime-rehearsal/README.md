# Runtime rehearsal node

Activation gate 2 (`VERSION2-FINAL.md` §14) runs the exact release bytecode of
the settlement contracts against the real Subtensor precompiles. Anvil cannot
run them, the testnet campaign closed and there is no localnet, so this
directory builds a node from the pinned runtime 481 source and
starts a one-node dev chain on it. The owned-route snapshot at finalized
block 9,320,004 on 2026-10-19 reported runtime 481 on mainnet (EVM chain 964).
The local runtime 481 gate 2 qualification passed on 2026-10-19; its source
review, captured-Wasm interface execution and complete R results are recorded
in [Runtime 481](#runtime-481). The broader 96-phase release gate has not passed,
and production owner approvals remain pending. The Go harness and its tests
are in `rehearsal/`. The gate first
ran on runtime 475; its measurements below are of that run, and
[Runtime 477](#runtime-477) and [Runtime 478](#runtime-478) record the reruns.
Runtime 478 changes the gas limit a stake move needs, so its section
supersedes the limits measured before it.

## Build

```sh
PROTOC=/path/to/protoc CARGO_TARGET_DIR=/path/to/target \
  scripts/runtime-rehearsal/build.sh /path/to/subtensor-checkout
```

`build.sh` clones `https://github.com/opentensor/subtensor.git` into the
checkout directory if it does not exist and checks out
`6a972693ae9580541225bd37696ac2fee26b8ae9`, whose tree is
`587abd7462d0cee0de4d0ec8bdf0df6a71cd947f`. The captured upstream tag `v481`
names that commit; the script checks the tag's tree when the remote returns
it. It then requires a clean tree, `spec_version: 481` in
`runtime/src/lib.rs` and every digest of
`docs/spec/runtime-v481-source.sha256`, and builds:

```sh
cargo build --locked --profile release -p node-subtensor \
  --features "metadata-hash fast-runtime" -j 10
```

(`RUNTIME_REHEARSAL_JOBS` overrides the `-j 10`.)

Upstream's localnet (`scripts/localnet.sh`, `fast_runtime=True`) builds the
workspace with `pow-faucet metadata-hash fast-runtime`. `metadata-hash` is also
in mainnet's release build, and `fast-runtime` gives fast blocks. `pow-faucet`
only adds a proof-of-work faucet call and drops the default subnet-registration
rate limit to 0, so it is left out and the runtime keeps mainnet's call set.

The script runs cargo inside the checkout, so rustup selects its
`rust-toolchain.toml` (1.89, target `wasm32v1-none`), and sets
`WASM_BUILD_WORKSPACE_HINT` to the checkout. Without the hint, a target
directory outside the checkout hides `Cargo.lock` from the runtime's Wasm
build, which then resolves unlocked dependencies and fails
(`ruint@1.20.1 requires rustc 1.90`). The build also needs `protoc`, `clang`,
`jq`, `xxd` and `b2sum`. Finally the script checks that the dev chain's
genesis `:code` is the built Wasm byte for byte and writes the record
`release/runtime-rehearsal-build.txt` in the target directory.

The 481 source manifest has 211 entries: the prior 479 manifest's 206 paths
and all five additional build paths changed by the 479-to-481 source delta.
All 39 changed runtime or node build paths are covered. This pins the source
inputs; it does not establish source-to-release-Wasm reproducibility.

Run the complete gate after the 481 source and metadata review is closed:

```sh
RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
  go test -tags runtime_rehearsal -count=1 -v -timeout=90m ./rehearsal/
RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
  go test -tags runtime_rehearsal -count=1 -v -timeout=30m ./mainnet/ \
  -run '^TestR1SettlementV2Phase$'
```

Both commands must pass, and every node-start line must name
`node-subtensor/481, reviewed source 6a972693ae9580541225bd37696ac2fee26b8ae9`.
The catalog also admits older reviewed runtimes, so a passing package alone
does not establish which runtime was exercised. Execute the captured mainnet
Wasm in a separate non-authoring local genesis and compare its code hash,
runtime version and exact metadata bytes with the captured 481 artifacts.
That verifies `Core_version` and `Metadata_metadata`; it does not establish
equivalence of every function body to the source build. Retain fresh source,
binary, artifact and result receipts, then re-read the current finalized
runtime on the approved route before deployment. The historical 475, 477 and
478 measurements below remain results of those runtimes; current 481 results
are recorded separately.

Measured on a 24-thread host shared with other jobs. On 2026-10-11, for
runtime 475 at `-j 10`, the build from scratch took about 37 minutes (2,243 s
wall clock, including one restart that kept every compiled crate, after the
missing workspace hint above), and a rebuild of the node crate alone 266 s. On
2026-10-13 the 477 build took 648 s at `-j 22`, and the 478 build 746 s at
`-j 10`, each started on a copy of the 475 build's target directory: every
workspace crate and the vendored Frontier rebuild, because a new checkout has
new file times, and the registry and git dependencies are reused. The
toolchain is 475's. The runtime Wasm of each build is:

| Runtime | Artifact | Bytes | SHA-256 | BLAKE2b-256 |
| --- | --- | ---: | --- | --- |
| 478 | `node_subtensor_runtime.compact.compressed.wasm` | 2,848,341 | `3d754c8b872a1728098fc980ba8bbd1bcaa7a1ec802565010f4ce173d628c308` | `1978bf7751e93d8b2d6c3ecaf44c5e4db54f41dca9a7ea750c2ed4f59ad03ae6` |
| 477 | `node_subtensor_runtime.compact.compressed.wasm` | 2,846,031 | `3cfabd8d93c1e0060d8395a6295445cecea957caf7e6da3991d0dc72ab20fe48` | `71b7103db2a61d9393cab5298471e4ddc627a44d3c75b77a3aa55267dfb79f2f` |
| 475 | `node_subtensor_runtime.compact.compressed.wasm` | 2,862,456 | `4bdb6b5dada78e742db06c94523dea8734d737832b5f6a118ecf8d26f6813ce4` | `a486ccc60a0ff5c2a3362e19470186fda365edcda1dfc688ba3d88e36e60fca6` |

The BLAKE2b-256 is the chain's `state_getStorageHash(":code")`. The 478 build
differs from mainnet's 478 Wasm (2,634,898 bytes, SHA-256 `1d64a44f…`), and the
477 build from mainnet's 477 Wasm (2,630,752 bytes, SHA-256 `e226dd03…`), as
expected: `fast-runtime` changes constants, and `docs/spec/runtime-475-audit.md`
explains why even a production-feature rebuild is not expected to match byte
for byte. `docs/spec/runtime-477-audit.md` and
`docs/spec/runtime-478-audit.md` review the 477 and 478 sources.

## Run

```sh
RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor scripts/runtime-rehearsal/run-node.sh
```

`run-node.sh` starts the `dev` chain spec: one authority, Alice as sudo, and
Alice, Bob and Charlie with 1,000,000 TAO each. It patches two raw genesis
values: the EVM chain ID becomes mainnet's 964 (`EVMChainId.ChainId`), and
contract creation is open to every account (`EVM.DisableWhitelistCheck`).
Blocks are sealed on demand, and finalized at once, by the RPC
`engine_createBlock(true, true, null)`; there is no GRANDPA. Substrate and
Ethereum JSON-RPC share one port on 127.0.0.1. The node takes no peers
(`--no-mdns --reserved-only`): rehearsal chains share one genesis, and two on
one host would otherwise find each other and exchange transactions. It uses
the single-state transaction pool, which validates a submission against the
newest block (the fork-aware pool could still use the block before), bans
nothing, and runs no offchain workers. Optional environment:
`RUNTIME_REHEARSAL_BASE_PATH` (default a new temporary directory),
`RUNTIME_REHEARSAL_RPC_PORT` (9944), `RUNTIME_REHEARSAL_P2P_PORT` (30333) and
`RUNTIME_REHEARSAL_SEALING` (`manual`; `instant`, or a period in milliseconds
to seal on a timer).

Differences from mainnet: `fast-runtime` sets 250 ms slots, a default subnet
tempo of 10 (mainnet 360), up to 32 epochs per block (mainnet 2) and short
rate limits and swap delays. The weight formulas, and so the gas schedule,
are mainnet's (40,000 weight per gas). The dev genesis has no SN25 state. With
tempo 10, the default 10-block admin freeze window closes a subnet's admin
calls in 9 of every 10 blocks; set `AdminUtils.sudo_set_admin_freeze_window(0)`
first. A second subnet within 7,200 blocks needs
`AdminUtils.sudo_set_network_rate_limit(0)`. The activity cutoff is
`ActivityCutoffFactorMilli × tempo / 1000`: 138 blocks at tempo 10.

eth_call and eth_estimateGas run at the latest block, a transaction in the
next one; a window-bound call must hold at both.

## Go harness and smoke test

```sh
RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
  go test -tags runtime_rehearsal ./rehearsal/ -run TestRehearsalSmoke -v
```

Run it from the repository root with `forge` on `PATH`. The test files of
`rehearsal/` carry the `runtime_rehearsal` build tag. Each test starts its own
node through `run-node.sh` on free ports in a temporary directory and stops it
at the end; the node is ready in about 10 s. A test refuses a node whose
runtime version has no reviewed source (`crv4.ReviewedNativeOwnerSpecSource`)
and logs the version and the source it ran on. The
helpers sign extrinsics with the repository's signer (`chain.EncodeSignedCall`
over a `crv4.DialChainContext` connection), seal each transaction into its own
block, check dispatch success with `crv4.VerifyFinalizedExtrinsicContext`, and
read storage with the `chain` readers. They fund SS58 and EVM accounts (an EVM
account's balance is its mapped account's), create a subnet, register
hotkeys, stake for any coldkey, including a contract's mapped coldkey
blake2b-256("evm:" ‖ h160) (`ss58.EvmMirrorPubkey`), dispatch as sudo, seal
blocks (100 empty blocks take about 1 s), build contracts with `forge` and the
pinned toolchain and deploy their creation bytecode, send and simulate EVM
calls, and read stake through the staking precompile (`getStake`) and through
storage (`TotalHotkeyAlpha`).

The smoke test deploys `evm/test/rehearsal/RehearsalProbe.sol`, which calls a
precompile as itself, measures the call's gas and reports the call value it
receives. Its mapped coldkey owns stake on an emitting subnet. After two tempos
of dividends the staked hotkeys' share price is above 1; tempo 0 then stops
epochs, so each delta is the operation's alone, and a hotkey registered after
that keeps share price 1.
Measured on 2026-10-11 (the test takes about 18 s):

- **getStake** through the probe equals the direct precompile read. The
  hotkey's `TotalHotkeyAlpha` is the probe's stake plus its owner's take plus
  1 rao of share-pool floor.
- **Moves and transfers** (eight amounts each, from 100,000,007 to
  5,000,000,003 rao): the source lost exactly the amount, in `getStake` and in
  `TotalHotkeyAlpha`, and the destination's `TotalHotkeyAlpha` gained exactly
  the amount, every time. The destination's `getStake` was one rao short in 1
  of 8 `moveStake` and 1 of 8 `transferStake` calls onto a hotkey at share
  price above 1, and in 3 of 4 whole-position calls there; it was exact in 16
  of 16 calls onto a hotkey at share price 1.
- **Floor**: below 100,000 rao of TAO the runtime refuses a same-subnet
  `moveStake` or `transferStake` with `AmountTooLow`. At price 9.014779842 TAO
  per alpha, 11,092 alpha rao was refused and 11,093 accepted, for both calls,
  which is ceil(100,000 / price). The price stayed the same across blocks of
  emission.
- **Whole position**: `moveStake` with 2^64−1, 2^64 and 2^256−1, and
  `transferStake` with 2^64−1, each moved the whole position.
  2^64−2 was refused with `NotEnoughStakeToWithdraw`.
- **Rollback**: a transaction that reverts after a successful `moveStake`
  leaves both positions unchanged.
- **Registration**: `registerLimit` and `burnedRegister` from the probe
  register a hotkey owned by the probe's mapped coldkey, which pays the burn
  (1,417,205,419 and 1,782,243,972 rao) with zero call value; `getUid` then
  returns `(true, uid)` with the stored UID, and `(false, 0)` for an
  unregistered hotkey.
- **Registration collateral** (`pallets/subtensor/src/subnets/collateral.rs`;
  `CollateralLockShare` defaults to 0, and the subnet owner or root sets it
  with `AdminUtils.sudo_set_collateral_lock_share`): at lock share
  2^15/65535 a `registerLimit` from the probe paid the full price,
  2,236,996,529 rao, swapped it into alpha, burned half of that alpha and
  staked and locked the other half, 115,828,883 alpha rao, on the (hotkey,
  probe coldkey) position. `getStake` returned exactly the lock, and the
  precompile's `getMinerCollateral(uint16,bytes32,bytes32)` returned
  `(true, 115828883, 2^64, 0, 0)` (exists, locked, drain ratio 1 as Q64.64
  bits, minimum locked, earned), and `(false, 0, 0, 0, 0)` for a position
  without collateral. With free stake added on top, `moveStake` and
  `transferStake` of the free amount plus 1 rao, and of 2^64−1, were refused
  with `StakeUnavailable`; exactly `getStake − locked` moved, leaving the lock.
- **Locked alpha** (`staking/lock.rs`, `AccountFlags`): every coldkey,
  including a contract's mapped coldkey, rejects incoming conviction-locked
  alpha unless it opts in (`getRejectLockedAlpha` returned true for the
  probe). A third party that locked 4,949,971,706 of its 9,899,943,412 rao
  could not `transfer_stake` the whole position to the probe
  (`AccountRejectsLockedAlpha`); its unlocked part moved.
- **Call value** (`SubtensorEvmBalanceConverter`, `runtime/src/lib.rs`): the
  runtime moves floor(value / 10^9) rao. Sent 1, 999,999,999, 1,000,000,000
  and 1,999,999,999 wei, the contract saw that exact `msg.value`, while its
  balance and its mapped account gained 0, 0, 1 and 1 rao, and the sender was
  debited the same whole rao beyond the fee: the sub-rao part never moves.
- **Refusals**: an `eth_call` from the contract's address to the precompile
  returns `evm error: Other("dispatch execution failed: <Error>")`. Inside a
  contract the call fails with empty return data and consumes all the gas
  forwarded to it: a refused transaction used 9,844,446 of its 10,000,000 gas.
- **Gas** (maximum over the move and transfer sweeps; transaction figures
  vary by a few gas units with the random keys in calldata):

  | Call | In the probe | Transaction used | `eth_estimateGas` limit |
  | --- | ---: | ---: | ---: |
  | `getStake` (view) | 4,748 | | |
  | `getUid` (view) | 995 | | |
  | `getMinerCollateral` (view) | 1,013 | | |
  | `moveStake` | 81,753 | 107,665 | 2,427,799 |
  | `transferStake` | 79,321 | 105,233 | 2,420,524 |
  | `registerLimit` | 1,409,550 | 1,434,798 | 1,493,416 |
  | `burnedRegister` | 1,396,194 | 1,421,266 | 1,479,461 |

  The runtime charges a dispatch's actual weight as gas, but admits it only if
  the gas forwarded covers its declared weight, which for `move_stake` and
  `transfer_stake` includes walking a full 256-entry `StakingHotkeys` list. A
  transaction that moves stake therefore needs a gas limit of about 2.4M
  while it uses about 105,000.
- **Emission**: no validator set weights, and Yuma's fallback paid the staked
  hotkeys validator emission in proportion to their stake every tempo. In an
  epoch without weight-derived emission it pays active permit holders by stake
  (every UID by stake if none is active), so the escrow hotkey stays at share
  price 1 only while validators set weights; R-3 needs one.

`TestRehearsalTransientStorage` deploys a Yul probe of EIP-1153 transient
storage (`rehearsal/transient_test.go`). Measured on 2026-10-12: TLOAD and
TSTORE run, and the storage is empty at the start of every transaction, but a
write made in a call frame that reverts survives the revert, which EIP-1153
forbids. After the probe wrote 1 and called itself to write 2 and revert, it
read 2. Frontier keeps one transient map per transaction and rolls back only
persistent storage (`SubstrateStackState` in
`vendor/frontier/frame/evm/src/runner/stack.rs` of the runtime source). A
reentrancy flag kept there would stay set after a guarded call reverts inside a
caller that catches the revert, such as a batch of claims, and refuse every
later call in that transaction, so the settlement contracts keep their guards
in persistent storage.

## R battery

```sh
RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
  go test -tags runtime_rehearsal ./rehearsal/ -v -timeout 60m
```

The whole package, the smoke test, the transient-storage probe and R-2 to
R-6 each on its own node in parallel, took 8 minutes 22 seconds on
2026-10-12 (R-7, added the same day, takes about 6 minutes on its own node).
Sealing runs at about 110 to 125 blocks a second whether or not blocks
finalize or several callers seal at once, so a 14,400-block challenge window
takes about 2 minutes.

Each R test deploys directly, from the exact forge artifacts, the stack
STSettlement2 needs: the v1 reserve sink, vault and coordinator proxy (in
`Deploy.s.sol`'s order, the v1 escrow registered), one v1 operator per pool,
then vault2, its escrow registration on the next nonce (SPEC A7), the
STSettlement2 proxy with one auditor and quorum 1, and the pool through
`registerOperatorPool`. R-1, the deployment through §8.5's phase with
readback, runs from `mainnet/` as `TestR1SettlementV2Phase`, with the same
`runtime_rehearsal` tag and `RUNTIME_REHEARSAL_NODE`
(`mainnet/BOOTSTRAP-CONTRACTS.md`). Epoch 0 holds the deployment (300 blocks);
epoch 1, every pool's first epoch, has the shortest cadence STSettlement2
accepts, a 10-block commit window plus `CHALLENGE_BLOCKS`, 14,410 blocks.
Tests that close many epochs continue with 100-block epochs (R-2, R-3 and R-4
from epoch 2, R-5 from epoch 3). R-2, R-4 and R-5 stop subnet epochs (tempo
0) so that no dividend moves a position between two reads; R-3 runs SN25's
tempo of 360. Measured:

- **R-1 deployment** (`TestR1SettlementV2Phase`, 100 seconds). The settlement
  v2 phase of `bootstrap-contracts` deployed the final vault2 and its escrow
  registration, sent back to back and sealed into one block, then the
  STSettlement2 implementation, the initialized proxy and vault2's binding,
  and, as the optional sixth action, the claim batcher. Each completed only
  after its readback against the real precompiles, and the escrow position
  held no registration collateral. Gas used of the approved limits:
  `vault2-create` 2,821,690 of 4,000,000, `escrow2-register` 1,463,833 of
  2,500,000, `settlement2-create` 1,935,622 of 3,000,000, `proxy2-create`
  310,486 of 1,000,000, `vault2-link` 31,329 of 150,000 and `batcher-create`
  285,209 of 400,000.
- **R-2 capture.** A pool holding nothing funded 0 without a runtime call
  (gas 110,018). With 88,974,634,979 rao on the pool, the close moved exactly
  that and escrow gained exactly that (gas 230,818). A pool registered at a
  collateral lock share of 2^15/65535 held 1,481,130,231 rao of locked
  collateral: alone it funded 0, and with stake added the close moved exactly
  `getStake − locked`, 38,742,541,778 of 40,223,672,009 rao, leaving the
  lock. Dust: 5,569 alpha rao, transferred to vault2 at a price where it was
  worth 120,016 rao of TAO, fell to 7,507 rao when the price fell; the close
  reverted (runtime `AmountTooLow`) and recorded nothing, and the next close
  deferred that epoch and captured the dust with a top-up.
- **R-3 claim.** Claims paid with a real `transferStake`: escrow lost exactly
  the amount and the account gained it or one rao less (one rao less in all
  three, the escrow having earned dividends). A 4,861 rao claim, below
  the floor at price 10.284, reverted (`AmountTooLow`); a claim to a coldkey
  holding 128 staking hotkeys reverted (`TooManyStakingHotkeys`); both stayed
  claimable, and the second paid once the coldkey freed one entry. Third
  parties' `add_stake`, `move_stake` off and onto the escrow hotkey,
  `remove_stake` and a `transfer_stake` to vault2 never lowered vault2's
  escrow position; a transfer carrying conviction-locked alpha to vault2 was
  refused (`AccountRejectsLockedAlpha`). Over two tempos without weights the
  escrow hotkey earned 3,595,812,938 rao (Yuma's fallback pays validator
  emission by stake to active permit holders); over two tempos while a
  validator holding most of the stake weighted a miner it earned nothing.
  Simulated from vault2, `transferStake` with 2^64 − 1 would move the whole
  escrow position; vault2 never sends it, and no pool can fund 2^64 − 1 rao,
  so its `AmountTooLarge` guard cannot be reached on a real chain. Third
  parties could add hotkeys to vault2's own `StakingHotkeys` only until it
  held 128; `absorbStake` and claims still worked, but a claim then used
  1,256,716 gas instead of 154,218, and `absorbStake` 1,220,874: the runtime
  charges a walk of the origin coldkey's list.
- **R-4 registration.** `registerOperatorPool` registered the pool hotkey
  under vault2's coldkey, burned 3.80 TAO and refunded the rest of the 100 TAO
  ceiling to the owner through STSettlement2 (gas 1,594,088); a second
  registration and a too-short first epoch were refused. With immunity 0 and
  the subnet full, registrations pruned the oldest UIDs first, the escrow
  hotkey before the pool hotkey; both kept stake and owner, and the close
  moved the pruned pool's stake into the pruned escrow. `registerPoolUid`
  re-registered the pool hotkey for its payer (gas 1,449,949; `UidExists`
  once registered). `absorbStake` moved 35,094,897,822 rao that vault2 held
  on the v1 escrow hotkey into escrow as surplus (gas 127,124). Since
  2026-10-13 the registration runs through `sn-mainnet register-operator-pool`
  (`mainnet/BOOTSTRAP-CONTRACTS.md`, settlement v2 activation), which R-4
  builds from `mainnet/` with `go build`: review's `eth_call` preflight from
  the owner predicted UID 7, the owner sent exactly the printed call, and
  readback confirmed the event, `pools` and `settlementAt`, with the same
  burn and gas (R-4 took 252 seconds). `TestR4RegistrationRefusals` (51
  seconds) drives what the chain refuses through the same command. The node
  answers a revert with code -32603, the message `VM Exception while
  processing transaction: revert` and the revert's bytes as `data`
  (`0xc52a9bd3` for `InvalidConfiguration`), a burn above the cap as a revert
  with data `0x`, and a sender short of the value as `evm error: OutOfFund`
  with data `0x`; a failed read, such as `header not found` (-32000), carries
  no data. review named each and exited 3, and readback did the same for a
  `settlementAt` that reverts `UnknownOperator`.
- **R-5 approval, demand and answer.** Finalize was refused without a quorum
  (`QuorumNotReached`), then inside the window (`ChallengeWindowOpen`); an
  approval needed an auditor and the exact pending root (`Unauthorized`,
  `PendingMismatch`, `AlreadyApproved`). A demand paying 1 rao short of
  `demandFeeWei` was refused (`InvalidDemandFee`); one paying it raised vault2's
  account by exactly 1,000,000 rao, blocked finalize (`DemandOpen`) until the
  operator's answer, and could not be repeated (`AlreadyDemanded`, then
  `NoFault`).
- **R-6 gas**, on ledgers of 2^29 accounts (depth 29, the deepest vault2
  takes), against the SN25 operator gas policy's `maximum_gas` of 1,000,000
  (`server/local/sn25golive/sn25-mainnet-gas-params.json`). "Before" is the
  same run against vault2 as it was before its gas reduction (snv2
  `7202d7cb`), on the same day:

  | Transaction | Sender | Gas used | Before | Gas limit needed |
  | --- | --- | ---: | ---: | ---: |
  | close, capturing | operator | 225,430 | 298,949 | 2,688,632 |
  | close, nothing to capture | operator | 126,610 | 131,501 | 144,347 |
  | close deferring 1 epoch | operator | 131,633 | 158,174 | 149,526 |
  | close deferring 144 epochs | operator | 2,409,622 | 5,456,724 | 2,949,195 |
  | commit | operator | 205,288 | 297,958 | 225,484 |
  | answer (checkPath) | operator | 149,074 | 158,843 | 154,089 |
  | finalize | operator | 94,389 | 179,386 | 100,877 |
  | approve | auditor | 132,524 | 134,343 | 135,736 |
  | demand | anyone | 98,268 | 104,481 | 104,424 |
  | claim, vault2's first | anyone | 186,682 | 236,314 | 2,572,701 |
  | claim, an account's first | anyone | 186,707 | 202,102 | 2,572,776 |
  | claim, an account's first after its neighbour's | anyone | 169,595 | 202,102 | 2,521,578 |
  | claim, a repeat | anyone | 169,606 | 185,026 | 2,521,610 |

  Each deferred epoch costs about 15,900 gas (before, 37,000), and about
  19,500 of limit. A close that captures, and a claim, need about 2.5 to 2.7
  million gas of limit because the runtime admits `moveStake` and
  `transferStake` only if the forwarded gas covers their declared weight; the
  capturing close is above the policy's `maximum_gas`, as is a close
  deferring more than about 43 epochs (before, 17). The limits are
  `eth_estimateGas` values, which this node computes with Frontier's
  `rpc-binary-search-estimate`: its search stops once a step moves the bound
  by less than 10%, so a limit can exceed the least sufficient one by up to
  that much and need not fall with the gas used.
- **R-7 batched claims** (`TestR7ClaimBatch`, 320 seconds, and
  `TestR7MaxGasDoesNotDependOnCalldata`, 12 seconds), on a root of 2^29
  accounts: claims through `STClaimBatcher` (`evm/src/STClaimBatcher.sol`),
  each item capped at the tooling's default gas per claim of 2,600,000
  (`onchain.ClaimBatchGasPerClaim`), and each batch sent with exactly the
  least gas limit at which an `eth_call` of it attempts every item, which then
  paid every item. A batch succeeds at any gas limit, attempting only the
  items its gas covers, so `eth_estimateGas` cannot size one; the tooling
  finds the limit with `eth_call` and adds 20%. The tools put at most 32
  claims in a batch (`onchain.ClaimBatchMaxClaims`): the daemon keeps a
  batch's signed bytes in each of its units.

  The block gas limit is 75,000,000, but the pool refuses a transaction that
  names more than 64,998,500 gas (`exceeds block gas limit`): the normal
  dispatch class's extrinsic weight limit, the 75% normal share of the 4 s
  block less the 10% kept for block initialization, at 40,000 weight per
  gas, less the transaction's own extension weight
  (`onchain.ClaimBatchMaxGas`). R-7 checks that this is exactly 13/15 of the
  block gas limit less 1,500 gas, from which the tools keep a 10,000-gas
  margin, and that it is the same with 0, 100,000 and 900,000 bytes of
  calldata. Two accounts' claimed watermarks share a slot of vault2, so
  consecutive accounts alternate between a claim that writes its slot's
  first value and one after its neighbour's: alone they used 187,115 to
  187,126 and 169,992 to 170,015 gas; `eth_estimateGas` gave a limit of up to
  2,574,029, and the least limit that paid was up to 2,443,953, below the
  default cap. Each batch claims consecutive accounts, so about half of each
  kind.

  | Claims | Gas used | Per claim | Least gas limit |
  | --- | ---: | ---: | ---: |
  | 1, batched | 180,926 | 180,926 | 2,704,281 |
  | 5, batched | 783,491 | 156,698 | 3,319,253 |
  | 10, batched | 1,509,076 | 150,907 | 4,058,837 |
  | 20, batched | 2,972,226 | 148,611 | 5,549,988 |
  | 32, the most the tools batch | 4,727,690 | 147,740 | 7,339,052 |
  | 418, the most one transaction holds | 61,210,029 | 146,435 | 64,902,281 |

  Against the same claims sent alone, a batch of 10 uses 15.5% less gas, a
  batch of 20 16.8%, one of 32 17.3% and the largest 18.0%: the 21,000
  intrinsic gas of a transaction and vault2's first, cold reads are paid
  once. A batch of 32 costs under 1% more a claim than the largest. Its least
  limit grows by about 149,000 gas per claim, because only the last item
  needs the 2.6M the runtime admits a transfer with; ten single claims need
  ten limits of about 2.44M. A batch of one, an account after its
  neighbour's, used 6% more than such a claim alone, so the tooling sends a
  lone claim directly. The most one transaction holds is searched by batch
  size, since offering more accounts than fit also spends their calldata's
  intrinsic gas; with shallower trees, and so shorter proofs, more than 418
  claims fit. Against vault2 before its gas reduction (snv2 `7202d7cb`)
  single claims used about 202,500 gas, a batch of 20 166,340 per claim, and
  one transaction held 339 claims.

  A last batch of four, whose second claim had a broken proof and whose third
  was below the transfer floor, paid the first and the fourth and reported
  the second's `InvalidProof` and the third's runtime refusal, which returns
  nothing, as selector zero; both accounts stayed claimable. It used
  2,974,627 gas: the runtime consumes all the gas forwarded to a precompile
  call it refuses, so the refused claim used nearly all of its 2.6M cap, and
  the tools preflight each claim to keep refusals out of a batch. vault2's
  reentrancy flag, persistent storage, was clear again for the claim after
  each refusal.

## Runtime 477

Mainnet moved from runtime 475 to 477 on 2026-10-12, and the gate repeats on
any runtime change (`VERSION2-FINAL.md` §14). On 2026-10-13 the whole battery
ran again on the 477 node, from sn `188c1547` with the changes of branch
`v2/runtime-477`:

```sh
go test -tags runtime_rehearsal ./rehearsal/ -v -timeout 60m
go test -tags runtime_rehearsal ./mainnet/ -run TestR1SettlementV2Phase -v
```

Every test passed: the smoke test, the transient-storage probe, R-2, R-3,
R-4, R-5 with R-6, both R-7 tests, and R-1, whose approval named the 477
source `5c6e83e2…` with the node's version. The `rehearsal` package took 776
seconds on a host at a load of 20 to 100 (R-5 755 s, R-7 653 s, R-3 545 s,
R-4 323 s, R-2 321 s), and R-1 120 seconds.

Every fact recorded above was reproduced: the exact deltas, the one-rao-short
destinations (1 of 8 onto an earning hotkey, 0 of 8 at share price 1), the
floor at 11,093 alpha rao and price 9.014779842, the whole-position amounts,
the burns (1,417,205,419 and 1,782,243,972 rao; 3.80 TAO for the pool), the
collateral (115,828,883 and 1,481,130,231 alpha rao locked; 38,742,541,778 of
40,223,672,009 rao captured), the dust refusal, the escrow hotkey's
3,595,812,938 rao without weights and 0 with, the call-value truncation, the
transient write that survives a reverted frame, the 64,998,500-gas ceiling,
and 418 claims in one transaction.

Every gas figure is the 475 figure, except these, which differ by multiples
of 12 gas. That is one calldata byte being nonzero instead of zero: each run
draws new keys, so its addresses and proofs differ in zero bytes. R-1 and the
smoke test ran twice on 477 and show the same variation between their own
runs.

| Figure | 475 | 477 |
| --- | ---: | ---: |
| `moveStake` transaction; `eth_estimateGas` limit | 107,665; 2,427,799 | 107,653; 2,427,763 and 107,665; 2,427,799 |
| `registerLimit` transaction; `eth_estimateGas` limit | 1,434,798; 1,493,416 | 1,434,786; 1,493,404 and 1,434,798; 1,493,416 |
| `burnedRegister` transaction; `eth_estimateGas` limit | 1,421,266; 1,479,461 | 1,421,266; 1,479,461 and 1,421,254; 1,479,448 |
| R-1 `proxy2-create` | 310,486 | 310,486 and 310,462 |
| R-1 `vault2-link` | 31,329 | 31,317 and 31,329 |
| R-3 claim with vault2's list at 128 | 1,256,716 | 1,256,740 |
| R-6 demand: used; limit needed | 98,268; 104,424 | 98,256; 104,413 |
| R-6 claim, an account's first after its neighbour's | 169,595; 2,521,578 | 169,583; 2,521,542 |
| R-7 single claims, first of a slot | 187,115 to 187,126 | 187,091 to 187,102 |
| R-7 single claims, after the neighbour's | 169,992 to 170,015 | 169,968 to 169,991 |
| R-7 single claim: `eth_estimateGas` limit; least limit, up to | 2,574,029; 2,443,953 | 2,573,957; 2,443,929 |
| R-7 batch of 1: used; least limit | 180,926; 2,704,281 | 180,902; 2,704,257 |
| R-7 batch of 5 | 783,491; 3,319,253 | 783,383; 3,319,145 |
| R-7 batch of 10 | 1,509,076; 4,058,837 | 1,508,836; 4,058,597 |
| R-7 batch of 20 | 2,972,226; 5,549,988 | 2,971,890; 5,549,652 |
| R-7 batch of 32 | 4,727,690; 7,339,052 | 4,727,534; 7,338,896 |
| R-7 batch of 418 | 61,210,029; 64,902,281 | 61,207,233; 64,899,485 |
| R-7 batch of 4 with two refusals | 2,974,627 | 2,974,699 |

A batch of 32 costs 147,735 gas a claim (147,740 on 475), and the largest
146,428 (146,435).

## Runtime 478

Mainnet moved from runtime 477 to 478 at block 9,276,925 on 2026-10-13. 478
changes how the staking precompile admits `moveStake` and `transferStake`, the
calls vault2 depends on (`docs/spec/runtime-478-audit.md`). On 475 and 477 the
gas forwarded to such a call had to cover a walk of a full 256-entry
`StakingHotkeys` list, so every stake move needed a limit of about 2.4M. On 478
it has to cover a walk of the caller's own list plus one entry, and the call
also reads the list's length, 625 gas. `TestRehearsalStakeExitAdmission`
(`rehearsal/admission_test.go`) measured the least gas forwarded, by
`eth_call` from a contract whose list third parties lengthened, and found the
rule exact to the gas:

| Caller's list | `moveStake` | `transferStake` |
| ---: | ---: | ---: |
| any, on 477 | 2,296,767 | 2,294,335 |
| 2 | 83,017 | 80,585 |
| 16 | 205,517 | 203,085 |
| 64 | 625,517 | 623,085 |
| 128 | 1,185,517 | 1,183,085 |

That is 65,517 and 63,085 gas plus 8,750 an entry. One gas less is refused and
consumes all the gas forwarded. On 2026-10-13 the whole battery ran on the
478 node, and the same harness pinned to 477 on the 477 node, each test
sending every measured transaction with the least gas limit at which an
`eth_call` of it succeeds:

```sh
go test -tags runtime_rehearsal ./rehearsal/ -v -timeout 60m
go test -tags runtime_rehearsal ./mainnet/ -run TestR1SettlementV2Phase -v
```

Every test passed on both, and R-1's approval named the reviewed 478 source
`850f2755…` with the node's version. Every behaviour of the runs above was
reproduced. The `rehearsal` package took about 560 seconds (R-5 555 s, R-7
486 s, R-3 376 s, R-4 246 s, R-2 199 s), and R-1 124 seconds. At depth 29, with
vault2's list at one or two entries, as at launch:

| Transaction | 477 used | 477 least limit | 478 used | 478 least limit |
| --- | ---: | ---: | ---: | ---: |
| close, capturing | 225,430 | 2,513,577 | 226,055 | 236,399 |
| close deferring 1 epoch | 131,633 | 141,343 | 131,633 | 141,343 |
| close deferring 144 epochs | 2,409,622 | 2,862,246 | 2,409,622 | 2,862,246 |
| commit | 205,288 | 214,491 | 205,288 | 214,491 |
| finalize | 94,389 | 100,822 | 94,389 | 100,822 |
| answer | 149,050 | 153,987 | 149,026 | 153,963 |
| approve | 132,524 | 134,165 | 132,524 | 134,165 |
| demand | 98,268 | 103,264 | 98,256 | 103,252 |
| claim, an account's first | 186,707 | 2,443,534 | 187,332 | 192,318 |
| claim, a repeat | 169,618 | 2,426,445 | 170,243 | 175,229 |
| batch of 32 claims | 4,728,002 | 7,339,364 | 4,748,110 | 7,358,847 |
| the most one transaction holds | 418 claims | 64,898,177 | 416 claims | 64,861,477 |

`eth_estimateGas` gave a capturing close 246,899 and an account's first claim
206,967 (2,688,632 and 2,572,776 on 477). Every stake move uses 625 gas more;
everything else is unchanged, and batches are still sized by the per-item cap
of 2,600,000. R-1's six actions used the 475 gas of their approved limits.

The limit a stake move needs now follows vault2's own list, which third
parties can lengthen to 128 entries at any time. R-3 filled it: a claim then
used 1,257,365 gas and needed a limit of 1,273,292 (154,831 and 159,817 before),
and `absorbStake` 1,221,499 and 1,245,279. A claim estimated at 173,484 gas
before the list filled, signed at that with 20% and sent after, ran out of gas
and left the account claimable; signed at 2,600,000 it paid. The claim tools
therefore sign a claim at its estimate with 20% and never below
`onchain.ClaimBatchGasPerClaim`, the limit every claim needed on 475 and 477;
the review gives the matching rule for closes and its bound.

## Runtime 481

The local runtime 481 gate 2 qualification passed on 2026-10-19 for source
`6a972693ae9580541225bd37696ac2fee26b8ae9`, tree
`587abd7462d0cee0de4d0ec8bdf0df6a71cd947f`. The
[consumed-interface review](../../docs/spec/runtime-v481-review.md) records the
finalized snapshot, exact source/release/artifact pins, semantic changes and
closed evidence hashes. This qualification does not credit the broader
96-phase release gate, which has not passed, or the pending production owner
approvals.

| Check | Actual result |
| --- | --- |
| Source and metadata | 211 source files verified; consumed selective-metagraph method and all 77 fields unchanged; strict 479-to-481 metadata delta verified |
| Build guards | Three deterministic controls passed: reviewed pins, old-tag refusal and changed-source refusal |
| API/catalog regressions | Baseline two expected API2-only failures; fixed 24 roots and 22 negative subcases passed under `-race`, with retained API2 and independent-authority controls |
| Node build | Pinned Rust 1.89 release node with `metadata-hash fast-runtime`; session 43737 joined exit 0 |
| Captured chain Wasm | Session 93353 joined exit 0; exact code hash, complete version/API reply and metadata bytes matched at one non-authoring local genesis |
| Complete rehearsal package | Session 16800 joined exit 0; all 11 roots and 28 nested cases passed in 351.759s |
| Separate R1 deployment rehearsal | Session 95789 joined exit 0; `TestR1SettlementV2Phase` passed in 26.93s, package 26.993s |

The package command exercised smoke, transient storage, stake-exit admission,
ledger/Merkle, R2, R3, both R4 roots, R5 with R6 and both R7 roots. All ten
package node-start lines and the separate R1 node line named exactly
`node-subtensor/481, reviewed source 6a972693ae9580541225bd37696ac2fee26b8ae9`.
Neither invocation had a failed or skipped test. The actual commands used
`-tags runtime_rehearsal -count=1 -v`, `GOMAXPROCS=4`, `GOFLAGS=-p=2`, and the
90-minute package / 30-minute R1 timeouts shown above. The node binary SHA-256
was `cf5e271549c5e9ea5cb4c6eb8ea2ec2610aa6bcc08a423f9343ed5f5618b0961`.

R1 completed vault creation, escrow registration, settlement implementation,
initialized proxy, vault linkage and claim-batcher creation through the real
local precompiles and receipt/state readback. Vault creation and escrow
registration shared block 32. Each action stayed within its approved test gas
limit; the review records all six measurements. Its custody and approval were
synthetic test fixtures; installation completion retained
`ActivationReady=false` and supplies no production signing authority.

API3 is admitted only for the exact `node-subtensor/481/1/1` tuple. It adds an
emission-weights query while preserving the consumed selective-metagraph
interface. Runtime 481 does change the five-bucket emission allocation, root
baseline and accrued-owner refund semantics; passing metadata and rehearsal
checks does not mean all economic behavior is unchanged. Source catalog
membership remains separate from current artifact authentication, owner
approval and migration completion.

The captured production Wasm SHA-256 is
`48225f3199bf317966d0214588bc45fb49c28c3e7bd510293bb4da7d6aea224b`, matching
the official release asset. The built fast-runtime Wasm is a different
artifact, SHA-256
`dbd9fe4eac85aea63092f220af4f77b3405ca908100d5d4fa2721e1d0bac4461`.
Independent source-to-production-Wasm reproducibility is not established.
Captured-Wasm execution proves only the locally executed `Core_version` and
`Metadata_metadata` results; its retained raw shutdown-task message and owned
cleanup are documented in the review. Fresh finalized runtime/code/metadata
observations and production approvals remain required before deployment.

## Runtime 486: the phase C upgrade (R-8)

Mainnet ran runtime 486 at finalized block 9,405,583 (2026-10-31). The phase C
upgrade of `STSettlement2` (`VERSION2-FINAL.md` §16) was rehearsed on 486 on
2026-10-31. Only R-8 ran; the rest of the R battery has not been rerun on 486.

The node was built from source `d665b0eb4c95809f985ddf0c5a100253b496a74e`,
tree `2e5c57e38129232f8dbe625399de82c14c0be3e2`, the tree of upstream tag
`v486` (merge commit `72323e13249c9e25c7e792a2b3c08502c84651d5`), whose spec
version is 486. It used `build.sh`'s cargo invocation with
`metadata-hash fast-runtime`. `build.sh` itself still pins 482. The build ran
natively on macOS 27 (Xcode 27.2, `ld-27106.1`).

That linker left one host proc-macro dylib, `libsqlx_macros` (sqlx-macros
0.8.6, used only by rustc to compile Frontier's SQL client backend), with its
LINKEDIT string pool 4 bytes off 8-byte alignment. The cause was an odd
indirect-symbol count. dyld refused it with "mis-aligned LINKEDIT string pool".
The fix:

- Its string pool was moved 4 bytes up, into the zero padding before its code
  signature, and `LC_SYMTAB.stroff` was set to match.
- It was re-signed ad hoc.
- Nothing in the runtime Wasm, the EVM or the node source is built from it.

The resulting fast-runtime Wasm has these hashes, and the dev chain's genesis
`:code` is that Wasm byte for byte:

- SHA-256 `dc7fc0918700e1a6ef9d253a122e5767eb81817dd96a853702469ee184ac720f`
- BLAKE2b-256 `ef2dc33ade2025a2ca7dc4da6ffab9fa36cdc911096d32cb8642393d13be513a`

```sh
RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
  go test -tags runtime_rehearsal -count=1 -v -timeout=90m -run '^TestR8PhaseCUpgrade$' ./rehearsal/
```

It passed in 327.885s. The node-start line named
`node-subtensor/486, reviewed source d665b0eb4c95809f985ddf0c5a100253b496a74e`.

As on mainnet, the sweep epoch was the pool's first v2 epoch. v1 and v2 both
closed it at one boundary. Then, in order:

1. The v2 root was committed and approved.
2. The single-leaf v1 sweep root was committed.
3. The implementation was created through the deterministic deployment
   proxy's exact code, using 2,238,297 gas for 9,968 runtime bytes.
4. The owner's `upgradeToAndCall(implementation, 0x)` used 37,253 gas. It kept
   every slot 0–50 and the pending root's approval.
5. The v1 root finalized, and its claim paid vault2's coldkey.
6. `absorbStake` turned that into surplus.
7. `recordV1Sweep` used 134,314 gas.
8. `assignSurplus` used 68,773 gas for 37,505,621,562 rao to epoch 2.
9. The approved root finalized and paid through the new implementation.
10. Epoch 2's close snapshotted the surplus with its own capture.

Every refusal R-8 names reverted with its own error on the runtime.

