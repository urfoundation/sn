// Completes the existing private fixture for the exact portable suite contract.
// Only the supplied frozen server inputs are read; no host vault is a fallback.
package main

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The generator owns this exact typed census. A changed upstream manifest
// requires an explicit resource implementation, never a generic empty file.
var suiteFixtureResourceKindNames = map[string][]string{
	"vault":      {"auth.yml", "brevo.yml", "circle.yml", "client.yml", "coinbase.yml", "helius.yml", "ipinfo.yml", "jwt.yml", "jwt-local-evaluator.pem", "password.yml", "pg.yml", "proxy.yml", "redis.yml", "services.yml", "st.yml", "stripe.yml", "wireguard.yml", "x402.yml"},
	"vault_tree": {"tls"},
	"config":     {"apple_roots.pem", "brevo.yml", "city-list.yml", "db.yml", "email.yml", "iso-country-list.yml", "pro.yml", "redis.yml", "settings.yml", "subsidy.yml", "tls.yml"},
}

// Suite admission reads a few fixed small physical source files only.
func readSuiteFixtureSource(server, relative string) ([]byte, error) {
	path := filepath.Join(server, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	resolved, resolveErr := filepath.EvalSymlinks(path)
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1024*1024 || resolveErr != nil || resolved != path {
		return nil, errors.New("suite fixture input is not a bounded physical file")
	}
	return os.ReadFile(path)
}

// Every required resource is admitted exactly once before any output exists.
func validateSuiteFixtureManifest(encoded []byte) error {
	want := map[string]bool{}
	for kind, names := range suiteFixtureResourceKindNames {
		for _, name := range names {
			want[kind+"="+name] = true
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(encoded))
	if !scanner.Scan() || scanner.Text() != "format=urnetwork-server-suite-resources-v1" {
		return errors.New("suite fixture manifest format differs")
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !want[line] {
			return fmt.Errorf("suite fixture manifest contains unknown or duplicate resource %q", line)
		}
		delete(want, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(want) != 0 {
		return errors.New("suite fixture manifest is incomplete")
	}
	return nil
}

// The existing shell contract chooses resource aliases, not certificate
// identities. Derive those required paths without copying live hostnames into
// fixture data; every generated certificate instead names fixture.example.
func suiteFixtureTlsAliases(encoded []byte) ([]string, error) {
	start := bytes.Index(encoded, []byte("test_env_tls_tree_complete() {\n"))
	if start < 0 {
		return nil, errors.New("suite fixture tls contract is missing")
	}
	section := encoded[start:]
	end := bytes.Index(section, []byte("\n}\n"))
	if end < 0 {
		return nil, errors.New("suite fixture tls contract is incomplete")
	}
	rows := regexp.MustCompile(`(?m)^    for host_name in ([a-z0-9. -]+); do$`).FindAllSubmatch(section[:end], -1)
	if len(rows) != 1 {
		return nil, errors.New("suite fixture tls aliases have no single literal contract")
	}
	names := strings.Fields(string(rows[0][1]))
	if len(names) == 0 || len(names) > 16 {
		return nil, errors.New("suite fixture tls alias census exceeds its bound")
	}
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	for _, name := range names {
		if len(name) > 253 || !pattern.MatchString(name) || seen[name] {
			return nil, errors.New("suite fixture tls alias is invalid or duplicated")
		}
		seen[name] = true
	}
	return names, nil
}

// The service owner supplies real daemon-assigned loopback endpoints. Neither
// a DNS name nor a port default can redirect fixture database traffic.
func validateSuiteFixtureAuthorities(postgres, redis string) error {
	if postgres == redis {
		return errors.New("suite fixture services require distinct authorities")
	}
	for _, authority := range []string{postgres, redis} {
		host, port, err := net.SplitHostPort(authority)
		number, numberErr := strconv.ParseUint(port, 10, 16)
		if err != nil || host != "127.0.0.1" || numberErr != nil || number == 0 || strconv.FormatUint(number, 10) != port {
			return errors.New("suite fixture services require exact loopback host and nonzero port")
		}
	}
	return nil
}

// Fresh independent key material never crosses the public report boundary.
func suiteFixtureSecret() ([]byte, error) {
	value := make([]byte, 32)
	_, err := rand.Read(value)
	return value, err
}

// This typed suite extension preserves the existing authentication-only CLI.
// Failed later writes retain the private output; original sources stay untouched.
func createSuiteFixture(parent, server, postgres, redis string) (report fixtureReport, returnErr error) {
	if err := validateSuiteFixtureAuthorities(postgres, redis); err != nil {
		return report, err
	}
	if err := requireServerInputs(server); err != nil {
		return report, err
	}
	manifest, err := readSuiteFixtureSource(server, "local/suite-resource-manifest.txt")
	if err != nil {
		return report, err
	}
	if err := validateSuiteFixtureManifest(manifest); err != nil {
		return report, err
	}
	contract, err := readSuiteFixtureSource(server, "test-env.sh")
	if err != nil {
		return report, err
	}
	aliases, err := suiteFixtureTlsAliases(contract)
	if err != nil {
		return report, err
	}
	dbConfig, err := readSuiteFixtureSource(server, "local/testdata/config/local/db.yml")
	if err != nil {
		return report, err
	}
	redisConfig, err := readSuiteFixtureSource(server, "local/testdata/config/local/redis.yml")
	if err != nil {
		return report, err
	}
	report, err = createFixture(parent, server)
	if err != nil {
		return report, err
	}
	defer func() {
		if returnErr != nil {
			returnErr = fmt.Errorf("complete suite fixture retained at %s: %w", report.Workspace, returnErr)
		}
	}()

	oauthKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return report, err
	}
	oauthDer, err := x509.MarshalECPrivateKey(oauthKey)
	if err != nil {
		return report, err
	}
	x := base64.RawURLEncoding.EncodeToString(oauthKey.X.FillBytes(make([]byte, 32)))
	y := base64.RawURLEncoding.EncodeToString(oauthKey.Y.FillBytes(make([]byte, 32)))
	thumbprint := sha256.Sum256([]byte(fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, x, y)))
	kid := base64.RawURLEncoding.EncodeToString(thumbprint[:])
	wgKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return report, err
	}
	secrets := make([][]byte, 5)
	for index := range secrets {
		secrets[index], err = suiteFixtureSecret()
		if err != nil {
			return report, err
		}
	}
	jwtKey, err := os.ReadFile(filepath.Join(report.Workspace, "vault", fixtureKeyName))
	if err != nil {
		return report, err
	}
	tlsKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return report, err
	}
	tlsDer, err := x509.MarshalECPrivateKey(tlsKey)
	if err != nil {
		return report, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return report, err
	}
	certificate := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "fixture.example"}, DNSNames: []string{"fixture.example"},
		NotBefore: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	certificateDer, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &tlsKey.PublicKey, tlsKey)
	if err != nil {
		return report, err
	}
	certificatePem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDer})
	keyPem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: tlsDer})
	pg := []byte(fmt.Sprintf("authority: %q\nuser: bringyour\npassword: urnetwork-local-test\ndb: bringyour\n", postgres))
	redisResource := []byte(fmt.Sprintf("authority: %q\npassword: \"\"\ndb: 0\ncluster: false\n", redis))
	resources := map[string][]byte{
		"vault/auth.yml":                []byte(fmt.Sprintf("reject_missing_expiration: true\nreject_expired: true\noauth:\n  issuer: https://auth.fixture.example\n  authorization_endpoint: https://auth.fixture.example/authorize\n  signer_keys:\n    - kid: %q\n      path: fixture-oauth.key\n      alg: ES256\n      create_time: '2000-01-01T00:00:00Z'\n", kid)),
		"vault/fixture-oauth.key":       pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: oauthDer}),
		"vault/jwt-local-evaluator.pem": jwtKey,
		"vault/client.yml":              []byte(fmt.Sprintf("client_ip_hash_pepper: %q\n", hex.EncodeToString(secrets[0]))),
		"vault/proxy.yml":               []byte(fmt.Sprintf("hosts:\n  proxy.fixture.example:\n    fixture:\n      socks: 1080\n      http: 8080\n      https: 8443\n      api: 8444\n      wg: 51820\nsecrets:\n  - %q\nwg:\n  private_key: %q\n  public_key: %q\n", hex.EncodeToString(secrets[1]), base64.StdEncoding.EncodeToString(wgKey.Bytes()), base64.StdEncoding.EncodeToString(wgKey.PublicKey().Bytes()))),
		"vault/wireguard.yml":           []byte(fmt.Sprintf("handoff_encryption_key: %q\n", hex.EncodeToString(secrets[2]))),
		"vault/pg.yml":                  pg, "vault/pg_maintenance.yml": pg, "vault/redis.yml": redisResource,
		"vault/brevo.yml":             []byte("brevo:\n  api_key: ''\n  webhook_bearers: []\n"),
		"vault/circle.yml":            []byte("wallet_set_id: ''\ncircle:\n  api_token: ''\n  entity_secret: ''\n  app_id: ''\n  solana_usdc_address: ''\n  polygon_usdc_address: ''\n  solana_wallet_id: ''\n  polygon_wallet_id: ''\n"),
		"vault/coinbase.yml":          []byte("api:\n  host: disabled.fixture.example\n  key: ''\nwebhook:\n  shared_secret: ''\n"),
		"vault/helius.yml":            []byte("api_key: ''\nhelius:\n  api_key: ''\n"),
		"vault/ipinfo.yml":            []byte("token: ''\n"),
		"vault/services.yml":          []byte("{}\n"),
		"vault/st.yml":                []byte("enabled: false\n"),
		"vault/stripe.yml":            []byte("api:\n  token: ''\n  publishable_key: ''\nwebhook:\n  signing_secret: ''\n"),
		"vault/x402.yml":              []byte("enabled: false\nskus: []\n"),
		"vault/verify.yml":            []byte(fmt.Sprintf("profile: testnet\nkeys:\n  - server_key_id: 0\n    seed: %q\negress_hash_key: %q\nsettings:\n  step_timeout_seconds: 5\n  step_timeout_grace_seconds: 1\n  trail_ttl_grace_seconds: 60\n  egress_ttl_seconds: 600\n  egress_refresh_seconds: 60\n  reliability_a_min: 1\n  stats_period_seconds: 60\n  egress_ipv4_prefix: 29\n  egress_ipv6_prefix: 48\n  egress_hash_key_id: fixture-egress\n  soft_guardrails_enabled: false\n  hard_seed_per_minute_per_source: 40\n  hard_extend_per_minute_per_source: 400\n  hard_active_trails_per_source: 32\n", base64.StdEncoding.EncodeToString(secrets[3]), base64.StdEncoding.EncodeToString(secrets[4]))),
		"config/apple_roots.pem":      certificatePem,
		"config/brevo.yml":            []byte("brevo:\n  list_ids:\n    new_networks: 1\n    network_users: 2\n"),
		"config/city-list.yml":        []byte("{}\n"),
		"config/iso-country-list.yml": []byte("{}\n"),
		"config/pro.yml":              []byte("{}\n"),
		"config/db.yml":               dbConfig, "config/db_maintenance.yml": dbConfig, "config/redis.yml": redisConfig,
		"config/email.yml":    []byte("company_sender_email: nobody@fixture.example\nreply_to_email: nobody@fixture.example\n"),
		"config/settings.yml": []byte("all: {}\n"),
		"config/subsidy.yml":  []byte("days: 1\nmin_days_fraction: 1\nusd_per_active_user: 0\nsubscription_net_revenue_fraction: 0\nmin_payout_usd: 1\nactive_user_byte_count_threshold: 1GiB\nreferral_parent_payout_fraction: 0\nreferral_child_payout_fraction: 0\naccount_points_per_payout: 0\nreliability_points_per_payout: 0\nreliability_subsidy_per_payout_usd: 0\ncountry_reliability_weight_target: 1\nmax_country_reliability_multiplier: 1\nmin_wallet_payout_usd: 1\nwallet_payout_timeout: 24h\nseeker_holder_multiplier: 1\n"),
		"config/tls.yml":      []byte("allowed_hosts:\n  - fixture.example\n"),
	}
	if !slices.Contains(aliases, "fixture.example") {
		aliases = append(aliases, "fixture.example")
	}
	for _, alias := range aliases {
		directory := filepath.Join(report.Workspace, "vault", "tls", alias)
		if err := os.MkdirAll(directory, 0700); err != nil {
			return report, err
		}
		resources["vault/tls/"+alias+"/"+alias+".crt"] = certificatePem
		resources["vault/tls/"+alias+"/"+alias+".key"] = keyPem
	}
	paths := make([]string, 0, len(resources))
	for path := range resources {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		if err := writePrivateFile(filepath.Join(report.Workspace, filepath.FromSlash(path)), resources[path]); err != nil {
			return report, err
		}
	}
	return report, nil
}
