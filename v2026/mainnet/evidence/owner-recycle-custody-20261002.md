# Owner recycle intent custody, 2026-10-02

The MG-06 offline owner transition could release a signing request after its
lock marker had been replaced, recreate a deleted signed journal while reporting
a finalized Recycle receipt, and re-export a valid earlier reservation after
signature import. Source `25aa15159f60e8d78dfadc1b90d027580e3a9346`, tree
`c99ae047ccc760643b5e01e51de8d22376d6e6f3`, closes these local custody failures.
The baseline is pushed main `c30f44ceddbd21e4bf2a4b9fb20eeeb41a898ff4`.

The public commands still have no signing, transmission or service operation.
The original action/approval schemas, owner, nonce, era, request, signature and
native extrinsic bytes are unchanged. No live transition or economic outcome
was performed or approved.

## Root cause and bounded correction

`owner_recycle_store.go` held an advisory flock on an open marker but did not
check whether that descriptor still named the private marker. Its writes accepted
a missing journal as a destination, even after the initial claim was complete.
Config/signature/checksum validation alone also accepted an older valid record
during active ownership. These are production call paths through `reserve`,
`export`, `import`, `status` and read-only `reconcile`.

The owner now holds the private physical parent descriptor, checks the marker's
inode, exact contents and single-link/private-file properties, and retains the
exact preceding journal digest. Reads and staged publication use that parent
descriptor; publication rechecks its predecessor before rename and rereads the
result after directory sync. Completed missing or changed custody permanently
fails the open instance with an integrity error. Restoring a path cannot revive
that instance. Cached export/import and the public command return boundary also
check original custody. Owner-descriptor close errors withhold public output.

Only an incomplete initial marker with no journal or an unused `reserved` row
may finish its interrupted claim. An incomplete marker beside an exported or
signed row cannot create fresh work. Ordinary ambiguous post-rename durability
can still reopen the same original marker/journal and recover the identical
request or signature. The existing readback-pending receipt recovery remains
unchanged. Local filesystem checks do not establish cross-host exclusion or
detect a whole-directory rollback performed while the process is stopped;
external original owner-key custody remains required.

The adjacent audit includes marker and parent replacement after publication,
cached finalized/import/status results, hardlinks, completed journal loss,
valid predecessor rollback and incomplete initial-claim recovery. No shared
storage helper, owner-trim source, action encoding, economic policy or release
artifact changed.

## Qualification

Author evidence is retained under
[`/mnt/data/sn-testnet/owner-recycle-transition-20261002/evidence/`](/mnt/data/sn-testnet/owner-recycle-transition-20261002/evidence/).
The [source fence](/mnt/data/sn-testnet/owner-recycle-transition-20261002/evidence/source-fence.json)
records clean SN and all six unchanged sibling trees, including server
`ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`, Go 1.26.6 linux/amd64, and the exact
baseline test overlay. Every new scratch/cache path is on `/mnt/data`.

- `go test -vet=off ./mainnet -run '^TestOwnerRecycle' -count=1 -timeout=15m -v`:
  23 roots passed, zero skips, package 26.512s. Eight roots are new; fifteen are
  existing typed encoding, approval, Ledger framing, public CLI, receipt and
  recovery neighbors.
- `go test -race -vet=off ./mainnet -run '^TestOwnerRecycle' -count=1 -timeout=20m -v`:
  the same 23 roots passed, zero skips, package 197.007s.
- `go vet ./mainnet` passed.
- Copying only `owner_recycle_custody_integrity_test.go` onto a clean exact
  baseline checkout makes all seven `^TestOwnerRecycleCustodyRefuses` roots fail
  at their intended boundaries, package 11.101s. The eighth new root preserves
  valid unused-claim recovery. The initial three-root reproduction and first
  fixed/recovery run are also retained, with no overwritten failed evidence.
- Independent Sol qualification on a separate clean checkout passes the same
  23 roots normally and with the race detector, plus package vet. Its identical
  test overlay on exact baseline source independently reproduces all seven
  intended fault failures. This is a separate scoped run, not reused author
  test output.

