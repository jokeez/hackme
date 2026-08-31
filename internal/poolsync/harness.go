package poolsync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// UploadHuntHarness POSTs a published harness blob to the coordinator pool API.
func UploadHuntHarness(ctx context.Context, coordURL, token, hash string, data []byte, sourceRel string) error {
	coordURL = strings.TrimRight(strings.TrimSpace(coordURL), "/")
	hash = strings.TrimSpace(hash)
	if coordURL == "" || hash == "" {
		return fmt.Errorf("pool harness sync: coordinator url and hash required")
	}
	if token == "" {
		return fmt.Errorf("pool harness sync: coordinator admin token not set")
	}
	if len(data) == 0 {
		return fmt.Errorf("pool harness sync: empty harness")
	}
	body, err := json.Marshal(map[string]any{
		"harness_hash": hash,
		"source_rel":   strings.TrimSpace(sourceRel),
		"binary_b64":   base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeoutDuration())
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, coordURL+"/api/fuzz/pool/hunt/harness", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("pool harness sync HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
