package main

// The independent validator evidence journal is a deployed contract, not a
// transitive dependency of coordinator analysis. Static coverage is a literal
// executable root census, not a count of names in comments.
import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The deployed contracts, including the settlement v2 coordinator, its phase C
// and capture the flag upgrades, vault, claim batcher and capture the flag
// carry contract, plus the settlement v2 ledger sum tree library, which
// VERSION2-FINAL.md §15 adds as a root of its own.
const solidityStaticDeployedRoots = "src/STCoordinator.sol\nsrc/STFleetBatcher.sol\nsrc/STValidatorEvidence.sol\nsrc/probe/STSubnetProbe.sol\nsrc/testnet/STCoordinatorAdversary.sol\nsrc/LedgerTree.sol\nsrc/STSettlement2.sol\nsrc/STSettlement2PhaseC.sol\nsrc/STSettlement2Ctf.sol\nsrc/STSettlementVault2.sol\nsrc/STClaimBatcher.sol\nsrc/STCtfCarry.sol"

// Admit the reviewed literal array only. A comment, duplicate, nondeployed
// library or dynamic expansion cannot stand in for one actual analysis root.
func verifySolidityStaticRootCensus(script string) error {
	required := map[string]bool{}
	for _, root := range strings.Split(solidityStaticDeployedRoots, "\n") {
		required[root] = true
	}
	counts := map[string]int{}
	arrays, loops := 0, 0
	inside, closed := false, false
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "contracts=(" {
			arrays++
			if inside || arrays != 1 {
				return errors.New("static root array is duplicated")
			}
			inside = true
			continue
		}
		if inside {
			if line == ")" {
				inside, closed = false, true
				continue
			}
			if !required[line] {
				return fmt.Errorf("static array includes an unreviewed or nonliteral root %q", line)
			}
			counts[line]++
			if counts[line] != 1 {
				return fmt.Errorf("static root %s is duplicated", line)
			}
			continue
		}
		if line == `for contract in "${contracts[@]}"; do` {
			loops++
		}
	}
	if arrays != 1 || !closed || inside || loops != 1 {
		return errors.New("static analysis lacks one closed root array and executable census loop")
	}
	for root := range required {
		if counts[root] != 1 {
			return fmt.Errorf("static analysis omits deployed root %s", root)
		}
	}
	if !strings.Contains(script, "all ${#contracts[@]} deployable roots") {
		return errors.New("static analysis does not report its complete root count")
	}
	return nil
}

// The pre-fix four-root array and a comment-only journal name fail without
// invoking Solidity tools. Adjacent duplicate, missing and loop omissions fail.
func TestSolidityStaticGateRejectsOmittedOrCommentOnlyEvidenceRoot(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-solidity-static.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(encoded)
	if err := verifySolidityStaticRootCensus(script); err != nil {
		t.Fatal(err)
	}
	for _, root := range strings.Split(solidityStaticDeployedRoots, "\n") {
		line := "  " + root + "\n"
		for _, replacement := range []string{"", "  # " + root + "\n", line + line} {
			changed := strings.Replace(script, line, replacement, 1)
			if changed == script || verifySolidityStaticRootCensus(changed) == nil {
				t.Fatalf("missing, commented or duplicate root %s passed static coverage", root)
			}
		}
	}
	for _, changed := range []string{
		strings.Replace(script, "  src/STValidatorEvidence.sol\n", "  src/lib/ValidatorEvidence.sol\n", 1),
		strings.Replace(script, `for contract in "${contracts[@]}"; do`, `# for contract in "${contracts[@]}"; do`, 1),
		strings.Replace(script, "contracts=(", "# contracts=(", 1),
	} {
		if changed == script || verifySolidityStaticRootCensus(changed) == nil {
			t.Fatal("a library, comment or absent executable census authorized static coverage")
		}
	}
}

