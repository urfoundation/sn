package miner

// The supervisor runs here on its hooks alone: no process starts and no real
// timer fires. Its log lines and idle points share one ordered journal, so a
// test waits for the loop to settle after exactly the event it caused.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/hotkeyauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Bounds a wait that only a broken supervisor would reach.
const testOperatorWait = 30 * time.Second

var testOperatorEpoch = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)

type testOperatorJournal struct {
	stateLock sync.Mutex
	entries   []string
	partial   string
	update    chan struct{}
}

func (self *testOperatorJournal) addWithLock(entry string) {
	self.entries = append(self.entries, entry)
	close(self.update)
	self.update = make(chan struct{})
}

// The supervisor's log; each complete line is one entry.
func (self *testOperatorJournal) Write(raw []byte) (int, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.partial += string(raw)
	for {
		index := strings.IndexByte(self.partial, '\n')
		if index < 0 {
			return len(raw), nil
		}
		self.addWithLock("log " + self.partial[:index])
		self.partial = self.partial[index+1:]
	}
}

func (self *testOperatorJournal) idle(next time.Time) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if next.IsZero() {
		self.addWithLock("idle")
	} else {
		self.addWithLock("idle " + next.Sub(testOperatorEpoch).String())
	}
}

// Waits for the first entry from index on that equals want, and returns the
// index after it.
func (self *testOperatorJournal) wait(t *testing.T, from int, want string) int {
	t.Helper()
	return self.waitMatch(t, from, want, func(entry string) bool { return entry == want })
}

func (self *testOperatorJournal) waitPrefix(t *testing.T, from int, prefix string) int {
	t.Helper()
	return self.waitMatch(t, from, prefix+"...", func(entry string) bool { return strings.HasPrefix(entry, prefix) })
}

func (self *testOperatorJournal) waitMatch(t *testing.T, from int, what string, match func(string) bool) int {
	t.Helper()
	timeout := time.After(testOperatorWait)
	for {
		var update chan struct{}
		var entries []string
		found := -1
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			if index := slices.IndexFunc(self.entries[from:], match); 0 <= index {
				found = from + index
			}
			update = self.update
			entries = slices.Clone(self.entries)
		}()
		if 0 <= found {
			return found + 1
		}
		select {
		case <-update:
		case <-timeout:
			t.Fatalf("no %q after entry %d; journal:\n%s", what, from, strings.Join(entries, "\n"))
		}
	}
}

func (self *testOperatorJournal) length() int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return len(self.entries)
}

type testOperatorClock struct {
	stateLock sync.Mutex
	now       time.Time
	timers    []testOperatorTimer
}

type testOperatorTimer struct {
	deadline time.Time
	fire     chan time.Time
}

func (self *testOperatorClock) Now() time.Time {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.now
}

func (self *testOperatorClock) After(delay time.Duration) <-chan time.Time {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	fire := make(chan time.Time, 1)
	if deadline := self.now.Add(delay); deadline.After(self.now) {
		self.timers = append(self.timers, testOperatorTimer{deadline: deadline, fire: fire})
	} else {
		fire <- self.now
	}
	return fire
}

// Moves the clock to the epoch plus offset and fires every timer then due.
func (self *testOperatorClock) set(offset time.Duration) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.now = testOperatorEpoch.Add(offset)
	pending := self.timers[:0]
	for _, timer := range self.timers {
		if timer.deadline.After(self.now) {
			pending = append(pending, timer)
		} else {
			timer.fire <- self.now
		}
	}
	self.timers = pending
}

// A list the test publishes, as the refresher would.
type testOperatorList struct {
	value *connect.MonitorValue[*operatorlist.Snapshot]
}

func (self testOperatorList) Snapshot() (*operatorlist.Snapshot, chan struct{}) {
	return self.value.Get()
}

