# MG-08 offline Safe release profiles: scoped qualification

Sol qualified `35095fa7c1f21184e519e546028ffeaf99284d49`, tree
`3db820ecd394814bbc3bbb89c0903cf314fa89b7`. Shared merge
`18a88db403bb8ae0b2fe43eae9d0e479cd96857a`, tree
`4a8fd2705ef248f14eb504d6503892afe23ae56b`, integrates that exact verifier,
catalog, public archives and tests onto `970034d1`. The sole conflict was help
text in `mainnet/main.go`; both the Safe artifact command and existing unsigned
successor usage were retained. The Safe implementation and artifact files are
byte-identical to the qualified candidate.

All six new roots and four nearest file/dispatch roots passed normally and with
race detection, with four exact package PASS/exit-zero invocations and no root
failures or skips. Eight isolated mutations each compiled and failed at the
intended named assertion in both modes: sixteen causal executions, each package
FAIL/exit one. They independently exercise public dispatch, false authority,
whole-archive identity, runtime code, ABI, source-tag content, compiler version
and storage-slot semantics.

| Retained receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg08-safe-release-sol-20260929/SOL-RESULT.md) | `a0ef94c6d18c0fb46465d7e1dba5a152a06aec62f8a216a7c03f1e408badd461` |
| [Structured result](/mnt/data/sn-testnet/qualification/mg08-safe-release-sol-20260929/SAFE-qualification.json) | `b278733e12e3f0a3319813acbe464bb5c70605ec944361e091fcfd2c998cb5e8` |
| [122-file manifest](/mnt/data/sn-testnet/qualification/mg08-safe-release-sol-20260929/SHA256SUMS) | `9f241acf28c3a5c54a12911e00a09b78d4d29559e55588bec20467ab85243a15` |
| [29-file author handoff](/home/by/urnetwork/temp/mg08-safe-release-profile-verifier-20260929/evidence/SHA256SUMS) | `20b6003dd2865ed32b40d2e1149ea314d30de770ae17c895496243b4c9fdd6da` |

Astra independently checked every receipt file, positive root/package terminal,
intended control assertion and process exit. All eleven actual physical source
paths, Git roots, heads, trees and clean states matched the sealed before/after
graph. Resolved module bytes were identical, SHA-256
`596e2c98e97c6b21bdc79fbfff0ab74e31355b43cf6ba6a23496e9283c4d0558`,
and `go mod verify` passed. The graph pins Connect `b163f9dd`, SDK `516521fb`
and isolated server `cfcbfcba`. Shared server `5ff7bf02` is outside this scoped
behavioral graph.

The integrated mainnet package passed compile-only `go test -c` and `go vet`
under the same pinned dependency versions, with SN/npipe resolved to the shared
merge. The command logs, module resolution and freshly measured physical roots
are retained at
`/home/by/urnetwork/temp/mg08-safe-release-integration-20260929`.
Composed behavioral qualification of the merge is recorded separately below.
The author ran no behavioral tests, live RPC,
signing or broadcasts during this integration.

## Separate composed qualification

Sol subsequently qualified the exact integration merge `18a88db403bb8ae0b2fe43eae9d0e479cd96857a`,
tree `4a8fd2705ef248f14eb504d6503892afe23ae56b`, with nine selected roots in both
normal and race modes: the six Safe roots, full-v3 successor command, successor
proposal and MG-07 read-incident continuity. Both invocations finished with exact
9/9 root PASS, package PASS and exit zero (108.49 and 470.28 seconds). There were
no root failures or skips. All seven physical local modules stayed clean and
unchanged, resolved modules matched byte-for-byte and `go mod verify` passed.
This composed graph includes qualified shared server `5ff7bf02`; it is distinct
from the isolated verifier graph above.

| Composed receipt | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg08-safe-composed-sol-20260929/SOL-RESULT.md) | `63425ad947b64b9b9967ecf3e4c51139b5a50c5e02ce1dae02e1a5e2ad2cc251` |
| [Structured result](/mnt/data/sn-testnet/qualification/mg08-safe-composed-sol-20260929/COMPOSED-qualification.json) | `e45e68db00c02280497137629e2a137e4ecc76a803f8c3cc400860a4c88e7710` |
| [19-file manifest](/mnt/data/sn-testnet/qualification/mg08-safe-composed-sol-20260929/SHA256SUMS) | `80421b6775411b0986eea6b511de58832c3e28e252ae5caaa8c4fc69a738b102` |

Astra independently checked the manifest, every selected root and package terminal,
process exits, identical before/after graphs and fresh actual Git roots, heads,
trees and clean state. This receipt adds no current Safe, account, signing or
installation authority and does not expand the partial coverage described below.

## Verified scope and remaining authority

The [offline command](../SAFE-RELEASE-VERIFY.md) requires an explicit version
(1.4.1 or 1.5.0), variant (Safe or SafeL2) and absolute local archive path.
Its immutable catalog and unchanged published archives authenticate package
identity, selected singleton/proxy creation and runtime code, ABI, published
compiler version/settings, input sources and dependencies, compiler-output
agreement and complete storage-layout hashes plus fixed slot meanings. Archives
are bounded and never extracted or executed.

The Safe-owned sources match the pinned release trees; support compilation
units retain separate locked OpenZeppelin and Safe mock-package provenance.
Published compiler input/output agreement supplies no independent compiler
rebuild or audit attestation. Factory and fallback-handler verification remain
outside this bounded command.

The sealed result keeps independent rebuild, current chain/account state,
initializer-owner binding, Safe authority, approval signing payload, executable
scope, network effects, installation and activation false. Neither the network
label nor an artifact match selects a live Safe or its owners. Existing versus
new Safe remains unresolved. Any new Safe must equal the original retained
initializerOwner address, or require separately authorized ownership migration.

Current proxy/singleton code and all owner/threshold/module/guard/fallback paths,
Safe digest/signature and inner-outcome evidence, separate nonce custody, signed
successor adoption and canonical anchor postconditions remain open. The eight
completed contract actions retain their original seals and cumulative exposure;
this command grants no replay or increased spending authority. It does not
expand the [150/270 adjacent battery or 543/618 SDK package coverage](bootstrap-contract-successor-full-v3-qualification-20260929.md#separate-composed-smoke-and-partial-adjacent-coverage),
or establish MG-08 activation and actual native 10/90 outcomes.
