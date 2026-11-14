# Original EVM bootstrap custody and borrowed-marker continuity

This MG-03/MG-08 increment hardens the original eight contract actions and the
separate readers that authenticate their retained installation. It supplies no
mainnet authority, signature, transaction, service activation or release approval.

## Source and failure

Writer source `cb9f3aa2` is based on SN `28ebfced`. Reader successor `1922981d`
includes that writer unchanged and separately corrects its read-only consumers.
Diagnostic successor `4e6b4a7e` restores only the original approval/predecessor
marker mismatch explanation. The physical custody algorithm is unchanged.
All use exact server `ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`, unchanged SN
`go.mod`/`go.sum`, Connect `e1b5d77b` and SDK `5d37be38`. No contract bytecode,
signed configuration, original signature, approval domain or serialized journal
format changes.

A filesystem lock protects an inode. Replacing its pathname with another file
containing identical marker bytes can admit another local owner while the first
owner still holds its old descriptor. The original action store checked private
permissions and took its lock only at opening. Its later publication could also
recreate a deleted completed signed journal from the owner's cached record.

Five deterministic tests on the frozen pre-fix source show that the owner could
send after marker replacement, recreate a missing signed journal, send after a
counted journal disappeared, accept a valid earlier journal during ownership,
and report a created contract after losing its newly published terminal journal.
The five-root invocation fails at all five intended assertions in 2.088s. These
are local synthetic transactions; they demonstrate lost local ownership and
allowance enforcement, not two successful on-chain executions of one nonce.

The read-only follow-up finds the same mistake in shared marker readers. After
an identical-byte marker replacement during a late native proof, the original
installation reader returned `InstallationComplete=true`. After replacement
during the eighth receipt read, the historical reader returned
`CanonicalReceiptsVerified=true`. Both tests fail on `cb9f3aa2` at their intended
assertions in a terminal 116.531s package. The installation fixture first completes
its nine-action synthetic installation; the receipt fixture retains the original
eight-action graph. Neither readback adds a submission.

## Correction and scope

The writer retains a descriptor for its original physical directory, checks the
named marker against the held inode and exact original marker bytes, and requires
private single-link regular files. Journal I/O and publication use that directory
descriptor. Every replacement compares the complete retained previous record;
missing, changed or rolled-back live custody is refused. The only permitted
initial creation is the existing incomplete, unsigned claim recovery boundary.
Publication reopens its exact result after directory sync.

The action owner rechecks the selected journal and every original prerequisite
after chain reads, before counting, after counted durability and before reporting
success. A fault after counting preserves that liability and performs no send.
Ordinary restart, completed receipt recovery and exact-byte uncertain-send retry
keep their original finite allowance.

All eight constructors use `openEvmSelectedActionStore`: reserve CREATE, vault
CREATE, coordinator CREATE, escrow registration, proxy CREATE, reserve link,
vault link and evidence CREATE. `bootstrap_contract_command.go` dispatches all
eight through the common owner; `bootstrap_chain.go` uses the reserve owner for
offline preparation. These are the direct writer call sites.

The separate reader commit adds a retained shared-marker object that rechecks
its physical parent, named inode, link count, permissions and exact bytes. The
original custody inspector rechecks every retained marker and record before
publishing complete allowance accounting. The historical receipt scope and
canonical successor adapter recheck all eight original marker objects at their existing
post-read checkpoints. Installation readback and the production validator's
`inspectBootstrapContractInstallationAt` consumer therefore cannot accept a
marker substitution observed during those reads. This does not turn a current
proof into complete Safe history or independent consensus authority.

## Qualification

Seven new writer roots pass normal in 75.660s. They cover the five failures above,
ten filesystem faults across every one of the eight action schemas, and loss of
all seven prerequisite markers after real reconciliation and after the eighth
action's counted publication. The latter case preserves its final original
attempt and retains exactly the seven earlier writes. The selected 71-root
writer scope includes those seven roots and 64 adjacent custody/recovery roots.
All 71 pass normal in two disjoint package-PASS partitions: 38 roots in 156.222s
and 33 roots in 698.561s. The first 38-root race partition passes in 861.401s.
The 33-root race invocation reaches its original 40-minute package timeout in
2400.284s after 22 roots pass; no assertion or data-race failure is reported.
That invocation remains failed. Exactly the eleven names lacking a terminal
pass are selected for disjoint three-root reserve-link and eight-root vault-link
continuations. The three reserve-link roots pass in a terminal 284.519s package,
including the originally interrupted claim-recovery test. The eight vault-link
roots pass in 805.372s. The exact 71-root race union is 38 + 22 + 3 + 8; the
failed timeout invocation is not relabeled. The retained selection checker
finds no missing, unexpected or duplicate passed names in either writer mode.

Two new reader roots pass normal in 112.587s on `1922981d`, with the complete
original graph, real Safe/contract execution and native storage proofs supplied
by synthetic fixtures. Both roots pass race in a terminal 742.734s package.
The public install/validator consumers retain their independent policy and
authority gates. Adjacent qualifications retain their own exact selections.

The initial adjacent reader invocation at `1922981d` passes ten roots and fails
`TestBootstrapContractReadinessRejectsChangedPredecessorLineage`; the terminal
package fails in 424.604s. The reader correctly refuses changed ancestry and
retains false readiness, but its new marker-byte mismatch message loses the
established `predecessor` diagnostic. Static review also finds an approval-cap
test depending on the same original message. Successor `4e6b4a7e` restores that
message without changing custody behavior. All nine original contract-readiness
roots then pass normally at `4e6b4a7e` in 82.504s, including the predecessor and
approval-cap diagnostics. Those nine roots plus current-command original
read-scope recovery pass race in a separate terminal 264.750s package. This is
not a race qualification of every receipt/installation command test. The
original failed invocation is retained.

