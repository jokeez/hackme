package hunt

import (
	"bytes"
	"io"
	"sync"
)

// DefaultHarnessOutputCap is the max stdout+stderr retained from an isolated
// customer harness (R03). Excess bytes are discarded so the parent cannot OOM
// on a flood while the child still sees successful writes.
const DefaultHarnessOutputCap = 1 << 20 // 1 MiB

// limitedWriter caps retained bytes in buf while always accepting Write so the
// child process is not killed by a broken pipe from the parent.
type limitedWriter struct {
	mu  sync.Mutex
	buf *bytes.Buffer
	max int
	n   int // total bytes observed (including discarded)
}

func newLimitedWriter(buf *bytes.Buffer, max int) *limitedWriter {
	if max <= 0 {
		max = DefaultHarnessOutputCap
	}
	return &limitedWriter{buf: buf, max: max}
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	remain := l.max - l.buf.Len()
	if remain > 0 {
		if len(p) <= remain {
			_, _ = l.buf.Write(p)
		} else {
			_, _ = l.buf.Write(p[:remain])
		}
	}
	l.n += len(p)
	return len(p), nil
}

// Truncated reports whether any bytes were discarded after the cap.
func (l *limitedWriter) Truncated() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n > l.max
}

// CapWriter wraps an arbitrary writer with a byte cap (discards after max).
func CapWriter(w io.Writer, max int) io.Writer {
	if w == nil {
		return nil
	}
	if max <= 0 {
		max = DefaultHarnessOutputCap
	}
	return &capWriter{w: w, max: max}
}

type capWriter struct {
	mu  sync.Mutex
	w   io.Writer
	max int
	n   int
}

func (c *capWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	remain := c.max - c.n
	if remain <= 0 {
		c.n += len(p)
		return len(p), nil
	}
	chunk := p
	if len(p) > remain {
		chunk = p[:remain]
	}
	_, err := c.w.Write(chunk)
	c.n += len(p)
	if err != nil {
		return len(p), err
	}
	return len(p), nil
}
