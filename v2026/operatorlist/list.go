// Package operatorlist reads the machine-readable list of UR network operators
// published at https://ur.xyz/operators.yml (canonical copy: operators.yml in
// this repository). The list only says where each operator serves its API and
// Connect endpoints. It grants no weight, payout or registration authority:
// validators still weight only operators pinned by their signed release config,
// and each operator authenticates its own accounts and mining keys.
package operatorlist

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

const Schema = "urnetwork-operators-v1"

// Where ur.xyz publishes the list.
const DefaultUrl = "https://ur.xyz/operators.yml"

// A list stays small; larger bytes are refused before parsing.
const MaximumBytes = 64 * 1024

const MaximumOperators = 256

// Where one operator serves its API and Connect endpoints. Domain is the
// operator's stable identity in the list and names its local state.
type Operator struct {
	Domain     string `yaml:"domain" json:"domain"`
	ApiUrl     string `yaml:"api_url" json:"api_url"`
	ConnectUrl string `yaml:"connect_url" json:"connect_url"`
}

// The published document. Operators keep their published order.
type List struct {
	Schema    string     `yaml:"schema" json:"schema"`
	Operators []Operator `yaml:"operators" json:"operators"`
}

// Strict: one bounded YAML document with known fields only, the exact schema,
// and every operator valid and unique by domain, API and Connect endpoint.
func Parse(raw []byte) (List, error) {
	if len(raw) == 0 || len(raw) > MaximumBytes {
		return List{}, fmt.Errorf("operator list must be 1 to %d bytes, got %d", MaximumBytes, len(raw))
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var list List
	if err := decoder.Decode(&list); err != nil {
		return List{}, fmt.Errorf("operator list is not valid YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return List{}, errors.New("operator list must be exactly one YAML document")
	}
	if err := list.Validate(); err != nil {
		return List{}, err
	}
	return list, nil
}

func (self List) Validate() error {
	if self.Schema != Schema {
		return fmt.Errorf("operator list schema is %q, want %q", self.Schema, Schema)
	}
	if len(self.Operators) == 0 || len(self.Operators) > MaximumOperators {
		return fmt.Errorf("operator list must name 1 to %d operators, got %d", MaximumOperators, len(self.Operators))
	}
	domains := map[string]bool{}
	endpoints := map[string]string{}
	for _, operator := range self.Operators {
		if !validDomain(operator.Domain) {
			return fmt.Errorf("operator domain %q is not a lowercase DNS name", operator.Domain)
		}
		if domains[operator.Domain] {
			return fmt.Errorf("operator domain %q is listed twice", operator.Domain)
		}
		domains[operator.Domain] = true
		for _, endpoint := range []struct {
			name, raw, secure, plain string
		}{
			{name: "api_url", raw: operator.ApiUrl, secure: "https", plain: "http"},
			{name: "connect_url", raw: operator.ConnectUrl, secure: "wss", plain: "ws"},
		} {
			key, err := endpointKey(endpoint.raw, endpoint.secure, endpoint.plain)
			if err != nil {
				return fmt.Errorf("operator %s %s: %w", operator.Domain, endpoint.name, err)
			}
			if prior, ok := endpoints[key]; ok {
				return fmt.Errorf("operator %s %s %q is also used by %s", operator.Domain, endpoint.name, endpoint.raw, prior)
			}
			endpoints[key] = operator.Domain
		}
	}
	return nil
}

// Exact domain lookup.
func (self List) Operator(domain string) (Operator, bool) {
	for _, operator := range self.Operators {
		if operator.Domain == domain {
			return operator, true
		}
	}
	return Operator{}, false
}

// Lowercase labels of letters, digits and inner hyphens; at least two labels
// and a top-level label that is not all digits, so an address cannot pass.
func validDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	top := labels[len(labels)-1]
	return strings.Trim(top, "0123456789") != ""
}

// Normalized endpoint identity. The secure scheme is required except for an
// explicit loopback host, which may use plaintext for local operators.
func endpointKey(raw string, secure string, plain string) (string, error) {
	if raw == "" {
		return "", errors.New("is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("%q must be scheme://host[/path] with no credentials, query or fragment", raw)
	}
	switch u.Scheme {
	case secure:
	case plain:
		if !loopbackHost(u.Hostname()) {
			return "", fmt.Errorf("%q must use %s; plaintext %s is allowed only for loopback", raw, secure, plain)
		}
	default:
		return "", fmt.Errorf("%q must use %s", raw, secure)
	}
	clean := strings.TrimSuffix(u.EscapedPath(), "/")
	if clean != "" && (path.Clean(clean) != clean || !strings.HasPrefix(clean, "/")) {
		return "", fmt.Errorf("%q has a non-canonical path", raw)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host) + clean, nil
}

// Only literal loopback names qualify; DNS is never consulted, so a later
// rebinding cannot move plaintext credentials off the host.
func loopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