type testOperatorChild struct {
	pid     int
	command operatorChildCommand
	// the operator directory's proxy file as the child would have read it
	proxy     []byte
	proxyMode os.FileMode
	signals   chan os.Signal
	exit      chan error
}

func (self *testOperatorChild) Pid() int {
	return self.pid
}

func (self *testOperatorChild) Signal(signal os.Signal) error {
	self.signals <- signal
	return nil
}

func (self *testOperatorChild) Wait() error {
	return <-self.exit
}

func (self *testOperatorChild) stateDirs() []string {
	var directories []string
	for _, entry := range self.command.env {
		if directory, ok := strings.CutPrefix(entry, "URNETWORK_STATE_DIR="); ok {
			directories = append(directories, directory)
		}
	}
	return directories
}

func (self *testOperatorChild) domain() string {
	return filepath.Base(self.stateDirs()[0])
}

func (self *testOperatorChild) option(name string) string {
	for _, arg := range self.command.args {
		if value, ok := strings.CutPrefix(arg, name+"="); ok {
			return value
		}
	}
	return ""
}

type testOperatorFixture struct {
	t            *testing.T
	base         string
	settings     operatorSupervisorSettings
	signIn       func(context.Context, hotkeyauth.Settings) (*hotkeyauth.Network, error)
	hotkeyWallet func(context.Context, hotkeyWalletTarget, *crv4.Keypair, []protocol.HotkeyWalletMappingConsent) (hotkeyWalletOutcome, error)
	clock        *testOperatorClock
	journal      *testOperatorJournal
	list         *connect.MonitorValue[*operatorlist.Snapshot]
	signals      chan os.Signal
	starts       chan *testOperatorChild
	supervisor   *operatorSupervisor
	done         chan struct{}

	stateLock sync.Mutex
	children  []*testOperatorChild
}

func newTestOperatorFixture(t *testing.T) *testOperatorFixture {
	base := t.TempDir()
	self := &testOperatorFixture{
		t:        t,
		base:     base,
		settings: operatorSupervisorSettings{baseStateDir: base, executable: "/synthetic/bin/miner"},
		signIn: func(context.Context, hotkeyauth.Settings) (*hotkeyauth.Network, error) {
			t.Error("signed in without --auto-register")
			return nil, errors.New("unexpected sign-in")
		},
		hotkeyWallet: ensureOperatorHotkeyWallet,
		clock:        &testOperatorClock{now: testOperatorEpoch},
		journal:      &testOperatorJournal{update: make(chan struct{})},
		list:         connect.NewMonitorValue[*operatorlist.Snapshot](nil),
		signals:      make(chan os.Signal, 4),
		starts:       make(chan *testOperatorChild, 64),
	}
	return self
}

// Writes the operator's jwt as provider auth --operator would.
func (self *testOperatorFixture) authenticate(domain string) {
	if err := clientauth.WriteToken(operatorJwtPath(self.base, domain), "synthetic-jwt-"+domain); err != nil {
		self.t.Fatal(err)
	}
}

func (self *testOperatorFixture) publish(operators ...operatorlist.Operator) {
	list, err := operatorlist.Parse([]byte(testOperatorListYaml(self.t, operators...)))
	if err != nil {
		self.t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(fmt.Sprint(list)))
	self.list.Set(&operatorlist.Snapshot{List: list, Sha256: "sha256:" + hex.EncodeToString(sum[:]), FetchedAt: testOperatorEpoch})
}

func (self *testOperatorFixture) start(command operatorChildCommand) (operatorChildProcess, error) {
	child := &testOperatorChild{command: command, signals: make(chan os.Signal, 16), exit: make(chan error, 1)}
	if directories := child.stateDirs(); len(directories) == 1 {
		if info, err := os.Stat(filepath.Join(directories[0], "proxy")); err == nil {
			child.proxyMode = info.Mode().Perm()
			child.proxy, _ = os.ReadFile(filepath.Join(directories[0], "proxy"))
		}
	}
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.children = append(self.children, child)
		child.pid = len(self.children)
	}()
	self.starts <- child
	return child, nil
}

