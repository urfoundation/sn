// The optional live-session source keeps its own original signing authority.
// Synthetic configured bytes exercise the actual production parser and WARP
// selection without signing any traffic or borrowing another role's key.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/server/v2026/model"
)

// The independent test key is confined to the selected original resource. It
// is unrelated to the fixture's ST EVM keys and incident approval authorities.
func repairOperatorSessionTestSource(t *testing.T, now time.Time, kind string) []byte {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x93}, ed25519.SeedSize))
	value := map[string]any{
		"schema": model.ProviderWorkSessionSourceSchema,
		"authority": map[string]any{
			"domain_hash": [32]byte{0x94}, "source_id": "00000000-0000-0000-0000-000000000001", "generation": "00000000-0000-0000-0000-000000000002",
			"public_key": [32]byte(key.Public().(ed25519.PublicKey)), "from_unix_micro": now.Add(-time.Hour).UnixMicro(), "through_unix_micro": now.Add(time.Hour).UnixMicro(),
			"max_endpoint_events": 4096, "max_cohort_members": 64, "directory_public_keys": [][32]byte{},
		},
		"private_key": []byte(key),
	}
	switch kind {
	case "wrong-key":
		value["private_key"] = []byte(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x95}, ed25519.SeedSize)))
	case "secret-shape":
		value["private_key"] = "private-input-must-not-appear-in-diagnostics"
	case "too-large":
		return bytes.Repeat([]byte{' '}, 64*1024+1)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A fixture's new resource must be admitted by both original signatures and
// complete physical tree pins before it can reach the selected-source check.
func repairOperatorSessionTestSign(t *testing.T, fixture *repairOperatorFixture) {
	t.Helper()
	p := &fixture.envelope.original.Plan
	repairOperatorTestServiceAccess(t, fixture.base.directory, *p)
	for index := range p.Resources {
		hash, err := inspectRepairOperatorTree(t.Context(), fixture.base.host, p.Resources[index], *p)
		if err != nil {
			t.Fatal(err)
		}
		p.Resources[index].Sha256 = hash
	}
	fixture.signHost()
	fixture.sign()
}

// Optional absence is allowed, while a configured source must parse under its
// own key. Literal precedence remains the actual Server resolver's decision.
func TestRepairOperatorOptionalSessionSourceUsesOriginalScopedResource(t *testing.T) {
	for _, kind := range []string{"absent", "valid", "lower-invalid", "higher-invalid", "wrong-key", "secret-shape", "too-large"} {
		f := newRepairOperatorFixture(t)
		p := f.envelope.original.Plan
		root := p.env("WARP_VAULT_HOME")
		path := filepath.Join(root, "main", "1.0.0", "provider_work_session.json")
		if kind != "absent" {
			raw := repairOperatorSessionTestSource(t, f.base.now, kind)
			if kind == "lower-invalid" {
				repairValidatorTestWrite(t, path, []byte("{}"), 0600)
				path = filepath.Join(root, "provider_work_session.json")
			}
			repairValidatorTestWrite(t, path, raw, 0600)
			if kind == "higher-invalid" {
				repairValidatorTestWrite(t, filepath.Join(root, "provider_work_session.json"), []byte("{}"), 0600)
			}
		}
		repairOperatorSessionTestSign(t, f)
		valid := kind == "absent" || kind == "valid" || kind == "lower-invalid"
		err := f.envelope.ready(f.ctx(), f.base.host, f.base.now)
		if !valid {
			if !errors.Is(err, errRpcIntegrity) || f.base.starts != 0 || strings.Contains(err.Error(), "private-input-must-not-appear-in-diagnostics") {
				t.Fatal("operator accepted or exposed a configured invalid optional session source", kind, err, f.base.starts)
			}
			continue
		}
		if err != nil {
			t.Fatal("valid optional source selection did not reach actual process recovery", kind, err)
		}
		f.claim()
		status, complete, err := f.resume()
		if err != nil || !complete || f.base.starts != 1 {
			t.Fatal("original optional source changed operator process ownership", kind, status, complete, err, f.base.starts)
		}
	}
}

// A temporary loss after an acknowledged start cannot become optional absence
// or a new start grant. Restoring those same original bytes and physical inode
// permits a later observation of the already acknowledged generation.
func TestRepairOperatorSessionSourceLossRetainsAcknowledgedGeneration(t *testing.T) {
	f := newRepairOperatorFixture(t)
	path := filepath.Join(f.envelope.original.Plan.env("WARP_VAULT_HOME"), "main", "1.0.0", "provider_work_session.json")
	repairValidatorTestWrite(t, path, repairOperatorSessionTestSource(t, f.base.now, "valid"), 0600)
	repairOperatorSessionTestSign(t, f)
	f.claim()
	f.status.Store("pending")
	status, complete, err := f.resume()
	if status != "waiting-progress" || complete || !errors.Is(err, errRepairProcessPending) || f.base.starts != 1 {
		t.Fatal("optional source fixture did not retain its acknowledged pending start", status, complete, err, f.base.starts)
	}
	retained := filepath.Join(f.base.directory, "retained-original-session-source.json")
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	status, complete, err = f.resume()
	if status != "source-refused" || complete || !errors.Is(err, errRpcIntegrity) || f.base.starts != 1 {
		t.Fatal("lost original configured source became optional absence or a renewed start", status, complete, err, f.base.starts)
	}
	if err := os.Rename(retained, path); err != nil {
		t.Fatal(err)
	}
	f.status.Store("ok")
	status, complete, err = f.resume()
	if err != nil || !complete || f.base.starts != 1 {
		t.Fatal("restored original source did not close the same acknowledged generation", status, complete, err, f.base.starts)
	}
}
