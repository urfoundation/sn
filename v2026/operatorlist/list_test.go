package operatorlist

import (
	"os"
	"strings"
	"testing"
)

const testListOne = `schema: urnetwork-operators-v1
operators:
  - domain: operator-one.example
    api_url: https://api.operator-one.example
    connect_url: wss://connect.operator-one.example
`

func TestParseAcceptsThePublishedList(t *testing.T) {
	raw, err := os.ReadFile("../operators.yml")
	if err != nil {
		t.Fatal(err)
	}
	list, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	operator, ok := list.Operator("bringyour.com")
	if !ok || operator.ApiUrl != "https://api.bringyour.com" || operator.ConnectUrl != "wss://connect.bringyour.com" {
		t.Fatalf("published list lacks the reference operator: %+v", list)
	}
}

func TestParseKeepsPublishedOrder(t *testing.T) {
	list, err := Parse([]byte(testListOne + `  - domain: operator-two.example
    api_url: https://operator-two.example/api
    connect_url: wss://operator-two.example/connect
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Operators) != 2 || list.Operators[0].Domain != "operator-one.example" || list.Operators[1].Domain != "operator-two.example" {
		t.Fatalf("operators reordered: %+v", list.Operators)
	}
	if _, ok := list.Operator("operator-three.example"); ok {
		t.Fatal("unlisted operator found")
	}
}

func TestParseAllowsPlaintextOnlyForLoopback(t *testing.T) {
	loopback := `schema: urnetwork-operators-v1
operators:
  - domain: local-operator.example
    api_url: http://127.0.0.1:8080
    connect_url: ws://localhost:8081
`
	if _, err := Parse([]byte(loopback)); err != nil {
		t.Fatalf("loopback plaintext refused: %v", err)
	}
	for _, raw := range []string{
		strings.Replace(loopback, "http://127.0.0.1:8080", "http://api.operator.example", 1),
		strings.Replace(loopback, "ws://localhost:8081", "ws://connect.operator.example", 1),
		strings.Replace(loopback, "ws://localhost:8081", "ws://127.0.0.1.nip.example", 1),
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("non-loopback plaintext admitted:\n%s", raw)
		}
	}
}

func TestParseRefusesInvalidLists(t *testing.T) {
	second := `  - domain: operator-two.example
    api_url: https://api.operator-two.example
    connect_url: wss://connect.operator-two.example
`
	for _, entry := range []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "oversized", raw: testListOne + "# " + strings.Repeat("x", MaximumBytes) + "\n"},
		{name: "wrong schema", raw: strings.Replace(testListOne, "urnetwork-operators-v1", "urnetwork-operators-v2", 1)},
		{name: "no operators", raw: "schema: urnetwork-operators-v1\noperators: []\n"},
		{name: "unknown top-level field", raw: testListOne + "extra: true\n"},
		{name: "unknown operator field", raw: testListOne + "    no_id: 1\n"},
		{name: "second document", raw: testListOne + "---\n" + testListOne},
		{name: "duplicate key", raw: testListOne + "schema: urnetwork-operators-v1\n"},
		{name: "uppercase domain", raw: strings.Replace(testListOne, "domain: operator-one.example", "domain: Operator-One.example", 1)},
		{name: "single-label domain", raw: strings.Replace(testListOne, "domain: operator-one.example", "domain: localhost", 1)},
		{name: "address domain", raw: strings.Replace(testListOne, "domain: operator-one.example", "domain: 192.0.2.1", 1)},
		{name: "hyphen-edged label", raw: strings.Replace(testListOne, "domain: operator-one.example", "domain: -operator.example", 1)},
		{name: "duplicate domain", raw: testListOne + strings.ReplaceAll(second, "operator-two.example", "operator-one.example")},
		{name: "duplicate api url", raw: testListOne + strings.Replace(second, "https://api.operator-two.example", "https://API.operator-one.example/", 1)},
		{name: "duplicate connect url", raw: testListOne + strings.Replace(second, "wss://connect.operator-two.example", "wss://connect.operator-one.example", 1)},
		{name: "api over websocket", raw: strings.Replace(testListOne, "https://api.operator-one.example", "wss://api.operator-one.example", 1)},
		{name: "connect over https", raw: strings.Replace(testListOne, "wss://connect.operator-one.example", "https://connect.operator-one.example", 1)},
		{name: "credentials", raw: strings.Replace(testListOne, "https://api.operator-one.example", "https://user:secret@api.operator-one.example", 1)},
		{name: "query", raw: strings.Replace(testListOne, "https://api.operator-one.example", "https://api.operator-one.example?route=1", 1)},
		{name: "fragment", raw: strings.Replace(testListOne, "wss://connect.operator-one.example", "wss://connect.operator-one.example#x", 1)},
		{name: "dot path", raw: strings.Replace(testListOne, "https://api.operator-one.example", "https://api.operator-one.example/a/../b", 1)},
		{name: "missing host", raw: strings.Replace(testListOne, "https://api.operator-one.example", "https:///api", 1)},
	} {
		if _, err := Parse([]byte(entry.raw)); err == nil {
			t.Errorf("%s: invalid list admitted", entry.name)
		}
	}
}
