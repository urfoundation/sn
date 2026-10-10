package durableinspect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const Schema = "urnetwork-durable-service-inspection-v1"
const MaximumReportBytes = 2 * 1024 * 1024

type Directory struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Uid    uint32 `json:"uid"`
	Gid    uint32 `json:"gid"`
}

// This reports physical read admission only. Runtime independently checks write
// reserve and the application's retained member/intent custody before effects.
type Report struct {
	Schema      string                  `json:"schema"`
	Reference   durablevolume.Reference `json:"reference"`
	Uid         uint32                  `json:"uid"`
	Gid         uint32                  `json:"gid"`
	Directories []Directory             `json:"directories"`
}

type pathsFlag []string

func (self *pathsFlag) String() string { return fmt.Sprint([]string(*self)) }
func (self *pathsFlag) Set(value string) error {
	if len(*self) == 256 {
		return errors.New("storage inspection has too many directories")
	}
	*self = append(*self, value)
	return nil
}

// The command accepts only an exact declaration and explicit directory paths.
// There is no config loading, RPC route, signer, publisher or service action.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("storage-inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var path, digest string
	flags.Func("durable-volumes", "exact daemon volume declaration", func(value string) error {
		if path != "" || value == "" {
			return errors.New("storage declaration is empty or repeated")
		}
		path = value
		return nil
	})
	flags.Func("durable-volumes-sha256", "exact declaration hash", func(value string) error {
		if digest != "" || value == "" {
			return errors.New("storage declaration hash is empty or repeated")
		}
		digest = value
		return nil
	})
	var paths pathsFlag
	flags.Var(&paths, "directory", "one explicit existing service directory; repeatable")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || (path == "") != (digest == "") {
		fmt.Fprintln(stderr, "storage inspection requires an exact declaration and explicit directory arguments")
		return 2
	}
	reference := durablevolume.Reference{Path: path, Sha256: digest}
	if retained, present := durablevolume.ReferenceFromContext(ctx); present {
		if reference.Path != "" && reference != retained {
			fmt.Fprintln(stderr, "storage inspection declaration differs from its caller")
			return 2
		}
		reference = retained
	}
	if reference.Path == "" || reference.Sha256 == "" {
		fmt.Fprintln(stderr, "storage inspection requires an explicit declaration and hash")
		return 2
	}
	report, err := Inspect(ctx, reference, paths)
	if err != nil {
		fmt.Fprintln(stderr, "storage inspection:", err)
		if errors.Is(err, durablevolume.ErrIdentity) {
			return 3
		}
		if errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrBusy) {
			return 4
		}
		return 1
	}
	raw, err := json.Marshal(report)
	if err := errors.Join(err, ctx.Err()); err != nil {
		fmt.Fprintln(stderr, "storage inspection:", err)
		return 1
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "storage inspection output:", errors.Join(err, io.ErrShortWrite))
		return 1
	}
	return 0
}

// The root installer checks the exact child request and actual credentials;
// only the independently pinned executable supplies the physical observation.
func Validate(raw []byte, reference durablevolume.Reference, uid, gid uint32, paths []string) error {
	ordered, err := orderedPaths(paths)
	if err != nil || len(raw) == 0 || len(raw) > MaximumReportBytes {
		return errors.Join(errors.New("service storage inspection report is invalid"), err)
	}
	var report Report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("service storage inspection report has trailing data")
	}
	if report.Schema != Schema || report.Reference != reference || report.Uid != uid || report.Gid != gid || len(report.Directories) != len(ordered) {
		return errors.New("service storage inspection identity differs from the approved request")
	}
	for index, directory := range report.Directories {
		if directory.Path != ordered[index] || directory.Inode == 0 {
			return errors.New("service storage inspection directory differs")
		}
	}
	return nil
}