func (self *testOperatorFixture) run() {
	hooks := operatorSupervisorHooks{
		start:        self.start,
		now:          self.clock.Now,
		after:        self.clock.After,
		signIn:       self.signIn,
		hotkeyWallet: self.hotkeyWallet,
		idle:         self.journal.idle,
	}
	self.supervisor = newOperatorSupervisor(self.settings, hooks, self.journal)
	ctx, cancel := context.WithCancel(context.Background())
	self.done = make(chan struct{})
	go func() {
		defer close(self.done)
		self.supervisor.Run(ctx, testOperatorList{value: self.list}, self.signals)
	}()
	// a failed test still joins the supervisor: it stops, and every child exits
	self.t.Cleanup(func() {
		cancel()
		self.stateLock.Lock()
		children := slices.Clone(self.children)
		self.stateLock.Unlock()
		for _, child := range children {
			select {
			case child.exit <- nil:
			default:
			}
		}
		select {
		case <-self.done:
		case <-time.After(testOperatorWait):
			self.t.Error("supervisor did not return")
		}
	})
}

func (self *testOperatorFixture) nextStart() *testOperatorChild {
	self.t.Helper()
	return testOperatorReceive(self.t, self.starts, "a provider start")
}

func (self *testOperatorFixture) noStart() {
	self.t.Helper()
	select {
	case child := <-self.starts:
		self.t.Fatalf("unexpected start of %s: %v", child.domain(), child.command.args)
	default:
	}
}

// Sends a stop signal, expects every child to get it, lets them exit, and
// waits for the supervisor to return.
func (self *testOperatorFixture) stop(signal os.Signal, children ...*testOperatorChild) {
	self.t.Helper()
	self.signals <- signal
	for _, child := range children {
		if received := testOperatorReceive(self.t, child.signals, child.domain()+" stop signal"); received != signal {
			self.t.Fatalf("%s got %v, want %v", child.domain(), received, signal)
		}
		child.exit <- nil
	}
	testOperatorReceive(self.t, self.done, "supervisor return")
	self.noStart()
}

