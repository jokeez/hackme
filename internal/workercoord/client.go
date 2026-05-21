// Package workercoord implements the pool worker HTTP client used against /api/work/*.
// Logic mirrors cmd/workerpoh claim/submit backoff behavior for testability.
package workercoord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ClaimResponse is the coordinator claim JSON body.
type ClaimResponse struct {
	OK         bool   `json:"ok"`
	Reason     string `json:"reason,omitempty"`
	BaseNonce  uint64 `json:"base_nonce"`
	BatchSize  uint64 `json:"batch_size"`
	WorkID     string `json:"work_id"`
	TargetMod  uint64 `json:"target_mod"`
	LeaseUntil int64  `json:"lease_until,omitempty"`
}

// SubmitRequest matches worker submit payload (subset used in tests).
type SubmitRequest struct {
	WorkerID    string  `json:"worker_id"`
	BaseNonce   uint64  `json:"base_nonce"`
	BatchSize   uint64  `json:"batch_size"`
	WorkID      string  `json:"work_id"`
	Attempts    uint64  `json:"attempts"`
	Found       bool    `json:"found"`
	FoundNonce  uint64  `json:"found_nonce,omitempty"`
	HashrateGHS float64 `json:"hashrate_gh_s,omitempty"`
	MinerPubKey string  `json:"miner_pubkey,omitempty"`
	MinerSig    string  `json:"miner_sig,omitempty"`
	MinerSigAlg string  `json:"miner_sig_alg,omitempty"`
	SubmitNonce uint64  `json:"submit_nonce,omitempty"`
}

// SubmitResponse is the coordinator submit JSON body.
type SubmitResponse struct {
	OK         bool    `json:"ok"`
	Accepted   bool    `json:"accepted"`
	Reason     string  `json:"reason,omitempty"`
	PayoutHMC  float64 `json:"payout_hmc"`
	StatusCode int     `json:"-"`
	RawBody    string  `json:"-"`
}

// Client talks to the coordinator with retry/backoff compatible with workerpoh.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client

	Backoff time.Duration
}

// NewHTTPClient builds a worker-style HTTP client with bounded timeouts.
func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	hdrTimeout := timeout - 2*time.Second
	if hdrTimeout < 3*time.Second {
		hdrTimeout = 3 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   12 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   12 * time.Second,
			ResponseHeaderTimeout: hdrTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       45 * time.Second,
		},
	}
}

// SleepBackoff doubles backoff up to 45s (workerpoh sleepWorkerBackoff).
func (c *Client) SleepBackoff(kind string) time.Duration {
	_ = kind
	wait := c.Backoff
	if wait < 2*time.Second {
		wait = 2 * time.Second
	}
	next := wait * 2
	if next > 45*time.Second {
		next = 45 * time.Second
	}
	c.Backoff = next
	return wait
}

func (c *Client) ResetBackoff() {
	c.Backoff = 2 * time.Second
}

func (c *Client) postJSON(path string, body any) (int, []byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, raw, err
}

// Claim requests a work lease.
func (c *Client) Claim(workerID string, batchSize uint64) (ClaimResponse, error) {
	code, raw, err := c.postJSON("/api/work/claim", map[string]any{
		"worker_id":  workerID,
		"batch_size": batchSize,
	})
	if err != nil {
		return ClaimResponse{}, err
	}
	var out ClaimResponse
	_ = json.Unmarshal(raw, &out)
	if code != http.StatusOK || !out.OK {
		if out.Reason == "" {
			out.Reason = fmt.Sprintf("http_%d", code)
		}
		return out, fmt.Errorf("claim rejected: %s", out.Reason)
	}
	return out, nil
}

// Submit reports work; returns response even on HTTP non-200 for caller policy checks.
func (c *Client) Submit(req SubmitRequest) (SubmitResponse, error) {
	code, raw, err := c.postJSON("/api/work/submit", req)
	out := SubmitResponse{StatusCode: code, RawBody: string(raw)}
	if err != nil {
		return out, err
	}
	_ = json.Unmarshal(raw, &out)
	out.StatusCode = code
	if code != http.StatusOK {
		return out, fmt.Errorf("submit http: %d", code)
	}
	return out, nil
}

// ClaimWithRetry attempts claim until success or maxAttempts; returns attempts and final backoff.
func (c *Client) ClaimWithRetry(workerID string, batchSize uint64, maxAttempts int) (ClaimResponse, int, error) {
	c.ResetBackoff()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cr, err := c.Claim(workerID, batchSize)
		if err == nil {
			c.ResetBackoff()
			return cr, attempt, nil
		}
		lastErr = err
		time.Sleep(c.SleepBackoff("claim"))
	}
	return ClaimResponse{}, maxAttempts, lastErr
}
