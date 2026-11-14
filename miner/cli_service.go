package miner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
)

// provider service runs the miner in the background and on every boot, with
// the system's own service manager: a systemd unit on Linux, a launchd job
// on macOS. Next to it a timer runs provider update --auto (cli_update.go).
// Windows has no service manager a standard user may use, so there a Startup
// folder shortcut starts a supervisor at sign-in (internal/winservice). It
// never touches the URnetwork installer's urnetwork.service or urnetwork.lnk.

const minerServiceDefaultName = "urnetwork-provider"
const minerLaunchdLabel = "xyz.ur.provider"

// The state directory a system service uses unless URNETWORK_STATE_DIR names one.
const minerSystemStateDir = "/var/lib/urnetwork"

// minerServiceName is the unit name; URNETWORK_SERVICE_NAME changes it, so
// a test build can sit beside a real install.
func minerServiceName() string {
	if name := strings.TrimSpace(os.Getenv("URNETWORK_SERVICE_NAME")); name != "" && minerServiceNamePattern(name) {
		return name
	}
	return minerServiceDefaultName
}

func minerServiceNamePattern(name string) bool {
	if len(name) == 0 || 64 < len(name) {
		return false
	}
	for _, r := range name {
		if !(r == '-' || r == '_' || r == '.' || '0' <= r && r <= '9' || 'a' <= r && r <= 'z') {
			return false
		}
	}
	return true
}

func minerLaunchdName() string {
	if name := minerServiceName(); name != minerServiceDefaultName {
		return minerLaunchdLabel + "." + name
	}
	return minerLaunchdLabel
}

// cliRun runs a service manager command. Tests replace it.
var cliRun = func(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	var out bytes.Buffer
	command.Stdout = &out
	command.Stderr = &out
	err := command.Run()
	return strings.TrimSpace(out.String()), err
}

// cliRunAttached runs a command on this terminal (logs --follow). Tests replace it.
var cliRunAttached = func(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// How long install and start wait for the service's miner to report in.
// Tests replace it.
var cliServiceStartWait = 20 * time.Second

// cliUserLookup finds the user of --as. Tests replace it.
var cliUserLookup = user.Lookup

// cliExecutable is the binary a service runs. Tests replace it.
var cliExecutable = func() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

// minerServiceEnvironment is the part of this environment the service needs
// too: extra TLS roots behind an inspecting proxy, the update settings, and the
// extender's router port mapping (connect/EXTENDER.md S10), so
// URNETWORK_PORT_MAPPING=0 provider service install keeps it off.
func minerServiceEnvironment() []string {
	out := []string{}
	for _, key := range []string{"URNETWORK_CONNECT_EXTRA_ROOT_CA_FILE", "URNETWORK_AUTO_UPDATE", "URNETWORK_UPDATE_URL", minerPortMappingEnv} {
		if value := os.Getenv(key); value != "" {
			out = append(out, key+"="+value)
		}
	}
	return out
}

// minerServiceManager is what one platform's service manager can do.
type minerServiceManager interface {
	kind() string
	installed() bool
	active() bool
	install(executable string, stateDir string, out io.Writer) error
	uninstall(out io.Writer) error
	control(action string) error
	logs(follow bool) error
	// foreign names a miner service this command didn't install, if any.
	foreign() string
	// configured is the binary and state folder the installed service uses.
	configured() (executable string, stateDir string)
}

// minerServiceFor picks the manager for this system, or explains why none.
func minerServiceFor(system bool) (minerServiceManager, error) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("systemctl"); err != nil {
			return nil, errors.New("This system has no systemd, so provider service can't manage the miner here.\n  Run provider start under your own service manager, tmux or screen instead.")
		}
		return newSystemdService(system), nil
	case "darwin":
		return newLaunchdService(system), nil
	case "windows":
		if system {
			return nil, errors.New(windowsSystemProblem())
		}
		return newWindowsService()
	}
	return nil, fmt.Errorf("provider service doesn't support %s yet.\n  Run provider start under your system's service manager instead.", runtime.GOOS)
}

// minerStartHint is how to start a miner that isn't running on this system.
func minerStartHint() string {
	return "provider start, or provider service install"
}

// minerServiceWhen is when the service starts by itself.
func minerServiceWhen() string {
	if runtime.GOOS == "windows" {
		return "whenever you sign in to Windows"
	}
	return "on every boot"
}

// minerServiceNextStart is when a stopped service starts again.
func minerServiceNextStart() string {
	if runtime.GOOS == "windows" {
		return "the next time you sign in"
	}
	return "on the next boot"
}

// minerOwnFolderHint is where to put a binary updates can replace.
func minerOwnFolderHint() string {
	if runtime.GOOS == "windows" {
		return `Put provider.exe in a folder you own, like %LOCALAPPDATA%\Programs\urnetwork, and install from there.`
	}
	return "Put the binary in a folder you own (like ~/.local/bin), or use a system service."
}

// installerMiner is the URnetwork installer's own background miner, when it
// mines this state folder: the everyday service commands drive it, and
// install refuses to start a second miner on the same identity.
type installerMiner interface {
	minerServiceManager
	// label is its name in messages, unit what provider status calls it
	label() string
	unit() string
	enabled() bool
	// switchHint is why install refuses, and how to switch
	switchHint() string
	// stopHint is how to stop it and keep it from starting again
	stopHint() string
}

