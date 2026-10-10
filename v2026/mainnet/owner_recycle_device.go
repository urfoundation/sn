// The recycle owner signs one portable request on their own Ledger computer.
// Shared physical custody retains unknown issuance without another device call.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"strconv"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
)

const ownerRecycleDeviceStateSchema = "urnetwork-mainnet-owner-recycle-device-custody-v1"
const ownerRecycleReplySchema = "urnetwork-mainnet-owner-recycle-signing-reply-v1"

// Reply fields share the public native envelope shape, with a separate domain.
// The verified signature establishes exact owner bytes, not hardware provenance.
func newOwnerRecycleSigningReply(request ownerRecycleSigningRequest, signature []byte) (ownerSigningReply, error) {
	action := request.Config.Action
	raw, err := action.signed(signature)
	if err != nil {
		return ownerSigningReply{}, err
	}
	return ownerSigningReply{Schema: ownerRecycleReplySchema, RequestHash: request.ContentHash, ActionHash: action.RequestHash,
		SignatureScheme: action.SignatureScheme, Owner: action.Owner, Signature: hex.EncodeToString(signature),
		RawExtrinsic: "0x" + hex.EncodeToString(raw), ExtrinsicHash: rootExtrinsicHash(raw)}, nil
}

// Domain, original request and complete signed envelope must all agree.
func validateOwnerRecycleSigningReply(request ownerRecycleSigningRequest, reply ownerSigningReply) ([]byte, error) {
	signature, err := rootOfflineSignatureBytes(reply.Signature)
	if err != nil {
		return nil, err
	}
	expected, err := newOwnerRecycleSigningReply(request, signature)
	if err != nil || reply != expected {
		return nil, errors.Join(errors.New("recycle owner reply differs from the original request or exact native envelope"), err)
	}
	return signature, nil
}

// Independent portable pins are checked before any local custody or hardware.
// Embedded deployment paths are only labels used for namespace separation.
func signOwnerRecycleRequest(ctx context.Context, config ownerSigningDeviceConfig, request ownerRecycleSigningRequest, trust ownerSigningTrust, adapter ownerSigningAdapter) (reply ownerSigningReply, resultErr error) {
	if ctx == nil {
		return reply, errors.New("recycle owner signing requires cancellation context")
	}
	if err := request.validate(trust); err != nil {
		return reply, err
	}
	action := request.Config.Action
	path, err := ownerLedgerDerivationPath(action.DerivationPath)
	if err != nil || action.SignatureScheme != "ed25519" || path[2]&0x80000000 == 0 || path[3] != 0x80000000 || path[4]&0x80000000 == 0 {
		return reply, errors.New("recycle Ledger signing requires Ed25519 and exact m/44'/354'/account'/0'/index' derivation")
	}
	store, err := openOwnerSigningDeviceScope(ctx, config, ownerSigningDeviceScope{Schema: ownerRecycleDeviceStateSchema,
		RequestHash: request.ContentHash, HostStatePath: action.StatePath,
		ValidateReply: func(reply ownerSigningReply) error {
			_, err := validateOwnerRecycleSigningReply(request, reply)
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
			return reply, errors.Join(errors.New("recycle Ledger metadata preparation failed before device access"), err)
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
		return reply, errors.Join(errors.New("recycle device issuance unresolved; retain original custody and response, never reissue"), issueErr, err)
	}
	var response ownerSigningAdapterResult
	if err := decodePlanJson(raw, &response); err != nil {
		return reply, err
	}
	if response.Schema != ownerSigningAdapterSchema || response.Mode != "sign" || response.RequestHash != request.ContentHash || response.SourceCommit != rootActionV1Source ||
		response.MetadataDigest != action.MetadataDigest || response.ProofHash != record.ProofHash || response.PublicKey != action.Owner || response.AppVersion != config.AppVersion {
		return reply, errors.New("recycle retained device response differs from original request, owner, proof or app")
	}
	encoded, err := rootReceiptHex(response.Response, 65)
	if err != nil || len(encoded) != 65 || encoded[0] != 0 {
		return reply, errors.Join(errors.New("recycle device response requires exactly MultiSignature::Ed25519"), err)
	}
	reply, err = newOwnerRecycleSigningReply(request, encoded[1:])
	if err != nil {
		return reply, err
	}
	record.Phase, record.Reply = "signed", &reply
	return reply, store.save(record)
}

// Only explicit owner-local inputs are opened. No RPC or deployment journal is
// used, and no key material enters this process or the public reply.
func ownerRecycleSignCommand(ctx context.Context, args []string, stderr io.Writer, adapter ownerSigningAdapter) (ownerSigningReply, error) {
	var result ownerSigningReply
	if err := durablepath.Require(ctx); err != nil {
		return result, err
	}
	flags := flag.NewFlagSet("owner-recycle sign", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requestPath := flags.String("request", "", "portable original recycle request on the owner's computer")
	trust := ownerSigningTrust{}
	flags.StringVar(&trust.RequestHash, "accept-request-hash", "", "independently reviewed request content hash")
	flags.StringVar(&trust.ApprovalKey, "approval-key", "", "independent original action approval key")
	flags.StringVar(&trust.Owner, "owner-account-id", "", "independent existing owner AccountId32")
	flags.StringVar(&trust.Genesis, "expected-genesis", "", "independently approved native genesis")
	config := ownerSigningDeviceConfig{}
	flags.StringVar(&config.StatePath, "owner-state", "", "permanent private owner-local one-request journal")
	flags.StringVar(&config.PythonPath, "ledger-python", "", "absolute reviewed Python executable")
	flags.StringVar(&config.HelperPath, "ledger-helper", "", "reviewed owner_ledger_adapter.py file")
	flags.StringVar(&config.HelperHash, "ledger-helper-sha256", "", "independent helper source pin")
	flags.StringVar(&config.BackendPath, "ledger-backend", "", "reviewed native Ledger SDK artifact")
	flags.StringVar(&config.BackendHash, "ledger-backend-sha256", "", "independent native SDK artifact pin")
	appVersion := flags.String("ledger-app-version", "", "exact approved generic app major.minor.patch")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *requestPath == "" {
		return result, errors.New("recycle sign requires a portable request, independent owner/action pins and owner-local Ledger custody")
	}
	parts := strings.Split(*appVersion, ".")
	if len(parts) != 3 {
		return result, errors.New("recycle sign requires canonical ledger-app-version major.minor.patch")
	}
	for i, part := range parts {
		value, err := strconv.ParseUint(part, 10, 16)
		if err != nil || strconv.FormatUint(value, 10) != part {
			return result, errors.New("recycle Ledger app version is noncanonical")
		}
		config.AppVersion[i] = uint16(value)
	}
	for _, suffix := range []string{"", ".lock", ".ledger-response"} {
		if *requestPath == config.StatePath+suffix {
			return result, errors.New("recycle owner state overlaps the portable request")
		}
	}
	raw, _, err := readBootstrapRootFile(ctx, *requestPath, ownerSigningRequestLimit)
	if err != nil {
		return result, err
	}
	var request ownerRecycleSigningRequest
	if err := decodePlanJson(raw, &request); err != nil {
		return result, err
	}
	return signOwnerRecycleRequest(ctx, config, request, trust, adapter)
}
