package miner

// Whether the state directory outlives this miner's container, or the
// machine's memory. The state directory is the device's identity: its
// provider key, the key's identity marker and its client credentials
// (PROVIDER-REGISTRATION.md). A start on an empty directory registers a new
// device, with a new provider client, a new key and a new extender key, and
// the old device's history, reliability and earnings standing stay behind. A
// directory lost with its container makes every new container such a start.
//
// The check reads only local facts. /.dockerenv, /run/.containerenv, the
// Kubernetes service environment and /proc/1/cgroup say whether this is an
// application container (Docker, Podman, Kubernetes), and
// /proc/self/mountinfo says which mount holds the state directory:
//   - In an application container the root filesystem belongs to the
//     container. A directory on it lasts as long as the container, and a new
//     container (an image update, docker compose up, docker run --rm) starts
//     empty. A volume or bind mount is a mount of its own.
//   - A tmpfs or ramfs, in a container or not, lasts until it is unmounted:
//     the container's stop, or the next reboot.
//   - A Docker or Podman anonymous volume (a volume named by 64 hex digits)
//     is kept by compose across recreation but made anew by each docker run.
//
// Systems that keep their root filesystem (LXC, systemd-nspawn, a virtual
// machine) are no application containers, and nothing is reported for them.
// Nothing here can tell a Kubernetes emptyDir from a persistent volume: both
// are mounts.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Why the state directory is lost, or "" while it is kept.
const (
	// it is in an application container's own filesystem
	minerStateLostContainer = "container_filesystem"
	// it is on a tmpfs or ramfs
	minerStateLostMemory = "memory"
	// it is on a Docker or Podman anonymous volume
	minerStateLostAnonymousVolume = "anonymous_volume"
)

// The application containers the check knows.
const (
	minerContainerDocker     = "docker"
	minerContainerPodman     = "podman"
	minerContainerKubernetes = "kubernetes"
	// a container runtime only the cgroup names (containerd, CRI-O)
	minerContainerOther = "container"
)

// The community provider image and the path of its miner (build
// all/community/provider/Dockerfile). A fix names the image when this miner is
// that binary, and <image> otherwise.
const (
	minerCommunityProviderImage  = "bringyour/community-provider:latest"
	minerCommunityProviderBinary = "/usr/local/libexec/bringyour-provider/provider"
)

// The volume the fixes create for the state directory.
const minerStateVolumeName = "urnetwork-provider"

// The environment the community image's wrapper sets when it runs the miner
// on a private state directory inside the container, because the state
// directory it was given can't keep a device (build
// all/community/provider/bringyour-provider.sh): that directory, and why. The
// miner only explains with them; it never chooses a directory by them.
const (
	minerStateSubstitutedDirEnv     = "BRINGYOUR_PROVIDER_STATE_DIR"
	minerStateSubstitutedProblemEnv = "BRINGYOUR_PROVIDER_STATE_PROBLEM"
)

// A Docker or Podman anonymous volume's directory, as a mount's root names it:
// <storage>/volumes/<64 hex digits>/_data.
var minerAnonymousVolumePattern = regexp.MustCompile(`(^|/)volumes/[0-9a-f]{64}/_data$`)

// The root provide, status and doctor read the host's facts below: "/", or a
// test's directory laid out the same way.
var minerHostRoot = "/"

// The facts of this host the check reads.
type minerHostFacts struct {
	// one of minerContainer*, or "" outside an application container
	container string
	// /proc/self/mountinfo, nil where there is none (off Linux)
	mountInfo []byte
}

// Reads the facts below root, which is "/" outside tests,
// with getenv for the environment.
func readMinerHostFacts(root string, getenv func(string) string) minerHostFacts {
	mountInfo, err := os.ReadFile(filepath.Join(root, "proc", "self", "mountinfo"))
	if err != nil {
		mountInfo = nil
	}
	return minerHostFacts{container: minerContainerKind(root, getenv), mountInfo: mountInfo}
}

// Names the application container this process runs in,
// or "" for none. Kubernetes comes first: its pods can carry another runtime's
// marker too.
func minerContainerKind(root string, getenv func(string) string) string {
	exists := func(path string) bool {
		_, err := os.Lstat(filepath.Join(root, path))
		return err == nil
	}
	switch {
	case getenv("KUBERNETES_SERVICE_HOST") != "":
		return minerContainerKubernetes
	case exists(".dockerenv"):
		return minerContainerDocker
	case exists(filepath.Join("run", ".containerenv")):
		return minerContainerPodman
	}
	// cgroup v1, and v2 outside a cgroup namespace, name the container's
	// cgroup; a namespaced v2 says only "0::/"
	cgroup, err := os.ReadFile(filepath.Join(root, "proc", "1", "cgroup"))
	if err != nil {
		return ""
	}
	switch {
	case bytes.Contains(cgroup, []byte("kubepods")):
		return minerContainerKubernetes
	case bytes.Contains(cgroup, []byte("docker")):
		return minerContainerDocker
	case bytes.Contains(cgroup, []byte("libpod")):
		return minerContainerPodman
	case bytes.Contains(cgroup, []byte("containerd")), bytes.Contains(cgroup, []byte("crio")):
		return minerContainerOther
	}
	return ""
}

