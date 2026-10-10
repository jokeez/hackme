package poolfuzz

// SubmitOutcome describes coordinator accept semantics for pool submit.
type SubmitOutcome struct {
	Async        bool   `json:"async,omitempty"`
	ReplayStatus string `json:"replay_status,omitempty"` // pending, done, hygiene_skip, sample, …
	QueueID      int64  `json:"queue_id,omitempty"`
}
