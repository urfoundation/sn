// Periodic refresh of the published operator list. A refresher publishes an
// immutable snapshot only when the fetched bytes change, keeps the last good
// list through fetch failures, and persists it so a restart without network
// access resumes from the same list. Methods are safe for concurrent use.
package operatorlist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// One published list. Snapshots are never mutated after publication.
type Snapshot struct {
	List      List
	Sha256    string
	FetchedAt time.Time
	// Loaded from the local cache and not yet confirmed by a fetch this run.
	Cached bool
}

type RefreshSettings struct {
	// DefaultUrl when empty. HTTPS, or plaintext HTTP for a loopback host.
	Url string
	// Between successful fetches, with up to 10% jitter. At least one minute.
	Interval time.Duration
	// Optional last good list, replaced atomically on every change.
	CachePath string
	// Bounds each fetch; 30 seconds when zero.
	FetchTimeout time.Duration
	// Optional transport for tests and proxies.
	Client *http.Client
}

const minimumInterval = time.Minute

// Test observers see each completed refresh; they cannot replace a fetch.
type refresherHooks struct {
	afterRefresh func(error)
}

type Refresher struct {
	settings RefreshSettings
	snapshot *connect.MonitorValue[*Snapshot]
	hooks    refresherHooks
	// Test-only manual trigger; production waits only on its interval.
	trigger chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}

	stateLock sync.Mutex
	lastError error
}

// Loads the cache when present and starts fetching immediately. The returned
// refresher may have no snapshot until its first fetch or cache load succeeds.
func NewRefresher(ctx context.Context, settings RefreshSettings) (*Refresher, error) {
	return newRefresher(ctx, settings, refresherHooks{})
}

func newRefresher(ctx context.Context, settings RefreshSettings, hooks refresherHooks) (*Refresher, error) {
	settings, err := settings.normalized()
	if err != nil {
		return nil, err
	}
	if settings.Interval < minimumInterval {
		return nil, fmt.Errorf("operator list refresh interval must be at least %s", minimumInterval)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	self := &Refresher{
		settings: settings,
		snapshot: connect.NewMonitorValue[*Snapshot](nil),
		hooks:    hooks,
		trigger:  make(chan struct{}),
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	if cached, err := readCache(settings.CachePath); err == nil && cached != nil {
		self.snapshot.Set(cached)
	}
	go self.run(cancelCtx)
	return self, nil
}

// The current snapshot (nil before any list is known) and a channel that
// closes at the next published change.
func (self *Refresher) Snapshot() (*Snapshot, chan struct{}) {
	return self.snapshot.Get()
}

// Blocks until a list is known or the context ends.
func (self *Refresher) Wait(ctx context.Context) (*Snapshot, error) {
	for {
		snapshot, update := self.snapshot.Get()
		if snapshot != nil {
			return snapshot, nil
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), self.LastError())
		case <-self.done:
			return nil, errors.Join(errors.New("operator list refresher closed"), self.LastError())
		case <-update:
		}
	}
}

// The latest fetch failure, or nil after a successful fetch.
func (self *Refresher) LastError() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.lastError
}

// Stops fetching and joins the worker. The last snapshot stays readable.
func (self *Refresher) Close() {
	self.cancel()
	<-self.done
}

func (self *Refresher) run(ctx context.Context) {
	defer close(self.done)
	failures := 0
	for {
		delay := jitter(self.settings.Interval)
		err := self.refresh(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			// 30s, 60s, ... capped at the interval; never a busy retry.
			delay = min(30*time.Second<<min(failures-1, 8), self.settings.Interval)
		} else {
			failures = 0
		}
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.lastError = err
		}()
		if self.hooks.afterRefresh != nil {
			self.hooks.afterRefresh(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-self.trigger:
		case <-time.After(delay):
		}
	}
}

