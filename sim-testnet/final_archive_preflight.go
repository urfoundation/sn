package main

// final_archive_preflight.go is the launch-time retention gate for the public
// RPCs named by the authenticated deployment manifest. A successful probe is
// evidence only for the tested depth; unavailable/pruned historical state is
// always terminal and is never replaced with a current-state read.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/stabi"
)

const (
	finalArchiveRetentionPreflightSchema = "urnetwork-final-archive-retention-preflight-v1"
	minimumFinalArchiveProbeDepthBlocks  = uint64(2_000)
)

type FinalArchiveProbeResult struct {
	Endpoint             string    `json:"endpoint"`
	FinalizedHead        ChainHead `json:"finalized_head"`
	EarliestRequiredHead ChainHead `json:"earliest_required_head"`
	HistoricalHead       ChainHead `json:"historical_probe_head"`
	RequiredDepthBlocks  uint64    `json:"required_depth_blocks"`
	MetadataHash         string    `json:"metadata_hash,omitempty"`
	EventsHash           string    `json:"events_hash,omitempty"`
	ExactMetadataHash    string    `json:"exact_metadata_hash,omitempty"`
	ExactEventsHash      string    `json:"exact_events_hash,omitempty"`
	GenericStateHash     string    `json:"generic_state_hash,omitempty"`
	ExactStateHash       string    `json:"exact_state_hash,omitempty"`
	DeploymentHead       ChainHead `json:"deployment_head,omitempty"`
	CodeHash             string    `json:"code_hash,omitempty"`
	CallResultHash       string    `json:"call_result_hash,omitempty"`
}

type FinalArchiveRetentionPreflight struct {
	Schema              string                  `json:"schema"`
	GeneratedAt         string                  `json:"generated_at"`
	DeploymentID        string                  `json:"deployment_id"`
	PublicManifestHash  string                  `json:"public_manifest_hash"`
	PlannedSpanBlocks   uint64                  `json:"planned_span_blocks"`
	SafetyMarginBlocks  uint64                  `json:"safety_margin_blocks"`
	RequiredDepthBlocks uint64                  `json:"required_depth_blocks"`
	Substrate           FinalArchiveProbeResult `json:"substrate"`
	EVM                 FinalArchiveProbeResult `json:"evm"`
	Passed              bool                    `json:"passed"`
	EvidenceHash        string                  `json:"evidence_hash"`
}

type finalArchiveProbe interface {
	Substrate(context.Context, string, ChainHead, uint64) (FinalArchiveProbeResult, error)
	EVM(context.Context, string, string, ChainHead, ChainHead, uint64) (FinalArchiveProbeResult, error)
}

// RunFinalArchiveRetentionPreflight probes and persists a generation-unique,
// content-addressed receipt beneath stateDir/receipts. Callers must invoke it
// immediately before topology launch or scenario start and retain its locator.
func RunFinalArchiveRetentionPreflight(ctx context.Context, stateDir string, public *PublicDeploymentManifest, plannedSpanBlocks, safetyMarginBlocks uint64) (*FinalArchiveRetentionPreflight, FinalArtifactLocator, error) {
	probe := liveFinalArchiveProbe{}
	if public != nil {
		probe.genesisHash = public.GenesisHash
		probe.runtimeVersion = runtimeVersionIdentity{
			SpecName: "node-subtensor", SpecVersion: public.RuntimeSpec,
			TransactionVersion: public.TransactionVersion, StateVersion: public.StateVersion,
		}
		probe.runtimeCodeHash = public.RuntimeCodeHash
		probe.runtimeMetadataHash = public.RuntimeMetadataHash
	}
	return runFinalArchiveRetentionPreflight(ctx, stateDir, public, plannedSpanBlocks, safetyMarginBlocks, time.Now, probe)
}

