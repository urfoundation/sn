// Preparation and execution share one derivation, so the exact reviewed input
// survives restarts and cannot acquire a different population at signing time.
package payoutroster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Duplicate or unknown JSON members never become a different reviewed object.
func decodeJson(raw []byte, result any) error {
	if len(raw) == 0 || len(raw) > MaxRequestBytes {
		return errors.New("roster request is empty or exceeds its size limit")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("roster request has trailing JSON")
	}
	return nil
}

// Full histories produce the retained head, not just the current effective
// consent. An expired or future mapping remains explicit unknown authority.
func providerHead(ctx context.Context, config Config, input Input, provider ProviderInput) (payoutartifact.WholeWorkExpectedProvider, error) {
	result := payoutartifact.WholeWorkExpectedProvider{ClientId: provider.ClientId, NetworkId: provider.NetworkId}
	if len(provider.WalletConsents) == 0 {
		return result, nil
	}
	if len(provider.WalletConsents) > protocol.MaxWalletMappingHistory {
		return result, protocol.ErrWalletMappingCapacity
	}
	for _, original := range provider.WalletConsents {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		statement, err := protocol.DecodeWalletMappingStatement(original.Message)
		if err != nil || statement.NetworkId != provider.NetworkId || statement.Schema == protocol.WalletMappingProspectiveSchema && statement.Prospective.Signer != config.ClientKeyRootSigner {
			return result, errors.Join(protocol.ErrWalletMappingIntegrity, err)
		}
	}
	last, hash, err := protocol.VerifyWalletMappingConsent(ctx, provider.WalletConsents[len(provider.WalletConsents)-1])
	if err != nil {
		return result, err
	}
	if last.Generation != uint64(len(provider.WalletConsents)) || last.NetworkId != provider.NetworkId {
		return result, protocol.ErrWalletMappingIntegrity
	}
	mapping, err := protocol.VerifyWalletMappingHistory(ctx, provider.WalletConsents, protocol.WalletMappingHistoryExpectation{Domain: config.Domain, ClientId: provider.ClientId, HeadHash: hash, Generation: last.Generation, Epoch: input.Epoch})
	if err != nil && !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
		return result, err
	}
	if mapping != nil {
		if mapping.Statement.NetworkId != provider.NetworkId {
			return result, protocol.ErrWalletMappingIntegrity
		}
		if err := protocol.VerifyProspectiveWalletMapping(ctx, mapping, config.ClientKeyRootSigner, input.Clock.Start.Number, input.Clock.StartTime.Unix()); err != nil && !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
			return result, err
		}
	}
	result.WalletHeadHash, result.WalletGeneration = hex.EncodeToString(hash[:]), last.Generation
	return result, nil
}

