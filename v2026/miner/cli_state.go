package miner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urfoundation/sn/v2026/operatorlist"
)

// What the friendly commands (start, status, setup, service, update) read
// about this device. Everything here only reads the state directory, except
// the local status file, which a running miner writes beside its state.
// None of these files start with ".provider", ".validator" or ".jwt", so
// registration custody never counts them as provider history.

const minerStatusFileName = "miner-status.json"
const minerStatusSchema = "urnetwork-miner-local-status-v1"

// How often a running miner rewrites its status file, and how old the file
// may be before status treats the miner as gone.
const minerStatusInterval = 5 * time.Second
const minerStatusStale = 30 * time.Second

// minerLocalStatus is the running miner's own account of itself, for
// provider status on the same device. It holds no credential.
type minerLocalStatus struct {
	Schema         string              `json:"schema"`
	Pid            int                 `json:"pid"`
	Version        string              `json:"version"`
	StartedAt      time.Time           `json:"started_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	StoppedAt      *time.Time          `json:"stopped_at,omitempty"`
	ConnectedSince *time.Time          `json:"connected_since,omitempty"`
	Members        []minerMemberStatus `json:"members"`
}

type minerMemberStatus struct {
	Slot      string `json:"slot"`
	ClientId  string `json:"client_id,omitempty"`
	Lifecycle string `json:"lifecycle"`
	Connected bool   `json:"connected"`
	Ready     bool   `json:"ready"`
}

// connected is true when every member is connected.
func (self *minerLocalStatus) connected() bool {
	if len(self.Members) == 0 {
		return false
	}
	for _, member := range self.Members {
		if !member.Connected {
			return false
		}
	}
	return true
}

func (self *minerLocalStatus) connectedCount() int {
	count := 0
	for _, member := range self.Members {
		if member.Connected {
			count++
		}
	}
	return count
}

// running is true when the file is fresh and its process is still alive.
func (self *minerLocalStatus) running(now time.Time) bool {
	return self.StoppedAt == nil && now.Sub(self.UpdatedAt) < minerStatusStale && minerProcessAlive(self.Pid)
}

func readMinerLocalStatus() (*minerLocalStatus, error) {
	path, err := providerStatePath(minerStatusFileName)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	status := &minerLocalStatus{}
	if err := json.Unmarshal(raw, status); err != nil {
		return nil, err
	}
	if status.Schema != minerStatusSchema {
		return nil, errors.New("unknown miner status schema")
	}
	return status, nil
}

// writeMinerLocalStatus replaces the status file in one rename, so a reader
// never sees half of it.
func writeMinerLocalStatus(status *minerLocalStatus) error {
	path, err := providerStatePath(minerStatusFileName)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// startMinerStatusFile keeps the status file current from the run's
// progress until the returned stop is called, which records the stop time.
// A status file that can't be written never stops the miner.
func startMinerStatusFile(ctx context.Context, progress *providerProgressOwner) (stop func()) {
	if progress == nil {
		return func() {}
	}
	now := time.Now().UTC()
	status := &minerLocalStatus{Schema: minerStatusSchema, Pid: os.Getpid(), Version: RequireVersion(), StartedAt: now}
	update := func() {
		snapshotCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		value, err := progress.snapshot(snapshotCtx)
		if err != nil {
			return
		}
		status.Members = status.Members[:0]
		for _, member := range value.Members {
			status.Members = append(status.Members, minerMemberStatus{Slot: member.Slot, ClientId: member.ClientId, Lifecycle: member.Lifecycle, Connected: member.Connected, Ready: member.Ready})
		}
		status.UpdatedAt = time.Now().UTC()
		if status.connected() {
			if status.ConnectedSince == nil {
				since := status.UpdatedAt
				status.ConnectedSince = &since
			}
		} else {
			status.ConnectedSince = nil
		}
		_ = writeMinerLocalStatus(status)
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(minerStatusInterval)
		defer ticker.Stop()
		update()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				update()
			}
		}
	}()
	return func() {
		close(done)
		<-finished
		stopped := time.Now().UTC()
		status.StoppedAt = &stopped
		status.UpdatedAt = stopped
		status.ConnectedSince = nil
		for i := range status.Members {
			status.Members[i].Connected = false
			status.Members[i].Ready = false
		}
		_ = writeMinerLocalStatus(status)
	}
}

// minerSignIn describes the network sign-in in the state directory.
type minerSignIn struct {
	signedIn    bool
	networkName string
}

func readMinerSignIn() minerSignIn {
	path, err := providerStatePath("jwt")
	if err != nil {
		return minerSignIn{}
	}
	return readMinerSignInFile(path)
}

func readMinerSignInFile(path string) minerSignIn {
	raw, err := os.ReadFile(path)
	if err != nil {
		return minerSignIn{}
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return minerSignIn{}
	}
	return minerSignIn{signedIn: true, networkName: unverifiedJwtString(token, "network_name")}
}

// minerDevice describes this device's provider identity in the state
// directory: its key, and the client the network gave it.
type minerDevice struct {
	hasKey     bool
	hasHistory bool
	// registered: the network gave some slot of this device a client
	registered bool
	clientId   string
}

func readMinerDevice() minerDevice {
	dir, err := providerStateDir()
	if err != nil {
		return minerDevice{}
	}
	return readMinerDeviceIn(dir)
}

func readMinerDeviceIn(dir string) minerDevice {
	device := minerDevice{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return device
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".provider") || name == ".provider.key.registration.lock" {
			continue
		}
		device.hasHistory = true
		if name == ".provider.key" {
			device.hasKey = true
		}
		if strings.HasSuffix(name, ".jwt") {
			device.registered = true
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, ".provider.jwt")); err == nil {
		device.clientId = unverifiedJwtString(strings.TrimSpace(string(raw)), "client_id")
	}
	return device
}

// readMinerOperatorIdentity is the sign-in and device of the first operator
// directory (operatorlist.DomainStateDir, in name order) that has them. provide
// --all-operators keeps each operator's jwt and provider identity there, never
// at the top of the state directory, so status reads them when the top has
// none. Display only: the supervisor decides which operators run.
func readMinerOperatorIdentity(base string) (minerSignIn, minerDevice) {
	signIn, device := minerSignIn{}, minerDevice{}
	entries, err := os.ReadDir(filepath.Join(base, "operators"))
	if err != nil {
		return signIn, device
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := operatorlist.DomainStateDir(base, entry.Name())
		if !signIn.signedIn {
			signIn = readMinerSignInFile(filepath.Join(dir, "jwt"))
		}
		if operatorDevice := readMinerDeviceIn(dir); !device.registered && (operatorDevice.registered || !device.hasHistory) {
			device = operatorDevice
		}
	}
	return signIn, device
}

// unverifiedJwtString reads one claim for display only. The server verifies
// every token it is given; nothing here trusts the value.
func unverifiedJwtString(token string, claim string) string {
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
		return ""
	}
	value, _ := claims[claim].(string)
	return value
}

// shortId shows the start and end of a long identifier.
func shortId(id string) string {
	if len(id) <= 13 {
		return id
	}
	return id[:4] + "…" + id[len(id)-4:]
}

// displayStateDir is the state directory as the user would type it.
func displayStateDir() string {
	dir, err := providerStateDir()
	if err != nil {
		return "~/.urnetwork"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if dir == home {
			return "~"
		}
		if strings.HasPrefix(dir, home+string(filepath.Separator)) {
			return "~" + dir[len(home):]
		}
	}
	return dir
}
