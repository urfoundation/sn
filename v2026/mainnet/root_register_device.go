// Explicit Ledger-profile requests may use an independently selected operator
// device. Portable hardware handoffs use export/import without this adapter.
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

const rootRegisterDeviceStateSchema = "urnetwork-mainnet-root-register-device-custody-v1"
const rootRegisterReplySchema = "urnetwork-mainnet-root-register-signing-reply-v1"

// Reply fields share the public native envelope shape, with a separate domain.
// The verified signature establishes operator bytes, not hardware provenance.
func newRootRegisterSigningReply(request rootRegisterSigningRequest, signature []byte) (ownerSigningReply, error) {
	action := request.Config.Action
	raw, err := action.signed(signature)
	if err != nil {
		return ownerSigningReply{}, err
	}
	return ownerSigningReply{Schema: rootRegisterReplySchema, RequestHash: request.ContentHash, ActionHash: action.RequestHash,
		SignatureScheme: action.SignatureScheme, Owner: action.Policy.Operator, Signature: hex.EncodeToString(signature),
		RawExtrinsic: "0x" + hex.EncodeToString(raw), ExtrinsicHash: rootExtrinsicHash(raw)}, nil
}

// Domain, original request and complete signed envelope must all agree.
func validateRootRegisterSigningReply(request rootRegisterSigningRequest, reply ownerSigningReply) ([]byte, error) {
	signature, err := rootOfflineSignatureBytes(reply.Signature)
	if err != nil {
		return nil, err
	}
	expected, err := newRootRegisterSigningReply(request, signature)
	if err != nil || reply != expected {
		return nil, errors.Join(errors.New("root registration operator reply differs from the original request or exact native envelope"), err)
	}
	return signature, nil
}