// The authority owner supplies explicit complete arrays. Enrollments only
// prove named identities; they cannot discover or declare a complete population.
func assemble(ctx context.Context, config Config, input Input) (payoutartifact.WholeWorkAuthority, error) {
	var authority payoutartifact.WholeWorkAuthority
	if ctx == nil {
		return authority, errors.New("roster preparation requires an owner context")
	}
	if err := errors.Join(ctx.Err(), config.Validate()); err != nil {
		return authority, err
	}
	if input.Schema != InputSchema || !input.Complete || input.Owners == nil || input.Providers == nil || input.PriorContracts == nil || input.Clock == nil {
		return authority, errors.New("roster requires an explicitly complete owner/provider inventory and original epoch clock")
	}
	if len(input.Owners) > payoutartifact.MaxWholeWorkOwners || len(input.Providers) > payoutartifact.MaxWholeWorkOwners || len(input.PriorContracts) > payoutartifact.MaxClosedWorkRecords || len(input.WorkSources) > payoutartifact.MaxWholeWorkSources {
		return authority, payoutartifact.ErrClosedWorkCapacity
	}
	authority = payoutartifact.WholeWorkAuthority{Schema: payoutartifact.WholeWorkAuthoritySchema, Domain: config.Domain, Epoch: input.Epoch, Start: input.Clock.Start, End: input.Clock.End, RequestPublicKey: config.RequestPublicKey, ClockProfile: input.Clock.HeaderProfile, Owners: []payoutartifact.WholeWorkOwner{}, ExpectedProviders: []payoutartifact.WholeWorkExpectedProvider{}, PriorContracts: append([]payoutartifact.WholeWorkPriorContract{}, input.PriorContracts...), WorkSources: append([]protocol.ProviderWorkSourceAuthority{}, input.WorkSources...), Signer: config.AuthoritySigner}
	if err := payoutartifact.VerifyWholeWorkWindowClock(ctx, authority, input.Clock); err != nil {
		return authority, fmt.Errorf("roster epoch clock: %w", err)
	}
	domainHash, _ := config.Domain.Digest()
	for _, source := range input.Owners {
		if err := ctx.Err(); err != nil {
			return authority, err
		}
		owner, err := coreprotocol.DecodeOriginalWorkOwnerEnrollment(ctx, source.Enrollment)
		if err != nil {
			return authority, fmt.Errorf("roster owner enrollment: %w", err)
		}
		registration := source.Registration
		if err := registration.VerifySignature(); err != nil || registration.Signer != config.ClientKeyRootSigner || registration.Domain != config.Domain || !registration.Present || registration.ClientID != owner.ClientId || registration.PublicKey != owner.PublicKey || owner.DomainHash != domainHash {
			return authority, errors.Join(errors.New("roster owner differs from its independently signed client-key registration"), err)
		}
		authority.Owners = append(authority.Owners, payoutartifact.WholeWorkOwner{ClientId: owner.ClientId, NetworkId: registration.NetworkID, Generation: owner.Generation, PublicKey: owner.PublicKey})
	}
	sort.Slice(authority.Owners, func(i, j int) bool {
		if order := bytes.Compare(authority.Owners[i].ClientId[:], authority.Owners[j].ClientId[:]); order != 0 {
			return order < 0
		}
		return bytes.Compare(authority.Owners[i].Generation[:], authority.Owners[j].Generation[:]) < 0
	})
	for _, provider := range input.Providers {
		if err := ctx.Err(); err != nil {
			return authority, err
		}
		head, err := providerHead(ctx, config, input, provider)
		if err != nil {
			return authority, fmt.Errorf("roster provider consent: %w", err)
		}
		authority.ExpectedProviders = append(authority.ExpectedProviders, head)
	}
	sort.Slice(authority.ExpectedProviders, func(i, j int) bool {
		return bytes.Compare(authority.ExpectedProviders[i].ClientId[:], authority.ExpectedProviders[j].ClientId[:]) < 0
	})
	sort.Slice(authority.PriorContracts, func(i, j int) bool {
		return bytes.Compare(authority.PriorContracts[i].ContractId[:], authority.PriorContracts[j].ContractId[:]) < 0
	})
	sort.Slice(authority.WorkSources, func(i, j int) bool {
		return authority.WorkSources[i].SourceId+"/"+authority.WorkSources[i].Generation < authority.WorkSources[j].SourceId+"/"+authority.WorkSources[j].Generation
	})
	return authority, nil
}

// Preparation is offline and never opens a signing key. The resulting bytes
// must be reviewed and pinned before any signing or publication command.
func Prepare(ctx context.Context, config Config, raw []byte) ([]byte, error) {
	var input Input
	if err := decodeJson(raw, &input); err != nil {
		return nil, err
	}
	authority, err := assemble(ctx, config, input)
	if err != nil {
		return nil, err
	}
	selections, err := prepareNetworkWallets(ctx, config, input, &authority)
	if err != nil {
		return nil, err
	}
	if _, err := authority.SigningDigest(ctx); err != nil {
		return nil, fmt.Errorf("roster derived authority: %w", err)
	}
	request, err := json.Marshal(Request{Schema: RequestSchema, Input: input, Authority: authority, WalletSelections: selections})
	if err != nil || len(request) > MaxRequestBytes {
		return nil, errors.Join(payoutartifact.ErrClosedWorkCapacity, err)
	}
	return request, nil
}

// Signing and publishing hold the same durable domain/epoch owner. A lost API
// acknowledgement only repeats its exact signed original, never a fresh roster.
func Execute(ctx context.Context, config Config, raw []byte, expectedHash string, publish bool) (Result, error) {
	return executeWithPublisher(ctx, config, raw, expectedHash, publish, func(ctx context.Context, original []byte, signer common.Address) error {
		transport, err := NewTransport(TransportSettings{ApiBase: config.ApiBase})
		if err != nil {
			return err
		}
		defer transport.Close()
		return transport.Publish(ctx, original, signer)
	})
}

