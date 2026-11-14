# Closed testnet service cleanup

At 2026-09-28 07:35:29 UTC, the two PostgreSQL and two Redis containers named
`ur-subnet-testnet-v1-{pg,redis}-{1,2}` exited cleanly with status zero. This
completes the service cleanup following the retained testnet process shutdown;
it does not alter R48's failed acceptance result or start another run.

Their exact Docker identities, sim-testnet ownership labels, volumes and restart
policies were captured before stopping them. No PostgreSQL/Redis TCP connection
was present, and the active mainnet qualification uses independent synthetic
fixtures. The containers were stopped by their captured full IDs, without
removing containers or volumes. Both PostgreSQL data volumes remain intact.
Redis acknowledged `SAVE` before shutdown; both RDB snapshots were additionally
copied to private files on `/mnt/data` and hashed. The other release-gate
containers were outside this identified cleanup and remain untouched.

Private operational evidence is retained at
`/mnt/data/sn-testnet/evidence/closed-testnet-services-20260928T0736Z`.
The directory's nominal timestamp is a label; `after.json` records the exact
completion time above. Database/cache contents are not published in Git.

| Record | SHA256 |
| --- | --- |
| `before.json` | `f7c1cb40739ff0881bbd98fef3e5d19be3df8cc8412c369e093b65e29cf7b11a` |
| `after.json` | `16cdcc684c8c0e0bab80e830d6dbef7f9acf6d5acc36244570ca4fc104d78c48` |
| `stop.json` | `db40e9de115db64c59629257f8f9e487692b7fb6e123fced1e0a6a99ca77d410` |
| `redis-snapshot-sha256.json` | `d0577607e2ba81e22aebe865dd26a2513022feb998556a9408133108818474ce` |

For mainnet, a service shutdown receipt must account for process children and
owned containers separately. Preserve state before cleanup, identify services
by retained ownership rather than broad name patterns, and keep evidence
retention independent of whether the application is running. These local
snapshots are retained recovery inputs; no restore rehearsal is claimed.
