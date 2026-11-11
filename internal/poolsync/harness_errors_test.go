package poolsync

import (
	"fmt"
	"net/http"
	"testing"
)

func TestClassifyHarnessSyncError(t *testing.T) {
	cases := []struct {
		err    error
		code   string
		status int
	}{
		{nil, "", 0},
		{fmt.Errorf("pool harness sync HTTP 413: <html>413</html>"), CodeHarnessTooLarge, http.StatusRequestEntityTooLarge},
		{ErrHarnessTooLarge(40<<20, CoordinatorHarnessUploadMaxBytes), CodeHarnessTooLarge, http.StatusRequestEntityTooLarge},
		{fmt.Errorf("hunt artifact: exceeds %d bytes", 36<<20), CodeHarnessTooLarge, http.StatusRequestEntityTooLarge},
		{fmt.Errorf("pool harness sync HTTP 429: orders publish rate limit exceeded"), CodePublishRateLimited, http.StatusTooManyRequests},
		{fmt.Errorf("publish_rate_limited"), CodePublishRateLimited, http.StatusTooManyRequests},
		{fmt.Errorf("pool harness sync HTTP 502: bad gateway"), CodeHarnessSyncFailed, http.StatusBadGateway},
		{fmt.Errorf("hunt harness sync: dial tcp: connection refused"), CodeHarnessSyncFailed, http.StatusBadGateway},
		{fmt.Errorf("harness_content_sha256 mismatch with body"), CodeHarnessContentMismatch, http.StatusConflict},
		{fmt.Errorf("hunt harness sync: harness_hash abc already bound to different binary"), CodeHarnessContentMismatch, http.StatusConflict},
	}
	for _, tc := range cases {
		code, status, msg := ClassifyHarnessSyncError(tc.err)
		if code != tc.code || status != tc.status {
			t.Fatalf("err=%v → code=%q status=%d want %q/%d", tc.err, code, status, tc.code, tc.status)
		}
		if tc.err != nil && stringsEmpty(msg) {
			t.Fatalf("expected message for %v", tc.err)
		}
	}
}

func stringsEmpty(s string) bool { return s == "" }

func TestErrHarnessTooLargeMessage(t *testing.T) {
	err := ErrHarnessTooLarge(43<<20, CoordinatorHarnessUploadMaxBytes)
	code, status, _ := ClassifyHarnessSyncError(err)
	if code != CodeHarnessTooLarge || status != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %s/%d", code, status)
	}
}
