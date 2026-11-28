// A preprovisioned snapshot head retains the complete acknowledged member
// census independently of the immutable files themselves. Directory identity
// alone cannot distinguish a fresh namespace from one whose members were lost.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const bootstrapSuccessorMemberSchema = "urnetwork-successor-member-census-v1"
const maximumBootstrapSuccessorMemberCount = 4096

// Includes 4096 worst-escaped 255-byte names, numeric/hash fields and one
// base64-encoded maximum 512KiB public payload without narrowing its profile.
const maximumBootstrapSuccessorMemberCensusBytes = 8 * 1024 * 1024
const maximumBootstrapSuccessorMemberTotalBytes = 256 * 1024 * 1024

// These two fixed owner kinds are separate preparation targets. Their snapshot
// heads must already exist; runtime admission never infers fresh enrollment.
func bootstrapSuccessorMemberSpec(registry bool) durablehead.Spec {
	if registry {
		return durablehead.Spec{Kind: "mainnet-successor-nonce-members", Name: ".successor-nonce-members.json", MaximumBytes: maximumBootstrapSuccessorMemberCensusBytes}
	}
	return durablehead.Spec{Kind: "mainnet-successor-local-members", Name: ".successor-local-members.json", MaximumBytes: maximumBootstrapSuccessorMemberCensusBytes}
}