// windowsSystemProblem answers --system on Windows, which has no root and no
// system service here.
func windowsSystemProblem() string {
	return "There's no system service on Windows. Run it again without --system,\n  in PowerShell as the account that should mine."
}

// minerServiceStateDir is the state directory the service will use.
func minerServiceStateDir(system bool) (string, error) {
	if override := strings.TrimSpace(os.Getenv("URNETWORK_STATE_DIR")); override != "" || !system {
		return providerStateDir()
	}
	return minerSystemStateDir, nil
}

func serviceCmd(opts docopt.Opts, stdout io.Writer, stderr io.Writer) int {
	system, _ := opts.Bool("--system")
	asName, _ := opts.String("--as")
	if system && runtime.GOOS == "windows" {
		writeCliError(stderr, windowsSystemProblem())
		return 1
	}
	if asName != "" && runtime.GOOS != "linux" {
		writeCliError(stderr, "--as is for Linux, where a system service may run the miner as a user with only the right to bind port 443.")
		return 1
	}
	if asName != "" && !system {
		// docopt takes the option alone too; it names the user of a system service
		writeCliError(stderr, "--as names the user a system service runs the miner as. Run it with --system:\n  sudo provider service install --system --as="+asName)
		return 1
	}
	// status and logs only read, so a user may look at a system service
	readOnly := cliOptBool(opts, "status") || cliOptBool(opts, "logs")
	if system && os.Geteuid() != 0 && !readOnly {
		writeCliError(stderr, "A system service needs root. Run it again with sudo:\n  sudo "+strings.Join(append([]string{"provider"}, os.Args[1:]...), " "))
		return 1
	}
	manager, err := minerServiceFor(system)
	if err != nil {
		writeCliError(stderr, err.Error())
		return 1
	}
	var asUser *minerServiceUser
	if asName != "" {
		asUser, err = minerServiceUserFor(asName, cliUserLookup, os.Getenv)
		if err != nil {
			writeCliError(stderr, err.Error())
			return 1
		}
		// the target user's state folder, for every read below: the status
		// file, the running miner and its sign-in
		os.Setenv("URNETWORK_STATE_DIR", asUser.stateDir)
		if systemd, ok := manager.(*systemdService); ok {
			systemd.runAs = asUser.name
		}
	}
	name := minerServiceName()
	byInstaller := false
	if !cliOptBool(opts, "install") && !cliOptBool(opts, "uninstall") && !manager.installed() {
		// start, stop, status and logs also work on the installer's service,
		// and on the system service that runs the miner as this user
		if other := installerService(manager); other != nil {
			manager, name, byInstaller = other, other.label(), true
		} else if own := ownSystemService(manager); own != nil {
			manager = own
		}
	}
	switch {
	case cliOptBool(opts, "install"):
		if other := installerService(manager); other != nil && other.enabled() {
			writeCliError(stderr, other.switchHint())
			return 1
		}
		if own := ownSystemService(manager); own != nil {
			// a user service beside it would be a second miner on this identity
			writeCliError(stderr, "This device already runs the miner as a system service ("+own.unit()+", as "+own.user()+"), which may bind port 443 for the extender.\n  Keep using it: provider service status, logs; sudo provider service start, stop, restart.\n  To use a user service instead, remove it first: sudo provider service uninstall --system")
			return 1
		}
		if asUser != nil {
			if problem := takeOverUserService(asUser, stdout); problem != "" {
				writeCliError(stderr, problem)
				return 1
			}
		}
		if problem := otherMinerProblem(manager); problem != "" {
			writeCliError(stderr, problem)
			return 1
		}
		executable, err := cliExecutable()
		if err != nil {
			writeCliError(stderr, "Couldn't find this program's own path: "+err.Error())
			return 1
		}
		if asUser != nil {
			// the service runs this file as the user: only root and that user
			// may be able to change it
			if problem := ownerOnlyProblem(executable, asUser.uid); problem != "" {
				writeCliError(stderr, problem+"\n  The service runs it as "+asUser.name+", so only root and "+asUser.name+" may be able to change it. Put it in a folder of "+asUser.name+"'s, like ~/.local/bin, and install from there.")
				return 1
			}
			if _, err := os.Stat(asUser.stateDir); errors.Is(err, os.ErrNotExist) {
				writeCliError(stderr, asUser.name+" has no miner state in "+asUser.stateDir+" yet. Sign the miner in first, as "+asUser.name+": provider setup --no-service\n  then run this again.")
				return 1
			}
			if problem := ownerOnlyProblem(asUser.stateDir, asUser.uid); problem != "" {
				writeCliError(stderr, problem+"\n  The service keeps "+asUser.name+"'s miner identity there, so only root and "+asUser.name+" may be able to change it.")
				return 1
			}
		} else if system {
			// root will run this file: only root may be able to change it
			if problem := rootOnlyProblem(executable); problem != "" {
				// Homebrew makes /usr/local/bin a user's own folder
				dest := "/usr/local/bin"
				if rootOnlyProblem(dest) != "" {
					dest = "/opt/urnetwork/bin"
				}
				writeCliError(stderr, problem+"\n  A system service runs as root, so its binary must be one only root can change. Copy it first:\n  sudo mkdir -p "+dest+" && sudo install -m 755 "+cliShellQuote(executable)+" "+dest+"/provider\n  sudo "+dest+"/provider service install --system")
				return 1
			}
		}
		stateDir, err := minerServiceStateDir(system)
		if err != nil {
			writeCliError(stderr, err.Error())
			return 1
		}
		if asUser != nil {
			stateDir = asUser.stateDir
		}
		if problem := serviceValueProblem(append([]string{executable, stateDir}, minerServiceEnvironment()...)...); problem != "" {
			writeCliError(stderr, "The service wasn't installed: "+problem+".")
			return 1
		}
		if err := manager.install(executable, stateDir, stdout); err != nil {
			writeCliError(stderr, "The service wasn't installed: "+err.Error())
			return 1
		}
		fmt.Fprintf(stdout, "  %s Installed %s (%s); it starts now and %s\n", cliGreen(stdout, "✓"), name, manager.kind(), minerServiceWhen())
		if asUser != nil {
			fmt.Fprintf(stdout, "  %s It runs as %s and may bind ports below 1024, such as the extender's TCP 443; nothing else\n", cliGreen(stdout, "✓"), asUser.name)
		}
		fmt.Fprintf(stdout, "  %s Automatic updates: checked every 6 hours\n", cliGreen(stdout, "✓"))
		if problem := writableDirProblem(filepath.Dir(executable)); problem != "" {
			fmt.Fprintf(stdout, "  %s But updates can't install: %s.\n    %s\n", cliYellow(stdout, "!"), problem, minerOwnFolderHint())
		}
		if source, test := minerUpdateSource(); test {
			fmt.Fprintf(stdout, "  %s Updates come from the test feed %s (URNETWORK_UPDATE_URL), not from releases\n", cliYellow(stdout, "!"), source)
		}
		reportServiceStart(stdout, cliServiceStartWait)
		if fix := minerServiceBindHint(runtime.GOOS, system, os.Geteuid(), executable); fix != "" {
			fmt.Fprintf(stdout, "  %s A user service may not bind TCP port 443, so this miner's extender stays off.\n    To let it bind the port, run: %s\n    (%s)\n", cliYellow(stdout, "!"), fix, minerExtenderDocs)
		}
		fmt.Fprintln(stdout, "\nCheck on it any time: provider status")
		return 0
	case cliOptBool(opts, "uninstall"):
		if !manager.installed() {
			fmt.Fprintf(stdout, "%s isn't installed; nothing to remove.\n", name)
			return 0
		}
		if err := manager.uninstall(stdout); err != nil {
			writeCliError(stderr, "The service wasn't removed: "+err.Error())
			return 1
		}
		fmt.Fprintf(stdout, "  %s Removed %s. Your sign-in and miner identity in %s are kept.\n", cliGreen(stdout, "✓"), name, displayStateDir())
		return 0
	case cliOptBool(opts, "logs"):
		if !manager.installed() {
			writeCliError(stderr, name+" isn't installed. Install it with: provider service install")
			return 1
		}
		follow, _ := opts.Bool("--follow")
		if err := manager.logs(follow); err != nil {
			writeCliError(stderr, err.Error())
			return 1
		}
		return 0
	case cliOptBool(opts, "status"):
		if !manager.installed() {
			fmt.Fprintf(stdout, "%s isn't installed. Install it with: provider service install\n", name)
			return minerExitOffline
		}
		if manager.active() {
			fmt.Fprintf(stdout, "%s is running (%s).\n", name, manager.kind())
			if runAs := serviceRunsAs(manager); runAs != "" {
				fmt.Fprintf(stdout, "It runs as %s and may bind ports below 1024, such as the extender's TCP 443.\n", runAs)
			}
			return 0
		}
		fmt.Fprintf(stdout, "%s is installed but not running. Start it with: provider service start\n", name)
		return minerExitOffline
	}
	for _, action := range []string{"start", "stop", "restart"} {
		if !cliOptBool(opts, action) {
			continue
		}
		if !manager.installed() {
			writeCliError(stderr, name+" isn't installed. Install it with: provider service install")
			return 1
		}
		if action != "stop" && !byInstaller {
			if problem := otherMinerProblem(manager); problem != "" {
				writeCliError(stderr, problem)
				return 1
			}
		}
		if err := manager.control(action); err != nil {
			if serviceRunsAs(manager) != "" && os.Geteuid() != 0 {
				hint := "sudo provider service " + action + " --system"
				if byInstaller {
					hint = "urnet-tools " + action
				} else if executable, err := cliExecutable(); err == nil {
					hint = "sudo " + cliShellQuote(executable) + " service " + action + " --system"
				}
				writeCliError(stderr, fmt.Sprintf("Couldn't %s %s: %s\n  It's a system service. Run: %s", action, name, err, hint))
				return 1
			}
			writeCliError(stderr, fmt.Sprintf("Couldn't %s %s: %s", action, name, err))
			return 1
		}
		switch {
		case action == "stop" && byInstaller:
			fmt.Fprintf(stdout, "  %s Stopped %s. It starts again %s.\n", cliGreen(stdout, "✓"), name, minerServiceNextStart())
		case action == "stop":
			fmt.Fprintf(stdout, "  %s Stopped %s. It starts again %s; provider service uninstall removes it.\n", cliGreen(stdout, "✓"), name, minerServiceNextStart())
		case byInstaller:
			// the installer's miner may be a release that doesn't report in
			fmt.Fprintf(stdout, "  %s %sed %s. Check on it with: provider status\n", cliGreen(stdout, "✓"), strings.ToUpper(action[:1])+action[1:], name)
		default:
			fmt.Fprintf(stdout, "  %s %sed %s\n", cliGreen(stdout, "✓"), strings.ToUpper(action[:1])+action[1:], name)
			reportServiceStart(stdout, cliServiceStartWait)
		}
		return 0
	}
	return 1
}

