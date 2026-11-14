# Owner Ledger native SDK build evidence

This candidate supplies a concrete Linux build of the exact SDK used by
[OWNER-SIGNING.md](OWNER-SIGNING.md). It does not qualify a physical Ledger,
firmware, the owner's existing key/path, or either deployed runtime metadata
artifact. The [scoped independent result](evidence/owner-ledger-native-sdk-qualification-20260930.md)
now covers actual native loading and the synthetic device boundary; building
the extension alone was not a behavioral test result.

## Source and artifact identity

| Input or output | Exact identity |
| --- | --- |
| Subtensor source commit | `67dcf7f791dc495064c293f080a0702cb433e51e` |
| Source Git tree | `0a266e8b9a746ba34c023d8fac09b6163f76b93f` |
| `Cargo.lock` SHA256 | `701071d3259e361325daa51dfa6f4768aa65bfceebf100d898d2f00060d73214` |
| Maturin | `1.7.8`, retained tool wheel and executable hashes |
| Rust | `1.89.0 (29483883e 2025-08-04)`; Cargo `1.89.0 (c24e10642 2025-06-23)` |
| Build interpreter | CPython `3.12.3`, Ubuntu x86_64 |
| Wheel | `bittensor_core-0.1.4-cp310-abi3-linux_x86_64.whl` |
| Wheel SHA256, build A | `a7be74dfd241171c839694c5abd6587183723b6e03f6659657706e538f9b5413` |
| Extracted `bittensor_core/bittensor_core.abi3.so` SHA256, build A | `77ffa6ac04459bc5d9895d225c2775827473d0db99c25802ea7aa32ae750a8d2` |

The Python distribution version is `0.1.4`; the Rust crate and native
`__core_version__` are `0.1.0`. Both come from that commit. Neither version is
substitutable for the source and artifact pins. The wheel contains a package
wrapper and the native extension; the owner adapter takes the **extension file**
path and its SHA256, not the wheel, package directory or `__init__.py`.

The untouched upstream default features include Ledger. On Linux its HID backend
is `hidapi/linux-native-basic-udev`, which needs no libudev headers or shared
library. PyO3 `0.23.5` enables `abi3-py310` and `extension-module`;
`merkleized-metadata` is locked to `0.5.1`. Native ELF inspection confirms
`PyInit_bittensor_core`, and dynamic dependencies are libc, libgcc_s and the ELF
loader. This build requires GLIBC `2.38` symbols. Its `linux_x86_64` tag makes no
manylinux portability promise. macOS, Windows, older glibc and other Python
interpreters require their own build and qualification; the ABI tag alone is
not evidence of successful execution on every Python version.

## Build and dependency provenance

All compiler outputs, temporary files, downloaded build tools and a separate
Cargo cache live under
`/mnt/data/sn-testnet/owner-ledger-sdk-20260930`. The source checkout is detached
at the commit above; source and lockfile remained unchanged. A read-only audit
verified the resolved SDK dependency closure: 564 packages, 27,875 registry
source files against their lock-hashed `.crate` archives, and three Git source
revisions/trees. This closure is a conservative workspace-resolution superset;
the retained Cargo feature tree and build logs identify compiled features. Cargo
cache `.cargo-ok` markers are recorded separately from compiled source.

The sealed evidence includes the exact build script, source/tool hashes, Cargo
metadata and feature graph, dependency audit, compiler/linker versions, complete
build logs, wheel members, ELF inspection, and artifact hashes. The builds used
the upstream manifests and defaults, `maturin build --release --locked --offline
--interpreter /usr/bin/python3 --compatibility linux`, eight Cargo jobs, no
incremental compilation, source-date epoch `1788818416`, `ZERO_AR_DATE=1`, and
Rust/C path remapping. No Cargo patch, lock update, feature removal or SDK source
change was used.

Fresh target directories A and B both built successfully but produce different
bytes. Vendored OpenSSL embeds the absolute installation paths of its engines
and provider modules; compiler path remapping does not rewrite those strings.
Build B's extension SHA256 is
`454e2e5d32a8cc9f25a47a6ce09dcdb2ffd44af50015d56f701b4224d14ed61d`.
Therefore a build from another path cannot be authenticated by assuming build
A's hash. A third build, from an empty target directory at A's original
canonical path, reproduced both A's extension and wheel byte-for-byte. The
retained logs and hashes establish reproducibility on that path with this
toolchain. Another builder must preserve the recorded paths or review and
authenticate its own artifact hash. There is no claim of independent-builder
or cross-host reproducibility.

## Native behavioral qualification

`owner_signing_native_sdk_test.go` requires four explicit environment values:

```sh
export SN_OWNER_LEDGER_BACKEND=/reviewed/bittensor_core.abi3.so
export SN_OWNER_LEDGER_BACKEND_SHA256=sha256:REVIEWED_NATIVE_EXTENSION
export SN_OWNER_LEDGER_SDK_SOURCE=/reviewed/subtensor-source
export SN_OWNER_LEDGER_PYTHON=/reviewed/python3
go test ./mainnet -count=1 -v -run '^TestOwnerSigningNativeSdk'
```

Absence of all four values skips these artifact-specific tests. A partial setup
fails. A qualifying run must report every root as passing, with no skip. The
source fixture files have independent fixed hashes from the pinned commit.

The roots exercise the complete native RFC78 proof vector and the independent
JavaScript proof envelope; an enabled-`CheckMetadataHash` owner-trim proof with
synthetic mortal-era payload values; preparation through the unmodified Go/Python bridge;
the actual native loader/digest/proof with only `LedgerDevice` substituted; and
wrong artifact hashes, symlinks, writable artifacts and invalid native files.
Synthetic device cases require the exact payload/proof/path/address-confirmation
arguments, reject key/version/digest/proof/signature mismatches, and retain a
private exact response without a second issuance after response publication.
No test invokes the real HID constructor. Existing portable-command, signature
verification, custody and import tests remain required adjacent checks.

The upstream localnet vector is a historical SDK compatibility fixture, not a
current owner-trim authorization. Passing it cannot establish an approved digest
for a deployed runtime or prove that runtime accepts enabled `CheckMetadataHash`.
The final owner bundle still needs its independently authenticated software
pins, actual platform/interpreter, physical Ledger/firmware/app review, existing
owner account/path match, and separate chain/custody authority evidence.
