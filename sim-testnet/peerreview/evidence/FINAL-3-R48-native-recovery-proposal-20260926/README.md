# R48 native recovery proposal evidence

Read-only diagnosis and current-boundary qualification, based on source
`dd0909a6`. No recovery permission, live change, native success or final acceptance
is claimed. The [proposal](../../R48-NATIVE-RECOVERY-PROPOSAL.md) states the missing
owner approval and the separate historical-runtime condition.

`native-sources.json` projects the two successful production
`ObserveReleaseNativeSourcesV2` calls. They authenticated original signed native
source/envelope bytes, finalized inclusion, identity/schedule and actual applied
weight rows. Both selected configs, intent files and EMA files were unchanged
across the reads. Both retained sources are native1690/settlement649. Validator 2
uses the separately signed source-role overlay config; its coordinator namespace
remains generation 2.

`measurement-pool-audits.json` projects the corresponding exact content-addressed
measurement artifacts, with their original artifact/envelope hashes. Both audits
reject source epoch648's unavailable payout roots and give zero pool weight.
Both actual applied rows omit pool UIDs 3 and 4.

`native-pool-history.json` projects independently read LAN native state at six
canonical block hashes. Each observation authenticated runtime471's exact code
and metadata before deriving storage keys, and rechecked the canonical hash
after all reads. Blocks 8,084,976–8,084,977 retain positive pool emissions and an
active validator with positive pool weights. Blocks 8,085,276–8,085,277 retain the
same positive weight row but mark both validators inactive, with zero pool
emission/incentive. `ActivityCutoff=5000` and the old `LastUpdate` support an
activity-expiry explanation. After R47 native1690, validators are active again
but their new rows omit the pools; pool emissions remain zero. A one-edge adoption
alone does not establish restored pool funding or successful claims.

These native observations do not constitute complete public-stream or settlement
replay, runtime economic review, immediate-parent reward causality, or acceptance.
The parent's EVM capture/entitlement receipts are separate evidence and are not
reproduced here. No new funding or stake injection is authorized.

`observed-basis.json` retains exact current setup, rollover, source-role, config,
intent, EMA and source hashes. It leaves the future native epoch unassigned:
R47's active source was not stopped for exclusive recovery capture. A new approved
one-edge request must recapture after the terminal handoff.

`qualification.json` records focused normal and race selectors and their four
passing logs. They verify existing strict/adoption/runtime/custody/rollover
boundaries. No new recovery code or red/green implementation is claimed.

The projections remove operational paths, local ownership/inode metadata, and
raw public key bytes. Public key SHA-256 values retain identity comparisons
across the historical cuts. Original read-only output hashes appear in each
projection and `qualification.json`; original outputs remain in the restricted
qualification directory recorded by the parent. No rendered config, credential,
private key, signed extrinsic bytes or private ledger was copied into this bundle.

The two `.go.txt` files preserve the read-only diagnostic source without adding
packages to recursive Go builds. To reproduce, copy each to its own temporary
`.go` file outside this bundle, then run through the qualified repository's Go
workspace, supplying the operator's exact state root and approved native RPC.
`observe-source` requires `--state-root` and `--substrate`;
`observe-pool-history` requires `--substrate`, `--netuid`, `--uids` and `--blocks`.
Its historical cut was `8084976,8084977,8085276,8085277,8090544,8091127`, with UIDs
`3,4,254,255`. Capture raw output only in the restricted evidence directory and
apply the same projection before publication. The original incomplete storage
probe is retained there as a failed diagnostic, not used as successful evidence.

Run `sha256sum -c SHA256SUMS` from this directory to verify the portable bytes.
