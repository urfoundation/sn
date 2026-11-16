// Package ctf is the pure core of capture the flag (CAPTURETHEFLAG.md): the
// country list and slots, the census and its hash, the seed and the draw, the
// amounts walk, the CTF ledger and its codecs, Build for the operator server
// and Verify for the validator's audit and `snclaim v2 check`. Like package
// ledger, on whose accounts and sum tree it builds, it does no database,
// network or chain access: callers pass chain facts in and reach objects
// through ledger.Fetcher and ledger.Custody. Section numbers are
// CAPTURETHEFLAG.md's unless named otherwise. Integers in rows are unsigned
// big-endian; amounts are rao in 16 bytes (below 2^128).
//
// # Pool, countries and slots (§3.1, §4.1)
//
// NO n's CTF pool is the vault2 pool PoolID(n) = 2^32 + n, n < 2^32; UR's is
// 4,294,967,297. L is the 249 ISO 3166-1 alpha-2 codes of
// server/model/iso_country_names.go, lowercase, in ascending code order
// (Countries, CountryAt, Country.Index, Country.Name, ParseCountry); Kosovo
// (XK) is not in L. The countries object is L's codes concatenated, 498 ASCII
// bytes "adae…zw", named by CountriesRef
// (sha256:1dc01d610882808f4f3c56e8025d6fdac9d2b1cb8ca12c0458f4a367d2a17986),
// which census and ledger headers name as "countries".
//
// A slot is (country, role, rank), role 1 provider or 2 extender, rank 1 to 5;
// S = NumSlots = 2,490. Slot order is country, then provider before extender,
// then rank: slot (c, r, i) is SlotIndex(c, r, i) = 10·c + 5·(r − 1) + i − 1,
// c the country's position in L. A pot is (country, role), 498 of them, at
// PotIndex(c, r) = 2·c + r − 1; pot j's slots are 5·j … 5·j + 4.
//
// # The census (§4.2, §4.3)
//
// A census row (CensusRowBytes, 52 bytes) is one candidate:
//
//	role(1: 1 provider, 2 extender) ‖ country(2, ASCII, lowercase) ‖ unit(32) ‖
//	unit family(1: 4 or 6) ‖ client_id(16)
//
// The country is the candidate's own published country at end(k). The unit is
// the keyed egress prefix hash of WHITEPAPER.md §8.4 and §8.6.2,
// HMAC-SHA256(egress_hash_key, "urnetwork/egress-prefix/v1" ‖
// masked_prefix), its IPv4 /29 first, else its IPv6 /48: exactly what the
// server's model.VerifyEgressIpHashWithSettings returns under the signed
// policy's granularity, and the family says which. A census is canonical when
// its rows are in strictly ascending (role, country, unit, client_id) order,
// each with a valid role, a country in L, family 4 or 6 and a nonzero unit
// and client id; a client appears at most once per role; and every row of a
// unit has the same family. Rows are a ledger chunk list: ledger.ChunkRows
// (131,072) rows in every chunk but a non-empty last one, none for no rows.
//
// The exclusion object holds counts only, 8-byte rows
//
//	country(2: a code of L, or "zz" for none) ‖ role(1) ‖ reason(1) ‖ count(4)
//
// in strictly ascending (country, role, reason) order with positive counts.
// Each miner the census left out is counted once, in its country at end(k),
// under the lowest-numbered reason that excludes it: 1 revoked or inactive, 2
// not online at the end, 3 not Public, 4 no valid location, 5 own network, 6
// head-bound, 7 no wallet, 8 duplicate address, 9 country changed, 10 too
// little presence. Reasons 9 and 10 therefore count the near misses of §9.1.
//
// The census header is canonical JSON, compact, keys in this order, integers
// as JSON numbers, no null:
//
//	{"schema":"urnetwork-ctf-census-v1","no_id":N,"pool_id":P,"epoch":K,
//	 "countries":"sha256:…","rows":["sha256:…",…],"candidates":C,
//	 "excused_samples":[S,…],"excluded":"sha256:…"}
//
// with P = 2^32 + N, C the row count, and the excused samples ascending
// distinct quarter-hour indexes below 672. censusHash = sha256(header) is
// what commitCtfCensus(k, n, censusHash) commits. EncodeCensus sorts and
// encodes a census, refusing one that would not be canonical; DecodeCensus,
// DecodeCensusHeader, DecodeCensusRows and DecodeExclusions accept only
// canonical bytes, decoding the header with unknown fields refused and
// requiring that re-encoding reproduce it.
//
// # The draw (§4.4)
//
//	ρ_k        = DrawRound(ts(B_k)): the first quicknet round with
//	             1692803367 + 3·(r − 1) >= ts(B_k) + 60, ts the EVM timestamp
//	             of the commitment's block, in seconds
//	rand_k     = sha256(signature), the 48-byte signature verified with
//	             e(σ, g2) = e(H(sha256(r:u64)), pk) against quicknet's key
//	             (Pulse.Verify; sn/crv4's constants)
//	σ_k        = keccak256("urnetwork/ctf-draw/v1" ‖ chain_id:u64 ‖ netuid:u16 ‖
//	             pool_id:u64 ‖ k:u64 ‖ censusHash ‖ rand_k)          (Seed)
//	ticket(u)  = keccak256(σ_k ‖ role:u8 ‖ country:2 ‖ unit:32)     (Ticket)
//	member(x)  = keccak256(σ_k ‖ role:u8 ‖ unit:32 ‖ client_id:16)  (MemberTicket)
//
// DrawCensus first leaves out every candidate with an active fleet binding at
// either boundary (P5, X5), from the same map ledger.HeadOracle gives. Each
// remaining unit enters, per role, the draw of its lowest client id's country
// (§4.2); |U_{c,r}| units are in pot (c, r)'s draw, and the min(5, |U_{c,r}|)
// smallest tickets win, ranked 1 … by ticket; in each winning unit the
// candidate with the smallest member ticket is paid. Ties, which need a keccak
// collision, go to the lower unit or client id. Leaving bound candidates out
// before the draw, rather than refusing a root whose winner is bound (§8
// check 3), keeps a root buildable: a committed census cannot be rebuilt.
//
// A committed census that is not canonical, whose bytes match its committed
// hash, gives no draw, as an uncommitted one does: §8 check 1 refusing it
// would leave the epoch, and every root after it, unbuildable. Build and
// Verify read a committed census in one order, the header, then each row
// chunk, then the exclusion object: a header that is not canonical or names
// another pool or epoch, or an object longer than any canonical census holds
// (256 KiB, 131,072 rows, 5,000 exclusion rows), ends the reading as not
// canonical; otherwise every object must be had before the rows and counts
// decide. The auditor reads an over-long object through its hash, within its
// byte budget. Objects served with other bytes are missing, never a reason to
// cancel a draw.
//
// # Amounts (§4.5 to §4.7)
//
// The walk covers p+1 … e. It starts owing X = O_p, the pots and pending
// awards of the root built on (its last epoch's pot out and pending rows;
// zero for the pool's first root): that root's walk gave the carry account
// everything it did not pay less what was still owed, so the carry account's
// cumulative less everything returned and written off is exactly O_p, the
// design's X. The carry contract's residual, the stake of its own it keeps on
// vault2's escrow hotkey so that its claims always land (§4.6), comes from
// outside the pool and is never recycled: it is in no account and no
// recycledFor, so it changes neither X nor any return. For each covered epoch
// k, in order:
//
//	f_k      = fundedThrough[k] − fundedThrough[k−1]   (the previous total for p)
//	raw      = recycledFor[k] + the returns of uncaptured epochs before it;
//	           an epoch with f_k = 0 (deferred) rolls its raw to the next
//	R_k      = min(raw, X)                       (stake beyond X is a donation, fresh)
//	M_k      = R_k + 8 >= X                      (matured: the carry is back)
//	credit   = X if M_k; R_k + 8 if 0 < R_k; else 0;     X ← X − credit
//	F_k      = f_k − credit, or 0 with the shortfall credit − f_k taken from the
//	           pots, the largest first (ties to pot order), then pending awards
//	a_{k,s}  = floor(F_k / 2490) + (1 if s < F_k mod 2490)   for slot s
//	per pot (c, r), with P its pot and W its winners, q = floor(P/5):
//	  rank i ≤ W gets a_{k,(c,r,i)} now and q, plus 1 if W = 5 and i ≤ P mod 5,
//	  now if M_k, else as a pending award (when positive)
//	  pot out = P − W·q − (W = 5 ? P mod 5 : 0) + Σ_{i>W} a_{k,(c,r,i)}
//	if M_k, every pending award is paid in full
//
// An epoch without a draw has no winners, so all of F_k joins the pots slot by
// slot. The carry contract's coldkey receives Σ f_k less everything paid,
// which is the obligations less what is still owed, never negative; the
// root's total is therefore fundedThrough[e]. A return recorded for a last
// covered epoch that funded nothing would roll past the root: Build refuses
// it and Verify fails the root with ErrReturnRolls (CheckAmounts).
// STSettlement2's commit window makes it impossible, as a root's last epoch
// is captured before its root is committed.
//
// Where §4.5 is wrong, this is the closest correct rule:
//
//   - Its M_k also needed f_k >= X_k, and its F_k = f_k − R_k could be
//     negative. With no fresh funding (validators not supporting CTF, §4.7) a
//     single rao lost to rounding then left every later epoch unmatured, so
//     pending awards never paid, and a capture a rao short of the return made
//     F_k negative. Here a shortfall the funding cannot cover is taken from
//     the pots instead, at most a few rao.
//   - Its tolerance was one cycle's rounding (CarryRounding, 4 rao). A
//     partial return then leaves the rounding of several cycles owed, a late
//     root's return and the next one's can come back in one epoch, and
//     whatever stays owed at a root's end is owed again by the next, so at the
//     4-rao bound per cycle the residual never shrank and pending awards piled
//     up for good. Here the tolerance is CarryTolerance, two cycles' rounding
//     (8 rao), and a partial return writes off as much: rounding left owed
//     shrinks by at least 4 rao with every single return and never grows with
//     two (TestCarryAcrossRoots). A write-off later returned is a donation,
//     counted as fresh.
//
// In every case the deltas sum to the funding (TestWalkConservation).
//
// # The ledger (§5)
//
//	award row   (AwardRowBytes, 117)  role(1) ‖ country(2) ‖ rank(1) ‖ unit(32) ‖
//	            client_id(16) ‖ coldkey(32) ‖ slot_rao(16) ‖ pot_rao(16) ‖
//	            deferred(1: 0 pot share paid now, 1 pending)
//	            by role, country, rank; ranks 1, 2, … per country and role
//	pot row     (PotRowBytes, 40)     country(2) ‖ role(1) ‖ units(4) ‖ winners(1) ‖
//	            pot_in(16) ‖ pot_out(16); the 498 pots in pot order
//	pending row (PendingRowBytes, 60) award epoch(8) ‖ role(1) ‖ country(2) ‖
//	            rank(1) ‖ coldkey(32) ‖ amount(16)
//	            by award epoch, role, country, rank; amounts positive
//
// Each covered epoch has an awards object (its winners), a pots object, and a
// pending object (the pending awards outstanding after it). An award row is
// deferred exactly when its pot share is a pending award: the epoch is not
// matured and the share is positive. pot_in is the pot carried in less any
// shortfall taken from it. An empty list is the empty object,
// sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855,
// which a verifier need not fetch. Accounts are the v2 ledger's
// (VERSION2-FINAL.md §6.1, §6.5): account chunks of coldkey(32) ‖
// cumulative(16) in position order, committed by the sum tree
// urnetwork-ledger-sum-tree-v1, new accounts appended in ascending coldkey
// order (ledger.ApplyDeltas). The carry account is the carry contract
// coldkey's ordinary account. The header is canonical JSON, at most 256 KiB
// and 96 covered epochs, null only for a census:
//
//	{"schema":"urnetwork-ctf-ledger-v1","no_id":N,"pool_id":P,"epoch":E,
//	 "countries":"sha256:…","carry_coldkey":"0x…",
//	 "epochs":[{"epoch":K,"census":"sha256:…"|null,"round":R,"awards":"sha256:…",
//	 "pots":"sha256:…","pending":"sha256:…"},…],"accounts":["sha256:…",…]}
//
// "census" is the committed census hash, null when none was committed;
// "round" is ρ_k for an epoch with a draw and 0 otherwise; "carry_coldkey" is
// 0x and 64 lowercase hex digits. ledgerHash = sha256(header), and the commit
// is (root, total = fundedThrough[e], leafCount, ledgerHash) for the pool
// PoolID(n), as for every v2 pool.
//
// # Building a root (server)
//
// Build takes a BuildInput: the operator NoID, ChainID and Netuid (964 and 25
// on mainnet), the root's Epoch e and the pool's FirstEpoch, the carry
// contract's CarryColdkey, the latest finalized root as Previous (its root,
// its accounts and its State; ReadPrevious reads them from the published
// objects; nil for the first root), FundedThrough for every covered epoch,
// and per covered epoch p+1 … e an EpochInput: the committed census
// (CensusInput: ctfCensus(n, k)'s hash, its block and that block's
// timestamp, and the census objects), the Pulse of DrawRound(time), the
// carry contract's recycledFor[k] as Recycled, the epoch's fleet bindings as
// Bound, the winners' payout coldkeys in Coldkeys, and for an epoch an
// earlier root covered, its first Awards reference (fixed inputs). It returns
// the Ledger: Commit, Header, Accounts and Tree, Objects (publish them,
// put-if-absent, before the header), each epoch's EpochResult (the census,
// the draw, f_k, R_k, X_k, M_k, F_k, the slot amount, awards, pots, pending
// awards and what was paid: the report for /sn/ctf), every Delta and the
// State after e. EncodeCensus builds the census to commit.
//
// # Auditing a root (validator, snclaim)
//
// Verify takes a VerifyInput read from chain at one pinned block: the pool's
// NoID, ChainID, Netuid, FirstEpoch and CarryColdkey, the Pending root and
// the latest finalized root as Previous, FundedThrough, and per covered epoch
// its EpochFacts (the census commitment or nil, the pulse, recycledFor[k]);
// a Fetcher and a Custody as for package ledger, with Custody.ValidRows of
// the pool PoolID(n) holding each epoch's audited awards; Heads for the
// bindings; MaxFetchBytes. It returns an error only for its own inputs
// (ErrInput, ledger.ErrFunding, ErrPulse for a pulse that does not verify),
// its context, its custody or its head oracle. Otherwise the Result holds the
// outcome: Check is the first check that failed and Err its error; the checks
// run in the order CheckPrevious (its accounts), CheckHeader, CheckChunks
// (with the faults against the previous root), CheckFixedInputs,
// CheckAccounts, CheckTree, CheckPrevious (its state, read only now, so that a
// withheld state object leads to a demand), then per covered epoch
// CheckCensus, CheckSeed, CheckDraw and CheckAmounts (which fetches the pot
// and pending rows the header names: the next audit starts from them),
// and finally CheckAllocation. Result.Action applies VERSION2-FINAL.md §9.3
// as for a provider pool (ledger.Action), and Result.Epochs reports each
// epoch exactly as Build does. Presence, the checks behind P1 and X1,
// countries, units, address dedupe, wallets and declared networks are
// attested and not checkable (§8).
package ctf
