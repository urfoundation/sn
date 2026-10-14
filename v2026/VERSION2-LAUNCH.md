# Version 2: launch plan

Prepared 2026-10-13. This is the runbook for deploying and activating
settlement v2, phase A of [VERSION2-FINAL.md](VERSION2-FINAL.md), on SN25
mainnet. The owner runs it. The plan of record wins wherever the two differ.

Pins. The first round of merges is done and pushed (STATE.md:276-281,297).
These `main` heads are the runbook's premises, and its file and line
citations hold at them:

| Repository | `main` head |
| --- | --- |
| sn, this repository | the revision that holds this runbook |
| server (`server/`) | `54ae2aacf` |
| sdk (`sdk/`) | `3bc7ec95` |
| connect | `f7318bca` |
| localizations (`localizations/`) | `3a1e7ca` |
| apple | `70f3ce7a` |
| android | `a46813ea5` |
| windows | `e549203` |
| linux | `3332164` |
| mmm | `e54f1fe46` |
| web | `9526a97b` |

The heads were read again late on 2026-10-13, after the second-round merges.
connect's and web's did not move.

The second round is under way (STATE.md:298).
- Merged in sn: `v2/launch-tools` (`c296b255`), `v2/launch-examples`
  (`9dc41365`), `v2/runtime-477` (`c412a293`), `v2/launch-tools-2`
  (`e2000ac3`) and `v2/gate-preflight` (`57bbf917`).
- Merged elsewhere: the consent branches of the sdk, every app, mmm and the
  localization store; the server's v2 close floor (`54ae2aacf`); the SDK's
  v2 claim floor (`3bc7ec95`); and ur.io's use of the SDK's claim gas
  (`e54f1fe46`).
- Still to merge: sn `v2/runtime-478`; sn `v2/release-local`, the local-mode
  release tooling (step 1); server `v2/launch-latch`; the claim floor in
  sn's own claim tools; and the server's v1 close floor and the fix for a
  stranded root key (section 7).

Step 1 freezes whatever is on `main` at that moment. For step 8 that must
include the reviewed source of runtime 478, which lands through
`v2/runtime-478` (step 8.3).

The orchestration log is [VERSION2-STATE.md](VERSION2-STATE.md), cited as
STATE.md. It is a snapshot of 2026-10-13 20:15 UTC; its lines 1 to 286 keep
the log's numbers. Nothing here has been run against mainnet. Unprefixed paths
are in this repository.

Conventions:

- **Owner** is the owner, with the Ledger-held keys
  ([mainnet/LAUNCH.md](mainnet/LAUNCH.md):161-166). **Operator** is whoever
  runs the UR server, the validator host and the tooling.
- A bare § cites VERSION2-FINAL.md. This runbook's own parts are called
  sections and steps.
- `O1` to `O26` are owner inputs (section 3). A step does not start until its
  inputs are fixed.
- Each step gives **Who**, **Do**, **Expect** and **Stop if**. To stop is to
  change nothing further, keep every journal and output, and decide with
  section 6. The steps the server and the validator run by themselves give
  **Act if** instead.
- Blocks are counted as the contracts count them. A mainnet epoch is 50,400
  blocks, the commit window 1,200 and vault2's challenge window 14,400
  (VERSION2-FINAL.md §5). At 12 seconds a block these are 7 days, 4 hours and
  48 hours (LAUNCH.md:29-32; WHITEPAPER.md §8.3).
- A file pin is `sha256:` and then the file's SHA-256 as 64 lowercase hex
  characters. Every `--…-sha256` flag in this runbook takes that form, and so
  do `--signed-transaction-hash` and `--escrow-signed-transaction-hash`. A
  bare digest is refused (`mainnet/plan_input.go:111-113`; connect
  `durablevolume/config.go:100-106`).
- The templates for steps 8.2, 8.3, 10 and 13 are indexed in
  [mainnet/SETTLEMENT-V2-EXAMPLES.md](mainnet/SETTLEMENT-V2-EXAMPLES.md).
  Replace every `<...>` placeholder in them. In JSON and YAML a placeholder
  is a quoted string, also where a number belongs: "write numbers bare"
  (:7-8). "A test fills each file's placeholders with synthetic values and
  runs it through the real parser" (:9-10).
- A gap is something the repositories do not settle. Each is marked **Gap**
  and listed again in section 8.

## 1. Purpose and scope

### 1.1 What ships

- **Contracts**, beside the unchanged v1 set (VERSION2-FINAL.md §7):
  - `STSettlementVault2` ("vault2", `evm/src/STSettlementVault2.sol`),
    immutable, which holds the funds;
  - `STSettlement2` (`evm/src/STSettlement2.sol`) behind an `ERC1967Proxy`,
    UUPS, owned by the governance Safe;
  - optionally `STClaimBatcher` (`evm/src/STClaimBatcher.sol`), immutable,
    ownerless and stateless (§10.2).
- **Deployment tooling**
  ([mainnet/BOOTSTRAP-CONTRACTS.md](mainnet/BOOTSTRAP-CONTRACTS.md)):
  - the settlement v2 phase of `sn-mainnet bootstrap-contracts` ("Settlement
    v2 contract phase");
  - `sn-mainnet register-operator-pool`, which reviews and reads back the
    activation call ("Settlement v2 activation").
- **Operator server** (VERSION2-FINAL.md §8):
  - the v2 epoch driver and the ledger builder;
  - `GET /sn/ledger/object` and `GET /sn/ledger/account`, public
    (`server/api/api.go:337-338`);
  - the v2 readers: the earnings email, `/account/epochs` and the event
    mirror (§8.4);
  - the commands `v2-unrecoverable`, `v2-rebuild` and `v2-rebuild-withdraw`
    (§8.6);
  - migrations 821 and 822, new tables only
    (`server/db_migrations.go:10013-10049`);
  - five alerts (`server/controller/st_ledger_v2_driver.go:50-56`).
- **Validator** (§9, §13): the optional `settlement_v2` config block, the
  ledger audit, approvals, challenges and demands, the shared weight resolver
  and the "settlement v2 adoption" successor class.
- **Provider tooling** (§10): `provider claim-daemon-v2` and `snclaim v2`.
- **SDK and apps** (§11): v2 balances beside v1, and v2 claims.

### 1.2 What "on" means

Settlement v2 passes through three states, in this order.

1. **Shipped, v2 off.** The new binaries run with today's configuration.
   - The server keeps v2 off while `settlement_v2_address` and
     `vault_v2_address` are absent (`server/st_ledger_v2_config.go:11-17`).
   - The validator reads nothing for v2 without its `settlement_v2` block
     (`validator/settlement_v2.go:44-51`). Today's `validator.yml` and history
     bundle hash identically under the new binary (VERSION2-FINAL.md §13).
2. **Configured, not active.** The server and the validator name the deployed
   contracts, but no pool is registered.
   - The server's v2 driver runs only the prune check, and the v1 driver keeps
     every epoch (`server/controller/st_ledger_v2_driver.go:255-280,308-325`).
   - The validator weights every epoch of an unregistered pool through
     `operatorAt` (§9.1).
   - The apps show the v2 balances the ledger returns, of which there are
     none yet, and offer no v2 claim until `Vault2Address` is pinned
     (`sdk/sn_chain.go:83-88`).
3. **Active.** Phase A activates when the owner Safe sends
   `registerOperatorPool` (§14). That call is the commitment point (§12.1
   step 5). From its `firstEpoch`, called E_s:
   - `settlementAt` returns vault2 and the v2 pool hotkey (§4.3 S4;
     `evm/src/STSettlement2.sol:99-108`);
   - the validator weights the v2 pool UID (§9.1);
   - the server's v2 driver closes, commits, finalizes and answers demands,
     and the v1 driver never closes or commits an epoch at or after E_s
     (§8.1; `server/controller/st_ledger_v2_driver.go:255-280`);
   - epoch E_s − 1 completes on v1 in its normal windows (§12.1 step 7).

In this runbook "on" means state 3. The first v2 funds move at the close of
epoch E_s, within 1,200 blocks of its end. The first claimable balances appear
when that root finalizes, at least 14,400 blocks after its commit (§5).

### 1.3 Not part of this launch

- **Later phases** (VERSION2-FINAL.md §16). Policy v1 stays in force (§2.2).
  The v1 contracts, deployment id, evidence journal, `verify.yml`,
  `provider_work.yml`, the policy file and the launch readiness digest are
  unchanged (§13).
- **Network payout consent** ships on its own. The owner chose network payout
  consent signing in the SDK and every app (owner decision, 2026-10-13). It is
  VERSION2-FINAL.md §11.1. One sr25519 signature by the coldkey, requested by
  the network's owner, covers every provider client of the network.
  - It is independent of activation and is not an activation gate (§11.1).
  - The operator accepts only a prospective consent: its `from_epoch` must
    exceed the current epoch at the finalized block. A consent therefore takes
    effect from the epoch after its signing, and epochs before a network's
    first consent stay unpaid (§11.1, "Earnings range").
  - v1 also pays consent-selected wallets (§2.1, decision 4). A consent signed
    before E_s therefore counts for the v1 epochs from the one after its
    signing, and then for v2.
  - v2 stays consent-only (§3; owner decision, 2026-10-13). Until its network
    signs, an app-only provider stays unpaid, in v2 as in v1.
  - Recommended: step 7's app release carries consent signing, so network
    owners can sign at least one epoch before E_s and be paid from E_s.
  - Every native app signs the consent through ur.io's bridge page, so ur.io
    is deployed before, or with, any app release that carries consent signing
    (STATE.md:268). It needs the consent bridge page (mmm `v2/consent-signing`,
    `9e2e8c2b5`) and the bridge fix (mmm `main`, `64173fd13`; STATE.md:297)
    (step 7).
- **The legacy USDC reconcile** is a separate, adjacent track. Its run order
  is:
  1. `bringyourctl payouts correct-legacy-attempts`, dry run, then `--apply`;
  2. `bringyourctl payouts reconcile-legacy-attempts`, dry run, then
     `--apply`;
  3. wait until the sender completes every attempt those two scheduled;
  4. `bringyourctl payouts sn-reconcile`, dry run, then
     `--send --expect=<digest>`.

  The source is the runbook on server branch `v2/sn-reconcile` (`513d600d`,
  `SN-RECONCILE.md`:40-59) and STATE.md:211-212. The track meets this launch
  in one place. sn.yml is parsed with known fields only
  (`server/provider_payout_transition.go:57-58`), and that branch's parser
  has no v2 fields. So run the reconcile before step 11 adds the v2 keys to
  sn.yml, or with a binary built from a revision that has both. This is an
  inference from the code, not a documented rule.
- **The dual-stack validator change** (`v2/dual-stack-hops`) is still in
  progress. The merge into `main` excluded it, and it stays out of scope
  until it is finished (STATE.md:244). It has its own rollout order: the
  server's API and taskworker first, then its connect service, then the
  validators, with providers and clients in any order (connect `IPV6.md`
  lines 312-317 on that branch). Its egress-index "v2" and "v3" are unrelated
  to settlement v2.

## 2. Preconditions and gates

Every item of sections 2.1 to 2.7 holds before step 5, the first step that
changes production. Gates 1 to 4 are VERSION2-FINAL.md §14's. Gate 5 (software
released and contracts deployed) and gate 6 (approvals) complete inside the
runbook, at steps 9 and 12.

### 2.1 Code merged to main and deployed

- The owner's instruction of 2026-10-13 was "Commit changes. Merge branches
  into main. Pull and push." (STATE.md:244). The first round is merged and
  pushed, without force, after a fresh fetch and a fast-forward check
  (STATE.md:278-281). The `main` heads are the Pins table above.
- The second round is under way (STATE.md:298). The Pins paragraph lists
  what is merged and what is still to merge. Step 1 freezes whatever is on
  `main` at that moment, and the runbook is then read against those heads.
- `v2/dual-stack-hops` is not in `main` (STATE.md:244).
- The localization store's `main` (`3a1e7ca`) holds the eight `sn_ledger_*`
  keys (VERSION2-FINAL.md §11) and the eleven `sn_consent_*` keys (§11.1).
  Every release build regenerates the app
  catalogs from the store (`localizations/README.md`:23-25), so each app is
  built against that store or a later one.
- The server is rebuilt against the sn revision that carries the new
  successor class (VERSION2-FINAL.md §13, Server row).
- Every launch binary is built from the frozen `main` (step 1).

### 2.2 CI green

- No repository has CI. The workflows were removed on 2026-10-05 and
  2026-10-06: "We build and test on our own hardware and do not use GitHub
  Actions" (sdk commit `7120b2cf`, with the matching removals in the app
  repositories and the localization store). So "CI green" means the owner's
  build pipeline and the hand-run suites below.
- The release gate of section 2.5 passes on the merged `main` (step 4).
- The apps' v2 code was not compiled where it was written (STATE.md:174,
  219, 228). Each app is built and its v2 suites pass:
  - apple: the build, `SnLedgerTests` (`app/networkTests/SnLedgerTests.swift`)
    and `AppShortcutsCatalogTests`;
  - windows: the build, and `go test ./tests`, which holds `TestSnPayoutLine`,
    `TestSnPayoutAgainstSdkHeader` and `TestSettlementV2Wiring`
    (`tests/sn_payout_test.go`);
  - linux: meson, msgfmt and the glibc floor, and meson's `unit` test, which
    builds `tests/SnLedgerPresentationTest.cpp` (`app/meson.build:613,853`);
  - android: the `androidTest` compile, and the unit tests
    `app/app/src/test/java/com/bringyour/network/ui/wallet/SnLedger{Tile,Claim,Wiring,Strings}Test.kt`;
  - mmm: `npm run test:sn-ledger-v2` in `ur.io/react` (`ur.io/react/package.json:79`).

### 2.3 The runtime against the rehearsal

- vault2 depends for good on the ABIs of the staking precompile's `getStake`,
  `getMinerCollateral`, `moveStake` and `transferStake`. A runtime that changes
  or drops one freezes every capture and claim, and vault2 cannot adapt
  (VERSION2-FINAL.md §17, third row).
- Gate 2 ran on a node built from runtime 475's source. "Any runtime change on
  mainnet repeats this gate" (§14 gate 2).
- Subtensor replaces its runtime every few days
  ([mainnet/VALIDATOR-PRODUCTION-RUNTIME.md](mainnet/VALIDATOR-PRODUCTION-RUNTIME.md):39).
  Step 2 checks it, and is repeated immediately before step 8 and before
  step 15.
- Mainnet moved from 475 to 477 on 2026-10-12 and to 478 on 2026-10-13
  (`docs/spec/runtime-477-audit.md`:29-46). Gate 2 holds for 477: the rerun
  is recorded (:405-408). The contracts will be deployed on 478, so gate 2
  must hold on 478. Its rerun there passed: R-1 to R-7 all pass on a node
  built from v478 (section 9.3, item 2). Step 8 still waits for 478's
  reviewed-source entry and a rebuild.

### 2.4 R-1 to R-7 on the then-current runtime

- The R battery runs the exact release bytecode on the rehearsal node
  (VERSION2-FINAL.md §15.8): R-1 the v2 contract phase with readback, R-2
  capture, R-3 claims, R-4 registration, R-5 approval and demands, R-6 gas and
  R-7 batched claims.
- It is rerun at launch on the runtime step 2 finds (step 3).

### 2.5 The release gates

- Gate 1: the Foundry suite at its coverage bars, `forge fmt --check`,
  `forge build --deny warnings --sizes` and `scripts/test-solidity-static.sh`,
  sizes recorded; and an independent review of vault2, `LedgerTree.sol` and
  `STSettlement2`, resolved (§14).
- Gate 3: the GT, GA, GL, SV, VA and CL suites, including GL-10 and SV-6
  inside their budgets.
- Gate 4: the E suite, E-1 to E-7.
- One command runs all of them (step 4).

### 2.6 Funding and gas

| Payer | Pays for | Size, from the rehearsal | Source |
| --- | --- | --- | --- |
| Deployer (O4) | the six v2 actions, and the escrow registration's value | limits 4,000,000, 2,500,000, 3,000,000, 1,000,000, 150,000 and 400,000; used 2,821,690, 1,463,833, 1,935,622, 310,486, 31,329 and 285,209; value `maximumBurnRao × 10^9` wei | BOOTSTRAP-CONTRACTS.md:660,706-721 |
| Governance Safe (O8) | `registerOperatorPool`'s value, which the Safe must hold before step 15's review; the surplus over the burn comes back to the Safe | R-4 sent a 100 TAO ceiling, burned 3.80 TAO and used 1,594,088 gas. Through a SafeL2 1.4.1 `execTransaction` the dry run used 1,654,438 gas, estimated at 1,797,143 (step 15) | `scripts/runtime-rehearsal/README.md` R-4; `rehearsal/stack_test.go:31-32,190-194` |
| Operator root key | every v1 and v2 close, commit, finalize and answer | the §8.1 table; a capturing close needed a limit of about 2.69M. The server signs every v2 close at max(estimate + 20%, 3,200,000) | VERSION2-FINAL.md §8.1; `server/controller/st_controller.go:1079-1091`; `server/controller/st_ledger_v2_client.go:357-387` |
| Validator auditor key (O10) | approvals, challenges and demands | approve: 132,524 used, limit 135,736; demand: 98,268 used, limit 104,424, plus the fee | §8.1 table |
| Watcher key (O21) | demands and challenges | each demand pays `demandFeeWei`, 750,000 rao, plus gas; a sample starts at 256 demands and doubles | §7.1, §9.3, §10.2 |
| Claimants: the daemon relayer, app gas keys, anyone | claims | an account's first claim uses 186,707 gas and needs a limit of 2,572,776; in a batch of 32, 147,740 gas a claim | §8.1 table; README R-7 |
| App gas key | v2 claims from the apps | the SDK signs each v2 claim at max(estimate + 25%, 2,600,000) gas, and the gas key must hold that limit × the price, or the claim fails with `needs_gas`. With legacy pricing, the node's price plus 10%, that is 0.0143 TAO at 5 gwei, so §11's 0.02 TAO holds up to about 7 gwei. EIP-1559 pricing, a fee cap of twice the base fee plus the tip, needs about 0.031 TAO. ur.io passes the SDK's limit to the wallet | §11; `sdk/sn_ledger_claim.go:74-88,318-339`; `sdk/sn_claims.go:907-940`; mmm `ur.io/react/src/app/sn/ledgerV2.js:168-175` |

- The deployer's `maximum_total_wei` covers every limit times its fee cap,
  plus the escrow funding (BOOTSTRAP-CONTRACTS.md:720-721). Each send's
  admission also needs the deployer's pending balance to cover the selected
  action and every later reservation of the same sender (:239-244).
