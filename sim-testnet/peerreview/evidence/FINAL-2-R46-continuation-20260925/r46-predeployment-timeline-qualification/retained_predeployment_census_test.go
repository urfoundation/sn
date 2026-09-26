package main

// Scratch-only census replay consumes copied, hash-pinned diagnostic files. It
// authenticates action admission without a network or deployment write path.
import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every local path is relative to the copied input directory.
type retainedCensusReplayFile struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Bytes  uint64 `json:"bytes"`
}

// The original locator remains the authority for copied artifact bytes.
type retainedCensusReplayArtifact struct {
	File    retainedCensusReplayFile `json:"file"`
	Locator FinalArtifactLocator     `json:"locator"`
}

// The staging script pins one completed diagnostic and only the source files
// required for historical proxy chronology.
type retainedCensusReplayManifest struct {
	Schema         string                         `json:"schema"`
	SourceRevision string                         `json:"source_revision"`
	Report         retainedCensusReplayFile       `json:"report"`
	Artifacts      []retainedCensusReplayArtifact `json:"artifacts"`
}

// Raw check evidence avoids normalizing a sealed JSON number through float64.
type retainedCensusReplayReport struct {
	Schema          string `json:"schema"`
	ReadOnly        bool   `json:"read_only"`
	FinalAcceptance bool   `json:"final_acceptance"`
	Status          string `json:"status"`
	RunId           string `json:"run_id"`
	PlanHash        string `json:"plan_hash"`
	DeploymentId    string `json:"deployment_id"`
	CompletedAt     string `json:"completed_at"`
	Checks          []struct {
		Id       string          `json:"id"`
		Status   string          `json:"status"`
		Evidence json.RawMessage `json:"evidence"`
	} `json:"checks"`
}

