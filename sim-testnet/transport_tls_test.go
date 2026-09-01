package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urnetwork/connect"
	serverpkg "github.com/urnetwork/server"
)

func TestOperatorConnectTLSArtifactsAreDeterministicAndOperatorScoped(t *testing.T) {
	cfg := testResolvedConfig(t)
	caFirst, leafFirst, keyFirst, err := operatorConnectTLSArtifacts(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	caAgain, leafAgain, keyAgain, err := operatorConnectTLSArtifacts(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(caFirst, caAgain) || !bytes.Equal(leafFirst, leafAgain) || !bytes.Equal(keyFirst, keyAgain) {
		t.Fatal("operator Connect TLS identity changed across identical derivations")
	}
	caCertificate, err := parseSingleCertificatePem(caFirst)
	if err != nil {
		t.Fatal(err)
	}
	leafCertificate, err := parseSingleCertificatePem(leafFirst)
	if err != nil {
		t.Fatal(err)
	}
	caKeySeed := derive32(cfg, "transport/connect-ca/key")
	caSerialSeed := derive32(cfg, "transport/connect-ca/serial")
	leafKeySeed := derive32(cfg, "transport/operator-1-connect/key")
	leafSerialSeed := derive32(cfg, "transport/operator-1-connect/serial")
	if caCertificate.SerialNumber.Cmp(operatorConnectCertificateSerial(caSerialSeed)) != 0 || caCertificate.SerialNumber.Cmp(operatorConnectCertificateSerial(caKeySeed)) == 0 {
		t.Fatal("CA serial is not isolated from the CA private-key derivation domain")
	}
	if leafCertificate.SerialNumber.Cmp(operatorConnectCertificateSerial(leafSerialSeed)) != 0 || leafCertificate.SerialNumber.Cmp(operatorConnectCertificateSerial(leafKeySeed)) == 0 {
		t.Fatal("leaf serial is not isolated from the leaf private-key derivation domain")
	}
	caSecond, leafSecond, keySecond, err := operatorConnectTLSArtifacts(cfg, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(caFirst, caSecond) || bytes.Equal(leafFirst, leafSecond) || bytes.Equal(keyFirst, keySecond) {
		t.Fatal("operators do not share exactly one CA with distinct leaf identities")
	}
	for _, invalid := range []int{-1, 0, cfg.Config.Topology.Operators + 1} {
		if _, _, _, err := operatorConnectTLSArtifacts(cfg, invalid); err == nil {
			t.Errorf("invalid operator %d received a TLS identity", invalid)
		}
	}
}

func TestRenderedOperatorConnectTLSLoadsThroughRealServerAndPinnedClient(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		if err := renderOperatorConnectTLS(cfg, stateDir, operator); err != nil {
			t.Fatalf("render operator %d: %v", operator, err)
		}
	}
	operator := 1
	host := operatorConnectHostIP(operator)
	operatorRoot := filepath.Join(stateDir, "runtime", "operator-1")
	t.Setenv("WARP_VAULT_HOME", filepath.Join(operatorRoot, "vault"))
	t.Setenv(connect.ExtraRootCAFileEnv, operatorConnectCAFile(stateDir))
	clientTLS, err := connect.DefaultTlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	transportTLS := serverpkg.NewTransportTls(
		map[string]bool{host: true},
		&serverpkg.TransportTlsSettings{DefaultHostName: host},
	)
	serverTLS, err := transportTLS.GetTlsConfigForClient(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("real server loader rejected an IP client without SNI: %v", err)
	}
	if len(serverTLS.Certificates) != 1 || len(serverTLS.Certificates[0].Certificate) == 0 {
		t.Fatalf("real server loader returned incomplete TLS config: %+v", serverTLS)
	}
	leaf, err := x509.ParseCertificate(serverTLS.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: clientTLS.RootCAs, DNSName: host,
		CurrentTime: operatorConnectCertificateNotBefore.Add(time.Hour),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("real pinned client rejected rendered server identity: %v", err)
	}
	if err := leaf.VerifyHostname(operatorConnectHostIP(2)); err == nil {
		t.Fatal("operator-1 certificate authenticated operator-2 IP")
	}
}

func TestOperatorConnectTLSValidationRejectsAdjacentTamperingAndModes(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	if err := renderOperatorConnectTLS(cfg, stateDir, 1); err != nil {
		t.Fatal(err)
	}
	host := operatorConnectHostIP(1)
	leafPath := filepath.Join(stateDir, "runtime", "operator-1", "vault", "tls", host, host+".crt")
	originalLeaf, err := os.ReadFile(leafPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leafPath, append(append([]byte(nil), originalLeaf...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateOperatorConnectTLSArtifacts(cfg, stateDir, 1); err == nil {
		t.Fatal("trailing certificate data was accepted")
	}
	if err := renderOperatorConnectTLS(cfg, stateDir, 1); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(stateDir, "runtime", "operator-1", "vault", "tls", host, host+".key")
	if err := os.Chmod(keyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := validateOperatorConnectTLSArtifacts(cfg, stateDir, 1); err == nil {
		t.Fatal("group-readable operator TLS key was accepted")
	}
}
