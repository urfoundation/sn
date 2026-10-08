# Literal slash declarations and retained HTTP evidence

The qualification checker correction is integrated as `931ba460`, from
Astra's `02b65eb9d97843f64f7b57d7e267f03195c3bb5a`. Terra ran 31 affected
checker roots normally and with race detection. All passed. Both causal
controls reached their specified assertion failures in both modes: restoring
the former immediate-prefix rule rejects a valid literal slash name; treating
every descendant as a direct root child misses a genuinely late parent.

All three maintained captures passed 4/4 stages, joined their processes and
reported unchanged source. Candidate bodies exited 0; controls exited 1.
Compile-only, vet and actual module/package admission passed. The checks used
the previously qualified runner, SHA-256
`cf73edc6abe2ddf42c7dbe5aa3840bd12d3093a6349ea7904e0b19cb3ba70368`,
so the new checker did not qualify itself. Only scheduling changed from two
workers to one; source, expected membership and assertions stayed fixed.

| Report | SHA-256 |
| --- | --- |
| `terra-positive/report.json` | `911bbfcc7a31c33ac331be1ae16b1fd9964d29d863877a2fff99b6c98a24a8ef` |
| `terra-control-a/report.json` | `ca63928c70f6dc93cb2061a288005a5fa2e8a58edda990edea9d30419596f61f` |
| `terra-control-b/report.json` | `434ccee98a3d4bf0bf79621e28aedd0e3e367a98ba5980c03d692266059ddafd` |

Raw plans, full manifests, source/module fences, commands and reports are in
`/mnt/data/sn-testnet/evidence/qualification-slash-parent-20260928`.
The [source receipt](qualification-slash-parent-source-20260928.md) explains
the nearest declared ancestor rule and its explicit ambiguous-sibling limit.
The event verifier itself is unchanged. Expected membership remains derived
from source, never from observed passing events.

After qualification, root replayed the original Connect normal/race event
streams with the new checker, SHA-256
`15da70c645c7de9839c7c0a6c511358a8c65b29f823f4f6e9dad0dfc1e338d9d`.
Both matched **33 passing roots and 32 passing descendants**, complete package
success and original binary exit 0. No Connect body was repeated. Input hashes
were verified before and after; `connect-retained-replay/receipt.json` records
the exact command, original exit files, input hashes and replay outputs.

Those bodies belong to Connect `bcf9b324abdf5d0588dc8cbd0f7b824d85c0e24c`
and remain in `/mnt/data/sn-testnet/evidence/connect-client-control-causes-20260928`.
The original capture owns execution/source/module fences; replay adds no new
execution claim. Its four causal roots already reached their intended failures
normally and under race detection. Their report SHA-256 is
`c5c3964d129b95337af99d67fec115aa54eac710e56ea1607647a386ecde7837`.
The earlier missing-descendant, unsorted-control and literal-parent metadata
refusals remain retained. Later Connect redirect/collector changes require their
own new bodies and cannot inherit this pass solely through ancestry.

The checker consumes standard-library implementation packages. Its physical
module graph still declares SN, Connect including nested SCTP, SDK, server,
proxy, glog, goidenticons and userwireguard; exact revisions and full content
manifests are in the handoff. This result does not qualify a production release,
database migration, live runtime or mainnet deployment.
