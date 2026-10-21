package validator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

// A shared fixture is built once, by the first test that needs it, and is
// read-only for every later test. Its directories, cleanups and context
// belong to the test process; the building test only reports its failures.
type sharedTestFixtureOwner struct {
	*testing.T
}

var sharedTestFixtures struct {
	stateLock sync.Mutex
	root      string
	cleanups  []func()
}

// Every call returns a new private directory under the process's own root.
func (self sharedTestFixtureOwner) TempDir() string {
	self.Helper()
	sharedTestFixtures.stateLock.Lock()
	defer sharedTestFixtures.stateLock.Unlock()
	if sharedTestFixtures.root == "" {
		root, err := os.MkdirTemp("", "validator-shared-")
		if err != nil {
			self.Fatal(err)
		}
		sharedTestFixtures.root = root
	}
	dir, err := os.MkdirTemp(sharedTestFixtures.root, "")
	if err != nil {
		self.Fatal(err)
	}
	return dir
}

// Cleanups run, last first, after every test of the process.
func (self sharedTestFixtureOwner) Cleanup(cleanup func()) {
	sharedTestFixtures.stateLock.Lock()
	defer sharedTestFixtures.stateLock.Unlock()
	sharedTestFixtures.cleanups = append(sharedTestFixtures.cleanups, cleanup)
}

// The context outlives the building test.
func (self sharedTestFixtureOwner) Context() context.Context {
	return context.Background()
}

// TestMain closes every shared fixture once the last test has finished. A
// cleanup that reports a failure through its finished building test panics;
// that panic is this process's failure, not another test's.
func closeSharedTestFixtures() (resultErr error) {
	sharedTestFixtures.stateLock.Lock()
	cleanups, root := sharedTestFixtures.cleanups, sharedTestFixtures.root
	sharedTestFixtures.cleanups, sharedTestFixtures.root = nil, ""
	sharedTestFixtures.stateLock.Unlock()
	for index := len(cleanups) - 1; index >= 0; index-- {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("shared fixture cleanup: %v", recovered))
				}
			}()
			cleanups[index]()
		}()
	}
	if root == "" {
		return resultErr
	}
	return errors.Join(resultErr, os.RemoveAll(root))
}
