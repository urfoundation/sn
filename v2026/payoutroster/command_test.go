// Command tests inject preparation and execution, exercising file and lifecycle
// boundaries without accessing keys, RPC, API, or the production roster engine.
package payoutroster

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
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Store public config separately from the inputs whose bytes a test controls.
func commandTestConfigPath(t *testing.T, config Config) string {
	t.Helper()
	path := filepath.Join(commandTestDirectory(t), "payout_roster.yml")
	commandTestWrite(t, path, commandTestConfigBytes(t, config))
	return path
}

// The queue fixture is private and starts without a completed archive.
func commandTestInbox(t *testing.T) Config {
	t.Helper()
	config := commandTestConfig(t)
	if err := os.Mkdir(config.InboxDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	return config
}

// Filename approval pins exactly the bytes written into the private queue.
func commandTestJob(t *testing.T, config Config, raw []byte) string {
	t.Helper()
	digest := sha256.Sum256(raw)
	requestSha256 := hex.EncodeToString(digest[:])
	commandTestWrite(t, filepath.Join(config.InboxDirectory, requestSha256+".json"), raw)
	return requestSha256
}

// Cursor ownership is explicit even for a single deterministic test poll.
func commandTestPoll(t *testing.T, ctx context.Context, config Config, stdout, stderr io.Writer, execute func(context.Context, Config, []byte, string, bool) (Result, error)) error {
	t.Helper()
	cursor := &inboxCursor{}
	defer cursor.close()
	return processInbox(ctx, config, stdout, stderr, execute, cursor)
}

// Preparation cannot sign and publishes only a digest after a durable, private,
// no-replacement output has been written.
func TestPayoutRosterCommandPrepareCreatesPrivateRequest(t *testing.T) {
	config := commandTestConfig(t)
	configPath := commandTestConfigPath(t, config)
	directory := commandTestDirectory(t)
	inputPath, outputPath := filepath.Join(directory, "input.json"), filepath.Join(directory, "request.json")
	input := []byte(`{"reviewed":"complete"}`)
	request := []byte(`{"canonical":"request"}`)
	commandTestWrite(t, inputPath, input)
	var stdout, stderr bytes.Buffer
	called := 0
	dependencies := commandDependencies{
		prepare: func(ctx context.Context, actual Config, raw []byte) ([]byte, error) {
			called++
			if actual != config || !bytes.Equal(raw, input) {
				t.Fatal("preparation inputs changed")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("preparation has no bounded owner")
			}
			return request, nil
		},
		execute: func(context.Context, Config, []byte, string, bool) (Result, error) {
			t.Fatal("prepare attempted signing")
			return Result{}, nil
		},
	}
	args := []string{"prepare", "--config", configPath, "--input", inputPath, "--output", outputPath}
	if err := runCommand(context.Background(), args, &stdout, &stderr, dependencies); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(actual, request) {
		t.Fatalf("prepared request changed: %q, %v", actual, err)
	}
	info, err := os.Stat(outputPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("prepared file is not private: %v, %v", info, err)
	}
	var result map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(request)
	if called != 1 || len(result) != 1 || result["request_sha256"] != hex.EncodeToString(digest[:]) || stderr.Len() != 0 {
		t.Fatalf("unexpected prepare result: calls %d, output %s, error %s", called, stdout.Bytes(), stderr.Bytes())
	}
	if err := runCommand(context.Background(), args, io.Discard, io.Discard, dependencies); err == nil {
		t.Fatal("prepare overwrote an existing reviewed output")
	}
	actual, err = os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(actual, request) {
		t.Fatal("rejected overwrite changed approved bytes")
	}
}

// Existing links, ordinary shared parent directories, and empty output cannot
// become newly prepared approval artifacts.
func TestPayoutRosterPreparedRequestRejectsAliasesAndSharedDirectory(t *testing.T) {
	directory := commandTestDirectory(t)
	target := filepath.Join(directory, "target.json")
	alias := filepath.Join(directory, "alias.json")
	commandTestWrite(t, target, []byte("existing"))
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if err := writePreparedRequest(alias, []byte("replacement")); err == nil {
		t.Fatal("symlink output accepted")
	}
	actual, err := os.ReadFile(target)
	if err != nil || string(actual) != "existing" {
		t.Fatal("output symlink target was changed")
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePreparedRequest(filepath.Join(directory, "new.json"), []byte("request")); err == nil {
		t.Fatal("shared output parent accepted")
	}
}

// Sign and once forward the same exact pin and bytes, differing only in their
// publication choice. Both honor an earlier caller deadline and cancel owners.
func TestPayoutRosterCommandSignAndOnceForwardReviewedRequest(t *testing.T) {
	config := commandTestConfig(t)
	configPath := commandTestConfigPath(t, config)
	raw := []byte(`{"reviewed":"request"}`)
	requestPath := filepath.Join(commandTestDirectory(t), "request.json")
	commandTestWrite(t, requestPath, raw)
	digest := sha256.Sum256(raw)
	pin := hex.EncodeToString(digest[:])
	for _, command := range []string{"sign", "once"} {
		deadline := time.Now().Add(time.Minute)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		var observed context.Context
		var stdout bytes.Buffer
		dependencies := commandDependencies{execute: func(ctx context.Context, actual Config, received []byte, hash string, publish bool) (Result, error) {
			observed = ctx
			if actual != config || !bytes.Equal(received, raw) || hash != pin || publish != (command == "once") {
				t.Fatalf("%s changed its reviewed execution inputs", command)
			}
			if actualDeadline, ok := ctx.Deadline(); !ok || !actualDeadline.Equal(deadline) {
				t.Fatal("command ignored the earlier owner deadline")
			}
			return Result{RequestSha256: hash, Published: publish}, nil
		}}
		err := runCommand(ctx, []string{command, "--config", configPath, "--request", requestPath, "--request-sha256", pin}, &stdout, io.Discard, dependencies)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if observed == nil || !errors.Is(observed.Err(), context.Canceled) {
			t.Fatal("command did not close its operation owner")
		}
		var result Result
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.RequestSha256 != pin || result.Published != (command == "once") {
			t.Fatalf("unexpected %s result: %s, %v", command, stdout.Bytes(), err)
		}
	}
}

// A pin mismatch is rejected at the file boundary before execution or key use.
func TestPayoutRosterCommandRejectsChangedReviewedBytes(t *testing.T) {
	config := commandTestConfig(t)
	configPath := commandTestConfigPath(t, config)
	requestPath := filepath.Join(commandTestDirectory(t), "request.json")
	commandTestWrite(t, requestPath, []byte(`{"changed":true}`))
	dependencies := commandDependencies{execute: func(context.Context, Config, []byte, string, bool) (Result, error) {
		t.Fatal("digest mismatch reached execution")
		return Result{}, nil
	}}
	err := runCommand(context.Background(), []string{"once", "--config", configPath, "--request", requestPath, "--request-sha256", strings.Repeat("a", 64)}, io.Discard, io.Discard, dependencies)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("reviewed digest mismatch accepted: %v", err)
	}
}

// Unsupported flags and missing pins fail before config reads. Flag errors
// never echo arbitrary command-line values that could be accidental secrets.
func TestPayoutRosterCommandRejectsInvalidGrammarBeforeIo(t *testing.T) {
	for _, args := range [][]string{
		{"unknown"},
		{"prepare", "--input", "unused", "--output", "unused"},
		{"once", "--config", "absent", "--request", "absent"},
		{"sign", "--config", "absent", "--request", "absent", "--request-sha256", strings.Repeat("A", 64)},
		{"run", "--config", "absent", "unexpected"},
		{"run", "--private-key", "synthetic-sensitive-value"},
	} {
		err := runCommand(context.Background(), args, io.Discard, io.Discard, commandDependencies{})
		if err == nil || strings.Contains(err.Error(), "synthetic-sensitive-value") || strings.Contains(err.Error(), "read payout roster config") {
			t.Fatalf("invalid grammar reached I/O or leaked input: %v", err)
		}
	}
}

// Help and pre-canceled operations need no valid files or injected core.
func TestPayoutRosterCommandHelpAndCancellationAvoidIo(t *testing.T) {
	var stdout bytes.Buffer
	if err := runCommand(context.Background(), []string{"--help"}, &stdout, io.Discard, commandDependencies{}); err != nil || !strings.Contains(stdout.String(), "--request-sha256") {
		t.Fatalf("help failed: %s, %v", stdout.String(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runCommand(ctx, []string{"run", "--config", "absent"}, io.Discard, io.Discard, commandDependencies{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled command reached config: %v", err)
	}
}

// A durable acknowledgement retires only the exact private queue object and
// emits the public result after both sides of the archive move are synced.
func TestPayoutRosterInboxArchivesAcknowledgedRequest(t *testing.T) {
	config := commandTestInbox(t)
	raw := []byte(`{"request":1}`)
	pin := commandTestJob(t, config, raw)
	var stdout, stderr bytes.Buffer
	execute := func(ctx context.Context, actual Config, received []byte, hash string, publish bool) (Result, error) {
		if actual != config || !bytes.Equal(received, raw) || hash != pin || !publish {
			t.Fatal("inbox altered approved execution")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("inbox job has no deadline")
		}
		return Result{RequestSha256: hash, Published: true}, nil
	}
	if err := commandTestPoll(t, context.Background(), config, &stdout, &stderr, execute); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config.InboxDirectory, pin+".json")); !os.IsNotExist(err) {
		t.Fatalf("acknowledged request was not retired: %v", err)
	}
	completed := filepath.Join(config.InboxDirectory, "completed")
	entries, err := os.ReadDir(completed)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), pin+".") {
		t.Fatalf("exact completed request absent: %v, %v", entries, err)
	}
	actual, err := os.ReadFile(filepath.Join(completed, entries[0].Name()))
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("completed request bytes changed")
	}
	info, err := os.Stat(completed)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("completed directory is not private")
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || !result.Published || result.RequestSha256 != pin || stderr.Len() != 0 {
		t.Fatalf("unexpected retirement output: %s, %s, %v", stdout.Bytes(), stderr.Bytes(), err)
	}
}

