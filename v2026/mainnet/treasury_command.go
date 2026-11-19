// Native treasury commands separate unsigned observation/planning, permanent
// host custody, owner-local hardware signing and bounded exact-byte submission.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
)

// Only qualification substitutes the hardware boundary; no production bypass.
func runTreasuryCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runTreasuryCommandWithAdapter(ctx, args, stdout, stderr, nil)
}

// The real dispatcher owns bounded original inputs and emits no inferred grants.
func runTreasuryCommandWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, adapter ownerSigningAdapter) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "treasury requires describe, observe, plan, policy-plan, approval-plan, approval-envelope, reserve, export, inspect-request, ledger-plan, sign, import-reply, status, reconcile or submit")
		return 2
	}
	var value any
	var err error
	switch args[0] {
	case "describe":
		flags := flag.NewFlagSet("treasury describe", flag.ContinueOnError)
		flags.SetOutput(stderr)
		path := flags.String("custody", "", "explicit owner-local public custody YAML")
		pin := flags.String("custody-sha256", "", "independent descriptor file SHA-256")
		destinationPath := flags.String("destination", "", "explicit receive-only public destination YAML")
		destinationHash := flags.String("destination-sha256", "", "independent public destination file SHA-256")
		if parseErr := flags.Parse(args[1:]); parseErr != nil || flags.NArg() != 0 ||
			(*path == "") == (*destinationPath == "") || *path == "" && *pin != "" || *destinationPath == "" && *destinationHash != "" {
			err = errors.New("treasury describe requires exactly one pinned --destination or --custody file")
		} else if *destinationPath != "" {
			value, err = readTreasuryDestination(ctx, *destinationPath, *destinationHash)
		} else {
			value, err = readTreasuryDescriptor(ctx, *path, *pin)
		}
	case "observe", "plan", "policy-plan":
		value, err = treasuryPlanCommand(ctx, args, stderr)
	case "approval-plan":
		value, err = treasuryApprovalPlanCommand(ctx, args, stderr)
	case "approval-envelope":
		value, err = treasuryApprovalEnvelopeCommand(ctx, args, stderr)
	case "inspect-request", "ledger-plan", "sign":
		value, err = treasuryOwnerCommand(ctx, args, stderr, adapter)
	case "reserve", "export", "import-reply", "status", "reconcile", "submit":
		value, err = treasuryCustodyCommand(ctx, args, stderr)
	default:
		err = errors.New("unknown treasury command")
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

// The route, genesis and runtime precede reads; observed values cannot pin self.
func treasuryPlanCommand(ctx context.Context, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("treasury "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("input", "", "public action/observation/metadata/route JSON")
	custodyPath := flags.String("custody", "", "explicit public custody YAML")
	custodyHash := flags.String("custody-sha256", "", "independent custody file hash")
	destinationPath := flags.String("destination", "", "explicit receive-only public destination YAML")
	destinationHash := flags.String("destination-sha256", "", "independent public destination file hash")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" {
		return nil, errors.New("treasury review requires --input FILE")
	}
	raw, _, err := readBootstrapRootFile(ctx, *path, ownerSigningRequestLimit)
	if err != nil {
		return nil, err
	}
	var kind struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(raw, &kind); err != nil {
		return nil, err
	}
	if *destinationPath != "" || *destinationHash != "" || kind.Schema == treasuryDestinationInputSchema {
		if *custodyPath != "" || *custodyHash != "" || args[0] == "plan" {
			return nil, errors.New("treasury receive-only input cannot select signing custody or an executable plan")
		}
		return treasuryDestinationCommand(ctx, args[0], raw, *destinationPath, *destinationHash)
	}
	var input treasuryPlanInput
	if err := decodePlanJson(raw, &input); err != nil {
		return nil, err
	}
	if *custodyPath != "" || *custodyHash != "" {
		descriptor, err := readTreasuryDescriptor(ctx, *custodyPath, *custodyHash)
		if err != nil {
			return nil, err
		}
		if input.Action.Descriptor.Schema != "" && rootObjectHash(input.Action.Descriptor) != rootObjectHash(descriptor) {
			return nil, errors.New("treasury input and independently pinned custody descriptor disagree")
		}
		input.Action.Descriptor = descriptor
	}
	if err := errors.Join(input.Action.Policy.validate(), input.Action.Descriptor.validate()); err != nil {
		return nil, err
	}
	if args[0] == "plan" {
		config, err := prepareTreasuryPlan(ctx, input)
		if err != nil {
			return nil, err
		}
		return struct {
			Config       treasuryConfig `json:"config"`
			SigningBytes string         `json:"approval_signing_bytes"`
		}{Config: config, SigningBytes: "0x" + hex.EncodeToString(config.signingBytes())}, nil
	}
	if args[0] == "policy-plan" {
		metadata, _, err := nativePinnedMetadata(input.Metadata, input.Action.Policy.RuntimeMetadataHash)
		if err != nil {
			return nil, err
		}
		facts, err := input.Observation.facts(ctx, input.Action, metadata)
		if err != nil {
			return nil, err
		}
		policy, err := treasuryPublicPolicy(input.Action.Descriptor, facts)
		if err != nil {
			return nil, err
		}
		hash, err := policy.Hash()
		if err != nil {
			return nil, err
		}
		return struct {
			Policy      any    `json:"treasury_policy"`
			Hash        string `json:"treasury_policy_hash"`
			Observation string `json:"observation_hash"`
			Approved    bool   `json:"approved"`
		}{Policy: policy, Hash: "0x" + hex.EncodeToString(hash[:]), Observation: input.Observation.ContentHash}, nil
	}
	return treasuryObserve(ctx, input.Action.Policy, input.Route, func(ctx context.Context, chain *rootCanonicalChain, hash string, number uint64) (treasuryObservation, error) {
		return chain.treasuryObservationAt(ctx, input.Action, hash, number)
	})
}

// Receiving and signing observations share the same bounded finality closure.
func treasuryObserve(ctx context.Context, policy treasuryChainPolicy, route ownedSubmissionRoute, observe func(context.Context, *rootCanonicalChain, string, uint64) (treasuryObservation, error)) (any, error) {
	if route.ReadRetrySeconds == 0 {
		route.ReadRetrySeconds = 300
	}
	if route.SendTimeoutSeconds == 0 {
		route.SendTimeoutSeconds = 60
	}
	if route.ReadRetrySeconds < 60 || route.ReadRetrySeconds > 900 {
		return nil, errors.New("treasury observation read owner requires 60 through 900 seconds")
	}
	client, err := newOwnedSubmissionClient(route)
	if err != nil {
		return nil, err
	}
	defer client.httpClient.CloseIdleConnections()
	p := policy
	chain, err := newRootCanonicalChain(client, identityExpectation{NativeChain: p.NativeChain, GenesisHash: p.GenesisHash, EvmChainId: p.EvmChainId}, []rootReceiptProfile{p.rootReceiptProfile})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(route.ReadRetrySeconds)*time.Second)
	defer cancel()
	if err := chain.network(ctx); err != nil {
		return nil, err
	}
	point, err := client.readNativeFinality(ctx)
	if err != nil {
		return nil, err
	}
	result, err := observe(ctx, chain, point.Hash, point.Number)
	if err != nil {
		return nil, err
	}
	if err := client.closeNativeFinality(ctx, point, point); err != nil {
		return nil, err
	}
	if err := chain.network(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// Public device settings omit the per-action state path, which is selected
// explicitly on the owner computer and bound by its durable declaration.
type treasuryLedgerConfig struct {
	Schema         string    `json:"schema"`
	AccountId      string    `json:"account_id"`
	DerivationPath string    `json:"derivation_path"`
	PythonPath     string    `json:"python_path"`
	HelperPath     string    `json:"helper_path"`
	HelperHash     string    `json:"helper_sha256"`
	BackendPath    string    `json:"backend_path"`
	BackendHash    string    `json:"backend_sha256"`
	AppVersion     [3]uint16 `json:"app_version"`
}

// Owner-side commands never follow a path supplied only by portable metadata.
func treasuryOwnerCommand(ctx context.Context, args []string, stderr io.Writer, adapter ownerSigningAdapter) (any, error) {
	flags := flag.NewFlagSet("treasury "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "original public signing request")
	trust := ownerSigningTrust{}
	flags.StringVar(&trust.RequestHash, "accept-request-hash", "", "independently accepted original request hash")
	flags.StringVar(&trust.ApprovalKey, "approval-key", "", "independent execution approval key")
	flags.StringVar(&trust.Owner, "signatory-account-id", "", "independently expected Ledger AccountId32")
	flags.StringVar(&trust.Genesis, "expected-genesis", "", "independently expected native genesis")
	proofPath := flags.String("metadata-proof", "", "original shortened RFC78 proof")
	proofHash := flags.String("metadata-proof-sha256", "", "independent proof file SHA-256")
	devicePath := flags.String("device-config", "", "explicit owner-local public Ledger configuration")
	statePath := flags.String("owner-state", "", "permanent original one-request Ledger journal")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" {
		return nil, errors.New("treasury owner command requires original request and independent owner/genesis/approval pins")
	}
	raw, _, err := readBootstrapRootFile(ctx, *path, ownerSigningRequestLimit)
	if err != nil {
		return nil, err
	}
	var request treasurySigningRequest
	if err := decodePlanJson(raw, &request); err != nil {
		return nil, err
	}
	if err := request.validate(trust); err != nil {
		return nil, err
	}
	if args[0] == "inspect-request" {
		return request, nil
	}
	if args[0] == "ledger-plan" {
		proof, actual, err := readBootstrapRootFile(ctx, *proofPath, ownerLedgerPayloadLimit)
		if err != nil || !planSha256(*proofHash) || actual != *proofHash {
			return nil, errors.Join(errors.New("treasury Ledger proof differs from independent pin"), err)
		}
		return request.ledgerTranscript(trust, proof)
	}
	if err := durablepath.Require(ctx); err != nil {
		return nil, err
	}
	var reference treasuryDeviceReference
	for _, signer := range request.Config.Action.Descriptor.Multisig.Signatories {
		if signer.AccountId == trust.Owner {
			reference = signer.DeviceConfig
		}
	}
	if *devicePath == "" || *devicePath != reference.Path || !bootstrapRootAbsolutePath(*statePath) {
		return nil, errors.New("treasury signing requires the descriptor's explicit local device file and original owner state")
	}
	for _, suffix := range []string{"", ".lock", ".ledger-response"} {
		if *path == *statePath+suffix || *devicePath == *statePath+suffix {
			return nil, errors.New("treasury owner custody overlaps a public input")
		}
	}
	deviceRaw, _, err := readBootstrapRootFile(ctx, *devicePath, treasuryDescriptorLimit)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(deviceRaw)
	if uint64(len(deviceRaw)) != reference.Bytes || reference.Sha256 != "0x"+hex.EncodeToString(digest[:]) {
		return nil, errors.New("treasury public device configuration differs from exact approved reference")
	}
	var device treasuryLedgerConfig
	if err := decodePlanJson(deviceRaw, &device); err != nil {
		return nil, err
	}
	if device.Schema != "urnetwork-native-treasury-ledger-device-v1" || device.AccountId != trust.Owner || device.DerivationPath != request.Config.Action.DerivationPath {
		return nil, errors.New("treasury Ledger device account/path differs from original action")
	}
	config := ownerSigningDeviceConfig{StatePath: *statePath, PythonPath: device.PythonPath, HelperPath: device.HelperPath, HelperHash: device.HelperHash, BackendPath: device.BackendPath, BackendHash: device.BackendHash, AppVersion: device.AppVersion}
	return signTreasuryRequest(ctx, config, request, trust, adapter)
}

// Host commands consume public replies only and preserve original durable kind.
func treasuryCustodyCommand(ctx context.Context, args []string, stderr io.Writer) (value any, resultErr error) {
	if err := durablepath.Require(ctx); err != nil {
		return nil, err
	}
	mode := args[0]
	flags := flag.NewFlagSet("treasury "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "independently signed treasury execution config")
	key := flags.String("approval-key", "", "independent approval public key")
	accepted := flags.String("accept-action-hash", "", "independently accepted original action hash")
	authority := flags.String("production-authority-hash", "", "independently accepted production authority hash")
	metadataPath := flags.String("metadata", "", "original metadata14 hex file")
	ledgerPath := flags.String("ledger-metadata", "", "original metadata15 hex file")
	replyPath := flags.String("reply", "", "original public owner-local signing reply")
	replyHash := flags.String("reply-sha256", "", "independent reply file pin")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" || !rootCanonicalHash(*key) || !planSha256(*accepted) || !planSha256(*authority) {
		return nil, errors.New("treasury custody requires original config, approval key, action hash and production authority hash")
	}
	raw, _, err := readBootstrapRootFile(ctx, *path, treasuryDescriptorLimit*2)
	if err != nil {
		return nil, err
	}
	var config treasuryConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return nil, err
	}
	if err := config.validate(*key); err != nil {
		return nil, err
	}
	if config.Action.RequestHash != *accepted || config.AuthorityHash != *authority {
		return nil, errors.New("treasury original action or production authority differs from independent pins")
	}
	for _, input := range []string{*path, *metadataPath, *ledgerPath, *replyPath} {
		if input == config.Action.StatePath || input == config.Action.StatePath+".lock" {
			return nil, errors.New("treasury public input overlaps original custody")
		}
	}
	store, err := openTreasuryStore(config, *key, mode == "reserve", ctx)
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
	custody := treasuryCustody{config: config, key: *key, store: store}
	switch mode {
	case "reserve", "status":
		return custody.load()
	case "export":
		metadata, _, err := readBootstrapRootFile(ctx, *metadataPath, 2*maxMetadataRpcReplyBytes+3)
		if err != nil {
			return nil, err
		}
		ledger, _, err := readBootstrapRootFile(ctx, *ledgerPath, 2*maxMetadataRpcReplyBytes+3)
		if err != nil {
			return nil, err
		}
		return custody.export(strings.TrimSpace(string(metadata)), strings.TrimSpace(string(ledger)))
	case "import-reply":
		reply, err := readOwnerSigningReply(ctx, *replyPath, *replyHash)
		if err != nil {
			return nil, err
		}
		return custody.importReply(reply)
	case "reconcile", "submit":
		chain, err := newTreasuryCanonicalChain(config, *key)
		if err != nil {
			return nil, err
		}
		defer chain.client.httpClient.CloseIdleConnections()
		if mode == "submit" {
			return custody.submit(ctx, chain)
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(config.Route.ReadRetrySeconds)*time.Second)
		defer cancel()
		return custody.reconcile(ctx, chain)
	}
	return nil, errors.New("unsupported treasury custody command")
}
