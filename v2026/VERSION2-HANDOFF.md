# Version 2: handoff to the deploying agent

Written 2026-10-13 at about 23:30 UTC, when deployment passed to another
agent. Updated 2026-10-14 at about 01:15 UTC: sections 1 and 2 follow the
merges since, and items 2 and 3 of section 3 follow the runbook. Updated again
at about 03:45 UTC: sections 1 and 2 follow the merges since, and item 7 of
section 3 says who runs the release gate and what stops it on goofe today.
Updated again at about 06:30 UTC: section 1's heads, fixes and gate repairs,
section 2's branches, and items 6, 7 and 8 of section 3 follow the owner's
decision to run the gate in its local mode and the move of E_s to epoch 3.
[VERSION2-LAUNCH.md](VERSION2-LAUNCH.md) is the runbook and
[VERSION2-FINAL.md](VERSION2-FINAL.md) the plan of record; both win over this
note. This note says what is on `main`, what is still landing, and the facts
found during the last day that the deployer must act on.
[VERSION2-STATE.md](VERSION2-STATE.md) is the earlier orchestration log.

## 1. What is on main

Heads on 2026-10-14 at about 06:25 UTC, each read from its `origin/main`
(gvisor: its `go` branch):

| repo | main |
| --- | --- |
| sn | the commit that updates this note, on `aaec4175` |
| server | `81484f5b1` |
| sdk | `0110c1ca` |
| connect | `1e7f92c0` |
| proxy | `ada3aac` |
| localizations | `3a1e7ca` |
| apple | `edcd545e` |
| android | `5ebbd7285` |
| windows | `fac2afd` |
| linux | `06717d7` |
| mmm (ur.io) | `6500bf924` |
| web | `9526a97b` |
| warp | `99e01ec` |
| xops | `a0c2727` |
| gvisor | `c0783dba` |

Settlement v2 itself:

- the contracts (`evm/src/STSettlementVault2.sol`, `STSettlement2.sol`,
  `STClaimBatcher.sol`, `LedgerTree.sol`);
- the server's driver and readers, with migrations 821 and 822, and the
  activation record, with migration 832;
- the validator's audit;
- the claim tools;
- the SDK, and every app.

Network payout consent is in the SDK, all five apps, ur.io and the
localization store.

Deploy tooling:

- `apply --action vault2-create` prints the escrow envelope;
- `sn-mainnet register-operator-pool review|readback` builds, preflights and
  reads back the registration;
- the examples and the fill recipe are in `mainnet/`
  ([SETTLEMENT-V2-FILL.md](mainnet/SETTLEMENT-V2-FILL.md),
  [SETTLEMENT-V2-EXAMPLES.md](mainnet/SETTLEMENT-V2-EXAMPLES.md),
  [SETTLEMENT-V2-ADOPTION.md](mainnet/SETTLEMENT-V2-ADOPTION.md)).

Runtime:

- reviewed sources for runtimes 477 and 478;
- audits in `docs/spec/runtime-477-audit.md` and `runtime-478-audit.md`;
- the rehearsal pinned at 478. R-1 to R-7 pass on 475, 477 and 478 with the
  catalogue's bytecode.

Gas floors for runtime 478. The server caps each of its floors at the gas
the operator gas policy still accepts at the attempt's fee cap: its
`maximum_gas`, and what the operation's and the lifetime liability still
cover once the other nonces' charges are taken off (server `10b0bd3f1`). So a
floor never causes a refusal by itself. No policy stands above the claim
floors.

