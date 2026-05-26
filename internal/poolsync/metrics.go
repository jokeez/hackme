package poolsync

import (
	"sync/atomic"
	"time"
)

// Metrics tracks coordinator campaign registration outcomes (node → pool).
type Metrics struct {
	TotalAttempts  uint64 `json:"total_attempts"`
	TotalOK        uint64 `json:"total_ok"`
	TotalFail      uint64 `json:"total_fail"`
	TotalQueued    uint64 `json:"total_queued"`
	TotalRetries   uint64 `json:"total_retries"`
	LastLatencyMS  int64  `json:"last_latency_ms"`
	LastStatus     string `json:"last_status"` // ok | fail | queued
	LastError      string `json:"last_error,omitempty"`
	LastCampaignID string `json:"last_campaign_id,omitempty"`
	LastAtUnix     int64  `json:"last_at_unix"`
	PendingCount   int64  `json:"pending_count"`
}

var (
	mAttempts  atomic.Uint64
	mOK        atomic.Uint64
	mFail      atomic.Uint64
	mQueued    atomic.Uint64
	mRetries   atomic.Uint64
	mLastMS    atomic.Int64
	lastStatus atomic.Value // string
	lastError  atomic.Value // string
	lastCID    atomic.Value // string
	lastAt     atomic.Int64
)

func RecordQueued(campaignID string) {
	mQueued.Add(1)
	lastStatus.Store("queued")
	lastCID.Store(campaignID)
	lastAt.Store(time.Now().Unix())
}

func RecordAttempt() {
	mAttempts.Add(1)
}

func RecordRetry() {
	mRetries.Add(1)
}

func RecordOK(campaignID string, latency time.Duration) {
	mOK.Add(1)
	mLastMS.Store(latency.Milliseconds())
	lastStatus.Store("ok")
	lastError.Store("")
	lastCID.Store(campaignID)
	lastAt.Store(time.Now().Unix())
}

func RecordFail(campaignID string, latency time.Duration, err error) {
	mFail.Add(1)
	mLastMS.Store(latency.Milliseconds())
	lastStatus.Store("fail")
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	lastError.Store(msg)
	lastCID.Store(campaignID)
	lastAt.Store(time.Now().Unix())
}

func Snapshot() Metrics {
	st, _ := lastStatus.Load().(string)
	err, _ := lastError.Load().(string)
	cid, _ := lastCID.Load().(string)
	return Metrics{
		TotalAttempts:  mAttempts.Load(),
		TotalOK:        mOK.Load(),
		TotalFail:      mFail.Load(),
		TotalQueued:    mQueued.Load(),
		TotalRetries:   mRetries.Load(),
		LastLatencyMS:  mLastMS.Load(),
		LastStatus:     st,
		LastError:      err,
		LastCampaignID: cid,
		LastAtUnix:     lastAt.Load(),
	}
}
