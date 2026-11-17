package miner

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/sdk/v2026"
)

func TestExtenderStandingReadsTheBindAndTheActivation(t *testing.T) {
	at := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status *sdk.ExtenderProvideStatus
		want   minerExtenderStanding
	}{
		{"no status", nil, minerExtenderStanding{Bind: "off", Activation: "none"}},
		{"role off", &sdk.ExtenderProvideStatus{Supported: true, State: sdk.ExtenderProvideStateOff}, minerExtenderStanding{Bind: "off", Activation: "none"}},
		{"linux user", &sdk.ExtenderProvideStatus{ErrorCase: sdk.ExtenderProvideErrorTcpUnavailable, TcpUnavailableError: "listen tcp :443: bind: permission denied"}, minerExtenderStanding{Bind: "tcp_443_permission_denied", Activation: "none", Reason: "tcp_443_permission_denied"}},
		{"eperm", &sdk.ExtenderProvideStatus{TcpUnavailableError: "listen tcp :443: bind: operation not permitted"}, minerExtenderStanding{Bind: "tcp_443_permission_denied", Activation: "none", Reason: "tcp_443_permission_denied"}},
		{"windows", &sdk.ExtenderProvideStatus{TcpUnavailableError: "listen tcp :443: bind: An attempt was made to access a socket in a way forbidden by its access permissions."}, minerExtenderStanding{Bind: "tcp_443_permission_denied", Activation: "none", Reason: "tcp_443_permission_denied"}},
		{"in use", &sdk.ExtenderProvideStatus{TcpUnavailableError: "listen tcp :443: bind: address already in use"}, minerExtenderStanding{Bind: "tcp_443_in_use", Activation: "none", Reason: "tcp_443_in_use"}},
		{"other", &sdk.ExtenderProvideStatus{TcpUnavailableError: "listen tcp :443: socket: too many open files"}, minerExtenderStanding{Bind: "tcp_443_bind_failed", Activation: "none", Reason: "tcp_443_bind_failed"}},
		{"binding", &sdk.ExtenderProvideStatus{Enabled: true}, minerExtenderStanding{Bind: "starting", Activation: "pending"}},
		{"activating", &sdk.ExtenderProvideStatus{Enabled: true, Listening: true}, minerExtenderStanding{Bind: "listening", Activation: "pending"}},
		{"active", &sdk.ExtenderProvideStatus{Enabled: true, Listening: true, ActivatedV6: true, LastActivationTime: at.UnixMilli()}, minerExtenderStanding{Bind: "listening", Activation: "activated", LastActivationAt: &at}},
		// the SDK's case, and the operator's words when a status lacks it
		{"unreachable", &sdk.ExtenderProvideStatus{Enabled: true, Listening: true, ErrorCase: sdk.ExtenderProvideErrorActivationUnreachable, LastActivationTime: at.UnixMilli(), LastActivationError: "x", LastActivationRefused: true}, minerExtenderStanding{Bind: "listening", Activation: "unreachable_on_tcp_443", Reason: "unreachable_on_tcp_443", LastActivationAt: &at}},
		{"unreachable words", &sdk.ExtenderProvideStatus{Enabled: true, Listening: true, LastActivationTime: at.UnixMilli(), LastActivationError: "the tcp carrier failed: connection refused", LastActivationRefused: true}, minerExtenderStanding{Bind: "listening", Activation: "unreachable_on_tcp_443", Reason: "unreachable_on_tcp_443", LastActivationAt: &at}},
		{"refused", &sdk.ExtenderProvideStatus{Enabled: true, Listening: true, LastActivationTime: at.UnixMilli(), LastActivationError: "the key is another network's", LastActivationRefused: true}, minerExtenderStanding{Bind: "listening", Activation: "refused", LastActivationAt: &at}},
		{"failed", &sdk.ExtenderProvideStatus{Enabled: true, Listening: true, LastActivationTime: at.UnixMilli(), LastActivationError: "timeout"}, minerExtenderStanding{Bind: "listening", Activation: "failed", LastActivationAt: &at}},
		{"revoked", &sdk.ExtenderProvideStatus{Enabled: true, Listening: true, RevokedTime: at.UnixMilli()}, minerExtenderStanding{Bind: "listening", Activation: "revoked"}},
		{"tcp lost", &sdk.ExtenderProvideStatus{Enabled: true, ListenError: "tcp: accept tcp [::]:443: use of closed network connection"}, minerExtenderStanding{Bind: "tcp_443_bind_failed", Activation: "pending", Reason: "tcp_443_bind_failed"}},
	}
	for _, c := range cases {
		got := minerExtenderStandingOf(c.status)
		if got.Bind != c.want.Bind || got.Activation != c.want.Activation || got.Reason != c.want.Reason ||
			(got.LastActivationAt == nil) != (c.want.LastActivationAt == nil) ||
			got.LastActivationAt != nil && !got.LastActivationAt.Equal(*c.want.LastActivationAt) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestExtenderBindFixIsTheExactCommand(t *testing.T) {
	cases := []struct {
		goos       string
		executable string
		installer  bool
		username   string
		euid       int
		want       string
	}{
		{"linux", "/home/alice/.local/bin/provider", false, "alice", 1000, "sudo /home/alice/.local/bin/provider service install --system --as=alice"},
		{"linux", "/home/al ice/bin/provider", false, "alice", 1000, "sudo '/home/al ice/bin/provider' service install --system --as=alice"},
		{"linux", "/home/alice/.local/share/urnetwork-provider/bin/urnetwork", true, "alice", 1000, "curl -fsSL https://raw.githubusercontent.com/urnetwork/connect/main/scripts/Provider_Install_Linux.sh | sh -s -- --system"},
		// root lacks the capability only in a sandbox, which no command here changes
		{"linux", "/usr/local/bin/provider", false, "root", 0, ""},
		// other systems don't keep port 443 from a user
		{"darwin", "/Users/alice/.local/bin/provider", false, "alice", 501, ""},
		{"windows", `C:\Users\alice\provider.exe`, false, "alice", -1, ""},
	}
	for _, c := range cases {
		if got := minerExtenderBindFixFor(c.goos, c.executable, c.installer, c.username, c.euid); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.goos, c.executable, got, c.want)
		}
	}
	// the log names the fix it was given, and root gets the sandbox reason
	fix := "sudo /home/alice/.local/bin/provider service install --system --as=alice"
	if guidance := minerExtenderGuidance(minerExtenderReasonTcpPermission, fix, 1000); !strings.Contains(guidance, "run: "+fix) {
		t.Fatal(guidance)
	}
	if guidance := minerExtenderGuidance(minerExtenderReasonTcpPermission, "", 0); !strings.Contains(guidance, "CAP_NET_BIND_SERVICE") {
		t.Fatal(guidance)
	}
	if minerExtenderGuidance("", fix, 1000) != "" {
		t.Fatal("guidance without a reason")
	}
}

