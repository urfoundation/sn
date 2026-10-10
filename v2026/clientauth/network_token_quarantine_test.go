//go:build linux || darwin

package clientauth

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

var quarantineTestBase = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func quarantineTestFiles(t *testing.T, networkPath string) []NetworkTokenQuarantine {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(networkPath))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return networkTokenQuarantines(filepath.Dir(networkPath), filepath.Base(networkPath), names)
}

// The rejected token is renamed aside with its mode and content, its lineage
// stays, the count runs while rejections come within a day of each other, and
// only the newest few set-aside files are kept.
func TestQuarantineNetworkTokenSetsTheRejectedTokenAside(t *testing.T) {
	dir := networkTokenTestDir(t)
	networkPath := filepath.Join(dir, "jwt")
	if err := WriteNetworkToken(networkPath, "sign-in"); err != nil {
		t.Fatal(err)
	}
	if ok, err := RenewNetworkToken(networkPath, "sign-in", "sign-in-renewed"); err != nil || !ok {
		t.Fatalf("renewal: ok=%t err=%v", ok, err)
	}
	lineage, err := os.ReadFile(networkPath + ".lineage")
	if err != nil {
		t.Fatal(err)
	}

	quarantine, ok, err := QuarantineNetworkToken(networkPath, "sign-in-renewed", quarantineTestBase)
	if err != nil || !ok {
		t.Fatalf("quarantine: ok=%t err=%v", ok, err)
	}
	if want := filepath.Join(dir, "jwt.rejected-"+strconv.FormatInt(quarantineTestBase.UnixNano(), 10)+"-1"); quarantine.Path != want || quarantine.Consecutive != 1 || !quarantine.Time.Equal(quarantineTestBase) {
		t.Fatalf("quarantine = %+v, want %s", quarantine, want)
	}
	if _, err := os.Stat(networkPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the rejected token is still in place: %v", err)
	}
	info, err := os.Stat(quarantine.Path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("set-aside file mode: %v %v", info, err)
	}
	if token, _ := ReadToken(quarantine.Path); token != "sign-in-renewed" {
		t.Fatalf("set-aside content = %q", token)
	}
	if after, err := os.ReadFile(networkPath + ".lineage"); err != nil || string(after) != string(lineage) {
		t.Fatalf("the lineage did not stay: %v", err)
	}
	if latest, ok, err := LatestNetworkTokenQuarantine(networkPath); err != nil || !ok || latest != quarantine {
		t.Fatalf("latest = %+v ok=%t err=%v", latest, ok, err)
	}

	// a sign-in rejected within a day is one more in a row; after a day the
	// count starts over
	at := quarantineTestBase
	for _, c := range []struct {
		after time.Duration
		want  int
	}{{time.Hour, 2}, {2 * time.Hour, 3}, {26 * time.Hour, 1}, {time.Minute, 2}, {time.Minute, 3}} {
		at = at.Add(c.after)
		if err := WriteNetworkToken(networkPath, "sign-in-"+c.after.String()); err != nil {
			t.Fatal(err)
		}
		quarantine, ok, err := QuarantineNetworkToken(networkPath, "sign-in-"+c.after.String(), at)
		if err != nil || !ok || quarantine.Consecutive != c.want {
			t.Fatalf("after %s: consecutive=%d ok=%t err=%v, want %d", c.after, quarantine.Consecutive, ok, err, c.want)
		}
	}
	kept := quarantineTestFiles(t, networkPath)
	if len(kept) != maximumNetworkTokenQuarantine || !kept[len(kept)-1].Time.Equal(at) {
		t.Fatalf("kept %d set-aside files, newest at %s", len(kept), kept[len(kept)-1].Time)
	}
	// a quarantine at the same instant as the latest still sorts after it
	if err := WriteNetworkToken(networkPath, "same-instant"); err != nil {
		t.Fatal(err)
	}
	same, ok, err := QuarantineNetworkToken(networkPath, "same-instant", at)
	if err != nil || !ok || !same.Time.After(at) {
		t.Fatalf("same instant: %+v ok=%t err=%v", same, ok, err)
	}
}

