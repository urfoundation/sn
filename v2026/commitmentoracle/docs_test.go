package commitmentoracle

// The operator docs must state the fleet commitment format and that nothing
// else is mirrored, so nobody mirrors a validator's source commitment by hand
// again (2026-10-15).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The text of the markdown section that starts with heading, up to the next
// heading of the same level.
func markdownSection(t *testing.T, path string, heading string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("%s has no %q", path, heading)
	}
	section := text[start+1:]
	level := heading[:strings.Index(heading, " ")+1]
	if end := strings.Index(section[len(heading):], "\n"+level); end >= 0 {
		section = section[:len(heading)+end]
	}
	return strings.Join(strings.Fields(section), " ")
}

func TestDocsStateFleetCommitmentFormat(t *testing.T) {
	launch := markdownSection(t, filepath.Join("..", "mainnet", "LAUNCH.md"), "## Commitment oracle")
	for _, phrase := range []string{
		"exactly one `Data::Raw40` field holding `urfleet1` followed by the 32-byte hash",
		"mirrors **only** that format",
		"never mirrors a one-field `Sha256` commitment",
		"it reads no role, permit or block body",
		"`provider fleet publish`",
		"9,360,673",
		"`legacy_sha256_commitments`",
	} {
		if !strings.Contains(launch, phrase) {
			t.Errorf("LAUNCH.md Commitment oracle does not say %q", phrase)
		}
	}
	for _, stale := range []string{"`fleet_format_excluded`", "`ValidatorPermit(netuid)`"} {
		if strings.Contains(launch, stale) {
			t.Errorf("LAUNCH.md Commitment oracle still says %q", stale)
		}
	}
	mainnet := markdownSection(t, filepath.Join("..", "mainnet", "MAINNET.md"), "## Fleet commitments get their own format — October 15")
	for _, phrase := range []string{
		"exactly one `Raw40` field holding the ASCII tag `urfleet1` followed by the 32-byte commitment hash",
		"mirrors **only** that format",
		"must republish",
	} {
		if !strings.Contains(mainnet, phrase) {
			t.Errorf("MAINNET.md October 15 does not say %q", phrase)
		}
	}
}
