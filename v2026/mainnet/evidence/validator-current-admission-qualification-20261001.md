# Two-UR current-admission qualification — 2026-10-01

The bounded initial-start implementation is qualified at SN
`b8dc332aeb2d934a1aaa3de02ba2bbffc7f98627`, tree
`0fa0bd62c929601f6044d8ced0325cdb78013ddd`, with the integrated anchor producer.
The primary graph pins clean server
`0b8e758db9ce5516de867e1b5d0a1c9660a0880b`, Connect `e1b5d77b5029`, and SDK
`5d37be3876e5`. Go was `go1.26.6 linux/amd64`. The external modfile SHA-256 is
`15468b6155a96705befc581a69e3ce0d747b8e98dc49aa997dcef016a1f26818`.
Final positive runs use the integrated source without an overlay.

The [current-admission policy](../VALIDATOR-CURRENT-ADMISSION.md) remains a
separately signed input. No real approval, mainnet transaction, signer operation,
live installation or service start was performed. Root-service authority,
applied-weight influence and economic success remain unproven.

## Completed qualification

| Author selection | Normal | Race |
| --- | --- | --- |
| 12 new current-admission roots | 12/12, 96.890 s | 12/12, 690.388 s |
| 23 adjacent mainnet roots | 23/23, 182.283 s | Interrupted diagnostic; no package-pass claim |
| 12 producer roots | 12/12, 37.133 s | Not started by author; independently covered below |

Author `go vet ./mainnet ./validator` passed. Completed qualifying package runs
contain 47 normal and 12 race root executions, with no skipped or failed roots.
New tests cover exact two-role starts, independent policy/custody signatures,
absent/partial/changed anchors and mappings, post-sync reference/journal/claim/
clock/expiry/cancellation refusal, partial-pair restart, lost acknowledgements,
alternate envelopes, incomplete proof domains, consumed-state preservation,
public opt-in refusal and rehashed journal narrowing.

The author stopped the redundant adjacent race sweep at the parent's explicit
instruction after the main race gates passed. Six roots completed: actual anchor
public readback, EVM receipt scan floor, original role-plan binding, committed
checkpoint/composition roots, and native epoch denial. The seventh root, native archive/
current-epoch observation, was interrupted by SIGTERM of the local test process.
The raw package exit is 1 with `signal: terminated`; this is an incomplete
diagnostic, **not a passing adjacent package**. No assertion had failed before
termination. The pre-stop copy and terminal stream are retained unchanged.

Independent Sol review found no blocker on the exact primary graph:

| Independent selection | Normal | Race |
| --- | --- | --- |
| Mainnet | 24/24, 242.883 s | All 12 new roots, 689.525 s |
| Producer | 12/12, 35.901 s | 12/12, 134.426 s |

Independent `go vet ./mainnet` passed. The 24-root normal selection includes the
production executed graph/substitution/public-start boundaries, committed
both-role composition, native/current epoch/runtime/rollback checks, and real
anchor readback. Controller tests use typed observation ports and a fake manager;
adjacent tests exercise concrete chain and proof readers. This is not a live
end-to-end deployment rehearsal.

Two omission controls failed causally in both normal and race modes, using exact
hashed overlays outside the frozen source. Bypassing post-sync complete admission
healed a deleted counted journal and issued both fake starts. Bypassing permanent
unit claims allowed a separately signed envelope with another journal path to
renew initial-start capacity. Corrected tests refuse both cases. Control package
times were 19.381/149.929 s and 6.467/37.799 s, respectively.

## Sealed evidence and separate dependency graph

Author evidence is under
`/mnt/data/sn-testnet/mainnet-validator-current-admission-20261001`:

- `receipt.json`: SHA-256 `d9ab2e0b1f4b200990617f8c567cb7c2ca2f7e32a2aeaf6ca9865369c1ded2ec`.
- `SHA256SUMS`: SHA-256 `fd5daf97b4f5042d6c366149696d0b1ad14312b6cb411e49a29000bca2aa5175`; all 81 entries verified.

The seal includes exact commands, selections, source/dependency pins, raw streams,
censuses, omission overlays, and preliminary failures. The initial journal-limit
and pre-first-weight majority errors were corrected before freezing; preliminary
anchor overlays are not substituted for final integrated-source qualification.

Independent primary evidence is
`/mnt/data/sn-testnet/sol-validator-current-independent-20261001/primary-receipt.json`,
SHA-256 `697ec75f7eeec05e3cb41b44340de85bfa446fcc0ee51bcbaa1101765b773408`.
Its manifest SHA-256 is
`816dfff4b1cbb94ad3ce385a5bff7a082663debe2ce84aec9fbd8c2a7b857853`.

A **separate bounded compatibility run** used the same frozen SN source with
server `720e7c61182983dd2cd6de667787bb5b52f4d8a4`: full SN `go build ./...`,
`go vet ./mainnet ./validator`, five focused normal roots and three focused race
roots passed. Its independent `server720-receipt.json` in that same evidence
directory has SHA-256
`5592356e4bd08ca522171ac5f5bb95e454f4213c9fe875393f121d8f81cae877`; manifest
SHA-256 `d66289fb5e9ff720bc31c78ad068c81c90f74f2bdb1800a6f89192a1b46c17f8`.
This is not a repeat of the full primary selection or acceptance of schema 750,
subscriber-v2 policy, migration rollout or live service behavior.

Live launch still requires exact installed and producer evidence, independent
current-policy and lifetime signer/host-custody acceptance, original finite native
and process windows, explicit Recycle mode, and actual host/systemd qualification.
Only complete authenticated empty tails are in scope; unfinished work needs
separate recovery authority. The controller grants no root-service, stop/restart
or transaction authority, and no retained report alone can open a start.
