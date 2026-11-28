// Public launch generation reaches the actual provider parser, device settings
// and Core signed-close producer. Every network and identity here is synthetic.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urnetwork/connect/v2026"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// Host selection is supplied separately; original approved preparation bytes
// remain unchanged when a provider's local installation directory is selected.
func newBootstrapProviderRoleFixture(t *testing.T) (*bootstrapChainFixture, bootstrapProviderRoleRequest, string, string) {
	t.Helper()
	f, _ := newBootstrapContractRoleFixture(t)
	directory := filepath.Dir(f.path)
	request := bootstrapProviderRoleRequest{Schema: bootstrapProviderRoleSchema, PreparationHash: f.preparation.Plan.ContentHash}
	for index, operator := range f.validators[0].config.Operators {
		id := []string{"provider-a", "provider-b"}[index]
		request.Providers = append(request.Providers, bootstrapProviderRoleHost{Id: id, NoId: operator.NoID, ApiUrl: operator.APIURL,
			ConnectUrl: operator.ConnectURL, StateDirectory: filepath.Join(directory, id, "state")})
	}
	path, output := filepath.Join(directory, "provider-hosts.json"), filepath.Join(directory, "provider-domains")
	bootstrapRootTestWrite(t, path, request)
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	return f, request, path, output
}

// This is the actual command vector used by all public exporter tests.
func bootstrapProviderRoleArgs(f *bootstrapChainFixture, request, output string) []string {
	return []string{"bootstrap-chain", "provider-role-config", "--config", f.path, "--providers", request, "--output-dir", output}
}

// Retains actual serialized Core reports rather than inventing signed outcomes.
type bootstrapProviderReportOob struct {
	stateLock sync.Mutex
	reports   []*coreprotocol.CloseContract
}

// Copies borrowed frames before completing the ordinary control acknowledgment.
func (self *bootstrapProviderReportOob) SendControl(frames []*coreprotocol.Frame, callback connect.OobResultFunction) {
	self.stateLock.Lock()
	for _, frame := range frames {
		message, err := connect.FromFrame(frame)
		if report, ok := message.(*coreprotocol.CloseContract); err == nil && ok {
			self.reports = append(self.reports, proto.Clone(report).(*coreprotocol.CloseContract))
		}
	}
	self.stateLock.Unlock()
	if callback != nil {
		callback(nil, nil)
	}
}

// The complete admitted domain reaches the real device settings and a real
// cleanup report. Different operators cannot borrow each other's namespace.
func TestBootstrapProviderRoleFilesReachActualSignedProviderReport(t *testing.T) {
	f, _, request, output := newBootstrapProviderRoleFixture(t)
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, request, output), &stdout, &stderr); code != 0 {
		t.Fatal("provider launch public generation failed", code, stderr.String())
	}
	var result bootstrapProviderRoleConfig
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	seal := result.ContentHash
	result.ContentHash = ""
	if seal != rootObjectHash(result) || result.PreparationHash != f.preparation.Plan.ContentHash || result.ActivationReady || result.NetworkEffects || len(result.ProviderDeclarations) != 2 {
		t.Fatal("provider launch changed original preparation or invented activation")
	}
	var previous [32]byte
	for _, launch := range result.ProviderDeclarations {
		raw, err := os.ReadFile(launch.DomainFile.Path)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		if launch.DomainFile.Sha256 != "sha256:"+hex.EncodeToString(hash[:]) || launch.Domain != launch.EnrollmentDomain ||
			!reflect.DeepEqual(launch.Arguments, []string{"provide", "--api_url=" + launch.Host.ApiUrl, "--connect_url=" + launch.Host.ConnectUrl, "--close-report-domain=" + launch.DomainFile.Path}) ||
			launch.Environment["URNETWORK_STATE_DIR"] != launch.Host.StateDirectory {
			t.Fatal("provider manifest lost original enrollment domain or actual command wiring")
		}
		domain, err := miner.ReadProviderCloseReportDomain(launch.DomainFile.Path)
		want, digestErr := launch.Domain.Digest()
		if err != nil || digestErr != nil || domain != want || domain == previous {
			t.Fatal("provider parser borrowed or lost a launch domain", err, digestErr)
		}
		previous = domain
		deviceSettings := miner.ProviderDeviceSettings(domain)
		settings := deviceSettings.ClientSettings
		settings.ControlPingTimeout = 0
		settings.Log = connect.NewNoopLogger()
		owner, stopOwner := context.WithTimeout(t.Context(), 10*time.Second)
		ctx, cancel := context.WithCancel(owner)
		oob := &bootstrapProviderReportOob{}
		client := connect.NewClient(ctx, connect.NewId(), oob, &settings)
		cancel()
		if err := client.CloseAndWait(owner); err != nil {
			stopOwner()
			t.Fatal("provider client did not join", err)
		}
		client.ContractManager().CloseContract(connect.NewId(), 121, 7)
		stopOwner()
		oob.stateLock.Lock()
		reports := append([]*coreprotocol.CloseContract(nil), oob.reports...)
		oob.stateLock.Unlock()
		if len(reports) != 1 {
			t.Fatal("actual provider obligation did not reach original close transport", len(reports))
		}
		original, err := coreprotocol.DecodeOriginalCloseReport(reports[0].OriginalReport)
		if err != nil || original.DomainHash != want || !original.Matches([16]byte(client.ClientId()), reports[0]) {
			t.Fatal("generated provider domain did not reach an actual signed original report", err)
		}
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !reflect.DeepEqual(reads, f.contracts.counts) {
		t.Fatal("optional provider export changed preparation custody or contacted the chain")
	}
}

