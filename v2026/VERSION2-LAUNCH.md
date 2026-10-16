# Version 2: launch plan

Prepared 2026-10-13. This is the runbook for deploying and activating
settlement v2, phase A of [VERSION2-FINAL.md](VERSION2-FINAL.md), on SN25
mainnet. The owner runs it. The plan of record wins wherever the two differ.

Pins. These `main` heads are the runbook's premises, and its file and line
citations hold at them. They were read at about 01:00 UTC on 2026-10-14.
[VERSION2-HANDOFF.md](VERSION2-HANDOFF.md), section 1, gives the current
heads; a citation into another repository at a later head names it:

| Repository | `main` head |
| --- | --- |
| sn, this repository | the revision that holds this runbook |
| server (`server/`) | `72699c819` |
| sdk (`sdk/`) | `1ee41b40` |
| connect | `8d22832d` |
| localizations (`localizations/`) | `3a1e7ca` |
| apple | `70f3ce7a` |
| android | `a46813ea5` |
| windows | `e549203` |
| linux | `3332164` |
| mmm | `ec4760983` |
| web | `9526a97b` |
| proxy, warp and xops, release repositories of step 4 | `ada3aac`, `99e01ec` and `770fffc` |

Merged since the previous version of this runbook:
- the dual-stack validator hops, in all four repositories: server
  `72699c819`, connect `8d22832d`, sdk `1ee41b40` and sn `00e383fe`
  (section 6.3);
- the server's fix for a stranded root key (`8d63033c9`, merged as
  `4e5759875`; section 7);
- ur.io sending the SDK's gas limit with v1 claims too (mmm `ce87b16e3`,
  merged as `37859e302`; section 2.6);
- the release gate's selection of the v1 close floor's tests and of
  `sn25golive`'s (sn `f3cc4492`, merged as `6caf9667`; step 4);
- upstream's server migrations 833 to 835 (step 6).

Merged on 2026-10-14 and 2026-10-15, after the Pins were read
(VERSION2-HANDOFF.md, section 1, which gives the heads):
- the release gate in its local mode, with `./mainnet`, `./validator`,
  `./sim-testnet` and connect's race run in hash shards, the `./mainnet`
  exports job, and the job runner's fix (sn `819f00c5`; step 4);
- the economic monitor's fixes: the witness budget, the archive's Yuma carry
  and the window clock (sn `f50e2f913`, `c9d5988af` and `ce5fc285e`);
- the H3 and alt admission fix (server `0d98bde5b`), whose tests the gate runs
  (step 4);
- the task runner's finalization fix and the drain handback (server
  `9b6368637` and `94b9b89bd`);
- the validator's provider request checkpoint as a fixed v2 record that fits
  ext4 (sn `27e786d97` and `c3628860f`), and its rule: no validator downgrade
  after any v2 checkpoint write (step 7; section 6.1, row 7);
- the connect restore re-apply fix (connect `e6af5701c`);
- the release gate's repairs since its second official run (step 4): the
  supervisor test's leak (sn `9974350b`), the receipt test (sn `5a6aaa08`),
  the SMTP 587 test (connect `0e3ded62`), the isolation pin's TUN subnets
  (server `3738da153`) and the job runner's held fixture (sn `64664738`),
  which failed the second to fourth runs; the taskworker race and the proxy
  tests' isolation, which the pre-check found (server `a8cc5103c` and
  `e22d50d09`); cheaper long tests (sn `ac6a73ed`, `aea400a9` and
  `e6c5d9fe`, connect `1aa99b73`); connect's test hygiene (`23849763`); two
  drifts between repositories (connect `2b71b989`, sdk `d2114f9c`); and the
  selection of the HF-1 replay tests and of the deposit key's fix's tests
  (sn `3acfaaac` and `59ef6bbd`);
- the deposit key's fix, which signs a reserved deposit whose deadline has
  passed, so that it reverts and frees the key (server `ed453384d` to
  `adc257c06`, merged as `d43dd4af1`; section 7);
- other sessions' product merges: the server's capability probe, switched
  off, and the FP2 modern provider gate, which turns on by itself at
  2026-10-21 00:00 UTC; lag.dev changes; the SN25 commitment oracle and its
  validator exclusion; and the validator's replay prefetch and intent read
  budget (VERSION2-HANDOFF.md, section 1).

Every branch that VERSION2-HANDOFF.md, section 2, listed has merged except
a review and the work on the open product issues, which its section 5
lists. The deposit key's gas floors and the apps' 0.02 TAO v1 gas top-up
hint have landed (VERSION2-HANDOFF.md, section 1).

The server's written recovery of a root key that an older build stranded has
landed: `docs/operator/st-stranded-nonce.md`, server `912859cd`, merged by
`10b0bd3f` (section 6.5).

The launch is not sealed. The release gate runs in its local mode on the
latest `main` of every repository. A failure is fixed, merged and pulled, and
the gate runs again, until it passes. The heads it passes on are the launch
revisions, O1 (steps 1 and 4). The owner's decisions of 2026-10-14: "The gate
should just run against the local checkout. We don't need to seal the
version. That is just overhead we can avoid." And: "Pull the latest main code
on all repos. That is what the gate should use. If it fails, fix, commit,
merge, push, pull, repeat."

The orchestration log is [VERSION2-STATE.md](VERSION2-STATE.md), cited as
STATE.md. It is a snapshot of 2026-10-13 20:15 UTC; its lines 1 to 286 keep
the log's numbers. Step 5 has been run in production, on 2026-10-14
(step 5). Unprefixed paths are in this repository.

Conventions:

- **Owner** is the owner, with the Ledger-held keys
  ([mainnet/LAUNCH.md](mainnet/LAUNCH.md):161-166). **Operator** is whoever
  runs the UR server, the validator host and the tooling.
- A bare § cites VERSION2-FINAL.md. This runbook's own parts are called
  sections and steps.
- `O1` to `O28` are owner inputs (section 3). A step does not start until its
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
    (`server/api/api.go:341-342`);
  - the v2 readers: the earnings email, `/account/epochs` and the event
    mirror (§8.4);
  - the commands `v2-unrecoverable`, `v2-rebuild` and `v2-rebuild-withdraw`
    (§8.6), and `st v2-activation` and `st v2-activation-clear` (SV-9;
    `server/bringyourctl/main.go:137-141`);
  - migrations 821 and 822, new tables only
    (`server/db_migrations.go:10013-10049`), and migration 832, the
    activation record's table (:10169-10187);
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
     every epoch (`server/controller/st_ledger_v2_driver.go:263-297,325-342`).
     The server records no activation yet, so its v2 pair can still be
     removed (`server/controller/st_ledger_v2_activation.go:18-25`).
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
     (§8.1; `server/controller/st_ledger_v2_driver.go:263-297`);
   - on its first successful read of the registration the server records
     the activation, which arms what its code calls the latch: from then on
     its st subsystem runs only with the recorded v2 pair (SV-9; section
     6.1, row 11);
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

  The source is the server's runbook (`server/SN-RECONCILE.md`:40-53) and
  STATE.md:211-212. Its branch, `v2/sn-reconcile` (`513d600d`), is in server
  `main`. The track meets this launch in one place: sn.yml is parsed with
  known fields only (`server/provider_payout_transition.go:57-58`). A
  `bringyourctl` built from the launch revisions knows sn.yml's v2 fields
  (:45-47), so the reconcile can run before or after step 11. This is an
  inference from the code, not a documented rule.
- **The dual-stack validator hops** are not part of settlement v2, but they
  are on `main` in sn, server, connect and sdk, so the launch's builds carry
  them (Pins).
  - Their own deploy order applies: the server's API and taskworker first,
    then its connect service, then the validators (`VALIDATOR.md`:812-828,
    "Rolling deploy"; connect `IPV6.md`:332-341). Steps 6 and 7 follow it.
  - `hop_latency_from_attributed_attempt` stays off. It is a server-only key
    of verify.yml's `settings`, off when absent
    (`server/controller/verify_controller.go:233-236`;
    `server/controller/verify_dual_stack_test.go:725-726`). It goes on only
    after every validator, third-party ones included, runs the receipt
    check that accepts a confirmed hop's moved `assigned_ms`
    (`VALIDATOR.md`:820-825).
  - Their egress-index versions are unrelated to settlement v2.

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
- The gate's fixes have landed (the preamble). The launch revisions are the
  heads on which step 4's gate passes (O1), and the runbook is then read
  against those heads.
- The dual-stack validator hops are on `main`, so the launch's builds carry
  them, and their deploy order applies (section 6.3).
- The gVisor fork is a release repository. The local build and the gate
  compile it from a sibling checkout `gvisor`, on its `go` branch (step 1).
- The localization store's `main` (`3a1e7ca`) holds the eight `sn_ledger_*`
  keys (VERSION2-FINAL.md §11) and the eleven `sn_consent_*` keys (§11.1).
  Every release build regenerates the app
  catalogs from the store (`localizations/README.md`:23-25), so each app is
  built against that store or a later one.
- The server is rebuilt against the sn revision that carries the new
  successor class (VERSION2-FINAL.md §13, Server row).
- Every launch binary is built from the launch revisions (step 1).

### 2.2 CI green

- No repository has CI. The workflows were removed on 2026-10-05 and
  2026-10-06: "We build and test on our own hardware and do not use GitHub
  Actions" (sdk commit `7120b2cf`, with the matching removals in the app
  repositories and the localization store). So "CI green" means the owner's
  build pipeline and the hand-run suites below.
- The release gate of section 2.5 passes, in its local mode, on the latest
  `main` of every repository (step 4).
- The apps' v2 code was not compiled where it was written (STATE.md:174,
  219, 228). Apple and Windows have never compiled the v2 and consent code,
  and Android's unit tests ran against a stub SDK, so its AAR is built from
  SDK `main` (VERSION2-HANDOFF.md, section 3, item 5). Each app is built and
  its v2 suites pass:
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
  built from v478 (section 9.3, item 2). `main` lists 478's reviewed source
  (`crv4/native_owner_source.go:10,23`), so an `sn-mainnet` built from the
  launch revisions admits it (step 8.3).

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
| Deployer (O4) | the six v2 actions, and the escrow registration's value | limits 4,000,000, 2,500,000, 3,000,000, 1,000,000, 150,000 and 400,000; used 2,821,690, 1,463,833, 1,935,622, 310,486, 31,329 and 285,209; value `maximumBurnRao × 10^9` wei | BOOTSTRAP-CONTRACTS.md:660,707-722 |
| Governance Safe (O8) | `registerOperatorPool`'s value, which the Safe must hold before step 15's review; the surplus over the burn comes back to the Safe | R-4 sent a 100 TAO ceiling, burned 3.80 TAO and used 1,594,088 gas. Through a SafeL2 1.4.1 `execTransaction` the dry run used 1,654,438 gas, estimated at 1,797,143 (step 15) | `scripts/runtime-rehearsal/README.md` R-4; `rehearsal/stack_test.go:31-32,190-194` |
| Operator root key | every v1 and v2 close, commit, finalize and answer | the §8.1 table; a capturing close needed a limit of about 2.69M. The server signs every v1 and v2 close at max(estimate + 20%, min(3,200,000, `maximum_gas`)) | VERSION2-FINAL.md §8.1; `server/controller/st_controller.go:1135-1153,1970-2002`; `server/controller/st_ledger_v2_client.go:367-385` |
| Operator deposit key | each deposit's staging and the deposit, two stake moves with floors of 2,800,000 and 3,400,000 | at 30 gwei a pair needs 0.06 TAO under a `maximum_gas` of 1,000,000 and 0.186 TAO at the full floors; one send can need `maximum_gas` at the fee cap, 0.105 TAO at 3,500,000 and 0.108 at revision 1's 3.6M. A key short of its largest send's limit at the fee cap has the send refused, and its later sends wait | server `docs/operator/st-stranded-nonce.md`, "When the account cannot pay", and `controller/st_controller.go:2049-2062`, at server `ab207cbf9`; VERSION2-HANDOFF.md, section 3, item 12 |
| Validator auditor key (O10) | approvals, challenges and demands | approve: 132,524 used, limit 135,736; demand: 98,268 used, limit 104,424, plus the fee | §8.1 table |
| Watcher key (O21) | demands and challenges | each demand pays `demandFeeWei`, 750,000 rao, plus gas; a sample starts at 256 demands and doubles | §7.1, §9.3, §10.2 |
| Claimants: the daemon relayer, app gas keys, anyone | claims | an account's first claim uses 186,707 gas and needs a limit of 2,572,776; in a batch of 32, 147,740 gas a claim | §8.1 table; README R-7 |
| App gas key | v2 claims from the apps | the SDK signs each v2 claim at max(estimate + 25%, 2,600,000) gas, and the gas key must hold that limit × the price, or the claim fails with `needs_gas`. With legacy pricing, the node's price plus 10%, that is 0.0143 TAO at 5 gwei, so §11's 0.02 TAO holds up to about 7 gwei. EIP-1559 pricing, a fee cap of twice the base fee plus the tip, needs about 0.031 TAO. ur.io passes the SDK's limit to the wallet | §11; `sdk/sn_ledger_claim.go:74-88,318-339`; `sdk/sn_claims.go:923-961`; mmm `ur.io/react/src/app/sn/ledgerV2.js:168-173` |

- The deployer's `maximum_total_wei` covers every limit times its fee cap,
  plus the escrow funding (BOOTSTRAP-CONTRACTS.md:721-722). Each send's
  admission also needs the deployer's pending balance to cover the selected
  action and every later reservation of the same sender (:239-244).
