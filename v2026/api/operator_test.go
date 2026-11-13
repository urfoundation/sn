package api

// operator_test.go holds operator.yml to its own contract. The server holds
// its routes to the document (its api/spec_conformance_test.go maps the
// routes operator.yml documents to it, /sn/ctf and /sn/openapi.yml, as it
// maps /competition to competition.yml; the ledger route is listed in
// connect/api/bringyour.yml too); this side keeps the document itself sound
// for the server, the SDK, the apps and the ur.xyz /ctf page that build on
// it:
//
//   - its operations are exactly the pinned list, each with an operationId,
//     a summary, one defined tag and explicit security;
//   - every $ref resolves and every component is used;
//   - every response with a body has an example, and every example is valid
//     against its schema, strictly: a property the schema does not declare
//     is an error, so a misspelled field fails;
//   - the conventions hold: one Access-Control-Allow-Origin on every answer,
//     the service-desc Link, Cache-Control, an ETag with a 304 and
//     Last-Modified on the cacheable answers, problem details for errors,
//     amounts as Amount objects and times in UTC;
//   - the examples' arithmetic is the design's (CAPTURETHEFLAG.md §4.5):
//     alpha and rao agree, slots add up, the chance to win is
//     min(1, 5/entries), the estimates follow from the pots and the slot
//     amount, the pots carry from epoch to epoch, the epoch's amounts
//     balance, and every ss58 address encodes its coldkey;
//   - no default win is claimed without a draw: the draw figures are null
//     together when last epoch had none;
//   - a client answer's wins agree with themselves and its epochs: each
//     amount is its slot and pot share, newest first, a win of a pending
//     root never claimed, and every win of the epochs it lists listed;
//   - the ledger route keeps every field connect/api/bringyour.yml gives it,
//     and no route operator.yml owns is listed there, when the connect
//     checkout is beside this one. Once bringyour.yml describes `kind`, it
//     must describe every field here.
//
// Lint it with redocly too: npx -y @redocly/cli lint api/operator.yml

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/ss58"
)

// The operations operator.yml documents. The server must serve each one, and
// a route under /sn/ctf is documented here and nowhere else.
var operatorOperations = []string{
	"GET /sn/ctf",
	"GET /sn/ctf/client",
	"GET /sn/ctf/country/{cc}",
	"GET /sn/ctf/epoch/{epoch}",
	"GET /sn/ctf/here",
	"GET /sn/ctf/top",
	"GET /sn/ledger/account",
	"GET /sn/openapi.yml",
}

// The only operation that takes a bearer token.
const operatorAuthenticated = "GET /sn/ctf/client"

const ctfSlots = 2490

type operatorDoc struct {
	root map[string]any
}

func loadOperatorDoc(t *testing.T) *operatorDoc {
	t.Helper()
	var root map[string]any
	if err := yaml.Unmarshal(OperatorYml, &root); err != nil {
		t.Fatal(err)
	}
	return &operatorDoc{root: root}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// ref resolves a local reference such as "#/components/schemas/Amount".
func (self *operatorDoc) ref(ref string) (map[string]any, bool) {
	pointer, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil, false
	}
	var node any = self.root
	for _, part := range strings.Split(pointer, "/") {
		next, found := asMap(node)[part]
		if !found {
			return nil, false
		}
		node = next
	}
	m := asMap(node)
	return m, m != nil
}

// deref follows a chain of $ref.
func (self *operatorDoc) deref(m map[string]any) map[string]any {
	for i := 0; i < 16 && m != nil; i++ {
		r, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		m, _ = self.ref(r)
	}
	return m
}

// operations returns "METHOD /path" -> operation.
func (self *operatorDoc) operations() map[string]map[string]any {
	out := map[string]map[string]any{}
	for path, item := range asMap(self.root["paths"]) {
		for method, op := range asMap(item) {
			switch method {
			case "get", "put", "post", "patch", "delete", "head", "options", "trace":
				out[strings.ToUpper(method)+" "+path] = asMap(op)
			}
		}
	}
	return out
}

// mediaExamples returns the example values of a media type object, by name.
func (self *operatorDoc) mediaExamples(media map[string]any) map[string]any {
	out := map[string]any{}
	if example, ok := media["example"]; ok {
		out["example"] = example
	}
	for name, example := range asMap(media["examples"]) {
		out[name] = self.deref(asMap(example))["value"]
	}
	return out
}

// eachResponse calls f for every response of every operation, dereferenced.
func (self *operatorDoc) eachResponse(f func(operation, status string, response map[string]any)) {
	operations := self.operations()
	keys := make([]string, 0, len(operations))
	for key := range operations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for status, response := range asMap(operations[key]["responses"]) {
			f(key, status, self.deref(asMap(response)))
		}
	}
}

func TestOperatorYmlIsTheFile(t *testing.T) {
	raw, err := os.ReadFile("operator.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, OperatorYml) {
		t.Fatal("OperatorYml is not operator.yml")
	}
}

