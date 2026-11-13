//go:build linux || darwin

package validator

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// mainnet/SETTLEMENT-V2-ADOPTION.md as single-spaced text, and its last
// fenced command, the restart, as words with each $NAME bound.
func adoptionDocument(t *testing.T, variables map[string]string) (string, []string) {
	t.Helper()
	raw, err := os.ReadFile("../mainnet/SETTLEMENT-V2-ADOPTION.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```sh\n(.*?)```").FindAllSubmatch(raw, -1)
	if len(blocks) == 0 {
		t.Fatal("the adoption page has no command")
	}
	restart := regexp.MustCompile(`\$[A-Z][A-Z0-9_]*`).ReplaceAllStringFunc(string(blocks[len(blocks)-1][1]), func(name string) string {
		value, ok := variables[name[1:]]
		if !ok {
			t.Fatalf("the page's restart uses %s, which the test does not bind", name)
		}
		return value
	})
	return strings.Join(strings.Fields(string(raw)), " "), strings.Fields(strings.NewReplacer("\\\n", " ", `"`, "").Replace(restart))
}

// VA-3, step 10: the adoption page's restart command is the auditor drop-in's
// ExecStart, placeholder for variable. It parses with `validator run`'s real
// usage in release config mode and loads the auditor key.
func TestSettlementV2AdoptionDocumentRestartIsTheDropIn(t *testing.T) {
	raw, err := os.ReadFile("../mainnet/validator-ledger-auditor.example.conf")
	if err != nil {
		t.Fatal(err)
	}
	start := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if value, found := strings.CutPrefix(line, "ExecStart="); found && value != "" {
			start = value
		}
	}
	variables := map[string]string{"ADOPTED_CONFIG": "<adopted-config>", "PROGRESS_FILE": "<progress-file>", "VALIDATOR_DECLARATION": "<validator-declaration>",
		"VALIDATOR_DECLARATION_SHA256": "<validator-declaration-sha256>", "AUDITOR_KEY_FILE": "<O10-auditor-key-file>"}
	if _, restart := adoptionDocument(t, variables); len(restart) < 2 || restart[0] != "validator" || !slices.Equal(restart[1:], strings.Fields(start)[1:]) {
		t.Fatalf("page restart %q is not the drop-in's ExecStart %q", restart, start)
	}
	variables["AUDITOR_KEY_FILE"] = filepath.Join(t.TempDir(), "auditor.key")
	if err := os.WriteFile(variables["AUDITOR_KEY_FILE"], []byte(strings.Repeat("2b", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, restart := adoptionDocument(t, variables)
	opts, err := parseValidatorArgsForTest(t, restart[1:])
	if err != nil || !optBool(opts, "run") || optString(opts, "--config", "") != "<adopted-config>" || optString(opts, "--progress-file", "") != "<progress-file>" ||
		optString(opts, "--durable-volumes", "") != "<validator-declaration>" || optString(opts, "--durable-volumes-sha256", "") != "<validator-declaration-sha256>" {
		t.Fatalf("page restart %q = %v, %v", restart, opts, err)
	}
	if options, err := settlementV2RuntimeOptionsFromOpts(opts); err != nil || options.AuditorKeyFile != variables["AUDITOR_KEY_FILE"] {
		t.Fatalf("page restart auditor options %+v: %v", options, err)
	}
}

// VA-6, step 10: the adoption page names what the producer loader gives for
// each wrong adoption: another change riding with the block, an approval
// changed beyond its config hash and a second appended entry. The loader never
// reaches the class's own history check, and it admits a config that adds the
// block and appends no entry, whose retained decisions the validator then
// refuses; the page says both. The exact adoption loads with the bundle that
// `validator run` retains, under the name the page gives, as its entry.
func TestSettlementV2AdoptionDocumentNamesTheLoaderRefusals(t *testing.T) {
	page, _ := adoptionDocument(t, map[string]string{"ADOPTED_CONFIG": "", "PROGRESS_FILE": "", "VALIDATOR_DECLARATION": "", "VALIDATOR_DECLARATION_SHA256": "", "AUDITOR_KEY_FILE": ""})
	predecessor := newSettlementV2TestPredecessor(t, true)
	adopt := func(mutate func(*ReleaseConfig, *OwnerRecycleApproval)) (*ReleaseConfig, error) {
		path, _ := predecessor.successor(t, func(cfg *ReleaseConfig, approval *OwnerRecycleApproval) {
			settlementV2TestAdopt(cfg, approval)
			mutate(cfg, approval)
		})
		return LoadReleaseConfig(path)
	}
	unreached := "settlement v2 adoption must append exactly one authority history entry"
	named := map[string]bool{}
	for name, mutate := range map[string]func(*ReleaseConfig, *OwnerRecycleApproval){
		"another change":  func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.PollSeconds++ },
		"approval change": func(_ *ReleaseConfig, approval *OwnerRecycleApproval) { approval.ValidThroughNativeBlock++ },
		"second entry": func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.ProductionAuthorityHistory = append(cfg.ProductionAuthorityHistory, cfg.ProductionAuthorityHistory[0])
		},
	} {
		_, err := adopt(mutate)
		if err == nil {
			t.Errorf("%s: the loader admitted the adoption", name)
			continue
		}
		refusal := err.Error()[strings.LastIndex(err.Error(), ": ")+2:]
		if refusal == unreached || named[refusal] || !strings.Contains(page, "| `"+refusal+"` |") {
			t.Errorf("%s: the page does not name the loader's own refusal %q", name, refusal)
		}
		named[refusal] = true
	}
	decision := &SteeringIntent{Treasury: &OwnerRecycleProductionIntent{Proof: OwnerRecycleProductionProof{Approval: predecessor.cfg.ownerRecycleProduction.encoded}}}
	bare, err := adopt(func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.ProductionAuthorityHistory = nil })
	if err != nil || !strings.Contains(page, "`"+unreached+"`") {
		t.Fatalf("an adoption without its entry was refused at load, or the page omits the unreached check: %v", err)
	}
	if _, err := productionConfigForIntent(bare, decision); err == nil || !strings.Contains(page, "`"+err.Error()+"`") {
		t.Fatalf("the page does not name the refusal of a retained decision without its entry: %v", err)
	}
	retained, err := RetainOwnerRecycleProductionAuthority(t.Context(), predecessor.cfg)
	if err != nil || retained.Path != filepath.Join(predecessor.cfg.StateDir, "owner-recycle-production-authority-sha256:"+strings.TrimPrefix(retained.SHA256, "0x")+".json") ||
		!strings.Contains(page, "`owner-recycle-production-authority-sha256:<hex>.json`") {
		t.Fatalf("the predecessor's bundle is retained as %s, not as the page names it: %v", retained.Path, err)
	}
	adopted, err := adopt(func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
		cfg.ProductionAuthorityHistory[len(cfg.ProductionAuthorityHistory)-1] = retained
	})
	if err != nil || adopted.SettlementV2 == nil {
		t.Fatal("the adoption with the retained bundle as its entry was refused", err)
	}
	if original, err := productionConfigForIntent(adopted, decision); err != nil || original.SettlementV2 != nil {
		t.Fatal("a decision made under the predecessor lost its authority after adoption", err)
	}
}
