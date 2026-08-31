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

	"hackme/internal/fuzzengine"
)

// UploadCorpusNamespace POSTs namespace corpus seeds to the coordinator pool API.
func UploadCorpusNamespace(ctx context.Context, coordURL, token, namespace string, seeds []fuzzengine.PoolCorpusSeed) error {
	coordURL = strings.TrimRight(strings.TrimSpace(coordURL), "/")
	namespace = strings.TrimSpace(namespace)
	if coordURL == "" || namespace == "" {
		return fmt.Errorf("pool corpus sync: coordinator url and namespace required")
	}
	if token == "" {
		return fmt.Errorf("pool corpus sync: coordinator admin token not set")
	}
	payloadSeeds := make([]map[string]any, 0, len(seeds))
	for _, s := range seeds {
		if s.Crash {
			continue
		}
		payloadSeeds = append(payloadSeeds, map[string]any{
			"input_u64":   s.Input,
			"input_bytes": base64.StdEncoding.EncodeToString(s.InputBytes),
			"energy":      s.Energy,
			"edge":        s.Edge,
			"path":        s.Path,
		})
	}
	if len(payloadSeeds) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"namespace": namespace,
		"seeds":     payloadSeeds,
	})
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeoutDuration())
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, coordURL+"/api/fuzz/pool/corpus/namespace", bytes.NewReader(body))
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
		return fmt.Errorf("pool corpus sync HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
