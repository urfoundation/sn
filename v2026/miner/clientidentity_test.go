package miner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

func TestStoredClientIdRoundTrips(t *testing.T) {
	dir := t.TempDir()

	id, err := readStoredClientId(dir)
	if err != nil {
		t.Fatalf("read on empty dir: %s", err)
	}
	if id != nil {
		t.Fatalf("expected no stored id, got %s", id)
	}

	want := connect.NewId()
	if err := writeStoredClientId(dir, want); err != nil {
		t.Fatalf("write: %s", err)
	}

	got, err := readStoredClientId(dir)
	if err != nil {
		t.Fatalf("read after write: %s", err)
	}
	if got == nil {
		t.Fatal("expected a stored id after write, got nil")
	}
	if *got != want {
		t.Errorf("stored id = %s, want %s", got, want)
	}
}

func TestStoredClientIdIgnoresCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "client_id"), []byte("not-a-uuid"), 0600); err != nil {
		t.Fatal(err)
	}

	// a corrupt file must not wedge the provider: it re-auths and overwrites
	// rather than refusing to start
	id, err := readStoredClientId(dir)
	if err != nil {
		t.Fatalf("read corrupt file: %s", err)
	}
	if id != nil {
		t.Errorf("expected nil for a corrupt file, got %s", id)
	}
}

func TestStoredClientIdFileIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	dir := t.TempDir()
	if err := writeStoredClientId(dir, connect.NewId()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "client_id"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		t.Errorf("client_id mode = %o, want no group/other access", perm)
	}
}

func TestClearStoredClientId(t *testing.T) {
	dir := t.TempDir()

	if err := writeStoredClientId(dir, connect.NewId()); err != nil {
		t.Fatalf("write: %s", err)
	}
	if err := clearStoredClientId(dir); err != nil {
		t.Fatalf("clear: %s", err)
	}

	id, err := readStoredClientId(dir)
	if err != nil {
		t.Fatalf("read after clear: %s", err)
	}
	if id != nil {
		t.Errorf("expected no stored id after clear, got %s", id)
	}
}

func TestClearStoredClientIdOnMissingFileIsNotAnError(t *testing.T) {
	// clearing runs on rejection paths where there may be nothing to clear,
	// and it must never be the reason a provider fails to start
	if err := clearStoredClientId(t.TempDir()); err != nil {
		t.Errorf("clear on empty dir: %s", err)
	}
}

func TestShouldRetryWithNewIdentity(t *testing.T) {
	transportErr := errors.New("dial tcp: connection refused")

	tests := []struct {
		name             string
		sentStoredId     bool
		transportErr     error
		resultErrMessage string
		want             bool
	}{
		{
			// the regression: a stored id the server does not know must be
			// dropped and re-minted, not left on disk to crash loop
			name:             "stored id rejected by the server",
			sentStoredId:     true,
			resultErrMessage: "Client does not exist.",
			want:             true,
		},
		{
			// wording is not matched on, only that the server rejected
			name:             "stored id rejected with different wording",
			sentStoredId:     true,
			resultErrMessage: "client_id not found",
			want:             true,
		},
		{
			// the crux: a network blip must NOT churn the identity
			name:         "stored id with a transport error",
			sentStoredId: true,
			transportErr: transportErr,
			want:         false,
		},
		{
			// a transport error wins even if a result error is also reported
			name:             "stored id with both a transport and a result error",
			sentStoredId:     true,
			transportErr:     transportErr,
			resultErrMessage: "Client does not exist.",
			want:             false,
		},
		{
			// nothing left to discard, retrying would repeat the same request
			name:             "no stored id, rejected by the server",
			sentStoredId:     false,
			resultErrMessage: "Client does not exist.",
			want:             false,
		},
		{
			name:         "no stored id, transport error",
			sentStoredId: false,
			transportErr: transportErr,
			want:         false,
		},
		{
			name:         "stored id accepted",
			sentStoredId: true,
			want:         false,
		},
		{
			name:         "no stored id, fresh auth succeeded",
			sentStoredId: false,
			want:         false,
		},
		{
			// a cancelled context arrives on the transport channel too
			name:         "stored id with a cancelled context",
			sentStoredId: true,
			transportErr: context.Canceled,
			want:         false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := shouldRetryWithNewIdentity(test.sentStoredId, test.transportErr, test.resultErrMessage)
			if got != test.want {
				t.Errorf(
					"shouldRetryWithNewIdentity(%t, %v, %q) = %t, want %t",
					test.sentStoredId, test.transportErr, test.resultErrMessage, got, test.want,
				)
			}
		})
	}
}
