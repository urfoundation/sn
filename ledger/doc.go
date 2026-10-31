// Package ledger builds and verifies a settlement v2 pool ledger
// (VERSION2-FINAL.md §6, §8.2, §9.2). It is pure and deterministic: no
// database, network or chain access. Callers pass chain reads in, and
// reach objects and records through the Fetcher, Custody and HeadOracle
// interfaces. The operator server builds roots with Build, the validator
// audits them with Verify, and `snclaim v2 check` runs Verify too.
//
// # Objects
//
// A root's ledger is a set of content-addressed objects named by Ref, the
// sha256 of their bytes, written "sha256:" followed by 64 lowercase hex
// digits (ParseRef, Ref.String, RefOf). The header is canonical JSON of at
// most MaxHeaderBytes, and the ledgerHash committed on chain is its sha256:
//
//	{"schema":"urnetwork-payout-ledger-v2","no_id":N,"epoch":E,
//	 "epochs":[{"epoch":K,"providers":["sha256:…",…]|null},…],
//	 "accounts":["sha256:…",…]}
//
// It covers the epochs p+1 … e, at most MaxCoveredEpochs of them, where p is
// the latest finalized root's epoch or firstEpoch − 1. "providers": [] is an
// epoch without rows; "providers": null is an epoch the operator declared
// unrecoverable. An epoch that carries a phase C recovery component (below)
// lists it after its providers, and only then:
//
//	{"epoch":K,"providers":[…],"recovery":[{"epoch":S,"amount":A,"providers":["sha256:…",…]|null},…]}
//
// The header lists chunks:
//
//   - provider chunks: ProviderRowBytes-byte rows (protocol.LedgerUsageRow),
//     client_id(16) ‖ coldkey(32) ‖ usage_bytes(8) ‖ head_excluded(1),
//     big-endian, sorted by client id;
//   - account chunks: AccountRowBytes-byte rows (merkle.LedgerAccount),
//     coldkey(32) ‖ cumulative(16), in account position order.
//
// A chunk list has ChunkRows rows in every chunk except a non-empty last
// one; an empty list has no chunks.
//
// EncodeHeader and DecodeHeader convert a Header. DecodeHeader accepts only
// the canonical bytes: it decodes with unknown fields refused, refuses
// trailing bytes, and requires that re-encoding reproduce the input; then it
// checks the schema, null only for providers, the epoch count and every
// reference. EncodeProviderChunks, DecodeProviderChunks,
// EncodeAccountChunks and DecodeAccountChunks convert chunk lists, checking
// the chunk shape and row encodings.
//
// # Building a root (server)
//
// Build takes a BuildInput:
//
//   - NoID, Epoch (e) and FirstEpoch;
//   - Previous: the latest finalized root as read from chain, with its
//     accounts in position order, or nil for the pool's first root. Build
//     refuses accounts that do not rebuild it (ErrPrevious);
//   - Epochs: one EpochInput per covered epoch p+1 … e, in order. Rows are
//     the epoch's provider rows (§6.2): sorted by client id and unique, with
//     nonzero client ids and positive usage, the zero coldkey for a provider
//     without a paying wallet, and HeadExcluded exactly for providers with an
//     active fleet binding at either boundary. Null declares the epoch
//     unrecoverable, only on an explicit operator command naming it. For an
//     epoch built by an earlier root, Chunks holds the references it was
//     first built with and Rows its rows (DecodeProviderChunks of those
//     chunks): Build refuses rows that encode differently (ErrFixedInputs).
//     Leave Chunks nil to build an epoch the first time, or to rebuild it on
//     an operator command;
//   - FundedThrough: vault2's fundedThrough[k] for every covered epoch.
//
// It walks the epochs (§6.4): f_k = fundedThrough[k] − fundedThrough[k−1],
// where fundedThrough[p] is the previous root's total (0 for the first
// root). An epoch whose rows have eligible usage is allocated f_k plus the
// carry with protocol.AllocateLedgerEpoch, and the carry clears; an epoch
// with no rows, no eligible usage, or null adds f_k to the carry. Each
// coldkey's positive summed delta is added to its account; coldkeys without
// one get new accounts, appended in ascending coldkey order. Previous
// positions never move.
//
// The Ledger it returns has Commit (epoch, root, total, leaf count and
// ledgerHash, the arguments of commitDistribution), the Header bytes, the
// Accounts and their Tree (proofs for the account endpoint, paths for
// answering demands), Objects (every object, keyed by reference: publish
// the chunks with put-if-absent before the header, Ref(Commit.LedgerHash)),
// and Epochs: per covered epoch its chunks, f_k, and when it allocated, the
// amount and every coldkey's delta, which readers need per epoch. The
// output depends only on the input.
//
// Build refuses with ErrTotalNotIncreased when fundedThrough[e] does not
// exceed the previous total, and with ErrEpochNotAllocated when epoch e
// itself does not allocate; in both cases the operator commits nothing for
// e, and the next root covers it. Other refusals: ErrInput (a nil
// context), ErrCoveredEpochs, ErrTooManyEpochs, ErrPrevious, ErrFunding,
// ErrEpochInput, ErrFixedInputs, the row errors (ErrRowZeroClientID,
// ErrRowZeroUsage, ErrRowsUnsorted, ErrRowDuplicate, ErrUsageOverflow) and
// ErrHeaderTooLarge.
//
// # Recovery components (phase C, VERSION2-FINAL.md §16 step 6)
//
// The v1 sweep moves v1 carry into vault2, which assigns it as surplus to an
// epoch's funding. A recovery component allocates a recovered source
// epoch's v1 funding A by that source epoch's own provider rows, built with
// the v2 rules over its own earning window, wallets and head bindings, with
// protocol.AllocateLedgerEpoch; the deltas sum exactly to A. The covered
// epoch's own rows then allocate f_k less the amounts the components
// allocated, plus the carry, as in the walk above. A component whose source
// epoch has no rows, no eligible usage, or is unrecoverable leaves A in the
// ordinary funding. Build refuses components that are not ascending
// distinct source epochs before their epoch with positive amounts
// (ErrRecoveryInput) and amounts beyond f_k (ErrRecoveryFunding). Source
// rows are fixed inputs like an epoch's: EpochInput.Recovery[i].Chunks, and
// custody's valid rows under the source epoch.
//
// Which source epochs a pool recovers is the owner's decision, fixed in
// code (ScheduledRecovery, keyed by vault2 and no_id; mainnet pool 1
// recovers epoch 3). Where it lands is a fact of the chain: PlanRecovery
// places it at the first epoch at or after firstEpoch whose own vault2
// SurplusAssigned amounts reach the scheduled total, and a root carries it
// only when it covers that epoch. Builder and verifier both check each
// source's amount against vault1's terminal RootMissed entitlement
// (RecoverySchedule.CheckSource).
//
// # Auditing a root (validator, snclaim)
//
// Verify takes a VerifyInput read from chain at one pinned block: NoID,
// FirstEpoch, the Pending root and its Seq, the latest finalized root
// (Previous, nil before the first), and fundedThrough for every covered
// epoch. Its sources are:
//
//   - Fetcher, which opens published objects (GET /sn/ledger/object).
//     Objects implements it over a map. Verify reads at most MaxFetchBytes
//     (DefaultMaxFetchBytes, 2 GiB, is the --ledger_max_bytes default) for
//     objects not in custody, and checks each against its reference. The
//     budget is per root: when retrying an audit, pass what earlier
//     attempts left of it (Result.FetchedBytes counts each attempt);
//   - Custody, durable: objects held (held objects are trusted and cost no
//     budget), and per pool epoch the chunks of rows found valid and the
//     null declarations recorded. Verify keeps every object it fetches,
//     records an epoch's rows as valid once they pass checks 4 to 6 and
//     allocate, and records every null it accepts. MemoryCustody is an
//     in-memory Custody;
//   - Heads, which returns for an epoch the candidates, the client ids with
//     a FleetBound event whose validity covers it, each mapped to whether
//     bindingAt is active at either boundary. A row's head_excluded must be
//     1 exactly for the candidates mapped to true. Nil skips check 6, for
//     tools without archive state.
//
// Verify returns an error only for its own inputs (ErrInput, ErrFunding),
// its context, or an error of Custody or Heads; fix or retry those.
// Otherwise the Result holds the outcome. Check is the first check that
// failed and Err its error, which wraps exactly one of the error values of
// that check (check 2's ErrPreviousRebuild also wraps its cause):
//
//	1 CheckHeader         ErrLedgerHash, the DecodeHeader errors, ErrHeaderNoID,
//	                      ErrHeaderEpoch, ErrCoveredEpochs, ErrTooManyEpochs
//	2 CheckPrevious       ErrPreviousNotTight, ErrPreviousRebuild
//	3 CheckFixedInputs    ErrFixedInputs, ErrNullAgainstValidRows
//	4 CheckChunks         ErrChunkHash, ErrChunkLength, ErrChunkShort,
//	                      ErrChunkEmpty, ErrRowEncoding
//	5 CheckRows           ErrRowZeroClientID, ErrRowZeroUsage, ErrRowsUnsorted,
//	                      ErrRowDuplicate, ErrUsageOverflow
//	6 CheckHeadExclusion  ErrHeadFlagSet, ErrHeadFlagCleared
//	7 CheckAllocation     ErrEpochNotAllocated, ErrAccountOmitted, ErrAccountDelta
//	8 CheckAccounts       ErrAccountMoved, ErrAccountLowered, ErrAccountDuplicate,
//	                      ErrAccountUnsorted, ErrAccountZeroDelta
//	9 CheckTree           ErrTreeLeafCount, ErrTreeTotal, ErrTreeRoot
//	  CheckFetch          ErrObjectUnavailable, ErrByteBudget
//	10 CheckRecovery      ErrRecoveryUnexpected, ErrRecoveryMissing, ErrRecoveryAmount
//
// The checks run in the order 2, 1, 4 for the pending account chunks, 10,
// 3, 8, 9, then per covered epoch 4, 5 and 6 with its allocation, each
// recovery component's source rows first (head bindings of the source
// epoch), then 7. The previous root comes first so that a failed root still
// has accounts to protect, and the pending accounts are read before the
// other checks so that their faults are known whatever else fails; a
// recovery dispute is therefore found with the root reproduced, and leads
// to a refusal, not a demand. VerifyInput.Recoveries holds the components
// the chain calls for, per covered epoch; the header must list exactly
// those (check 10).
//
// The Result also says what §9.3 needs. Previous and PreviousAccounts are
// the finalized root's verified tree and accounts. Accounts and Tree are the
// pending root's published accounts and their tree, and Reproduced reports
// that Tree is exactly the pending root. Faults lists each previous account
// whose published row is lower (merkle.LedgerFaultLowerValue), names another
// coldkey or is missing (merkle.LedgerFaultWrongLeaf): when Reproduced, that
// is what checkPath finds at the leaf. Missing lists the objects that were
// unavailable or did not match their reference.
// FetchedBytes counts the bytes fetched. Result.Action applies §9.3:
// ActionApprove when every check passed; ActionChallenge when Reproduced
// and Faults is not empty (send checkPath with PreviousAccounts[i],
// Previous.Proof(i) and Tree.Path(i)); ActionDemand when objects are
// missing or the rows do not reproduce the root and a finalized root exists
// (demand, in DemandSet's order, a value-weighted sample of previous
// accounts and every fault's index, each demand with PreviousAccounts[i]
// and Previous.Proof(i)); ActionRefuse otherwise. A budget too small to
// read the pending accounts also leads to ActionDemand.
//
// # Demands (validator, snclaim)
//
// demandPath and checkPath take the index of the finalized root an account
// is proven against, any finalized root, so a withheld newest tree cannot
// stop demands. A watcher protects the newest finalized root whose account
// rows it holds and has verified: normally the latest, Result.Previous and
// Result.PreviousAccounts; when the latest root's objects are missing, an
// older one it can still verify. Faults(previous, accounts) lists what a
// pending root's published rows show against any such root, as Verify
// does for the latest.
//
// DemandSet selects the §9.3 demand set for ActionDemand from the protected
// root's accounts and their faults, the doubling count and again, the
// earlier unanswered indexes. Both come from vault2's events since the
// latest finalized root was finalized: each earlier pending root over it
// that was replaced (DistributionCommitted.replacedSeq) or deleted
// (DistributionDeleted) while a demand was open, PathDemanded without
// PathAnswered (both keyed by seq, rootIndex and index), adds one doubling
// and its unanswered indexes. The set lists, in the order to send it, a
// sample of DemandSample (256) << doublings of all of the protected root's
// accounts, never more than there are, drawn by value without replacement
// with randomness read from a CSPRNG after the commit was seen, in the
// order drawn; then every index in again the sample did not draw; then
// every other fault's index. Demands cut short by the challenge window or a
// fee budget are therefore a value-weighted sample whatever the operator
// published or left unanswered (§4.2 G2). Each demand pays exactly
// demandFeeWei and carries the protected root's index, its account i and
// the proof of i against it. DemandSet refuses an index outside the
// protected root (ErrDemandIndex) and returns the random source's error.
package ledger
