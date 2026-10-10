// The command layer moves reviewed files through preparation and publication.
// Continuous mode consumes only digest-named approved requests; it never
// discovers the roster population. Durably acknowledged queue objects are
// archived without deletion so completed jobs cannot stall future approvals.
package payoutroster

import (
	"github.com/urnetwork/connect/v2026/durablesys"

	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const maxInboxEntries = 128
const operationTimeout = 300 * time.Second
const inboxPollInterval = 15 * time.Second

const commandUsage = `Usage:
  payoutroster prepare --config <file> --input <file> --output <new-file>
  payoutroster sign --config <file> --request <file> --request-sha256 <digest>
  payoutroster once --config <file> --request <file> --request-sha256 <digest>
  payoutroster run --config <file>

prepare writes an unsigned canonical request for independent review.
sign retains the exact signed authority locally. once also publishes it.
run processes approved <lowercase-request-sha256>.json files every 15 seconds.
Acknowledged requests move into a private completed directory in the inbox.
Config, input, request, and key files must be physical regular files.
Prepare output and inbox requests use mode 0600 in private mode 0700 directories.
`

// Dependencies are per invocation, so tests can observe command admission and
// cancellation without replacing global state or invoking keys or transport.
type commandDependencies struct {
	prepare func(context.Context, Config, []byte) ([]byte, error)
	execute func(context.Context, Config, []byte, string, bool) (Result, error)
	wait    func(context.Context, time.Duration) error
}

// A retained directory cursor advances past failed jobs and unrelated entries.
// Each poll holds at most one bounded batch; end of directory starts a new pass.
type inboxCursor struct {
	directory *os.File
}

// Return the directory descriptor when a full pass ends or the runner exits.
func (self *inboxCursor) close() {
	if self.directory != nil {
		self.directory.Close()
		self.directory = nil
	}
}

// Run the CLI with a caller-owned context and streams. The executable owns
// signals and exit codes; this package never terminates the host process.
func RunCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runCommand(ctx, args, stdout, stderr, commandDependencies{
		prepare: Prepare,
		execute: Execute,
		wait: func(ctx context.Context, delay time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				return nil
			}
		},
	})
}