// What provider status and provider doctor print for each standing.
func TestExtenderRowSaysTheFix(t *testing.T) {
	now := time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)
	at := now.Add(-2 * time.Hour)
	fix := "sudo /home/alice/.local/bin/provider service install --system --as=alice"
	cases := []struct {
		standing *minerExtenderStanding
		mark     any
		says     string
	}{
		{nil, nil, "doesn't report its extender"},
		{&minerExtenderStanding{Bind: "off", Activation: "none"}, nil, "off on this miner"},
		{&minerExtenderStanding{Bind: "tcp_443_permission_denied", Activation: "none", Reason: "tcp_443_permission_denied"}, "!", "off: may not bind TCP 443 (permission denied). Fix: " + fix},
		{&minerExtenderStanding{Bind: "tcp_443_in_use", Activation: "none", Reason: "tcp_443_in_use"}, "!", "another program holds TCP 443"},
		{&minerExtenderStanding{Bind: "listening", Activation: "unreachable_on_tcp_443", Reason: "unreachable_on_tcp_443", LastActivationAt: &at}, "!", "unreachable on TCP 443 from the internet, last tried 2h 0m ago. Open inbound TCP 443, UDP 443 and UDP 4053"},
		{&minerExtenderStanding{Bind: "listening", Activation: "activated", LastActivationAt: &at}, true, "TCP 443 bound and activated, last tried 2h 0m ago"},
		{&minerExtenderStanding{Bind: "listening", Activation: "pending"}, nil, "TCP 443 bound, activating"},
	}
	for _, c := range cases {
		mark, detail := minerExtenderRow(c.standing, fix, 1000, now)
		if mark != c.mark || !strings.Contains(detail, c.says) {
			t.Errorf("%+v: %v %q, want %v %q", c.standing, mark, detail, c.mark, c.says)
		}
	}
	// a refused bind with no command to give points at the page
	if _, detail := minerExtenderRow(&minerExtenderStanding{Bind: "tcp_443_permission_denied"}, "", 0, now); !strings.Contains(detail, minerExtenderDocs) {
		t.Fatal(detail)
	}
}

