package workerid

import "testing"

func TestSanitizeHostname(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"My-PC.local", "my-pc-local"},
		{"  ", "local"},
		{"worker_01", "worker-01"},
		{"abc", "abc"},
	}
	for _, tc := range tests {
		if got := SanitizeHostname(tc.in); got != tc.want {
			t.Fatalf("SanitizeHostname(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
