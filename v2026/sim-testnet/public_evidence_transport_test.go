package main

import (
	"strings"
	"testing"
)

func loopbackEvidenceManifestForTest(explicit bool) *PublicDeploymentManifest {
	manifest := &PublicDeploymentManifest{
		ChainID:     testnetChainID,
		GenesisHash: testnetGenesis,
		Topology:    TopologyConfig{Operators: 2},
		Operators: []PublicOperator{
			{NoID: 1, APIURL: testnetLoopbackEvidenceOrigins[0], VerifyURL: testnetLoopbackEvidenceOrigins[0] + "/verify", HistoryURL: testnetLoopbackEvidenceOrigins[0] + "/sn/evidence/history"},
			{NoID: 2, APIURL: testnetLoopbackEvidenceOrigins[1], VerifyURL: testnetLoopbackEvidenceOrigins[1] + "/verify", HistoryURL: testnetLoopbackEvidenceOrigins[1] + "/sn/evidence/history"},
		},
	}
	if explicit {
		manifest.EvidenceTransportProfile = publicEvidenceTransportTestnetLoopbackHTTP
	}
	return manifest
}

func TestPublicEvidenceTransportDerivesOnlyExactTestnetLoopbackTuple(t *testing.T) {
	profile, origins, err := publicEvidenceTransportForOrigins(
		testnetLoopbackEvidenceOrigins[:], 2, testnetChainID, testnetGenesis, "bittensor-testnet",
	)
	if err != nil || profile != publicEvidenceTransportTestnetLoopbackHTTP || len(origins) != 2 || origins[0] != testnetLoopbackEvidenceOrigins[0] || origins[1] != testnetLoopbackEvidenceOrigins[1] {
		t.Fatalf("exact loopback profile = %q %v, %v", profile, origins, err)
	}

	tests := []struct {
		name    string
		origins []string
		chainID uint64
		genesis string
		network string
	}{
		{name: "reversed", origins: []string{testnetLoopbackEvidenceOrigins[1], testnetLoopbackEvidenceOrigins[0]}, chainID: testnetChainID, genesis: testnetGenesis, network: "bittensor-testnet"},
		{name: "wrong port", origins: []string{testnetLoopbackEvidenceOrigins[0], "http://127.0.0.1:18083"}, chainID: testnetChainID, genesis: testnetGenesis, network: "bittensor-testnet"},
		{name: "encoded slash", origins: []string{testnetLoopbackEvidenceOrigins[0] + "/%2f", testnetLoopbackEvidenceOrigins[1]}, chainID: testnetChainID, genesis: testnetGenesis, network: "bittensor-testnet"},
		{name: "non-loopback HTTP", origins: []string{"http://operator-1.example", "http://operator-2.example"}, chainID: testnetChainID, genesis: testnetGenesis, network: "bittensor-testnet"},
		{name: "localhost", origins: []string{"http://localhost:18081", "http://localhost:18082"}, chainID: testnetChainID, genesis: testnetGenesis, network: "bittensor-testnet"},
		{name: "wrong chain", origins: testnetLoopbackEvidenceOrigins[:], chainID: 1, genesis: testnetGenesis, network: "bittensor-testnet"},
		{name: "wrong genesis", origins: testnetLoopbackEvidenceOrigins[:], chainID: testnetChainID, genesis: "0x" + strings.Repeat("00", 32), network: "bittensor-testnet"},
		{name: "mainnet network", origins: testnetLoopbackEvidenceOrigins[:], chainID: testnetChainID, genesis: testnetGenesis, network: "bittensor-mainnet"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := publicEvidenceTransportForOrigins(test.origins, 2, test.chainID, test.genesis, test.network); err == nil {
				t.Fatal("unsafe HTTP origin tuple was accepted")
			}
		})
	}
}

func TestPublicEvidenceTransportKeepsPublicHTTPSAndSSRFFailClosed(t *testing.T) {
	profile, origins, err := publicEvidenceTransportForOrigins(
		[]string{"https://operator-1.example/", "https://operator-2.example"}, 2, 1, "mainnet-genesis", "bittensor-mainnet",
	)
	if err != nil || profile != publicEvidenceTransportHTTPS || origins[0] != "https://operator-1.example" {
		t.Fatalf("public HTTPS profile = %q %v, %v", profile, origins, err)
	}
	for _, origins := range [][]string{
		{"https://127.0.0.1:18081", "https://operator-2.example"},
		{"https://localhost:18081", "https://operator-2.example"},
		{"https://[::1]:18081", "https://operator-2.example"},
		{"http://operator-1.example", "https://operator-2.example"},
	} {
		if _, _, err := publicEvidenceTransportForOrigins(origins, 2, 1, "mainnet-genesis", "bittensor-mainnet"); err == nil {
			t.Fatalf("unsafe mainnet origin tuple accepted: %v", origins)
		}
	}
}