// The publication boundary is injected by deterministic custody tests. The
// public command always uses the authenticated production transport above.
func executeWithPublisher(ctx context.Context, config Config, raw []byte, expectedHash string, publish bool, publishOriginal func(context.Context, []byte, common.Address) error) (result Result, resultErr error) {
	if ctx == nil {
		return result, errors.New("roster execution requires an owner context")
	}
	if err := errors.Join(ctx.Err(), config.Validate()); err != nil {
		return result, err
	}
	if len(raw) == 0 || len(raw) > MaxRequestBytes {
		return result, payoutartifact.ErrClosedWorkCapacity
	}
	requestHash := sha256.Sum256(raw)
	if len(expectedHash) != 64 || expectedHash != hex.EncodeToString(requestHash[:]) {
		return result, errors.New("roster request differs from its independently reviewed SHA-256")
	}
	var request Request
	if err := decodeJson(raw, &request); err != nil {
		return result, err
	}
	inputRaw, err := json.Marshal(request.Input)
	if err != nil || request.Schema != RequestSchema {
		return result, errors.Join(errors.New("roster request schema is invalid"), err)
	}
	prepared, err := Prepare(ctx, config, inputRaw)
	if err != nil || !bytes.Equal(prepared, raw) {
		return result, errors.Join(errors.New("roster request differs from the complete canonical derivation"), err)
	}
	domainHash, _ := config.Domain.Digest()
	result = Result{DomainHash: hex.EncodeToString(domainHash[:]), Epoch: request.Authority.Epoch, RequestSha256: expectedHash}
	store, err := OpenStore(ctx, config.StateDirectory)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	matchesRequest := func(original []byte) error {
		signed, err := payoutartifact.DecodeWholeWorkAuthority(ctx, original, config.AuthoritySigner)
		if err != nil {
			return err
		}
		signed.Signature = [65]byte{}
		unsigned, err := json.Marshal(signed)
		wantUnsigned, wantErr := json.Marshal(request.Authority)
		if err != nil || wantErr != nil || !bytes.Equal(unsigned, wantUnsigned) {
			return errors.Join(ErrStoreConflict, errors.New("retained roster differs from its reviewed request"), err, wantErr)
		}
		return nil
	}
	original, loadErr := store.Load(ctx, domainHash, result.Epoch)
	if loadErr == nil {
		if err := matchesRequest(original); err != nil {
			return result, err
		}
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return result, loadErr
	}
	signOriginal := func() ([]byte, error) {
		key, err := LoadSigningKey(config.KeyFile)
		if err != nil {
			return nil, err
		}
		if crypto.PubkeyToAddress(key.PublicKey) != config.AuthoritySigner {
			key.D.SetInt64(0)
			return nil, errors.New("roster signing key differs from the independently pinned authority")
		}
		signed, signErr := payoutartifact.SignWholeWorkAuthority(ctx, request.Authority, key)
		key.D.SetInt64(0)
		if signErr != nil {
			return nil, signErr
		}
		candidate, err := signed.Bytes(ctx)
		if err != nil {
			return nil, err
		}
		if err := matchesRequest(candidate); err != nil {
			return nil, err
		}
		return candidate, nil
	}
	acknowledgedHash, acknowledged, err := store.PublishedHash(ctx, domainHash, result.Epoch)
	if err != nil {
		return result, err
	}
	if acknowledged {
		if errors.Is(loadErr, os.ErrNotExist) {
			// Reconstruct a previously acknowledged decision locally before
			// filling either missing slot. A wrong proposal cannot poison it.
			original, err = signOriginal()
			if err != nil {
				return result, err
			}
		}
		if sha256.Sum256(original) != acknowledgedHash {
			return result, ErrStoreConflict
		}
	}
	// Re-adoption of a missing request must first agree with any retained
	// signed decision; a failed proposal cannot poison an otherwise valid run.
	if err := store.RetainRequest(ctx, domainHash, result.Epoch, raw); err != nil {
		return result, err
	}
	if errors.Is(loadErr, os.ErrNotExist) {
		if !acknowledged {
			original, err = signOriginal()
			if err != nil {
				return result, err
			}
		}
		if err := store.Retain(ctx, domainHash, result.Epoch, original); err != nil {
			return result, err
		}
	}
	authorityHash := sha256.Sum256(original)
	result.AuthoritySha256 = hex.EncodeToString(authorityHash[:])
	result.Published, err = store.IsPublished(ctx, domainHash, result.Epoch, authorityHash)
	if err != nil || result.Published || !publish {
		return result, err
	}
	if publishOriginal == nil {
		return result, errors.New("roster publisher is unavailable")
	}
	if err := publishOriginal(ctx, original, config.AuthoritySigner); err != nil {
		return result, err
	}
	if err := store.RetainPublished(ctx, domainHash, result.Epoch, authorityHash); err != nil {
		return result, err
	}
	result.Published = true
	return result, nil
}