- The operator gas policy caps every operator transaction. Today's
  `maximum_gas` of 1,000,000 is below the 3,200,000 floor of a close, and its
  per-intent liability of 5·10^16 wei is below three signatures of a close at
  1,000,000 gas and its 30 gwei fee cap. So the policy is re-signed before
  step 6's server build runs (step 5; VERSION2-FINAL.md §8.1, §13).
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
  - a batch of 32 uses 148,378 gas a claim, and a transaction holds at most
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
  - **The server** applies it on `main` to every v1 and v2 close, on a first
    attempt and on a fee replacement, and caps the floor at the operator gas
    policy's `maximum_gas`. A close is signed at max(estimate + 20%,
    min(3,200,000, `maximum_gas`)) gas
    (`server/controller/st_controller.go:1135-1153,1970-2002`;
    `server/controller/st_ledger_v2_client.go:367-385`). So a floor alone
    never makes the policy refuse a close. Under today's `maximum_gas` of
    1,000,000 a close is signed at 1,000,000, and is refused only when its
    own estimate plus 20% is above that, as before the floor. The whole
    floor applies once `maximum_gas` is at least 3,200,000 (step 5). The
    tests are SV-7 for the v2 close
    (`server/controller/st_ledger_v2_gas_test.go:81-308`) and SV-4 for the v1
    close (`server/controller/st_v1_close_gas_test.go:53-237`).
  - **The SDK** applies it on `main`: every v2 claim is signed at
    max(estimate + 25%, 2,600,000), and an external signer is given
    2,600,000 (`sdk/sn_ledger_claim.go:56-59,74-88,318-339,421`). ur.io sends
    each v2 claim with that limit and refuses claims without it (mmm
    `ur.io/react/src/app/sn/ledgerV2.js:168-173`).
  - **The SDK's v1 claims**, to the v1 vault, have a floor of their own: each
    is signed at max(estimate + 25%, 2,700,000), and an external signer is
    given 2,700,000. Below that the v1 vault accepts a claim but defers its
    payment (`sdk/sn_claims.go:140-143,857,907-921,995-996`;
    `evm/src/STSettlementVault.sol:106,455-477`). ur.io sends each v1 claim
    with that limit too, and refuses a claim without it (mmm
    `ur.io/react/src/app/screens/Claim.jsx:119-124`;
    `ur.io/react/src/app/sn/evm.js:62-69`).
  - **The apps' v1 gas top-up hint** is 0.02 TAO. The gas key must hold a
    claim's limit × the price, or the claim fails with `needs_gas`
    (`sdk/sn_claims.go:1002-1005`). At 2,700,000 gas that is about 0.0149
    TAO at 5 gwei with legacy pricing. The apps' `main` suggest 0.02 TAO
    since android `0a3a0030c`, apple `edcd545e`, linux `06717d7` and windows
    `fac2afd` (VERSION2-HANDOFF.md, section 1).
  - **sn's claim tools** sign a single claim, from `snclaim` or the claim
    daemon, at max(estimate + 20%, 2,600,000) (`miner/onchain/v2_claim.go:319`;
    `miner/onchain/eth.go:203-206`; `miner/claim_v2_daemon.go:269-287`), so a
    signed claim also passes the daemon's rebroadcast preflight after the
    list grows. A batch forwards each claim the larger of 2,600,000 and the
    batch's largest estimate (`miner/onchain/v2_batch.go:28-42,181-208`). R-3
    shows the floor on 478 (`docs/spec/runtime-478-audit.md`, "vault2's own
    staking-hotkey list").

### 2.7 Key custody

| Key | Role in this launch | Custody | Source |
| --- | --- | --- | --- |
| Deployer EVM key (O4) | sends the six v2 actions at consecutive nonces | an external EVM signer; the v1 deployer is on the owner's Ledger | BOOTSTRAP-CONTRACTS.md:652-655; LAUNCH.md:161-163 |
| Ed25519 approval key | signs the v2 phase approval | `root/subtensor/approval/`, operator-held | LAUNCH.md:187,371-378 |
| Governance Safe owner and Safe relayer (O8) | own `STSettlement2`; send `registerOperatorPool`; upgrade; pause | the owner's Ledger; the Safe is 1-of-1 | LAUNCH.md:47-48,164-166,189-190; VERSION2-FINAL.md §4.1 |
| Guardian | pauses v1 settlement, and with it v2 | the owner's Ledger | LAUNCH.md:161-163; VERSION2-FINAL.md §4.1 |
| Operator gas approver key | signs the operator gas policy | off the operator host; the server refuses any other approver for the scope | `server/st_operator_gas_policy.go:24-25`; `server/model/st_transaction_gas_model.go:386-388` |
| Treasury approval key | re-signs the treasury production approval | the Ed25519 key the validator config pins as `treasury_approval.signer` | `mainnet/treasury_approval.go:192-251`; `validator/treasury_approval_draft.go:64-66` |
| Operator root key | signs v2 closes, commits, finalizes and answers | `root_key` in `st.yml`, in the deployment vault | `server/controller/st_controller.go:251`; `server/controller/st_ledger_v2_client.go:387-393`; VERSION2-FINAL.md §7.2 |
| Validator auditor key (O10) | approvals, challenges and demands | a hex secp256k1 key file on the validator host, `--ledger_auditor_key_file` | `validator/run.go:142-143` |
| Watcher key (O21) | `snclaim v2 check --all --challenge --demand` | a host other than the validator's | VERSION2-FINAL.md §10.2 |
| Claim daemon relayer key (O22) | claims | the daemon config's `key_file` | `miner/claim_v2_daemon.go:61` |
| SN25 owner, `ur-owner` | holds SN25's stake-transfer switch, which can freeze every claim | 2-of-3 native multisig | LAUNCH.md:186,199-205,224-237; VERSION2-FINAL.md §4.1, §17 |

The deployer, guardian, Safe relayer and Safe owner keys are all on the
owner's single Ledger. The owner's availability is therefore a dependency of
steps 5, 8, 10 and 15 (VERSION2-FINAL.md §13, §17).

## 3. Owner inputs

| ID | Input | Constraint or default | Used in | Source |
| --- | --- | --- | --- | --- |
| O1 | The launch revisions: the `main` head of every release repository, and gvisor's `go` head, on which step 4's gate passes in its local mode | the latest `main` of each, pulled; a failing gate is fixed, merged and pulled, and runs again until it passes (O28). gvisor's `go` head is `c0783dba2eeff7b79692a7f122ffee5f8bd837c1`, the commit that `server/local/source-graph/lock.yml`:37-39 names. The dual-stack validator hops are on `main`, so the launch carries them (section 6.3) | 1, 4 | the preamble; section 2.1; `scripts/mainnet-release-build/PIN-PROCEDURE.md`:143-144 |
| O2 | The mainnet route: native and EVM RPC URLs, the HTTPS leaf SPKI pin if HTTPS, and the approved native chain name | the launch used Snow's Finney archive, `ws://172.28.208.185:9944`, or `http://` for EVM JSON-RPC. Every `sn-mainnet` command here takes only the `http(s)://` form; step 2 lists the scheme each command accepts. The route is part of each signed plan | 2, 8, 13, 15, 20 | LAUNCH.md:124-141; BOOTSTRAP-CONTRACTS.md:208-221; `mainnet/identity.go:74-79` |
| O3 | The runtime identity the v2 phase config binds: version, code and metadata hashes and the reviewed source commit | for runtime 478: its version, code hash and metadata hash from the owner's own `runtime-snapshot`, taken immediately before step 8, and the reviewed 478 source commit `850f2755bb05a3851f948489f0ab27b0645cebce`. The binary must list that commit beside that version; `main` lists 470, 473, 475, 477 and 478 (step 8.3) | 8 | BOOTSTRAP-CONTRACTS.md:208-210,669-679; `crv4/native_owner_source.go:6-24`; `mainnet/evm_phase.go:183-191` |
| O4 | The v2 deployer: its EVM address, next nonce and signer | neither the Safe nor an auditor; funded per section 2.6 | 8 | BOOTSTRAP-CONTRACTS.md:652-655,752-753 |
| O5 | The escrow hotkey | 32 bytes, nonzero, with no UID on SN25 at the send; not the pool hotkey | 8 | BOOTSTRAP-CONTRACTS.md:730-737, and "Back-to-back escrow registration (A7)" |
| O6 | The escrow registration cap `maximumBurnRao` | nonzero; the value is the cap × 10^9 wei | 8 | BOOTSTRAP-CONTRACTS.md:660,743-745 |
| O7 | The phase envelope: each action's gas limit and fees, `maximum_total_wei`, `maximum_attempts`, the native start checkpoint and send window, the source-lock, cutover and custody-fence evidence hashes, the custody identifier and the run directory | gas starts from BOOTSTRAP-CONTRACTS.md:715-718; `maximum_attempts` at most 8 across all actions, the back-to-back escrow send included; the start checkpoint taken just before the approval is signed, and the window at most 7,200 blocks (step 8.3). The three evidence pins are only format-checked: the binary signs them into the plan, and nothing compares them with a file. The fill recipe suggests the frozen checkout's `sn-mainnet source-lock` output for the source lock and step 8.2's fence file for the custody fence; it names none for the cutover evidence. Nothing is frozen: by the owner's decision of 2026-10-14 the gate runs in its local mode against the `main` heads, with no freeze and no release-lock refresh (the preamble; step 4), so the recipe's frozen checkout is sn's O1 checkout | 8 | BOOTSTRAP-CONTRACTS.md:208-213,683-687; `mainnet/evm_phase.go:59-62,180`; `mainnet/SETTLEMENT-V2-FILL.md`, section 2 |
| O8 | The `STSettlement2` owner and its signers | the governance Safe `0x56F4Dad575576CC0B679FEf52899630f9F605418`, 1-of-1, owner `0x16C372dbBb24cd8473345ab13971E40814C8658F` at the v1 launch; confirm | 8, 15 | LAUNCH.md:47-48,164-166; VERSION2-FINAL.md §7.2 |
| O9 | The v1 coordinator for `initialize` | the proxy `0xC18925925E2B7bb9059b7d696b8c92762AE86406`; confirm | 8 | LAUNCH.md:42 |
| O10 | The auditor key and its EVM address | one address, quorum 1 at launch; the same key runs with `--ledger_auditor_key_file` | 8, 10 | BOOTSTRAP-CONTRACTS.md:750-754; VERSION2-FINAL.md §2.1 decision 2 |
| O11 | Whether to create the claim batcher | optional sixth action | 8, 13 | VERSION2-FINAL.md §8.5 |
| O12 | The demand fee | decided: 750,000 rao, 750,000,000,000,000 wei (owner decision, 2026-10-12); the command has no default | 8 | VERSION2-FINAL.md §7.1; BOOTSTRAP-CONTRACTS.md:734-739 |
| O13 | The re-signed operator gas policy: `revision`, `previous_policy_sha256`, `maximum_gas`, `maximum_intent_liability_wei`, `maximum_lifetime_liability_wei`, the validity window and the attempt caps | `maximum_gas` about 3.5M, and at least 3,200,000, the floor of every v1 and v2 close; intent liability at least 3 × `maximum_gas` × `maximum_fee_per_gas_wei`, 3.15·10^17 wei at 3.5M and 30 gwei; `maximum_lifetime_liability_wei` at least 2·10^19 once deposits are priced; `maximum_intent_attempts` at most 3; same bindings (step 5) | 5 | VERSION2-FINAL.md §8.1, §13; `server/local/sn25golive/gas.go:26-28`; `server/model/st_transaction_gas_model.go:95-112,332-368` |
| O14 | The treasury re-sign inputs: the input file and the unsigned policy plan the live approval was drafted from, with their SHA-256, and the approval key's signature | unchanged; only `config_hash` may change. The written sequence is [mainnet/SETTLEMENT-V2-ADOPTION.md](mainnet/SETTLEMENT-V2-ADOPTION.md) | 10 | `mainnet/treasury_approval_test.go:534-557`; `validator/production_authority_history.go:368-388` |
| O15 | The adopted validator config: its absolute path, the predecessor's retained authority bundle, the progress file, the durable-volume declaration, and how the service command line gains `--ledger_auditor_key_file` | all paths absolute; on snow the flag comes from xops `group_vars/validator.yml`, which renders it into `sn-validator.service`'s `ExecStart`; for a unit this repository renders, through the systemd drop-in [mainnet/validator-ledger-auditor.example.conf](mainnet/validator-ledger-auditor.example.conf) (step 10) | 10 | `validator/treasury_approval_draft.go:71-77`; `validator/run.go:112-117`; SETTLEMENT-V2-EXAMPLES.md:102-123 |
| O16 | The v2 pool hotkey (`poolHotkeyV2`) | new; nonzero; not the escrow hotkey; not used by another pool | 15 | `evm/src/STSettlementVault2.sol:166-171`; VERSION2-FINAL.md §12.1 |
| O17 | The pool registration cap `maximumBurnRao` and the value sent | the rehearsal sent the cap × 10^9 wei at the runtime's default MaxBurn of 100 TAO | 15 | `rehearsal/stack_test.go:31-32,190-194` |
| O18 | E_s and the registration date | decided: E_s is epoch 3, which starts at block 9,348,028, about 2026-10-23 18:10 UTC. The owner first chose epoch 2 (2026-10-13: "we are launching v2 now to be active the next ur epoch") and then moved E_s to epoch 3. The registration window is blocks 9,333,628 to 9,340,828, and every gate passes by block 9,333,628. Section 9 gives the schedule | 14, 15 | section 9; VERSION2-FINAL.md §12.1 step 5, §14 |
| O19 | `v2_prune_alert_positions` | default 3 | 11 | `server/controller/st_ledger_v2_prune.go:33-35` |
| O20 | `--ledger_max_bytes` | default 2147483648 (2 GiB); a runtime flag, raised without a re-sign | 10 | `validator/run.go:144-145`; VERSION2-FINAL.md §6.6 |
| O21 | The watcher: host, key, RPC and fee budget, and how often it runs | a host other than the validator's; the example units run it hourly as the system user `snclaim-watcher` | 13 | VERSION2-FINAL.md §10.2; SETTLEMENT-V2-EXAMPLES.md:143-161 |
| O22 | The claim daemon: coldkeys, relayer key, state directory, threshold and batcher | threshold default 1,900,000,000 rao (1.9 α), never below 15,200,000 rao; the state directory is a durable root of its own, prepared as `provider-claim-queue` (step 13) | 13, 20 | `miner/run.go:197-199`; `miner/claim_v2_daemon.go:39-112`; SETTLEMENT-V2-EXAMPLES.md:125-141 |
| O23 | The SDK release that pins `Vault2Address`, and the app release dates | lands before step 15 | 9 | VERSION2-FINAL.md §11; `sdk/sn_chain.go:83-89` |
| O24 | Who runs the first claim, and for which account | anyone may claim for anyone; payment goes to the leaf's coldkey | 20 | VERSION2-FINAL.md §1, §17 |
| O25 | Whether and how to hold the SN25 owner key `ur-owner` under the Safe's governance | recommended; no coldkey swap; no tooling | section 8 | VERSION2-FINAL.md §4.1, §17 |
| O26 | Gate 1's independent review: confirmation that it is resolved | the owner confirmed it resolved, 2026-10-13 | 4 | VERSION2-FINAL.md §14; STATE.md:91,99,103,176,189,198 |
| O27 | The native-fee denomination policy for runtime 478: its `native_authority`, `last_native_block`, wei-per-rao ratio and signature, and its authority file | `runtime_code_sha256` 478's and `first_native_block` 9,276,926; once a settlement has recorded an approver key for the scope, only that key can sign (step 5) | 5 | `server/st_native_fee_denomination.go:27-54`; `server/model/st_native_fee_settlement_model.go:124-125,189-197` |
| O28 | How the release gate's failures are handled | decided: none is accepted. Each is fixed, committed, merged, pushed and pulled, and the gate runs again until it passes (owner decision, 2026-10-14) | 1, 4 | the preamble; VERSION2-HANDOFF.md, section 2 |
| O29 | Whether step 6's taskworker and api may carry commits the gate did not run | decided: yes, narrowly. They build from run 5's gated heads plus exactly four reviewed FP2 routing fixes: in the taskworker, native evidence that no longer expires between exports and stale city targets that stop serving departed providers; in the api, the no-live-connection exclusion with Quality borrowing only within Quality's cutoffs, and fixed-IP named redials that skip only the Quality ARIN check. Each comes in by `cherry-pick -x`, or by merge when it is based on an ancestor of run 5's server head, with no rebase and nothing else from `main`. Their commits are recorded here once merged. Extended the same day by the owner ("yes include arin. We need to get the arin changes out"): the ARIN dual-database canary (`feat/arindb-dual-canary` `a9459ec31` with its reviewed conflict resolution) rides step 6's api and taskworker and the step-6 connect build, after migrations 837 and 838 are applied, with its paired config release right after step 6. Extended again ("yes deploy sooner"): the reviewed upload-admission fix (`fix/upload-admission-silent-drop`, after the api's 20:04-20:40Z silent refusals) ships first in an api hotfix with `blocks_per_range` 10000 and at least 120 s between `maximum_refresh_seconds` and `maximum_head_age_seconds`, and rides step 6's sn head so step 6 does not roll it back. A departure from O1 and O28, accepted by the owner, 2026-10-15 | 6 | O1; O28 |

