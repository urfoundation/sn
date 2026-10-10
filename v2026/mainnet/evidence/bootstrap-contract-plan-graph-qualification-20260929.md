# Contract plan graph qualification

The frozen optimization `1c87fce892095419341af835e47c476eb32fcd9f`
was integrated byte-for-byte as `6aa1b62164e6765cd39befc4a1f3f95f10fa131a`.
It retains approval, journal, result and signing bytes while limiting repeated
copying and validation of shared contract predecessor objects within one
command invocation. The [implementation handoff](bootstrap-contract-plan-graph-work-20260929.md)
documents the work expansion and unchanged guards.

Sol's [final raw receipt](/mnt/data/sn-testnet/qualification/sol-evm-plan-graph-work-20260929/RESULT.md)
(SHA-256 `de3c067866f723f0ad0dcaa6905ae432371d4f5570e3b4d547bfbb0c37c159d2`)
and [machine-checkable root census](/mnt/data/sn-testnet/qualification/sol-evm-plan-graph-work-20260929/CENSUS-scoped.json)
(SHA-256 `5dfd088a860a65ec2a3e44cb71d1b58748f5cfc96f7c46050b90d584df509ddc`)
record all 210 changed-source roots passing normally and under race detection:
five graph roots, 28 evidence CREATE roots and 177 adjacent bootstrap roots.
Six isolated graph causal controls failed at their assigned assertions; `go
vet` and source/dependency fences passed. The unchanged seven-predecessor
checkpoint race passed in 1,975.38 seconds after an earlier exact attempt hit
a 30-minute package timer without an assertion.

An independent full `./mainnet` normal package reached its 60-minute timer
after 299 distinct top-level roots passed and zero root assertions. The
unfinished vault-link recovery root had already passed in exact adjacent normal
and race runs. This broad package check is incomplete; its raw stack and pass
census are retained. No full-package restart was made.

These are source qualification results. The owned Snow route, approved mainnet
genesis, live authority, signing custody, Safe evidence anchor, deployment,
service activation and composed release acceptance remain separate gates.
