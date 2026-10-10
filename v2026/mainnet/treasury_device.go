// The treasury owner signs one portable request on their own Ledger computer.
// Shared physical custody retains unknown issuance without another device call.
package main

import (
	"context"
	"encoding/hex"
	"errors"
)

const treasuryDeviceStateSchema = "urnetwork-mainnet-native-treasury-device-custody-v1"
const treasuryReplySchema = "urnetwork-mainnet-native-treasury-signing-reply-v1"

// Reply fields share the public native envelope shape, with a separate domain.
// The verified signature establishes exact owner bytes, not hardware provenance.
func newTreasurySigningReply(request treasurySigningRequest, signature []byte) (ownerSigningReply, error) {
	action := request.Config.Action
	raw, err := action.signed(signature)
	if err != nil {
		return ownerSigningReply{}, err
	}
	return ownerSigningReply{Schema: treasuryReplySchema, RequestHash: request.ContentHash, ActionHash: action.RequestHash,
		SignatureScheme: action.SignatureScheme, Owner: action.Owner, Signature: hex.EncodeToString(signature),
		RawExtrinsic: "0x" + hex.EncodeToString(raw), ExtrinsicHash: rootExtrinsicHash(raw)}, nil
}

// Domain, original request and complete signed envelope must all agree.
func validateTreasurySigningReply(request treasurySigningRequest, reply ownerSigningReply) ([]byte, error) {
	signature, err := rootOfflineSignatureBytes(reply.Signature)
	if err != nil {
		return nil, err
	}
	expected, err := newTreasurySigningReply(request, signature)
	if err != nil || reply != expected {
		return nil, errors.Join(errors.New("treasury owner reply differs from the original request or exact native envelope"), err)
	}
	return signature, nil
}

// Independent portable pins are checked before any local custody or hardware.
// Embedded deployment paths are only labels used for namespace separation.
func signTreasuryRequest(ctx context.Context, config ownerSigningDeviceConfig, request treasurySigningRequest, trust ownerSigningTrust, adapter ownerSigningAdapter) (reply ownerSigningReply, resultErr error) {
	if ctx == nil {
		return reply, errors.New("treasury owner signing requires cancellation context")
	}
	if err := request.validate(trust); err != nil {
		return reply, err
	}
	action := request.Config.Action
	path, err := ownerLedgerDerivationPath(action.DerivationPath)
	if err != nil || action.SignatureScheme != "ed25519" || path[2]&0x80000000 == 0 || path[3] != 0x80000000 || path[4]&0x80000000 == 0 {
		return reply, errors.New("treasury Ledger signing requires Ed25519 and exact m/44'/354'/account'/0'/index' derivation")
	}
	store, err := openOwnerSigningDeviceScope(ctx, config, ownerSigningDeviceScope{Schema: treasuryDeviceStateSchema,
		RequestHash: request.ContentHash, HostStatePath: action.StatePath,
		ValidateReply: func(reply ownerSigningReply) error {
			_, err := validateTreasurySigningReply(request, reply)
			return err
		}})
	if err != nil {
		return reply, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.close()) }()
	record, err := store.load()
	if err != nil {
		return reply, err
	}
	if record.Phase == "signed" {
		return *record.Reply, nil
	}
	if adapter == nil {
		adapter = runOwnerLedgerAdapter
	}
	var issueErr error
	if record.Phase == "reserved" {
		call, _ := hex.DecodeString(action.Call[2:])
		payload, _ := hex.DecodeString(action.Payload[2:])
		extraLength := 2 + len(rootCompact(uint64(action.Nonce))) + 2
		input := ownerSigningAdapterInput{Schema: ownerSigningAdapterSchema, Mode: "prepare", RequestHash: request.ContentHash,
			BackendPath: config.BackendPath, BackendHash: config.BackendHash, MetadataHex: request.LedgerMetadata,
			MetadataDigest: action.MetadataDigest, SpecName: action.Policy.RuntimeVersion.SpecName, SpecVersion: action.Policy.RuntimeVersion.SpecVersion,
			Owner: action.Owner, Account: path[2] & 0x7fffffff, Index: path[4] & 0x7fffffff,
			Call: action.Call, IncludedExtrinsic: "0x" + hex.EncodeToString(payload[len(call):len(call)+extraLength]),
			IncludedSignedData: "0x" + hex.EncodeToString(payload[len(call)+extraLength:]), ResponsePath: config.StatePath + ".ledger-response", AppVersion: config.AppVersion}
		if err := store.storage.checkWrite(nil); err != nil {
			return reply, err
		}
		prepared, err := adapter(ctx, config, input)
		if err != nil || prepared.Schema != ownerSigningAdapterSchema || prepared.Mode != "prepare" || prepared.RequestHash != request.ContentHash ||
			prepared.SourceCommit != rootActionV1Source || prepared.MetadataDigest != action.MetadataDigest || !planSha256(prepared.ProofHash) ||
			prepared.PublicKey != "" || prepared.Response != "" || prepared.AppVersion != [3]uint16{} {
			return reply, errors.Join(errors.New("treasury Ledger metadata preparation failed before device access"), err)
		}
		record.Phase, record.ProofHash = "signing", prepared.ProofHash
		if err := store.save(record); err != nil {
			return reply, err
		}
		input.Mode, input.ProofHash = "sign", prepared.ProofHash
		if err := store.storage.checkWrite(nil); err != nil {
			return reply, err
		}
		_, issueErr = adapter(ctx, config, input)
	}
	raw, _, err := store.storage.readAuxiliaryFile(context.Background(), config.StatePath+".ledger-response", ownerSigningReplyLimit)
	if err != nil {
		return reply, errors.Join(errors.New("treasury device issuance unresolved; retain original custody and response, never reissue"), issueErr, err)
	}
	var response ownerSigningAdapterResult
	if err := decodePlanJson(raw, &response); err != nil {
		return reply, err
	}
	if response.Schema != ownerSigningAdapterSchema || response.Mode != "sign" || response.RequestHash != request.ContentHash || response.SourceCommit != rootActionV1Source ||
		response.MetadataDigest != action.MetadataDigest || response.ProofHash != record.ProofHash || response.PublicKey != action.Owner || response.AppVersion != config.AppVersion {
		return reply, errors.New("treasury retained device response differs from original request, owner, proof or app")
	}
	encoded, err := rootReceiptHex(response.Response, 65)
	if err != nil || len(encoded) != 65 || encoded[0] != 0 {
		return reply, errors.Join(errors.New("treasury device response requires exactly MultiSignature::Ed25519"), err)
	}
	reply, err = newTreasurySigningReply(request, encoded[1:])
	if err != nil {
		return reply, err
	}
	record.Phase, record.Reply = "signed", &reply
	return reply, store.save(record)
}
