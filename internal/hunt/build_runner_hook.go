package hunt

import (
	"context"
	"io"
	"time"

	"hackme/internal/fuzzupstream"
)

func init() {
	// Catalog builds (clang/cargo/afl-cc in fuzzupstream) inherit Hunt's bwrap cage
	// + scrubbed env when this package is linked (coordinator / hunt tools).
	fuzzupstream.BuildRunner = func(ctx context.Context, bin string, args []string, env []string, dir string, extraBind []string, wall time.Duration, stdout, stderr io.Writer) error {
		return RunBuildIsolated(ctx, BuildIsolateOpts{
			Bin:       bin,
			Args:      args,
			Dir:       dir,
			Env:       env,
			// RunBuildIsolated also filterSecretPrefixRoots ExtraBind/Dir — belt + suspenders.
			ExtraBind: filterSecretPrefixRoots(uniqueBindPaths(extraBind...)),
			Wall:      wall,
			Stdout:    stdout,
			Stderr:    stderr,
		})
	}
}