// Independent portable pins are checked before any local custody or hardware.
// Embedded deployment paths are only labels used for namespace separation.
func signRootRegisterRequest(ctx context.Context, config ownerSigningDeviceConfig, request rootRegisterSigningRequest, trust ownerSigningTrust, derivationPath string, adapter ownerSigningAdapter) (reply ownerSigningReply, resultErr error) {
	if ctx == nil {
		return reply, errors.New("root registration operator signing requires cancellation context")
	}
	if err := request.validate(trust); err != nil {
		return reply, err
	}
	action := request.Config.Action
	if action.SigningProfile != rootRegisterLedgerHardware {
		return reply, errors.New("root registration sign requires explicit Ledger profile approval; portable operator hardware uses export/import")
	}
	path, err := ownerLedgerDerivationPath(action.DerivationPath)
	if err != nil || derivationPath != action.DerivationPath || action.SignatureScheme != "ed25519" || path[2]&0x80000000 == 0 || path[3] != 0x80000000 || path[4]&0x80000000 == 0 {
		return reply, errors.New("root registration Ledger signing requires Ed25519 and independently pinned m/44'/354'/account'/0'/index' derivation")
	}
	store, err := openOwnerSigningDeviceScope(ctx, config, ownerSigningDeviceScope{Schema: rootRegisterDeviceStateSchema,
		RequestHash: request.ContentHash, HostStatePath: action.StatePath,
		ValidateReply: func(reply ownerSigningReply) error {
			_, err := validateRootRegisterSigningReply(request, reply)
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
			Owner: action.Policy.Operator, Account: path[2] & 0x7fffffff, Index: path[4] & 0x7fffffff,
			Call: action.Call, IncludedExtrinsic: "0x" + hex.EncodeToString(payload[len(call):len(call)+extraLength]),
			IncludedSignedData: "0x" + hex.EncodeToString(payload[len(call)+extraLength:]), ResponsePath: config.StatePath + ".ledger-response", AppVersion: config.AppVersion}
		if err := store.storage.checkWrite(nil); err != nil {
			return reply, err
		}
		prepared, err := adapter(ctx, config, input)
		// This is the reviewed SDK source pin, independent of the approved
		// current runtime source and this command's root_register call.
		if err != nil || prepared.Schema != ownerSigningAdapterSchema || prepared.Mode != "prepare" || prepared.RequestHash != request.ContentHash ||
			prepared.SourceCommit != rootActionV1Source || prepared.MetadataDigest != action.MetadataDigest || !planSha256(prepared.ProofHash) ||
			prepared.PublicKey != "" || prepared.Response != "" || prepared.AppVersion != [3]uint16{} {
			return reply, errors.Join(errors.New("root registration Ledger metadata preparation failed before device access"), err)
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
		return reply, errors.Join(errors.New("root registration device issuance unresolved; retain original custody and response, never reissue"), issueErr, err)
	}
	var response ownerSigningAdapterResult
	if err := decodePlanJson(raw, &response); err != nil {
		return reply, err
	}
	if response.Schema != ownerSigningAdapterSchema || response.Mode != "sign" || response.RequestHash != request.ContentHash || response.SourceCommit != rootActionV1Source ||
		response.MetadataDigest != action.MetadataDigest || response.ProofHash != record.ProofHash || response.PublicKey != action.Policy.Operator || response.AppVersion != config.AppVersion {
		return reply, errors.New("root registration retained device response differs from original request, operator, proof or app")
	}
	encoded, err := rootReceiptHex(response.Response, 65)
	if err != nil || len(encoded) != 65 || encoded[0] != 0 {
		return reply, errors.Join(errors.New("root registration device response requires exactly MultiSignature::Ed25519"), err)
	}
	reply, err = newRootRegisterSigningReply(request, encoded[1:])
	if err != nil {
		return reply, err
	}
	record.Phase, record.Reply = "signed", &reply
	return reply, store.save(record)
}

// Only explicit operator-local inputs are opened. No RPC or deployment journal is
// used, and no key material enters this process or the public reply.
func rootRegisterSignCommand(ctx context.Context, args []string, stderr io.Writer, adapter ownerSigningAdapter) (ownerSigningReply, error) {
	var result ownerSigningReply
	if err := durablepath.Require(ctx); err != nil {
		return result, err
	}
	flags := flag.NewFlagSet("root-register sign", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requestPath := flags.String("request", "", "portable original registration request on the operator's computer")
	trust := ownerSigningTrust{}
	flags.StringVar(&trust.RequestHash, "accept-request-hash", "", "independently reviewed request content hash")
	flags.StringVar(&trust.ApprovalKey, "approval-key", "", "independent original action approval key")
	flags.StringVar(&trust.Owner, "operator-account-id", "", "independent operator AccountId32 distinct from reserve and subnet owner")
	flags.StringVar(&trust.Genesis, "expected-genesis", "", "independently approved native genesis")
	derivationPath := flags.String("operator-derivation-path", "", "independently selected operator derivation matching the approved action")
	config := ownerSigningDeviceConfig{}
	flags.StringVar(&config.StatePath, "operator-state", "", "permanent private operator-local one-request journal")
	flags.StringVar(&config.PythonPath, "ledger-python", "", "absolute reviewed Python executable")
	flags.StringVar(&config.HelperPath, "ledger-helper", "", "reviewed owner_ledger_adapter.py file")
	flags.StringVar(&config.HelperHash, "ledger-helper-sha256", "", "independent helper source pin")
	flags.StringVar(&config.BackendPath, "ledger-backend", "", "reviewed native Ledger SDK artifact")
	flags.StringVar(&config.BackendHash, "ledger-backend-sha256", "", "independent native SDK artifact pin")
	appVersion := flags.String("ledger-app-version", "", "exact approved generic app major.minor.patch")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *requestPath == "" || *derivationPath == "" {
		return result, errors.New("root registration sign requires a portable request, independent operator/action/derivation pins and operator-local Ledger custody")
	}
	parts := strings.Split(*appVersion, ".")
	if len(parts) != 3 {
		return result, errors.New("root registration sign requires canonical ledger-app-version major.minor.patch")
	}
	for i, part := range parts {
		value, err := strconv.ParseUint(part, 10, 16)
		if err != nil || strconv.FormatUint(value, 10) != part {
			return result, errors.New("root registration Ledger app version is noncanonical")
		}
		config.AppVersion[i] = uint16(value)
	}
	for _, suffix := range []string{"", ".lock", ".ledger-response"} {
		if *requestPath == config.StatePath+suffix {
			return result, errors.New("root registration operator state overlaps the portable request")
		}
	}
	raw, _, err := readBootstrapRootFile(ctx, *requestPath, rootRegisterSigningRequestLimit)
	if err != nil {
		return result, err
	}
	var request rootRegisterSigningRequest
	if err := decodePlanJson(raw, &request); err != nil {
		return result, err
	}
	return signRootRegisterRequest(ctx, config, request, trust, *derivationPath, adapter)
}