// runSolidityStaticGate runs the real static script with stand-ins for
// Slither, solc and forge. The fake Slither records each root it analyzes and
// reports a medium finding for the root named by finding: with a failing exit,
// or with a successful one when silent is set. The fake forge prints layout as
// STSettlement2's storage layout, phaseC as STSettlement2PhaseC's and ctf as
// STSettlement2Ctf's.
func runSolidityStaticGate(t *testing.T, finding string, silent bool, layout, phaseC, ctf string) (string, []string, error) {
	t.Helper()
	bin := t.TempDir()
	for name, body := range map[string]string{
		"slither": `if [[ "$1" == --version ]]; then echo 0.11.6; exit 0; fi
printf '%s\n' "$1" >> "$FAKE_SLITHER_ROOTS"
if [[ "$1" == "$FAKE_SLITHER_FINDING" ]]; then
  printf '{"results":{"detectors":[{"impact":"Medium","check":"locked-ether","description":"fake finding"}]}}' > "${@: -1}"
  [[ -n "$FAKE_SLITHER_SILENT" ]] && exit 0
  exit 255
fi
printf '{"results":{"detectors":[]}}' > "${@: -1}"`,
		"solc": `echo 'Version: 0.8.24+commit.e11b9ed9.Linux.g++'`,
		"forge": `[[ "$*" == "inspect STSettlement2 storageLayout --json" ]] && cat "$FAKE_FORGE_LAYOUT"
[[ "$*" == "inspect STSettlement2PhaseC storageLayout --json" ]] && cat "$FAKE_FORGE_LAYOUT_PHASE_C"
[[ "$*" == "inspect STSettlement2Ctf storageLayout --json" ]] && cat "$FAKE_FORGE_LAYOUT_CTF"
true`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	roots := filepath.Join(t.TempDir(), "roots")
	layoutPath := filepath.Join(t.TempDir(), "layout.json")
	if err := os.WriteFile(layoutPath, []byte(layout), 0o600); err != nil {
		t.Fatal(err)
	}
	phaseCPath := filepath.Join(t.TempDir(), "layout-phase-c.json")
	if err := os.WriteFile(phaseCPath, []byte(phaseC), 0o600); err != nil {
		t.Fatal(err)
	}
	ctfPath := filepath.Join(t.TempDir(), "layout-ctf.json")
	if err := os.WriteFile(ctfPath, []byte(ctf), 0o600); err != nil {
		t.Fatal(err)
	}
	silentValue := ""
	if silent {
		silentValue = "1"
	}
	command := exec.CommandContext(t.Context(), "bash", "../scripts/test-solidity-static.sh")
	command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SLITHER_BIN="+filepath.Join(bin, "slither"), "SOLC_BIN="+filepath.Join(bin, "solc"),
		"SLITHER_FOUNDRY_OUT="+t.TempDir(), "SLITHER_FOUNDRY_CACHE_PATH="+t.TempDir(),
		"FAKE_SLITHER_ROOTS="+roots, "FAKE_SLITHER_FINDING="+finding, "FAKE_SLITHER_SILENT="+silentValue,
		"FAKE_FORGE_LAYOUT="+layoutPath, "FAKE_FORGE_LAYOUT_PHASE_C="+phaseCPath, "FAKE_FORGE_LAYOUT_CTF="+ctfPath)
	output, err := command.CombinedOutput()
	analyzed, readErr := os.ReadFile(roots)
	if readErr != nil {
		t.Fatalf("no root was analyzed: %v\n%s", readErr, output)
	}
	return string(output), strings.Fields(string(analyzed)), err
}