func testOperatorReceive[T any](t *testing.T, channel <-chan T, what string) T {
	t.Helper()
	select {
	case value, ok := <-channel:
		if !ok {
			var zero T
			return zero
		}
		return value
	case <-time.After(testOperatorWait):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func testNoSignal(t *testing.T, child *testOperatorChild) {
	t.Helper()
	select {
	case received := <-child.signals:
		t.Fatalf("%s got unexpected %v", child.domain(), received)
	default:
	}
}

func TestOperatorSupervisorStartsOneProviderPerListedOperator(t *testing.T) {
	// an inherited state directory must not reach any child
	t.Setenv("URNETWORK_STATE_DIR", "/synthetic/inherited")
	fixture := newTestOperatorFixture(t)
	// nor does a network saved with choose_network
	if err := writeNetworkConfig(fixture.base, "https://api.saved.example", "wss://connect.saved.example"); err != nil {
		t.Fatal(err)
	}
	fixture.authenticate("alpha.example")
	fixture.authenticate("beta.example")
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"))
	fixture.run()
	alpha := fixture.nextStart()
	beta := fixture.nextStart()
	for _, child := range []*testOperatorChild{alpha, beta} {
		domain := child.domain()
		want := []string{"provide", "--api_url=https://api." + domain, "--connect_url=wss://connect." + domain, "--port=0"}
		if child.command.path != "/synthetic/bin/miner" || !reflect.DeepEqual(child.command.args, want) {
			t.Fatalf("%s child = %s %v, want %v", domain, child.command.path, child.command.args, want)
		}
		if directories := child.stateDirs(); !reflect.DeepEqual(directories, []string{operatorlist.DomainStateDir(fixture.base, domain)}) {
			t.Fatalf("%s state directories = %v", domain, directories)
		}
	}
	if alpha.domain() != "alpha.example" || beta.domain() != "beta.example" {
		t.Fatalf("started %s and %s", alpha.domain(), beta.domain())
	}
	fixture.stop(syscall.SIGTERM, alpha, beta)
}

func TestOperatorSupervisorPassesVerbosityAndClientRegistration(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.settings.verbosity = 2
	fixture.settings.allowClientRegistration = true
	fixture.authenticate("alpha.example")
	fixture.publish(testListedOperator("alpha.example"))
	fixture.run()
	alpha := fixture.nextStart()
	want := []string{"provide", "--api_url=https://api.alpha.example", "--connect_url=wss://connect.alpha.example", "--port=0", "--allow-client-registration", "-vv"}
	if !reflect.DeepEqual(alpha.command.args, want) {
		t.Fatalf("child args = %v, want %v", alpha.command.args, want)
	}
	fixture.stop(syscall.SIGTERM, alpha)
}

// A new operator directory has no provider client, so auto mode lets every
// child register one even when the command line did not.
func TestOperatorSupervisorAutoRegisterAllowsClientRegistration(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.settings.autoRegister = true
	fixture.settings.hotkey = testHotkey(t, 3)
	fixture.authenticate("alpha.example")
	fixture.publish(testListedOperator("alpha.example"))
	fixture.run()
	alpha := fixture.nextStart()
	if !slices.Contains(alpha.command.args, "--allow-client-registration") {
		t.Fatalf("auto mode child args = %v", alpha.command.args)
	}
	fixture.stop(syscall.SIGTERM, alpha)
}

func TestOperatorSupervisorStopsADelistedOperatorAndKeepsItsState(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.authenticate("alpha.example")
	fixture.authenticate("beta.example")
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"))
	fixture.run()
	alpha := fixture.nextStart()
	beta := fixture.nextStart()

	from := fixture.journal.length()
	fixture.publish(testListedOperator("alpha.example"))
	if received := testOperatorReceive(t, beta.signals, "delisted stop"); received != syscall.SIGTERM {
		t.Fatalf("delisted child got %v", received)
	}
	at := fixture.journal.wait(t, from, "idle 1m0s")
	// the grace period has not passed
	testNoSignal(t, beta)
	fixture.clock.set(operatorStopGrace)
	if received := testOperatorReceive(t, beta.signals, "delisted kill"); received != os.Kill {
		t.Fatalf("delisted child got %v after the grace period", received)
	}
	beta.exit <- nil
	fixture.journal.wait(t, at, "log operator beta.example: provider (pid 2) stopped")
	if _, err := clientauth.ReadToken(operatorJwtPath(fixture.base, "beta.example")); err != nil {
		t.Fatalf("delisted operator lost its state: %v", err)
	}
	testNoSignal(t, alpha)
	fixture.noStart()
	fixture.stop(syscall.SIGTERM, alpha)
}

func TestOperatorSupervisorRestartsAProviderWhoseUrlsChanged(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.authenticate("alpha.example")
	fixture.publish(testListedOperator("alpha.example"))
	fixture.run()
	alpha := fixture.nextStart()

	moved := testListedOperator("alpha.example")
	moved.ApiUrl = "https://api-two.alpha.example"
	fixture.publish(moved)
	if received := testOperatorReceive(t, alpha.signals, "url change stop"); received != syscall.SIGTERM {
		t.Fatalf("changed child got %v", received)
	}
	alpha.exit <- nil
	// a deliberate restart waits for no backoff
	restarted := fixture.nextStart()
	if restarted.option("--api_url") != "https://api-two.alpha.example" || restarted.option("--connect_url") != "wss://connect.alpha.example" {
		t.Fatalf("restarted child args = %v", restarted.command.args)
	}
	fixture.stop(syscall.SIGTERM, restarted)
}

func TestOperatorSupervisorRestartsAnExitedProviderWithBackoff(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.authenticate("alpha.example")
	fixture.publish(testListedOperator("alpha.example"))
	fixture.run()
	child := fixture.nextStart()
	var offset time.Duration
	for _, delay := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute} {
		from := fixture.journal.length()
		child.exit <- errors.New("exit status 1")
		fixture.journal.wait(t, from, "idle "+(offset+delay).String())
		fixture.noStart()
		offset += delay
		fixture.clock.set(offset)
		child = fixture.nextStart()
	}
	// a run as long as the cap starts the doubling over
	offset += operatorRestartMaximumDelay
	fixture.clock.set(offset)
	from := fixture.journal.length()
	child.exit <- nil
	fixture.journal.wait(t, from, "idle "+(offset+30*time.Second).String())
	fixture.noStart()
	fixture.clock.set(offset + 30*time.Second)
	child = fixture.nextStart()
	fixture.stop(syscall.SIGTERM, child)
}

func TestOperatorSupervisorForwardsStopSignalsAndWaitsForEveryChild(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.authenticate("alpha.example")
	fixture.authenticate("beta.example")
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"))
	fixture.run()
	alpha := fixture.nextStart()
	beta := fixture.nextStart()

	fixture.signals <- syscall.SIGINT
	for _, child := range []*testOperatorChild{alpha, beta} {
		if received := testOperatorReceive(t, child.signals, child.domain()+" interrupt"); received != syscall.SIGINT {
			t.Fatalf("%s got %v", child.domain(), received)
		}
	}
	from := fixture.journal.length()
	alpha.exit <- nil
	at := fixture.journal.wait(t, from, "log operator alpha.example: provider (pid 1) stopped")
	fixture.journal.wait(t, at, "idle 1m0s")
	select {
	case <-fixture.done:
		t.Fatal("supervisor returned before every child exited")
	default:
	}
	// a second signal is forwarded too, and the grace period still ends in a kill
	from = fixture.journal.length()
	fixture.signals <- syscall.SIGTERM
	if received := testOperatorReceive(t, beta.signals, "second signal"); received != syscall.SIGTERM {
		t.Fatalf("second signal reached beta as %v", received)
	}
	fixture.journal.wait(t, from, "idle 1m0s")
	fixture.clock.set(operatorStopGrace)
	if received := testOperatorReceive(t, beta.signals, "shutdown kill"); received != os.Kill {
		t.Fatalf("beta got %v after the grace period", received)
	}
	beta.exit <- nil
	testOperatorReceive(t, fixture.done, "supervisor return")
	fixture.noStart()
}

func TestOperatorSupervisorSharesMaxMemoryOverItsProviders(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.settings.maxMemory = 3 * 1024 * 1024 * 1024
	domains := []string{"alpha.example", "beta.example", "gamma.example"}
	for _, domain := range domains {
		fixture.authenticate(domain)
	}
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"), testListedOperator("gamma.example"))
	fixture.run()
	var children []*testOperatorChild
	for range domains {
		child := fixture.nextStart()
		if child.option("--max-memory") != "1gib" {
			t.Fatalf("%s share = %v", child.domain(), child.command.args)
		}
		children = append(children, child)
	}

	// gamma leaves; the two left restart into half each
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"))
	for _, child := range children {
		if received := testOperatorReceive(t, child.signals, child.domain()+" stop"); received != syscall.SIGTERM {
			t.Fatalf("%s got %v", child.domain(), received)
		}
		child.exit <- nil
	}
	alpha := fixture.nextStart()
	beta := fixture.nextStart()
	for _, child := range []*testOperatorChild{alpha, beta} {
		if child.option("--max-memory") != "1536mib" {
			t.Fatalf("%s share after delisting = %v", child.domain(), child.command.args)
		}
	}
	fixture.stop(syscall.SIGTERM, alpha, beta)
}

func TestFormatMemoryShareReadsBackExactly(t *testing.T) {
	for _, c := range []struct {
		byteCount connect.ByteCount
		want      string
	}{
		{byteCount: 1024 * 1024 * 1024, want: "1gib"},
		{byteCount: 1536 * 1024 * 1024, want: "1536mib"},
		{byteCount: 3 * 1024, want: "3kib"},
		{byteCount: 1024 * 1024 * 1024 / 3, want: "357913941b"},
		{byteCount: 1, want: "1b"},
	} {
		formatted := formatMemoryShare(c.byteCount)
		parsed, err := connect.ParseByteCount(formatted)
		if formatted != c.want || err != nil || parsed != c.byteCount {
			t.Errorf("%d formats as %q and reads back as %d, %v; want %q", c.byteCount, formatted, parsed, err, c.want)
		}
	}
}

func TestOperatorSupervisorCopiesTheProxyFileBeforeStarting(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	proxy := []byte(`{"auths":null,"servers":{"proxy.example:1080":""}}`)
	proxyPath := filepath.Join(fixture.base, "proxy")
	if err := os.WriteFile(proxyPath, proxy, 0640); err != nil {
		t.Fatal(err)
	}
	// independent of the test's umask
	if err := os.Chmod(proxyPath, 0640); err != nil {
		t.Fatal(err)
	}
	fixture.authenticate("alpha.example")
	fixture.publish(testListedOperator("alpha.example"))
	fixture.run()
	alpha := fixture.nextStart()
	if string(alpha.proxy) != string(proxy) || alpha.proxyMode != 0640 {
		t.Fatalf("child started with proxy %q mode %v", alpha.proxy, alpha.proxyMode)
	}
	entries, err := os.ReadDir(operatorlist.DomainStateDir(fixture.base, "alpha.example"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".proxy.tmp") {
			t.Fatalf("temporary proxy file left behind: %s", entry.Name())
		}
	}
	fixture.stop(syscall.SIGTERM, alpha)

	// without a base proxy file an operator's own is left alone
	empty := t.TempDir()
	directory := filepath.Join(empty, "operators", "alpha.example")
	if err := copyOperatorProxyFile(empty, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "proxy")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("proxy file appeared without a base copy: %v", err)
	}
}

