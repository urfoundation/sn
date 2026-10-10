// Export complete synthetic originals for the cross-package economic consumer.
// The input supplies the actual trial identities; no verified facts are exported.
package payoutartifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// Marshal fixture originals without silently ignoring any serializer failure.
func wholeWorkExportJson(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Normal qualification exercises the same builder with deterministic IDs. An
// explicit actual-attempt artifact supplies the real fixture IDs during export.
func wholeWorkExportInput(t *testing.T) *Artifact {
	t.Helper()
	if path := os.Getenv("URNETWORK_PROVIDER_WORK_ATTEMPT_ARTIFACT"); path != "" {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			t.Fatal("attempt artifact input must be an absolute original path")
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) > 2*1024*1024 {
			t.Fatal("bounded attempt artifact original", err)
		}
		artifact, err := DecodeWithContext(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return artifact
	}
	artifact := testArtifact(t)
	providers := []ProviderInput{}
	for index := 0; index < 9; index++ {
		provider := ProviderInput{ClientID: [16]byte{byte(index + 1)}, NetworkID: [16]byte{71}, Coldkey: [32]byte{73}, BindingGeneration: 1}
		if index < 7 {
			provider.UsageBytes, provider.Assignments, provider.Confirmations, provider.Eligible = 100, 8, 8, true
		} else if index == 7 {
			provider.Assignments = 1
		}
		providers = append(providers, provider)
	}
	start, end := wholeWorkExportHeaders()
	artifact.Epoch, artifact.NoID, artifact.ReliabilityAMin, artifact.Providers = 42, 9, 8, providers
	artifact.Start, artifact.End = Boundary{Number: 1, Hash: start.Hash().Hex()}, Boundary{Number: 101, Hash: end.Hash().Hex()}
	return artifact
}

// These are the exact original headers used by the actual completed M8 fixture.
func wholeWorkExportHeaders() (*types.Header, *types.Header) {
	return &types.Header{Number: big.NewInt(1), Time: 1700000000, GasLimit: 30000000, Extra: []byte("synthetic-provider-start")},
		&types.Header{Number: big.NewInt(101), Time: 1700001000, GasLimit: 30000000, Extra: []byte("synthetic-provider-end")}
}

// Build seven independent ordinary reservations, bilateral report inventories,
// full SDK cuts and admission receipts for the original trial provider universe.
func newWholeWorkExportFixture(t *testing.T, input *Artifact) *wholeWorkTestFixture {
	t.Helper()
	startHeader, endHeader := wholeWorkExportHeaders()
	if input != nil && input.Start.Number == 2 {
		startHeader.Number, endHeader.Number = big.NewInt(2), big.NewInt(102)
	}
	start, end := time.Unix(int64(startHeader.Time), 0).UTC(), time.Unix(int64(endHeader.Time), 0).UTC()
	if input == nil || input.Start != (Boundary{Number: startHeader.Number.Uint64(), Hash: startHeader.Hash().Hex()}) || input.End != (Boundary{Number: endHeader.Number.Uint64(), Hash: endHeader.Hash().Hex()}) || input.Epoch != 42 || input.NoID != 9 || len(input.Providers) < 9 || len(input.Providers) > 128 || input.ReliabilityAMin != 8 {
		t.Fatal("input does not identify the actual completed/failed/idle original trial fixture")
	}
	providers := append([]ProviderInput(nil), input.Providers...)
	sort.Slice(providers, func(i, j int) bool { return bytes.Compare(providers[i].ClientID[:], providers[j].ClientID[:]) < 0 })
	sourceIndex, earning := -1, 0
	for index := range providers {
		provider := &providers[index]
		if provider.UsageBytes == 100 && provider.Assignments == 8 && provider.Confirmations == 8 && provider.Eligible {
			earning++
		} else if provider.UsageBytes == 0 && provider.Assignments < 8 && !provider.Eligible {
			provider.ExclusionReason = "reliability_exposure_floor"
			if sourceIndex < 0 && provider.Assignments == 0 {
				sourceIndex = index
			}
		} else {
			t.Fatal("actual trial input does not preserve the independent earning/failed/idle classes")
		}
	}
	if earning != 7 || sourceIndex < 0 {
		t.Fatal("complete provider universe lacks its seven earners or original idle consumer")
	}
	domain, err := ClosedWorkReportDomain(input)
	if err != nil {
		t.Fatal(err)
	}
	domainHash, err := domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	authorityKey, err := crypto.HexToECDSA(strings.Repeat("39", 32))
	if err != nil {
		t.Fatal(err)
	}
	rootKey, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{94}, 32))
	fixture := &wholeWorkTestFixture{requestKey: requestKey, authorityKey: authorityKey,
		expected: WholeWorkExpectation{AuthoritySigner: crypto.PubkeyToAddress(authorityKey.PublicKey), ClientKeyRootSigner: crypto.PubkeyToAddress(rootKey.PublicKey), AttributionSigner: crypto.PubkeyToAddress(authorityKey.PublicKey)}}
	fixture.authority = WholeWorkAuthority{Domain: domain, Epoch: input.Epoch, Start: input.Start, End: input.End, RequestPublicKey: [32]byte(requestKey[32:]), Owners: []WholeWorkOwner{}, ExpectedProviders: []WholeWorkExpectedProvider{}, PriorContracts: []WholeWorkPriorContract{}}
	fixture.inventory = &WholeWorkInventory{Schema: WholeWorkInventorySchema, Owners: []WholeWorkOwnerCuts{}, Clock: &ClosedWorkWindowClock{Start: input.Start, End: input.End, StartTime: start, EndTime: end}, AttributionOriginals: [][]byte{}}
	fixture.inventory.Clock.StartHeader, err = rlp.EncodeToBytes(startHeader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.inventory.Clock.EndHeader, err = rlp.EncodeToBytes(endHeader)
	if err != nil {
		t.Fatal(err)
	}
	for index, provider := range providers {
		seed := sha256.Sum256(append([]byte("synthetic-whole-work-owner-v1:"), provider.ClientID[:]...))
		key := ed25519.NewKeyFromSeed(seed[:])
		fixture.ownerKeys = append(fixture.ownerKeys, key)
		owner := WholeWorkOwner{ClientId: provider.ClientID, NetworkId: provider.NetworkID, Generation: [16]byte{0x44, byte(index + 1)}, PublicKey: [32]byte(key[32:])}
		fixture.authority.Owners = append(fixture.authority.Owners, owner)
		fixture.authority.ExpectedProviders = append(fixture.authority.ExpectedProviders, WholeWorkExpectedProvider{ClientId: owner.ClientId, NetworkId: owner.NetworkId})
	}
	source := fixture.authority.Owners[sourceIndex]
	contracts := map[[16]byte][]coreprotocol.OriginalWorkContract{}
	census := &ClosedWorkCensus{Schema: ClosedWorkSchema, DeploymentId: input.DeploymentID, ChainId: input.ChainID, GenesisHash: input.GenesisHash, Netuid: input.Netuid, Coordinator: input.Coordinator, SettlementVault: input.SettlementVault, Epoch: input.Epoch, NoId: input.NoID, PolicyHash: input.PolicyHash, Start: input.Start, End: input.End, WindowStart: start.Format(time.RFC3339Nano), WindowEnd: end.Format(time.RFC3339Nano), Records: []ClosedWorkRecord{}}
	for providerIndex, provider := range providers {
		if provider.UsageBytes == 0 {
			continue
		}
		id := [16]byte{0xa1, byte(len(census.Records) + 1)}
		stored, err := proto.Marshal(&coreprotocol.StoredContract{ContractId: id[:], SourceId: source.ClientId[:], DestinationId: provider.ClientID[:], TransferByteCount: 100})
		if err != nil {
			t.Fatal(err)
		}
		reports := ClosedWorkReports{Schema: ClosedWorkInventoryReportsSchema, Count: 2, Reports: []ClosedWorkReport{}}
		for party, ownerIndex := range []int{sourceIndex, providerIndex} {
			owner, key := fixture.authority.Owners[ownerIndex], fixture.ownerKeys[ownerIndex]
			reportId := [16]byte{0xb1, id[1], byte(party + 1)}
			original, err := coreprotocol.SignOriginalCloseReport(coreprotocol.OriginalCloseReport{DomainHash: domainHash, ClientId: owner.ClientId, ContractId: id, ReportId: reportId, AckedByteCount: 100}, key)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := original.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			head, err := coreprotocol.SignOriginalCloseInventory(coreprotocol.OriginalCloseInventory{DomainHash: domainHash, ClientId: owner.ClientId, ContractId: id, ReportHash: sha256.Sum256(raw), Sequence: 1, CumulativeAckedBytes: 100, Terminal: true}, key)
			if err != nil {
				t.Fatal(err)
			}
			headRaw, err := head.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			registration := protocol.ClientKeyRegistration{Domain: domain, ClientID: owner.ClientId, NetworkID: owner.NetworkId, Generation: 1, Present: true, PublicKey: owner.PublicKey, EffectiveBoundary: protocol.ClientKeyEffectiveBoundary{Epoch: input.Epoch, Block: input.Start.Number, Hash: [32]byte(common.HexToHash(input.Start.Hash))}}
			if err := protocol.SignClientKeyRegistration(&registration, rootKey); err != nil {
				t.Fatal(err)
			}
			registrationRaw, err := registration.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			amount, unacked, checkpoint := uint64(100), uint64(0), false
			name := "source"
			if party == 1 {
				name = "destination"
			}
			reports.Reports = append(reports.Reports, ClosedWorkReport{ClientId: participantTestId(owner.ClientId), ReportId: participantTestId(reportId), Party: name, AckedBytes: &amount, UnackedBytes: &unacked, Checkpoint: &checkpoint, Original: raw, Inventory: headRaw, KeyRegistration: registrationRaw})
			contracts[owner.ClientId] = append(contracts[owner.ClientId], coreprotocol.OriginalWorkContract{ContractId: id, StoredContract: stored, LatestInventory: headRaw})
		}
		request := &coreprotocol.CreateContract{DestinationId: provider.ClientID[:], TransferByteCount: 100}
		requestBody, err := proto.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		requestFrame, err := proto.Marshal(&coreprotocol.Frame{MessageType: coreprotocol.MessageType_TransferCreateContract, MessageBytes: requestBody})
		if err != nil {
			t.Fatal(err)
		}
		originalRequest, err := coreprotocol.SignOriginalContractRequest(t.Context(), coreprotocol.OriginalContractRequest{DomainHash: domainHash, ClientId: source.ClientId, Generation: source.Generation, RequestId: [16]byte{0xc1, id[1]}, RequestFrame: requestFrame}, fixture.ownerKeys[sourceIndex])
		if err != nil {
			t.Fatal(err)
		}
		requestRaw, err := originalRequest.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		resultBody, err := proto.Marshal(&coreprotocol.CreateContractResult{CreateContract: request, Contract: &coreprotocol.Contract{StoredContractBytes: stored, ProvideMode: coreprotocol.ProvideMode_Public}})
		if err != nil {
			t.Fatal(err)
		}
		resultFrame, err := proto.Marshal(&coreprotocol.Frame{MessageType: coreprotocol.MessageType_TransferCreateContractResult, MessageBytes: resultBody})
		if err != nil {
			t.Fatal(err)
		}
		admission, err := coreprotocol.SignOriginalContractAdmission(t.Context(), coreprotocol.OriginalContractAdmission{Request: requestRaw, ResultFrame: resultFrame}, fixture.ownerKeys[sourceIndex])
		if err != nil {
			t.Fatal(err)
		}
		creation, err := admission.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		contracts[source.ClientId][len(contracts[source.ClientId])-1].OriginalCreation = creation
		sort.Slice(reports.Reports, func(i, j int) bool {
			return reports.Reports[i].ClientId+"/"+reports.Reports[i].ReportId < reports.Reports[j].ClientId+"/"+reports.Reports[j].ReportId
		})
		snapshot := []byte(fmt.Sprintf(`{"version":1,"byte_count":100,"providers":[{"client_id":%q,"network_id":%q,"byte_count":100}]}`, participantTestId(provider.ClientID), participantTestId(provider.NetworkID)))
		census.Records = append(census.Records, ClosedWorkRecord{ContractId: id, ClosedAt: start.Add(500 * time.Second).Format(time.RFC3339Nano), Original: snapshot, OriginalReports: wholeWorkExportJson(t, reports)})
	}
	census.Count = uint64(len(census.Records))
	fixture.artifact, err = BuildWithContext(t.Context(), BuildInput{ClosedWork: census, DeploymentID: input.DeploymentID, GenesisHash: input.GenesisHash, PolicyHash: input.PolicyHash, ChainID: input.ChainID, Netuid: input.Netuid, Coordinator: input.Coordinator, SettlementVault: input.SettlementVault, Epoch: input.Epoch, NoID: input.NoID, Start: input.Start, End: input.End, OperatorSnapshotHash: input.OperatorSnapshotHash, FleetSnapshotHash: input.FleetSnapshotHash, Providers: providers, ReliabilityAMin: input.ReliabilityAMin, CreatedAt: end})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := crypto.HexToECDSA(strings.Repeat("0", 62) + "55")
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(fixture.artifact, publisher); err != nil {
		t.Fatal("synthetic publisher", err)
	}
	fixture.inventory.Window = inventoryWindow(t, fixture.artifact)
	for index, owner := range fixture.authority.Owners {
		pair := WholeWorkOwnerCuts{}
		for side, boundary := range []Boundary{input.Start, input.End} {
			cut := coreprotocol.OriginalWorkCut{DomainHash: domainHash, ClientId: owner.ClientId, Generation: owner.Generation, Epoch: input.Epoch, Block: boundary.Number, BlockHash: [32]byte(common.HexToHash(boundary.Hash)), Complete: true, Contracts: []coreprotocol.OriginalWorkContract{}}
			if side == 1 {
				cut.Contracts = append(cut.Contracts, contracts[owner.ClientId]...)
				cut.Revision = uint64(len(cut.Contracts) * 3)
			}
			cut, err = coreprotocol.SignOriginalWorkCut(t.Context(), cut, fixture.ownerKeys[index])
			if err != nil {
				t.Fatal(err)
			}
			raw, err := cut.Bytes(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			kind, issued := "start", start.Unix()
			if side == 1 {
				kind, issued = "end", end.Unix()
			}
			permission, err := coreprotocol.SignOriginalWorkRequest(coreprotocol.OriginalWorkRequest{RequestId: [16]byte{0xd1, byte(index + 1), byte(side + 1)}, DomainHash: domainHash, ClientId: owner.ClientId, Generation: owner.Generation, PublicKey: owner.PublicKey, Epoch: input.Epoch, Kind: kind, Block: boundary.Number, BlockHash: cut.BlockHash, IssuedAtUnix: issued, ExpiresAtUnix: issued + 300}, requestKey)
			if err != nil {
				t.Fatal(err)
			}
			permissionRaw, err := permission.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if side == 0 {
				pair.Start, pair.StartRequest = raw, permissionRaw
			} else {
				pair.End, pair.EndRequest = raw, permissionRaw
			}
		}
		fixture.inventory.Owners = append(fixture.inventory.Owners, pair)
	}
	// The source receipt helper consumes the source's exact current originals.
	// It assumes owner zero for its component fixtures, so retain canonical owner
	// order and supply a separate temporary view solely while creating originals.
	view := *fixture
	view.inventory = &WholeWorkInventory{}
	*view.inventory = *fixture.inventory
	view.inventory.Owners = append([]WholeWorkOwnerCuts(nil), fixture.inventory.Owners...)
	view.inventory.Owners[0] = fixture.inventory.Owners[sourceIndex]
	participantAttachOriginals(t, &view)
	fixture.authority = view.authority
	fixture.inventory.AttributionOriginals = view.inventory.AttributionOriginals
	fixture.signAuthority(t)
	return fixture
}

// Optional export is a create-once retained fixture artifact. The independent
// runner owns a precreated private directory and binds the exact bytes afterward.
func TestWholeWorkExportsCompleteOriginalsForActualTrialProviderUniverse(t *testing.T) {
	fixture := newWholeWorkExportFixture(t, wholeWorkExportInput(t))
	verified, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || verified == nil || !verified.Complete || !verified.AttributionComplete || verified.Credited != 7 || verified.Reports == nil || verified.Reports.ClosedWork.UsageBytes != 700 || len(verified.ExpectedProviders) != len(fixture.artifact.Providers) {
		t.Fatal("exported original universe did not pass the actual public verifier", verified, err)
	}
	root := os.Getenv("URNETWORK_PROVIDER_WORK_FIXTURE_DIR")
	if root == "" {
		return
	}
	if os.Getenv("URNETWORK_PROVIDER_WORK_ATTEMPT_ARTIFACT") == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		t.Fatal("cross-package export requires the actual original trial artifact and absolute output")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("export root is not precreated private custody", err)
	}
	manifest := map[string]any{"schema": "synthetic-whole-work-consumer-fixture-v1", "authority_scalar_hex": strings.Repeat("39", 32), "client_root_scalar_hex": strings.Repeat("37", 32), "publisher_scalar_hex": strings.Repeat("0", 62) + "55", "clock": fixture.inventory.Clock, "purpose": "test-only canonical originals; no serialized verified authority"}
	for name, value := range map[string]any{"artifact.json": fixture.artifact, "inventory.json": fixture.inventory, "expected.json": fixture.expected, "manifest.json": manifest} {
		raw := wholeWorkExportJson(t, value)
		file, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
		if err != nil {
			t.Fatal(err)
		}
		n, writeErr := file.Write(raw)
		if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil || n != len(raw) {
			t.Fatal("export custody", err)
		}
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatal("export readback", err)
		}
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		t.Fatal(err)
	}
}