- The operator gas policy caps every operator transaction. Today's
  `maximum_gas` of 1,000,000 and per-intent liability of 5·10^16 wei are below
  a v2 close's floor, so the policy is re-signed before E_s (step 5;
  VERSION2-FINAL.md §8.1, §13).
- **On runtime 478, as measured.** The table's sizes are from the rehearsal on
  475. The whole R battery passed on a node built from v478 (section 9.3,
  item 2). `docs/spec/runtime-478-audit.md` and the README's "Runtime 478"
  record them:
  - the deploy gas is unchanged from 475. R-1's six actions used 2,821,678,
    1,463,833, 1,935,622, 310,474, 31,329 and 285,209 gas, under the table's
    limits;
  - each `moveStake` or `transferStake` costs 625 gas more;
  - the least limits that `eth_estimateGas` gives collapse: 246,899 for a
    capturing close and 206,967 for a first claim. A capturing close used
    226,055 gas, with a least limit of 236,399 at which an `eth_call` of it
    succeeds. A first claim used 187,332 gas, with a least limit of 192,318;
  - a batch of 32 uses 148,358 gas a claim, and a transaction holds at most
    416 claims, against 418 on 475;
  - a stake exit's admission now grows by 8,750 gas for each entry of
    vault2's own staking-hotkey list. The list holds up to 128 entries, which
    is about 1.12M more. Third parties can lengthen that list (section 7). So
    a limit sized from an estimate taken at one list length can fall short
    at a longer one.
