// Owner-side commands consume only portable public files and independent pins.
// No Snow journal, RPC or native secret is opened. Sign uses the owner-local
// journal and an explicitly pinned SDK adapter for hardware access.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
)

// One bounded read retains exact bytes; no embedded file references are opened.
func readOwnerSigningRequest(ctx context.Context, path string, trust ownerSigningTrust) (ownerSigningRequest, error) {
	var request ownerSigningRequest
	raw, _, err := readBootstrapRootFile(ctx, path, ownerSigningRequestLimit)
	if err != nil {
		return request, err
	}
	if err := decodePlanJson(raw, &request); err != nil {
		return request, err
	}
	return request, request.validate(trust)
}

// A received signature artifact is pinned before parsing, as contract imports are.
func readOwnerSigningReply(ctx context.Context, path, digest string) (ownerSigningReply, error) {
	var reply ownerSigningReply
	raw, actual, err := readBootstrapRootFile(ctx, path, ownerSigningReplyLimit)
	if err != nil || !planSha256(digest) || actual != digest {
		return reply, errors.Join(errors.New("owner signed reply file differs from its exact sha256 pin"), err)
	}
	return reply, decodePlanJson(raw, &reply)
}

// All modes work on the owner's own computer. Sign invokes the pinned SDK;
// ledger-plan only emits an unqualified bounded protocol transcript.
func runOwnerSigningCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runOwnerSigningCommandWithAdapter(ctx, args, stdout, stderr, nil)
}

