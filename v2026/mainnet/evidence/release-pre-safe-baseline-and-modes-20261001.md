# Pre-Safe release baseline and artifact permissions, 2026-10-01

This is a retained baseline for SN
`1806b3b3c8c6d08b288d7f3ad415a5813947b2e7` and server
`0b8e758db9ce5516de867e1b5d0a1c9660a0880b`. It includes the integrated validator
and operator custody changes, but predates the later public Safe-submission
implementation. It must not be relabeled as a build of that implementation or
of the later release-builder fix. Effective Connect `e1b5d77b` and SDK
`5d37be38` replacements remain unchanged.

All evidence below is retained under
`/mnt/data/sn-testnet/mainnet-successor-release-astra-20261001`.
Two builds from clean physical source checkouts, separate empty compiler
caches and fixed tool/epoch/platform settings each produced 17 Linux/amd64
executables, five freshly selected contracts and eight image contexts. The
170 artifact hashes, exact clean binary VCS stamps and ten selected
creation/runtime byte pairs passed readback. The two builds produced identical
bytes for all 17 executables and ten contract byte pairs. This is repeated
local construction on one host, not independent-builder reproducibility.

| Retained input or result | SHA256 |
| --- | --- |
| `candidate-a/manifest.json` | `2317066d5db57b37db94dc3f8b60c6a6afc8e45e64dbafa7db979ed06e981cb9` |
| `candidate-b/manifest.json` | `68d59edd52ada66879423eb0fe4fb0c28c945040d15bf79824e026f5d48eafdd` |
| `evidence/source-lock.json` | `51f07ea8fbd43ecc9da5aa3598c5eb6158a89beb0c15f03864cd740a25e65ba5` |
| `evidence/audit-compare-source.json` | `75ec3395188626bdabe376913585fc9eb96ec8a8dc226f7b246189636ac1871f` |
| `evidence/migration-inventory.json` | `c2589c65eebbfd9ab7ad332003f73f584feaaa6ab86a04099dcb50ab199841b5` |
| `images-package-a-mode-fix/image-receipt.json` | `ce900e550481286f0921dfee00ba5cff5a3a720f4a6856ae43be237f6c62b56e` |
| `images-scratch-a-mode-fix/image-receipt.json` | `a6fc82dd5349a3ea56903f0f37b420566431f6a7c50aa9e6ee52f73dbe58dfc6` |
| `aggregate-a-mode-fix/image-aggregate.json` | `d2dbd3071b0d3d2de69c947c829ba9018acf9fb7d2656bf994e2dca2f732efcd` |
| `evidence/release-inventory.json` | `d9a14f4fc93d88f5a8a9dbad0a83cce61efcefe9e02faa95ab4d3f9a0dab738a` |
| `evidence/baseline-receipt.json` | `027690572d48404a5a4f4181a067afcb78c098aeb333f746744ec46e5a16e23d` |

The [baseline receipt](/mnt/data/sn-testnet/mainnet-successor-release-astra-20261001/evidence/baseline-receipt.json)
is sealed with `evidence/pre-safe-SHA256SUMS`; all 642 selected files verify.

The earlier SN `2d53e6f2` / server `ecbf3aad` manifest and aggregate were
checked as historical comparison inputs only. All 17 new executable hashes
differ; all five selected contract creation/runtime pairs remain equal to
that earlier selection. No prior image or attestation claim is inherited.

The first package-image attempt correctly refused its API export. Under the
builder process's private `umask 077`, `copyBuildFile` passed `0755` only to
file creation, which masked the installed executable down to `0700`. The
failed OCI archive contains the exact parent binary bytes, but the executable
mode violates the existing strict readback. The scratch worker's non-root
user makes this an adjacent execution defect, not merely metadata drift. The
failed archive, command, exit and diagnostic are preserved, with no receipt.

The separate source fix `9bfa7cecf613fa97f3003ec28e621ed2b8679a52`, tree
`e9a1f3f9eba18c36469772502c20c2b6b3e7b736`, creates the new file privately,
then sets its exact declared permissions through the owned descriptor. It
preserves exclusive creation, immutable input bytes and independent source/
output hashing. The OCI verifier remains unchanged. The causal test owns
`000`, `077` and `777` umasks in child processes, exercises executable and
private input modes, and checks refused overwrites and source preservation.
All 89 builder roots pass normally and with race detection; vet passes. The
same new root on unchanged SN `1806b3b3` fails for the intended `077`/`0755`
assertion in both modes.

Using a tool built from the clean physical `9bfa7cec` checkout, fresh disjoint
image outputs under explicit `umask 077` pass all seven package-backed and one
scratch OCI readbacks. A separate direct tar read confirms the original failed
API has mode `0700`, while the corrected API and non-root scratch worker have
`0755`; all retain their exact original parent binary bytes. The offline
aggregate rechecks all 337 parent/supplement artifacts and all eight source/
OCI joins, advancing only local `source_to_image_verified`. Its content seal
is `sha256:a32b1712b9e6b8b04e345d746cc9c879f398eba2ad91c24e2d8ef56ae191e8a6`.
The image source remains SN `1806b3b3` / server `0b8e758d`; `9bfa7cec` identifies
the correction tool, not the applications inside those images. This baseline
does not include a second image build or independent image reproduction.

The [code qualification receipt](/mnt/data/sn-testnet/mainnet-successor-release-astra-20261001/evidence/mode-fix-receipt.json)
has SHA256 `10177a879cd4ef93d40f2fc707e726862f1b6dbe61c0e78eba11412a56033dcb`;
all entries in its sibling `mode-fix-SHA256SUMS` verify. The original complete
test attempt also retains two fixture setup failures caused by an overlong
temporary Unix-socket path; the subsequent 88-root baseline and 89-root fixed
suites use a short owned temporary directory and pass. No product assertion
or retained failed result was hidden by that fixture correction.

The migration inventory retains exact committed bytes of all 19 non-test
server `db*.go` and signal-migration files, including schema bodies outside the
builder's narrower default filename glob. Its Go syntax census finds 749
catalogue entries. This describes source, not an inspected database version,
applied migration, restoration rehearsal or authorized cutover. Build configs
are available; actual production service configuration, signed payout-policy
selection and published/deployed image identity remain missing.
The unsigned file inventory selects 56 artifacts totaling 806,363,754 bytes
and is identical on repeat. Its missing categories are `policy` and
`image-identity`; release completion, provenance proof and deployment approval
remain false. The local aggregate is retained as dependency evidence, without
inventing an immutable registry reference. An initial external inventory
config used inadmissible Foundry artifact labels; its refusal is retained,
and only those labels changed in the accepted configuration.

MG-02 stays open. A later release must rebuild its own exact source and image
set after Safe and the builder fix are integrated. Independent builder,
compiler/package archive and restore qualification, attestation/SBOM/scanner
policy, actual service/configuration/policy coverage and release approval are
still separate gates. No application service was started, image published,
transaction signed or mainnet deployment performed.
