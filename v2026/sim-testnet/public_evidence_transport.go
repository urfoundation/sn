package main

// Public evidence transport is intentionally separate from the operator's
// private control-plane transport. The normal profile accepts only public
// HTTPS origins. The loopback exception exists solely for this simulator's
// two fixed Bittensor-testnet operator listeners and cannot authorize another
// HTTP host, port, chain, or genesis.

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
)

const (
	publicEvidenceTransportHTTPS               = "public-https-v1"
	publicEvidenceTransportTestnetLoopbackHTTP = "testnet-loopback-http-v1"
)

var testnetLoopbackEvidenceOrigins = [...]string{
	"http://127.0.0.1:18081",
	"http://127.0.0.1:18082",
}

type campaignArtifactOriginAllowlist struct {
	profile     string
	chainID     uint64
	genesisHash string
	origins     map[string]bool
}

func validatePublicEvidenceTransportProfile(profile string, chainID uint64, genesisHash string) error {
	switch profile {
	case publicEvidenceTransportHTTPS:
		return nil
	case publicEvidenceTransportTestnetLoopbackHTTP:
		if chainID != testnetChainID || !strings.EqualFold(genesisHash, testnetGenesis) {
			return errors.New("testnet loopback evidence transport is bound to the pinned Bittensor testnet identity")
		}
		return nil
	default:
		return fmt.Errorf("public evidence transport profile %q is unsupported", profile)
	}
}

func parsePublicEvidenceURL(raw string) (*url.URL, error) {
	if raw == "" || raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\\\r\n\x00") {
		return nil, errors.New("public evidence URL is empty or unsafe")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Fragment != "" {
		return nil, errors.New("public evidence URL is not canonical and credential-free")
	}
	if parsed.RawPath != "" {
		return nil, errors.New("public evidence URL has an encoded path")
	}
	return parsed, nil
}

// campaignHTTPSOrigin retains the public/default SSRF boundary: localhost and
// every literal IP remain ineligible even when wrapped in TLS.
func campaignHTTPSOrigin(raw string) (string, error) {
	parsed, err := parsePublicEvidenceURL(raw)
	if err != nil || parsed.Scheme != "https" {
		return "", errors.New("campaign artifact origin is not credential-free HTTPS")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || hostname == "localhost" || hostname == "localhost." || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".localhost.") || net.ParseIP(strings.TrimSuffix(hostname, ".")) != nil {
		return "", errors.New("campaign artifact origin uses a local or literal IP host")
	}
	return "https://" + strings.ToLower(parsed.Host), nil
}

func testnetLoopbackEvidenceOrigin(raw string, chainID uint64, genesisHash string) (string, error) {
	if err := validatePublicEvidenceTransportProfile(publicEvidenceTransportTestnetLoopbackHTTP, chainID, genesisHash); err != nil {
		return "", err
	}
	parsed, err := parsePublicEvidenceURL(raw)
	if err != nil || parsed.Scheme != "http" {
		return "", errors.New("testnet loopback evidence URL is not credential-free HTTP")
	}
	origin := "http://" + strings.ToLower(parsed.Host)
	for _, allowed := range testnetLoopbackEvidenceOrigins {
		if origin == allowed {
			return origin, nil
		}
	}
	return "", fmt.Errorf("testnet loopback evidence origin %q is not one of the two fixed operator origins", origin)
}

func publicEvidenceOrigin(raw, profile string, chainID uint64, genesisHash string) (string, error) {
	if err := validatePublicEvidenceTransportProfile(profile, chainID, genesisHash); err != nil {
		return "", err
	}
	parsed, err := parsePublicEvidenceURL(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "https":
		return campaignHTTPSOrigin(raw)
	case "http":
		if profile != publicEvidenceTransportTestnetLoopbackHTTP {
			return "", errors.New("plaintext public evidence transport is disabled")
		}
		return testnetLoopbackEvidenceOrigin(raw, chainID, genesisHash)
	default:
		return "", fmt.Errorf("public evidence URL scheme %q is unsupported", parsed.Scheme)
	}
}

func canonicalBarePublicEvidenceOrigin(raw, profile string, chainID uint64, genesisHash string) (string, error) {
	parsed, err := parsePublicEvidenceURL(raw)
	if err != nil || parsed.RawQuery != "" || parsed.ForceQuery || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("operator API URL is not a bare public evidence origin")
	}
	origin, err := publicEvidenceOrigin(raw, profile, chainID, genesisHash)
	if err != nil {
		return "", err
	}
	return origin, nil
}