func cliOptBool(opts docopt.Opts, key string) bool {
	value, _ := opts.Bool(key)
	return value
}

// reportServiceStart waits a little for the service's miner to report in.
func reportServiceStart(stdout io.Writer, wait time.Duration) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if status, err := readMinerLocalStatus(); err == nil && status.running(time.Now()) {
			if status.connected() {
				fmt.Fprintf(stdout, "  %s Miner running and connected\n", cliGreen(stdout, "✓"))
			} else {
				fmt.Fprintf(stdout, "  %s Miner running, still connecting\n", cliGreen(stdout, "✓"))
			}
			return
		}
		time.Sleep(time.Second)
	}
	fmt.Fprintf(stdout, "  %s The miner hasn't reported in yet. Check with: provider status\n", cliYellow(stdout, "!"))
}

// systemd

type systemdService struct {
	system bool
	name   string
	dir    string
	// the user a system service runs the miner as (provider service install
	// --system --as), empty for root; read from the installed unit when not set
	runAs string
}

// The file a system service run as a user (--as) watches: that user's update
// writes it after it replaced the binary, and a root path unit restarts the
// service, which that user may not do itself. In the state folder, where only
// that user writes; it never starts with .provider, so custody never counts it.
const minerServiceRestartFileName = "service-restart.request"

