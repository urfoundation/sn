# Offline Safe release verification

`safe-release-verify` verifies explicit published proxy/singleton artifacts
without selecting an account or supplying authority:

```sh
sn-mainnet safe-release-verify --version 1.4.1 --variant Safe \
  --archive /absolute/path/safe-contracts-1.4.1.tgz
```

Both `--version` and `--variant` are required. Supported versions are 1.4.1 and
1.5.0, and variants are `Safe` and `SafeL2`. There is no automatic latest-version
choice or variant inferred from a chain label. The public archives and catalog
are retained under [safe-release](safe-release/README.md).

The bounded command checks the exact whole archive, package name/version,
published artifact bytes, proxy and selected singleton creation/runtime Keccak,
ABI, compiler version/settings, compiler-output agreement, source content hashes
and complete storage-layout hashes. Safe-owned input files match the pinned Git
release; external compiler inputs retain their own locked package provenance.
The verified layouts place singleton at slot zero and the Safe nonce at slot
five. Safe proxy storage is not ERC-1967 storage.

The result has status `published-release-artifacts-verified`, an exact content
seal, selected artifact identities and explicit false fields for independent
rebuild, current chain, account address, initializer-owner binding, Safe authority,
signing payload, executable scope, network effects, installation and activation.
Exit zero means this local artifact check passed. Exit one reports an unresolved
file/provenance/output/cancellation error; exit two rejects CLI/profile scope.
The command neither extracts archives nor runs installation/build scripts.

Published compiler input/output agreement is not an independent compiler rebuild
or audit attestation. Factory and fallback-handler profiles are outside this
bounded verifier. It verifies no current chain-964 deployment or registry entry,
and no account owner/threshold/modules/guards/fallback/nonce or signature. Exact
live proxy and singleton code, reviewed authority paths, pending nonce custody,
Safe transaction digest/signature semantics, outer relayer liability, canonical
receipt with Safe-inner success and the evidence immutable domain remain separate
prerequisites.

The retained eight contract actions fix `initializerOwner`. Any new Safe must
have that exact address; a different address requires a separately authorized
ownership migration. These artifact results cannot relabel that owner, revise an
old signed plan, authorize a successor, replay completed actions or grant more
spending. The existing-versus-new Safe choice remains independent of this command.

The [scoped Sol qualification](evidence/safe-release-profile-qualification-20260929.md)
passes six new and four adjacent roots normally and with race detection, plus
all sixteen intended causal executions. It verifies this offline artifact
boundary; live installation and executable successor authority remain open.
