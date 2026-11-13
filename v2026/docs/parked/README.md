# Parked: the (X)-phase effort-bounty implementation

WHITEPAPER v0.3 (decision **D23**, `WHITEPAPER_DISCUSS.md`) deferred the validator
effort bounty out of the v1 launch surface: v1 validators earn native dividends only
(the owner is the majority validator early — α holdings + the buyback reserve staked to
its hotkey), and every deposit is a locked buyback instead of a distributable pool.

These files preserve the **built, adversarially reviewed, and hardened** v0.2
implementation byte-for-byte, so the (X) phase (WHITEPAPER §9.3, §13.6 — trigger:
governance wants owner-independent trail coverage) starts from working code instead of
a rewrite. Nothing here compiles in-tree (`.ref` suffixes); it is reference material.

| file | what it is |
|---|---|
| `STSubnet-v0.2-effort.sol.ref` | the full v0.2 contract: validator/vpk registry, `setOperatorServerKey`, `submitTrails` → sampled `proveTrailSamples` (F1 sample-estimator credit), `reseedTrailSamples` (HF-2 cap), both dispute paths (A2 coverage-bound sigs over `sha256(finalDigest ‖ coverage)`), `claimValidator`, fee pool/φ/ω |
| `Trails.t.sol.ref` | the trails/effort area suite (incl. the HF-2 reseed-cap regression) |
| `EffortHardening.t.sol.ref` | the mutation-verified F1/A2 exploit regressions |
| `validator-effort.go.ref` | the validator-side effort-claim builder (leaf tree, calldata) |
| `validator-effort_test.go.ref` | its tests (incl. contract-parity sampling vectors) |

Also still live in-tree (deliberately, they are the phase's inputs): the `/verify`
coverage attestation (`connect.VerifyEffortDigest` — the server signs the
coverage-bound FINAL today), the validator's trail-proof persistence
(`validator/trail.go` ProofStore), and the reserved `trailsWindowBlocks` epoch dial.

Reviews that apply to this code: `docs/REVIEW_FINDINGS.md` (F1/A2 fixed; HF-2 fixed;
the deferred residual = commit `leafCount` at submit).
