// This unit verifies decoded release-contract events in public receipts. A log
// hash establishes byte identity but not economic meaning, so decoded fields
// are bound to typed evidence before public-chain verification accepts them.
package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/ss58"
)

// Holds decoded deposit facts retained for cross-receipt nonce validation.
type finalSemanticReceiptPayload struct {
	deposits []finalSemanticReceiptDeposit
}

// Represents the authenticated fields shared by deposit-like coordinator logs.
type finalSemanticReceiptDeposit struct {
	NoID       uint64
	Epoch      uint64
	Amount     *big.Int
	Nonce      *big.Int
	PolicyHash string
	Funder     string
}

var finalSemanticReceiptABIs = struct {
	once        sync.Once
	coordinator abi.ABI
	vault       abi.ABI
	reserve     abi.ABI
	err         error
}{}

// Lazily parses immutable release ABIs once for canonical event decoding.
func finalSemanticReceiptContractABIs() (abi.ABI, abi.ABI, abi.ABI, error) {
	finalSemanticReceiptABIs.once.Do(func() {
		finalSemanticReceiptABIs.coordinator, finalSemanticReceiptABIs.err = abi.JSON(strings.NewReader(CoordinatorABI))
		if finalSemanticReceiptABIs.err != nil {
			return
		}
		finalSemanticReceiptABIs.vault, finalSemanticReceiptABIs.err = abi.JSON(strings.NewReader(SettlementVaultABI))
		if finalSemanticReceiptABIs.err != nil {
			return
		}
		finalSemanticReceiptABIs.reserve, finalSemanticReceiptABIs.err = abi.JSON(strings.NewReader(ReserveSinkABI))
	})
	return finalSemanticReceiptABIs.coordinator, finalSemanticReceiptABIs.vault, finalSemanticReceiptABIs.reserve, finalSemanticReceiptABIs.err
}

// Binds each decoded event to typed evidence and returns deposit facts for
// cross-receipt nonce checks. Logs must be canonical and release-owned.
func verifyFinalSemanticReceiptPayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, logs []finalCanonicalEVMLog) (*finalSemanticReceiptPayload, error) {
	if evidence == nil {
		return nil, errors.New("semantic evidence is nil")
	}
	events, err := finalSemanticReceiptEvents(evidence, receipt, logs)
	if err != nil {
		return nil, err
	}
	payload := &finalSemanticReceiptPayload{}
	for _, event := range events {
		switch event.Name {
		case "Deposit":
			deposit, decodeErr := finalSemanticReceiptDepositEvent(event)
			if decodeErr != nil {
				return nil, decodeErr
			}
			if verifyErr := verifyFinalSemanticDepositPayload(evidence, receipt, event, deposit); verifyErr != nil {
				return nil, verifyErr
			}
			payload.deposits = append(payload.deposits, deposit)
		case "ConvictionAdded":
			deposit, decodeErr := finalSemanticReceiptDepositEvent(event)
			if decodeErr != nil {
				return nil, decodeErr
			}
			if verifyErr := verifyFinalSemanticConvictionPayload(evidence, receipt, event, deposit); verifyErr != nil {
				return nil, verifyErr
			}
		case "PoolRegistered":
			if err := verifyFinalSemanticPoolRegistrationPayload(evidence, receipt, event); err != nil {
				return nil, err
			}
		case "OperatorScheduled":
			if err := verifyFinalSemanticOperatorSchedulePayload(evidence, receipt, event); err != nil {
				return nil, err
			}
		case "ReservePrincipalAdded":
			if err := verifyFinalSemanticReservePayload(evidence, receipt, event); err != nil {
				return nil, err
			}
		case "EmissionCaptured", "EmissionDeferred", "EmissionDustDeferred":
			if err := verifyFinalSemanticCapturePayload(evidence, receipt, event); err != nil {
				return nil, err
			}
		case "OperatorRootCommitted":
			if err := verifyFinalSemanticRootPayload(evidence, receipt, event); err != nil {
				return nil, err
			}
		case "EntitlementFinalized", "RootMissed", "OperatorEpochFinalized":
			if err := verifyFinalSemanticFinalizePayload(evidence, receipt, event); err != nil {
				return nil, err
			}
		case "Claimed", "ClaimPaid", "ClaimPaymentDeferred":
			if err := verifyFinalSemanticClaimPayload(evidence, receipt, event); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("release event %s is not represented by final semantic evidence", event.Name)
		}
	}
	if err := verifyFinalSemanticReceiptRequiredEvents(evidence, receipt, events); err != nil {
		return nil, err
	}
	return payload, nil
}

