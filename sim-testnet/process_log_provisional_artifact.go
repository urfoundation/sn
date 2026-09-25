// A provisional owner may continue after one authenticated client disconnect.
// Original classification, signed evidence and every final gate stay blocking.
package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// The quoted joined cause must contain only a canceled request and its exact
// TCP broken-pipe write. Extra storage, integrity or deadline causes refuse.
func processLogArtifactClientDisconnect(text string) bool {
	prefix, encoded, found := strings.Cut(text, " context=context canceled error=")
	if !found || !processLogArtifactRequestCancellation(prefix+" context=context canceled error=context canceled") {
		return false
	}
	cause, err := strconv.Unquote(encoded)
	if err != nil {
		return false
	}
	parts := strings.Split(cause, "\n")
	if len(parts) != 2 || parts[1] != "context canceled" {
		return false
	}
	endpoints, found := strings.CutPrefix(parts[0], "write tcp ")
	if !found {
		return false
	}
	endpoints, found = strings.CutSuffix(endpoints, ": write: broken pipe")
	if !found {
		return false
	}
	source, destination, found := strings.Cut(endpoints, "->")
	if !found {
		return false
	}
	for _, endpoint := range []string{source, destination} {
		address, err := netip.ParseAddrPort(endpoint)
		if err != nil || address.Port() == 0 || address.String() != endpoint {
			return false
		}
	}
	return true
}

// Authenticate the actual retained single warning; endpoints alone cannot
// describe all members of an aggregated warning row. No row is reclassified.
// Copy state under its lock, then perform bounded local reads without the lock.
func (self *processLogGate) provisionalArtifactStreamCancellation(finding ProcessLogFinding) bool {
	if self == nil || finding.Class != "warning" || finding.Role != "operator-api" || !finding.Blocking || finding.Disposition != "unexplained" || finding.Count != 1 || finding.AcceptanceScope == "" || finding.FirstOffset < 0 || finding.FirstOffset != finding.LastOffset || finding.FirstLineSHA256 == "" || finding.FirstLineSHA256 != finding.LastLineSHA256 {
		return false
	}
	var cursor processLogCursor
	matched := func() bool {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.state.AcceptanceBoundary == nil || self.state.AcceptanceBoundary.ContentHash != finding.AcceptanceScope {
			return false
		}
		retained := false
		for _, known := range self.state.Findings {
			retained = retained || reflect.DeepEqual(known, finding)
		}
		if !retained {
			return false
		}
		for _, known := range self.state.Cursors {
			if known.ProcessID == finding.ProcessID && known.Role == finding.Role && known.Stream == finding.Stream {
				cursor = known
				return true
			}
		}
		return false
	}()
	if !matched || cursor.Offset <= finding.FirstOffset || cursor.Path != filepath.Join("processes", finding.ProcessID+"."+finding.Stream+".log") {
		return false
	}
	path := filepath.Join(self.stateDir, cursor.Path)
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) || info.Size() < cursor.Offset {
		return false
	}
	device, inode, err := processLogIdentity(info)
	if err != nil || device != cursor.Device || inode != cursor.Inode {
		return false
	}
	raw, err := io.ReadAll(io.NewSectionReader(file, finding.FirstOffset, min(cursor.Offset-finding.FirstOffset, processLogMaximumLineBytes+1)))
	if err != nil {
		return false
	}
	end := bytes.IndexByte(raw, '\n')
	if end < 0 || end > processLogMaximumLineBytes {
		return false
	}
	line := bytes.TrimSuffix(raw[:end], []byte{'\r'})
	if fmt.Sprintf("%x", sha256.Sum256(line)) != finding.FirstLineSHA256 || !processLogArtifactClientDisconnect(string(line)) {
		return false
	}
	after, err := os.Lstat(path)
	return err == nil && after.Mode().IsRegular() && os.SameFile(info, after) && after.Size() >= cursor.Offset
}
