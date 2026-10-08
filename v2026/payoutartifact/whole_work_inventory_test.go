// Independent roster, original SDK cuts and actual signed close chains must all
// agree before a complete whole-window component can leave the verifier.
package payoutartifact

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// Synthetic roots are distinct from the artifact publisher and SDK owners.
type wholeWorkTestFixture struct {
	artifact     *Artifact
	inventory    *WholeWorkInventory
	expected     WholeWorkExpectation
	authority    WholeWorkAuthority
	authorityKey *ecdsa.PrivateKey
	requestKey   ed25519.PrivateKey
	ownerKeys    []ed25519.PrivateKey
}

// Generate actual canonical originals without sharing any verification verdict.
func newWholeWorkTestFixture(t *testing.T) *wholeWorkTestFixture {
	t.Helper()
	artifact, root := inventoryArtifact(t)
	start, _ := time.Parse(time.RFC3339Nano, artifact.ClosedWork.WindowStart)
	end, _ := time.Parse(time.RFC3339Nano, artifact.ClosedWork.WindowEnd)
	startHeader := &types.Header{Number: new(big.Int).SetUint64(artifact.Start.Number), Time: uint64(start.Unix())}
	endHeader := &types.Header{Number: new(big.Int).SetUint64(artifact.End.Number), Time: uint64(end.Unix())}
	artifact.Start.Hash, artifact.End.Hash = startHeader.Hash().Hex(), endHeader.Hash().Hex()
	artifact.ClosedWork.Start, artifact.ClosedWork.End = artifact.Start, artifact.End
	clock := &ClosedWorkWindowClock{Start: artifact.Start, End: artifact.End, StartTime: start, EndTime: end}
	clock.StartHeader, _ = rlp.EncodeToBytes(startHeader)
	clock.EndHeader, _ = rlp.EncodeToBytes(endHeader)
	domain, _ := ClosedWorkReportDomain(artifact)
	domainHash, _ := domain.Digest()
	authorityKey, err := crypto.HexToECDSA(strings.Repeat("39", 32))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{94}, 32))
	authority := WholeWorkAuthority{Domain: domain, Epoch: artifact.Epoch, Start: artifact.Start, End: artifact.End, RequestPublicKey: [32]byte(requestKey[32:]), Owners: []WholeWorkOwner{}, ExpectedProviders: []WholeWorkExpectedProvider{}, PriorContracts: []WholeWorkPriorContract{}}
	fixture := &wholeWorkTestFixture{artifact: artifact, authorityKey: authorityKey, requestKey: requestKey, expected: WholeWorkExpectation{AuthoritySigner: crypto.PubkeyToAddress(authorityKey.PublicKey), ClientKeyRootSigner: crypto.PubkeyToAddress(root.PublicKey)}}
	fixture.inventory = &WholeWorkInventory{Schema: WholeWorkInventorySchema, Window: inventoryWindow(t, artifact), Clock: clock, Owners: []WholeWorkOwnerCuts{}}
	for ownerIndex := 0; ownerIndex < 2; ownerIndex++ {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(95 + ownerIndex)}, 32))
		fixture.ownerKeys = append(fixture.ownerKeys, key)
		owner := WholeWorkOwner{ClientId: [16]byte{byte(ownerIndex + 1)}, NetworkId: [16]byte{byte((ownerIndex + 1) * 10)}, Generation: [16]byte{byte(ownerIndex + 31)}, PublicKey: [32]byte(key[32:])}
		authority.Owners = append(authority.Owners, owner)
		authority.ExpectedProviders = append(authority.ExpectedProviders, WholeWorkExpectedProvider{ClientId: owner.ClientId, NetworkId: owner.NetworkId})
		var pair WholeWorkOwnerCuts
		for side, boundary := range []Boundary{artifact.Start, artifact.End} {
			cut := coreprotocol.OriginalWorkCut{DomainHash: domainHash, ClientId: owner.ClientId, Generation: owner.Generation, Epoch: artifact.Epoch, Block: boundary.Number, BlockHash: [32]byte{}, Complete: true, Contracts: []coreprotocol.OriginalWorkContract{}}
			copy(cut.BlockHash[:], commonWholeWorkHash(t, boundary.Hash))
			if side == 1 {
				cut.Revision = 6
				for _, row := range artifact.ClosedWork.Records {
					stored, _ := proto.Marshal(&coreprotocol.StoredContract{ContractId: row.ContractId[:], SourceId: authority.Owners[0].ClientId[:], DestinationId: []byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, TransferByteCount: 100})
					var reports ClosedWorkReports
					if json.Unmarshal(row.OriginalReports, &reports) != nil {
						t.Fatal("fixture reports")
					}
					var head []byte
					for _, report := range reports.Reports {
						client, _ := closedWorkId(report.ClientId)
						if client == owner.ClientId && report.Checkpoint != nil && !*report.Checkpoint {
							head = bytes.Clone(report.Inventory)
						}
					}
					cut.Contracts = append(cut.Contracts, coreprotocol.OriginalWorkContract{ContractId: row.ContractId, StoredContract: stored, LatestInventory: head})
				}
			}
			cut, err = coreprotocol.SignOriginalWorkCut(t.Context(), cut, key)
			if err != nil {
				t.Fatal(err)
			}
			kind := "start"
			if side == 1 {
				kind = "end"
			}
			issuedAt := []time.Time{start, end}[side].Unix()
			request, err := coreprotocol.SignOriginalWorkRequest(coreprotocol.OriginalWorkRequest{RequestId: [16]byte{byte(ownerIndex + 41), byte(side + 1)}, DomainHash: domainHash, ClientId: owner.ClientId, Generation: owner.Generation, PublicKey: owner.PublicKey, Epoch: artifact.Epoch, Kind: kind, Block: boundary.Number, BlockHash: cut.BlockHash, IssuedAtUnix: issuedAt, ExpiresAtUnix: issuedAt + 300}, requestKey)
			if err != nil {
				t.Fatal(err)
			}
			requestRaw, _ := request.Bytes()
			cutRaw, _ := cut.Bytes(t.Context())
			if side == 0 {
				pair.StartRequest, pair.Start = requestRaw, cutRaw
			} else {
				pair.EndRequest, pair.End = requestRaw, cutRaw
			}
		}
		fixture.inventory.Owners = append(fixture.inventory.Owners, pair)
	}
	fixture.authority = authority
	fixture.signAuthority(t)
	closedWorkTestSign(t, artifact)
	return fixture
}