| Sender | Floor | Where |
| --- | --- | --- |
| v2 close | 3,200,000 | server |
| v1 close | 3,200,000 | server |
| deposit, on the deposit key | 3,400,000 | server |
| the deposit's staging, on the deposit key | 2,800,000 | server |
| v2 claim | 2,600,000 | SDK |
| v1 claim | 2,700,000 | SDK |
| claim tools | 2,600,000 | sn |
| claim gas passed to the wallet, v1 and v2 | (the SDK's) | ur.io |

ur.io passes the SDK's gas with v1 claims too since mmm `37859e302`, and
refuses a claim without it. The apps suggest a v1 gas top-up of 0.02 TAO
(apple `edcd545e`, linux `06717d7`, windows `fac2afd`, android `0a3a0030c`).
Linux and Android offer a claim from 0.015 TAO; Windows holds Claim only for
an empty gas key and leaves the rest to the SDK.

The server activation record (SV-9, migration 832): once the server observes
the pool's registration, the st subsystem refuses to run without the
recorded v2 address pair. `st v2-activation` shows the record, and
`st v2-activation-clear` clears one that names the wrong pair.

The server's database head is 835. Upstream added migrations 833 to 835
after the activation record's 832, and `bringyourctl db migrate` applies
them all (runbook step 6).

The root-key stranding fix (server `8d63033c9`, merged as `4e5759875`):
under a gas policy a new nonce is reserved only together with its first
attempt's gas reservation. So an attempt the policy refuses takes no nonce,
and the key's later sends go out.

- The policy still refuses that operation's further tries. Three reverted
  closes of one epoch spend the close's three signatures.
- A key that a build before the fix stranded stays stranded. Server
  `docs/operator/st-stranded-nonce.md` is the procedure, and it wins where the
  runbook's section 6.5 differs:
  1. stop every send of the account until step 5: stop the taskworkers, and
     run no `bringyourctl st deposit`, `commit` or `finalize`;
  2. confirm that the intent is stranded, and that the account's nonce at the
     finalized block is the intent's;
  3. from outside the engine, send a 0-value self-transfer of the account at
     that nonce;
  4. wait until it is finalized;
  5. start the sends again.

  Never delete or edit rows. The log lines are in the runbook's section 5.
- A root-key execution whose gas was reserved, but which was not yet signed
  when its v1 close expired or its v2 operation was spent, is now signed as
  reserved: it reverts once and frees the nonce (server `b7371dc10`).
- When a dead operation's cancellation cannot be signed, as when its
  signatures are spent, its latest signed attempt is broadcast again. That
  takes no new signature or liability: it reverts and frees the nonce, where
  before a lost broadcast left the account blocked (server `bac7e5770`,
  merged as `bfdc067ca`).
- Known limitation: on the deposit key, a deposit reserved but not signed
  before its deadline stays refused, and no operator step frees it. It is
  dormant while deposits are at zero price, since the deposit task then sends
  nothing. Fix it before deposits are priced.

The dual-stack validator hops, in all four repositories: server `72699c819`,
connect `8d22832d`, sdk `1ee41b40` and sn `00e383fe`. The launch's builds
carry them.

- Deploy order: the server's API and taskworker, then its connect service,
  then the validators.
- `hop_latency_from_attributed_attempt`, a server-only key of verify.yml's
  `settings`, stays off until every validator, third-party ones included,
  runs the relaxed receipt check.
- Roll back in the reverse order, that setting off first.
- sn `VALIDATOR.md`:812-828 ("Rolling deploy") and connect
  `IPV6.md`:332-341 describe it; the runbook's section 6.3 places it in the
  launch.
- A tunnel connection that a reset leaves without a remote address no longer
  ends the validator. The hops' family check called a method on that nil
  address, on net/http's dial goroutine. It now reads the family as unknown
  (sn `ee56449b`, merged as `aaec4175`).

Release gate repairs:

- preflight module classification and tidiness;
- the 19 stale census pins (the native-stake startup pin follows the current
  code, by the owner's decision);
- the Solidity coverage and static jobs;
- the test vault guard under `GOTMPDIR`;
- the selection of the v1 close floor's tests and of `sn25golive`'s (sn
  `6caf9667`), and of the deposit floors' and the pending reservation's
  (sn `839c906b`);
- the gate's local mode, `RELEASE_GATE_DEPENDENCY_MODE=local` (sn
  `53491e14`);
- `mainnet` tests stale since the custody, census and classifier commits:
  27 bootstrap and EVM tests, eleven original-custody tests, two read-failure
  tests and the native fee export test (sn `3dd174fc`);
- sim-testnet's release evidence, priced at the conviction-zero tier, which
  fixes the three `TestProduceFinalSemanticOutputs*` (sn `692022ac`);
- the server unit job's database-backed tests, moved into server-db (sn
  `8e0d4f26`);