- **The sizing rule on 478**, approved by the owner: never sign a stake-moving
  vault2 transaction below its pre-478 size.
  - A close is signed at max(estimate + 20%, 3,200,000) gas.
  - A claim is signed at max(estimate + margin, 2,600,000) gas.
  - Those two sizes cover every length of the list, for a capturing close
    that defers at most 94 epochs. A close that defers more is signed at no
    less than 1,400,000 + 19,100 gas per deferred epoch
    (`docs/spec/runtime-478-audit.md`, "Gas limits on 478"). `maximum_gas`
    stays at about 3.5M; step 5 sizes the per-intent liability for the close
    floor.
  - **The server** applies it on its `main` (`54ae2aacf`): every v2 close is
    signed at max(estimate + 20%, 3,200,000), on a first attempt and on a
    fee replacement (`server/controller/st_controller.go:1079-1091`;
    `server/controller/st_ledger_v2_client.go:357-387`). Its tests are SV-7
    (`server/controller/st_ledger_v2_gas_test.go:85-323`). The operator gas
    policy still caps the floor, so step 5 sizes the policy for it. Deploy a
    server build at or after `54ae2aacf` before the first close, at block
    9,348,028 (section 9.1).
  - **The SDK** applies it on its `main` (`3bc7ec95`): every v2 claim is
    signed at max(estimate + 25%, 2,600,000), and an external signer is
    given 2,600,000 (`sdk/sn_ledger_claim.go:54-58,318-339,421`). ur.io sends
    each claim with that limit and refuses claims without it (mmm
    `ur.io/react/src/app/sn/ledgerV2.js:168-175`).
  - **sn's claim tools** sign a single claim, from `snclaim` or the claim
    daemon, at max(estimate + 20%, 2,600,000) (`miner/onchain/eth.go`,
    `runTx`; `miner/claim_v2_daemon.go`, `submit`), so a signed claim also
    passes the daemon's rebroadcast preflight after the list grows. A batch
    already forwards each claim the larger of 2,600,000 and the batch's
    largest estimate (`miner/onchain/v2_batch.go:28-42,181-208`). R-3 shows
    the floor on 478 (`docs/spec/runtime-478-audit.md`, "vault2's own
    staking-hotkey list").
  - **The v1 close** shares the root key and keeps its estimate plus 20%, with
    no floor (`server/controller/st_ledger_v2_gas_test.go:134-138`). Its
    floor is being built (section 7).

### 2.7 Key custody

| Key | Role in this launch | Custody | Source |
| --- | --- | --- | --- |
| Deployer EVM key (O4) | sends the six v2 actions at consecutive nonces | an external EVM signer; the v1 deployer is on the owner's Ledger | BOOTSTRAP-CONTRACTS.md:652-655; LAUNCH.md:161-163 |
| Ed25519 approval key | signs the v2 phase approval | `root/subtensor/approval/`, operator-held | LAUNCH.md:185,369-376 |
| Governance Safe owner and Safe relayer (O8) | own `STSettlement2`; send `registerOperatorPool`; upgrade; pause | the owner's Ledger; the Safe is 1-of-1 | LAUNCH.md:47-48,164-166,187-188; VERSION2-FINAL.md §4.1 |
| Guardian | pauses v1 settlement, and with it v2 | the owner's Ledger | LAUNCH.md:161-163; VERSION2-FINAL.md §4.1 |
| Operator gas approver key | signs the operator gas policy | off the operator host; the server refuses any other approver for the scope | `server/st_operator_gas_policy.go:24-25`; `server/model/st_transaction_gas_model.go:386-388` |
| Treasury approval key | re-signs the treasury production approval | the Ed25519 key the validator config pins as `treasury_approval.signer` | `mainnet/treasury_approval.go:192-251`; `validator/treasury_approval_draft.go:64-66` |
| Operator root key | signs v2 closes, commits, finalizes and answers | `root_key` in `st.yml`, in the deployment vault | `server/controller/st_controller.go:251`; `server/controller/st_ledger_v2_client.go:389-395`; VERSION2-FINAL.md §7.2 |
| Validator auditor key (O10) | approvals, challenges and demands | a hex secp256k1 key file on the validator host, `--ledger_auditor_key_file` | `validator/run.go:142-143` |
| Watcher key (O21) | `snclaim v2 check --all --challenge --demand` | a host other than the validator's | VERSION2-FINAL.md §10.2 |
| Claim daemon relayer key (O22) | claims | the daemon config's `key_file` | `miner/claim_v2_daemon.go:61` |
| SN25 owner, `ur-owner` | holds SN25's stake-transfer switch, which can freeze every claim | 2-of-3 native multisig | LAUNCH.md:184,197-203,222-235; VERSION2-FINAL.md §4.1, §17 |

The deployer, guardian, Safe relayer and Safe owner keys are all on the
owner's single Ledger. The owner's availability is therefore a dependency of
steps 5, 8, 10 and 15 (VERSION2-FINAL.md §13, §17).

## 3. Owner inputs

| ID | Input | Constraint or default | Used in | Source |
| --- | --- | --- | --- | --- |
| O1 | The launch revisions: the `main` head of each repository that step 1 freezes | the Pins table or later, after the second merge round; without `v2/dual-stack-hops` | 1 | section 2.1 |
| O2 | The mainnet route: native and EVM RPC URLs, the HTTPS leaf SPKI pin if HTTPS, and the approved native chain name | the launch used Snow's Finney archive, `ws://172.28.208.185:9944`, or `http://` for EVM JSON-RPC. Every `sn-mainnet` command here takes only the `http(s)://` form; step 2 lists the scheme each command accepts. The route is part of each signed plan | 2, 8, 13, 15, 20 | LAUNCH.md:124-141; BOOTSTRAP-CONTRACTS.md:208-221; `mainnet/identity.go:74-79` |
| O3 | The runtime identity the v2 phase config binds: version, code and metadata hashes and the reviewed source commit | for runtime 478: its version, code hash and metadata hash from the owner's own `runtime-snapshot`, taken immediately before step 8, and the reviewed 478 source commit `850f2755bb05a3851f948489f0ab27b0645cebce`. The binary must list that commit beside that version; today it lists 470, 473, 475 and 477, not 478 (step 8.3) | 8 | BOOTSTRAP-CONTRACTS.md:208-210,669-678; `crv4/native_owner_source.go:6-29`; `mainnet/evm_phase.go:183-191` |
| O4 | The v2 deployer: its EVM address, next nonce and signer | neither the Safe nor an auditor; funded per section 2.6 | 8 | BOOTSTRAP-CONTRACTS.md:652-655,751-752 |
| O5 | The escrow hotkey | 32 bytes, nonzero, with no UID on SN25 at the send; not the pool hotkey | 8 | BOOTSTRAP-CONTRACTS.md:729-736, and "Back-to-back escrow registration (A7)" |
| O6 | The escrow registration cap `maximumBurnRao` | nonzero; the value is the cap × 10^9 wei | 8 | BOOTSTRAP-CONTRACTS.md:660,742-744 |
| O7 | The phase envelope: each action's gas limit and fees, `maximum_total_wei`, `maximum_attempts`, the native start checkpoint and send window, the source-lock, cutover and custody-fence evidence hashes, the custody identifier and the run directory | gas starts from BOOTSTRAP-CONTRACTS.md:714-717; `maximum_attempts` at most 8 across all actions, the back-to-back escrow send included; the start checkpoint taken just before the approval is signed, and the window at most 7,200 blocks (step 8.3). The three evidence pins are only format-checked: the binary signs them into the plan, and nothing compares them with a file. The fill recipe suggests the frozen checkout's `sn-mainnet source-lock` output for the source lock and step 8.2's fence file for the custody fence; it names none for the cutover evidence | 8 | BOOTSTRAP-CONTRACTS.md:208-213,682-686; `mainnet/evm_phase.go:59-62,180`; `mainnet/SETTLEMENT-V2-FILL.md`, section 2 |
| O8 | The `STSettlement2` owner and its signers | the governance Safe `0x56F4Dad575576CC0B679FEf52899630f9F605418`, 1-of-1, owner `0x16C372dbBb24cd8473345ab13971E40814C8658F` at the v1 launch; confirm | 8, 15 | LAUNCH.md:47-48,164-166; VERSION2-FINAL.md §7.2 |
| O9 | The v1 coordinator for `initialize` | the proxy `0xC18925925E2B7bb9059b7d696b8c92762AE86406`; confirm | 8 | LAUNCH.md:42 |
| O10 | The auditor key and its EVM address | one address, quorum 1 at launch; the same key runs with `--ledger_auditor_key_file` | 8, 10 | BOOTSTRAP-CONTRACTS.md:749-753; VERSION2-FINAL.md §2.1 decision 2 |
| O11 | Whether to create the claim batcher | optional sixth action | 8, 13 | VERSION2-FINAL.md §8.5 |
| O12 | The demand fee | decided: 750,000 rao, 750,000,000,000,000 wei (owner decision, 2026-10-12); the command has no default | 8 | VERSION2-FINAL.md §7.1; BOOTSTRAP-CONTRACTS.md:733-738 |
| O13 | The re-signed operator gas policy: `revision`, `previous_policy_sha256`, `maximum_gas`, `maximum_intent_liability_wei`, `maximum_lifetime_liability_wei`, the validity window and the attempt caps | `maximum_gas` about 3.5M, and at least 3,200,000, the floor of every v2 close; intent liability at least 3 × `maximum_gas` × `maximum_fee_per_gas_wei`, 3.15·10^17 wei at 3.5M and 30 gwei; `maximum_intent_attempts` at most 3; same bindings (step 5) | 5 | VERSION2-FINAL.md §8.1, §13; `server/local/sn25golive/gas.go:26-28`; `server/model/st_transaction_gas_model.go:95-112,332-368` |
| O14 | The treasury re-sign inputs: the input file and the unsigned policy plan the live approval was drafted from, with their SHA-256, and the approval key's signature | unchanged; only `config_hash` may change. The written sequence is [mainnet/SETTLEMENT-V2-ADOPTION.md](mainnet/SETTLEMENT-V2-ADOPTION.md) | 10 | `mainnet/treasury_approval_test.go:534-557`; `validator/production_authority_history.go:368-388` |
| O15 | The adopted validator config: its absolute path, the predecessor's retained authority bundle, the progress file, the durable-volume declaration, and how the service command line gains `--ledger_auditor_key_file` | all paths absolute; the flag comes through the systemd drop-in [mainnet/validator-ledger-auditor.example.conf](mainnet/validator-ledger-auditor.example.conf) for the validator unit (step 10) | 10 | `validator/treasury_approval_draft.go:71-77`; `validator/run.go:112-117`; SETTLEMENT-V2-EXAMPLES.md:102-123 |
| O16 | The v2 pool hotkey (`poolHotkeyV2`) | new; nonzero; not the escrow hotkey; not used by another pool | 15 | `evm/src/STSettlementVault2.sol:166-171`; VERSION2-FINAL.md §12.1 |
| O17 | The pool registration cap `maximumBurnRao` and the value sent | the rehearsal sent the cap × 10^9 wei at the runtime's default MaxBurn of 100 TAO | 15 | `rehearsal/stack_test.go:31-32,190-194` |
| O18 | E_s and the registration date | decided: E_s is epoch 2, which starts at block 9,297,628 (owner decision, 2026-10-13: "we are launching v2 now to be active the next ur epoch"). Section 9 gives its schedule and the departures the owner must still accept | 14, 15 | section 9; VERSION2-FINAL.md §12.1 step 5, §14 |
| O19 | `v2_prune_alert_positions` | default 3 | 11 | `server/controller/st_ledger_v2_prune.go:33-35` |
| O20 | `--ledger_max_bytes` | default 2147483648 (2 GiB); a runtime flag, raised without a re-sign | 10 | `validator/run.go:144-145`; VERSION2-FINAL.md §6.6 |
| O21 | The watcher: host, key, RPC and fee budget, and how often it runs | a host other than the validator's; the example units run it hourly as the system user `snclaim-watcher` | 13 | VERSION2-FINAL.md §10.2; SETTLEMENT-V2-EXAMPLES.md:143-161 |
| O22 | The claim daemon: coldkeys, relayer key, state directory, threshold and batcher | threshold default 1,900,000,000 rao (1.9 α), never below 15,200,000 rao; the state directory is a durable root of its own, prepared as `provider-claim-queue` (step 13) | 13, 20 | `miner/run.go:195-197`; `miner/claim_v2_daemon.go:39-112`; SETTLEMENT-V2-EXAMPLES.md:125-141 |
| O23 | The SDK release that pins `Vault2Address`, and the app release dates | lands before step 15 | 9 | VERSION2-FINAL.md §11; `sdk/sn_chain.go:83-89` |
| O24 | Who runs the first claim, and for which account | anyone may claim for anyone; payment goes to the leaf's coldkey | 20 | VERSION2-FINAL.md §1, §17 |
| O25 | Whether and how to hold the SN25 owner key `ur-owner` under the Safe's governance | recommended; no coldkey swap; no tooling | section 8 | VERSION2-FINAL.md §4.1, §17 |
| O26 | Gate 1's independent review: confirmation that it is resolved | the owner confirms | 4 | VERSION2-FINAL.md §14; STATE.md:91,99,103,176,189,198 |

## 4. Runbook

The order is VERSION2-FINAL.md §12.1's. Each sub-step sits where its inputs
first exist.

| Step | What | Who | Gate |
| --- | --- | --- | --- |
| 1 | Freeze the revisions and build | Owner, operator | |
| 2 | Check the mainnet runtime | Operator | |
| 3 | Rerun R-1 to R-7 | Operator | 2 |
| 4 | Run the release gates | Operator, owner | 1, 3, 4 |
| 5 | Re-sign the operator gas policy | Owner, operator | 6 |
| 6 | Ship the server v2 build, v2 off | Operator | 5 |
| 7 | Ship the validator, tools, SDK and apps, v2 off | Owner, operator | 5 |
| 8 | Deploy the v2 contracts with readback | Owner, operator | 5 |
| 9 | Pin `Vault2Address` and release the apps | Owner | 5 |
| 10 | Adopt the validator config, re-sign the treasury approval | Owner, operator | 6 |
| 11 | Switch the server config | Operator | 6 |
| 12 | Check what runs | Operator | |
| 13 | Start the watcher and set up the claim tools | Operator | |
| 14 | Schedule E_s | Owner | |
| 15 | Register the pool (activation) | Owner | |
| 16 | The switch at the start of E_s | Operator | |
| 17 | Close and commit epoch E_s | Server; operator watches | |
| 18 | Audit and approval | Validator, watcher | |
| 19 | Challenge window and finalization | Server | |
| 20 | First claims | Operator | |

### Phase A. Qualify; nothing in production changes

**Step 1. Freeze the launch revisions and build.**

- **Who.** Owner and operator.
- **Do.**
  1. Fix O1, and freeze in this order:
     1. the last merges of section 2.1;
     2. the local-mode release tooling, sn branch `v2/release-local`;
     3. the release-lock refresh, as sn's last commit, pushed (step 4);
     4. then the release gate (step 4).

     Step 1 freezes whatever is on `main` at that moment, and that last sn
     commit is sn's O1. Confirm section 2.1, including the localization
     store's `main`.
  2. Build `sn-mainnet` with Go 1.27.2. The pin is the `go` line of `go.mod`
     (`go.mod:3`, `go 1.27.2`; the server's `go.mod:3` is the same). Under
     `GOTOOLCHAIN=local` an older local toolchain is refused: "go: go.mod
     requires go >= 1.27.2 (running go …; GOTOOLCHAIN=local)". The command is
     [mainnet/MACOS.md](mainnet/MACOS.md):24-29's:

     ```bash
     GOTOOLCHAIN=local CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=true \
       -o /absolute/path/sn-mainnet ./mainnet
     ```

  3. Build the production composition, which holds `cli/validator`,
     `cli/snclaim`, `cli/miner` and the server services
     ([mainnet/RELEASE-BUILD.md](mainnet/RELEASE-BUILD.md):3-8,37-42), in the
     local mode:

     ```bash
     go run -mod=readonly ./scripts/mainnet-release-build --config /absolute/config.json
     ```

     - The launch builds in the local mode, from the frozen checkouts of sn,
       server, connect, sdk, gvisor and the other census repositories, each
       clean at its recorded commit. sn's and the server's `go.mod` resolve
       those modules to the sibling checkouts (`go.mod:205-234`; server
       `go.mod:212-235`). `main` keeps these local substitutions.
     - The config selects the mode with `"dependency_mode": "local"` and lists
       connect, sdk and gvisor among its `repositories`
       ([mainnet/RELEASE-BUILD.md](mainnet/RELEASE-BUILD.md), `dependency_mode`).
       Without the field the builder keeps the pinned mode, which accepts
       `github.com/urnetwork/sdk`, `github.com/urnetwork/connect` and
       `github.com/pion/sctp` only as checksum-bound released versions and
       refuses the local checkouts: "… must resolve to its checksum-bound
       reviewed release API" (`scripts/mainnet-release-build/sources.go:137-152`).
     - A pinned build version, from released module versions, is produced
       later by the owner's build
       (`scripts/mainnet-release-build/PIN-PROCEDURE.md`).
  4. Retain each binary's SHA-256 and `go version -m` output (LAUNCH.md:334-335).
- **Expect.** Every binary's build information names its O1 commit.
- **Stop if** a repository's `main` lacks its pin, `v2/dual-stack-hops` is in
  it, or the store's `main` lacks the `sn_ledger_*` keys.
- **For step 8.**
  - The frozen sn revision must list the reviewed source of the runtime that
    step 8 deploys on (step 8.3). That runtime is 478 (step 2). `main` lists
    477's source. 478's entry lands through sn branch `v2/runtime-478`.
    Freeze after that merge. If steps 5 to 7 cannot wait for it, freeze sn a
    second time at the merged `main`, and build step 8's `sn-mainnet` from
    that revision. The release lock and step 4's gate then follow that
    second freeze.
  - Clone the sn tree under umask 022. Step 8.1's `stabi` check and step 4's
    gate read the modes of checked-out files.

**Step 2. Check the mainnet runtime against the rehearsal.**

- **Who.** Operator.
- **Do.** Capture the finalized runtime (`mainnet/runtime_snapshot.go:183-224`;
  genesis from LAUNCH.md:114-119) and read its version and hashes (fields at
  `mainnet/runtime_snapshot.go:33-41`):

  ```bash
  sn-mainnet runtime-snapshot --rpc "$MAINNET_RPC" \
    --expected-chain "$APPROVED_NATIVE_CHAIN" \
    --expected-genesis 0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03 \
    --expected-evm-chain-id 964 > /secure/ur-mainnet/runtime-snapshot.json
  jq '.snapshot.runtime_version, .snapshot.runtime_code_hash, .snapshot.runtime_metadata_hash' \
    /secure/ur-mainnet/runtime-snapshot.json
  ```

- **The route's scheme.** `--rpc` here takes only an `http://` or `https://`
  URL. O2's `ws://` form exits 2: "RPC route must be an explicit HTTP(S) URL
  without credentials, query or fragment". For Snow's archive, use the same
  host and port with `http://` (LAUNCH.md:124-126). What each command of this
  runbook accepts:

  | Command | Accepts | Source |
  | --- | --- | --- |
  | `sn-mainnet runtime-snapshot --rpc` (steps 2, 8 and 15) | `http://` or `https://` only | `mainnet/identity.go:74-79`; `mainnet/runtime_snapshot.go:203-207` |
  | `sn-mainnet register-operator-pool review` and `readback`, `--rpc` (step 15) | the same client: `http://` or `https://` only; anything else exits 2 | `mainnet/evm_v2_pool.go:189-193` |
  | the phase config's `route.rpc_url`, which `bootstrap-contracts … --online` uses (step 8) | `http://` with a literal IP and an explicit port, or `https://` with the leaf SPKI pin `tls_spki_sha256` | `mainnet/owned_submission_http.go:36-66`; BOOTSTRAP-CONTRACTS.md:214-221 |
  | `snclaim … --rpc`, repeatable (steps 13, 16 and 20) | `http(s)://` or `ws(s)://` | `miner/onchain/eth.go:33-58`; `evmrpc/client.go:34-38` |
  | the claim daemon config's `rpc` list (step 13) | `http(s)://` or `ws(s)://` | `miner/claim_v2_chain.go:158-161`; `evmrpc/client.go:34-38` |

- **Expect.** The identity of a runtime that gate 2 holds on. The contracts
  are then deployed on that runtime, which is now 478 (below). The record of
  gate 2 covers three runtimes:
  - 475: `specVersion` 475, code hash
    `0x557634c8c31bc639ea6552e297dcfb781cd7d9bdd491a5352c151305db33d3a0` and
    metadata hash
    `0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff`
    (`docs/spec/runtime-475-audit.md`:84,93);
  - 477: code hash
    `0x18b52484d9c5d7523f704c86b17b2ab959fffbc6edb6766a77e9877f0ad19387` and
    metadata hash
    `0x5a268863a7f24c626e362103afa4f2856a589ee157ab915d91c289b1b5a6f11e`
    (`docs/spec/runtime-477-audit.md`:81,94-96). "Gate 2 therefore holds for
    runtime 477, and only for it" (:405-408);
  - 478: `specVersion` 478, code hash
    `0x7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1` and
    metadata hash
    `0x4f96a9cb954ebe3ae0e364fe7e34ec8a0e902239832f68dfe90aaa1e4511c747`
    (`docs/spec/runtime-478-audit.md`, "Exact provenance"). "Gate 2 therefore
    holds for runtime 478" ("Limits and assessment").

  The output is an unapproved observation, never an approval
  ([mainnet/MAINNET.md](mainnet/MAINNET.md):1653).
- **Observed already.** These are public nodes' answers, not the check of
  record. The owner's own `runtime-snapshot` above still decides, and it
  gives O3's version and hashes.
  - **478 is live.** Mainnet installed runtime 478 in block 9,276,925, at
    2026-10-13 21:08:36 UTC, with code hash
    `0x7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1`
    (`docs/spec/runtime-478-audit.md`, "Exact provenance"). Two public nodes reported
    `node-subtensor/478` at finalized block 9,276,937. The snapshot's
    `runtime_code_hash` is the BLAKE2b-256 hash of the same `:code` key
    (`mainnet/runtime_snapshot.go:29,107-124`), so on 478 it should print
    this value.
  - v478, commit `850f2755bb05a3851f948489f0ab27b0645cebce`, "sends the
    staking precompile's `moveStake` and `transferStake` through a new
    dispatch path that changes the gas limit they need" (:377-385). vault2
    depends on both for good (section 2.3).
  - **477 ran before it,** from block 9,267,632 through 9,276,924,
    byte-identical to the official v477 release asset (:29-46,391-394).
  - A runtime can land soon after it is proposed. 478 was proposed at block
    9,276,607 and installed 318 blocks later (:377-383). 475's proposal
    executed 196 blocks after it was made
    (`docs/spec/runtime-475-audit.md`:142-144), and 477's 26,475 blocks after
    (`docs/spec/runtime-477-audit.md`:137-140).
- **Repeat** this step immediately before step 8 and immediately before
  step 15. A runtime can land between any two steps.
- **Stop if** the version, the code hash or the metadata hash is not that of
  a runtime gate 2 holds on. Gate 2 then repeats on that runtime before
  step 8. For 478 the rerun passed (section 9.3, item 2). A runtime after 478
  needs its own.
  - **Pins.** `scripts/runtime-rehearsal/build.sh` builds only tag `v478`,
    commit `850f2755…`, against `docs/spec/runtime-v478-source.sha256`
    (build.sh:13-14,21,45). Another runtime first needs a reviewed change of
    those pins and a source review like `docs/spec/runtime-478-audit.md`.

**Step 3. Rerun the runtime rehearsal, R-1 to R-7.**

- **Who.** Operator.
- **Do.** From the repository root, with `forge` on `PATH`
  (`scripts/runtime-rehearsal/README.md`:14-17,112,230-233;
  BOOTSTRAP-CONTRACTS.md, "Runtime rehearsal (R-1)"):

  ```bash
  PROTOC=/path/to/protoc CARGO_TARGET_DIR=/path/to/target \
    scripts/runtime-rehearsal/build.sh /path/to/subtensor-checkout
  RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
    go test -tags runtime_rehearsal ./rehearsal/ -v -timeout 60m
  RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
    go test -tags runtime_rehearsal ./mainnet/ -run TestR1SettlementV2Phase -v
  ```

- **Expect.**
  - The build passes its source and genesis-code checks and writes
    `release/runtime-rehearsal-build.txt` in the target directory
    (README:19-46).
  - Every test passes. On 475, R-2 to R-6 took 8 minutes 22 seconds, R-7
    about 6 and R-1 80 to 100 seconds (README:235-237; BOOTSTRAP-CONTRACTS.md,
    "Runtime rehearsal (R-1)"). On 477 the `rehearsal` package took 776
    seconds on a loaded host, and R-1 120 seconds (README:437-441). On 478
    the battery took 1,091 seconds and R-1 140 (section 9.3, item 2).
  - R-1 uses the approved 750,000 rao fee and completes every action under
    the step 8 limits.
  - R-6's operator transactions fit the policy that step 5 re-signs (§15.8
    R-6).
- **Stop if** any test fails, or the logged gas exceeds the limits of
  section 2.6.
- `build.sh` builds the 478 node, and the battery that counts is the one on
  it. It passed, R-1 to R-7, with `main`'s bytecode, and the README records
  it (README, "Runtime 478"; section 9.3, item 2). On 478, R-3 and R-6 log
  each stake-moving transaction's least gas limit beside `eth_estimateGas`'s;
  both are far below section 2.6's 475 limits while vault2's list is short.
- "A test refuses a node whose runtime version has no reviewed source"
  (README:112-116).

**Step 4. Run the release gates.**

- **Who.** Operator runs; owner confirms O26.
- **Do.** In a workspace with the sibling checkouts the script names
  (`scripts/test-release-1.0-local.sh`:7-9), with `SN_TEST_STORAGE_ROOT` set
  or `/mnt/data` mounted (`scripts/test-storage.sh`:6-17):

  ```bash
  RUN_SERVER_DB_TESTS=1 ./scripts/test-release-1.0-local.sh
  ```

  This is the documented invocation (FINALIZE.md:5211-5214). Run the app
  suites of section 2.2 beside it.
- **What the gate needs,** as known so far. More detail will follow from the
  repair of its source-freeze preflight. The owner chose to refresh the
  release lock and has authorized running it.
  - **The order** is step 1's: the last merges, then the local-mode release
    tooling (sn `v2/release-local`), then the lock refresh, then the gate.
    The launch is built and gated in the local mode, from the same frozen
    checkouts, each clean at its recorded commit. `main` keeps its local
    `../` module substitutions; a pinned build version comes later, from the
    owner's build. The local mode is being added on `v2/release-local`.
  - **The release repositories**, beneath one workspace: sn, server, connect,
    sdk, glog, goidenticons, proxy, userwireguard, warp, gvisor, vault, xops
    and config (`scripts/test-release-1.0-local.sh`:9). Each is a physical
    checkout, not a symlink (`sim-testnet/README.md`:1349-1351). Each is on a
    branch that tracks `origin/main`, or `origin/master` for glog and
    userwireguard, or `origin/go` for gvisor, a clone of
    `github.com/urnetwork/gvisor`. Each is clean, with no tracked, staged or
    untracked change, and at exactly its upstream's freshly fetched head
    (`scripts/check-release-source-freeze.sh`:57-64,125-130,146-159). The
    script on `main` demands that exactness only of sn. For the others it
    admits any commit reachable from the upstream (:160-171). Its Go module
    inventory must equal its reviewed classification, and every live module
    must be tidy (:180-205). gvisor's module is inventoried, not tidied.
  - **The vault and xops checkouts** are among them. The source freeze
    refuses a missing one: "release repository is missing or not a Git
    checkout" (`scripts/check-release-source-freeze.sh`:113-118). The lock is
    observed over both (`sim-testnet/README.md`:1276-1283), and "A fresh
    git-crypt vault clone contains ciphertext until unlocked" (:1351-1352).
    The `xops` job runs that repository's infrastructure tests
    (`scripts/test-release-1.0-local.sh`:585-596).
  - **The release lock**, `deploy/testnet/release.lock.yml`, refreshed as the
    last commit after the freeze and after the local-mode tooling has merged
    (`sim-testnet/README.md`, "Atomic
    release-lock refresh", :1306-1345). The gate ends by checking the lock
    against the checkouts (`TestReleaseLockMatchesCheckout`,
    `scripts/test-release-1.0-local.sh`:622-628). From the sn repository,
    review the candidate without writing, then apply it:

    ```bash
    go run ./sim-testnet release-lock --config sim-testnet/testnet.yml
    ```

    ```bash
    SN_REPO="$(pwd -P)"
    WORKSPACE="$(dirname "$SN_REPO")"
    release_head="$(git rev-parse HEAD)"
    build_utc="$(date -u +%Y%m%dT%H%M%SZ)"
    SIM_TESTNET_RELEASE_DIR="$WORKSPACE/temp/sim-testnet-lock-${release_head}-${build_utc}"
    SIM_TESTNET_BINARY="$SIM_TESTNET_RELEASE_DIR/sim-testnet"
    mkdir -p "$SIM_TESTNET_RELEASE_DIR"
    go build -trimpath -buildvcs=true -o "$SIM_TESTNET_BINARY" ./sim-testnet
    go version -m "$SIM_TESTNET_BINARY"
    sha256sum "$SIM_TESTNET_BINARY"
    "$SIM_TESTNET_BINARY" release-lock --config sim-testnet/testnet.yml --apply
    ```

    That executable "must be built from a clean commit already equal to
    `origin/main` and invoked by that exact absolute, nonsymlink path".
    `--apply` leaves sn dirty only by the lock file: "review and commit that
    file" (:1337-1345). Have that commit pushed before the gate runs, because
    the gate needs sn clean and at `origin/main`.
  - **The generated files at mode 644**: the fifteen ABI and binding files of
    step 8.1. Check public sources out under umask 022, and keep the server's
    `local/postgres/initdb` directory and its script at 755
    (`sim-testnet/README.md`:1356-1361).
- **Expect.** The last line is `[release-1.0] local release gate passed`
  (:653). The settlement v2 jobs:
  - `sn-v2`, gate 3's own verdict (:171-185);
  - `sn-scale` and `server-scale`, GL-10 and SV-6 (:187-205,519-538);
  - `sn-e2e`, gate 4 (:540-562);
  - `solidity`: format, release build with sizes, tests, the `v2_invariant`
    campaign, and the catalog and binding checks (:214-237);
  - `solidity-coverage`, gate 1's bar (:239-254), and `solidity-static`
    (:211);
  - `sdk`, CL-2 and CL-4 (:374-381), and the server's v2 suites with
    migrations 821 and 822 (:469-481).
  - The wiring tests `TestReleaseGateSettlementV2*` of
    `sim-testnet/release_gate_settlement_v2_test.go` run in the aggregate job.
- **Stop if** any job fails, or the script prints `[release-1.0] DB suites
  deferred` (:582): the database suites, the scale shard's database half and
  the E suite then did not run.

### Phase B. Release, approve and deploy

**Step 5. Re-sign the operator gas policy.**

The policy binds the v1 coordinator, the policy hash and the two accounts,
not call targets (`server/controller/st_transaction_gas_policy.go:97-104`). So
it does not depend on the deployment. It must be in force before the first v2
close at the end of E_s.

The policy must admit the server's v2 close floor. Server `main` signs every
v2 close with at least 3,200,000 gas, the rule of section 2.6
(`server/controller/st_ledger_v2_client.go:357-387`;
`server/controller/st_controller.go:1088-1091`). The policy checks two
limits, in `server/model/st_transaction_gas_model.go`:

- **Each signature.** A transaction's gas must not exceed `maximum_gas`, and
  its gas × fee cap must not exceed `maximum_intent_liability_wei`
  (:95-112). So `maximum_gas` is at least 3,200,000. Under today's 1,000,000
  the server refuses every v2 close before any signature, "with its nonce
  kept, so that the root key's other sends stop at it", until the policy is
  re-signed (`server/controller/st_ledger_v2_gas_test.go:196-203`; SV-7). The
  refusal names the floor: "st: gas limit 3200000 is the v2 stake-move floor
  (operator gas policy maximum_gas …)" (`server/controller/st_controller.go:1128-1135`).
- **Each operation.** The liability is summed over every nonce one logical
  operation takes, across its retries, and each operation is allowed at most
  `maximum_intent_attempts` signatures (:332-368). A per-intent liability
  that covers one floored close strands the root key after a single revert
  (`server/controller/st_ledger_v2_gas_test.go:428-460`; section 7). So
  `maximum_intent_liability_wei` is at least 3 × `maximum_gas` ×
  `maximum_fee_per_gas_wei`. That never binds before the cap of three
  signatures.

`maximum_gas` therefore stays at about 3.5M (O13), and the per-intent
liability is at least 3.15·10^17 wei at 3.5M and 30 gwei. Deploy a server
build at or after `54ae2aacf` before the first close, at block 9,348,028.

- **Who.** Owner signs with the approver key; operator installs.
- **Do.**
  1. Copy the launch request `server/local/sn25golive/sn25-mainnet-gas-params.json`
     to a new params file. Change only:
     - `revision`: the live revision plus 1;
     - `previous_policy_sha256`: the live policy's digest. The launch request's
       digest is `e2de5acf1ef30de0b5c00f0e9cb5291db1b56b5fe6d0a17a524bb65806b5d64d`
       (`server/local/sn25golive/main_test.go:26-28`), and LAUNCH.md:73-75
       names the installed one `e2de5acf…`. Confirm it against `st.yml`;
     - `maximum_gas`: about 3,500,000, at least the 3,200,000 floor of a v2
       close, with room for a few deferred epochs (§13);
     - `maximum_intent_liability_wei`: at least 3 × `maximum_gas` ×
       `maximum_fee_per_gas_wei`, 3.15·10^17 wei at 3.5M and 30 gwei;
     - `maximum_lifetime_liability_wei`, the validity window and the attempt
       caps: checked against v2 traffic (O13; §13).

     Keep `chain_id`, `genesis_hash`, `no_id`, `coordinator`, `policy_hash` and
     `accounts` ("re-signed with the same bindings", §13).
  2. Review and sign with the launch approver key (usage at
     `server/local/sn25golive/main.go:32-38`):

     ```bash
     sn25golive gas-policy --params PARAMS.json --dry-run
     sn25golive gas-policy --params PARAMS.json --approver-key KEY.json \
       --out-policy POLICY.yml --out-authority AUTHORITY.yml
     ```

  3. Merge POLICY.yml's top-level `operator_gas_policy` into `vault/main/st.yml`
     (`server/local/sn25golive/gas.go:277`). Install AUTHORITY.yml as
     `config/main/operator-gas-authority.yml` (gas.go flag text;
     `server/controller/st_transaction_gas_policy.go:75`).
  4. Check offline, then restart api, alt and taskworker, which load st.yml
     once per process (`server/controller/st_controller.go:298-302`):

     ```bash
     sn25golive st-check --st st.yml --profile mainnet --authority AUTHORITY.yml --sn sn.yml
     ```

- **Expect.**
  - `policy_sha256`, an `approver_public_key` equal to the launch approver's,
    and `limits: gas 3500000, …` (gas.go:216-233).
  - The server admits the policy at its next signing
    (`server/model/st_transaction_gas_model.go:369-391`), and v1 sends
    continue.
- **Stop if** the server refuses with one of:
  - "operator gas policy rollback or disconnected revision";
  - "operator gas independent approval key cannot replace its original trust
    root";
  - "operator gas account owner or original history pin changed";
  - "operator gas policy differs from the selected deployment and vault role
    accounts".

  These are at `server/model/st_transaction_gas_model.go:386-391,420` and
  `server/controller/st_transaction_gas_policy.go:102-104`.
- **Gap.** `sn25golive` is the server's `local/sn25golive` command. No build or
  run instructions exist beyond its usage text.

**Step 6. Ship the server v2 build with v2 off.**

- **Who.** Operator.
- **Do.**
  1. Apply the migrations first. Migration 821 says "Apply before deploying a
     server with v2 addresses" (`server/db_migrations.go:10013-10025`).
     v2's migrations are 821 and 822. Upstream has since added 823 to 831 on
     server `main` (:10051-10170), and the same command applies them:

     ```bash
     bringyourctl db migrate
     bringyourctl db version
     ```

  2. Deploy the step 1 build of every server service. The ones that read
     st.yml or sn.yml for v2 are api, alt (which mounts the same router),
     taskworker and `bringyourctl` (`server/api/run.go:274-293`;
     `server/alt/run.go:178`; `server/taskworker/workload_profile.go:95-96`).
  3. Leave st.yml and sn.yml without v2 keys. The binary goes before the
     config, because sn.yml is parsed with known fields only (§13;
     `server/provider_payout_transition.go:57-58`).
- **Expect.**
  - The database head is at least the binary's own migration count, and
    readiness passes. That count is 831 on server `main` (`54ae2aacf`), so
    the head is 831 or higher at the freeze. A binary stays not-ready while
    the head is below its count: "database migration head %d is below
    binary-required head %d" (`server/router/warp_handlers.go:209-210,255-260`;
    `server/db_runtime_migration.go:8-15`). `bringyourctl db version` prints
    "Current DB version: %d" (`server/bringyourctl/main.go:484-487`).
  - v1 settlement runs unchanged (section 1.2, state 1).
- **Stop if** readiness fails, or any v1 settlement step stops.
- **The sizing rule.** The build carries section 2.6's v2 close floor when it
  is from server `main` at or after `54ae2aacf`. Replace an older build with
  such a build before the first close, at block 9,348,028.
- No v2 code runs in the server's connect service or in the connect library.
  connect's v2 change is its API description, `api/bringyour.yml` at
  `029fb40d` (STATE.md:215). So connect has no place in this deploy order.

**Step 7. Ship the validator, the tools, the SDK and the apps, v2 off.**

This completes the "gate 5 software is released" part of §12.1 step 1.

- **Who.** Operator for the validator and the tools; owner for the releases.
- **Do.**
  1. Run the new validator binary on its unchanged config.
  2. Publish `snclaim` and the `provider` binary (`cli/snclaim`, `cli/miner`;
     RELEASE-BUILD.md:39).
  3. Release the SDK and the apps with the v2 display and claims, and without
     `Vault2Address`. Build the apps against SDK `3bc7ec95` or later, which
     carries the claim API in `sdk/sn_ledger_claim.go` and the claim gas
     floor of section 2.6.
  4. Recommended: let this release carry network payout consent signing
     (section 1.3). Every app's `main` now carries it (Pins). A consent takes
     effect from the epoch after its signing, so a network that signs at
     least one epoch before E_s is paid from E_s. Two things then come first:
     1. Deploy ur.io from mmm `main` at `e54f1fe46` or later, which holds the
        consent bridge page (`9e2e8c2b5`) and the bridge fix (`64173fd13`).
        Every native app signs the consent through that page
        (STATE.md:268,297).
     2. Build every app against the localization store's `main`, which holds
        the eleven `sn_consent_*` keys (section 2.1; §11.1).
- **Expect.**
  - The validator's config and history hash as before (§13; VA-6,
    `validator/settlement_v2_test.go:43-47`).
  - The apps show v1 as before and no v2 tile: with no `Vault2Address` a tile
    shows only accounts the ledger returned, and there are none. Each app
    encodes this:
    - android `app/app/src/main/java/com/bringyour/network/ui/wallet/SnLedger.kt:83-93`;
    - windows `app/src/App/SnPayoutPresentation.cpp:73`;
    - linux `app/src/SnLedgerPresentation.hpp:112`;
    - apple `app/network/Main/Account/Earnings/SnLedgerPresentation.swift:55-64`;
    - mmm `ur.io/react/src/app/load/readViews.js:44-45`.
