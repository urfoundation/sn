# Passive root process repair

`root-passive` repair consumes a separately signed, single-incident start for an
already installed passive netuid-0 observer. The signature domain is
`urnetwork-mainnet-root-passive-repair-approval-v1`; the envelope schema is
`urnetwork-mainnet-root-passive-repair-v1`. Validator and initial activation
signatures do not authorize this operation.

The envelope pins the original passive host approval and its independent public
key, the original consumed host journal, the original monitor checkpoint bytes
and decoded record, and the exact previous process invocation. Its new state
path, validity window, one-start limit and observation limit are signed. Read
ownership defaults to 300 seconds and accepts explicit bounds of 60–900 seconds.
The approval window is at most 24 hours; observations are bounded to 1–1024.

The existing unit, binary, runtime, sandbox, machine, boot, original bootstrap
journals, durable-volume declaration and checkpoint remain in place. Repair
performs no installation, reload, stop, configuration generation, checkpoint
replacement, key enrollment, native transaction or budget renewal. Before a
start, the manager must report the approved stopped generation and its entire
cgroup must be empty. A shared unit lock excludes another cooperating action
owner. Two permanent generation markers prevent another approval or journal
path from obtaining a second allowance for that same process generation.

The start reservation is durably published before `systemctl start`. An
unacknowledged start remains held even if a running process later appears. An
acknowledged generation may continue bounded observations after reopening the
same repair journal. Completion requires fresh successful publication through
the retained checkpoint while the exact new generation remains running. This
is local process recovery evidence; it does not prove independent finality,
native emissions, economic correctness or continuing service health.

A later incident names the previous repair's exact signed approval, independent
key and acknowledged journal through `plan.previous_repair`. Its original host
approval and initial host journal must still match. An uncertain predecessor
cannot supply a previous generation. Each later incident requires a new explicit
owner signature and retains its own single start; the controller cannot issue
those approvals.

The controller adapter loads `plan.original_host_approval` before deriving the
unit from the original approval's `plan.unit_file.path`. The new journal is
`plan.state_path`. Deployment tooling must retain the exact existing paths and
hashes. It may copy approved review envelopes into protected custody, but must
not copy or recreate dynamic journals, checkpoint markers, credentials or
bootstrap state. Live owner approval and actual host rehearsal remain required
inputs; source fixtures execute only a private synthetic manager transport.
