package hms

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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

// DeriveWorkerPushBearer returns a per-worker push credential derived from the
// fleet master push token. Coordinator sends this (not the master) so a
// malicious registered endpoint_url cannot harvest a bearer that authorizes
// writes on every other storage listener.
func DeriveWorkerPushBearer(master, workerID string) string {
	master = strings.TrimSpace(master)
	workerID = trimWorkerID(workerID)
	if master == "" || workerID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(master))
	_, _ = mac.Write([]byte("hms-push-v1:" + workerID))
	return hex.EncodeToString(mac.Sum(nil))
}

// AuthorizeWorkerPushBearer accepts the derived per-worker bearer. The shared
// master is rejected unless HMS_WORKER_PUSH_ALLOW_SHARED=1 (legacy lab only).
func AuthorizeWorkerPushBearer(authorizationHeader, master, workerID string) bool {
	auth := strings.TrimSpace(authorizationHeader)
	master = strings.TrimSpace(master)
	if auth == "" || master == "" {
		return false
	}
	wantDerived := "Bearer " + DeriveWorkerPushBearer(master, workerID)
	if subtle.ConstantTimeCompare([]byte(auth), []byte(wantDerived)) == 1 {
		return true
	}
	if allowSharedWorkerPushBearer() {
		wantShared := "Bearer " + master
		return subtle.ConstantTimeCompare([]byte(auth), []byte(wantShared)) == 1
	}
	return false
}

func allowSharedWorkerPushBearer() bool {
	for _, k := range []string{"HMS_WORKER_PUSH_ALLOW_SHARED", "HACKME_HMS_WORKER_PUSH_ALLOW_SHARED"} {
		v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			return true
		}
	}
	return false
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
	bearer := DeriveWorkerPushBearer(token, workerID)
	if bearer == "" {
		return fmt.Errorf("derived push bearer empty")
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
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-HMS-Worker-ID", workerID)
	client := egressHTTPClient(60 * time.Second)
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
