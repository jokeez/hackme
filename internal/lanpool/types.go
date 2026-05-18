package lanpool

// MetricsRow is one rig for /api/metrics.mining_rigs and pool tables.
type MetricsRow struct {
	WorkerID       string  `json:"worker_id"`
	Name           string  `json:"name"`
	HashrateGHS    float64 `json:"hashrate_gh_s"`
	LastSeenUnix   int64   `json:"last_seen_unix"`
	IP             string  `json:"ip"`
	Online         bool    `json:"online"`
	Source         string  `json:"source"` // "local" | "remote" | "local-gpu"
	GPUBackend     string  `json:"gpu_backend,omitempty"`
	GPUTempC       float64 `json:"gpu_temp_c,omitempty"`
	SharesAccepted uint64  `json:"shares_accepted,omitempty"`
}

// NetworkStatsResponse is GET /api/network/stats.
type NetworkStatsResponse struct {
	TotalMiners       int             `json:"total_miners"`
	GlobalHashrateTHS float64         `json:"global_hashrate_th_s"`
	PeerConnections   int             `json:"peer_connections"`
	TopMiners         []string        `json:"top_miners"`
	ActiveRigs        []NetworkRigRow `json:"active_rigs"`
	GlobalMock        bool            `json:"global_mock"`
	Note              string          `json:"note,omitempty"`
}

// NetworkRigRow is one fleet row in network stats.
type NetworkRigRow struct {
	MetricsRow
	Simulated bool `json:"simulated,omitempty"`
}