// FinalCompositeArchiveSpan covers the complete two-phase evidence lifetime:
// the five accelerated epochs, a worst-case discarded partial accelerated
// epoch and its finalization, the three production epochs, and a worst-case
// discarded partial production epoch and its finalization. Callers add a
// publication/reviewer safety margin separately.
func FinalCompositeArchiveSpan(cfg *ResolvedConfig) (uint64, error) {
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || cfg.Config.Scenarios.ShortEpochs <= 0 || cfg.Config.Scenarios.ProductionEpochs <= 0 {
		return 0, errors.New("composite archive span configuration is incomplete")
	}
	terms := []uint64{
		uint64(cfg.Config.Scenarios.ShortEpochs), cfg.Policy.Settlement.EpochBlocks,
		cfg.Policy.Settlement.EpochBlocks, cfg.Policy.Settlement.FinalizeOffsetBlocks,
		uint64(cfg.Config.Scenarios.ProductionEpochs), cfg.Policy.ProductionCadence.EpochBlocks,
		cfg.Policy.ProductionCadence.EpochBlocks, cfg.Policy.ProductionCadence.FinalizeOffsetBlocks,
	}
	short, ok := checkedMul(terms[0], terms[1])
	if !ok {
		return 0, errors.New("accelerated archive span overflows uint64")
	}
	production, ok := checkedMul(terms[4], terms[5])
	if !ok {
		return 0, errors.New("production archive span overflows uint64")
	}
	span := short
	for _, term := range []uint64{terms[2], terms[3], production, terms[6], terms[7]} {
		var added bool
		span, added = checkedAdd(span, term)
		if !added {
			return 0, errors.New("composite archive span overflows uint64")
		}
	}
	return span, nil
}

func RunFinalCompositeArchiveRetentionPreflight(ctx context.Context, cfg *ResolvedConfig, stateDir string, public *PublicDeploymentManifest, safetyMarginBlocks uint64) (*FinalArchiveRetentionPreflight, FinalArtifactLocator, error) {
	span, err := FinalCompositeArchiveSpan(cfg)
	if err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	return RunFinalArchiveRetentionPreflight(ctx, stateDir, public, span, safetyMarginBlocks)
}