// The run's listener has the fix for this install, and keeps the standing
// for the status file.
func TestExtenderListenerNamesTheFixAndKeepsTheStanding(t *testing.T) {
	fix := "sudo /home/alice/.local/bin/provider service install --system --as=alice"
	sink := &providerDiagnosticRecorder{records: make(chan []byte, 4)}
	owner := newProviderDiagnosticTestOwner(t, sink)
	standings := newMinerExtenderStandings()
	listener := newProviderExtenderStatusListener(owner, 1).withStanding(standings, fix)
	listener.ExtenderProvideStatusChanged(&sdk.ExtenderProvideStatus{ErrorCase: sdk.ExtenderProvideErrorTcpUnavailable, TcpUnavailableError: "listen tcp :443: bind: permission denied"})
	raw := sink.next(t)
	if os.Geteuid() != 0 && !bytes.Contains(raw, []byte("To let it bind the port, run: "+fix)) {
		t.Fatalf("the record doesn't name the fix:\n%s", raw)
	}
	if current := standings.current(); current == nil || current.Reason != minerExtenderReasonTcpPermission || current.Bind != minerExtenderReasonTcpPermission {
		t.Fatalf("%+v", current)
	}
}

func TestExtenderStandingsPickTheSlotThatRunsTheRole(t *testing.T) {
	var none *minerExtenderStandings
	none.set(0, minerExtenderStanding{Bind: "listening"})
	if none.current() != nil {
		t.Fatal("a nil holder reported")
	}
	standings := newMinerExtenderStandings()
	if standings.current() != nil {
		t.Fatal("reported before any slot did")
	}
	standings.set(0, minerExtenderStanding{Bind: "off", Activation: "none"})
	if current := standings.current(); current == nil || current.Bind != "off" {
		t.Fatalf("%+v", current)
	}
	standings.set(2, minerExtenderStanding{Bind: "tcp_443_permission_denied", Reason: "tcp_443_permission_denied"})
	standings.set(1, minerExtenderStanding{Bind: "off", Activation: "none"})
	if current := standings.current(); current == nil || current.Reason != "tcp_443_permission_denied" {
		t.Fatalf("%+v", current)
	}
}