func TestOperatorDocument(t *testing.T) {
	doc := loadOperatorDoc(t)
	if doc.root["openapi"] != "3.1.0" {
		t.Fatalf("openapi is %v, want 3.1.0", doc.root["openapi"])
	}
	info := asMap(doc.root["info"])
	if info["title"] == "" || info["version"] == nil || asMap(info["license"])["url"] == nil {
		t.Fatal("info needs a title, a version and a license url")
	}
	if url := asMap(asSlice(doc.root["servers"])[0])["url"]; url != "https://api.bringyour.com" {
		t.Fatalf("server is %v", url)
	}

	operations := doc.operations()
	var keys []string
	for key := range operations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if !slices.Equal(keys, operatorOperations) {
		t.Fatalf("operations are %v, want %v", keys, operatorOperations)
	}

	tags := map[string]bool{}
	for _, tag := range asSlice(doc.root["tags"]) {
		tags[asMap(tag)["name"].(string)] = true
	}
	operationIds := map[string]string{}
	camel := regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	pathParam := regexp.MustCompile(`\{([^}]+)\}`)
	for _, key := range keys {
		op := operations[key]
		id, _ := op["operationId"].(string)
		if !camel.MatchString(id) {
			t.Errorf("%s: operationId %q is not lowerCamelCase", key, id)
		}
		if other, dup := operationIds[id]; dup {
			t.Errorf("%s: operationId %q is also %s's", key, id, other)
		}
		operationIds[id] = key
		if s, _ := op["summary"].(string); s == "" {
			t.Errorf("%s: no summary", key)
		}
		if s, _ := op["description"].(string); s == "" {
			t.Errorf("%s: no description", key)
		}
		if opTags := asSlice(op["tags"]); len(opTags) != 1 || !tags[opTags[0].(string)] {
			t.Errorf("%s: tags %v, want one of %v", key, opTags, tags)
		}
		security, declared := op["security"]
		switch {
		case !declared:
			t.Errorf("%s: security is not declared", key)
		case key == operatorAuthenticated:
			if len(asSlice(security)) != 1 || asMap(asSlice(security)[0])["BearerAuth"] == nil {
				t.Errorf("%s: security %v, want BearerAuth", key, security)
			}
		case len(asSlice(security)) != 0:
			t.Errorf("%s: a public route declares security %v, want []", key, security)
		}
		if asMap(asMap(op["responses"])["200"]) == nil {
			t.Errorf("%s: no 200", key)
		}
		params := map[string]map[string]any{}
		for _, p := range asSlice(op["parameters"]) {
			p := doc.deref(asMap(p))
			params[p["in"].(string)+":"+p["name"].(string)] = p
		}
		for _, m := range pathParam.FindAllStringSubmatch(key, -1) {
			if p := params["path:"+m[1]]; p == nil || p["required"] != true {
				t.Errorf("%s: path parameter %q is not declared and required", key, m[1])
			}
		}
	}
}

// Every reference resolves, and every component is referenced.
func TestOperatorRefs(t *testing.T) {
	doc := loadOperatorDoc(t)
	used := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch node := node.(type) {
		case map[string]any:
			if r, ok := node["$ref"].(string); ok {
				if _, found := doc.ref(r); !found {
					t.Errorf("unresolved $ref %q", r)
				} else if !used[r] {
					used[r] = true
					target, _ := doc.ref(r)
					walk(target)
				}
			}
			for key, value := range node {
				if key != "$ref" {
					walk(value)
				}
			}
		case []any:
			for _, value := range node {
				walk(value)
			}
		}
	}
	walk(doc.root["paths"])
	for _, op := range doc.operations() {
		for _, requirement := range asSlice(op["security"]) {
			for scheme := range asMap(requirement) {
				used["#/components/securitySchemes/"+scheme] = true
			}
		}
	}
	for section, components := range asMap(doc.root["components"]) {
		for name := range asMap(components) {
			if r := "#/components/" + section + "/" + name; !used[r] {
				t.Errorf("%s is not used", r)
			}
		}
	}
}

func TestOperatorExamples(t *testing.T) {
	doc := loadOperatorDoc(t)
	report := func(where string, problems []string) {
		for _, problem := range problems {
			t.Errorf("%s: %s", where, problem)
		}
	}
	doc.eachResponse(func(operation, status string, response map[string]any) {
		for mediaType, media := range asMap(response["content"]) {
			media := asMap(media)
			examples := doc.mediaExamples(media)
			if len(examples) == 0 {
				t.Errorf("%s %s %s: no example", operation, status, mediaType)
			}
			for name, value := range examples {
				report(fmt.Sprintf("%s %s %s example %s", operation, status, mediaType, name), doc.check(asMap(media["schema"]), value, "$"))
			}
		}
		for name, header := range asMap(response["headers"]) {
			header := doc.deref(asMap(header))
			if example, ok := header["example"]; ok {
				report(fmt.Sprintf("%s %s header %s", operation, status, name), doc.check(asMap(header["schema"]), example, "$"))
			}
		}
	})
	for key, op := range doc.operations() {
		for _, p := range asSlice(op["parameters"]) {
			p := doc.deref(asMap(p))
			example, ok := p["example"]
			if !ok {
				t.Errorf("%s: parameter %v has no example", key, p["name"])
				continue
			}
			report(fmt.Sprintf("%s parameter %v", key, p["name"]), doc.check(asMap(p["schema"]), example, "$"))
		}
	}
	// the schemas' own examples
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch node := node.(type) {
		case map[string]any:
			for i, example := range asSlice(node["examples"]) {
				report(fmt.Sprintf("%s examples[%d]", path, i), doc.check(node, example, "$"))
			}
			for key, value := range node {
				if key != "examples" {
					walk(path+"/"+key, value)
				}
			}
		case []any:
			for i, value := range node {
				walk(fmt.Sprintf("%s/%d", path, i), value)
			}
		}
	}
	walk("#/components/schemas", asMap(doc.root["components"])["schemas"])
}

// The validator: the JSON Schema keywords the document uses, strictly, so an
// example's undeclared property is a problem rather than ignored.

var (
	uuidPattern         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	knownSchemaKeywords = map[string]bool{
		"$ref": true, "type": true, "const": true, "enum": true, "properties": true, "required": true,
		"additionalProperties": true, "items": true, "oneOf": true, "anyOf": true, "allOf": true,
		"pattern": true, "minimum": true, "maximum": true, "minLength": true, "maxLength": true,
		"minItems": true, "maxItems": true, "format": true, "description": true, "examples": true,
		"default": true, "title": true,
	}
)

