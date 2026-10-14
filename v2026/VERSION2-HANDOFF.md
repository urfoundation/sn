# Version 2: handoff to the deploying agent

Written 2026-10-13 at about 23:30 UTC, when deployment passed to another
agent. [VERSION2-LAUNCH.md](VERSION2-LAUNCH.md) is the runbook and
[VERSION2-FINAL.md](VERSION2-FINAL.md) the plan of record; both win over this
note. This note says what is on `main`, what is still landing, and the facts
found during the last day that the deployer must act on.
[VERSION2-STATE.md](VERSION2-STATE.md) is the earlier orchestration log.

## 1. What is on main

Heads at handoff:

| repo | main |
| --- | --- |
| sn | the commit that adds this file, on `60be2538` |
| server | `c2567a6c2` |
| sdk | `5ff17c06` |
| connect | `f7318bca` |
| proxy | `ada3aac` |
| localizations | `3a1e7ca` |
| apple | `70f3ce7a` |
| android | `a46813ea5` |
| windows | `e549203` |
| linux | `3332164` |
| mmm (ur.io) | `6dc2fced2` |
| web | `9526a97b` |
| warp | `99e01ec` |
| xops | `770fffc` |

Settlement v2 itself:

- the contracts (`evm/src/STSettlementVault2.sol`, `STSettlement2.sol`,
  `STClaimBatcher.sol`, `LedgerTree.sol`);
- the server's driver and readers, with migrations 821 and 822;
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

Gas floors for runtime 478. Each is capped at the operator gas policy's
`maximum_gas`, so a floor never causes a refusal by itself.

| Sender | Floor | Where |
| --- | --- | --- |
| v2 close | 3,200,000 | server |
| v1 close | 3,200,000 | server |
| v2 claim | 2,600,000 | SDK |
| v1 claim | 2,700,000 | SDK |
| claim tools | 2,600,000 | sn |
| claim gas passed to the wallet | (the SDK's) | ur.io |

The server activation record (SV-9, migration 832): once the server observes
the pool's registration, the st subsystem refuses to run without the
recorded v2 address pair. `st v2-activation` shows and clears the record.

Release gate repairs:

- preflight module classification and tidiness;
- the 19 stale census pins (the native-stake startup pin follows the current
  code, by the owner's decision);
- the Solidity coverage and static jobs;
- the test vault guard under `GOTMPDIR`.

Release builder: an explicit `dependency_mode`.

- `local` uses the working checkouts. This launch uses it.
- `pinned` is the build version, made with
  `scripts/mainnet-release-build/pin.sh`.

## 2. Still landing on main

This session keeps merging these and pushes each as it verifies:

1. **Dual-stack validator hops** (sn, server, connect, sdk; all four
   together). Two conflicts with new upstream work are being resolved:
   - on sn, the per-hop contract flush in `f5cb384e`;
   - on server, the chunked proxy-egress refresh in `0b2a51cea`.

   Deploy order once merged: API and taskworker, then connect, then
   validators. `hop_latency_from_attributed_attempt` stays off until every
   validator runs the relaxed receipt check.
2. **The root-key stranding fix** (server). Today three reverted closes of
   one logical key strand the operator root key: the fourth attempt reserves a
   nonce the policy will not sign, and every later v1 and v2 send stops. Until
   the fix lands, watch the log lines in the runbook's section 5.
3. **Floors for the deposit-key stake moves** (server), with the same cap.
4. **A last runbook round** covering migration 832 and the activation
   record's texts and commands, the capped floors, the native-fee policy note
   below, and the Solidity and guard repairs.

## 3. Facts the deployer must act on

1. **Mainnet runs runtime 478** since block 9,276,925 (2026-10-13 21:08:36
   UTC); `:code` BLAKE2b-256 is `0x7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1`.
   - Step 8's phase config names source `850f2755bb05a3851f948489f0ab27b0645cebce`
     at version 478. `sn-mainnet` refuses any source named for another
     version.
   - Repeat step 2 immediately before steps 8 and 15.
2. **Re-sign the operator gas policy (step 5) before switching the server to
   v2 (step 11).**
   - `maximum_gas` must be at least 3,200,000; 3.5M is planned.
   - `maximum_intent_liability_wei` must be at least 3 × `maximum_gas` × the
     fee cap: 3.15·10^17 wei at 3.5M gas and 30 gwei.
   - Under today's 1M policy the floors sign closes at 1M, and nothing new is
     refused.
3. **Re-sign the native-fee settlement policy for runtime 478.**
   - The policy binds the runtime code hash and a native block range
     (`model/st_native_fee_settlement_model.go`, `policy.RuntimeCodeSha256`).
   - Statements executed since block 9,276,925 are refused as "outside its
     approved denomination" until the policy names 478.
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
   - The workspace needs the vault and xops checkouts, GitHub, the Go proxy and
     the LAN node. Every release repository must be clean, on a branch
     tracking `origin/main`, and at exactly `origin/main`.
   - Refresh the release lock as the last commit after the freeze
     (`sim-testnet/README.md`, "Atomic release-lock refresh"). The owner has
     authorized it.
   - Still failing on pristine main, for an unfound reason:
     `TestProduceFinalSemanticOutputs{WritesOneSealedPair,ScansBeforeCreatingFiles,ScannerCannotMutateVerifiedBytes}`
     ("measurement-derived pool decision no_id 1 differs"), in sn-all-normal
     and sn-simulator-race.
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
