// The public producer gate uses independently encoded synthetic finality and
// the actual original-Wasm fixture. Two distinct Rust images must execute it;
// no Go protocol peer supplies the capture, amounts or replay result.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/server/v2026/strecovery"
	"golang.org/x/crypto/blake2b"
)

func nativeProducerTestCertificate(t *testing.T, boundary economicEmissionBoundary, key ed25519.PrivateKey, round, setId uint64) []byte {
	t.Helper()
	hash, err := hex.DecodeString(strings.TrimPrefix(boundary.Hash, "0x"))
	if err != nil || len(hash) != 32 || boundary.Number > uint64(^uint32(0)) {
		t.Fatal("invalid independently signed fixture boundary", err)
	}
	vote := binary.LittleEndian.AppendUint32(append([]byte(nil), hash...), uint32(boundary.Number))
	message := append([]byte{1}, vote...)
	message = binary.LittleEndian.AppendUint64(message, round)
	message = binary.LittleEndian.AppendUint64(message, setId)
	raw := binary.LittleEndian.AppendUint64(nil, round)
	raw = append(raw, vote...)
	raw = append(raw, 4) // SCALE one precommit.
	raw = append(raw, vote...)
	raw = append(raw, ed25519.Sign(key, message)...)
	raw = append(raw, key.Public().(ed25519.PublicKey)...)
	return append(raw, 0) // No extra ancestry is needed by the one exact vote.
}

