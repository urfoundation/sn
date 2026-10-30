//go:build linux || darwin

// A failed steering attempt's cause record reads like the shutdown graph
// without its wrappers, stays one bounded line and never carries credentials.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Error formatting is the only effect a leaf may have; this one panics.
type releaseErrorCausePanickingError struct{}

func (releaseErrorCausePanickingError) Error() string { panic("leaf formatter panicked") }

func releaseErrorCauseArtifactTimeout() error {
	return &url.Error{Op: "Get", URL: "https://api.example/sn/attempt-artifact?hash=0xb203&kind=metadata", Err: context.DeadlineExceeded}
}

// Observed wrappers and joins add no text; repeated leaves count once each.
func TestReleaseErrorCausesCollapseObservedWrappers(t *testing.T) {
	raw := errors.Join(
		fmt.Errorf("read census replica: %w", errors.Join(releaseErrorCauseArtifactTimeout(), releaseErrorCauseArtifactTimeout(), context.Canceled)),
		fmt.Errorf("prepare cut: %w", fmt.Errorf("retry: %w", releaseErrorCauseArtifactTimeout())),
		errors.New("attempt ledger cut is waiting for active trails"),
	)
	causes, truncated := releaseErrorCauses(observeReleaseError(raw))
	if truncated {
		t.Fatal("a small graph was truncated")
	}
	expected := []releaseErrorCause{
		{text: `Get "https://api.example/sn/attempt-artifact?hash=0xb203&kind=metadata": context deadline exceeded`, count: 3},
		{text: "context canceled", count: 1},
		{text: "attempt ledger cut is waiting for active trails", count: 1},
	}
	if !reflect.DeepEqual(causes, expected) {
		t.Fatalf("causes %#v", causes)
	}
	line := string(formatReleaseErrorCauses("hard_error", 3, raw))
	if line != "validator steering causes: code=hard_error attempt=3 leaves=5 distinct=3: "+
		`3x Get "https://api.example/sn/attempt-artifact?hash=0xb203&kind=metadata": context deadline exceeded | 1x context canceled | 1x attempt ledger cut is waiting for active trails`+"\n" {
		t.Fatalf("line %q", line)
	}
	if strings.Contains(line, "release observed") || strings.Contains(line, "release original") {
		t.Fatal("observed wrapper text reached the record", line)
	}
}

// Network and file operations keep their operation; read markers keep only
// their retry verdict, as the shutdown graph prints them.
func TestReleaseErrorCausesKeepOperationContext(t *testing.T) {
	dial := &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 443}, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	file := &os.PathError{Op: "open", Path: "/srv/state/head-ema.json", Err: syscall.EACCES}
	native := &releaseObservedOriginal{original: errors.New("synthetic"), value: &releaseObservedNativeRead{original: errors.New("websocket closed with private detail"), retryable: true}}
	causes, _ := releaseErrorCauses(errors.Join(dial, file, native))
	var texts []string
	for _, cause := range causes {
		texts = append(texts, cause.text)
	}
	expected := []string{"dial tcp 192.0.2.1:443: connection refused", "open /srv/state/head-ema.json: permission denied", "native read failed (retryable=true)"}
	if !reflect.DeepEqual(texts, expected) {
		t.Fatalf("texts %q", texts)
	}
}

// Credentials in URLs, headers and response bodies never reach the record;
// public content hashes and kinds stay readable.
func TestReleaseErrorCausesRedactCredentials(t *testing.T) {
	jwt := "eyJhbGciOiJFUzI1NiJ9.eyJleHAiOjF9.c2lnbmF0dXJl"
	raw := errors.Join(
		&url.Error{Op: "Post", URL: "https://operator:hunter2@api.example/sn/attempt-upload?token=abc.def&hash=0x12&kind=records&jwt=" + jwt, Err: context.DeadlineExceeded},
		errors.New("upstream refused Authorization: Bearer "+jwt),
		errors.New(`unexpected response {"by_jwt":"`+jwt+`"}`),
		errors.New("proxy said bearer s3cr3t-token and api_key=k-123 in ?api_key=k-456&kind=metadata"),
		errors.New("gateway refused Proxy-Authorization: Basic dXNlcjpwYXNzd29yZA== with basic constraints"),
	)
	line := string(formatReleaseErrorCauses("read_wait", 0, raw))
	for _, secret := range []string{"hunter2", "abc.def", "eyJ", "s3cr3t-token", "k-123", "k-456", "dXNlcjpwYXNzd29yZA"} {
		if strings.Contains(line, secret) {
			t.Fatalf("%q reached the record: %s", secret, line)
		}
	}
	for _, public := range []string{"hash=0x12", "kind=records", "kind=metadata", "<redacted", "api.example/sn/attempt-upload", "with basic constraints"} {
		if !strings.Contains(line, public) {
			t.Fatalf("%q missing: %s", public, line)
		}
	}
}

