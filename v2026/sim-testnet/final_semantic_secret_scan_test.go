package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func finalSemanticLargeSecretRoles(count int) *RoleSecrets {
	roles := &RoleSecrets{EVM: map[string]EVMRoleSecret{}, Substrate: map[string]SubstrateRoleSecret{}, Clients: map[string]ClientRoleSecret{}}
	for index := 0; index < count; index++ {
		seed := fmt.Sprintf("%064x", index+1)
		roles.Clients[fmt.Sprintf("miner-%d", index+1)] = ClientRoleSecret{SeedHex: seed}
	}
	return roles
}

func TestFinalSemanticSecretMatcherNormalizesLargeHaystackOnceAndDeduplicates(t *testing.T) {
	roles := finalSemanticLargeSecretRoles(2_000)
	// Exercise case-insensitive deduplication across inventory classes too.
	roles.EVM["duplicate"] = EVMRoleSecret{PrivateKeyHex: strings.ToUpper(roles.Clients["miner-1"].SeedHex)}
	matcher := newFinalSemanticSecretMatcher(roles, roles.Clients["miner-2"].SeedHex)
	if got, want := len(matcher.needles), 2_000; got != want {
		t.Fatalf("deduplicated matcher needles=%d, want %d", got, want)
	}
	if len(matcher.nodes) == 0 {
		t.Fatal("large secret inventory did not compile a matcher automaton")
	}
	normalizations := 0
	matcher.lowercase = func(data []byte) []byte {
		normalizations++
		return bytes.ToLower(data)
	}
	large := bytes.Repeat([]byte("public-semantic-evidence\n"), 28_000) // > 650 KiB
	if err := matcher.scan("large.json", large); err != nil {
		t.Fatal(err)
	}
	if normalizations != 1 {
		t.Fatalf("large haystack normalizations=%d, want exactly 1", normalizations)
	}
	secret := strings.ToUpper(roles.Clients["miner-1999"].SeedHex)
	if err := matcher.scan("secret.json", append(large, secret...)); err == nil || !strings.Contains(err.Error(), "secret.json") {
		t.Fatalf("case-insensitive large secret was not detected: %v", err)
	}
	if normalizations != 2 {
		t.Fatalf("second haystack normalizations=%d, want exactly 2 total", normalizations)
	}
}

func TestFinalSemanticSecretMatcherPreservesOverlappingSubstringSemantics(t *testing.T) {
	matcher := newFinalSemanticSecretMatcher(nil,
		"abcdefgh", "bcdefghi", "abcdefghij", "XYZxyz12", "xyZXYz12",
		"short", "", "  ",
	)
	if got, want := len(matcher.needles), 4; got != want {
		t.Fatalf("normalized matcher needles=%d, want %d", got, want)
	}
	for _, test := range []struct {
		name    string
		content string
		match   bool
	}{
		{name: "exact", content: "abcdefgh", match: true},
		{name: "proper prefix needle", content: "--ABCDEFGHIJ--", match: true},
		{name: "proper suffix overlap", content: "aBCDEFGHi", match: true},
		{name: "case folded duplicate", content: "prefix-XyZxYz12-suffix", match: true},
		{name: "crosses ordinary prefix", content: "01234567abcdefgh89", match: true},
		{name: "one byte short", content: "abcdefg", match: false},
		{name: "near miss", content: "abcdxfghij", match: false},
		{name: "ignored short candidate", content: "short", match: false},
	} {
		err := matcher.scan(test.name, []byte(test.content))
		if test.match && err == nil {
			t.Fatalf("%s: secret substring was not detected", test.name)
		}
		if !test.match && err != nil {
			t.Fatalf("%s: public content was rejected: %v", test.name, err)
		}
	}
}

