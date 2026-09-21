// Strict, portable inputs and source custody for offline qualification.
package main

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
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const metadataLimit = 1024 * 1024
const sourceManifestLimit = 16 * metadataLimit
const capturedJSONLimit = 64 * metadataLimit
const regularFileLimit = 1024 * metadataLimit

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var rootPattern = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
var manifestPattern = regexp.MustCompile(`^([0-9a-f]{64})  (.+)$`)

type sourceSpec struct {
	Root     string `json:"root"`
	Manifest string `json:"manifest"`
}
type limitsSpec struct {
	Jobs         int `json:"jobs"`
	BuildSeconds int `json:"build_seconds"`
	TestSeconds  int `json:"test_seconds"`
	OuterSeconds int `json:"outer_seconds"`
	Parallel     int `json:"parallel"`
	GOMAXPROCS   int `json:"gomaxprocs"`
}
type packageSpec struct {
	Id         string `json:"id"`
	Directory  string `json:"directory"`
	ImportPath string `json:"import_path"`
}
type suiteSpec struct {
	Id              string `json:"id"`
	Package         string `json:"package"`
	Mode            string `json:"mode"`
	Outcomes        string `json:"outcomes"`
	FailureLiterals string `json:"failure_literals"`
}
type planSpec struct {
	Version    int           `json:"version"`
	SourceRoot string        `json:"source_root"`
	Sources    []sourceSpec  `json:"sources"`
	Limits     limitsSpec    `json:"limits"`
	Packages   []packageSpec `json:"packages"`
	Suites     []suiteSpec   `json:"suites"`
}
type expectedSuite struct {
	Roots    []string
	Outcomes map[string]string
	Markers  map[string]string
}
type fileProof struct {
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}
type sourceProof struct {
	Root           string               `json:"root"`
	ManifestSHA256 string               `json:"manifest_sha256"`
	Head           string               `json:"head"`
	StatusHex      string               `json:"status_hex"`
	Files          map[string]fileProof `json:"files"`
}

// JSON duplicate keys must not silently replace routing or ownership inputs.
func uniqueJSON(decoder *json.Decoder) error {
	return uniqueJSONDepth(decoder, 0)
}