// Failed jobs remain approved queue entries while independent jobs in the same
// batch can complete. An unacknowledged success also remains queued.
func TestPayoutRosterInboxContinuesAfterFailureAndKeepsUnpublished(t *testing.T) {
	config := commandTestInbox(t)
	failedPin := commandTestJob(t, config, []byte(`{"job":"failed"}`))
	unsignedPin := commandTestJob(t, config, []byte(`{"job":"unsigned"}`))
	publishedPin := commandTestJob(t, config, []byte(`{"job":"published"}`))
	seen := map[string]bool{}
	var stderr bytes.Buffer
	execute := func(_ context.Context, _ Config, _ []byte, hash string, publish bool) (Result, error) {
		seen[hash] = true
		if !publish {
			t.Fatal("inbox disabled publication")
		}
		if hash == failedPin {
			return Result{}, errors.New("synthetic hard job failure")
		}
		return Result{RequestSha256: hash, Published: hash == publishedPin}, nil
	}
	if err := commandTestPoll(t, context.Background(), config, io.Discard, &stderr, execute); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || !strings.Contains(stderr.String(), failedPin) {
		t.Fatalf("job failure blocked independent work: seen %v, errors %s", seen, stderr.String())
	}
	for _, pin := range []string{failedPin, unsignedPin} {
		if _, err := os.Stat(filepath.Join(config.InboxDirectory, pin+".json")); err != nil {
			t.Fatalf("unacknowledged request was removed: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(config.InboxDirectory, publishedPin+".json")); !os.IsNotExist(err) {
		t.Fatal("acknowledged independent request was not archived")
	}
}

// Only digest-pinned regular requests can enter execution. Invalid filenames
// are ignored, and mismatched bytes and symlink requests remain unexecuted.
func TestPayoutRosterInboxRejectsUnreviewedNamesBytesAndLinks(t *testing.T) {
	config := commandTestInbox(t)
	commandTestWrite(t, filepath.Join(config.InboxDirectory, "unreviewed.json"), []byte("unreviewed"))
	commandTestWrite(t, filepath.Join(config.InboxDirectory, strings.Repeat("A", 64)+".json"), []byte("uppercase"))
	commandTestWrite(t, filepath.Join(config.InboxDirectory, strings.Repeat("a", 64)+".json"), []byte("mismatched"))
	target := filepath.Join(commandTestDirectory(t), "target.json")
	commandTestWrite(t, target, []byte("external"))
	if err := os.Symlink(target, filepath.Join(config.InboxDirectory, strings.Repeat("b", 64)+".json")); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	execute := func(context.Context, Config, []byte, string, bool) (Result, error) {
		t.Fatal("unreviewed file reached execution")
		return Result{}, nil
	}
	if err := commandTestPoll(t, context.Background(), config, &stdout, &stderr, execute); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "digest") || !strings.Contains(stderr.String(), strings.Repeat("b", 64)) {
		t.Fatalf("unexpected rejected-file reporting: %s, %s", stdout.Bytes(), stderr.Bytes())
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "external" {
		t.Fatal("rejected symlink changed its target")
	}
}

// Cursor progress is the fairness proof: persistent failed jobs in the first
// batch cannot monopolize all future admission slots.
func TestPayoutRosterInboxCursorAdvancesPastFailedBatch(t *testing.T) {
	config := commandTestInbox(t)
	const jobs = maxInboxEntries + 3
	for i := 0; i < jobs; i++ {
		commandTestJob(t, config, []byte(fmt.Sprintf(`{"job":%d}`, i)))
	}
	seen := map[string]bool{}
	execute := func(_ context.Context, _ Config, _ []byte, hash string, _ bool) (Result, error) {
		seen[hash] = true
		return Result{}, errors.New("synthetic persistent failure")
	}
	cursor := &inboxCursor{}
	defer cursor.close()
	if err := processInbox(context.Background(), config, io.Discard, io.Discard, execute, cursor); err != nil {
		t.Fatal(err)
	}
	if len(seen) != maxInboxEntries {
		t.Fatalf("first poll processed %d jobs, want %d", len(seen), maxInboxEntries)
	}
	if err := processInbox(context.Background(), config, io.Discard, io.Discard, execute, cursor); err != nil {
		t.Fatal(err)
	}
	if len(seen) != jobs {
		t.Fatalf("later approved jobs starved behind failed batch: %d of %d", len(seen), jobs)
	}
}

// Several full historical batches retire without a lifetime queue cap, and a
// newly approved request progresses after their completed archives accumulate.
func TestPayoutRosterInboxRetiresMultipleBatchesAndAdmitsNewWork(t *testing.T) {
	config := commandTestInbox(t)
	const jobs = 2*maxInboxEntries + 3
	for i := 0; i < jobs; i++ {
		commandTestJob(t, config, []byte(fmt.Sprintf(`{"job":%d}`, i)))
	}
	seen := map[string]bool{}
	execute := func(_ context.Context, _ Config, _ []byte, hash string, _ bool) (Result, error) {
		seen[hash] = true
		return Result{RequestSha256: hash, Published: true}, nil
	}
	cursor := &inboxCursor{}
	defer cursor.close()
	for poll := 0; poll < 8 && len(seen) < jobs; poll++ {
		before := len(seen)
		if err := processInbox(context.Background(), config, io.Discard, io.Discard, execute, cursor); err != nil {
			t.Fatal(err)
		}
		if len(seen)-before > maxInboxEntries {
			t.Fatal("poll exceeded its bounded batch")
		}
	}
	if len(seen) != jobs {
		t.Fatalf("historical queue stalled: %d of %d", len(seen), jobs)
	}
	newPin := commandTestJob(t, config, []byte(`{"job":"new approval"}`))
	for poll := 0; poll < 4 && !seen[newPin]; poll++ {
		if err := processInbox(context.Background(), config, io.Discard, io.Discard, execute, cursor); err != nil {
			t.Fatal(err)
		}
	}
	if !seen[newPin] {
		t.Fatal("completed histories blocked a new approval")
	}
	entries, err := os.ReadDir(filepath.Join(config.InboxDirectory, "completed"))
	if err != nil || len(entries) != jobs+1 {
		t.Fatalf("completed originals were not preserved: %d, %v", len(entries), err)
	}
}

// A synchronous execution hook replaces the queue name at exactly the race
// boundary. Retirement must preserve and restore the unreviewed replacement.
func TestPayoutRosterInboxRetirementPreservesReplacement(t *testing.T) {
	config := commandTestInbox(t)
	original := []byte(`{"approved":"original"}`)
	replacement := []byte(`{"unreviewed":"replacement"}`)
	pin := commandTestJob(t, config, original)
	path := filepath.Join(config.InboxDirectory, pin+".json")
	retainedOriginal := filepath.Join(config.InboxDirectory, "retained-original")
	var stdout, stderr bytes.Buffer
	execute := func(context.Context, Config, []byte, string, bool) (Result, error) {
		if err := os.Rename(path, retainedOriginal); err != nil {
			t.Fatal(err)
		}
		commandTestWrite(t, path, replacement)
		return Result{RequestSha256: pin, Published: true}, nil
	}
	if err := commandTestPoll(t, context.Background(), config, &stdout, &stderr, execute); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, replacement) {
		t.Fatalf("queue replacement was lost: %q, %v", actual, err)
	}
	actual, err = os.ReadFile(retainedOriginal)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("held original was changed")
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "replacement restored") {
		t.Fatalf("replacement race was not reported: %s, %s", stdout.Bytes(), stderr.Bytes())
	}
}

