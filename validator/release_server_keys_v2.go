//go:build linux || darwin

// The configured operator's real public verify-key endpoint supplies replay
// keys. It is independent of validator VPKs and destination upload credentials.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/urnetwork/sdk"
)

// The same explicit control bound applies before reading or decoding. All
// response bodies close before return, including refusal, cancellation and EOF.
func readReleaseServerKeysV2(ctx context.Context, cfg *ReleaseConfig) (result map[uint64]map[byte]ed25519.PublicKey, resultErr error) {
	return readReleaseServerKeysV2WithCapture(ctx, cfg, nil)
}

func readReleaseServerKeysV2WithCapture(ctx context.Context, cfg *ReleaseConfig, retain func(ReleaseEvidenceV2CaptureSource, []byte) error) (result map[uint64]map[byte]ed25519.PublicKey, resultErr error) {
	if ctx == nil || cfg == nil || len(cfg.Operators) == 0 || uint64(len(cfg.Operators)) > cfg.EvidenceV2.Bounds.MaxOperators {
		return nil, errors.New("release server-key census is incomplete")
	}
	limit := cfg.EvidenceV2.Bounds.MaxControlBytes
	if err := validateReleaseMeasurementInputV2Limit(limit); err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	result = make(map[uint64]map[byte]ed25519.PublicKey, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		if operator.NoID == 0 || result[operator.NoID] != nil {
			return nil, errors.New("release server-key operator is duplicated")
		}
		reader, err := NewHTTPAttemptStreamV2Reader(operator.APIURL, cfg.EvidenceV2.Bounds.Cut)
		if err != nil {
			return nil, err
		}
		endpoint := reader.endpoint
		endpoint.Path = "/verify/keys"
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Accept-Encoding", "identity")
		client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		encoded, err := func() (encoded []byte, resultErr error) {
			defer func() {
				resultErr = errors.Join(resultErr, response.Body.Close(), ctx.Err())
				if resultErr != nil {
					encoded = nil
				}
			}()
			contentType, _, typeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if response.StatusCode != http.StatusOK || typeErr != nil || len(response.Header.Values("Content-Type")) != 1 || contentType != "application/json" ||
				response.Uncompressed || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Range") != "" || response.ContentLength > int64(limit) {
				return nil, fmt.Errorf("release server-key endpoint returned invalid bounded JSON framing (status %d)", response.StatusCode)
			}
			encoded, resultErr = io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
			if uint64(len(encoded)) > limit {
				return nil, errors.New("release server-key response exceeds its control bound")
			}
			return encoded, resultErr
		}()
		if err != nil {
			return nil, err
		}
		keys, err := decodeReleaseServerKeysV2(encoded, limit)
		if err != nil {
			return nil, err
		}
		if retain != nil {
			if err := retain(ReleaseEvidenceV2CaptureSource{Kind: "server-keys", Name: fmt.Sprintf("no-%d", operator.NoID), Origin: operator.APIURL}, encoded); err != nil {
				return nil, err
			}
		}
		result[operator.NoID] = keys
	}
	return result, nil
}

func decodeReleaseServerKeysV2(encoded []byte, limit uint64) (map[byte]ed25519.PublicKey, error) {
	var value sdk.VerifyKeysResult
	if err := decodeAttemptStreamV2JSON(encoded, limit, &value); err != nil {
		return nil, err
	}
	if len(value.Keys) == 0 || len(value.Keys) > 256 {
		return nil, errors.New("release server-key response has an invalid version census")
	}
	keys := make(map[byte]ed25519.PublicKey, len(value.Keys))
	for _, key := range value.Keys {
		if key == nil || key.ServerKeyId < 0 || key.ServerKeyId > 255 || len(key.PublicKey) != ed25519.PublicKeySize || keys[byte(key.ServerKeyId)] != nil {
			return nil, errors.New("release server-key version is duplicated or has invalid width")
		}
		keys[byte(key.ServerKeyId)] = bytes.Clone(key.PublicKey)
	}
	return keys, nil
}