// A finding in one root, reported by a failing or by a successful Slither
// exit, or a changed STSettlement2, STSettlement2PhaseC or STSettlement2Ctf
// layout, still fails the gate naming it, and no longer stops the analysis:
// every later root and every layout pin runs (VERSION2-FINAL.md §14 gate 1,
// S-9, §16; CAPTURETHEFLAG.md §3.1).
func TestSolidityStaticGateAnalyzesEveryRootAfterAFinding(t *testing.T) {
	pinned, err := os.ReadFile("../evm/layout/STSettlement2.json")
	if err != nil {
		t.Fatal(err)
	}
	phaseC, err := os.ReadFile("../evm/layout/STSettlement2PhaseC.json")
	if err != nil {
		t.Fatal(err)
	}
	ctf, err := os.ReadFile("../evm/layout/STSettlement2Ctf.json")
	if err != nil {
		t.Fatal(err)
	}
	output, analyzed, err := runSolidityStaticGate(t, "", false, string(pinned), string(phaseC), string(ctf))
	if err != nil || strings.Join(analyzed, "\n") != solidityStaticDeployedRoots ||
		!strings.Contains(output, "release Solidity static gate passed for all 12 deployable roots") {
		t.Fatalf("clean roots: %v %q\n%s", err, analyzed, output)
	}
	for _, test := range []struct {
		name, finding, layout, phaseC, ctf, failed, report string
		silent                                             bool
	}{
		{name: "a failing exit", finding: "src/probe/STSubnetProbe.sol", layout: string(pinned), phaseC: string(phaseC), ctf: string(ctf),
			failed: "src/probe/STSubnetProbe.sol", report: "[Medium] locked-ether: fake finding"},
		{name: "a successful exit", finding: "src/STCoordinator.sol", silent: true, layout: string(pinned), phaseC: string(phaseC), ctf: string(ctf),
			failed: "src/STCoordinator.sol", report: "Slither emitted a high/medium finding despite a successful exit for src/STCoordinator.sol"},
		{name: "a finding in the carry contract", finding: "src/STCtfCarry.sol", layout: string(pinned), phaseC: string(phaseC), ctf: string(ctf),
			failed: "src/STCtfCarry.sol", report: "[Medium] locked-ether: fake finding"},
		{name: "a changed layout", layout: strings.Replace(string(pinned), `"label": "vault"`, `"label": "vault2"`, 1), phaseC: string(phaseC), ctf: string(ctf),
			failed: "layout/STSettlement2.json", report: "STSettlement2 storage layout differs from layout/STSettlement2.json"},
		{name: "a changed phase C layout", layout: string(pinned), phaseC: strings.Replace(string(phaseC), `"slot": "50"`, `"slot": "7"`, 1), ctf: string(ctf),
			failed: "layout/STSettlement2PhaseC.json", report: "STSettlement2PhaseC storage layout differs from layout/STSettlement2PhaseC.json"},
		{name: "a failed phase C inspection", layout: string(pinned), ctf: string(ctf),
			failed: "layout/STSettlement2PhaseC.json", report: "STSettlement2PhaseC storage layout differs from layout/STSettlement2PhaseC.json"},
		{name: "a changed CTF layout", layout: string(pinned), phaseC: string(phaseC), ctf: strings.Replace(string(ctf), `"slot": "51"`, `"slot": "50"`, 1),
			failed: "layout/STSettlement2Ctf.json", report: "STSettlement2Ctf storage layout differs from layout/STSettlement2Ctf.json"},
		{name: "a failed CTF inspection", layout: string(pinned), phaseC: string(phaseC),
			failed: "layout/STSettlement2Ctf.json", report: "STSettlement2Ctf storage layout differs from layout/STSettlement2Ctf.json"},
	} {
		output, analyzed, err := runSolidityStaticGate(t, test.finding, test.silent, test.layout, test.phaseC, test.ctf)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || strings.Join(analyzed, "\n") != solidityStaticDeployedRoots ||
			!strings.Contains(output, test.report) || !strings.Contains(output, "release Solidity static gate failed for: "+test.failed+"\n") ||
			strings.Contains(output, "passed for all") {
			t.Fatalf("%s: %v %q\n%s", test.name, err, analyzed, output)
		}
	}
}

