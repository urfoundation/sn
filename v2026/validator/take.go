//go:build linux || darwin

// Delegate and childkey takes for the release hotkey (docs/OPERATOR-DISCOVERY.md
// section 7.1). The coldkey that owns the hotkey signs decrease_take,
// increase_take or set_childkey_take on the stake add path: the finalized
// runtime is authenticated against the config pin, the live take, bounds and
// last change are read at that block, the fee is quoted against
// --fee_limit_rao, and only --apply journals, broadcasts and waits for
// finality. A dry run prints the current and target values and the exact call
// it would sign.
//
// Takes are PerU16 parts of 65535 (18% is 11796). The preflight mirrors the
// subtensor rules, unchanged from runtime 455 through 473:
//   - decrease_take: below a stored take (any take when none is stored), at
//     least MinDelegateTake, never rate limited, and it restarts the increase
//     window;
//   - increase_take: above a stored take, at most MaxDelegateTake, and more
//     than TxDelegateTakeRateLimit blocks after the last change, kept in
//     LastRateLimitedBlock[LastTxBlockDelegateTake(hotkey)];
//   - set_childkey_take: from max(MinChildkeyTake, MinChildkeyTakePerSubnet)
//     to MaxChildkeyTake; raising the effective take needs
//     TxChildkeyTakeRateLimit blocks after the last change, kept in
//     TransactionKeyLastBlock[(hotkey, netuid, SetChildkeyTake)].
//
// The runtime checks every rule again at inclusion; the preflight only keeps a
// fee from being paid for a call that would fail.
package validator

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/docopt/docopt-go"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ss58"
)

// The PerU16 denominator: 65535 parts is a 100% take.
const takePartsPerWhole = 65535

// TransactionType::SetChildkeyTake, the TransactionKeyLastBlock discriminant.
// It is a runtime constant that metadata does not describe.
const childkeyTakeTransactionType = uint16(1)

func takeCommand(opts docopt.Opts) {
	if optBool(opts, "status") {
		exitOnError("validator take status", runTakeStatus(opts, os.Stdout))
		return
	}
	command := "validator take set"
	if optBool(opts, "childkey") {
		command = "validator take childkey"
	}
	exitOnError(command, runTakeChange(opts, command, os.Stdout))
}

