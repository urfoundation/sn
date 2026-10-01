// EVM fleet recovery binds exact signed calls to canonical receipts and the
// first finalized native insertion of their EVM block hash. Missing mapping
// remains unresolved; an EVM height alone is never native finality.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Borrows the exact EVM connection for authenticated native identity/history.
func fleetRecoveryEvmNative(client *ethclient.Client, authority *fleetMainnetRuntimeAuthority) *crv4.Chain {
	genesis, _ := types.NewHashFromHexString(authority.GenesisHash)
	return &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: &fleetEvmNativeReadClient{client: client.Client()}}, GenesisHash: genesis}
}

// An old pending transaction can be reconciled after an upgrade without
// installing a new runtime approval or authorizing any fresh signature.
func fleetRecoveryDialEvm(ctx context.Context, endpoints []string, authority *fleetMainnetRuntimeAuthority) (*ethclient.Client, string, error) {
	var errs []error
	for _, endpoint := range endpoints {
		client, err := ethclient.DialContext(ctx, endpoint)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		id, err := client.ChainID(ctx)
		if err != nil {
			client.Close()
			return nil, "", fmt.Errorf("read fleet recovery EVM chain id: %w", err)
		}
		if id == nil || !id.IsUint64() || id.Uint64() != authority.EvmChainId {
			client.Close()
			return nil, "", errors.New("fleet recovery EVM chain id differs")
		}
		if err := authority.recoveryNetwork(ctx, fleetRecoveryEvmNative(client, authority)); err != nil {
			client.Close()
			return nil, "", err
		}
		return client, endpoint, nil
	}
	if len(errs) != 0 {
		return nil, "", fmt.Errorf("fleet recovery EVM transport unavailable: %w", errors.Join(errs...))
	}
	return nil, "", errors.New("fleet recovery EVM transport endpoints are absent")
}