// The production helper uses canonical bytes; this fixture hash decoder does not
// share header hashing, window membership or roster decisions with the verifier.
func commonWholeWorkHash(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(decoded) != 32 {
		t.Fatal("fixture hash", err)
	}
	return decoded
}

// Each negative can retain a valid independent signature over changed input.
func (self *wholeWorkTestFixture) signAuthority(t *testing.T) {
	t.Helper()
	value, err := SignWholeWorkAuthority(t.Context(), self.authority, self.authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	self.authority = value
	self.inventory.Authority, err = value.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

// Mutations retain actual SDK signatures, reaching semantic joins rather than
// failing merely at outer custody or an invalid detached signature.
func (self *wholeWorkTestFixture) changeCut(t *testing.T, owner, side int, change func(*coreprotocol.OriginalWorkCut)) {
	t.Helper()
	pair := &self.inventory.Owners[owner]
	raw := pair.Start
	if side == 1 {
		raw = pair.End
	}
	cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	change(&cut)
	cut, err = coreprotocol.SignOriginalWorkCut(t.Context(), cut, self.ownerKeys[owner])
	if err != nil {
		t.Fatal(err)
	}
	raw, err = cut.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if side == 0 {
		pair.Start = raw
	} else {
		pair.End = raw
	}
}

func TestWholeWorkIndependentRosterAndOriginalCutsReconstructProviders(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	before := fixture.artifact.ContentHash
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || !value.Complete || value.Contracts != 2 || value.Credited != 2 || len(value.ExpectedProviders) != 2 || value.ExpectedProviders[0].UsageBytes != 100 || value.ExpectedProviders[1].UsageBytes != 100 || value.Domain != fixture.authority.Domain {
		t.Fatal("original complete inventory did not reconstruct provider inputs", value, err)
	}
	if fixture.artifact.ClosedWork.WholeInventory != nil || fixture.artifact.ContentHash != before {
		t.Fatal("companion verification rewrote signed artifact")
	}
	fixture.artifact.ClosedWork.WholeInventory = fixture.inventory
	closedWorkTestSign(t, fixture.artifact)
	if value, err := VerifyWholeWorkInventory(t.Context(), fixture.artifact, fixture.expected); err != nil || !value.Complete {
		t.Fatal("embedded original did not share verifier", value, err)
	}
}

func TestWholeWorkMissingOwnerAndIncompleteCutRemainUnknown(t *testing.T) {
	for _, change := range []func(*wholeWorkTestFixture){func(f *wholeWorkTestFixture) { f.inventory.Owners = f.inventory.Owners[:1] }, func(f *wholeWorkTestFixture) { f.inventory.Owners[0].Start = nil }, func(f *wholeWorkTestFixture) {
		f.changeCut(t, 1, 1, func(c *coreprotocol.OriginalWorkCut) { c.Complete = false })
	}, func(f *wholeWorkTestFixture) { f.inventory.Clock.StartHeader = nil }, func(f *wholeWorkTestFixture) { f.expected.AuthoritySigner = common.Address{} }} {
		fixture := newWholeWorkTestFixture(t)
		change(fixture)
		if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkUnavailable) {
			t.Fatal("missing original became complete or false disagreement", value, err)
		}
	}
}

func TestWholeWorkExpectedRosterCannotBeSelectedByArtifactOrReturnedRows(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.authorityKey, _ = crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	fixture.signAuthority(t)
	fixture.expected.AuthoritySigner = fixture.artifact.Signer
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("publisher authorized its own roster", err)
	}
	fixture = newWholeWorkTestFixture(t)
	fixture.expected.AuthorityHash = "sha256:" + strings.Repeat("11", 32)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("wrong separately pinned authority accepted", err)
	}
	fixture = newWholeWorkTestFixture(t)
	fixture.authority.Owners[0].Generation[0]++
	fixture.signAuthority(t)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("foreign SDK generation matched returned owner count", err)
	}
}

