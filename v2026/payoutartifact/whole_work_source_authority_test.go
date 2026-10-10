// Live admission keys enter only through the independently signed roster. Old
// authority bytes retain their original omission grammar when no role is added.
package payoutartifact

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// A receipt cannot choose another network or reuse a generation declaration.
// The original authority signature authenticates the full delegated source.
func TestWholeWorkSourceDelegationRequiresDomainAndUniqueGeneration(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	domainHash, err := fixture.authority.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{201}, ed25519.SeedSize))
	source := protocol.ProviderWorkSourceAuthority{
		DomainHash: domainHash, SourceId: "01000000-0000-0000-0000-000000000000",
		Generation: "02000000-0000-0000-0000-000000000000", PublicKey: [32]byte(key[ed25519.SeedSize:]),
		FromUnixMicro: fixture.inventory.Clock.StartTime.UnixMicro(), ThroughUnixMicro: fixture.inventory.Clock.EndTime.UnixMicro(),
		MaxEndpointEvents: protocol.MaximumProviderWorkEndpointEvents, MaxCohortMembers: protocol.MaximumProviderWorkCohortMembers,
	}
	fixture.authority.WorkSources = []protocol.ProviderWorkSourceAuthority{source}
	fixture.signAuthority(t)
	decoded, err := DecodeWholeWorkAuthority(t.Context(), fixture.inventory.Authority, fixture.expected.AuthoritySigner)
	if err != nil || len(decoded.WorkSources) != 1 || decoded.WorkSources[0].PublicKey != source.PublicKey {
		t.Fatal("independently signed source delegation was not retained", decoded, err)
	}
	fixture.authority.WorkSources[0].DomainHash[0] ^= 1
	if _, err := SignWholeWorkAuthority(t.Context(), fixture.authority, fixture.authorityKey); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("foreign original source acquired this network's authority", err)
	}
	fixture.authority.WorkSources = []protocol.ProviderWorkSourceAuthority{source, source}
	if _, err := SignWholeWorkAuthority(t.Context(), fixture.authority, fixture.authorityKey); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("duplicate source generation acquired another complete declaration", err)
	}
}

// Optional source delegation cannot rewrite already signed historical rosters.
func TestWholeWorkSourceDelegationPreservesLegacyAuthorityOmission(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	original := bytes.Clone(fixture.inventory.Authority)
	fixture.authority.WorkSources = []protocol.ProviderWorkSourceAuthority{}
	fixture.signAuthority(t)
	if !bytes.Equal(original, fixture.inventory.Authority) {
		t.Fatal("empty source delegation changed original legacy authority bytes")
	}
}