The [author receipt](/mnt/data/sn-testnet/owner-recycle-transition-20261002/evidence/receipt.json)
is `PASS_SCOPED`, SHA-256
`786639eb899a8ebed97440ddadfe14d465ba1d558f42c1e7c6733e070bd4e7a7`.
The verified [21-file manifest](/mnt/data/sn-testnet/owner-recycle-transition-20261002/evidence/author-evidence.sha256sums)
has SHA-256 `59a7f18243291df7b00d76dae6d38240686eb0198d58190ddd7152fe0e63c454`;
the [retained author bundle](/mnt/data/sn-testnet/owner-recycle-transition-20261002/owner-recycle-custody-author-evidence.tar.gz)
has SHA-256 `6b5f3b7c93880fb8a4809dc399c2ab355e261f23861f98e4fe91893e351b86d9`.
Its normal run began on the final source bytes immediately before their clean
commit; the source fence and per-file hashes bind the same frozen tree. Later
documentation edits do not alter that Go source scope.

The [independent Sol receipt](/mnt/data/sn-testnet/sol-mainnet-owner-recycle-independent-20261002/receipt.json)
is `PASS_SCOPED`, SHA-256
`d8e688690ec7c53e52d30b5bbdf6cf76af95a188225590602627ad92fbc11c51`.
It binds the same exact source/tree and baseline overlay, independently rehashes
all 21 author manifest entries, and records the author receipt/bundle hashes
separately. Neither receipt supplies live owner or economic authority.

The frozen source is backed up in
[`recycle-custody-source.bundle`](/mnt/data/sn-testnet/owner-recycle-transition-20261002/recycle-custody-source.bundle);
bundle verification passed. It is a later source increment than the retained
`3d1e2ecf` / server `ac86855d` release. No release was rebuilt for this audit.

## Contract-source integration

Merge `c4f78228a28a2655b2274c067c785de6fe8087e4`, tree
`a712dfe8391fe344d97eb90f29d32a710ff0265b`, combines the reviewed recycle branch
with pushed contract-admission main `a6f76388a392e459486574d74383865bfef9f82e`.
Both MAINNET/PRELAUNCH workstreams merged without conflicts. All five recycle
source/test files remain byte-identical to `25aa1515`; the three contract
source/test files remain byte-identical to `a6f76388`.

The [separate integration receipt](/mnt/data/sn-testnet/owner-recycle-transition-20261002/integration/receipt.json),
SHA-256 `3bef127a8f49f12ae694f97d131c752c23a815e65a91617519b106b4dbea5102`,
records all 26 selected roots passing normally, zero skips, package 43.244s,
and `go vet ./mainnet` exiting 0. The exact selector is
`^Test(OwnerRecycle|EvmReserveAdmission|EvmProxyAdmission)`: all 23 recycle roots
plus the three new contract-admission roots. This is a focused normal/vet
integration check; the separate component race receipts retain their original
source pins. No merged-source race, release or live qualification is claimed.
Later documentation reconciliation lists trim `0f7c8698`, contract admission
`ebf69b9` and recycle custody `25aa1515` as inputs to the next exact release.

## Runtime and economic limits

The full retained official v470 metadata, Blake2b-256
`8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`, passes the
unchanged native emission event and all fifteen state-storage profiles. Its
separate probe passes in 0.184s on baseline source. An earlier probe used the
recycle-only metadata projection and failed because that projection deliberately
omits `RegisteredSubnetCounter`; this was a fixture-scope mistake, not a runtime
failure. Both logs are retained. No metadata validation was relaxed or replaced
with hardcoded v470 storage values.

The [pinned source audit](../../docs/spec/runtime-470-audit.md) and
[transition authority](../OWNER-RECYCLE-TRANSITION.md) retain the actual v470
owner-call capability, per-subnet hyperparameter limit and admin-window rules.
The exact v470 artifact exception remains planning-only. A fresh independent
action approval, actual owner device and RFC78 material where applicable,
original request custody, explicit bounded transmission authority and current
chain checks still precede any live transition. The trim-only signing workflow
does not authorize a recycle action; this patch does not add such a signer or
submitter. No new live mode observation was collected in this audit.

Canonical inclusion and Recycle at that inclusion establish only the mode
transition. The native observer still leaves the pre-withholding miner
allocation, runtime quantization tolerance, provider entitlement, actual owner
recycling and target result null/unverified. Complete intra-block accrual/drain,
fixed-point normalization/truncation, zero-incentive fallback, execution-time
recipient generations and collateral/tail accounting are separate evidence.
The 10/90 weight input cannot promise the final native outcome under independent
validator/Yuma effects. Recycling also affects subsequent emission allocation
through the withheld ratio and provides no reserve funding. MG-06 remains open
for the actual approved transition and post-activation economic observation.
