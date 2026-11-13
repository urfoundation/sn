package operatorlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testListTwo = testListOne + `  - domain: operator-two.example
    api_url: https://api.operator-two.example
    connect_url: wss://connect.operator-two.example
`

// Serves whatever the test last set; each request reports itself.
type testPublisher struct {
	stateLock sync.Mutex
	status    int
	body      string
}

func (self *testPublisher) set(status int, body string) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.status, self.body = status, body
}

func (self *testPublisher) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	status, body := self.status, self.body
	self.stateLock.Unlock()
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(body))
}

// Starts a refresher whose completed refreshes are delivered on a channel, so
// tests wait on actual fetch outcomes instead of time.
func startTestRefresher(t *testing.T, settings RefreshSettings) (*Refresher, chan error) {
	t.Helper()
	refreshes := make(chan error, 16)
	refresher, err := newRefresher(t.Context(), settings, refresherHooks{afterRefresh: func(err error) { refreshes <- err }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(refresher.Close)
	return refresher, refreshes
}

func nextRefresh(t *testing.T, refreshes chan error) error {
	t.Helper()
	select {
	case err := <-refreshes:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("refresh did not complete")
		return nil
	}
}

func TestRefresherPublishesChangesAndKeepsTheLastGoodList(t *testing.T) {
	publisher := &testPublisher{}
	publisher.set(http.StatusOK, testListOne)
	server := httptest.NewServer(publisher)
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "operators.yml")
	refresher, refreshes := startTestRefresher(t, RefreshSettings{Url: server.URL, Interval: time.Hour, CachePath: cachePath})
	if err := nextRefresh(t, refreshes); err != nil {
		t.Fatal(err)
	}
	first, update := refresher.Snapshot()
	if first == nil || first.Cached || len(first.List.Operators) != 1 {
		t.Fatalf("first fetch not published: %+v", first)
	}
	if raw, err := os.ReadFile(cachePath); err != nil || string(raw) != testListOne {
		t.Fatalf("first fetch not cached: %q %v", raw, err)
	}

	publisher.set(http.StatusServiceUnavailable, "unavailable")
	refresher.trigger <- struct{}{}
	if err := nextRefresh(t, refreshes); err == nil || refresher.LastError() == nil {
		t.Fatal("unavailable publisher reported success")
	}
	publisher.set(http.StatusOK, "schema: urnetwork-operators-v1\noperators: []\n")
	refresher.trigger <- struct{}{}
	if err := nextRefresh(t, refreshes); err == nil {
		t.Fatal("invalid publication reported success")
	}
	if current, _ := refresher.Snapshot(); current != first {
		t.Fatal("a failed refresh replaced the last good list")
	}
	select {
	case <-update:
		t.Fatal("a failed refresh notified consumers")
	default:
	}

	publisher.set(http.StatusOK, testListOne)
	refresher.trigger <- struct{}{}
	if err := nextRefresh(t, refreshes); err != nil || refresher.LastError() != nil {
		t.Fatal("recovered publisher still failing", err)
	}
	if current, _ := refresher.Snapshot(); current != first {
		t.Fatal("unchanged bytes republished the list")
	}

	publisher.set(http.StatusOK, testListTwo)
	refresher.trigger <- struct{}{}
	if err := nextRefresh(t, refreshes); err != nil {
		t.Fatal(err)
	}
	<-update
	second, _ := refresher.Snapshot()
	if second == first || len(second.List.Operators) != 2 {
		t.Fatalf("changed list not published: %+v", second)
	}
	if raw, err := os.ReadFile(cachePath); err != nil || string(raw) != testListTwo {
		t.Fatalf("changed list not cached: %q %v", raw, err)
	}
	if info, err := os.Stat(cachePath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache is not private: %v %v", info, err)
	}
}

func TestRefresherResumesFromCacheWhenThePublisherIsUnavailable(t *testing.T) {
	publisher := &testPublisher{}
	publisher.set(http.StatusServiceUnavailable, "unavailable")
	server := httptest.NewServer(publisher)
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "operators.yml")
	if err := os.WriteFile(cachePath, []byte(testListTwo), 0600); err != nil {
		t.Fatal(err)
	}
	refresher, refreshes := startTestRefresher(t, RefreshSettings{Url: server.URL, Interval: time.Hour, CachePath: cachePath})
	snapshot, err := refresher.Wait(t.Context())
	if err != nil || !snapshot.Cached || len(snapshot.List.Operators) != 2 {
		t.Fatalf("cached list not resumed: %+v %v", snapshot, err)
	}
	if err := nextRefresh(t, refreshes); err == nil {
		t.Fatal("unavailable publisher reported success")
	}
	publisher.set(http.StatusOK, testListTwo)
	refresher.trigger <- struct{}{}
	if err := nextRefresh(t, refreshes); err != nil {
		t.Fatal(err)
	}
	if confirmed, _ := refresher.Snapshot(); confirmed.Cached || confirmed.Sha256 != snapshot.Sha256 {
		t.Fatalf("matching fetch did not confirm the cached list: %+v", confirmed)
	}
}

