# Public Safe current-only submission qualification

This October 1 increment adds a domain-separated v2 risk-policy acceptance and
an exact accepted-revision opt-in to the public bootstrap successor command.
V1 remains public read-only. No production policy acceptance, real signer or live
transaction is part of this work. The original complete-history route remains
unimplemented and distinct.

**Scoped qualification complete.** All 68 author-selected roots pass normal and
race, with no skips or failures; three causal controls reproduce their exact
intended assertion in both modes. Author vet passes. Sol's independent 14-root
selection passes normal/race and vet, with static review verdict **GO, no blocker**.

Frozen source is `f314159153fa9171f8f2da32445578bbb5ef752c`, tree
`aad88541f4c387c7be2d5ba66fcbc834b345b35f`, based on SN
`1806b3b3c8c6d08b288d7f3ad415a5813947b2e7`. The isolated worktree is
`/home/by/urnetwork/temp/sn-mainnet-safe-current-submit-20261001`.
This follow-up changes documentation only. The parent integrated the implementation
at `095a22083b98aa2ec4472181f3bd1e216dbcee51`; its mainnet Go files and module files
match the frozen source. The only intervening source change is the separate release
builder mode fix integrated at `512927e9`. Release-image qualification is separate.

## Behaviors and boundaries

The deterministic public command fixture uses synthetic independent signatures,
published Safe bytecode, the retained eight-action local graph and authenticated
native trie witnesses. It covers absent acceptance, absent/wrong opt-in, v1 public
refusal, hidden owner/module authority, reorgs, advancing proof/admission heads,
late pending Safe/relayer nonce and runtime changes, a consumed reservation after
proof-time mutation, exact lost-reply execution and read-only receipt recovery.
The separate acceptance tests cover independent signing domains and exact partial
publication recovery from v1 to v2, with unchanged earlier counted authority,
original custody, nonces and maximum liabilities across runtime revision.

The complete-history capability remains distinct. A v2 acceptance cannot be mixed
with it, and the original signed history statement remains retained without a
history-verification claim. Acceptance-file import alone cannot submit; every
public submit requires the exact latest accepted object hash. An explicitly
supplied valid import may remain retained after a wrong-hash refusal, with no new
attempt or send. Recovery can omit the original import file because the approved
bytes are already in the immutable original journal.

Adjacent coverage includes all selected successor execution/canonical/runtime
authority and custody roots, readmission, pending-transaction reconciliation,
counted-record/marker tampering and native Safe storage proof roots. The three new
roots and 65 adjacent roots are recorded individually in the sealed selection and
raw Go test streams; this is not a full mainnet package sweep.

## Exact results and dependency graphs

Elapsed values below are the terminal Go test JSON package values.

| Selection | Roots | Normal | Race |
| --- | ---: | --- | --- |
| Author light authority, custody and proof | 62 | PASS 112.843s | PASS 671.074s |
| Author heavy commands | 6 | PASS 420.420s | Public 1: PASS 622.387s; adjacent 5: PASS 1989.436s |
| Independent focused current-policy selection | 14 | PASS 186.072s | PASS 1294.192s |

Both author and independent `go vet ./mainnet` runs exit 0. Each of the three
isolated overlays removes one boundary: v2 independent-signature verification,
v1 public-route refusal, or exact caller acceptance-hash matching. All six runs
compile and fail the named assertion, with no race/panic confounder. The wrong-hash
control reaches an otherwise successful synthetic command, demonstrating the
caller pin prevents a real fixture send rather than merely changing diagnostics.
Earlier preliminary streams are retained and do not replace frozen-source results.

Author runs use Go 1.26.6, `GOMAXPROCS=2`, external `active.go.mod` with SHA-256
`c4737431f99338782f82e687bdadc27d4e6472de9e6a3218ac9a2f52b326ca38`, and clean
shared server `898dc8f3b211d1e2fca1b0a0c970f7673b36fd7b`. All six local siblings,
both in-tree replacements, module graph and changed-source hashes are retained.
The final dependency audit confirms their exact original clean source fence.
Sol uses a separate external modfile, SHA-256
`0d26dc8c2c7272ecce4de435a07114b74cf9bf0f67cea38c74a626588a2d9643`, with clean
integrated server `0b8e758db9ce5516de867e1b5d0a1c9660a0880b` from its isolated
checkout. The broader author selection is not attributed to that different graph.
The tracked module files are unchanged. Longer package harness bounds cover race
instrumentation; production RPC, native-window and transaction deadlines are unchanged.

## Sealed evidence

Restricted evidence is `/mnt/data/sn-testnet/mainnet-safe-current-submit-20261001/`.
The 79-file `manifest.sha256` verifies, with SHA-256
`66ac1a48c6e2c444624119d318e390fe1212e8e37b333798afab077d7b63c7ec`.
`qualification.json` has SHA-256
`226f1f7b22d9bf51df10273e5a7001eeeebce029fa772b67c0fda5042602edcc`.
The independent `sol-independent/receipt.json` has SHA-256
`149f07dd90d967aa39071d05b67d30ed55d3471f82dd7ab335f39b7a4c4f8c5f`;
its verified `SHA256SUMS` seal has SHA-256
`5198558eb414fd7008798d6e025632b0b647480c89750ba8b1e5a59b9ed5a955`.

## Remaining production gates

The actual production independent v2 risk-policy acceptance, owned-route/current
runtime qualification, exclusive signer cutover, complete custody and live chain
readback remain gates. Current-only proof does not establish complete historical
initialization/delegatecalls, independent finality or an authenticated pending
overlay; its scoped pending reads cannot exclude changes between calls.
