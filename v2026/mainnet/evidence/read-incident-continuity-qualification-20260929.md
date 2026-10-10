# MG-07 read incident continuity qualification

The exact qualified SN source `1bb311fc99f56054744ea2dd093111e5ad25f049`,
tree `db3212e1480e5e868949af6a3ef68fda39be116d`, was fast-forwarded from
`2cdc4d84` onto `codex/mainnet-hardening-20260927`. The non-force push and
remote head were verified. The [read incident implementation](../READ-INCIDENTS.md)
retains stable per-role incident IDs, first/latest failures, successful-read
recovery and recurrence through checkpoint restart. Recovery describes source
readability and does not grant service-health, repair or spending authority.

Sol qualified the exact 82-root affected census: 71 `TestMonitor*` roots,
including ten new incident roots, and 11 `TestRootMonitor*` roots. All 82 started
and passed in each of normal and race modes, with zero failures/skips and
package/process PASS exits. The runs took 28.24 and 178.06 seconds respectively.
All six isolated causal controls compiled and failed their specified named
behavioral assertion in both modes, for 12/12 discriminating executions.

The sealed source remained clean after qualification. The 634-module resolved
graph matched all ten physical selections: seven local modules and the three
versioned SDK/Connect/SCTP replacements. Module verification and the original
handoff hashes passed. No deployed collector, mainnet RPC, signing or repair
operation was part of these results.

| Retained receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg07-read-incidents-sol-20260929/SOL-RESULT.md) | `2753b21ecd3f72626e99e74619d282ffbe18caa60979a5de5485d783ffae8b49` |
| [Machine qualification](/mnt/data/sn-testnet/qualification/mg07-read-incidents-sol-20260929/MG07-qualification.json) | `7e0fcfb14a10c356c74196a9c0ff81bd68a7b07b143337e677ebccfe3d78a236` |
| [67-file manifest](/mnt/data/sn-testnet/qualification/mg07-read-incidents-sol-20260929/SHA256SUMS) | `7ff3d802f948fdb522183dddef0b78d3792b75815da74b0cc0d24b0faeabc485` |

Astra independently rechecked the manifest, exact commit/tree, clean source,
fast-forward ancestry and remote integration. These results qualify the recorded
source and graph. Compatible checkpoint/log-consumer rollout, deployed alert
delivery, complete incident retention and repair authority remain MG-07 work.
The [SDK race package gap](incremental-source-composition-20260929.md)
remains separate: 618/618 root-body passes do not close the 208 roots without
package-PASS coverage at that audited snapshot. This scoped integration does
not claim a complete composed release or mainnet activation.