// Intent selection and the journal lock precede every consent or transaction
// signature. Pending records keep their original actor, approval and bytes.
func fleetRecoverableEvm(opts docopt.Opts, manifest *protocol.FleetManifest, authority *fleetMainnetRuntimeAuthority, action string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fleetStatusTimeout)
	defer cancel()
	intent, err := fleetRecoveryNewIntent(action, authority, manifest)
	if err != nil {
		return err
	}
	intent.ClientId, err = parseClientID16(fleetOpt(opts, "--client_id"))
	if err != nil {
		return err
	}
	member, err := fleetMember(manifest, intent.ClientId)
	if err != nil {
		return err
	}
	if action == "bind" {
		intent.FromEpoch, err = parseFleetEpoch(opts, "--valid_from_epoch")
		if err != nil {
			return err
		}
		intent.ToEpoch, err = parseFleetEpoch(opts, "--valid_to_epoch")
	} else {
		intent.FromEpoch, err = parseFleetEpoch(opts, "--effective_epoch")
	}
	if err != nil {
		return err
	}
	store, err := openFleetRecoveryStore()
	if err != nil {
		return err
	}
	defer store.close()
	record, err := store.find(intent)
	if err != nil {
		return err
	}
	key, err := onchain.LoadKeyFile(fleetOpt(opts, "--relayer_key_file"))
	if err != nil {
		return err
	}
	signer := fleetRecoverySigner{evm: key}
	if record != nil {
		if record.EvmSigner != crypto.PubkeyToAddress(key.PublicKey) {
			return errors.New("fleet recovery original EVM signer differs")
		}
		if record.Stage == "finalized" {
			return fleetRecoveryCompleted(record)
		}
		authority, _, err = record.authority()
		if err != nil {
			return err
		}
	}
	client, endpoint, err := fleetRecoveryDialEvm(ctx, fleetOpts(opts, "--rpc"), authority)
	if err != nil {
		return err
	}
	defer client.Close()
	if record != nil {
		return fleetRecoveryResumeEvm(ctx, store, record, signer, authority, client, !mustBoolOpt(opts, "--dry-run"))
	}
	if err := authority.admitEvm(ctx, client, nil); err != nil {
		return err
	}
	chain := fleetRecoveryEvmNative(client, authority)
	start, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return err
	}
	header, err := chain.HeaderAtContext(ctx, start)
	if err != nil {
		return err
	}
	var calldata []byte
	if action == "bind" {
		binding, clientSignature, hotkeySignature, err := fleetBindingAndSign(opts, manifest)
		if err != nil {
			return err
		}
		calldata, err = onchain.BuildFleetBindingCalldata(binding, clientSignature, hotkeySignature)
		if err != nil {
			return err
		}
	} else {
		revoke := protocol.FleetRevoke{ChainID: manifest.ChainID, Netuid: manifest.Netuid, Coordinator: manifest.Coordinator, ClientID: intent.ClientId, Generation: manifest.Generation, EffectiveEpoch: intent.FromEpoch}
		digest, err := revoke.Digest()
		if err != nil {
			return err
		}
		ret, _, err := authority.coordinatorCall(ctx, manifest, []string{endpoint}, stCoordinator.PackFleetRevokeDigest(intent.ClientId, manifest.Generation, intent.FromEpoch))
		if err != nil {
			return err
		}
		observed, err := stCoordinator.UnpackFleetRevokeDigest(ret)
		if err != nil || digest != observed {
			return errors.Join(errors.New("fleet revoke digest differs from local signing domain"), err)
		}
		private, err := loadEd25519Seed(fleetOpt(opts, "--client_seed_file"))
		if err != nil {
			return err
		}
		if !bytes.Equal(private.Public().(ed25519.PublicKey), member.ClientKey[:]) {
			return errors.New("client seed does not match manifest member")
		}
		calldata, err = stCoordinator.TryPackRevokeFleetBinding(intent.ClientId, manifest.Generation, intent.FromEpoch, ed25519.Sign(private, digest[:]))
		if err != nil {
			return err
		}
	}
	receipt, err := onchain.SubmitWithHooks(ctx, onchain.SubmitParams{Contract: common.Address(manifest.Coordinator), Rpcs: []string{endpoint}, Key: key, Calldata: calldata, ChainID: new(big.Int).SetUint64(manifest.ChainID), DryRun: mustBoolOpt(opts, "--dry-run"), RuntimeAdmission: func(ctx context.Context, actual *ethclient.Client, number *big.Int) error {
		// Receipt admission needs the exact hash and native mapping, checked
		// below. Preflight/sign/send use this submitter's actual connection.
		if number != nil {
			return nil
		}
		return authority.admitEvm(ctx, actual, nil)
	}}, onchain.SubmitHooks{Prepared: func(hash common.Hash, raw []byte) error {
		var tx ethtypes.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return err
		}
		record = fleetRecoveryPrepared(intent, authority, start, uint64(header.Number))
		record.EvmSigner, record.Nonce, record.Raw, record.TxHash = crypto.PubkeyToAddress(key.PublicKey), tx.Nonce(), append([]byte(nil), raw...), hash.Hex()
		return store.put(record, signer)
	}, BeforeBroadcast: func(common.Hash) error {
		copy := *record
		copy.Stage = "may_have_sent"
		if err := store.put(&copy, signer); err != nil {
			return err
		}
		record = &copy
		return nil
	}})
	if err != nil {
		if record != nil {
			return fleetRecoveryUnresolved(record, err)
		}
		return err
	}
	if receipt == nil {
		return nil
	}
	return fleetRecoveryFinishEvm(ctx, store, record, signer, authority, client, receipt)
}

