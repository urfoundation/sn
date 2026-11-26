// A runtime's transaction version is signed data (CheckTxVersion's additional
// field). The producer encoding family began at transaction version 1; a
// raised version joins it only as an exact reviewed identity.
package crv4

import "slices"

// Runtime494 (subtensor v494, a68c6e8c; Finney from block 9570596,
// 2026-11-23) is the first node-subtensor runtime to raise its transaction
// version, from 1 to 2, because it appends calls: the delegated-weight
// SubtensorModule calls at 170-174 and Commitments.set_commitment_to at 3.
// Every call, event and storage item the producer consumes keeps its index and
// encoding, and the signed-extension set is unchanged (CheckMetadataHash
// included), so its transaction version 2 changes only the signed value that
// the signer already takes from the authenticated runtime. Pinned in
// runtime494_metadata_test.go.
// Runtime495 (subtensor v495, e536e6bf; Finney from block 9578500,
// 2026-11-24) keeps transaction version 2: it appends no call, every call,
// event and storage item the producer consumes keeps its index and encoding,
// and the signed-extension set is unchanged. Pinned in
// runtime495_metadata_test.go.
// Runtime497 (subtensor v497, 24e1fc7b; Finney from block 9587045,
// 2026-11-25) keeps transaction version 2 and changes only its spec version
// from 495's: no call, event or storage item moves, and the signed-extension
// set is unchanged. Pinned in runtime497_metadata_test.go.
// Any other identity, including a later spec at transaction version 2 or the
// proposed 496 that was never enacted, stays outside the family until its own
// review.
var reviewedRaisedTransactionVersions = []RuntimeVersionIdentity{
	{SpecName: "node-subtensor", SpecVersion: 494, TransactionVersion: 2, StateVersion: 1},
	{SpecName: "node-subtensor", SpecVersion: 495, TransactionVersion: 2, StateVersion: 1},
	{SpecName: "node-subtensor", SpecVersion: 497, TransactionVersion: 2, StateVersion: 1},
}

// Reports whether this exact identity raised its transaction version above the
// family's original 1 and was reviewed as encoding-identical for the producer.
// It grants no artifact authority: callers still authenticate the block's
// exact artifact and check its consumed interfaces.
func reviewedRaisedTransactionVersion(version RuntimeVersionIdentity) bool {
	return version.TransactionVersion != 1 && slices.Contains(reviewedRaisedTransactionVersions, version)
}