func TestWholeWorkOmittedWholeContractAndSourceOwnerAreDetected(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.changeCut(t, 0, 1, func(c *coreprotocol.OriginalWorkCut) { c.Contracts = c.Contracts[1:] })
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("omitted source contract became full union", err)
	}
	fixture = newWholeWorkTestFixture(t)
	for owner := range fixture.inventory.Owners {
		fixture.changeCut(t, owner, 1, func(c *coreprotocol.OriginalWorkCut) {
			c.Contracts = nil
			c.Contracts = []coreprotocol.OriginalWorkContract{}
		})
	}
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("empty SDK cuts erased real original earning rows", err)
	}
}

func TestWholeWorkLateStartCannotDateTerminalWorkWithoutIndependentPriorContext(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	for owner := range fixture.inventory.Owners {
		end, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), fixture.inventory.Owners[owner].End)
		if err != nil {
			t.Fatal(err)
		}
		fixture.changeCut(t, owner, 0, func(c *coreprotocol.OriginalWorkCut) { c.Contracts = end.Contracts; c.Revision = end.Revision })
	}
	// A late capture already containing terminal work must still account for it.
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); err != nil || value.Credited != 2 {
		t.Fatal("late start silently excluded current earning work", value, err)
	}
	fixture.authority.PriorContracts = []WholeWorkPriorContract{{ContractId: fixture.artifact.ClosedWork.Records[0].ContractId, ReconciledEpoch: fixture.artifact.Epoch - 1, InventoryHash: "sha256:" + strings.Repeat("45", 32), StoredContractHash: [32]byte{1}, SourceInventoryHash: [32]byte{2}}}
	fixture.signAuthority(t)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("invented prior original checkpoint excluded work", err)
	}
}

func TestWholeWorkChangedTerminalHeadAndRevisionCannotMasqueradeAsComplete(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.changeCut(t, 0, 1, func(c *coreprotocol.OriginalWorkCut) { c.Contracts[0].LatestInventory = nil })
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("missing terminal head granted full closure", err)
	}
	fixture = newWholeWorkTestFixture(t)
	fixture.changeCut(t, 0, 1, func(c *coreprotocol.OriginalWorkCut) { c.Revision = 0 })
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("changed owner state retained unchanged revision", err)
	}
}

