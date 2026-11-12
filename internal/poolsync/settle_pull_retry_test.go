package poolsync

import "testing"

func TestSettleOutboxTransient(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"settle outbox HTTP 429: too many requests", true},
		{"settle outbox HTTP 502: error code: 502", true},
		{"settle outbox HTTP 503: unavailable", true},
		{"settle outbox HTTP 504: gateway", true},
		{"Get ...: read: connection reset by peer", true},
		{"settle outbox HTTP 401: admin authentication required", false},
		{"settle outbox HTTP 403: forbidden", false},
		{"settle outbox HTTP 500: boom", false},
	}
	for _, tc := range cases {
		err := errString(tc.msg)
		if got := settleOutboxTransient(err); got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.msg, got, tc.want)
		}
	}
	if settleOutboxTransient(nil) {
		t.Fatal("nil must not be transient")
	}
	if !SettleOutboxErrAuth(errString("settle outbox HTTP 403: forbidden")) {
		t.Fatal("403 must be auth")
	}
	if SettleOutboxErrAuth(errString("settle outbox HTTP 502: bad gateway")) {
		t.Fatal("502 must not be auth")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