// Canonical EVM inclusion is necessary but insufficient: native first-insert
// storage proof under the original reviewed artifact is required as well.
func fleetRecoveryEvmMapping(ctx context.Context, record *fleetRecoveryRecord, authority *fleetMainnetRuntimeAuthority, client *ethclient.Client, receipt *ethtypes.Receipt) (*crv4.EVMCheckpointObservation, error) {
	if receipt == nil || receipt.TxHash.Hex() != record.TxHash || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Sign() <= 0 || receipt.BlockHash == (common.Hash{}) {
		return nil, errors.New("fleet recovery EVM receipt identity differs")
	}
	finalized, err := onchain.ReadEVMBlockIdentity(ctx, client, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return nil, fmt.Errorf("read fleet recovery EVM finalized head: %w", err)
	}
	if finalized.Number < receipt.BlockNumber.Uint64() {
		return nil, errors.New("fleet recovery EVM receipt is not finalized")
	}
	canonical, err := onchain.ReadEVMBlockIdentity(ctx, client, receipt.BlockNumber)
	if err != nil {
		return nil, fmt.Errorf("read fleet recovery EVM canonical block: %w", err)
	}
	if canonical.Hash != receipt.BlockHash {
		return nil, errors.New("fleet recovery EVM receipt is not canonical")
	}
	chain := fleetRecoveryEvmNative(client, authority)
	if err := authority.recoveryNetwork(ctx, chain); err != nil {
		return nil, err
	}
	headHash, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return nil, err
	}
	end, _, err := chain.ReceiptHeaderAtContext(ctx, headHash)
	if err != nil {
		return nil, err
	}
	if _, err := fleetRecoveryNativeHeader(ctx, chain, end, headHash); err != nil {
		return nil, err
	}
	from, parent := record.StartNumber, record.StartHash
	if record.ScanNumber != 0 {
		from, parent = record.ScanNumber, record.ScanHash
	}
	if end <= record.StartNumber || end < from {
		return nil, errors.New("fleet recovery EVM/native mapping range unavailable")
	}
	if _, err := fleetRecoveryNativeHeader(ctx, chain, record.StartNumber, record.StartHash); err != nil {
		return nil, err
	}
	if _, err := fleetRecoveryNativeHeader(ctx, chain, from, parent); err != nil {
		return nil, err
	}
	through := min(end, from+fleetRecoveryScanLimit)
	for number := from + 1; number <= through; number++ {
		var native types.Hash
		if err := chain.API.Client.CallContext(ctx, &native, "chain_getBlockHash", number); err != nil {
			return nil, err
		}
		header, err := fleetRecoveryNativeHeader(ctx, chain, number, native)
		if err != nil {
			return nil, err
		}
		if header.ParentHash != parent {
			return nil, errors.New("fleet recovery EVM/native ancestry differs")
		}
		// Both artifact/name checks and first-insertion proof are required.
		artifact, err := authority.authenticateAt(ctx, chain, native)
		if err != nil {
			return nil, err
		}
		arg := make([]byte, 32)
		binary.LittleEndian.PutUint64(arg, receipt.BlockNumber.Uint64())
		key, err := types.CreateStorageKey(artifact.Metadata, "Ethereum", "BlockHash", arg)
		if err != nil {
			return nil, err
		}
		var encoded *string
		if err := chain.API.Client.CallContext(ctx, &encoded, "state_getStorage", key.Hex(), native.Hex()); err != nil {
			return nil, err
		}
		parent = native
		if encoded == nil {
			continue
		}
		value, err := codec.HexDecodeString(*encoded)
		if err != nil || len(value) != 32 {
			return nil, errors.New("fleet recovery native/EVM mapping bytes malformed")
		}
		if bytes.Equal(value, make([]byte, 32)) {
			continue
		}
		if !bytes.Equal(value, receipt.BlockHash[:]) {
			return nil, errors.New("fleet recovery native/EVM mapping conflicts with canonical receipt")
		}
		query := crv4.EVMCheckpointQuery{GenesisHash: chain.GenesisHash, NativeHash: native, NativeNumber: number, EVMHash: types.Hash(receipt.BlockHash), EVMNumber: receipt.BlockNumber.Uint64()}
		observation, err := crv4.ReadEVMCheckpointAtContext(ctx, chain, query, authority.artifactIdentity())
		if err == nil {
			return &observation, nil
		}
		return nil, err
	}
	if through != end {
		return nil, &fleetRecoveryScanPending{number: through, hash: parent}
	}
	return nil, errors.New("fleet recovery finalized native/EVM mapping unresolved")
}