// The pinned phase C layout keeps every STSettlement2 entry at its slot,
// offset and type, and adds entries only past the end of STSettlement2's gap,
// so the upgrade moves no live value (VERSION2-FINAL.md §16, S-9).
func TestSettlement2PhaseCLayoutExtendsPhaseA(t *testing.T) {
	type entry struct {
		Label  string `json:"label"`
		Offset int    `json:"offset"`
		Slot   string `json:"slot"`
		Type   string `json:"type"`
	}
	type layout struct {
		Storage []entry `json:"storage"`
		Types   map[string]struct {
			NumberOfBytes string `json:"numberOfBytes"`
		} `json:"types"`
	}
	read := func(path string) layout {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var parsed layout
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	phaseA, phaseC := read("../evm/layout/STSettlement2.json"), read("../evm/layout/STSettlement2PhaseC.json")
	gap := phaseA.Storage[len(phaseA.Storage)-1]
	start, err := strconv.Atoi(gap.Slot)
	if err != nil || gap.Label != "__gap" {
		t.Fatalf("STSettlement2's last entry is %+v, not its gap", gap)
	}
	size, err := strconv.Atoi(phaseA.Types[gap.Type].NumberOfBytes)
	if err != nil {
		t.Fatal(err)
	}
	free := start + size/32
	if len(phaseC.Storage) <= len(phaseA.Storage) {
		t.Fatal("the phase C layout adds no entry")
	}
	for i, kept := range phaseA.Storage {
		if phaseC.Storage[i] != kept {
			t.Fatalf("entry %d is %+v, STSettlement2 has %+v", i, phaseC.Storage[i], kept)
		}
	}
	for _, added := range phaseC.Storage[len(phaseA.Storage):] {
		if slot, err := strconv.Atoi(added.Slot); err != nil || slot < free {
			t.Fatalf("%s at slot %s is inside STSettlement2's storage, which ends at slot %d", added.Label, added.Slot, free)
		}
	}
	if free != 50 || phaseC.Storage[len(phaseA.Storage)].Label != "_v1Sweeps" {
		t.Fatalf("the phase C mapping is %+v, past slot %d", phaseC.Storage[len(phaseA.Storage)], free)
	}
}

// The pinned capture the flag layout keeps every phase C entry at its slot,
// offset and type, phase C's V1Sweep struct member for member, and adds only
// the census mapping, past phase C's last slot, so the upgrade moves no live
// value (CAPTURETHEFLAG.md §3.1).
func TestSettlement2CtfLayoutExtendsPhaseC(t *testing.T) {
	type entry struct {
		Label  string `json:"label"`
		Offset int    `json:"offset"`
		Slot   string `json:"slot"`
		Type   string `json:"type"`
	}
	type layout struct {
		Storage []entry `json:"storage"`
		Types   map[string]struct {
			Members []entry `json:"members"`
		} `json:"types"`
	}
	read := func(path string) layout {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var parsed layout
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	phaseC, ctf := read("../evm/layout/STSettlement2PhaseC.json"), read("../evm/layout/STSettlement2Ctf.json")
	last, err := strconv.Atoi(phaseC.Storage[len(phaseC.Storage)-1].Slot)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctf.Storage) != len(phaseC.Storage)+1 {
		t.Fatalf("the CTF layout has %d entries, phase C %d", len(ctf.Storage), len(phaseC.Storage))
	}
	for i, kept := range phaseC.Storage {
		if ctf.Storage[i] != kept {
			t.Fatalf("entry %d is %+v, phase C has %+v", i, ctf.Storage[i], kept)
		}
	}
	added := ctf.Storage[len(phaseC.Storage)]
	if slot, err := strconv.Atoi(added.Slot); err != nil || slot != last+1 || added.Label != "_ctfCensuses" ||
		added.Type != "t_mapping(t_uint256,t_mapping(t_uint256,t_struct(CtfCensus)_storage))" {
		t.Fatalf("the CTF census mapping is %+v, want slot %d", added, last+1)
	}
	sweep := "t_struct(V1Sweep)_storage"
	if fmt.Sprint(ctf.Types[sweep].Members) != fmt.Sprint(phaseC.Types[sweep].Members) || len(phaseC.Types[sweep].Members) != 3 {
		t.Fatalf("V1Sweep members %+v, phase C %+v", ctf.Types[sweep].Members, phaseC.Types[sweep].Members)
	}
	census := ctf.Types["t_struct(CtfCensus)_storage"].Members
	if len(census) != 2 || census[0] != (entry{Label: "censusHash", Slot: "0", Type: "t_bytes32"}) ||
		census[1] != (entry{Label: "blockNumber", Slot: "1", Type: "t_uint64"}) {
		t.Fatalf("CtfCensus members %+v", census)
	}
}