func publicEvidenceTransportForOrigins(origins []string, operators int, chainID uint64, genesisHash, network string) (string, []string, error) {
	if len(origins) != operators {
		return "", nil, fmt.Errorf("operator API origins has %d entries, want %d", len(origins), operators)
	}
	if operators == len(testnetLoopbackEvidenceOrigins) {
		canonical := make([]string, len(origins))
		exact := true
		for index, raw := range origins {
			origin, err := canonicalBarePublicEvidenceOrigin(raw, publicEvidenceTransportTestnetLoopbackHTTP, chainID, genesisHash)
			if err != nil || origin != testnetLoopbackEvidenceOrigins[index] {
				exact = false
				break
			}
			canonical[index] = origin
		}
		if exact {
			if network != "" && network != "bittensor-testnet" {
				return "", nil, errors.New("loopback public evidence transport is forbidden outside bittensor-testnet")
			}
			return publicEvidenceTransportTestnetLoopbackHTTP, canonical, nil
		}
	}

	canonical := make([]string, len(origins))
	seen := make(map[string]bool, len(origins))
	for index, raw := range origins {
		origin, err := canonicalBarePublicEvidenceOrigin(raw, publicEvidenceTransportHTTPS, chainID, genesisHash)
		if err != nil {
			return "", nil, fmt.Errorf("operator API origin %d: %w", index+1, err)
		}
		if seen[origin] {
			return "", nil, fmt.Errorf("operator API origin %d duplicates %q", index+1, origin)
		}
		seen[origin] = true
		canonical[index] = origin
	}
	return publicEvidenceTransportHTTPS, canonical, nil
}

func resolvedPublicEvidenceTransportProfile(cfg *ResolvedConfig) (string, error) {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil {
		return "", errors.New("resolved public evidence transport context is incomplete")
	}
	profile, _, err := publicEvidenceTransportForOrigins(
		cfg.OperatorAPIOrigins,
		cfg.Config.Topology.Operators,
		cfg.ChainID,
		cfg.Public.Chain.GenesisHash,
		cfg.Config.Deployment.Network,
	)
	return profile, err
}

// effectivePublicEvidenceTransportProfile accepts a legacy manifest without
// an explicit field only when its already signed origin tuple unambiguously
// derives one of the two profiles. This preserves the active testnet campaign
// without turning a missing field into an open-ended HTTP compatibility mode.
func effectivePublicEvidenceTransportProfile(public *PublicDeploymentManifest) (string, error) {
	if public == nil {
		return "", errors.New("public deployment manifest is missing")
	}
	if public.EvidenceTransportProfile != "" {
		if err := validatePublicEvidenceTransportProfile(public.EvidenceTransportProfile, public.ChainID, public.GenesisHash); err != nil {
			return "", err
		}
		for _, operator := range public.Operators {
			if _, err := publicEvidenceOrigin(operator.APIURL, public.EvidenceTransportProfile, public.ChainID, public.GenesisHash); err != nil {
				return "", fmt.Errorf("public deployment manifest operator %d transport: %w", operator.NoID, err)
			}
		}
		return public.EvidenceTransportProfile, nil
	}
	origins := make([]string, len(public.Operators))
	seen := make(map[int]bool, len(public.Operators))
	for _, operator := range public.Operators {
		if operator.NoID < 1 || operator.NoID > len(origins) || seen[operator.NoID] {
			return "", errors.New("public deployment manifest operator identity is invalid")
		}
		seen[operator.NoID] = true
		origins[operator.NoID-1] = operator.APIURL
	}
	derived, _, err := publicEvidenceTransportForOrigins(origins, len(public.Operators), public.ChainID, public.GenesisHash, "")
	if err != nil {
		return "", err
	}
	return derived, nil
}

