package hunt

import (
	"bytes"
	"strings"
	"testing"
)

func TestLimitedWriterCapsRetention(t *testing.T) {
	var buf bytes.Buffer
	w := newLimitedWriter(&buf, 16)
	n, err := w.Write([]byte(strings.Repeat("a", 40)))
	if err != nil || n != 40 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if buf.Len() != 16 {
		t.Fatalf("retained %d want 16", buf.Len())
	}
	if !w.Truncated() {
		t.Fatal("expected Truncated")
	}
}
