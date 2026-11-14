// Apply bounded OCI layer changes as data. Parent links, ambiguous paths and
// unsupported inode metadata fail closed instead of approximating extraction.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

const maximumPackageLayerBytes int64 = 768 * 1024 * 1024
const maximumPackageRootEntries = 100000

// Content hashes and inode metadata cover every final path; only dpkg status
// needs retained bytes. No symlink or archive path is resolved on the host.
type packageRootEntry struct {
	Kind   byte   `json:"kind"`
	Mode   int64  `json:"mode"`
	Uid    int    `json:"uid"`
	Gid    int    `json:"gid"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
	Raw    []byte `json:"-"`
}

// Accept conventional tar directory spelling; reject all other path aliases.
func packageTarPath(name string, directory bool) (string, error) {
	if directory {
		name = strings.TrimSuffix(name, "/")
	}
	if name == "." && directory {
		return name, nil
	}
	if name == "" || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\r\n") || len(name) > 4096 {
		return "", errors.New("noncanonical rootfs path")
	}
	return name, nil
}

// Read all whiteouts before merging ordinary entries, so layer record order
// cannot change the OCI rule that deletions affect lower layers only.
func applyPackageLayer(compressed io.Reader, diffId string, filesystem map[string]packageRootEntry) error {
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	hash := sha256.New()
	limited := &io.LimitedReader{R: gzipReader, N: maximumPackageLayerBytes + 1}
	stream := io.TeeReader(limited, hash)
	reader := tar.NewReader(stream)
	entries := map[string]packageRootEntry{}
	whiteouts := map[string]bool{}
	seen := map[string]bool{}
	order := []string{}
	for count := 0; ; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if count >= maximumPackageRootEntries || header.Size < 0 || header.Size > maximumPackageLayerBytes || header.Mode < 0 || header.Mode > 07777 || header.Uid < 0 || header.Gid < 0 || len(header.Xattrs) != 0 {
			return errors.New("rootfs entry count, size or inode metadata bound")
		}
		// Unicode certificate paths need pax path/linkpath records. archive/tar
		// resolves these into Name/Linkname; all other extended metadata is refused.
		for key, value := range header.PAXRecords {
			if key == "path" && value == header.Name || key == "linkpath" && value == header.Linkname {
				continue
			}
			return errors.New("unsupported rootfs extended metadata")
		}
		name, err := packageTarPath(header.Name, header.Typeflag == tar.TypeDir)
		if err != nil {
			return err
		}
		if seen[name] {
			return errors.New("duplicate rootfs path in layer")
		}
		seen[name] = true
		if strings.HasPrefix(path.Base(name), ".wh.") {
			if header.Typeflag != tar.TypeReg || header.Size != 0 || header.Linkname != "" {
				return errors.New("malformed rootfs whiteout")
			}
			target := strings.TrimPrefix(path.Base(name), ".wh.")
			if target == "" || target == "." || target == ".." {
				return errors.New("malformed whiteout target")
			}
			whiteouts[name] = true
			continue
		}
		entry := packageRootEntry{Kind: header.Typeflag, Mode: header.Mode, Uid: header.Uid, Gid: header.Gid, Size: header.Size, Link: header.Linkname}
		switch header.Typeflag {
		case tar.TypeReg:
			if header.Linkname != "" {
				return errors.New("regular rootfs entry has link")
			}
			hash := sha256.New()
			var raw strings.Builder
			writer := io.Writer(hash)
			if name == "var/lib/dpkg/status" {
				if header.Size > 4*1024*1024 {
					return errors.New("package database exceeds bound")
				}
				writer = io.MultiWriter(hash, &raw)
			}
			n, err := io.Copy(writer, reader)
			if err != nil || n != header.Size {
				return errors.New("truncated rootfs file")
			}
			entry.Sha256 = "sha256:" + hex.EncodeToString(hash.Sum(nil))
			if name == "var/lib/dpkg/status" {
				entry.Raw = []byte(raw.String())
			}
		case tar.TypeDir:
			if header.Size != 0 || header.Linkname != "" {
				return errors.New("directory metadata differs")
			}
		case tar.TypeSymlink:
			if header.Size != 0 || header.Linkname == "" || len(header.Linkname) > 4096 || strings.ContainsAny(header.Linkname, "\x00\r\n") {
				return errors.New("symlink metadata differs")
			}
		case tar.TypeLink:
			if header.Size != 0 {
				return errors.New("hardlink contains payload")
			}
			if _, err := packageTarPath(header.Linkname, false); err != nil {
				return err
			}
		default:
			return errors.New("unsupported rootfs inode type")
		}
		entries[name] = entry
		order = append(order, name)
	}
	if err := drainImagePadding(stream); err != nil {
		return err
	}
	if limited.N == 0 || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != diffId {
		return errors.New("rootfs diff ID or decompressed bound differs")
	}
	// Refuse writes through symlink/file parents, including a parent introduced
	// later in the tar. Root metadata is optional; deeper parents must exist.
	parentDirectory := func(name string) error {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			entry, ok := entries[parent]
			if !ok {
				entry, ok = filesystem[parent]
			}
			if !ok || entry.Kind != tar.TypeDir {
				return fmt.Errorf("rootfs parent is not a directory: %s", parent)
			}
		}
		return nil
	}
	for name := range entries {
		if err := parentDirectory(name); err != nil {
			return err
		}
	}
	for name := range whiteouts {
		if err := parentDirectory(name); err != nil {
			return err
		}
	}
	linkTargets := map[string]bool{}
	for _, entry := range filesystem {
		if entry.Kind == tar.TypeLink {
			linkTargets[entry.Link] = true
		}
	}
	removedPaths, opaquePaths := map[string]bool{}, map[string]bool{}
	for name := range whiteouts {
		directory := path.Dir(name)
		if path.Base(name) == ".wh..wh..opq" {
			opaquePaths[directory] = true
		} else {
			removedPaths[path.Join(directory, strings.TrimPrefix(path.Base(name), ".wh."))] = true
		}
	}
	for existing := range filesystem {
		remove := removedPaths[existing] || opaquePaths["."] && existing != "."
		for parent := path.Dir(existing); parent != "." && !remove; parent = path.Dir(parent) {
			remove = removedPaths[parent] || opaquePaths[parent]
		}
		if remove {
			if linkTargets[existing] {
				return errors.New("whiteout changes an existing hardlink target")
			}
			delete(filesystem, existing)
		}
	}
	for _, name := range order {
		entry := entries[name]
		if linkTargets[name] {
			return errors.New("layer replaces an existing hardlink target")
		}
		if lower, ok := filesystem[name]; ok && lower.Kind == tar.TypeDir && entry.Kind != tar.TypeDir {
			return errors.New("directory type replacement is unsupported")
		}
		if entry.Kind == tar.TypeLink {
			target, ok := filesystem[entry.Link]
			if !ok || target.Kind != tar.TypeReg || target.Mode != entry.Mode || target.Uid != entry.Uid || target.Gid != entry.Gid {
				return errors.New("hardlink target or metadata differs")
			}
			entry.Size = target.Size
			entry.Sha256 = target.Sha256
			linkTargets[entry.Link] = true
		}
		filesystem[name] = entry
	}
	if len(filesystem) > maximumPackageRootEntries {
		return errors.New("rootfs final entry bound")
	}
	// Whiteouts can remove a directory needed by new entries. Check the merged
	// topology, not just the pre-deletion view used to authenticate record paths.
	for name := range filesystem {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if filesystem[parent].Kind != tar.TypeDir {
				return errors.New("final rootfs has missing or non-directory parent")
			}
		}
	}
	return nil
}

// Final package status and critical runtime files are inspected without exec.
func verifyPackageRootfs(filesystem map[string]packageRootEntry, binary buildArtifact, packages []imagePackage) ([]imagePackage, error) {
	name := strings.TrimPrefix(binary.Id, "server-")
	executable := filesystem["usr/local/sbin/bringyour-"+name]
	if executable.Kind != tar.TypeReg || executable.Mode != 0755 || executable.Uid != 0 || executable.Gid != 0 || executable.Size != binary.Bytes || executable.Sha256 != binary.Sha256 {
		return nil, errors.New("embedded package image binary differs")
	}
	aws := filesystem["root/.aws"]
	if aws.Kind != tar.TypeSymlink || aws.Link != "/srv/warp/vault/.aws" || aws.Uid != 0 || aws.Gid != 0 {
		return nil, errors.New("runtime credential link differs")
	}
	for _, name := range []string{"etc/ld.so.cache", "etc/ssl/certs/ca-certificates.crt", "var/lib/dpkg/status", "usr/bin/openssl"} {
		entry := filesystem[name]
		if entry.Kind != tar.TypeReg || entry.Size <= 0 || entry.Uid != 0 || entry.Gid != 0 {
			return nil, fmt.Errorf("runtime package file missing: %s", name)
		}
	}
	if name == "proxy" {
		entry := filesystem["usr/bin/curl"]
		if entry.Kind != tar.TypeReg || entry.Size <= 0 || entry.Mode&0111 == 0 {
			return nil, errors.New("proxy curl missing")
		}
	}
	for entry := range filesystem {
		if entry == "runtime-packages" || strings.HasPrefix(entry, "runtime-packages/") || entry == "var/log/dpkg.log" || entry == "var/cache/ldconfig/aux-cache" {
			return nil, errors.New("build-only package input or volatile output remains")
		}
	}
	statusKVs, err := parsePackageStatus(filesystem["var/lib/dpkg/status"].Raw)
	if err != nil {
		return nil, err
	}
	selected := []imagePackage{}
	for _, p := range packages {
		if p.Architecture != "amd64" && p.Architecture != "all" {
			continue
		}
		fields, ok := statusKVs[p.Name]
		if !ok || fields["Status"] != "install ok installed" || fields["Version"] != p.Version || fields["Architecture"] != p.Architecture {
			return nil, fmt.Errorf("installed package differs: %s", p.Id)
		}
		selected = append(selected, p)
	}
	if len(selected) == 0 {
		return nil, errors.New("no selected runtime packages")
	}
	return selected, nil
}

// Deb822 continuations are allowed only after a field; duplicate identities or
// partially configured packages cannot masquerade as the requested closure.
func parsePackageStatus(raw []byte) (map[string]map[string]string, error) {
	if len(raw) == 0 || len(raw) > 4*1024*1024 || strings.ContainsRune(string(raw), '\r') {
		return nil, errors.New("invalid package database")
	}
	packages := map[string]map[string]string{}
	fields := map[string]string{}
	last := ""
	flush := func() error {
		if len(fields) == 0 {
			return nil
		}
		name := fields["Package"]
		if name == "" || packages[name] != nil || fields["Status"] != "install ok installed" {
			return errors.New("duplicate, missing or unconfigured package identity")
		}
		packages[name] = fields
		fields = map[string]string{}
		last = ""
		return nil
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last == "" {
				return nil, errors.New("orphan package continuation")
			}
			if last == "Package" || last == "Status" || last == "Version" || last == "Architecture" {
				return nil, errors.New("continued package identity")
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || strings.TrimSpace(key) != key {
			return nil, errors.New("malformed package field")
		}
		value = strings.TrimLeft(value, " \t")
		if _, ok := fields[key]; ok {
			return nil, errors.New("duplicate package field")
		}
		fields[key] = value
		last = key
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return packages, nil
}

// Stable rootfs identity covers all final inode records, not only the executable.
func packageRootHash(filesystem map[string]packageRootEntry) (string, error) {
	names := make([]string, 0, len(filesystem))
	for name := range filesystem {
		names = append(names, name)
	}
	sort.Strings(names)
	hasher := sha256.New()
	if _, err := hasher.Write([]byte("urnetwork-image-rootfs-v1\x00")); err != nil {
		return "", err
	}
	encoder := json.NewEncoder(hasher)
	for _, name := range names {
		if err := encoder.Encode(struct {
			Name  string           `json:"name"`
			Entry packageRootEntry `json:"entry"`
		}{Name: name, Entry: filesystem[name]}); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}