func nativeProducerTestHeader(t *testing.T, encoded string, expected historicalReplayDigest) (rootReceiptHeader, uint64) {
	t.Helper()
	raw, err := historicalReplayHex(encoded, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	var header types.Header
	if err := codec.Decode(raw, &header); err != nil {
		t.Fatal(err)
	}
	canonical, err := codec.Encode(header)
	if err != nil || !bytes.Equal(canonical, raw) {
		t.Fatal("independent header fixture is not canonical", err)
	}
	result := rootReceiptHeader{ParentHash: header.ParentHash.Hex(), Number: fmt.Sprintf("0x%x", uint64(header.Number)), StateRoot: header.StateRoot.Hex(), ExtrinsicsRoot: header.ExtrinsicsRoot.Hex()}
	result.Digest.Logs = []string{}
	for _, item := range header.Digest {
		value, err := codec.Encode(item)
		if err != nil {
			t.Fatal(err)
		}
		result.Digest.Logs = append(result.Digest.Logs, nativeExecutionTestHex(value))
	}
	if number, err := result.authenticate(nativeExecutionTestHex(expected[:])); err != nil || number != uint64(header.Number) {
		t.Fatal("fixture header did not preserve its independently encoded identity", err)
	}
	return result, uint64(header.Number)
}

type nativeProducerPublicFixture struct {
	ctx                  context.Context
	source               *economicEmissionFixture
	authority            nativeProducerAuthority
	policy               string
	requests             atomic.Int64
	blocks               atomic.Int64
	proofs               atomic.Int64
	failClosing          atomic.Bool
	corruptCertificate   atomic.Bool
	cancelAfterCompleted context.CancelFunc
}

func newNativeProducerPublicFixture(t *testing.T) *nativeProducerPublicFixture {
	return nativeProducerPublicFixtureFrom(t, false)
}

func nativeProducerPublicFixtureFrom(t *testing.T, continuous bool, additionalJobs ...historicalReplayJob) *nativeProducerPublicFixture {
	t.Helper()
	return nativeProducerPublicFixtureWithFee(t, continuous, nil, additionalJobs...)
}

// Optional original fee authority is signed before any producer owner opens.
// Existing fixture families retain their exact nil authority and body bounds.
func nativeProducerPublicFixtureWithFee(t *testing.T, continuous bool, feePolicy *nativeFeeCensusPolicy, additionalJobs ...historicalReplayJob) *nativeProducerPublicFixture {
	t.Helper()
	return nativeProducerPublicFixtureWithRuntime(t, continuous, feePolicy, false, additionalJobs...)
}

// Renewal fixtures additionally prove an actual original :code write and use
// the next program's original profile. Ordinary fixtures retain exact inputs.
func nativeProducerPublicFixtureWithRuntime(t *testing.T, continuous bool, feePolicy *nativeFeeCensusPolicy, runtimeRenewal bool, additionalJobs ...historicalReplayJob) *nativeProducerPublicFixture {
	t.Helper()
	return nativeProducerPublicFixtureWithTreasury(t, continuous, feePolicy, runtimeRenewal, nil, additionalJobs...)
}

// Treasury fixtures select their own original export and signed public policy.
// Nil retains every historical producer fixture input and authority byte.
func nativeProducerPublicFixtureWithTreasury(t *testing.T, continuous bool, feePolicy *nativeFeeCensusPolicy, runtimeRenewal bool, treasury *nativeTreasuryAuthority, additionalJobs ...historicalReplayJob) *nativeProducerPublicFixture {
	t.Helper()
	capturePath, replayPath, fixturePath := os.Getenv("URNETWORK_NATIVE_CAPTURE_ENGINE"), os.Getenv("URNETWORK_NATIVE_EXECUTION_ENGINE"), os.Getenv("URNETWORK_NATIVE_EXECUTION_FIXTURE")
	if treasury != nil {
		fixturePath = os.Getenv("URNETWORK_NATIVE_TREASURY_FIXTURE")
		if capturePath == "" || replayPath == "" || fixturePath == "" {
			t.Fatal("treasury producer requires explicit original export and capture/replay engines")
		}
	}
	continuationDirectory := os.Getenv("URNETWORK_NATIVE_PRODUCER_FIXTURE")
	if continuous && continuationDirectory != "" && !runtimeRenewal {
		fixturePath = filepath.Join(continuationDirectory, "native-job-101.json")
	}
	if capturePath == "" && replayPath == "" && fixturePath == "" {
		t.Skip("requires explicit real capture/replay/public-producer gate")
	}
	if continuous && continuationDirectory == "" {
		t.Fatal("continuation gate requires actual exported five-block original-program jobs")
	}
	_, captureHash, err := readPlanFile(t.Context(), capturePath, historicalReplayEngineLimit)
	if err != nil {
		t.Fatal(err)
	}
	_, replayHash, err := readPlanFile(t.Context(), replayPath, historicalReplayEngineLimit)
	if err != nil || captureHash == replayHash {
		t.Fatal("public producer requires distinct real fixed-protocol images", err)
	}
	raw, _, err := readPlanFile(t.Context(), fixturePath, historicalNativeJobLimit)
	if err != nil {
		t.Fatal(err)
	}
	var job historicalReplayJob
	if err := decodePlanJson(raw, &job); err != nil || job.ObservationProfile == nil {
		t.Fatal("actual original-program fixture is absent", err)
	}
	parent, parentNumber := nativeProducerTestHeader(t, job.ParentHeaderHex, job.ParentHash)
	child, childNumber := nativeProducerTestHeader(t, job.ChildHeaderHex, job.ChildHash)
	maximumExtrinsics := 2
	if feePolicy != nil {
		maximumExtrinsics = 3
	}
	if parentNumber != 100 || childNumber != 101 || len(job.ExtrinsicsHex) > maximumExtrinsics || len(job.ExtrinsicsHex) != 0 && !job.PrincipalEffects && feePolicy == nil || len(additionalJobs) > 1 || continuous && len(additionalJobs) != 0 {
		t.Fatal("unexpected original-program fixture shape")
	}
	source := newEconomicEmissionFixture(t)
	source.policy.Network.NativeChain = "fixture-mainnet"
	source.chain.action.Scope.NativeChain = "fixture-mainnet"
	// The Rust original appends pallet 7/event 250. Independently round-trip
	// matching metadata so the public path decodes its actual event bytes too.
	metadata, metadataHex, metadataHash := economicEmissionTestMetadata(t, func(value *types.Metadata) {
		used := map[types.U8]bool{}
		for _, pallet := range value.AsMetadataV14.Pallets {
			used[pallet.Index] = true
		}
		var replacement types.U8
		for index := 0; index < 256; index++ {
			if !used[types.U8(index)] {
				replacement = types.U8(index)
				break
			}
		}
		for index := range value.AsMetadataV14.Pallets {
			pallet := &value.AsMetadataV14.Pallets[index]
			if pallet.Name != "SubtensorModule" {
				if pallet.Index == 7 {
					pallet.Index = replacement
				}
				continue
			}
			pallet.Index = 7
			events := value.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()]
			for variant := range events.Def.Variant.Variants {
				item := &events.Def.Variant.Variants[variant]
				if item.Name == "IncentiveAlphaEmittedToMiners" {
					item.Index = 250
				} else if item.Index == 250 {
					t.Fatal("fixture event 250 is already occupied")
				}
			}
		}
	})
	source.chain.metadata, source.chain.metadataHex = metadata, metadataHex
	source.policy.Runtime.RuntimeMetadataHash = metadataHash
	source.policy.Runtime.RuntimeSourceCommit = nativeExecutionRuntimeSource(treasury)
	source.policy.Runtime.RuntimeCodeHash = nativeExecutionTestHex(job.RuntimeCodeBlake2b256[:])
	source.policy.Runtime.RuntimeVersion.StateVersion = job.ExecutionStateVersion
	source.chain.profile = source.policy.Runtime
	for _, selected := range []struct {
		number uint64
		header rootReceiptHeader
		hash   historicalReplayDigest
	}{{number: parentNumber, header: parent, hash: job.ParentHash}, {number: childNumber, header: child, hash: job.ChildHash}} {
		old := source.chain.byHeight[selected.number]
		hash := nativeExecutionTestHex(selected.hash[:])
		source.storageKVs[hash] = source.storageKVs[old]
		delete(source.storageKVs, old)
		delete(source.chain.headers, old)
		delete(source.chain.bodies, old)
		source.chain.headers[hash], source.chain.byHeight[selected.number], source.chain.bodies[hash] = selected.header, hash, append([]string{}, job.ExtrinsicsHex...)
	}
	source.policy.From = economicEmissionBoundary{Number: parentNumber, Hash: nativeExecutionTestHex(job.ParentHash[:])}
	source.policy.Through = economicEmissionBoundary{Number: childNumber, Hash: nativeExecutionTestHex(job.ChildHash[:])}
	source.chain.finalized = source.policy.Through.Hash
	allocationCount := 2
	for _, rule := range job.ObservationProfile.Rules {
		if rule.Purpose == "native-epoch" {
			for _, memory := range rule.Memory {
				if memory.Name == "hotkeys" && memory.Repeat == nil {
					allocationCount = int(memory.Bytes) / 32
				}
			}
		}
	}
	if allocationCount != 2 && allocationCount != 1024 && allocationCount != 2048 {
		t.Fatal("unsupported actual fixture allocation census", allocationCount)
	}
	amounts := make([]uint64, allocationCount)
	amounts[0], amounts[1] = 9, 89
	populated := nativeYumaTestOriginalEvents(t, job, allocationCount)
	if populated != nil {
		amounts = populated.Emissions
	}
	source.incentive(t, 101, 25, amounts...)
	if allocationCount > 2 {
		source.policy.MaximumUids = uint16(allocationCount)
	}
	source.chain.storageKVs[runtimeCodeStorageKey] = job.RuntimeCodeHex
	ctx, filePolicy, files := nativeProducerTestFiles(t, nil)
	if err := files.close(); err != nil {
		t.Fatal(err)
	}
	profileRaw, err := json.Marshal(job.ObservationProfile)
	if err != nil {
		t.Fatal(err)
	}
	approval := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	consensus := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize))
	nextConsensus := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, ed25519.SeedSize))
	jobs := map[string]historicalReplayJob{source.policy.Through.Hash: job}
	proofs := map[string][]string{source.policy.From.Hash: job.ProofNodesHex}
	certificates := map[string][]byte{source.policy.Through.Hash: nativeProducerTestCertificate(t, source.policy.Through, consensus, 19, 9)}
	if continuous {
		// The selected child has no direct certificate. The outgoing set
		// certifies 104, where the delayed change announced at102 enacts.
		if !runtimeRenewal {
			delete(certificates, source.policy.Through.Hash)
		}
		previous := job
		for number := uint64(102); number <= 105; number++ {
			input, _, err := readPlanFile(t.Context(), filepath.Join(continuationDirectory, fmt.Sprintf("native-job-%d.json", number)), historicalNativeJobLimit)
			if err != nil {
				t.Fatal(err)
			}
			var current historicalReplayJob
			if err := decodePlanJson(input, &current); err != nil {
				t.Fatal(err)
			}
			if (!runtimeRenewal || number == 102) && (current.RuntimeCodeSha256 != job.RuntimeCodeSha256 || current.RuntimeCodeBlake2b256 != job.RuntimeCodeBlake2b256 || !reflect.DeepEqual(current.ObservationProfile, job.ObservationProfile)) {
				t.Fatal("continuation changed original runtime or reviewed callsites")
			}
			if runtimeRenewal && number >= 103 && (current.RuntimeCodeSha256 == job.RuntimeCodeSha256 || current.RuntimeCodeBlake2b256 == job.RuntimeCodeBlake2b256 || current.ObservationProfile == nil || !current.PrincipalEffects || !reflect.DeepEqual(current.PrincipalQueries, job.PrincipalQueries)) {
				t.Fatal("runtime renewal requires distinct actual code and the original principal query authority")
			}
			encoded, err := historicalReplayHex(current.ChildHeaderHex, 64*1024)
			if err != nil {
				t.Fatal(err)
			}
			var nextHeader types.Header
			if err := codec.Decode(encoded, &nextHeader); err != nil || uint64(nextHeader.Number) != number {
				t.Fatal("continuation fixture child", err)
			}
			nextHeader.ParentHash = types.Hash(previous.ChildHash)
			if number == 102 {
				change := append([]byte{1, 4}, nextConsensus.Public().(ed25519.PublicKey)...)
				change = binary.LittleEndian.AppendUint64(change, 1)
				change = binary.LittleEndian.AppendUint32(change, 2)
				digest := append([]byte{4, 'F', 'R', 'N', 'K'}, rootCompact(uint64(len(change)))...)
				digest = append(digest, change...)
				var item types.DigestItem
				if err := codec.Decode(digest, &item); err != nil {
					t.Fatal(err)
				}
				nextHeader.Digest = append(nextHeader.Digest, item)
			}
			encoded, err = codec.Encode(nextHeader)
			if err != nil {
				t.Fatal(err)
			}
			current.ParentHeaderHex, current.ParentHash = previous.ChildHeaderHex, previous.ChildHash
			current.ChildHeaderHex, current.ChildHash = nativeExecutionTestHex(encoded), historicalReplayDigest(blake2b.Sum256(encoded))
			header, _ := nativeProducerTestHeader(t, current.ChildHeaderHex, current.ChildHash)
			hash := nativeExecutionTestHex(current.ChildHash[:])
			source.chain.headers[hash], source.chain.byHeight[number], source.chain.bodies[hash] = header, hash, append([]string{}, current.ExtrinsicsHex...)
			source.storageKVs[hash] = map[string]*string{}
			for key, value := range source.storageKVs[source.policy.Through.Hash] {
				source.storageKVs[hash][key] = value
			}
			source.set(t, number, "Events", []byte{0})
			jobs[hash], proofs[nativeExecutionTestHex(current.ParentHash[:])] = current, current.ProofNodesHex
			if runtimeRenewal && number >= 103 {
				profile := source.policy.Runtime
				profile.RuntimeVersion.SpecVersion++
				profile.RuntimeCodeHash = nativeExecutionTestHex(current.RuntimeCodeBlake2b256[:])
				for _, boundaryHash := range []string{hash, nativeExecutionTestHex(current.ParentHash[:])} {
					source.chain.runtimeKVs[boundaryHash] = profile
					code := current.RuntimeCodeHex
					source.storageKVs[boundaryHash][runtimeCodeStorageKey] = &code
				}
			}
			if number == 104 || number == 105 {
				key, setId := consensus, uint64(9)
				if number == 105 {
					key, setId = nextConsensus, 10
				}
				certificates[hash] = nativeProducerTestCertificate(t, economicEmissionBoundary{Number: number, Hash: hash}, key, 20+number, setId)
			}
			previous = current
		}
		if !runtimeRenewal {
			source.chain.finalized = source.chain.byHeight[105]
		}
		filePolicy.Producer.MaximumDescendantHeaders = 3
	}
	for _, current := range additionalJobs {
		header, number := nativeProducerTestHeader(t, current.ChildHeaderHex, current.ChildHash)
		if number != 102 || current.ParentHeaderHex != job.ChildHeaderHex || current.ParentHash != job.ChildHash || header.ParentHash != nativeExecutionTestHex(job.ChildHash[:]) || current.RuntimeCodeSha256 != job.RuntimeCodeSha256 || current.RuntimeCodeBlake2b256 != job.RuntimeCodeBlake2b256 || !reflect.DeepEqual(current.ObservationProfile, job.ObservationProfile) || len(current.ExtrinsicsHex) != 1 || !current.PrincipalEffects {
			t.Fatal("original capture continuation changed its parent, program or body census")
		}
		hash := nativeExecutionTestHex(current.ChildHash[:])
		source.chain.headers[hash], source.chain.byHeight[number], source.chain.bodies[hash] = header, hash, append([]string{}, current.ExtrinsicsHex...)
		source.storageKVs[hash] = map[string]*string{}
		for key, value := range source.storageKVs[source.policy.Through.Hash] {
			source.storageKVs[hash][key] = value
		}
		source.incentive(t, number, 25, amounts...)
		// This job executes another real epoch. Its synthetic RPC context must
		// advance the epoch and completed boundary together with that event.
		source.set(t, number, "SubnetEpochIndex", nativeExecutionTestWords(9))
		source.set(t, number, "LastEpochBlock", nativeExecutionTestWords(number))
		jobs[hash], proofs[nativeExecutionTestHex(current.ParentHash[:])] = current, current.ProofNodesHex
		certificates[hash] = nativeProducerTestCertificate(t, economicEmissionBoundary{Number: number, Hash: hash}, consensus, 20, 9)
	}
	if treasury != nil {
		nativeTreasuryTestBindPolicy(t, treasury, source.policy)
	}
	filePolicy.Treasury = treasury
	filePolicy.Schema = nativeTreasurySchema(treasury, nativeExecutionPolicySchema, nativeTreasuryExecutionPolicySchema)
	filePolicy.ApprovalPublicKey = nativeExecutionTestHex(approval.Public().(ed25519.PublicKey))
	filePolicy.ReviewSha256 = "sha256:" + hex.EncodeToString(job.ObservationProfile.SourceReviewSha256[:])
	filePolicy.ProfileSha256 = monitorReadDigest(profileRaw)
	filePolicy.Engine = planFileReference{Path: replayPath, Sha256: replayHash}
	filePolicy.Producer.Schema = nativeProducerSchema
	filePolicy.Producer.CaptureEngine = planFileReference{Path: capturePath, Sha256: captureHash}
	filePolicy.FeeCensus = feePolicy
	if job.PrincipalQueries != nil {
		filePolicy.Principal = &nativePrincipalPolicy{Schema: historicalPrincipalSchema, Api: historicalPrincipalApi, LayoutSha256: monitorReadDigest([]byte(historicalPrincipalLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic original parent API/layout review")), Parent: source.policy.From, Queries: job.PrincipalQueries}
	}
	if job.PrincipalEffects {
		filePolicy.Principal.Effects = &nativePrincipalEffectsPolicy{Schema: nativePrincipalEffectsSchema, ReviewSha256: monitorReadDigest([]byte("synthetic complete original stake cause and top-storage review")), StoragePrefixes: job.ObservationProfile.PrincipalStoragePrefixes}
	}
	if treasury != nil {
		if filePolicy.Principal == nil {
			t.Fatal("treasury fixture omitted original principal queries")
		}
		filePolicy.Principal.Schema = nativePrincipalAvailabilitySchema
		filePolicy.Principal.Availability = nativeTreasuryTestPrincipal(treasury, source.policy.From).Availability
	}
	for _, rule := range job.ObservationProfile.Rules {
		if historicalYumaPurpose(rule.Purpose) {
			filePolicy.Yuma = &nativeYumaPolicy{Schema: nativeYumaSchema, LayoutSha256: monitorReadDigest([]byte(nativeYumaLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic complete original Yuma source and layout review")), MaximumWitnessBytes: 64 * 1024, HotBlockReserve: 1, MaximumEdges: 64, MaximumOperations: 200000}
		}
	}
	if allocationCount > 2 {
		filePolicy.Yuma.MaximumWitnessBytes = 24 * 1024 * 1024
		filePolicy.Yuma.MaximumOperations = 64000000
	}
	if populated != nil {
		workload := nativeYumaTestWorkload(allocationCount)
		forecast, err := workload.forecast(15 * 1024 * 1024)
		if err != nil {
			t.Fatal(err)
		}
		filePolicy.Yuma.Workload = &workload
		filePolicy.Yuma.MaximumWitnessBytes = 15 * 1024 * 1024
		filePolicy.Yuma.HotBlockReserve = 2
		filePolicy.Yuma.MaximumEdges, filePolicy.Yuma.MaximumOperations = forecast.ReservedEdges, forecast.ReservedOperations
	}
	source.policy.Execution = filePolicy
	authority := nativeProducerAuthority{Schema: nativeProducerAuthoritySchema, Network: source.policy.Network, Netuid: source.policy.Netuid, Registration: *source.policy.SubnetRegistrationBlock, Generation: *source.policy.SubnetGeneration, From: source.policy.From, Runtime: source.policy.Runtime, ReviewSha256: filePolicy.ReviewSha256, Profile: job.ObservationProfile, CaptureEngine: filePolicy.Producer.CaptureEngine, ReplayEngine: filePolicy.Engine, Directory: filePolicy.Directory, Nodes: filePolicy.Producer.Nodes, MaximumJobs: filePolicy.Producer.MaximumJobs, MaximumBytes: filePolicy.Producer.MaximumBytes, MaximumEntries: filePolicy.Producer.MaximumEntries, Checkpoint: strecovery.NativeFinalityCheckpoint{Schema: strecovery.NativeFinalityCheckpointSchema, CodecProfile: strecovery.NativeFinalityCodecProfile, Genesis: source.policy.Network.GenesisHash, HeaderScale: job.ParentHeaderHex, SetId: 9, LiveState: "live", Authorities: []strecovery.GrandpaAuthority{{PublicKey: nativeExecutionTestHex(consensus.Public().(ed25519.PublicKey)), Weight: 1}}}, Providers: []nativeProducerProvider{{Hotkey: nativeExecutionTestHex(bytes.Repeat([]byte{0x11}, 32)), Coldkey: nativeExecutionTestHex(bytes.Repeat([]byte{0x33}, 32))}}}
	authority.Principal = filePolicy.Principal
	authority.Treasury = treasury
	if treasury != nil {
		authority.Schema = nativeTreasuryProducerAuthoritySchema
		authority.Providers = []nativeProducerProvider{}
	}
	authority.Yuma = filePolicy.Yuma
	authority.FeeCensus = filePolicy.FeeCensus
	authority.MaximumDescendantHeaders = filePolicy.Producer.MaximumDescendantHeaders
	message, err := authority.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	authority.Signature = hex.EncodeToString(ed25519.Sign(approval, message))
	authorityRaw, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	authorityRaw = append(authorityRaw, '\n')
	authorityPath := filepath.Join(filepath.Dir(filePolicy.Directory), "producer-authority.json")
	if err := os.WriteFile(authorityPath, authorityRaw, 0600); err != nil {
		t.Fatal(err)
	}
	filePolicy.Producer.Authority = planFileReference{Path: authorityPath, Sha256: monitorReadDigest(authorityRaw)}
	if _, err := loadNativeProducerAuthority(ctx, source.policy); err != nil {
		t.Fatal("complete actual public fixture did not pass authority admission", err)
	}
	fixture := &nativeProducerPublicFixture{ctx: ctx, source: source, authority: authority}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024))
		if err != nil {
			t.Error(err)
			return
		}
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			t.Error(err)
			return
		}
		fixture.requests.Add(1)
		if call.Method == "chain_getFinalizedHead" && fixture.failClosing.Load() {
			completed := filepath.Join(filePolicy.Directory, fmt.Sprintf("b%010d-%s", 101, strings.TrimPrefix(nativeExecutionTestHex(job.ChildHash[:]), "0x")), "complete.json")
			if _, err := os.Stat(completed); err == nil && fixture.failClosing.CompareAndSwap(true, false) {
				fixture.cancelAfterCompleted()
				http.Error(writer, "closing observation canceled after durable completion", http.StatusServiceUnavailable)
				return
			}
		}
		respond := func(result any) {
			if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}); err != nil {
				t.Error(err)
			}
		}
		if call.Method == "system_version" {
			respond("synthetic-node")
			return
		}
		if call.Method == "state_getReadProof" {
			var at string
			if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &at) != nil || proofs[at] == nil {
				t.Error("public producer requested a later or unrelated trie")
				return
			}
			fixture.proofs.Add(1)
			respond(map[string]any{"at": at, "proof": proofs[at]})
			return
		}
		if call.Method == "chain_getBlock" {
			fixture.blocks.Add(1)
			var hash string
			if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &hash) != nil {
				t.Error("public producer changed original execution body")
				return
			}
			selected, exists := jobs[hash]
			if !exists {
				t.Error("public producer requested an absent block")
				return
			}
			var justifications any
			if certificate, exists := certificates[hash]; exists {
				numbers := make([]uint16, len(certificate))
				for index, value := range certificate {
					numbers[index] = uint16(value)
				}
				if fixture.corruptCertificate.Load() {
					numbers[81] ^= 1
				}
				justifications = []any{[]any{[]uint16{'F', 'R', 'N', 'K'}, numbers}}
			}
			respond(map[string]any{"block": map[string]any{"header": source.chain.headers[hash], "extrinsics": selected.ExtrinsicsHex}, "justifications": justifications})
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		buffer := httptest.NewRecorder()
		source.chain.serve(buffer, request)
		var result map[string]json.RawMessage
		if err := json.Unmarshal(buffer.Body.Bytes(), &result); err != nil {
			t.Error(err)
			return
		}
		result["id"] = call.Id
		if err := json.NewEncoder(writer).Encode(result); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	source.client, err = newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.client.httpClient.CloseIdleConnections)
	policyRaw, err := json.Marshal(source.policy)
	if err != nil {
		t.Fatal(err)
	}
	fixture.policy = filepath.Join(filepath.Dir(filePolicy.Directory), "policy.json")
	if err := os.WriteFile(fixture.policy, policyRaw, 0600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// A complete contradictory certificate stops only its native owner. A healthy
// validator retains its separate checkpoint, publisher and observation loop.
func TestNativeProducerPublicForgeryQuarantinesOnlyNativeOwner(t *testing.T) {
	producer := newNativeProducerPublicFixture(t)
	producer.corruptCertificate.Store(true)
	fixture := &monitorEconomicTestFixture{
		source: producer.source, services: newMonitorServicesFixture(t, "validator-a"), url: producer.source.client.url,
		policy: monitorEconomicNativePolicy{Role: "native-a", Observation: producer.source.policy, BatchBlocks: 1, HistoryEntries: 32, StallSeconds: 60, HistoricalFinality: "owned-rpc-assertion"},
	}
	fixture.prepare(t)
	// Both original roots retain their inode/generation. The joined fixture
	// declaration grants no extra production path or enrollment authority.
	fixture.ctx = durablefixture.New(t, t.Context(), fixture.services.directory, producer.source.policy.Execution.Directory).Context
	terminal := make(chan int, 1)
	run := fixture.start(t, monitorServiceHooks{afterWorker: func(role string, code int) {
		if role == fixture.policy.Role {
			terminal <- code
		}
	}})
	var event monitorEconomicTestEvent
	select {
	case event = <-run.sink.economics:
	case <-run.done:
		t.Fatal("public monitor ended before native refusal", run.exit, run.diagnostic.String())
	case <-time.After(300 * time.Second):
		t.Fatal("bounded native finality refusal did not complete")
	}
	if event.Status != "identity-conflict" || event.Current || event.State.Cursor != producer.source.policy.From || event.State.BatchCount != 0 || event.State.NativeMinerAllocationAlpha != nil || !strings.Contains(event.Issue, strecovery.ErrNativeFinalityConflict.Error()) || producer.proofs.Load() != 0 {
		t.Fatal("forged certificate retried as observation or produced a native amount", event)
	}
	select {
	case code := <-terminal:
		if code != 3 {
			t.Fatal("native conflict lost its quarantine result", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("native conflict did not release its worker")
	}
	for sample := 0; sample < 2; sample++ {
		if sample != 0 {
			run.peerResume <- struct{}{}
		}
		select {
		case peer := <-run.sink.peers:
			if peer.Role != "validator-a" {
				t.Fatal("unexpected independent peer", peer.Role)
			}
		case <-run.done:
			t.Fatal("native finality conflict canceled the healthy peer")
		case <-time.After(10 * time.Second):
			t.Fatal("healthy peer did not publish after native quarantine")
		}
	}
	root := producer.source.policy.Execution.Directory
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "b0000000101-") {
			continue
		}
		for _, name := range []string{"job.json", "complete.json", filepath.Join("finality", "native-proof.json")} {
			if _, err := os.Stat(filepath.Join(root, entry.Name(), name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("conflict published downstream execution custody", name, err)
			}
		}
	}
	run.stop(t)
	if run.exit != 3 {
		t.Fatal("aggregate status forgot the quarantined native owner", run.exit)
	}
}

func (self *nativeProducerPublicFixture) command(t *testing.T) (economicEmissionObservation, int, string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	code := runMain(self.ctx, []string{"observe-native-miner-emission", "--policy", self.policy, "--rpc", self.source.client.url, "--retry-window", "300s"}, &output, &diagnostic)
	var observation economicEmissionObservation
	if output.Len() != 0 {
		if err := decodePlanJson(output.Bytes(), &observation); err != nil {
			t.Fatal(err)
		}
	}
	return observation, code, diagnostic.String()
}

func TestNativeProducerPublicOriginalCaptureAndUnacknowledgedRestart(t *testing.T) {
	f := newNativeProducerPublicFixture(t)
	base := f.ctx
	f.ctx, f.cancelAfterCompleted = context.WithCancel(base)
	defer f.cancelAfterCompleted()
	f.failClosing.Store(true)
	partial, code, issue := f.command(t)
	if code == 0 || partial.Complete || partial.ExecutionProducer != nil || f.failClosing.Load() || len(partial.Blocks) != 1 || partial.Blocks[0].ExecutionOutcome == nil {
		t.Fatal("closing-read control did not preserve a completed, unacknowledged original job", code, issue)
	}
	root := f.source.policy.Execution.Directory
	before := mainnetNamespaceTest(t, root)
	proofs, blocks := f.proofs.Load(), f.blocks.Load()
	// The archive advances after the original job completes, before its outer
	// accounting checkpoint. Recovery must reuse the original certified proof.
	header, hash := rootReceiptHeaderFixture(t, f.source.policy.Through.Hash, 102, nil, false)
	f.source.chain.stateLock.Lock()
	f.source.chain.headers[hash], f.source.chain.byHeight[102], f.source.chain.finalized = header, hash, hash
	f.source.chain.stateLock.Unlock()
	f.ctx = base
	first, code, issue := f.command(t)
	if code != 0 || !first.Complete || first.ExecutionProducer == nil || first.ExecutionProducer.Completed != 1 || first.ExecutionProducer.Cursor != f.source.policy.Through || first.ExecutionWindow == nil || first.NativeMinerAllocationAlpha == nil || *first.NativeMinerAllocationAlpha != "100" || *first.ProviderEntitlementAlpha != "9" || *first.OwnerRecycledAlpha != "89" {
		t.Fatal("actual public producer did not reach original execution/accounting", code, issue, first.ExecutionProducer, first.ExecutionWindow)
	}
	if f.proofs.Load() == 0 || first.ActualNativeOutcomeVerified || first.ActivationReady || first.RuntimeSourceProven || first.TargetMet != nil {
		t.Fatal("original execution feed was bypassed or component facts became live authority")
	}
	if f.proofs.Load() != proofs || f.blocks.Load()-blocks != 1 || !reflect.DeepEqual(partial.Blocks[0].ExecutionOutcome, first.Blocks[0].ExecutionOutcome) {
		t.Fatal("unacknowledged restart recaptured a certificate/input/proof or changed original outcome", f.blocks.Load()-blocks)
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, root)) {
		t.Fatal("restart replaced retained job/certificate/node/completion custody")
	}
	if _, err := os.Stat(filepath.Join(root, strings.TrimPrefix(f.source.policy.Through.Hash, "0x")+".json")); !os.IsNotExist(err) {
		t.Fatal("continuous producer created a per-block approval", err)
	}
	// A later observer cannot reinterpret a missing acknowledged job as a new
	// capture. The positive baseline established every required input first.
	completionRaw, err := os.ReadFile(first.ExecutionProducer.Completion.Path)
	if err != nil {
		t.Fatal(err)
	}
	var completion nativeProducerCompletion
	if err := decodePlanJson(completionRaw, &completion); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(completion.Admission.Job.Path, completion.Admission.Job.Path+".held"); err != nil {
		t.Fatal(err)
	}
	refused, code, issue := f.command(t)
	if code == 0 || refused.Complete || refused.ExecutionProducer != nil || f.proofs.Load() != proofs || !strings.Contains(issue, "no such file") {
		t.Fatal("missing original job acquired another capture or accounted cursor", code, issue)
	}
	if err := os.Rename(completion.Admission.Job.Path+".held", completion.Admission.Job.Path); err != nil {
		t.Fatal(err)
	}
}

func TestNativeProducerPublicRestartConsumesCertifiedDescendantsBeforeHandoff(t *testing.T) {
	f := nativeProducerPublicFixtureFrom(t, true)
	base := f.ctx
	var retained *nativeExecutionProducerState
	var window planFileReference
	originals := map[string][]byte{}
	previousChain := f.source.policy.Execution.Producer.Authority.Sha256
	for number := uint64(101); number <= 105; number++ {
		if retained != nil {
			// The worker normally supplies this exact acknowledged state together
			// with its economic cursor. Each command below is a fresh owner.
			f.ctx = context.WithValue(base, nativeProducerStateKey{}, retained)
			f.source.policy.From = retained.Cursor
			f.source.policy.Through = economicEmissionBoundary{Number: number, Hash: f.source.chain.byHeight[number]}
			raw, err := json.Marshal(f.source.policy)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.policy, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		observation, code, issue := f.command(t)
		if code != 0 || !observation.Complete || len(observation.Blocks) != 1 || observation.ExecutionProducer == nil || observation.ExecutionWindow == nil {
			t.Fatal("actual producer restart omitted an economic child", number, code, issue)
		}
		state := observation.ExecutionProducer
		if state.Cursor != f.source.policy.Through || state.Completed != number-100 || state.Completion == nil || state.Window == nil || state.Certified == nil || state.AuthorityHash != f.source.policy.Execution.Producer.Authority.Sha256 {
			t.Fatal("producer skipped a child or reset original authority", number, state)
		}
		anchor, err := strecovery.NativeExecutionCheckpointIdentity(base, f.source.policy.Network.GenesisHash, &state.Anchor)
		if err != nil {
			t.Fatal(err)
		}
		if number <= 104 {
			if anchor.Number != 100 || state.Anchor.SetId != 9 || state.Certified.Number != 104 || state.Certified.Hash != f.source.chain.byHeight[104] {
				t.Fatal("later certified authority skipped an unconsumed child", number, state)
			}
			if number == 101 {
				window = *state.Window
			} else if *state.Window != window {
				t.Fatal("intervening child replaced original certified proof window", number)
			}
		} else if anchor.Number != 104 || anchor.Hash != f.source.chain.byHeight[104] || state.Anchor.SetId != 10 || state.Certified.Number != 105 || len(state.Anchor.Authorities) != 1 || state.Anchor.Authorities[0].PublicKey == f.authority.Checkpoint.Authorities[0].PublicKey {
			t.Fatal("fully consumed window did not hand off exact certified authority", state)
		}
		want := "0"
		if number == 101 {
			want = "100"
		}
		if observation.ExecutionWindow.MinerAllocation != want || (number > 101 && (len(observation.Blocks[0].Events) != 0 || observation.ExecutionWindow.ProviderEntitlement != "0" || observation.ExecutionWindow.OwnerRecycled != "0")) {
			t.Fatal("ordinary empty block re-accounted earlier native amounts", number, observation.ExecutionWindow)
		}
		raw, err := os.ReadFile(state.Completion.Path)
		if err != nil {
			t.Fatal(err)
		}
		var completion nativeProducerCompletion
		if err := decodePlanJson(raw, &completion); err != nil || completion.Previous != previousChain || completion.Sequence != number-100 || completion.Admission.Parent.Number+1 != completion.Admission.Child.Number || completion.Admission.Child != state.Cursor || completion.Admission.Signature != "" {
			t.Fatal("original completion chain or unsigned derived admission changed", number, err)
		}
		originals[state.Completion.Path] = append([]byte(nil), raw...)
		for path, expected := range originals {
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, expected) {
				t.Fatal("later handoff rewrote original completed evidence", path, err)
			}
		}
		previousChain, retained = state.CompletionChain, state
	}
}