// Decodes canonical receipt logs after proving their identity and contract
// ownership against the immutable deployment.
func finalSemanticReceiptEvents(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, logs []finalCanonicalEVMLog) ([]finalSemanticEvent, error) {
	if len(logs) == 0 {
		return nil, errors.New("release receipt has no canonical logs")
	}
	canonical, err := finalCanonicalizeLogs(logs)
	if err != nil {
		return nil, err
	}
	for index := range logs {
		if !finalSemanticCanonicalLogEqual(logs[index], canonical[index]) {
			return nil, errors.New("release receipt logs are not in canonical order")
		}
	}
	logsHash, err := finalCanonicalReceiptLogsHash(canonical)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(logsHash, receipt.LogsHash) {
		return nil, errors.New("release receipt canonical logs hash differs from semantic evidence")
	}
	coordinator, vault, reserve, err := finalSemanticReceiptContractABIs()
	if err != nil {
		return nil, err
	}
	contracts := map[string]abi.ABI{}
	for address, contract := range map[string]abi.ABI{
		strings.ToLower(evidence.Deployment.CoordinatorProxy): coordinator,
		strings.ToLower(evidence.Deployment.SettlementVault):  vault,
		strings.ToLower(evidence.Deployment.ReserveSink):      reserve,
	} {
		if !common.IsHexAddress(address) || common.HexToAddress(address) == (common.Address{}) {
			return nil, fmt.Errorf("release contract %q is invalid", address)
		}
		contracts[address] = contract
	}
	result := make([]finalSemanticEvent, 0, len(canonical))
	for _, log := range canonical {
		if !strings.EqualFold(log.TransactionHash, receipt.TransactionHash) || log.BlockNumber != receipt.Block.Number || !strings.EqualFold(log.BlockHash, receipt.Block.Hash) {
			return nil, errors.New("release receipt log identity differs from its semantic receipt")
		}
		contract, found := contracts[strings.ToLower(log.Address)]
		if !found {
			return nil, fmt.Errorf("release receipt log address %s is not an immutable release contract", log.Address)
		}
		if len(log.Topics) == 0 {
			return nil, errors.New("release receipt event has no topic")
		}
		event, found := finalSemanticReceiptABIEvent(contract, log.Topics[0])
		if !found {
			return nil, fmt.Errorf("release receipt has unknown event topic %s", log.Topics[0])
		}
		data, decodeErr := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
		if decodeErr != nil {
			return nil, fmt.Errorf("decode release event %s data: %w", event.Name, decodeErr)
		}
		args := map[string]any{}
		if decodeErr = event.Inputs.NonIndexed().UnpackIntoMap(args, data); decodeErr != nil {
			return nil, fmt.Errorf("decode release event %s data: %w", event.Name, decodeErr)
		}
		topics := make([]common.Hash, len(log.Topics)-1)
		for index := 1; index < len(log.Topics); index++ {
			topics[index-1] = common.HexToHash(log.Topics[index])
		}
		if decodeErr = abi.ParseTopicsIntoMap(args, indexedABIArguments(event.Inputs), topics); decodeErr != nil {
			return nil, fmt.Errorf("decode release event %s topics: %w", event.Name, decodeErr)
		}
		result = append(result, finalSemanticEvent{Name: event.Name, Log: log, Args: args})
	}
	return result, nil
}

// Finds an ABI event by its first indexed topic.
func finalSemanticReceiptABIEvent(contract abi.ABI, topic string) (abi.Event, bool) {
	for _, event := range contract.Events {
		if strings.EqualFold(event.ID.Hex(), topic) {
			return event, true
		}
	}
	return abi.Event{}, false
}

// Extracts and validates fields common to coordinator deposit-like events.
func finalSemanticReceiptDepositEvent(event finalSemanticEvent) (finalSemanticReceiptDeposit, error) {
	noID, noOK := finalSemanticUint(event.Args, "noId")
	epoch, epochOK := finalSemanticUint(event.Args, "epoch")
	amount, amountOK := finalSemanticInteger(event.Args, "amount")
	nonce, nonceOK := finalSemanticInteger(event.Args, "nonce")
	policyHash, policyOK := finalSemanticHex32(event.Args, "policyHash")
	funder, funderOK := finalSemanticAddress(event.Args, "funder")
	if !noOK || noID == 0 || !epochOK || epoch == 0 || !amountOK || amount.Sign() <= 0 || !nonceOK || !policyOK || !funderOK {
		return finalSemanticReceiptDeposit{}, fmt.Errorf("release %s event has invalid amount/policy/nonce/funder fields", event.Name)
	}
	return finalSemanticReceiptDeposit{NoID: noID, Epoch: epoch, Amount: amount, Nonce: nonce, PolicyHash: strings.ToLower(policyHash), Funder: strings.ToLower(funder)}, nil
}

// Returns the evidence row for one non-ephemeral operator id.
func finalSemanticReceiptPool(evidence *FinalSemanticEvidence, noID uint64) *FinalPoolUIDEvidence {
	for index := range evidence.Pools {
		if evidence.Pools[index].NoID == noID {
			return &evidence.Pools[index]
		}
	}
	return nil
}

// Compares the immutable receipt identity fields used by semantic bindings.
func finalSemanticReceiptMatches(left, right FinalEVMReceipt) bool {
	return strings.EqualFold(left.TransactionHash, right.TransactionHash) && left.Block == right.Block && left.Status == right.Status && strings.EqualFold(left.LogsHash, right.LogsHash)
}

