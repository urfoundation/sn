// Native production commands have one durable owner across preparation,
// uncertain transmission, historical dispatch verification and postcondition.
package miner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/docopt/docopt-go"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const fleetRecoveryScanLimit = 4096

// A verified finite archive range can be continued by the next invocation.
type fleetRecoveryScanPending struct {
	number uint64
	hash   types.Hash
}

// Diagnostic progress is not a transaction outcome or replacement authority.
func (self *fleetRecoveryScanPending) Error() string {
	return fmt.Sprintf("native archive scan checkpointed through %d; re-run to continue bounded recovery", self.number)
}

// An unresolved liability is never permission to sign a replacement.
func fleetRecoveryUnresolved(record *fleetRecoveryRecord, err error) error {
	return fmt.Errorf("fleet %s transaction %s remains unresolved; original signed bytes retained: %w", record.Intent.Action, record.TxHash, err)
}

// A terminal failed transaction stays terminal and reports failure on every
// retry. It cannot be silently replaced, but no longer owns a pending nonce.
func fleetRecoveryCompleted(record *fleetRecoveryRecord) error {
	if !record.Succeeded {
		return fmt.Errorf("fleet %s original transaction %s finalized with failure: %s", record.Intent.Action, record.TxHash, record.Outcome)
	}
	fmt.Printf("fleet %s already finalized: %s (%s)\n", record.Intent.Action, record.TxHash, record.Outcome)
	return nil
}

// Captures exact canonical manifest semantics before loading any signing key.
func fleetRecoveryNewIntent(action string, authority *fleetMainnetRuntimeAuthority, manifest *protocol.FleetManifest) (fleetRecoveryIntent, error) {
	raw, err := manifest.Canonical()
	return fleetRecoveryIntent{Action: action, Genesis: authority.GenesisHash, Manifest: raw}, err
}

// The original approval is copied at prepare time, not reopened after send.
func fleetRecoveryPrepared(intent fleetRecoveryIntent, authority *fleetMainnetRuntimeAuthority, start types.Hash, number uint64) *fleetRecoveryRecord {
	digest := sha256.Sum256(authority.document)
	return &fleetRecoveryRecord{Schema: fleetRecoverySchema, Id: intent.id(), Intent: intent, Authority: append([]byte(nil), authority.document...), AuthoritySha256: hex.EncodeToString(digest[:]), StartHash: start, StartNumber: number, Stage: "prepared"}
}

// A reached network must match the original identity even when its current
// runtime upgraded. Historical recovery authenticates each consumed artifact.
func (self *fleetMainnetRuntimeAuthority) recoveryNetwork(ctx context.Context, chain *crv4.Chain) error {
	var genesis types.Hash
	var name string
	if err := chain.API.Client.CallContext(ctx, &genesis, "chain_getBlockHash", uint64(0)); err != nil {
		return err
	}
	if err := chain.API.Client.CallContext(ctx, &name, "system_chain"); err != nil {
		return err
	}
	if genesis.Hex() != self.GenesisHash || name != self.NativeChain || chain.GenesisHash != genesis || chain.ProvisionalRuntimeCompatibilityEnabled() {
		return errors.New("fleet recovery original fresh network identity differs")
	}
	return ctx.Err()
}

// Reads a canonical native header; no equal-number or receipt assertion alone
// substitutes for canonical hash and parent continuity.
func fleetRecoveryNativeHeader(ctx context.Context, chain *crv4.Chain, number uint64, want types.Hash) (types.Header, error) {
	var hash types.Hash
	if err := chain.API.Client.CallContext(ctx, &hash, "chain_getBlockHash", number); err != nil {
		return types.Header{}, err
	}
	if hash == (types.Hash{}) || (want != (types.Hash{}) && hash != want) {
		return types.Header{}, errors.New("fleet recovery native canonical hash differs")
	}
	observedNumber, parent, err := chain.ReceiptHeaderAtContext(ctx, hash)
	if err != nil {
		return types.Header{}, err
	}
	if observedNumber != number {
		return types.Header{}, errors.New("fleet recovery native header differs")
	}
	return types.Header{Number: types.BlockNumber(number), ParentHash: parent}, nil
}