// The unit a system service run as a user is: that user, the one capability
// at every start (so a replaced binary keeps it, unlike setcap), and a root
// path unit that only ever runs systemctl.
func TestSystemdUnitsAsUserGrantOnlyTheBind(t *testing.T) {
	t.Setenv("URNETWORK_UPDATE_URL", "")
	t.Setenv("URNETWORK_AUTO_UPDATE", "")
	t.Setenv("URNETWORK_CONNECT_EXTRA_ROOT_CA_FILE", "")
	units := systemdUnitSet("urnetwork-provider", "/home/alice/.local/bin/provider", "/home/alice/.urnetwork", true, "alice", "/usr/bin/systemctl")
	want := `# Installed by provider service install --system --as=alice. Remove with: sudo provider service uninstall --system
[Unit]
Description=URnetwork miner
Documentation=https://ur.xyz/docs/miner/extender
Wants=network-online.target
After=network-online.target

[Service]
User=alice
ExecStart=/home/alice/.local/bin/provider start
Environment=URNETWORK_STATE_DIR=/home/alice/.urnetwork
# the one privilege the extender needs: to listen on ports below 1024 (TCP and UDP 443)
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
# 3 means the device needs setup (provider setup); restarting won't help
Restart=on-failure
RestartSec=15
RestartPreventExitStatus=3

[Install]
WantedBy=multi-user.target
`
	if got := units["urnetwork-provider.service"]; got != want {
		t.Fatalf("service unit:\n%s\nwant:\n%s", got, want)
	}
	update := units["urnetwork-provider-update.service"]
	if !strings.Contains(update, "User=alice\nNoNewPrivileges=yes\nExecStart=/home/alice/.local/bin/provider update --auto\n") || strings.Contains(update, "Capabilit") {
		t.Fatalf("update unit:\n%s", update)
	}
	path := units["urnetwork-provider-restart.path"]
	if !strings.Contains(path, "PathChanged=/home/alice/.urnetwork/service-restart.request\n") || !strings.Contains(path, "WantedBy=multi-user.target") {
		t.Fatalf("restart path:\n%s", path)
	}
	restart := units["urnetwork-provider-restart.service"]
	if !strings.Contains(restart, "ExecStart=/usr/bin/systemctl try-restart urnetwork-provider.service\n") || strings.Contains(restart, "User=") || strings.Contains(restart, "/home/alice/.local/bin/provider") {
		t.Fatalf("root's restart unit must run systemctl alone:\n%s", restart)
	}
	if len(units) != 5 {
		t.Fatalf("%d units", len(units))
	}
	if systemdUnitUser(units["urnetwork-provider.service"]) != "alice" {
		t.Fatal("the unit's user doesn't read back")
	}
	// a user service and a root service are as they were: no user, no
	// capability lines, no restart units
	for _, system := range []bool{false, true} {
		plain := systemdUnits("urnetwork-provider", "/opt/urnetwork/bin/provider", "/var/lib/urnetwork", system)
		for file, text := range plain {
			if strings.Contains(text, "User=") || strings.Contains(text, "Capabilit") || strings.Contains(text, "NoNewPrivileges") || strings.Contains(file, "restart") {
				t.Fatalf("system=%v %s:\n%s", system, file, text)
			}
		}
		if len(plain) != 3 || systemdUnitUser(plain["urnetwork-provider.service"]) != "" {
			t.Fatalf("system=%v: %d units", system, len(plain))
		}
	}
	if systemdUnitUser("[Service]\nUser=root\n") != "" {
		t.Fatal("root read as a user")
	}
}

