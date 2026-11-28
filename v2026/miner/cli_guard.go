package miner

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Checks that keep the friendly commands from doing something unsafe for a
// person who didn't mean it: root acting on files another user can change,
// a service file a value could rewrite, and a second miner on one identity.

// rootUpdateProblem says why root must not update this binary: root would
// run it (--version) and swap files in a folder another user can change, or
// write its update state into that user's state folder.
func rootUpdateProblem(executable string) string {
	if os.Geteuid() != 0 {
		return ""
	}
	if problem := rootOnlyProblem(executable); problem != "" {
		return problem
	}
	if dir, err := providerStateDir(); err == nil {
		if _, err := os.Stat(dir); err == nil {
			if problem := rootOnlyProblem(dir); problem != "" {
				return problem
			}
		}
	}
	return ""
}

// serviceValueProblem says when a value can't go into a unit file or plist
// as one value: a line break would start a line of its own.
func serviceValueProblem(values ...string) string {
	for _, value := range values {
		if i := strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }); 0 <= i {
			return fmt.Sprintf("%q has a control character, which a service file can't hold", value)
		}
	}
	return ""
}

// otherMinerProblem says when a miner the service manager didn't start
// already runs on this state folder, so starting the service would put a
// second miner on the same identity.
func otherMinerProblem(manager minerServiceManager) string {
	if manager.installed() && manager.active() {
		return ""
	}
	if status, err := readMinerLocalStatus(); err == nil && status.running(time.Now()) {
		return fmt.Sprintf("A miner is already running on this device (process %d) with the same identity.\n  Stop it first (Ctrl-C in its window), then run this again.", status.Pid)
	}
	if other := installerService(manager); other != nil && other.active() {
		return "The URnetwork installer's " + other.unit() + " is running the miner with the same identity.\n  Keep using it, or stop it first: " + other.stopHint()
	}
	return ""
}
