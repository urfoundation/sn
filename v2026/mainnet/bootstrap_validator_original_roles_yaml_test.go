// Role export is an unsigned proposal. Actual adoption must preserve its wire
// fields and obtain a fresh original config approval before the strict loader.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/validator"
	"gopkg.in/yaml.v3"
)

// The real public exporter supplies these additions. Fresh synthetic signatures
// belong to separate files, so the old approval cannot silently authorize them.
func bootstrapValidatorOriginalAdoptExport(t *testing.T) (*bootstrapChainFixture, []*bootstrapChainValidatorFixture, bootstrapValidatorOriginalRoleConfig) {
	t.Helper()
	f, _, requestPath, digest := newBootstrapValidatorOriginalRoleFixture(t)
	originalBytes := map[string][]byte{}
	for _, original := range f.validators {
		for _, path := range []string{original.path, original.config.OwnerRecycleApproval.Approval.Path} {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			originalBytes[path] = raw
		}
	}
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapValidatorOriginalRoleArgs(f, requestPath, digest), &stdout, &stderr); code != 0 {
		t.Fatal("public original role export failed before adoption", code, stderr.String())
	}
	var exported bootstrapValidatorOriginalRoleConfig
	if err := decodePlanJson(stdout.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if !exported.RequiresOwnerSigningAndAdoption || len(exported.Validators) != len(f.validators) {
		t.Fatal("public export lost explicit adoption or complete validator census")
	}
	var adopted []*bootstrapChainValidatorFixture
	for index, patch := range exported.Validators {
		original := f.validators[index]
		candidate := *original
		candidate.path = filepath.Join(filepath.Dir(original.path), fmt.Sprintf("adopted-validator-%d.yml", index))
		candidate.config.Operators = append([]validator.OperatorConfig(nil), patch.RoleConfig.Operators...)
		approvalSelection := *original.config.OwnerRecycleApproval
		candidate.config.OwnerRecycleApproval = &approvalSelection
		candidate.writeConfig(t)
		if _, err := validator.LoadReleaseConfig(candidate.path); err == nil || !strings.Contains(err.Error(), "approval names a different complete configuration") {
			t.Fatal("unsigned exported fields reused the original config approval", err)
		}
		candidate.config.OwnerRecycleApproval.Approval.Path = filepath.Join(filepath.Dir(original.path), fmt.Sprintf("adopted-validator-%d-approval.json", index))
		candidate.publish(t)
		loaded, err := validator.LoadReleaseConfig(candidate.path)
		if err != nil || loaded == nil {
			t.Fatal("freshly approved role YAML failed actual strict production load", err)
		}
		if !reflect.DeepEqual(loaded.Operators, patch.RoleConfig.Operators) {
			t.Fatal("actual role export changed across approved YAML adoption")
		}
		adopted = append(adopted, &candidate)
	}
	for path, original := range originalBytes {
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, original) {
			t.Fatal("separate role adoption overwrote an original signed input", err)
		}
	}
	return f, adopted, exported
}

// The actual producer output, YAML syntax and public strict loader retain every
// nested receipt field and the exact JSON bytes used by the signature domain.
func TestBootstrapValidatorOriginalRoleExportAdoptsCanonicalYaml(t *testing.T) {
	_, adopted, exported := bootstrapValidatorOriginalAdoptExport(t)
	for index, candidate := range adopted {
		raw, err := os.ReadFile(candidate.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"request_receipt_scope:", "genesis_hash:", "deployment_id:", "deployment_key:", "policy_hash:", "no_id:"} {
			if !bytes.Contains(raw, []byte(key)) {
				t.Fatal("exported role YAML omitted its canonical nested key", key)
			}
		}
		loaded, err := validator.LoadReleaseConfig(candidate.path)
		if err != nil {
			t.Fatal("exact adopted role could not reopen", err)
		}
		for operatorIndex, operator := range exported.Validators[index].RoleConfig.Operators {
			before, err := json.Marshal(operator.RequestReceiptScope)
			if err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(loaded.Operators[operatorIndex].RequestReceiptScope)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("signed receipt JSON changed across actual role YAML adoption", err)
			}
		}
	}
}

// Snake-case names are part of the reviewed role wire. Implicit yaml.v3 field
// spellings must fail at decoding, before any approval or producer authority.
func TestBootstrapValidatorOriginalRoleAdoptionRejectsImplicitYamlAliases(t *testing.T) {
	_, adopted, _ := bootstrapValidatorOriginalAdoptExport(t)
	original, err := os.ReadFile(adopted[0].path)
	if err != nil {
		t.Fatal(err)
	}
	mappingValue := func(node *yaml.Node, key string) *yaml.Node {
		if node == nil || node.Kind != yaml.MappingNode {
			t.Fatal("actual role YAML mapping is absent", key)
		}
		for index := 0; index < len(node.Content); index += 2 {
			if node.Content[index].Value == key {
				return node.Content[index+1]
			}
		}
		t.Fatal("actual role YAML key is absent", key)
		return nil
	}
	for _, spelling := range []struct{ canonical, implicit string }{
		{canonical: "genesis_hash:", implicit: "genesishash:"},
		{canonical: "deployment_id:", implicit: "deploymentid:"},
		{canonical: "deployment_key:", implicit: "deploymentkey:"},
		{canonical: "policy_hash:", implicit: "policyhash:"},
		{canonical: "no_id:", implicit: "noid:"},
	} {
		var document yaml.Node
		if err := yaml.Unmarshal(original, &document); err != nil || len(document.Content) != 1 {
			t.Fatal("actual role YAML document", err)
		}
		operators := mappingValue(document.Content[0], "operators")
		if operators.Kind != yaml.SequenceNode || len(operators.Content) == 0 {
			t.Fatal("actual role YAML has no operator sequence")
		}
		for _, operator := range operators.Content {
			scope := mappingValue(operator, "request_receipt_scope")
			found := false
			for index := 0; index < len(scope.Content); index += 2 {
				if scope.Content[index].Value == strings.TrimSuffix(spelling.canonical, ":") {
					scope.Content[index].Value = strings.TrimSuffix(spelling.implicit, ":")
					found = true
				}
			}
			if !found {
				t.Fatal("missing actual exported nested YAML key", spelling.canonical)
			}
		}
		changed, err := yaml.Marshal(&document)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(filepath.Dir(adopted[0].path), "implicit-"+strings.TrimSuffix(spelling.implicit, ":")+".yml")
		if err := os.WriteFile(path, changed, 0600); err != nil {
			t.Fatal(err)
		}
		_, err = validator.LoadReleaseConfig(path)
		if err == nil || !strings.Contains(err.Error(), "field "+strings.TrimSuffix(spelling.implicit, ":")+" not found") {
			t.Fatal("actual strict role loader accepted an implicit nested YAML alias", spelling.implicit, err)
		}
	}
}