func runFinalArchiveRetentionPreflight(ctx context.Context, stateDir string, public *PublicDeploymentManifest, plannedSpanBlocks, safetyMarginBlocks uint64, now func() time.Time, probe finalArchiveProbe) (*FinalArchiveRetentionPreflight, FinalArtifactLocator, error) {
	if ctx == nil || public == nil || public.Contracts == nil || probe == nil || now == nil || stateDir == "" {
		return nil, FinalArtifactLocator{}, errors.New("archive-retention preflight input is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	if public.Schema != "urnetwork-sim-public-deployment-v1" || public.DeploymentID == "" || public.Contracts.CoordinatorProxy == ([20]byte{}) {
		return nil, FinalArtifactLocator{}, errors.New("archive-retention preflight public manifest is invalid")
	}
	if err := validatePublishedRuntimeIdentityShape(public); err != nil {
		return nil, FinalArtifactLocator{}, fmt.Errorf("archive-retention preflight runtime identity: %w", err)
	}
	if err := verifyFinalPublicEndpoint("Substrate", public.SubstrateRPC, "wss", "https"); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	if err := verifyFinalPublicEndpoint("EVM", public.EVMRPC, "https", "wss"); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	futureDepth, carry := plannedSpanBlocks, ^uint64(0)-plannedSpanBlocks < safetyMarginBlocks
	if carry {
		return nil, FinalArtifactLocator{}, errors.New("archive-retention probe depth overflows uint64")
	}
	futureDepth += safetyMarginBlocks
	if futureDepth < minimumFinalArchiveProbeDepthBlocks {
		futureDepth = minimumFinalArchiveProbeDepthBlocks
	}
	substrateFloor, evmFloor, err := finalArchiveEvidenceFloors(stateDir, public)
	if err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	deploymentHead := ChainHead{Number: public.Contracts.DeployBlock, Hash: strings.ToLower(public.Contracts.DeployBlockHash)}
	if err := verifyFinalHead("archive-retention deployment head", deploymentHead); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	manifestHash, err := canonicalHashHex(public)
	if err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	substrate, err := probe.Substrate(ctx, public.SubstrateRPC, substrateFloor, futureDepth)
	if err != nil {
		return nil, FinalArtifactLocator{}, fmt.Errorf("public Substrate archive-retention floor %d plus future depth %d: %w", substrateFloor.Number, futureDepth, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	evm, err := probe.EVM(ctx, public.EVMRPC, public.Contracts.CoordinatorProxy.Hex(), evmFloor, deploymentHead, futureDepth)
	if err != nil {
		return nil, FinalArtifactLocator{}, fmt.Errorf("public EVM archive-retention floor %d plus future depth %d: %w", evmFloor.Number, futureDepth, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	required := substrate.RequiredDepthBlocks
	if evm.RequiredDepthBlocks > required {
		required = evm.RequiredDepthBlocks
	}
	result := &FinalArchiveRetentionPreflight{
		Schema: finalArchiveRetentionPreflightSchema, GeneratedAt: now().UTC().Format(time.RFC3339Nano), DeploymentID: public.DeploymentID,
		PublicManifestHash: manifestHash, PlannedSpanBlocks: plannedSpanBlocks, SafetyMarginBlocks: safetyMarginBlocks, RequiredDepthBlocks: required,
		Substrate: substrate, EVM: evm, Passed: true,
	}
	result.EvidenceHash, err = finalArchiveRetentionPreflightHash(result)
	if err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	if err := verifyFinalArchiveRetentionPreflight(result); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	wire, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	wire = append(wire, '\n')
	relative := filepath.ToSlash(filepath.Join("receipts", "archive-retention-preflight-"+strings.TrimPrefix(result.EvidenceHash, "0x")+".json"))
	absolute := filepath.Join(stateDir, filepath.FromSlash(relative))
	if err := ctx.Err(); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	if err := writeImmutableEvidenceArchive(absolute, wire); err != nil {
		return nil, FinalArtifactLocator{}, err
	}
	return result, FinalArtifactLocator{Kind: "archive-retention-preflight", URI: relative, ContentHash: bytesSHA256(wire), SizeBytes: uint64(len(wire))}, nil
}

func verifyFinalArchiveRetentionPreflight(value *FinalArchiveRetentionPreflight) error {
	if value == nil || value.Schema != finalArchiveRetentionPreflightSchema || !value.Passed || value.DeploymentID == "" {
		return errors.New("archive-retention preflight is incomplete")
	}
	generatedAt, err := time.Parse(time.RFC3339Nano, value.GeneratedAt)
	if err != nil || value.GeneratedAt != generatedAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("archive-retention preflight timestamp is invalid")
	}
	if err := requireFinalHex32("archive-retention public manifest hash", value.PublicManifestHash); err != nil {
		return err
	}
	futureDepth, ok := checkedAdd(value.PlannedSpanBlocks, value.SafetyMarginBlocks)
	if !ok {
		return errors.New("archive-retention preflight depth overflows uint64")
	}
	if futureDepth < minimumFinalArchiveProbeDepthBlocks {
		futureDepth = minimumFinalArchiveProbeDepthBlocks
	}
	wantRequiredDepth := uint64(0)
	for name, result := range map[string]FinalArchiveProbeResult{"Substrate": value.Substrate, "EVM": value.EVM} {
		if result.Endpoint == "" || result.EarliestRequiredHead.Number <= futureDepth || result.HistoricalHead.Number != result.EarliestRequiredHead.Number-futureDepth || result.FinalizedHead.Number < result.EarliestRequiredHead.Number || result.FinalizedHead.Number-result.HistoricalHead.Number != result.RequiredDepthBlocks || result.RequiredDepthBlocks < futureDepth {
			return fmt.Errorf("%s archive-retention depth is insufficient", name)
		}
		if result.RequiredDepthBlocks > wantRequiredDepth {
			wantRequiredDepth = result.RequiredDepthBlocks
		}
		if err := verifyFinalHead(name+" finalized head", result.FinalizedHead); err != nil {
			return err
		}
		if err := verifyFinalHead(name+" historical head", result.HistoricalHead); err != nil {
			return err
		}
		if err := verifyFinalHead(name+" earliest required head", result.EarliestRequiredHead); err != nil {
			return err
		}
	}
	if value.RequiredDepthBlocks != wantRequiredDepth {
		return errors.New("archive-retention aggregate depth differs from its chain probes")
	}
	if err := requireFinalSHA256("Substrate metadata hash", value.Substrate.MetadataHash); err != nil {
		return err
	}
	if err := requireFinalSHA256("Substrate events hash", value.Substrate.EventsHash); err != nil {
		return err
	}
	if err := requireFinalSHA256("Substrate exact metadata hash", value.Substrate.ExactMetadataHash); err != nil {
		return err
	}
	if err := requireFinalSHA256("Substrate exact events hash", value.Substrate.ExactEventsHash); err != nil {
		return err
	}
	if err := requireFinalHex32("EVM generic historical state hash", value.EVM.GenericStateHash); err != nil {
		return err
	}
	if err := requireFinalHex32("EVM exact historical state hash", value.EVM.ExactStateHash); err != nil {
		return err
	}
	if err := verifyFinalHead("EVM deployment head", value.EVM.DeploymentHead); err != nil {
		return err
	}
	for label, digest := range map[string]string{"EVM code hash": value.EVM.CodeHash, "EVM call result hash": value.EVM.CallResultHash} {
		if err := requireFinalHex32(label, digest); err != nil {
			return err
		}
	}
	if value.EVM.DeploymentHead.Number < value.EVM.EarliestRequiredHead.Number || value.EVM.DeploymentHead.Number > value.EVM.FinalizedHead.Number {
		return errors.New("EVM archive-retention deployment checkpoint is outside the retained interval")
	}
	wantHash, err := finalArchiveRetentionPreflightHash(value)
	if err != nil {
		return err
	}
	if value.EvidenceHash == "" || value.EvidenceHash != wantHash {
		return errors.New("archive-retention preflight evidence hash differs")
	}
	return nil
}

func finalArchiveRetentionPreflightHash(value *FinalArchiveRetentionPreflight) (string, error) {
	copy := *value
	copy.EvidenceHash = ""
	return canonicalHashHex(copy)
}

// finalArchiveEvidenceFloors finds the oldest exact checkpoint that the
// closed public evidence graph can require. The preflight must preserve that
// already-consumed history plus the entire future campaign; probing only the
// future span gives a false pass for an old deployment.
func finalArchiveEvidenceFloors(stateDir string, public *PublicDeploymentManifest) (ChainHead, ChainHead, error) {
	if stateDir == "" || public == nil || public.Contracts == nil {
		return ChainHead{}, ChainHead{}, errors.New("archive-retention evidence floor inputs are incomplete")
	}
	var substrateFloor, evmFloor ChainHead
	consider := func(current *ChainHead, candidate ChainHead) error {
		if candidate.Number == 0 && candidate.Hash == "" {
			return nil
		}
		candidate.Hash = strings.ToLower(candidate.Hash)
		if err := verifyFinalHead("archive-retention evidence floor", candidate); err != nil {
			return err
		}
		if current.Number == 0 || candidate.Number < current.Number {
			*current = candidate
		} else if candidate.Number == current.Number && !strings.EqualFold(candidate.Hash, current.Hash) {
			return fmt.Errorf("archive-retention evidence floor block %d has conflicting hashes", candidate.Number)
		}
		return nil
	}
	postconditionRoot := filepath.Join(stateDir, "receipts", "postconditions")
	walkErr := filepath.WalkDir(postconditionRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive-retention postcondition path %s is a symlink", path)
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16*1024*1024 {
			return fmt.Errorf("archive-retention postcondition %s has invalid size/type", path)
		}
		wire, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record ActionPostcondition
		if err := decodeStrictJSONBytes(wire, &record); err != nil {
			return fmt.Errorf("decode archive-retention postcondition %s: %w", path, err)
		}
		if record.DeploymentID != public.DeploymentID || record.ActionID == "" || record.IntentHash == "" || !strings.HasPrefix(record.Schema, "urnetwork-sim-action-postcondition-v") {
			return fmt.Errorf("archive-retention postcondition %s has an invalid identity", path)
		}
		if err := consider(&substrateFloor, record.SubstrateFinalized); err != nil {
			return err
		}
		return consider(&evmFloor, record.EVMFinalized)
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return ChainHead{}, ChainHead{}, walkErr
	}
	for name, raw := range public.SetupEvidence {
		nativeHeads, evmHeads, err := finalArchiveSetupEvidenceHeads(name, raw, public.DeploymentID)
		if err != nil {
			return ChainHead{}, ChainHead{}, err
		}
		for _, head := range nativeHeads {
			if err := consider(&substrateFloor, head); err != nil {
				return ChainHead{}, ChainHead{}, err
			}
		}
		for _, head := range evmHeads {
			if err := consider(&evmFloor, head); err != nil {
				return ChainHead{}, ChainHead{}, err
			}
		}
	}
	deployment := ChainHead{Number: public.Contracts.DeployBlock, Hash: strings.ToLower(public.Contracts.DeployBlockHash)}
	if err := consider(&evmFloor, deployment); err != nil {
		return ChainHead{}, ChainHead{}, err
	}
	if substrateFloor.Number == 0 || evmFloor.Number == 0 {
		return ChainHead{}, ChainHead{}, errors.New("archive-retention exact evidence floors are unavailable")
	}
	return substrateFloor, evmFloor, nil
}

// Setup evidence mixes native commitment finality with EVM transaction
// receipts. Their block hashes are different namespaces even on a
// Frontier-style chain, so each authenticated setup class must contribute only
// to its own archive floor.
func finalArchiveSetupEvidenceHeads(name string, raw json.RawMessage, deploymentID string) ([]ChainHead, []ChainHead, error) {
	switch {
	case name == "voluntary_conviction":
		var evidence VoluntaryConvictionEvidence
		if err := decodeStrictJSONBytes(raw, &evidence); err != nil || evidence.Schema != "urnetwork-voluntary-conviction-evidence-v1" || evidence.DeploymentID != deploymentID || evidence.FinalizedBlock == 0 || evidence.FinalizedHash == "" {
			return nil, nil, stateMismatchError(err, "archive-retention voluntary-conviction evidence is invalid")
		}
		return nil, []ChainHead{{Number: evidence.FinalizedBlock, Hash: strings.ToLower(evidence.FinalizedHash)}}, nil
	case strings.HasPrefix(name, "fleet_") && strings.HasSuffix(name, "_commitment"):
		var evidence FleetCommitmentEvidence
		if err := decodeStrictJSONBytes(raw, &evidence); err != nil || evidence.Schema != fleetCommitmentEvidenceSchemaV2 || evidence.FinalizedBlock == 0 || evidence.FinalizedBlockHash == "" {
			return nil, nil, stateMismatchError(err, "archive-retention fleet commitment evidence %s is invalid", name)
		}
		return []ChainHead{{Number: evidence.FinalizedBlock, Hash: strings.ToLower(evidence.FinalizedBlockHash)}}, nil, nil
	case strings.HasPrefix(name, "fleet_") && strings.Contains(name, "_binding_"):
		var evidence FleetBindingEvidence
		if err := decodeStrictJSONBytes(raw, &evidence); err != nil || evidence.Schema != "urnetwork-fleet-binding-evidence-v1" || evidence.BlockNumber == 0 || evidence.BlockHash == "" {
			return nil, nil, stateMismatchError(err, "archive-retention fleet binding evidence %s is invalid", name)
		}
		return nil, []ChainHead{{Number: evidence.BlockNumber, Hash: strings.ToLower(evidence.BlockHash)}}, nil
	case strings.HasPrefix(name, "fleet_") && strings.HasSuffix(name, "_manifest"):
		if !json.Valid(raw) {
			return nil, nil, fmt.Errorf("archive-retention fleet manifest evidence %s is invalid", name)
		}
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("archive-retention public setup evidence class %q is unsupported", name)
	}
}

type liveFinalArchiveProbe struct {
	genesisHash         string
	runtimeVersion      runtimeVersionIdentity
	runtimeCodeHash     string
	runtimeMetadataHash string
}

func (live liveFinalArchiveProbe) Substrate(ctx context.Context, endpoint string, earliest ChainHead, futureDepth uint64) (FinalArchiveProbeResult, error) {
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	defer chain.API.Client.Close()
	var finalizedRaw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &finalizedRaw, "chain_getFinalizedHead"); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	finalizedHash, err := finalDecodeRPCString("chain_getFinalizedHead", finalizedRaw)
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	finalizedNativeHash, err := gsrpctypes.NewHashFromHexString(finalizedHash)
	if err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("decode archive finalized hash: %w", err)
	}
	if !strings.EqualFold(chain.GenesisHash.Hex(), live.genesisHash) {
		return FinalArchiveProbeResult{}, fmt.Errorf("archive genesis %s, want %s", chain.GenesisHash.Hex(), live.genesisHash)
	}
	version, err := runtimeVersionAt(chain, finalizedNativeHash)
	if err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("read archive finalized runtime identity: %w", err)
	}
	if err := validateRuntimeVersionIdentity(version, live.runtimeVersion.SpecVersion, live.runtimeVersion.TransactionVersion, live.runtimeVersion.StateVersion); err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("authenticate archive finalized runtime identity: %w", err)
	}
	codeHash, err := runtimeCodeHashAt(chain, finalizedNativeHash)
	if err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("read archive finalized runtime code hash: %w", err)
	}
	if err := validateRuntimeCodeHash(codeHash, live.runtimeCodeHash); err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("authenticate archive finalized runtime code: %w", err)
	}
	_, metadataHash, err := runtimeMetadataAt(chain, finalizedNativeHash)
	if err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("read archive finalized runtime metadata: %w", err)
	}
	if err := validateRuntimeMetadataHash(metadataHash, live.runtimeMetadataHash); err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("authenticate archive finalized runtime metadata: %w", err)
	}
	var finalizedHeader json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &finalizedHeader, "chain_getHeader", finalizedHash); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	finalizedNumber, _, err := finalSubstrateHeader(finalizedHeader)
	if err != nil || finalizedNumber < earliest.Number || earliest.Number <= futureDepth {
		return FinalArchiveProbeResult{}, fmt.Errorf("finalized head %d / earliest evidence %d cannot retain future depth %d", finalizedNumber, earliest.Number, futureDepth)
	}
	historicalNumber := earliest.Number - futureDepth
	var historicalHashRaw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &historicalHashRaw, "chain_getBlockHash", historicalNumber); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	historicalHash, err := finalDecodeRPCString("chain_getBlockHash", historicalHashRaw)
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	var metadataRaw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &metadataRaw, "state_getMetadata", historicalHash); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	metadataHex, err := finalDecodeRPCString("state_getMetadata", metadataRaw)
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("decode historical metadata: %w", err)
	}
	eventsKey, err := gsrpctypes.CreateStorageKey(metadata, "System", "Events")
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	var eventsRaw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &eventsRaw, "state_getStorage", eventsKey.Hex(), historicalHash); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	var eventsHex *string
	if json.Unmarshal(eventsRaw, &eventsHex) != nil || eventsHex == nil || !strings.HasPrefix(*eventsHex, "0x") {
		return FinalArchiveProbeResult{}, errors.New("historical System.Events storage is unavailable")
	}
	var exactHashRaw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &exactHashRaw, "chain_getBlockHash", earliest.Number); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	exactHash, err := finalDecodeRPCString("chain_getBlockHash", exactHashRaw)
	if err != nil || !strings.EqualFold(exactHash, earliest.Hash) {
		return FinalArchiveProbeResult{}, errors.New("earliest required Substrate checkpoint is not canonical")
	}
	var exactMetadataRaw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &exactMetadataRaw, "state_getMetadata", exactHash); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	exactMetadataHex, err := finalDecodeRPCString("state_getMetadata", exactMetadataRaw)
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	exactMetadata, _, err := crv4.DecodeRuntimeMetadata(exactMetadataHex)
	if err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("decode exact historical metadata: %w", err)
	}
	exactEventsKey, err := gsrpctypes.CreateStorageKey(exactMetadata, "System", "Events")
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	var exactEventsRaw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &exactEventsRaw, "state_getStorage", exactEventsKey.Hex(), exactHash); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	var exactEventsHex *string
	if json.Unmarshal(exactEventsRaw, &exactEventsHex) != nil || exactEventsHex == nil || !strings.HasPrefix(*exactEventsHex, "0x") {
		return FinalArchiveProbeResult{}, errors.New("earliest required Substrate System.Events storage is unavailable")
	}
	return FinalArchiveProbeResult{
		Endpoint: endpoint, FinalizedHead: ChainHead{Number: finalizedNumber, Hash: strings.ToLower(finalizedHash)}, EarliestRequiredHead: earliest, HistoricalHead: ChainHead{Number: historicalNumber, Hash: strings.ToLower(historicalHash)}, RequiredDepthBlocks: finalizedNumber - historicalNumber,
		MetadataHash: bytesSHA256([]byte(metadataHex)), EventsHash: bytesSHA256([]byte(*eventsHex)), ExactMetadataHash: bytesSHA256([]byte(exactMetadataHex)), ExactEventsHash: bytesSHA256([]byte(*exactEventsHex)),
	}, nil
}

