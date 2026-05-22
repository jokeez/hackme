package workercoord

import (
	"bytes"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// FaultConfig simulates poor miner uplink (latency + packet loss).
type FaultConfig struct {
	MinLatency time.Duration
	MaxLatency time.Duration
	DropRate   float64 // 0..1 fraction of requests dropped before hitting base
}

// FaultTransport wraps a base RoundTripper with latency and random drops.
type FaultTransport struct {
	Base   http.RoundTripper
	Config FaultConfig
	mu     sync.Mutex
	rng    *rand.Rand

	Calls     int
	Dropped   int
	Delivered int
}

func NewFaultTransport(base http.RoundTripper, cfg FaultConfig) *FaultTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if cfg.MinLatency < 0 {
		cfg.MinLatency = 0
	}
	if cfg.MaxLatency < cfg.MinLatency {
		cfg.MaxLatency = cfg.MinLatency
	}
	if cfg.DropRate < 0 {
		cfg.DropRate = 0
	}
	if cfg.DropRate > 1 {
		cfg.DropRate = 1
	}
	return &FaultTransport{
		Base:   base,
		Config: cfg,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (t *FaultTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.Calls++
	drop := t.rng.Float64() < t.Config.DropRate
	t.mu.Unlock()
	if drop {
		t.mu.Lock()
		t.Dropped++
		t.mu.Unlock()
		return nil, &netFaultError{msg: "simulated packet loss"}
	}
	delay := t.Config.MinLatency
	if t.Config.MaxLatency > t.Config.MinLatency {
		span := t.Config.MaxLatency - t.Config.MinLatency
		t.mu.Lock()
		delay += time.Duration(t.rng.Int63n(int64(span) + 1))
		t.mu.Unlock()
	}
	time.Sleep(delay)
	resp, err := t.Base.RoundTrip(req)
	if err == nil {
		t.mu.Lock()
		t.Delivered++
		t.mu.Unlock()
	}
	return resp, err
}

type netFaultError struct {
	msg string
}

func (e *netFaultError) Error() string   { return e.msg }
func (e *netFaultError) Timeout() bool   { return true }
func (e *netFaultError) Temporary() bool { return true }

// Snapshot returns transport counters.
func (t *FaultTransport) Snapshot() (calls, dropped, delivered int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Calls, t.Dropped, t.Delivered
}

// drainBody replays request body for retries in tests.
func drainBody(req *http.Request) error {
	if req.Body == nil {
		return nil
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	return nil
}