- **Stop if**:
  - the validator's config hash changes under the new binary;
  - an app build fails. Against a store without the keys, the Android build
    fails and ur.io's prebuild refuses with "store key missing" (mmm
    `ur.io/react/scripts/build-locales.mjs:82`);
  - an app release carries consent signing while ur.io is not deployed with
    both the consent bridge page and the bridge fix. Hold that release.
- **The sizing rule.** The SDK and the apps built on SDK `3bc7ec95` or later
  carry section 2.6's claim floor, and ur.io at `e54f1fe46` or later sends
  the SDK's limit. `snclaim` and the claim daemon still size a single claim
  from its estimate until their floor merges (step 20).

**Step 8. Deploy the v2 contracts with readback.**

- **Who.** Owner signs the approval and every transaction; operator runs the
  commands.
- **Inputs.** O2 to O12.
- **Start only when** all of these hold:
  1. Gate 2 holds on runtime 478. Its rerun passed, R-1 to R-7 (section 9.3,
     item 2).
  2. The reviewed-source entry for 478 is merged, and `sn-mainnet` is rebuilt
     from that `main`, so that it lists the reviewed source of runtime 478
     (step 8.3).
  3. Step 2, repeated immediately before 8.1, shows 478 with the identity
     that gate 2 passed on. That snapshot gives O3's version and hashes.
- **Dry run.** Steps 8.2 to 8.5 and step 15 ran end to end on a rehearsal
  node, through the documented commands and the built `sn-mainnet`. What it
  corrected is in the text of those steps.

*Umask and ancestry.* Set the umask before the first command of step 8, in
every shell that runs the step:

```bash
umask 077
```

The tools decide by the modes the kernel reports. The dry run met both
refusals quoted below: `storage-prepare plan` and `apply` passed only under
umask 077 and a protected ancestry.

- **Private inputs.** `sn-mainnet` reads the phase config, the release file
  and each signed file only as a regular file with no group or other
  permission bit, in a directory with none, on a path without a symlink. The
  run directory is held to the same rule
  (`mainnet/bootstrap_root_plan.go:118-131,135-151`; `mainnet/evm_phase.go:784`).
- **Protected files.** The custody request, the preparation plan and the
  durable-volume declaration must each be a regular file with one link and no
  group- or other-write bit, owned by root or the running user (connect
  `durablevolume/host_unix.go:46-63`). Under umask 002 the request has mode
  664 and is refused: "durable path … is not a protected physical object
  (mode 100664, …)".
- **Ancestry.** No directory above the run directory or above one of these
  files, from `/` down, may be group- or other-writable, a symlink, or owned
  by anyone but root or the running user. A root-owned sticky directory is
  the one exception, and never as the file's own directory
  (`:67-79,82-112`). A group-writable directory further up is refused as
  "durable ancestor is not protected". The file's own directory, when
  group-writable, is refused as "durable path … is not a protected physical
  object" (`mainnet/settlement_v2_launch_examples_test.go:293-295`).
- **The preparation's directories.** These are the run directory, or its
  parent when the request creates it; the staging directory; and the
  directories of the marker, lease, declaration, control and fence files.
  Each has no group or other permission bit and belongs to the running user.
  Otherwise: "preparation path must be private and owned by the applying
  identity" (connect `durablevolume/preparation_plan_unix.go:182-194,268-294`;
  OWNER-CUSTODY-PREPARATION.md:26-29).
- The umask covers only what step 8 creates. Check `/secure/ur-mainnet` and
  every directory above it before 8.2.

*8.1 Build and verify the catalog.* "Catalog" means `sim-testnet/contracts_gen.go`,
whose `SettlementV2ReleaseArtifacts` the release file is exported from
(BOOTSTRAP-CONTRACTS.md:686-704; `evm/README.md`:53-61).

- `stabi/generate.sh --check` requires mode 644 on each of its fifteen
  generated files as they are checked out (`stabi/generate.sh:148-164,174-177`).
  With any other mode it exits 1: "generated binding/ABI has non-portable
  mode: ../evm/abi/STSubnet.abi.json". A clone made under umask 002 has the
  group-writable mode 664 (STATE.md:9-10).
- The umask at run time changes nothing, step 8's 077 included. Clone the
  tree under umask 022 (step 1), or set the modes before the check:

  ```bash
  chmod 644 evm/abi/*.abi.json stctl/st_abi.json \
    stabi/stsubnet.go stabi/stcoordinator.go stabi/stsettlementvault.go \
    stabi/streservesink.go stabi/stvalidatorevidence.go stabi/stsettlement2.go
  ```

  Those are the eight ABI files, `stctl/st_abi.json` and the six bindings.

```bash
(cd evm && forge build --deny warnings --sizes)
go run ./sim-testnet/gencontracts --check evm/out sim-testnet/contracts_gen.go
stabi/generate.sh --check
go run ./sim-testnet/gencontracts --release-v2-json evm/out sim-testnet/contracts_gen.go \
  /secure/ur-mainnet/settlement-v2-contracts.json
```

- **Expect.** Both checks pass. The release file has schema
  `urnetwork-settlement-v2-contract-release-artifacts-v1` and holds exactly
  `SettlementVault2`, `Settlement2`, `ERC1967Proxy` and `ClaimBatcher`
  (BOOTSTRAP-CONTRACTS.md:686-690).
- **Reference values**, so that the owner can compare the build byte for
  byte. They were produced on the build machine on 2026-10-13 from sn
  `29c8bd70`, an earlier merge of `v2/launch-tools`. `evm/`, the catalog and
  the generator do not differ between that revision and this `main`, so the
  contracts are unchanged since.
  - The release file from `--release-v2-json` is 131,719 bytes. Its SHA-256
    is `d0ffbe0b52ab61370fa4a33bec9f5688dd579f312263e867bd9056d78d673240`.
  - Each contract's runtime in that file. The catalog's `…RuntimeBytecode`
    constants on `main` have the same sizes and hashes
    (`sim-testnet/contracts_gen.go`):

    | Contract | Runtime bytes | SHA-256 of the runtime |
    | --- | --- | --- |
    | `SettlementVault2` | 12,815 | `789ca577fea787b5157349f55e6359eeadb1c962018d0f2c41f0ed97808c09dd` |
    | `Settlement2` | 8,588 | `ad51fc5c0bb4ba30b95d36273f07a497c40c2f5bba9c948b02c247e92f5320fa` |
    | `ERC1967Proxy` | 130 | `bd65bf2fc79a460d4e23a2ea4c231986900610d387d32b88c8ef0679843d0014` |
    | `ClaimBatcher` | 1,073 | `a7da410a84bd399d3f735b7b201ca6c2cc6d57e63a028c2c518bd7cfd5b326cc` |

  - Compare the release file, not raw `evm/out`. The raw artifacts differ
    from it in a 32-byte metadata hash for three of the four contracts,
    while `gencontracts --check` still passes.
  - Read the file's size and hash with:

    ```bash
    stat -c %s /secure/ur-mainnet/settlement-v2-contracts.json
    sha256sum /secure/ur-mainnet/settlement-v2-contracts.json
    ```

- **Stop if** either check fails, or the release file's SHA-256 differs from
  the reference value.

*8.2 Prepare custody.* Prepare one fresh `mainnet-evm-action` snapshot for each
approved journal, with daemon scope (BOOTSTRAP-CONTRACTS.md:774-777 and "Runtime rehearsal (R-1)";
[mainnet/OWNER-CUSTODY-PREPARATION.md](mainnet/OWNER-CUSTODY-PREPARATION.md):40-56,105-109).

- The journals are `vault2-create.json`, `escrow2-register.json`,
  `settlement2-create.json`, `proxy2-create.json`, `vault2-link.json` and, if
  O11, `batcher-create.json` (BOOTSTRAP-CONTRACTS.md:657-664).
- Each holds at most 524,288 bytes (`mainnet/evm_action_store.go:361`).
- The request and its fence are
  [mainnet/settlement-v2-custody-request.example.json](mainnet/settlement-v2-custody-request.example.json)
  and
  [mainnet/settlement-v2-custody-fence.example.json](mainnet/settlement-v2-custody-fence.example.json)
  (SETTLEMENT-V2-EXAMPLES.md:84-100). `TestSettlementV2LaunchCustodyRequestExample`
  plans and applies them, then opens vault2's journal under the declaration
  they return (`mainnet/settlement_v2_launch_examples_test.go:255-343`). Fill
  them so:
  1. Under `umask 077`, create `run-v2` (empty), `run-v2-custody` and
     `run-v2-staging` under `/secure/ur-mainnet`, each mode 0700.
  2. Write the fence as `/secure/ur-mainnet/run-v2-custody/former-writer.json`.
     `<root-inode>` is the inode of `run-v2` (`stat -c %i`).
     `<fence-evidence>` says why no earlier writer exists, and "the parser
     cannot check that text".
  3. In the request, `<mount-path>`, `<filesystem-uuid>` and
     `<filesystem-type>` are the mount that holds `/secure/ur-mainnet`
     (`findmnt -no TARGET,UUID,FSTYPE --target /secure/ur-mainnet`): ext4,
     xfs or btrfs. `<fence-file-sha256>` is `sha256:` and the fence file's
     SHA-256.
  4. Keep the request outside `run-v2`, for example as
     `/secure/ur-mainnet/run-v2-preparation-request.json`. Without O11 it may
     drop `batcher-create.json` (:71-72).
- Plan, review and apply the preparation (usage at
  `mainnet/storage_prepare_unix.go:119`):

  ```bash
  sn-mainnet storage-prepare plan --request "$V2_CUSTODY_REQUEST" \
    --request-sha256 "sha256:$V2_CUSTODY_REQUEST_DIGEST" > /secure/ur-mainnet/run-v2-preparation-plan.json
  sn-mainnet storage-prepare apply --plan /secure/ur-mainnet/run-v2-preparation-plan.json \
    --plan-sha256 "sha256:$V2_CUSTODY_PLAN_DIGEST" > /secure/ur-mainnet/run-v2-preparation-result.json
  ```

- A refusal prints `storage preparation: …` and exits 2
  (`mainnet/storage_prepare_unix.go:161-164`). The two that the dry run met,
  a request file of mode 664 and a group-writable parent, are in the note on
  umask and ancestry above.
- Pass the result's `declaration.path` and `declaration.sha256` on every
  `apply` and `resume` (OWNER-CUSTODY-PREPARATION.md:100-102):
  `HOST=(--durable-volumes "$HOST_DECLARATION" --durable-volumes-sha256 "$HOST_DECLARATION_SHA256")`
  (LAUNCH.md:349-352). The result's `declaration.sha256` already has the
  `sha256:` prefix. Pass it whole. With the example request the declaration
  is `/secure/ur-mainnet/run-v2-custody/durable-volumes.json`.

*8.3 Draft, preview, approve and plan.*

- Draft `/secure/ur-mainnet/settlement-v2.unsigned.json`, schema
  `urnetwork-mainnet-settlement-v2-contract-phase-config-v1`
  (BOOTSTRAP-CONTRACTS.md:666-669). Every input is in this file, never in a
  flag:
  - the deployer is each action's `sender`;
  - the escrow hotkey and the fee are action 0's constructor words;
  - the burn cap is action 1's calldata, with `value_wei` equal to the cap ×
    10^9;
  - the owner, v1 coordinator, auditors and quorum are action 3's initializer
    (`mainnet/evm_v2_contracts.go:81-86,219-295`; `mainnet/evm_phase.go:37-47,58,77-82,180`).
- Start from the example
  [mainnet/settlement-v2.unsigned.example.json](mainnet/settlement-v2.unsigned.example.json)
  (SETTLEMENT-V2-EXAMPLES.md:32-77), and fill it with the recipe
  [mainnet/SETTLEMENT-V2-FILL.md](mainnet/SETTLEMENT-V2-FILL.md).
  `TestSettlementV2LaunchPhaseConfigExample` fills the example with synthetic
  values and passes `preview` and, once signed, `plan`
  (`mainnet/settlement_v2_launch_examples_test.go:189-246`).
  - No command builds the six actions' `data`. The index's table of derived
    fields says how each is built (:58-69). The recipe derives them with
    `cast`, `jq`, `b2sum` and `xxd`, fills the example, runs `preview` and
    compares its answer with the owner inputs (its sections 4 to 7).
  - The recipe's `bash` blocks were extracted and run in order against
    `sn-mainnet` built from `29c8bd70` and from `c844f308`, with and without
    the batcher: `preview` exited 0 with 21 of 21 checks `ok`, and `plan`
    accepted the same plan hash.
  - **Read the comparison.** `preview` accepts any well-formed wrong value:
    another escrow hotkey, demand fee, burn cap, owner, v1 coordinator or
    auditor, or a wrong deployer or nonce used throughout. Only the recipe's
    comparison step catches these (its sections 7 and 8). The owner reads
    it, against the written owner inputs, before signing.
  - "Preview rebuilds every derived word and refuses one that differs", and
    it "refuses the example while any placeholder is left"
    (SETTLEMENT-V2-EXAMPLES.md:72-75).
  - Preview checks `native_chain` and `specName` only for being non-empty
    (`mainnet/evm_phase.go:180,184`). Review both by eye: they are
    `Bittensor` and `node-subtensor` (SETTLEMENT-V2-EXAMPLES.md:36-40).
    "only the online steps compare them with the chain, after the approval
    is signed" (:76-77).