func number(v any) (float64, bool) {
	switch v := v.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

func isType(v any, typ string) bool {
	switch typ {
	case "null":
		return v == nil
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		f, ok := number(v)
		return ok && f == math.Trunc(f)
	case "number":
		_, ok := number(v)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	}
	return false
}

func jsonEqual(a, b any) bool {
	if fa, ok := number(a); ok {
		fb, ok := number(b)
		return ok && fa == fb
	}
	return reflect.DeepEqual(a, b)
}

func (self *operatorDoc) check(schema map[string]any, value any, path string) []string {
	if schema == nil {
		return []string{path + ": no schema"}
	}
	var out []string
	fail := func(format string, args ...any) {
		out = append(out, path+": "+fmt.Sprintf(format, args...))
	}
	for keyword := range schema {
		if !knownSchemaKeywords[keyword] {
			fail("the validator does not know the keyword %q", keyword)
		}
	}
	if r, ok := schema["$ref"].(string); ok {
		target, found := self.ref(r)
		if !found {
			fail("unresolved %s", r)
			return out
		}
		out = append(out, self.check(target, value, path)...)
	}
	for _, member := range asSlice(schema["allOf"]) {
		out = append(out, self.check(asMap(member), value, path)...)
	}
	if alternatives := asSlice(schema["oneOf"]); alternatives != nil {
		matched := 0
		var nearest []string
		for _, alternative := range alternatives {
			problems := self.check(asMap(alternative), value, path)
			if len(problems) == 0 {
				matched++
			} else if nearest == nil || len(problems) < len(nearest) {
				nearest = problems
			}
		}
		if matched != 1 {
			fail("matches %d of oneOf's %d alternatives %v", matched, len(alternatives), nearest)
		}
	}
	if alternatives := asSlice(schema["anyOf"]); alternatives != nil {
		if !slices.ContainsFunc(alternatives, func(alternative any) bool {
			return len(self.check(asMap(alternative), value, path)) == 0
		}) {
			fail("matches none of anyOf's alternatives")
		}
	}
	if typ, ok := schema["type"]; ok {
		var types []string
		switch typ := typ.(type) {
		case string:
			types = []string{typ}
		case []any:
			for _, t := range typ {
				types = append(types, t.(string))
			}
		}
		if !slices.ContainsFunc(types, func(typ string) bool { return isType(value, typ) }) {
			fail("%v is not of type %v", value, types)
			return out
		}
	}
	if c, ok := schema["const"]; ok && !jsonEqual(c, value) {
		fail("%v is not %v", value, c)
	}
	if enum, ok := schema["enum"]; ok && !slices.ContainsFunc(asSlice(enum), func(e any) bool { return jsonEqual(e, value) }) {
		fail("%v is not one of %v", value, enum)
	}
	switch v := value.(type) {
	case string:
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(v) {
			fail("%q does not match %s", v, pattern)
		}
		if n, ok := number(schema["minLength"]); ok && float64(len([]rune(v))) < n {
			fail("%q is shorter than %v", v, n)
		}
		if n, ok := number(schema["maxLength"]); ok && float64(len([]rune(v))) > n {
			fail("%q is longer than %v", v, n)
		}
		switch schema["format"] {
		case "date-time":
			if _, err := time.Parse(time.RFC3339, v); err != nil {
				fail("%q is not RFC 3339", v)
			}
		case "uuid":
			if !uuidPattern.MatchString(v) {
				fail("%q is not a lowercase uuid", v)
			}
		case "uri":
			if u, err := url.Parse(v); err != nil || !u.IsAbs() {
				fail("%q is not an absolute uri", v)
			}
		}
	case []any:
		if n, ok := number(schema["minItems"]); ok && float64(len(v)) < n {
			fail("fewer than %v items", n)
		}
		if n, ok := number(schema["maxItems"]); ok && float64(len(v)) > n {
			fail("more than %v items", n)
		}
		if items := asMap(schema["items"]); items != nil {
			for i, item := range v {
				out = append(out, self.check(items, item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	case map[string]any:
		for _, name := range asSlice(schema["required"]) {
			if _, ok := v[name.(string)]; !ok {
				fail("missing required %q", name)
			}
		}
		properties := asMap(schema["properties"])
		for name, property := range v {
			if declared := asMap(properties[name]); declared != nil {
				out = append(out, self.check(declared, property, path+"."+name)...)
				continue
			}
			if properties == nil && schema["additionalProperties"] == nil {
				// a composed schema ($ref, oneOf, allOf) checks its own
				continue
			}
			switch additional := schema["additionalProperties"].(type) {
			case map[string]any:
				out = append(out, self.check(additional, property, path+"."+name)...)
			case bool:
				if !additional {
					fail("undeclared property %q", name)
				}
			default:
				fail("undeclared property %q", name)
			}
		}
	default:
		if f, ok := number(value); ok {
			if n, ok := number(schema["minimum"]); ok && f < n {
				fail("%v is below %v", value, n)
			}
			if n, ok := number(schema["maximum"]); ok && f > n {
				fail("%v is above %v", value, n)
			}
		}
	}
	return out
}

// The validator itself refuses what it should.
func TestOperatorValidator(t *testing.T) {
	doc := loadOperatorDoc(t)
	amount := map[string]any{"$ref": "#/components/schemas/Amount"}
	for _, tc := range []struct {
		name   string
		schema map[string]any
		value  any
		ok     bool
	}{
		{"amount", amount, map[string]any{"alpha": "0.048025660", "rao": 48025660}, true},
		{"undeclared property", amount, map[string]any{"alpha": "0.048025660", "rao": 48025660, "raoo": 1}, false},
		{"missing property", amount, map[string]any{"alpha": "0.048025660"}, false},
		{"pattern", amount, map[string]any{"alpha": "0.04802566", "rao": 48025660}, false},
		{"type", amount, map[string]any{"alpha": "0.048025660", "rao": "48025660"}, false},
		{"nullable null", map[string]any{"$ref": "#/components/schemas/NullableAmount"}, nil, true},
		{"nullable wrong", map[string]any{"$ref": "#/components/schemas/NullableAmount"}, 1, false},
		{"enum", map[string]any{"$ref": "#/components/schemas/Role"}, "miner", false},
		{"const", map[string]any{"$ref": "#/components/schemas/Unit"}, map[string]any{"token": "alpha", "symbol": "α", "netuid": 26, "decimals": 9}, false},
		{"maximum", map[string]any{"type": "number", "maximum": 1}, 1.5, false},
		{"date-time", map[string]any{"$ref": "#/components/schemas/Timestamp"}, "2026-12-11T22:24:12+00:00", false},
	} {
		if problems := doc.check(tc.schema, tc.value, "$"); (len(problems) == 0) != tc.ok {
			t.Errorf("%s: problems %v, want ok %v", tc.name, problems, tc.ok)
		}
	}
}

func TestOperatorConventions(t *testing.T) {
	doc := loadOperatorDoc(t)
	schemaOf := func(media any) string {
		r, _ := asMap(asMap(media)["schema"])["$ref"].(string)
		return r
	}
	doc.eachResponse(func(operation, status string, response map[string]any) {
		headers := asMap(response["headers"])
		if headers["Access-Control-Allow-Origin"] == nil {
			t.Errorf("%s %s: no Access-Control-Allow-Origin", operation, status)
		}
		content := asMap(response["content"])
		switch {
		case status == "200":
			if headers["Cache-Control"] == nil {
				t.Errorf("%s: no Cache-Control", operation)
			}
			if operation != "GET /sn/openapi.yml" && headers["Link"] == nil {
				t.Errorf("%s: no service-desc Link", operation)
			}
			if headers["ETag"] != nil {
				op := doc.operations()[operation]
				if asMap(op["responses"])["304"] == nil {
					t.Errorf("%s: an ETag without a 304", operation)
				}
				if !slices.ContainsFunc(asSlice(op["parameters"]), func(p any) bool {
					return doc.deref(asMap(p))["name"] == "If-None-Match"
				}) {
					t.Errorf("%s: an ETag without If-None-Match", operation)
				}
				if content["application/json"] != nil && headers["Last-Modified"] == nil {
					t.Errorf("%s: a cacheable JSON answer without Last-Modified", operation)
				}
			}
		case status == "429":
			// the load balancer's own page
		case status >= "400":
			if status == "401" && headers["WWW-Authenticate"] == nil {
				t.Errorf("%s 401: no WWW-Authenticate", operation)
			}
			if len(content) != 1 || schemaOf(content["application/problem+json"]) != "#/components/schemas/Problem" {
				t.Errorf("%s %s: an error that is not a problem document", operation, status)
			}
		}
	})

	// amounts are Amount objects, times are Timestamps, countries are codes
	// (the ledger route keeps the decimal strings of rao its clients parse)
	for name, schema := range asMap(asMap(doc.root["components"])["schemas"]) {
		for property, value := range asMap(asMap(schema)["properties"]) {
			r, _ := asMap(value)["$ref"].(string)
			where := name + "." + property
			if strings.HasSuffix(property, "_rao") || strings.HasSuffix(property, "_alpha") {
				t.Errorf("%s: an amount is an Amount object, not a bare _rao or _alpha field", where)
			}
			if property == "as_of" && r != "#/components/schemas/Timestamp" {
				t.Errorf("%s: as_of is not a Timestamp", where)
			}
			if strings.HasPrefix(name, "Ctf") && (property == "code" || property == "country") {
				// a code, or a nullable one; a country's whole row is an object
				alternatives := []any{value}
				if oneOf := asSlice(asMap(value)["oneOf"]); oneOf != nil {
					alternatives = oneOf
				}
				for _, alternative := range alternatives {
					alternative := asMap(alternative)
					if typ := doc.deref(alternative)["type"]; typ != "string" {
						continue
					}
					if alternative["$ref"] != "#/components/schemas/CountryCode" {
						t.Errorf("%s: a country code is a CountryCode", where)
					}
				}
			}
		}
	}
}

// The examples' arithmetic: the design's rules, so an example a client
// copies is one the server could have answered.

func intOf(t *testing.T, v any) int64 {
	t.Helper()
	f, ok := number(v)
	if !ok {
		t.Fatalf("%v is not a number", v)
	}
	return int64(f)
}

func raoOf(t *testing.T, amount any) int64 {
	t.Helper()
	return intOf(t, asMap(amount)["rao"])
}

func alphaString(rao int64) string {
	return fmt.Sprintf("%d.%09d", rao/1e9, rao%1e9)
}

func TestOperatorExampleArithmetic(t *testing.T) {
	doc := loadOperatorDoc(t)
	var values []any
	doc.eachResponse(func(_, _ string, response map[string]any) {
		for _, media := range asMap(response["content"]) {
			for _, value := range doc.mediaExamples(asMap(media)) {
				values = append(values, value)
			}
		}
	})
	if len(values) == 0 {
		t.Fatal("no examples")
	}

	// rules on any object of a shape, wherever it appears
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch node := node.(type) {
		case map[string]any:
			checkShapes(t, path, node)
			for key, value := range node {
				walk(path+"."+key, value)
			}
		case []any:
			for i, value := range node {
				walk(fmt.Sprintf("%s[%d]", path, i), value)
			}
		}
	}
	for i, value := range values {
		walk(fmt.Sprintf("example%d", i), value)
	}

	// rules on whole answers, which need their context
	for _, value := range values {
		answer := asMap(value)
		if answer == nil || answer["unit"] == nil {
			continue
		}
		// the slot amount and whether last epoch had a draw, when the answer
		// carries them (GET /sn/ctf, /country, /top)
		var slot int64 = -1
		last := asMap(answer["last_epoch"])
		if last != nil {
			slot = raoOf(t, asMap(last["amounts"])["slot"])
		}
		drawn := last != nil && last["drawn"] == true
		if totals := asMap(answer["totals"]); totals != nil && (totals["not_fully_captured"] == nil) == drawn {
			t.Errorf("totals: not_fully_captured is null exactly without a draw last epoch (drawn %v)", drawn)
		}
		var countries []map[string]any
		for _, c := range asSlice(answer["countries"]) {
			if inner := asMap(asMap(c)["country"]); inner != nil {
				countries = append(countries, inner)
			} else {
				countries = append(countries, asMap(c))
			}
		}
		if c := asMap(answer["country"]); c != nil {
			countries = append(countries, c)
		}
		if _, ok := answer["current_epoch"]; ok {
			for _, c := range countries {
				checkCountryState(t, c, slot, drawn)
			}
		}
		if role, ok := answer["role"].(string); ok {
			checkTop(t, answer, role)
		}
		if history := asSlice(answer["history"]); history != nil {
			checkHistory(t, asMap(answer["country"]), history, intOf(t, asMap(answer["pool"])["first_epoch"]))
		}
		if answer["awards"] != nil {
			checkEpoch(t, answer)
		}
		if _, ok := answer["wins"]; ok {
			checkClient(t, answer)
		}
	}
}

func checkShapes(t *testing.T, path string, m map[string]any) {
	t.Helper()
	has := func(keys ...string) bool {
		for _, key := range keys {
			if _, ok := m[key]; !ok {
				return false
			}
		}
		return true
	}
	if has("alpha", "rao") && len(m) == 2 {
		if want := alphaString(intOf(t, m["rao"])); m["alpha"] != want {
			t.Errorf("%s: alpha %v, rao %v: want alpha %s", path, m["alpha"], m["rao"], want)
		}
	}
	if has("total", "won", "open") && len(m) == 3 {
		if intOf(t, m["won"])+intOf(t, m["open"]) != intOf(t, m["total"]) {
			t.Errorf("%s: won + open is not total: %v", path, m)
		}
	}
	if has("units", "entries", "chance_to_win") {
		// a role's draw figures: null together without a draw last epoch
		figures := []string{"slots", "units", "near_misses", "entries", "chance_to_win"}
		nulls := 0
		for _, key := range figures {
			if m[key] == nil {
				nulls++
			}
		}
		if nulls != 0 && nulls != len(figures) {
			t.Errorf("%s: the draw figures %v are null together or not at all", path, figures)
		}
		if nulls == 0 {
			units, entries := intOf(t, m["units"]), intOf(t, m["entries"])
			if entries != units+1 {
				t.Errorf("%s: entries %d for %d units", path, entries, units)
			}
			chance, _ := number(m["chance_to_win"])
			if want := math.Min(1, 5/float64(entries)); math.Abs(chance-want) > 1e-15 {
				t.Errorf("%s: chance_to_win %v, want min(1, 5/%d) = %v", path, chance, entries, want)
			}
			if won := intOf(t, asMap(m["slots"])["won"]); (won < 5) != (chance == 1) {
				t.Errorf("%s: %d won and a chance of %v: a role not fully captured, and only that, is won by default", path, won, chance)
			}
		}
	}
	if has("epoch", "census_hash", "census_block", "round") {
		if (m["census_hash"] == nil) != (m["census_block"] == nil) || (m["census_hash"] == nil) != (m["round"] == nil) {
			t.Errorf("%s: a pending draw's census and round are null together", path)
		}
	}
	if has("funding", "returned", "owed", "matured", "fresh", "slot", "paid", "pending_in", "pending", "carried_in", "carried") {
		r := func(key string) int64 { return raoOf(t, m[key]) }
		owed := r("returned")
		if m["matured"] == true {
			owed = r("owed")
		}
		if r("fresh") != r("funding")-owed {
			t.Errorf("%s: fresh is not funding less what was owed back", path)
		}
		if r("slot") != r("fresh")/ctfSlots {
			t.Errorf("%s: slot is not floor(fresh / 2490)", path)
		}
		if r("carried") != r("carried_in")+r("fresh")-r("paid")+r("pending_in")-r("pending") {
			t.Errorf("%s: carried does not balance", path)
		}
		if r("paid") < r("pending_paid") {
			t.Errorf("%s: paid is less than the pending awards it paid", path)
		}
	}
	if has("coldkey", "coldkey_ss58") && m["coldkey"] != nil {
		key, err := hex.DecodeString(strings.TrimPrefix(m["coldkey"].(string), "0x"))
		if err != nil || len(key) != 32 {
			t.Fatalf("%s: coldkey %v", path, m["coldkey"])
		}
		if address, _ := ss58.Encode([32]byte(key), 42); m["coldkey_ss58"] != address {
			t.Errorf("%s: coldkey_ss58 %v does not encode %v (%s)", path, m["coldkey_ss58"], m["coldkey"], address)
		}
	}
	if has("carry_contract", "carry_coldkey") {
		contract, err := hex.DecodeString(strings.TrimPrefix(m["carry_contract"].(string), "0x"))
		if err != nil || len(contract) != 20 {
			t.Fatalf("%s: carry_contract %v", path, m["carry_contract"])
		}
		if mirror := fmt.Sprintf("0x%x", ss58.EvmMirrorPubkey([20]byte(contract))); m["carry_coldkey"] != mirror {
			t.Errorf("%s: carry_coldkey %v is not the carry contract's mirror %s", path, m["carry_coldkey"], mirror)
		}
		if intOf(t, m["pool_id"]) != 1<<32+intOf(t, m["no_id"]) {
			t.Errorf("%s: pool_id is not 2^32 + no_id", path)
		}
	}
	if has("accounts", "unavailable_pools") {
		checkLedger(t, path, m)
	}
}

// A country's row: the estimates follow from the pots and the slot amount.
// slot is last epoch's slot amount, -1 before the first root; drawn says
// whether last epoch had a draw.
func checkCountryState(t *testing.T, c map[string]any, slot int64, drawn bool) {
	t.Helper()
	p, x := asMap(c["providers"]), asMap(c["extenders"])
	for _, role := range []map[string]any{p, x} {
		if (role["slots"] != nil) != drawn {
			t.Errorf("%v: the draw figures are set exactly when last epoch had a draw (drawn %v)", c["code"], drawn)
			return
		}
		if slot < 0 {
			if role["win_estimate"] != nil || raoOf(t, role["pot"]) != 0 {
				t.Errorf("%v: before the first root the pots are empty and the estimates null", c["code"])
			}
			continue
		}
		if want := slot + raoOf(t, role["pot"])/5; raoOf(t, role["win_estimate"]) != want {
			t.Errorf("%v: win_estimate %v, want a slot and a fifth of the pot, %d", c["code"], role["win_estimate"], want)
		}
		if drawn && intOf(t, asMap(role["slots"])["won"]) == 5 && raoOf(t, role["pot"]) != 0 {
			t.Errorf("%v: five winners leave a pot", c["code"])
		}
	}
	if slot < 0 {
		if c["available"] != nil {
			t.Errorf("%v: an estimate before the first root", c["code"])
		}
	} else if want := raoOf(t, p["pot"]) + raoOf(t, x["pot"]) + 10*slot; raoOf(t, c["available"]) != want {
		t.Errorf("%v: available %v, want both pots and ten slots, %d", c["code"], c["available"], want)
	}
	if raoOf(t, c["awarded"]) != raoOf(t, p["awarded"])+raoOf(t, x["awarded"]) {
		t.Errorf("%v: awarded is not both roles'", c["code"])
	}
	if !drawn {
		if c["near_misses"] != nil || c["flag_held"] != false {
			t.Errorf("%v: without a draw, no near misses and no flag held", c["code"])
		}
		return
	}
	if intOf(t, c["near_misses"]) != intOf(t, p["near_misses"])+intOf(t, x["near_misses"]) {
		t.Errorf("%v: near_misses is not both roles'", c["code"])
	}
	held := intOf(t, asMap(p["slots"])["won"]) == 5 && intOf(t, asMap(x["slots"])["won"]) == 5
	if c["flag_held"] != held {
		t.Errorf("%v: flag_held %v with %v and %v won", c["code"], c["flag_held"], p["slots"], x["slots"])
	}
}

// GET /sn/ctf/top: countries with a role not fully captured at last epoch's
// draw, ranked by the open pot, largest first, then by code.
func checkTop(t *testing.T, answer map[string]any, role string) {
	t.Helper()
	open := func(state any) bool {
		slots := asMap(asMap(state)["slots"])
		return slots != nil && intOf(t, slots["open"]) > 0
	}
	var previous map[string]any
	for i, entry := range asSlice(answer["countries"]) {
		entry := asMap(entry)
		c := asMap(entry["country"])
		pot := raoOf(t, asMap(c["providers"])["pot"]) + raoOf(t, asMap(c["extenders"])["pot"])
		listed := open(c["providers"]) || open(c["extenders"])
		if role != "any" {
			pot = raoOf(t, asMap(c[role+"s"])["pot"])
			listed = open(c[role+"s"])
		}
		if !listed {
			t.Errorf("top %v: listed, but its %s slots were all won or not drawn", c["code"], role)
		}
		if raoOf(t, entry["open_pot"]) != pot {
			t.Errorf("top %v: open_pot %v, want the %s pot %d", c["code"], entry["open_pot"], role, pot)
		}
		if intOf(t, entry["rank"]) != int64(i+1) {
			t.Errorf("top %v: rank %v at %d", c["code"], entry["rank"], i)
		}
		if answer["include_uninhabited"] == false && c["inhabited"] == false {
			t.Errorf("top %v: an uninhabited territory", c["code"])
		}
		if previous != nil {
			before := raoOf(t, previous["open_pot"])
			if before < pot || (before == pot && asMap(previous["country"])["code"].(string) > c["code"].(string)) {
				t.Errorf("top %v: out of order", c["code"])
			}
		}
		previous = entry
	}
}

// The pot recurrence of CAPTURETHEFLAG.md §4.5 for one role in one epoch:
// each of W winners takes floor(P/5) (five also share P mod 5), and each of
// the 5 − W empty slots adds its amount. A slot's amount is the epoch's slot,
// or one rao more for the first slots in slot order, so the open slots may
// add up to one rao each beyond it.
func checkPots(t *testing.T, where string, slot int64, role map[string]any) {
	t.Helper()
	won := intOf(t, asMap(role["slots"])["won"])
	in, out := raoOf(t, role["pot_in"]), raoOf(t, role["pot_out"])
	want := in - won*(in/5) + (5-won)*slot
	if won == 5 {
		want = 0
	}
	if diff := out - want; diff < 0 || diff > 5-won {
		t.Errorf("%s: pot_out %d, want %d (and up to one rao per open slot)", where, out, want)
	}
	if units := intOf(t, role["units"]); won != min(units, 5) && units > 0 {
		t.Errorf("%s: %d won of %d units", where, won, units)
	}
}

func checkHistory(t *testing.T, country map[string]any, history []any, firstEpoch int64) {
	t.Helper()
	for i, entry := range history {
		entry := asMap(entry)
		slot := raoOf(t, entry["slot"])
		for _, name := range []string{"providers", "extenders"} {
			role := asMap(entry[name])
			where := fmt.Sprintf("history %v epoch %v %s", country["code"], entry["epoch"], name)
			checkPots(t, where, slot, role)
			won, units := intOf(t, asMap(role["slots"])["won"]), intOf(t, role["units"])
			share, _ := number(role["winning_share"])
			switch {
			case units == 0 && role["winning_share"] != nil:
				t.Errorf("%s: a winning share without units", where)
			case units > 0 && math.Abs(share-float64(won)/float64(units)) > 1e-15:
				t.Errorf("%s: winning_share %v, want %d/%d", where, share, won, units)
			}
			if won == 0 {
				if role["median_award"] != nil {
					t.Errorf("%s: a median award without winners", where)
				}
			} else if median, want := raoOf(t, role["median_award"]), slot+raoOf(t, role["pot_in"])/5; median < want || median > want+2 {
				t.Errorf("%s: median_award %d, want a slot and a fifth of the pot, %d", where, median, want)
			}
			if i+1 < len(history) {
				older := asMap(asMap(history[i+1])[name])
				if raoOf(t, role["pot_in"]) != raoOf(t, older["pot_out"]) {
					t.Errorf("%s: pot_in is not the epoch before's pot_out", where)
				}
			} else if intOf(t, entry["epoch"]) == firstEpoch && raoOf(t, role["pot_in"]) != 0 {
				t.Errorf("%s: the first epoch starts with a pot", where)
			}
			if i == 0 && raoOf(t, role["pot_out"]) != raoOf(t, asMap(country[name])["pot"]) {
				t.Errorf("%s: the newest pot_out is not the pot carried into this epoch", where)
			}
		}
	}
}

func checkEpoch(t *testing.T, answer map[string]any) {
	t.Helper()
	amounts := asMap(answer["amounts"])
	slot := raoOf(t, amounts["slot"])
	if (answer["census"] == nil) != (answer["draw"] == nil) {
		t.Errorf("epoch %v: a census without a draw, or a draw without one", answer["epoch"])
	}
	if census := asMap(answer["census"]); census != nil && census["hash"] != asMap(answer["objects"])["census"] {
		t.Errorf("epoch %v: census.hash is not the census object", answer["epoch"])
	}
	if asMap(answer["root"])["ledger_hash"] != asMap(answer["objects"])["header"] {
		t.Errorf("epoch %v: the header object is not the root's ledger hash", answer["epoch"])
	}
	if asMap(answer["pool"])["countries_version"] != asMap(answer["objects"])["countries"] {
		t.Errorf("epoch %v: the countries object is not the pool's countries_version", answer["epoch"])
	}
	rows := map[string]map[string]any{}
	for _, c := range asSlice(answer["countries"]) {
		c := asMap(c)
		for _, name := range []string{"providers", "extenders"} {
			role := asMap(c[name])
			checkPots(t, fmt.Sprintf("epoch %v %v %s", answer["epoch"], c["code"], name), slot, role)
			rows[c["code"].(string)+"/"+strings.TrimSuffix(name, "s")] = role
		}
	}
	type roleAwards struct {
		count int64
		total int64
	}
	sums := map[string]*roleAwards{}
	var previous []string
	for _, award := range asSlice(answer["awards"]) {
		award := asMap(award)
		key := award["country"].(string) + "/" + award["role"].(string)
		order := []string{map[string]string{"provider": "1", "extender": "2"}[award["role"].(string)], award["country"].(string), fmt.Sprint(award["rank"])}
		if previous != nil && strings.Join(previous, " ") >= strings.Join(order, " ") {
			t.Errorf("epoch %v: awards out of order at %s", answer["epoch"], key)
		}
		previous = order
		if s := raoOf(t, award["slot"]); s != slot && s != slot+1 {
			t.Errorf("epoch %v %s: slot %d, want %d", answer["epoch"], key, s, slot)
		}
		if row := rows[key]; row != nil {
			q := raoOf(t, row["pot_in"]) / 5
			if share := raoOf(t, award["pot_share"]); share != q && share != q+1 {
				t.Errorf("epoch %v %s: pot_share %d, want a fifth of the pot, %d", answer["epoch"], key, share, q)
			}
		}
		if sums[key] == nil {
			sums[key] = &roleAwards{}
		}
		sums[key].count++
		sums[key].total += raoOf(t, award["slot"]) + raoOf(t, award["pot_share"])
	}
	for key, row := range rows {
		won := intOf(t, asMap(row["slots"])["won"])
		if sum := sums[key]; sum != nil && sum.count == won && sum.total != raoOf(t, row["awarded"]) {
			t.Errorf("epoch %v %s: the awards add up to %d, not awarded %v", answer["epoch"], key, sum.total, row["awarded"])
		}
	}
}

// checkClient holds a GET /sn/ctf/client answer to its rules: each role's
// fields agree with whether it won, and the wins agree with themselves and with
// the epochs.
func checkClient(t *testing.T, answer map[string]any) {
	t.Helper()
	if answer["client_id"] == nil && len(asSlice(answer["epochs"])) != 0 {
		t.Errorf("client: a network's answer, with no client_id, has epochs")
	}
	checkWins(t, answer)
	for _, epoch := range asSlice(answer["epochs"]) {
		epoch := asMap(epoch)
		for _, name := range []string{"providers", "extenders"} {
			role := asMap(epoch[name])
			where := fmt.Sprintf("client epoch %v %s", epoch["epoch"], name)
			if (role["candidate"] == true) != (role["reason"] == nil) {
				t.Errorf("%s: a candidate has no reason, and only a candidate", where)
			}
			if role["candidate"] == true && role["units_in_draw"] == nil {
				t.Errorf("%s: a candidate without its draw's units", where)
			}
			if presence := asMap(role["presence"]); role["candidate"] == true && intOf(t, presence["present"]) < intOf(t, presence["required"]) {
				t.Errorf("%s: a candidate short of the presence rule", where)
			}
			won := role["won"] == true
			for _, key := range []string{"rank", "slot", "pot_share", "coldkey", "coldkey_ss58"} {
				if (role[key] != nil) != won {
					t.Errorf("%s: %s is set exactly when it won", where, key)
				}
			}
			if won && role["candidate"] != true {
				t.Errorf("%s: won without being a candidate", where)
			}
		}
	}
}

// checkWins: each win's amount is its slot and pot share; the wins are newest
// first, then providers, country and rank; a win whose root is pending is not
// claimed, never unknown; a client's wins are its own, and in the epochs it
// lists they are exactly the roles it won there.
func checkWins(t *testing.T, answer map[string]any) {
	t.Helper()
	roleOrder := map[any]int{"provider": 0, "extender": 1}
	var previous map[string]any
	listed := map[string]map[string]any{}
	for i, raw := range asSlice(answer["wins"]) {
		win := asMap(raw)
		where := fmt.Sprintf("client win %d (epoch %v %v %v rank %v)", i, win["epoch"], win["role"], win["country"], win["rank"])
		if raoOf(t, win["amount"]) != raoOf(t, win["slot"])+raoOf(t, win["pot_share"]) {
			t.Errorf("%s: amount is not slot + pot_share", where)
		}
		if win["root_status"] == "pending" && win["claimed"] != false {
			t.Errorf("%s: a win of a pending root is claimed %v, want false", where, win["claimed"])
		}
		if client := answer["client_id"]; client != nil && win["client_id"] != client {
			t.Errorf("%s: client %v in the answer of client %v", where, win["client_id"], client)
		}
		if previous != nil {
			order := cmp.Or(
				cmp.Compare(intOf(t, previous["epoch"]), intOf(t, win["epoch"]))*-1,
				cmp.Compare(roleOrder[previous["role"]], roleOrder[win["role"]]),
				cmp.Compare(previous["country"].(string), win["country"].(string)),
				cmp.Compare(intOf(t, previous["rank"]), intOf(t, win["rank"])),
			)
			if order >= 0 {
				t.Errorf("%s: not after the win before it, newest first", where)
			}
		}
		previous = win
		listed[fmt.Sprintf("%v/%v", win["epoch"], win["role"])] = win
	}
	for _, raw := range asSlice(answer["epochs"]) {
		epoch := asMap(raw)
		for name, role := range map[string]string{"providers": "provider", "extenders": "extender"} {
			state := asMap(epoch[name])
			win := listed[fmt.Sprintf("%v/%s", epoch["epoch"], role)]
			where := fmt.Sprintf("client epoch %v %s", epoch["epoch"], name)
			if (state["won"] == true) != (win != nil) {
				t.Errorf("%s: won %v, with a win listed %v: every win of the listed epochs is listed", where, state["won"], win != nil)
				continue
			}
			if win == nil {
				continue
			}
			if win["country"] != state["country"] || !jsonEqual(win["rank"], state["rank"]) || !reflect.DeepEqual(win["slot"], state["slot"]) ||
				!reflect.DeepEqual(win["pot_share"], state["pot_share"]) || win["pending"] != state["pending"] || win["coldkey"] != state["coldkey"] ||
				win["root_status"] != epoch["root_status"] {
				t.Errorf("%s: the win %v is not the role %v", where, win, state)
			}
		}
	}
}

func checkLedger(t *testing.T, path string, answer map[string]any) {
	t.Helper()
	decimal := func(v any) *big.Int {
		n, ok := new(big.Int).SetString(v.(string), 10)
		if !ok {
			t.Fatalf("%s: %v is not a decimal", path, v)
		}
		return n
	}
	kindOf := func(noId int64) string {
		if noId >= 1<<32 {
			return "ctf"
		}
		return "pool"
	}
	for _, list := range []string{"accounts", "unavailable_pools"} {
		var previous int64 = -1
		for _, entry := range asSlice(answer[list]) {
			entry := asMap(entry)
			noId := intOf(t, entry["no_id"])
			if noId <= previous {
				t.Errorf("%s: %s not in ascending no_id", path, list)
			}
			previous = noId
			if entry["kind"] != kindOf(noId) {
				t.Errorf("%s: no_id %d is kind %v, want %s", path, noId, entry["kind"], kindOf(noId))
			}
			if list != "accounts" {
				continue
			}
			if (entry["root_index"] == nil) != (entry["proof"] == nil) {
				t.Errorf("%s: no_id %d: a root index without a proof, or one without the other", path, noId)
			}
			if decimal(entry["claimed"]).Cmp(decimal(entry["cumulative"])) > 0 {
				t.Errorf("%s: no_id %d: claimed above cumulative", path, noId)
			}
			if proof := asSlice(entry["proof"]); proof != nil && intOf(t, entry["index"]) >= 1<<len(proof) {
				t.Errorf("%s: no_id %d: index %v does not fit a proof of %d levels", path, noId, entry["index"], len(proof))
			}
		}
	}
}

// The ledger route is listed in connect/api/bringyour.yml too, for the
// generated clients and the API reference. Every field that document gives
// the answer is here with the same type, and no /sn/ctf route is there.
func TestOperatorLedgerKeepsBringyourFields(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "connect", "api", "bringyour.yml"))
	if os.IsNotExist(err) {
		t.Skip("no connect checkout beside sn")
	}
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	bringyour := &operatorDoc{root: root}
	doc := loadOperatorDoc(t)

	for path := range asMap(root["paths"]) {
		if path == "/sn/ctf" || strings.HasPrefix(path, "/sn/ctf/") || path == "/sn/openapi.yml" {
			t.Errorf("bringyour.yml lists %s, which operator.yml documents", path)
		}
	}
	theirs := asMap(asMap(asMap(root["paths"])["/sn/ledger/account"])["get"])
	ours := doc.operations()["GET /sn/ledger/account"]
	if theirs["operationId"] != ours["operationId"] {
		t.Errorf("operationId %v in bringyour.yml, %v here", theirs["operationId"], ours["operationId"])
	}

	typeOf := func(d *operatorDoc, schema any) string {
		s := d.deref(asMap(schema))
		return fmt.Sprint(s["type"])
	}
	// Until connect describes the new fields, they are logged; once it
	// describes `kind`, it has to describe all of them.
	theirAccount, _ := bringyour.ref("#/components/schemas/SnLedgerAccount")
	updated := asMap(asMap(theirAccount)["properties"])["kind"] != nil
	for _, name := range []string{"SnLedgerAccountResult", "SnLedgerAccount", "SnLedgerProofNode"} {
		their, _ := bringyour.ref("#/components/schemas/" + name)
		our, _ := doc.ref("#/components/schemas/" + name)
		if their == nil || our == nil {
			t.Fatalf("%s is missing from bringyour.yml or here", name)
		}
		ourProperties := asMap(our["properties"])
		for property, schema := range asMap(their["properties"]) {
			if ourProperties[property] == nil {
				t.Errorf("%s.%s is in bringyour.yml and not here", name, property)
			} else if a, b := typeOf(bringyour, schema), typeOf(doc, ourProperties[property]); a != b {
				t.Errorf("%s.%s is %s in bringyour.yml and %s here", name, property, a, b)
			}
		}
		for _, required := range asSlice(their["required"]) {
			if !slices.Contains(asSlice(our["required"]), required) {
				t.Errorf("%s.%s is required in bringyour.yml and not here", name, required)
			}
		}
		for property := range ourProperties {
			switch {
			case asMap(their["properties"])[property] != nil:
			case updated:
				t.Errorf("connect/api/bringyour.yml describes kind but not %s.%s", name, property)
			default:
				t.Logf("connect/api/bringyour.yml does not yet describe %s.%s", name, property)
			}
		}
	}
}