// Local file completion and lost output are idempotent across original
// preparation apply/resume; no original plan must be rebuilt for this export.
func TestBootstrapProviderRoleFilesRetryAfterOutputLossAndPreparation(t *testing.T) {
	f, _, request, output := newBootstrapProviderRoleFixture(t)
	args := bootstrapProviderRoleArgs(f, request, output)
	if code := runMain(f.storageContext(t.Context()), args, bootstrapContractRoleFailedOutput{}, io.Discard); code != 1 {
		t.Fatal("provider output failure was not retained", code)
	}
	var first, second bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), args, &first, io.Discard); code != 0 {
		t.Fatal("exact retained provider files did not recover", code)
	}
	prepared := f.result(t, "apply")
	if code := runMain(f.storageContext(t.Context()), args, &second, io.Discard); code != 0 || !bytes.Equal(first.Bytes(), second.Bytes()) || !reflect.DeepEqual(prepared, f.result(t, "resume")) {
		t.Fatal("provider retry altered the original preparation", code)
	}
}

// Provider-selected endpoints and operator IDs cannot create a new enrollment
// authority. Nothing is published for a missing or foreign selection.
func TestBootstrapProviderRoleRefusesForeignOperatorAndRoute(t *testing.T) {
	f, request, path, output := newBootstrapProviderRoleFixture(t)
	original := request.Providers[0]
	for _, foreign := range []bootstrapProviderRoleHost{
		{Id: original.Id, NoId: 999, ApiUrl: original.ApiUrl, ConnectUrl: original.ConnectUrl, StateDirectory: original.StateDirectory},
		{Id: original.Id, NoId: original.NoId, ApiUrl: "https://foreign.example", ConnectUrl: original.ConnectUrl, StateDirectory: original.StateDirectory},
		{Id: original.Id, NoId: original.NoId, ApiUrl: original.ApiUrl, ConnectUrl: "wss://foreign.example", StateDirectory: original.StateDirectory},
	} {
		request.Providers[0] = foreign
		bootstrapRootTestWrite(t, path, request)
		var stdout, stderr bytes.Buffer
		if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, output), &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "operator or routes differ") {
			t.Fatal("foreign provider selection reached launch publication", code, stderr.String())
		}
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused provider selection left domain files", err)
	}
}

// Both inputs remain correctly signed; route disagreement must be detected
// separately from signature admission and cannot borrow the first producer.
func TestBootstrapProviderRoleRefusesSignedOperatorDisagreement(t *testing.T) {
	f, request, path, output := newBootstrapProviderRoleFixture(t)
	f.validators[1].config.Operators[0].APIURL = "https://different-operator.example"
	bootstrapContractRolePublish(t, f)
	request.PreparationHash = f.preparation.Plan.ContentHash
	bootstrapRootTestWrite(t, path, request)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, output), &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "signed operator domain or routes disagree") {
		t.Fatal("two signed operator routes were silently merged", code, stderr.String())
	}
}

// An old inventory cannot select current policy implicitly. Canceled export and
// requests for overlapping custody leave all original observations untouched.
func TestBootstrapProviderRoleRequiresExactInventoryAndSeparateCustody(t *testing.T) {
	f, request, path, output := newBootstrapProviderRoleFixture(t)
	args := bootstrapProviderRoleArgs(f, path, output)
	original := request.PreparationHash
	request.PreparationHash = "sha256:" + strings.Repeat("aa", 32)
	bootstrapRootTestWrite(t, path, request)
	if code := runMain(f.storageContext(t.Context()), args, io.Discard, io.Discard); code != 2 {
		t.Fatal("stale provider preparation was admitted", code)
	}
	request.PreparationHash = original
	bootstrapRootTestWrite(t, path, request)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := runMain(f.storageContext(ctx), args, io.Discard, io.Discard); code == 0 {
		t.Fatal("canceled provider export reported success")
	}
	var stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, path, f.config.RunDirectory), io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "output overlaps") {
		t.Fatal("provider files overlapped retained preparation", code, stderr.String())
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused or canceled provider export mutated output", err)
	}
}

// Exact retained output is preserved; a different domain or symlink cannot be
// repaired by overwriting it with today's otherwise valid launch declaration.
func TestBootstrapProviderRoleRefusesChangedAndLinkedRetainedFile(t *testing.T) {
	f, _, request, output := newBootstrapProviderRoleFixture(t)
	result, err := loadBootstrapProviderRoleConfig(f.storageContext(t.Context()), f.path, request, output)
	if err != nil {
		t.Fatal(err)
	}
	first := result.ProviderDeclarations[0]
	changed := first.Domain
	changed.NoID++
	raw, _ := json.Marshal(changed)
	if err := os.WriteFile(first.DomainFile.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, linked := range []bool{false, true} {
		if linked {
			if err := os.Rename(first.DomainFile.Path, first.DomainFile.Path+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(first.DomainFile.Path+".original", first.DomainFile.Path); err != nil {
				t.Fatal(err)
			}
		}
		var stdout bytes.Buffer
		if code := runMain(f.storageContext(t.Context()), bootstrapProviderRoleArgs(f, request, output), &stdout, io.Discard); code != 3 || stdout.Len() != 0 {
			t.Fatal("foreign retained provider file was replaced", code, linked)
		}
		if retained, err := os.ReadFile(first.DomainFile.Path); err != nil || !bytes.Equal(retained, raw) {
			t.Fatal("provider refusal overwrote existing evidence", err)
		}
	}
}