// Read only: status needs neither the coldkey nor the journal.
func runTakeStatus(opts docopt.Opts, output io.Writer) error {
	cfg, err := LoadReleaseConfigPreActivation(optString(opts, "--config", ""))
	if err != nil {
		return err
	}
	netuids := []uint16{cfg.Netuid}
	if value := optString(opts, "--netuid", ""); value != "" {
		netuid, err := parseTakeNetuid(value)
		if err != nil {
			return err
		}
		if netuid != cfg.Netuid {
			netuids = append(netuids, netuid)
		}
	}
	hotkey, err := loadReleaseHotkey(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := nativeCommandContext()
	defer cancel()
	native, err := dialPinnedNative(ctx, cfg)
	if err != nil {
		return err
	}
	defer native.API.Client.Close()
	bound, runtime, err := snchain.AuthenticateFinalizedRuntimeContext(ctx, native, releaseNativeRuntimeIdentity(cfg))
	if err != nil {
		return err
	}
	return printTakeStatus(ctx, bound, runtime, hotkey.PublicKey(), netuids, output)
}

// Follows runStakeAdd: owner custody, journal, release hotkey, coldkey seed,
// then the pinned native endpoint.
func runTakeChange(opts docopt.Opts, command string, output io.Writer) error {
	cfg, err := LoadReleaseConfigPreActivation(optString(opts, "--config", ""))
	if err != nil {
		return err
	}
	take, err := parseTakePercent(optString(opts, "--take", ""))
	if err != nil {
		return err
	}
	var netuid uint16
	if optBool(opts, "childkey") {
		if netuid, err = parseTakeNetuid(optString(opts, "--netuid", "")); err != nil {
			return err
		}
	}
	ctx, cancel := nativeCommandContext()
	defer cancel()
	ctx = nativeOwnerStorageContext(ctx, opts)
	journal, err := openReleaseNativeJournal(cfg, ctx)
	if err != nil {
		return err
	}
	defer journal.Close()
	hotkey, err := loadReleaseHotkey(cfg)
	if err != nil {
		return err
	}
	coldkey, err := snchain.LoadKeypairFile(expandHome(optString(opts, "--coldkey_seed_file", "")))
	if err != nil {
		return fmt.Errorf("coldkey seed: %w", err)
	}
	native, err := dialPinnedNative(ctx, cfg)
	if err != nil {
		return err
	}
	defer native.API.Client.Close()
	return changeTake(ctx, native, takeRequest{
		Command: command, Netuid: netuid, Hotkey: hotkey.PublicKey(), Coldkey: coldkey, Take: take,
		FeeLimitRao: optUint64(opts, "--fee_limit_rao", defaultNativeFeeLimitRao),
		Allowed:     []crv4.RuntimeArtifactIdentity{releaseNativeRuntimeIdentity(cfg)}, Journal: journal, Apply: optBool(opts, "--apply"), Output: output,
	})
}

// Reads a percent with at most two decimals and rounds it down to parts of
// 65535, as the runtime documents (1% is 655, 18% is 11796).
func parseTakePercent(value string) (uint16, error) {
	invalid := fmt.Errorf("--take %q is not a percent from 0 to 100 with at most two decimals", value)
	whole, fraction, decimal := strings.Cut(value, ".")
	if whole == "" || len(whole) > 3 || (decimal && (fraction == "" || len(fraction) > 2)) {
		return 0, invalid
	}
	for _, digit := range whole + fraction {
		if digit < '0' || '9' < digit {
			return 0, invalid
		}
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	hundredths, err := strconv.ParseUint(whole+fraction, 10, 64)
	if err != nil || 100*100 < hundredths {
		return 0, invalid
	}
	return uint16(hundredths * takePartsPerWhole / (100 * 100)), nil
}

// Root has no child hotkeys (set_children refuses netuid 0), so a childkey
// take names a subnet.
func parseTakeNetuid(value string) (uint16, error) {
	netuid, err := strconv.ParseUint(value, 10, 16)
	if err != nil || netuid == 0 {
		return 0, fmt.Errorf("--netuid %q is not a subnet netuid from 1 to 65535; root has no child hotkeys, and its validator take is the delegate take", value)
	}
	return uint16(netuid), nil
}

// The exact parts with the nearest percent.
func formatTake(parts uint16) string {
	hundredths := (uint64(parts)*100*100 + takePartsPerWhole/2) / takePartsPerWhole
	return fmt.Sprintf("%d/%d (%d.%02d%%)", parts, takePartsPerWhole, hundredths/100, hundredths%100)
}

// For operators; hex remains the exact identity.
func takeSs58(account [32]byte) string {
	address, err := ss58.Encode(account, crv4.SS58PrefixSubstrate)
	if err != nil {
		return snchain.Hex32(account)
	}
	return address
}

// Owner is a ValueQuery whose zero-account default means unowned.
func takeOwnerText(owner [32]byte) string {
	if owner == ([32]byte{}) {
		return "none (no owning coldkey on chain)"
	}
	return takeSs58(owner)
}

// One take change signed by the hotkey's owning coldkey: the delegate take
// when Netuid is zero, otherwise the childkey take on Netuid.
type takeRequest struct {
	Command     string
	Netuid      uint16
	Hotkey      [32]byte
	Coldkey     *crv4.Keypair
	Take        uint16
	FeeLimitRao uint64
	Allowed     []crv4.RuntimeArtifactIdentity
	Journal     *snchain.Journal
	Apply       bool
	Output      io.Writer
}

// A hotkey's delegate take with the runtime's bounds at one block. An
// unstored take is the runtime default.
type delegateTakeState struct {
	Take       uint16
	Stored     bool
	Minimum    uint16
	Maximum    uint16
	RateLimit  uint64
	LastChange uint64
}

// A hotkey's childkey take on one subnet with the runtime's bounds at one
// block. The runtime pays the larger of the take and the effective minimum,
// stored or not.
type childkeyTakeState struct {
	Netuid        uint16
	SubnetExists  bool
	Take          uint16
	Stored        bool
	GlobalMinimum uint16
	SubnetMinimum uint16
	Maximum       uint16
	RateLimit     uint64
	LastChange    uint64
}

func (self childkeyTakeState) minimum() uint16 {
	return max(self.GlobalMinimum, self.SubnetMinimum)
}

func (self childkeyTakeState) effective() uint16 {
	return max(self.Take, self.minimum())
}

// The runtime's ownership check: the hotkey account exists and belongs to the
// signing coldkey.
func takeOwnerDecision(owner, coldkey [32]byte) error {
	if owner == ([32]byte{}) {
		return errors.New("hotkey has no owning coldkey on chain; register it before setting a take")
	}
	if owner != coldkey {
		return fmt.Errorf("hotkey is owned by coldkey %s, not the signing coldkey %s", takeSs58(owner), takeSs58(coldkey))
	}
	return nil
}

// Names the call that moves the delegate take to target, or "" when it is
// already stored there. The runtime compares only against a stored take, so
// decrease_take also pins the default when none is stored.
func delegateTakeDecision(state delegateTakeState, target uint16, inclusion uint64) (string, error) {
	if state.Maximum < target {
		return "", fmt.Errorf("target delegate take %s is above the chain maximum %s", formatTake(target), formatTake(state.Maximum))
	}
	if state.Stored && state.Take == target {
		return "", nil
	}
	if target <= state.Take {
		if target < state.Minimum {
			return "", fmt.Errorf("target delegate take %s is below the chain minimum %s", formatTake(target), formatTake(state.Minimum))
		}
		return "decrease_take", nil
	}
	// The runtime refuses while blocks since the last change <= the limit.
	if state.RateLimit != 0 && state.LastChange != 0 && takeBlocksSince(state.LastChange, inclusion) <= state.RateLimit {
		return "", fmt.Errorf("delegate take increases wait %d blocks after the last change at block %d; the earliest is block %d", state.RateLimit, state.LastChange, state.LastChange+state.RateLimit+1)
	}
	return "increase_take", nil
}

// Names set_childkey_take, or "" when the take is already stored at target.
// Only raising the effective take is rate limited.
func childkeyTakeDecision(state childkeyTakeState, target uint16, inclusion uint64) (string, error) {
	if !state.SubnetExists {
		return "", fmt.Errorf("netuid %d does not exist", state.Netuid)
	}
	if state.Maximum < target {
		return "", fmt.Errorf("target childkey take %s is above the chain maximum %s", formatTake(target), formatTake(state.Maximum))
	}
	if target < state.minimum() {
		return "", fmt.Errorf("target childkey take %s is below the chain minimum %s for netuid %d", formatTake(target), formatTake(state.minimum()), state.Netuid)
	}
	if state.Stored && state.Take == target {
		return "", nil
	}
	// The runtime admits a first change, or one at least the limit after the last.
	if state.effective() < target && state.LastChange != 0 && takeBlocksSince(state.LastChange, inclusion) < state.RateLimit {
		return "", fmt.Errorf("childkey take increases on netuid %d wait %d blocks after the last change at block %d; the earliest is block %d", state.Netuid, state.RateLimit, state.LastChange, state.LastChange+state.RateLimit)
	}
	return "set_childkey_take", nil
}

// The runtime saturates the subtraction.
func takeBlocksSince(last, block uint64) uint64 {
	if block < last {
		return 0
	}
	return block - last
}

// SubtensorModule.decrease_take or increase_take(hotkey, take), or
// set_childkey_take(hotkey, netuid, take). A runtime that no longer declares
// the reviewed arguments is refused, so a changed dispatchable is never signed
// blind.
func takeCall(meta *types.Metadata, name string, hotkey [32]byte, netuid uint16, take uint16) (types.Call, error) {
	if meta == nil || hotkey == ([32]byte{}) {
		return types.Call{}, errors.New("take call metadata or hotkey is missing")
	}
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	shapes, args := []string{"hotkey:[u8;32]", "take:u16"}, []any{*account, types.NewU16(take)}
	switch {
	case name == "set_childkey_take" && netuid != 0:
		shapes, args = []string{"hotkey:[u8;32]", "netuid:u16", "take:u16"}, []any{*account, types.NewU16(netuid), types.NewU16(take)}
	case (name == "decrease_take" || name == "increase_take") && netuid == 0:
	default:
		return types.Call{}, fmt.Errorf("%s is not a take call for netuid %d", name, netuid)
	}
	report, err := (&crv4.Chain{Meta: meta}).DescribeCall(crv4.PalletName, name)
	if err != nil {
		return types.Call{}, err
	}
	declared := make([]string, len(report.Args))
	for i, arg := range report.Args {
		declared[i] = arg.Name + ":" + arg.Shape
	}
	if !report.Found || !slices.Equal(declared, shapes) {
		return types.Call{}, fmt.Errorf("runtime %s.%s takes (%s), not the reviewed (%s)", crv4.PalletName, name, strings.Join(declared, ", "), strings.Join(shapes, ", "))
	}
	return types.NewCall(meta, crv4.PalletName+"."+name, args...)
}

// Decodes one SubtensorModule value at the block. Stored is false when the
// runtime's declared default supplied the value, which for Delegates and
// ChildkeyTake means no take was ever set.
func readTakeStorage(ctx context.Context, chain *crv4.Chain, block types.Hash, storage string, value any, args ...[]byte) (bool, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || chain.Meta == nil || block == (types.Hash{}) {
		return false, errors.New("take storage read dependencies are unavailable")
	}
	key, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, storage, args...)
	if err != nil {
		return false, fmt.Errorf("storage key %s.%s: %w", crv4.PalletName, storage, err)
	}
	var encoded *string
	if err := chain.API.Client.CallContext(ctx, &encoded, "state_getStorage", key.Hex(), block.Hex()); err != nil {
		return false, fmt.Errorf("read %s.%s: %w", crv4.PalletName, storage, err)
	}
	if encoded == nil {
		entry, err := chain.Meta.FindStorageEntryMetadata(crv4.PalletName, storage)
		if err != nil {
			return false, err
		}
		present, err := snchain.DecodeStorageFallback(entry, value)
		if err == nil && !present {
			err = fmt.Errorf("%s.%s storage is absent", crv4.PalletName, storage)
		}
		return false, err
	}
	raw, err := codec.HexDecodeString(*encoded)
	if err == nil {
		err = codec.Decode(raw, value)
	}
	if err != nil {
		return false, fmt.Errorf("decode %s.%s: %w", crv4.PalletName, storage, err)
	}
	return true, nil
}

// Delegates(hotkey) when netuid is zero, otherwise ChildkeyTake(hotkey,
// netuid).
func readStoredTake(ctx context.Context, chain *crv4.Chain, block types.Hash, hotkey [32]byte, netuid uint16) (uint16, bool, error) {
	var take types.U16
	storage, args := "Delegates", [][]byte{hotkey[:]}
	if netuid != 0 {
		storage, args = "ChildkeyTake", [][]byte{hotkey[:], snchain.NetuidArg(netuid)}
	}
	stored, err := readTakeStorage(ctx, chain, block, storage, &take, args...)
	return uint16(take), stored, err
}

// RateLimitKey::LastTxBlockDelegateTake(hotkey), with the variant index the
// runtime's own metadata declares.
func delegateTakeRateKey(meta *types.Metadata, hotkey [32]byte) ([]byte, error) {
	entry, err := meta.FindStorageEntryMetadata(crv4.PalletName, "LastRateLimitedBlock")
	if err != nil {
		return nil, err
	}
	if entryV14, ok := entry.(types.StorageEntryMetadataV14); ok && entryV14.Type.IsMap {
		if keyType := meta.AsMetadataV14.EfficientLookup[entryV14.Type.AsMap.Key.Int64()]; keyType != nil && keyType.Def.IsVariant {
			for _, variant := range keyType.Def.Variant.Variants {
				if variant.Name == "LastTxBlockDelegateTake" && len(variant.Fields) == 1 {
					return append([]byte{byte(variant.Index)}, hotkey[:]...), nil
				}
			}
		}
	}
	return nil, errors.New("runtime LastRateLimitedBlock has no LastTxBlockDelegateTake(account) key")
}

// Every value fails closed: a runtime without one of these items is refused.
func readDelegateTake(ctx context.Context, chain *crv4.Chain, hotkey [32]byte, block types.Hash) (delegateTakeState, error) {
	var state delegateTakeState
	if chain == nil || chain.Meta == nil {
		return state, errors.New("delegate take metadata is unavailable")
	}
	rateKey, err := delegateTakeRateKey(chain.Meta, hotkey)
	if err != nil {
		return state, err
	}
	if state.Take, state.Stored, err = readStoredTake(ctx, chain, block, hotkey, 0); err != nil {
		return state, err
	}
	var minimum, maximum types.U16
	var rateLimit, lastChange types.U64
	read := func(storage string, value any, args ...[]byte) {
		if err == nil {
			_, err = readTakeStorage(ctx, chain, block, storage, value, args...)
		}
	}
	read("MinDelegateTake", &minimum)
	read("MaxDelegateTake", &maximum)
	read("TxDelegateTakeRateLimit", &rateLimit)
	read("LastRateLimitedBlock", &lastChange, rateKey)
	state.Minimum, state.Maximum, state.RateLimit, state.LastChange = uint16(minimum), uint16(maximum), uint64(rateLimit), uint64(lastChange)
	return state, err
}

// Every value fails closed, MinChildkeyTakePerSubnet included.
func readChildkeyTake(ctx context.Context, chain *crv4.Chain, hotkey [32]byte, netuid uint16, block types.Hash) (childkeyTakeState, error) {
	state := childkeyTakeState{Netuid: netuid}
	var err error
	if state.Take, state.Stored, err = readStoredTake(ctx, chain, block, hotkey, netuid); err != nil {
		return state, err
	}
	var exists types.Bool
	var globalMinimum, subnetMinimum, maximum types.U16
	var rateLimit, lastChange types.U64
	read := func(storage string, value any, args ...[]byte) {
		if err == nil {
			_, err = readTakeStorage(ctx, chain, block, storage, value, args...)
		}
	}
	netuidArg := snchain.NetuidArg(netuid)
	read("NetworksAdded", &exists, netuidArg)
	read("MinChildkeyTake", &globalMinimum)
	read("MinChildkeyTakePerSubnet", &subnetMinimum, netuidArg)
	read("MaxChildkeyTake", &maximum)
	read("TxChildkeyTakeRateLimit", &rateLimit)
	read("TransactionKeyLastBlock", &lastChange, hotkey[:], netuidArg, binary.LittleEndian.AppendUint16(nil, childkeyTakeTransactionType))
	state.SubnetExists, state.GlobalMinimum, state.SubnetMinimum, state.Maximum = bool(exists), uint16(globalMinimum), uint16(subnetMinimum), uint16(maximum)
	state.RateLimit, state.LastChange = uint64(rateLimit), uint64(lastChange)
	return state, err
}

// One ChildKeys or ParentKeys entry: a share of the parent's stake weight (of
// u64::MAX) and the related hotkey.
type takeRelation struct {
	Proportion types.U64
	Hotkey     types.AccountID
}

// Nearest percent to two decimals; display only.
func formatTakeProportion(proportion uint64) string {
	return fmt.Sprintf("%.2f%%", float64(proportion)/float64(math.MaxUint64)*100)
}

func (self takeRequest) complete() bool {
	return self.Coldkey != nil && self.Output != nil && self.Hotkey != ([32]byte{})
}

// Authenticates the finalized runtime against the pin and changes the take on
// that bound view.
func changeTake(ctx context.Context, chain *crv4.Chain, req takeRequest) error {
	if !req.complete() {
		return errors.New("take request is incomplete")
	}
	bound, runtime, err := snchain.AuthenticateFinalizedRuntimeContext(ctx, chain, req.Allowed...)
	if err != nil {
		return err
	}
	return changeTakeAt(ctx, bound, runtime, req)
}

// Decides from the values at the authenticated block, signs the call and,
// with Apply, verifies the stored take at the inclusion block.
func changeTakeAt(ctx context.Context, bound *crv4.Chain, runtime snchain.FinalizedRuntime, req takeRequest) error {
	if !req.complete() {
		return errors.New("take request is incomplete")
	}
	printTakeRuntime(req.Output, runtime)
	owner, err := snchain.HotkeyOwnerAtContext(ctx, bound, req.Hotkey, runtime.Hash)
	if err != nil {
		return err
	}
	fmt.Fprintf(req.Output, "hotkey: %s (%s)\ncoldkey: %s (hotkey owner %s)\n", takeSs58(req.Hotkey), snchain.Hex32(req.Hotkey), req.Coldkey.Address(), takeOwnerText(owner))
	if err := takeOwnerDecision(owner, req.Coldkey.PublicKey()); err != nil {
		return err
	}
	// The earliest block that can include the extrinsic.
	inclusion := runtime.Number + 1
	label := "delegate take"
	var name string
	var rateLimit uint64
	if req.Netuid == 0 {
		state, err := readDelegateTake(ctx, bound, req.Hotkey, runtime.Hash)
		if err != nil {
			return err
		}
		printDelegateTake(req.Output, state)
		fmt.Fprintf(req.Output, "target %s: %s\n", label, formatTake(req.Take))
		if name, err = delegateTakeDecision(state, req.Take, inclusion); err != nil {
			return err
		}
		rateLimit = state.RateLimit
	} else {
		label = fmt.Sprintf("childkey take (netuid %d)", req.Netuid)
		state, err := readChildkeyTake(ctx, bound, req.Hotkey, req.Netuid, runtime.Hash)
		if err != nil {
			return err
		}
		printChildkeyTake(req.Output, state)
		fmt.Fprintf(req.Output, "target %s: %s\n", label, formatTake(req.Take))
		if name, err = childkeyTakeDecision(state, req.Take, inclusion); err != nil {
			return err
		}
		rateLimit = state.RateLimit
	}
	if name == "" {
		fmt.Fprintf(req.Output, "%s is already stored at the target; nothing to submit\n", label)
		return nil
	}
	// Every change, a decrease included, records the block that rate limits increases.
	if rateLimit != 0 {
		fmt.Fprintf(req.Output, "note: after this change, raising the %s waits %d blocks\n", label, rateLimit)
	}
	call, err := takeCall(bound.Meta, name, req.Hotkey, req.Netuid, req.Take)
	if err != nil {
		return err
	}
	data, err := codec.Encode(call)
	if err != nil {
		return err
	}
	fmt.Fprintf(req.Output, "call: %s.%s to %s, call data 0x%x\n", crv4.PalletName, name, formatTake(req.Take), data)
	submit, err := snchain.SubmitCall(ctx, bound, snchain.SubmitRequest{Command: req.Command, Netuid: req.Netuid, Hotkey: req.Hotkey, Signer: req.Coldkey, Call: call, FeeLimitRao: req.FeeLimitRao, Journal: req.Journal, Apply: req.Apply, Output: req.Output})
	if err != nil || submit.Receipt == nil {
		return err
	}
	return verifyStoredTake(ctx, bound, *submit.Receipt, req, label)
}

// Requires the target as the stored take in the finalized inclusion block.
func verifyStoredTake(ctx context.Context, bound *crv4.Chain, receipt crv4.FinalizedExtrinsic, req takeRequest, label string) error {
	take, stored, err := readStoredTake(ctx, bound, receipt.BlockHash, req.Hotkey, req.Netuid)
	if err != nil {
		return err
	}
	if !stored || take != req.Take {
		return fmt.Errorf("%s is %s (stored %t) after the finalized extrinsic, want %s", label, formatTake(take), stored, formatTake(req.Take))
	}
	fmt.Fprintf(req.Output, "%s: %s at finalized block %d\n", label, formatTake(take), receipt.BlockNumber)
	return nil
}

// The hotkey's owner, its root and subnet registrations, its delegate take and
// auto parent delegation, and its childkey take, children and parents on each
// netuid, all at one authenticated block.
func printTakeStatus(ctx context.Context, bound *crv4.Chain, runtime snchain.FinalizedRuntime, hotkey [32]byte, netuids []uint16, output io.Writer) error {
	printTakeRuntime(output, runtime)
	owner, err := snchain.HotkeyOwnerAtContext(ctx, bound, hotkey, runtime.Hash)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "hotkey: %s (%s)\nowner coldkey: %s\n", takeSs58(hotkey), snchain.Hex32(hotkey), takeOwnerText(owner))
	for _, netuid := range append([]uint16{0}, netuids...) {
		uid, registered, err := snchain.UIDAtContext(ctx, bound, netuid, hotkey, runtime.Hash)
		if err != nil {
			return err
		}
		if registered {
			fmt.Fprintf(output, "registration (netuid %d): uid %d\n", netuid, uid)
		} else {
			fmt.Fprintf(output, "registration (netuid %d): not registered\n", netuid)
		}
	}
	delegate, err := readDelegateTake(ctx, bound, hotkey, runtime.Hash)
	if err != nil {
		return err
	}
	printDelegateTake(output, delegate)
	// Unless disabled before root_register, the runtime makes a new root
	// validator the full-weight parent of every subnet owner hotkey.
	if _, err := bound.Meta.FindStorageEntryMetadata(crv4.PalletName, "AutoParentDelegationEnabled"); err != nil {
		fmt.Fprintf(output, "auto parent delegation: not declared by this runtime\n")
	} else {
		var enabled types.Bool
		stored, err := readTakeStorage(ctx, bound, runtime.Hash, "AutoParentDelegationEnabled", &enabled, hotkey[:])
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "auto parent delegation: %t (%s)\n", bool(enabled), takeSource(stored))
	}
	for _, netuid := range netuids {
		childkey, err := readChildkeyTake(ctx, bound, hotkey, netuid, runtime.Hash)
		if err != nil {
			return err
		}
		printChildkeyTake(output, childkey)
		if !childkey.SubnetExists {
			continue
		}
		var children, parents []takeRelation
		if _, err := readTakeStorage(ctx, bound, runtime.Hash, "ChildKeys", &children, hotkey[:], snchain.NetuidArg(netuid)); err != nil {
			return err
		}
		if _, err := readTakeStorage(ctx, bound, runtime.Hash, "ParentKeys", &parents, hotkey[:], snchain.NetuidArg(netuid)); err != nil {
			return err
		}
		childTexts := []string{"none"}
		if len(children) != 0 {
			childTexts = childTexts[:0]
			for _, child := range children {
				childTexts = append(childTexts, takeSs58(child.Hotkey)+" "+formatTakeProportion(uint64(child.Proportion)))
			}
		}
		fmt.Fprintf(output, "children (netuid %d): %s\n", netuid, strings.Join(childTexts, ", "))
		fmt.Fprintf(output, "parents (netuid %d): %d hotkeys name this hotkey as a child\n", netuid, len(parents))
	}
	return nil
}