// minerServiceUser is the user a system service runs the miner as.
type minerServiceUser struct {
	name     string
	uid      int
	gid      int
	home     string
	stateDir string
}

// minerServiceUserFor looks up the user of --as: a real user other than root,
// whose state folder is ~/.urnetwork unless URNETWORK_STATE_DIR names one.
func minerServiceUserFor(name string, lookup func(string) (*user.User, error), getenv func(string) string) (*minerServiceUser, error) {
	if !minerServiceUserPattern.MatchString(name) {
		return nil, fmt.Errorf("%q isn't a user name", name)
	}
	found, err := lookup(name)
	if err != nil {
		return nil, fmt.Errorf("There's no user %s on this system", name)
	}
	uid, uidErr := strconv.Atoi(found.Uid)
	gid, gidErr := strconv.Atoi(found.Gid)
	if uidErr != nil || gidErr != nil {
		return nil, fmt.Errorf("%s has no numeric user id", name)
	}
	if uid == 0 {
		return nil, errors.New("--as names the user the miner runs as instead of root. For a service run as root, leave it out: sudo provider service install --system")
	}
	if found.HomeDir == "" || !filepath.IsAbs(found.HomeDir) {
		return nil, fmt.Errorf("%s has no home folder", name)
	}
	stateDir := filepath.Join(found.HomeDir, ".urnetwork")
	if override := strings.TrimSpace(getenv("URNETWORK_STATE_DIR")); override != "" {
		if !filepath.IsAbs(override) {
			return nil, errors.New("URNETWORK_STATE_DIR must be an absolute path for a system service")
		}
		stateDir = filepath.Clean(override)
	}
	return &minerServiceUser{name: found.Username, uid: uid, gid: gid, home: found.HomeDir, stateDir: stateDir}, nil
}

var minerServiceUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_.-]*[$]?$`)

// minerServiceBindHint is what a new service that can't bind tcp 443 says to
// run instead: on Linux a user service may not bind ports below 1024, which
// the extender needs. Empty for a system service and off Linux.
func minerServiceBindHint(goos string, system bool, euid int, executable string) string {
	if system {
		return ""
	}
	username := ""
	if current, err := user.Current(); err == nil {
		username = current.Username
	}
	return minerExtenderBindFixFor(goos, executable, false, username, euid)
}

// ownSystemService is, for a user service manager, the system service of the
// same name that runs the miner as this user (--as): installed, so this user
// mines through it. Nil for any other manager.
func ownSystemService(manager minerServiceManager) *systemdService {
	systemd, ok := manager.(*systemdService)
	if !ok || systemd.system {
		return nil
	}
	current, err := user.Current()
	if err != nil {
		return nil
	}
	own := newSystemdService(true)
	if !own.installed() || own.user() != current.Username {
		return nil
	}
	return own
}

// serviceRunsAs is the user a system service runs the miner as, empty for
// any other service.
func serviceRunsAs(manager minerServiceManager) string {
	if systemd, ok := manager.(*systemdService); ok && systemd.system {
		return systemd.user()
	}
	return ""
}

// takeOverUserService stops and removes the target user's own user service
// before a system service takes over the same miner: two services must never
// run one identity. It acts as that user, through runuser, so root changes
// nothing in the user's folders itself. It answers why it can't, if so.
func takeOverUserService(target *minerServiceUser, out io.Writer) string {
	userDir := filepath.Join(target.home, ".config", "systemd", "user")
	if _, err := os.Stat(filepath.Join(userDir, "urnetwork.service")); err == nil {
		return "The URnetwork installer runs this miner for " + target.name + " (urnetwork.service). Switch it with the installer instead, as " + target.name + ":\n  " + minerExtenderInstallerFix
	}
	name := minerServiceName()
	files := []string{}
	for _, suffix := range []string{".service", "-update.service", "-update.timer"} {
		if _, err := os.Stat(filepath.Join(userDir, name+suffix)); err == nil {
			files = append(files, filepath.Join(userDir, name+suffix))
		}
	}
	if len(files) == 0 {
		return ""
	}
	asUser := []string{"-u", target.name, "--", "env", "XDG_RUNTIME_DIR=/run/user/" + strconv.Itoa(target.uid)}
	_, _ = cliRun("runuser", append(asUser, "systemctl", "--user", "disable", "--now", name+".service", name+"-update.timer")...)
	_, _ = cliRun("runuser", append([]string{"-u", target.name, "--", "rm", "-f", "--"}, files...)...)
	_, _ = cliRun("runuser", append(asUser, "systemctl", "--user", "daemon-reload")...)
	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			return "Couldn't remove " + target.name + "'s user service " + name + ". As " + target.name + ", run: provider service uninstall\n  then run this again."
		}
	}
	fmt.Fprintf(out, "  %s Stopped and removed %s's user service %s\n", cliGreen(out, "✓"), target.name, name)
	return ""
}

// Where system units go. Tests replace it.
var systemdSystemUnitDir = "/etc/systemd/system"

func newSystemdService(system bool) *systemdService {
	dir := systemdSystemUnitDir
	if !system {
		configDir, err := os.UserConfigDir()
		if err != nil {
			home, _ := os.UserHomeDir()
			configDir = filepath.Join(home, ".config")
		}
		dir = filepath.Join(configDir, "systemd", "user")
	}
	return &systemdService{system: system, name: minerServiceName(), dir: dir}
}

func (self *systemdService) kind() string {
	if self.system {
		if runAs := self.user(); runAs != "" {
			return "systemd system service, as " + runAs
		}
		return "systemd system service"
	}
	return "systemd user service"
}

// user is who a system service runs the miner as: the one install was given,
// else the installed unit's User=. Empty for root and for a user service.
func (self *systemdService) user() string {
	if !self.system {
		return ""
	}
	if self.runAs != "" {
		return self.runAs
	}
	raw, err := os.ReadFile(self.unitPath(".service"))
	if err != nil {
		return ""
	}
	return systemdUnitUser(string(raw))
}

// systemdUnitUser is a unit's User=, empty for none or root.
func systemdUnitUser(unit string) string {
	runAs := ""
	for _, line := range strings.Split(unit, "\n") {
		if rest, ok := strings.CutPrefix(line, "User="); ok {
			runAs = systemdUnquote(rest)
		}
	}
	if runAs == "root" || runAs == "0" {
		return ""
	}
	return runAs
}

func (self *systemdService) systemctl(args ...string) (string, error) {
	if !self.system {
		args = append([]string{"--user"}, args...)
	}
	out, err := cliRun("systemctl", args...)
	if err != nil && out != "" {
		return out, fmt.Errorf("%s", firstLine(out))
	}
	return out, err
}

func (self *systemdService) unitPath(suffix string) string {
	return filepath.Join(self.dir, self.name+suffix)
}

func (self *systemdService) installed() bool {
	_, err := os.Stat(self.unitPath(".service"))
	return err == nil
}

func (self *systemdService) active() bool {
	out, _ := self.systemctl("is-active", self.name+".service")
	return out == "active"
}

// installerService is the URnetwork installer's urnetwork.service (on
// Windows its urnetwork.lnk), when the manager found one mining this state
// folder, so the everyday service commands can drive it. A test install with
// its own state folder is a separate miner and gets its own service.
func installerService(manager minerServiceManager) installerMiner {
	if other := platformInstallerService(manager); other != nil {
		return other
	}
	systemd, ok := manager.(*systemdService)
	if !ok {
		return nil
	}
	if systemd.foreign() == "" {
		if !systemd.system {
			// the installer's system service (its --system) runs this user's
			// miner in place of its user service
			if system := newSystemdService(true); system.foreign() != "" {
				installer := *system
				installer.name = "urnetwork"
				if current, err := user.Current(); err == nil && installer.user() == current.Username {
					return installerService(system)
				}
			}
		}
		return nil
	}
	other := *systemd
	other.name = "urnetwork"
	other.runAs = ""
	_, stateDir := other.configured()
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if runAs := other.user(); runAs != "" {
			// the installer's system service (its --system) mines its user's folder
			if found, lookupErr := user.Lookup(runAs); lookupErr == nil {
				home, err = found.HomeDir, nil
			}
		}
		if err != nil {
			return nil
		}
		stateDir = filepath.Join(home, ".urnetwork")
	}
	if ours, err := providerStateDir(); err != nil || filepath.Clean(ours) != filepath.Clean(stateDir) {
		return nil
	}
	return &other
}

func (self *systemdService) label() string { return self.name }

func (self *systemdService) unit() string { return self.name + ".service" }

func (self *systemdService) switchHint() string {
	if self.system {
		return "This device already runs the miner with the URnetwork installer's system service (urnetwork.service).\n  Keep using it: urnet-tools start, stop, status and logs work on it.\n  To switch to this command's service, remove it first (your identity is kept): urnet-tools uninstall"
	}
	return "This device already runs the miner with the URnetwork installer (urnetwork.service).\n  Keep using it: provider service start, stop, status and logs work on it.\n  To switch to this command's service, turn the installer's off first (your identity is kept):\n  systemctl --user disable --now urnetwork.service urnetwork-update.timer\n  Don't use urnet-tools uninstall for this: older installers delete ~/.urnetwork with it."
}

func (self *systemdService) stopHint() string {
	if self.system {
		return "sudo systemctl disable --now urnetwork.service urnetwork-restart.path"
	}
	return "systemctl --user disable --now urnetwork.service urnetwork-update.timer"
}

func (self *systemdService) enabled() bool {
	out, _ := self.systemctl("is-enabled", self.name+".service")
	return out == "enabled"
}

func (self *systemdService) foreign() string {
	if _, err := os.Stat(filepath.Join(self.dir, "urnetwork.service")); err == nil {
		return "urnetwork.service"
	}
	return ""
}

// systemdQuote writes one value for a unit file: % is systemd's specifier
// mark, so it is doubled.
func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	if !strings.ContainsAny(value, " \t\"\\'") {
		return value
	}
	return strconv.Quote(value)
}

// systemdExecQuote is systemdQuote for a command line, where $ also expands.
func systemdExecQuote(value string) string {
	return systemdQuote(strings.ReplaceAll(value, "$", "$$"))
}

func systemdUnquote(value string) string {
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = unquoted
	}
	return strings.ReplaceAll(value, "%%", "%")
}

func (self *systemdService) configured() (string, string) {
	raw, err := os.ReadFile(self.unitPath(".service"))
	if err != nil {
		return "", ""
	}
	executable, stateDir := "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, "ExecStart="); ok {
			rest = strings.TrimSuffix(strings.TrimSpace(rest), " start")
			executable = strings.ReplaceAll(systemdUnquote(rest), "$$", "$")
		}
		if rest, ok := strings.CutPrefix(line, "Environment="); ok {
			if value, ok := strings.CutPrefix(systemdUnquote(rest), "URNETWORK_STATE_DIR="); ok {
				stateDir = value
			}
		}
	}
	return executable, stateDir
}

// systemdUnits are the files provider service install writes on Linux.
func systemdUnits(name string, executable string, stateDir string, system bool) map[string]string {
	return systemdUnitSet(name, executable, stateDir, system, "", "")
}

// systemdUnitSet is systemdUnits, and for a system service run as `runAs`
// (--as) the units that let it bind port 443 and restart after an update:
//   - the miner runs as that user with only CAP_NET_BIND_SERVICE, which the
//     unit grants at every start, so it survives the update that replaces
//     the binary, unlike a setcap file capability;
//   - the update runs as that user too, and replaces the binary as before;
//   - a root path unit restarts the miner when the update writes the restart
//     file, because that user may not restart a system service; root runs
//     only `systemctl`, never the user's binary.
//
// `systemctl` is the absolute path of systemctl, for that restart.
func systemdUnitSet(name string, executable string, stateDir string, system bool, runAs string, systemctl string) map[string]string {
	wantedBy := "default.target"
	if system {
		wantedBy = "multi-user.target"
	}
	environment := fmt.Sprintf("Environment=%s\n", systemdQuote("URNETWORK_STATE_DIR="+stateDir))
	if name != minerServiceDefaultName {
		environment += fmt.Sprintf("Environment=%s\n", systemdQuote("URNETWORK_SERVICE_NAME="+name))
	}
	for _, variable := range minerServiceEnvironment() {
		environment += fmt.Sprintf("Environment=%s\n", systemdQuote(variable))
	}
	header := "# Installed by provider service install. Remove with: provider service uninstall\n"
	documentation := ""
	runAsLines := ""
	privilege := ""
	updateRunAs := ""
	if system && runAs != "" {
		header = fmt.Sprintf("# Installed by provider service install --system --as=%s. Remove with: sudo provider service uninstall --system\n", runAs)
		documentation = "Documentation=" + minerExtenderDocs + "\n"
		runAsLines = fmt.Sprintf("User=%s\n", systemdQuote(runAs))
		privilege = "# the one privilege the extender needs: to listen on ports below 1024 (TCP and UDP 443)\nAmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE\nNoNewPrivileges=yes\n"
		updateRunAs = runAsLines + "NoNewPrivileges=yes\n"
	}
	units := map[string]string{
		name + ".service": fmt.Sprintf(`%s[Unit]