func TestOperatorSupervisorAwaitsAuthThenStartsTheOperator(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.authenticate("alpha.example")
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"))
	fixture.run()
	// one operator awaiting auth never holds back another
	alpha := fixture.nextStart()
	at := fixture.journal.wait(t, 0, "log operator beta.example is awaiting auth: provider auth --operator=beta.example")
	at = fixture.journal.wait(t, at, "idle 5m0s")
	fixture.noStart()

	fixture.clock.set(operatorAuthRecheckInterval)
	at = fixture.journal.wait(t, at, "log operator beta.example is awaiting auth: provider auth --operator=beta.example")
	at = fixture.journal.wait(t, at, "idle 10m0s")
	fixture.noStart()

	fixture.authenticate("beta.example")
	fixture.clock.set(2 * operatorAuthRecheckInterval)
	beta := fixture.nextStart()
	if beta.domain() != "beta.example" {
		t.Fatalf("started %s after auth", beta.domain())
	}
	testNoSignal(t, alpha)
	fixture.stop(syscall.SIGTERM, alpha, beta)
}

func TestOperatorSupervisorAutoRegisterWritesTheJwt(t *testing.T) {
	hotkey := testHotkey(t, 5)
	operator := newTestWalletOperator(t, hotkey)
	fixture := newTestOperatorFixture(t)
	fixture.settings.autoRegister = true
	fixture.settings.hotkey = hotkey
	fixture.signIn = hotkeyauth.SignIn
	fixture.publish(operator.operator("local.example"))
	fixture.run()
	at := fixture.journal.wait(t, 0, "log operator local.example: signing in with the hotkey")
	fixture.journal.wait(t, at, "log operator local.example: signed in with the hotkey and created its network")
	child := fixture.nextStart()
	if child.domain() != "local.example" || !slices.Contains(child.command.args, "--allow-client-registration") {
		t.Fatalf("auto-registered child = %v", child.command.args)
	}
	jwt, err := clientauth.ReadToken(operatorJwtPath(fixture.base, "local.example"))
	if want := "jwt:" + hotkeyauth.NetworkName(hotkey.PublicKey()); err != nil || jwt != want {
		t.Fatalf("operator jwt = %q, %v; want %q", jwt, err, want)
	}
	fixture.stop(syscall.SIGTERM, child)
}