// Which user and state folder --as installs for.
func TestServiceUserSelection(t *testing.T) {
	users := map[string]*user.User{
		"alice":  {Username: "alice", Uid: "1000", Gid: "1000", HomeDir: "/home/alice"},
		"root":   {Username: "root", Uid: "0", Gid: "0", HomeDir: "/root"},
		"nohome": {Username: "nohome", Uid: "1001", Gid: "1001", HomeDir: ""},
	}
	lookup := func(name string) (*user.User, error) {
		if found, ok := users[name]; ok {
			return found, nil
		}
		return nil, user.UnknownUserError(name)
	}
	env := map[string]string{}
	getenv := func(key string) string { return env[key] }
	got, err := minerServiceUserFor("alice", lookup, getenv)
	if err != nil || got.name != "alice" || got.uid != 1000 || got.gid != 1000 || got.stateDir != "/home/alice/.urnetwork" {
		t.Fatalf("%+v %v", got, err)
	}
	env["URNETWORK_STATE_DIR"] = "/srv/miner/state/"
	if got, err := minerServiceUserFor("alice", lookup, getenv); err != nil || got.stateDir != "/srv/miner/state" {
		t.Fatalf("%+v %v", got, err)
	}
	env["URNETWORK_STATE_DIR"] = "relative/state"
	if _, err := minerServiceUserFor("alice", lookup, getenv); err == nil {
		t.Fatal("a relative state folder was taken")
	}
	delete(env, "URNETWORK_STATE_DIR")
	for name, says := range map[string]string{"root": "leave it out", "bob": "no user bob", "nohome": "no home folder", "-rf": "isn't a user name", "a b": "isn't a user name"} {
		if _, err := minerServiceUserFor(name, lookup, getenv); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a user service on Linux says how to get the bind; a system service and
	// other systems don't
	current, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	if hint := minerServiceBindHint("linux", false, 1000, "/home/me/.local/bin/provider"); hint != "sudo /home/me/.local/bin/provider service install --system --as="+cliShellQuote(current.Username) {
		t.Fatal(hint)
	}
	if minerServiceBindHint("linux", true, 0, "/usr/local/bin/provider") != "" || minerServiceBindHint("darwin", false, 501, "/Users/me/provider") != "" {
		t.Fatal("hint where nothing keeps the port")
	}
}

// Never two services on one identity: a user service install finds the
// system service that runs this user's miner, and the takeover of a user's
// own service acts as that user and refuses the installer's.
func TestUserAndSystemServicesNeverRunTogether(t *testing.T) {
	phase2State(t)
	current, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root's system service runs as root, not as a user")
	}
	systemDir := t.TempDir()
	systemdSystemUnitDir = systemDir
	defer func() { systemdSystemUnitDir = "/etc/systemd/system" }()
	userManager := &systemdService{name: minerServiceName(), dir: t.TempDir()}
	if ownSystemService(userManager) != nil {
		t.Fatal("found a system service where there is none")
	}
	units := systemdUnitSet(minerServiceName(), "/home/x/provider", "/home/x/.urnetwork", true, current.Username, "/usr/bin/systemctl")
	for file, text := range units {
		os.WriteFile(filepath.Join(systemDir, file), []byte(text), 0644)
	}
	own := ownSystemService(userManager)
	if own == nil || own.user() != current.Username || !strings.Contains(own.kind(), "as "+current.Username) {
		t.Fatalf("the system service of this user wasn't found: %+v", own)
	}
	if ownSystemService(own) != nil {
		t.Fatal("a system manager looked for itself")
	}
	if serviceRunsAs(own) != current.Username || serviceRunsAs(userManager) != "" {
		t.Fatal("serviceRunsAs")
	}
	// another user's system service is not this user's
	other := systemdUnitSet(minerServiceName(), "/home/x/provider", "/home/x/.urnetwork", true, "someone-else", "/usr/bin/systemctl")
	os.WriteFile(filepath.Join(systemDir, minerServiceName()+".service"), []byte(other[minerServiceName()+".service"]), 0644)
	if ownSystemService(userManager) != nil {
		t.Fatal("another user's service was taken as this user's")
	}

	// the installer's system service (urnet-tools --system) for this user is
	// the installer's miner, from the user's own commands too
	os.Remove(filepath.Join(systemDir, minerServiceName()+".service"))
	t.Setenv("URNETWORK_STATE_DIR", filepath.Join(current.HomeDir, ".urnetwork"))
	installerUnit := "# Installed by urnet-tools --system. Remove with: urnet-tools uninstall\n[Service]\nUser=" + current.Username + "\nExecStart=\"/home/x/.local/share/urnetwork-provider/bin/urnetwork\" provide\n"
	os.WriteFile(filepath.Join(systemDir, "urnetwork.service"), []byte(installerUnit), 0644)
	installer := installerService(userManager)
	if installer == nil || installer.unit() != "urnetwork.service" || !strings.Contains(installer.switchHint(), "urnet-tools uninstall") {
		t.Fatalf("the installer's system service wasn't found: %+v", installer)
	}
	os.WriteFile(filepath.Join(systemDir, "urnetwork.service"), []byte(strings.Replace(installerUnit, "User="+current.Username, "User=someone-else", 1)), 0644)
	if installerService(userManager) != nil {
		t.Fatal("another user's installer service was taken as this user's")
	}
	os.Remove(filepath.Join(systemDir, "urnetwork.service"))

	// the takeover
	home := t.TempDir()
	target := &minerServiceUser{name: "alice", uid: 1000, gid: 1000, home: home, stateDir: filepath.Join(home, ".urnetwork")}
	userDir := filepath.Join(home, ".config", "systemd", "user")
	os.MkdirAll(userDir, 0755)
	var calls []string
	cliRun = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "runuser" && 3 < len(args) && args[3] == "rm" {
			for _, file := range args[6:] {
				os.Remove(file)
			}
		}
		return "", nil
	}
	defer func() { cliRun = defaultCliRun }()
	var out bytes.Buffer
	if problem := takeOverUserService(target, &out); problem != "" || 0 < len(calls) {
		t.Fatalf("nothing to take over: %q %v", problem, calls)
	}
	for _, suffix := range []string{".service", "-update.service", "-update.timer"} {
		os.WriteFile(filepath.Join(userDir, minerServiceName()+suffix), []byte("[Unit]\n"), 0644)
	}
	if problem := takeOverUserService(target, &out); problem != "" {
		t.Fatal(problem)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"runuser -u alice -- env XDG_RUNTIME_DIR=/run/user/1000 systemctl --user disable --now " + minerServiceName() + ".service " + minerServiceName() + "-update.timer",
		"runuser -u alice -- rm -f -- " + filepath.Join(userDir, minerServiceName()+".service"),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in\n%s", want, joined)
		}
	}
	if _, err := os.Stat(filepath.Join(userDir, minerServiceName()+".service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the user service is still there")
	}
	// a user service that can't be removed stops the install
	cliRun = func(string, ...string) (string, error) { return "", nil }
	os.WriteFile(filepath.Join(userDir, minerServiceName()+".service"), []byte("[Unit]\n"), 0644)
	if problem := takeOverUserService(target, &out); !strings.Contains(problem, "provider service uninstall") {
		t.Fatal(problem)
	}
	os.Remove(filepath.Join(userDir, minerServiceName()+".service"))
	// the installer's service is the installer's to switch
	os.WriteFile(filepath.Join(userDir, "urnetwork.service"), []byte("[Unit]\n"), 0644)
	if problem := takeOverUserService(target, &out); !strings.Contains(problem, minerExtenderInstallerFix) {
		t.Fatal(problem)
	}
}