// Registration and publication share the same durable path; testnet retains
// its existing operational behavior and never acquires this mainnet authority.
func fleetRecoverableNative(ctx context.Context, opts docopt.Opts, manifest *protocol.FleetManifest, authority *fleetMainnetRuntimeAuthority, action string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	intent, err := fleetRecoveryNewIntent(action, authority, manifest)
	if err != nil {
		return err
	}
	store, err := openFleetRecoveryStore(ctx)
	if err != nil {
		return err
	}
	defer store.close()
	record, err := store.find(intent)
	if err != nil {
		return err
	}
	seed, err := crv4.LoadSeedFile(fleetOpt(opts, "--hotkey_seed_file"))
	if err != nil {
		return err
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil || hotkey.PublicKey() != manifest.Hotkey {
		return errors.Join(errors.New("hotkey seed does not match manifest hotkey"), err)
	}
	key := hotkey
	if action == "register" {
		key, err = snchain.LoadKeypairFile(fleetOpt(opts, "--coldkey_seed_file"))
		if err != nil {
			return err
		}
	}
	signer := fleetRecoverySigner{native: key}
	if record != nil {
		if record.NativeSigner != key.PublicKey() {
			return errors.New("fleet recovery original native signer differs")
		}
		if record.Stage == "finalized" {
			return fleetRecoveryCompleted(record)
		}
		authority, _, err = record.authority()
		if err != nil {
			return err
		}
	}
	endpointContext := func(parent context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
		return context.WithTimeout(parent, budget)
	}
	chain, _, err := dialFleetNativeWithEndpointContext(ctx, fleetOpts(opts, "--substrate"), fleetNativeEndpointTimeout, endpointContext, crv4.DialChainContext, authority.recoveryNetwork)
	if err != nil {
		return err
	}
	defer closeFleetNative(chain)
	if record != nil {
		return fleetRecoveryResumeNative(ctx, store, record, signer, authority, chain, action == "publish" || mustBoolOpt(opts, "--apply"))
	}
	purpose := fleetNativeWritePurpose(action)
	view, start, err := authority.finalizedFor(ctx, chain, purpose)
	if err != nil {
		return err
	}
	number, _, err := chain.ReceiptHeaderAtContext(ctx, start)
	if err != nil {
		return err
	}
	prepare := func(result snchain.SubmitResult) error {
		record = fleetRecoveryPrepared(intent, authority, start, number)
		record.NativeSigner, record.Nonce, record.Raw, record.TxHash = key.PublicKey(), uint64(result.Nonce), append([]byte(nil), result.Raw...), result.ExtrinsicHash.Hex()
		return store.put(record, signer)
	}
	before := func() error {
		copy := *record
		copy.Stage = "may_have_sent"
		if err := store.put(&copy, signer); err != nil {
			return err
		}
		record = &copy
		return nil
	}
	var receipt *crv4.FinalizedExtrinsic
	if action == "register" {
		burnLimit, err := fleetUint64Opt(opts, "--burn_limit_rao", 0)
		if err != nil {
			return err
		}
		feeLimit, err := fleetUint64Opt(opts, "--fee_limit_rao", fleetDefaultFeeLimitRao)
		if err != nil {
			return err
		}
		stateDir, err := providerStateDir()
		if err != nil {
			return err
		}
		journal, err := snchain.OpenOwnerLocalJournal(ctx, filepath.Join(stateDir, "fleet-native"))
		// The failed opener has joined/closed its own descriptors. One bounded
		// exact-custody reconciliation resumes only this native owner; unknown
		// partial history stays retained and refuses a new signing/send attempt.
		if errors.Is(err, snchain.ErrJournalUncertain) {
			journal, err = snchain.ReconcileOwnerLocalJournal(ctx, filepath.Join(stateDir, "fleet-native"))
		}
		if err != nil {
			return err
		}
		defer journal.Close()
		admission := func(readCtx context.Context, block types.Hash) error {
			if block == (types.Hash{}) {
				return authority.signingAdmission(readCtx, chain, start, purpose)
			}
			_, err := authority.authenticateFor(readCtx, chain, block, crv4.FleetRegistrationRead)
			return err
		}
		receiptRuntime := func(readCtx context.Context, block types.Hash) (crv4.AuthenticatedRuntimeArtifact, error) {
			return authority.authenticateFor(readCtx, chain, block, crv4.FleetRegistrationRead)
		}
		result, err := snchain.RegisterHotkey(ctx, view, snchain.RegisterRequest{Command: "provider fleet register", Netuid: manifest.Netuid, Hotkey: manifest.Hotkey, Coldkey: key, BurnLimitRao: burnLimit, FeeLimitRao: feeLimit, Allowed: authority.artifactIdentities(), RuntimeAdmission: admission, ExecutionRuntime: authority.executionAdmission(chain, start, purpose), ReceiptRuntime: receiptRuntime, Journal: journal, Apply: mustBoolOpt(opts, "--apply"), Output: os.Stdout, Prepared: prepare, BeforeBroadcast: before})
		if err != nil {
			if record != nil {
				return fleetRecoveryUnresolved(record, err)
			}
			return err
		}
		if result.Submit == nil || result.Submit.Receipt == nil {
			return nil
		}
		receipt = result.Submit.Receipt
	} else {
		hash, _ := manifest.CommitmentHash()
		call, err := view.NewSetFleetCommitmentCall(manifest.Netuid, hash)
		if err != nil {
			return err
		}
		nonce, err := view.AccountNonceContext(ctx, key.Address())
		if err != nil {
			return err
		}
		if err := authority.signingAdmission(ctx, chain, start, purpose); err != nil {
			return err
		}
		if err := store.beforeExternal(ctx); err != nil {
			return err
		}
		raw, err := snchain.EncodeSignedCall(view, key.Ring, call, nonce)
		if err != nil {
			return err
		}
		if err := prepare(snchain.SubmitResult{Raw: raw, Nonce: nonce, ExtrinsicHash: snchain.ExtrinsicHash(raw)}); err != nil {
			return err
		}
		if err := authority.signingAdmission(ctx, chain, start, purpose); err != nil {
			return fleetRecoveryUnresolved(record, err)
		}
		if err := before(); err != nil {
			return err
		}
		if err := store.beforeExternal(ctx); err != nil {
			return err
		}
		receipt, err = view.SubmitRawAndWatchFinalizedRuntime(ctx, codec.HexEncodeToString(record.Raw), authority.executionAdmission(chain, start, purpose))
		if err != nil {
			return fleetRecoveryUnresolved(record, err)
		}
	}
	return fleetRecoveryFinishNative(ctx, store, record, signer, authority, chain, receipt)
}

// Historical decoding uses the original exact approval. Neither an upgraded
// current head nor an error after inclusion causes a fresh signature.
func fleetRecoveryFinishNative(ctx context.Context, store *fleetRecoveryStore, record *fleetRecoveryRecord, signer fleetRecoverySigner, authority *fleetMainnetRuntimeAuthority, chain *crv4.Chain, receipt *crv4.FinalizedExtrinsic) error {
	if receipt == nil || receipt.ExtrinsicHash.Hex() != record.TxHash || receipt.BlockNumber <= record.StartNumber {
		return fleetRecoveryUnresolved(record, errors.New("native receipt identity differs"))
	}
	finalized, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	finalizedNumber, _, err := chain.ReceiptHeaderAtContext(ctx, finalized)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	if finalizedNumber < receipt.BlockNumber {
		return fleetRecoveryUnresolved(record, errors.New("native receipt is not covered by finality"))
	}
	if _, err := fleetRecoveryNativeHeader(ctx, chain, finalizedNumber, finalized); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	if _, err := fleetRecoveryNativeHeader(ctx, chain, receipt.BlockNumber, receipt.BlockHash); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	_, parent, err := chain.ReceiptHeaderAtContext(ctx, receipt.BlockHash)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	execution, err := authority.executionAdmission(chain, record.StartHash, fleetNativeWritePurpose(record.Intent.Action))(ctx, parent)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	view := *chain
	if err := view.BindRuntimeArtifact(execution); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	if err := view.VerifyFinalizedExtrinsicContext(ctx, receipt.BlockHash, receipt.ExtrinsicHash); err != nil {
		var failed *crv4.FinalizedDispatchError
		if !errors.As(err, &failed) || failed.ExtrinsicHash != receipt.ExtrinsicHash || failed.BlockHash != receipt.BlockHash {
			return fleetRecoveryUnresolved(record, err)
		}
		copy := *record
		copy.NativeReceipt, copy.Stage, copy.Outcome = receipt, "finalized", failed.Error()
		if err := store.put(&copy, signer); err != nil {
			return err
		}
		return fleetRecoveryCompleted(&copy)
	}
	_, manifest, err := record.authority()
	if err != nil {
		return err
	}
	copy := *record
	if record.Intent.Action == "publish" {
		hash, _ := manifest.CommitmentHash()
		observed, err := authority.commitmentWrite(ctx, chain, manifest.Netuid, manifest.Hotkey, hash, receipt)
		if err != nil {
			return fleetRecoveryUnresolved(record, err)
		}
		copy.Outcome = fmt.Sprintf("commitment 0x%x written at native block %d", observed.Hash, observed.CommitmentBlock)
	} else {
		postState, err := authority.viewFor(ctx, chain, receipt.BlockHash, crv4.FleetRegistrationRead)
		if err != nil {
			return fleetRecoveryUnresolved(record, err)
		}
		uid, present, err := snchain.UIDAtContext(ctx, postState, manifest.Netuid, manifest.Hotkey, receipt.BlockHash)
		if err != nil {
			return fleetRecoveryUnresolved(record, err)
		}
		if !present {
			return fleetRecoveryUnresolved(record, errors.New("native registration readback absent"))
		}
		owner, err := snchain.HotkeyOwnerAtContext(ctx, postState, manifest.Hotkey, receipt.BlockHash)
		if err != nil {
			return fleetRecoveryUnresolved(record, err)
		}
		if owner != record.NativeSigner {
			return fleetRecoveryUnresolved(record, errors.New("native registration owner differs"))
		}
		copy.Outcome = fmt.Sprintf("uid %d registered under 0x%x", uid, owner)
	}
	copy.NativeReceipt, copy.Stage, copy.Succeeded = receipt, "finalized", true
	if err := store.put(&copy, signer); err != nil {
		return err
	}
	fmt.Printf("fleet %s finalized: %s (%s)\n", record.Intent.Action, record.TxHash, copy.Outcome)
	return nil
}

// Each invocation scans a finite canonical range, retaining its authenticated
// absence checkpoint so long archive gaps can continue without starting over.
func fleetRecoveryResumeNative(ctx context.Context, store *fleetRecoveryStore, record *fleetRecoveryRecord, signer fleetRecoverySigner, authority *fleetMainnetRuntimeAuthority, chain *crv4.Chain, apply bool) error {
	return fleetRecoveryResumeNativeRange(ctx, store, record, signer, authority, chain, apply, fleetRecoveryScanLimit)
}

// The range budget is instance-owned so tests can force the exact checkpoint
// boundary without thousands of synthetic network calls or timing assumptions.
func fleetRecoveryResumeNativeRange(ctx context.Context, store *fleetRecoveryStore, record *fleetRecoveryRecord, signer fleetRecoverySigner, authority *fleetMainnetRuntimeAuthority, chain *crv4.Chain, apply bool, maxBlocks uint64) error {
	from, parent := record.StartNumber, record.StartHash
	if record.ScanProof == fleetRecoveryNativeScanProof && record.ScanNumber != 0 {
		from, parent = record.ScanNumber, record.ScanHash
	}
	if record.ScanProof != "" && record.ScanProof != fleetRecoveryNativeScanProof {
		return fleetRecoveryUnresolved(record, errors.New("native archive scan proof version is unsupported"))
	}
	if maxBlocks == 0 || maxBlocks > fleetRecoveryScanLimit {
		return fleetRecoveryUnresolved(record, errors.New("native archive recovery range differs"))
	}
	if _, err := fleetRecoveryNativeHeader(ctx, chain, record.StartNumber, record.StartHash); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	extrinsicHash, err := types.NewHashFromHexString(record.TxHash)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	var finalized types.Hash
	remaining := maxBlocks
	for {
		scan, err := chain.ScanFinalizedExtrinsicRange(ctx, extrinsicHash, crv4.FinalizedExtrinsicScanRange{
			First: from + 1, PreviousHash: parent, MaximumBlocks: min(remaining, crv4.ReceiptScanChunkBlockLimit),
		})
		if err != nil {
			// The failed body contributes no evidence. Earlier complete bodies
			// can survive a late read failure without changing its hard/unknown
			// classification or allocating another signature.
			if through, hash, covered := scan.AbsenceBoundary(); covered && ctx.Err() == nil {
				copy := *record
				copy.ScanNumber, copy.ScanHash, copy.ScanProof = through, hash, fleetRecoveryNativeScanProof
				if writeErr := store.put(&copy, signer); writeErr != nil {
					return errors.Join(err, writeErr)
				}
				record = &copy
			}
			return fleetRecoveryUnresolved(record, err)
		}
		if receipt := scan.Receipt(); receipt != nil {
			return fleetRecoveryFinishNative(ctx, store, record, signer, authority, chain, receipt)
		}
		through, hash, covered := scan.AbsenceBoundary()
		if !covered {
			end, endHash := scan.FinalizedBoundary()
			if end < from {
				return fleetRecoveryUnresolved(record, &crv4.ReceiptEvidenceUnavailableError{BlockHash: parent, Field: "finalized receipt head has not reached retained checkpoint"})
			}
			if end != from || endHash != parent {
				return fleetRecoveryUnresolved(record, errors.New("native archive empty range differs from retained checkpoint"))
			}
			// StartNumber was finalized before signing, or a qualified signed
			// checkpoint already covers this exact head. No newer nonce is read.
			finalized = endHash
			break
		}
		if through <= from || through-from > remaining {
			return fleetRecoveryUnresolved(record, errors.New("native archive chunk exceeds its requested range"))
		}
		copy := *record
		copy.ScanNumber, copy.ScanHash, copy.ScanProof = through, hash, fleetRecoveryNativeScanProof
		if err := store.put(&copy, signer); err != nil {
			return err
		}
		record = &copy
		remaining -= through - from
		from, parent = through, hash
		if scan.ReachedFinalizedBoundary() {
			finalized = hash
			break
		}
		if remaining == 0 {
			return fleetRecoveryUnresolved(record, fmt.Errorf("native archive scan checkpointed through %d; re-run to continue bounded recovery", through))
		}
	}
	purpose := fleetNativeWritePurpose(record.Intent.Action)
	view, err := authority.viewFor(ctx, chain, finalized, purpose)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	nonce, err := view.AccountNonceAtContext(ctx, record.NativeSigner, finalized)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	if uint64(nonce) != record.Nonce {
		return fleetRecoveryUnresolved(record, errors.New("native original nonce is not available"))
	}
	if !apply {
		return fleetRecoveryUnresolved(record, errors.New("native replay requires --apply"))
	}
	if err := authority.signingAdmission(ctx, chain, record.StartHash, purpose); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	copy := *record
	copy.Stage = "may_have_sent"
	if err := store.put(&copy, signer); err != nil {
		return err
	}
	if err := store.beforeExternal(ctx); err != nil {
		return err
	}
	receipt, err := view.SubmitRawAndWatchFinalizedRuntime(ctx, codec.HexEncodeToString(record.Raw), authority.executionAdmission(chain, record.StartHash, purpose))
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	return fleetRecoveryFinishNative(ctx, store, &copy, signer, authority, chain, receipt)
}
