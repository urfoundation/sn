# Registration and diagnostics composition qualification

The frozen SN source at `151343c02532b9a992b8f58642e51ab773784843`
combines the registration transport and API corrections with the miner and
validator diagnostic-cause corrections. Its program and embedded runtime-profile
bytes match the integrated mainnet-hardening branch; documentation changed after
that source commit. The physical dependency graph is sealed in the evidence
directory below, including Connect `b163f9dd`, SDK `516521fb`, and server
`5dc11761`. This is a source qualification, not a release, deployed service,
mainnet activation, or proof of delivered alerts.

Sol medium executed the maintained composed check on the sealed sources. All
**18 of 18 stages passed**: six normal/race test-binary builds and twelve suite
stages. The exact selected roots were one `clientauth`, two miner, and four
validator roots; all **seven roots passed normally and with race detection**.
Each validator root ran in its own joined process. The runner reported unchanged
sources. Sol's independent after-fence admitted the unchanged sealed inputs,
ten physical sources, four module graphs and ten affected package identities.
It did not rerun the separately
qualified server model, SDK/Connect component controls, or diagnostic controls;
those prior receipts retain their own source scope and caveats.

Astra max independently compiled the affected SN, server, SDK, and Connect
packages in normal and race modes (eight commands) and ran four package vet
checks. All exited zero. Its before/after fences admitted the same ten physical
source roots, four module graphs, and ten affected package identities. No test
bodies ran in the author check. The first discovery attempt is retained with
exit 2: it probed the SN repository root as a Go package, although that directory
has no Go files. The corrected discovery excluded only that nonexistent package;
its before and after fences both exited zero with unchanged source and the exact
seven-root membership. That metadata mistake did not change product code or
require repeating a passing test body.

Raw reports, binaries, source/module manifests, stage logs, and the sealed
handoff are under
`/mnt/data/sn-testnet/evidence/mainnet-registration-diagnostics-composed-20260928`.

| Evidence file | SHA-256 |
| --- | --- |
| `inputs.sha256` | `85236134cffc555378de63ab5a68eb635afe6733e7ee5177bd69c25ce43ac18b` |
| `sol-positive/report.json` | `cb30ce0e64cff979386364562fdff9efcfcbd5a223349bd9842e968b8ad02863` |
| `sol-after/status.json` | `c7f393bbc92723fe93a94b4c73f81b37b161b8c09e2891113da6e0f1ffe8d4f4` |
| `AUTHOR-CHECKS.json` | `7024381e5fca5114069f82df3b8cdac69f814e19d7b317cbd8e3d6b7691c0a86` |

The remaining launch obligations include a production release manifest and
artifact provenance, server-first rollout and migration, live collection and
alert delivery, custody and economic qualification, and the independently
approved mainnet chain identity. No public RPC or testnet identity is an accepted
mainnet route.