func TestWholeWorkCancellationCapacityAndCanonicalAuthorityStayBounded(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := VerifyWholeWorkInventoryWithWitness(ctx, fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	fixture.inventory.Authority = append([]byte(" "), fixture.inventory.Authority...)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("alternate authority spelling admitted", err)
	}
	if _, err := DecodeWholeWorkInventory(t.Context(), bytes.Repeat([]byte{1}, MaxWholeWorkInventoryBytes+1)); !errors.Is(err, ErrClosedWorkCapacity) {
		t.Fatal("oversized whole witness parsed", err)
	}
	fixture = newWholeWorkTestFixture(t)
	fixture.inventory.Owners[0].Start = make([]byte, MaxWholeWorkInventoryBytes+1)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkCapacity) {
		t.Fatal("direct whole witness bypassed byte capacity", err)
	}
}

// Rebuild the actual signed payout rather than editing only its total counters.
func (self *wholeWorkTestFixture) empty(t *testing.T) {
	t.Helper()
	old := self.artifact
	census := *old.ClosedWork
	census.Count = 0
	census.Records = []ClosedWorkRecord{}
	created, _ := time.Parse(time.RFC3339Nano, old.CreatedAt)
	artifact, err := Build(BuildInput{ClosedWork: &census, DeploymentID: old.DeploymentID, GenesisHash: old.GenesisHash, PolicyHash: old.PolicyHash, ChainID: old.ChainID, Netuid: old.Netuid, Coordinator: old.Coordinator, SettlementVault: old.SettlementVault, Epoch: old.Epoch, NoID: old.NoID, Start: old.Start, End: old.End, OperatorSnapshotHash: old.OperatorSnapshotHash, FleetSnapshotHash: old.FleetSnapshotHash, Providers: []ProviderInput{}, ReliabilityAMin: old.ReliabilityAMin, CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	closedWorkTestSign(t, artifact)
	self.artifact = artifact
	self.inventory.Window.Records = []ClosedWorkWindowRecord{}
	for owner := range self.inventory.Owners {
		self.changeCut(t, owner, 1, func(c *coreprotocol.OriginalWorkCut) {
			c.Contracts = []coreprotocol.OriginalWorkContract{}
			c.Revision = 0
		})
	}
}

func TestWholeWorkKnownEmptyRequiresEveryExpectedSdkCut(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.empty(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || !value.Complete || value.Contracts != 0 || len(value.ExpectedProviders) != 2 || value.ExpectedProviders[0].UsageBytes != 0 || value.ExpectedProviders[1].UsageBytes != 0 {
		t.Fatal("explicit known-empty owner roster did not join", value, err)
	}
	fixture.inventory.Owners = fixture.inventory.Owners[:1]
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("zero SQL rows replaced missing empty SDK cut", err)
	}
}

func TestWholeWorkIndependentIdleProviderAndWalletHeadsStayExplicit(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.empty(t)
	fixture.authority.ExpectedProviders[0].WalletHeadHash = strings.Repeat("7a", 32)
	fixture.authority.ExpectedProviders[0].WalletGeneration = 3
	fixture.signAuthority(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || len(value.ExpectedProviders) != 2 || value.ExpectedProviders[0].UsageBytes != 0 || value.ExpectedProviders[0].WalletGeneration != 3 || value.ExpectedProviders[0].WalletHeadHash != strings.Repeat("7a", 32) || value.ExpectedProviders[1].WalletHeadHash != "" {
		t.Fatal("independently admitted idle or unknown wallet input disappeared", value, err)
	}
	fixture.authority.ExpectedProviders = nil
	fixture.signAuthority(t)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("missing original provider universe became known empty", err)
	}
	fixture = newWholeWorkTestFixture(t)
	fixture.authority.ExpectedProviders = fixture.authority.ExpectedProviders[:1]
	fixture.signAuthority(t)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("earning row selected an unadmitted provider", err)
	}
	fixture = newWholeWorkTestFixture(t)
	fixture.authority.ExpectedProviders[0].WalletHeadHash = strings.Repeat("AB", 32)
	fixture.authority.ExpectedProviders[0].WalletGeneration = 1
	if _, err := SignWholeWorkAuthority(t.Context(), fixture.authority, fixture.authorityKey); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("noncanonical wallet checkpoint admitted", err)
	}
}