// Tests replace only the hardware boundary; the public parser, portable trust
// and durable one-request owner remain the actual implementation.
func runOwnerSigningCommandWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, adapter ownerSigningAdapter) int {
	if len(args) == 0 || args[0] != "inspect" && args[0] != "reply" && args[0] != "verify" && args[0] != "ledger-plan" && args[0] != "sign" {
		fmt.Fprintln(stderr, "owner-signing requires inspect, reply, verify, ledger-plan or sign")
		return 2
	}
	mode := args[0]
	if mode == "sign" {
		if err := durablepath.Require(ctx); err != nil {
			fmt.Fprintln(stderr, "owner signing requires approved local durable custody:", err)
			return 2
		}
	}
	flags := flag.NewFlagSet("owner-signing "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	requestPath := flags.String("request", "", "portable exported request on the owner's computer")
	trust := ownerSigningTrust{}
	flags.StringVar(&trust.RequestHash, "accept-request-hash", "", "independently reviewed request content hash")
	flags.StringVar(&trust.ApprovalKey, "trim-approval-key", "", "independent approval public key")
	flags.StringVar(&trust.Owner, "owner-account-id", "", "independent existing subnet-owner AccountId32")
	flags.StringVar(&trust.Genesis, "expected-genesis", "", "independently approved native genesis")
	flags.StringVar(&trust.Signatory, "signatory-account-id", "", "native multisig owner only: independent AccountId32 of this computer's named signatory")
	var signaturePath, ledgerResponsePath, replyPath, replyHash, proofPath, proofHash string
	deviceConfig := ownerSigningDeviceConfig{}
	appVersion := ""
	if mode == "sign" {
		flags.StringVar(&deviceConfig.StatePath, "owner-state", "", "one permanent private journal on the owner's own computer")
		flags.StringVar(&deviceConfig.PythonPath, "ledger-python", "", "absolute reviewed Python 3 executable")
		flags.StringVar(&deviceConfig.HelperPath, "ledger-helper", "", "private owner_ledger_adapter.py file")
		flags.StringVar(&deviceConfig.HelperHash, "ledger-helper-sha256", "", "independent helper source file pin")
		flags.StringVar(&deviceConfig.BackendPath, "ledger-backend", "", "absolute reviewed bittensor_core native extension file")
		flags.StringVar(&deviceConfig.BackendHash, "ledger-backend-sha256", "", "independent native SDK build artifact pin")
		flags.StringVar(&appVersion, "ledger-app-version", "", "exact independently reviewed installed generic app version")
	}
	if mode == "reply" {
		flags.StringVar(&signaturePath, "signature", "", "original public 64-byte native signature as canonical hex")
		flags.StringVar(&ledgerResponsePath, "ledger-response", "", "original 65-byte Ed25519 MultiSignature as canonical hex, without status words")
	}
	if mode == "verify" {
		flags.StringVar(&replyPath, "reply", "", "owner's original public signed reply")
		flags.StringVar(&replyHash, "reply-sha256", "", "exact reply file sha256")
	}
	if mode == "ledger-plan" {
		flags.StringVar(&proofPath, "ledger-metadata", "", "bounded binary shortened RFC78 metadata proof")
		flags.StringVar(&proofHash, "ledger-metadata-sha256", "", "independent exact proof file sha256")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *requestPath == "" || !planSha256(trust.RequestHash) ||
		!rootCanonicalHash(trust.ApprovalKey) || !rootCanonicalHash(trust.Owner) || !rootCanonicalHash(trust.Genesis) || trust.Signatory != "" && !rootCanonicalHash(trust.Signatory) ||
		mode == "reply" && (signaturePath == "") == (ledgerResponsePath == "") ||
		mode == "verify" && (replyPath == "" || !planSha256(replyHash)) ||
		mode == "ledger-plan" && (proofPath == "" || !planSha256(proofHash)) ||
		mode == "sign" && (deviceConfig.StatePath == "" || deviceConfig.PythonPath == "" || deviceConfig.HelperPath == "" || deviceConfig.BackendPath == "" ||
			!planSha256(deviceConfig.HelperHash) || !planSha256(deviceConfig.BackendHash) || appVersion == "") {
		fmt.Fprintln(stderr, "owner-signing requires a portable --request and independent --accept-request-hash, --trim-approval-key, --owner-account-id and --expected-genesis pins")
		return 2
	}
	if mode == "sign" {
		parts := strings.Split(appVersion, ".")
		if len(parts) != 3 {
			fmt.Fprintln(stderr, "owner signing requires an exact canonical ledger-app-version major.minor.patch")
			return 2
		}
		for index, part := range parts {
			value, err := strconv.ParseUint(part, 10, 16)
			if err != nil || strconv.FormatUint(value, 10) != part {
				fmt.Fprintln(stderr, "owner signing app version is noncanonical")
				return 2
			}
			deviceConfig.AppVersion[index] = uint16(value)
		}
		for _, suffix := range []string{"", ".lock", ".ledger-response"} {
			if deviceConfig.StatePath+suffix == *requestPath {
				fmt.Fprintln(stderr, "owner state overlaps the portable request")
				return 2
			}
		}
	}
	request, err := readOwnerSigningRequest(ctx, *requestPath, trust)
	if err != nil {
		fmt.Fprintln(stderr, "owner signing request:", err)
		return 3
	}
	var result any
	switch mode {
	case "inspect":
		// A multisig step displays its exact outer call; the device shows the
		// nested trim call through the same RFC78 proof of that outer call.
		call, gates := "AdminUtils.sudo_trim_to_max_allowed_uids(netuid, max_n)", []string{}
		if multisig := request.Config.Action.Multisig; multisig != nil {
			call = "Multisig.as_multi(threshold, other_signatories, maybe_timepoint, AdminUtils.sudo_trim_to_max_allowed_uids(netuid, max_n), max_weight)"
			if multisig.kind() == "cancel" {
				call = "Multisig.cancel_as_multi(threshold, other_signatories, timepoint, blake2_256(AdminUtils.sudo_trim_to_max_allowed_uids(netuid, max_n)))"
			}
			gates = append(gates, "multisig-"+multisig.kind()+"-step-of-ordered-signatory-custody")
		}
		result = struct {
			RequestHash     string          `json:"request_hash"`
			Action          ownerTrimAction `json:"action"`
			SignatureScheme string          `json:"signature_scheme"`
			SigningBytes    string          `json:"signing_bytes"`
			Call            string          `json:"call"`
			DeviceQualified bool            `json:"device_qualified"`
			Signing         bool            `json:"signing"`
			NetworkEffects  bool            `json:"network_effects"`
			OpenGates       []string        `json:"open_gates"`
		}{RequestHash: request.ContentHash, Action: request.Config.Action, SignatureScheme: request.SignatureScheme,
			SigningBytes: request.SigningBytes, Call: call,
			OpenGates: append([]string{"physical-device-and-sdk-build-qualification", "runtime-rfc78-digest-enabled-in-approved-wasm", "global-owner-nonce-and-custody-fence", "current-owner-trim-enforced-authority"}, gates...)}
	case "sign":
		result, err = signOwnerRequest(ctx, deviceConfig, request, adapter)
		if err != nil {
			fmt.Fprintln(stderr, "owner Ledger signing; preserve original local custody:", err)
			return 3
		}
	case "reply":
		path, maximum := signaturePath, 129
		if ledgerResponsePath != "" {
			path, maximum = ledgerResponsePath, 131
		}
		raw, _, err := readBootstrapRootFile(ctx, path, maximum)
		if err != nil {
			fmt.Fprintln(stderr, "owner public signature:", err)
			return 2
		}
		var signature []byte
		if ledgerResponsePath == "" {
			signature, err = rootOfflineSignatureBytes(strings.TrimSpace(string(raw)))
		} else {
			encoded := strings.TrimSpace(string(raw))
			response, decodeErr := hex.DecodeString(encoded)
			if decodeErr != nil || hex.EncodeToString(response) != encoded {
				err = errors.New("Ledger response requires canonical lowercase hex")
			} else {
				signature, err = ownerLedgerResponse(request, response)
			}
		}
		if err == nil {
			result, err = newOwnerSigningReply(request, signature)
		}
		if err != nil {
			fmt.Fprintln(stderr, "owner signature does not match the approved native action:", err)
			return 3
		}
	case "verify":
		reply, err := readOwnerSigningReply(ctx, replyPath, replyHash)
		if err == nil {
			_, err = reply.validate(request)
		}
		if err != nil {
			fmt.Fprintln(stderr, "owner signed reply:", err)
			return 3
		}
		result = reply
	case "ledger-plan":
		proof, actual, err := readBootstrapRootFile(ctx, proofPath, ownerLedgerPayloadLimit)
		if err != nil || actual != proofHash {
			fmt.Fprintln(stderr, "owner Ledger shortened metadata proof pin differs:", err)
			return 2
		}
		result, err = newOwnerLedgerTranscript(request, proof)
		if err != nil {
			fmt.Fprintln(stderr, "owner Ledger transcript:", err)
			return 3
		}
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "owner signing output:", err)
		return 1
	}
	return 0
}