// Plan/event nesting is finite independently of the enclosing byte ceiling.
func uniqueJSONDepth(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeds bound")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return errors.New("unexpected JSON delimiter")
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate or invalid JSON key")
			}
			seen[name] = true
		}
		if err := uniqueJSONDepth(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func decodeJSON(data []byte, value any) error {
	if len(data) > metadataLimit || !utf8.Valid(data) {
		return errors.New("JSON metadata exceeds bound or is not UTF-8")
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueJSON(tokens); err != nil {
		return err
	}
	if _, err := tokens.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	destination := reflect.ValueOf(value)
	if destination.Kind() != reflect.Pointer || destination.IsNil() {
		return errors.New("JSON destination must be a nonnil pointer")
	}
	if err := jsonShape(data, destination.Type().Elem()); err != nil {
		return err
	}
	// A reused destination cannot retain authority from an earlier document when
	// a field is omitted, and a rejected document cannot partially replace it.
	fresh := reflect.New(destination.Type().Elem())
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(fresh.Interface()); err != nil {
		return err
	}
	destination.Elem().Set(fresh.Elem())
	return nil
}

// Require exact tagged field spelling and refuse null scalar/struct values.
// Nullable pointers and collections remain available for captured result types.
func jsonShape(data []byte, shape reflect.Type) error {
	if shape == reflect.TypeFor[json.RawMessage]() || shape.Kind() == reflect.Interface {
		return nil
	}
	isNull := bytes.Equal(bytes.TrimSpace(data), []byte("null"))
	if shape.Kind() == reflect.Pointer {
		if isNull {
			return nil
		}
		return jsonShape(data, shape.Elem())
	}
	switch shape.Kind() {
	case reflect.Struct:
		if isNull {
			return errors.New("null JSON object")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		allowed := map[string]reflect.Type{}
		for index := 0; index < shape.NumField(); index++ {
			field := shape.Field(index)
			if !field.IsExported() {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			allowed[name] = field.Type
		}
		for name, raw := range fields {
			field, ok := allowed[name]
			if !ok {
				return fmt.Errorf("unknown JSON field %q", name)
			}
			if err := jsonShape(raw, field); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	case reflect.Map:
		if isNull {
			return nil
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		for _, raw := range fields {
			if err := jsonShape(raw, shape.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if isNull {
			return nil
		}
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, raw := range values {
			if err := jsonShape(raw, shape.Elem()); err != nil {
				return err
			}
		}
	default:
		if isNull {
			return errors.New("null JSON scalar")
		}
	}
	return nil
}

// Open a physical regular file without blocking on a substituted FIFO/device.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	if err := physicalPath(path, false); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() < 0 || info.Size() > regularFileLimit) {
		err = errors.New("regular file exceeds byte bound or changed type")
	}
	if err == nil {
		err = sameRegular(path, file, info)
	}
	if err != nil {
		return nil, nil, errors.Join(err, file.Close())
	}
	return file, info, nil
}

// The path and retained descriptor must still denote the same unchanged file.
func sameRegular(path string, file *os.File, before os.FileInfo) error {
	if err := physicalPath(path, false); err != nil {
		return err
	}
	current, err := file.Stat()
	if err != nil {
		return err
	}
	named, err := os.Stat(path)
	if err != nil {
		return err
	}
	for _, info := range []os.FileInfo{current, named} {
		if !os.SameFile(before, info) || !info.Mode().IsRegular() || info.Size() != before.Size() || info.Mode() != before.Mode() || !info.ModTime().Equal(before.ModTime()) {
			return errors.New("regular file changed during read")
		}
	}
	return nil
}

func readBounded(path string, maximum int64) ([]byte, error) {
	if maximum < 0 || maximum > regularFileLimit {
		return nil, errors.New("invalid file byte bound")
	}
	file, info, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maximum {
		return nil, errors.Join(errors.New("file exceeds declared bound"), file.Close())
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err == nil && int64(len(data)) != info.Size() {
		err = errors.New("file changed size during read")
	}
	return data, errors.Join(err, sameRegular(path, file, info), file.Close())
}

func readJSON(path string, value any) error {
	data, err := readBounded(path, metadataLimit)
	if err != nil {
		return err
	}
	return decodeJSON(data, value)
}

func fileHash(path string) (string, error) {
	proof, err := regularProof(path)
	return proof.SHA256, err
}

// Bind mode and content to one bounded retained file, rather than separate opens.
func regularProof(path string) (fileProof, error) {
	file, info, err := openRegular(path)
	if err != nil {
		return fileProof{}, err
	}
	hash := sha256.New()
	count, readErr := io.Copy(hash, io.LimitReader(file, info.Size()+1))
	if count != info.Size() {
		readErr = errors.Join(readErr, errors.New("file changed size during hash"))
	}
	if err := errors.Join(readErr, sameRegular(path, file, info), file.Close()); err != nil {
		return fileProof{}, err
	}
	return fileProof{SHA256: hex.EncodeToString(hash.Sum(nil)), Mode: uint32(info.Mode().Perm())}, nil
}

func physicalPath(path string, directory bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\r\n") {
		return errors.New("path must be absolute and canonical")
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if actual != path {
		return errors.New("path must be physical, not a symlink alias")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if directory != info.IsDir() || (!directory && !info.Mode().IsRegular()) {
		return errors.New("wrong path type")
	}
	return nil
}

func inside(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func metadataRows(path string, empty bool) ([]string, error) {
	data, err := readBounded(path, metadataLimit)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 && empty {
		return nil, nil
	}
	if len(data) == 0 || data[len(data)-1] != '\n' || bytes.ContainsAny(data, "\r\x00") || !utf8.Valid(data) {
		return nil, errors.New("metadata must have canonical UTF-8 newline-terminated rows")
	}
	rows := strings.Split(string(data[:len(data)-1]), "\n")
	for _, row := range rows {
		if row == "" {
			return nil, errors.New("blank metadata row")
		}
	}
	return rows, nil
}

func expectedInputs(outcomes, markers string) (expectedSuite, error) {
	result := expectedSuite{Outcomes: map[string]string{}, Markers: map[string]string{}}
	rows, err := metadataRows(outcomes, false)
	if err != nil {
		return result, err
	}
	previous := ""
	for _, row := range rows {
		fields := strings.Split(row, "\t")
		if len(fields) != 2 || !validTestIdentity(fields[0]) || fields[0] <= previous || (fields[1] != "PASS" && fields[1] != "FAIL") {
			return result, errors.New("invalid, duplicate or unsorted expected root")
		}
		previous = fields[0]
		if !strings.Contains(fields[0], "/") {
			result.Roots = append(result.Roots, fields[0])
		}
		result.Outcomes[fields[0]] = strings.ToLower(fields[1])
	}
	rows, err = metadataRows(markers, true)
	if err != nil {
		return result, err
	}
	previous = ""
	for _, row := range rows {
		fields := strings.Split(row, "\t")
		if len(fields) != 2 || fields[0] <= previous || result.Outcomes[fields[0]] != "fail" || len(fields[1]) == 0 || len(fields[1]) > 4096 {
			return result, errors.New("invalid failure literal")
		}
		for _, r := range fields[1] {
			if r < 32 {
				return result, errors.New("control character in failure literal")
			}
		}
		previous = fields[0]
		result.Markers[fields[0]] = fields[1]
	}
	for root, outcome := range result.Outcomes {
		if outcome == "fail" && result.Markers[root] == "" {
			return result, errors.New("expected failure has no owning literal")
		}
	}
	if _, err := expectedParents(result); err != nil {
		return result, err
	}
	return result, nil
}

// Keep the legacy root grammar. Go testing rewrites child whitespace and
// nonprintable runes; emitted punctuation and Unicode remain literal identities.
func validTestIdentity(name string) bool {
	if !strings.Contains(name, "/") {
		return rootPattern.MatchString(name)
	}
	if len(name) > 4096 || strings.Count(name, "/") > 64 || !utf8.ValidString(name) {
		return false
	}
	components := strings.Split(name, "/")
	if !rootPattern.MatchString(components[0]) {
		return false
	}
	for _, component := range components[1:] {
		if component == "" {
			return false
		}
		for _, r := range component {
			if r == ' ' || !strconv.IsPrint(r) {
				return false
			}
		}
	}
	// No path cleaning, pattern matching or implicit descendant admission.
	return true
}

// Derive a complete hierarchy from declarations, never from observed events.
// A failing child must also declare each failing ancestor and its own literal.
func expectedParents(expected expectedSuite) (map[string]string, error) {
	if len(expected.Outcomes) == 0 || len(expected.Outcomes) > 256*1024 {
		return nil, errors.New("expected test identity census is empty or exceeds bound")
	}
	parents := map[string]string{}
	var roots []string
	for name, outcome := range expected.Outcomes {
		if !validTestIdentity(name) || outcome != "pass" && outcome != "fail" {
			return nil, errors.New("invalid declared test identity or outcome")
		}
		if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
			parent := name[:slash]
			if expected.Outcomes[parent] == "" {
				return nil, errors.New("declared descendant has no declared parent")
			}
			if outcome == "fail" && expected.Outcomes[parent] != "fail" {
				return nil, errors.New("failing descendant has a non-failing parent")
			}
			parents[name] = parent
		} else {
			roots = append(roots, name)
		}
		if outcome == "fail" && expected.Markers[name] == "" {
			return nil, errors.New("expected failure has no owning literal")
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 || !reflect.DeepEqual(roots, expected.Roots) {
		return nil, errors.New("declared top-level root census differs")
	}
	for name, literal := range expected.Markers {
		if expected.Outcomes[name] != "fail" || len(literal) == 0 || len(literal) > 4096 || !utf8.ValidString(literal) {
			return nil, errors.New("invalid identity-owned failure literal")
		}
		for _, r := range literal {
			if r < 32 {
				return nil, errors.New("control character in failure literal")
			}
		}
	}
	return parents, nil
}

func validatePlan(plan planSpec) error {
	if plan.Version != 1 || len(plan.Sources) == 0 || len(plan.Sources) > 256 || len(plan.Packages) == 0 || len(plan.Packages) > 256 || len(plan.Suites) == 0 || len(plan.Suites) > 1024 {
		return errors.New("invalid version or matrix cardinality")
	}
	if err := physicalPath(plan.SourceRoot, true); err != nil {
		return err
	}
	for _, value := range []int{plan.Limits.Jobs, plan.Limits.BuildSeconds, plan.Limits.TestSeconds, plan.Limits.OuterSeconds, plan.Limits.Parallel, plan.Limits.GOMAXPROCS} {
		if value < 1 || value > 86400 {
			return errors.New("invalid positive limit")
		}
	}
	if plan.Limits.Jobs > 256 || plan.Limits.OuterSeconds <= plan.Limits.TestSeconds {
		return errors.New("invalid concurrency or nested timeout limits")
	}
	roots := map[string]bool{}
	for _, source := range plan.Sources {
		if err := physicalPath(source.Root, true); err != nil {
			return err
		}
		if err := physicalPath(source.Manifest, false); err != nil {
			return err
		}
		if roots[source.Root] {
			return errors.New("duplicate source root")
		}
		roots[source.Root] = true
	}
	if !roots[plan.SourceRoot] {
		return errors.New("main source has no manifest")
	}
	packages := map[string]packageSpec{}
	for _, item := range plan.Packages {
		if !namePattern.MatchString(item.Id) || packages[item.Id].Id != "" {
			return errors.New("invalid or duplicate package id")
		}
		if err := physicalPath(item.Directory, true); err != nil {
			return err
		}
		owned := false
		for root := range roots {
			owned = owned || inside(item.Directory, root)
		}
		if !owned || !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(item.ImportPath) || strings.HasPrefix(item.ImportPath, "/") || filepath.Clean(item.ImportPath) != item.ImportPath || item.ImportPath == "." || item.ImportPath == ".." || strings.HasPrefix(item.ImportPath, "../") {
			return errors.New("package owner or import path is invalid")
		}
		for _, previous := range packages {
			if previous.Directory == item.Directory || previous.ImportPath == item.ImportPath {
				return errors.New("package aliases would duplicate a physical package census")
			}
		}
		packages[item.Id] = item
	}
	suites := map[string]bool{}
	memberships := map[string]map[string]bool{}
	for _, item := range plan.Suites {
		if !namePattern.MatchString(item.Id) || suites[item.Id] || packages[item.Package].Id == "" || (item.Mode != "normal" && item.Mode != "race") {
			return errors.New("invalid or duplicate suite identity")
		}
		suites[item.Id] = true
		if err := physicalPath(item.Outcomes, false); err != nil {
			return err
		}
		if err := physicalPath(item.FailureLiterals, false); err != nil {
			return err
		}
		expected, err := expectedInputs(item.Outcomes, item.FailureLiterals)
		if err != nil {
			return err
		}
		group := item.Package + "/" + item.Mode
		if memberships[group] == nil {
			memberships[group] = map[string]bool{}
		}
		for _, root := range expected.Roots {
			if memberships[group][root] {
				return errors.New("duplicate roots in one package/mode; use an obligation map")
			}
			memberships[group][root] = true
		}
	}
	return nil
}

func sourceFence(sources []sourceSpec) ([]sourceProof, error) {
	return sourceFenceContext(context.Background(), sources)
}

// Git discovery is read-only, bounded, and cannot invoke configured diff or
// filesystem-monitor programs outside the retained stage-owner boundary.
func sourceFenceContext(ctx context.Context, sources []sourceSpec) ([]sourceProof, error) {
	result := []sourceProof{}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := physicalPath(source.Root, true); err != nil {
			return nil, err
		}
		data, err := readBounded(source.Manifest, sourceManifestLimit)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 || data[len(data)-1] != '\n' || !utf8.Valid(data) || bytes.ContainsAny(data, "\r\x00") {
			return nil, errors.New("empty or noncanonical source manifest")
		}
		proof := sourceProof{Root: source.Root, Files: map[string]fileProof{}}
		manifestHash := sha256.Sum256(data)
		proof.ManifestSHA256 = hex.EncodeToString(manifestHash[:])
		for _, row := range strings.Split(string(data[:len(data)-1]), "\n") {
			fields := manifestPattern.FindStringSubmatch(row)
			if fields == nil {
				return nil, errors.New("invalid source manifest row")
			}
			relative := fields[2]
			path := filepath.Join(source.Root, relative)
			if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || !inside(path, source.Root) || proof.Files[relative].SHA256 != "" {
				return nil, errors.New("noncanonical or duplicate manifest path")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			actual, err := regularProof(path)
			if err != nil {
				return nil, err
			}
			if actual.SHA256 != fields[1] {
				return nil, fmt.Errorf("source hash mismatch: %s", path)
			}
			proof.Files[relative] = actual
		}
		git := func(args ...string) ([]byte, error) {
			commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			command := exec.CommandContext(commandCtx, "git", append([]string{"--no-pager", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", source.Root}, args...)...)
			for _, pair := range os.Environ() {
				if !strings.HasPrefix(pair, "GIT_") {
					command.Env = append(command.Env, pair)
				}
			}
			command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
			output := &boundedOutput{Maximum: sourceManifestLimit}
			command.Stdout, command.Stderr = output, io.Discard
			command.WaitDelay = 10 * time.Second
			err := command.Run()
			if err != nil {
				return nil, errors.Join(err, commandCtx.Err())
			}
			return output.Bytes(), nil
		}
		top, err := git("rev-parse", "--show-toplevel")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(top)) != source.Root {
			return nil, errors.New("source root is not its Git worktree root")
		}
		head, err := git("rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		proof.Head = strings.TrimSpace(string(head))
		status, err := git("status", "--porcelain=v1", "-z")
		if err != nil {
			return nil, err
		}
		proof.StatusHex = hex.EncodeToString(status)
		for _, args := range [][]string{{"diff", "--no-ext-diff", "--no-textconv", "--name-only", "--diff-filter=d", "-z", "HEAD"}, {"ls-files", "--others", "--exclude-standard", "-z"}} {
			changed, err := git(args...)
			if err != nil {
				return nil, err
			}
			for _, path := range strings.Split(string(changed), "\x00") {
				if path != "" && proof.Files[path].SHA256 == "" {
					return nil, fmt.Errorf("dirty/new source missing from manifest: %s/%s", source.Root, path)
				}
			}
		}
		result = append(result, proof)
	}
	return result, nil
}

// Bound command metadata without retaining an unbounded Output allocation.
type boundedOutput struct {
	buffer  bytes.Buffer
	Maximum int
	err     error
}

func (self *boundedOutput) Write(data []byte) (int, error) {
	if self.err != nil {
		return 0, self.err
	}
	if len(data) > self.Maximum-self.buffer.Len() {
		self.err = errors.New("command metadata exceeds byte bound")
		return 0, self.err
	}
	return self.buffer.Write(data)
}

// Do not embed bytes.Buffer: its promoted ReadFrom bypasses a capped Write.
func (self *boundedOutput) Bytes() []byte { return self.buffer.Bytes() }

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > capturedJSONLimit {
		return errors.New("captured JSON exceeds byte bound")
	}
	temporary := path + ".new"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	count, writeErr := file.Write(data)
	if count != len(data) {
		writeErr = errors.Join(writeErr, io.ErrShortWrite)
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func sortedKeys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for name := range values {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Go reports a replacement's lexical directory even when that directory is a
// workspace symlink. Only this observed route may resolve; configured roots
// and package directories retain their strict physical-path contract.
type moduleSourceProof struct {
	Module            string    `json:"module"`
	Root              string    `json:"root"`
	Directory         string    `json:"directory"`
	PhysicalDirectory string    `json:"physical_directory"`
	GoMod             string    `json:"go_mod"`
	GoModFile         fileProof `json:"go_mod_file"`
}

// A local module route must terminate inside one explicitly declared physical
// source owner. A go.mod symlink cannot redirect that authority elsewhere.
func captureModuleSource(module, directory, goMod string, replacement bool, sources []sourceSpec) (moduleSourceProof, error) {
	zero := moduleSourceProof{}
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || strings.ContainsAny(directory, "\x00\r\n") {
		return zero, errors.New("reported module directory is not absolute and canonical")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return zero, err
	}
	if !replacement && resolved != directory {
		return zero, errors.New("main module directory is not physical")
	}
	if err := physicalPath(resolved, true); err != nil {
		return zero, err
	}
	root := ""
	for _, source := range sources {
		if err := physicalPath(source.Root, true); err != nil {
			return zero, err
		}
		if inside(resolved, source.Root) && len(source.Root) > len(root) {
			root = source.Root
		}
	}
	if root == "" {
		return zero, fmt.Errorf("local module outside declared source roots: %s", module)
	}
	if goMod != filepath.Join(directory, "go.mod") {
		return zero, errors.New("local module metadata path differs")
	}
	resolvedMod, err := filepath.EvalSymlinks(goMod)
	if err != nil {
		return zero, err
	}
	if resolvedMod != filepath.Join(resolved, "go.mod") {
		return zero, errors.New("local module metadata redirects outside its resolved directory")
	}
	if err := physicalPath(resolvedMod, false); err != nil {
		return zero, err
	}
	proof, err := regularProof(resolvedMod)
	if err != nil {
		return zero, err
	}
	return moduleSourceProof{Module: module, Root: root, Directory: directory, PhysicalDirectory: resolved, GoMod: goMod, GoModFile: proof}, nil
}

// Recheck the admitted route and metadata before and after compilation/test
// execution. Another declared root is still a different admitted module.
func checkModuleSourceProofs(proofs []moduleSourceProof) error {
	for _, proof := range proofs {
		actual, err := captureModuleSource(proof.Module, proof.Directory, proof.GoMod, true, []sourceSpec{{Root: proof.Root}})
		if err != nil {
			return fmt.Errorf("local module route changed for %s: %w", proof.Module, err)
		}
		if actual != proof {
			return fmt.Errorf("local module route or metadata changed for %s", proof.Module)
		}
	}
	return nil
}

// Capture every local source from the actual bounded Go module graph.
func captureModuleSources(data []byte, sources []sourceSpec) ([]moduleSourceProof, error) {
	proofs := []moduleSourceProof{}
	err := inspectModuleSources(data, sources, func(proof moduleSourceProof) { proofs = append(proofs, proof) })
	if err != nil {
		return nil, err
	}
	sort.Slice(proofs, func(first, second int) bool { return proofs[first].Module < proofs[second].Module })
	if err := checkModuleSourceProofs(proofs); err != nil {
		return nil, err
	}
	return proofs, nil
}

// Retain the existing read-only verifier interface for preflight callers.
func verifyModuleSources(data []byte, sources []sourceSpec) error {
	_, err := captureModuleSources(data, sources)
	return err
}

// The module graph may contain harmless additional Go-version fields, but
// every local replacement used by this build must name declared source.
func inspectModuleSources(data []byte, sources []sourceSpec, accept func(moduleSourceProof)) error {
	if len(data) == 0 || len(data) > sourceManifestLimit {
		return errors.New("module graph exceeds byte bound or is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	paths := map[string]bool{}
	mainCount := 0
	for count := 0; ; count++ {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if count >= 16384 {
			return errors.New("module graph exceeds item bound")
		}
		var fields map[string]json.RawMessage
		if err := decodeJSON(raw, &fields); err != nil {
			return err
		}
		if fields == nil {
			return errors.New("null module object")
		}
		read := func(fields map[string]json.RawMessage, name string) (string, error) {
			raw, ok := fields[name]
			if !ok {
				return "", nil
			}
			var value *string
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", err
			}
			if value == nil {
				return "", errors.New("null module identity")
			}
			return *value, nil
		}
		path, err := read(fields, "Path")
		if err != nil || path == "" || paths[path] {
			return errors.New("missing, duplicate or invalid module identity")
		}
		paths[path] = true
		local, mainModule, localReplacement := false, false, false
		if main, ok := fields["Main"]; ok {
			var value *bool
			if err := json.Unmarshal(main, &value); err != nil || value == nil {
				return errors.New("invalid main module flag")
			}
			local = *value
			mainModule = *value
			if local {
				mainCount++
			}
		}
		if replacement, ok := fields["Replace"]; ok {
			if mainModule {
				return errors.New("main source module unexpectedly has a replacement")
			}
			var replaced map[string]json.RawMessage
			if err := decodeJSON(replacement, &replaced); err != nil || replaced == nil {
				return errors.New("invalid module replacement")
			}
			version, err := read(replaced, "Version")
			if err != nil {
				return err
			}
			if version == "" {
				replacementPath, pathErr := read(replaced, "Path")
				if pathErr != nil || replacementPath == "" || strings.ContainsAny(replacementPath, "\x00\r\n") {
					return errors.New("local replacement path is absent or invalid")
				}
				for _, name := range []string{"Dir", "GoMod"} {
					outer, outerErr := read(fields, name)
					inner, innerErr := read(replaced, name)
					if outerErr != nil || innerErr != nil || outer != "" && outer != inner {
						return errors.New("local replacement competes with outer module metadata")
					}
				}
				local = true
				localReplacement = true
				fields = replaced
			}
		}
		if !local {
			version, err := read(fields, "Version")
			if err != nil || version == "" {
				return errors.New("non-main module lacks a version or explicit local replacement")
			}
			directory, err := read(fields, "Dir")
			if err != nil {
				return err
			}
			if directory != "" {
				resolved, resolveErr := filepath.EvalSymlinks(directory)
				if resolveErr == nil {
					for _, source := range sources {
						if inside(resolved, source.Root) {
							return errors.New("declared source module lacks an explicit local replacement")
						}
					}
				}
			}
			continue
		}
		directory, err := read(fields, "Dir")
		if err != nil {
			return err
		}
		goMod, err := read(fields, "GoMod")
		if err != nil {
			return err
		}
		proof, err := captureModuleSource(path, directory, goMod, localReplacement, sources)
		if err != nil {
			return fmt.Errorf("local module %s: %w", path, err)
		}
		accept(proof)
	}
	if len(paths) == 0 || mainCount != 1 {
		return errors.New("module graph requires exactly one main source module")
	}
	return nil
}

// Resolve an executable once, then fence that physical path and its bytes.
func executablePath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if err := physicalPath(path, false); err != nil {
		return "", err
	}
	return path, nil
}