- The runtime identity O3 is the file's `plan.runtime`: `runtime_version`,
  `runtime_code_hash`, `runtime_metadata_hash` and `runtime_source_commit`
  (`mainnet/evm_phase.go:55`; `mainnet/root_receipt_profile.go:21-26`).
  - The contracts are deployed on runtime 478. The version and the two
    hashes are 478's, from the owner's own snapshot of step 2, taken
    immediately before step 8.
  - `runtime_source_commit` is the reviewed 478 source,
    `850f2755bb05a3851f948489f0ab27b0645cebce`.
  - The binary admits a config only if `runtime_source_commit` is a reviewed
    source. On `main` those are the commits of runtimes 470, 473, 475 and
    477, and one historical source that earlier plans keep
    (`crv4/native_owner_source.go:6-29`; `mainnet/runtime_source_profile.go:8-12`).
    Any other commit is refused at `preview` and at `plan`: "contract phase
    runtime/source approval is incomplete" (`mainnet/evm_phase.go:183-186`).
  - So a binary built from today's `main` refuses 478's commit. The dry
    run's binary refused 477's own,
    `5c6e83e2f4936f257234f246957f75684c35227c`, in the same way, before its
    entry merged. The 478 entry follows on sn branch `v2/runtime-478`.
    Step 1's freeze and the `sn-mainnet` build must include it. So step 8
    waits for that merge and a rebuild.
  - Never name another runtime's commit beside the live `specVersion`, such
    as 475's `d1718c99c34cf96abbf2bf09c0e9e48b945c76f5` under 477 or 478.
    The approval would then name a source review that nobody made of the
    running runtime. That is a mislabelled approval, and it must not be
    used. The v2 phase now refuses such a pair, in `preview` too: "contract
    phase runtime version differs from the one its source commit was
    reviewed for" (`mainnet/evm_phase.go:187-191`;
    `mainnet/runtime_source_profile.go:14-20`;
    BOOTSTRAP-CONTRACTS.md:669-678).
  - The historical source declares no version and is still accepted, "so
    the approver checks that the config names the runtime's reviewed commit"
    (:675-677). "A valid preview does not authenticate externally supplied
    runtime, custody, cutover or network claims" (:186-188).
  - A runtime that lands during step 8 stops the next send. Before a send
    the tool reads the live runtime and refuses a difference from the
    config: "EVM current runtime differs from independent phase approval"
    (`mainnet/evm_action_rpc.go:593-599`; BOOTSTRAP-CONTRACTS.md:590-592). An
    inclusion under an unapproved runtime "remains unresolved and needs
    separately qualified authority migration, not a fresh journal or an
    inferred compatibility waiver" (:625-628).
- Take the plan's native start checkpoint, `start_native_number` and
  `start_native_hash`, just before the approval is signed.
  - The recipe reads it from step 2's snapshot, its
    `identity.finalized_number` and `finalized_hash`. So repeat step 2 just
    before the recipe.
  - Every later `resume --online` scans forward from that block, 128 native
    headers a call (step 8.4).
  - The send window ends at `valid_through_native`, at most 7,200 blocks
    after the checkpoint (`mainnet/evm_phase.go:180`).
- The recipe ends with the preview and its comparison with the owner
  inputs. The block below repeats that preview, then extracts the approval
  bytes and signs them (BOOTSTRAP-CONTRACTS.md:194-198; LAUNCH.md:369-376):

  ```bash
  umask 077
  sn-mainnet bootstrap-contracts preview --action vault2-create \
    --config /secure/ur-mainnet/settlement-v2.unsigned.json > /secure/ur-mainnet/settlement-v2.preview.json
  jq -r '.approval_signing_message_hex' /secure/ur-mainnet/settlement-v2.preview.json |
    xxd -r -p > /secure/ur-mainnet/settlement-v2.approval.bin
  APPROVAL=/absolute/path/root/subtensor/approval
  go run "$APPROVAL/sign.go" -key "$APPROVAL/sn25-mainnet-approval.key" \
    /secure/ur-mainnet/settlement-v2.approval.bin
  ```

- Review the preview before signing (BOOTSTRAP-CONTRACTS.md:761-768):
  - `vault2_constructor`: the escrow hotkey O5 and the fee
    750,000,000,000,000 wei;
  - the predicted addresses and runtime hashes of vault2, the implementation,
    the proxy and, if O11, the batcher;
  - the initializer: owner O8, v1 coordinator O9, the auditor O10 and quorum
    1;
  - the gas, fees and nonces of O7.
- Put the 128-character lowercase signature into `approval_signature_ed25519`
  (:139-143), save the config as `/secure/ur-mainnet/settlement-v2.json`, and
  run `plan`. It verifies the approval and prints `plan_hash`, which becomes
  `SETTLEMENT_V2_HASH`:

  ```bash
  sn-mainnet bootstrap-contracts plan --action vault2-create --config /secure/ur-mainnet/settlement-v2.json
  ```

  `plan` prints the preview's `plan_hash`. Unlike `preview`, it needs step
  8.2's run directory to exist. Without it, it exits 2 with "contract phase
  authority: bootstrap root directory cannot traverse symlinks"
  (`mainnet/evm_phase.go:784`; `mainnet/bootstrap_root_plan.go:118-125`;
  `mainnet/bootstrap_contract_command.go:65-69`). So complete step 8.2 first.
- **Stop if** any preview value differs from the owner inputs, a comparison
  of the recipe does not print `ok`, or `plan` refuses.

*8.4 vault2 and its escrow registration, back to back (A7).*

- Vault2's creation reveals the escrow hotkey. Anyone who registers it first
  makes `registerEscrow` fail for good, so the registration is the deployer's
  very next nonce (BOOTSTRAP-CONTRACTS.md, "Back-to-back escrow
  registration (A7)").
- The commands are the block after that paragraph. Append `"${HOST[@]}"` to
  each `apply` and `resume`. The statuses are the ones R-1 asserts
  (`mainnet/evm_v2_rehearsal_test.go:550-561`).
  1. Open vault2's journal. Expect `signature-awaiting-import`. The result
     prints vault2's `signing_digest` and `unsigned_transaction` and, beside
     them, the escrow registration's `escrow_signing_digest` and
     `escrow_unsigned_transaction`, the approved action 1
     (BOOTSTRAP-CONTRACTS.md, "Back-to-back escrow registration (A7)";
     `mainnet/evm_action.go:90-91`):

     ```bash
     sn-mainnet bootstrap-contracts apply --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH"
     ```

  2. The deployer signs both before any send: `unsigned_transaction` and
     `escrow_unsigned_transaction`. Each signature is canonical binary
     EIP-1559 bytes in its own file. The file's pin is `sha256:` and its
     SHA-256 in 64 lowercase hex characters (LAUNCH.md:422-423,443;
     `mainnet/bootstrap_contract_command.go:36,38,42`). `escrow2-register`'s
     later `apply` prints the identical transaction and digest as its own.
  3. Import both, send both back to back, and complete vault2. Expect
     `signed-custody-complete`, then
     `submitted-with-escrow-registration-awaiting-canonical-receipts`. Then
     repeat the last command, `resume --online`, until it prints
     `vault2-created` (the note after sub-step 4). That status comes after
     the readback of vault2's exact runtime and getters: `CHALLENGE_BLOCKS`
     14,400, `coordinator` zero, the counters zero and `conservationHolds`
     true (BOOTSTRAP-CONTRACTS.md:783-793).

     ```bash
     sn-mainnet bootstrap-contracts resume --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --signed-transaction /secure/ur-mainnet/vault2.signed.bin --signed-transaction-hash "$VAULT2_SIGNED_FILE_HASH" --escrow-signed-transaction /secure/ur-mainnet/escrow2.signed.bin --escrow-signed-transaction-hash "$ESCROW2_SIGNED_FILE_HASH"
     sn-mainnet bootstrap-contracts resume --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --online --submit
     sn-mainnet bootstrap-contracts resume --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --online
     ```

  4. Open the escrow journal and import the same escrow bytes. Expect
     `signature-awaiting-import`, then `signed-custody-complete`. Then repeat
     the last command, `resume --online`, until it prints
     `escrow2-registered`. That status comes after the readback of the
     `EscrowRegistered` event, the UID mapping and `escrowRegistered` true,
     and `escrow_collateral` must read `(false, 0, 0, 0, 0)` (:794-805).

     ```bash
     sn-mainnet bootstrap-contracts apply --action escrow2-register --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH"
     sn-mainnet bootstrap-contracts resume --action escrow2-register --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --signed-transaction /secure/ur-mainnet/escrow2.signed.bin --signed-transaction-hash "$ESCROW2_SIGNED_FILE_HASH"
     sn-mainnet bootstrap-contracts resume --action escrow2-register --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --online
     ```
- **One `resume --online` does not reach the completed status.** Repeat it
  until it prints the status named in the sub-step. This holds for every
  action of steps 8.4 and 8.5.
  - Until it reaches the receipt, each call exits 0 with an intermediate
    status. `pending-nonce-unresolved`: no receipt yet, and the sender's
    pending nonce is past the action's
    (`mainnet/evm_action_rpc.go:630-633`). `receipt-awaiting-finalized-mapping`:
    the receipt exists, and the scan has not reached its native block
    (`:295,305`).
  - Each call scans at most 128 native headers, and each action's journal
    starts at the plan's `start_native_number`
    (`mainnet/evm_action_rpc.go:292-305`; `mainnet/evm_action_store.go:404`;
    BOOTSTRAP-CONTRACTS.md:586-587). So an action needs
    ceil((inclusion block − start block) / 128) calls. A start checkpoint 6
    hours old, 1,800 blocks, means about 15 calls an action.
  - The next action's `apply` exits 3 until its predecessor has completed
    (`mainnet/bootstrap_contract_command.go:260-263`).
  - So take the native start checkpoint just before the approval is signed
    (step 8.3).
- **Stop if**:
  - `escrow-registration-reverted-nonce-consumed`. Someone registered the
    escrow hotkey first. No funds are at risk; recover by redeploying under a
    new approval with a fresh escrow hotkey and fresh nonces ("Two residuals
    remain", after the A7 paragraph; section 6).
  - `escrow-registration-allowance-exhausted` (the A7 paragraph).
  - Any readback refusal, or a nonzero escrow collateral.
  - A call exits 1 with "EVM current runtime differs from independent phase
    approval": the runtime changed after the approval (step 8.3).

*8.5 The implementation, the proxy, the binding and the batcher.*

- For `settlement2-create`, `proxy2-create`, `vault2-link` and, if O11,
  `batcher-create`, in that order, each after the previous one's completed
  status (BOOTSTRAP-CONTRACTS.md, after the command block):
  1. `apply`, which exits 3 until the previous action has completed;
  2. sign its `unsigned_transaction`;
  3. `resume --signed-transaction … --signed-transaction-hash …`;
  4. `resume --online --submit`;
  5. `resume --online`, repeated until the action's completed status (the
     note in step 8.4).
- **Expect.**
  - The statuses `signature-awaiting-import`, `signed-custody-complete` and
    `submitted-awaiting-canonical-receipt`. Then the intermediate statuses of
    step 8.4's note. Then `settlement2-created`, `proxy2-created-initialized`,
    `vault2-coordinator-bound` and `batcher-created`
    (`mainnet/evm_v2_phase.go:37-49`), each after the readback of
    BOOTSTRAP-CONTRACTS.md:806-833.
  - At `vault2-link`: `installation_complete` true and `activation_ready`
    false (:835-838). The result names `vault2_address`, `settlement2_address`
    (the proxy), `settlement2_implementation_address` and, after the sixth
    action, `claim_batcher_address` (`mainnet/evm_action.go:81-84`).
- **Stop if** any readback mismatches, or a call exits 1 for a changed
  runtime (step 8.3). Never repeat a completed CREATE, and never change the
  accepted graph or nonces to escape an ambiguous outcome
  (LAUNCH.md:443-447).

*8.6 Keep the record.* The six journals' receipts are the v2 deployment record
(BOOTSTRAP-CONTRACTS.md:838-842). Retain the run directory, the config, the
release file and every signed file. The launch readiness digest is unchanged
(§8.5).

**Step 9. Pin `Vault2Address` and release the apps.**

Activation needs v2 claims in the apps (§2.1 decision 8; §11 "At activation").
No app reads `Vault2Address` from a server or a config: "/sn/epoch never sets
it: the vault a claim is sent to is pinned by the release"
(`sdk/sn_chain.go:83-88`). Pinning therefore takes an SDK change and a second
release of every app.

- **Who.** Owner, with the app releases.
- **Do.**
  1. In `DefaultSnChainSettings()` (`sdk/sn_chain.go:89`), set `Vault2Address`
     to step 8's `vault2_address`, lowercase `0x` (the field: :42).
  2. Change the two tests that require an empty default:
     `TestSnChainSettingsVault2Address` (`sdk/sn_ledger_claim_test.go:1763-1765`)
     and `TestSnChainSettingsWasmVault2Address` (`sdk/js/sn_wasm_test.go:107-109`).
  3. Run the SDK's v2 suites, the `sdk` job selector of the release gate
     (`scripts/test-release-1.0-local.sh`:374-381).
  4. Rebuild and release every app against that SDK: the Android AAR, the
     Apple framework, the Windows DLL, the Linux library and the ur.io wasm.
- **Expect.** Each app shows the v2 tile and offers Claim when an account has
  anything claimable. Before the first finalization nothing is claimable.
- **Stop if** the pinned address differs from the server's `vault_v2_address`
  of step 11. The SDK claims an account only in the pinned vault2
  (`sdk/sn_ledger_claim.go:181-182`), and the server names the vault from its
  config (`server/controller/sn_ledger_v2_handler.go:193`).
- The releases land before step 15 (O23). The SDK can also read
  `Vault2Address` from the network space's `sn_chain` values
  (`sdk/network_space.go:92`), but no app writes them.

**Step 10. Adopt the validator config and re-sign the treasury approval.**

- **Who.** Owner signs; operator runs.
- The written command sequence is
  [mainnet/SETTLEMENT-V2-ADOPTION.md](mainnet/SETTLEMENT-V2-ADOPTION.md).
  `TestSettlementV2AdoptionDocumentIsTheVA6Sequence` holds it word for word
  to the round that VA-6 runs (`mainnet/settlement_v2_adoption_test.go:60-131`).
  VA-6 is `TestTreasuryApprovalPlanResignsSettlementV2Adoption`: "settlement
  v2 adoption needs no new tooling" (`mainnet/treasury_approval_test.go:534-557`).
  On that page each `…_SHA256` variable of a command line holds the whole
  `sha256:` pin. The commands below show the prefix instead.
- **Do.**
  1. Write the adopted config: the running config plus exactly these three
     changes (SETTLEMENT-V2-ADOPTION.md:13-40;
     `validator/production_authority_history.go:350-388`):

     ```yaml
     settlement_v2:
       settlement: "$SETTLEMENT2_ADDRESS"
       vault: "$VAULT2_ADDRESS"
     production_authority_history:
       - path: "$PREDECESSOR_AUTHORITY"
         bytes: $PREDECESSOR_AUTHORITY_BYTES
         sha256: "$PREDECESSOR_AUTHORITY_SHA256"
     treasury_approval:
       approval:
         path: "$ADOPTED_ENVELOPE"
       signer: "$TREASURY_APPROVAL_KEY"
     ```

     - `settlement_v2` names step 8's `settlement2_address`, the proxy, and
       its `vault2_address`, both lowercase `0x`
       (`validator/settlement_v2.go:14-30`).
     - The history entry is appended after any the config already has. It is
       the running config's authority bundle, which `validator run` retains
       in its `state_dir` as
       `owner-recycle-production-authority-sha256:<hex>.json`
       (`validator/recycle_approval_file.go:43-65`;
       `validator/production_authority_history.go:80-83`). Give its path, its
       size and `0x<hex>`. Inside the config a `sha256` field is `0x` and the
       hex, not the `sha256:` form of a command line.
     - `treasury_approval` keeps its `signer` and names a new envelope path.
       Sub-step 5 supplies the envelope's size and hash.

     Every path is absolute (`validator/treasury_approval_draft.go:71-77`).
  2. Check the appended entry by hand, before anything is signed. The loader
     does not refuse an adoption that adds the block and appends no entry
     (SETTLEMENT-V2-ADOPTION.md:96-101):
     - the adopted config's `production_authority_history` has exactly one
       entry more than the running config's, and the earlier entries are
       unchanged;
     - the last entry's `path` is the bundle above, its `bytes` is that
       file's size, and its `sha256` is `0x` and that file's SHA-256, the
       same hex as in the file's name:

       ```bash
       stat -c %s "$PREDECESSOR_AUTHORITY"
       sha256sum "$PREDECESSOR_AUTHORITY"
       ```

  3. Draft the approval again, from the same input and policy plan as the
     live one (O14; `mainnet/treasury_approval.go:192-251`):

     ```bash
     sn-mainnet treasury approval-plan --input "$TREASURY_INPUT" \
       --config "$ADOPTED_CONFIG" --config-sha256 "sha256:$ADOPTED_CONFIG_DIGEST" \
       --policy-plan "$TREASURY_POLICY_PLAN" --policy-plan-sha256 "sha256:$TREASURY_POLICY_PLAN_DIGEST" \
       --approval-key "$TREASURY_APPROVAL_KEY" --out "$ADOPTED_APPROVAL"
     ```

     - Both `-sha256` flags need the `sha256:` prefix, before the file's
       SHA-256 in 64 lowercase hex characters. `--approval-key` is `0x` and
       the public key's 64 lowercase hex characters.
     - With a bare digest the command only prints "treasury approval-plan
       requires --input, --config with --config-sha256, --policy-plan with
       --policy-plan-sha256, --approval-key and --out"
       (`mainnet/treasury_approval.go:202-204`; `mainnet/plan_input.go:111-113`).
     - The input, the config and the policy plan are each read only as a
       private regular file in an owner-private directory, as in step 8
       (`mainnet/treasury_approval.go:211,219,223`;
       `mainnet/bootstrap_root_plan.go:118-131,135-151`). The config's hash
       does not depend on where the file sits while every path in it is
       absolute (`validator/treasury_approval_draft.go:71-77`), so a private
       copy of the adopted config serves.

  4. Sign the printed `signing_digest`, the 32 digest bytes and not their
     hex, with the approval key (Ed25519). The printed derivation gives the
     check (`mainnet/treasury_approval.go:24-31`), which prints the digest's
     hex digits:

     ```bash
     printf 'urnetwork-native-treasury-approval-signature-v1\n%s' "$(cat "$ADOPTED_APPROVAL")" | shasum -a 256
     ```

  5. Assemble the envelope, then replace the adopted config's
     `treasury_approval` with the one this prints (:252-277). `--signature` is
     the signature's 128 lowercase hex characters (:256):

     ```bash
     sn-mainnet treasury approval-envelope --approval "$ADOPTED_APPROVAL" \
       --signature "$SIGNATURE" --approval-key "$TREASURY_APPROVAL_KEY" --out "$ADOPTED_ENVELOPE"
     ```

  6. Restart the validator on the adopted config, with the auditor key,
     through the drop-in
     [mainnet/validator-ledger-auditor.example.conf](mainnet/validator-ledger-auditor.example.conf)
     (SETTLEMENT-V2-EXAMPLES.md:102-123). Install it as
     `/etc/systemd/system/sn-mainnet-validator-<role>.service.d/ledger-auditor.conf`,
     run `systemctl daemon-reload` and restart the unit. Its first, empty
     `ExecStart=` clears the unit's own. Its second is the command line the
     repository renders for the unit, naming the adopted config, plus the
     auditor flag (`validator/run.go:112-117,142-145`):

     ```bash
     validator run --config="$ADOPTED_CONFIG" --progress-file="$PROGRESS_FILE" \
       --durable-volumes="$VALIDATOR_DECLARATION" --durable-volumes-sha256="sha256:$VALIDATOR_DECLARATION_DIGEST" \
       --ledger_auditor_key_file="$AUDITOR_KEY_FILE"
     ```

     - The declaration's pin keeps its `sha256:` prefix
       (`validator/run.go:489`; connect `durablevolume/config.go:240-251`).
     - The key file holds the O10 auditor's hex secp256k1 key. "Root owns it,
       its group is the service's GID and its mode 0440, in a directory the
       service cannot write" (SETTLEMENT-V2-EXAMPLES.md:112-116).
     - Without the flag the validator sends no approval, challenge or demand
       (`validator/run.go:142-143`), so no root is approved. The repository's
       own unit carries no such flag
       (`mainnet/repair_validator_authority.go:181-183`), and its installer
       never overwrites a unit
       ([mainnet/VALIDATOR-ACTIVATION.md](mainnet/VALIDATOR-ACTIVATION.md):198-204).
     - Three tests hold the drop-in to the rendered unit and to
       `validator run`'s usage: `TestSettlementV2LaunchAuditorDropInExample`,
       `validator/TestSettlementV2LaunchAuditorDropInParses` and
       `validator/TestSettlementV2AdoptionDocumentRestartIsTheDropIn`.

- **Expect.**
  - The approval equals the old one except `config_hash` (the VA-6 test).
  - The loader admits the adopted config.
  - At its first finalized snapshot the validator confirms
    `STSettlement2.vault()`, `STSettlement2.v1Coordinator()` and
    `vault2.coordinator()` (`validator/settlement_v2.go:44-71`;
    `validator/release_runtime_v2.go:379-387`).
- **Stop if** sub-step 2's check fails: the history does not have exactly
  one entry more, an earlier entry changed, or the last entry's path, size
  or hash is not the bundle's. Correct the config before anything is signed.
- **Stop if** any of these appears:
  - a loader refusal of a wrong adoption, when the validator starts
    (SETTLEMENT-V2-ADOPTION.md:86-94):
    - "settlement v2 adoption changes signed configuration beyond the added
      settlement_v2 block": another change rides with the block
      (`validator/production_authority_history.go:382`);
    - "settlement v2 adoption changes the approval beyond its config hash"
      (:385);
    - "original production config is not canonical or changes its signed
      predecessor prefix": more than the one bundle is appended (:225);
  - "production intent original authority is absent from the signed config
    history" (:522). The config loaded without its appended entry, and the
    validator now refuses every decision made under the running config.
    Sub-step 2's check was missed;
  - "settlement_v2 … at block … returned …" (`validator/settlement_v2.go:67`).
- The text "settlement v2 adoption must append exactly one authority history
  entry" never appears: the loader does not reach that check (:374;
  SETTLEMENT-V2-ADOPTION.md:96-98).
