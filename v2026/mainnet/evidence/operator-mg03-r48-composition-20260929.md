# MG03 and R48 server source composition

Server root `codex/mainnet-server-hardening-20260927` now includes the
qualified test-only successor `05fee56f3051b8b96ff00f16bbf7f994ad57404d`,
tree `2c053ddda1fa3946f78f8410e03926e383728d5e`, by fast-forward integration.
Its parent `34fec2baaa6cb972f0e8f9e2a5d9897407418517` is a full merge of
the former root `0633780c` and the complete qualified MG03 lineage `26008e58`.
That parent's tree `20791e914b91fa283bf88c83f850aef3a89d58f2` is byte-identical
to the qualified lineage. Both histories remain reachable, including the
three earlier controller recovery fixes that a narrow cherry-pick would omit.
The integrated successor changes only `model/mainnet_composition_test.go`;
production implementation, migrations and tracked module files are unchanged.

Astra's [source-composition report](/mnt/data/sn-testnet/qualification/mg03-r48-server-composition-20260929/RESULT.md)
(SHA-256 `328e4a537dbfa09481ad971e56bee8ac0a920a49a1b75ed16be9b249aa6f15d8`)
preserves the six original merge conflicts, their resolutions, exact tree and
module identities, historical approvals and a 134-root affected test manifest.
Every conflict resolves to the already qualified source bytes. The integrated
test-only successor adds two
deterministic composition roots for settlement/archive custody and
registration/policy-history recovery, taking the target manifest to 136 roots.

Sol's separate [final behavioral receipt](/mnt/data/sn-testnet/qualification/mg03-r48-server-composition-20260929/SOL-QUALIFICATION.md)
(SHA-256 `27bc7348a0eafbf72f49cafea126bfa8455dd33f055fe47335e5e2e63b60fa12`)
qualifies these exact scopes:

| Source | Declared roots | Normal | Race |
| --- | --- | ---: | ---: |
| Full merge `34fec2ba` | 134 recovery, controller, migration, usage, escrow, policy and monitor roots | 134 pass | 134 pass |
| Successor `05fee56f` | Two new composition roots | 2 pass | 2 pass |
| Successor `05fee56f` | 52 adjacent model roots repeated on the successor | 52 pass | 52 pass |

The [per-root receipt](/mnt/data/sn-testnet/qualification/mg03-r48-server-composition-20260929/per-root-results.json)
(SHA-256 `ea6b1646e17045f2ce537d2c0b15061ba0d7f1c9a56d8f326de76b02da6d6f6e`)
records all 188 candidate/root rows as pass/pass. These are separate scoped
runs on two commits; the full 1,125-root model package was not run in this
handoff. Retained preflight refusals and interrupted setup/compile attempts
are excluded from the passing totals, with their raw logs preserved.

Both worktrees and their physical dependency graphs remained clean. The
qualified graph retains SN `5198f6c9`, SDK `516521fb` and Connect `b163f9dd`;
external absolute-replacement modfiles identify those paths without editing
tracked `go.mod` or `go.sum`. Resolved-module records before and after are
byte-identical. Tests used disposable private PostgreSQL/Redis fixtures, and
all owned-service cleanup exits were zero. The original Astra source report
and earlier MG03 approval receipts retain their exact hashes and source scope.

The server root and isolated successor branch are pushed. The composed release
build, actual artifact/dependency lock, deployment, trusted production
receipt/finality producer, actual chain fees and live custody remain open.
This qualification does not attest the current SN/SDK/Connect roots as a new
combined release. No live database, chain, signer or deployment was touched.
