package main

import (
	"encoding/hex"
	"strings"

	"github.com/zeebo/blake3"
)

// publicWorkerKey returns a stable 8-byte hex pseudonym for a worker id (public APIs).
func publicWorkerKey(workerID string) string {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return ""
	}
	sum := blake3.Sum256([]byte("hackme-public-worker-v1|" + workerID))
	return hex.EncodeToString(sum[:8])
}

// publicWorkersMap re-keys workers under publicWorkerKey and drops raw hostname-like ids.
func publicWorkersMap(workers map[string]workerPayoutStat) map[string]workerPayoutStat {
	out := make(map[string]workerPayoutStat, len(workers))
	for id, st := range workers {
		key := publicWorkerKey(id)
		if key == "" {
			continue
		}
		st = redactPublicWorkerStat(st)
		if prev, ok := out[key]; ok {
			merged, _ := mergeWorkerStat(prev, st)
			out[key] = merged
			continue
		}
		out[key] = st
	}
	return out
}

