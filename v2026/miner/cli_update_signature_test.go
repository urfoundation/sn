package miner

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// signedReleaseCase serves a release over httptest: the archive, and
// optionally SHA256SUMS and SHA256SUMS.sig, signed by a key made here.
type signedReleaseCase struct {
	sums     func(name string, archive []byte) string // nil: no SHA256SUMS
	sign     bool                                     // false: no SHA256SUMS.sig
	badSig   bool
	required bool
}

func runSignedReleaseCase(t *testing.T, c signedReleaseCase) (error, string) {
	t.Helper()
	phase2State(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previousKey, previousRequired, previousWarn := minerUpdatePublicKey, requireUpdateSignature, minerUpdateWarn
	warn := &bytes.Buffer{}
	minerUpdatePublicKey, requireUpdateSignature, minerUpdateWarn = public, c.required, warn
	t.Cleanup(func() {
		minerUpdatePublicKey, requireUpdateSignature, minerUpdateWarn = previousKey, previousRequired, previousWarn
	})

	name := "urnetwork-provider-" + strings.TrimPrefix(updaterTestNewerTag, "v") + ".tar.gz"
	archive := updaterTestArchive(t, updaterTestNewerStamp)
	files := map[string][]byte{"/" + name: archive}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256(archive)
	assets := []any{map[string]any{
		"name":                 name,
		"digest":               "sha256:" + hex.EncodeToString(digest[:]),
		"size":                 len(archive),
		"browser_download_url": server.URL + "/" + name,
	}}
	add := func(asset string, body []byte) {
		files["/"+asset] = body
		assets = append(assets, map[string]any{"name": asset, "browser_download_url": server.URL + "/" + asset})
	}
	if c.sums != nil {
		sums := []byte(c.sums(name, archive))
		add("SHA256SUMS", sums)
		if c.sign {
			signed := sums
			if c.badSig {
				signed = append([]byte("x"), sums...)
			}
			add("SHA256SUMS.sig", []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, signed))+"\n"))
		}
	}
	feed, _ := json.Marshal(map[string]any{
		"tag_name":     updaterTestNewerTag,
		"published_at": time.Now().UTC().Format(time.RFC3339),
		"assets":       assets,
	})
	files["/release.json"] = feed
	t.Setenv("URNETWORK_UPDATE_URL", server.URL+"/release.json")

	release, err := fetchMinerRelease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	executable := fakeExecutable(t)
	err = installMinerRelease(t.Context(), release, executable)
	raw, _ := os.ReadFile(executable)
	if installed := strings.Contains(string(raw), updaterTestNewerStamp); installed != (err == nil) {
		t.Fatalf("installed=%v but err=%v", installed, err)
	}
	return err, warn.String()
}

func goodSums(name string, archive []byte) string {
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:]) + "  other.tar.gz\n" + hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
}

func TestUpdateSignedReleaseInstalls(t *testing.T) {
	skipOnWindows(t)
	err, warn := runSignedReleaseCase(t, signedReleaseCase{sums: goodSums, sign: true, required: true})
	if err != nil || warn != "" {
		t.Fatalf("a signed release: err=%v warn=%q", err, warn)
	}
}

func TestUpdateBadSignatureRefused(t *testing.T) {
	skipOnWindows(t)
	err, _ := runSignedReleaseCase(t, signedReleaseCase{sums: goodSums, sign: true, badSig: true})
	if err == nil || !strings.Contains(err.Error(), "doesn't verify") {
		t.Fatalf("a bad signature: err=%v", err)
	}
}

func TestUpdateSignedHashMismatchRefused(t *testing.T) {
	skipOnWindows(t)
	err, _ := runSignedReleaseCase(t, signedReleaseCase{sign: true, sums: func(name string, archive []byte) string {
		return strings.Repeat("ab", 32) + "  " + name + "\n"
	}})
	if err == nil || !strings.Contains(err.Error(), "signed SHA256SUMS line") {
		t.Fatalf("a signed hash that doesn't match: err=%v", err)
	}
}

func TestUpdateSignedMissingLineRefused(t *testing.T) {
	skipOnWindows(t)
	err, _ := runSignedReleaseCase(t, signedReleaseCase{sign: true, sums: func(name string, archive []byte) string {
		sum := sha256.Sum256(archive)
		return hex.EncodeToString(sum[:]) + "  other.tar.gz\n"
	}})
	if err == nil || !strings.Contains(err.Error(), "no line for") {
		t.Fatalf("no SHA256SUMS line: err=%v", err)
	}
}

func TestUpdateUnsignedWarnsWhenNotRequired(t *testing.T) {
	skipOnWindows(t)
	err, warn := runSignedReleaseCase(t, signedReleaseCase{sums: goodSums})
	if err != nil || !strings.Contains(warn, "isn't signed") {
		t.Fatalf("an unsigned release, not required: err=%v warn=%q", err, warn)
	}
}

func TestUpdateUnsignedRefusedWhenRequired(t *testing.T) {
	skipOnWindows(t)
	err, _ := runSignedReleaseCase(t, signedReleaseCase{required: true})
	if err == nil || !strings.Contains(err.Error(), "isn't signed") {
		t.Fatalf("an unsigned release, required: err=%v", err)
	}
}

func TestUpdateSignatureDefaultIsNotRequired(t *testing.T) {
	if requireUpdateSignature {
		t.Fatal("requireUpdateSignature should start false (staged rollout)")
	}
	if len(minerUpdatePublicKey) != ed25519.PublicKeySize {
		t.Fatal("the embedded public key isn't an ed25519 key")
	}
}
