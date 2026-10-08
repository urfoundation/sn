package miner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/sdk/v2026"
)

// The real SDK implements this read-only interface. The owner calls it outside
// its state lock and rechecks the same generation before publishing any facts.
type providerProgressDevice interface {
	GetClientId() *sdk.Id
	GetProviderConnected() bool
	GetProviderClientKeyRegistered() bool
	GetProviderReady() bool
}

var _ providerProgressDevice = (*sdk.DeviceLocal)(nil)

type providerProgressSlot struct {
	generation uint64
	done       <-chan struct{}
	device     providerProgressDevice
	lifecycle  string
}

type providerProgressOwner struct {
	stateLock sync.Mutex
	requests  sync.WaitGroup
	closed    bool
	source    protocol.ProviderProgressSource
	instance  string
	started   time.Time
	sequence  uint64
	slots     map[string]providerProgressSlot
}

// This source digest contains only configured routing/slot/public identity
// inputs. A monitor must supply its own expected digest and client roster.
type providerProgressConfigMember struct {
	Slot        string `json:"slot"`
	ApiUrl      string `json:"api_url"`
	ConnectUrl  string `json:"connect_url"`
	DnsPumpHost string `json:"dns_pump_host,omitempty"`
	Wallet      string `json:"wallet,omitempty"`
	SourceIp    string `json:"source_ip,omitempty"`
}

func newProviderProgressOwner(mode string, members []providerProgressConfigMember) (*providerProgressOwner, error) {
	if len(members) == 0 || len(members) > protocol.MaxProviderProgressMembers {
		return nil, errors.New("provider progress census exceeds its bound")
	}
	members = append([]providerProgressConfigMember(nil), members...)
	sort.Slice(members, func(i, j int) bool { return members[i].Slot < members[j].Slot })
	slots := map[string]providerProgressSlot{}
	for _, member := range members {
		if !protocol.ValidProviderSlot(member.Slot) || len(member.ApiUrl) > 2048 || len(member.ConnectUrl) > 2048 || len(member.DnsPumpHost) > 253 || len(member.Wallet) > 128 || len(member.SourceIp) > 64 {
			return nil, errors.New("provider progress configuration is invalid")
		}
		if _, ok := slots[member.Slot]; ok {
			return nil, errors.New("provider progress repeats a slot")
		}
		slots[member.Slot] = providerProgressSlot{lifecycle: "starting"}
	}
	raw, err := json.Marshal(struct {
		Mode    string                         `json:"mode"`
		Members []providerProgressConfigMember `json:"members"`
	}{mode, members})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	source := protocol.ProviderProgressSource{Mode: mode, ConfigHash: hex.EncodeToString(digest[:])}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	var instance [16]byte
	if _, err := rand.Read(instance[:]); err != nil {
		return nil, err
	}
	return &providerProgressOwner{source: source, instance: hex.EncodeToString(instance[:]), started: time.Now().UTC(), slots: slots}, nil
}

// A new admitted member consumes one generation; a stale teardown can only
// retire that exact generation. No callback or SDK close runs under this lock.
func (self *providerProgressOwner) attach(ctx context.Context, slot string, device providerProgressDevice) (uint64, error) {
	if ctx == nil {
		return 0, errors.New("provider member lifetime is absent")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	done := ctx.Done()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closed {
		return 0, errors.New("provider observation owner is closed")
	}
	state, ok := self.slots[slot]
	if !ok || state.generation == math.MaxUint64 {
		return 0, errors.New("provider progress slot or generation is exhausted")
	}
	state.generation++
	state.done = done
	state.device = device
	state.lifecycle = "running"
	self.slots[slot] = state
	return state.generation, nil
}

func (self *providerProgressOwner) retire(slot string, generation uint64) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	state, ok := self.slots[slot]
	if ok && state.generation == generation {
		state.device = nil
		state.lifecycle = "stopped"
		self.slots[slot] = state
	}
}

