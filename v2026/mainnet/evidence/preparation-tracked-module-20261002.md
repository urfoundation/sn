# Tracked offline preparation checkpoint — October 2

The author scope is `PASS_SCOPED`; it is not a release or live preparation
approval. [Exact receipt](preparation-tracked-module-author-20261002.json),
SHA-256 `562feb80e702ac9259727e99cc5ce6f80246e26cd1c939d6b7a7b6481d0bf15a`,
binds 130 immutable evidence paths, source bundles, raw archive and verification
binaries under `/mnt/data/sn-testnet/mainnet-storage-owner-adoption-20261002/ledger-preparation`.

| Source | Exact pin |
| --- | --- |
| SN, completed tracked source | `0384cbfc90ee7f7f8cc7b8764f119b71aa62783e`, tree `7e5a1146e54f34b463d3310838f2c0eaaedb5ae5` |
| SN, first 68 tests per mode | `1d51f8be81588991223b74d71164460ae5e3491c`, tree `469bb4382fda3ace72e2c0d3d4a1d6fe9e201dae` |
| Connect | `7600ea5c82272ab33a90bc1d0a55f11720e09257`, tree `65b3f1887528df2e965c755dbbd1974fa1a570ce` |
| Server | `c2563f9a7ae7d6641569da6a1c22f772dc3793d6`, tree `5c7fdda4cb10a199f8d66d9023543d8e83e41747` |
| Latest-main source join only | `9d7d57fcd4303df5fa4e265de1aa6e467c4056ff`, tree `d0e5d5d467fbb8fdf2e458cb7453088483702f47` |

Tests used `GOWORK=off`, tracked `go.mod`/`go.sum` and no alternate modfile.
Core14, public 31, native 2, ledger 11 and miner 10 pass normal/race on 1d51.
Server blob 7 passes normal/race on 0384; seven package vets and three verification
binary builds also pass on 0384. Its only change is four test-dependency sums:
all Go files, `go.mod` and effective selected module versions remain identical.
The first three server-dependent attempts failed before any test ran because
those sums were absent. Their original logs and failed results remain retained.
The combined 75-test scope therefore has two exact source execution fences.

Connect is pinned as `v0.0.0-20261002204525-7600ea5c8227`, with module checksum
`h1:QQ+2MQbTJyln/vo5x6pjlEf01TXwAsiO6ud46U9efZw=`. The local file-proxy package
was built from the exact clean Git commit and authenticated before the consumed
graph was resolved. This proves source identity, not publication or SumDB
authority. The failed first packaging attempt against a linked worktree remains
separate from the successful real-clone packaging. Publish the exact qualified
dependency before promoting this pin as normally downloadable.

The actual public plan/apply commands cover fresh validator ledger, native
journal, fixed file-lock snapshots, fleet recovery, claim queue and successor
member-head owners. Explicit private-root mode stages and publishes only the
reviewed leaf inode; its target parent, staging and metadata directories remain
precreated. Original signed bytes, retained nonces and pending/completed custody
are never reinterpreted as fresh. Actual daemon and owner-local native startup
controls refuse EIO/EMFILE observations without changing custody, then reopen the
same original root/log/raw/anchor when those observations recover.

The [source join](preparation-current-main-join-20261002.json), SHA-256
`19af2f019a159befb6b37a1d44afe5560a3821088df88f9f585fa9a6ff1e210b`, proves that
9d7 retains all 27 preparation paths from 0384 and all 21 newer main paths from
e08e1b11, including the eleven claim producer files. It adds no test claim.
Independent qualification of this later composition remains pending.

Retained/restore semantic rebinding, joined capacity/retention revisions,
deployment assets, actual recovery rehearsal and the composed release remain
open. A separate physical-export candidate retains original member inodes for
future restore validation, but has only passed dependency preflight and is not
qualified. Neither the preparation result nor an inventory authorizes restart.
Remote PostgreSQL/MinIO recovery and the full current server suite are outside
these local-owner tests. No key, transaction, deployment or service was started.
