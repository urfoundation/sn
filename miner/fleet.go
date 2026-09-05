package miner

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/miner/onchain"
	"github.com/urfoundation/sn/protocol"
)

func fleetOpt(opts docopt.Opts, name string) string {
	v, _ := opts.String(name)
	return strings.TrimSpace(v)
}

func fleetOpts(opts docopt.Opts, name string) []string {
	if value := opts[name]; value != nil {
		if out, ok := value.([]string); ok {
			return out
		}
	}
	return nil
}

func loadFleetManifest(path string) (*protocol.FleetManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return protocol.ParseFleetManifest(b)
}

func parseClientID16(value string) ([16]byte, error) {
	var out [16]byte
	b, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(b) != len(out) {
		return out, errors.New("--client_id must be a 16-byte hex value")
	}
	copy(out[:], b)
	return out, nil
}

func parseFleetEpoch(opts docopt.Opts, name string) (uint64, error) {
	value := fleetOpt(opts, name)
	epoch, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return epoch, nil
}

func fleetMember(manifest *protocol.FleetManifest, clientID [16]byte) (protocol.FleetMember, error) {
	for _, member := range manifest.Members {
		if member.ClientID == clientID {
			return member, nil
		}
	}
	return protocol.FleetMember{}, fmt.Errorf("client_id 0x%x is not in the manifest", clientID)
}

func loadEd25519Seed(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.SeedSize {
		trimmed := strings.TrimSpace(string(raw))
		decoded, decodeErr := hex.DecodeString(strings.TrimPrefix(trimmed, "0x"))
		if decodeErr != nil || len(decoded) != ed25519.SeedSize {
			return nil, fmt.Errorf("%s: expected raw or hex 32-byte Ed25519 seed", path)
		}
		raw = decoded
	}
	return ed25519.NewKeyFromSeed(raw), nil
}

// dialFleetNative preserves the internal compatibility surface for callers
// without an operation context. Each endpoint remains hard-bounded.
func dialFleetNative(urls []string) (*crv4.Chain, string, error) {
	return dialFleetNativeContext(context.Background(), urls)
}

// dialFleetNativeContext tries ordered endpoints under one caller-owned
// operation context and one ten-block, independent deadline for each dial
// and runtime-artifact authentication attempt.
func dialFleetNativeContext(ctx context.Context, urls []string) (*crv4.Chain, string, error) {
	return dialFleetNativeWithContext(ctx, urls, fleetNativeEndpointTimeout, crv4.DialChainContext)
}

// fleetNativeDialContext keeps the endpoint deadline boundary deterministic
// under test while production binds it to CRv4's context-aware dialer.
type fleetNativeDialContext func(context.Context, string) (*crv4.Chain, error)

// fleetNativeEndpointContext creates one bounded context for one ordered
// endpoint. It is injected below only to make timeout/failover behavior
// deterministic without mutating process-global time in tests.
type fleetNativeEndpointContext func(context.Context, time.Duration) (context.Context, context.CancelFunc)

// fleetNativeRuntimeAuthenticator retains the release identity gate while
// allowing the endpoint-boundary test to isolate failover from metadata bytes.
type fleetNativeRuntimeAuthenticator func(context.Context, *crv4.Chain) error

// dialFleetNativeWithContext performs one bounded dial plus exact runtime
// authentication per endpoint. A failed endpoint never leaves its transport
// open while failover advances to the next ordered address.
func dialFleetNativeWithContext(ctx context.Context, urls []string, endpointTimeout time.Duration, dial fleetNativeDialContext) (*crv4.Chain, string, error) {
	return dialFleetNativeWithEndpointContext(ctx, urls, endpointTimeout, context.WithTimeout, dial, func(endpointCtx context.Context, chain *crv4.Chain) error {
		_, err := authenticateAndBindFleetRuntimeFinalizedContext(endpointCtx, chain)
		return err
	})
}

