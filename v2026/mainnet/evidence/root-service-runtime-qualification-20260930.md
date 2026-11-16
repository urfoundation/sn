# Root-service runtime qualification

The frozen Go/test source is `5e797380c7a1d8594f734ae2e33f2062d3d1b94b`
(tree `2d0237923a85a6bcacbb62133bbd36dd2dd47640`). Later author changes
through `88c7f91b` affect only ROOT-SERVICE.md and ROOT-SUBMISSION.md; the
integrated source is byte-identical. The public `root-service
plan|prepare|status|run|activate` command admits exact original inputs, owns a
bounded observation/recovery loop, and can recover an original issued signature
even before a local basket decision was recorded. `activate` remains blocked
before journal or RPC ownership because production current authority, root
hardware signing and live submission ports are absent.

Sol medium independently verified all **12 focused roots** normally and under
race detection. All **100 adjacent roots** passed normally and in six
package-PASS race shards with exact equal membership. Five normal and two
selected race fault checks failed their intended assertions. The initial
combined race attempt exceeded its cumulative time budget after 86 root passes;
its partial log is retained and is not counted as a package-pass result.

The [sealed result](/home/by/urnetwork/temp/sol-root-qualification-20260930/evidence/RESULT.md)
and [81-entry checksum manifest](/home/by/urnetwork/temp/sol-root-qualification-20260930/evidence/SHA256SUMS)
retain source/dependency hashes, test binaries, build info, selectors, raw logs,
causal patches, exits and the interrupted attempts. The manifest SHA-256 is
`83aa4dfb16102cc930b1462e482c11234d43d6601222720a5c8a04f32eb87b79`.

This is fixture qualification of recovery and refusal only. It proves no live
root seat, signer/device behavior, global nonce/hotkey exclusion, effective
eligibility, fee-exposure bound, mainnet chain identity, broadcast or activation.