// Parses a canonical nonnegative decimal, optionally excluding zero.
func finalSemanticReceiptDecimal(label, value string, positive bool) (*big.Int, error) {
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok || parsed.Sign() < 0 || positive && parsed.Sign() == 0 || parsed.String() != value {
		return nil, fmt.Errorf("%s is not a canonical decimal", label)
	}
	return parsed, nil
}

// Requires one unsigned decoded field to equal its typed evidence value.
func finalSemanticReceiptRequireUint(event finalSemanticEvent, field string, want uint64) error {
	got, ok := finalSemanticUint(event.Args, field)
	if !ok || got != want {
		return fmt.Errorf("release %s %s differs from semantic evidence", event.Name, field)
	}
	return nil
}

// Requires one decimal decoded field to equal canonical typed evidence.
func finalSemanticReceiptRequireDecimal(event finalSemanticEvent, field, want string) error {
	expected, err := finalSemanticReceiptDecimal("expected "+event.Name+" "+field, want, false)
	if err != nil {
		return err
	}
	got, ok := finalSemanticInteger(event.Args, field)
	if !ok || got.Cmp(expected) != 0 {
		return fmt.Errorf("release %s %s differs from semantic evidence", event.Name, field)
	}
	return nil
}

// Requires one bytes32 decoded field to equal its expected canonical value.
func finalSemanticReceiptRequireHex(event finalSemanticEvent, field, want string) error {
	got, ok := finalSemanticHex32(event.Args, field)
	if !ok || !strings.EqualFold(got, want) {
		return fmt.Errorf("release %s %s differs from semantic evidence", event.Name, field)
	}
	return nil
}

// Requires one decoded EVM address to equal the expected canonical address.
func finalSemanticReceiptRequireAddress(event finalSemanticEvent, field, want string) error {
	got, ok := finalSemanticAddress(event.Args, field)
	if !ok || !strings.EqualFold(got, want) {
		return fmt.Errorf("release %s %s differs from semantic evidence", event.Name, field)
	}
	return nil
}

// Converts a Bittensor ss58 key into the bytes32 contract representation.
func finalSemanticReceiptSS58Hex(value string) (string, error) {
	key, err := ss58.DecodeWithPrefix(value, ss58.BittensorPrefix)
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(key[:]), nil
}

// Binds a deposit event to signed audit evidence or dishonest-deposit recovery.
func verifyFinalSemanticDepositPayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent, deposit finalSemanticReceiptDeposit) error {
	pool := finalSemanticReceiptPool(evidence, deposit.NoID)
	if pool == nil || !strings.EqualFold(deposit.PolicyHash, evidence.PolicyHash) || !strings.EqualFold(deposit.Funder, pool.DepositSigner) {
		return errors.New("release Deposit event does not bind the operator policy and signer")
	}
	if dishonest := evidence.DishonestDeposit; dishonest != nil {
		for _, expected := range []struct {
			receipt FinalEVMReceipt
			amount  string
			epoch   uint64
		}{
			{receipt: dishonest.UnderpaymentReceipt, amount: dishonest.ObservedDepositRao, epoch: finalSemanticDishonestDepositEpoch(dishonest.Penalties)},
			{receipt: dishonest.RecoveryDepositReceipt, amount: dishonest.RecoveryObservedDepositRao, epoch: finalSemanticDishonestDepositEpoch(dishonest.Recoveries)},
		} {
			if !finalSemanticReceiptMatches(receipt, expected.receipt) {
				continue
			}
			if expected.epoch == 0 || deposit.NoID != dishonest.NoID || deposit.Epoch != expected.epoch {
				return errors.New("dishonest-deposit event differs from the signed operator or epoch")
			}
			if err := finalSemanticReceiptRequireDecimal(event, "amount", expected.amount); err != nil {
				return err
			}
			return nil
		}
	}
	matchedAudit := false
	for _, cycle := range finalSemanticReceiptCycles(evidence) {
		for _, candidate := range cycle.Pools {
			if !finalSemanticReceiptMatches(receipt, candidate.DepositReceipt) {
				continue
			}
			if candidate.NoID != deposit.NoID || cycle.SettlementEpoch != deposit.Epoch {
				return errors.New("release Deposit event differs from the signed validator audit epoch or operator")
			}
			observed, err := finalSemanticReceiptDecimal("signed observed deposit", candidate.ObservedDepositRao, true)
			if err != nil {
				return err
			}
			if deposit.Amount.Cmp(observed) > 0 {
				return errors.New("release Deposit event exceeds the signed cumulative deposit")
			}
			matchedAudit = true
		}
	}
	if matchedAudit {
		return nil
	}
	return errors.New("release Deposit event has no signed validator-audit or dishonest-deposit binding")
}

