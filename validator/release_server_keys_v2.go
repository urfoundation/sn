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
	"net/url"

	"github.com/urnetwork/sdk"
)

// The same explicit control bound applies before reading or decoding. All
// response bodies close before return, including refusal, cancellation and EOF.
func readReleaseServerKeysV2(ctx context.Context, cfg *ReleaseConfig) (result map[uint64]map[byte]ed25519.PublicKey, resultErr error) {
	return readReleaseServerKeysV2WithCapture(ctx, cfg, nil)
}

func readReleaseServerKeysV2WithCapture(ctx context.Context, cfg *ReleaseConfig, retain func(ReleaseEvidenceV2CaptureSource, []byte) error) (result map[uint64]map[byte]ed25519.PublicKey, resultErr error) {
	client := &http.Client{Timeout: releaseHttpGetAttemptTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return readReleaseServerKeysV2WithCaptureAndRetry(ctx, cfg, retain, client, releaseHttpGetRetryHooks{})
}

// The public owner fixes client policy; tests inject only transport and timing.
// Successful decoded key sets are captured once after all retries have ended.
func readReleaseServerKeysV2WithCaptureAndRetry(ctx context.Context, cfg *ReleaseConfig, retain func(ReleaseEvidenceV2CaptureSource, []byte) error, client *http.Client, hooks releaseHttpGetRetryHooks) (result map[uint64]map[byte]ed25519.PublicKey, resultErr error) {
	if ctx == nil || cfg == nil || client == nil || len(cfg.Operators) == 0 || uint64(len(cfg.Operators)) > cfg.EvidenceV2.Bounds.MaxOperators {
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
		var encoded []byte
		err = retryReleaseHttpGet(ctx, func(attemptCtx context.Context) (resultErr error) {
			encoded = nil
			request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, endpoint.String(), nil)
			if err != nil {
				return err
			}
			request.Header.Set("Accept", "application/json")
			request.Header.Set("Accept-Encoding", "identity")
			response, err := client.Do(request)
			if err != nil {
				if response != nil && response.Body != nil {
					err = errors.Join(err, response.Body.Close())
				}
				return err
			}
			defer func() {
				resultErr = errors.Join(resultErr, response.Body.Close(), attemptCtx.Err())
				if resultErr != nil {
					encoded = nil
				}
			}()
			if response.StatusCode != http.StatusOK {
				return &releaseHttpGetStatusError{endpoint: endpoint.String(), status: response.StatusCode, retryAfter: attemptStreamHttpRetryAfter(response.Header)}
			}
			contentType, _, typeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if typeErr != nil || len(response.Header.Values("Content-Type")) != 1 || contentType != "application/json" ||
				response.Uncompressed || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Range") != "" || response.ContentLength > int64(limit) {
				return fmt.Errorf("release server-key endpoint returned invalid bounded JSON framing (status %d)", response.StatusCode)
			}
			encoded, err = io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
			if uint64(len(encoded)) > limit {
				return errors.Join(errors.New("release server-key response exceeds its control bound"), err)
			}
			if err != nil {
				return &url.Error{Op: "Read", URL: endpoint.String(), Err: err}
			}
			return nil
		}, hooks)
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