// One fetch. A changed list is cached before it is published so consumers
// never act on a list a restart would not see again.
func (self *Refresher) refresh(ctx context.Context) error {
	raw, list, err := fetch(ctx, self.settings)
	if err != nil {
		return err
	}
	digest := sha256Digest(raw)
	if current := self.snapshot.Value(); current != nil && current.Sha256 == digest {
		if current.Cached {
			self.snapshot.Set(&Snapshot{List: current.List, Sha256: digest, FetchedAt: time.Now(), Cached: false})
		}
		return nil
	}
	if err := writeCache(self.settings.CachePath, raw); err != nil {
		return fmt.Errorf("operator list cache: %w", err)
	}
	self.snapshot.Set(&Snapshot{List: list, Sha256: digest, FetchedAt: time.Now()})
	return nil
}

// One fetch with a fallback to the cache, for commands that need a list once.
func Load(ctx context.Context, settings RefreshSettings) (*Snapshot, error) {
	settings, err := settings.normalized()
	if err != nil {
		return nil, err
	}
	raw, list, fetchErr := fetch(ctx, settings)
	if fetchErr == nil {
		if err := writeCache(settings.CachePath, raw); err != nil {
			return nil, fmt.Errorf("operator list cache: %w", err)
		}
		return &Snapshot{List: list, Sha256: sha256Digest(raw), FetchedAt: time.Now()}, nil
	}
	cached, cacheErr := readCache(settings.CachePath)
	if cacheErr == nil && cached != nil {
		return cached, nil
	}
	return nil, errors.Join(fetchErr, cacheErr)
}

// Where one operator's credentials and keys live below a base state directory.
func DomainStateDir(base string, domain string) string {
	return filepath.Join(base, "operators", domain)
}

func (self RefreshSettings) normalized() (RefreshSettings, error) {
	if self.Url == "" {
		self.Url = DefaultUrl
	}
	u, err := url.Parse(self.Url)
	if err != nil {
		return self, fmt.Errorf("operator list url: %w", err)
	}
	if u.User != nil || u.Host == "" || u.Scheme != "https" && !(u.Scheme == "http" && loopbackHost(u.Hostname())) {
		return self, fmt.Errorf("operator list url %q must use https (plaintext http only for loopback)", self.Url)
	}
	if self.FetchTimeout <= 0 {
		self.FetchTimeout = 30 * time.Second
	}
	if self.Client == nil {
		self.Client = &http.Client{Timeout: self.FetchTimeout}
	}
	return self, nil
}

func fetch(ctx context.Context, settings RefreshSettings) ([]byte, List, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, settings.FetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, settings.Url, nil)
	if err != nil {
		return nil, List{}, err
	}
	request.Header.Set("Accept", "application/yaml, text/yaml;q=0.9, text/plain;q=0.5")
	response, err := settings.Client.Do(request)
	if err != nil {
		return nil, List{}, fmt.Errorf("fetch operator list: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, List{}, fmt.Errorf("fetch operator list: %s", response.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, MaximumBytes+1))
	if err != nil {
		return nil, List{}, fmt.Errorf("read operator list: %w", err)
	}
	list, err := Parse(raw)
	if err != nil {
		return nil, List{}, err
	}
	return raw, list, nil
}

// A missing cache is (nil, nil); an invalid cache is an error and is ignored by
// callers, never published.
func readCache(path string) (*Snapshot, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaximumBytes+1))
	if err != nil {
		return nil, err
	}
	list, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("cached operator list: %w", err)
	}
	return &Snapshot{List: list, Sha256: sha256Digest(raw), FetchedAt: info.ModTime(), Cached: true}, nil
}

// Write a private temporary beside the cache, sync it, then rename over the
// previous copy, so a crash leaves either the old or the new complete list.
func writeCache(path string, raw []byte) (returnErr error) {
	if path == "" {
		return nil
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if returnErr != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	parent, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(parent.Sync(), parent.Close())
}

func sha256Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Spreads refreshes from many hosts by up to 10% of the interval.
func jitter(interval time.Duration) time.Duration {
	spread := int64(interval / 10)
	if spread <= 0 {
		return interval
	}
	return interval - time.Duration(spread) + time.Duration(rand.Int64N(2*spread+1))
}
