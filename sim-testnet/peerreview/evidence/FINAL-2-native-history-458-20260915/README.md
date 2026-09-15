# Native historical-lock refusal after the runtime458 build

These retained captures concern SN c572d993de3116a68b1b2875d91a67c05c839e22,
xops42bfe0be2a7a7c51bbda87fb44886424604f509e and release-lock SHA-256
d11b2a41ca6e836f9267088f8899c4fb0faf53b78b3b9ca8804bab589cd63e7b.
The native refusal remains a failure; this bundle does not establish final
acceptance or a running soak.

The canonical CLI build completed with actual build/outer exits0 at
2026-09-15T00:19:36Z. Its genuine Git revision is c572d993, modified=false,
with trimpath enabled. The executable's SHA-256 is
e5375d52e46758c392ba538bcd95a1d1fb6e37eac5e689a2832dbf299c927638;
all13 repository observations match before/after. cli-c572 retains the build
receipts, command and metadata; the140,668,976-byte binary and caches are omitted.

Both full gate attempts passed source-freeze, source-integrity and binding
toolchain preflights, then closed with exit28 in the runtime-source preflight.
Producer ended00:21:09Z and aggregate00:23:59Z. Neither admitted any phase or
service owner. Their errors were timeouts fetching exact upstream source from
raw.githubusercontent.com. These are transport preflight failures, not executed
test suites or completed gates. Complete raw preflight/outer records are under
producer-c572 and aggregate-c572.

The actual read-only setup preview started00:24:42Z and ended00:24:56Z with
body/outer exit1 and no JSON plan output. It reported:

> persisted setup plan: validator evidence original release lock: release lock runtime identity is not the reviewed testnet runtime 458 release

The existing validator-evidence archive was passed to the current-runtime lock
validator after its canonical lock hash matched its original approval. The
historical source path therefore rejected the earlier reviewed455 lock.
native-preview-c572 retains the actual command, request, error, exits and
before/after receipts. All six recorded state files, executable bytes and lock
bytes were unchanged. There was no --apply, new plan hash or transaction.
No custody state or signed transaction payload is included.

At00:31UTC, two bounded read-only fetches of the exact failed455 dispatches.rs
URL returned HTTP200/exit0 using both default curl configuration and --disable.
Both bodies have SHA-256
82e0107406ae8ac60dc2f323ddf3a238749e630fdc2af18e179e86fd6066d483,
matching docs/spec/runtime-v455-source.sha256. github-connectivity retains
those replies. This demonstrates recovery at that observation time; it does
not pass either failed gate.

The reviewed correction is frozen at3ffc1277d1acd1e21b908c3b2cfd37612602e07b.
Its36-root normal and race matrices pass with exact root membership, complete
raw events and equal source/dependency/binary observations. Both actual archive
and restart readers have three consecutive normal passes. The race owner
closed2026-09-15T01:13:12Z. Normal binary SHA-256:
dc3b2ed96df3cfec634ee481b1d4494b51dff2683230a146c3ae3f17b64046dd;
race binary:
dce59c07d098f66c0b99a7a7a5c3a6e397a900fd61c52d900e2d7072f080f740.
The portable positive records are under qualification-positive; its RESULT.json
SHA-256 is39da839ee1afd9994f04925df36e0fa1774967ba361d2ee61d13e8b547b8be5b
and its SHA256SUMS digest is
1d28dde1dfebb2b08cae379e80f94fa369a69a772bf22d0415e41ff954cfae59.

The separate production causal restores only the old historical dispatch.
Both reader roots fail with the original455/current458 refusal, while the
static-validation and current-authority controls pass: exact2 FAIL/2 PASS,
37events, actual body1, converter/checker/outer0, unchanged candidate/dependency,
mutant diff and binary observations. Mutant binary SHA-256:
4130d1c4e143d81da057189b63d78febeec326071deb43caa28908f4623bc4fc.
Its wrong initial handoff operand and missing capture_root configuration are
retained as pre-build refusals. They did not run a compiler or test body.
qualification-causal contains the raw control records, both setup refusals and
a later seal-only shell operand refusal; no qualification was repeated for it.
Its RESULT.json SHA-256 is
6d64bc42b69d8e3606743a462d3cd4d6bb63e6ae91c818e29fa7edad343b35d4;
its SHA256SUMS digest is
22cf33e90a622d8657aa754d0615025b94f069e909e6d6e42b2ba8acebf14cc3.
correction-source retains the exact handoff, patch, source identities and
formatter receipts; the initially empty formatter diff is preserved alongside
the actual +15/-13 mechanical test-only diff. All three production files are
byte-identical before and after formatting.

final-lock retains the
read-only successor render, which reused the accepted c572 executable. It
closed0 at00:54:37Z with all19 repository/library observations unchanged.
The exact YAML SHA-256 is
5b8c412454adfde72f2cf51c682b5ce4b9b335a2095eaafc21bf4e92a153a4ab.
Only repositories.sn_go_source_hash changes; all other lock fields are equal.
The rawd11c544 render input and formatted3ffc127 differ only in test formatting,
which is excluded from this production Go source digest. The exact lock is
integrated at628f29ab9f3160c1df2cf5e836d3a34b38be00b4.
It has not changed the live campaign plan or the source under qualification.

Existing source overrides also let later gates reuse exact
clean upstream checkouts for their primary source loops. Neither changes the
outcomes of these retained attempts.

Run `sha256sum --check SHA256SUMS` from this bundle's directory to verify the
portable records. Nested manifests keep their original captured scope and
locators. Compiled binaries, caches, temporary worktrees, custody contents and
signed transaction payloads are omitted. These are local build/test and
read-only preparation receipts; they add no on-chain acceptance claim.
