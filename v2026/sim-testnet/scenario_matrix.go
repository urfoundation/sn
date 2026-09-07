package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ScenarioMatrixRow struct {
	ID             string   `json:"id"`
	Requirement    string   `json:"requirement"`
	LiveMode       string   `json:"live_mode"`
	LiveAssertions []string `json:"live_assertions"`
	FaultPrefixes  []string `json:"fault_prefixes,omitempty"`
	LocalTests     []string `json:"local_tests"`
}

type ScenarioMatrix struct {
	Schema  string              `json:"schema"`
	Release string              `json:"release"`
	Rows    []ScenarioMatrixRow `json:"rows"`
	Hash    string              `json:"-"`
}

func loadScenarioMatrix(snRepo string) (*ScenarioMatrix, error) {
	path := filepath.Join(snRepo, "docs", "spec", "scenario-matrix-v1.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var matrix ScenarioMatrix
	if err := json.Unmarshal(b, &matrix); err != nil {
		return nil, err
	}
	if matrix.Schema != "urnetwork-scenario-matrix-v1" || matrix.Release != "1.0" || len(matrix.Rows) != 20 {
		return nil, errors.New("release scenario matrix must be schema v1/release 1.0 with exactly 20 rows")
	}
	seen := map[string]bool{}
	for _, row := range matrix.Rows {
		if row.ID == "" || seen[row.ID] || row.Requirement == "" || row.LiveMode == "" || len(row.LiveAssertions) == 0 || len(row.LocalTests) == 0 {
			return nil, fmt.Errorf("invalid or duplicate scenario matrix row %q", row.ID)
		}
		seen[row.ID] = true
		for _, value := range append(append([]string{}, row.LiveAssertions...), row.FaultPrefixes...) {
			if value == "" || strings.TrimSpace(value) != value {
				return nil, fmt.Errorf("scenario matrix row %s contains a malformed proof reference", row.ID)
			}
		}
	}
	var canonical any
	if err := json.Unmarshal(b, &canonical); err != nil {
		return nil, err
	}
	matrix.Hash, err = canonicalHashHex(canonical)
	if err != nil {
		return nil, err
	}
	return &matrix, nil
}

func validateScenarioMatrixCoverage(matrix *ScenarioMatrix, checks []scenarioCheck, faults []scenarioFaultSpec) error {
	if matrix == nil {
		return errors.New("scenario matrix is absent")
	}
	checkIDs := map[string]bool{}
	for _, check := range checks {
		checkIDs[check.ID] = true
	}
	faultIDs := make([]string, len(faults))
	for i := range faults {
		faultIDs[i] = "fault_" + faults[i].ID
	}
	for _, row := range matrix.Rows {
		for _, id := range row.LiveAssertions {
			if !checkIDs[id] {
				return fmt.Errorf("scenario matrix row %s references missing live assertion %s", row.ID, id)
			}
		}
		for _, prefix := range row.FaultPrefixes {
			found := false
			for _, id := range faultIDs {
				found = found || strings.HasPrefix(id, prefix)
			}
			if !found {
				return fmt.Errorf("scenario matrix row %s references unscheduled fault prefix %s", row.ID, prefix)
			}
		}
	}
	return nil
}