// Validate the exact command grammar before loading config or file contents.
func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer, dependencies commandDependencies) error {
	if ctx == nil || stdout == nil || stderr == nil {
		return errors.New("payout roster command requires a context and output streams")
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(stdout, commandUsage)
		return err
	}
	command := args[0]
	if command != "prepare" && command != "sign" && command != "once" && command != "run" {
		return errors.New("unknown payout roster command; use --help")
	}
	flags := flag.NewFlagSet("payoutroster "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "reviewed YAML configuration")
	var inputPath, outputPath, requestPath, requestSha256 string
	if command == "prepare" {
		flags.StringVar(&inputPath, "input", "", "reviewed complete population input")
		flags.StringVar(&outputPath, "output", "", "new private canonical request file")
	}
	if command == "sign" || command == "once" {
		flags.StringVar(&requestPath, "request", "", "reviewed canonical request file")
		flags.StringVar(&requestSha256, "request-sha256", "", "exact lowercase SHA-256 of reviewed request bytes")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err := io.WriteString(stdout, commandUsage)
			return err
		}
		return errors.New("invalid payout roster command flags; use --help")
	}
	if flags.NArg() != 0 || *configPath == "" {
		return errors.New("payout roster command requires --config and no positional arguments")
	}
	if command == "prepare" && (inputPath == "" || outputPath == "") {
		return errors.New("payout roster prepare requires --input and --output")
	}
	if (command == "sign" || command == "once") && (requestPath == "" || !isRequestDigest(requestSha256)) {
		return errors.New("payout roster signing requires --request and a lowercase 64-character --request-sha256")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	config, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if command == "run" {
		return runInbox(ctx, config, stdout, stderr, dependencies)
	}
	operationCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if command == "prepare" {
		raw, err := readRosterFile(inputPath, MaxRequestBytes, false)
		if err != nil {
			return fmt.Errorf("read payout roster population input: %w", err)
		}
		if err := operationCtx.Err(); err != nil {
			return err
		}
		request, err := dependencies.prepare(operationCtx, config, raw)
		if err != nil {
			return err
		}
		if err := operationCtx.Err(); err != nil {
			return err
		}
		if err := writePreparedRequest(outputPath, request); err != nil {
			return err
		}
		digest := sha256.Sum256(request)
		return json.NewEncoder(stdout).Encode(struct {
			RequestSha256 string `json:"request_sha256"`
		}{RequestSha256: hex.EncodeToString(digest[:])})
	}
	raw, err := readRosterFile(requestPath, MaxRequestBytes, true)
	if err != nil {
		return fmt.Errorf("read approved payout roster request: %w", err)
	}
	if err := operationCtx.Err(); err != nil {
		return err
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != requestSha256 {
		return errors.New("payout roster request digest does not match the independently reviewed bytes")
	}
	result, err := dependencies.execute(operationCtx, config, raw, requestSha256, command == "once")
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// Retain only a newly created file in an independently owned private review
// directory. Sync both file and directory before printing its approval digest.
func writePreparedRequest(path string, raw []byte) (resultErr error) {
	if path == "" || len(raw) == 0 || len(raw) > MaxRequestBytes {
		return errors.New("prepared payout roster request path or byte length is invalid")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return errors.New("resolve prepared payout roster request path failed")
	}
	directory, err := openRosterDirectory(filepath.Dir(absolute), true)
	if err != nil {
		return fmt.Errorf("open private payout roster review directory: %w", err)
	}
	defer directory.Close()
	name := filepath.Base(absolute)
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return fmt.Errorf("create new prepared payout roster request: %w", err)
	}
	file := os.NewFile(uintptr(fd), "payout-roster-prepared-request")
	defer func() {
		file.Close()
		if resultErr != nil {
			if err := unix.Unlinkat(int(directory.Fd()), name, 0); err != nil {
				resultErr = errors.Join(resultErr, errors.New("remove incomplete prepared payout roster request failed"))
			}
			directory.Sync()
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return errors.New("protect prepared payout roster request failed")
	}
	if _, err := file.Write(raw); err != nil {
		return errors.New("write prepared payout roster request failed")
	}
	if err := file.Sync(); err != nil {
		return errors.New("sync prepared payout roster request failed")
	}
	if err := file.Close(); err != nil {
		return errors.New("close prepared payout roster request failed")
	}
	if err := directory.Sync(); err != nil {
		return errors.New("sync payout roster review directory failed")
	}
	return nil
}

// Public request pins use one spelling so a filename is also an unambiguous
// approval digest. No path or other caller-controlled string enters job logs.
func isRequestDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// A missing inbox or failed job remains pending for a later poll. The loop
// owns no worker goroutines and returns as soon as its context is canceled.
func runInbox(ctx context.Context, config Config, stdout, stderr io.Writer, dependencies commandDependencies) error {
	cursor := &inboxCursor{}
	defer cursor.close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := processInbox(ctx, config, stdout, stderr, dependencies.execute, cursor); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if _, writeErr := fmt.Fprintf(stderr, "payout roster inbox pending: %v\n", err); writeErr != nil {
				return errors.New("write payout roster inbox status failed")
			}
		}
		if err := dependencies.wait(ctx, inboxPollInterval); err != nil {
			return err
		}
	}
}