- **The repair and activation controllers refuse the adopted validator.**
  These are `sn-mainnet repair-validator`, `repair-active-validator` and
  `activate-validators` (`mainnet/main.go:192-206`). They accept only the
  unit as the repository renders it. Any drop-in is refused, "validator
  repair loaded unit property differs: DropInPaths", and so is any other
  loaded `ExecStart`, "validator repair loaded execution differs"
  (`mainnet/repair_validator_host.go:370-377,400-403`;
  `mainnet/validator_activation_host.go:42`;
  [mainnet/VALIDATOR-REPAIR.md](mainnet/VALIDATOR-REPAIR.md):74-78;
  SETTLEMENT-V2-EXAMPLES.md:118-123).
  - To restart the adopted validator, restart its unit with the drop-in in
    place, as in sub-step 6. That needs neither controller.
  - Before running either controller, remove the drop-in and run
    `systemctl daemon-reload`, so that the loaded unit is the rendered one
    again. That unit starts the pre-adoption config without the auditor key:
    adoption gives the validator "a successor config and an auditor key on
    its command line, which this profile does not render"
    (VALIDATOR-ACTIVATION.md:206-207).
  - So afterwards reinstall the drop-in, reload, restart the unit on the
    adopted config and repeat step 12's check. Never leave the validator on
    the rendered command line from E_s on: without `settlement_v2` it
    resolves every epoch through `operatorAt` (§9.1).
  - **Gap.** No rendered unit names the adopted config and the auditor key,
    so neither controller can start the adopted validator.
- Adoption is one-way: "once adopted it can be neither re-pointed nor removed"
  (`validator/production_authority_history.go:256-260`).
- Step 11 follows directly. The server's reserved upload lane pins the
  validator config file by its path, bytes and SHA-256
  (VALIDATOR-UPLOAD-RUNTIME.md:3-9).

**Step 11. Switch the server config.**

The event mirror reads v2 logs only while v2 is enabled, from one shared
checkpoint, with no backfill (`server/controller/st_controller.go:2605-2611,4262-4265`).
The switch therefore comes before step 15.

- **Who.** Operator.
- **Do.**
  1. In one change, add the same pair to both files:
     - `vault/main/st.yml`, mainnet profile: `settlement_v2_address` and
       `vault_v2_address`, and optionally `v2_prune_alert_positions` (O19)
       (`server/controller/st_controller.go:243-244,262`);
     - `config/main/sn.yml`, under `mainnet:`: `settlement_v2_address` and
       `vault_v2_address` (`server/provider_payout_transition.go:45-47`).

     Both are lowercase `0x`, nonzero and distinct, and differ from the v1
     contracts (`server/st_ledger_v2_config.go:14-27`;
     `server/controller/st_ledger_v2_driver.go:94-98`).
  2. Point `reserved_attempt_upload.admission.production_runtime_config` at
     the adopted validator config, with the new approval and authority files
     beside it (§13; VALIDATOR-UPLOAD-RUNTIME.md:3-33;
     `server/controller/st_reserved_attempt_upload.go:31-35`). Those copies
     are named by the selected hash without `0x`:
     `treasury-production-approval-<hex>.json` and
     `owner-recycle-production-authority-<hex>.json` (:19-25). That is not
     the `sha256:<hex>` name under which step 10 found the bundle.
  3. Check offline:

     ```bash
     sn25golive st-check --st st.yml --profile mainnet --authority AUTHORITY.yml --sn sn.yml
     ```

     It runs the mainnet admission with the v2 fields
     (`server/local/sn25golive/stcheck.go:274-296`), but its printout omits
     the v2 addresses (:298-322). Compare those against step 8 by eye.
  4. Restart api, alt and taskworker. st.yml is loaded once per process, and
     the builder is installed at start (`server/controller/st_controller.go:298-302`;
     `server/api/run.go:283-284`).
- **Expect.**
  - No `[st]st.yml unavailable (st subsystem disabled)` in the logs
    (`server/controller/st_controller.go:305,312`).
  - The gauge `urnetwork_api_reserved_upload_startup_failed` reads 0
    (`server/api/run.go:255-272`).
  - `GET /sn/ledger/account` answers, with no accounts yet.
  - v1 closes and commits continue; the pool is not registered.
- **Stop if**, and restore both files together:
  - "sn: selected deployment differs from reviewed earnings schedule"
    (`server/provider_payout_transition.go:161-163`). Every mainnet send, v1
    included, is refused while the two files disagree.
  - "[st]v1 cutoff for epoch … unknown" (`server/taskworker/work/st_ledger_v2_work.go:29-37`).
    A failed `vault2.pools` read leaves that turn's v1 close and commit
    unscheduled.
  - The reserved-upload gauge reads 1.

**Step 12. Check what runs.**

This is §12.1 step 4. Gate 6 completes here: the validator config is adopted,
the treasury approval and the gas policy are re-signed, and the server is
configured (§14 gate 6). Gate 5 completed at step 9.

- **Who.** Operator.
- **Do.**
  - Confirm the validator runs the adopted config, and that its v2 identity
    check passed.
  - Confirm the running validator unit carries `--ledger_auditor_key_file`,
    through the step 10 drop-in. Without it the audit sends no approval
    (`validator/run.go:142-143`). Read the loaded unit, as the repository's
    own controller does (`mainnet/repair_validator_host.go:251,342`):

    ```bash
    systemctl --system --no-pager show --all \
      --property=DropInPaths,NeedDaemonReload,ExecStart -- sn-mainnet-validator-<role>.service
    ```

    `DropInPaths` names `ledger-auditor.conf`, `NeedDaemonReload` is `no`,
    and `ExecStart` carries `--config=` with the adopted config and
    `--ledger_auditor_key_file=`.
  - Confirm the adopted config's last `production_authority_history` entry
    once more (step 10, sub-step 2).
  - Confirm every server service runs the step 6 build with the step 5 policy
    and the step 11 config.
  - Confirm the step 9 app releases are out.
- **Stop if** any of these differs.

**Step 13. Start the watcher and set up the claim tools.**

- **Who.** Operator.
- **Do.**
  1. The watcher runs on a host other than the validator's (§10.2), against
     each pending root, from its commit until its window closes. Install
     [mainnet/settlement-v2-watcher.example.service](mainnet/settlement-v2-watcher.example.service)
     and
     [mainnet/settlement-v2-watcher.example.timer](mainnet/settlement-v2-watcher.example.timer)
     as `/etc/systemd/system/sn-mainnet-settlement-v2-watcher.service` and
     `.timer`, and enable the timer (SETTLEMENT-V2-EXAMPLES.md:143-161). The
     service runs as the system user `snclaim-watcher`. Its `ExecStart` is
     (usage at `miner/onchain/run.go:32-42`):

     ```bash
     /usr/local/bin/snclaim v2 check --all --vault=<vault2-address> --no_id=1 --rpc=<O21-evm-rpc-url> --api_url=<operator-api-url> --chain_id=964 --challenge --demand --key_file=<O21-watcher-key-file>
     ```

     - `<vault2-address>` is step 8.5's `vault2_address`. Repeat `--rpc` for
       failover. `--no_id=1` is the UR pool, and `--chain_id=964` refuses an
       RPC on another chain.
     - The key file is the watcher's hex secp256k1 key, funded for demands
       at 750,000 rao each plus gas. "Root owns it, its group is
       `snclaim-watcher` and its mode 0440."
     - `--challenge` and `--demand` need `--key_file`
       (`miner/onchain/v2_check.go:204-213`). `--api_url` defaults to
       `https://api.bringyour.com` (`miner/onchain/run.go:64-67`).
     - The timer runs the watcher hourly. That is about 300 blocks at 12
       seconds, so a root's 14,400-block challenge window holds about 48
       runs. A run stops demanding when the window closes
       (`miner/onchain/v2_check.go:677`), and the timer never starts a run
       while one is still sending.
     - "A failed run leaves the service failed; alert on that."
     - `TestV2WatcherUnitExample` parses the `ExecStart` with `snclaim`'s
       real usage and holds the timer to 48 runs a window
       (`miner/onchain/v2_watcher_example_test.go:37-62`).
  2. The claim daemon config is
     [mainnet/claim-daemon-v2.example.yml](mainnet/claim-daemon-v2.example.yml)
     (SETTLEMENT-V2-EXAMPLES.md:125-141; keys at
     `miner/claim_v2_daemon.go:51-112`):
     - `schema_version: 2`, `api_url`, `rpc`, `chain_id: 964`;
     - `vault`: step 8.5's `vault2_address`, lowercase;
     - `batcher`: step 8.5's `claim_batcher_address`, lowercase and not the
       vault. Without O11, delete it;
     - `coldkeys` in SS58, one line each; `key_file`, the relayer's hex
       secp256k1 key, which must exist when the daemon loads; `state_dir`;
       and `poll_seconds` from 5 to 3600, default 30.

     `TestClaimV2LaunchConfigExample` passes the filled example through the
     real loader (`miner/claim_v2_launch_example_test.go:18-62`). "The loader
     accepts any `api_url`, `rpc` and `state_dir` that is not empty, so a
     wrong one is refused only when the daemon starts."
  3. Prepare the daemon's `state_dir`. It is a durable root of its own,
     prepared with a daemon-scope `storage-prepare` request whose owner kind
     is `provider-claim-queue` (SETTLEMENT-V2-EXAMPLES.md:136-139;
     `mainnet/storage_prepare_owners_unix.go:106-107`). Plan and apply it as
     in step 8.2, under the same umask and ancestry rules. Its one owner is
     fixed (`miner/storage_preparation_unix.go:31-58`;
     `miner/claim_queue_capacity.go:9`):

     ```json
     {
       "kind": "provider-claim-queue",
       "relative_path": ".",
       "purpose": "fresh",
       "inputs": {
         "schema": "urnetwork-miner-snapshot-preparation-v1",
         "name": "claim-queue.json",
         "maximum_bytes": 16777216
       }
     }
     ```

     **Gap.** No example of this request is written. Its other fields are
     those of OWNER-CUSTODY-PREPARATION.md:16-39, with `scope: daemon` and
     the state directory as `root_path`.
  4. Run the daemon with the declaration that this preparation returns,
     which it requires (`miner/run.go:136-138`; `miner/claim_v2_daemon.go:643`):

     ```bash
     provider claim-daemon-v2 --config=<path> [--v1-config=<path>] [--threshold-rao=<rao>] [--claim-now] \
       --durable-volumes=<path> --durable-volumes-sha256=sha256:<digest>
     ```

     The threshold is the `--threshold-rao` flag. The pin is a "Reviewed
     sha256: digest" (`miner/run.go:182`), and the declaration is a protected
     file under a protected ancestry, as in step 8 (connect
     `durablevolume/config.go:240-251`).
- **Expect.**
  - The daemon starts only if the batcher's code is exactly the release
    runtime (`miner/claim_v2_daemon.go:445-454`).
  - It claims an account when its claimable reaches the threshold, default
    1.9 α (`miner/run.go:195-197`).
- **Stop if** the daemon refuses its batcher, or refuses a threshold below the
  floor (`miner/claim_v2_daemon.go:640-642`).
- The daemon and `snclaim` need the release that carries section 2.6's sizing
  rule before they claim (step 20).

### Phase C. Activate

**Step 14. Schedule E_s.**

- **Who.** Owner (O18).
- **Do.**
  1. E_s is decided: epoch 2 (O18). Section 9 holds its schedule, and the
     departures from the plan of record that the owner must accept first.
     The plan's own rule takes E_s as the first epoch that starts at least
     two days after the last gate passes, and calls epoch 3 (block 9,348,028)
     not reachable (§14). That rule applies again if E_s moves.
  2. Compute its start. With the weekly policy effective from epoch 1 at
     block 9,247,228 (LAUNCH.md:29-32), `epochStartBlock(e)` is
     9,247,228 + 50,400 × (e − 1) (`evm/src/STCoordinator.sol:302-310`). This
     holds while the v1 policy schedule is unchanged. Epoch 2 starts at block
     9,297,628.
  3. Pick the registration date: one to two days before the start of E_s,
     inside the 21,600-block registration immunity (§12.1 step 5). At 12
     seconds a block that is 7,200 to 14,400 blocks before the start: blocks
     9,283,228 to 9,290,428 for epoch 2.
  4. Check the v1 policy for E_s: `epochBlocks >= rootCommitWindowBlocks +
     14,400` (`evm/src/STSettlement2.sol:86-89`). Mainnet's 50,400 and 1,200
     satisfy it (§5).
- **Note.** The treasury production approval expires at native block
  9,739,882, about 2026-12-17 (§13). That falls inside epoch 10, blocks
  9,700,828 to 9,751,227. Its renewal is a separate successor, after adoption.
  So plan steps 10 to 15 to leave time for that renewal.

**Step 15. Register the pool. This is the activation.**

The Safe sends `registerOperatorPool(1, poolHotkeyV2, maximumBurnRao, E_s)`
with value `maximumBurnRao × 10^9` wei, as a direct call, the way v1's
`registerOperator` went (§12.1 step 5; LAUNCH.md:47-56).
`sn-mainnet register-operator-pool` builds and preflights that call and reads
its result back; "it never signs or sends" (BOOTSTRAP-CONTRACTS.md,
"Settlement v2 activation"). Every flag is required.

- **Who.** Owner, through the Safe; operator runs the tool.
- **Do.**
  1. Repeat step 2, immediately before the review. Its snapshot must show a
     runtime that gate 2 holds on. If the runtime has changed since step 8,
     gate 2 repeats on the new one first.
  2. Review the call. `--pool-hotkey` is `0x` and the hotkey's 32 bytes in
     hex, and `--value-wei` is `maximumBurnRao × 10^9` (O16, O17). The Safe
     must already hold that value, because the preflight runs the call from
     the Safe with it:

     ```bash
     sn-mainnet register-operator-pool review --rpc "$EVM_RPC" --settlement2 "$SETTLEMENT2_PROXY" \
       --safe "$GOVERNANCE_SAFE" --no-id 1 --pool-hotkey "$POOL_HOTKEY_V2" \
       --maximum-burn-rao "$MAXIMUM_BURN_RAO" --value-wei "$VALUE_WEI" --first-epoch "$E_S"
     ```

  3. The Safe sends exactly the printed `to`, `value_wei` and `data`.
  4. Read the registration back, with the Safe transaction's hash:

     ```bash
     sn-mainnet register-operator-pool readback --rpc "$EVM_RPC" --settlement2 "$SETTLEMENT2_PROXY" \
       --no-id 1 --pool-hotkey "$POOL_HOTKEY_V2" --first-epoch "$E_S" --transaction-hash "$SAFE_TRANSACTION_HASH"
     ```

- **Expect.**
  - `review` exits 0. It prints the call, `to`, `value_wei`, `data`,
    `function` and `args`, with `first_epoch_start_block`, `blocks_remaining`
    and `preflight_uid`, the UID the preflight registered at the head.
  - `readback` exits 0. It has confirmed vault2's `PoolRegistered` event,
    `pools(noId)` and `settlementAt(noId, E_s)` with version 2, and it prints
    the event's `uid`, `transaction_hash`, `block_number` and `head_block`.
  - The surplus over the burn returns to the Safe
    (`evm/src/STSettlement2.sol:90-96`).
  - Dry-run evidence, from a rehearsal node: through a SafeL2 1.4.1
    `execTransaction`, the governance Safe's own version (LAUNCH.md:164), the
    registration used 1,654,438 gas, against an `eth_estimateGas` of
    1,797,143, and the surplus returned to the Safe.
  - Within a minute the server's driver sees the pool, and the v1 driver
    keeps only epochs below E_s
    (`server/controller/st_ledger_v2_driver.go:197-214,255-280`).
- **Exit codes**, the same for both modes (BOOTSTRAP-CONTRACTS.md:1003-1008;
  `mainnet/evm_v2_pool.go:33-36,198-205`):

  | Exit | Meaning | Do |
  | --- | --- | --- |
  | 0 | The result is printed: the reviewed call, or the confirmed registration | `review`: the Safe sends exactly the printed call. `readback`: done |
  | 1 | Only a transport or read failure, which decides nothing: no answer, an HTTP error, a reply that is no JSON-RPC, an RPC error without evidence of a failed call, or an output that could not be written | Run it again |
  | 2 | Usage: a missing, unknown or malformed argument. Nothing was read | Correct the command |
  | 3 | `review` refuses, so the call must not be sent, or `readback` differs, so the chain does not hold the reviewed registration. Every revert and EVM error is here, named or not, `OutOfFund` included | Stop |

