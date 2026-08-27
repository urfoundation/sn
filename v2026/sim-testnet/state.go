package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func resolveStateDir(r *ResolvedConfig, override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	return filepath.Join(filepath.Dir(r.ConfigPath), "runs", r.Config.Deployment.DeploymentID), nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("state directory %s is accessible by group/other (mode %o)", path, info.Mode().Perm())
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err == nil {
		defer d.Close()
		return d.Sync()
	}
	return nil
}

func requireApproved(apply bool, provided, want string) error {
	if !apply {
		return errors.New("dry-run only: re-run with --apply and the exact printed --plan-hash")
	}
	if provided == "" {
		return errors.New("--plan-hash is required with --apply")
	}
	if provided != want {
		return fmt.Errorf("plan hash mismatch: provided %s, current %s; inspect and approve the current plan", provided, want)
	}
	return nil
}
