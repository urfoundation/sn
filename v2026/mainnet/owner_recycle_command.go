// Recycle commands separate portable owner-local Ledger signing from host
// custody and independently approved bounded submission of original bytes.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

// Public dispatcher keeps reads separate from offline custody operations.
func runOwnerRecycleCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runOwnerRecycleCommandWithAdapter(ctx, args, stdout, stderr, nil)
}

// Only tests substitute the hardware boundary; no public bypass flag exists.
func runOwnerRecycleCommandWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, adapter ownerSigningAdapter) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "owner-recycle requires observe, plan, reserve, export, inspect-request, ledger-plan, sign, import, import-reply, status, reconcile, submit-plan or submit")
		return 2
	}
	var value any
	var err error
	switch args[0] {
	case "observe":
		value, err = ownerRecycleObserveCommand(ctx, args[1:], stderr)
	case "plan":
		flags := flag.NewFlagSet("owner-recycle plan", flag.ContinueOnError)
		flags.SetOutput(stderr)
		inputPath := flags.String("input", "", "private JSON with reviewed action, birth observation, metadata and route")
		if parseErr := flags.Parse(args[1:]); parseErr != nil || flags.NArg() != 0 || *inputPath == "" {
			err = errors.New("recycle plan requires --input FILE")
		} else {
			var raw []byte
			raw, _, err = readBootstrapRootFile(ctx, *inputPath, ownerSigningRequestLimit)
			var input ownerRecyclePlanInput
			if err == nil {
				err = decodePlanJson(raw, &input)
			}
			if err == nil {
				value, err = prepareOwnerRecyclePlan(input)
			}
		}
	case "inspect-request", "ledger-plan":
		value, err = ownerRecycleRequestCommand(ctx, args, stderr)
	case "sign":
		value, err = ownerRecycleSignCommand(ctx, args[1:], stderr, adapter)
	case "reserve", "export", "import", "import-reply", "status", "reconcile", "submit-plan", "submit":
		value, err = ownerRecycleCustodyCommand(ctx, args, stderr)
	default:
		err = errors.New("unknown owner-recycle command")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// The explicit policy/owner predate the RPC response; observed pins are not
// automatically approved. Every storage read uses one authenticated header hash.
func ownerRecycleObserveCommand(ctx context.Context, args []string, stderr io.Writer) (ownerRecycleObservation, error) {
	var result ownerRecycleObservation
	flags := flag.NewFlagSet("owner-recycle observe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "independently reviewed recycle policy JSON")
	owner := flags.String("owner-account-id", "", "independently pinned raw AccountId32")
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC")
	retry := flags.Duration("retry-window", 60*time.Second, "bounded read window")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *policyPath == "" || !rootCanonicalHash(*owner) || *rpcUrl == "" {
		return result, errors.New("recycle observe requires --policy FILE --owner-account-id HEX --rpc URL")
	}
	var policy recyclePolicy
	if _, err := readEconomicInput(*policyPath, &policy); err != nil {
		return result, err
	}
	if err := policy.validate(); err != nil {
		return result, err
	}
	client, err := newRpcClient(*rpcUrl, *retry)
	if err != nil {
		return result, err
	}
	defer client.httpClient.CloseIdleConnections()
	profile := rootReceiptProfile{RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion, RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash}
	native, err := newRootCanonicalChain(client, identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}, []rootReceiptProfile{profile})
	if err != nil {
		return result, err
	}
	operationCtx, cancel := context.WithTimeout(ctx, *retry)
	defer cancel()
	if err := native.network(operationCtx); err != nil {
		return result, err
	}
	var hash string
	if err := client.call(operationCtx, "chain_getFinalizedHead", []any{}, &hash); err != nil {
		return result, err
	}
	_, number, err := native.header(operationCtx, hash)
	if err != nil {
		return result, err
	}
	result, err = native.recycleObservationAt(operationCtx, policy, *owner, hash, number)
	if err != nil {
		return ownerRecycleObservation{}, err
	}
	var canonical string
	if err := client.call(operationCtx, "chain_getBlockHash", []any{number}, &canonical); err != nil {
		return ownerRecycleObservation{}, err
	}
	if canonical != hash {
		return ownerRecycleObservation{}, errors.New("recycle finalized mapping changed during observation")
	}
	if err := native.network(operationCtx); err != nil {
		return ownerRecycleObservation{}, err
	}
	return result, nil
}

// Owner-side inspection opens only explicit local paths and has no host-custody
// or device effects. A Ledger transcript requires an independently pinned proof.
func ownerRecycleRequestCommand(ctx context.Context, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("owner-recycle "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "portable request file")
	trust := ownerSigningTrust{}
	flags.StringVar(&trust.RequestHash, "accept-request-hash", "", "independently pinned request hash")
	flags.StringVar(&trust.ApprovalKey, "approval-key", "", "independent recycle approval public key")
	flags.StringVar(&trust.Owner, "owner-account-id", "", "independently pinned owner AccountId32")
	flags.StringVar(&trust.Genesis, "expected-genesis", "", "independently pinned genesis")
	var proofPath, proofHash string
	if args[0] == "ledger-plan" {
		flags.StringVar(&proofPath, "metadata-proof", "", "shortened RFC78 proof file")
		flags.StringVar(&proofHash, "metadata-proof-sha256", "", "independently pinned proof file hash")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" {
		return nil, errors.New("recycle request command requires explicit request and independent trust pins")
	}
	raw, _, err := readBootstrapRootFile(ctx, *path, ownerSigningRequestLimit)
	if err != nil {
		return nil, err
	}
	var request ownerRecycleSigningRequest
	if err := decodePlanJson(raw, &request); err != nil {
		return nil, err
	}
	if err := request.validate(trust); err != nil {
		return nil, err
	}
	if args[0] == "inspect-request" {
		return request, nil
	}
	proof, actual, err := readBootstrapRootFile(ctx, proofPath, ownerLedgerPayloadLimit)
	if err != nil || !planSha256(proofHash) || actual != proofHash {
		return nil, errors.Join(errors.New("recycle shortened metadata proof differs from independent pin"), err)
	}
	return request.ledgerTranscript(trust, proof)
}

// Host custody imports public bytes only. Submission additionally requires a
// separately signed production authority policy and never invokes a signer.
func ownerRecycleCustodyCommand(ctx context.Context, args []string, stderr io.Writer) (value any, resultErr error) {
	mode := args[0]
	flags := flag.NewFlagSet("owner-recycle "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "independently approved recycle execution config")
	key := flags.String("approval-key", "", "independent approval public key")
	accepted := flags.String("accept-action-hash", "", "independently accepted action request hash")
	var metadataPath, ledgerPath, signaturePath, signatureHash, requestHash string
	var replyPath, replyHash, submissionPath, submissionHash, submissionKey, authorityHash string
	maximumPosts := uint(1)
	var ledgerResponse bool
	if mode == "export" {
		flags.StringVar(&metadataPath, "metadata", "", "exact metadata14 hex file")
		flags.StringVar(&ledgerPath, "ledger-metadata", "", "exact metadata15 hex file for Ledger profile")
	}
	if mode == "import" {
		flags.StringVar(&signaturePath, "signature", "", "original public signature hex file")
		flags.StringVar(&signatureHash, "signature-file-sha256", "", "independently pinned signature file hash")
		flags.StringVar(&requestHash, "accept-request-hash", "", "original exported request hash")
		flags.BoolVar(&ledgerResponse, "ledger-response", false, "require exactly 00 plus64 Ed25519 bytes, excluding status words")
	}
	if mode == "import-reply" {
		flags.StringVar(&replyPath, "reply", "", "original owner-local public recycle reply")
		flags.StringVar(&replyHash, "reply-sha256", "", "exact original public reply file pin")
		flags.StringVar(&requestHash, "accept-request-hash", "", "original retained exported request hash")
	}
	if mode == "submit-plan" || mode == "submit" {
		flags.StringVar(&authorityHash, "production-authority-hash", "", "independently reviewed production runtime, release, finality and global owner custody authority")
	}
	if mode == "submit-plan" {
		flags.UintVar(&maximumPosts, "maximum-posts", 1, "unsigned finite cumulative post allowance, 1 through 8")
	}
	if mode == "submit" {
		flags.StringVar(&submissionPath, "submission-policy", "", "separate independently signed original-transaction submission approval")
		flags.StringVar(&submissionHash, "submission-policy-sha256", "", "exact signed submission file pin")
		flags.StringVar(&submissionKey, "submission-approval-key", "", "independently supplied submission approval key")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *configPath == "" || !rootCanonicalHash(*key) || !planSha256(*accepted) {
		return nil, errors.New("recycle custody requires --config FILE --approval-key HEX --accept-action-hash HASH")
	}
	if mode == "import-reply" && (replyPath == "" || !planSha256(replyHash) || !planSha256(requestHash)) ||
		(mode == "submit-plan" || mode == "submit") && !planSha256(authorityHash) ||
		mode == "submit-plan" && (maximumPosts == 0 || maximumPosts > 8) ||
		mode == "submit" && (submissionPath == "" || !planSha256(submissionHash) || !rootCanonicalHash(submissionKey)) {
		return nil, errors.New("recycle owner reply and submission modes require their separate exact independent pins and bounds")
	}
	raw, _, err := readBootstrapRootFile(ctx, *configPath, 128*1024)
	if err != nil {
		return nil, err
	}
	var config ownerRecycleConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return nil, err
	}
	if err := config.validate(*key); err != nil {
		return nil, err
	}
	if config.Action.RequestHash != *accepted {
		return nil, errors.New("recycle action differs from independently accepted hash")
	}
	for _, path := range []string{*configPath, metadataPath, ledgerPath, signaturePath, replyPath, submissionPath} {
		if path == config.Action.StatePath || path == config.Action.StatePath+".lock" {
			return nil, errors.New("recycle input overlaps custody journal")
		}
	}
	store, err := openOwnerRecycleStore(config, *key, mode == "reserve", ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr == nil {
			_, resultErr = store.load()
		}
		resultErr = errors.Join(resultErr, store.close())
		if resultErr != nil {
			value = nil
		}
	}()
	custody := ownerRecycleCustody{config: config, key: *key, store: store}
	switch mode {
	case "reserve", "status":
		return custody.load()
	case "import-reply":
		record, err := custody.load()
		if err != nil || record.Request == nil || record.Request.ContentHash != requestHash {
			return nil, errors.Join(errors.New("recycle owner reply lacks original exported custody"), err)
		}
		reply, err := readOwnerSigningReply(ctx, replyPath, replyHash)
		if err != nil {
			return nil, err
		}
		signature, err := validateOwnerRecycleSigningReply(*record.Request, reply)
		if err != nil {
			return nil, err
		}
		return custody.importSignature(requestHash, signature)
	case "submit-plan":
		record, err := custody.load()
		if err != nil {
			return nil, err
		}
		approval, err := ownerRecycleSubmissionTemplate(record)
		if err != nil {
			return nil, err
		}
		approval.AuthorityHash, approval.MaximumAttempts = authorityHash, uint8(maximumPosts)
		return struct {
			Approval     ownerRecycleSubmissionApproval `json:"approval_template"`
			SigningBytes string                         `json:"signing_bytes"`
		}{Approval: approval, SigningBytes: "0x" + hex.EncodeToString(approval.signingBytes())}, nil
	case "export":
		metadata, _, err := readBootstrapRootFile(ctx, metadataPath, 2*maxMetadataRpcReplyBytes+3)
		if err != nil {
			return nil, err
		}
		var ledger []byte
		if ledgerPath != "" {
			ledger, _, err = readBootstrapRootFile(ctx, ledgerPath, 2*maxMetadataRpcReplyBytes+3)
			if err != nil {
				return nil, err
			}
		}
		return custody.export(strings.TrimSpace(string(metadata)), strings.TrimSpace(string(ledger)))
	case "import":
		encoded, actual, err := readBootstrapRootFile(ctx, signaturePath, 256)
		if err != nil || !planSha256(signatureHash) || actual != signatureHash {
			return nil, errors.Join(errors.New("recycle signature file differs from pinned hash"), err)
		}
		signature, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(encoded)), "0x"))
		if err != nil {
			return nil, err
		}
		if ledgerResponse {
			if config.Action.SignatureScheme != "ed25519" || len(signature) != 65 || signature[0] != 0 {
				return nil, errors.New("recycle Ledger response is not exactly MultiSignature::Ed25519")
			}
			signature = signature[1:]
		}
		return custody.importSignature(requestHash, signature)
	case "reconcile", "submit":
		var approval ownerRecycleSubmissionApproval
		if mode == "submit" {
			raw, actual, err := readBootstrapRootFile(ctx, submissionPath, 32*1024)
			if err != nil || actual != submissionHash {
				return nil, errors.Join(errors.New("recycle submission policy file differs from independent pin"), err)
			}
			if err := decodePlanJson(raw, &approval); err != nil {
				return nil, err
			}
			if approval.AuthorityHash != authorityHash {
				return nil, errors.New("recycle submission policy differs from independently pinned production authority")
			}
		}
		chain, err := newOwnerRecycleCanonicalChain(config, *key)
		if err != nil {
			return nil, err
		}
		defer chain.client.httpClient.CloseIdleConnections()
		if mode == "submit" {
			return custody.submit(ctx, chain, approval, submissionKey)
		}
		return custody.reconcile(ctx, chain)
	}
	return nil, errors.New("unknown recycle custody operation")
}