// Binds a voluntary-conviction event to its final operator evidence.
func verifyFinalSemanticConvictionPayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent, conviction finalSemanticReceiptDeposit) error {
	pool := finalSemanticReceiptPool(evidence, conviction.NoID)
	if pool == nil || !finalSemanticReceiptMatches(receipt, pool.ConvictionReceipt) || !strings.EqualFold(conviction.PolicyHash, evidence.PolicyHash) || !strings.EqualFold(conviction.Funder, pool.DepositSigner) {
		return errors.New("release ConvictionAdded event does not bind the final operator conviction evidence")
	}
	if err := finalSemanticReceiptRequireUint(event, "noId", pool.NoID); err != nil {
		return err
	}
	return nil
}

// Binds a registration event to the recorded operator id, hotkey, and uid.
func verifyFinalSemanticPoolRegistrationPayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent) error {
	for _, pool := range evidence.Pools {
		if !finalSemanticReceiptMatches(receipt, pool.Registration) {
			continue
		}
		hotkey, err := finalSemanticReceiptSS58Hex(pool.Hotkey)
		if err != nil {
			return err
		}
		if err = finalSemanticReceiptRequireUint(event, "noId", pool.NoID); err != nil {
			return err
		}
		if err = finalSemanticReceiptRequireHex(event, "hotkey", hotkey); err != nil {
			return err
		}
		return finalSemanticReceiptRequireUint(event, "uid", uint64(pool.UID))
	}
	return errors.New("release PoolRegistered event has no pool-ownership receipt binding")
}

// Binds the initial historic schedule without conflating it with later authority.
func verifyFinalSemanticOperatorSchedulePayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent) error {
	noID, ok := finalSemanticUint(event.Args, "noId")
	pool := finalSemanticReceiptPool(evidence, noID)
	if !ok || pool == nil || !finalSemanticReceiptMatches(receipt, pool.Registration) {
		return errors.New("release OperatorScheduled event has an unknown operator")
	}
	// A registration emits the initial schedule. Coldkey and pool hotkey are
	// immutable across later versions; mutable deposit/root authority is bound
	// by the terminal operatorAt query in executeFinalSemanticOnChain instead
	// of being incorrectly compared with this historic initial event.
	coldkey, err := finalSemanticReceiptSS58Hex(pool.OperatorColdkey)
	if err != nil {
		return err
	}
	poolHotkey, err := finalSemanticReceiptSS58Hex(pool.Hotkey)
	if err != nil {
		return err
	}
	for _, want := range []struct {
		field string
		value string
	}{{"coldkey", coldkey}, {"poolHotkey", poolHotkey}} {
		if err = finalSemanticReceiptRequireHex(event, want.field, want.value); err != nil {
			return err
		}
	}
	effectiveEpoch, effectiveOK := finalSemanticUint(event.Args, "effectiveEpoch")
	depositHotkey, depositOK := finalSemanticHex32(event.Args, "depositHotkey")
	depositSigner, signerOK := finalSemanticAddress(event.Args, "depositSigner")
	rootSigner, rootOK := finalSemanticAddress(event.Args, "rootSigner")
	_, activeOK := event.Args["active"].(bool)
	if !effectiveOK || effectiveEpoch > pool.EffectiveEpoch || !depositOK || common.HexToHash(depositHotkey) == (common.Hash{}) || !signerOK || common.HexToAddress(depositSigner) == (common.Address{}) || !rootOK || common.HexToAddress(rootSigner) == (common.Address{}) || !activeOK {
		return errors.New("release OperatorScheduled historic authority fields are invalid")
	}
	return nil
}

// Binds one reserve principal transition to its observed post-baseline ledger.
func verifyFinalSemanticReservePayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent) error {
	for _, addition := range evidence.Reserve.PrincipalAdditions {
		if !finalSemanticReceiptMatches(receipt, addition.Receipt) {
			continue
		}
		for _, want := range []struct {
			field string
			value string
		}{{"amount", addition.AmountRao}, {"operatorPrincipal", addition.OperatorPrincipalRao}, {"totalPrincipal", addition.TotalPrincipalRao}, {"liveStake", addition.LiveStakeRao}} {
			if err := finalSemanticReceiptRequireDecimal(event, want.field, want.value); err != nil {
				return err
			}
		}
		if err := finalSemanticReceiptRequireUint(event, "epoch", addition.Epoch); err != nil {
			return err
		}
		return finalSemanticReceiptRequireUint(event, "noId", addition.NoID)
	}
	if receipt.Block.Number <= evidence.Window.BaselineHead.Number {
		return nil
	}
	return errors.New("release ReservePrincipalAdded event is absent from the post-baseline reserve evidence")
}