func TestSignedPublicManifestTransportProfileMatchesIdentityAndOrigins(t *testing.T) {
	legacy := loopbackEvidenceManifestForTest(false)
	profile, err := effectivePublicEvidenceTransportProfile(legacy)
	if err != nil || profile != publicEvidenceTransportTestnetLoopbackHTTP {
		t.Fatalf("legacy exact signed tuple profile = %q, %v", profile, err)
	}
	if err := validatePublicCampaignOperatorOrigins(legacy); err != nil {
		t.Fatalf("legacy exact signed tuple rejected: %v", err)
	}

	explicit := loopbackEvidenceManifestForTest(true)
	if err := validatePublicCampaignOperatorOrigins(explicit); err != nil {
		t.Fatalf("explicit loopback transport rejected: %v", err)
	}
	equivalent, err := publicManifestEquivalent(legacy, explicit)
	if err != nil || !equivalent {
		t.Fatalf("legacy and explicit exact transports equivalent=%t, err=%v", equivalent, err)
	}

	for name, mutate := range map[string]func(*PublicDeploymentManifest){
		"profile downgrade":  func(value *PublicDeploymentManifest) { value.EvidenceTransportProfile = publicEvidenceTransportHTTPS },
		"wrong chain":        func(value *PublicDeploymentManifest) { value.ChainID = 1 },
		"wrong genesis":      func(value *PublicDeploymentManifest) { value.GenesisHash = "0x" + strings.Repeat("00", 32) },
		"duplicate operator": func(value *PublicDeploymentManifest) { value.Operators[1].NoID = 1 },
		"wrong port":         func(value *PublicDeploymentManifest) { value.Operators[1].APIURL = "http://127.0.0.1:18083" },
		"cross origin": func(value *PublicDeploymentManifest) {
			value.Operators[1].HistoryURL = testnetLoopbackEvidenceOrigins[0] + "/sn/evidence/history"
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := *explicit
			value.Operators = append([]PublicOperator(nil), explicit.Operators...)
			mutate(&value)
			if err := validatePublicCampaignOperatorOrigins(&value); err == nil {
				t.Fatal("inconsistent signed transport was accepted")
			}
		})
	}
}

func TestSignedPublicManifestTransportIsBoundToResolvedConfigOrigins(t *testing.T) {
	cfg := testResolvedConfig(t)
	public := &PublicDeploymentManifest{
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash,
		EvidenceTransportProfile: publicEvidenceTransportHTTPS,
		Topology:                 TopologyConfig{Operators: 2},
		Operators: []PublicOperator{
			{NoID: 1, APIURL: cfg.OperatorAPIOrigins[0], VerifyURL: cfg.OperatorAPIOrigins[0] + "/verify", HistoryURL: cfg.OperatorAPIOrigins[0] + "/sn/evidence/history"},
			{NoID: 2, APIURL: cfg.OperatorAPIOrigins[1], VerifyURL: cfg.OperatorAPIOrigins[1] + "/verify", HistoryURL: cfg.OperatorAPIOrigins[1] + "/sn/evidence/history"},
		},
	}
	if err := validatePublicEvidenceManifestTransportAgainstConfig(cfg, public); err != nil {
		t.Fatalf("matching config-bound HTTPS transport rejected: %v", err)
	}
	public.Operators[1].APIURL = "https://attacker.example"
	public.Operators[1].VerifyURL = "https://attacker.example/verify"
	public.Operators[1].HistoryURL = "https://attacker.example/sn/evidence/history"
	if err := validatePublicEvidenceManifestTransportAgainstConfig(cfg, public); err == nil {
		t.Fatal("signed HTTPS origin substitution was not rejected against resolved config")
	}

	cfg.OperatorAPIOrigins = append([]string(nil), testnetLoopbackEvidenceOrigins[:]...)
	loopback := loopbackEvidenceManifestForTest(true)
	if err := validatePublicEvidenceManifestTransportAgainstConfig(cfg, loopback); err != nil {
		t.Fatalf("matching config-bound loopback transport rejected: %v", err)
	}
}