// Exact receipt events and a hash-pinned contract getter prove the semantic
// outcome; a successful status or unrelated event cannot complete the intent.
func fleetRecoveryFinishEvm(ctx context.Context, store *fleetRecoveryStore, record *fleetRecoveryRecord, signer fleetRecoverySigner, authority *fleetMainnetRuntimeAuthority, client *ethclient.Client, receipt *ethtypes.Receipt) error {
	mapping, err := fleetRecoveryEvmMapping(ctx, record, authority, client, receipt)
	if err != nil {
		var scan *fleetRecoveryScanPending
		if errors.As(err, &scan) {
			copy := *record
			copy.ScanNumber, copy.ScanHash = scan.number, scan.hash
			if saveErr := store.put(&copy, signer); saveErr != nil {
				return saveErr
			}
		}
		return fleetRecoveryUnresolved(record, fmt.Errorf("runtime admission at EVM receipt: %w", err))
	}
	included, err := client.TransactionInBlock(ctx, receipt.BlockHash, receipt.TransactionIndex)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	includedRaw, err := included.MarshalBinary()
	if err != nil || !bytes.Equal(includedRaw, record.Raw) {
		return fleetRecoveryUnresolved(record, errors.Join(errors.New("canonical EVM block does not contain the exact signed transaction"), err))
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		if receipt.Status != ethtypes.ReceiptStatusFailed {
			return fleetRecoveryUnresolved(record, errors.New("original EVM transaction receipt status is invalid"))
		}
		copy := *record
		copy.Stage, copy.Outcome, copy.EvmReceipt, copy.Mapping = "finalized", "original EVM transaction reverted", receipt, mapping
		if err := store.put(&copy, signer); err != nil {
			return err
		}
		return fleetRecoveryCompleted(&copy)
	}
	_, manifest, err := record.authority()
	if err != nil {
		return err
	}
	member, err := fleetMember(manifest, record.Intent.ClientId)
	if err != nil {
		return err
	}
	hash, _ := manifest.CommitmentHash()
	matches := 0
	var uid uint16
	for _, log := range receipt.Logs {
		if log == nil || log.Address != common.Address(manifest.Coordinator) {
			continue
		}
		if log.Removed || log.TxHash != receipt.TxHash || log.BlockHash != receipt.BlockHash || log.BlockNumber != receipt.BlockNumber.Uint64() || log.TxIndex != receipt.TransactionIndex {
			return fleetRecoveryUnresolved(record, errors.New("fleet event inclusion differs"))
		}
		if record.Intent.Action == "bind" {
			event, err := stCoordinator.UnpackFleetBoundEvent(log)
			if err != nil {
				continue
			}
			if event.ClientId != record.Intent.ClientId || event.FleetId != manifest.FleetID || event.Hotkey != manifest.Hotkey || event.Generation != manifest.Generation || event.ValidFromEpoch != record.Intent.FromEpoch || event.ValidToEpoch != record.Intent.ToEpoch {
				return fleetRecoveryUnresolved(record, errors.New("fleet binding event differs"))
			}
			uid = event.Uid
		} else {
			event, err := stCoordinator.UnpackFleetBindingRevokedEvent(log)
			if err != nil {
				continue
			}
			if event.ClientId != record.Intent.ClientId || event.Generation != manifest.Generation || event.EffectiveEpoch != record.Intent.FromEpoch {
				return fleetRecoveryUnresolved(record, errors.New("fleet revocation event differs"))
			}
		}
		matches++
	}
	if matches != 1 {
		return fleetRecoveryUnresolved(record, fmt.Errorf("fleet receipt has %d exact events, want one", matches))
	}
	contract := common.Address(manifest.Coordinator)
	ret, err := client.CallContractAtHash(ctx, ethereum.CallMsg{To: &contract, Data: stCoordinator.PackGetFleetBinding(record.Intent.ClientId)}, receipt.BlockHash)
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	binding, err := stCoordinator.UnpackGetFleetBinding(ret)
	if err != nil || binding.FleetId != manifest.FleetID || binding.Hotkey != manifest.Hotkey || binding.ClientKey != member.ClientKey || binding.CommitmentHash != hash || binding.Generation != manifest.Generation {
		return fleetRecoveryUnresolved(record, errors.Join(errors.New("fleet binding readback identity differs"), err))
	}
	if record.Intent.Action == "bind" {
		if binding.Uid != uid || binding.ValidFromEpoch != record.Intent.FromEpoch || binding.ValidToEpoch != record.Intent.ToEpoch || binding.Cleaned {
			return fleetRecoveryUnresolved(record, errors.New("fleet binding readback epochs or uid differ"))
		}
	} else if record.Intent.FromEpoch == 0 || binding.ValidToEpoch != record.Intent.FromEpoch-1 {
		return fleetRecoveryUnresolved(record, errors.New("fleet revocation readback differs"))
	}
	// The same historical block remains admitted after the getter returns.
	if _, err := authority.authenticateAt(ctx, fleetRecoveryEvmNative(client, authority), mapping.Query.NativeHash); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	copy := *record
	copy.Stage, copy.Outcome, copy.EvmReceipt, copy.Mapping, copy.Succeeded = "finalized", "exact event and binding readback", receipt, mapping, true
	if err := store.put(&copy, signer); err != nil {
		return err
	}
	fmt.Printf("fleet %s finalized: %s (native %d, EVM %d)\n", record.Intent.Action, record.TxHash, mapping.Query.NativeNumber, mapping.Query.EVMNumber)
	return nil
}

