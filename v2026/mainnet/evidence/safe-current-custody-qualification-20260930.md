# Safe current-policy custody qualification

**Scoped qualification complete.** Eight new and thirty-one adjacent roots pass
normal/race, for 78 positive root executions. All eight normal and exactly five
selected race controls are causal. Exact source/module/local-dependency evidence
is sealed and independently audited. This qualifies signed local policy custody,
without policy approval, a public import route or submission activation.
Public submission remains closed.

Frozen implementation is `3f88a9484c639e999081cf8a2b8a8f2cf14ed536`, tree
`c4cf3de3ad382b6894e4bb9081f4120e13a06458`, based on qualified shared
`af501f6ea21113af81393f3174f0babda2526058`. The implementation worktree is
`/home/by/urnetwork/sn-successor-safe-current-custody-owner-20260930`.
Documentation composition preserves every non-Markdown byte of the frozen
source, all module files, the SDK oracle and earlier qualification receipts.

## Property and limits

The [proposal](../BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CUSTODY.md) adds a separate
domain-signed acceptance around the current-only signed proposal. The original
independent approver signs both. Exact predecessor, distinct evidence and
monotonic retained runtime-prefix requirements preserve the immutable original
execution and history statements. Old omitted event fields retain their original
bytes and seals. Current proof never claims complete history was proved.

Counted attempts name the exact completed policy revision. Terminal outcomes
keep the policy of their counted attempt even if a later revision arrives.
Original Safe/relayer signatures, nonce claims, attempt counts and maximum
liabilities cannot be reset by imports. Hash-bound partial stages fix one
acceptance, including an empty stage; publication failure closes the owner.
Interrupted counted attempts remain consumed, while partial outcomes block new
runtime/policy imports until canonical recovery. Reopen and live checkpoints
reauthenticate the complete journal and reject lost, forked or changed authority.
This remains local file custody, without an external monotonic rollback log.

This slice has no public import flag or production current-policy capability.
Both generic-owner and direct-adapter writes refuse such custody. Historical
reconciliation remains available. A separately qualified native capability and
explicit approval/route installation remain P0 gates; the complete-history
interface and signed original statement are not weakened. Automatic compatible
runtime admission is also still open: signed additive revisions are incremental
authority, not automatic runtime compatibility.

## Author handoff

Astra max authored implementation, debugging and compile-only/vet checks. Sol
medium owns behavioral qualification. Formatting, compile-only and vet pass;
all eight isolated mutations also compile and pass vet. No author behavioral
test, live RPC, real signature or transaction was performed.

The handoff is `/tmp/safe-current-custody-handoff-20260930`. Its immutable
14-file `PAYLOAD.SHA256SUMS` hashes to
`5a778e42e1e45dd1111bfd6004de5d4396bb1777df2a42aa94af99fa2b705788`.
The supplementary 32-file `SHA256SUMS` hashes to
`26aafa1ed1d3e8c080dfb79faa9604a1ad6a07f72be27f0d711fa217aa292767`.
`CONTROLS.json` hashes to
`31a91fa8e56d5b7e84675ad2a92ba3fec15eeaf01a55eb2248a087a7a9a933c7`;
`TEST-PLAN.json` hashes to
`feab9db105d26e3ba7672fde1376078cd4a8af0563ec2e77eb46f9aa3dadb547`;
`SOURCE-FENCE.json` hashes to
`61b0818629d18b1e6a603879169d2d5c36644b4a78d0084d14a1a18921309b6a`.

## Independent sealed matrix

Raw evidence is under
`/home/by/urnetwork/temp/safe-current-custody-validation-3f88a948`.
The final `manifest.json` hashes to
`6df25096fcb63909481a1d6a7a509bea8f0f7c4ce3f4d0b83ff7101a87a65d49`.
Its 26-file `SHA256SUMS` hashes to
`037ee16b48226d8a0b1b88c149271d8c6261617cbe750ad461ec0ccbb6eb1e25`;
every checksum passes. The independent read-only author audit is
`/tmp/safe-current-custody-final-seal-audit.json`, SHA-256
`179c0c185d5d2610edaf77e2598897a53e16199c194ab7c8c66032dae172676f`.
It checks all raw positives and controls, source/modules/six local replacements,
and the immutable static and supplemental author handoffs.

The completed matrix is eight new roots, thirty adjacent light roots and one
full original-graph canonical adapter root, each normal and race: 78 positive
root executions. Every stream has exact slash-free top-level RUN/PASS census and
package PASS, with no failure, skip, build, panic, timeout or race report.
Environment is `GOMAXPROCS=2 GOPROXY=off`,
`-p 1 -count=1 -v`; each group keeps its separately bounded harness timeout.
Production retry windows are unchanged.

| Stream | Roots | Package PASS | SHA-256 |
| --- | --- | --- | --- |
| `focused-normal.log` | 8/8 | 21.083s | `931129506ee3a1d46202d313ee70c3003d0912e5ed9e2525ca4d2ce1edd5ce88` |
| `focused-race.log` | 8/8 | 98.254s | `63d752d0c22e4362a1f2616814dd52c5b2479d1fc197c2b5eb184f90f11df16f` |
| `adjacent-adjacent-custody-light-normal.log` | 30/30 | 93.722s | `cf777c340e0479418e8298a859c843dbc2650d12717dedfa401fff5a335a9c8e` |
| `adjacent-adjacent-custody-light-race.log` | 30/30 | 485.235s | `7620ab77dc8a727b23d938128b646631c932e13e25a1fcd6595ce93f3ff8a982` |
| `adjacent-adjacent-canonical-adapter-normal.log` | 1/1 | 49.048s | `953c5564adc6004323f6580cbde3698ef779c9de343063d77e7ece2ea35d41bf` |
| `adjacent-adjacent-canonical-adapter-race.log` | 1/1 | 306.293s | `a397f555155ab89ac74fc4645ab2e8921bf55961249e20b002d4eb248fb0533d` |

| Control | Barrier | Normal | Race |
| --- | --- | --- | --- |
| `acceptance_signature` | Separate independent acceptance signature. | Causal | Causal |
| `acceptance_predecessor` | Exact immutable signed predecessor. | Causal | Not selected |
| `counted_policy_reference` | Every counted policy authority remains retained. | Causal | Causal |
| `partial_policy_attempt` | Partial policy blocks a counted attempt. | Causal | Causal |
| `pending_policy_outcome` | New authority cannot change partial terminal recovery. | Causal | Not selected |
| `exact_outcome_policy` | Outcome retains the exact counted policy. | Causal | Causal |
| `policy_history_snapshot` | A live owner detects removed authority suffixes. | Causal | Not selected |
| `unavailable_policy_capability` | Signed custody cannot supply a send capability. | Causal | Causal |

Exactly eight normal and five selected race controls completed. Each reached
its named assertion, selected root/package FAIL and process exit one,
without a build, panic, timeout or race confounder. No result is claimed for the
three unselected race mutations. Integration preserves the exact qualified
non-Markdown source and earlier immutable receipts. A concrete native capability,
explicit policy approval and qualified public-route installation remain separate
gates; original historical truth is not established by this custody increment.