func validatePublicEvidenceManifestTransportAgainstConfig(cfg *ResolvedConfig, public *PublicDeploymentManifest) error {
	configuredProfile, err := resolvedPublicEvidenceTransportProfile(cfg)
	if err != nil {
		return err
	}
	if err := validatePublicCampaignOperatorOrigins(public); err != nil {
		return err
	}
	publicProfile, err := effectivePublicEvidenceTransportProfile(public)
	if err != nil || publicProfile != configuredProfile {
		return stateMismatchError(err, "public manifest evidence transport profile does not match the configured origin tuple")
	}
	if len(public.Operators) != len(cfg.OperatorAPIOrigins) {
		return errors.New("public manifest operator origins do not match the configured origin tuple")
	}
	seen := make(map[int]bool, len(public.Operators))
	for _, operator := range public.Operators {
		if operator.NoID < 1 || operator.NoID > len(cfg.OperatorAPIOrigins) || seen[operator.NoID] {
			return errors.New("public manifest operator directory is invalid")
		}
		seen[operator.NoID] = true
		origin, originErr := canonicalBarePublicEvidenceOrigin(operator.APIURL, publicProfile, public.ChainID, public.GenesisHash)
		if originErr != nil {
			return fmt.Errorf("public manifest operator %d origin: %w", operator.NoID, originErr)
		}
		if origin != cfg.OperatorAPIOrigins[operator.NoID-1] {
			return fmt.Errorf("public manifest operator %d origin does not match the configured origin tuple", operator.NoID)
		}
	}
	return nil
}

func campaignArtifactAllowedOriginsForConfig(cfg *ResolvedConfig) (campaignArtifactOriginAllowlist, error) {
	profile, err := resolvedPublicEvidenceTransportProfile(cfg)
	if err != nil {
		return campaignArtifactOriginAllowlist{}, err
	}
	allowed := campaignArtifactOriginAllowlist{profile: profile, chainID: cfg.ChainID, genesisHash: cfg.Public.Chain.GenesisHash, origins: map[string]bool{}}
	for _, raw := range cfg.OperatorAPIOrigins {
		origin, err := publicEvidenceOrigin(raw, profile, allowed.chainID, allowed.genesisHash)
		if err != nil {
			return campaignArtifactOriginAllowlist{}, err
		}
		allowed.origins[origin] = true
	}
	return allowed, nil
}

func campaignArtifactAllowedOrigins(public *PublicDeploymentManifest, manifestURI string) (campaignArtifactOriginAllowlist, error) {
	profile, err := effectivePublicEvidenceTransportProfile(public)
	if err != nil {
		return campaignArtifactOriginAllowlist{}, err
	}
	allowed := campaignArtifactOriginAllowlist{profile: profile, chainID: public.ChainID, genesisHash: public.GenesisHash, origins: map[string]bool{}}
	for _, operator := range public.Operators {
		origin, err := publicEvidenceOrigin(operator.APIURL, profile, public.ChainID, public.GenesisHash)
		if err != nil {
			return campaignArtifactOriginAllowlist{}, err
		}
		allowed.origins[origin] = true
	}
	if manifestURI != "" {
		origin, err := publicEvidenceOrigin(manifestURI, profile, public.ChainID, public.GenesisHash)
		if err != nil {
			return campaignArtifactOriginAllowlist{}, err
		}
		allowed.origins[origin] = true
	}
	return allowed, nil
}

func validateCampaignArtifactOrigin(raw string, allowed campaignArtifactOriginAllowlist) error {
	if len(allowed.origins) == 0 {
		return errors.New("campaign artifact origin allowlist is empty")
	}
	origin, err := publicEvidenceOrigin(raw, allowed.profile, allowed.chainID, allowed.genesisHash)
	if err != nil {
		return err
	}
	if !allowed.origins[origin] {
		return fmt.Errorf("campaign artifact origin %q is not in the authenticated public manifest allowlist", origin)
	}
	return nil
}

func verifyPublicEvidenceObjectURI(label, raw, profile string, chainID uint64, genesisHash string) error {
	if _, err := publicEvidenceOrigin(raw, profile, chainID, genesisHash); err != nil {
		return fmt.Errorf("%s URI: %w", label, err)
	}
	parsed, _ := url.Parse(raw)
	if parsed.Path == "" || path.Clean(parsed.Path) != parsed.Path || parsed.ForceQuery {
		return fmt.Errorf("%s URI path is empty or non-canonical", label)
	}
	return nil
}

func publicEvidenceTransportForURI(raw string, chainID uint64, genesisHash string) (string, error) {
	parsed, err := parsePublicEvidenceURL(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "https":
		if _, err := publicEvidenceOrigin(raw, publicEvidenceTransportHTTPS, chainID, genesisHash); err != nil {
			return "", err
		}
		return publicEvidenceTransportHTTPS, nil
	case "http":
		if _, err := publicEvidenceOrigin(raw, publicEvidenceTransportTestnetLoopbackHTTP, chainID, genesisHash); err != nil {
			return "", err
		}
		return publicEvidenceTransportTestnetLoopbackHTTP, nil
	default:
		return "", fmt.Errorf("public evidence URL scheme %q is unsupported", parsed.Scheme)
	}
}