// dialFleetNativeWithEndpointContext owns endpoint cleanup on both dial and
// identity failures. The default production path above supplies WithTimeout
// and the exact-v454 authenticator; the injected parameters exist solely for
// deterministic cancellation and failover tests.
func dialFleetNativeWithEndpointContext(ctx context.Context, urls []string, endpointTimeout time.Duration, endpointContext fleetNativeEndpointContext, dial fleetNativeDialContext, authenticate fleetNativeRuntimeAuthenticator) (*crv4.Chain, string, error) {
	if ctx == nil || endpointTimeout <= 0 || endpointContext == nil || dial == nil || authenticate == nil {
		return nil, "", errors.New("fleet native dial context is incomplete")
	}
	var errs []error
	for _, endpoint := range urls {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		endpointCtx, cancel := endpointContext(ctx, endpointTimeout)
		if endpointCtx == nil || cancel == nil {
			return nil, "", errors.New("fleet native endpoint context is incomplete")
		}
		chain, err := dial(endpointCtx, endpoint)
		if err != nil {
			cancel()
			errs = append(errs, fmt.Errorf("%s: %w", endpoint, err))
			continue
		}
		if err := authenticate(endpointCtx, chain); err != nil {
			cancel()
			closeFleetNative(chain)
			errs = append(errs, fmt.Errorf("%s: runtime identity does not match the release pin: %w", endpoint, err))
			continue
		}
		cancel()
		return chain, endpoint, nil
	}
	return nil, "", fmt.Errorf("no release Substrate endpoint answered: %w", errors.Join(errs...))
}

// closeFleetNative is deliberately nil-safe because dial/auth failure paths
// must not turn an incomplete provider object into a process panic.
func closeFleetNative(chain *crv4.Chain) {
	if chain != nil && chain.API != nil && chain.API.Client != nil {
		chain.API.Client.Close()
	}
}

func fleetCommand(opts docopt.Opts) error {
	manifest, err := loadFleetManifest(fleetOpt(opts, "--manifest"))
	if err != nil {
		return err
	}
	switch {
	case mustBoolOpt(opts, "manifest"):
		canonical, _ := manifest.Canonical()
		hash, _ := manifest.CommitmentHash()
		fmt.Printf("%s\ncommitment_sha256: 0x%x\nmembers: %d\n", canonical, hash, len(manifest.Members))
		return nil
	case mustBoolOpt(opts, "publish"):
		return fleetPublish(opts, manifest)
	case mustBoolOpt(opts, "bind"):
		return fleetBind(opts, manifest)
	case mustBoolOpt(opts, "status"):
		return fleetStatus(opts, manifest)
	case mustBoolOpt(opts, "revoke"):
		return fleetRevoke(opts, manifest)
	default:
		return errors.New("unknown fleet command")
	}
}

func mustBoolOpt(opts docopt.Opts, name string) bool {
	v, _ := opts.Bool(name)
	return v
}

func fleetPublish(opts docopt.Opts, manifest *protocol.FleetManifest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	chain, endpoint, err := dialFleetNativeContext(ctx, fleetOpts(opts, "--substrate"))
	if err != nil {
		return err
	}
	defer chain.API.Client.Close()
	seed, err := crv4.LoadSeedFile(fleetOpt(opts, "--hotkey_seed_file"))
	if err != nil {
		return err
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return err
	}
	if hotkey.PublicKey() != manifest.Hotkey {
		return errors.New("hotkey seed does not match manifest hotkey")
	}
	hash, _ := manifest.CommitmentHash()
	if _, err := authenticateAndBindFleetRuntimeFinalizedContext(ctx, chain); err != nil {
		return fmt.Errorf("authenticate fleet runtime before publish: %w", err)
	}
	receipt, err := chain.SetFleetCommitment(ctx, hotkey, manifest.Netuid, hash)
	if err != nil {
		return err
	}
	verified, err := verifyPinnedFleetCommitmentWriteContext(ctx, chain, manifest.Netuid, manifest.Hotkey, hash, receipt)
	if err != nil {
		return fmt.Errorf("verify pinned fleet commitment write: %w", err)
	}
	fmt.Printf("fleet commitment finalized\n  endpoint: %s\n  netuid: %d\n  hotkey: 0x%x\n  commitment: 0x%x\n  extrinsic: %s\n  finalized_block: %d\n  finalized_hash: %s\n", endpoint, manifest.Netuid, manifest.Hotkey, hash, verified.ExtrinsicHash.Hex(), verified.FinalizedAt, verified.FinalizedHash.Hex())
	return nil
}

