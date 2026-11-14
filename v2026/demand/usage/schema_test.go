package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// jsonNames lists the names a struct's fields take in JSON, in field order, which is
// the order the statement's signature covers them in. An embedded struct's fields
// take its place when expand is set, as a version 2 statement writes its extender
// pool, and are left out otherwise, as a version 1 statement writes none.
func jsonNames(t reflect.Type, expand bool) []string {
	var names []string
	for i := range t.NumField() {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Anonymous {
			if expand {
				names = append(names, jsonNames(field.Type.Elem(), false)...)
			}
			continue
		}
		if !field.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		names = append(names, name)
	}
	return names
}

// objectKeys lists a JSON object's keys in the order they are written.
func objectKeys(raw []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var keys []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("an object key is not a string")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// The published schemas, docs/spec/epoch-usage-v1.schema.json and
// epoch-usage-v2.schema.json, must name a statement's fields in the order Statement
// writes them, and require exactly the fields every sealed statement of their
// version writes.
func TestSchemaMatchesStatement(t *testing.T) {
	t.Run("v1", func(t *testing.T) { checkSchemaMatchesStatement(t, "epoch-usage-v1.schema.json", nil) })
	t.Run("v2", func(t *testing.T) {
		checkSchemaMatchesStatement(t, "epoch-usage-v2.schema.json", &ExtenderPool{ExtenderCensus: []string{}, ExtenderChunks: []string{}})
	})
}

// Checks that the JSON schema docs/spec/file refuses the fields Decode
// refuses and describes exactly the fields of a statement with pool, nil for
// version 1.
func checkSchemaMatchesStatement(t *testing.T, file string, pool *ExtenderPool) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec", file))
	if err != nil {
		t.Fatal(err)
	}
	type object struct {
		AdditionalProperties *bool           `json:"additionalProperties"`
		Required             []string        `json:"required"`
		Properties           json.RawMessage `json:"properties"`
		Pattern              string          `json:"pattern"`
	}
	var schema struct {
		object
		Definitions map[string]object `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	var properties map[string]struct {
		Const   string `json:"const"`
		Ref     string `json:"$ref"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(schema.Properties, &properties); err != nil {
		t.Fatal(err)
	}
	block, ok := schema.Definitions["block"]
	if !ok {
		t.Fatal("the schema defines no block")
	}
	// Decode refuses unknown fields at every level.
	for name, part := range map[string]object{"statement": schema.object, "block": block} {
		if part.AdditionalProperties == nil || *part.AdditionalProperties {
			t.Fatalf("the schema's %s allows fields Decode refuses", name)
		}
	}

	names, err := objectKeys(schema.Properties)
	if err != nil {
		t.Fatal(err)
	}
	if want := jsonNames(reflect.TypeFor[Statement](), pool != nil); !slices.Equal(names, want) {
		t.Fatalf("the schema lists %v, but Statement writes %v", names, want)
	}
	wantSchema := Schema
	if pool != nil {
		wantSchema = SchemaV2
	}
	if properties["schema"].Const != wantSchema {
		t.Fatalf("the schema names %q, not %q", properties["schema"].Const, wantSchema)
	}
	blockNames, err := objectKeys(block.Properties)
	if err != nil {
		t.Fatal(err)
	}
	if want := jsonNames(reflect.TypeFor[Boundary](), false); !slices.Equal(blockNames, want) {
		t.Fatalf("the schema's block lists %v, but Boundary writes %v", blockNames, want)
	}
	statementType := reflect.TypeFor[Statement]()
	for i := range statementType.NumField() {
		if field := statementType.Field(i); field.Type == reflect.TypeFor[Boundary]() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if properties[name].Ref != "#/$defs/block" {
				t.Fatalf("%s is a Boundary, but the schema gives it %q", name, properties[name].Ref)
			}
		}
	}

	// Seal a statement with every value it may leave zero at zero: what it still
	// writes is what every published statement carries.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	minimal := &Statement{
		DeploymentID: "d", ChainID: 1, GenesisHash: "g", Netuid: 1,
		Coordinator: common.HexToAddress("0x1"), VaultV2: common.HexToAddress("0x2"), PolicyHash: "p",
		NoID: 1, Start: Boundary{Hash: "a"}, End: Boundary{Number: 1, Hash: "b"}, ExtenderPool: pool,
	}
	if err := minimal.Seal(key); err != nil {
		t.Fatal(err)
	}
	encoded, err := minimal.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	written, err := objectKeys(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if required := slices.Sorted(slices.Values(schema.Required)); !slices.Equal(required, slices.Sorted(slices.Values(written))) {
		t.Fatalf("the schema requires %v, but a sealed statement writes %v", required, written)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	boundaryWritten, err := objectKeys(fields["start"])
	if err != nil {
		t.Fatal(err)
	}
	if required := slices.Sorted(slices.Values(block.Required)); !slices.Equal(required, slices.Sorted(slices.Values(boundaryWritten))) {
		t.Fatalf("the schema's block requires %v, but a boundary writes %v", required, boundaryWritten)
	}

	// The formats this package writes itself: addresses and the signature.
	address := schema.Definitions["address"].Pattern
	for name, pattern := range map[string]string{"coordinator": address, "vault_v2": address, "signer": address, "signature": properties["signature"].Pattern} {
		var value string
		if err := json.Unmarshal(fields[name], &value); err != nil {
			t.Fatal(name, err)
		}
		if pattern == "" || !regexp.MustCompile(pattern).MatchString(value) {
			t.Fatalf("a sealed statement writes %s as %q, which the schema's pattern %q refuses", name, value, pattern)
		}
	}
	if pool == nil {
		return
	}
	// A version 2 statement writes its chunk lists as arrays, [] when empty, never
	// null, and each of its references in the form the schema's chunk pattern takes.
	chunk, ok := schema.Definitions["chunk"]
	if !ok || chunk.Pattern == "" {
		t.Fatal("the v2 schema defines no chunk pattern")
	}
	for _, name := range []string{"extender_census", "extender_chunks"} {
		if string(fields[name]) != "[]" {
			t.Fatalf("an empty %s is written as %s, not []", name, fields[name])
		}
	}
	if !regexp.MustCompile(chunk.Pattern).MatchString("sha256:" + strings.Repeat("ab", 32)) {
		t.Fatalf("the v2 schema's chunk pattern %q refuses a reference", chunk.Pattern)
	}
}
