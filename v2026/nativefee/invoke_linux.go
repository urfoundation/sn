//go:build linux

// Owns a pinned verifier process and admits only bounded, joined output.
package nativefee

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const maximumExecutableBytes = 512 * 1024 * 1024
const maximumStatementBytes = 2 * 1024 * 1024

type invocationHooks struct {
	beforeStart func(context.Context, *os.File)
	afterOutput func()
}

// Invoke runs the approved original verifier and joins its complete process
// tree. Failed, canceled, unknown or malformed output yields no usable result.
// One budget includes file pinning, native verification, output and child join.
func Invoke(ctx context.Context, authority Authority, request Reference, transactionHash string, budget time.Duration) (*Verified, error) {
	return invoke(ctx, authority, request, transactionHash, budget, invocationHooks{})
}

func invoke(ctx context.Context, authority Authority, request Reference, transactionHash string, budget time.Duration, hooks invocationHooks) (result *Verified, resultErr error) {
	if ctx == nil || budget < time.Minute || budget > 15*time.Minute || !canonicalHex(transactionHash, "0x", 32) {
		return nil, errors.New("native fee invocation requires owner, transaction and 60s–15m total budget")
	}
	if err := errors.Join(authority.Validate(), request.Validate(), ctx.Err()); err != nil {
		return nil, err
	}
	owner, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, owner.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	verifier, err := retainedExecutable(owner, authority.Verifier)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, verifier.Close()) }()
	supervisor, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, supervisor.Close()) }()
	policy, err := json.Marshal(authority.NativePolicy)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(owner, "/proc/self/fd/4", "verify-native-fee-outcome", "--request", request.Path, "--request-sha256", request.Sha256, "--transaction", transactionHash, "--policy-json", string(policy), "--budget", budget.String())
	command.Args[0] = "urnetwork-native-fee-supervisor"
	command.ExtraFiles = []*os.File{verifier, supervisor}
	command.Dir = filepath.Dir(request.Path)
	command.Env = []string{"LANG=C", "LC_ALL=C", "RUST_BACKTRACE=0"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := command.Process.Signal(syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	// The dedicated supervisor owns group termination and reaping. It has no
	// unrelated children and cannot pass a successful result before that join.
	command.WaitDelay = 10 * time.Second
	stdout := boundedOutput{maximum: maximumStatementBytes, cancel: cancel, after: hooks.afterOutput}
	stderr := boundedOutput{maximum: 64 * 1024, cancel: cancel}
	command.Stdout, command.Stderr = &stdout, &stderr
	if hooks.beforeStart != nil {
		hooks.beforeStart(owner, verifier)
	}
	if err := command.Run(); err != nil {
		return nil, errors.Join(err, stdout.err, stderr.err, owner.Err(), fmt.Errorf("native fee verifier: %s", stderr.buffer.Bytes()))
	}
	if err := errors.Join(stdout.err, stderr.err, owner.Err()); err != nil {
		return nil, err
	}
	var statement Statement
	decoder := json.NewDecoder(bytes.NewReader(stdout.buffer.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&statement); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("native fee verifier output requires exactly one statement")
	}
	if err := statement.Validate(authority.NativePolicy, request, transactionHash); err != nil {
		return nil, err
	}
	return &Verified{statement: statement, authority: authority, completed: true}, owner.Err()
}

// One command copy goroutine owns each stream until Run joins it. Keep the
// buffer private: embedding would promote ReadFrom and let io.Copy bypass Write.
type boundedOutput struct {
	buffer  bytes.Buffer
	maximum int
	cancel  context.CancelFunc
	after   func()
	err     error
}

// Admits each copied chunk before allocation and latches the first refusal.
func (self *boundedOutput) Write(raw []byte) (int, error) {
	if self.err != nil {
		return 0, self.err
	}
	if len(raw) > self.maximum-self.buffer.Len() {
		self.err = errors.New("native fee verifier output exceeds bound")
		self.cancel()
		return 0, self.err
	}
	n, err := self.buffer.Write(raw)
	if self.after != nil {
		self.after()
	}
	return n, err
}

// Execute the immutable bytes we hashed, not the path that happened to supply
// them. Descriptor retention also closes the hash/exec path replacement race.
func retainedExecutable(ctx context.Context, reference Reference) (result *os.File, resultErr error) {
	if err := errors.Join(reference.Validate(), ctx.Err()); err != nil {
		return nil, err
	}
	fd, err := unix.Open(reference.Path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	source := os.NewFile(uintptr(fd), reference.Path)
	var retained *os.File
	defer func() {
		resultErr = errors.Join(resultErr, source.Close())
		if resultErr != nil {
			if retained != nil {
				resultErr = errors.Join(resultErr, retained.Close())
			}
			result = nil
		}
	}()
	before, err := source.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Mode().Perm()&0111 == 0 || before.Mode().Perm()&0022 != 0 || before.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || before.Size() < 4 || before.Size() > maximumExecutableBytes || stat.Nlink != 1 {
		return nil, errors.New("native fee verifier must be a protected bounded executable")
	}
	fd, err = unix.MemfdCreate("urnetwork-native-fee-verifier", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	retained = os.NewFile(uintptr(fd), "retained-native-fee-verifier")
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := source.Read(buffer)
		if total == 0 && (n < 4 || !bytes.Equal(buffer[:4], []byte{0x7f, 'E', 'L', 'F'})) {
			return nil, errors.New("native fee verifier is not ELF")
		}
		total += int64(n)
		if total > before.Size() || total > maximumExecutableBytes {
			return nil, errors.New("native fee verifier grew during retention")
		}
		if _, writeErr := io.MultiWriter(retained, digest).Write(buffer[:n]); writeErr != nil {
			return nil, writeErr
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	after, err := source.Stat()
	if err != nil {
		return nil, err
	}
	final, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev != final.Dev || stat.Ino != final.Ino || stat.Mode != final.Mode || stat.Nlink != final.Nlink || stat.Mtim != final.Mtim || stat.Ctim != final.Ctim || total != before.Size() || after.Size() != total || "sha256:"+hex.EncodeToString(digest.Sum(nil)) != reference.Sha256 {
		return nil, errors.New("native fee verifier changed or differs from independent pin")
	}
	if err := retained.Chmod(0500); err != nil {
		return nil, err
	}
	if _, err := unix.FcntlInt(retained.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, err
	}
	return retained, ctx.Err()
}
