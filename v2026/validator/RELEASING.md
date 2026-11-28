# Releasing the validator

Validator releases come from `sn` main. The subnet owner approves each release
before it is tagged.

## When a release is needed

- **A new subtensor runtime.**
  - A runtime that keeps every interface the validator consumes is admitted
    without a release, as an authenticated successor of the configuration's
    approved runtime.
  - Some checks need a reviewed identity in code: the `SubnetInfoRuntimeApi`
    version 3 tuples in `crv4/runtime_profile.go`, and a raised transaction
    version in `crv4/runtime_transaction_version.go`. When one of these is
    missing, validators hold steering (`validator steering hold:`) until a
    release admits the runtime.
- **A protocol or policy change**, such as a new settlement header or a new
  amendment reader.
- **A fix.**

## Reviewing a runtime

Review as early as possible. A subtensor release is tagged and reaches testnet
before mainnet, so a release can often ship before the mainnet upgrade.

1. Read the runtime identity: `state_getRuntimeVersion` at the upgrade block,
   giving spec, impl, transaction and state versions, and the runtime APIs.
2. Compare it with the last reviewed runtime:
   - the runtime API identities and versions;
   - the metadata, a byte diff of `state_getMetadata` either side of the
     upgrade;
   - the consumed calls, events and storage that `crv4/runtime_profile.go`
     lists;
   - the signed extensions.
3. If the transaction version rose, check that every consumed call keeps its
   index and encoding. Do a read-only `validate_transaction` dry run with a key
   that holds no funds: the old version must give `BadProof` and the new one
   `Payment`.
4. Add the reviewed identity in `crv4`, with the captured metadata under
   `crv4/testdata` and tests that pin it. A later identity stays refused until
   its own review.

The network operator's reserved upload lane runs the same admission. Each
review ships in the operator's api at the same time as the validator release.

## Cutting a release

1. Merge the reviewed change to `sn` main, after the validator, `crv4`,
   `clientauth` and `hotkeyauth` tests pass on the merged tree.
2. With the owner's approval, tag the merge commit, for example
   `validator-v2026.11.24` or `validator-v2026.11.24-runtime-495`.
3. Build the archives from the tag with every sibling repository at a
   recorded commit. List those commits in the release notes.

   ```sh
   make -C validator package VALIDATOR_VERSION=<tag> GOOS_ARCH='linux/amd64 linux/arm64'
   (cd validator/build/releases && sha256sum *.tar.gz > SHA256SUMS)
   ```

4. Publish a GitHub release on the tag with the archives, `SHA256SUMS`, the
   sibling commits and what changed for operators. Say whether the release is
   needed before a known runtime upgrade.
5. Announce it to registered validators.
