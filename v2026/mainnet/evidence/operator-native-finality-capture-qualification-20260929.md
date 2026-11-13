# MG-03 bounded native finality capture qualification

The exact qualified server source `5ff7bf0264b775920049910896c23ec58862429f`,
tree `ceb2395a28ec5371d3c14eb9f7f7cbaac0f9754e`, was fast-forwarded from
`cfcbfcba` onto `codex/mainnet-server-hardening-20260927`. The non-force push
and remote head were verified. The [capture command and dependency contract](https://github.com/urnetwork/server/blob/5ff7bf0264b775920049910896c23ec58862429f/strecovery/NATIVE-FINALITY-CAPTURE.md)
obtain exact native headers and stored GRANDPA certificates from an explicitly
selected archive, preserve bounded partial evidence across restart, and replay
the existing offline finality/receipt verifier before publishing a private proof.

Proof v2 can accept a bounded certified descendant when the original collection
boundary has no stored certificate. It authenticates that exact ancestor's
Frontier mapping and preserves the original collection, EVM/native boundary,
signatures and history. V1 retains its exact-boundary contract. Request
reservations precede transport; interrupted responses consume the retained byte
allowance conservatively. Restart neither renews that allowance nor treats a
missing certificate as proof of non-finality. Completed proof replay is offline.

Sol qualified all 23 new roots and the complete declared 112-root affected
scope in normal and race modes. Each expanded mode comprises 109 non-service
roots in `strecovery` and `cli/strecovery`, plus three database roots under a
new disposable PostgreSQL/Redis fixture. Every selected root started and passed,
with zero failures/skips and successful package/process exits. Fixture source,
module and cleanup checks passed; the owned containers were removed.

All seven isolated causal patches compiled and failed the specified named
behavioral assertion in both modes, for 14/14 discriminating executions. Vet
passed. The exact candidate, eight pinned sibling worktrees, resolver and
module graph remained clean/unchanged. No live chain RPC, signing, submission
or shared database was used.

| Retained receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg03-native-finality-sol-20260929/SOL-RESULT.md) | `8f59acde98c224e3ea2f58425c285298c4ddd0ecdd069910bc6bcc2539dce60d` |
| [Positive qualification](/mnt/data/sn-testnet/qualification/mg03-native-finality-sol-20260929/server-qualification.json) | `014f496dd184cb6efd017a39e7301f0a500733ee4d195f5cf57158c319cb0c3f` |
| [Causal qualification](/mnt/data/sn-testnet/qualification/mg03-native-finality-sol-20260929/server-controls-qualification.json) | `ecffea986e9f63780f73c711cd7e1ecb32e2729ad58b4a85dd0ca666137c0ff1` |
| [176-file manifest](/mnt/data/sn-testnet/qualification/mg03-native-finality-sol-20260929/SHA256SUMS) | `e335040f870b5d7aba8e70806affb210e01dcbe707d9c42a777e409385fbc5cf` |

Astra independently rechecked all manifest entries, exact commit/tree, clean
source, fast-forward ancestry and remote integration. These results qualify
the recorded capture source and graph; they do not approve a checkpoint or
establish mainnet identity. Archive body/certificate retention, independently
admitted checkpoint/genesis/runtime provenance, account nonce/native-state
proofs, runtime-qualified debit/refund attribution, production service adoption,
composed release artifacts and live custody remain separate MG-03/PF-03 gates.
Actual fees remain null and all accounting/spending authority flags remain false.