## 4. Runbook

The order is VERSION2-FINAL.md §12.1's. Each sub-step sits where its inputs
first exist.

| Step | What | Who | Gate |
| --- | --- | --- | --- |
| 1 | Fix the launch revisions and build | Owner, operator | |
| 2 | Check the mainnet runtime | Operator | |
| 3 | Rerun R-1 to R-7 | Operator | 2 |
| 4 | Run the release gates | Operator, owner | 1, 3, 4 |
| 5 | Re-sign the operator gas and native-fee policies | Owner, operator | 6 |
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

**Step 1. Fix the launch revisions and build.**

- **Who.** Owner and operator.
- **Do.**
  1. Check out the latest `main` of every release repository beside sn, and
     gVisor's `go` (step 4, "The workspace"). The session that orchestrates
     the launch runs the gate on goofe from worktrees at those heads
     (VERSION2-HANDOFF.md, section 3, item 7). Confirm section 2.1,
     including the localization store's `main`.

     gVisor's checkout is `gvisor`, cloned from
     `https://github.com/urnetwork/gvisor` on its branch `go`. Its head is
     `c0783dba2eeff7b79692a7f122ffee5f8bd837c1`, the commit that
     `server/local/source-graph/lock.yml`:37-39 names
     (`scripts/mainnet-release-build/PIN-PROCEDURE.md`:143-144). With
     `WORKSPACE` the directory that holds the sn checkout:

     ```bash
     git clone --branch go https://github.com/urnetwork/gvisor "$WORKSPACE/gvisor"
     git -C "$WORKSPACE/gvisor" rev-parse HEAD
     ```

  2. Run step 4's gate on those checkouts. If it fails, fix, commit, merge,
     push and pull, and run it again, until it passes (O28). The heads it
     passes on are O1.
  3. Build `sn-mainnet` with Go 1.27.2. The pin is the `go` line of `go.mod`
     (`go.mod:3`, `go 1.27.2`; the server's `go.mod:3` is the same). Under
     `GOTOOLCHAIN=local` an older local toolchain is refused: "go: go.mod
     requires go >= 1.27.2 (running go …; GOTOOLCHAIN=local)". The command is
     [mainnet/MACOS.md](mainnet/MACOS.md):24-29's:

     ```bash
     GOTOOLCHAIN=local CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=true \
       -o /absolute/path/sn-mainnet ./mainnet
     ```

  4. Build the production composition, which holds `cli/validator`,
     `cli/snclaim`, `cli/miner` and the server services
     ([mainnet/RELEASE-BUILD.md](mainnet/RELEASE-BUILD.md):3-8,37-42), in the
     local mode:

     ```bash
     go run -mod=readonly ./scripts/mainnet-release-build --config /absolute/config.json
     ```

     - The launch builds in the local mode, from the gated checkouts of sn,
       server, connect, sdk, gvisor and the other census repositories, each
       clean at its O1 commit. sn's and the server's `go.mod` resolve
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
  5. Retain each binary's SHA-256 and `go version -m` output (LAUNCH.md:336-337).
- **Expect.** Every binary's build information names its O1 commit.
- **Stop if** a repository's `main` lacks its pin, the store's `main` lacks
  the `sn_ledger_*` keys, or a binary's build information names a commit
  other than O1's.
- **For step 8.**
  - sn's launch revision must list the reviewed source of the runtime that
    step 8 deploys on (step 8.3). That runtime is 478 (step 2), and `main`
    lists its source (`crv4/native_owner_source.go:10,23`). A runtime after
    478 first needs its own reviewed source on `main`, and so new launch
    revisions.
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
  ([mainnet/MAINNET.md](mainnet/MAINNET.md):1663).
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
    dispatch path that changes the gas limit they need"
    (`docs/spec/runtime-477-audit.md`:377-385). vault2 depends on both for
    good (section 2.3).
  - **477 ran before it,** from block 9,267,632 through 9,276,924,
    byte-identical to the official v477 release asset
    (`docs/spec/runtime-477-audit.md`:29-46,391-394). Block 9,276,926 is the
    first block that 478 executed (`docs/spec/runtime-478-audit.md`, "Exact
    provenance").
  - A runtime can land soon after it is proposed. 478 was proposed at block
    9,276,607 and installed 318 blocks later
    (`docs/spec/runtime-477-audit.md`:377-383). 475's proposal
    executed 196 blocks after it was made
    (`docs/spec/runtime-475-audit.md`:142-144), and 477's 26,475 blocks after
    (`docs/spec/runtime-477-audit.md`:137-140).
- **Repeat** this step immediately before step 8 and immediately before
  step 15. A runtime can land between any two steps. A new runtime also
  needs its own native-fee denomination policy before its operator fees can
  settle (step 5, O27).
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
  (`scripts/runtime-rehearsal/README.md`:16-19,117,235-238;
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
    (README:21-48).
  - Every test passes. On 475, R-2 to R-6 took 8 minutes 22 seconds, R-7
    about 6 and R-1 80 to 100 seconds (README:240-242; BOOTSTRAP-CONTRACTS.md,
    "Runtime rehearsal (R-1)"). On 477 the `rehearsal` package took 776
    seconds on a loaded host, and R-1 120 seconds (README:442-446). On 478
    the `rehearsal` package took about 560 seconds, and R-1 124
    (README:520-523).
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
  (README:117-121).

**Step 4. Run the release gates.**

- **Who.** The session that orchestrates the launch runs the gate on goofe
  (VERSION2-HANDOFF.md, section 3, item 7). The owner decided that its
  failures are fixed, not accepted (O28).
- **Do.** In a workspace with the checkouts of step 1, with
  `SN_TEST_STORAGE_ROOT` set or `/mnt/data` mounted
  (`scripts/test-storage.sh`:6-17), run the gate in its local mode:

  ```bash
  RUN_SERVER_DB_TESTS=1 RELEASE_GATE_DEPENDENCY_MODE=local RELEASE_GATE_CONCURRENT_GATES=1 \
    ./scripts/test-release-1.0-local.sh
  ```

  - `RELEASE_GATE_DEPENDENCY_MODE=local` tests the working checkouts as they
    are (`scripts/test-release-1.0-local.sh`:4-18; `sim-testnet/README.md`:839-846).
  - `RUN_SERVER_DB_TESTS=1` runs the database suites, the scale shard's
    database half and the E suite, on the gate's private disposable
    PostgreSQL and Redis (`scripts/test-release-1.0-local.sh`:894-1101).
  - `RELEASE_GATE_CONCURRENT_GATES=1` gives this one gate the host's whole
    CPU and memory share, where the default reserves half of it for a second
    gate (`scripts/release-gate-jobs.sh`:34-36,99-101).

  Run the app suites of section 2.2 beside it.
- **What local mode skips.** Exactly the checks that seal a run
  (`sim-testnet/README.md`:848-856), each printed with its reason before
  anything runs (`scripts/test-release-1.0-local.sh`:16-24):
  - the source freeze's provenance checks: clean, origin, upstream, fetch and
    at-origin at the start, and the final snapshot with its comparison
    (`scripts/check-release-source-freeze.sh`:203-209,238-243);
  - `TestReleaseLockMatchesCheckout`, by name in every job that would run
    it, and as the final fence
    (`scripts/test-release-1.0-local.sh`:173-183,1144-1153). So the release
    lock needs no refresh: the owner's decision of 2026-10-14 is that the
    gate runs in this mode against the `main` heads, with no freeze and no
    lock refresh (the preamble);
  - `TestRepositoryDiscoveryUsesModuleIdentity`, by name in the same jobs,
    because it stats the workspace vault's `main/st.yml`
    (`sim-testnet/config_test.go:608-618`);
  - the runtime 454 source and runtime-metadata attestations, which read
    GitHub and the testnet archive node rather than the checkouts
    (`scripts/test-release-1.0-local.sh`:81-91).

  It keeps the module inventory and tidy checks, the sourceguard and
  binding-toolchain preflights, patch hygiene, every test job and the
  diagnostic mode. `sim-testnet/release_gate_local_skips_test.go` holds the
  printed skips and the named tests to this list (:3-5).
- **It never touches a vault.** The source freeze and patch hygiene skip the
  vault checkout, its Git metadata included
  (`scripts/check-release-source-freeze.sh`:132-145;
  `scripts/test-release-1.0-local.sh`:1134-1142), and no test that would
  read it runs (above). The two named skips reach every job since sn
  `v2/gate-mainnet-shards` merged (`a47111d6`). Before that,
  `TestReleaseLockMatchesCheckout` still ran in `sn-simulator-race`, and
  `TestRepositoryDiscoveryUsesModuleIdentity` failed without the vault:
  "cannot discover vault repository; use --vault-repo"
  (`sim-testnet/config.go:1440-1458`).
- **What it prints.** Its mode first: "[release-1.0] dependency mode local:
  the working checkouts as they are, not sealed", and then each skip with
  its reason (`scripts/test-release-1.0-local.sh`:16-24). Then "source
  freeze: local mode checks the module inventory and tidiness of the working
  checkouts, not their provenance"
  (`scripts/check-release-source-freeze.sh`:208), and "[release-1.0] runtime
  source and metadata artifact attestations skipped (local mode)" and
  "[release-1.0] final release-lock and source-freeze checkouts skipped
  (local mode)" (`scripts/test-release-1.0-local.sh`:84,1145).
- **The workspace:** sn, server, connect, sdk, glog, goidenticons, proxy,
  userwireguard, warp, gvisor, xops and config, each a Git checkout beside
  sn (`scripts/test-release-1.0-local.sh`:49;
  `scripts/check-release-source-freeze.sh`:132-145). Local mode needs no
  vault (above). The `xops` job runs that repository's infrastructure tests
  (`scripts/test-release-1.0-local.sh`:1103-1114).
- **The generated files at mode 644**: the fifteen ABI and binding files of
  step 8.1. Check public sources out under umask 022, and keep the server's
  `local/postgres/initdb` directory and its script at 755
  (`sim-testnet/README.md`:1378-1383).
- **The pinned mode**, the default, seals a run and stays for the build
  pipeline; the launch does not use it
  (`scripts/test-release-1.0-local.sh`:4-12).
- **The jobs.** The long packages run in hash shards, each shard a job that
  runs the tests whose names hash to it
  (`scripts/test-release-1.0-local.sh`:206-333): `./mainnet` in eight,
  `sn-mainnet-1` to `sn-mainnet-8`; `./validator` in 2 normal and 12 race;
  `./sim-testnet` in 5 normal and 17 race; and connect's race run in 15,
  `connect-race-1` to `connect-race-15`, beside 15 seeded-shuffle race runs
  (:484-533,811-840). `sn-mainnet-exports` builds the `./mainnet`
  qualification exports offline from the working checkouts, and every
  `./mainnet` shard waits for it: a failed build fails every shard
  (:104-113,484-496).
- **Repaired on `main`**, so these no longer fail:
  - the source freeze's module classification and tidiness (sn
    `v2/gate-preflight`);
  - the 19 stale census pins (sn `v2/release-census`; the native-stake
    startup pin follows the current code, by the owner's decision,
    `f26f88f4`);
  - the `solidity-coverage` job: a test reads the height with
    `vm.getBlockNumber` after `vm.roll` (`2911329b`);
  - the `solidity-static` job: Slither's triage database
    `evm/slither.db.json` accepts its one locked-ether finding, in
    `STSubnetProbe`, by id (`45916933`; `scripts/test-solidity-static.sh`);
  - the server suites' test vault guard, which accepts a vault below
    `GOTMPDIR` (server `07cc843e5`);
  - the gate's selection (sn `f3cc4492`): its `server-unit` job runs
    `go test ./local/sn25golive`, and its database suites select SV-4's v1
    close floor tests beside SV-7's
    (`scripts/test-release-1.0-local.sh`:633,968-978);
  - the local mode itself (sn `53491e14`), and the further repairs that
    VERSION2-HANDOFF.md, section 1, lists. Among them sn `692022ac` fixes
    the three `TestProduceFinalSemanticOutputs*` tests;
  - the job runner (sn `819f00c5`): a nested fixture gate whose initializer
    refused removed the running gate's exports, which failed every
    `./mainnet` shard of the second official run's first attempt. The
    initializer now sets its own exports path before its exit trap, and
    only the gate that made the exports removes them
    (`scripts/release-gate-jobs.sh`:70-83,377-384);
  - the H3 and alt admission tests of server `0d98bde5b`, which
    `server-connect` and `server-db` run in normal and race mode (sn
    `58faf413`; `scripts/test-release-1.0-local.sh`:878-887,955-965);
  - the second to fourth official runs' failures: the supervisor test's
    leak, the receipt test, the SMTP 587 test, the isolation pin's TUN
    subnets and the job runner's held fixture; and the pre-check's
    taskworker race and proxy test isolation (VERSION2-HANDOFF.md, section
    1);
  - the HF-1 replay tests, which the `server-unit` job's `./model` selector
    never matched (sn `e5908acf`; :652), and `server-db`'s selection of the
    deposit key's fix's tests (sn `ca5e318e`; :968-978).
- **Status.** Four official runs ended without a pass; the fifth is in
  progress (VERSION2-HANDOFF.md, section 3, item 7). The second, from
  2026-10-15 02:14 UTC on sn `819f00c5`, ended with 92 jobs passed, 4 failed
  and 2 stopped, and each failure is fixed. The third failed on a drift
  between sn's server fixture and a server test, fixed by server
  `1502e131`; the fourth on an intermittent job runner test, since fixed by
  sn `64664738`, and it was stopped at 18:17:16 UTC. The fifth started at
  18:47:36 UTC on sn `64664738`, server `e22d50d09`, connect `23849763` and
  sdk `1b92af87`. A pass qualifies the heads recorded at its run's start,
  and several sessions merge into `main` every hour. The server-db job's
  `./proxy` can fail on a pre-existing stall (VERSION2-HANDOFF.md, section
  5).
- **Expect.** The last line is `[release-1.0] local release gate passed
  (local mode: working checkouts, not sealed)`
  (`scripts/test-release-1.0-local.sh`:1179-1180). It qualifies those
  checkouts, not a sealed release (`sim-testnet/README.md`:856-858). The
  settlement v2 jobs:
  - `sn-v2`, gate 3's own verdict (`scripts/test-release-1.0-local.sh`:535-553);
  - `sn-scale` and `server-scale`, GL-10 and SV-6 (:555-573,1032-1050);
  - `sn-e2e`, gate 4 (:1051-1080);
  - `solidity`: format, release build with sizes, tests, the `v2_invariant`
    campaign, and the catalog and binding checks (:581-605);
  - `solidity-coverage`, gate 1's bar (:607-622), and `solidity-static`
    (:575-579);
  - `sdk`, CL-2 and CL-4 (:854-859), and the server's v2 suites, the
    activation record's (SV-9) and the v1 close floor's (SV-4) among them,
    with migrations 821, 822 and 832 (:968-984).
  - The wiring tests `TestReleaseGateSettlementV2*` of
    `sim-testnet/release_gate_settlement_v2_test.go` run in the
    `./sim-testnet` shards.
- **Stop if** the script prints `[release-1.0] DB suites deferred`
  (`scripts/test-release-1.0-local.sh`:1100): the database suites, the scale
  shard's database half and the E suite then did not run. Any failed job is
  not a stop: fix it, commit, merge, push and pull, and run the gate again
  (O28).

### Phase B. Release, approve and deploy

**Step 5. Re-sign the operator gas policy and the native-fee policy.**

**Done** since 2026-10-14 19:20 UTC. Operator gas policy revision 1,
`policy_sha256`
`1ef468cea2810f72eff1e04f63298eccbe13080fb4339627c148f67c149cc073`, is
installed: the config authority (config `f647123`) and the vault's st.yml,
with api, alt and the taskworker restarted. The native-fee policy has
nothing to re-sign (below). VERSION2-HANDOFF.md, section 3, items 2 and 3,
has the detail.

*The operator gas policy.* It binds the v1 coordinator, the policy hash and
the two accounts, not call targets
(`server/controller/st_transaction_gas_policy.go:97-104`). So it does not
depend on the deployment. Install it before step 6's server build runs
(below).

Server `main` signs every v1 and v2 close with at least
min(3,200,000, `maximum_gas`) gas, the rule of section 2.6
(`server/controller/st_controller.go:1144-1153`). The policy checks two
limits, in `server/model/st_transaction_gas_model.go`:

- **Each signature.** A transaction's gas must not exceed `maximum_gas`, and
  its gas × fee cap must not exceed `maximum_intent_liability_wei`
  (`server/model/st_transaction_gas_model.go:95-112`). The floor is capped at
  `maximum_gas`, so it alone never fails this check. Under today's 1,000,000
  a close is signed at 1,000,000 gas, and only a close whose own estimate
  plus 20% is above that is refused, before any signature, as before the
  floor. It keeps no nonce (`server/controller/st_ledger_v2_gas_test.go:184-246`;
  `server/controller/st_v1_close_gas_test.go:134-198`). The whole floor
  needs a `maximum_gas` of at least 3,200,000.
- **Each operation.** The liability is summed over every nonce one logical
  operation takes, across its retries, and each operation is allowed at most
  `maximum_intent_attempts` signatures
  (`server/model/st_transaction_gas_model.go:329-368`). A try the policy
  refuses takes no nonce, so the root key's other sends go on (section 7).
  But the root key cannot send that operation again: a per-intent liability
  that covers one floored close leaves a reverted close no second signature
  (`server/controller/st_ledger_v2_gas_test.go:450-481`). So
  `maximum_intent_liability_wei` is at least 3 × `maximum_gas` ×
  `maximum_fee_per_gas_wei`. That never binds before the cap of three
  signatures.
  - Today's policy does not meet this: 3 × 1,000,000 × 30 gwei is 9·10^16
    wei, above its 5·10^16.
  - Under step 6's build a close is signed at 1,000,000 gas. A second
    signature of the same close then fits only while the signed fee cap is
    at most 25 gwei, and a third only while it is at most about 16.7 gwei.
  - So install the re-signed policy before step 6's build runs. The next
    close is v1's of epoch 1, whose window opens at block 9,297,628
    (section 9.1).
- **A refusal at the floor** names it: "st: gas limit <n> is the v1 close's
  stake-move floor (operator gas policy maximum_gas <m>): …", or "the v2
  close's" (`server/controller/st_controller.go:1189-1196`).

`maximum_gas` therefore stays at about 3.5M (O13), and the per-intent
liability is at least 3.15·10^17 wei at 3.5M and 30 gwei.

- **Who.** Owner signs with the approver key; operator installs.
- **The tool.** `sn25golive` is the server's `local/sn25golive` command.
  Build it in the server checkout, with the sibling checkouts of step 1
  beside it (`server/local/sn25golive/README.md`:17-31):

  ```bash
  go build -o sn25golive ./local/sn25golive
  go test ./local/sn25golive
  ```

  A refused command prints `sn25golive <command>: <reason>` to stderr and
  exits 1 (README:33-38).
- **Do.**
  1. Copy the launch request `server/local/sn25golive/sn25-mainnet-gas-params.json`
     to a new params file. Change only:
     - `revision`: the live revision plus 1;
     - `previous_policy_sha256`: the live policy's digest. The launch request's
       digest is `e2de5acf1ef30de0b5c00f0e9cb5291db1b56b5fe6d0a17a524bb65806b5d64d`
       (`server/local/sn25golive/main_test.go:26-28`), and LAUNCH.md:73-75
       names the installed one `e2de5acf…`. Confirm it against `st.yml`;
     - `maximum_gas`: about 3,500,000, at least the 3,200,000 floor of a
       close, with room for a few deferred epochs (§13);
     - `maximum_intent_liability_wei`: at least 3 × `maximum_gas` ×
       `maximum_fee_per_gas_wei`, 3.15·10^17 wei at 3.5M and 30 gwei;
     - `maximum_lifetime_liability_wei`, the validity window and the attempt
       caps: checked against v2 traffic (O13; §13). Until the native-fee
       policy below settles them, every send counts at its full ceiling.
       Once deposits are priced, the lifetime liability is at least 2·10^19
       wei: a year of floored closes, stagings and deposits at 30 gwei comes
       to about 1.5·10^19 (VERSION2-HANDOFF.md, section 3, item 2).

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

*The native-fee denomination policy (O27).* The server charges an operator
transaction its signed ceiling, gas × fee cap, against the operator gas
policy's intent and lifetime liabilities. Only a native-fee settlement replaces that ceiling
with the fee actually paid ("a finalized nonce is charged its actual fee
once while every unresolved nonce keeps its maximum candidate ceiling",
`server/model/st_native_fee_settlement_model.go:1-3`;
`server/model/st_transaction_gas_model.go:329-331`). `stnativefees settle`
makes one, under a signed denomination policy
(`server/cli/stnativefees/README.md`:1-24).

- The policy binds one runtime code and one native block interval. A fee
  executed outside them is refused: "native fee original execution is
  outside its approved denomination"
  (`server/model/st_native_fee_settlement_model.go:124-125`). One signed for
  477's code settles nothing that 478 executed, from block 9,276,926
  (`docs/spec/runtime-478-audit.md`, "Hard-coded figures" and "Exact
  provenance"). None was ever installed: native-fee settlement was never
  provisioned on mainnet, so there is nothing to re-sign for 478, and the
  rest of this sub-step does not apply to this launch (VERSION2-HANDOFF.md,
  section 3, item 3).
- **Who.** Owner signs with the denomination approver key; operator
  installs.
- **Do.**
  1. Write the policy, schema `urnetwork-native-fee-denomination-v1`, with
     the fields of `server/st_native_fee_denomination.go:27-42`:
     - `profile` `mainnet`, `chain_id` 964, `genesis_hash` and `no_id` 1, the
       operator deployment's (`server/controller/st_native_fee_settlement.go:90-92`);
     - `native_authority`: the verifier, the approved `sn-mainnet` ELF's
       path and pin, and its native policy: the approval key, genesis, EVM
       chain, and the engine, checkpoint, runtime semantics review and
       callsite profile pins (sn `nativefee/verified.go:54-70`);
     - `runtime_code_sha256`: `sha256:` and the SHA-256 of 478's `:code`,
       `1d64a44f3607eb431ef8bbd909dd35dfcbcf9cc9168d9bf74923c198d3619ac6`
       (`docs/spec/runtime-478-audit.md`, "Exact provenance"); the replay
       hashes the `:code` bytes with SHA-256
       (`mainnet/economic_native_execution.go:655-659`);
     - `first_native_block` 9,276,926, and `last_native_block`, at most
       4,294,967,295;
     - `native_unit` `tao-rao`, `budget_unit` `evm-wei`, and the reduced
       ratio `wei_per_rao_numerator` over `wei_per_rao_denominator`. Neither
       has a default (`server/st_native_fee_denomination.go:1-2,97-122`);
     - `signature`: the approver's Ed25519 signature, in hex, of the schema
       line and the policy's JSON without its signature (:124-136).
  2. Write the authority, schema
     `urnetwork-native-fee-denomination-authority-v1`: the same `profile`,
     `chain_id`, `genesis_hash` and `no_id`, the `approver_public_key`, and
     `policy_sha256`, the SHA-256 of those signing bytes (:44-54,138-146).
  3. Install them as the Config resources
     `native-fee-denomination-policy.yml` and
     `native-fee-denomination-authority.yml`, beside
     `operator-gas-authority.yml`
     (`server/controller/st_native_fee_settlement.go:40-55,74-88`).
- **Expect.** `stnativefees settle` for an operator transaction of block
  9,276,926 or later no longer refuses it as outside its denomination.
- **Stop if** a settlement refuses with "native fee denomination cannot
  replace its original approval key", "native fee denomination differs from
  independent authority", "native fee denomination signature or exact
  independent pin is invalid", or "native fee denomination differs from the
  configured operator deployment"
  (`server/model/st_native_fee_settlement_model.go:196`;
  `server/st_native_fee_denomination.go:155-163`;
  `server/controller/st_native_fee_settlement.go:90-92`).
- **Gap.** No repository tool writes or signs this policy, and "these source
  changes and their deterministic Go fixtures remain unexecuted"
  (`server/cli/stnativefees/README.md`:17-28). Whether 477's native policy,
  inside `native_authority`, serves 478 unchanged is not settled.

**Step 6. Ship the server v2 build with v2 off.**

- **Who.** Operator.
- **Do.**
  1. Apply the migrations first. Migration 821 says "Apply before deploying a
     server with v2 addresses" (`server/db_migrations.go:10013-10025`).
     v2's migrations are 821, 822 and 832. Upstream added 823 to 831 on
     server `main` (:10051-10168). 832 is the activation record's table,
     `st_ledger_v2_activation`: "A new table: nothing is rewritten. Apply
     before deploying a server with v2 addresses" (:10169-10187). Upstream
     added 833 to 835 after it (:10188-10381). The same command applies them
     all:

     ```bash
     bringyourctl db migrate
     bringyourctl db version
     ```

  2. Deploy the step 1 build of every server service, in the dual-stack
     order: the API and the taskworker first, then the connect service
     (section 6.3). The ones that read st.yml or sn.yml for v2 are api, alt
     (which mounts the same router), taskworker and `bringyourctl`
     (`server/api/run.go:274-293`; `server/alt/run.go:178`;
     `server/taskworker/workload_profile.go:95-96`).
     - gossip, mcp and proxy, also server builds, link the same sn.yml
       parser, `server.LoadProviderPayoutTransition`, which decodes with
       known fields only (`server/provider_payout_transition.go:57-58`). A
       build of any of them before server `9c14471a`, which added the v2
       fields, refuses sn.yml once it carries them: "sn.yml: yaml: unmarshal
       errors: … field settlement_v2_address not found in type
       server.ProviderPayoutMainnet". So deploy their step 1 build in this
       step too: every server service that reads sn.yml runs a build that
       knows the v2 fields before step 11.
  3. Leave st.yml and sn.yml without v2 keys. The binary goes before the
     config, because sn.yml is parsed with known fields only (§13;
     `server/provider_payout_transition.go:57-58`).
- **Expect.**
  - The database head is at least the binary's own migration count, and
    readiness passes. That count is 835 on server `main` (`72699c819`), so
    the head is 835 or higher at the launch revisions. A binary stays not-ready while
    the head is below its count: "database migration head %d is below
    binary-required head %d" (`server/router/warp_handlers.go:209-210,255-260`).
    The required head is `MinimumRuntimeMigrationVersion`, which returns
    `MigrationCount`, the number of migrations
    (`server/db_runtime_migration.go:8-15`; `server/db_migrations.go:160-165`).
    `bringyourctl db version` prints
    "Current DB version: %d" (`server/bringyourctl/main.go:490-493`).
  - v1 settlement runs unchanged (section 1.2, state 1).
  - `bringyourctl st v2-activation` prints one JSON object with the
    deployment key and `"armed":false`. Without the v2 pair the server reads
    the record only where its table exists, and nothing is recorded before
    the pool registers (`server/bringyourctl/st_ledger_v2.go:68-79`;
    `server/controller/st_ledger_v2_activation.go:77-89,123-153`).
- **Stop if** readiness fails, or any v1 settlement step stops.
- **The sizing rule.** The build carries section 2.6's v1 and v2 close
  floors, capped at `maximum_gas`. Run it under step 5's re-signed policy
  (step 5, "Each operation").
- No settlement v2 code runs in the server's connect service or in the
  connect library. connect's v2 change is its API description,
  `api/bringyour.yml` at `029fb40d` (STATE.md:215). The connect service has
  its place in this deploy order through the dual-stack validator hops,
  which this build carries (section 6.3).

**Step 7. Ship the validator, the tools, the SDK and the apps, v2 off.**

This completes the "gate 5 software is released" part of §12.1 step 1.

- **Who.** Operator for the validator and the tools; owner for the releases.
- **Do.**
  1. Run the new validator binary on its unchanged config, after step 6's
     connect service is deployed (section 6.3). It writes its provider
     request checkpoint as the v2 record, so from that write on a validator
     with `request_preparation` enabled is never downgraded below it
     (section 6.1, row 7).
  2. Publish `snclaim` and the `provider` binary (`cli/snclaim`, `cli/miner`;
     RELEASE-BUILD.md:39).
  3. Release the SDK and the apps with the v2 display and claims, and without
     `Vault2Address`. Build the apps against SDK `main`, `5ff17c06` or later,
     which carries the claim API in `sdk/sn_ledger_claim.go` and the claim
     gas floors of section 2.6.
  4. Recommended: let this release carry network payout consent signing
     (section 1.3). Every app's `main` now carries it (Pins). A consent takes
     effect from the epoch after its signing, so a network that signs at
     least one epoch before E_s is paid from E_s. Two things then come first:
     1. Deploy ur.io from mmm `main`, `37859e302` or later, which holds the
        consent bridge page (`9e2e8c2b5`), the bridge fix (`64173fd13`) and
        the SDK's gas limit on v1 claims. Every native app signs the consent
        through that page (STATE.md:268,297).
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
    both the consent bridge page and the bridge fix. Hold that release;
  - on Windows, the long consent link does not open in the HTTPS browser
    that holds Talisman, or wallet sign-in fails. Sign-in sessions are now
    final once cancelled (VERSION2-HANDOFF.md, section 3, item 5).
- **The sizing rule.** The SDK and the apps built on SDK `main` carry
  section 2.6's claim floors, and ur.io sends the SDK's limit with each v2
  claim. `snclaim` and the claim daemon of sn `main` carry the v2 claim
  floor.

**Step 8. Deploy the v2 contracts with readback.**

- **Who.** Owner signs the approval and every transaction; operator runs the
  commands.
- **Inputs.** O2 to O12.
- **Start only when** all of these hold:
  1. Gate 2 holds on runtime 478. Its rerun passed, R-1 to R-7 (section 9.3,
     item 2).
  2. `sn-mainnet` is built from the launch revisions, which list the
     reviewed source of runtime 478 (step 8.3).
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
(BOOTSTRAP-CONTRACTS.md:687-705; `evm/README.md`:53-61).

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
  (BOOTSTRAP-CONTRACTS.md:687-691).
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
approved journal, with daemon scope (BOOTSTRAP-CONTRACTS.md:775-778 and "Runtime rehearsal (R-1)";
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
  (LAUNCH.md:351-354). The result's `declaration.sha256` already has the
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
    source. On `main` those are the commits of runtimes 470, 473, 475, 477
    and 478, and one historical source that earlier plans keep
    (`crv4/native_owner_source.go:6-24`; `mainnet/runtime_source_profile.go:8-12`).
    Any other commit is refused at `preview` and at `plan`: "contract phase
    runtime/source approval is incomplete" (`mainnet/evm_phase.go:183-186`).
  - A binary built before 478's entry merged (`a0a8814e`, through
    `e1fc2838`) refuses 478's commit. The dry run's binary refused 477's own,
    `5c6e83e2f4936f257234f246957f75684c35227c`, in the same way, before its
    entry merged. Build `sn-mainnet` from the launch revisions (step 1).
  - Never name another runtime's commit beside the live `specVersion`, such
    as 475's `d1718c99c34cf96abbf2bf09c0e9e48b945c76f5` under 477 or 478.
    The approval would then name a source review that nobody made of the
    running runtime. That is a mislabelled approval, and it must not be
    used. The v2 phase now refuses such a pair, in `preview` too: "contract
    phase runtime version differs from the one its source commit was
    reviewed for" (`mainnet/evm_phase.go:187-191`;
    `mainnet/runtime_source_profile.go:14-20`;
    BOOTSTRAP-CONTRACTS.md:669-679).
  - The historical source declares no version and is still accepted, "so
    the approver checks that the config names the runtime's reviewed commit"
    (:676-678). "A valid preview does not authenticate externally supplied
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
  bytes and signs them (BOOTSTRAP-CONTRACTS.md:194-198; LAUNCH.md:371-378):

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

- Review the preview before signing (BOOTSTRAP-CONTRACTS.md:762-769):
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
     SHA-256 in 64 lowercase hex characters (LAUNCH.md:424-425,445;
     `mainnet/bootstrap_contract_command.go:36,38,42`). `escrow2-register`'s
     later `apply` prints the identical transaction and digest as its own.
  3. Import both, send both back to back, and complete vault2. Expect
     `signed-custody-complete`, then
     `submitted-with-escrow-registration-awaiting-canonical-receipts`. Then
     repeat the last command, `resume --online`, until it prints
     `vault2-created` (the note after sub-step 4). That status comes after
     the readback of vault2's exact runtime and getters: `CHALLENGE_BLOCKS`
     14,400, `coordinator` zero, the counters zero and `conservationHolds`
     true (BOOTSTRAP-CONTRACTS.md:784-794).

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
     and `escrow_collateral` must read `(false, 0, 0, 0, 0)` (:795-806).

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
    BOOTSTRAP-CONTRACTS.md:807-834.
  - At `vault2-link`: `installation_complete` true and `activation_ready`
    false (:836-839). The result names `vault2_address`, `settlement2_address`
    (the proxy), `settlement2_implementation_address` and, after the sixth
    action, `claim_batcher_address` (`mainnet/evm_action.go:81-84`).
- **Stop if** any readback mismatches, or a call exits 1 for a changed
  runtime (step 8.3). Never repeat a completed CREATE, and never change the
  accepted graph or nonces to escape an ambiguous outcome
  (LAUNCH.md:445-449).

*8.6 Keep the record.* The six journals' receipts are the v2 deployment record
(BOOTSTRAP-CONTRACTS.md:839-843). Retain the run directory, the config, the
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
     (`scripts/test-release-1.0-local.sh`:854-859).
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

  6. Restart the validator on the adopted config, with the auditor key.
     - **On snow** the unit is `sn-validator.service`, which xops renders
       with `playbook-validator.yml` from `templates/sn-validator.service.j2`
       (xops `main/ansible/`, at `a0c2727`; the paths in this bullet are
       under it). Its own `ExecStart` carries every flag
       (`sn_validator_command`, `playbook-validator.yml`:50-64), so no
       drop-in applies. Set two values in `group_vars/validator.yml`:
       `sn_validator_config: /etc/sn-validator/validator-v2.yml`, the adopted
       config, root:sn-validator 0440 or 0640, and
       `sn_validator_ledger_auditor_key_file: /srv/sn25/sn-validator/keys/ledger-auditor.key`,
       the O10 key, sn-validator 0400 or 0600, 64 to 67 bytes, in a
       directory that group and others cannot write
       (`group_vars/validator.yml`:18-20,48-56). Change with them the two
       assertions of `tests/test_validator_playbook.py`:473-496 that pin the
       old values. Then run the playbook (`run-validator.sh`): it renders
       `--config=` and `--ledger_auditor_key_file=` into `ExecStart`, and a
       changed config restarts the validator on that run
       (`templates/sn-validator.service.j2`:3,15).
     - **For a unit the repository renders**, `sn-mainnet-validator-<role>.service`,
       restart it through the drop-in
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
       (`validator/run.go:492`; connect `durablevolume/config.go:240-251`).
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
  - On snow neither controller runs: xops renders and restarts
    `sn-validator.service` (sub-step 6), and none of this applies.
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
checkpoint, with no backfill (`server/controller/st_controller.go:2727-2733,4394-4397`).
The switch therefore comes before step 15. The server also records the
activation, with the pair it is configured with, on its first successful
read of the registered pool (step 15). So the pair must be right before the
pool registers.

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
  3. Check offline, against the server's activation status:

     ```bash
     bringyourctl st v2-activation > ACTIVATION.json
     sn25golive st-check --st st.yml --profile mainnet --authority AUTHORITY.yml --sn sn.yml \
       --v2-activation ACTIVATION.json
     ```

     - It runs the mainnet admission with the v2 fields
       (`server/local/sn25golive/stcheck.go:277-299`).
     - Its `identity` lines print `settlement_v2_address` and
       `vault_v2_address` in the lowercase form both files declare
       (:342-345). Step 8 prints `settlement2_address` and `vault2_address`
       in EIP-55 mixed case (`Address.Hex()`, `mainnet/evm_v2_phase.go:346-354`),
       so compare them case-insensitively: lowercase step 8's first, for
       example `jq -r '.settlement2_address, .vault2_address | ascii_downcase'`
       on its result file, then compare the lowercase strings byte for byte.
     - Before step 15 the latch is not armed: "note: v2 activation: the
       latch is not armed: the server has recorded no activation and would
       not refuse an st.yml without the v2 pair" (:326-330).
     - The last line is `result: ok`. With any `FAIL` line it exits 1 with
       `<n> check(s) failed` (`server/local/sn25golive/README.md`:193-194).
  4. Restart api, alt and taskworker. st.yml is loaded once per process, and
     the builder is installed at start (`server/controller/st_controller.go:298-302`;
     `server/api/run.go:283-284`).
- **Expect.**
  - No `[st]st.yml unavailable (st subsystem disabled)` in the logs
    (`server/controller/st_controller.go:305,312`).
  - The gauge `urnetwork_api_reserved_upload_startup_failed` reads 0
    (`server/api/run.go:255-272`).
  - `GET /sn/ledger/account` answers, with no accounts yet. It answers the
    same from step 6 on, with or without the v2 pair
    (`server/controller/sn_ledger_v2_handler.go:137-142`), so it shows the
    build, not the switch. Before step 15 no process reports its pair, so
    the switch is shown by these together:
    - every api, alt and taskworker block runs a container started after
      both files were delivered, on the config version that carries the new
      sn.yml (`warpctl ls versions main <service> --sample`). st.yml is read
      once per process at its start (`server/controller/st_controller.go:298-302`),
      and sn.yml from the container's mounted config version;
    - `sn25golive st-check` on the delivered pair prints the identity line
      equal to step 8's addresses (sub-step 3);
    - no process logs "sn: selected deployment differs from reviewed
      earnings schedule". sn.yml now names the pair, so a process whose
      st.yml lacked it would refuse its next mainnet admission with that
      line (`server/provider_payout_transition.go:161-163`);
    - the conclusive check comes at step 15, sub-step 5: the taskworker
      records the activation only with the pair its own st.yml names, and
      only if vault2 names that `STSettlement2` as its coordinator
      (`server/controller/st_ledger_v2_activation.go:58-75`).
  - v1 closes and commits continue; the pool is not registered.
  - `bringyourctl st v2-activation` still prints `"armed":false`.
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
  - Confirm the running validator unit carries `--ledger_auditor_key_file`.
    Without it the audit sends no approval (`validator/run.go:142-143`).
    Read the loaded unit, as the repository's own controller does
    (`mainnet/repair_validator_host.go:251,342`). On snow it is the
    xops-rendered `sn-validator.service` (step 10, sub-step 6):

    ```bash
    systemctl --system --no-pager show --all \
      --property=DropInPaths,NeedDaemonReload,ExecStart -- sn-validator.service
    ```

    `DropInPaths` is empty, `NeedDaemonReload` is `no`, and `ExecStart`
    carries `--config=/etc/sn-validator/validator-v2.yml` and
    `--ledger_auditor_key_file=/srv/sn25/sn-validator/keys/ledger-auditor.key`.
    For a unit the repository renders, read
    `sn-mainnet-validator-<role>.service` the same way: its `DropInPaths`
    names `ledger-auditor.conf`, and its `ExecStart` carries the same two
    flags.
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
     which it requires (`miner/run.go:136-138`; `miner/claim_v2_daemon.go:656`):

     ```bash
     provider claim-daemon-v2 --config=<path> [--v1-config=<path>] [--threshold-rao=<rao>] [--claim-now] \
       --durable-volumes=<path> --durable-volumes-sha256=sha256:<digest>
     ```

     The threshold is the `--threshold-rao` flag. The pin is a "Reviewed
     sha256: digest" (`miner/run.go:184`), and the declaration is a protected
     file under a protected ancestry, as in step 8 (connect
     `durablevolume/config.go:240-251`).
- **Expect.**
  - The daemon starts only if the batcher's code is exactly the release
    runtime (`miner/claim_v2_daemon.go:448-457`).
  - It claims an account when its claimable reaches the threshold, default
    1.9 α (`miner/run.go:197-199`).
- **Stop if** the daemon refuses its batcher, or refuses a threshold below the
  floor (`miner/claim_v2_daemon.go:653-655`).
- The daemon and `snclaim` of sn's launch revision carry section 2.6's claim
  floor (step 20).

### Phase C. Activate

**Step 14. Schedule E_s.**

- **Who.** Owner (O18). Nobody signs: this step changes no contract, file,
  key or config. E_s is read from the chain once the pool registers, never
  from config (VERSION2-FINAL.md §13). Its one output is step 15's
  `firstEpoch`, and the dates.
- **Do.**
  1. E_s is decided: epoch 3 (O18). Section 9 holds its schedule. The plan's
     own rule takes E_s as the first epoch that starts at least two days
     after the last gate passes (§14): for epoch 3 every gate passes by
     block 9,333,628. That rule applies again if E_s moves.
  2. Compute its start. With the weekly policy effective from epoch 1 at
     block 9,247,228 (LAUNCH.md:29-32), `epochStartBlock(e)` is
     9,247,228 + 50,400 × (e − 1) (`evm/src/STCoordinator.sol:302-310`). This
     holds while the v1 policy schedule is unchanged. Epoch 3 starts at block
     9,348,028, which `epochStartBlock(3)` on the v1 coordinator returns.
  3. Pick the registration date: one to two days before the start of E_s,
     inside the 21,600-block registration immunity (§12.1 step 5). At 12
     seconds a block that is 7,200 to 14,400 blocks before the start: blocks
     9,333,628 to 9,340,828 for epoch 3.
  4. Check the v1 policy for E_s: `epochBlocks >= rootCommitWindowBlocks +
     14,400` (`evm/src/STSettlement2.sol:86-89`). Mainnet's 50,400 and 1,200
     satisfy it (§5), and `policyAt(3)` on the v1 coordinator returns them.
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

  5. Once the registration is finalized, check on the server host that the
     activation record is armed, and check the installed st.yml against it:

     ```bash
     bringyourctl st v2-activation > ACTIVATION.json
     sn25golive st-check --st st.yml --profile mainnet --authority AUTHORITY.yml --sn sn.yml \
       --v2-activation ACTIVATION.json
     ```

     `bringyourctl sn-transition-status` carries the same status as
     `ledger_v2_activation` (`server/controller/st_payout_transition.go:78-79,115`).
     The latch arms only on a successful read of the registration. Until it
     does, removing the pair is not refused
     (`server/controller/st_ledger_v2_activation.go:18-28`). Never remove the
     pair after this step.

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
  - Within a minute of the registration's finality the server's driver sees
    the pool, and the v1 driver keeps only epochs below E_s
    (`server/controller/st_ledger_v2_driver.go:197-222,263-297`).
  - On that first successful read the server records the activation in
    `st_ledger_v2_activation`, with the pair it is configured with, if
    vault2's `coordinator()` is its `settlement_v2_address`
    (`server/controller/st_ledger_v2_activation.go:58-75`). From then on its
    st subsystem runs only with that pair (section 6.1, row 11).
  - Sub-step 5: `bringyourctl st v2-activation` prints `"armed":true`,
    `no_id` 1, `first_epoch` E_s, `vault_v2_address` and
    `settlement_v2_address` equal to step 11's, `recorded_at`, and no
    `refusal` (`server/controller/st_ledger_v2_activation.go:123-153`).
    `st-check` prints "ok: v2 activation: the latch is armed (pool 1 from
    epoch E_s) and st.yml names its recorded v2 pair" and ends with
    `result: ok` (`server/local/sn25golive/stcheck.go:326-327`).
- **Exit codes**, the same for both modes (BOOTSTRAP-CONTRACTS.md:1004-1009;
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
      (`mainnet/evm_v2_pool.go:276-301`; BOOTSTRAP-CONTRACTS.md:989-1002).
  - `readback` exits 3. It names each difference: no receipt; a reverted
    transaction; no `PoolRegistered` event of `noId` from vault2, or one with
    another hotkey or first epoch; `pools(noId)` unregistered or different;
    `settlementAt` with another vault, hotkey or version; a view that the
    chain ran and failed.
  - A reverted registration: "If it fails, nothing has changed, and E_s
    moves" (§12.1 step 5).
  - Sub-step 5 shows a `refusal`, or `st-check` a `FAIL`: the loaded st.yml
    does not name the recorded pair. Restore the recorded pair in both files
    (section 6.1, row 11), or, for a record of the wrong pair, see section
    6.2.
  - Sub-step 5 still shows `"armed":false` once the registration is
    finalized. While the reads succeed the record arms within about a
    minute. The taskworker log names what stops it:
    - `[st]v2 sync failed: st ledger v2: st.yml's settlement_v2_address is
      not vault2's coordinator: vault2 0x… is bound to 0x…, not 0x…; the
      pool's activation is not recorded`: st.yml names another
      `STSettlement2`, and nothing is recorded
      (`server/controller/st_ledger_v2_activation.go:63-71`;
      `server/taskworker/work/st_ledger_v2_work.go:42-46`);
    - a failed read of `vault2.pools` or `vault2.coordinator()`: the read
      repeats each turn, and the record arms on the first that succeeds
      (`server/controller/st_ledger_v2_driver.go:203-222`).
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
    (`server/controller/st_ledger_v2_client.go:33-48`;
    `server/controller/st_ledger_v2_driver.go:8-14`).
  - Every send passes its `eth_call` preflight and is signed within the
    policy (`server/controller/st_ledger_v2_client.go:387-409`). Commit,
    finalize and answer are signed at the estimate plus 20%, and a close at
    max(estimate + 20%, min(3,200,000, `maximum_gas`)) gas
    (`server/controller/st_controller.go:1135-1153`; section 2.6).
- **Before this step** the server runs step 6's build under step 5's policy,
  whose `maximum_gas` is at least 3,200,000. For E_s = 3 the first v2 close
  window opens at block 9,398,428, the end of epoch 3, about 2026-10-30 18:10
  UTC, and closes at block 9,399,628.
- **Act if** `v2_missed_close` or `v2_missed_commit_window` fires (section 5).
  Anyone may close inside the window (§7.2). A missed close defers the epoch with
  zero funding, and the next close captures its emission (§6.4).
- **Act if** a close reverts. No alert names it; section 5 gives its log line.
  The policy allows one close three signatures. A refused try takes no
  nonce, and the root key's other sends go on (section 7).

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

     `snclaim` signs a single claim at section 2.6's size, max(estimate +
     20%, 2,600,000) gas (`miner/onchain/v2_claim.go:319`;
     `miner/onchain/eth.go:203-206`). Give no `--gas_limit`: it replaces
     that size, the floor included, for a single transaction, and does not
     apply to a `--batch` one (`miner/onchain/run.go:91-93`). A batch
     forwards each claim the larger of 2,600,000 and the batch's largest
     estimate (`miner/onchain/v2_batch.go:28-42,181-208`).
  3. Then let the daemon and the apps claim. The apps built on SDK `main`
     carry the rule. The claim daemon batches two or more due accounts, sized
     as a `--batch` is, and claims a lone one alone at no less than 2,600,000
     gas (`miner/claim_v2_daemon.go:210,269-287,323-329,421`).
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
| `v2_missed_close` | taskworker log | no close by end(e) + 1,200 | the epoch defers with zero funding; the next close captures it | `server/controller/st_ledger_v2_driver.go:366-369,540-543` |
| `v2_missed_commit_window` | taskworker log | no root committed by end(e) + 1,200 | the next root covers the epoch from its own usage | `server/controller/st_ledger_v2_driver.go:370-376,672-675`; VERSION2-FINAL.md §6.4 |
| `v2_open_demand_over_one_day` | taskworker log | a demand open for one day, 7,200 blocks at 12 seconds | check the server's answer of that demand | `server/controller/st_ledger_v2_driver.go:384-389` |
| `v2_root_not_finalized` | taskworker log | not finalized by end(e) + 14,400 + 1,200 | check approval, demands and pause | `server/controller/st_ledger_v2_driver.go:393-406` |
| `v2_sn25_prune_risk` | taskworker log | SN25 among the lowest 3 non-immune subnets by moving price, or no longer added; read every ten minutes | encourage claims (§17); dissolution strands custody | `server/controller/st_ledger_v2_prune.go:33-38,138-165` |
| `[st]v2 prune check failed` | taskworker log | any | the census read failed | `server/controller/st_ledger_v2_driver.go:334-338` |
| `[st]v1 cutoff for epoch … unknown` | taskworker log | any | a `vault2.pools` or `vault2.coordinator()` read failed, or the activation could not be recorded (below); that turn's v1 steps wait | `server/taskworker/work/st_ledger_v2_work.go:29-37`; `server/controller/st_ledger_v2_driver.go:273-297` |
| "st: settlement v2 is active from epoch … (pool …), but st.yml is missing the recorded … (st subsystem disabled)", or "… but st.yml differs from the recorded …" | taskworker log, after `[st]sync chain state failed: `; the `readiness_reason` of `bringyourctl sn-transition-status` | any | st.yml lacks the recorded v2 pair, or names another. Every sync turn, mirror turn, chain write and v2 operator command stops; restore the recorded pair in both files and restart (section 6.1, row 11) | `server/controller/st_ledger_v2_activation.go:91-121`; `server/taskworker/work/st_work.go:103-106`; `server/controller/st_payout_transition.go:34-37,115-118` |
| "st ledger v2: st.yml's settlement_v2_address is not vault2's coordinator: …" | taskworker log, after `[st]v2 sync failed: ` or in the v1 cutoff line | any | the activation is not recorded, so the latch is not armed; correct both files to the deployed pair (step 11) | `server/controller/st_ledger_v2_activation.go:63-71`; `server/taskworker/work/st_ledger_v2_work.go:29-46` |
| "st ledger v2: the deployment's activation is already recorded: …" | taskworker log | any | a later read of the registration differs from the record. The v2 driver waits, and v1 keeps the recorded cutoff | `server/model/st_ledger_v2_activation.go:33-59`; `server/controller/st_ledger_v2_activation_test.go:305-331` |
| "sn: selected deployment differs from reviewed earnings schedule" | server log | any | st.yml and sn.yml disagree; every mainnet send is refused | `server/provider_payout_transition.go:161-163` |
| "unsigned gas/fee/tip/intent envelope exceeds approval" | server log | any | the gas policy is too low for a send | `server/model/st_transaction_gas_model.go:109-110` |
| a reverted close | taskworker log | any. No alert names it. v2: a `[st]v2 epoch step epoch <e> …: failed …` line; v1: `[st]epoch <e> close failed (attempt <n>): st: epoch <e> emission close not confirmed: failed …`. Each reason holds `st: finalized transaction <hash> reverted` | find the cause. The policy allows one close three signatures, and less under an undersized liability; a refused try takes no nonce (section 7) | `server/controller/st_controller.go:1322-1325,3768-3773`; `server/controller/st_ledger_v2_driver.go:471-483`; `server/taskworker/work/st_ledger_v2_work.go:90`; `server/taskworker/work/st_work.go:244` |
| an operation the gas policy refuses | taskworker log | a send refused with `operator gas allowance is unavailable or exhausted; original liabilities retained: original logical intent <key>` | the operation `<key>` has spent its three signatures or its liability. Its try takes no nonce, and the key's other sends go on. If sends of other operations are refused with that same `<key>`, an older build stranded the key: recover it by section 6.5 | `server/model/st_transaction_gas_model.go:24,363-364`; `server/controller/st_controller.go:1720-1753`; `server/controller/st_ledger_v2_gas_test.go:334-448` |
| `urnetwork_api_reserved_upload_startup_failed` | API metric | 1 | the reserved upload lane is off | `server/api/run.go:33-38,255-272` |
| `ledger_audit_approve`, `_challenge`, `_demand`, `_refuse`, `_retrying` | validator diagnostics | anything but an approval of an honest root | read the custody record | `validator/release_diagnostics.go:223`; `validator/settlement_v2_custody.go:3-8` |
| the byte budget | validator audit | a root's new objects over `--ledger_max_bytes` | raise the flag; it needs no re-sign | VERSION2-FINAL.md §6.6; `validator/settlement_v2_runtime.go:3-5` |
| the watcher's report | `snclaim v2 check` output | any provable fault or "unverifiable" | challenge or demand inside the window | VERSION2-FINAL.md §10.2 |
| the watcher's service | systemd on the watcher host | the service is failed | "A failed run leaves the service failed; alert on that" | SETTLEMENT-V2-EXAMPLES.md:159-161 |
| claim gas | claim receipts | about 1.27M gas a claim instead of about 0.2M | a third party filled vault2's staking-hotkey list; raise daemon thresholds to about 10 α | §3, §10.1, §17 |
| validator weights | every tempo | a tempo without weights | the escrow hotkey earns by stake in such a tempo; that is surplus | §7.3, §17 |
| the runtime | `sn-mainnet runtime-snapshot` | a `specVersion` after 478, the runtime of step 8 | rehearse vault2 on it (gate 2), and sign a native-fee denomination policy for its code | §14, §17; steps 2 and 5 |
| the treasury approval | the native head | block 9,739,882 | renew before it, as a separate successor | §13 |

## 6. Abort and rollback

### 6.1 What each step leaves behind

| Step | Can it be undone? | How |
| --- | --- | --- |
| 1 to 4 | Yes | Nothing in production changed. |
| 5, gas policy | Only forward | A changed policy must be the next revision naming the current digest; the server refuses "operator gas policy rollback or disconnected revision" (`server/model/st_transaction_gas_model.go:389-391`). To lower the caps, sign a further revision with the same approver key. The native-fee policy is replaced by installing another policy and authority pair. Once a settlement has recorded an approver key, only that key can sign one, and a transaction already settled keeps its original denomination (`server/model/st_native_fee_settlement_model.go:189-197,266-269`). |
| 6, server build | Yes, while v2 is off | v2's migrations 821, 822 and 832 add new tables only, which v1 settlement does not need: without the v2 pair the server reads the activation table only where it exists (`server/db_migrations.go:10013-10025,10169-10187`; `server/model/st_ledger_v2_activation.go:71-86`). An older binary requires a lower migration head, so it passes readiness (`server/router/warp_handlers.go:186-189,255-260`). Upstream's 823 to 831 and 833 to 835 come with the same `main`; their rollback is upstream's, not this runbook's. |
| 7, validator build and app releases | Yes, the validator only until it writes its request checkpoint | Without the block, the new validator hashes today's config identically (§13). The released app code hides the v2 claim without `Vault2Address`. The launch's validator writes its provider request checkpoint as a v2 record, which an older release refuses, failing closed. So a validator with `request_preparation` enabled is not downgraded below sn `43ed5e30` once that release has written its checkpoint, including by its own storage-prepare (`validator/PROVIDER-REQUEST-CUSTODY.md`:46-56). |
| 8, contracts | No, but they can be abandoned | vault2 is immutable. Until a pool registers it holds nothing and nothing depends on it (BOOTSTRAP-CONTRACTS.md, "Two residuals remain"). A redeploy needs a new approval, a fresh escrow hotkey and fresh nonces. After step 10 a redeploy also meets the validator's one-way adoption (row 10). |
| 9, `Vault2Address` | Yes, by a release | The compiled default is never stored on the device (`sdk/sn_chain.go:352-355`; `sdk/sn_local_state.go:72-80`), so a release that clears or changes it takes effect on update. This is an inference from the code. |
| 10, adoption | No | The block "can be neither re-pointed nor removed" (`validator/production_authority_history.go:256-260`). Until step 15 the validator still weights through `operatorAt` (§9.1). No repository path re-points it; a redeploy after adoption needs new validator code. |
| 11, server switch | Before step 15 only | Remove the pair from st.yml and sn.yml together, then restart. v1 is untouched: until vault2 registers the pool the server records nothing, and the pair and its removal behave as before (SV-9; `server/controller/st_ledger_v2_activation.go:18-25`). After step 15, never remove it. On its first successful read of the registration the server records the activation with its configured pair, in `st_ledger_v2_activation` (migration 832; `server/controller/st_ledger_v2_driver.go:211-219`). From then on, without that pair or with another, it refuses every sync turn, mirror turn, chain write and v2 operator command, and payout readiness gives the reason: "st: settlement v2 is active from epoch <E_s> (pool 1), but st.yml is missing the recorded settlement_v2_address <address> and vault_v2_address <address> (st subsystem disabled)". A changed pair gives "differs from", and names only the keys that differ (`server/controller/st_ledger_v2_activation.go:91-121`). The mirror's checkpoint stays where it was, so restoring the pair resumes from it, and the recorded first epoch stays the v1 cutoff (`server/controller/st_ledger_v2_driver.go:273-297`; `server/controller/st_controller.go:4384-4388`). Before the record arms, removing the pair lets the v1 driver close and commit E_s and later (`server/controller/st_ledger_v2_driver.go:274-276`) and the mirror pass v2 logs unread, with no backfill (`server/controller/st_controller.go:2727-2733,4394-4397`). So check that it is armed (step 15, sub-step 5). A wrong record is cleared only by `bringyourctl st v2-activation-clear` (section 6.2). |
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
- **A later st.yml change**, such as a gas policy revision: check the
  candidate first with `sn25golive st-check … --v2-activation`, against a
  fresh `bringyourctl st v2-activation`. An armed latch gives `FAIL` with the
  server's own refusal for an st.yml that lacks or changes the recorded pair
  (`server/local/sn25golive/README.md`:175-182).
- **A wrong activation record.** If the record names a pair that is
  consistent on chain but is not the deployment's, the server refuses the
  corrected st.yml until the operator clears the record (SV-9):

  ```bash
  bringyourctl st v2-activation-clear --no_id=<no_id> --first_epoch=<first_epoch> \
    --vault_v2_address=<vault_v2_address> --settlement_v2_address=<settlement_v2_address> --by=<operator>
  ```

  - Every value must equal what `st v2-activation` shows
    (`server/bringyourctl/main.go:140-141`).
  - Otherwise it panics with the reason, exits 2 and changes nothing: "st
    ledger v2: the activation is not cleared: no_id, first_epoch,
    vault_v2_address and settlement_v2_address must name deployment <key>'s
    record exactly", "st ledger v2: the activation is not cleared:
    deployment <key> has none recorded", or "st ledger v2: clearing the
    activation must name who clears it"
    (`server/controller/st_ledger_v2_activation.go:170-194`;
    `server/bringyourctl/st_ledger_v2_test.go:115-117,189-193`).
  - On success it prints "activation of pool <n> from epoch <e> cleared by
    <operator>", and the server logs who cleared it
    (`server/bringyourctl/st_ledger_v2.go:81-108`).
  - The server records the activation again at its next read of the
    registration, after a restart if it already read it. Clearing a correct
    record only disarms the latch until then.

### 6.3 Server and connect order

- **Deploy:** `bringyourctl db migrate`; then the server binaries, in the
  dual-stack order below (step 6); then the validators (step 7); then,
  later, st.yml and sn.yml together; then a restart (step 11). Old binaries
  refuse an sn.yml with the v2 keys
  (`server/provider_payout_transition.go:57-58`).
- **Roll back:** the config first, both files together, then a restart, then
  the binaries. Never roll back the config after step 15: the server then
  refuses to run without the recorded pair (section 6.1, row 11).
- **connect** takes no part in settlement v2. No v2 code runs in the connect
  library or in the server's connect service. The v2 routes live in api and
  alt, and the driver in the taskworker (step 6).
- **The dual-stack validator hops** set the order of the binaries. The
  launch's builds carry them (section 1.3).
  - Deploy: the server's API and taskworker first, which read both versions
    of the egress index; then its connect service, which writes the new
    one; then the validators, which handle the retryable answer
    (`VALIDATOR.md`:812-828; connect `IPV6.md`:332-341).
  - `hop_latency_from_attributed_attempt` stays off in verify.yml until
    every validator, third-party ones included, runs the relaxed receipt
    check (section 1.3).
  - Roll back: that setting off first; then the connect service; then the
    API and the taskworker (`VALIDATOR.md`:825-828).

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

### 6.5 A root key stranded by an older server build

Before the fix of section 7 a try that the gas policy refused kept its nonce
unsigned. Such an intent still holds its nonce under the new build, and every
send of the key stops at it. A re-signed policy does not lift it.

1. Stop the engine's sends for that key, at the taskworker or at payout
   admission.
2. From outside the engine, send a 0-value self-transfer of the key at the
   stranded nonce.
3. Wait until that transaction is finalized. Then let the engine send again.
   Its next send retires the stranded intent as superseded, with "st: account
   nonce <n> was consumed without a known canonical attempt", and the send
   after it goes out (`server/controller/st_controller.go:1588-1600`;
   `server/controller/st_ledger_v2_gas_test.go:385-448`).

Never delete rows. Steps 1 and 3 are the correction that the fix's review
made. The server's written procedure is `docs/operator/st-stranded-nonce.md`,
added by server `912859cd` and merged into `main` by `10b0bd3f`. It wins where
this section differs (VERSION2-HANDOFF.md, section 1).

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
| The validator unit carries no `--ledger_auditor_key_file`, so no root is approved | On snow, xops's `sn_validator_ledger_auditor_key_file`, rendered into the unit's `ExecStart`; for a unit this repository renders, the drop-in `mainnet/validator-ledger-auditor.example.conf` (step 10); and the unit check of step 12 |
| st.yml and sn.yml disagree, which refuses every mainnet send, v1 included | One change for both files, `sn25golive st-check` and the step 11 stop conditions |
| A failed `vault2.pools` read delays v1 closes and commits | The log line of section 5; RPC health |
| The mirror has no v2 backfill, and without the pair the v1 driver would resume epochs at or after E_s | Switch the server before step 15, and never remove the pair after it. The activation record (SV-9) refuses the st subsystem without the recorded pair once the server has read the pool's registration (section 6.1, row 11) |
| The record arms only on a successful read of the registration. Before that read, while it fails, or while the server is stopped, removing the pair is not refused | Check that it is armed after step 15 (step 15, sub-step 5), and never remove the pair after step 15 (`server/controller/st_ledger_v2_activation.go:18-28`) |
| An app is built against a store without its strings | The store's `main` holds the `sn_ledger_*` and `sn_consent_*` keys (section 2.1); build each app against it or a later one |
| No CI anywhere | The owner's pipeline and the hand-run suites of section 2.2 |
| The rehearsal tooling builds only runtime 478, and mainnet replaces its runtime every few days | Step 2's check; a reviewed pin change before any rerun on a later runtime |
| A new runtime lands between two steps. 478 was installed 318 blocks after it was proposed | Step 2 immediately before steps 8 and 15. A send on a changed runtime is refused, and an inclusion under an unapproved runtime stays unresolved (step 8.3) |
| A binary that lists no reviewed source for the live runtime refuses the phase config | `main` lists 478's (step 8.3). Never name another runtime's commit: the v2 phase refuses a reviewed source beside another version |
| A shell umask of 002, or a group-writable directory above the run directory, makes the custody tools refuse | `umask 077` and the ancestry check at the start of step 8 |
| One `resume --online` does not complete an action, and a late start checkpoint multiplies the calls | Take the start checkpoint just before the approval; repeat `resume --online` until the completed status (steps 8.3 and 8.4) |
| On 478 a stake exit's admission grows by 8,750 gas for each entry of vault2's own staking-hotkey list, which third parties can lengthen. A limit sized from an estimate can then fall short | The owner-approved sizing rule of section 2.6: a close at max(estimate + 20%, 3,200,000) gas and a claim at max(estimate + margin, 2,600,000). The server's v1 and v2 close floors, capped at `maximum_gas`, the SDK's claim floors and sn's claim tools' floor are on `main`, and ur.io sends the SDK's limit with v1 and v2 claims. The whole close floor needs the re-signed policy (step 5) |
| Three reverted closes of one epoch spend the three signatures the policy allows an operation. Before the fix the fourth try reserved a nonce the policy would not sign, and every later v1 and v2 send of the root key stopped at it | Fixed on server `main` (`8d63033c9`, merged as `4e5759875`): under a gas policy a new nonce is reserved only together with its first attempt's gas reservation, so a refused try takes no nonce and the key's other sends go on (`server/controller/st_controller.go:1720-1753`; `server/model/st_transaction_gas_model.go:498-506`; SV-1, `server/controller/st_ledger_v2_gas_test.go:334-383`). The policy still refuses that close, also when re-signed with more signatures. Watch for reverted closes (section 5) |
| A per-intent liability that covers one floored close leaves a reverted close no second signature, because the policy sums an operation's liability over every nonce it takes. The refused try takes no nonce | `maximum_intent_liability_wei` of at least 3 × `maximum_gas` × the fee cap, which today's policy does not meet (step 5; `server/controller/st_ledger_v2_gas_test.go:450-481`) |
| A key that an older server build stranded stays stranded under the new build | The recovery of section 6.5: stop the engine's sends for the key, consume the stranded nonce with a 0-value self-transfer from outside the engine, and wait for its finality. Never delete rows |
| The v1 close shares the root key, and its capture moves stake as a v2 close's does | Server `main` signs it at no less than min(3,200,000, `maximum_gas`) (SV-4; `server/controller/st_controller.go:1970-2002`). Watch v1 closes too (section 5) |
| Under today's gas policy, `maximum_gas` 1,000,000 and per-intent liability 5·10^16 wei, step 6's build signs a close at 1,000,000 gas, and above a 25 gwei fee cap a reverted close gets no second signature | Install the re-signed policy before step 6's build runs (step 5; `server/controller/st_ledger_v2_gas_test.go:184-246,450-481`) |
| Until a native-fee denomination policy names runtime 478, every operator send from block 9,276,926 keeps its full gas ceiling against the gas policy's intent and lifetime liabilities | Re-sign it for 478 (step 5, O27); size `maximum_lifetime_liability_wei` for sends at their ceilings (O13) |
| A gas key topped up below a v1 claim's limit × the price fails the claim with `needs_gas`: about 0.0149 TAO at 2,700,000 gas and 5 gwei | The apps' `main` suggest a 0.02 TAO v1 top-up (section 2.6), and the SDK's `needs_gas` message names the amount the claim needs (`sdk/sn_claims.go:903-905`) |
| The release gate fails on the latest `main` | Each failure is fixed on a branch, merged and pulled, and the gate runs again until it passes (O28; VERSION2-HANDOFF.md, section 2) |
| The loader admits an adoption that appends no authority history entry, and the validator then refuses every decision made under the running config | The by-hand check of step 10, sub-step 2, and step 10's stop condition |
| The repair and activation controllers refuse the adopted validator, and the rendered unit starts the pre-adoption config without the auditor key | Restart the unit with its drop-in. Around a controller run, remove the drop-in and reinstall it (step 10) |
| `preview` cannot tell a wrong but well-formed owner input, and it checks `native_chain` and `specName` only for being non-empty | The comparison step of `mainnet/SETTLEMENT-V2-FILL.md`, read against the owner inputs, and the by-eye review of step 8.3 |
| The claim daemon's state directory is not a prepared durable root | The `provider-claim-queue` preparation of step 13 |
| A deposit's staging transfer reserved but not signed before its epoch rolled stays refused on the deposit key ("st: retained deposit intent belongs to another immutable operation"), and the key's later sends stop at it. Signing it would move stake, so the engine does not. No operator step frees it; it needs a change to the engine | Dormant while deposits are at zero price, since the deposit task then sends nothing. An expired deposit reserved but not signed no longer blocks the key: server `ed453384d` to `adc257c06`, merged as `d43dd4af1`, which run 5's server head contains (VERSION2-HANDOFF.md, section 3, item 12) |
| An intent of a replaced deployment that holds an unsigned execution reservation blocks its key: its cancellation is refused while the reservation stands | Pre-existing; the expired-deposit fix does not cover it (VERSION2-HANDOFF.md, section 3, item 12) |
| A validator downgraded below the launch's release after that release wrote its v2 request checkpoint refuses the record and fails closed | Never downgrade it below that release (section 6.1, row 7) |

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
4. **The gate 3 CI job.** SV-8's `TestEvmV2*` runs only in the `./mainnet`
   hash shards, not in `sn-v2`, gate 3's own job (STATE.md:129;
   `scripts/test-release-1.0-local.sh`:177-178,535-553). The `mainnet`
   package, which exceeded the 90-minute budget on the build machine
   (STATE.md:133), now runs in eight shards (step 4). No CI runs any of this
   (section 2.2).