Description=URnetwork miner
%sWants=network-online.target
After=network-online.target

[Service]
%sExecStart=%s start
%s%s# 3 means the device needs setup (provider setup); restarting won't help
Restart=on-failure
RestartSec=15
RestartPreventExitStatus=3

[Install]
WantedBy=%s
`, header, documentation, runAsLines, systemdExecQuote(executable), environment, privilege, wantedBy),
		name + "-update.service": fmt.Sprintf(`# Installed by provider service install. Turn updates off with: provider update --off
[Unit]
Description=URnetwork miner automatic update
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
%sExecStart=%s update --auto
%s`, updateRunAs, systemdExecQuote(executable), environment),
		name + "-update.timer": `# Installed by provider service install
[Unit]
Description=Check for URnetwork miner updates every 6 hours

[Timer]
OnBootSec=15min
OnUnitActiveSec=6h
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
`,
	}
	if system && runAs != "" {
		units[name+"-restart.path"] = fmt.Sprintf(`# Installed by provider service install --system --as=%s
[Unit]
Description=Restart the URnetwork miner when its update asks

[Path]
PathChanged=%s

[Install]
WantedBy=multi-user.target
`, runAs, systemdQuote(filepath.Join(stateDir, minerServiceRestartFileName)))
		units[name+"-restart.service"] = fmt.Sprintf(`# Installed by provider service install --system --as=%s