- **Stop if**:
  - Step 2 shows a runtime that gate 2 does not hold on. Do not send.
  - `review` exits 3. It names the refusal (`mainnet/evm_v2_pool.go:45-77`):
    - before any read, a zero pool hotkey, or a value other than
      `maximumBurnRao × 10^9` wei;
    - a route that is not EVM chain 964, vault2's own escrow hotkey, or a
      first epoch whose start block the head has reached;
    - a call the chain ran and failed, in the preflight or in a view. A
      revert is named by its selector: `InactiveOperator`, `InvalidEpoch`,
      `CadenceTooShort`, `NativeRefundFailed`, vault2's
      `InvalidConfiguration`, `OwnableUnauthorizedAccount` (the Safe does not
      own `STSettlement2`) or the v1 coordinator's `UnknownOperator`;
    - a Safe that holds less than the value: `OutOfFund`;
    - any other failed call, with the node's own message and data. A revert
      without a reason has data `0x`. That "is how the runtime refuses the
      registration itself, for one a burn above `maximumBurnRao`"
      (:276-301; BOOTSTRAP-CONTRACTS.md:988-1001).
  - `readback` exits 3. It names each difference: no receipt; a reverted
    transaction; no `PoolRegistered` event of `noId` from vault2, or one with
    another hotkey or first epoch; `pools(noId)` unregistered or different;
    `settlementAt` with another vault, hotkey or version; a view that the
    chain ran and failed.
  - A reverted registration: "If it fails, nothing has changed, and E_s
    moves" (§12.1 step 5).
- The dry run met the node's real answers before the tool named them: a
  reverted preflight and an unfunded Safe then exited 1. `main` refuses both
  with exit 3 since `e2000ac3`, merged as `1581374a`. Build `sn-mainnet` from
  a `main` that holds it.
- R-4 registers its pool through both modes on the rehearsal node, and
  `TestR4RegistrationRefusals` drives the node's real reverts, `OutOfFund`
  and a reverted view through them. PR-1 to PR-6 run both modes on the
  in-repo chain (`rehearsal/r4_registration_test.go` and
  `mainnet/evm_v2_pool_test.go`).

**Step 16. The switch at the start of E_s.**

- **Who.** Operator watches.
- **Expect.**
  - The validator weights the v2 pool UID, and no longer consults the v1 pool
    hotkey (§9.1).
  - The v1 driver closes and commits nothing at or after E_s.
  - Epoch E_s − 1 completes on v1 in its normal windows. If a v1 step fails,
    v1 rules apply, and the phase C sweep recovers the value (§12.1 step 7;
    §12.2).
  - v1 claims stay open until each v1 expiry, and v1 credit stays withdrawable
    with `snclaim withdraw --vault=v1 --coldkey=<key> --contract=<v1 vault>
    --rpc=<url> --key_file=<path>` (§12.1 step 8; `miner/onchain/run.go:32-42`;
    `miner/onchain/withdraw.go:28-30`).
- **Stop if** the validator refuses "settlementAt differs from vault2's
  immutable pool" (`validator/settlement_v2_resolver.go:75-103`).

### Phase D. The first cycle

The windows, in blocks after end(E_s), from VERSION2-FINAL.md §5:

| Blocks after end(E_s) | What | Who |
| --- | --- | --- |
| 0 to 1,200 | `closeOperatorEpoch(E_s)` captures the pool hotkey's movable stake | anyone; the server |
| 0 to 1,200, after the close | `commitDistribution(E_s)` | root signer |
| from the commit to commit + 14,400 | `demandPath`, `checkPath` | anyone; the server answers |
| from the commit to end(E_s + 1) | `approveDistribution` | the validator |
| at or after commit + 14,400 | `finalizeDistribution`, once approved, with no open demand | anyone; the server |

**Step 17. Close and commit epoch E_s.**

- **Who.** The server; the operator watches.
- **Expect.**
  - `v2close:E_s:1`, then `EmissionCaptured`.
  - The ledger built and published, chunks before the header.
  - `v2commit:E_s:1`, then `DistributionCommitted`
    (`server/controller/st_ledger_v2_client.go:34-49`;
    `server/controller/st_ledger_v2_driver.go:8-14`).
  - Every send passes its `eth_call` preflight and is signed within the
    policy (`server/controller/st_ledger_v2_client.go:389-411`). Commit,
    finalize and answer are signed at the estimate plus 20%, and a close at
    max(estimate + 20%, 3,200,000) gas (`server/controller/st_controller.go:1079-1091`;
    section 2.6).
- **Before this step** the server runs a build at or after `54ae2aacf`, under
  the policy of step 5. The close window opens at block 9,348,028.
- **Act if** `v2_missed_close` or `v2_missed_commit_window` fires (section 5).
  Anyone may close inside the window (§7.2). A missed close defers the epoch with
  zero funding, and the next close captures its emission (§6.4).
- **Act if** a close reverts. No alert names it; section 5 gives its log line.
  Three reverted closes of one epoch strand the root key, one already under
  an undersized per-intent liability (section 7).

**Step 18. Audit and approval.**

- **Who.** The validator; the watcher; the operator reads both.
- **Expect.**
  - The validator audits and sends `approveDistribution` before end(E_s + 1)
    (§9.3).
  - Its diagnostic is `ledger_audit_approve` (`validator/release_diagnostics.go:223`).
    It records the finding and decision under `<state_dir>/settlement-v2-ledger`
    (`validator/settlement_v2_custody.go:3-8`).
  - The watcher reports no fault for every account of the protected root.
- **Act if**:
  - the validator logs `ledger_audit_refuse`, `ledger_audit_challenge` or
    `ledger_audit_demand`;
  - the watcher reports a provable fault or "unverifiable";
  - a demand is open: the server answers it (`v2answer`). Watch for
    `v2_open_demand_over_one_day`.

**Step 19. The challenge window and finalization.**

- **Who.** The server.
- **Expect.**
  - At or after commit + 14,400, with the one approval and no open demand,
    `v2finalize` and `DistributionFinalized`; vault2 then holds one root.
  - The earnings email and `/account/epochs` show the epoch's v2 amounts
    (§8.4).
- **Act if** `v2_root_not_finalized` fires: the root is not finalized by
  end(E_s) + 14,400 + 1,200. A root still unfinalized at end(E_s + 1) is
  replaced by the next commit, and its epochs' funding stays (§4.3 S3).

**Step 20. First claims.**

- **Who.** Operator (O24).
- **Do.**
  1. Read an account (`miner/onchain/run.go:32-42`):

     ```bash
     snclaim v2 status --coldkey=<ss58> --vault=<vault2_address> --rpc=<url> --api_url=<operator api>
     ```

  2. Preflight, then claim it:

     ```bash
     snclaim v2 claim --coldkey=<ss58> --vault=<vault2_address> --rpc=<url> --key_file=<key> --dry-run
     snclaim v2 claim --coldkey=<ss58> --vault=<vault2_address> --rpc=<url> --key_file=<key>
     ```

     With the batcher, `snclaim v2 claim --all --coldkey=<ss58> --batch=<batcher> …`
     claims two or more accounts in one transaction, at most 32.

     Size the claim by section 2.6's rule: max(estimate + margin,
     2,600,000) gas. On `main`, `snclaim` signs a single claim at its
     estimate plus 20% (`miner/onchain/eth.go:202-205`). Until it carries the
     rule, size a single claim by hand: `--dry-run` prints the estimated gas,
     and `--gas_limit` takes the larger of 2,600,000 and that estimate plus
     20%. `--gas_limit` applies to a single transaction, not to a `--batch`
     one (`miner/onchain/run.go:91-94`). A batch already forwards each claim
     the larger of 2,600,000 and the batch's largest estimate
     (`miner/onchain/v2_batch.go:28-35,176-200`).
  3. Then let the daemons and the apps claim. The apps built on SDK
     `3bc7ec95` or later carry the rule. The claim daemon batches two or more
     due accounts, but claims a lone one alone, sized from its estimate
     (`miner/claim_v2_daemon.go:210,268-272,320-326`). So until sn's floor
     merges, a lone daemon claim can run short on 478.
- **Expect.** `Claimed(noId, rootIndex, index, coldkey, cumulative, amount, caller)`
  pays the leaf's coldkey (§7.1), and `status` then shows the amount as
  claimed. "After the first v2 finalization, claim at least one account
  before relying on it" (§17, first row).
- **Stop if** a claim reverts with anything but a documented refusal:
  `AmountTooLow` below the floor, `TooManyStakingHotkeys`, or `NothingToClaim`
  after a third party's claim (§5, §7.3). Then vault2 may not pay, and a
  vault2 defect is permanent for what it holds (section 6; §17).

## 5. Monitoring during the first epochs

The server's v2 alerts are log lines only, `[st]v2 ALERT <name> <subject>: …`.
Each fires once per process and subject, so a restart can fire it again
(`server/controller/st_ledger_v2_driver.go:176-187`). No metric or dashboard
exists for them.

| Watch | Where | Threshold | Response | Source |
| --- | --- | --- | --- | --- |
| `v2_missed_close` | taskworker log | no close by end(e) + 1,200 | the epoch defers with zero funding; the next close captures it | `server/controller/st_ledger_v2_driver.go:349-352,523-526` |
| `v2_missed_commit_window` | taskworker log | no root committed by end(e) + 1,200 | the next root covers the epoch from its own usage | :353-359,655-658; VERSION2-FINAL.md §6.4 |
| `v2_open_demand_over_one_day` | taskworker log | a demand open for one day, 7,200 blocks at 12 seconds | check the server's answer of that demand | :366-371 |
| `v2_root_not_finalized` | taskworker log | not finalized by end(e) + 14,400 + 1,200 | check approval, demands and pause | :378-388 |
| `v2_sn25_prune_risk` | taskworker log | SN25 among the lowest 3 non-immune subnets by moving price, or no longer added; read every ten minutes | encourage claims (§17); dissolution strands custody | `server/controller/st_ledger_v2_prune.go:33-38,138-165` |
| `[st]v2 prune check failed` | taskworker log | any | the census read failed | `server/controller/st_ledger_v2_driver.go:317-321` |
| `[st]v1 cutoff for epoch … unknown` | taskworker log | any | a `vault2.pools` read failed; that turn's v1 steps wait | `server/taskworker/work/st_ledger_v2_work.go:29-37` |
| "sn: selected deployment differs from reviewed earnings schedule" | server log | any | st.yml and sn.yml disagree; every mainnet send is refused | `server/provider_payout_transition.go:161-163` |
| "unsigned gas/fee/tip/intent envelope exceeds approval" | server log | any | the gas policy is too low for a send | `server/model/st_transaction_gas_model.go:109-110` |
| a reverted close | taskworker log | any. No alert names it. v2: a `[st]v2 epoch step epoch <e> …: failed …` line; v1: `[st]epoch <e> close failed (attempt <n>): st: epoch <e> emission close not confirmed: failed …`. Each reason holds `st: finalized transaction <hash> reverted` | find the cause before the same epoch's close reverts a third time: the fourth try strands the root key (section 7) | `server/controller/st_controller.go:1279-1282,3646-3651`; `server/controller/st_ledger_v2_driver.go:454-466`; `server/taskworker/work/st_ledger_v2_work.go:90`; `server/taskworker/work/st_work.go:244` |
| a stranded root key | taskworker log | sends refused with `operator gas allowance is unavailable or exhausted; original liabilities retained: original logical intent <key>` | every later v1 and v2 send of the root key stops at the stranded intent. Re-signing the policy does not lift it; a transaction from outside the engine that consumes the reserved nonce frees the key | `server/model/st_transaction_gas_model.go:24,363-364`; `server/controller/st_ledger_v2_gas_test.go:349-426` |
| `urnetwork_api_reserved_upload_startup_failed` | API metric | 1 | the reserved upload lane is off | `server/api/run.go:33-38,255-272` |
| `ledger_audit_approve`, `_challenge`, `_demand`, `_refuse`, `_retrying` | validator diagnostics | anything but an approval of an honest root | read the custody record | `validator/release_diagnostics.go:223`; `validator/settlement_v2_custody.go:3-8` |
| the byte budget | validator audit | a root's new objects over `--ledger_max_bytes` | raise the flag; it needs no re-sign | VERSION2-FINAL.md §6.6; `validator/settlement_v2_runtime.go:3-5` |
| the watcher's report | `snclaim v2 check` output | any provable fault or "unverifiable" | challenge or demand inside the window | VERSION2-FINAL.md §10.2 |
| the watcher's service | systemd on the watcher host | the service is failed | "A failed run leaves the service failed; alert on that" | SETTLEMENT-V2-EXAMPLES.md:159-161 |
| claim gas | claim receipts | about 1.27M gas a claim instead of about 0.2M | a third party filled vault2's staking-hotkey list; raise daemon thresholds to about 10 α | §3, §10.1, §17 |
| validator weights | every tempo | a tempo without weights | the escrow hotkey earns by stake in such a tempo; that is surplus | §7.3, §17 |
| the runtime | `sn-mainnet runtime-snapshot` | a `specVersion` after 478, the runtime of step 8 | rehearse vault2 on it (gate 2) | §14, §17; step 2 |
| the treasury approval | the native head | block 9,739,882 | renew before it, as a separate successor | §13 |

## 6. Abort and rollback

### 6.1 What each step leaves behind

| Step | Can it be undone? | How |
| --- | --- | --- |
| 1 to 4 | Yes | Nothing in production changed. |
| 5, gas policy | Only forward | A changed policy must be the next revision naming the current digest; the server refuses "operator gas policy rollback or disconnected revision" (`server/model/st_transaction_gas_model.go:389-391`). To lower the caps, sign a further revision with the same approver key. |
| 6, server build | Yes, while v2 is off | v2's migrations 821 and 822 add new tables only, which v1 settlement does not read (`server/db_migrations.go:10013-10025`). An older binary requires a lower migration head, so it passes readiness (`server/router/warp_handlers.go:186-189,255-260`). Upstream's 823 to 831 come with the same `main`; their rollback is upstream's, not this runbook's. |
| 7, validator build and app releases | Yes | Without the block, the new validator hashes today's config identically (§13). The released app code hides the v2 claim without `Vault2Address`. |
| 8, contracts | No, but they can be abandoned | vault2 is immutable. Until a pool registers it holds nothing and nothing depends on it (BOOTSTRAP-CONTRACTS.md, "Two residuals remain"). A redeploy needs a new approval, a fresh escrow hotkey and fresh nonces. After step 10 a redeploy also meets the validator's one-way adoption (row 10). |
| 9, `Vault2Address` | Yes, by a release | The compiled default is never stored on the device (`sdk/sn_chain.go:352-355`; `sdk/sn_local_state.go:72-80`), so a release that clears or changes it takes effect on update. This is an inference from the code. |
| 10, adoption | No | The block "can be neither re-pointed nor removed" (`validator/production_authority_history.go:256-260`). Until step 15 the validator still weights through `operatorAt` (§9.1). No repository path re-points it; a redeploy after adoption needs new validator code. |
| 11, server switch | Before step 15 only | Remove the pair from st.yml and sn.yml together, then restart. v1 is untouched. After step 15, do not remove it. Without the pair the v1 driver again closes and commits every epoch, E_s and later included (`server/controller/st_ledger_v2_driver.go:263-266`), against the v1 cutoff (§8.1). And the mirror misses every v2 event while the pair is absent, with no backfill (`server/controller/st_controller.go:2605-2611,4262-4265`). Coming: a server latch, so that once the server has observed the pool's registration it fails closed without the pair. Its exact refusal text will be cited here. |
| 15, pool registration | No | It is the commitment point. `register-operator-pool review` refuses, before any send, what the chain would refuse (step 15). A revert changes nothing, and E_s moves (§12.1 step 5). |

### 6.2 After activation

- **Pause.** The guardian or the Safe pauses v1 settlement, and with it v2
  closes, commits and finalizations. Claims, approvals, challenges and
  demands continue (§4.1; §4.3 S5).
- **Upgrade.** The Safe can upgrade `STSettlement2`, which is UUPS (§7.2).
  vault2's guarantees G1 to G6 hold whatever `STSettlement2` does (§4.2). An
  upgrade is new code, reviewed as such, outside phase A.
- **vault2** cannot change. A vault2 defect is permanent for the funds it
  holds (§17, first row).
- **The transfer switch.** Only the SN25 owner `ur-owner`, or the chain's
  root, can freeze claims (`sudo_set_toggle_transfer`, §4.1).

### 6.3 Server and connect order

- **Deploy:** `bringyourctl db migrate`; then the server binaries, api, alt,
  taskworker and `bringyourctl` among them (step 6); then, later, st.yml and
  sn.yml together; then a restart (step 11). Old binaries refuse an sn.yml
  with the v2 keys (`server/provider_payout_transition.go:57-58`).
- **Roll back:** the config first, both files together, then a restart, then
  the binaries. Never roll back the config after step 15.
- **connect** takes no part. No v2 code runs in the connect library or in the
  server's connect service. The v2 routes live in api and alt, and the driver
  in the taskworker (step 6).

### 6.4 What happens to accrued v2 epochs

- **Funding** snapshots are written once and never change (G5).
- **Finalized roots** are all kept. Their accounts stay claimable by anyone,
  for good (G4). Contracts never stop claims (S5).
- **A missed close** defers its epoch with zero funding. The emission stays on
  the pool hotkey, and the next close captures it (§6.4).
- **An unfinalized root** is replaced at the next commit after end(e + 1), and
  its epochs' funding stays (S3). The next root covers every epoch since the
  last finalized root, each allocated from its own usage, at most 96 (§6.4).
- **A long outage**: at 1M providers the default `--ledger_max_bytes` covers
  about 36 epochs of rows not yet fetched; raise it for a longer one (§6.6).
- **While the proof service is down**, the apps show "unavailable", never zero
  (§11). `snclaim` needs the published objects, which the validator retains
  (§6.5).
- **v1** epochs before E_s are unaffected. The phase C sweep recovers v1
  carry and reveal-lag emission (§12.2, §16).

## 7. Risks carried at launch

From VERSION2-FINAL.md §17:

