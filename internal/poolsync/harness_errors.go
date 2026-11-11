package poolsync

import (
	"fmt"
	"net/http"
	"strings"
)

// Stable Orders / Hunt harness publish+sync error codes (JSON `code` field).
const (
	CodeHarnessTooLarge        = "harness_too_large"
	CodePublishRateLimited     = "publish_rate_limited"
	CodeHarnessSyncFailed      = "harness_sync_failed"
	CodeHarnessContentMismatch = "harness_content_mismatch"
)

// CoordinatorHarnessUploadMaxBytes matches cmd/coordinator MaxBytesReader for
// POST /api/fuzz/pool/hunt/harness (keep in sync with hunt.CoordinatorHarnessUploadMaxBytes).
const CoordinatorHarnessUploadMaxBytes = 36 << 20

// ClassifyHarnessSyncError maps coordinator / local publish failures to stable API codes.
func ClassifyHarnessSyncError(err error) (code string, status int, message string) {
	if err == nil {
		return "", 0, ""
	}
	msg := strings.TrimSpace(err.Error())
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "harness_too_large"),
		strings.Contains(lower, "http 413"),
		strings.Contains(lower, "request entity too large"),
		strings.Contains(lower, "exceeds") && strings.Contains(lower, "bytes"):
		return CodeHarnessTooLarge, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("harness exceeds coordinator max (%d bytes); strip debug symbols or shrink binary", CoordinatorHarnessUploadMaxBytes)
	case strings.Contains(lower, "publish_rate_limited"),
		strings.Contains(lower, "http 429"),
		strings.Contains(lower, "rate limit"):
		return CodePublishRateLimited, http.StatusTooManyRequests,
			"orders publish rate limit exceeded — retry later (harness upload shares register caps)"
	case strings.Contains(lower, "content_sha") && strings.Contains(lower, "mismatch"),
		strings.Contains(lower, "already bound to different binary"):
		return CodeHarnessContentMismatch, http.StatusConflict, msg
	default:
		if msg == "" {
			msg = "harness coordinator sync failed"
		}
		return CodeHarnessSyncFailed, http.StatusBadGateway, msg
	}
}

// ErrHarnessTooLarge is returned by preflight when binary_path exceeds the upload max.
func ErrHarnessTooLarge(size, max int64) error {
	return fmt.Errorf("%s: harness size %d exceeds max %d bytes", CodeHarnessTooLarge, size, max)
}
