# Diagnostic cause isolation qualification

Astra's `07eb81722db747d54794972187f54ec387d23783` is integrated as
`de01a776`. Optional miner, trail and release diagnostics now classify exact
sentinels and reviewed concrete fields with a 32-node bound. They do not invoke
an arbitrary error's `Error`, `Unwrap`, `Is`, `As`, `Timeout` or `Temporary`
method. Opaque/custom wrappers report `unknown`. Original errors and required
retry/custody predicates remain unchanged; a diagnostic label grants no action
authority. Miner cancellation precedes optional failure reporting.

Terra completed all **20 affected roots normally and with race detection**:
two shared diagnostic roots, nine miner roots and nine validator roots. All
passed; the maintained positive capture passed 12/12 stages. Five causal roots
in three controls reached their intended assertions in both modes. Controls A
and B restore the prior miner and validator traversal; control C deliberately
adds generic `Unwrap` traversal to the new reader. Controls retain fixed test
bytes and join the deliberately entered foreign methods before failing.

All captures report unchanged source, exact membership and joined processes.
Positive bodies exited 0; control bodies exited 1. Compile-only, vet and actual
module/package admission passed, and the complete driver exited 0. No rule or
diagnostic wire schema changed. Root/shared scalar output did not need a rerun.

Raw evidence and complete source/module manifests:
`/mnt/data/sn-testnet/evidence/mainnet-diagnostic-cause-isolation-20260928`.

| Report | SHA-256 |
| --- | --- |
| `terra-positive/report.json` | `4f0029b1520bbf0663dd3c88e6f46296a3e69b779d2db369af9aa5356af5c6f8` |
| `terra-control-a/report.json` | `80c83153327774820ef80f9bbb0bfbef9422ba2fbfafbece053316d64c24f25b` |
| `terra-control-b/report.json` | `4c353aa596c75dfdeeab4f12dbe3395ea6eb9dc57acac440e3ed33c593ecfd19` |
| `terra-control-c/report.json` | `c9156599f6d9ee2049cbc0f2126626fee1dcbbed72559121e5d0d51794e9dd16` |

The component graph uses Connect `c68689c4`, SDK `42241118` and server
`4468a696`, with the remaining exact physical replacements retained in the
handoff. The qualified runner was `cf73edc6`; root narrowed jobs to one without
changing source, assertions or expected membership. The later combined
registration graph has its own interface checks. SDK/internal output, required
core error classifiers, live telemetry delivery and deployment remain outside
this component claim. See the [source scope](diagnostic-cause-isolation-source-20260928.md).