func TestLoopbackCampaignArtifactAllowlistIsExact(t *testing.T) {
	public := loopbackEvidenceManifestForTest(true)
	hash := "sha256:" + strings.Repeat("11", 32)
	manifestURI := testnetLoopbackEvidenceOrigins[0] + "/sn/evidence?hash=" + hash
	allowed, err := campaignArtifactAllowedOrigins(public, manifestURI)
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{
		testnetLoopbackEvidenceOrigins[0] + "/sn/evidence?hash=" + hash,
		testnetLoopbackEvidenceOrigins[1] + "/sn/evidence/history",
	} {
		if err := validateCampaignArtifactOrigin(uri, allowed); err != nil {
			t.Errorf("allowed loopback evidence URI %q rejected: %v", uri, err)
		}
	}
	locatorURI := testnetLoopbackEvidenceOrigins[1] + "/sn/artifact?hash=" + hash
	locator := []byte(`{"kind":"proof","uri":"` + locatorURI + `","content_sha256":"` + hash + `","size_bytes":1}`)
	references, err := campaignArtifactReferences(map[string][]byte{"locator.json": locator})
	if err != nil || references[locatorURI].URI != locatorURI {
		t.Fatalf("loopback campaign artifact locator was not collected: %v, %v", references, err)
	}
	if err := validateCampaignArtifactOrigin(locatorURI, allowed); err != nil {
		t.Fatalf("collected loopback campaign artifact locator was not admitted by its signed profile: %v", err)
	}
	for _, uri := range []string{
		"http://127.0.0.1:18083/sn/evidence?hash=" + hash,
		"http://127.0.0.2:18081/sn/evidence?hash=" + hash,
		"http://localhost:18081/sn/evidence?hash=" + hash,
		"http://operator.example/sn/evidence?hash=" + hash,
		"http://127.0.0.1:18081@attacker.example/sn/evidence?hash=" + hash,
		"https://attacker.example/sn/evidence?hash=" + hash,
	} {
		if err := validateCampaignArtifactOrigin(uri, allowed); err == nil {
			t.Errorf("unauthorized evidence URI %q was accepted", uri)
		}
	}
}

func TestFinalEvidenceURIRequiresMatchingSignedTransportProfile(t *testing.T) {
	hash := "sha256:" + strings.Repeat("22", 32)
	loopback := testnetLoopbackEvidenceOrigins[0] + "/sn/evidence?hash=" + hash
	if err := verifyFinalEvidenceURI("test", loopback, publicEvidenceTransportTestnetLoopbackHTTP, testnetChainID, testnetGenesis); err != nil {
		t.Fatalf("exact testnet loopback final URI rejected: %v", err)
	}
	if err := verifyFinalEvidenceURI("test", "https://evidence.example/object?hash="+hash, publicEvidenceTransportHTTPS, 1, "mainnet-genesis"); err != nil {
		t.Fatalf("public HTTPS final URI rejected: %v", err)
	}
	for name, input := range map[string]struct {
		uri     string
		profile string
		chainID uint64
		genesis string
	}{
		"TLS profile":       {uri: loopback, profile: publicEvidenceTransportHTTPS, chainID: testnetChainID, genesis: testnetGenesis},
		"wrong chain":       {uri: loopback, profile: publicEvidenceTransportTestnetLoopbackHTTP, chainID: 1, genesis: testnetGenesis},
		"wrong genesis":     {uri: loopback, profile: publicEvidenceTransportTestnetLoopbackHTTP, chainID: testnetChainID, genesis: "mainnet"},
		"wrong port":        {uri: "http://127.0.0.1:18083/object?hash=" + hash, profile: publicEvidenceTransportTestnetLoopbackHTTP, chainID: testnetChainID, genesis: testnetGenesis},
		"non-loopback HTTP": {uri: "http://evidence.example/object?hash=" + hash, profile: publicEvidenceTransportTestnetLoopbackHTTP, chainID: testnetChainID, genesis: testnetGenesis},
		"literal HTTPS":     {uri: "https://127.0.0.1/object?hash=" + hash, profile: publicEvidenceTransportHTTPS, chainID: 1, genesis: "mainnet"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyFinalEvidenceURI("test", input.uri, input.profile, input.chainID, input.genesis); err == nil {
				t.Fatal("unsafe final evidence URI was accepted")
			}
		})
	}
}