- the stmonitor fixture's mainnet roots, on the gate's private PostgreSQL,
  and server-db's 30-minute deadline for the ledger v2 commands (sn
  `e7cb82b4`);
- the `miner` tests, in two rounds, and sn-core-race's 120-minute deadline
  (sn `2573bba1`, `54a06100`, `d3545d94`);
- the server's: the reserved-upload tests' SCALE hashes, the assignment
  filter test's seeds, the proxy test's synthetic GeoLite2 database, the
  monitor's schema contracts for migrations 833 to 835, its catalog
  fixtures' deadlines and the gate PostgreSQL's statement text (server
  `6fafc66f6`), and the partial-index guard fixture's deadline (server
  `666940321`).

Release builder: an explicit `dependency_mode`.

- `local` uses the working checkouts. This launch uses it.
- `pinned` is the build version, made with
  `scripts/mainnet-release-build/pin.sh`.

## 2. Still landing on main

This session keeps fixing what the release gate still fails, each fix on its
branch, and merges each as it verifies. The state of each branch at about
06:30 UTC:

1. sn `v2/gate-mainnet-shards`: the `./mainnet` package sharded on tmpfs
   into 8 jobs. Two commits so far. `ec3f3d80` makes local mode skip, in
   every job, the two sim-testnet tests that only a sealed run passes, and
   print each local skip with its reason before it starts. `e07be0a4` gives
   sn-v2's selected v2 suites an explicit 30-minute deadline. On `main`,
   `TestReleaseLockMatchesCheckout` still runs in sn-simulator-race, and
   `TestRepositoryDiscoveryUsesModuleIdentity` stats the workspace vault's
   `main/st.yml`. The sharding has no commit yet.
2. sn `v2/gate-mainnet-tests`: ten more `mainnet` test fixes, to
   `40e92be8`, beyond the round merged as `3dd174fc`.
3. server `v2/task-finalization-session`: the task runner's session race, in
   one commit, `8a4e335e5`. A Drain that lands during a Run loop's claim
   refill closes the loop's database session, so the loop's owned
   finalizations are lost and their rows run again after their five-minute
   lease. It is a production bug that repeats task execution. The claim
   fence and the task bodies' checks of their applied state keep any effect
   from applying twice.
4. server `v2/proxy-synctest-wireguard`: the proxy tests' synctest crash, in
   one commit, `6a1efd2cd`. Two WireGuard tests made their proxy inside a
   synctest bubble, so its peers' queues were bubble channels. A finalizer
   outside the bubble drains them, which crashed the whole package. The proxy
   and its peers are now made outside the bubble.
5. sn `v2/gate-sim-tests`: sim-testnet's remaining tests, five commits, to
   `17c1e970`. The last fixes the crv4 regression from `65759103`: under a
   provisional runtime approval, the validator's strict stake and schedule
   reads at reviewed runtimes work again. Mainnet never enables provisional
   compatibility.
6. sn `v2/gate-utf8-parity`: the UTF-8 parity test, in one commit,
   `d300211c`. The test expected the `\ufffd` escape that `encoding/json`
   wrote before Go 1.27. Go 1.27.2's default encoder, jsonv2, writes each
   invalid byte as a literal U+FFFD. The test now derives the expected bytes
   from the linked encoder. The comparison itself was right.
7. sn `v2/semantic-evidence-gaps`: the semantic verifier's gaps, four
   commits, to `385596dc`, with its review on `v2/semantic-gaps-review`
   (`763fd96e`).

## 3. Facts the deployer must act on

1. **Mainnet runs runtime 478** since block 9,276,925 (2026-10-13 21:08:36
   UTC); `:code` BLAKE2b-256 is `0x7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1`.
   - Step 8's phase config names source `850f2755bb05a3851f948489f0ab27b0645cebce`
     at version 478. `sn-mainnet` refuses any source named for another
     version.
   - Repeat step 2 immediately before steps 8 and 15.
