//go:build linux || darwin

package durableinspect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

func TestInspectionReadOnlyRefusesMissingAndReplacedRoots(t *testing.T) {
	root := t.TempDir()
	fixture := durablefixture.New(t, t.Context(), root)
	if report, err := Inspect(fixture.Context, fixture.Reference, []string{root}); err != nil || len(report.Directories) != 1 {
		t.Fatal(report, err)
	}
	fixture.Host.SetReserve(0, 0)
	if _, err := Inspect(fixture.Context, fixture.Reference, []string{root}); err != nil {
		t.Fatal("read-only inspection required write capacity", err)
	}
	if _, err := Inspect(fixture.Context, fixture.Reference, []string{filepath.Join(root, "absent")}); err == nil {
		t.Fatal("absent service directory was admitted")
	}
	if _, err := os.Stat(filepath.Join(root, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created an absent directory", err)
	}
	if err := os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(root+"-retained", root) })
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(fixture.Context, fixture.Reference, []string{root}); err == nil {
		t.Fatal("replacement root was admitted")
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
}

func TestInspectionCommandBindsInputsAndJoinsBeforeOutput(t *testing.T) {
	root := t.TempDir()
	fixture := durablefixture.New(t, t.Context(), root)
	args := []string{"--durable-volumes", fixture.Reference.Path, "--durable-volumes-sha256", fixture.Reference.Sha256, "--directory", root}
	var stdout, stderr bytes.Buffer
	if code := Run(fixture.Context, args, &stdout, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if err := Validate(stdout.Bytes(), fixture.Reference, uint32(os.Geteuid()), uint32(os.Getegid()), []string{root}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(stdout.Bytes(), fixture.Reference, uint32(os.Geteuid())+1, uint32(os.Getegid()), []string{root}); err == nil {
		t.Fatal("report accepted another execution credential")
	}
	for _, invalid := range [][]string{nil, {"--directory", root}, append(append([]string{}, args...), "--durable-volumes", fixture.Reference.Path), append(append([]string{}, args...), "--directory", root)} {
		stdout.Reset()
		stderr.Reset()
		if code := Run(context.Background(), invalid, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatal("ambiguous or missing input emitted an admitted report", code, stdout.String())
		}
	}
	canceled, cancel := context.WithCancel(fixture.Context)
	cancel()
	stdout.Reset()
	if code := Run(canceled, args, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
		t.Fatal("canceled inspection emitted a report", code)
	}
	if code := Run(fixture.Context, args, inspectionShortWriter{}, &stderr); code == 0 {
		t.Fatal("short output was acknowledged")
	}
	if _, err := Inspect(fixture.Context, fixture.Reference, []string{root}); err != nil {
		t.Fatal("short output left an unjoined directory owner", err)
	}
}

type inspectionShortWriter struct{}

func (inspectionShortWriter) Write(raw []byte) (int, error) { return len(raw) / 2, io.ErrShortWrite }