// Binds capture, defer, and dust-defer branches to one pool-epoch transition.
func verifyFinalSemanticCapturePayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent) error {
	row := finalSemanticReceiptEpochByReceipt(evidence, receipt, func(row FinalEpochOperatorEvidence) FinalEVMReceipt { return row.Capture })
	if row == nil {
		return errors.New("release capture event has no pool-epoch receipt binding")
	}
	if err := finalSemanticReceiptRequireUint(event, "epoch", row.Epoch); err != nil {
		return err
	}
	if err := finalSemanticReceiptRequireUint(event, "noId", row.NoID); err != nil {
		return err
	}
	pool := finalSemanticReceiptPool(evidence, row.NoID)
	if pool == nil {
		return errors.New("release capture event has no pool identity")
	}
	switch event.Name {
	case "EmissionCaptured":
		hotkey, err := finalSemanticReceiptSS58Hex(pool.Hotkey)
		if err != nil {
			return err
		}
		if err = finalSemanticReceiptRequireHex(event, "poolHotkey", hotkey); err != nil {
			return err
		}
		return finalSemanticReceiptRequireDecimal(event, "amount", row.CapturedRao)
	case "EmissionDeferred":
		captured, err := finalSemanticReceiptDecimal("semantic captured amount", row.CapturedRao, false)
		if err != nil || captured.Sign() != 0 {
			return errors.New("release EmissionDeferred event differs from a nonzero captured amount")
		}
		return nil
	case "EmissionDustDeferred":
		hotkey, err := finalSemanticReceiptSS58Hex(pool.Hotkey)
		if err != nil {
			return err
		}
		if err = finalSemanticReceiptRequireHex(event, "poolHotkey", hotkey); err != nil {
			return err
		}
		minimum, ok := finalSemanticUint(event.Args, "minimumTransferTaoRao")
		if !ok || minimum != evidence.Deployment.VaultMinimumTransferTaoRao {
			return errors.New("release EmissionDustDeferred minimum transfer differs from custody evidence")
		}
		if _, ok = finalSemanticInteger(event.Args, "observedAlphaRao"); !ok {
			return errors.New("release EmissionDustDeferred observed alpha is invalid")
		}
		if _, ok = finalSemanticInteger(event.Args, "taoEquivalentRao"); !ok {
			return errors.New("release EmissionDustDeferred TAO equivalent is invalid")
		}
		return nil
	default:
		return errors.New("release capture receipt contains an unknown capture event")
	}
}

// Selects the unique signed committer for a pool epoch or its fallback signer.
func finalSemanticReceiptRootCommitter(evidence *FinalSemanticEvidence, epoch, noID uint64) string {
	committer := ""
	for _, cycle := range finalSemanticReceiptCycles(evidence) {
		for _, pool := range cycle.Pools {
			if pool.NoID != noID || pool.SourceEpoch != epoch {
				continue
			}
			if committer == "" {
				committer = pool.RootCommitter
			} else if !strings.EqualFold(committer, pool.RootCommitter) {
				return ""
			}
		}
	}
	if committer != "" {
		return committer
	}
	if pool := finalSemanticReceiptPool(evidence, noID); pool != nil {
		return pool.PayoutRootSigner
	}
	return ""
}

// Binds a committed Merkle root to either pool-epoch or fleet artifact evidence.
func verifyFinalSemanticRootPayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent) error {
	var epoch, noID uint64
	var payoutRoot, artifactHash string
	bound := false
	for index := range evidence.Epochs {
		row := &evidence.Epochs[index]
		if row.Root != nil && finalSemanticReceiptMatches(receipt, *row.Root) {
			epoch, noID, payoutRoot, artifactHash, bound = row.Epoch, row.NoID, row.PayoutRoot, row.ArtifactHash, true
			break
		}
	}
	if !bound && evidence.FleetLifecycle != nil {
		for _, item := range evidence.FleetLifecycle.PayoutArtifacts {
			if !finalSemanticReceiptMatches(receipt, item.Root) {
				continue
			}
			for _, payout := range evidence.FleetLifecycle.State.Payouts {
				if payout.Epoch == item.Epoch && uint64(payout.NoID) == item.NoID {
					epoch, noID, payoutRoot, artifactHash, bound = item.Epoch, item.NoID, payout.PayoutRoot, "0x"+strings.TrimPrefix(item.Artifact.ContentHash, "sha256:"), true
					break
				}
			}
			if bound {
				break
			}
		}
	}
	if !bound {
		return errors.New("release OperatorRootCommitted event has no payout-root receipt binding")
	}
	if err := finalSemanticReceiptRequireUint(event, "epoch", epoch); err != nil {
		return err
	}
	if err := finalSemanticReceiptRequireUint(event, "noId", noID); err != nil {
		return err
	}
	if err := finalSemanticReceiptRequireHex(event, "payoutRoot", payoutRoot); err != nil {
		return err
	}
	if err := finalSemanticReceiptRequireHex(event, "artifactHash", artifactHash); err != nil {
		return err
	}
	committer := finalSemanticReceiptRootCommitter(evidence, epoch, noID)
	if committer == "" {
		return errors.New("release OperatorRootCommitted event has no deterministic signer binding")
	}
	return finalSemanticReceiptRequireAddress(event, "committer", committer)
}

