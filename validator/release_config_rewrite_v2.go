package validator

// The configuration file is the operator's document. Activation rewrites only
// the evidence_v2.operators sequence, node by node, so every other key, its
// order and its comments survive, and the result is admitted by the strict
// loader before it replaces the original.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// yamlMappingValue returns the value node of key within a mapping node.
func yamlMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

// RenderReleaseConfigEvidenceV2Operators returns the configuration bytes with
// evidence_v2.operators replaced by the rendered entries. It never writes.
func RenderReleaseConfigEvidenceV2Operators(original []byte, operators []ReleaseEvidenceV2OperatorConfig) ([]byte, error) {
	if len(operators) == 0 {
		return nil, errors.New("rendered evidence operators are empty")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(original, &document); err != nil {
		return nil, err
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("configuration is not one YAML mapping document")
	}
	evidence := yamlMappingValue(document.Content[0], "evidence_v2")
	if evidence == nil || evidence.Kind != yaml.MappingNode {
		return nil, errors.New("configuration has no evidence_v2 mapping")
	}
	var rendered yaml.Node
	if err := rendered.Encode(operators); err != nil {
		return nil, err
	}
	if existing := yamlMappingValue(evidence, "operators"); existing != nil {
		*existing = rendered
	} else {
		evidence.Content = append(evidence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "operators"}, &rendered)
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// RewriteReleaseConfigEvidenceV2Operators pins the rendered operator entries in
// the configuration file. The original is kept once as <path>.pre-activation,
// and the replacement is strictly loaded before it is renamed into place.
func RewriteReleaseConfigEvidenceV2Operators(path string, operators []ReleaseEvidenceV2OperatorConfig) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("configuration %s is not a regular file", abs)
	}
	original, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	rendered, err := RenderReleaseConfigEvidenceV2Operators(original, operators)
	if err != nil {
		return err
	}
	if _, err := decodeReleaseConfigBytes(abs, rendered); err != nil {
		return fmt.Errorf("rewritten configuration is not admitted by the strict loader: %w", err)
	}
	backup := abs + ".pre-activation"
	if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(backup, original, info.Mode().Perm()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	temporary := abs + ".activation-tmp"
	if err := os.WriteFile(temporary, rendered, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(temporary, abs)
}