// The user a system service runs as can't restart it; its update asks root's
// path unit by writing the restart file, and calls no systemctl.
func TestSystemServiceAsUserRestartsThroughTheRestartFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root restarts with systemctl")
	}
	phase2State(t)
	stateDir := t.TempDir()
	unitDir := t.TempDir()
	manager := &systemdService{system: true, name: "urnetwork-provider", dir: unitDir}
	units := systemdUnitSet(manager.name, "/home/alice/.local/bin/provider", stateDir, true, "alice", "/usr/bin/systemctl")
	for file, text := range units {
		os.WriteFile(filepath.Join(unitDir, file), []byte(text), 0644)
	}
	var calls []string
	cliRun = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	defer func() { cliRun = defaultCliRun }()
	if err := manager.control("restart"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, minerServiceRestartFileName))
	if err != nil || len(raw) == 0 || 0 < len(calls) {
		t.Fatalf("restart file %q %v, calls %v", raw, err, calls)
	}
	// stop is systemctl's, which says when it needs root
	if err := manager.control("stop"); err != nil || len(calls) != 1 || calls[0] != "systemctl stop urnetwork-provider.service" {
		t.Fatalf("%v %v", err, calls)
	}
	// uninstall takes the restart units too
	if err := manager.uninstall(&bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(unitDir); len(entries) != 0 {
		t.Fatalf("left %v", entries)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "systemctl disable --now urnetwork-provider-restart.path") {
		t.Fatalf("%v", calls)
	}
}

func TestOwnerOnlyProblem(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root owns everything it makes")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "provider")
	os.WriteFile(file, []byte("x"), 0755)
	uid := os.Geteuid()
	if problem := ownerOnlyProblem(file, uid); problem != "" && !strings.Contains(problem, "can be changed by a user other than") {
		t.Fatal(problem)
	}
	// the file of another user, as the service's user sees it
	if problem := ownerOnlyProblem(file, uid+1); problem == "" {
		t.Fatal("another user's file was taken")
	}
	os.Chmod(file, 0777)
	if problem := ownerOnlyProblem(file, uid); problem == "" {
		t.Fatal("a world-writable file was taken")
	}
	if problem := ownerOnlyProblem(filepath.Join(dir, "missing"), uid); problem == "" {
		t.Fatal("a missing file was taken")
	}
}