[Unit]
Description=Restart the URnetwork miner after an update

[Service]
Type=oneshot
ExecStart=%s try-restart %s
`, runAs, systemdExecQuote(systemctl), name+".service")
	}
	return units
}

// systemdRestartUnitSuffixes are the units only a system service run as a
// user has.
var systemdRestartUnitSuffixes = []string{"-restart.path", "-restart.service"}

func (self *systemdService) install(executable string, stateDir string, out io.Writer) error {
	if err := os.MkdirAll(self.dir, 0755); err != nil {
		return err
	}
	runAs := ""
	if self.system {
		runAs = self.runAs
	}
	systemctl := ""
	if runAs != "" {
		// that user's folder exists and is theirs (serviceCmd checked); root
		// makes nothing in it
		path, err := exec.LookPath("systemctl")
		if err != nil {
			return err
		}
		if systemctl, err = filepath.Abs(path); err != nil {
			return err
		}
		// the restart file exists before its path unit watches it, so the
		// first update's write is seen; made by that user, as all of their files
		if _, err := cliRun("runuser", "-u", runAs, "--", "touch", "--", filepath.Join(stateDir, minerServiceRestartFileName)); err != nil {
			return fmt.Errorf("couldn't make the restart file as %s: %w", runAs, err)
		}
	} else if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	if self.system && runAs == "" {
		// a service that ran as a user before leaves its restart units behind
		self.removeRestartUnits()
	}
	// a running service keeps the old unit until it restarts, and a switch
	// to another user must not keep running as the last one
	wasActive := runAs != "" && self.installed() && self.active()
	units := systemdUnitSet(self.name, executable, stateDir, self.system, runAs, systemctl)
	removeUnits := func() {
		for file := range units {
			os.Remove(filepath.Join(self.dir, file))
		}
		_, _ = self.systemctl("daemon-reload")
	}
	for file, text := range units {
		if err := os.WriteFile(filepath.Join(self.dir, file), []byte(text), 0644); err != nil {
			removeUnits()
			return err
		}
	}
	if _, err := self.systemctl("daemon-reload"); err != nil {
		removeUnits()
		if strings.Contains(err.Error(), "bus") && !self.system {
			sudo := "sudo "
			if os.Geteuid() == 0 {
				sudo = ""
			}
			return fmt.Errorf("%s.\n  This login has no systemd user session (common over su, some SSH setups and containers).\n  Use a system service instead: %sprovider service install --system", err, sudo)
		}
		return err
	}
	enable := []string{"enable", "--now", self.name + ".service", self.name + "-update.timer"}
	if runAs != "" {
		enable = append(enable, self.name+"-restart.path")
	}
	if _, err := self.systemctl(enable...); err != nil {
		removeUnits()
		return err
	}
	if wasActive {
		if _, err := self.systemctl("restart", self.name+".service"); err != nil {
			return err
		}
	}
	if !self.system {
		// without lingering, a user service stops at logout and waits for a login after boot
		if current, err := user.Current(); err == nil {
			if _, err := cliRun("loginctl", "enable-linger", current.Username); err != nil {
				fmt.Fprintf(out, "  %s Couldn't keep the service running after you log out (loginctl enable-linger failed).\n    Run: sudo loginctl enable-linger %s\n", cliYellow(out, "!"), current.Username)
			}
		}
	}
	return nil
}

func (self *systemdService) uninstall(out io.Writer) error {
	_, _ = self.systemctl("disable", "--now", self.name+".service", self.name+"-update.timer")
	for _, suffix := range []string{".service", "-update.service", "-update.timer"} {
		if err := os.Remove(self.unitPath(suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if self.system {
		if err := self.removeRestartUnits(); err != nil {
			return err
		}
	}
	_, err := self.systemctl("daemon-reload")
	return err
}

// removeRestartUnits stops and removes the restart units of a system service
// run as a user, if there are any.
func (self *systemdService) removeRestartUnits() error {
	if _, err := os.Stat(self.unitPath("-restart.path")); err == nil {
		_, _ = self.systemctl("disable", "--now", self.name+"-restart.path")
	}
	for _, suffix := range systemdRestartUnitSuffixes {
		if err := os.Remove(self.unitPath(suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (self *systemdService) control(action string) error {
	if action == "restart" && os.Geteuid() != 0 && self.user() != "" {
		// the user a system service runs as may not restart it (the update
		// does this): writing the restart file has root's path unit restart it
		_, stateDir := self.configured()
		if stateDir == "" {
			return errors.New("the service names no state folder to ask for its restart in")
		}
		return os.WriteFile(filepath.Join(stateDir, minerServiceRestartFileName), []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0600)
	}
	_, err := self.systemctl(action, self.name+".service")
	return err
}

func (self *systemdService) logs(follow bool) error {
	args := []string{"-u", self.name + ".service", "-u", self.name + "-update.service", "-n", "100", "--no-pager"}
	if !self.system {
		args = append([]string{"--user"}, args...)
	}
	if follow {
		args = append(args, "-f")
	}
	return cliRunAttached("journalctl", args...)
}

// launchd

type launchdService struct {
	system bool
	label  string
	dir    string
}

func newLaunchdService(system bool) *launchdService {
	dir := "/Library/LaunchDaemons"
	if !system {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "LaunchAgents")
	}
	return &launchdService{system: system, label: minerLaunchdName(), dir: dir}
}

func (self *launchdService) kind() string {
	if self.system {
		return "launchd daemon"
	}
	return "launchd agent"
}

func (self *launchdService) domain() string {
	if self.system {
		return "system"
	}
	return "gui/" + strconv.Itoa(os.Getuid())
}

func (self *launchdService) plistPath(label string) string {
	return filepath.Join(self.dir, label+".plist")
}

func (self *launchdService) installed() bool {
	_, err := os.Stat(self.plistPath(self.label))
	return err == nil
}

func (self *launchdService) active() bool {
	out, err := cliRun("launchctl", "print", self.domain()+"/"+self.label)
	return err == nil && strings.Contains(out, "state = running")
}

func (self *launchdService) foreign() string { return "" }

var launchdProgramPattern = regexp.MustCompile(`<key>ProgramArguments</key>\s*<array>\s*<string>([^<]*)</string>`)
var launchdStatePattern = regexp.MustCompile(`<key>URNETWORK_STATE_DIR</key>\s*<string>([^<]*)</string>`)

func plistUnescape(value string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(value)
}

func (self *launchdService) configured() (string, string) {
	raw, err := os.ReadFile(self.plistPath(self.label))
	if err != nil {
		return "", ""
	}
	executable, stateDir := "", ""
	if match := launchdProgramPattern.FindSubmatch(raw); match != nil {
		executable = plistUnescape(string(match[1]))
	}
	if match := launchdStatePattern.FindSubmatch(raw); match != nil {
		stateDir = plistUnescape(string(match[1]))
	}
	return executable, stateDir
}

func plistEscape(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// launchdPlists are the files provider service install writes on macOS.
func launchdPlists(label string, executable string, stateDir string, serviceName string) map[string]string {
	environment := fmt.Sprintf("\t\t<key>URNETWORK_STATE_DIR</key>\n\t\t<string>%s</string>\n", plistEscape(stateDir))
	if serviceName != minerServiceDefaultName {
		environment += fmt.Sprintf("\t\t<key>URNETWORK_SERVICE_NAME</key>\n\t\t<string>%s</string>\n", plistEscape(serviceName))
	}
	for _, variable := range minerServiceEnvironment() {
		key, value, _ := strings.Cut(variable, "=")
		environment += fmt.Sprintf("\t\t<key>%s</key>\n\t\t<string>%s</string>\n", key, plistEscape(value))
	}
	logPath := plistEscape(filepath.Join(stateDir, "service.log"))
	return map[string]string{
		label: fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Installed by provider service install. Remove with: provider service uninstall -->
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>start</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
%s	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, plistEscape(label), plistEscape(executable), environment, logPath, logPath),
		label + ".update": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Installed by provider service install. provider update with the off option turns updates off. -->
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s.update</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>update</string>
		<string>--auto</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
%s	</dict>
	<key>StartInterval</key>
	<integer>21600</integer>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, plistEscape(label), plistEscape(executable), environment, logPath, logPath),
	}
}

func (self *launchdService) install(executable string, stateDir string, out io.Writer) error {
	if err := os.MkdirAll(self.dir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	for label, text := range launchdPlists(self.label, executable, stateDir, minerServiceName()) {
		path := self.plistPath(label)
		// a job that is already loaded must be unloaded to take the new file
		_, _ = cliRun("launchctl", "bootout", self.domain()+"/"+label)
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			return err
		}
		var out string
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			if out, err = cliRun("launchctl", "bootstrap", self.domain(), path); err == nil {
				break
			}
			// "Input/output error" while the old job is still being torn down
			time.Sleep(time.Second)
		}
		if err != nil {
			_ = self.uninstall(io.Discard)
			if out != "" {
				return fmt.Errorf("%s", firstLine(out))
			}
			return err
		}
	}
	return nil
}

func (self *launchdService) uninstall(out io.Writer) error {
	for _, label := range []string{self.label, self.label + ".update"} {
		_, _ = cliRun("launchctl", "bootout", self.domain()+"/"+label)
		if err := os.Remove(self.plistPath(label)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (self *launchdService) control(action string) error {
	target := self.domain() + "/" + self.label
	var err error
	switch action {
	case "start":
		_, err = cliRun("launchctl", "kickstart", target)
	case "restart":
		_, err = cliRun("launchctl", "kickstart", "-k", target)
	case "stop":
		_, err = cliRun("launchctl", "kill", "SIGTERM", target)
	}
	return err
}

func (self *launchdService) logs(follow bool) error {
	stateDir, err := minerServiceStateDir(self.system)
	if err != nil {
		return err
	}
	args := []string{"-n", "100"}
	if follow {
		args = append(args, "-f")
	}
	return cliRunAttached("tail", append(args, filepath.Join(stateDir, "service.log"))...)
}

// minerServiceState is what provider status says about the service.
type minerServiceState struct {
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Foreign   string `json:"other,omitempty"`
}

func readMinerServiceState() minerServiceState {
	for _, system := range []bool{false, true} {
		manager, err := minerServiceFor(system)
		if err != nil {
			return minerServiceState{}
		}
		if manager.installed() {
			return minerServiceState{Installed: true, Active: manager.active(), Name: minerServiceName(), Kind: manager.kind()}
		}
		if other := installerService(manager); other != nil {
			return minerServiceState{Foreign: other.unit(), Active: other.active()}
		}
	}
	return minerServiceState{}
}