func fleetBindingAndSign(opts docopt.Opts, manifest *protocol.FleetManifest) (protocol.FleetBinding, []byte, []byte, error) {
	clientID, err := parseClientID16(fleetOpt(opts, "--client_id"))
	if err != nil {
		return protocol.FleetBinding{}, nil, nil, err
	}
	member, err := fleetMember(manifest, clientID)
	if err != nil {
		return protocol.FleetBinding{}, nil, nil, err
	}
	from, err := parseFleetEpoch(opts, "--valid_from_epoch")
	if err != nil {
		return protocol.FleetBinding{}, nil, nil, err
	}
	to, err := parseFleetEpoch(opts, "--valid_to_epoch")
	if err != nil {
		return protocol.FleetBinding{}, nil, nil, err
	}
	binding, err := manifest.Binding(member, from, to)
	if err != nil {
		return binding, nil, nil, err
	}
	clientPrivate, err := loadEd25519Seed(fleetOpt(opts, "--client_seed_file"))
	if err != nil {
		return binding, nil, nil, err
	}
	clientSignature, err := binding.SignClient(clientPrivate)
	if err != nil {
		return binding, nil, nil, err
	}
	hotkeySeed, err := crv4.LoadSeedFile(fleetOpt(opts, "--hotkey_seed_file"))
	if err != nil {
		return binding, nil, nil, err
	}
	hotkey, err := crv4.KeypairFromSeed(hotkeySeed)
	if err != nil || hotkey.PublicKey() != manifest.Hotkey {
		return binding, nil, nil, errors.New("hotkey seed does not match manifest")
	}
	digest, _ := binding.Digest()
	hotkeySignature, err := hotkey.Sign(digest[:])
	return binding, clientSignature, hotkeySignature, err
}

func fleetBind(opts docopt.Opts, manifest *protocol.FleetManifest) error {
	binding, clientSignature, hotkeySignature, err := fleetBindingAndSign(opts, manifest)
	if err != nil {
		return err
	}
	calldata, err := onchain.BuildFleetBindingCalldata(binding, clientSignature, hotkeySignature)
	if err != nil {
		return err
	}
	relayer, err := onchain.LoadKeyFile(fleetOpt(opts, "--relayer_key_file"))
	if err != nil {
		return err
	}
	receipt, err := onchain.Submit(context.Background(), onchain.SubmitParams{
		Contract: common.Address(manifest.Coordinator), Rpcs: fleetOpts(opts, "--rpc"), Key: relayer,
		Calldata: calldata, ChainID: new(big.Int).SetUint64(manifest.ChainID), DryRun: mustBoolOpt(opts, "--dry-run"),
	})
	if err != nil || receipt == nil {
		return err
	}
	for _, log := range receipt.Logs {
		if event, unpackErr := stCoordinator.UnpackFleetBoundEvent(log); unpackErr == nil {
			fmt.Printf("fleet member binding finalized: client=0x%x fleet=0x%x hotkey=0x%x uid=%d generation=%d epochs=[%d,%d]\n", event.ClientId, event.FleetId, event.Hotkey, event.Uid, event.Generation, event.ValidFromEpoch, event.ValidToEpoch)
		}
	}
	return nil
}

func finalizedCoordinatorCall(ctx context.Context, manifest *protocol.FleetManifest, rpcs []string, calldata []byte) ([]byte, string, error) {
	var errs []error
	for _, endpoint := range rpcs {
		chainIDHex, err := ethRpcHexResult(ctx, endpoint, "eth_chainId", []any{})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		chainID, err := parseEthHexQuantity(chainIDHex)
		if err != nil || chainID != manifest.ChainID {
			errs = append(errs, fmt.Errorf("%s chain id %d, manifest %d", endpoint, chainID, manifest.ChainID))
			continue
		}
		result, err := ethRpcHexResult(ctx, endpoint, "eth_call", []any{map[string]any{"to": common.Address(manifest.Coordinator).Hex(), "data": "0x" + hex.EncodeToString(calldata)}, "finalized"})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		decoded, err := parseEthHexBytes(result)
		if err == nil {
			return decoded, endpoint, nil
		}
		errs = append(errs, err)
	}
	return nil, "", fmt.Errorf("no finalized coordinator view answered: %w", errors.Join(errs...))
}

