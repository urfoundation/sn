// SETTLEMENT-V2-ADOPTION.md is held to the adoption sequence VA-6 runs: its
// config edit and its two treasury command lines go through the same round.
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/validator"
	"gopkg.in/yaml.v3"
)

// The page's fenced blocks in order, each as its language and its text.
func adoptionDocumentBlocks(t *testing.T) [][2]string {
	t.Helper()
	raw, err := os.ReadFile("SETTLEMENT-V2-ADOPTION.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := [][2]string{}
	for _, match := range regexp.MustCompile("(?s)```([a-z]+)\n(.*?)```").FindAllSubmatch(raw, -1) {
		blocks = append(blocks, [2]string{string(match[1]), string(match[2])})
	}
	return blocks
}

// A block with each $NAME replaced by its value.
func adoptionDocumentExpand(t *testing.T, text string, variables map[string]string) string {
	t.Helper()
	return regexp.MustCompile(`\$[A-Z][A-Z0-9_]*`).ReplaceAllStringFunc(text, func(name string) string {
		value, ok := variables[name[1:]]
		if !ok {
			t.Fatalf("the page uses %s, which the test does not bind", name)
		}
		return value
	})
}

// A documented command line as its words: values filled, continuation lines
// joined and quotes dropped.
func adoptionDocumentCommand(t *testing.T, text string, variables map[string]string) []string {
	t.Helper()
	return strings.Fields(strings.NewReplacer("\\\n", " ", `"`, "").Replace(adoptionDocumentExpand(t, text, variables)))
}

// VA-6, step 10: the page's sequence is the one
// TestTreasuryApprovalPlanResignsSettlementV2Adoption runs. Its config edit,
// with the fixture's values, is that test's edit. Its approval-plan and
// approval-envelope lines are, word for word, the commands of the shared round,
// which runs them through the real dispatcher in the page's order and has the
// producer loader admit the result. Its digest check is the derivation
// approval-plan prints, and its last command is the restart. approval-plan
// refuses the page's line when a digest lacks its sha256: prefix.
func TestSettlementV2AdoptionDocumentIsTheVA6Sequence(t *testing.T) {
	blocks := adoptionDocumentBlocks(t)
	languages := []string{}
	for _, block := range blocks {
		languages = append(languages, block[0])
	}
	if !slices.Equal(languages, []string{"yaml", "sh", "sh", "sh", "sh"}) {
		t.Fatalf("page blocks %v are not the config edit and four commands", languages)
	}
	f := newTreasuryApprovalTestFixture(t)
	before, predecessor := f.approve(t, "predecessor", nil)
	block := f.adopt(t, predecessor)
	entry := f.config.ProductionAuthorityHistory[len(f.config.ProductionAuthorityHistory)-1]
	variables := map[string]string{
		"SETTLEMENT2_ADDRESS": block.Settlement, "VAULT2_ADDRESS": block.Vault,
		"PREDECESSOR_AUTHORITY": entry.Path, "PREDECESSOR_AUTHORITY_BYTES": strconv.FormatUint(entry.Bytes, 10), "PREDECESSOR_AUTHORITY_SHA256": entry.SHA256,
		"ADOPTED_CONFIG": f.configPath, "ADOPTED_APPROVAL": filepath.Join(f.directory, "adopted-approval.json"), "ADOPTED_ENVELOPE": filepath.Join(f.directory, "adopted-envelope.json"),
		"TREASURY_INPUT": filepath.Join(f.directory, "approval-input.json"), "TREASURY_POLICY_PLAN": f.planPath, "TREASURY_POLICY_PLAN_SHA256": f.planHash, "TREASURY_APPROVAL_KEY": f.key,
	}
	var edit struct {
		SettlementV2               *validator.ReleaseSettlementV2Config     `yaml:"settlement_v2"`
		ProductionAuthorityHistory []validator.ReleaseEvidenceV2File        `yaml:"production_authority_history"`
		TreasuryApproval           *validator.ReleaseTreasuryApprovalConfig `yaml:"treasury_approval"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(adoptionDocumentExpand(t, blocks[0][1], variables)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&edit); err != nil || !reflect.DeepEqual(edit.SettlementV2, f.config.SettlementV2) || !reflect.DeepEqual(edit.ProductionAuthorityHistory, []validator.ReleaseEvidenceV2File{entry}) ||
		!reflect.DeepEqual(edit.TreasuryApproval, f.config.TreasuryApproval) {
		t.Fatalf("page config edit %+v differs from the adoption edit: %v", edit, err)
	}
	ran := []int{}
	documented := func(args []string) []string {
		raw, err := os.ReadFile(f.configPath)
		if err != nil {
			t.Fatal(err)
		}
		variables["ADOPTED_CONFIG_SHA256"] = monitorReadDigest(raw)
		if args[0] == "approval-envelope" {
			_, variables["SIGNATURE"] = f.sign(t, variables["ADOPTED_APPROVAL"])
		}
		for index, block := range blocks {
			if !strings.HasPrefix(block[1], "sn-mainnet treasury "+args[0]+" ") {
				continue
			}
			words := adoptionDocumentCommand(t, block[1], variables)
			if !slices.Equal(words[2:], args) {
				t.Fatalf("page %s line %q is not the round's %q", args[0], words[2:], args)
			}
			ran = append(ran, index)
			return words[2:]
		}
		t.Fatalf("page has no %s line", args[0])
		return nil
	}
	after, adopted := f.approve(t, "adopted", documented)
	unchanged := before.Approval
	unchanged.ConfigHash = after.Approval.ConfigHash
	if !slices.Equal(ran, []int{1, 3}) || !reflect.DeepEqual(unchanged, after.Approval) || adopted.SettlementV2 == nil || *adopted.SettlementV2 != block || len(adopted.ProductionAuthorityHistory) != 1 {
		t.Fatalf("page commands ran as blocks %v or adopted another config", ran)
	}
	variables["ADOPTED_CONFIG_SHA256"], variables["ADOPTED_APPROVAL"] = strings.TrimPrefix(variables["ADOPTED_CONFIG_SHA256"], "sha256:"), filepath.Join(f.directory, "bare-digest-approval.json")
	if code, _, diagnostic := treasuryApprovalTestRun(t, adoptionDocumentCommand(t, blocks[1][1], variables)[2:]...); code == 0 || !strings.Contains(diagnostic, "treasury approval-plan requires") {
		t.Fatalf("approval-plan took a config digest without its sha256: prefix: %d %s", code, diagnostic)
	}
	_, check, found := strings.Cut(treasuryApprovalDerivation, "Check: ")
	if !found || strings.TrimSpace(blocks[2][1]) != strings.Replace(check, "APPROVAL_FILE", `"$ADOPTED_APPROVAL"`, 1) {
		t.Fatalf("page digest check %q is not the printed derivation's %q", blocks[2][1], check)
	}
	if !strings.HasPrefix(blocks[4][1], `validator run --config="$ADOPTED_CONFIG" `) {
		t.Fatalf("page does not end with the restart: %q", blocks[4][1])
	}
}
