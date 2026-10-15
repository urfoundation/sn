package commitmentoracle

// The operator docs must state the validator exclusion, so nobody mirrors a
// validator's source commitment by hand again (2026-10-15).

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

func TestDocsStateValidatorExclusion(t *testing.T) {
	launch := markdownSection(t, filepath.Join("..", "mainnet", "LAUNCH.md"), "## Commitment oracle")
	for _, phrase := range []string{
		"never mirrors a hotkey that holds a validator permit, is unregistered, or has no standalone fleet `set_commitment`",
		"if a role cannot be read, it mirrors nothing in that cycle",
		"`ValidatorPermit(netuid)`",
		"positive fleet evidence",
		"`fleet_format_excluded`",
	} {
		if !strings.Contains(launch, phrase) {
			t.Errorf("LAUNCH.md Commitment oracle does not say %q", phrase)
		}
	}
	mainnet := markdownSection(t, filepath.Join("..", "mainnet", "MAINNET.md"), "## Commitment oracle excludes validators — October 15")
	for _, phrase := range []string{
		"never mirrors a hotkey that holds a validator permit, is unregistered, or has no standalone fleet `set_commitment`",
		"mirrors nothing in that cycle",
	} {
		if !strings.Contains(mainnet, phrase) {
			t.Errorf("MAINNET.md October 15 does not say %q", phrase)
		}
	}
}
