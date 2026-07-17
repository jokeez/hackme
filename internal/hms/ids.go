package hms

import (
	"regexp"
	"strings"
)

var workerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

// ValidateWorkerID rejects empty IDs and path-traversal characters.
func ValidateWorkerID(workerID string) error {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return errf("worker_id required")
	}
	if strings.Contains(workerID, "..") || strings.ContainsAny(workerID, `/\`) {
		return errf("worker_id path characters rejected")
	}
	if !workerIDPattern.MatchString(workerID) {
		return errf("worker_id must be alphanumeric (._- allowed, max 63)")
	}
	return nil
}

// ValidateChunkID rejects empty IDs and path-traversal characters.
func ValidateChunkID(chunkID string) error {
	chunkID = strings.TrimSpace(chunkID)
	if chunkID == "" {
		return errf("chunk_id required")
	}
	if strings.Contains(chunkID, "..") || strings.ContainsAny(chunkID, `/\`) {
		return errf("chunk_id path characters rejected")
	}
	if len(chunkID) > 128 {
		return errf("chunk_id too long")
	}
	return nil
}
