package miner

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The 64 hex digits Docker names an anonymous volume by; synthetic.
const minerTestAnonymousVolume = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeMinerHost lays out a host below a new directory: each file's path is
// relative to the root, and its text the file's content.
func fakeMinerHost(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A mountinfo line (proc(5)) for a mount of root at mountPoint.
func minerTestMountLine(id int, root string, mountPoint string, fsType string) string {
	escape := func(path string) string {
		return strings.NewReplacer(`\`, `\134`, " ", `\040`, "\t", `\011`, "\n", `\012`).Replace(path)
	}
	return strings.Join([]string{
		strconv.Itoa(id), "1", "0:42", escape(root), escape(mountPoint),
		"rw,relatime", "shared:7", "master:1", "-", fsType, "synthetic-source", "rw",
	}, " ")
}

// The mount table of a Docker container: its overlay root, the hosts file
// Docker binds in, and any more lines.
func minerTestContainerMounts(more ...string) string {
	lines := []string{
		minerTestMountLine(101, "/", "/", "overlay"),
		minerTestMountLine(102, "/var/lib/docker/containers/synthetic/hosts", "/etc/hosts", "ext4"),
		minerTestMountLine(103, "/", "/dev/shm", "tmpfs"),
	}
	return strings.Join(append(lines, more...), "\n") + "\n"
}

// A state directory that does not exist yet, below a test's directory.
func minerTestStateDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "urnetwork state")
}

func noMinerTestEnv(string) string { return "" }

// mountinfo's fields, its optional fields and its octal escapes are read as
// proc(5) lays them out; a line of another shape is skipped.
func TestMinerMountInfoReadsFieldsAndEscapes(t *testing.T) {
	raw := minerTestMountLine(101, "/", "/", "overlay") + "\n" +
		minerTestMountLine(102, "/volumes/name with space/_data", "/srv/state dir", "ext4") + "\n" +
		"malformed line without a separator\n" +
		"103 1 0:42 / /short rw - \n"
	mounts := parseMinerMountInfo([]byte(raw))
	if len(mounts) != 2 {
		t.Fatalf("mounts: %+v", mounts)
	}
	if mounts[1].root != "/volumes/name with space/_data" || mounts[1].mountPoint != "/srv/state dir" || mounts[1].fsType != "ext4" {
		t.Fatalf("escaped mount read as %+v", mounts[1])
	}
	if mount, ok := minerMountOf(mounts, "/srv/state dir/operators/a.example"); !ok || mount.mountPoint != "/srv/state dir" {
		t.Fatalf("the deepest mount holding a path: %+v", mount)
	}
	if mount, ok := minerMountOf(mounts, "/srv/state directory"); !ok || mount.mountPoint != "/" {
		t.Fatalf("a mount point is a whole path component, not a prefix: %+v", mount)
	}
	// a later mount at one point hides the earlier
	over := append(mounts, minerMount{root: "/", mountPoint: "/srv/state dir", fsType: "tmpfs"})
	if mount, _ := minerMountOf(over, "/srv/state dir"); mount.fsType != "tmpfs" {
		t.Fatalf("the later mount at one point: %+v", mount)
	}
}

// The root cause of a fleet whose nodes came back as new devices: a state
// directory in a Docker container's own filesystem, with no volume mounted
// there. The check reads it from /.dockerenv and the container's mount table
// and names the exact volume that keeps it.
func TestMinerStateInContainerFilesystemIsLost(t *testing.T) {
	dir := minerTestStateDir(t)
	root := fakeMinerHost(t, map[string]string{
		".dockerenv":          "",
		"proc/self/mountinfo": minerTestContainerMounts(),
	})
	facts := readMinerHostFacts(root, noMinerTestEnv)
	if facts.container != minerContainerDocker {
		t.Fatalf("container %q", facts.container)
	}
	persistence := assessMinerStatePersistence(facts, dir)
	if persistence.lost != minerStateLostContainer || persistence.mount.mountPoint != "/" {
		t.Fatalf("a state directory in the container's filesystem: %+v", persistence)
	}
	finding := minerStatePersistenceFinding(persistence, nil, true, minerCommunityProviderImage, time.Now())
	if finding == nil || finding.Code != minerFindingStateNotPersistent {
		t.Fatalf("finding %+v", finding)
	}
	for _, want := range []string{dir, "no volume is mounted there", "This start registered a new device", "registers another new device"} {
		if !strings.Contains(finding.Summary, want) {
			t.Errorf("summary lacks %q: %s", want, finding.Summary)
		}
	}
	mount := "urnetwork-provider:'" + dir + "'"
	for _, want := range []string{
		"docker volume create urnetwork-provider",
		"docker run --rm -it -v " + mount + " " + minerCommunityProviderImage + " auth <auth_code>",
		"docker run -d --restart always -v " + mount + " " + minerCommunityProviderImage + " provide",
		"urnetwork-provider:" + dir,
		"docker exec <container> bringyour-provider doctor",
	} {
		if !strings.Contains(finding.Fix, want) {
			t.Errorf("fix lacks %q:\n%s", want, finding.Fix)
		}
	}
	// a restart of the same container keeps its identity, but a new container
	// still won't
	again := minerStatePersistenceFinding(persistence, nil, false, minerCommunityProviderImage, time.Now())
	if again == nil || strings.Contains(again.Summary, "registered a new device") {
		t.Fatalf("a kept key in a container's filesystem: %+v", again)
	}
}

// A named volume, or a bind mount, at the state directory or above it
// outlives the container: nothing is reported.
func TestMinerStateOnVolumeIsKept(t *testing.T) {
	dir := minerTestStateDir(t)
	for _, mountPoint := range []string{minerResolvedPath(dir), filepath.Dir(minerResolvedPath(dir))} {
		root := fakeMinerHost(t, map[string]string{
			".dockerenv":          "",
			"proc/self/mountinfo": minerTestContainerMounts(minerTestMountLine(104, "/var/lib/docker/volumes/urnetwork-provider/_data", mountPoint, "ext4")),
		})
		persistence := assessMinerStatePersistence(readMinerHostFacts(root, noMinerTestEnv), dir)
		if persistence.lost != "" || persistence.mount.mountPoint != mountPoint {
			t.Fatalf("a state directory on a volume at %s: %+v", mountPoint, persistence)
		}
		if finding := minerStatePersistenceFinding(persistence, nil, true, minerCommunityProviderImage, time.Now()); finding != nil {
			t.Fatalf("a kept state directory reported %+v", finding)
		}
	}
}

// docker run makes a new anonymous volume for each container, so a state
// directory on one is lost with its container too; the fix names it.
func TestMinerStateOnAnonymousVolumeIsLost(t *testing.T) {
	dir := minerTestStateDir(t)
	root := fakeMinerHost(t, map[string]string{
		".dockerenv":          "",
		"proc/self/mountinfo": minerTestContainerMounts(minerTestMountLine(104, "/var/lib/docker/volumes/"+minerTestAnonymousVolume+"/_data", minerResolvedPath(dir), "ext4")),
	})
	persistence := assessMinerStatePersistence(readMinerHostFacts(root, noMinerTestEnv), dir)
	if persistence.lost != minerStateLostAnonymousVolume {
		t.Fatalf("a state directory on an anonymous volume: %+v", persistence)
	}
	finding := minerStatePersistenceFinding(persistence, nil, false, "<image>", time.Now())
	if finding == nil || !strings.Contains(finding.Summary, "anonymous volume") || !strings.HasPrefix(finding.Fix, "Name the volume") || !strings.Contains(finding.Fix, "<image> provide") {
		t.Fatalf("finding %+v", finding)
	}
}

// Off containers only memory loses the state directory: a tmpfs is reported,
// with a fix that keeps the state on a disk, and a disk never is.
func TestMinerStateOutsideContainersIsLostOnlyInMemory(t *testing.T) {
	dir := minerTestStateDir(t)
	disk := fakeMinerHost(t, map[string]string{
		"proc/self/mountinfo": minerTestMountLine(101, "/", "/", "ext4") + "\n",
	})
	if persistence := assessMinerStatePersistence(readMinerHostFacts(disk, noMinerTestEnv), dir); persistence.container != "" || persistence.lost != "" {
		t.Fatalf("a state directory on a host's disk: %+v", persistence)
	}
	memory := fakeMinerHost(t, map[string]string{
		"proc/self/mountinfo": minerTestMountLine(101, "/", "/", "ext4") + "\n" + minerTestMountLine(102, "/", filepath.Dir(minerResolvedPath(dir)), "tmpfs") + "\n",
	})
	persistence := assessMinerStatePersistence(readMinerHostFacts(memory, noMinerTestEnv), dir)
	if persistence.lost != minerStateLostMemory {
		t.Fatalf("a state directory on a tmpfs: %+v", persistence)
	}
	finding := minerStatePersistenceFinding(persistence, nil, true, "<image>", time.Now())
	if finding == nil || !strings.Contains(finding.Title, "every reboot") || !strings.Contains(finding.Fix, "URNETWORK_STATE_DIR") || strings.Contains(finding.Fix, "docker") {
		t.Fatalf("finding %+v", finding)
	}
	// no mount table at all (macOS, Windows): nothing can be said
	if persistence := assessMinerStatePersistence(readMinerHostFacts(t.TempDir(), noMinerTestEnv), dir); persistence.mounted || persistence.lost != "" {
		t.Fatalf("a host without a mount table: %+v", persistence)
	}
}

// The container runtimes, by their markers and their cgroups; Kubernetes
// first, since its pods may carry another runtime's marker too.
func TestMinerContainerKindReadsMarkersAndCgroup(t *testing.T) {
	kubernetes := func(name string) string {
		if name == "KUBERNETES_SERVICE_HOST" {
			return "198.51.100.1"
		}
		return ""
	}
	for _, c := range []struct {
		files  map[string]string
		getenv func(string) string
		want   string
	}{
		{files: map[string]string{".dockerenv": ""}, getenv: kubernetes, want: minerContainerKubernetes},
		{files: map[string]string{".dockerenv": ""}, getenv: noMinerTestEnv, want: minerContainerDocker},
		{files: map[string]string{"run/.containerenv": ""}, getenv: noMinerTestEnv, want: minerContainerPodman},
		{files: map[string]string{"proc/1/cgroup": "12:memory:/docker/" + minerTestAnonymousVolume + "\n"}, getenv: noMinerTestEnv, want: minerContainerDocker},
		{files: map[string]string{"proc/1/cgroup": "0::/kubepods.slice/kubepods-burstable.slice/synthetic.scope\n"}, getenv: noMinerTestEnv, want: minerContainerKubernetes},
		{files: map[string]string{"proc/1/cgroup": "0::/machine.slice/libpod-synthetic.scope\n"}, getenv: noMinerTestEnv, want: minerContainerPodman},
		{files: map[string]string{"proc/1/cgroup": "0::/system.slice/containerd.service\n"}, getenv: noMinerTestEnv, want: minerContainerOther},
		// a host's own init, a namespaced cgroup, and no files at all
		{files: map[string]string{"proc/1/cgroup": "0::/init.scope\n"}, getenv: noMinerTestEnv, want: ""},
		{files: map[string]string{"proc/1/cgroup": "0::/\n"}, getenv: noMinerTestEnv, want: ""},
		{files: map[string]string{}, getenv: noMinerTestEnv, want: ""},
	} {
		if got := minerContainerKind(fakeMinerHost(t, c.files), c.getenv); got != c.want {
			t.Errorf("%v: container %q, want %q", c.files, got, c.want)
		}
	}
}

// The community image's wrapper runs the miner on a private directory inside
// the container when the directory it was given can't keep a device, the
// state directory a bind mount owned by the host's user being the common
// case. The finding names that directory, the wrapper's reason, and a volume
// at that directory, not at the private one.
func TestMinerStateSubstitutionNamesTheGivenDirectory(t *testing.T) {
	private := minerTestStateDir(t)
	root := fakeMinerHost(t, map[string]string{
		".dockerenv":          "",
		"proc/self/mountinfo": minerTestContainerMounts(minerTestMountLine(104, "/home/synthetic/.urnetwork", "/root/.urnetwork", "ext4")),
	})
	getenv := func(name string) string {
		switch name {
		case minerStateSubstitutedDirEnv:
			return "/root/.urnetwork"
		case minerStateSubstitutedProblemEnv:
			return "it is owned by uid 1000, and this container runs as uid 0"
		}
		return ""
	}
	substitution := readMinerStateSubstitution(getenv, private)
	if substitution == nil || substitution.dir != "/root/.urnetwork" {
		t.Fatalf("substitution %+v", substitution)
	}
	persistence := assessMinerStatePersistence(readMinerHostFacts(root, noMinerTestEnv), private)
	finding := minerStatePersistenceFinding(persistence, substitution, true, minerCommunityProviderImage, time.Now())
	if finding == nil {
		t.Fatal("a substituted private directory in the container's filesystem was not reported")
	}
	for _, want := range []string{"/root/.urnetwork can't keep this device's identity (it is owned by uid 1000, and this container runs as uid 0)", private, "This start registered a new device"} {
		if !strings.Contains(finding.Summary, want) {
			t.Errorf("summary lacks %q: %s", want, finding.Summary)
		}
	}
	if !strings.Contains(finding.Fix, "-v urnetwork-provider:/root/.urnetwork "+minerCommunityProviderImage) || strings.Contains(finding.Fix, "urnetwork-provider:"+private) || !strings.Contains(finding.Fix, "sudo chown -R 0:0") {
		t.Fatalf("fix:\n%s", finding.Fix)
	}
	// no substitution named, or one naming the state directory itself
	if readMinerStateSubstitution(noMinerTestEnv, private) != nil {
		t.Fatal("a substitution read from an empty environment")
	}
	same := func(name string) string {
		if name == minerStateSubstitutedDirEnv {
			return private
		}
		return ""
	}
	if readMinerStateSubstitution(same, private) != nil {
		t.Fatal("a substitution of the state directory by itself")
	}
}

// Kubernetes keeps state on a PersistentVolumeClaim, not a docker volume.
func TestMinerStateFixForKubernetes(t *testing.T) {
	dir := minerTestStateDir(t)
	root := fakeMinerHost(t, map[string]string{"proc/self/mountinfo": minerTestContainerMounts()})
	kubernetes := func(name string) string {
		if name == "KUBERNETES_SERVICE_HOST" {
			return "198.51.100.1"
		}
		return ""
	}
	persistence := assessMinerStatePersistence(readMinerHostFacts(root, kubernetes), dir)
	finding := minerStatePersistenceFinding(persistence, nil, true, minerCommunityProviderImage, time.Now())
	if finding == nil || !strings.Contains(finding.Fix, "PersistentVolumeClaim at "+dir) || strings.Contains(finding.Fix, "docker") {
		t.Fatalf("finding %+v", finding)
	}
}
