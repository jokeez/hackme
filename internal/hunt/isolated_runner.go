package hunt

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzupstream"
)

// IsolatedHarnessRunner returns a fuzzupstream.HarnessRunner that executes under
// RunIsolated (bwrap when available; STRICT fail-closed). Used by coordinator
// Hunt prove / trim so customer harnesses never get a bare exec on the hub.
//
// Stdout and stderr share one mutex-backed writer: RunIsolated's MultiWriter on
// stderr plus concurrent process pipes must not race a bare bytes.Buffer.
// Output is capped (DefaultHarnessOutputCap) so a flooding harness cannot OOM
// the parent coordinator (audit R03).
func IsolatedHarnessRunner() fuzzupstream.HarnessRunner {
	return func(ctx context.Context, bin string, args []string, env []string, stdin []byte, wall time.Duration) (string, error) {
		if wall <= 0 {
			wall = 8 * time.Second // match fuzzupstream harnessProveWall (bwrap cold start)
		}
		var out bytes.Buffer
		lim := newLimitedWriter(&out, DefaultHarnessOutputCap)
		sw := newSyncWriter(lim)
		var stdinR io.Reader
		if len(stdin) > 0 {
			stdinR = bytes.NewReader(stdin)
		}
		extraRo := []string{}
		if d := filepath.Dir(bin); d != "" && d != "." {
			extraRo = append(extraRo, d)
		}
		// Crash / corpus inputs are often siblings of the harness; RO-bind their dirs too.
		for _, a := range args {
			if a == "" || strings.HasPrefix(a, "-") {
				continue
			}
			if d := filepath.Dir(a); d != "" && d != "." {
				extraRo = append(extraRo, d)
			}
		}
		err := RunIsolated(ctx, IsolateOpts{
			Bin:         bin,
			Args:        args,
			Env:         ScrubBuildEnv(env),
			Wall:        wall,
			Stdin:       stdinR,
			Stdout:      sw,
			Stderr:      sw,
			AllowBare:   false,
			ExtraRoBind: extraRo,
			// ASAN/libFuzzer need large VA — RLIMIT_AS kills prove (see engine_runner).
			RSSLimitMB: 0,
		})
		return out.String(), err
	}
}
