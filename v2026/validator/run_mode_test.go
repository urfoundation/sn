package validator

import (
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
)

func parseValidatorArgsForTest(t *testing.T, args []string) (docopt.Opts, error) {
	t.Helper()
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	return parser.ParseArgs(mainUsage(), args, "test")
}

func TestWeightWritingCLIRequiresReleaseConfig(t *testing.T) {
	if _, err := parseValidatorArgsForTest(t, []string{"run", "--config=/tmp/release.yml"}); err != nil {
		t.Fatalf("release config mode did not parse: %v", err)
	}
	if _, err := parseValidatorArgsForTest(t, []string{"run", "--substrate=ws://127.0.0.1:9944", "--netuid=7"}); err == nil {
		t.Fatal("legacy steering flags still form a runnable CLI path")
	}
	opts := docopt.Opts{"--substrate": []string{"ws://127.0.0.1:9944"}, "--netuid": "7"}
	if err := rejectLegacySteeringOptions(opts); err == nil || !strings.Contains(err.Error(), "--config") {
		t.Fatalf("legacy steering defense did not fail closed: %v", err)
	}
	measurement, err := parseValidatorArgsForTest(t, []string{"run", "--api_url=http://127.0.0.1:8080", "--connect_url=ws://127.0.0.1:8081"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectLegacySteeringOptions(measurement); err != nil {
		t.Fatalf("measurement-only legacy mode rejected: %v", err)
	}
}
