# Passive observer continuation checkpoint — October 2, 2026

An exhausted temporary preparation read must consume only its current signed
sample. The rendered passive unit uses `Restart=no`; returning from its process
after that sample abandons the remaining finite sample allowance. A service
restart policy cannot be assumed to restore it. The observer must retain its
original preparation and checkpoint owners, report an unavailable sample without
fresh readiness or accepted finalized progress, and continue at the signed
interval. Proven custody loss, RPC integrity and uncertain checkpoint writes
remain distinct terminal conditions for the affected owner.

This is a source and evidence checkpoint. The new observer successor and complete
SN/server composition are still under qualification. Existing release
`258e25b4` / server `0aa1e244`, prior component receipts and live authority gates
remain unchanged; no signing, broadcast, deployment or start is authorized.

## Earlier observer scope and newly reproduced failure

The [byte-identical author receipt for `653061a1`](durable-observer-653-author-20261002.json),
SHA-256 `bd5a0172c2c00e1c08527ca2b0685549b90617648ef53ef1f0639908c6a700da`,
binds 49 retained evidence/tool/module files. Its 11 selected roots pass normal
and race, and all three package vets pass. Four no-retry controls and one joined
error-classification control fail as intended in each mode. The latter is an
already-classified joined-error invariant, not a claimed mixed kernel failure.
The receipt records `PASS_SCOPED_WITH_CONFIRMED_CONTINUATION_GAP`, not a complete
observer or deployment qualification. It also identifies the historical command
record's fixed overlay-hash field; the actual distinct overlay maps and bodies
are separately bound. These records were not rewritten.

Test-only `81a492bb` leaves production `653061a1` unchanged and supplies an actual
public, synthetically signed two-sample passive service. All four controls fail
in normal and race: unavailability before RPC, unavailability after a complete
RPC preview, inter-sample cancellation, and inter-sample custody loss. The
original production loop returns after exhausting the first sample instead of
reaching those later signed boundaries.

The implemented `97c7ae85` successor emits a bounded `storage-unavailable` event
and numeric metrics status 8, with no fresh observation or readiness. It retains
the same physical preparation/checkpoint descriptors and waits the signed
interval before the next sample. Known RPC integrity, finalized conflict and
checkpoint publication uncertainty cannot become soft continuation merely
because preparation observation is also unavailable. The adjacent service and
operator loops already continue ordinary read/output failures; exhausted storage
reconciliation owns different joined/uncertain-writer semantics and has not
been broadly relaxed.

The first `97c7ae85` author run passed vet and 16 of 17 normal roots. Its sole
failure was the after-RPC test's assumption that one complete preview makes one
finalized-head call: the actual preview performs several consistency reads.
That failed run remains retained, and its race phase did not start. Test-only
`6d398662` changes no production bytes. It measures one complete production
preview's method vector, captures the full locked census at the first injected
EIO, requires no change through the failed event, and requires exactly one
complete vector for the recovered sample. All 17 corrected normal roots and
three-package vet now pass; race, causal and independent qualification remain
pending at this checkpoint. Their terminal normal/vet results are retained at
`integration/evidence/observer-6d39866/normal.result.json` and `vet.result.json`
under the author workspace below. These partial results are not a complete
successor qualification.

Two separate qualification attempts encountered newly created temporary
ancestors with mode `0775`, so protected-path admission refused setup before
their intended causal bodies. These attempts remain setup failures, not product
regressions or successful negative controls. Qualification launchers must set
`umask 077` before creating task roots, temporary directories, fixtures and
capture files; validate the required physical ancestry before launching a
selection, and check each expected failure's actual cause. Correct only owned
scratch permissions and preserve the original refusal and retry records. The
observer author runner already sets this umask and uses its existing private
temporary root; future composed runners must retain that preflight explicitly.

The [terminal historical census](durable-observer-soft-census-20261002.json)
binds both four-failure baseline runs, the failed `97c7ae85` normal run and its
passing vet, including exact commands, logs, exits and source pins. Active
successor output is excluded from that seal. This census is an author evidence
readback, not an independent rerun.

## Separately qualified exclusive successor writer

The [byte-identical independent `f3c8a618` receipt](durable-successor-write-admission-independent-20261002.json),
SHA-256 `01dcdb4f250f62c3f798673b27ea26c15a8e2815a9325ce70d2bac81c5a532f5`,
binds SN tree `a8d3c6269e29d47e6f40e8daf96c6fd99a29bca6`, server
`1b7cc78b` and Connect `6cd720cf`. Eight new admission roots plus two typed
identity neighbors pass normal and race, and vet passes. The old three-file
overlay produces eight intended failures per mode; isolating the two old
publication files produces six per mode. Its public control proves shared-reader
refusal, joined exclusive resume and unchanged no-send authority.

That receipt does not inherit the observer's later module graph or qualify the
complete composition. The broader author normal scope remains 84 passes and one
legacy diagnostic-assertion failure; a separate 17-root race scope and vet have
terminated successfully. Test-only `33e57ee7` corrects that remaining expected
typed-identity diagnostic without changing production. Its focused gate is
separate. The original failed broad normal scope is not relabeled as passing.

The next composed source must preserve all existing qualified component bytes,
include all eight exclusive-writer controls plus the observer and affected
CLI/lifecycle controls, and obtain an exact independent composition receipt.
Consumer adoption of Connect `71df099c` then needs its own module join and actual
startup observation control. Provider progress, expected-provider monitoring,
fleet runtime capability continuation and the full
[38-requirement backlog](mainnet-implementation-backlog-20261002.md) remain active.

Raw evidence remains under
`/mnt/data/sn-testnet/mainnet-durable-volume-20261002/integration/evidence/` and
`/mnt/data/sn-testnet/sol-native-snapshot-independent-20261002/write-admission/`.
The copied small receipts preserve their original hashes; referenced raw logs
and source archives remain in those external retained workspaces.