func TestWholeWorkNextWindowRequiresIndependentlyRetainedPriorReconciliation(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	first, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || len(first.ReconciledContracts) != 2 || first.InventoryHash == "" {
		t.Fatal("first complete window did not derive original checkpoints", first, err)
	}
	priorCuts := make([]coreprotocol.OriginalWorkCut, len(fixture.inventory.Owners))
	for owner := range priorCuts {
		priorCuts[owner], err = coreprotocol.DecodeOriginalWorkCut(t.Context(), fixture.inventory.Owners[owner].End)
		if err != nil {
			t.Fatal(err)
		}
	}
	fixture.empty(t)
	fixture.artifact.Epoch++
	fixture.artifact.ClosedWork.Epoch++
	start, end := fixture.inventory.Clock.EndTime, fixture.inventory.Clock.EndTime.Add(time.Hour)
	startHeader := &types.Header{Number: new(big.Int).SetUint64(fixture.artifact.End.Number), Time: uint64(start.Unix())}
	endHeader := &types.Header{Number: new(big.Int).SetUint64(fixture.artifact.End.Number + 100), Time: uint64(end.Unix())}
	fixture.artifact.Start = Boundary{Number: startHeader.Number.Uint64(), Hash: startHeader.Hash().Hex()}
	fixture.artifact.End = Boundary{Number: endHeader.Number.Uint64(), Hash: endHeader.Hash().Hex()}
	fixture.artifact.ClosedWork.Start, fixture.artifact.ClosedWork.End = fixture.artifact.Start, fixture.artifact.End
	fixture.artifact.ClosedWork.WindowStart, fixture.artifact.ClosedWork.WindowEnd = start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)
	fixture.inventory.Window.Start, fixture.inventory.Window.End = fixture.artifact.ClosedWork.WindowStart, fixture.artifact.ClosedWork.WindowEnd
	fixture.inventory.Clock = &ClosedWorkWindowClock{Start: fixture.artifact.Start, End: fixture.artifact.End, StartTime: start, EndTime: end}
	fixture.inventory.Clock.StartHeader, _ = rlp.EncodeToBytes(startHeader)
	fixture.inventory.Clock.EndHeader, _ = rlp.EncodeToBytes(endHeader)
	fixture.authority.Epoch, fixture.authority.Start, fixture.authority.End = fixture.artifact.Epoch, fixture.artifact.Start, fixture.artifact.End
	fixture.authority.PriorContracts = append([]WholeWorkPriorContract(nil), first.ReconciledContracts...)
	for owner := range fixture.inventory.Owners {
		for side, boundary := range []Boundary{fixture.artifact.Start, fixture.artifact.End} {
			fixture.changeCut(t, owner, side, func(c *coreprotocol.OriginalWorkCut) {
				c.Epoch, c.Block, c.BlockHash = fixture.artifact.Epoch, boundary.Number, [32]byte(commonWholeWorkHash(t, boundary.Hash))
				c.Contracts, c.Revision = priorCuts[owner].Contracts, priorCuts[owner].Revision
			})
			pair := &fixture.inventory.Owners[owner]
			requestRaw := pair.StartRequest
			if side == 1 {
				requestRaw = pair.EndRequest
			}
			request, err := coreprotocol.DecodeOriginalWorkRequest(requestRaw, fixture.authority.RequestPublicKey)
			if err != nil {
				t.Fatal(err)
			}
			request.Epoch, request.Block, request.BlockHash = fixture.artifact.Epoch, boundary.Number, [32]byte(commonWholeWorkHash(t, boundary.Hash))
			request.IssuedAtUnix = []time.Time{start, end}[side].Unix()
			request.ExpiresAtUnix = request.IssuedAtUnix + 300
			request, err = coreprotocol.SignOriginalWorkRequest(request, fixture.requestKey)
			if err != nil {
				t.Fatal(err)
			}
			requestRaw, _ = request.Bytes()
			if side == 0 {
				pair.StartRequest = requestRaw
			} else {
				pair.EndRequest = requestRaw
			}
		}
	}
	fixture.signAuthority(t)
	closedWorkTestSign(t, fixture.artifact)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("newly signed prior claim replaced absent original reconciliation", err)
	}
	fixture.expected.PriorContracts = append([]WholeWorkPriorContract(nil), first.ReconciledContracts...)
	next, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || !next.Complete || next.Credited != 0 || next.Contracts != 0 || len(next.ReconciledContracts) != 2 || next.ReconciledContracts[0] != first.ReconciledContracts[0] {
		t.Fatal("retained exact original did not permit next empty window", next, err)
	}
	fixture.expected.PriorContracts[0].InventoryHash = "sha256:" + strings.Repeat("8a", 32)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("foreign prior reconciliation hash admitted", err)
	}
}

