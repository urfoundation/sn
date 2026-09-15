# Runtime configuration identity migration

This bundle records the correction for the retained setup failure
`coordinator repair configured strict domain differs`. It is local qualification
and release preparation, not a live-soak acceptance result. The preceding actual
setup failure and canceled gates are in the sibling
`FINAL-2-eccfae8-native-config-20260915` bundle after repository installation.

The implementation at `85c09583ce8a0728b4b33b04b19c9e7d7ea1272a` explicitly
preserves the reviewed original455 configuration identity while current runtime
authority remains exact458. An optional public pin is also bound into the new
setup plan hash. Original signatures, archive bytes and historical equality
checks remain intact. The final fixture-only source is
`5a411d2bf57e03c60a6ea3b3998d1cf401877342`; its only changed file is
`sim-testnet/runtime_config_identity_test.go`.

## Evidence scopes

- `source-85/` and `formatter-85/`: the implementation handoff, exact selected
  roots and descendants, causal operands and actual formatting receipts.
- `qualification-85/`: both original70-test matrices remain **69 PASS / one
  FAIL per mode**. Each mode also passed all58 declared child cases. The failed
  root authenticated the original repair, then used a post-approval mock RPC
  route in a plan comparison. Both actual failures and the pre-compiler capture
  refusals remain recorded. Corrected verification of retained events, using an
  absolute `body.exit` path, reproduced the four actual matrix outcomes without
  rerunning test bodies. The repaired replay does not turn the failed matrices
  into passes.
- `fixture-successor/`: the one-file correction preserves the approved route,
  independently reproduces its original resolved-input hash, and checks that
  changing a synthetic route changes only `config.render` intent. Terra's
  formatter produced no delta. The other69 bodies per mode, their helpers and
  all production code are unchanged.
- `fixture-qualification/`: the corrected root passed p1, p2 and p3 sequentially
  in normal mode and p1, p2 and p3 sequentially with race detection. Each fresh
  process has the exact one-test list, seven events, zero descendants and actual
  outer/body/converter/verifier exits0. All13 source/dependency observations and
  each mode's binary remain equal. The final race owner closed02:38:10UTC.
  Normal binary SHA-256 is
  `011e9497babf39ee8760a8fcaed7e3921a2403eac9f2ed92e43d2abeb6f77657`;
  race binary is
  `7dc0224ad65db3cacb61172d0b6fab13e14f10d16c3704de0379372035b1c06c`.
  Combined coverage uses the69 unchanged passing roots per mode on85c0958 and
  this one corrected root on5a411d2, with both source scopes explicitly retained.
- `causal-original-runtime-hashing/`: one-line restoration of the old production
  hashing yields exactly3 expected FAIL / two PASS, body1 and converter,
  checker and outer0, with42 events. All source/dependency/binary/diff
  observations match. The repaired root's failure is at its unchanged
  authentication call, before the fixture's changed planning assertions; the
  other four control bodies are unchanged. This scope is reused, not relabeled
  as a new execution on5a411d2.
- `readonly-parser/`: the successful updated parser build, SHA-256
  `5cee8ea3fe7dd23678d8ee9b646433ee2666390ad61cb1ace833caecebf327e6`,
  with13 equal source/dependency observations. It is accepted for read-only
  commands; a stamped canonical executable remains required for release writes.
- `final-lock/`: the read-only render closed0 at01:58:28UTC with19 equal
  repository/library observations. Exact YAML SHA-256 is
  `c437900d8cb2d29ff0d363ef629b88ceb8ace35976bd28bfd194a2d8dc6eb639`.
  Only `repositories.sn_go_source_hash` changes to
  `sha256:d7423269c0f2b23b9de9c282921312191e9c9f30bed4f4ad12aa1e4a3cd69f28`.
  The prepared request anticipated a possible protocol hash change; the actual
  protocol digest is unchanged because its source selection excludes the public
  manifest. The later test-only correction leaves all lock inputs unchanged.

Binary, cache and worktree contents are omitted; their retained hashes and raw
capture locators preserve the distinction. The original source receipts may
name absolute execution paths. The final top-level `SHA256SUMS` uses paths
relative to this portable bundle and can be checked with
`sha256sum --check SHA256SUMS` from this directory.

These artifacts do not claim new on-chain transactions, a running soak, either
complete release gate or final testnet acceptance. The numbered report retains
the remaining live validation obligations and the original deployment custody.