func TestOperatorSupervisorStatusShowsEachOperator(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.authenticate("alpha.example")
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"))
	fixture.run()
	alpha := fixture.nextStart()
	// published before the loop waits on beta's recheck
	fixture.journal.wait(t, 0, "idle 5m0s")
	recorder := httptest.NewRecorder()
	fixture.supervisor.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	var status struct {
		Status    string           `json:"status"`
		Operators []operatorStatus `json:"operators"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	want := []operatorStatus{{Domain: "alpha.example", State: "running", Pid: 1}, {Domain: "beta.example", State: "awaiting auth"}}
	if status.Status != "ok" || !reflect.DeepEqual(status.Operators, want) {
		t.Fatalf("status = %s", recorder.Body.String())
	}
	fixture.stop(syscall.SIGTERM, alpha)
}

// The upkeep stores the chain and delegates at start and then hourly. A
// failing operator never holds back the others, and recovers at a later run.
func TestOperatorSupervisorKeepsTheHotkeyWalletDelegated(t *testing.T) {
	coldkey, hotkey := testHotkey(t, 0x41), testHotkey(t, 0x31)
	healthy := newTestHotkeyWalletOperator(t, 1, nil)
	failing := newTestHotkeyWalletOperator(t, 2, func(operator *testHotkeyWalletOperator) {
		operator.unavailable = true
	})
	fixture := newTestOperatorFixture(t)
	fixture.settings.hotkey = hotkey
	healthy.authenticate(t, fixture.base, "healthy.example")
	failing.authenticate(t, fixture.base, "failing.example")
	chain := testHotkeyWalletChain(t, fixture.base, coldkey, hotkey)
	_, headHash, err := protocol.VerifyHotkeyWalletMappingLineage(t.Context(), chain)
	if err != nil {
		t.Fatal(err)
	}
	fixture.publish(healthy.listed("healthy.example"), failing.listed("failing.example"))
	fixture.run()
	first, second := fixture.nextStart(), fixture.nextStart()
	at := fixture.journal.wait(t, 0, "log operator healthy.example: hotkey wallet chain stored; delegated to generation 1 from epoch 53 through 65588")
	fixture.journal.waitPrefix(t, 0, "log operator failing.example: hotkey wallet not delegated: storing the chain: ")
	at = fixture.journal.wait(t, at, "log hotkey wallet generation 1 is delegated at 1 of 2 authenticated operators")
	at = fixture.journal.wait(t, at, "idle 1h0m0s")
	if delegation := healthy.delegationHead(t); delegation == nil || delegation.ConsentHeadHash != headHash || delegation.Hotkey != hotkey.PublicKey() {
		t.Fatalf("healthy operator delegation = %+v", delegation)
	}

	failing.setUnavailable(false)
	fixture.clock.set(operatorWalletUpkeepInterval)
	at = fixture.journal.wait(t, at, "log operator failing.example: hotkey wallet chain stored; delegated to generation 1 from epoch 53 through 65588")
	fixture.journal.wait(t, at, "log hotkey wallet generation 1 is delegated at 2 of 2 authenticated operators")
	if _, accepts := healthy.counts(); accepts != 1 {
		t.Fatalf("the adopted delegation was signed again: %d accepts", accepts)
	}
	fixture.stop(syscall.SIGTERM, first, second)
}