// In-place mutation retains the inode, so retirement must also compare captured
// bytes rather than accepting identity alone.
func TestPayoutRosterInboxRetirementPreservesInPlaceChange(t *testing.T) {
	config := commandTestInbox(t)
	pin := commandTestJob(t, config, []byte(`{"approved":true}`))
	path := filepath.Join(config.InboxDirectory, pin+".json")
	replacement := []byte(`{"approved":false}`)
	var stderr bytes.Buffer
	execute := func(context.Context, Config, []byte, string, bool) (Result, error) {
		commandTestWrite(t, path, replacement)
		return Result{RequestSha256: pin, Published: true}, nil
	}
	if err := commandTestPoll(t, context.Background(), config, io.Discard, &stderr, execute); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, replacement) || !strings.Contains(stderr.String(), "replacement restored") {
		t.Fatalf("in-place replacement was not preserved: %q, %v, %s", actual, err, stderr.Bytes())
	}
}

// Archive failure after publication leaves the queue retryable. A subsequent
// execution can return its retained acknowledgement and safely finish moving.
func TestPayoutRosterInboxRetriesInterruptedAcknowledgedRetirement(t *testing.T) {
	config := commandTestInbox(t)
	raw := []byte(`{"approved":"request"}`)
	pin := commandTestJob(t, config, raw)
	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	execute := func(_ context.Context, _ Config, received []byte, hash string, _ bool) (Result, error) {
		seen++
		if !bytes.Equal(received, raw) || hash != pin {
			t.Fatal("retry changed acknowledged request")
		}
		if seen == 1 {
			cancel()
		}
		return Result{RequestSha256: hash, Published: true}, nil
	}
	if err := commandTestPoll(t, ctx, config, io.Discard, io.Discard, execute); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted retirement did not return cancellation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.InboxDirectory, pin+".json")); err != nil {
		t.Fatal("interrupted retirement removed the approved request")
	}
	if err := commandTestPoll(t, context.Background(), config, io.Discard, io.Discard, execute); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("acknowledged retry count: got %d", seen)
	}
	if _, err := os.Stat(filepath.Join(config.InboxDirectory, pin+".json")); !os.IsNotExist(err) {
		t.Fatal("acknowledged retry did not finish retirement")
	}
}

// The loop waits after a poll and joins caller cancellation through an explicit
// wait hook, without timer sleeps or a scheduler-dependent negative assertion.
func TestPayoutRosterInboxLoopWaitsAndJoinsCancellation(t *testing.T) {
	config := commandTestInbox(t)
	ctx, cancel := context.WithCancel(context.Background())
	waits := 0
	dependencies := commandDependencies{
		execute: func(context.Context, Config, []byte, string, bool) (Result, error) {
			t.Fatal("empty inbox executed a request")
			return Result{}, nil
		},
		wait: func(owner context.Context, delay time.Duration) error {
			waits++
			if delay != 15*time.Second || owner != ctx {
				t.Fatal("loop changed its poll owner or delay")
			}
			cancel()
			return owner.Err()
		},
	}
	if err := runInbox(ctx, config, io.Discard, io.Discard, dependencies); !errors.Is(err, context.Canceled) || waits != 1 {
		t.Fatalf("loop did not join explicit cancellation: waits %d, %v", waits, err)
	}
}