func fleetStatus(opts docopt.Opts, manifest *protocol.FleetManifest) error {
	ctx, cancel := context.WithTimeout(context.Background(), fleetStatusTimeout)
	defer cancel()
	clientID, err := parseClientID16(fleetOpt(opts, "--client_id"))
	if err != nil {
		return err
	}
	want, _ := manifest.CommitmentHash()
	chain, endpoint, err := dialFleetNativeContext(ctx, fleetOpts(opts, "--substrate"))
	if err != nil {
		return err
	}
	native, nativeErr := pinnedFleetCommitmentFinalizedContext(ctx, chain, manifest.Netuid, manifest.Hotkey)
	chain.API.Client.Close()
	if nativeErr != nil {
		return nativeErr
	}
	ret, rpc, err := finalizedCoordinatorCall(ctx, manifest, fleetOpts(opts, "--rpc"), stCoordinator.PackGetFleetBinding(clientID))
	if err != nil {
		return err
	}
	record, err := stCoordinator.UnpackGetFleetBinding(ret)
	if err != nil {
		return err
	}
	fmt.Printf("fleet status\n  manifest_commitment: 0x%x\n  native_commitment: 0x%x (block %d, finalized %d via %s)\n  coordinator: %s via %s\n  client: 0x%x generation=%d epochs=[%d,%d] cleaned=%d uid=%d\n", want, native.Hash, native.CommitmentBlock, native.FinalizedAt, endpoint, common.Address(manifest.Coordinator), rpc, clientID, record.Generation, record.ValidFromEpoch, record.ValidToEpoch, record.CleanedAtEpoch, record.Uid)
	if native.Hash != want || record.CommitmentHash != want || record.Hotkey != manifest.Hotkey || record.FleetId != manifest.FleetID {
		return errors.New("fleet status does not match the canonical manifest")
	}
	return nil
}

func fleetRevoke(opts docopt.Opts, manifest *protocol.FleetManifest) error {
	clientID, err := parseClientID16(fleetOpt(opts, "--client_id"))
	if err != nil {
		return err
	}
	member, err := fleetMember(manifest, clientID)
	if err != nil {
		return err
	}
	effective, err := parseFleetEpoch(opts, "--effective_epoch")
	if err != nil {
		return err
	}
	ret, _, err := finalizedCoordinatorCall(context.Background(), manifest, fleetOpts(opts, "--rpc"), stCoordinator.PackFleetRevokeDigest(clientID, manifest.Generation, effective))
	if err != nil {
		return err
	}
	digest, err := stCoordinator.UnpackFleetRevokeDigest(ret)
	if err != nil {
		return err
	}
	private, err := loadEd25519Seed(fleetOpt(opts, "--client_seed_file"))
	if err != nil {
		return err
	}
	if !bytesEqual(private.Public().(ed25519.PublicKey), member.ClientKey[:]) {
		return errors.New("client seed does not match manifest member")
	}
	signature := ed25519.Sign(private, digest[:])
	calldata, err := stCoordinator.TryPackRevokeFleetBinding(clientID, manifest.Generation, effective, signature)
	if err != nil {
		return err
	}
	relayer, err := onchain.LoadKeyFile(fleetOpt(opts, "--relayer_key_file"))
	if err != nil {
		return err
	}
	_, err = onchain.Submit(context.Background(), onchain.SubmitParams{
		Contract: common.Address(manifest.Coordinator), Rpcs: fleetOpts(opts, "--rpc"), Key: relayer,
		Calldata: calldata, ChainID: new(big.Int).SetUint64(manifest.ChainID), DryRun: mustBoolOpt(opts, "--dry-run"),
	})
	return err
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
