# Closed managed-start readiness failure

Strict resume session 92055 failed at 2026-09-16 10:20:25 UTC with
`release topology semantic readiness timeout: every validator must complete a fresh verified trail through every operator`.
Its unchanged executable was built from SN
`aeda6abbd2dc0abc92bb0f60975cf89b509e8017`. This note diagnoses that closed
attempt; it is not a later acceptance receipt.

The owned-LAN startup selected five minutes solely from its RPC route. Both
validators started at 10:15:04 and were still inside required retained-history
authentication when their supervisor stopped them. Validator 1's exact fresh
stderr identifies complete terminal replay, compact settlement NO 2.
Validator 2's exact fresh stderr identifies ordinary NO 1 replay through
origin 1, including a local metadata artifact read. Their context/termination
and incomplete-EOF messages occur in the cancellation tail. The log gate's
last scan at 10:20:10 saw only 302 bytes of ordinary environment initialization
per validator and no blocking findings; it did not cause this failure.

The source orders these operations explicitly: retained semantic startup at
[release_run.go:686](/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/validator/release_run.go:686)
must succeed before operator startup at line 713 and worker creation at line
735. The retained reader starts at
[release_startup_v2.go:54](/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/validator/release_startup_v2.go:54).
The observed ordinary and terminal error contexts come from
[release_startup_history_v2.go:294](/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/validator/release_startup_history_v2.go:294)
and line 378. Thus the absence of new proofs is consistent with the stopped
pre-worker phase, without treating authenticated history as unnecessary.
There is no retained evidence that thirty minutes will suffice; the correction
must still be exercised on this representative history and resource load.

The four proof domains did not advance: validator 1 had 16,698 and 16,464
complete rows, validator 2 had 11,740 and 11,785, all last written September 12.
The independent recovery census owns all transaction/queue reconciliation.
This note does not claim the rest of the managed topology made no chain writes.

The narrow correction gives retained strict-history startup the existing
thirty-minute cap on every RPC route. It leaves fresh ordinary LAN startup and
post-tournament freshness at five minutes. Both launch paths already share
[process.go:867](/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/sim-testnet/process.go:867)
and line 891's selector; the later tournament wait is independently fixed at
line 570. Four fresh proof domains, exact generation, every child healthy,
zero strict restarts and a clean log gate remain required. No per-request
deadline, history signature check, relay capacity, first-native-epoch rule or
campaign window is changed. Deterministic tests also cover cancellation before
admission; a canceled invocation must not pass from an already-ready snapshot.

## Exact closed slices

The two included stderr files were copied byte-for-byte from Terra's CLOSED
private evidence directory:
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r3/strict-resume-failure-evidence`.
They were manually reviewed before copying: their only payloads are ordinary
empty environment settings, public local artifact locators, and startup
cancellation errors. They contain no signed transactions, private keys,
credentials, calldata, or database/config contents. The larger private source
logs remain private. No new extraction from active runtime files was performed.

`CURSOR-SLICE-SUMMARY.tsv` preserves source inode, exact initial/gate/EOF byte
boundaries, captured lengths, hashes and stable before/after metadata. The
validator 1 slice is 739 bytes, SHA256
`4055800c9b5f011f3b57c1763464093c88b3d55070f701da2f0e69c583e65fd4`;
validator 2 is 695 bytes, SHA256
`5560358baab4b64c34aeb3a35061f76d5488dc4afd559d3098fddf88f8e9df23`.
They include respectively 437 and 393 bytes beyond the last gate scan.
The zero-length stdout rows in that original summary remain descriptions of
the private originals; no stdout payload is needed here. Generation metadata
binds supervisor PID 3676320/start ticks 184001647 and both stopped validators
with zero restarts. Original source-file hashes retain their original absolute
path provenance and are not represented as a portable payload manifest.

## Existing provisional route

It cannot preserve this failed generation: all children are stopped, while
[prepareProvisionalLiveTopology](/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/sim-testnet/provisional_live_resume.go:75)
requires an already-live exact supervisor to avoid rebuilding/replaying it.
Strict-history flags explicitly reject provisional resume at
[strict_history_adoption.go:70](/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/sim-testnet/strict_history_adoption.go:70),
and this continuation's startup requires that exact strict history at
[evidence_relay_continuation_startup.go:51](/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/sim-testnet/evidence_relay_continuation_startup.go:51).
Existing live provisional adoption records waived fresh-proof startup and
publication, always with final acceptance false. It provides no supported
shortcut for authenticating this stopped strict continuation before selecting
a new end block. Returning to strict acceptance would still require its
ordinary history and live campaign checks. No provisional behavior is changed
by this patch.
