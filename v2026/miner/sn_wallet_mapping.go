// Provider wallet commands retain and deliver the canonical prospective
// coldkey consent consumed by release earning epochs. Login is a separate path.
package miner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// The SDK implements this interface; swarm retains its existing single-send
// HTTP owner with the same SDK request/response types and registered routes.
type snWalletMappingApi interface {
	SnEpochSyncWithContext(context.Context) (*sdk.SnEpochResult, error)
	SnWalletMappingChallengeSyncWithContext(context.Context, *sdk.SnWalletMappingChallengeArgs) (*sdk.SnWalletMappingChallengeResult, error)
	SnSetWalletSyncWithContext(context.Context, *sdk.SnSetWalletArgs) (*sdk.SnSetWalletResult, error)
}

// Default consent covers the next earning epoch through the protocol's finite
// 65,536-epoch interval. Explicit bounds are signed exactly as supplied.
func snWalletMappingInterval(ctx context.Context, api snWalletMappingApi, proof *snWalletProof) (int64, int64, error) {
	if proof != nil && proof.fromEpoch != nil {
		return *proof.fromEpoch, *proof.throughEpoch, nil
	}
	epoch, err := api.SnEpochSyncWithContext(ctx)
	if err != nil {
		return 0, 0, err
	}
	if epoch == nil || epoch.Epoch < 0 || epoch.Epoch > math.MaxInt64-65536 {
		return 0, 0, errors.New("wallet consent earning epoch is unavailable")
	}
	return epoch.Epoch + 1, epoch.Epoch + 65536, nil
}

// Canonical decoding verifies the operator's prospective signature. The
// selected authenticated API supplies deployment identity, never SQL wallets.
func snWalletMappingIdentity(message, token string, coldkey [32]byte) (*protocol.WalletMappingStatement, error) {
	statement, err := protocol.DecodeWalletMappingStatement(message)
	if err != nil {
		return nil, err
	}
	if statement.Schema != protocol.WalletMappingProspectiveSchema || statement.Coldkey != coldkey {
		return nil, protocol.ErrWalletMappingIntegrity
	}
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
		return nil, err
	}
	for claim, expected := range map[string][16]byte{"client_id": statement.ClientId, "network_id": statement.NetworkId, "user_id": statement.UserId} {
		raw, ok := claims[claim].(string)
		id, err := connect.ParseId(raw)
		if !ok || err != nil || id == (connect.Id{}) || [16]byte(id) != expected {
			return nil, protocol.ErrWalletMappingIntegrity
		}
	}
	return statement, nil
}

// A fresh challenge never signs a different client, coldkey or earning window.
func snRequestWalletMapping(ctx context.Context, api snWalletMappingApi, token, address string, coldkey [32]byte, clientId *sdk.Id, proof *snWalletProof) (string, error) {
	from, through, err := snWalletMappingInterval(ctx, api, proof)
	if err != nil {
		return "", err
	}
	challenge, err := api.SnWalletMappingChallengeSyncWithContext(ctx, &sdk.SnWalletMappingChallengeArgs{ClientId: clientId, ColdkeySs58: address, FromEpoch: from, ThroughEpoch: through})
	if err != nil {
		return "", err
	}
	if challenge == nil {
		return "", errors.New("wallet mapping challenge is unavailable")
	}
	statement, err := snWalletMappingIdentity(challenge.Message, token, coldkey)
	if err != nil {
		return "", err
	}
	if statement.FromEpoch != uint64(from) || statement.ThroughEpoch != uint64(through) {
		return "", protocol.ErrWalletMappingIntegrity
	}
	return challenge.Message, nil
}

