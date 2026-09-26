# R48 composed recovery qualification

The frozen source is `d6ea0d0ccfde355b11eda0212bfd0e9e959776ae` on
`codex/r48-recovery-composed-20260926`. It composes native recovery, the separately
signed 60/60 campaign timeout migration, and the actual launch admission fixes.
No live file, process, signer, plan, generation or validator state was changed by
this qualification. The previous source observation and qualification bundles
remain byte-for-byte unchanged.

`composition-red.log` reproduces four actual failures: old signed sealed R47
rejected after migration; a fresh launch reaching its test-controlled migration
executable with an unapproved selector; a retained launch ignoring absent
native generation authority; and live adoption ignoring an explicit selection.
The normal and race logs qualify their fixes and adjacent generation/source-role,
strict-adoption, startup and namespace boundaries. `qualification.json` records
source hashes, selectors, exact durations, and raw and portable log hashes.
Portable logs omit environment/provenance chatter and normalize synthetic paths.

The owner-signed timeout migration authenticates the actual archived old YAML
and its recomputed hash. Native recovery requires its immediate predecessor and
exact signed successor, pins both the request and signed receipt hashes, and
keeps the new native approval bound to the new config/base plan. The R47-shaped
positive remains an early failed interval (observed8091300/planned8092324,
settlement653, six assertions/five failures), with unchanged signed source bytes
and historical fault snapshot. Missing or altered migration authority fails.

Fresh launch checks selection before database migration. Retained launch cannot
fall back when native selection lacks generation authority. Live adoption permits
only the existing exact child arguments and refuses missing/tampered pins or a
different valid receipt. It writes no adoption provenance on that refusal.

Pending-N restart tests use the actual atomic intent and EMA writers with inert
local intent values. They show admission before/after a durable EMA commit and
an idempotent same-epoch fold; they do not authenticate a native source or replace
ordinary semantic replay. An observer-induced interruption after an actual EMA
exchange retains marker/preimage bytes and fails closed in both recovery and the
ordinary loader. No automatic uncertain-publication reconciliation is claimed.

Cutover still requires an exclusive stopped source recapture, a future N with
more than the observed 20-minute startup time plus margin, and exact new owner
approval. Apply the signed timeout migration first, then capture/apply native
recovery under its new base plan, then restart once. The qualified executable
may remain pinned to the source commit above; this bundle adds evidence only.
Strict historical-runtime/final acceptance and positive completed trail, pool
weight, native emission, stake and nonzero capture gates remain separate. R48 is
the final testnet run and must reach a truthful terminal report, even if partial;
there is no R49.

`candidate-schedule.json` is a later read-only LAN observation at finalized
8092041/native1695, observed18:19:36 UTC. Both eligible validators retain the
same config, applied1690 intent and EMA byte hashes. Tempo360, LastEpochBlock
8091971 and PendingEpochAt0, with12s/block observed over100 blocks, predict
native1696 at8092331 around19:17 UTC and1697 at8092691 around20:29 UTC. These
are estimates under an unchanged schedule, not approved N values. A live owner
must recapture after stopping, allow the measured23-minute startup plus margin,
and review the resulting exact new plan. The generic `.go.txt` collector is
read-only and can repeat the observation using the approved LAN endpoint.

Verify with `sha256sum -c SHA256SUMS` in this directory.
