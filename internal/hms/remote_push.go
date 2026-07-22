package hms

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// WorkerPushToken returns the dedicated token used only for coordinator→worker chunk push.
// Never falls back to shared worker/admin tokens (HMC-RES-03).
func WorkerPushToken() string {
	for _, k := range []string{"HMS_WORKER_PUSH_TOKEN", "HACKME_HMS_WORKER_PUSH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func requireRemotePush() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HMS_MARKET_REQUIRE_REMOTE_PUSH")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (c *Coordinator) workerEndpointURL(workerID string) (string, error) {
	workerID = trimWorkerID(workerID)
	var ep string
	err := c.db.QueryRow(`SELECT COALESCE(endpoint_url,'') FROM hms_workers WHERE worker_id=?`, workerID).Scan(&ep)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(ep), nil
}

// pushChunkToWorkerEndpoint PUTs ciphertext to a registered worker endpoint using
// HMS_WORKER_PUSH_TOKEN only. Empty endpoint is a no-op (same-host lab path).
func (c *Coordinator) pushChunkToWorkerEndpoint(workerID, chunkID string, ciphertext []byte) error {
	ep, err := c.workerEndpointURL(workerID)
	if err != nil {
		return err
	}
	if ep == "" {
		if requireRemotePush() {
			return fmt.Errorf("remote push required but worker %s has no endpoint_url", workerID)
		}
		return nil
	}
	norm, err := ValidateWorkerEndpointURL(ep)
	if err != nil {
		return fmt.Errorf("worker endpoint unsafe: %w", err)
	}
	token := WorkerPushToken()
	if token == "" {
		return fmt.Errorf("HMS_WORKER_PUSH_TOKEN required for remote chunk push")
	}
	if err := ValidateChunkID(chunkID); err != nil {
		return err
	}
	url := norm + "/api/worker/storage/chunks/" + chunkID
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(ciphertext))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-HMS-Worker-ID", workerID)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("remote push HTTP %d", resp.StatusCode)
	}
	return nil
}