// Depth, node and distinct-leaf bounds stop the walk without failing it.
func TestReleaseErrorCausesBoundTheWalk(t *testing.T) {
	var deep error = errors.New("deep leaf")
	for range releaseErrorCauseMaximumDepth * 2 {
		deep = &releaseObservedCause{cause: deep}
	}
	walk := &releaseErrorCauseWalk{remaining: releaseErrorCauseMaximumNodes, counts: make(map[string]int)}
	walk.visit(deep, 0)
	if !walk.truncated || len(walk.order) != 0 {
		t.Fatalf("depth bound: truncated=%t order=%q", walk.truncated, walk.order)
	}
	wide := make([]error, releaseErrorCauseMaximumNodes*2)
	for index := range wide {
		wide[index] = fmt.Errorf("leaf %d", index)
	}
	walk = &releaseErrorCauseWalk{remaining: releaseErrorCauseMaximumNodes, counts: make(map[string]int)}
	walk.visit(&releaseObservedCauses{causes: wide}, 0)
	if !walk.truncated || len(walk.order) != releaseErrorCauseMaximumDistinct {
		t.Fatalf("width bound: truncated=%t distinct=%d", walk.truncated, len(walk.order))
	}
	// An over-deep raw graph is refused by observation, as shutdown prints it.
	var raw error = errors.New("bottom")
	for index := range 100 {
		raw = fmt.Errorf("level %d: %w", index, raw)
	}
	causes, _ := releaseErrorCauses(raw)
	if len(causes) != 1 || causes[0].text != "release original error graph is incomplete or refused" {
		t.Fatalf("refused graph %#v", causes)
	}
}

// One line of at most 1 KiB, even with many long distinct leaves; control
// characters never split the record.
func TestFormatReleaseErrorCausesIsOneBoundedLine(t *testing.T) {
	leaves := make([]error, 100)
	for index := range leaves {
		leaves[index] = fmt.Errorf("leaf %03d %s\nsecond line", index, strings.Repeat("x", 300))
	}
	root := &releaseObservedOriginal{original: errors.New("synthetic"), value: &releaseObservedCauses{causes: leaves}}
	line := string(formatReleaseErrorCauses("hard_error", 9, root))
	if len(line) > releaseErrorCauseMaximumLineBytes || strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("unbounded line (%d bytes): %q", len(line), line)
	}
	if !strings.Contains(line, "leaves=100 distinct=100") || !strings.Contains(line, " more\n") || !strings.Contains(line, "…") {
		t.Fatalf("bounded line lost its summary: %q", line)
	}
}

// A leaf whose formatter panics replaces the causes with a fixed marker.
func TestFormatReleaseErrorCausesSurvivesAPanickingLeaf(t *testing.T) {
	line := string(formatReleaseErrorCauses("hard_error", 1, errors.Join(errors.New("ordinary"), releaseErrorCausePanickingError{})))
	if line != "validator steering causes: code=hard_error attempt=1 causes unavailable: a leaf could not be formatted\n" {
		t.Fatalf("line %q", line)
	}
}

// The steering loop offers each hard error's and read wait's causes on the
// steering domain right after their scalar events.
func TestProductionSteeringLoopOffersFailureCauses(t *testing.T) {
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 4)}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	err = runReleaseProductionSteeringLoopWithWait(ctx, func() error {
		attempts++
		if attempts == 1 {
			return &productionSteeringReadWait{phase: productionReadPreparation, cause: releaseErrorCauseArtifactTimeout()}
		}
		return errors.Join(errors.New("compact head EMA epoch jumped"), releaseErrorCauseArtifactTimeout())
	}, func() bool {
		// Each attempt's two records drain before the next attempt offers more.
		<-output.written
		<-output.written
		return attempts < 2
	}, nil)
	if err == nil {
		t.Fatal("hard error vanished")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		"code=read_wait",
		"validator steering causes: code=read_wait attempt=0 leaves=1 distinct=1: " + `1x Get "https://api.example/sn/attempt-artifact?hash=0xb203&kind=metadata": context deadline exceeded` + "\n",
		"code=hard_error",
		"validator steering causes: code=hard_error attempt=1 leaves=2 distinct=2: 1x compact head EMA epoch jumped | " + `1x Get "https://api.example/sn/attempt-artifact?hash=0xb203&kind=metadata": context deadline exceeded` + "\n",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
}
