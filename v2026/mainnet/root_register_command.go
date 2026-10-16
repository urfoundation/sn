// Root registration uses a portable operator hardware handoff and isolated host
// custody. Optional Ledger signing and submission require separate explicit pins.
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
func runRootRegisterCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runRootRegisterCommandWithAdapter(ctx, args, stdout, stderr, nil)
}

// Only tests substitute the hardware boundary; no public bypass flag exists.
func runRootRegisterCommandWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, adapter ownerSigningAdapter) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "root-register requires observe, plan, reserve, export, inspect-request, ledger-plan, sign, import, import-reply, status, reconcile, submit-plan, submit or bootstrap-handoff")
		return 2
	}
	var value any
	var err error
	switch args[0] {
	case "observe":
		value, err = rootRegisterObserveCommand(ctx, args[1:], stderr)
	case "plan":
		flags := flag.NewFlagSet("root-register plan", flag.ContinueOnError)
		flags.SetOutput(stderr)
		inputPath := flags.String("input", "", "private JSON with reviewed action, birth observation, metadata and route")
		if parseErr := flags.Parse(args[1:]); parseErr != nil || flags.NArg() != 0 || *inputPath == "" {
			err = errors.New("root registration plan requires --input FILE")
		} else {
			var raw []byte
			raw, _, err = readBootstrapRootFile(ctx, *inputPath, rootRegisterSigningRequestLimit)
			var input rootRegisterPlanInput
			if err == nil {
				err = decodePlanJson(raw, &input)
			}
			if err == nil {
				value, err = prepareRootRegisterPlan(input)
			}
		}
	case "inspect-request", "ledger-plan":
		value, err = rootRegisterRequestCommand(ctx, args, stderr)
	case "sign":
		value, err = rootRegisterSignCommand(ctx, args[1:], stderr, adapter)
	case "bootstrap-handoff":
		value, err = rootRegisterBootstrapCommand(ctx, args[1:], stderr)
	case "reserve", "export", "import", "import-reply", "status", "reconcile", "submit-plan", "submit":
		value, err = rootRegisterCustodyCommand(ctx, args, stderr)
	default:
		err = errors.New("unknown root-register command")
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

// The explicit policy/operator predate the RPC response; observed pins are not
// automatically approved. Every storage read uses one authenticated header hash.
func rootRegisterObserveCommand(ctx context.Context, args []string, stderr io.Writer) (rootRegisterObservation, error) {
	var result rootRegisterObservation
	flags := flag.NewFlagSet("root-register observe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "independently reviewed root registration policy JSON")
	operator := flags.String("operator-account-id", "", "independently pinned operator AccountId32")
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC")
	retry := flags.Duration("retry-window", 60*time.Second, "bounded read window, 60s through 15m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || ctx == nil || *policyPath == "" || !rootCanonicalHash(*operator) || *rpcUrl == "" || *retry < 60*time.Second || *retry > 15*time.Minute {
		return result, errors.New("root registration observe requires --policy FILE --operator-account-id HEX --rpc URL")
	}
	var policy rootRegisterPolicy
	raw, _, err := readBootstrapRootFile(ctx, *policyPath, 32*1024)
	if err != nil {
		return result, err
	}
	if err := decodePlanJson(raw, &policy); err != nil {
		return result, err
	}
	if err := policy.validate(); err != nil {
		return result, err
	}
	if policy.Operator != *operator {
		return result, errors.New("root registration policy differs from the independent operator pin")
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
	result, err = native.rootRegisterObservationAt(operationCtx, policy, hash, number)
	if err != nil {
		return rootRegisterObservation{}, err
	}
	// A lagging tag with unchanged original canonical history is unavailable,
	// not a reorg. The shared reader retries within this same operation budget.
	if _, err := client.readNativeFinalityCovering(operationCtx, nativeFinalityPoint{Number: number, Hash: hash}); err != nil {
		return rootRegisterObservation{}, err
	}
	if err := native.network(operationCtx); err != nil {
		return rootRegisterObservation{}, err
	}
	if err := operationCtx.Err(); err != nil {
		return rootRegisterObservation{}, err
	}
	return result, nil
}

// Operator inspection opens only explicit local paths and has no host-custody
// or device effects. A Ledger transcript requires an independently pinned proof.
func rootRegisterRequestCommand(ctx context.Context, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("root-register "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "portable request file")
	trust := ownerSigningTrust{}
	flags.StringVar(&trust.RequestHash, "accept-request-hash", "", "independently pinned request hash")
	flags.StringVar(&trust.ApprovalKey, "approval-key", "", "independent root registration approval public key")
	flags.StringVar(&trust.Owner, "operator-account-id", "", "independently pinned operator AccountId32")
	flags.StringVar(&trust.Genesis, "expected-genesis", "", "independently pinned genesis")
	var proofPath, proofHash string
	if args[0] == "ledger-plan" {
		flags.StringVar(&proofPath, "metadata-proof", "", "shortened RFC78 proof file")
		flags.StringVar(&proofHash, "metadata-proof-sha256", "", "independently pinned proof file hash")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" {
		return nil, errors.New("root registration request command requires explicit request and independent trust pins")
	}
	raw, _, err := readBootstrapRootFile(ctx, *path, rootRegisterSigningRequestLimit)
	if err != nil {
		return nil, err
	}
	var request rootRegisterSigningRequest
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
		return nil, errors.Join(errors.New("root registration shortened metadata proof differs from independent pin"), err)
	}
	return request.ledgerTranscript(trust, proof)
}

// Host custody imports public bytes only. Submission additionally requires a
// separately signed production authority policy and never invokes a signer.
func rootRegisterCustodyCommand(ctx context.Context, args []string, stderr io.Writer) (value any, resultErr error) {
	mode := args[0]
	flags := flag.NewFlagSet("root-register "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "independently approved root registration execution config")
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
		flags.BoolVar(&ledgerResponse, "ledger-response", false, "explicit Ledger profile only: exactly 00 plus64 Ed25519 bytes, excluding status words")
	}
	if mode == "import-reply" {
		flags.StringVar(&replyPath, "reply", "", "original operator-local public root registration reply")
		flags.StringVar(&replyHash, "reply-sha256", "", "exact original public reply file pin")
		flags.StringVar(&requestHash, "accept-request-hash", "", "original retained exported request hash")
	}
	if mode == "submit-plan" || mode == "submit" {
		flags.StringVar(&authorityHash, "production-authority-hash", "", "independently reviewed runtime, finality and global operator custody authority")
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
		return nil, errors.New("root registration custody requires --config FILE --approval-key HEX --accept-action-hash HASH")
	}
	if mode == "import" && (signaturePath == "" || !planSha256(signatureHash) || !planSha256(requestHash)) ||
		mode == "import-reply" && (replyPath == "" || !planSha256(replyHash) || !planSha256(requestHash)) ||
		(mode == "submit-plan" || mode == "submit") && !planSha256(authorityHash) ||
		mode == "submit-plan" && (maximumPosts == 0 || maximumPosts > 8) ||
		mode == "submit" && (submissionPath == "" || !planSha256(submissionHash) || !rootCanonicalHash(submissionKey)) {
		return nil, errors.New("root registration signature, operator reply and submission modes require their separate independent pins and bounds")
	}
	raw, _, err := readBootstrapRootFile(ctx, *configPath, 128*1024)
	if err != nil {
		return nil, err
	}
	var config rootRegisterConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return nil, err
	}
	if err := config.validate(*key); err != nil {
		return nil, err
	}
	if config.Action.RequestHash != *accepted {
		return nil, errors.New("root registration action differs from independently accepted hash")
	}
	for _, path := range []string{*configPath, metadataPath, ledgerPath, signaturePath, replyPath, submissionPath} {
		if path == config.Action.StatePath || path == config.Action.StatePath+".lock" {
			return nil, errors.New("root registration input overlaps custody journal")
		}
	}
	store, err := openRootRegisterStore(config, *key, mode == "reserve", ctx)
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
	custody := rootRegisterCustody{config: config, key: *key, store: store}
	switch mode {
	case "reserve", "status":
		return custody.load()
	case "import-reply":
		record, err := custody.load()
		if err != nil || record.Request == nil || record.Request.ContentHash != requestHash {
			return nil, errors.Join(errors.New("root registration operator reply lacks original exported custody"), err)
		}
		reply, err := readOwnerSigningReply(ctx, replyPath, replyHash)
		if err != nil {
			return nil, err
		}
		signature, err := validateRootRegisterSigningReply(*record.Request, reply)
		if err != nil {
			return nil, err
		}
		return custody.importSignature(requestHash, signature)
	case "submit-plan":
		record, err := custody.load()
		if err != nil {
			return nil, err
		}
		approval, err := rootRegisterSubmissionTemplate(record)
		if err != nil {
			return nil, err
		}
		approval.AuthorityHash, approval.MaximumAttempts = authorityHash, uint8(maximumPosts)
		return struct {
			Approval     rootRegisterSubmissionApproval `json:"approval_template"`
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
			return nil, errors.Join(errors.New("root registration signature file differs from pinned hash"), err)
		}
		signature, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(encoded)), "0x"))
		if err != nil {
			return nil, err
		}
		if ledgerResponse {
			if config.Action.SigningProfile != rootRegisterLedgerHardware || config.Action.SignatureScheme != "ed25519" || len(signature) != 65 || signature[0] != 0 {
				return nil, errors.New("root registration Ledger response is not exactly MultiSignature::Ed25519")
			}
			signature = signature[1:]
		}
		return custody.importSignature(requestHash, signature)
	case "reconcile", "submit":
		var approval rootRegisterSubmissionApproval
		if mode == "submit" {
			raw, actual, err := readBootstrapRootFile(ctx, submissionPath, 32*1024)
			if err != nil || actual != submissionHash {
				return nil, errors.Join(errors.New("root registration submission policy file differs from independent pin"), err)
			}
			if err := decodePlanJson(raw, &approval); err != nil {
				return nil, err
			}
			if approval.AuthorityHash != authorityHash {
				return nil, errors.New("root registration submission policy differs from independently pinned production authority")
			}
		}
		chain, err := newRootRegisterCanonicalChain(config, *key)
		if err != nil {
			return nil, err
		}
		defer chain.client.httpClient.CloseIdleConnections()
		if mode == "submit" {
			return custody.submit(ctx, chain, approval, submissionKey)
		}
		return custody.reconcile(ctx, chain)
	}
	return nil, errors.New("unknown root registration custody operation")
}