// Binds finalization branches to committed-root or missed-root evidence.
func verifyFinalSemanticFinalizePayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent) error {
	row := finalSemanticReceiptEpochByReceipt(evidence, receipt, func(row FinalEpochOperatorEvidence) FinalEVMReceipt { return row.Finalize })
	if row == nil {
		return errors.New("release finalize event has no pool-epoch receipt binding")
	}
	if err := finalSemanticReceiptRequireUint(event, "epoch", row.Epoch); err != nil {
		return err
	}
	if err := finalSemanticReceiptRequireUint(event, "noId", row.NoID); err != nil {
		return err
	}
	switch event.Name {
	case "OperatorEpochFinalized":
		rootPresent, ok := event.Args["rootPresent"].(bool)
		if !ok || rootPresent != (row.Root != nil) {
			return errors.New("release OperatorEpochFinalized root presence differs from semantic evidence")
		}
	case "EntitlementFinalized":
		if row.Root == nil {
			return errors.New("release EntitlementFinalized event exists for a missed root")
		}
		if err := finalSemanticReceiptRequireHex(event, "payoutRoot", row.PayoutRoot); err != nil {
			return err
		}
		if err := finalSemanticReceiptRequireHex(event, "artifactHash", row.ArtifactHash); err != nil {
			return err
		}
		if err := finalSemanticReceiptRequireDecimal(event, "total", row.TotalRao); err != nil {
			return err
		}
		expiry, ok := finalSemanticUint(event.Args, "expiryBlock")
		if !ok || expiry <= receipt.Block.Number {
			return errors.New("release EntitlementFinalized expiry is invalid")
		}
	case "RootMissed":
		if row.Root != nil {
			return errors.New("release RootMissed event exists for a committed root")
		}
		if err := finalSemanticReceiptRequireDecimal(event, "carried", row.CapturedRao); err != nil {
			return err
		}
	default:
		return errors.New("release finalize receipt contains an unknown finalization event")
	}
	return nil
}

// Binds claim, payment, and deferred-credit branches to their semantic records.
func verifyFinalSemanticClaimPayload(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, event finalSemanticEvent) error {
	row, claim := finalSemanticReceiptClaimByReceipt(evidence, receipt)
	switch event.Name {
	case "Claimed":
		if row == nil || claim == nil {
			return errors.New("release Claimed event has no pool-claim receipt binding")
		}
		for _, want := range []struct {
			field string
			value uint64
		}{{"epoch", row.Epoch}, {"noId", row.NoID}, {"shareBps", claim.ShareBPS}} {
			if err := finalSemanticReceiptRequireUint(event, want.field, want.value); err != nil {
				return err
			}
		}
		if err := finalSemanticReceiptRequireHex(event, "coldkey", claim.Payee); err != nil {
			return err
		}
		if err := finalSemanticReceiptRequireDecimal(event, "amount", claim.ClaimedRao); err != nil {
			return err
		}
		if relayer, ok := finalSemanticAddress(event.Args, "relayer"); !ok || common.HexToAddress(relayer) == (common.Address{}) {
			return errors.New("release Claimed relayer is invalid")
		}
	case "ClaimPaid":
		payment := finalSemanticReceiptClaimPaymentByReceipt(evidence, receipt)
		if payment == nil {
			return errors.New("release ClaimPaid event has no canonical payment-record binding")
		}
		if err := finalSemanticReceiptRequireHex(event, "coldkey", payment.Coldkey); err != nil {
			return err
		}
		if err := finalSemanticReceiptRequireDecimal(event, "amount", payment.AmountRao); err != nil {
			return err
		}
		if claim != nil && (claim.PaidRao != payment.AmountRao || !strings.EqualFold(claim.Payee, payment.Coldkey)) {
			return errors.New("release ClaimPaid event differs from its paired claim evidence")
		}
		if relayer, ok := finalSemanticAddress(event.Args, "relayer"); !ok || common.HexToAddress(relayer) == (common.Address{}) {
			return errors.New("release ClaimPaid relayer is invalid")
		}
	case "ClaimPaymentDeferred":
		if row == nil || claim == nil {
			return errors.New("release ClaimPaymentDeferred event has no pool-claim receipt binding")
		}
		if err := finalSemanticReceiptRequireHex(event, "coldkey", claim.Payee); err != nil {
			return err
		}
		if err := finalSemanticReceiptRequireDecimal(event, "creditAlphaRao", claim.DeferredRao); err != nil {
			return err
		}
		minimum, ok := finalSemanticUint(event.Args, "minimumTransferTaoRao")
		if !ok || minimum != evidence.Deployment.VaultMinimumTransferTaoRao {
			return errors.New("release ClaimPaymentDeferred minimum transfer differs from custody evidence")
		}
		if amount, ok := finalSemanticInteger(event.Args, "taoEquivalentRao"); !ok || amount.Sign() < 0 {
			return errors.New("release ClaimPaymentDeferred TAO equivalent is invalid")
		}
		reason, ok := finalSemanticUint(event.Args, "reason")
		if !ok || reason == 0 || reason > 3 {
			return errors.New("release ClaimPaymentDeferred reason is invalid")
		}
	default:
		return errors.New("release claim receipt contains an unknown claim event")
	}
	return nil
}