func TestWholeWorkBoundaryPermissionCannotCaptureBeforeCommittedClock(t *testing.T) {
	for side := 0; side < 2; side++ {
		fixture := newWholeWorkTestFixture(t)
		pair := &fixture.inventory.Owners[0]
		raw := pair.StartRequest
		if side == 1 {
			raw = pair.EndRequest
		}
		request, err := coreprotocol.DecodeOriginalWorkRequest(raw, fixture.authority.RequestPublicKey)
		if err != nil {
			t.Fatal(err)
		}
		request.IssuedAtUnix--
		request, err = coreprotocol.SignOriginalWorkRequest(request, fixture.requestKey)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ = request.Bytes()
		if side == 0 {
			pair.StartRequest = raw
		} else {
			pair.EndRequest = raw
		}
		if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("valid signed permission allowed an early fixed-boundary cut", side, err)
		}
	}
}

func TestWholeWorkPublicVerifierPreservesIndependentlyExpectedZeroProviderRows(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.empty(t)
	old := fixture.artifact
	created, _ := time.Parse(time.RFC3339Nano, old.CreatedAt)
	inputs := make([]ProviderInput, 0, len(fixture.authority.ExpectedProviders))
	for index, provider := range fixture.authority.ExpectedProviders {
		inputs = append(inputs, ProviderInput{ClientID: provider.ClientId, NetworkID: provider.NetworkId, Coldkey: [32]byte{byte(31 + index)}, Assignments: 1, Confirmations: 0, Eligible: false})
	}
	artifact, err := Build(BuildInput{ClosedWork: old.ClosedWork, DeploymentID: old.DeploymentID, GenesisHash: old.GenesisHash, PolicyHash: old.PolicyHash, ChainID: old.ChainID, Netuid: old.Netuid, Coordinator: old.Coordinator, SettlementVault: old.SettlementVault, Epoch: old.Epoch, NoID: old.NoID, Start: old.Start, End: old.End, OperatorSnapshotHash: old.OperatorSnapshotHash, FleetSnapshotHash: old.FleetSnapshotHash, Providers: inputs, ReliabilityAMin: old.ReliabilityAMin, CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	closedWorkTestSign(t, artifact)
	if _, err := VerifyClosedWork(t.Context(), artifact); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("legacy component acquired an unadmitted zero-row exception", err)
	}
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || len(value.ExpectedProviders) != 2 || value.ExpectedProviders[0].UsageBytes != 0 || value.ExpectedProviders[1].UsageBytes != 0 {
		t.Fatal("public whole-work path dropped independently admitted idle or failed-zero rows", value, err)
	}
	fixture.authority.ExpectedProviders = fixture.authority.ExpectedProviders[:1]
	fixture.signAuthority(t)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("unadmitted zero provider gained census authority", err)
	}
}

func TestWholeWorkSignedSdkCoverageAloneCannotAuthorizeSqlParticipantAttribution(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.Credited != 2 || value.AttributionComplete {
		t.Fatal("complete endpoint cuts falsely authorized absent participant and direction originals", value, err)
	}
}