// Retain before POST. A lost acknowledgement replays the exact nonce and
// randomized sr25519 signature; only a confirmed original may be superseded.
func snSetProviderWallet(ctx context.Context, api snWalletMappingApi, apiUrl, token string, clientId *sdk.Id, address string, coldkey [32]byte, proof *snWalletProof, selection snCredentialSelection) (returnErr error) {
	path, err := snCredentialPath(selection)
	if err != nil {
		return err
	}
	owner, err := openSnWalletConsent(ctx, path, apiUrl)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, owner.close()) }()
	original, acknowledged, err := owner.load(ctx)
	if err != nil {
		return err
	}
	if original != nil {
		statement, identityErr := snWalletMappingIdentity(original.Message, token, coldkey)
		matches := identityErr == nil
		if matches && proof != nil && proof.fromEpoch != nil {
			matches = statement.FromEpoch == uint64(*proof.fromEpoch) && statement.ThroughEpoch == uint64(*proof.throughEpoch)
		}
		if matches && proof != nil && proof.Message != "" {
			signature, decodeErr := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(proof.Signature, "0x"), "0X"))
			matches = decodeErr == nil && len(signature) == 64 && original.Message == snUnescapeMessage(proof.Message) && original.Signature == [64]byte(signature)
		}
		if !acknowledged && !matches {
			return errors.New("retained wallet consent must be acknowledged before selecting another mapping")
		}
		if acknowledged && matches {
			if proof == nil || proof.fromEpoch == nil {
				from, _, err := snWalletMappingInterval(ctx, api, nil)
				if err != nil {
					return err
				}
				matches = statement.FromEpoch <= uint64(from) && uint64(from) <= statement.ThroughEpoch
			}
			// Replay the exact original to the current Server. Cached local ACK
			// alone cannot authenticate a redeployed endpoint.
			if matches {
				acknowledged = false
			}
		}
		if acknowledged {
			original = nil
		}
	}
	if original == nil {
		if proof.unsigned() {
			return errors.New("provider wallet requires signed mapping consent; use --coldkey_seed_file or provider wallet challenge with --message and --signature")
		}
		message, signature := snUnescapeMessage(proof.Message), proof.Signature
		if proof.SeedFile != "" {
			key, err := snLoadColdkey(proof.SeedFile, address, coldkey)
			if err != nil {
				return err
			}
			message, err = snRequestWalletMapping(ctx, api, token, address, coldkey, clientId, proof)
			if err != nil {
				return err
			}
			signature, err = snSignWalletChallenge(key, message)
			if err != nil {
				return err
			}
		}
		statement, err := snWalletMappingIdentity(message, token, coldkey)
		if err != nil {
			return err
		}
		if proof.fromEpoch != nil && (statement.FromEpoch != uint64(*proof.fromEpoch) || statement.ThroughEpoch != uint64(*proof.throughEpoch)) {
			return protocol.ErrWalletMappingIntegrity
		}
		encoded, err := snNormalizeSignatureHex(signature)
		if err != nil {
			return err
		}
		raw, err := hex.DecodeString(strings.TrimPrefix(encoded, "0x"))
		if err != nil {
			return err
		}
		original = &protocol.WalletMappingConsent{Message: message, Signature: [64]byte(raw)}
		if _, _, err := protocol.VerifyWalletMappingConsent(ctx, *original); err != nil {
			return err
		}
		if err := owner.retain(ctx, *original); err != nil {
			return err
		}
	}
	statement, hash, err := protocol.VerifyWalletMappingConsent(ctx, *original)
	if err != nil {
		return err
	}
	result, err := api.SnSetWalletSyncWithContext(ctx, &sdk.SnSetWalletArgs{ClientId: clientId, ColdkeySs58: address, Message: original.Message, Signature: "0x" + hex.EncodeToString(original.Signature[:])})
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("provider wallet consent was not acknowledged; original retained for retry")
	}
	if result.Error != nil {
		return fmt.Errorf("provider wallet consent was not acknowledged; original retained for retry: %s", result.Error.Message)
	}
	if result.MappingHash != hex.EncodeToString(hash[:]) || result.MappingGeneration <= 0 || uint64(result.MappingGeneration) != statement.Generation {
		return errors.New("wallet acknowledgement differs from the retained original consent")
	}
	if err := owner.acknowledge(ctx, hash, statement.Generation); err != nil {
		return err
	}
	return snReportWalletMapping(ctx, *original)
}

// Operators receive the exact original and its head for independent roster
// review. This output does not approve or rewrite a payout capture profile.
func snReportWalletMapping(ctx context.Context, original protocol.WalletMappingConsent) error {
	statement, hash, err := protocol.VerifyWalletMappingConsent(ctx, original)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(original)
	if err != nil {
		return err
	}
	fmt.Printf("provider wallet consent accepted: head=%x generation=%d earning_epochs=%d..%d\noriginal_wallet_consent: %s\n", hash, statement.Generation, statement.FromEpoch, statement.ThroughEpoch, raw)
	return nil
}