// Finds the pool-epoch row selected by one of its receipt fields.
func finalSemanticReceiptEpochByReceipt(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, selectReceipt func(FinalEpochOperatorEvidence) FinalEVMReceipt) *FinalEpochOperatorEvidence {
	for index := range evidence.Epochs {
		if finalSemanticReceiptMatches(receipt, selectReceipt(evidence.Epochs[index])) {
			return &evidence.Epochs[index]
		}
	}
	return nil
}

// Finds the pool-epoch and leaf represented by a claim receipt.
func finalSemanticReceiptClaimByReceipt(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt) (*FinalEpochOperatorEvidence, *FinalClaimEvidence) {
	for rowIndex := range evidence.Epochs {
		for claimIndex := range evidence.Epochs[rowIndex].Claims {
			if finalSemanticReceiptMatches(receipt, evidence.Epochs[rowIndex].Claims[claimIndex].Receipt) {
				return &evidence.Epochs[rowIndex], &evidence.Epochs[rowIndex].Claims[claimIndex]
			}
		}
	}
	return nil, nil
}

// Finds the canonical payment record represented by a receipt.
func finalSemanticReceiptClaimPaymentByReceipt(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt) *FinalClaimPaymentEvidence {
	for index := range evidence.ClaimPayments {
		if finalSemanticReceiptMatches(receipt, evidence.ClaimPayments[index].Receipt) {
			return &evidence.ClaimPayments[index]
		}
	}
	return nil
}

// Collects ordinary and dishonest-deposit validator cycles for receipt binding.
func finalSemanticReceiptCycles(evidence *FinalSemanticEvidence) []FinalCRv4Cycle {
	result := make([]FinalCRv4Cycle, 0)
	for _, validator := range evidence.Validators {
		result = append(result, validator.Cycles...)
	}
	if evidence.DishonestDeposit != nil {
		for _, decision := range evidence.DishonestDeposit.Penalties {
			result = append(result, decision.Cycle)
		}
		for _, decision := range evidence.DishonestDeposit.Recoveries {
			result = append(result, decision.Cycle)
		}
	}
	return result
}

// Returns one unanimous nonzero settlement epoch, otherwise zero.
func finalSemanticDishonestDepositEpoch(decisions []FinalDishonestDepositDecision) uint64 {
	if len(decisions) == 0 {
		return 0
	}
	epoch := decisions[0].Cycle.SettlementEpoch
	if epoch == 0 {
		return 0
	}
	for _, decision := range decisions[1:] {
		if decision.Cycle.SettlementEpoch != epoch {
			return 0
		}
	}
	return epoch
}