// A sign-in that replaced the token first is never set aside, a missing token
// is nothing to set aside, and a held owner lock is a busy, retryable answer.
func TestQuarantineNetworkTokenNeverSetsAsideANewerSignIn(t *testing.T) {
	dir := networkTokenTestDir(t)
	networkPath := filepath.Join(dir, "jwt")
	if _, ok, err := QuarantineNetworkToken(networkPath, "rejected", quarantineTestBase); err != nil || ok {
		t.Fatalf("missing token: ok=%t err=%v", ok, err)
	}
	if err := WriteNetworkToken(networkPath, "new-sign-in"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := QuarantineNetworkToken(networkPath, "rejected", quarantineTestBase); err != nil || ok {
		t.Fatalf("a newer sign-in: ok=%t err=%v", ok, err)
	}
	if token, _ := ReadToken(networkPath); token != "new-sign-in" || len(quarantineTestFiles(t, networkPath)) != 0 {
		t.Fatal("a newer sign-in was set aside")
	}
	owner, err := openRegistrationStore(networkPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := QuarantineNetworkToken(networkPath, "new-sign-in", quarantineTestBase); ok || !errors.Is(err, ErrNetworkTokenBusy) {
		t.Fatalf("held lock: ok=%t err=%v", ok, err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := QuarantineNetworkToken(networkPath, " ", quarantineTestBase); ok || err == nil {
		t.Fatalf("an empty token: ok=%t err=%v", ok, err)
	}
}

// An explicit sign-in that starts while a quarantine holds the owner lock
// waits, then writes its token: the rejected token is set aside and the new
// sign-in is current. The other order sets nothing aside.
func TestQuarantineAndExplicitSignInAreSerialized(t *testing.T) {
	dir := networkTokenTestDir(t)
	networkPath := filepath.Join(dir, "jwt")
	if err := WriteNetworkToken(networkPath, "rejected"); err != nil {
		t.Fatal(err)
	}
	paused, release := make(chan struct{}), make(chan struct{})
	networkQuarantineTestHooks.beforeRename = func() {
		close(paused)
		<-release
	}
	defer func() { networkQuarantineTestHooks.beforeRename = nil }()
	quarantined := make(chan error, 1)
	go func() {
		_, ok, err := QuarantineNetworkToken(networkPath, "rejected", quarantineTestBase)
		if err == nil && !ok {
			err = errors.New("the quarantine was discarded")
		}
		quarantined <- err
	}()
	select {
	case <-paused:
	case <-time.After(10 * time.Second):
		t.Fatal("the quarantine did not reach its rename")
	}
	explicit := make(chan error, 1)
	go func() { explicit <- WriteNetworkToken(networkPath, "new-sign-in") }()
	select {
	case err := <-explicit:
		t.Fatalf("the explicit sign-in did not wait for the quarantine's owner lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-quarantined; err != nil {
		t.Fatal(err)
	}
	if err := <-explicit; err != nil {
		t.Fatal(err)
	}
	if token, _ := ReadToken(networkPath); token != "new-sign-in" {
		t.Fatalf("the explicit sign-in was lost: %q", token)
	}
	kept := quarantineTestFiles(t, networkPath)
	if len(kept) != 1 {
		t.Fatalf("set-aside files = %d", len(kept))
	}
	if token, _ := ReadToken(kept[0].Path); token != "rejected" {
		t.Fatalf("set aside %q, want the rejected token", token)
	}

	// the explicit sign-in first: the quarantine of the old token finds the
	// new one and sets nothing aside
	networkQuarantineTestHooks.beforeRename = nil
	if _, ok, err := QuarantineNetworkToken(networkPath, "rejected", quarantineTestBase.Add(time.Minute)); err != nil || ok {
		t.Fatalf("stale quarantine: ok=%t err=%v", ok, err)
	}
	if token, _ := ReadToken(networkPath); token != "new-sign-in" || len(quarantineTestFiles(t, networkPath)) != 1 {
		t.Fatal("a stale quarantine set the new sign-in aside")
	}
}

func TestNetworkTokenQuarantineNames(t *testing.T) {
	at, consecutive, ok := parseNetworkTokenQuarantineName("jwt", "jwt.rejected-1791576000000000000-4")
	if !ok || consecutive != 4 || at.UnixNano() != 1791576000000000000 {
		t.Fatalf("parse: %s %d %t", at, consecutive, ok)
	}
	for _, leaf := range []string{"jwt", "jwt.lineage", "jwt.rejected-", "jwt.rejected-12", "jwt.rejected-12-0", "jwt.rejected-012-1", "jwt.rejected-12-01", "jwt.rejected--1-1", "jwt.rejected-12-x", "other.rejected-12-1", "jwt.rejected-12-1.lineage"} {
		if _, _, ok := parseNetworkTokenQuarantineName("jwt", leaf); ok {
			t.Errorf("%s parsed as a set-aside file", leaf)
		}
	}
	if _, ok, err := LatestNetworkTokenQuarantine(filepath.Join(t.TempDir(), "missing", "jwt")); err != nil || ok {
		t.Fatalf("missing directory: ok=%t err=%v", ok, err)
	}
}