// The additional checkpoint does not consume a historical application slot.
// Only this exact owner's fixed name is excluded, never a filename prefix.
func bootstrapSuccessorApplicationNames(names []string, checkpoint string) []string {
	filtered := names[:0]
	for _, name := range names {
		if name != checkpoint {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

type bootstrapSuccessorMember struct {
	Name   string `json:"name"`
	Inode  uint64 `json:"inode"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
}

// The expected payload is authenticated by its original application approval.
// Before stage creation, only that exact name and payload may be resumed. Once
// its inode is acknowledged, disappearance or replacement is permanent loss.
type bootstrapSuccessorMemberPending struct {
	Name       string `json:"name"`
	Stage      string `json:"stage,omitempty"`
	Size       int64  `json:"size"`
	Sha256     string `json:"sha256"`
	Payload    string `json:"payload"`
	StageInode uint64 `json:"stage_inode,omitempty"`
	Append     bool   `json:"append,omitempty"`
}

type bootstrapSuccessorMemberCensus struct {
	Schema  string                           `json:"schema"`
	Kind    string                           `json:"kind"`
	Members []bootstrapSuccessorMember       `json:"members"`
	Pending *bootstrapSuccessorMemberPending `json:"pending,omitempty"`
}

// Payload authentication is retained for this open owner only. All named-fd
// metadata is still observed at every boundary; access time is not identity.
type bootstrapSuccessorMemberObservation struct {
	member bootstrapSuccessorMember
	stat   unix.Stat_t
}

// The enclosing preparation/execution owner serializes calls and joins before
// closing the borrowed directory/lock. This helper never upgrades that lock.
type bootstrapSuccessorMembers struct {
	storage         *mainnetDurableDirectory
	file            *os.File
	head            *durablehead.Owner
	restoredHead    *bootstrapSuccessorRestoredMemberHead
	spec            durablehead.Spec
	census          bootstrapSuccessorMemberCensus
	registry        bool
	readOnly        bool
	observedNameKVs map[string]bootstrapSuccessorMemberObservation
	afterRead       func(string, int)
}

// A passive preparation may authenticate existing members during a later
// execution's pending operation, but cannot acknowledge or reconcile it.
func openBootstrapSuccessorMembers(storage *mainnetDurableDirectory, file *os.File, registry, readOnly bool) (*bootstrapSuccessorMembers, error) {
	if storage == nil || file == nil {
		return nil, errors.New("successor member custody owner is absent")
	}
	if err := storage.check(file); err != nil {
		return nil, err
	}
	spec := bootstrapSuccessorMemberSpec(registry)
	open := durablehead.Open
	if readOnly {
		open = durablehead.OpenReadOnly
	}
	head, err := open(storage.ctx, storage.directory, file, spec)
	if errors.Is(err, durablehead.ErrUncertain) && !readOnly {
		// The failed primitive opener retained no live users. The enclosing
		// application still holds the exact original exclusive directory lock.
		head, err = durablehead.Reconcile(storage.ctx, storage.directory, file, spec)
	}
	if err != nil {
		return nil, storage.snapshotError(err)
	}
	self := &bootstrapSuccessorMembers{storage: storage, file: file, head: head, spec: spec, registry: registry, readOnly: readOnly,
		observedNameKVs: map[string]bootstrapSuccessorMemberObservation{},
		census:          bootstrapSuccessorMemberCensus{Schema: bootstrapSuccessorMemberSchema, Kind: spec.Kind, Members: []bootstrapSuccessorMember{}}}
	raw, present, err := head.Read()
	if err == nil && present {
		err = decodePlanJson(raw, &self.census)
		if err == nil {
			canonical, encodeErr := json.Marshal(self.census)
			if encodeErr != nil || !bytes.Equal(raw, canonical) {
				err = errors.Join(errors.New("successor member census is not canonical"), encodeErr)
			}
		}
		if err != nil {
			err = storage.identity("successor member census encoding changed", err)
		}
	}
	if err == nil {
		err = self.validate()
	}
	if err == nil {
		err = self.check()
	}
	if err != nil {
		return nil, errors.Join(storage.snapshotError(err), head.Close())
	}
	return self, nil
}

// Unknown names inside an owned namespace are included and then refused by the
// retained census. Unrelated bootstrap role snapshots are separate owners.
func (self *bootstrapSuccessorMembers) owns(name string) bool {
	if self.restoredHead != nil && name == self.restoredHead.temporary {
		return false
	}
	return bootstrapSuccessorMemberOwns(self.spec, self.registry, name)
}

// Offline restoration uses the same fixed namespace as the actual owner.
func bootstrapSuccessorMemberOwns(spec durablehead.Spec, registry bool, name string) bool {
	if registry {
		return name != spec.Name
	}
	return name == bootstrapSuccessorPreparationFile || name == bootstrapSuccessorPreparationFile+".lock" ||
		strings.HasPrefix(name, bootstrapSuccessorStagePrefix) || strings.HasPrefix(name, bootstrapSuccessorExecutionPrefix) ||
		strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix)
}

// Schema limits are implementation constants, never values selected by disk.
func (self *bootstrapSuccessorMembers) validate() error {
	if err := validateBootstrapSuccessorMemberCensus(self.census, self.spec, self.registry); err != nil {
		self.storage.failed = errors.Join(durablevolume.ErrIdentity, err)
		return self.storage.failed
	}
	return nil
}

// Parsing a retained census does not require or grant a writer capability.
func validateBootstrapSuccessorMemberCensus(census bootstrapSuccessorMemberCensus, spec durablehead.Spec, registry bool) error {
	if census.Schema != bootstrapSuccessorMemberSchema || census.Kind != spec.Kind || census.Members == nil || len(census.Members) > maximumBootstrapSuccessorMemberCount {
		return errors.New("successor member census scope or bounds changed")
	}
	validName := func(name string) bool {
		return name != "" && name != "." && name != ".." && filepath.Base(name) == name && len(name) <= 255 && !strings.ContainsRune(name, 0) && bootstrapSuccessorMemberOwns(spec, registry, name)
	}
	previous, total := "", int64(0)
	for _, member := range census.Members {
		if !validName(member.Name) || member.Name <= previous || member.Inode == 0 || member.Size < 0 || member.Size > maximumBootstrapSuccessorExecutionBytes || !planSha256(member.Sha256) || member.Size > maximumBootstrapSuccessorMemberTotalBytes-total {
			return errors.New("successor member census contains invalid or unbounded identity")
		}
		previous, total = member.Name, total+member.Size
	}
	if pending := census.Pending; pending != nil {
		if !validName(pending.Name) || pending.Size <= 0 || pending.Size > maximumBootstrapSuccessorExecutionBytes || !planSha256(pending.Sha256) ||
			pending.Append && (pending.Stage != "" || pending.StageInode != 0 || registry || pending.Name != bootstrapSuccessorPreparationFile+".lock") ||
			!pending.Append && (!validName(pending.Stage) || pending.Stage == pending.Name) {
			return errors.New("successor pending member authority is invalid")
		}
		if len(pending.Payload) > base64.StdEncoding.EncodedLen(maximumBootstrapSuccessorExecutionBytes) {
			return errors.New("successor pending payload exceeds its byte bound")
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(pending.Payload)
		if err != nil || int64(len(raw)) != pending.Size || safeReleaseHash(raw) != pending.Sha256 || base64.StdEncoding.EncodeToString(raw) != pending.Payload {
			return errors.Join(errors.New("successor pending payload differs from its exact original bytes"), err)
		}
		index := sort.Search(len(census.Members), func(i int) bool { return census.Members[i].Name >= pending.Name })
		exists := index < len(census.Members) && census.Members[index].Name == pending.Name
		prior := bootstrapSuccessorMember{}
		if exists {
			prior = census.Members[index]
		}
		if pending.Append != exists {
			return errors.New("successor pending member predecessor differs")
		}
		if pending.Size > maximumBootstrapSuccessorMemberTotalBytes-(total-prior.Size) || !exists && len(census.Members) == maximumBootstrapSuccessorMemberCount {
			return errors.New("successor pending member exceeds its retained census bounds")
		}
	}
	return nil
}

func (self *bootstrapSuccessorMembers) member(name string) (bootstrapSuccessorMember, bool) {
	index := sort.Search(len(self.census.Members), func(i int) bool { return self.census.Members[i].Name >= name })
	if index < len(self.census.Members) && self.census.Members[index].Name == name {
		return self.census.Members[index], true
	}
	return bootstrapSuccessorMember{}, false
}

// The first observation authenticates bounded bytes; unchanged later checks
// inspect metadata only. A committed member cannot acquire changed metadata.
func (self *bootstrapSuccessorMembers) observe(name string) (bootstrapSuccessorMember, error) {
	result := bootstrapSuccessorMember{Name: name}
	if err := self.storage.ctx.Err(); err != nil {
		return result, err
	}
	fd, err := unix.Openat(int(self.file.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return result, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var before, after, named, root unix.Stat_t
	if err := errors.Join(unix.Fstat(fd, &before), unix.Fstat(int(self.file.Fd()), &root)); err != nil {
		return result, mainnetDurableUnavailable("cannot inspect successor member descriptor", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0077 != 0 || before.Nlink != 1 || before.Dev != root.Dev || before.Uid != root.Uid || before.Size < 0 || before.Size > maximumBootstrapSuccessorExecutionBytes {
		return result, self.storage.identity("successor member is not a bounded private original file", nil)
	}
	if retained, ok := self.observedNameKVs[name]; ok {
		if bootstrapSuccessorMemberSameStat(before, retained.stat) {
			if err := unix.Fstatat(int(self.file.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return result, self.observation("successor member name became unavailable", err, true)
			}
			if !bootstrapSuccessorMemberSameStat(before, named) {
				return result, self.storage.identity("successor member changed during observation", nil)
			}
			return retained.member, nil
		}
		pending := self.census.Pending
		if _, committed := self.member(name); committed && (pending == nil || !pending.Append || pending.Name != name) {
			return result, self.storage.identity("acknowledged successor member metadata changed", nil)
		}
	}
	hash := sha256.New()
	buffer := make([]byte, 32*1024)
	reader := io.NewSectionReader(file, 0, before.Size)
	for {
		if err := self.storage.ctx.Err(); err != nil {
			return result, err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
			if self.afterRead != nil {
				self.afterRead(name, n)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return result, mainnetDurableUnavailable("cannot hash successor member", readErr)
		}
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return result, mainnetDurableUnavailable("cannot recheck successor member descriptor", err)
	}
	if err := unix.Fstatat(int(self.file.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return result, self.observation("successor member name became unavailable", err, true)
	}
	if !bootstrapSuccessorMemberSameStat(before, after) || !bootstrapSuccessorMemberSameStat(after, named) {
		return result, self.storage.identity("successor member changed during observation", nil)
	}
	result.Inode, result.Size, result.Sha256 = before.Ino, before.Size, "sha256:"+hex.EncodeToString(hash.Sum(nil))
	self.observedNameKVs[name] = bootstrapSuccessorMemberObservation{member: result, stat: after}
	return result, nil
}

// Reads can change atime; publication, replacement and protection changes
// affect the retained identity fields and cannot reuse an old byte digest.
func bootstrapSuccessorMemberSameStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Nlink == b.Nlink &&
		a.Uid == b.Uid && a.Gid == b.Gid && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func (self *bootstrapSuccessorMembers) observation(message string, err error, expected bool) error {
	if errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) && expected || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return self.storage.identity(message, err)
	}
	if errors.Is(err, os.ErrNotExist) {
		return err
	}
	return mainnetDurableUnavailable(message, err)
}

// Pending members may be exact empty/partial stages under their original
// reservation. Only publication with the exact approved payload can advance
// them; they are never silently adopted as committed census members.
func (self *bootstrapSuccessorMembers) check() error {
	if err := self.storage.check(self.file); err != nil {
		return err
	}
	if self.restoredHead != nil {
		if !self.readOnly {
			return self.storage.identity("restored passive census acquired writer mode", nil)
		}
		if err := self.restoredHead.check(); err != nil {
			return err
		}
	} else {
		if self.head == nil {
			return errors.New("successor member head is closed")
		}
		if err := self.head.Check(); err != nil {
			return self.storage.snapshotError(err)
		}
	}
	fd, err := unix.Openat(int(self.file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return mainnetDurableUnavailable("cannot census successor members", err)
	}
	directory := os.NewFile(uintptr(fd), self.file.Name())
	names, readErr := directory.Readdirnames(maximumBootstrapSuccessorMemberCount + 8)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil {
		return mainnetDurableUnavailable("cannot complete successor member census", errors.Join(readErr, closeErr))
	}
	if len(names) >= maximumBootstrapSuccessorMemberCount+8 {
		return mainnetDurableUnavailable("successor namespace exceeds its finite scan capacity", nil)
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !self.owns(name) {
			continue
		}
		seen[name] = true
		actual, err := self.observe(name)
		if err != nil {
			return self.observation("retained successor member cannot be observed", err, true)
		}
		pending := self.census.Pending
		if pending != nil && (name == pending.Stage || name == pending.Name) {
			if pending.Append {
				prior, _ := self.member(name)
				if actual.Inode != prior.Inode || actual.Size < prior.Size || actual.Size > pending.Size || actual.Size == prior.Size && actual.Sha256 != prior.Sha256 || actual.Size == pending.Size && actual.Sha256 != pending.Sha256 {
					return self.storage.identity("pending successor completion changed its original member", nil)
				}
			} else if actual.Size > pending.Size || pending.StageInode != 0 && actual.Inode != pending.StageInode || name == pending.Name && (pending.StageInode == 0 || actual.Size != pending.Size || actual.Sha256 != pending.Sha256) || actual.Size == pending.Size && actual.Sha256 != pending.Sha256 {
				return self.storage.identity("pending successor publication changed its exact member", nil)
			}
			continue
		}
		retained, ok := self.member(name)
		if !ok || actual != retained {
			return self.storage.identity("successor member differs from the retained complete census", nil)
		}
	}
	for _, member := range self.census.Members {
		if !seen[member.Name] {
			return self.storage.identity("acknowledged successor member disappeared", nil)
		}
	}
	if pending := self.census.Pending; pending != nil && !pending.Append {
		if seen[pending.Stage] && seen[pending.Name] || pending.StageInode != 0 && !seen[pending.Stage] && !seen[pending.Name] {
			return self.storage.identity("acknowledged successor pending member disappeared or forked", nil)
		}
	}
	return self.storage.check(self.file)
}

// Snapshot publication itself has the separately qualified uncertainty class.
// The in-memory census advances only after that exact checkpoint is durable.
func (self *bootstrapSuccessorMembers) publish(census bootstrapSuccessorMemberCensus) error {
	if self.readOnly {
		return durablehead.ErrReadOnly
	}
	raw, err := json.Marshal(census)
	if err != nil {
		return err
	}
	if len(raw) > maximumBootstrapSuccessorMemberCensusBytes {
		return mainnetDurableUnavailable("successor member census exceeds its fixed byte capacity", nil)
	}
	if err := self.head.Publish(raw, nil); err != nil {
		return self.storage.snapshotError(err)
	}
	self.census = census
	return nil
}

// A new reservation is durable before creating a member or changing its bytes.
// Existing pending authority cannot be switched to another name or payload.
func (self *bootstrapSuccessorMembers) reserve(name, stage string, raw []byte, appendMember bool) error {
	if err := self.check(); err != nil {
		return err
	}
	if err := self.storage.checkWrite(self.file); err != nil {
		return err
	}
	if self.readOnly {
		return durablehead.ErrReadOnly
	}
	if len(raw) == 0 || len(raw) > maximumBootstrapSuccessorExecutionBytes || !self.owns(name) {
		return errors.New("successor member reservation is invalid")
	}
	expected := bootstrapSuccessorMemberPending{Name: name, Stage: stage, Size: int64(len(raw)), Sha256: safeReleaseHash(raw), Payload: base64.StdEncoding.EncodeToString(raw), Append: appendMember}
	if prior, exists := self.member(name); exists && prior.Size == expected.Size && prior.Sha256 == expected.Sha256 {
		return nil
	}
	if pending := self.census.Pending; pending != nil {
		expected.StageInode = pending.StageInode
		if expected != *pending {
			return errors.New("successor member retains another pending publication")
		}
		return nil
	}
	if prior, exists := self.member(name); exists {
		if !appendMember || self.registry || name != bootstrapSuccessorPreparationFile+".lock" || expected.Size <= prior.Size {
			return errors.New("successor execution retained publication differs; preserve custody")
		}
	} else if appendMember {
		return self.storage.identity("successor completion lost its acknowledged predecessor", nil)
	}
	if len(self.census.Members) >= maximumBootstrapSuccessorMemberCount && !appendMember {
		return mainnetDurableUnavailable("successor member census capacity is exhausted", nil)
	}
	total := int64(len(raw))
	for _, member := range self.census.Members {
		if member.Name != name {
			total += member.Size
		}
	}
	if total > maximumBootstrapSuccessorMemberTotalBytes {
		return mainnetDurableUnavailable("successor member byte capacity is exhausted", nil)
	}
	next := self.census
	next.Pending = &expected
	return self.publish(next)
}

// The already synced stage inode is acknowledged before any payload write.
func (self *bootstrapSuccessorMembers) retainStage(name string, file *os.File) error {
	pending := self.census.Pending
	if pending == nil || pending.Append || pending.Stage != name || file == nil {
		return errors.New("successor stage lacks its retained publication reservation")
	}
	if err := self.check(); err != nil {
		return err
	}
	actual, err := self.observe(name)
	if err != nil {
		return self.observation("successor reserved stage cannot be observed", err, true)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &opened); err != nil {
		return mainnetDurableUnavailable("cannot inspect original successor stage", err)
	}
	if actual.Inode != opened.Ino || pending.StageInode != 0 && pending.StageInode != actual.Inode {
		return self.storage.identity("successor stage no longer names the original descriptor", nil)
	}
	if pending.StageInode == actual.Inode {
		return nil
	}
	copy := *pending
	copy.StageInode = actual.Inode
	next := self.census
	next.Pending = &copy
	return self.publish(next)
}

// Only a synced exact final member advances the complete census. This also
// recovers a lost acknowledgement after the original no-replace publication.
func (self *bootstrapSuccessorMembers) commit(name string, raw []byte) error {
	if err := self.check(); err != nil {
		return err
	}
	actual, err := self.observe(name)
	if err != nil {
		return self.observation("successor final member cannot be observed", err, true)
	}
	if actual.Size != int64(len(raw)) || actual.Sha256 != safeReleaseHash(raw) {
		return self.storage.identity("successor final member differs from its approved publication", nil)
	}
	if prior, exists := self.member(name); exists && prior == actual {
		return nil
	}
	if self.census.Pending == nil {
		return self.storage.identity("successor final member has no retained publication authority", nil)
	}
	pending := self.census.Pending
	if pending.Name != name || pending.Size != actual.Size || pending.Sha256 != actual.Sha256 {
		return errors.New("successor final member differs from its pending operation")
	}
	next := self.census
	next.Pending = nil
	next.Members = append([]bootstrapSuccessorMember{}, self.census.Members...)
	index := sort.Search(len(next.Members), func(i int) bool { return next.Members[i].Name >= name })
	if index < len(next.Members) && next.Members[index].Name == name {
		next.Members[index] = actual
	} else {
		next.Members = append(next.Members, bootstrapSuccessorMember{})
		copy(next.Members[index+1:], next.Members[index:])
		next.Members[index] = actual
	}
	if err := self.publish(next); err != nil {
		return err
	}
	delete(self.observedNameKVs, pending.Stage)
	return nil
}

// Recovery may authenticate an already-published fixed name before replaying
// later members. Only the exact outstanding publication gains an acknowledgement.
func (self *bootstrapSuccessorMembers) resumePublished(name string, raw []byte) error {
	if pending := self.census.Pending; pending == nil || pending.Append || pending.Name != name {
		return nil
	}
	if self.readOnly {
		return durablehead.ErrReadOnly
	}
	if err := self.file.Sync(); err != nil {
		return self.publicationError(err)
	}
	return self.commit(name, raw)
}

// A failed syscall after a member mutation may have lost its acknowledgement.
// Only a joined new owner may reconcile that exact pending operation.
func (self *bootstrapSuccessorMembers) publicationError(err error) error {
	if err == nil || errors.Is(err, durablevolume.ErrIdentity) {
		return err
	}
	return self.storage.snapshotError(errors.Join(errMainnetDurablePublicationUncertain, err))
}

// Close joins the census owner before its borrowed directory is released.
func (self *bootstrapSuccessorMembers) close() error {
	if self == nil {
		return nil
	}
	var err error
	if self.restoredHead != nil {
		err = self.restoredHead.close()
		self.restoredHead = nil
	}
	if self.head != nil {
		err = errors.Join(err, self.head.Close())
	}
	self.head = nil
	return err
}