// Requires every semantic record represented by this receipt to occur exactly once.
func verifyFinalSemanticReceiptRequiredEvents(evidence *FinalSemanticEvidence, receipt FinalEVMReceipt, events []finalSemanticEvent) error {
	for _, pool := range evidence.Pools {
		if finalSemanticReceiptMatches(receipt, pool.Registration) {
			if err := finalSemanticReceiptRequireOne(events, "PoolRegistered", func(event finalSemanticEvent) bool {
				noID, ok := finalSemanticUint(event.Args, "noId")
				return event.Name == "PoolRegistered" && ok && noID == pool.NoID
			}); err != nil {
				return err
			}
			if err := finalSemanticReceiptRequireOne(events, "OperatorScheduled", func(event finalSemanticEvent) bool {
				noID, ok := finalSemanticUint(event.Args, "noId")
				return event.Name == "OperatorScheduled" && ok && noID == pool.NoID
			}); err != nil {
				return err
			}
		}
		if finalSemanticReceiptMatches(receipt, pool.ConvictionReceipt) {
			if err := finalSemanticReceiptRequireOne(events, "Deposit/ConvictionAdded", func(event finalSemanticEvent) bool {
				noID, ok := finalSemanticUint(event.Args, "noId")
				return (event.Name == "Deposit" || event.Name == "ConvictionAdded") && ok && noID == pool.NoID
			}); err != nil {
				return err
			}
		}
	}
	for _, cycle := range finalSemanticReceiptCycles(evidence) {
		for _, pool := range cycle.Pools {
			if !finalSemanticReceiptMatches(receipt, pool.DepositReceipt) {
				continue
			}
			if err := finalSemanticReceiptRequireOne(events, "Deposit", func(event finalSemanticEvent) bool {
				noID, noOK := finalSemanticUint(event.Args, "noId")
				epoch, epochOK := finalSemanticUint(event.Args, "epoch")
				return event.Name == "Deposit" && noOK && epochOK && noID == pool.NoID && epoch == cycle.SettlementEpoch
			}); err != nil {
				return err
			}
		}
	}
	for _, addition := range evidence.Reserve.PrincipalAdditions {
		if finalSemanticReceiptMatches(receipt, addition.Receipt) {
			if err := finalSemanticReceiptRequireOne(events, "ReservePrincipalAdded", func(event finalSemanticEvent) bool {
				noID, noOK := finalSemanticUint(event.Args, "noId")
				epoch, epochOK := finalSemanticUint(event.Args, "epoch")
				return event.Name == "ReservePrincipalAdded" && noOK && epochOK && noID == addition.NoID && epoch == addition.Epoch
			}); err != nil {
				return err
			}
		}
	}
	for _, row := range evidence.Epochs {
		if finalSemanticReceiptMatches(receipt, row.Capture) {
			if err := finalSemanticReceiptRequireOne(events, "EmissionCaptured/EmissionDeferred", func(event finalSemanticEvent) bool {
				noID, noOK := finalSemanticUint(event.Args, "noId")
				epoch, epochOK := finalSemanticUint(event.Args, "epoch")
				return (event.Name == "EmissionCaptured" || event.Name == "EmissionDeferred") && noOK && epochOK && noID == row.NoID && epoch == row.Epoch
			}); err != nil {
				return err
			}
		}
		if row.Root != nil && finalSemanticReceiptMatches(receipt, *row.Root) {
			if err := finalSemanticReceiptRequireOne(events, "OperatorRootCommitted", func(event finalSemanticEvent) bool {
				noID, noOK := finalSemanticUint(event.Args, "noId")
				epoch, epochOK := finalSemanticUint(event.Args, "epoch")
				return event.Name == "OperatorRootCommitted" && noOK && epochOK && noID == row.NoID && epoch == row.Epoch
			}); err != nil {
				return err
			}
		}
		if finalSemanticReceiptMatches(receipt, row.Finalize) {
			if err := finalSemanticReceiptRequireOne(events, "OperatorEpochFinalized", func(event finalSemanticEvent) bool {
				noID, noOK := finalSemanticUint(event.Args, "noId")
				epoch, epochOK := finalSemanticUint(event.Args, "epoch")
				return event.Name == "OperatorEpochFinalized" && noOK && epochOK && noID == row.NoID && epoch == row.Epoch
			}); err != nil {
				return err
			}
			wanted := "RootMissed"
			if row.Root != nil {
				wanted = "EntitlementFinalized"
			}
			if err := finalSemanticReceiptRequireOne(events, wanted, func(event finalSemanticEvent) bool {
				noID, noOK := finalSemanticUint(event.Args, "noId")
				epoch, epochOK := finalSemanticUint(event.Args, "epoch")
				return event.Name == wanted && noOK && epochOK && noID == row.NoID && epoch == row.Epoch
			}); err != nil {
				return err
			}
		}
		for _, claim := range row.Claims {
			if !finalSemanticReceiptMatches(receipt, claim.Receipt) {
				continue
			}
			if err := finalSemanticReceiptRequireOne(events, "Claimed", func(event finalSemanticEvent) bool {
				return event.Name == "Claimed"
			}); err != nil {
				return err
			}
			wanted := "ClaimPaymentDeferred"
			if claim.PaidRao != "0" {
				wanted = "ClaimPaid"
			}
			if err := finalSemanticReceiptRequireOne(events, wanted, func(event finalSemanticEvent) bool {
				return event.Name == wanted
			}); err != nil {
				return err
			}
		}
	}
	for _, payment := range evidence.ClaimPayments {
		if !finalSemanticReceiptMatches(receipt, payment.Receipt) {
			continue
		}
		if err := finalSemanticReceiptRequireOne(events, "ClaimPaid", func(event finalSemanticEvent) bool {
			if event.Name != "ClaimPaid" {
				return false
			}
			coldkey, coldkeyOK := finalSemanticHex32(event.Args, "coldkey")
			amount, amountOK := finalSemanticInteger(event.Args, "amount")
			return coldkeyOK && amountOK && strings.EqualFold(coldkey, payment.Coldkey) && amount.String() == payment.AmountRao
		}); err != nil {
			return err
		}
	}
	if lifecycle := evidence.FleetLifecycle; lifecycle != nil {
		for _, item := range lifecycle.PayoutArtifacts {
			if !finalSemanticReceiptMatches(receipt, item.Root) {
				continue
			}
			if err := finalSemanticReceiptRequireOne(events, "OperatorRootCommitted", func(event finalSemanticEvent) bool {
				noID, noOK := finalSemanticUint(event.Args, "noId")
				epoch, epochOK := finalSemanticUint(event.Args, "epoch")
				return event.Name == "OperatorRootCommitted" && noOK && epochOK && noID == item.NoID && epoch == item.Epoch
			}); err != nil {
				return err
			}
		}
	}
	if dishonest := evidence.DishonestDeposit; dishonest != nil {
		for _, expected := range []FinalEVMReceipt{dishonest.UnderpaymentReceipt, dishonest.RecoveryDepositReceipt} {
			if finalSemanticReceiptMatches(receipt, expected) {
				if err := finalSemanticReceiptRequireOne(events, "Dishonest Deposit", func(event finalSemanticEvent) bool { return event.Name == "Deposit" }); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Requires exactly one decoded event to satisfy a semantic predicate.
func finalSemanticReceiptRequireOne(events []finalSemanticEvent, label string, match func(finalSemanticEvent) bool) error {
	count := 0
	for _, event := range events {
		if match(event) {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("release receipt has %d %s events, want exactly one", count, label)
	}
	return nil
}
