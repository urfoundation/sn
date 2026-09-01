package main

// transport_tls.go creates the private loopback PKI used only by the real
// simulator topology. Each connect server receives an IP-SAN leaf while every
// miner and validator adds the common CA to Connect's normal pinned roots.

import (
	"crypto/ed25519"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	operatorConnectCertificateNotBefore = time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	operatorConnectCertificateNotAfter  = time.Date(2035, time.January, 1, 0, 0, 0, 0, time.UTC)
)

// Keeps serials positive, nonzero, stable, and within x509's 20-byte limit.
func operatorConnectCertificateSerial(seed [32]byte) *big.Int {
	serialBytes := append([]byte(nil), seed[:20]...)
	serialBytes[0] &= 0x7f
	serial := new(big.Int).SetBytes(serialBytes)
	if serial.Sign() == 0 {
		return big.NewInt(1)
	}
	return serial
}

// Returns the public CA path inherited by every unprivileged real client.
func operatorConnectCAFile(stateDir string) string {
	return filepath.Join(stateDir, "runtime", "connect-ca.crt")
}

// Produces deterministic Ed25519 certificates so resume and independent
// rendering retain the exact same trust boundary without storing a CA key.
func operatorConnectTLSArtifacts(cfg *ResolvedConfig, operator int) (caCertificatePem, leafCertificatePem, leafPrivateKeyPem []byte, err error) {
	if cfg == nil || cfg.Config == nil || operator < 1 || operator > cfg.Config.Topology.Operators {
		return nil, nil, nil, errors.New("invalid operator connect TLS request")
	}
	host := operatorConnectHostIP(operator)
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || !ip.IsLoopback() {
		return nil, nil, nil, fmt.Errorf("operator %d connect host %q is not an IPv4 loopback address", operator, host)
	}

	caSeed := derive32(cfg, "transport/connect-ca")
	caPrivateKey := ed25519.NewKeyFromSeed(caSeed[:])
	caTemplate := &x509.Certificate{
		SerialNumber:          operatorConnectCertificateSerial(caSeed),
		Subject:               pkix.Name{CommonName: cfg.Config.Deployment.DeploymentID + " connect CA"},
		NotBefore:             operatorConnectCertificateNotBefore,
		NotAfter:              operatorConnectCertificateNotAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDer, err := x509.CreateCertificate(nil, caTemplate, caTemplate, caPrivateKey.Public(), caPrivateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create operator connect CA: %w", err)
	}
	caCertificate, err := x509.ParseCertificate(caDer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse operator connect CA: %w", err)
	}

	leafSeed := derive32(cfg, fmt.Sprintf("transport/operator-%d-connect", operator))
	leafPrivateKey := ed25519.NewKeyFromSeed(leafSeed[:])
	leafTemplate := &x509.Certificate{
		SerialNumber: operatorConnectCertificateSerial(leafSeed),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    operatorConnectCertificateNotBefore,
		NotAfter:     operatorConnectCertificateNotAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{append(net.IP(nil), ip.To4()...)},
	}
	leafDer, err := x509.CreateCertificate(nil, leafTemplate, caCertificate, leafPrivateKey.Public(), caPrivateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create operator %d connect certificate: %w", operator, err)
	}
	leafPrivateKeyDer, err := x509.MarshalPKCS8PrivateKey(leafPrivateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal operator %d connect key: %w", operator, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDer}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDer}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafPrivateKeyDer}), nil
}

// Installs one exact-IP server leaf and the matching config allowlist. The CA
// is public; leaf keys stay inside each private operator vault overlay.
func renderOperatorConnectTLS(cfg *ResolvedConfig, stateDir string, operator int) error {
	caCertificatePem, leafCertificatePem, leafPrivateKeyPem, err := operatorConnectTLSArtifacts(cfg, operator)
	if err != nil {
		return err
	}
	host := operatorConnectHostIP(operator)
	if err := atomicWrite(operatorConnectCAFile(stateDir), caCertificatePem, 0o644); err != nil {
		return err
	}
	operatorRoot := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator))
	certificateDirectory := filepath.Join(operatorRoot, "vault", "tls", host)
	if err := atomicWrite(filepath.Join(certificateDirectory, host+".crt"), leafCertificatePem, 0o600); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(certificateDirectory, host+".key"), leafPrivateKeyPem, 0o600); err != nil {
		return err
	}
	configBytes, err := yaml.Marshal(map[string]any{"allowed_hosts": []string{host}})
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(operatorRoot, "config", "tls.yml"), configBytes, 0o600); err != nil {
		return err
	}
	certificateInfo, err := os.Stat(filepath.Join(certificateDirectory, host+".key"))
	if err != nil {
		return err
	}
	if certificateInfo.Mode().Perm()&0o077 != 0 {
		return errors.New("operator connect TLS private key is not private")
	}
	return nil
}