2. **Re-sign the operator gas policy (step 5) before the server build of
   step 6 runs, and so before switching the server to v2 (step 11).**
   - `maximum_gas` must be at least 3,200,000; 3.5M is planned.
   - `maximum_intent_liability_wei` must be at least 3 × `maximum_gas` × the
     fee cap: 3.15·10^17 wei at 3.5M gas and 30 gwei.
   - Under today's 1M policy the floors sign closes at 1M, and nothing new is
     refused.
   - `maximum_lifetime_liability_wei`: today's 10^19 covers a year of floored
     closes at a 30 gwei fee cap with about half to spare. Once deposits are
     priced, the floors of the closes, the stagings and the deposits need
     about 1.5·10^19 a year, so set at least 2·10^19.
3. **Re-sign the native-fee settlement policy for runtime 478.**
   - The policy binds the runtime code hash and a native block range
     (`model/st_native_fee_settlement_model.go`, `policy.RuntimeCodeSha256`).
   - Statements of the blocks that 478 executed, from block 9,276,926, are
     refused as "outside its approved denomination" until the policy names
     478.
4. **Deploy ur.io from mmm main** before any app release that carries consent
   signing. It carries the wallet bridge security fix (`64173fd13`), the
   consent bridge page and the claim gas.
5. **Build the apps.** No repository has CI.
   - Apple and Windows have never compiled the v2 and consent code.
   - Android's unit tests ran against a stub SDK; build its AAR from sdk main.
   - On Windows, confirm the long consent link opens in the https browser that
     holds Talisman, and that wallet sign-in still works (sign-in sessions are
     now final once cancelled).
6. **Step 1 and the release build** use Go 1.27.2 (`go.mod`) with
   `dependency_mode: local`.
   - The census includes connect, sdk and gvisor.
   - gvisor is a checkout beside sn, cloned from
     `github.com/urnetwork/gvisor` on its `go` branch, at `c0783dba`. The
     local gate inventories its module without checking its provenance, and
     the release build records its commit.
7. **The release gate (step 4)** runs in its local mode, on the latest `main`
   of every repository. The owner's decisions of 2026-10-14: "The gate should
   just run against the local checkout. We don't need to seal the version.
   That is just overhead we can avoid." And: "Pull the latest main code on
   all repos. That is what the gate should use. If it fails, fix, commit,
   merge, push, pull, repeat."
   - The command:

     ```bash
     RUN_SERVER_DB_TESTS=1 RELEASE_GATE_DEPENDENCY_MODE=local RELEASE_GATE_CONCURRENT_GATES=1 \
       ./scripts/test-release-1.0-local.sh
     ```

   - This session runs it on goofe, from worktrees at those heads, alone on
     the host. The deploying session does not run a second gate there. Each
     failure is fixed on a branch, merged, pushed and pulled, and the gate
     runs again until it passes (section 2). The heads it passes on are the
     launch revisions.
   - Local mode skips exactly the checks that seal a run: the source freeze's
     provenance checks, `TestReleaseLockMatchesCheckout` in sn-all-normal and
     as the final fence, and the runtime 454 source and runtime-metadata
     attestations, which read GitHub and the testnet archive node. So the
     release lock needs no refresh, and Snow's Finney archive at the testnet
     archive's address no longer stops the gate. It keeps the module
     inventory and tidy checks, the sourceguard and binding-toolchain
     preflights, patch hygiene, every test job and diagnostic mode
     (`sim-testnet/README.md`, the paragraph on `RELEASE_GATE_DEPENDENCY_MODE`).
   - Its scripts never touch a vault checkout. Until `v2/gate-mainnet-shards`
     merges, one test still stats the workspace vault's `main/st.yml`, and
     fails without it (section 2, item 1).
   - Local mode does not compare the checkouts again at the end, so nothing
     would catch a worktree that changes during the run. Leave them as they
     are until the gate reports.
   - A local pass ends with `[release-1.0] local release gate passed (local
     mode: working checkouts, not sealed)`. It qualifies those checkouts, not
     a sealed release. The pinned mode stays for the build pipeline.
8. **The schedule.** E_s is epoch 3 (the runbook's O18). The runbook's
   section 9 gives its blocks and dates, the registration window, the
   departures from the plan of record and the decision point if the window
   is missed.
9. **The safeguard's first read.** The activation record arms on the first
   successful registration read. Check it with `sn-transition-status` or
   `st-check --v2-activation` after step 15, and never remove the v2 addresses
   after it.
