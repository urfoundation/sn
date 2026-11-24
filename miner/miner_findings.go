package miner

// What the running miner finds wrong with how it runs here, each with the fix
// its operator applies: a state directory that won't outlive its container
// (state_persistence.go) and a release this build is behind
// (release_check.go). A finding goes three ways: once into the log as a
// framed block a person reading `docker logs` or `journalctl` can't miss, as
// a diagnostics record (urnetwork-provider-diagnostics-v1) for programs, and
// into the status file, where `provider status` shows it until it no longer
// holds. `provider doctor` makes the same checks itself, so it answers with
// the miner stopped too.

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// The closed codes of a finding.
const (
	// the state directory, and so this device's identity, is lost with its
	// container or with the machine's memory
	minerFindingStateNotPersistent = "state_not_persistent"
	// a newer release is out and nothing here installs it
	minerFindingUpdateAvailable = "update_available"
)

// One thing the miner found wrong. Title is a line for a status row; Summary
// says what and why it matters, in sentences; Fix is the commands or steps,
// one per line. None holds a credential or an identity.
type minerFinding struct {
	Code    string    `json:"code"`
	Title   string    `json:"title"`
	Summary string    `json:"summary"`
	Fix     string    `json:"fix,omitempty"`
	FoundAt time.Time `json:"found_at"`
}

// The finding as one guidance text: the title as its first sentence, the
// summary, then "Fix: " and the fix's lines, a command ended by "; ".
func (self minerFinding) guidance() string {
	var text strings.Builder
	text.WriteString(self.Title + ". " + self.Summary)
	if self.Fix == "" {
		return text.String()
	}
	text.WriteString(" Fix:")
	for _, line := range strings.Split(self.Fix, "\n") {
		text.WriteString(" " + line)
		if !strings.HasSuffix(line, ".") && !strings.HasSuffix(line, ":") {
			text.WriteString(";")
		}
	}
	return strings.TrimSuffix(text.String(), ";")
}

// The current findings of one run, by code, for the status file. Safe for
// concurrent use.
type minerFindings struct {
	stateLock sync.Mutex
	byCode    map[string]minerFinding
}

func newMinerFindings() *minerFindings {
	return &minerFindings{byCode: map[string]minerFinding{}}
}

// Records finding in place of an earlier one with its code.
func (self *minerFindings) set(finding minerFinding) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.byCode[finding.Code] = finding
}

// Removes the finding with code, once it no longer holds.
func (self *minerFindings) clear(code string) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	delete(self.byCode, code)
}

// The findings in code order, nil for none.
func (self *minerFindings) current() []minerFinding {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.byCode) == 0 {
		return nil
	}
	findings := make([]minerFinding, 0, len(self.byCode))
	for _, finding := range self.byCode {
		findings = append(findings, finding)
	}
	slices.SortFunc(findings, func(a minerFinding, b minerFinding) int {
		return strings.Compare(a.Code, b.Code)
	})
	return findings
}

// checkStatePersistence tells of a state directory lost with its container or
// the machine's memory, once per run: in findings, as a diagnostics record and
// as a block in the finding log. created says this run generated its key, so
// it registers a new device.
func (self providerRunSettings) checkStatePersistence(facts minerHostFacts, output *providerDiagnostics, findings *minerFindings, created bool) {
	dir, err := providerStateDir()
	if err != nil {
		return
	}
	executable, _ := cliExecutable()
	finding := minerStatePersistenceFinding(assessMinerStatePersistence(facts, dir), readMinerStateSubstitution(os.Getenv, dir), created, minerContainerImage(executable), time.Now())
	if finding == nil {
		return
	}
	findings.set(*finding)
	output.observeGuided(providerStateNotPersistent, 0, false, nil, 0, nil, dir, finding.guidance())
	if !self.operatorChild || created {
		writeMinerFindingBlock(self.findingLog, *finding)
	}
}

// newReleaseCheck is the run's release check, in container (one of
// minerContainer*, or ""): a finding goes into findings, the diagnostics and
// the finding log.
func (self providerRunSettings) newReleaseCheck(output *providerDiagnostics, findings *minerFindings, container string) *minerReleaseCheck {
	executable, _ := cliExecutable()
	return &minerReleaseCheck{
		current:  RequireVersion(),
		fetch:    fetchMinerRelease,
		updater:  func() minerUpdater { return readMinerUpdater(container) },
		image:    minerContainerImage(executable),
		now:      time.Now,
		findings: findings,
		report: func(finding minerFinding) {
			output.observeGuided(providerUpdateAvailable, 0, false, nil, 0, nil, "", finding.guidance())
			writeMinerFindingBlock(self.findingLog, finding)
		},
		settings: self.releaseCheck,
	}
}

// startOperatorSupervisorChecks makes the supervisor of provide
// --all-operators' checks, the ones its children leave to it: how this host
// keeps the state directory every operator's lies below, at start, and the
// release check until ctx ends. Both write their findings to log. stop joins
// the release check.
func startOperatorSupervisorChecks(ctx context.Context, hostRoot string, settings *minerReleaseCheckSettings, log io.Writer) (stop func()) {
	facts := readMinerHostFacts(hostRoot, os.Getenv)
	executable, _ := cliExecutable()
	image := minerContainerImage(executable)
	if dir, err := providerStateDir(); err == nil {
		if finding := minerStatePersistenceFinding(assessMinerStatePersistence(facts, dir), readMinerStateSubstitution(os.Getenv, dir), false, image, time.Now()); finding != nil {
			writeMinerFindingBlock(log, *finding)
		}
	}
	release := &minerReleaseCheck{
		current:  RequireVersion(),
		fetch:    fetchMinerRelease,
		updater:  func() minerUpdater { return readMinerUpdater(facts.container) },
		image:    image,
		now:      time.Now,
		findings: newMinerFindings(),
		report: func(finding minerFinding) {
			writeMinerFindingBlock(log, finding)
		},
		settings: settings,
	}
	return release.start(ctx)
}

// Writes finding to a log as a framed block: a rule, the title, the summary,
// the fix indented under "Fix:", and a rule, every line marked "provider:" so
// a search of a shared log finds it.
func writeMinerFindingBlock(w io.Writer, finding minerFinding) {
	if w == nil {
		return
	}
	var block strings.Builder
	rule := "provider: " + strings.Repeat("!", 72) + "\n"
	block.WriteString(rule)
	fmt.Fprintf(&block, "provider: ! %s.\n", finding.Title)
	fmt.Fprintf(&block, "provider: ! %s\n", finding.Summary)
	if finding.Fix != "" {
		block.WriteString("provider: ! Fix:\n")
		for _, line := range strings.Split(finding.Fix, "\n") {
			fmt.Fprintf(&block, "provider: !   %s\n", line)
		}
	}
	block.WriteString(rule)
	// one write, so the block isn't interleaved with other lines
	_, _ = io.WriteString(w, block.String())
}
