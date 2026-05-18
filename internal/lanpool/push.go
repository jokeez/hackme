package lanpool

// PushWorkBody is POST /api/push_work JSON.
type PushWorkBody struct {
	WorkerID       string  `json:"worker_id"`
	Name           string  `json:"name"`
	HashrateGHS    float64 `json:"hashrate_gh_s"`
	IP             string  `json:"ip"`
	ShareAccepted  *bool   `json:"share_accepted"`
	SharesAccepted uint64  `json:"shares_accepted"`
}
