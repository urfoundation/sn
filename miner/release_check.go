package miner

// The running miner's release check. A miner that nothing updates says so,
// with the command that updates it, when the latest release of urnetwork/build
// (the feed provider update installs from, cli_update.go) is newer than this
// build. Nothing updates a miner in a container: its binary is the image's,
// and only a new image replaces it. Nothing updates a miner run by hand
// either, nor one whose automatic updates are off. Where an updater runs (the
// service's update timer, the official installer's), a release is reported
// only once the updater should have installed it and hasn't.
//
// The check only reads the feed; it never installs. It runs in the process
// that owns the run (provide, or the supervisor of provide --all-operators),
// never in an operator child, so a host asks the feed once per interval.

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// The release check's timing.
type minerReleaseCheckSettings struct {
	// FirstCheckDelay is the wait after start before the first check, so a
	// miner that exits at once, such as one restarting in a loop, asks the
	// feed nothing: 2 minutes. Non-positive checks at once.
	FirstCheckDelay time.Duration
	// CheckInterval is the wait between checks: 6 hours, the update timer's
	// interval. Non-positive checks only once.
	CheckInterval time.Duration
	// CheckJitter is the most added at random to each wait, so the miners of
	// a fleet restarted together don't ask the feed together: 1 hour.
	// Non-positive adds nothing.
	CheckJitter time.Duration
	// AutomaticUpdateGrace is how long after its publication an automatic
	// updater should have installed a release: the rollout window
	// (minerUpdateRollout) and two check intervals, 28 hours. Where an updater
	// runs, only an older release is reported.
	AutomaticUpdateGrace time.Duration
}

func defaultMinerReleaseCheckSettings() *minerReleaseCheckSettings {
	return &minerReleaseCheckSettings{
		FirstCheckDelay:      2 * time.Minute,
		CheckInterval:        6 * time.Hour,
		CheckJitter:          time.Hour,
		AutomaticUpdateGrace: minerUpdateRollout + 12*time.Hour,
	}
}

// Who installs a new release of this miner.
type minerUpdater struct {
	// one of minerContainer*: only a new image updates a container's miner
	container string
	// the official installer that manages the binary, nil for none
	installer *minerInstaller
	// the installer's automatic update is on, or can't be read
	installerAuto bool
	// a service is installed, whose update timer installs releases
	timer bool
	// why automatic updates are off, "" while they are on
	off string
}

// readMinerUpdater reads who updates this miner, in container (one of
// minerContainer*, or "").
func readMinerUpdater(container string) minerUpdater {
	updater := minerUpdater{container: container}
	if container != "" {
		return updater
	}
	if executable, err := cliExecutable(); err == nil {
		if installer := findMinerInstaller(executable); installer != nil {
			on, known := installer.autoUpdate()
			updater.installer, updater.installerAuto = installer, on || !known
			return updater
		}
	}
	if on, why := minerAutoUpdate(readMinerUpdateState()); !on {
		updater.off = why
	}
	updater.timer = readMinerServiceState().Installed
	return updater
}