5. **The phase C sweep**, with `E_sweep >= E_s + 8` (§16).
6. **Network payout consent** (§11.1), the largest single increase in
   providers paid. It ships on its own, independent of activation, after
   ur.io has the consent bridge page (step 7). If it misses step 7's release,
   release it as soon as it is ready: a network's app-only providers stay
   unpaid until the epoch after it signs.
7. **The Windows v2 Claim colour**, blue or lime (STATE.md:200).
8. **Tooling gaps** marked above:
   - an engine change that frees a deposit's staging transfer reserved but
     not signed before its epoch rolled (section 7);
   - a tool that writes and signs the native-fee denomination policy, and
     a decision on its native authority for 478 (step 5);
   - a rendered validator unit that names the adopted config and the auditor
     key, for the repair and activation controllers (step 10);
   - an example of the claim daemon's `provider-claim-queue` request
     (step 13);
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
     sending the SDK's limit (section 2.6);
   - the reviewed source of runtime 478 and its rehearsal pins (steps 2 and
     8.3);
   - sn's claim floor for a single claim, the server's v1 close floor, the
     cap of both close floors at `maximum_gas`, and the SDK's v1 claim floor
     (section 2.6);
   - the local build mode and its config field (step 1);
   - the server's activation record (section 6.1, row 11);
   - build and run instructions for `sn25golive` (step 5);
   - the server's fix for a stranded root key (section 7);
   - ur.io sending the SDK's limit with v1 claims (section 2.6);
   - the release gate's selection of SV-4's and `sn25golive`'s tests
     (step 4);
   - the server's written recovery of a root key that an older build
     stranded, `docs/operator/st-stranded-nonce.md` (server `912859cd`;
     section 6.5);
   - the release gate's failing tests, each fixed and merged
     (VERSION2-HANDOFF.md, sections 1 and 2);
   - the deposit key's fix for a deposit reserved but not signed before its
     deadline (server `d43dd4af1`; section 7).
