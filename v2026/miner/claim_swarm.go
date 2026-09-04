package miner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const ClaimSwarmSchema = "urnetwork-claim-swarm-v1"

type ClaimSwarmMember struct {
	ID         string `json:"id"`
	ConfigPath string `json:"config_path"`
}

type ClaimSwarmConfig struct {
	Schema        string             `json:"schema"`
	ListenAddress string             `json:"listen_address"`
	Members       []ClaimSwarmMember `json:"members"`
}

func (self ClaimSwarmConfig) Validate() error {
	if self.Schema != ClaimSwarmSchema || len(self.Members) == 0 || len(self.Members) > 1_000 {
		return errors.New("claim swarm requires schema v1 and between 1 and 1000 members")
	}
	listen, err := netip.ParseAddrPort(self.ListenAddress)
	if err != nil || !listen.Addr().IsLoopback() || listen.Port() == 0 {
		return errors.New("claim swarm status listener must be a nonzero loopback address")
	}
	seenIDs := map[string]bool{}
	seenConfigs := map[string]bool{}
	for index, member := range self.Members {
		if member.ID == "" || seenIDs[member.ID] || strings.ContainsAny(member.ID, `/\`) {
			return fmt.Errorf("claim member %d has an empty, duplicate or unsafe id", index)
		}
		seenIDs[member.ID] = true
		if !filepath.IsAbs(member.ConfigPath) || seenConfigs[filepath.Clean(member.ConfigPath)] {
			return fmt.Errorf("claim member %s config_path must be absolute and unique", member.ID)
		}
		seenConfigs[filepath.Clean(member.ConfigPath)] = true
	}
	return nil
}

func LoadClaimSwarmConfig(path string) (*ClaimSwarmConfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	decoder := json.NewDecoder(bufio.NewReader(f))
	decoder.DisallowUnknownFields()
	var config ClaimSwarmConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode claim swarm: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("claim swarm config contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode claim swarm trailing data: %w", err)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

type claimSwarmStatus struct {
	Schema     string            `json:"schema"`
	Configured int               `json:"configured"`
	Running    int               `json:"running"`
	Failures   map[string]string `json:"failures,omitempty"`
}

type ClaimSwarm struct {
	config    *ClaimSwarmConfig
	stateLock sync.Mutex
	running   map[string]bool
	failures  map[string]string
}

func NewClaimSwarm(config *ClaimSwarmConfig) (*ClaimSwarm, error) {
	if config == nil {
		return nil, errors.New("claim swarm config is nil")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &ClaimSwarm{config: config, running: map[string]bool{}, failures: map[string]string{}}, nil
}

func (self *ClaimSwarm) status() claimSwarmStatus {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	failures := map[string]string{}
	for id, detail := range self.failures {
		failures[id] = detail
	}
	return claimSwarmStatus{Schema: ClaimSwarmSchema, Configured: len(self.config.Members), Running: len(self.running), Failures: failures}
}

func (self *ClaimSwarm) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.URL.Path != "/status" {
		http.NotFound(writer, request)
		return
	}
	status := self.status()
	writer.Header().Set("Content-Type", "application/json")
	if status.Running != status.Configured || len(status.Failures) != 0 {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(writer).Encode(status)
}

func loadClaimSwarmMembers(config *ClaimSwarmConfig) (map[string]*ClaimDaemonConfig, time.Duration, error) {
	loaded := make(map[string]*ClaimDaemonConfig, len(config.Members))
	var keyFile string
	var rpc []string
	minimumPoll := time.Duration(0)
	for _, member := range config.Members {
		claimConfig, err := LoadClaimDaemonConfig(member.ConfigPath)
		if err != nil {
			return nil, 0, fmt.Errorf("claim member %s: %w", member.ID, err)
		}
		if claimConfig.JWTFile == "" {
			return nil, 0, fmt.Errorf("claim member %s must bind an explicit jwt_file", member.ID)
		}
		if keyFile == "" {
			keyFile = claimConfig.KeyFile
			rpc = append([]string(nil), claimConfig.RPC...)
		} else if claimConfig.KeyFile != keyFile || !slices.Equal(claimConfig.RPC, rpc) {
			return nil, 0, errors.New("claim swarm members must share one relayer key and exact RPC failover list")
		}
		poll := time.Duration(claimConfig.PollSeconds) * time.Second
		if minimumPoll == 0 || poll < minimumPoll {
			minimumPoll = poll
		}
		loaded[member.ID] = claimConfig
	}
	return loaded, minimumPoll, nil
}

func (self *ClaimSwarm) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("claim swarm context is nil")
	}
	_, pollPeriod, err := loadClaimSwarmMembers(self.config)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{Addr: self.config.ListenAddress, Handler: self}
	serverErrors := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	members := append([]ClaimSwarmMember(nil), self.config.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	terminalErrors := make(chan error, 1)
	var chainStateLock sync.Mutex
	for index, member := range members {
		delay := time.Duration(index) * pollPeriod / time.Duration(len(members))
		go func(member ClaimSwarmMember, initialDelay time.Duration) {
			onReady := func() {
				self.stateLock.Lock()
				self.running[member.ID] = true
				self.stateLock.Unlock()
			}
			if runErr := runClaimDaemonWithLock(runCtx, member.ConfigPath, &chainStateLock, initialDelay, onReady); runErr != nil {
				self.stateLock.Lock()
				delete(self.running, member.ID)
				self.failures[member.ID] = runErr.Error()
				self.stateLock.Unlock()
				select {
				case terminalErrors <- fmt.Errorf("claim member %s: %w", member.ID, runErr):
				default:
				}
				cancel()
			}
		}(member, delay)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-serverErrors:
		return err
	case err := <-terminalErrors:
		return err
	}
}

func RunClaimSwarm(ctx context.Context, configPath string) error {
	config, err := LoadClaimSwarmConfig(configPath)
	if err != nil {
		return err
	}
	swarm, err := NewClaimSwarm(config)
	if err != nil {
		return err
	}
	return swarm.Run(ctx)
}