Writer, reader `1922981d` and diagnostic successor package vet passed. Omitting
only the physical marker checkpoint makes `TestEvmCreateLostLiveMarkerRefusesSend`
fail at its assigned assertion in normal and race (0.409s and 1.478s).
Separately omitting only the owner's complete custody checkpoint allows the
eighth action to send after all seven predecessor markers disappear; its
normal/race controls fail at the assigned assertion in
38.015s and 120.465s. Corrected source preserves all seven earlier writes and
performs no eighth send under those faults.

The initial diagnostic baseline build collided with an in-progress source edit
and failed to compile; it is retained as invalid baseline evidence. The later
baseline uses immutable Go overlays from `28ebfced`. Test timeouts, incomplete
streams and later independent qualifications must remain individually scoped.

The separate [independent writer receipt](/mnt/data/sn-testnet/sol-mainnet-mg02-release-independent-20261001/custody-writer-receipt.json)
qualifies the seven new roots on a clean `cb9f3aa2` checkout: normal 93.044s,
race 542.849s and package vet pass. Its exact `28ebfced` overlay independently
reproduces the first three send/recreation failures. Its scope excludes the
author's broader action partitions and release artifacts. The receipt SHA-256
is `2044cffa929b403f420208412f44477627a5f57f36fe04407e2e2334b2699a29`;
an unchanged copy is retained in the author evidence directory.

The separate [independent reader receipt](/mnt/data/sn-testnet/sol-mainnet-mg02-release-independent-20261001/reader-custody-receipt.json)
passes the `1922981d` security pair normally in 144.895s, under race in 742.835s,
and vet. Its exact `cb9f3aa2` overlay reproduces both false-readiness failures.
On `4e6b4a7e`, the two security roots and both predecessor/approval diagnostic
roots pass normal in 105.654s, race in 687.534s and vet. Its receipt SHA-256 is
`80951a255b74af474758f86c787c349793cf3530688cc185b024b50c24d7ac13`;
the unchanged receipt is also retained with the author evidence. These checks
do not replace the separate adjacent selections or composed release qualification.

Local source, commands, exact root selections, module resolution, dependency
fences, unmodified logs and causal overlays are retained under
`/home/by/urnetwork/temp/sn-original-evm-custody-20261001/evidence`.
The writer and reader worktrees are its sibling `sn` and `sn-reader` directories;
the diagnostic successor and documentation are in `sn-reader-successor`.
The other local dependencies are Warp `7498864c`, proxy `6204ae7d`,
userwireguard `85fb1ca4`, glog `892ade4a` and goidenticons `325750b3`.
The execution platform is Go 1.26.6 on linux/amd64.

The [107-file manifest](/mnt/data/sn-testnet/original-evm-custody-20261001/author-evidence/MANIFEST.sha256)
has SHA-256 `465ae2415ba861a994015c00e38aadff4dbfdc06f0f95dec493e3fa3386d7dc6`.
It seals the raw streams, source patch, commands, dependency fences, causal
overlays, copied independent receipts and [exact selection closure](/mnt/data/sn-testnet/original-evm-custody-20261001/author-evidence/qualification-outcomes.json).
After the host reported root-device read errors elsewhere, the completed set
was copied to `/mnt/data`; both copies verified all 107 checksums. No source
read or test I/O error was observed in this qualification. Active source
worktrees were preserved and later scratch/cache paths were on the data volume.

## Adjacent obligations and release gate

The older root and trim stores still have separate opening-only marker patterns.
This increment does not qualify them. Their current production call sites do
not grant fresh effects: `root_service_runtime.go` constructs
`newRootOwnedSubmission(..., nil)` and omits the service Authority/Submitter
ports; `owner_trim_execution_command.go` constructs its reconciliation adapter
with nil authority, whose submission method refuses before RPC. The offline
root custody implementation imports public signatures only. `bootstrap_root.go`
and `bootstrap_chain.go` prepare local children and expose no chain submit port.
The standalone `rootActionStore` is used by internal/test composition rather
than a public production action command.

Those stores and the distinct five-marker `bootstrapChainReadinessState`
reader are outside this receipt's physical custody scope. The separate
[October 2 readiness qualification](bootstrap-readiness-custody-qualification-20261001.md)
now covers that reader and its passive three-marker subset at source `3d1e2ecf`.
The legacy root/trim writers remain deferred before enabling fresh native sends.
Neither scope is supplied by the original eight-action writer or reader correction.

These are local checks at explicit admission boundaries. They do not establish
cross-host signer exclusion, protection from a hostile filesystem owner, or an
atomic filesystem/network transaction. Missing live custody is a stop condition;
never copy a cached record into a missing completed journal to continue.

The SN `28ebfced` / server `ac86855d` release baseline predates both corrections.
The [exact custody successor](release-1d580d5e-serverac86-20261001.md) now includes
the qualified source at SN `1d580d5e` / server `ac86855d`, with matching repeated
local binaries, bytecode and OCI artifacts. This does not close independent
build/provenance or live release gates. Independent network/checkpoint approval, runtime
authority for live use, actual signer cutover, production acceptance, funding,
live installation and both validator roles remain open. The exact v470 artifact's
planning-only exception does not supply those live gates. No release, deployment
or activation gate closes here.