// Receipt recovery always precedes replay. Only the original signed bytes
// may be retransmitted, under their original runtime and finalized nonce.
func fleetRecoveryResumeEvm(ctx context.Context, store *fleetRecoveryStore, record *fleetRecoveryRecord, signer fleetRecoverySigner, authority *fleetMainnetRuntimeAuthority, client *ethclient.Client, apply bool) error {
	receipt, err := client.TransactionReceipt(ctx, common.HexToHash(record.TxHash))
	if err == nil {
		return fleetRecoveryFinishEvm(ctx, store, record, signer, authority, client, receipt)
	}
	if !errors.Is(err, ethereum.NotFound) {
		return fleetRecoveryUnresolved(record, err)
	}
	if err := authority.admitEvm(ctx, client, nil); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	head, err := onchain.ReadEVMBlockIdentity(ctx, client, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	nonce, err := client.NonceAtHash(ctx, record.EvmSigner, head.Hash)
	if err != nil {
		return fleetRecoveryUnresolved(record, fmt.Errorf("read original EVM nonce: %w", err))
	}
	if nonce != record.Nonce {
		return fleetRecoveryUnresolved(record, errors.New("original EVM nonce is not available"))
	}
	if !apply {
		return fleetRecoveryUnresolved(record, errors.New("EVM replay is disabled by --dry-run"))
	}
	var tx ethtypes.Transaction
	if err := tx.UnmarshalBinary(record.Raw); err != nil {
		return err
	}
	copy := *record
	copy.Stage = "may_have_sent"
	if err := store.put(&copy, signer); err != nil {
		return err
	}
	if err := authority.admitEvm(ctx, client, nil); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	if err := client.SendTransaction(ctx, &tx); err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	receipt, err = client.TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		return fleetRecoveryUnresolved(record, err)
	}
	return fleetRecoveryFinishEvm(ctx, store, &copy, signer, authority, client, receipt)
}
