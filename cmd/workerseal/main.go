// HMS seal worker — CPU SHA256 grind (ASIC via Stratum when HMS_STRATUM_ENABLE on coordinator).
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/hms"
)

func main() {
	coord := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_HMS_COORDINATOR_URL")), "/")
	if coord == "" {
		coord = "http://127.0.0.1:18082"
	}
	workerID := strings.TrimSpace(os.Getenv("HACKME_WORKER_ID"))
	if workerID == "" {
		workerID = "worker-hms-seal"
	}
	token := strings.TrimSpace(os.Getenv("HACKME_HMS_WORKER_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_WORKER_TOKEN"))
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatal(err)
	}
	pubHex := hex.EncodeToString(pub)
	_ = postJSON(coord+"/api/seal/register", token, map[string]any{
		"worker_id": workerID, "pubkey_hex": pubHex,
	}, nil)
	log.Printf("seal worker %s registered", workerID)

	for {
		work, err := getJSON(coord+"/api/seal/work", token)
		if err != nil {
			log.Printf("seal work: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		epochID := int64(work["epoch_id"].(float64))
		rootHex, _ := work["manifest_root"].(string)
		targetHex, _ := work["target"].(string)
		root, _ := hex.DecodeString(rootHex)
		target, _ := hex.DecodeString(targetHex)
		var root32 [32]byte
		copy(root32[32-len(root):], root)
		target32 := make([]byte, 32)
		copy(target32[32-len(target):], target)
		poolID, _ := work["pool_id"].(string)
		if poolID == "" {
			poolID = "hackme-official"
		}
		found := grind(epochID, root32, poolID, target32)
		if found == nil {
			time.Sleep(2 * time.Second)
			continue
		}
		payload := hms.SealSubmitPayload{WorkerID: workerID, EpochID: epochID, Nonce: found.nonce}
		body, _ := json.Marshal(payload)
		sig := ed25519.Sign(priv, body)
		req := map[string]any{}
		_ = json.Unmarshal(body, &req)
		req["pubkey_hex"] = pubHex
		req["sig_hex"] = hex.EncodeToString(sig)
		if err := postJSON(coord+"/api/seal/submit", token, req, nil); err != nil {
			log.Printf("seal submit: %v", err)
		} else {
			log.Printf("seal accepted epoch=%d nonce=%d", epochID, found.nonce)
			time.Sleep(time.Minute)
		}
	}
}

type grindResult struct{ nonce uint64 }

func grind(epochID int64, root [32]byte, poolID string, target []byte) *grindResult {
	deadline := time.Now().Add(3 * time.Second)
	for n := uint64(0); time.Now().Before(deadline); n++ {
		h := hms.SealHash(epochID, root, poolID, n)
		if hms.HashBelowTarget(h[:], target) {
			return &grindResult{nonce: n}
		}
	}
	return nil
}

func getJSON(url, token string) (map[string]any, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out map[string]any
	return out, json.Unmarshal(raw, &out)
}

func postJSON(url, token string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
