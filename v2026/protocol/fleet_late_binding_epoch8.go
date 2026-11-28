package protocol

// The epoch 8 late-binding window: a one-time exception, for SN25 mainnet
// epoch 8 only, to the rule that a fleet binding starts no earlier than the
// settlement epoch after the one it lands in (STCoordinator.bindFleetMember
// requires validFromEpoch > currentEpoch()).
//
// Why: Finney moved to node-subtensor runtime 497 at block 9,587,045
// (2026-11-25). `provider fleet` refuses to register, publish or bind under a
// runtime until that runtime's reviewed fleet runtime authority is published,
// and the authority for runtime 497
// (mainnet/fleet-runtime-authority/runtime-497.json) was published only at
// about 2026-11-27 20:40Z, after epoch 8 began at block 9,600,028
// (2026-11-27 18:33:24Z). So no fleet could bind in time for epoch 8. The
// owner decided on 2026-11-27 (about 20:50Z) that a binding that lands from
// epoch 8's first block until 2026-11-29 20:40Z counts from epoch 8, starting
// when it lands (mainnet/notices/2026-11-27-epoch-8-late-binding.md).
//
// The window is block-exact: blocks 9,600,028 through 9,615,060. Block
// 9,615,061 is the first block at or after 2026-11-29 20:40:00Z: block
// 9,600,763 carries 2026-11-27 21:00:24Z, 14,298 blocks of 12 s earlier, and
// Finney's 12-second slots never produce blocks faster than that. A binding
// that lands in block 9,615,061 or later starts at its signed epoch, as do
// bindings that landed before epoch 8 and the bindings of every other epoch.
//
// Only a binding signed to start at epoch 9, the earliest epoch the
// coordinator admits during epoch 8, is moved. It counts for epoch 8 only
// where the client has no binding in force for epoch 8: the window never
// displaces a binding made before epoch 8.
//
// The window must never generalize: there is no setting, and no other epoch,
// block range or deployment has one.

const (
	// The chain of the SN25 mainnet coordinator the window belongs to.
	Epoch8LateBindingChainID uint64 = 964
	// The settlement epoch the window lets bindings count for.
	Epoch8LateBindingEpoch uint64 = 8
	// Epoch 8's first block, the first block of the window.
	Epoch8LateBindingFirstBlock uint64 = 9_600_028
	// The first block after the window, at or after 2026-11-29 20:40:00Z.
	Epoch8LateBindingEndBlock uint64 = 9_615_061
)

// The SN25 mainnet coordinator proxy, 0xC18925925E2B7bb9059b7d696b8c92762AE86406
// (mainnet/MAINNET.md), the only coordinator the window belongs to.
var Epoch8LateBindingCoordinator = [20]byte{
	0xc1, 0x89, 0x25, 0x92, 0x5e, 0x2b, 0x7b, 0xb9, 0x05, 0x9b,
	0x7d, 0x69, 0x6b, 0x8c, 0x92, 0x76, 0x2a, 0xe8, 0x64, 0x06,
}

// Epoch8LateBindingDeployment reports whether a coordinator is the SN25
// mainnet coordinator the window belongs to.
func Epoch8LateBindingDeployment(chainID uint64, coordinator [20]byte) bool {
	return chainID == Epoch8LateBindingChainID && coordinator == Epoch8LateBindingCoordinator
}

// Epoch8LateBindingLanded reports whether a binding signed to start at
// validFromEpoch, whose FleetBound event is in landedBlock, landed in the
// window and so counts from epoch 8.
func Epoch8LateBindingLanded(chainID uint64, coordinator [20]byte, validFromEpoch uint64, landedBlock uint64) bool {
	return Epoch8LateBindingDeployment(chainID, coordinator) &&
		validFromEpoch == Epoch8LateBindingEpoch+1 &&
		Epoch8LateBindingFirstBlock <= landedBlock && landedBlock < Epoch8LateBindingEndBlock
}

// FleetBindingFirstEpoch returns the first settlement epoch a binding counts
// for: its signed validFromEpoch, or epoch 8 for a binding that landed in the
// window.
func FleetBindingFirstEpoch(chainID uint64, coordinator [20]byte, validFromEpoch uint64, landedBlock uint64) uint64 {
	if Epoch8LateBindingLanded(chainID, coordinator, validFromEpoch, landedBlock) {
		return Epoch8LateBindingEpoch
	}
	return validFromEpoch
}

// FleetBindingCoversEpoch reports whether a FleetBound event's validity, with
// the window applied, covers epoch. A revocation only shortens a validity and
// a cleanup only ends one, so a client bound for epoch at any block has an
// event that covers it.
func FleetBindingCoversEpoch(chainID uint64, coordinator [20]byte, validFromEpoch uint64, validToEpoch uint64, landedBlock uint64, epoch uint64) bool {
	return FleetBindingFirstEpoch(chainID, coordinator, validFromEpoch, landedBlock) <= epoch && epoch <= validToEpoch
}

// Epoch8LateBindingAppliesAt reports whether a read of the coordinator's
// bindingAt(client, epoch) at block must also look for a window binding: on
// the mainnet coordinator, for epoch 8, at a block in or after epoch 8. A
// binding can only have landed in the window by such a block.
func Epoch8LateBindingAppliesAt(chainID uint64, coordinator [20]byte, epoch uint64, block uint64) bool {
	return Epoch8LateBindingDeployment(chainID, coordinator) && epoch == Epoch8LateBindingEpoch && Epoch8LateBindingFirstBlock <= block
}

// Epoch8LateBindingInForce reports whether a window binding counts for epoch
// 8, from its coordinator record at the block read. activeFrom9 is the
// coordinator's own answer for epoch 9, its first signed epoch: its validity,
// cleanup and live UID tests. A binding revoked to end with epoch 8 is
// inactive for epoch 9, but a revocation takes effect at the next epoch, as
// for every binding, so it still counts for epoch 8. replaced is a window
// binding that a binding landing after the window has replaced, which the
// coordinator admits only once the window binding was revoked or cleaned: it
// still counts for epoch 8. A binding cleaned in epoch 8 no longer counts.
func Epoch8LateBindingInForce(activeFrom9 bool, validToEpoch uint64, cleaned bool, cleanedAtEpoch uint64, replaced bool) bool {
	if cleaned && cleanedAtEpoch <= Epoch8LateBindingEpoch {
		return false
	}
	return activeFrom9 || replaced || validToEpoch == Epoch8LateBindingEpoch
}
