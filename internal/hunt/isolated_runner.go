package hunt

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"time"

	"hackme/internal/fuzzupstream"
)

// IsolatedHarnessRunner returns a fuzzupstream.HarnessRunner that executes under
// RunIsolated (bwrap when available; STRICT fail-closed). Used by coordinator
// Hunt prove / trim so customer harnesses never get a bare exec on the hub.
//
// Stdout and stderr share one mutex-backed writer: RunIsolated's MultiWriter on
// stderr plus concurrent process pipes must not race a bare bytes.Buffer.
func IsolatedHarnessRunner() fuzzupstream.HarnessRunner {
	return func(ctx context.Context, bin string, args []string, env []string, stdin []byte, wall time.Duration) (string, error) {
		if wall <= 0 {
			wall = 8 * time.Second // match fuzzupstream harnessProveWall (bwrap cold start)
		}
		var out bytes.Buffer
		sw := newSyncWriter(&out)
		var stdinR io.Reader
		if len(stdin) > 0 {
			stdinR = bytes.NewReader(stdin)
		}
		extraRo := []string{}
		if d := filepath.Dir(bin); d != "" && d != "." {
			extraRo = append(extraRo, d)
		}
		err := RunIsolated(ctx, IsolateOpts{
			Bin:         bin,
			Args:        args,
			Env:         env,
			Wall:        wall,
			Stdin:       stdinR,
			Stdout:      sw,
			Stderr:      sw,
			AllowBare:   false,
			ExtraRoBind: extraRo,
		})
		return out.String(), err
	}
}