// Each disposition retains actual signed SDK inventory, without inventing an
// original admission or cancellation clock from a publisher's SQL label.
func wholeWorkNonCreditedFixture(t *testing.T, dispositions ...string) *wholeWorkTestFixture {
	t.Helper()
	fixture := newWholeWorkTestFixture(t)
	fixture.empty(t)
	domainHash, _ := fixture.authority.Domain.Digest()
	for index, disposition := range dispositions {
		id := [16]byte{byte(index + 3)}
		source, destination := [16]byte{1}, [16]byte{2}
		stored, _ := proto.Marshal(&coreprotocol.StoredContract{ContractId: id[:], SourceId: source[:], DestinationId: destination[:], TransferByteCount: 100})
		record := coreprotocol.OriginalWorkContract{ContractId: id, StoredContract: stored}
		row := ClosedWorkWindowRecord{ContractId: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), Disposition: disposition}
		if disposition == "canceled" {
			closed := "2026-10-06T00:30:00Z"
			row.ClosedAt = &closed
			original, err := coreprotocol.SignOriginalCloseReport(coreprotocol.OriginalCloseReport{DomainHash: domainHash, ClientId: source, ContractId: id, ReportId: [16]byte{67}}, fixture.ownerKeys[0])
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := original.Bytes()
			head, err := coreprotocol.SignOriginalCloseInventory(coreprotocol.OriginalCloseInventory{DomainHash: domainHash, ClientId: source, ContractId: id, ReportHash: sha256.Sum256(raw), Sequence: 1, Terminal: true}, fixture.ownerKeys[0])
			if err != nil {
				t.Fatal(err)
			}
			record.LatestInventory, _ = head.Bytes()
		}
		fixture.changeCut(t, 0, 1, func(c *coreprotocol.OriginalWorkCut) { c.Contracts = append(c.Contracts, record); c.Revision++ })
		fixture.inventory.Window.Records = append(fixture.inventory.Window.Records, row)
	}
	return fixture
}

func TestWholeWorkOpenNeedsOriginalStateAtBoundary(t *testing.T) {
	fixture := wholeWorkNonCreditedFixture(t, "open")
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete || value.Contracts != 1 || value.Open != 1 || value.Credited != 0 || value.Canceled != 0 {
		t.Fatal("undated open SDK work acquired complete original state authority", value, err)
	}
}

func TestWholeWorkCanceledNeedsOriginalCancellationClock(t *testing.T) {
	fixture := wholeWorkNonCreditedFixture(t, "canceled")
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete || value.Contracts != 1 || value.Canceled != 1 || value.Credited != 0 || value.Open != 0 {
		t.Fatal("zero terminal SDK work acquired an original cancellation clock", value, err)
	}
}

func TestWholeWorkOpenAndZeroCanceledContractsRemainExplicit(t *testing.T) {
	fixture := wholeWorkNonCreditedFixture(t, "canceled", "open")
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || !value.Complete || value.AttributionComplete || value.Contracts != 2 || value.Canceled != 1 || value.Open != 1 || value.Credited != 0 {
		t.Fatal("explicit non-credit contracts were omitted or credited", value, err)
	}
	fixture.inventory.Window.Records = fixture.inventory.Window.Records[1:]
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("canceled contract was silently removed", err)
	}
}

func TestWholeWorkLateBoundaryCutCannotInventAnIndividualContractDate(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.empty(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.Contracts != 0 {
		t.Fatal("initial independently complete empty window was unavailable", value, err)
	}
	request, err := coreprotocol.DecodeOriginalWorkRequest(fixture.inventory.Owners[0].EndRequest, fixture.authority.RequestPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	request.IssuedAtUnix = fixture.inventory.Clock.EndTime.Unix() + 1
	request.ExpiresAtUnix = request.IssuedAtUnix + 300
	request, err = coreprotocol.SignOriginalWorkRequest(request, fixture.requestKey)
	if err != nil {
		t.Fatal(err)
	}
	fixture.inventory.Owners[0].EndRequest, err = request.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	id, source, destination := [16]byte{3}, [16]byte{1}, [16]byte{2}
	stored, err := proto.Marshal(&coreprotocol.StoredContract{ContractId: id[:], SourceId: source[:], DestinationId: destination[:], TransferByteCount: 100})
	if err != nil {
		t.Fatal(err)
	}
	fixture.changeCut(t, 0, 1, func(c *coreprotocol.OriginalWorkCut) {
		c.Revision++
		c.Contracts = []coreprotocol.OriginalWorkContract{{ContractId: id, StoredContract: stored}}
	})
	value, err = VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if value != nil || !errors.Is(err, ErrClosedWorkUnavailable) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("late undated SDK admission invented a complete window or an integrity fault", value, err)
	}
}