// The whole --system --as install, as root on Linux: the user it names, that
// user's state folder, the restart file made as that user, and the five units.
func TestServiceInstallSystemAsUser(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("a system service install needs root on Linux")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("no systemctl")
	}
	phase2State(t)
	executable := fakeExecutable(t)
	systemDir := t.TempDir()
	systemdSystemUnitDir = systemDir
	defer func() { systemdSystemUnitDir = "/etc/systemd/system" }()
	home := t.TempDir()
	stateDir := filepath.Join(home, ".urnetwork")
	os.Mkdir(stateDir, 0700)
	cliUserLookup = func(name string) (*user.User, error) {
		if name != "alice" {
			return nil, user.UnknownUserError(name)
		}
		return &user.User{Username: "alice", Uid: "1000", Gid: "1000", HomeDir: home}, nil
	}
	defer func() { cliUserLookup = user.Lookup }()
	// no miner runs to report in
	cliServiceStartWait = 0
	defer func() { cliServiceStartWait = 20 * time.Second }()
	var calls []string
	cliRun = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	defer func() { cliRun = defaultCliRun }()
	t.Setenv("URNETWORK_STATE_DIR", "")
	var out, errOut bytes.Buffer
	if code := serviceCmd(docopt.Opts{"install": true, "--system": true, "--as": "alice"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	unit, err := os.ReadFile(filepath.Join(systemDir, "urnetwork-provider.service"))
	if err != nil || !strings.Contains(string(unit), "User=alice\nExecStart="+executable+" start\nEnvironment=URNETWORK_STATE_DIR="+stateDir+"\n") || !strings.Contains(string(unit), "AmbientCapabilities=CAP_NET_BIND_SERVICE") {
		t.Fatalf("%v\n%s", err, unit)
	}
	for _, file := range []string{"urnetwork-provider-update.service", "urnetwork-provider-update.timer", "urnetwork-provider-restart.path", "urnetwork-provider-restart.service"} {
		if _, err := os.Stat(filepath.Join(systemDir, file)); err != nil {
			t.Fatal(err)
		}
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"runuser -u alice -- touch -- " + filepath.Join(stateDir, minerServiceRestartFileName),
		"systemctl daemon-reload",
		"systemctl enable --now urnetwork-provider.service urnetwork-provider-update.timer urnetwork-provider-restart.path",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "loginctl") || strings.Contains(joined, "--user") {
		t.Fatalf("a system service touched a user manager:\n%s", joined)
	}
	if !strings.Contains(out.String(), "It runs as alice and may bind ports below 1024") {
		t.Fatal(out.String())
	}
	// the user service install of the same user now refuses: never two
	// services on one identity. A user runs it, so look as that user would
	calls = nil
	if code := serviceCmd(docopt.Opts{"install": true, "--system": true, "--as": "bob"}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "no user bob") {
		t.Fatalf("an unknown user was taken: %s", errOut.String())
	}
	// no state folder yet: sign in first, nothing changed
	os.RemoveAll(stateDir)
	errOut.Reset()
	if code := serviceCmd(docopt.Opts{"install": true, "--system": true, "--as": "alice"}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "provider setup --no-service") {
		t.Fatalf("installed without a state folder: %s", errOut.String())
	}
	for _, call := range calls {
		for _, change := range []string{"enable", "disable", "daemon-reload", "touch", "restart", "rm "} {
			if strings.Contains(call, change) {
				t.Fatalf("a refused install changed something: %v", calls)
			}
		}
	}
}

// --as names the user of a system service, and only of one.
func TestServiceAsParsesOnlyWithSystem(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, SkipHelpFlags: true}
	opts, err := parser.ParseArgs(mainUsage(), []string{"service", "install", "--system", "--as=alice"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if as, _ := opts.String("--as"); as != "alice" || !cliOptBool(opts, "--system") || !cliOptBool(opts, "install") {
		t.Fatalf("%v", opts)
	}
	for _, args := range [][]string{
		{"service", "uninstall", "--system", "--as=alice"},
		{"service", "start", "--system", "--as=alice"},
	} {
		if _, err := parser.ParseArgs(mainUsage(), args, ""); err == nil {
			t.Errorf("%v parsed", args)
		}
	}
	// docopt takes --as without --system, so the command refuses it, before
	// it looks up anyone or changes anything
	phase2State(t)
	var calls []string
	cliRun = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	defer func() { cliRun = defaultCliRun }()
	cliUserLookup = func(name string) (*user.User, error) {
		t.Fatal("looked up a user")
		return nil, nil
	}
	defer func() { cliUserLookup = user.Lookup }()
	var out, errOut bytes.Buffer
	code := serviceCmd(docopt.Opts{"install": true, "--as": "alice"}, &out, &errOut)
	if code == 0 || 0 < len(calls) {
		t.Fatalf("exit %d, calls %v: %s", code, calls, errOut.String())
	}
	if runtime.GOOS == "linux" && !strings.Contains(errOut.String(), "--system --as=alice") {
		t.Fatal(errOut.String())
	}
}
