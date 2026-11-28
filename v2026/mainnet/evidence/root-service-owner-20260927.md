# Root service owner qualification — 2026-09-27

The [root service decision owner](../ROOT-SERVICE.md) now owns one independently
approved existing-seat basket from finalized observation through durable native
intent and canonical completion. One private composite journal prevents a
decision/action persistence gap; restart never chooses another request, nonce,
era or allowance. Observation, current authority, custody and submission remain
separate ports. Missing mutation ports block before fresh effects or broadcast
reservation. Existing signatures and old receipts remain recoverable.

This is a package-local production ownership layer with a finite joined
supervisor, not an activated root validator. No live key, native signer, real
RPC submission, activation, deployment, merge or push was used. Root basket
destinations are netuids; ordinary evidence-based `sn/validator` miner scoring
and SN25 owner reset are separate roles.

Worktree: `/home/by/urnetwork/sn-mg08-root-service-owner-20260927`.
Branch: `codex/mg08-root-service-owner-20260927`.
Base: `b15ad611bab1f9d47567ec5c636e0b61fd31ce8e`.
Toolchain: Go 1.26.6 linux/amd64. The sibling connect tree was clean at
`358cefaef9b058cdd06ba5e9c4feeadef64ae1fb`.

Final-source results:

| Command | Result |
| --- | --- |
| `go test -json -exec /mnt/data/sn-testnet/evidence/mainnet-root-service-owner-20260927/capture-normal.sh ./mainnet -count=1 -timeout=15m` | 253 roots and 41 subtests passed; 173.371s reported package time |
| `go test -race -json -exec /mnt/data/sn-testnet/evidence/mainnet-root-service-owner-20260927/capture-race.sh ./mainnet -run '^TestRootService' -count=1 -timeout=15m` | All 20 affected roots passed; 159.119s reported package time |
| `go vet ./mainnet` | Passed |
| Source hash verification; staged/unstaged `git diff --check` | Passed |

All commands ran from the worktree above. The `-exec` wrappers only copy the
exact generated test binary, record its build metadata and exec it unchanged;
they do not alter test selection or behavior. Test binaries execute with the
Go package working directory `mainnet/`. JSON logs, complete root membership
and terminal summaries retain zero failed or skipped events. The 20 new tests
use no subtests. Earlier focused and full normal runs also passed; only comment
case cleanup preceded the final captures. No failure, timeout or race report
occurred in this qualification.

The frozen Go patch has SHA-256
`84f52ad3caf369319f19f74d1e4a897eae07c167cb0716ffc500bdd4aec4aa6b`.
The source manifest, covering all mainnet Go files and `go.mod`/`go.sum`, has
SHA-256 `2e828f24002eeb264a1b39283c5785df08c241bb7a4bdce03f83f17c6fdb395c`.
The actual normal binary has SHA-256
`2a3e54dda2937fcc1ec8e23c14c4aa68c76cf80b0abd08f21c4d264e858f9041`;
the actual race binary has SHA-256
`a06f31828bc41c618518a23fe1701f49e8e5c91380d425682c8723cca8fdd1e5`.

The retained evidence is
[`/mnt/data/sn-testnet/evidence/mainnet-root-service-owner-20260927/RESULT.md`](/mnt/data/sn-testnet/evidence/mainnet-root-service-owner-20260927/RESULT.md).
It includes the exact source patch, binaries/build identities, selection lists,
raw JSON logs, summaries, source hashes, pinned Rust files, replay harness,
dependency archive and output-vector hashes. Its final manifest excludes only
Cargo build intermediates and the manifest itself.

The pinned source is Subtensor `67dcf7f791dc495064c293f080a0702cb433e51e`.
An offline Rust replay compiles the original unmodified max-upscale functions
with locked `substrate-fixed` revision
`d5f70362f2e05b5f33fb51cd7baa825323e4e6c5`. For maxima 32768, 32769 and
65535, every input from zero through the maximum produces 131,075 two-element
vectors in total. The Go regression reproduces all three independent digests;
separate vectors cover zero and positive half rounding. The source references
and admission limits are in [ROOT-SERVICE.md](../ROOT-SERVICE.md).

Tests force finalized rollback/fork, changed runtime/code/metadata/network,
interrupted canonical rechecks, invalid/defaulted storage and complete true
network census. They force decision persistence before effects, request/intent
substitution, missing or denied independent ports, signer timeout/restart,
same-byte broadcast bounds, terminal fee retention, consumed observation budgets,
both sides of ambiguous writes, cancellation, concurrent callers, publication
failure and private/special/foreign journal refusal. Adjacent action, offline
custody, receipt and storage implementations remain unchanged and are covered by
the full normal run; earlier race evidence is not claimed as a new broad race run.

Remaining gates: independently approved owned mainnet identity/runtime artifacts,
actual owned root seat and effective eligibility, global hotkey/nonce fencing,
qualified native device signing and idempotent submission, enforceable payment
exposure, inclusion-time seat/runtime protection and service activation. The
read-only node remains a finality/storage trust boundary. Native generation,
code/source hash and maximum fee are not signed execution predicates. There is
no standalone-journal migration, new root registration, automatic action renewal
or arbitrary zero-weight reset.