func TestFinalSemanticSecretMatcherMatchesReferenceSearchAcrossAdjacentInputs(t *testing.T) {
	candidates := []string{
		"aaaaaaaa", "aaaaaaab", "baaaaaaa", "abababab", "babababa",
		"0123456789abcdef", "FEDCBA9876543210", "mixedCASE-secret",
		"unicode-Ångström", "shared-prefix-one", "shared-prefix-two",
	}
	matcher := newFinalSemanticSecretMatcher(nil, candidates...)
	haystacks := [][]byte{
		{}, []byte("public evidence"), []byte("aaaaaaa"), []byte("aaaaaaaaa"),
		[]byte("xxABABABABxx"), []byte("prefix-mIxEdCaSe-SeCrEt-suffix"),
		[]byte("UNICODE-åNGSTRÖM"), []byte("shared-prefix-three"),
	}
	for index := 0; index < 256; index++ {
		candidate := []byte(fmt.Sprintf("public-%03d-%064x", index, index*index+17))
		if index%7 == 0 {
			candidate = append(candidate, candidates[index%len(candidates)]...)
		}
		haystacks = append(haystacks, candidate)
	}
	for index, haystack := range haystacks {
		normalized := bytes.ToLower(haystack)
		want := false
		for _, needle := range matcher.needles {
			if bytes.Contains(normalized, needle) {
				want = true
				break
			}
		}
		if got := matcher.containsNormalized(normalized); got != want {
			t.Fatalf("case %d automaton match=%t, reference=%t", index, got, want)
		}
	}
}

func TestFinalSemanticOutputSecretScannerCoversLargeDirectOutput(t *testing.T) {
	roles := finalSemanticLargeSecretRoles(2_000)
	scan := NewFinalSemanticSecretScanner(roles)
	large := bytes.Repeat([]byte("safe-final-output\n"), 42_000)
	if err := scan(finalSemanticEvidenceFilename, large); err != nil {
		t.Fatal(err)
	}
	secret := strings.ToUpper(roles.Clients["miner-2000"].SeedHex)
	if err := scan(finalSemanticMarkdownFilename, append(large, secret...)); err == nil || !strings.Contains(err.Error(), finalSemanticMarkdownFilename) {
		t.Fatalf("large direct output secret was not detected: %v", err)
	}
}

func TestFinalSemanticEvidenceTreeSecretScannerScansBundleEntriesAndPathsOnce(t *testing.T) {
	stateDir := t.TempDir()
	secret := "ThisIsAnEmbeddedSecretOnly"
	entryData := []byte("prefix-" + strings.ToUpper(secret) + "-suffix")
	bundle := FinalCollectedFileBundle{
		Schema: finalCollectedFileBundleSchema,
		Name:   "scanner",
		Files: []FinalCollectedFileBundleEntry{{
			Path: "nested/secret.bin", ContentHash: bytesSHA256(entryData), SizeBytes: uint64(len(entryData)), Data: entryData,
		}},
	}
	encoded, err := json.Marshal(&bundle)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(encoded), bytes.ToLower([]byte(secret))) {
		t.Fatal("fixture secret unexpectedly appears verbatim outside the base64 bundle entry")
	}
	bundlePath := filepath.Join(stateDir, "public", "bundle.json")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	matcher := newFinalSemanticSecretMatcher(nil, secret)
	normalizations := 0
	matcher.lowercase = func(data []byte) []byte {
		normalizations++
		return bytes.ToLower(data)
	}
	// stateDir as runDir deliberately overlaps every nested root. The visited
	// set must scan the outer file once; the second normalization is the exact
	// decoded bundle entry, not a duplicate filesystem traversal.
	err = scanEvidenceSecretsWithMatcher(stateDir, stateDir, matcher)
	if err == nil || !strings.Contains(err.Error(), "bundle.json[nested/secret.bin]") {
		t.Fatalf("embedded bundle secret was not attributed exactly: %v", err)
	}
	if normalizations != 2 {
		t.Fatalf("overlapping-root and bundle normalizations=%d, want outer+entry=2", normalizations)
	}
}