// One mount of /proc/self/mountinfo.
type minerMount struct {
	// the directory of its filesystem the mount shows
	root       string
	mountPoint string
	fsType     string
}

// Reads /proc/self/mountinfo (proc(5)). Each line is the
// mount id, the parent id, major:minor, the root, the mount point, the mount
// options and optional fields up to a lone "-", then the filesystem type, the
// source and the superblock options. Paths escape space, tab, newline and
// backslash in octal (\040). A line of another shape is skipped.
func parseMinerMountInfo(raw []byte) []minerMount {
	mounts := []minerMount{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || len(fields) <= separator+1 {
			continue
		}
		mounts = append(mounts, minerMount{
			root:       unescapeMinerMountPath(fields[3]),
			mountPoint: unescapeMinerMountPath(fields[4]),
			fsType:     fields[separator+1],
		})
	}
	return mounts
}

// The path with mountinfo's octal escapes (\040) undone.
func unescapeMinerMountPath(path string) string {
	if !strings.Contains(path, `\`) {
		return path
	}
	var unescaped strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' && i+3 < len(path) {
			if value, err := strconv.ParseUint(path[i+1:i+4], 8, 8); err == nil {
				unescaped.WriteByte(byte(value))
				i += 3
				continue
			}
		}
		unescaped.WriteByte(path[i])
	}
	return unescaped.String()
}

// The mount that holds path: of the mounts at path or one of
// its parents, the deepest, and of those at one point the last listed, which
// hides the earlier.
func minerMountOf(mounts []minerMount, path string) (minerMount, bool) {
	best := -1
	for i, mount := range mounts {
		within := mount.mountPoint == "/" || path == mount.mountPoint || strings.HasPrefix(path, strings.TrimSuffix(mount.mountPoint, "/")+"/")
		if !within {
			continue
		}
		if best < 0 || len(mounts[best].mountPoint) <= len(mount.mountPoint) {
			best = i
		}
	}
	if best < 0 {
		return minerMount{}, false
	}
	return mounts[best], true
}

// The path with the symbolic links of its deepest existing
// parent resolved, as mountinfo names it. A directory not made yet resolves
// through the parent that will hold it.
func minerResolvedPath(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Join(path, rest)
		}
		rest = filepath.Join(filepath.Base(path), rest)
		path = parent
	}
}

// Whether a state directory outlives its container or the machine's memory.
type minerStatePersistence struct {
	// the state directory as configured
	dir string
	// one of minerContainer*, or ""
	container string
	// the mount holding dir, when the host lists its mounts
	mount   minerMount
	mounted bool
	// one of minerStateLost*, or "" while the directory is kept or nothing
	// says it isn't
	lost string
}

// Decides from facts whether dir is lost with its
// container or the machine's memory.
func assessMinerStatePersistence(facts minerHostFacts, dir string) minerStatePersistence {
	persistence := minerStatePersistence{dir: filepath.Clean(dir), container: facts.container}
	persistence.mount, persistence.mounted = minerMountOf(parseMinerMountInfo(facts.mountInfo), minerResolvedPath(dir))
	if !persistence.mounted {
		return persistence
	}
	switch {
	case persistence.mount.fsType == "tmpfs" || persistence.mount.fsType == "ramfs":
		persistence.lost = minerStateLostMemory
	case facts.container == "":
	case persistence.mount.mountPoint == "/":
		persistence.lost = minerStateLostContainer
	case minerAnonymousVolumePattern.MatchString(persistence.mount.root):
		persistence.lost = minerStateLostAnonymousVolume
	}
	return persistence
}

// The state directory the community image's wrapper was given and could not
// use, and why, when it runs the miner on a private one instead.
type minerStateSubstitution struct {
	dir     string
	problem string
}

// Reads the wrapper's substitution from getenv,
// nil when it made none, or when it names stateDir itself.
func readMinerStateSubstitution(getenv func(string) string, stateDir string) *minerStateSubstitution {
	dir := strings.TrimSpace(getenv(minerStateSubstitutedDirEnv))
	if dir == "" || filepath.Clean(dir) == filepath.Clean(stateDir) {
		return nil
	}
	return &minerStateSubstitution{dir: filepath.Clean(dir), problem: strings.TrimSpace(getenv(minerStateSubstitutedProblemEnv))}
}

// The image to name in a fix for the miner at
// executable.
func minerContainerImage(executable string) string {
	if executable == minerCommunityProviderBinary {
		return minerCommunityProviderImage
	}
	return "<image>"
}

// How a person runs provider doctor on the miner at
// executable: in the community image, through its wrapper, the image's
// command on the PATH, which runs doctor on the state directory the miner
// uses.
func minerDoctorCommand(executable string) string {
	if executable == minerCommunityProviderBinary {
		return "bringyour-provider doctor"
	}
	return "provider doctor"
}

// The finding for a state directory that is
// lost, nil for one that is kept. created says this start registered a new
// device; substitution is the wrapper's, or nil; image names the container's
// image in the fix.
func minerStatePersistenceFinding(persistence minerStatePersistence, substitution *minerStateSubstitution, created bool, image string, now time.Time) *minerFinding {
	if persistence.lost == "" {
		return nil
	}
	// the directory the operator mounts: the one the wrapper was given, else
	// the state directory itself
	mountDir := persistence.dir
	if substitution != nil {
		mountDir = substitution.dir
	}
	registered := ""
	if created {
		registered = "This start registered a new device. "
	}
	standing := "and this device's history, reliability and earnings standing stay behind."
	finding := &minerFinding{Code: minerFindingStateNotPersistent, FoundAt: now.UTC()}
	switch {
	case persistence.lost == minerStateLostMemory && persistence.container == "":
		finding.Title = "This device's identity is lost at every reboot"
		finding.Summary = fmt.Sprintf("The state directory %s is on a %s, in memory, so it is gone after the next reboot. %sThe start after that registers another new device, %s", persistence.dir, persistence.mount.fsType, registered, standing)
		finding.Fix = "Keep the state on a disk: set URNETWORK_STATE_DIR to a directory on a disk (the default is ~/.urnetwork), then sign in there once:\n" +
			"provider login <auth_code>"
		return finding
	case persistence.lost == minerStateLostMemory:
		finding.Title = "This device's identity is lost when its container stops"
		finding.Summary = fmt.Sprintf("The state directory %s is on a %s, in memory, so it is gone once this container stops. %sThe next start registers another new device, %s", persistence.dir, persistence.mount.fsType, registered, standing)
	case persistence.lost == minerStateLostAnonymousVolume:
		finding.Title = "This device's identity is lost with its container"
		finding.Summary = fmt.Sprintf("The state directory %s is on an anonymous volume, which docker run makes anew for every container (docker run --rm removes it). %sA new container registers another new device, %s", persistence.dir, registered, standing)
		finding.Fix = minerStateVolumeFix(persistence.container, mountDir, image, "Name the volume")
		return finding
	case substitution != nil:
		problem := ""
		if substitution.problem != "" {
			problem = " (" + substitution.problem + ")"
		}
		finding.Title = "This device's identity is lost with its container"
		finding.Summary = fmt.Sprintf("The state directory %s can't keep this device's identity%s, so the miner keeps it in %s, in this container's own filesystem. %sWhen this container is removed or recreated (an image update, docker compose up, docker run --rm), the next start registers another new device, %s", substitution.dir, problem, persistence.dir, registered, standing)
		finding.Fix = minerStateVolumeFix(persistence.container, mountDir, image, "Mount a named volume there instead") + "\n" +
			fmt.Sprintf("Or keep the host directory you mount at %s: make it owned by root and writable only by its owner (sudo chown -R 0:0 <host directory>; sudo chmod 700 <host directory>), one for each container.", mountDir)
		return finding
	default:
		finding.Title = "This device's identity is lost with its container"
		finding.Summary = fmt.Sprintf("The state directory %s is in this container's own filesystem: no volume is mounted there. %sWhen this container is removed or recreated (an image update, docker compose up, docker run --rm), the next start registers another new device, %s", persistence.dir, registered, standing)
	}
	finding.Fix = minerStateVolumeFix(persistence.container, mountDir, image, "Keep the state directory on a named volume")
	return finding
}

// The commands that put dir on a named volume in container, led by lead.
func minerStateVolumeFix(container string, dir string, image string, lead string) string {
	mount := minerStateVolumeName + ":" + cliShellQuote(dir)
	if container == minerContainerKubernetes {
		return lead + ": mount a PersistentVolumeClaim at " + dir + ", one for each pod (a StatefulSet's volumeClaimTemplates); an emptyDir is lost with its pod.\n" +
			"Sign in once into the volume: kubectl exec <pod> -- provider auth <auth_code>"
	}
	tool := "docker"
	if container == minerContainerPodman {
		tool = "podman"
	}
	doctor := "provider doctor"
	if image == minerCommunityProviderImage {
		doctor = minerDoctorCommand(minerCommunityProviderBinary)
	}
	return lead + ", one volume for each node, and sign in there once:\n" +
		tool + " volume create " + minerStateVolumeName + "\n" +
		tool + " run --rm -it -v " + mount + " " + image + " auth <auth_code>\n" +
		tool + " run -d --restart always -v " + mount + " " + image + " provide\n" +
		"In compose.yaml, the service's volumes: [\"" + minerStateVolumeName + ":" + dir + "\"], and the top-level volumes: {" + minerStateVolumeName + ": {external: true}}, the volume signed in above.\n" +
		"Check it with: " + tool + " exec <container> " + doctor
}