| Risk | Mitigation at launch |
| --- | --- |
| A vault2 defect is permanent for the funds it holds | Gates 1 and 2 (steps 3, 4); claim at least one account after the first finalization (step 20) |
| The rehearsal node is a fast-runtime dev chain, not mainnet | It keeps mainnet's call set, weight formulas and gas schedule; gate 2 cannot be waived; any runtime change repeats it (steps 2, 3) |
| vault2 depends for good on the staking precompile's `getStake`, `getMinerCollateral`, `moveStake` and `transferStake` ABIs | Rehearse each runtime release with vault2's exact bytecode when it is published; encourage claims, which keeps custody small |
| SN25 is dissolved, which strands vault2's custody (accepted by the owner, 2026-10-11) | The `v2_sn25_prune_risk` alert; encourage claims through the daemon and the apps |
| `ur-owner` or root disables stake transfers on SN25 | Nothing is taken or redirected; claims revert and stay claimable; captures continue. Recommended: hold `ur-owner` under the Safe's governance (O25) |
| Someone registers the escrow hotkey between vault2's creation and `registerEscrow` | Back-to-back sends at consecutive nonces (step 8.4); otherwise a redeploy, with no funds at risk |
| Admission bypassed, with the tree withheld | Demands block finalization and may name any finalized root; watchers sample by value (step 13) |
| A finalized root that is not tight, or that lowered an account of an older root, halts honest successors | The validator refuses to build on a root that is not tight; anyone can delete such a successor; recovery needs a reviewed validator release |
| Demand griefing | Each demand costs its gas and the 750,000 rao fee; answers are automatic |
| Stake moves need a gas limit far above their use, and a refused precompile call burns all its forwarded gas | Preflight with `eth_call`, size with `eth_estimateGas`; the re-signed gas policy (step 5); anyone can send a long deferring close. On 478 the sizing rule of section 2.6 keeps each stake move at its pre-478 size |
| Third parties fill vault2's staking-hotkey list | Claims and `absorbStake` keep working at about 1.27M gas; raise daemon thresholds to about 10 α. On 478 each entry also adds 8,750 gas to a stake exit's admission, which the sizing rule of section 2.6 covers |
| A missed close moves an epoch's share to the next epoch's earners | The 1,200-block window and the alerts (section 5) |
| App users without a paying consent stay unpaid | Network payout consent in the SDK and every app (§11.1), independent of phase A and not a gate. A consent takes effect from the epoch after its signing, and epochs before a network's first consent stay unpaid. Ship it with step 7's release, after ur.io has the consent bridge page, so networks can sign before E_s |
| The owner's availability for Ledger-signed steps | Plan steps 5, 8, 10 and 15 with the owner |
| The treasury approval expires about 2026-12-17 | A separate successor after adoption (step 14 note) |
| Small balances are uneconomic to claim | Accrual never expires; the published threshold; an optional UR subsidy |
| The escrow hotkey earns emission in a tempo without weights | The validator sets weights every tempo; escrow earnings are surplus; claims accept a destination one rao short |
| A later v1 policy breaks `epochBlocks >= rootCommitWindowBlocks + 14,400` | A governance constraint on the v1 policy schedule (§13) |

Found while writing this runbook:

| Risk | Mitigation |
| --- | --- |
| The validator unit carries no `--ledger_auditor_key_file`, so no root is approved | The drop-in `mainnet/validator-ledger-auditor.example.conf` (step 10), and the unit check of step 12 |
| st.yml and sn.yml disagree, which refuses every mainnet send, v1 included | One change for both files, `sn25golive st-check` and the step 11 stop conditions |
| A failed `vault2.pools` read delays v1 closes and commits | The log line of section 5; RPC health |
| The mirror has no v2 backfill, and without the pair the v1 driver resumes epochs at or after E_s | Switch the server before step 15, and never remove the pair after it. Coming: a server latch that fails closed without the pair once it has observed the pool's registration; its refusal text will be cited here |
| An app is built against a store without its strings | The store's `main` holds the `sn_ledger_*` and `sn_consent_*` keys (section 2.1); build each app against it or a later one |
| No CI anywhere | The owner's pipeline and the hand-run suites of section 2.2 |
| On `main` the rehearsal tooling builds only runtime 477, and mainnet runs 478 | Step 2's check; the 478 pins land through sn `v2/runtime-478`; a reviewed pin change before any rerun on a later runtime |
| A new runtime lands between two steps. 478 was installed 318 blocks after it was proposed | Step 2 immediately before steps 8 and 15. A send on a changed runtime is refused, and an inclusion under an unapproved runtime stays unresolved (step 8.3) |
| The binary lists no reviewed source for runtime 478, so it refuses the phase config | 478's entry on sn `v2/runtime-478`; freeze and rebuild after its merge (steps 1 and 8.3). Never name another runtime's commit: the v2 phase now refuses a reviewed source beside another version |
| A shell umask of 002, or a group-writable directory above the run directory, makes the custody tools refuse | `umask 077` and the ancestry check at the start of step 8 |
| One `resume --online` does not complete an action, and a late start checkpoint multiplies the calls | Take the start checkpoint just before the approval; repeat `resume --online` until the completed status (steps 8.3 and 8.4) |
| On 478 a stake exit's admission grows by 8,750 gas for each entry of vault2's own staking-hotkey list, which third parties can lengthen. A limit sized from an estimate can then fall short | The owner-approved sizing rule of section 2.6: a close at max(estimate + 20%, 3,200,000) gas and a claim at max(estimate + margin, 2,600,000). The server's v2 close floor and the SDK's claim floor are on `main`, and ur.io sends the SDK's limit; sn's claim tools apply the claim floor. Deploy a server build at or after `54ae2aacf` before the first close, at block 9,348,028 |
| Three reverted closes of one epoch strand the operator root key. The policy allows each operation three signatures, so the fourth try reserves a nonce the policy will not sign, and every later v1 and v2 send of the root key stops at it. Re-signing the policy, even with more signatures, does not lift the cap the operation's first signature recorded. Only a transaction from outside the engine that consumes the nonce frees the key. This defect predates v2 | A root-cause fix, which decides before reserving a nonce, is being built. Meanwhile: watch for reverted closes (section 5), and find the cause before a third (`server/controller/st_ledger_v2_gas_test.go:349-426,462-467`) |
| A per-intent liability that covers one floored close strands the root key after a single revert, because the policy sums an operation's liability over every nonce it takes | `maximum_intent_liability_wei` of at least 3 × `maximum_gas` × the fee cap (step 5; `server/controller/st_ledger_v2_gas_test.go:428-460`) |
| The v1 close shares the root key and is signed at its estimate plus 20%, with no floor, so on 478 it can be griefed into reverting the same way | A v1 close floor is being built. Meanwhile, watch v1 closes too (section 5; `server/controller/st_ledger_v2_gas_test.go:134-138`) |
| Under today's gas policy, `maximum_gas` 1,000,000, the server refuses every v2 close before any signature and keeps its nonce, so the root key's other sends stop at it | Re-sign the policy before the first v2 close (step 5); the re-signed policy lets the retained close through (`server/controller/st_ledger_v2_gas_test.go:196-203`) |
| The loader admits an adoption that appends no authority history entry, and the validator then refuses every decision made under the running config | The by-hand check of step 10, sub-step 2, and step 10's stop condition |
| The repair and activation controllers refuse the adopted validator, and the rendered unit starts the pre-adoption config without the auditor key | Restart the unit with its drop-in. Around a controller run, remove the drop-in and reinstall it (step 10) |
| `preview` cannot tell a wrong but well-formed owner input, and it checks `native_chain` and `specName` only for being non-empty | The comparison step of `mainnet/SETTLEMENT-V2-FILL.md`, read against the owner inputs, and the by-eye review of step 8.3 |
| The claim daemon's state directory is not a prepared durable root | The `provider-claim-queue` preparation of step 13 |

## 8. Follow-ups after launch

Open owner decisions and work that the repositories leave open:

1. **The SN25 owner key.** Decide whether and how `ur-owner` comes under the
   Safe's governance (O25; VERSION2-FINAL.md §17; contract review M2,
   STATE.md:91,99).
2. **The treasury approval renewal**, before native block 9,739,882, as a
   separate successor after adoption (§13).
3. **Custody retention.** The validator keeps every ledger object it fetched,
   with no pruning (`validator/settlement_v2_custody.go:3-8`). At 1M providers
   that grows by about 46 MiB of accounts per root and about 54 MiB of rows
   per epoch (§6.6). Decide its retention and disk. Retain the step 8 run
   directory as the v2 deployment record.
4. **The gate 3 CI job.** SV-8's `TestEvmV2*` runs only in the aggregate
   normal job (STATE.md:129; `scripts/test-release-1.0-local.sh`:116-119), and
   the `mainnet` package exceeded the 90-minute budget on the build machine
   (STATE.md:133). No CI runs any of this (section 2.2).
5. **The phase C sweep**, with `E_sweep >= E_s + 8` (§16).
6. **Network payout consent** (§11.1), the largest single increase in
   providers paid. It ships on its own, independent of activation, after
   ur.io has the consent bridge page (step 7). If it misses step 7's release,
   release it as soon as it is ready: a network's app-only providers stay
   unpaid until the epoch after it signs.
7. **The Windows v2 Claim colour**, blue or lime (STATE.md:200).
8. **Tooling gaps** marked in section 4:
   - being closed: the reviewed source of runtime 478 (step 8.3) and the
     rehearsal pins for 478 (step 2), through sn `v2/runtime-478`;
   - being closed: sn's claim floor for a single claim (section 2.6);
   - being closed: the server's v1 close floor, and the fix that decides
     before reserving a nonce, so that a spent close cannot strand the root
     key (section 7);
   - being closed: the local-mode release tooling and its config field, sn
     `v2/release-local` (step 1);
   - coming: the server latch of section 6.1;
   - a rendered validator unit that names the adopted config and the auditor
     key, for the repair and activation controllers (step 10);
   - an example of the claim daemon's `provider-claim-queue` request
     (step 13);
   - build and run instructions for `sn25golive` (step 5);
   - rehearsal pins for a runtime after 478 (step 2).

   Closed since the first version of this runbook:
   - the escrow registration's envelope (step 8.4), and the
     `registerOperatorPool` review and readback (step 15);
   - the examples of the custody request, the v2 phase config, the claim
     daemon config and the watcher units (steps 8.2, 8.3 and 13);
   - the validator unit's auditor flag, through the drop-in, and the written
     adoption commands (step 10);
   - the recipe that fills the phase config, `mainnet/SETTLEMENT-V2-FILL.md`
     (step 8.3);
   - the reviewed source of runtime 477, its binding to the spec version and
     the record of gate 2 on 477 (steps 2 and 8.3);
   - `register-operator-pool` refusing every call the chain ran and failed,
     with exit 3 (step 15);
   - the server's v2 close floor and the SDK's claim floor on 478, with ur.io
     sending the SDK's limit (section 2.6).

## 9. Schedule for E_s = 2

Owner decision, 2026-10-13: "we are launching v2 now to be active the next ur
epoch". The current epoch is 1, so E_s is epoch 2 (O18).

One chain reading anchors the times below: the public RPC's `eth_blockNumber`
was 9,276,620 at 2026-10-13 20:07 UTC. It is an observation, not a repository
fact. The blocks are exact (step 14). The times assume 12 seconds a block
(`server/controller/st_controller.go:89`), so they are approximate.

### 9.1 Blocks and times

| Block | About, UTC | What | Source |
| --- | --- | --- | --- |
| 9,276,620 | 2026-10-13 20:07 | the reading, in epoch 1 | |
| 9,283,228 | 2026-10-14 18:10 | the plan's registration window opens, 14,400 blocks before E_s starts; about 22 hours after the reading | §12.1 step 5 |
| 9,290,428 | 2026-10-15 18:10 | the plan's registration window closes, 7,200 blocks before E_s starts; about 46 hours after the reading | §12.1 step 5 |
| 9,297,628 | 2026-10-16 18:10 | epoch 2 starts, about 70 hours after the reading. This is the hard on-chain limit: `registerOperatorPool` must be mined before this block, while the current epoch is 1 | `evm/src/STSettlement2.sol:80-82` |
| 9,348,028 | 2026-10-23 18:10 | end(E_s): the close and commit window opens. The server build that applies the 478 sizing rule is deployed before this block (section 2.6) | §5 |
| 9,349,228 | 2026-10-23 22:10 | close and commit by this block | §5 |
| 9,362,428 to 9,363,628 | 2026-10-25 18:10 to 22:10 | the earliest finalization, 14,400 blocks after the commit | §5 |
| after the finalization | | the first claims (step 20) | §17 |

### 9.2 A compressed order of steps 1 to 15

Steps 1 to 12 must be complete before step 15 is sent. Their dependencies set
the order:

| Order | Steps | Runs in parallel | Owner signs |
| --- | --- | --- | --- |
| 1 | 1: the last merges, with the reviewed source of runtime 478; the local-mode release tooling; the lock refresh as sn's last commit; then build in the local mode | with step 2 | |
| 2 | 2, 3, 4: the runtime check and the release gates. Gate 2's rerun on runtime 478 has passed, R-1 to R-7 | with each other | O26, the gate 1 confirmation |
| 3 | 5, 6, 7: the gas policy, the server build with migrations, the validator binary and the tools | with each other, and while 478's reviewed source merges | step 5, with the gas approver key |
| 4 | 8: deploy the contracts, after steps 2 to 4 pass, 478's reviewed source is merged, `sn-mainnet` is rebuilt with it, and step 2 has run again | its six actions run in sequence | the phase approval, with the approval key; then six transactions, with the deployer key |
| 5 | 10: adopt the validator config, after step 8 and step 7's validator binary | with step 13 | the treasury approval, with its approval key |
| 6 | 11, then 12: the server switch, after steps 6, 8 and 10 | with step 13 | |
| 7 | 15: register the pool, inside blocks 9,283,228 to 9,290,428, after step 2 once more | | the Safe |

- Step 8 waits for the merge of 478's reviewed source and for a rebuild of
  `sn-mainnet`. Gate 2's rerun on runtime 478 has passed (section 9.3,
  item 2). Steps 5 to 7 touch nothing that the runtime decides, so they
  proceed meanwhile.
- The server build that applies the 478 sizing rule is deployed before block
  9,348,028 (section 2.6). It can follow step 15.
- Step 2 runs again immediately before step 8 and immediately before
  step 15.
- Step 9, the pinned app releases, moves after step 15 (section 9.3, item 4).
- Step 13 is off the critical path. The watcher must run by the first commit,
  at block 9,348,028 or later.
- Step 14 is decided.
- Known durations:
  - the rehearsal node built in about 37 minutes from scratch for 475, and
    in 648 seconds for 477 on a copy of that build. The R battery ran in
    about 15 minutes on 475 and in 776 seconds on 477
    (`scripts/runtime-rehearsal/README.md`:48-55,235-237,437-441), and in
    1,091 seconds on 478;
  - the release gate's `mainnet` package alone exceeded 90 minutes on the
    build machine (STATE.md:133);
  - v1's eight contract actions landed within 84 blocks, about 17 minutes
    (LAUNCH.md:36-45).

### 9.3 Departures from the plan of record

Each item needs the owner's explicit acceptance, and this runbook accepts
none of them. Item 2 is not open to acceptance: it is a gate. Its rerun has
passed, and step 8 waits for 478's reviewed source and a rebuild.

1. **The two-day rule.** "E_s is the first epoch that starts at least two days
   after the last gate passes" (§14). For epoch 2 the last gate, gate 6 at
   step 12, would have to pass by block 9,283,228, about 22 hours after the
   reading. §14 also calls epoch 3 "not reachable", and epoch 2 is earlier.
2. **Gate 2 on runtime 478.** Gate 2 ran on 475. "Any runtime change on
   mainnet repeats this gate" (§14), and "gate 2 cannot be waived" (§17).
   Mainnet has since moved to 477 and, at block 9,276,925, to 478 (step 2).
   The contracts will be deployed on 478, so gate 2 must hold on 478.
   - The verdict on 478: R-1 to R-7 all pass on a node built from v478, with
     `main`'s bytecode. R-1 took 140 seconds and the battery 1,091 seconds.
     The deploy gas is unchanged from 475. The changes are in section 2.6.
   - 478's reviewed-source row, `crv4.NativeOwnerSource478`, admits commit
     `850f2755…` at version 478, and R-1 on the 478 node names it and passes.
     Without the row the phase refuses both choices on 478: 478's own commit
     with "contract phase runtime/source approval is incomplete", and 475's
     or 477's commit with version 478 with "contract phase runtime version
     differs from the one its source commit was reviewed for"
     (`mainnet/evm_phase.go:183-191`).
   - The 477 run passed in full, with gas equal to 475's. It is recorded on
     `main` (`docs/spec/runtime-477-audit.md`:405-408;
     `scripts/runtime-rehearsal/README.md`:425-441).
   - Step 8 waits for `sn-mainnet` rebuilt from a `main` that has 478's
     reviewed source. The 477 entry (`3b5628aa`) binds each reviewed source
     to its spec version for the v2 phase, and the 478 entry, the 478 pins
     and the record of this rerun come with sn branch `v2/runtime-478`
     (`docs/spec/runtime-478-audit.md`).
   - Step 2 is repeated immediately before step 8 and before step 15.
3. **O26.** Gate 1 needs the independent review of vault2, `LedgerTree.sol`
   and `STSettlement2` resolved (§14). The owner's confirmation is not given
   yet, and step 8 waits for it.
4. **The apps.** Activation needs v2 claims in the apps (§2.1 decision 8; §11;
   §14 gate 5). The releases with the pinned `Vault2Address` (step 9) cannot
   precede step 15 here. Claims are impossible before the first finalization,
   at block 9,362,428 or later, so the apps have until then.
5. **Consent.** A consent takes effect from the epoch after its signing
   (§11.1), so only one signed before block 9,297,628 covers epoch 2. Consent
   signing is now on every app's `main` (Pins). STATE.md records no release
   that carries it, and ur.io not yet deployed with the bridge page the apps
   sign it through (STATE.md:268,297). Unless both happen and a network signs
   before that block, no app-signed network consent is in effect for epoch
   2, and app-only providers stay unpaid in it, as in v1.
6. **Other validators.** A validator weights the v2 pool UID only through its
   `settlement_v2` block; without it, it resolves every epoch through
   `operatorAt` (§9.1). Any third-party validator must adopt before E_s.

### 9.4 Decision point

If any stop condition fires, or the registration window of section 9.1 is
missed, E_s moves to epoch 3, which starts at block 9,348,028. Its
registration window is blocks 9,333,628 to 9,340,828. The chain itself takes
a registration for epoch 2 until block 9,297,627; the plan's window is the
earlier one.

Nothing is lost before step 15: "If it fails, nothing has changed, and E_s
moves" (§12.1 step 5). What steps 5 to 12 put in place stays as section 6.1
describes it.