func TestRefresherIgnoresAnInvalidCache(t *testing.T) {
	publisher := &testPublisher{}
	publisher.set(http.StatusServiceUnavailable, "unavailable")
	server := httptest.NewServer(publisher)
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "operators.yml")
	if err := os.WriteFile(cachePath, []byte("not: a list\n"), 0600); err != nil {
		t.Fatal(err)
	}
	refresher, refreshes := startTestRefresher(t, RefreshSettings{Url: server.URL, Interval: time.Hour, CachePath: cachePath})
	if err := nextRefresh(t, refreshes); err == nil {
		t.Fatal("unavailable publisher reported success")
	}
	if snapshot, _ := refresher.Snapshot(); snapshot != nil {
		t.Fatalf("invalid cache published: %+v", snapshot)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := refresher.Wait(ctx); err == nil {
		t.Fatal("wait returned without a list")
	}
}

func TestRefresherRefusesOversizedPublications(t *testing.T) {
	publisher := &testPublisher{}
	publisher.set(http.StatusOK, testListOne+"# "+strings.Repeat("x", MaximumBytes)+"\n")
	server := httptest.NewServer(publisher)
	defer server.Close()
	refresher, refreshes := startTestRefresher(t, RefreshSettings{Url: server.URL, Interval: time.Hour})
	if err := nextRefresh(t, refreshes); err == nil {
		t.Fatal("oversized publication admitted")
	}
	if snapshot, _ := refresher.Snapshot(); snapshot != nil {
		t.Fatal("oversized publication published")
	}
}

func TestLoadFallsBackToTheCache(t *testing.T) {
	publisher := &testPublisher{}
	publisher.set(http.StatusOK, testListOne)
	server := httptest.NewServer(publisher)
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "operators.yml")
	settings := RefreshSettings{Url: server.URL, CachePath: cachePath}
	fetched, err := Load(t.Context(), settings)
	if err != nil || fetched.Cached || len(fetched.List.Operators) != 1 {
		t.Fatalf("load did not fetch: %+v %v", fetched, err)
	}
	publisher.set(http.StatusBadGateway, "down")
	cached, err := Load(t.Context(), settings)
	if err != nil || !cached.Cached || cached.Sha256 != fetched.Sha256 {
		t.Fatalf("load did not fall back to the cache: %+v %v", cached, err)
	}
	if _, err := Load(t.Context(), RefreshSettings{Url: server.URL}); err == nil {
		t.Fatal("load without a cache hid the fetch failure")
	}
}

func TestRefresherRefusesUnsafeSettings(t *testing.T) {
	for _, settings := range []RefreshSettings{
		{Url: "http://ur.example/operators.yml", Interval: time.Hour},
		{Url: "https://user:secret@ur.example/operators.yml", Interval: time.Hour},
		{Url: "ftp://ur.example/operators.yml", Interval: time.Hour},
		{Url: "https://ur.example/operators.yml", Interval: time.Second},
	} {
		if refresher, err := NewRefresher(t.Context(), settings); err == nil {
			refresher.Close()
			t.Errorf("unsafe settings admitted: %+v", settings)
		}
	}
}

func TestDomainStateDirSeparatesOperators(t *testing.T) {
	if got := DomainStateDir("/var/lib/miner", "operator-one.example"); got != filepath.Join("/var/lib/miner", "operators", "operator-one.example") {
		t.Fatalf("unexpected operator state directory %q", got)
	}
}
