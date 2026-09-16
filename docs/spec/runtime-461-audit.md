# Runtime461 compatibility review

Runtime461 is admitted as the exact testnet artifact below. This is a source and
artifact compatibility review for the subnet's consumed interfaces, not a claim
that all upstream economic behavior is unchanged. Earlier artifact identities
and signed approvals remain separate historical authorities.

The LAN-only observation at finalized block8018145,
`0x43093d12230005ca09a38835fb1506e7b018fb52233597e68ad50c440c2d7272`,
returned `node-subtensor/461/1/1`, metadata14. The exact code and metadata are in
[runtime-metadata-artifacts.json](runtime-metadata-artifacts.json); the current
code SHA256 is `a236f7d2ac285615ee1789953e5e009464cc96f357d48278a848f82cdc771cc4`
(2,534,293 bytes), and metadata SHA256 is
`ddeffac09b36b85f584ad08b11441f7eae184728a67fa286c0c9b919d27f0405`
(344,267 bytes). No independent-provider observation is claimed.

[CI artifact10445345071](https://api.github.com/repos/RaoFoundation/subtensor/actions/artifacts/10445345071)
binds [workflow35046095878](https://github.com/RaoFoundation/subtensor/actions/runs/35046095878)
to immutable source `7c9d45ebd423c7f6b0b477e11414fe2fe3a3794b`. Its authoritative
ZIP SHA256 is `6cb1ce0ef00453faf2354dab33c39baee8cf7d8efdb1c4ccd02a106ae4ced24e`.
The mirrored ZIP matched that digest, and its extracted Wasm byte-equaled the LAN
`:code`; the srtool digest independently agrees on code hashes, size and tuple.
The exact source tar SHA256 is
`864ce118ad144b688183346066ca63f174f2ae27e2e40a8a695208e0071cc875`.
Cargo.lock is unchanged from460, retaining SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`.

The [460-to461 comparison](https://github.com/RaoFoundation/subtensor/compare/8d5f20ec1a5e5d90295d43046dacdefc54aaed06...7c9d45ebd423c7f6b0b477e11414fe2fe3a3794b)
contains68 commits. Of the71 earlier source witnesses,53 are byte-identical and18
changed; none are omitted from the94-path
[runtime-v461-source.sha256](runtime-v461-source.sha256). The new witnesses include
root-basket trading, flushing, views, root-weight migration, proxy and associated
tests. Old source manifests are retained unchanged.

The frozen77-field selective-metagraph layout `bb7420226d39c0eb`, inherited-stake
formula, stake/permit admission, schedule, registration/leasing, parent/child
semantics and EVM address/transfer precompiles remain compatible. Runtime pallet
indices and signed extensions are unchanged. The source commitment batch still
uses Commitments18/0, Utility11/2 and CRv4 calls113/118 under Subtensor7. Metadata
grows7,909 bytes; it is not a version-constant-only change.

Substantial upstream changes remove root basket target vectors, mirror holdings
on deposits, accrue dividends in place and introduce bounded basket trading.
BetaBasketRuntimeApi changes3→4, removes `get_validator_weights`, adds trading
status and changes BasketSummary. Root-only call146 is removed and basket call150
is added. The root-weight migration clears only root weights and checks that
non-root rows remain. Shared author-fee settlement is extracted with the same
existing amounts/recipients/error branches; a new fast-price EMA follows the
unchanged slow EMA. SN's production Go consumes none of the removed root basket
APIs. These facts support reviewed compatibility, not automatic admission of any
future Wasm with similar metadata.

The existing exact-Wasm offline probe passed using the unchanged SDK, matching
all version/hash/size fields and immutable before/after input fences. Receipt:
`/mnt/data/sn-testnet/qualification/runtime461-terra-offline-probe-20260916-r1`;
RESULT SHA256 `70d8b9a870525b26565ab173b439f2b040e6f17b8542649eb4e0ffbf92d511a3`,
seal SHA256 `76d48bc891ea1de1f51cdc9999327d00093786609ca3d5f58c4d5e250c0e3ab7`.
Focused Go qualification is recorded separately; this review does not claim a
full gate, native setup, continuation or campaign success.

Admission appends461 to the immutable artifact catalog, with exact current
provenance, reviewed native encoding/stake layouts and the explicit455→461
configuration-identity transition. It also preserves former-current460 companion
source provenance. Current writes cannot use old artifacts, and old signatures
cannot be relabeled461. The manifest cardinality check derives length from its
exact version vector; the metadata cache capacity already derives from the shared
catalog.

Compatible-update automation remains a separate architectural change: version
selection would need a reviewed interface/semantic profile, exact-block metadata
binding and dynamic native signing domains. Metadata shape alone cannot certify
root-basket or fee semantics. This recovery therefore retains exact review and
fail-closed admission rather than extending trust to unknown versions.
