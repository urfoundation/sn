// Descriptor-relative immutable publication retains claimant identity even
// across an empty staged file. Local and registry locks follow one fixed order.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// One owner uses this descriptor serially. No operation deletes, replaces or
// releases a nonce claim; signatures outlive the reviewed native-block window.
type bootstrapSuccessorExecutionDirectory struct {
	ctx   context.Context
	path  string
	root  bootstrapSuccessorRootIdentity
	file  *os.File
	claim string
	hook  func(string) error
}

// Registry opening follows original preparation locks and exclusive root
// directory ownership. A second local owner fails without waiting or writing.
func openBootstrapSuccessorExecutionDirectory(ctx context.Context, path string, root bootstrapSuccessorRootIdentity, claim string, hook func(string) error) (_ *bootstrapSuccessorExecutionDirectory, resultErr error) {
	if ctx == nil || !planSha256(claim) {
		return nil, errors.New("successor execution directory lacks context or claim")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	self := &bootstrapSuccessorExecutionDirectory{ctx: ctx, path: path, root: root, file: os.NewFile(uintptr(fd), path), claim: claim, hook: hook}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("successor nonce registry already has an owner"), err)
	}
	return self, self.checkpoint("registry-acquired")
}

// Recheck both signed path identity and open descriptor before and after each
// hook. Root replacement cannot redirect a later publication into another tree.
func (self *bootstrapSuccessorExecutionDirectory) checkpoint(stage string) error {
	check := func() error {
		if self == nil || self.file == nil {
			return errors.New("successor execution directory is closed")
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(self.file.Fd()), &stat); err != nil {
			return err
		}
		physical, err := bootstrapSuccessorPhysicalRoot(self.path)
		if err != nil || physical != self.root || uint64(stat.Dev) != self.root.Device || stat.Ino != self.root.Inode || stat.Mode&0077 != 0 {
			return errors.Join(errors.New("successor execution physical directory changed"), err)
		}
		return self.ctx.Err()
	}
	if err := check(); err != nil {
		return err
	}
	if self.hook != nil {
		if err := self.hook(stage); err != nil {
			return err
		}
	}
	return check()
}

// A fresh directory descriptor gives every census a bounded complete view.
func (self *bootstrapSuccessorExecutionDirectory) names() ([]string, error) {
	fd, err := unix.Openat(int(self.file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), self.path)
	defer file.Close()
	names, err := file.Readdirnames(4097)
	if err != nil && !errors.Is(err, io.EOF) || len(names) > 4096 {
		return nil, errors.Join(errors.New("successor execution custody exceeds its bounded census"), err)
	}
	return names, nil
}

// Extra links, shared permissions and nonregular files never become custody.
func (self *bootstrapSuccessorExecutionDirectory) read(name string) ([]byte, error) {
	fd, err := unix.Openat(int(self.file.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	if err := bootstrapSuccessorPrivateRegular(file); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumBootstrapSuccessorExecutionBytes+1))
	if err != nil || len(raw) > maximumBootstrapSuccessorExecutionBytes {
		return nil, errors.Join(errors.New("successor execution file exceeds its byte bound"), err)
	}
	return raw, nil
}

// The target name scopes the nonce even when its claimant's stage is empty.
// The kind also distinguishes a partial terminal result from a send reservation.
func (self *bootstrapSuccessorExecutionDirectory) stageName(name, kind string) string {
	return bootstrapSuccessorExecutionStagePrefix + name + "-" + strings.TrimPrefix(self.claim, "sha256:") + "-" + kind
}

// Existing final bytes must be exact and have no competing stage. Recovery
// repairs only this approved immutable prefix and publishes with no replacement.
func (self *bootstrapSuccessorExecutionDirectory) publish(name, kind string, raw []byte) error {
	if len(raw) == 0 || len(raw) > maximumBootstrapSuccessorExecutionBytes {
		return errors.New("successor execution publication exceeds its byte bound")
	}
	if err := self.checkpoint(name + ":begin"); err != nil {
		return err
	}
	stage := self.stageName(name, kind)
	names, err := self.names()
	if err != nil {
		return err
	}
	staged := false
	for _, candidate := range names {
		if strings.HasPrefix(candidate, bootstrapSuccessorExecutionStagePrefix+name+"-") {
			if candidate != stage {
				return errors.New("successor execution retains a competing staged nonce or event")
			}
			staged = true
		}
	}
	retained, err := self.read(name)
	if err == nil {
		if staged || !bytes.Equal(raw, retained) {
			return errors.New("successor execution retained publication differs; preserve custody")
		}
		return self.file.Sync()
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fd := int(self.file.Fd())
	stageFd, err := unix.Openat(fd, stage, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CREAT|unix.O_EXCL, 0600)
	if errors.Is(err, unix.EEXIST) {
		stageFd, err = unix.Openat(fd, stage, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(stageFd), stage)
	defer file.Close()
	if err := bootstrapSuccessorPrivateRegular(file); err != nil {
		return err
	}
	retained, err = io.ReadAll(io.LimitReader(file, int64(len(raw))+1))
	if err != nil || !bytes.HasPrefix(raw, retained) {
		return errors.Join(errors.New("successor execution stage differs from its exact immutable prefix"), err)
	}
	if err := self.checkpoint(name + ":name-created"); err != nil {
		return err
	}
	if err := self.file.Sync(); err != nil {
		return err
	}
	if err := self.checkpoint(name + ":name-synced"); err != nil {
		return err
	}
	written, err := file.WriteAt(raw, 0)
	if err != nil || written != len(raw) {
		return errors.Join(io.ErrShortWrite, err)
	}
	if err := self.checkpoint(name + ":stage-written"); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := self.checkpoint(name + ":stage-synced"); err != nil {
		return err
	}
	if err := unix.Renameat2(fd, stage, fd, name, unix.RENAME_NOREPLACE); err != nil {
		return errors.Join(errors.New("successor execution publication refused an existing fixed name"), err)
	}
	if err := self.checkpoint(name + ":published"); err != nil {
		return err
	}
	if err := self.file.Sync(); err != nil {
		return err
	}
	return self.checkpoint(name + ":published-synced")
}

// Ownership ends without releasing any durable nonce or financial liability.
func (self *bootstrapSuccessorExecutionDirectory) close() error {
	if self == nil || self.file == nil {
		return nil
	}
	err := self.file.Close()
	self.file = nil
	return err
}