func printTakeRuntime(output io.Writer, runtime snchain.FinalizedRuntime) {
	version := runtime.Artifact.Version
	fmt.Fprintf(output, "runtime: %s/%d/%d/%d at finalized block %d (%s)\n", version.SpecName, version.SpecVersion, version.TransactionVersion, version.StateVersion, runtime.Number, runtime.Hash.Hex())
}

func printDelegateTake(output io.Writer, state delegateTakeState) {
	fmt.Fprintf(output, "delegate take: %s %s; chain minimum %s, maximum %s\n", formatTake(state.Take), takeSource(state.Stored), formatTake(state.Minimum), formatTake(state.Maximum))
	fmt.Fprintf(output, "delegate take increases: one per %d blocks; %s\n", state.RateLimit, takeLastChange(state.LastChange, state.LastChange+state.RateLimit+1))
}

func printChildkeyTake(output io.Writer, state childkeyTakeState) {
	label := fmt.Sprintf("childkey take (netuid %d)", state.Netuid)
	if !state.SubnetExists {
		fmt.Fprintf(output, "%s: the subnet does not exist\n", label)
		return
	}
	effective := ""
	if state.effective() != state.Take {
		effective = ", effective " + formatTake(state.effective())
	}
	fmt.Fprintf(output, "%s: %s %s%s; chain minimum %s (global %s, subnet %s), maximum %s\n", label, formatTake(state.Take), takeSource(state.Stored), effective, formatTake(state.minimum()), formatTake(state.GlobalMinimum), formatTake(state.SubnetMinimum), formatTake(state.Maximum))
	fmt.Fprintf(output, "%s increases: one per %d blocks; %s\n", label, state.RateLimit, takeLastChange(state.LastChange, state.LastChange+state.RateLimit))
}

func takeSource(stored bool) string {
	if stored {
		return "stored"
	}
	return "runtime default, none stored"
}

func takeLastChange(last, next uint64) string {
	if last == 0 {
		return "no change recorded"
	}
	return fmt.Sprintf("last change at block %d, next increase from block %d", last, next)
}