func (liveFinalArchiveProbe) EVM(ctx context.Context, endpoint, coordinator string, earliest, deployment ChainHead, futureDepth uint64) (FinalArchiveProbeResult, error) {
	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	defer client.Close()
	var finalizedRaw json.RawMessage
	if err := client.CallContext(ctx, &finalizedRaw, "eth_getBlockByNumber", "finalized", false); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	finalized, err := finalEVMBlock(finalizedRaw)
	if err != nil || finalized.Number < earliest.Number || earliest.Number <= futureDepth || deployment.Number < earliest.Number || deployment.Number > finalized.Number {
		return FinalArchiveProbeResult{}, fmt.Errorf("finalized head %d / earliest evidence %d / deployment %d cannot retain future depth %d", finalized.Number, earliest.Number, deployment.Number, futureDepth)
	}
	historicalNumber := earliest.Number - futureDepth
	var historicalRaw json.RawMessage
	if err := client.CallContext(ctx, &historicalRaw, "eth_getBlockByNumber", hexutil.EncodeUint64(historicalNumber), false); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	historical, err := finalEVMBlock(historicalRaw)
	if err != nil {
		return FinalArchiveProbeResult{}, err
	}
	selector := finalEVMBlockSelector{BlockHash: historical.Hash, RequireCanonical: true}
	var genericBalance string
	if err := client.CallContext(ctx, &genericBalance, "eth_getBalance", "0x0000000000000000000000000000000000000000", selector); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	if _, err := hexutil.DecodeBig(genericBalance); err != nil {
		return FinalArchiveProbeResult{}, errors.New("generic historical EVM account state is unavailable")
	}
	var exactRaw json.RawMessage
	if err := client.CallContext(ctx, &exactRaw, "eth_getBlockByNumber", hexutil.EncodeUint64(earliest.Number), false); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	exact, err := finalEVMBlock(exactRaw)
	if err != nil || exact != earliest {
		return FinalArchiveProbeResult{}, errors.New("earliest required EVM checkpoint is not canonical")
	}
	var exactBalance string
	if err := client.CallContext(ctx, &exactBalance, "eth_getBalance", "0x0000000000000000000000000000000000000000", finalEVMBlockSelector{BlockHash: exact.Hash, RequireCanonical: true}); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	if _, err := hexutil.DecodeBig(exactBalance); err != nil {
		return FinalArchiveProbeResult{}, errors.New("earliest required EVM account state is unavailable")
	}
	var deploymentRaw json.RawMessage
	if err := client.CallContext(ctx, &deploymentRaw, "eth_getBlockByNumber", hexutil.EncodeUint64(deployment.Number), false); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	canonicalDeployment, err := finalEVMBlock(deploymentRaw)
	if err != nil || canonicalDeployment != deployment {
		return FinalArchiveProbeResult{}, errors.New("coordinator deployment checkpoint is not canonical")
	}
	deploymentSelector := finalEVMBlockSelector{BlockHash: deployment.Hash, RequireCanonical: true}
	var codeHex string
	if err := client.CallContext(ctx, &codeHex, "eth_getCode", coordinator, deploymentSelector); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	code, err := hexutil.Decode(codeHex)
	if err != nil || len(code) == 0 {
		return FinalArchiveProbeResult{}, errors.New("historical coordinator code is unavailable")
	}
	var callHex string
	call := struct {
		To   string `json:"to"`
		Data string `json:"data"`
	}{To: coordinator, Data: hexutil.Encode(stabi.NewSTCoordinator().PackOwner())}
	if err := client.CallContext(ctx, &callHex, "eth_call", call, deploymentSelector); err != nil {
		return FinalArchiveProbeResult{}, err
	}
	callResult, err := hexutil.Decode(callHex)
	if err != nil || len(callResult) == 0 {
		return FinalArchiveProbeResult{}, errors.New("historical coordinator call is unavailable")
	}
	if _, err := stabi.NewSTCoordinator().UnpackOwner(callResult); err != nil {
		return FinalArchiveProbeResult{}, fmt.Errorf("decode historical coordinator owner: %w", err)
	}
	return FinalArchiveProbeResult{
		Endpoint: endpoint, FinalizedHead: finalized, EarliestRequiredHead: earliest, HistoricalHead: historical, RequiredDepthBlocks: finalized.Number - historicalNumber,
		GenericStateHash: strings.ToLower(crypto.Keccak256Hash([]byte(genericBalance)).Hex()), ExactStateHash: strings.ToLower(crypto.Keccak256Hash([]byte(exactBalance)).Hex()), DeploymentHead: deployment,
		CodeHash: strings.ToLower(crypto.Keccak256Hash(code).Hex()), CallResultHash: strings.ToLower(crypto.Keccak256Hash(callResult).Hex()),
	}, nil
}