// One bounded poll is a deterministic unit for command tests. Approved jobs
// execute sequentially with independent deadlines; one failed job cannot
// block later batches. Successful records remain in custody and Execute
// recognizes their already-retained acknowledgements on interrupted retirement.
func processInbox(ctx context.Context, config Config, stdout, stderr io.Writer, execute func(context.Context, Config, []byte, string, bool) (Result, error), cursor *inboxCursor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := openRosterDirectory(config.InboxDirectory, true)
	if err != nil {
		cursor.close()
		return err
	}
	if cursor.directory != nil {
		current, currentErr := directory.Stat()
		retained, retainedErr := cursor.directory.Stat()
		if currentErr != nil || retainedErr != nil || !os.SameFile(current, retained) {
			cursor.close()
		}
	}
	if cursor.directory == nil {
		cursor.directory = directory
	} else {
		directory.Close()
		directory = cursor.directory
	}
	entries, err := directory.ReadDir(maxInboxEntries)
	if err != nil && !errors.Is(err, io.EOF) {
		cursor.close()
		return errors.New("read payout roster inbox failed")
	}
	if errors.Is(err, io.EOF) {
		defer cursor.close()
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".json") && isRequestDigest(strings.TrimSuffix(name, ".json")) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		requestSha256 := strings.TrimSuffix(name, ".json")
		result, jobErr := func() (Result, error) {
			operationCtx, cancel := context.WithTimeout(ctx, operationTimeout)
			defer cancel()
			requestFile, err := openRosterFileAt(directory, name)
			if err != nil {
				return Result{}, err
			}
			defer requestFile.Close()
			raw, err := readRosterOpenedFile(requestFile, MaxRequestBytes, true)
			if err != nil {
				return Result{}, err
			}
			if err := operationCtx.Err(); err != nil {
				return Result{}, err
			}
			if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != requestSha256 {
				return Result{}, errors.New("approved filename digest does not match request bytes")
			}
			result, err := execute(operationCtx, config, raw, requestSha256, true)
			if err != nil {
				return Result{}, err
			}
			if result.Published {
				if err := archiveApprovedRequest(operationCtx, directory, name, requestFile, raw); err != nil {
					return Result{}, err
				}
			}
			return result, nil
		}()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if jobErr != nil {
			if _, err := fmt.Fprintf(stderr, "payout roster request %s pending: %v\n", requestSha256, jobErr); err != nil {
				return errors.New("write payout roster job status failed")
			}
			continue
		}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return errors.New("write payout roster result failed")
		}
	}
	return nil
}

// Capture the queue name atomically before checking its identity. A replaced
// object is restored without replacement or preserved in completed custody;
// no pathname-based conditional unlink can delete an unreviewed replacement.
func archiveApprovedRequest(ctx context.Context, directory *os.File, name string, requestFile *os.File, expected []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Mkdirat(int(directory.Fd()), "completed", 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return errors.New("create payout roster completed directory failed")
	}
	fd, err := unix.Openat(int(directory.Fd()), "completed", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("open physical payout roster completed directory failed")
	}
	completed := os.NewFile(uintptr(fd), "payout-roster-completed-directory")
	defer completed.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o7777 != 0o700 {
		return errors.New("payout roster completed directory must be current-user owned with mode 0700")
	}
	if err := directory.Sync(); err != nil {
		return errors.New("sync payout roster completed directory creation failed")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errors.New("prepare unique payout roster completed filename failed")
	}
	archivedName := strings.TrimSuffix(name, ".json") + "." + hex.EncodeToString(nonce[:]) + ".json"
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := durablesys.RenameNoReplace(int(directory.Fd()), name, fd, archivedName); err != nil {
		return errors.New("capture acknowledged payout roster request failed")
	}
	validCapture := func() bool {
		captured, err := openRosterFileAt(completed, archivedName)
		if err != nil {
			return false
		}
		defer captured.Close()
		originalInfo, originalErr := requestFile.Stat()
		capturedInfo, capturedErr := captured.Stat()
		if originalErr != nil || capturedErr != nil || !os.SameFile(originalInfo, capturedInfo) {
			return false
		}
		raw, err := readRosterOpenedFile(captured, MaxRequestBytes, true)
		return err == nil && bytes.Equal(raw, expected)
	}()
	if !validCapture {
		restoreErr := durablesys.RenameNoReplace(fd, archivedName, int(directory.Fd()), name)
		syncErr := errors.Join(completed.Sync(), directory.Sync())
		if restoreErr != nil {
			return errors.New("payout roster queue changed during retirement; captured replacement preserved in completed because queue restoration was unavailable")
		}
		if syncErr != nil {
			return errors.New("payout roster queue changed during retirement; replacement restored but directory sync failed")
		}
		return errors.New("payout roster queue changed during retirement; replacement restored")
	}
	if err := errors.Join(completed.Sync(), directory.Sync()); err != nil {
		return errors.New("sync completed payout roster request retirement failed")
	}
	return nil
}