// minerUpdateFinding is what to tell the operator of a miner that runs
// current while release is the latest, nil for nothing: current is the
// latest, or isn't a release, or an updater will install the release in time.
func minerUpdateFinding(current string, release *minerRelease, updater minerUpdater, image string, now time.Time, settings *minerReleaseCheckSettings) *minerFinding {
	if release == nil {
		return nil
	}
	latest, ok := parseMinerReleaseVersion(release.TagName)
	running, runningOk := parseMinerReleaseVersion(current)
	if !ok || !runningOk || !latest.newerThan(running) {
		return nil
	}
	age := now.Sub(release.PublishedAt)
	late := !release.PublishedAt.IsZero() && settings.AutomaticUpdateGrace <= age
	finding := &minerFinding{
		Code:    minerFindingUpdateAvailable,
		Title:   fmt.Sprintf("provider %s is out; this miner runs %s", release.version(), current),
		FoundAt: now.UTC(),
	}
	since := "since its release"
	if !release.PublishedAt.IsZero() {
		since = cliDuration(age) + " after its release"
	}
	switch {
	case updater.container == minerContainerKubernetes:
		finding.Summary = "This miner runs in a container, which never updates itself: only a new image does. A miner that falls behind misses fixes to providing."
		finding.Fix = "Roll the pods onto the new image: kubectl rollout restart <deployment or statefulset>, with imagePullPolicy: Always, or set the image to " + minerVersionedImage(image, release.version()) + ".\n" +
			"Keep the state directory's volume, so the device keeps its identity."
	case updater.container != "":
		tool := "docker"
		if updater.container == minerContainerPodman {
			tool = "podman"
		}
		finding.Summary = "This miner runs in a container, which never updates itself: only a new image does. A miner that falls behind misses fixes to providing."
		finding.Fix = "With compose: " + tool + " compose pull && " + tool + " compose up -d\n" +
			"Otherwise: " + tool + " pull " + image + ", then remove this container and start a new one with the same command and state volume (" + tool + " rm -f <container>, then the same " + tool + " run).\n" +
			"Keep the state directory's volume, so the device keeps its identity."
	case updater.installer != nil && updater.installerAuto:
		if !late {
			return nil
		}
		finding.Summary = fmt.Sprintf("The URnetwork installer updates this miner, but %s it still runs the older build.", since)
		finding.Fix = updater.installer.display("update")
	case updater.installer != nil:
		finding.Summary = "Automatic updates are off in the URnetwork installer."
		finding.Fix = updater.installer.display("update") + "\n" +
			"or turn automatic updates back on: provider update --on"
	case updater.off != "":
		finding.Summary = "Automatic updates are off: " + updater.off + "."
		finding.Fix = "provider update, then restart the miner if no service runs it\n" +
			"or turn automatic updates back on: provider update --on"
	case updater.timer:
		if !late {
			return nil
		}
		finding.Summary = fmt.Sprintf("Automatic updates are on, but %s this miner still runs the older build. provider update --status says what the last check found.", since)
		finding.Fix = "provider update"
	default:
		finding.Summary = "Nothing updates this miner: automatic updates come with the background service."
		finding.Fix = "provider update, then restart the miner\n" +
			"or run it as the background service, which updates it: provider service install"
	}
	return finding
}

// The image of version, for the community image; another image is named with
// its new tag.
func minerVersionedImage(image string, version string) string {
	if image == minerCommunityProviderImage {
		return strings.TrimSuffix(image, ":latest") + ":" + version
	}
	return image + " at its new tag"
}

// The parts of one release check, so tests supply their own feed and clock.
type minerReleaseCheck struct {
	// the version this miner runs
	current string
	fetch   func(context.Context) (*minerRelease, error)
	updater func() minerUpdater
	image   string
	now     func() time.Time
	// set while this build is behind, cleared once it isn't
	findings *minerFindings
	// tells the operator of a finding
	report   func(minerFinding)
	settings *minerReleaseCheckSettings
}

// check asks the feed once. A finding is set and reported; a build that is the
// latest clears it; a feed that can't be read changes nothing.
func (self *minerReleaseCheck) check(ctx context.Context) {
	release, err := self.fetch(ctx)
	if err != nil {
		return
	}
	finding := minerUpdateFinding(self.current, release, self.updater(), self.image, self.now(), self.settings)
	if finding == nil {
		self.findings.clear(minerFindingUpdateAvailable)
		return
	}
	self.findings.set(*finding)
	if self.report != nil {
		self.report(*finding)
	}
}

// start checks FirstCheckDelay after it is called and then every
// CheckInterval, each wait plus up to CheckJitter, until ctx ends or stop is
// called. stop joins the loop.
func (self *minerReleaseCheck) start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait := self.settings.FirstCheckDelay
		for {
			if 0 < self.settings.CheckJitter {
				wait += rand.N(self.settings.CheckJitter)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			self.check(ctx)
			if self.settings.CheckInterval <= 0 {
				return
			}
			wait = self.settings.CheckInterval
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