func (self *providerProgressOwner) snapshot(ctx context.Context) (*protocol.ProviderProgress, error) {
	if ctx == nil {
		return nil, errors.New("provider observation requires context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.stateLock.Lock()
	if self.closed {
		self.stateLock.Unlock()
		return nil, errors.New("provider observation owner is closed")
	}
	slots := make(map[string]providerProgressSlot, len(self.slots))
	for id, slot := range self.slots {
		slots[id] = slot
	}
	self.stateLock.Unlock()
	value := &protocol.ProviderProgress{Schema: protocol.ProviderProgressSchema, Source: self.source, InstanceId: self.instance, StartedAt: self.started.Format(time.RFC3339Nano), Proof: "unknown", Settlement: "unknown"}
	names := make([]string, 0, len(slots))
	for name := range slots {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slot := slots[name]
		member := protocol.ProviderMemberProgress{Slot: name, Generation: slot.generation, Lifecycle: slot.lifecycle}
		if slot.device != nil && !providerProgressCanceled(slot.done) {
			identity := slot.device.GetClientId()
			if identity != nil {
				member.ClientId = identity.String()
			}
			member.Connected = slot.device.GetProviderConnected()
			member.KeyRegistered = slot.device.GetProviderClientKeyRegistered()
			member.Ready = slot.device.GetProviderReady() && member.Connected && member.KeyRegistered
			member.Current = member.ClientId != ""
		}
		self.stateLock.Lock()
		current := self.slots[name]
		same := current.generation == slot.generation && current.lifecycle == slot.lifecycle
		self.stateLock.Unlock()
		if !same {
			member = protocol.ProviderMemberProgress{Slot: name, Generation: current.generation, Lifecycle: current.lifecycle}
		}
		if !member.Current {
			member.Connected, member.KeyRegistered, member.Ready = false, false, false
		}
		value.Members = append(value.Members, member)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.stateLock.Lock()
	for index, member := range value.Members {
		current := self.slots[member.Slot]
		if current.generation != member.Generation || current.lifecycle != member.Lifecycle || providerProgressCanceled(current.done) {
			value.Members[index] = protocol.ProviderMemberProgress{Slot: member.Slot, Generation: current.generation, Lifecycle: current.lifecycle}
		}
	}
	if self.sequence == math.MaxUint64 {
		self.stateLock.Unlock()
		return nil, errors.New("provider observation sequence is exhausted")
	}
	self.sequence++
	value.Sequence = self.sequence
	self.stateLock.Unlock()
	value.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func providerProgressCanceled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func serveProviderProgress(writer http.ResponseWriter, request *http.Request, owner *providerProgressOwner) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodGet || request.URL.RawQuery != "" {
		http.Error(writer, "provider progress requires an unqualified GET", http.StatusBadRequest)
		return
	}
	if owner == nil {
		http.Error(writer, "provider observation owner is absent", http.StatusServiceUnavailable)
		return
	}
	if request.Context().Err() != nil {
		return
	}
	owner.stateLock.Lock()
	if owner.closed {
		owner.stateLock.Unlock()
		http.Error(writer, "provider observation owner is closed", http.StatusServiceUnavailable)
		return
	}
	owner.requests.Add(1)
	owner.stateLock.Unlock()
	defer owner.requests.Done()
	value, err := owner.snapshot(request.Context())
	if err != nil {
		http.Error(writer, "provider observation is unavailable", http.StatusServiceUnavailable)
		return
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > protocol.MaxProviderProgressBytes {
		http.Error(writer, "provider observation exceeds its bound", http.StatusServiceUnavailable)
		return
	}
	if request.Context().Err() != nil {
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_, _ = writer.Write(raw)
}

// The enclosing HTTP lifetime interrupts responses before joining here.
// Admission and Add are serialized with closure; no later request can race Wait.
func (self *providerProgressOwner) close() {
	self.stateLock.Lock()
	self.closed = true
	self.stateLock.Unlock()
	self.requests.Wait()
}