9. **The open product issues** of VERSION2-HANDOFF.md, section 5: the proxy
   stall at a contract rollover, the speed-test echo that closes H1, and the
   server suite fixture's missing GeoLite2 database.

## 9. Schedule for E_s = 3

E_s is epoch 3 (O18). The owner first chose epoch 2 (2026-10-13: "we are
launching v2 now to be active the next ur epoch") and then moved E_s to
epoch 3.

One chain reading anchors the times below: Snow's archive answered block
9,279,060 with the timestamp 2026-10-14 04:15:36 UTC. It is an observation,
not a repository fact. The blocks are exact (step 14). The times assume 12
seconds a block (`server/controller/st_controller.go:89`), so they are
approximate; recent blocks have run slightly slower.

### 9.1 Blocks and times

| Block | About, UTC | What | Source |
| --- | --- | --- | --- |
| 9,279,060 | 2026-10-14 04:16 | the reading, in epoch 1 | |
| 9,297,628 | 2026-10-16 18:10 | epoch 2 starts, and with it v1's close of epoch 1, the first under step 5's policy | step 5 |
| 9,333,628 | 2026-10-21 18:10 | the plan's registration window opens, 14,400 blocks before E_s starts. Every gate passes by this block (§14) | §12.1 step 5 |
| 9,340,828 | 2026-10-22 18:10 | the plan's registration window closes, 7,200 blocks before E_s starts | §12.1 step 5 |
| 9,348,028 | 2026-10-23 18:10 | epoch 3 starts. This is the hard on-chain limit: `registerOperatorPool` must be mined before this block, while the current epoch is 2 or lower | `evm/src/STSettlement2.sol:80-82` |
| 9,398,428 | 2026-10-30 18:10 | end(E_s): the first v2 close and commit window opens. By then the server runs step 6's build under step 5's policy, with a `maximum_gas` of at least 3,200,000 (step 17) | §5 |
| 9,399,628 | 2026-10-30 22:10 | close and commit by this block | §5 |
| 9,412,828 to 9,414,028 | 2026-11-01 18:10 to 22:10 | the earliest finalization, 14,400 blocks after the commit | §5 |
| after the finalization | | the first claims (step 20) | §17 |

### 9.2 A compressed order of steps 1 to 15

Steps 1 to 12 must be complete before step 15 is sent. Their dependencies set
the order:

| Order | Steps | Runs in parallel | Owner signs |
| --- | --- | --- | --- |
| 1 | 1 and 4: the latest `main` of every repository; the gate in its local mode, its failures fixed and merged until it passes; then build in the local mode from the heads it passed on | with steps 2 and 3 | |
| 2 | 2 and 3: the runtime check and the runtime rehearsal. Gate 2's rerun on runtime 478 has passed, R-1 to R-7 | with each other, and with order 1 | none: O26, the gate 1 confirmation, was given 2026-10-13, and O28 is decided |
| 3 | 5, 6, 7: the gas policy and the native-fee policy, the server build with migrations, the validator binary and the tools | 7's tools and apps with 5 and 6; 6 once 5's gas policy is installed; 7's validator binary after 6's connect service (section 6.3) | step 5, with the gas approver key and the denomination approver key |
| 4 | 8: deploy the contracts, after steps 2 to 4 pass and step 2 has run again | its six actions run in sequence | the phase approval, with the approval key; then six transactions, with the deployer key |
| 5 | 10: adopt the validator config, after step 8 and step 7's validator binary | with step 13 | the treasury approval, with its approval key |
| 6 | 11, then 12: the server switch, after steps 6, 8 and 10 | with step 13 | |
| 7 | 15: register the pool, inside blocks 9,333,628 to 9,340,828, after step 2 once more | | the Safe |

- Gate 2's rerun on runtime 478 has passed, and `main` lists 478's reviewed
  source (section 9.3, item 2).
- Step 6's build runs under step 5's re-signed gas policy (step 5), and it
  precedes step 11, because the binary goes before the config.
- Step 2 runs again immediately before step 8 and immediately before
  step 15.
- Step 9, the pinned app releases, can precede step 15 again (section 9.3,
  item 4).
- Step 13 is off the critical path. The watcher must run by the first commit,
  at block 9,398,428 or later.
- Step 14 is decided.
- Known durations:
  - the rehearsal node built in about 37 minutes from scratch for 475, and
    in 648 seconds for 477 and 746 seconds for 478 on a copy of that build.
    The R battery ran in about 15 minutes on 475, in 776 seconds on 477 and
    in about 560 seconds on 478
    (`scripts/runtime-rehearsal/README.md`:50-58,240-242,442-446,520-523);
  - the release gate's `mainnet` package alone exceeded 90 minutes on the
    build machine (STATE.md:133);
  - v1's eight contract actions landed within 84 blocks, about 17 minutes
    (LAUNCH.md:36-45).

### 9.3 Departures from the plan of record

The owner accepted items 1, 4, 5 and 6 for epoch 2 (2026-10-13). Under
epoch 3 each is moot or relaxed, as it says. Item 2 is a gate, not a
departure, and its rerun has passed.

1. **The two-day rule.** "E_s is the first epoch that starts at least two days
   after the last gate passes" (§14). For epoch 2 the last gate, gate 6 at
   step 12, would have had to pass by block 9,283,228. For epoch 3 it passes
   by block 9,333,628, about 2026-10-21 18:10 UTC, and the rule then holds:
   the departure is moot. A gate that passes later is a departure again,
   which the owner accepts anew, or E_s moves to epoch 4 (section 9.4).
2. **Gate 2 on runtime 478.** Gate 2 ran on 475. "Any runtime change on
   mainnet repeats this gate" (§14), and "gate 2 cannot be waived" (§17).
   Mainnet has since moved to 477 and, at block 9,276,925, to 478 (step 2).
   The contracts will be deployed on 478, so gate 2 must hold on 478.
   - The verdict on 478: R-1 to R-7 all pass on a node built from v478, with
     `main`'s bytecode. The `rehearsal` package took about 560 seconds and
     R-1 124 (`scripts/runtime-rehearsal/README.md`:520-523). The deploy gas
     is unchanged from 475. The changes are in section 2.6.
   - 478's reviewed-source row, `crv4.NativeOwnerSource478`, admits commit
     `850f2755…` at version 478, and R-1 on the 478 node names it and passes
     (`crv4/native_owner_source.go:10,23`). Without the row the phase refuses
     both choices on 478: 478's own commit with "contract phase
     runtime/source approval is incomplete", and 475's or 477's commit with
     version 478 with "contract phase runtime version differs from the one
     its source commit was reviewed for" (`mainnet/evm_phase.go:183-191`).
   - The 477 run passed in full, with gas equal to 475's. It is recorded on
     `main` (`docs/spec/runtime-477-audit.md`:405-408;
     `scripts/runtime-rehearsal/README.md`:430-446).
   - The 478 entry, the 478 pins and the record of this rerun are on `main`
     (`e1fc2838`; `docs/spec/runtime-478-audit.md`). The 477 entry
     (`3b5628aa`) binds each reviewed source to its spec version for the v2
     phase.
   - Step 2 is repeated immediately before step 8 and before step 15.
3. **O26.** Gate 1 needs the independent review of vault2, `LedgerTree.sol`
   and `STSettlement2` resolved (§14). The owner confirmed it resolved on
   2026-10-13, so gate 1 does not hold step 8.
4. **The apps.** Activation needs v2 claims in the apps (§2.1 decision 8; §11;
   §14 gate 5). For epoch 2 the releases with the pinned `Vault2Address`
   (step 9) could not precede step 15. For epoch 3 they can precede it
   again, as §12.1 orders, since step 8 deploys the contracts before the
   registration window opens. Relaxed: a store whose review is slower still
   has until the first claims, which are impossible before the first
   finalization, at block 9,412,828 or later.
5. **Consent.** A consent takes effect from the epoch after its signing
   (§11.1), so only one signed before block 9,348,028 covers epoch 3, a week
   later than epoch 2's 9,297,628. Relaxed by that week. What it needs is
   unchanged: ur.io deployed with the consent bridge page and the bridge fix
   (STATE.md:268,297), an app release that carries consent signing (step 7),
   and a network's signature before that block. Without them, app-only
   providers stay unpaid in epoch 3, as in v1.
6. **Other validators.** A validator weights the v2 pool UID only through its
   `settlement_v2` block; without it, it resolves every epoch through
   `operatorAt` (§9.1). Any third-party validator must adopt before E_s, now
   block 9,348,028 instead of 9,297,628. Relaxed by one epoch; the
   requirement stands.

### 9.4 Decision point

If any stop condition fires, or the registration window of section 9.1 is
missed, E_s moves to epoch 4, which starts at block 9,398,428. Its
registration window is blocks 9,384,028 to 9,391,228, and every gate passes by
block 9,384,028. The chain itself takes a registration for epoch 3 until
block 9,348,027; the plan's window is the earlier one.

Nothing is lost before step 15: "If it fails, nothing has changed, and E_s
moves" (§12.1 step 5). What steps 5 to 12 put in place stays as section 6.1
describes it.
