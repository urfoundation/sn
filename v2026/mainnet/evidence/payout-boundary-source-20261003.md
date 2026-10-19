# Durable provider earning boundary: frozen source, not qualification

Reviewed on October 3, 2026. Server commit
`cdcb61fa3db72fb5c5844358224eeb0fb3cbd661`, tree
`3a6b153001e7f36c9ec986ca06931a9a49b461f3`, parent `63027130`.
Physical candidate: `/mnt/data/sn-testnet/provider-usdc-transition-20261002/server-boundary-anchor`.
Root checked the clean worktree, exact commit/tree, implementation and public
consumer references. The initial checkpoint was source inspection; subsequent
normal execution is recorded separately below. No deployment claim is made.

The [sealed test intake](payout-boundary-intake-20261003.json), SHA-256
`d5893a214f374ccac75d7182be7b7cae06cdfcd6e86ca850056b779d60a2bee0`,
binds 48 files. Root independently rehashed all bindings and compared all 25
bound candidate source/module files with Git `cdcb61fa`. It requests 13 new
roots plus 15 neighbors, five separately labeled causal groups with eight
expected failures per mode and an accepted-reconciliation positive control.
These are requested scopes, not executed results.

The appended migration installs an immutable singleton earning identity and
UPDATE/DELETE/TRUNCATE guards. Explicit `db migrate --sn-schedule-sha256` prepares
it from the exact reviewed configuration digest. Same-boundary preparation is
idempotent; conflicting identity is refused without overwriting the original.
The identity includes earning cutoff, attribution, legacy policy and selected
mainnet chain/genesis/netuid. Readiness may change without redefining earnings.

Actual planners, bonus/point allocation, SN usage readers and final Circle send
admission use `LoadProviderPayoutEarningPolicy`. Workers cannot initialize missing
boundary authority; removing the declaration after adoption also refuses new
allocation. Accepted processor attempts retain reconciliation. Database outages
retain unavailable/retry classification; schema damage, policy mismatch and
owner cancellation remain distinct. Programming panics propagate.

Thirteen new test roots exist: seven server boundary controls, two model
controls, three controller controls and one CLI control. They cover concurrent
same/conflicting preparation, immutable schema, process restart, declaration
drift around the actual send barrier, accepted-attempt reconciliation and a
real PostgreSQL statement timeout feeding retained-payment continuation.
The [independent normal batch](payout-boundary-normal-20261003.json) now passes
all 28 selected roots (13 new plus 15 neighbors), with no failures or skips and
owned fixture cleanup complete. Root verified all 37 receipt bindings and the
actual test events. The [same 28 roots under race detection](payout-boundary-race-20261003.json)
also pass, with no race reports and completed fixture cleanup. Root verified
all 33 race-receipt bindings and actual events. The [normal regression
controls](payout-boundary-causal-normal-20261003.json) reproduce all eight
intended failures across five deliberately modified groups while accepted
reconciliation remains a positive pass. Root verified 62 bindings and actual
assertions; cleanup completed. Race controls, affected package vets and CLI
build remain pending. These roots do not substitute for the full model run
on the earlier exact source or the eventual composed release qualification.

Merge/publication, production migration and anchor preparation, running worker
identity, live chain/custody authority and activation are still required. The
October 6 schedule is not operational merely because this source exists.
