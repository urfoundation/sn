# Version 2: handoff to the deploying agent

Written 2026-10-13 at about 23:30 UTC, when deployment passed to another
agent. Updated 2026-10-14 at about 01:15 UTC: sections 1 and 2 follow the
merges since, and items 2 and 3 of section 3 follow the runbook. Updated again
at about 03:45 UTC: sections 1 and 2 follow the merges since, and item 7 of
section 3 says who runs the release gate and what stops it on goofe today.
[VERSION2-LAUNCH.md](VERSION2-LAUNCH.md) is the runbook and
[VERSION2-FINAL.md](VERSION2-FINAL.md) the plan of record; both win over this
note. This note says what is on `main`, what is still landing, and the facts
found during the last day that the deployer must act on.
[VERSION2-STATE.md](VERSION2-STATE.md) is the earlier orchestration log.

## 1. What is on main

Heads on 2026-10-14 at about 03:40 UTC:

| repo | main |
| --- | --- |
| sn | the commit that updates this note, on `839c906b` |
| server | `10b0bd3f1` |
| sdk | `1ee41b40` |
| connect | `8d22832d` |
| proxy | `ada3aac` |
| localizations | `3a1e7ca` |
| apple | `edcd545e` |
| android | `0a3a0030c` |
| windows | `fac2afd` |
| linux | `06717d7` |
| mmm (ur.io) | `ec4760983` |
| web | `9526a97b` |
| warp | `99e01ec` |
| xops | `2f2af94` |

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

Release gate repairs:

- preflight module classification and tidiness;
- the 19 stale census pins (the native-stake startup pin follows the current
  code, by the owner's decision);
- the Solidity coverage and static jobs;
- the test vault guard under `GOTMPDIR`;
- the selection of the v1 close floor's tests and of `sn25golive`'s (sn
  `6caf9667`), and of the deposit floors' and the pending reservation's
  (sn `839c906b`).

Release builder: an explicit `dependency_mode`.

- `local` uses the working checkouts. This launch uses it.
- `pinned` is the build version, made with
  `scripts/mainnet-release-build/pin.sh`.

## 2. Still landing on main

This session keeps merging these and pushes each as it verifies:

1. **The release gate's failing tests**, each on a branch. All but the last
   are root-caused. Apart from the monitor's missing schema contracts for
   migrations 833 to 835, a monitoring gap, none so far is a fault in what
   ships.
   - sn `v2/gate-mainnet-tests`: 13 `TestBootstrap*` tests and
     `TestIdentityFinalityClosingOutageRetainsTransportCause` in `mainnet`,
     stale since the custody, census and classifier commits of 2 to 4
     October.
   - sn `v2/gate-semantic-outputs`: 11 tests of sim-testnet's release
     evidence, the three `TestProduceFinalSemanticOutputs*` among them.
     Upstream `9b386fe8` changed the validator's pool-weight formula and the
     whitepaper's §8.1, but not sim-testnet's verifier, builder and fixtures.
   - server `v2/gate-server-tests`: two reserved-upload tests (a stale
     SCALE-hash fixture), a verify-simulation test (stale since the SEED
     replay of 4 October), the proxy test's GeoLite fixture, the monitor's
     schema contracts for 833 to 835, and the monitor tests' time budgets;
     with an sn patch that moves the DB-backed tests out of the server-unit
     job and into server-db.
   - sn `v2/gate-miner-tests`: `TestClaimClockRestartRetainsUnfinalizedSignedReceipt`,
     and the `miner` package outrunning sn-core-race's default 10-minute
     clock under race.
2. **A lost last signature is rebroadcast** (server `v2/gas-floor-3`). In a
   narrow case that predates this work, the root key stays blocked.

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
   - The source freeze requires gvisor cloned from
     `github.com/urnetwork/gvisor`, with that remote, on its `go` branch at
     `c0783dba`.
7. **The release gate (step 4).**
   - This session runs the official gate on goofe, on the frozen heads, when
     the owner calls the freeze. It fast-forwards every release checkout on
     goofe to its upstream, refreshes the release lock as the last sn commit
     (`sim-testnet/README.md`, "Atomic release-lock refresh"; the owner has
     authorized it), pushes it, and starts the gate alone on the host. The
     deploying session does not run a second gate there.
   - From the freeze until the gate reports, nobody pushes to sn `main`: the
     gate requires the sn checkout at exactly `origin/main` when it starts and
     again when it ends. The other release repositories may advance upstream,
     but their checkouts on goofe must not change.
   - The workspace needs the vault and xops checkouts, GitHub, the Go proxy and
     the LAN node. Every release repository must be clean and on the branch
     the source freeze names.
   - **The runtime-metadata preflight cannot pass on goofe today.**
     `scripts/check-runtime-metadata-artifacts.sh` reads
     `http://192.168.1.162:9944` and requires a Bittensor testnet archive
     there (genesis `0x8f9cf856…`). Snow now runs a Finney archive (xops
     `main/ansible/host_files/snow/subtensor/README.md`), so the address
     answers with mainnet's genesis (`0x2f0555cc…`, runtime 478), and the
     strict gate stops at that preflight. The owner decides how the gate
     attests the testnet runtime metadata from now on.
   - The binding-toolchain and runtime-source preflights pass on goofe at sn
     `8381bb59`, and the xops job passes at xops `770fffc`.
   - The tests the gate still fails are in section 2.
8. **The schedule.**
   - E_s is epoch 2, which starts at block 9,297,628, about 2026-10-16 18:10
     UTC.
   - The plan's registration window is blocks 9,283,228 to 9,290,428.
   - The six departures in the runbook's section 9.3 need the owner's explicit
     acceptance. If the window is missed, E_s moves to epoch 3.
9. **The safeguard's first read.** The activation record arms on the first
   successful registration read. Check it with `sn-transition-status` or
   `st-check --v2-activation` after step 15, and never remove the v2 addresses
   after it.