// An explicit opt-in keeps an overlay compile check from executing the replay.
func TestRetainedPredeploymentCensus(t *testing.T) {
	manifestPath := os.Getenv("RETAINED_PREDEPLOYMENT_CENSUS_MANIFEST")
	if manifestPath == "" {
		t.Skip("scratch census requires its copied immutable input manifest")
	}
	root := filepath.Dir(manifestPath)
	outputRoot := "/mnt/data/sn-testnet/qualification/r46-predeployment-timeline-evidence-20260926"
	receipt := map[string]any{"schema": "urnetwork-retained-chain-census-replay-v1", "read_only": true, "final_acceptance": false, "rpc_reads": 0, "validator_recaptures": 0, "full_canonical_native_pass": false, "scope": "historical-release-contract-census-only", "started_at": time.Now().UTC().Format(time.RFC3339Nano)}
	runErr := func() error {
		manifestData, err := os.ReadFile(manifestPath)
		if err != nil {
			return err
		}
		var manifest retainedCensusReplayManifest
		if err := decodeStrictJSONBytes(manifestData, &manifest); err != nil {
			return err
		}
		if manifest.Schema != "urnetwork-retained-chain-census-inputs-v1" || len(manifest.SourceRevision) != 40 {
			return errors.New("scratch input manifest identity is incomplete")
		}
		receipt["source_revision"], receipt["manifest_sha256"], receipt["diagnostic_report_sha256"] = manifest.SourceRevision, bytesSHA256(manifestData), manifest.Report.Sha256
		read := func(file retainedCensusReplayFile) ([]byte, error) {
			if filepath.IsAbs(file.Path) || filepath.ToSlash(filepath.Clean(file.Path)) != file.Path {
				return nil, errors.New("copied input path is not canonical")
			}
			path := filepath.Join(root, filepath.FromSlash(file.Path))
			if !pathWithinRoot(root, path) {
				return nil, errors.New("copied input path escapes its directory")
			}
			if err := rejectFinalArtifactSymlinkComponents(root, path); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if uint64(len(data)) != file.Bytes || bytesSHA256(data) != file.Sha256 {
				return nil, fmt.Errorf("copied input changed: %s", file.Path)
			}
			return data, nil
		}
		reportData, err := read(manifest.Report)
		if err != nil {
			return err
		}
		var report retainedCensusReplayReport
		if err := json.Unmarshal(reportData, &report); err != nil {
			return err
		}
		if report.Schema != terminalDiagnosticSchema || !report.ReadOnly || report.FinalAcceptance || report.CompletedAt == "" || (report.Status != "complete" && report.Status != "complete-with-findings") {
			return errors.New("diagnostic report is not sealed")
		}
		checkEvidence := make(map[string]json.RawMessage)
		for _, check := range report.Checks {
			if _, duplicate := checkEvidence[check.Id]; duplicate {
				return errors.New("diagnostic duplicates a check")
			}
			if check.Status == "pass" {
				checkEvidence[check.Id] = check.Evidence
			}
		}
		for _, id := range []string{"closed-foundation-receipts-and-topology", "original-scenario-result"} {
			if len(checkEvidence[id]) == 0 {
				return fmt.Errorf("required sealed diagnostic check did not pass: %s", id)
			}
		}
		var closed struct {
			Bundles  []FinalArtifactLocator `json:"bundles"`
			Result   FinalArtifactLocator   `json:"result"`
			Terminal FinalArtifactLocator   `json:"terminal"`
			History  FinalArtifactLocator   `json:"history"`
		}
		if err := decodeStrictJSONBytes(checkEvidence["closed-foundation-receipts-and-topology"], &closed); err != nil {
			return err
		}
		artifactData := make(map[string][]byte)
		artifactLocators := make(map[string]FinalArtifactLocator)
		for _, artifact := range manifest.Artifacts {
			if _, duplicate := artifactLocators[artifact.Locator.URI]; duplicate {
				return errors.New("copied artifact locator is duplicated")
			}
			data, err := read(artifact.File)
			if err != nil {
				return err
			}
			if artifact.Locator.ContentHash != bytesSHA256(data) || artifact.Locator.SizeBytes != uint64(len(data)) {
				return errors.New("copied artifact differs from its original locator")
			}
			artifactData[artifact.Locator.URI], artifactLocators[artifact.Locator.URI] = data, artifact.Locator
		}
		used := make(map[string]bool)
		load := func(locator FinalArtifactLocator) ([]byte, error) {
			if artifactLocators[locator.URI] != locator {
				return nil, fmt.Errorf("sealed artifact not copied exactly: %s", locator.URI)
			}
			used[locator.URI] = true
			return artifactData[locator.URI], nil
		}
		resultData, err := load(closed.Result)
		if err != nil {
			return err
		}
		var result, originalResult ScenarioResult
		if err := decodeStrictJSONBytes(resultData, &result); err != nil {
			return err
		}
		if err := decodeStrictJSONBytes(checkEvidence["original-scenario-result"], &originalResult); err != nil {
			return err
		}
		resultHash, err := canonicalScenarioResultHash(&result)
		if err != nil || resultHash != result.EvidenceHash || !finalJSONEqual(result, originalResult) || result.RunID != report.RunId || result.DeploymentID != report.DeploymentId {
			return errors.Join(errors.New("copied result differs from the original sealed result"), err)
		}
		receipt["result_evidence_hash"] = resultHash
		terminalData, err := load(closed.Terminal)
		if err != nil {
			return err
		}
		var terminal ScenarioObservation
		if err := decodeStrictJSONBytes(terminalData, &terminal); err != nil {
			return err
		}
		if terminal.Status == nil || terminal.Status.Contracts == nil || terminal.Status.Contracts.Deployment == nil {
			return errors.New("copied terminal deployment is absent")
		}
		files := make(map[string][]byte)
		for _, locator := range closed.Bundles {
			name := filepath.Base(locator.URI)
			if !strings.HasPrefix(name, "launch-foundation") && !strings.HasPrefix(name, "plan-history") && !strings.HasPrefix(name, "public") {
				continue
			}
			data, err := load(locator)
			if err != nil {
				return err
			}
			bundle, err := decodeFinalCollectedFileBundle(data)
			if err != nil {
				return err
			}
			class := finalSemanticBundleClass(bundle.Name)
			if class != "launch-foundation" && class != "plan-history" && class != "public" {
				return errors.New("copied bundle has an unexpected class")
			}
			for _, entry := range bundle.Files {
				name := class + "/" + entry.Path
				if _, duplicate := files[name]; duplicate {
					return errors.New("copied source file is duplicated")
				}
				files[name] = entry.Data
			}
		}
		current, err := decodeFinalHistoricalPlanBytes(files["launch-foundation/plan.json"])
		if err != nil {
			return err
		}
		if current.PlanHash != report.PlanHash || current.DeploymentID != result.DeploymentID || current.ChainID != result.ChainID || current.Netuid != result.Netuid {
			return errors.New("copied current plan differs from sealed campaign")
		}
		plans := map[string]*SetupPlan{current.PlanHash: current}
		names, err := finalHistoricalCoordinatorPlanHistoryNames(files)
		if err != nil {
			return err
		}
		for _, name := range names {
			plan, err := decodeFinalHistoricalPlanBytes(files[name])
			if err != nil {
				return err
			}
			if name != "plan-history/"+stringsTrim0x(plan.PlanHash)+".json" || !current.allowedPlanHashes()[plan.PlanHash] || plan.DeploymentID != current.DeploymentID || plan.ChainID != current.ChainID || plan.Netuid != current.Netuid {
				return errors.New("copied predecessor differs from approved lineage")
			}
			if plans[plan.PlanHash] != nil {
				return errors.New("copied predecessor is duplicated")
			}
			plans[plan.PlanHash] = plan
		}
		if len(plans) != len(current.PriorPlanHashes)+1 {
			return errors.New("copied approved plan lineage is incomplete")
		}
		for _, hash := range current.PriorPlanHashes {
			if plans[hash] == nil {
				return errors.New("copied approved predecessor is missing")
			}
		}
		entries, err := decodeFinalSemanticJournalBytes(files["launch-foundation/journal.jsonl"])
		if err != nil {
			return err
		}
		sources, err := finalHistoricalJournalSourcesFromFiles(current, plans, entries, files)
		if err != nil {
			return err
		}
		proxies, err := finalHistoricalCoordinatorProxyCensus(current, plans, entries)
		if err != nil {
			return err
		}
		if len(proxies) != 2 {
			return fmt.Errorf("retained proxy census has %d proxies, want 2", len(proxies))
		}
		zeroPlanHashes := []string{
			"0x3e185d04085070d8b5a510ca8bbbfe412af9089030ec386dbd5a5fe1903b7578",
			"0x6c38dec8760ff0515861f21aecad751318714ce6b146bf8e06112011ce713565",
			"0xd5f67a4c0bb5957285f67809cca1634256a8ea27be00286056c6c84ae7f42b5e",
			"0xf8c4aae726d757f68994898e0185b5c080f4c1db651837db652ce80579d76244",
		}
		for _, hash := range zeroPlanHashes {
			plan := plans[hash]
			if plan == nil || !finalJSONEqual(plan.Deployment, ContractDeployment{}) {
				return fmt.Errorf("retained predeployment plan %s is not the exact zero deployment", hash)
			}
		}
		receipt["zero_proxy_plan_hashes"], receipt["deployed_proxy_count"] = zeroPlanHashes, len(proxies)
		var deployment ContractDeployment
		if err := decodeStrictJSONBytes(files["public/contracts.json"], &deployment); err != nil {
			return err
		}
		evmHead := terminal.Status.Contracts.FinalizedHead
		if deployment.DeploymentID != current.DeploymentID || deployment.CoordinatorProxy != current.Deployment.CoordinatorProxy || evmHead != result.EndHead || deployment.DeployBlock == 0 || evmHead.Number < deployment.DeployBlock {
			return errors.New("copied deployment differs from the sealed terminal range")
		}
		batcher, _, err := finalPlanFleetBatcher(current)
		if err != nil {
			return err
		}
		census, err := finalCaptureReleaseContractCensusWithSources(current, &deployment, batcher, plans, entries, sources)
		if err != nil {
			return err
		}
		if len(used) != len(artifactLocators) {
			return errors.New("copied census input manifest contains unused artifacts")
		}
		receipt["plan_count"], receipt["journal_entry_count"], receipt["relay_request_count"] = len(plans), len(entries), len(sources.relayRequests)
		value := map[string]any{
			"schema": "urnetwork-retained-release-contract-census-v1", "source_revision": manifest.SourceRevision,
			"diagnostic_report_sha256": manifest.Report.Sha256, "manifest_sha256": bytesSHA256(manifestData),
			"plan_hash": current.PlanHash, "deployment_id": current.DeploymentID, "chain_id": current.ChainID, "netuid": current.Netuid,
			"from_block": census.fromBlock, "current_release_from_block": deployment.DeployBlock,
			"campaign_start_head": result.CampaignStartHead, "terminal_evm_head": evmHead,
			"current_release_addresses": census.currentAddresses, "release_contract_addresses": census.releaseAddresses,
			"read_only": true, "rpc_reads": 0, "validator_recaptures": 0, "final_acceptance": false, "full_canonical_native_pass": false,
		}
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(outputRoot, "retained-replayed-census.json"), data, 0o600); err != nil {
			return err
		}
		receipt["census_sha256"], receipt["from_block"], receipt["current_address_count"], receipt["release_address_count"] = bytesSHA256(data), census.fromBlock, len(census.currentAddresses), len(census.releaseAddresses)
		receipt["input_artifacts"] = manifest.Artifacts
		return nil
	}()
	receipt["completed_at"], receipt["status"] = time.Now().UTC().Format(time.RFC3339Nano), "pass"
	if runErr != nil {
		receipt["status"], receipt["error"] = "fail", runErr.Error()
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "retained-census-receipt.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
}
