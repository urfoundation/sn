package validator

// artifact.go is the validator's bounded, public operator-artifact reader. It
// resolves one exact (deployment, netuid, epoch, no_id) history prefix, rejects
// equivocation, and runs the shared canonical reconstruction before returning
// any usage value to steering.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfoundation/sn/payoutartifact"
)

const (
	maximumArtifactHistoryBytes = 16 * 1024 * 1024
	maximumPayoutArtifactBytes  = 32 * 1024 * 1024
	maximumExactArtifactObjects = 8
)

var (
	ErrArtifactUnavailable  = errors.New("operator payout artifact is unavailable")
	ErrArtifactEquivocation = errors.New("operator payout artifact history equivocated")
)

// ArtifactReader returns one canonical artifact for an exact operator epoch.
type ArtifactReader interface {
	Read(ctx context.Context, epoch uint64, noID uint64) (*payoutartifact.Artifact, error)
}

// HTTPArtifactReader reads public history and immutable content from one
// operator API. It is safe for concurrent use.
type HTTPArtifactReader struct {
	baseURL      *url.URL
	deploymentID string
	netuid       uint16
	client       *http.Client
}

// NewHTTPArtifactReader pins the deployment identity and disables redirects so
// an operator cannot silently move validation onto a different origin.
func NewHTTPArtifactReader(apiURL, deploymentID string, netuid uint16) (*HTTPArtifactReader, error) {
	if err := validateEndpoint("artifact api_url", apiURL, "http", "https"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(deploymentID) == "" || strings.ContainsAny(deploymentID, "/\\.") || netuid == 0 {
		return nil, errors.New("artifact deployment identity is invalid")
	}
	baseURL, err := url.Parse(apiURL)
	if err != nil {
		return nil, err
	}
	return &HTTPArtifactReader{
		baseURL: baseURL, deploymentID: deploymentID, netuid: netuid,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (self *HTTPArtifactReader) endpoint(path string, query url.Values) string {
	resolved := self.baseURL.ResolveReference(&url.URL{Path: path})
	resolved.RawQuery = query.Encode()
	return resolved.String()
}

func (self *HTTPArtifactReader) get(ctx context.Context, endpoint string, maximumBytes int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := self.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", endpoint, response.StatusCode)
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		return nil, fmt.Errorf("%s returned content type %q", endpoint, mediaType)
	}
	if response.ContentLength > maximumBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", endpoint, maximumBytes)
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, maximumBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > maximumBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", endpoint, maximumBytes)
	}
	return value, nil
}

type artifactHistoryObject struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
}

type artifactHistoryResponse struct {
	Schema  string                  `json:"schema"`
	Objects []artifactHistoryObject `json:"objects"`
}

func decodeArtifactHistory(value []byte) (*artifactHistoryResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var history artifactHistoryResponse
	if err := decoder.Decode(&history); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("artifact history contains a trailing JSON value")
		}
		return nil, err
	}
	if history.Schema != "urnetwork-payout-artifact-history-v1" {
		return nil, fmt.Errorf("unsupported artifact history schema %q", history.Schema)
	}
	return &history, nil
}

func artifactObjectHash(object artifactHistoryObject) (string, error) {
	if object.Size < 0 || !payoutartifact.IsDigest(object.ContentHash, "sha256:") {
		return "", errors.New("artifact history object has invalid size or content hash")
	}
	hash := strings.TrimPrefix(strings.ToLower(object.ContentHash), "sha256:")
	if filepath.Ext(object.Key) != ".json" || !strings.EqualFold(strings.TrimSuffix(filepath.Base(object.Key), ".json"), hash) {
		return "", errors.New("artifact history key does not match its content hash")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", errors.New("artifact history hash is not hex")
	}
	return "sha256:" + hash, nil
}

// Read rejects multiple distinct content identities even when each artifact is
// individually valid: a signed operator equivocation cannot pick a winner by
// object-list ordering.
func (self *HTTPArtifactReader) Read(ctx context.Context, epoch uint64, noID uint64) (*payoutartifact.Artifact, error) {
	if noID == 0 {
		return nil, errors.New("artifact no_id is zero")
	}
	historyEndpoint := self.endpoint("/sn/artifacts", url.Values{
		"deployment_id": []string{self.deploymentID},
		"netuid":        []string{fmt.Sprint(self.netuid)},
		"epoch":         []string{fmt.Sprint(epoch)},
		"no_id":         []string{fmt.Sprint(noID)},
	})
	value, err := self.get(ctx, historyEndpoint, maximumArtifactHistoryBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactUnavailable, err)
	}
	history, err := decodeArtifactHistory(value)
	if err != nil {
		return nil, fmt.Errorf("artifact history integrity: %w", err)
	}
	if len(history.Objects) > maximumExactArtifactObjects {
		return nil, fmt.Errorf("%w: exact history contains %d objects", ErrArtifactEquivocation, len(history.Objects))
	}
	hashes := map[string]bool{}
	sizes := map[string]int64{}
	expectedKeySuffix := fmt.Sprintf("/st/v1/history/%s/%d/%d/%d/", self.deploymentID, self.netuid, epoch, noID)
	for _, object := range history.Objects {
		hash, hashErr := artifactObjectHash(object)
		if hashErr != nil {
			return nil, hashErr
		}
		if !strings.HasSuffix(filepath.ToSlash(object.Key), expectedKeySuffix+strings.TrimPrefix(hash, "sha256:")+".json") {
			return nil, errors.New("artifact history object is outside the requested identity")
		}
		hashes[hash] = true
		sizes[hash] = object.Size
	}
	if len(hashes) == 0 {
		return nil, ErrArtifactUnavailable
	}
	if len(hashes) != 1 {
		return nil, fmt.Errorf("%w: exact history contains %d content identities", ErrArtifactEquivocation, len(hashes))
	}
	if len(history.Objects) != 1 {
		return nil, fmt.Errorf("artifact history contains %d duplicate objects for one identity", len(history.Objects))
	}
	var contentHash string
	for hash := range hashes {
		contentHash = hash
	}
	contentEndpoint := self.endpoint("/sn/artifact", url.Values{"hash": []string{contentHash}})
	value, err = self.get(ctx, contentEndpoint, maximumPayoutArtifactBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactUnavailable, err)
	}
	if sizes[contentHash] != int64(len(value)) {
		return nil, errors.New("artifact content size does not match history")
	}
	artifact, err := payoutartifact.Decode(value)
	if err != nil {
		return nil, fmt.Errorf("artifact integrity: %w", err)
	}
	if !strings.EqualFold(artifact.ContentHash, contentHash) {
		return nil, errors.New("artifact content response does not match history")
	}
	return artifact, nil
}
